// Команда telegram-proxy — минимальный forward-прокси для доступа к Telegram
// Bot API. Разворачивается НЕ на iot-backend VPS, а на машине с OpenVPN-
// сервером (она должна иметь реальный сетевой доступ к api.telegram.org) —
// см. CLAUDE.md, раздел про обход блокировки Telegram.
//
// iot-backend подключается к этому прокси через VPN-туннель и ходит через
// него на api.telegram.org методом HTTP CONNECT — сам прокси лишь проверяет
// авторизацию, ограничивает список разрешённых хостов и прозрачно
// прокидывает байты TLS-сессии, не терминируя её.
package main

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"
)

// proxyConfig — конфигурация telegram-proxy, читается один раз в main из
// переменных окружения.
type proxyConfig struct {
	// listenAddr — адрес, на котором прокси принимает соединения. Должен
	// быть внутренним VPN-адресом (например, адресом tun-интерфейса), а не
	// публичным — иначе прокси будет доступен всем в интернете.
	listenAddr string
	// allowedHosts — множество хостов, на которые прокси разрешает CONNECT
	// (порт всегда должен быть 443, это не настраивается).
	allowedHosts map[string]struct{}
	// token — общий секрет, ожидаемый в пароле заголовка Proxy-Authorization.
	// Обязателен: в той же VPN-сети есть другие клиенты, и без токена любой
	// из них мог бы использовать прокси как открытый релей.
	token string
}

// loadConfig читает proxyConfig из переменных окружения PROXY_LISTEN_ADDR,
// PROXY_ALLOWED_HOSTS и PROXY_TOKEN. Возвращает ошибку, если обязательные
// переменные не заданы.
func loadConfig() (proxyConfig, error) {
	listenAddr := os.Getenv("PROXY_LISTEN_ADDR")
	if listenAddr == "" {
		return proxyConfig{}, errors.New("PROXY_LISTEN_ADDR is required")
	}

	token := os.Getenv("PROXY_TOKEN")
	if token == "" {
		return proxyConfig{}, errors.New("PROXY_TOKEN is required")
	}

	rawHosts := os.Getenv("PROXY_ALLOWED_HOSTS")
	if rawHosts == "" {
		rawHosts = "api.telegram.org"
	}
	allowedHosts := map[string]struct{}{}
	for _, h := range strings.Split(rawHosts, ",") {
		h = strings.ToLower(strings.TrimSpace(h))
		if h != "" {
			allowedHosts[h] = struct{}{}
		}
	}

	return proxyConfig{
		listenAddr:   listenAddr,
		allowedHosts: allowedHosts,
		token:        token,
	}, nil
}

// main читает конфиг и запускает Server, слушая до сигнала завершения
// (SIGINT/SIGTERM).
func main() {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))

	cfg, err := loadConfig()
	if err != nil {
		logger.Error("load config", "error", err)
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	srv := &Server{
		listenAddr:   cfg.listenAddr,
		allowedHosts: cfg.allowedHosts,
		token:        cfg.token,
		logger:       logger,
	}

	if err := srv.ListenAndServe(ctx); err != nil {
		logger.Error("telegram-proxy stopped", "error", err)
		os.Exit(1)
	}
}
