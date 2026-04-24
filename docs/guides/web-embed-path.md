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

### 4. 提交到 Git
```bash
git add cmd/server/web/
git commit -m "feat: update embedded Flutter Web build"
git push origin main
```

## 自动化脚本

使用 `scripts/update-web-and-push.sh` 一键完成所有步骤：

```bash
./scripts/update-web-and-push.sh
```

## 用户拉取后的操作

```bash
# 1. 拉取最新代码
git pull origin main

# 2. 重新编译（包含新的嵌入资源）
go build -o server ./cmd/server

# 3. 启动服务
AUTH_TOKEN=test ./server

# 4. 访问 http://localhost:8001
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
2. **`cmd/server/web/`**：Go 实际嵌入的目录，必须保持最新

建议：
- 开发时在 `mobile_vc/` 中修改
- 构建后复制到 `cmd/server/web/`
- 提交 `cmd/server/web/` 到 Git

## 已修复

✅ 已将 Flutter Web 构建产物复制到 `cmd/server/web/`
✅ 已提交到 Git
✅ 已推送到 GitHub

用户现在拉取代码后，重新编译即可看到 Flutter Web 版本。
