package api

import (
	"encoding/json"
	"net/http"
)

// errorResponse — единый формат JSON-ошибок REST API: {"error": "..."}.
type errorResponse struct {
	Error string `json:"error"`
}

// writeJSON сериализует v в JSON и отправляет клиенту с заданным HTTP-статусом.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// writeError отправляет клиенту JSON-ошибку в едином формате errorResponse.
func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, errorResponse{Error: message})
}
