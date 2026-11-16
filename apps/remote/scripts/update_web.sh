#!/bin/bash
set -e

echo "🔨 Building Flutter web..."
cd mobile_vc
flutter build web --release

echo "📦 Backing up old web directory..."
cd ..
if [ -d "web" ]; then
  rm -rf web.backup
  mv web web.backup
  echo "✅ Old web backed up to web.backup"
fi

echo "📂 Copying Flutter web build..."
cp -r mobile_vc/build/web web

echo "✅ Web directory updated successfully!"
echo ""
echo "To test:"
echo "  AUTH_TOKEN=test go run ./cmd/server"
echo "  open http://localhost:8001"
