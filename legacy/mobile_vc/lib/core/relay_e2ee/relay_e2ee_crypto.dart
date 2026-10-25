import 'dart:convert';
import 'dart:typed_data';

import 'package:cryptography/cryptography.dart';
import 'package:pointycastle/pointycastle.dart' as pc;

const relayE2eeSuite = 'p256-ecdsa+p256-ecdh+hkdf-sha256+aes-256-gcm';
const relayE2eeVersion = 1;
const relayE2eeNonceLength = 12;
const relayE2eeKeyLength = 32;
const relayE2eeDirectionClientToAgent = 'client_to_agent';
const relayE2eeDirectionAgentToClient = 'agent_to_client';

class RelayE2eeFrameContext {
  const RelayE2eeFrameContext({
    required this.sessionId,
    required this.clientId,
    required this.handshakeId,
    required this.direction,
    required this.streamId,
    required this.counter,
  });

  final String sessionId;
  final String clientId;
  final String handshakeId;
  final String direction;
  final int streamId;
  final int counter;
}

class RelayE2eeCrypto {
  static final pc.ECDomainParameters _p256 = pc.ECDomainParameters('secp256r1');
  static final AesGcm _aesGcm = AesGcm.with256bits();

  static Uint8List publicKeyFromPrivate(Uint8List privateScalar) {
    _validatePrivateScalar(privateScalar);
    final point = _p256.G * _bytesToBigInt(privateScalar);
    if (point == null || point.isInfinity) {
      throw ArgumentError.value(privateScalar, 'privateScalar',
          'invalid P-256 private key produced infinity');
    }
    return Uint8List.fromList(point.getEncoded(false));
  }

  static Uint8List sharedSecret({
    required Uint8List privateScalar,
    required Uint8List remotePublicKey,
  }) {
    _validatePrivateScalar(privateScalar);
    final point = _p256.curve.decodePoint(remotePublicKey);
    if (point == null || point.isInfinity) {
      throw ArgumentError.value(
        remotePublicKey,
        'remotePublicKey',
        'invalid P-256 public key',
      );
    }
    final agreement = pc.ECDHBasicAgreement()
      ..init(pc.ECPrivateKey(_bytesToBigInt(privateScalar), _p256));
    final value = agreement.calculateAgreement(pc.ECPublicKey(point, _p256));
    return _bigIntToFixedBytes(value, relayE2eeKeyLength);
  }

  static Future<Uint8List> fingerprint(Uint8List publicKey) async {
    final hash = await Sha256().hash(publicKey);
    return Uint8List.fromList(hash.bytes);
  }

  static String shortFingerprint(Uint8List fingerprint) {
    final encoded = _base32NoPadding(fingerprint);
    final prefix = encoded.length > 20 ? encoded.substring(0, 20) : encoded;
    final groups = <String>[];
    for (var i = 0; i < prefix.length; i += 4) {
      final end = i + 4 > prefix.length ? prefix.length : i + 4;
      groups.add(prefix.substring(i, end));
    }
    return groups.join('-');
  }

  static Future<Uint8List> trafficSalt(RelayE2eeFrameContext context) async {
    final hash = await Sha256().hash(
      utf8.encode(
        '${context.sessionId}|${context.clientId}|${context.handshakeId}',
      ),
    );
    return Uint8List.fromList(hash.bytes);
  }

  static Future<Uint8List> deriveTrafficKey({
    required Uint8List sharedSecret,
    required RelayE2eeFrameContext context,
  }) async {
    _validateContext(context);
    final hkdf = Hkdf(hmac: Hmac.sha256(), outputLength: relayE2eeKeyLength);
    final salt = await trafficSalt(context);
    final key = await hkdf.deriveKey(
      secretKey: SecretKey(sharedSecret),
      nonce: salt,
      info: utf8.encode(
        'mobilevc relay e2ee traffic v1|$relayE2eeSuite|${context.direction}',
      ),
    );
    return Uint8List.fromList(await key.extractBytes());
  }

  static Future<Uint8List> nonce(RelayE2eeFrameContext context) async {
    _validateContext(context);
    final bytes = BytesBuilder(copy: false)
      ..add(utf8.encode('mobilevc relay e2ee nonce v1'))
      ..addByte(0)
      ..add(utf8.encode(context.handshakeId))
      ..addByte(0)
      ..add(utf8.encode(context.direction))
      ..add(_uint64(context.streamId))
      ..add(_uint64(context.counter));
    final hash = await Sha256().hash(bytes.takeBytes());
    return Uint8List.fromList(hash.bytes.take(relayE2eeNonceLength).toList());
  }

  static Uint8List aad(RelayE2eeFrameContext context) {
    _validateContext(context);
    final bytes = BytesBuilder(copy: false)
      ..add(utf8.encode('MVCE'))
      ..add(_uint16(relayE2eeVersion));
    for (final value in [
      relayE2eeSuite,
      context.sessionId,
      context.clientId,
      context.handshakeId,
      context.direction,
    ]) {
      bytes.add(_lengthPrefixed(value));
    }
    bytes
      ..add(_uint64(context.streamId))
      ..add(_uint64(context.counter));
    return bytes.takeBytes();
  }

  static Future<Uint8List> encrypt({
    required Uint8List key,
    required Uint8List plaintext,
    required RelayE2eeFrameContext context,
  }) async {
    _validateKey(key);
    final secretBox = await _aesGcm.encrypt(
      plaintext,
      secretKey: SecretKey(key),
      nonce: await nonce(context),
      aad: aad(context),
    );
    return Uint8List.fromList([
      ...secretBox.cipherText,
      ...secretBox.mac.bytes,
    ]);
  }

  static Future<Uint8List> decrypt({
    required Uint8List key,
    required Uint8List sealed,
    required RelayE2eeFrameContext context,
  }) async {
    _validateKey(key);
    if (sealed.length < 16) {
      throw ArgumentError.value(sealed, 'sealed', 'missing AES-GCM tag');
    }
    final tagOffset = sealed.length - 16;
    final secretBox = SecretBox(
      sealed.sublist(0, tagOffset),
      nonce: await nonce(context),
      mac: Mac(sealed.sublist(tagOffset)),
    );
    final plaintext = await _aesGcm.decrypt(
      secretBox,
      secretKey: SecretKey(key),
      aad: aad(context),
    );
    return Uint8List.fromList(plaintext);
  }

  static void _validateContext(RelayE2eeFrameContext context) {
    final validDirection =
        context.direction == relayE2eeDirectionClientToAgent ||
            context.direction == relayE2eeDirectionAgentToClient;
    if (!validDirection) {
      throw ArgumentError.value(
        context.direction,
        'direction',
        'invalid E2EE direction',
      );
    }
    if (context.streamId < 0 || context.counter < 0) {
      throw ArgumentError('streamId and counter must be non-negative');
    }
  }

  static void _validateKey(Uint8List key) {
    if (key.length != relayE2eeKeyLength) {
      throw ArgumentError.value(key, 'key', 'invalid AES-256 key length');
    }
  }

  static void _validatePrivateScalar(Uint8List privateScalar) {
    if (privateScalar.length != relayE2eeKeyLength) {
      throw ArgumentError.value(
        privateScalar,
        'privateScalar',
        'invalid P-256 private scalar length',
      );
    }
    final value = _bytesToBigInt(privateScalar);
    if (value <= BigInt.zero || value >= _p256.n) {
      throw ArgumentError.value(
        privateScalar,
        'privateScalar',
        'invalid P-256 private scalar range',
      );
    }
  }

  static Uint8List _lengthPrefixed(String value) {
    final encoded = utf8.encode(value);
    if (encoded.length > 0xffff) {
      throw ArgumentError.value(value, 'value', 'AAD field is too large');
    }
    return Uint8List.fromList([..._uint16(encoded.length), ...encoded]);
  }

  static Uint8List _uint16(int value) {
    final bytes = ByteData(2)..setUint16(0, value);
    return bytes.buffer.asUint8List();
  }

  static Uint8List _uint64(int value) {
    final bytes = ByteData(8)..setUint64(0, value);
    return bytes.buffer.asUint8List();
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

  static String _base32NoPadding(Uint8List bytes) {
    const alphabet = 'ABCDEFGHIJKLMNOPQRSTUVWXYZ234567';
    final buffer = StringBuffer();
    var bits = 0;
    var value = 0;
    for (final byte in bytes) {
      value = (value << 8) | byte;
      bits += 8;
      while (bits >= 5) {
        buffer.write(alphabet[(value >> (bits - 5)) & 0x1f]);
        bits -= 5;
      }
    }
    if (bits > 0) {
      buffer.write(alphabet[(value << (5 - bits)) & 0x1f]);
    }
    return buffer.toString();
  }
}
