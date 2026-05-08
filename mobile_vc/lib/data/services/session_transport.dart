import 'dart:async';
import '../models/events.dart';

abstract class SessionTransport {
  Stream<AppEvent> get events;
  bool get isConnected;

  /// Connect to the backend.
  /// For local mode: pass the WebSocket URL.
  /// For official mode: ignored (connectToNode is used instead).
  Future<void> connect(String url);

  Future<void> disconnect();
  bool send(Map<String, dynamic> payload);
  Future<void> dispose();
}
