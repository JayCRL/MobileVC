import 'dart:async';
import 'dart:typed_data';

import 'relay_tunnel.dart';

const relayFileDownloadDefaultChunkSize = 256 * 1024;
const relayFileDownloadMaxChunkSize = 512 * 1024;

const relayFileDownloadErrorCancelled = 'stream_cancelled';
const relayFileDownloadErrorWindowExceeded = 'stream_window_exceeded';
const relayFileDownloadErrorDenied = 'download_denied';
const relayFileDownloadErrorFailed = 'download_failed';

class RelayFileDownloadMetadata {
  const RelayFileDownloadMetadata({
    required this.path,
    this.fileName = '',
    this.contentType = '',
    this.size,
  });

  final String path;
  final String fileName;
  final String contentType;
  final int? size;

  Map<String, String> toMap() {
    final out = <String, String>{'path': path};
    if (fileName.trim().isNotEmpty) {
      out['fileName'] = fileName;
    }
    if (contentType.trim().isNotEmpty) {
      out['contentType'] = contentType;
    }
    final valueSize = size;
    if (valueSize != null && valueSize >= 0) {
      out['size'] = valueSize.toString();
    }
    return out;
  }
}

RelayTunnelFrame relayFileDownloadOpenFrame({
  required int streamId,
  required RelayFileDownloadMetadata metadata,
  required int window,
}) {
  final frame = RelayTunnelFrame(
    type: tunnelFrameStreamOpen,
    streamId: streamId,
    streamType: tunnelStreamFileDownload,
    window: window,
    metadata: metadata.toMap(),
  );
  validateRelayFileDownloadFrame(frame);
  return frame;
}

RelayTunnelFrame relayFileDownloadDataFrame({
  required int streamId,
  required int seq,
  required List<int> chunk,
  int maxChunkSize = relayFileDownloadDefaultChunkSize,
}) {
  final frame = RelayTunnelFrame(
    type: tunnelFrameStreamData,
    streamId: streamId,
    seq: seq,
    payload: List<int>.unmodifiable(chunk),
  );
  validateRelayFileDownloadFrame(frame, maxChunkSize: maxChunkSize);
  return frame;
}

RelayTunnelFrame relayFileDownloadAckFrame({
  required int streamId,
  required int ack,
  required int window,
}) {
  final frame = RelayTunnelFrame(
    type: tunnelFrameStreamAck,
    streamId: streamId,
    ack: ack,
    window: window,
  );
  validateRelayFileDownloadFrame(frame);
  return frame;
}

RelayTunnelFrame relayFileDownloadCloseFrame({
  required int streamId,
  required int seq,
}) {
  final frame = RelayTunnelFrame(
    type: tunnelFrameStreamClose,
    streamId: streamId,
    seq: seq,
  );
  validateRelayFileDownloadFrame(frame);
  return frame;
}

RelayTunnelFrame relayFileDownloadCancelFrame({
  required int streamId,
  String reason = '',
}) {
  final metadata = <String, String>{'reason': relayFileDownloadErrorCancelled};
  if (reason.trim().isNotEmpty) {
    metadata['message'] = reason;
  }
  final frame = RelayTunnelFrame(
    type: tunnelFrameStreamReset,
    streamId: streamId,
    metadata: metadata,
  );
  validateRelayFileDownloadFrame(frame);
  return frame;
}

RelayTunnelFrame relayFileDownloadErrorFrame({
  required int streamId,
  required String code,
  Map<String, String> metadata = const <String, String>{},
}) {
  final frame = RelayTunnelFrame(
    type: tunnelFrameStreamError,
    streamId: streamId,
    errorCode: code,
    metadata: metadata,
  );
  validateRelayFileDownloadFrame(frame);
  return frame;
}

void validateRelayFileDownloadFrame(
  RelayTunnelFrame frame, {
  int maxChunkSize = relayFileDownloadDefaultChunkSize,
}) {
  frame.validate();
  _validateFileDownloadShape(frame);
  if (frame.type == tunnelFrameStreamData) {
    _validateFileDownloadChunk(frame.payload, maxChunkSize);
  }
}

Stream<Uint8List> relayFileDownloadChunks(
  Stream<List<int>> source, {
  int chunkSize = relayFileDownloadDefaultChunkSize,
}) async* {
  final normalized = _normalizeChunkSize(chunkSize);
  final pending = BytesBuilder(copy: false);
  await for (final piece in source) {
    if (piece.isEmpty) {
      continue;
    }
    pending.add(piece);
    while (pending.length >= normalized) {
      final bytes = pending.takeBytes();
      yield Uint8List.sublistView(bytes, 0, normalized);
      if (bytes.length > normalized) {
        pending.add(Uint8List.sublistView(bytes, normalized));
      }
    }
  }
  if (pending.length > 0) {
    yield pending.takeBytes();
  }
}

class RelayFileDownloadSendWindow {
  RelayFileDownloadSendWindow(
    int window, {
    int maxChunkSize = relayFileDownloadDefaultChunkSize,
  })  : _window = window,
        _maxChunkSize = _normalizeChunkSize(maxChunkSize) {
    if (window == 0) {
      throw StateError(relayFileDownloadErrorWindowExceeded);
    }
  }

  int _window;
  final int _maxChunkSize;
  final Set<int> _inFlight = <int>{};

  void observeSend(RelayTunnelFrame frame) {
    validateRelayFileDownloadFrame(frame, maxChunkSize: _maxChunkSize);
    if (frame.type != tunnelFrameStreamData) {
      return;
    }
    if (_inFlight.length >= _window) {
      throw StateError(relayFileDownloadErrorWindowExceeded);
    }
    _inFlight.add(frame.seq);
  }

  void observeAck(RelayTunnelFrame frame) {
    validateRelayFileDownloadFrame(frame);
    if (frame.type != tunnelFrameStreamAck) {
      return;
    }
    _window = frame.window;
    _inFlight.removeWhere((seq) => seq <= frame.ack);
  }
}

void _validateFileDownloadShape(RelayTunnelFrame frame) {
  if (frame.type == tunnelFrameStreamOpen &&
      frame.streamType != tunnelStreamFileDownload) {
    throw const FormatException(
      'file download stream.open must use file.download',
    );
  }
  if (frame.type == tunnelFrameStreamOpen &&
      (frame.metadata['path'] ?? '').trim().isEmpty) {
    throw const FormatException('file download path is required');
  }
  if (frame.type == tunnelFrameStreamError &&
      !_isFileDownloadErrorCode(frame.errorCode)) {
    throw FormatException(
      'unknown file download error code: ${frame.errorCode}',
    );
  }
}

void _validateFileDownloadChunk(List<int> chunk, int maxChunkSize) {
  final normalized = _normalizeChunkSize(maxChunkSize);
  if (chunk.isEmpty) {
    throw const FormatException('file download chunk is empty');
  }
  if (chunk.length > normalized) {
    throw FormatException('file download chunk exceeds $normalized bytes');
  }
}

int _normalizeChunkSize(int size) {
  if (size == 0) {
    return relayFileDownloadDefaultChunkSize;
  }
  if (size < 0 || size > relayFileDownloadMaxChunkSize) {
    throw FormatException('invalid file download chunk size: $size');
  }
  return size;
}

bool _isFileDownloadErrorCode(String code) {
  return switch (code) {
    relayFileDownloadErrorCancelled ||
    relayFileDownloadErrorWindowExceeded ||
    relayFileDownloadErrorDenied ||
    relayFileDownloadErrorFailed =>
      true,
    _ => false,
  };
}
