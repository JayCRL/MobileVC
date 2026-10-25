import 'dart:math';

import 'package:flutter_test/flutter_test.dart';
import 'package:mobile_vc/core/relay_e2ee/relay_e2ee_capability.dart';
import 'package:mobile_vc/core/relay_e2ee/relay_e2ee_crypto.dart';
import 'package:mobile_vc/core/relay_e2ee/relay_e2ee_handshake.dart';

void main() {
  test('production capabilities pass validation', () {
    RelayE2eeCapabilitySet.production().validateProduction();
  });

  test('production capabilities reject plaintext test mode', () {
    final capabilities = RelayE2eeCapabilitySet(
      relayProtocolVersion:
          RelayE2eeCapabilitySet.supportedRelayProtocolVersion,
      e2eeProtocolVersion: relayE2eeVersion,
      cryptoSuite: relayE2eeSuite,
      tunnelProtocolVersion:
          RelayE2eeCapabilitySet.supportedTunnelProtocolVersion,
      supportsMultiplexStreams: true,
      supportsFileDownload: true,
      supportsDeviceManagement: true,
      requiresE2EE: true,
      plaintextTestMode: true,
    );

    expect(capabilities.validateProduction, throwsArgumentError);
  });

  test('production capabilities reject missing tunnel feature', () {
    final capabilities = RelayE2eeCapabilitySet(
      relayProtocolVersion:
          RelayE2eeCapabilitySet.supportedRelayProtocolVersion,
      e2eeProtocolVersion: relayE2eeVersion,
      cryptoSuite: relayE2eeSuite,
      tunnelProtocolVersion:
          RelayE2eeCapabilitySet.supportedTunnelProtocolVersion,
      supportsMultiplexStreams: true,
      supportsFileDownload: false,
      supportsDeviceManagement: true,
      requiresE2EE: true,
      plaintextTestMode: false,
    );

    expect(capabilities.validateProduction, throwsArgumentError);
  });

  test('plaintext test capabilities require explicit test mode', () {
    RelayE2eeCapabilitySet.plaintextTestMode().validatePlaintextTestMode();

    final capabilities = RelayE2eeCapabilitySet.production();
    expect(capabilities.validatePlaintextTestMode, throwsArgumentError);
  });

  test('capabilities apply to handshake transcript', () async {
    final clientEphemeral = await RelayE2eeHandshake.newEphemeralKeyPair(
      random: Random(41),
    );
    final nodeEphemeral = await RelayE2eeHandshake.newEphemeralKeyPair(
      random: Random(42),
    );
    final nodeIdentity = await RelayE2eeHandshake.newEphemeralKeyPair(
      random: Random(43),
    );
    final input = RelayE2eeCapabilitySet.production().applyToHandshake(
      RelayE2eeHandshakeInput(
        kind: relayE2eeHandshakeKindPairing,
        sessionId: 'rs_capability',
        clientId: 'rc_capability',
        handshakeId: 'hs_capability',
        relayProtocolVersion: 0,
        e2eeProtocolVersion: 0,
        tunnelProtocolVersion: 0,
        cryptoSuite: 'unset',
        clientEphemeralPublicKey: clientEphemeral.publicKey,
        nodeEphemeralPublicKey: nodeEphemeral.publicKey,
        nodeIdentityPublicKey: nodeIdentity.publicKey,
        requiresE2EE: false,
        plaintextTestMode: true,
        supportsMultiplexStreams: false,
        supportsFileDownload: false,
        supportsDeviceManagement: false,
      ),
    );

    expect(RelayE2eeHandshake.transcript(input), isNotEmpty);
    expect(input.requiresE2EE, isTrue);
    expect(input.plaintextTestMode, isFalse);
  });

  test('unsupported capability version is rejected', () {
    final capabilities = RelayE2eeCapabilitySet(
      relayProtocolVersion: 2,
      e2eeProtocolVersion: relayE2eeVersion,
      cryptoSuite: relayE2eeSuite,
      tunnelProtocolVersion:
          RelayE2eeCapabilitySet.supportedTunnelProtocolVersion,
      supportsMultiplexStreams: true,
      supportsFileDownload: true,
      supportsDeviceManagement: true,
      requiresE2EE: true,
      plaintextTestMode: false,
    );

    expect(capabilities.validateProduction, throwsArgumentError);
  });
}
