#!/bin/bash
set -e

echo "🔨 Building Flutter Web and updating embedded web directory..."

# 1. Build Flutter Web
echo "📦 Building Flutter Web..."
cd mobile_vc
flutter build web --release
cd ..

# 2. Replace cmd/server/web directory (this is what Go embeds)
echo "📂 Replacing cmd/server/web directory..."
rm -rf cmd/server/web
cp -r mobile_vc/build/web cmd/server/web

echo "✅ Embedded web directory updated"
echo "  Size: $(du -sh cmd/server/web | awk '{print $1}')"

# 3. Rebuild Go binary
echo "🔨 Rebuilding Go binary..."
go build -o server ./cmd/server

echo "✅ Server binary updated"
echo "  Size: $(ls -lh server | awk '{print $5}')"

# 4. Commit changes
echo "📝 Committing changes..."
git add cmd/server/web/
git commit -m "feat: update embedded Flutter Web build

- Latest Flutter Web build with all features
- Includes iOS APNs push notification support
- Responsive design for desktop and mobile browsers
- Full MobileVC functionality on web"

# 5. Push to GitHub
echo "📤 Pushing to GitHub..."
git push origin main

echo "✅ Done! Users can now pull the latest Flutter Web version."
echo ""
echo "Users should run:"
echo "  git pull origin main"
echo "  go build -o server ./cmd/server"
echo "  AUTH_TOKEN=test ./server"
