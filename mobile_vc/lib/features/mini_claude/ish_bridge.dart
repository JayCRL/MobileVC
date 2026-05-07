import 'package:flutter/services.dart';

class IshBridge {
  static const _channel = MethodChannel('mobilevc/ish');
  static bool _ready = false;

  static bool get isAvailable => true;

  static Future<void> ensureInitialized() async {
    if (_ready) return;
    try {
      final result = await _channel.invokeMethod('init');
      _ready = result == true;
    } catch (e) {
      _ready = false;
    }
  }

  static bool get ready => _ready;

  static Future<IshResult> exec(String command) async {
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
