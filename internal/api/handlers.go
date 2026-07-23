package api

import (
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/MixFon/MQTT/internal/storage"
)

// defaultInterval и defaultRange — значения по умолчанию для /api/readings,
// если клиент не передал interval или from.
const (
	defaultInterval = "5 minutes"
	defaultRange    = 24 * time.Hour
)

// intervalPattern ограничивает interval простыми конструкциями Postgres INTERVAL
// вида "5 minutes", "1 hour 30 minutes" — этого достаточно для агрегации показаний
// и защищает от передачи в СУБД произвольного текста.
var intervalPattern = regexp.MustCompile(`(?i)^([0-9]+\s*(seconds?|minutes?|hours?|days?|weeks?|months?|years?)\s*)+$`)

// handleRooms отдаёт список комнат, по которым есть хотя бы одно показание.
func (a *API) handleRooms(w http.ResponseWriter, r *http.Request) {
	rooms, err := a.store.Rooms(r.Context())
	if err != nil {
		a.writeInternalError(w, "get rooms", err)
		return
	}
	writeJSON(w, http.StatusOK, rooms)
}

// handleMetrics отдаёт список метрик, которые реально пишутся по указанной комнате.
func (a *API) handleMetrics(w http.ResponseWriter, r *http.Request) {
	room := r.URL.Query().Get("room")
	if room == "" {
		writeError(w, http.StatusBadRequest, "room is required")
		return
	}

	metrics, err := a.store.Metrics(r.Context(), room)
	if err != nil {
		a.writeInternalError(w, "get metrics", err)
		return
	}
	writeJSON(w, http.StatusOK, metrics)
}

// handleLatest отдаёт последнее показание по каждой метрике для указанной комнаты.
func (a *API) handleLatest(w http.ResponseWriter, r *http.Request) {
	room := r.URL.Query().Get("room")
	if room == "" {
		writeError(w, http.StatusBadRequest, "room is required")
		return
	}

	readings, err := a.store.Latest(r.Context(), room)
	if err != nil {
		a.writeInternalError(w, "get latest readings", err)
		return
	}
	writeJSON(w, http.StatusOK, readings)
}

// handleReadings отдаёт агрегированные по интервалам показания для комнаты и метрики
// за период [from, to], по умолчанию — последние 24 часа с шагом 5 минут.
func (a *API) handleReadings(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()

	room := query.Get("room")
	if room == "" {
		writeError(w, http.StatusBadRequest, "room is required")
		return
	}
	metric := query.Get("metric")
	if metric == "" {
		writeError(w, http.StatusBadRequest, "metric is required")
		return
	}

	to := time.Now()
	if v := query.Get("to"); v != "" {
		parsed, err := time.Parse(time.RFC3339, v)
		if err != nil {
			writeError(w, http.StatusBadRequest, "to must be RFC3339")
			return
		}
		to = parsed
	}

	from := to.Add(-defaultRange)
	if v := query.Get("from"); v != "" {
		parsed, err := time.Parse(time.RFC3339, v)
		if err != nil {
			writeError(w, http.StatusBadRequest, "from must be RFC3339")
			return
		}
		from = parsed
	}

	interval := strings.TrimSpace(query.Get("interval"))
	if interval == "" {
		interval = defaultInterval
	} else if !intervalPattern.MatchString(interval) {
		writeError(w, http.StatusBadRequest, "interval must look like '5 minutes' or '1 hour'")
		return
	}

	buckets, err := a.store.Readings(r.Context(), storage.ReadingsQuery{
		Room:     room,
		Metric:   metric,
		From:     from,
		To:       to,
		Interval: interval,
	})
	if err != nil {
		a.writeInternalError(w, "get readings", err)
		return
	}
	writeJSON(w, http.StatusOK, buckets)
}

// writeInternalError логирует внутреннюю ошибку (например, сбой БД) и отдаёт
// клиенту 500 без деталей — детали остаются только в логах сервера.
func (a *API) writeInternalError(w http.ResponseWriter, action string, err error) {
	a.logger.Error(action, "error", err)
	writeError(w, http.StatusInternalServerError, "internal error")
}
