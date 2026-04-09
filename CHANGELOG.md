# Changelog

## [1.1.0] - 2026-04-09

### Added
- 🎉 **iOS APNs 推送通知支持**
  - 完整的 APNs 推送集成
  - 支持 Token 和证书两种认证方式
  - 自动在权限请求和代码审核时推送通知
  - 异步推送，不阻塞主流程
  - 详细配置指南：`PUSH_INTEGRATION_CHECKLIST.md`

- 🌐 **Flutter Web 支持**
  - Web 端迁移到 Flutter Web
  - 与移动端代码共享
  - 完整的 MobileVC 功能
  - 响应式设计，支持桌面和移动浏览器
  - 迁移说明：`WEB_MIGRATION_COMPLETE.md`

- 📦 **推送服务架构**
  - `internal/push/service.go` - 推送服务接口和 APNs 实现
  - `internal/ws/push_helper.go` - 推送辅助函数
  - `internal/store` - Push token 存储
  - Flutter 端推送服务抽象层

### Fixed
- 🔧 修复会话衔接和 Flutter 端无感重连
- 🔧 规范化 session cwd symlink 路径
- 🔧 修复 symlink 等价 cwd 会话过滤

### Changed
- 📝 更新 README，添加快速开始和新功能说明
- 📝 添加完整的推送集成文档
- 📝 添加 Web 迁移文档

### Dependencies
- ➕ 添加 `github.com/sideshow/apns2` v0.25.0
- ➕ Flutter 添加 `firebase_core` 和 `firebase_messaging`

## [1.0.0] - 2026-03-31

### Added
- 🎉 初始版本发布
- 📱 手机直接接管 AI 助手会话
- 🔐 权限确认与 Plan Mode 支持
- 📝 代码审查与 Diff 管理
- 📂 文件浏览与下载
- 🔧 Skill / Memory / Session Context 管理
- 📊 终端日志查看
- 🔄 会话恢复与历史管理
- 📱 ADB 设备管理
- 🔄 Codex 线程同步
