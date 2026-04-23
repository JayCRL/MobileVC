# MobileVC 项目索引

最后整理：2026-04-23  
适用范围：当前工作区代码态；如果本文和代码冲突，以代码为准，并同步更新本文。

## 1. 先看哪几份

- [README.md](README.md)：对外介绍、快速开始、当前能力概览。
- [CONTEXT.md](CONTEXT.md)：当前代码事实索引；主架构、关键状态、事件流、已确认漂移点。
- [CHANGELOG.md](CHANGELOG.md)：按 npm 包版本整理的变更记录，当前主线版本为 `0.1.18`。
- [blueprint.md](blueprint.md)：长期设计蓝图和架构草案，适合查背景，不作为现状 source of truth。
- [CLAUDE.md](CLAUDE.md)：本仓库的协作规则、构建/发布约束。

## 2. 当前代码进度

### 会话与恢复

- `SessionController.connect()` 在连接成功后会主动 bootstrap：切 cwd、拉 runtime info、skill/memory/session、ADB 状态。
- 如果当前已经选中 session，连接或重连后会直接补发 `session_resume`，并带上 `lastSeenEventCursor` / `lastKnownRuntimeState`。
- `switchWorkingDirectory(...)` 在已连接且 cwd 变化时会主动刷新 `session_list`，目录切换后的会话列表能跟上当前项目。
- 首次输入存在延迟保护：会话列表尚未同步完成时，前端会暂存输入并在会话创建后自动补发。

### 原生会话整合

- Claude 原生会话来自 `~/.claude/projects/<cwd>/*.jsonl`，会以“电脑 Claude”镜像条目并入列表，可直接恢复。
- Codex 原生会话来自 `~/.codex/state_5.sqlite` + `~/.codex/history.jsonl`，会以“电脑 Codex”镜像条目并入列表，可直接恢复。
- 旧 MobileVC 本地会话在 projection 不完整时，会尝试从原生 Claude jsonl 补齐 assistant 历史。
- Claude 原生会话扫描已兼容 `cwd` 原样路径、绝对路径和 `EvalSymlinks` 结果，降低 Windows / symlink 场景漏检。

### 权限、审核与通知

- 权限规则自动应用已经收敛到后端；前端只展示结果，不再自己匹配规则。
- Claude 权限批准路径已有临时 grant store，避免 hot swap 后重复弹相同授权。
- 权限 continuation 已切到官方 `permission-model-auto` 语义。
- 前端用 `_isStopping` 区分“发起停止”和“后端确认停止”，避免 banner 和按钮状态不同步。
- Diff 查看器支持字符级高亮和 unchanged block 折叠。
- 通知链路已覆盖 APNs、本地 action-needed 去重、前后台切换补发。

### 发布与交付

- npm 包：`@justprove/mobilevc@0.1.18`。
- Flutter Web 嵌入链路：`mobile_vc/build/web/` -> `cmd/server/web/`，由 `npm run sync:web` 同步。
- 当前已跟踪的安装页相关脚本位于 `mobile_vc/scripts/`：
  - `build_ios_ota.sh`
  - `build_ios_testflight.sh`
  - `render_install_page.py`
  - `update_install_page_testflight.sh`

## 3. 代码结构索引

### 根目录

- `bin/`：npm 启动器。
- `cmd/server/`：Go 服务入口；`main.go` 负责 HTTP/WebSocket 启动与嵌入 Web 静态资源。
- `cmd/server/web/`：当前真正嵌入到 Go 二进制里的 Flutter Web 构建产物。
- `internal/`：后端核心模块。
- `mobile_vc/`：Flutter 客户端工程。
- `packages/`：npm 可选依赖形式分发的预编译后端二进制。
- `scripts/`：仓库级构建/发布脚本，例如 `sync-embedded-web.js`。
- `sidecar/chattts/`：可选 TTS 侧车。

### `internal/` 核心模块

- `internal/ws/`：WebSocket action 分发、会话恢复、权限/审核/同步编排。
- `internal/runtime/`：runner 生命周期、hot swap、resume、active runner 管理。
- `internal/runner/`：PTY / exec runner 及 Claude/Codex 交互适配。
- `internal/store/`：session projection、skill/memory snapshot 落盘。
- `internal/claudesync/`：Claude 原生 jsonl 会话镜像。
- `internal/codexsync/`：Codex 原生 sqlite/jsonl 会话镜像。
- `internal/adb/`：ADB / 模拟器 / WebRTC 桥接。
- `internal/push/`：APNs 推送。
- `internal/protocol/`：事件与 action 对应的数据结构。

### `mobile_vc/lib/` 核心模块

- `features/session/`：主状态机、会话列表、连接恢复、输入分流。
- `features/diff/`：diff 审核与折叠查看器。
- `features/files/`：文件浏览和查看。
- `features/permissions/`：权限规则管理和调试入口。
- `features/adb/`：Android 模拟器调试面板。
- `features/runtime_info/`：runtime/info/doctor 视图。
- `data/models/` + `data/services/`：协议模型、WS mapper、服务层。

## 4. 文档索引

### 当前有效

- [README.md](README.md)
- [PROJECT_INDEX.md](PROJECT_INDEX.md)
- [CONTEXT.md](CONTEXT.md)
- [CHANGELOG.md](CHANGELOG.md)
- [CLAUDE.md](CLAUDE.md)

### 主题参考

- [blueprint.md](blueprint.md)：产品/架构蓝图。
- [PUSH_INTEGRATION_CHECKLIST.md](PUSH_INTEGRATION_CHECKLIST.md)：APNs 配置与验证。
- [PUSH_SETUP.md](PUSH_SETUP.md)：推送环境准备。

### 历史/归档参考

- [WEB_MIGRATION.md](WEB_MIGRATION.md)
- [WEB_MIGRATION_COMPLETE.md](WEB_MIGRATION_COMPLETE.md)
- [WEB_MIGRATION_DONE.md](WEB_MIGRATION_DONE.md)
- [PUSH_INTEGRATION_SUMMARY.md](PUSH_INTEGRATION_SUMMARY.md)
- [NPM_PUBLISH_SUMMARY.md](NPM_PUBLISH_SUMMARY.md)
- [NPM_PUBLISH_v0.1.12.md](NPM_PUBLISH_v0.1.12.md)

### 一次性排障/专题说明

- [WEB_EMBED_PATH_FIX.md](WEB_EMBED_PATH_FIX.md)
- [FLUTTER_WEB_BLANK_SCREEN_DEBUG.md](FLUTTER_WEB_BLANK_SCREEN_DEBUG.md)
- [BUGFIX_PLAN.md](BUGFIX_PLAN.md)
- [CONTENT.md](CONTENT.md)

这些文件可以保留作为背景资料，但不应继续充当“当前项目索引”。

## 5. 维护约定

- `README.md`、`CHANGELOG.md`、`package.json` 的版本口径保持一致。
- 代码结构有变化时，先更新 `PROJECT_INDEX.md`，再决定是否需要同步 `README.md` / `CONTEXT.md`。
- 某份旧文档如果已经变成历史资料，不要继续往里追加“现状”说明，改为在本文标记用途。
