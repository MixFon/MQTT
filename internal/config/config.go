// Package config читает конфигурацию приложения из переменных окружения.
package config

import (
	"errors"
	"fmt"
	"os"
	"time"
)

// Config — конфигурация приложения, собранная из переменных окружения один раз в main.
type Config struct {
	MQTTBrokerURL  string
	MQTTUsername   string
	MQTTPassword   string
	MQTTCACertFile string
	DatabaseURL    string
	HTTPAddr       string

	// TelegramBotToken и TelegramChatID — реквизиты для отправки алертов в Telegram.
	// Если хотя бы одна пустая, фоновая проверка алертов не запускается (см. main.go).
	TelegramBotToken string
	TelegramChatID   string
	// AlertCheckInterval — как часто проверять показания на offline и превышение порогов.
	AlertCheckInterval time.Duration
	// AlertOfflineAfter — через сколько времени без новых показаний метрика считается offline.
	AlertOfflineAfter time.Duration
	// AlertThresholdsRaw — сырая строка порогов вида "metric:min:max,...",
	// парсится в internal/alert.ParseThresholds.
	AlertThresholdsRaw string
}

// Load читает переменные окружения и возвращает Config.
// Возвращает ошибку, если обязательные переменные не заданы.
func Load() (Config, error) {
	cfg := Config{
		MQTTBrokerURL:      os.Getenv("MQTT_BROKER_URL"),
		MQTTUsername:       os.Getenv("MQTT_USERNAME"),
		MQTTPassword:       os.Getenv("MQTT_PASSWORD"),
		MQTTCACertFile:     os.Getenv("MQTT_CA_CERT_FILE"),
		DatabaseURL:        os.Getenv("DATABASE_URL"),
		HTTPAddr:           os.Getenv("HTTP_ADDR"),
		TelegramBotToken:   os.Getenv("TELEGRAM_BOT_TOKEN"),
		TelegramChatID:     os.Getenv("TELEGRAM_CHAT_ID"),
		AlertThresholdsRaw: os.Getenv("ALERT_THRESHOLDS"),
	}

	if cfg.DatabaseURL == "" {
		return Config{}, errors.New("DATABASE_URL is required")
	}
	if cfg.HTTPAddr == "" {
		cfg.HTTPAddr = ":8080"
	}

	var err error
	cfg.AlertCheckInterval, err = durationEnv("ALERT_CHECK_INTERVAL", time.Minute)
	if err != nil {
		return Config{}, err
	}
	cfg.AlertOfflineAfter, err = durationEnv("ALERT_OFFLINE_AFTER", 3*time.Minute)
	if err != nil {
		return Config{}, err
	}

	return cfg, nil
}

// durationEnv читает переменную окружения name как time.Duration; если она не задана,
// возвращает def. Формат — как у time.ParseDuration ("1m", "3m30s", "1h").
func durationEnv(name string, def time.Duration) (time.Duration, error) {
	v := os.Getenv(name)
	if v == "" {
		return def, nil
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		return 0, fmt.Errorf("parse %s: %w", name, err)
	}
	return d, nil
}
