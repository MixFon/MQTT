#!/bin/sh
# Перенос состояния со старого VPS на новый (см. deploy/VPS_MIGRATION.md).
# Переносит:
#   1. сертификаты и файл паролей Mosquitto — чтобы ESP32 не пришлось
#      перепрошивать (ca.crt остаётся тот же, меняется только IP за DNS)
#   2. данные sensor_readings через COPY (не pg_dump/pg_restore целиком —
#      pg_dump в custom-формате тащит служебные таблицы расширения
#      _timescaledb_catalog.*, их формат привязан к точной версии TimescaleDB
#      и не восстанавливается, если версии на серверах отличаются; схему же
#      создаёт сам iot-backend через свои миграции, см. ensure_schema ниже)
#
# Использование:
#   ./deploy/migrate.sh user@old-vps-host user@new-vps-host [old-port] [new-port]
# Хосты и порты можно не передавать аргументами, а задать через переменные
# окружения OLD_TARGET/NEW_TARGET и OLD_PORT/NEW_PORT. Порты по умолчанию — 22.
#
# Предполагается, что на новом сервере уже выполнены разделы 2 и 3 (кроме
# генерации сертификатов) из VPS_DEPLOY.md: установлены PostgreSQL+TimescaleDB,
# создана роль/база `iot`, установлен (но не настроен) Mosquitto.
set -eu

OLD_TARGET="${1:-${OLD_TARGET:-}}"
NEW_TARGET="${2:-${NEW_TARGET:-}}"
OLD_PORT="${3:-${OLD_PORT:-22}}"
NEW_PORT="${4:-${NEW_PORT:-22}}"

if [ -z "$OLD_TARGET" ] || [ -z "$NEW_TARGET" ]; then
	echo "Usage: $0 user@old-vps-host user@new-vps-host [old-port] [new-port]" >&2
	echo "  (или задайте OLD_TARGET/NEW_TARGET, OLD_PORT/NEW_PORT)" >&2
	exit 1
fi

# Локальная временная директория — буфер для файлов между старым и новым
# сервером, чтобы не зависеть от того, видны ли VPS друг другу напрямую
# (scp -3 требует прямой связности между хостами, её может не быть).
WORKDIR="$(mktemp -d)"
cleanup_local() {
	rm -rf "$WORKDIR"
}
trap cleanup_local EXIT

# Забирает сертификаты и passwd-файл Mosquitto со старого сервера архивом
# (нужен sudo — server.key лежит с правами 640 root:mosquitto).
fetch_mosquitto_certs() {
	echo "==> Забираю сертификаты Mosquitto с $OLD_TARGET"
	ssh -p "$OLD_PORT" "$OLD_TARGET" \
		'sudo tar czf /tmp/mosquitto-certs.tar.gz -C /etc/mosquitto certs passwd'
	scp -P "$OLD_PORT" "$OLD_TARGET:/tmp/mosquitto-certs.tar.gz" "$WORKDIR/"
	ssh -p "$OLD_PORT" "$OLD_TARGET" 'rm -f /tmp/mosquitto-certs.tar.gz'
}

# Раскладывает сертификаты на новом сервере и выставляет права — те же,
# что в разделе 3 VPS_DEPLOY.md. Права проставляем заново, а не полагаемся
# на права внутри архива: gid группы mosquitto может отличаться между
# серверами (создаётся при установке пакета, не гарантированно одинаковый).
deploy_mosquitto_certs() {
	echo "==> Разворачиваю сертификаты Mosquitto на $NEW_TARGET"
	scp -P "$NEW_PORT" "$WORKDIR/mosquitto-certs.tar.gz" "$NEW_TARGET:/tmp/"
	ssh -p "$NEW_PORT" "$NEW_TARGET" '
		set -eu
		sudo systemctl stop mosquitto 2>/dev/null || true
		sudo tar xzf /tmp/mosquitto-certs.tar.gz -C /etc/mosquitto
		rm -f /tmp/mosquitto-certs.tar.gz

		sudo chown -R root:mosquitto /etc/mosquitto/certs
		sudo chmod 755 /etc/mosquitto/certs
		sudo chmod 640 /etc/mosquitto/certs/server.key
		sudo chmod 644 /etc/mosquitto/certs/server.crt
		sudo chmod 600 /etc/mosquitto/certs/ca.key
		sudo chmod 644 /etc/mosquitto/certs/ca.crt

		sudo chown root:mosquitto /etc/mosquitto/passwd
		sudo chmod 640 /etc/mosquitto/passwd

		if [ -f /etc/mosquitto/conf.d/iot.conf ]; then
			sudo systemctl enable --now mosquitto
		fi
	'
	if ssh -p "$NEW_PORT" "$NEW_TARGET" '[ -f /etc/mosquitto/conf.d/iot.conf ]'; then
		echo "    mosquitto перезапущен с новыми сертификатами."
	else
		echo "    /etc/mosquitto/conf.d/iot.conf ещё не создан (раздел 3 VPS_DEPLOY.md) —"
		echo "    mosquitto остановлен, создайте конфиг и sudo systemctl enable --now mosquitto."
	fi
}

# Снимает данные sensor_readings со старого сервера от имени postgres (не от
# iot — так не нужен пароль приложения на этом шаге) через \copy TO STDOUT —
# только строки таблицы, без служебных таблиц расширения TimescaleDB.
dump_database() {
	echo "==> Забираю данные sensor_readings с $OLD_TARGET"
	ssh -p "$OLD_PORT" "$OLD_TARGET" '
		set -eu
		sudo -u postgres psql -d iot -v ON_ERROR_STOP=1 -c \
			"\copy (SELECT time, room, metric, value FROM sensor_readings ORDER BY time) TO STDOUT WITH CSV"
	' > "$WORKDIR/sensor_readings.csv"
}

# Убеждается, что на новом сервере уже есть таблица sensor_readings — её
# должен создать сам iot-backend через встроенный раннер миграций
# (internal/migrate), а не этот скрипт: в проекте схема БД создаётся только
# миграциями, см. CLAUDE.md. Если сервис на новом сервере ещё ни разу не
# стартовал, запускает его на пару секунд ради миграций и останавливает —
# запускать надолго рано, база ещё пустая до restore_database.
ensure_schema() {
	echo "==> Проверяю схему БД на $NEW_TARGET"
	ssh -p "$NEW_PORT" "$NEW_TARGET" '
		set -eu
		exists="$(sudo -u postgres psql -d iot -tAc "SELECT to_regclass(\$\$public.sensor_readings\$\$)")"
		if [ -z "$exists" ]; then
			echo "    таблицы ещё нет — запускаю iot-backend на пару секунд для применения миграций"
			sudo systemctl start iot-backend
			sleep 2
			sudo systemctl stop iot-backend
		fi
	'
}

# Загружает данные в новую базу через \copy FROM STDIN — только сами строки,
# схему (таблицу, hypertable) уже создал iot-backend в ensure_schema. Таблица
# должна быть пустой (свежесозданной миграцией), TRUNCATE — для того, чтобы
# скрипт можно было безопасно перезапускать заново после сбоя на этом шаге.
restore_database() {
	echo "==> Загружаю данные sensor_readings на $NEW_TARGET"
	ssh -p "$NEW_PORT" "$NEW_TARGET" 'sudo systemctl stop iot-backend 2>/dev/null || true'
	cat "$WORKDIR/sensor_readings.csv" | ssh -p "$NEW_PORT" "$NEW_TARGET" '
		set -eu
		sudo -u postgres psql -d iot -v ON_ERROR_STOP=1 -c "TRUNCATE sensor_readings"
		sudo -u postgres psql -d iot -v ON_ERROR_STOP=1 -c \
			"\copy sensor_readings (time, room, metric, value) FROM STDIN WITH CSV"
	'
}

fetch_mosquitto_certs
deploy_mosquitto_certs
dump_database
ensure_schema
restore_database

echo "==> Готово. Дальше вручную (см. deploy/VPS_MIGRATION.md, раздел 3):"
echo "    - проверить конфиг /etc/mosquitto/conf.d/iot.conf и запустить mosquitto"
echo "    - sudo systemctl start iot-backend"
echo "    - проверить mosquitto_sub и curl (см. 'Быстрая проверка' в VPS_NOTES.md)"
