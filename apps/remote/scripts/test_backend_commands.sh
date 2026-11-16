#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

echo "[test] go test ./internal/ws -run Slash"
go test ./internal/ws -run Slash

echo "[test] go test ./..."
go test ./...
