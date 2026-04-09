# ✅ Web 端迁移完成

## 已完成的工作

### 1. Flutter Web 平台支持
- ✅ 添加 Web 平台支持 `flutter create . --platforms=web --org=top.mobilevc`
- ✅ 修复 Firebase 推送在 Web 平台的兼容性问题
  - 创建平台特定实现：`push_notification_service_mobile.dart` (iOS/Android)
  - 创建 Web 空实现：`push_notification_service_web.dart`
  - 使用条件导入自动选择正确的实现

### 2. Flutter Web 构建
```bash
cd mobile_vc
flutter build web --release
```
- ✅ 构建成功
- ✅ 产物位于 `mobile_vc/build/web/`

### 3. 替换 Web 目录
```bash
rm -rf web
cp -r mobile_vc/build/web web
```
- ✅ 原 HTML 版本备份到 `web.backup/`
- ✅ Flutter Web 构建产物复制到 `web/`

### 4. Go 后端重新编译
```bash
go build -o server ./cmd/server
```
- ✅ 编译成功，包含新的 Flutter Web 资源
- ✅ 二进制大小：~17MB（包含完整 Flutter Web 运行时）

## 目录结构

```
MobileVC/
├── web/                    # Flutter Web 构建产物（新）
│   ├── index.html
│   ├── flutter.js
│   ├── flutter_bootstrap.js
│   ├── main.dart.js
│   ├── assets/
│   ├── canvaskit/
│   └── ...
├── web.backup/             # 原 HTML 版本（备份，160KB）
│   └── index.html
├── mobile_vc/              # Flutter 源码
│   ├── lib/
│   ├── web/                # Web 平台配置
│   └── build/web/          # 构建产物源
├── server                  # Go 后端二进制（17MB，包含 web 资源）
└── cmd/server/main.go      # Go 后端源码（embed web/*）
```

## 使用方式

### 启动服务
```bash
AUTH_TOKEN=test ./server
# 或
AUTH_TOKEN=test go run ./cmd/server
```

### 访问 Web 端
```
http://localhost:8001
```

现在访问将看到完整的 Flutter Web 应用！

## 功能对比

| 功能 | 原 HTML 版本 | Flutter Web 版本 |
|------|-------------|-----------------|
| 大小 | ~160KB | ~数MB |
| 会话管理 | ❌ | ✅ |
| 文件浏览 | ❌ | ✅ |
| Diff 审核 | ❌ | ✅ |
| Skill/Memory | ❌ | ✅ |
| 终端日志 | ❌ | ✅ |
| 推送通知 | ❌ | ✅ (需浏览器支持) |
| ADB 管理 | ❌ | ✅ |
| Codex 同步 | ❌ | ✅ |
| 响应式设计 | ❌ | ✅ |
| 代码共享 | ❌ | ✅ (与移动端) |

## 后续更新流程

当 Flutter 代码更新时：

```bash
# 方式 1: 使用自动化脚本
./scripts/update_web.sh

# 方式 2: 手动执行
cd mobile_vc
flutter build web --release
cd ..
rm -rf web
cp -r mobile_vc/build/web web
go build -o server ./cmd/server
```

## 开发模式

开发时可以分别运行：

```bash
# Terminal 1: Flutter Web 开发服务器（支持热重载）
cd mobile_vc
flutter run -d chrome

# Terminal 2: Go 后端
AUTH_TOKEN=test go run ./cmd/server
```

## 技术细节

### 平台条件导入
使用 Dart 的条件导入特性，根据平台自动选择正确的实现：

```dart
import 'push_notification_service_mobile.dart'
    if (dart.library.html) 'push_notification_service_web.dart';
```

- 移动平台（iOS/Android）：使用 `push_notification_service_mobile.dart`（真实 Firebase 实现）
- Web 平台：使用 `push_notification_service_web.dart`（空实现）

### Go 后端 Embed
Go 后端使用 `//go:embed web/*` 将 web 目录嵌入到二进制：

```go
//go:embed web/*
webAssets embed.FS

staticFS, _ := fs.Sub(webAssets, "web")
mux.Handle("/", http.FileServer(http.FS(staticFS)))
```

这意味着：
- 编译时打包 web 资源
- 运行时从内存提供静态文件
- 无需外部 web 目录
- 单个二进制文件即可部署

## 性能优化建议

### 1. 启用 Gzip 压缩
```go
import "github.com/NYTimes/gziphandler"
handler := gziphandler.GzipHandler(mux)
```

### 2. 添加缓存头
```go
w.Header().Set("Cache-Control", "public, max-age=3600")
```

### 3. 使用 CanvasKit 渲染器
```bash
flutter build web --web-renderer canvaskit
```

## 已知限制

1. **首次加载较慢**：需要下载 main.dart.js（数MB）
2. **某些原生功能不可用**：如文件系统直接访问
3. **推送通知**：需要浏览器支持 Web Push API

## 回滚方案

如需回退到原 HTML 版本：

```bash
rm -rf web
mv web.backup web
go build -o server ./cmd/server
```

## 总结

✅ Web 端已成功迁移到 Flutter Web  
✅ 原 HTML 版本已备份  
✅ 提供自动化更新脚本  
✅ Go 后端无需修改  
✅ 支持完整的 MobileVC 功能  
✅ 与移动端代码共享  

现在访问 `http://localhost:8001` 将看到完整的 Flutter Web 应用！
