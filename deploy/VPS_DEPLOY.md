# Деплой на VPS (Этап 6)

Инструкция по применению артефактов из `deploy/systemd/` и `deploy/Caddyfile`
на боевом сервере. Выполняется вручную, Claude доступа к VPS не имеет.

Целевой сервер: Ubuntu 22.04, amd64, 1GB RAM.
Домен: `iot.mrmixfon.ru` (поддомен — `mrmixfon.ru` настроен на другую машину).

## 1. DNS

Добавить A-запись `iot.mrmixfon.ru` → IP этого VPS. Без неё Caddy не сможет
выпустить TLS-сертификат через Let's Encrypt.

## 2. Собрать и залить бинарник

Собирается локально (кросс-компиляция под Linux), не на самой VPS —
на 1GB RAM сборка может быть медленной/упасть по памяти.

```bash
GOOS=linux GOARCH=amd64 go build -o server ./cmd/server
scp server user@vps-host:/tmp/server
```

На VPS:

```bash
sudo mkdir -p /opt/iot-backend
sudo mv /tmp/server /opt/iot-backend/server
sudo chmod +x /opt/iot-backend/server
```

## 3. Системный пользователь

```bash
sudo useradd -r -s /usr/sbin/nologin iot-backend
sudo chown -R iot-backend:iot-backend /opt/iot-backend
```

## 4. Секреты (env-файл)

```bash
sudo cp deploy/systemd/iot-backend.env.example /etc/iot-backend.env
sudo nano /etc/iot-backend.env   # заполнить реальные MQTT_PASSWORD, DATABASE_URL и т.д.
sudo chown root:iot-backend /etc/iot-backend.env
sudo chmod 640 /etc/iot-backend.env
```

## 5. systemd unit

```bash
sudo cp deploy/systemd/iot-backend.service /etc/systemd/system/
sudo systemctl daemon-reload
sudo systemctl enable --now iot-backend
sudo systemctl status iot-backend
journalctl -u iot-backend -f
```

## 6. Caddy (reverse proxy)

```bash
sudo apt install -y caddy   # если ещё не установлен
sudo cp deploy/Caddyfile /etc/caddy/Caddyfile
sudo systemctl reload caddy
```

Проверить: `https://iot.mrmixfon.ru/api/rooms` должен отвечать тем же, что
и `http://127.0.0.1:8080/api/rooms` на самом сервере.

## Обновление бинарника после изменений в коде

```bash
GOOS=linux GOARCH=amd64 go build -o server ./cmd/server
scp server user@vps-host:/tmp/server
ssh user@vps-host 'sudo systemctl stop iot-backend && \
  sudo mv /tmp/server /opt/iot-backend/server && \
  sudo systemctl start iot-backend'
```
