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

  Future<void> showNotification(NotificationPayload payload);
}

class FlutterLocalNotificationService implements LocalNotificationService {
  FlutterLocalNotificationService({
    FlutterLocalNotificationsPlugin? plugin,
  }) : _plugin = plugin ?? FlutterLocalNotificationsPlugin();

  static const AndroidNotificationChannel _channel = AndroidNotificationChannel(
    'mobilevc_action_needed',
    'MobileVC 后台提醒',
    description: 'AI 助手进入等待用户下一步操作时发送提醒',
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
  bool _permissionGranted = true;

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
      final androidPlugin = _plugin.resolvePlatformSpecificImplementation<
          AndroidFlutterLocalNotificationsPlugin>();
      await androidPlugin?.createNotificationChannel(_channel);
      final iosPlugin = _plugin.resolvePlatformSpecificImplementation<
          IOSFlutterLocalNotificationsPlugin>();
      final macosPlugin = _plugin.resolvePlatformSpecificImplementation<
          MacOSFlutterLocalNotificationsPlugin>();
      final iosGranted = await iosPlugin?.requestPermissions(
        alert: true,
        badge: true,
        sound: true,
      );
      final macosGranted = await macosPlugin?.requestPermissions(
        alert: true,
        badge: true,
        sound: true,
      );
      _permissionGranted = (iosGranted ?? true) && (macosGranted ?? true);
      _initialized = true;
      debugPrint(
        '[startup] notification init end permissionGranted=$_permissionGranted',
      );
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
  Future<void> showNotification(NotificationPayload payload) async {
    await initialize();
    if (!_initialized || !_available) {
      debugPrint('[startup] notification skipped: service unavailable');
      return;
    }
    if (!_permissionGranted) {
      debugPrint('[startup] notification skipped: permission denied');
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
