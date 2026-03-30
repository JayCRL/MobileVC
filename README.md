---

# 📱 MobileVC — 手机就是你的 Claude Code 控制台

<p align="center">
  <img src="mobile_vc/lib/logo-2.png" alt="MobileVC logo" width="220" />
</p>

<p align="center">
  <strong>摆脱键盘和鼠标的束缚，用手机随时接管电脑上的 Claude Code。</strong>
</p>

<p align="center">
  <em>MobileVC 把 Claude Code 的等待、审批、审核和继续执行，变成一套专为移动端设计的操作闭环。</em>
</p>

<p align="center">
  <img src="https://img.shields.io/badge/Go-1.21-blue" />
  <img src="https://img.shields.io/badge/Flutter-3.13-blue" />
  <img src="https://img.shields.io/badge/License-MIT-green" />
</p>

---

## 这是什么

MobileVC 不是桌面终端的镜像，也不是远程桌面的替代品。

它做的是一件更直接的事：**把电脑上的 Claude Code 变成可以被手机完整操控的工作台。**

你不在电脑前，也能继续把任务推进下去：

- 继续跟 Claude 对话
- 批准或拒绝权限请求
- 接住 Plan Mode 的多轮计划交互
- 审核 diff、接受或回滚修改
- 浏览文件、日志和运行状态
- 恢复历史会话
- 管理 Skill / Memory / Session Context
- 在 Claude 需要你时收到提醒

MobileVC 解决的不是“怎么远程看见电脑”，而是：

> **怎么让你只靠手机，就能完成电脑上的几乎全部 Claude Code 工作流。**

---

## 核心价值

### 1. 为手机重写 Claude Code 的交互

手机上的操作不该依赖键盘盲输。MobileVC 把 Claude Code 的关键等待态拆出来，做成更适合触摸屏的一键动作和可视化面板：

- 普通输入
- 权限确认
- Plan Mode 继续/推进
- 代码审查
- 会话恢复
- 技能 / 记忆管理

### 2. 让你离开电脑也不掉线

你不需要守在桌面前，也不需要回到键盘旁。

- 出门在外也能继续推进任务
- 电脑不在身边也能批准修改
- Claude 正在等待你时，手机能立即介入
- 复杂工作流不会因为离开键盘而中断

### 3. 把工作流做成“点一下就能继续”的手机体验

MobileVC 不是为了展示“能远程看见什么”，而是为了让你真正把事做完，而且做得更快、更直观：

- 看得见：skill 胶囊、memory 卡片、diff 组、日志和运行状态一目了然
- 点得快：启用 / 停用、允许 / 拒绝、接受 / 回滚都能直接点选
- 一键化：一键同步 skill / memory，一句话生成 skill，一句话修改 memory
- 自动化：Claude 生成结果可自动回写 catalog，并刷新管理面板
- 可视化：当前会话启用项、同步状态、最近同步时间都清楚展示

这是一套为手机设计的 Claude Code 控制台。
---

## 主要功能

### 1. 手机直接接管 Claude Code

- 在手机上连接本机 Claude Code 会话
- 继续当前任务，而不是重新开始
- 支持创建、切换、加载、删除会话

### 2. 权限确认与 Plan Mode

- 支持权限请求的允许 / 拒绝
- 支持 Claude 进入 Plan Mode 后的多轮计划交互
- 计划、权限、普通输入分流处理
- 移动端用按钮推进流程，不再依赖 CLI 盲输

### 3. 多文件 Diff 审核

- 按修改组查看待审内容
- 在同一组内切换多个文件
- 查看文件内容或 diff
- 支持 accept / revert / revise
- 支持一键接受全部待审核 diff

### 4. 文件、日志与运行状态查看

- 浏览项目文件树
- 读取文件内容
- 通过 HTTP 下载文件
- 查看终端执行日志
- 在不同 execution 间切换 stdout / stderr
- 查看 runtime info 和 session 历史

### 5. Skill / Memory / Session Context 管理

- Skill 以“胶囊”形式展示，轻点即可执行，长按即可查看详情和修改入口
- Memory 以“卡片”形式展示，内容、启用状态、来源和同步状态一眼可见
- 支持一键同步 skill / memory，和本机 Claude 目录保持一致
- 支持一句话生成 skill、一句话修改 skill / memory
- 结果可自动回写 catalog，并立即刷新管理面板

### 6. 后台提醒

- 当 Claude 需要你操作时发送提醒
- 覆盖继续输入、权限确认、Plan Mode、代码审核
- 通过 action-needed 信号去重，避免重复打扰

### 7. 可选 TTS

- 支持把 Claude 的关键信息转成语音
- 更适合移动中、通勤中或不方便盯屏的场景

---

## 系统架构

```text
Mobile browser / Flutter app
         │
         ▼
  MobileVC Go server
         │
         ├─ WebSocket event stream
         ├─ Claude Code runtime / PTY runner
         ├─ session + projection store
         └─ Python ChatTTS sidecar (optional)
```

### Go 后端

- 入口：`cmd/server/main.go`
- 负责 `/ws`、`/healthz`、`/download`、`/api/tts/synthesize`
- 通过 WebSocket 驱动完整会话状态流
- 管理 PTY runner、session store、Skill / Memory、文件系统与 TTS

### Flutter 客户端

- 入口：`mobile_vc/lib/main.dart` -> `mobile_vc/lib/app/app.dart`
- 根状态由 `SessionController` 驱动
- 首页是 `SessionHomePage`
- 负责把后端事件变成手机上可操作的 UI 状态

### 后端协议

Go 后端通过结构化事件流向前端推送状态，例如：

- `runtime_phase`
- `interaction_request`
- `session_history`
- `skill_catalog_result`
- `memory_list_result`
- `file_diff`
- `prompt_request`
- `agent_state`

---

## 工作原理

1. Flutter 连接 Go 后端 WebSocket
2. Go 后端启动或恢复 Claude Code 的 PTY 会话
3. Claude 在执行中发出等待态、权限态、计划态等结构化信号
4. Flutter 将这些信号渲染成适合手机的操作界面
5. 用户在手机上批准、继续、回退、审核或输入
6. 决策再回灌给 Claude，形成完整闭环

这套设计的核心不是“远程操作一台电脑”，而是：

> **让手机成为你操控电脑上 Claude Code 的主入口。**

---

## 快速开始

### Codex 适配（新增）

MobileVC 现在支持把 `codex` 作为 AI 引擎使用（例如在移动端连接配置里把 `Engine` 设置为 `codex`）。

- Skill 执行会按 `Engine=codex` 路由为 `codex "<prompt>"`。
- 运行态模型识别会显示为 `codex`。
- `runtime_info: doctor` 会额外检查 `codex` CLI 是否可用。

> 说明：当前“会话热恢复/权限热切换”仍以 Claude 的 `--resume` 机制为核心，Codex 以通用 PTY 交互能力为主。

### 1. 安装 Node 启动器

> Smoke test：运行 `AUTH_TOKEN=test ./scripts/test_smoke_flow.sh` 可快速验证后端、WebSocket 与会话主链路。
>
> Codex smoke：运行 `AUTH_TOKEN=test ./scripts/smoke_codex_backend.sh` 可验证 Codex 适配后的后端启动、WS 会话与基础交互链路。

在仓库根目录执行：

```bash
npm i
```

安装后可以直接使用 `mobilevc` 命令。

### 2. 首次启动并配置

第一次运行会提示你输入后端端口和 `AUTH_TOKEN`：

```bash
mobilevc
```

也可以随时重新配置：

```bash
mobilevc setup
```

### 3. 启动 Go 服务

```bash
mobilevc start
```

### 4. 查看状态 / 日志 / 停止

```bash
mobilevc status
mobilevc logs
mobilevc logs --follow
mobilevc stop
```

### 5. 健康检查

```bash
curl http://127.0.0.1:8001/healthz
```

### 6. 打开 Web 工作台

```text
http://127.0.0.1:8001/
```

### 7. 启动 Claude 会话

```text
claude
```

### 仍然支持直接启动 Go 后端

如果你想绕过 Node 启动器，原来的方式仍然可用：

```bash
AUTH_TOKEN=test go run ./cmd/server
```

---

## Flutter 客户端

```bash
cd mobile_vc
flutter pub get
flutter run
```

> 确保 host / port / token 配置正确。

---

## 测试

### Smoke test

建议优先运行一次 smoke test，先确认本地后端、鉴权和 WebSocket 主链路正常。

运行前请先确认本地 Go 服务已启动且 `AUTH_TOKEN` 与测试命令一致。

可直接运行 `AUTH_TOKEN=test ./scripts/test_smoke_flow.sh` 做一次最小主链路自检。

建议先启动 Go 服务，再运行一次最小主链路自检命令。

Smoke test：`AUTH_TOKEN=test ./scripts/test_smoke_flow.sh`，用于快速验证后端、WebSocket 和会话流是否可用。
它会连接本地服务并跑一轮最小端到端流程，帮助你确认环境是否正常。
如果该命令通过，通常说明鉴权、WebSocket 与会话主链路都已就绪。
建议在启动 Go 服务后先跑一次，快速确认 WebSocket、会话流和鉴权都可用。
也可在使用 `mobilevc start` 启动后立即执行同一命令做一次主链路自检。

```bash
AUTH_TOKEN=test ./scripts/test_smoke_flow.sh
```

### Go

```bash
go test ./...
```

### Flutter

```bash
cd mobile_vc
flutter test
```

---

## 项目结构

```text
cmd/server/        # Go 服务入口
internal/          # 后端编排、运行时、协议、存储
web/               # 浏览器工作台
mobile_vc/         # Flutter 客户端
sidecar/chattts/   # 可选 TTS 侧车
```

---

## English Summary

MobileVC turns your phone into the control center for Claude Code running on your computer.

It is built for the moments when you are away from the keyboard but still need to keep shipping: approve permissions, handle Plan Mode, review diffs, inspect files and logs, resume sessions, and keep the workflow moving.

### What it gives you

- Mobile Claude Code control
- Permission confirmations
- Plan Mode handling
- Multi-file diff review
- File / log / runtime inspection
- Session resume and history
- Skill capsules and memory cards
- One-tap sync and AI-assisted authoring
- Skill / Memory / Context management
- Optional TTS notifications

### The idea

Not a terminal mirror.
Not a desktop clone.

**A phone-first workflow that lets you operate Claude Code on your computer almost entirely from mobile.**

---
smoke test passed
