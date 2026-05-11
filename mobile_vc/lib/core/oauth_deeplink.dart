import 'dart:async';
import 'package:flutter/services.dart';

class OAuthDeepLink {
  static const _channel = MethodChannel('mobilevc/deeplink');
  static String? _lastHandled;

  static Stream<String> get onUri => EventChannel('mobilevc/deeplink_uri')
      .receiveBroadcastStream()
      .where((e) => e is String && (e as String).isNotEmpty)
      .cast<String>();

  static Future<String?> getInitialUri() async {
    try {
      final result = await _channel.invokeMethod<String>('getInitialUri');
      if (result != null && result.isNotEmpty && result != _lastHandled) {
        _lastHandled = result;
        return result;
      }
      return null;
    } catch (_) {
      return null;
    }
  }
}
