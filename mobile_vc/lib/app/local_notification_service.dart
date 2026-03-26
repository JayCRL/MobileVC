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

  @override
  Future<void> initialize() async {
    if (_initialized) {
      return;
    }
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
  }

  @override
  Future<void> showActionNeededNotification(NotificationPayload payload) async {
    await initialize();
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
    await _plugin.show(
      1001,
      payload.title,
      payload.body,
      details,
    );
  }
}
