# MobileVC for DeepSeek Harness

A **MobileVC-style mobile/PWA surface for [DeepSeek Harness](https://github.com/deepseek-ai/deepseek-harness)**.

It is a DSH **host plugin** + static mobile UI — not a second Agent, not a relay, not a copy of the session state machine.

```text
Phone / PWA
    │  /mobilevc/  (SSE + JSON)
    ▼
DSH host plugin
    ├── ctx.sessionController  sessions / messages / prompt / cancel / follow
    ├── ctx.fileUploads        image & file attachments
    ├── ctx.approval           allow-once / reject
    └── ctx.pluginManager      enable / disable DSH plugins
    ▼
DSH runtime (the only state machine)
```

---

## Status

- **0.1.0**, developed against DSH `0.2.0-rc.2`
- Real data only; no mock session backend
- Host plugin: `plugin/index.mjs`
- UI: `preview/` (vanilla HTML/CSS/JS, no build step)
- Optional read-only viewer: `tools/server.py`

## What this is / is not

**This is**

- A MobileVC-flavoured UI for DSH on a phone
- A real DSH client surface: send, stop, approve, browse files, attach images/files, manage plugins
- A PWA that can be added to the iOS/Android home screen
- One state machine: every read/write goes through the running DSH Host

**This is not**

- The original Flutter MobileVC client
- The original Go relay / E2EE transport
- A multi-tenant SaaS: every authenticated user sees the same DSH sessions and files
- A replacement for DSH Web on desktop

## Why the direction changed

The original MobileVC (Flutter + Go relay) solved remote access, but had two sources of truth: the local Agent and the relay/phone session. That produced the sync and queue-drop problems this project started from.

This branch takes the other road:

> **DSH owns the runtime. MobileVC only owns the surface.**

No second Agent, no relay queue, no duplicated session log. The phone talks to the same `sessionController` the desktop does.

The part of MobileVC worth keeping is the **mobile interaction language**: one session surface, top bar, command dock, iOS-like sheets, blue/white cards, context ring. That is what `preview/` preserves.

## Features

- **Real sessions** — list, switch, inspect, search by title/cwd
- **Live follow** — `/mobilevc/api/follow` consumes `sessionController.follow()`; durable events + `reasoning-delta` / `text-delta`
- **Live reasoning** — “正在思考” card streams; settled reasoning becomes a collapsible “深度思考 · N 字” card
- **Tool cards** — collapsed by default; status/args visible; tap to expand output
- **read_image preview** — DSH attachment thumbnails + full-screen viewer
- **Attachments** — send images as image parts; files via `ctx.fileUploads` receipts
- **Direct approvals** — Allow once / Reject from the phone; stale decisions are a no-op
- **Trace** — DSH-style “迹” timeline of user / thinking / tool / reply / compaction
- **Files** — real workspace browsing with breadcrumbs and subdirectory navigation
- **Plugin management** — real `listPlugins` / `listBundles` / toggles; MobileVC locks itself
- **PWA** — standalone manifest, icons, service worker, safe-area, no pinch/double-tap zoom
- **Quota/error visibility** — `turn/end` failures become an error card (e.g. QUOTA 402)
- **Read-only fallback** — Python viewer for a quick no-write deployment

## Install

### As a DSH bundle (recommended)

From this branch:

```bash
dsh plugin --profile web add "github:JayCRL/MobileVC#mobilevc-dsh"
systemctl restart dsh-web
```

Or from a local clone:

```bash
git clone --branch mobilevc-dsh https://github.com/JayCRL/MobileVC.git mobilevc-dsh
dsh plugin --profile web add ./mobilevc-dsh
systemctl restart dsh-web
```

Open:

```text
http://127.0.0.1:8898/mobilevc/
```

Health check:

```bash
curl http://127.0.0.1:8898/mobilevc/api/health
# {"ok":true,"plugin":"mobilevc","interactive":true}
```

Full deployment, nginx, PWA and troubleshooting: [INSTALL.md](./INSTALL.md).

> For remote access, put `/mobilevc/` behind an authenticating HTTPS reverse proxy. The plugin itself has no login screen and runs with DSH Host privileges.

## Use

- **+** → image or file attachment
- **Enter** send, **Shift+Enter** newline
- During a run, the red pulse button right of Send interrupts the current turn
- Tap a tool card to expand its output
- Tap a `read_image` thumbnail for full screen
- Tap **迹** for the execution timeline
- Tap **插件** for DSH plugin management
- Tap **Memory** to read/write real `AGENTS.md`
- Approvals appear as cards with **拒绝 / 允许一次**

## Security model

The `/mobilevc/` routes are registered by a Host plugin, **outside DSH’s `/api` browser-trust fence**. They can read sessions, files and logs, write `AGENTS.md`, send prompts, approve tool calls and toggle plugins.

- Put it behind your own auth/HTTPS reverse proxy
- Treat every user as the same DSH user; there is no per-user isolation
- Do not expose port 8898 directly to the internet
- Image attachments require a model that declares image input

## Direction

Near term:

- Publish this as a proper versioned DSH bundle / release (this branch is the packaging step)
- Tighten live trace: step grouping, durations, per-tool timing
- Better attachment UX (paste, camera, drag/drop)
- Approval presence: mobile-first when a mobile follow is open, desktop otherwise (already the default)
- Optional native shell for iOS/Android using the same PWA/HTTP surface; no second session runtime

Deliberately not planned:

- Re-implementing the agent loop or session log
- A second relay/E2EE layer inside this plugin
- Multi-tenant isolation inside one DSH process

## Layout

```text
plugin/index.mjs      Host plugin: /mobilevc + sessionController bridge
preview/              MobileVC UI (index.html, styles.css, app-live.js)
tools/server.py       Optional read-only Python viewer
cordis.patch.yml      DSH bundle patch
package.json          DSH bundle metadata
INSTALL.md            Deployment / nginx / PWA / troubleshooting
```

## License

MIT — see [LICENSE](./LICENSE).
