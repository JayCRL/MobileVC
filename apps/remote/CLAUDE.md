# apps/remote 项目规则

这条线是 **Claude Code / Codex** 的移动端控制台：Go 后端 + Flutter 客户端 + Relay。

> 仓库根目录还有一条并行产品线（DSH 插件版），见仓库根的 `CLAUDE.md`。

## 编译与运行

```bash
# 后端（需 Go 1.25+，go.mod 已声明）
go build -o server ./cmd/server

# 单元测试
go test ./... -count=1

# 只跑某个包
go test ./internal/session/... -count=1
```

启动后端需要的环境变量（按部署环境填入实际值）：

```bash
AUTH_TOKEN="<token>" \
APNS_AUTH_KEY_PATH="<AuthKey>.p8" \
APNS_KEY_ID="<key-id>" \
APNS_TEAM_ID="<team-id>" \
APNS_TOPIC="com.justprove.mobilevc" \
APNS_PRODUCTION="false" \
./server
```

启动成功的标志：日志出现 `APNs service ready` 和 `Ready: addr=:8001`。

### Flutter

```bash
cd mobile_vc
flutter analyze
flutter build ios        # 或 flutter build apk
```

iOS 打包上传走团队的 `mobilevc-ota` 流程，具体步骤见部署文档。

## 改动范围原则

- 只改后端 → 只重启后端
- 只改 Flutter → 只重新打包
- 两端都改 → 分别执行各自的流程

## 核心模块（以代码为准）

| 目录 | 职责 |
|---|---|
| `internal/session` | 会话管理与控制器状态 |
| `internal/engine` | 进程 / PTY 与 CLI 适配 |
| `internal/gateway` | HTTP / WebSocket 入口（组合根） |
| `internal/data` | 会话落盘、同步、技能与记忆 |
| `internal/relay` | 中继服务 |
| `internal/protocol` | 移动端契约（叶子包） |

分层与依赖方向见 `docs/architecture/rules.md`，由 `apps/remote/internal/archtest` 里的测试强制执行。

## 前后端对接

涉及 Flutter 与后端协议改动时：

1. 必须同时读两端代码
2. 确认事件名与数据结构一致
3. 契约改动先落到 `packages/contract`，再改两端

## 代码理解原则

- 不知道具体逻辑时先读代码，仍不确定就问用户，不要猜
- 改动方案有多种选择时先问

## Git

- 可以自动 commit 到本地
- push 前需要用户确认
- 提交时排除 `.claude/worktrees/` 等环境噪音
