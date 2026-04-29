# MobileVC ↔ Claude CLI 双向会话同步计划

## 目标

让 Claude Code CLI 能直接看到 MobileVC 创建的会话，并且 CLI 继续编辑后 MobileVC 也能看到更新。

## 当前状态

- **Claude CLI → MobileVC**: 已支持，通过 `claudesync.ListNativeSessions` 扫描 `~/.claude/projects/` 下的 JSONL 文件镜像到 MobileVC session list
- **MobileVC → Claude CLI**: 不支持，MobileVC 会话只存在自己的 store 里

## Claude CLI JSONL 格式要求

`parseSessionFromFile`（claudesync/threads.go:382）只消费三种类型：

| type | 用途 | 必需字段 |
|------|------|---------|
| `user` | 用户输入 | `message: {role: "user", content: "text..."}` |
| `assistant` | AI 回复文本 | `message: {content: [{type: "text", text: "..."}]}` |
| 其他类型 | 跳过 | — |

每行公共字段: `type`, `sessionId`, `cwd`, `timestamp`, `uuid`, `parentUuid`, `entrypoint`, `version`, `gitBranch` 等

文件命名: `<session-uuid>.jsonl`，放在 `~/.claude/projects/<encoded-cwd>/` 下

## 方案：MobileVC 写 JSONL + 增量同步回 store

### 第 1 步: 新增 `WriteSessionToJSONL` 函数 (claudesync/)

```
claudesync.WriteSessionToJSONL(cwd, sessionID, claudeSessionUUID string, events []JSONLEvent) error
```

- 目标路径: `~/.claude/projects/<encoded-cwd>/<claudeSessionUUID>.jsonl`
- 追加写入（非覆盖），文件不存在时创建
- 写入事件类型:
  - `user` — 对应用户输入
  - `assistant` — 对应 AI markdown 回复（只写 text block）
  - `ai-title` — 会话标题更新时
  - `last-prompt` — 标记最后一条用户提示
- 需要文件锁避免并发冲突

### 第 2 步: SessionRecord 增加 `ClaudeSessionUUID` 字段

在 `store.SessionRecord` / `store.SessionSummary` 中增加可选字段 `ClaudeSessionUUID string`，用于关联 MobileVC 会话与 JSONL 文件。

### 第 3 步: 调用点

以下时机写入 JSONL:

1. **用户发送消息后** (`handler.go` session_send 处理完成)
   → 写入 `user` 事件行

2. **AI 回复完成时** (runner 输出拦截/总结)
   → 写入 `assistant` 事件行（extractAssistantText 的内容）

3. **会话标题更新时**
   → 写入 `ai-title` 事件行

4. **会话 idle/结束**
   → 写入 `last-prompt` 事件行

### 第 4 步: 反向同步 (Claude CLI 继续编辑后读回)

在 `session_load` 时:
- 如果 session 有 `ClaudeSessionUUID`，检查对应 JSONL 文件的 mtime 是否比 store session 的 updatedAt 更新
- 如果更新，用 `parseSessionFromFile` 读回 JSONL，将新的事件合并到 store session 的 LogEntries 中
- 这样 MobileVC 能看到 CLI 端后续添加的对话内容

### 第 5 步: session_list 去重

在 `mergeSessionSummaries` 中:
- MobileVC store session 的 ID 是 `session-xxx`
- Mirror session 的 ID 是 `claude-session:<uuid>`
- 如果 store session 的 `ClaudeSessionUUID` 匹配某个 mirror session 的 UUID，说明是同一个逻辑会话
- 此时: 优先用 mirror session 的 LogEntries（更新更全），但保留 store session 的 controller/runtime 元数据

### 调用链路

```
Flutter 发送消息
  → handler.session_send
    → 写入 store
    → claudesync.WriteSessionToJSONL(cwd, sid, uuid, userEvent)  ← 新增
    → runner 执行
  → assistant 回复
    → 写入 store
    → claudesync.WriteSessionToJSONL(cwd, sid, uuid, assistantEvent)  ← 新增

Claude CLI 继续对话
  → CLI 追加事件到同一 JSONL 文件

Flutter 请求 session_load
  → 读 store session
  → 检测 claudeSessionUUID + JSONL mtime
  → 如果 JSONL 更新: parseSessionFromFile + 合并新 LogEntries  ← 新增
  → 返回合并后的 session
```

## 局限

- MobileVC 写入的 assistant 事件只能包含纯文本（markdown），不包含 tool_use / thinking 细节。如果 Claude CLI 继续这个会话，它需要自己重建上下文
- last-prompt 事件可以帮助 Claude CLI 的会话恢复机制
- UUID 需要是合法的 UUID v4 格式，以便 Claude CLI 正确识别
