import 'dart:async';
import 'dart:convert';

import 'package:flutter_webrtc/flutter_webrtc.dart';

class AdbWebRtcService {
  static const Duration _iceGatheringTimeout = Duration(seconds: 12);

  final RTCVideoRenderer renderer = RTCVideoRenderer();

  RTCPeerConnection? _peerConnection;
  RTCDataChannel? _controlChannel;
  MediaStream? _remoteStream;
  bool _initialized = false;
  void Function(String message)? _debugLogger;

  Future<void> ensureInitialized() async {
    if (_initialized) {
      return;
    }
    await renderer.initialize();
    _initialized = true;
  }

  bool get isReady => _initialized;

  Future<void> start({
    List<Map<String, dynamic>> iceServers = const <Map<String, dynamic>>[],
    bool forceRelay = false,
    required Future<void> Function(String sdpType, String sdp) onOfferReady,
    void Function(RTCPeerConnectionState state)? onConnectionState,
    void Function(String message)? onDebug,
  }) async {
    await ensureInitialized();
    await stop();
    _debugLogger = onDebug;

    final peerConnection = await createPeerConnection(<String, dynamic>{
      'iceServers': iceServers,
      'iceTransportPolicy': forceRelay ? 'relay' : 'all',
      'sdpSemantics': 'unified-plan',
    });
    _emitDebug(
        forceRelay ? 'WebRTC 使用 relay-only 模式' : 'WebRTC 使用 mixed ICE 模式');

    peerConnection.onAddStream = (MediaStream stream) {
      _attachRemoteStream(stream);
    };
    peerConnection.onTrack = (RTCTrackEvent event) {
      if (event.streams.isNotEmpty) {
        _attachRemoteStream(event.streams.first);
        return;
      }
      unawaited(_attachRemoteTrack(event.track));
    };
    peerConnection.onConnectionState = (state) {
      onConnectionState?.call(state);
      if (state == RTCPeerConnectionState.RTCPeerConnectionStateFailed ||
          state == RTCPeerConnectionState.RTCPeerConnectionStateDisconnected) {
        unawaited(_emitStatsSnapshot(peerConnection, '连接态'));
      }
    };
    peerConnection.onIceConnectionState = (state) {
      _emitDebug('ICE 状态: ${_enumLabel(state)}');
      if (state == RTCIceConnectionState.RTCIceConnectionStateFailed ||
          state == RTCIceConnectionState.RTCIceConnectionStateDisconnected) {
        unawaited(_emitStatsSnapshot(peerConnection, 'ICE'));
      }
    };
    peerConnection.onIceGatheringState = (state) {
      _emitDebug('ICE 收集: ${_enumLabel(state)}');
    };
    peerConnection.onSignalingState = (state) {
      _emitDebug('信令状态: ${_enumLabel(state)}');
    };

    // iOS native WebRTC is less reliable with legacy offerToReceiveVideo
    // constraints; create an explicit recvonly video transceiver first.
    await peerConnection.addTransceiver(
      kind: RTCRtpMediaType.RTCRtpMediaTypeVideo,
      init: RTCRtpTransceiverInit(
        direction: TransceiverDirection.RecvOnly,
      ),
    );

    final dataChannel = await peerConnection.createDataChannel(
      'adb-control',
      RTCDataChannelInit()..ordered = true,
    );

    _peerConnection = peerConnection;
    _controlChannel = dataChannel;

    final offer = await peerConnection.createOffer(<String, dynamic>{
      'offerToReceiveAudio': false,
      'offerToReceiveVideo': true,
    });
    await peerConnection.setLocalDescription(offer);
    await _waitForIceGatheringComplete(peerConnection);

    final local = await peerConnection.getLocalDescription();
    if (local == null || (local.sdp ?? '').trim().isEmpty) {
      throw StateError('missing local SDP offer');
    }
    final localCandidateSummary = _summarizeSdpCandidates(local.sdp ?? '');
    _emitDebug('客户端 Offer 候选: ${localCandidateSummary.describe()}');
    if (forceRelay && localCandidateSummary.relay == 0) {
      throw StateError(
        'TURN 未返回客户端 relay 候选，请检查手机到 TURN 的 3478/UDP、3478/TCP 与凭据',
      );
    }
    await onOfferReady(local.type ?? 'offer', local.sdp!);
  }

  void _attachRemoteStream(MediaStream stream) {
    _remoteStream = stream;
    renderer.srcObject = stream;
    _emitDebug('已绑定远端视频流');
  }

  Future<void> _attachRemoteTrack(MediaStreamTrack? track) async {
    if (track == null) {
      return;
    }
    final stream = _remoteStream ?? await createLocalMediaStream('adb-remote');
    await stream.addTrack(track, addToNative: false);
    _attachRemoteStream(stream);
  }

  Future<void> applyAnswer(String sdpType, String sdp) async {
    final peerConnection = _peerConnection;
    if (peerConnection == null || sdp.trim().isEmpty) {
      return;
    }
    _emitDebug('服务端 Answer 候选: ${_summarizeSdpCandidates(sdp).describe()}');
    await peerConnection.setRemoteDescription(
      RTCSessionDescription(sdp, sdpType.trim().isEmpty ? 'answer' : sdpType),
    );
  }

  bool get canSendControl =>
      _controlChannel != null &&
      _controlChannel!.state == RTCDataChannelState.RTCDataChannelOpen;

  void sendTap(int x, int y) {
    final controlChannel = _controlChannel;
    if (controlChannel == null ||
        controlChannel.state != RTCDataChannelState.RTCDataChannelOpen) {
      return;
    }
    controlChannel.send(
      RTCDataChannelMessage(
        jsonEncode(<String, dynamic>{'type': 'tap', 'x': x, 'y': y}),
      ),
    );
  }

  void sendSwipe(
    int startX,
    int startY,
    int endX,
    int endY, {
    int durationMs = 220,
  }) {
    final controlChannel = _controlChannel;
    if (controlChannel == null ||
        controlChannel.state != RTCDataChannelState.RTCDataChannelOpen) {
      return;
    }
    controlChannel.send(
      RTCDataChannelMessage(
        jsonEncode(<String, dynamic>{
          'type': 'swipe',
          'startX': startX,
          'startY': startY,
          'endX': endX,
          'endY': endY,
          'durationMs': durationMs,
        }),
      ),
    );
  }

  void sendKeyevent(String keycode) {
    final controlChannel = _controlChannel;
    final normalized = keycode.trim();
    if (normalized.isEmpty ||
        controlChannel == null ||
        controlChannel.state != RTCDataChannelState.RTCDataChannelOpen) {
      return;
    }
    controlChannel.send(
      RTCDataChannelMessage(
        jsonEncode(<String, dynamic>{
          'type': 'keyevent',
          'keycode': normalized,
        }),
      ),
    );
  }

  Future<void> stop() async {
    final controlChannel = _controlChannel;
    final peerConnection = _peerConnection;
    final remoteStream = _remoteStream;

    _controlChannel = null;
    _peerConnection = null;
    _remoteStream = null;
    _debugLogger = null;
    if (_initialized) {
      renderer.srcObject = null;
    }

    await controlChannel?.close();
    await peerConnection?.close();
    await remoteStream?.dispose();
  }

  Future<void> dispose() async {
    await stop();
    if (_initialized) {
      await renderer.dispose();
      _initialized = false;
    }
  }

  Future<void> _waitForIceGatheringComplete(RTCPeerConnection peerConnection) {
    if (peerConnection.iceGatheringState ==
        RTCIceGatheringState.RTCIceGatheringStateComplete) {
      return Future<void>.value();
    }

    final completer = Completer<void>();
    final previous = peerConnection.onIceGatheringState;
    peerConnection.onIceGatheringState = (RTCIceGatheringState state) {
      previous?.call(state);
      if (state == RTCIceGatheringState.RTCIceGatheringStateComplete &&
          !completer.isCompleted) {
        completer.complete();
      }
    };

    return completer.future.timeout(
      _iceGatheringTimeout,
      onTimeout: () {},
    );
  }

  void _emitDebug(String message) {
    final normalized = message.trim();
    if (normalized.isEmpty) {
      return;
    }
    _debugLogger?.call(normalized);
  }

  Future<void> _emitStatsSnapshot(
    RTCPeerConnection peerConnection,
    String label,
  ) async {
    try {
      final stats = await peerConnection.getStats();
      final summary = _summarizeStats(stats);
      if (summary.isNotEmpty) {
        _emitDebug('$label 统计: $summary');
      }
    } catch (_) {}
  }

  String _summarizeStats(List<StatsReport> stats) {
    if (stats.isEmpty) {
      return '';
    }
    final byId = <String, StatsReport>{};
    for (final report in stats) {
      byId[report.id] = report;
    }

    StatsReport? pair;
    for (final report in stats) {
      if (report.type != 'candidate-pair') {
        continue;
      }
      final values = report.values;
      final selected =
          _truthy(values['selected']) || _truthy(values['nominated']);
      final state = '${values['state'] ?? ''}'.trim().toLowerCase();
      if (selected || state == 'succeeded') {
        pair = report;
        if (selected) {
          break;
        }
      }
    }
    if (pair == null) {
      return '';
    }

    final pairValues = pair.values;
    final local = byId['${pairValues['localCandidateId'] ?? ''}'];
    final remote = byId['${pairValues['remoteCandidateId'] ?? ''}'];
    final localLabel = _candidateStatsLabel(local);
    final remoteLabel = _candidateStatsLabel(remote);
    final rtt = '${pairValues['currentRoundTripTime'] ?? ''}'.trim();
    final suffix = rtt.isEmpty ? '' : ' rtt=$rtt';
    return '$localLabel -> $remoteLabel$suffix';
  }

  String _candidateStatsLabel(StatsReport? report) {
    if (report == null) {
      return '?';
    }
    final values = report.values;
    final type = '${values['candidateType'] ?? values['type'] ?? '?'}'.trim();
    final protocol = '${values['protocol'] ?? '?'}'.trim().toLowerCase();
    final address = '${values['address'] ?? values['ip'] ?? '?'}'.trim();
    final port = '${values['port'] ?? ''}'.trim();
    final host = port.isEmpty ? address : '$address:$port';
    return '$type/$protocol@$host';
  }

  bool _truthy(dynamic value) {
    if (value is bool) {
      return value;
    }
    final normalized = '${value ?? ''}'.trim().toLowerCase();
    return normalized == 'true' || normalized == '1' || normalized == 'yes';
  }

  String _enumLabel(Object value) {
    return value.toString().split('.').last;
  }

  _SdpCandidateSummary _summarizeSdpCandidates(String sdp) {
    final summary = _SdpCandidateSummary();
    for (final match in RegExp(
      r'^(?:a=)?candidate:\S+\s+\d+\s+(\S+)\s+\d+\s+\S+\s+\d+\s+typ\s+(\S+)',
      multiLine: true,
      caseSensitive: false,
    ).allMatches(sdp)) {
      final protocol = (match.group(1) ?? '').trim().toLowerCase();
      final candidateType = (match.group(2) ?? '').trim().toLowerCase();
      summary.add(candidateType, protocol);
    }
    return summary;
  }
}

class _SdpCandidateSummary {
  int host = 0;
  int srflx = 0;
  int prflx = 0;
  int relay = 0;
  int udp = 0;
  int tcp = 0;

  void add(String candidateType, String protocol) {
    switch (candidateType) {
      case 'host':
        host++;
        break;
      case 'srflx':
        srflx++;
        break;
      case 'prflx':
        prflx++;
        break;
      case 'relay':
        relay++;
        break;
    }
    switch (protocol) {
      case 'udp':
        udp++;
        break;
      case 'tcp':
      case 'ssltcp':
        tcp++;
        break;
    }
  }

  String describe() {
    return 'host=$host srflx=$srflx prflx=$prflx relay=$relay udp=$udp tcp=$tcp';
  }
}
