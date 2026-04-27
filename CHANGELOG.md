# Changelog

This changelog tracks repository-facing npm package changes. Current package version: `0.2.0`.

## Unreleased

### Fixed

- **切后台误入观察模式**：新增 `Ownership` 字段标记会话归属（`mobilevc` / `claude-native` / `codex-native`），创建时确定，只升级不降级。Flutter 端 `_isExternalNativeSession` 优先读此字段，断连时重置 `_selectedSessionExternalNative`。
- **切后台回来状态跳动**：新增 `ExecutionActive` 运行锁存器，非 IDLE 即锁存为 `true`，IDLE 或 runtime session 超时释放时解锁。Flutter 端 `AgentStateEvent` 中 `WAIT_INPUT` 不再清除 `_sessionRuntimeAlive`。
- **后台收不到进度通知**：推送触发事件扩展为 `AgentStateEvent`（THINKING/RUNNING_TOOL）、`StepUpdateEvent`、`LogEvent`（assistant_reply）、`ErrorEvent`。进度类事件 30s 防抖，WebSocket 在线时跳过进度推送，离线时才发 APNs。

### Added

- `runtimeSessionRegistry` 增加 `onCleanup` 回调，runtime session 超时释放时自动解锁 `ExecutionActive`。
- `runtimeSessionRegistry.HasActiveConnection()` 方法，供推送模块判断 Flutter 是否在线。

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
