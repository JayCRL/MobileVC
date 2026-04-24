# MobileVC Architecture Blueprint

Last updated: 2026-04-24

This blueprint documents the current code path. It is descriptive, not a product roadmap.

## 1. High-Level Shape

MobileVC connects a Flutter client to a Go backend over WebSocket. The backend owns runner lifecycle, session persistence, native Claude/Codex history mirroring, permissions, review state, and embedded Flutter Web hosting.

Main files:

- Flutter state hub: `mobile_vc/lib/features/session/session_controller.dart`
- Flutter WebSocket service: `mobile_vc/lib/data/services/mobilevc_ws_service.dart`
- Flutter config/QR parsing: `mobile_vc/lib/core/config/app_config.dart`
- Go WebSocket handler: `internal/ws/handler.go`
- Runtime manager: `internal/runtime/manager.go`
- Protocol structs: `internal/protocol/event.go`
- npm launcher: `bin/mobilevc.js`

## 2. Launcher and Connection Bootstrap

1. User runs `mobilevc` or `mobilevc start` from a project directory.
2. `bin/mobilevc.js` loads launcher config from `~/.mobilevc/launcher/config.json`.
3. It starts the platform backend binary with:
   - `PORT`
   - `AUTH_TOKEN`
   - `RUNTIME_WORKSPACE_ROOT=process.cwd()`
4. It prints local/LAN URLs and a terminal QR code.
5. The URL includes `token` and `cwd`; scanning it in Flutter fills host, port, token, and CWD.
6. Flutter persists the resulting `AppConfig` and connects to `ws://host:port/ws?token=...`.

Important current behavior: if a backend is already running, QR generation still uses the current `mobilevc` invocation directory for `cwd`, so users can re-scan from the intended project directory.

## 3. Flutter Connection State Machine

`SessionController.connect()`:

1. Opens WebSocket via `MobileVcWsService.connect(...)`.
2. Marks connected state and bootstraps runtime/session data.
3. Switches to configured CWD and requests runtime info, skill catalog, memory list, session list, ADB status, session context, permission rules, and review state.
4. If a session is already selected, sends `session_resume` with:
   - `sessionId`
   - `cwd`
   - `reason`
   - `lastSeenEventCursor`
   - `lastKnownRuntimeState`

`resumeConnectionIfNeeded()`:

- If the socket is still considered connected, it sends `session_resume` directly.
- Otherwise it schedules foreground reconnect.

Health monitor:

- Periodically sends `action=ping`.
- Any incoming event refreshes the last-seen timestamp.
- If no event arrives within the silence timeout, Flutter closes the stale channel and enters reconnect.

## 4. Go WebSocket Handler

`internal/ws/handler.go` handles these relevant actions:

- `ping`: replies with a lightweight `pong` event.
- `session_list`: returns MobileVC sessions merged with native Claude/Codex mirrors.
- `session_load`: loads persisted projection/history and attaches the runtime session.
- `session_resume`: loads projection, emits recovery state/history/review state, replays pending runtime events since the client cursor, and returns `session_resume_result`.
- `exec`: starts a command/AI runner.
- `input`: sends input to active PTY or resumes a managed Claude session.
- `permission_decision` / `review_decision`: continue permission/review flows.

Server writer behavior:

- All outbound WS events go through a write channel.
- Writes now set a deadline, so broken mobile/network connections fail instead of blocking indefinitely.

## 5. Runtime Sessions and Replay

`internal/ws/runtime_sessions.go` provides per-session runtime ownership:

- Active listeners receive live events.
- Prompt/interaction events get an event cursor and are buffered.
- Detached sessions can accumulate resume notices.
- `pendingSince(cursor)` is used by `session_resume` to replay missed events.

Known limitation: the pending buffer is bounded; very long/high-frequency output can evict old pending events.

## 6. AI Command Defaults

Flutter builds preferred AI commands in `_preferredAiCommandForEngine(...)`:

- Claude default: `claude` (`default` model means no `--model` flag).
- Claude explicit model: `claude --model <model>`.
- Codex default: `codex -m gpt-5-codex --config model_reasoning_effort=medium`.
- Gemini: `gemini`.

The UI display model for empty Claude config is `Default`.

## 7. Session and History Model

- MobileVC-owned sessions persist projections in the file store.
- Native Claude sessions are mirrored from `~/.claude/projects/<cwd>/*.jsonl`.
- Native Codex sessions are mirrored from `~/.codex/state_5.sqlite` and `~/.codex/history.jsonl`.
- Session list filtering uses the current CWD and normalizes path variants to reduce Windows/symlink mismatches.

## 8. Flutter Event Handling

`MobileVcMapper` maps backend JSON `type` values into Dart `AppEvent` subclasses.

`SessionController._handleEvent(...)`:

- Tracks event cursors.
- Updates agent/session/runtime state.
- Restores history/projections.
- Maintains timelines, diffs, terminal logs, review state, pending prompts, notifications, and derived activity banners.
- Treats `ws_closed`, `ws_stream_error`, and `ws_send_error` as unexpected socket disconnects when auto reconnect is enabled.

## 9. Files Worth Updating Together

When changing connection/reconnect behavior, check:

- `mobile_vc/lib/features/session/session_controller.dart`
- `mobile_vc/lib/data/services/mobilevc_ws_service.dart`
- `internal/ws/handler.go`
- `internal/ws/runtime_sessions.go`
- `internal/protocol/event.go`

When changing launcher QR/default workspace behavior, check:

- `bin/mobilevc.js`
- `mobile_vc/lib/core/config/app_config.dart`
- `mobile_vc/lib/features/session/session_home_page.dart`
- `internal/config/config.go`

When changing model selection defaults, check:

- `mobile_vc/lib/features/session/claude_model_utils.dart`
- `mobile_vc/lib/features/session/session_controller.dart`
- runtime model detection in `internal/runtime/manager.go`
