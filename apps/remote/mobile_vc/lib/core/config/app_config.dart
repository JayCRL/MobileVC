import 'adb_ice_config.dart';
import 'app_config_engine_models.dart';
import 'app_connection_endpoint.dart';
import 'app_connection_environment.dart';
import 'app_connection_urls.dart';
import '../relay_e2ee/relay_e2ee_capability.dart';
import 'relay_config.dart';

const Object _unchanged = Object();
const int defaultHistoryWindowLimit = 120;

class AppConfig {
  static const String adbIcePort = AdbIceConfig.port;

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
    this.codexSandboxMode = 'workspace-write',
    this.codexTargetMode = false,
    this.historyWindowLimit = defaultHistoryWindowLimit,
    this.lastSessionId = '',
    this.lastSessionCwd = '',
    this.permissionMode = 'auto',
    this.fastMode = false,
    this.adbIceServersJson = '',
    this.secureTransport,
    this.connectionMode = 'direct',
    this.relayUrl = '',
    this.relaySessionId = '',
    this.relayPairingSecret = '',
    this.relayPairingExpiresAt = 0,
    this.relayClientId = '',
    this.relayClientReconnectSecret = '',
    this.relayNodeFingerprintHex = '',
    this.relayCapabilities,
    this.voiceApiUrl = '',
    this.voiceApiKey = '',
    this.voiceModelName = '',
    this.voiceTtsUrl = '',
    this.voiceTtsApiKey = '',
    this.voiceTtsModelName = '',
    this.voiceTtsVoice = 'alloy',
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
  final String codexSandboxMode;
  final bool codexTargetMode;
  final int historyWindowLimit;
  final String lastSessionId;
  final String lastSessionCwd;
  final String permissionMode;
  final bool fastMode;
  final String adbIceServersJson;
  final bool? secureTransport;
  final String connectionMode;
  final String relayUrl;
  final String relaySessionId;
  final String relayPairingSecret;
  final int relayPairingExpiresAt;
  final String relayClientId;
  final String relayClientReconnectSecret;
  final String relayNodeFingerprintHex;
  final RelayE2eeCapabilitySet? relayCapabilities;
  final String voiceApiUrl;
  final String voiceApiKey;
  final String voiceModelName;
  final String voiceTtsUrl;
  final String voiceTtsApiKey;
  final String voiceTtsModelName;
  final String voiceTtsVoice;

  bool get isRelayMode => connectionMode == ConnectionMode.relay.name;

  bool get isAutoMode => connectionMode == ConnectionMode.auto.name;

  bool get canUseRelay =>
      relayUrl.trim().isNotEmpty &&
      relaySessionId.trim().isNotEmpty &&
      (relayPairingSecret.trim().isNotEmpty ||
          (relayClientId.trim().isNotEmpty &&
              relayClientReconnectSecret.trim().isNotEmpty));

  bool get hasDirectEndpoint =>
      host.trim().isNotEmpty &&
      port.trim().isNotEmpty &&
      token.trim().isNotEmpty;

  bool get hasVoiceCallConfig =>
      voiceApiUrl.trim().isNotEmpty && voiceModelName.trim().isNotEmpty;

  bool get hasVoiceTtsConfig =>
      voiceTtsUrl.trim().isNotEmpty && voiceTtsModelName.trim().isNotEmpty;

  String get baseHttpUrl => baseHttpUrlFor();

  String get wsUrl => wsUrlFor();

  String get displayEndpoint => _connectionUrls(null).displayEndpoint;

  String get displayHost => _connectionUrls(null).displayHost;

  String baseHttpUrlFor({bool? secureTransport}) =>
      _connectionUrls(secureTransport).baseHttpUrl;

  String wsUrlFor({bool? secureTransport}) =>
      _connectionUrls(secureTransport).wsUrl;

  String get adbIceUsername => _adbIceConfig.settings.username;

  String get adbIceCredential => _adbIceConfig.settings.credential;

  String get adbIceHostOverride => _adbIceConfig.settings.host;

  bool get hasAutoAdbIceConfig => _adbIceConfig.settings.isAuto;

  bool get hasTurnAdbIceServer => _adbIceConfig.hasTurnServer;

  bool get shouldForceAdbRelay => _adbIceConfig.shouldForceRelay;

  List<Map<String, dynamic>> get adbIceServers => _adbIceConfig.servers;

  static String encodeAutoAdbIceConfig({
    String host = '',
    required String username,
    required String credential,
  }) {
    return AdbIceConfig.encodeAuto(
      host: host,
      username: username,
      credential: credential,
    );
  }

  Uri downloadUri(String path, {bool? secureTransport}) =>
      _connectionUrls(secureTransport).downloadUri(path);

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
    String? codexSandboxMode,
    bool? codexTargetMode,
    int? historyWindowLimit,
    String? lastSessionId,
    String? lastSessionCwd,
    String? permissionMode,
    bool? fastMode,
    String? adbIceServersJson,
    Object? secureTransport = _unchanged,
    String? connectionMode,
    String? relayUrl,
    String? relaySessionId,
    String? relayPairingSecret,
    int? relayPairingExpiresAt,
    String? relayClientId,
    String? relayClientReconnectSecret,
    String? relayNodeFingerprintHex,
    Object? relayCapabilities = _unchanged,
    String? voiceApiUrl,
    String? voiceApiKey,
    String? voiceModelName,
    String? voiceTtsUrl,
    String? voiceTtsApiKey,
    String? voiceTtsModelName,
    String? voiceTtsVoice,
  }) {
    final nextEngine = engine ?? this.engine;
    final nextModels = AppConfigEngineModels.resolve(
      nextEngine: nextEngine,
      model: model,
      reasoningEffort: reasoningEffort,
      currentClaudeModel: this.claudeModel,
      currentCodexModel: this.codexModel,
      currentCodexReasoningEffort: this.codexReasoningEffort,
      claudeModel: claudeModel,
      codexModel: codexModel,
      codexReasoningEffort: codexReasoningEffort,
    );
    final endpoint = AppConnectionEndpoint.parse(
      host ?? this.host,
      fallbackPort: port ?? this.port,
    );
    return AppConfig(
      host: endpoint.host,
      port: endpoint.port,
      token: token ?? this.token,
      cwd: cwd ?? this.cwd,
      engine: nextEngine,
      model: model ?? this.model,
      reasoningEffort: reasoningEffort ?? this.reasoningEffort,
      claudeModel: nextModels.claudeModel,
      codexModel: nextModels.codexModel,
      codexReasoningEffort: nextModels.codexReasoningEffort,
      codexSandboxMode:
          normalizeCodexSandboxMode(codexSandboxMode ?? this.codexSandboxMode),
      codexTargetMode: codexTargetMode ?? this.codexTargetMode,
      historyWindowLimit: parseHistoryWindowLimit(
          historyWindowLimit ?? this.historyWindowLimit),
      lastSessionId: lastSessionId ?? this.lastSessionId,
      lastSessionCwd: lastSessionCwd ?? this.lastSessionCwd,
      permissionMode: _normalizePermissionMode(
        permissionMode ?? this.permissionMode,
      ),
      fastMode: fastMode ?? this.fastMode,
      adbIceServersJson: adbIceServersJson ?? this.adbIceServersJson,
      secureTransport: identical(secureTransport, _unchanged)
          ? endpoint.secureTransport ?? this.secureTransport
          : secureTransport as bool?,
      connectionMode:
          normalizeConnectionMode(connectionMode ?? this.connectionMode),
      relayUrl: relayUrl ?? this.relayUrl,
      relaySessionId: relaySessionId ?? this.relaySessionId,
      relayPairingSecret: relayPairingSecret ?? this.relayPairingSecret,
      relayPairingExpiresAt:
          relayPairingExpiresAt ?? this.relayPairingExpiresAt,
      relayClientId: relayClientId ?? this.relayClientId,
      relayClientReconnectSecret:
          relayClientReconnectSecret ?? this.relayClientReconnectSecret,
      relayNodeFingerprintHex:
          relayNodeFingerprintHex ?? this.relayNodeFingerprintHex,
      relayCapabilities: identical(relayCapabilities, _unchanged)
          ? this.relayCapabilities
          : relayCapabilities as RelayE2eeCapabilitySet?,
      voiceApiUrl: voiceApiUrl ?? this.voiceApiUrl,
      voiceApiKey: voiceApiKey ?? this.voiceApiKey,
      voiceModelName: voiceModelName ?? this.voiceModelName,
      voiceTtsUrl: voiceTtsUrl ?? this.voiceTtsUrl,
      voiceTtsApiKey: voiceTtsApiKey ?? this.voiceTtsApiKey,
      voiceTtsModelName: voiceTtsModelName ?? this.voiceTtsModelName,
      voiceTtsVoice: voiceTtsVoice ?? this.voiceTtsVoice,
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
        'codexSandboxMode': codexSandboxMode,
        'codexTargetMode': codexTargetMode,
        'historyWindowLimit': historyWindowLimit,
        if (lastSessionId.trim().isNotEmpty)
          'lastSessionId': lastSessionId.trim(),
        if (lastSessionCwd.trim().isNotEmpty)
          'lastSessionCwd': lastSessionCwd.trim(),
        'permissionMode': permissionMode,
        'fastMode': fastMode,
        'adbIceServersJson': adbIceServersJson,
        'connectionMode': connectionMode,
        if (relayUrl.trim().isNotEmpty) 'relayUrl': relayUrl,
        if (relaySessionId.trim().isNotEmpty) 'relaySessionId': relaySessionId,
        if (relayClientId.trim().isNotEmpty) 'relayClientId': relayClientId,
        if (relayClientReconnectSecret.trim().isNotEmpty)
          'relayClientReconnectSecret': relayClientReconnectSecret,
        if (relayNodeFingerprintHex.trim().isNotEmpty)
          'relayNodeFingerprintHex': relayNodeFingerprintHex,
        if (relayCapabilities != null)
          'relayCapabilities': relayCapabilities!.toJson(),
        if (secureTransport != null) 'secureTransport': secureTransport!,
        if (voiceApiUrl.trim().isNotEmpty) 'voiceApiUrl': voiceApiUrl,
        if (voiceApiKey.trim().isNotEmpty) 'voiceApiKey': voiceApiKey,
        if (voiceModelName.trim().isNotEmpty) 'voiceModelName': voiceModelName,
        if (voiceTtsUrl.trim().isNotEmpty) 'voiceTtsUrl': voiceTtsUrl,
        if (voiceTtsApiKey.trim().isNotEmpty) 'voiceTtsApiKey': voiceTtsApiKey,
        if (voiceTtsModelName.trim().isNotEmpty)
          'voiceTtsModelName': voiceTtsModelName,
        if (voiceTtsVoice.trim().isNotEmpty) 'voiceTtsVoice': voiceTtsVoice,
      };

  factory AppConfig.fromJson(Map<String, Object?> json) {
    final engine = (json['engine'] ?? 'claude').toString();
    final legacyModel = (json['model'] ?? '').toString();
    final legacyReasoningEffort = (json['reasoningEffort'] ?? '').toString();
    final endpoint = AppConnectionEndpoint.parse(
      (json['host'] ?? 'localhost').toString(),
      fallbackPort: (json['port'] ?? '19080').toString(),
    );
    return AppConfig(
      host: endpoint.host,
      port: endpoint.port,
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
      codexSandboxMode: normalizeCodexSandboxMode(
        (json['codexSandboxMode'] ?? 'workspace-write').toString(),
      ),
      codexTargetMode: json['codexTargetMode'] == true,
      historyWindowLimit: parseHistoryWindowLimit(
        json['historyWindowLimit'],
        defaultWhenMissing: true,
      ),
      lastSessionId: (json['lastSessionId'] ?? '').toString(),
      lastSessionCwd: (json['lastSessionCwd'] ?? '').toString(),
      permissionMode: _normalizePermissionMode(
        (json['permissionMode'] ?? 'auto').toString(),
      ),
      fastMode: json['fastMode'] == true,
      adbIceServersJson: (json['adbIceServersJson'] ?? '').toString(),
      secureTransport: endpoint.secureTransport ??
          parseSecureTransport(json['secureTransport']),
      connectionMode: normalizeConnectionMode(json['connectionMode']),
      relayUrl: (json['relayUrl'] ?? '').toString(),
      relaySessionId: (json['relaySessionId'] ?? '').toString(),
      relayClientId: (json['relayClientId'] ?? '').toString(),
      relayClientReconnectSecret:
          (json['relayClientReconnectSecret'] ?? '').toString(),
      relayNodeFingerprintHex:
          (json['relayNodeFingerprintHex'] ?? '').toString(),
      relayCapabilities: _relayCapabilitiesFromJson(
        json['relayCapabilities'],
        hasRelayNodeFingerprint: (json['relayNodeFingerprintHex'] ?? '')
            .toString()
            .trim()
            .isNotEmpty,
      ),
      voiceApiUrl: (json['voiceApiUrl'] ?? '').toString(),
      voiceApiKey: (json['voiceApiKey'] ?? '').toString(),
      voiceModelName: (json['voiceModelName'] ?? '').toString(),
      voiceTtsUrl: (json['voiceTtsUrl'] ?? '').toString(),
      voiceTtsApiKey: (json['voiceTtsApiKey'] ?? '').toString(),
      voiceTtsModelName: (json['voiceTtsModelName'] ?? '').toString(),
      voiceTtsVoice: (json['voiceTtsVoice'] ?? 'alloy').toString(),
    );
  }

  static String _normalizePermissionMode(String value) {
    return normalizePermissionModeForDisplay(value);
  }

  static String normalizePermissionModeForDisplay(String value) {
    switch (value.trim()) {
      case 'bypassPermissions':
        return 'bypassPermissions';
      case 'config':
        return 'config';
      case 'default':
        return 'default';
      default:
        return 'auto';
    }
  }

  static String normalizeCodexSandboxMode(String value) {
    switch (value.trim()) {
      case 'read-only':
        return 'read-only';
      case 'danger-full-access':
        return 'danger-full-access';
      case 'config':
        return 'config';
      case 'workspace-write':
      default:
        return 'workspace-write';
    }
  }

  static int parseHistoryWindowLimit(
    Object? value, {
    bool defaultWhenMissing = false,
  }) {
    if (value == null) {
      if (defaultWhenMissing) {
        return defaultHistoryWindowLimit;
      }
      throw const FormatException('historyWindowLimit is required');
    }
    final parsed = value is num
        ? _parseNumericHistoryWindowLimit(value)
        : int.tryParse(value.toString().trim());
    if (parsed == null || parsed <= 0) {
      throw const FormatException(
        'historyWindowLimit must be a positive integer',
      );
    }
    return parsed;
  }

  static int? _parseNumericHistoryWindowLimit(num value) {
    if (!value.isFinite || value % 1 != 0) {
      return null;
    }
    return value.toInt();
  }

  static AppConfig? fromLaunchUri(
    String raw, {
    AppConfig fallback = const AppConfig(),
  }) {
    final trimmed = raw.trim();
    if (trimmed.isEmpty) {
      return null;
    }
    final relayPairing = parseRelayPairingUri(trimmed);
    if (relayPairing != null) {
      final sameRelaySession =
          fallback.connectionMode != ConnectionMode.direct.name &&
              fallback.relayUrl.trim() == relayPairing.relayUrl.trim() &&
              fallback.relaySessionId.trim() == relayPairing.sessionId.trim();
      final hasReconnectCredentials =
          fallback.relayClientId.trim().isNotEmpty &&
              fallback.relayClientReconnectSecret.trim().isNotEmpty;
      final canReconnectSameRelaySession =
          sameRelaySession && hasReconnectCredentials;
      if (!canReconnectSameRelaySession && _relayPairingExpired(relayPairing)) {
        throw const FormatException(
          'Relay 配对链接已过期，请在电脑端生成新的 Relay 链接后重新导入',
        );
      }
      return fallback.copyWith(
        connectionMode: relayPairing.hasLanEndpoint
            ? ConnectionMode.auto.name
            : ConnectionMode.relay.name,
        host:
            relayPairing.hasLanEndpoint ? relayPairing.lanHost : fallback.host,
        port:
            relayPairing.hasLanEndpoint ? relayPairing.lanPort : fallback.port,
        token: relayPairing.hasLanEndpoint
            ? relayPairing.lanToken
            : fallback.token,
        cwd: relayPairing.lanCwd.trim().isNotEmpty
            ? relayPairing.lanCwd
            : fallback.cwd,
        secureTransport: relayPairing.lanSecureTransport ??
            (relayPairing.hasLanEndpoint ? fallback.secureTransport : null),
        relayUrl: relayPairing.relayUrl,
        relaySessionId: relayPairing.sessionId,
        relayPairingSecret:
            canReconnectSameRelaySession ? '' : relayPairing.pairingSecret,
        relayPairingExpiresAt:
            canReconnectSameRelaySession ? 0 : relayPairing.expiresAt,
        relayClientId:
            canReconnectSameRelaySession ? fallback.relayClientId : '',
        relayClientReconnectSecret: canReconnectSameRelaySession
            ? fallback.relayClientReconnectSecret
            : '',
        relayNodeFingerprintHex: relayPairing.nodeFingerprintHex,
        relayCapabilities: relayPairing.capabilities,
      );
    }
    final uri = Uri.tryParse(trimmed);
    if (uri == null || uri.host.trim().isEmpty) {
      return null;
    }
    final port = _launchUriPort(uri, fallback.port);
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
      secureTransport: secureTransportFromScheme(uri.scheme),
    );
  }

  static bool _relayPairingExpired(RelayPairing pairing) {
    if (pairing.expiresAt <= 0) {
      return false;
    }
    final nowSeconds = DateTime.now().millisecondsSinceEpoch ~/ 1000;
    return nowSeconds > pairing.expiresAt;
  }

  AppConnectionUrls _connectionUrls(bool? secureTransport) {
    final endpoint = AppConnectionEndpoint.parse(
      host,
      fallbackPort: port,
    );
    final effectiveSecureTransport = secureTransport ??
        (defaultSecureBackendTransport
            ? true
            : endpoint.secureTransport ?? this.secureTransport ?? false);
    return AppConnectionUrls(
      host: endpoint.host,
      port: endpoint.port,
      token: token,
      secureTransport: effectiveSecureTransport,
    );
  }

  AdbIceConfig get _adbIceConfig =>
      AdbIceConfig(host: host, rawJson: adbIceServersJson);
}

RelayE2eeCapabilitySet? _relayCapabilitiesFromJson(
  Object? value, {
  required bool hasRelayNodeFingerprint,
}) {
  if (value == null) {
    return hasRelayNodeFingerprint ? RelayE2eeCapabilitySet.production() : null;
  }
  if (value is! Map) {
    return hasRelayNodeFingerprint ? RelayE2eeCapabilitySet.production() : null;
  }
  try {
    return RelayE2eeCapabilitySet.fromJson(
      Map<String, Object?>.from(value),
    );
  } on FormatException {
    return hasRelayNodeFingerprint ? RelayE2eeCapabilitySet.production() : null;
  } on ArgumentError {
    return hasRelayNodeFingerprint ? RelayE2eeCapabilitySet.production() : null;
  }
}

String _launchUriPort(Uri uri, String fallbackPort) {
  if (uri.hasPort && uri.port > 0) {
    return uri.port.toString();
  }
  return secureTransportFromScheme(uri.scheme) == null ? fallbackPort : '';
}
