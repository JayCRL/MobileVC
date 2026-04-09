# iOS APNs 推送集成检查清单

## ✅ 已完成的代码集成

### Go 后端
- [x] 推送服务接口 `internal/push/service.go`
- [x] APNs 推送实现（支持 Token 和证书认证）
- [x] Push token 存储方法 `internal/store/file_store.go`
- [x] WebSocket handler 集成 `internal/ws/handler.go`
- [x] 推送辅助函数 `internal/ws/push_helper.go`
- [x] 推送触发逻辑（在 `PromptRequestEvent` 和 `InteractionRequestEvent` 时自动发送）
- [x] `register_push_token` action 处理
- [x] Go 依赖已安装 `github.com/sideshow/apns2`

### Flutter 端
- [x] 推送服务抽象层 `mobile_vc/lib/app/push_notification_service.dart`
- [x] Firebase 推送实现
- [x] App 生命周期集成 `mobile_vc/lib/app/app.dart`
- [x] SessionController token 注册方法
- [x] pubspec.yaml 添加 `firebase_core` 和 `firebase_messaging`
- [x] iOS AppDelegate 初始化 Firebase

## 📋 需要手动完成的配置步骤

### 1. 获取 APNs 认证凭据

#### 方式 A: Token 认证（推荐）
1. 登录 [Apple Developer](https://developer.apple.com/account/resources/authkeys/list)
2. 创建 APNs Auth Key (.p8 文件)
3. 记录以下信息：
   - Key ID（10 位字符）
   - Team ID（在 Membership 页面查看）
   - 下载 .p8 文件

#### 方式 B: 证书认证
1. 在 Xcode 中生成 Push Notification 证书
2. 导出为 .p12 文件
3. 记录证书密码

### 2. 配置 Firebase 项目

1. 访问 [Firebase Console](https://console.firebase.google.com/)
2. 创建新项目或选择现有项目
3. 添加 iOS 应用：
   - Bundle ID: `com.yourcompany.mobilevc`（需要与 Xcode 项目一致）
   - 下载 `GoogleService-Info.plist`
4. 上传 APNs 认证凭据到 Firebase：
   - 进入 Project Settings > Cloud Messaging
   - 上传 .p8 文件或 .p12 证书

### 3. 配置 iOS 项目

1. 将 `GoogleService-Info.plist` 放到 `mobile_vc/ios/Runner/` 目录
2. 在 Xcode 中打开 `mobile_vc/ios/Runner.xcworkspace`
3. 确认 Bundle ID 与 Firebase 配置一致
4. 启用 Push Notifications capability：
   - 选择 Runner target
   - Signing & Capabilities
   - 点击 + Capability
   - 添加 Push Notifications

### 4. 配置后端环境变量

创建 `.env` 文件或在启动时设置：

```bash
# 基础配置
export AUTH_TOKEN=test
export PORT=8001

# APNs Token 认证（推荐）
export APNS_AUTH_KEY_PATH="/path/to/AuthKey_XXXXXXXXXX.p8"
export APNS_KEY_ID="XXXXXXXXXX"
export APNS_TEAM_ID="XXXXXXXXXX"
export APNS_TOPIC="com.yourcompany.mobilevc"
export APNS_PRODUCTION="false"  # 开发环境用 false

# 或使用证书认证
# export APNS_CERT_PATH="/path/to/cert.p12"
# export APNS_CERT_PASS="your_password"
```

### 5. 启动服务

```bash
# 启动后端
cd /Users/wust_lh/MobileVC
source .env  # 或直接 export 环境变量
go run ./cmd/server

# 运行 Flutter App
cd mobile_vc
flutter run
```

### 6. 测试推送

1. 启动 App，确认日志中显示 token 注册成功
2. 让 Claude 执行需要权限的操作（如修改文件）
3. 立即切到后台或锁屏
4. 应该收到推送通知："Claude 需要你的授权确认"
5. 点击通知，App 自动打开

## 🔍 故障排查

### 后端日志检查

```bash
# 查看推送相关日志
grep "push" /path/to/server.log

# 查看 token 注册
cat ~/.mobilevc/sessions/push_tokens.json
```

### Flutter 端检查

```bash
# 查看 Flutter 日志
flutter logs | grep push
```

### 常见问题

1. **推送未收到**
   - 检查 APNs 配置是否正确
   - 确认 token 已注册到后端
   - 查看后端日志中的 APNs 响应状态码

2. **Flutter 无法获取 token**
   - 确认 Firebase 配置正确
   - 确认 iOS 推送权限已授予
   - 检查 `GoogleService-Info.plist` 位置

3. **APNs 返回 403**
   - Token/证书无效或过期
   - Team ID 或 Key ID 不匹配
   - Topic (Bundle ID) 不匹配

4. **APNs 返回 410**
   - Device token 无效（用户卸载重装或系统重置）
   - 需要清理无效 token

## 📝 生产环境注意事项

1. 使用 Token 认证而非证书（Token 永不过期）
2. 设置 `APNS_PRODUCTION=true`
3. 妥善保管 .p8 文件，不要提交到 Git
4. 监控推送失败率，及时清理无效 token
5. 考虑添加推送频率限制
6. 添加推送统计和监控

## 🎯 下一步优化

- [ ] 支持 Android FCM 推送
- [ ] 添加推送内容模板和本地化
- [ ] 支持静默推送（background fetch）
- [ ] 添加推送统计和监控面板
- [ ] 支持推送频率限制和去重
