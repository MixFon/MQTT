// Package sensor содержит доменную модель показаний датчиков,
// общую для MQTT-подписчика, слоя хранения и REST API.
package sensor

import "time"

// Reading — одно показание датчика: комната, метрика (тип измерения),
// значение и момент получения. Metric — открытое множество, новый тип
// датчика не требует изменений в этой структуре.
type Reading struct {
	Room   string
	Metric string
	Value  float64
	Time   time.Time
}
