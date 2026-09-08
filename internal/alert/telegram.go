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
func NewTelegramNotifier(botToken, chatID string) *TelegramNotifier {
	return &TelegramNotifier{
		botToken: botToken,
		chatID:   chatID,
		client:   &http.Client{},
	}
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
