import 'dart:async';
import 'dart:io';

import 'package:flutter/foundation.dart';
import 'package:flutter/services.dart';

import 'push_notification_service.dart';

class APNsPushNotificationService implements PushNotificationService {
  static const MethodChannel _channel = MethodChannel('top.mobilevc.app/push');

  bool _initialized = false;
  String? _cachedToken;
  void Function(String token)? _tokenRefreshCallback;
  void Function(Map<String, dynamic> message)? _messageReceivedCallback;
  void Function(Map<String, dynamic> message)? _messageOpenedAppCallback;
  void Function(String message)? _registrationErrorCallback;

  @override
  bool get isAvailable => Platform.isIOS;

  @override
  Future<void> initialize() async {
    if (_initialized) return;
    _initialized = true;
    _channel.setMethodCallHandler(_handleMethodCall);
    await _channel.invokeMethod<void>('requestPermissionAndRegister');
    _cachedToken = await getDeviceToken();
    debugPrint('[push] APNs initialized token=$_cachedToken');
  }

  @override
  Future<String?> getDeviceToken() async {
    if (!_initialized) {
      await initialize();
    }
    final token = await _channel.invokeMethod<String>('getDeviceToken');
    if (token != null && token.trim().isNotEmpty) {
      _cachedToken = token.trim();
    }
    return _cachedToken;
  }

  @override
  void onTokenRefresh(void Function(String token) callback) {
    _tokenRefreshCallback = callback;
  }

  @override
  void onMessageReceived(void Function(Map<String, dynamic> message) callback) {
    _messageReceivedCallback = callback;
  }

  @override
  void onMessageOpenedApp(
    void Function(Map<String, dynamic> message) callback,
  ) {
    _messageOpenedAppCallback = callback;
  }

  @override
  void onRegistrationError(void Function(String message) callback) {
    _registrationErrorCallback = callback;
  }

  Future<void> _handleMethodCall(MethodCall call) async {
    switch (call.method) {
      case 'onToken':
        final token = (call.arguments as String?)?.trim() ?? '';
        if (token.isEmpty) return;
        _cachedToken = token;
        _tokenRefreshCallback?.call(token);
        return;
      case 'onRegistrationError':
        final message = (call.arguments as String?)?.trim() ?? 'APNs registration failed';
        _registrationErrorCallback?.call(message);
        return;
      case 'onMessageReceived':
        final payload = Map<String, dynamic>.from(
          (call.arguments as Map?)?.cast<dynamic, dynamic>() ?? const {},
        );
        _messageReceivedCallback?.call(payload);
        return;
      case 'onMessageOpenedApp':
        final payload = Map<String, dynamic>.from(
          (call.arguments as Map?)?.cast<dynamic, dynamic>() ?? const {},
        );
        _messageOpenedAppCallback?.call(payload);
        return;
      default:
        return;
    }
  }
}

PushNotificationService createPushNotificationService() {
  return APNsPushNotificationService();
}
