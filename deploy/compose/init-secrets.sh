#!/bin/sh
set -eu

ROOT=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
ENV_FILE="$ROOT/.env"
EXAMPLE="$ROOT/.env.example"
SECRETS="$ROOT/secrets"

[ -f "$ENV_FILE" ] || cp "$EXAMPLE" "$ENV_FILE"
mkdir -p "$SECRETS"
chmod 700 "$SECRETS"

random_hex() {
  od -An -N "${1:-24}" -tx1 /dev/urandom | tr -d ' \n'
}

random_key_base64() {
  head -c 32 /dev/urandom | base64 | tr -d '\n'
}

set_env() {
  key=$1
  value=$2
  if grep -q "^${key}=" "$ENV_FILE"; then
    sed -i "s|^${key}=.*|${key}=${value}|" "$ENV_FILE"
  else
    printf '%s=%s\n' "$key" "$value" >> "$ENV_FILE"
  fi
}

write_secret() {
  printf %s "$2" > "$SECRETS/$1"
  chmod 600 "$SECRETS/$1"
}

POSTGRES=$(random_hex 24)
MIGRATOR=$(random_hex 24)
RUNTIME=$(random_hex 24)
REDIS=$(random_hex 24)
ADMIN="Cp!$(random_hex 16)"

set_env POSTGRES_ADMIN_PASSWORD "$POSTGRES"
set_env CHAOSPLUS_MIGRATOR_PASSWORD "$MIGRATOR"
set_env CHAOSPLUS_RUNTIME_PASSWORD "$RUNTIME"
set_env REDIS_PASSWORD "$REDIS"

write_secret redis_password "$REDIS"
write_secret authn_signing_key "$(random_key_base64)"
write_secret authn_mfa_key "$(random_key_base64)"
write_secret initial_admin_password "$ADMIN"
write_secret chaosplus_migration_dsn "postgres://chaosplus_migrator:${MIGRATOR}@postgres:5432/chaosplus?sslmode=disable"
write_secret chaosplus_runtime_dsn "postgres://chaosplus_app:${RUNTIME}@postgres:5432/chaosplus?sslmode=disable"

chmod 600 "$ENV_FILE"
printf 'Generated %s and Docker secret files.\n' "$ENV_FILE"
printf 'Initial login: admin@chaosplus.local\nInitial password: %s\n' "$ADMIN"
