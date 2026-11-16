# 架构决策记录（ADR）

一页一个决策：**背景 → 选项 → 结论 → 后果**。

ADR 记录的是"为什么这样选"，不是"现在长什么样"（后者看 `docs/architecture/rules.md` 和
`docs/project-index.md`，它们允许随代码更新；ADR 定了就不再改，要推翻就新写一篇并标记取代）。

| 编号 | 决策 |
|---|---|
| [0001](0001-two-parallel-product-lines.md) | 两条并行产品线 + 共享契约 |
| [0002](0002-dsh-reuses-host-session-controller.md) | DSH 版复用宿主 sessionController，不启第二个 agent |
| [0003](0003-fix-platform-differences-per-platform.md) | 平台差异只在该平台修正 |

## 什么时候写

- 改动跨模块、难以回退，或会约束后续实现
- 同一件事讨论过两次以上仍要重新解释
- 修完一个 bug 后发现"当初为什么会这么做"本身值得留档

## 格式

```
# NNNN. 标题（结论式，不是疑问句）

状态：已采纳 / 已被 NNNN 取代
日期：YYYY-MM-DD

## 背景
## 选项
## 结论
## 后果
```

写短一点。一篇 ADR 超过一屏，通常说明还不够确定。
