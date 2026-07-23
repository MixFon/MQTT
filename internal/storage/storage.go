// Package storage — слой работы с TimescaleDB: запись и чтение показаний датчиков.
// Ничего не знает про MQTT или HTTP.
package storage

import (
	"context"
	"database/sql"
	"fmt"
	"time"

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

// Bucket — агрегированное (усреднённое) значение метрики за один временной интервал.
type Bucket struct {
	Time  time.Time `json:"time"`
	Value float64   `json:"value"`
}

// ReadingsQuery — параметры выборки агрегированных показаний одной метрики в комнате.
type ReadingsQuery struct {
	Room     string
	Metric   string
	From     time.Time
	To       time.Time
	Interval string // строка вида "5 minutes", парсится Postgres как interval
}

// Readings возвращает показания room/metric за период [From, To], усреднённые
// по интервалам Interval через TimescaleDB time_bucket.
func (s *Storage) Readings(ctx context.Context, q ReadingsQuery) ([]Bucket, error) {
	const stmt = `
		SELECT time_bucket($1::interval, time) AS bucket, avg(value) AS value
		FROM sensor_readings
		WHERE room = $2 AND metric = $3 AND time >= $4 AND time <= $5
		GROUP BY bucket
		ORDER BY bucket`

	rows, err := s.db.QueryContext(ctx, stmt, q.Interval, q.Room, q.Metric, q.From, q.To)
	if err != nil {
		return nil, fmt.Errorf("query readings: %w", err)
	}
	defer rows.Close()

	buckets := []Bucket{}
	for rows.Next() {
		var b Bucket
		if err := rows.Scan(&b.Time, &b.Value); err != nil {
			return nil, fmt.Errorf("scan bucket: %w", err)
		}
		buckets = append(buckets, b)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate buckets: %w", err)
	}
	return buckets, nil
}

// Latest возвращает последнее показание по каждой метрике, которая пишется в заданной комнате.
func (s *Storage) Latest(ctx context.Context, room string) ([]sensor.Reading, error) {
	const stmt = `
		SELECT DISTINCT ON (metric) time, room, metric, value
		FROM sensor_readings
		WHERE room = $1
		ORDER BY metric, time DESC`

	rows, err := s.db.QueryContext(ctx, stmt, room)
	if err != nil {
		return nil, fmt.Errorf("query latest readings: %w", err)
	}
	defer rows.Close()

	readings := []sensor.Reading{}
	for rows.Next() {
		var r sensor.Reading
		if err := rows.Scan(&r.Time, &r.Room, &r.Metric, &r.Value); err != nil {
			return nil, fmt.Errorf("scan reading: %w", err)
		}
		readings = append(readings, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate readings: %w", err)
	}
	return readings, nil
}

// Rooms возвращает список комнат, по которым есть хотя бы одно показание.
func (s *Storage) Rooms(ctx context.Context) ([]string, error) {
	const stmt = `SELECT DISTINCT room FROM sensor_readings ORDER BY room`
	return s.queryStrings(ctx, stmt)
}

// Metrics возвращает список метрик, которые реально пишутся по заданной комнате
// (а не фиксированный список) — так фронт узнаёт о новых датчиках без хардкода.
func (s *Storage) Metrics(ctx context.Context, room string) ([]string, error) {
	const stmt = `SELECT DISTINCT metric FROM sensor_readings WHERE room = $1 ORDER BY metric`
	return s.queryStrings(ctx, stmt, room)
}

// queryStrings выполняет запрос, возвращающий один текстовый столбец, и собирает
// результат в срез строк. Общий код для Rooms и Metrics.
func (s *Storage) queryStrings(ctx context.Context, query string, args ...any) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query: %w", err)
	}
	defer rows.Close()

	values := []string{}
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err != nil {
			return nil, fmt.Errorf("scan value: %w", err)
		}
		values = append(values, v)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate values: %w", err)
	}
	return values, nil
}
