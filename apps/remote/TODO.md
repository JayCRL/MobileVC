# TODO / Bug 跟踪

> 更新日期：2026-05-31  
> 说明：旧清单里已经修复的项不再放在主待办里，只保留摘要方便回溯；仍未完全闭环的项放在“待回归”和“仍需处理”。

## 状态说明

- `[已修复]`：代码或发布链路已处理，并有基础验证
- `[待回归]`：已有修复或实现，但还需要最新客户端 / 实机 / 长任务场景确认
- `[待排查]`：还没有明确修复，需要继续定位

---

## 已修复

### [已修复] Relay 重启后重新注册 / 配对失败

**现象：**

- 客户端通过 `--relay` 成功连接一次后，停止后端再重启，可能无法重新注册或配对

**处理：**

- `mobilevc public --relay ...` 每次启动使用独立临时 `RELAY_AGENT_SESSION_STATE_PATH`
- 启动失败、配对超时和停止时都会清理临时 state
- 已随 `@justprove/mobilevc@0.2.9` 发布

**验证：**

- `npm run test:launcher`
- `npm pack --dry-run`
- npm latest 已确认是 `0.2.9`

### [已修复] iOS Xcode Archive 直接报签名错误

**现象：**

- 直接 Archive 时 Release 配置走到 `Apple Development` 签名，容易在签名阶段报错

**处理：**

- Runner 的 Release / Profile 配置明确使用 `Apple Distribution`
- OTA 脚本继续保持“无签名 archive，再由 export 阶段签名”的流程

**验证：**

- `Release` / `Profile` build settings 已显示 `CODE_SIGN_IDENTITY = Apple Distribution`
- 无签名 archive 编译验证通过

### [已修复] Android 发布包架构与安装入口

**现象：**

- Android 发布只需要 arm64 包，安装说明也需要和官网入口保持一致

**处理：**

- Android release 使用 `--target-platform android-arm64`
- 官网 Android 下载入口指向 `1.0.0 (20260531 arm64)`
- README 已改为“官网首页安装，iOS 走 TestFlight，Android 走 APK 下载”

**验证：**

- 公网 APK hash 与本地一致
- APK 内只包含 `arm64-v8a`

### [已修复] 权限授权链路重复 / 旧请求误批准

**现象：**

- 权限批准后曾出现热重启循环、旧 `permissionRequestId` 被误用、待权限期间普通文本误进 stdin 等问题

**处理：**

- 权限批准直接写回结构化 `control_response`
- 旧 `permissionRequestId` 会刷新为当前待处理请求
- 待权限期间普通输入由后端拦截
- Claude 默认采用官方 `permission-model-auto`

**验证：**

- 后端权限路由与输入守卫已有测试覆盖

---

## 已修复，待最新客户端回归

### [待回归] 会话接管后台后回退观察模式（原 #1）

**原现象：**

- iOS 端接管会话后切后台，再回来会回到观察模式

**当前处理：**

- 会话 `Ownership` 已区分 `mobilevc` / `claude-native`
- 连接或重连时会补发 `session_resume`
- 运行态使用 `ExecutionActive` 锁存，减少前后台切换造成的状态回滚

**回归建议：**

- 最新 TestFlight 客户端
- 接管电脑 Claude / Codex 会话后切后台 30 秒再切回
- 确认无需重新点接管即可继续输入

### [待回归] 接管后发送按钮闪烁（原 #2）

**原现象：**

- 接管会话后发送消息，发送按钮短暂变停止按钮又回退

**当前处理：**

- 运行态和停止态已经拆分
- `_isStopping`、`ExecutionActive` 和后端 session state 不再混用同一个 UI 判断

**回归建议：**

- 接管历史会话后发送一条普通消息
- 确认按钮状态、banner 和实际输入写入一致

### [待回归] 后台切回后状态球卡在工具调用（原 #6）

**原现象：**

- 任务已经完成，但左下角状态球仍显示 `edit`、`read` 等工具调用状态

**当前处理：**

- 运行态锁存和 completion / wait-input 事件处理已经收敛
- 前后台恢复会补抓状态，减少旧状态覆盖最新状态

**回归建议：**

- 后台执行一个包含工具调用的长任务
- 任务结束后切回前台，确认状态球恢复为空闲 / 等待输入

### [待回归] 任务执行时 thinking / reasoning 不显示（原 #3）

**当前状态：**

- 后端已加入 `ThinkingEvent`
- Flutter 端已有 thinking 模型、映射和 UI 样式
- 旧记录中已部署 iOS OTA `1.0.0 (20260529015155)`

**回归建议：**

- 用最新 TestFlight 客户端和最新后端测试 Claude thinking 输出
- 如果仍不显示，抓一段 WebSocket 事件流确认 thinking 块位置

### [待回归] 后台通知重复与长任务进度推送（原 #8）

**原现象：**

- 切后台时重复收到“需要继续输入”
- 长任务执行期间进度通知不持续

**当前处理：**

- action-needed 推送已做内容指纹去重
- agent 思考、工具执行、错误、assistant 回复等进度事件会触发 APNs 推送
- 进度推送 30 秒防抖，WebSocket 在线时跳过
- iOS / Android 后台且会话忙碌时会开启短时保活

**回归建议：**

- 后台跑 2 分钟以上长任务
- 确认不会重复轰炸，且关键进度能持续收到

---

## 仍需处理

### [待排查] 文件内容页面出现多余白色“等待输入”元素（原 #4）

**现象：**

- 查看文件内容时，输入框上方偶尔多出一个白色等待输入元素

**下一步：**

- 定位文件查看页是否错误复用了会话等待输入组件
- 检查页面切换时输入态和 session state 是否清理完整

### [待排查] Claude 上下文占用显示不准（原 #5）

**现象：**

- 上下文占用百分比会突然跳到 20%、100% 或 0%

**下一步：**

- 对照 Claude 实际 usage 字段，重新确认 `tokensUsed` / `tokenLimit`
- 检查 `context_window_usage` 事件是否乱序或被旧值覆盖

### [待排查] 单个 emoji 回复不立即显示（原 #7）

**现象：**

- Claude 只回复一个 emoji 时，聊天界面不立即显示，切后台再回来才出现

**下一步：**

- 抓 WebSocket 事件，确认后端是否已发送该 assistant message
- 检查前端噪音过滤、短文本过滤和 timeline flush 逻辑

### [待排查] 连接状态详情下方显示历史提示词（原 #9）

**现象：**

- 连接状态详情页面下方出现之前发送过的 prompt

**下一步：**

- 检查连接状态页是否误复用了消息列表或输入缓存
- 检查页面切换时上一条 prompt 是否被清理

### [待排查] Memory / Skill 目录与 Claude 默认目录不一致（原 #10）

**现象：**

- MobileVC 读取的 memory / skill 目录可能和 Claude 默认写入目录不一致，需要手动同步

**下一步：**

- 明确 Claude 默认目录和 MobileVC 配置目录
- 评估是否通过配置、软链或文件监听实现自动同步

---

## 下次回归优先级

1. Relay 重启重新配对：确认 `mobilevc public --relay ...` 停止后再启动仍能扫码
2. iOS TestFlight 客户端：确认接管、后台恢复、发送按钮和状态球
3. 后台长任务：确认 APNs 去重和进度推送
4. thinking 显示：确认最新 Claude 输出能在移动端展示
5. 未处理 UI 小问题：文件页白色元素、emoji 延迟、连接状态页 prompt 泄漏
