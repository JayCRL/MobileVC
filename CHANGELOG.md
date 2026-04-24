# Changelog

This changelog tracks repository-facing npm package changes. Current package version: `0.1.18`.

## Unreleased

### Fixed

- QR launch URLs now include the current `mobilevc` invocation directory as `cwd`, and Flutter scan handling uses it to overwrite the connection path.
- The npm launcher passes `RUNTIME_WORKSPACE_ROOT=process.cwd()` to the backend and records the launch CWD in state.
- Claude's empty/default model now resolves to `default`, so the default command stays plain `claude` instead of adding `--model sonnet`.
- Flutter treats `ws_send_error` as a recoverable socket disconnect.
- WebSocket server writes now use a write deadline to avoid stuck writes on broken connections.

### Added

- Application-level Flutter -> server `ping` / server -> Flutter `pong` health checks.
- Foreground/connect/reconnect `session_resume` requests with `lastSeenEventCursor` and `lastKnownRuntimeState`.

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
