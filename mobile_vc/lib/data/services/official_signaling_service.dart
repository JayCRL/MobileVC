import 'dart:async';
import 'dart:convert';
import 'dart:io';

class SignalingMessage {
  final String type;
  final String? nodeId;
  final String? peerId;
  final Map<String, dynamic>? data;
  final bool? accept;

  SignalingMessage({
    required this.type,
    this.nodeId,
    this.peerId,
    this.data,
    this.accept,
  });

  Map<String, dynamic> toJson() => {
        'type': type,
        if (nodeId != null) 'nodeId': nodeId,
        if (peerId != null) 'peerId': peerId,
        if (data != null) 'data': data,
        if (accept != null) 'accept': accept,
      };

  factory SignalingMessage.fromJson(Map<String, dynamic> json) =>
      SignalingMessage(
        type: json['type'] ?? '',
        nodeId: json['nodeId'],
        peerId: json['peerId'],
        data: json['data'] as Map<String, dynamic>?,
        accept: json['accept'] as bool?,
      );
}

class OfficialSignalingService {
  WebSocket? _ws;
  final _messageCtrl = StreamController<SignalingMessage>.broadcast();
  final _errorCtrl = StreamController<String>.broadcast();

  Stream<SignalingMessage> get messages => _messageCtrl.stream;
  Stream<String> get errors => _errorCtrl.stream;

  bool get isConnected => _ws != null;

  Future<void> connect(String serverUrl, String accessToken) async {
    final wsUrl = serverUrl
        .replaceFirst('http://', 'ws://')
        .replaceFirst('https://', 'wss://');
    final uri = Uri.parse('$wsUrl/ws/signaling?token=$accessToken');

    // ignore: avoid_print
    print('[Signaling] connecting to $uri');

    final ws = await WebSocket.connect(uri.toString());
    _ws = ws;
    // ignore: avoid_print
    print('[Signaling] connected OK');

    ws.listen(
      (data) {
        try {
          final json = jsonDecode(data as String) as Map<String, dynamic>;
          _messageCtrl.add(SignalingMessage.fromJson(json));
        } catch (_) {}
      },
      onError: (e) {
        _ws = null;
        _errorCtrl.add('信令错误: $e');
      },
      onDone: () {
        _ws = null;
      },
    );
  }

  void announceNode(String nodeId) {
    _send(SignalingMessage(type: 'node_online', nodeId: nodeId));
  }

  void sendConnectRequest(String nodeId, String peerId) {
    _send(SignalingMessage(
      type: 'connect_request',
      nodeId: nodeId,
      peerId: peerId,
    ));
  }

  void sendWebRTC(String peerId, Map<String, dynamic> data) {
    _send(SignalingMessage(type: 'webrtc', peerId: peerId, data: data));
  }

  void sendConnectResponse(String peerId, bool accept) {
    _send(SignalingMessage(
      type: 'connect_response',
      peerId: peerId,
      accept: accept,
    ));
  }

  void _send(SignalingMessage msg) {
    if (_ws != null) {
      _ws!.add(jsonEncode(msg.toJson()));
    }
  }

  Future<void> disconnect() async {
    await _ws?.close();
    _ws = null;
  }

  void dispose() {
    disconnect();
    _messageCtrl.close();
    _errorCtrl.close();
  }
}
