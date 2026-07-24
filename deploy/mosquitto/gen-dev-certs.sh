#!/bin/sh
# Generates a self-signed CA + server certificate for local Mosquitto TLS,
# and creates the password file. For local development only.
set -eu

cd "$(dirname "$0")"
mkdir -p certs

openssl req -x509 -nodes -newkey rsa:2048 -days 3650 \
  -keyout certs/ca.key -out certs/ca.crt \
  -subj "/CN=mqtt-dev-ca"

openssl req -nodes -newkey rsa:2048 \
  -keyout certs/server.key -out certs/server.csr \
  -subj "/CN=localhost" -addext "subjectAltName=DNS:localhost"

# -copy_extensions copy переносит subjectAltName из CSR в итоговый сертификат:
# Go 1.15+ проверяет только SAN, CN он больше не читает.
openssl x509 -req -in certs/server.csr -CA certs/ca.crt -CAkey certs/ca.key \
  -CAcreateserial -out certs/server.crt -days 3650 -copy_extensions copy

rm certs/server.csr

docker run --rm -v "$(pwd):/work" -w /work eclipse-mosquitto:2 \
  mosquitto_passwd -b -c passwd iot changeme

echo "Generated certs/ and passwd. Default MQTT login: iot / changeme"
