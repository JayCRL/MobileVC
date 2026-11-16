import 'dart:async';
import 'dart:convert';

import 'package:flutter_test/flutter_test.dart';
import 'package:mobile_vc/core/relay_e2ee/relay_file_download.dart';
import 'package:mobile_vc/core/relay_e2ee/relay_tunnel.dart';

void main() {
  test('builds stream.open with encrypted download metadata', () {
    final frame = relayFileDownloadOpenFrame(
      streamId: 42,
      metadata: const RelayFileDownloadMetadata(
        path: '/workspace/build/app-release.apk',
        fileName: 'app-release.apk',
        contentType: 'application/vnd.android.package-archive',
        size: 1234,
      ),
      window: 4,
    );

    expect(frame.streamType, tunnelStreamFileDownload);
    expect(frame.metadata['path'], '/workspace/build/app-release.apk');
    final raw = jsonEncode(frame.toJson());
    final decoded = RelayTunnelFrame.fromJson(
      jsonDecode(raw) as Map<String, dynamic>,
    );
    expect(decoded.metadata['fileName'], 'app-release.apk');
    expect(decoded.metadata['size'], '1234');
    expect(
      () => relayFileDownloadOpenFrame(
        streamId: 42,
        metadata: const RelayFileDownloadMetadata(path: ''),
        window: 4,
      ),
      throwsFormatException,
    );
  });

  test('chunks download stream without requiring whole file payload frame',
      () async {
    final source = Stream<List<int>>.fromIterable(<List<int>>[
      List<int>.filled(relayFileDownloadDefaultChunkSize - 1, 1),
      <int>[2, 3],
      <int>[4, 5, 6],
    ]);

    final chunks = await relayFileDownloadChunks(source).toList();

    expect(chunks, hasLength(2));
    expect(chunks.first.length, relayFileDownloadDefaultChunkSize);
    expect(chunks.last.length, 4);
  });

  test('rejects oversized chunks and invalid chunk size', () {
    expect(
      () => relayFileDownloadDataFrame(
        streamId: 42,
        seq: 1,
        chunk: List<int>.filled(relayFileDownloadMaxChunkSize + 1, 1),
        maxChunkSize: 0,
      ),
      throwsFormatException,
    );
    expect(
      () => relayFileDownloadDataFrame(
        streamId: 42,
        seq: 1,
        chunk: const <int>[1],
        maxChunkSize: relayFileDownloadMaxChunkSize + 1,
      ),
      throwsFormatException,
    );
  });

  test('window ack and cancel frames are explicit', () {
    final window = RelayFileDownloadSendWindow(1);
    final first = relayFileDownloadDataFrame(
      streamId: 42,
      seq: 1,
      chunk: const <int>[1],
    );
    window.observeSend(first);

    final second = relayFileDownloadDataFrame(
      streamId: 42,
      seq: 2,
      chunk: const <int>[2],
    );
    expect(() => window.observeSend(second), throwsStateError);

    window.observeAck(relayFileDownloadAckFrame(
      streamId: 42,
      ack: 1,
      window: 1,
    ));
    window.observeSend(second);

    final cancel = relayFileDownloadCancelFrame(
      streamId: 42,
      reason: 'user cancelled',
    );
    expect(cancel.type, tunnelFrameStreamReset);
    expect(cancel.metadata['reason'], relayFileDownloadErrorCancelled);
  });

  test('download error codes are stable', () {
    for (final code in <String>[
      relayFileDownloadErrorCancelled,
      relayFileDownloadErrorWindowExceeded,
      relayFileDownloadErrorDenied,
      relayFileDownloadErrorFailed,
    ]) {
      expect(
        relayFileDownloadErrorFrame(streamId: 42, code: code).errorCode,
        code,
      );
    }
    expect(
      () => relayFileDownloadErrorFrame(streamId: 42, code: 'unknown'),
      throwsFormatException,
    );
  });
}
