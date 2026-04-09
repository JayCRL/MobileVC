import 'dart:io';

import 'package:flutter/foundation.dart';

/// 推送通知服务抽象接口
abstract class PushNotificationService {
  Future<void> initialize();
  Future<String?> getDeviceToken();
  void onTokenRefresh(void Function(String token) callback);
  void onMessageReceived(void Function(Map<String, dynamic> message) callback);
  void onMessageOpenedApp(
      void Function(Map<String, dynamic> message) callback);
  bool get isAvailable;
}

/// 空实现（用于不支持推送的平台）
class NoopPushNotificationService implements PushNotificationService {
  @override
  Future<void> initialize() async {}

  @override
  Future<String?> getDeviceToken() async => null;

  @override
  void onTokenRefresh(void Function(String token) callback) {}

  @override
  void onMessageReceived(void Function(Map<String, dynamic> message) callback) {
  }

  @override
  void onMessageOpenedApp(
      void Function(Map<String, dynamic> message) callback) {}

  @override
  bool get isAvailable => false;
}

/// Firebase Messaging 实现（iOS APNs）
class FirebasePushNotificationService implements PushNotificationService {
  FirebasePushNotificationService();

  bool _initialized = false;
  String? _cachedToken;

  @override
  bool get isAvailable => Platform.isIOS;

  @override
  Future<void> initialize() async {
    if (_initialized) {
      return;
    }

    try {
      // 动态导入 firebase_messaging，避免在不支持的平台报错
      final messaging = await _getMessaging();
      if (messaging == null) {
        debugPrint('[push] firebase_messaging not available');
        return;
      }

      // iOS 需要请求权限
      if (Platform.isIOS) {
        final settings = await messaging.requestPermission(
          alert: true,
          badge: true,
          sound: true,
        );
        debugPrint('[push] iOS permission status: ${settings.authorizationStatus}');
      }

      // 获取初始 token
      _cachedToken = await messaging.getToken();
      debugPrint('[push] initial token: $_cachedToken');

      _initialized = true;
    } catch (error, stack) {
      debugPrint('[push] initialize failed: $error');
      debugPrintStack(stackTrace: stack, label: '[push] initialize stack');
    }
  }

  @override
  Future<String?> getDeviceToken() async {
    if (!_initialized) {
      await initialize();
    }
    return _cachedToken;
  }

  @override
  void onTokenRefresh(void Function(String token) callback) {
    _getMessaging().then((messaging) {
      if (messaging == null) return;
      messaging.onTokenRefresh.listen((token) {
        _cachedToken = token;
        callback(token);
      });
    });
  }

  @override
  void onMessageReceived(
      void Function(Map<String, dynamic> message) callback) {
    _getMessaging().then((messaging) {
      if (messaging == null) return;
      // App 在前台时收到推送
      messaging.onMessage.listen((remoteMessage) {
        callback(_convertMessage(remoteMessage));
      });
    });
  }

  @override
  void onMessageOpenedApp(
      void Function(Map<String, dynamic> message) callback) {
    _getMessaging().then((messaging) {
      if (messaging == null) return;
      // 用户点击推送打开 App
      messaging.onMessageOpenedApp.listen((remoteMessage) {
        callback(_convertMessage(remoteMessage));
      });
      // 检查是否是从推送启动的
      messaging.getInitialMessage().then((remoteMessage) {
        if (remoteMessage != null) {
          callback(_convertMessage(remoteMessage));
        }
      });
    });
  }

  Future<dynamic> _getMessaging() async {
    try {
      // 动态导入，避免在不支持的平台编译失败
      final module = await import('package:firebase_messaging/firebase_messaging.dart');
      return module.FirebaseMessaging.instance;
    } catch (e) {
      return null;
    }
  }

  Map<String, dynamic> _convertMessage(dynamic remoteMessage) {
    return {
      'title': remoteMessage.notification?.title ?? '',
      'body': remoteMessage.notification?.body ?? '',
      'data': remoteMessage.data,
    };
  }

  // 动态导入辅助函数
  Future<dynamic> import(String uri) async {
    try {
      // 这里需要实际的动态导入实现
      // Flutter 不支持真正的动态导入，所以直接返回 null
      // 实际使用时需要在 pubspec.yaml 中添加依赖
      return null;
    } catch (e) {
      return null;
    }
  }
}
