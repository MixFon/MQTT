package main

import (
	"bufio"
	"context"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// headerTimeout — сколько ждём CONNECT-запрос от клиента, пока не отдали
// соединение под TLS-туннель.
const headerTimeout = 10 * time.Second

// dialTimeout — сколько ждём установления TCP-соединения с целевым хостом.
const dialTimeout = 10 * time.Second

// Server — сам прокси: принимает TCP-соединения, разбирает CONNECT-запрос,
// проверяет авторизацию и разрешённый хост, дальше прозрачно прокидывает
// байты между клиентом и целевым хостом.
type Server struct {
	listenAddr   string
	allowedHosts map[string]struct{}
	token        string
	logger       *slog.Logger
}

// ListenAndServe открывает listener на s.listenAddr и принимает соединения
// в цикле, обрабатывая каждое в своей горутине. Блокируется до отмены ctx.
func (s *Server) ListenAndServe(ctx context.Context) error {
	ln, err := net.Listen("tcp", s.listenAddr)
	if err != nil {
		return fmt.Errorf("listen %s: %w", s.listenAddr, err)
	}

	go func() {
		<-ctx.Done()
		ln.Close()
	}()

	s.logger.Info("telegram-proxy listening", "addr", s.listenAddr, "allowed_hosts", s.allowedHostsList())

	for {
		conn, err := ln.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return fmt.Errorf("accept: %w", err)
		}
		go s.handleConn(conn)
	}
}

// allowedHostsList возвращает список разрешённых хостов как срез строк —
// только для логирования при старте.
func (s *Server) allowedHostsList() []string {
	hosts := make([]string, 0, len(s.allowedHosts))
	for h := range s.allowedHosts {
		hosts = append(hosts, h)
	}
	return hosts
}

// handleConn обрабатывает одно входящее соединение: разбирает CONNECT-
// запрос, проверяет авторизацию и хост, устанавливает соединение с целью
// и запускает прозрачную передачу байт в обе стороны.
func (s *Server) handleConn(conn net.Conn) {
	defer conn.Close()

	if err := conn.SetDeadline(time.Now().Add(headerTimeout)); err != nil {
		s.logger.Error("set deadline", "remote", conn.RemoteAddr(), "error", err)
		return
	}

	br := bufio.NewReader(conn)
	req, err := http.ReadRequest(br)
	if err != nil {
		s.logger.Warn("read connect request", "remote", conn.RemoteAddr(), "error", err)
		return
	}

	if req.Method != http.MethodConnect {
		s.logger.Warn("unsupported method", "remote", conn.RemoteAddr(), "method", req.Method)
		writeStatus(conn, http.StatusMethodNotAllowed)
		return
	}

	if !s.authorized(req) {
		s.logger.Warn("unauthorized connect", "remote", conn.RemoteAddr(), "host", req.Host)
		writeStatus(conn, http.StatusProxyAuthRequired)
		return
	}

	if !s.hostAllowed(req.Host) {
		s.logger.Warn("host not allowed", "remote", conn.RemoteAddr(), "host", req.Host)
		writeStatus(conn, http.StatusForbidden)
		return
	}

	target, err := net.DialTimeout("tcp", req.Host, dialTimeout)
	if err != nil {
		s.logger.Error("dial target", "host", req.Host, "error", err)
		writeStatus(conn, http.StatusBadGateway)
		return
	}
	defer target.Close()

	if _, err := conn.Write([]byte("HTTP/1.1 200 Connection Established\r\n\r\n")); err != nil {
		s.logger.Error("write connect response", "remote", conn.RemoteAddr(), "error", err)
		return
	}

	// Снимаем таймаут заголовков — дальше соединение живёт, пока живёт
	// сам HTTPS-запрос клиента к Telegram.
	if err := conn.SetDeadline(time.Time{}); err != nil {
		s.logger.Error("reset deadline", "remote", conn.RemoteAddr(), "error", err)
		return
	}

	s.logger.Info("tunnel established", "remote", conn.RemoteAddr(), "host", req.Host)
	relay(br, conn, target)
}

// authorized проверяет пароль из заголовка Proxy-Authorization (Basic) —
// он должен совпадать с s.token. Логин в паре "логин:пароль" не проверяется,
// он нужен только потому, что net/http сам формирует Basic-авторизацию из
// userinfo в URL прокси (см. internal/alert.NewTelegramNotifier).
func (s *Server) authorized(req *http.Request) bool {
	const prefix = "Basic "
	header := req.Header.Get("Proxy-Authorization")
	if !strings.HasPrefix(header, prefix) {
		return false
	}

	decoded, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(header, prefix))
	if err != nil {
		return false
	}

	_, password, ok := strings.Cut(string(decoded), ":")
	if !ok {
		return false
	}

	return subtle.ConstantTimeCompare([]byte(password), []byte(s.token)) == 1
}

// hostAllowed проверяет, что hostport (вида "host:port" из CONNECT-запроса)
// указывает на разрешённый хост и порт 443 — прокси создан только для
// HTTPS-запросов к Telegram Bot API, другие порты не нужны и не разрешены.
func (s *Server) hostAllowed(hostport string) bool {
	host, port, err := net.SplitHostPort(hostport)
	if err != nil {
		return false
	}
	if port != "443" {
		return false
	}
	_, ok := s.allowedHosts[strings.ToLower(host)]
	return ok
}

// writeStatus пишет клиенту минимальный HTTP-ответ с кодом code и без тела —
// используется для отказов (403/407/405/502) до установления туннеля.
func writeStatus(conn net.Conn, code int) {
	fmt.Fprintf(conn, "HTTP/1.1 %d %s\r\n\r\n", code, http.StatusText(code))
}

// relay прозрачно копирует байты между клиентом и целевым соединением в обе
// стороны и ждёт завершения обоих направлений. br может содержать байты,
// прочитанные с запасом при разборе CONNECT-запроса (начало TLS-хендшейка
// клиента) — их нужно переслать target первыми, иначе они потеряются.
func relay(br *bufio.Reader, client, target net.Conn) {
	if n := br.Buffered(); n > 0 {
		buf := make([]byte, n)
		if _, err := io.ReadFull(br, buf); err == nil {
			target.Write(buf)
		}
	}

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		io.Copy(target, client)
		target.Close()
	}()
	go func() {
		defer wg.Done()
		io.Copy(client, target)
		client.Close()
	}()
	wg.Wait()
}
