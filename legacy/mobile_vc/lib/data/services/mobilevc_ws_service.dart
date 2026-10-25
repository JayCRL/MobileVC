import 'dart:async';
import 'dart:convert';
import 'dart:typed_data';
import 'package:flutter/foundation.dart';
import 'package:uuid/uuid.dart';
import 'package:web_socket_channel/web_socket_channel.dart';
import 'package:web_socket_channel/io.dart';

import '../../core/relay_e2ee/relay_e2ee_capability.dart';
import '../../core/relay_e2ee/relay_e2ee_crypto.dart';
import '../../core/relay_e2ee/relay_device_identity.dart';
import '../../core/relay_e2ee/relay_e2ee_handshake.dart';
import '../../core/relay_e2ee/relay_e2ee_handshake_frames.dart';
import '../../core/relay_e2ee/relay_file_download.dart';
import '../../core/relay_e2ee/relay_mobilevc_stream.dart';
import '../../core/relay_e2ee/relay_security_state.dart';
import '../../core/relay_e2ee/relay_tunnel.dart';
import '../models/events.dart';
import '../models/runtime_meta.dart';
import 'mobilevc_mapper.dart';

const _relayDownloadWindow = 4;

class MobileVcWsService {
  MobileVcWsService({
    MobileVcMapper? mapper,
    RelayDeviceIdentityStore? relayDeviceIdentityStore,
    RelayDeviceCredentialStore? relayDeviceCredentialStore,
  })  : _mapper = mapper ?? const MobileVcMapper(),
        _relayDeviceIdentityStore = relayDeviceIdentityStore ??
            RelayDeviceIdentityStore(
              secureStore: const FlutterRelaySecureStore(),
            ),
        _relayDeviceCredentialStore = relayDeviceCredentialStore ??
            RelayDeviceCredentialStore(
              secureStore: const FlutterRelaySecureStore(),
            );

  final MobileVcMapper _mapper;
  final RelayDeviceIdentityStore _relayDeviceIdentityStore;
  final RelayDeviceCredentialStore _relayDeviceCredentialStore;
  final StreamController<AppEvent> _events =
      StreamController<AppEvent>.broadcast();
  WebSocketChannel? _channel;
  StreamSubscription? _subscription;
  int _connectionEpoch = 0;

  Stream<AppEvent> get events => _events.stream;
  bool get isConnected => _channel != null;
  bool get hasRelayE2eeHandshake => _relayE2eeState?.complete == true;
  bool get hasRelayE2eeDeviceBinding =>
      hasRelayE2eeHandshake &&
      _relayE2eeState?.boundDeviceId.trim().isNotEmpty == true;
  RelaySecurityInput relaySecurityInput({
    required String connectionMode,
    String expectedNodeFingerprintHex = '',
    RelayE2eeCapabilitySet? configuredCapabilities,
  }) {
    final state = _relayE2eeState;
    final input = state?.input;
    final capabilities = state?.capabilities ?? configuredCapabilities;
    final activeRelaySession = _relayContext.sessionId.trim().isNotEmpty;
    final expectsProduction = _relayContext.requiresE2EE ||
        (activeRelaySession &&
            capabilities?.requiresE2EE == true &&
            capabilities?.plaintextTestMode == false);
    final plaintextTestMode = capabilities?.plaintextTestMode ??
        (connectionMode == 'relay' && activeRelaySession && !expectsProduction);
    return RelaySecurityInput(
      connectionMode: connectionMode,
      expectedNodeFingerprintHex: state?.expectedNodeFingerprintHex ??
          expectedNodeFingerprintHex.trim().toLowerCase(),
      actualNodePublicKey: input?.nodeIdentityPublicKey ?? const <int>[],
      nodeFingerprintConfirmed:
          state?.expectedNodeFingerprintHex.trim().isNotEmpty == true &&
              state?.complete == true,
      handshakeComplete: state?.complete == true,
      protocolSupportsE2ee: capabilities?.requiresE2EE == true,
      protocolSupportsTunnel: capabilities != null,
      supportsMultiplexStreams: capabilities?.supportsMultiplexStreams == true,
      supportsFileDownload: capabilities?.supportsFileDownload == true,
      supportsDeviceManagement: capabilities?.supportsDeviceManagement == true,
      requiresE2ee: capabilities?.requiresE2EE == true,
      plaintextTestMode: plaintextTestMode,
      productionPlaintextRejected: expectsProduction && activeRelaySession,
      deviceBound: state?.boundDeviceId.trim().isNotEmpty == true,
    );
  }

  Future<void> connect(String url) async {
    await _connectChannel(Uri.parse(url));
  }

  Future<void> connectDirectAfterReady(String url) async {
    final channel = await _openReadyChannel(Uri.parse(url));
    await _replaceWithReadyDirectChannel(channel);
  }

  Future<void> connectRelay({
    required String relayUrl,
    required String sessionId,
    String pairingSecret = '',
    String clientId = '',
    String clientReconnectSecret = '',
    String nodeFingerprintHex = '',
    RelayE2eeCapabilitySet? relayCapabilities,
  }) async {
    final uri = Uri.parse(relayUrl).replace(path: '/relay/client');
    await _connectChannel(
      uri,
      relaySessionId: sessionId,
      relayPairingSecret: pairingSecret,
      relayClientId: clientId,
      relayClientReconnectSecret: clientReconnectSecret,
      relayNodeFingerprintHex: nodeFingerprintHex,
      relayCapabilities: relayCapabilities,
    );
  }

  Future<void> _connectChannel(
    Uri uri, {
    String relaySessionId = '',
    String relayPairingSecret = '',
    String relayClientId = '',
    String relayClientReconnectSecret = '',
    String relayNodeFingerprintHex = '',
    RelayE2eeCapabilitySet? relayCapabilities,
  }) async {
    final initialRelayClientId = relayClientId.trim();
    final initialRelayReconnectSecret = relayClientReconnectSecret.trim();
    _validateRelayE2EEConnectMode(
      relayPairingSecret: relayPairingSecret,
      relayClientId: initialRelayClientId,
      relayClientReconnectSecret: initialRelayReconnectSecret,
      capabilities: relayCapabilities,
    );
    final previousSubscription = _subscription;
    final previousChannel = _channel;
    _subscription = null;
    _channel = null;
    _relayContext = const _RelayContext();
    _relayE2eeState = null;
    _relaySendQueue = Future<void>.value();
    _relayReceiveQueue = Future<void>.value();
    _failRelayDownloads(StateError('Relay connection replaced'));
    _connectionEpoch++;
    await previousSubscription?.cancel();
    await previousChannel?.sink.close();
    final channel = _createChannel(uri);
    _channel = channel;
    final epoch = _connectionEpoch;
    var activeRelayClientId = initialRelayClientId;
    var activeRelayReconnectSecret = initialRelayReconnectSecret;
    final pairedCompleter =
        relaySessionId.isNotEmpty ? Completer<void>() : null;
    final e2eeCompleter = _requiresRelayE2EE(capabilities: relayCapabilities)
        ? Completer<void>()
        : null;
    _RelayE2eeHandshakeState? e2eeState;
    if (relaySessionId.isNotEmpty) {
      channel.sink.add(jsonEncode(_relayAuthFrame(
        relaySessionId: relaySessionId,
        relayPairingSecret: relayPairingSecret,
        relayClientId: activeRelayClientId,
        relayClientReconnectSecret: relayClientReconnectSecret,
      )));
    }
    var disconnectEmitted = false;
    void emitDisconnect({
      required String code,
      required String message,
      Map<String, dynamic> raw = const <String, dynamic>{},
    }) {
      if (disconnectEmitted ||
          epoch != _connectionEpoch ||
          _channel != channel) {
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

    void completeRelayConnectError(Object error) {
      if (e2eeCompleter != null && !e2eeCompleter.isCompleted) {
        e2eeCompleter.completeError(error);
        return;
      }
      final completer = pairedCompleter;
      if (completer != null && !completer.isCompleted) {
        completer.completeError(error);
      }
    }

    _subscription = channel.stream.listen(
      (dynamic data) {
        if (epoch != _connectionEpoch || _channel != channel) {
          return;
        }
        final decoded = jsonDecode(data as String);
        if (decoded is! Map<String, dynamic>) {
          return;
        }
        if (relaySessionId.isEmpty) {
          _events.add(_mapper.mapEvent(decoded));
          return;
        }
        if (isRelayAuthError(decoded, pairedCompleter?.isCompleted != true)) {
          completeRelayConnectError(RelayPairingException.fromFrame(decoded));
          return;
        }
        if (e2eeCompleter != null &&
            _isRelayE2EEFrame(decoded) &&
            _handleRelayE2EEFrame(
              channel: channel,
              epoch: epoch,
              frame: decoded,
              state: e2eeState,
              updateState: (next) => e2eeState = next,
              complete: e2eeCompleter,
              paired: pairedCompleter,
            )) {
          return;
        }
        if (e2eeCompleter != null &&
            !e2eeCompleter.isCompleted &&
            _isRelayForwardFrame(decoded)) {
          final error = RelayPairingException(
            'e2ee_decrypt_failed',
            relayErrorMessage(const <String, dynamic>{
              'type': 'relay.error',
              'code': 'e2ee_decrypt_failed',
            }),
          );
          completeRelayConnectError(error);
          unawaited(disconnect());
          return;
        }
        _queueRelayReceive(
          channel: channel,
          frame: decoded,
          epoch: epoch,
          setClientId: (clientId, reconnectSecret) {
            activeRelayClientId = clientId;
            if (reconnectSecret.trim().isNotEmpty) {
              activeRelayReconnectSecret = reconnectSecret;
            }
            if (e2eeCompleter != null &&
                !e2eeCompleter.isCompleted &&
                e2eeState == null) {
              unawaited(() async {
                try {
                  _ensureCurrentChannel(channel, epoch);
                  final handshakeKind = await _relayE2EEHandshakeKind(
                    relayPairingSecret: relayPairingSecret,
                  );
                  _ensureCurrentChannel(channel, epoch);
                  e2eeState = await _startRelayE2EEHandshake(
                    channel: channel,
                    epoch: epoch,
                    sessionId: relaySessionId,
                    clientId: clientId,
                    kind: handshakeKind,
                    pairingSecret: relayPairingSecret,
                    expectedNodeFingerprintHex: relayNodeFingerprintHex,
                    capabilities: relayCapabilities!,
                    updateState: (next) => e2eeState = next,
                  );
                } catch (error, stackTrace) {
                  if (!e2eeCompleter.isCompleted) {
                    e2eeCompleter.completeError(error, stackTrace);
                  }
                }
              }());
            }
            final completer = pairedCompleter;
            if (completer != null &&
                !completer.isCompleted &&
                e2eeCompleter == null) {
              completer.complete();
            }
          },
        );
      },
      onError: (Object error, StackTrace stackTrace) {
        completeRelayConnectError(error);
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
        completeRelayConnectError(StateError(message));
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
    _relayContext = _RelayContext(
      sessionId: relaySessionId,
      clientIdProvider: () => activeRelayClientId,
      requiresE2EE: _requiresRelayE2EE(capabilities: relayCapabilities),
    );
    if (pairedCompleter != null) {
      try {
        await (e2eeCompleter?.future ?? pairedCompleter.future).timeout(
          const Duration(seconds: 10),
          onTimeout: () {
            throw TimeoutException(
              e2eeCompleter == null ? 'Relay 配对超时' : 'Relay E2EE 握手超时',
            );
          },
        );
      } catch (_) {
        if (_isCurrentChannel(channel, epoch)) {
          await disconnect();
        }
        rethrow;
      }
    }
    if (relaySessionId.isNotEmpty) {
      _relaySession = RelaySession(
        sessionId: relaySessionId,
        clientId: activeRelayClientId,
        clientReconnectSecret: activeRelayReconnectSecret,
      );
    }
  }

  WebSocketChannel _createChannel(Uri uri) {
    return kIsWeb
        ? WebSocketChannel.connect(uri)
        : IOWebSocketChannel.connect(
            uri,
            pingInterval: const Duration(seconds: 15),
            connectTimeout: const Duration(seconds: 15),
          );
  }

  Future<WebSocketChannel> _openReadyChannel(Uri uri) async {
    final channel = _createChannel(uri);
    try {
      await channel.ready.timeout(const Duration(seconds: 15));
      return channel;
    } catch (_) {
      await channel.sink.close();
      rethrow;
    }
  }

  Future<void> _replaceWithReadyDirectChannel(WebSocketChannel channel) async {
    final previousSubscription = _subscription;
    final previousChannel = _channel;
    _subscription = null;
    _channel = null;
    _relayContext = const _RelayContext();
    _relayE2eeState = null;
    _relaySendQueue = Future<void>.value();
    _relayReceiveQueue = Future<void>.value();
    _failRelayDownloads(StateError('Relay connection replaced'));
    _connectionEpoch++;
    await previousSubscription?.cancel();
    await previousChannel?.sink.close();
    _channel = channel;
    final epoch = _connectionEpoch;
    var disconnectEmitted = false;
    void emitDisconnect({
      required String code,
      required String message,
      Map<String, dynamic> raw = const <String, dynamic>{},
    }) {
      if (disconnectEmitted ||
          epoch != _connectionEpoch ||
          _channel != channel) {
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
    _relayE2eeState = null;
    _relaySendQueue = Future<void>.value();
    _relayReceiveQueue = Future<void>.value();
    _failRelayDownloads(StateError('Relay connection closed'));
    _connectionEpoch++;
    await previousSubscription?.cancel();
    await previousChannel?.sink.close();
  }

  bool _isCurrentChannel(WebSocketChannel channel, int epoch) {
    return epoch == _connectionEpoch && _channel == channel;
  }

  void _ensureCurrentChannel(WebSocketChannel channel, int epoch) {
    if (!_isCurrentChannel(channel, epoch)) {
      throw StateError('Relay connection was replaced');
    }
  }

  bool send(Map<String, dynamic> payload) {
    final channel = _channel;
    if (channel == null) {
      _events.add(
        ErrorEvent(
          timestamp: DateTime.now(),
          sessionId: (payload['sessionId'] ?? '').toString(),
          runtimeMeta: const RuntimeMeta(),
          raw: const <String, dynamic>{
            'type': 'error',
            'code': 'ws_not_connected',
            'msg': 'WebSocket is not connected',
          },
          code: 'ws_not_connected',
          message: 'WebSocket is not connected',
        ),
      );
      return false;
    }
    try {
      final relayContext = _relayContext;
      if (relayContext.sessionId.isNotEmpty) {
        _queueRelaySend(
          channel: channel,
          payload: payload,
          relayContext: relayContext,
          epoch: _connectionEpoch,
        );
        return true;
      }
      channel.sink.add(jsonEncode(payload));
      return true;
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
      return false;
    }
  }

  Future<void> dispose() async {
    await disconnect();
    await _events.close();
  }

  _RelayContext _relayContext = const _RelayContext();
  RelaySession? _relaySession;
  _RelayE2eeHandshakeState? _relayE2eeState;
  Future<void> _relaySendQueue = Future<void>.value();
  Future<void> _relayReceiveQueue = Future<void>.value();
  final Map<int, _RelayFileDownloadState> _relayDownloads =
      <int, _RelayFileDownloadState>{};
  int _nextDownloadStreamId = 41;

  void markRelayDeviceRegistered(
    RelayDeviceRegisterResultEvent event,
  ) {
    final state = _relayE2eeState;
    final deviceId = event.deviceId.trim();
    if (state == null || !state.complete || deviceId.isEmpty) {
      return;
    }
    final next = state.copyWithBoundDeviceId(deviceId);
    _relayE2eeState = next;
  }

  Future<void> resetRelayDeviceBinding() async {
    await _relayDeviceIdentityStore.reset();
    await _relayDeviceCredentialStore.reset();
    _relayE2eeState = null;
  }

  int _nextRelayDownloadStreamId() {
    _nextDownloadStreamId += 2;
    return _nextDownloadStreamId;
  }

  String _fallbackFileName(String path) {
    final normalized = path.replaceAll('\\', '/').trim();
    if (normalized.isEmpty) {
      return 'download.bin';
    }
    final index = normalized.lastIndexOf('/');
    final fileName = index == -1 ? normalized : normalized.substring(index + 1);
    return fileName.isEmpty ? 'download.bin' : fileName;
  }

  Future<RelayFileDownloadResult> downloadRelayFile(
    String path, {
    void Function(int receivedBytes, int? totalBytes)? onProgress,
    FutureOr<void> Function(Uint8List chunk)? onChunk,
    RelayFileDownloadCancelToken? cancelToken,
  }) async {
    final channel = _channel;
    final state = _relayE2eeState;
    final codec = state?.streamCodec;
    final target = path.trim();
    if (channel == null || _relayContext.sessionId.isEmpty) {
      throw StateError('Relay is not connected');
    }
    if (target.isEmpty) {
      throw const FormatException('download path is required');
    }
    if (state?.complete != true || codec == null) {
      throw StateError('Relay E2EE stream is not ready');
    }
    if (state!.boundDeviceId.trim().isEmpty) {
      throw StateError('Relay E2EE device is not bound');
    }
    if (!state.capabilities.supportsFileDownload) {
      throw StateError('Relay E2EE file download is unsupported');
    }
    if (cancelToken?.isCancelled == true) {
      throw StateError(relayFileDownloadErrorCancelled);
    }

    final streamId = _nextRelayDownloadStreamId();
    final pending = _RelayFileDownloadState(
      streamId: streamId,
      fallbackFileName: _fallbackFileName(target),
      onProgress: onProgress,
      onChunk: onChunk,
    );
    _relayDownloads[streamId] = pending;
    cancelToken?._bind(() async {
      await _cancelRelayDownload(
        channel: channel,
        state: state,
        streamId: streamId,
        reason: 'user cancelled',
      );
    });
    try {
      final openFrame = relayFileDownloadOpenFrame(
        streamId: streamId,
        metadata: RelayFileDownloadMetadata(path: target),
        window: _relayDownloadWindow,
      );
      final forward = await codec.encodeTunnelFrame(
        messageId: 'msg_${const Uuid().v4()}',
        frame: openFrame,
      );
      if (cancelToken?.isCancelled == true) {
        throw StateError(relayFileDownloadErrorCancelled);
      }
      if (_channel != channel) {
        throw StateError('Relay connection changed during download');
      }
      channel.sink.add(jsonEncode(forward));
      return await pending.result.future;
    } catch (error) {
      _relayDownloads.remove(streamId);
      if (!pending.result.isCompleted) {
        pending.result.completeError(error);
      }
      rethrow;
    } finally {
      cancelToken?._unbind();
    }
  }

  RelaySession? takeRelaySession() {
    final session = _relaySession;
    _relaySession = null;
    return session;
  }

  Future<Map<String, dynamic>?> _decodeRelayFrame(
    Map<String, dynamic> frame,
    void Function(String clientId, String clientReconnectSecret) setClientId,
    WebSocketChannel channel,
  ) async {
    final type = (frame['type'] ?? '').toString();
    if (type == 'client.paired') {
      setClientId(
        (frame['clientId'] ?? '').toString(),
        (frame['clientReconnectSecret'] ?? '').toString(),
      );
      return null;
    }
    if (type == 'relay.error') {
      return <String, dynamic>{
        'type': 'error',
        'code': frame['code'] ?? 'relay_error',
        'msg': relayErrorMessage(frame),
      };
    }
    if (type != 'relay.forward') {
      return null;
    }
    final e2eeState = _relayE2eeState;
    if (frame['encryption'] == relayE2eeSuite) {
      final codec = e2eeState?.streamCodec;
      if (codec == null) {
        throw StateError(
            'Relay E2EE frame received before encrypted stream is ready');
      }
      if (_relayFrameStreamId(frame) != relayMobileVcStreamId) {
        await _handleRelayTunnelFrame(
          channel: channel,
          frame: frame,
          state: e2eeState!,
        );
        return null;
      }
      return codec.decodeJson(frame);
    }
    if (e2eeState?.complete == true) {
      throw const FormatException(
          'plaintext relay.forward received after E2EE activation');
    }
    if (_relayContext.requiresE2EE) {
      throw const FormatException(
          'plaintext relay.forward received before E2EE activation');
    }
    final raw =
        base64Url.decode(base64Url.normalize(frame['payload'].toString()));
    final decoded = jsonDecode(utf8.decode(raw));
    return decoded is Map<String, dynamic> ? decoded : null;
  }

  Future<void> _handleRelayTunnelFrame({
    required WebSocketChannel channel,
    required Map<String, dynamic> frame,
    required _RelayE2eeHandshakeState state,
  }) async {
    final tunnelFrame = await state.streamCodec!.decodeTunnelFrame(frame);
    if (tunnelFrame.type == tunnelFrameStreamError) {
      _handleRelayTunnelError(tunnelFrame);
      return;
    }
    if (tunnelFrame.type == tunnelFramePing ||
        tunnelFrame.type == tunnelFramePong) {
      return;
    }
    if (tunnelFrame.streamType.isNotEmpty &&
        tunnelFrame.streamType != tunnelStreamFileDownload) {
      throw FormatException(
          'unsupported relay tunnel stream ${tunnelFrame.streamType}');
    }
    await _handleRelayFileDownloadFrame(
      channel: channel,
      tunnelFrame: tunnelFrame,
      state: state,
    );
  }

  Future<void> _handleRelayFileDownloadFrame({
    required WebSocketChannel channel,
    required RelayTunnelFrame tunnelFrame,
    required _RelayE2eeHandshakeState state,
  }) async {
    validateRelayFileDownloadFrame(tunnelFrame);
    final pending = _relayDownloads[tunnelFrame.streamId];
    if (pending == null) {
      if (tunnelFrame.type == tunnelFrameStreamData) {
        await _sendRelayDownloadAck(
          channel: channel,
          state: state,
          streamId: tunnelFrame.streamId,
          seq: tunnelFrame.seq,
        );
      }
      return;
    }
    switch (tunnelFrame.type) {
      case tunnelFrameStreamOpen:
        pending.applyOpen(tunnelFrame);
      case tunnelFrameStreamData:
        await pending.addChunk(tunnelFrame.payload);
        await _sendRelayDownloadAck(
          channel: channel,
          state: state,
          streamId: tunnelFrame.streamId,
          seq: tunnelFrame.seq,
        );
      case tunnelFrameStreamClose:
        _relayDownloads.remove(tunnelFrame.streamId);
        pending.complete();
      case tunnelFrameStreamError:
        _relayDownloads.remove(tunnelFrame.streamId);
        pending.completeError(_relayTunnelException(tunnelFrame));
      case tunnelFrameStreamReset:
        _relayDownloads.remove(tunnelFrame.streamId);
        pending.completeError(StateError(
          tunnelFrame.metadata['message'] ?? 'Relay download cancelled',
        ));
      default:
        throw FormatException(
            'unexpected file download frame ${tunnelFrame.type}');
    }
  }

  void _handleRelayTunnelError(RelayTunnelFrame frame) {
    final pending = _relayDownloads.remove(frame.streamId);
    final error = _relayTunnelException(frame);
    if (pending != null) {
      pending.completeError(error);
      return;
    }
    throw error;
  }

  RelayPairingException _relayTunnelException(RelayTunnelFrame frame) {
    return RelayPairingException(
      frame.errorCode,
      relayErrorMessage(<String, dynamic>{
        'type': 'relay.error',
        'code': frame.errorCode,
        'message': frame.metadata['message'] ?? '',
      }),
    );
  }

  Future<void> _sendRelayDownloadAck({
    required WebSocketChannel channel,
    required _RelayE2eeHandshakeState state,
    required int streamId,
    required int seq,
  }) async {
    final ack = relayFileDownloadAckFrame(
      streamId: streamId,
      ack: seq,
      window: _relayDownloadWindow,
    );
    final forward = await state.streamCodec!.encodeTunnelFrame(
      messageId: 'msg_${const Uuid().v4()}',
      frame: ack,
    );
    if (_channel == channel) {
      channel.sink.add(jsonEncode(forward));
    }
  }

  Future<void> _cancelRelayDownload({
    required WebSocketChannel channel,
    required _RelayE2eeHandshakeState state,
    required int streamId,
    required String reason,
  }) async {
    final pending = _relayDownloads.remove(streamId);
    if (pending == null) {
      return;
    }
    pending.completeError(StateError(relayFileDownloadErrorCancelled));
    final reset = relayFileDownloadCancelFrame(
      streamId: streamId,
      reason: reason,
    );
    final forward = await state.streamCodec!.encodeTunnelFrame(
      messageId: 'msg_${const Uuid().v4()}',
      frame: reset,
    );
    if (_channel == channel) {
      channel.sink.add(jsonEncode(forward));
    }
  }

  void _failRelayDownloads(Object error) {
    if (_relayDownloads.isEmpty) {
      return;
    }
    final downloads = List<_RelayFileDownloadState>.of(_relayDownloads.values);
    _relayDownloads.clear();
    for (final download in downloads) {
      download.completeError(error);
    }
  }

  Future<_RelayE2eeHandshakeState> _startRelayE2EEHandshake({
    required WebSocketChannel channel,
    required int epoch,
    required String sessionId,
    required String clientId,
    required String kind,
    required String pairingSecret,
    required String expectedNodeFingerprintHex,
    required RelayE2eeCapabilitySet capabilities,
    required void Function(_RelayE2eeHandshakeState) updateState,
  }) async {
    final clientEphemeral = await RelayE2eeHandshake.newEphemeralKeyPair();
    final deviceIdentity = kind == relayE2eeHandshakeKindReconnect
        ? await _relayDeviceIdentityStore.loadOrCreate()
        : null;
    final deviceCredential = kind == relayE2eeHandshakeKindReconnect
        ? await _relayDeviceCredentialStore.loadOrCreate()
        : null;
    final handshakeId = 'hs_${const Uuid().v4()}';
    final state = _RelayE2eeHandshakeState(
      kind: kind,
      sessionId: sessionId,
      clientId: clientId,
      handshakeId: handshakeId,
      pairingSecret: pairingSecret,
      expectedNodeFingerprintHex: expectedNodeFingerprintHex.toLowerCase(),
      capabilities: capabilities,
      clientEphemeral: clientEphemeral,
      deviceIdentity: deviceIdentity,
      deviceCredential: deviceCredential,
    );
    final hello = RelayE2eeClientHelloFrame(
      sessionId: sessionId,
      clientId: clientId,
      handshakeId: handshakeId,
      kind: kind,
      capabilities: capabilities,
      clientEphemeralPublicKey: clientEphemeral.publicKey,
      deviceId: deviceIdentity?.fullFingerprintHex ?? '',
      deviceIdentityPublicKey: deviceIdentity?.publicKey ?? const <int>[],
    );
    _ensureCurrentChannel(channel, epoch);
    updateState(state);
    _ensureCurrentChannel(channel, epoch);
    channel.sink.add(jsonEncode(hello.toJson()));
    return state;
  }

  Future<String> _relayE2EEHandshakeKind({
    required String relayPairingSecret,
  }) async {
    if (relayPairingSecret.trim().isNotEmpty) {
      return relayE2eeHandshakeKindPairing;
    }
    return relayE2eeHandshakeKindReconnect;
  }

  bool _handleRelayE2EEFrame({
    required WebSocketChannel channel,
    required int epoch,
    required Map<String, dynamic> frame,
    required _RelayE2eeHandshakeState? state,
    required void Function(_RelayE2eeHandshakeState) updateState,
    required Completer<void> complete,
    required Completer<void>? paired,
  }) {
    final type = (frame['type'] ?? '').toString();
    if (type == relayFrameAgentE2eeHello) {
      unawaited(_completeRelayE2EEProof(
        channel: channel,
        epoch: epoch,
        pending: state,
        frame: frame,
        updateState: updateState,
        complete: complete,
      ));
      return true;
    }
    if (type == relayFrameAgentE2eeResult) {
      try {
        final result = RelayE2eeAgentResultFrame.fromJson(frame);
        if (!result.ok) {
          throw RelayPairingException(
            result.errorCode,
            relayErrorMessage(<String, dynamic>{
              'type': 'relay.error',
              'code': result.errorCode,
            }),
          );
        }
        final pending = state;
        if (pending == null ||
            !pending.matches(
              result.sessionId,
              result.clientId,
              result.handshakeId,
            ) ||
            pending.trafficKeys == null ||
            pending.input == null) {
          throw StateError('Relay E2EE 握手路由不匹配');
        }
        _ensureCurrentChannel(channel, epoch);
        _relayE2eeState = pending.markComplete();
        if (_relayE2eeState!.kind == relayE2eeHandshakeKindPairing) {
          _queueRelayDeviceRegister(
            channel: channel,
            state: _relayE2eeState!,
            epoch: epoch,
          );
        }
        if (!complete.isCompleted) {
          complete.complete();
        }
        if (paired != null && !paired.isCompleted) {
          paired.complete();
        }
      } catch (error, stackTrace) {
        if (!complete.isCompleted) {
          complete.completeError(error, stackTrace);
        }
      }
      return true;
    }
    return false;
  }

  Future<void> _completeRelayE2EEProof({
    required WebSocketChannel channel,
    required int epoch,
    required _RelayE2eeHandshakeState? pending,
    required Map<String, dynamic> frame,
    required void Function(_RelayE2eeHandshakeState) updateState,
    required Completer<void> complete,
  }) async {
    try {
      if (pending == null) {
        throw StateError('Relay E2EE 握手状态不存在');
      }
      _ensureCurrentChannel(channel, epoch);
      final agentHello = RelayE2eeAgentHelloFrame.fromJson(frame);
      if (!pending.matches(
        agentHello.sessionId,
        agentHello.clientId,
        agentHello.handshakeId,
      )) {
        throw StateError('Relay E2EE 握手路由不匹配');
      }
      final fingerprint = await RelayE2eeCrypto.fingerprint(
        Uint8List.fromList(agentHello.nodeIdentityPublicKey),
      );
      if (_hex(fingerprint) != pending.expectedNodeFingerprintHex) {
        throw const RelayPairingException(
          'e2ee_fingerprint_mismatch',
          'Relay 节点指纹不一致：请停止连接并重新确认配对链接',
        );
      }
      final input = pending.capabilities.applyToHandshake(
        RelayE2eeHandshakeInput(
          kind: pending.kind,
          sessionId: pending.sessionId,
          clientId: pending.clientId,
          handshakeId: pending.handshakeId,
          relayProtocolVersion: 0,
          e2eeProtocolVersion: 0,
          tunnelProtocolVersion: 0,
          cryptoSuite: '',
          clientEphemeralPublicKey: pending.clientEphemeral.publicKey,
          nodeEphemeralPublicKey: agentHello.nodeEphemeralPublicKey,
          nodeIdentityPublicKey: agentHello.nodeIdentityPublicKey,
          deviceIdentityPublicKey:
              pending.deviceIdentity?.publicKey ?? const <int>[],
          requiresE2EE: false,
          plaintextTestMode: false,
          supportsMultiplexStreams: false,
          supportsFileDownload: false,
          supportsDeviceManagement: false,
        ),
      );
      final transcript = RelayE2eeHandshake.transcript(input);
      final signatureOk = await RelayDeviceIdentityStore.verifyWithPublicKey(
        publicKey: Uint8List.fromList(agentHello.nodeIdentityPublicKey),
        transcript: transcript,
        signature: Uint8List.fromList(agentHello.nodeSignature),
      );
      if (!signatureOk) {
        throw StateError('Relay E2EE 节点签名验证失败');
      }
      final proofFrame = await _relayE2EEProofFrame(pending, transcript);
      _ensureCurrentChannel(channel, epoch);
      final next = pending.copyWith(
        input: input,
        trafficKeys: await RelayE2eeHandshake.deriveTrafficKeys(
          privateScalar: pending.clientEphemeral.privateScalar,
          remotePublicKey:
              Uint8List.fromList(agentHello.nodeEphemeralPublicKey),
          input: input,
        ),
      );
      _ensureCurrentChannel(channel, epoch);
      updateState(next);
      _ensureCurrentChannel(channel, epoch);
      channel.sink.add(jsonEncode(proofFrame.toJson()));
    } catch (error, stackTrace) {
      if (!complete.isCompleted) {
        complete.completeError(error, stackTrace);
      }
    }
  }

  Future<RelayE2eeClientProofFrame> _relayE2EEProofFrame(
    _RelayE2eeHandshakeState pending,
    Uint8List transcript,
  ) async {
    if (pending.kind == relayE2eeHandshakeKindPairing) {
      return RelayE2eeClientProofFrame(
        sessionId: pending.sessionId,
        clientId: pending.clientId,
        handshakeId: pending.handshakeId,
        kind: pending.kind,
        pairingProof: await RelayE2eeHandshake.pairingProof(
          pairingSecret: pending.pairingSecret,
          transcript: transcript,
        ),
      );
    }
    final identity = pending.deviceIdentity;
    final credential = pending.deviceCredential;
    if (identity == null || credential == null) {
      throw StateError('Relay E2EE 设备凭证不存在');
    }
    return RelayE2eeClientProofFrame(
      sessionId: pending.sessionId,
      clientId: pending.clientId,
      handshakeId: pending.handshakeId,
      kind: pending.kind,
      deviceProof: await RelayE2eeHandshake.deviceProof(
        deviceCredential: credential.value,
        transcript: transcript,
      ),
      deviceSignature: await _relayDeviceIdentityStore.signTranscript(
        identity: identity,
        transcript: transcript,
      ),
    );
  }

  Map<String, dynamic> _relayAuthFrame({
    required String relaySessionId,
    required String relayPairingSecret,
    required String relayClientId,
    required String relayClientReconnectSecret,
  }) {
    if (relayPairingSecret.trim().isNotEmpty) {
      return <String, dynamic>{
        'type': 'client.pair',
        'version': 1,
        'sessionId': relaySessionId,
        'pairingSecret': relayPairingSecret,
        'deviceName': _defaultRelayDeviceName(),
      };
    }
    if (relayClientId.trim().isNotEmpty &&
        relayClientReconnectSecret.trim().isNotEmpty) {
      return <String, dynamic>{
        'type': 'client.reconnect',
        'version': 1,
        'sessionId': relaySessionId,
        'clientId': relayClientId,
        'clientReconnectSecret': relayClientReconnectSecret,
        'deviceName': _defaultRelayDeviceName(),
      };
    }
    return <String, dynamic>{
      'type': 'client.pair',
      'version': 1,
      'sessionId': relaySessionId,
      'pairingSecret': relayPairingSecret,
      'deviceName': _defaultRelayDeviceName(),
    };
  }

  void _queueRelayReceive({
    required WebSocketChannel channel,
    required Map<String, dynamic> frame,
    required int epoch,
    required void Function(String clientId, String clientReconnectSecret)
        setClientId,
  }) {
    _relayReceiveQueue = _relayReceiveQueue.then((_) async {
      if (epoch != _connectionEpoch || _channel != channel) {
        return;
      }
      final relayEvent = await _decodeRelayFrame(frame, setClientId, channel);
      if (relayEvent != null &&
          epoch == _connectionEpoch &&
          _channel == channel) {
        _events.add(_mapper.mapEvent(relayEvent));
      }
    }).catchError((Object error, StackTrace stackTrace) {
      if (!_isCurrentChannel(channel, epoch)) {
        return;
      }
      final code = error is StateError && error.message.contains('replay')
          ? 'e2ee_replay_detected'
          : 'e2ee_decrypt_failed';
      _events.add(ErrorEvent(
        timestamp: DateTime.now(),
        sessionId: '',
        runtimeMeta: const RuntimeMeta(),
        raw: <String, dynamic>{
          'type': 'error',
          'code': code,
          'msg': 'Relay E2EE receive failed: $error',
          'stack': stackTrace.toString(),
        },
        code: code,
        message: relayErrorMessage(<String, dynamic>{
          'type': 'relay.error',
          'code': code,
        }),
      ));
      unawaited(disconnect());
    });
  }

  void _queueRelaySend({
    required WebSocketChannel channel,
    required Map<String, dynamic> payload,
    required _RelayContext relayContext,
    required int epoch,
  }) {
    _relaySendQueue = _relaySendQueue.then((_) async {
      if (epoch != _connectionEpoch || _channel != channel) {
        return;
      }
      final frame = await _encodeRelayFrame(payload, relayContext);
      if (epoch != _connectionEpoch || _channel != channel) {
        return;
      }
      channel.sink.add(jsonEncode(frame));
    }).catchError((Object error, StackTrace stackTrace) {
      if (!_isCurrentChannel(channel, epoch)) {
        return;
      }
      _events.add(ErrorEvent(
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
      ));
      unawaited(disconnect());
    });
  }

  void _queueRelayDeviceRegister({
    required WebSocketChannel channel,
    required _RelayE2eeHandshakeState state,
    required int epoch,
  }) {
    _relaySendQueue = _relaySendQueue.then((_) async {
      if (epoch != _connectionEpoch || _channel != channel) {
        return;
      }
      final identity = await _relayDeviceIdentityStore.loadOrCreate();
      final credential = await _relayDeviceCredentialStore.loadOrCreate();
      final frame = await state.streamCodec!.encodeJson(
        messageId: 'msg_${const Uuid().v4()}',
        payload: <String, dynamic>{
          'action': 'relay_device_register',
          'deviceId': identity.fullFingerprintHex,
          'displayName': _defaultRelayDeviceName(),
          'deviceIdentityPublicKey': encodeRelayFrameBytes(identity.publicKey),
          'deviceCredential': credential.value,
        },
      );
      if (epoch != _connectionEpoch || _channel != channel) {
        return;
      }
      channel.sink.add(jsonEncode(frame));
    }).catchError((Object error, StackTrace stackTrace) {
      if (!_isCurrentChannel(channel, epoch)) {
        return;
      }
      _events.add(ErrorEvent(
        timestamp: DateTime.now(),
        sessionId: '',
        runtimeMeta: const RuntimeMeta(),
        raw: <String, dynamic>{
          'type': 'error',
          'code': 'device_unknown',
          'msg': 'Relay device registration failed: $error',
          'stack': stackTrace.toString(),
        },
        code: 'device_unknown',
        message: 'Relay 设备绑定失败：$error',
      ));
      unawaited(disconnect());
    });
  }

  Future<Map<String, dynamic>> _encodeRelayFrame(
    Map<String, dynamic> payload,
    _RelayContext relayContext,
  ) async {
    final codec = _relayE2eeState?.streamCodec;
    if (codec != null) {
      return codec.encodeJson(
        messageId: 'msg_${const Uuid().v4()}',
        payload: payload,
      );
    }
    if (relayContext.requiresE2EE) {
      throw StateError('Relay E2EE stream is not ready');
    }
    return <String, dynamic>{
      'type': 'relay.forward',
      'version': 1,
      'sessionId': relayContext.sessionId,
      'clientId': relayContext.clientIdProvider(),
      'direction': 'client_to_agent',
      'messageId': 'msg_${const Uuid().v4()}',
      'contentType': 'mobilevc.ws.v1',
      'encryption': 'none',
      'payloadEncoding': 'base64url',
      'payload': base64UrlEncode(utf8.encode(jsonEncode(payload))),
    };
  }
}

bool _requiresRelayE2EE({
  required RelayE2eeCapabilitySet? capabilities,
}) {
  if (capabilities == null) {
    return false;
  }
  capabilities.validateRelayMode();
  return capabilities.requiresE2EE && !capabilities.plaintextTestMode;
}

void _validateRelayE2EEConnectMode({
  required String relayPairingSecret,
  required String relayClientId,
  required String relayClientReconnectSecret,
  required RelayE2eeCapabilitySet? capabilities,
}) {
  if (capabilities == null) {
    return;
  }
  capabilities.validateRelayMode();
  if (!capabilities.requiresE2EE || capabilities.plaintextTestMode) {
    return;
  }
  if (relayPairingSecret.trim().isNotEmpty) {
    return;
  }
  if (relayClientId.trim().isNotEmpty &&
      relayClientReconnectSecret.trim().isNotEmpty) {
    return;
  }
  throw const FormatException('Relay E2EE 配对信息不完整，请重新扫码');
}

bool _isRelayE2EEFrame(Map<String, dynamic> frame) {
  final type = (frame['type'] ?? '').toString();
  return type == relayFrameAgentE2eeHello || type == relayFrameAgentE2eeResult;
}

bool _isRelayForwardFrame(Map<String, dynamic> frame) {
  return (frame['type'] ?? '').toString() == relayForwardType;
}

int _relayFrameStreamId(Map<String, dynamic> frame) {
  final value = frame['streamId'];
  if (value is int) {
    return value;
  }
  if (value is num) {
    return value.toInt();
  }
  return int.tryParse(value?.toString() ?? '') ?? 0;
}

String _hex(Uint8List bytes) {
  final buffer = StringBuffer();
  for (final byte in bytes) {
    buffer.write(byte.toRadixString(16).padLeft(2, '0'));
  }
  return buffer.toString();
}

String _defaultRelayDeviceName() => defaultTargetPlatform == TargetPlatform.iOS
    ? 'iPhone'
    : defaultTargetPlatform == TargetPlatform.android
        ? 'Android Device'
        : 'MobileVC Device';

class _RelayE2eeHandshakeState {
  const _RelayE2eeHandshakeState({
    required this.kind,
    required this.sessionId,
    required this.clientId,
    required this.handshakeId,
    required this.pairingSecret,
    required this.expectedNodeFingerprintHex,
    required this.capabilities,
    required this.clientEphemeral,
    this.deviceIdentity,
    this.deviceCredential,
    this.input,
    this.trafficKeys,
    this.streamCodec,
    this.boundDeviceId = '',
    this.complete = false,
  });

  final String kind;
  final String sessionId;
  final String clientId;
  final String handshakeId;
  final String pairingSecret;
  final String expectedNodeFingerprintHex;
  final RelayE2eeCapabilitySet capabilities;
  final RelayE2eeEphemeralKeyPair clientEphemeral;
  final RelayDeviceIdentity? deviceIdentity;
  final RelayDeviceCredential? deviceCredential;
  final RelayE2eeHandshakeInput? input;
  final RelayE2eeTrafficKeys? trafficKeys;
  final RelayMobileVcStreamCodec? streamCodec;
  final String boundDeviceId;
  final bool complete;

  bool matches(String sessionId, String clientId, String handshakeId) {
    return this.sessionId == sessionId &&
        this.clientId == clientId &&
        this.handshakeId == handshakeId;
  }

  _RelayE2eeHandshakeState copyWith({
    RelayE2eeHandshakeInput? input,
    RelayE2eeTrafficKeys? trafficKeys,
  }) {
    return _RelayE2eeHandshakeState(
      kind: kind,
      sessionId: sessionId,
      clientId: clientId,
      handshakeId: handshakeId,
      pairingSecret: pairingSecret,
      expectedNodeFingerprintHex: expectedNodeFingerprintHex,
      capabilities: capabilities,
      clientEphemeral: clientEphemeral,
      deviceIdentity: deviceIdentity,
      deviceCredential: deviceCredential,
      input: input ?? this.input,
      trafficKeys: trafficKeys ?? this.trafficKeys,
      streamCodec: streamCodec,
      boundDeviceId: boundDeviceId,
      complete: complete,
    );
  }

  _RelayE2eeHandshakeState copyWithBoundDeviceId(String deviceId) {
    return _RelayE2eeHandshakeState(
      kind: kind,
      sessionId: sessionId,
      clientId: clientId,
      handshakeId: handshakeId,
      pairingSecret: pairingSecret,
      expectedNodeFingerprintHex: expectedNodeFingerprintHex,
      capabilities: capabilities,
      clientEphemeral: clientEphemeral,
      deviceIdentity: deviceIdentity,
      deviceCredential: deviceCredential,
      input: input,
      trafficKeys: trafficKeys,
      streamCodec: streamCodec,
      boundDeviceId: deviceId.trim(),
      complete: complete,
    );
  }

  _RelayE2eeHandshakeState markComplete() {
    final keys = trafficKeys;
    if (keys == null) {
      throw StateError('Relay E2EE traffic keys missing');
    }
    return _RelayE2eeHandshakeState(
      kind: kind,
      sessionId: sessionId,
      clientId: clientId,
      handshakeId: handshakeId,
      pairingSecret: pairingSecret,
      expectedNodeFingerprintHex: expectedNodeFingerprintHex,
      capabilities: capabilities,
      clientEphemeral: clientEphemeral,
      deviceIdentity: deviceIdentity,
      deviceCredential: deviceCredential,
      input: input,
      trafficKeys: keys,
      streamCodec: RelayMobileVcStreamCodec.client(
        sessionId: sessionId,
        clientId: clientId,
        handshakeId: handshakeId,
        keys: keys,
      ),
      boundDeviceId: boundDeviceId.trim().isNotEmpty
          ? boundDeviceId
          : kind == relayE2eeHandshakeKindReconnect
              ? deviceIdentity?.fullFingerprintHex ?? ''
              : '',
      complete: true,
    );
  }
}

class RelaySession {
  const RelaySession({
    required this.sessionId,
    required this.clientId,
    required this.clientReconnectSecret,
  });

  final String sessionId;
  final String clientId;
  final String clientReconnectSecret;
}

class RelayFileDownloadResult {
  const RelayFileDownloadResult({
    required this.fileName,
    this.bytes,
    this.contentType = '',
    this.totalBytes,
  });

  final Uint8List? bytes;
  final String fileName;
  final String contentType;
  final int? totalBytes;
}

class RelayFileDownloadCancelToken {
  bool _cancelled = false;
  Future<void> Function()? _onCancel;

  bool get isCancelled => _cancelled;

  void cancel() {
    if (_cancelled) {
      return;
    }
    _cancelled = true;
    final callback = _onCancel;
    if (callback != null) {
      unawaited(callback());
    }
  }

  void _bind(Future<void> Function() onCancel) {
    _onCancel = onCancel;
    if (_cancelled) {
      unawaited(onCancel());
    }
  }

  void _unbind() {
    _onCancel = null;
  }
}

class _RelayFileDownloadState {
  _RelayFileDownloadState({
    required this.streamId,
    required this.fallbackFileName,
    this.onProgress,
    this.onChunk,
  });

  final int streamId;
  final String fallbackFileName;
  final void Function(int receivedBytes, int? totalBytes)? onProgress;
  final FutureOr<void> Function(Uint8List chunk)? onChunk;
  final Completer<RelayFileDownloadResult> result =
      Completer<RelayFileDownloadResult>();
  final BytesBuilder _bytes = BytesBuilder(copy: false);
  int _receivedBytes = 0;
  String fileName = '';
  String contentType = '';
  int? totalBytes;

  void applyOpen(RelayTunnelFrame frame) {
    fileName = (frame.metadata['fileName'] ?? '').trim();
    contentType = (frame.metadata['contentType'] ?? '').trim();
    totalBytes = int.tryParse((frame.metadata['size'] ?? '').trim());
    onProgress?.call(_receivedBytes, totalBytes);
  }

  Future<void> addChunk(List<int> chunk) async {
    final immutableChunk = Uint8List.fromList(chunk);
    if (onChunk == null) {
      _bytes.add(immutableChunk);
    } else {
      await onChunk!(immutableChunk);
    }
    _receivedBytes += immutableChunk.length;
    onProgress?.call(_receivedBytes, totalBytes);
  }

  void complete() {
    if (result.isCompleted) {
      return;
    }
    result.complete(RelayFileDownloadResult(
      bytes: onChunk == null ? _bytes.takeBytes() : null,
      fileName: fileName.isNotEmpty ? fileName : fallbackFileName,
      contentType: contentType,
      totalBytes: totalBytes,
    ));
  }

  void completeError(Object error) {
    if (!result.isCompleted) {
      result.completeError(error);
    }
  }
}

class _RelayContext {
  const _RelayContext({
    this.sessionId = '',
    this.clientIdProvider = _emptyClientId,
    this.requiresE2EE = false,
  });

  final String sessionId;
  final String Function() clientIdProvider;
  final bool requiresE2EE;
}

String _emptyClientId() => '';

@visibleForTesting
bool isRelayPairingError(Map<String, dynamic> frame, String clientId) =>
    (frame['type'] ?? '').toString() == 'relay.error' && clientId.isEmpty;

@visibleForTesting
bool isRelayAuthError(Map<String, dynamic> frame, bool authPending) =>
    authPending && (frame['type'] ?? '').toString() == 'relay.error';

String relayErrorMessage(Map<String, dynamic> frame) =>
    switch ((frame['code'] ?? '').toString()) {
      'payload_too_large' => 'Relay 数据包过大：当前消息超过中继限制，请关闭 ADB 截屏流或减少单次同步内容后重试',
      'agent_disconnected' => '本机 Relay 正在重连，请稍后自动恢复',
      'e2ee_required' => 'Relay 已禁用明文连接：请更新手机端和本机服务，重新配对并启用 E2EE',
      'e2ee_unsupported_version' => 'Relay E2EE 版本不兼容：请更新手机端和本机服务后重新连接',
      'e2ee_fingerprint_mismatch' => 'Relay 节点指纹不一致：请停止连接并重新确认配对链接',
      'e2ee_handshake_failed' => 'Relay E2EE 握手失败：请重新配对，确认链接未过期',
      'e2ee_decrypt_failed' => 'Relay E2EE 解密失败：消息认证未通过，请重新连接',
      'e2ee_replay_detected' => 'Relay 检测到重复或过期的加密消息：请重新连接',
      'device_revoked' => '此设备已被本机撤销：请在本机重新授权后再配对',
      'device_unknown' => '此设备未绑定或本机身份已轮换：请重新配对',
      'stream_cancelled' => 'Relay 加密流已取消',
      'stream_window_exceeded' => 'Relay 加密流窗口超限：请重试或减少并发传输',
      'download_denied' => '本机文件策略拒绝下载：请选择允许的工作区文件',
      'download_failed' => 'Relay 加密下载失败：请重试并查看本机日志',
      _ => (frame['message'] ?? 'Relay error').toString(),
    };

class RelayPairingException implements Exception {
  const RelayPairingException(this.code, this.message);

  factory RelayPairingException.fromFrame(Map<String, dynamic> frame) {
    final code = (frame['code'] ?? 'relay_error').toString();
    if (code == 'pairing_rejected') {
      return const RelayPairingException(
        'pairing_rejected',
        'Relay 认证被拒绝：链接可能已过期，或本机服务已重启，请导入最新中继链接',
      );
    }
    return RelayPairingException(code, relayErrorMessage(frame));
  }

  final String code;
  final String message;

  @override
  String toString() => message;
}
