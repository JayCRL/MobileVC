# Bugfix Plan Archive

Last updated: 2026-04-24

This file is now an archive of previously investigated session-management issues. Current source-of-truth implementation notes live in:

- `PROJECT_INDEX.md`
- `blueprint.md`
- `CONTENT.md`
- `CHANGELOG.md`

## Current Status

The latest code includes these fixes related to the historical issues in this plan:

- Flutter sends `session_resume` on connect/reconnect/foreground recovery when a session is selected.
- Resume requests include `lastSeenEventCursor` and `lastKnownRuntimeState`.
- Go server replays buffered runtime events via `pendingSince(...)` and returns `session_resume_result`.
- Flutter has an application-level heartbeat and stale-socket recovery.
- Go WebSocket writer uses a write deadline.
- QR launch URLs carry `cwd` so clients do not keep stale development paths after scanning.
- Claude default model is `default` and no longer forces `--model sonnet`.

## Remaining Follow-Up

- Increase or persist the runtime replay buffer for very long/high-output tasks.
- Add focused tests around QR `cwd`, heartbeat, `session_resume`, and send-error recovery.
- Continue reducing `SessionController` size by splitting reducers/coordinators.
