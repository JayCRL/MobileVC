# MobileVC

> Mobile-first local runtime panel for Shell, Claude Code, and AI-powered skills.
>
> 面向手机浏览器的本地运行时面板，用于访问 Shell、Claude Code 与 AI 驱动的技能中心。

## Overview | 项目简介

**MobileVC** is a local-first web console that exposes your desktop command-line environment to a phone browser through a lightweight Go server and WebSocket connection.
It is designed for quick mobile access to trusted local or LAN development environments, with support for interactive PTY sessions, structured runtime events, workspace switching, AI session handling (Claude Code and Gemini), and a built-in Skill Center for code review, debugging, and security analysis.

**MobileVC** 是一个本地优先的 Web 控制台，通过轻量 Go 服务和 WebSocket，将桌面上的命令行环境映射到手机浏览器。
它面向可信的本机或局域网开发场景，支持交互式 PTY 会话、结构化运行时事件、工作区切换、AI 会话处理（Claude Code 与 Gemini），以及内置的技能中心（Skill Center），提供代码审查、调试分析和安全审计等能力。

## Why MobileVC | 为什么做这个项目

**EN**
- Use your local CLI from a phone browser without full remote desktop.
- Keep the runtime on your own machine.
- Handle interactive prompts and long-running terminal workflows more naturally on mobile.
- Bridge Claude Code and shell-based tooling into a mobile-friendly UI.
- Run AI-driven skills (review, debug, simplify) directly from the mobile panel.

**中文**
- 不依赖完整远程桌面，也能在手机浏览器中使用本地 CLI。
- 运行时仍留在你自己的电脑上，本地优先。
- 更自然地处理交互提示和长时间运行的终端任务。
- 将 Claude Code 与 Shell 工具桥接到适合手机操作的 UI 中。
- 在手机面板中直接运行 AI 驱动的技能（审查、调试、简化）。

## Highlights | 核心特性

### 1. Mobile-first runtime access | 面向手机的本地运行时访问
- Access a local command environment from a browser.
- Open the UI on a phone and run commands on the host machine.
- 从浏览器访问本地命令环境。
- 在手机上打开页面后即可在宿主机执行命令。

### 2. Dual execution modes | 双执行模式
- `exec` for standard commands.
- `pty` for interactive terminal workflows.
- `exec` 适合普通命令。
- `pty` 适合交互式终端流程。

### 3. Structured and raw terminal views | 结构化视图与原始终端双视图
- Switch between a structured UI view and a raw terminal view.
- Both views share the same runtime stream and preserve context.
- 支持在结构化 UI 视图与原始终端视图之间切换。
- 两种视图共享同一份运行流，切换时不会丢失上下文。

### 4. Interactive prompt loop | 交互输入闭环
- Detect common prompts and return them as input requests.
- Reply from quick action buttons or manual input.
- 识别常见交互提示，并以下发输入请求的方式暴露给前端。
- 可通过快捷按钮或手动输入完成回复。

### 5. Workspace file tree | 工作区文件树
- Browse directories from the phone UI.
- Change working directory before executing commands.
- 在手机端浏览目录。
- 执行命令前可切换工作目录。

### 6. AI session handling | AI 会话处理
- Supports Claude Code and Gemini session handling.
- Includes structured stream processing, multi-turn continuation, `--resume` session reuse, and permission mode for AI tool approvals.
- Auto-detects AI prompt states and transitions the session controller accordingly.
- Real-time activity panel showing current tool name, target file, elapsed time, and estimated remaining time.
- Session completion summary with duration, turn count, and cost statistics.
- Dynamic permission mode switching during an active session.
- 支持 Claude Code 与 Gemini 的会话处理。
- 包含结构化流处理、多轮连续对话、`--resume` 会话复用，以及 AI 工具审批的 permission mode。
- 自动识别 AI 提示符状态，驱动会话控制器状态切换。
- 实时活动面板，展示当前工具名称、目标文件、已用时间与预计剩余时间。
- 会话完成摘要，包含耗时、轮数与费用统计。
- 支持在活跃会话中动态切换 permission mode。

### 7. Skill Center | 技能中心
- Built-in skills: `review`, `simplify`, `debug`, `security-review`, `explain-step`, `next-step`.
- Invoke AI-powered analysis on diffs, steps, or errors from the frontend.
- Extensible skill registry with structured prompt building and result routing.
- Skills can be sent as input to an already-running AI session, enabling in-session skill invocation.
- 内置技能：`review`、`simplify`、`debug`、`security-review`、`explain-step`、`next-step`。
- 从前端对 diff、step 或 error 发起 AI 驱动的分析。
- 可扩展的技能注册表，支持结构化 prompt 构建与结果路由。
- 技能可作为输入发送到已运行的 AI 会话中，实现会话内技能调用。

### 8. Step and diff overlays | Step 与 Diff 增强层
- Display step updates in the top panel.
- Surface file diffs in a modal overlay.
- 在顶部展示 step 更新。
- 通过弹窗展示文件 diff。

### 9. Lightweight frontend | 轻量前端
- Single-page frontend built with plain HTML and JavaScript.
- Tailwind CSS, Markdown rendering, and syntax highlighting included.
- 前端为单页应用，基于原生 HTML 与 JavaScript。
- 集成 Tailwind CSS、Markdown 渲染与代码高亮。

## Screens of Responsibility | 当前能力边界

**This project is not:**
- a full remote desktop,
- a mobile IDE,
- a hardened public internet gateway.

**本项目不是：**
- 完整远程桌面，
- 手机 IDE，
- 可直接公网暴露的高安全网关。

It is best suited for **local use or trusted LAN environments**.
它更适合 **本机使用或可信局域网环境**。

## Architecture | 架构概览

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

### Backend modules | 后端模块

- `cmd/server`
  Server entrypoint. HTTP routes and static file hosting.
  服务入口，负责 HTTP 路由和静态页面托管。

- `internal/config`
  Environment-based configuration loading.
  基于环境变量的配置加载。

- `internal/protocol`
  WebSocket event and request contracts.
  WebSocket 请求与事件协议定义。

- `internal/ws`
  WebSocket auth, action dispatching, runner lifecycle management.
  WebSocket 鉴权、action 分发与 runner 生命周期管理。

- `internal/runner`
  Command execution core for `exec` and `pty`, with permission mode support for AI tool approvals.
  `exec`/`pty` 执行核心，支持 AI 工具审批的 permission mode。

- `internal/runtime`
  Runtime service layer: runner lifecycle management, session coordination, and event routing.
  运行时服务层：runner 生命周期管理、会话协调与事件路由。

- `internal/skills`
  Skill Center: built-in skill registry, structured prompt builder, and AI-driven analysis launcher.
  技能中心：内置技能注册表、结构化 prompt 构建器与 AI 驱动的分析启动器。

- `internal/adapter`
  ANSI cleanup, error aggregation, output adaptation.
  ANSI 清理、错误聚合、输出适配。

- `internal/session`
  Session controller with state machine (IDLE, THINKING, WAIT_INPUT, RUNNING_TOOL), AI prompt detection, and event deduplication.
  会话控制器，包含状态机（IDLE、THINKING、WAIT_INPUT、RUNNING_TOOL）、AI 提示符检测与事件去重。

- `internal/store`
  Storage abstraction reserved for future persistence.
  为未来持久化预留的存储抽象。

- `web`
  Mobile-first single-page frontend.
  面向手机的单页前端。

## Repository Layout | 目录结构

```text
MobileVC/
├─ cmd/
│  └─ server/
│     └─ main.go
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
├─ web/
│  └─ index.html
├─ run.bat
└─ README.md
```

## Tech Stack | 技术栈

### Backend
- Go 1.21
- `github.com/gorilla/websocket`
- `github.com/creack/pty`

### Frontend
- Plain HTML + JavaScript
- Tailwind CSS (CDN)
- `marked`
- `highlight.js`

## Runtime Protocol | 运行时协议

### Client → Server | 前端上行
- `exec`
- `input`
- `skill`
- `fs_list`

### Server → Client | 后端下行
- `session_state`
- `agent_state`
- `log`
- `progress`
- `error`
- `prompt_request`
- `step_update`
- `file_diff`
- `fs_list_result`

## Quick Start | 快速开始

### 1) Prerequisites | 环境要求
- Go 1.21+
- A local shell environment
- Claude Code CLI if you want Claude-related workflows
- Go 1.21+
- 可用的本地 Shell 环境
- 如果需要 Claude 相关能力，请安装 Claude Code CLI

### 2) Start the server | 启动服务

```bash
AUTH_TOKEN=test PORT=8080 go run ./cmd/server
```

### 3) Open the UI | 打开页面

```text
http://127.0.0.1:8080/
```

### 4) Enter token | 输入 token

```text
test
```

### Windows helper | Windows 快捷启动

You can also use:

```bat
run.bat
```

It prepares the default port/token and starts the server.
它会准备默认端口与 token，并启动服务。

## Typical Workflows | 常见使用方式

### Run a normal command | 运行普通命令
```bash
pwd
```

### Run an interactive command | 运行交互命令
```bash
printf 'Input your name: '; read name; printf 'Hello, %s\n' "$name"
```

### Test error rendering | 测试错误展示
```bash
command_that_does_not_exist_12345
```

### Use Claude Code | 使用 Claude Code
```bash
claude
```

## Development | 开发

### Build and test | 构建与测试

```bash
go test ./...
go build ./...
```

### Format | 格式化

```bash
gofmt -w ./cmd/server/main.go ./internal/protocol/event.go ./internal/ws/handler.go
```

## Current Status | 当前进展

### Implemented | 已实现
- Mobile-first web console for local CLI access
- `exec` and `pty` command execution with permission mode support
- Workspace file tree and cwd switching
- Structured view and raw terminal view
- Prompt request / input response loop
- Step panel and diff modal
- AI session handling for Claude Code and Gemini (stream processing, multi-turn, `--resume`)
- Runtime service layer with runner lifecycle management and event routing
- Skill Center with built-in skills: review, simplify, debug, security-review, explain-step, next-step
- Session controller state machine with AI prompt detection and event deduplication
- RuntimeMeta propagation for skill context and result routing
- Real-time activity panel with tool name, target, elapsed time, and ETA
- Claude stream JSON parsing for assistant/user/result messages with tool-use step tracking
- Session completion summary (duration, turns, cost)
- Dynamic permission mode switching during active sessions
- In-session skill invocation: skills sent as input to running AI sessions
- 面向手机的本地 CLI Web 控制台
- `exec` 与 `pty` 双执行模式，支持 permission mode
- 工作区文件树与 cwd 切换
- 结构化视图与原始终端双视图
- prompt_request / input 交互闭环
- step 面板与 diff 弹窗
- Claude Code 与 Gemini 的 AI 会话处理（流处理、多轮对话、`--resume`）
- 运行时服务层，包含 runner 生命周期管理与事件路由
- 技能中心，内置技能：review、simplify、debug、security-review、explain-step、next-step
- 会话控制器状态机，支持 AI 提示符检测与事件去重
- RuntimeMeta 全链路传播，用于技能上下文与结果路由
- 实时活动面板，显示工具名称、目标文件、已用时间与预计剩余时间
- Claude 流式 JSON 解析，支持 assistant/user/result 消息与工具调用步骤追踪
- 会话完成摘要（耗时、轮数、费用）
- 活跃会话中动态切换 permission mode
- 会话内技能调用：技能作为输入发送到运行中的 AI 会话

### Planned / not fully landed yet | 规划中 / 尚未完全落地
- Persistent session storage
- Multi-session UI management
- Stronger prompt detection coverage
- Explicit backend output format fields
- File preview and editing
- More restrictive security controls
- 会话持久化存储
- 多会话 UI 管理
- 更强的 prompt 检测覆盖
- 后端显式输出格式字段
- 文件预览与编辑
- 更严格的安全控制

## Security Notes | 安全说明

**EN**
Current defaults are intentionally simple for local use, not hardened deployment:
- commands can be executed by the connected client,
- directory listing is not yet limited by a whitelist,
- WebSocket origin checks are currently permissive.

Do **not** expose this service directly to the public internet.
Use it on your own machine or inside a trusted LAN.

**中文**
当前默认实现以本地使用为目标，并非强化安全部署：
- 已连接客户端可以执行命令，
- 目录读取尚未加入白名单限制，
- WebSocket 的 origin 检查当前较宽松。

**不要** 直接将该服务暴露到公网。
建议只在自己的机器或可信局域网中使用。

## Known Limitations | 已知限制

- One WebSocket connection currently supports only one active command at a time.
  单个 WebSocket 连接当前只支持一个活动命令。

- Prompt detection is still rule-based, not universal.
  Prompt 检测仍以规则匹配为主，并非对所有 CLI 通用。

- The file tree supports browsing and cwd switching, not full file editing.
  文件树当前支持浏览和切换 cwd，不支持完整文件编辑。

- Session persistence exists as an abstraction, but is not fully integrated yet.
  会话持久化目前只有抽象层，尚未真正完整接入主流程。

## Roadmap | 路线图

- [ ] File preview
- [ ] Explicit backend output format (`text` / `markdown`)
- [ ] Stronger prompt detection
- [ ] Session persistence
- [ ] Security hardening
- [ ] Multi-session management
- [ ] Custom skill registration (user-defined skills)
- [ ] 文件预览
- [ ] 后端显式输出格式（`text` / `markdown`）
- [ ] 更强的 prompt 检测
- [ ] 会话持久化
- [ ] 安全加固
- [ ] 多会话管理
- [ ] 自定义技能注册（用户自定义 skill）

## Project Positioning | 项目定位

**EN**
MobileVC focuses on the runtime layer first: reliable command execution, interactive CLI handling, mobile usability, AI session plumbing (Claude Code and Gemini), and a Skill Center for AI-driven code analysis.
It is a strong fit for personal workflows, local experiments, and phone-accessible development operations.

**中文**
MobileVC 当前优先聚焦运行时层：稳定的命令执行、交互式 CLI 处理、移动端可用性、AI 会话桥接能力（Claude Code 与 Gemini），以及用于 AI 驱动代码分析的技能中心。
它适合个人工作流、本地实验，以及可从手机访问的开发操作场景。

## License | 许可证

License information has not been defined yet.
当前仓库尚未定义许可证信息。

If you plan to open source the project publicly, add a license file before release.
如果准备正式开源发布，建议在发布前补充 LICENSE 文件。
