// Package storage — слой работы с TimescaleDB: запись и чтение показаний датчиков.
// Ничего не знает про MQTT или HTTP.
package storage

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/MixFon/MQTT/internal/sensor"
)

// Storage хранит подключение к БД и предоставляет методы для работы с показаниями.
type Storage struct {
	db *sql.DB
}

// New создаёт Storage поверх уже открытого и проверенного (Ping) подключения к БД.
func New(db *sql.DB) *Storage {
	return &Storage{db: db}
}

// InsertReading записывает одно показание датчика в таблицу sensor_readings.
func (s *Storage) InsertReading(ctx context.Context, r sensor.Reading) error {
	const stmt = `INSERT INTO sensor_readings (time, room, metric, value) VALUES ($1, $2, $3, $4)`
	if _, err := s.db.ExecContext(ctx, stmt, r.Time, r.Room, r.Metric, r.Value); err != nil {
		return fmt.Errorf("insert reading: %w", err)
	}
	return nil
}
