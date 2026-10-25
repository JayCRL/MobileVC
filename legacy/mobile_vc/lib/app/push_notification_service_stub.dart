import 'push_notification_service.dart';

/// Stub implementation for unsupported platforms
PushNotificationService createPushNotificationService() {
  return NoopPushNotificationService();
}
