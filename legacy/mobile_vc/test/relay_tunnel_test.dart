import 'dart:convert';

import 'package:flutter_test/flutter_test.dart';
import 'package:mobile_vc/core/relay_e2ee/relay_tunnel.dart';

void main() {
  test('round-trips stream.open with required fields', () {
    const frame = RelayTunnelFrame(
      type: tunnelFrameStreamOpen,
      streamId: 7,
      streamType: tunnelStreamMobileVcWs,
      window: 32,
      metadata: <String, String>{'route': '/ws'},
    );

    final decoded = RelayTunnelFrame.fromJson(frame.toJson());

    expect(decoded.type, tunnelFrameStreamOpen);
    expect(decoded.streamId, 7);
    expect(decoded.metadata['route'], '/ws');
    expect(
      () => const RelayTunnelFrame(
        type: tunnelFrameStreamOpen,
        streamId: 7,
        window: 32,
      ).toJson(),
      throwsFormatException,
    );
  });

  test('stream.data payload is base64 encoded in JSON', () {
    const frame = RelayTunnelFrame(
      type: tunnelFrameStreamData,
      streamId: 7,
      seq: 1,
      payload: <int>[115, 101, 99, 114, 101, 116],
    );

    final raw = jsonEncode(frame.toJson());

    expect(raw, isNot(contains('secret')));
    final decoded = RelayTunnelFrame.fromJson(
      jsonDecode(raw) as Map<String, dynamic>,
    );
    expect(String.fromCharCodes(decoded.payload), 'secret');
  });

  test('counter state rejects replay per stream and tracks windows', () {
    final state = RelayTunnelCounterState();
    state.observe(const RelayTunnelFrame(
      type: tunnelFrameStreamOpen,
      streamId: 7,
      streamType: tunnelStreamFileDownload,
      window: 8,
    ));

    final seq = state.nextSeq(7);
    expect(seq, 1);

    final data = RelayTunnelFrame(
      type: tunnelFrameStreamData,
      streamId: 7,
      seq: seq,
      payload: const <int>[1, 2, 3],
    );
    state.observe(data);
    expect(() => state.observe(data), throwsStateError);

    state.observe(RelayTunnelFrame(
      type: tunnelFrameStreamData,
      streamId: 8,
      seq: seq,
      payload: const <int>[1, 2, 3],
    ));
    expect(state.nextSeq(8), 1);
  });

  test('zero stream window fails explicitly', () {
    final state = RelayTunnelCounterState();
    expect(
      () => state.observe(const RelayTunnelFrame(
        type: tunnelFrameStreamOpen,
        streamId: 7,
        streamType: tunnelStreamFileDownload,
      )),
      throwsFormatException,
    );
  });

  test('rejects unexpected fields for frame type', () {
    expect(
      () => const RelayTunnelFrame(
        type: tunnelFrameStreamData,
        streamId: 7,
        streamType: tunnelStreamMobileVcWs,
        seq: 1,
        payload: <int>[1],
      ).validate(),
      throwsFormatException,
    );

    expect(
      () => const RelayTunnelFrame(
        type: tunnelFramePing,
        metadata: <String, String>{'route': '/ws'},
      ).validate(),
      throwsFormatException,
    );
  });

  test('rejects unknown stream type', () {
    expect(
      () => const RelayTunnelFrame(
        type: tunnelFrameStreamOpen,
        streamId: 7,
        streamType: 'unknown.route',
        window: 32,
      ).validate(),
      throwsFormatException,
    );
  });
}
