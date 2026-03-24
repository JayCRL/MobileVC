import 'dart:async';
import 'dart:convert';

import 'package:web_socket_channel/web_socket_channel.dart';

import '../models/events.dart';
import '../models/runtime_meta.dart';
import 'mobilevc_mapper.dart';

class MobileVcWsService {
  MobileVcWsService({MobileVcMapper? mapper}) : _mapper = mapper ?? const MobileVcMapper();

  final MobileVcMapper _mapper;
  final StreamController<AppEvent> _events = StreamController<AppEvent>.broadcast();
  WebSocketChannel? _channel;
  StreamSubscription? _subscription;

  Stream<AppEvent> get events => _events.stream;
  bool get isConnected => _channel != null;

  Future<void> connect(String url) async {
    await disconnect();
    final channel = WebSocketChannel.connect(Uri.parse(url));
    _channel = channel;
    _subscription = channel.stream.listen(
      (dynamic data) {
        final decoded = jsonDecode(data as String);
        if (decoded is Map<String, dynamic>) {
          _events.add(_mapper.mapEvent(decoded));
        }
      },
      onError: (Object error, StackTrace stackTrace) {
        _events.add(
          ErrorEvent(
            timestamp: DateTime.now(),
            sessionId: '',
            runtimeMeta: const RuntimeMeta(),
            raw: {'type': 'error', 'msg': error.toString()},
            message: error.toString(),
          ),
        );
      },
      onDone: () {
        _channel = null;
      },
      cancelOnError: false,
    );
  }

  Future<void> disconnect() async {
    await _subscription?.cancel();
    await _channel?.sink.close();
    _subscription = null;
    _channel = null;
  }

  void send(Map<String, dynamic> payload) {
    _channel?.sink.add(jsonEncode(payload));
  }

  Future<void> dispose() async {
    await disconnect();
    await _events.close();
  }
}
