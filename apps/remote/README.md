# 📱 MobileVC — 手机就是你的 AI 编程助手控制台

<p align="center">
  <img src="mobile_vc/lib/logo-2.png" alt="MobileVC logo" width="220" />
</p>

<p align="center">
  <strong>摆脱键盘和鼠标，用手机直接接管电脑上的 Claude / Codex 会话。</strong>
</p>

<p align="center">
  <em>MobileVC 把等待、审批、审核和继续执行，整理成一套适合手机操作的闭环。</em>
</p>

<p align="center">
  <img src="https://img.shields.io/badge/Go-1.25-blue" />
  <img src="https://img.shields.io/badge/Flutter-3.41-blue" />
  <img src="https://img.shields.io/badge/License-MIT-green" />
  <img src="https://img.shields.io/npm/v/%40justprove%2Fmobilevc" />
</p>

<p align="center">
  <strong>官网：</strong><a href="https://www.mobilevc.top">https://www.mobilevc.top</a>
</p>

<p align="center">
  <strong>手机安装：</strong><a href="https://www.mobilevc.top">官网首页</a>（iOS 走 TestFlight，Android 走 APK 下载）
</p>

<p align="center">
  <strong>社区讨论：</strong><a href="https://linux.do">LINUX DO</a>
</p>

---

## 这是什么

MobileVC 不是远程桌面，也不是终端镜像。它把电脑上正在运行的 AI 助手 CLI 会话带到手机上，让你在离开电脑时继续推进任务：

- 接住权限请求
- 推进 Plan Mode
- 审核 diff
- 恢复历史会话
- 浏览文件、日志和运行状态
- 管理 Skill / Memory / Context

---

## 快速开始

### 一键安装启动

```bash
npm install -g @justprove/mobilevc
mobilevc start
```

首次启动会引导你完成基础配置，并打印本机地址、局域网地址和二维码。

> 配置 Token 时，终端输入内容不会显示，这是正常的。直接输入完整 Token 后按回车即可。

### 手机连接

1. 手机访问 [官网首页](https://www.mobilevc.top)
2. iOS 点击 TestFlight 链接安装，Android 点击 APK 下载
3. 打开客户端，使用扫码连接或手动配置
4. 连接成功后，在输入框输入 `claude` 或 `codex`

> 官网可能需要国内网络环境访问。如果页面打不开，先切换到国内网络后再安装。

### 常用命令

```bash
mobilevc status
mobilevc logs
mobilevc logs --follow
mobilevc config
mobilevc stop
```

### Relay 模式

不在同一 WiFi 下也能连：

```bash
mobilevc public --relay wss://relay.mobilevc.top:9443
```

如果只想走 Relay，不开放局域网：

```bash
mobilevc public --relay wss://relay.mobilevc.top:9443 --network-exposure-mode relay-only
```

Relay 只转发加密数据包，服务器看不到明文。密钥通过扫码带外传递。

自建 Relay 指南见 [docs/guides/relay-deployment.md](docs/guides/relay-deployment.md)。

---

## 最近更新

- **全新语音通话助手**：自配 Voice API / TTS API 后，先用语音和助手预沟通、确认任务，再一键交给本机 Claude / Codex 执行；支持自动识别停顿发送、调优静默判定（避免刚开口就被提前发送），交接后的执行态也不再误提示“需要补充信息”
- **Codex 移动端控制增强**：新增沙箱模式设置，细化权限控制，Codex 目标切换移入聊天工具栏，权限菜单改用审批图标，并跟随 Codex 默认 reasoning 展示
- **会话更稳**：会话历史窗口可配置、续接历史按窗口收敛；修复会话同步、上下文占用显示和切后台状态跳动等问题
- **公网 Relay 更可靠**：修复 `mobilevc public --relay` 重启后无法重新注册 / 配对，以及 agent 重连后陈旧连接的恢复问题
- **界面 polish**：优化移动端浅色模式视觉一致性，修复聊天键盘布局与输入冻结
- **发布 `0.2.10`**：各平台预编译后端二进制同步更新；Android 切到 `arm64-v8a`（官网入口指向 `1.0.0 (20260601 arm64)`），iOS Release / Profile 的 Archive 签名明确为 `Apple Distribution`

---

## 主要能力

- 语音通话预沟通，并把确认后的任务交给本机 Claude / Codex
- 继续电脑上的 Claude / Codex 会话
- 处理权限确认和 Plan Mode
- 按组查看并接受或回滚多文件 diff
- 浏览文件、日志、运行态和历史会话
- 管理 Skill / Memory / Session Context
- 接收权限和进度推送
- 可选 TTS
- Android 模拟器调试

---

## 对比定位

MobileVC 的定位不是“把终端塞进手机”，而是把 AI 编程助手的高频等待态做成手机上的结构化操作。下面只展示 MobileVC 在这些对比场景里的优势，不评价其他产品优劣。

| 对比对象 | MobileVC 展示出的优势 |
|---|---|
| Codex-Remote | 同时覆盖 Claude / Codex；本机 npm 后端可控；权限、Plan Mode、diff 审核、Skill / Memory / Context、文件和日志都在同一套移动端工作流里 |
| 传统 SSH / 远程终端工具 | 不需要在小屏幕里操作终端；常见动作变成按钮和卡片；扫码连接、Token 鉴权、通知、状态同步和文件 / diff 查看都围绕 AI 助手场景设计 |
| Happy | 更偏本机私有工作台：Go 后端跑在自己的开发机上，支持 LAN + 官方 Relay，安装可从 npm 开始，并围绕 Claude / Codex 的权限、审查、会话恢复和项目文件流做深度整合 |

---

## 工作方式

```text
Mobile browser / Flutter app
        │
        ▼
   MobileVC Go server
        │
        ├─ WebSocket event stream
        ├─ PTY / assistant runtime
        ├─ ADB + WebRTC bridge
        ├─ session store
        └─ ChatTTS sidecar (optional)
```

### Go 后端

- 入口：`cmd/server/main.go`
- 负责 `/ws`、`/healthz`、`/download`、`/api/tts/synthesize`
- 编排会话、权限、文件、日志、Skill / Memory 和 ADB 调试

### Flutter 客户端

- 入口：`mobile_vc/lib/main.dart`
- 根状态由 `SessionController` 驱动
- 首页是 `SessionHomePage`
- 负责把后端事件渲染成手机上可操作的界面

---

## 文档导航

- 文档索引：[docs/README.md](docs/README.md)
- 项目索引：[docs/project-index.md](docs/project-index.md)
- 架构蓝图：[docs/architecture/blueprint.md](docs/architecture/blueprint.md)
- 当前逻辑说明：[docs/architecture/current-logic.md](docs/architecture/current-logic.md)
- Relay 部署指南：[docs/guides/relay-deployment.md](docs/guides/relay-deployment.md)
- 推送集成清单：[docs/guides/push-integration-checklist.md](docs/guides/push-integration-checklist.md)
- Web 嵌入链路：[docs/guides/web-embed-path.md](docs/guides/web-embed-path.md)
- Flutter Web 白屏排查：[docs/troubleshooting/flutter-web-blank-screen.md](docs/troubleshooting/flutter-web-blank-screen.md)
- 变更历史：[CHANGELOG.md](CHANGELOG.md)

---

## 从源码构建

```bash
git clone https://github.com/JayCRL/MobileVC.git
cd MobileVC
npm install
cd mobile_vc
flutter pub get
flutter build web --release
cd ..
npm run sync:web
AUTH_TOKEN=test go run ./cmd/server
```

### Flutter 移动端开发

```bash
cd mobile_vc
flutter pub get
flutter run
```

### smoke test

```bash
AUTH_TOKEN=test ./scripts/test_smoke_flow.sh
```

---

## 发布与安装页

- `mobile_vc/scripts/build_ios_ota.sh`：生成 iPhone OTA IPA，并可选用于内部测试分发
- `mobile_vc/scripts/build_ios_testflight.sh`：TestFlight 打包 / 上传辅助脚本
- `mobile_vc/scripts/update_install_page_testflight.sh`：更新官网首页上的 TestFlight 信息
- `mobile_vc/scripts/render_install_page.py`：统一渲染官网安装入口页面
- Android release：`flutter build apk --release --target-platform android-arm64`

---

## 项目结构

```text
bin/               # npm 启动器
cmd/server/        # Go 服务入口
cmd/server/web/    # 当前嵌入到 Go 二进制的 Flutter Web 产物
internal/          # 后端编排、运行时、协议、存储
mobile_vc/         # Flutter 客户端
mobile_vc/scripts/ # iOS OTA / TestFlight / Android 安装页脚本
packages/          # npm 预编译后端二进制包
scripts/           # 仓库级构建脚本
sidecar/chattts/   # 可选 TTS 侧车
```

---

## 测试

```bash
go test ./...
cd mobile_vc && flutter test
```

---

## 开源许可

本项目采用 [MIT License](LICENSE) 开源协议。

---

## 贡献

欢迎提交 Issue 和 Pull Request。

如果你想参与开发或交流实现细节，可以到 [LINUX DO 社区](https://linux.do) 讨论。
