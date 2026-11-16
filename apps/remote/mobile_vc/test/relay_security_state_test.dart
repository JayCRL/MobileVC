import 'dart:math';

import 'package:flutter_test/flutter_test.dart';
import 'package:mobile_vc/core/relay_e2ee/relay_e2ee_crypto.dart';
import 'package:mobile_vc/core/relay_e2ee/relay_e2ee_handshake.dart';
import 'package:mobile_vc/core/relay_e2ee/relay_security_state.dart';

void main() {
  test('shows verified only when every E2EE condition is satisfied', () async {
    final node = await RelayE2eeHandshake.newEphemeralKeyPair(
      random: Random(31),
    );
    final fingerprint = await RelayE2eeCrypto.fingerprint(node.publicKey);
    final state = await RelaySecurityStateEvaluator.evaluate(
      _validRelayInput(
        nodePublicKey: node.publicKey,
        expectedFingerprintHex: _hex(fingerprint),
      ),
    );

    expect(state.mode, RelaySecurityMode.relayE2eeVerified);
    expect(state.canShowVerified, isTrue);
    expect(state.title, 'E2EE 已验证');
    expect(state.shortFingerprint.split('-'), hasLength(5));
  });

  test('relay plaintext test mode cannot look verified', () async {
    final node = await RelayE2eeHandshake.newEphemeralKeyPair(
      random: Random(32),
    );
    final state = await RelaySecurityStateEvaluator.evaluate(
      _validRelayInput(
        nodePublicKey: node.publicKey,
        plaintextTestMode: true,
      ),
    );

    expect(state.mode, RelaySecurityMode.relayTestMode);
    expect(state.canShowVerified, isFalse);
    expect(state.title, 'Relay 测试模式');
  });

  test('plaintext test mode label wins over missing E2EE capabilities',
      () async {
    final state = await RelaySecurityStateEvaluator.evaluate(
      _validRelayInput(
        plaintextTestMode: true,
        protocolSupportsE2ee: false,
        supportsDeviceManagement: false,
      ),
    );

    expect(state.mode, RelaySecurityMode.relayTestMode);
    expect(state.title, 'Relay 测试模式');
    expect(state.canShowVerified, isFalse);
  });

  test('fingerprint mismatch blocks trust', () async {
    final node = await RelayE2eeHandshake.newEphemeralKeyPair(
      random: Random(33),
    );
    final state = await RelaySecurityStateEvaluator.evaluate(
      _validRelayInput(
        nodePublicKey: node.publicKey,
        expectedFingerprintHex: '00',
      ),
    );

    expect(state.mode, RelaySecurityMode.fingerprintMismatch);
    expect(state.isBlocking, isTrue);
    expect(state.title, '指纹已变化');
  });

  test('revoked device and decrypt failure are blocking states', () async {
    final revoked = await RelaySecurityStateEvaluator.evaluate(
      _validRelayInput(deviceRevoked: true),
    );
    final decryptFailed = await RelaySecurityStateEvaluator.evaluate(
      _validRelayInput(decryptFailed: true),
    );

    expect(revoked.mode, RelaySecurityMode.deviceRevoked);
    expect(revoked.isBlocking, isTrue);
    expect(decryptFailed.mode, RelaySecurityMode.encryptionUnavailable);
    expect(decryptFailed.isBlocking, isTrue);
  });

  test('missing capability and plaintext rejection prevent verified label',
      () async {
    final unsupported = await RelaySecurityStateEvaluator.evaluate(
      _validRelayInput(protocolSupportsE2ee: false),
    );
    final plaintextNotRejected = await RelaySecurityStateEvaluator.evaluate(
      _validRelayInput(productionPlaintextRejected: false),
    );

    expect(unsupported.mode, RelaySecurityMode.encryptionUnavailable);
    expect(unsupported.canShowVerified, isFalse);
    expect(plaintextNotRejected.mode, RelaySecurityMode.plaintextDisabled);
    expect(plaintextNotRejected.canShowVerified, isFalse);
  });

  test('device binding is required for verified relay state', () async {
    final state = await RelaySecurityStateEvaluator.evaluate(
      _validRelayInput(deviceBound: false),
    );

    expect(state.mode, RelaySecurityMode.relayNotVerified);
    expect(state.canShowVerified, isFalse);
  });

  test('direct mode never implies E2EE verified', () async {
    final state = await RelaySecurityStateEvaluator.evaluate(
      _validRelayInput(connectionMode: 'direct'),
    );

    expect(state.mode, RelaySecurityMode.direct);
    expect(state.canShowVerified, isFalse);
    expect(state.title, 'LAN 直连');
  });
}

RelaySecurityInput _validRelayInput({
  String connectionMode = 'relay',
  List<int> nodePublicKey = const <int>[],
  String expectedFingerprintHex = '',
  bool nodeFingerprintConfirmed = true,
  bool handshakeComplete = true,
  bool protocolSupportsE2ee = true,
  bool protocolSupportsTunnel = true,
  bool supportsMultiplexStreams = true,
  bool supportsFileDownload = true,
  bool supportsDeviceManagement = true,
  bool requiresE2ee = true,
  bool plaintextTestMode = false,
  bool deviceRevoked = false,
  bool productionPlaintextRejected = true,
  bool decryptFailed = false,
  bool deviceBound = true,
}) {
  return RelaySecurityInput(
    connectionMode: connectionMode,
    expectedNodeFingerprintHex: expectedFingerprintHex,
    actualNodePublicKey: nodePublicKey,
    nodeFingerprintConfirmed: nodeFingerprintConfirmed,
    handshakeComplete: handshakeComplete,
    protocolSupportsE2ee: protocolSupportsE2ee,
    protocolSupportsTunnel: protocolSupportsTunnel,
    supportsMultiplexStreams: supportsMultiplexStreams,
    supportsFileDownload: supportsFileDownload,
    supportsDeviceManagement: supportsDeviceManagement,
    requiresE2ee: requiresE2ee,
    plaintextTestMode: plaintextTestMode,
    deviceRevoked: deviceRevoked,
    productionPlaintextRejected: productionPlaintextRejected,
    decryptFailed: decryptFailed,
    deviceBound: deviceBound,
  );
}

String _hex(List<int> bytes) {
  const digits = '0123456789abcdef';
  final buffer = StringBuffer();
  for (final byte in bytes) {
    buffer
      ..write(digits[byte >> 4])
      ..write(digits[byte & 0x0f]);
  }
  return buffer.toString();
}
