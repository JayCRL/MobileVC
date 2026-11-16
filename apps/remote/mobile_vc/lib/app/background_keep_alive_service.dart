import 'package:flutter/foundation.dart';
import 'package:flutter/services.dart';

class BackgroundKeepAliveService {
  static const MethodChannel _channel = MethodChannel(
    'top.mobilevc.app/background_keep_alive',
  );
  static const Duration _defaultTimeout = Duration(seconds: 90);

  bool _active = false;

  bool get _supportedPlatform =>
      !kIsWeb &&
      (defaultTargetPlatform == TargetPlatform.iOS ||
          defaultTargetPlatform == TargetPlatform.android);

  Future<void> setActive(bool value) async {
    if (!_supportedPlatform || _active == value) {
      return;
    }
    _active = value;
    try {
      if (value) {
        await _channel.invokeMethod<void>('start', {
          'timeoutMs': _defaultTimeout.inMilliseconds,
        });
      } else {
        await _channel.invokeMethod<void>('stop');
      }
    } on MissingPluginException {
      _active = false;
    } on PlatformException {
      _active = false;
    }
  }

  Future<void> dispose() async {
    await setActive(false);
  }
}
