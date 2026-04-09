# Web 端迁移说明

## 已完成的工作

### 1. Flutter Web 构建
- ✅ 执行 `flutter build web --release`
- ✅ 生成产物位于 `mobile_vc/build/web/`

### 2. 替换 Web 目录
- ✅ 原 `web/` 目录备份到 `web.backup/`
- ✅ Flutter Web 构建产物复制到 `web/`
- ✅ Go 后端 `cmd/server/main.go` 无需修改（已使用 embed）

### 3. 自动化脚本
- ✅ 创建 `scripts/update_web.sh` 用于后续更新

## 目录结构

```
MobileVC/
├── web/                    # Flutter Web 构建产物（新）
│   ├── index.html
│   ├── flutter.js
│   ├── main.dart.js
│   ├── assets/
│   └── ...
├── web.backup/             # 原 HTML 版本（备份）
│   └── index.html
├── mobile_vc/              # Flutter 源码
│   └── build/web/          # 构建产物源
└── cmd/server/main.go      # Go 后端（embed web/*）
```

## 工作原理

Go 后端使用 `//go:embed web/*` 将 web 目录嵌入到二进制文件中：

```go
//go:embed web/*
webAssets embed.FS

// ...

staticFS, err := fs.Sub(webAssets, "web")
mux.Handle("/", http.FileServer(http.FS(staticFS)))
```

这意味着：
1. 编译时会将 `web/` 目录打包进二进制
2. 运行时直接从内存提供静态文件
3. 无需外部 web 目录

## 测试验证

```bash
# 启动后端
AUTH_TOKEN=test go run ./cmd/server

# 访问 Web 端
open http://localhost:8001
```

应该看到 Flutter Web 应用而不是原来的 HTML 页面。

## 后续更新流程

当 Flutter 代码有更新时：

```bash
# 方式 1: 使用脚本
./scripts/update_web.sh

# 方式 2: 手动执行
cd mobile_vc
flutter build web --release
cd ..
rm -rf web.backup
mv web web.backup
cp -r mobile_vc/build/web web
```

然后重新编译 Go 后端：

```bash
go build ./cmd/server
```

## 注意事项

### 1. 构建时机
- 每次 Flutter 代码更新后需要重新构建 web
- 每次替换 web 目录后需要重新编译 Go 后端（因为 embed）

### 2. 开发模式
开发时可以分别运行：

```bash
# Terminal 1: Flutter Web 开发服务器
cd mobile_vc
flutter run -d chrome

# Terminal 2: Go 后端
AUTH_TOKEN=test go run ./cmd/server
```

Flutter Web 开发服务器支持热重载，更方便调试。

### 3. 生产部署
生产环境部署时：

```bash
# 1. 构建 Flutter Web
cd mobile_vc
flutter build web --release

# 2. 替换 web 目录
cd ..
rm -rf web
cp -r mobile_vc/build/web web

# 3. 编译 Go 后端
go build -o mobilevc-server ./cmd/server

# 4. 部署二进制文件
# mobilevc-server 已包含所有 web 资源
```

### 4. 回滚方案
如果需要回退到原 HTML 版本：

```bash
rm -rf web
mv web.backup web
go build ./cmd/server
```

## 功能对比

### 原 HTML 版本 (web.backup/)
- 简单的 WebSocket 客户端
- 基础的聊天界面
- 仅支持桌面浏览器

### Flutter Web 版本 (web/)
- 完整的 MobileVC 功能
- 响应式设计（支持桌面和移动浏览器）
- 与 Flutter 移动端代码共享
- 支持：
  - 会话管理
  - 文件浏览和 Diff 审核
  - Skill 和 Memory 管理
  - 终端日志查看
  - 推送通知（需要浏览器支持）
  - ADB 设备管理
  - Codex 线程同步

## 已知限制

### Flutter Web 限制
1. 首次加载较慢（需要下载 main.dart.js）
2. 某些原生功能不可用（如文件系统访问）
3. 推送通知需要浏览器支持 Web Push API

### 解决方案
1. 使用 CDN 加速静态资源
2. 启用 gzip 压缩
3. 使用 PWA 模式提升体验

## 性能优化建议

### 1. 启用 Gzip 压缩
在 Go 后端添加 gzip 中间件：

```go
import "github.com/NYTimes/gziphandler"

handler := gziphandler.GzipHandler(mux)
server := &http.Server{
    Addr:    addr,
    Handler: handler,
}
```

### 2. 添加缓存头
```go
mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
    // 静态资源缓存 1 小时
    w.Header().Set("Cache-Control", "public, max-age=3600")
    http.FileServer(http.FS(staticFS)).ServeHTTP(w, r)
})
```

### 3. 使用 CanvasKit 渲染器
```bash
flutter build web --web-renderer canvaskit
```

更好的性能和一致性，但包体积更大。

## 总结

✅ Web 端已成功迁移到 Flutter Web
✅ 原 HTML 版本已备份
✅ 提供自动化更新脚本
✅ Go 后端无需修改

现在访问 `http://localhost:8001` 将看到完整的 Flutter Web 应用！
