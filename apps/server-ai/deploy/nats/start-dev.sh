#!/bin/sh
set -eu

SCRIPT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
SERVER_AI_DIR=$(CDPATH= cd -- "$SCRIPT_DIR/../.." && pwd)

if command -v docker >/dev/null 2>&1 && docker info >/dev/null 2>&1; then
  if docker compose version >/dev/null 2>&1; then
    exec docker compose -f "$SCRIPT_DIR/compose.dev.yaml" up --wait
  fi
  printf '%s\n' "Docker is available but the Compose plugin is missing." >&2
  exit 1
fi

printf '%s\n' "Docker is unavailable; starting the development-only embedded NATS fallback." >&2
cd "$SERVER_AI_DIR"
CONTROL_ENV=development exec go run ./cmd/nats
