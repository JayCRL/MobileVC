import 'dart:io';
import 'package:flutter/foundation.dart';
import 'package:firebase_messaging/firebase_messaging.dart';
import 'push_notification_service.dart';

/// Firebase 推送服务实现（移动端）
class FirebasePushNotificationService implements PushNotificationService {
  bool _initialized = false;
  String? _cachedToken;

  @override
  bool get isAvailable => Platform.isIOS || Platform.isAndroid;

  @override
  Future<void> initialize() async {
    if (_initialized) return;

    if (!isAvailable) {
      debugPrint('[push] platform not supported');
      return;
    }

    try {
      // iOS 需要请求权限
      if (Platform.isIOS) {
        final settings = await FirebaseMessaging.instance.requestPermission(
          alert: true,
          badge: true,
          sound: true,
        );
        debugPrint('[push] iOS permission: ${settings.authorizationStatus}');
      }

      // 获取 token
      _cachedToken = await FirebaseMessaging.instance.getToken();
      debugPrint('[push] device token: $_cachedToken');

      _initialized = true;
    } catch (e) {
      debugPrint('[push] initialize failed: $e');
    }
  }

  @override
  Future<String?> getDeviceToken() async {
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
  void onMessageReceived(
      void Function(Map<String, dynamic> message) callback) {
    FirebaseMessaging.onMessage.listen((remoteMessage) {
      callback({
        'title': remoteMessage.notification?.title ?? '',
        'body': remoteMessage.notification?.body ?? '',
        'data': remoteMessage.data,
      });
    });
  }

  @override
  void onMessageOpenedApp(
      void Function(Map<String, dynamic> message) callback) {
    // 监听从通知打开 App
    FirebaseMessaging.onMessageOpenedApp.listen((remoteMessage) {
      callback({
        'title': remoteMessage.notification?.title ?? '',
        'body': remoteMessage.notification?.body ?? '',
        'data': remoteMessage.data,
      });
    });

    // 检查是否从通知冷启动
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

/// 工厂方法：创建移动端推送服务
PushNotificationService createPushNotificationService() {
  return FirebasePushNotificationService();
}
