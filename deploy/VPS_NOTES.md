# Шпаргалка по сервису на VPS (mrmixfon.ru)

Короткая справка «что где лежит и как этим управлять» — для будущего себя.
Подробности первоначальной установки — в `VPS_DEPLOY.md`, эта заметка про
повседневную эксплуатацию уже развёрнутого сервиса.

Сервер: Ubuntu 22.04, amd64, 1GB RAM. Домен `mrmixfon.ru`.

## Что установлено

| Компонент             | Как установлен      | Роль |
|------------------------|----------------------|------|
| PostgreSQL 16 + TimescaleDB | нативно через apt | хранилище показаний (`sensor_readings`) |
| Mosquitto               | нативно через apt   | MQTT-брокер, TLS на 8883, принимает данные от ESP32 |
| iot-backend              | свой Go-бинарник, systemd unit | MQTT-подписчик + REST API, слушает `127.0.0.1:8080` |
| Caddy                   | нативно через apt    | reverse proxy, TLS (Let's Encrypt) для `mrmixfon.ru` → `127.0.0.1:8080` |

Docker не используется в проде — `docker-compose.yml` в репозитории только для
локальной разработки.

## Где лежат конфиги и файлы

| Что | Путь |
|-----|------|
| Бинарник и рабочая директория iot-backend | `/opt/iot-backend/server` |
| systemd unit iot-backend | `/etc/systemd/system/iot-backend.service` |
| Секреты iot-backend (env) | `/etc/iot-backend.env` (владелец `root:iot-backend`, права `640`) |
| Системный пользователь | `iot-backend` (без shell, `nologin`) |
| Mosquitto — основной конфиг | `/etc/mosquitto/mosquitto.conf` |
| Mosquitto — конфиг слушателя/TLS/паролей | `/etc/mosquitto/conf.d/iot.conf` |
| Mosquitto — сертификаты (CA, server) | `/etc/mosquitto/certs/` (`ca.crt`, `ca.key`, `server.crt`, `server.key`) |
| Mosquitto — файл паролей | `/etc/mosquitto/passwd` |
| Mosquitto — лог | `/var/log/mosquitto/mosquitto.log` (ошибки TLS/прав сюда, не в journalctl) |
| Caddy — конфиг | `/etc/caddy/Caddyfile` |
| PostgreSQL — конфиг (подстроен `timescaledb-tune`) | `/etc/postgresql/16/main/postgresql.conf` |
| База/роль приложения | БД `iot`, роль `iot`, слушает только `localhost:5432` |

## Переменные окружения iot-backend (`/etc/iot-backend.env`)

```
MQTT_BROKER_URL=tls://mrmixfon.ru:8883
MQTT_USERNAME=iot
MQTT_PASSWORD=<секрет>
MQTT_CA_CERT_FILE=/etc/mosquitto/certs/ca.crt
DATABASE_URL=postgres://iot:<секрет>@localhost:5432/iot?sslmode=disable
HTTP_ADDR=127.0.0.1:8080
```

## Команды управления

### iot-backend (Go-сервис: MQTT-подписчик + REST API)

```bash
sudo systemctl start iot-backend
sudo systemctl stop iot-backend
sudo systemctl restart iot-backend
sudo systemctl status iot-backend
journalctl -u iot-backend -f          # логи в реальном времени
journalctl -u iot-backend -n 200      # последние 200 строк
```

### Mosquitto (MQTT-брокер)

```bash
sudo systemctl start mosquitto
sudo systemctl stop mosquitto
sudo systemctl restart mosquitto
sudo systemctl status mosquitto
tail -f /var/log/mosquitto/mosquitto.log
```

### PostgreSQL / TimescaleDB

```bash
sudo systemctl start postgresql
sudo systemctl stop postgresql
sudo systemctl restart postgresql
sudo systemctl status postgresql
sudo -u postgres psql -d iot          # зайти в базу под суперпользователем
psql "postgres://iot:<пароль>@localhost:5432/iot"   # зайти под ролью приложения
```

### Caddy (reverse proxy)

```bash
sudo systemctl start caddy
sudo systemctl stop caddy
sudo systemctl restart caddy
sudo systemctl reload caddy           # применить Caddyfile без обрыва соединений
sudo systemctl status caddy
journalctl -u caddy -f
```

## Быстрая проверка, что всё живо

```bash
sudo systemctl status postgresql mosquitto iot-backend caddy --no-pager
curl -s https://mrmixfon.ru/api/rooms
mosquitto_sub -h localhost -p 8883 --cafile /etc/mosquitto/certs/ca.crt \
  -u iot -P <пароль> -t 'home/#' -v      # проверить, что показания идут
```

## Обновление бинарника после изменений в коде

Собирается локально (кросс-компиляция), не на самой VPS — см. `VPS_DEPLOY.md`,
раздел «Обновление бинарника».

```bash
GOOS=linux GOARCH=amd64 go build -o server ./cmd/server
scp server user@vps-host:/tmp/server
ssh user@vps-host 'sudo systemctl stop iot-backend && \
  sudo mv /tmp/server /opt/iot-backend/server && \
  sudo systemctl start iot-backend'
```

## На заметку

- Порт `5432` (Postgres) наружу закрыт — приложение и БД на одной машине.
- Порт `8883` (MQTT) открыт наружу для ESP32 (`sudo ufw allow 8883/tcp`).
- Порты `80`/`443` открыты для Caddy (HTTP→HTTPS редирект и TLS).
- Grafana в этот стек пока не входит — разворачивается отдельно, в Caddy не
  проксирована.
