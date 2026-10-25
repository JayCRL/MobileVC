#!/bin/bash
set -e

echo "🔨 Building and publishing all MobileVC packages..."

# 1. Update main package version
echo "📦 Updating main package version..."
cd /Users/wust_lh/MobileVC
npm version patch --no-git-tag-version

MAIN_VERSION=$(cat package.json | grep '"version"' | head -1 | awk -F'"' '{print $4}')
echo "✅ Main package version: $MAIN_VERSION"

# 2. Build binaries
echo "🔨 Building binaries..."
npm run build:binaries

# 3. Update all binary package versions
echo "📦 Updating binary package versions to $MAIN_VERSION..."
for dir in packages/server-*; do
  if [ -d "$dir" ]; then
    echo "  - Updating $dir..."
    cd "$dir"
    npm version $MAIN_VERSION --no-git-tag-version --allow-same-version
    cd ../..
  fi
done

# 4. Publish binary packages
echo "📤 Publishing binary packages..."
for dir in packages/server-*; do
  if [ -d "$dir" ]; then
    echo "  - Publishing $dir..."
    (cd "$dir" && npm publish)
  fi
done

# 5. Update main package dependencies
echo "📦 Updating main package dependencies..."
cat > /tmp/update_deps.js << 'EOF'
const fs = require('fs');
const pkg = JSON.parse(fs.readFileSync('package.json', 'utf8'));
const version = pkg.version;

for (const dep in pkg.optionalDependencies) {
  if (dep.startsWith('@justprove/mobilevc-server-')) {
    pkg.optionalDependencies[dep] = version;
  }
}

fs.writeFileSync('package.json', JSON.stringify(pkg, null, 2) + '\n');
console.log('✅ Updated optionalDependencies to version', version);
EOF

node /tmp/update_deps.js

# 6. Publish main package
echo "📤 Publishing main package..."
npm publish

# 7. Commit and push
echo "📝 Committing version updates..."
git add package.json packages/server-*/package.json
git commit -m "chore: bump all package versions to $MAIN_VERSION"
git push origin main

echo "✅ All packages published successfully!"
echo ""
echo "📦 Published packages:"
echo "  - @justprove/mobilevc@$MAIN_VERSION"
echo "  - @justprove/mobilevc-server-darwin-arm64@$MAIN_VERSION"
echo "  - @justprove/mobilevc-server-darwin-x64@$MAIN_VERSION"
echo "  - @justprove/mobilevc-server-linux-arm64@$MAIN_VERSION"
echo "  - @justprove/mobilevc-server-linux-x64@$MAIN_VERSION"
echo "  - @justprove/mobilevc-server-win32-x64@$MAIN_VERSION"
