# Changelog

这份变更记录按仓库对外分发的 npm 包版本维护，当前主线版本为 `0.1.18`。

## [0.1.18] - 2026-04-21

### Fixed

- Flutter `switchWorkingDirectory(...)` 在 cwd 变化后会主动刷新 `session_list`，避免会话列表停留在旧目录。
- 后台断开再重连时，如果当前已有选中 session，会自动补发 `session_resume`，并携带 `lastSeenEventCursor` / `lastKnownRuntimeState`。

## [0.1.17] - 2026-04-21

### Fixed

- Claude 原生会话扫描在 Windows 和 symlink 场景下增加 cwd 候选路径回退，降低 `~/.claude/projects/...` 漏匹配概率。
- 会同时尝试原始路径、绝对路径和 `EvalSymlinks` 结果来匹配 Claude CLI 的项目目录编码。

## [0.1.16] - 2026-04-21

### Changed

- 版本整理发布，无额外功能差异；用于承接 `0.1.15` 之后的包版本对齐。

## [0.1.15] - 2026-04-21

### Added

- 接入原生 Claude CLI 会话镜像：扫描 `~/.claude/projects/<cwd>/*.jsonl`，在 MobileVC 会话列表展示为“电脑 Claude”。
- 恢复旧 MobileVC 会话时，可从原生 Claude jsonl 补齐缺失的 assistant 回复历史。

### Changed

- `npm run sync:web` 的同步源切换到 `mobile_vc/build/web/`，目标为 `cmd/server/web/`。
- Diff 查看器支持字符级高亮和 unchanged block 折叠，窄屏可读性更好。

## [0.1.13] - 2026-04-10

### Added

- Flutter Web 构建产物完整嵌入 Go 二进制，启动后端即可直接访问 Web 工作台。
- Web 端与移动端共享主要 UI/状态管理逻辑。

### Fixed

- 修复 JavaScript MIME 类型问题。
- Web 端移除 Firebase 依赖，移动端保留推送功能。

## [0.1.12] - 2026-04-09

### Added

- iOS APNs 推送通知支持。
- Flutter Web 迁移完成。
- 推送服务接口、APNs 实现和对应存储链路落地。

### Fixed

- 修复会话衔接和 Flutter 端无感重连。
- 规范化 session cwd symlink 路径。

### Docs

- 更新 `README.md`。
- 新增 `PUSH_INTEGRATION_CHECKLIST.md` 和 Web 迁移文档。
