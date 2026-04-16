# 项目核心逻辑文档

## 权限授予与会话恢复机制

### 问题背景

当 Claude 进程自然结束时，如果用户在此时批准权限请求，会话无法恢复，Flutter 端显示"Claude 空闲中"。

### 根本原因

权限请求发送给前端时，`RuntimeMeta` 中缺少 `Command` 字段，导致权限批准后无法识别这是 Claude 会话，从而无法触发会话恢复逻辑。

### 完整流程

#### 1. Claude 输出权限请求（后端解析阶段）

**文件：** `internal/runner/pty_runner.go:1753-1809`

当 Claude 输出 JSON 格式的 `control_request` 时：

```go
case "control_request":
    requestID := strings.TrimSpace(envelope.RequestID)
    if requestID != "" {
        r.mu.Lock()
        r.pendingControlRequestID = requestID  // 缓存 request ID
        r.mu.Unlock()
    }
    
    // 构造 promptMeta，包含完整的运行时元信息
    r.mu.Lock()
    command := r.pendingReq.Command
    cwd := r.currentDir
    engine := r.pendingReq.Engine
    permMode := r.permissionMode
    r.mu.Unlock()
    
    promptMeta := protocol.RuntimeMeta{
        ResumeSessionID: envelope.SessionID,
        Command:         command,           // ← 关键：必须包含 command
        CWD:             cwd,
        Engine:          engine,
        PermissionMode:  permMode,
    }
    
    // 发送权限请求事件给前端
    sendEvent(sink, protocol.ApplyRuntimeMeta(
        protocol.NewPromptRequestEvent(sessionID, promptMessage, promptChoices),
        promptMeta,
    ))
```

**关键点：**
- `promptMeta` 必须包含 `Command` 字段
- `Command` 来自 `r.pendingReq.Command`，即当前运行的 Claude 命令
- 这个 `Command` 会通过 WebSocket 发送给前端

#### 2. 前端接收并缓存权限请求

**文件：** `mobile_vc/lib/features/session/session_controller.dart:2857, 2989`

前端收到权限请求后，缓存 `runtimeMeta`：

```dart
// 缓存权限请求的元信息
_pendingPermissionMeta = RuntimeMeta.fromJson(event['runtimeMeta']);

// 其中包含：
// - command: "claude ..."
// - resumeSessionId: "f54a6ce7-..."
// - cwd: "/path/to/project"
// - engine: "claude"
// - permissionMode: "prompt"
```

#### 3. 用户批准权限，前端发送决策

**文件：** `mobile_vc/lib/features/session/session_controller.dart:2857, 2989`

用户点击批准后，前端发送权限决策：

```dart
{
  'type': 'permission_decision',
  'sessionId': sessionId,
  'decision': 'approve',
  'command': decisionMeta.command,        // ← 从缓存的 meta 中获取
  'resumeSessionId': decisionMeta.resumeSessionId,
  'cwd': decisionMeta.cwd,
  'engine': decisionMeta.engine,
  'permissionMode': decisionMeta.permissionMode,
}
```

#### 4. 后端接收权限决策并执行

**文件：** `internal/ws/permission_decision.go:43-106`

后端收到权限决策后，提取 `command` 字段：

```go
// 从多个来源提取 command（优先级从高到低）
inputMeta := protocol.RuntimeMeta{
    Command: firstNonEmptyString(
        permissionEvent.FallbackCommand,    // ← 前端发送的 command
        projection.Runtime.Command,          // 当前投影的 command
        controller.CurrentCommand,           // 控制器的 command
        controller.ActiveMeta.Command,       // 活动元信息的 command
    ),
    // ... 其他字段
}

// 检查是否是 Claude 命令
if !isClaudeCommandLike(inputMeta.Command) {
    // 如果不是 Claude 命令，尝试发送给当前运行的进程
    if err := service.SendPermissionDecision(...); err == nil {
        return nil
    } else if errors.Is(err, runtimepkg.ErrNoActiveRunner) {
        // 如果没有活动进程，继续往下走，尝试从 resume 恢复
    } else {
        return err
    }
}

// 尝试在当前运行的会话中热重启
currentRunner := service.CurrentRunner()
if currentRunner != nil {
    if err := service.HotSwapApproveWithTemporaryElevation(...); err == nil || !errors.Is(err, runtimepkg.ErrNoActiveRunner) {
        return err
    }
}

// 检查是否可以从 resume 恢复
if !service.CanHotSwapClaudeSession(req) || !service.HasResumeSession(req) {
    return runtimepkg.ErrNoActiveRunner
}

// 从 resume 恢复会话
return service.HotSwapApproveFromResume(ctx, sessionID, req, replayInput, emitAndPersist)
```

**关键逻辑：**
1. 如果 `inputMeta.Command` 为空或不是 Claude 命令，会先尝试 `SendPermissionDecision`
2. 如果发送失败且错误是 `ErrNoActiveRunner`，会继续尝试从 resume 恢复
3. 如果 `currentRunner` 不为 nil，优先在当前会话中热重启
4. 如果 `currentRunner` 为 nil，检查是否可以从 resume 恢复
5. 最后调用 `HotSwapApproveFromResume` 恢复会话

#### 5. 会话恢复检查

**文件：** `internal/runtime/manager.go:650-665`

```go
// 检查是否可以热重启 Claude 会话
func (s *Service) CanHotSwapClaudeSession(req ExecuteRequest) bool {
    currentRunner, activeMeta, currentSessionID := s.manager.current()
    if req.Mode != runner.ModePTY {
        return false
    }
    if currentRunner != nil && currentSessionID != "" {
        return runnerIsClaudeSession(currentRunner, req.Command, activeMeta.Command)
    }
    // 即使 currentRunner 为 nil，也检查历史元信息
    return runnerIsClaudeSession(nil, req.Command, activeMeta.Command, s.manager.snapshot().ActiveMeta.Command)
}

// 检查是否有可恢复的会话
func (s *Service) HasResumeSession(req ExecuteRequest) bool {
    currentRunner, activeMeta, _ := s.manager.current()
    resumeSessionID := resolveResumeSessionID(currentRunner, req.RuntimeMeta, activeMeta, s.manager.snapshot().ActiveMeta, protocol.RuntimeMeta{ResumeSessionID: s.manager.snapshot().ResumeSessionID})
    return strings.TrimSpace(resumeSessionID) != ""
}
```

**关键点：**
- `CanHotSwapClaudeSession` 需要 `req.Command` 不为空，才能识别这是 Claude 会话
- `HasResumeSession` 需要 `req.RuntimeMeta.ResumeSessionID` 不为空
- 两个条件都满足时，才能成功恢复会话

### 修复方案

**修改文件：** `internal/runner/pty_runner.go:1784-1797`

在构造 `promptMeta` 时，添加完整的运行时元信息：

```go
// 修改前（缺少 Command 等字段）
promptMeta := protocol.RuntimeMeta{ResumeSessionID: envelope.SessionID}

// 修改后（包含完整信息）
r.mu.Lock()
command := r.pendingReq.Command
cwd := r.currentDir
engine := r.pendingReq.Engine
permMode := r.permissionMode
r.mu.Unlock()

promptMeta := protocol.RuntimeMeta{
    ResumeSessionID: envelope.SessionID,
    Command:         command,
    CWD:             cwd,
    Engine:          engine,
    PermissionMode:  permMode,
}
```

### 效果

修复后，即使 Claude 进程在权限批准时已经结束：
1. 前端发送的权限决策包含完整的 `command` 字段
2. 后端能正确识别这是 Claude 命令（`isClaudeCommandLike` 返回 true）
3. `CanHotSwapClaudeSession` 和 `HasResumeSession` 检查通过
4. 成功调用 `HotSwapApproveFromResume` 恢复会话
5. Claude 会话继续执行，不会显示"空闲中"

### 相关文件

- `internal/runner/pty_runner.go` - PTY Runner 实现，解析 Claude 输出
- `internal/ws/permission_decision.go` - 权限决策处理
- `internal/runtime/manager.go` - 会话管理和恢复
- `internal/protocol/event.go` - 事件和元信息定义
- `mobile_vc/lib/features/session/session_controller.dart` - 前端会话控制器
- `mobile_vc/lib/data/models/runtime_meta.dart` - 前端运行时元信息模型
