# 事件表

事件名的权威来源是 `apps/dsh/plugin/index.mjs`。`apps/remote` 需要把它的内部事件映射到这里。

## 服务端 → 客户端

| 事件 | 触发时机 | 关键载荷 |
|---|---|---|
| `snapshot` | 建立连接 / 重连后补齐 | `session`, `messages[]` |
| `session/title` | 会话标题变更 | `sessionId`, `title` |
| `session/end-seed` | 历史回放结束，之后是实时流 | `sessionId` |
| `turn/start` | 一轮开始 | `sessionId`, `turnId` |
| `turn/end` | 一轮结束 | `sessionId`, `turnId`, `reason?` |
| `assistant-stream` | 逐 token 增量 | `text`, `kind`: `text` \| `reasoning` |
| `assistant/message` | 一段完整回复落定 | `text` |
| `tool/call` | 工具开始调用 | `call{ callId, name, args, status }` |
| `tool/result` | 工具返回 | `call`, `output?` |
| `approval/asked` | 需要用户批准 | `request{ callId, kind, title, detail, actions }` |
| `approval/decided` | 审批已处理（本端或另一端） | `callId`, `decision` |
| `compaction/summary` | 上下文压缩 | `summary` |
| `request/header` | 请求头信息（模型/额度等） | `header` |

## 客户端 → 服务端

| 指令 | 说明 | 字段 |
|---|---|---|
| `prompt` | 发送消息 | `sessionId`, `text`, `mode`, `attachments[]` |
| `cancel` | 打断当前 turn | `sessionId` |
| `approval/decide` | 提交审批 | `sessionId`, `callId`, `decision` |

## 兼容规则

1. 收到未知 `type`：**忽略并记录**，不要断开连接。
2. 审批的 `callId` 是幂等键：同一 `callId` 重复提交，服务端返回已有结果。
3. `assistant-stream` 可能乱序到达（重连补齐），客户端按 `turnId` 归并、按文本偏移拼接。
4. `snapshot` 一定是完整状态，收到后可丢弃此前同 `sessionId` 的所有增量。
