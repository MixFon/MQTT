// Package web раздаёт статику собственного веб-дашборда (HTML/CSS/JS),
// вшитую в бинарник через embed.FS. Ничего не знает про MQTT или БД —
// дашборд общается с сервером только через REST API из internal/api.
package web

import (
	"embed"
	"fmt"
	"io/fs"
	"net/http"
)

//go:embed static
var staticFS embed.FS

// Register добавляет в mux раздачу статики дашборда по "/". Регистрируется
// с более общим паттерном, чем "/api/...", поэтому не конфликтует с REST API:
// ServeMux в Go 1.22+ выбирает наиболее специфичный маршрут.
func Register(mux *http.ServeMux) error {
	static, err := fs.Sub(staticFS, "static")
	if err != nil {
		return fmt.Errorf("web: open embedded static dir: %w", err)
	}
	mux.Handle("/", http.FileServer(http.FS(static)))
	return nil
}
