// Package alert — фоновая проверка показаний датчиков: определяет, что датчик
// перестал присылать данные (offline) или показание вышло за заданные пороги,
// и отправляет уведомление через Notifier. Ничего не знает про MQTT или HTTP —
// работает поверх internal/storage и internal/sensor.
package alert

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/MixFon/MQTT/internal/sensor"
	"github.com/MixFon/MQTT/internal/storage"
)

// Threshold — допустимый диапазон значений метрики. nil-граница значит,
// что с этой стороны диапазон не ограничен.
type Threshold struct {
	Min *float64
	Max *float64
}

// Breach сообщает, нарушает ли value границы порога, и если да — текстовую причину.
func (t Threshold) Breach(value float64) (bool, string) {
	if t.Max != nil && value > *t.Max {
		return true, fmt.Sprintf("выше максимума %.2f", *t.Max)
	}
	if t.Min != nil && value < *t.Min {
		return true, fmt.Sprintf("ниже минимума %.2f", *t.Min)
	}
	return false, ""
}

// ParseThresholds разбирает пороги из строки вида "metric:min:max,metric2:min:max",
// где min/max можно опустить (пустая граница = не ограничена), например
// "temperature::30,humidity:20:80". Пустая строка — пороги не заданы, ошибка не возвращается.
func ParseThresholds(raw string) (map[string]Threshold, error) {
	thresholds := map[string]Threshold{}
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return thresholds, nil
	}

	for _, part := range strings.Split(raw, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}

		fields := strings.Split(part, ":")
		if len(fields) != 3 {
			return nil, fmt.Errorf("некорректный формат порога %q, ожидается metric:min:max", part)
		}

		metric := strings.TrimSpace(fields[0])
		if metric == "" {
			return nil, fmt.Errorf("не указана метрика в пороге %q", part)
		}

		min, err := parseBound(fields[1])
		if err != nil {
			return nil, fmt.Errorf("порог %q: min: %w", part, err)
		}
		max, err := parseBound(fields[2])
		if err != nil {
			return nil, fmt.Errorf("порог %q: max: %w", part, err)
		}

		thresholds[metric] = Threshold{Min: min, Max: max}
	}

	return thresholds, nil
}

// parseBound парсит одну границу порога: пустая строка — граница не задана.
func parseBound(raw string) (*float64, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	value, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return nil, fmt.Errorf("parse float %q: %w", raw, err)
	}
	return &value, nil
}

// Notifier отправляет текстовое уведомление пользователю (например, в Telegram).
type Notifier interface {
	Notify(ctx context.Context, message string) error
}

// Config — параметры фоновой проверки датчиков.
type Config struct {
	// CheckInterval — как часто проверять показания.
	CheckInterval time.Duration
	// OfflineAfter — через сколько времени без новых показаний метрика считается offline.
	OfflineAfter time.Duration
	// Thresholds — допустимые диапазоны значений по метрикам (metric -> Threshold).
	// Метрики без записи в карте порогами не проверяются.
	Thresholds map[string]Threshold
}

// Checker периодически опрашивает последние показания датчиков и уведомляет
// об уходе в offline и о выходе значений за пороги. Уведомление отправляется
// только на переходе состояния (стало плохо / снова стало хорошо), а не на каждой проверке.
type Checker struct {
	cfg      Config
	store    *storage.Storage
	notifier Notifier
	logger   *slog.Logger

	// offline и threshold хранят текущее состояние алерта по ключу "room|metric" —
	// true, если по этому ключу уведомление уже отправлено и ситуация не восстановилась.
	offline   map[string]bool
	threshold map[string]bool
}

// New создаёт Checker поверх готовых Storage и Notifier.
func New(cfg Config, store *storage.Storage, notifier Notifier, logger *slog.Logger) *Checker {
	return &Checker{
		cfg:       cfg,
		store:     store,
		notifier:  notifier,
		logger:    logger,
		offline:   map[string]bool{},
		threshold: map[string]bool{},
	}
}

// Run запускает периодическую проверку показаний с интервалом cfg.CheckInterval
// и блокируется до отмены ctx. Первая проверка выполняется сразу, не дожидаясь тика.
func (c *Checker) Run(ctx context.Context) {
	c.checkOnce(ctx)

	ticker := time.NewTicker(c.cfg.CheckInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			c.checkOnce(ctx)
		}
	}
}

// checkOnce читает последние показания по всем комнатам и метрикам и проверяет
// каждое на offline и на выход за пороги.
func (c *Checker) checkOnce(ctx context.Context) {
	readings, err := c.store.LatestAll(ctx)
	if err != nil {
		c.logger.Error("alert: get latest readings", "error", err)
		return
	}

	now := time.Now()
	for _, r := range readings {
		key := r.Room + "|" + r.Metric
		c.checkOffline(ctx, key, r, now)
		if th, ok := c.cfg.Thresholds[r.Metric]; ok {
			c.checkThreshold(ctx, key, r, th)
		}
	}
}

// checkOffline сравнивает возраст последнего показания с cfg.OfflineAfter и шлёт
// уведомление на переходе online -> offline и обратно.
func (c *Checker) checkOffline(ctx context.Context, key string, r sensor.Reading, now time.Time) {
	age := now.Sub(r.Time)
	isOffline := age >= c.cfg.OfflineAfter
	wasOffline := c.offline[key]

	switch {
	case isOffline && !wasOffline:
		c.notify(ctx, fmt.Sprintf(
			"⚠️ %s/%s не в сети уже %s (последнее показание в %s)",
			r.Room, r.Metric, age.Round(time.Second), r.Time.Format("2006-01-02 15:04:05"),
		))
		c.offline[key] = true
	case !isOffline && wasOffline:
		c.notify(ctx, fmt.Sprintf("✅ %s/%s снова на связи", r.Room, r.Metric))
		c.offline[key] = false
	}
}

// checkThreshold сравнивает значение показания с порогом и шлёт уведомление
// на переходе в нарушение порога и обратно в норму.
func (c *Checker) checkThreshold(ctx context.Context, key string, r sensor.Reading, th Threshold) {
	breach, reason := th.Breach(r.Value)
	wasBreach := c.threshold[key]

	switch {
	case breach && !wasBreach:
		c.notify(ctx, fmt.Sprintf("🚨 %s/%s = %.2f — %s", r.Room, r.Metric, r.Value, reason))
		c.threshold[key] = true
	case !breach && wasBreach:
		c.notify(ctx, fmt.Sprintf("✅ %s/%s = %.2f — снова в норме", r.Room, r.Metric, r.Value))
		c.threshold[key] = false
	}
}

// notify отправляет уведомление через Notifier и логирует ошибку отправки,
// не прерывая проверку остальных показаний.
func (c *Checker) notify(ctx context.Context, message string) {
	if err := c.notifier.Notify(ctx, message); err != nil {
		c.logger.Error("alert: send notification", "error", err)
	}
}
