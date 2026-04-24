# MobileVC Current Logic Notes

Last updated: 2026-04-24

This document summarizes important runtime logic in the current codebase.

## Connection and Resume

Flutter owns connection state in `SessionController`:

- Initializes `MobileVcWsService` event subscription.
- Opens the WebSocket with `connect()`.
- Bootstraps runtime/session/catalog state after connect.
- Starts a periodic connection health monitor.
- Tracks `eventCursor` per session.
- Requests `task_snapshot_get` and `session_delta_get` after connect/reconnect/foreground recovery when a session is selected.

Server support lives in `internal/ws/handler.go` and `internal/ws/runtime_sessions.go`:

- `session_delta_get` reloads the authoritative backend projection and returns only new history entries, updated diffs, terminal output suffixes, review/context metadata, and task snapshot state.
- `session_resume` reloads persisted session projection/history as the full-sync fallback. Blocking prompt/interaction events may still be replayed after `lastSeenEventCursor`.
- It returns `session_resume_result` with latest cursor, runtime state, and replay count.

## Heartbeat and Stale Socket Handling

Flutter:

- Sends `action=ping` every health interval while connected.
- Treats any incoming event as proof of life.
- If silence exceeds the timeout, closes the service and calls reconnect handling.
- Treats `ws_closed`, `ws_stream_error`, and `ws_send_error` as recoverable socket failures.

Go backend:

- Handles `ping` by emitting a lightweight `pong` JSON event plus a `task_snapshot` for the current session.
- Sets a write deadline before each WebSocket JSON write.

## Launcher QR and CWD

`bin/mobilevc.js` is the npm launcher.

Current behavior:

- `mobilevc start` uses the current shell directory as workspace CWD.
- It passes `RUNTIME_WORKSPACE_ROOT=process.cwd()` to the backend.
- It stores `cwd` in launcher state.
- It includes `cwd` in printed local/LAN URLs and QR codes.
- If the backend is already running, guided QR printing still uses the current invocation directory.

Flutter side:

- `AppConfig.fromLaunchUri(...)` parses `cwd` from QR URLs.
- The connection sheet writes `scanned.cwd` into the CWD input field.
- Saving the connection config persists that CWD for subsequent session operations.

## AI Command Defaults

Claude:

- Empty model config normalizes to `default`.
- `default` means no `--model` flag.
- Default command is plain `claude`.

Codex:

- Empty model config normalizes to `gpt-5-codex`.
- Empty reasoning effort normalizes to `medium`.
- Default command includes `-m gpt-5-codex --config model_reasoning_effort=medium`.

## Session Lists and Native Histories

MobileVC session list merges:

- File-store MobileVC sessions.
- Native Claude sessions from `~/.claude/projects/<cwd>/*.jsonl`.
- Native Codex sessions from `~/.codex/state_5.sqlite` and `~/.codex/history.jsonl`.

CWD matching attempts normalized path variants to reduce mismatch across Windows, symlinks, and absolute/relative paths.

## Permission and Review

- Backend-side permission rules can auto-apply decisions.
- Temporary permission grants reduce duplicate prompts during Claude hot-swap/resume.
- Review state and diffs are stored in session projection.
- Flutter focuses on presenting pending state and sending explicit user decisions.

## Known Edge Cases

- The short pending buffer is only for blocking prompt/interaction replay; progress continuity should come from direct projection/history sync and task snapshots.
- If a client has an old saved CWD, it must re-scan a current QR or manually update the connection path.
- Generated Flutter Web artifacts under `cmd/server/web/` are build output, not source.
