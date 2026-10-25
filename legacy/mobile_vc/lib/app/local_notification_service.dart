import 'dart:convert';

import 'package:flutter/foundation.dart';
import 'package:flutter_local_notifications/flutter_local_notifications.dart';

class NotificationPayload {
  const NotificationPayload({
    required this.title,
    required this.body,
    this.data = const <String, dynamic>{},
  });

  final String title;
  final String body;
  final Map<String, dynamic> data;
}

abstract class LocalNotificationService {
  bool get isAvailable;

  Future<void> initialize();

  void onMessageOpenedApp(void Function(Map<String, dynamic> message) callback);

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
  void Function(Map<String, dynamic> message)? _messageOpenedAppCallback;

  @override
  bool get isAvailable => _available;

  @override
  void onMessageOpenedApp(
      void Function(Map<String, dynamic> message) callback) {
    _messageOpenedAppCallback = callback;
  }

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
      await _plugin.initialize(
        settings,
        onDidReceiveNotificationResponse: _handleNotificationResponse,
        onDidReceiveBackgroundNotificationResponse:
            _handleBackgroundNotificationResponse,
      );
      final androidPlugin = _plugin.resolvePlatformSpecificImplementation<
          AndroidFlutterLocalNotificationsPlugin>();
      await androidPlugin?.createNotificationChannel(_channel);
      final androidGranted =
          await androidPlugin?.requestNotificationsPermission();
      final androidEnabled = await androidPlugin?.areNotificationsEnabled();
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
      _permissionGranted = (androidGranted ?? androidEnabled ?? true) &&
          (iosGranted ?? true) &&
          (macosGranted ?? true);
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
      iOS: const DarwinNotificationDetails(
        presentAlert: true,
        presentBadge: true,
        presentSound: true,
      ),
      macOS: const DarwinNotificationDetails(
        presentAlert: true,
        presentBadge: true,
        presentSound: true,
      ),
    );
    try {
      await _plugin.show(
        1001,
        payload.title,
        payload.body,
        details,
        payload: payload.data.isEmpty ? null : jsonEncode(payload.data),
      );
    } catch (error, stack) {
      debugPrint('[startup] notification show failed: $error');
      debugPrintStack(
        stackTrace: stack,
        label: '[startup] notification show stack',
      );
    }
  }

  void _handleNotificationResponse(NotificationResponse response) {
    final payload = response.payload;
    if (payload == null || payload.trim().isEmpty) {
      _messageOpenedAppCallback?.call(const <String, dynamic>{});
      return;
    }
    try {
      final decoded = jsonDecode(payload);
      if (decoded is Map) {
        _messageOpenedAppCallback?.call(
          Map<String, dynamic>.from(decoded.cast<dynamic, dynamic>()),
        );
        return;
      }
    } catch (_) {}
    _messageOpenedAppCallback?.call(<String, dynamic>{'payload': payload});
  }
}

@pragma('vm:entry-point')
void _handleBackgroundNotificationResponse(NotificationResponse response) {}
