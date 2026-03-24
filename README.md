222# MobileVC

MobileVC 是一个面向手机浏览器的**Claude Code 移动控制台**。

它的核心目标非常明确：

> **让你在手机上远程控制电脑上的 Claude Code，持续进行 vibe coding。**

你不需要把桌面搬到手机上，也不需要在手机上复刻完整 IDE。MobileVC 做的是更有价值的事情：把电脑上的 Claude Code 会话、运行状态、文件上下文、diff 审阅和会话恢复能力，整理成一个适合手机操作的工作台。

也就是说，这个项目不是“手机看一眼日志”的辅助页，而是一个围绕 **Claude Code 工作流** 设计的移动端控制界面。

> 建议仅在本机或可信局域网内使用，不要直接暴露到公网。

---

## 项目定位

MobileVC 的定位不是远程桌面，也不是传统 Web IDE。

它是：

- **手机上的 Claude Code 控制台**
- **面向 vibe coding 的移动端操作台**
- **离开电脑后继续指挥本机开发会话的工作台**

它解决的核心问题是：

- Claude Code 正在电脑上跑，但人不在电脑前
- 你想继续发指令、看状态、看 diff、看文件
- 你想在手机上继续推进开发，而不是等回到工位再处理

所以它的本质是：

**把电脑上的 Claude Code 工作流接管到手机端。**

---

## 核心价值

### 1. 在手机上继续 Claude Code 会话
当电脑上的 Claude Code 正在工作时，你可以在手机上：

- 查看当前会话状态
- 继续发送输入
- 处理等待确认的步骤
- 查看最近步骤与错误
- 恢复历史会话继续推进

### 2. 用手机处理 vibe coding 里的高频动作
MobileVC 重点承接的是 vibe coding 中最常见、最需要“随时接一下”的动作：

- 看 Claude 当前在干什么
- 看最近改了哪些内容
- 读某个文件
- 判断当前 diff 是否接受
- 切换会话
- 恢复上次上下文
- 快速诊断现在卡在哪

### 3. 用界面承接上下文，而不是全靠记命令
当前前端已经提供了适合手机使用的工作区界面，包括：

- token 登录页
- 主输入区
- 会话面板
- Skill 管理面板
- Memory 管理面板
- 文件抽屉
- 单文件查看页
- diff 审阅栏
- 运行日志抽屉
- 状态详情面板
- 权限模式切换
- 重连 / 恢复入口

所以 MobileVC 的重点不是“在手机上手敲一堆命令”，而是把 Claude Code 工作流中的关键上下文做成可直接操作的移动界面。

---

## 快速开始

### 1. 启动服务

```bash
AUTH_TOKEN=test go run ./cmd/server
```

默认端口：`8001`

### 2. 在手机浏览器打开

```text
http://127.0.0.1:8001/
```

### 3. 输入 token

```text
test
```

### 4. 开始控制 Claude Code
进入工作台后，你可以：

- 打开会话面板，进入历史 Claude 会话
- 恢复可恢复会话
- 查看 Claude 当前步骤、错误和状态
- 打开文件抽屉浏览项目文件
- 查看 diff 并决定 accept / revert / revise
- 查看运行日志
- 切换权限模式
- 继续向当前会话输入内容

如果你需要新起会话，也可以直接输入：

```text
claude
```

### TTS 语音合成接口

MobileVC 现已支持最小 REST TTS 能力：`POST /api/tts/synthesize`。

它的工作方式是：

- Go 服务接收文本请求
- Go 服务通过本地 HTTP 调用独立运行的 Python ChatTTS sidecar
- Python sidecar 直接返回 `audio/wav` 音频 bytes
- MobileVC 再把音频流直接返回给客户端

#### Python sidecar 目录

仓库内已内置最小 sidecar 子项目：`sidecar/chattts`

包含文件：

- `sidecar/chattts/app.py`
- `sidecar/chattts/requirements.txt`
- `sidecar/chattts/run.sh`

#### Python 版本与安装

建议使用 Python `3.10+`，并在 sidecar 目录内创建虚拟环境：

```bash
cd sidecar/chattts
python3 -m venv .venv
source .venv/bin/activate
pip install -r requirements.txt
```

当前 `requirements.txt` 已直接包含：

- `Flask`
- `numpy`
- `ChatTTS`

`torch`、`transformers`、`vocos` 等运行依赖会通过 `ChatTTS` 安装链自动解决。

#### 模型下载与缓存目录

这个项目**没有单独的 Go 侧模型下载接口**。

这里所说的“下载模型”，实际含义是：Python sidecar 在初始化 `ChatTTS` 时，由 `ChatTTS` 自身去准备模型文件。默认情况下，仓库通过 `CHATTTS_MODEL_DIR` 把模型缓存放到仓库外目录，避免把大模型权重写进 git。

默认缓存目录：

```text
~/.cache/mobilevc/chattts
```

你也可以显式指定：

```bash
CHATTTS_MODEL_DIR="$HOME/.cache/mobilevc/chattts" bash sidecar/chattts/run.sh
```

如果你希望在正式启动前预拉取模型，可以直接执行一次严格模式健康检查，或让 sidecar 以 `chattts` 模式启动；首次启动会触发模型准备。

#### sidecar 运行模式

通过 `SIDECAR_MODE` 控制 sidecar 行为：

- `mock`：始终返回本地生成的可播放蜂鸣音 wav
- `auto`：优先尝试真实 ChatTTS，失败时自动回退到 mock，默认值
- `chattts`：强制使用真实 ChatTTS，初始化失败时 `/healthz` 返回 `503`，`/synthesize` 也返回 `503`

#### sidecar 启动

```bash
PYTHON_BIN="$PWD/sidecar/chattts/.venv/bin/python" \
CHATTTS_MODEL_DIR="$HOME/.cache/mobilevc/chattts" \
bash sidecar/chattts/run.sh
```

支持的环境变量：

- `SIDECAR_HOST`：默认 `127.0.0.1`
- `SIDECAR_PORT`：默认 `9966`
- `SIDECAR_MODE`：默认 `auto`
- `CHATTTS_SAMPLE_RATE`：默认 `24000`
- `CHATTTS_MODEL_DIR`：默认 `~/.cache/mobilevc/chattts`
- `PYTHON_BIN`：默认 `python3`

例如强制 mock 模式：

```bash
SIDECAR_MODE=mock bash sidecar/chattts/run.sh
```

例如强制真实 ChatTTS 模式：

```bash
SIDECAR_MODE=chattts bash sidecar/chattts/run.sh
```

#### Python sidecar 协议

Go 侧默认按如下协议调用本机 sidecar：

- `GET /healthz`
- `POST /synthesize`

`/synthesize` 请求体示例：

```json
{
  "text": "你好，欢迎使用 MobileVC。",
  "format": "wav"
}
```

期望 sidecar 响应：

- `Content-Type: audio/wav`
- body 直接为 wav 音频 bytes

#### sidecar 单独验证

```bash
curl http://127.0.0.1:9966/healthz
curl -X POST http://127.0.0.1:9966/synthesize \
  -H 'Content-Type: application/json' \
  -d '{"text":"你好，欢迎使用 MobileVC。","format":"wav"}' \
  --output /tmp/reply.wav
```

成功时：

- `/healthz` 返回 `200`
- `/synthesize` 返回 `audio/wav`
- `/tmp/reply.wav` 可播放

如果你在 `chattts` 模式下尚未安装真实模型依赖，或当前网络无法拉取 `ChatTTS` 模型，则 sidecar 会明确返回 `503`。

#### 环境变量

在原有环境变量之外，新增：

- `TTS_ENABLED`：是否启用 TTS，默认 `false`
- `TTS_PROVIDER`：提供方标识，默认 `chattts-http`
- `TTS_PYTHON_SERVICE_URL`：Python sidecar 地址，默认 `http://127.0.0.1:9966`
- `TTS_REQUEST_TIMEOUT_SECONDS`：请求超时秒数，默认 `30`
- `TTS_MAX_TEXT_LENGTH`：最大文本长度，默认 `200`
- `TTS_DEFAULT_FORMAT`：默认音频格式，当前仅支持 `wav`

#### 启动示例

先启动 Python ChatTTS sidecar，再启动 MobileVC：

```bash
AUTH_TOKEN=test \
TTS_ENABLED=true \
TTS_PROVIDER=chattts-http \
TTS_PYTHON_SERVICE_URL=http://127.0.0.1:9966 \
TTS_REQUEST_TIMEOUT_SECONDS=30 \
TTS_MAX_TEXT_LENGTH=200 \
TTS_DEFAULT_FORMAT=wav \
go run ./cmd/server
```

#### Go 服务联调验证

```bash
curl 'http://127.0.0.1:8001/api/tts/healthz?token=test'
curl -X POST 'http://127.0.0.1:8001/api/tts/synthesize?token=test' \
  -H 'Content-Type: application/json' \
  -d '{"text":"你好，欢迎使用 MobileVC。","format":"wav"}' \
  --output /tmp/mobilevc_tts.wav
```

成功时：

- MobileVC `/api/tts/healthz` 返回 `200`
- MobileVC `/api/tts/synthesize` 返回 `200`
- `Content-Type: audio/wav`
- `/tmp/mobilevc_tts.wav` 可播放

#### 健康检查

```bash
curl 'http://127.0.0.1:8001/api/tts/healthz?token=test'
```

如果 Python sidecar 正常，返回类似：

```json
{"provider":"chattts-http","status":"ok"}
```

#### 调用示例

使用 query token：

```bash
curl -X POST 'http://127.0.0.1:8001/api/tts/synthesize?token=test' \
  -H 'Content-Type: application/json' \
  -d '{"text":"你好，欢迎使用 MobileVC。","format":"wav"}' \
  --output reply.wav
```

也支持 Bearer Token：

```bash
curl -X POST 'http://127.0.0.1:8001/api/tts/synthesize' \
  -H 'Authorization: Bearer test' \
  -H 'Content-Type: application/json' \
  -d '{"text":"你好","format":"wav"}' \
  --output reply.wav
```

成功时：

- HTTP `200`
- `Content-Type: audio/wav`
- 响应体为可播放的 wav 音频流

#### 错误约定

- `400`：空文本、文本超长、格式不支持
- `401`：token 无效
- `405`：请求方法不允许
- `502`：Python sidecar 不可用、返回异常、返回空音频
- `504`：Python sidecar 超时
- `500`：服务内部错误

#### 常见问题

- sidecar 未启动：通常返回 `502` 或 `504`
- `SIDECAR_MODE=mock`：可以直接返回蜂鸣音 wav，用于联调
- `SIDECAR_MODE=chattts` 但真实模型未准备好：sidecar `/healthz` 与 `/synthesize` 返回 `503`
- `format` 不是 `wav`：返回 `400`
- 文本为空或过长：返回 `400`
- sidecar 返回的不是音频流：返回 `502`

---

## 这是为哪种场景设计的

### 场景 1：电脑上的 Claude 还在跑，但你人已经离开工位
这时你最需要的不是远程桌面，而是一个能快速接管当前 AI 开发流程的界面。

MobileVC 允许你直接在手机上：

- 查看当前是否还在执行
- 看最近一步在做什么
- 看有没有报错
- 看 diff 是否需要处理
- 继续给 Claude 新指令

### 场景 2：你正在进行 vibe coding，不想被设备绑定
很多时候开发链路是连续的，但人不一定一直坐在电脑前。

MobileVC 让你可以把“继续推进 Claude Code 会话”这件事带到手机上，而不是让工作流中断。

### 场景 3：你需要快速确认这次改动值不值得接受
你不一定想在手机上完整写代码，但你很需要：

- 看当前 diff
- 看当前文件
- 看 Claude 为什么这样改
- 决定接受、撤销还是继续调整

这正是 MobileVC 当前最有价值的部分。

---

## 当前前端已经具备的能力

## 1. 登录与连接

- token 登录
- 记住 token 并自动尝试连接
- 登录状态提示
- 重新输入 token
- 主动重连

后端通过 WebSocket token 做访问校验。

---

## 2. 会话管理

当前会话能力已经比较完整：

- 新建会话
- 查看会话列表
- 打开历史会话
- 删除会话
- 恢复可恢复会话
- 记住当前活跃会话
- 恢复历史投影

恢复内容包括：

- 历史日志
- 最近 diff
- 当前步骤
- 最近错误
- stdout / stderr 投影
- resume 相关运行时信息

这意味着 MobileVC 已经不是“一次性连接页”，而是一个可持续使用的 Claude Code 工作台。

---

## 3. Claude Code 工作流相关能力

围绕 Claude Code 当前已经支持：

- 启动 `claude`
- 恢复可续接会话
- 在运行中的会话里继续输入
- 处理 `prompt_request`
- 查看 `step_update`
- 查看 `error`
- 查看运行中的 `agent_state`
- 在 diff / 文件 / 错误 / 步骤上下文中继续操作

这部分正是 MobileVC 的核心，不是附属能力。

---

## 4. 文件浏览与上下文查看

当前文件能力适合移动端快速定位：

- 打开文件抽屉
- 浏览目录
- 进入子目录
- 返回上一级
- 刷新目录
- 打开单个文件
- 单文件全屏查看
- 代码高亮
- 行号显示
- JSON 格式化展示
- 非文本文件提示
- 代码块复制

这让你可以在手机上完成：

**看 Claude 改了什么 → 打开对应文件 → 基于当前文件继续推进**

> 当前文件能力不是在线代码编辑器，但也不只是只读查看：它承接的是“文件上下文查看 + 定向指令输入 + diff 决策”。

---

## 5. Diff 审阅闭环

MobileVC 当前已经具备移动端 diff 审阅闭环：

- 顶部 diff 徽标提示
- 待处理数量展示
- 聊天流 diff 卡片
- 单文件页 diff 审阅栏
- `accept`
- `revert`
- `revise`

这意味着 Claude Code 产生改动后，你可以直接在手机上做关键决策，而不必立刻回到电脑前。

---

## 6. 运行日志与状态详情

当前前端已经围绕“Claude 现在到底在干什么”做了完整展示：

- 聊天流显示 terminal / markdown / error / step 内容
- 运行日志抽屉查看 stdout / stderr 投影
- 顶部运行状态显示
- 活动计时器
- 当前工具 / 技能标签
- 连接状态详情面板

状态详情可查看：

- 当前工作目录
- 当前 sessionId
- resumeSessionId
- permissionMode
- agentState
- recentDiff
- recentStep
- recentError

这部分能力非常适合手机端快速判断 Claude Code 当前运行到哪一步。

---

## 7. 权限模式

当前支持三种权限模式：

- `acceptEdits`：自动同意修改
- `default`：逐项确认
- `plan`：仅规划

前端支持：

- 顶部下拉切换
- 本地持久化保存
- 同步到后端运行时

这使得 MobileVC 不只是“远程控制”，而且能对 Claude Code 的行为方式做移动端侧控制。

---

## 8. Skill Center 与会话级能力

当前已经不只是支持固定 skill，而是形成了一个可管理的 Skill Center：

### 内置 skill
- `review`
- `analyze`
- `doctor`
- `simplify`
- `debug`
- `security-review`
- `explain-step`
- `next-step`

### Skill 管理能力
- 查看全局 skill catalog
- 同步 external skill
- 新增 / 编辑 local skill
- 区分 builtin / local / external 来源
- 为 skill 指定 `targetType` 与 `resultView`

### 会话级 skill 启用
每个会话都可以单独勾选启用哪些 skill。

这意味着你可以为不同 Claude Code 会话配置不同的 skill 组合，而不是所有会话共用一套固定工具。

### Skill Catalog 持久化
当前 skill catalog 会以文件形式持久化保存，来源分为：

- builtin
- local
- external

其中 local skill 可直接在前端新增或编辑，external skill 可同步拉取进入 catalog。

### 上下文感知 skill 执行
当前 skill 不只支持对 diff 做处理，也支持基于不同上下文发起：

- diff 上下文
- step 上下文
- error 上下文

也就是说，你可以针对“当前 diff”“当前步骤”“当前错误”分别发起更合适的分析或诊断。

---

## 9. Memory 管理与会话记忆

MobileVC 当前已经提供**内部显式 Memory 层**，用于给 skill 与会话补充稳定上下文。

> 这里的 Memory 是 MobileVC 自己管理的内部记忆，不等同于 Claude CLI 的隐式 `/memory`。

### Memory 能力
- 查看 memory 列表
- 新增 memory
- 编辑 memory
- 自动生成 memory id
- 持久化保存 memory catalog

### 会话级 memory 启用
和 skill 一样，memory 也支持按会话勾选启用。

启用后的 memory 会在触发 skill 时自动注入 prompt 前缀，作为会话级补充上下文一起发送给 Claude / Gemini。

### Memory Catalog 持久化
当前 memory catalog 同样以文件形式持久化保存，可作为稳定的会话补充上下文层。

这很适合保存例如：

- 当前项目约定
- 特定目录的修改规则
- 团队代码风格
- 当前任务长期有效的约束
- 某个会话里需要一直记住的背景信息

这让 MobileVC 不只是“看会话”，而是开始具备**按会话组织技能与记忆上下文**的能力。

---

## 10. Slash Command 与辅助能力

### Slash Command 分类
当前后端会把 slash command 分成几类来处理：

### Runtime Info
- `/help`
- `/context`
- `/model`
- `/cost`
- `/doctor`
- `/memory`

其中 `/memory` 会打开或提示使用 MobileVC 内部 Memory 面板，而不是直接等同于 Claude CLI 的隐式 `/memory`。

### Skill
- `/review`
- `/analyze`

### 执行类
- `/init`
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

### 本地命令
- `/clear`
- `/exit`
- `/quit`
- `/fast`
- `/diff`

其中本地命令由前端本地处理；`/diff` 会直接触发当前文件 diff 展示。

这部分能力让 MobileVC 不只是“接管 Claude”，也能接管与 Claude 协同的常见开发操作。

---

## 为什么这就是一个面向 vibe coding 的产品

vibe coding 的关键不只是“让 AI 写代码”，而是：

- 会话不断
- 上下文不断
- 决策不断
- 人机协作不断

MobileVC 当前做的事情，正好覆盖了这个流程里最关键的移动端环节：

- 继续会话
- 读当前上下文
- 看 Claude 输出
- 看 diff
- 做确认决策
- 继续下一轮输入

所以它最准确的定义不是“移动端开发工具”，而是：

**面向 Claude Code vibe coding 的移动控制台。**

---

## 项目结构

```text
MobileVC/
├─ cmd/server/               # HTTP 服务入口
├─ internal/
│  ├─ adapter/               # ANSI / 输出解析
│  ├─ config/                # 环境变量配置加载
│  ├─ protocol/              # 前后端事件协议
│  ├─ runner/                # exec / pty 执行与 CLI 封装
│  ├─ runtime/               # 运行时服务与 runtime info
│  ├─ session/               # 会话状态整理与 diff 上下文
│  ├─ skills/                # skill 注册与 prompt 构建
│  ├─ store/                 # 会话存储与恢复
│  └─ ws/                    # WebSocket handler 与 action 分发
├─ scripts/                  # 辅助脚本
├─ web/index.html            # 移动优先前端页面
├─ go.mod
└─ README.md
```

---

## 关键文件

- 服务入口：`cmd/server/main.go`
- 默认端口配置：`internal/config/config.go`
- WebSocket 入口：`internal/ws/handler.go`
- slash command：`internal/ws/slash_command.go`
- 事件协议：`internal/protocol/event.go`
- runtime info：`internal/runtime/info.go`
- 会话存储：`internal/store/file_store.go`
- skill 注册：`internal/skills/registry.go`
- skill prompt 组装：`internal/skills/launcher.go`
- 前端页面：`web/index.html`

---

## 接口

### HTTP
- `GET /`：前端页面
- `GET /healthz`：健康检查
- `GET /download?token=...&path=...`：下载单个文件（仅允许文件，不允许目录）

### WebSocket
- `GET /ws?token=...`

### 常见客户端 action
- `session_create`
- `session_list`
- `session_load`
- `session_delete`
- `session_context_get`
- `session_context_update`
- `skill_catalog_get`
- `skill_catalog_upsert`
- `skill_sync_pull`
- `memory_list`
- `memory_upsert`
- `exec`
- `input`
- `review_decision`
- `set_permission_mode`
- `skill_exec`
- `runtime_info`
- `slash_command`
- `fs_list`
- `fs_read`

### 常见服务端事件
- `session_state`
- `session_created`
- `session_list_result`
- `session_history`
- `agent_state`
- `log`
- `progress`
- `error`
- `prompt_request`
- `step_update`
- `file_diff`
- `runtime_info_result`
- `skill_catalog_result`
- `memory_list_result`
- `session_context_result`
- `skill_sync_result`
- `fs_list_result`
- `fs_read_result`

---

## 配置

配置加载文件：`internal/config/config.go`

### 必填环境变量
- `AUTH_TOKEN`：WebSocket 鉴权 token

### 常用环境变量
- `PORT`：HTTP 端口，默认 `8001`
- `RUNTIME_DEFAULT_COMMAND`：默认命令，默认 `claude`
- `RUNTIME_DEFAULT_MODE`：默认模式，默认 `pty`
- `RUNTIME_DEBUG`：是否开启调试输出
- `RUNTIME_WORKSPACE_ROOT`：运行时工作区根目录
- `RUNTIME_ENHANCED_PROJECTION`
- `RUNTIME_ENABLE_STEP_PROJECTION`
- `RUNTIME_ENABLE_DIFF_PROJECTION`
- `RUNTIME_ENABLE_PROMPT_PROJECTION`

---

## 当前边界

为了让项目定位和当前实现保持一致，需要明确几点：

- 它不是远程桌面
- 它不是传统 Web IDE
- 它当前不是在线代码编辑器
- 但它也不是单纯的文件只读页，而是围绕 **文件上下文 + 会话输入 + diff 审阅** 组织的移动工作台
- 它的核心不是替代电脑，而是**控制电脑上的 Claude Code 工作流**
- 它最强的部分是：**会话接管、状态查看、diff 审阅、文件上下文、恢复续接**

这正是它适合 vibe coding 的原因：

**在你不坐在电脑前的时候，开发流程依然可以继续。**
