#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

SIDECAR_HOST="${SIDECAR_HOST:-127.0.0.1}"
SIDECAR_PORT="${SIDECAR_PORT:-9966}"
SIDECAR_MODE="${SIDECAR_MODE:-auto}"
CHATTTS_SAMPLE_RATE="${CHATTTS_SAMPLE_RATE:-24000}"
CHATTTS_MODEL_DIR="${CHATTTS_MODEL_DIR:-${HOME}/.cache/mobilevc/chattts}"
PYTHON_BIN="${PYTHON_BIN:-python3}"

if ! command -v "$PYTHON_BIN" >/dev/null 2>&1; then
  echo "python interpreter not found: $PYTHON_BIN" >&2
  exit 1
fi

cd "$SCRIPT_DIR"
mkdir -p "$CHATTTS_MODEL_DIR"

echo "[chattts-sidecar] host=$SIDECAR_HOST port=$SIDECAR_PORT mode=$SIDECAR_MODE sample_rate=$CHATTTS_SAMPLE_RATE model_dir=$CHATTTS_MODEL_DIR python=$PYTHON_BIN"

export SIDECAR_HOST SIDECAR_PORT SIDECAR_MODE CHATTTS_SAMPLE_RATE CHATTTS_MODEL_DIR
exec "$PYTHON_BIN" app.py
