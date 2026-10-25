import 'dart:async';

import 'package:flutter/services.dart';

class DeepLinkService {
  DeepLinkService({MethodChannel? channel})
      : _channel = channel ?? const MethodChannel(_channelName);

  static const _channelName = 'top.mobilevc.app/deep_link';

  final MethodChannel _channel;

  Future<void> initialize(ValueChanged<String> onLink) async {
    _channel.setMethodCallHandler((call) async {
      if (call.method != 'onLink') {
        return null;
      }
      final link = call.arguments?.toString().trim() ?? '';
      if (link.isNotEmpty) {
        onLink(link);
      }
      return null;
    });
    final initial = await _takeInitialLink();
    if (initial != null) {
      onLink(initial);
    }
  }

  Future<String?> _takeInitialLink() async {
    try {
      final link = await _channel.invokeMethod<String>('takeInitialLink');
      final trimmed = link?.trim() ?? '';
      return trimmed.isEmpty ? null : trimmed;
    } on MissingPluginException {
      return null;
    }
  }
}
