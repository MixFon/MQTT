#!/bin/sh
# Сборка бинарника, копирование на VPS и разворачивание там (см. раздел
# "Обновление бинарника" в deploy/VPS_DEPLOY.md). Использование:
#   ./deploy/deploy.sh user@vps-host [ssh-port]
# Хост и порт можно не передавать аргументами, а задать через переменные
# окружения VPS_TARGET и VPS_PORT — удобно, если деплоите с одной машины
# постоянно. Порт по умолчанию — 22.
set -eu

TARGET="${1:-${VPS_TARGET:-}}"
PORT="${2:-${VPS_PORT:-22}}"
if [ -z "$TARGET" ]; then
	echo "Usage: $0 user@vps-host [ssh-port]  (или задайте VPS_TARGET/VPS_PORT)" >&2
	exit 1
fi

# Переходим в корень репозитория, чтобы go build и пути ниже не зависели
# от того, из какой директории запущен скрипт.
cd "$(dirname "$0")/.."

# Собирает бинарник под Linux amd64 локально — на VPS с 1GB RAM сборка
# может быть медленной или упасть по памяти (см. VPS_DEPLOY.md, раздел 4).
build_binary() {
	echo "==> Собираю бинарник (GOOS=linux GOARCH=amd64)"
	GOOS=linux GOARCH=amd64 go build -o server ./cmd/server
}

# Копирует собранный бинарник во временную директорию на VPS.
upload_binary() {
	echo "==> Копирую бинарник на $TARGET (порт $PORT)"
	scp -P "$PORT" server "$TARGET:/tmp/server"
}

# Останавливает сервис, подменяет бинарник в /opt/iot-backend и запускает
# сервис заново. Останавливаем перед подменой файла, а не просто перезапускаем
# после — иначе systemd может запустить старый бинарник, если mv не успеет
# выполниться до перезапуска.
deploy_remote() {
	echo "==> Разворачиваю на $TARGET (порт $PORT)"
	ssh -p "$PORT" "$TARGET" '
		set -eu
		sudo systemctl stop iot-backend
		sudo mv /tmp/server /opt/iot-backend/server
		sudo chmod +x /opt/iot-backend/server
		sudo systemctl start iot-backend
		sudo systemctl status iot-backend --no-pager
	'
}

build_binary
upload_binary
deploy_remote

echo "==> Готово"
