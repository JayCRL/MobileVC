# 测试日志 / Testing Log

**最近更新**: 2026-05-03
**总用例数**: 538 个 Go 单元/组件测试 + 3 个 Python 端到端回归测试

本文档记录后端四层架构（`data` / `engine` / `session` / `gateway`）重构后补全的测试现状、各层次测试模拟的真实用户场景，以及尚未补上的测试盲区。

---

## 1. 当前进度

### 1.1 覆盖率

| 包 | 起始 | 最终 | 备注 |
|---|---|---|---|
| `internal/session` ⚠️ 核心稳定区 | 28% | **70.8%** | CLAUDE.md 点名的核心模块 |
| `internal/data/claudesync` | 0% | **84.6%** | JSONL 持久化层 |
| `internal/data/skills` | 0% | **88.1%** | Skill 注册 / 启动器 |
| `internal/data` (file_store) | 67% | 67% | 已被原有测试覆盖 |
| `internal/gateway` | 52% | 53% | handler/permission/slash 已覆盖 |
| `internal/engine` | 50% | 50% | parser/pty/exec 已覆盖 |

### 1.2 新增测试文件

| 文件 | 测试目标 | 用例数（约） |
|---|---|---|
| `internal/session/permission_router_test.go` | 决策路由 + 意图识别 + Plan 构造 | 20+ |
| `internal/session/interaction_helpers_test.go` | 输入守卫 / 权限刷新 / Review 文案 | 22 |
| `internal/session/manager_extended_test.go` | Service 公开方法（SendPermissionDecision / Review / Plan / StopActive 等） | 26 |
| `internal/session/projector_test.go` | 投影器 / lifecycle / Resume 恢复事件 | 25 |
| `internal/session/projection_apply_test.go` | ApplyEventToProjection / AIStatus / log helpers | 33 |
| `internal/session/projection_events_test.go` | 协议转换 / Restore 状态推导 | 18 |
| `internal/data/claudesync/threads_test.go` | JSONL 写读 / 镜像 / Merge | 25 |
| `internal/data/skills/skills_test.go` | Registry / Launcher / SessionContext | 33 |
| `internal/gateway/push_test.go` | 推送行为 + payload + debounce | 18 |
| `tests/regression/test_push_token_lifecycle.py` | push token 注册/轮换/平台切换 | 1 流程（5 步） |

### 1.3 已修复

- `gateway` 两个 handler 测试在 `-count=2` 下并行竞争（temp dir 与 SaveProjection race）
  - 修复：测试末尾显式 `conn.Close()` + `runtimeSessions.CleanupAll()`
- `tests/unit/` 两个 Claude e2e 集成测试不稳定（依赖真实 Claude CLI 行为变化）
  - 修复：默认 `t.Skip`，需要时设 `MOBILEVC_RUN_CLAUDE_INTEGRATION=1`
- `tests/regression/run_regression.py` 的 `PROJECT_ROOT` 路径 bug

---

## 2. 测试分层与模拟的用户行为

### 2.1 单元测试（约 70% 用例）

**做什么**：测纯函数 / helper 的输入输出，不起任何外部依赖。

**模拟的"用户行为"**：内部数据流，不直接对应用户操作。例如：
- 用户写一个文件路径 `/Users/x/MobileVC` → `EncodeCWDToProjectDir` 返回 `-Users-x-MobileVC`（这是 Claude CLI 的存储约定）
- Claude 输出 `tool=Read` → `aiStatusVerbForTool` 返回 `"正在读取"`（用户在客户端看到的文案）

### 2.2 组件级测试（约 25% 用例）

**做什么**：用 stub/mock 替代真实 Claude 进程，但走完整代码路径，验证一个 Service / Handler 模块的行为。

**关键的 stub 类型**：
- `permissionStubRunner` —— 模拟 claude PTY 进程，可注入 "有 pending 权限请求"、"writeErr=ErrNoPendingControlRequest" 等场景
- `chanPushService` —— 模拟 APNs 服务，每次 SendNotification 投递到 channel，测试可阻塞等待并校验 payload
- `stubPendingLookup` / `stubResponder` —— 模拟 prompt 缓存与权限响应器

**模拟的用户行为示例**：

| 测试 | 模拟的用户场景 |
|---|---|
| `TestService_SendPermissionDecision_HappyPathWritesDecision` | 用户在 App 上点击"批准权限"按钮 → SendPermissionDecision → 验证真的写到 runner，且 activeMeta 被更新 |
| `TestService_SendPermissionDecision_RequestIDMismatch` | 用户在旧权限提示上点了批准（但服务端已经换了新的 requestID） → 应该返回 `ErrNoPendingControlRequest` |
| `TestService_SendPermissionDecision_ResumeNotFoundErr` | resume 的 sessionID 已失效 → Claude CLI 报错 → 后端转换为 `ErrResumeConversationNotFound` |
| `TestService_StopActive_WithRunner` | 用户在 App 上点"停止"按钮 → 验证 emit 了 `session_state=stopped` 事件 |
| `TestApplyEventToProjection_FileDiff` | Claude 工具产出一份 diff → 验证 projection.Diffs / ReviewGroups / CurrentDiff 都正确填充 |
| `TestApplyEventToProjection_PromptRequestSetsWaitInput` | Claude 弹出权限提示 → 验证 controller.State 切到 WAIT_INPUT，lifecycle 切到 waiting_input |
| `TestRestoredAgentStateEventFromRecord_ThinkingWithResumeBecomesWaitInput` | App 重启后从存储恢复一个状态为 THINKING 的 session（但实际 runner 已不存在）→ 应当降级成 WAIT_INPUT 而不是显示"思考中" |
| `TestPush_PromptRequestTriggersActionNeeded` | 后端收到一个 PromptRequestEvent → 给该 session 注册过的 token 发推送，data.type = `action_needed` |
| `TestPush_BlockingKindOverridesBody` | 后端按 blockingKind=permission/review/plan/reply 给推送 body 用对应中文文案 |
| `TestPush_ProgressDebouncedWithinWindow` | Claude 一直在跑工具，30s 内连续多次 RUNNING 状态 → 推送只发一次（去抖） |
| `TestRegistry_LocalOverridesBuiltin` | 用户在 Skill 管理界面修改了内置 skill `review` 的 prompt → ListSkills 返回的就是覆盖后的版本 |
| `TestLauncher_BuildInvocationDisabled` | 用户没在 SessionContext 启用某 skill 就尝试调用 → 返回"未在本会话启用"错误 |

### 2.3 端到端回归测试（Python，3 个）

**做什么**：真起 backend 进程（`./server`），用 WebSocket 发真实协议，断言 server.log + 收到的事件。

**模拟的真实用户场景**：

#### `test_permission_input_guard.py`
模拟用户：
1. 打开 App，连接后端
2. 创建会话，启动 Claude
3. 输入 `请用 bash rm 命令删除 /tmp/test_mobilevc_nonexistent.txt`（触发权限请求）
4. **关键**：在权限弹窗还没批准时，又输入了一句 `你好，请忽略上一条指令`
5. 验证后端拒绝了第二条输入（log 出现 "input blocked by pending permission request"）
6. 用户批准权限
7. 用户输入 `好的，现在正常回答：1+1等于几？`
8. 验证后端接受了输入，Claude 正常应答

→ 覆盖 Bug：权限挂起期间的文本输入必须被 backend block，不能透传给 Claude。

#### `test_session_resume_permission.py`
模拟用户：
1. 创建会话，启动 Claude，触发权限请求
2. **断开 WebSocket 连接**（模拟 App 切到后台）
3. 重新连接，发送 `session_resume` + `session_delta_get`
4. 验证：权限提示仍然处于 active 状态
5. 验证：在还没批准权限时再发文本输入，仍然被 block

→ 覆盖 Bug：FileDiffEvent 不能清掉权限提示；session_resume 后输入守卫还要工作。

#### `test_push_token_lifecycle.py`（本次新增）
模拟用户：
1. App 启动，注册 iOS push token `tok-alpha-001`
2. iOS 系统轮换 token，App 重新注册 `tok-beta-002`
3. 用户在另一个设备登录（Android），注册 `tok-gamma-003` + platform=android
4. App bug 导致发了空 token → 后端必须返回 `token is required` 错误
5. App 没传 platform 字段 → 后端默认填 ios，不报错

→ 覆盖：push token 注册协议的常见使用模式。

---

## 3. 跳过 / 弱化的测试

### 3.1 默认 skip 的 e2e 测试

需要 `MOBILEVC_RUN_CLAUDE_INTEGRATION=1` 才会跑：

- `TestClaudeSessionBackgroundTask` — 多步任务 + 权限审批链路，需要 ≥100s
- `TestClaudeSessionDisconnectReconnect` — 断线重连后是否记得上下文，依赖 Claude CLI resume 行为

**原因**：依赖真实 Claude binary 的速度和行为，且 Claude 自身行为会随版本变化波动。本地手动验证用，CI 不跑。

### 3.2 用户明确跳过的范围

- `internal/data/codexsync/` —— Codex 历史同步层（暂不优先）
- `internal/adb/` 的 webrtc 信令分发（暂不优先）
- 真实 APNs HTTP 端到端 —— 用 mock service 验证调用行为 + payload 字段；接入真实 APNs 后再补

---

## 4. 还可以补的盲区（未优先）

| 模块 | 当前状态 | 风险 |
|---|---|---|
| `internal/data/codexsync` | 0% | Codex 历史会话扫描有 bug 时无回归保护 |
| `internal/gateway/adb.go` | 部分 | adb webrtc 信令路由 15 个未覆盖函数 |
| `internal/push` 包内部（NoopService / APNsService） | 仅构造校验 | 真实 APNs 推送链路（payload 编码、token 失效重试）需要 mock HTTP server |
| 端到端：网络抖动 / 弱网 / push 失败重试 | 未覆盖 | 移动端常见但难以构造稳定测试 |

---

## 5. 如何运行

```bash
# 全部 Go 测试（约 1 分钟）
go test ./...

# 仅核心稳定区
go test ./internal/session/...

# 看覆盖率
go test -coverprofile=cov.out ./internal/session/
go tool cover -html=cov.out

# 端到端回归测试（会重建并启动后端）
python3 tests/regression/run_regression.py

# 单跑一个
python3 tests/regression/run_regression.py --test test_push_token_lifecycle

# 跑被 skip 的 Claude e2e 测试（需要本机有 claude CLI 且有 anthropic 鉴权）
MOBILEVC_RUN_CLAUDE_INTEGRATION=1 go test ./tests/unit/... -run TestClaudeSession -v
```
