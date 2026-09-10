# Деплой telegram-proxy (обход блокировки Telegram в РФ)

Инструкция по развёртыванию `cmd/telegram-proxy` на сервере с OpenVPN —
**не** на iot-backend VPS (см. CLAUDE.md, «Этап 7» и `deploy/VPS_NOTES.md`,
раздел «На заметку»). Выполняется вручную на сервере с OpenVPN — Claude
доступа туда не имеет. Повторяемая часть (сборка + заливка бинарника) —
через `deploy/deploy-telegram-proxy.sh`, см. низ этого файла.

Предпосылка: у вас уже есть OpenVPN-сервер вне РФ, к которому
подключается или может подключиться iot-backend VPS как клиент.

## 1. Сетевая схема

```
iot-backend VPS (РФ, Telegram заблокирован)
     |  OpenVPN-клиент, split-tunnel
     v
tun-интерфейс OpenVPN-сервера (например 10.8.0.1)
     |
     v
telegram-proxy слушает 10.8.0.1:3128, CONNECT только на api.telegram.org:443
     |
     v
api.telegram.org (доступен с этого сервера)
```

Важно: `PROXY_LISTEN_ADDR` должен быть адресом **tun-интерфейса**, не
публичным адресом сервера — иначе прокси окажется открыт всему интернету.
Узнать реальный адрес: `ip addr show tun0` на сервере с OpenVPN.

## 2. OpenVPN-клиент на iot-backend VPS

Если iot-backend VPS ещё не подключён к вашему OpenVPN как клиент:

1. Сгенерировать `.ovpn`-профиль для этого VPS обычным способом для вашего
   OpenVPN-сервера (свой удостоверяющий центр/easy-rsa и т.д. — вне рамок
   этого проекта).
2. В профиле **не** должно быть `redirect-gateway` — нужен split-tunnel,
   через VPN должен ходить только трафик к `10.8.0.x` (сеть OpenVPN), весь
   остальной трафик VPS (MQTT от ESP32, HTTPS от Caddy) должен идти как
   раньше, напрямую. Иначе весь трафик сервера пойдёт через VPN, что не
   нужно и может уронить доступность MQTT/сайта.
3. На iot-backend VPS: `sudo apt install -y openvpn`, положить `.ovpn` в
   `/etc/openvpn/client/telegram.conf`, `sudo systemctl enable --now
   openvpn-client@telegram`.
4. Проверить: `ip addr show tun0` на iot-backend VPS должен показать адрес
   в сети `10.8.0.0/24` (или той, что использует ваш OpenVPN), и
   `ping 10.8.0.1` должен отвечать.

## 3. Первый деплой telegram-proxy на сервере с OpenVPN

Сборка и заливка бинарника — автоматизированы, см. раздел 5. Но перед первым
запуском нужно руками завести секреты (скрипт их не трогает, чтобы не
затирать):

```bash
# на локальной машине — скопировать шаблон на сервер
scp deploy/telegram-proxy/telegram-proxy.env.example \
    user@openvpn-host:/tmp/telegram-proxy.env
```

На сервере с OpenVPN:

```bash
sudo mv /tmp/telegram-proxy.env /etc/telegram-proxy.env
sudo vim /etc/telegram-proxy.env
```

Заполнить:
- `PROXY_LISTEN_ADDR` — реальный адрес tun-интерфейса + порт, например
  `10.8.0.1:3128` (см. `ip addr show tun0`).
- `PROXY_ALLOWED_HOSTS` — оставить `api.telegram.org`, если не нужно
  большего.
- `PROXY_TOKEN` — сгенерировать случайный секрет, например
  `openssl rand -hex 32`. Обязателен: в той же VPN-сети могут быть другие
  клиенты, без токена прокси стал бы для них открытым релеем.

Затем ограничить права (пользователь `telegram-proxy` будет создан
скриптом при первом деплое):

```bash
sudo chown root:telegram-proxy /etc/telegram-proxy.env
sudo chmod 640 /etc/telegram-proxy.env
```

Если порядок нарушен и пользователя `telegram-proxy` ещё нет —
`chown` упадёт с «invalid group»; выполните его после первого запуска
`deploy-telegram-proxy.sh` (шаг 5 ниже создаёт пользователя) либо один раз
`sudo chown root:root /etc/telegram-proxy.env`, а потом перевыполните
`chown` после деплоя.

## 4. Файрвол

Порт из `PROXY_LISTEN_ADDR` (3128) слушается только на tun-интерфейсе —
наружу в интернет открывать не нужно (`ufw` по умолчанию ничего не трогает
на `tun0`, но если на сервере есть общий `ufw deny incoming` — явно
разрешить его только для подсети OpenVPN, не `0.0.0.0/0`).

## 5. Сборка и заливка бинарника (автоматизировано)

```bash
./deploy/deploy-telegram-proxy.sh user@openvpn-host
# нестандартный SSH-порт или архитектура (например ARM):
./deploy/deploy-telegram-proxy.sh user@openvpn-host 2222 arm64
# либо через переменные окружения, если деплоите постоянно с одной машины:
export PROXY_TARGET=user@openvpn-host PROXY_PORT=2222 PROXY_GOARCH=arm64
./deploy/deploy-telegram-proxy.sh
```

Скрипт: собирает бинарник кросс-компиляцией под Linux, копирует его и
`telegram-proxy.service` на сервер, создаёт системного пользователя
`telegram-proxy` (если его ещё нет), проверяет, что `/etc/telegram-proxy.env`
уже создан (см. раздел 3) — если нет, останавливается с подсказкой, а не
запускает сервис без секретов, — и включает/перезапускает сервис через
systemd. Тот же скрипт используется и для последующих обновлений бинарника.

## 6. Проверка

На сервере с OpenVPN:

```bash
sudo systemctl status telegram-proxy --no-pager
journalctl -u telegram-proxy -f
```

В логе при старте должна быть строка `telegram-proxy listening addr=... 
allowed_hosts=[api.telegram.org]`.

С iot-backend VPS (через VPN-туннель) проверить, что прокси отвечает на
`CONNECT`:

```bash
curl -v --proxy http://iot:<PROXY_TOKEN>@10.8.0.1:3128 \
     https://api.telegram.org
```

Ожидается успешный TLS-хендшейк (curl достучится до Telegram через
туннель) — содержимое ответа неважно, важно отсутствие ошибок сети/443.

## 7. Включить прокси в iot-backend

На iot-backend VPS, в `/etc/iot-backend.env` (см. `deploy/VPS_NOTES.md`):

```
TELEGRAM_PROXY_URL=http://iot:<PROXY_TOKEN>@10.8.0.1:3128
```

Логин (`iot` в примере) может быть любым — проверяется только пароль
(`PROXY_TOKEN`), см. `cmd/telegram-proxy/proxy.go` (`authorized`). Адрес и
порт — те же, что в `PROXY_LISTEN_ADDR` на сервере с OpenVPN.

```bash
sudo systemctl restart iot-backend
journalctl -u iot-backend -f | grep -i telegram
```

Ошибки `send telegram request: ... i/o timeout` должны исчезнуть — вместо
них при следующем переходе состояния (offline/порог) должно появиться
успешное уведомление в Telegram.

## 8. Обновление в дальнейшем

Изменения в коде `cmd/telegram-proxy` — обновить тем же скриптом:

```bash
./deploy/deploy-telegram-proxy.sh user@openvpn-host
```

Секреты (`/etc/telegram-proxy.env`) он не трогает, менять их — только
руками через `sudo vim /etc/telegram-proxy.env` + `sudo systemctl restart
telegram-proxy`.
