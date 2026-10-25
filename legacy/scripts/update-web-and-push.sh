#!/bin/bash
set -e

echo "🔨 Building Flutter Web and updating local embedded web directory..."

# 1. Build Flutter Web
echo "📦 Building Flutter Web..."
cd mobile_vc
if [[ -x .fvm/flutter_sdk/bin/flutter ]]; then
  .fvm/flutter_sdk/bin/flutter build web --release --pwa-strategy=none
else
  flutter build web --release --pwa-strategy=none
fi
cd ..

# 2. Replace cmd/server/web directory (this is what Go embeds)
echo "📂 Replacing cmd/server/web directory..."
node scripts/sync-embedded-web.js

echo "✅ Embedded web directory updated"
echo "  Size: $(du -sh cmd/server/web | awk '{print $1}')"

# 3. Rebuild Go binary
echo "🔨 Rebuilding Go binary..."
go build -o server ./cmd/server

echo "✅ Server binary updated"
echo "  Size: $(ls -lh server | awk '{print $5}')"

echo "✅ Done. cmd/server/web is ignored by Git; rebuild it locally before go build."
