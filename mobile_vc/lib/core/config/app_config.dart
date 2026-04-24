import 'dart:convert';

class AppConfig {
  static const String adbIcePort = '3478';

  const AppConfig({
    this.host = 'localhost',
    this.port = '19080',
    this.token = 'test',
    this.cwd = '.',
    this.engine = 'claude',
    this.model = '',
    this.reasoningEffort = '',
    this.claudeModel = '',
    this.codexModel = '',
    this.codexReasoningEffort = '',
    this.permissionMode = 'default',
    this.fastMode = false,
    this.adbIceServersJson = '',
  });

  final String host;
  final String port;
  final String token;
  final String cwd;
  final String engine;
  final String model;
  final String reasoningEffort;
  final String claudeModel;
  final String codexModel;
  final String codexReasoningEffort;
  final String permissionMode;
  final bool fastMode;
  final String adbIceServersJson;

  String get baseHttpUrl => 'http://$host:$port';
  String get wsUrl => 'ws://$host:$port/ws?token=$token';

  String get adbIceUsername => _parseAdbIceSettings().username;
  String get adbIceCredential => _parseAdbIceSettings().credential;
  String get adbIceHostOverride => _parseAdbIceSettings().host;
  bool get hasAutoAdbIceConfig => _parseAdbIceSettings().isAuto;
  bool get hasTurnAdbIceServer => adbIceServers.any((server) {
        final urls = server['urls'];
        if (urls is! List) {
          return false;
        }
        return urls.any((entry) {
          final normalized = entry.toString().trim().toLowerCase();
          return normalized.startsWith('turn:') ||
              normalized.startsWith('turns:');
        });
      });
  bool get shouldForceAdbRelay =>
      hasTurnAdbIceServer && _isLikelyPublicHost(host);

  List<Map<String, dynamic>> get adbIceServers {
    final raw = adbIceServersJson.trim();
    if (raw.isEmpty) {
      return const <Map<String, dynamic>>[];
    }
    try {
      final decoded = jsonDecode(raw);
      if (decoded is Map) {
        final settings = _parseAutoAdbIceSettings(decoded);
        if (settings == null) {
          return const <Map<String, dynamic>>[];
        }
        return _buildAutoAdbIceServers(
          hostOverride: settings.host,
          username: settings.username,
          credential: settings.credential,
        );
      }
      if (decoded is! List) {
        return const <Map<String, dynamic>>[];
      }
      return _parseLegacyAdbIceServers(decoded);
    } catch (_) {
      return const <Map<String, dynamic>>[];
    }
  }

  static String encodeAutoAdbIceConfig({
    String host = '',
    required String username,
    required String credential,
  }) {
    final trimmedHost = host.trim();
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

  Uri downloadUri(String path) {
    return Uri.parse(baseHttpUrl).replace(
      path: '/download',
      queryParameters: {
        'token': token,
        'path': path,
      },
    );
  }

  AppConfig copyWith({
    String? host,
    String? port,
    String? token,
    String? cwd,
    String? engine,
    String? model,
    String? reasoningEffort,
    String? claudeModel,
    String? codexModel,
    String? codexReasoningEffort,
    String? permissionMode,
    bool? fastMode,
    String? adbIceServersJson,
  }) {
    final nextEngine = engine ?? this.engine;
    var nextClaudeModel = claudeModel ?? this.claudeModel;
    var nextCodexModel = codexModel ?? this.codexModel;
    var nextCodexReasoningEffort =
        codexReasoningEffort ?? this.codexReasoningEffort;
    if (model != null) {
      if (nextEngine.trim().toLowerCase() == 'codex') {
        nextCodexModel = model;
      } else {
        nextClaudeModel = model;
      }
    }
    if (reasoningEffort != null && nextEngine.trim().toLowerCase() == 'codex') {
      nextCodexReasoningEffort = reasoningEffort;
    }
    return AppConfig(
      host: host ?? this.host,
      port: port ?? this.port,
      token: token ?? this.token,
      cwd: cwd ?? this.cwd,
      engine: nextEngine,
      model: model ?? this.model,
      reasoningEffort: reasoningEffort ?? this.reasoningEffort,
      claudeModel: nextClaudeModel,
      codexModel: nextCodexModel,
      codexReasoningEffort: nextCodexReasoningEffort,
      permissionMode: permissionMode ?? this.permissionMode,
      fastMode: fastMode ?? this.fastMode,
      adbIceServersJson: adbIceServersJson ?? this.adbIceServersJson,
    );
  }

  String modelForEngine(String targetEngine) {
    switch (targetEngine.trim().toLowerCase()) {
      case 'codex':
        if (codexModel.trim().isNotEmpty) {
          return codexModel.trim();
        }
        if (engine.trim().toLowerCase() == 'codex') {
          return model.trim();
        }
        return '';
      case 'claude':
        if (claudeModel.trim().isNotEmpty) {
          return claudeModel.trim();
        }
        if (engine.trim().toLowerCase() == 'claude') {
          return model.trim();
        }
        return '';
      default:
        return model.trim();
    }
  }

  String reasoningEffortForEngine(String targetEngine) {
    if (targetEngine.trim().toLowerCase() != 'codex') {
      return '';
    }
    if (codexReasoningEffort.trim().isNotEmpty) {
      return codexReasoningEffort.trim();
    }
    if (engine.trim().toLowerCase() == 'codex') {
      return reasoningEffort.trim();
    }
    return '';
  }

  Map<String, Object> toJson() => {
        'host': host,
        'port': port,
        'token': token,
        'cwd': cwd,
        'engine': engine,
        'model': model,
        'reasoningEffort': reasoningEffort,
        'claudeModel': claudeModel,
        'codexModel': codexModel,
        'codexReasoningEffort': codexReasoningEffort,
        'permissionMode': permissionMode,
        'fastMode': fastMode,
        'adbIceServersJson': adbIceServersJson,
      };

  factory AppConfig.fromJson(Map<String, Object?> json) {
    final engine = (json['engine'] ?? 'claude').toString();
    final legacyModel = (json['model'] ?? '').toString();
    final legacyReasoningEffort = (json['reasoningEffort'] ?? '').toString();
    return AppConfig(
      host: (json['host'] ?? 'localhost').toString(),
      port: (json['port'] ?? '19080').toString(),
      token: (json['token'] ?? 'test').toString(),
      cwd: (json['cwd'] ?? '.').toString(),
      engine: engine,
      model: legacyModel,
      reasoningEffort: legacyReasoningEffort,
      claudeModel: (json['claudeModel'] ??
              (engine.trim().toLowerCase() == 'claude' ? legacyModel : ''))
          .toString(),
      codexModel: (json['codexModel'] ??
              (engine.trim().toLowerCase() == 'codex' ? legacyModel : ''))
          .toString(),
      codexReasoningEffort: (json['codexReasoningEffort'] ??
              (engine.trim().toLowerCase() == 'codex'
                  ? legacyReasoningEffort
                  : ''))
          .toString(),
      permissionMode: (json['permissionMode'] ?? 'default').toString(),
      fastMode: json['fastMode'] == true,
      adbIceServersJson: (json['adbIceServersJson'] ?? '').toString(),
    );
  }

  static AppConfig? fromLaunchUri(
    String raw, {
    AppConfig fallback = const AppConfig(),
  }) {
    final trimmed = raw.trim();
    if (trimmed.isEmpty) {
      return null;
    }
    final uri = Uri.tryParse(trimmed);
    if (uri == null || uri.host.trim().isEmpty) {
      return null;
    }
    final port =
        uri.hasPort && uri.port > 0 ? uri.port.toString() : fallback.port;
    final token = (uri.queryParameters['token'] ?? fallback.token).trim();
    final ice =
        (uri.queryParameters['ice'] ?? fallback.adbIceServersJson).trim();
    final cwd = (uri.queryParameters['cwd'] ?? fallback.cwd).trim();
    return fallback.copyWith(
      host: uri.host.trim(),
      port: port,
      token: token,
      cwd: cwd,
      adbIceServersJson: ice,
    );
  }

  _AdbIceSettings _parseAdbIceSettings() {
    final raw = adbIceServersJson.trim();
    if (raw.isEmpty) {
      return const _AdbIceSettings();
    }
    try {
      final decoded = jsonDecode(raw);
      if (decoded is Map) {
        return _parseAutoAdbIceSettings(decoded) ?? const _AdbIceSettings();
      }
      if (decoded is List) {
        for (final server in decoded) {
          if (server is! Map) {
            continue;
          }
          final username = (server['username'] ?? '').toString().trim();
          final credential = (server['credential'] ?? '').toString().trim();
          if (username.isEmpty && credential.isEmpty) {
            continue;
          }
          return _AdbIceSettings(
            username: username,
            credential: credential,
          );
        }
      }
    } catch (_) {
      return const _AdbIceSettings();
    }
    return const _AdbIceSettings();
  }

  List<Map<String, dynamic>> _buildAutoAdbIceServers({
    String hostOverride = '',
    required String username,
    required String credential,
  }) {
    final normalizedHost = _formatIceHostLiteral(
        hostOverride.trim().isEmpty ? host : hostOverride);
    if (normalizedHost.isEmpty) {
      return const <Map<String, dynamic>>[];
    }
    final servers = <Map<String, dynamic>>[
      <String, dynamic>{
        'urls': <String>['stun:$normalizedHost:$adbIcePort'],
      },
    ];
    if (username.trim().isEmpty || credential.trim().isEmpty) {
      return servers;
    }
    servers.add(<String, dynamic>{
      'urls': <String>[
        'turn:$normalizedHost:$adbIcePort?transport=udp',
        'turn:$normalizedHost:$adbIcePort?transport=tcp',
      ],
      'username': username.trim(),
      'credential': credential.trim(),
    });
    return servers;
  }

  static List<Map<String, dynamic>> _parseLegacyAdbIceServers(List decoded) {
    final servers = <Map<String, dynamic>>[];
    for (final item in decoded) {
      if (item is! Map) {
        continue;
      }
      final rawUrls = item['urls'] ?? item['url'];
      final urls = switch (rawUrls) {
        String value when value.trim().isNotEmpty => <String>[value.trim()],
        List value => value
            .whereType<Object>()
            .map((entry) => entry.toString().trim())
            .where((entry) => entry.isNotEmpty)
            .toList(),
        _ => const <String>[],
      };
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

  static _AdbIceSettings? _parseAutoAdbIceSettings(Map decoded) {
    final username = (decoded['username'] ?? '').toString().trim();
    final credential = (decoded['credential'] ?? '').toString().trim();
    final host = (decoded['host'] ?? '').toString().trim();
    if (username.isEmpty && credential.isEmpty && host.isEmpty) {
      return null;
    }
    return _AdbIceSettings(
      username: username,
      credential: credential,
      host: host,
      isAuto: true,
    );
  }

  static String _formatIceHostLiteral(String rawHost) {
    final trimmed = rawHost.trim();
    if (trimmed.isEmpty) {
      return '';
    }
    if (trimmed.startsWith('[') && trimmed.endsWith(']')) {
      return trimmed;
    }
    if (trimmed.contains(':')) {
      return '[$trimmed]';
    }
    return trimmed;
  }

  static bool _isLikelyPublicHost(String rawHost) {
    final trimmed = rawHost.trim().toLowerCase();
    if (trimmed.isEmpty) {
      return false;
    }
    if (trimmed == 'localhost' ||
        trimmed == '127.0.0.1' ||
        trimmed == '::1' ||
        trimmed == '[::1]' ||
        trimmed.endsWith('.local')) {
      return false;
    }

    final ipv6 = trimmed.startsWith('[') && trimmed.endsWith(']')
        ? trimmed.substring(1, trimmed.length - 1)
        : trimmed;
    if (ipv6.contains(':')) {
      return !(ipv6 == '::1' ||
          ipv6.startsWith('fe80:') ||
          ipv6.startsWith('fc') ||
          ipv6.startsWith('fd'));
    }

    final ipv4Match =
        RegExp(r'^(\d{1,3})\.(\d{1,3})\.(\d{1,3})\.(\d{1,3})$').firstMatch(
      trimmed,
    );
    if (ipv4Match == null) {
      return true;
    }
    final octets = List<int>.generate(4, (index) {
      return int.tryParse(ipv4Match.group(index + 1) ?? '') ?? -1;
    });
    if (octets.any((value) => value < 0 || value > 255)) {
      return false;
    }
    final first = octets[0];
    final second = octets[1];
    if (first == 10 ||
        first == 127 ||
        (first == 169 && second == 254) ||
        (first == 172 && second >= 16 && second <= 31) ||
        (first == 192 && second == 168) ||
        (first == 100 && second >= 64 && second <= 127)) {
      return false;
    }
    return true;
  }
}

class _AdbIceSettings {
  const _AdbIceSettings({
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
