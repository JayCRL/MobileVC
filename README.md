# 📱 MobileVC — 手机就是你的 AI 编程助手控制台

<p align=”center”>
  <img src=”mobile_vc/lib/logo-2.png” alt=”MobileVC logo” width=”220” />
</p>

<p align=”center”>
  <strong>摆脱键盘和鼠标的束缚，用手机随时接管电脑上的 AI 编程助手（Claude / Codex）。</strong>
</p>

<p align=”center”>
  <em>MobileVC 把 AI 助手会话中的等待、审批、审核和继续执行，变成一套专为移动端设计的操作闭环。</em>
</p>

<p align=”center”>
  <img src=”https://img.shields.io/badge/Go-1.21-blue” />
  <img src=”https://img.shields.io/badge/Flutter-3.13-blue” />
  <img src=”https://img.shields.io/badge/License-MIT-green” />
  <img src=”https://img.shields.io/npm/v/mobilevc” />
</p>

<p align=”center”>
  <strong>官网：</strong><a href=”https://www.mobilevc.top”>https://www.mobilevc.top</a>
</p>

<p align=”center”>
  快速访问：<a href=”https://www.mobilevc.top”>mobilevc.top</a>
</p>

<p align=”center”>
  <strong>社区讨论：</strong><a href=”https://linux.do”>LINUX DO</a>
</p>

---

## 🚀 快速开始

### 一键安装启动（推荐）

```bash
# 1. 全局安装
npm install -g @justprove/mobilevc

# 2. 启动服务（首次会引导配置）
mobilevc start

# 3. 访问 Web 端
# 浏览器打开 http://localhost:8001
```

启动后会自动显示：
- 本机访问地址
- 局域网访问地址  
- 手机扫码连接二维码

### 移动端连接

**方式一：扫码连接**
1. 手机访问 https://mobilevc.top/install/ 安装客户端
2. 打开客户端，点击"扫码连接"
3. 扫描终端显示的二维码即可

**方式二：手动配置**
- 局域网：填写终端显示的局域网地址、端口和 Token
- 公网：配置反向代理后填写公网地址

### 常用命令

```bash
mobilevc status          # 查看服务状态
mobilevc logs            # 查看日志
mobilevc logs --follow   # 实时日志
mobilevc config          # 重新配置
mobilevc stop            # 停止服务
```

---

## 从源码构建（开发者）

如果你想从源码编译或参与开发：

```bash
# 克隆仓库
git clone https://github.com/JayCRL/MobileVC.git
cd MobileVC

# 安装依赖
npm install

# 构建 Flutter Web
cd mobile_vc
flutter pub get
flutter build web --release
cd ..

# 同步 Web 构建产物
npm run sync:web

# 启动 Go 后端
AUTH_TOKEN=test go run ./cmd/server

# 或构建二进制
go build -o mobilevc-server ./cmd/server
./mobilevc-server
```

### Flutter 移动端开发

```bash
cd mobile_vc
flutter pub get
flutter run
```

---

## 这是什么

MobileVC 不是桌面终端的镜像，也不是远程桌面的替代品。

它做的是一件更直接的事：**把电脑上的 AI 助手 CLI 会话变成可以被手机完整操控的工作台。**

你不在电脑前，也能继续把任务推进下去：

- 继续跟助手对话
- 批准或拒绝权限请求
- 接住 Plan Mode 的多轮计划交互
- 审核 diff、接受或回滚修改
- 浏览文件、日志和运行状态
- 恢复历史会话
- 管理 Skill / Memory / Session Context
- 在助手需要你时收到推送通知（iOS APNs）

MobileVC 解决的不是”怎么远程看见电脑”，而是：

> **怎么让你只靠手机，就能完成电脑上的几乎全部 AI 助手工作流。**

---

## ✨ 新功能

### v0.1.13 (2026-04-10)

#### 🌐 Flutter Web 完整集成
- ✅ Flutter Web 构建产物完整嵌入 Go 二进制
- ✅ 启动后端即可直接访问 Web 版（无需单独构建）
- ✅ 修复 JavaScript MIME 类型问题
- ✅ Web 端移除 Firebase 依赖（移动端保留推送功能）
- ✅ 二进制包含完整 Flutter Web 运行时（~38MB）

### v0.1.12 (2026-04-09)

#### 🎉 iOS APNs 推送通知
- ✅ 完整的 APNs 推送集成
- ✅ 支持 Token 和证书两种认证方式
- ✅ 自动在权限请求和代码审核时推送
- ✅ 异步推送，不阻塞主流程
- 📖 详细配置指南：[PUSH_INTEGRATION_CHECKLIST.md](PUSH_INTEGRATION_CHECKLIST.md)

#### 🌐 Flutter Web 支持
- ✅ Web 端迁移到 Flutter Web
- ✅ 与移动端代码共享
- ✅ 完整的 MobileVC 功能
- ✅ 响应式设计，支持桌面和移动浏览器
- 📖 迁移说明：[WEB_MIGRATION_COMPLETE.md](WEB_MIGRATION_COMPLETE.md)

#### 🔧 其他改进
- ✅ 修复会话衔接和 Flutter 端无感重连
- ✅ 规范化 session cwd symlink 路径
- ✅ 添加 AI 编程工作流 skill

---

## 核心价值

### 1. 为手机重写 AI 助手的交互

手机上的操作不该依赖键盘盲输。MobileVC 把 AI 助手会话的关键等待态拆出来，做成更适合触摸屏的一键动作和可视化面板：

- 普通输入
- 权限确认
- Plan Mode 继续/推进
- 代码审查
- 会话恢复
- 技能 / 记忆管理

### 2. 让你离开电脑也不掉线

你不需要守在桌面前，也不需要回到键盘旁。

- 出门在外也能继续推进任务
- 电脑不在身边也能批准修改
- 助手正在等待你时，手机能立即收到推送通知
- 复杂工作流不会因为离开键盘而中断

### 3. 把工作流做成”点一下就能继续”的手机体验

MobileVC 不是为了展示”能远程看见什么”，而是为了让你真正把事做完，而且做得更快、更直观：

- 看得见：skill 胶囊、memory 卡片、diff 组、日志和运行状态一目了然
- 点得快：启用 / 停用、允许 / 拒绝、接受 / 回滚都能直接点选
- 一键化：一键同步 skill / memory，一句话生成 skill，一句话修改 memory
- 自动化：助手生成结果可自动回写 catalog，并刷新管理面板
- 可视化：当前会话启用项、同步状态、最近同步时间都清楚展示

这是一套为手机设计的 AI 助手控制台。

---

## 主要功能

管 AI 助手会话

- 在手机上连接本机 AI 助手 CLI 会话（Claude / Codex）
- 继续当前任务，而不是重新开始
- 支持创建、切换、加载、删除会话

### 2. 权限确认与 Plan Mode

- 支持权限请求的允许 / 拒绝
- 自动识别已过期或被撤回的权限请求，并提醒等待 AI 重新发起
- 支持助手进入 Plan Mode 后的多轮计划交互
- 计划、权限、普通输入分流处理
- 移动端用按钮推进流程，不再依赖 CLI 盲输

### 3. 多文件 Diff 审核

- 按修改组查看待审内容
- 在同一组内切换多个文件
- 查看文件内容或 diff
- 支持 accept / revert / revise
- review 操作会自动锁定当前真正待处理的 diff，避免切换文件后误点到旧目标
- 审核决策会发送显式文本指令，对 Claude / Codex 的兼容性更稳定
- 支持一键接受全部待审核 diff

### 4. 文件、日志、运行控制与状态查看

- 浏览项目文件树
- 读取文件内容
- 通过 HTTP 下载文件
- 查看终端执行日志
- 在不同 execution 间切换 stdout / stderr
- 会话执行中可直接从输入栏停止当前运行
- 自动清理终端噪音（`Wall time`、空 `Output:` 报头、重复错误），时间线只保留有效内容
- 查看 runtime info 和 session 历史

### 5. Skill / Memory / Session Context 管理

- Skill 以“胶囊”形式展示，轻点即可执行，长按即可查看详情和修改入口
- Memory 以“卡片”形式展示，内容、启用状态、来源和同步状态一眼可见
- 支持一键同步 skill / memory，和本机助手目录保持一致
- 支持一句话生成 skill、一句话修改 skill / memory
- 结果可自动回写 catalog，并立即刷新管理面板

### 6. 后台提醒

- 当助手需要你操作时发送提醒
- 覆盖继续输入、权限确认、Plan Mode、代码审核
- 支持前台到后台的过渡排队：`inactive` 状态收到的提醒会在真正进入后台后补发
- 从 `inactive` 恢复到前台时也会补抓一次遗漏提醒，并按内容指纹去重
- Android 初始化时会主动请求通知权限，iOS / macOS 也会同步申请系统通知授权
- iOS / Android 在后台且会话忙碌时会自动开启短时保活，降低关键提醒丢失概率
- 通过 action-needed 信号去重，避免重复打扰

### 7. 可选 TTS

- 支持把助手的关键信息转成语音
- 更适合移动中、通勤中或不方便盯屏的场景

### 8. Android 模拟器调试

- Flutter 端右上角提供 ADB 调试入口
- 后端会自动检测本机 `adb`、`emulator`、可用 AVD 和已连接设备
- 已有在线设备时可直接进入调试，通过 `WebRTC + H264` 实时推送模拟器画面到移动端
- 没有在线设备但存在可用 AVD 时，可在前端直接启动模拟器
- 在移动端点击预览画面会通过 WebRTC DataChannel 即时回传为 `adb tap`

---

## 系统架构

```text
Mobile browser / Flutter app
         │
         ▼
  MobileVC Go server
         │
         ├─ WebSocket event stream
         ├─ Assistant CLI runtime / PTY runner
         ├─ ADB / Android Emulator + WebRTC(H264) bridge
         ├─ session + projection store
         └─ Python ChatTTS sidecar (optional)
```

### Go 后端

- 入口：`cmd/server/main.go`
- 负责 `/ws`、`/healthz`、`/download`、`/api/tts/synthesize`
- 通过 WebSocket 驱动完整会话状态流
- 管理 PTY runner、ADB 调试、session store、Skill / Memory、文件系统与 TTS

### Flutter 客户端

- 入口：`mobile_vc/lib/main.dart` -> `mobile_vc/lib/app/app.dart`
- 根状态由 `SessionController` 驱动
- 首页是 `SessionHomePage`
- 负责把后端事件变成手机上可操作的 UI 状态
- 右上角 ADB 图标可打开模拟器调试面板

### 后端协议

Go 后端通过结构化事件流向前端推送状态，例如：

- `runtime_phase`
- `interaction_request`
- `session_history`
- `skill_catalog_result`
- `memory_list_result`
- `file_diff`
- `prompt_request`
- `agent_state`

---

## 工作原理

1. Flutter 连接 Go 后端 WebSocket
2. Go 后端启动或恢复 AI 助手 CLI 的 PTY 会话
3. 助手在执行中发出等待态、权限态、计划态等结构化信号
4. Flutter 将这些信号渲染成适合手机的操作界面
5. 用户在手机上批准、继续、回退、审核或输入
6. 决策再回灌给助手，形成完整闭环

这套设计的核心不是“远程操作一台电脑”，而是：

> **让手机成为你操控电脑上 AI 助手的主入口。**

---

## 快速开始

推荐按照下面这条路径上手：

1. 在电脑上安装并启动 MobileVC
2. 在手机上安装客户端并连接
3. 开始使用 AI 助手

详细步骤见上方"🚀 快速开始"章节。

---

MobileVC 支持把 `Claude` 或 `codex` 作为 AI 引擎使用（例如在移动端连接配置里把 `Engine` 设置为 `codex`）。

- Skill 执行会按 `Engine=codex` 路由为 `codex "<prompt>"`。
- 运行态模型识别会显示为 `codex`。
- `runtime_info: doctor` 会额外检查 `codex` CLI 是否可用。

> 说明：当前“会话热恢复/权限热切换”在支持 `resume` 的引擎下体验更完整；Codex 以通用 PTY 交互能力为主。

### 会话目录过滤与电脑端 Codex 无感恢复

当你在 Flutter 端进入某个项目目录后，再打开“会话列表”，MobileVC 会：

- 把当前目录作为 `cwd` 传给后端
- 只展示这个目录下相关的 MobileVC 本地会话
- 同时自动合并电脑上该目录对应的原生 Codex 会话

这意味着你可以直接在手机端：

- 进入项目目录
- 打开会话管理
- 看到之前在电脑上用 Codex 跑过的历史会话
- 点开后直接继续聊，不需要手动记会话 ID，也不需要手动输入 `codex resume ...`

实现方式上，后端会读取本机：

- `~/.codex/state_5.sqlite`
- `~/.codex/history.jsonl`

并为这些电脑端原生会话建立 MobileVC 本地镜像记录。镜像只负责：

- 在手机端展示历史
- 记录当前移动端继续输入后的投影状态
- 把后续消息自动路由成对应的 `codex resume <session-id>` 续聊

列表里标记为 `电脑 Codex` 的会话就是这类原生 Codex 会话。它们支持加载和继续，不支持在 MobileVC 内删除。

> Smoke test：运行 `AUTH_TOKEN=test ./scripts/test_smoke_flow.sh` 可快速验证后端、WebSocket 与会话主链路。
>
> Codex smoke：运行 `AUTH_TOKEN=test ./scripts/smoke_codex_backend.sh` 可验证 Codex 适配后的后端启动、WS 会话与基础交互链路。

### 7.1 Android 模拟器调试

后端会优先自动探测：

- `ADB_PATH` / `EMULATOR_PATH`
- `ANDROID_HOME` / `ANDROID_SDK_ROOT`
- macOS 常见 SDK 路径，如 `~/Library/Android/sdk`

如果你有多个 ADB server 冲突，也可以显式指定：

```bash
ADB_SERVER_PORT=5038 AUTH_TOKEN=test go run ./cmd/server
```

进入方式：

1. 启动后端并连接 Flutter 客户端
2. 点击右上角手机图标打开 `ADB 调试`
3. 如果已检测到在线设备，直接点“进入调试”
4. 如果没有在线设备但检测到 AVD，直接点“启动模拟器”
5. 视频链路会通过 `WebRTC + H264` 建立，点击控制通过 DataChannel 回传
6. 画面出现后，可在移动端直接点击预览画面进行调试

说明：

- 当前首帧通常会在模拟器画面发生变化后出现，因此连接后如果界面完全静止，首帧可能略晚到达
- 如本机存在多个 ADB server 冲突，建议显式指定 `ADB_SERVER_PORT`

### 8. 启动 AI 助手会话（示例）

```text
claude
# 或
codex
```

### 9. 仍然支持直接启动 Go 后端

如果你想绕过 Node 启动器，原来的方式仍然可用：

```bash
AUTH_TOKEN=test go run ./cmd/server
```

---

## Flutter 客户端

```bash
cd mobile_vc
flutter pub get
flutter run
```

> 确保 host / port / token 配置正确。
>
> 在 iOS / Android 上，如果 app 已退到后台但当前会话还在运行，客户端会自动开启约 90 秒的短时后台保活，尽量把最后一轮回复和提醒接住。

---

## 测试

### Smoke test

建议优先运行一次 smoke test，先确认本地后端、鉴权和 WebSocket 主链路正常。

运行前请先确认本地 Go 服务已启动且 `AUTH_TOKEN` 与测试命令一致。

可直接运行 `AUTH_TOKEN=test ./scripts/test_smoke_flow.sh` 做一次最小主链路自检。

建议先启动 Go 服务，再运行一次最小主链路自检命令。

Smoke test：`AUTH_TOKEN=test ./scripts/test_smoke_flow.sh`，用于快速验证后端、WebSocket 和会话流是否可用。
它会连接本地服务并跑一轮最小端到端流程，帮助你确认环境是否正常。
如果该命令通过，通常说明鉴权、WebSocket 与会话主链路都已就绪。
建议在启动 Go 服务后先跑一次，快速确认 WebSocket、会话流和鉴权都可用。
也可在使用 `mobilevc start` 启动后立即执行同一命令做一次主链路自检。

```bash
AUTH_TOKEN=test ./scripts/test_smoke_flow.sh
```

### Go

```bash
go test ./...
```

### Flutter

```bash
cd mobile_vc
flutter test
```

---

## 项目结构

```text
cmd/server/        # Go 服务入口
internal/          # 后端编排、运行时、协议、存储
web/               # 浏览器工作台
mobile_vc/         # Flutter 客户端
sidecar/chattts/   # 可选 TTS 侧车
```

---

## 最近更新（2026-04-04）

- Review 流程更稳：审核按钮会优先绑定当前真正待处理的 diff，等待 review 输入时不会因为时间线刷新或打开文件切换而丢失审核提示；`accept / revert / revise` 也会发送显式文本指令，而不是依赖数字选项猜测。
- 可直接停止当前运行：当会话仍在执行、但还没进入等待输入态时，输入栏主按钮会切换成 `Stop`，可从手机端直接终止当前运行。
- 后台保活补强：在 iOS / Android 上，只要 app 已退到后台且当前会话仍在忙碌，客户端会自动申请一小段后台保活时间，尽量把最后一轮回复、权限或 review 提醒送达。
- 通知链路更完整：Android 初始化时会主动请求通知权限；从 `inactive` 恢复到前台时，如果中途有权限、回复或 action-needed 信号，也会补发并做去重，避免漏提醒或重复提醒。
- 会话时间线继续瘦身：终端日志流会自动剥离 `Wall time` 报头、空的 `Output:` 包裹、ANSI/结构化日志和重复性噪音，只保留真正需要你关注的 stderr/stdout 内容。

---

## English Summary

MobileVC turns your phone into the control center for an AI coding assistant CLI session (Claude or Codex) running on your computer.

It is built for the moments when you are away from the keyboard but still need to keep shipping: approve permissions, handle Plan Mode, review diffs, inspect files and logs, resume sessions, and keep the workflow moving.

### What it gives you

- Mobile AI assistant control
- Permission confirmations
- Plan Mode handling
- Multi-file diff review
- File / log / runtime inspection
- Session resume and history
- Directory-scoped session discovery with seamless desktop Codex resume
- Skill capsules and memory cards
- One-tap sync and AI-assisted authoring
- Skill / Memory / Context management
- Optional TTS notifications

### The idea

Not a terminal mirror.
Not a desktop clone.

**A phone-first workflow that lets you operate your desktop AI coding assistant almost entirely from mobile.**

---

## 📄 开源许可

本项目采用 [MIT License](LICENSE) 开源协议。

这意味着你可以：
- ✅ 自由使用、复制、修改、合并、发布、分发本软件
- ✅ 用于商业或非商业目的
- ✅ 再许可和销售软件副本

唯一的要求是：
- 📋 在所有副本中保留版权声明和许可声明

详细条款请查看 [LICENSE](LICENSE) 文件。

### 贡献

欢迎提交 Issue 和 Pull Request！

如果你想参与开发或有任何建议，欢迎到 [LINUX DO 社区](https://linux.do) 讨论交流。

---
