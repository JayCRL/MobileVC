import 'dart:convert';
import 'dart:math';
import 'dart:typed_data';

import 'package:cryptography/cryptography.dart';

import 'relay_device_identity.dart';
import 'relay_e2ee_crypto.dart';

const relayE2eeHandshakeKindPairing = 'pairing';
const relayE2eeHandshakeKindReconnect = 'reconnect';

class RelayE2eeHandshakeInput {
  const RelayE2eeHandshakeInput({
    required this.kind,
    required this.sessionId,
    required this.clientId,
    required this.handshakeId,
    required this.relayProtocolVersion,
    required this.e2eeProtocolVersion,
    required this.tunnelProtocolVersion,
    required this.cryptoSuite,
    required this.clientEphemeralPublicKey,
    required this.nodeEphemeralPublicKey,
    required this.nodeIdentityPublicKey,
    this.deviceIdentityPublicKey = const <int>[],
    required this.requiresE2EE,
    required this.plaintextTestMode,
    required this.supportsMultiplexStreams,
    required this.supportsFileDownload,
    required this.supportsDeviceManagement,
  });

  final String kind;
  final String sessionId;
  final String clientId;
  final String handshakeId;
  final int relayProtocolVersion;
  final int e2eeProtocolVersion;
  final int tunnelProtocolVersion;
  final String cryptoSuite;
  final List<int> clientEphemeralPublicKey;
  final List<int> nodeEphemeralPublicKey;
  final List<int> nodeIdentityPublicKey;
  final List<int> deviceIdentityPublicKey;
  final bool requiresE2EE;
  final bool plaintextTestMode;
  final bool supportsMultiplexStreams;
  final bool supportsFileDownload;
  final bool supportsDeviceManagement;
}

class RelayE2eeEphemeralKeyPair {
  const RelayE2eeEphemeralKeyPair({
    required this.privateScalar,
    required this.publicKey,
  });

  final Uint8List privateScalar;
  final Uint8List publicKey;
}

class RelayE2eeTrafficKeys {
  const RelayE2eeTrafficKeys({
    required this.clientToAgent,
    required this.agentToClient,
  });

  final Uint8List clientToAgent;
  final Uint8List agentToClient;
}

class RelayE2eeHandshake {
  static Future<RelayE2eeEphemeralKeyPair> newEphemeralKeyPair({
    Random? random,
  }) async {
    final rng = random ?? Random.secure();
    while (true) {
      final scalar = Uint8List(relayE2eeKeyLength);
      for (var i = 0; i < scalar.length; i++) {
        scalar[i] = rng.nextInt(256);
      }
      try {
        return RelayE2eeEphemeralKeyPair(
          privateScalar: scalar,
          publicKey: RelayE2eeCrypto.publicKeyFromPrivate(scalar),
        );
      } on ArgumentError {
        continue;
      }
    }
  }

  static Uint8List transcript(RelayE2eeHandshakeInput input) {
    _validateInput(input);
    final bytes = BytesBuilder(copy: false)
      ..add(utf8.encode('MVCH'))
      ..add(_uint16(relayE2eeVersion));
    for (final field in [
      input.kind,
      input.sessionId,
      input.clientId,
      input.handshakeId,
      input.cryptoSuite,
    ]) {
      bytes.add(_lengthPrefixedString(field));
    }
    bytes
      ..add(_int32(input.relayProtocolVersion))
      ..add(_int32(input.e2eeProtocolVersion))
      ..add(_int32(input.tunnelProtocolVersion))
      ..addByte(input.requiresE2EE ? 1 : 0)
      ..addByte(input.plaintextTestMode ? 1 : 0)
      ..addByte(input.supportsMultiplexStreams ? 1 : 0)
      ..addByte(input.supportsFileDownload ? 1 : 0)
      ..addByte(input.supportsDeviceManagement ? 1 : 0);
    for (final key in [
      input.clientEphemeralPublicKey,
      input.nodeEphemeralPublicKey,
      input.nodeIdentityPublicKey,
      input.deviceIdentityPublicKey,
    ]) {
      bytes.add(_lengthPrefixedBytes(key));
    }
    return bytes.takeBytes();
  }

  static Future<Uint8List> pairingProof({
    required String pairingSecret,
    required Uint8List transcript,
  }) {
    return _proof('pairing_secret', pairingSecret, transcript);
  }

  static Future<Uint8List> deviceProof({
    required String deviceCredential,
    required Uint8List transcript,
  }) {
    return _proof('device_credential', deviceCredential, transcript);
  }

  static Future<bool> verifyPairingProof({
    required String pairingSecret,
    required Uint8List transcript,
    required Uint8List expected,
  }) async {
    final actual = await pairingProof(
      pairingSecret: pairingSecret,
      transcript: transcript,
    );
    return _constantTimeEquals(actual, expected);
  }

  static Future<bool> verifyDeviceProof({
    required String deviceCredential,
    required Uint8List transcript,
    required Uint8List expected,
  }) async {
    final actual = await deviceProof(
      deviceCredential: deviceCredential,
      transcript: transcript,
    );
    return _constantTimeEquals(actual, expected);
  }

  static Future<RelayE2eeTrafficKeys> deriveTrafficKeys({
    required Uint8List privateScalar,
    required Uint8List remotePublicKey,
    required RelayE2eeHandshakeInput input,
  }) async {
    final transcriptBytes = transcript(input);
    final sharedSecret = RelayE2eeCrypto.sharedSecret(
      privateScalar: privateScalar,
      remotePublicKey: remotePublicKey,
    );
    final transcriptHash = await Sha256().hash(transcriptBytes);
    final hkdf = Hkdf(
      hmac: Hmac.sha256(),
      outputLength: relayE2eeKeyLength * 2,
    );
    final material = await hkdf.deriveKey(
      secretKey: SecretKey(sharedSecret),
      nonce: transcriptHash.bytes,
      info: utf8.encode(
        'mobilevc relay e2ee handshake traffic v1|$relayE2eeSuite',
      ),
    );
    final bytes = await material.extractBytes();
    return RelayE2eeTrafficKeys(
      clientToAgent:
          Uint8List.fromList(bytes.take(relayE2eeKeyLength).toList()),
      agentToClient:
          Uint8List.fromList(bytes.skip(relayE2eeKeyLength).toList()),
    );
  }

  static Future<void> validatePairingHandshake({
    required RelayE2eeHandshakeInput input,
    required String pairingSecret,
    required Uint8List pairingProof,
    required Uint8List nodeSignature,
  }) async {
    final transcriptBytes = transcript(input);
    final signatureOk = await RelayDeviceIdentityStore.verifyWithPublicKey(
      publicKey: Uint8List.fromList(input.nodeIdentityPublicKey),
      transcript: transcriptBytes,
      signature: nodeSignature,
    );
    final proofOk = await verifyPairingProof(
      pairingSecret: pairingSecret,
      transcript: transcriptBytes,
      expected: pairingProof,
    );
    if (!signatureOk || !proofOk) {
      throw StateError('e2ee handshake failed');
    }
  }

  static Future<void> validateReconnectHandshake({
    required RelayE2eeHandshakeInput input,
    required String deviceCredential,
    required Uint8List deviceProof,
    required Uint8List nodeSignature,
    required Uint8List deviceSignature,
  }) async {
    final transcriptBytes = transcript(input);
    final nodeOk = await RelayDeviceIdentityStore.verifyWithPublicKey(
      publicKey: Uint8List.fromList(input.nodeIdentityPublicKey),
      transcript: transcriptBytes,
      signature: nodeSignature,
    );
    final deviceOk = await RelayDeviceIdentityStore.verifyWithPublicKey(
      publicKey: Uint8List.fromList(input.deviceIdentityPublicKey),
      transcript: transcriptBytes,
      signature: deviceSignature,
    );
    final proofOk = await verifyDeviceProof(
      deviceCredential: deviceCredential,
      transcript: transcriptBytes,
      expected: deviceProof,
    );
    if (!nodeOk || !deviceOk || !proofOk) {
      throw StateError('e2ee handshake failed');
    }
  }

  static Future<Uint8List> _proof(
    String purpose,
    String secret,
    Uint8List transcript,
  ) async {
    final secretHash = await Sha256().hash(utf8.encode(secret));
    final transcriptHash = await Sha256().hash(transcript);
    final bytes = BytesBuilder(copy: false)
      ..add(utf8.encode('$purpose|'))
      ..add(secretHash.bytes)
      ..addByte('|'.codeUnitAt(0))
      ..add(transcriptHash.bytes);
    final proofHash = await Sha256().hash(bytes.takeBytes());
    return Uint8List.fromList(
      utf8.encode(base64Url.encode(proofHash.bytes).replaceAll('=', '')),
    );
  }

  static void _validateInput(RelayE2eeHandshakeInput input) {
    final validKind = input.kind == relayE2eeHandshakeKindPairing ||
        input.kind == relayE2eeHandshakeKindReconnect;
    if (!validKind) {
      throw ArgumentError.value(input.kind, 'kind', 'invalid handshake kind');
    }
    if (input.cryptoSuite != relayE2eeSuite ||
        input.e2eeProtocolVersion != relayE2eeVersion) {
      throw ArgumentError('unsupported E2EE handshake capability');
    }
    if (input.requiresE2EE && input.plaintextTestMode) {
      throw ArgumentError('conflicting plaintext and E2EE requirements');
    }
    for (final key in [
      input.clientEphemeralPublicKey,
      input.nodeEphemeralPublicKey,
      input.nodeIdentityPublicKey,
    ]) {
      _validatePublicKey(key);
    }
    if (input.kind == relayE2eeHandshakeKindReconnect) {
      _validatePublicKey(input.deviceIdentityPublicKey);
    }
  }

  static void _validatePublicKey(List<int> key) {
    if (key.length != 65 || key.first != 0x04) {
      throw ArgumentError('invalid P-256 public key');
    }
  }

  static bool _constantTimeEquals(List<int> a, List<int> b) {
    if (a.length != b.length) {
      return false;
    }
    var diff = 0;
    for (var i = 0; i < a.length; i++) {
      diff |= a[i] ^ b[i];
    }
    return diff == 0;
  }

  static Uint8List _lengthPrefixedString(String value) {
    return _lengthPrefixedBytes(utf8.encode(value));
  }

  static Uint8List _lengthPrefixedBytes(List<int> value) {
    if (value.length > 0xffff) {
      throw ArgumentError.value(value, 'value', 'handshake field is too large');
    }
    return Uint8List.fromList([..._uint16(value.length), ...value]);
  }

  static Uint8List _uint16(int value) {
    final bytes = ByteData(2)..setUint16(0, value);
    return bytes.buffer.asUint8List();
  }

  static Uint8List _int32(int value) {
    final bytes = ByteData(4)..setUint32(0, value);
    return bytes.buffer.asUint8List();
  }
}
