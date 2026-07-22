// Команда server — точка входа приложения.
// На Этапе 1 поднимает подключение к TimescaleDB и применяет миграции;
// MQTT-подписчик и HTTP API подключаются на последующих этапах.
package main

import (
	"context"
	"database/sql"
	"log/slog"
	"os"

	_ "github.com/lib/pq"

	"github.com/MixFon/MQTT/internal/config"
	"github.com/MixFon/MQTT/internal/migrate"
	"github.com/MixFon/MQTT/migrations"
)

// main читает конфиг, подключается к БД, применяет миграции и логирует результат.
func main() {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))

	cfg, err := config.Load()
	if err != nil {
		logger.Error("load config", "error", err)
		os.Exit(1)
	}

	db, err := sql.Open("postgres", cfg.DatabaseURL)
	if err != nil {
		logger.Error("open database", "error", err)
		os.Exit(1)
	}
	defer db.Close()

	ctx := context.Background()
	if err := db.PingContext(ctx); err != nil {
		logger.Error("ping database", "error", err)
		os.Exit(1)
	}

	if err := migrate.Run(ctx, db, migrations.FS); err != nil {
		logger.Error("run migrations", "error", err)
		os.Exit(1)
	}

	logger.Info("migrations applied", "http_addr", cfg.HTTPAddr)
}
