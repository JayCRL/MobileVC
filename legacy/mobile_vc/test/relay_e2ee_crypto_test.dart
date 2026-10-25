import 'dart:convert';
import 'dart:typed_data';

import 'package:cryptography/cryptography.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:mobile_vc/core/relay_e2ee/relay_e2ee_crypto.dart';

void main() {
  const context = RelayE2eeFrameContext(
    sessionId: 'rs_test_vector',
    clientId: 'rc_test_client',
    handshakeId: 'hs_test_vector_01',
    direction: relayE2eeDirectionClientToAgent,
    streamId: 7,
    counter: 42,
  );

  test('matches Go cross-language vector', () async {
    final clientPrivate = _hex(
      '201f1e1d1c1b1a191817161514131211100f0e0d0c0b0a090807060504030201',
    );
    final agentPrivate = _hex(
      '2f2e2d2c2b2a292827262524232221201f1e1d1c1b1a19181716151413121110',
    );

    final clientPublic = RelayE2eeCrypto.publicKeyFromPrivate(clientPrivate);
    final agentPublic = RelayE2eeCrypto.publicKeyFromPrivate(agentPrivate);

    expect(
      _toHex(clientPublic),
      '0421e184d5162d8a4d59f7d99fa819f84f0b6b162339ec1859c78f77362e37c28ff9289adbfe3f2a462e1043cd661a56bc7ded65a454b1c9e3f88bc47e2d1e8bf1',
    );
    expect(
      _toHex(agentPublic),
      '04b0c9b23dbe2da93634265119a5f60ff0e0ff38695b6214b4bc934a4fe8a43124bbdfe38eb01ecd82ffa5b6dc3d139f4c5f2bc579e8cb8ff24a317bf5f5fca859',
    );

    final clientShared = RelayE2eeCrypto.sharedSecret(
      privateScalar: clientPrivate,
      remotePublicKey: agentPublic,
    );
    final agentShared = RelayE2eeCrypto.sharedSecret(
      privateScalar: agentPrivate,
      remotePublicKey: clientPublic,
    );

    expect(clientShared, agentShared);
    expect(
      _toHex(clientShared),
      '3731680e7b78859914321fa055572ebbfe67a819cbf50ae4e412258d20667d25',
    );

    final salt = await RelayE2eeCrypto.trafficSalt(context);
    expect(
      _toHex(salt),
      'b9b61603d86e47979f5f5d27b7bed551e29c1979a485d55236c299285f853f33',
    );

    final key = await RelayE2eeCrypto.deriveTrafficKey(
      sharedSecret: clientShared,
      context: context,
    );
    expect(
      _toHex(key),
      'fe783fd2cb680d136b04d39b0e7c605826e418afd88c3819838bb8e57738144d',
    );

    final nonce = await RelayE2eeCrypto.nonce(context);
    expect(_toHex(nonce), 'c7aa9ffa41338991891dada0');

    final aad = RelayE2eeCrypto.aad(context);
    expect(
      _toHex(aad),
      '4d5643450001002c703235362d65636473612b703235362d656364682b686b64662d7368613235362b6165732d3235362d67636d000e72735f746573745f766563746f72000e72635f746573745f636c69656e74001168735f746573745f766563746f725f3031000f636c69656e745f746f5f6167656e740000000000000007000000000000002a',
    );

    final sealed = await RelayE2eeCrypto.encrypt(
      key: key,
      plaintext: Uint8List.fromList(
        utf8.encode('mobilevc relay e2ee vector payload'),
      ),
      context: context,
    );
    expect(
      _toHex(sealed),
      '7631c834d3631978cd53028ba4ab870585bcc6301b31c79b49e03d062343b411b3f907b104581d2abb56b5c5ce44b2c344aa',
    );

    final plaintext = await RelayE2eeCrypto.decrypt(
      key: key,
      sealed: sealed,
      context: context,
    );
    expect(utf8.decode(plaintext), 'mobilevc relay e2ee vector payload');

    final fingerprint = await RelayE2eeCrypto.fingerprint(agentPublic);
    expect(
      _toHex(fingerprint),
      '4e7d5371dd01c77385af5e2cad89a989671481cf162b803dbcdf5a7585b321a0',
    );
    expect(
      RelayE2eeCrypto.shortFingerprint(fingerprint),
      'JZ6V-G4O5-AHDX-HBNP-LYWK',
    );
  });

  test('decrypt rejects tampered ciphertext', () async {
    final key = _hex(
      'fe783fd2cb680d136b04d39b0e7c605826e418afd88c3819838bb8e57738144d',
    );
    final sealed = _hex(
      '7631c834d3631978cd53028ba4ab870585bcc6301b31c79b49e03d062343b411b3f907b104581d2abb56b5c5ce44b2c344aa',
    );
    sealed[0] ^= 0x01;

    await expectLater(
      RelayE2eeCrypto.decrypt(key: key, sealed: sealed, context: context),
      throwsA(isA<SecretBoxAuthenticationError>()),
    );
  });

  test('invalid context and key lengths fail explicitly', () async {
    const invalidContext = RelayE2eeFrameContext(
      sessionId: 'rs_test_vector',
      clientId: 'rc_test_client',
      handshakeId: 'hs_test_vector_01',
      direction: 'client-to-agent',
      streamId: 7,
      counter: 42,
    );

    await expectLater(
      RelayE2eeCrypto.nonce(invalidContext),
      throwsArgumentError,
    );
    await expectLater(
      RelayE2eeCrypto.encrypt(
        key: Uint8List.fromList([1, 2, 3]),
        plaintext: Uint8List(0),
        context: context,
      ),
      throwsArgumentError,
    );
  });
}

Uint8List _hex(String value) {
  final bytes = <int>[];
  for (var i = 0; i < value.length; i += 2) {
    bytes.add(int.parse(value.substring(i, i + 2), radix: 16));
  }
  return Uint8List.fromList(bytes);
}

String _toHex(List<int> bytes) {
  const digits = '0123456789abcdef';
  final buffer = StringBuffer();
  for (final byte in bytes) {
    buffer
      ..write(digits[byte >> 4])
      ..write(digits[byte & 0x0f]);
  }
  return buffer.toString();
}
