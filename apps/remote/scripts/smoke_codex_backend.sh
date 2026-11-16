#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

export AUTH_TOKEN="${AUTH_TOKEN:-test}"
export PORT="${PORT:-8001}"
export HOST="${HOST:-127.0.0.1}"
export GOCACHE="${GOCACHE:-/tmp/mobilevc-gocache}"

if command -v codex >/dev/null 2>&1; then
  SMOKE_AI_COMMAND_DEFAULT="codex --version"
  SMOKE_ENGINE_DEFAULT="codex"
else
  echo "[smoke] warn: codex CLI not found, fallback to echo command"
  SMOKE_AI_COMMAND_DEFAULT="echo CODEX_NOT_INSTALLED"
  SMOKE_ENGINE_DEFAULT="codex"
fi

export SMOKE_AI_COMMAND="${SMOKE_AI_COMMAND:-$SMOKE_AI_COMMAND_DEFAULT}"
export SMOKE_ENGINE="${SMOKE_ENGINE:-$SMOKE_ENGINE_DEFAULT}"

SERVER_LOG="${SERVER_LOG:-/tmp/mobilevc-codex-smoke-server.log}"

echo "[smoke] start backend PORT=$PORT AUTH_TOKEN=$AUTH_TOKEN"
AUTH_TOKEN="$AUTH_TOKEN" PORT="$PORT" go run ./cmd/server >"$SERVER_LOG" 2>&1 &
SERVER_PID=$!

cleanup() {
  if kill -0 "$SERVER_PID" >/dev/null 2>&1; then
    kill "$SERVER_PID" >/dev/null 2>&1 || true
    wait "$SERVER_PID" >/dev/null 2>&1 || true
  fi
}
trap cleanup EXIT

echo "[smoke] wait /healthz"
for _ in $(seq 1 50); do
  if curl -fsS "http://$HOST:$PORT/healthz" >/dev/null 2>&1; then
    break
  fi
  sleep 0.2
done
curl -fsS "http://$HOST:$PORT/healthz" >/dev/null

echo "[smoke] run codex-basic ws smoke"
go run ./scripts/ws_smoke_flow.go \
  --url "ws://$HOST:$PORT/ws?token=$AUTH_TOKEN" \
  --scenario codex-basic \
  --ai-command "$SMOKE_AI_COMMAND" \
  --engine "$SMOKE_ENGINE"

if command -v codex >/dev/null 2>&1; then
  echo "[smoke] run codex-readme-write ws smoke"
  go run ./scripts/ws_smoke_flow.go \
    --url "ws://$HOST:$PORT/ws?token=$AUTH_TOKEN" \
    --scenario codex-readme-write \
    --ai-command "codex" \
    --engine "$SMOKE_ENGINE"
else
  echo "[smoke] skip codex-readme-write (codex CLI not found)"
fi

echo "[smoke] done"
