# Шпаргалка по сервису на VPS (mrmixfon.ru)

Короткая справка «что где лежит и как этим управлять» — для будущего себя.
Подробности первоначальной установки — в `VPS_DEPLOY.md`, эта заметка про
повседневную эксплуатацию уже развёрнутого сервиса.

Сервер: Ubuntu 22.04, amd64, 1GB RAM. Домен `mrmixfon.ru`.

Сентябрь 2026: сервис переехал на новый VPS — старый IP заблокировали по
ТСПУ. Домен и все данные (БД, сертификаты Mosquitto) перенесены на новый
сервер, ESP32 переподключились сами, без перепрошивки — см.
`deploy/VPS_MIGRATION.md` и `deploy/migrate.sh` для деталей самого переноса.
Новый сервер — тоже Ubuntu 22.04/1GB RAM, у хостинг-провайдера `ruweb.place`.

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
TELEGRAM_BOT_TOKEN=<секрет>
TELEGRAM_CHAT_ID=<id чата/пользователя>
ALERT_CHECK_INTERVAL=1m
ALERT_OFFLINE_AFTER=3m
ALERT_THRESHOLDS=temperature::30,humidity:20:80
```

`ALERT_THRESHOLDS` — список `metric:min:max` через запятую, граница может быть
пустой (не ограничена). В примере выше: `temperature` алертит только выше 30
(нижняя граница не задана), `humidity` — вне диапазона 20–80.

Алерты (offline датчика, выход показания за порог) уходят в Telegram — см.
`internal/alert` и раздел «Этап 7» в `CLAUDE.md`. Если `TELEGRAM_BOT_TOKEN`/
`TELEGRAM_CHAT_ID` пустые, фоновая проверка не запускается и в логах при
старте будет строка `alert checker disabled`.

## Команды управления

### iot-backend (Go-сервис: MQTT-подписчик + REST API)

```bash
sudo systemctl start iot-backend
sudo systemctl stop iot-backend
sudo systemctl restart iot-backend
sudo systemctl status iot-backend
journalctl -u iot-backend -f          # логи в реальном времени
journalctl -u iot-backend -n 200      # последние 200 строк
journalctl -u iot-backend -f | grep -i alert   # только события алертов (offline/пороги)
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

## Диагностика: iot-backend в busy-restart-loop

`iot-backend` не ретраит подключение к MQTT-брокеру внутри себя при старте —
если на момент запуска брокер недоступен (mosquitto потушен, не тот
сертификат и т.п.), процесс сразу завершается с `exit 1`, а `systemd`
перезапускает его заново (`restart counter` в статусе быстро растёт).
Выглядит как загадочный краш-луп, а причина почти всегда простая:

```bash
journalctl -u iot-backend -n 50 --no-pager | grep -i mqtt
# ищем что-то вроде:
# error="connect to mqtt broker: network Error : dial tcp ...:8883: connect: connection refused"
```

Если видите такую строку — проверьте `mosquitto`:
`sudo systemctl status mosquitto`, `tail -f /var/log/mosquitto/mosquitto.log`.
После того как брокер поднят, `iot-backend` подключается на следующей
попытке рестарта сам, вручную перезапускать не обязательно.

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
- **Telegram-алерты** с этого сервера напрямую не работают (хостинг в РФ):
  `api.telegram.org` недоступен на сетевом уровне (`dial tcp ...:443: i/o
  timeout`, IPv6-маршрут вообще недостижим, `ufw` тут ни при чём — проверено
  `curl` напрямую). Решено с 2026-09-10: запросы идут через `telegram-proxy`
  на сервере пользователя с OpenVPN (он не в РФ) — `TELEGRAM_PROXY_URL` в
  `/etc/iot-backend.env`, само подключение — OpenVPN-клиент на этом VPS
  (split-tunnel, без `redirect-gateway`). Уведомления реально доставляются,
  проверено на срабатывании порога. Подробная инструкция и troubleshooting —
  `deploy/TELEGRAM_PROXY_DEPLOY.md`.

  Если ошибки `send telegram request: ... i/o timeout` снова появились в
  `journalctl -u iot-backend` — вероятные причины (по опыту первого деплоя):
  1. На VPS не поднят/упал OpenVPN-клиент — `ip addr show tun0` должен
     показывать адрес из VPN-подсети, `ping` до адреса `telegram-proxy` должен
     отвечать.
  2. `telegram-proxy` не работает на стороне OpenVPN-сервера — там же
     `systemctl status telegram-proxy`.
  3. Задеплоен бинарник `iot-backend` без поддержки `TELEGRAM_PROXY_URL` —
     проверить дату сборки/передеплоить `./deploy/deploy.sh`, убедиться, что
     переменная реально есть в `/etc/iot-backend.env` (не в аналогичном файле
     на другом сервере — важно не перепутать хосты при `./deploy/deploy.sh`
     и `./deploy/deploy-telegram-proxy.sh`, у них разные цели).
