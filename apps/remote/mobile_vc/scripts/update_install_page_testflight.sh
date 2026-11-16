#!/bin/bash

set -euo pipefail

SERVER_DEFAULT="root@8.162.1.176"
SERVER_DIR_DEFAULT="/var/www/mobilevc-7899/install"
SITE_URL_DEFAULT="https://mobilevc.top/install"

SCRIPT_DIR="$(realpath "$(dirname "${BASH_SOURCE[0]}")")"

SERVER="$SERVER_DEFAULT"
SERVER_DIR="$SERVER_DIR_DEFAULT"
SITE_URL="$SITE_URL_DEFAULT"
TESTFLIGHT_URL=""
TESTFLIGHT_VERSION=""
TESTFLIGHT_BUNDLE_ID=""
REMOVE_TESTFLIGHT=0

usage() {
  cat <<'EOF'
Usage:
  scripts/update_install_page_testflight.sh [options]

Update the website install page's TestFlight card while preserving
existing OTA iOS and Android entries.

Options:
  --testflight-url URL      TestFlight public invite link.
  --testflight-version V    Version label shown on the card.
  --testflight-bundle-id ID Bundle identifier shown on the card.
  --remove-testflight       Remove the TestFlight card from the page.
  --server USER@HOST        SSH target. Default: root@8.162.1.176.
  --server-dir DIR          Remote install dir. Default: /var/www/mobilevc-7899/install.
  --site-url URL            Public install base URL. Default: https://mobilevc.top/install.
  --help                    Show this message.

Examples:
  scripts/update_install_page_testflight.sh \
    --testflight-url https://testflight.apple.com/join/XXXXXX \
    --testflight-version "1.0.0 (TestFlight)" \
    --testflight-bundle-id com.wustlh.mobilevc.codex20260403

  scripts/update_install_page_testflight.sh --remove-testflight
EOF
}

die() {
  echo "ERROR: $*" >&2
  exit 1
}

log() {
  echo "==> $*"
}

normalize_site_url() {
  local value="$1"
  value="${value%/}"
  printf '%s' "$value"
}

extract_card_attr() {
  local html_file="$1"
  local platform="$2"
  local attr="$3"
  grep "data-platform=\"$platform\"" "$html_file" 2>/dev/null | sed -n "s/.*data-${attr}=\"\\([^\"]*\\)\".*/\\1/p" | head -n 1 || true
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --testflight-url)
      [[ $# -ge 2 ]] || die "--testflight-url requires a value"
      TESTFLIGHT_URL="$2"
      shift 2
      ;;
    --testflight-version)
      [[ $# -ge 2 ]] || die "--testflight-version requires a value"
      TESTFLIGHT_VERSION="$2"
      shift 2
      ;;
    --testflight-bundle-id)
      [[ $# -ge 2 ]] || die "--testflight-bundle-id requires a value"
      TESTFLIGHT_BUNDLE_ID="$2"
      shift 2
      ;;
    --remove-testflight)
      REMOVE_TESTFLIGHT=1
      shift
      ;;
    --server)
      [[ $# -ge 2 ]] || die "--server requires a value"
      SERVER="$2"
      shift 2
      ;;
    --server-dir)
      [[ $# -ge 2 ]] || die "--server-dir requires a value"
      SERVER_DIR="$2"
      shift 2
      ;;
    --site-url)
      [[ $# -ge 2 ]] || die "--site-url requires a value"
      SITE_URL="$2"
      shift 2
      ;;
    --help|-h)
      usage
      exit 0
      ;;
    *)
      die "unknown argument: $1"
      ;;
  esac
done

SITE_URL="$(normalize_site_url "$SITE_URL")"

if [[ "$REMOVE_TESTFLIGHT" -eq 0 ]]; then
  [[ -n "$TESTFLIGHT_URL" ]] || die "--testflight-url is required"
  [[ -n "$TESTFLIGHT_VERSION" ]] || die "--testflight-version is required"
  [[ -n "$TESTFLIGHT_BUNDLE_ID" ]] || die "--testflight-bundle-id is required"
fi

OUTPUT_ROOT="$(mktemp -d "/tmp/mobilevc-install-page-testflight.XXXXXX")"
REMOTE_INDEX_CACHE="$OUTPUT_ROOT/remote_index.html"
LOCAL_INDEX_PATH="$OUTPUT_ROOT/index.html"
INDEX_BASENAME="index.html"

log "reading current install page"
ssh "$SERVER" "cat '$SERVER_DIR/$INDEX_BASENAME' 2>/dev/null || true" > "$REMOTE_INDEX_CACHE" || true

IOS_URL="$(extract_card_attr "$REMOTE_INDEX_CACHE" ios url)"
IOS_VERSION="$(extract_card_attr "$REMOTE_INDEX_CACHE" ios version)"
IOS_BUNDLE_ID="$(extract_card_attr "$REMOTE_INDEX_CACHE" ios package)"
ANDROID_URL="$(extract_card_attr "$REMOTE_INDEX_CACHE" android url)"
ANDROID_VERSION="$(extract_card_attr "$REMOTE_INDEX_CACHE" android version)"
ANDROID_PACKAGE="$(extract_card_attr "$REMOTE_INDEX_CACHE" android package)"

[[ -n "$IOS_URL$ANDROID_URL" ]] || die "remote install page does not contain iOS or Android cards to preserve"

RENDER_ARGS=(--output "$LOCAL_INDEX_PATH")
if [[ -n "$IOS_URL" && -n "$IOS_VERSION" && -n "$IOS_BUNDLE_ID" ]]; then
  RENDER_ARGS+=(
    --ios-url "$IOS_URL"
    --ios-version "$IOS_VERSION"
    --ios-bundle-id "$IOS_BUNDLE_ID"
  )
fi
if [[ -n "$ANDROID_URL" && -n "$ANDROID_VERSION" && -n "$ANDROID_PACKAGE" ]]; then
  RENDER_ARGS+=(
    --android-url "$ANDROID_URL"
    --android-version "$ANDROID_VERSION"
    --android-package-id "$ANDROID_PACKAGE"
  )
fi
if [[ "$REMOVE_TESTFLIGHT" -eq 0 ]]; then
  RENDER_ARGS+=(
    --testflight-url "$TESTFLIGHT_URL"
    --testflight-version "$TESTFLIGHT_VERSION"
    --testflight-bundle-id "$TESTFLIGHT_BUNDLE_ID"
  )
fi

python3 "$SCRIPT_DIR/render_install_page.py" "${RENDER_ARGS[@]}"

log "uploading updated install page"
scp "$LOCAL_INDEX_PATH" "$SERVER:$SERVER_DIR/$INDEX_BASENAME"

log "verifying public url"
curl -fsSI "${SITE_URL}/" >/dev/null

echo
echo "Install page updated"
echo "  public_install_page: ${SITE_URL}/"
if [[ "$REMOVE_TESTFLIGHT" -eq 0 ]]; then
  echo "  testflight_url: $TESTFLIGHT_URL"
  echo "  testflight_version: $TESTFLIGHT_VERSION"
  echo "  testflight_bundle_id: $TESTFLIGHT_BUNDLE_ID"
else
  echo "  testflight_removed: true"
fi
