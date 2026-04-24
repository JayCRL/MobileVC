# iOS APNs 推送配置指南

本文档说明如何完成 iOS APNs 推送的配置和集成。

## 已完成的工作

### 1. Go 后端
- ✅ 创建推送服务接口 `internal/push/service.go`
- ✅ 实现 APNs 推送服务（基于 `github.com/sideshow/apns2`）
- ✅ 在 `internal/store` 中添加 push token 存储方法
- ✅ 在 `internal/ws/handler.go` 中添加 `register_push_token` 处理
- ✅ 创建推送辅助函数 `internal/ws/push_helper.go`

### 2. Flutter 端
- ✅ 创建推送服务抽象层 `mobile_vc/lib/app/push_notification_service.dart`
- ✅ 在 `app.dart` 中集成推送初始化
- ✅ 在 `session_controller.dart` 中添加 token 注册方法

## 需要手动完成的步骤

### 步骤 1: 安装 Go 依赖

```bash
cd /Users/wust_lh/MobileVC
go get github.com/sideshow/apns2
go mod tidy
```

### 步骤 2: 配置 APNs 证书

#### 方式 A: 使用 Token 认证（推荐）

1. 登录 [Apple Developer](https://developer.apple.com/account/resources/authkeys/list)
2. 创建 APNs Auth Key (.p8 文件)
3. 记录 Key ID 和 Team ID
4. 将 .p8 文件保存到服务器（如 `~/.mobilevc/apns_auth_key.p8`）

#### 方式 B: 使用证书认证

1. 在 Xcode 中生成 Push Notification 证书
2. 导出为 .p12 文件
3. 保存到服务器（如 `~/.mobilevc/apns_cert.p12`）

### 步骤 3: 修改 `cmd/server/main.go`

在 `main.go` 中初始化推送服务：

```go
import (
    "mobilevc/internal/push"
    // ... 其他 import
)

func main() {
    // ... 现有代码 ...
    
    // 初始化推送服务
    var pushService push.Service
    
    // 从环境变量读取配置
    apnsAuthKeyPath := os.Getenv("APNS_AUTH_KEY_PATH")
    apnsKeyID := os.Getenv("APNS_KEY_ID")
    apnsTeamID := os.Getenv("APNS_TEAM_ID")
    apnsTopic := os.Getenv("APNS_TOPIC") // 你的 App Bundle ID
    apnsProduction := os.Getenv("APNS_PRODUCTION") == "true"
    
    if apnsAuthKeyPath != "" && apnsKeyID != "" && apnsTeamID != "" && apnsTopic != "" {
        apnsService, err := push.NewAPNsService(push.APNsConfig{
            AuthKeyPath: apnsAuthKeyPath,
            KeyID:       apnsKeyID,
            TeamID:      apnsTeamID,
            Topic:       apnsTopic,
            Production:  apnsProduction,
        })
        if err != nil {
            logx.Warn("bootstrap", "initialize APNs service failed: %v", err)
            pushService = &push.NoopService{}
        } else {
            pushService = apnsService
            logx.Info("bootstrap", "APNs service ready: topic=%s production=%v", apnsTopic, apnsProduction)
        }
    } else {
        logx.Info("bootstrap", "APNs not configured, push notifications disabled")
        pushService = &push.NoopService{}
    }
    
    // 将 pushService 传给 wsHandler
    wsHandler := ws.NewHandler(cfg.AuthToken, sessionStore)
    wsHandler.PushService = pushService
    
    // ... 其他代码 ...
}
```

### 步骤 4: 配置环境变量

创建 `.env` 文件或在启动时设置：

```bash
export APNS_AUTH_KEY_PATH="/path/to/apns_auth_key.p8"
export APNS_KEY_ID="YOUR_KEY_ID"
export APNS_TEAM_ID="YOUR_TEAM_ID"
export APNS_TOPIC="com.yourcompany.mobilevc"
export APNS_PRODUCTION="false"  # 开发环境用 false，生产环境用 true
```

### 步骤 5: Flutter 端配置 Firebase

1. 在 `mobile_vc/pubspec.yaml` 中添加依赖：

```yaml
dependencies:
  firebase_core: ^2.24.0
  firebase_messaging: ^14.7.0
```

2. 修改 `mobile_vc/lib/app/push_notification_service.dart`：

删除动态导入相关代码，直接使用：

```dart
import 'package:firebase_messaging/firebase_messaging.dart';

class FirebasePushNotificationService implements PushNotificationService {
  bool _initialized = false;
  String? _cachedToken;

  @override
  bool get isAvailable => Platform.isIOS;

  @override
  Future<void> initialize() async {
    if (_initialized) return;

    if (Platform.isIOS) {
      final settings = await FirebaseMessaging.instance.requestPermission(
        alert: true,
        badge: true,
        sound: true,
      );
      debugPrint('[push] iOS permission: ${settings.authorizationStatus}');
    }

    _cachedToken = await FirebaseMessaging.instance.getToken();
    debugPrint('[push] device token: $_cachedToken');
    _initialized = true;
  }

  @override
  Future<String?> getDeToken() async {
    if (!_initialized) await initialize();
    return _cachedToken;
  }

  @override
  void onTokenRefresh(void Function(String token) callback) {
    FirebaseMessaging.instance.onTokenRefresh.listen((token) {
      _cachedToken = token;
      callback(token);
    });
  }

  @override
  void onMessageReceived(void Function(Map<String, dynamic> message) callback) {
    FirebaseMessaging.onMessage.listen((remoteMessage) {
      callback({
        'title': remoteMessage.notification?.title ?? '',
        'body': remoteMessage.notification?.body ?? ' 'data': remoteMessage.data,
      });
    });
  }

  @override
  void onMessageOpenedApp(void Function(Map<String, dynamic> message) callback) {
    FirebaseMessaging.onMessageOpenedApp.listen((remoteMessage) {
      callback({
        'title': remoteMessage.notification?.title ?? '',
        'body': remoteMessage.notification?.body ?? '',
        'data': remoteMessage.data,
      });
    });

    FirebaseMessaging.instance.getInitialMessage().then((remoteMessage) {
      if (remoteMessage != null) {
        callback({
          'title': remoteMessage.notification?.title ?? '',
          'body': remoteMessage.notification?.body ?? '',
          'data': remoteMessage.data,
        });
      }
    });
  }
}
```

3. 配置 Firebase 项目：
   - 访问 [Firebase Console](https://console.firebase.google.com/)
   - 创建项目并添加 iOS 应用
   - 下载 `GoogleService-Info.plist` 放到 `mobile_vc/ios/Runner/`
   - 上传 APNs 证书到 Firebase Console

4. 在 `mobile_vc/ios/Runner/AppDelegate.swift` 中初始化 Firebase：

```swift
import UIKit
import Flutter
import Firebase

@UIApplicationMain
@objc class AppDelegate: FlutterAppDelegate {
  override func application(
    _ application: UIApplication,
    didFinishLaunchingWithOptions launchOptions: [UIApplication.LaunchOptionsKey: Any]?
  ) -> Bool {
  seApp.configure()
    GeneratedPluginRegistrant.register(with: self)
    return super.application(application, didFinishLaunchingWithOptions: launchOptions)
  }
}
```

### 步骤 6: 在后端触发推送

在 `internal/ws/handler.go` 的事件发送处添加推送触发：

找到发送 `PromptRequestEvent` 和 `InteractionRequestEvent` 的地方，添加：

```go
// 发送事件后立即尝试推送通知
emit(promptEvent)
h.sendPushNotificationIfNeeded(ctx, sessionID, promptEvent)
```

### 步骤 7: 测试推送

1. 启动后端：
```bash
cd /Users/wust_lh/MobileVC
AUTH_TOKEN=test \
APNS_AUTH_KEY_PATH="/path/to/key.p8" \
APNS_KEY_ID="YOUR_KEY_ID" \
APNS_TEAM_ID="YOUR_TEAM_ID" \
APNS_TOPIC="com.yourcompany.mobilevc" \
APNS_PRODUCTION="false" \
go run ./cmd/server
```

2. 运行 Flutter App，确认 token 注册成功
3. 让 Claude 执行需要权限的操作
4. 切到后台，应该收到推送通知

## 故障排查

### 推送未收到

1. 检查后端日志：
```bash
grep "push" /path/to/server.log
```

2. 检查 token 是否注册：
```bash
cat ~/.mobilevc/sessions/push_tokens.json
```

3. 检查 APNs 响应：
- Status 200: 成功
- Status 400: 请求格式错误
- Status 403: 证书/Token 无效
- Status 410: Device token 无效

### Flutter 端无法获取 token

1. 确认 Firebase 配置正确
2. 确认 iOS 推送权限已授予
3. 检查 `GoogleService-Info.plist` 是否在正确位置

## 生产环境注意事项

1. 使用 Token 认证而非证书（Token 永不过期）
2. 设置 `APNS_PRODUCTION=true`
3. 妥善保管 .p8 文件，不要提交到 Git
4. 监控推送失败率，及时清理无效 token
5. 考虑添加推送频率限制，避免骚扰用户

## 后续优化

1. 支持 Android FCM 推送（使用个推统一接入）
2. 添加推送内容模板和本地化
3. 支持静默推送（background fetch）
4. 添加推送统计和监控
