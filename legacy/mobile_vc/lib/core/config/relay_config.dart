import 'dart:convert';

import '../relay_e2ee/relay_e2ee_capability.dart';

enum ConnectionMode { direct, relay, auto }

class RelayPairing {
  const RelayPairing({
    required this.relayUrl,
    required this.sessionId,
    required this.pairingSecret,
    required this.expiresAt,
    required this.nodeFingerprintHex,
    this.capabilities,
    this.lanHost = '',
    this.lanPort = '',
    this.lanToken = '',
    this.lanCwd = '',
    this.lanSecureTransport,
  });

  final String relayUrl;
  final String sessionId;
  final String pairingSecret;
  final int expiresAt;
  final String nodeFingerprintHex;
  final RelayE2eeCapabilitySet? capabilities;
  final String lanHost;
  final String lanPort;
  final String lanToken;
  final String lanCwd;
  final bool? lanSecureTransport;

  bool get hasLanEndpoint =>
      lanHost.trim().isNotEmpty && lanToken.trim().isNotEmpty;
}

String relayPairingUriFromEventJson(Map<String, Object?> json) {
  final relayUrl = (json['relayUrl'] ?? '').toString().trim();
  final sessionId = (json['sessionId'] ?? '').toString().trim();
  final secret = (json['pairingSecret'] ?? '').toString().trim();
  final expiresAt = (json['expiresAt'] as num?)?.toInt() ?? 0;
  final nodeFingerprintHex =
      (json['nodeFingerprintHex'] ?? '').toString().trim().toLowerCase();
  final capabilities = _relayCapabilitiesFromEvent(json['capabilities']);
  if (relayUrl.isEmpty ||
      sessionId.isEmpty ||
      secret.isEmpty ||
      !_isFingerprintHex(nodeFingerprintHex)) {
    throw const FormatException('relay pairing event is missing fields');
  }
  final query = <String, String>{
    'relay': relayUrl,
    'session': sessionId,
    'secret': secret,
    'exp': expiresAt.toString(),
    'nodeFingerprint': nodeFingerprintHex,
    ..._relayCapabilityQuery(capabilities),
  };
  _appendLanPairingQuery(query, json);
  return Uri(
    scheme: 'mobilevc',
    host: 'relay',
    path: '/v1',
    queryParameters: query,
  ).toString();
}

String normalizeConnectionMode(Object? value) {
  switch (value?.toString().trim()) {
    case 'relay':
      return ConnectionMode.relay.name;
    case 'auto':
      return ConnectionMode.auto.name;
    default:
      return ConnectionMode.direct.name;
  }
}

void validateRelayUrl(String raw) {
  final uri = Uri.parse(raw.trim());
  if (uri.scheme == 'wss') {
    _validateRelayUrlShape(uri);
    return;
  }
  if (uri.scheme != 'ws') {
    throw const FormatException('relay url must use ws:// or wss://');
  }
  _validateRelayUrlShape(uri);
  if (!_isDevelopmentRelayHost(uri.host)) {
    throw const FormatException(
      'ws:// relay urls are allowed only for loopback or LAN hosts',
    );
  }
}

RelayPairing? parseRelayPairingUri(String raw) {
  final normalized = _normalizeRelayPairingInput(raw);
  final uri = Uri.tryParse(normalized);
  if (uri == null || uri.scheme != 'mobilevc') {
    return null;
  }
  if (uri.host != 'relay' || uri.path != '/v1') {
    return null;
  }
  final relayUrl = (uri.queryParameters['relay'] ?? '').trim();
  final sessionId = (uri.queryParameters['session'] ?? '').trim();
  final secret = (uri.queryParameters['secret'] ?? '').trim();
  final expiresAt = int.tryParse(uri.queryParameters['exp'] ?? '');
  final nodeFingerprintHex =
      (uri.queryParameters['nodeFingerprint'] ?? '').trim().toLowerCase();
  final missingFields = <String>[
    if (relayUrl.isEmpty) 'relay',
    if (sessionId.isEmpty) 'session',
    if (secret.isEmpty) 'secret',
  ];
  if (missingFields.isNotEmpty) {
    throw FormatException(
      'relay pairing uri is missing fields: ${missingFields.join(', ')}',
    );
  }
  _validatePairingSecret(secret);
  if (!_isFingerprintHex(nodeFingerprintHex)) {
    throw const FormatException(
        'relay pairing uri is missing node fingerprint');
  }
  final capabilities = _parseRelayCapabilities(uri);
  final lanHost = (uri.queryParameters['lanHost'] ?? '').trim();
  final lanPort = (uri.queryParameters['lanPort'] ?? '').trim();
  final lanToken = (uri.queryParameters['lanToken'] ?? '').trim();
  final lanCwd = (uri.queryParameters['lanCwd'] ?? '').trim();
  final lanSecureTransport = _optionalBool(uri, 'lanSecureTransport');
  if ((lanHost.isEmpty) != (lanToken.isEmpty)) {
    throw const FormatException(
      'relay pairing uri LAN endpoint must include lanHost and lanToken',
    );
  }
  validateRelayUrl(relayUrl);
  return RelayPairing(
    relayUrl: relayUrl,
    sessionId: sessionId,
    pairingSecret: secret,
    expiresAt: expiresAt ?? 0,
    nodeFingerprintHex: nodeFingerprintHex,
    capabilities: capabilities,
    lanHost: lanHost,
    lanPort: lanPort,
    lanToken: lanToken,
    lanCwd: lanCwd,
    lanSecureTransport: lanSecureTransport,
  );
}

void _appendLanPairingQuery(
  Map<String, String> query,
  Map<String, Object?> json,
) {
  final host = (json['lanHost'] ?? json['host'] ?? '').toString().trim();
  final token = (json['lanToken'] ?? json['token'] ?? '').toString().trim();
  if (host.isEmpty || token.isEmpty) {
    return;
  }
  query['lanHost'] = host;
  final port = (json['lanPort'] ?? json['port'] ?? '').toString().trim();
  if (port.isNotEmpty) {
    query['lanPort'] = port;
  }
  query['lanToken'] = token;
  final cwd = (json['lanCwd'] ?? json['cwd'] ?? '').toString().trim();
  if (cwd.isNotEmpty) {
    query['lanCwd'] = cwd;
  }
  final secureTransport = json['lanSecureTransport'] ?? json['secureTransport'];
  if (secureTransport is bool) {
    query['lanSecureTransport'] = secureTransport.toString();
  }
}

String _normalizeRelayPairingInput(String raw) {
  final trimmed = raw.trim();
  if (!trimmed.startsWith('{')) {
    return trimmed;
  }
  final decoded = jsonDecode(trimmed);
  if (decoded is! Map) {
    throw const FormatException('relay pairing json must be an object');
  }
  return relayPairingUriFromEventJson(Map<String, Object?>.from(decoded));
}

void _validatePairingSecret(String secret) {
  final normalized = secret.trim().toLowerCase();
  if (normalized == '<redacted>' || normalized == 'redacted') {
    throw const FormatException(
      'relay pairing uri secret is redacted; scan the QR code or paste the full link',
    );
  }
}

bool _isFingerprintHex(String value) {
  if (value.length != 64) {
    return false;
  }
  for (final unit in value.codeUnits) {
    final isDigit = unit >= 0x30 && unit <= 0x39;
    final isLowerHex = unit >= 0x61 && unit <= 0x66;
    if (!isDigit && !isLowerHex) {
      return false;
    }
  }
  return true;
}

RelayE2eeCapabilitySet? _parseRelayCapabilities(Uri uri) {
  final hasCapabilities = _relayCapabilityQueryKeys.any(
    uri.queryParameters.containsKey,
  );
  if (!hasCapabilities) {
    return null;
  }
  return RelayE2eeCapabilitySet.fromJson(<String, Object?>{
    'relayProtocolVersion': _requiredInt(uri, 'relayProtocolVersion'),
    'e2eeProtocolVersion': _requiredInt(uri, 'e2eeProtocolVersion'),
    'cryptoSuite': _requiredString(uri, 'cryptoSuite'),
    'tunnelProtocolVersion': _requiredInt(uri, 'tunnelProtocolVersion'),
    'supportsMultiplexStreams': _requiredBool(
      uri,
      'supportsMultiplexStreams',
    ),
    'supportsFileDownloadStream': _requiredBool(
      uri,
      'supportsFileDownloadStream',
    ),
    'supportsDeviceManagement': _requiredBool(
      uri,
      'supportsDeviceManagement',
    ),
    'requiresE2EE': _requiredBool(uri, 'requiresE2EE'),
    'plaintextTestMode': _requiredBool(uri, 'plaintextTestMode'),
  });
}

RelayE2eeCapabilitySet _relayCapabilitiesFromEvent(Object? raw) {
  if (raw is! Map) {
    throw const FormatException('relay pairing event is missing capabilities');
  }
  return RelayE2eeCapabilitySet.fromJson(Map<String, Object?>.from(raw));
}

Map<String, String> _relayCapabilityQuery(RelayE2eeCapabilitySet capabilities) {
  return <String, String>{
    'relayProtocolVersion': capabilities.relayProtocolVersion.toString(),
    'e2eeProtocolVersion': capabilities.e2eeProtocolVersion.toString(),
    'cryptoSuite': capabilities.cryptoSuite,
    'tunnelProtocolVersion': capabilities.tunnelProtocolVersion.toString(),
    'supportsMultiplexStreams':
        capabilities.supportsMultiplexStreams.toString(),
    'supportsFileDownloadStream': capabilities.supportsFileDownload.toString(),
    'supportsDeviceManagement':
        capabilities.supportsDeviceManagement.toString(),
    'requiresE2EE': capabilities.requiresE2EE.toString(),
    'plaintextTestMode': capabilities.plaintextTestMode.toString(),
  };
}

const _relayCapabilityQueryKeys = <String>{
  'relayProtocolVersion',
  'e2eeProtocolVersion',
  'cryptoSuite',
  'tunnelProtocolVersion',
  'supportsMultiplexStreams',
  'supportsFileDownloadStream',
  'supportsDeviceManagement',
  'requiresE2EE',
  'plaintextTestMode',
};

String _requiredString(Uri uri, String key) {
  final value = (uri.queryParameters[key] ?? '').trim();
  if (value.isEmpty) {
    throw FormatException('relay pairing uri is missing capability field $key');
  }
  return value;
}

int _requiredInt(Uri uri, String key) {
  final value = int.tryParse(_requiredString(uri, key));
  if (value == null) {
    throw FormatException(
        'relay pairing uri has invalid capability field $key');
  }
  return value;
}

bool _requiredBool(Uri uri, String key) {
  final value = _requiredString(uri, key).toLowerCase();
  return switch (value) {
    'true' => true,
    'false' => false,
    _ => throw FormatException(
        'relay pairing uri has invalid capability field $key',
      ),
  };
}

bool? _optionalBool(Uri uri, String key) {
  if (!uri.queryParameters.containsKey(key)) {
    return null;
  }
  final value = (uri.queryParameters[key] ?? '').trim().toLowerCase();
  return switch (value) {
    'true' => true,
    'false' => false,
    _ => throw FormatException('relay pairing uri has invalid field $key'),
  };
}

void _validateRelayUrlShape(Uri uri) {
  if (uri.host.trim().isEmpty || uri.userInfo.trim().isNotEmpty) {
    throw const FormatException('invalid relay url');
  }
  if (uri.path.isNotEmpty && uri.path != '/') {
    throw const FormatException('relay url path is not allowed');
  }
  if (uri.hasQuery || uri.hasFragment) {
    throw const FormatException('relay url query and fragment are not allowed');
  }
}

bool _isDevelopmentRelayHost(String host) {
  final normalized = host.trim().toLowerCase();
  if (normalized == 'localhost') {
    return true;
  }
  if (normalized.contains('.')) {
    return _isPrivateIPv4(normalized);
  }
  return normalized == '::1' ||
      normalized.startsWith('fc') ||
      normalized.startsWith('fd') ||
      normalized.startsWith('fe80:');
}

bool _isPrivateIPv4(String host) {
  final parts = host.split('.').map(int.tryParse).toList();
  if (parts.length != 4 || parts.any((part) => part == null)) {
    return false;
  }
  final first = parts[0]!;
  final second = parts[1]!;
  return first == 10 ||
      first == 127 ||
      (first == 169 && second == 254) ||
      (first == 192 && second == 168) ||
      (first == 172 && second >= 16 && second <= 31);
}
