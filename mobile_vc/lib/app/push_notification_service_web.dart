import 'package:flutter/foundation.dart';
import 'push_notification_service.dart';

/// Web 平台推送服务（空实现）
class FirebasePushNotificationService implements PushNotificationService {
  @override
  bool get isAvailable => false;

  @override
  Future<void> initialize() async {
    debugPrint('[push] Web platform - push notifications not supported');
  }

  @override
  Future<String?> getDeviceToken() async => null;

  @override
  void onTokenRefresh(void Function(String token) callback) {}

  @override
  void onMessageReceived(
      void Function(Map<String, dynamic> message) callback) {}

  @override
  void onMessageOpenedApp(
      void Function(Map<String, dynamic> message) callback) {}
}

/// 工厂方法：创建 Web 端推送服务（空实现）
PushNotificationService createPushNotificationService() {
  return FirebasePushNotificationService();
}
