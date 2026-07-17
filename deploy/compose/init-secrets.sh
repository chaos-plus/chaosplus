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
ZITADEL_DB=$(random_hex 24)
SPICEDB_DB=$(random_hex 24)
MIGRATOR=$(random_hex 24)
RUNTIME=$(random_hex 24)
REDIS=$(random_hex 24)
SPICEDB_TOKEN=$(random_hex 32)
ADMIN="Cp!$(random_hex 16)"
MASTER_KEY=$(random_hex 16)
SESSION_KEY=$(random_hex 16)
EXPIRATION=$(date -u -d '+1 year' '+%Y-%m-%dT%H:%M:%SZ')

set_env POSTGRES_ADMIN_PASSWORD "$POSTGRES"
set_env ZITADEL_DB_PASSWORD "$ZITADEL_DB"
set_env SPICEDB_DB_PASSWORD "$SPICEDB_DB"
set_env CHAOSPLUS_MIGRATOR_PASSWORD "$MIGRATOR"
set_env CHAOSPLUS_RUNTIME_PASSWORD "$RUNTIME"
set_env REDIS_PASSWORD "$REDIS"
set_env SPICEDB_TOKEN "$SPICEDB_TOKEN"
set_env ZITADEL_FIRST_ADMIN_PASSWORD "$ADMIN"
set_env ZITADEL_MACHINE_KEY_EXPIRATION "$EXPIRATION"
set_env ZITADEL_LOGIN_PAT_EXPIRATION "$EXPIRATION"

write_secret zitadel_masterkey "$MASTER_KEY"
write_secret redis_password "$REDIS"
write_secret spicedb_token "$SPICEDB_TOKEN"
write_secret session_encryption_key "$SESSION_KEY"
write_secret chaosplus_migration_dsn "postgres://chaosplus_migrator:${MIGRATOR}@postgres:5432/chaosplus?sslmode=disable"
write_secret chaosplus_runtime_dsn "postgres://chaosplus_app:${RUNTIME}@postgres:5432/chaosplus?sslmode=disable"

chmod 600 "$ENV_FILE"
printf 'Generated %s and Docker secret files.\n' "$ENV_FILE"
printf 'Initial login: admin@chaosplus.local\nInitial password: %s\n' "$ADMIN"
