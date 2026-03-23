# MobileVC

MobileVC 是一个面向手机浏览器的本地 Web 控制台，用于**远程操控本机开发环境**。它把本地 Shell、Claude Code / Gemini CLI、运行状态、文件浏览和 diff 审阅投射到一个移动优先的网页里。

它不是远程桌面，也不是完整 IDE；定位是**轻量、便捷、可远程操控本机开发会话的移动控制台**。

> 安全建议：仅用于本机或可信局域网，不要直接暴露到公网。

---

## 项目目标

这个项目的核心目标不是“在手机上顺手接一下电脑上的工作”，而是：

- 让手机浏览器可以直接连接本机开发运行时
- 远程发起和控制本机命令执行
- 远程操控 Claude Code / Gemini CLI 会话
- 远程查看日志、步骤、diff、文件与运行状态
- 在离开工位时，仍然保有对本机开发链路的操作能力

换句话说，MobileVC 的定位是**移动端远程控制台**，而不是单纯的信息查看器。

---

## 快速开始

### 1. 启动服务

```bash
AUTH_TOKEN=test go run ./cmd/server
```

### 2. 打开页面

```text
http://127.0.0.1:8001/
```

### 3. 输入 token

```text
test
```

### 4. 开始远程操控

进入页面后，你可以直接：
- 输入 `claude`
- 输入 `gemini`
- 输入 `pwd`、`git status`、`go test ./...`
- 使用 `/build`、`/test`、`/review`、`/context` 等命令
- 查看文件、日志、diff 和会话状态

### 5. 健康检查

```text
http://127.0.0.1:8001/healthz
```

返回 `ok` 表示服务正常。

---

## 你可以用它做什么

- 在手机上连接本机 CLI 会话
- 启动或续接 `claude` / `gemini` 会话
- 执行命令，查看实时输出和步骤状态
- 查看最近 diff，并执行 `accept / revert / revise`
- 浏览目录、读取文件，在文件上下文继续发指令
- 查看运行时信息，如 `context`、`doctor`、`model`、`cost`
- 管理会话列表，创建、切换、恢复已保存会话

---

## 典型使用场景

### 1. 离开工位后继续操控 Claude Code
电脑上已经启动开发环境后，你可以在手机上打开 MobileVC，直接进入当前会话，继续发送提示词、查看步骤状态、处理确认请求，而不必回到电脑前。

### 2. 远程发起本机开发命令
你可以直接在手机上执行：
- `go test ./...`
- `git status`
- `git diff`
- `pwd`
- `/build`
- `/test`

适合做快速检查、构建验证和状态确认。

### 3. 查看并处理最近 diff
当本机 AI 会话产生改动后，你可以在手机上直接查看 diff，决定是继续调整、接受改动，还是撤销当前修改。

### 4. 在手机上检查运行状态
如果你想确认当前会话在做什么，可以通过运行日志、步骤更新、`/context`、`/doctor` 等信息快速判断：
- 当前是否还在运行
- 当前工作目录是什么
- 当前会话是否可继续
- 本机 CLI 环境是否正常

### 5. 浏览项目文件并基于文件继续发指令
你可以先浏览目录、读取单个文件，再基于当前文件继续下发指令，例如：
- 解释这个文件
- 继续修改这个文件
- 分析这里的报错来源

这类场景适合在移动端做快速定位和远程协作式控制。

---

## 核心设计

### 前端
前端是一个移动优先的单页页面：`web/index.html`

主要界面能力：
- token 登录
- 聊天 / 命令输入区
- 会话面板
- 文件抽屉与单文件查看
- diff 审阅栏
- 状态详情面板
- 运行日志抽屉
- 权限模式切换

### 后端
后端入口：`cmd/server/main.go`

后端主要提供：
- `GET /`：前端页面
- `GET /healthz`：健康检查
- `GET /ws?token=...`：WebSocket 通道

WebSocket 侧负责：
- token 校验
- action 分发
- runner 生命周期管理
- 会话投影持久化
- 文件读取与目录浏览
- slash command 解析
- skill 执行与 runtime info 返回

---

## 配置

配置加载文件：`internal/config/config.go`

### 必填
- `AUTH_TOKEN`：WebSocket 鉴权 token

### 常用
- `PORT`：HTTP 端口，默认 `8001`
- `RUNTIME_DEFAULT_COMMAND`：默认命令，默认 `claude`
- `RUNTIME_DEFAULT_MODE`：默认运行模式，默认 `pty`
- `RUNTIME_DEBUG`：是否开启调试输出
- `RUNTIME_WORKSPACE_ROOT`：运行时工作区根目录
- `RUNTIME_ENHANCED_PROJECTION`
- `RUNTIME_ENABLE_STEP_PROJECTION`
- `RUNTIME_ENABLE_DIFF_PROJECTION`
- `RUNTIME_ENABLE_PROMPT_PROJECTION`

---

## 功能清单

### 1. 命令执行
支持两类 runner：
- `exec`：适合一次性命令
- `pty`：适合持续交互场景

相关实现：
- `internal/runner/exec_runner.go`
- `internal/runner/pty_runner.go`

### 2. 会话管理
支持：
- 新建会话
- 会话列表
- 加载历史会话
- 恢复投影状态

相关实现：
- `internal/store/file_store.go`
- `internal/session/session.go`
- `internal/ws/handler.go`

### 3. 文件能力
支持：
- `fs_list`：列目录
- `fs_read`：读取文件

定位是**浏览和阅读**，不是完整在线编辑器。

### 4. Diff 审阅
支持展示 diff，并在前端发出：
- `accept`
- `revert`
- `revise`

### 5. Runtime Info
当前支持：
- `help`
- `model`
- `cost`
- `context`
- `doctor`

实现位置：`internal/runtime/info.go`

### 6. Skill Center
当前内置技能：
- `review`
- `analyze`
- `doctor`
- `simplify`
- `debug`
- `security-review`
- `explain-step`
- `next-step`

实现位置：`internal/skills/registry.go`

---

## Slash Command

后端当前已支持的 slash command 包括：

### 本地命令
- `/clear`
- `/exit`
- `/quit`
- `/fast`
- `/diff`

### Runtime Info
- `/help`
- `/context`
- `/model`
- `/cost`
- `/doctor`

### Skill
- `/review`
- `/analyze`

### 执行类
- `/init`
- `/memory`
- `/add-dir`
- `/plan`
- `/execute`
- `/compact`
- `/run`
- `/build`
- `/test`
- `/git status`
- `/git diff`
- `/git commit`
- `/git push`
- `/git pull`
- `/pr create`

实现位置：`internal/ws/slash_command.go`

说明：
- 本地命令由前端处理，不会全部交给后端执行
- 部分高风险命令带确认语义
- slash command 的最终执行仍受当前运行模式与会话状态影响

---

## 常用接口

### HTTP
- `GET /`
- `GET /healthz`

### WebSocket
- `GET /ws?token=...`

### 常见客户端 action
- `session_create`
- `session_list`
- `session_load`
- `exec`
- `input`
- `review_decision`
- `skill_exec`
- `runtime_info`
- `slash_command`
- `fs_list`
- `fs_read`

### 常见服务端事件
- `session_state`
- `session_list_result`
- `session_created`
- `session_history`
- `agent_state`
- `log`
- `progress`
- `error`
- `prompt_request`
- `step_update`
- `file_diff`
- `runtime_info_result`
- `fs_list_result`

---

## 项目结构

```text
MobileVC/
├─ cmd/server/               # HTTP 服务入口
├─ internal/
│  ├─ adapter/               # 终端输出适配与 ANSI 解析
│  ├─ config/                # 配置加载
│  ├─ protocol/              # 前后端事件协议
│  ├─ runner/                # exec / pty 执行与 CLI 封装
│  ├─ runtime/               # 运行时服务与 runtime info
│  ├─ session/               # 会话状态整理与 diff 上下文
│  ├─ skills/                # 技能注册与 prompt 构建
│  ├─ store/                 # 会话存储
│  └─ ws/                    # WebSocket handler 与 action 分发
├─ scripts/                  # 辅助脚本
├─ web/index.html            # 移动优先前端
├─ go.mod
└─ README.md
```

---

## 关键文件

- 服务入口：`cmd/server/main.go`
- 默认端口配置：`internal/config/config.go`
- WebSocket 入口：`internal/ws/handler.go`
- slash command：`internal/ws/slash_command.go`
- runtime info：`internal/runtime/info.go`
- skill 注册：`internal/skills/registry.go`
- 前端页面：`web/index.html`

---

## 适合的使用方式

MobileVC 更适合下面这种日常工作流：
- 电脑上启动本地开发环境
- 手机上查看当前 AI / Shell 是否还在运行
- 继续发命令或补一句提示词
- 快速看日志、看 diff、看文件
- 在离开工位时维持开发链路不断开

如果你的目标是“随时接一下当前工作，而不是完整搬到手机上开发”，这个项目就是为这个场景设计的。