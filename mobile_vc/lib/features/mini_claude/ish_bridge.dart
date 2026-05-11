import 'package:flutter/services.dart';

class IshBridge {
  static const _channel = MethodChannel('mobilevc/ish');
  static bool _ready = false;
  static String? _lastError;

  static bool get isAvailable => true;
  static bool get ready => _ready;
  static String? get lastError => _lastError;

  static Future<void> ensureInitialized() async {
    if (_ready) return;
    _lastError = null;
    try {
      final result = await _channel.invokeMethod('init');
      if (result == true) {
        _ready = true;
      } else {
        _lastError = 'ish_init returned false';
      }
    } on PlatformException catch (e) {
      _lastError = 'iSH init failed: ${e.code} - ${e.message}';
    } catch (e) {
      _lastError = 'iSH init unexpected error: $e';
    }
  }

  static Future<IshResult> exec(String command) async {
    if (!_ready) return const IshResult(exitCode: -1, output: 'iSH not initialized');
    try {
      final result = await _channel.invokeMethod('exec', command);
      if (result is Map) {
        return IshResult(
          exitCode: (result['exitCode'] as int?) ?? -1,
          output: (result['output'] as String?) ?? '',
        );
      }
      return const IshResult(exitCode: -1, output: 'unexpected response');
    } catch (e) {
      return IshResult(exitCode: -1, output: '$e');
    }
  }
}

class IshResult {
  final int exitCode;
  final String output;

  const IshResult({required this.exitCode, required this.output});

  bool get ok => exitCode == 0;
}
