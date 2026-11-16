# 0002. DSH 版复用宿主 sessionController，不启第二个 agent

状态：已采纳
日期：2026-11-17

## 背景

给 agent 做手机界面有两种做法：在外部另起一个 agent 进程并同步状态，或者直接接进宿主。
MobileVC 的 Claude/Codex 线走的是前者：自己 spawn CLI、自己解析输出、自己维护 session store，
再用 relay 把状态送到手机。

这条路带来的问题在 issue 列表里很集中：配对失败、relay 起不来、重连后状态不一致、
"卡在思考中"。根因不是实现质量，而是**同一份会话状态存在两个副本**，靠同步协议保持一致。

## 选项

1. **外部再起一套**：手机端独立跑 agent，与桌面会话解耦。好处是不依赖宿主插件 API；
   代价是重新引入双状态机，并且与桌面会话割裂（不是"另一个入口"而是"另一个会话"）。
2. **进程内插件，复用 `ctx.sessionController`**：所有读写走宿主，不复制状态机，不引入第二套 relay。

## 结论

选 2。DSH 版的关键约束：

- 不启第二个 agent、不复制 session 状态机、不引入第二个 relay
- 读写统一走 `sessionController`；附件走 `fileUploads`；插件管理走 `pluginManager`
- 手机是这个会话的**另一个入口**，而不是一个新的会话

## 后果

- 好处：一整类"同步不一致"bug 从架构上消失；无需部署额外服务；数据与鉴权沿用宿主的。
- 代价：深度依赖 DSH 的插件 API（年轻、会变）。API 变更时由 DSH 版单独适配，
  `packages/contract` 的事件名尽量保持稳定，避免影响 Claude/Codex 线。
- 安全边界：`/mobilevc/` 绕过宿主的 browser-trust fence，因此**公网必须加认证与 HTTPS**，
  这一条写进了 README 的安全段落。
