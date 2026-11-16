import 'dart:convert';

import 'app_connection_endpoint.dart';

const _adbIcePort = '3478';

class AdbIceConfig {
  const AdbIceConfig({required this.host, required this.rawJson});

  static const String port = _adbIcePort;

  final String host;
  final String rawJson;

  AdbIceSettings get settings => _parseSettings(rawJson);

  bool get hasTurnServer => servers.any((server) {
        final urls = server['urls'];
        if (urls is! List) {
          return false;
        }
        return urls.any((entry) => _isTurnUrl(entry));
      });

  bool get shouldForceRelay => hasTurnServer && _isLikelyPublicHost(host);

  List<Map<String, dynamic>> get servers {
    final raw = rawJson.trim();
    if (raw.isEmpty) {
      return const <Map<String, dynamic>>[];
    }
    try {
      final decoded = jsonDecode(raw);
      if (decoded is Map) {
        final parsed = _parseAutoSettings(decoded);
        return parsed == null ? const [] : _buildAutoServers(parsed);
      }
      if (decoded is List) {
        return _parseLegacyServers(decoded);
      }
    } catch (_) {
      return const <Map<String, dynamic>>[];
    }
    return const <Map<String, dynamic>>[];
  }

  static String encodeAuto({
    String host = '',
    required String username,
    required String credential,
  }) {
    final trimmedHost = _normalizeHost(host);
    final trimmedUsername = username.trim();
    final trimmedCredential = credential.trim();
    if (trimmedHost.isEmpty &&
        trimmedUsername.isEmpty &&
        trimmedCredential.isEmpty) {
      return '';
    }
    return jsonEncode(<String, String>{
      if (trimmedHost.isNotEmpty) 'host': trimmedHost,
      'username': trimmedUsername,
      'credential': trimmedCredential,
    });
  }

  AdbIceSettings _parseSettings(String rawJson) {
    final raw = rawJson.trim();
    if (raw.isEmpty) {
      return const AdbIceSettings();
    }
    try {
      final decoded = jsonDecode(raw);
      if (decoded is Map) {
        return _parseAutoSettings(decoded) ?? const AdbIceSettings();
      }
      return decoded is List
          ? _parseLegacySettings(decoded)
          : const AdbIceSettings();
    } catch (_) {
      return const AdbIceSettings();
    }
  }

  AdbIceSettings _parseLegacySettings(List decoded) {
    for (final server in decoded) {
      if (server is! Map) {
        continue;
      }
      final username = (server['username'] ?? '').toString().trim();
      final credential = (server['credential'] ?? '').toString().trim();
      if (username.isEmpty && credential.isEmpty) {
        continue;
      }
      return AdbIceSettings(username: username, credential: credential);
    }
    return const AdbIceSettings();
  }

  List<Map<String, dynamic>> _buildAutoServers(AdbIceSettings settings) {
    final normalizedHost = _formatHostLiteral(
      settings.host.isEmpty ? host : settings.host,
    );
    if (normalizedHost.isEmpty) {
      return const <Map<String, dynamic>>[];
    }
    final servers = <Map<String, dynamic>>[
      <String, dynamic>{
        'urls': <String>['stun:$normalizedHost:$_adbIcePort'],
      },
    ];
    if (settings.username.isEmpty || settings.credential.isEmpty) {
      return servers;
    }
    servers.add(<String, dynamic>{
      'urls': <String>[
        'turn:$normalizedHost:$_adbIcePort?transport=udp',
        'turn:$normalizedHost:$_adbIcePort?transport=tcp',
      ],
      'username': settings.username,
      'credential': settings.credential,
    });
    return servers;
  }
}

class AdbIceSettings {
  const AdbIceSettings({
    this.username = '',
    this.credential = '',
    this.host = '',
    this.isAuto = false,
  });

  final String username;
  final String credential;
  final String host;
  final bool isAuto;
}

List<Map<String, dynamic>> _parseLegacyServers(List decoded) {
  final servers = <Map<String, dynamic>>[];
  for (final item in decoded) {
    if (item is! Map) {
      continue;
    }
    final urls = _parseLegacyUrls(item['urls'] ?? item['url']);
    if (urls.isEmpty) {
      continue;
    }
    servers.add(<String, dynamic>{
      'urls': urls,
      if ((item['username'] ?? '').toString().trim().isNotEmpty)
        'username': item['username'].toString().trim(),
      if ((item['credential'] ?? '').toString().trim().isNotEmpty)
        'credential': item['credential'].toString().trim(),
    });
  }
  return servers;
}

List<String> _parseLegacyUrls(Object? rawUrls) {
  return switch (rawUrls) {
    String value when value.trim().isNotEmpty => <String>[value.trim()],
    List value => value
        .whereType<Object>()
        .map((entry) => entry.toString().trim())
        .where((entry) => entry.isNotEmpty)
        .toList(),
    _ => const <String>[],
  };
}

AdbIceSettings? _parseAutoSettings(Map decoded) {
  final username = (decoded['username'] ?? '').toString().trim();
  final credential = (decoded['credential'] ?? '').toString().trim();
  final host = (decoded['host'] ?? '').toString().trim();
  if (username.isEmpty && credential.isEmpty && host.isEmpty) {
    return null;
  }
  return AdbIceSettings(
    username: username,
    credential: credential,
    host: _normalizeHost(host),
    isAuto: true,
  );
}

String _formatHostLiteral(String rawHost) {
  final trimmed = _normalizeHost(rawHost);
  if (trimmed.isEmpty) {
    return '';
  }
  if (trimmed.startsWith('[') && trimmed.endsWith(']')) {
    return trimmed;
  }
  return trimmed.contains(':') ? '[$trimmed]' : trimmed;
}

String _normalizeHost(String rawHost) {
  final trimmed = rawHost.trim();
  if (trimmed.isEmpty) {
    return '';
  }
  return AppConnectionEndpoint.parse(trimmed).host;
}

bool _isTurnUrl(Object entry) {
  final normalized = entry.toString().trim().toLowerCase();
  return normalized.startsWith('turn:') || normalized.startsWith('turns:');
}

bool _isLikelyPublicHost(String rawHost) {
  final trimmed = rawHost.trim().toLowerCase();
  if (trimmed.isEmpty || _isKnownPrivateHost(trimmed)) {
    return false;
  }
  final ipv6 = _unbracketIpv6(trimmed);
  if (ipv6.contains(':')) {
    return _isLikelyPublicIpv6(ipv6);
  }
  final octets = _parseIpv4Octets(trimmed);
  return octets == null ? true : !_isPrivateIpv4(octets);
}

bool _isKnownPrivateHost(String value) {
  return value == 'localhost' ||
      value == '127.0.0.1' ||
      value == '::1' ||
      value == '[::1]' ||
      value.endsWith('.local');
}

String _unbracketIpv6(String value) {
  return value.startsWith('[') && value.endsWith(']')
      ? value.substring(1, value.length - 1)
      : value;
}

bool _isLikelyPublicIpv6(String value) {
  return !(value == '::1' ||
      value.startsWith('fe80:') ||
      value.startsWith('fc') ||
      value.startsWith('fd'));
}

List<int>? _parseIpv4Octets(String value) {
  final match = RegExp(
    r'^(\d{1,3})\.(\d{1,3})\.(\d{1,3})\.(\d{1,3})$',
  ).firstMatch(value);
  if (match == null) {
    return null;
  }
  final octets = List<int>.generate(4, (index) {
    return int.tryParse(match.group(index + 1) ?? '') ?? -1;
  });
  return octets.any((value) => value < 0 || value > 255) ? null : octets;
}

bool _isPrivateIpv4(List<int> octets) {
  final first = octets[0];
  final second = octets[1];
  return first == 10 ||
      first == 127 ||
      (first == 169 && second == 254) ||
      (first == 172 && second >= 16 && second <= 31) ||
      (first == 192 && second == 168) ||
      (first == 100 && second >= 64 && second <= 127);
}
