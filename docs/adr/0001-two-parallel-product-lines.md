# 0001. 两条并行产品线 + 共享契约

状态：已采纳
日期：2026-11-17

## 背景

MobileVC 最初是「Claude Code / Codex 的手机遥控器」：Go 后端 + Flutter 客户端 + 自建 relay。
后来 DSH（DeepSeek Harness）出现，宿主本身已经提供 agent runtime、durable session log、
follow 流、审批和插件系统，于是又有了「DSH 宿主的手机界面」这一形态。

两条线的接入方式完全不同：

- Claude/Codex 线：自己维护 session store、自己实现 relay、自己复制一份状态机
- DSH 线：进程内插件，读写都走宿主的 `sessionController`

把它们硬塞进一套实现里，会让两边的状态机互相污染——这正是老版本几十个
「重连后状态不一致」类 issue 的来源。

## 选项

1. **只保留 DSH 线**：最干净，但一次性丢掉 Claude/Codex 用户，且 Codex/Claude 的用户量
   目前仍然远大于 DSH。
2. **合并成一套实现**：共用 session store 与传输层。代价是继续维护第二套状态机，
   等于把已经证明会出问题的架构再维护一遍。
3. **两条并行线，共享契约不共享代码**：各自独立实现，只在事件名、审批形状、diff 结构上对齐。

## 结论

选 3。目录上 `apps/remote` 与 `apps/dsh` 并列，`packages/contract` 只放契约（类型与事件表），
不含任何传输、存储或 UI 实现。

两条线**不存在主次**：任何一条都不被称为 legacy，也不写"旧版"字样。

## 后果

- 支持面变成两倍：两套构建、两套发布、两套排错路径。
- 因此约定：**每季度只有一条线加新功能**，另一条只修 bug，季度末轮换；当季主打的那条在
  README 顶部置顶。
- 交互必须一致（审批弹层、diff 审核、会话续接），因为这才是产品，传输方式只是实现。
- 新增第三条线（例如 Copilot CLI）时，先在 `packages/contract` 扩展契约，再实现，不允许单边先跑。
