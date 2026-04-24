# MobileVC

MobileVC lets a phone or Flutter Web client control AI coding assistants running on your computer. It combines a Go backend, a Flutter client, and an npm launcher that starts the backend and prints a QR code for quick LAN connection.

Current npm package: `@justprove/mobilevc@0.1.18`.

## What It Does

- Control Claude/Codex sessions from a mobile-friendly Flutter UI.
- Start or continue long-running AI tasks over WebSocket.
- Resume selected sessions after foreground/background transitions or stale mobile network connections.
- Review file diffs, approve permissions, send follow-up input, and inspect terminal/runtime state.
- Mirror native Claude and Codex histories into the MobileVC session list.
- Serve embedded Flutter Web directly from the Go backend.

## Quick Start

```bash
npm install -g @justprove/mobilevc
cd /path/to/your/project
mobilevc
```

The launcher will:

1. Ask for language, port, and `AUTH_TOKEN` on first setup.
2. Start the bundled backend binary.
3. Print local/LAN URLs and a terminal QR code.
4. Include the current shell directory as `cwd` in the QR URL.

Scan the QR from the Flutter client to fill host, port, token, and project path. If the backend is already running, run `mobilevc` from the desired project directory and scan the newly printed QR to update the client-side CWD.

## Current Connection Behavior

- Flutter connects via WebSocket to `/ws?token=...`.
- The client sends application-level `ping` events and the server replies with `pong`.
- If no server events arrive for the configured silence timeout, Flutter treats the socket as stale and reconnects.
- On connect, reconnect, or foreground return, Flutter sends `session_resume` with the last seen event cursor and runtime state.
- The backend replays pending session events and emits `session_resume_result`.

## Launcher and Workspace Path

The npm launcher uses the directory where `mobilevc` is invoked as the project workspace:

- It passes `RUNTIME_WORKSPACE_ROOT=process.cwd()` to the backend.
- It stores `cwd` in launcher state.
- It adds `cwd` to local/LAN launch URLs and QR codes.
- Flutter parses the `cwd` query parameter and overwrites the connection sheet path.

This prevents phone clients from keeping an old development path after scanning a new launcher QR.

## AI Model Defaults

- Claude default model is `default`; MobileVC launches plain `claude` and does not add `--model sonnet` unless explicitly selected.
- Codex default model is `gpt-5-codex` with reasoning effort fallback `medium`.

## Key Paths

- `bin/mobilevc.js`: npm launcher and QR URL generation.
- `cmd/server/`: Go backend entry and embedded Flutter Web assets.
- `internal/ws/`: WebSocket actions, session load/resume, permission/review flow.
- `internal/runtime/`: runner lifecycle, hot swap, resume, runtime info.
- `mobile_vc/lib/features/session/session_controller.dart`: Flutter session/connection state machine.
- `mobile_vc/lib/core/config/app_config.dart`: client config and QR URL parsing.

## Developer Commands

```bash
npm run sync:web        # copy mobile_vc/build/web into cmd/server/web
npm run build:binaries  # build platform backend binaries for npm packages
npm run test:launcher   # launcher unit tests
```

For repository structure and current implementation notes, see `PROJECT_INDEX.md` and `blueprint.md`.
