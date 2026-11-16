# 架构规则（apps/remote）

这份文件回答一个问题：**什么不许做。** 描述"现在是什么样"的文档会过期，规则不会。

改动分层时，同时更新这里和 `apps/remote/internal/archtest/arch_test.go` —— 后者是这条规则的执行者。

## 依赖方向

```text
cmd/*           组合根，可依赖一切
  ├── gateway        组合根：HTTP / WebSocket 入口，可依赖一切内部包
  └── relayclient    客户端装配，可依赖一切内部包

session          会话与控制器状态        → adb, data, engine, logx, protocol
engine           进程/PTY 与 CLI 适配     → logx, protocol
data             会话落盘、同步、技能      → protocol
relay            中继服务                 → logx
adb / push       设备与推送               → （叶子）
protocol         移动端契约               → （叶子）
logx             日志                     → （叶子）
```

箭头方向 = 允许的依赖。**反向依赖一律禁止。**

## 三条硬规则

1. **`protocol` 是叶子**：它是两条产品线共用的契约，不许 import 任何 `internal/` 包。
   一旦它开始依赖 `session` 或 `data`，契约就被实现污染了。

2. **`engine` 不认识传输层**：不许 import `gateway` / `relay` / `data` / `session`。
   引擎只负责"跑进程、解析输出"，把它接到哪个入口是 gateway 的事。

3. **只有组合根可以全知**：`gateway`、`relayclient`、`cmd/*` 允许依赖一切；
   其他包按上表收紧。

## 命名约束

- **接口名不许带厂商**：`ClaudeSessionProvider` 这种名字说明抽象是围着某一个实现长出来的。
  改成能力型命名（`SessionHistoryProvider`），否则加第二个 CLI 时一定会漏。
- 新增 CLI 适配 = 在 `engine` 里加实现 + 在 `protocol` 里加能力，**不是**在 `gateway` 里加 `if provider == "xxx"`。

## 明确不做

- 不在插件/服务内再起一套 relay 或 E2EE
- 不在单进程内做多租户隔离
- 不重写 agent loop（那是 CLI 的事）
