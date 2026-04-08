---
name: ai-coding-workflow
description: Use when the user wants to work with Codex, Claude Code, or other AI coding agents more efficiently, especially to avoid long-session slowdown, split large tasks into bounded steps, define scope and non-goals, separate diagnosis from implementation, compress context between sessions, or turn a vague coding request into a tighter execution prompt. Also use when the user asks for an AI coding prompt/template, asks why AI coding gets slower later in a task, or wants a repeatable collaboration workflow for medium-to-large codebases.
---

# AI Coding Workflow

Use this skill when the task is about improving how an AI coding agent should collaborate, not just implementing a product change.

Typical triggers:

- "why does AI coding get slower later"
- "make this easier for Codex / Claude Code"
- "help me write a better prompt"
- "split this into smaller steps"
- "不要顺手改别的"
- "先定位再改"
- "给我一个交接摘要"

Do not use this skill as a substitute for product-specific skills. Pair it with the relevant implementation skill when the user also wants code changes.

## Default workflow

For medium or large coding tasks, drive the work in this order:

1. Restate the target as one concrete outcome.
2. Lock the scope: directories, modules, files, or interfaces that may change.
3. Lock the non-goals: what must not be refactored, redesigned, or "cleaned up".
4. Define completion criteria before editing.
5. If uncertainty is high, split the work into:
   - diagnosis / impact assessment
   - implementation / validation
6. Keep the current turn narrow. Avoid expanding into adjacent cleanup unless the user explicitly approves it.
7. If the session grows noisy, emit a short handoff summary and continue in a fresh turn.

## Required output shape

When the request is vague, ask for only the missing items below:

- `Goal`: the exact thing to fix, build, or verify
- `Scope`: where you are allowed to look or edit
- `Do not do`: explicit non-goals
- `Validation`: how success will be checked

When enough information already exists, proceed without asking all four again.

Before implementation on a non-trivial task, provide a bounded plan in this format:

```text
Goal: ...
Scope: ...
Do not do: ...
Validation: ...
Approach: ...
```

## Guardrails

- Prefer the smallest working change over a broad "cleanup".
- Do not mix diagnosis, refactor, feature work, and release work in one pass unless the user asked for that bundle.
- Resist full-repo scans when the user already named a likely area.
- If a task starts drifting, stop and restate the current boundary.
- If more than one milestone has been completed, summarize state before continuing.

## Session compression

When context gets long or the user wants to restart cleanly, produce a compact handoff:

```text
Current goal:
Root cause / current finding:
Files touched:
Validation run:
Open risk:
Next smallest step:
```

## Prompt template

Offer this template when the user wants a reusable prompt:

```text
目标：...
范围：只看 ...
不要做：不要重构，不要顺手修别的，不要改未指定模块
交付：先给根因和最小方案；确认后直接改代码并做最小必要验证
完成标准：...
```

For English-first users:

```text
Goal: ...
Scope: only inspect/edit ...
Do not do: no refactor, no adjacent cleanup, no unrelated module changes
Deliverable: first give root cause and minimum fix; after confirmation, implement and run the smallest necessary validation
Done when: ...
```
