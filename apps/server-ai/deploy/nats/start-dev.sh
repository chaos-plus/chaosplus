#!/bin/sh
set -eu

SCRIPT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
SERVER_AI_DIR=$(CDPATH= cd -- "$SCRIPT_DIR/../.." && pwd)

if ! command -v docker >/dev/null 2>&1; then
  printf '%s\n' "Docker is required: development NATS runs from the official image." >&2
  exit 1
fi

if ! docker info >/dev/null 2>&1; then
  printf '%s\n' "Docker daemon is unavailable: start Docker and retry." >&2
  exit 1
fi

if ! docker compose version >/dev/null 2>&1; then
  printf '%s\n' "Docker Compose is required to start development NATS." >&2
  exit 1
fi

exec docker compose -f "$SCRIPT_DIR/compose.dev.yaml" up --wait
