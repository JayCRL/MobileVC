# Web 端迁移完成

## ✅ 已完成

### 1. Flutter Web 构建
```bash
cd mobile_vc
flutter build web --release
```

### 2. 替换 Web 目录
- 原 `web/` 目录备份到 `web.backup/`
- Flutter Web 构建产物复制到 `web/`
- Go 后端重新编译（embed 新的 web 资源）

### 3. 编译验证
```bash
go build -o server ./cmd/server
```

## 📊 对比

| 项目 | 原 HTML 版本 | Flutter Web 版本 |
|------|-------------|-----------------|
| 大小 | ~160KB | ~数MB |
| 功能 | 基础聊天 | 完整 MobileVC |
| 响应式 | 否 | 是 |
| 代码共享 | 否 | 与移动端共享 |

## 🚀 使用方式

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

## 🔄 后续更新流程

当 Flutter 代码更新时：

```bash
# 使用自动化脚本
./scripts/update_web.sh

# 或手动执行
cd mobile_vc
flutter build web --release
cd ..
rm -rf web
cp -r mobile_vc/build/web web
go build -o server ./cmd/server
```

## 📝 注意事项

1. **构建时机**：每次 Flutter 代码更新后需要重新构建
2. **编译时机**：每次替换 web 目录后需要重新编译 Go 后端（因为 embed）
3. **开发模式**：开发时可以用 `flutter run -d chrome` 支持热重载
4. **回滚方案**：如需回退到原 HTML 版本，执行 `mv web.backup web`

## ✨ 功能增强

Flutter Web 版本支持：
- 完整的会话管理
- 文件浏览和 Diff 审核
- Skill 和 Memory 管理
- 终端日志查看
- 推送通知（需浏览器支持）
- ADB 设备管理
- Codex 线程同步
- 响应式设计（支持桌面和移动浏览器）

详细说明见 `WEB_MIGRATION.md`
