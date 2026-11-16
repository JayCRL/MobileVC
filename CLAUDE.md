# MobileVC 仓库规则

一个仓库，两条**并行**产品线，共享同一个承诺：把本机跑的 AI 编程会话变成手机上的另一个入口。

| 线 | 目录 | 形态 | 安装 |
|---|---|---|---|
| **DSH** | `apps/dsh` | DSH host plugin + PWA | `dsh plugin add` |
| **Remote** | `apps/remote` | Go 后端 + Flutter + Relay | `npm i -g @justprove/mobilevc` |

## 目录地图

```text
apps/dsh/plugin        host plugin 入口（注册 /mobilevc/api/*）
apps/dsh/preview       PWA 静态资源
apps/dsh/site          官网页面
apps/dsh/edge          Caddy + cloudflared 自部署
apps/remote/cmd        后端入口（server / relay）
apps/remote/internal   后端实现（按层拆包）
apps/remote/mobile_vc  Flutter 客户端
packages/contract      两条线共用的事件 / 审批 / diff 契约
scripts              仓库级检查脚本
docs                  截图、演示、架构规则
```

## 常用命令

```bash
# DSH 线（仓库根就是它的 npm 包）
npm install && npm test

# Remote 线
cd apps/remote && go test ./... -count=1

# 文档路径检查（CI 也在跑）
node scripts/check-doc-paths.mjs
```

## 硬规则

1. **契约先行**：事件名、审批、diff 结构只在 `packages/contract` 定义。改契约要同时改两端实现，不允许单边先跑。
2. **依赖方向**：`apps/remote` 的分层由 `apps/remote/internal/archtest` 用测试锁死，越界即测试失败。规则文本见 `docs/architecture/rules.md`。
3. **文档不能指向不存在的路径**：`scripts/check-doc-paths.mjs` 会校验文档里的行内路径，CI 红即视为漂移。
4. **测试必须全绿**：`go test ./...` 是 CI 门禁，不允许长期红着。过去 800+ 个用例因为没有 CI 烂掉过一轮，已修复。

## 提交

- 允许自动 commit 到本地分支
- push 前需要用户确认
- 提交信息用 `type(scope): 说明`（与 NoteBoard 保持一致），例如 `fix(relay): 重连后恢复配对状态`
- 排除 `.claude/worktrees/`、`node_modules`、构建产物

## 遇到不确定

- 逻辑不清楚 → 先读代码，仍不清楚就问
- 有多种方案 → 先问
- 涉及 `packages/contract` 或协议 → 必须先读两端
