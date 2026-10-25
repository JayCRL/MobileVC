# Changelog

This changelog tracks repository-facing npm package changes. Current package version: `0.2.10`.

## Unreleased

_No package-facing changes yet._

## [0.2.10] - 2026-06-01

### Added

- **移动端语音通话助手**：新增可配置 Voice API / TTS API 的通话入口，支持先用语音澄清任务、权限模式和约束，再交接给本机 Claude Code / Codex 执行。
- **移动端 Codex 沙箱设置**：新增 Codex 沙箱模式选项，可在移动端直接配置。
- **会话历史窗口可配置**：会话历史长度可配置，续接历史按窗口收敛。

### Changed

- **npm 发布到 `0.2.10`**：平台二进制包和主包已同步发布。
- **Android 安装入口更新**：Android 下载入口指向 `1.0.0 (20260601 arm64)`。
- **Codex 移动端控制增强**：细化移动端 Codex 权限控制，Codex 目标切换移入聊天工具栏，权限菜单改用审批图标，并跟随 Codex 默认 reasoning 展示。
- **Claude 权限模式 UI**：调整 Claude 权限模式选择与展示。

### Fixed

- **语音通话自动提交**：支持自动识别停顿发送，并调优静默判定，避免刚开口就被提前发送；修正交接后误提示“需要补充信息”的执行态。
- **公网 Relay agent 重连恢复**：修复 agent 重连后陈旧连接 / stale relay link 的恢复与导入问题。
- **会话同步与续接**：修复会话同步、Codex 短回复、手动 compact resume 元数据和续接历史回放等问题。
- **聊天键盘布局**：修复移动端聊天键盘 inset 布局与输入冻结。

## [0.2.9] - 2026-05-31

### Added

- **Thinking / reasoning 展示链路**：新增后端 `ThinkingEvent` 解析，以及 Flutter 端模型、映射和 UI 展示。
- **实时 token / context 使用展示**：补充 Claude 上下文占用事件处理，并对显示稳定性做了后续修复。
- **Skill / Memory 页面优化**：优化移动端 Skill / Memory 管理入口与展示体验。

### Changed

- **npm 发布到 `0.2.9`**：平台二进制包和主包已同步发布。
- **Android 发布限定 arm64**：Android release 只打 `arm64-v8a`，安装入口当前指向 `1.0.0 (20260531 arm64)`。
- **README 重新整理**：收敛重复段落，明确官网首页安装入口、iOS TestFlight、Android APK 下载、Token 输入不回显和国内网络访问提示。
- **iOS Archive 签名配置**：Runner 的 Release / Profile 配置明确使用 `Apple Distribution`；OTA 脚本继续保持无签名 archive + export 签名流程。
- **移动端浅色模式 polish**：优化移动端浅色模式下的视觉一致性。
- **iOS OTA 部署说明更新**：补充安装页 / OTA 发布相关说明。

### Fixed

- **官方 Relay 后端重启后无法重新注册 / 配对**：`mobilevc public --relay ...` 每次启动使用独立临时 `RELAY_AGENT_SESSION_STATE_PATH`，并在启动失败、配对超时、停止时清理。
- **会话接管自动回退**：补充会话归属和运行态同步，减少前后台切换后误回观察模式。
- **发送按钮和运行态闪烁**：补齐可接受输入时的 `WAIT_INPUT` 判断，移除 projector 冗余判断，修复输入栏防抖字段缺失。
- **会话状态同步与通知去重**：修复多个 UI 状态同步问题，并收敛 action-needed / 进度推送去重逻辑。
- **Claude context usage 稳定性**：修复上下文占用显示中的旧值覆盖和状态跳动问题。

## [0.2.8] - 2026-05-29

### Fixed

- **停止按钮闪烁**：停止中 UI 状态拆分为独立 `_isStopping`，避免 banner、按钮和输入框语义不一致。
- **Windows Codex 路径**：修复 Windows 环境下 Codex 路径处理问题。
- **Linux PTY pipe broken**：修复 Linux PTY 退出 / 管道关闭时的异常处理。
- **会话接管状态同步**：补充接管后状态同步，减少前端和后端运行态不一致。

### Changed

- **Codex reasoning 默认展示**：跟随 Codex 默认 reasoning 显示策略。
- **原生会话历史优先**：镜像会话优先使用原生历史，并对 session history 进行去重。
- **Relay session state 保留**：优化 Relay session state 生命周期，为后续 `0.2.9` 的 per-launch state 隔离打基础。

## [0.1.18] - 2026-04-21

### Fixed

- Flutter refreshes `session_list` after `switchWorkingDirectory(...)` changes CWD.
- Reconnect flow can resume an already-selected session using `session_resume` with client cursor/runtime state.

## [0.1.17] - 2026-04-21

### Fixed

- Claude native session scanning gained CWD fallback paths for Windows and symlink-heavy workspaces.
- Matching tries original, absolute, and `EvalSymlinks` path forms for Claude project directory encoding.

## [0.1.16] - 2026-04-21

### Changed

- Version alignment release following `0.1.15`.

## [0.1.15] - 2026-04-21

### Added

- Native Claude CLI history mirroring from `~/.claude/projects/<cwd>/*.jsonl`.
- Missing assistant replies can be backfilled from native Claude JSONL when restoring MobileVC sessions.

### Changed

- `npm run sync:web` syncs from `mobile_vc/build/web/` into `cmd/server/web/`.
- Diff viewer supports character-level highlights and unchanged block folding.

## [0.1.13] - 2026-04-10

### Added

- Embedded Flutter Web assets in the Go backend binary.
- Shared Flutter Web/mobile UI and state logic.

### Fixed

- JavaScript MIME handling for embedded Web assets.
- Removed Firebase dependency from Web while keeping mobile push support.

## [0.1.12] - 2026-04-09

### Added

- iOS APNs push notification support.
- Flutter Web migration and push service interfaces.

### Fixed

- Session handoff and Flutter reconnect paths.
- Session CWD symlink normalization.
