# MobileVC Project Index

Last updated: 2026-04-24
Scope: current repository state. If this document conflicts with code, treat code as the source of truth and update this file.

## Start Here

- `README.md`: user-facing overview, install/start commands, and current capabilities.
- `PROJECT_INDEX.md`: repository map and current implementation status.
- `blueprint.md`: architecture blueprint for Flutter -> Go WebSocket -> runtime/session flows.
- `CHANGELOG.md`: npm-oriented change history. Current package version is `0.1.18`.
- `CLAUDE.md`: repository collaboration/build/publish notes.

## Current Implementation Status

### Connection, Reconnect, and Resume

- Flutter WebSocket service lives in `mobile_vc/lib/data/services/mobilevc_ws_service.dart`.
- `SessionController` is the Flutter state hub: `mobile_vc/lib/features/session/session_controller.dart`.
- Native Flutter uses a WebSocket ping interval; the app now also sends application-level `ping` events and expects server `pong` events.
- If the Flutter client receives no server events for about 45 seconds, it treats the connection as stale, disconnects, and enters auto-reconnect.
- On connect/reconnect/foreground recovery, Flutter sends `session_resume` for the selected session with `lastSeenEventCursor` and `lastKnownRuntimeState`.
- The Go server handles `session_resume` in `internal/ws/handler.go`, replays pending events from `runtimeSession.pendingSince(...)`, emits session history/review state, and returns `session_resume_result`.
- Server WebSocket writes now use a write deadline to avoid long-lived stuck writes on broken mobile/network connections.

### Launcher, QR, and Workspace CWD

- npm launcher entry: `bin/mobilevc.js`.
- `mobilevc start` starts the backend with `RUNTIME_WORKSPACE_ROOT=process.cwd()`.
- The printed LAN/local URL and terminal QR code include `cwd=<current launcher directory>`.
- When the backend is already running, guided QR printing still uses the directory of the current `mobilevc` invocation, not stale state.
- Flutter scan parsing reads `cwd` from the URL via `AppConfig.fromLaunchUri(...)` and fills the connection sheet CWD field.
- This fixes the old behavior where scanning a QR kept a previously saved/development path on the phone.

### AI Runtime and Model Defaults

- Claude/Codex command selection is generated in `SessionController._preferredAiCommandForEngine(...)`.
- Claude's empty/default model now resolves to `default`, so the default launch command is plain `claude` without `--model sonnet`.
- Codex still defaults to `gpt-5-codex` with reasoning effort fallback `medium`.
- Runtime execution, input, hot swap, and resume orchestration live under `internal/runtime/` and `internal/ws/handler.go`.

### Native Session Integration

- Claude native history is mirrored from `~/.claude/projects/<cwd>/*.jsonl` and can be loaded/resumed from the MobileVC session list.
- Codex native history is mirrored from `~/.codex/state_5.sqlite` plus `~/.codex/history.jsonl`.
- Session list filtering is CWD-aware and includes symlink/absolute path fallbacks for native Claude/Codex matching.

### Permissions, Review, and Notifications

- Permission rule matching and auto-apply are handled on the Go side; Flutter displays resulting state/events.
- Temporary permission grants reduce repeated prompts during Claude hot-swap/resume flows.
- Review state, file diffs, terminal logs, and runtime process info are persisted into session projections.
- Push/local notification plumbing remains split between Flutter app services and Go push helpers.

## Repository Map

### Root

- `bin/`: npm launcher (`mobilevc`).
- `cmd/server/`: Go backend entry and embedded Flutter Web assets.
- `internal/`: backend core modules.
- `mobile_vc/`: Flutter mobile/web client.
- `packages/`: optional platform-specific npm packages containing prebuilt backend binaries.
- `scripts/`: repository-level build/release helpers, including embedded web sync.
- `sidecar/chattts/`: optional TTS sidecar.
- `test/`: launcher tests and related repo tests.

### Backend Modules

- `internal/ws/`: WebSocket action dispatch, session load/resume, permission/review orchestration, ADB WebRTC bridge.
- `internal/runtime/`: active runner lifecycle, hot swap, resume, process snapshots, runtime info.
- `internal/runner/`: PTY/exec runners and Claude/Codex command adaptation.
- `internal/store/`: file-backed session projection and catalogs.
- `internal/claudesync/`: native Claude JSONL history mirroring.
- `internal/codexsync/`: native Codex SQLite/history mirroring.
- `internal/protocol/`: event/request structs shared by backend and Flutter JSON payloads.
- `internal/config/`: environment-driven backend config (`PORT`, `AUTH_TOKEN`, `RUNTIME_WORKSPACE_ROOT`, etc.).

### Flutter Client

- `mobile_vc/lib/app/`: app lifecycle, notifications, background keep-alive.
- `mobile_vc/lib/core/config/app_config.dart`: persisted connection/config model and QR launch URI parsing.
- `mobile_vc/lib/data/services/mobilevc_ws_service.dart`: WebSocket channel, event mapping, send error reporting.
- `mobile_vc/lib/features/session/session_controller.dart`: main state machine for connection/session/runtime/UI state.
- `mobile_vc/lib/features/session/session_home_page.dart`: primary UI, connection sheet, QR scan handling.
- `mobile_vc/lib/data/models/events.dart`: Flutter event models.

## Current Known Gaps

- `runtimeSession` pending replay buffer is still limited (`defaultRuntimeSessionPendingLimit`), so extremely high-frequency long-task output can still evict old events.
- Some historical docs are version-specific release notes and may intentionally describe older npm versions.
- Several large generated/build artifacts are present; avoid treating `cmd/server/web/main.dart.js` as source.
