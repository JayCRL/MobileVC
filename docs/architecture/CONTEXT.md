# CONTEXT

这份文件只记录**当前代码态**下最实用的上下文：主架构、关键 action / state 名称、以及容易踩坑的现状。

如果这里和代码冲突：**信代码，并更新这份文件。**

这里放的是**代码与当前实现事实**；用户偏好、外部部署流程、长期协作习惯放到 memory，不和这里重复。

## 1. 项目现在到底是什么

- `MobileVC` 不是终端复刻，而是把 Claude Code / Codex 一类长生命周期 AI CLI 工作流，重组为适合移动端的结构化交互。
- 主形态是：**Flutter 客户端 + Go WebSocket 后端 + 长生命周期 runner/runtime + 文件投影/会话存储**。
- 核心通道不是 REST，而是 **WebSocket 事件流**。
- Session、review、permission、plan、skill、memory、runtime info 都挂在同一条事件流/投影体系里。

## 2. 主架构一屏看懂

### Flutter

- 入口：`mobile_vc/lib/main.dart`
- App 装配：`mobile_vc/lib/app/app.dart`
- 状态中枢：`mobile_vc/lib/features/session/session_controller.dart`
- 主页面：`mobile_vc/lib/features/session/session_home_page.dart`

### Go

- 服务入口：`cmd/server/main.go`
- WebSocket 编排：`internal/ws/handler.go`
- Runtime 主实现：`internal/runtime/manager.go`
- 协议定义：`internal/protocol/event.go`
- 会话/投影存储：`internal/store/`

### 主链路

- Flutter 发 action
- `MobileVcWsService` 通过 WebSocket 送到 Go
- `ws.Handler` 分发到 session store / runtime service / runner / review / catalog 等模块
- Go 持续回推 event
- `SessionController` 把 event 派生成 UI 状态

可以直接把它理解成：

`SessionController -> WebSocket -> ws.Handler -> runtime.Service / store -> event stream -> SessionController`

## 3. 当前会话生命周期

### connect() 现在真实会做什么

`SessionController.connect()` 连接成功后，不是只等用户手动操作，而是会立刻：

1. `switchWorkingDirectory(_config.cwd)`
2. `requestRuntimeInfo('context')`
3. `requestSkillCatalog()`
4. `requestMemoryList()`
5. `requestSessionList()`
6. `requestAdbDevices()`
7. 如果当前已经选中 session，则发 `session_resume`，并带上 `lastSeenEventCursor` / `lastKnownRuntimeState`
8. 否则请求 `session_context` / `permission_rule_list` / `review_state`
9. 如果有通知指定的目标 session，再继续走 `loadSession(...)`

结论：**连接阶段已经是“主动补全上下文”的 bootstrap，不再是被动空连。**

补充两个现在很容易漏掉的点：

- 这里的“如果当前已经选中 session，则发 `session_resume`”同样适用于**断线重连**，不是只在手动恢复时才会发生。
- `switchWorkingDirectory(...)` 在已连接且 cwd 变化时，会主动补发一次 `session_list`，所以目录切换和会话列表刷新现在是绑定的。

### session 相关 action

当前最关键的 session action：

- `session_create`
- `session_list`
- `session_load`
- `session_resume`
- `session_delete`

### auto_bind 语义还在

前端自动绑会话时，会发送：

- `session_create` + `reason: "auto_bind"`
- 或 `session_load` + `reason: "auto_bind"`

后端如果发现当前连接已经有选中 session，会忽略这类自动绑定请求，避免重复切换。

### session_resume 不是“只读历史”

前端恢复会话时会带：

- `sessionId`
- `cwd`
- `lastSeenEventCursor`
- `lastKnownRuntimeState`

后端恢复时会：

1. 加载 session record
2. 切换到对应 runtime session
3. 重建 projection / history / review state
4. 按 `lastSeenEventCursor` 回放 pending events
5. 发出 `session_resume_result`

结论：**现在的 resume 是“历史恢复 + 运行态补齐 + 增量事件 replay”，不是旧式静态加载。**

### Flutter 侧的恢复语义也更强了

当前前端对“恢复会话”的理解，也不能再简化成“把旧 timeline 读出来”。

恢复后前端会同时重建和继续使用：

- timeline / terminal logs / terminal executions
- session context
- review state / pending diff
- runtime info / resumeRuntimeMeta
- continuation 能力（是否还能继续 input）

而且新建会话时，会主动清空旧 continuation / Claude mode / runtime info / terminal state，避免把上一个可恢复会话的状态误带到新会话。

## 4. Flutter 端当前真正的状态中枢

`SessionController` 是移动端事实上的总状态机。它统一管理：

- 连接/重连
- session 选择与恢复
- timeline
- prompt / interaction 阻塞态
- review diff 队列与组
- 文件列表 / 打开文件
- runtime info
- skill / memory / permission rule catalog
- adb / webrtc 状态
- action-needed 通知信号

### 关键状态名

这些名字值得记住，因为很多行为都是围绕它们派生的：

- `SessionConnectionStage`
- `_selectedSessionId`
- `_sessionEventCursors`
- `_pendingPrompt`
- `_pendingInteraction`
- `_pendingPlanQuestions`
- `_actionNeededSignal`
- `_notificationSignal`
- `_recentDiffs`
- `_reviewGroups`
- `_activeReviewGroupId`
- `_activeReviewDiffId`
- `_isStopping`
- `_sessionRuntimeAlive` — 标记后端 runtime 是否存活（原生会话/恢复态）。AgentStateEvent 中 `WAIT_INPUT` 现在也会保持为 `true`，不再错误清除运行态
- `_selectedSessionExternalNative` — 当前选中的会话是否为电脑端原生 Codex/Claude 会话。**断连时会重置为 `false`**，重连后由后端 `ownership` 字段重新决定
- `_continueSameSessionEnabled` — 用户是否已在手机端继续同一原生会话
- `_continuedSameSessionId` — 正在继续的原生会话 ID
- `_pendingOutboundActions` — 待确认的出站消息队列，带重试和 ack 确认
- `_clientActionSequence` — 客户端动作 ID 自增序列

### 关键派生 getter

- `awaitInput`
- `isSessionBusy`
- `hasPendingPermissionPrompt`
- `hasPendingPlanPrompt`
- `hasPendingPlanQuestions`
- `shouldShowPermissionChoices`
- `shouldShowPlanChoices`
- `shouldShowReviewChoices`
- `pendingDiffCount`
- `isObservingRemoteActiveSession` — 是否为观察模式（电脑端原生会话正在运行）
- `isSessionReadOnly` — 同 `isObservingRemoteActiveSession`，控制输入框锁定
- `shouldShowSessionObservationBanner` — 是否显示观察模式横幅
- `canContinueSameSession` — 观察模式下是否可以"继续同一会话"
- `canSendToContinuedSameSession` — 已继续同一会话后，输入框可交互

### 一个很重要的现状

`awaitInput` 现在**不只是**后端 agent state 的直译。

它还会受到这些本地状态影响：

- `_pendingPrompt`
- `_pendingInteraction`
- plan question 流程
- 对 Codex 来说，未 settled 的当前 execution 即使短暂进入 `WAIT_INPUT`，前端也会继续把它视为运行中，而不是立刻退出顶部 running banner

所以别再把“是否阻塞”简单理解成“后端说现在 awaitInput 吗”。

### 顶部 running banner 的当前语义

`activityVisible` / `activityBannerVisible` 不是简单跟着单个 `agent_state` 走。

当前正确理解是：

- Claude 启动但尚未真正进入执行前，可以显示静态”待输入”占位
- Codex 在当前 turn 尚未 settled 前，即使中途出现 `WAIT_INPUT`，顶部仍应保持”AI 助手正在运行中”的动画与耗时
- 一旦真正进入 permission / review / plan 等阻塞态，阻塞态仍优先，不能让 running banner 抢焦点
- 当 assistant 最终回复已经落到当前 execution，running banner 应立即退出，不必等额外 idle 事件
- **`_isStopping` 会影响 banner 标题/详情**：停止中时 banner 继续显示，标题为”正在停止”，详情为”等待后端确认停止...”，动画关闭
- **`isObservingRemoteActiveSession` 独立控制 banner**：观察模式下 banner 持续显示，标题为”观察模式”，详情为”正在同步电脑端原生会话进度”，无动画、不计耗时

### Claude / Codex 模式切换的当前理解

当前前端已经不是”只要进入 AI 模式，就一直把 Claude mode 粘住”。

更准确的理解是：

- Claude 支持启动后先进入静态待输入占位，再走后续 continuation
- `sendInputText(...)` 在 Claude continuation 场景下优先走 `input`，而不是重新发新的 `exec`
- stop 当前 Claude run 后，前端会立即退出对应的 Claude 待输入占位和 AI mode 粘滞态，后续普通命令重新按 shell 路径处理
- Codex / Claude 都依赖当前 execution 是否 settled，来决定 busy、stop、banner 和 continuation 的语义边界

### 停止按钮与 `_isStopping` 状态

**问题背景**：恢复正在执行操作的会话后点击停止按钮，状态栏立即消失但按钮仍为红色，用户体验不一致。

**`_isStopping` 机制**：

- 点击停止后，立即设置 `_isStopping = true`
- `canStopCurrentRun` 在 `_isStopping` 为 true 时返回 false（按钮立即变灰，防止重复点击）
- `activityBannerVisible` 包含 `_isStopping`，停止中状态栏继续显示
- `activityBannerTitle` 显示”正在停止”，`activityBannerDetail` 显示”等待后端确认停止...”
- 输入框在 `_isStopping` 时被锁定
- 收到后端 `stopped` 事件后，清除 `_isStopping` 标记，状态栏消失

## 5. 当前 UI/交互约束

### 阻塞优先级

当前前端阻塞优先级是：

`review > permission > plan > generic reply / WAIT_INPUT`

也就是说：

- 有 review 时，优先 review
- permission 不应该盖过 review
- generic WAIT_INPUT 不应该把现有更强 blocker 冲掉

### action-needed 是前端本地综合派生

`actionNeededSignal` 不是后端直接给的一个总开关，而是 `SessionController` 基于当前状态综合计算出来的，来源包括：

- pending review
- pending permission
- pending plan
- 一般 reply 需求
- 单纯 `WAIT_INPUT`

### 技能/记忆入口的现状

旧资料里如果提到“顶部 compact skill/memory capsule 是主入口”，现在不要再这么理解。

当前主交互模型里：

- skill / memory 仍然是会话级上下文系统
- 但启用/管理更偏向各自管理面板，而不是把顶部胶囊条当成核心主入口

## 6. Go 后端当前真正的运行模型

### backend 不是无状态 API

Go 后端的核心不是”收到一个请求，跑完就结束”。

而是：

- 管理长生命周期 runner
- 维护 session projection
- 维护 runtime 状态
- 按事件流持续向 Flutter 回推状态变化

### WebSocket ping/pong 保活机制

位于 `internal/ws/handler.go` 和 `internal/runner/pty_runner.go`：

- 后端处理 WebSocket ping/pong 消息，避免长时间空闲时连接超时
- 对旧版本 Claude CLI 的连接也能保持兼容
- 这是 6db330d “旧版本的最大化兼容” commit 引入的改动

### runtime.Service 的主要实现位置

一个特别容易踩坑的地方：

- `internal/runtime/service.go` 不是主要实现所在地
- `runtime.Service` 的核心实现现在在：`internal/runtime/manager.go`

所以以后查运行时主逻辑，优先看 `manager.go`。

### active runner 仍然是单实例约束

`manager.start(...)` 当前仍要求同一 runtime 下只有一个 active runner。

如果已经有 runner 在跑，又重复发 `exec`，会直接报：

- `another command is already running`

这条约束仍然是理解前后端交互的关键。

### SendInputOrResume 很关键

后端已经不是“只有 exec 才能继续会话”。

`SendInputOrResume(...)` 的语义是：

- 有 active runner：直接继续写入输入
- 没有 active runner 但可恢复：走 resume/hot-resume 路径

所以后续设计前端行为时，不要默认所有继续输入都必须重启一次 `exec`。

### runtime 已经是 session-scoped

现在的 runtime 不再只是“某个 websocket 连接临时持有的一段状态”。

当前后端已有：

- session-scoped runtime registry
- pending event buffer
- cursor-based replay

这意味着：**断线重连 / 多连接恢复 / session_resume 的语义，比旧文档里描述的更强。**

## 7. 权限授予与临时 grant 机制

### 核心问题

Claude 会话在 hot swap 后，如果没有临时授权机制，会重复弹权限确认，打断用户体验。

### 解决方案：临时 grant store

后端新增 `permissionGrantStore`，位于 `internal/ws/permission_grants.go`：

- **Issue(sessionID, targetPath, permissionRequestID, ttl)**：用户批准权限后，签发临时 grant，默认 TTL 60 秒
- **ConsumeIfValid(sessionID, targetPath, permissionRequestID)**：runner 执行前消费 grant，校验签名、过期时间与 requestID
- **Revoke(sessionID, targetPath)**：执行失败时撤销 grant

### 关键字段

- `permissionRequestID`：权限请求唯一标识，前后端通过此字段关联权限决策与执行
- `targetPath`：权限目标路径，经过 `filepath.Clean` 标准化
- `signature`：HMAC-SHA256 签名，防止伪造

### 权限决策流程

位于 `internal/ws/permission_decision.go`：

1. 前端发送 `permission_decision` action，携带 `permissionRequestID`、`targetPath`、`decision`
2. 后端合并 `RuntimeMeta`，优先使用 prompt/interaction 中的元信息
3. 如果是 Claude 会话且 decision 为 approve：
   - 签发临时 grant
   - 尝试 `HotSwapApproveWithTemporaryElevation`（当前 runner 存在）
   - 或 `HotSwapApproveFromResume`（从 resume session 恢复）
4. 如果 decision 为 deny：
   - 先发送决策到 runner（避免状态丢失）
   - 后端更新会话状态为 `ControllerStateWaitInput`，blockingKind 为 `ready`
   - 发送 `agent_state` 事件，标题为"已拒绝权限，可继续输入"

### 前端权限决策同步

位于 `mobile_vc/lib/features/session/session_controller.dart`：

- 权限决策时，前端现在会合并 `prompt.runtimeMeta` 与 `interaction.runtimeMeta`
- 发送 `permission_decision` 时携带完整的 `permissionRequestId`、`command`、`cwd`、`engine`、`target`、`targetType`
- 避免后端因元信息缺失导致权限结果与运行状态不一致

### 权限规则自动应用机制

**架构设计**：权限规则匹配与自动应用由**后端统一处理**，前端只负责响应结果。这是 2026-04-16 重大架构简化的结果。

**后端实现**：位于 `internal/ws/permission_rules.go:maybeAutoApplyPermissionEvent()`

- 收到 `PromptRequestEvent` 或 `InteractionRequestEvent` 时，后端自动检查会话规则和持久规则
- 通过 `looksLikePermissionPromptForRule` / `looksLikePermissionInteractionForRule` 判断是否为权限请求
- 如果规则匹配，直接调用 `executePermissionDecision` 处理逻辑，签发临时 grant 并触发热重载
- 发送 `PermissionAutoAppliedEvent` 通知前端规则已自动应用
- 如果规则不匹配，正常转发权限请求给前端

**匹配逻辑**：位于 `matchPermissionRule()`

- 按顺序检查：`Engine` → `Kind` → `CommandHead` → `TargetPathPrefix`
- 只有规则启用（`Enabled == true`）才参与匹配
- 匹配成功后更新 `MatchCount` 和 `LastMatchedAt`

**前端实现**：位于 `mobile_vc/lib/features/session/session_controller.dart`

1. 收到 `PromptRequestEvent` 时：
   - 设置 `_pendingPrompt = prompt`
   - 后端会自动检查权限规则并应用，前端只需等待 `PermissionAutoAppliedEvent`
   - 不再调用任何前端匹配逻辑

2. 收到 `PermissionAutoAppliedEvent` 时：
   - 清空 `_pendingPrompt` 和 `_pendingInteraction`
   - 显示自动应用消息（如 "已按会话权限规则自动允许"）
   - 刷新权限规则列表

3. 权限按钮显示逻辑（`_shouldHidePromptCard`）：
   - 只检查 `prompt == null` 和 `isBypassPermissionsMode`
   - 不再前端预判规则匹配，完全依赖后端 `PermissionAutoAppliedEvent`

**热重载提示简化**：

- `hotSwapApproveContinuation()` 从复杂的多行提示简化为 "已授权，继续"
- Claude 会话已恢复，权限已提升，无需冗长解释

**调试能力**：

- 前端新增 `_debugLogs` 队列（最多 200 条）
- 通过权限规则管理面板查看调试日志入口
- 记录权限规则匹配、自动应用等关键事件

**关键点**：

- 前端完全移除权限规则匹配逻辑（删除了 `_maybeAutoApplyPermissionRule`、`_wouldAutoApplyPermissionRule`、`_matchPermissionRule` 等方法）
- 前端只负责显示权限请求和响应后端的自动应用结果
- 规则匹配后，后端直接处理，前端收到 `PermissionAutoAppliedEvent` 后清空 pending 状态
- 规则不匹配时，前端正常显示权限按钮，等待用户手动授权
- 避免前后端重复实现导致的不一致问题

### permission-model-auto 二层授权架构

**背景**：当前后端在启动 Claude Code CLI 时传入 `--permission-mode auto`。这不是让 Claude 无条件放行所有操作，而是引入了两层机制：

**第一层：Claude Code 内置安全策略**

即使在 `auto` 模式下，以下高风险操作仍会触发授权提示：

| 操作类型 | 示例 |
|---|---|
| 递归强制删除 | `rm -rf` |
| 提权操作 | `sudo`、修改系统目录 |
| 全局包安装 | `npm install -g`、`pip install --user` |
| 敏感路径访问 | `~/.ssh/`、`~/.aws/`、`/etc/`（非 cwd） |
| 非安全网络请求 | 非 HTTPS 的 curl/wget、访问内网 IP |

**第二层：App Server 响应作用域**

当授权请求到达后端时，`permissionMode == "auto"` 与 `"acceptEdits"` 的核心区别在于批准作用域：

| 模式 | 批准作用域 | 效果 |
|---|---|---|
| `acceptEdits` | turn-scoped（单次） | 本次执行完即失效，下一个 turn 再次弹窗 |
| `auto` | session-scoped（会话级） | 整个会话期间有效，热重载后仍有效，任务中断率大幅降低 |

后端实现在 `internal/runner/codex_app_server.go`：

```go
func codexFileApprovalDecision(permissionMode, decision string) string {
    if decision == "deny" { return "decline" }
    if strings.TrimSpace(permissionMode) == "auto" { return "acceptForSession" }
    return "accept"
}
```

**总结**：在 auto 模式下，普通权限请求（读文件、运行命令等）由 Claude Code 内置白名单自动批准，无需到达后端；只有白名单之外的请求才触发授权流程，且批准后在本会话内持续有效。

## 8. 会话承接与首次输入

### 核心问题

用户连接后立即输入，但此时会话列表尚未同步完成，导致输入丢失或需要手动重试。

### 解决方案：延迟首次输入

前端新增 `_DeferredFirstInput` 机制：

- `_sessionListSyncedSinceConnect`：标记会话列表是否已同步
- `_shouldAutoCreateSessionOnFirstInput()`：判断是否需要自动创建会话
- `_deferFirstInputAndCreateSession(text)`：暂存输入，触发会话创建
- `_flushDeferredFirstInputIfNeeded()`：会话创建成功后，自动发送暂存的输入

### 流程

1. 用户连接后立即输入
2. 前端检测到 `_sessionListSyncedSinceConnect == false`
3. 暂存输入到 `_deferredFirstInput`，触发 `createSession()`
4. 收到 `session_create_result` 后，调用 `_flushDeferredFirstInputIfNeeded()`
5. 自动发送暂存的输入，用户无感知

### 边界处理

- 会话创建失败时，调用 `_clearDeferredFirstInput()` 清空暂存
- 断线重连时，清空暂存，避免误发旧输入

## 9. resumeSessionID 与会话恢复

### resumeSessionID 的作用

`resumeSessionID` 是 Claude/Codex 会话的持久化标识，用于跨 runner 生命周期恢复对话上下文。

### 来源优先级

位于 `internal/runtime/manager.go:resolveResumeSessionID()`：

1. `RuntimeMeta.Command` 中的 `--resume` 或 `--session-id` 参数
2. `RuntimeMeta.ResumeSessionID` 字段
3. 当前 runner 的 `ClaudeSessionID()`（如果实现了 `ClaudeSessionProvider`）
4. manager 快照中的 `ResumeSessionID`

### 命令行参数提取

- **Claude**：`--resume <sessionID>` 或 `--session-id <sessionID>`
- **Codex**：`codex resume <sessionID>` 或 `--resume <sessionID>`

### 关键函数

- `extractManagedClaudeSessionID(command, fallback)`：从命令行提取 session ID
- `extractResumeArg(command)`：提取 `--resume` 参数值
- `ensureResumeCommand(command, resumeSessionID)`：确保命令包含 resume 参数
- `stripResumeArg(command)`：移除 resume 参数，用于重新构建命令

## 10. 热重载（Hot Swap）机制

### 什么是热重载

热重载允许在不丢失会话上下文的情况下，重启 Claude runner 并继续对话。典型场景：

- 用户批准权限后，需要以新的 `permissionMode` 重启 runner
- 从 `resumeSessionID` 恢复会话并继续输入

### 三种热重载路径

位于 `internal/runtime/manager.go`：

#### 1. HotSwapApproveWithTemporaryElevation

**前提**：当前有 active runner

**流程**：
1. 调用 `buildHotSwapStreamRequest`，构建新的 stream 命令，**continuation 参数为 `"auto"`**（采用官方 permission-model-auto 模式）
2. 关闭当前 runner（`closeActiveAndWait`）
3. 启动新 runner，继承 `resumeSessionID`
4. 发送 continuation 输入（`"auto"`，而非旧的 `"acceptEdits"`）

#### 2. HotSwapApproveFromResume

**前提**：当前无 active runner，但有 `resumeSessionID`

**流程**：
1. 调用 `buildDetachedHotSwapStreamRequest`，从快照中提取 `resumeSessionID`，**continuation 参数为 `"auto"`**
2. 构建 `codex resume <sessionID>` 或 `claude --resume <sessionID>` 命令
3. 启动新 runner
4. 发送 `"auto"` continuation 输入

#### 3. buildHotSwapRequest（基础方法）

**核心逻辑**：
- 合并当前 `activeMeta` 与新请求的 `RuntimeMeta`
- 继承 `resumeSessionID`、`contextId`、`targetPath` 等字段
- 确保命令包含 `--resume` 参数

### 判断是否可以热重载

- `CanHotSwapClaudeSession(req)`：检查当前 runner 是否为 Claude 会话
- `HasResumeSession(req)`：检查是否有可用的 `resumeSessionID`

### 临时权限提升

- `temporaryElevated` 标志：标记当前 runner 是否处于临时提升权限状态
- `safePermissionMode`：记录安全的权限模式，用于热重载后恢复

## 11. runner 生命周期与 generation

### 核心问题

Claude 会话在 hot swap 或 resume 时，旧 runner 的 `defer clear()` 可能清空新 runner 的状态，导致后续输入失败。

### 解决方案：generation 机制

位于 `internal/runner/pty_runner.go`：

- `runGeneration uint64`：每次 `Run()` 递增
- `nextRunGeneration()`：原子递增并返回新 generation
- `clearGeneration(generation)`：只有 generation 匹配时才清空状态
- 所有 `defer r.clear()` 改为 `defer r.clearGeneration(generation)`

### 效果

- 旧 runner 退出时，不会误清空新 runner 的状态
- hot swap 后，新 runner 可以安全继承 `claudeSessionID`、`permissionMode` 等字段

## 10. 现在最该记住的 action / event / field

### 常用 action

- `session_create`
- `session_list`
- `session_load`
- `session_resume`
- `session_delete`
- `exec`
- `input`
- `stop`
- `permission_decision`
- `review_decision`
- `plan_decision`
- `session_context_get`
- `session_context_update`
- `skill_catalog_get`
- `memory_list`
- `skill_exec`
- `skill_sync_pull`
- `memory_sync_pull`

### 常用 event

- `session_history`
- `session_resume_result`
- `session_resume_notice`
- `prompt_request`
- `interaction_request`
- `agent_state`
- `runtime_phase`
- `review_state`
- `step_update`
- `file_diff`
- `session_context_result`
- `skill_catalog_result`
- `memory_list_result`
- `catalog_sync_status`
- `catalog_sync_result`
- `runtime_info_result`

### 常用字段

- `eventCursor`
- `lastSeenEventCursor`
- `lastKnownRuntimeState`
- `resumeSessionId`
- `executionId`
- `groupId`
- `groupTitle`
- `contextId`
- `contextTitle`
- `cwd`
- `permissionMode`
- `claudeLifecycle`
- `permissionRequestId`
- `targetPath`
- `runGeneration`
- `ownership` — 会话归属（`mobilevc` / `claude-native` / `codex-native`），Flutter 优先读此字段判断是否外部会话
- `executionActive` — 运行锁存器，非 IDLE 即为 `true`，用于切后台重连时保持运行态

## 11. 已确认的”旧索引 vs 现状”漂移点

这些是这次对比代码后，确认已经值得更新认知的点：

1. **resume 已升级**
   - 旧理解：恢复历史列表/时间线
   - 现状：history + projection + runtime recovery + event replay

2. **runtime 主实现位置容易看错**
   - 旧理解：`service.go` 是主入口实现
   - 现状：核心逻辑在 `internal/runtime/manager.go`

3. **connect bootstrap 更积极**
   - 旧理解：连上后等用户再拉各种信息
   - 现状：连接成功即切 cwd，并主动请求 runtime/catalog/session/device 等数据

4. **顶部 context capsule 不是当前核心 UI 模式**
   - 旧理解：主界面顶部 skill/memory 胶囊是主要启用入口
   - 现状：更应理解为管理面板驱动，顶部胶囊不该再当主路径记忆

5. **前端阻塞态是本地综合派生**
   - 旧理解：只看后端是否 awaitInput
   - 现状：还必须结合 `_pendingPrompt` / `_pendingInteraction` / review / plan 等本地派生态

6. **Claude mode / continuation 语义已经细化**
   - 旧理解：AI 模式只要进了就一直挂着，继续输入很多时候等价于重新 exec
   - 现状：Claude 启动前后区分静态待输入占位、活跃运行态与 continuation；继续输入优先走 `input` / resume 语义，stop 后也会及时退出粘滞 AI mode

7. **恢复态和新会话隔离更严格**
   - 旧理解：恢复旧会话后，新建会话只要切 session 即可
   - 现状：新建会话时还必须主动清空旧 continuation、runtime info、terminal logs/executions 与 Claude mode 残留，避免把恢复态误带入新会话

8. **权限授予不再重复弹窗**
   - 旧理解：Claude 会话每次 hot swap 都需要重新确认权限
   - 现状：引入临时 grant 机制，用户批准后 60 秒内自动续接，避免重复打断

9. **首次输入不再丢失**
   - 旧理解：连接后立即输入可能因会话未就绪而丢失
   - 现状：前端暂存首次输入，等会话列表同步完成后自动创建会话并发送

10. **runner 生命周期隔离更严格**
    - 旧理解：runner 退出时直接 clear 所有状态
    - 现状：引入 generation 机制，旧 runner 退出时不会误清空新 runner 的状态

11. **热重载续参已切换为 "auto"**
    - 旧理解：热重载后发送 `"acceptEdits"` 继续执行
    - 现状：采用 Claude Code 官方 `permission-model-auto` 模式，continuation 参数为 `"auto"`，修复了任务中断的 bug

12. **停止按钮有独立的 `_isStopping` 状态**
    - 旧理解：stop 后端即清空状态，按钮和 banner 状态不同步
    - 现状：`_isStopping` 标记隔离了"发起停止"和"确认停止"两个阶段，按钮立即变灰、banner 继续显示"正在停止"、输入框锁定

13. **权限拒绝后状态同步**
    - 旧理解：拒绝权限后可能状态不一致
    - 现状：后端拒绝时先发决策到 runner，再更新 ControllerStateWaitInput，发送"已拒绝权限，可继续输入"事件

14. **WebSocket 连接保活**
    - 旧理解：长时间空闲可能导致连接超时断开
    - 现状：后端处理 ping/pong 消息，保持连接活跃，并对旧版本 Claude CLI 最大化兼容

15. **切换目录会主动刷新会话列表**
    - 旧理解：切 cwd 只影响文件树，会话列表要额外手动刷新
    - 现状：前端 `switchWorkingDirectory(...)` 在连接态且目录发生变化时会直接 `requestSessionList()`

16. **重连会主动补发 session_resume**
    - 旧理解：后台断开后重新连上，只会重新拉基础列表，是否恢复上次会话取决于用户后续手动操作
    - 现状：只要 `_selectedSessionId` 还在，重连时会自动带着 cursor/runtime state 补发 `session_resume`

17. **新增客户端行为确认与去重机制**
    - 旧理解：前端发送消息后没有确认机制，后端可能重复处理
    - 现状：前端每条出站消息携带 `clientActionId`，后端去重后返回 `client_action_ack`；前端 `_PendingOutboundAction` 队列管理待确认消息并支持自动重发

18. **深色模式全面支持**
    - 旧理解：只有浅色主题，无法切换
    - 现状：`AppTheme.dark()` 提供完整深色主题，通过 `SharedPreferences` 持久化用户偏好，所有组件（事件卡、输入栏、面板）使用 theme-aware 颜色

19. **原生会话观察模式**
    - 旧理解：只能操作 MobileVC 自己创建的会话
    - 现状：可观察电脑端原生 Codex 会话实时进度，支持"继续同一会话"将输入发送到原生会话；观察模式时输入框只读、banner 显示同步状态

20. **Claude 启动流程重构（修复启动问题）**
    - 旧理解：Claude `Run()` 直接进入 select 等待
    - 现状：引入 `processDone` channel，先启动 goroutine 执行 `runClaudeStream`，循环等待 `interactive` 就绪后再发送 `PromptRequestEvent`，避免启动阶段状态不同步

21. **会话归属由 Ownership 字段决定**
    - 旧理解：是否外部会话取决于 `External` + `Source` + `Runtime.Source` 多重推导，切后台重连时可能误判
    - 现状：`SessionSummary.Ownership` 在创建时设为 `"mobilevc"`，只在桌面 Claude CLI 接管时升级为 `"claude-native"`，不受 projection 覆盖；Flutter `_isExternalNativeSession` 优先读此字段

22. **ExecutionActive 锁存运行态**
    - 旧理解：切后台回来时 `WAIT_INPUT` 会把 `_sessionRuntimeAlive` 置为 `false`，导致运行态指示跳动
    - 现状：后端 `ExecutionActive` 在非 IDLE 时锁存为 `true`；Flutter `AgentStateEvent` 把 `WAIT_INPUT` 纳入运行态保持列表；runtime session 超时释放时兜底解锁

23. **后台推送覆盖进度事件**
    - 旧理解：只在需要用户介入时（权限/审核）才发 APNs 推送
    - 现状：`AgentStateEvent`、`StepUpdateEvent`、`LogEvent`（assistant_reply）、`ErrorEvent` 也触发推送；进度类事件 30s 防抖；连接在线时跳过进度推送（WebSocket 已送达）

## 12. 更新这份文件时先看哪里

### Flutter source of truth

- `mobile_vc/lib/features/session/session_controller.dart`
- `mobile_vc/lib/features/session/session_home_page.dart`
- `mobile_vc/lib/data/models/session_models.dart`
- `mobile_vc/lib/data/models/events.dart`
- `mobile_vc/lib/app/theme.dart`
- `mobile_vc/lib/app/app.dart`
- `mobile_vc/lib/features/files/file_viewer_sheet.dart`

### Go source of truth

- `internal/ws/handler.go`
- `internal/ws/runtime_sessions.go`
- `internal/ws/push_helper.go`
- `internal/ws/permission_grants.go`
- `internal/ws/permission_decision.go`
- `internal/runtime/manager.go`
- `internal/runner/pty_runner.go`
- `internal/runner/codex_app_server.go`
- `internal/protocol/event.go`
- `internal/store/store.go`
- `internal/store/file_store.go`
- `internal/push/service.go`
- `internal/codexsync/threads.go`

### 只能当参考、不能高于代码

- `README.md`
- `PROJECT_INDEX.md`
- `blueprint.md`
- `BUGFIX_PLAN.md`

## 13. 维护这份文件的规则

只保留这些内容：

- 代码已证实的当前行为
- 高价值 action / state / event 名
- 会影响后续开发判断的架构约束
- 旧文档里最容易误导人的漂移点

不要继续往里塞：

- 产品宣传
- release note
- 大段历史故事
- 未来规划
- 过细测试命令
- 与主链路无关的边角子系统细节

## 14. 深色模式

### 架构

位于 `mobile_vc/lib/app/theme.dart` 和 `mobile_vc/lib/app/app.dart`：

- `AppTheme.dark()` 以 `Color(0xFF60A5FA)` 为种子色，`Brightness.dark` 创建深色配色
- `_build()` 统一构建方法，参数化 `scaffoldBackground`、`surface`、`snackBar` 和 `outlineAlpha`
- `MobileVcApp` 通过 `ThemeMode.dark/light` + `darkTheme` 实现双主题

### 持久化

- 使用 `SharedPreferences` 存储 `mobilevc.dark_mode_enabled` 键
- `_loadThemeMode()` 在 `_startApp()` 中尽早加载，避免亮屏闪烁
- `_toggleThemeMode()` 切换状态并持久化

### 组件适配

所有硬编码白色背景已替换为 theme-aware 颜色：
- `event_card.dart`：`Colors.white` → `scheme.surfaceContainerLowest`
- `command_input_bar.dart`：硬编码 `#FFF7F8FC` → `scheme.surfaceContainerHighest`，阴影使用 `scheme.shadow`
- 文本颜色使用 `scheme.onSurface` 而非固定 `#0F172A`

### 切换入口

`SessionHomePage` 顶栏右侧新增主题切换图标按钮，tooltip 动态显示"切换深色模式"/"切换浅色模式"

## 15. 客户端行为确认与去重（Client Action Ack）

### 问题背景

前端发送 exec/input 等消息后，可能因为网络闪断、重连等场景导致：
- 后端收到但前端未收到确认，前端重发导致重复执行
- 后端未收到但前端认为已发送，消息静默丢失

### 解决方案：clientActionId + ack 机制

**前端**：`session_controller.dart`

- `_sendUserVisibleAction()`：发送前生成 `clientActionId`（时间戳+自增序列），附加到 payload
- `_PendingOutboundAction`：记录每条出站消息的 payload、userText、label、sendAttempts、displayed
- `_flushPendingOutboundActions()`：重连后自动补发未确认的排队消息
- `_handleClientActionAck()`：收到后端 `client_action_ack` 后移除已确认消息
- `_schedulePendingOutboundRetry()`：6 秒超时未确认则自动重试
- `MobileVcWsService.send()` 改为返回 `bool`，发送失败时推 `ErrorEvent`

**后端**：`internal/ws/handler.go` + `internal/ws/runtime_sessions.go`

- `ackClientAction()`：解析 `clientActionId`，调用 `sessionRuntime.markClientAction()` 去重
- 返回 `ClientActionAckEvent`，携带 `action`、`clientActionId`、`status`、`duplicate` 字段
- `markPersisted()`：事件持久化后标记，防止 replay 时重复处理

**协议**：`internal/protocol/event.go` + `mobile_vc/lib/data/models/events.dart`

- 新增 `ClientEvent.SessionID` 和 `ClientEvent.ClientActionID` 字段
- 新增 `ClientActionAckEvent` 类型（type = `client_action_ack`）
- `InputRequestEvent` 增加 `RuntimeMeta` 嵌入

### 效果

- 网络断开期间的消息自动排队，恢复后补发
- 后端自动去重，避免重复执行
- 用户可见的消息如果补发成功会推系统消息"已自动补发 N 条排队消息"

## 16. 原生会话观察模式（Native Session Observation）

### 问题背景

电脑端原生 Codex 会话被同步到手机端后，用户只能看到历史记录，无法实时跟踪执行进度，也无法从手机端继续输入。

### 状态字段

位于 `session_controller.dart`：

- `_sessionRuntimeAlive`：后端 runtime 是否存活（来自 `SessionHistoryEvent.runtimeAlive` / `SessionDeltaEvent.runtimeAlive`）
- `_selectedSessionExternalNative`：当前会话是否是电脑端原生 Codex 会话
- `_continueSameSessionEnabled`：用户已确认"继续同一会话"
- `_continuedSameSessionId`：正在继续的原生会话 ID
- `_lastContinuationInputAt`：上次继续输入的时间，用于 8 秒内抑制 banner 动画

### 观察模式行为

- `isObservingRemoteActiveSession` = `_selectedSessionExternalNative && !_continueSameSessionEnabled`
- `isSessionReadOnly` 锁定输入框，显示"电脑端原生会话正在运行，手机端当前为观察模式"
- `activityBannerVisible` 在观察模式下恒为 true，标题"观察模式"，详情"正在同步电脑端原生会话进度"
- `activityBannerAnimated` 在观察模式下为 false（无旋转动画）
- `shouldShowSessionObservationBanner` 综合判断显示观察模式横幅

### 继续同一会话

- 用户点击"继续"后，设置 `_continueSameSessionEnabled = true`
- `canSendToContinuedSameSession` 允许输入框交互
- 输入通过 `_sendUserVisibleAction` 发送到后端，`sessionId` 指向原生会话
- banner 标题变为"已在手机继续同一会话"，详情提示"请避免同时在电脑端原生终端输入"

### 周期性同步

- `_observedSessionSyncTimer`：每 3 秒请求一次 `_requestSessionDelta()`
- `_syncObservedSessionPolling()`：切到前台时启动，切到后台时停止
- 用于实时拉取原生会话的最新执行输出

## 17. 回复去重（Assistant Dedup）

### 问题背景

Codex 的 app-server 协议中，同一段 assistant 回复可能通过多个通道到达（delta 流 + turn_completed + item/completed），导致前端重复显示。

### 解决方案

位于 `internal/runner/codex_app_server.go`：

**`emitAssistantChunk(text)`**：
- `lastAssistantText`：缓存上一次发出的文本片段
- 如果当前 text 与 `lastAssistantText` 相同 → 跳过
- 如果当前 text 包含 `lastAssistantText` 且长度增长在合理范围 → 更新缓存但不发送，认为是同一段回复的增量
- 否则视为新内容，发送 `LogEvent`

**`emitAssistantCompletedText(text)`**：
- 对比 `assistantEmitted`（已发出的完整文本）与当前完成事件文本
- 如果完全相同 → 跳过
- 如果完成文本以已发出文本开头 → 剥离已发出部分，只发增量
- 避免 `rawResponseItem/completed` 与 delta 流重复

**`emitReadyPromptAfterReply()`**：
- `readyPromptSeq` 自增序号 + 150ms 定时器去重
- 多个完成事件同时触发时，只发最后一次 `prompt_request`，避免前端收到多个"等待输入"

### 后端 runner 启动修复

位于 `internal/runner/pty_runner.go`：

- Claude `Run()` 启动时先发 `AgentStateEvent("检查环境...")`
- 启动 goroutine 执行 `runClaudeStream`，主循环等待 `interactive` 就绪
- 就绪后再发送 `PromptRequestEvent("等待输入")`
- `lazyStart` 从 `true` 改为 `false`，不再延迟初始化
- 新增 `sendLazyReadyPrompt()` 在 `claude.Run` 启动时提前发送就绪信号

### 协议新增事件

- `PongEvent`（type = `pong`）：Flutter 端解析后端 ping/pong
- `CatalogAuthoringResultEvent`（type = `catalog_authoring_result`）：skill/memory 编辑结果通知

### CodexSync 改进

位于 `internal/codexsync/threads.go`：

- `seenMessages` map 去重，避免原生同步时重复插入相同消息
- 支持 `rolloutResponseItemPayload` 类型（`response_item` 事件），提取完整 response 文本
- `responseItemText()` 从 `content[]` 中拼接文本

## 18. 会话归属与运行状态锁存器

### Ownership — 会话归属

**问题**：之前"这个会话属于谁"的判断分散在 `External`、`Source`、`Runtime.Source` 三个字段里，通过多重推导得出。切后台回来时可能误判为外部会话，进入观察模式。

**字段**：`store.SessionSummary.Ownership`（`store/store.go:152`）

**值**：
- `"mobilevc"` — 手机端创建并拥有
- `"claude-native"` — 桌面 Claude CLI 接管（在 `mergeClaudeJSONLToRecord` 中升级）
- `"codex-native"` — Codex 接管

**生命周期**：
- `CreateSession` → `"mobilevc"`（`file_store.go:78`）
- `mergeClaudeJSONLToRecord` 发现桌面 Claude CLI 写入了新 JSONL 条目 → 升级为 `"claude-native"`（`handler.go:5269`）
- `normalizeSessionRecord` 只在 Ownership 为空时设默认值，已有值**不会被覆盖降级**

**Flutter 侧**：
- `_isExternalNativeSession()` 优先读 `ownership`：`"mobilevc"` → 直接返回 `false`；`"claude-native"` / `"codex-native"` → 直接返回 `true`
- 断连时 `_handleUnexpectedSocketDisconnect` **重置** `_selectedSessionExternalNative = false`
- `_mergedSessionSummary` 合并时 incoming 优先，否则保留 existing

### ExecutionActive — 运行状态锁存器

**问题**：`AgentStateEvent` 处理中 `WAIT_INPUT` 会把 `_sessionRuntimeAlive` 置为 `false`，导致切后台回来时状态在"运行中"↔"等待输入"之间来回跳动。

**字段**：`store.SessionSummary.ExecutionActive`（`store/store.go:155`）

**触发（→ true）**：控制器状态变为非 IDLE（THINKING、WAIT_INPUT、RUNNING_TOOL 均视为活跃）

**解锁（→ false）**：
1. 控制器状态变为 IDLE（`OnCommandFinished` 正常结束）
2. Runtime session 超时释放（`runtime_sessions.go` `cleanupIfOrphaned` → `onCleanup` 回调）
3. Runtime session 立即释放（切会话时 `Release(..., true)`）
4. 全局清理（`CleanupAll`）

**注意**：`OnCommandFinished` 在 `WAIT_INPUT` 状态下有短路——runner 退出时如果当前状态是 `WAIT_INPUT`（等待用户继续），不会切到 IDLE，`ExecutionActive` 保持 `true`。此时如果用户超时 15 分钟不回来，runtime session 超时释放 → `onCleanup` 兜底解锁。

**Flutter 侧**：
- `AgentStateEvent` 处理中 `WAIT_INPUT` 加入 `_sessionRuntimeAlive` 保持列表

## 19. 后台推送通知扩展

### 问题

之前只在 `prompt_request` / `interaction_request`（需要用户确认权限/审核代码）时才发 APNs 推送。切后台后 agent 思考、执行工具的进度完全不通知。

### 推送触发事件（`push_helper.go`）

| 事件 | 推送内容 | 防抖 |
|---|---|---|
| `PromptRequestEvent` | 需要用户确认权限 | 无（立即） |
| `InteractionRequestEvent` | 需要用户审核代码变更 | 无（立即） |
| `AgentStateEvent` (THINKING/RUNNING_TOOL) | "AI 助手运行中 / 思考中 / 执行工具中" | 30s |
| `StepUpdateEvent` | "正在执行: <工具名>" | 30s |
| `LogEvent` (assistant_reply) | 截取回复前 200 字符 | 30s |
| `ErrorEvent` | 错误信息 | 无（立即） |

**连接感知**：`runtimeSessionRegistry.HasActiveConnection(sessionID)` 检测 Flutter 是否在线。在线时只发需要用户介入的推送（WebSocket 已推送进度）；离线时才发进度推送。

**防抖**：`Handler.lastProgressPush` map 按 session 记录上次推送时间，30 秒内不重复推送进度类事件。

### 关键位置

- `internal/ws/push_helper.go` — 推送判断与发送
- `internal/ws/runtime_sessions.go` — `HasActiveConnection()` 连接检测；`onCleanup` 释放回调
- `internal/push/service.go` — APNs 客户端实现
