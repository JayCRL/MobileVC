# MobileVC

MobileVC 是一个面向手机浏览器的本地 Web 控制台，用来把桌面环境中的 Shell、Claude Code / Gemini CLI 会话、slash command、技能调用、文件浏览和 diff 审阅投射到移动端。

它不是远程桌面，也不是完整 IDE；更适合在离开电脑时快速查看运行状态、继续对话、触发命令、检查文件和审阅改动。

> 当前定位：**本机 / 可信局域网使用**
>
> **不要直接暴露到公网。**

---

## 核心能力

### 1. 移动端连接本地运行时
- 浏览器通过 token 连接后端 WebSocket
- 实时接收命令输出、会话状态、步骤更新、提示请求和 diff 事件
- 进入页面后可直接在手机上继续当前工作流

### 2. 本地命令执行
- 支持 `exec` 与 `pty` 两种模式
- `exec` 适合一次性命令
- `pty` 适合持续交互场景，如 Shell、Claude、Gemini
- 支持切换工作目录后执行命令

### 3. Claude / Gemini 会话续接
- 可直接启动 `claude` / `gemini`
- 支持 `--resume` 续接会话上下文
- 可识别等待输入、工具运行、步骤更新等状态
- 会话运行过程中可以继续发送输入

### 4. 技能中心
当前内置技能定义位于 `internal/skills/registry.go`，包括：
- `review`
- `analyze`
- `doctor`
- `simplify`
- `debug`
- `security-review`
- `explain-step`
- `next-step`

这些技能可基于当前 diff、步骤或错误上下文构造 prompt，并在当前会话继续执行，或新起一次 AI 执行。

### 5. Slash command 支持
当前后端支持多类 slash command：

- 本地语义命令：`/clear`、`/exit`、`/quit`、`/diff`、`/fast`
- runtime info：`/help`、`/context`、`/model`、`/cost`、`/doctor`
- skill 命令：`/review`、`/analyze`
- exec 映射：`/init`、`/memory`、`/add-dir`、`/plan`、`/execute`、`/compact`、`/run`、`/build`、`/test`、`/git status`、`/git diff`、`/git commit`、`/git push`、`/git pull`、`/pr create`

说明：
- 并不是所有 slash command 都会直接触发后端执行
- 一部分命令是前端本地语义命令
- 部分高风险命令在前端语义上会先要求确认

### 6. 文件浏览与读取
- 支持列目录：`fs_list`
- 支持读取文件：`fs_read`
- 前端提供移动端文件抽屉与单文件视图
- 可从文件上下文继续发出指令

当前文件能力定位为**浏览 / 读取**，不是完整在线编辑器。

### 7. Diff 审阅与状态反馈
- 支持 `file_diff`、`step_update`、`prompt_request` 等事件
- 前端可展示最近 diff、步骤状态、运行日志
- 支持对 diff 执行 `accept / revert / revise` 决策

---

## 适用场景

- 在手机上查看本机 AI 编码会话是否仍在运行
- 离开电脑时继续给 Claude / Gemini 发送下一条指令
- 快速查看最近 diff，并决定接受、撤销或继续修改
- 在移动端触发 `go build ./...`、`go test ./...`、`git status` 等命令
- 浏览项目目录并读取单个文件内容

---

## 项目结构

```text
MobileVC/
├─ cmd/server/               # HTTP 服务入口
├─ internal/
│  ├─ adapter/              # ANSI / 输出解析与适配
│  ├─ config/               # 环境变量配置加载
│  ├─ protocol/             # WebSocket 请求与事件协议
│  ├─ runner/               # exec / pty 执行器，Claude/Gemini 命令封装
│  ├─ runtime/              # 运行时服务、活动 runner 管理、runtime info
│  ├─ session/              # 会话状态机、提示检测、diff 上下文
│  ├─ skills/               # 技能注册与 prompt 构建
│  ├─ store/                # 存储抽象
│  └─ ws/                   # WebSocket handler、action 分发、文件操作、slash command
├─ scripts/                 # 测试辅助脚本
├─ web/index.html           # 移动优先单页前端
├─ go.mod
└─ README.md
```

---

## 架构概览

```text
Phone Browser
   ↓
web/index.html
   ↓ WebSocket / HTTP
cmd/server
   ↓
internal/ws
   ↓
internal/runtime + internal/session
   ↓
internal/runner
   ↓
Shell / Claude Code / Gemini / CLI
```

核心模块职责：

- `cmd/server/main.go`：启动 HTTP 服务并挂载 `/`、`/healthz`、`/ws`
- `internal/ws/handler.go`：统一处理 `exec`、`input`、`skill_exec`、`runtime_info`、`slash_command`、`fs_list`、`fs_read`
- `internal/runtime/manager.go`：维护当前活动 runner，并管理运行快照
- `internal/runtime/info.go`：提供 `help`、`context`、`model`、`cost`、`doctor` 等 runtime info
- `internal/session/session.go`：把原始 runner 事件整理成更适合前端展示的状态
- `internal/runner/`：处理 `exec` / `pty` 执行，以及 Claude / Gemini 的交互包装
- `internal/skills/`：根据上下文构造技能 prompt，并决定执行方式
- `internal/protocol/event.go`：定义前后端事件协议
- `web/index.html`：移动优先单文件 SPA

---

## 运行要求

### 后端
- Go 1.21+
- 本地可用 Shell
- 可选：`claude` CLI
- 可选：`gemini` CLI
- 可选：`gh`（如果要使用 `/pr create`）

### 使用建议
- macOS 下建议从你日常使用的终端环境启动，避免 PATH 不一致
- 如果要体验 AI 交互，建议本机已能直接执行 `claude` 或 `gemini`

---

## 配置

当前配置加载逻辑见 `internal/config/config.go`。

### 必填环境变量
- `AUTH_TOKEN`：访问 token

### 常用环境变量
- `PORT`：HTTP 端口，默认 `8080`
- `RUNTIME_DEFAULT_COMMAND`：默认命令，默认 `claude`
- `RUNTIME_DEFAULT_MODE`：默认模式，默认 `pty`
- `RUNTIME_DEBUG`：是否开启调试输出
- `RUNTIME_WORKSPACE_ROOT`：运行时工作区根目录
- `RUNTIME_ENHANCED_PROJECTION`
- `RUNTIME_ENABLE_STEP_PROJECTION`
- `RUNTIME_ENABLE_DIFF_PROJECTION`
- `RUNTIME_ENABLE_PROMPT_PROJECTION`

示例：

```bash
AUTH_TOKEN=test PORT=8080 go run ./cmd/server
```

启动后访问：

```text
http://127.0.0.1:8080/
```

然后在页面中输入 token，例如：

```text
test
```

---

## HTTP / WebSocket 接口

### HTTP
- `GET /`：前端页面
- `GET /healthz`：健康检查

### WebSocket
- `GET /ws?token=...`

---

## WebSocket Action 一览

### 客户端 → 服务端
- `exec`：执行命令
- `input`：向当前活动会话发送输入
- `review_decision`：对 diff 做 accept / revert / revise
- `skill_exec`：执行技能
- `runtime_info`：查询运行时信息
- `slash_command`：执行 slash command
- `fs_list`：列目录
- `fs_read`：读取文件

### 服务端 → 客户端
常见事件包括：
- `session_state`
- `agent_state`
- `log`
- `progress`
- `error`
- `prompt_request`
- `step_update`
- `file_diff`
- `runtime_info_result`
- `fs_list_result`
- 文件读取结果事件（由 `fs_read` 返回）

---

## runtime_info 当前支持的查询

后端实现位于 `internal/runtime/info.go`。

支持：
- `help`：列出可用 runtime info 与 slash command 概览
- `model`：查看当前模型识别状态（有限可见）
- `cost`：查看成本遥测接入状态（当前未接入真实 telemetry）
- `context`：查看当前 cwd / 会话 / 运行状态
- `doctor`：检查环境与连接信息

说明：
- `model` 与 `cost` 不是 Claude Code 官方真实状态透传
- `doctor` 为只读检查，不会主动启动 runner

---

## 前端界面

当前前端位于 `web/index.html`，以单文件 SPA 方式实现，主要包含：
- token 登录页
- 主聊天 / 命令输入区
- 文件抽屉
- 单文件查看页
- diff 徽标与 diff 审阅栏
- 状态详情面板
- 运行日志抽屉

前端使用：
- Tailwind CSS CDN
- `marked`
- `highlight.js`

不依赖额外前端构建流程，便于快速本地启动。

---

## 测试与验证

仓库内提供基础测试脚本：

```bash
./scripts/test_backend_commands.sh
```

该脚本会执行：

```bash
go test ./internal/ws -run Slash
go test ./...
```

启动服务后，建议优先验证以下链路：

1. Web 页面能正常连接
2. `/context`、`/doctor` 能返回结果
3. `pwd`、`ls`、`go test ./...` 等命令能正常执行
4. `claude` 或 `gemini` 会话能进入可交互状态
5. 文件抽屉能列目录
6. 单文件视图能读取文件内容
7. 有 diff 时前端能展示并回传 review decision
8. `/review`、`/analyze` 能基于当前上下文工作

---

## 当前限制

基于当前实现，可以明确看到这些限制：

- 单个 WebSocket 连接只维护一个活动 runner
- 默认安全策略更适合本地环境，不适合直接公网部署
- 前端是单 HTML 文件实现，后续功能继续扩展时维护成本会逐步上升
- `model` / `cost` 当前仍是有限可见信息，不是完整真实遥测
- 文件能力当前以浏览、读取、结合上下文发指令为主
- 会话持久化与更强的存储能力还没有完整落地

---

## 安全说明

当前实现更偏向本地开发工具，而不是安全加固后的远程平台。

需要特别注意：
- 连接成功后的客户端可以发起本地命令执行
- 目录浏览与文件读取能力默认没有严格白名单隔离
- WebSocket `CheckOrigin` 当前是宽松策略
- 如果本机 CLI 已登录 Claude / GitHub 等工具，前端实际上具备代操作能力

因此：

**请不要直接把服务暴露到公网。**

如果后续要增强安全性，至少应考虑：
- 更严格的 Origin 校验
- IP 白名单或反向代理鉴权
- 目录 / 文件访问范围限制
- 更细粒度的命令白名单
- 审计日志与操作确认机制

---

## 技术栈

### 后端
- Go
- `gorilla/websocket`
- `creack/pty`

### 前端
- HTML
- 原生 JavaScript
- Tailwind CSS
- `marked`
- `highlight.js`

---

## 最近能力演进

从当前仓库状态可以看到，近期演进主要集中在：
- 更完整的 runtime service 与 skill center
- Claude stream parsing 与更丰富的会话状态投影
- in-session skill invocation
- 更好的工具错误检测、活动状态反馈与 Web UI 展示
- 更完善的 slash command 与 runtime info 支持

---

## License

当前仓库尚未定义 License。
如果准备公开发布，建议补充 `LICENSE` 文件。
