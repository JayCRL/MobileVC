import 'dart:math';
import 'dart:typed_data';

import 'package:flutter_test/flutter_test.dart';
import 'package:mobile_vc/core/relay_e2ee/relay_e2ee_capability.dart';
import 'package:mobile_vc/core/relay_e2ee/relay_e2ee_handshake.dart';
import 'package:mobile_vc/core/relay_e2ee/relay_e2ee_handshake_frames.dart';

void main() {
  test('pairing handshake frames round-trip required fields', () async {
    final clientEphemeral =
        await RelayE2eeHandshake.newEphemeralKeyPair(random: Random(1));
    final nodeEphemeral =
        await RelayE2eeHandshake.newEphemeralKeyPair(random: Random(2));
    final nodeIdentity =
        await RelayE2eeHandshake.newEphemeralKeyPair(random: Random(3));
    final capabilities = RelayE2eeCapabilitySet.production();

    final clientHello = RelayE2eeClientHelloFrame(
      sessionId: 'rs_frames',
      clientId: 'rc_frames',
      handshakeId: 'hs_frames',
      kind: relayE2eeHandshakeKindPairing,
      capabilities: capabilities,
      clientEphemeralPublicKey: clientEphemeral.publicKey,
    );
    final decodedClientHello = RelayE2eeClientHelloFrame.fromJson(
      clientHello.toJson(),
    );
    expect(
        decodedClientHello.clientEphemeralPublicKey, clientEphemeral.publicKey);

    final agentHello = RelayE2eeAgentHelloFrame(
      sessionId: 'rs_frames',
      clientId: 'rc_frames',
      handshakeId: 'hs_frames',
      capabilities: capabilities,
      nodeEphemeralPublicKey: nodeEphemeral.publicKey,
      nodeIdentityPublicKey: nodeIdentity.publicKey,
      nodeSignature: Uint8List.fromList([1, 2, 3]),
    );
    final decodedAgentHello = RelayE2eeAgentHelloFrame.fromJson(
      agentHello.toJson(),
    );
    expect(decodedAgentHello.nodeEphemeralPublicKey, nodeEphemeral.publicKey);

    final proof = RelayE2eeClientProofFrame(
      sessionId: 'rs_frames',
      clientId: 'rc_frames',
      handshakeId: 'hs_frames',
      kind: relayE2eeHandshakeKindPairing,
      pairingProof: Uint8List.fromList([4, 5, 6]),
    );
    expect(
      RelayE2eeClientProofFrame.fromJson(proof.toJson()).pairingProof,
      [4, 5, 6],
    );

    final result = RelayE2eeAgentResultFrame(
      sessionId: 'rs_frames',
      clientId: 'rc_frames',
      handshakeId: 'hs_frames',
      ok: true,
    );
    expect(RelayE2eeAgentResultFrame.fromJson(result.toJson()).ok, isTrue);
  });

  test('reconnect client hello requires device identity', () async {
    final clientEphemeral =
        await RelayE2eeHandshake.newEphemeralKeyPair(random: Random(4));
    final deviceIdentity =
        await RelayE2eeHandshake.newEphemeralKeyPair(random: Random(5));

    final frame = RelayE2eeClientHelloFrame(
      sessionId: 'rs_frames',
      clientId: 'rc_frames',
      handshakeId: 'hs_frames',
      kind: relayE2eeHandshakeKindReconnect,
      capabilities: RelayE2eeCapabilitySet.production(),
      clientEphemeralPublicKey: clientEphemeral.publicKey,
      deviceId: 'rd_frames',
      deviceIdentityPublicKey: deviceIdentity.publicKey,
    );
    expect(
      RelayE2eeClientHelloFrame.fromJson(frame.toJson())
          .deviceIdentityPublicKey,
      deviceIdentity.publicKey,
    );

    final json = frame.toJson()..remove('deviceIdentityPublicKey');
    expect(
      () => RelayE2eeClientHelloFrame.fromJson(json),
      throwsFormatException,
    );
  });

  test('rejects malformed handshake frames explicitly', () async {
    final clientEphemeral =
        await RelayE2eeHandshake.newEphemeralKeyPair(random: Random(6));
    final frame = RelayE2eeClientHelloFrame(
      sessionId: 'rs_frames',
      clientId: 'rc_frames',
      handshakeId: 'hs_frames',
      kind: relayE2eeHandshakeKindPairing,
      capabilities: RelayE2eeCapabilitySet.production(),
      clientEphemeralPublicKey: clientEphemeral.publicKey,
    ).toJson();

    expect(
      () => RelayE2eeClientHelloFrame.fromJson({
        ...frame,
        'clientEphemeralPublicKey': 'not base64',
      }),
      throwsFormatException,
    );
    expect(
      () =>
          RelayE2eeClientHelloFrame.fromJson({...frame, 'capabilities': null}),
      throwsFormatException,
    );
    expect(
      () => RelayE2eeClientHelloFrame.fromJson({
        ...frame,
        'capabilities': RelayE2eeCapabilitySet.plaintextTestMode().toJson(),
      }),
      throwsArgumentError,
    );

    expect(
      () => const RelayE2eeAgentResultFrame(
        sessionId: 'rs_frames',
        clientId: 'rc_frames',
        handshakeId: 'hs_frames',
        ok: false,
      ).toJson(),
      throwsFormatException,
    );
  });

  test('client proof rejects pairing and device field mixups', () {
    expect(
      () => const RelayE2eeClientProofFrame(
        sessionId: 'rs_frames',
        clientId: 'rc_frames',
        handshakeId: 'hs_frames',
        kind: relayE2eeHandshakeKindPairing,
        pairingProof: [1],
        deviceProof: [2],
      ).toJson(),
      throwsFormatException,
    );

    expect(
      () => const RelayE2eeClientProofFrame(
        sessionId: 'rs_frames',
        clientId: 'rc_frames',
        handshakeId: 'hs_frames',
        kind: relayE2eeHandshakeKindReconnect,
        pairingProof: [1],
        deviceProof: [2],
        deviceSignature: [3],
      ).toJson(),
      throwsFormatException,
    );
  });
}
