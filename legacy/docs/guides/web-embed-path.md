# ⚠️ 重要：Flutter Web 嵌入路径说明

## 问题根因

Go 后端使用 `//go:embed web/*`，但这个路径是**相对于 `cmd/server/main.go` 的**，所以实际嵌入的是：

```
cmd/server/web/
```

而不是根目录的 `web/`。

## 目录结构

```
MobileVC/
├── web/                      # ❌ 这个不会被 Go 嵌入
│   └── (Flutter Web build)
├── cmd/server/
│   ├── main.go              # Go 后端入口
│   └── web/                 # ✅ 这个才会被 Go 嵌入
│       └── (Flutter Web build)
└── mobile_vc/
    └── build/web/           # Flutter 构建产物源
```

## 正确的更新流程

### 1. 构建 Flutter Web
```bash
cd mobile_vc
flutter build web --release
cd ..
```

### 2. 复制到正确的位置
```bash
# 复制到 cmd/server/web（Go 嵌入的位置）
rm -rf cmd/server/web
cp -r mobile_vc/build/web cmd/server/web
```

### 3. 重新编译 Go 后端
```bash
go build -o server ./cmd/server
```

### 4. Git 跟踪规则

`cmd/server/web/` 是本地嵌入产物目录，不再提交 Flutter Web 构建产物到 Git。仓库只保留 `cmd/server/web/.gitkeep`，用于保证 `//go:embed web/*` 在干净 clone 后有匹配文件。

## 自动化脚本

使用 `scripts/update-web-and-push.sh` 一键完成本地构建、同步和 Go 编译：

```bash
./scripts/update-web-and-push.sh
```

## 用户拉取后的操作

```bash
# 1. 拉取最新代码
git pull origin main

# 2. 本地生成 Flutter Web 嵌入资源
cd mobile_vc
flutter build web --release
cd ..
node scripts/sync-embedded-web.js

# 3. 重新编译（包含本地嵌入资源）
go build -o server ./cmd/server

# 4. 启动服务
AUTH_TOKEN=test ./server

# 5. 访问 http://localhost:8001
# 现在应该看到 Flutter Web 版本了
```

## 验证方式

### 检查嵌入的 web 目录
```bash
ls -la cmd/server/web/
```

应该看到：
- `index.html`
- `flutter.js`
- `flutter_bootstrap.js`
- `canvaskit/` 目录
- 等 Flutter Web 文件

### 检查二进制大小
```bash
ls -lh server
```

应该是 ~17MB（包含 Flutter Web 运行时）

### 访问测试
访问 `http://localhost:8001`，应该看到：
- Flutter 加载动画
- 完整的 MobileVC 界面
- 会话管理、文件浏览等功能

## 为什么有两个 web 目录？

1. **根目录 `web/`**：方便开发和查看，但不会被 Go 嵌入
2. **`cmd/server/web/`**：Go 实际嵌入的本地产物目录，必须在 `go build` 前重新生成

建议：
- 开发时在 `mobile_vc/` 中修改
- 构建后复制到 `cmd/server/web/`
- 不提交 `cmd/server/web/` 的构建产物到 Git

## 已修复

✅ `cmd/server/web/` 是 Go 嵌入目录
✅ Flutter Web 构建产物需要本地生成
✅ 构建产物不再提交到 Git
