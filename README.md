# MobileVC

MobileVC 是一个面向手机浏览器的本地 Vibe Coding 面板。

它把一个本地运行的 Go 服务包装成“移动端可用的命令执行 + 交互输入 + 文件树工作区 + 结构化日志”工具。你可以直接在手机浏览器打开页面，输入 token 后连接到电脑上的服务，在指定目录中执行命令、接收输出、响应交互提示，并以更接近聊天 + 终端混合的方式使用 Shell / AI CLI。

---

# 1. 项目目标

本项目的目标不是做一个完整桌面 IDE，而是提供一个：

- 轻量
- 本地优先
- 手机可访问
- 支持 PTY 交互
- 支持工作区切换
- 支持结构化日志与输入提示
- 可承载 AI CLI / Shell 工具

的 Vibe Coding 面板。

它适合的场景包括：
- 在手机上远程操作你自己电脑上的本地开发环境
- 通过浏览器运行 Shell / Python / Claude Code 等 CLI 工具
- 处理交互式命令
- 快速查看目录并切换工作区
- 在无需完整桌面远控的情况下完成轻量开发操作

---

# 2. 当前整体能力

## 2.1 后端能力

### HTTP 服务
- `GET /healthz`
  - 健康检查
  - 返回 `200 OK`
- `GET /`
  - 托管前端静态页面 `web/index.html`
- `GET /ws`
  - WebSocket 入口
  - 通过 `?token=...` 做鉴权

### WebSocket 协议能力
后端支持以下前端上行 action：
- `exec`
- `input`
- `fs_list`

后端支持以下下行 event：
- `session_state`
- `agent_state`
- `log`
- `error`
- `prompt_request`
- `step_update`
- `file_diff`
- `fs_list_result`

### 命令执行能力
- 支持 `exec` 模式执行普通命令
- 支持 `pty` 模式执行交互式命令
- 支持 `cwd` 指定工作目录
- 单个连接同一时刻只允许一个活动命令

### 交互输入能力
- 可向 PTY 会话写入输入
- 支持普通文本输入
- 支持中断信号类输入，例如 `Ctrl+C`

### 文件系统能力
- 支持浏览目录
- 返回绝对路径
- 返回文件与文件夹列表
- 文件夹优先排序
- 支持前端基于目录切换后再执行命令

### 输出结构化能力
- 普通输出流按 `log` 下发
- 错误下发为 `error`
- 交互提示识别后下发为 `prompt_request`
- Agent/Claude 会话状态按 `agent_state` 下发
- 步骤状态按 `step_update` 下发
- 文件变更按 `file_diff` 下发
- 会话生命周期变化下发为 `session_state`

### 异常信息提炼能力
- 对 Python Traceback / Java 异常栈做基础识别
- 尝试把多行错误聚合成一个结构化 `error` 事件

---

## 2.2 前端能力

### 移动端深色 UI
- 单文件页面：`web/index.html`
- 原生 HTML + 原生 JS
- Tailwind CDN
- 手机优先布局
- 深色主题

### 登录与连接
- 首次进入弹出 token 输入框
- token 保存在 `localStorage.vibe_token`
- 自动建立 WebSocket 连接
- 支持重连与重新输入 token

### 聊天式展示
- 用户命令 / 用户输入：右侧气泡
- 服务端输出 / Markdown / 错误：左侧区域
- 系统事件：居中显示
- Claude/Agent 会话支持顶部状态驱动的连续交互

### 双视图日志展示
- 支持 `结构化视图`
- 支持 `原始终端` 视图
- 两种视图共享同一份 WebSocket 日志流
- 切换视图不会丢失日志、输入状态与增强 UI 状态

### 终端式日志展示
- 普通日志使用 `<pre>` + 等宽字体显示
- 连续同 stream 输出自动合并
- 避免“每一行一个气泡”的碎片化效果
- 原始终端视图会复用 carriage-return / transient line 处理逻辑

### Markdown / AI 输出展示
- 使用 `marked` 做 Markdown 渲染
- 使用 `highlight.js` 做代码高亮
- 代码块带复制按钮
- 长文本 / Markdown 风格输出会被前端启发式渲染为 AI 卡片

### 错误卡
- 红色错误卡展示
- 支持复制错误信息
- 有 `stack` 时支持折叠查看

### 交互输入卡
- 收到 `prompt_request` 后弹出底部输入卡
- 支持手动输入
- 支持快捷按钮输入（如 `y` / `n`）
- Claude 连续对话会在每轮回复后重新回到可输入态

### 当前步骤与 Diff 增强
- 顶部 step panel 展示 `step_update`
- 文件树可根据 `step_update.target` 高亮文件
- `file_diff` 会弹出 diff modal
- 这些增强层在结构化视图与原始终端视图下都可用

### 快捷动作盘
- `Ctrl+C 中断`
- `清屏`
- `文件树`
- 动态 prompt 选项按钮

### 文件树侧栏
- 左上角 `📁` 按钮打开侧栏
- 带遮罩层与滑出动画
- 支持刷新、返回上一级
- 显示当前工作区路径
- 支持点击文件夹进入子目录

### 工作区联动执行
- 当前目录保存在 `currentCwd`
- 执行命令时自动把 `cwd` 传给后端
- 命令在当前所选目录下执行

---

# 3. 前端完整功能说明

前端核心文件：
- `web/index.html`

## 3.1 页面布局
页面主要分为四块：

1. 顶部状态栏
2. 中间聊天 / 日志区域
3. 左侧文件树抽屉
4. 底部操作区（动作盘 + 输入卡 + 命令框）

## 3.2 顶部状态栏功能
- 打开文件树
- 展示当前工作区路径
- 展示连接状态
- 展示当前会话 ID
- 重新输入 token
- 重连服务

## 3.3 日志区域功能

### 用户侧消息
当用户执行命令或发送输入时：
- 右侧显示用户气泡
- 标记来源如“命令”“回复”“快捷动作”

### 服务端普通日志
当收到 `type: log` 且为普通文本输出时：
- 以终端块方式展示
- `<pre>` 保留换行
- 自动合并连续同类输出
- stderr 使用偏红色样式
- stdout 使用正常深色终端样式

### 服务端 Markdown / AI 风格输出
当前前端会根据输出内容做启发式识别：
- 如果像 Markdown / 代码块 / 长 AI 回复，则按 Markdown 卡片渲染
- 代码块会进行语法高亮
- 每个代码块右上角有复制按钮

### 错误输出
当收到 `type: error` 时：
- 渲染为红色错误卡
- 支持复制按钮
- 如果有 `stack`，使用 `<details>` 展开

### 系统状态
当收到 `session_state` 时：
- 居中显示系统消息
- 例如：connected / command started / command finished
- 同步影响页面按钮启用状态

## 3.4 输入卡功能
当收到 `prompt_request` 时：
- 底部输入卡弹出
- 显示 prompt 文案
- 如果有 `options`，生成快捷按钮
- 用户可以点击快捷按钮，也可以手动输入
- 输入通过 `input` action 发回后端

## 3.5 快捷动作盘功能
Action Panel 当前支持：
- `Ctrl+C 中断`
- `清屏`
- `文件树`
- prompt 动态选项按钮

这部分主要解决手机输入不方便的问题。

## 3.6 文件树侧边栏功能
侧边栏支持：
- 展示当前目录
- 展示目录内文件列表
- 文件夹在前，文件在后
- 文件夹使用 `📁`
- 文件使用 `📄`
- 点击文件夹进入下一层
- 点击“上一级”返回父目录
- 点击“刷新”重新读取当前目录

## 3.7 执行命令功能
底部命令框支持：
- 输入一条命令
- 点击运行发送到后端
- 自动带上当前 `cwd`
- 命令执行中禁用重复提交

发送格式：

```json
{
  "action": "exec",
  "mode": "pty",
  "cmd": "pwd",
  "cwd": "D:/MobileVC"
}
```

---

# 4. 后端完整功能说明

后端入口：
- `cmd/server/main.go`

后端核心职责：
- 读取配置
- 启动 HTTP 服务
- 提供健康检查
- 托管前端静态页面
- 提供 WebSocket 协议处理
- 将命令执行能力、文件系统能力和结构化事件能力暴露给前端

## 4.1 配置加载
文件：
- `internal/config/config.go`

当前支持环境变量：
- `PORT`
  - 默认 `8080`
- `AUTH_TOKEN`
  - 必填

如果未设置 `AUTH_TOKEN`，服务启动失败。

## 4.2 HTTP 路由
文件：
- `cmd/server/main.go`

当前路由：
- `/healthz`
- `/ws`
- `/`

说明：
- `/ws` 使用 WebSocket handler
- `/healthz` 返回健康状态
- `/` 使用 `http.FileServer` 托管 `./web`

## 4.3 WebSocket 鉴权
文件：
- `internal/ws/handler.go`

行为：
- 从查询参数中读取 `token`
- 与配置中的 `AUTH_TOKEN` 比较
- 不一致返回 `401 unauthorized`

## 4.4 WebSocket 会话管理
当前实现中：
- 每次连接会生成一个 `sessionID`
- 每个连接维护一个事件写入 goroutine
- 每个连接最多允许一个 active runner
- 连接关闭时会自动清理 active runner

## 4.5 命令执行模式
文件：
- `internal/runner/runner.go`
- `internal/runner/exec_runner.go`
- `internal/runner/pty_runner.go`

支持两种模式：
- `exec`
- `pty`

### `exec` 模式
特征：
- 普通命令执行
- 使用 stdout / stderr pipe 读取输出
- 不支持运行中输入
- `Write()` 会返回 `ErrInputNotSupported`

### `pty` 模式
特征：
- 适合交互式命令
- 通过 PTY 启动进程
- 支持运行中输入
- 支持 prompt 检测
- 支持更接近真实终端的行为
- 在 Windows 下对 `claude` 命令做了专门桥接，避免 npm 版 Claude Code 的重 TUI 输出无法稳定映射到前端
- Claude 会话支持连续对话：每轮通过机器可读输出获取回复，并复用 Claude session 上下文

当前前端主要走 `pty` 模式。

## 4.6 Runner 抽象
文件：
- `internal/runner/runner.go`

抽象接口：

```go
type Runner interface {
    Run(ctx context.Context, req ExecRequest, sink EventSink) error
    Write(ctx context.Context, data []byte) error
    Close() error
}
```

作用：
- 统一命令执行接口
- 让 handler 只关注协议，不关心具体执行实现
- 支持在 `exec` / `pty` 间切换

## 4.7 命令执行生命周期
后端在执行命令时大致流程如下：

1. 前端发送 `exec`
2. handler 校验：
   - JSON 格式
   - cmd 非空
   - mode 合法
   - 当前没有活动命令
3. handler 创建 runner
4. runner 发送 `session_state(command started)`
5. runner 执行命令
6. 持续将输出转换成事件下发
7. 命令结束后：
   - 成功 -> `session_state(closed, command finished)`
   - 失败 -> `error` + `session_state(closed, command finished with error)`

## 4.8 输出解析架构
文件：
- `internal/adapter/generic_parser.go`
- `internal/adapter/ansi.go`

### ANSI 处理
PTY 输出先经过 ANSI 清理：
- 去掉颜色控制字符
- 降低前端渲染负担

### GenericParser
当前通用解析器支持：
- 普通行输出 -> `log`
- Python Traceback 聚合 -> `error`
- Java 异常栈聚合 -> `error`

作用：
- 把原本一行行原始 stdout/stderr，转换为更适合 UI 展示的结构化事件

## 4.9 Prompt 检测能力
文件：
- `internal/runner/pty_runner.go`

当前 PTY runner 在处理未换行的尾部输出时，会尝试识别 prompt：
- `[y/N]`
- `[Y/n]`
- `(y/n)`
- `(Y/n)`
- 以及带 `continue` / `confirm` / `password` / `input` / `select` 关键词的 `?` / `:` 提示

识别成功后：
- 发送 `prompt_request`
- 附带可能的快捷选项，如 `y/n`

## 4.10 文件系统接口架构
文件：
- `internal/ws/handler.go`
- `internal/protocol/event.go`

当前后端支持 `fs_list`：
- 读取请求 path
- 清理路径 `filepath.Clean`
- 转绝对路径 `filepath.Abs`
- 使用 `os.ReadDir` 读取目录
- 组装为 `FSListResultEvent`
- 文件夹优先排序

目前它是一个“通用目录读取能力”，还没有目录白名单限制。

## 4.11 会话模型
文件：
- `internal/session/session.go`

当前定义了三种会话状态：
- `active`
- `hibernating`
- `closed`

目前真实使用最多的是：
- `active`
- `closed`

## 4.12 存储接口
文件：
- `internal/store/store.go`

当前只定义了接口：

```go
type Store interface {
    SaveSession(ctx context.Context, sess session.Session) error
    GetSession(ctx context.Context, sessionID string) (session.Session, error)
}
```

说明：
- 这是为未来会话持久化预留的抽象
- 当前主流程里还没有真正接入具体实现

---

# 5. 后端架构说明

当前后端可以理解为五层结构。

## 5.1 入口层
文件：
- `cmd/server/main.go`

职责：
- 启动服务
- 加载配置
- 组装路由

## 5.2 协议层
文件：
- `internal/protocol/event.go`

职责：
- 定义 WebSocket 上下行事件结构
- 统一字段命名与 JSON 格式
- 作为前后端通信契约

## 5.3 传输 / Handler 层
文件：
- `internal/ws/handler.go`

职责：
- WebSocket 升级与鉴权
- 读取客户端 action
- 分发到具体逻辑
- 把结构化事件写回前端
- 控制单连接的 active runner 生命周期

## 5.4 执行层
文件：
- `internal/runner/*.go`

职责：
- 真正执行命令
- 处理 stdout/stderr/pty
- 处理运行时输入
- 产出事件

## 5.5 适配 / 解析层
文件：
- `internal/adapter/*.go`

职责：
- 处理 ANSI 清理
- 聚合错误栈
- 将原始输出适配为前端更容易消费的结构化事件

这种分层的好处是：
- handler 不直接关心命令怎么跑
- runner 不直接关心 WebSocket
- parser 不直接关心页面展示
- 协议结构独立，前后端耦合面可控

---

# 6. 通信协议完整说明

## 6.1 前端 -> 后端

### `exec`

```json
{
  "action": "exec",
  "mode": "pty",
  "cmd": "python app.py",
  "cwd": "D:/MobileVC"
}
```

字段：
- `action`: 固定 `exec`
- `mode`: `exec` 或 `pty`
- `cmd`: 要执行的命令
- `cwd`: 可选，工作目录

### `input`

```json
{
  "action": "input",
  "data": "hello\n"
}
```

字段：
- `action`: 固定 `input`
- `data`: 原始输入数据

### `fs_list`

```json
{
  "action": "fs_list",
  "path": "."
}
```

字段：
- `action`: 固定 `fs_list`
- `path`: 目录路径；空或 `.` 表示当前启动目录

## 6.2 后端 -> 前端

### `session_state`
表示会话状态变化。

示例：

```json
{
  "type": "session_state",
  "sessionId": "session-123",
  "state": "active",
  "msg": "command started"
}
```

### `log`
表示普通输出。

示例：

```json
{
  "type": "log",
  "sessionId": "session-123",
  "stream": "stdout",
  "msg": "Hello World"
}
```

### `error`
表示结构化错误。

示例：

```json
{
  "type": "error",
  "sessionId": "session-123",
  "msg": "command exited with code 1",
  "stack": "..."
}
```

### `prompt_request`
表示后端判断命令当前在等待用户输入。

示例：

```json
{
  "type": "prompt_request",
  "sessionId": "session-123",
  "msg": "Proceed? [y/N]",
  "options": ["y", "n"]
}
```

### `fs_list_result`
表示文件树结果。

示例：

```json
{
  "type": "fs_list_result",
  "sessionId": "session-123",
  "current_path": "D:\\MobileVC",
  "items": [
    {"name": "cmd", "is_dir": true, "size": 0},
    {"name": "README.md", "is_dir": false, "size": 2048}
  ]
}
```

---

# 7. 目录结构与模块职责

```text
MobileVC/
├─ cmd/
│  └─ server/
│     └─ main.go
├─ internal/
│  ├─ adapter/
│  │  ├─ ansi.go
│  │  ├─ generic_parser.go
│  │  └─ *_test.go
│  ├─ config/
│  │  └─ config.go
│  ├─ protocol/
│  │  └─ event.go
│  ├─ runner/
│  │  ├─ runner.go
│  │  ├─ exec_runner.go
│  │  ├─ pty_runner.go
│  │  └─ *_test.go
│  ├─ session/
│  │  └─ session.go
│  ├─ store/
│  │  └─ store.go
│  └─ ws/
│     ├─ handler.go
│     └─ handler_test.go
├─ web/
│  └─ index.html
└─ README.md
```

## 模块职责概览

### `cmd/server`
服务启动入口。

### `internal/config`
环境变量配置加载。

### `internal/protocol`
协议结构定义层，前后端共享契约来源。

### `internal/ws`
WebSocket 传输与 action 分发层。

### `internal/runner`
命令执行层，屏蔽 exec / pty 差异。

### `internal/adapter`
输出适配和错误聚合层。

### `internal/session`
会话状态模型。

### `internal/store`
存储抽象接口，当前预留未来扩展。

### `web`
前端单页应用。

---

# 8. 运行流程说明

## 8.1 启动流程
1. 读取环境变量配置
2. 检查 `AUTH_TOKEN`
3. 注册 HTTP 路由
4. 启动监听端口

## 8.2 页面初始化流程
1. 浏览器打开 `/`
2. 加载 `web/index.html`
3. 读取本地缓存 token
4. 建立 WebSocket 连接
5. 连接成功后自动请求 `fs_list('.')`
6. 更新文件树与工作区路径

## 8.3 执行命令流程
1. 用户在底部输入命令
2. 前端发送 `exec`
3. 后端校验命令与模式
4. 创建 runner 并执行
5. 输出持续转换为 `log` / `error` / `prompt_request`
6. 页面渲染结果
7. 命令结束后发送 `session_state.closed`

## 8.4 交互输入流程
1. 后端检测 prompt
2. 下发 `prompt_request`
3. 前端弹出输入卡
4. 用户点击快捷按钮或手动输入
5. 前端发送 `input`
6. 后端写入活动 PTY

## 8.5 文件树切换流程
1. 前端发送 `fs_list(path)`
2. 后端读取目录并返回 `fs_list_result`
3. 前端刷新侧边栏列表
4. 更新 `currentCwd`
5. 后续命令自动在该目录执行

---

# 9. 本地启动与使用

## 9.1 启动服务

```bash
AUTH_TOKEN=test PORT=8080 go run ./cmd/server
```

## 9.2 打开页面

```text
http://127.0.0.1:8080/
```

## 9.3 输入 token

```text
test
```

## 9.4 常见操作

### 浏览目录
- 点击左上角 `📁`
- 进入目录 / 返回上一级 / 刷新

### 执行命令
例如：

```bash
pwd
```

```bash
ls
```

```bash
python -c "print('hello')"
```

### 测试交互输入

```bash
printf 'Input your name: '; read name; printf 'Hello, %s\n' "$name"
```

### 测试错误链路

```bash
command_that_does_not_exist_12345
```

---

# 10. 已知限制与当前边界

## 10.1 安全边界仍然偏宽
当前实现是本地工具优先，尚未做更严格限制：
- `fs_list` 可以读取服务进程权限范围内的目录
- `exec` 可以执行传入命令
- `CheckOrigin` 当前直接返回 `true`

这意味着它适合：
- 本机使用
- 可信局域网环境
- 自己控制的机器

不适合直接暴露到公网。

## 10.2 Markdown 判断仍是启发式
当前前端会根据输出内容猜测是不是 AI / Markdown 输出，而不是由后端显式告诉前端 `format: markdown`。

## 10.3 文件树目前只支持浏览目录
当前文件树：
- 可以进入目录
- 可以切换 cwd
- 不能直接预览文件内容
- 不能编辑文件

## 10.4 单连接单活动命令
一个 WebSocket 连接当前只支持一个活动命令。

## 10.5 Prompt 检测仍基于规则
例如：
- `Input your name:` 较容易识别
- `Enter your name:` 这类提示不一定总能识别为 `prompt_request`

---

# 11. 安全建议

如果你准备长期使用，建议至少补这些能力：

- 自定义强 token，不要长期使用 `test`
- 限制服务监听范围
- 限制可访问目录的白名单
- 增加 Origin / Host 校验
- 增加命令执行白名单或运行隔离
- 增加操作审计日志

---

# 12. 测试与验证

建议开发阶段执行：

```bash
gofmt -w ./cmd/server/main.go ./internal/protocol/event.go ./internal/ws/handler.go
go test ./...
go build ./...
```

已验证过的关键链路包括：
- `/healthz`
- `/`
- WebSocket token 鉴权
- PTY 命令执行
- prompt_request -> input 闭环
- error 事件渲染
- 文件树目录拉取与 cwd 联动

---

# 13. 当前默认访问信息

如果按当前方式启动：

```bash
AUTH_TOKEN=test PORT=8080 go run ./cmd/server
```

则：
- 页面地址：`http://127.0.0.1:8080/`
- token：`test`

---

# 14. 后续推荐增强方向

建议优先级从高到低：

1. 文件预览
   - 点击文件直接查看内容

2. 后端显式输出格式字段
   - 如 `format: text | markdown`
   - 让前端不再靠启发式猜测

3. 更稳的 prompt 检测
   - 覆盖更多 CLI 的交互提示模式

4. 会话持久化
   - 利用 `store` 抽象真正保存 session

5. 更严格的安全控制
   - cwd 白名单
   - 命令执行限制
   - Origin 校验

6. 多会话 UI
   - 支持一个页面管理多个独立命令会话
# MobileVC
