import 'dart:async';
import 'dart:convert';

import 'package:flutter/foundation.dart';
import 'package:web_socket_channel/web_socket_channel.dart';
import 'package:web_socket_channel/io.dart';

import '../models/events.dart';
import '../models/runtime_meta.dart';
import 'mobilevc_mapper.dart';

class MobileVcWsService {
  MobileVcWsService({MobileVcMapper? mapper})
      : _mapper = mapper ?? const MobileVcMapper();

  final MobileVcMapper _mapper;
  final StreamController<AppEvent> _events =
      StreamController<AppEvent>.broadcast();
  WebSocketChannel? _channel;
  StreamSubscription? _subscription;
  int _connectionEpoch = 0;

  Stream<AppEvent> get events => _events.stream;
  bool get isConnected => _channel != null;

  Future<void> connect(String url) async {
    final previousSubscription = _subscription;
    final previousChannel = _channel;
    _subscription = null;
    _channel = null;
    _connectionEpoch++;
    await previousSubscription?.cancel();
    await previousChannel?.sink.close();

    final uri = Uri.parse(url);
    final channel = kIsWeb
        ? WebSocketChannel.connect(uri)
        : IOWebSocketChannel.connect(
            uri,
            pingInterval: const Duration(seconds: 15),
            connectTimeout: const Duration(seconds: 15),
          );
    _channel = channel;
    final epoch = _connectionEpoch;
    var disconnectEmitted = false;
    void emitDisconnect({
      required String code,
      required String message,
      Map<String, dynamic> raw = const <String, dynamic>{},
    }) {
      if (disconnectEmitted || epoch != _connectionEpoch || _channel != channel) {
        return;
      }
      disconnectEmitted = true;
      _channel = null;
      _subscription = null;
      _events.add(
        ErrorEvent(
          timestamp: DateTime.now(),
          sessionId: '',
          runtimeMeta: const RuntimeMeta(),
          raw: <String, dynamic>{
            'type': 'error',
            'code': code,
            'msg': message,
            ...raw,
          },
          code: code,
          message: message,
        ),
      );
    }

    _subscription = channel.stream.listen(
      (dynamic data) {
        if (epoch != _connectionEpoch || _channel != channel) {
          return;
        }
        final decoded = jsonDecode(data as String);
        if (decoded is Map<String, dynamic>) {
          _events.add(_mapper.mapEvent(decoded));
        }
      },
      onError: (Object error, StackTrace stackTrace) {
        emitDisconnect(
          code: 'ws_stream_error',
          message: 'WebSocket 连接异常：$error',
          raw: <String, dynamic>{
            'stack': stackTrace.toString(),
          },
        );
      },
      onDone: () {
        final closeCode = channel.closeCode;
        final closeReason = channel.closeReason;
        final message = closeCode == null
            ? 'WebSocket 连接已断开'
            : closeReason == null || closeReason.isEmpty
                ? 'WebSocket 连接已断开（$closeCode）'
                : 'WebSocket 连接已断开（$closeCode: $closeReason）';
        emitDisconnect(
          code: 'ws_closed',
          message: message,
          raw: <String, dynamic>{
            'closeCode': closeCode,
            'closeReason': closeReason,
          },
        );
      },
      cancelOnError: false,
    );
  }

  Future<void> disconnect() async {
    final previousSubscription = _subscription;
    final previousChannel = _channel;
    _subscription = null;
    _channel = null;
    _connectionEpoch++;
    await previousSubscription?.cancel();
    await previousChannel?.sink.close();
  }

  void send(Map<String, dynamic> payload) {
    final channel = _channel;
    if (channel == null) {
      return;
    }
    try {
      channel.sink.add(jsonEncode(payload));
    } catch (error, stackTrace) {
      _channel = null;
      _subscription = null;
      _events.add(
        ErrorEvent(
          timestamp: DateTime.now(),
          sessionId: (payload['sessionId'] ?? '').toString(),
          runtimeMeta: const RuntimeMeta(),
          raw: <String, dynamic>{
            'type': 'error',
            'code': 'ws_send_error',
            'msg': 'WebSocket send failed: $error',
            'stack': stackTrace.toString(),
          },
          code: 'ws_send_error',
          message: 'WebSocket send failed: $error',
        ),
      );
    }
  }

  Future<void> dispose() async {
    await disconnect();
    await _events.close();
  }
}
