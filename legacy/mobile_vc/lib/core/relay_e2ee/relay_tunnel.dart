import 'dart:convert';

const relayTunnelVersion = 1;

const tunnelFrameStreamOpen = 'stream.open';
const tunnelFrameStreamData = 'stream.data';
const tunnelFrameStreamAck = 'stream.ack';
const tunnelFrameStreamClose = 'stream.close';
const tunnelFrameStreamReset = 'stream.reset';
const tunnelFrameStreamError = 'stream.error';
const tunnelFramePing = 'ping';
const tunnelFramePong = 'pong';

const tunnelStreamMobileVcWs = 'mobilevc.ws';
const tunnelStreamFileDownload = 'file.download';

class RelayTunnelFrame {
  const RelayTunnelFrame({
    required this.type,
    this.version = relayTunnelVersion,
    this.streamId = 0,
    this.streamType = '',
    this.seq = 0,
    this.ack = 0,
    this.window = 0,
    this.payload = const <int>[],
    this.errorCode = '',
    this.metadata = const <String, String>{},
  });

  final String type;
  final int version;
  final int streamId;
  final String streamType;
  final int seq;
  final int ack;
  final int window;
  final List<int> payload;
  final String errorCode;
  final Map<String, String> metadata;

  Map<String, dynamic> toJson() {
    validate();
    final json = <String, dynamic>{
      'type': type,
      'version': version,
    };
    if (streamId != 0) {
      json['streamId'] = streamId;
    }
    if (streamType.isNotEmpty) {
      json['streamType'] = streamType;
    }
    if (seq != 0) {
      json['seq'] = seq;
    }
    if (ack != 0) {
      json['ack'] = ack;
    }
    if (window != 0) {
      json['window'] = window;
    }
    if (payload.isNotEmpty) {
      json['payload'] = base64Url.encode(payload).replaceAll('=', '');
    }
    if (errorCode.isNotEmpty) {
      json['errorCode'] = errorCode;
    }
    if (metadata.isNotEmpty) {
      json['metadata'] = Map<String, String>.of(metadata);
    }
    return json;
  }

  factory RelayTunnelFrame.fromJson(Map<String, dynamic> json) {
    final payloadValue = (json['payload'] ?? '').toString();
    final metadataValue = json['metadata'];
    final frame = RelayTunnelFrame(
      type: (json['type'] ?? '').toString(),
      version: _intField(json, 'version'),
      streamId: _intField(json, 'streamId'),
      streamType: (json['streamType'] ?? '').toString(),
      seq: _intField(json, 'seq'),
      ack: _intField(json, 'ack'),
      window: _intField(json, 'window'),
      payload: payloadValue.isEmpty
          ? const <int>[]
          : base64Url.decode(base64Url.normalize(payloadValue)),
      errorCode: (json['errorCode'] ?? '').toString(),
      metadata: metadataValue is Map
          ? metadataValue.map((key, value) => MapEntry(
                key.toString(),
                value.toString(),
              ))
          : const <String, String>{},
    );
    frame.validate();
    return frame;
  }

  void validate() {
    if (version != relayTunnelVersion) {
      throw const FormatException('invalid tunnel frame version');
    }
    switch (type) {
      case tunnelFrameStreamOpen:
        _validateFields(
          const _RelayTunnelFieldRule(
            streamId: true,
            streamType: true,
            window: true,
            metadata: true,
          ),
        );
      case tunnelFrameStreamData:
        _validateFields(
          const _RelayTunnelFieldRule(streamId: true, seq: true, payload: true),
        );
      case tunnelFrameStreamAck:
        _validateFields(
          const _RelayTunnelFieldRule(streamId: true, ack: true, window: true),
        );
      case tunnelFrameStreamClose:
        _validateFields(
          const _RelayTunnelFieldRule(streamId: true, seq: true),
        );
      case tunnelFrameStreamReset:
        _validateFields(
          const _RelayTunnelFieldRule(streamId: true, metadata: true),
        );
      case tunnelFrameStreamError:
        _validateFields(
          const _RelayTunnelFieldRule(
            streamId: true,
            errorCode: true,
            metadata: true,
          ),
        );
      case tunnelFramePing:
      case tunnelFramePong:
        _validateFields(const _RelayTunnelFieldRule());
      default:
        throw FormatException('unknown tunnel frame type: $type');
    }
  }

  void _validateFields(_RelayTunnelFieldRule rule) {
    if (rule.streamId && streamId == 0) {
      throw const FormatException('tunnel frame missing streamId');
    }
    if (!rule.streamId && streamId != 0) {
      throw const FormatException('tunnel frame has unexpected streamId');
    }
    if (rule.streamType && streamType.trim().isEmpty) {
      throw const FormatException('tunnel frame missing streamType');
    }
    if (!rule.streamType && streamType.trim().isNotEmpty) {
      throw const FormatException('tunnel frame has unexpected streamType');
    }
    if (rule.streamType && !_isTunnelStreamType(streamType)) {
      throw FormatException('unknown tunnel stream type: $streamType');
    }
    if (rule.seq && seq == 0) {
      throw const FormatException('tunnel frame missing seq');
    }
    if (!rule.seq && seq != 0) {
      throw const FormatException('tunnel frame has unexpected seq');
    }
    if (rule.ack && ack == 0) {
      throw const FormatException('tunnel frame missing ack');
    }
    if (!rule.ack && ack != 0) {
      throw const FormatException('tunnel frame has unexpected ack');
    }
    if (rule.window && window == 0) {
      throw const FormatException('tunnel frame missing window');
    }
    if (!rule.window && window != 0) {
      throw const FormatException('tunnel frame has unexpected window');
    }
    if (rule.payload && payload.isEmpty) {
      throw const FormatException('tunnel frame missing payload');
    }
    if (!rule.payload && payload.isNotEmpty) {
      throw const FormatException('tunnel frame has unexpected payload');
    }
    if (rule.errorCode && errorCode.trim().isEmpty) {
      throw const FormatException('tunnel frame missing errorCode');
    }
    if (!rule.errorCode && errorCode.trim().isNotEmpty) {
      throw const FormatException('tunnel frame has unexpected errorCode');
    }
    if (!rule.metadata && metadata.isNotEmpty) {
      throw const FormatException('tunnel frame has unexpected metadata');
    }
  }

  static int _intField(Map<String, dynamic> json, String key) {
    final value = json[key];
    if (value == null) return 0;
    if (value is int) return value;
    if (value is num) return value.toInt();
    return int.parse(value.toString());
  }
}

class RelayTunnelCounterState {
  final Map<int, int> _nextSeq = <int, int>{};
  final Map<int, Set<int>> _seen = <int, Set<int>>{};
  final Map<int, int> windows = <int, int>{};

  int nextSeq(int streamId) {
    if (streamId == 0) {
      throw const FormatException('tunnel frame missing streamId');
    }
    final seq = (_nextSeq[streamId] ?? 0) + 1;
    _nextSeq[streamId] = seq;
    return seq;
  }

  void observe(RelayTunnelFrame frame) {
    frame.validate();
    if (frame.type == tunnelFrameStreamOpen ||
        frame.type == tunnelFrameStreamAck) {
      if (frame.window == 0) {
        throw StateError('stream window exceeded');
      }
      windows[frame.streamId] = frame.window;
    }
    if (frame.type != tunnelFrameStreamData &&
        frame.type != tunnelFrameStreamClose) {
      return;
    }
    final seenByStream = _seen.putIfAbsent(frame.streamId, () => <int>{});
    if (seenByStream.contains(frame.seq)) {
      throw StateError('e2ee replay detected');
    }
    seenByStream.add(frame.seq);
  }
}

class _RelayTunnelFieldRule {
  const _RelayTunnelFieldRule({
    this.streamId = false,
    this.streamType = false,
    this.seq = false,
    this.ack = false,
    this.window = false,
    this.payload = false,
    this.errorCode = false,
    this.metadata = false,
  });

  final bool streamId;
  final bool streamType;
  final bool seq;
  final bool ack;
  final bool window;
  final bool payload;
  final bool errorCode;
  final bool metadata;
}

bool _isTunnelStreamType(String streamType) {
  return switch (streamType) {
    tunnelStreamMobileVcWs || tunnelStreamFileDownload => true,
    _ => false,
  };
}
