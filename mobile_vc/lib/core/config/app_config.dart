import 'dart:convert';

class AppConfig {
  const AppConfig({
    this.host = 'localhost',
    this.port = '19080',
    this.token = 'test',
    this.cwd = '.',
    this.engine = 'claude',
    this.permissionMode = 'default',
    this.fastMode = false,
    this.adbIceServersJson = '',
  });

  final String host;
  final String port;
  final String token;
  final String cwd;
  final String engine;
  final String permissionMode;
  final bool fastMode;
  final String adbIceServersJson;

  String get baseHttpUrl => 'http://$host:$port';
  String get wsUrl => 'ws://$host:$port/ws?token=$token';

  List<Map<String, dynamic>> get adbIceServers {
    final raw = adbIceServersJson.trim();
    if (raw.isEmpty) {
      return const <Map<String, dynamic>>[];
    }
    try {
      final decoded = jsonDecode(raw);
      if (decoded is! List) {
        return const <Map<String, dynamic>>[];
      }
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
    } catch (_) {
      return const <Map<String, dynamic>>[];
    }
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
    String? permissionMode,
    bool? fastMode,
    String? adbIceServersJson,
  }) {
    return AppConfig(
      host: host ?? this.host,
      port: port ?? this.port,
      token: token ?? this.token,
      cwd: cwd ?? this.cwd,
      engine: engine ?? this.engine,
      permissionMode: permissionMode ?? this.permissionMode,
      fastMode: fastMode ?? this.fastMode,
      adbIceServersJson: adbIceServersJson ?? this.adbIceServersJson,
    );
  }

  Map<String, Object> toJson() => {
        'host': host,
        'port': port,
        'token': token,
        'cwd': cwd,
        'engine': engine,
        'permissionMode': permissionMode,
        'fastMode': fastMode,
        'adbIceServersJson': adbIceServersJson,
      };

  factory AppConfig.fromJson(Map<String, Object?> json) {
    return AppConfig(
      host: (json['host'] ?? 'localhost').toString(),
      port: (json['port'] ?? '19080').toString(),
      token: (json['token'] ?? 'test').toString(),
      cwd: (json['cwd'] ?? '.').toString(),
      engine: (json['engine'] ?? 'claude').toString(),
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
    return fallback.copyWith(
      host: uri.host.trim(),
      port: port,
      token: token,
      adbIceServersJson: ice,
    );
  }
}
