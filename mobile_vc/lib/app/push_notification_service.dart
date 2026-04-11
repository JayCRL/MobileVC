import 'package:flutter/foundation.dart';
import 'push_notification_service_stub.dart'
    if (dart.library.io) 'push_notification_service_mobile.dart'
    if (dart.library.html) 'push_notification_service_web.dart';

export 'push_notification_service_stub.dart'
    if (dart.library.io) 'push_notification_service_mobile.dart'
    if (dart.library.html) 'push_notification_service_web.dart';

/// 推送通知服务抽象接口
abstract class PushNotificationService {
  Future<void> initialize();
  Future<String?> getDeviceToken();
  void onTokenRefresh(void Function(String token) callback);
  void onMessageReceived(void Function(Map<String, dynamic> message) callback);
  void onMessageOpenedApp(
      void Function(Map<String, dynamic> message) callback);
  void onRegistrationError(void Function(String message) callback);
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
  void onRegistrationError(void Function(String message) callback) {}

  @override
  bool get isAvailable => false;
}
