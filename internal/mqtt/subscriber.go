// Package mqtt — подписчик на топики home/+/+, ничего не знает про HTTP или
// схему БД: разобранные показания отдаются наружу через Handler.
package mqtt

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	paho "github.com/eclipse/paho.mqtt.golang"

	"github.com/MixFon/MQTT/internal/sensor"
)

// topicPrefix и statusMetric описывают соглашение по топикам:
// home/{room}/{metric}, где {metric} == "status" — служебное сообщение
// online/offline (Last Will), а не числовое показание для записи в БД.
const (
	topicFilter  = "home/+/+"
	topicPrefix  = "home"
	statusMetric = "status"
)

// Config — параметры подключения к MQTT-брокеру.
type Config struct {
	BrokerURL string
	Username  string
	Password  string
}

// Handler обрабатывает одно распарсенное показание датчика (обычно — запись в БД).
type Handler func(ctx context.Context, r sensor.Reading) error

// Subscriber — подписчик на топики home/+/+ с TLS и автопереподключением к брокеру.
type Subscriber struct {
	client  paho.Client
	logger  *slog.Logger
	handler Handler
}

// New создаёт Subscriber и настраивает клиента с автопереподключением,
// но не подключается к брокеру — для этого нужно вызвать Start.
func New(cfg Config, logger *slog.Logger, handler Handler) *Subscriber {
	s := &Subscriber{logger: logger, handler: handler}

	opts := paho.NewClientOptions().
		AddBroker(cfg.BrokerURL).
		SetUsername(cfg.Username).
		SetPassword(cfg.Password).
		SetAutoReconnect(true).
		SetOnConnectHandler(s.onConnect).
		SetConnectionLostHandler(s.onConnectionLost)

	s.client = paho.NewClient(opts)
	return s
}

// Start подключается к брокеру и блокируется до установления соединения (или ошибки).
// Подписка на топики выполняется в onConnect и повторяется при каждом автопереподключении.
func (s *Subscriber) Start() error {
	token := s.client.Connect()
	token.Wait()
	if err := token.Error(); err != nil {
		return fmt.Errorf("connect to mqtt broker: %w", err)
	}
	return nil
}

// Stop корректно отключается от брокера, дожидаясь завершения текущих операций.
func (s *Subscriber) Stop() {
	s.client.Disconnect(250)
}

// onConnect подписывается на общий топик home/+/+. Вызывается paho при каждом
// установлении соединения, включая повторные — после разрыва и автопереподключения.
func (s *Subscriber) onConnect(client paho.Client) {
	token := client.Subscribe(topicFilter, 1, s.onMessage)
	token.Wait()
	if err := token.Error(); err != nil {
		s.logger.Error("mqtt subscribe", "topic", topicFilter, "error", err)
		return
	}
	s.logger.Info("mqtt connected and subscribed", "topic", topicFilter)
}

// onConnectionLost логирует разрыв соединения; само переподключение делает paho (AutoReconnect).
func (s *Subscriber) onConnectionLost(_ paho.Client, err error) {
	s.logger.Warn("mqtt connection lost", "error", err)
}

// onMessage разбирает входящее сообщение и передаёт результат в handler.
// Некорректный топик или payload логируется и отбрасывается — подписчик не падает
// и не перестаёт получать следующие сообщения.
func (s *Subscriber) onMessage(_ paho.Client, msg paho.Message) {
	room, metric, ok := splitTopic(msg.Topic())
	if !ok {
		s.logger.Error("unexpected mqtt topic", "topic", msg.Topic())
		return
	}

	// status — служебное online/offline от Last Will, не показание датчика.
	if metric == statusMetric {
		s.logger.Info("device status", "room", room, "status", string(msg.Payload()))
		return
	}

	reading, err := parseReading(room, metric, msg.Payload())
	if err != nil {
		s.logger.Error("parse mqtt payload", "topic", msg.Topic(), "error", err)
		return
	}

	if err := s.handler(context.Background(), reading); err != nil {
		s.logger.Error("handle reading", "topic", msg.Topic(), "error", err)
	}
}

// splitTopic разбирает топик вида home/{room}/{metric} на комнату и метрику.
// ok == false, если топик не соответствует этому формату.
func splitTopic(topic string) (room, metric string, ok bool) {
	parts := strings.Split(topic, "/")
	if len(parts) != 3 || parts[0] != topicPrefix {
		return "", "", false
	}
	return parts[1], parts[2], true
}

// parseReading парсит числовой payload в sensor.Reading для заданных комнаты и метрики.
// Метрика не сверяется со списком известных — новый тип датчика пишется как есть,
// валидация набора метрик (если понадобится) делается на уровне API, не здесь.
func parseReading(room, metric string, payload []byte) (sensor.Reading, error) {
	value, err := strconv.ParseFloat(strings.TrimSpace(string(payload)), 64)
	if err != nil {
		return sensor.Reading{}, fmt.Errorf("parse payload %q: %w", payload, err)
	}
	return sensor.Reading{
		Room:   room,
		Metric: metric,
		Value:  value,
		Time:   time.Now(),
	}, nil
}
