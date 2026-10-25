import 'dart:typed_data';

import 'package:flutter_test/flutter_test.dart';
import 'package:mobile_vc/core/relay_e2ee/relay_e2ee_crypto.dart';
import 'package:mobile_vc/core/relay_e2ee/relay_e2ee_handshake.dart';
import 'package:mobile_vc/core/relay_e2ee/relay_file_download.dart';
import 'package:mobile_vc/core/relay_e2ee/relay_mobilevc_stream.dart';

void main() {
  test('encrypts MobileVC JSON without leaking plaintext', () async {
    final (client, agent) = _codecs();
    final frame = await client.encodeJson(
      messageId: 'msg_1',
      payload: <String, dynamic>{
        'type': 'user_message',
        'text': 'secret command',
      },
    );

    expect(frame['encryption'], relayE2eeSuite);
    expect(frame['streamId'], relayMobileVcStreamId);
    expect(frame['counter'], 0);
    expect(frame['payload'].toString(), isNot(contains('secret')));

    final decoded = await agent.decodeJson(frame);
    expect(decoded['text'], 'secret command');
  });

  test('rejects replay and metadata tampering', () async {
    final (client, agent) = _codecs();
    final frame = await client.encode(
      messageId: 'msg_1',
      plaintext: Uint8List.fromList('{"type":"x"}'.codeUnits),
    );

    await agent.decode(frame);
    await expectLater(agent.decode(frame), throwsStateError);

    final (_, freshAgent) = _codecs();
    final streamTampered = Map<String, dynamic>.of(frame);
    streamTampered['streamId'] = 2;
    await expectLater(freshAgent.decode(streamTampered), throwsFormatException);

    final directionTampered = Map<String, dynamic>.of(frame);
    directionTampered['direction'] = relayE2eeDirectionAgentToClient;
    await expectLater(
      freshAgent.decode(directionTampered),
      throwsFormatException,
    );
  });

  test('frame shape matches relay.forward metadata', () async {
    final (client, _) = _codecs();
    final frame = await client.encode(
      messageId: 'msg_1',
      plaintext: Uint8List.fromList('{"type":"x"}'.codeUnits),
    );

    for (final key in <String>[
      'type',
      'version',
      'sessionId',
      'clientId',
      'direction',
      'messageId',
      'contentType',
      'encryption',
      'payloadEncoding',
      'payload',
      'streamId',
      'counter',
      'handshakeId',
    ]) {
      expect(frame, contains(key));
    }
  });

  test('tunnel frame streams use independent counters', () async {
    final (client, agent) = _codecs();
    final open = relayFileDownloadOpenFrame(
      streamId: 43,
      metadata: const RelayFileDownloadMetadata(path: '/workspace/a.txt'),
      window: 4,
    );
    final first = await client.encodeTunnelFrame(
      messageId: 'msg_download_1',
      frame: open,
    );
    final second = await client.encodeJson(
      messageId: 'msg_mobilevc_1',
      payload: <String, dynamic>{'type': 'ping'},
    );

    expect(first['streamId'], 43);
    expect(first['counter'], 0);
    expect(second['streamId'], relayMobileVcStreamId);
    expect(second['counter'], 0);
    expect(first['payload'].toString(), isNot(contains('/workspace/a.txt')));

    final decoded = await agent.decodeTunnelFrame(first);
    expect(decoded.streamId, 43);
    expect(decoded.metadata['path'], '/workspace/a.txt');
    expect((await agent.decodeJson(second))['type'], 'ping');
  });
}

(RelayMobileVcStreamCodec, RelayMobileVcStreamCodec) _codecs() {
  final keys = RelayE2eeTrafficKeys(
    clientToAgent: _hex(
      'fe783fd2cb680d136b04d39b0e7c605826e418afd88c3819838bb8e57738144d',
    ),
    agentToClient: _hex(
      '51cc1a21ce5b5098780c55c7e0487e4e413347bf11114e942e682c890aac7209',
    ),
  );
  return (
    RelayMobileVcStreamCodec.client(
      sessionId: 'rs_stream',
      clientId: 'rc_stream',
      handshakeId: 'hs_stream_01',
      keys: keys,
    ),
    RelayMobileVcStreamCodec.agent(
      sessionId: 'rs_stream',
      clientId: 'rc_stream',
      handshakeId: 'hs_stream_01',
      keys: keys,
    ),
  );
}

Uint8List _hex(String value) {
  final bytes = <int>[];
  for (var i = 0; i < value.length; i += 2) {
    bytes.add(int.parse(value.substring(i, i + 2), radix: 16));
  }
  return Uint8List.fromList(bytes);
}
