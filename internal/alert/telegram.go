package alert

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// telegramAPIURL — базовый URL Telegram Bot API для отправки сообщений.
const telegramAPIURL = "https://api.telegram.org"

// TelegramNotifier отправляет уведомления через Telegram Bot API обычным
// HTTP-запросом (net/http), без стороннего SDK.
type TelegramNotifier struct {
	botToken string
	chatID   string
	client   *http.Client
}

// NewTelegramNotifier создаёт TelegramNotifier для бота botToken, отправляющий
// сообщения в чат chatID (id пользователя или группы, который узнают у @userinfobot
// либо из ответа getUpdates после первого сообщения боту).
//
// Если proxyURL не пустой, запросы к Telegram Bot API идут через HTTP(S)-прокси
// по этому адресу вместо прямого соединения — нужно, когда сам сервер не имеет
// сетевого доступа к api.telegram.org (например, блокировка в РФ), а прокси
// поднят на другой стороне VPN-туннеля (см. cmd/telegram-proxy). Формат:
// "http://логин:токен@host:port" — логин и токен net/http сам добавит в
// заголовок Proxy-Authorization при установке CONNECT-туннеля.
func NewTelegramNotifier(botToken, chatID, proxyURL string) (*TelegramNotifier, error) {
	client := &http.Client{}
	if proxyURL != "" {
		u, err := url.Parse(proxyURL)
		if err != nil {
			return nil, fmt.Errorf("parse telegram proxy url: %w", err)
		}
		client.Transport = &http.Transport{Proxy: http.ProxyURL(u)}
	}

	return &TelegramNotifier{
		botToken: botToken,
		chatID:   chatID,
		client:   client,
	}, nil
}

// Notify отправляет message в чат c.chatID методом sendMessage Telegram Bot API.
func (t *TelegramNotifier) Notify(ctx context.Context, message string) error {
	endpoint := fmt.Sprintf("%s/bot%s/sendMessage", telegramAPIURL, t.botToken)

	form := url.Values{
		"chat_id": {t.chatID},
		"text":    {message},
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return fmt.Errorf("build telegram request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := t.client.Do(req)
	if err != nil {
		return fmt.Errorf("send telegram request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("telegram api returned %d: %s", resp.StatusCode, body)
	}
	return nil
}
