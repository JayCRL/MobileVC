import 'dart:async';
import 'dart:convert';

import 'package:flutter_webrtc/flutter_webrtc.dart';

class AdbWebRtcService {
  final RTCVideoRenderer renderer = RTCVideoRenderer();

  RTCPeerConnection? _peerConnection;
  RTCDataChannel? _controlChannel;
  MediaStream? _remoteStream;
  bool _initialized = false;

  Future<void> ensureInitialized() async {
    if (_initialized) {
      return;
    }
    await renderer.initialize();
    _initialized = true;
  }

  bool get isReady => _initialized;

  Future<void> start({
    required Future<void> Function(String sdpType, String sdp) onOfferReady,
    void Function(RTCPeerConnectionState state)? onConnectionState,
  }) async {
    await ensureInitialized();
    await stop();

    final peerConnection = await createPeerConnection(<String, dynamic>{
      'iceServers': const <Map<String, dynamic>>[],
      'sdpSemantics': 'unified-plan',
    });

    peerConnection.onTrack = (RTCTrackEvent event) {
      if (event.streams.isNotEmpty) {
        _remoteStream = event.streams.first;
        renderer.srcObject = _remoteStream;
      }
    };
    peerConnection.onConnectionState = onConnectionState;

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
    await onOfferReady(local.type ?? 'offer', local.sdp!);
  }

  Future<void> applyAnswer(String sdpType, String sdp) async {
    final peerConnection = _peerConnection;
    if (peerConnection == null || sdp.trim().isEmpty) {
      return;
    }
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

  Future<void> stop() async {
    final controlChannel = _controlChannel;
    final peerConnection = _peerConnection;
    final remoteStream = _remoteStream;

    _controlChannel = null;
    _peerConnection = null;
    _remoteStream = null;
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
      const Duration(seconds: 5),
      onTimeout: () {},
    );
  }
}
