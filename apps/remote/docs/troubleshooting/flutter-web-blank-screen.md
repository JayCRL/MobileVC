# 🔍 Flutter Web 白屏问题诊断

## 可能的原因

### 1. Flutter Web 构建不完整
Flutter Web 构建失败或不完整，缺少关键的 JavaScript 文件。

**检查方式：**
```bash
ls -la mobile_vc/build/web/
```

应该包含：
- `index.html`
- `flutter.js`
- `flutter_bootstrap.js`
- `main.dart.js` ⚠️ 这个文件很重要
- `assets/` 目录

### 2. 资源路径问题
Flutter Web 的资源路径配置不正确，导致无法加载 JavaScript 文件。

**检查方式：**
打开浏览器开发者工具（F12），查看：
- Console 标签：是否有 JavaScript 错误
- Network 标签：是否有 404 错误

### 3. Base URL 配置问题
Flutter Web 需要正确的 base URL 配置。

**解决方案：**
在 `mobile_vc/web/index.html` 中添加：
```html
<base href="/">
```

### 4. 编译模式问题
使用 `--release` 模式可能导致某些问题。

**尝试使用 profile 模式：**
```bash
cd mobile_vc
flutter build web --profile
```

## 快速诊断步骤

### 步骤 1: 检查 Flutter 构建
```bash
cd mobile_vc
flutter clean
flutter pub get
flutter build web --release
```

### 步骤 2: 检查构建产物
```bash
ls -la build/web/
# 应该看到 main.dart.js 或类似的 JS 文件
```

### 步骤 3: 检查浏览器控制台
1. 启动服务：`AUTH_TOKEN=test ./server`
2. 访问：`http://localhost:8001`
3. 按 F12 打开开发者工具
4. 查看 Console 和 Network 标签

### 步骤 4: 测试简单的 HTML
创建一个简单的测试页面：
```bash
echo '<html><body><h1>Test</h1></body></html>' > cmd/server/web/test.html
```

访问 `http://localhost:8001/test.html`，如果能看到，说明服务器正常。

## 常见解决方案

### 方案 1: 重新构建 Flutter Web
```bash
cd mobile_vc
flutter clean
rm -rf build/
flutter pub get
flutter build web --release --web-renderer canvaskit
```

### 方案 2: 使用 HTML 渲染器
```bash
flutter build web --release --web-renderer html
```

### 方案 3: 检查 pubspec.yaml
确保没有缺少依赖：
```bash
cd mobile_vc
flutter pub get
flutter doctor
```

### 方案 4: 使用开发模式测试
```bash
cd mobile_vc
flutter run -d chrome
```

如果开发模式正常，说明是构建配置问题。

## 临时回退方案

如果 Flutter Web 一直有问题，可以临时回退到 HTML 版本：

```bash
# 恢复旧的 HTML 版本
rm -rf cmd/server/web
cp -r web.backup cmd/server/web

# 重新编译
go build -o server ./cmd/server

# 启动
AUTH_TOKEN=test ./server
```

## 需要提供的信息

为了更好地诊断问题，请提供：

1. **浏览器控制台错误**（F12 -> Console）
2. **Network 标签的错误**（F12 -> Network）
3. **Flutter 构建输出**
4. **是否看到任何内容**（完全空白 vs 有加载动画）

---

**下一步：请打开浏览器开发者工具（F12），告诉我 Console 里显示什么错误信息。**
