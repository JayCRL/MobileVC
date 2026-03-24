class AppConfig {
  const AppConfig({
    this.host = 'localhost',
    this.port = '19080',
    this.token = 'test',
    this.cwd = '.',
    this.engine = 'claude',
    this.permissionMode = 'default',
    this.fastMode = false,
  });

  final String host;
  final String port;
  final String token;
  final String cwd;
  final String engine;
  final String permissionMode;
  final bool fastMode;

  String get baseHttpUrl => 'http://$host:$port';
  String get wsUrl => 'ws://$host:$port/ws?token=$token';

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
  }) {
    return AppConfig(
      host: host ?? this.host,
      port: port ?? this.port,
      token: token ?? this.token,
      cwd: cwd ?? this.cwd,
      engine: engine ?? this.engine,
      permissionMode: permissionMode ?? this.permissionMode,
      fastMode: fastMode ?? this.fastMode,
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
    );
  }
}
