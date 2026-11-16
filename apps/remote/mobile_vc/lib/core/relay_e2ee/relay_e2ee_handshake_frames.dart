import 'dart:convert';
import 'dart:typed_data';

import 'relay_e2ee_capability.dart';
import 'relay_e2ee_handshake.dart';

const relayFrameClientE2eeHello = 'client.e2ee_hello';
const relayFrameAgentE2eeHello = 'agent.e2ee_hello';
const relayFrameClientE2eeProof = 'client.e2ee_proof';
const relayFrameAgentE2eeResult = 'agent.e2ee_result';
const relayFrameProtocolVersion = 1;

class RelayE2eeClientHelloFrame {
  const RelayE2eeClientHelloFrame({
    required this.sessionId,
    required this.clientId,
    required this.handshakeId,
    required this.kind,
    required this.capabilities,
    required this.clientEphemeralPublicKey,
    this.deviceId = '',
    this.deviceIdentityPublicKey = const <int>[],
  });

  factory RelayE2eeClientHelloFrame.fromJson(Map<String, dynamic> json) {
    _validateHeader(json, relayFrameClientE2eeHello);
    final capabilities = _capabilitiesFromJson(json['capabilities']);
    final frame = RelayE2eeClientHelloFrame(
      sessionId: _requiredString(json, 'sessionId'),
      clientId: _requiredString(json, 'clientId'),
      handshakeId: _requiredString(json, 'handshakeId'),
      kind: _requiredString(json, 'kind'),
      capabilities: capabilities,
      clientEphemeralPublicKey: _decodeRequiredPublicKey(
        json,
        'clientEphemeralPublicKey',
      ),
      deviceId: (json['deviceId'] ?? '').toString(),
      deviceIdentityPublicKey: json.containsKey('deviceIdentityPublicKey')
          ? _decodeRequiredPublicKey(json, 'deviceIdentityPublicKey')
          : const <int>[],
    );
    frame.validate();
    return frame;
  }

  final String sessionId;
  final String clientId;
  final String handshakeId;
  final String kind;
  final RelayE2eeCapabilitySet capabilities;
  final List<int> clientEphemeralPublicKey;
  final String deviceId;
  final List<int> deviceIdentityPublicKey;

  Map<String, Object> toJson() {
    validate();
    final json = <String, Object>{
      'type': relayFrameClientE2eeHello,
      'version': relayFrameProtocolVersion,
      'sessionId': sessionId,
      'clientId': clientId,
      'handshakeId': handshakeId,
      'kind': kind,
      'capabilities': capabilities.toJson(),
      'clientEphemeralPublicKey': encodeRelayFrameBytes(
        clientEphemeralPublicKey,
      ),
    };
    if (deviceId.trim().isNotEmpty) {
      json['deviceId'] = deviceId;
    }
    if (deviceIdentityPublicKey.isNotEmpty) {
      json['deviceIdentityPublicKey'] = encodeRelayFrameBytes(
        deviceIdentityPublicKey,
      );
    }
    return json;
  }

  void validate() {
    _validateRoutingIds(sessionId, clientId, handshakeId);
    _validateKind(kind);
    capabilities.validateProduction();
    _validatePublicKey(clientEphemeralPublicKey);
    if (kind == relayE2eeHandshakeKindReconnect) {
      if (deviceId.trim().isEmpty || deviceIdentityPublicKey.isEmpty) {
        throw const FormatException('device identity is required');
      }
      _validatePublicKey(deviceIdentityPublicKey);
      return;
    }
    if (deviceId.trim().isNotEmpty || deviceIdentityPublicKey.isNotEmpty) {
      throw const FormatException('pairing hello has unexpected device fields');
    }
  }
}

class RelayE2eeAgentHelloFrame {
  const RelayE2eeAgentHelloFrame({
    required this.sessionId,
    required this.clientId,
    required this.handshakeId,
    required this.capabilities,
    required this.nodeEphemeralPublicKey,
    required this.nodeIdentityPublicKey,
    required this.nodeSignature,
  });

  factory RelayE2eeAgentHelloFrame.fromJson(Map<String, dynamic> json) {
    _validateHeader(json, relayFrameAgentE2eeHello);
    final frame = RelayE2eeAgentHelloFrame(
      sessionId: _requiredString(json, 'sessionId'),
      clientId: _requiredString(json, 'clientId'),
      handshakeId: _requiredString(json, 'handshakeId'),
      capabilities: _capabilitiesFromJson(json['capabilities']),
      nodeEphemeralPublicKey: _decodeRequiredPublicKey(
        json,
        'nodeEphemeralPublicKey',
      ),
      nodeIdentityPublicKey: _decodeRequiredPublicKey(
        json,
        'nodeIdentityPublicKey',
      ),
      nodeSignature: _decodeRequiredBytes(json, 'nodeSignature'),
    );
    frame.validate();
    return frame;
  }

  final String sessionId;
  final String clientId;
  final String handshakeId;
  final RelayE2eeCapabilitySet capabilities;
  final List<int> nodeEphemeralPublicKey;
  final List<int> nodeIdentityPublicKey;
  final List<int> nodeSignature;

  Map<String, Object> toJson() {
    validate();
    return <String, Object>{
      'type': relayFrameAgentE2eeHello,
      'version': relayFrameProtocolVersion,
      'sessionId': sessionId,
      'clientId': clientId,
      'handshakeId': handshakeId,
      'capabilities': capabilities.toJson(),
      'nodeEphemeralPublicKey': encodeRelayFrameBytes(
        nodeEphemeralPublicKey,
      ),
      'nodeIdentityPublicKey': encodeRelayFrameBytes(nodeIdentityPublicKey),
      'nodeSignature': encodeRelayFrameBytes(nodeSignature),
    };
  }

  void validate() {
    _validateRoutingIds(sessionId, clientId, handshakeId);
    capabilities.validateProduction();
    _validatePublicKey(nodeEphemeralPublicKey);
    _validatePublicKey(nodeIdentityPublicKey);
    if (nodeSignature.isEmpty) {
      throw const FormatException('node signature is required');
    }
  }
}

class RelayE2eeClientProofFrame {
  const RelayE2eeClientProofFrame({
    required this.sessionId,
    required this.clientId,
    required this.handshakeId,
    required this.kind,
    this.pairingProof = const <int>[],
    this.deviceProof = const <int>[],
    this.deviceSignature = const <int>[],
  });

  factory RelayE2eeClientProofFrame.fromJson(Map<String, dynamic> json) {
    _validateHeader(json, relayFrameClientE2eeProof);
    final frame = RelayE2eeClientProofFrame(
      sessionId: _requiredString(json, 'sessionId'),
      clientId: _requiredString(json, 'clientId'),
      handshakeId: _requiredString(json, 'handshakeId'),
      kind: _requiredString(json, 'kind'),
      pairingProof: json.containsKey('pairingProof')
          ? _decodeRequiredBytes(json, 'pairingProof')
          : const <int>[],
      deviceProof: json.containsKey('deviceProof')
          ? _decodeRequiredBytes(json, 'deviceProof')
          : const <int>[],
      deviceSignature: json.containsKey('deviceSignature')
          ? _decodeRequiredBytes(json, 'deviceSignature')
          : const <int>[],
    );
    frame.validate();
    return frame;
  }

  final String sessionId;
  final String clientId;
  final String handshakeId;
  final String kind;
  final List<int> pairingProof;
  final List<int> deviceProof;
  final List<int> deviceSignature;

  Map<String, Object> toJson() {
    validate();
    final json = <String, Object>{
      'type': relayFrameClientE2eeProof,
      'version': relayFrameProtocolVersion,
      'sessionId': sessionId,
      'clientId': clientId,
      'handshakeId': handshakeId,
      'kind': kind,
    };
    if (pairingProof.isNotEmpty) {
      json['pairingProof'] = encodeRelayFrameBytes(pairingProof);
    }
    if (deviceProof.isNotEmpty) {
      json['deviceProof'] = encodeRelayFrameBytes(deviceProof);
    }
    if (deviceSignature.isNotEmpty) {
      json['deviceSignature'] = encodeRelayFrameBytes(deviceSignature);
    }
    return json;
  }

  void validate() {
    _validateRoutingIds(sessionId, clientId, handshakeId);
    _validateKind(kind);
    if (kind == relayE2eeHandshakeKindPairing) {
      if (pairingProof.isEmpty) {
        throw const FormatException('pairing proof is required');
      }
      if (deviceProof.isNotEmpty || deviceSignature.isNotEmpty) {
        throw const FormatException(
            'pairing proof has unexpected device fields');
      }
      return;
    }
    if (deviceProof.isEmpty || deviceSignature.isEmpty) {
      throw const FormatException('device proof and signature are required');
    }
    if (pairingProof.isNotEmpty) {
      throw const FormatException(
          'reconnect proof has unexpected pairing proof');
    }
  }
}

class RelayE2eeAgentResultFrame {
  const RelayE2eeAgentResultFrame({
    required this.sessionId,
    required this.clientId,
    required this.handshakeId,
    required this.ok,
    this.errorCode = '',
  });

  factory RelayE2eeAgentResultFrame.fromJson(Map<String, dynamic> json) {
    _validateHeader(json, relayFrameAgentE2eeResult);
    final frame = RelayE2eeAgentResultFrame(
      sessionId: _requiredString(json, 'sessionId'),
      clientId: _requiredString(json, 'clientId'),
      handshakeId: _requiredString(json, 'handshakeId'),
      ok: json['ok'] == true,
      errorCode: (json['errorCode'] ?? '').toString(),
    );
    frame.validate();
    return frame;
  }

  final String sessionId;
  final String clientId;
  final String handshakeId;
  final bool ok;
  final String errorCode;

  Map<String, Object> toJson() {
    validate();
    final json = <String, Object>{
      'type': relayFrameAgentE2eeResult,
      'version': relayFrameProtocolVersion,
      'sessionId': sessionId,
      'clientId': clientId,
      'handshakeId': handshakeId,
      'ok': ok,
    };
    if (errorCode.trim().isNotEmpty) {
      json['errorCode'] = errorCode;
    }
    return json;
  }

  void validate() {
    _validateRoutingIds(sessionId, clientId, handshakeId);
    if (ok && errorCode.trim().isNotEmpty) {
      throw const FormatException('successful result has error code');
    }
    if (!ok && errorCode.trim().isEmpty) {
      throw const FormatException('failed result requires error code');
    }
  }
}

String encodeRelayFrameBytes(List<int> value) =>
    base64Url.encode(value).replaceAll('=', '');

RelayE2eeCapabilitySet _capabilitiesFromJson(Object? value) {
  if (value is! Map<String, dynamic>) {
    throw const FormatException('capabilities are required');
  }
  final capabilities = RelayE2eeCapabilitySet(
    relayProtocolVersion: _requiredInt(value, 'relayProtocolVersion'),
    e2eeProtocolVersion: _requiredInt(value, 'e2eeProtocolVersion'),
    cryptoSuite: _requiredString(value, 'cryptoSuite'),
    tunnelProtocolVersion: _requiredInt(value, 'tunnelProtocolVersion'),
    supportsMultiplexStreams: _requiredBool(value, 'supportsMultiplexStreams'),
    supportsFileDownload: _requiredBool(value, 'supportsFileDownloadStream'),
    supportsDeviceManagement: _requiredBool(value, 'supportsDeviceManagement'),
    requiresE2EE: _requiredBool(value, 'requiresE2EE'),
    plaintextTestMode: _requiredBool(value, 'plaintextTestMode'),
  );
  capabilities.validateProduction();
  return capabilities;
}

void _validateHeader(Map<String, dynamic> json, String expectedType) {
  if (json['type'] != expectedType ||
      json['version'] != relayFrameProtocolVersion) {
    throw const FormatException('invalid E2EE handshake frame header');
  }
}

void _validateRoutingIds(
  String sessionId,
  String clientId,
  String handshakeId,
) {
  if (sessionId.trim().isEmpty ||
      clientId.trim().isEmpty ||
      handshakeId.trim().isEmpty) {
    throw const FormatException('missing E2EE handshake routing id');
  }
}

void _validateKind(String kind) {
  if (kind != relayE2eeHandshakeKindPairing &&
      kind != relayE2eeHandshakeKindReconnect) {
    throw const FormatException('invalid E2EE handshake kind');
  }
}

List<int> _decodeRequiredPublicKey(Map<String, dynamic> json, String key) {
  final value = _decodeRequiredBytes(json, key);
  _validatePublicKey(value);
  return value;
}

List<int> _decodeRequiredBytes(Map<String, dynamic> json, String key) {
  final raw = _requiredString(json, key);
  try {
    return Uint8List.fromList(base64Url.decode(base64Url.normalize(raw)));
  } on FormatException {
    throw FormatException('invalid $key encoding');
  }
}

void _validatePublicKey(List<int> value) {
  if (value.length != 65 || value.first != 0x04) {
    throw const FormatException('invalid P-256 public key');
  }
}

String _requiredString(Map<String, dynamic> json, String key) {
  final value = json[key];
  if (value is! String || value.trim().isEmpty) {
    throw FormatException('$key is required');
  }
  return value;
}

int _requiredInt(Map<String, dynamic> json, String key) {
  final value = json[key];
  if (value is! int) {
    throw FormatException('$key is required');
  }
  return value;
}

bool _requiredBool(Map<String, dynamic> json, String key) {
  final value = json[key];
  if (value is! bool) {
    throw FormatException('$key is required');
  }
  return value;
}
