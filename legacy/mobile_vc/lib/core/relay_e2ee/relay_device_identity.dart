import 'dart:convert';
import 'dart:math';
import 'dart:typed_data';

import 'package:cryptography/cryptography.dart';
import 'package:flutter_secure_storage/flutter_secure_storage.dart';
import 'package:pointycastle/pointycastle.dart' as pc;

import 'relay_e2ee_crypto.dart';

abstract interface class RelaySecureStore {
  Future<String?> read(String key);

  Future<void> write(String key, String value);

  Future<void> delete(String key);
}

class FlutterRelaySecureStore implements RelaySecureStore {
  const FlutterRelaySecureStore({
    FlutterSecureStorage storage = const FlutterSecureStorage(),
  }) : _storage = storage;

  final FlutterSecureStorage _storage;

  @override
  Future<String?> read(String key) => _storage.read(key: key);

  @override
  Future<void> write(String key, String value) =>
      _storage.write(key: key, value: value);

  @override
  Future<void> delete(String key) => _storage.delete(key: key);
}

class RelayDeviceIdentity {
  const RelayDeviceIdentity({
    required this.privateScalar,
    required this.publicKey,
    required this.fingerprint,
  });

  final Uint8List privateScalar;
  final Uint8List publicKey;
  final Uint8List fingerprint;

  String get fullFingerprintHex =>
      RelayDeviceIdentityStore.fingerprintHex(fingerprint);

  String get shortFingerprint => RelayE2eeCrypto.shortFingerprint(fingerprint);
}

class RelayDeviceIdentityStore {
  RelayDeviceIdentityStore({
    required RelaySecureStore secureStore,
    Random? random,
  })  : _secureStore = secureStore,
        _random = random ?? Random.secure();

  static const _privateKeyName = 'mobilevc.relay.device_identity.v1';

  final RelaySecureStore _secureStore;
  final Random _random;
  final pc.ECDomainParameters _p256 = pc.ECDomainParameters('secp256r1');

  Future<RelayDeviceIdentity> loadOrCreate() async {
    final stored = await _secureStore.read(_privateKeyName);
    if (stored != null && stored.trim().isNotEmpty) {
      return _identityFromPrivate(_decodePrivateScalar(stored));
    }
    final identity = await _generate();
    await _secureStore.write(
      _privateKeyName,
      base64Url.encode(identity.privateScalar),
    );
    return identity;
  }

  Future<bool> hasStored() async {
    final stored = await _secureStore.read(_privateKeyName);
    return stored != null && stored.trim().isNotEmpty;
  }

  Future<void> reset() => _secureStore.delete(_privateKeyName);

  Future<Uint8List> signTranscript({
    required RelayDeviceIdentity identity,
    required Uint8List transcript,
  }) async {
    return signWithPrivateScalar(
      privateScalar: identity.privateScalar,
      transcript: transcript,
    );
  }

  static Future<Uint8List> signWithPrivateScalar({
    required Uint8List privateScalar,
    required Uint8List transcript,
  }) async {
    final p256 = pc.ECDomainParameters('secp256r1');
    final signer = pc.Signer('SHA-256/DET-ECDSA');
    signer.init(
      true,
      pc.PrivateKeyParameter<pc.PrivateKey>(
        pc.ECPrivateKey(
          _bytesToBigInt(privateScalar),
          p256,
        ),
      ),
    );
    final signature = signer.generateSignature(transcript) as pc.ECSignature;
    return _encodeSignature(signature);
  }

  Future<bool> verifyTranscriptSignature({
    required Uint8List publicKey,
    required Uint8List transcript,
    required Uint8List signature,
  }) async {
    return verifyWithPublicKey(
      publicKey: publicKey,
      transcript: transcript,
      signature: signature,
    );
  }

  static Future<bool> verifyWithPublicKey({
    required Uint8List publicKey,
    required Uint8List transcript,
    required Uint8List signature,
  }) async {
    final p256 = pc.ECDomainParameters('secp256r1');
    final point = p256.curve.decodePoint(publicKey);
    if (point == null || point.isInfinity) {
      throw ArgumentError.value(publicKey, 'publicKey', 'invalid public key');
    }
    final signer = pc.Signer('SHA-256/DET-ECDSA');
    signer.init(
      false,
      pc.PublicKeyParameter<pc.PublicKey>(pc.ECPublicKey(point, p256)),
    );
    return signer.verifySignature(
      transcript,
      _decodeSignature(signature),
    );
  }

  Future<RelayDeviceIdentity> _generate() async {
    while (true) {
      final scalar = _randomBytes(relayE2eeKeyLength);
      final value = _bytesToBigInt(scalar);
      if (value == BigInt.zero || value >= _p256.n) {
        continue;
      }
      return _identityFromPrivate(scalar);
    }
  }

  Future<RelayDeviceIdentity> _identityFromPrivate(
    Uint8List privateScalar,
  ) async {
    final publicKey = RelayE2eeCrypto.publicKeyFromPrivate(privateScalar);
    final fingerprint = await RelayE2eeCrypto.fingerprint(publicKey);
    return RelayDeviceIdentity(
      privateScalar: privateScalar,
      publicKey: publicKey,
      fingerprint: fingerprint,
    );
  }

  Uint8List _randomBytes(int length) {
    final bytes = Uint8List(length);
    for (var i = 0; i < bytes.length; i++) {
      bytes[i] = _random.nextInt(256);
    }
    return bytes;
  }

  static Uint8List _decodePrivateScalar(String stored) {
    try {
      return Uint8List.fromList(base64Url.decode(stored));
    } on FormatException catch (error) {
      throw FormatException('invalid relay device identity', error.source);
    }
  }

  static Uint8List _encodeSignature(pc.ECSignature signature) {
    return Uint8List.fromList([
      ..._bigIntToFixedBytes(signature.r, relayE2eeKeyLength),
      ..._bigIntToFixedBytes(signature.s, relayE2eeKeyLength),
    ]);
  }

  static pc.ECSignature _decodeSignature(Uint8List signature) {
    if (signature.length != relayE2eeKeyLength * 2) {
      throw ArgumentError.value(signature, 'signature', 'invalid signature');
    }
    return pc.ECSignature(
      _bytesToBigInt(signature.sublist(0, relayE2eeKeyLength)),
      _bytesToBigInt(signature.sublist(relayE2eeKeyLength)),
    );
  }

  static BigInt _bytesToBigInt(Uint8List bytes) {
    var result = BigInt.zero;
    for (final byte in bytes) {
      result = (result << 8) | BigInt.from(byte);
    }
    return result;
  }

  static Uint8List _bigIntToFixedBytes(BigInt value, int length) {
    final bytes = Uint8List(length);
    var remaining = value;
    for (var i = length - 1; i >= 0; i--) {
      bytes[i] = (remaining & BigInt.from(0xff)).toInt();
      remaining >>= 8;
    }
    if (remaining != BigInt.zero) {
      throw ArgumentError.value(value, 'value', 'integer exceeds byte length');
    }
    return bytes;
  }

  static String fingerprintHex(List<int> bytes) {
    const digits = '0123456789abcdef';
    final buffer = StringBuffer();
    for (final byte in bytes) {
      buffer
        ..write(digits[byte >> 4])
        ..write(digits[byte & 0x0f]);
    }
    return buffer.toString();
  }
}

class RelayDeviceCredential {
  const RelayDeviceCredential(this.value);

  final String value;

  Future<String> hash() => RelayDeviceCredentialStore.hashCredential(value);
}

class RelayDeviceCredentialStore {
  RelayDeviceCredentialStore({
    required RelaySecureStore secureStore,
    Random? random,
  })  : _secureStore = secureStore,
        _random = random ?? Random.secure();

  static const _credentialName = 'mobilevc.relay.device_credential.v1';

  final RelaySecureStore _secureStore;
  final Random _random;

  Future<RelayDeviceCredential> loadOrCreate() async {
    final stored = await _secureStore.read(_credentialName);
    if (stored != null && stored.trim().isNotEmpty) {
      return RelayDeviceCredential(stored);
    }
    final credential = RelayDeviceCredential(_newCredential());
    await _secureStore.write(_credentialName, credential.value);
    return credential;
  }

  Future<bool> hasStored() async {
    final stored = await _secureStore.read(_credentialName);
    return stored != null && stored.trim().isNotEmpty;
  }

  Future<void> reset() => _secureStore.delete(_credentialName);

  String _newCredential() {
    final bytes = Uint8List(relayE2eeKeyLength);
    for (var i = 0; i < bytes.length; i++) {
      bytes[i] = _random.nextInt(256);
    }
    return base64Url.encode(bytes).replaceAll('=', '');
  }

  static Future<String> hashCredential(String credential) async {
    final hash = await Sha256().hash(utf8.encode(credential));
    return base64Url.encode(hash.bytes).replaceAll('=', '');
  }
}
