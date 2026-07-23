// Package api — HTTP-обработчики REST API для чтения показаний датчиков.
// Ничего не знает про MQTT: только читает данные через internal/storage.
package api

import (
	"log/slog"
	"net/http"

	"github.com/MixFon/MQTT/internal/storage"
)

// API держит зависимости HTTP-обработчиков: доступ к слою хранения и логгер.
type API struct {
	store  *storage.Storage
	logger *slog.Logger
}

// New создаёт API поверх уже готового Storage.
func New(store *storage.Storage, logger *slog.Logger) *API {
	return &API{store: store, logger: logger}
}

// Register добавляет маршруты REST API в общий mux (общий — чтобы в том же
// mux можно было зарегистрировать раздачу статики фронтенда рядом, в main).
func (a *API) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/rooms", a.handleRooms)
	mux.HandleFunc("GET /api/metrics", a.handleMetrics)
	mux.HandleFunc("GET /api/latest", a.handleLatest)
	mux.HandleFunc("GET /api/readings", a.handleReadings)
}
