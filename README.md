# MobileVC

> Mobile-first local runtime panel for Shell, Claude Code, and AI-powered skills.
>
> 面向手机浏览器的本地运行时面板，用于访问 Shell、Claude Code 与 AI 驱动的技能中心。

---

## What is MobileVC | 项目简介

**MobileVC** turns your desktop terminal into a phone-friendly web console.
A lightweight Go server bridges your local shell, AI CLI tools (Claude Code / Gemini), and a built-in Skill Center to a mobile browser via WebSocket — no remote desktop required.

**MobileVC** 将桌面终端变为手机友好的 Web 控制台。
一个轻量 Go 服务，通过 WebSocket 将本地 Shell、AI CLI 工具（Claude Code / Gemini）和内置技能中心桥接到手机浏览器 — 无需远程桌面。

> **Best suited for local or trusted LAN environments.**
> **适用于本机或可信局域网环境。**

---

## Features | 核心特性

### Terminal & Execution | 终端与执行
- **Dual execution modes** — `exec` (standard) + `pty` (interactive terminal)
- **Structured & raw views** — switch between structured UI and raw terminal; both share the same stream
- **Interactive prompt loop** — auto-detect prompts, reply via quick-action buttons or manual input
- **Workspace file tree** — browse directories and switch `cwd` from the phone

### AI Session | AI 会话
- **Claude Code & Gemini** — structured stream parsing, multi-turn continuation, `--resume` session reuse
- **Permission mode** — approve/reject AI tool calls; dynamically switch mode during a live session (auto-edit, full-auto, plan-only, manual)
- **Activity panel** — real-time display of current tool name, target file, elapsed time, ETA, and animated tool icon
- **Smart activity timer** — auto-reset on each user turn, auto-stop on prompt request; no stale timers across interactions
- **Tool error detection** — recognize Claude internal `tool_result` errors and surface retry status without exposing raw failures
- **Session summary** — duration, turn count, and cost statistics on completion
- **Claude stream parsing** — JSON parsing for assistant/user/result messages with tool-use step tracking and error-aware routing

### Skill Center | 技能中心
- **Built-in skills** — `review`, `simplify`, `debug`, `security-review`, `explain-step`, `next-step`
- **In-session invocation** — send skills as input to a running AI session
- **Extensible registry** — structured prompt building and result routing
- **Context-aware menus** — trigger skills from diffs, steps, or errors

### UI | 界面
- **Step & diff overlays** — step updates in top panel, file diffs in modal
- **Animated tool icons** — CSS-animated indicators for active tool execution
- **Mobile-first SPA** — plain HTML + JS, Tailwind CSS, `marked`, `highlight.js`

---

## Architecture | 架构

```text
Phone Browser
    ↓
Static Web UI (web/index.html)
    ↓ WebSocket
Go HTTP Server (cmd/server)
    ↓
WS Handler / Protocol Layer
    ↓
Runtime Service + Skill Center
    ↓
Runner Layer (exec / pty)
    ↓
Shell / Claude Code / Gemini / CLI tools
```

### Backend Modules | 后端模块

| Module | Description |
|--------|-------------|
| `cmd/server` | HTTP 入口、路由与静态托管 |
| `internal/config` | 环境变量配置加载 |
| `internal/protocol` | WebSocket 请求与事件协议 |
| `internal/ws` | WebSocket 鉴权、action 分发、runner 生命周期 |
| `internal/runner` | `exec`/`pty` 执行核心，支持 permission mode |
| `internal/runtime` | 运行时服务层：runner 管理、会话协调、事件路由 |
| `internal/session` | 会话状态机 (IDLE → THINKING → WAIT_INPUT → RUNNING_TOOL)、AI 提示符检测、事件去重 |
| `internal/skills` | 技能注册表、prompt 构建器、AI 分析启动器 |
| `internal/adapter` | ANSI 清理、错误聚合、输出适配 |
| `internal/store` | 存储抽象（预留） |
| `web/` | 面向手机的单页前端 |

---

## Quick Start | 快速开始

### Prerequisites | 环境要求
- Go 1.21+
- 可用的本地 Shell
- （可选）Claude Code CLI

### Start | 启动

```bash
AUTH_TOKEN=test PORT=8080 go run ./cmd/server
```

Or on Windows:

```bat
run.bat
```

### Open | 打开

```
http://127.0.0.1:8080/
```

Enter token `test` to connect.

---

## Runtime Protocol | 运行时协议

| Direction | Events |
|-----------|--------|
| Client → Server | `exec`, `input`, `skill`, `fs_list` |
| Server → Client | `session_state`, `agent_state`, `log`, `progress`, `error`, `prompt_request`, `step_update`, `file_diff`, `fs_list_result` |

---

## Tech Stack | 技术栈

| Layer | Stack |
|-------|-------|
| Backend | Go 1.21 · `gorilla/websocket` · `creack/pty` |
| Frontend | HTML + JS · Tailwind CSS (CDN) · `marked` · `highlight.js` |

---

## Repository Layout | 目录结构

```text
MobileVC/
├─ cmd/server/main.go
├─ internal/
│  ├─ adapter/
│  ├─ config/
│  ├─ protocol/
│  ├─ runner/
│  ├─ runtime/
│  ├─ session/
│  ├─ skills/
│  ├─ store/
│  └─ ws/
├─ web/index.html
├─ run.bat
└─ README.md
```

---

## Roadmap | 路线图

- [ ] File preview & editing | 文件预览与编辑
- [ ] Explicit output format (`text` / `markdown`) | 后端显式输出格式
- [ ] Stronger prompt detection | 更强的 prompt 检测
- [ ] Session persistence | 会话持久化
- [ ] Multi-session management | 多会话管理
- [ ] Custom skill registration | 自定义技能注册
- [ ] Security hardening | 安全加固

---

## Security | 安全说明

Current defaults target **local use**, not hardened deployment:
- Connected clients can execute arbitrary commands
- Directory listing has no whitelist restriction
- WebSocket origin checks are permissive

**Do not expose this service to the public internet.**

当前默认面向 **本地使用**，非安全加固部署。**请勿直接暴露到公网。**

---

## Known Limitations | 已知限制

- Single active command per WebSocket connection | 单连接仅支持一个活动命令
- Rule-based prompt detection, not universal | 提示符检测为规则匹配，非通用
- File tree supports browsing/cwd switching only | 文件树仅支持浏览与切换 cwd
- Session persistence is abstracted but not fully integrated | 会话持久化有抽象层，未完整接入

---

## License | 许可证

Not yet defined. Add a `LICENSE` file before public release.
尚未定义。正式发布前请补充 LICENSE 文件。
