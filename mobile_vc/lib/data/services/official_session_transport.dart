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
  final _errorCtrl = StreamController<String>.broadcast();
  final _debugCtrl = StreamController<String>.broadcast();

  @override
  Stream<AppEvent> get events => _eventCtrl.stream;
  Stream<String> get errors => _errorCtrl.stream;
  Stream<String> get debugLog => _debugCtrl.stream;

  bool _connected = false;
  @override
  bool get isConnected => _connected;

  StreamSubscription? _sigSub;
  StreamSubscription? _sigErrorSub;
  Timer? _connPollTimer;
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
    _debugCtrl.add('connectToNode: nodeId=$nodeId peerId=$_peerId');

    // 1. Connect signaling WebSocket
    final wsUrl = apiService.baseUrl;
    final token = apiService.accessToken;
    _debugCtrl.add('signaling.connect: url=$wsUrl token=${token.substring(0, 10)}...');
    try {
      await signaling.connect(wsUrl, token);
      _debugCtrl.add('signaling.connect OK');
    } catch (e) {
      _debugCtrl.add('signaling.connect FAILED: $e');
      _errorCtrl.add('无法连接信令服务器: $e');
      rethrow;
    }

    // Listen for signaling errors
    _sigErrorSub = signaling.errors.listen((err) {
      _debugCtrl.add('signaling error: $err');
      _errorCtrl.add(err);
    });

    // Listen for signaling messages
    final completer = Completer<void>();

    _sigSub = signaling.messages.listen((msg) async {
      _debugCtrl.add('signaling msg: type=${msg.type} accept=${msg.accept}');
      switch (msg.type) {
        case 'connect_response':
          if (msg.accept == true) {
            _debugCtrl.add('connect_response accepted, starting WebRTC...');
            // 2. Start WebRTC offer
            try {
              await _startWebRTC();
              _debugCtrl.add('_startWebRTC completed');
              // 3. Poll for DataChannel connection
              _startConnectionPoll(completer);
            } catch (e, st) {
              _debugCtrl.add('_startWebRTC FAILED: $e\n$st');
              _errorCtrl.add('WebRTC 启动失败: $e');
              if (!completer.isCompleted) {
                completer.completeError('WebRTC 启动失败: $e');
              }
            }
          } else {
            _debugCtrl.add('connect_response rejected');
            if (!completer.isCompleted) {
              completer.completeError('节点拒绝了连接');
            }
          }
          break;

        case 'webrtc':
          final data = msg.data ?? {};
          final sdpType = data['type'] as String?;
          final sdp = data['sdp'] as String?;
          final candidate = data['candidate'] as Map<String, dynamic>?;
          _debugCtrl.add('webrtc msg: sdpType=$sdpType hasSdp=${sdp != null} hasCandidate=${candidate != null}');

          try {
            if (sdpType == 'answer' && sdp != null) {
              _debugCtrl.add('applying WebRTC answer...');
              await webrtc.applyAnswer(sdpType!, sdp);
            } else if (candidate != null) {
              await webrtc.addIceCandidate(candidate);
            }
          } catch (e, st) {
            _debugCtrl.add('webrtc processing error: $e\n$st');
            _errorCtrl.add('WebRTC 处理错误: $e');
          }
          break;

        case 'peer_disconnected':
          _debugCtrl.add('peer_disconnected');
          _connected = false;
          break;

        case 'node_offline':
          _debugCtrl.add('node_offline');
          _connected = false;
          break;
      }
    });

    // Listen for WebRTC DataChannel events
    webrtc.events.listen((json) {
      _eventCtrl.add(mapper.mapEvent(json));
    });

    // Send connect request
    _debugCtrl.add('sending connect_request: nodeId=$_nodeId peerId=$_peerId');
    signaling.sendConnectRequest(_nodeId!, _peerId!);

    // Wait for DataChannel to open (up to 30s)
    _debugCtrl.add('waiting for DataChannel...');
    try {
      await completer.future.timeout(const Duration(seconds: 30));
      _connected = true;
      _debugCtrl.add('DataChannel opened! connected=true');
    } catch (_) {
      _connected = webrtc.isConnected;
      _debugCtrl.add('timeout/error. webrtc.isConnected=$_connected');
    }
    _debugCtrl.add('connectToNode done');
  }

  void _startConnectionPoll(Completer<void> completer) {
    _connPollTimer?.cancel();
    _connPollTimer = Timer.periodic(const Duration(seconds: 1), (_) {
      if (webrtc.isConnected && !completer.isCompleted) {
        _connPollTimer?.cancel();
        completer.complete();
      }
    });
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
      onDebug: (msg) => _debugCtrl.add('[webrtc] $msg'),
      onOfferReady: (type, sdp) {
        signaling.sendWebRTC(_nodeId!, _peerId!, {
          'type': type,
          'sdp': sdp,
        });
      },
      onIceCandidate: (candidate) {
        signaling.sendWebRTC(_nodeId!, _peerId!, {
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
    _connPollTimer?.cancel();
    _sigSub?.cancel();
    _sigErrorSub?.cancel();
    await webrtc.disconnect();
    await signaling.disconnect();
  }

  @override
  Future<void> dispose() async {
    await disconnect();
    await webrtc.dispose();
    signaling.dispose();
    await _eventCtrl.close();
    await _errorCtrl.close();
    await _debugCtrl.close();
  }
}
