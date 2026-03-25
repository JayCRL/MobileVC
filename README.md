# MobileVC

MobileVC 是一个把 **Claude Code 带到手机上** 的移动控制台。

它的目标很简单：当你离开电脑时，仍然可以通过手机浏览器或 Flutter 客户端，继续控制本机正在运行的 Claude Code 会话。

> 建议仅在本机或可信局域网中使用，不要直接暴露到公网。

---

## 它能做什么

MobileVC 当前主要支持：

- 在手机上连接本机 Claude Code
- 新建、恢复、切换历史会话
- 继续向当前会话发送指令
- 查看聊天时间线、运行日志和状态
- 浏览文件与下载本机文件
- 在移动端审阅 diff，并把决策回灌给 Claude
- 查看和管理 Skill / Memory / Session Context
- 通过独立 TTS 接口生成语音

---

## 最新功能更新

最近这轮更新，项目已经从“移动端聊天入口”进一步变成了更完整的 **Claude Code 移动工作台**。

### 1. 会话恢复更完整

现在恢复历史会话时，不再只是恢复聊天文本，还会尽量恢复完整工作现场，包括：

- session history
- pending / recent diff
- terminal execution 记录
- stdout / stderr 日志
- session context（已启用的 skills / memories）
- resume 元信息与 `canResume` 状态

这意味着你在手机上重新打开历史会话时，更容易直接接着上次工作继续操作。

### 2. 权限 prompt 与 diff review 已分成两条链路

当前移动端已经明确区分三类交互：

- 普通输入：继续向 Claude 发送文本
- permission prompt：发送 `permission_decision`
- review prompt：发送 `review_decision`

这解决了过去把等待态 prompt 当成普通输入的问题，尤其对 Claude Code 的文件修改授权和 diff 审核更重要。

同时，权限 prompt 已兼容中英文常见决策值，例如：

- `allow` / `deny`
- `允许` / `拒绝`
- `同意` / `取消`

### 3. 文件查看器升级为“查看 + 审核 + 继续提问”面板

Flutter 文件面板现在不只是看文件，还支持：

- 在普通文件内容与 diff 视图之间切换
- 查看待审核修改组与组内文件
- 对当前 diff 执行 accept / revert / revise
- 直接把当前文件作为上下文继续提问 Claude
- 在权限等待态下继续对当前文件做授权决策

### 4. 终端日志按命令拆分展示

移动端现在不仅显示原始 terminal log，还会记录每条 terminal execution，并展示：

- 命令标题
- cwd
- startedAt / endedAt
- exit code
- 分离的 `stdout`
- 分离的 `stderr`

这让你在手机上排查 runner 或脚本执行结果时更清楚。

### 5. Skill / Memory / Session Context 已升级为会话级上下文系统

这部分现在已经进入主交互链路，不再只是附属面板。

#### Skill 当前支持

- 查看当前 skill catalog
- 同步外部 skill（`skill_sync_pull`）
- 在移动端新增 / 编辑本地 skill
- 为 skill 定义 `description`、`prompt`、`targetType`、`resultView`
- 按会话启用 / 禁用 skill
- 在执行 skill 时携带 target / context / result view 等运行时元信息

#### Memory 当前支持

- 查看 memory 列表
- 在移动端新增 / 编辑 memory
- 按会话启用 / 禁用 memory
- 将 memory 作为当前 session 的显式上下文持久化保存

> 这里的 Memory 是 MobileVC 内部显式记忆层，不等同于 Claude CLI 的隐式 `/memory`。

#### Session Context 当前支持

- Skill 和 Memory 的启用状态会绑定到当前 session
- 不同 session 可以使用不同的 skill / memory 组合
- session 恢复时会一起带回启用状态

#### 持久化方式

Skill / Memory 目录会持久化到本地 store：

- `skills.catalog.json`
- `memory.catalog.json`

因此它们现在已经形成三层结构：

- **目录层**：有哪些可用 skill / memory
- **会话层**：当前 session 启用了哪些条目
- **运行时层**：skill 执行时会携带上下文元信息进入执行链路

### 6. 会话首页更接近完整控制台

当前首页已经整合了：

- 文件树抽屉
- Diff 入口与 pending 数量徽标
- Runtime / Status / Logs 快速入口
- Skill / Memory 管理入口
- 会话切换入口
- 连接配置入口

连接配置也可以直接在 App 内修改：

- host
- port
- token
- cwd
- engine
- permission mode

### 7. Android / Flutter 测试基建增强

移动端工程已经加入更完整的测试与集成测试基础设施：

- `integration_test`
- `patrol`
- `PatrolJUnitRunner`
- `ANDROIDX_TEST_ORCHESTRATOR`

这说明项目正在向更稳定的真机 / instrumentation 测试链路演进。

---

## 项目组成

这个仓库不是单一前端，而是一个多组件工程：

- `cmd/server/`：Go 服务入口
- `internal/`：Claude 会话编排、WebSocket 协议、运行时管理、会话存储、TTS 转发
- `web/`：浏览器工作台
- `mobile_vc/`：Flutter 客户端
- `sidecar/chattts/`：Python ChatTTS sidecar

整体链路如下：

```text
Mobile browser / Flutter app
          │
          ▼
   MobileVC Go server
          │
          ├─ WebSocket event stream
          │
          ├─ Claude Code runner / session runtime
          │
          └─ Python ChatTTS sidecar (optional)
```

其中最核心的设计是：**前端不是直接改文件，而是通过 WebSocket 接收结构化事件；diff 审核结果会被转换成新的输入，再送回当前 Claude 会话。**

---

## 3 分钟快速启动

### 1. 准备环境

你至少需要：

- Go
- Claude Code 已能在本机正常运行
- 一个你自己设置的 `AUTH_TOKEN`

例如：

```bash
export AUTH_TOKEN=test
```

### 2. 启动 Go 服务

在仓库根目录运行：

```bash
AUTH_TOKEN=test go run ./cmd/server
```

默认端口是 `8001`。

服务会注册：

- `/ws`
- `/healthz`
- `/download`
- `/`（静态 Web 工作台）

### 3. 检查服务是否正常

```bash
curl http://127.0.0.1:8001/healthz
```

预期返回：

```text
ok
```

### 4. 在浏览器打开

本机打开：

```text
http://127.0.0.1:8001/
```

如果要在手机上访问，把 `127.0.0.1` 替换成电脑的局域网 IP，例如：

```text
http://192.168.1.20:8001/
```

### 5. 输入 token 登录

输入你启动服务时使用的 `AUTH_TOKEN`，例如：

```text
test
```

### 6. 开始一个 Claude 会话

进入工作台后，直接输入：

```text
claude
```

这样就可以启动一个新的 Claude Code 会话。

---

## 最小可用启动方式

如果你只想尽快跑起来，下面这两步就够了：

```bash
cd /path/to/MobileVC
AUTH_TOKEN=test go run ./cmd/server
```

然后打开：

```text
http://127.0.0.1:8001/
```

登录后输入：

```text
claude
```

---

## 运行方式

### 方式一：只用 Go 服务 + Web 工作台

这是最直接的使用方式：

```bash
AUTH_TOKEN=test go run ./cmd/server
```

适合：

- 先快速验证项目是否可用
- 只使用浏览器工作台
- 暂时不启用 TTS

### 方式二：Go 服务 + TTS sidecar

如果你想启用语音合成，需要额外启动 Python sidecar。

### 方式三：连接 Flutter 客户端

仓库中也包含 Flutter 客户端工程：`mobile_vc/`。

已确认依赖包括：

- `web_socket_channel`
- `shared_preferences`
- `url_launcher`
- `path_provider`
- `file_picker`
- `share_plus`
- `flutter_markdown`

Flutter 客户端的职责不是单纯壳应用，而是移动端的完整控制面板，负责：

- 连接后端 WebSocket
- 拉取 session list、runtime info、skill catalog、memory list
- 维护会话状态与聊天时间线
- 展示文件、diff、日志和运行状态
- 在等待输入时区分普通输入、权限决策和 diff 审核决策
- 在文件面板中直接将当前文件作为上下文继续提问
- 管理本地 skill / memory 目录，并按会话启用对应上下文

### Flutter 客户端当前交互重点

如果你主要通过 Flutter 客户端使用 MobileVC，建议优先理解下面这几点：

- **文件树**：从首页左上角打开抽屉，浏览当前工作目录、切换目录、打开文件或下载文件。
- **文件查看器**：打开文件后，可以查看文件内容；如果该文件正处于待审核状态，也能直接切到 diff 视图完成审核。
- **继续提问**：在文件查看器中点击“继续提问”，可以把当前文件作为上下文继续操作当前 Claude 会话。
- **Skill 管理**：可以在移动端查看当前 skill catalog、切换当前会话启用的 skill、同步外部 skill，并新增 / 编辑本地 skill 定义。
- **Memory 管理**：可以在移动端查看 memory 列表、切换当前会话启用的 memory，并直接新增 / 编辑 memory 内容。
- **Session Context**：Skill 和 Memory 的启用状态是当前会话上下文的一部分，不同 session 可以拥有不同启用组合。
- **权限 prompt**：遇到 Claude Code 的授权提示时，客户端会把按钮操作映射到 `permission_decision`，而不是误发普通输入。
- **review prompt**：遇到 diff 审核提示时，客户端会把 accept / revert / revise 映射到 `review_decision`。
- **日志查看**：日志面板支持按命令切换，并分别查看 `stdout` 与 `stderr`。
- **连接设置**：可直接在客户端内调整 host、port、token、cwd、engine 与 permission mode，适合连接本机或局域网内后端。

---

## TTS 快速启动

当前 TTS 链路如下：

```text
Client -> MobileVC Go server -> Python ChatTTS sidecar
```

### 1. 安装 Python sidecar 依赖

```bash
cd sidecar/chattts
python3 -m venv .venv
source .venv/bin/activate
pip install -r requirements.txt
```

### 2. 启动 sidecar

回到仓库根目录运行：

```bash
PYTHON_BIN="$PWD/sidecar/chattts/.venv/bin/python" \
CHATTTS_MODEL_DIR="$HOME/.cache/mobilevc/chattts" \
bash sidecar/chattts/run.sh
```

默认配置：

- sidecar 地址：`http://127.0.0.1:9966`
- 模型缓存目录：`~/.cache/mobilevc/chattts`
- 默认采样率：`24000`

### 3. 启动 Go 服务并启用 TTS

```bash
AUTH_TOKEN=test \
TTS_ENABLED=true \
TTS_PROVIDER=chattts-http \
TTS_PYTHON_SERVICE_URL=http://127.0.0.1:9966 \
go run ./cmd/server
```

### 4. 检查 TTS 状态

检查 sidecar：

```bash
curl http://127.0.0.1:9966/healthz
```

检查 Go 侧 TTS API：

```bash
curl 'http://127.0.0.1:8001/api/tts/healthz?token=test'
```

### 5. 生成语音

```bash
curl -X POST 'http://127.0.0.1:8001/api/tts/synthesize?token=test' \
  -H 'Content-Type: application/json' \
  -d '{"text":"你好，欢迎使用 MobileVC。","format":"wav"}' \
  --output /tmp/mobilevc_tts.wav
```

---

## sidecar 运行模式

`sidecar/chattts/app.py` 当前支持三种模式：

- `auto`：优先尝试真实 ChatTTS，失败时回退到 mock
- `mock`：始终返回本地生成的测试 wav
- `chattts`：强制使用真实 ChatTTS，加载失败返回 `503`

例如：

```bash
SIDECAR_MODE=mock bash sidecar/chattts/run.sh
```

```bash
SIDECAR_MODE=chattts bash sidecar/chattts/run.sh
```

---

## 关于 ChatTTS 模型缓存

这个项目没有单独的 Go 侧模型下载接口。

这里的“模型准备”实际上发生在 Python sidecar 内：

- sidecar 初始化 `ChatTTS`
- `ChatTTS` 自行加载或准备模型
- 模型缓存到 `CHATTTS_MODEL_DIR`

默认缓存目录：

```text
~/.cache/mobilevc/chattts
```

这意味着：

- 模型文件不会提交到仓库
- Go 服务本身不直接管理模型文件
- TTS 是否可真实工作，取决于 sidecar 是否成功加载模型

---

## 关键接口

### 健康检查

```bash
curl http://127.0.0.1:8001/healthz
```

### WebSocket

```text
ws://127.0.0.1:8001/ws?token=test
```

### 下载本机文件

```bash
curl 'http://127.0.0.1:8001/download?token=test&path=/tmp/reply.wav' --output reply.wav
```

### TTS 健康检查

```bash
curl 'http://127.0.0.1:8001/api/tts/healthz?token=test'
```

### TTS 合成

```bash
curl -X POST 'http://127.0.0.1:8001/api/tts/synthesize?token=test' \
  -H 'Content-Type: application/json' \
  -d '{"text":"你好","format":"wav"}' \
  --output reply.wav
```

---

## 当前架构重点

如果你准备继续开发这个项目，最值得先理解的是下面几个模块：

### Go 服务入口

- `cmd/server/main.go`

负责：

- 加载配置
- 初始化 session store
- 注册 WebSocket、健康检查、下载和 TTS 路由
- 托管静态 Web 前端

### WebSocket 编排层

- `internal/ws/handler.go`

负责：

- token 鉴权
- action 分发
- 会话切换与恢复
- 技能目录、记忆目录、上下文同步
- 将 runtime / session / store / fs / TTS 等能力汇总到统一事件流

### 运行时主链路

- `internal/runtime/manager.go`
- `internal/runner/pty_runner.go`
- `internal/session/session.go`

负责：

- 启动 runner
- 管理当前 active 会话
- 将 Claude 输出映射为结构化事件
- 接收后续 input 并回灌给当前 PTY runner

### 事件协议

- `internal/protocol/event.go`

当前协议覆盖的事件包括：

- `log`
- `progress`
- `error`
- `prompt_request`
- `session_state`
- `agent_state`
- `file_diff`
- `session_list_result`
- `session_history`
- `skill_catalog_result`
- `memory_list_result`
- `session_context_result`

### Flutter 端状态中枢

- `mobile_vc/lib/features/session/session_controller.dart`

它是移动端最关键的状态协调层，负责：

- 连接后端
- 发送 exec / input / review / session / fs / skill / memory 等动作
- 维护聊天时间线
- 管理 pending diff、文件浏览和 runtime info

---

## 当前主要能力

MobileVC 现在更像一个“面向手机的 Claude Code 控制面板”，而不是单纯聊天窗口。

当前可用能力包括：

- Claude Code 会话启动与恢复
- 多 session 管理
- 移动端聊天时间线
- 文件浏览与下载
- diff 审阅与回灌决策
- terminal log / status / runtime info 展示
- Skill / Memory / Session Context 管理
- 可选 TTS HTTP 接口

---

## 常见问题

### 测试说明

当前仓库已经补充了较多与移动端交互相关的测试，重点覆盖：

- 权限 prompt / review prompt 分流
- 文件查看器与待审核 diff 行为
- terminal log 面板展示
- session controller 的状态同步
- Go 侧 PTY runner / session / WebSocket handler 行为

Flutter 测试请在 `mobile_vc/` 目录下执行，例如：

```bash
cd mobile_vc
flutter test
```

如果你要继续补移动端端到端或真机测试，可以进一步使用：

- `integration_test/`
- `patrol`
- Android instrumentation runner（Patrol）

---

## 常见问题

### 1. 页面打不开

先确认服务是否已启动：

```bash
curl http://127.0.0.1:8001/healthz
```

### 2. 手机上打不开

常见原因：

- 访问的不是电脑的局域网 IP
- 端口没有放通
- 电脑和手机不在同一个网络
- 服务启动后你填错了地址或端口

### 3. 登录失败

确认你输入的 token 与启动服务时使用的 `AUTH_TOKEN` 完全一致。

### 4. TTS healthz 是 ok，但没有真实语音

这通常表示 sidecar 在 `auto` 模式下回退到了 `mock`。

你可以检查：

```bash
curl http://127.0.0.1:9966/healthz
```

看返回里的 `backend` 是 `chattts` 还是 `mock`。

### 5. `SIDECAR_MODE=chattts` 返回 503

通常表示：

- `ChatTTS` 依赖没有正确安装
- 当前环境无法完成模型准备
- 模型尚未成功加载

---

## 目录结构

```text
cmd/server/           Go 服务入口
internal/             后端核心编排与协议
sidecar/chattts/      Python ChatTTS sidecar
web/                  浏览器工作台
mobile_vc/            Flutter 客户端工程
```

---

## 一句话总结

如果你只想马上用起来，请记住：

```bash
AUTH_TOKEN=test go run ./cmd/server
```

然后打开：

```text
http://127.0.0.1:8001/
```

登录后输入：

```text
claude
```

你就能在手机上继续控制本机的 Claude Code 会话。
