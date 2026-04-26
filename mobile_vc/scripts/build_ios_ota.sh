#!/bin/bash

set -euo pipefail

TEAM_ID_DEFAULT="947RV2M27F"
SERVER_DEFAULT="root@8.162.1.176"
SERVER_DIR_DEFAULT="/var/www/mobilevc-7899/install"
SITE_URL_DEFAULT="https://mobilevc.top/install"
KEYCHAIN_PATH_DEFAULT="$HOME/Library/Keychains/login.keychain-db"

SCRIPT_DIR="$(realpath "$(dirname "${BASH_SOURCE[0]}")")"
PROJECT_ROOT="$(realpath "$SCRIPT_DIR/..")"

BUILD_NUMBER="$(date +%Y%m%d%H%M%S)"
TEAM_ID="$TEAM_ID_DEFAULT"
SERVER="$SERVER_DEFAULT"
SERVER_DIR="$SERVER_DIR_DEFAULT"
SITE_URL="$SITE_URL_DEFAULT"
OUTPUT_ROOT=""
DEPLOY=0
TESTFLIGHT_URL=""
TESTFLIGHT_VERSION=""
TESTFLIGHT_BUNDLE_ID=""
KEYCHAIN_PASSWORD="${KEYCHAIN_PASSWORD:-}"
KEYCHAIN_PATH="${KEYCHAIN_PATH:-$KEYCHAIN_PATH_DEFAULT}"
KEYCHAIN_SERVICE_NAME="${KEYCHAIN_SERVICE_NAME:-mobilevc-ota-keychain-password}"

usage() {
  cat <<'EOF'
Usage:
  scripts/build_ios_ota.sh [options]

Build a MobileVC iOS OTA IPA with:
  1. unsigned archive
  2. Xcode release-testing export
  3. bundle-id sanity checks
  4. optional upload to the install site

Options:
  --build-number N      Override CFBundleVersion. Default: current timestamp.
  --team-id ID          Apple team ID. Default: 947RV2M27F.
  --output-root DIR     Keep all build artifacts under DIR.
  --deploy              Upload IPA + manifest + install page to the server.
  --server USER@HOST    SSH target for deploy. Default: root@8.162.1.176.
  --server-dir DIR      Remote install directory. Default: /var/www/mobilevc-7899/install.
  --site-url URL        Public install base URL. Default: https://mobilevc.top/install.
  --testflight-url URL  Optional TestFlight invite link to show on the install page.
  --testflight-version V
                        Optional TestFlight version label shown on the install page.
  --testflight-bundle-id ID
                        Optional TestFlight bundle identifier shown on the install page.
  --keychain-password P Unlock login keychain before xcodebuild codesign.
                        Can also be passed via KEYCHAIN_PASSWORD env.
  --keychain-path PATH  Keychain to unlock/authorize. Default: ~/Library/Keychains/login.keychain-db.
  --keychain-service S  macOS Keychain service name for auto-reading the password.
                        Default: mobilevc-ota-keychain-password.
  --help                Show this message.

Examples:
  scripts/build_ios_ota.sh
  scripts/build_ios_ota.sh --build-number 11
  scripts/build_ios_ota.sh --build-number 12 --deploy
  scripts/build_ios_ota.sh --build-number 13 --deploy \
    --testflight-url https://testflight.apple.com/join/XXXXXX \
    --testflight-version "1.0.0 (TestFlight)" \
    --testflight-bundle-id com.wustlh.mobilevc.codex20260403
EOF
}

die() {
  echo "ERROR: $*" >&2
  exit 1
}

log() {
  echo "==> $*"
}

sanitize_generated_registrant() {
  local registrant="$PROJECT_ROOT/ios/Runner/GeneratedPluginRegistrant.m"
  [[ -f "$registrant" ]] || return 0

  /usr/bin/perl -0pi -e '
    s/\n#if __has_include\(<integration_test\/IntegrationTestPlugin\.h>\)\n#import <integration_test\/IntegrationTestPlugin\.h>\n#else\n\@import integration_test;\n#endif\n//g;
    s/\n#if __has_include\(<patrol\/PatrolPlugin\.h>\)\n#import <patrol\/PatrolPlugin\.h>\n#else\n\@import patrol;\n#endif\n//g;
    s/\n  \[IntegrationTestPlugin registerWithRegistrar:\[registry registrarForPlugin:@"IntegrationTestPlugin"\]\];\n/\n/g;
    s/\n  \[PatrolPlugin registerWithRegistrar:\[registry registrarForPlugin:@"PatrolPlugin"\]\];\n/\n/g;
  ' "$registrant"
}

resolve_keychain_password() {
  if [[ -n "$KEYCHAIN_PASSWORD" ]]; then
    return 0
  fi

  KEYCHAIN_PASSWORD="$(security find-generic-password -a "$USER" -s "$KEYCHAIN_SERVICE_NAME" -w 2>/dev/null || true)"
}

prepare_keychain_for_codesign() {
  resolve_keychain_password
  [[ -n "$KEYCHAIN_PASSWORD" ]] || return 0
  [[ -f "$KEYCHAIN_PATH" ]] || die "keychain not found at $KEYCHAIN_PATH"

  log "unlocking keychain for codesign"
  security unlock-keychain -p "$KEYCHAIN_PASSWORD" "$KEYCHAIN_PATH"
  security set-keychain-settings -lut 21600 "$KEYCHAIN_PATH"
  security set-key-partition-list -S apple-tool:,apple:,codesign: -s -k "$KEYCHAIN_PASSWORD" "$KEYCHAIN_PATH"
}

plist_get() {
  /usr/libexec/PlistBuddy -c "Print :$2" "$1"
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
    --build-number)
      [[ $# -ge 2 ]] || die "--build-number requires a value"
      BUILD_NUMBER="$2"
      shift 2
      ;;
    --team-id)
      [[ $# -ge 2 ]] || die "--team-id requires a value"
      TEAM_ID="$2"
      shift 2
      ;;
    --output-root)
      [[ $# -ge 2 ]] || die "--output-root requires a value"
      OUTPUT_ROOT="$2"
      shift 2
      ;;
    --deploy)
      DEPLOY=1
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
    --keychain-password)
      [[ $# -ge 2 ]] || die "--keychain-password requires a value"
      KEYCHAIN_PASSWORD="$2"
      shift 2
      ;;
    --keychain-path)
      [[ $# -ge 2 ]] || die "--keychain-path requires a value"
      KEYCHAIN_PATH="$2"
      shift 2
      ;;
    --keychain-service)
      [[ $# -ge 2 ]] || die "--keychain-service requires a value"
      KEYCHAIN_SERVICE_NAME="$2"
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

[[ "$BUILD_NUMBER" =~ ^[0-9]+$ ]] || die "build number must be digits only"
SITE_URL="$(normalize_site_url "$SITE_URL")"

if [[ -n "$TESTFLIGHT_URL$TESTFLIGHT_VERSION$TESTFLIGHT_BUNDLE_ID" ]]; then
  [[ -n "$TESTFLIGHT_URL" ]] || die "--testflight-url is required when setting TestFlight install page fields"
  [[ -n "$TESTFLIGHT_VERSION" ]] || die "--testflight-version is required when setting TestFlight install page fields"
  [[ -n "$TESTFLIGHT_BUNDLE_ID" ]] || die "--testflight-bundle-id is required when setting TestFlight install page fields"
fi

[[ -d "$PROJECT_ROOT/ios" ]] || die "expected Flutter project root at $PROJECT_ROOT"
[[ -f "$PROJECT_ROOT/pubspec.yaml" ]] || die "missing pubspec.yaml at $PROJECT_ROOT"
[[ -d "$PROJECT_ROOT/ios/Runner.xcworkspace" ]] || die "missing workspace at $PROJECT_ROOT/ios/Runner.xcworkspace"

if [[ -z "$OUTPUT_ROOT" ]]; then
  OUTPUT_ROOT="$(mktemp -d "/tmp/mobilevc-ios-ota-${BUILD_NUMBER}.XXXXXX")"
else
  mkdir -p "$OUTPUT_ROOT"
fi

ARCHIVE_PATH="$OUTPUT_ROOT/Runner.xcarchive"
DERIVED_DATA_PATH="$OUTPUT_ROOT/deriveddata"
EXPORT_PATH="$OUTPUT_ROOT/export"
UNZIP_PATH="$OUTPUT_ROOT/unpacked"
EXPORT_OPTIONS_PLIST="$OUTPUT_ROOT/ExportOptions.plist"
PROFILE_PLIST="$OUTPUT_ROOT/embedded_profile.plist"
ARCHIVE_LOG="$OUTPUT_ROOT/archive.log"
EXPORT_LOG="$OUTPUT_ROOT/export.log"

PUBSPEC_VERSION="$(awk '/^version:/{print $2; exit}' "$PROJECT_ROOT/pubspec.yaml")"
[[ -n "$PUBSPEC_VERSION" ]] || die "could not read version from pubspec.yaml"
SHORT_VERSION="${PUBSPEC_VERSION%%+*}"

IPA_BASENAME="mobile_vc_v${BUILD_NUMBER}.ipa"
MANIFEST_BASENAME="manifest_v${BUILD_NUMBER}.xml"
INDEX_BASENAME="index.html"
PUBLIC_INDEX_URL="${SITE_URL}/"
PUBLIC_MANIFEST_URL="${SITE_URL}/${MANIFEST_BASENAME}"
PUBLIC_IPA_URL="${SITE_URL}/${IPA_BASENAME}"

export FLUTTER_BUILD_NUMBER="$BUILD_NUMBER"
unset PRODUCT_BUNDLE_IDENTIFIER
unset EXPANDED_CODE_SIGN_IDENTITY
unset EXPANDED_CODE_SIGN_IDENTITY_NAME

log "output root: $OUTPUT_ROOT"
log "build number: $BUILD_NUMBER"
log "short version: $SHORT_VERSION"
log "sanitizing GeneratedPluginRegistrant.m for OTA release"
sanitize_generated_registrant
prepare_keychain_for_codesign

cat > "$EXPORT_OPTIONS_PLIST" <<EOF
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>destination</key>
  <string>export</string>
  <key>method</key>
  <string>release-testing</string>
  <key>signingStyle</key>
  <string>automatic</string>
  <key>signingCertificate</key>
  <string>Apple Distribution</string>
  <key>stripSwiftSymbols</key>
  <true/>
  <key>teamID</key>
  <string>${TEAM_ID}</string>
  <key>thinning</key>
  <string>&lt;none&gt;</string>
</dict>
</plist>
EOF

log "archiving unsigned app"
if ! xcodebuild \
  -workspace "$PROJECT_ROOT/ios/Runner.xcworkspace" \
  -scheme Runner \
  -configuration Release \
  -destination generic/platform=iOS \
  -archivePath "$ARCHIVE_PATH" \
  -derivedDataPath "$DERIVED_DATA_PATH" \
  CODE_SIGNING_ALLOWED=NO \
  CODE_SIGNING_REQUIRED=NO \
  CODE_SIGN_IDENTITY="" \
  FLUTTER_BUILD_NUMBER="$BUILD_NUMBER" \
  archive >"$ARCHIVE_LOG" 2>&1; then
  tail -n 200 "$ARCHIVE_LOG" >&2 || true
  die "archive failed, see $ARCHIVE_LOG"
fi

log "exporting release-testing ipa"
if ! xcodebuild \
  -exportArchive \
  -archivePath "$ARCHIVE_PATH" \
  -exportOptionsPlist "$EXPORT_OPTIONS_PLIST" \
  -exportPath "$EXPORT_PATH" \
  -allowProvisioningUpdates >"$EXPORT_LOG" 2>&1; then
  tail -n 200 "$EXPORT_LOG" >&2 || true
  die "export failed, see $EXPORT_LOG"
fi

IPA_PATH="$(find "$EXPORT_PATH" -maxdepth 1 -type f -name '*.ipa' | head -n 1)"
[[ -n "$IPA_PATH" ]] || die "export succeeded but no ipa was produced"

rm -rf "$UNZIP_PATH"
mkdir -p "$UNZIP_PATH"
unzip -q "$IPA_PATH" -d "$UNZIP_PATH"

APP_INFO_PLIST="$UNZIP_PATH/Payload/Runner.app/Info.plist"
EMBEDDED_PROFILE="$UNZIP_PATH/Payload/Runner.app/embedded.mobileprovision"

[[ -f "$APP_INFO_PLIST" ]] || die "missing Runner.app/Info.plist in exported ipa"
[[ -f "$EMBEDDED_PROFILE" ]] || die "missing embedded.mobileprovision in exported ipa"

APP_BUNDLE_ID="$(plist_get "$APP_INFO_PLIST" CFBundleIdentifier)"
APP_BUILD_NUMBER="$(plist_get "$APP_INFO_PLIST" CFBundleVersion)"
APP_SHORT_VERSION="$(plist_get "$APP_INFO_PLIST" CFBundleShortVersionString)"

[[ "$APP_BUILD_NUMBER" = "$BUILD_NUMBER" ]] || die "expected build number $BUILD_NUMBER, got $APP_BUILD_NUMBER"
[[ "$APP_SHORT_VERSION" = "$SHORT_VERSION" ]] || die "expected short version $SHORT_VERSION, got $APP_SHORT_VERSION"

FRAMEWORK_DIR="$UNZIP_PATH/Payload/Runner.app/Frameworks"
[[ -d "$FRAMEWORK_DIR" ]] || die "missing Frameworks directory in exported ipa"

CONFLICTING_FRAMEWORKS="$OUTPUT_ROOT/conflicting_frameworks.txt"
MISSING_FRAMEWORK_IDS="$OUTPUT_ROOT/missing_framework_ids.txt"
find "$FRAMEWORK_DIR" -maxdepth 2 -name Info.plist -print0 | while IFS= read -r -d '' plist; do
  framework_id="$(/usr/libexec/PlistBuddy -c 'Print :CFBundleIdentifier' "$plist" 2>/dev/null || true)"
  if [[ -z "$framework_id" ]]; then
    echo "$plist" >> "$MISSING_FRAMEWORK_IDS"
  elif [[ "$framework_id" = "$APP_BUNDLE_ID" ]]; then
    echo "$plist" >> "$CONFLICTING_FRAMEWORKS"
  fi
done

if [[ -f "$MISSING_FRAMEWORK_IDS" ]]; then
  cat "$MISSING_FRAMEWORK_IDS" >&2
  die "one or more embedded frameworks are missing CFBundleIdentifier"
fi

if [[ -f "$CONFLICTING_FRAMEWORKS" ]]; then
  cat "$CONFLICTING_FRAMEWORKS" >&2
  die "one or more embedded frameworks were rewritten to the app bundle identifier"
fi

if find "$FRAMEWORK_DIR" -maxdepth 1 \( -name 'integration_test.framework' -o -name 'patrol.framework' \) | grep -q .; then
  die "test-only frameworks are still embedded in the exported ipa"
fi

security cms -D -i "$EMBEDDED_PROFILE" > "$PROFILE_PLIST"
PROFILE_NAME="$(plist_get "$PROFILE_PLIST" Name)"
PROFILE_UUID="$(plist_get "$PROFILE_PLIST" UUID)"

SHA256="$(shasum -a 256 "$IPA_PATH" | awk '{print $1}')"

log "validated ipa"
log "bundle id: $APP_BUNDLE_ID"
log "profile: $PROFILE_NAME ($PROFILE_UUID)"
log "sha256: $SHA256"

if [[ "$DEPLOY" -eq 1 ]]; then
  DEPLOY_DIR="$OUTPUT_ROOT/deploy"
  mkdir -p "$DEPLOY_DIR"

  INDEX_PATH="$DEPLOY_DIR/$INDEX_BASENAME"
  MANIFEST_PATH="$DEPLOY_DIR/$MANIFEST_BASENAME"
  DEPLOYED_IPA_PATH="$DEPLOY_DIR/$IPA_BASENAME"
  REMOTE_INDEX_CACHE="$OUTPUT_ROOT/remote_index.html"
  PRESERVE_ANDROID_URL=""
  PRESERVE_ANDROID_VERSION=""
  PRESERVE_ANDROID_PACKAGE=""
  PRESERVE_TESTFLIGHT_URL=""
  PRESERVE_TESTFLIGHT_VERSION=""
  PRESERVE_TESTFLIGHT_BUNDLE_ID=""
  FINAL_TESTFLIGHT_URL="$TESTFLIGHT_URL"
  FINAL_TESTFLIGHT_VERSION="$TESTFLIGHT_VERSION"
  FINAL_TESTFLIGHT_BUNDLE_ID="$TESTFLIGHT_BUNDLE_ID"

  cp "$IPA_PATH" "$DEPLOYED_IPA_PATH"

  cat > "$MANIFEST_PATH" <<EOF
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>items</key>
  <array>
    <dict>
      <key>assets</key>
      <array>
        <dict>
          <key>kind</key>
          <string>software-package</string>
          <key>url</key>
          <string>${PUBLIC_IPA_URL}</string>
        </dict>
        <dict>
          <key>kind</key>
          <string>display-image</string>
          <key>needs-shine</key>
          <false/>
          <key>url</key>
          <string>${SITE_URL}/mobilevc-display-image.png</string>
        </dict>
        <dict>
          <key>kind</key>
          <string>full-size-image</string>
          <key>needs-shine</key>
          <false/>
          <key>url</key>
          <string>${SITE_URL}/mobilevc-full-size-image.png</string>
        </dict>
      </array>
      <key>metadata</key>
      <dict>
        <key>bundle-identifier</key>
        <string>${APP_BUNDLE_ID}</string>
        <key>bundle-version</key>
        <string>${BUILD_NUMBER}</string>
        <key>kind</key>
        <string>software</string>
        <key>title</key>
        <string>MobileVC</string>
      </dict>
    </dict>
  </array>
</dict>
</plist>
EOF

  log "creating remote install directory"
  ssh "$SERVER" "mkdir -p '$SERVER_DIR'"

  ssh "$SERVER" "cat '$SERVER_DIR/$INDEX_BASENAME' 2>/dev/null || true" > "$REMOTE_INDEX_CACHE" || true
  PRESERVE_ANDROID_URL="$(extract_card_attr "$REMOTE_INDEX_CACHE" android url)"
  PRESERVE_ANDROID_VERSION="$(extract_card_attr "$REMOTE_INDEX_CACHE" android version)"
  PRESERVE_ANDROID_PACKAGE="$(extract_card_attr "$REMOTE_INDEX_CACHE" android package)"
  PRESERVE_TESTFLIGHT_URL="$(extract_card_attr "$REMOTE_INDEX_CACHE" testflight url)"
  PRESERVE_TESTFLIGHT_VERSION="$(extract_card_attr "$REMOTE_INDEX_CACHE" testflight version)"
  PRESERVE_TESTFLIGHT_BUNDLE_ID="$(extract_card_attr "$REMOTE_INDEX_CACHE" testflight package)"

  if [[ -z "$FINAL_TESTFLIGHT_URL" ]]; then
    FINAL_TESTFLIGHT_URL="$PRESERVE_TESTFLIGHT_URL"
  fi
  if [[ -z "$FINAL_TESTFLIGHT_VERSION" ]]; then
    FINAL_TESTFLIGHT_VERSION="$PRESERVE_TESTFLIGHT_VERSION"
  fi
  if [[ -z "$FINAL_TESTFLIGHT_BUNDLE_ID" ]]; then
    FINAL_TESTFLIGHT_BUNDLE_ID="$PRESERVE_TESTFLIGHT_BUNDLE_ID"
  fi

  RENDER_ARGS=(
    --output "$INDEX_PATH"
    --ios-url "itms-services://?action=download-manifest&amp;url=${PUBLIC_MANIFEST_URL}"
    --ios-version "${SHORT_VERSION} (${BUILD_NUMBER})"
    --ios-bundle-id "$APP_BUNDLE_ID"
  )
  if [[ -n "$FINAL_TESTFLIGHT_URL" || -n "$FINAL_TESTFLIGHT_VERSION" || -n "$FINAL_TESTFLIGHT_BUNDLE_ID" ]]; then
    [[ -n "$FINAL_TESTFLIGHT_URL" ]] || die "missing TestFlight url while rendering install page"
    [[ -n "$FINAL_TESTFLIGHT_VERSION" ]] || die "missing TestFlight version while rendering install page"
    [[ -n "$FINAL_TESTFLIGHT_BUNDLE_ID" ]] || die "missing TestFlight bundle id while rendering install page"
    RENDER_ARGS+=(
      --testflight-url "$FINAL_TESTFLIGHT_URL"
      --testflight-version "$FINAL_TESTFLIGHT_VERSION"
      --testflight-bundle-id "$FINAL_TESTFLIGHT_BUNDLE_ID"
    )
  fi
  if [[ -n "$PRESERVE_ANDROID_URL" && -n "$PRESERVE_ANDROID_VERSION" && -n "$PRESERVE_ANDROID_PACKAGE" ]]; then
    RENDER_ARGS+=(
      --android-url "$PRESERVE_ANDROID_URL"
      --android-version "$PRESERVE_ANDROID_VERSION"
      --android-package-id "$PRESERVE_ANDROID_PACKAGE"
    )
  fi
  python3 "$SCRIPT_DIR/render_install_page.py" "${RENDER_ARGS[@]}"

  log "uploading ipa and install page"
  scp "$DEPLOYED_IPA_PATH" "$MANIFEST_PATH" "$INDEX_PATH" "$SERVER:$SERVER_DIR/"

  log "verifying public urls"
  curl -fsSI "$PUBLIC_INDEX_URL" >/dev/null
  curl -fsSI "$PUBLIC_MANIFEST_URL" >/dev/null
  curl -fsSI "$PUBLIC_IPA_URL" >/dev/null
fi

echo
echo "Build complete"
echo "  output_root: $OUTPUT_ROOT"
echo "  archive_log: $ARCHIVE_LOG"
echo "  export_log: $EXPORT_LOG"
echo "  ipa_path: $IPA_PATH"
echo "  bundle_id: $APP_BUNDLE_ID"
echo "  version: $APP_SHORT_VERSION ($APP_BUILD_NUMBER)"
echo "  profile: $PROFILE_NAME"
echo "  sha256: $SHA256"

if [[ "$DEPLOY" -eq 1 ]]; then
  echo "  public_install_page: $PUBLIC_INDEX_URL"
  echo "  public_manifest: $PUBLIC_MANIFEST_URL"
  echo "  public_ipa: $PUBLIC_IPA_URL"
fi
