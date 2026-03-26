---

# 📱 MobileVC — Claude Code 移动端全工作流控制台 ✨

![Go](https://img.shields.io/badge/Go-1.21-blue) ![Flutter](https://img.shields.io/badge/Flutter-3.13-blue) ![License](https://img.shields.io/badge/License-MIT-green)

---

## 中文版

### 简介

> 把 **Claude Code** 真正带到手机上。不是远程看一眼，而是 **用移动端逻辑完整接管你的 Claude Code 工作流**。

MobileVC 是一个面向手机浏览器与 Flutter App 的移动工作台，让你在离开电脑后，依然能继续控制本机正在运行的 Claude Code：

* 继续对话
* 处理权限确认
* 审核和切换多文件 diff
* 浏览文件与日志
* 恢复历史 session
* 管理会话上下文里的 Skill / Memory
* 在后台收到“Claude 现在需要你”的提醒

它不是把桌面界面硬塞到手机上，而是围绕 **手机上的决策动作** 重新组织了 Claude Code 的核心流程。

> 建议仅在本机或可信局域网中使用，不要直接暴露到公网。

---

### 为什么它有意思

Claude Code 很强，但真实工作里总会遇到这些场景：

* 你离开电脑了，但 Claude 正在等你确认权限
* 它已经改了多份文件，等你逐个审核 diff
* 你只想在手机上快速看日志、看文件、回一句话
* 你想恢复刚才那轮 session，而不是重新讲一遍上下文

**MobileVC 的目标**：把这些“必须回到电脑上才能做”的动作，变成手机上就能完成的自然操作。

* 拆分 Claude Code 的等待态，适合移动端交互
* 将 diff 审核变成点、切、批量操作的工作流
* Skill / Memory 成为会话上下文的一部分
* 将“需要你介入”的时机转成后台通知

---

### 核心卖点

#### 1. 真正围绕手机操作重构 Claude Code

* **继续输入**：Claude 需要你补一句话
* **权限确认**：允许或拒绝操作
* **代码审核**：accept / revert / revise
* **继续回复**：推动下一步等待态

#### 2. 多文件 diff 审核 — 手机上也丝滑

* 按修改组查看待审核内容
* 在同一组内切换多个文件
* 文件内容 / diff 双模式切换
* accept / revert / revise
* 支持一键接受全部待审核 diff

#### 3. Skill / Memory 无感同步、Claude 生成修改与可视化管理

* Claude 本地目录镜像 + 当前会话启用态
* UI 显示 `sourceOfTruth`、`syncState`、`driftDetected`、`lastSyncedAt`、`lastError`
* Skill / Memory 的启用与关闭统一放在各自管理面板，不再占用主界面顶部
* 支持直接让 Claude 生成或修改 Skill / Memory，结果会自动回写 catalog 并刷新管理面板

#### 4. 后台通知 — 移动协作的关键能力

* Claude 等待你操作时才通知
* 支持继续输入、权限确认、代码审核
* 去重边沿触发，避免重复通知

#### 5. 完整工作台体验

* 会话创建 / 切换 / 删除 / 恢复
* 聊天时间线与步骤状态
* 终端日志、execution 切换
* 文件树浏览、读取、下载
* runtime info 诊断
* pending / recent diff 恢复

---

### 核心能力总结

* 在手机上连接本机 Claude Code
* 新建 / 切换 / 恢复 session
* 区分普通输入、权限授权、diff 审核
* 浏览聊天时间线、步骤状态、错误与运行状态
* 文件树浏览、读取、下载
* 多文件 diff 审核（修改组、切换、批量操作）
* 终端日志按 execution 切换 `stdout` / `stderr`
* 会话级 Skill / Memory / Context 管理
* Skill / Memory 与本机 Claude 目录镜像同步
* 支持一句话让 Claude 生成或修改 Skill / Memory，并自动回写到 catalog
* runtime info 查看
* 后台通知触发 action-needed
* 可选 TTS 语音播报

---

### 架构与工作原理

```text
Mobile browser / Flutter app
         │
         ▼
  MobileVC Go server
         │
         ├─ WebSocket event stream
         ├─ Claude Code runtime / PTY runner
         ├─ session + projection store
         └─ Python ChatTTS sidecar (optional)
```

#### Go 服务入口

* `cmd/server/main.go`
* 支持 `/ws`、`/healthz`、`/download`、`/api/tts/synthesize`、静态 Web 工作台
* 读取 `AUTH_TOKEN` 和 `PORT`，默认 8001

#### WebSocket 编排

* 校验 token
* 建立长连接
* 分发 action
* 同步 session、skill、memory
* 汇总 runtime、store、文件系统为事件流
* 持久化 session projection

#### Claude 运行时链路

* Flutter/Web 输入 -> Go 后端
* active runner 管理
* exec / pty / claude 会话
* 输出映射成结构化事件回前端

#### 会话存储

* 默认路径：`~/.mobilevc/sessions`
* 持久化 `skills.catalog.json`、`memory.catalog.json`

---

### 快速启动

#### 1. 环境准备

* Go
* Claude Code
* 设置 `AUTH_TOKEN`

```bash
export AUTH_TOKEN=test
```

#### 2. 启动 Go 服务

```bash
AUTH_TOKEN=test go run ./cmd/server
```

#### 3. 健康检查

```bash
curl http://127.0.0.1:8001/healthz
```

#### 4. 打开工作台

```text
http://127.0.0.1:8001/
```

#### 5. 启动 Claude 会话

```text
claude
```

---

### Flutter 客户端

```bash
cd mobile_vc
flutter pub get
flutter run
```

> 确保 host/port/token 配置正确

---

### 可选 TTS

```bash
cd sidecar/chattts
python3 -m venv .venv
source .venv/bin/activate
pip install -r requirements.txt

PYTHON_BIN="$PWD/.venv/bin/python" \
CHATTTS_MODEL_DIR="$HOME/.cache/mobilevc/chattts" \
bash run.sh
```

---

### 测试

#### Flutter

```bash
cd mobile_vc
flutter test
```

#### Go

```bash
go test ./...
```

---

### 项目结构

```text
cmd/server/        # Go 服务入口
internal/          # 后端编排、运行时、协议、存储
web/               # 浏览器工作台
mobile_vc/         # Flutter 客户端
sidecar/chattts/   # 可选 TTS 侧车
```

---

## English Version

### Overview

MobileVC brings **Claude Code** to your mobile device — not just viewing, but **taking full control of your workflow** on the go.

* Continue conversations
* Handle permission confirmations
* Review & switch multi-file diffs
* Browse files & logs
* Resume past sessions
* Manage session Skill / Memory
* Receive notifications when Claude needs you

> Recommended for local or trusted LAN use only.

---

### Core Features

1. **Mobile-First Interaction**

   * Input, permissions, code review, next-step decisions separated for mobile UI
2. **Multi-File Diff Review**

   * Group view, switch files in a group, diff/content toggle, accept/revert/revise, bulk accept
3. **Skill / Memory Management**

   * Directory mirror view, per-session enable/disable inside dedicated management sheets, sync with local Claude
   * Ask Claude to generate or revise Skill / Memory, then auto-write the result back into the catalog
4. **Background Notifications**

   * Triggered only when action is needed, edge deduplication prevents spam
5. **Full Workflow Console**

   * Session creation/switch/delete/resume, timeline, terminal logs, runtime info, file tree, pending diffs

---

### Architecture

```text
Mobile browser / Flutter app
         │
         ▼
  MobileVC Go server
         │
         ├─ WebSocket event stream
         ├─ Claude Code runtime / PTY runner
         ├─ session + projection store
         └─ Python ChatTTS sidecar (optional)
```

* Go Server: session orchestration, runtime bridge, event protocol
* WebSocket layer: real-time state sync
* Claude runtime: native PTY support
* File-based session



