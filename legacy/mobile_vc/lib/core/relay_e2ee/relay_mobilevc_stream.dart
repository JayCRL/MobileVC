import 'dart:convert';
import 'dart:typed_data';

import 'relay_e2ee_crypto.dart';
import 'relay_e2ee_handshake.dart';
import 'relay_tunnel.dart';

const relayMobileVcStreamId = 1;
const relayMobileVcStreamType = tunnelStreamMobileVcWs;
const relayForwardType = 'relay.forward';
const relayForwardContentTypeMobileVc = 'mobilevc.ws.v1';
const relayForwardPayloadBase64Url = 'base64url';

class RelayMobileVcStreamCodec {
  RelayMobileVcStreamCodec({
    required this.sessionId,
    required this.clientId,
    required this.handshakeId,
    required this.sendKey,
    required this.receiveKey,
    required this.sendDirection,
  }) : receiveDirection = _oppositeDirection(sendDirection) {
    if (sendKey.length != relayE2eeKeyLength ||
        receiveKey.length != relayE2eeKeyLength) {
      throw ArgumentError('invalid E2EE stream key length');
    }
    if (sessionId.trim().isEmpty ||
        clientId.trim().isEmpty ||
        handshakeId.trim().isEmpty) {
      throw ArgumentError('session, client, and handshake ids are required');
    }
  }

  factory RelayMobileVcStreamCodec.client({
    required String sessionId,
    required String clientId,
    required String handshakeId,
    required RelayE2eeTrafficKeys keys,
  }) {
    return RelayMobileVcStreamCodec(
      sessionId: sessionId,
      clientId: clientId,
      handshakeId: handshakeId,
      sendKey: keys.clientToAgent,
      receiveKey: keys.agentToClient,
      sendDirection: relayE2eeDirectionClientToAgent,
    );
  }

  factory RelayMobileVcStreamCodec.agent({
    required String sessionId,
    required String clientId,
    required String handshakeId,
    required RelayE2eeTrafficKeys keys,
  }) {
    return RelayMobileVcStreamCodec(
      sessionId: sessionId,
      clientId: clientId,
      handshakeId: handshakeId,
      sendKey: keys.agentToClient,
      receiveKey: keys.clientToAgent,
      sendDirection: relayE2eeDirectionAgentToClient,
    );
  }

  final String sessionId;
  final String clientId;
  final String handshakeId;
  final Uint8List sendKey;
  final Uint8List receiveKey;
  final String sendDirection;
  final String receiveDirection;
  final Map<int, int> _sendCounters = <int, int>{};
  final Map<int, Set<int>> _seenCountersByStream = <int, Set<int>>{};
  final Map<int, Set<int>> _pendingCountersByStream = <int, Set<int>>{};

  Future<Map<String, dynamic>> encodeJson({
    required String messageId,
    required Map<String, dynamic> payload,
  }) async {
    return encode(
      messageId: messageId,
      plaintext: Uint8List.fromList(utf8.encode(jsonEncode(payload))),
    );
  }

  Future<Map<String, dynamic>> encode({
    required String messageId,
    required Uint8List plaintext,
    int streamId = relayMobileVcStreamId,
  }) async {
    if (messageId.trim().isEmpty) {
      throw ArgumentError('message id is required');
    }
    if (streamId == 0) {
      throw ArgumentError('stream id is required');
    }
    final counter = _sendCounters[streamId] ?? 0;
    _sendCounters[streamId] = counter + 1;
    final sealed = await RelayE2eeCrypto.encrypt(
      key: sendKey,
      plaintext: plaintext,
      context: _frameContext(sendDirection, streamId, counter),
    );
    return <String, dynamic>{
      'type': relayForwardType,
      'version': 1,
      'sessionId': sessionId,
      'clientId': clientId,
      'direction': sendDirection,
      'messageId': messageId,
      'contentType': relayForwardContentTypeMobileVc,
      'encryption': relayE2eeSuite,
      'payloadEncoding': relayForwardPayloadBase64Url,
      'payload': base64Url.encode(sealed).replaceAll('=', ''),
      'streamId': streamId,
      'counter': counter,
      'handshakeId': handshakeId,
    };
  }

  Future<Map<String, dynamic>> decodeJson(Map<String, dynamic> frame) async {
    final plaintext = await decode(frame);
    final decoded = jsonDecode(utf8.decode(plaintext));
    if (decoded is! Map<String, dynamic>) {
      throw const FormatException(
          'MobileVC relay payload is not a JSON object');
    }
    return decoded;
  }

  Future<Uint8List> decode(Map<String, dynamic> frame) async {
    _validateFrame(frame);
    if (_intField(frame, 'streamId') != relayMobileVcStreamId) {
      throw const FormatException('invalid MobileVC E2EE relay frame');
    }
    return decodeStream(frame);
  }

  Future<Uint8List> decodeStream(Map<String, dynamic> frame) async {
    _validateFrame(frame);
    final streamId = _intField(frame, 'streamId');
    final counter = _intField(frame, 'counter');
    final seenCounters =
        _seenCountersByStream.putIfAbsent(streamId, () => <int>{});
    final pendingCounters =
        _pendingCountersByStream.putIfAbsent(streamId, () => <int>{});
    if (seenCounters.contains(counter) || pendingCounters.contains(counter)) {
      throw StateError('e2ee replay detected');
    }
    pendingCounters.add(counter);
    final sealed = Uint8List.fromList(
      base64Url.decode(base64Url.normalize(frame['payload'].toString())),
    );
    try {
      final plaintext = await RelayE2eeCrypto.decrypt(
        key: receiveKey,
        sealed: sealed,
        context: _frameContext(receiveDirection, streamId, counter),
      );
      seenCounters.add(counter);
      return plaintext;
    } finally {
      pendingCounters.remove(counter);
    }
  }

  Future<Map<String, dynamic>> encodeTunnelFrame({
    required String messageId,
    required RelayTunnelFrame frame,
  }) {
    final raw = Uint8List.fromList(utf8.encode(jsonEncode(frame.toJson())));
    return encode(
      messageId: messageId,
      plaintext: raw,
      streamId: frame.streamId,
    );
  }

  Future<RelayTunnelFrame> decodeTunnelFrame(Map<String, dynamic> frame) async {
    final raw = await decodeStream(frame);
    final decoded = jsonDecode(utf8.decode(raw));
    if (decoded is! Map<String, dynamic>) {
      throw const FormatException('Relay tunnel payload is not a JSON object');
    }
    return RelayTunnelFrame.fromJson(decoded);
  }

  RelayE2eeFrameContext _frameContext(
    String direction,
    int streamId,
    int counter,
  ) {
    return RelayE2eeFrameContext(
      sessionId: sessionId,
      clientId: clientId,
      handshakeId: handshakeId,
      direction: direction,
      streamId: streamId,
      counter: counter,
    );
  }

  void _validateFrame(Map<String, dynamic> frame) {
    final valid = frame['type'] == relayForwardType &&
        frame['version'] == 1 &&
        frame['sessionId'] == sessionId &&
        frame['clientId'] == clientId &&
        frame['direction'] == receiveDirection &&
        frame['contentType'] == relayForwardContentTypeMobileVc &&
        frame['encryption'] == relayE2eeSuite &&
        frame['payloadEncoding'] == relayForwardPayloadBase64Url &&
        frame['handshakeId'] == handshakeId &&
        _intField(frame, 'streamId') != 0 &&
        frame['messageId'].toString().trim().isNotEmpty &&
        frame['payload'].toString().trim().isNotEmpty;
    if (!valid) {
      throw const FormatException('invalid MobileVC E2EE relay frame');
    }
  }

  static int _intField(Map<String, dynamic> frame, String key) {
    final value = frame[key];
    if (value is int) {
      return value;
    }
    if (value is num) {
      return value.toInt();
    }
    return int.parse(value.toString());
  }

  static String _oppositeDirection(String direction) {
    return switch (direction) {
      relayE2eeDirectionClientToAgent => relayE2eeDirectionAgentToClient,
      relayE2eeDirectionAgentToClient => relayE2eeDirectionClientToAgent,
      _ =>
        throw ArgumentError.value(direction, 'direction', 'invalid direction'),
    };
  }
}
