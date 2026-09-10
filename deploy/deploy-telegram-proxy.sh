#!/bin/sh
# Сборка telegram-proxy, копирование на сервер с OpenVPN и разворачивание там
# (см. deploy/TELEGRAM_PROXY_DEPLOY.md — первоначальная настройка сервера,
# .ovpn-клиент и заполнение секретов туда не входят, это делается один раз
# руками). Разворачивается НЕ на iot-backend VPS. Использование:
#   ./deploy/deploy-telegram-proxy.sh user@openvpn-host [ssh-port] [goarch]
# Хост/порт/архитектуру можно не передавать аргументами, а задать через
# переменные окружения PROXY_TARGET/PROXY_PORT/PROXY_GOARCH — удобно, если
# деплоите с одной машины постоянно. Порт по умолчанию — 22, архитектура —
# amd64 (поменяйте на arm64/arm, если сервер OpenVPN не amd64).
set -eu

TARGET="${1:-${PROXY_TARGET:-}}"
PORT="${2:-${PROXY_PORT:-22}}"
GOARCH="${3:-${PROXY_GOARCH:-amd64}}"
if [ -z "$TARGET" ]; then
	echo "Usage: $0 user@openvpn-host [ssh-port] [goarch]  (или задайте PROXY_TARGET/PROXY_PORT/PROXY_GOARCH)" >&2
	exit 1
fi

# Переходим в корень репозитория, чтобы go build и пути ниже не зависели
# от того, из какой директории запущен скрипт.
cd "$(dirname "$0")/.."

# Удаляет локально собранный бинарник — он нужен только чтобы передать его
# на сервер, хранить его в рабочей директории незачем. Навешено через trap,
# а не вызовом в конце скрипта, чтобы бинарник удалился и при ошибке на любом
# шаге.
cleanup_local() {
	rm -f telegram-proxy
}
trap cleanup_local EXIT

# Собирает бинарник под Linux локально — кросс-компиляция, чтобы не тащить
# toolchain на целевой сервер. GOARCH настраивается: сервер с OpenVPN не
# обязательно amd64 (например, домашний сервер на ARM).
build_binary() {
	echo "==> Собираю бинарник (GOOS=linux GOARCH=$GOARCH)"
	GOOS=linux GOARCH="$GOARCH" go build -o telegram-proxy ./cmd/telegram-proxy
}

# Копирует собранный бинарник и systemd unit во временную директорию на
# сервере.
upload_artifacts() {
	echo "==> Копирую бинарник и systemd unit на $TARGET (порт $PORT)"
	scp -P "$PORT" telegram-proxy "$TARGET:/tmp/telegram-proxy"
	scp -P "$PORT" deploy/telegram-proxy/telegram-proxy.service "$TARGET:/tmp/telegram-proxy.service"
}

# Создаёт системного пользователя и рабочую директорию при первом запуске
# (идемпотентно), подменяет бинарник и unit, перезапускает сервис.
# /etc/telegram-proxy.env не трогаем — секреты (PROXY_TOKEN) заполняются
# вручную один раз, см. deploy/TELEGRAM_PROXY_DEPLOY.md. Если файла ещё нет —
# останавливаем деплой с понятной подсказкой, а не оставляем сервис в
# неработающем состоянии молча.
deploy_remote() {
	echo "==> Разворачиваю на $TARGET (порт $PORT)"
	ssh -p "$PORT" "$TARGET" '
		set -eu
		id -u telegram-proxy >/dev/null 2>&1 || sudo useradd -r -s /usr/sbin/nologin telegram-proxy
		sudo mkdir -p /opt/telegram-proxy
		sudo chown telegram-proxy:telegram-proxy /opt/telegram-proxy

		if [ ! -f /etc/telegram-proxy.env ]; then
			echo "!!! /etc/telegram-proxy.env не найден на сервере." >&2
			echo "!!! Скопируйте deploy/telegram-proxy/telegram-proxy.env.example, заполните PROXY_TOKEN и повторите деплой." >&2
			exit 1
		fi

		sudo systemctl stop telegram-proxy 2>/dev/null || true
		sudo mv /tmp/telegram-proxy /opt/telegram-proxy/telegram-proxy
		sudo chmod +x /opt/telegram-proxy/telegram-proxy
		sudo mv /tmp/telegram-proxy.service /etc/systemd/system/telegram-proxy.service
		sudo systemctl daemon-reload
		sudo systemctl enable --now telegram-proxy
		sudo systemctl status telegram-proxy --no-pager
	'
}

build_binary
upload_artifacts
deploy_remote

echo "==> Готово"
