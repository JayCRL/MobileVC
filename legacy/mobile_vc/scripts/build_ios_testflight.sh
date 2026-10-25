#!/bin/bash

set -euo pipefail

TEAM_ID_DEFAULT="947RV2M27F"

SCRIPT_DIR="$(realpath "$(dirname "${BASH_SOURCE[0]}")")"
PROJECT_ROOT="$(realpath "$SCRIPT_DIR/..")"

BUILD_NUMBER="$(date +%Y%m%d%H%M%S)"
TEAM_ID="$TEAM_ID_DEFAULT"
BUILD_NAME=""
OUTPUT_ROOT=""
EXPORT_ONLY=0
INTERNAL_ONLY=0
AUTH_KEY_PATH="${APP_STORE_CONNECT_API_KEY_PATH:-}"
AUTH_KEY_ID="${APP_STORE_CONNECT_API_KEY_ID:-}"
AUTH_ISSUER_ID="${APP_STORE_CONNECT_API_ISSUER_ID:-}"

usage() {
  cat <<'EOF'
Usage:
  scripts/build_ios_testflight.sh [options]

Build and optionally upload a MobileVC iOS archive to TestFlight.

Options:
  --build-number N       Override CFBundleVersion. Default: current timestamp.
  --build-name V         Override CFBundleShortVersionString. Default: pubspec version.
  --team-id ID           Apple team ID. Default: 947RV2M27F.
  --output-root DIR      Keep all artifacts under DIR.
  --export-only          Build and export the IPA locally, but skip TestFlight upload.
  --internal-only        Mark upload as internal-TestFlight-only.
  --auth-key-path PATH   App Store Connect API key (.p8) path.
  --auth-key-id ID       App Store Connect API key ID.
  --auth-issuer-id ID    App Store Connect API issuer ID.
  --help                 Show this message.

Environment fallback:
  APP_STORE_CONNECT_API_KEY_PATH
  APP_STORE_CONNECT_API_KEY_ID
  APP_STORE_CONNECT_API_ISSUER_ID

Examples:
  scripts/build_ios_testflight.sh
  scripts/build_ios_testflight.sh --export-only
  scripts/build_ios_testflight.sh --internal-only \
    --auth-key-path /path/to/AuthKey_ABC123XYZ.p8 \
    --auth-key-id ABC123XYZ \
    --auth-issuer-id 00000000-0000-0000-0000-000000000000
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

plist_get() {
  /usr/libexec/PlistBuddy -c "Print :$2" "$1"
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --build-number)
      [[ $# -ge 2 ]] || die "--build-number requires a value"
      BUILD_NUMBER="$2"
      shift 2
      ;;
    --build-name)
      [[ $# -ge 2 ]] || die "--build-name requires a value"
      BUILD_NAME="$2"
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
    --export-only)
      EXPORT_ONLY=1
      shift
      ;;
    --internal-only)
      INTERNAL_ONLY=1
      shift
      ;;
    --auth-key-path)
      [[ $# -ge 2 ]] || die "--auth-key-path requires a value"
      AUTH_KEY_PATH="$2"
      shift 2
      ;;
    --auth-key-id)
      [[ $# -ge 2 ]] || die "--auth-key-id requires a value"
      AUTH_KEY_ID="$2"
      shift 2
      ;;
    --auth-issuer-id)
      [[ $# -ge 2 ]] || die "--auth-issuer-id requires a value"
      AUTH_ISSUER_ID="$2"
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

if [[ -n "$AUTH_KEY_PATH$AUTH_KEY_ID$AUTH_ISSUER_ID" ]]; then
  [[ -n "$AUTH_KEY_PATH" ]] || die "--auth-key-path is required when using App Store Connect API key auth"
  [[ -n "$AUTH_KEY_ID" ]] || die "--auth-key-id is required when using App Store Connect API key auth"
  [[ -n "$AUTH_ISSUER_ID" ]] || die "--auth-issuer-id is required when using App Store Connect API key auth"
  [[ -f "$AUTH_KEY_PATH" ]] || die "auth key file not found: $AUTH_KEY_PATH"
fi

[[ -d "$PROJECT_ROOT/ios" ]] || die "expected Flutter project root at $PROJECT_ROOT"
[[ -f "$PROJECT_ROOT/pubspec.yaml" ]] || die "missing pubspec.yaml at $PROJECT_ROOT"
[[ -d "$PROJECT_ROOT/ios/Runner.xcworkspace" ]] || die "missing workspace at $PROJECT_ROOT/ios/Runner.xcworkspace"

PUBSPEC_VERSION="$(awk '/^version:/{print $2; exit}' "$PROJECT_ROOT/pubspec.yaml")"
[[ -n "$PUBSPEC_VERSION" ]] || die "could not read version from pubspec.yaml"
PUBSPEC_SHORT_VERSION="${PUBSPEC_VERSION%%+*}"

if [[ -z "$BUILD_NAME" ]]; then
  BUILD_NAME="$PUBSPEC_SHORT_VERSION"
fi

if [[ -z "$OUTPUT_ROOT" ]]; then
  OUTPUT_ROOT="$(mktemp -d "/tmp/mobilevc-ios-testflight-${BUILD_NUMBER}.XXXXXX")"
else
  mkdir -p "$OUTPUT_ROOT"
fi

ARCHIVE_PATH="$OUTPUT_ROOT/Runner.xcarchive"
DERIVED_DATA_PATH="$OUTPUT_ROOT/deriveddata"
EXPORT_PATH="$OUTPUT_ROOT/export"
UNZIP_PATH="$OUTPUT_ROOT/unpacked"
ARCHIVE_LOG="$OUTPUT_ROOT/archive.log"
EXPORT_LOG="$OUTPUT_ROOT/export.log"
UPLOAD_LOG="$OUTPUT_ROOT/upload.log"
EXPORT_OPTIONS_PLIST="$OUTPUT_ROOT/ExportOptions.plist"
UPLOAD_OPTIONS_PLIST="$OUTPUT_ROOT/UploadOptions.plist"

AUTH_ARGS=()
if [[ -n "$AUTH_KEY_PATH" ]]; then
  AUTH_ARGS+=(
    -authenticationKeyPath "$AUTH_KEY_PATH"
    -authenticationKeyID "$AUTH_KEY_ID"
    -authenticationKeyIssuerID "$AUTH_ISSUER_ID"
  )
fi

run_export_archive() {
  local export_options_plist="$1"
  local export_path="$2"
  local log_path="$3"

  if [[ ${#AUTH_ARGS[@]} -gt 0 ]]; then
    xcodebuild \
      -exportArchive \
      -archivePath "$ARCHIVE_PATH" \
      -exportOptionsPlist "$export_options_plist" \
      -exportPath "$export_path" \
      -allowProvisioningUpdates \
      "${AUTH_ARGS[@]}" >"$log_path" 2>&1
  else
    xcodebuild \
      -exportArchive \
      -archivePath "$ARCHIVE_PATH" \
      -exportOptionsPlist "$export_options_plist" \
      -exportPath "$export_path" \
      -allowProvisioningUpdates >"$log_path" 2>&1
  fi
}

export FLUTTER_BUILD_NUMBER="$BUILD_NUMBER"
export FLUTTER_BUILD_NAME="$BUILD_NAME"
unset PRODUCT_BUNDLE_IDENTIFIER
unset EXPANDED_CODE_SIGN_IDENTITY
unset EXPANDED_CODE_SIGN_IDENTITY_NAME

log "output root: $OUTPUT_ROOT"
log "build number: $BUILD_NUMBER"
log "build name: $BUILD_NAME"
log "sanitizing GeneratedPluginRegistrant.m for TestFlight release"
sanitize_generated_registrant

cat > "$EXPORT_OPTIONS_PLIST" <<EOF
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>destination</key>
  <string>export</string>
  <key>manageAppVersionAndBuildNumber</key>
  <false/>
  <key>method</key>
  <string>app-store-connect</string>
  <key>signingStyle</key>
  <string>automatic</string>
  <key>stripSwiftSymbols</key>
  <true/>
  <key>teamID</key>
  <string>${TEAM_ID}</string>
  <key>uploadSymbols</key>
  <true/>
EOF

if [[ "$INTERNAL_ONLY" -eq 1 ]]; then
  cat >> "$EXPORT_OPTIONS_PLIST" <<'EOF'
  <key>testFlightInternalTestingOnly</key>
  <true/>
EOF
fi

cat >> "$EXPORT_OPTIONS_PLIST" <<'EOF'
</dict>
</plist>
EOF

cp "$EXPORT_OPTIONS_PLIST" "$UPLOAD_OPTIONS_PLIST"
/usr/libexec/PlistBuddy -c "Set :destination upload" "$UPLOAD_OPTIONS_PLIST"

log "archiving unsigned app"
if ! xcodebuild \
  -workspace "$PROJECT_ROOT/ios/Runner.xcworkspace" \
  -scheme Runner \
  -configuration Release \
  -destination generic/platform=iOS \
  -archivePath "$ARCHIVE_PATH" \
  -derivedDataPath "$DERIVED_DATA_PATH" \
  DEVELOPMENT_TEAM="$TEAM_ID" \
  CODE_SIGN_STYLE=Automatic \
  CODE_SIGNING_ALLOWED=NO \
  CODE_SIGNING_REQUIRED=NO \
  CODE_SIGN_IDENTITY= \
  FLUTTER_BUILD_NUMBER="$BUILD_NUMBER" \
  FLUTTER_BUILD_NAME="$BUILD_NAME" \
  archive >"$ARCHIVE_LOG" 2>&1; then
  tail -n 200 "$ARCHIVE_LOG" >&2 || true
  die "archive failed, see $ARCHIVE_LOG"
fi

log "exporting app-store-connect ipa"
if ! run_export_archive "$EXPORT_OPTIONS_PLIST" "$EXPORT_PATH" "$EXPORT_LOG"; then
  tail -n 200 "$EXPORT_LOG" >&2 || true
  die "export failed, see $EXPORT_LOG"
fi

IPA_PATH="$(find "$EXPORT_PATH" -maxdepth 1 -type f -name '*.ipa' | head -n 1)"
[[ -n "$IPA_PATH" ]] || die "export succeeded but no ipa was produced"

rm -rf "$UNZIP_PATH"
mkdir -p "$UNZIP_PATH"
unzip -q "$IPA_PATH" -d "$UNZIP_PATH"

APP_INFO_PLIST="$UNZIP_PATH/Payload/Runner.app/Info.plist"
FRAMEWORK_DIR="$UNZIP_PATH/Payload/Runner.app/Frameworks"

[[ -f "$APP_INFO_PLIST" ]] || die "missing Runner.app/Info.plist in exported ipa"
[[ -d "$FRAMEWORK_DIR" ]] || die "missing Frameworks directory in exported ipa"

APP_BUNDLE_ID="$(plist_get "$APP_INFO_PLIST" CFBundleIdentifier)"
APP_BUILD_NUMBER="$(plist_get "$APP_INFO_PLIST" CFBundleVersion)"
APP_SHORT_VERSION="$(plist_get "$APP_INFO_PLIST" CFBundleShortVersionString)"

[[ "$APP_BUILD_NUMBER" = "$BUILD_NUMBER" ]] || die "expected build number $BUILD_NUMBER, got $APP_BUILD_NUMBER"
[[ "$APP_SHORT_VERSION" = "$BUILD_NAME" ]] || die "expected short version $BUILD_NAME, got $APP_SHORT_VERSION"

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

SHA256="$(shasum -a 256 "$IPA_PATH" | awk '{print $1}')"

UPLOAD_STATUS="skipped"
if [[ "$EXPORT_ONLY" -eq 0 ]]; then
  log "uploading build to App Store Connect"
  if ! run_export_archive "$UPLOAD_OPTIONS_PLIST" "$OUTPUT_ROOT/upload" "$UPLOAD_LOG"; then
    tail -n 200 "$UPLOAD_LOG" >&2 || true
    die "upload failed, see $UPLOAD_LOG"
  fi
  UPLOAD_STATUS="uploaded"
fi

echo
echo "Build complete"
echo "  output_root: $OUTPUT_ROOT"
echo "  archive_log: $ARCHIVE_LOG"
echo "  export_log: $EXPORT_LOG"
echo "  ipa_path: $IPA_PATH"
echo "  bundle_id: $APP_BUNDLE_ID"
echo "  version: $APP_SHORT_VERSION ($APP_BUILD_NUMBER)"
echo "  internal_only: $INTERNAL_ONLY"
echo "  upload_status: $UPLOAD_STATUS"
echo "  sha256: $SHA256"

if [[ "$EXPORT_ONLY" -eq 0 ]]; then
  echo "  upload_log: $UPLOAD_LOG"
fi
