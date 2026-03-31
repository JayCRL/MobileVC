# MobileVC 代码蓝图：Flutter 发送链路与 Skill/Memory 同步链路

本文档分两部分：

1. 当前代码里的真实逻辑
2. 已确认的目标蓝图（下一阶段重构方向）

重点围绕两条主线：

- Flutter 端用户发送消息后，到 Go 后端的执行/输入链路
- Flutter 与 Go 端的 Skill / Memory 获取与同步链路

---

## 1. 整体定位

当前项目的主交互通道是 WebSocket 事件流。

- Flutter 入口状态中枢：`mobile_vc/lib/features/session/session_controller.dart`
- Flutter WS 发送层：`mobile_vc/lib/data/services/mobilevc_ws_service.dart`
- Go WS 分发层：`internal/ws/handler.go`
- Go 运行时编排层：`internal/runtime/manager.go`

其中：

- Flutter 负责把 UI 行为转换成 `action=exec/input/permission_decision/review_decision/...`
- Go 后端负责维护当前活跃 runner、Claude PTY 会话、resume 信息、session projection，以及 Skill/Memory catalog

---

## 2. 当前代码：Flutter 端发送消息主链路

### 2.1 发送入口

用户在 Flutter 端发送普通文本，入口是：

- `mobile_vc/lib/features/session/session_controller.dart:1253`
- 方法：`sendInputText(String text)`

这个方法先做一层状态分流：

1. 会话切换中：直接拦截
2. 当前有 permission prompt：拦截，要求先授权
3. 当前有 plan prompt / plan questions：拦截，要求先完成 plan 决策
4. 当前 `awaitInput == true`：走 `_submitAwaitingPrompt(value)`
5. 当前是 slash command：走 `_handleSlashCommand(value)`
6. 当前 `inClaudeMode == true`：走 `_submitAwaitingInput(value)`
7. 不是 Claude 命令：走普通 `exec`
8. 是 Claude 命令：走 `_startClaudeTurn(...)`

### 2.2 `awaitInput` 为真时

当 Flutter 判断当前处于等待输入态时，发送逻辑会走：

- `mobile_vc/lib/features/session/session_controller.dart:1360`
- `_submitAwaitingPrompt(...)`

再进一步落到：

- `mobile_vc/lib/features/session/session_controller.dart:1568`
- `_submitAwaitingInput(...)`

这个路径只发一条 WebSocket 消息：

```json
{
  "action": "input",
  "data": "<user text>\n",
  "permissionMode": "..."
}
```

也就是说，**当前会话被 Flutter 认定为可继续输入时，前端不会重新 `exec claude`，而是只发 `input`。**

### 2.3 新开一轮 Claude 请求时

如果 Flutter 没走 `awaitInput`，且最终落到 Claude 逻辑，会走：

- `mobile_vc/lib/features/session/session_controller.dart:1147`
- `_startClaudeTurn(...)`

这里的行为是固定两段式：

1. 先发送：

```json
{
  "action": "exec",
  "cmd": "claude",
  "cwd": "...",
  "mode": "pty",
  "permissionMode": "...",
  "...runtimeMeta"
}
```

2. 如果 prompt 非空，再立即发送：

```json
{
  "action": "input",
  "data": "<prompt>\n",
  "cwd": "...",
  "permissionMode": "...",
  "...runtimeMeta"
}
```

对应代码：

- `mobile_vc/lib/features/session/session_controller.dart:1160-1178`

因此，**Flutter 当前对 Claude 的启动方式不是单消息，而是 `exec(claude)` + `input(prompt)` 的双发模式。**

### 2.4 `inClaudeMode` 的判定

Flutter 是否认为当前已经在 Claude 会话里，取决于：

- `mobile_vc/lib/features/session/session_controller.dart:489-495`

```dart
final command = currentMeta.command.trim().toLowerCase();
return command == 'claude' || command.startsWith('claude ');
```

也就是说，前端是通过 `currentMeta.command` 是否像 `claude ...` 来推断自己是否仍在 Claude 模式里。

---

## 3. 当前代码：Go 后端执行 / 输入链路

### 3.1 WebSocket 分发入口

后端所有动作从这里进入：

- `internal/ws/handler.go`
- 主 `switch clientEvent.Action`

和发送链路相关的 action 主要有：

- `exec`：`internal/ws/handler.go:529`
- `input`：`internal/ws/handler.go:582`
- `permission_decision`：`internal/ws/handler.go:709`
- `review_decision`：`internal/ws/handler.go:810`

### 3.2 `exec` 的后端逻辑

当收到 `action=exec` 时：

- `internal/ws/handler.go:529-581`

后端会：

1. 解析请求
2. 记录用户投影日志 `appendUserProjectionEntry(...)`
3. 调用 `runtime.Service.Execute(...)`

#### `Execute(...)` 做了什么

位置：

- `internal/runtime/manager.go:181-238`

逻辑：

1. 选 runner（PTY / Exec）
2. `prepareExecuteRequest(req)` 预处理命令
3. `manager.start(...)` 把 runner 注册成当前 active runner
4. 触发 `controller.OnExecStart(...)`
5. 异步执行 runner
6. 处理 runner 输出事件，映射为协议事件，再写回前端
7. 结束时 `finishIfCurrent(...)`

#### `prepareExecuteRequest(...)` 的特殊逻辑

位置：

- `internal/runtime/manager.go:554-584`

如果是 Claude PTY，会自动补充受管 session id：

- 若命令里没有 `--session-id`
- 且 runtime meta 里没有现成 resume id
- 则自动生成 managed Claude session id
- 并把命令改成：
  - `claude --session-id <generated-id>`

这说明：**后端不会原样执行前端传来的纯 `claude`，而是会把它包装成受控 Claude 会话。**

### 3.3 `another command is already running` 的来源

位置：

- `internal/runtime/manager.go:41-56`

`manager.start(...)` 内部逻辑：

```go
if m.activeRunner != nil {
    return errors.New("another command is already running")
}
```

也就是说：

- 只要当前 runtime manager 里还有 active runner
- 又来了新的 `Execute(...)`
- 就会直接拒绝并报这个错误

因此，只要 Flutter 在 active Claude 会话仍存在时再次发送 `action=exec`，就会命中这里。

---

## 4. 当前代码：Go 后端 `input` 的真实能力

### 4.1 `input` 入口

位置：

- `internal/ws/handler.go:582-708`

收到 `action=input` 后，后端并不是简单写 stdin，而是会：

1. 记录投影日志
2. 更新 permission mode（如果请求里带了）
3. 读取 controller / projection / runtime snapshot
4. 如果处于 temporary elevated 模式，先恢复安全权限模式
5. 构造 `resumeReq`
6. 调用 `service.SendInputOrResume(...)`

### 4.2 `SendInputOrResume(...)`

位置：

- `internal/runtime/manager.go:278-312`

逻辑顺序：

1. 先尝试 `SendInput(...)`
2. 如果成功，直接结束
3. 如果失败且不是 `ErrNoActiveRunner`，直接返回错误
4. 如果没有 active runner，但当前是 Claude PTY 且存在 resume session
5. 自动构造 detached hot-swap stream request
6. 调用 `Execute(...)` 恢复 Claude runner
7. 等 runner ready
8. 再把这次 input 发进去

这意味着：

**后端原生支持 “input 优先，必要时自动 resume Claude 会话”。**

也就是说，从 Go 端能力看，普通用户继续发话，理论上不一定需要前端先主动发 `exec claude`。

---

## 5. 当前代码：发送链路的结构性特征

### 5.1 Flutter 有两种发消息方式

#### 方式 A：只发 `input`
触发条件：

- `awaitInput == true`
- 或 `inClaudeMode == true`

走向：

- `_submitAwaitingInput(...)`
- `action=input`

#### 方式 B：`exec + input` 双发
触发条件：

- Flutter 认为当前不是可继续输入态
- 且用户意图是 Claude 对话

走向：

- `_startClaudeTurn(...)`
- 先 `action=exec cmd=claude`
- 再 `action=input data=<prompt>`

### 5.2 Go 端只允许同时存在一个 active runner

只要 active runner 没结束，再来新的 `exec` 就会报：

- `another command is already running`

### 5.3 前后端的能力边界并不完全一致

- Flutter 偏向于“开启一轮 Claude 请求 = 先 exec 再 input”
- Go 偏向于“能直接 input 就 input；没有 active runner 才尝试 resume”

因此，当前代码形态下，**重复触发 `exec claude` 很容易与后端 active runner 管理相撞。**

---

## 6. 当前代码：权限 / 审核 / 计划问题对发送链路的影响

### 6.1 权限决策

Flutter：

- `_sendPermissionDecision(...)`：`mobile_vc/lib/features/session/session_controller.dart:1580`
- `_sendInteractionDecision(...)`：`mobile_vc/lib/features/session/session_controller.dart:1528`

后端：

- `internal/ws/handler.go:709-809`

当前 approve 权限并不是直接给底层 runner 一个布尔确认，而是：

1. 构造热重启请求
2. 切到临时 `acceptEdits`
3. 恢复/重启 Claude runner
4. 回灌 continuation prompt

也就是：

- `HotSwapApproveWithTemporaryElevation(...)`
- `HotSwapApproveFromResume(...)`

位置：

- `internal/runtime/manager.go:356-399`

### 6.2 审核决策

Flutter 发：

- `action=review_decision`

后端：

- `internal/ws/handler.go:810-878`
- 最终走 `service.ReviewDecision(...)`
- 再被转换成 `1\n / 2\n / 3\n` 这样的交互输入

位置：

- `internal/runtime/manager.go:452-465`

### 6.3 计划决策

Flutter 发：

- `action=plan_decision`

后端：

- `internal/runtime/manager.go:467-482`

本质上仍然是把 plan 结果作为输入继续送回当前 Claude 会话。

结论：

**permission / review / plan 都是“结构化 UI 决策 -> 回灌当前 Claude 会话”，而不是另开一条独立执行链。**

---

## 7. 当前代码：Skill / Memory 的数据来源与状态模型

### 7.1 Flutter 端的请求入口

`SessionController` 中相关方法：

- 请求技能列表：`requestSkillCatalog()`
  - `mobile_vc/lib/features/session/session_controller.dart:861-863`
- 同步技能：`syncSkills()`
  - `mobile_vc/lib/features/session/session_controller.dart:949-951`
- 请求记忆列表：`requestMemoryList()`
  - `mobile_vc/lib/features/session/session_controller.dart:957-959`
- 同步记忆：`syncMemories()`
  - `mobile_vc/lib/features/session/session_controller.dart:953-955`

Flutter 这里只负责发送 action：

- `skill_catalog_get`
- `skill_sync_pull`
- `memory_list`
- `memory_sync_pull`

### 7.2 Flutter 端的接收逻辑

处理事件位置：

- `mobile_vc/lib/features/session/session_controller.dart:2038-2084`

其中：

- `SkillCatalogResultEvent`：直接覆盖 `_skills`
- `MemoryListResultEvent`：直接覆盖 `_memoryItems`
- `CatalogSyncStatusEvent`：只更新 sync 状态文案
- `CatalogSyncResultEvent`：更新 sync meta 与提示文案

也就是说：

**Flutter 对 Skill / Memory 结果基本不做额外裁剪，主要是后端给什么它就显示什么。**

### 7.3 Flutter 端数据模型

位置：

- `mobile_vc/lib/data/models/session_models.dart:318-494`

#### SkillDefinition
字段包含：

- `name`
- `description`
- `prompt`
- `resultView`
- `targetType`
- `source`
- `sourceOfTruth`
- `syncState`
- `editable`
- `driftDetected`
- `updatedAt`
- `lastSyncedAt`

#### MemoryItem
字段包含：

- `id`
- `title`
- `content`
- `source`
- `sourceOfTruth`
- `syncState`
- `editable`
- `driftDetected`
- `updatedAt`
- `lastSyncedAt`

#### CatalogMetadata
字段包含：

- `domain`
- `sourceOfTruth`
- `syncState`
- `driftDetected`
- `lastSyncedAt`
- `versionToken`
- `lastError`

这说明 Flutter 的模型本身是支持展示完整 catalog 元数据的。

---

## 8. 当前代码：Go 后端 Skill / Memory 获取链路

### 8.1 初始连接时就会发 catalog

在 WS 连接建立后，handler 会立即发：

- `emitSkillCatalogResult(...)`
- `emitMemoryListResult(...)`

位置：

- `internal/ws/handler.go:250-255`

因此，Flutter 一连上后端，即使用户没有手动点同步，也会先拿到当前 session store 中已有的 skill/memory catalog 快照。

### 8.2 `skill_catalog_get`

位置：

- `internal/ws/handler.go:432-433`

实际调用：

- `emitSkillCatalogResult(...)`：`internal/ws/handler.go:1295-1306`

逻辑只是：

1. 从 `sessionStore.GetSkillCatalogSnapshot(ctx)` 读快照
2. 转成协议事件
3. 发给前端

### 8.3 `memory_list`

位置：

- `internal/ws/handler.go:482-483`

实际调用：

- `emitMemoryListResult(...)`：`internal/ws/handler.go:1308-1319`

逻辑同上：

1. 从 `sessionStore.GetMemoryCatalogSnapshot(ctx)` 读快照
2. 转成协议事件
3. 发给前端

结论：

**`skill_catalog_get` 和 `memory_list` 读取的是本地 session store 的 catalog snapshot，而不是外部系统实时扫描。**

---

## 9. 当前代码：Go 后端 Skill / Memory 同步链路

### 9.1 `skill_sync_pull`

入口：

- `internal/ws/handler.go:447-481`

流程：

1. 读取 skill catalog snapshot
2. 把 meta 改成 `syncing`
3. 发 `catalog_sync_status`
4. 调 `syncExternalSkills(...)`
5. 成功后发：
   - `skill_sync_result`
   - `catalog_sync_result`
   - `skill_catalog_result`

#### `syncExternalSkills(...)`

位置：

- `internal/ws/handler.go:1364-1401`

当前逻辑不是去外部真实同步，而是：

1. 读取已有 snapshot
2. 只保留 `item.Source == store.SkillSourceLocal` 的本地 skill
3. 再追加一个硬编码 external skill：
   - `external-diff-summary`
4. 把整个 snapshot 标记为 `synced`
5. 保存回 store

硬编码 skill 的内容：

- name: `external-diff-summary`
- description: `外部同步示例 skill`
- resultView: `review-card`
- targetType: `diff`
- source: `external`

结论：

**当前 `skill_sync_pull` 是 stub / mock 型同步，不是真正对 Claude 外部 skill 系统的全量同步。**

### 9.2 `memory_sync_pull`

入口：

- `internal/ws/handler.go:484-516`

流程：

1. 读取 memory catalog snapshot
2. 把 meta 改成 `syncing`
3. 发 `catalog_sync_status`
4. 调 `syncExternalMemories(...)`
5. 成功后发：
   - `catalog_sync_result`
   - `memory_list_result`

#### `syncExternalMemories(...)`

位置：

- `internal/ws/handler.go:1448-1475`

当前逻辑：

1. 读取已有 snapshot
2. 遍历 snapshot.Items
3. 只保留 `item.Source == "local"` 的 memory
4. 对这些 local memory 打上：
   - `SourceOfTruth = claude`
   - `SyncState = draft`
   - `Editable = true`
   - `DriftDetected = true`
5. 不会新增任何真正的 external memory
6. 把 snapshot meta 标记成 `synced`
7. 保存回 store

结论：

**当前 `memory_sync_pull` 并没有真正从 Claude 的 memory 文件系统或外部目录拉取全量 memory，而只是把本地 catalog 中已有 local 项重新整理后回写。**

---

## 10. 当前代码：Catalog authoring 闭环

除了手动同步，当前项目还有一条“让 Claude 生成 skill/memory JSON，再自动回写 catalog”的链路。

### 10.1 Flutter 发起 authoring 请求

位置：

- `mobile_vc/lib/features/session/session_controller.dart:1109-1145`

`_dispatchContextualClaudeRequest(...)` 会把 runtime meta 的 `source` 设成：

- `catalog-authoring`

然后继续走 `_startClaudeTurn(...)`。

### 10.2 PTY runner 识别 Claude 返回的结构化 JSON

位置：

- `internal/runner/pty_runner.go:1448-1520`

关键方法：

- `tryEmitCatalogAuthoringResult(...)`
- `parseCatalogAuthoringPayload(...)`

只有当：

- `pendingReq.Source == "catalog-authoring"`

并且 Claude 输出满足：

- `mobilevcCatalogAuthoring == true`
- `kind == "skill"` 或 `kind == "memory"`

才会被解析成：

- `CatalogAuthoringResultEvent`

### 10.3 handler 自动写回 store

位置：

- `internal/ws/handler.go:202-240`

在 `emitAndPersistFor(...)` 里，遇到 `CatalogAuthoringResultEvent`：

- 如果是 skill：`upsertLocalSkill(...)`，然后 `emitSkillCatalogResult(...)`
- 如果是 memory：`upsertMemoryItem(...)`，然后 `emitMemoryListResult(...)`

### 10.4 本地 upsert 的元数据

#### `upsertLocalSkill(...)`
位置：

- `internal/ws/handler.go:1321-1362`

写入后属性是：

- `Source = local`
- `SourceOfTruth = claude`
- `SyncState = draft`
- `Editable = true`
- `DriftDetected = true`

#### `upsertMemoryItem(...)`
位置：

- `internal/ws/handler.go:1403-1445`

写入后属性是：

- `Source = local`（默认）
- `SourceOfTruth = claude`
- `SyncState = draft`
- `Editable = true`
- `DriftDetected = true`

结论：

**当前项目的 skill/memory authoring 闭环是真实可写的，但 sync_pull 仍是占位型同步。**

---

## 11. 已确认的目标蓝图

这一节记录已经明确的目标方向，用于后续重构对齐。

### 11.1 Claude 模式改为三态，而不是继续依赖推断

目标：Flutter 不再主要依赖 `currentMeta.command`、`awaitInput` 之类的派生条件去猜“自己是不是在 Claude 里”，而是维护一个显式三态。

建议语义如下：

1. `shell`
   - 当前不在 Claude 会话里
   - 普通用户输入默认走 shell / `exec`

2. `claude_busy`
   - 当前已经进入 Claude 会话
   - Claude 正在思考、跑工具、处理中
   - 普通文本不应再走新的 `exec claude`
   - 应等待 Claude 进入可输入态，或继续走 permission/review/plan 的专属链路

3. `claude_wait_input`
   - 当前已经进入 Claude 会话
   - Claude 正等待用户继续输入
   - 普通文本应优先走 `input`

这个三态是会话级显式状态，不再建议继续依赖当前的“命令长得像 claude 就算 Claude mode”。

### 11.2 Claude 进入与退出由显式动作驱动

目标：

- 用户主动输入 `claude` 或通过显式入口进入 Claude，会把状态切到 Claude 三态之一
- 用户可以在 Flutter 端显式“退出 Claude”
- 退出后状态恢复到 `shell`

也就是说，Claude 模式不再只是被动从日志/事件推断，而是由“进入 Claude / 退出 Claude”两个明确动作驱动。

### 11.3 发送分流改为按三态走固定线路

目标分流：

#### 当状态为 `shell`
- 普通命令：走 `exec`
- 用户明确输入 `claude`：启动 Claude 会话

#### 当状态为 `claude_busy`
- 不再重复发新的 `exec claude`
- permission / review / plan 仍走专属 action
- 普通文本默认不直接当成新 shell exec

#### 当状态为 `claude_wait_input`
- 普通文本优先走 `input`
- 文件继续处理、catalog authoring 等 Claude 上下文行为，也优先走当前 Claude 会话的 `input` / 专属 action

结论：

**后续分流的核心依据应是显式三态，而不是当前大量的隐式推断。**

### 11.4 Skill / Memory 的唯一真实来源改为后端真实数据

目标：

- Flutter 不再依赖手机端本地拼装、镜像或伪同步结果
- Skill / Memory 的唯一真实来源是 Go 后端提供的数据
- Flutter 只负责：
  - 请求原始数据
  - 在前端解析结构化内容
  - 展示、筛选、交互

也就是说：

**手机端不再承担 catalog 真值源角色。**

### 11.5 后端不再负责解析 Skill / Memory 的结构化内容

目标：

- Go 后端不再把真实 skill / memory 原始数据强行解析成当前 `SkillDefinition` / `MemoryItem` 再下发
- 后端更偏向做原始数据的透传、同步、缓存、鉴权、文件读取与分发
- Flutter 负责把这些真实原始数据解析成界面需要的结构

换句话说：

- 后端负责“拿到真实数据并发出来”
- Flutter 负责“解析这些结构化原始数据并展示”

### 11.6 真实同步的目标形态

目标不是：

- `skill_sync_pull` 再塞一个示例 skill
- `memory_sync_pull` 只保留本地 local 项

目标应该是：

1. Go 后端直接对接真实的 skill / memory 数据源
2. 后端返回真实原始数据给 Flutter
3. Flutter 在前端完成结构化解析、列表构建与展示

也就是说，后续 sync_pull 的目标职责是：

- **同步真实数据源**
- **返回原始结果**
- **不再提供 mock / stub catalog 结果**

---

## 12. 当前代码层面的关键事实汇总

### 12.1 关于发送链路

1. Flutter 发送 Claude 新请求时，默认是 `exec(claude)` + `input(prompt)` 双发
2. Flutter 只有在自己判断是 `awaitInput` 或 `inClaudeMode` 时，才只发 `input`
3. Go runtime manager 同时只允许一个 active runner
4. 只要 active runner 还在，再来新的 `exec` 就会报：
   - `another command is already running`
5. Go 后端本身支持 `SendInputOrResume(...)`，即“先直接 input，必要时自动 resume Claude PTY”

### 12.2 关于 Skill / Memory

1. Flutter 的 `skill_catalog_get` / `memory_list` 只是读取后端本地 snapshot
2. Flutter 自身不对结果做明显裁剪，主要展示后端返回值
3. `skill_sync_pull` 当前不是外部真实同步，而是：
   - 保留 local skill
   - 额外塞一个硬编码 `external-diff-summary`
4. `memory_sync_pull` 当前不是外部真实同步，而是：
   - 只保留本地 local memory
   - 不会去真实扫描 Claude memory 系统
5. catalog authoring 闭环是真实存在的，但 sync_pull 仍是 stub/mock 化实现
6. 已确认的目标方向是：
   - 三态 Claude mode
   - 后端只对接真实数据源并透传原始数据
   - Flutter 负责结构化解析 Skill / Memory 原始数据

---

## 13. 相关核心文件索引

### Flutter

- `mobile_vc/lib/features/session/session_controller.dart`
  - 发送消息主路由
  - prompt / permission / review / plan 分流
  - skill / memory 请求与状态更新
- `mobile_vc/lib/data/services/mobilevc_ws_service.dart`
  - WebSocket connect / send / event stream
- `mobile_vc/lib/data/models/session_models.dart`
  - `SkillDefinition` / `MemoryItem` / `CatalogMetadata`
- `mobile_vc/lib/data/models/events.dart`
  - `SkillCatalogResultEvent` / `MemoryListResultEvent` / `CatalogSyncStatusEvent` / `CatalogSyncResultEvent`

### Go

- `internal/ws/handler.go`
  - WebSocket action 分发
  - projection 持久化
  - skill / memory catalog 读写与 sync_pull
- `internal/runtime/manager.go`
  - active runner 管理
  - Claude execute / input / resume / hot-swap
- `internal/runner/pty_runner.go`
  - Claude PTY 运行与 catalog authoring JSON 识别
- `internal/store/file_store.go`
  - skill / memory catalog snapshot 的实际落盘

---

## 14. 一句话总结

当前代码的真实状态是：

- **发送链路**：Flutter 还保留着“新一轮 Claude 请求 = `exec claude` + `input`”的双发模型，而 Go 后端已经具备“`input` 优先、必要时自动 resume”的能力，二者之间存在行为张力。
- **Skill / Memory 同步链路**：Flutter 展示层基本正常，但 Go 后端的 `skill_sync_pull` / `memory_sync_pull` 仍是占位实现，因此同步结果会表现为 skill 很少、memory 不全。

下一阶段已确认的目标方向是：

- **会话模式三态化**：`shell / claude_busy / claude_wait_input`
- **分流显式化**：发送线路按三态决定
- **数据源收敛**：Skill / Memory 只认后端真实数据
- **解析职责前移**：后端透传真实原始数据，Flutter 负责结构化解析与展示
