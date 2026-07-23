// Команда server — точка входа приложения.
// Поднимает подключение к TimescaleDB, применяет миграции и запускает
// MQTT-подписчик и HTTP REST API.
package main

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	_ "github.com/lib/pq"

	"github.com/MixFon/MQTT/internal/api"
	"github.com/MixFon/MQTT/internal/config"
	"github.com/MixFon/MQTT/internal/migrate"
	"github.com/MixFon/MQTT/internal/mqtt"
	"github.com/MixFon/MQTT/internal/storage"
	"github.com/MixFon/MQTT/migrations"
)

// shutdownTimeout — время, отведённое HTTP-серверу на завершение текущих
// запросов при остановке приложения.
const shutdownTimeout = 5 * time.Second

// main читает конфиг, подключается к БД, применяет миграции, запускает
// MQTT-подписчик и ждёт сигнала завершения (SIGINT/SIGTERM) для остановки.
func main() {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))

	cfg, err := config.Load()
	if err != nil {
		logger.Error("load config", "error", err)
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	db, err := sql.Open("postgres", cfg.DatabaseURL)
	if err != nil {
		logger.Error("open database", "error", err)
		os.Exit(1)
	}
	defer db.Close()

	if err := db.PingContext(ctx); err != nil {
		logger.Error("ping database", "error", err)
		os.Exit(1)
	}

	if err := migrate.Run(ctx, db, migrations.FS); err != nil {
		logger.Error("run migrations", "error", err)
		os.Exit(1)
	}

	store := storage.New(db)

	subscriber := mqtt.New(mqtt.Config{
		BrokerURL: cfg.MQTTBrokerURL,
		Username:  cfg.MQTTUsername,
		Password:  cfg.MQTTPassword,
	}, logger, store.InsertReading)

	if err := subscriber.Start(); err != nil {
		logger.Error("start mqtt subscriber", "error", err)
		os.Exit(1)
	}
	defer subscriber.Stop()

	httpServer := &http.Server{
		Addr:    cfg.HTTPAddr,
		Handler: api.New(store, logger).Router(),
	}

	go func() {
		logger.Info("http server started", "addr", cfg.HTTPAddr)
		if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("http server", "error", err)
		}
	}()

	<-ctx.Done()
	logger.Info("shutting down")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		logger.Error("shutdown http server", "error", err)
	}
}
