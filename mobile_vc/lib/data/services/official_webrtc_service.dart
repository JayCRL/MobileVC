import 'dart:async';
import 'dart:convert';
import 'dart:math';
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
  bool _offerSent = false;
  final List<RTCIceCandidate> _pendingCandidates = [];
  bool _remoteDescSet = false;
  final List<Map<String, dynamic>> _pendingPeerCandidates = [];

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
      if (c == null || c.isEmpty) {
        _log('ICE candidate gathering complete');
        return;
      }
      final candLower = c.toLowerCase();
      final cType = candLower.contains('typ host') ? 'host' :
          candLower.contains('typ srflx') ? 'srflx' :
          candLower.contains('typ relay') ? 'relay' : 'unknown';
      _log('ICE candidate: type=$cType candidate=${c.substring(0, min(c.length, 80))}');
      if (_offerSent) {
        onIceCandidate(candidate);
      } else {
        _pendingCandidates.add(candidate);
      }
    };

    _pc!.onIceConnectionState = (state) {
      _log('ICE state: ${_enumLabel(state)}');
      if (state == RTCIceConnectionState.RTCIceConnectionStateFailed ||
          state == RTCIceConnectionState.RTCIceConnectionStateDisconnected) {
        _dumpStats();
      }
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

    _pc!.onIceGatheringState = (state) {
      _log('ICE gathering: ${_enumLabel(state)}');
      if (state == RTCIceGatheringState.RTCIceGatheringStateComplete) {
        _dumpStats();
      }
    };

    // Create SDP offer
    final offer = await _pc!.createOffer();
    await _pc!.setLocalDescription(offer);

    // Send offer right away so desktop creates PeerConnection before candidates arrive
    final desc = await _pc!.getLocalDescription();
    if (desc != null && desc.sdp != null && desc.type != null) {
      onOfferReady(desc.type!, desc.sdp!);
    }

    // Flush pending candidates now that offer is sent
    _offerSent = true;
    for (final c in _pendingCandidates) {
      onIceCandidate(c);
    }
    _pendingCandidates.clear();
  }

  Future<void> applyAnswer(String sdpType, String sdp) async {
    if (_pc == null) return;
    _log('applyAnswer: type=$sdpType sdpLen=${sdp.length}');
    await _pc!.setRemoteDescription(
      RTCSessionDescription(sdp, sdpType),
    );
    _remoteDescSet = true;
    _log('Remote answer applied, flushing ${_pendingPeerCandidates.length} pending candidates');
    for (final c in _pendingPeerCandidates) {
      await _addCandidateInternal(c, 'flushed');
    }
    _pendingPeerCandidates.clear();
  }

  Future<void> addIceCandidate(Map<String, dynamic> candidate) async {
    if (_pc == null) return;
    if (!_remoteDescSet) {
      String c = candidate['candidate'] as String? ?? '';
      String t = c.toLowerCase().contains('typ relay') ? 'relay' :
          c.toLowerCase().contains('typ srflx') ? 'srflx' :
          c.toLowerCase().contains('typ host') ? 'host' : '?';
      _log('buffering $t candidate (remote desc not set yet)');
      _pendingPeerCandidates.add(candidate);
      return;
    }
    await _addCandidateInternal(candidate, 'live');
  }

  Future<void> _addCandidateInternal(Map<String, dynamic> candidate, String tag) async {
    final candStr = candidate['candidate'] as String? ?? '';
    final mid = candidate['sdpMid'] as String? ?? '';
    final idx = candidate['sdpMLineIndex'] as int? ?? 0;
    try {
      await _pc!.addCandidate(RTCIceCandidate(candStr, mid, idx));
      final cType = candStr.toLowerCase().contains('typ relay') ? 'relay' :
          candStr.toLowerCase().contains('typ srflx') ? 'srflx' : 'host';
      _log('addCandidate $tag OK: type=$cType mid=$mid idx=$idx');
    } catch (e) {
      _log('addCandidate $tag FAIL: $e | mid=$mid idx=$idx');
    }
  }

  void _setupDataChannel(RTCDataChannel channel) {
    channel.onMessage = (RTCDataChannelMessage msg) {
      if (msg.isBinary) {
        _log('DataChannel recv BINARY (ignored)');
        return;
      }
      try {
        final data = msg.text;
        final json = jsonDecode(data) as Map<String, dynamic>;
        _log('DataChannel recv OK: action=${json['action'] ?? json['type']} len=${data.length}');
        _eventCtrl.add(json);
      } catch (e) {
        _log('DataChannel recv FAIL: $e');
      }
    };

    channel.onDataChannelState = (state) {
      _log('DataChannel state: ${_enumLabel(state)}');
      if (state == RTCDataChannelState.RTCDataChannelOpen) {
        _connected = true;
        _log('P2P DataChannel OPEN');
      } else if (state == RTCDataChannelState.RTCDataChannelClosed ||
                 state == RTCDataChannelState.RTCDataChannelClosing) {
        _connected = false;
        _log('DataChannel closed');
      }
    };
  }

  void _dumpStats() {
    if (_pc == null) return;
    _pc!.getStats().then((stats) {
      final buf = StringBuffer();
      for (final r in stats) {
        if (r.type == 'candidate-pair') {
          final s = r.values;
          buf.writeln('  pair: local=${s['localCandidateId']} remote=${s['remoteCandidateId']} '
              'state=${s['state']} nominated=${s['nominated']} '
              'bytesSent=${s['bytesSent']} bytesRecv=${s['bytesReceived']}');
        }
      }
      if (buf.isNotEmpty) {
        _log('getStats candidate-pairs:\n$buf');
      }
    }).catchError((e) { _log('getStats error: $e'); });
  }

  bool send(Map<String, dynamic> payload) {
    if (_dataChannel == null) {
      _log('send FAIL: dataChannel is null');
      return false;
    }
    if (!_connected) {
      _log('send FAIL: not connected');
      return false;
    }
    final data = jsonEncode(payload);
    _dataChannel!.send(RTCDataChannelMessage(data));
    _log('send OK: action=${payload['action']} len=${data.length}');
    return true;
  }

  Future<void> _cleanup() async {
    _connected = false;
    _offerSent = false;
    _remoteDescSet = false;
    _pendingCandidates.clear();
    _pendingPeerCandidates.clear();
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
