import 'dart:async';
import '../models/events.dart';
import 'official_api_service.dart';
import 'official_signaling_service.dart';
import 'official_webrtc_service.dart';
import 'mobilevc_mapper.dart';
import 'session_transport.dart';

class OfficialSessionTransport implements SessionTransport {
  final OfficialApiService apiService;
  final OfficialSignalingService signaling;
  final OfficialWebRtcService webrtc;
  final MobileVcMapper mapper;

  final _eventCtrl = StreamController<AppEvent>.broadcast();
  @override
  Stream<AppEvent> get events => _eventCtrl.stream;
  bool _connected = false;
  @override
  bool get isConnected => _connected;

  StreamSubscription? _sigSub;
  String? _peerId;
  String? _nodeId;

  OfficialSessionTransport({
    required this.apiService,
    OfficialSignalingService? signalingService,
    OfficialWebRtcService? webrtcService,
    this.mapper = const MobileVcMapper(),
  })  : signaling = signalingService ?? OfficialSignalingService(),
        webrtc = webrtcService ?? OfficialWebRtcService();

  @override
  Future<void> connect(String url) async {
    // Official mode doesn't use URL-based connect; call connectToNode instead
    throw UnimplementedError('Use connectToNode for official mode');
  }

  Future<void> connectToNode(String nodeId, {String? peerId}) async {
    _nodeId = nodeId;
    _peerId = peerId ?? 'mobile-${DateTime.now().millisecondsSinceEpoch}';

    // 1. Connect signaling WebSocket
    final wsUrl = apiService.baseUrl;
    final token = apiService.accessToken;
    await signaling.connect(wsUrl, token);

    // Listen for signaling messages
    final completer = Completer<void>();
    var remoteSdp = false;

    _sigSub = signaling.messages.listen((msg) async {
      switch (msg.type) {
        case 'connect_response':
          if (msg.accept == true) {
            // 2. Start WebRTC offer
            await _startWebRTC();
          } else {
            completer.completeError('Node rejected connection');
          }
          break;

        case 'webrtc':
          final data = msg.data ?? {};
          final sdpType = data['type'] as String?;
          final sdp = data['sdp'] as String?;
          final candidate = data['candidate'] as Map<String, dynamic>?;

          if (sdpType == 'answer' && sdp != null) {
            await webrtc.applyAnswer(sdpType!, sdp);
            if (!remoteSdp) {
              remoteSdp = true;
              // We'll complete when DataChannel opens
            }
          } else if (candidate != null) {
            await webrtc.addIceCandidate(candidate);
          } else if (sdpType != null && sdp != null) {
            // Answer already applied; ignore duplicate
          }
          break;

        case 'peer_disconnected':
          _connected = false;
          break;

        case 'node_offline':
          _connected = false;
          break;
      }
    });

    // Listen for WebRTC DataChannel events
    webrtc.events.listen((json) {
      _eventCtrl.add(mapper.mapEvent(json));
    });

    // Send connect request
    signaling.sendConnectRequest(_nodeId!, _peerId!);

    // Wait for connection to establish (DataChannel open)
    try {
      await completer.future.timeout(const Duration(seconds: 60));
    } catch (_) {
      // DataChannel will handle connection naturally via webrtc.isConnected
    }
  }

  Future<void> _startWebRTC() async {
    // Get ICE servers from the target node
    List<Map<String, dynamic>> iceServers = [
      {'urls': 'stun:stun.l.google.com:19302'},
    ];

    try {
      final nodes = await apiService.listNodes();
      for (final node in nodes) {
        if (node.id == _nodeId && node.stunHost.isNotEmpty) {
          final turn = {
            'urls': [
              'turn:${node.stunHost}:${node.turnPort}?transport=udp',
              'turn:${node.stunHost}:${node.turnPort}?transport=tcp',
            ],
            'username': node.turnUser,
            'credential': node.turnPass,
          };
          iceServers = [turn, ...iceServers];
        }
      }
    } catch (_) {}

    await webrtc.createOffer(
      iceServers: iceServers,
      onOfferReady: (type, sdp) {
        signaling.sendWebRTC(_peerId!, {
          'type': type,
          'sdp': sdp,
        });
      },
      onIceCandidate: (candidate) {
        signaling.sendWebRTC(_peerId!, {
          'candidate': candidate.candidate,
          'sdpMid': candidate.sdpMid,
          'sdpMLineIndex': candidate.sdpMLineIndex,
        });
      },
    );
  }

  @override
  bool send(Map<String, dynamic> payload) {
    return webrtc.send(payload);
  }

  @override
  Future<void> disconnect() async {
    _connected = false;
    _sigSub?.cancel();
    await webrtc.disconnect();
    await signaling.disconnect();
  }

  @override
  Future<void> dispose() async {
    await disconnect();
    await webrtc.dispose();
    signaling.dispose();
    await _eventCtrl.close();
  }
}
