import 'package:flutter/foundation.dart';
import 'package:flutter_local_notifications/flutter_local_notifications.dart';

class NotificationPayload {
  const NotificationPayload({
    required this.title,
    required this.body,
  });

  final String title;
  final String body;
}

abstract class LocalNotificationService {
  bool get isAvailable;

  Future<void> initialize();

  Future<void> showActionNeededNotification(NotificationPayload payload);
}

class FlutterLocalNotificationService implements LocalNotificationService {
  FlutterLocalNotificationService({
    FlutterLocalNotificationsPlugin? plugin,
  }) : _plugin = plugin ?? FlutterLocalNotificationsPlugin();

  static const AndroidNotificationChannel _channel = AndroidNotificationChannel(
    'mobilevc_action_needed',
    'MobileVC 后台提醒',
    description: 'Claude 进入等待用户下一步操作时发送提醒',
    importance: Importance.high,
  );

  static const AndroidInitializationSettings _androidInitializationSettings =
      AndroidInitializationSettings('@mipmap/ic_launcher');

  static const DarwinInitializationSettings _darwinInitializationSettings =
      DarwinInitializationSettings(
    requestAlertPermission: false,
    requestBadgePermission: false,
    requestSoundPermission: false,
  );

  final FlutterLocalNotificationsPlugin _plugin;
  bool _initialized = false;
  bool _available = true;

  @override
  bool get isAvailable => _available;

  @override
  Future<void> initialize() async {
    if (_initialized || !_available) {
      return;
    }
    debugPrint('[startup] notification init start');
    try {
      const settings = InitializationSettings(
        android: _androidInitializationSettings,
        iOS: _darwinInitializationSettings,
        macOS: _darwinInitializationSettings,
      );
      await _plugin.initialize(settings);
      final androidPlugin = _plugin
          .resolvePlatformSpecificImplementation<
              AndroidFlutterLocalNotificationsPlugin>();
      await androidPlugin?.createNotificationChannel(_channel);
      _initialized = true;
      debugPrint('[startup] notification init end');
    } catch (error, stack) {
      _available = false;
      debugPrint('[startup] notification init failed: $error');
      debugPrintStack(
        stackTrace: stack,
        label: '[startup] notification init stack',
      );
    }
  }

  @override
  Future<void> showActionNeededNotification(NotificationPayload payload) async {
    await initialize();
    if (!_initialized || !_available) {
      debugPrint('[startup] notification skipped: service unavailable');
      return;
    }
    final details = NotificationDetails(
      android: AndroidNotificationDetails(
        _channel.id,
        _channel.name,
        channelDescription: _channel.description,
        importance: Importance.high,
        priority: Priority.high,
      ),
      iOS: const DarwinNotificationDetails(),
      macOS: const DarwinNotificationDetails(),
    );
    try {
      await _plugin.show(
        1001,
        payload.title,
        payload.body,
        details,
      );
    } catch (error, stack) {
      debugPrint('[startup] notification show failed: $error');
      debugPrintStack(
        stackTrace: stack,
        label: '[startup] notification show stack',
      );
    }
  }
}
