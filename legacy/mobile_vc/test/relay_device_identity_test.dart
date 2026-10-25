import 'dart:math';
import 'dart:typed_data';

import 'package:flutter_test/flutter_test.dart';
import 'package:mobile_vc/core/relay_e2ee/relay_device_identity.dart';

void main() {
  test('loadOrCreate persists the same private identity in secure store',
      () async {
    final store = MemoryRelaySecureStore();
    final identities = RelayDeviceIdentityStore(
      secureStore: store,
      random: Random(7),
    );

    final first = await identities.loadOrCreate();
    final second = await identities.loadOrCreate();

    expect(second.privateScalar, first.privateScalar);
    expect(second.publicKey, first.publicKey);
    expect(second.fingerprint, first.fingerprint);
    expect(first.publicKey, hasLength(65));
    expect(first.fingerprint, hasLength(32));
    expect(first.shortFingerprint.split('-'), hasLength(5));
    expect(store.values.keys, ['mobilevc.relay.device_identity.v1']);
  });

  test('reset deletes the persisted device identity', () async {
    final store = MemoryRelaySecureStore();
    final identities = RelayDeviceIdentityStore(
      secureStore: store,
      random: Random(11),
    );

    final first = await identities.loadOrCreate();
    await identities.reset();
    final second = await identities.loadOrCreate();

    expect(second.privateScalar, isNot(first.privateScalar));
  });

  test('device identity signs and verifies handshake transcripts', () async {
    final identities = RelayDeviceIdentityStore(
      secureStore: MemoryRelaySecureStore(),
      random: Random(13),
    );
    final identity = await identities.loadOrCreate();
    final transcript = Uint8List.fromList(
      'mobilevc relay device handshake transcript'.codeUnits,
    );

    final signature = await identities.signTranscript(
      identity: identity,
      transcript: transcript,
    );

    final verified = await identities.verifyTranscriptSignature(
      publicKey: identity.publicKey,
      transcript: transcript,
      signature: signature,
    );
    expect(verified, isTrue);

    final tampered = await identities.verifyTranscriptSignature(
      publicKey: identity.publicKey,
      transcript: Uint8List.fromList('tampered'.codeUnits),
      signature: signature,
    );
    expect(tampered, isFalse);
  });

  test('device credential persists only in secure store and hashes stably',
      () async {
    final store = MemoryRelaySecureStore();
    final credentials = RelayDeviceCredentialStore(
      secureStore: store,
      random: Random(19),
    );

    final first = await credentials.loadOrCreate();
    final second = await credentials.loadOrCreate();
    final firstHash = await first.hash();
    final secondHash =
        await RelayDeviceCredentialStore.hashCredential(second.value);

    expect(second.value, first.value);
    expect(first.value, isNot(firstHash));
    expect(firstHash, secondHash);
    expect(store.values.keys, ['mobilevc.relay.device_credential.v1']);
  });

  test('reset deletes persisted device credential', () async {
    final store = MemoryRelaySecureStore();
    final credentials = RelayDeviceCredentialStore(
      secureStore: store,
      random: Random(23),
    );

    final first = await credentials.loadOrCreate();
    await credentials.reset();
    final second = await credentials.loadOrCreate();

    expect(second.value, isNot(first.value));
  });
}

class MemoryRelaySecureStore implements RelaySecureStore {
  final Map<String, String> values = <String, String>{};

  @override
  Future<String?> read(String key) async => values[key];

  @override
  Future<void> write(String key, String value) async {
    values[key] = value;
  }

  @override
  Future<void> delete(String key) async {
    values.remove(key);
  }
}
