// Package config читает конфигурацию приложения из переменных окружения.
package config

import (
	"errors"
	"os"
)

// Config — конфигурация приложения, собранная из переменных окружения один раз в main.
type Config struct {
	MQTTBrokerURL string
	MQTTUsername  string
	MQTTPassword  string
	DatabaseURL   string
	HTTPAddr      string
}

// Load читает переменные окружения и возвращает Config.
// Возвращает ошибку, если обязательные переменные не заданы.
func Load() (Config, error) {
	cfg := Config{
		MQTTBrokerURL: os.Getenv("MQTT_BROKER_URL"),
		MQTTUsername:  os.Getenv("MQTT_USERNAME"),
		MQTTPassword:  os.Getenv("MQTT_PASSWORD"),
		DatabaseURL:   os.Getenv("DATABASE_URL"),
		HTTPAddr:      os.Getenv("HTTP_ADDR"),
	}

	if cfg.DatabaseURL == "" {
		return Config{}, errors.New("DATABASE_URL is required")
	}
	if cfg.HTTPAddr == "" {
		cfg.HTTPAddr = ":8080"
	}

	return cfg, nil
}
