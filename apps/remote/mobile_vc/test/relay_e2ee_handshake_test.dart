import 'dart:math';
import 'dart:typed_data';

import 'package:flutter_test/flutter_test.dart';
import 'package:mobile_vc/core/relay_e2ee/relay_device_identity.dart';
import 'package:mobile_vc/core/relay_e2ee/relay_e2ee_crypto.dart';
import 'package:mobile_vc/core/relay_e2ee/relay_e2ee_handshake.dart';

void main() {
  test('pairing handshake authenticates and derives directional keys',
      () async {
    final clientEphemeral =
        await RelayE2eeHandshake.newEphemeralKeyPair(random: Random(1));
    final nodeEphemeral =
        await RelayE2eeHandshake.newEphemeralKeyPair(random: Random(2));
    final nodeIdentity =
        await RelayE2eeHandshake.newEphemeralKeyPair(random: Random(3));
    final input = _handshakeInput(
      kind: relayE2eeHandshakeKindPairing,
      clientEphemeral: clientEphemeral,
      nodeEphemeral: nodeEphemeral,
      nodeIdentityPublicKey: nodeIdentity.publicKey,
    );
    final transcript = RelayE2eeHandshake.transcript(input);
    final pairingSecret = 'pair-secret-128-bit-minimum';
    final pairingProof = await RelayE2eeHandshake.pairingProof(
      pairingSecret: pairingSecret,
      transcript: transcript,
    );
    final nodeSignature = await RelayDeviceIdentityStore.signWithPrivateScalar(
      privateScalar: nodeIdentity.privateScalar,
      transcript: transcript,
    );

    await RelayE2eeHandshake.validatePairingHandshake(
      input: input,
      pairingSecret: pairingSecret,
      pairingProof: pairingProof,
      nodeSignature: nodeSignature,
    );

    final clientKeys = await RelayE2eeHandshake.deriveTrafficKeys(
      privateScalar: clientEphemeral.privateScalar,
      remotePublicKey: nodeEphemeral.publicKey,
      input: input,
    );
    final nodeKeys = await RelayE2eeHandshake.deriveTrafficKeys(
      privateScalar: nodeEphemeral.privateScalar,
      remotePublicKey: clientEphemeral.publicKey,
      input: input,
    );

    expect(clientKeys.clientToAgent, nodeKeys.clientToAgent);
    expect(clientKeys.agentToClient, nodeKeys.agentToClient);
    expect(clientKeys.clientToAgent, isNot(clientKeys.agentToClient));

    final rekeyInput = _handshakeInput(
      kind: relayE2eeHandshakeKindPairing,
      handshakeId: 'hs_pairing_02',
      clientEphemeral: clientEphemeral,
      nodeEphemeral: nodeEphemeral,
      nodeIdentityPublicKey: nodeIdentity.publicKey,
    );
    final rekeyed = await RelayE2eeHandshake.deriveTrafficKeys(
      privateScalar: clientEphemeral.privateScalar,
      remotePublicKey: nodeEphemeral.publicKey,
      input: rekeyInput,
    );
    expect(rekeyed.clientToAgent, isNot(clientKeys.clientToAgent));
  });

  test('pairing handshake rejects bad proof and signature', () async {
    final clientEphemeral =
        await RelayE2eeHandshake.newEphemeralKeyPair(random: Random(4));
    final nodeEphemeral =
        await RelayE2eeHandshake.newEphemeralKeyPair(random: Random(5));
    final nodeIdentity =
        await RelayE2eeHandshake.newEphemeralKeyPair(random: Random(6));
    final input = _handshakeInput(
      kind: relayE2eeHandshakeKindPairing,
      clientEphemeral: clientEphemeral,
      nodeEphemeral: nodeEphemeral,
      nodeIdentityPublicKey: nodeIdentity.publicKey,
    );
    final transcript = RelayE2eeHandshake.transcript(input);
    final nodeSignature = await RelayDeviceIdentityStore.signWithPrivateScalar(
      privateScalar: nodeIdentity.privateScalar,
      transcript: transcript,
    );

    await expectLater(
      RelayE2eeHandshake.validatePairingHandshake(
        input: input,
        pairingSecret: 'right',
        pairingProof: await RelayE2eeHandshake.pairingProof(
          pairingSecret: 'wrong',
          transcript: transcript,
        ),
        nodeSignature: nodeSignature,
      ),
      throwsStateError,
    );

    final tampered = Uint8List.fromList(nodeSignature);
    tampered[0] ^= 0x01;
    await expectLater(
      RelayE2eeHandshake.validatePairingHandshake(
        input: input,
        pairingSecret: 'right',
        pairingProof: await RelayE2eeHandshake.pairingProof(
          pairingSecret: 'right',
          transcript: transcript,
        ),
        nodeSignature: tampered,
      ),
      throwsStateError,
    );
  });

  test('reconnect handshake requires device signature and fresh rekey',
      () async {
    final clientEphemeral =
        await RelayE2eeHandshake.newEphemeralKeyPair(random: Random(7));
    final nodeEphemeral =
        await RelayE2eeHandshake.newEphemeralKeyPair(random: Random(8));
    final nodeIdentity =
        await RelayE2eeHandshake.newEphemeralKeyPair(random: Random(9));
    final deviceIdentity =
        await RelayE2eeHandshake.newEphemeralKeyPair(random: Random(10));
    final input = _handshakeInput(
      kind: relayE2eeHandshakeKindReconnect,
      clientEphemeral: clientEphemeral,
      nodeEphemeral: nodeEphemeral,
      nodeIdentityPublicKey: nodeIdentity.publicKey,
      deviceIdentityPublicKey: deviceIdentity.publicKey,
    );
    final transcript = RelayE2eeHandshake.transcript(input);
    final deviceCredential = 'device-credential-128-bit-minimum';
    final deviceProof = await RelayE2eeHandshake.deviceProof(
      deviceCredential: deviceCredential,
      transcript: transcript,
    );
    final nodeSignature = await RelayDeviceIdentityStore.signWithPrivateScalar(
      privateScalar: nodeIdentity.privateScalar,
      transcript: transcript,
    );
    final deviceSignature =
        await RelayDeviceIdentityStore.signWithPrivateScalar(
      privateScalar: deviceIdentity.privateScalar,
      transcript: transcript,
    );

    await RelayE2eeHandshake.validateReconnectHandshake(
      input: input,
      deviceCredential: deviceCredential,
      deviceProof: deviceProof,
      nodeSignature: nodeSignature,
      deviceSignature: deviceSignature,
    );

    final traffic = await RelayE2eeHandshake.deriveTrafficKeys(
      privateScalar: clientEphemeral.privateScalar,
      remotePublicKey: nodeEphemeral.publicKey,
      input: input,
    );
    final rekeyedInput = _handshakeInput(
      kind: relayE2eeHandshakeKindReconnect,
      handshakeId: 'hs_reconnect_02',
      clientEphemeral: clientEphemeral,
      nodeEphemeral: nodeEphemeral,
      nodeIdentityPublicKey: nodeIdentity.publicKey,
      deviceIdentityPublicKey: deviceIdentity.publicKey,
    );
    final rekeyed = await RelayE2eeHandshake.deriveTrafficKeys(
      privateScalar: clientEphemeral.privateScalar,
      remotePublicKey: nodeEphemeral.publicKey,
      input: rekeyedInput,
    );
    expect(rekeyed.clientToAgent, isNot(traffic.clientToAgent));

    final badDeviceSignature = Uint8List.fromList(deviceSignature);
    badDeviceSignature[badDeviceSignature.length - 1] ^= 0x01;
    await expectLater(
      RelayE2eeHandshake.validateReconnectHandshake(
        input: input,
        deviceCredential: deviceCredential,
        deviceProof: deviceProof,
        nodeSignature: nodeSignature,
        deviceSignature: badDeviceSignature,
      ),
      throwsStateError,
    );
  });
}

RelayE2eeHandshakeInput _handshakeInput({
  required String kind,
  String? handshakeId,
  required RelayE2eeEphemeralKeyPair clientEphemeral,
  required RelayE2eeEphemeralKeyPair nodeEphemeral,
  required Uint8List nodeIdentityPublicKey,
  Uint8List? deviceIdentityPublicKey,
}) {
  return RelayE2eeHandshakeInput(
    kind: kind,
    sessionId: 'rs_handshake',
    clientId: 'rc_handshake',
    handshakeId: handshakeId ??
        (kind == relayE2eeHandshakeKindPairing
            ? 'hs_pairing_01'
            : 'hs_reconnect_01'),
    relayProtocolVersion: 1,
    e2eeProtocolVersion: relayE2eeVersion,
    tunnelProtocolVersion: 1,
    cryptoSuite: relayE2eeSuite,
    clientEphemeralPublicKey: clientEphemeral.publicKey,
    nodeEphemeralPublicKey: nodeEphemeral.publicKey,
    nodeIdentityPublicKey: nodeIdentityPublicKey,
    deviceIdentityPublicKey: deviceIdentityPublicKey ?? Uint8List(0),
    requiresE2EE: true,
    plaintextTestMode: false,
    supportsMultiplexStreams: true,
    supportsFileDownload: true,
    supportsDeviceManagement: true,
  );
}
