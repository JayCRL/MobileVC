# MobileVC 项目规则

## 代码改动与编译

### 后端改动流程
1. 在项目根目录执行编译：`go build -o server ./cmd/server`
2. 停止旧进程：`pkill -f './server'`
3. 启动新进程（必须带完整环境变量）：
```bash
AUTH_TOKEN="test-token-12345" \
APNS_AUTH_KEY_PATH="/Users/wust_lh/AuthKey_UUZWBMP5Z5.p8" \
APNS_KEY_ID="UUZWBMP5Z5" \
APNS_TEAM_ID="4YFXZ5X5YJ" \
APNS_TOPIC="com.justprove.mobilevc" \
APNS_PRODUCTION="false" \
nohup ./server > server.log 2>&1 &
```
4. 验证启动成功：检查 `server.log` 确认看到 `APNs service ready` 和 `Ready: addr=:8001`

### Flutter 改动流程
1. 编译检查
2. 打包上传 iOS OTA：`source ~/.zshrc && mobilevc-ota`
3. 详细流程参考 memory 中的 `ios_ota_build_workflow.md`

### 改动范围原则
- 只改动后端 → 只重启后端
- 只改动 Flutter → 只打包上传 iOS
- 同时改动两端 → 分别执行各自的流程

## 核心稳定区

以下模块是与 Claude 交互的核心逻辑，改动前必须：
1. 先查 memory 和 CONTEXT.md
2. 理解现有实现
3. 不确定时先问用户

**核心模块**：
- `internal/session/` - 会话管理和控制器状态
- `internal/runner/` - 命令执行和 Claude 交互
- `internal/ws/` - WebSocket 处理和协议通信
- `internal/runtime/` - 运行时管理

## 前后端对接规则

**涉及 Flutter 端与后端对接的功能改动时**：
1. 必须读两端的代码
2. 理解对接方式（协议、数据结构、事件流）
3. 确保改动后两端协议一致

## 代码理解原则

**当不知道具体的代码逻辑时**：
- 必须先问用户
- 不要想当然或猜测实现
- 先读代码理解现有逻辑，不确定时再问

## Git 提交规则

- 可以自动 commit 到本地仓库
- push 到 GitHub 远程仓库前必须经过用户确认
- 提交时排除 `.claude/worktrees/` 等环境噪音

## 遇到不确定的情况

- 代码逻辑不清楚 → 先问用户
- 改动方案有多种选择 → 先问用户
- 编译/启动失败 → 报告错误，等待用户指示
- 涉及核心稳定区 → 先查 memory，不确定时问用户
