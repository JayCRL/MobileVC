# MobileVC 项目架构说明

> 基于 2026-05-07 最新代码。本文只描述架构与逻辑，不涉及具体代码语法。

---

## 目录

- [一、整体架构](#一整体架构)
- [二、后端架构](#二后端-go-架构)
- [三、Flutter 前端架构](#三flutter-前端架构)
- [四、关键逻辑链](#四关键逻辑链)
- [五、交互逻辑详解](#五交互逻辑详解) ← 核心
- [六、对外接口一览](#六对外接口一览)
- [七、部署与运行](#七部署与运行)
- [八、目录树](#八目录树完整)

---

## 一、整体架构

```
┌─────────────────────────────┐
│   Flutter 客户端 (iOS/Android/Web)   │
│   mobile_vc/                     │
└──────────────┬──────────────┘
               │ WebSocket (ws://host:8001/ws?token=xxx)
               │ + 事件流协议 (JSON)
┌──────────────┴──────────────┐
│   Go 后端 (cmd/server)         │
│   ┌─────────────────────────┐ │
│   │ HTTP Server (:8001)        │ │
│   │  /ws          → WebSocket  │ │
│   │  /healthz     → 健康检查    │ │
│   │  /version     → 版本信息    │ │
│   │  /download    → 文件下载    │ │
│   │  /api/tts/*   → TTS 语音   │ │
│   │  /*           → 静态文件    │ │
│   └─────────────────────────┘ │
└──────────────────────────────┘
```

**核心理念**：手机通过 WebSocket 长连到开发机上的后端，后端在开发机上执行 CLI 命令（claude/codex/gemini），将终端输出和 AI 事件实时推送到手机。手机可以发送输入、权限决策、代码审查决策等。

---

## 二、后端 (Go) 架构

### 2.1 入口层 — `cmd/server/main.go`

**职责**：组装所有组件并启动 HTTP 服务。

**启动流程**：
1. 加载配置（环境变量驱动）
2. 初始化 Session 存储（文件系统，`~/.mobilevc/sessions/`）
3. 初始化 APNs 推送服务（可选，依赖环境变量）
4. 创建 WebSocket Handler（注入 AuthToken + SessionStore）
5. 初始化 TTS HTTP Handler（可选）
6. 注册路由并启动 HTTP Server

**依赖注入链**：`Config → SessionStore → WebSocketHandler → RuntimeSessionRegistry`

---

### 2.2 配置层 — `internal/config/`

**配置项**（全部从环境变量读取，无配置文件）：

| 类别 | 配置项 | 说明 |
|------|--------|------|
| 基础 | `PORT` | 服务端口，默认 8001 |
| 基础 | `AUTH_TOKEN` | WebSocket 鉴权 token |
| 运行时 | `RUNTIME_DEFAULT_COMMAND` | 默认 AI 命令，默认 `claude` |
| 运行时 | `RUNTIME_DEFAULT_MODE` | 执行模式 `exec`/`pty`，默认 `pty` |
| 运行时 | `RUNTIME_DEBUG` | 调试开关 |
| 运行时 | `RUNTIME_WORKSPACE_ROOT` | 工作目录 |
| 投影 | `RUNTIME_ENHANCED_PROJECTION` | 增强投影（Step/Diff/Prompt） |
| 投影 | `RUNTIME_ENABLE_STEP_PROJECTION` | Step 投影 |
| 投影 | `RUNTIME_ENABLE_DIFF_PROJECTION` | Diff 投影 |
| 投影 | `RUNTIME_ENABLE_PROMPT_PROJECTION` | Prompt 投影 |
| TTS | `TTS_ENABLED` | 语音合成开关 |
| TTS | `TTS_PROVIDER` / `TTS_PYTHON_SERVICE_URL` | 语音合成配置 |

---

### 2.3 引擎层 — `internal/engine/`

**核心抽象**：`Engine`（别名 `Runner`）接口定义三种能力：

```
Run(ctx, request, sink) → error    // 启动命令执行
Write(ctx, data) → error           // 向运行中的进程写入数据
Close() → error                    // 关闭
```

**两种执行模式**：

- **`ModeExec`** → `ExecRunner`：使用 `exec.Cmd` 运行一次性命令，通过管道读取 stdout/stderr 并输出事件
- **`ModePTY`** → `PtyRunner`：使用 PTY（伪终端）运行交互式命令（如 `claude`），支持持续交互、权限响应、AI session 管理

**PtyRunner 关键能力**：
- 通过 PTY 运行长期 AI CLI 进程
- 从输出中解析 JSON 事件流（`stream-json` 格式）
- 管理 Claude Session ID（通过 `--session-id` flag）
- 管理权限请求状态（`HasPendingPermissionRequest` / `WritePermissionResponse`）
- 交互就绪检测（`CanAcceptInteractiveInput`）
- **Stall Watchdog**：检测引擎输出沉默，分级处理（60s 警告 → 90s 警告 → 120s 强制终止；工具执行中放宽到 600s）

**事件解析链**：
- `Parser` → `GenericParser`（解析通用 CLI 输出）
- `CodexTransport`（解析 OpenAI Codex CLI 的输出格式）
- `ANSI` 处理（终端转义序列清洗）

**其他能力接口**（Runner 可选实现）：
- `ProcessProvider`：暴露进程引用（PID、命令等）
- `InteractiveStateProvider`：查询是否可接受交互输入
- `PermissionResponseWriter`：写入权限决策到 STDIO
- `ClaudeSessionProvider`：暴露当前 Claude Session ID

---

### 2.4 会话层 — `internal/session/`

这是与 AI 交互的核心逻辑层。

#### 2.4.1 核心结构

- **`Service`**：每个 session 一个实例，管理单个会话的完整生命周期
  - 内部包含 `Controller`（状态机）和 `manager`（运行中引擎的管理器）
  - 提供：`Execute`、`SendInput`、`SendPermissionDecision`、`ReviewDecision`、`PlanDecision` 等核心方法
  - 支持自动恢复：`SendInputOrResume`（输入失败时自动恢复 AI session 再发送）

- **`Controller`**：AI 状态机，管理 4 种状态
  - `IDLE` — 空闲
  - `THINKING` — AI 思考中
  - `RUNNING_TOOL` — 执行工具中
  - `WAIT_INPUT` — 等待用户输入

- **`manager`**：管理当前活跃的 Engine Runner 实例，线程安全
  - 记录当前 runner、取消函数、runtime meta、resume session ID
  - 推导 Claude 生命周期状态（`inactive` / `starting` / `active` / `waiting_input` / `resumable` / `unknown`）

#### 2.4.2 事件处理流程

Controller 根据 Runner 产生的事件驱动状态迁移：

| 输入事件 | 触发动作 | 状态迁移 |
|----------|----------|----------|
| `PromptRequestEvent` | AI 请求输入/权限 | → `WAIT_INPUT` |
| `StepUpdateEvent` | 工具执行步骤更新 | → `RUNNING_TOOL` |
| `FileDiffEvent` | 代码 diff 推送 | → `RUNNING_TOOL` |
| `LogEvent`（含 AI prompt） | 检测到 CLI prompt | → `WAIT_INPUT` |
| `OnExecStart` | 命令开始执行 | → `THINKING` |
| `OnInputSent` | 用户发送输入 | → `THINKING` |
| `OnCommandFinished` | 命令结束 | → `IDLE` 或保持 `WAIT_INPUT` |

#### 2.4.3 权限决策路由 — `permission_router.go`

处理手机端发来的权限决策（allow/deny/always），构建决策计划：
- **direct**：直接转发到 Runner
- **deny_then_input**：拒绝权限后需要额外发一条 prompt 文本
- **auto_then_direct**：先通过自动应用的权限规则处理，再转发

#### 2.4.4 投影系统 — `projector.go` / `projection_apply.go` / `projection_events.go`

**投影（Projection）**是将运行时会话状态序列化并持久化到存储层的机制。手机重连时可以根据投影恢复界面。

- `ProjectionSnapshot`：完整的会话投影快照（终端内容、日志、最近 diff、controller 状态、runtime 元信息、session context、review groups 等）
- `WithRuntimeSnapshot`：将实时 Runtime 状态合并到存储的投影中
- `NormalizeProjectionSnapshot`：规范化投影快照（兜底默认值、推导生命周期、重建 review groups）
- `BuildTaskSnapshotEvent`：构建任务快照事件（手机端切换 session 时的状态同步）
- `BuildResumeRecoveryStateEvent`：构建恢复状态事件（连接重连时的过渡态）

#### 2.4.5 交互辅助 — `interaction_helpers.go`

- 权限 Prompt 刷新
- Claude Session ID 管理（生成 UUID 格式的 session ID）
- Claude 权限模式规范化（`auto` / `default` / `bypassPermissions`）

#### 2.4.6 辅助模块 — `process.go` / `info.go`

- `process.go`：进程级别的运行时信息收集
- `info.go`：Session 级别的运行时信息查询

---

### 2.5 网关/WebSocket 层 — `internal/gateway/`

#### 2.5.1 Handler — `gateway.go`

**职责**：处理 WebSocket 连接的完整生命周期。

每个 WebSocket 连接对应一个 goroutine，内部：
1. Token 鉴权
2. WebSocket 升级
3. 创建 detached RuntimeService 和 RuntimeSession
4. 启动写协程（writeCh → conn.WriteJSON）
5. 读循环：解析 JSON → 反序列化为 ClientEvent → 根据 Action 分发

**Client Action 分发表**（从 Flutter 端发来的请求）：

| Action | 处理逻辑 |
|--------|----------|
| `exec` | 创建 Runner → 启动执行 → 事件流转发 |
| `input` | 向 Runner 发送输入文本 |
| `ai-turn` | AI 指令（引擎选择 + 文本 + 权限模式） |
| `permission-decision` | 权限决策（allow/deny/always） |
| `review-decision` | 代码审查决策（accept/revert/revise） |
| `plan-decision` | 计划决策 |
| `stop` | 停止当前运行 |
| `session-create` / `session-list` / `session-history` | Session CRUD |
| `session-resume` | 恢复会话 |
| `slash-command` | 斜杠命令（`/help`, `/clear`, `/init` 等） |
| `adb-*` | ADB/WebRTC 远程控制 |
| `permission-rule-list` / `permission-rule-upsert` / `permission-rule-delete` | 权限规则管理 |
| `skill-*` | Skill 目录管理 |
| `memory-*` | Memory 管理 |
| `catalog-*` | Catalog（Skill/Memory）同步操作 |
| `runtime-info` / `runtime-process-list` / `runtime-process-log` | 运行时信息查询 |
| `fs-list` / `fs-read` | 文件系统浏览 |
| `session-context` / `session-context-set` | 会话上下文管理 |
| `ping` | 心跳 |

**RuntimeSession 生命周期**：
- `Attach`：连接激活时绑定 listener
- `Release`：连接断开时释放 listener，启动 15 分钟延迟清理定时器
- `cleanupIfOrphaned`：定时器触发时检查是否还有连接，无则清理

**事件投影持久化**：每次连接处理 session 相关事件时，都会将控制器状态、终端内容、diff 等写入 `SessionStore`，供断线重连时恢复。

#### 2.5.2 运行时注册表 — `registry.go`

`runtimeSessionRegistry` 管理所有活跃的 RuntimeSession：

- `Ensure(sessionID)`：获取或创建 RuntimeSession
- `Attach/Release`：管理监听者连接
- `FindByResumeSessionID`：通过 UUID 查找对应 session（跨 session 的恢复场景）
- 延迟清理：15 分钟无连接后自动释放

每个 `runtimeSession` 内部维护：
- 事件监听者列表
- 事件缓冲区（pipeline sink，防止积压）
- 待处理事件队列（最大 512 条，支持 cursor 追赶）
- Client Action 去重（防重复提交）

#### 2.5.3 斜杠命令 — `slash.go`

内置命令目录（按类别）：

| 类别 | 命令 | 行为 |
|------|------|------|
| runtime-info | `/help`, `/context`, `/model`, `/cost`, `/doctor`, `/memory` | 重定向到 AI CLI 运行时查询 |
| local | `/clear`, `/exit`, `/quit`, `/fast`, `/diff` | 纯前端处理，不发到后端 |
| skill | `/review`, `/analyze`, `/flutter-context` | 触发 Skill 执行 |
| exec | `/init`, `/plan`, `/execute`, `/compact`, `/build`, `/test`, `/run` | 生成 AI CLI 命令执行 |
| exec-confirm | `/git commit`, `/git push`, `/pr create` | 需要确认后执行的命令 |

#### 2.5.4 推送通知 — `push.go`

在 AI 需要用户注意力时推送 APNs 通知到手机：

- `PromptRequestEvent` → 权限/输入请求时推送
- `InteractionRequestEvent` → 代码审查请求时推送
- `AgentStateEvent`（非空闲态） → 进度推送（30 秒去抖）
- `StepUpdateEvent` / `FileDiffEvent` → 进度推送
- `ErrorEvent` → 错误推送（5 分钟内限流）

#### 2.5.5 权限规则 — `permission_rules.go`

权限规则的数据结构定义和协议转换。规则分为：
- **Session 级别**：仅对当前会话生效
- **Persistent 级别**：跨会话持久化

每条规则包含：作用域、引擎类型、命令头匹配、目标路径前缀匹配、启用状态等。

#### 2.5.6 ADB/WebRTC — `adb.go`

通过 WebRTC 实现远程 Android 设备屏幕投射和触控：

- `adbWebRTCBridge`：管理 WebRTC PeerConnection
- 接收 Flutter 端发来的 SDP Answer 建立连接
- 接收触控事件（`adb_control`）转发到 ADB
- 通过 RTCP 流接收屏幕帧并通过 WebSocket 推送到手机
- 利用 `pion/webrtc` 库实现 WebRTC 协议栈

---

### 2.6 协议层 — `internal/protocol/`

**职责**：定义后端与 Flutter 之间的所有事件类型和数据模型。

#### 2.6.1 RuntimeMeta

所有事件共享的元数据载体，贯穿整个事件流：

```text
RuntimeMeta {
    Source, SkillName, Target, TargetType, TargetPath, ResultView
    ResumeSessionID, ExecutionID, GroupID, GroupTitle
    ContextID, ContextTitle, TargetText
    Command, Engine, Model, ReasoningEffort, CWD
    PermissionMode, PermissionRequestID
    ClaudeSessionUUID, ClaudeLifecycle, BlockingKind
}
```

#### 2.6.2 事件类型

**基础事件**：
- `LogEvent`：终端日志输出
- `ProgressEvent`：进度更新
- `ErrorEvent`：错误信息
- `Event`：通用事件（带 `EventCursor` 增量序号）

**AI 交互事件**：
- `PromptRequestEvent`：AI 请求输入/权限确认（含 `BlockingKind`：`permission`/`review`/`plan`/`reply`/`ready`）
- `InteractionRequestEvent`：代码审查交互请求
- `AgentStateEvent`：AI 状态更新（含状态、消息、是否等待输入、当前命令、步骤、工具）
- `AIStatusEvent`：AI 详细状态
- `StepUpdateEvent`：工具执行步骤
- `FileDiffEvent`：代码改动 diff
- `RuntimePhaseEvent`：运行时阶段

**会话管理事件**：
- `SessionCreatedEvent`：会话创建
- `SessionListResultEvent`：会话列表
- `SessionHistoryEvent`：会话历史
- `SessionResumeResultEvent` / `SessionResumeNoticeEvent`：会话恢复
- `SessionStateEvent`：会话状态变更
- `TaskSnapshotEvent`：任务快照（手机切换 session 时的状态同步）

**代码审查事件**：
- `ReviewStateEvent`：审查状态

**文件系统事件**：
- `FSListResultEvent` / `FSReadResultEvent`：文件浏览

**运行时事件**：
- `RuntimeInfoResultEvent`：运行时信息
- `RuntimeProcessListResultEvent` / `RuntimeProcessLogResultEvent`：进程信息

**Skill/Memory/Catalog 事件**：
- `SkillCatalogResultEvent` / `MemoryListResultEvent`
- `CatalogAuthoringResultEvent` / `CatalogSyncStatusEvent` / `CatalogSyncResultEvent`
- `SessionContextResultEvent` / `SkillSyncResultEvent`

**权限事件**：
- `PermissionRuleListResultEvent` / `PermissionAutoAppliedEvent`

**ADB 事件**：
- `ADBDevicesResultEvent` / `ADBStreamState` / `ADBFrame`
- `ADBWebRTCAnswer` / `ADBWebRTCState`

#### 2.6.3 客户端请求事件

对应 Flutter 端发来的各种 action：

- `ExecRequestEvent`：执行命令
- `InputRequestEvent`：发送输入
- `AITurnRequestEvent`：AI 指令
- `PermissionDecisionRequestEvent`：权限决策
- `ReviewDecisionRequestEvent`：审查决策
- `PermissionRuleListRequestEvent`：权限规则查询
- `SlashCommandRequestEvent`：斜杠命令
- 各种 ADB/Session/Skill/Memory/Catalog 请求事件

---

### 2.7 数据存储层 — `internal/data/`

#### 2.7.1 Store 接口 — `store.go`

抽象数据存取接口，定义 session CRUD、projection 读写、permission rules 管理、push tokens 管理、skill/memory catalog 管理等方法。

#### 2.7.2 FileStore 实现 — `file_store.go`

基于文件系统的持久化：

```text
~/.mobilevc/sessions/
  ├── index.json              # 会话索引
  ├── skills.catalog.json     # Skill 目录
  ├── memory.catalog.json     # Memory 目录
  ├── permissions.rules.json  # 持久权限规则
  ├── push_tokens.json        # APNs device tokens
  └── {session-id}-projection.json  # 每个会话的投影快照
```

核心特性：
- 线程安全（Mutex 保护）
- Session ID 基于时间戳生成（`session-{UnixNano}`）
- 每个 Session 创建时生成 `ClaudeSessionUUID`
- 支持 `Ownership` 标记（`mobilevc` / `claude-native`）用于区分由谁管理

#### 2.7.3 数据模型

- `SessionSummary`：会话摘要（ID、标题、时间、来源、所有权、运行时状态）
- `SessionRecord`：完整会话记录（摘要 + 投影 + 上下文 + 权限规则）
- `ProjectionSnapshot`：会话投影（终端内容、日志、diff、controller 状态、runtime、session context、review groups）
- `PermissionRule`：权限规则
- `SkillDefinition` / `MemoryItem`：Skill 和 Memory 定义
- `CatalogMetadata`：Catalog 元信息（同步状态、版本 token、来源）

#### 2.7.4 子模块

**`internal/data/skills/`**：
- `registry.go`：Skill 注册表（内置 + 本地 + 外部三类来源）
- `launcher.go`：Skill 启动器（解析 Skill 定义并生成执行命令）
- `session_context.go`：会话上下文管理（启用哪些 Skill/Memory）

**`internal/data/claudesync/`**：
- `jsonl_writer.go`：JSONL 格式写入器
- `session_sync.go`：将 session 对话条目同步到 Claude CLI 的 JSONL 文件
- `threads.go`：Claude 对话线程管理

**`internal/data/codexsync/`**：
- `threads.go`：Codex 对话线程管理（类似于 Claude 的 threads）

---

### 2.8 推送服务 — `internal/push/`

- `Service` 接口：`SendNotification(ctx, request) → error`
- `APNsService`：iOS APNs HTTP/2 推送实现（支持 Token 认证和证书认证）
- `NoopService`：空实现（未配置时的默认兜底）

推送请求结构：Token + Platform + Title + Body + Data

---

### 2.9 TTS 语音合成 — `internal/tts/`

- `Provider` 接口：`Synthesize(text, format) → (audioBytes, error)`
- `ChatTTSHTTPProvider`：通过 HTTP 调用 Python ChatTTS 服务
- `Service`：封装文本长度限制、格式默认值
- `HTTPHandler`：提供 `/api/tts/synthesize` 和 `/api/tts/healthz` 端点

---

### 2.10 ADB 服务 — `internal/adb/`

- `service.go`：ADB 设备发现和管理
- `webrtc.go`：ADB 屏幕流通过 WebRTC 传输的核心逻辑

---

### 2.11 日志 — `internal/logx/`

轻量日志库，支持结构化字段（标签 + 格式化消息）、panic 恢复（`Recover` 函数）、调用栈捕获（`StackTrace`）。

---

## 三、Flutter 前端架构

### 3.1 目录总览

```text
mobile_vc/lib/
├── main.dart                           # 应用入口
├── app/                                # 应用级配置与服务
│   ├── app.dart                        # MaterialApp + 路由
│   ├── theme.dart                      # 主题定义
│   ├── push_notification_service.dart  # 推送服务抽象
│   ├── push_notification_service_mobile.dart  # 移动端推送实现
│   ├── push_notification_service_web.dart     # Web 端推送存根
│   ├── push_notification_service_stub.dart    # 桌面端推送存根
│   ├── local_notification_service.dart # 本地通知
│   ├── app_notification_coordinator.dart     # 通知协调器
│   └── background_keep_alive_service.dart    # 后台保活
├── core/                               # 核心工具
│   ├── config/app_config.dart          # 应用配置
│   └── format/time_formatters.dart     # 时间格式化
├── data/                               # 数据层
│   ├── models/
│   │   ├── events.dart                 # 事件模型（~40+ 种事件类型）
│   │   ├── runtime_meta.dart           # RuntimeMeta 模型
│   │   └── session_models.dart         # Session 数据模型
│   └── services/
│       ├── mobilevc_ws_service.dart    # WebSocket 连接管理
│       ├── mobilevc_mapper.dart        # 事件类型映射器
│       └── adb_webrtc_service.dart     # ADB WebRTC 服务
├── features/                           # 功能模块
│   ├── session/                        # 会话核心
│   │   ├── session_controller.dart     # 会话控制器（核心）
│   │   ├── session_home_page.dart      # 会话首页
│   │   ├── session_list_sheet.dart     # 会话列表
│   │   ├── session_display_text.dart   # 显示文本辅助
│   │   ├── claude_model_utils.dart     # 模型识别工具
│   │   ├── connection_scan_sheet.dart  # 连接扫描页
│   │   └── activity_runner_bar.dart    # 活动状态栏
│   ├── chat/                           # 聊天
│   │   ├── chat_timeline.dart          # 聊天时间线
│   │   └── command_input_bar.dart      # 命令输入栏
│   ├── diff/                           # 代码差异
│   │   ├── diff_viewer_sheet.dart      # Diff 查看器弹窗
│   │   └── diff_code_view.dart         # 代码 Diff 视图组件
│   ├── permissions/                    # 权限管理
│   │   └── permission_rule_management_sheet.dart
│   ├── adb/                            # ADB 远程控制
│   │   ├── adb_debug_page.dart         # ADB 调试页
│   │   └── adb_debug_sheet.dart        # ADB 调试弹窗
│   ├── files/                          # 文件浏览
│   │   ├── file_browser_sheet.dart     # 文件浏览器
│   │   └── file_viewer_sheet.dart      # 文件查看器
│   ├── skills/                         # Skill 管理
│   │   └── skill_management_sheet.dart
│   ├── memory/                         # Memory 管理
│   │   └── memory_management_sheet.dart
│   ├── status/                         # 状态显示
│   │   ├── activity_bar.dart           # 活动指示条
│   │   ├── status_detail_sheet.dart    # 状态详情
│   │   └── terminal_log_sheet.dart     # 终端日志查看
│   ├── runtime_info/                   # 运行时信息
│   │   └── runtime_info_sheet.dart
│   └── debug/                          # 调试
│       └── debug_log_viewer.dart
└── widgets/                            # 通用组件
    ├── event_card.dart                 # 事件卡片
    ├── status_badge.dart               # 状态标签
    └── brand_badge.dart                # 品牌标签
```

---

### 3.2 数据流架构

```
[WebSocket 原始 JSON]
        │
        ▼
[MobileVcWsService]  ← 管理 WebSocket 连接、重连、心跳
        │
        ▼  Stream<Map<String, dynamic>>
[MobileVcMapper.mapEvent()]  ← 将 JSON 映射为强类型 AppEvent
        │
        ▼  Stream<AppEvent>
[SessionController]  ← **核心控制器**
  ├── 消费事件流
  ├── 维护 UI 状态（会话列表、当前会话、连接状态、action needed）
  ├── 发送请求（exec、input、permission-decision 等）
  └── 管理 WebRTC 连接
        │
        ▼
[UI Widgets]  ← 响应状态变化，渲染界面
```

### 3.3 SessionController — 前端核心

**职责**：前端所有业务逻辑的中心，相当于前端的"大脑"。

**内部状态管理**：
- 连接状态机：`disconnected → connecting → connected → catchingUp → ready`（含 `reconnecting`、`backgroundSuspended`、`failed`）
- 当前会话 ID 和会话列表
- AI 状态跟踪（thinking、running tool、waiting input 等）
- Action Needed 信号（permission、review、plan、reply、continueInput）
- 事件缓冲区（用于 catch-up 追事件）
- 本地通知触发

**主要方法**：
- `connect(url)`：建立 WebSocket 连接
- `sendExec(command, mode, permissionMode)`：发送执行命令
- `sendInput(data)`：发送输入
- `sendAITurn(engine, data, permissionMode)`：发送 AI 指令
- `sendPermissionDecision(decision, ...)`：发送权限决策
- `sendReviewDecision(decision, ...)`：发送审查决策
- `switchSession(sessionId)`：切换会话（触发 catch-up 追事件）
- `requestSessionList()` / `requestSessionHistory()`：查询会话
- `requestFileList(path)` / `requestFileContent(path)`：文件浏览

**连接恢复机制**：
- 连接断开后自动重连
- 重连后发送 `session-resume` 请求
- 后端返回恢复状态事件 → 前端展示过渡态 → 追完事件流后进入 `ready`

**权限卡片生命周期**：
- 收到 `PromptRequestEvent`（`blockingKind=permission`）→ 弹出权限卡片
- AI turn 结束时清除未决权限状态（避免显示过期的授权卡片）
- `permissionRequestID` 用于关联前后端权限请求

### 3.4 WebSocket 服务 — `MobileVcWsService`

- 使用 `web_socket_channel` 包建立 WebSocket 连接
- 支持 Web 和原生（iOS/Android）双平台
- 连接时自动发送 ping 保活（15 秒间隔）
- 连接断开时发送 `ErrorEvent` 通知上层
- 支持 `connectionEpoch` 机制防止旧连接的过期事件污染

### 3.5 事件映射 — `MobileVcMapper`

将后端发来的 JSON 事件根据 `type` 字段映射为 40+ 种强类型 Dart 事件模型。所有事件模型继承自抽象类 `AppEvent`（含 type、timestamp、sessionId、runtimeMeta、raw）。

### 3.6 推送通知链路

```
后端 APNs Service
     │
     ▼ (HTTP/2 APNs)
Apple APNs Server
     │
     ▼ (Push Notification)
iOS Device
     │
     ▼
[PushNotificationService]  ← 接收远程推送
     │
     ▼
[AppNotificationCoordinator]  ← 协调本地/远程通知
     │
     ▼
[LocalNotificationService]  ← 显示本地通知
```

**后台保活**：`BackgroundKeepAliveService` 在 App 进入后台时维持 WebSocket 连接一定时间。

### 3.7 ADB/WebRTC 远程控制

Flutter 端通过 `flutter_webrtc` 插件实现：
1. 发现 ADB 设备
2. 建立 WebRTC PeerConnection（接收 Offer → 创建 Answer → 通过 WebSocket 发送给后端）
3. 接收视频轨道（屏幕投射）
4. 发送触控事件（通过 DataChannel 或 WebSocket 转发）

---

## 四、关键逻辑链（简要）

以下是对最核心流程的简要时序描述，**详细交互逻辑见第五章**。

### 4.1 AI Turn（简要）

```
手机 sendAITurn("claude", "帮我重构...")
  → WebSocket {"action":"ai_turn", "engine":"claude", "data":"帮我重构..."}
  → 后端组装 ExecuteRequest(Mode=PTY)
  → PtyRunner 启动 claude CLI
  → 事件流推送到手机
```

### 4.2 权限请求（简要）

```
Claude 请求权限 → PromptRequestEvent → 手机弹卡片
  → 用户点 Allow → sendPermissionDecision("allow")
  → PtyRunner.WritePermissionResponse → Claude 继续
```


## 五、交互逻辑详解（核心）

下面按"用户实际使用时的完整调用顺序"描述每个交互场景。

---

### 5.1 连接建立

```
时间线：App 启动 → 用户输入服务器地址 → 点击连接

┌─ Flutter 端 ──────────────────────────────────────────────────────┐
│                                                                     │
│  1. SessionController.connect(url)                                  │
│     ├─ 设置 connectionStage = connecting                            │
│     ├─ 检查是否为 localhost（iPhone 不能用 localhost）               │
│     │                                                               │
│  2. MobileVcWsService.connect(url)                                  │
│     ├─ 关闭旧连接（如有）                                           │
│     ├─ WebSocket.connect(wsUrl)                                     │
│     │   原生: IOWebSocketChannel(pingInterval: 15s, timeout: 15s)   │
│     │   Web:  WebSocketChannel.connect(uri)                         │
│     ├─ 订阅 stream，解码 JSON → mapEvent → 推入 Stream<AppEvent>    │
│     └─ connectionEpoch++（防止旧连接事件污染新连接）                 │
│                                                                     │
│  3. 连接成功后 — connect() 恢复执行：                                │
│     ├─ connectionStage = connected                                  │
│     ├─ switchWorkingDirectory(config.cwd)                           │
│     ├─ requestRuntimeInfo('context')                                │
│     ├─ requestSkillCatalog()                                        │
│     ├─ requestMemoryList()                                          │
│     ├─ requestSessionList()        ← 获取会话列表                   │
│     ├─ requestAdbDevices()                                          │
│     ├─ requestSessionContext()                                      │
│     ├─ requestPermissionRuleList()                                  │
│     ├─ requestReviewState()                                         │
│     ├─ requestTaskSnapshot()                                        │
│     ├─ 如果有 selectedSessionId:                                    │
│     │   └─ _requestSessionResume(reason: "connect")                │
│     │      → {"action":"session_resume","sessionId":"xxx"}         │
│     ├─ _sendCachedPushTokenIfPossible()  ← 注册 APNs token         │
│     └─ _flushPendingOutboundActions()    ← 重发排队消息             │
│                                                                     │
└─────────────────────────────────────────────────────────────────────┘
                              │
              WebSocket JSON ─┤
                              │
┌─ 后端 ─────────────────────────────────────────────────────────────┐
│                                                                     │
│  4. gateway.go ServeHTTP()                                          │
│     ├─ Token 鉴权（URL query "token" vs AuthToken）                  │
│     ├─ WebSocket 升级                                               │
│     ├─ 创建 detached RuntimeService + RuntimeSession                │
│     ├─ 启动写协程（writeCh → conn.WriteJSON）                        │
│     │                                                               │
│  5. 连接时立即发送：                                                 │
│     ├─ SessionStateEvent("active", "connected")                     │
│     ├─ AgentStateEvent（初始状态: "空闲"）                           │
│     └─ Skill 目录结果                                               │
│                                                                     │
│  6. 读循环：for { msgType, payload, err := conn.ReadMessage() }      │
│     ├─ 控制帧（ping/pong/close）→ 直接处理                          │
│     ├─ 文本帧 → json.Unmarshal → ClientEvent                        │
│     └─ 根据 action 进入分发表                                       │
│                                                                     │
└─────────────────────────────────────────────────────────────────────┘
```

**连接状态机（前端）**：

```
disconnected ──connect()──► connecting ──成功──► connected ──session_resume──► catchingUp ──事件追完──► ready
     ▲                        │                     │
     │                        │ 失败                 │ 断开
     │                        ▼                     ▼
     │                     failed ◄────────── reconnecting ◄── 自动重连（最多4次前台）
     │                        ▲                     │
     └────disconnect()────────┘                     │
                              backgroundSuspended ◄─┘（后台时）
```

---

### 5.2 会话创建与首次对话

```
时间线：首次使用 → 自动创建会话 → 发送第一条消息

┌─ Flutter ───────────────────────────────────────────────────────────┐
│                                                                     │
│  1. 用户输入文本 "用 Flutter 写一个登录页面"                          │
│                                                                     │
│  2. sendInputText(text)                                             │
│     ├─ 检测 _shouldAutoCreateSessionOnFirstInput() == true          │
│     │   （条件：无选中 session + 无挂起的通知 session + 未在创建中）   │
│     │                                                               │
│  3. _deferFirstInputAndCreateSession(text)                         │
│     ├─ _deferredFirstInput = text   ← 暂存输入                      │
│     ├─ _autoSessionRequested = true                                 │
│     ├─ _autoSessionCreating = true                                  │
│     └─ createSession()                                              │
│        → {"action":"session_create","title":"","cwd":"..."}         │
│                                                                     │
└─────────────────────────────────────────────────────────────────────┘
                              │
┌─ 后端 ─────────────────────────────────────────────────────────────┐
│                                                                     │
│  4. case "session_create":                                          │
│     ├─ SessionStore.CreateSession(ctx, title)                       │
│     ├─ switchRuntimeSession(created.ID)  ← 切换到新 session          │
│     ├─ emit SessionCreatedEvent                                     │
│     └─ emit SessionListResultEvent                                  │
│                                                                     │
└─────────────────────────────────────────────────────────────────────┘
                              │
┌─ Flutter ───────────────────────────────────────────────────────────┐
│                                                                     │
│  5. 收到 SessionCreatedEvent                                        │
│     ├─ _selectedSessionId = event.sessionId                         │
│     ├─ _finishSessionLoading()                                      │
│     ├─ _flushDeferredFirstInputIfNeeded()                           │
│     │   └─ _deferredFirstInput = "用 Flutter 写一个登录页面"         │
│     │   └─ sendInputText("用 Flutter 写一个登录页面")  ← 再次调用    │
│     │                                                               │
│  6. 第二次 sendInputText —— 现在有 selectedSessionId 了              │
│     ├─ 非 AI 命令头（不以 claude/codex/gemini 开头）                  │
│     ├─ shouldShowClaudeMode?                                        │
│     │   └─ 检查 claudeLifecycle == "starting"/"active"/"waiting_input"/"resumable" │
│     │   └─ 首次无 runner → false                                    │
│     │                                                               │
│     ├─ → 进入普通 exec 路径                                          │
│     ├─ _beginUserSubmission()  ← 设置 _isSubmitting = true          │
│     ├─ 组装 payload:                                                │
│     │   {"action":"exec","cmd":"用 Flutter 写一个登录页面",           │
│     │    "cwd":"...","mode":"pty","permissionMode":"auto"}          │
│     ├─ _sendUserVisibleAction(payload)                              │
│     │   ├─ 生成 clientActionId                                      │
│     │   ├─ _service.send(payload)  ← WebSocket 发送                 │
│     │   ├─ 加入 _pendingOutboundActions（等待 ack）                  │
│     │   └─ _appendLocalUserTimeline(text) ← 本地时间线              │
│     └─ 通知 UI 刷新                                                 │
│                                                                     │
└─────────────────────────────────────────────────────────────────────┘
                              │
┌─ 后端 ─────────────────────────────────────────────────────────────┐
│                                                                     │
│  7. case "exec":                                                    │
│     ├─ ackClientAction（去重检查）                                   │
│     │   └─ 首次 → emit ClientActionAckEvent(status:"accepted")      │
│     │   └─ 重复 → emit ClientActionAckEvent(duplicate:true) + skip  │
│     ├─ 构建 ExecuteRequest:                                         │
│     │   Command = "claude --session-id <uuid> --print --verbose     │
│     │              --output-format stream-json                      │
│     │              --input-format stream-json                       │
│     │              --permission-prompt-tool stdio"                  │
│     │   Mode = PTY                                                   │
│     │   CWD = 请求中的 cwd                                           │
│     │   PermissionMode = "auto"（规范化后）                          │
│     ├─ sessionService.Execute(ctx, sessionID, execReq, emitAndPersist) │
│     │   ├─ manager.start(sessionID, runner, cancel, meta)           │
│     │   ├─ controller.OnExecStart(command, meta)                    │
│     │   │   → AgentStateEvent(state="THINKING", message="思考中")   │
│     │   └─ go runner.Run(ctx, req, sink) ← 异步执行                  │
│     │                                                               │
│     └─ PtyRunner.Run():                                             │
│        ├─ 创建 PTY → 启动 claude 进程                                │
│        ├─ 写入 stdin（如有 initialInput）                            │
│        ├─ 从 PTY stdout 读取输出                                     │
│        ├─ 解析 JSON 事件流（stream-json 格式）                       │
│        │   ├─ GenericParser / CodexTransport                        │
│        │   └─ ANSI 清洗（终端控制序列）                               │
│        └─ 产生事件 → sink → controller.OnRunnerEvent → emit → 手机   │
│                                                                     │
└─────────────────────────────────────────────────────────────────────┘
                              │
              📤 事件流（推送到手机）◄──
                              │
┌─ Flutter ───────────────────────────────────────────────────────────┐
│                                                                     │
│  8. 收到事件序列：                                                   │
│                                                                     │
│     ClientActionAckEvent                                            │
│       ├─ _ackOutboundAction(clientActionId) ← 从待确认队列移除       │
│       └─ 不渲染                                                     │
│                                                                     │
│     AgentStateEvent(state="THINKING", message="思考中")              │
│       ├─ 存储 _agentState                                           │
│       ├─ _setAiStatusVisible(true, label="思考中")                  │
│       ├─ _activityVisible = true, _activityStartedAt = now          │
│       └─ ActivityBar 显示 "思考中" + 耗时计时器                      │
│                                                                     │
│     LogEvent(stream="stdout", message="...")                        │
│       ├─ _eventTargetsCurrentSession? → 追加到时间线                 │
│       ├─ 累积到 _terminalStdout                                     │
│       └─ ChatTimeline 渲染终端日志卡片                               │
│                                                                     │
│     StepUpdateEvent(message="正在读取文件...", target="app.dart")    │
│       ├─ controller.OnRunnerEvent → state → RUNNING_TOOL            │
│       ├─ _currentStep = {message, target}                           │
│       ├─ _activityToolLabel = "app.dart"                            │
│       └─ ActivityBar 更新 "调用工具 · app.dart"                     │
│                                                                     │
│     FileDiffEvent(path="app.dart", diff="...", title="编辑 app.dart") │
│       ├─ controller 缓存到 recentDiff                                │
│       ├─ _currentDiff = event（前端也缓存）                           │
│       └─ 通知 Diff 相关 UI 可展示                                    │
│                                                                     │
│     PromptRequestEvent(message="Do you want to proceed?") ← AI 结束 │
│       ├─ _pendingPrompt = event                                     │
│       ├─ _agentState.awaitInput = true                              │
│       ├─ _activityVisible = false（ActivityBar 隐藏）               │
│       ├─ _setAiStatusVisible(false)  ← 延时 600ms 隐藏 AI 状态球    │
│       ├─ ActionNeededSignal(type=reply/continueInput)               │
│       └─ 输入栏点亮，等待用户回复                                    │
│                                                                     │
│     最终 AgentStateEvent(state="IDLE"/"WAIT_INPUT")                 │
│       ├─ _endUserSubmissionProtection() ← _isSubmitting = false     │
│       └─ AI 回合结束                                                │
│                                                                     │
└─────────────────────────────────────────────────────────────────────┘
```

**关于 clientActionId 去重机制**：

```
前端每次发送都生成唯一 clientActionId（微秒时间戳 + 递增序列号）。
后端 gateway 收到后，在 runtimeSession 的 clientActions map 中检查：
  - 不存在 → 记录并正常处理，返回 ack(status="accepted")
  - 已存在 → 返回 ack(duplicate=true)，skip 业务处理

前端收到 ack 后从 _pendingOutboundActions 移除对应项。
未收到 ack 时每 6 秒重试（最多保留 2 小时内的 action）。
```

---

### 5.3 AI 引擎启动与对话延续（Claude Mode）

```
场景：第一次发送 claude 命令后，claude CLI 进程持续运行，
     后续消息都在同一个 PTY session 中交互。

┌─ Flutter ─────────────────────────────────────────────────────────┐
│                                                                   │
│  状态判定：shouldShowClaudeMode                                    │
│    条件：claudeLifecycle ∈ {starting, active, waiting_input,       │
│           resumable}                                               │
│                                                                   │
│  第一次 "claude 帮我重构" → _startClaudeTurn()                     │
│  第二次 "把颜色改成蓝色" → 检测到 shouldShowClaudeMode == true      │
│    → _submitClaudeContinuation("把颜色改成蓝色")                    │
│    → payload = {"action":"ai_turn", "engine":"claude",             │
│                 "data":"把颜色改成蓝色\n"}                          │
│    → 后端用 SendInputOrResume 处理                                 │
│                                                                   │
│  如果用户手动打 "claude 新任务" → 视为新的 AI 命令头                │
│    → _startClaudeTurn() 而非 _submitClaudeContinuation()           │
│                                                                   │
└───────────────────────────────────────────────────────────────────┘
                            │
┌─ 后端 ───────────────────────────────────────────────────────────┐
│                                                                   │
│  case "ai_turn":                                                  │
│    ├─ 解析引擎（从请求/运行时/投影中提取 engine）                  │
│    ├─ 推导命令（command head 校准）                                │
│    ├─ applyAICommandPreferences（注入 model/reasoningEffort）      │
│    ├─ 如果有 data（用户输入文本）:                                 │
│    │   ├─ RecordUserInput(data)                                   │
│    │   ├─ 注入 Skill/Memory 前缀                                   │
│    │   │   InjectConversationPrefixes(data, skillPrefix,           │
│    │   │                             memoryPrefix)                 │
│    │   ├─ SendInputOrResume(sessionID, execReq, inputReq, emit)    │
│    │   │   ├─ 先尝试 SendInput（写入现有 runner 的 stdin）         │
│    │   │   │   ├─ 成功 → 完成                                      │
│    │   │   │   └─ ErrNoActiveRunner → 进入恢复路径                 │
│    │   │   └─ 恢复路径:                                            │
│    │   │       ├─ buildDetachedResumeRequest() 构建 resume 命令     │
│    │   │       ├─ Execute() 启动新 runner（带 --resume <id>）      │
│    │   │       ├─ waitForRunnerStart() 等待 runner 就绪            │
│    │   │       └─ sendInputWhenRunnerReady() 写入输入              │
│    │   └─ 如果以上都失败 → 创建新 runner 并 Execute                │
│    │                                                               │
│    ├─ 如果无 data（纯启动）:                                       │
│    │   └─ Execute() 直接启动                                       │
│    │                                                               │
└───────────────────────────────────────────────────────────────────┘
```

**ClaudeLifecycle 状态推导**（后端 `Controller.deriveClaudeLifecycleLocked`）：

```
输入条件                           → 推导结果
─────────────────────────────────────────────
meta.ClaudeLifecycle 明确指定      → 直接用
state==WAIT_INPUT && isClaudeCmd   → waiting_input
state==THINKING/RUNNING_TOOL && isClaudeCmd → active
resumeSessionID 非空               → resumable
isClaudeCmd && runner存在           → unknown
其他                               → inactive
```

---

### 5.4 权限请求与决策（完整时序）

```
这是最关键的交互之一。Claude CLI 在需要执行危险操作时会暂停并请求权限。

┌─ 事件流 ────────────────────────────────────────────────────────────┐
│                                                                     │
│  1. Claude CLI stdout 输出权限请求 JSON:                             │
│     {"type":"permission_request", "id":"perm-abc123",               │
│      "message":"Claude wants to run: rm -rf /tmp/cache",            │
│      "options":["allow","deny","always allow"]}                     │
│                                                                     │
│  2. PtyRunner 解析 → PromptRequestEvent:                            │
│     type = "prompt_request"                                         │
│     message = "Claude wants to run: rm -rf /tmp/cache"              │
│     blockingKind = "permission"                                     │
│     permissionRequestID = "perm-abc123"                             │
│     contextID = "..."（操作上下文）                                   │
│     contextTitle = "..."（操作描述）                                  │
│     targetPath = "/tmp/cache"（目标路径）                            │
│     target = "rm"（目标命令）                                        │
│     targetType = "shell_command"（目标类型）                         │
│                                                                     │
│  3. runnerSink → applyRuntimeMeta → controller.OnRunnerEvent()      │
│     ├─ 匹配到 PromptRequestEvent case                               │
│     ├─ currentState = WAIT_INPUT                                    │
│     ├─ claudeLifecycle = "waiting_input"                            │
│     ├─ blockingKind = "permission"                                  │
│     ├─ 缓存 permissionRequestID / contextID / contextTitle /        │
│     │       targetPath / target / targetType                        │
│     ├─ newAgentStateEvent(message, awaitInput=true)                 │
│     │   → AgentStateEvent(state="WAIT_INPUT", awaitInput=true,      │
│     │       blockingKind="permission")                              │
│     └─ 返回 [AgentStateEvent]                                       │
│                                                                     │
│  4. emitAndPersist 处理事件:                                         │
│     ├─ 检测到 blockingKind="permission"                             │
│     ├─ maybeAutoApplyPermissionEvent() ← 尝试自动应用权限规则        │
│     │   ├─ 查询 session 级别权限规则                                 │
│     │   ├─ 查询 persistent 级别权限规则                              │
│     │   ├─ 匹配：Engine + CommandHead + TargetPathPrefix            │
│     │   ├─ 命中 → 自动 allow → PermissionAutoAppliedEvent           │
│     │   │        → 跳过后续 → 直接 WritePermissionResponse("allow") │
│     │   └─ 未命中 → 正常转发给客户端                                 │
│     │                                                               │
│     ├─ 未自动应用的 → emit(event) → WebSocket 推送到手机            │
│     ├─ sendPushNotificationIfNeeded() ← 推 APNs（后台场景）          │
│     └─ ApplyEventToProjection() → persistProjectionFor()            │
│                                                                     │
└─────────────────────────────────────────────────────────────────────┘
                              │
                       WebSocket
                         │
┌─ Flutter ───────────────────────────────────────────────────────────┐
│                                                                     │
│  5. 收到 PromptRequestEvent（blockingKind="permission"）             │
│     ├─ _pendingPrompt = event                                       │
│     ├─ 检查是否已有相同 requestID 的 prompt → 去重                   │
│     │                                                               │
│  6. 随后收到 AgentStateEvent（awaitInput=true）                      │
│     ├─ _agentState = event                                          │
│     ├─ _activityVisible = false                                     │
│     ├─ _setAiStatusVisible(false)                                   │
│     ├─ _actionNeededType = permission                               │
│     ├─ _actionNeededKey = "permission-{requestID}"                 │
│     ├─ _actionNeededMessage = event.message                         │
│     ├─ _actionNeededRevision = event.requestID                      │
│     └─ notifyListeners() → UI 弹出权限确认卡片                      │
│                                                                     │
│  7. 权限卡片 UI 展示：                                               │
│     ┌─────────────────────────────────────┐                        │
│     │  ⚠️ Claude 需要权限确认              │                        │
│     │                                     │                        │
│     │  Claude wants to run:               │                        │
│     │  rm -rf /tmp/cache                  │                        │
│     │                                     │                        │
│     │  目标: /tmp/cache                   │                        │
│     │  操作: rm                           │                        │
│     │                                     │                        │
│     │  [允许本次] [始终允许] [拒绝]        │                        │
│     └─────────────────────────────────────┘                        │
│                                                                     │
│  8. 用户点击"允许本次"                                               │
│     ├─ submitPromptOption("allow")                                  │
│     ├─ 检测 pendingPrompt.isPermission == true                      │
│     ├─ _parsePermissionDecisionSelection("allow")                   │
│     │   → {decision: "approve", scope: ""}                         │
│     └─ _sendPermissionDecision(prompt, selection)                   │
│        → {"action":"permission_decision",                            │
│           "decision":"approve",                                     │
│           "permissionRequestId":"perm-abc123",                      │
│           "resumeSessionId":"...",                                  │
│           "contextId":"...",                                        │
│           "contextTitle":"...",                                     │
│           "targetPath":"/tmp/cache",                                │
│           "permissionMode":"auto",                                  │
│           "command":"claude","cwd":"...",                           │
│           "engine":"claude"}                                        │
│                                                                     │
└─────────────────────────────────────────────────────────────────────┘
                              │
┌─ 后端 ─────────────────────────────────────────────────────────────┐
│                                                                     │
│  9. case "permission_decision":                                     │
│     ├─ resolvePermissionDecisionRuntime(req)                        │
│     │   ├─ 优先使用 req.SessionID                                    │
│     │   ├─ 否则通过 ResumeSessionID 查找对应 session                 │
│     │   └─ switchRuntimeSession(matchedID)                          │
│     │                                                               │
│     ├─ BuildPermissionDecisionPlan(req, projection, controller)     │
│     │   ├─ 决策 = "approve" / "deny"                                │
│     │   ├─ 引擎 = "claude"（isClaudeCommandLike? yes）              │
│     │   ├─ approve → direct: 直接 WritePermissionResponse           │
│     │   └─ deny → deny_then_input: 拒绝 + 发送 prompt               │
│     │                                                               │
│     ├─ Service.SendPermissionDecision(ctx, sessionID, decision,     │
│     │                                    meta, emit)               │
│     │   ├─ 获取 currentRunner                                       │
│     │   ├─ 验证 PermissionResponseWriter 接口可用                    │
│     │   ├─ 验证 HasPendingPermissionRequest() == true               │
│     │   ├─ 如果 requestID 指定 → 验证匹配（防过期权限）              │
│     │   ├─ 如果权限模式变化 → SetPermissionMode                     │
│     │   └─ runner.WritePermissionResponse(ctx, "allow")             │
│     │       → PtyRunner 写入 stdin（JSON 格式的权限响应）             │
│     │                                                                 │
│     ├─ Controller.OnInputSent(meta)                                 │
│     │   ├─ 清除 permissionRequestID                                 │
│     │   ├─ 清除 blockingKind                                         │
│     │   ├─ state → THINKING                                         │
│     │   └─ AgentStateEvent("思考中")                                │
│     │                                                               │
│     └─ Claude 收到权限响应 → 继续执行 → 产生后续事件                  │
│                                                                     │
└─────────────────────────────────────────────────────────────────────┘
                              │
┌─ Flutter ───────────────────────────────────────────────────────────┐
│                                                                     │
│  10. 收到权限决策后的后续事件：                                       │
│      ├─ ClientActionAckEvent（确认）                                │
│      ├─ AgentStateEvent(state="THINKING")                           │
│      │   ├─ _pendingPrompt = null（清除权限卡片）                    │
│      │   └─ _setAiStatusVisible(true, label="思考中")               │
│      └─ 后续 LogEvent / StepUpdateEvent / FileDiffEvent...          │
│                                                                     │
└─────────────────────────────────────────────────────────────────────┘
```

**权限自动应用（权限规则匹配）**：

```
maybeAutoApplyPermissionEvent 检查流程：
  1. 从 SessionStore 读取 session 级别 + persistent 级别的权限规则
  2. 对每条规则进行匹配：
     - Engine 匹配（claude/codex/gemini）
     - CommandHead 匹配（命令头）
     - TargetPathPrefix 匹配（目标路径前缀）
     - Kind 匹配（always_allow / always_deny）
     - Enabled 状态
  3. 匹配到规则 → 自动发送对应的权限响应到 Runner
     → 发送 PermissionAutoAppliedEvent 到客户端
     → 客户端收到后清除对应 pendingPrompt
  4. 未匹配到 → 正常展示权限卡片，等待用户手动决策
```

**权限 decision 的三种值**：
- `approve` = 允许本次
- `always_allow` = 始终允许（同时创建持久化权限规则）
- `deny` = 拒绝（对 Claude: 发送拒绝 + prompt 文本 "The user denied this operation."）

---

### 5.5 代码审查（Review/Diff）流程

```
场景：Claude 在执行过程中产生了文件改动，需要通过手机审核。

┌─ 事件流 ────────────────────────────────────────────────────────────┐
│                                                                     │
│  1. Claude CLI 输出 file_diff JSON:                                  │
│     PtyRunner 解析 → FileDiffEvent                                  │
│     {type:"file_diff", path:"lib/app.dart", diff:"@@ -1,5 +1,7 @@", │
│      title:"编辑 app.dart", lang:"dart",                            │
│      contextID:"diff-abc", executionID:"exec-123"}                   │
│                                                                     │
│  2. Controller.OnRunnerEvent(FileDiffEvent):                        │
│     ├─ state → RUNNING_TOOL                                         │
│     ├─ 缓存到 recentDiff（在 recentDiffs 列表中 upsert）             │
│     ├─ PendingReview 判断：                                          │
│     │   └─ shouldAutoAcceptReviewForPermissionMode(mode)            │
│     │       如果是 "bypassPermissions" → 自动 accept                │
│     │       否则 → pending                                           │
│     ├─ recentDiff = pickActiveRecentDiffLocked()  ← 最新待审核 diff │
│     └─ AgentStateEvent（含 diff 预览信息）                           │
│                                                                     │
└─────────────────────────────────────────────────────────────────────┘
                              │
┌─ Flutter ───────────────────────────────────────────────────────────┐
│                                                                     │
│  3. 收到 FileDiffEvent                                               │
│     ├─ _currentDiff = event                                          │
│     ├─ 加入 _recentDiffs 列表                                        │
│     ├─ 触发 rebuildReviewGroups → 更新 ReviewGroup 列表              │
│     └─ Diff 卡片的显示/隐藏由权限模式决定：                           │
│         ├─ bypassPermissions → 自动 accept，不弹卡片                │
│         ├─ auto → 弹 Diff 审核卡片                                   │
│         └─ default → 弹 Diff 审核卡片                                │
│                                                                     │
│  4. 用户点击 Diff 卡片查看：                                          │
│     ├─ DiffViewerSheet 打开（上下分屏：diff 视图 + 代码视图）         │
│     └─ DiffCodeView 渲染语法高亮的代码差异                            │
│                                                                     │
│  5. 用户做出审查决策：                                                │
│     ├─ sendReviewDecision("accept")   → {"action":"review_decision", │
│     │                                    "decision":"accept", ...}   │
│     ├─ sendReviewDecision("revert")   ← 撤销改动                    │
│     └─ sendReviewDecision("revise")   ← 要 AI 重新修改              │
│                                                                     │
└─────────────────────────────────────────────────────────────────────┘
                              │
┌─ 后端 ─────────────────────────────────────────────────────────────┐
│                                                                     │
│  6. case "review_decision":                                         │
│     ├─ Service.ReviewDecision(ctx, sessionID, req, emit)            │
│     ├─ 规范化 decision: "accept"/"revert"/"revise"                  │
│     ├─ IsReviewOnly 判断：                                          │
│     │   ├─ isReviewOnly=true && decision≠"revert" → 仅更新状态     │
│     │   │   Controller.OnInputSent() 标记 diff 为非 pending         │
│     │   │   不发送到 AI                                              │
│     │   └─ isReviewOnly=false → 构建 review text 发送到 AI          │
│     │       ├─ accept: "Review decision: ACCEPT. Please land..."    │
│     │       ├─ revert: "Review decision: REVERT. Please drop..."    │
│     │       └─ revise: "Review decision: REVISE. Please update..."  │
│     └─ SendInput(ctx, sessionID, InputRequest{Data: payload}, emit) │
│                                                                     │
│  7. Controller.OnInputSent(meta):                                   │
│     ├─ Source="review-decision"                                     │
│     ├─ 根据 decision 更新 recentDiffs:                              │
│     │   ├─ accept/revert → markRecentDiffPendingLocked(id,path,false)│
│     │   └─ revise → markRecentDiffPendingLocked(id,path,true)       │
│     ├─ recentDiff = pickActiveRecentDiffLocked() ← 下一个待审核 diff │
│     ├─ state → THINKING                                              │
│     └─ AgentStateEvent("思考中")                                     │
│                                                                     │
└─────────────────────────────────────────────────────────────────────┘
```

**ReviewGroup 聚合逻辑**：

```
多个 FileDiffEvent 可能属于同一轮修改，通过 GroupID 聚合：
  - 同 executionID 的 diff 自动归为一组
  - 组的状态：pending / accepted / reverted / revised / mixed
  - 活跃组选取：最近的有 pending 项的组（遍历到最前面）
  - 活跃文件选取：组内第一个 pending 项

前端展示时先展示组摘要（PendingCount/AcceptedCount等），
点击展开到组内具体文件列表。
```

---

### 5.6 会话切换与恢复

```
场景：用户在会话列表中切换到另一个 session，
    或 App 杀后台后重新打开需要恢复之前的 AI 会话。

┌─ Flutter ───────────────────────────────────────────────────────────┐
│                                                                     │
│  1. loadSession(sessionId)                                           │
│     ├─ connectionStage = catchingUp（如果已连接）                     │
│     ├─ _beginSessionLoading(targetId: sessionId)                     │
│     │   ├─ _isLoadingSession = true                                  │
│     │   ├─ _pendingSessionTargetId = sessionId                       │
│     │   ├─ 清空 pendingPrompt / pendingInteraction / agentState 等   │
│     │   └─ _agentPhaseLabel = "切换会话中"                           │
│     └─ {"action":"session_load", "sessionId":"xxx"}                  │
│                                                                     │
└─────────────────────────────────────────────────────────────────────┘
                              │
┌─ 后端 ─────────────────────────────────────────────────────────────┐
│                                                                     │
│  2. case "session_load":                                            │
│     ├─ 从 SessionStore 加载 SessionRecord                            │
│     ├─ 如果是 auto_bind 且已有 session → skip                        │
│     ├─ switchRuntimeSession(record.Summary.ID)                      │
│     │   ├─ 释放旧 runtimeSession 的当前连接 listener                 │
│     │   └─ Attach 到新 runtimeSession（成为 listener）               │
│     ├─ mergeClaudeJSONLToRecord() ← 合并 Claude CLI 本地 JSONL 增量  │
│     ├─ 构建 projection（含 live runtime 状态合并）                   │
│     │   └─ WithRuntimeSnapshot(projection, runtimeSvc)              │
│     ├─ 发送 SessionHistoryEvent（完整历史 + runtime 状态）            │
│     ├─ 发送 ReviewState（当前 review groups/diffs）                  │
│     ├─ 发送 restoredAgentStateEvent（恢复态）                        │
│     │   ├─ 如果投影表明 busy → "恢复执行中"                          │
│     │   ├─ 如果可恢复 → "会话已暂停，可继续对话"                     │
│     │   ├─ 如果等待输入 → "等待输入"                                 │
│     │   └─ 否则 → "空闲"                                             │
│     └─ 发送 SessionStateEvent("active", "history loaded")           │
│                                                                     │
└─────────────────────────────────────────────────────────────────────┘
                              │
┌─ Flutter ───────────────────────────────────────────────────────────┐
│                                                                     │
│  3. 收到 SessionHistoryEvent:                                       │
│     ├─ 检查 _isHistoryEventForActiveTarget(sessionId)                │
│     │   → 是目标 session → 应用完整投影                              │
│     ├─ _selectedSessionId = event.sessionId                          │
│     ├─ _selectedSessionTitle = event.title                           │
│     ├─ 恢复 terminal contents（_terminalStdout/Stderr）              │
│     ├─ 恢复 timeline（_timeline = event.entries）                    │
│     ├─ 恢复 diffs / reviewGroups                                     │
│     ├─ 恢复 sessionContext                                           │
│     ├─ 恢复 runtimeMeta（resumeSessionID, command, engine, cwd）      │
│     ├─ 恢复 permissionRules                                          │
│     └─ _finishSessionLoading()                                       │
│                                                                     │
│  4. 收到 restoredAgentStateEvent:                                    │
│     ├─ 设置 _agentState = event                                      │
│     ├─ connectionStage = ready（如果 catchingUp → ready）             │
│     ├─ 如果 state="RECOVERING" → ActivityBar 显示过渡态              │
│     └─ 后续正常消费事件流                                            │
│                                                                     │
│  5. 事件补发（catch-up）：                                            │
│     ├─ 前端保存 _sessionEventCursors[sessionId] = latestCursor       │
│     ├─ 重连后发送 session_resume                                     │
│     ├─ 后端 runtimeSession.pendingSince(cursor) 返回缺失事件          │
│     ├─ 按顺序补发到手机                                              │
│     └─ 前端追完 → connectionStage = ready                            │
│                                                                     │
└─────────────────────────────────────────────────────────────────────┘
```

**Session Resume（恢复 AI 会话）**：

```
如果投影显示 claudeLifecycle = "resumable"（有 resumeSessionID），
前端显示"恢复会话"按钮。用户点击后：

  前端 → {"action":"session_resume","sessionId":"xxx"}
  
  后端 case "session_resume":
    ├─ 构建 ExecuteRequest（Mode=PTY, Command="claude --resume <id>..."）
    ├─ 发送过渡态 AgentStateEvent("恢复会话中")
    ├─ Execute() 启动 PtyRunner
    ├─ 前端收到事件流后 connectionStage → ready
    └─ 用户可继续在原 AI 会话中对话
```

---

### 5.7 后台保活与断线重连

```
场景：用户切换到其他 App → MobileVC 进后台 → 一段时间后回来。

┌─ 后台处理 ──────────────────────────────────────────────────────────┐
│                                                                     │
│  1. App 进后台：handleForegroundStateChanged(false)                  │
│     ├─ _appInForeground = false                                      │
│     ├─ 不主动断开 WebSocket（BackgroundKeepAliveService 保持）        │
│     └─ _stopObservedSessionSync() ← 停止轮询                         │
│                                                                     │
│  2. 后台 WebSocket 断开：_handleUnexpectedSocketDisconnect()          │
│     ├─ _connected = false                                            │
│     ├─ connectionStage = backgroundSuspended（如果不在前台）          │
│     ├─ _autoReconnectEnabled 保持 true（不取消自动重连）              │
│     └─ 后台期间不发重连，等待回到前台                                 │
│                                                                     │
│  3. App 回前台：handleForegroundStateChanged(true)                    │
│     ├─ _scheduleReconnect(immediate: true)                           │
│     ├─ _syncObservedSessionPolling() ← 恢复轮询                      │
│     └─ 连接成功后 flushPendingOutboundActions()                       │
│                                                                     │
│  4. 重连策略：_scheduleReconnect()                                    │
│     ├─ _reconnectAttempt 递增                                        │
│     ├─ 延迟策略:                                                      │
│     │   第1次: 0s（立即）                                            │
│     │   第2次: 1s                                                    │
│     │   第3次: 3s                                                    │
│     │   第4次: 5s                                                    │
│     │   超过4次: 10s + 不再自动重连前台                              │
│     └─ connect(silently: true, restoreSession: true)                 │
│                                                                     │
│  5. 后台期间如果有 AI 事件：                                          │
│     ├─ 后端 sendPushNotificationIfNeeded() 检测到无连接              │
│     └─ 发送 APNs 推送 → 手机通知栏 → 用户点击 → App 回前台           │
│                                                                     │
└─────────────────────────────────────────────────────────────────────┘
```

**Pending Outbound Queue（离线消息队列）**：

```
用户在前端点击发送时，如果 WebSocket 断开：
  1. _sendUserVisibleAction() 检测 _service.send() 返回 false
  2. 如果 queueOnFailure=true → 加入 _pendingOutboundActions 列表
  3. 启动重连 + _schedulePendingOutboundRetry()（6 秒间隔）
  4. 连接恢复后 _flushPendingOutboundActions() 按顺序重发
  5. 每条都生成新的 clientActionId，用新时间戳
  6. 每条都 appendLocalUserTimeline()（防止重复显示）
```

---

### 5.8 AI Turn 内部的精确调用链

```
用户输入文本 → sendInputText() 的分支决策树：

  输入文本
     │
     ├─ _isLoadingSession? ─→ 拒绝（"会话切换中"）
     │
     ├─ hasPendingPermissionPrompt? ─→ 拒绝（"请先完成授权"）
     │
     ├─ hasPendingPlanQuestions? ─→ 拒绝（"请先完成计划选择"）
     │
     ├─ canSendToContinuedSameSession? ─→ _submitClaudeContinuation()
     │   （条件：AI 刚结束 turn，收到过 TaskSnapshot 的 sameSessionId）
     │
     ├─ awaitInput? ─→ _submitAwaitingPrompt()
     │   （条件：_agentState.awaitInput==true 且无 pending permission/review）
     │   │
     │   ├─ pendingInteraction?.isReview? ─→ sendReviewDecision()
     │   ├─ pendingInteraction?.isPermission? ─→ _sendInteractionDecision()
     │   └─ 否则 ─→ _submitAwaitingInput()（直接发送 input）
     │
     ├─ 以 "/" 开头? ─→ _handleSlashCommand()
     │
     ├─ _shouldAutoCreateSessionOnFirstInput()? ─→ _deferFirstInputAndCreateSession()
     │
     ├─ 是 AI 命令头? (claude/codex/gemini) ─→ _startClaudeTurn()
     │
     ├─ shouldShowClaudeMode? ─→ _submitClaudeContinuation()
     │   （payload: {"action":"ai_turn","data":"<text>\n","engine":"claude"})
     │
     └─ 否则 ─→ 普通 exec 路径
                 （payload: {"action":"exec","cmd":"<text>","mode":"pty"})
```

**后端 ai_turn 处理的分支路径**：

```
收到 {"action":"ai_turn","data":"<text>","engine":"claude"}

  1. 解析 engine + command:
     如果 data 非空:
       ├─ InjectConversationPrefixes(data, skillPrefix, memoryPrefix)
       └─ SendInputOrResume():
            ├─ 尝试 SendInput()（写入现有 runner stdin）
            │   ├─ 成功 → done
            │   └─ ErrNoActiveRunner
            │       ├─ CanResumeAISession? HasResumeSession?
            │       ├─ buildDetachedResumeRequest():
            │       │    构造 "claude --resume <id> --print --verbose
            │       │            --output-format stream-json
            │       │            --input-format stream-json
            │       │            --permission-prompt-tool stdio"
            │       ├─ Execute() 启动新 runner
            │       ├─ waitForRunnerStart()
            │       └─ sendInputWhenRunnerReady() 写入 data
            │
            └─ 以上都失败 → Execute() 直接启动（带 initialInput）

     如果 data 为空:
       └─ Execute() 启动（纯启动，无输入）
```

---

### 5.9 事件消费与 UI 状态同步

```
后端每条事件的 emitAndPersist 处理链：

  事件从 Runner 发出
    │
    ├─ 如果是 PromptRequestEvent / InteractionRequestEvent:
    │   └─ maybeAutoApplyPermissionEvent()
    │       ├─ 自动应用 → PermissionAutoAppliedEvent → emit
    │       └─ 不能自动应用 → 正常进入后续
    │
    ├─ 如果是 CatalogAuthoringResultEvent:
    │   └─ 持久化 skill/memory 定义 → emit 刷新结果
    │
    ├─ 正常路径:
    │   ├─ prepareSessionEventForResume()（补充 resume 信息）
    │   ├─ emitSessionEvent() → WebSocket 推送
    │   ├─ sendPushNotificationIfNeeded() → APNs
    │   ├─ ApplyEventToProjection() → 更新投影快照
    │   │   ├─ 修改 terminal stdout/stderr
    │   │   ├─ 追加 log entry
    │   │   ├─ 更新 diff / controller / runtime 状态
    │   │   └─ 更新 timeline（用户/助手条目）
    │   └─ persistProjectionFor() → 写入文件
    │       └─ syncSessionEntriesToClaudeJSONL() → 同步到 Claude CLI
    │
    └─ AIStatusEvent 生成:
        └─ AIStatusEventForBackendEvent()
            ├─ 检测是否应该发 AI 状态更新
            └─ 条件满足 → emitAIStatusEvent()

前端事件消费链：

  MobileVcWsService.stream
    │
    ├─ MobileVcMapper.mapEvent(json) → Typed AppEvent
    │
    └─ SessionController._onEvent(event):
        ├─ ClientActionAckEvent → _ackOutboundAction()
        ├─ AgentStateEvent → 更新 AI 状态/activity bar
        ├─ LogEvent → 追加到时间线
        ├─ StepUpdateEvent → 更新步骤/工具标签
        ├─ FileDiffEvent → 更新 diff 列表
        ├─ PromptRequestEvent → 弹出交互卡片
        ├─ InteractionRequestEvent → 弹出交互卡片
        ├─ AIStatusEvent → 更新 AI 状态球
        ├─ SessionHistoryEvent → 加载会话历史
        ├─ SessionListResultEvent → 更新会话列表
        ├─ RuntimePhaseEvent → 更新运行阶段
        ├─ ErrorEvent → 错误提示
        ├─ TaskSnapshotEvent → 更新任务快照
        ├─ FSListResultEvent → 更新文件列表
        ├─ FSReadResultEvent → 展示文件内容
        └─ ADBFrameEvent → 更新屏幕帧

ActivityBar 显示/隐藏逻辑：

  显示条件（任一满足）:
    - AgentStateEvent state=THINKING/RUNNING_TOOL/RECOVERING
    - StepUpdateEvent 消息非空
    - FileDiffEvent 有内容
    - _isSubmitting=true（用户刚发送）

  隐藏条件（所有满足）:
    - AgentStateEvent state=IDLE/WAIT_INPUT
    - _isSubmitting=false
    - 延时 600ms 淡出（防抖动）

AI 状态球显示/隐藏：
  显示: AgentStateEvent state≠IDLE 或有正在处理的信号
  隐藏: AgentStateEvent state=IDLE/WAIT_INPUT + 延时 600ms
  保护: _isSubmitting=true 时不隐藏（用户发送后到收到首个 reply 前的间隙）
```

---

### 5.10 Slash 命令处理

```
用户输入 "/help" → sendInputText("/help"):

  _handleSlashCommand("/help")
    ├─ /help → category="runtime-info"
    │   → {"action":"runtime_info","query":"help"}
    │
    ├─ /clear → category="local"
    │   → 前端直接清空时间线，不发网络请求
    │
    ├─ /init → category="exec"
    │   → 生成 exec 请求: {"action":"exec","cmd":"claude /init"}
    │
    ├─ /review → category="skill"
    │   → {"action":"skill_exec","skillName":"review"}
    │
    ├─ /git commit "fix bug" → category="exec" + requiresArgs + confirmOnly
    │   → 先弹确认对话框 → 用户确认后发送 exec
    │
    └─ /run npm test → category="exec"
        → {"action":"exec","cmd":"npm test"}
```

---

### 5.11 前端连接期间心跳与同步

```
连接健康检查（ConnectionHealthTimer，10 秒间隔）:
  检查 _lastServerEventAt:
    - 超过 45 秒无事件 → 视为连接失活
    - 触发重连流程

Observed Session Sync（3 秒间隔，仅外源 session）:
  条件：selectedSession.external==true 且有 active runtime
  → 定时请求 session_resume 获取最新投影
  → 同步远端 CLI 的进度到本地

Ping 心跳:
  前端不主动发 ping（依赖 WebSocket 底层 ping/pong）
  后端 case "ping" → 回复 PongEvent + TaskSnapshotEvent
```

---

### 5.12 通知触发与分类

```
前端 ActionNeededSignal 分类：

  权限请求:
    type = permission
    条件: _pendingPrompt?.isPermission == true
  
  代码审查:
    type = review
    条件: _pendingInteraction?.isReview == true 或 _pendingPrompt?.isReview == true
  
  计划确认:
    type = plan
    条件: _pendingInteraction?.isPlan == true 或 hasPendingPlanQuestions
  
  回复:
    type = reply
    条件: _pendingPrompt != null 且不是 permission/review/plan
  
  继续输入:
    type = continueInput
    条件: _agentState?.awaitInput == true 且无 pendingPrompt

前端 AppNotificationSignal 分类（触发本地通知）:
  assistantReply: AI 有新的 Assistant 条目
  actionNeeded: 权限/审查/计划需要用户操作
  error: 运行错误
```

---

### 5.13 Permission Mode 影响链路

```
前端 _config.permissionMode 三种值:
  auto / default / bypassPermissions

影响点:
  1. 发送请求时注入到 payload.permissionMode
  2. 收到 diff 时决定是否自动 accept:
     bypassPermissions → shouldAutoAcceptReviewForPermissionMode() = true
     其他 → pending
  3. 后端 PtyRunner 启动时注入 --permission-mode 参数
  4. 权限规则自动匹配时参考
  5. 切换 mode 时自动 accept 所有待审 diff
```

---

---

## 六、对外接口一览

| 端点 | 方法 | 说明 |
|------|------|------|
| `/ws?token=xxx` | WebSocket | 核心通信通道，所有业务逻辑 |
| `/healthz` | HTTP GET | 健康检查，返回 `ok` |
| `/version` | HTTP GET | 版本/构建信息（JSON） |
| `/download?token=xxx&path=xxx` | HTTP GET | 文件下载 |
| `/api/tts/synthesize` | HTTP POST | 文本转语音 |
| `/api/tts/healthz` | HTTP GET | TTS 服务健康检查 |
| `/*` | HTTP GET | 嵌入式前端静态页面 |

---

## 七、部署与运行

### 6.1 后端启动

```text
必需: AUTH_TOKEN=xxx ./server
可选: APNS_AUTH_KEY_PATH + APNS_KEY_ID + APNS_TEAM_ID + APNS_TOPIC
可选: TTS_ENABLED=true + TTS_PYTHON_SERVICE_URL
```

### 6.2 端口映射

本地 8001 → SSH 反向隧道 → 远程服务器 8.162.1.176:1868（供手机公网访问）

### 6.3 iOS 客户端

通过 OTA 分发（`mobilevc-ota` 脚本打包、签名、上传），手机扫码安装。

---

## 八、目录树（完整）

```text
MobileVC/
├── cmd/server/main.go              # 后端入口
├── internal/
│   ├── adb/                        # ADB 控制（设备发现 + WebRTC 流）
│   ├── config/                     # 配置加载
│   ├── data/                       # 数据持久化
│   │   ├── claudesync/             # Claude JSONL 同步
│   │   ├── codexsync/              # Codex 线程同步
│   │   └── skills/                 # Skill 注册/启动
│   ├── engine/                     # 命令执行引擎（Exec + PTY）
│   ├── gateway/                    # WebSocket 网关
│   ├── logx/                       # 日志
│   ├── protocol/                   # 事件协议定义
│   ├── push/                       # APNs 推送
│   ├── session/                    # 会话管理核心
│   └── tts/                        # 文本转语音
├── mobile_vc/                      # Flutter 客户端
│   └── lib/
│       ├── app/                    # 应用级服务
│       ├── core/                   # 工具类
│       ├── data/models/            # 数据模型
│       ├── data/services/          # WebSocket/WebRTC 服务
│       ├── features/               # 功能模块（按领域划分）
│       └── widgets/                # 通用组件
├── tests/                          # 测试
│   └── regression/                 # 回归测试脚本
├── scripts/                        # 构建/部署脚本
├── claude-skills/                  # Claude Skill 定义文件
├── docs/                           # 文档
├── CLAUDE.md                       # Claude Code 项目规则
├── go.mod / go.sum                 # Go 依赖
└── package.json                    # Node 工具依赖
```
