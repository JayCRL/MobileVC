import 'dart:async';
import 'dart:convert';
import 'package:flutter_webrtc/flutter_webrtc.dart';

class OfficialWebRtcService {
  RTCPeerConnection? _pc;
  RTCDataChannel? _dataChannel;
  final _eventCtrl = StreamController<Map<String, dynamic>>.broadcast();
  Stream<Map<String, dynamic>> get events => _eventCtrl.stream;
  bool _connected = false;
  bool get isConnected => _connected;
  void Function(String)? _debugLog;
  Timer? _iceTimer;

  Future<void> createOffer({
    required List<Map<String, dynamic>> iceServers,
    required void Function(String sdpType, String sdp) onOfferReady,
    required void Function(RTCIceCandidate candidate) onIceCandidate,
    void Function(String)? onDebug,
  }) async {
    _debugLog = onDebug;
    await _cleanup();

    _pc = await createPeerConnection({
      'iceServers': iceServers,
      'iceTransportPolicy': 'all',
      'sdpSemantics': 'unified-plan',
    });

    _pc!.onIceCandidate = (candidate) {
      final c = candidate.candidate;
      if (c != null && c.isNotEmpty) {
        onIceCandidate(candidate);
      }
    };

    _pc!.onIceConnectionState = (state) {
      _log('ICE state: ${_enumLabel(state)}');
    };

    _pc!.onDataChannel = (channel) {
      _log('DataChannel received: ${channel.label}');
      _setupDataChannel(channel);
    };

    // Create DataChannel
    final init = RTCDataChannelInit()
      ..ordered = true
      ..protocol = 'json';
    _dataChannel = await _pc!.createDataChannel('mobilevc-control', init);
    _setupDataChannel(_dataChannel!);

    // Create SDP offer
    final offer = await _pc!.createOffer();
    await _pc!.setLocalDescription(offer);

    // Wait for ICE gathering
    final completer = Completer<void>();
    _iceTimer = Timer(const Duration(seconds: 12), () {
      if (!completer.isCompleted) completer.complete();
    });

    _pc!.onIceGatheringState = (state) {
      _log('ICE gathering: ${_enumLabel(state)}');
      if (state == RTCIceGatheringState.RTCIceGatheringStateComplete) {
        if (!completer.isCompleted) completer.complete();
      }
    };

    await completer.future.timeout(const Duration(seconds: 15));
    _iceTimer?.cancel();

    final desc = await _pc!.getLocalDescription();
    if (desc != null && desc.sdp != null && desc.type != null) {
      onOfferReady(desc.type!, desc.sdp!);
    }
  }

  Future<void> applyAnswer(String sdpType, String sdp) async {
    if (_pc == null) return;
    await _pc!.setRemoteDescription(
      RTCSessionDescription(sdp, sdpType),
    );
    _log('Remote answer applied');
  }

  Future<void> addIceCandidate(Map<String, dynamic> candidate) async {
    if (_pc == null) return;
    await _pc!.addCandidate(
      RTCIceCandidate(
        candidate['candidate'] as String? ?? '',
        candidate['sdpMid'] as String? ?? '',
        candidate['sdpMLineIndex'] as int? ?? 0,
      ),
    );
  }

  void _setupDataChannel(RTCDataChannel channel) {
    channel.onMessage = (RTCDataChannelMessage msg) {
      if (msg.isBinary) return;
      try {
        final data = msg.text;
        final json = jsonDecode(data) as Map<String, dynamic>;
        _eventCtrl.add(json);
      } catch (_) {}
    };

    channel.onDataChannelState = (state) {
      _log('DataChannel state: ${_enumLabel(state)}');
      if (state == RTCDataChannelState.RTCDataChannelOpen) {
        _connected = true;
        _log('P2P DataChannel OPEN');
      } else if (state == RTCDataChannelState.RTCDataChannelClosed ||
                 state == RTCDataChannelState.RTCDataChannelClosing) {
        _connected = false;
      }
    };
  }

  bool send(Map<String, dynamic> payload) {
    if (_dataChannel == null || !_connected) return false;
    _dataChannel!.send(RTCDataChannelMessage(jsonEncode(payload)));
    return true;
  }

  Future<void> _cleanup() async {
    _connected = false;
    _iceTimer?.cancel();
    _iceTimer = null;
    _dataChannel?.close();
    _dataChannel = null;
    await _pc?.close();
    _pc = null;
  }

  Future<void> disconnect() => _cleanup();

  Future<void> dispose() async {
    await disconnect();
    await _eventCtrl.close();
  }

  void _log(String msg) => _debugLog?.call(msg);

  String _enumLabel(dynamic e) => e.toString().split('.').last;
}
