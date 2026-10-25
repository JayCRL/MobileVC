#!/usr/bin/env bash
set -euo pipefail

# Caddy uses bcrypt for basic_auth. The hash is safe to paste into .env.
# Single-quote it so the $ characters are not expanded.
read -rsp "Password: " password
echo
docker run --rm caddy:2.8-alpine caddy hash-password --plaintext "$password"
