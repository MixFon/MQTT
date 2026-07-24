# Деплой на VPS (Этап 6)

Инструкция по применению артефактов из `deploy/systemd/` и `deploy/Caddyfile`
на боевом сервере. Выполняется вручную, Claude доступа к VPS не имеет.

Целевой сервер: Ubuntu 22.04, amd64, 1GB RAM.
Домен: `mrmixfon.ru`.

## 1. DNS

A-запись `mrmixfon.ru` → IP этого VPS настроена. Без нее Caddy не смог бы
выпустить TLS-сертификат через Let's Encrypt.

## 2. PostgreSQL + TimescaleDB

`docker-compose.yml` в репозитории — только для локальной разработки (см.
CLAUDE.md), на VPS БД ставится нативно через apt: меньше накладных расходов
на 1GB RAM и не нужен отдельный докер-демон ради одного контейнера.

```bash
sudo apt install -y gnupg postgresql-common apt-transport-https lsb-release curl
sudo /usr/share/postgresql-common/pgdg/apt.postgresql.org.sh -y
curl -s https://packagecloud.io/install/repositories/timescale/timescaledb/script.deb.sh | sudo bash
sudo apt install -y timescaledb-2-postgresql-16 postgresql-client-16

sudo timescaledb-tune --quiet --yes   # подстраивает postgresql.conf под объём RAM
sudo systemctl restart postgresql
```

Создать роль и базу для приложения:

```bash
sudo -u postgres psql -c "CREATE USER iot WITH PASSWORD '<сгенерировать пароль>';"
sudo -u postgres psql -c "CREATE DATABASE iot OWNER iot;"
sudo -u postgres psql -d iot -c "CREATE EXTENSION IF NOT EXISTS timescaledb;"
```

Расширение создаётся здесь от имени `postgres`, потому что `CREATE EXTENSION`
обычно требует прав суперпользователя, а роль `iot` их не имеет. Миграция
`migrations/0001_enable_timescaledb.sql` использует `IF NOT EXISTS` и при
старте приложения просто не найдёт работы — это ожидаемо, не ошибка.

PostgreSQL на Ubuntu по умолчанию слушает только `localhost` — порт `5432`
в фаерволе наружу открывать не нужно, приложение работает на этой же машине.
`DATABASE_URL` для env-файла: `postgres://iot:<пароль>@localhost:5432/iot?sslmode=disable`.

## 3. Mosquitto

```bash
sudo apt install -y mosquitto mosquitto-clients
sudo systemctl stop mosquitto   # донастроим, прежде чем запускать
```

Сертификаты и файл паролей — тот же принцип, что в
`deploy/mosquitto/gen-dev-certs.sh` для локальной разработки, но без Docker
и с настоящим паролем вместо `changeme`:

```bash
sudo mkdir -p /etc/mosquitto/certs
cd /etc/mosquitto/certs

sudo openssl req -x509 -nodes -newkey rsa:2048 -days 3650 \
  -keyout ca.key -out ca.crt -subj "/CN=mqtt-ca"
sudo openssl req -nodes -newkey rsa:2048 \
  -keyout server.key -out server.csr -subj "/CN=mrmixfon.ru" \
  -addext "subjectAltName=DNS:mrmixfon.ru"
# -copy_extensions copy переносит subjectAltName из CSR в сертификат: Go 1.15+
# при проверке TLS смотрит только на SAN, CN он больше не читает — без этой
# опции клиент (Go-бэкенд, mosquitto_pub и т.д.) откажется доверять сертификату
# даже с правильным CA.
sudo openssl x509 -req -in server.csr -CA ca.crt -CAkey ca.key \
  -CAcreateserial -out server.crt -days 3650 -copy_extensions copy
sudo rm server.csr

sudo mosquitto_passwd -c /etc/mosquitto/passwd iot   # ввести реальный пароль
```

Mosquitto по умолчанию сбрасывает привилегии с root на системного пользователя
`mosquitto` сразу после старта (даже если директива `user` не указана явно в
конфиге — это встроенное значение по умолчанию), и уже от его имени читает
TLS-сертификаты и `password_file`. Файлы выше созданы через `sudo` и принадлежат
`root` с правами `600`/`644` — без явной раздачи прав группе `mosquitto` демон не
сможет их открыть и упадёт с `Unable to load server key file ... Permission denied`
в `/var/log/mosquitto/mosquitto.log` (в лог systemd эта ошибка не попадает, только
в файл — см. `log_dest file` в `/etc/mosquitto/mosquitto.conf`). Поэтому сразу
после генерации сертификатов и passwd-файла:

```bash
sudo chown -R root:mosquitto /etc/mosquitto/certs
sudo chmod 755 /etc/mosquitto/certs

sudo chmod 640 /etc/mosquitto/certs/server.key
sudo chmod 644 /etc/mosquitto/certs/server.crt

sudo chown root:mosquitto /etc/mosquitto/passwd
sudo chmod 640 /etc/mosquitto/passwd
```

`ca.key` (нужен только для подписи будущих серверных сертификатов, демону в
рантайме не требуется) можно оставить `600 root:root`. `ca.crt` — публичный
сертификат, не секрет — оставляем ему дефолтные права `644 root:root` из
`openssl`: его должен уметь прочитать не только `mosquitto`, но и сам
Go-бэкенд (пользователь `iot-backend`, см. раздел 5), которому нужно доверять
этому CA при подключении к брокеру — см. `MQTT_CA_CERT_FILE` в разделе 6.

`ca.crt` затем нужно зашить в прошивку ESP32 — брокер использует
самоподписанный сертификат, устройство должно доверять именно этому CA.
Публичный CA (Let's Encrypt и т.п.) здесь не нужен: устройства пинят
конкретный сертификат, а не проверяют общий список доверенных корней.

Конфиг (по образцу `deploy/mosquitto/mosquitto.conf`, пути адаптированы
под нативную установку):

```bash
sudo tee /etc/mosquitto/conf.d/iot.conf >/dev/null <<'EOF'
listener 8883
protocol mqtt

allow_anonymous false
password_file /etc/mosquitto/passwd

cafile /etc/mosquitto/certs/ca.crt
certfile /etc/mosquitto/certs/server.crt
keyfile /etc/mosquitto/certs/server.key
EOF

sudo systemctl enable --now mosquitto
sudo systemctl status mosquitto
```

`MQTT_BROKER_URL` для env-файла: `tls://mrmixfon.ru:8883` — обязательно домен,
а не `localhost`: сертификат брокера выпущен на CN/SAN `mrmixfon.ru`, а Go 1.15+
проверяет SAN сертификата против того хоста, к которому реально подключается
клиент. `MQTT_CA_CERT_FILE` — `/etc/mosquitto/certs/ca.crt`, иначе Go-бэкенд
не будет доверять самоподписанному сертификату. Оба уже подставлены как
дефолтные значения в `deploy/systemd/iot-backend.env.example` — при
редактировании `/etc/iot-backend.env` в разделе 6 их менять не нужно. Порт
`8883/tcp` должен быть открыт в фаерволе снаружи — к нему подключаются
ESP32 напрямую из Wi-Fi сети, не через Caddy (`sudo ufw allow 8883/tcp`).

## 4. Собрать и залить бинарник и файлы деплоя

Бинарник собирается локально (кросс-компиляция под Linux), не на самой VPS —
на 1GB RAM сборка может быть медленной/упасть по памяти. Файлы из `deploy/`
(env-шаблон, systemd unit, Caddyfile) на VPS сами по себе не появляются —
их тоже нужно скопировать с локальной машины, иначе `cp` в шагах 6–8 ниже
будет ссылаться на несуществующий путь.

```bash
GOOS=linux GOARCH=amd64 go build -o server ./cmd/server
scp server user@vps-host:/tmp/server
scp -r deploy/systemd deploy/Caddyfile user@vps-host:/tmp/deploy-artifacts/
```

На VPS:

```bash
sudo mkdir -p /opt/iot-backend
sudo mv /tmp/server /opt/iot-backend/server
sudo chmod +x /opt/iot-backend/server
```

## 5. Системный пользователь

```bash
sudo useradd -r -s /usr/sbin/nologin iot-backend
sudo chown -R iot-backend:iot-backend /opt/iot-backend
```

## 6. Секреты (env-файл)

```bash
sudo cp /tmp/deploy-artifacts/systemd/iot-backend.env.example /etc/iot-backend.env
sudo vim /etc/iot-backend.env   # заполнить реальные MQTT_PASSWORD, DATABASE_URL и т.д.
sudo chown root:iot-backend /etc/iot-backend.env
sudo chmod 640 /etc/iot-backend.env
```

## 7. systemd unit

```bash
sudo cp /tmp/deploy-artifacts/systemd/iot-backend.service /etc/systemd/system/
sudo systemctl daemon-reload
sudo systemctl enable --now iot-backend
sudo systemctl status iot-backend
journalctl -u iot-backend -f
```

## 8. Caddy (reverse proxy)

```bash
sudo apt install -y caddy   # если ещё не установлен
sudo cp /tmp/deploy-artifacts/Caddyfile /etc/caddy/Caddyfile
sudo systemctl reload caddy
```

Проверить: `https://mrmixfon.ru/api/rooms` должен отвечать тем же, что
и `http://127.0.0.1:8080/api/rooms` на самом сервере.

## Обновление бинарника после изменений в коде

```bash
GOOS=linux GOARCH=amd64 go build -o server ./cmd/server
scp server user@vps-host:/tmp/server
ssh user@vps-host 'sudo systemctl stop iot-backend && \
  sudo mv /tmp/server /opt/iot-backend/server && \
  sudo systemctl start iot-backend'
```
