# MobileVC Contract

两条产品线共用的**移动端交互契约**。

## 为什么要有它

MobileVC 有两个入口，接入方式完全不同：

| | `apps/dsh` | `apps/remote` |
|---|---|---|
| 后端 | DSH host plugin（进程内） | 自建 Go server + relay |
| 传输 | HTTPS + SSE | WebSocket + E2EE relay |
| 会话归属 | `ctx.sessionController` | 自己维护的 session store |

但它们面对的是**同一套手机交互**：看会话、发消息、打断、审批、看 diff、续接历史。
如果两边各写一套术语和载荷，用户在两个产品之间迁移时体验会断层，第三个适配器
（Hermes / Copilot CLI）也要从零再设计一遍。

所以这里只固化**最小契约**：事件名、枚举值、审批与 diff 的形状。传输层、存储、
鉴权都不属于契约。

## 内容

| 文件 | 用途 |
|---|---|
| `contract.ts` | TypeScript 类型，供 `apps/dsh` 及未来的 Web 端直接引用 |
| `contract.go` | Go 结构体，供 `apps/remote` 映射 |
| `events.md` | 事件表：名称、方向、载荷要点 |

## 约束

1. **只增不改**：已有字段不改语义，新需求加字段或加事件类型。
2. **未知类型必须可忽略**：接收方遇到不认识的事件要跳过，不能崩。
3. **载荷用 camelCase**：Go 端靠 `json` tag 映射，不要依赖字段名默认值。
4. **不放进契约的东西**：鉴权、传输格式、UI 布局、模型参数。

## 当前状态

初版，从 `apps/dsh/plugin/index.mjs` 的实际事件名反推而来。
`apps/remote` 尚未接入（第一阶段先让两边事件名对齐，再做字段对齐）。
