import 'dart:async';
import 'dart:convert';
import 'dart:io';
import 'dart:math';

import 'package:flutter_test/flutter_test.dart';
import 'package:flutter/foundation.dart';
import 'package:shared_preferences/shared_preferences.dart';
import 'package:mobile_vc/core/config/app_config.dart';
import 'package:mobile_vc/core/config/relay_config.dart';
import 'package:mobile_vc/core/relay_e2ee/relay_e2ee_capability.dart';
import 'package:mobile_vc/core/relay_e2ee/relay_device_identity.dart';
import 'package:mobile_vc/core/relay_e2ee/relay_e2ee_crypto.dart';
import 'package:mobile_vc/core/relay_e2ee/relay_e2ee_handshake.dart';
import 'package:mobile_vc/core/relay_e2ee/relay_e2ee_handshake_frames.dart';
import 'package:mobile_vc/core/relay_e2ee/relay_file_download.dart';
import 'package:mobile_vc/core/relay_e2ee/relay_tunnel.dart';
import 'package:mobile_vc/core/relay_e2ee/relay_mobilevc_stream.dart';
import 'package:mobile_vc/core/relay_e2ee/relay_security_state.dart';
import 'package:mobile_vc/data/models/events.dart';
import 'package:mobile_vc/data/models/runtime_meta.dart';
import 'package:mobile_vc/data/models/session_models.dart';
import 'package:mobile_vc/data/services/mobilevc_ws_service.dart';
import 'package:mobile_vc/features/session/session_controller.dart';

Future<void> _flushEvents() async {
  await Future<void>.delayed(const Duration(milliseconds: 1));
  await Future<void>.delayed(const Duration(milliseconds: 1));
}

Future<void> _servePairingE2eeRelay(
  WebSocket socket,
  RelayE2eeEphemeralKeyPair nodeIdentity,
  List<Map<String, dynamic>>? observedForwards,
  List<Map<String, dynamic>>? observedPlaintexts, {
  List<Map<String, dynamic>>? observedControls,
  bool keepDownloadOpen = false,
  String downloadErrorCode = '',
}) async {
  final capabilities = RelayE2eeCapabilitySet.production();
  RelayMobileVcStreamCodec? codec;
  RelayE2eeHandshakeInput? handshakeInput;
  RelayE2eeEphemeralKeyPair? nodeEphemeralKey;
  await for (final raw in socket) {
    final frame = jsonDecode(raw as String) as Map<String, dynamic>;
    observedControls?.add(Map<String, dynamic>.from(frame));
    switch ((frame['type'] ?? '').toString()) {
      case 'client.pair':
        socket.add(jsonEncode(const <String, dynamic>{
          'type': 'client.paired',
          'version': 1,
          'sessionId': 'rs_test',
          'clientId': 'rc_test',
          'clientReconnectSecret': 'reconnect_secret',
        }));
      case relayFrameClientE2eeHello:
        final hello = RelayE2eeClientHelloFrame.fromJson(frame);
        final nodeEphemeral = await RelayE2eeHandshake.newEphemeralKeyPair();
        nodeEphemeralKey = nodeEphemeral;
        final input = capabilities.applyToHandshake(
          RelayE2eeHandshakeInput(
            kind: relayE2eeHandshakeKindPairing,
            sessionId: hello.sessionId,
            clientId: hello.clientId,
            handshakeId: hello.handshakeId,
            relayProtocolVersion: 0,
            e2eeProtocolVersion: 0,
            tunnelProtocolVersion: 0,
            cryptoSuite: '',
            clientEphemeralPublicKey: hello.clientEphemeralPublicKey,
            nodeEphemeralPublicKey: nodeEphemeral.publicKey,
            nodeIdentityPublicKey: nodeIdentity.publicKey,
            requiresE2EE: false,
            plaintextTestMode: false,
            supportsMultiplexStreams: false,
            supportsFileDownload: false,
            supportsDeviceManagement: false,
          ),
        );
        handshakeInput = input;
        final transcript = RelayE2eeHandshake.transcript(input);
        final signature = await RelayDeviceIdentityStore.signWithPrivateScalar(
          privateScalar: nodeIdentity.privateScalar,
          transcript: transcript,
        );
        socket.add(jsonEncode(RelayE2eeAgentHelloFrame(
          sessionId: hello.sessionId,
          clientId: hello.clientId,
          handshakeId: hello.handshakeId,
          capabilities: capabilities,
          nodeEphemeralPublicKey: nodeEphemeral.publicKey,
          nodeIdentityPublicKey: nodeIdentity.publicKey,
          nodeSignature: signature,
        ).toJson()));
      case relayFrameClientE2eeProof:
        final proof = RelayE2eeClientProofFrame.fromJson(frame);
        final input = handshakeInput;
        final nodeEphemeral = nodeEphemeralKey;
        if (input == null || nodeEphemeral == null) {
          throw StateError('test relay handshake input missing');
        }
        codec = RelayMobileVcStreamCodec.agent(
          sessionId: proof.sessionId,
          clientId: proof.clientId,
          handshakeId: proof.handshakeId,
          keys: await RelayE2eeHandshake.deriveTrafficKeys(
            privateScalar: nodeEphemeral.privateScalar,
            remotePublicKey: Uint8List.fromList(input.clientEphemeralPublicKey),
            input: input,
          ),
        );
        socket.add(jsonEncode(RelayE2eeAgentResultFrame(
          sessionId: proof.sessionId,
          clientId: proof.clientId,
          handshakeId: proof.handshakeId,
          ok: true,
        ).toJson()));
      case relayForwardType:
        observedForwards?.add(Map<String, dynamic>.from(frame));
        final relayCodec = codec;
        if (relayCodec == null) {
          throw StateError('test relay e2ee codec missing');
        }
        final streamId = (frame['streamId'] as num?)?.toInt() ?? 0;
        if (streamId != relayMobileVcStreamId) {
          final tunnelFrame = await relayCodec.decodeTunnelFrame(frame);
          observedPlaintexts?.add(tunnelFrame.toJson());
          if (tunnelFrame.type == tunnelFrameStreamReset) {
            break;
          }
          if (tunnelFrame.type == tunnelFrameStreamOpen &&
              tunnelFrame.streamType == tunnelStreamFileDownload) {
            if (downloadErrorCode.isNotEmpty) {
              socket.add(jsonEncode(await relayCodec.encodeTunnelFrame(
                messageId: 'msg_download_error',
                frame: RelayTunnelFrame(
                  type: tunnelFrameStreamError,
                  streamId: tunnelFrame.streamId,
                  errorCode: downloadErrorCode,
                  metadata: const <String, String>{
                    'message': 'selected route rejected',
                  },
                ),
              )));
              continue;
            }
            socket.add(jsonEncode(await relayCodec.encodeTunnelFrame(
              messageId: 'msg_download_open',
              frame: relayFileDownloadOpenFrame(
                streamId: tunnelFrame.streamId,
                metadata: const RelayFileDownloadMetadata(
                  path: '/workspace/secret.txt',
                  fileName: 'secret.txt',
                  contentType: 'text/plain',
                  size: 11,
                ),
                window: 4,
              ),
            )));
            if (keepDownloadOpen) {
              continue;
            }
            socket.add(jsonEncode(await relayCodec.encodeTunnelFrame(
              messageId: 'msg_download_data',
              frame: relayFileDownloadDataFrame(
                streamId: tunnelFrame.streamId,
                seq: 1,
                chunk: utf8.encode('hello relay'),
              ),
            )));
            socket.add(jsonEncode(await relayCodec.encodeTunnelFrame(
              messageId: 'msg_download_close',
              frame: relayFileDownloadCloseFrame(
                streamId: tunnelFrame.streamId,
                seq: 2,
              ),
            )));
          }
          break;
        }
        final decoded = await relayCodec.decodeJson(frame);
        observedPlaintexts?.add(Map<String, dynamic>.from(decoded));
        if (decoded['type'] == 'ping') {
          socket.add(jsonEncode(await relayCodec.encodeJson(
            messageId: 'msg_server_ack',
            payload: <String, dynamic>{
              'type': 'pong',
              'sessionId': 'session-test',
              'pingId': (decoded['pingId'] ?? '').toString(),
            },
          )));
        }
    }
  }
}

Future<void> _serveReconnectE2eeRelay({
  required WebSocket socket,
  required RelayE2eeEphemeralKeyPair nodeIdentity,
  required Map<String, dynamic> trustedDevice,
  List<Map<String, dynamic>>? observedForwards,
  List<Map<String, dynamic>>? observedPlaintexts,
  List<Map<String, dynamic>>? observedControls,
  String resultErrorCode = '',
}) async {
  final capabilities = RelayE2eeCapabilitySet.production();
  RelayMobileVcStreamCodec? codec;
  RelayE2eeHandshakeInput? handshakeInput;
  RelayE2eeEphemeralKeyPair? nodeEphemeralKey;
  Uint8List? nodeSignature;
  await for (final raw in socket) {
    final frame = jsonDecode(raw as String) as Map<String, dynamic>;
    observedControls?.add(Map<String, dynamic>.from(frame));
    switch ((frame['type'] ?? '').toString()) {
      case 'client.pair':
        socket.add(jsonEncode(const <String, dynamic>{
          'type': 'client.paired',
          'version': 1,
          'sessionId': 'rs_test',
          'clientId': 'rc_test',
          'clientReconnectSecret': 'reconnect_secret',
        }));
      case 'client.reconnect':
        socket.add(jsonEncode(const <String, dynamic>{
          'type': 'client.paired',
          'version': 1,
          'sessionId': 'rs_test',
          'clientId': 'rc_test',
        }));
      case relayFrameClientE2eeHello:
        final hello = RelayE2eeClientHelloFrame.fromJson(frame);
        expect(hello.kind, relayE2eeHandshakeKindReconnect);
        expect(hello.deviceId, trustedDevice['deviceId']);
        final nodeEphemeral = await RelayE2eeHandshake.newEphemeralKeyPair();
        nodeEphemeralKey = nodeEphemeral;
        final input = capabilities.applyToHandshake(
          RelayE2eeHandshakeInput(
            kind: relayE2eeHandshakeKindReconnect,
            sessionId: hello.sessionId,
            clientId: hello.clientId,
            handshakeId: hello.handshakeId,
            relayProtocolVersion: 0,
            e2eeProtocolVersion: 0,
            tunnelProtocolVersion: 0,
            cryptoSuite: '',
            clientEphemeralPublicKey: hello.clientEphemeralPublicKey,
            nodeEphemeralPublicKey: nodeEphemeral.publicKey,
            nodeIdentityPublicKey: nodeIdentity.publicKey,
            deviceIdentityPublicKey: hello.deviceIdentityPublicKey,
            requiresE2EE: false,
            plaintextTestMode: false,
            supportsMultiplexStreams: false,
            supportsFileDownload: false,
            supportsDeviceManagement: false,
          ),
        );
        handshakeInput = input;
        final transcript = RelayE2eeHandshake.transcript(input);
        final signature = await RelayDeviceIdentityStore.signWithPrivateScalar(
          privateScalar: nodeIdentity.privateScalar,
          transcript: transcript,
        );
        nodeSignature = signature;
        socket.add(jsonEncode(RelayE2eeAgentHelloFrame(
          sessionId: hello.sessionId,
          clientId: hello.clientId,
          handshakeId: hello.handshakeId,
          capabilities: capabilities,
          nodeEphemeralPublicKey: nodeEphemeral.publicKey,
          nodeIdentityPublicKey: nodeIdentity.publicKey,
          nodeSignature: signature,
        ).toJson()));
      case relayFrameClientE2eeProof:
        final proof = RelayE2eeClientProofFrame.fromJson(frame);
        final input = handshakeInput;
        final nodeEphemeral = nodeEphemeralKey;
        final signature = nodeSignature;
        if (input == null || nodeEphemeral == null || signature == null) {
          throw StateError('test reconnect handshake input missing');
        }
        if (resultErrorCode.isEmpty) {
          await RelayE2eeHandshake.validateReconnectHandshake(
            input: input,
            deviceCredential: trustedDevice['deviceCredential'] as String,
            deviceProof: Uint8List.fromList(proof.deviceProof),
            nodeSignature: signature,
            deviceSignature: Uint8List.fromList(proof.deviceSignature),
          );
          codec = RelayMobileVcStreamCodec.agent(
            sessionId: proof.sessionId,
            clientId: proof.clientId,
            handshakeId: proof.handshakeId,
            keys: await RelayE2eeHandshake.deriveTrafficKeys(
              privateScalar: nodeEphemeral.privateScalar,
              remotePublicKey:
                  Uint8List.fromList(input.clientEphemeralPublicKey),
              input: input,
            ),
          );
        }
        socket.add(jsonEncode(RelayE2eeAgentResultFrame(
          sessionId: proof.sessionId,
          clientId: proof.clientId,
          handshakeId: proof.handshakeId,
          ok: resultErrorCode.isEmpty,
          errorCode: resultErrorCode,
        ).toJson()));
      case relayForwardType:
        observedForwards?.add(Map<String, dynamic>.from(frame));
        final relayCodec = codec;
        if (relayCodec == null) {
          throw StateError('test reconnect e2ee codec missing');
        }
        final decoded = await relayCodec.decodeJson(frame);
        observedPlaintexts?.add(Map<String, dynamic>.from(decoded));
        if (decoded['type'] == 'ping') {
          socket.add(jsonEncode(await relayCodec.encodeJson(
            messageId: 'msg_reconnect_ack',
            payload: <String, dynamic>{
              'type': 'pong',
              'sessionId': 'session-reconnect',
              'pingId': (decoded['pingId'] ?? '').toString(),
            },
          )));
        }
    }
  }
}

MobileVcWsService _testRelayService({int seed = 101}) {
  final store = _MemoryRelaySecureStore();
  return MobileVcWsService(
    relayDeviceIdentityStore: RelayDeviceIdentityStore(
      secureStore: store,
      random: Random(seed),
    ),
    relayDeviceCredentialStore: RelayDeviceCredentialStore(
      secureStore: store,
      random: Random(seed + 1),
    ),
  );
}

class _MemoryRelaySecureStore implements RelaySecureStore {
  final Map<String, String> values = <String, String>{};

  @override
  Future<String?> read(String key) async => values[key];

  @override
  Future<void> write(String key, String value) async {
    values[key] = value;
  }

  @override
  Future<void> delete(String key) async {
    values.remove(key);
  }
}

class _BlockingFirstReadRelaySecureStore implements RelaySecureStore {
  _BlockingFirstReadRelaySecureStore(this.delegate);

  final _MemoryRelaySecureStore delegate;
  final Completer<void> firstReadStarted = Completer<void>();
  final Completer<void> releaseFirstRead = Completer<void>();
  var _blocked = false;

  @override
  Future<String?> read(String key) async {
    if (!_blocked) {
      _blocked = true;
      if (!firstReadStarted.isCompleted) {
        firstReadStarted.complete();
      }
      await releaseFirstRead.future;
    }
    return delegate.read(key);
  }

  @override
  Future<void> write(String key, String value) => delegate.write(key, value);

  @override
  Future<void> delete(String key) => delegate.delete(key);
}

String _testHex(Uint8List bytes) {
  final buffer = StringBuffer();
  for (final byte in bytes) {
    buffer.write(byte.toRadixString(16).padLeft(2, '0'));
  }
  return buffer.toString();
}

Future<String> _testFingerprintHex(Uint8List publicKey) async {
  return _testHex(await RelayE2eeCrypto.fingerprint(publicKey));
}

Future<Map<String, dynamic>> _waitForRelayDeviceRegistration(
  List<Map<String, dynamic>> observed,
) async {
  for (var i = 0; i < 100; i++) {
    final matches =
        observed.where((event) => event['action'] == 'relay_device_register');
    if (matches.isNotEmpty) {
      return matches.single;
    }
    await Future<void>.delayed(const Duration(milliseconds: 10));
  }
  throw StateError('relay device registration was not observed');
}

Future<void> _waitForObservedFrame(
  List<Map<String, dynamic>> observed,
  bool Function(Map<String, dynamic>) matches,
) async {
  for (var i = 0; i < 100; i++) {
    if (observed.any(matches)) {
      return;
    }
    await Future<void>.delayed(const Duration(milliseconds: 10));
  }
  throw StateError('expected relay frame was not observed');
}

ActionNeededSignal _expectSignal(
    SessionController controller, ActionNeededType type) {
  final signal = controller.actionNeededSignal;
  expect(signal, isNotNull);
  expect(signal!.type, type);
  return signal;
}

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();
  setUp(() {
    SharedPreferences.setMockInitialValues({});
  });

  group('shouldPreserveAdbFailureStatus', () {
    test('keeps detailed TURN and ICE diagnostics', () {
      expect(
        shouldPreserveAdbFailureStatus(
          'TURN 未返回服务端 relay 候选，请检查 TURN 的 external-ip、3478/UDP、3478/TCP 与凭据配置',
        ),
        isTrue,
      );
      expect(
        shouldPreserveAdbFailureStatus('服务端 ICE 状态：failed'),
        isTrue,
      );
      expect(
        shouldPreserveAdbFailureStatus(
          '连接态 统计: relay/udp@8.162.1.176:49188 -> relay/udp@1.2.3.4:55555',
        ),
        isTrue,
      );
    });

    test('does not keep generic progress text', () {
      expect(shouldPreserveAdbFailureStatus(''), isFalse);
      expect(shouldPreserveAdbFailureStatus('WebRTC 连接中…'), isFalse);
      expect(
          shouldPreserveAdbFailureStatus('WebRTC answer 已收到，等待连接…'), isFalse);
    });
  });

  group('MobileVcWsService reconnect semantics', () {
    test('relay pairing rejected message is actionable', () {
      final error = RelayPairingException.fromFrame(const <String, dynamic>{
        'type': 'relay.error',
        'code': 'pairing_rejected',
        'message': 'pairing rejected',
      });

      expect(error.code, 'pairing_rejected');
      expect(error.toString(), contains('Relay 认证被拒绝'));
      expect(error.toString(), isNot(contains('Bad state')));
    });

    test('relay payload too large message is actionable', () {
      final message = relayErrorMessage(const <String, dynamic>{
        'type': 'relay.error',
        'code': 'payload_too_large',
        'message': 'payload too large',
      });

      expect(message, contains('Relay 数据包过大'));
      expect(message, contains('ADB 截屏流'));
      expect(message, isNot(contains('payload too large')));
    });

    test('relay e2ee errors are actionable', () {
      expect(
        relayErrorMessage(const <String, dynamic>{
          'type': 'relay.error',
          'code': 'e2ee_required',
          'message': 'e2ee required',
        }),
        contains('禁用明文连接'),
      );
      expect(
        relayErrorMessage(const <String, dynamic>{
          'type': 'relay.error',
          'code': 'e2ee_unsupported_version',
          'message': 'e2ee unsupported version',
        }),
        contains('版本不兼容'),
      );
      expect(
        relayErrorMessage(const <String, dynamic>{
          'type': 'relay.error',
          'code': 'device_revoked',
          'message': 'device revoked',
        }),
        contains('已被本机撤销'),
      );
    });

    test('relay agent disconnected message is reconnectable', () {
      final message = relayErrorMessage(const <String, dynamic>{
        'type': 'relay.error',
        'code': 'agent_disconnected',
        'message': 'agent disconnected',
      });

      expect(message, contains('正在重连'));
      expect(message, isNot(contains('重新配对')));
    });

    test('relay auth error is pending for reconnect attempts', () {
      expect(
        isRelayAuthError(
          const <String, dynamic>{
            'type': 'relay.error',
            'code': 'pairing_rejected',
          },
          true,
        ),
        isTrue,
      );
      expect(
        isRelayAuthError(
          const <String, dynamic>{
            'type': 'relay.error',
            'code': 'payload_too_large',
          },
          false,
        ),
        isFalse,
      );
    });

    test('relay reconnect rejection surfaces before timeout', () async {
      final server = await HttpServer.bind(InternetAddress.loopbackIPv4, 0);
      addTearDown(server.close);
      server.transform(WebSocketTransformer()).listen((socket) {
        socket.listen((_) {
          socket.add(jsonEncode(const <String, dynamic>{
            'type': 'relay.error',
            'version': 1,
            'code': 'pairing_rejected',
            'message': 'pairing rejected',
          }));
        });
        addTearDown(socket.close);
      });
      final service = MobileVcWsService();
      addTearDown(service.dispose);

      expect(
        service.connectRelay(
          relayUrl: 'ws://127.0.0.1:${server.port}',
          sessionId: 'rs_test',
          clientId: 'rc_test',
          clientReconnectSecret: 'bad_secret',
        ),
        throwsA(isA<RelayPairingException>()),
      );
    });

    test('production E2EE relay reconnect performs device rekey', () async {
      final nodeIdentity = await RelayE2eeHandshake.newEphemeralKeyPair();
      final nodeFingerprint = await RelayE2eeCrypto.fingerprint(
        nodeIdentity.publicKey,
      );
      final server = await HttpServer.bind(InternetAddress.loopbackIPv4, 0);
      addTearDown(server.close);
      final observedPairingPlaintexts = <Map<String, dynamic>>[];
      server.transform(WebSocketTransformer()).listen((socket) {
        _servePairingE2eeRelay(
          socket,
          nodeIdentity,
          null,
          observedPairingPlaintexts,
        );
      });
      final service = _testRelayService();
      addTearDown(service.dispose);
      final relayUrl = 'ws://127.0.0.1:${server.port}';

      await service.connectRelay(
        relayUrl: relayUrl,
        sessionId: 'rs_test',
        pairingSecret: 'pair-secret-128-bit-minimum',
        nodeFingerprintHex: _testHex(nodeFingerprint),
        relayCapabilities: RelayE2eeCapabilitySet.production(),
      );
      final relaySession = service.takeRelaySession();
      expect(relaySession, isNotNull);
      final trustedDevice =
          await _waitForRelayDeviceRegistration(observedPairingPlaintexts);
      await service.disconnect();

      final reconnectServer =
          await HttpServer.bind(InternetAddress.loopbackIPv4, 0);
      addTearDown(reconnectServer.close);
      final observedControls = <Map<String, dynamic>>[];
      final observedForwards = <Map<String, dynamic>>[];
      final observedPlaintexts = <Map<String, dynamic>>[];
      reconnectServer.transform(WebSocketTransformer()).listen((socket) {
        _serveReconnectE2eeRelay(
          socket: socket,
          nodeIdentity: nodeIdentity,
          trustedDevice: trustedDevice,
          observedControls: observedControls,
          observedForwards: observedForwards,
          observedPlaintexts: observedPlaintexts,
        );
      });
      final events = <AppEvent>[];
      final subscription = service.events.listen(events.add);
      addTearDown(subscription.cancel);

      await service.connectRelay(
        relayUrl: 'ws://127.0.0.1:${reconnectServer.port}',
        sessionId: 'rs_test',
        clientId: relaySession!.clientId,
        clientReconnectSecret: relaySession.clientReconnectSecret,
        nodeFingerprintHex: _testHex(nodeFingerprint),
        relayCapabilities: RelayE2eeCapabilitySet.production(),
      );

      expect(service.hasRelayE2eeHandshake, isTrue);
      expect(service.send(const <String, dynamic>{'type': 'ping'}), isTrue);
      await Future<void>.delayed(const Duration(milliseconds: 100));

      final deviceCredential = trustedDevice['deviceCredential'] as String;
      expect(
        observedControls
            .any((frame) => frame.toString().contains(deviceCredential)),
        isFalse,
      );
      expect(observedForwards, hasLength(1));
      expect(observedForwards.single['encryption'], relayE2eeSuite);
      expect(observedForwards.single['payload'].toString(),
          isNot(contains('ping')));
      expect(observedPlaintexts.single['type'], 'ping');
      expect(
          events.whereType<PongEvent>().single.sessionId, 'session-reconnect');
      expect(events.whereType<ErrorEvent>(), isEmpty);
    });

    test('new pairing link forces E2EE pairing and device registration',
        () async {
      final nodeIdentity = await RelayE2eeHandshake.newEphemeralKeyPair();
      final nodeFingerprint = await RelayE2eeCrypto.fingerprint(
        nodeIdentity.publicKey,
      );
      final server = await HttpServer.bind(InternetAddress.loopbackIPv4, 0);
      addTearDown(server.close);
      final observedPairingPlaintexts = <Map<String, dynamic>>[];
      server.transform(WebSocketTransformer()).listen((socket) {
        _servePairingE2eeRelay(
          socket,
          nodeIdentity,
          null,
          observedPairingPlaintexts,
        );
      });
      final service = _testRelayService();
      addTearDown(service.dispose);

      await service.connectRelay(
        relayUrl: 'ws://127.0.0.1:${server.port}',
        sessionId: 'rs_test',
        pairingSecret: 'pair-secret-128-bit-minimum',
        nodeFingerprintHex: _testHex(nodeFingerprint),
        relayCapabilities: RelayE2eeCapabilitySet.production(),
      );
      await _waitForRelayDeviceRegistration(observedPairingPlaintexts);
      await service.disconnect();

      final nextServer = await HttpServer.bind(InternetAddress.loopbackIPv4, 0);
      addTearDown(nextServer.close);
      final observedControls = <Map<String, dynamic>>[];
      final observedPlaintexts = <Map<String, dynamic>>[];
      nextServer.transform(WebSocketTransformer()).listen((socket) {
        _servePairingE2eeRelay(
          socket,
          nodeIdentity,
          null,
          observedPlaintexts,
          observedControls: observedControls,
        );
      });

      await service.connectRelay(
        relayUrl: 'ws://127.0.0.1:${nextServer.port}',
        sessionId: 'rs_test',
        pairingSecret: 'new-pair-secret-after-backend-restart',
        nodeFingerprintHex: _testHex(nodeFingerprint),
        relayCapabilities: RelayE2eeCapabilitySet.production(),
      );

      expect(service.hasRelayE2eeHandshake, isTrue);
      expect(
        observedControls
            .where((frame) => frame['type'] == relayFrameClientE2eeHello)
            .single['kind'],
        relayE2eeHandshakeKindPairing,
      );
      await _waitForRelayDeviceRegistration(observedPlaintexts);
      expect(
        observedPlaintexts
            .where((event) => event['action'] == 'relay_device_register'),
        hasLength(1),
      );
    });

    test('production E2EE relay reconnect surfaces device rejection', () async {
      final nodeIdentity = await RelayE2eeHandshake.newEphemeralKeyPair();
      final nodeFingerprint = await RelayE2eeCrypto.fingerprint(
        nodeIdentity.publicKey,
      );
      final server = await HttpServer.bind(InternetAddress.loopbackIPv4, 0);
      addTearDown(server.close);
      final observedPairingPlaintexts = <Map<String, dynamic>>[];
      server.transform(WebSocketTransformer()).listen((socket) {
        _servePairingE2eeRelay(
          socket,
          nodeIdentity,
          null,
          observedPairingPlaintexts,
        );
      });
      final service = _testRelayService();
      addTearDown(service.dispose);

      await service.connectRelay(
        relayUrl: 'ws://127.0.0.1:${server.port}',
        sessionId: 'rs_test',
        pairingSecret: 'pair-secret-128-bit-minimum',
        nodeFingerprintHex: _testHex(nodeFingerprint),
        relayCapabilities: RelayE2eeCapabilitySet.production(),
      );
      final relaySession = service.takeRelaySession();
      final trustedDevice =
          await _waitForRelayDeviceRegistration(observedPairingPlaintexts);
      await service.disconnect();

      final reconnectServer =
          await HttpServer.bind(InternetAddress.loopbackIPv4, 0);
      addTearDown(reconnectServer.close);
      reconnectServer.transform(WebSocketTransformer()).listen((socket) {
        _serveReconnectE2eeRelay(
          socket: socket,
          nodeIdentity: nodeIdentity,
          trustedDevice: trustedDevice,
          resultErrorCode: 'device_revoked',
        );
      });

      expect(
        service.connectRelay(
          relayUrl: 'ws://127.0.0.1:${reconnectServer.port}',
          sessionId: 'rs_test',
          clientId: relaySession!.clientId,
          clientReconnectSecret: relaySession.clientReconnectSecret,
          nodeFingerprintHex: _testHex(nodeFingerprint),
          relayCapabilities: RelayE2eeCapabilitySet.production(),
        ),
        throwsA(
          isA<RelayPairingException>().having(
            (error) => error.code,
            'code',
            'device_revoked',
          ),
        ),
      );
    });

    test('production E2EE relay rejects forward before handshake completes',
        () async {
      final server = await HttpServer.bind(InternetAddress.loopbackIPv4, 0);
      addTearDown(server.close);
      server.transform(WebSocketTransformer()).listen((socket) {
        socket.listen((_) {
          socket.add(jsonEncode(const <String, dynamic>{
            'type': 'client.paired',
            'version': 1,
            'sessionId': 'rs_test',
            'clientId': 'rc_test',
            'clientReconnectSecret': 'reconnect_secret',
          }));
          socket.add(jsonEncode(<String, dynamic>{
            'type': 'relay.forward',
            'version': 1,
            'sessionId': 'rs_test',
            'clientId': 'rc_test',
            'direction': 'agent_to_client',
            'messageId': 'msg_plain',
            'contentType': 'mobilevc.ws.v1',
            'encryption': 'none',
            'payloadEncoding': 'base64url',
            'payload': base64UrlEncode(utf8.encode('{"type":"pong"}')),
          }));
        });
      });
      final service = _testRelayService();
      addTearDown(service.dispose);

      expect(
        service.connectRelay(
          relayUrl: 'ws://127.0.0.1:${server.port}',
          sessionId: 'rs_test',
          pairingSecret: 'pair-secret-128-bit-minimum',
          nodeFingerprintHex: '0' * 64,
          relayCapabilities: RelayE2eeCapabilitySet.production(),
        ),
        throwsA(
          isA<RelayPairingException>().having(
            (error) => error.code,
            'code',
            'e2ee_decrypt_failed',
          ),
        ),
      );
    });

    test(
        'relay pairing runs production E2EE handshake before connect completes',
        () async {
      final nodeIdentity = await RelayE2eeHandshake.newEphemeralKeyPair();
      final nodeFingerprint = await RelayE2eeCrypto.fingerprint(
        nodeIdentity.publicKey,
      );
      final server = await HttpServer.bind(InternetAddress.loopbackIPv4, 0);
      addTearDown(server.close);
      server.transform(WebSocketTransformer()).listen((socket) {
        _servePairingE2eeRelay(socket, nodeIdentity, null, null);
      });
      final service = _testRelayService();
      addTearDown(service.dispose);

      await service.connectRelay(
        relayUrl: 'ws://127.0.0.1:${server.port}',
        sessionId: 'rs_test',
        pairingSecret: 'pair-secret-128-bit-minimum',
        nodeFingerprintHex: _testHex(nodeFingerprint),
        relayCapabilities: RelayE2eeCapabilitySet.production(),
      );

      expect(service.hasRelayE2eeHandshake, isTrue);
      expect(service.takeRelaySession()?.clientId, 'rc_test');
    });

    test('stale E2EE reconnect handshake cannot poison replacement connection',
        () async {
      final nodeIdentity = await RelayE2eeHandshake.newEphemeralKeyPair();
      final nodeFingerprint = await RelayE2eeCrypto.fingerprint(
        nodeIdentity.publicKey,
      );
      final firstServer =
          await HttpServer.bind(InternetAddress.loopbackIPv4, 0);
      addTearDown(firstServer.close);
      final firstSockets = <WebSocket>[];
      firstServer.transform(WebSocketTransformer()).listen((socket) {
        firstSockets.add(socket);
        socket.listen((_) {
          socket.add(jsonEncode(const <String, dynamic>{
            'type': 'client.paired',
            'version': 1,
            'sessionId': 'rs_test',
            'clientId': 'rc_test',
          }));
        });
      });
      final secondServer =
          await HttpServer.bind(InternetAddress.loopbackIPv4, 0);
      addTearDown(secondServer.close);
      final observedPlaintexts = <Map<String, dynamic>>[];
      secondServer.transform(WebSocketTransformer()).listen((socket) {
        _servePairingE2eeRelay(
          socket,
          nodeIdentity,
          null,
          observedPlaintexts,
        );
      });

      final memoryStore = _MemoryRelaySecureStore();
      final blockingStore = _BlockingFirstReadRelaySecureStore(memoryStore);
      final service = MobileVcWsService(
        relayDeviceIdentityStore: RelayDeviceIdentityStore(
          secureStore: blockingStore,
          random: Random(201),
        ),
        relayDeviceCredentialStore: RelayDeviceCredentialStore(
          secureStore: memoryStore,
          random: Random(202),
        ),
      );
      addTearDown(service.dispose);
      final events = <AppEvent>[];
      final subscription = service.events.listen(events.add);
      addTearDown(subscription.cancel);

      final staleConnect = service.connectRelay(
        relayUrl: 'ws://127.0.0.1:${firstServer.port}',
        sessionId: 'rs_test',
        clientId: 'rc_test',
        clientReconnectSecret: 'reconnect_secret',
        nodeFingerprintHex: _testHex(nodeFingerprint),
        relayCapabilities: RelayE2eeCapabilitySet.production(),
      );
      await blockingStore.firstReadStarted.future.timeout(
        const Duration(seconds: 2),
      );

      await service.connectRelay(
        relayUrl: 'ws://127.0.0.1:${secondServer.port}',
        sessionId: 'rs_test',
        pairingSecret: 'pair-secret-128-bit-minimum',
        nodeFingerprintHex: _testHex(nodeFingerprint),
        relayCapabilities: RelayE2eeCapabilitySet.production(),
      );
      expect(service.hasRelayE2eeHandshake, isTrue);

      for (final socket in firstSockets) {
        await socket.close();
      }
      blockingStore.releaseFirstRead.complete();
      await expectLater(staleConnect, throwsStateError);

      expect(service.hasRelayE2eeHandshake, isTrue);
      expect(service.send(const <String, dynamic>{'type': 'ping'}), isTrue);
      await Future<void>.delayed(const Duration(milliseconds: 100));

      expect(
        events
            .whereType<ErrorEvent>()
            .where((event) => event.code == 'e2ee_decrypt_failed'),
        isEmpty,
      );
      expect(
        observedPlaintexts.any((event) => event['type'] == 'ping'),
        isTrue,
      );
    });

    test('relay E2EE encrypts forward payloads after handshake', () async {
      final nodeIdentity = await RelayE2eeHandshake.newEphemeralKeyPair();
      final nodeFingerprint = await RelayE2eeCrypto.fingerprint(
        nodeIdentity.publicKey,
      );
      final server = await HttpServer.bind(InternetAddress.loopbackIPv4, 0);
      addTearDown(server.close);
      final observedForwards = <Map<String, dynamic>>[];
      final observedPlaintexts = <Map<String, dynamic>>[];
      server.transform(WebSocketTransformer()).listen((socket) {
        _servePairingE2eeRelay(
          socket,
          nodeIdentity,
          observedForwards,
          observedPlaintexts,
        );
      });
      final service = _testRelayService();
      addTearDown(service.dispose);
      final events = <AppEvent>[];
      final subscription = service.events.listen(events.add);
      addTearDown(subscription.cancel);

      await service.connectRelay(
        relayUrl: 'ws://127.0.0.1:${server.port}',
        sessionId: 'rs_test',
        pairingSecret: 'pair-secret-128-bit-minimum',
        nodeFingerprintHex: _testHex(nodeFingerprint),
        relayCapabilities: RelayE2eeCapabilitySet.production(),
      );

      expect(service.send(const <String, dynamic>{'type': 'ping'}), isTrue);
      await Future<void>.delayed(const Duration(milliseconds: 100));

      expect(observedForwards, hasLength(greaterThanOrEqualTo(2)));
      expect(observedForwards[0]['encryption'], relayE2eeSuite);
      expect(observedForwards[1]['encryption'], relayE2eeSuite);
      expect(
          observedForwards[1]['payload'].toString(), isNot(contains('ping')));
      expect(
        observedPlaintexts.first['action'],
        'relay_device_register',
      );
      expect(
        observedForwards[0]['payload'].toString(),
        isNot(contains(observedPlaintexts.first['deviceCredential'])),
      );
      expect(events.whereType<PongEvent>().single.sessionId, 'session-test');
      expect(events.whereType<ErrorEvent>(), isEmpty);
    });

    test('relay E2EE downloads files through encrypted tunnel', () async {
      final nodeIdentity = await RelayE2eeHandshake.newEphemeralKeyPair();
      final nodeFingerprint = await RelayE2eeCrypto.fingerprint(
        nodeIdentity.publicKey,
      );
      final server = await HttpServer.bind(InternetAddress.loopbackIPv4, 0);
      addTearDown(server.close);
      final observedForwards = <Map<String, dynamic>>[];
      final observedPlaintexts = <Map<String, dynamic>>[];
      server.transform(WebSocketTransformer()).listen((socket) {
        _servePairingE2eeRelay(
          socket,
          nodeIdentity,
          observedForwards,
          observedPlaintexts,
        );
      });
      final service = _testRelayService();
      addTearDown(service.dispose);

      await service.connectRelay(
        relayUrl: 'ws://127.0.0.1:${server.port}',
        sessionId: 'rs_test',
        pairingSecret: 'pair-secret-128-bit-minimum',
        nodeFingerprintHex: _testHex(nodeFingerprint),
        relayCapabilities: RelayE2eeCapabilitySet.production(),
      );
      service.markRelayDeviceRegistered(RelayDeviceRegisterResultEvent(
        timestamp: DateTime.now(),
        sessionId: 'rs_test',
        runtimeMeta: const RuntimeMeta(),
        raw: const <String, dynamic>{'type': 'relay_device_register_result'},
        deviceId: 'device-bound',
      ));

      final progress = <int>[];
      final result = await service.downloadRelayFile(
        '/workspace/secret.txt',
        onProgress: (received, _) => progress.add(received),
      );
      await _waitForObservedFrame(
        observedPlaintexts,
        (frame) =>
            frame['type'] == tunnelFrameStreamAck &&
            (frame['streamId'] as num?)?.toInt() != 1,
      );

      expect(utf8.decode(result.bytes!), 'hello relay');
      expect(result.fileName, 'secret.txt');
      expect(progress, contains(11));
      final downloadForwards = observedForwards
          .where((frame) => (frame['streamId'] as num?)?.toInt() != 1)
          .toList();
      expect(downloadForwards, hasLength(greaterThanOrEqualTo(1)));
      expect(
        downloadForwards.first['payload'].toString(),
        isNot(contains('/workspace/secret.txt')),
      );
      expect(
        downloadForwards.first['payload'].toString(),
        isNot(contains('hello relay')),
      );
      expect(
        observedPlaintexts.any((frame) =>
            frame['type'] == tunnelFrameStreamAck &&
            (frame['streamId'] as num?)?.toInt() != 1),
        isTrue,
      );
    });

    test('relay E2EE download cancellation sends encrypted reset', () async {
      final nodeIdentity = await RelayE2eeHandshake.newEphemeralKeyPair();
      final nodeFingerprint = await RelayE2eeCrypto.fingerprint(
        nodeIdentity.publicKey,
      );
      final server = await HttpServer.bind(InternetAddress.loopbackIPv4, 0);
      addTearDown(server.close);
      final observedForwards = <Map<String, dynamic>>[];
      final observedPlaintexts = <Map<String, dynamic>>[];
      server.transform(WebSocketTransformer()).listen((socket) {
        _servePairingE2eeRelay(
          socket,
          nodeIdentity,
          observedForwards,
          observedPlaintexts,
          keepDownloadOpen: true,
        );
      });
      final service = _testRelayService();
      addTearDown(service.dispose);

      await service.connectRelay(
        relayUrl: 'ws://127.0.0.1:${server.port}',
        sessionId: 'rs_test',
        pairingSecret: 'pair-secret-128-bit-minimum',
        nodeFingerprintHex: _testHex(nodeFingerprint),
        relayCapabilities: RelayE2eeCapabilitySet.production(),
      );
      service.markRelayDeviceRegistered(RelayDeviceRegisterResultEvent(
        timestamp: DateTime.now(),
        sessionId: 'rs_test',
        runtimeMeta: const RuntimeMeta(),
        raw: const <String, dynamic>{'type': 'relay_device_register_result'},
        deviceId: 'device-bound',
      ));

      final cancelToken = RelayFileDownloadCancelToken();
      final download = service.downloadRelayFile(
        '/workspace/secret.txt',
        cancelToken: cancelToken,
      );
      await _waitForObservedFrame(
        observedPlaintexts,
        (frame) =>
            frame['type'] == tunnelFrameStreamOpen &&
            frame['streamType'] == tunnelStreamFileDownload,
      );
      cancelToken.cancel();

      await expectLater(download, throwsStateError);
      await _waitForObservedFrame(
        observedPlaintexts,
        (frame) =>
            frame['type'] == tunnelFrameStreamReset &&
            (frame['streamId'] as num?)?.toInt() != 1,
      );
      final resetForward = observedForwards.last;
      expect(resetForward['payload'].toString(), isNot(contains('cancelled')));
    });

    test('relay E2EE download surfaces generic tunnel stream errors', () async {
      final nodeIdentity = await RelayE2eeHandshake.newEphemeralKeyPair();
      final nodeFingerprint = await RelayE2eeCrypto.fingerprint(
        nodeIdentity.publicKey,
      );
      final server = await HttpServer.bind(InternetAddress.loopbackIPv4, 0);
      addTearDown(server.close);
      server.transform(WebSocketTransformer()).listen((socket) {
        _servePairingE2eeRelay(
          socket,
          nodeIdentity,
          null,
          null,
          downloadErrorCode: 'protocol_error',
        );
      });
      final service = _testRelayService();
      addTearDown(service.dispose);

      await service.connectRelay(
        relayUrl: 'ws://127.0.0.1:${server.port}',
        sessionId: 'rs_test',
        pairingSecret: 'pair-secret-128-bit-minimum',
        nodeFingerprintHex: _testHex(nodeFingerprint),
        relayCapabilities: RelayE2eeCapabilitySet.production(),
      );
      service.markRelayDeviceRegistered(RelayDeviceRegisterResultEvent(
        timestamp: DateTime.now(),
        sessionId: 'rs_test',
        runtimeMeta: const RuntimeMeta(),
        raw: const <String, dynamic>{'type': 'relay_device_register_result'},
        deviceId: 'device-bound',
      ));

      await expectLater(
        service.downloadRelayFile('/workspace/secret.txt'),
        throwsA(
          isA<RelayPairingException>().having(
            (error) => error.code,
            'code',
            'protocol_error',
          ),
        ),
      );
    });

    test('relay pairing rejects E2EE node fingerprint mismatch', () async {
      final nodeIdentity = await RelayE2eeHandshake.newEphemeralKeyPair();
      final server = await HttpServer.bind(InternetAddress.loopbackIPv4, 0);
      addTearDown(server.close);
      server.transform(WebSocketTransformer()).listen((socket) {
        _servePairingE2eeRelay(socket, nodeIdentity, null, null);
      });
      final service = _testRelayService();
      addTearDown(service.dispose);

      expect(
        service.connectRelay(
          relayUrl: 'ws://127.0.0.1:${server.port}',
          sessionId: 'rs_test',
          pairingSecret: 'pair-secret-128-bit-minimum',
          nodeFingerprintHex: '0' * 64,
          relayCapabilities: RelayE2eeCapabilitySet.production(),
        ),
        throwsA(isA<RelayPairingException>()),
      );
    });

    test('replacing connection does not emit stale disconnect event', () async {
      final server = await HttpServer.bind(InternetAddress.loopbackIPv4, 0);
      addTearDown(server.close);
      server.transform(WebSocketTransformer()).listen((socket) {
        addTearDown(socket.close);
      });

      final service = MobileVcWsService();
      addTearDown(service.dispose);

      final events = <AppEvent>[];
      final subscription = service.events.listen(events.add);
      addTearDown(subscription.cancel);

      final url = 'ws://127.0.0.1:${server.port}';
      await service.connect(url);
      await service.connect(url);
      await _flushEvents();

      expect(
        events.whereType<ErrorEvent>().where((e) => e.code == 'ws_closed'),
        isEmpty,
      );
      expect(
        events
            .whereType<ErrorEvent>()
            .where((e) => e.code == 'ws_stream_error'),
        isEmpty,
      );
    });
  });

  group('SessionController compact', () {
    test('手动 Compact 会携带当前 Codex resume 元信息', () async {
      final service = _FakeMobileVcWsService();
      final controller = SessionController(service: service);
      await controller.initialize();
      addTearDown(controller.disposeController);

      await controller.saveConfig(const AppConfig(
        cwd: '/workspace/MobileVC',
        engine: 'codex',
        permissionMode: 'bypassPermissions',
        codexSandboxMode: 'danger-full-access',
      ));
      await controller.connect();
      service.emit(
        SessionHistoryEvent(
          timestamp: _timestamp,
          sessionId: 'codex-thread:thread-current',
          runtimeMeta: const RuntimeMeta(engine: 'codex'),
          raw: const {'type': 'session_history'},
          summary: const SessionSummary(
            id: 'codex-thread:thread-current',
            title: 'Codex thread',
            runtime: RuntimeMeta(
              engine: 'codex',
              command: 'codex -m gpt-5.5',
              cwd: '/workspace/MobileVC',
              resumeSessionId: 'thread-current',
            ),
          ),
          canResume: true,
          runtimeAlive: true,
          resumeRuntimeMeta: const RuntimeMeta(
            engine: 'codex',
            command: 'codex -m gpt-5.5',
            cwd: '/workspace/MobileVC',
            resumeSessionId: 'thread-current',
            claudeLifecycle: 'waiting_input',
          ),
        ),
      );
      await _flushEvents();
      service.sentPayloads.clear();

      controller.compactCurrentSession();

      expect(service.sentPayloads, hasLength(1));
      final payload = service.sentPayloads.single;
      expect(payload['action'], 'compact');
      expect(payload['sessionId'], 'codex-thread:thread-current');
      expect(payload['resumeSessionId'], 'thread-current');
      expect(payload['command'], 'codex -m gpt-5.5');
      expect(payload['engine'], 'codex');
      expect(payload['cwd'], '/workspace/MobileVC');
      expect(payload['permissionMode'], 'bypassPermissions');
      expect(payload['codexSandboxMode'], 'danger-full-access');
      expect(payload['source'], 'compact');
      expect(payload['targetType'], 'compact');
    });

    test('恢复的 Codex 会话即使 engine 为空也允许手动 Compact', () async {
      final service = _FakeMobileVcWsService();
      final controller = SessionController(service: service);
      await controller.initialize();
      addTearDown(controller.disposeController);

      await controller.saveConfig(const AppConfig(
        cwd: '/workspace/MobileVC',
        engine: 'claude',
        permissionMode: 'auto',
      ));
      await controller.connect();
      service.emit(
        SessionHistoryEvent(
          timestamp: _timestamp,
          sessionId: 'codex-thread:thread-command-only',
          runtimeMeta: const RuntimeMeta(),
          raw: const {'type': 'session_history'},
          summary: const SessionSummary(
            id: 'codex-thread:thread-command-only',
            title: 'Codex thread',
            runtime: RuntimeMeta(
              command: 'codex resume thread-command-only',
              cwd: '/workspace/MobileVC',
              resumeSessionId: 'thread-command-only',
            ),
          ),
          canResume: true,
          runtimeAlive: true,
          resumeRuntimeMeta: const RuntimeMeta(
            command: 'codex resume thread-command-only',
            cwd: '/workspace/MobileVC',
            resumeSessionId: 'thread-command-only',
            claudeLifecycle: 'waiting_input',
          ),
        ),
      );
      await _flushEvents();
      service.sentPayloads.clear();

      expect(controller.shouldShowCompactButton, isTrue);
      expect(controller.canCompactCurrentSession, isTrue);

      controller.compactCurrentSession();

      expect(service.sentPayloads, hasLength(1));
      final payload = service.sentPayloads.single;
      expect(payload['action'], 'compact');
      expect(payload['sessionId'], 'codex-thread:thread-command-only');
      expect(payload['resumeSessionId'], 'thread-command-only');
      expect(payload['command'], 'codex resume thread-command-only');
      expect(payload['engine'], 'codex');
      expect(payload['cwd'], '/workspace/MobileVC');
    });

    test('恢复的 Codex 空闲会话有残留 RUNNING 状态时仍允许手动 Compact', () async {
      final service = _FakeMobileVcWsService();
      final controller = SessionController(service: service);
      await controller.initialize();
      addTearDown(controller.disposeController);

      await controller.saveConfig(const AppConfig(
        cwd: '/workspace/MobileVC',
        engine: 'codex',
        permissionMode: 'bypassPermissions',
      ));
      await controller.connect();
      service.emit(
        SessionHistoryEvent(
          timestamp: _timestamp,
          sessionId: 'codex-thread:thread-stale-running',
          runtimeMeta: const RuntimeMeta(engine: 'codex'),
          raw: const {'type': 'session_history'},
          summary: const SessionSummary(
            id: 'codex-thread:thread-stale-running',
            title: 'Codex thread',
            source: 'codex-native',
            external: true,
            runtime: RuntimeMeta(
              engine: 'codex',
              command: 'codex resume thread-stale-running',
              cwd: '/workspace/MobileVC',
              resumeSessionId: 'thread-stale-running',
              claudeLifecycle: 'waiting_input',
            ),
          ),
          canResume: true,
          runtimeAlive: true,
          resumeRuntimeMeta: const RuntimeMeta(
            engine: 'codex',
            command: 'codex resume thread-stale-running',
            cwd: '/workspace/MobileVC',
            resumeSessionId: 'thread-stale-running',
            claudeLifecycle: 'waiting_input',
          ),
        ),
      );
      service.emit(
        SessionStateEvent(
          timestamp: _timestamp,
          sessionId: 'codex-thread:thread-stale-running',
          runtimeMeta: const RuntimeMeta(
            engine: 'codex',
            command: 'codex resume thread-stale-running',
            cwd: '/workspace/MobileVC',
            resumeSessionId: 'thread-stale-running',
            claudeLifecycle: 'waiting_input',
          ),
          raw: const {'type': 'session_state'},
          state: 'RUNNING',
          message: 'runtime alive',
        ),
      );
      await _flushEvents();
      service.sentPayloads.clear();

      expect(controller.shouldShowCompactButton, isTrue);
      expect(controller.isSessionBusy, isTrue);
      expect(controller.canCompactCurrentSession, isTrue);

      controller.compactCurrentSession();

      expect(service.sentPayloads, hasLength(1));
      final payload = service.sentPayloads.single;
      expect(payload['action'], 'compact');
      expect(payload['sessionId'], 'codex-thread:thread-stale-running');
      expect(payload['resumeSessionId'], 'thread-stale-running');
      expect(payload['engine'], 'codex');
    });

    test('真实运行中的 Codex 会话仍禁止手动 Compact', () async {
      final service = _FakeMobileVcWsService();
      final controller = SessionController(service: service);
      await controller.initialize();
      addTearDown(controller.disposeController);

      await controller.saveConfig(const AppConfig(
        cwd: '/workspace/MobileVC',
        engine: 'codex',
      ));
      await controller.connect();
      service.emit(
        SessionHistoryEvent(
          timestamp: _timestamp,
          sessionId: 'codex-thread:thread-running',
          runtimeMeta: const RuntimeMeta(engine: 'codex'),
          raw: const {'type': 'session_history'},
          summary: const SessionSummary(
            id: 'codex-thread:thread-running',
            title: 'Codex running',
            source: 'codex-native',
            external: true,
            runtime: RuntimeMeta(
              engine: 'codex',
              command: 'codex resume thread-running',
              cwd: '/workspace/MobileVC',
              resumeSessionId: 'thread-running',
            ),
          ),
          canResume: true,
          runtimeAlive: true,
          resumeRuntimeMeta: const RuntimeMeta(
            engine: 'codex',
            command: 'codex resume thread-running',
            cwd: '/workspace/MobileVC',
            resumeSessionId: 'thread-running',
          ),
        ),
      );
      service.emit(
        SessionStateEvent(
          timestamp: _timestamp,
          sessionId: 'codex-thread:thread-running',
          runtimeMeta: const RuntimeMeta(
            engine: 'codex',
            command: 'codex resume thread-running',
            cwd: '/workspace/MobileVC',
            resumeSessionId: 'thread-running',
            executionId: 'exec-running',
          ),
          raw: const {'type': 'session_state'},
          state: 'RUNNING',
          message: 'running',
        ),
      );
      await _flushEvents();
      service.sentPayloads.clear();

      expect(controller.shouldShowCompactButton, isTrue);
      expect(controller.isSessionBusy, isTrue);
      expect(controller.canCompactCurrentSession, isFalse);

      controller.compactCurrentSession();

      expect(service.sentPayloads, isEmpty);
    });
  });

  group('SessionController action needed signal', () {
    test(
        'initialize reconnects when previous page had active connection intent',
        () async {
      SharedPreferences.setMockInitialValues({
        'mobilevc.connection_intent': true,
        'mobilevc.app_config': jsonEncode(const AppConfig(
          host: 'https://example.com',
          port: '9999',
        ).toJson()),
      });
      final service = _FakeMobileVcWsService();
      final controller = SessionController(
        service: service,
        outboundAckStaleTimeout: const Duration(milliseconds: 40),
      );
      await controller.initialize();
      addTearDown(controller.disposeController);
      await _flushEvents();

      expect(service.connectCalls, 1);
      expect(service.connectedUrls, ['wss://example.com:9999/ws?token=test']);
      expect(controller.connected, isTrue);
      expect(controller.autoReconnectEnabled, isTrue);
    });

    test('initialize does not reconnect without previous connection intent',
        () async {
      SharedPreferences.setMockInitialValues({
        'mobilevc.app_config': jsonEncode(const AppConfig(
          host: 'https://example.com',
          port: '9999',
        ).toJson()),
      });
      final service = _FakeMobileVcWsService();
      final controller = SessionController(service: service);
      await controller.initialize();
      addTearDown(controller.disposeController);
      await _flushEvents();

      expect(service.connectCalls, 0);
      expect(controller.connected, isFalse);
      expect(controller.autoReconnectEnabled, isFalse);
    });

    test('manual disconnect clears persisted connection intent', () async {
      SharedPreferences.setMockInitialValues({});
      final service = _FakeMobileVcWsService();
      final controller = SessionController(service: service);
      await controller.initialize();
      addTearDown(controller.disposeController);

      await controller.connect();
      await controller.disconnect();
      final prefs = await SharedPreferences.getInstance();

      expect(prefs.getBool('mobilevc.connection_intent'), isFalse);
      expect(service.disconnectCalls, 1);
    });

    test('external relay deep link imports relay config instead of host',
        () async {
      final service = _FakeMobileVcWsService();
      final controller = SessionController(service: service);
      await controller.initialize();
      addTearDown(controller.disposeController);

      final imported = await controller.importConnectionLink(
        'mobilevc://relay/v1?relay=wss%3A%2F%2Frelay.example.test'
        '&session=rs_import&secret=pair_secret&exp=4102444800'
        '&nodeFingerprint=1111111111111111111111111111111111111111111111111111111111111111'
        '&relayProtocolVersion=1&e2eeProtocolVersion=1'
        '&cryptoSuite=p256-ecdsa%2Bp256-ecdh%2Bhkdf-sha256%2Baes-256-gcm'
        '&tunnelProtocolVersion=1&supportsMultiplexStreams=true'
        '&supportsFileDownloadStream=true&supportsDeviceManagement=true'
        '&requiresE2EE=true&plaintextTestMode=false',
      );

      expect(imported, isTrue);
      expect(controller.config.isRelayMode, isTrue);
      expect(controller.config.host, 'localhost');
      expect(controller.config.relayUrl, 'wss://relay.example.test');
      expect(controller.config.relaySessionId, 'rs_import');
      expect(controller.config.relayPairingSecret, 'pair_secret');
      expect(controller.config.relayCapabilities, isNotNull);

      final prefs = await SharedPreferences.getInstance();
      final persisted = jsonDecode(prefs.getString('mobilevc.app_config')!)
          as Map<String, dynamic>;
      expect(persisted['connectionMode'], 'relay');
      expect(persisted['relayUrl'], 'wss://relay.example.test');
      expect(persisted.containsKey('relayPairingSecret'), isFalse);
    });

    test('external relay deep link with LAN endpoint imports auto mode',
        () async {
      final service = _FakeMobileVcWsService();
      final controller = SessionController(service: service);
      await controller.initialize();
      addTearDown(controller.disposeController);

      final imported = await controller.importConnectionLink(
        'mobilevc://relay/v1?relay=wss%3A%2F%2Frelay.example.test'
        '&session=rs_import&secret=pair_secret&exp=4102444800'
        '&nodeFingerprint=1111111111111111111111111111111111111111111111111111111111111111'
        '&lanHost=192.168.1.2&lanPort=8001&lanToken=direct-token'
        '&lanCwd=%2Fworkspace'
        '&relayProtocolVersion=1&e2eeProtocolVersion=1'
        '&cryptoSuite=p256-ecdsa%2Bp256-ecdh%2Bhkdf-sha256%2Baes-256-gcm'
        '&tunnelProtocolVersion=1&supportsMultiplexStreams=true'
        '&supportsFileDownloadStream=true&supportsDeviceManagement=true'
        '&requiresE2EE=true&plaintextTestMode=false',
      );

      expect(imported, isTrue);
      expect(controller.config.connectionMode, ConnectionMode.auto.name);
      expect(controller.config.host, '192.168.1.2');
      expect(controller.config.port, '8001');
      expect(controller.config.token, 'direct-token');
      expect(controller.config.cwd, '/workspace');
      expect(controller.config.relayUrl, 'wss://relay.example.test');
    });

    test('relay connect validates url and persists reconnect fields', () async {
      SharedPreferences.setMockInitialValues({
        'mobilevc.app_config': jsonEncode(const AppConfig(
          connectionMode: 'relay',
          relayUrl: 'wss://relay.example.test',
          relaySessionId: 'rs_test',
          relayPairingSecret: 'pair_secret',
          relayPairingExpiresAt: 4102444800,
        ).toJson()),
      });
      final service = _FakeMobileVcWsService();
      final controller = SessionController(service: service);
      await controller.initialize();
      addTearDown(controller.disposeController);

      await controller.saveConfig(const AppConfig(
        connectionMode: 'relay',
        relayUrl: 'wss://relay.example.test',
        relaySessionId: 'rs_test',
        relayPairingSecret: 'pair_secret',
        relayPairingExpiresAt: 4102444800,
      ));
      await controller.connect();

      expect(
          service.connectedRelays.single.relayUrl, 'wss://relay.example.test');
      expect(controller.config.relayUrl, 'wss://relay.example.test');
      expect(controller.config.relaySessionId, 'rs_test');
      expect(controller.config.relayPairingSecret, isEmpty);
      expect(controller.config.relayClientId, 'rc_test');
      expect(controller.config.relayClientReconnectSecret, 'reconnect_secret');
      final prefs = await SharedPreferences.getInstance();
      final persisted = jsonDecode(prefs.getString('mobilevc.app_config')!)
          as Map<String, dynamic>;
      expect(persisted['relayUrl'], 'wss://relay.example.test');
      expect(persisted['relaySessionId'], 'rs_test');
      expect(persisted.containsKey('relayPairingSecret'), isFalse);
      expect(persisted['relayClientId'], 'rc_test');
      expect(persisted['relayClientReconnectSecret'], 'reconnect_secret');
    });

    test('expired new relay link is rejected without changing current config',
        () async {
      SharedPreferences.setMockInitialValues({
        'mobilevc.app_config': jsonEncode(const AppConfig(
          connectionMode: 'relay',
          relayUrl: 'wss://relay.example.test',
          relaySessionId: 'rs_existing',
          relayClientId: 'rc_existing',
          relayClientReconnectSecret: 'reconnect_existing',
        ).toJson()),
      });
      final service = _FakeMobileVcWsService();
      final controller = SessionController(service: service);
      await controller.initialize();
      addTearDown(controller.disposeController);

      final imported = await controller.importConnectionLink(
        'mobilevc://relay/v1?relay=wss%3A%2F%2Frelay.example.test'
        '&session=rs_expired&secret=stale_pair_secret&exp=1'
        '&nodeFingerprint=1111111111111111111111111111111111111111111111111111111111111111'
        '&relayProtocolVersion=1&e2eeProtocolVersion=1'
        '&cryptoSuite=p256-ecdsa%2Bp256-ecdh%2Bhkdf-sha256%2Baes-256-gcm'
        '&tunnelProtocolVersion=1&supportsMultiplexStreams=true'
        '&supportsFileDownloadStream=true&supportsDeviceManagement=true'
        '&requiresE2EE=true&plaintextTestMode=false',
      );

      expect(imported, isFalse);
      expect(controller.connectionMessage, contains('Relay 配对链接已过期'));
      expect(controller.config.relaySessionId, 'rs_existing');
      expect(controller.config.relayClientId, 'rc_existing');
      expect(
        controller.config.relayClientReconnectSecret,
        'reconnect_existing',
      );
      expect(service.disconnectCalls, 0);
    });

    test('relay pairing failure keeps one-time secret for explicit retry',
        () async {
      final service = _FakeMobileVcWsService()
        ..connectRelayError = const RelayPairingException(
          'e2ee_handshake_failed',
          'Relay E2EE 握手失败',
        );
      final controller = SessionController(service: service);
      await controller.initialize();
      addTearDown(controller.disposeController);

      await controller.saveConfig(const AppConfig(
        connectionMode: 'relay',
        relayUrl: 'wss://relay.example.test',
        relaySessionId: 'rs_test',
        relayPairingSecret: 'pair_secret',
        relayPairingExpiresAt: 4102444800,
      ));

      await controller.connect();

      expect(controller.connected, isFalse);
      expect(controller.connectionStage, SessionConnectionStage.failed);
      expect(controller.connectionMessage, contains('Relay E2EE 握手失败'));
      expect(controller.config.relayPairingSecret, 'pair_secret');
      expect(controller.config.relayPairingExpiresAt, 4102444800);

      final prefs = await SharedPreferences.getInstance();
      final persisted = jsonDecode(prefs.getString('mobilevc.app_config')!)
          as Map<String, dynamic>;
      expect(persisted.containsKey('relayPairingSecret'), isFalse);
      expect(persisted.containsKey('relayPairingExpiresAt'), isFalse);
    });

    test('auto mode tries LAN first and falls back to relay', () async {
      final service = _FakeMobileVcWsService()
        ..connectErrors.add(StateError('LAN unavailable'));
      final controller = SessionController(service: service);
      await controller.initialize();
      addTearDown(controller.disposeController);

      await controller.saveConfig(const AppConfig(
        connectionMode: 'auto',
        host: '192.168.1.2',
        port: '8001',
        token: 'direct-token',
        relayUrl: 'wss://relay.example.test',
        relaySessionId: 'rs_test',
        relayClientId: 'rc_test',
        relayClientReconnectSecret: 'reconnect_secret',
      ));

      await controller.connect();

      expect(controller.connected, isTrue);
      expect(controller.activeTransportPath, ActiveTransportPath.relay);
      expect(service.connectedUrls.single,
          'ws://192.168.1.2:8001/ws?token=direct-token');
      expect(
          service.connectedRelays.single.relayUrl, 'wss://relay.example.test');
    });

    test('auto mode skips iOS loopback LAN and falls back to relay', () async {
      debugDefaultTargetPlatformOverride = TargetPlatform.iOS;
      addTearDown(() {
        debugDefaultTargetPlatformOverride = null;
      });
      final service = _FakeMobileVcWsService();
      final controller = SessionController(service: service);
      await controller.initialize();
      addTearDown(controller.disposeController);

      await controller.saveConfig(const AppConfig(
        connectionMode: 'auto',
        host: 'localhost',
        port: '8001',
        token: 'direct-token',
        relayUrl: 'wss://relay.example.test',
        relaySessionId: 'rs_test',
        relayClientId: 'rc_test',
        relayClientReconnectSecret: 'reconnect_secret',
      ));

      await controller.connect();

      expect(controller.connected, isTrue);
      expect(controller.activeTransportPath, ActiveTransportPath.relay);
      expect(service.connectedUrls, isEmpty);
      expect(
          service.connectedRelays.single.relayUrl, 'wss://relay.example.test');
    });

    test('auto mode does not probe iOS loopback after relay fallback',
        () async {
      debugDefaultTargetPlatformOverride = TargetPlatform.iOS;
      addTearDown(() {
        debugDefaultTargetPlatformOverride = null;
      });
      final service = _FakeMobileVcWsService();
      final controller = SessionController(
        service: service,
        lanReturnProbeInterval: const Duration(milliseconds: 10),
      );
      await controller.initialize();
      addTearDown(controller.disposeController);

      await controller.saveConfig(const AppConfig(
        connectionMode: 'auto',
        host: 'localhost',
        port: '8001',
        token: 'direct-token',
        relayUrl: 'wss://relay.example.test',
        relaySessionId: 'rs_test',
        relayClientId: 'rc_test',
        relayClientReconnectSecret: 'reconnect_secret',
      ));

      await controller.connect();
      await Future<void>.delayed(const Duration(milliseconds: 35));

      expect(controller.activeTransportPath, ActiveTransportPath.relay);
      expect(service.connectedUrls, isEmpty);
      expect(service.readyDirectUrls, isEmpty);
      expect(service.connectedRelays, hasLength(1));
    });

    test('auto relay to LAN return keeps relay when LAN preconnect fails',
        () async {
      final service = _FakeMobileVcWsService()
        ..connectErrors.add(StateError('LAN unavailable'))
        ..readyDirectErrors.add(StateError('LAN still unavailable'));
      final controller = SessionController(
        service: service,
        lanReturnProbeInterval: const Duration(milliseconds: 10),
      );
      await controller.initialize();
      addTearDown(controller.disposeController);

      await controller.saveConfig(const AppConfig(
        connectionMode: 'auto',
        host: '192.168.1.2',
        port: '8001',
        token: 'direct-token',
        relayUrl: 'wss://relay.example.test',
        relaySessionId: 'rs_test',
        relayClientId: 'rc_test',
        relayClientReconnectSecret: 'reconnect_secret',
      ));

      await controller.connect();
      await Future<void>.delayed(const Duration(milliseconds: 35));

      expect(controller.activeTransportPath, ActiveTransportPath.relay);
      expect(service.readyDirectUrls, [
        'ws://192.168.1.2:8001/ws?token=direct-token',
      ]);
      expect(service.connectedRelays, hasLength(1));
    });

    test('auto relay to LAN return switches only after ready direct connect',
        () async {
      final service = _FakeMobileVcWsService()
        ..connectErrors.add(StateError('LAN unavailable'));
      final controller = SessionController(
        service: service,
        lanReturnProbeInterval: const Duration(milliseconds: 10),
      );
      await controller.initialize();
      addTearDown(controller.disposeController);

      await controller.saveConfig(const AppConfig(
        connectionMode: 'auto',
        host: '192.168.1.2',
        port: '8001',
        token: 'direct-token',
        relayUrl: 'wss://relay.example.test',
        relaySessionId: 'rs_test',
        relayClientId: 'rc_test',
        relayClientReconnectSecret: 'reconnect_secret',
      ));

      await controller.connect();
      await Future<void>.delayed(const Duration(milliseconds: 35));

      expect(controller.activeTransportPath, ActiveTransportPath.lan);
      expect(service.readyDirectUrls, [
        'ws://192.168.1.2:8001/ws?token=direct-token',
      ]);
      expect(service.connectedRelays, hasLength(1));
    });

    test('auto relay security state follows actual relay path', () async {
      final service = _FakeMobileVcWsService()
        ..connectErrors.add(StateError('LAN unavailable'))
        ..relayE2eeHandshake = true
        ..relayE2eeDeviceBinding = true;
      final controller = SessionController(service: service);
      await controller.initialize();
      addTearDown(controller.disposeController);
      final expectedFingerprint =
          await _testFingerprintHex(service.relayNodeFingerprint);

      await controller.saveConfig(AppConfig(
        connectionMode: 'auto',
        host: '192.168.1.2',
        port: '8001',
        token: 'direct-token',
        relayUrl: 'wss://relay.example.test',
        relaySessionId: 'rs_test',
        relayClientId: 'rc_test',
        relayClientReconnectSecret: 'reconnect_secret',
        relayNodeFingerprintHex: expectedFingerprint,
        relayCapabilities: RelayE2eeCapabilitySet.production(),
      ));

      await controller.connect();
      final state = await controller.relaySecurityState();

      expect(controller.activeTransportPath, ActiveTransportPath.relay);
      expect(state.title, 'E2EE 已验证');
      expect(state.canShowVerified, isTrue);
    });

    test('relay agent reconnecting does not clear one-time pairing secret',
        () async {
      final service = _FakeMobileVcWsService()
        ..connectRelayError = const RelayPairingException(
          'agent_disconnected',
          '本机 Relay 正在重连，请稍后自动恢复',
        );
      final controller = SessionController(service: service);
      await controller.initialize();
      addTearDown(controller.disposeController);

      await controller.saveConfig(const AppConfig(
        connectionMode: 'relay',
        relayUrl: 'wss://relay.example.test',
        relaySessionId: 'rs_test',
        relayPairingSecret: 'pair_secret',
        relayPairingExpiresAt: 4102444800,
      ));

      await controller.connect();

      expect(controller.connected, isFalse);
      expect(controller.connectionStage, SessionConnectionStage.reconnecting);
      expect(controller.config.relayPairingSecret, 'pair_secret');
      expect(controller.config.relayPairingExpiresAt, 4102444800);
    });

    test('relay agent disconnect auto reconnects and resends unacked action',
        () async {
      final service = _FakeMobileVcWsService();
      final controller = SessionController(service: service);
      await controller.initialize();
      addTearDown(controller.disposeController);

      await controller.saveConfig(const AppConfig(
        connectionMode: 'relay',
        relayUrl: 'wss://relay.example.test',
        relaySessionId: 'rs_test',
        relayClientId: 'rc_test',
        relayClientReconnectSecret: 'reconnect_secret',
      ));
      await controller.connect();
      service.emit(
        SessionCreatedEvent(
          timestamp: _timestamp,
          sessionId: 'session-1',
          runtimeMeta: const RuntimeMeta(),
          raw: const {'type': 'session_created'},
          summary: const SessionSummary(id: 'session-1', title: 'Relay'),
        ),
      );
      await _flushEvents();
      service.sentPayloads.clear();

      controller.sendInputText('自动恢复消息');
      await _flushEvents();

      final initialTurn = service.sentPayloads.singleWhere(
        (payload) => payload['action'] == 'ai_turn',
      );
      final clientActionId = initialTurn['clientActionId'];
      expect(clientActionId, isA<String>());
      expect(controller.timeline.where((item) => item.body == '自动恢复消息'),
          hasLength(1));

      service.emit(
        ErrorEvent(
          timestamp: _timestamp.add(const Duration(seconds: 1)),
          sessionId: 'session-1',
          runtimeMeta: const RuntimeMeta(),
          raw: const {'type': 'error'},
          code: 'agent_disconnected',
          message: '本机 Relay 正在重连，请稍后自动恢复',
        ),
      );
      await _flushEvents();
      await _flushEvents();

      expect(service.connectCalls, 2);
      expect(controller.connected, isTrue);
      final resentTurns = service.sentPayloads
          .where((payload) =>
              payload['action'] == 'ai_turn' &&
              payload['clientActionId'] == clientActionId)
          .toList();
      expect(resentTurns, hasLength(2));
      expect(controller.timeline.where((item) => item.body == '自动恢复消息'),
          hasLength(1));

      service.emit(ClientActionAckEvent(
        timestamp: _timestamp.add(const Duration(seconds: 2)),
        sessionId: 'session-1',
        runtimeMeta: const RuntimeMeta(),
        raw: const {'type': 'client_action_ack'},
        action: 'ai_turn',
        clientActionId: clientActionId as String,
        status: 'accepted',
      ));
      await _flushEvents();
      service.sentPayloads.clear();
      controller.resumeConnectionIfNeeded();
      await _flushEvents();

      expect(
        service.sentPayloads.where((payload) =>
            payload['action'] == 'ai_turn' &&
            payload['clientActionId'] == clientActionId),
        isEmpty,
      );
    });

    test('relay reconnect resumes selected session and keeps visible state',
        () async {
      final service = _FakeMobileVcWsService();
      final controller = SessionController(service: service);
      await controller.initialize();
      addTearDown(controller.disposeController);

      await controller.saveConfig(const AppConfig(
        connectionMode: 'relay',
        relayUrl: 'wss://relay.example.test',
        relaySessionId: 'rs_test',
        relayClientId: 'rc_test',
        relayClientReconnectSecret: 'reconnect_secret',
      ));
      await controller.connect();
      service.emit(
        SessionHistoryEvent(
          timestamp: _timestamp,
          sessionId: 'session-relay',
          runtimeMeta: const RuntimeMeta(command: 'codex', engine: 'codex'),
          raw: const {'type': 'session_history'},
          summary: const SessionSummary(
            id: 'session-relay',
            title: 'Relay Session',
          ),
          logEntries: const [
            HistoryLogEntry(
              kind: 'assistant',
              message: 'relay history',
              label: 'Assistant',
            ),
          ],
          resumeRuntimeMeta: const RuntimeMeta(
            command: 'codex',
            engine: 'codex',
          ),
        ),
      );
      await _flushEvents();
      expect(controller.selectedSessionId, 'session-relay');
      expect(controller.timeline.single.body, 'relay history');
      expect(controller.shouldShowSessionSurface, isTrue);

      service.sentPayloads.clear();
      service.emit(
        ErrorEvent(
          timestamp: _timestamp.add(const Duration(seconds: 1)),
          sessionId: 'session-relay',
          runtimeMeta: const RuntimeMeta(command: 'codex', engine: 'codex'),
          raw: const {'type': 'error'},
          code: 'agent_disconnected',
          message: '本机 Relay 正在重连，请稍后自动恢复',
        ),
      );
      await _flushEvents();
      await _flushEvents();

      expect(service.connectCalls, 2);
      expect(controller.connected, isTrue);
      expect(controller.activeTransportPath, ActiveTransportPath.relay);
      expect(controller.selectedSessionId, 'session-relay');
      expect(controller.timeline.single.body, 'relay history');
      expect(controller.shouldShowSessionSurface, isTrue);
      expect(
        service.sentPayloads.where((payload) =>
            payload['action'] == 'session_resume' &&
            payload['sessionId'] == 'session-relay' &&
            payload['reason'] == 'reconnect' &&
            payload['limit'] == 120),
        hasLength(1),
      );
    });

    test('relay reconnect keeps awaiting input context while chatting',
        () async {
      final service = _FakeMobileVcWsService();
      final controller = SessionController(service: service);
      await controller.initialize();
      addTearDown(controller.disposeController);

      await controller.saveConfig(const AppConfig(
        connectionMode: 'relay',
        relayUrl: 'wss://relay.example.test',
        relaySessionId: 'rs_test',
        relayClientId: 'rc_test',
        relayClientReconnectSecret: 'reconnect_secret',
      ));
      await controller.connect();
      service.emit(
        SessionHistoryEvent(
          timestamp: _timestamp,
          sessionId: 'session-relay-chat',
          runtimeMeta: const RuntimeMeta(command: 'claude', engine: 'claude'),
          raw: const {'type': 'session_history'},
          summary: const SessionSummary(
            id: 'session-relay-chat',
            title: 'Relay Chat',
          ),
          logEntries: const [
            HistoryLogEntry(
              kind: 'assistant',
              message: '等待你继续',
              label: 'Assistant',
            ),
          ],
          resumeRuntimeMeta: const RuntimeMeta(
            command: 'claude',
            engine: 'claude',
            claudeLifecycle: 'waiting_input',
          ),
        ),
      );
      service.emit(
        AgentStateEvent(
          timestamp: _timestamp.add(const Duration(milliseconds: 100)),
          sessionId: 'session-relay-chat',
          runtimeMeta: const RuntimeMeta(
            command: 'claude',
            engine: 'claude',
            claudeLifecycle: 'waiting_input',
          ),
          raw: const {'type': 'agent_state'},
          state: 'WAIT_INPUT',
          message: '等待输入',
          command: 'claude',
          awaitInput: true,
        ),
      );
      await _flushEvents();
      expect(controller.awaitInput, isTrue);
      expect(controller.shouldShowClaudeMode, isTrue);

      service.sentPayloads.clear();
      service.emit(
        ErrorEvent(
          timestamp: _timestamp.add(const Duration(seconds: 1)),
          sessionId: 'session-relay-chat',
          runtimeMeta: const RuntimeMeta(command: 'claude', engine: 'claude'),
          raw: const {'type': 'error'},
          code: 'agent_disconnected',
          message: '本机 Relay 正在重连，请稍后自动恢复',
        ),
      );
      await _flushEvents();
      await _flushEvents();

      expect(service.connectCalls, 2);
      expect(controller.connected, isTrue);
      expect(controller.awaitInput, isTrue);
      expect(controller.shouldShowClaudeMode, isTrue);

      controller.sendInputText('继续这个对话');
      await _flushEvents();

      final inputPayload = service.sentPayloads.singleWhere(
        (payload) => payload['action'] == 'input',
      );
      expect(inputPayload['sessionId'], 'session-relay-chat');
      expect(inputPayload['data'], '继续这个对话\n');
      expect(
        service.sentPayloads.where((payload) =>
            payload['action'] == 'ai_turn' || payload['action'] == 'exec'),
        isEmpty,
      );
      expect(
        controller.timeline.where((item) => item.body == '继续这个对话'),
        hasLength(1),
      );
    });

    test('unacknowledged relay send reconnects and resends queued action',
        () async {
      final service = _FakeMobileVcWsService();
      final controller = SessionController(
        service: service,
        outboundAckRetryDelay: const Duration(milliseconds: 40),
        outboundAckStaleTimeout: const Duration(milliseconds: 20),
      );
      await controller.initialize();
      addTearDown(controller.disposeController);

      await controller.saveConfig(const AppConfig(
        connectionMode: 'relay',
        relayUrl: 'wss://relay.example.test',
        relaySessionId: 'rs_test',
        relayClientId: 'rc_test',
        relayClientReconnectSecret: 'reconnect_secret',
      ));
      await controller.connect();
      service.emit(
        SessionCreatedEvent(
          timestamp: _timestamp,
          sessionId: 'session-1',
          runtimeMeta: const RuntimeMeta(),
          raw: const {'type': 'session_created'},
          summary: const SessionSummary(id: 'session-1', title: 'Relay'),
        ),
      );
      await _flushEvents();
      service.sentPayloads.clear();

      controller.sendInputText('半开连接消息');
      await _flushEvents();

      final initialTurn = service.sentPayloads.singleWhere(
        (payload) => payload['action'] == 'ai_turn',
      );
      final clientActionId = initialTurn['clientActionId'];
      expect(clientActionId, isA<String>());
      expect(controller.timeline.where((item) => item.body == '半开连接消息'),
          hasLength(1));

      await Future<void>.delayed(const Duration(milliseconds: 120));
      await _flushEvents();

      expect(service.disconnectCalls, greaterThanOrEqualTo(1));
      expect(service.connectCalls, greaterThanOrEqualTo(2));
      final resentTurns = service.sentPayloads
          .where((payload) =>
              payload['action'] == 'ai_turn' &&
              payload['clientActionId'] == clientActionId)
          .toList();
      expect(resentTurns.length, greaterThanOrEqualTo(2));
      expect(controller.timeline.where((item) => item.body == '半开连接消息'),
          hasLength(1));
    });

    test('unacknowledged relay send reconnects despite server deltas',
        () async {
      final service = _FakeMobileVcWsService();
      final controller = SessionController(
        service: service,
        outboundAckRetryDelay: const Duration(milliseconds: 40),
        outboundAckStaleTimeout: const Duration(milliseconds: 20),
      );
      await controller.initialize();
      addTearDown(controller.disposeController);

      await controller.saveConfig(const AppConfig(
        connectionMode: 'relay',
        relayUrl: 'wss://relay.example.test',
        relaySessionId: 'rs_test',
        relayClientId: 'rc_test',
        relayClientReconnectSecret: 'reconnect_secret',
      ));
      await controller.connect();
      service.emit(
        SessionCreatedEvent(
          timestamp: _timestamp,
          sessionId: 'session-1',
          runtimeMeta: const RuntimeMeta(),
          raw: const {'type': 'session_created'},
          summary: const SessionSummary(id: 'session-1', title: 'Relay'),
        ),
      );
      await _flushEvents();
      service.sentPayloads.clear();

      controller.sendInputText('有心跳但无确认');
      await _flushEvents();

      final initialTurn = service.sentPayloads.singleWhere(
        (payload) => payload['action'] == 'ai_turn',
      );
      final clientActionId = initialTurn['clientActionId'];
      service.emit(SessionStateEvent(
        timestamp: _timestamp.add(const Duration(milliseconds: 30)),
        sessionId: 'session-1',
        runtimeMeta: const RuntimeMeta(),
        raw: const {'type': 'session_state'},
        state: 'idle',
        message: 'heartbeat',
      ));
      await _flushEvents();

      await Future<void>.delayed(const Duration(milliseconds: 120));
      await _flushEvents();

      expect(service.disconnectCalls, greaterThanOrEqualTo(1));
      expect(service.connectCalls, greaterThanOrEqualTo(2));
      final resentTurns = service.sentPayloads
          .where((payload) =>
              payload['action'] == 'ai_turn' &&
              payload['clientActionId'] == clientActionId)
          .toList();
      expect(resentTurns.length, greaterThanOrEqualTo(2));
      expect(controller.timeline.where((item) => item.body == '有心跳但无确认'),
          hasLength(1));
    });

    test('relay health monitor requires matching pong before miss reconnect',
        () async {
      final service = _FakeMobileVcWsService();
      final controller = SessionController(
        service: service,
        connectionHealthInterval: const Duration(milliseconds: 40),
        maxMissedHealthPongs: 2,
      );
      await controller.initialize();
      addTearDown(controller.disposeController);

      await controller.saveConfig(const AppConfig(
        connectionMode: 'relay',
        relayUrl: 'wss://relay.example.test',
        relaySessionId: 'rs_test',
        relayClientId: 'rc_test',
        relayClientReconnectSecret: 'reconnect_secret',
      ));
      await controller.connect();
      service.sentPayloads.clear();
      service.emit(
        SessionCreatedEvent(
          timestamp: _timestamp,
          sessionId: 'session-health',
          runtimeMeta: const RuntimeMeta(),
          raw: const {'type': 'session_created'},
          summary: const SessionSummary(id: 'session-health', title: 'Relay'),
        ),
      );
      await _flushEvents();

      await Future<void>.delayed(const Duration(milliseconds: 45));
      await _flushEvents();
      final firstPing = service.sentPayloads.singleWhere(
        (payload) => payload['action'] == 'ping',
      );
      final firstPingId = firstPing['pingId'] as String;
      expect(firstPingId, isNotEmpty);

      service.emit(PongEvent(
        timestamp: _timestamp.add(const Duration(milliseconds: 30)),
        sessionId: 'session-health',
        runtimeMeta: const RuntimeMeta(),
        raw: {'type': 'pong', 'pingId': firstPingId},
        pingId: firstPingId,
      ));
      await _flushEvents();
      await Future<void>.delayed(const Duration(milliseconds: 45));
      await _flushEvents();

      expect(service.disconnectCalls, 0);
      expect(controller.connected, isTrue);

      await Future<void>.delayed(const Duration(milliseconds: 130));
      await _flushEvents();

      expect(service.disconnectCalls, greaterThanOrEqualTo(1));
      expect(service.connectCalls, greaterThanOrEqualTo(2));
      expect(controller.connectionStage, isNot(SessionConnectionStage.failed));
    });

    test('relay reconnect uses persisted client credentials', () async {
      SharedPreferences.setMockInitialValues({
        'mobilevc.app_config': jsonEncode(const AppConfig(
          connectionMode: 'relay',
          relayUrl: 'wss://relay.example.test',
          relaySessionId: 'rs_test',
          relayClientId: 'rc_test',
          relayClientReconnectSecret: 'reconnect_secret',
        ).toJson()),
      });
      final service = _FakeMobileVcWsService();
      final controller = SessionController(service: service);
      await controller.initialize();
      addTearDown(controller.disposeController);

      await controller.connect();

      expect(service.connectedRelays.single.sessionId, 'rs_test');
      expect(service.connectedRelays.single.pairingSecret, isEmpty);
      expect(service.connectedRelays.single.clientId, 'rc_test');
      expect(
        service.connectedRelays.single.clientReconnectSecret,
        'reconnect_secret',
      );
      expect(controller.connected, isTrue);
    });

    test('imported relay pairing link overrides stale reconnect credentials',
        () async {
      SharedPreferences.setMockInitialValues({
        'mobilevc.app_config': jsonEncode(const AppConfig(
          connectionMode: 'relay',
          relayUrl: 'wss://relay.example.test',
          relaySessionId: 'rs_old',
          relayClientId: 'rc_old',
          relayClientReconnectSecret: 'old_reconnect_secret',
        ).toJson()),
      });
      final service = _FakeMobileVcWsService();
      final controller = SessionController(service: service);
      await controller.initialize();
      addTearDown(controller.disposeController);

      final imported = await controller.importConnectionLink(
        'mobilevc://relay/v1?relay=wss%3A%2F%2Frelay.example.test'
        '&session=rs_new&secret=new_pair_secret&exp=4102444800'
        '&nodeFingerprint=1111111111111111111111111111111111111111111111111111111111111111'
        '&relayProtocolVersion=1&e2eeProtocolVersion=1'
        '&cryptoSuite=p256-ecdsa%2Bp256-ecdh%2Bhkdf-sha256%2Baes-256-gcm'
        '&tunnelProtocolVersion=1&supportsMultiplexStreams=true'
        '&supportsFileDownloadStream=true&supportsDeviceManagement=true'
        '&requiresE2EE=true&plaintextTestMode=false',
      );
      expect(imported, isTrue);

      await controller.connect();

      final call = service.connectedRelays.single;
      expect(call.sessionId, 'rs_new');
      expect(call.pairingSecret, 'new_pair_secret');
      expect(call.clientId, isEmpty);
      expect(call.clientReconnectSecret, isEmpty);
      expect(controller.config.relayClientId, 'rc_test');
      expect(controller.config.relayClientReconnectSecret, 'reconnect_secret');
    });

    test('reimporting same relay link keeps existing reconnect credentials',
        () async {
      SharedPreferences.setMockInitialValues({
        'mobilevc.app_config': jsonEncode(const AppConfig(
          connectionMode: 'relay',
          relayUrl: 'wss://relay.example.test',
          relaySessionId: 'rs_test',
          relayClientId: 'rc_existing',
          relayClientReconnectSecret: 'reconnect_existing',
        ).toJson()),
      });
      final service = _FakeMobileVcWsService();
      final controller = SessionController(service: service);
      await controller.initialize();
      addTearDown(controller.disposeController);

      final imported = await controller.importConnectionLink(
        'mobilevc://relay/v1?relay=wss%3A%2F%2Frelay.example.test'
        '&session=rs_test&secret=stale_pair_secret&exp=4102444800'
        '&nodeFingerprint=1111111111111111111111111111111111111111111111111111111111111111'
        '&relayProtocolVersion=1&e2eeProtocolVersion=1'
        '&cryptoSuite=p256-ecdsa%2Bp256-ecdh%2Bhkdf-sha256%2Baes-256-gcm'
        '&tunnelProtocolVersion=1&supportsMultiplexStreams=true'
        '&supportsFileDownloadStream=true&supportsDeviceManagement=true'
        '&requiresE2EE=true&plaintextTestMode=false',
      );

      expect(imported, isTrue);
      expect(controller.config.relayPairingSecret, isEmpty);
      expect(controller.config.relayClientId, 'rc_existing');
      expect(
          controller.config.relayClientReconnectSecret, 'reconnect_existing');

      await controller.connect();

      final call = service.connectedRelays.single;
      expect(call.pairingSecret, isEmpty);
      expect(call.clientId, 'rc_existing');
      expect(call.clientReconnectSecret, 'reconnect_existing');
    });

    test('reimporting same connected relay link does not disconnect', () async {
      SharedPreferences.setMockInitialValues({
        'mobilevc.app_config': jsonEncode(const AppConfig(
          connectionMode: 'relay',
          relayUrl: 'wss://relay.example.test',
          relaySessionId: 'rs_test',
          relayClientId: 'rc_existing',
          relayClientReconnectSecret: 'reconnect_existing',
        ).toJson()),
      });
      final service = _FakeMobileVcWsService();
      final controller = SessionController(service: service);
      await controller.initialize();
      addTearDown(controller.disposeController);

      await controller.connect();
      expect(controller.connected, isTrue);

      final imported = await controller.importConnectionLink(
        'mobilevc://relay/v1?relay=wss%3A%2F%2Frelay.example.test'
        '&session=rs_test&secret=stale_pair_secret&exp=1'
        '&nodeFingerprint=1111111111111111111111111111111111111111111111111111111111111111'
        '&relayProtocolVersion=1&e2eeProtocolVersion=1'
        '&cryptoSuite=p256-ecdsa%2Bp256-ecdh%2Bhkdf-sha256%2Baes-256-gcm'
        '&tunnelProtocolVersion=1&supportsMultiplexStreams=true'
        '&supportsFileDownloadStream=true&supportsDeviceManagement=true'
        '&requiresE2EE=true&plaintextTestMode=false',
      );

      expect(imported, isTrue);
      expect(controller.connected, isTrue);
      expect(controller.connectionStage, SessionConnectionStage.connected);
      expect(controller.connectionMessage, '已恢复 Relay 连接配置');
      expect(service.disconnectCalls, 0);
      expect(controller.config.relayPairingSecret, isEmpty);
      expect(controller.config.relayClientId, 'rc_existing');
      expect(
        controller.config.relayClientReconnectSecret,
        'reconnect_existing',
      );
    });

    test('imported relay pairing link disconnects active stale relay session',
        () async {
      SharedPreferences.setMockInitialValues({});
      final service = _FakeMobileVcWsService();
      final controller = SessionController(service: service);
      await controller.initialize();
      addTearDown(controller.disposeController);

      await controller.saveConfig(const AppConfig(
        connectionMode: 'relay',
        relayUrl: 'wss://relay.example.test',
        relaySessionId: 'rs_old',
        relayClientId: 'rc_old',
        relayClientReconnectSecret: 'old_reconnect_secret',
      ));
      await controller.connect();
      expect(controller.connected, isTrue);

      final imported = await controller.importConnectionLink(
        'mobilevc://relay/v1?relay=wss%3A%2F%2Frelay.example.test'
        '&session=rs_new&secret=new_pair_secret&exp=4102444800'
        '&nodeFingerprint=1111111111111111111111111111111111111111111111111111111111111111'
        '&relayProtocolVersion=1&e2eeProtocolVersion=1'
        '&cryptoSuite=p256-ecdsa%2Bp256-ecdh%2Bhkdf-sha256%2Baes-256-gcm'
        '&tunnelProtocolVersion=1&supportsMultiplexStreams=true'
        '&supportsFileDownloadStream=true&supportsDeviceManagement=true'
        '&requiresE2EE=true&plaintextTestMode=false',
      );

      expect(imported, isTrue);
      expect(service.disconnectCalls, 1);
      expect(controller.connected, isFalse);
      expect(controller.connecting, isFalse);
      expect(controller.autoReconnectEnabled, isFalse);
      expect(controller.connectionStage, SessionConnectionStage.disconnected);
      expect(controller.connectionMessage, '已导入 Relay 配对，点击连接完成配对');
      expect(controller.config.relaySessionId, 'rs_new');
      expect(controller.config.relayPairingSecret, 'new_pair_secret');
      expect(controller.config.relayClientId, isEmpty);
      expect(controller.config.relayClientReconnectSecret, isEmpty);

      final prefs = await SharedPreferences.getInstance();
      expect(prefs.getBool('mobilevc.connection_intent'), isFalse);
      final persisted = jsonDecode(prefs.getString('mobilevc.app_config')!)
          as Map<String, dynamic>;
      expect(persisted['relaySessionId'], 'rs_new');
      expect(persisted.containsKey('relayPairingSecret'), isFalse);
      expect(persisted.containsKey('relayClientId'), isFalse);
      expect(persisted.containsKey('relayClientReconnectSecret'), isFalse);
    });

    test('relay device register result enables management list refresh',
        () async {
      SharedPreferences.setMockInitialValues({});
      final service = _FakeMobileVcWsService()
        ..relayE2eeHandshake = true
        ..relayE2eeDeviceBinding = false;
      final controller = SessionController(service: service);
      await controller.initialize();
      addTearDown(controller.disposeController);

      await controller.saveConfig(const AppConfig(
        connectionMode: 'relay',
        relayUrl: 'wss://relay.example.test',
        relaySessionId: 'rs_test',
        relayClientId: 'rc_test',
        relayClientReconnectSecret: 'reconnect_secret',
      ));
      await controller.connect();
      await _flushEvents();

      expect(
        service.sentPayloads
            .where((payload) => payload['action'] == 'relay_device_list'),
        isEmpty,
      );

      service.emit(RelayDeviceRegisterResultEvent.fromJson(const {
        'type': 'relay_device_register_result',
        'sessionId': 'rs_test',
        'deviceId': 'dev_current',
        'fingerprintHex':
            '1111111111111111111111111111111111111111111111111111111111111111',
        'status': 'registered',
      }));
      await _flushEvents();

      expect(service.markedRelayDeviceId, 'dev_current');
      expect(
        service.sentPayloads
            .where((payload) => payload['action'] == 'relay_device_list'),
        hasLength(1),
      );
    });

    test('relay device list and revoke use E2EE management actions', () async {
      SharedPreferences.setMockInitialValues({});
      final service = _FakeMobileVcWsService()
        ..relayE2eeHandshake = true
        ..relayE2eeDeviceBinding = true;
      final controller = SessionController(service: service);
      await controller.initialize();
      addTearDown(controller.disposeController);

      await controller.saveConfig(const AppConfig(
        connectionMode: 'relay',
        relayUrl: 'wss://relay.example.test',
        relaySessionId: 'rs_test',
        relayClientId: 'rc_test',
        relayClientReconnectSecret: 'reconnect_secret',
      ));
      await controller.connect();
      await _flushEvents();

      expect(
        service.sentPayloads
            .where((payload) => payload['action'] == 'relay_device_list'),
        isNotEmpty,
      );

      service.emit(RelayDeviceListResultEvent.fromJson(const {
        'type': 'relay_device_list_result',
        'sessionId': 'rs_test',
        'devices': [
          {
            'deviceId': 'dev_current',
            'displayName': 'Pixel',
            'fingerprintHex':
                '1111111111111111111111111111111111111111111111111111111111111111',
            'createdAt': '2026-05-21T10:00:00Z',
            'lastSeenAt': '2026-05-21T10:02:00Z',
            'activeSessionId': 'hs_current',
            'connected': true,
            'currentDevice': true,
          },
          {
            'deviceId': 'dev_lost',
            'displayName': 'Old phone',
            'fingerprintHex':
                '2222222222222222222222222222222222222222222222222222222222222222',
            'createdAt': '2026-05-20T10:00:00Z',
            'lastSeenAt': '2026-05-20T10:02:00Z',
          },
        ],
      }));
      await _flushEvents();

      expect(controller.relayDevices.length, 2);
      controller.revokeRelayDevice('dev_lost');
      expect(service.sentPayloads.last, {
        'action': 'relay_device_revoke',
        'deviceId': 'dev_lost',
      });

      controller.revokeRelayDevice('dev_current');
      expect(service.sentPayloads.last['deviceId'], 'dev_lost');
      expect(controller.relayDeviceStatus, contains('不能'));
    });

    test('relay device rotate clears local binding and disconnects', () async {
      SharedPreferences.setMockInitialValues({});
      final service = _FakeMobileVcWsService()
        ..relayE2eeHandshake = true
        ..relayE2eeDeviceBinding = true;
      final controller = SessionController(service: service);
      await controller.initialize();
      addTearDown(controller.disposeController);

      await controller.saveConfig(const AppConfig(
        connectionMode: 'relay',
        relayUrl: 'wss://relay.example.test',
        relaySessionId: 'rs_test',
        relayClientId: 'rc_test',
        relayClientReconnectSecret: 'reconnect_secret',
        relayNodeFingerprintHex:
            '1111111111111111111111111111111111111111111111111111111111111111',
      ));
      await controller.connect();
      await _flushEvents();

      controller.rotateRelayDevices();
      expect(service.sentPayloads.last, {'action': 'relay_device_rotate'});

      service.emit(RelayDeviceRotateResultEvent.fromJson(const {
        'type': 'relay_device_rotate_result',
        'sessionId': 'rs_test',
        'nodeFingerprintHex':
            '2222222222222222222222222222222222222222222222222222222222222222',
        'status': 'rotated',
      }));
      await _flushEvents();

      expect(service.resetRelayBindingCalls, 1);
      expect(service.disconnectCalls, greaterThanOrEqualTo(1));
      expect(controller.connected, isFalse);
      expect(controller.config.relaySessionId, isEmpty);
      expect(controller.config.relayClientId, isEmpty);
      expect(controller.config.relayClientReconnectSecret, isEmpty);
      expect(
        controller.config.relayNodeFingerprintHex,
        '2222222222222222222222222222222222222222222222222222222222222222',
      );
      expect(controller.relayDeviceStatus, contains('重新配对'));
    });

    test('relay security state requires backend confirmed device binding',
        () async {
      SharedPreferences.setMockInitialValues({});
      final service = _FakeMobileVcWsService()
        ..relayE2eeHandshake = true
        ..relayE2eeDeviceBinding = false;
      final expectedFingerprint =
          await _testFingerprintHex(service.relayNodeFingerprint);
      final controller = SessionController(service: service);
      await controller.initialize();
      addTearDown(controller.disposeController);

      await controller.saveConfig(AppConfig(
        connectionMode: 'relay',
        relayUrl: 'wss://relay.example.test',
        relaySessionId: 'rs_test',
        relayClientId: 'rc_test',
        relayClientReconnectSecret: 'reconnect_secret',
        relayNodeFingerprintHex: expectedFingerprint,
        relayCapabilities: RelayE2eeCapabilitySet.production(),
      ));
      await controller.connect();

      final waiting = await controller.relaySecurityState();
      expect(waiting.mode, RelaySecurityMode.relayNotVerified);
      expect(waiting.canShowVerified, isFalse);

      service.relayE2eeDeviceBinding = true;
      final verified = await controller.relaySecurityState();
      expect(verified.mode, RelaySecurityMode.relayE2eeVerified);
      expect(verified.canShowVerified, isTrue);
      expect(verified.actualFingerprintHex, expectedFingerprint);
    });

    test('relay connect rejects public ws url before service connect',
        () async {
      final service = _FakeMobileVcWsService();
      final controller = SessionController(service: service);
      await controller.initialize();
      addTearDown(controller.disposeController);

      await controller.saveConfig(const AppConfig(
        connectionMode: 'relay',
        relayUrl: 'ws://relay.example.test',
        relaySessionId: 'rs_test',
        relayPairingSecret: 'pair_secret',
      ));
      await controller.connect();

      expect(service.connectedRelays, isEmpty);
      expect(controller.connected, isFalse);
      expect(controller.connectionStage, SessionConnectionStage.failed);
    });

    test('notification restore immediately loads target session when connected',
        () async {
      final service = _FakeMobileVcWsService();
      final controller = SessionController(service: service);
      await controller.initialize();
      addTearDown(controller.disposeController);

      await controller.connect();
      service.sentPayloads.clear();

      await controller.restoreSessionFromNotification('session-target');

      expect(service.sentPayloads, isNotEmpty);
      expect(service.sentPayloads.last['action'], 'session_load');
      expect(service.sentPayloads.last['sessionId'], 'session-target');
    });

    test('notification restore reconnects first when disconnected', () async {
      final service = _FakeMobileVcWsService();
      final controller = SessionController(service: service);
      await controller.initialize();
      addTearDown(controller.disposeController);

      await controller.restoreSessionFromNotification('session-target');

      expect(service.connectCalls, 1);
      expect(
          service.sentPayloads.any((item) => item['action'] == 'session_load'),
          isTrue);
    });

    test('notification restore target clears after matching history arrives',
        () async {
      final service = _FakeMobileVcWsService();
      final controller = SessionController(service: service);
      await controller.initialize();
      addTearDown(controller.disposeController);

      await controller.connect();
      service.sentPayloads.clear();
      await controller.restoreSessionFromNotification('session-target');
      final sentBeforeHistory = service.sentPayloads.length;

      service.emit(
        SessionHistoryEvent(
          timestamp: _timestamp,
          sessionId: 'session-target',
          runtimeMeta: const RuntimeMeta(),
          raw: const {'type': 'session_history'},
          summary: const SessionSummary(
            id: 'session-target',
            title: 'Target',
          ),
          logEntries: const [],
          diffs: const [],
          reviewGroups: const [],
          rawTerminalByStream: const {},
          terminalExecutions: const [],
          sessionContext: const SessionContext(),
          skillCatalogMeta: const CatalogMetadata(domain: 'skill'),
          memoryCatalogMeta: const CatalogMetadata(domain: 'memory'),
          resumeRuntimeMeta: const RuntimeMeta(),
        ),
      );
      await _flushEvents();
      service.sentPayloads.clear();

      service.emit(
        SessionStateEvent(
          timestamp: _timestamp,
          sessionId: 'session-target',
          runtimeMeta: const RuntimeMeta(),
          raw: const {'type': 'session_state'},
          state: 'idle',
          message: 'ready',
        ),
      );
      await _flushEvents();

      expect(sentBeforeHistory, greaterThan(0));
      expect(
          service.sentPayloads.any((item) => item['action'] == 'session_load'),
          isFalse);
    });

    test('运行态进入普通 WAIT_INPUT 时产出继续输入信号', () async {
      final service = _FakeMobileVcWsService();
      final controller = SessionController(service: service);
      await controller.initialize();
      addTearDown(controller.disposeController);

      await controller.connect();
      service.emit(
        AgentStateEvent(
          timestamp: _timestamp,
          sessionId: 'session-1',
          runtimeMeta:
              const RuntimeMeta(command: 'claude', executionId: 'exec-1'),
          raw: const {'type': 'agent_state'},
          state: 'THINKING',
          message: '思考中',
          command: 'claude',
        ),
      );
      await _flushEvents();
      expect(controller.actionNeededSignal, isNull);

      service.emit(
        AgentStateEvent(
          timestamp: _timestamp,
          sessionId: 'session-1',
          runtimeMeta: const RuntimeMeta(
            command: 'claude',
            executionId: 'exec-1',
            blockingKind: 'ready',
          ),
          raw: const {'type': 'agent_state'},
          state: 'WAIT_INPUT',
          message: '等待继续输入',
          awaitInput: true,
          command: 'claude',
        ),
      );
      await _flushEvents();

      final signal = _expectSignal(controller, ActionNeededType.continueInput);
      expect(signal.message, 'AI 助手需要你继续输入');
    });

    test('仅残留 permission_blocked phase 时不显示授权确认', () async {
      final service = _FakeMobileVcWsService();
      final controller = SessionController(service: service);
      await controller.initialize();
      addTearDown(controller.disposeController);

      await controller.connect();
      service.emit(
        RuntimePhaseEvent(
          timestamp: _timestamp,
          sessionId: 'session-1',
          runtimeMeta: const RuntimeMeta(command: 'claude'),
          raw: const {'type': 'runtime_phase'},
          phase: 'permission_blocked',
          kind: 'permission',
          message: 'Allow edit a.dart?',
        ),
      );
      await _flushEvents();

      expect(controller.shouldShowPermissionChoices, isFalse);
      expect(controller.hasPendingPermissionPrompt, isFalse);

      controller.sendInputText('你好');

      expect(service.sentPayloads, isNotEmpty);
      expect(service.sentPayloads.last['action'], 'ai_turn');
      expect(service.sentPayloads.last['data'], '你好\n');
      expect(controller.timeline.any((item) => item.body.contains('请先在上方完成授权')),
          isFalse);
    });

    test('permission prompt 到来时产出权限确认信号', () async {
      final service = _FakeMobileVcWsService();
      final controller = SessionController(service: service);
      await controller.initialize();
      addTearDown(controller.disposeController);

      await controller.connect();
      service.emit(
        RuntimePhaseEvent(
          timestamp: _timestamp,
          sessionId: 'session-1',
          runtimeMeta: const RuntimeMeta(command: 'claude'),
          raw: const {'type': 'runtime_phase'},
          phase: 'permission_blocked',
          kind: 'permission',
          message: 'Allow edit a.dart?',
        ),
      );
      service.emit(
        InteractionRequestEvent(
          timestamp: _timestamp,
          sessionId: 'session-1',
          runtimeMeta: const RuntimeMeta(
            command: 'claude',
            contextId: 'perm-1',
            targetPath: '/workspace/a.dart',
          ),
          raw: const {
            'type': 'interaction_request',
            'kind': 'permission',
            'title': 'Permission required',
            'message': 'Allow edit a.dart?',
          },
          kind: 'permission',
          title: 'Permission required',
          message: 'Allow edit a.dart?',
          options: const [PromptOption(value: 'y'), PromptOption(value: 'n')],
        ),
      );
      await _flushEvents();

      final signal = _expectSignal(controller, ActionNeededType.permission);
      expect(signal.message, 'AI 助手需要你确认权限');
    });

    test('permission-like prompt_request 到来时也进入权限状态', () async {
      final service = _FakeMobileVcWsService();
      final controller = SessionController(service: service);
      await controller.initialize();
      addTearDown(controller.disposeController);

      await controller.connect();
      service.emit(
        PromptRequestEvent(
          timestamp: _timestamp,
          sessionId: 'session-1',
          runtimeMeta: const RuntimeMeta(
            command: 'claude',
            contextId: 'perm-prompt-1',
            targetPath: '/workspace/a.dart',
            blockingKind: 'permission',
          ),
          raw: const {
            'type': 'prompt_request',
            'msg': 'Allow edit a.dart?',
          },
          message: 'Allow edit a.dart?',
          options: const [PromptOption(value: 'y'), PromptOption(value: 'n')],
        ),
      );
      await _flushEvents();

      expect(controller.shouldShowPermissionChoices, isTrue);
      final signal = _expectSignal(controller, ActionNeededType.permission);
      expect(signal.message, 'AI 助手需要你确认权限');
    });

    test('review prompt 到来时产出审核信号', () async {
      final service = _FakeMobileVcWsService();
      final controller = SessionController(service: service);
      await controller.initialize();
      addTearDown(controller.disposeController);

      await controller.connect();
      service.emit(_reviewDiffEvent(
        contextId: 'diff-1',
        path: '/workspace/a.dart',
        title: 'a.dart',
        groupId: 'group-1',
        groupTitle: '组一',
      ));
      service.emit(
        AgentStateEvent(
          timestamp: _timestamp,
          sessionId: 'session-1',
          runtimeMeta:
              const RuntimeMeta(command: 'claude', contextId: 'diff-1'),
          raw: const {'type': 'agent_state'},
          state: 'WAIT_INPUT',
          message: '等待审核',
          awaitInput: true,
          command: 'claude',
        ),
      );
      service.emit(
        PromptRequestEvent(
          timestamp: _timestamp,
          sessionId: 'session-1',
          runtimeMeta: const RuntimeMeta(
            command: 'claude',
            contextId: 'diff-1',
            targetPath: '/workspace/a.dart',
          ),
          raw: const {
            'type': 'prompt_request',
            'msg': 'Please accept, revert, or revise this diff',
          },
          message: 'Please accept, revert, or revise this diff',
          options: const [
            PromptOption(value: 'accept'),
            PromptOption(value: 'revert'),
            PromptOption(value: 'revise'),
          ],
        ),
      );
      await _flushEvents();

      final signal = _expectSignal(controller, ActionNeededType.review);
      expect(signal.message, 'AI 助手需要你处理代码审核');
    });

    test('普通 prompt 到来时产出等待回复信号', () async {
      final service = _FakeMobileVcWsService();
      final controller = SessionController(service: service);
      await controller.initialize();
      addTearDown(controller.disposeController);

      await controller.connect();
      service.emit(
        PromptRequestEvent(
          timestamp: _timestamp,
          sessionId: 'session-1',
          runtimeMeta:
              const RuntimeMeta(command: 'claude', contextId: 'prompt-1'),
          raw: const {'type': 'prompt_request', 'msg': '请补充上下文'},
          message: '请补充上下文',
          options: const [],
        ),
      );
      await _flushEvents();

      final signal = _expectSignal(controller, ActionNeededType.reply);
      expect(signal.message, 'AI 助手正在等待你的回复');
    });

    test('permission-like prompt_request 遇到普通 ready prompt 时会切回继续输入', () async {
      final service = _FakeMobileVcWsService();
      final controller = SessionController(service: service);
      await controller.initialize();
      addTearDown(controller.disposeController);

      await controller.connect();
      service.emit(
        RuntimePhaseEvent(
          timestamp: _timestamp,
          sessionId: 'session-1',
          runtimeMeta: RuntimeMeta(command: 'claude'),
          raw: const {'type': 'runtime_phase'},
          phase: 'permission_blocked',
          kind: 'permission',
          message: 'Allow edit a.dart?',
        ),
      );
      service.emit(
        PromptRequestEvent(
          timestamp: _timestamp,
          sessionId: 'session-1',
          runtimeMeta: const RuntimeMeta(
            command: 'claude',
            contextId: 'perm-1',
            targetPath: '/workspace/a.dart',
            blockingKind: 'permission',
          ),
          raw: const {'type': 'prompt_request', 'msg': 'Allow edit a.dart?'},
          message: 'Allow edit a.dart?',
          options: const [PromptOption(value: 'y'), PromptOption(value: 'n')],
        ),
      );
      await _flushEvents();
      expect(controller.shouldShowPermissionChoices, isTrue);
      expect(controller.pendingPrompt?.message, 'Allow edit a.dart?');

      service.emit(
        PromptRequestEvent(
          timestamp: _timestamp.add(const Duration(seconds: 1)),
          sessionId: 'session-1',
          runtimeMeta: const RuntimeMeta(
            command: 'claude',
            blockingKind: 'ready',
          ),
          raw: const {'type': 'prompt_request', 'msg': 'AI 会话已就绪，可继续输入'},
          message: 'AI 会话已就绪，可继续输入',
          options: const [],
        ),
      );
      await _flushEvents();

      expect(controller.shouldShowPermissionChoices, isFalse);
      expect(controller.pendingPrompt?.message, 'AI 会话已就绪，可继续输入');
      expect(controller.pendingInteraction, isNull);
      final signal = _expectSignal(controller, ActionNeededType.continueInput);
      expect(signal.message, 'AI 助手需要你继续输入');
    });

    test('review prompt 不会被普通 WAIT_INPUT 更新覆盖', () async {
      final service = _FakeMobileVcWsService();
      final controller = SessionController(service: service);
      await controller.initialize();
      addTearDown(controller.disposeController);

      await controller.connect();
      service.emit(_reviewDiffEvent(
        contextId: 'diff-1',
        path: '/workspace/a.dart',
        title: 'a.dart',
        groupId: 'group-1',
        groupTitle: '组一',
      ));
      service.emit(
        AgentStateEvent(
          timestamp: _timestamp,
          sessionId: 'session-1',
          runtimeMeta:
              const RuntimeMeta(command: 'claude', contextId: 'diff-1'),
          raw: const {'type': 'agent_state'},
          state: 'WAIT_INPUT',
          message: '等待审核',
          awaitInput: true,
          command: 'claude',
        ),
      );
      service.emit(
        PromptRequestEvent(
          timestamp: _timestamp,
          sessionId: 'session-1',
          runtimeMeta: const RuntimeMeta(
            command: 'claude',
            contextId: 'diff-1',
            targetPath: '/workspace/a.dart',
          ),
          raw: const {
            'type': 'prompt_request',
            'msg': 'Please accept, revert, or revise this diff',
          },
          message: 'Please accept, revert, or revise this diff',
          options: const [
            PromptOption(value: 'accept'),
            PromptOption(value: 'revert'),
            PromptOption(value: 'revise'),
          ],
        ),
      );
      await _flushEvents();
      expect(controller.shouldShowReviewChoices, isTrue);

      service.emit(
        AgentStateEvent(
          timestamp: _timestamp.add(const Duration(seconds: 1)),
          sessionId: 'session-1',
          runtimeMeta: const RuntimeMeta(command: 'claude'),
          raw: const {'type': 'agent_state'},
          state: 'WAIT_INPUT',
          message: '等待输入',
          awaitInput: true,
          command: 'claude',
        ),
      );
      expect(controller.currentReviewDiff?.path, '/workspace/a.dart');
      final signal = _expectSignal(controller, ActionNeededType.review);
      expect(signal.message, 'AI 助手需要你处理代码审核');
    });

    test('普通 WAIT_INPUT 更新不会覆盖已有普通 prompt', () async {
      final service = _FakeMobileVcWsService();
      final controller = SessionController(service: service);
      await controller.initialize();
      addTearDown(controller.disposeController);

      await controller.connect();
      service.emit(
        PromptRequestEvent(
          timestamp: _timestamp,
          sessionId: 'session-1',
          runtimeMeta:
              const RuntimeMeta(command: 'claude', contextId: 'prompt-1'),
          raw: const {'type': 'prompt_request', 'msg': '请补充上下文'},
          message: '请补充上下文',
          options: const [],
        ),
      );
      await _flushEvents();
      expect(controller.pendingPrompt?.message, '请补充上下文');

      service.emit(
        AgentStateEvent(
          timestamp: _timestamp.add(const Duration(seconds: 1)),
          sessionId: 'session-1',
          runtimeMeta: const RuntimeMeta(command: 'claude'),
          raw: const {'type': 'agent_state'},
          state: 'WAIT_INPUT',
          message: '等待输入',
          awaitInput: true,
          command: 'claude',
        ),
      );
      await _flushEvents();

      expect(controller.pendingPrompt?.message, '请补充上下文');
      final signal = _expectSignal(controller, ActionNeededType.reply);
      expect(signal.message, 'AI 助手正在等待你的回复');
    });

    test('断开连接时不发信号', () async {
      final service = _FakeMobileVcWsService();
      final controller = SessionController(service: service);
      await controller.initialize();
      addTearDown(controller.disposeController);

      service.emit(
        PromptRequestEvent(
          timestamp: _timestamp,
          sessionId: 'session-1',
          runtimeMeta:
              const RuntimeMeta(command: 'claude', contextId: 'prompt-1'),
          raw: const {'type': 'prompt_request', 'msg': '请补充上下文'},
          message: '请补充上下文',
          options: const [],
        ),
      );
      await _flushEvents();

      expect(controller.actionNeededSignal, isNull);
    });

    test('加载历史会话时不发信号', () async {
      final service = _FakeMobileVcWsService();
      final controller = SessionController(service: service);
      await controller.initialize();
      addTearDown(controller.disposeController);

      await controller.connect();
      service.emit(
        SessionHistoryEvent(
          timestamp: _timestamp,
          sessionId: 'session-1',
          runtimeMeta: const RuntimeMeta(command: 'claude'),
          raw: const {'type': 'session_history'},
          summary: const SessionSummary(id: 'session-1', title: '历史会话'),
          currentStep:
              const HistoryContext(message: '等待输入', status: 'WAIT_INPUT'),
          canResume: true,
        ),
      );
      service.emit(
        AgentStateEvent(
          timestamp: _timestamp,
          sessionId: 'session-1',
          runtimeMeta:
              const RuntimeMeta(command: 'claude', executionId: 'exec-his'),
          raw: const {'type': 'agent_state'},
          state: 'WAIT_INPUT',
          message: '历史等待输入',
          awaitInput: true,
          command: 'claude',
        ),
      );
      await _flushEvents();

      expect(controller.actionNeededSignal, isNull);
    });

    test('用户处理后下一轮等待态可以再次发信号', () async {
      final service = _FakeMobileVcWsService();
      final controller = SessionController(service: service);
      await controller.initialize();
      addTearDown(controller.disposeController);

      await controller.connect();
      service.emit(
        PromptRequestEvent(
          timestamp: _timestamp,
          sessionId: 'session-1',
          runtimeMeta:
              const RuntimeMeta(command: 'claude', contextId: 'prompt-1'),
          raw: const {'type': 'prompt_request', 'msg': '请补充上下文'},
          message: '请补充上下文',
          options: const [],
        ),
      );
      await _flushEvents();
      final firstId = _expectSignal(controller, ActionNeededType.reply).id;

      controller.sendInputText('补充内容');
      service.emit(
        AgentStateEvent(
          timestamp: _timestamp.add(const Duration(seconds: 1)),
          sessionId: 'session-1',
          runtimeMeta:
              const RuntimeMeta(command: 'claude', executionId: 'exec-2'),
          raw: const {'type': 'agent_state'},
          state: 'THINKING',
          message: '处理中',
          command: 'claude',
        ),
      );
      service.emit(
        AgentStateEvent(
          timestamp: _timestamp.add(const Duration(seconds: 2)),
          sessionId: 'session-1',
          runtimeMeta: const RuntimeMeta(
            command: 'claude',
            executionId: 'exec-2',
            blockingKind: 'ready',
          ),
          raw: const {'type': 'agent_state'},
          state: 'WAIT_INPUT',
          message: '等待继续输入',
          awaitInput: true,
          command: 'claude',
        ),
      );
      await _flushEvents();

      final second = _expectSignal(controller, ActionNeededType.continueInput);
      expect(second.id, greaterThan(firstId));
    });
    test('session_list 已同步且当前无会话时，首条普通输入会先自动创建会话再续发', () async {
      final service = _FakeMobileVcWsService();
      final controller = SessionController(service: service);
      await controller.initialize();
      addTearDown(controller.disposeController);

      await controller.connect();
      service.sentPayloads.clear();

      service.emit(
        SessionListResultEvent(
          timestamp: _timestamp,
          sessionId: 'conn-1',
          runtimeMeta: const RuntimeMeta(),
          raw: const {'type': 'session_list_result'},
          items: const [],
        ),
      );
      await _flushEvents();
      service.sentPayloads.clear();

      controller.sendInputText('pwd');

      expect(service.sentPayloads, hasLength(1));
      expect(service.sentPayloads.single['action'], 'session_create');

      service.emit(
        SessionCreatedEvent(
          timestamp: _timestamp,
          sessionId: 'conn-1',
          runtimeMeta: const RuntimeMeta(),
          raw: const {'type': 'session_created'},
          summary: const SessionSummary(id: 'session-new', title: '新会话'),
        ),
      );
      await _flushEvents();

      final actions =
          service.sentPayloads.map((item) => item['action']).toList();
      actions.remove('context_window_usage_get');
      expect(actions, ['session_create', 'exec']);
      expect(service.sentPayloads.last['cmd'], 'pwd');
      expect(controller.selectedSessionId, 'session-new');

      await Future<void>.delayed(const Duration(milliseconds: 520));
      await _flushEvents();

      final delayedActions =
          service.sentPayloads.map((item) => item['action']).toList();
      expect(delayedActions, contains('session_context_get'));
      expect(delayedActions, contains('permission_rule_list'));
    });

    test('session_list 已同步且当前无会话时，首条 claude 输入会先自动创建会话再启动 Claude', () async {
      final service = _FakeMobileVcWsService();
      final controller = SessionController(service: service);
      await controller.initialize();
      addTearDown(controller.disposeController);

      await controller.connect();
      service.sentPayloads.clear();

      service.emit(
        SessionListResultEvent(
          timestamp: _timestamp,
          sessionId: 'conn-1',
          runtimeMeta: const RuntimeMeta(),
          raw: const {'type': 'session_list_result'},
          items: const [],
        ),
      );
      await _flushEvents();
      service.sentPayloads.clear();

      controller.sendInputText('claude 请帮我总结当前问题');

      expect(service.sentPayloads, hasLength(1));
      expect(service.sentPayloads.single['action'], 'session_create');

      service.emit(
        SessionCreatedEvent(
          timestamp: _timestamp,
          sessionId: 'conn-1',
          runtimeMeta: const RuntimeMeta(),
          raw: const {'type': 'session_created'},
          summary: const SessionSummary(id: 'session-new', title: '新会话'),
        ),
      );
      await _flushEvents();

      final actions =
          service.sentPayloads.map((item) => item['action']).toList();
      actions.remove('context_window_usage_get');
      expect(actions, ['session_create', 'ai_turn']);
      final aiTurn = service.sentPayloads.singleWhere(
        (payload) => payload['action'] == 'ai_turn',
      );
      expect(aiTurn['engine'], 'claude');
      expect(aiTurn['data'], '请帮我总结当前问题\n');
      expect(controller.selectedSessionId, 'session-new');

      await Future<void>.delayed(const Duration(milliseconds: 520));
      await _flushEvents();

      final delayedActions =
          service.sentPayloads.map((item) => item['action']).toList();
      expect(delayedActions, contains('session_context_get'));
      expect(delayedActions, contains('permission_rule_list'));
    });

    test('已有选中会话时发送首条输入不会自动创建新会话', () async {
      final service = _FakeMobileVcWsService();
      final controller = SessionController(service: service);
      await controller.initialize();
      addTearDown(controller.disposeController);

      await controller.connect();
      service.sentPayloads.clear();

      service.emit(
        SessionCreatedEvent(
          timestamp: _timestamp,
          sessionId: 'conn-1',
          runtimeMeta: const RuntimeMeta(),
          raw: const {'type': 'session_created'},
          summary:
              const SessionSummary(id: 'session-current', title: 'Current'),
        ),
      );
      await _flushEvents();
      service.sentPayloads.clear();

      controller.sendInputText('pwd');

      expect(service.sentPayloads, hasLength(1));
      expect(service.sentPayloads.single['action'], 'exec');
      expect(service.sentPayloads.single['cmd'], 'pwd');
    });
  });

  group('SessionController Claude turn dispatch', () {
    test('sendInputText 非等待态输入 shell 命令时按 shell 执行', () async {
      final service = _FakeMobileVcWsService();
      final controller = SessionController(service: service);
      await controller.initialize();
      addTearDown(controller.disposeController);

      await controller.saveConfig(const AppConfig(host: '192.168.0.2'));
      await controller.connect();
      service.sentPayloads.clear();

      controller.sendInputText('pwd');

      expect(service.sentPayloads, hasLength(1));
      expect(service.sentPayloads[0]['action'], 'exec');
      expect(service.sentPayloads[0]['cmd'], 'pwd');
    });

    test('sendInputText 非等待态输入自然语言时启动 AI turn', () async {
      final service = _FakeMobileVcWsService();
      final controller = SessionController(service: service);
      await controller.initialize();
      addTearDown(controller.disposeController);

      await controller.saveConfig(const AppConfig(host: '192.168.0.2'));
      await controller.connect();
      service.sentPayloads.clear();

      controller.sendInputText('你好');

      expect(service.sentPayloads, hasLength(1));
      expect(service.sentPayloads[0]['action'], 'ai_turn');
      expect(service.sentPayloads[0]['engine'], 'claude');
      expect(service.sentPayloads[0]['data'], '你好\n');
      expect(service.sentPayloads[0].containsKey('cmd'), isFalse);
    });

    test('sendInputTextWithImages sends image attachments to AI turn',
        () async {
      final service = _FakeMobileVcWsService();
      final controller = SessionController(service: service);
      await controller.initialize();
      addTearDown(controller.disposeController);

      await controller.saveConfig(const AppConfig(host: '192.168.0.2'));
      await controller.connect();
      service.sentPayloads.clear();

      controller.sendInputTextWithImages(
        '看图',
        [
          ChatImageAttachment(
            name: 'screen.png',
            mimeType: 'image/png',
            bytes: utf8.encode('png-bytes'),
          ),
        ],
      );

      expect(service.sentPayloads, hasLength(1));
      final payload = service.sentPayloads.single;
      expect(payload['action'], 'ai_turn');
      expect(payload['data'], '看图\n');
      final attachments = payload['imageAttachments'] as List<dynamic>;
      expect(attachments, hasLength(1));
      final attachment = attachments.single as Map<String, dynamic>;
      expect(attachment['name'], 'screen.png');
      expect(attachment['mimeType'], 'image/png');
      expect(attachment['data'], base64Encode(utf8.encode('png-bytes')));
    });

    test('sendInputText 非等待态输入 claude 时只启动 Claude 不发送空 input', () async {
      final service = _FakeMobileVcWsService();
      final controller = SessionController(service: service);
      await controller.initialize();
      addTearDown(controller.disposeController);

      await controller.connect();
      service.sentPayloads.clear();

      controller.sendInputText('claude');

      expect(service.sentPayloads, hasLength(1));
      expect(service.sentPayloads[0]['action'], 'ai_turn');
      expect(service.sentPayloads[0]['engine'], 'claude');
      expect(service.sentPayloads[0]['model'], 'default');
    });

    test('Claude 模型切换会把选中的模型注入启动命令', () async {
      final service = _FakeMobileVcWsService();
      final controller = SessionController(service: service);
      await controller.initialize();
      addTearDown(controller.disposeController);

      await controller.saveConfig(
        const AppConfig(
          engine: 'claude',
          model: 'opus',
        ),
      );
      await controller.connect();
      service.sentPayloads.clear();

      controller.sendInputText('claude');

      expect(service.sentPayloads, hasLength(1));
      expect(service.sentPayloads[0]['action'], 'ai_turn');
      expect(service.sentPayloads[0]['engine'], 'claude');
      expect(service.sentPayloads[0]['model'], 'opus');
    });

    test('Claude 启动时不会把残留的 Codex 模型配置发给后端', () async {
      final service = _FakeMobileVcWsService();
      final controller = SessionController(service: service);
      await controller.initialize();
      addTearDown(controller.disposeController);

      await controller.saveConfig(
        const AppConfig(
          engine: 'claude',
          model: 'gpt-5-codex',
          reasoningEffort: 'high',
        ),
      );
      await controller.connect();
      service.sentPayloads.clear();

      controller.sendInputText('claude');

      expect(service.sentPayloads, hasLength(1));
      expect(service.sentPayloads[0]['action'], 'ai_turn');
      expect(service.sentPayloads[0]['engine'], 'claude');
      expect(service.sentPayloads[0]['model'], 'default');
    });

    test('sendInputText 非等待态输入 claude 后跟正文时会启动 Claude 并通过 input 发送正文',
        () async {
      final service = _FakeMobileVcWsService();
      final controller = SessionController(service: service);
      await controller.initialize();
      addTearDown(controller.disposeController);

      await controller.connect();
      service.sentPayloads.clear();

      controller.sendInputText('claude 请帮我总结当前问题');

      expect(service.sentPayloads, hasLength(1));
      expect(service.sentPayloads[0]['action'], 'ai_turn');
      expect(service.sentPayloads[0]['engine'], 'claude');
      expect(service.sentPayloads[0]['model'], 'default');
      expect(service.sentPayloads[0]['data'], '请帮我总结当前问题\n');
    });

    test('Codex 模型切换会把模型与推理强度注入启动命令', () async {
      final service = _FakeMobileVcWsService();
      final controller = SessionController(service: service);
      await controller.initialize();
      addTearDown(controller.disposeController);

      await controller.saveConfig(
        const AppConfig(
          engine: 'codex',
          model: 'gpt-5-codex',
          reasoningEffort: 'high',
        ),
      );
      await controller.connect();
      service.sentPayloads.clear();

      controller.sendInputText('codex');

      expect(service.sentPayloads, hasLength(1));
      expect(service.sentPayloads[0]['action'], 'ai_turn');
      expect(service.sentPayloads[0]['engine'], 'codex');
      expect(service.sentPayloads[0]['model'], 'gpt-5-codex');
      expect(service.sentPayloads[0]['reasoningEffort'], 'high');
    });

    test('Codex 已选择 gpt-5.5 时不会从旧 runtime command 回退到 gpt-5-codex', () async {
      final service = _FakeMobileVcWsService();
      final controller = SessionController(service: service);
      await controller.initialize();
      addTearDown(controller.disposeController);

      await controller.saveConfig(
        const AppConfig(
          engine: 'codex',
          codexModel: 'gpt-5.5',
          codexReasoningEffort: 'high',
        ),
      );
      await controller.connect();
      service.emit(
        SessionStateEvent(
          timestamp: _timestamp,
          sessionId: 'session-1',
          runtimeMeta: const RuntimeMeta(
            command: 'codex -m gpt-5-codex',
            engine: 'codex',
            model: 'gpt-5-codex',
          ),
          raw: const {'type': 'session_state'},
          state: 'IDLE',
          message: 'old codex session',
        ),
      );
      await _flushEvents();
      service.sentPayloads.clear();

      controller.sendInputText('codex 你好');

      expect(service.sentPayloads, hasLength(1));
      expect(service.sentPayloads[0]['action'], 'ai_turn');
      expect(service.sentPayloads[0]['engine'], 'codex');
      expect(service.sentPayloads[0]['model'], 'gpt-5.5');
      expect(service.sentPayloads[0]['reasoningEffort'], 'high');
      expect(service.sentPayloads[0].containsKey('command'), isFalse);
    });

    test('命令栏模型摘要使用保存的配置，不被旧 runtime meta 覆盖', () async {
      final service = _FakeMobileVcWsService();
      final controller = SessionController(service: service);
      await controller.initialize();
      addTearDown(controller.disposeController);

      await controller.saveConfig(
        const AppConfig(
          engine: 'codex',
          codexModel: 'gpt-5.5',
          codexReasoningEffort: 'high',
        ),
      );
      await controller.connect();
      service.emit(
        SessionStateEvent(
          timestamp: _timestamp,
          sessionId: 'session-1',
          runtimeMeta: const RuntimeMeta(
            command: 'codex -m gpt-5-codex',
            engine: 'codex',
            model: 'gpt-5-codex',
            reasoningEffort: 'medium',
          ),
          raw: const {'type': 'session_state'},
          state: 'IDLE',
          message: 'old codex session',
        ),
      );
      await _flushEvents();

      expect(controller.configuredAiEngine, 'codex');
      expect(controller.configuredAiModel, 'gpt-5.5');
      expect(controller.configuredAiReasoningEffort, 'high');
      expect(controller.commandBarModelSummary, 'gpt-5.5 · HIGH');
    });

    test('配置为 Codex 时模型更新不被旧 Claude runtime meta 分流', () async {
      final service = _FakeMobileVcWsService();
      final controller = SessionController(service: service);
      await controller.initialize();
      addTearDown(controller.disposeController);

      await controller.saveConfig(
        const AppConfig(
          engine: 'codex',
          codexModel: 'gpt-5-codex',
          codexReasoningEffort: 'medium',
        ),
      );
      await controller.connect();
      service.emit(
        AgentStateEvent(
          timestamp: _timestamp,
          sessionId: 'session-1',
          runtimeMeta: const RuntimeMeta(
            command: 'claude',
            engine: 'claude',
            claudeLifecycle: 'waiting_input',
          ),
          raw: const {'type': 'agent_state'},
          state: 'WAIT_INPUT',
          message: '等待输入',
          awaitInput: true,
          command: 'claude',
        ),
      );
      await _flushEvents();

      await controller.updateAiModelSelection(
        model: 'gpt-5.5',
        reasoningEffort: 'high',
      );

      expect(controller.config.claudeModel, isEmpty);
      expect(controller.config.codexModel, 'gpt-5.5');
      expect(controller.config.codexReasoningEffort, 'high');
      expect(controller.commandBarModelSummary, 'gpt-5.5 · HIGH');
    });

    test('显式 engine 保存 Codex 模型会切换配置引擎并保留 Claude 模型', () async {
      final service = _FakeMobileVcWsService();
      final controller = SessionController(service: service);
      await controller.initialize();
      addTearDown(controller.disposeController);

      await controller.saveConfig(
        const AppConfig(
          engine: 'claude',
          claudeModel: 'sonnet',
          codexReasoningEffort: 'medium',
        ),
      );
      await controller.connect();

      await controller.updateAiModelSelection(
        model: 'gpt-5.5',
        reasoningEffort: 'high',
        engine: 'codex',
      );

      expect(controller.config.engine, 'codex');
      expect(controller.config.claudeModel, 'sonnet');
      expect(controller.config.codexModel, 'gpt-5.5');
      expect(controller.config.codexReasoningEffort, 'high');
      expect(controller.commandBarModelSummary, 'gpt-5.5 · HIGH');
    });

    test('Codex Default 模型跟随 config.toml 且不下发推理强度覆盖', () async {
      final service = _FakeMobileVcWsService();
      final controller = SessionController(service: service);
      await controller.initialize();
      addTearDown(controller.disposeController);

      await controller.saveConfig(
        const AppConfig(
          engine: 'codex',
          codexModel: 'gpt-5.5',
          codexReasoningEffort: 'high',
        ),
      );
      await controller.connect();

      await controller.updateAiModelSelection(
        model: '',
        reasoningEffort: '',
        engine: 'codex',
      );

      expect(controller.config.codexModel, isEmpty);
      expect(controller.config.codexReasoningEffort, isEmpty);
      expect(controller.commandBarModelSummary, 'Default · config.toml');

      service.sentPayloads.clear();
      controller.sendInputText('codex');

      expect(service.sentPayloads.single['action'], 'ai_turn');
      expect(service.sentPayloads.single['engine'], 'codex');
      expect(service.sentPayloads.single['model'], 'default');
      expect(
        service.sentPayloads.single.containsKey('reasoningEffort'),
        isFalse,
      );
    });

    test('Codex 启动时不会把残留的 Claude 模型配置发给后端', () async {
      final service = _FakeMobileVcWsService();
      final controller = SessionController(service: service);
      await controller.initialize();
      addTearDown(controller.disposeController);

      await controller.saveConfig(
        const AppConfig(
          engine: 'codex',
          model: 'opus',
          reasoningEffort: 'high',
        ),
      );
      await controller.connect();
      service.sentPayloads.clear();

      controller.sendInputText('codex');

      expect(service.sentPayloads, hasLength(1));
      expect(service.sentPayloads[0]['action'], 'ai_turn');
      expect(service.sentPayloads[0]['engine'], 'codex');
      expect(service.sentPayloads[0]['reasoningEffort'], 'high');
      expect(service.sentPayloads[0]['model'], 'default');
    });

    test('输入 codex 后等待后端 runtime meta 确认模式', () async {
      final service = _FakeMobileVcWsService();
      final controller = SessionController(service: service);
      await controller.initialize();
      addTearDown(controller.disposeController);

      await controller.saveConfig(
        const AppConfig(
          engine: 'claude',
          model: 'sonnet',
        ),
      );
      await controller.connect();
      service.sentPayloads.clear();

      controller.sendInputText('codex');

      expect(controller.commandBarEngine, 'shell');
      expect(service.sentPayloads, hasLength(1));
      expect(service.sentPayloads[0]['action'], 'ai_turn');
      expect(service.sentPayloads[0]['engine'], 'codex');
      expect(service.sentPayloads[0]['model'], 'default');
      expect(service.sentPayloads[0].containsKey('reasoningEffort'), isFalse);
    });

    test('runtime_info /model 结果会自动回填 Claude 模型配置', () async {
      final service = _FakeMobileVcWsService();
      final controller = SessionController(service: service);
      await controller.initialize();
      addTearDown(controller.disposeController);
      await controller.connect();

      service.emit(
        RuntimeInfoResultEvent(
          timestamp: _timestamp,
          sessionId: 'session-1',
          runtimeMeta: const RuntimeMeta(command: 'claude', engine: 'claude'),
          raw: const {'type': 'runtime_info_result'},
          query: 'model',
          items: const [
            RuntimeInfoItem(
              label: 'active_ai',
              value: 'opus',
              available: true,
            ),
          ],
        ),
      );
      await _flushEvents();

      expect(controller.config.claudeModel, 'opus');
    });

    test('runtime_info /model 结果会自动回填 Codex 模型与强度配置', () async {
      final service = _FakeMobileVcWsService();
      final controller = SessionController(service: service);
      await controller.initialize();
      addTearDown(controller.disposeController);
      await controller.connect();

      service.emit(
        RuntimeInfoResultEvent(
          timestamp: _timestamp,
          sessionId: 'session-1',
          runtimeMeta: const RuntimeMeta(command: 'codex', engine: 'codex'),
          raw: const {'type': 'runtime_info_result'},
          query: 'model',
          items: const [
            RuntimeInfoItem(
              label: 'active_ai',
              value: 'gpt-5.4 · HIGH',
              available: true,
            ),
          ],
        ),
      );
      await _flushEvents();

      expect(controller.config.codexModel, 'gpt-5.4');
      expect(controller.config.codexReasoningEffort, 'high');
    });

    test('runtime_info /model 会保留 Codex xhigh 推理强度', () async {
      final service = _FakeMobileVcWsService();
      final controller = SessionController(service: service);
      await controller.initialize();
      addTearDown(controller.disposeController);
      await controller.connect();

      service.emit(
        RuntimeInfoResultEvent(
          timestamp: _timestamp,
          sessionId: 'session-1',
          runtimeMeta: const RuntimeMeta(command: 'codex', engine: 'codex'),
          raw: const {'type': 'runtime_info_result'},
          query: 'model',
          items: const [
            RuntimeInfoItem(
              label: 'active_ai',
              value: 'gpt-5.4 · XHIGH',
              available: true,
            ),
          ],
        ),
      );
      await _flushEvents();

      expect(controller.config.codexModel, 'gpt-5.4');
      expect(controller.config.codexReasoningEffort, 'xhigh');
      expect(controller.commandBarModelSummary, 'GPT-5.4 · XHIGH');
    });

    test('请求 Codex 原生模型目录会发送 codex_models 查询', () async {
      final service = _FakeMobileVcWsService();
      final controller = SessionController(service: service);
      await controller.initialize();
      addTearDown(controller.disposeController);
      await controller.connect();
      service.sentPayloads.clear();

      controller.requestCodexModelCatalog();

      expect(controller.codexModelCatalogLoading, isTrue);
      expect(service.sentPayloads, hasLength(1));
      expect(service.sentPayloads.first['action'], 'runtime_info');
      expect(service.sentPayloads.first['query'], 'codex_models');
    });

    test('请求 Voice API 配置候选会发送 voice_api_configs 查询', () async {
      final service = _FakeMobileVcWsService();
      final controller = SessionController(service: service);
      await controller.initialize();
      addTearDown(controller.disposeController);
      await controller.connect();
      service.sentPayloads.clear();

      controller.requestVoiceApiConfigCandidates();

      expect(controller.voiceApiConfigLoading, isTrue);
      expect(service.sentPayloads, hasLength(1));
      expect(service.sentPayloads.first['action'], 'runtime_info');
      expect(service.sentPayloads.first['query'], 'voice_api_configs');
    });

    test('codex_models 结果会填充动态 Codex 模型目录且不覆盖普通 runtime info', () async {
      final service = _FakeMobileVcWsService();
      final controller = SessionController(service: service);
      await controller.initialize();
      addTearDown(controller.disposeController);
      await controller.connect();

      service.emit(
        RuntimeInfoResultEvent(
          timestamp: _timestamp,
          sessionId: 'session-1',
          runtimeMeta: const RuntimeMeta(command: 'codex', engine: 'codex'),
          raw: const {'type': 'runtime_info_result'},
          query: 'context',
          items: const [
            RuntimeInfoItem(
              label: 'cwd',
              value: '.',
              available: true,
            ),
          ],
        ),
      );
      await _flushEvents();
      expect(controller.runtimeInfo?.query, 'context');

      service.emit(
        RuntimeInfoResultEvent(
          timestamp: _timestamp,
          sessionId: 'session-1',
          runtimeMeta: const RuntimeMeta(command: 'codex', engine: 'codex'),
          raw: const {'type': 'runtime_info_result'},
          query: 'codex_models',
          message: '已同步 1 个 Codex 原生模型，可用于 Flutter 侧动态选择。',
          items: const [
            RuntimeInfoItem(
              label: 'gpt-5.4',
              value: 'GPT-5.4',
              status: 'default',
              available: true,
              detail: '旗舰推理模型',
              meta: {
                'id': 'model-1',
                'model': 'gpt-5.4',
                'displayName': 'GPT-5.4',
                'description': '旗舰推理模型',
                'defaultReasoningEffort': 'high',
                'supportedReasoningEfforts': [
                  'minimal',
                  'low',
                  'medium',
                  'high',
                  'xhigh',
                ],
                'reasoningEffortOptions': [
                  {
                    'reasoningEffort': 'minimal',
                    'description': '最轻',
                  },
                  {
                    'reasoningEffort': 'low',
                    'description': '较快',
                  },
                  {
                    'reasoningEffort': 'medium',
                    'description': '平衡',
                  },
                  {
                    'reasoningEffort': 'high',
                    'description': '深入',
                  },
                  {
                    'reasoningEffort': 'xhigh',
                    'description': '最强',
                  },
                ],
                'isDefault': true,
                'hidden': false,
              },
            ),
          ],
        ),
      );
      await _flushEvents();

      expect(controller.runtimeInfo?.query, 'context');
      expect(controller.codexModelCatalogLoading, isFalse);
      expect(controller.codexModelCatalogMessage, contains('已同步 1 个'));
      expect(controller.codexModelCatalog, hasLength(1));
      expect(controller.codexModelCatalog.first.model, 'gpt-5.4');
      expect(controller.codexModelDisplayLabel('gpt-5.4'), 'GPT-5.4');
      expect(
        controller
            .codexReasoningEffortOptionsForModel('gpt-5.4')
            .map((item) => item.reasoningEffort)
            .toList(),
        <String>['minimal', 'low', 'medium', 'high', 'xhigh'],
      );
      expect(
        controller.preferredCodexReasoningEffortForModel(
          'gpt-5.4',
          fallback: 'xhigh',
        ),
        'xhigh',
      );
      expect(
        controller.preferredCodexReasoningEffortForModel(
          '',
          fallback: '',
        ),
        '',
      );
      await controller.saveConfig(
        controller.config.copyWith(engine: 'codex'),
      );
      expect(controller.configuredAiModel, isEmpty);
      expect(controller.configuredAiReasoningEffort, isEmpty);
      expect(controller.commandBarModelSummary, 'Default · config.toml');
    });

    test('voice_api_configs 结果会填充候选项且不覆盖普通 runtime info', () async {
      final service = _FakeMobileVcWsService();
      final controller = SessionController(service: service);
      await controller.initialize();
      addTearDown(controller.disposeController);
      await controller.connect();

      service.emit(
        RuntimeInfoResultEvent(
          timestamp: _timestamp,
          sessionId: 'session-1',
          runtimeMeta: const RuntimeMeta(command: 'codex', engine: 'codex'),
          raw: const {'type': 'runtime_info_result'},
          query: 'context',
          items: const [
            RuntimeInfoItem(
              label: 'cwd',
              value: '.',
              available: true,
            ),
          ],
        ),
      );
      await _flushEvents();
      expect(controller.runtimeInfo?.query, 'context');

      service.emit(
        RuntimeInfoResultEvent(
          timestamp: _timestamp,
          sessionId: 'session-1',
          runtimeMeta: const RuntimeMeta(command: 'codex', engine: 'codex'),
          raw: const {'type': 'runtime_info_result'},
          query: 'voice_api_configs',
          message: '已读取 1 个可同步 Voice API 配置。',
          items: const [
            RuntimeInfoItem(
              label: 'Codex',
              value: 'gpt-5.5 · responses',
              status: 'ready',
              available: true,
              detail: '来自 ~/.codex/config.toml 和 ~/.codex/auth.json',
              meta: {
                'provider': 'codex',
                'apiUrl': 'https://api.example.test/v1/responses',
                'apiKey': 'codex-key',
                'modelName': 'gpt-5.5',
                'endpointType': 'responses',
              },
            ),
          ],
        ),
      );
      await _flushEvents();

      expect(controller.voiceApiConfigLoading, isFalse);
      expect(controller.voiceApiConfigMessage, '已读取 1 个可同步 Voice API 配置。');
      expect(controller.runtimeInfo?.query, 'context');
      expect(controller.voiceApiConfigCandidates, hasLength(1));
      final candidate = controller.voiceApiConfigCandidates.single;
      expect(candidate.provider, 'codex');
      expect(candidate.apiUrl, 'https://api.example.test/v1/responses');
      expect(candidate.apiKey, 'codex-key');
      expect(candidate.modelName, 'gpt-5.5');
      expect(candidate.hasUsableConfig, isTrue);
    });

    test('手动应用 Codex 配置后不会被旧运行时模型回填覆盖', () async {
      final service = _FakeMobileVcWsService();
      final controller = SessionController(service: service);
      await controller.initialize();
      addTearDown(controller.disposeController);
      await controller.connect();

      await controller.saveConfig(
        controller.config.copyWith(
          engine: 'codex',
          codexModel: 'gpt-5-codex',
          codexReasoningEffort: 'medium',
        ),
      );

      await controller.updateAiModelSelection(
        model: 'gpt-5-codex',
        reasoningEffort: 'high',
      );
      expect(controller.configuredAiModel, 'gpt-5-codex');
      expect(controller.configuredAiReasoningEffort, 'high');

      service.emit(
        RuntimeInfoResultEvent(
          timestamp: _timestamp,
          sessionId: 'session-1',
          runtimeMeta: const RuntimeMeta(command: 'codex', engine: 'codex'),
          raw: const {'type': 'runtime_info_result'},
          query: 'model',
          items: const [
            RuntimeInfoItem(
              label: 'active_ai',
              value: 'gpt-5-codex · MEDIUM',
              available: true,
            ),
          ],
        ),
      );
      await _flushEvents();

      expect(controller.config.codexModel, 'gpt-5-codex');
      expect(controller.config.codexReasoningEffort, 'high');
      expect(controller.configuredAiReasoningEffort, 'high');
      expect(controller.commandBarModelSummary, 'GPT-5-Codex · HIGH');
    });

    test('Claude 与 Codex 模型配置会分别保存，不互相覆盖', () async {
      final seedController =
          SessionController(service: _FakeMobileVcWsService());
      await seedController.initialize();
      addTearDown(seedController.disposeController);
      await seedController.saveConfig(
        const AppConfig(
          engine: 'claude',
          claudeModel: 'opus',
          codexModel: 'gpt-5.4',
          codexReasoningEffort: 'high',
        ),
      );

      final claudeService = _FakeMobileVcWsService();
      final claudeController = SessionController(service: claudeService);
      await claudeController.initialize();
      addTearDown(claudeController.disposeController);
      await claudeController.connect();
      claudeService.sentPayloads.clear();
      claudeController.sendInputText('claude');

      expect(claudeService.sentPayloads[0]['action'], 'ai_turn');
      expect(claudeService.sentPayloads[0]['engine'], 'claude');
      expect(claudeService.sentPayloads[0]['model'], 'opus');

      final codexService = _FakeMobileVcWsService();
      final codexController = SessionController(service: codexService);
      await codexController.initialize();
      addTearDown(codexController.disposeController);
      await codexController.saveConfig(
        codexController.config.copyWith(engine: 'codex'),
      );
      await codexController.connect();
      codexService.sentPayloads.clear();
      codexController.sendInputText('codex');

      expect(codexService.sentPayloads[0]['action'], 'ai_turn');
      expect(codexService.sentPayloads[0]['engine'], 'codex');
      expect(codexService.sentPayloads[0]['model'], 'gpt-5.4');
      expect(codexService.sentPayloads[0]['reasoningEffort'], 'high');
    });

    test('Codex xhigh 配置会带入启动命令', () async {
      final service = _FakeMobileVcWsService();
      final controller = SessionController(service: service);
      await controller.initialize();
      addTearDown(controller.disposeController);
      await controller.saveConfig(
        controller.config.copyWith(
          engine: 'codex',
          codexModel: 'gpt-5.4',
          codexReasoningEffort: 'xhigh',
        ),
      );

      await controller.connect();
      service.sentPayloads.clear();
      controller.sendInputText('codex');

      expect(service.sentPayloads[0]['action'], 'ai_turn');
      expect(service.sentPayloads[0]['engine'], 'codex');
      expect(service.sentPayloads[0]['model'], 'gpt-5.4');
      expect(service.sentPayloads[0]['reasoningEffort'], 'xhigh');
      expect(controller.commandBarModelSummary, 'GPT-5.4 · XHIGH');
    });

    test('Codex 沙箱配置会带入 ai_turn payload', () async {
      final service = _FakeMobileVcWsService();
      final controller = SessionController(service: service);
      await controller.initialize();
      addTearDown(controller.disposeController);
      await controller.saveConfig(
        controller.config.copyWith(
          engine: 'codex',
          codexSandboxMode: 'danger-full-access',
          permissionMode: 'config',
        ),
      );

      await controller.connect();
      service.sentPayloads.clear();
      controller.sendInputText('codex');

      expect(service.sentPayloads[0]['action'], 'ai_turn');
      expect(service.sentPayloads[0]['engine'], 'codex');
      expect(service.sentPayloads[0]['permissionMode'], 'config');
      expect(
        service.sentPayloads[0]['codexSandboxMode'],
        'danger-full-access',
      );
    });

    test('Codex 目标模式关闭时不带目标上下文', () async {
      final service = _FakeMobileVcWsService();
      final controller = SessionController(service: service);
      await controller.initialize();
      addTearDown(controller.disposeController);
      await controller.saveConfig(
        controller.config.copyWith(
          engine: 'codex',
          codexTargetMode: false,
        ),
      );

      await controller.connect();
      service.emit(
        FileDiffEvent(
          timestamp: _timestamp,
          sessionId: 'session-1',
          runtimeMeta: const RuntimeMeta(
            command: 'codex',
            engine: 'codex',
            targetPath: '/workspace/lib/main.dart',
          ),
          raw: const {'type': 'file_diff'},
          path: '/workspace/lib/main.dart',
          title: 'main.dart',
          diff: '@@ -1 +1 @@\n-old\n+new',
          lang: 'dart',
        ),
      );
      await _flushEvents();

      service.sentPayloads.clear();
      controller.sendInputText('codex fix it');

      expect(service.sentPayloads[0]['action'], 'ai_turn');
      expect(service.sentPayloads[0]['engine'], 'codex');
      expect(service.sentPayloads[0].containsKey('targetPath'), isFalse);
      expect(service.sentPayloads[0].containsKey('targetDiff'), isFalse);
    });

    test('Codex 目标模式开启时带目标上下文', () async {
      final service = _FakeMobileVcWsService();
      final controller = SessionController(service: service);
      await controller.initialize();
      addTearDown(controller.disposeController);
      await controller.saveConfig(
        controller.config.copyWith(
          engine: 'codex',
          codexTargetMode: true,
        ),
      );

      await controller.connect();
      service.emit(
        FileDiffEvent(
          timestamp: _timestamp,
          sessionId: 'session-1',
          runtimeMeta: const RuntimeMeta(command: 'codex', engine: 'codex'),
          raw: const {'type': 'file_diff'},
          path: '/workspace/lib/main.dart',
          title: 'main.dart',
          diff: '@@ -1 +1 @@\n-old\n+new',
          lang: 'dart',
        ),
      );
      await _flushEvents();

      service.sentPayloads.clear();
      controller.sendInputText('codex fix it');

      expect(service.sentPayloads[0]['targetPath'], '/workspace/lib/main.dart');
      expect(service.sentPayloads[0]['targetDiff'], contains('-old'));
    });

    test('sendInputText 在 Claude 模式下继续普通文本时走 ai_turn', () async {
      final service = _FakeMobileVcWsService();
      final controller = SessionController(service: service);
      await controller.initialize();
      addTearDown(controller.disposeController);

      await controller.connect();
      service.emit(
        AgentStateEvent(
          timestamp: _timestamp,
          sessionId: 'session-1',
          runtimeMeta: const RuntimeMeta(
            command: 'claude',
            executionId: 'exec-keep',
            claudeLifecycle: 'active',
          ),
          raw: const {'type': 'agent_state'},
          state: 'IDLE',
          message: 'ready',
          command: 'claude',
        ),
      );
      service.emit(
        SessionStateEvent(
          timestamp: _timestamp,
          sessionId: 'session-1',
          runtimeMeta: const RuntimeMeta(
            command: 'claude',
            executionId: 'exec-keep',
            claudeLifecycle: 'active',
          ),
          raw: const {'type': 'session_state'},
          state: 'ACTIVE',
          message: 'command started',
        ),
      );
      await _flushEvents();
      service.sentPayloads.clear();

      controller.sendInputText('继续处理这个问题');

      expect(service.sentPayloads, hasLength(1));
      expect(service.sentPayloads[0]['action'], 'ai_turn');
      expect(service.sentPayloads[0]['data'], '继续处理这个问题\n');
    });

    test('Claude 空启动后首条正文要等待后端进入输入态', () async {
      final service = _FakeMobileVcWsService();
      final controller = SessionController(service: service);
      await controller.initialize();
      addTearDown(controller.disposeController);

      await controller.connect();
      service.sentPayloads.clear();

      controller.sendInputText('claude');
      expect(service.sentPayloads, hasLength(1));
      expect(service.sentPayloads[0]['action'], 'ai_turn');

      service.emit(
        AgentStateEvent(
          timestamp: _timestamp,
          sessionId: 'session-1',
          runtimeMeta: const RuntimeMeta(
            command: 'claude --model sonnet',
            executionId: 'exec-pending',
            claudeLifecycle: 'starting',
          ),
          raw: const {'type': 'agent_state'},
          state: 'THINKING',
          message: '思考中',
          command: 'claude --model sonnet',
        ),
      );
      await _flushEvents();

      controller.sendInputText('继续处理');

      expect(service.sentPayloads, hasLength(1));
    });

    test('发送 claude 文本会走 slash 命令启动，不发送空 input', () async {
      final service = _FakeMobileVcWsService();
      final controller = SessionController(service: service);
      await controller.initialize();
      addTearDown(controller.disposeController);

      await controller.connect();
      service.sentPayloads.clear();

      controller.sendInputText('/claude');

      expect(service.sentPayloads, hasLength(1));
      expect(service.sentPayloads[0]['action'], 'slash_command');
      expect(service.sentPayloads[0]['command'], '/claude');
    });

    test('continueWithCurrentFile 在 Claude 会话中走后端 ai_turn continuation',
        () async {
      final service = _FakeMobileVcWsService();
      final controller = SessionController(service: service);
      await controller.initialize();
      addTearDown(controller.disposeController);

      await controller.connect();
      service.emit(
        AgentStateEvent(
          timestamp: _timestamp,
          sessionId: 'session-1',
          runtimeMeta: const RuntimeMeta(
            command: 'claude',
            executionId: 'exec-file',
            claudeLifecycle: 'active',
          ),
          raw: const {'type': 'agent_state'},
          state: 'IDLE',
          message: 'ready',
          command: 'claude',
        ),
      );
      service.emit(
        FSReadResultEvent(
          timestamp: _timestamp,
          sessionId: 'session-1',
          runtimeMeta: const RuntimeMeta(command: 'claude'),
          raw: const {'type': 'fs_read_result'},
          result: const FileReadResult(
            path: '/workspace/lib/main.dart',
            content: 'void main() {}',
            isText: true,
          ),
        ),
      );
      await _flushEvents();
      service.sentPayloads.clear();

      controller.continueWithCurrentFile('基于当前文件继续处理');

      expect(service.sentPayloads, hasLength(1));
      expect(service.sentPayloads[0]['action'], 'ai_turn');
      expect(
        (service.sentPayloads[0]['data'] as String)
            .contains('TargetPath: /workspace/lib/main.dart'),
        isTrue,
      );
    });

    test('等待权限确认时不会显示顶部运行态', () async {
      final service = _FakeMobileVcWsService();
      final controller = SessionController(service: service);
      await controller.initialize();
      addTearDown(controller.disposeController);

      await controller.connect();
      service.emit(
        AgentStateEvent(
          timestamp: _timestamp,
          sessionId: 'session-1',
          runtimeMeta:
              const RuntimeMeta(command: 'claude', executionId: 'exec-1'),
          raw: const {'type': 'agent_state'},
          state: 'THINKING',
          message: '思考中',
          command: 'claude',
        ),
      );
      await _flushEvents();
      expect(controller.activityVisible, isTrue);

      service.emit(
        PromptRequestEvent(
          timestamp: _timestamp.add(const Duration(seconds: 1)),
          sessionId: 'session-1',
          runtimeMeta:
              const RuntimeMeta(command: 'claude', contextId: 'perm-1'),
          raw: const {'type': 'prompt_request', 'msg': 'Allow edit a.dart?'},
          message: 'Allow edit a.dart?',
          options: const [PromptOption(value: 'y'), PromptOption(value: 'n')],
        ),
      );
      await _flushEvents();

      expect(controller.awaitInput, isTrue);
      expect(controller.activityVisible, isFalse);
    });

    test('收到 Claude 回复后等待后端 idle 状态退出运行态', () async {
      final service = _FakeMobileVcWsService();
      final controller = SessionController(service: service);
      await controller.initialize();
      addTearDown(controller.disposeController);

      await controller.connect();
      service.emit(
        SessionStateEvent(
          timestamp: _timestamp,
          sessionId: 'session-1',
          runtimeMeta:
              const RuntimeMeta(command: 'claude', executionId: 'exec-1'),
          raw: const {'type': 'session_state'},
          state: 'RUNNING',
          message: 'claude running',
        ),
      );
      service.emit(
        AgentStateEvent(
          timestamp: _timestamp,
          sessionId: 'session-1',
          runtimeMeta:
              const RuntimeMeta(command: 'claude', executionId: 'exec-1'),
          raw: const {'type': 'agent_state'},
          state: 'THINKING',
          message: '思考中',
          command: 'claude',
        ),
      );
      await _flushEvents();
      expect(controller.activityVisible, isTrue);
      expect(controller.isSessionBusy, isTrue);

      service.emit(
        LogEvent(
          timestamp: _timestamp.add(const Duration(seconds: 1)),
          sessionId: 'session-1',
          runtimeMeta:
              const RuntimeMeta(command: 'claude', executionId: 'exec-1'),
          raw: const {'type': 'log'},
          message: '你好，我是 Claude，由 Anthropic 开发。有什么我可以帮你处理的吗？',
          stream: 'stdout',
        ),
      );
      service.emit(
        AgentStateEvent(
          timestamp: _timestamp.add(const Duration(seconds: 1)),
          sessionId: 'session-1',
          runtimeMeta: const RuntimeMeta(
              command: 'claude',
              engine: 'claude',
              executionId: 'exec-1',
              claudeLifecycle: 'settled'),
          raw: const {'type': 'agent_state'},
          state: 'IDLE',
          message: '完成',
          command: 'claude',
        ),
      );
      await _flushEvents();

      expect(controller.activityVisible, isTrue);
      expect(controller.isSessionBusy, isTrue);
    });

    test('收到 Claude 最终回复后未有后端 idle 前保持运行态', () async {
      final service = _FakeMobileVcWsService();
      final controller = SessionController(service: service);
      await controller.initialize();
      addTearDown(controller.disposeController);

      await controller.connect();
      service.emit(
        AgentStateEvent(
          timestamp: _timestamp,
          sessionId: 'session-1',
          runtimeMeta:
              const RuntimeMeta(command: 'claude', executionId: 'exec-live-1'),
          raw: const {'type': 'agent_state'},
          state: 'THINKING',
          message: '思考中',
          command: 'claude',
        ),
      );
      service.emit(
        SessionStateEvent(
          timestamp: _timestamp,
          sessionId: 'session-1',
          runtimeMeta:
              const RuntimeMeta(command: 'claude', executionId: 'exec-live-1'),
          raw: const {'type': 'session_state'},
          state: 'RUNNING',
          message: 'claude running',
        ),
      );
      await _flushEvents();

      expect(controller.activityVisible, isTrue);
      expect(controller.isSessionBusy, isTrue);
      expect(controller.canStopCurrentRun, isTrue);

      service.emit(
        LogEvent(
          timestamp: _timestamp.add(const Duration(seconds: 1)),
          sessionId: 'session-1',
          runtimeMeta:
              const RuntimeMeta(command: 'claude', executionId: 'exec-live-1'),
          raw: const {'type': 'log'},
          message: '你好，有什么我可以帮你处理的？',
          stream: 'stdout',
        ),
      );
      await _flushEvents();

      expect(controller.activityVisible, isTrue);
      expect(controller.isSessionBusy, isTrue);
      expect(controller.canStopCurrentRun, isTrue);
    });

    test('执行中收到 assistant 文本日志时不会错误闪回空闲', () async {
      final service = _FakeMobileVcWsService();
      final controller = SessionController(service: service);
      await controller.initialize();
      addTearDown(controller.disposeController);

      await controller.connect();
      service.emit(
        AgentStateEvent(
          timestamp: _timestamp,
          sessionId: 'session-1',
          runtimeMeta:
              const RuntimeMeta(command: 'claude', executionId: 'exec-keep-1'),
          raw: const {'type': 'agent_state'},
          state: 'RUNNING_TOOL',
          message: '正在执行工具',
          command: 'claude',
          tool: 'edit_file',
        ),
      );
      await _flushEvents();

      expect(controller.agentState?.state, 'RUNNING_TOOL');
      expect(controller.agentPhaseLabel, '执行中');
      expect(controller.activityVisible, isTrue);
      expect(controller.isSessionBusy, isTrue);

      service.emit(
        LogEvent(
          timestamp: _timestamp.add(const Duration(seconds: 1)),
          sessionId: 'session-1',
          runtimeMeta:
              const RuntimeMeta(command: 'claude', executionId: 'exec-keep-1'),
          raw: const {'type': 'log'},
          message: '我先整理一下当前修改点，然后继续处理剩余步骤。',
          stream: 'stdout',
        ),
      );
      await _flushEvents();

      expect(controller.agentState?.state, 'RUNNING_TOOL');
      expect(controller.agentPhaseLabel, '执行中');
      expect(controller.activityVisible, isTrue);
      expect(controller.isSessionBusy, isTrue);
    });

    test('codex 未 settled 的 WAIT_INPUT 期间仍保持顶部运行态动画与耗时', () async {
      final service = _FakeMobileVcWsService();
      final controller = SessionController(service: service);
      await controller.initialize();
      addTearDown(controller.disposeController);

      await controller.connect();
      service.emit(
        SessionStateEvent(
          timestamp: _timestamp,
          sessionId: 'session-1',
          runtimeMeta: const RuntimeMeta(
              command: 'codex', executionId: 'exec-codex-busy-1'),
          raw: const {'type': 'session_state'},
          state: 'RUNNING',
          message: 'codex running',
        ),
      );
      service.emit(
        AgentStateEvent(
          timestamp: _timestamp,
          sessionId: 'session-1',
          runtimeMeta: const RuntimeMeta(
              command: 'codex', executionId: 'exec-codex-busy-1'),
          raw: const {'type': 'agent_state'},
          state: 'THINKING',
          message: '处理中',
          command: 'codex',
        ),
      );
      await _flushEvents();

      expect(controller.isSessionBusy, isTrue);
      expect(controller.activityVisible, isTrue);
      expect(controller.activityBannerVisible, isTrue);
      expect(controller.activityBannerAnimated, isTrue);
      expect(controller.activityBannerShowsElapsed, isTrue);
      expect(controller.activityBannerTitle, '处理中');

      service.emit(
        AgentStateEvent(
          timestamp: _timestamp.add(const Duration(seconds: 1)),
          sessionId: 'session-1',
          runtimeMeta: const RuntimeMeta(
              command: 'codex', executionId: 'exec-codex-busy-1'),
          raw: const {'type': 'agent_state'},
          state: 'WAIT_INPUT',
          message: '等待继续输入',
          awaitInput: true,
          command: 'codex',
        ),
      );
      await _flushEvents();

      expect(controller.awaitInput, isTrue);
      expect(controller.isSessionBusy, isFalse);
      expect(controller.activityVisible, isFalse);
      expect(controller.activityBannerVisible, isFalse);
      expect(controller.activityBannerAnimated, isFalse);
      expect(controller.activityBannerShowsElapsed, isFalse);
      expect(controller.activityBannerTitle, '待输入');

      service.emit(
        LogEvent(
          timestamp: _timestamp.add(const Duration(seconds: 2)),
          sessionId: 'session-1',
          runtimeMeta: const RuntimeMeta(
              command: 'codex', executionId: 'exec-codex-busy-1'),
          raw: const {'type': 'log'},
          message: '处理完成，我已经修好了。',
          stream: 'stdout',
        ),
      );
      await _flushEvents();

      expect(controller.awaitInput, isTrue);
      expect(controller.isSessionBusy, isFalse);
      expect(controller.activityVisible, isFalse);
      expect(controller.activityBannerAnimated, isFalse);
      expect(controller.activityBannerShowsElapsed, isFalse);
    });

    test('codex 等待权限确认时不会继续显示顶部运行态', () async {
      final service = _FakeMobileVcWsService();
      final controller = SessionController(service: service);
      await controller.initialize();
      addTearDown(controller.disposeController);

      await controller.connect();
      service.emit(
        SessionStateEvent(
          timestamp: _timestamp,
          sessionId: 'session-1',
          runtimeMeta: const RuntimeMeta(
              command: 'codex', executionId: 'exec-codex-perm-1'),
          raw: const {'type': 'session_state'},
          state: 'RUNNING',
          message: 'codex running',
        ),
      );
      service.emit(
        AgentStateEvent(
          timestamp: _timestamp,
          sessionId: 'session-1',
          runtimeMeta: const RuntimeMeta(
              command: 'codex', executionId: 'exec-codex-perm-1'),
          raw: const {'type': 'agent_state'},
          state: 'THINKING',
          message: '处理中',
          command: 'codex',
        ),
      );
      await _flushEvents();

      expect(controller.activityVisible, isTrue);

      service.emit(
        PromptRequestEvent(
          timestamp: _timestamp.add(const Duration(seconds: 1)),
          sessionId: 'session-1',
          runtimeMeta:
              const RuntimeMeta(command: 'codex', contextId: 'perm-codex-1'),
          raw: const {'type': 'prompt_request', 'msg': 'Allow edit a.dart?'},
          message: 'Allow edit a.dart?',
          options: const [PromptOption(value: 'y'), PromptOption(value: 'n')],
        ),
      );
      await _flushEvents();

      expect(controller.awaitInput, isTrue);
      expect(controller.isSessionBusy, isFalse);
      expect(controller.activityVisible, isFalse);
      expect(controller.activityBannerAnimated, isFalse);
      expect(controller.activityBannerShowsElapsed, isFalse);
    });

    test('仅有 RUNNING session state 时等待后端状态确认退出运行态', () async {
      final service = _FakeMobileVcWsService();
      final controller = SessionController(service: service);
      await controller.initialize();
      addTearDown(controller.disposeController);

      await controller.connect();
      service.emit(
        SessionStateEvent(
          timestamp: _timestamp,
          sessionId: 'session-1',
          runtimeMeta:
              const RuntimeMeta(command: 'codex', executionId: 'exec-run-1'),
          raw: const {'type': 'session_state'},
          state: 'RUNNING',
          message: 'codex running',
        ),
      );
      await _flushEvents();

      expect(controller.isSessionBusy, isTrue);

      service.emit(
        LogEvent(
          timestamp: _timestamp.add(const Duration(seconds: 1)),
          sessionId: 'session-1',
          runtimeMeta:
              const RuntimeMeta(command: 'codex', executionId: 'exec-run-1'),
          raw: const {'type': 'log'},
          message: '处理完成，我已经修好了。',
          stream: 'stdout',
        ),
      );
      await _flushEvents();

      expect(controller.isSessionBusy, isTrue);
      expect(
        controller.timeline.any((item) => item.body.contains('处理完成')),
        isTrue,
      );
    });

    test('运行中点击 stop 会发送 stop action', () async {
      final service = _FakeMobileVcWsService();
      final controller = SessionController(service: service);
      await controller.initialize();
      addTearDown(controller.disposeController);

      await controller.connect();
      service.emit(
        SessionStateEvent(
          timestamp: _timestamp,
          sessionId: 'session-1',
          runtimeMeta:
              const RuntimeMeta(command: 'codex', executionId: 'exec-stop-1'),
          raw: const {'type': 'session_state'},
          state: 'RUNNING',
          message: 'codex running',
        ),
      );
      await _flushEvents();

      expect(controller.canStopCurrentRun, isTrue);

      controller.stopCurrentRun();

      expect(service.sentPayloads, isNotEmpty);
      expect(service.sentPayloads.last['action'], 'stop');
    });

    test('恢复历史只带 runtimeAlive 时也允许停止 MobileVC 管理的运行', () async {
      final service = _FakeMobileVcWsService();
      final controller = SessionController(service: service);
      await controller.initialize();
      addTearDown(controller.disposeController);

      await controller.connect();
      service.emit(
        SessionHistoryEvent(
          timestamp: _timestamp,
          sessionId: 'session-restored-running',
          runtimeMeta: const RuntimeMeta(),
          raw: const {'type': 'session_history'},
          summary: const SessionSummary(
            id: 'session-restored-running',
            title: 'Restored',
            ownership: 'mobilevc',
            executionActive: true,
            runtime: RuntimeMeta(command: 'codex', engine: 'codex'),
          ),
          runtimeAlive: true,
          resumeRuntimeMeta: const RuntimeMeta(),
        ),
      );
      await _flushEvents();

      expect(controller.isSessionReadOnly, isFalse);
      expect(controller.canStopCurrentRun, isTrue);

      service.sentPayloads.clear();
      controller.stopCurrentRun();

      expect(service.sentPayloads, hasLength(1));
      expect(service.sentPayloads.single['action'], 'stop');
      expect(service.sentPayloads.single['clientActionId'], isNotEmpty);
      expect(
          service.sentPayloads.single['sessionId'], 'session-restored-running');
    });

    test('外部原生观察态只有 runtimeAlive 时不会误显示停止', () async {
      final service = _FakeMobileVcWsService();
      final controller = SessionController(service: service);
      await controller.initialize();
      addTearDown(controller.disposeController);

      await controller.connect();
      service.emit(
        SessionHistoryEvent(
          timestamp: _timestamp,
          sessionId: 'codex-thread:running',
          runtimeMeta: const RuntimeMeta(),
          raw: const {'type': 'session_history'},
          summary: const SessionSummary(
            id: 'codex-thread:running',
            title: 'Native Codex',
            source: 'codex-native',
            external: true,
            executionActive: true,
            runtime: RuntimeMeta(command: 'codex', engine: 'codex'),
          ),
          runtimeAlive: true,
          resumeRuntimeMeta:
              const RuntimeMeta(command: 'codex', engine: 'codex'),
        ),
      );
      await _flushEvents();

      expect(controller.isSessionReadOnly, isTrue);
      expect(controller.canStopCurrentRun, isFalse);
    });

    test('Codex 等待输入且只有 runtimeAlive 时选图会发送 input 而不是显示停止', () async {
      final service = _FakeMobileVcWsService();
      final controller = SessionController(service: service);
      await controller.initialize();
      addTearDown(controller.disposeController);

      await controller.connect();
      service.emit(
        SessionHistoryEvent(
          timestamp: _timestamp,
          sessionId: 'session-codex-idle',
          runtimeMeta: const RuntimeMeta(),
          raw: const {'type': 'session_history'},
          summary: const SessionSummary(
            id: 'session-codex-idle',
            title: 'Codex idle',
            ownership: 'mobilevc',
            runtime: RuntimeMeta(command: 'codex', engine: 'codex'),
          ),
          runtimeAlive: true,
          resumeRuntimeMeta: const RuntimeMeta(
            command: 'codex',
            engine: 'codex',
            claudeLifecycle: 'waiting_input',
          ),
        ),
      );
      service.emit(
        AgentStateEvent(
          timestamp: _timestamp,
          sessionId: 'session-codex-idle',
          runtimeMeta: const RuntimeMeta(
            command: 'codex',
            engine: 'codex',
            claudeLifecycle: 'waiting_input',
          ),
          raw: const {'type': 'agent_state'},
          state: 'WAIT_INPUT',
          message: '等待输入',
          awaitInput: true,
          command: 'codex',
        ),
      );
      await _flushEvents();

      expect(controller.awaitInput, isTrue);
      expect(controller.isSessionBusy, isFalse);
      expect(controller.canStopCurrentRun, isFalse);

      service.sentPayloads.clear();
      controller.stopCurrentRun();
      expect(service.sentPayloads, isEmpty);

      controller.sendInputTextWithImages(
        '看下这张图',
        [
          ChatImageAttachment(
            name: 'screen.png',
            mimeType: 'image/png',
            bytes: utf8.encode('png-bytes'),
          ),
        ],
      );

      expect(service.sentPayloads, hasLength(1));
      final payload = service.sentPayloads.single;
      expect(payload['action'], 'input');
      expect(payload['data'], '看下这张图\n');
      expect(payload['permissionMode'], 'auto');
      expect(payload['codexSandboxMode'], 'workspace-write');
      expect(payload.containsKey('imageAttachments'), isTrue);
      expect(controller.canStopCurrentRun, isTrue);
    });

    test('外部原生 history 落地后立即启动观察增量同步', () async {
      final service = _FakeMobileVcWsService();
      final controller = SessionController(service: service);
      await controller.initialize();
      addTearDown(controller.disposeController);

      await controller.connect();
      service.sentPayloads.clear();
      service.emit(
        SessionHistoryEvent(
          timestamp: _timestamp,
          sessionId: 'codex-thread:observe',
          runtimeMeta: const RuntimeMeta(),
          raw: const {'type': 'session_history'},
          summary: const SessionSummary(
            id: 'codex-thread:observe',
            title: 'Native Codex',
            source: 'codex-native',
            external: true,
            executionActive: true,
            runtime: RuntimeMeta(command: 'codex', engine: 'codex'),
          ),
          runtimeAlive: true,
          resumeRuntimeMeta:
              const RuntimeMeta(command: 'codex', engine: 'codex'),
        ),
      );
      await _flushEvents();

      expect(
        service.sentPayloads.any((payload) =>
            payload['action'] == 'session_delta_get' &&
            payload['sessionId'] == 'codex-thread:observe' &&
            payload['reason'] == 'observe_active_session_start'),
        isTrue,
      );
    });

    test('观察模式继续 Codex 会话时带入当前配置权限', () async {
      final service = _FakeMobileVcWsService();
      final controller = SessionController(service: service);
      await controller.initialize();
      addTearDown(controller.disposeController);
      await controller.saveConfig(
        controller.config.copyWith(
          engine: 'codex',
          codexSandboxMode: 'danger-full-access',
          permissionMode: 'config',
        ),
      );

      await controller.connect();
      service.emit(
        SessionHistoryEvent(
          timestamp: _timestamp,
          sessionId: 'codex-thread:observe',
          runtimeMeta: const RuntimeMeta(),
          raw: const {'type': 'session_history'},
          summary: const SessionSummary(
            id: 'codex-thread:observe',
            title: 'Native Codex',
            source: 'codex-native',
            external: true,
            executionActive: true,
            runtime: RuntimeMeta(command: 'codex', engine: 'codex'),
          ),
          runtimeAlive: true,
          resumeRuntimeMeta: const RuntimeMeta(),
        ),
      );
      await _flushEvents();

      service.sentPayloads.clear();
      controller.continueSameSessionFromPhone();

      final resumes = service.sentPayloads
          .where((payload) => payload['action'] == 'session_resume')
          .toList();
      expect(resumes, hasLength(1));
      expect(resumes.single['reason'], 'continue_same_session');
      expect(resumes.single['engine'], 'codex');
      expect(resumes.single['codexSandboxMode'], 'danger-full-access');
      expect(resumes.single['permissionMode'], 'config');
    });

    test('Codex thread id 恢复时即使 runtime 为空也带入当前配置权限', () async {
      final service = _FakeMobileVcWsService();
      final controller = SessionController(service: service);
      await controller.initialize();
      addTearDown(controller.disposeController);
      await controller.saveConfig(
        controller.config.copyWith(
          engine: 'claude',
          codexSandboxMode: 'danger-full-access',
          permissionMode: 'bypassPermissions',
        ),
      );

      await controller.connect();
      service.emit(
        SessionHistoryEvent(
          timestamp: _timestamp,
          sessionId: 'codex-thread:early',
          runtimeMeta: const RuntimeMeta(),
          raw: const {'type': 'session_history'},
          summary: const SessionSummary(
            id: 'codex-thread:early',
            title: 'Codex thread',
          ),
          resumeRuntimeMeta: const RuntimeMeta(),
        ),
      );
      await _flushEvents();

      service.sentPayloads.clear();
      controller.resumeConnectionIfNeeded();

      final resumes = service.sentPayloads
          .where((payload) => payload['action'] == 'session_resume')
          .toList();
      expect(resumes, hasLength(1));
      expect(resumes.single['engine'], 'codex');
      expect(resumes.single['codexSandboxMode'], 'danger-full-access');
      expect(resumes.single['permissionMode'], 'bypassPermissions');
    });

    test('前台恢复空 runtime 元信息时不会按当前配置误带 Codex 沙箱', () async {
      final service = _FakeMobileVcWsService();
      final controller = SessionController(service: service);
      await controller.initialize();
      addTearDown(controller.disposeController);
      await controller.saveConfig(
        controller.config.copyWith(
          engine: 'codex',
          codexSandboxMode: 'danger-full-access',
        ),
      );

      await controller.connect();
      service.emit(
        SessionHistoryEvent(
          timestamp: _timestamp,
          sessionId: 'session-unknown-runtime',
          runtimeMeta: const RuntimeMeta(),
          raw: const {'type': 'session_history'},
          summary: const SessionSummary(
            id: 'session-unknown-runtime',
            title: 'Unknown runtime',
          ),
          resumeRuntimeMeta: const RuntimeMeta(),
        ),
      );
      await _flushEvents();

      service.sentPayloads.clear();
      controller.resumeConnectionIfNeeded();

      final resumes = service.sentPayloads
          .where((payload) => payload['action'] == 'session_resume')
          .toList();
      expect(resumes, hasLength(1));
      expect(resumes.single.containsKey('codexSandboxMode'), isFalse);
      expect(resumes.single.containsKey('engine'), isFalse);
      expect(resumes.single.containsKey('permissionMode'), isFalse);
    });

    test('summary executionActive=false 会清掉陈旧 stop 锁存', () async {
      final service = _FakeMobileVcWsService();
      final controller = SessionController(service: service);
      await controller.initialize();
      addTearDown(controller.disposeController);

      await controller.connect();
      service.emit(
        SessionHistoryEvent(
          timestamp: _timestamp,
          sessionId: 'session-latched',
          runtimeMeta: const RuntimeMeta(),
          raw: const {'type': 'session_history'},
          summary: const SessionSummary(
            id: 'session-latched',
            title: 'Latched',
            ownership: 'mobilevc',
            executionActive: true,
            runtime: RuntimeMeta(command: 'codex', engine: 'codex'),
          ),
          runtimeAlive: true,
          resumeRuntimeMeta:
              const RuntimeMeta(command: 'codex', engine: 'codex'),
        ),
      );
      await _flushEvents();
      expect(controller.canStopCurrentRun, isTrue);

      service.emit(
        SessionDeltaEvent(
          timestamp: _timestamp.add(const Duration(seconds: 1)),
          sessionId: 'session-latched',
          runtimeMeta: const RuntimeMeta(),
          raw: const {'type': 'session_delta'},
          summary: const SessionSummary(
            id: 'session-latched',
            title: 'Latched',
            ownership: 'mobilevc',
            executionActive: false,
            runtime: RuntimeMeta(command: 'codex', engine: 'codex'),
          ),
          runtimeAlive: false,
          resumeRuntimeMeta:
              const RuntimeMeta(command: 'codex', engine: 'codex'),
        ),
      );
      await _flushEvents();

      expect(controller.canStopCurrentRun, isFalse);
    });

    test('idle task snapshot 会清掉 executionActive stop 状态', () async {
      final service = _FakeMobileVcWsService();
      final controller = SessionController(service: service);
      await controller.initialize();
      addTearDown(controller.disposeController);

      await controller.connect();
      service.emit(
        SessionHistoryEvent(
          timestamp: _timestamp,
          sessionId: 'session-snapshot-idle',
          runtimeMeta: const RuntimeMeta(),
          raw: const {'type': 'session_history'},
          summary: const SessionSummary(
            id: 'session-snapshot-idle',
            title: 'Snapshot idle',
            ownership: 'mobilevc',
            executionActive: true,
            runtime: RuntimeMeta(command: 'codex', engine: 'codex'),
          ),
          runtimeAlive: true,
          resumeRuntimeMeta:
              const RuntimeMeta(command: 'codex', engine: 'codex'),
        ),
      );
      await _flushEvents();
      expect(controller.canStopCurrentRun, isTrue);

      service.emit(
        TaskSnapshotEvent(
          timestamp: _timestamp.add(const Duration(seconds: 1)),
          sessionId: 'session-snapshot-idle',
          runtimeMeta: const RuntimeMeta(command: 'codex', engine: 'codex'),
          raw: const {'type': 'task_snapshot'},
          state: 'IDLE',
          message: 'Task idle',
          runtimeAlive: false,
        ),
      );
      await _flushEvents();

      expect(controller.canStopCurrentRun, isFalse);
    });

    test('空的 history/delta context usage 不覆盖已有 Claude 用量', () async {
      final service = _FakeMobileVcWsService();
      final controller = SessionController(service: service);
      await controller.initialize();
      addTearDown(controller.disposeController);

      await controller.connect();
      service.emit(
        SessionHistoryEvent(
          timestamp: _timestamp,
          sessionId: 'session-context-usage',
          runtimeMeta: const RuntimeMeta(),
          raw: const {'type': 'session_history'},
          summary:
              const SessionSummary(id: 'session-context-usage', title: 'Usage'),
          resumeRuntimeMeta:
              const RuntimeMeta(command: 'claude', engine: 'claude'),
        ),
      );
      service.emit(
        ContextWindowUsageEvent(
          timestamp: _timestamp.add(const Duration(milliseconds: 1)),
          sessionId: 'session-context-usage',
          runtimeMeta: const RuntimeMeta(command: 'claude', engine: 'claude'),
          raw: const {'type': 'context_window_usage'},
          usage: const ContextWindowUsage(
            tokensUsed: 40000,
            tokenLimit: 200000,
          ),
        ),
      );
      await _flushEvents();

      expect(controller.contextWindowUsage.tokensUsed, 40000);
      expect(controller.contextWindowUsage.tokenLimit, 200000);

      service.emit(
        SessionDeltaEvent(
          timestamp: _timestamp.add(const Duration(milliseconds: 2)),
          sessionId: 'session-context-usage',
          runtimeMeta: const RuntimeMeta(),
          raw: const {'type': 'session_delta'},
          summary:
              const SessionSummary(id: 'session-context-usage', title: 'Usage'),
          resumeRuntimeMeta:
              const RuntimeMeta(command: 'claude', engine: 'claude'),
        ),
      );
      await _flushEvents();

      expect(controller.contextWindowUsage.tokensUsed, 40000);
      expect(controller.contextWindowUsage.tokenLimit, 200000);
    });

    test('切换会话时会清掉上一条 Claude context usage', () async {
      final service = _FakeMobileVcWsService();
      final controller = SessionController(service: service);
      await controller.initialize();
      addTearDown(controller.disposeController);

      await controller.connect();
      service.emit(
        SessionHistoryEvent(
          timestamp: _timestamp,
          sessionId: 'session-context-source',
          runtimeMeta: const RuntimeMeta(),
          raw: const {'type': 'session_history'},
          summary: const SessionSummary(
            id: 'session-context-source',
            title: 'Source',
          ),
          resumeRuntimeMeta:
              const RuntimeMeta(command: 'claude', engine: 'claude'),
        ),
      );
      service.emit(
        ContextWindowUsageEvent(
          timestamp: _timestamp.add(const Duration(milliseconds: 1)),
          sessionId: 'session-context-source',
          runtimeMeta: const RuntimeMeta(command: 'claude', engine: 'claude'),
          raw: const {'type': 'context_window_usage'},
          usage: const ContextWindowUsage(
            tokensUsed: 40000,
            tokenLimit: 200000,
          ),
        ),
      );
      await _flushEvents();

      expect(controller.contextWindowUsage.isAvailable, isTrue);

      controller.loadSession('session-context-target');

      expect(controller.contextWindowUsage.isAvailable, isFalse);

      service.emit(
        SessionHistoryEvent(
          timestamp: _timestamp.add(const Duration(milliseconds: 2)),
          sessionId: 'session-context-target',
          runtimeMeta: const RuntimeMeta(),
          raw: const {'type': 'session_history'},
          summary: const SessionSummary(
            id: 'session-context-target',
            title: 'Target',
          ),
          resumeRuntimeMeta:
              const RuntimeMeta(command: 'claude', engine: 'claude'),
        ),
      );
      await _flushEvents();

      expect(controller.contextWindowUsage.isAvailable, isFalse);
    });

    test('提交后未收到运行态时点击 stop 也会发送 stop action', () async {
      final service = _FakeMobileVcWsService();
      final controller = SessionController(service: service);
      await controller.initialize();
      addTearDown(controller.disposeController);

      await controller.connect();
      service.sentPayloads.clear();

      controller.sendInputText('codex 你好');
      expect(service.sentPayloads.single['action'], 'ai_turn');

      controller.stopCurrentRun();

      expect(service.sentPayloads.last['action'], 'stop');
      expect(controller.activityBannerTitle, '正在停止');
    });

    test('Codex failed session state 会解除 busy 和 stop 状态', () async {
      final service = _FakeMobileVcWsService();
      final controller = SessionController(service: service);
      await controller.initialize();
      addTearDown(controller.disposeController);

      await controller.connect();
      service.sentPayloads.clear();

      controller.sendInputText('codex 你好');
      controller.stopCurrentRun();
      expect(controller.activityBannerTitle, '正在停止');

      service.emit(
        SessionStateEvent(
          timestamp: _timestamp,
          sessionId: 'session-1',
          runtimeMeta: const RuntimeMeta(command: 'codex', engine: 'codex'),
          raw: const {'type': 'session_state'},
          state: 'FAILED',
          message: 'unexpected status 503 Service Unavailable',
        ),
      );
      await _flushEvents();

      expect(controller.isSessionBusy, isFalse);
      expect(controller.canStopCurrentRun, isFalse);
      expect(controller.activityBannerVisible, isFalse);
    });

    test('stop 后收到 idle task snapshot 也会退出停止中', () async {
      final service = _FakeMobileVcWsService();
      final controller = SessionController(service: service);
      await controller.initialize();
      addTearDown(controller.disposeController);

      await controller.connect();
      service.emit(
        SessionHistoryEvent(
          timestamp: _timestamp,
          sessionId: 'session-1',
          runtimeMeta: const RuntimeMeta(),
          raw: const {'type': 'session_history'},
          summary: const SessionSummary(id: 'session-1', title: 'Stop target'),
        ),
      );
      service.emit(
        SessionStateEvent(
          timestamp: _timestamp,
          sessionId: 'session-1',
          runtimeMeta:
              const RuntimeMeta(command: 'codex', executionId: 'exec-stop-2'),
          raw: const {'type': 'session_state'},
          state: 'RUNNING',
          message: 'codex running',
        ),
      );
      await _flushEvents();

      controller.stopCurrentRun();
      expect(controller.activityBannerTitle, '正在停止');

      service.emit(
        TaskSnapshotEvent(
          timestamp: _timestamp.add(const Duration(seconds: 1)),
          sessionId: 'session-1',
          runtimeMeta:
              const RuntimeMeta(command: 'codex', executionId: 'exec-stop-2'),
          raw: const {'type': 'task_snapshot'},
          state: 'IDLE',
          message: 'heartbeat idle',
          runtimeAlive: false,
        ),
      );
      await _flushEvents();

      expect(controller.activityBannerVisible, isFalse);
      expect(controller.canStopCurrentRun, isFalse);
    });

    test('运行中点击 stop 后只发送停止请求并显示停止中', () async {
      final service = _FakeMobileVcWsService();
      final controller = SessionController(service: service);
      await controller.initialize();
      addTearDown(controller.disposeController);

      await controller.connect();
      service.sentPayloads.clear();

      controller.sendInputText('claude');
      expect(service.sentPayloads, hasLength(1));
      expect(controller.activityBannerVisible, isFalse);

      service.emit(
        SessionStateEvent(
          timestamp: _timestamp,
          sessionId: 'session-1',
          runtimeMeta: const RuntimeMeta(
              command: 'claude', executionId: 'exec-stop-claude'),
          raw: const {'type': 'session_state'},
          state: 'RUNNING',
          message: 'claude running',
        ),
      );
      await _flushEvents();

      expect(controller.canStopCurrentRun, isTrue);

      controller.stopCurrentRun();

      expect(service.sentPayloads.last['action'], 'stop');
      expect(controller.activityBannerTitle, '正在停止');
    });

    test('stop Claude 后会退出 AI 模式，后续 ls 重新走 shell exec', () async {
      final service = _FakeMobileVcWsService();
      final controller = SessionController(service: service);
      await controller.initialize();
      addTearDown(controller.disposeController);

      await controller.connect();
      service.sentPayloads.clear();

      controller.sendInputText('claude');
      expect(service.sentPayloads, hasLength(1));
      expect(service.sentPayloads.single['action'], 'ai_turn');

      service.emit(
        SessionStateEvent(
          timestamp: _timestamp,
          sessionId: 'session-1',
          runtimeMeta: const RuntimeMeta(
            command: 'claude --model sonnet',
            engine: 'claude',
            claudeLifecycle: 'starting',
            executionId: 'exec-stop-shell-1',
          ),
          raw: const {'type': 'session_state'},
          state: 'RUNNING',
          message: 'claude running',
        ),
      );
      await _flushEvents();

      expect(controller.shouldShowClaudeMode, isTrue);
      expect(controller.canStopCurrentRun, isTrue);

      controller.stopCurrentRun();

      expect(service.sentPayloads.last['action'], 'stop');
      expect(controller.shouldShowClaudeMode, isFalse);
      expect(controller.awaitInput, isFalse);
      expect(controller.canResumeCurrentSession, isFalse);

      service.sentPayloads.clear();
      controller.sendInputText('ls');

      expect(service.sentPayloads, hasLength(1));
      expect(service.sentPayloads.single['action'], 'exec');
      expect(service.sentPayloads.single['cmd'], 'ls');
    });

    test('stop Claude 后会继续接收后续 AI 状态与 prompt', () async {
      final service = _FakeMobileVcWsService();
      final controller = SessionController(service: service);
      await controller.initialize();
      addTearDown(controller.disposeController);

      await controller.connect();
      service.sentPayloads.clear();

      controller.sendInputText('claude');
      service.emit(
        SessionStateEvent(
          timestamp: _timestamp,
          sessionId: 'session-1',
          runtimeMeta: const RuntimeMeta(
            command: 'claude --model sonnet',
            engine: 'claude',
            claudeLifecycle: 'starting',
            executionId: 'exec-stop-stale-1',
          ),
          raw: const {'type': 'session_state'},
          state: 'RUNNING',
          message: 'claude running',
        ),
      );
      await _flushEvents();

      controller.stopCurrentRun();
      expect(controller.shouldShowClaudeMode, isFalse);

      service.emit(
        PromptRequestEvent(
          timestamp: _timestamp.add(const Duration(seconds: 1)),
          sessionId: 'session-1',
          runtimeMeta: const RuntimeMeta(
            command: 'claude --model sonnet',
            engine: 'claude',
            claudeLifecycle: 'waiting_input',
          ),
          raw: const {'type': 'prompt_request'},
          message: 'AI 会话已就绪，可继续输入',
        ),
      );
      service.emit(
        AgentStateEvent(
          timestamp: _timestamp.add(const Duration(seconds: 2)),
          sessionId: 'session-1',
          runtimeMeta: const RuntimeMeta(
            command: 'claude --model sonnet',
            engine: 'claude',
            claudeLifecycle: 'waiting_input',
          ),
          raw: const {'type': 'agent_state'},
          state: 'WAIT_INPUT',
          message: '等待输入',
          awaitInput: true,
          command: 'claude --model sonnet',
        ),
      );
      await _flushEvents();

      expect(controller.shouldShowClaudeMode, isTrue);
      expect(controller.awaitInput, isTrue);
    });

    test('有待审核 diff 但没有 review prompt 时仍允许显示 differ 审核按钮', () async {
      final service = _FakeMobileVcWsService();
      final controller = SessionController(service: service);
      await controller.initialize();
      addTearDown(controller.disposeController);

      await controller.connect();
      service.emit(
        SessionHistoryEvent(
          timestamp: _timestamp,
          sessionId: 'session-1',
          runtimeMeta: const RuntimeMeta(),
          raw: const {'type': 'session_history'},
          summary:
              const SessionSummary(id: 'session-1', title: 'review session'),
          diffs: const [
            HistoryContext(
              id: 'diff-1',
              type: 'diff',
              path: '/workspace/lib/main.dart',
              title: 'main.dart',
              diff: '@@ -1 +1 @@\n-old\n+new',
              pendingReview: true,
              executionId: 'exec-review-1',
              groupId: 'group-review-1',
              groupTitle: '本轮修改',
            ),
          ],
          reviewGroups: const [
            ReviewGroup(
              id: 'group-review-1',
              title: '本轮修改',
              executionId: 'exec-review-1',
              pendingReview: true,
              pendingCount: 1,
              files: [
                ReviewFile(
                  id: 'diff-1',
                  path: '/workspace/lib/main.dart',
                  title: 'main.dart',
                  pendingReview: true,
                  executionId: 'exec-review-1',
                ),
              ],
            ),
          ],
        ),
      );
      await _flushEvents();

      expect(controller.shouldShowReviewChoices, isTrue);
      expect(controller.currentReviewDiff?.id, 'diff-1');
      expect(controller.reviewActionTargetDiff?.id, 'diff-1');
      expect(controller.canShowReviewActions, isTrue);
    });

    test('恢复态里从 differ 同意后不会继续显示需要同意', () async {
      final service = _FakeMobileVcWsService();
      final controller = SessionController(service: service);
      await controller.initialize();
      addTearDown(controller.disposeController);

      await controller.connect();
      service.emit(
        SessionHistoryEvent(
          timestamp: _timestamp,
          sessionId: 'session-1',
          runtimeMeta: const RuntimeMeta(),
          raw: const {'type': 'session_history'},
          summary:
              const SessionSummary(id: 'session-1', title: 'review session'),
          diffs: const [
            HistoryContext(
              id: 'diff-restore-1',
              type: 'diff',
              path: '/workspace/lib/main.dart',
              title: 'main.dart',
              diff: '@@ -1 +1 @@\n-old\n+new',
              pendingReview: true,
              executionId: 'exec-restore-1',
              groupId: 'group-restore-1',
              groupTitle: '本轮修改',
            ),
          ],
          reviewGroups: const [
            ReviewGroup(
              id: 'group-restore-1',
              title: '本轮修改',
              executionId: 'exec-restore-1',
              pendingReview: true,
              pendingCount: 1,
              files: [
                ReviewFile(
                  id: 'diff-restore-1',
                  path: '/workspace/lib/main.dart',
                  title: 'main.dart',
                  pendingReview: true,
                  executionId: 'exec-restore-1',
                ),
              ],
            ),
          ],
        ),
      );
      await _flushEvents();

      expect(controller.canShowReviewActions, isTrue);
      expect(controller.pendingDiffs, hasLength(1));
      service.sentPayloads.clear();

      controller.sendReviewDecision('accept');

      expect(service.sentPayloads, hasLength(1));
      expect(service.sentPayloads.last['action'], 'review_decision');
      expect(service.sentPayloads.last['decision'], 'accept');
      expect(controller.pendingDiffs, isEmpty);
      expect(controller.currentReviewDiff, isNull);
      expect(controller.reviewActionTargetDiff, isNull);
      expect(controller.canShowReviewActions, isFalse);
      expect(controller.shouldShowReviewChoices, isFalse);
    });

    test('连续三轮 Claude 交互后不会因 session idle 遗留运行态', () async {
      final service = _FakeMobileVcWsService();
      final controller = SessionController(service: service);
      await controller.initialize();
      addTearDown(controller.disposeController);

      await controller.connect();

      service.emit(
        AgentStateEvent(
          timestamp: _timestamp,
          sessionId: 'session-1',
          runtimeMeta:
              const RuntimeMeta(command: 'claude', executionId: 'exec-1'),
          raw: const {'type': 'agent_state'},
          state: 'THINKING',
          message: '第一轮思考中',
          command: 'claude',
        ),
      );
      service.emit(
        SessionStateEvent(
          timestamp: _timestamp,
          sessionId: 'session-1',
          runtimeMeta:
              const RuntimeMeta(command: 'claude', executionId: 'exec-1'),
          raw: const {'type': 'session_state'},
          state: 'RUNNING',
          message: '第一轮运行中',
        ),
      );
      await _flushEvents();
      expect(controller.activityVisible, isTrue);

      service.emit(
        AgentStateEvent(
          timestamp: _timestamp.add(const Duration(seconds: 1)),
          sessionId: 'session-1',
          runtimeMeta:
              const RuntimeMeta(command: 'claude', executionId: 'exec-1'),
          raw: const {'type': 'agent_state'},
          state: 'WAIT_INPUT',
          message: '等待输入',
          awaitInput: true,
          command: 'claude',
        ),
      );
      await _flushEvents();
      expect(controller.awaitInput, isTrue);
      expect(controller.activityVisible, isFalse);

      controller.sendInputText('继续第二轮');
      expect(service.sentPayloads, isNotEmpty);
      expect(service.sentPayloads.last['action'], 'input');
      service.sentPayloads.clear();

      service.emit(
        AgentStateEvent(
          timestamp: _timestamp.add(const Duration(seconds: 2)),
          sessionId: 'session-1',
          runtimeMeta:
              const RuntimeMeta(command: 'claude', executionId: 'exec-2'),
          raw: const {'type': 'agent_state'},
          state: 'THINKING',
          message: '第二轮思考中',
          command: 'claude',
        ),
      );
      service.emit(
        SessionStateEvent(
          timestamp: _timestamp.add(const Duration(seconds: 2)),
          sessionId: 'session-1',
          runtimeMeta:
              const RuntimeMeta(command: 'claude', executionId: 'exec-2'),
          raw: const {'type': 'session_state'},
          state: 'RUNNING',
          message: '第二轮运行中',
        ),
      );
      await _flushEvents();
      expect(controller.activityVisible, isTrue);

      service.emit(
        AgentStateEvent(
          timestamp: _timestamp.add(const Duration(seconds: 3)),
          sessionId: 'session-1',
          runtimeMeta:
              const RuntimeMeta(command: 'claude', executionId: 'exec-2'),
          raw: const {'type': 'agent_state'},
          state: 'WAIT_INPUT',
          message: '等待输入',
          awaitInput: true,
          command: 'claude',
        ),
      );
      await _flushEvents();
      expect(controller.awaitInput, isTrue);
      expect(controller.activityVisible, isFalse);

      controller.sendInputText('继续第三轮');
      expect(service.sentPayloads, isNotEmpty);
      expect(service.sentPayloads.last['action'], 'input');
      service.sentPayloads.clear();

      service.emit(
        AgentStateEvent(
          timestamp: _timestamp.add(const Duration(seconds: 4)),
          sessionId: 'session-1',
          runtimeMeta:
              const RuntimeMeta(command: 'claude', executionId: 'exec-3'),
          raw: const {'type': 'agent_state'},
          state: 'THINKING',
          message: '第三轮思考中',
          command: 'claude',
        ),
      );
      service.emit(
        SessionStateEvent(
          timestamp: _timestamp.add(const Duration(seconds: 4)),
          sessionId: 'session-1',
          runtimeMeta:
              const RuntimeMeta(command: 'claude', executionId: 'exec-3'),
          raw: const {'type': 'session_state'},
          state: 'RUNNING',
          message: '第三轮运行中',
        ),
      );
      await _flushEvents();
      expect(controller.activityVisible, isTrue);
      expect(controller.isSessionBusy, isTrue);

      service.emit(
        AgentStateEvent(
          timestamp: _timestamp.add(const Duration(seconds: 5)),
          sessionId: 'session-1',
          runtimeMeta:
              const RuntimeMeta(command: 'claude', executionId: 'exec-3'),
          raw: const {'type': 'agent_state'},
          state: 'WAIT_INPUT',
          message: '等待输入',
          awaitInput: true,
          command: 'claude',
        ),
      );
      service.emit(
        LogEvent(
          timestamp: _timestamp.add(const Duration(seconds: 5)),
          sessionId: 'session-1',
          runtimeMeta:
              const RuntimeMeta(command: 'claude', executionId: 'exec-3'),
          raw: const {'type': 'log'},
          message: '第三轮回复完成，继续吧。',
          stream: 'stdout',
        ),
      );
      await _flushEvents();

      expect(controller.awaitInput, isTrue);
      expect(controller.pendingPrompt, isNull);
      expect(controller.activityVisible, isFalse);
      expect(controller.isSessionBusy, isFalse);
    });

    test('codex 会话在残留 busy 状态下仍允许继续输入', () async {
      final service = _FakeMobileVcWsService();
      final controller = SessionController(service: service);
      await controller.initialize();
      addTearDown(controller.disposeController);

      await controller.connect();
      service.emit(
        SessionStateEvent(
          timestamp: _timestamp,
          sessionId: 'session-1',
          runtimeMeta: const RuntimeMeta(
            command: 'codex',
            executionId: 'exec-codex-1',
            claudeLifecycle: 'resumable',
          ),
          raw: const {'type': 'session_state'},
          state: 'RUNNING',
          message: 'codex still rendering',
        ),
      );
      service.emit(
        AgentStateEvent(
          timestamp: _timestamp,
          sessionId: 'session-1',
          runtimeMeta: const RuntimeMeta(
            command: 'codex',
            executionId: 'exec-codex-1',
            claudeLifecycle: 'resumable',
          ),
          raw: const {'type': 'agent_state'},
          state: 'THINKING',
          message: '处理中',
          command: 'codex',
        ),
      );
      await _flushEvents();

      expect(controller.isSessionBusy, isTrue);

      controller.sendInputText('继续');

      expect(service.sentPayloads, isNotEmpty);
      expect(service.sentPayloads.last['action'], 'ai_turn');
      expect(service.sentPayloads.last['data'], '继续\n');
    });
  });

  group('SessionController session loading and mode', () {
    test('loadSession 发起后立即进入 loading，并阻断旧等待态输入', () async {
      final service = _FakeMobileVcWsService();
      final controller = SessionController(service: service);
      await controller.initialize();
      addTearDown(controller.disposeController);

      service.emit(
        PromptRequestEvent(
          timestamp: _timestamp,
          sessionId: 'session-old',
          runtimeMeta: const RuntimeMeta(command: 'claude'),
          raw: const {'type': 'prompt_request', 'msg': '请输入补充说明'},
          message: '请输入补充说明',
          options: const [],
        ),
      );
      await _flushEvents();
      expect(controller.awaitInput, isTrue);

      controller.loadSession('session-new');

      expect(controller.isLoadingSession, isTrue);
      expect(controller.awaitInput, isFalse);
      expect(controller.isSessionBusy, isTrue);
      expect(controller.pendingPrompt, isNull);
      expect(service.sentPayloads.single['action'], 'session_load');

      service.sentPayloads.clear();
      controller.sendInputText('不该发送');
      controller.continueWithCurrentFile('不该继续');
      controller.submitPromptOption('不该提交');
      expect(service.sentPayloads, isEmpty);
    });

    test('收到目标 SessionHistoryEvent 后退出 loading', () async {
      final service = _FakeMobileVcWsService();
      final controller = SessionController(service: service);
      await controller.initialize();
      addTearDown(controller.disposeController);

      controller.loadSession('session-new');
      expect(controller.isLoadingSession, isTrue);

      service.emit(
        SessionHistoryEvent(
          timestamp: _timestamp,
          sessionId: 'session-new',
          runtimeMeta: const RuntimeMeta(command: 'claude'),
          raw: const {'type': 'session_history'},
          summary: const SessionSummary(id: 'session-new', title: '新会话'),
          resumeRuntimeMeta: const RuntimeMeta(
              command: 'claude', claudeLifecycle: 'resumable'),
        ),
      );
      await _flushEvents();

      expect(controller.isLoadingSession, isFalse);
      expect(controller.selectedSessionId, 'session-new');
    });

    test('恢复历史会话时，历史 markdown 卡片默认关闭打字机动画', () async {
      final service = _FakeMobileVcWsService();
      final controller = SessionController(service: service);
      await controller.initialize();
      addTearDown(controller.disposeController);

      service.emit(
        SessionHistoryEvent(
          timestamp: _timestamp,
          sessionId: 'session-new',
          runtimeMeta: const RuntimeMeta(command: 'claude'),
          raw: const {'type': 'session_history'},
          summary: const SessionSummary(id: 'session-new', title: '新会话'),
          logEntries: const [
            HistoryLogEntry(
              kind: 'markdown',
              message: '这是恢复出来的历史回复',
              timestamp: '2026-01-01T00:00:00Z',
            ),
          ],
          resumeRuntimeMeta: const RuntimeMeta(command: 'claude'),
        ),
      );
      await _flushEvents();

      expect(controller.timeline, hasLength(1));
      expect(controller.timeline.single.kind, 'markdown');
      expect(controller.timeline.single.animateBody, isFalse);
    });

    test('恢复历史会话时会重新合并 codex 回复分片', () async {
      final service = _FakeMobileVcWsService();
      final controller = SessionController(service: service);
      await controller.initialize();
      addTearDown(controller.disposeController);

      service.emit(
        SessionHistoryEvent(
          timestamp: _timestamp,
          sessionId: 'session-history-merge',
          runtimeMeta: const RuntimeMeta(command: 'codex', engine: 'codex'),
          raw: const {'type': 'session_history'},
          summary: const SessionSummary(
            id: 'session-history-merge',
            title: '历史会话',
          ),
          logEntries: const [
            HistoryLogEntry(
              kind: 'markdown',
              message:
                  '- ADB 调试：[mobile_vc/lib/features/adb/adb_debug_page.dart]',
              timestamp: '2026-01-01T00:00:00Z',
              stream: 'stdout',
              executionId: 'exec-history-1',
            ),
            HistoryLogEntry(
              kind: 'terminal',
              message:
                  '(/Users/wust_lh/MobileVc/mobile_vc/lib/features/adb/adb_debug_page.dart)',
              text:
                  '(/Users/wust_lh/MobileVc/mobile_vc/lib/features/adb/adb_debug_page.dart)',
              timestamp: '2026-01-01T00:00:01Z',
              stream: 'stdout',
              executionId: 'exec-history-1',
            ),
          ],
          resumeRuntimeMeta: const RuntimeMeta(
            command: 'codex',
            engine: 'codex',
          ),
        ),
      );
      await _flushEvents();

      expect(controller.timeline, hasLength(1));
      expect(controller.timeline.single.kind, 'markdown');
      expect(
        controller.timeline.single.body,
        '- ADB 调试：[mobile_vc/lib/features/adb/adb_debug_page.dart](/Users/wust_lh/MobileVc/mobile_vc/lib/features/adb/adb_debug_page.dart)',
      );
      expect(controller.timeline.single.animateBody, isFalse);
    });

    test('同一 contextId 的 thinking 事件会更新同一条 timeline', () async {
      final service = _FakeMobileVcWsService();
      final controller = SessionController(service: service);
      await controller.initialize();
      addTearDown(controller.disposeController);

      const contextId = 'codex-reasoning:turn-1:reasoning-1';
      service.emit(
        ThinkingEvent(
          timestamp: _timestamp,
          sessionId: 'session-thinking-live',
          runtimeMeta: const RuntimeMeta(
            engine: 'codex',
            source: 'codex/reasoning-summary',
            executionId: 'turn-1',
            contextId: contextId,
          ),
          raw: const {'type': 'thinking'},
          content: '正在定位问题',
        ),
      );
      await _flushEvents();
      service.emit(
        ThinkingEvent(
          timestamp: _timestamp.add(const Duration(milliseconds: 1)),
          sessionId: 'session-thinking-live',
          runtimeMeta: const RuntimeMeta(
            engine: 'codex',
            source: 'codex/reasoning-summary',
            executionId: 'turn-1',
            contextId: contextId,
          ),
          raw: const {'type': 'thinking'},
          content: '正在定位问题\n已经找到相关文件',
        ),
      );
      await _flushEvents();

      final thinkingItems =
          controller.timeline.where((item) => item.kind == 'thinking').toList();
      expect(thinkingItems, hasLength(1));
      expect(thinkingItems.single.id, 'thinking-$contextId');
      expect(thinkingItems.single.body, contains('已经找到相关文件'));
      expect(thinkingItems.single.meta.contextId, contextId);
    });

    test('同 timestamp 但不同 contextId 的 live thinking 会保留两条', () async {
      final service = _FakeMobileVcWsService();
      final controller = SessionController(service: service);
      await controller.initialize();
      addTearDown(controller.disposeController);

      service.emit(
        ThinkingEvent(
          timestamp: _timestamp,
          sessionId: 'session-thinking-live-contexts',
          runtimeMeta: const RuntimeMeta(
            engine: 'claude',
            contextId: 'claude-thinking:session-1:1:0:2026-01-01T00:00:00Z',
          ),
          raw: const {'type': 'thinking'},
          content: '第一段思考摘要',
        ),
      );
      await _flushEvents();
      service.emit(
        ThinkingEvent(
          timestamp: _timestamp,
          sessionId: 'session-thinking-live-contexts',
          runtimeMeta: const RuntimeMeta(
            engine: 'claude',
            contextId: 'claude-thinking:session-1:2:1:2026-01-01T00:00:00Z',
          ),
          raw: const {'type': 'thinking'},
          content: '第二段思考摘要',
        ),
      );
      await _flushEvents();

      final thinkingItems =
          controller.timeline.where((item) => item.kind == 'thinking').toList();
      expect(thinkingItems, hasLength(2));
      expect(thinkingItems[0].body, '第一段思考摘要');
      expect(thinkingItems[1].body, '第二段思考摘要');
      expect(thinkingItems[0].id, isNot(thinkingItems[1].id));
      expect(thinkingItems[0].id, startsWith('thinking-claude-thinking:'));
      expect(thinkingItems[1].id, startsWith('thinking-claude-thinking:'));
    });

    test('恢复历史 thinking 会使用 contextId 作为稳定 item id', () async {
      final service = _FakeMobileVcWsService();
      final controller = SessionController(service: service);
      await controller.initialize();
      addTearDown(controller.disposeController);

      const contextId = 'codex-reasoning:turn-2:reasoning-1';
      service.emit(
        SessionHistoryEvent(
          timestamp: _timestamp,
          sessionId: 'session-thinking-history',
          runtimeMeta: const RuntimeMeta(command: 'codex', engine: 'codex'),
          raw: const {'type': 'session_history'},
          summary: const SessionSummary(
            id: 'session-thinking-history',
            title: '历史会话',
          ),
          logEntries: const [
            HistoryLogEntry(
              kind: 'thinking',
              message: '恢复出来的思考摘要',
              timestamp: '2026-01-01T00:00:00Z',
              executionId: 'turn-2',
              context: HistoryContext(
                id: contextId,
                type: 'thinking',
                title: '思考过程',
                message: '恢复出来的思考摘要',
                source: 'codex/reasoning-summary',
                executionId: 'turn-2',
              ),
            ),
          ],
          resumeRuntimeMeta: const RuntimeMeta(
            command: 'codex',
            engine: 'codex',
          ),
        ),
      );
      await _flushEvents();

      expect(controller.timeline, hasLength(1));
      final item = controller.timeline.single;
      expect(item.kind, 'thinking');
      expect(item.id, 'thinking-$contextId');
      expect(item.meta.contextId, contextId);
      expect(item.animateBody, isFalse);
    });

    test('恢复无 context 的同秒 thinking 历史不会合并成一条', () async {
      final service = _FakeMobileVcWsService();
      final controller = SessionController(service: service);
      await controller.initialize();
      addTearDown(controller.disposeController);

      service.emit(
        SessionHistoryEvent(
          timestamp: _timestamp,
          sessionId: 'session-thinking-contextless-history',
          runtimeMeta: const RuntimeMeta(command: 'claude', engine: 'claude'),
          raw: const {'type': 'session_history'},
          summary: const SessionSummary(
            id: 'session-thinking-contextless-history',
            title: '历史会话',
          ),
          logEntries: const [
            HistoryLogEntry(
              kind: 'thinking',
              message: '第一段思考摘要，正在定位历史恢复身份',
              timestamp: '2026-01-01T00:00:00Z',
            ),
            HistoryLogEntry(
              kind: 'thinking',
              message: '第二段思考摘要，正在验证同秒恢复',
              timestamp: '2026-01-01T00:00:00Z',
            ),
          ],
          resumeRuntimeMeta: const RuntimeMeta(
            command: 'claude',
            engine: 'claude',
          ),
        ),
      );
      await _flushEvents();

      final thinkingItems =
          controller.timeline.where((item) => item.kind == 'thinking').toList();
      expect(thinkingItems, hasLength(2));
      expect(thinkingItems[0].body, '第一段思考摘要，正在定位历史恢复身份');
      expect(thinkingItems[1].body, '第二段思考摘要，正在验证同秒恢复');
      expect(thinkingItems[0].id, isNot(thinkingItems[1].id));
      expect(thinkingItems[0].meta.contextId, isEmpty);
      expect(thinkingItems[1].meta.contextId, isEmpty);
      expect(thinkingItems[0].animateBody, isFalse);
      expect(thinkingItems[1].animateBody, isFalse);
    });

    test('恢复态 runtime meta 会直接恢复 AI continuation 模式', () async {
      final service = _FakeMobileVcWsService();
      final controller = SessionController(service: service);
      await controller.initialize();
      addTearDown(controller.disposeController);

      service.emit(
        SessionHistoryEvent(
          timestamp: _timestamp,
          sessionId: 'session-1',
          runtimeMeta: const RuntimeMeta(),
          raw: const {'type': 'session_history'},
          summary: const SessionSummary(id: 'session-1', title: '历史会话'),
          resumeRuntimeMeta: const RuntimeMeta(
            command: 'claude --resume session-1',
            cwd: '/workspace/history',
            claudeLifecycle: 'resumable',
          ),
        ),
      );
      await _flushEvents();

      expect(controller.inClaudeMode, isTrue);
      expect(controller.shouldShowClaudeMode, isTrue);
      expect(controller.effectiveCwd, '/workspace/history');
      expect(controller.currentMeta.cwd, '/workspace/history');
    });

    test('加载可恢复历史会话后，普通输入会走 ai_turn continuation', () async {
      final service = _FakeMobileVcWsService();
      final controller = SessionController(service: service);
      await controller.initialize();
      addTearDown(controller.disposeController);

      service.emit(
        SessionHistoryEvent(
          timestamp: _timestamp,
          sessionId: 'session-1',
          runtimeMeta: const RuntimeMeta(),
          raw: const {'type': 'session_history'},
          summary: const SessionSummary(id: 'session-1', title: '历史会话'),
          resumeRuntimeMeta: const RuntimeMeta(
            command: 'claude --resume session-1',
            cwd: '/workspace/history',
            claudeLifecycle: 'resumable',
          ),
        ),
      );
      await _flushEvents();

      service.sentPayloads.clear();
      controller.sendInputText('hello');

      expect(service.sentPayloads, hasLength(1));
      expect(service.sentPayloads.single['action'], 'ai_turn');
      expect(service.sentPayloads.single['data'], 'hello\n');
    });

    test('恢复态 WAIT_INPUT 续聊后会立刻切到 Codex 处理中', () async {
      final service = _FakeMobileVcWsService();
      final controller = SessionController(service: service);
      await controller.initialize();
      addTearDown(controller.disposeController);

      await controller.connect();

      service.emit(
        SessionHistoryEvent(
          timestamp: _timestamp,
          sessionId: 'session-1',
          runtimeMeta: const RuntimeMeta(),
          raw: const {'type': 'session_history'},
          summary: const SessionSummary(id: 'session-1', title: '历史会话'),
          resumeRuntimeMeta: const RuntimeMeta(
            command: 'codex',
            engine: 'codex',
            cwd: '/workspace/history',
            resumeSessionId: 'thread-restore-1',
            claudeLifecycle: 'waiting_input',
          ),
        ),
      );
      service.emit(
        AgentStateEvent(
          timestamp: _timestamp,
          sessionId: 'session-1',
          runtimeMeta: const RuntimeMeta(
            command: 'codex',
            engine: 'codex',
            cwd: '/workspace/history',
            resumeSessionId: 'thread-restore-1',
            claudeLifecycle: 'waiting_input',
          ),
          raw: const {'type': 'agent_state'},
          state: 'WAIT_INPUT',
          message: '等待输入',
          awaitInput: true,
          command: 'codex',
        ),
      );
      await _flushEvents();

      expect(controller.awaitInput, isTrue);
      expect(controller.isSessionBusy, isFalse);

      service.sentPayloads.clear();
      controller.sendInputText('继续处理');

      expect(service.sentPayloads, hasLength(1));
      expect(service.sentPayloads.single['action'], 'input');
      expect(service.sentPayloads.single['data'], '继续处理\n');
      expect(controller.awaitInput, isFalse);
      expect(controller.isSessionBusy, isTrue);
      expect(controller.canStopCurrentRun, isTrue);
      expect(controller.activityVisible, isTrue);
      expect(controller.activityBannerVisible, isTrue);
      expect(controller.agentState?.state, 'THINKING');
      expect(controller.agentState?.awaitInput, isFalse);
      expect(controller.agentState?.command, 'codex');
      expect(controller.agentPhaseLabel, '思考中');
    });

    test('同 executionId 的 WAIT_INPUT 续聊会清空已 settled 状态并恢复 stop/busy/banner',
        () async {
      final service = _FakeMobileVcWsService();
      final controller = SessionController(service: service);
      await controller.initialize();
      addTearDown(controller.disposeController);

      await controller.connect();

      service.emit(
        SessionStateEvent(
          timestamp: _timestamp,
          sessionId: 'session-1',
          runtimeMeta: const RuntimeMeta(
            command: 'claude',
            engine: 'claude',
            executionId: 'exec-same-1',
            claudeLifecycle: 'active',
          ),
          raw: const {'type': 'session_state'},
          state: 'RUNNING',
          message: '处理中',
        ),
      );
      service.emit(
        AgentStateEvent(
          timestamp: _timestamp,
          sessionId: 'session-1',
          runtimeMeta: const RuntimeMeta(
            command: 'claude',
            engine: 'claude',
            executionId: 'exec-same-1',
            claudeLifecycle: 'active',
          ),
          raw: const {'type': 'agent_state'},
          state: 'THINKING',
          message: '思考中',
          command: 'claude',
        ),
      );
      service.emit(
        LogEvent(
          timestamp: _timestamp.add(const Duration(seconds: 1)),
          sessionId: 'session-1',
          runtimeMeta: const RuntimeMeta(
            command: 'claude',
            engine: 'claude',
            executionId: 'exec-same-1',
            claudeLifecycle: 'active',
          ),
          raw: const {'type': 'log'},
          message: '上一轮回复已完成。',
          stream: 'stdout',
        ),
      );
      service.emit(
        AgentStateEvent(
          timestamp: _timestamp.add(const Duration(seconds: 2)),
          sessionId: 'session-1',
          runtimeMeta: const RuntimeMeta(
            command: 'claude',
            engine: 'claude',
            executionId: 'exec-same-1',
            claudeLifecycle: 'waiting_input',
          ),
          raw: const {'type': 'agent_state'},
          state: 'WAIT_INPUT',
          message: '等待输入',
          awaitInput: true,
          command: 'claude',
        ),
      );
      await _flushEvents();

      expect(controller.awaitInput, isTrue);
      expect(controller.isSessionBusy, isFalse);
      expect(controller.canStopCurrentRun, isFalse);
      expect(controller.activityBannerVisible, isFalse);

      service.sentPayloads.clear();
      controller.sendInputText('继续第三轮');

      expect(service.sentPayloads, hasLength(1));
      expect(service.sentPayloads.single['action'], 'input');
      expect(service.sentPayloads.single['data'], '继续第三轮\n');
      expect(controller.awaitInput, isFalse);
      expect(controller.isSessionBusy, isTrue);
      expect(controller.canStopCurrentRun, isTrue);
      expect(controller.activityVisible, isTrue);
      expect(controller.activityBannerVisible, isTrue);
      expect(controller.agentState?.state, 'THINKING');
    });

    test('新建会话会清空旧 continuation 状态，首条 codex 输入重新走 ai_turn', () async {
      final service = _FakeMobileVcWsService();
      final controller = SessionController(service: service);
      await controller.initialize();
      addTearDown(controller.disposeController);

      service.emit(
        SessionHistoryEvent(
          timestamp: _timestamp,
          sessionId: 'session-old',
          runtimeMeta: const RuntimeMeta(),
          raw: const {'type': 'session_history'},
          summary: const SessionSummary(id: 'session-old', title: '旧会话'),
          logEntries: const [
            HistoryLogEntry(
              kind: 'user',
              text: '继续处理旧问题',
              timestamp: '2026-04-01T10:00:00Z',
            ),
          ],
          rawTerminalByStream: const {'stdout': 'old stdout'},
          terminalExecutions: const [
            TerminalExecution(
              executionId: 'exec-old',
              command: 'claude',
              stdout: 'old stdout',
            ),
          ],
          sessionContext: const SessionContext(enabledSkillNames: ['ios']),
          canResume: true,
          resumeRuntimeMeta: const RuntimeMeta(
            engine: 'claude',
            command: 'claude --resume session-old',
            claudeLifecycle: 'resumable',
          ),
        ),
      );
      service.emit(
        RuntimeInfoResultEvent(
          timestamp: _timestamp.add(const Duration(milliseconds: 1)),
          sessionId: 'session-old',
          runtimeMeta: const RuntimeMeta(
            engine: 'claude',
            command: 'claude --resume session-old',
            claudeLifecycle: 'resumable',
          ),
          raw: const {'type': 'runtime_info_result'},
          query: 'context',
        ),
      );
      service.emit(
        PromptRequestEvent(
          timestamp: _timestamp.add(const Duration(milliseconds: 2)),
          sessionId: 'session-old',
          runtimeMeta: const RuntimeMeta(command: 'claude'),
          raw: const {'type': 'prompt_request'},
          message: '请输入补充说明',
        ),
      );
      await _flushEvents();

      expect(controller.shouldShowClaudeMode, isTrue);
      expect(controller.awaitInput, isTrue);
      expect(controller.timeline, isNotEmpty);
      expect(controller.terminalStdout, 'old stdout');
      expect(controller.runtimeInfo, isNotNull);
      expect(controller.sessionContext.enabledSkillNames, contains('ios'));

      service.sentPayloads.clear();
      service.emit(
        SessionCreatedEvent(
          timestamp: _timestamp.add(const Duration(seconds: 1)),
          sessionId: 'conn-1',
          runtimeMeta: const RuntimeMeta(),
          raw: const {'type': 'session_created'},
          summary: const SessionSummary(id: 'session-new', title: '新会话'),
        ),
      );
      await _flushEvents();

      expect(controller.selectedSessionId, 'session-new');
      expect(controller.timeline, isEmpty);
      expect(controller.awaitInput, isFalse);
      expect(controller.shouldShowClaudeMode, isFalse);
      expect(controller.canResumeCurrentSession, isFalse);
      expect(controller.runtimeInfo, isNull);
      expect(controller.terminalStdout, isEmpty);
      expect(controller.terminalExecutions, isEmpty);
      expect(controller.sessionContext.enabledSkillNames, isEmpty);

      final refreshActions = service.sentPayloads
          .map((item) => (item['action'] ?? '').toString())
          .toList();
      expect(refreshActions, isNot(contains('session_context_get')));
      expect(refreshActions, isNot(contains('permission_rule_list')));

      service.sentPayloads.clear();
      controller.sendInputText('codex');

      expect(service.sentPayloads, hasLength(1));
      expect(service.sentPayloads.single['action'], 'ai_turn');
      expect(service.sentPayloads.single['engine'], 'codex');
    });
  });

  group('SessionController auto session binding', () {
    test('connect 会主动请求全局 session_list，用于同步可恢复项目', () async {
      final service = _FakeMobileVcWsService();
      final controller = SessionController(service: service);
      await controller.initialize();
      addTearDown(controller.disposeController);

      await controller.connect();

      final sessionListRequest = service.sentPayloads.firstWhere(
        (item) => item['action'] == 'session_list',
      );
      expect(sessionListRequest, isNot(contains('cwd')));
    });

    test('session_updated 会合并刷新列表和当前会话 delta', () async {
      final service = _FakeMobileVcWsService();
      final controller = SessionController(service: service);
      await controller.initialize();
      addTearDown(controller.disposeController);

      await controller.connect();
      service.emit(
        SessionCreatedEvent(
          timestamp: _timestamp,
          sessionId: 'conn-1',
          runtimeMeta: const RuntimeMeta(),
          raw: const {'type': 'session_created'},
          summary: const SessionSummary(
            id: 'session-fresh',
            title: 'Fresh',
            ownership: 'mobilevc',
          ),
        ),
      );
      await _flushEvents();
      service.sentPayloads.clear();

      service.emit(SessionUpdatedEvent(
        timestamp: _timestamp,
        sessionId: 'session-fresh',
        runtimeMeta: const RuntimeMeta(),
        raw: const {'type': 'session_updated'},
        generation: 4,
        eventCursor: 10,
        reason: 'projection_persisted',
      ));
      service.emit(SessionUpdatedEvent(
        timestamp: _timestamp.add(const Duration(milliseconds: 1)),
        sessionId: 'session-fresh',
        runtimeMeta: const RuntimeMeta(),
        raw: const {'type': 'session_updated'},
        generation: 4,
        eventCursor: 9,
        reason: 'projection_persisted',
      ));
      service.emit(SessionUpdatedEvent(
        timestamp: _timestamp.add(const Duration(milliseconds: 2)),
        sessionId: 'session-fresh',
        runtimeMeta: const RuntimeMeta(),
        raw: const {'type': 'session_updated'},
        generation: 5,
        eventCursor: 11,
        reason: 'projection_persisted',
      ));
      await Future<void>.delayed(const Duration(milliseconds: 450));

      final listRequests = service.sentPayloads
          .where((payload) => payload['action'] == 'session_list')
          .toList();
      final deltaRequests = service.sentPayloads
          .where((payload) => payload['action'] == 'session_delta_get')
          .toList();
      expect(listRequests, hasLength(1));
      expect(deltaRequests, hasLength(1));
      expect(deltaRequests.single['sessionId'], 'session-fresh');
      expect(deltaRequests.single['reason'],
          'session_updated:projection_persisted');
    });

    test('session_updated delta 被节流时会保留尾部刷新', () async {
      final service = _FakeMobileVcWsService();
      final controller = SessionController(service: service);
      await controller.initialize();
      addTearDown(controller.disposeController);

      await controller.connect();
      service.emit(
        SessionCreatedEvent(
          timestamp: _timestamp,
          sessionId: 'conn-1',
          runtimeMeta: const RuntimeMeta(),
          raw: const {'type': 'session_created'},
          summary: const SessionSummary(
            id: 'session-fresh',
            title: 'Fresh',
            ownership: 'mobilevc',
          ),
        ),
      );
      await _flushEvents();
      service.sentPayloads.clear();

      service.emit(SessionUpdatedEvent(
        timestamp: _timestamp,
        sessionId: 'session-fresh',
        runtimeMeta: const RuntimeMeta(),
        raw: const {'type': 'session_updated'},
        generation: 4,
        eventCursor: 10,
        reason: 'projection_persisted',
      ));
      await Future<void>.delayed(const Duration(milliseconds: 450));
      expect(
        service.sentPayloads
            .where((payload) => payload['action'] == 'session_delta_get'),
        hasLength(1),
      );

      service.emit(SessionUpdatedEvent(
        timestamp: _timestamp.add(const Duration(milliseconds: 1)),
        sessionId: 'session-fresh',
        runtimeMeta: const RuntimeMeta(),
        raw: const {'type': 'session_updated'},
        generation: 5,
        eventCursor: 11,
        reason: 'projection_persisted',
      ));
      await Future<void>.delayed(const Duration(milliseconds: 120));
      expect(
        service.sentPayloads
            .where((payload) => payload['action'] == 'session_delta_get'),
        hasLength(1),
      );

      await Future<void>.delayed(const Duration(milliseconds: 780));
      final deltaRequests = service.sentPayloads
          .where((payload) => payload['action'] == 'session_delta_get')
          .toList();
      expect(deltaRequests, hasLength(2));
      expect(
          deltaRequests.last['reason'], 'session_updated:projection_persisted');
    });

    test('session_updated 同一 generation 的新 cursor 只刷新当前会话 delta', () async {
      final service = _FakeMobileVcWsService();
      final controller = SessionController(service: service);
      await controller.initialize();
      addTearDown(controller.disposeController);

      await controller.connect();
      service.emit(
        SessionCreatedEvent(
          timestamp: _timestamp,
          sessionId: 'conn-1',
          runtimeMeta: const RuntimeMeta(),
          raw: const {'type': 'session_created'},
          summary: const SessionSummary(
            id: 'session-fresh',
            title: 'Fresh',
            ownership: 'mobilevc',
          ),
        ),
      );
      await _flushEvents();
      service.sentPayloads.clear();

      service.emit(SessionUpdatedEvent(
        timestamp: _timestamp,
        sessionId: 'session-fresh',
        runtimeMeta: const RuntimeMeta(),
        raw: const {'type': 'session_updated'},
        generation: 7,
        eventCursor: 20,
        reason: 'projection_persisted',
      ));
      await Future<void>.delayed(const Duration(milliseconds: 450));
      expect(
        service.sentPayloads
            .where((payload) => payload['action'] == 'session_list'),
        hasLength(1),
      );
      expect(
        service.sentPayloads
            .where((payload) => payload['action'] == 'session_delta_get'),
        hasLength(1),
      );

      service.emit(SessionUpdatedEvent(
        timestamp: _timestamp.add(const Duration(milliseconds: 1)),
        sessionId: 'session-fresh',
        runtimeMeta: const RuntimeMeta(),
        raw: const {'type': 'session_updated'},
        generation: 7,
        eventCursor: 21,
        reason: 'projection_persisted',
      ));
      await Future<void>.delayed(const Duration(milliseconds: 900));

      expect(
        service.sentPayloads
            .where((payload) => payload['action'] == 'session_list'),
        hasLength(1),
      );
      final deltaRequests = service.sentPayloads
          .where((payload) => payload['action'] == 'session_delta_get')
          .toList();
      expect(deltaRequests, hasLength(2));
      expect(deltaRequests.last['sessionId'], 'session-fresh');
    });

    test('session_updated 当前会话新 cursor 会尽快拉取 delta', () async {
      final service = _FakeMobileVcWsService();
      final controller = SessionController(service: service);
      await controller.initialize();
      addTearDown(controller.disposeController);

      await controller.connect();
      service.emit(
        SessionCreatedEvent(
          timestamp: _timestamp,
          sessionId: 'conn-1',
          runtimeMeta: const RuntimeMeta(),
          raw: const {'type': 'session_created'},
          summary: const SessionSummary(
            id: 'session-fresh',
            title: 'Fresh',
            ownership: 'mobilevc',
          ),
        ),
      );
      await _flushEvents();
      service.sentPayloads.clear();

      service.emit(SessionUpdatedEvent(
        timestamp: _timestamp,
        sessionId: 'session-fresh',
        runtimeMeta: const RuntimeMeta(),
        raw: const {'type': 'session_updated'},
        generation: 8,
        eventCursor: 30,
        reason: 'projection_persisted',
      ));
      await Future<void>.delayed(const Duration(milliseconds: 120));

      final deltaRequests = service.sentPayloads
          .where((payload) => payload['action'] == 'session_delta_get')
          .toList();
      expect(deltaRequests, hasLength(1));
      expect(deltaRequests.single['reason'],
          'session_updated:projection_persisted');
    });

    test('当前会话 session_updated delta 会抢占已有列表刷新 debounce', () async {
      final service = _FakeMobileVcWsService();
      final controller = SessionController(service: service);
      await controller.initialize();
      addTearDown(controller.disposeController);

      await controller.connect();
      service.emit(
        SessionCreatedEvent(
          timestamp: _timestamp,
          sessionId: 'conn-1',
          runtimeMeta: const RuntimeMeta(),
          raw: const {'type': 'session_created'},
          summary: const SessionSummary(
            id: 'session-fresh',
            title: 'Fresh',
            ownership: 'mobilevc',
          ),
        ),
      );
      await _flushEvents();
      service.sentPayloads.clear();

      service.emit(SessionUpdatedEvent(
        timestamp: _timestamp,
        sessionId: 'session-other',
        runtimeMeta: const RuntimeMeta(),
        raw: const {'type': 'session_updated'},
        generation: 3,
        eventCursor: 20,
        reason: 'projection_persisted',
      ));
      service.emit(SessionUpdatedEvent(
        timestamp: _timestamp.add(const Duration(milliseconds: 1)),
        sessionId: 'session-fresh',
        runtimeMeta: const RuntimeMeta(),
        raw: const {'type': 'session_updated'},
        generation: 9,
        eventCursor: 31,
        reason: 'projection_persisted',
      ));
      await Future<void>.delayed(const Duration(milliseconds: 120));

      final deltaRequests = service.sentPayloads
          .where((payload) => payload['action'] == 'session_delta_get')
          .toList();
      expect(deltaRequests, hasLength(1));
      expect(deltaRequests.single['sessionId'], 'session-fresh');
    });

    test('loadSessionFromSummary 会先切换到会话 cwd 再加载', () async {
      final service = _FakeMobileVcWsService();
      final controller = SessionController(service: service);
      await controller.initialize();
      addTearDown(controller.disposeController);

      await controller.connect();
      service.sentPayloads.clear();

      await controller.loadSessionFromSummary(const SessionSummary(
        id: 'session-target',
        title: 'Target',
        runtime: RuntimeMeta(cwd: '/workspace/Target'),
      ));
      await _flushEvents();

      expect(controller.effectiveCwd, '/workspace/Target');
      expect(service.sentPayloads.first['action'], 'fs_list');
      expect(service.sentPayloads.first['path'], '/workspace/Target');
      final load = service.sentPayloads.firstWhere(
        (payload) => payload['action'] == 'session_load',
      );
      expect(load['sessionId'], 'session-target');
      expect(load['cwd'], '/workspace/Target');
    });

    test('连接后收到非空 session 列表时，仅刷新列表，不自动 load 历史会话', () async {
      final service = _FakeMobileVcWsService();
      final controller = SessionController(service: service);
      await controller.initialize();
      addTearDown(controller.disposeController);

      await controller.connect();
      service.sentPayloads.clear();

      service.emit(
        SessionListResultEvent(
          timestamp: _timestamp,
          sessionId: 'conn-1',
          runtimeMeta: const RuntimeMeta(),
          raw: const {'type': 'session_list_result'},
          items: const [
            SessionSummary(id: 'session-a', title: 'A'),
            SessionSummary(id: 'session-b', title: 'B'),
          ],
        ),
      );
      await _flushEvents();

      expect(service.sentPayloads, isEmpty);
      expect(controller.sessions.map((item) => item.id).toList(), [
        'session-a',
        'session-b',
      ]);
      expect(controller.selectedSessionId, isEmpty);
    });

    test('连接后自动恢复配置里的上次会话', () async {
      SharedPreferences.setMockInitialValues({
        'mobilevc.app_config': jsonEncode(const AppConfig(
          lastSessionId: 'session-target',
          lastSessionCwd: '/workspace/Saved',
          historyWindowLimit: 240,
        ).toJson()),
      });
      final service = _FakeMobileVcWsService();
      final controller = SessionController(service: service);
      await controller.initialize();
      addTearDown(controller.disposeController);

      await controller.connect();
      service.sentPayloads.clear();

      service.emit(
        SessionListResultEvent(
          timestamp: _timestamp,
          sessionId: 'conn-1',
          runtimeMeta: const RuntimeMeta(),
          raw: const {'type': 'session_list_result'},
          items: const [
            SessionSummary(
              id: 'session-target',
              title: '上次会话',
              runtime: RuntimeMeta(cwd: '/workspace/Target'),
            ),
            SessionSummary(id: 'session-other', title: 'Other'),
          ],
        ),
      );
      await _flushEvents();

      final loads = service.sentPayloads
          .where((payload) => payload['action'] == 'session_load')
          .toList();
      expect(loads, hasLength(1));
      expect(loads.single['sessionId'], 'session-target');
      expect(loads.single['cwd'], '/workspace/Target');
      expect(loads.single['limit'], 240);
    });

    test('通知目标优先于配置里的上次会话恢复', () async {
      SharedPreferences.setMockInitialValues({
        'mobilevc.app_config': jsonEncode(const AppConfig(
          lastSessionId: 'session-old',
        ).toJson()),
      });
      final service = _FakeMobileVcWsService();
      final controller = SessionController(service: service);
      await controller.initialize();
      addTearDown(controller.disposeController);

      await controller.connect();
      service.sentPayloads.clear();

      await controller.restoreSessionFromNotification('session-notify');
      service.emit(
        SessionListResultEvent(
          timestamp: _timestamp,
          sessionId: 'conn-1',
          runtimeMeta: const RuntimeMeta(),
          raw: const {'type': 'session_list_result'},
          items: const [
            SessionSummary(id: 'session-old', title: 'Old'),
          ],
        ),
      );
      await _flushEvents();

      final loads = service.sentPayloads
          .where((payload) => payload['action'] == 'session_load')
          .toList();
      expect(loads, hasLength(1));
      expect(loads.single['sessionId'], 'session-notify');
    });

    test('成功恢复历史后保存上次会话', () async {
      final service = _FakeMobileVcWsService();
      final controller = SessionController(service: service);
      await controller.initialize();
      addTearDown(controller.disposeController);

      await controller.connect();
      service.emit(
        SessionHistoryEvent(
          timestamp: _timestamp,
          sessionId: 'session-history',
          runtimeMeta: const RuntimeMeta(command: 'claude'),
          raw: const {'type': 'session_history'},
          summary: const SessionSummary(
            id: 'session-history',
            title: '历史会话',
          ),
          resumeRuntimeMeta: const RuntimeMeta(cwd: '/workspace/History'),
        ),
      );
      await _flushEvents();

      final prefs = await SharedPreferences.getInstance();
      final raw = prefs.getString('mobilevc.app_config');
      final restored =
          AppConfig.fromJson(jsonDecode(raw!) as Map<String, dynamic>);
      expect(restored.lastSessionId, 'session-history');
      expect(restored.lastSessionCwd, '/workspace/History');
    });

    test('连接后收到空 session 列表时，不自动 create session', () async {
      final service = _FakeMobileVcWsService();
      final controller = SessionController(service: service);
      await controller.initialize();
      addTearDown(controller.disposeController);

      await controller.connect();
      service.sentPayloads.clear();

      service.emit(
        SessionListResultEvent(
          timestamp: _timestamp,
          sessionId: 'conn-1',
          runtimeMeta: const RuntimeMeta(),
          raw: const {'type': 'session_list_result'},
          items: const [],
        ),
      );
      await _flushEvents();

      expect(service.sentPayloads, isEmpty);
      expect(controller.sessions, isEmpty);
      expect(controller.selectedSessionId, isEmpty);
    });

    test('已有选中会话时，即使列表里没有该会话，也不会再次自动 create 或 load', () async {
      final service = _FakeMobileVcWsService();
      final controller = SessionController(service: service);
      await controller.initialize();
      addTearDown(controller.disposeController);

      await controller.connect();
      service.sentPayloads.clear();

      service.emit(
        SessionCreatedEvent(
          timestamp: _timestamp,
          sessionId: 'conn-1',
          runtimeMeta: const RuntimeMeta(),
          raw: const {'type': 'session_created'},
          summary:
              const SessionSummary(id: 'session-current', title: 'Current'),
        ),
      );
      await _flushEvents();

      service.sentPayloads.clear();
      service.emit(
        SessionListResultEvent(
          timestamp: _timestamp,
          sessionId: 'conn-1',
          runtimeMeta: const RuntimeMeta(),
          raw: const {'type': 'session_list_result'},
          items: const [],
        ),
      );
      await _flushEvents();

      expect(service.sentPayloads, isEmpty);
      expect(controller.sessions, hasLength(1));
      expect(controller.sessions.single.id, 'session-current');
    });

    test('后台断开后回前台重连不会自动恢复上次会话', () async {
      final service = _FakeMobileVcWsService();
      final controller = SessionController(service: service);
      await controller.initialize();
      addTearDown(controller.disposeController);

      await controller.connect();
      service.sentPayloads.clear();

      service.emit(
        SessionHistoryEvent(
          timestamp: _timestamp,
          sessionId: 'session-reconnect',
          runtimeMeta: const RuntimeMeta(command: 'claude'),
          raw: const {'type': 'session_history'},
          summary: const SessionSummary(
            id: 'session-reconnect',
            title: '恢复中的会话',
          ),
          logEntries: const [
            HistoryLogEntry(
              kind: 'assistant',
              message: '上一条回复',
              label: 'Assistant',
            ),
          ],
          resumeRuntimeMeta: const RuntimeMeta(
            command: 'claude --resume session-reconnect',
            claudeLifecycle: 'resumable',
          ),
        ),
      );
      await _flushEvents();
      expect(controller.selectedSessionId, 'session-reconnect');

      controller.handleForegroundStateChanged(false);
      service.sentPayloads.clear();

      service.emit(
        ErrorEvent(
          timestamp: _timestamp.add(const Duration(seconds: 1)),
          sessionId: 'session-reconnect',
          runtimeMeta: const RuntimeMeta(command: 'claude'),
          raw: const {'type': 'error'},
          message: 'websocket closed',
          code: 'ws_closed',
        ),
      );
      await _flushEvents();

      controller.handleForegroundStateChanged(true);
      await _flushEvents();

      expect(service.connectCalls, 2);
      expect(
        service.sentPayloads.any((item) => item['action'] == 'session_resume'),
        isFalse,
      );
      expect(controller.connectionStage, SessionConnectionStage.connected);
    });

    test('缓存 token 会在新建 session 后补发注册', () async {
      final service = _FakeMobileVcWsService();
      final controller = SessionController(service: service);
      await controller.initialize();
      addTearDown(controller.disposeController);

      await controller.connect();
      service.sentPayloads.clear();
      controller.setDevicePushToken('apns-token-created');
      expect(
        service.sentPayloads
            .where((item) => item['action'] == 'register_push_token'),
        isEmpty,
      );

      service.emit(
        SessionCreatedEvent(
          timestamp: _timestamp,
          sessionId: 'conn-1',
          runtimeMeta: const RuntimeMeta(),
          raw: const {'type': 'session_created'},
          summary: const SessionSummary(
            id: 'session-created',
            title: 'Created',
          ),
        ),
      );
      await _flushEvents();

      expect(
        service.sentPayloads
            .where((item) => item['action'] == 'register_push_token'),
        [
          {
            'action': 'register_push_token',
            'sessionId': 'session-created',
            'token': 'apns-token-created',
            'platform': 'ios',
          },
        ],
      );
    });

    test('缓存 token 会在恢复历史 session 后补发注册', () async {
      final service = _FakeMobileVcWsService();
      final controller = SessionController(service: service);
      await controller.initialize();
      addTearDown(controller.disposeController);

      await controller.connect();
      service.sentPayloads.clear();
      controller.setDevicePushToken('apns-token-history');
      expect(
        service.sentPayloads
            .where((item) => item['action'] == 'register_push_token'),
        isEmpty,
      );

      service.emit(
        SessionHistoryEvent(
          timestamp: _timestamp,
          sessionId: 'session-history',
          runtimeMeta: const RuntimeMeta(command: 'claude'),
          raw: const {'type': 'session_history'},
          summary: const SessionSummary(
            id: 'session-history',
            title: '历史会话',
          ),
          logEntries: const [
            HistoryLogEntry(
              kind: 'assistant',
              message: '上一条回复',
              label: 'Assistant',
            ),
          ],
        ),
      );
      await _flushEvents();

      expect(
        service.sentPayloads
            .where((item) => item['action'] == 'register_push_token'),
        [
          {
            'action': 'register_push_token',
            'sessionId': 'session-history',
            'token': 'apns-token-history',
            'platform': 'ios',
          },
        ],
      );
    });

    test('缓存 token 会在重连成功后对当前 session 补发注册', () async {
      final service = _FakeMobileVcWsService();
      final controller = SessionController(service: service);
      await controller.initialize();
      addTearDown(controller.disposeController);

      await controller.connect();
      service.sentPayloads.clear();

      service.emit(
        SessionHistoryEvent(
          timestamp: _timestamp,
          sessionId: 'session-reconnect',
          runtimeMeta: const RuntimeMeta(command: 'codex', engine: 'codex'),
          raw: const {'type': 'session_history'},
          summary: const SessionSummary(
            id: 'session-reconnect',
            title: '恢复中的会话',
          ),
          logEntries: const [
            HistoryLogEntry(
              kind: 'assistant',
              message: '上一条回复',
              label: 'Assistant',
            ),
          ],
        ),
      );
      await _flushEvents();

      service.sentPayloads.clear();
      controller.setDevicePushToken('apns-token-reconnect');
      expect(
        service.sentPayloads
            .where((item) => item['action'] == 'register_push_token'),
        [
          {
            'action': 'register_push_token',
            'sessionId': 'session-reconnect',
            'token': 'apns-token-reconnect',
            'platform': 'ios',
          },
        ],
      );

      controller.handleForegroundStateChanged(false);
      service.sentPayloads.clear();
      service.emit(
        ErrorEvent(
          timestamp: _timestamp.add(const Duration(seconds: 1)),
          sessionId: 'session-reconnect',
          runtimeMeta: const RuntimeMeta(command: 'codex', engine: 'codex'),
          raw: const {'type': 'error'},
          message: 'websocket closed',
          code: 'ws_closed',
        ),
      );
      await _flushEvents();

      controller.handleForegroundStateChanged(true);
      await _flushEvents();

      final registerPayloads = service.sentPayloads
          .where((item) => item['action'] == 'register_push_token')
          .toList();
      expect(registerPayloads, [
        {
          'action': 'register_push_token',
          'sessionId': 'session-reconnect',
          'token': 'apns-token-reconnect',
          'platform': 'ios',
        },
      ]);
    });

    test('切换到另一条历史 session 时会向新 session 注册缓存 token', () async {
      final service = _FakeMobileVcWsService();
      final controller = SessionController(service: service);
      await controller.initialize();
      addTearDown(controller.disposeController);

      await controller.connect();
      service.sentPayloads.clear();
      controller.setDevicePushToken('apns-token-switch');
      expect(
        service.sentPayloads
            .where((item) => item['action'] == 'register_push_token'),
        isEmpty,
      );

      service.emit(
        SessionHistoryEvent(
          timestamp: _timestamp,
          sessionId: 'session-a',
          runtimeMeta: const RuntimeMeta(),
          raw: const {'type': 'session_history'},
          summary: const SessionSummary(id: 'session-a', title: 'A'),
          logEntries: const [],
        ),
      );
      await _flushEvents();

      service.sentPayloads.clear();
      controller.loadSession('session-b');
      service.emit(
        SessionHistoryEvent(
          timestamp: _timestamp.add(const Duration(seconds: 1)),
          sessionId: 'session-b',
          runtimeMeta: const RuntimeMeta(),
          raw: const {'type': 'session_history'},
          summary: const SessionSummary(id: 'session-b', title: 'B'),
          logEntries: const [],
        ),
      );
      await _flushEvents();

      expect(
        service.sentPayloads
            .where((item) => item['action'] == 'register_push_token'),
        [
          {
            'action': 'register_push_token',
            'sessionId': 'session-b',
            'token': 'apns-token-switch',
            'platform': 'ios',
          },
        ],
      );
    });

    test('后台断开后会保留会话界面，并在回前台时静默重连恢复会话', () async {
      final service = _FakeMobileVcWsService();
      final controller = SessionController(service: service);
      await controller.initialize();
      addTearDown(controller.disposeController);

      await controller.connect();
      service.sentPayloads.clear();

      service.emit(
        SessionHistoryEvent(
          timestamp: _timestamp,
          sessionId: 'session-reconnect',
          runtimeMeta: const RuntimeMeta(command: 'codex', engine: 'codex'),
          raw: const {'type': 'session_history'},
          summary: const SessionSummary(
            id: 'session-reconnect',
            title: '恢复中的会话',
          ),
          logEntries: const [
            HistoryLogEntry(
              kind: 'assistant',
              message: '上一条回复',
              label: 'Assistant',
            ),
          ],
          resumeRuntimeMeta: const RuntimeMeta(
            command: 'codex',
            engine: 'codex',
            claudeLifecycle: 'resumable',
          ),
        ),
      );
      await _flushEvents();
      expect(controller.selectedSessionId, 'session-reconnect');
      expect(controller.shouldShowSessionSurface, isTrue);
      service.emit(
        AgentStateEvent(
          timestamp: _timestamp.add(const Duration(milliseconds: 200)),
          sessionId: 'session-reconnect',
          runtimeMeta: const RuntimeMeta(command: 'codex', engine: 'codex'),
          raw: const {'type': 'agent_state'},
          state: 'RUNNING_TOOL',
          message: '执行中',
          command: 'codex',
        ),
      );
      service.emit(
        PromptRequestEvent(
          timestamp: _timestamp.add(const Duration(milliseconds: 300)),
          sessionId: 'session-reconnect',
          runtimeMeta: const RuntimeMeta(command: 'codex', engine: 'codex'),
          raw: const {'type': 'prompt_request', 'eventCursor': 7},
          message: '继续输入',
        ),
      );
      await _flushEvents();

      controller.handleForegroundStateChanged(false);
      service.sentPayloads.clear();

      service.emit(
        ErrorEvent(
          timestamp: _timestamp.add(const Duration(seconds: 1)),
          sessionId: 'session-reconnect',
          runtimeMeta: const RuntimeMeta(command: 'codex', engine: 'codex'),
          raw: const {'type': 'error'},
          message: 'websocket closed',
          code: 'ws_closed',
        ),
      );
      await _flushEvents();

      expect(controller.connected, isFalse);
      expect(controller.reconnecting, isTrue);
      expect(controller.connectionMessage, '后台连接已暂停');
      expect(controller.shouldShowSessionSurface, isTrue);
      expect(service.connectCalls, 1);
      expect(
        service.sentPayloads.where((item) => item['action'] == 'session_load'),
        isEmpty,
      );

      controller.handleForegroundStateChanged(true);
      await _flushEvents();

      expect(service.connectCalls, 2);
      expect(controller.connected, isTrue);
      expect(controller.reconnecting, isFalse);
      expect(controller.connectionStage, SessionConnectionStage.connected);
      expect(
        service.sentPayloads.any((item) => item['action'] == 'session_resume'),
        isFalse,
      );
      expect(
        service.sentPayloads.where((item) => item['action'] == 'session_list'),
        isNotEmpty,
      );
      await Future<void>.delayed(const Duration(milliseconds: 520));
      await _flushEvents();

      expect(
        service.sentPayloads
            .where((item) => item['action'] == 'session_context_get'),
        isNotEmpty,
      );
      expect(
        service.sentPayloads
            .where((item) => item['action'] == 'session_diff_page_get'),
        isEmpty,
      );
    });

    test('session_resume_notice 会触发补发通知而不插入 timeline', () async {
      final service = _FakeMobileVcWsService();
      final controller = SessionController(service: service);
      await controller.initialize();
      addTearDown(controller.disposeController);

      await controller.connect();
      service.emit(
        SessionResumeNoticeEvent(
          timestamp: _timestamp,
          sessionId: 'session-1',
          runtimeMeta: const RuntimeMeta(command: 'codex', engine: 'codex'),
          raw: const {'type': 'session_resume_notice', 'eventCursor': 3},
          noticeType: 'assistant_reply',
          level: 'info',
          title: 'MobileVC',
          message: '后台期间有新的回复',
        ),
      );
      await _flushEvents();

      expect(controller.notificationSignal, isNotNull);
      expect(
        controller.notificationSignal?.type,
        AppNotificationType.assistantReply,
      );
      expect(controller.notificationSignal?.body, '后台期间有新的回复');
      expect(controller.timeline, isEmpty);
    });

    test('后台 assistant 通知后恢复历史时，正文会从 history 进入 timeline', () async {
      final service = _FakeMobileVcWsService();
      final controller = SessionController(service: service);
      await controller.initialize();
      addTearDown(controller.disposeController);

      await controller.connect();
      service.emit(
        SessionResumeNoticeEvent(
          timestamp: _timestamp,
          sessionId: 'session-1',
          runtimeMeta: const RuntimeMeta(command: 'claude', engine: 'claude'),
          raw: const {'type': 'session_resume_notice', 'eventCursor': 3},
          noticeType: 'assistant_reply',
          level: 'info',
          title: 'MobileVC',
          message: '后台期间有新的回复',
        ),
      );
      await _flushEvents();

      expect(controller.notificationSignal?.type,
          AppNotificationType.assistantReply);
      expect(controller.timeline, isEmpty);

      service.emit(
        SessionHistoryEvent(
          timestamp: _timestamp.add(const Duration(seconds: 1)),
          sessionId: 'session-1',
          runtimeMeta: const RuntimeMeta(command: 'claude', engine: 'claude'),
          raw: const {'type': 'session_history'},
          summary: const SessionSummary(id: 'session-1', title: '历史会话'),
          logEntries: const [
            HistoryLogEntry(
              kind: 'markdown',
              message: '我先帮你梳理下根因，再给你一个最稳的修复方案。',
              stream: 'stdout',
              timestamp: '2026-04-13T10:20:30Z',
            ),
          ],
        ),
      );
      await _flushEvents();

      final markdownItem = controller.timeline.singleWhere(
        (entry) => entry.kind == 'markdown',
      );
      expect(markdownItem.body, '我先帮你梳理下根因，再给你一个最稳的修复方案。');
    });

    test('system/bootstrap source 的日志不会进入 timeline 或通知', () async {
      final service = _FakeMobileVcWsService();
      final controller = SessionController(service: service);
      await controller.initialize();
      addTearDown(controller.disposeController);

      await controller.connect();
      service.emit(
        LogEvent(
          timestamp: _timestamp,
          sessionId: 'session-1',
          runtimeMeta: const RuntimeMeta(
            command: 'codex',
            engine: 'codex',
            source: 'system/bootstrap',
          ),
          raw: const {'type': 'log'},
          message: 'Using codex medium mode',
          stream: 'stdout',
        ),
      );
      await _flushEvents();

      expect(controller.timeline, isEmpty);
      expect(controller.notificationSignal, isNull);
    });

    test('session_list_result 缺少当前新建会话时，仍保留本地选中项', () async {
      final service = _FakeMobileVcWsService();
      final controller = SessionController(service: service);
      await controller.initialize();
      addTearDown(controller.disposeController);

      service.emit(
        SessionCreatedEvent(
          timestamp: _timestamp,
          sessionId: 'conn-1',
          runtimeMeta: const RuntimeMeta(),
          raw: const {'type': 'session_created'},
          summary:
              const SessionSummary(id: 'session-current', title: 'Current'),
        ),
      );
      await _flushEvents();

      service.emit(
        SessionListResultEvent(
          timestamp: _timestamp.add(const Duration(seconds: 1)),
          sessionId: 'conn-1',
          runtimeMeta: const RuntimeMeta(),
          raw: const {'type': 'session_list_result'},
          items: const [
            SessionSummary(id: 'session-other', title: 'Other'),
          ],
        ),
      );
      await _flushEvents();

      expect(
        controller.sessions.map((item) => item.id).toList(),
        ['session-current', 'session-other'],
      );
      expect(controller.selectedSessionId, 'session-current');
    });

    test('session_list_result 大列表合并会保留已有可见摘要', () async {
      final service = _FakeMobileVcWsService();
      final controller = SessionController(service: service);
      await controller.initialize();
      addTearDown(controller.disposeController);

      service.emit(
        SessionListResultEvent(
          timestamp: _timestamp,
          sessionId: '',
          runtimeMeta: const RuntimeMeta(),
          raw: const {'type': 'session_list_result'},
          items: [
            const SessionSummary(
              id: 'session-keep',
              title: 'session',
              lastPreview: '用户刚才的问题',
            ),
            ...List<SessionSummary>.generate(
              240,
              (index) => SessionSummary(
                id: 'session-$index',
                title: 'Old $index',
              ),
            ),
          ],
        ),
      );
      await _flushEvents();

      service.emit(
        SessionListResultEvent(
          timestamp: _timestamp.add(const Duration(seconds: 1)),
          sessionId: '',
          runtimeMeta: const RuntimeMeta(),
          raw: const {'type': 'session_list_result'},
          items: [
            const SessionSummary(id: 'session-keep', title: 'session'),
            ...List<SessionSummary>.generate(
              240,
              (index) => SessionSummary(
                id: 'session-$index',
                title: 'New $index',
              ),
            ),
          ],
        ),
      );
      await _flushEvents();

      expect(controller.sessions, hasLength(241));
      expect(controller.sessions.first.id, 'session-keep');
      expect(controller.sessions.first.lastPreview, '用户刚才的问题');
      expect(controller.sessions.last.id, 'session-239');
      expect(controller.sessions.last.title, 'New 239');
    });

    test('deleteSession 立即移除本地会话并发送删除请求', () async {
      final service = _FakeMobileVcWsService();
      final controller = SessionController(service: service);
      await controller.initialize();
      addTearDown(controller.disposeController);

      service.emit(
        SessionListResultEvent(
          timestamp: _timestamp,
          sessionId: '',
          runtimeMeta: const RuntimeMeta(),
          raw: const {'type': 'session_list_result'},
          items: const [
            SessionSummary(id: 'session-a', title: 'Session A'),
            SessionSummary(id: 'session-b', title: 'Session B'),
          ],
        ),
      );
      await _flushEvents();
      service.sentPayloads.clear();

      controller.deleteSession('session-a');
      await _flushEvents();

      expect(controller.sessions.map((item) => item.id).toList(), [
        'session-b',
      ]);
      expect(service.sentPayloads, hasLength(1));
      expect(service.sentPayloads.single, {
        'action': 'session_delete',
        'sessionId': 'session-a',
      });

      service.emit(
        SessionListResultEvent(
          timestamp: _timestamp.add(const Duration(seconds: 1)),
          sessionId: '',
          runtimeMeta: const RuntimeMeta(),
          raw: const {'type': 'session_list_result'},
          items: const [
            SessionSummary(id: 'session-b', title: 'Session B'),
          ],
        ),
      );
      await _flushEvents();

      expect(controller.sessions.map((item) => item.id).toList(), [
        'session-b',
      ]);
    });

    test('deleteSession 删除当前会话时立即清空选中会话', () async {
      final service = _FakeMobileVcWsService();
      final controller = SessionController(service: service);
      await controller.initialize();
      addTearDown(controller.disposeController);

      service.emit(
        SessionCreatedEvent(
          timestamp: _timestamp,
          sessionId: 'session-a',
          runtimeMeta: const RuntimeMeta(),
          raw: const {'type': 'session_created'},
          summary: const SessionSummary(id: 'session-a', title: 'Session A'),
        ),
      );
      await _flushEvents();
      expect(controller.selectedSessionId, 'session-a');

      controller.deleteSession('session-a');
      await _flushEvents();

      expect(controller.selectedSessionId, isEmpty);

      service.emit(
        LogEvent(
          timestamp: _timestamp.add(const Duration(seconds: 1)),
          sessionId: 'session-a',
          runtimeMeta: const RuntimeMeta(engine: 'codex', command: 'codex'),
          raw: const {'type': 'log'},
          message: 'stale reply',
          stream: 'stdout',
        ),
      );
      await _flushEvents();

      expect(controller.timeline.any((item) => item.body == 'stale reply'),
          isFalse);
    });

    test('deleteSession 删除当前会话后会忽略迟到历史快照', () async {
      final service = _FakeMobileVcWsService();
      final controller = SessionController(service: service);
      await controller.initialize();
      addTearDown(controller.disposeController);

      service.emit(
        SessionCreatedEvent(
          timestamp: _timestamp,
          sessionId: 'session-a',
          runtimeMeta: const RuntimeMeta(),
          raw: const {'type': 'session_created'},
          summary: const SessionSummary(id: 'session-a', title: 'Session A'),
        ),
      );
      await _flushEvents();

      controller.deleteSession('session-a');
      await _flushEvents();

      service.emit(
        SessionHistoryEvent(
          timestamp: _timestamp.add(const Duration(seconds: 1)),
          sessionId: 'session-a',
          runtimeMeta: const RuntimeMeta(),
          raw: const {'type': 'session_history'},
          summary: const SessionSummary(id: 'session-a', title: 'Session A'),
          logEntries: const [
            HistoryLogEntry(
              kind: 'markdown',
              message: 'stale history',
              timestamp: '2026-06-08T00:00:00Z',
            ),
          ],
        ),
      );
      await _flushEvents();

      expect(controller.selectedSessionId, isEmpty);
      expect(controller.timeline.any((item) => item.body == 'stale history'),
          isFalse);
    });

    test('deleteSession 不会把同一 Claude UUID 重新显示成电脑 Claude', () async {
      final service = _FakeMobileVcWsService();
      final controller = SessionController(service: service);
      await controller.initialize();
      addTearDown(controller.disposeController);

      service.emit(
        SessionListResultEvent(
          timestamp: _timestamp,
          sessionId: '',
          runtimeMeta: const RuntimeMeta(),
          raw: const {'type': 'session_list_result'},
          items: const [
            SessionSummary(
              id: 'session-a',
              title: 'MobileVC Claude',
              ownership: 'mobilevc',
              source: 'mobilevc',
              runtime: RuntimeMeta(
                engine: 'claude',
                command: 'claude',
                claudeSessionUuid: 'native-claude-1',
                source: 'mobilevc',
              ),
            ),
            SessionSummary(id: 'session-b', title: 'Session B'),
          ],
        ),
      );
      await _flushEvents();

      controller.deleteSession('session-a');
      await _flushEvents();

      service.emit(
        SessionListResultEvent(
          timestamp: _timestamp.add(const Duration(seconds: 1)),
          sessionId: '',
          runtimeMeta: const RuntimeMeta(),
          raw: const {'type': 'session_list_result'},
          items: const [
            SessionSummary(
              id: 'claude-session:native-claude-1',
              title: '电脑 Claude',
              external: true,
              source: 'claude-native',
              runtime: RuntimeMeta(
                engine: 'claude',
                command: 'claude',
                resumeSessionId: 'native-claude-1',
                source: 'claude-native',
              ),
            ),
            SessionSummary(id: 'session-b', title: 'Session B'),
          ],
        ),
      );
      await _flushEvents();

      expect(controller.sessions.map((item) => item.id).toList(), [
        'session-b',
      ]);
    });

    test('deleteSession 失败时恢复本地会话并显示错误', () async {
      final service = _FakeMobileVcWsService();
      final controller = SessionController(service: service);
      await controller.initialize();
      addTearDown(controller.disposeController);

      service.emit(
        SessionListResultEvent(
          timestamp: _timestamp,
          sessionId: '',
          runtimeMeta: const RuntimeMeta(),
          raw: const {'type': 'session_list_result'},
          items: const [
            SessionSummary(id: 'session-a', title: 'Session A'),
            SessionSummary(id: 'session-b', title: 'Session B'),
          ],
        ),
      );
      await _flushEvents();

      controller.deleteSession('session-a');
      await _flushEvents();
      expect(controller.sessions.map((item) => item.id).toList(), [
        'session-b',
      ]);

      service.emit(
        ErrorEvent(
          timestamp: _timestamp.add(const Duration(seconds: 1)),
          sessionId: '',
          runtimeMeta: const RuntimeMeta(),
          raw: const {'type': 'error'},
          message: 'store write failed',
          code: 'session_delete_failed',
        ),
      );
      await _flushEvents();

      expect(controller.sessions.map((item) => item.id).toList(), [
        'session-a',
        'session-b',
      ]);
      expect(controller.timeline.last.kind, 'error');
      expect(controller.timeline.last.body, contains('删除会话失败'));
    });

    test(
        'deleteSession pending does not relabel runtime errors as delete failure',
        () async {
      final service = _FakeMobileVcWsService();
      final controller = SessionController(service: service);
      await controller.initialize();
      addTearDown(controller.disposeController);

      service.emit(
        SessionListResultEvent(
          timestamp: _timestamp,
          sessionId: '',
          runtimeMeta: const RuntimeMeta(),
          raw: const {'type': 'session_list_result'},
          items: const [
            SessionSummary(id: 'session-a', title: 'Session A'),
            SessionSummary(id: 'session-b', title: 'Session B'),
          ],
        ),
      );
      await _flushEvents();

      controller.deleteSession('session-a');
      await _flushEvents();

      service.emit(
        ErrorEvent(
          timestamp: _timestamp.add(const Duration(seconds: 1)),
          sessionId: 'session-b',
          runtimeMeta: const RuntimeMeta(),
          raw: const {'type': 'error'},
          message:
              'start codex app-server: exec: "codex": executable file not found in \$PATH',
          code: '',
        ),
      );
      await _flushEvents();

      expect(controller.sessions.map((item) => item.id).toList(), [
        'session-b',
      ]);
      expect(controller.timeline.last.kind, 'error');
      expect(controller.timeline.last.body, contains('start codex app-server'));
      expect(controller.timeline.last.body, isNot(contains('删除会话失败')));
    });

    test('历史 terminal entry 优先用 text，并可恢复成 markdown', () async {
      final service = _FakeMobileVcWsService();
      final controller = SessionController(service: service);
      await controller.initialize();
      addTearDown(controller.disposeController);

      service.emit(
        SessionHistoryEvent(
          timestamp: _timestamp,
          sessionId: 'session-history',
          runtimeMeta: const RuntimeMeta(),
          raw: const {'type': 'session_history'},
          summary: const SessionSummary(id: 'session-history', title: '历史会话'),
          logEntries: const [
            HistoryLogEntry(
              kind: 'terminal',
              message: '不是在 ChatGPT 应用里那个聊天形态；这里我是通过 Codex/CX 这个编码代理在你的工作区里协作。',
              text:
                  '不是在 ChatGPT 应用里那个聊天形态；这里我是通过 Codex/CX 这个编码代理在你的工作区里协作。\n底层是 GPT 系列模型。',
              stream: 'stdout',
              timestamp: '2026-03-31T08:06:44Z',
            ),
          ],
        ),
      );
      await _flushEvents();

      final item = controller.timeline.firstWhere(
        (entry) => entry.body.contains('底层是 GPT 系列模型。'),
      );
      expect(item.body,
          '不是在 ChatGPT 应用里那个聊天形态；这里我是通过 Codex/CX 这个编码代理在你的工作区里协作。\n底层是 GPT 系列模型。');
      expect(item.kind, 'markdown');
    });

    test('恢复历史时会过滤启动命令和 command finished 噪声', () async {
      final service = _FakeMobileVcWsService();
      final controller = SessionController(service: service);
      await controller.initialize();
      addTearDown(controller.disposeController);

      service.emit(
        SessionHistoryEvent(
          timestamp: _timestamp,
          sessionId: 'session-history',
          runtimeMeta: const RuntimeMeta(),
          raw: const {'type': 'session_history'},
          summary: const SessionSummary(
            id: 'session-history',
            title: 'codex -m gpt-5-codex --config model_reasoning_effort=high',
          ),
          logEntries: const [
            HistoryLogEntry(
              kind: 'terminal',
              text: 'codex -m gpt-5-codex --config model_reasoning_effort=high',
              stream: 'stdout',
              timestamp: '2026-03-31T08:06:40Z',
            ),
            HistoryLogEntry(
              kind: 'terminal',
              text: 'command finished',
              stream: 'stdout',
              timestamp: '2026-03-31T08:06:41Z',
            ),
            HistoryLogEntry(
              kind: 'terminal',
              text: '这是恢复后的第一条正常回复。',
              stream: 'stdout',
              timestamp: '2026-03-31T08:06:42Z',
            ),
          ],
        ),
      );
      await _flushEvents();

      expect(controller.selectedSessionTitle, 'Codex 会话');
      expect(
        controller.timeline.map((item) => item.body).toList(),
        ['这是恢复后的第一条正常回复。'],
      );
    });

    test('外部 Codex 会话在空历史时会用最后预览兜底，避免空白', () async {
      final service = _FakeMobileVcWsService();
      final controller = SessionController(service: service);
      await controller.initialize();
      addTearDown(controller.disposeController);

      service.emit(
        SessionHistoryEvent(
          timestamp: _timestamp,
          sessionId: 'codex-thread:1',
          runtimeMeta: const RuntimeMeta(),
          raw: const {'type': 'session_history'},
          summary: const SessionSummary(
            id: 'codex-thread:1',
            title: '2026-04-01 20:15',
            lastPreview: '修一下登录页按钮间距',
            source: 'codex-native',
            external: true,
          ),
          resumeRuntimeMeta: const RuntimeMeta(
            engine: 'codex',
            command: 'codex',
            resumeSessionId: 'thread-1',
          ),
        ),
      );
      await _flushEvents();

      expect(controller.selectedSessionTitle, '电脑 Codex');
      expect(controller.timeline, hasLength(1));
      expect(controller.timeline.single.kind, 'user');
      expect(controller.timeline.single.body, '修一下登录页按钮间距');
    });

    test('外部 Codex 会话恢复后左上角标题使用电脑来源名称', () async {
      final service = _FakeMobileVcWsService();
      final controller = SessionController(service: service);
      await controller.initialize();
      addTearDown(controller.disposeController);

      service.emit(
        SessionHistoryEvent(
          timestamp: _timestamp,
          sessionId: 'codex-thread:3',
          runtimeMeta: const RuntimeMeta(),
          raw: const {'type': 'session_history'},
          summary: const SessionSummary(
            id: 'codex-thread:3',
            title: '这里有问题不能删除mobilevc喃',
            source: 'codex-native',
            external: true,
            runtime: RuntimeMeta(
              source: 'codex-native',
              engine: 'codex',
              cwd: '/workspace/MobileVC',
            ),
          ),
          resumeRuntimeMeta: const RuntimeMeta(
            source: 'codex-native',
            engine: 'codex',
            command: 'codex',
            resumeSessionId: 'thread-3',
          ),
        ),
      );
      await _flushEvents();

      expect(controller.selectedSessionTitle, '电脑 Codex');
      expect(controller.sessions.single.title, '这里有问题不能删除mobilevc喃');
    });

    test('Codex mirror id 恢复时优先于旧 Claude runtime source', () async {
      final service = _FakeMobileVcWsService();
      final controller = SessionController(service: service);
      await controller.initialize();
      addTearDown(controller.disposeController);

      await controller.saveConfig(const AppConfig(
        engine: 'claude',
        permissionMode: 'bypassPermissions',
        codexSandboxMode: 'danger-full-access',
      ));
      await controller.connect();
      service.emit(
        SessionHistoryEvent(
          timestamp: _timestamp,
          sessionId: 'codex-thread:stale-runtime',
          runtimeMeta: const RuntimeMeta(),
          raw: const {'type': 'session_history'},
          summary: const SessionSummary(
            id: 'codex-thread:stale-runtime',
            title: '修复会话来源',
            external: true,
            runtime: RuntimeMeta(
              source: 'claude-native',
              engine: 'claude',
              command: 'claude --resume stale-runtime',
            ),
          ),
          runtimeAlive: true,
          resumeRuntimeMeta: const RuntimeMeta(
            source: 'claude-native',
            engine: 'claude',
            command: 'claude --resume stale-runtime',
            resumeSessionId: 'stale-runtime',
            claudeLifecycle: 'waiting_input',
          ),
        ),
      );
      await _flushEvents();

      expect(controller.selectedSessionTitle, '电脑 Codex');
      expect(controller.shouldShowClaudeMode, isFalse);
      expect(controller.isSessionReadOnly, isTrue);

      service.sentPayloads.clear();
      controller.continueSameSessionFromPhone();

      final resumes = service.sentPayloads
          .where((payload) => payload['action'] == 'session_resume')
          .toList();
      expect(resumes, hasLength(1));
      expect(resumes.single['engine'], 'codex');
      expect(resumes.single['codexSandboxMode'], 'danger-full-access');
      expect(resumes.single['permissionMode'], 'bypassPermissions');

      service.sentPayloads.clear();
      controller.sendInputText('继续处理');

      final turns = service.sentPayloads
          .where((payload) => payload['action'] == 'ai_turn')
          .toList();
      expect(turns, hasLength(1));
      expect(turns.single['engine'], 'codex');
    });

    test('外部 Codex 会话只有空白历史项时仍会补可见预览', () async {
      final service = _FakeMobileVcWsService();
      final controller = SessionController(service: service);
      await controller.initialize();
      addTearDown(controller.disposeController);

      service.emit(
        SessionHistoryEvent(
          timestamp: _timestamp,
          sessionId: 'codex-thread:2',
          runtimeMeta: const RuntimeMeta(),
          raw: const {'type': 'session_history'},
          summary: const SessionSummary(
            id: 'codex-thread:2',
            title: 'Desktop Codex Session',
            source: 'codex-native',
            external: true,
          ),
          logEntries: const [
            HistoryLogEntry(kind: 'system', message: ''),
          ],
          resumeRuntimeMeta: const RuntimeMeta(
            engine: 'codex',
            command: 'codex',
            resumeSessionId: 'thread-2',
          ),
        ),
      );
      await _flushEvents();

      expect(controller.timeline, hasLength(1));
      expect(controller.timeline.single.body, 'Desktop Codex Session');
    });

    test('会话列表结果不会覆盖已从历史提取出的最后一句用户输入', () async {
      final service = _FakeMobileVcWsService();
      final controller = SessionController(service: service);
      await controller.initialize();
      addTearDown(controller.disposeController);

      service.emit(
        SessionHistoryEvent(
          timestamp: _timestamp,
          sessionId: 'session-1',
          runtimeMeta: const RuntimeMeta(),
          raw: const {'type': 'session_history'},
          summary: const SessionSummary(
            id: 'session-1',
            title: 'session',
            lastPreview: '',
          ),
          logEntries: const [
            HistoryLogEntry(
              kind: 'user',
              text: '修一下登录页按钮间距',
              timestamp: '2026-04-01T10:00:00Z',
            ),
          ],
        ),
      );
      await _flushEvents();

      expect(controller.sessions.single.lastPreview, '修一下登录页按钮间距');

      service.emit(
        SessionListResultEvent(
          timestamp: _timestamp.add(const Duration(seconds: 1)),
          sessionId: 'session-1',
          runtimeMeta: const RuntimeMeta(),
          raw: const {'type': 'session_list_result'},
          items: const [
            SessionSummary(
              id: 'session-1',
              title: 'session',
              lastPreview: 'Codex gpt-5-codex -medium',
            ),
          ],
        ),
      );
      await _flushEvents();

      expect(controller.sessions.single.lastPreview, '修一下登录页按钮间距');
      expect(controller.sessions.single.title, 'session');
    });

    test('连续 codex markdown 日志会合并成单条回复', () async {
      final service = _FakeMobileVcWsService();
      final controller = SessionController(service: service);
      await controller.initialize();
      addTearDown(controller.disposeController);

      const meta = RuntimeMeta(
        command: 'codex',
        engine: 'codex',
        executionId: 'exec-codex-1',
        contextId: 'turn-1',
      );
      service.emit(
        LogEvent(
          timestamp: _timestamp,
          sessionId: 'session-1',
          runtimeMeta: meta,
          raw: const {'type': 'log'},
          message: '这是第一句。',
          stream: 'stdout',
        ),
      );
      service.emit(
        LogEvent(
          timestamp: _timestamp.add(const Duration(milliseconds: 120)),
          sessionId: 'session-1',
          runtimeMeta: meta,
          raw: const {'type': 'log'},
          message: '这是第二句。',
          stream: 'stdout',
        ),
      );
      await _flushEvents();

      final markdownItems =
          controller.timeline.where((item) => item.kind == 'markdown').toList();
      expect(markdownItems, hasLength(1));
      expect(markdownItems.single.body, '这是第一句。这是第二句。');
    });

    test('codex 简短文本回复不会再落到 terminal', () async {
      final service = _FakeMobileVcWsService();
      final controller = SessionController(service: service);
      await controller.initialize();
      addTearDown(controller.disposeController);

      service.emit(
        SessionCreatedEvent(
          timestamp: _timestamp,
          sessionId: 'session-1',
          runtimeMeta: const RuntimeMeta(),
          raw: const {'type': 'session_created'},
          summary: const SessionSummary(id: 'session-1', title: 'Session 1'),
        ),
      );
      await _flushEvents();

      service.emit(
        LogEvent(
          timestamp: _timestamp,
          sessionId: 'session-1',
          runtimeMeta: const RuntimeMeta(
            command: 'codex',
            engine: 'codex',
            executionId: 'exec-codex-short',
            contextId: 'turn-short',
          ),
          raw: const {'type': 'log'},
          message: '好的',
          stream: 'stdout',
        ),
      );
      await _flushEvents();

      final item =
          controller.timeline.singleWhere((entry) => entry.body == '好的');
      expect(item.kind, 'markdown');
    });

    test('metadata 不完整时 codex 短中文回复仍会显示到 timeline', () async {
      final service = _FakeMobileVcWsService();
      final controller = SessionController(service: service);
      await controller.initialize();
      addTearDown(controller.disposeController);

      service.emit(
        SessionStateEvent(
          timestamp: _timestamp,
          sessionId: 'session-1',
          runtimeMeta:
              const RuntimeMeta(executionId: 'exec-codex-short-fallback'),
          raw: const {'type': 'session_state'},
          state: 'RUNNING',
          message: 'codex running',
        ),
      );
      service.emit(
        LogEvent(
          timestamp: _timestamp.add(const Duration(seconds: 1)),
          sessionId: 'session-1',
          runtimeMeta:
              const RuntimeMeta(executionId: 'exec-codex-short-fallback'),
          raw: const {'type': 'log'},
          message: '你好',
          stream: 'stdout',
        ),
      );
      await _flushEvents();

      final item =
          controller.timeline.singleWhere((entry) => entry.body == '你好');
      expect(item.kind, 'markdown');
      expect(controller.isSessionBusy, isFalse);
    });

    test('短 waiting 文本不会被误判为 assistant 回复', () async {
      final service = _FakeMobileVcWsService();
      final controller = SessionController(service: service);
      await controller.initialize();
      addTearDown(controller.disposeController);

      service.emit(
        LogEvent(
          timestamp: _timestamp,
          sessionId: 'session-1',
          runtimeMeta: const RuntimeMeta(executionId: 'exec-waiting-short'),
          raw: const {'type': 'log'},
          message: '等待输入',
          stream: 'stdout',
        ),
      );
      await _flushEvents();

      expect(controller.timeline.where((item) => item.kind == 'markdown'),
          isEmpty);
    });

    test('短 terminal 风格文本不会被误判为 assistant 回复', () async {
      final service = _FakeMobileVcWsService();
      final controller = SessionController(service: service);
      await controller.initialize();
      addTearDown(controller.disposeController);

      service.emit(
        LogEvent(
          timestamp: _timestamp,
          sessionId: 'session-1',
          runtimeMeta: const RuntimeMeta(executionId: 'exec-terminal-short'),
          raw: const {'type': 'log'},
          message: 'a=b',
          stream: 'stdout',
        ),
      );
      await _flushEvents();

      expect(controller.timeline.where((item) => item.kind == 'markdown'),
          isEmpty);
    });

    test('带时间戳的 ws 结构化日志只保留在 terminal logs，不进入 timeline', () async {
      final service = _FakeMobileVcWsService();
      final controller = SessionController(service: service);
      await controller.initialize();
      addTearDown(controller.disposeController);

      service.emit(
        LogEvent(
          timestamp: _timestamp,
          sessionId: 'session-1',
          runtimeMeta: const RuntimeMeta(
            command: 'codex',
            engine: 'codex',
            executionId: 'exec-codex-ws-log',
            contextId: 'turn-ws-log',
          ),
          raw: const {'type': 'log'},
          message:
              '2026/04/04 15:12:25 [INFO][ws] incoming session_create: connectionID=conn-1 sessionID= remoteAddr=127.0.0.1:49396 title="review-session"',
          stream: 'stdout',
        ),
      );
      await _flushEvents();

      expect(controller.timeline, isEmpty);
      expect(controller.terminalStdout,
          contains('[INFO][ws] incoming session_create'));
    });

    test('codex 启动握手会清洗成正常招呼，不展示 reasoning effort 回显', () async {
      final service = _FakeMobileVcWsService();
      final controller = SessionController(service: service);
      await controller.initialize();
      addTearDown(controller.disposeController);

      service.emit(
        LogEvent(
          timestamp: _timestamp,
          sessionId: 'session-1',
          runtimeMeta: const RuntimeMeta(
            command: 'codex',
            engine: 'codex',
            executionId: 'exec-codex-start-1',
            contextId: 'turn-start-1',
          ),
          raw: const {'type': 'log'},
          message: 'Reasoning effort set to medium. How can I help you?',
          stream: 'stdout',
        ),
      );
      await _flushEvents();

      final item = controller.timeline.singleWhere(
        (entry) => entry.kind == 'markdown',
      );
      expect(item.body, 'How can I help you?');
    });

    test('英文 markdown 分片合并时会补句间空格', () async {
      final service = _FakeMobileVcWsService();
      final controller = SessionController(service: service);
      await controller.initialize();
      addTearDown(controller.disposeController);

      const meta = RuntimeMeta(
        command: 'codex',
        engine: 'codex',
        executionId: 'exec-codex-2',
        contextId: 'turn-2',
      );
      service.emit(
        LogEvent(
          timestamp: _timestamp,
          sessionId: 'session-1',
          runtimeMeta: meta,
          raw: const {'type': 'log'},
          message: 'First sentence explains the current issue clearly.',
          stream: 'stdout',
        ),
      );
      service.emit(
        LogEvent(
          timestamp: _timestamp.add(const Duration(milliseconds: 180)),
          sessionId: 'session-1',
          runtimeMeta: meta,
          raw: const {'type': 'log'},
          message: 'Second sentence describes the expected fix path.',
          stream: 'stdout',
        ),
      );
      await _flushEvents();

      final markdownItems =
          controller.timeline.where((item) => item.kind == 'markdown').toList();
      expect(markdownItems, hasLength(1));
      expect(
        markdownItems.single.body,
        'First sentence explains the current issue clearly. Second sentence describes the expected fix path.',
      );
    });

    test('codex 混合 stdout 分片会合并成单条回复并刷新通知预览', () async {
      final service = _FakeMobileVcWsService();
      final controller = SessionController(service: service);
      await controller.initialize();
      addTearDown(controller.disposeController);

      const meta = RuntimeMeta(
        command: 'codex',
        engine: 'codex',
        executionId: 'exec-codex-mixed-1',
        contextId: 'turn-mixed-1',
      );
      service.emit(
        LogEvent(
          timestamp: _timestamp,
          sessionId: 'session-1',
          runtimeMeta: meta,
          raw: const {'type': 'log'},
          message: '**Code Updates**',
          stream: 'stdout',
        ),
      );
      service.emit(
        LogEvent(
          timestamp: _timestamp.add(const Duration(milliseconds: 120)),
          sessionId: 'session-1',
          runtimeMeta: meta,
          raw: const {'type': 'log'},
          message: '- Added permission fix.',
          stream: 'stdout',
        ),
      );
      service.emit(
        LogEvent(
          timestamp: _timestamp.add(const Duration(milliseconds: 240)),
          sessionId: 'session-1',
          runtimeMeta: meta,
          raw: const {'type': 'log'},
          message: 'Push completed successfully.',
          stream: 'stdout',
        ),
      );
      await _flushEvents();

      expect(controller.timeline, hasLength(1));
      expect(controller.timeline.single.kind, 'markdown');
      expect(
        controller.timeline.single.body,
        '**Code Updates**\n- Added permission fix. Push completed successfully.',
      );
      expect(
        controller.notificationSignal?.body,
        '**Code Updates** - Added permission fix. Push completed successfully.',
      );
    });

    test('codex 短 delta 分片第一块会立即进入 timeline', () async {
      final service = _FakeMobileVcWsService();
      final controller = SessionController(service: service);
      await controller.initialize();
      addTearDown(controller.disposeController);

      const meta = RuntimeMeta(
        command: 'codex',
        engine: 'codex',
        source: 'codex/assistant',
        executionId: 'turn-short-delta-1',
      );
      service.emit(LogEvent(
        timestamp: _timestamp,
        sessionId: 'session-1',
        runtimeMeta: meta,
        raw: const {'type': 'log'},
        message: 'Tip :',
        stream: 'stdout',
      ));
      await _flushEvents();

      expect(controller.timeline, hasLength(1));
      expect(controller.timeline.single.kind, 'markdown');
      expect(controller.timeline.single.body, 'Tip :');

      service.emit(LogEvent(
        timestamp: _timestamp.add(const Duration(milliseconds: 120)),
        sessionId: 'session-1',
        runtimeMeta: meta,
        raw: const {'type': 'log'},
        message: 'hello world',
        stream: 'stdout',
      ));
      await _flushEvents();

      expect(controller.timeline, hasLength(1));
      expect(controller.timeline.single.body, 'Tip : hello world');
    });

    test('codex 最终完整回复会覆盖已合并流式片段而不是重复拼接', () async {
      final service = _FakeMobileVcWsService();
      final controller = SessionController(service: service);
      await controller.initialize();
      addTearDown(controller.disposeController);

      const meta = RuntimeMeta(
        command: 'codex',
        engine: 'codex',
        executionId: 'exec-codex-final-1',
        contextId: 'turn-final-1',
      );
      service.emit(
        LogEvent(
          timestamp: _timestamp,
          sessionId: 'session-1',
          runtimeMeta: meta,
          raw: const {'type': 'log'},
          message: '已修复 relay 重连问题，',
          stream: 'stdout',
        ),
      );
      service.emit(
        LogEvent(
          timestamp: _timestamp.add(const Duration(milliseconds: 120)),
          sessionId: 'session-1',
          runtimeMeta: meta,
          raw: const {'type': 'log'},
          message: '并补充了回归测试。',
          stream: 'stdout',
        ),
      );
      service.emit(
        LogEvent(
          timestamp: _timestamp.add(const Duration(milliseconds: 240)),
          sessionId: 'session-1',
          runtimeMeta: meta,
          raw: const {'type': 'log'},
          message: '已修复 relay 重连问题，并补充了回归测试。',
          stream: 'stdout',
        ),
      );
      await _flushEvents();

      final markdownItems =
          controller.timeline.where((item) => item.kind == 'markdown').toList();
      expect(markdownItems, hasLength(1));
      expect(markdownItems.single.body, '已修复 relay 重连问题，并补充了回归测试。');
    });

    test('session_delta 回放已实时显示的完整 assistant 回复时不会重复拼接', () async {
      final service = _FakeMobileVcWsService();
      final controller = SessionController(service: service);
      await controller.initialize();
      addTearDown(controller.disposeController);

      const meta = RuntimeMeta(
        command: 'codex',
        engine: 'codex',
        executionId: 'exec-codex-delta-final-1',
        contextId: 'turn-delta-final-1',
      );
      await controller.connect();
      service.emit(SessionCreatedEvent(
        timestamp: _timestamp,
        sessionId: 'conn-1',
        runtimeMeta: meta,
        raw: const {'type': 'session_created'},
        summary: const SessionSummary(id: 'session-1', title: 'Session 1'),
      ));
      await _flushEvents();

      service.emit(LogEvent(
        timestamp: _timestamp.add(const Duration(milliseconds: 100)),
        sessionId: 'session-1',
        runtimeMeta: meta,
        raw: const {'type': 'log'},
        message: '手机发送后页面刷新慢，',
        stream: 'stdout',
      ));
      service.emit(LogEvent(
        timestamp: _timestamp.add(const Duration(milliseconds: 200)),
        sessionId: 'session-1',
        runtimeMeta: meta,
        raw: const {'type': 'log'},
        message: '并且回复重复显示。',
        stream: 'stdout',
      ));
      await _flushEvents();

      service.emit(SessionDeltaEvent(
        timestamp: _timestamp.add(const Duration(milliseconds: 800)),
        sessionId: 'session-1',
        runtimeMeta: meta,
        raw: const {'type': 'session_delta'},
        summary: const SessionSummary(id: 'session-1', title: 'Session 1'),
        base: const SessionDeltaKnown(eventCursor: 1, logEntryCount: 0),
        latest: const SessionDeltaKnown(eventCursor: 2, logEntryCount: 1),
        appendLogEntries: const [
          HistoryLogEntry(
            kind: 'markdown',
            message: '手机发送后页面刷新慢，并且回复重复显示。',
            timestamp: '2026-05-27T05:53:00Z',
            executionId: 'exec-codex-delta-final-1',
            context: HistoryContext(
              id: 'turn-delta-final-1',
              source: 'codex',
              type: 'assistant',
            ),
          ),
        ],
        resumeRuntimeMeta: meta,
      ));
      await _flushEvents();

      final markdownItems =
          controller.timeline.where((item) => item.kind == 'markdown').toList();
      expect(markdownItems, hasLength(1));
      expect(markdownItems.single.body, '手机发送后页面刷新慢，并且回复重复显示。');
    });

    test('history 已恢复完整 assistant 回复后会吸收后到的同源 live 尾段', () async {
      final service = _FakeMobileVcWsService();
      final controller = SessionController(service: service);
      await controller.initialize();
      addTearDown(controller.disposeController);

      const meta = RuntimeMeta(
        command: 'codex',
        engine: 'codex',
        executionId: 'exec-codex-restored-tail-1',
        contextId: 'turn-restored-tail-1',
      );
      const fullReply = '''
修改内容：

- 修改了 mobile_vc/lib/features/session/session_controller.dart
- 修改了 mobile_vc/test/session_controller_test.dart
- 统计：283 insertions(+), 16 deletions(-)

当前工作区是干净的，没有还能再提交的变更。
Flutter 打出来的 app-release.apk 在 mobile_vc/build/ 下，是 ignored 构建产物，不会被正常 Git commit。''';
      const tailReplay = '''
- 统计：283 insertions(+), 16 deletions(-)

当前工作区是干净的，没有还能再提交的变更。
Flutter 打出来的 app-release.apk 在 mobile_vc/build/ 下，是 ignored 构建产物，不会被正常 Git commit。''';

      await controller.connect();
      service.emit(SessionHistoryEvent(
        timestamp: _timestamp,
        sessionId: 'session-1',
        runtimeMeta: meta,
        raw: const {'type': 'session_history'},
        summary: const SessionSummary(id: 'session-1', title: 'Session 1'),
        logEntries: const [
          HistoryLogEntry(
            kind: 'markdown',
            message: fullReply,
            timestamp: '2026-06-06T15:39:00Z',
            executionId: 'exec-codex-restored-tail-1',
            context: HistoryContext(
              id: 'turn-restored-tail-1',
              source: 'codex',
              type: 'assistant',
            ),
          ),
        ],
        resumeRuntimeMeta: meta,
      ));
      await _flushEvents();

      service.emit(LogEvent(
        timestamp: _timestamp.add(const Duration(milliseconds: 500)),
        sessionId: 'session-1',
        runtimeMeta: meta,
        raw: const {'type': 'log'},
        message: tailReplay,
        stream: 'stdout',
      ));
      await _flushEvents();

      final markdownItems =
          controller.timeline.where((item) => item.kind == 'markdown').toList();
      expect(markdownItems, hasLength(1));
      expect(markdownItems.single.body, fullReply);
    });

    test('codex 单行总结式回复不会再被误判为 terminal 输出', () async {
      final service = _FakeMobileVcWsService();
      final controller = SessionController(service: service);
      await controller.initialize();
      addTearDown(controller.disposeController);

      service.emit(
        LogEvent(
          timestamp: _timestamp,
          sessionId: 'session-1',
          runtimeMeta: const RuntimeMeta(
            command: 'codex',
            engine: 'codex',
            executionId: 'exec-codex-summary-1',
            contextId: 'turn-summary-1',
          ),
          raw: const {'type': 'log'},
          message: 'Summary: fixed the missing reply rendering in Flutter.',
          stream: 'stdout',
        ),
      );
      await _flushEvents();

      final item = controller.timeline.singleWhere(
        (entry) =>
            entry.kind == 'markdown' &&
            entry.body ==
                'Summary: fixed the missing reply rendering in Flutter.',
      );
      expect(item.kind, 'markdown');
      expect(
        controller.terminalStdout.contains(
          'Summary: fixed the missing reply rendering in Flutter.',
        ),
        isFalse,
      );
    });

    test('codex 极短 OK 回复会进入 timeline', () async {
      final service = _FakeMobileVcWsService();
      final controller = SessionController(service: service);
      await controller.initialize();
      addTearDown(controller.disposeController);

      service.emit(
        LogEvent(
          timestamp: _timestamp,
          sessionId: 'session-1',
          runtimeMeta: const RuntimeMeta(
            command: 'codex',
            engine: 'codex',
            executionId: 'exec-codex-ok-1',
            contextId: 'turn-ok-1',
          ),
          raw: const {'type': 'log'},
          message: 'OK',
          stream: 'stdout',
        ),
      );
      await _flushEvents();

      expect(controller.timeline, hasLength(1));
      expect(controller.timeline.single.kind, 'markdown');
      expect(controller.timeline.single.body, 'OK');
    });

    test('ws_not_connected 不展示为红色 timeline 错误', () async {
      final service = _FakeMobileVcWsService();
      final controller = SessionController(service: service);
      await controller.initialize();
      addTearDown(controller.disposeController);

      service.emit(
        ErrorEvent(
          timestamp: _timestamp,
          sessionId: '',
          runtimeMeta: const RuntimeMeta(),
          raw: const {'type': 'error'},
          code: 'ws_not_connected',
          message: 'WebSocket 未连接',
        ),
      );
      await _flushEvents();

      expect(controller.latestError, isNull);
      expect(controller.timeline, isEmpty);
    });

    test('权限交接中的 signal killed 噪声不会进入 timeline', () async {
      final service = _FakeMobileVcWsService();
      final controller = SessionController(service: service);
      await controller.initialize();
      addTearDown(controller.disposeController);

      await controller.connect();
      service.emit(
        RuntimePhaseEvent(
          timestamp: _timestamp,
          sessionId: 'session-1',
          runtimeMeta: const RuntimeMeta(command: 'claude'),
          raw: const {'type': 'runtime_phase'},
          phase: 'permission_blocked',
          kind: 'permission',
          message: 'Allow edit a.dart?',
        ),
      );
      service.emit(
        SessionStateEvent(
          timestamp: _timestamp,
          sessionId: 'session-1',
          runtimeMeta: const RuntimeMeta(permissionMode: 'acceptEdits'),
          raw: const {'type': 'session_state'},
          state: 'closed',
          message: 'command finished with error',
        ),
      );
      service.emit(
        ErrorEvent(
          timestamp: _timestamp,
          sessionId: 'session-1',
          runtimeMeta: const RuntimeMeta(permissionMode: 'acceptEdits'),
          raw: const {'type': 'error', 'msg': 'signal: killed'},
          message: 'signal: killed',
        ),
      );
      await _flushEvents();

      expect(
          controller.timeline
              .any((item) => item.body.contains('signal: killed')),
          isFalse);
      expect(
          controller.timeline
              .any((item) => item.body.contains('command finished with error')),
          isFalse);
    });

    test('codex 工具噪声仅保留在 terminal logs，不进入 timeline', () async {
      final service = _FakeMobileVcWsService();
      final controller = SessionController(service: service);
      await controller.initialize();
      addTearDown(controller.disposeController);

      await controller.connect();
      service.emit(
        LogEvent(
          timestamp: _timestamp,
          sessionId: 'session-1',
          runtimeMeta: const RuntimeMeta(command: 'codex', engine: 'codex'),
          raw: const {'type': 'log'},
          message:
              '2026-03-31T18:15:54.641890Z ERROR codex_core::tools::router: error=Exit code: 128',
          stream: 'stderr',
        ),
      );
      service.emit(
        LogEvent(
          timestamp: _timestamp.add(const Duration(milliseconds: 120)),
          sessionId: 'session-1',
          runtimeMeta: const RuntimeMeta(command: 'codex', engine: 'codex'),
          raw: const {'type': 'log'},
          message:
              'Wall time: 0 seconds\nOutput:\nfatal: not a git repository (or any of the parent directories): .git',
          stream: 'stderr',
        ),
      );
      service.emit(
        LogEvent(
          timestamp: _timestamp.add(const Duration(milliseconds: 240)),
          sessionId: 'session-1',
          runtimeMeta: const RuntimeMeta(command: 'codex', engine: 'codex'),
          raw: const {'type': 'log'},
          message:
              'Wall time: 0 seconds\nOutput:\ncat: .gitmodules: No such file or directory',
          stream: 'stderr',
        ),
      );
      service.emit(
        LogEvent(
          timestamp: _timestamp.add(const Duration(milliseconds: 360)),
          sessionId: 'session-1',
          runtimeMeta: const RuntimeMeta(command: 'codex', engine: 'codex'),
          raw: const {'type': 'log'},
          message:
              "Output:\nfatal: no submodule mapping found in .gitmodules for path '.claude/worktrees/agent-a0055fcc'",
          stream: 'stderr',
        ),
      );
      service.emit(
        LogEvent(
          timestamp: _timestamp.add(const Duration(milliseconds: 480)),
          sessionId: 'session-1',
          runtimeMeta: const RuntimeMeta(command: 'codex', engine: 'codex'),
          raw: const {'type': 'log'},
          message: 'Output',
          stream: 'stderr',
        ),
      );
      service.emit(
        LogEvent(
          timestamp: _timestamp.add(const Duration(milliseconds: 600)),
          sessionId: 'session-1',
          runtimeMeta: const RuntimeMeta(command: 'codex', engine: 'codex'),
          raw: const {'type': 'log'},
          message:
              "Output:\nfatal: Unable to create '/Users/wust_lh/MobileVC/.git/index.lock': File exists",
          stream: 'stderr',
        ),
      );
      await _flushEvents();

      expect(controller.terminalStderr, contains('codex_core::tools::router'));
      expect(
          controller.terminalStderr, contains('fatal: not a git repository'));
      expect(controller.terminalStderr,
          contains('cat: .gitmodules: No such file or directory'));
      expect(controller.terminalStderr,
          contains('no submodule mapping found in .gitmodules'));
      expect(
          controller.terminalStderr,
          contains(
              "fatal: Unable to create '/Users/wust_lh/MobileVC/.git/index.lock': File exists"));
      expect(
        controller.timeline
            .any((item) => item.body.contains('codex_core::tools::router')),
        isFalse,
      );
      expect(
        controller.timeline
            .any((item) => item.body.contains('fatal: not a git repository')),
        isFalse,
      );
      expect(
        controller.timeline.any((item) => item.body.contains('Wall time:')),
        isFalse,
      );
      expect(
        controller.timeline.any((item) => item.body.contains('.gitmodules')),
        isFalse,
      );
      expect(
        controller.timeline
            .any((item) => item.body.trim().toLowerCase() == 'output'),
        isFalse,
      );
      expect(
        controller.timeline.any((item) => item.body.contains('Output:')),
        isFalse,
      );
      expect(
        controller.timeline.any((item) => item.body.contains(
            "fatal: Unable to create '/Users/wust_lh/MobileVC/.git/index.lock': File exists")),
        isFalse,
      );
    });

    test('手动 loadSession 仍能恢复历史 timeline / diff / session meta / terminal logs',
        () async {
      final service = _FakeMobileVcWsService();
      final controller = SessionController(service: service);
      await controller.initialize();
      addTearDown(controller.disposeController);

      controller.loadSession('session-history');
      service.sentPayloads.clear();

      service.emit(
        SessionHistoryEvent(
          timestamp: _timestamp,
          sessionId: 'session-history',
          runtimeMeta: const RuntimeMeta(),
          raw: const {'type': 'session_history'},
          summary: const SessionSummary(id: 'session-history', title: '历史会话'),
          logEntries: const [
            HistoryLogEntry(
                kind: 'assistant', message: '旧回复', label: 'Assistant'),
          ],
          diffs: const [
            HistoryContext(
              id: 'diff-1',
              type: 'diff',
              path: '/workspace/README.md',
              title: 'README.md',
              diff: '@@ -1 +1 @@',
              pendingReview: true,
            ),
          ],
          currentStep: const HistoryContext(
            id: 'step-1',
            type: 'step',
            title: '恢复中',
            message: '历史步骤',
          ),
          rawTerminalByStream: const {
            'stdout': 'global stdout',
            'stderr': 'global stderr',
          },
          terminalExecutions: [
            TerminalExecution(
              executionId: 'exec-1',
              command: 'npm test',
              cwd: '/workspace/app',
              source: 'user',
              sourceLabel: '用户输入',
              stdout: 'exec-1 stdout',
              stderr: 'exec-1 stderr',
              completedAt: DateTime(2026, 3, 28, 18, 0, 5),
              exitCode: 0,
            ),
            TerminalExecution(
              executionId: 'exec-2',
              command: 'flutter test',
              cwd: '/workspace/mobile_vc',
              source: 'review-follow-up',
              sourceLabel: '审核后续',
              stdout: 'exec-2 stdout',
              stderr: 'exec-2 stderr',
              running: true,
            ),
          ],
          resumeRuntimeMeta: const RuntimeMeta(
            command: 'claude --resume session-history',
            permissionMode: 'acceptEdits',
            claudeLifecycle: 'resumable',
          ),
        ),
      );
      await _flushEvents();

      expect(controller.selectedSessionId, 'session-history');
      expect(controller.timeline.any((item) => item.body == '旧回复'), isTrue);
      expect(controller.recentDiffs, hasLength(1));
      expect(controller.currentStepSummary, '历史步骤');
      expect(controller.displayPermissionMode, 'auto');
      expect(controller.terminalExecutions, hasLength(2));
      expect(controller.terminalExecutions.first.completedAt, isNotNull);
      expect(controller.terminalExecutions.first.running, isFalse);
      expect(controller.activeTerminalExecutionId, 'exec-2');
      expect(controller.activeTerminalStdout, 'exec-2 stdout');
      expect(controller.activeTerminalStderr, 'exec-2 stderr');
      expect(controller.terminalStdout, 'global stdout');
      expect(controller.terminalStderr, 'global stderr');

      controller.setActiveTerminalExecution('exec-1');
      expect(controller.activeTerminalExecutionId, 'exec-1');
      expect(controller.activeTerminalStdout, 'exec-1 stdout');
      expect(controller.activeTerminalStderr, 'exec-1 stderr');
    });

    test('runtime_process_list 会自动选中进程并请求日志', () async {
      final service = _FakeMobileVcWsService();
      final controller = SessionController(service: service);
      await controller.initialize();
      addTearDown(controller.disposeController);

      await controller.connect();
      service.sentPayloads.clear();

      controller.requestRuntimeProcessList();
      expect(controller.runtimeProcessListLoading, isTrue);
      expect(
        service.sentPayloads.last,
        containsPair('action', 'runtime_process_list'),
      );

      service.emit(
        RuntimeProcessListResultEvent(
          timestamp: _timestamp,
          sessionId: 'session-1',
          runtimeMeta: const RuntimeMeta(),
          raw: const {'type': 'runtime_process_list_result'},
          rootPid: 101,
          items: const [
            RuntimeProcessItem(
              pid: 101,
              ppid: 1,
              state: 'Ss',
              elapsed: '00:12',
              command: 'bash -lc codex',
              cwd: '/workspace',
              executionId: 'exec-101',
              source: 'codex',
              root: true,
              logAvailable: true,
            ),
            RuntimeProcessItem(
              pid: 202,
              ppid: 101,
              state: 'S+',
              elapsed: '00:03',
              command: 'ps -axo',
            ),
          ],
        ),
      );
      await _flushEvents();

      expect(controller.runtimeProcesses, hasLength(2));
      expect(controller.activeRuntimeProcessPid, 101);
      expect(controller.runtimeProcessListLoading, isFalse);
      expect(controller.runtimeProcessLogLoading, isTrue);
      expect(
        service.sentPayloads.last,
        containsPair('action', 'runtime_process_log_get'),
      );
      expect(service.sentPayloads.last['pid'], 101);

      service.emit(
        RuntimeProcessLogResultEvent(
          timestamp: _timestamp,
          sessionId: 'session-1',
          runtimeMeta: const RuntimeMeta(),
          raw: const {'type': 'runtime_process_log_result'},
          pid: 101,
          executionId: 'exec-101',
          command: 'bash -lc codex',
          cwd: '/workspace',
          source: 'codex',
          stdout: 'process stdout',
          stderr: 'process stderr',
        ),
      );
      await _flushEvents();

      expect(controller.runtimeProcessLogLoading, isFalse);
      expect(controller.activeRuntimeProcessStdout, 'process stdout');
      expect(controller.activeRuntimeProcessStderr, 'process stderr');

      controller.setActiveRuntimeProcess(202);
      expect(controller.activeRuntimeProcessPid, 202);
      expect(
        service.sentPayloads.last,
        containsPair('action', 'runtime_process_log_get'),
      );
      expect(service.sentPayloads.last['pid'], 202);
    });

    test('runtime_process_log_result 不可用消息不会渲染为错误 timeline', () async {
      final service = _FakeMobileVcWsService();
      final controller = SessionController(service: service);
      await controller.initialize();
      addTearDown(controller.disposeController);

      await controller.connect();
      service.sentPayloads.clear();

      service.emit(
        RuntimeProcessListResultEvent(
          timestamp: _timestamp,
          sessionId: 'session-1',
          runtimeMeta: const RuntimeMeta(),
          raw: const {'type': 'runtime_process_list_result'},
          rootPid: 101,
          items: const [
            RuntimeProcessItem(
              pid: 101,
              ppid: 1,
              state: 'Ss',
              elapsed: '00:12',
              command: 'codex exec',
              cwd: '/workspace',
              executionId: 'exec-missing',
              source: 'codex',
              root: true,
              logAvailable: true,
            ),
          ],
        ),
      );
      await _flushEvents();

      expect(controller.runtimeProcessLogLoading, isTrue);
      service.emit(
        RuntimeProcessLogResultEvent(
          timestamp: _timestamp,
          sessionId: 'session-1',
          runtimeMeta: const RuntimeMeta(),
          raw: const {'type': 'runtime_process_log_result'},
          pid: 101,
          executionId: 'exec-missing',
          command: 'codex exec',
          cwd: '/workspace',
          source: 'codex',
          message: '该进程没有可用的终端执行日志',
        ),
      );
      await _flushEvents();

      expect(controller.runtimeProcessLogLoading, isFalse);
      expect(controller.activeRuntimeProcessPid, 101);
      expect(controller.activeRuntimeProcessStdout, isEmpty);
      expect(controller.activeRuntimeProcessStderr, isEmpty);
      expect(
        controller.activeRuntimeProcessMessage,
        '该进程没有可用的终端执行日志',
      );
      expect(
          controller.timeline.where((item) => item.kind == 'error'), isEmpty);
    });

    test('session_history 会清空旧的 runtime process 状态', () async {
      final service = _FakeMobileVcWsService();
      final controller = SessionController(service: service);
      await controller.initialize();
      addTearDown(controller.disposeController);

      await controller.connect();

      service.emit(
        RuntimeProcessListResultEvent(
          timestamp: _timestamp,
          sessionId: 'session-1',
          runtimeMeta: const RuntimeMeta(),
          raw: const {'type': 'runtime_process_list_result'},
          rootPid: 101,
          items: const [
            RuntimeProcessItem(
              pid: 101,
              ppid: 1,
              state: 'Ss',
              elapsed: '00:12',
              command: 'bash -lc codex',
              executionId: 'exec-101',
              root: true,
              logAvailable: true,
            ),
          ],
        ),
      );
      service.emit(
        RuntimeProcessLogResultEvent(
          timestamp: _timestamp,
          sessionId: 'session-1',
          runtimeMeta: const RuntimeMeta(),
          raw: const {'type': 'runtime_process_log_result'},
          pid: 101,
          executionId: 'exec-101',
          stdout: 'process stdout',
        ),
      );
      await _flushEvents();

      expect(controller.runtimeProcesses, hasLength(1));
      expect(controller.activeRuntimeProcessPid, 101);
      expect(controller.activeRuntimeProcessLog, isNotNull);

      service.emit(
        SessionHistoryEvent(
          timestamp: _timestamp,
          sessionId: 'session-history',
          runtimeMeta: const RuntimeMeta(),
          raw: const {'type': 'session_history'},
          summary: const SessionSummary(id: 'session-history', title: '历史会话'),
          rawTerminalByStream: const {'stdout': '', 'stderr': ''},
        ),
      );
      await _flushEvents();

      expect(controller.runtimeProcesses, isEmpty);
      expect(controller.activeRuntimeProcessPid, 0);
      expect(controller.activeRuntimeProcessLog, isNull);
      expect(controller.runtimeProcessListLoading, isFalse);
      expect(controller.runtimeProcessLogLoading, isFalse);
    });

    test('[debug] 调试信息不会进入 timeline，但 system/error 仍保留', () async {
      final service = _FakeMobileVcWsService();
      final controller = SessionController(service: service);
      await controller.initialize();
      addTearDown(controller.disposeController);

      await controller.connect();
      service.emit(
        PromptRequestEvent(
          timestamp: _timestamp,
          sessionId: 'session-1',
          runtimeMeta: const RuntimeMeta(command: 'claude'),
          raw: const {'type': 'prompt_request', 'msg': 'Allow write?'},
          message: 'Allow write?',
          options: const [PromptOption(value: 'y'), PromptOption(value: 'n')],
        ),
      );
      service.emit(
        ErrorEvent(
          timestamp: _timestamp,
          sessionId: 'session-1',
          runtimeMeta: const RuntimeMeta(),
          raw: const {'type': 'error', 'msg': 'boom'},
          message: 'boom',
        ),
      );
      await _flushEvents();

      expect(
        controller.timeline
            .any((item) => item.body.trim().startsWith('[debug]')),
        isFalse,
      );
      expect(
        controller.timeline
            .any((item) => item.kind == 'error' && item.body == 'boom'),
        isTrue,
      );
    });

    test('消费新的 catalog sync 事件并维护 skill 元数据', () async {
      final service = _FakeMobileVcWsService();
      final controller = SessionController(service: service);
      await controller.initialize();
      addTearDown(controller.disposeController);

      service.emit(
        SkillSyncResultEvent(
          timestamp: _timestamp,
          sessionId: 'session-1',
          runtimeMeta: const RuntimeMeta(),
          raw: const {'type': 'skill_sync_result'},
          message: 'skill 同步完成',
        ),
      );
      service.emit(
        CatalogSyncStatusEvent(
          timestamp: _timestamp,
          sessionId: 'session-1',
          runtimeMeta: const RuntimeMeta(),
          raw: const {'type': 'catalog_sync_status', 'domain': 'skill'},
          domain: 'skill',
          meta: const CatalogMetadata(
            domain: 'skill',
            sourceOfTruth: 'claude',
            syncState: 'syncing',
          ),
        ),
      );
      service.emit(
        SkillCatalogResultEvent(
          timestamp: _timestamp,
          sessionId: 'session-1',
          runtimeMeta: const RuntimeMeta(),
          raw: const {'type': 'skill_catalog_result'},
          meta: const CatalogMetadata(
            domain: 'skill',
            sourceOfTruth: 'claude',
            syncState: 'synced',
            driftDetected: false,
          ),
          items: const [
            SkillDefinition(
              name: 'external-diff-summary',
              source: 'external',
              sourceOfTruth: 'claude',
              syncState: 'synced',
            ),
          ],
        ),
      );
      service.emit(
        CatalogSyncResultEvent(
          timestamp: _timestamp,
          sessionId: 'session-1',
          runtimeMeta: const RuntimeMeta(),
          raw: const {'type': 'catalog_sync_result', 'domain': 'skill'},
          domain: 'skill',
          success: true,
          message: 'skill 同步完成',
          meta: const CatalogMetadata(
            domain: 'skill',
            sourceOfTruth: 'claude',
            syncState: 'synced',
          ),
        ),
      );
      await _flushEvents();

      expect(controller.skillCatalogMeta.syncState, 'synced');
      expect(controller.skillCatalogMeta.sourceOfTruth, 'claude');
      expect(controller.skillSyncStatus, 'skill 同步完成');
      expect(controller.skills.single.syncState, 'synced');
      expect(controller.skills.single.sourceOfTruth, 'claude');
      expect(
        controller.timeline
            .where((item) => item.body.trim() == 'skill 同步完成')
            .length,
        1,
      );
    });

    test('消费新的 catalog sync 事件并维护 memory 元数据', () async {
      final service = _FakeMobileVcWsService();
      final controller = SessionController(service: service);
      await controller.initialize();
      addTearDown(controller.disposeController);

      service.emit(
        CatalogSyncStatusEvent(
          timestamp: _timestamp,
          sessionId: 'session-1',
          runtimeMeta: const RuntimeMeta(),
          raw: const {'type': 'catalog_sync_status', 'domain': 'memory'},
          domain: 'memory',
          meta: const CatalogMetadata(
            domain: 'memory',
            sourceOfTruth: 'claude',
            syncState: 'syncing',
          ),
        ),
      );
      service.emit(
        MemoryListResultEvent(
          timestamp: _timestamp,
          sessionId: 'session-1',
          runtimeMeta: const RuntimeMeta(),
          raw: const {'type': 'memory_list_result'},
          meta: const CatalogMetadata(
            domain: 'memory',
            sourceOfTruth: 'claude',
            syncState: 'synced',
            driftDetected: false,
          ),
          items: const [
            MemoryItem(
              id: 'mem-1',
              title: 'Memory 1',
              source: 'external',
              sourceOfTruth: 'claude',
              syncState: 'synced',
            ),
          ],
        ),
      );
      service.emit(
        CatalogSyncResultEvent(
          timestamp: _timestamp,
          sessionId: 'session-1',
          runtimeMeta: const RuntimeMeta(),
          raw: const {'type': 'catalog_sync_result', 'domain': 'memory'},
          domain: 'memory',
          success: true,
          message: 'memory 同步完成',
          meta: const CatalogMetadata(
            domain: 'memory',
            sourceOfTruth: 'claude',
            syncState: 'synced',
          ),
        ),
      );
      await _flushEvents();

      expect(controller.memoryCatalogMeta.syncState, 'synced');
      expect(controller.memoryCatalogMeta.sourceOfTruth, 'claude');
      expect(controller.memorySyncStatus, 'memory 同步完成');
      expect(controller.memoryItems.single.syncState, 'synced');
      expect(controller.memoryItems.single.sourceOfTruth, 'claude');
    });

    test('memory 列表与 session enabled 态分离维护', () async {
      final service = _FakeMobileVcWsService();
      final controller = SessionController(service: service);
      await controller.initialize();
      addTearDown(controller.disposeController);

      service.emit(
        MemoryListResultEvent(
          timestamp: _timestamp,
          sessionId: 'session-1',
          runtimeMeta: const RuntimeMeta(),
          raw: const {'type': 'memory_list_result'},
          meta: const CatalogMetadata(
            domain: 'memory',
            sourceOfTruth: 'claude',
            syncState: 'draft',
            driftDetected: true,
          ),
          items: const [
            MemoryItem(
              id: 'mem-1',
              title: 'Memory 1',
              source: 'local',
              sourceOfTruth: 'claude',
              syncState: 'draft',
              driftDetected: true,
            ),
          ],
        ),
      );
      service.emit(
        SessionContextResultEvent(
          timestamp: _timestamp,
          sessionId: 'session-1',
          runtimeMeta: const RuntimeMeta(),
          raw: const {'type': 'session_context_result'},
          sessionContext: const SessionContext(enabledMemoryIds: ['mem-1']),
        ),
      );
      await _flushEvents();

      expect(controller.memoryCatalogMeta.syncState, 'draft');
      expect(controller.memoryCatalogMeta.driftDetected, true);
      expect(controller.memoryItems.single.syncState, 'draft');
      expect(controller.sessionContext.enabledMemoryIds, ['mem-1']);
    });

    test('syncMemories 改为真实 memory_sync_pull 请求', () async {
      final service = _FakeMobileVcWsService();
      final controller = SessionController(service: service);
      await controller.initialize();
      addTearDown(controller.disposeController);

      controller.syncMemories();

      expect(service.sentPayloads, hasLength(1));
      expect(service.sentPayloads.single['action'], 'memory_sync_pull');
      expect(service.sentPayloads.single['cwd'], '.');
    });

    test('长 assistant 回复在实时日志阶段立即进入 timeline', () async {
      final service = _FakeMobileVcWsService();
      final controller = SessionController(service: service);
      await controller.initialize();
      addTearDown(controller.disposeController);

      await controller.connect();
      service.emit(
        SessionStateEvent(
          timestamp: _timestamp,
          sessionId: 'session-1',
          runtimeMeta: const RuntimeMeta(
            command: 'claude',
            engine: 'claude',
            executionId: 'exec-long-1',
          ),
          raw: const {'type': 'session_state'},
          state: 'RUNNING',
          message: 'claude running',
        ),
      );
      await _flushEvents();

      service.emit(
        LogEvent(
          timestamp: _timestamp.add(const Duration(seconds: 1)),
          sessionId: 'session-1',
          runtimeMeta: const RuntimeMeta(executionId: 'exec-long-1'),
          raw: const {'type': 'log'},
          message: '结论先说：这个问题我已经定位到实时展示链路了。\n\n接下来我会把根因和修复方案一起整理给你。',
          stream: 'stdout',
        ),
      );
      await _flushEvents();

      expect(
        controller.timeline
            .any((item) => item.body.contains('结论先说：这个问题我已经定位到实时展示链路了')),
        isTrue,
      );
    });

    test('saveMemory 只发送 upsert，等待后端回流最新列表', () async {
      final service = _FakeMobileVcWsService();
      final controller = SessionController(service: service);
      await controller.initialize();
      addTearDown(controller.disposeController);

      controller.saveMemory(const MemoryItem(
        id: 'mem-2',
        title: 'New Memory',
        content: 'remember this',
      ));

      expect(service.sentPayloads, hasLength(1));
      expect(service.sentPayloads.single['action'], 'memory_upsert');
    });

    test('catalog 回流后结束 saving skill 状态并刷新列表', () async {
      final service = _FakeMobileVcWsService();
      final controller = SessionController(service: service);
      await controller.initialize();
      addTearDown(controller.disposeController);

      controller.saveSkill(const SkillDefinition(
        name: 'authoring-skill',
        description: 'desc',
        prompt: 'prompt',
        resultView: 'review-card',
        targetType: 'diff',
      ));
      expect(controller.isSavingSkill, isTrue);

      service.emit(
        SkillCatalogResultEvent(
          timestamp: _timestamp,
          sessionId: 'session-1',
          runtimeMeta: const RuntimeMeta(source: 'catalog-authoring'),
          raw: const {'type': 'skill_catalog_result'},
          items: const [
            SkillDefinition(
              name: 'authoring-skill',
              description: 'generated',
              prompt: 'new prompt',
              resultView: 'review-card',
              targetType: 'diff',
            ),
          ],
        ),
      );
      await _flushEvents();

      expect(controller.isSavingSkill, isFalse);
      expect(controller.skills.single.name, 'authoring-skill');
    });

    test('catalog 回流后结束 saving memory 状态并刷新列表', () async {
      final service = _FakeMobileVcWsService();
      final controller = SessionController(service: service);
      await controller.initialize();
      addTearDown(controller.disposeController);

      controller.saveMemory(const MemoryItem(
        id: 'mem-author',
        title: '偏好',
        content: 'old',
      ));
      expect(controller.isSavingMemory, isTrue);

      service.emit(
        MemoryListResultEvent(
          timestamp: _timestamp,
          sessionId: 'session-1',
          runtimeMeta: const RuntimeMeta(source: 'catalog-authoring'),
          raw: const {'type': 'memory_list_result'},
          items: const [
            MemoryItem(id: 'mem-author', title: '偏好', content: 'generated'),
          ],
        ),
      );
      await _flushEvents();

      expect(controller.isSavingMemory, isFalse);
      expect(controller.memoryItems.single.id, 'mem-author');
    });

    test('saveGeneratedSkill 在 Claude 会话中走 ai_turn continuation', () async {
      final service = _FakeMobileVcWsService();
      final controller = SessionController(service: service);
      await controller.initialize();
      addTearDown(controller.disposeController);

      await controller.saveConfig(
        const AppConfig(
          cwd: '/workspace',
          engine: 'claude',
          permissionMode: 'default',
        ),
      );
      service.emit(
        AgentStateEvent(
          timestamp: _timestamp,
          sessionId: 'session-1',
          runtimeMeta: const RuntimeMeta(
            command: 'claude',
            executionId: 'exec-skill',
            claudeLifecycle: 'active',
          ),
          raw: const {'type': 'agent_state'},
          state: 'IDLE',
          message: 'ready',
          command: 'claude',
        ),
      );
      await _flushEvents();
      service.sentPayloads.clear();

      controller.saveGeneratedSkill(request: '生成一个总结 diff 的 skill');

      expect(service.sentPayloads, hasLength(1));
      expect(service.sentPayloads[0]['action'], 'ai_turn');
      expect(service.sentPayloads[0]['engine'], 'claude');
      expect(service.sentPayloads[0]['targetType'], 'skill');
      expect(service.sentPayloads[0]['resultView'], 'skill-catalog');
      expect(
          (service.sentPayloads[0]['data'] as String)
              .contains('生成一个总结 diff 的 skill'),
          isTrue);
    });

    test('saveGeneratedSkill 首次发起时走单条 ai_turn 编排', () async {
      final service = _FakeMobileVcWsService();
      final controller = SessionController(service: service);
      await controller.initialize();
      addTearDown(controller.disposeController);

      await controller.saveConfig(
        const AppConfig(
          cwd: '/workspace',
          engine: 'claude',
          permissionMode: 'default',
        ),
      );

      controller.saveGeneratedSkill(request: '生成一个总结 diff 的 skill');

      expect(service.sentPayloads, hasLength(1));
      expect(service.sentPayloads[0]['action'], 'ai_turn');
      expect(service.sentPayloads[0]['engine'], 'claude');
      expect(service.sentPayloads[0]['targetType'], 'skill');
      expect(service.sentPayloads[0]['resultView'], 'skill-catalog');
      expect(
          (service.sentPayloads[0]['data'] as String)
              .contains('"mobilevcCatalogAuthoring":true'),
          isTrue);
      expect(
          (service.sentPayloads[0]['data'] as String)
              .contains('"kind":"skill"'),
          isTrue);
      expect(
        (service.sentPayloads[0]['data'] as String)
            .contains('生成一个总结 diff 的 skill'),
        isTrue,
      );
    });

    test('reviseMemoryWithClaude 在 Claude 会话中走 ai_turn continuation', () async {
      final service = _FakeMobileVcWsService();
      final controller = SessionController(service: service);
      await controller.initialize();
      addTearDown(controller.disposeController);

      await controller.saveConfig(
        const AppConfig(
          cwd: '/workspace',
          engine: 'claude',
          permissionMode: 'default',
        ),
      );
      service.emit(
        AgentStateEvent(
          timestamp: _timestamp,
          sessionId: 'session-1',
          runtimeMeta: const RuntimeMeta(
            command: 'claude',
            executionId: 'exec-memory',
            claudeLifecycle: 'active',
          ),
          raw: const {'type': 'agent_state'},
          state: 'IDLE',
          message: 'ready',
          command: 'claude',
        ),
      );
      await _flushEvents();
      service.sentPayloads.clear();

      controller.reviseMemoryWithClaude(
        const MemoryItem(id: 'mem-9', title: '偏好', content: '用户偏爱深色模式'),
        '改成强调 iOS 风格 UI 偏好',
      );

      expect(service.sentPayloads, hasLength(1));
      expect(service.sentPayloads[0]['action'], 'ai_turn');
      expect(service.sentPayloads[0]['engine'], 'claude');
      expect(service.sentPayloads[0]['targetType'], 'memory');
      expect(service.sentPayloads[0]['resultView'], 'memory-catalog');
      expect(
          (service.sentPayloads[0]['data'] as String)
              .contains('改成强调 iOS 风格 UI 偏好'),
          isTrue);
    });

    test('reviseMemoryWithClaude 首次发起时走单条 ai_turn 编排', () async {
      final service = _FakeMobileVcWsService();
      final controller = SessionController(service: service);
      await controller.initialize();
      addTearDown(controller.disposeController);

      await controller.saveConfig(
        const AppConfig(
          cwd: '/workspace',
          engine: 'claude',
          permissionMode: 'default',
        ),
      );

      controller.reviseMemoryWithClaude(
        const MemoryItem(id: 'mem-9', title: '偏好', content: '用户偏爱深色模式'),
        '改成强调 iOS 风格 UI 偏好',
      );

      expect(service.sentPayloads, hasLength(1));
      expect(service.sentPayloads[0]['action'], 'ai_turn');
      expect(service.sentPayloads[0]['engine'], 'claude');
      expect(service.sentPayloads[0]['targetType'], 'memory');
      expect(service.sentPayloads[0]['resultView'], 'memory-catalog');
      expect(
          (service.sentPayloads[0]['data'] as String)
              .contains('"mobilevcCatalogAuthoring":true'),
          isTrue);
      expect(
          (service.sentPayloads[0]['data'] as String)
              .contains('"kind":"memory"'),
          isTrue);
      expect(
        (service.sentPayloads[0]['data'] as String)
            .contains('改成强调 iOS 风格 UI 偏好'),
        isTrue,
      );
    });

    test('executeSkill 仍发送 skill_exec', () async {
      final service = _FakeMobileVcWsService();
      final controller = SessionController(service: service);
      await controller.initialize();
      addTearDown(controller.disposeController);

      await controller.saveConfig(
        const AppConfig(
          cwd: '/workspace',
          engine: 'claude',
          permissionMode: 'default',
        ),
      );

      controller.executeSkill('review-pr');

      expect(service.sentPayloads, hasLength(1));
      expect(service.sentPayloads.single['action'], 'skill_exec');
      expect(service.sentPayloads.single['name'], 'review-pr');
    });

    test('PromptRequestEvent 对中文和拒绝类英文权限词也识别为 permission', () {
      final zhPrompt = PromptRequestEvent(
        timestamp: _timestamp,
        sessionId: 'session-1',
        runtimeMeta: const RuntimeMeta(),
        raw: const {'type': 'prompt_request', 'msg': '是否同意写入？可拒绝或取消'},
        message: '是否同意写入？可拒绝或取消',
        options: const [],
      );
      final enPrompt = PromptRequestEvent(
        timestamp: _timestamp,
        sessionId: 'session-1',
        runtimeMeta: const RuntimeMeta(),
        raw: const {
          'type': 'prompt_request',
          'msg': 'reject or cancel this permission request'
        },
        message: 'reject or cancel this permission request',
        options: const [],
      );

      expect(zhPrompt.message, isNotEmpty);
      expect(enPrompt.message, isNotEmpty);
    });

    test('PromptRequestEvent 对 y/n 与 allow/deny options 识别为 permission', () {
      final ynPrompt = PromptRequestEvent(
        timestamp: _timestamp,
        sessionId: 'session-1',
        runtimeMeta: const RuntimeMeta(),
        raw: const {'type': 'prompt_request', 'msg': 'Proceed?'},
        message: 'Proceed?',
        options: const [PromptOption(value: 'y'), PromptOption(value: 'n')],
      );
      final allowDenyPrompt = PromptRequestEvent(
        timestamp: _timestamp,
        sessionId: 'session-1',
        runtimeMeta: const RuntimeMeta(),
        raw: const {'type': 'prompt_request', 'msg': 'Choose an option'},
        message: 'Choose an option',
        options: const [
          PromptOption(value: 'allow'),
          PromptOption(value: 'deny'),
        ],
      );

      expect(ynPrompt.options, hasLength(2));
      expect(allowDenyPrompt.options, hasLength(2));
    });

    test('PromptRequestEvent 对 approve/reject options 识别为 permission', () {
      final prompt = PromptRequestEvent(
        timestamp: _timestamp,
        sessionId: 'session-1',
        runtimeMeta: const RuntimeMeta(),
        raw: const {'type': 'prompt_request', 'msg': 'Choose an option'},
        message: 'Choose an option',
        options: const [
          PromptOption(value: 'approve'),
          PromptOption(value: 'reject'),
        ],
      );

      expect(prompt.options, hasLength(2));
    });

    test('PromptRequestEvent 不把 accept/revert/revise 识别为 permission', () {
      final prompt = PromptRequestEvent(
        timestamp: _timestamp,
        sessionId: 'session-1',
        runtimeMeta: const RuntimeMeta(),
        raw: const {'type': 'prompt_request', 'msg': 'Choose an option'},
        message: 'Choose an option',
        options: const [
          PromptOption(value: 'accept'),
          PromptOption(value: 'revert'),
          PromptOption(value: 'revise'),
        ],
      );

      expect(prompt.options, hasLength(3));
    });

    test('connect 时会补发 session_context_get，但不主动拉取 diff page', () async {
      final service = _FakeMobileVcWsService();
      final controller = SessionController(service: service);
      await controller.initialize();
      addTearDown(controller.disposeController);

      await controller.saveConfig(
        const AppConfig(
          cwd: '/workspace',
          engine: 'claude',
          permissionMode: 'default',
        ),
      );

      await controller.connect();

      await Future<void>.delayed(const Duration(milliseconds: 520));
      await _flushEvents();

      final actions = service.sentPayloads
          .map((payload) => payload['action'])
          .whereType<String>()
          .toList();
      expect(actions, contains('session_context_get'));
      expect(actions, isNot(contains('review_state_get')));
      expect(actions, isNot(contains('session_diff_page_get')));
    });

    test('review 决策后优先跳到同组下一个待审文件', () async {
      final service = _FakeMobileVcWsService();
      final controller = SessionController(service: service);
      await controller.initialize();
      addTearDown(controller.disposeController);

      service.emit(_reviewDiffEvent(
        contextId: 'diff-1',
        path: '/workspace/a.dart',
        title: 'a.dart',
        groupId: 'group-1',
        groupTitle: '组一',
      ));
      service.emit(_reviewDiffEvent(
        contextId: 'diff-2',
        path: '/workspace/b.dart',
        title: 'b.dart',
        groupId: 'group-1',
        groupTitle: '组一',
      ));
      await _flushEvents();

      controller.setActiveReviewGroup('group-1');
      controller.setActiveReviewDiff('diff-1');
      controller.sendReviewDecision('accept');

      expect(service.sentPayloads.last['action'], 'review_decision');
      expect(controller.activeReviewGroupId, 'group-1');
      expect(controller.activeReviewDiffId, 'diff-2');
      expect(controller.currentReviewDiff?.id, 'diff-2');
    });

    test('当前组审完后切到下一个待审组', () async {
      final service = _FakeMobileVcWsService();
      final controller = SessionController(service: service);
      await controller.initialize();
      addTearDown(controller.disposeController);

      service.emit(_reviewDiffEvent(
        contextId: 'diff-1',
        path: '/workspace/a.dart',
        title: 'a.dart',
        groupId: 'group-1',
        groupTitle: '组一',
      ));
      service.emit(_reviewDiffEvent(
        contextId: 'diff-2',
        path: '/workspace/c.dart',
        title: 'c.dart',
        groupId: 'group-2',
        groupTitle: '组二',
      ));
      await _flushEvents();

      controller.setActiveReviewGroup('group-1');
      controller.setActiveReviewDiff('diff-1');
      controller.sendReviewDecision('accept');

      expect(controller.activeReviewGroupId, 'group-2');
      expect(controller.activeReviewDiffId, 'diff-2');
      expect(controller.currentReviewDiff?.id, 'diff-2');
    });

    test('后端临时 permission mode 不改写配置和显示模式', () async {
      final service = _FakeMobileVcWsService();
      final controller = SessionController(service: service);
      await controller.initialize();
      addTearDown(controller.disposeController);

      await controller.saveConfig(
        const AppConfig(
          cwd: '/workspace',
          engine: 'claude',
          permissionMode: 'default',
        ),
      );

      service.emit(
        AgentStateEvent(
          timestamp: _timestamp,
          sessionId: 'session-1',
          runtimeMeta: const RuntimeMeta(
            command: 'claude',
            permissionMode: 'acceptEdits',
          ),
          raw: const {'type': 'agent_state'},
          state: 'RUNNING_TOOL',
          message: '恢复权限中',
          command: 'claude',
        ),
      );
      await _flushEvents();

      expect(controller.config.permissionMode, 'default');
      expect(controller.displayPermissionMode, 'default');

      service.emit(
        SessionStateEvent(
          timestamp: _timestamp,
          sessionId: 'session-1',
          runtimeMeta: const RuntimeMeta(permissionMode: 'default'),
          raw: const {'type': 'session_state'},
          state: 'RUNNING',
          message: '恢复完成',
        ),
      );
      await _flushEvents();

      expect(controller.config.permissionMode, 'default');
      expect(controller.displayPermissionMode, 'default');
    });

    test('用户切换到手动审核时配置和后端 payload 都保留 default', () async {
      final service = _FakeMobileVcWsService();
      final controller = SessionController(service: service);
      await controller.initialize();
      addTearDown(controller.disposeController);

      controller.updatePermissionMode('default');

      expect(controller.config.permissionMode, 'default');
      expect(controller.displayPermissionMode, 'default');
      expect(service.sentPayloads, hasLength(1));
      expect(service.sentPayloads.single['action'], 'set_permission_mode');
      expect(service.sentPayloads.single['permissionMode'], 'default');
    });

    test('用户切换权限模式后旧运行态不会把 UI 压回去', () async {
      final service = _FakeMobileVcWsService();
      final controller = SessionController(service: service);
      await controller.initialize();
      addTearDown(controller.disposeController);

      await controller.saveConfig(
        const AppConfig(
          cwd: '/workspace',
          engine: 'claude',
          permissionMode: 'auto',
        ),
      );

      controller.updatePermissionMode('default');
      expect(controller.displayPermissionMode, 'default');

      service.emit(
        SessionDeltaEvent(
          timestamp: _timestamp,
          sessionId: 'session-1',
          runtimeMeta: const RuntimeMeta(),
          raw: const {'type': 'session_delta'},
          summary: const SessionSummary(id: 'session-1', title: 'session'),
          base: const SessionDeltaKnown(),
          latest: const SessionDeltaKnown(),
          resumeRuntimeMeta: const RuntimeMeta(permissionMode: 'auto'),
        ),
      );
      service.emit(
        AgentStateEvent(
          timestamp: _timestamp,
          sessionId: 'session-1',
          runtimeMeta: const RuntimeMeta(
            command: 'claude',
            permissionMode: 'auto',
          ),
          raw: const {'type': 'agent_state'},
          state: 'RUNNING',
          message: '旧状态',
          command: 'claude',
        ),
      );
      await _flushEvents();

      expect(controller.config.permissionMode, 'default');
      expect(controller.displayPermissionMode, 'default');
    });

    test('permission decision 优先沿用当前交互的 permission mode', () async {
      final service = _FakeMobileVcWsService();
      final controller = SessionController(service: service);
      await controller.initialize();
      addTearDown(controller.disposeController);

      await controller.saveConfig(
        const AppConfig(
          cwd: '/workspace',
          engine: 'claude',
          permissionMode: 'default',
        ),
      );

      service.emit(
        InteractionRequestEvent(
          timestamp: _timestamp,
          sessionId: 'session-1',
          runtimeMeta: const RuntimeMeta(
            command: 'claude',
            permissionMode: 'acceptEdits',
            contextId: 'ctx-1',
            targetPath: '/workspace/README.md',
          ),
          raw: const {
            'type': 'interaction_request',
            'kind': 'permission',
            'title': 'Permission required',
            'message': 'Claude needs permission to write README.md',
          },
          kind: 'permission',
          title: 'Permission required',
          message: 'Claude needs permission to write README.md',
          options: const [PromptOption(value: 'y'), PromptOption(value: 'n')],
        ),
      );
      await _flushEvents();

      controller.submitPromptOption('允许');

      expect(service.sentPayloads, hasLength(1));
      expect(service.sentPayloads.single['action'], 'permission_decision');
      expect(service.sentPayloads.single['permissionMode'], 'auto');
      expect(controller.config.permissionMode, 'default');
    });

    test('普通新输入仍使用默认 permission mode', () async {
      final service = _FakeMobileVcWsService();
      final controller = SessionController(service: service);
      await controller.initialize();
      addTearDown(controller.disposeController);

      await controller.saveConfig(
        const AppConfig(
          cwd: '/workspace',
          engine: 'claude',
          permissionMode: 'default',
        ),
      );

      service.emit(
        SessionStateEvent(
          timestamp: _timestamp,
          sessionId: 'session-1',
          runtimeMeta: const RuntimeMeta(
            command: 'claude',
            permissionMode: 'acceptEdits',
            claudeLifecycle: 'active',
          ),
          raw: const {'type': 'session_state'},
          state: 'IDLE',
          message: '恢复中间态',
        ),
      );
      await _flushEvents();
      service.sentPayloads.clear();

      controller.sendInputText('继续处理');

      expect(service.sentPayloads, hasLength(1));
      expect(service.sentPayloads[0]['action'], 'ai_turn');
      expect(service.sentPayloads[0]['data'], '继续处理\n');
      expect(service.sentPayloads[0]['permissionMode'], 'default');
    });

    test('permission 等待期间收到 idle-like state 不会提前清掉 pending prompt', () async {
      final service = _FakeMobileVcWsService();
      final controller = SessionController(service: service);
      await controller.initialize();
      addTearDown(controller.disposeController);

      service.emit(
        RuntimePhaseEvent(
          timestamp: _timestamp,
          sessionId: 'session-1',
          runtimeMeta: RuntimeMeta(command: 'claude'),
          raw: const {'type': 'runtime_phase'},
          phase: 'permission_blocked',
          kind: 'permission',
          message: 'Awaiting approval',
        ),
      );
      service.emit(
        RuntimePhaseEvent(
          timestamp: _timestamp,
          sessionId: 'session-1',
          runtimeMeta: RuntimeMeta(command: 'claude'),
          raw: const {'type': 'runtime_phase'},
          phase: 'permission_blocked',
          kind: 'permission',
          message: 'Awaiting approval',
        ),
      );
      service.emit(
        RuntimePhaseEvent(
          timestamp: _timestamp,
          sessionId: 'session-1',
          runtimeMeta: RuntimeMeta(command: 'claude'),
          raw: const {'type': 'runtime_phase'},
          phase: 'permission_blocked',
          kind: 'permission',
          message: 'Awaiting approval',
        ),
      );
      service.emit(
        RuntimePhaseEvent(
          timestamp: _timestamp,
          sessionId: 'session-1',
          runtimeMeta: RuntimeMeta(command: 'claude'),
          raw: const {'type': 'runtime_phase'},
          phase: 'permission_blocked',
          kind: 'permission',
          message: 'Awaiting approval',
        ),
      );
      service.emit(
        RuntimePhaseEvent(
          timestamp: _timestamp,
          sessionId: 'session-1',
          runtimeMeta: RuntimeMeta(command: 'claude'),
          raw: const {'type': 'runtime_phase'},
          phase: 'permission_blocked',
          kind: 'permission',
          message: 'Awaiting approval',
        ),
      );
      service.emit(
        PromptRequestEvent(
          timestamp: _timestamp,
          sessionId: 'session-1',
          runtimeMeta:
              const RuntimeMeta(command: 'claude', blockingKind: 'permission'),
          raw: const {
            'type': 'prompt_request',
            'msg': 'Allow write to README.md?'
          },
          message: 'Allow write to README.md?',
          options: const [PromptOption(value: 'y'), PromptOption(value: 'n')],
        ),
      );
      await _flushEvents();
      expect(controller.shouldShowPermissionChoices, isTrue);

      service.emit(
        AgentStateEvent(
          timestamp: _timestamp,
          sessionId: 'session-1',
          runtimeMeta: const RuntimeMeta(command: 'claude'),
          raw: const {'type': 'agent_state'},
          state: 'IDLE',
          message: '中间态',
          command: 'claude',
        ),
      );
      await _flushEvents();

      expect(controller.pendingPrompt?.message, 'Allow write to README.md?');
      expect(controller.shouldShowPermissionChoices, isTrue);
    });

    test('review 等待期间收到 idle-like state 不会提前清掉 review 交互', () async {
      final service = _FakeMobileVcWsService();
      final controller = SessionController(service: service);
      await controller.initialize();
      addTearDown(controller.disposeController);

      service.emit(_reviewDiffEvent(
        contextId: 'diff-1',
        path: '/workspace/a.dart',
        title: 'a.dart',
        groupId: 'group-1',
        groupTitle: '组一',
      ));
      service.emit(
        AgentStateEvent(
          timestamp: _timestamp,
          sessionId: 'session-1',
          runtimeMeta: const RuntimeMeta(command: 'claude'),
          raw: const {'type': 'agent_state'},
          state: 'WAIT_INPUT',
          message: '等待审核',
          awaitInput: true,
          command: 'claude',
        ),
      );
      service.emit(
        InteractionRequestEvent(
          timestamp: _timestamp,
          sessionId: 'session-1',
          runtimeMeta: const RuntimeMeta(command: 'claude'),
          raw: const {
            'type': 'interaction_request',
            'kind': 'review',
            'title': 'Review required',
            'message': '请处理 diff',
          },
          kind: 'review',
          title: 'Review required',
          message: '请处理 diff',
          options: const [
            PromptOption(value: 'accept'),
            PromptOption(value: 'revert'),
            PromptOption(value: 'revise'),
          ],
        ),
      );
      await _flushEvents();
      expect(controller.shouldShowReviewChoices, isTrue);

      service.emit(
        SessionStateEvent(
          timestamp: _timestamp,
          sessionId: 'session-1',
          runtimeMeta: const RuntimeMeta(command: 'claude'),
          raw: const {'type': 'session_state'},
          state: 'IDLE',
          message: '中间态',
        ),
      );
      await _flushEvents();

      expect(controller.pendingInteraction?.isReview, isTrue);
      expect(controller.shouldShowReviewChoices, isTrue);
    });

    test('permission prompt 选择允许发送 permission_decision', () async {
      final service = _FakeMobileVcWsService();
      final controller = SessionController(service: service);
      await controller.initialize();
      addTearDown(controller.disposeController);

      await controller.saveConfig(
        const AppConfig(
          cwd: '/workspace',
          engine: 'claude',
          permissionMode: 'default',
        ),
      );

      service.emit(
        FSReadResultEvent(
          timestamp: _timestamp,
          sessionId: 'session-1',
          runtimeMeta: RuntimeMeta(),
          raw: {
            'type': 'fs_read_result',
            'path': '/workspace/README.md',
          },
          result: FileReadResult(
            path: '/workspace/README.md',
            content: '# MobileVC\n',
            lang: 'markdown',
            isText: true,
            size: 11,
            encoding: 'utf-8',
          ),
        ),
      );
      service.emit(
        RuntimePhaseEvent(
          timestamp: _timestamp,
          sessionId: 'session-1',
          runtimeMeta: RuntimeMeta(command: 'claude'),
          raw: const {'type': 'runtime_phase'},
          phase: 'permission_blocked',
          kind: 'permission',
          message: 'Allow write to README.md?',
        ),
      );
      service.emit(
        PromptRequestEvent(
          timestamp: _timestamp,
          sessionId: 'session-1',
          runtimeMeta: const RuntimeMeta(
            resumeSessionId: 'resume-123',
            command: 'claude',
            contextId: 'ctx-1',
            contextTitle: 'README',
            targetPath: '/workspace/README.md',
            targetType: 'file',
            blockingKind: 'permission',
          ),
          raw: const {
            'type': 'prompt_request',
            'msg': 'Allow write to README.md?',
          },
          message: 'Allow write to README.md?',
          options: const [PromptOption(value: 'y'), PromptOption(value: 'n')],
        ),
      );
      await _flushEvents();

      controller.submitPromptOption('allow');

      expect(service.sentPayloads, hasLength(1));
      final payload = service.sentPayloads.single;
      expect(payload['action'], 'permission_decision');
      expect(payload['decision'], 'approve');
      expect(payload['permissionMode'], 'default');
      expect(payload['targetPath'], '/workspace/README.md');
      expect(payload['promptMessage'], 'Allow write to README.md?');
      expect(payload['cwd'], '/workspace');
      expect(payload.containsKey('codexSandboxMode'), isFalse);
    });

    test('permission prompt 中文允许也发送 permission_decision', () async {
      final service = _FakeMobileVcWsService();
      final controller = SessionController(service: service);
      await controller.initialize();
      addTearDown(controller.disposeController);

      await controller.saveConfig(
        const AppConfig(
          cwd: '/workspace',
          engine: 'claude',
          permissionMode: 'default',
        ),
      );

      service.emit(
        RuntimePhaseEvent(
          timestamp: _timestamp,
          sessionId: 'session-1',
          runtimeMeta: RuntimeMeta(command: 'claude'),
          raw: const {'type': 'runtime_phase'},
          phase: 'permission_blocked',
          kind: 'permission',
          message: 'Allow write to README.md?',
        ),
      );
      service.emit(
        PromptRequestEvent(
          timestamp: _timestamp,
          sessionId: 'session-1',
          runtimeMeta: const RuntimeMeta(
            command: 'claude',
            targetPath: '/workspace/README.md',
            blockingKind: 'permission',
          ),
          raw: const {
            'type': 'prompt_request',
            'msg': 'Allow write to README.md?',
          },
          message: 'Allow write to README.md?',
          options: const [PromptOption(value: 'y'), PromptOption(value: 'n')],
        ),
      );
      await _flushEvents();

      controller.submitPromptOption('允许');

      expect(service.sentPayloads, hasLength(1));
      final payload = service.sentPayloads.single;
      expect(payload['action'], 'permission_decision');
      expect(payload['decision'], 'approve');
    });

    test('permission prompt 中文拒绝也发送 permission_decision', () async {
      final service = _FakeMobileVcWsService();
      final controller = SessionController(service: service);
      await controller.initialize();
      addTearDown(controller.disposeController);

      service.emit(
        RuntimePhaseEvent(
          timestamp: _timestamp,
          sessionId: 'session-1',
          runtimeMeta: RuntimeMeta(command: 'claude'),
          raw: const {'type': 'runtime_phase'},
          phase: 'permission_blocked',
          kind: 'permission',
          message: 'Allow write to README.md?',
        ),
      );
      service.emit(
        PromptRequestEvent(
          timestamp: _timestamp,
          sessionId: 'session-1',
          runtimeMeta:
              const RuntimeMeta(command: 'claude', blockingKind: 'permission'),
          raw: const {
            'type': 'prompt_request',
            'msg': 'Allow write to README.md?',
          },
          message: 'Allow write to README.md?',
          options: const [PromptOption(value: 'y'), PromptOption(value: 'n')],
        ),
      );
      await _flushEvents();

      controller.submitPromptOption('拒绝');

      expect(service.sentPayloads, hasLength(1));
      final payload = service.sentPayloads.single;
      expect(payload['action'], 'permission_decision');
      expect(payload['decision'], 'deny');
    });

    test('permission rule list result 会更新规则状态与摘要', () async {
      final service = _FakeMobileVcWsService();
      final controller = SessionController(service: service);
      await controller.initialize();
      addTearDown(controller.disposeController);

      service.emit(
        PermissionRuleListResultEvent(
          timestamp: _timestamp,
          sessionId: 'session-1',
          runtimeMeta: const RuntimeMeta(),
          raw: const {'type': 'permission_rule_list_result'},
          sessionEnabled: true,
          persistentEnabled: true,
          sessionRules: const [
            PermissionRule(
              id: 'session-rule',
              scope: 'session',
              enabled: true,
              engine: 'codex',
              kind: 'write',
              commandHead: 'bash',
              targetPathPrefix: '/workspace/lib',
              summary: 'Codex · write · bash · /workspace/lib',
            ),
          ],
          persistentRules: const [
            PermissionRule(
              id: 'persistent-rule',
              scope: 'persistent',
              enabled: true,
              engine: 'codex',
              kind: 'shell',
              commandHead: 'python',
              summary: 'Codex · shell · python',
            ),
          ],
        ),
      );
      await _flushEvents();

      expect(controller.sessionPermissionRulesEnabled, isTrue);
      expect(controller.persistentPermissionRulesEnabled, isTrue);
      expect(controller.sessionPermissionRules, hasLength(1));
      expect(controller.persistentPermissionRules, hasLength(1));
      expect(controller.permissionRuleCount, 2);
      expect(controller.permissionRuleSummary, '2 条 · 会话 / 长期');
    });

    test('permission prompt 选择本会话允许会发送 session scope', () async {
      final service = _FakeMobileVcWsService();
      final controller = SessionController(service: service);
      await controller.initialize();
      addTearDown(controller.disposeController);

      service.emit(
        PromptRequestEvent(
          timestamp: _timestamp,
          sessionId: 'session-1',
          runtimeMeta: const RuntimeMeta(
            command: 'codex',
            targetPath: '/workspace/lib/main.dart',
            blockingKind: 'permission',
          ),
          raw: const {'type': 'prompt_request', 'msg': 'Allow edit main.dart?'},
          message: 'Allow edit main.dart?',
          options: const [PromptOption(value: 'y'), PromptOption(value: 'n')],
        ),
      );
      await _flushEvents();
      service.sentPayloads.clear();

      controller.submitPromptOption('approve:session');

      expect(service.sentPayloads, hasLength(1));
      final payload = service.sentPayloads.single;
      expect(payload['action'], 'permission_decision');
      expect(payload['decision'], 'approve');
      expect(payload['scope'], 'session');
    });

    test('Codex permission prompt 会带入沙箱配置', () async {
      final service = _FakeMobileVcWsService();
      final controller = SessionController(service: service);
      await controller.initialize();
      addTearDown(controller.disposeController);

      await controller.saveConfig(
        controller.config.copyWith(
          engine: 'codex',
          codexSandboxMode: 'danger-full-access',
          permissionMode: 'config',
        ),
      );
      service.emit(
        PromptRequestEvent(
          timestamp: _timestamp,
          sessionId: 'session-1',
          runtimeMeta: const RuntimeMeta(
            command: 'codex',
            engine: 'codex',
            targetPath: '/workspace/lib/main.dart',
            blockingKind: 'permission',
          ),
          raw: const {'type': 'prompt_request', 'msg': 'Allow edit main.dart?'},
          message: 'Allow edit main.dart?',
          options: const [PromptOption(value: 'y'), PromptOption(value: 'n')],
        ),
      );
      await _flushEvents();
      service.sentPayloads.clear();

      controller.submitPromptOption('approve');

      expect(service.sentPayloads, hasLength(1));
      final payload = service.sentPayloads.single;
      expect(payload['action'], 'permission_decision');
      expect(payload['decision'], 'approve');
      expect(payload['engine'], 'codex');
      expect(payload['codexSandboxMode'], 'danger-full-access');
    });

    test('permission prompt 选择长期允许会发送 persistent scope', () async {
      final service = _FakeMobileVcWsService();
      final controller = SessionController(service: service);
      await controller.initialize();
      addTearDown(controller.disposeController);

      service.emit(
        PromptRequestEvent(
          timestamp: _timestamp,
          sessionId: 'session-1',
          runtimeMeta: const RuntimeMeta(
            command: 'codex',
            targetPath: '/workspace/lib/main.dart',
            blockingKind: 'permission',
          ),
          raw: const {'type': 'prompt_request', 'msg': 'Allow edit main.dart?'},
          message: 'Allow edit main.dart?',
          options: const [PromptOption(value: 'y'), PromptOption(value: 'n')],
        ),
      );
      await _flushEvents();
      service.sentPayloads.clear();

      controller.submitPromptOption('approve:persistent');

      expect(service.sentPayloads, hasLength(1));
      final payload = service.sentPayloads.single;
      expect(payload['action'], 'permission_decision');
      expect(payload['decision'], 'approve');
      expect(payload['scope'], 'persistent');
    });

    test('setPermissionRuleEnabled 会发送 permission_rule_upsert', () async {
      final service = _FakeMobileVcWsService();
      final controller = SessionController(service: service);
      await controller.initialize();
      addTearDown(controller.disposeController);

      controller.setPermissionRuleEnabled(
        const PermissionRule(
          id: 'rule-1',
          scope: 'persistent',
          enabled: true,
          engine: 'codex',
          kind: 'write',
          commandHead: 'bash',
          targetPathPrefix: '/workspace/lib',
          summary: 'rule',
        ),
        false,
      );

      expect(service.sentPayloads, hasLength(1));
      expect(service.sentPayloads.single['action'], 'permission_rule_upsert');
      final rule = service.sentPayloads.single['rule'] as Map<String, dynamic>;
      expect(rule['id'], 'rule-1');
      expect(rule['scope'], 'persistent');
      expect(rule['enabled'], isFalse);
    });

    test('setPermissionRulesEnabled 会发送 scope 开关请求', () async {
      final service = _FakeMobileVcWsService();
      final controller = SessionController(service: service);
      await controller.initialize();
      addTearDown(controller.disposeController);

      controller.setPermissionRulesEnabled('persistent', true);

      expect(service.sentPayloads, hasLength(1));
      expect(service.sentPayloads.single, {
        'action': 'permission_rules_set_enabled',
        'scope': 'persistent',
        'enabled': true,
      });
    });

    test('后端 acceptEdits 不会覆盖手动审核配置', () async {
      final service = _FakeMobileVcWsService();
      final controller = SessionController(service: service);
      await controller.initialize();
      addTearDown(controller.disposeController);

      await controller.saveConfig(
        const AppConfig(
          cwd: '/workspace',
          engine: 'claude',
          permissionMode: 'default',
        ),
      );

      service.emit(
        SessionHistoryEvent(
          timestamp: _timestamp,
          sessionId: 'session-1',
          runtimeMeta: const RuntimeMeta(),
          raw: const {'type': 'session_history'},
          summary: const SessionSummary(id: 'session-1', title: '历史会话'),
          resumeRuntimeMeta: const RuntimeMeta(
            command: 'claude --resume session-1',
            permissionMode: 'acceptEdits',
            claudeLifecycle: 'resumable',
          ),
        ),
      );
      await _flushEvents();

      service.emit(
        _reviewDiffEvent(
          contextId: 'diff-auto-check',
          path: '/workspace/lib/main.dart',
          title: 'main.dart diff',
          groupId: 'group-auto-check',
          groupTitle: '自动接受检查',
        ),
      );
      await _flushEvents();

      expect(controller.isAutoAcceptMode, isFalse);
      expect(controller.pendingDiffs, hasLength(1));
      expect(controller.pendingDiffs.single.reviewStatus, 'pending');
      expect(controller.pendingDiffs.single.pendingReview, isTrue);
    });

    test('自动模式下新 diff 直接通过且不显示审核阻塞', () async {
      final service = _FakeMobileVcWsService();
      final controller = SessionController(service: service);
      await controller.initialize();
      addTearDown(controller.disposeController);

      await controller.saveConfig(
        const AppConfig(
          cwd: '/workspace',
          engine: 'claude',
          permissionMode: 'default',
        ),
      );
      controller.updatePermissionMode('auto');
      service.sentPayloads.clear();

      service.emit(
        _reviewDiffEvent(
          contextId: 'diff-auto-accept',
          path: '/workspace/lib/main.dart',
          title: 'main.dart diff',
          groupId: 'group-auto-accept',
          groupTitle: '自动接受',
        ),
      );
      service.emit(
        PromptRequestEvent(
          timestamp: _timestamp,
          sessionId: 'session-1',
          runtimeMeta: const RuntimeMeta(
            command: 'claude',
            contextId: 'diff-auto-accept',
            targetPath: '/workspace/lib/main.dart',
            blockingKind: 'review',
          ),
          raw: const {'type': 'prompt_request'},
          message: 'Please accept, revert, or revise this diff',
          options: const [
            PromptOption(value: 'accept'),
            PromptOption(value: 'revert'),
            PromptOption(value: 'revise'),
          ],
        ),
      );
      await _flushEvents();

      expect(controller.isAutoAcceptMode, isTrue);
      expect(controller.pendingDiffs, isEmpty);
      expect(controller.hasPendingReview, isFalse);
      expect(controller.shouldShowReviewChoices, isFalse);
      expect(controller.pendingPrompt, isNull);
      expect(controller.recentDiffs.single.reviewStatus, 'accepted');
      expect(controller.recentDiffs.single.pendingReview, isFalse);
    });

    test('手动模式先授权，已有 diff 随后显示 review 按钮', () async {
      final service = _FakeMobileVcWsService();
      final controller = SessionController(service: service);
      await controller.initialize();
      addTearDown(controller.disposeController);

      await controller.saveConfig(
        const AppConfig(
          cwd: '/workspace',
          engine: 'claude',
          permissionMode: 'default',
        ),
      );

      service.emit(
        InteractionRequestEvent(
          timestamp: _timestamp,
          sessionId: 'session-1',
          runtimeMeta: const RuntimeMeta(
            command: 'claude',
            targetPath: '/workspace/README.md',
            blockingKind: 'permission',
            permissionMode: 'acceptEdits',
          ),
          raw: const {
            'type': 'interaction_request',
            'kind': 'permission',
            'message': 'Allow write to README.md?'
          },
          kind: 'permission',
          message: 'Allow write to README.md?',
          actions: const [
            InteractionAction(id: 'approve', label: '允许', value: 'approve'),
            InteractionAction(id: 'deny', label: '拒绝', value: 'deny'),
          ],
        ),
      );
      service.emit(
        FileDiffEvent(
          timestamp: _timestamp,
          sessionId: 'session-1',
          runtimeMeta: const RuntimeMeta(
            contextId: 'diff-with-permission-interaction',
            contextTitle: 'README diff',
            targetPath: '/workspace/README.md',
            groupId: 'group-review-1',
            groupTitle: '组一',
            permissionMode: 'acceptEdits',
          ),
          raw: const {'type': 'file_diff'},
          path: '/workspace/README.md',
          title: 'README diff',
          diff: '@@ -1 +1 @@\n-old\n+new',
          lang: 'markdown',
        ),
      );
      await _flushEvents();

      expect(controller.currentReviewDiff?.path, '/workspace/README.md');
      expect(controller.pendingInteraction?.isPermission, isTrue);
      expect(controller.displayPermissionMode, 'default');
      expect(controller.isAutoAcceptMode, isFalse);
      expect(controller.shouldShowPermissionChoices, isTrue);
      expect(controller.shouldShowReviewChoices, isFalse);
      expect(controller.canShowReviewActions, isFalse);

      controller.submitPromptOption('approve');

      expect(service.sentPayloads, hasLength(1));
      expect(service.sentPayloads.single['action'], 'permission_decision');
      expect(controller.pendingInteraction, isNull);
      expect(controller.shouldShowPermissionChoices, isFalse);
      expect(controller.shouldShowReviewChoices, isTrue);
      expect(controller.canShowReviewActions, isTrue);
    });

    test('permission allow 后出现 diff 不会自动 accept，必须显式 review_decision 才推进',
        () async {
      final service = _FakeMobileVcWsService();
      final controller = SessionController(service: service);
      await controller.initialize();
      addTearDown(controller.disposeController);

      await controller.saveConfig(
        const AppConfig(
          cwd: '/workspace',
          engine: 'claude',
          permissionMode: 'default',
        ),
      );

      service.emit(
        PromptRequestEvent(
          timestamp: _timestamp,
          sessionId: 'session-1',
          runtimeMeta: const RuntimeMeta(
            command: 'claude',
            contextId: 'perm-1',
            targetPath: '/workspace/README.md',
            claudeLifecycle: 'waiting_input',
            blockingKind: 'permission',
          ),
          raw: const {
            'type': 'prompt_request',
            'msg': 'Allow write to README.md?'
          },
          message: 'Allow write to README.md?',
          options: const [PromptOption(value: 'y'), PromptOption(value: 'n')],
        ),
      );
      await _flushEvents();
      expect(controller.shouldShowPermissionChoices, isTrue);

      controller.submitPromptOption('allow');

      expect(service.sentPayloads, hasLength(1));
      expect(service.sentPayloads.single['action'], 'permission_decision');
      service.sentPayloads.clear();

      service.emit(
        _reviewDiffEvent(
          contextId: 'diff-after-permission',
          path: '/workspace/README.md',
          title: 'README diff',
          groupId: 'group-1',
          groupTitle: '组一',
        ),
      );
      service.emit(
        AgentStateEvent(
          timestamp: _timestamp.add(const Duration(seconds: 1)),
          sessionId: 'session-1',
          runtimeMeta: const RuntimeMeta(
            command: 'claude',
            contextId: 'diff-after-permission',
            claudeLifecycle: 'waiting_input',
            blockingKind: 'review',
          ),
          raw: const {'type': 'agent_state'},
          state: 'WAIT_INPUT',
          message: '等待审核',
          awaitInput: true,
          command: 'claude',
        ),
      );
      service.emit(
        PromptRequestEvent(
          timestamp: _timestamp.add(const Duration(seconds: 1)),
          sessionId: 'session-1',
          runtimeMeta: const RuntimeMeta(
            command: 'claude',
            contextId: 'diff-after-permission',
            targetPath: '/workspace/README.md',
            claudeLifecycle: 'waiting_input',
            blockingKind: 'review',
          ),
          raw: const {
            'type': 'prompt_request',
            'msg': 'Please accept, revert, or revise this diff',
          },
          message: 'Please accept, revert, or revise this diff',
          options: const [
            PromptOption(value: 'accept'),
            PromptOption(value: 'revert'),
            PromptOption(value: 'revise'),
          ],
        ),
      );
      await _flushEvents();

      expect(controller.shouldShowReviewChoices, isTrue);
      expect(controller.currentReviewDiff?.path, '/workspace/README.md');
      expect(controller.pendingDiffs, hasLength(1));
      expect(controller.pendingDiffs.single.reviewStatus, 'pending');
      expect(controller.displayPermissionMode, 'default');
      expect(service.sentPayloads, isEmpty);

      controller.submitPromptOption('accept');

      expect(service.sentPayloads, hasLength(1));
      expect(service.sentPayloads.single['action'], 'review_decision');
      expect(service.sentPayloads.single['decision'], 'accept');
      expect(service.sentPayloads.single['is_review_only'], isTrue);

      service.sentPayloads.clear();
      expect(controller.pendingPrompt, isNull);
      expect(controller.pendingInteraction, isNull);
      expect(controller.shouldShowPermissionChoices, isFalse);
      expect(controller.shouldShowReviewChoices, isFalse);

      controller.sendInputText('继续处理');

      expect(service.sentPayloads, hasLength(1));
      expect(service.sentPayloads.single['action'], 'input');
      expect(service.sentPayloads.single['data'], '继续处理\n');
    });

    test('review prompt 的 revert 会发送非 review-only 决策', () async {
      final service = _FakeMobileVcWsService();
      final controller = SessionController(service: service);
      await controller.initialize();
      addTearDown(controller.disposeController);

      service.emit(
        FileDiffEvent(
          timestamp: _timestamp,
          sessionId: 'session-1',
          runtimeMeta: RuntimeMeta(
            contextId: 'diff-revert-1',
            contextTitle: 'README diff',
            targetPath: '/workspace/README.md',
          ),
          raw: {'type': 'file_diff'},
          path: '/workspace/README.md',
          title: 'README diff',
          diff: '@@ -1 +1 @@',
          lang: 'markdown',
        ),
      );
      service.emit(
        PromptRequestEvent(
          timestamp: _timestamp,
          sessionId: 'session-1',
          runtimeMeta: const RuntimeMeta(
            contextId: 'diff-revert-1',
            contextTitle: 'README diff',
            targetPath: '/workspace/README.md',
            blockingKind: 'review',
          ),
          raw: const {
            'type': 'prompt_request',
            'msg': 'Please accept, revert, or revise this diff',
          },
          message: 'Please accept, revert, or revise this diff',
          options: const [
            PromptOption(value: 'accept', label: '接受'),
            PromptOption(value: 'revert', label: '撤销'),
            PromptOption(value: 'revise', label: '继续修改'),
          ],
        ),
      );
      await _flushEvents();

      controller.submitPromptOption('revert');

      expect(service.sentPayloads, hasLength(1));
      expect(service.sentPayloads.single['action'], 'review_decision');
      expect(service.sentPayloads.single['decision'], 'revert');
      expect(service.sentPayloads.single['is_review_only'], isFalse);
    });

    test('review prompt 仍发送 review_decision', () async {
      final service = _FakeMobileVcWsService();
      final controller = SessionController(service: service);
      await controller.initialize();
      addTearDown(controller.disposeController);

      service.emit(
        FileDiffEvent(
          timestamp: _timestamp,
          sessionId: 'session-1',
          runtimeMeta: RuntimeMeta(
            contextId: 'diff-1',
            contextTitle: 'README diff',
            targetPath: '/workspace/README.md',
          ),
          raw: {'type': 'file_diff'},
          path: '/workspace/README.md',
          title: 'README diff',
          diff: '@@ -1 +1 @@',
          lang: 'markdown',
        ),
      );
      service.emit(
        AgentStateEvent(
          timestamp: _timestamp,
          sessionId: 'session-1',
          runtimeMeta: RuntimeMeta(command: 'claude', blockingKind: 'review'),
          raw: {'type': 'agent_state'},
          state: 'WAIT_INPUT',
          message: '等待审核',
          awaitInput: true,
          command: 'claude',
        ),
      );
      service.emit(
        PromptRequestEvent(
          timestamp: _timestamp,
          sessionId: 'session-1',
          runtimeMeta: const RuntimeMeta(
            contextId: 'diff-1',
            contextTitle: 'README diff',
            targetPath: '/workspace/README.md',
            blockingKind: 'review',
          ),
          raw: const {
            'type': 'prompt_request',
            'msg': 'Please accept, revert, or revise this diff',
          },
          message: 'Please accept, revert, or revise this diff',
          options: const [
            PromptOption(value: 'accept', label: '接受'),
            PromptOption(value: 'revert', label: '撤销'),
            PromptOption(value: 'revise', label: '继续修改'),
          ],
        ),
      );
      await _flushEvents();

      controller.submitPromptOption('accept');

      expect(service.sentPayloads, hasLength(1));
      final payload = service.sentPayloads.single;
      expect(payload['action'], 'review_decision');
      expect(payload['decision'], 'accept');
      expect(payload['is_review_only'], isTrue);
    });

    test('diff 同意后会退出 review 交互并恢复普通输入', () async {
      final service = _FakeMobileVcWsService();
      final controller = SessionController(service: service);
      await controller.initialize();
      addTearDown(controller.disposeController);

      service.emit(
        _reviewDiffEvent(
          contextId: 'diff-accept-1',
          path: '/workspace/lib/main.dart',
          title: 'main.dart',
          groupId: 'group-accept-1',
          groupTitle: '组一',
        ),
      );
      service.emit(
        AgentStateEvent(
          timestamp: _timestamp,
          sessionId: 'session-1',
          runtimeMeta: const RuntimeMeta(command: 'claude'),
          raw: const {'type': 'agent_state'},
          state: 'WAIT_INPUT',
          message: '等待审核',
          awaitInput: true,
          command: 'claude',
        ),
      );
      service.emit(
        InteractionRequestEvent(
          timestamp: _timestamp,
          sessionId: 'session-1',
          runtimeMeta: const RuntimeMeta(command: 'claude'),
          raw: const {
            'type': 'interaction_request',
            'kind': 'review',
            'title': 'Review required',
            'message': '请处理 diff',
          },
          kind: 'review',
          title: 'Review required',
          message: '请处理 diff',
          options: const [
            PromptOption(value: 'accept'),
            PromptOption(value: 'revert'),
            PromptOption(value: 'revise'),
          ],
        ),
      );
      await _flushEvents();

      expect(controller.shouldShowReviewChoices, isTrue);
      expect(controller.pendingInteraction?.isReview, isTrue);

      controller.submitPromptOption('accept');

      expect(service.sentPayloads, hasLength(1));
      expect(service.sentPayloads.single['action'], 'review_decision');
      expect(controller.pendingInteraction, isNull);
      expect(controller.pendingPrompt, isNull);
      expect(controller.shouldShowReviewChoices, isFalse);

      service.sentPayloads.clear();
      controller.submitPromptOption('继续执行');

      expect(service.sentPayloads, hasLength(1));
      expect(service.sentPayloads.single['action'], 'input');
      expect(service.sentPayloads.single['data'], '继续执行\n');
    });

    test('diff 后收到空 prompt_request 仍进入可审核状态', () async {
      final service = _FakeMobileVcWsService();
      final controller = SessionController(service: service);
      await controller.initialize();
      addTearDown(controller.disposeController);

      service.emit(
        FileDiffEvent(
          timestamp: _timestamp,
          sessionId: 'session-1',
          runtimeMeta: const RuntimeMeta(
            command: 'codex',
            engine: 'codex',
            contextId: 'diff-blank-prompt',
            contextTitle: 'README diff',
            targetPath: '/workspace/README.md',
          ),
          raw: const {'type': 'file_diff'},
          path: '/workspace/README.md',
          title: 'README diff',
          diff: '@@ -1 +1 @@',
          lang: 'markdown',
        ),
      );
      service.emit(
        AgentStateEvent(
          timestamp: _timestamp,
          sessionId: 'session-1',
          runtimeMeta: const RuntimeMeta(
            command: 'codex',
            engine: 'codex',
            contextId: 'diff-blank-prompt',
          ),
          raw: const {'type': 'agent_state'},
          state: 'WAIT_INPUT',
          message: '等待审核',
          awaitInput: true,
          command: 'codex',
        ),
      );
      service.emit(
        PromptRequestEvent(
          timestamp: _timestamp,
          sessionId: 'session-1',
          runtimeMeta: const RuntimeMeta(
            command: 'codex',
            engine: 'codex',
            contextId: 'diff-blank-prompt',
            targetPath: '/workspace/README.md',
          ),
          raw: const {'type': 'prompt_request'},
          message: '',
          options: const [],
        ),
      );
      await _flushEvents();

      expect(controller.awaitInput, isTrue);
      expect(controller.shouldShowReviewChoices, isTrue);
      expect(controller.currentReviewDiff?.path, '/workspace/README.md');
    });

    test('plan prompt 会进入计划阻塞态并显示首个问题', () async {
      final service = _FakeMobileVcWsService();
      final controller = SessionController(service: service);
      await controller.initialize();
      addTearDown(controller.disposeController);

      await controller.connect();
      service.emit(
        InteractionRequestEvent(
          timestamp: _timestamp,
          sessionId: 'session-1',
          runtimeMeta: const RuntimeMeta(
            command: 'claude',
            contextId: 'plan-1',
            targetPath: '/workspace/plan.md',
          ),
          raw: const {
            'type': 'interaction_request',
            'kind': 'plan',
            'title': 'Plan required',
            'message': '请完成计划选择',
          },
          kind: 'plan',
          title: 'Plan required',
          message: '请完成计划选择',
          planQuestions: const [
            PlanQuestion(
              id: 'q1',
              title: '选择实现方式',
              message: '请先选择实现方向',
              options: [
                PromptOption(value: 'a', label: '方案 A'),
                PromptOption(value: 'b', label: '方案 B'),
              ],
            ),
          ],
        ),
      );
      await _flushEvents();

      expect(controller.pendingInteraction?.isPlan, isTrue);
      expect(controller.shouldShowPlanChoices, isTrue);
      expect(controller.pendingPlanQuestion?.id, 'q1');
      expect(controller.pendingPlanProgressLabel, '1/1');
      final signal = _expectSignal(controller, ActionNeededType.plan);
      expect(signal.message, 'AI 助手需要你完成计划选择');
    });

    test('单问题 plan 选择会发送 plan_decision', () async {
      final service = _FakeMobileVcWsService();
      final controller = SessionController(service: service);
      await controller.initialize();
      addTearDown(controller.disposeController);

      await controller.connect();
      service.emit(
        InteractionRequestEvent(
          timestamp: _timestamp,
          sessionId: 'session-1',
          runtimeMeta: const RuntimeMeta(
            command: 'claude',
            contextId: 'plan-1',
            targetPath: '/workspace/plan.md',
          ),
          raw: const {
            'type': 'interaction_request',
            'kind': 'plan',
            'title': 'Plan required',
            'message': '请完成计划选择',
          },
          kind: 'plan',
          title: 'Plan required',
          message: '请完成计划选择',
          planQuestions: const [
            PlanQuestion(
              id: 'q1',
              title: '选择实现方式',
              message: '请先选择实现方向',
              options: [
                PromptOption(value: 'a', label: '方案 A'),
                PromptOption(value: 'b', label: '方案 B'),
              ],
            ),
          ],
        ),
      );
      await _flushEvents();
      service.sentPayloads.clear();

      controller.submitPromptOption('a');

      expect(service.sentPayloads, hasLength(1));
      final payload = service.sentPayloads.single;
      expect(payload['action'], 'plan_decision');
      expect(payload['decision'], isA<String>());
      expect(payload['decision'], contains('"kind":"plan"'));
      expect(payload['decision'], contains('"q1"'));
      expect(payload['decision'], contains('"方案 A"'));
      expect(controller.pendingInteraction, isNull);
      expect(controller.shouldShowPlanChoices, isFalse);
    });

    test('多问题 plan 会先本地收集再统一提交', () async {
      final service = _FakeMobileVcWsService();
      final controller = SessionController(service: service);
      await controller.initialize();
      addTearDown(controller.disposeController);

      await controller.connect();
      service.emit(
        InteractionRequestEvent(
          timestamp: _timestamp,
          sessionId: 'session-1',
          runtimeMeta: const RuntimeMeta(
            command: 'claude',
            contextId: 'plan-2',
            targetPath: '/workspace/plan.md',
          ),
          raw: const {
            'type': 'interaction_request',
            'kind': 'plan',
            'title': 'Plan required',
            'message': '请完成计划选择',
          },
          kind: 'plan',
          title: 'Plan required',
          message: '请完成计划选择',
          planQuestions: const [
            PlanQuestion(
              id: 'q1',
              title: '选择实现方式',
              message: '请先选择实现方向',
              options: [
                PromptOption(value: 'a', label: '方案 A'),
                PromptOption(value: 'b', label: '方案 B'),
              ],
            ),
            PlanQuestion(
              id: 'q2',
              title: '选择验证方式',
              message: '请再选择验证方向',
              options: [
                PromptOption(value: 'c', label: '方案 C'),
                PromptOption(value: 'd', label: '方案 D'),
              ],
            ),
          ],
        ),
      );
      await _flushEvents();
      service.sentPayloads.clear();

      controller.submitPromptOption('a');

      expect(service.sentPayloads, isEmpty);
      expect(controller.pendingPlanQuestion?.id, 'q2');
      expect(controller.pendingPlanProgressLabel, '2/2');
      expect(controller.pendingPlanAnswers['q1'], '方案 A');
      expect(controller.shouldShowPlanChoices, isTrue);

      controller.submitPromptOption('方案 D');

      expect(service.sentPayloads, hasLength(1));
      final payload = service.sentPayloads.single;
      expect(payload['action'], 'plan_decision');
      final decision = payload['decision'] as String;
      expect(decision, contains('"kind":"plan"'));
      expect(decision, contains('"q1":"方案 A"'));
      expect(decision, contains('"q2":"方案 D"'));
      expect(controller.pendingPlanQuestion, isNull);
      expect(controller.shouldShowPlanChoices, isFalse);
    });

    test('普通 prompt 继续发送 input', () async {
      final service = _FakeMobileVcWsService();
      final controller = SessionController(service: service);
      await controller.initialize();
      addTearDown(controller.disposeController);

      service.emit(
        PromptRequestEvent(
          timestamp: _timestamp,
          sessionId: 'session-1',
          runtimeMeta: const RuntimeMeta(command: 'claude'),
          raw: const {
            'type': 'prompt_request',
            'msg': '请输入补充说明',
          },
          message: '请输入补充说明',
          options: const [],
        ),
      );
      service.emit(
        AgentStateEvent(
          timestamp: _timestamp,
          sessionId: 'session-1',
          runtimeMeta: const RuntimeMeta(command: 'claude'),
          raw: const {'type': 'agent_state'},
          state: 'WAIT_INPUT',
          message: '等待输入',
          awaitInput: true,
          command: 'claude',
        ),
      );
      await _flushEvents();

      controller.submitPromptOption('补充说明');

      expect(service.sentPayloads, hasLength(1));
      final payload = service.sentPayloads.single;
      expect(payload['action'], 'input');
      expect(payload['data'], '补充说明\n');
    });

    test('仅有 pendingInteraction 时 awaitInput == true', () async {
      final service = _FakeMobileVcWsService();
      final controller = SessionController(service: service);
      await controller.initialize();
      addTearDown(controller.disposeController);

      expect(controller.awaitInput, isFalse);

      service.emit(
        InteractionRequestEvent(
          timestamp: _timestamp,
          sessionId: 'session-1',
          runtimeMeta: const RuntimeMeta(command: 'claude'),
          raw: const {
            'type': 'interaction_request',
            'kind': 'permission',
            'title': 'Permission required',
            'message': 'Claude needs permission to write README.md',
          },
          kind: 'permission',
          title: 'Permission required',
          message: 'Claude needs permission to write README.md',
          options: const [PromptOption(value: 'y'), PromptOption(value: 'n')],
        ),
      );
      await _flushEvents();

      expect(controller.awaitInput, isTrue);
    });

    test('pendingInteraction permission 场景下普通输入会被拦截等待顶部授权', () async {
      final service = _FakeMobileVcWsService();
      final controller = SessionController(service: service);
      await controller.initialize();
      addTearDown(controller.disposeController);

      await controller.saveConfig(
        const AppConfig(
          cwd: '/workspace',
          engine: 'claude',
          permissionMode: 'default',
        ),
      );

      service.emit(
        RuntimePhaseEvent(
          timestamp: _timestamp,
          sessionId: 'session-1',
          runtimeMeta: RuntimeMeta(command: 'claude'),
          raw: const {'type': 'runtime_phase'},
          phase: 'permission_blocked',
          kind: 'permission',
          message: 'Claude needs permission to write README.md',
        ),
      );
      service.emit(
        RuntimePhaseEvent(
          timestamp: _timestamp,
          sessionId: 'session-1',
          runtimeMeta: RuntimeMeta(command: 'claude'),
          raw: const {'type': 'runtime_phase'},
          phase: 'permission_blocked',
          kind: 'permission',
          message: 'Claude needs permission to write README.md',
        ),
      );
      service.emit(
        InteractionRequestEvent(
          timestamp: _timestamp,
          sessionId: 'session-1',
          runtimeMeta: const RuntimeMeta(
            command: 'claude',
            contextId: 'ctx-1',
            targetPath: '/workspace/README.md',
          ),
          raw: const {
            'type': 'interaction_request',
            'kind': 'permission',
            'title': 'Permission required',
            'message': 'Claude needs permission to write README.md',
          },
          kind: 'permission',
          title: 'Permission required',
          message: 'Claude needs permission to write README.md',
          options: const [PromptOption(value: 'y'), PromptOption(value: 'n')],
        ),
      );
      await _flushEvents();

      controller.sendInputText('允许');

      expect(service.sentPayloads, isEmpty);
      expect(controller.timeline.last.kind, 'session');
      expect(controller.timeline.last.body, '请先在上方完成授权');
    });

    test('文件编辑权限 interaction 不会被通用 ready prompt 冲掉', () async {
      final service = _FakeMobileVcWsService();
      final controller = SessionController(service: service);
      await controller.initialize();
      addTearDown(controller.disposeController);

      await controller.connect();
      service.emit(
        FSReadResultEvent(
          timestamp: _timestamp,
          sessionId: 'session-1',
          runtimeMeta: const RuntimeMeta(command: 'claude'),
          raw: const {'type': 'fs_read_result'},
          result: const FileReadResult(
            path: '/workspace/README.md',
            content: '# MobileVC\n',
            isText: true,
          ),
        ),
      );
      service.emit(
        RuntimePhaseEvent(
          timestamp: _timestamp,
          sessionId: 'session-1',
          runtimeMeta: const RuntimeMeta(command: 'claude'),
          raw: const {'type': 'runtime_phase'},
          phase: 'permission_blocked',
          kind: 'permission',
          message: 'Allow edit README.md?',
        ),
      );
      service.emit(
        InteractionRequestEvent(
          timestamp: _timestamp,
          sessionId: 'session-1',
          runtimeMeta: const RuntimeMeta(
            command: 'claude',
            contextId: 'edit-1',
            contextTitle: 'README.md',
            targetPath: '/workspace/README.md',
          ),
          raw: const {
            'type': 'interaction_request',
            'kind': 'permission',
            'title': 'Permission required',
            'message': 'Allow edit README.md?',
          },
          kind: 'permission',
          title: 'Permission required',
          message: 'Allow edit README.md?',
          options: const [PromptOption(value: 'y'), PromptOption(value: 'n')],
        ),
      );
      await _flushEvents();
      expect(controller.pendingInteraction?.message, 'Allow edit README.md?');
      expect(controller.shouldShowPermissionChoices, isTrue);

      service.emit(
        PromptRequestEvent(
          timestamp: _timestamp.add(const Duration(seconds: 1)),
          sessionId: 'session-1',
          runtimeMeta:
              const RuntimeMeta(command: 'claude', blockingKind: 'ready'),
          raw: const {'type': 'prompt_request', 'msg': 'Claude 会话已就绪，可继续输入'},
          message: 'Claude 会话已就绪，可继续输入',
          options: const [],
        ),
      );
      await _flushEvents();

      expect(controller.pendingInteraction?.message, 'Allow edit README.md?');
      expect(controller.shouldShowPermissionChoices, isTrue);

      service.sentPayloads.clear();
      controller.continueWithCurrentFile('允许并继续');

      expect(service.sentPayloads, hasLength(1));
      expect(service.sentPayloads.single['action'], 'permission_decision');
      expect(service.sentPayloads.single['decision'], 'approve');
    });
  });

  group('Bug fix: activityBannerTitle dynamic status', () {
    test('returns default "运行中" when no step summary or phase label', () async {
      final service = _FakeMobileVcWsService();
      final controller = SessionController(service: service);
      await controller.initialize();
      addTearDown(controller.disposeController);

      expect(controller.activityBannerTitle, '未连接');

      await controller.connect();
      // After connect, no agent state yet — should show phase label or default
      final title = controller.activityBannerTitle;
      // Connected but no activity, should not return the old static text
      expect(title, isNot('AI 助手正在运行中'));
    });

    test('reflects current step summary from agent_state', () async {
      final service = _FakeMobileVcWsService();
      final controller = SessionController(service: service);
      await controller.initialize();
      addTearDown(controller.disposeController);

      await controller.connect();
      service.emit(SessionCreatedEvent(
        timestamp: _timestamp,
        sessionId: 'session-1',
        runtimeMeta: const RuntimeMeta(),
        raw: const {'type': 'session_created'},
        summary: const SessionSummary(id: 'session-1', title: 'Test'),
      ));
      await _flushEvents();

      // Send "claude" to trigger pending launch
      controller.sendInputText('claude');
      await _flushEvents();

      // Should show "待输入" when pending input
      expect(controller.activityBannerTitle, '待输入');

      // Simulate backend response with thinking state
      service.emit(AgentStateEvent(
        timestamp: _timestamp,
        sessionId: 'session-1',
        runtimeMeta:
            const RuntimeMeta(command: 'claude', executionId: 'exec-1'),
        raw: const {'type': 'agent_state'},
        state: 'THINKING',
        message: '分析需求中',
        command: 'claude',
      ));
      await _flushEvents();

      // After agent_state THINKING, phase label should be "思考中"
      expect(controller.agentPhaseLabel, '思考中');

      // Step summary from syncStepSummary should be set
      final title = controller.activityBannerTitle;
      expect(title, isNotEmpty);
      expect(title, isNot('AI 助手正在运行中'));
    });
  });

  group('Bug fix: _isDefinitiveAgentState session states', () {
    test('sessionState THINKING produces visible activity', () async {
      final service = _FakeMobileVcWsService();
      final controller = SessionController(service: service);
      await controller.initialize();
      addTearDown(controller.disposeController);

      await controller.connect();
      service.emit(SessionCreatedEvent(
        timestamp: _timestamp,
        sessionId: 'session-1',
        runtimeMeta: const RuntimeMeta(),
        raw: const {'type': 'session_created'},
        summary: const SessionSummary(id: 'session-1', title: 'Test'),
      ));
      await _flushEvents();

      // Simulate a session that is THINKING
      service.emit(SessionStateEvent(
        timestamp: _timestamp,
        sessionId: 'session-1',
        runtimeMeta:
            const RuntimeMeta(executionId: 'exec-1', command: 'claude'),
        raw: const {'type': 'session_state'},
        state: 'THINKING',
        message: 'thinking...',
      ));
      await _flushEvents();

      // Activity should be visible with sessionState=THINKING
      expect(controller.activityVisible, isTrue);
      expect(controller.activityBannerVisible, isTrue);
    });

    test('sessionState RUNNING produces visible activity', () async {
      final service = _FakeMobileVcWsService();
      final controller = SessionController(service: service);
      await controller.initialize();
      addTearDown(controller.disposeController);

      await controller.connect();
      service.emit(SessionCreatedEvent(
        timestamp: _timestamp,
        sessionId: 'session-1',
        runtimeMeta: const RuntimeMeta(),
        raw: const {'type': 'session_created'},
        summary: const SessionSummary(id: 'session-1', title: 'Test'),
      ));
      await _flushEvents();

      // Set execution active first
      controller.sendInputText('claude');
      await _flushEvents();

      // Simulate session_state RUNNING
      service.emit(SessionStateEvent(
        timestamp: _timestamp,
        sessionId: 'session-1',
        runtimeMeta:
            const RuntimeMeta(executionId: 'exec-1', command: 'claude'),
        raw: const {'type': 'session_state'},
        state: 'RUNNING',
        message: 'running...',
      ));
      await _flushEvents();

      // Session state RUNNING should keep activity visible
      expect(controller.activityVisible, isTrue);
    });
  });

  group('AI status indicator', () {
    test('follows backend ai_status and ignores stale snapshot', () async {
      final service = _FakeMobileVcWsService();
      final controller = SessionController(service: service);
      await controller.initialize();
      addTearDown(controller.disposeController);

      await controller.connect();
      service.emit(SessionCreatedEvent(
        timestamp: _timestamp,
        sessionId: 'session-1',
        runtimeMeta: const RuntimeMeta(),
        raw: const {'type': 'session_created'},
        summary: const SessionSummary(id: 'session-1', title: 'Test'),
      ));
      service.emit(AgentStateEvent(
        timestamp: _timestamp,
        sessionId: 'session-1',
        runtimeMeta: const RuntimeMeta(
          command: 'claude',
          claudeLifecycle: 'waiting_input',
          executionId: 'exec-inline-stale',
        ),
        raw: const {'type': 'agent_state'},
        state: 'WAIT_INPUT',
        message: '等待输入',
        awaitInput: true,
        command: 'claude',
      ));
      await _flushEvents();

      controller.sendInputText('hello');
      await _flushEvents();

      service.emit(AIStatusEvent(
        timestamp: _timestamp.add(const Duration(milliseconds: 50)),
        sessionId: 'session-1',
        runtimeMeta: const RuntimeMeta(
            command: 'claude', executionId: 'exec-inline-stale'),
        raw: const {'type': 'ai_status'},
        visible: true,
        label: '思考中',
        phase: 'thinking',
      ));
      await _flushEvents();

      expect(controller.aiStatusIndicatorVisible, isTrue);
      expect(controller.aiStatusIndicatorLabel, '思考中');

      service.emit(TaskSnapshotEvent(
        timestamp: _timestamp.add(const Duration(milliseconds: 100)),
        sessionId: 'session-1',
        runtimeMeta:
            const RuntimeMeta(command: 'claude', claudeLifecycle: 'resumable'),
        raw: const {'type': 'task_snapshot'},
        state: 'IDLE',
        message: 'Task resumable',
        runtimeAlive: false,
        command: 'claude',
      ));
      await _flushEvents();

      expect(controller.aiStatusIndicatorVisible, isTrue);
      expect(controller.aiStatusIndicatorLabel, '思考中');
    });

    test('hides when backend ai_status marks settled', () async {
      final service = _FakeMobileVcWsService();
      final controller = SessionController(service: service);
      await controller.initialize();
      addTearDown(controller.disposeController);

      await controller.connect();
      service.emit(AIStatusEvent(
        timestamp: _timestamp,
        sessionId: 'session-1',
        runtimeMeta:
            const RuntimeMeta(command: 'claude', executionId: 'exec-inline-1'),
        raw: const {'type': 'ai_status'},
        visible: true,
        label: '思考中',
        phase: 'thinking',
      ));
      await _flushEvents();

      expect(controller.aiStatusIndicatorVisible, isTrue);

      service.emit(AIStatusEvent(
        timestamp: _timestamp.add(const Duration(seconds: 1)),
        sessionId: 'session-1',
        runtimeMeta:
            const RuntimeMeta(command: 'claude', executionId: 'exec-inline-1'),
        raw: const {'type': 'ai_status'},
        visible: false,
        phase: 'settled',
      ));
      await _flushEvents();
      await Future<void>.delayed(const Duration(milliseconds: 650));

      expect(controller.aiStatusIndicatorVisible, isFalse);
    });

    test('keeps submitted turn visible through stale waiting_input status',
        () async {
      final service = _FakeMobileVcWsService();
      final controller = SessionController(service: service);
      await controller.initialize();
      addTearDown(controller.disposeController);

      await controller.connect();
      service.emit(SessionCreatedEvent(
        timestamp: _timestamp,
        sessionId: 'session-1',
        runtimeMeta: const RuntimeMeta(),
        raw: const {'type': 'session_created'},
        summary: const SessionSummary(id: 'session-1', title: 'Test'),
      ));
      service.emit(AgentStateEvent(
        timestamp: _timestamp,
        sessionId: 'session-1',
        runtimeMeta: const RuntimeMeta(
          command: 'claude',
          claudeLifecycle: 'waiting_input',
        ),
        raw: const {'type': 'agent_state'},
        state: 'WAIT_INPUT',
        message: '等待输入',
        awaitInput: true,
        command: 'claude',
      ));
      await _flushEvents();

      controller.sendInputText('hello');
      await _flushEvents();

      expect(controller.aiStatusIndicatorVisible, isTrue);

      service.emit(AIStatusEvent(
        timestamp: _timestamp.add(const Duration(milliseconds: 100)),
        sessionId: 'session-1',
        runtimeMeta: const RuntimeMeta(
          command: 'claude',
          claudeLifecycle: 'waiting_input',
        ),
        raw: const {'type': 'ai_status'},
        visible: false,
        phase: 'waiting_input',
      ));
      await _flushEvents();

      expect(controller.aiStatusIndicatorVisible, isTrue);

      service.emit(PromptRequestEvent(
        timestamp: _timestamp.add(const Duration(seconds: 1)),
        sessionId: 'session-1',
        runtimeMeta: const RuntimeMeta(
          command: 'claude',
          claudeLifecycle: 'waiting_input',
          blockingKind: 'ready',
        ),
        raw: const {'type': 'prompt_request', 'msg': '等待输入'},
        message: '等待输入',
      ));
      service.emit(AIStatusEvent(
        timestamp: _timestamp.add(const Duration(seconds: 1)),
        sessionId: 'session-1',
        runtimeMeta: const RuntimeMeta(
          command: 'claude',
          claudeLifecycle: 'waiting_input',
        ),
        raw: const {'type': 'ai_status'},
        visible: false,
        phase: 'waiting_input',
      ));
      await _flushEvents();
      await Future<void>.delayed(const Duration(milliseconds: 650));

      expect(controller.aiStatusIndicatorVisible, isFalse);
    });

    test('提交后 SessionDelta 携带 stale waiting_input 不应熄灭状态球', () async {
      final service = _FakeMobileVcWsService();
      final controller = SessionController(service: service);
      await controller.initialize();
      addTearDown(controller.disposeController);

      await controller.connect();
      service.emit(SessionCreatedEvent(
        timestamp: _timestamp,
        sessionId: 'session-1',
        runtimeMeta: const RuntimeMeta(),
        raw: const {'type': 'session_created'},
        summary: const SessionSummary(id: 'session-1', title: 'Test'),
      ));
      service.emit(AgentStateEvent(
        timestamp: _timestamp,
        sessionId: 'session-1',
        runtimeMeta: const RuntimeMeta(
          command: 'claude',
          claudeLifecycle: 'waiting_input',
        ),
        raw: const {'type': 'agent_state'},
        state: 'WAIT_INPUT',
        message: '等待输入',
        awaitInput: true,
        command: 'claude',
      ));
      await _flushEvents();

      controller.sendInputText('hello');
      await _flushEvents();
      expect(controller.aiStatusIndicatorVisible, isTrue);

      // 后端紧接着回流 delta，resumeRuntimeMeta 仍是上一轮残留的 waiting_input。
      service.emit(SessionDeltaEvent(
        timestamp: _timestamp.add(const Duration(milliseconds: 30)),
        sessionId: 'session-1',
        runtimeMeta: const RuntimeMeta(command: 'claude'),
        raw: const {'type': 'session_delta'},
        summary: const SessionSummary(id: 'session-1', title: 'Test'),
        base: const SessionDeltaKnown(),
        latest: const SessionDeltaKnown(),
        resumeRuntimeMeta: const RuntimeMeta(
          command: 'claude',
          claudeLifecycle: 'waiting_input',
        ),
      ));
      await _flushEvents();

      expect(controller.aiStatusIndicatorVisible, isTrue,
          reason: '用户提交保护锁应屏蔽 stale waiting_input 的强制熄灭');
    });

    test('keeps submitted turn visible after fresh run then stale hide',
        () async {
      final service = _FakeMobileVcWsService();
      final controller = SessionController(service: service);
      await controller.initialize();
      addTearDown(controller.disposeController);

      await controller.connect();
      service.emit(SessionCreatedEvent(
        timestamp: _timestamp,
        sessionId: 'session-1',
        runtimeMeta: const RuntimeMeta(),
        raw: const {'type': 'session_created'},
        summary: const SessionSummary(id: 'session-1', title: 'Test'),
      ));
      service.emit(AgentStateEvent(
        timestamp: _timestamp,
        sessionId: 'session-1',
        runtimeMeta: const RuntimeMeta(
          command: 'claude',
          claudeLifecycle: 'waiting_input',
          executionId: 'exec-old',
        ),
        raw: const {'type': 'agent_state'},
        state: 'WAIT_INPUT',
        message: '等待输入',
        awaitInput: true,
        command: 'claude',
      ));
      await _flushEvents();

      controller.sendInputText('hello');
      await _flushEvents();

      expect(controller.aiStatusIndicatorVisible, isTrue);

      service.emit(AgentStateEvent(
        timestamp: _timestamp.add(const Duration(milliseconds: 50)),
        sessionId: 'session-1',
        runtimeMeta: const RuntimeMeta(
          command: 'claude',
          claudeLifecycle: 'active',
          executionId: 'exec-new',
        ),
        raw: const {'type': 'agent_state'},
        state: 'THINKING',
        message: '思考中',
        command: 'claude',
      ));
      service.emit(AIStatusEvent(
        timestamp: _timestamp.add(const Duration(milliseconds: 60)),
        sessionId: 'session-1',
        runtimeMeta: const RuntimeMeta(
          command: 'claude',
          claudeLifecycle: 'active',
          executionId: 'exec-new',
        ),
        raw: const {'type': 'ai_status'},
        visible: true,
        label: '思考中',
        phase: 'thinking',
      ));
      service.emit(AIStatusEvent(
        timestamp: _timestamp.add(const Duration(milliseconds: 70)),
        sessionId: 'session-1',
        runtimeMeta: const RuntimeMeta(
          command: 'claude',
          claudeLifecycle: 'waiting_input',
          executionId: 'exec-old',
        ),
        raw: const {'type': 'ai_status'},
        visible: false,
        phase: 'waiting_input',
      ));
      await _flushEvents();
      await Future<void>.delayed(const Duration(milliseconds: 650));

      expect(controller.aiStatusIndicatorVisible, isTrue);

      service.emit(LogEvent(
        timestamp: _timestamp.add(const Duration(seconds: 1)),
        sessionId: 'session-1',
        runtimeMeta:
            const RuntimeMeta(command: 'claude', executionId: 'exec-new'),
        raw: const {'type': 'log'},
        message: '处理好了，可以继续。',
        stream: 'stdout',
      ));
      service.emit(AIStatusEvent(
        timestamp: _timestamp.add(const Duration(seconds: 1)),
        sessionId: 'session-1',
        runtimeMeta:
            const RuntimeMeta(command: 'claude', executionId: 'exec-new'),
        raw: const {'type': 'ai_status'},
        visible: false,
        phase: 'settled',
      ));
      await _flushEvents();
      await Future<void>.delayed(const Duration(milliseconds: 650));

      expect(controller.aiStatusIndicatorVisible, isFalse);
    });

    test('codex 第二轮无 markdown 日志时 settled 会结束提交保护', () async {
      final service = _FakeMobileVcWsService();
      final controller = SessionController(service: service);
      await controller.initialize();
      addTearDown(controller.disposeController);

      await controller.connect();
      service.emit(SessionCreatedEvent(
        timestamp: _timestamp,
        sessionId: 'session-1',
        runtimeMeta: const RuntimeMeta(),
        raw: const {'type': 'session_created'},
        summary: const SessionSummary(id: 'session-1', title: 'Test'),
      ));
      service.emit(AgentStateEvent(
        timestamp: _timestamp,
        sessionId: 'session-1',
        runtimeMeta: const RuntimeMeta(
          command: 'codex',
          engine: 'codex',
          executionId: 'exec-old',
          claudeLifecycle: 'waiting_input',
        ),
        raw: const {'type': 'agent_state'},
        state: 'WAIT_INPUT',
        message: '等待输入',
        awaitInput: true,
        command: 'codex',
      ));
      await _flushEvents();

      controller.sendInputText('继续第二轮');
      await _flushEvents();

      expect(controller.aiStatusIndicatorVisible, isTrue);

      service.emit(AIStatusEvent(
        timestamp: _timestamp.add(const Duration(milliseconds: 100)),
        sessionId: 'session-1',
        runtimeMeta: const RuntimeMeta(
          command: 'codex',
          engine: 'codex',
          executionId: 'exec-new',
          claudeLifecycle: 'active',
        ),
        raw: const {'type': 'ai_status'},
        visible: true,
        label: '思考中',
        phase: 'thinking',
      ));
      service.emit(AIStatusEvent(
        timestamp: _timestamp.add(const Duration(seconds: 1)),
        sessionId: 'session-1',
        runtimeMeta: const RuntimeMeta(
          command: 'codex',
          engine: 'codex',
          executionId: 'exec-new',
          claudeLifecycle: 'settled',
        ),
        raw: const {'type': 'ai_status'},
        visible: false,
        phase: 'settled',
      ));
      await _flushEvents();
      await Future<void>.delayed(const Duration(milliseconds: 650));

      expect(controller.aiStatusIndicatorVisible, isFalse);
    });

    test('does not use completed step as active status text', () async {
      final service = _FakeMobileVcWsService();
      final controller = SessionController(service: service);
      await controller.initialize();
      addTearDown(controller.disposeController);

      await controller.connect();
      service.emit(SessionCreatedEvent(
        timestamp: _timestamp,
        sessionId: 'session-1',
        runtimeMeta: const RuntimeMeta(),
        raw: const {'type': 'session_created'},
        summary: const SessionSummary(id: 'session-1', title: 'Test'),
      ));
      service.emit(StepUpdateEvent(
        timestamp: _timestamp.add(const Duration(milliseconds: 10)),
        sessionId: 'session-1',
        runtimeMeta: const RuntimeMeta(command: 'codex', engine: 'codex'),
        raw: const {'type': 'step_update'},
        message: '正在运行命令',
        status: 'running',
        target: 'command',
        tool: 'commandExecution',
        command: 'codex',
      ));
      await _flushEvents();

      expect(controller.currentStepSummary, '正在运行命令');

      service.emit(StepUpdateEvent(
        timestamp: _timestamp.add(const Duration(milliseconds: 20)),
        sessionId: 'session-1',
        runtimeMeta: const RuntimeMeta(command: 'codex', engine: 'codex'),
        raw: const {'type': 'step_update'},
        message: 'command completed',
        status: 'done',
        target: 'command',
        tool: 'commandExecution',
        command: 'codex',
      ));
      await _flushEvents();

      expect(controller.currentStepSummary, '正在运行命令');

      service.emit(SessionDeltaEvent(
        timestamp: _timestamp.add(const Duration(milliseconds: 30)),
        sessionId: 'session-1',
        runtimeMeta: const RuntimeMeta(command: 'codex', engine: 'codex'),
        raw: const {'type': 'session_delta'},
        summary: const SessionSummary(id: 'session-1', title: 'Test'),
        currentStep: const HistoryContext(
          type: 'step',
          message: 'Completed command',
          status: 'done',
          title: 'Completed command',
        ),
      ));
      await _flushEvents();

      expect(controller.currentStepSummary, '正在运行命令');
    });

    test('ignores completed ai_status labels from stale backend events',
        () async {
      final service = _FakeMobileVcWsService();
      final controller = SessionController(service: service);
      await controller.initialize();
      addTearDown(controller.disposeController);

      await controller.connect();
      service.emit(AIStatusEvent(
        timestamp: _timestamp,
        sessionId: 'session-1',
        runtimeMeta: const RuntimeMeta(command: 'codex', engine: 'codex'),
        raw: const {'type': 'ai_status'},
        visible: true,
        label: '思考中',
        phase: 'thinking',
      ));
      await _flushEvents();

      service.emit(AIStatusEvent(
        timestamp: _timestamp.add(const Duration(milliseconds: 10)),
        sessionId: 'session-1',
        runtimeMeta: const RuntimeMeta(command: 'codex', engine: 'codex'),
        raw: const {'type': 'ai_status'},
        visible: true,
        label: 'Completed command',
        phase: 'running_tool',
      ));
      await _flushEvents();

      expect(controller.aiStatusIndicatorVisible, isTrue);
      expect(controller.aiStatusIndicatorLabel, '思考中');
    });

    test('hides stale status when restored runtime is waiting input', () async {
      final service = _FakeMobileVcWsService();
      final controller = SessionController(service: service);
      await controller.initialize();
      addTearDown(controller.disposeController);

      await controller.connect();
      service.emit(AIStatusEvent(
        timestamp: _timestamp,
        sessionId: 'session-1',
        runtimeMeta: const RuntimeMeta(command: 'codex', engine: 'codex'),
        raw: const {'type': 'ai_status'},
        visible: true,
        label: '思考中',
        phase: 'thinking',
      ));
      await _flushEvents();

      service.emit(SessionHistoryEvent(
        timestamp: _timestamp.add(const Duration(milliseconds: 10)),
        sessionId: 'session-1',
        runtimeMeta: const RuntimeMeta(),
        raw: const {'type': 'session_history'},
        summary: const SessionSummary(id: 'session-1', title: 'Test'),
        runtimeAlive: true,
        resumeRuntimeMeta: const RuntimeMeta(
          command: 'codex',
          engine: 'codex',
          claudeLifecycle: 'waiting_input',
        ),
      ));
      await _flushEvents();

      expect(controller.aiStatusIndicatorVisible, isFalse);

      service.emit(AIStatusEvent(
        timestamp: _timestamp.add(const Duration(milliseconds: 20)),
        sessionId: 'session-1',
        runtimeMeta: const RuntimeMeta(command: 'codex', engine: 'codex'),
        raw: const {'type': 'ai_status'},
        visible: true,
        label: '思考中',
        phase: 'thinking',
      ));
      service.emit(SessionDeltaEvent(
        timestamp: _timestamp.add(const Duration(milliseconds: 30)),
        sessionId: 'session-1',
        runtimeMeta: const RuntimeMeta(),
        raw: const {'type': 'session_delta'},
        summary: const SessionSummary(id: 'session-1', title: 'Test'),
        runtimeAlive: true,
        resumeRuntimeMeta: const RuntimeMeta(
          command: 'codex',
          engine: 'codex',
          claudeLifecycle: 'waiting_input',
        ),
      ));
      await _flushEvents();

      expect(controller.aiStatusIndicatorVisible, isFalse);
    });

    test('shows concrete tool detail while Claude is running a tool', () async {
      final service = _FakeMobileVcWsService();
      final controller = SessionController(service: service);
      await controller.initialize();
      addTearDown(controller.disposeController);

      await controller.connect();
      service.emit(AIStatusEvent(
        timestamp: _timestamp,
        sessionId: 'session-1',
        runtimeMeta: const RuntimeMeta(
          command: 'claude',
          executionId: 'exec-inline-tool',
          targetPath: '/workspace/lib/main.dart',
        ),
        raw: const {'type': 'ai_status'},
        visible: true,
        label: '正在修改 · main.dart',
        phase: 'running_tool',
      ));
      await _flushEvents();

      expect(controller.aiStatusIndicatorVisible, isTrue);
      expect(controller.aiStatusIndicatorLabel, '正在修改 · main.dart');
    });

    test('does not derive visibility from agent_state alone', () async {
      final service = _FakeMobileVcWsService();
      final controller = SessionController(service: service);
      await controller.initialize();
      addTearDown(controller.disposeController);

      await controller.connect();
      service.emit(SessionCreatedEvent(
        timestamp: _timestamp,
        sessionId: 'session-1',
        runtimeMeta: const RuntimeMeta(),
        raw: const {'type': 'session_created'},
        summary: const SessionSummary(id: 'session-1', title: 'Test'),
      ));
      await _flushEvents();

      service.emit(AgentStateEvent(
        timestamp: _timestamp,
        sessionId: 'session-1',
        runtimeMeta:
            const RuntimeMeta(command: 'claude', executionId: 'exec-inline-2'),
        raw: const {'type': 'agent_state'},
        state: 'THINKING',
        message: '思考中',
        command: 'claude',
      ));
      await _flushEvents();

      expect(controller.aiStatusIndicatorVisible, isFalse);

      service.emit(AIStatusEvent(
        timestamp: _timestamp.add(const Duration(milliseconds: 100)),
        sessionId: 'session-1',
        runtimeMeta:
            const RuntimeMeta(command: 'claude', executionId: 'exec-inline-2'),
        raw: const {'type': 'ai_status'},
        visible: true,
        label: '正在读取 · README.md',
        phase: 'running_tool',
      ));
      await _flushEvents();

      expect(controller.aiStatusIndicatorVisible, isTrue);
      expect(controller.aiStatusIndicatorLabel, '正在读取 · README.md');
    });
  });

  group('Bug fix: delta/history do not overwrite during loading', () {
    test('SessionDeltaEvent does not overwrite selectedSessionId while loading',
        () async {
      final service = _FakeMobileVcWsService();
      final controller = SessionController(service: service);
      await controller.initialize();
      addTearDown(controller.disposeController);

      await controller.connect();
      // Simulate session list synced so auto-create can trigger
      service.emit(SessionListResultEvent(
        timestamp: _timestamp,
        sessionId: '',
        runtimeMeta: const RuntimeMeta(),
        raw: const {'type': 'session_list_result'},
        items: const [],
      ));
      await _flushEvents();

      // Trigger auto-create-session by sending 'claude' with no session
      controller.sendInputText('claude');
      await _flushEvents();

      // Now _isLoadingSession should be true (auto-create in progress)
      expect(controller.isLoadingSession, isTrue);

      // Simulate stale SessionDeltaEvent arriving during loading
      service.emit(SessionDeltaEvent(
        timestamp: _timestamp,
        sessionId: 'stale-session',
        runtimeMeta: const RuntimeMeta(),
        raw: const {'type': 'session_delta'},
        summary: const SessionSummary(id: 'stale-session', title: 'Stale'),
        appendLogEntries: const [],
        upsertDiffs: const [],
        reviewGroups: const [],
        rawTerminalByStream: const {},
        terminalExecutions: const [],
        base: const SessionDeltaKnown(),
        latest: const SessionDeltaKnown(),
        sessionContext: const SessionContext(),
        skillCatalogMeta: const CatalogMetadata(domain: 'skill'),
        memoryCatalogMeta: const CatalogMetadata(domain: 'memory'),
        resumeRuntimeMeta: const RuntimeMeta(),
        requiresFullSync: false,
        runtimeAlive: false,
        canResume: false,
      ));
      await _flushEvents();

      // selectedSessionId should NOT be 'stale-session' — guard prevented overwrite
      expect(controller.selectedSessionId, isNot('stale-session'));
      // Should still be loading
      expect(controller.isLoadingSession, isTrue);
    });

    test('SessionDeltaEvent for other session does not overwrite active chat',
        () async {
      final service = _FakeMobileVcWsService();
      final controller = SessionController(service: service);
      await controller.initialize();
      addTearDown(controller.disposeController);

      await controller.connect();
      service.emit(SessionCreatedEvent(
        timestamp: _timestamp,
        sessionId: 'conn-1',
        runtimeMeta: const RuntimeMeta(command: 'claude'),
        raw: const {'type': 'session_created'},
        summary: const SessionSummary(id: 'session-current', title: '当前会话'),
      ));
      await _flushEvents();
      service.emit(SessionHistoryEvent(
        timestamp: _timestamp,
        sessionId: 'session-current',
        runtimeMeta: const RuntimeMeta(command: 'claude', engine: 'claude'),
        raw: const {'type': 'session_history'},
        summary: const SessionSummary(id: 'session-current', title: 'Current'),
        logEntries: const [
          HistoryLogEntry(
            kind: 'assistant',
            message: '当前会话回复',
            label: 'Assistant',
          ),
        ],
        resumeRuntimeMeta: const RuntimeMeta(
          command: 'claude',
          engine: 'claude',
          claudeLifecycle: 'waiting_input',
        ),
        runtimeAlive: true,
      ));
      service.emit(AgentStateEvent(
        timestamp: _timestamp.add(const Duration(milliseconds: 100)),
        sessionId: 'session-current',
        runtimeMeta: const RuntimeMeta(
          command: 'claude',
          engine: 'claude',
          claudeLifecycle: 'waiting_input',
        ),
        raw: const {'type': 'agent_state'},
        state: 'WAIT_INPUT',
        message: '等待输入',
        command: 'claude',
        awaitInput: true,
      ));
      await _flushEvents();
      expect(controller.selectedSessionId, 'session-current');
      expect(controller.awaitInput, isTrue);

      service.emit(SessionDeltaEvent(
        timestamp: _timestamp.add(const Duration(milliseconds: 200)),
        sessionId: 'session-other',
        runtimeMeta: const RuntimeMeta(command: 'codex', engine: 'codex'),
        raw: const {'type': 'session_delta'},
        summary: const SessionSummary(id: 'session-other', title: 'Other'),
        appendLogEntries: const [
          HistoryLogEntry(
            kind: 'assistant',
            message: '其他会话回复',
            label: 'Assistant',
          ),
        ],
        base: const SessionDeltaKnown(),
        latest: const SessionDeltaKnown(eventCursor: 9, logEntryCount: 1),
        resumeRuntimeMeta: const RuntimeMeta(
          command: 'codex',
          engine: 'codex',
          claudeLifecycle: 'active',
        ),
        runtimeAlive: true,
      ));
      await _flushEvents();

      expect(controller.selectedSessionId, 'session-current');
      expect(controller.awaitInput, isTrue);
      expect(controller.shouldShowClaudeMode, isTrue);
      expect(
        controller.timeline.any((item) => item.body == '其他会话回复'),
        isFalse,
      );

      service.sentPayloads.clear();
      controller.sendInputText('继续当前会话');
      await _flushEvents();

      final inputPayload = service.sentPayloads.singleWhere(
        (payload) => payload['action'] == 'input',
      );
      expect(inputPayload['sessionId'], 'session-current');
      expect(inputPayload['data'], '继续当前会话\n');
    });

    test(
        'SessionHistoryEvent does not overwrite selectedSessionId while loading',
        () async {
      final service = _FakeMobileVcWsService();
      final controller = SessionController(service: service);
      await controller.initialize();
      addTearDown(controller.disposeController);

      await controller.connect();
      service.emit(SessionListResultEvent(
        timestamp: _timestamp,
        sessionId: '',
        runtimeMeta: const RuntimeMeta(),
        raw: const {'type': 'session_list_result'},
        items: const [],
      ));
      await _flushEvents();

      // Trigger auto-create
      controller.sendInputText('claude');
      await _flushEvents();

      expect(controller.isLoadingSession, isTrue);

      // Simulate stale SessionHistoryEvent
      service.emit(SessionHistoryEvent(
        timestamp: _timestamp,
        sessionId: 'stale-history-session',
        runtimeMeta: const RuntimeMeta(),
        raw: const {'type': 'session_history'},
        summary: const SessionSummary(
            id: 'stale-history-session', title: 'Stale History'),
        logEntries: const [],
        diffs: const [],
        reviewGroups: const [],
        rawTerminalByStream: const {},
        terminalExecutions: const [],
        sessionContext: const SessionContext(),
        skillCatalogMeta: const CatalogMetadata(domain: 'skill'),
        memoryCatalogMeta: const CatalogMetadata(domain: 'memory'),
        resumeRuntimeMeta: const RuntimeMeta(),
        runtimeAlive: false,
        canResume: false,
        currentStep: null,
        latestError: null,
        activeReviewGroup: null,
      ));
      await _flushEvents();

      // selectedSessionId should NOT be overwritten
      expect(controller.selectedSessionId, isNot('stale-history-session'));
      expect(controller.isLoadingSession, isTrue);
    });

    test('SessionHistoryEvent for other session does not overwrite active chat',
        () async {
      final service = _FakeMobileVcWsService();
      final controller = SessionController(service: service);
      await controller.initialize();
      addTearDown(controller.disposeController);

      await controller.connect();
      service.emit(SessionCreatedEvent(
        timestamp: _timestamp,
        sessionId: 'conn-1',
        runtimeMeta: const RuntimeMeta(command: 'claude'),
        raw: const {'type': 'session_created'},
        summary: const SessionSummary(id: 'session-current', title: '当前会话'),
      ));
      await _flushEvents();
      service.sentPayloads.clear();

      service.emit(SessionHistoryEvent(
        timestamp: _timestamp,
        sessionId: 'session-current',
        runtimeMeta: const RuntimeMeta(command: 'claude', engine: 'claude'),
        raw: const {'type': 'session_history'},
        summary: const SessionSummary(id: 'session-current', title: 'Current'),
        logEntries: const [
          HistoryLogEntry(
            kind: 'assistant',
            message: '当前会话回复',
            label: 'Assistant',
          ),
        ],
        resumeRuntimeMeta: const RuntimeMeta(
          command: 'claude',
          engine: 'claude',
          claudeLifecycle: 'waiting_input',
        ),
        runtimeAlive: true,
      ));
      service.emit(AgentStateEvent(
        timestamp: _timestamp.add(const Duration(milliseconds: 100)),
        sessionId: 'session-current',
        runtimeMeta: const RuntimeMeta(
          command: 'claude',
          engine: 'claude',
          claudeLifecycle: 'waiting_input',
        ),
        raw: const {'type': 'agent_state'},
        state: 'WAIT_INPUT',
        message: '等待输入',
        command: 'claude',
        awaitInput: true,
      ));
      await _flushEvents();
      expect(controller.selectedSessionId, 'session-current');
      expect(controller.awaitInput, isTrue);

      service.emit(SessionHistoryEvent(
        timestamp: _timestamp.add(const Duration(milliseconds: 200)),
        sessionId: 'session-other',
        runtimeMeta: const RuntimeMeta(command: 'codex', engine: 'codex'),
        raw: const {'type': 'session_history'},
        summary: const SessionSummary(id: 'session-other', title: 'Other'),
        logEntries: const [
          HistoryLogEntry(
            kind: 'assistant',
            message: '其他会话全量历史',
            label: 'Assistant',
          ),
        ],
        resumeRuntimeMeta: const RuntimeMeta(
          command: 'codex',
          engine: 'codex',
          claudeLifecycle: 'active',
        ),
        runtimeAlive: true,
      ));
      await _flushEvents();

      expect(controller.selectedSessionId, 'session-current');
      expect(controller.awaitInput, isTrue);
      expect(controller.shouldShowClaudeMode, isTrue);
      expect(
        controller.timeline.any((item) => item.body == '其他会话全量历史'),
        isFalse,
      );

      service.sentPayloads.clear();
      controller.sendInputText('继续当前会话');
      await _flushEvents();

      final inputPayload = service.sentPayloads.singleWhere(
        (payload) => payload['action'] == 'input',
      );
      expect(inputPayload['sessionId'], 'session-current');
      expect(inputPayload['data'], '继续当前会话\n');
    });

    test('other session runtime events do not overwrite active chat context',
        () async {
      final service = _FakeMobileVcWsService();
      final controller = SessionController(service: service);
      await controller.initialize();
      addTearDown(controller.disposeController);

      await controller.connect();
      service.emit(SessionHistoryEvent(
        timestamp: _timestamp,
        sessionId: 'session-current',
        runtimeMeta: const RuntimeMeta(command: 'claude', engine: 'claude'),
        raw: const {'type': 'session_history'},
        summary: const SessionSummary(id: 'session-current', title: 'Current'),
        logEntries: const [
          HistoryLogEntry(
            kind: 'assistant',
            message: '当前会话回复',
            label: 'Assistant',
          ),
        ],
        resumeRuntimeMeta: const RuntimeMeta(
          command: 'claude',
          engine: 'claude',
          claudeLifecycle: 'waiting_input',
        ),
        runtimeAlive: true,
      ));
      service.emit(AgentStateEvent(
        timestamp: _timestamp.add(const Duration(milliseconds: 100)),
        sessionId: 'session-current',
        runtimeMeta: const RuntimeMeta(
          command: 'claude',
          engine: 'claude',
          claudeLifecycle: 'waiting_input',
        ),
        raw: const {'type': 'agent_state'},
        state: 'WAIT_INPUT',
        message: '等待输入',
        command: 'claude',
        awaitInput: true,
      ));
      await _flushEvents();
      expect(controller.selectedSessionId, 'session-current');
      expect(controller.awaitInput, isTrue);

      service.emit(RuntimePhaseEvent(
        timestamp: _timestamp.add(const Duration(milliseconds: 200)),
        sessionId: 'session-other',
        runtimeMeta: const RuntimeMeta(command: 'claude'),
        raw: const {'type': 'runtime_phase'},
        phase: 'permission_blocked',
        kind: 'permission',
        message: '其他会话需要授权',
      ));
      service.emit(LogEvent(
        timestamp: _timestamp.add(const Duration(milliseconds: 300)),
        sessionId: 'session-other',
        runtimeMeta: const RuntimeMeta(command: 'codex', engine: 'codex'),
        raw: const {'type': 'log'},
        message: '其他会话输出',
        stream: 'stdout',
      ));
      service.emit(ProgressEvent(
        timestamp: _timestamp.add(const Duration(milliseconds: 400)),
        sessionId: 'session-other',
        runtimeMeta: const RuntimeMeta(command: 'codex', engine: 'codex'),
        raw: const {'type': 'progress'},
        message: '其他会话进度',
        percent: 50,
      ));
      service.emit(PromptRequestEvent(
        timestamp: _timestamp.add(const Duration(milliseconds: 500)),
        sessionId: 'session-other',
        runtimeMeta: const RuntimeMeta(
          command: 'claude',
          blockingKind: 'permission',
          targetPath: '/workspace/other.dart',
        ),
        raw: const {'type': 'prompt_request'},
        message: 'Allow edit other.dart?',
        options: const [PromptOption(value: 'y'), PromptOption(value: 'n')],
      ));
      service.emit(InteractionRequestEvent(
        timestamp: _timestamp.add(const Duration(milliseconds: 600)),
        sessionId: 'session-other',
        runtimeMeta: const RuntimeMeta(
          command: 'claude',
          contextId: 'other-permission',
          targetPath: '/workspace/other.dart',
        ),
        raw: const {'type': 'interaction_request'},
        kind: 'permission',
        title: 'Permission required',
        message: '其他会话需要授权',
        options: const [PromptOption(value: 'y'), PromptOption(value: 'n')],
      ));
      service.emit(StepUpdateEvent(
        timestamp: _timestamp.add(const Duration(milliseconds: 700)),
        sessionId: 'session-other',
        runtimeMeta: const RuntimeMeta(command: 'codex', engine: 'codex'),
        raw: const {'type': 'step_update'},
        message: '其他会话步骤',
        status: 'running',
        target: 'command',
        command: 'codex',
      ));
      service.emit(ReviewStateEvent(
        timestamp: _timestamp.add(const Duration(milliseconds: 800)),
        sessionId: 'session-other',
        runtimeMeta: const RuntimeMeta(command: 'codex', engine: 'codex'),
        raw: const {'type': 'review_state'},
        groups: const [
          ReviewGroup(
            id: 'other-review',
            title: 'Other review',
            pendingReview: true,
            pendingCount: 1,
          ),
        ],
      ));
      service.emit(FileDiffEvent(
        timestamp: _timestamp.add(const Duration(milliseconds: 900)),
        sessionId: 'session-other',
        runtimeMeta: const RuntimeMeta(
          command: 'codex',
          engine: 'codex',
          contextId: 'other-diff',
          targetPath: '/workspace/other.dart',
        ),
        raw: const {'type': 'file_diff'},
        path: '/workspace/other.dart',
        title: 'Other diff',
        diff: '@@ -1 +1 @@\n-old\n+new',
        lang: 'dart',
      ));
      service.emit(SessionContextResultEvent(
        timestamp: _timestamp.add(const Duration(milliseconds: 1000)),
        sessionId: 'session-other',
        runtimeMeta: const RuntimeMeta(command: 'codex', engine: 'codex'),
        raw: const {'type': 'session_context_result'},
        sessionContext: const SessionContext(
          enabledSkillNames: ['other-skill'],
          enabledMemoryIds: ['other-memory'],
        ),
      ));
      await _flushEvents();

      expect(controller.selectedSessionId, 'session-current');
      expect(controller.awaitInput, isTrue);
      expect(controller.shouldShowClaudeMode, isTrue);
      expect(controller.shouldShowPermissionChoices, isFalse);
      expect(controller.shouldShowReviewChoices, isFalse);
      expect(controller.terminalStdout, isEmpty);
      expect(controller.currentStepSummary, isNot('其他会话进度'));
      expect(controller.currentStepSummary, isNot('其他会话步骤'));
      expect(controller.reviewGroups, isEmpty);
      expect(controller.enabledSkillSummary, isEmpty);
      expect(controller.enabledMemorySummary, isEmpty);
      expect(
        controller.timeline.any((item) => item.body == '其他会话输出'),
        isFalse,
      );
      expect(
        controller.timeline.any((item) => item.body == '/workspace/other.dart'),
        isFalse,
      );

      service.sentPayloads.clear();
      controller.sendInputText('继续当前会话');
      await _flushEvents();

      final inputPayload = service.sentPayloads.singleWhere(
        (payload) => payload['action'] == 'input',
      );
      expect(inputPayload['sessionId'], 'session-current');
      expect(inputPayload['data'], '继续当前会话\n');
    });

    test('loadSession 后匹配的 SessionHistoryEvent 必须还原 timeline', () async {
      final service = _FakeMobileVcWsService();
      final controller = SessionController(service: service);
      await controller.initialize();
      addTearDown(controller.disposeController);

      await controller.connect();

      controller.loadSession('session-target');
      expect(controller.isLoadingSession, isTrue);
      expect(controller.timeline, isEmpty);

      service.emit(SessionHistoryEvent(
        timestamp: _timestamp,
        sessionId: 'session-target',
        runtimeMeta: const RuntimeMeta(command: 'claude'),
        raw: const {'type': 'session_history'},
        summary: const SessionSummary(id: 'session-target', title: '历史会话'),
        logEntries: const [
          HistoryLogEntry(
            kind: 'markdown',
            message: '历史里的助手回复',
            timestamp: '2026-01-01T00:00:00Z',
          ),
        ],
        resumeRuntimeMeta: const RuntimeMeta(
          command: 'claude',
          claudeLifecycle: 'resumable',
        ),
        runtimeAlive: false,
        canResume: true,
      ));
      await _flushEvents();

      expect(controller.isLoadingSession, isFalse,
          reason: '匹配 target 的 history 应该被处理而不是被早返回丢弃');
      expect(controller.selectedSessionId, 'session-target');
      expect(controller.timeline, isNotEmpty,
          reason: '主界面不能在 loadSession 之后仍停留在 logo');
      expect(controller.timeline.any((item) => item.body.contains('历史里的助手回复')),
          isTrue);
    });

    test('session_history 使用时间升序恢复 timeline，避免旧记录叠到最新消息后面', () async {
      final service = _FakeMobileVcWsService();
      final controller = SessionController(service: service);
      await controller.initialize();
      addTearDown(controller.disposeController);

      await controller.connect();
      service.emit(SessionHistoryEvent(
        timestamp: _timestamp,
        sessionId: 'session-current',
        runtimeMeta: const RuntimeMeta(command: 'claude'),
        raw: const {'type': 'session_history'},
        summary: const SessionSummary(id: 'session-current', title: '当前会话'),
        logEntries: const [
          HistoryLogEntry(
            kind: 'markdown',
            message: 'Android release APK 已重新打包成功',
            timestamp: '2026-05-27T05:53:00Z',
          ),
          HistoryLogEntry(
            kind: 'user',
            message: 'LAN-first 和 Relay fallback 方案',
            timestamp: '2026-05-27T05:20:00Z',
          ),
        ],
        resumeRuntimeMeta: RuntimeMeta(command: 'claude'),
      ));
      await _flushEvents();

      expect(
        controller.timeline.map((item) => item.body).toList(),
        containsAllInOrder([
          'LAN-first 和 Relay fallback 方案',
          'Android release APK 已重新打包成功',
        ]),
      );
    });

    test('loadSession 请求首屏历史窗口，避免一次加载完整长会话', () async {
      final service = _FakeMobileVcWsService();
      final controller = SessionController(service: service);
      await controller.initialize();
      addTearDown(controller.disposeController);

      await controller.connect();
      service.sentPayloads.clear();
      controller.loadSession('session-window');

      expect(service.sentPayloads, hasLength(1));
      expect(service.sentPayloads.single['action'], 'session_load');
      expect(service.sentPayloads.single['sessionId'], 'session-window');
      expect(service.sentPayloads.single['limit'], 120);
    });

    test('connect 阶段延后非关键 bootstrap，避免 relay loading burst', () async {
      final service = _FakeMobileVcWsService();
      final controller = SessionController(service: service);
      await controller.initialize();
      addTearDown(controller.disposeController);

      await controller.connect();
      final initialActions =
          service.sentPayloads.map((payload) => payload['action']).toList();
      expect(initialActions, contains('fs_list'));
      expect(initialActions, contains('session_list'));
      expect(initialActions, isNot(contains('session_delta_get')),
          reason: 'no selected session exists during initial connect');
      expect(initialActions, isNot(contains('runtime_info')));
      expect(initialActions, isNot(contains('skill_catalog_get')));
      expect(initialActions, isNot(contains('memory_list')));
      expect(initialActions, isNot(contains('adb_devices')));
      expect(initialActions, isNot(contains('session_context_get')));
      expect(initialActions, isNot(contains('permission_rule_list')));
      expect(initialActions, isNot(contains('session_diff_page_get')));
      expect(initialActions, isNot(contains('task_snapshot_get')));
      expect(initialActions, isNot(contains('context_window_usage_get')));

      await Future<void>.delayed(const Duration(milliseconds: 520));
      await _flushEvents();

      final delayedActions =
          service.sentPayloads.map((payload) => payload['action']).toList();
      expect(delayedActions, contains('runtime_info'));
      expect(delayedActions, contains('skill_catalog_get'));
      expect(delayedActions, contains('memory_list'));
      expect(delayedActions, contains('adb_devices'));
      expect(delayedActions, contains('session_context_get'));
      expect(delayedActions, contains('permission_rule_list'));
      expect(delayedActions, contains('task_snapshot_get'));
      expect(delayedActions, contains('context_window_usage_get'));
      expect(delayedActions, isNot(contains('relay_device_list')));
    });

    test('会话历史窗口使用配置里的条数', () async {
      final service = _FakeMobileVcWsService();
      final controller = SessionController(service: service);
      await controller.initialize();
      addTearDown(controller.disposeController);

      await controller.saveConfig(const AppConfig(historyWindowLimit: 240));
      await controller.connect();
      service.sentPayloads.clear();
      controller.loadSession('session-window');

      expect(service.sentPayloads.single['action'], 'session_load');
      expect(service.sentPayloads.single['limit'], 240);

      service.emit(SessionHistoryEvent(
        timestamp: _timestamp,
        sessionId: 'session-window',
        runtimeMeta: const RuntimeMeta(command: 'claude'),
        raw: const {'type': 'session_history'},
        summary: const SessionSummary(id: 'session-window', title: '当前会话'),
        logEntryStart: 20,
        logEntryTotal: 260,
        hasMoreBefore: true,
        resumeRuntimeMeta: const RuntimeMeta(cwd: '/workspace'),
      ));
      await _flushEvents();

      service.sentPayloads.clear();
      controller.loadOlderTimelineEntries();
      expect(service.sentPayloads.single['action'], 'session_history_page');
      expect(service.sentPayloads.single['limit'], 240);

      service.sentPayloads.clear();
      controller.resumeConnectionIfNeeded();
      await _flushEvents();
      expect(
        service.sentPayloads.any((payload) =>
            payload['action'] == 'session_resume' && payload['limit'] == 240),
        isTrue,
      );
    });

    test('session_history_page 会把更早历史插入 timeline 前面', () async {
      final service = _FakeMobileVcWsService();
      final controller = SessionController(service: service);
      await controller.initialize();
      addTearDown(controller.disposeController);

      await controller.connect();
      service.emit(SessionHistoryEvent(
        timestamp: _timestamp,
        sessionId: 'session-current',
        runtimeMeta: const RuntimeMeta(command: 'claude'),
        raw: const {'type': 'session_history'},
        summary: const SessionSummary(id: 'session-current', title: '当前会话'),
        logEntryStart: 2,
        logEntryTotal: 4,
        hasMoreBefore: true,
        logEntries: const [
          HistoryLogEntry(
            kind: 'markdown',
            message: '第三条',
            timestamp: '2026-05-27T05:20:03Z',
          ),
          HistoryLogEntry(
            kind: 'markdown',
            message: '第四条',
            timestamp: '2026-05-27T05:20:04Z',
          ),
        ],
        resumeRuntimeMeta: RuntimeMeta(command: 'claude'),
      ));
      await _flushEvents();

      expect(controller.hasOlderTimelineEntries, isTrue);
      service.sentPayloads.clear();
      controller.loadOlderTimelineEntries();
      expect(service.sentPayloads.single['action'], 'session_history_page');
      expect(service.sentPayloads.single['before'], 2);
      expect(service.sentPayloads.single['limit'], 120);

      service.emit(SessionHistoryPageEvent(
        timestamp: _timestamp,
        sessionId: 'session-current',
        runtimeMeta: const RuntimeMeta(command: 'claude'),
        raw: const {'type': 'session_history_page'},
        logEntryStart: 0,
        logEntryTotal: 4,
        hasMoreBefore: false,
        logEntries: const [
          HistoryLogEntry(
            kind: 'markdown',
            message: '第一条',
            timestamp: '2026-05-27T05:20:01Z',
          ),
          HistoryLogEntry(
            kind: 'markdown',
            message: '第二条',
            timestamp: '2026-05-27T05:20:02Z',
          ),
        ],
        resumeRuntimeMeta: RuntimeMeta(command: 'claude'),
      ));
      await _flushEvents();

      expect(
        controller.timeline.map((item) => item.body).toList(),
        ['第一条第二条', '第三条第四条'],
      );
      expect(controller.hasOlderTimelineEntries, isFalse);
      expect(controller.isLoadingOlderTimelineEntries, isFalse);
    });

    test('session_history 会折叠 Codex 原生工具事件', () async {
      final service = _FakeMobileVcWsService();
      final controller = SessionController(service: service);
      await controller.initialize();
      addTearDown(controller.disposeController);

      await controller.connect();
      service.emit(SessionHistoryEvent(
        timestamp: _timestamp,
        sessionId: 'session-current',
        runtimeMeta: const RuntimeMeta(command: 'codex', engine: 'codex'),
        raw: const {'type': 'session_history'},
        summary: const SessionSummary(id: 'session-current', title: '当前会话'),
        logEntries: const [
          HistoryLogEntry(
            kind: 'user',
            message: '看一下项目',
            timestamp: '2026-05-27T05:20:00Z',
          ),
          HistoryLogEntry(
            kind: 'system',
            message: '调用 shell',
            timestamp: '2026-05-27T05:20:01Z',
            context: HistoryContext(
              source: 'codex-native',
              type: 'codex_tool_call',
              tool: 'functions.exec_command',
              command:
                  '{"cmd":"sed -n \'1,80p\' internal/engine/codex_transport.go"}',
            ),
          ),
          HistoryLogEntry(
            kind: 'system',
            message: 'exit 0',
            timestamp: '2026-05-27T05:20:02Z',
            context: HistoryContext(
              source: 'codex-native',
              type: 'codex_tool_output',
              tool: 'functions.exec_command',
            ),
          ),
          HistoryLogEntry(
            kind: 'system',
            message: 'apply_patch success',
            timestamp: '2026-05-27T05:20:03Z',
            context: HistoryContext(
              source: 'codex-native',
              type: 'codex_patch',
              status: 'success',
            ),
          ),
          HistoryLogEntry(
            kind: 'markdown',
            message: '已经看完了',
            timestamp: '2026-05-27T05:20:04Z',
          ),
        ],
        resumeRuntimeMeta: const RuntimeMeta(command: 'codex', engine: 'codex'),
      ));
      await _flushEvents();

      expect(controller.timeline.map((item) => item.kind), [
        'user',
        'codex_tool_group',
        'markdown',
      ]);
      final group = controller.timeline.singleWhere(
        (item) => item.kind == 'codex_tool_group',
      );
      expect(group.status, contains('工具调用 1'));
      expect(group.status, contains('输出 1'));
      expect(group.status, contains('Patch 1'));
      expect(
          group.codexSteps,
          containsAll([
            '正在读取 codex_transport.go',
            '补丁结果：success',
          ]));
      expect(group.body, contains('## 工具调用 (1)'));
      expect(group.body, contains('## 工具输出 (1)'));
      expect(group.body, contains('## Patch (1)'));
      expect(group.body, contains('- **functions.exec_command**'));
      expect(
          controller.timeline.where((item) => item.kind == 'system'), isEmpty);
    });

    test('session_delta 会折叠 Codex 原生工具事件', () async {
      final service = _FakeMobileVcWsService();
      final controller = SessionController(service: service);
      await controller.initialize();
      addTearDown(controller.disposeController);

      await controller.connect();
      service.emit(SessionDeltaEvent(
        timestamp: _timestamp,
        sessionId: 'session-current',
        runtimeMeta: const RuntimeMeta(command: 'codex', engine: 'codex'),
        raw: const {'type': 'session_delta'},
        summary: const SessionSummary(id: 'session-current', title: '当前会话'),
        appendLogEntries: const [
          HistoryLogEntry(
            kind: 'system',
            message: 'task started',
            timestamp: '2026-05-27T05:21:01Z',
            context: HistoryContext(
              source: 'codex-native',
              type: 'codex_task',
              status: 'started',
            ),
          ),
          HistoryLogEntry(
            kind: 'system',
            message: '调用 shell',
            timestamp: '2026-05-27T05:21:02Z',
            context: HistoryContext(
              source: 'codex-native',
              type: 'codex_tool_call',
              tool: 'functions.exec_command',
            ),
          ),
          HistoryLogEntry(
            kind: 'system',
            message: 'exit 0',
            timestamp: '2026-05-27T05:21:03Z',
            context: HistoryContext(
              source: 'codex-native',
              type: 'codex_tool_output',
              tool: 'functions.exec_command',
            ),
          ),
        ],
        resumeRuntimeMeta: const RuntimeMeta(command: 'codex', engine: 'codex'),
      ));
      await _flushEvents();

      expect(controller.timeline, hasLength(1));
      final group = controller.timeline.single;
      expect(group.kind, 'codex_tool_group');
      expect(group.status, contains('工具调用 1'));
      expect(group.status, contains('输出 1'));
      expect(group.status, contains('任务状态 1'));
      expect(group.body, contains('## 任务状态 (1)'));
      expect(group.body, contains('## 工具调用 (1)'));
      expect(group.body, contains('## 工具输出 (1)'));
      expect(group.body, contains('- **started**'));
      expect(group.body, contains('- **functions.exec_command**'));
    });

    test('session_delta 补完成状态时更新 Codex 原生操作组而不是重复追加', () async {
      final service = _FakeMobileVcWsService();
      final controller = SessionController(service: service);
      await controller.initialize();
      addTearDown(controller.disposeController);

      const started = HistoryLogEntry(
        kind: 'system',
        message: 'task started',
        timestamp: '2026-05-27T05:21:01Z',
        context: HistoryContext(
          source: 'codex-native',
          type: 'codex_task',
          status: 'started',
        ),
      );
      const toolCall = HistoryLogEntry(
        kind: 'system',
        message: '调用 shell',
        timestamp: '2026-05-27T05:21:02Z',
        context: HistoryContext(
          source: 'codex-native',
          type: 'codex_tool_call',
          tool: 'functions.exec_command',
        ),
      );
      const completed = HistoryLogEntry(
        kind: 'system',
        message: 'task completed',
        timestamp: '2026-05-27T05:21:03Z',
        context: HistoryContext(
          source: 'codex-native',
          type: 'codex_task',
          status: 'completed',
        ),
      );

      await controller.connect();
      service.emit(SessionHistoryEvent(
        timestamp: _timestamp,
        sessionId: 'session-current',
        runtimeMeta: const RuntimeMeta(command: 'codex', engine: 'codex'),
        raw: const {'type': 'session_history'},
        summary: const SessionSummary(id: 'session-current', title: '当前会话'),
        logEntries: const [started, toolCall],
        latest: const SessionDeltaKnown(eventCursor: 2, logEntryCount: 2),
        resumeRuntimeMeta: const RuntimeMeta(command: 'codex', engine: 'codex'),
      ));
      await _flushEvents();

      expect(
        controller.timeline.where((item) => item.kind == 'codex_tool_group'),
        hasLength(1),
      );

      service.emit(SessionDeltaEvent(
        timestamp: _timestamp.add(const Duration(seconds: 1)),
        sessionId: 'session-current',
        runtimeMeta: const RuntimeMeta(command: 'codex', engine: 'codex'),
        raw: const {'type': 'session_delta'},
        summary: const SessionSummary(id: 'session-current', title: '当前会话'),
        base: const SessionDeltaKnown(eventCursor: 2, logEntryCount: 2),
        latest: const SessionDeltaKnown(eventCursor: 3, logEntryCount: 3),
        appendLogEntries: const [started, toolCall, completed],
        resumeRuntimeMeta: const RuntimeMeta(command: 'codex', engine: 'codex'),
      ));
      await _flushEvents();

      final groups = controller.timeline
          .where((item) => item.kind == 'codex_tool_group')
          .toList(growable: false);
      expect(groups, hasLength(1));
      expect(groups.single.codexSteps, contains('Codex 任务已完成'));
      expect(groups.single.body, contains('## 工具调用 (1)'));
      expect(groups.single.body, contains('## 任务状态 (2)'));
      expect('## 工具调用'.allMatches(groups.single.body), hasLength(1));
      expect(
        controller.timeline.map((item) => item.kind).toList(),
        ['codex_tool_group'],
      );
    });

    test('session_delta 只补 completed 时保留已有 Codex 原生操作细节', () async {
      final service = _FakeMobileVcWsService();
      final controller = SessionController(service: service);
      await controller.initialize();
      addTearDown(controller.disposeController);

      const toolCall = HistoryLogEntry(
        kind: 'system',
        message: '调用 shell',
        timestamp: '2026-05-27T05:22:02Z',
        context: HistoryContext(
          source: 'codex-native',
          type: 'codex_tool_call',
          tool: 'functions.exec_command',
          command: '{"cmd":"sed -n \'1,20p\' lib/main.dart"}',
        ),
      );
      const completed = HistoryLogEntry(
        kind: 'system',
        message: 'task completed',
        timestamp: '2026-05-27T05:22:06Z',
        context: HistoryContext(
          source: 'codex-native',
          type: 'codex_task',
          status: 'completed',
        ),
      );

      await controller.connect();
      service.emit(SessionHistoryEvent(
        timestamp: _timestamp,
        sessionId: 'session-current',
        runtimeMeta: const RuntimeMeta(command: 'codex', engine: 'codex'),
        raw: const {'type': 'session_history'},
        summary: const SessionSummary(id: 'session-current', title: '当前会话'),
        logEntries: const [toolCall],
        latest: const SessionDeltaKnown(eventCursor: 1, logEntryCount: 1),
        resumeRuntimeMeta: const RuntimeMeta(command: 'codex', engine: 'codex'),
      ));
      await _flushEvents();

      service.emit(SessionDeltaEvent(
        timestamp: _timestamp.add(const Duration(seconds: 1)),
        sessionId: 'session-current',
        runtimeMeta: const RuntimeMeta(command: 'codex', engine: 'codex'),
        raw: const {'type': 'session_delta'},
        summary: const SessionSummary(id: 'session-current', title: '当前会话'),
        base: const SessionDeltaKnown(eventCursor: 1, logEntryCount: 1),
        latest: const SessionDeltaKnown(eventCursor: 2, logEntryCount: 2),
        appendLogEntries: const [completed],
        resumeRuntimeMeta: const RuntimeMeta(command: 'codex', engine: 'codex'),
      ));
      await _flushEvents();

      final groups = controller.timeline
          .where((item) => item.kind == 'codex_tool_group')
          .toList(growable: false);
      expect(groups, hasLength(1));
      expect(groups.single.codexSteps, contains('正在读取 main.dart'));
      expect(groups.single.codexSteps, contains('Codex 任务已完成'));
      expect(groups.single.body, contains('## 工具调用 (1)'));
      expect(groups.single.body, contains('## 任务状态 (1)'));
    });

    test('session_delta 后到 completed 和旧回复片段不会追加到最终回答后', () async {
      final service = _FakeMobileVcWsService();
      final controller = SessionController(service: service);
      await controller.initialize();
      addTearDown(controller.disposeController);

      const meta = RuntimeMeta(
        command: 'codex',
        engine: 'codex',
        executionId: 'turn-completed-after-answer',
        contextId: 'turn-completed-after-answer',
      );
      const toolCall = HistoryLogEntry(
        kind: 'system',
        message: '调用 shell',
        timestamp: '2026-05-27T05:22:02Z',
        context: HistoryContext(
          source: 'codex-native',
          type: 'codex_tool_call',
          tool: 'functions.exec_command',
          command:
              '{"cmd":"dart analyze mobile_vc/lib/features/session/session_controller.dart"}',
        ),
      );
      const completed = HistoryLogEntry(
        kind: 'system',
        message: 'task completed',
        timestamp: '2026-05-27T05:22:03Z',
        context: HistoryContext(
          source: 'codex-native',
          type: 'codex_task',
          status: 'completed',
        ),
      );
      const staleReply = HistoryLogEntry(
        kind: 'markdown',
        message: 'dart analyze 已通过。',
        timestamp: '2026-05-27T05:22:04Z',
        executionId: 'turn-completed-after-answer',
        context: HistoryContext(
          id: 'turn-completed-after-answer',
          source: 'codex',
          type: 'assistant',
        ),
      );

      await controller.connect();
      service.emit(SessionHistoryEvent(
        timestamp: _timestamp,
        sessionId: 'session-current',
        runtimeMeta: meta,
        raw: const {'type': 'session_history'},
        summary: const SessionSummary(id: 'session-current', title: '当前会话'),
        logEntries: const [toolCall],
        latest: const SessionDeltaKnown(eventCursor: 1, logEntryCount: 1),
        resumeRuntimeMeta: meta,
      ));
      await _flushEvents();

      service.emit(LogEvent(
        timestamp: DateTime.parse('2026-05-27T05:22:05Z'),
        sessionId: 'session-current',
        runtimeMeta: meta,
        raw: const {'type': 'log'},
        message: '已提交，验证也都通过了。',
        stream: 'stdout',
      ));
      await _flushEvents();

      service.emit(SessionDeltaEvent(
        timestamp: _timestamp.add(const Duration(seconds: 1)),
        sessionId: 'session-current',
        runtimeMeta: meta,
        raw: const {'type': 'session_delta'},
        summary: const SessionSummary(id: 'session-current', title: '当前会话'),
        base: const SessionDeltaKnown(eventCursor: 1, logEntryCount: 1),
        latest: const SessionDeltaKnown(eventCursor: 3, logEntryCount: 3),
        appendLogEntries: const [completed, staleReply],
        resumeRuntimeMeta: meta,
      ));
      await _flushEvents();

      final groups = controller.timeline
          .where((item) => item.kind == 'codex_tool_group')
          .toList(growable: false);
      final markdownItems = controller.timeline
          .where((item) => item.kind == 'markdown')
          .toList(growable: false);

      expect(groups, hasLength(1));
      expect(groups.single.codexSteps, contains('Codex 任务已完成'));
      expect(markdownItems, hasLength(1));
      expect(markdownItems.single.body, '已提交，验证也都通过了。');
      expect(controller.timeline.last.body, '已提交，验证也都通过了。');
    });

    test('session_delta 后到无 turn id 的旧回复片段不会插回最终回答后', () async {
      final service = _FakeMobileVcWsService();
      final controller = SessionController(service: service);
      await controller.initialize();
      addTearDown(controller.disposeController);

      const toolCall = HistoryLogEntry(
        kind: 'system',
        message: '调用 shell',
        timestamp: '2026-05-27T05:22:02Z',
        context: HistoryContext(
          source: 'codex-native',
          type: 'codex_tool_call',
          tool: 'functions.exec_command',
          command:
              '{"cmd":"dart analyze mobile_vc/lib/features/session/session_controller.dart"}',
        ),
      );
      const completed = HistoryLogEntry(
        kind: 'system',
        message: 'task completed',
        timestamp: '2026-05-27T05:22:03Z',
        context: HistoryContext(
          source: 'codex-native',
          type: 'codex_task',
          status: 'completed',
        ),
      );
      const staleReply = HistoryLogEntry(
        kind: 'markdown',
        message: 'dart analyze 已通过。',
        timestamp: '2026-05-27T05:22:07Z',
        context: HistoryContext(
          source: 'codex',
          type: 'assistant',
        ),
      );

      await controller.connect();
      service.emit(SessionHistoryEvent(
        timestamp: _timestamp,
        sessionId: 'session-current',
        runtimeMeta: const RuntimeMeta(command: 'codex', engine: 'codex'),
        raw: const {'type': 'session_history'},
        summary: const SessionSummary(id: 'session-current', title: '当前会话'),
        logEntries: const [toolCall],
        latest: const SessionDeltaKnown(eventCursor: 1, logEntryCount: 1),
        resumeRuntimeMeta: const RuntimeMeta(command: 'codex', engine: 'codex'),
      ));
      await _flushEvents();

      service.emit(LogEvent(
        timestamp: DateTime.parse('2026-05-27T05:22:05Z'),
        sessionId: 'session-current',
        runtimeMeta: const RuntimeMeta(command: 'codex', engine: 'codex'),
        raw: const {'type': 'log'},
        message: '已提交，验证也都通过了。',
        stream: 'stdout',
      ));
      await _flushEvents();

      service.emit(SessionDeltaEvent(
        timestamp: _timestamp.add(const Duration(seconds: 1)),
        sessionId: 'session-current',
        runtimeMeta: const RuntimeMeta(command: 'codex', engine: 'codex'),
        raw: const {'type': 'session_delta'},
        summary: const SessionSummary(id: 'session-current', title: '当前会话'),
        base: const SessionDeltaKnown(eventCursor: 1, logEntryCount: 1),
        latest: const SessionDeltaKnown(eventCursor: 3, logEntryCount: 3),
        appendLogEntries: const [completed, staleReply],
        resumeRuntimeMeta: const RuntimeMeta(command: 'codex', engine: 'codex'),
      ));
      await _flushEvents();

      final markdownItems = controller.timeline
          .where((item) => item.kind == 'markdown')
          .toList(growable: false);

      expect(markdownItems, hasLength(1));
      expect(markdownItems.single.body, '已提交，验证也都通过了。');
      expect(controller.timeline.last.body, '已提交，验证也都通过了。');
    });

    test('session_delta 混合 thinking 时 completed 后旧回复仍不会插回最终回答后', () async {
      final service = _FakeMobileVcWsService();
      final controller = SessionController(service: service);
      await controller.initialize();
      addTearDown(controller.disposeController);

      const completed = HistoryLogEntry(
        kind: 'system',
        message: 'task completed',
        timestamp: '2026-05-27T05:22:03Z',
        context: HistoryContext(
          source: 'codex-native',
          type: 'codex_task',
          status: 'completed',
        ),
      );
      const thinking = HistoryLogEntry(
        kind: 'thinking',
        message: '正在复核 session_delta 渲染边界',
        timestamp: '2026-05-27T05:22:04Z',
        context: HistoryContext(
          id: 'codex-thinking-mixed-delta',
          source: 'codex',
          type: 'thinking',
        ),
      );
      const staleReply = HistoryLogEntry(
        kind: 'markdown',
        message: 'dart analyze 已通过。',
        timestamp: '2026-05-27T05:22:07Z',
        context: HistoryContext(
          source: 'codex',
          type: 'assistant',
        ),
      );

      await controller.connect();
      service.emit(SessionHistoryEvent(
        timestamp: _timestamp,
        sessionId: 'session-current',
        runtimeMeta: const RuntimeMeta(command: 'codex', engine: 'codex'),
        raw: const {'type': 'session_history'},
        summary: const SessionSummary(id: 'session-current', title: '当前会话'),
        latest: const SessionDeltaKnown(eventCursor: 1, logEntryCount: 1),
        resumeRuntimeMeta: const RuntimeMeta(command: 'codex', engine: 'codex'),
      ));
      await _flushEvents();

      service.emit(LogEvent(
        timestamp: DateTime.parse('2026-05-27T05:22:05Z'),
        sessionId: 'session-current',
        runtimeMeta: const RuntimeMeta(command: 'codex', engine: 'codex'),
        raw: const {'type': 'log'},
        message: '已提交，验证也都通过了。',
        stream: 'stdout',
      ));
      await _flushEvents();

      service.emit(SessionDeltaEvent(
        timestamp: _timestamp.add(const Duration(seconds: 1)),
        sessionId: 'session-current',
        runtimeMeta: const RuntimeMeta(command: 'codex', engine: 'codex'),
        raw: const {'type': 'session_delta'},
        summary: const SessionSummary(id: 'session-current', title: '当前会话'),
        base: const SessionDeltaKnown(eventCursor: 1, logEntryCount: 1),
        latest: const SessionDeltaKnown(eventCursor: 4, logEntryCount: 4),
        appendLogEntries: const [completed, thinking, staleReply],
        resumeRuntimeMeta: const RuntimeMeta(command: 'codex', engine: 'codex'),
      ));
      await _flushEvents();

      final markdownItems = controller.timeline
          .where((item) => item.kind == 'markdown')
          .toList(growable: false);
      final thinkingItems = controller.timeline
          .where((item) => item.kind == 'thinking')
          .toList(growable: false);

      expect(markdownItems, hasLength(1));
      expect(markdownItems.single.body, '已提交，验证也都通过了。');
      expect(thinkingItems, hasLength(1));
      expect(thinkingItems.single.body, '正在复核 session_delta 渲染边界');
      expect(controller.timeline.last.body, '已提交，验证也都通过了。');
    });

    test('session_delta 同批旧回复时间早于 completed 也不会插回最终回答后', () async {
      final service = _FakeMobileVcWsService();
      final controller = SessionController(service: service);
      await controller.initialize();
      addTearDown(controller.disposeController);

      const staleReply = HistoryLogEntry(
        kind: 'markdown',
        message: 'dart analyze 已通过。',
        timestamp: '2026-05-27T05:22:03Z',
        context: HistoryContext(
          source: 'codex',
          type: 'assistant',
        ),
      );
      const completed = HistoryLogEntry(
        kind: 'system',
        message: 'task completed',
        timestamp: '2026-05-27T05:22:04Z',
        context: HistoryContext(
          source: 'codex-native',
          type: 'codex_task',
          status: 'completed',
        ),
      );

      await controller.connect();
      service.emit(SessionHistoryEvent(
        timestamp: _timestamp,
        sessionId: 'session-current',
        runtimeMeta: const RuntimeMeta(command: 'codex', engine: 'codex'),
        raw: const {'type': 'session_history'},
        summary: const SessionSummary(id: 'session-current', title: '当前会话'),
        latest: const SessionDeltaKnown(eventCursor: 1, logEntryCount: 1),
        resumeRuntimeMeta: const RuntimeMeta(command: 'codex', engine: 'codex'),
      ));
      await _flushEvents();

      service.emit(LogEvent(
        timestamp: DateTime.parse('2026-05-27T05:22:05Z'),
        sessionId: 'session-current',
        runtimeMeta: const RuntimeMeta(command: 'codex', engine: 'codex'),
        raw: const {'type': 'log'},
        message: '已提交，验证也都通过了。',
        stream: 'stdout',
      ));
      await _flushEvents();

      service.emit(SessionDeltaEvent(
        timestamp: _timestamp.add(const Duration(seconds: 1)),
        sessionId: 'session-current',
        runtimeMeta: const RuntimeMeta(command: 'codex', engine: 'codex'),
        raw: const {'type': 'session_delta'},
        summary: const SessionSummary(id: 'session-current', title: '当前会话'),
        base: const SessionDeltaKnown(eventCursor: 1, logEntryCount: 1),
        latest: const SessionDeltaKnown(eventCursor: 3, logEntryCount: 3),
        appendLogEntries: const [staleReply, completed],
        resumeRuntimeMeta: const RuntimeMeta(command: 'codex', engine: 'codex'),
      ));
      await _flushEvents();

      final markdownItems = controller.timeline
          .where((item) => item.kind == 'markdown')
          .toList(growable: false);

      expect(markdownItems, hasLength(1));
      expect(markdownItems.single.body, '已提交，验证也都通过了。');
      expect(controller.timeline.last.body, '已提交，验证也都通过了。');
    });

    test('session_delta 同批包含当前轮 user 时仍会抑制 completed 旧回复', () async {
      final service = _FakeMobileVcWsService();
      final controller = SessionController(service: service);
      await controller.initialize();
      addTearDown(controller.disposeController);

      const currentUser = HistoryLogEntry(
        kind: 'user',
        message: '在仔细检查一小',
        timestamp: '2026-05-27T05:21:59Z',
      );
      const completed = HistoryLogEntry(
        kind: 'system',
        message: 'task completed',
        timestamp: '2026-05-27T05:22:03Z',
        context: HistoryContext(
          source: 'codex-native',
          type: 'codex_task',
          status: 'completed',
        ),
      );
      const staleReply = HistoryLogEntry(
        kind: 'markdown',
        message: 'dart analyze 已通过。',
        timestamp: '2026-05-27T05:22:07Z',
        context: HistoryContext(
          source: 'codex',
          type: 'assistant',
        ),
      );

      await controller.connect();
      service.emit(SessionHistoryEvent(
        timestamp: _timestamp,
        sessionId: 'session-current',
        runtimeMeta: const RuntimeMeta(command: 'codex', engine: 'codex'),
        raw: const {'type': 'session_history'},
        summary: const SessionSummary(id: 'session-current', title: '当前会话'),
        latest: const SessionDeltaKnown(eventCursor: 1, logEntryCount: 1),
        resumeRuntimeMeta: const RuntimeMeta(command: 'codex', engine: 'codex'),
      ));
      await _flushEvents();

      service.emit(LogEvent(
        timestamp: DateTime.parse('2026-05-27T05:22:05Z'),
        sessionId: 'session-current',
        runtimeMeta: const RuntimeMeta(command: 'codex', engine: 'codex'),
        raw: const {'type': 'log'},
        message: '已提交，验证也都通过了。',
        stream: 'stdout',
      ));
      await _flushEvents();

      service.emit(SessionDeltaEvent(
        timestamp: _timestamp.add(const Duration(seconds: 1)),
        sessionId: 'session-current',
        runtimeMeta: const RuntimeMeta(command: 'codex', engine: 'codex'),
        raw: const {'type': 'session_delta'},
        summary: const SessionSummary(id: 'session-current', title: '当前会话'),
        base: const SessionDeltaKnown(eventCursor: 1, logEntryCount: 1),
        latest: const SessionDeltaKnown(eventCursor: 4, logEntryCount: 4),
        appendLogEntries: const [currentUser, completed, staleReply],
        resumeRuntimeMeta: const RuntimeMeta(command: 'codex', engine: 'codex'),
      ));
      await _flushEvents();

      final markdownItems = controller.timeline
          .where((item) => item.kind == 'markdown')
          .toList(growable: false);

      expect(markdownItems, hasLength(1));
      expect(markdownItems.single.body, '已提交，验证也都通过了。');
      expect(
        controller.timeline.where((item) => item.kind == 'user').single.body,
        '在仔细检查一小',
      );
      expect(controller.timeline.last.body, '已提交，验证也都通过了。');
    });

    test('session_delta completed 后出现新 user 时不会抑制下一轮 assistant', () async {
      final service = _FakeMobileVcWsService();
      final controller = SessionController(service: service);
      await controller.initialize();
      addTearDown(controller.disposeController);

      const completed = HistoryLogEntry(
        kind: 'system',
        message: 'task completed',
        timestamp: '2026-05-27T05:22:03Z',
        context: HistoryContext(
          source: 'codex-native',
          type: 'codex_task',
          status: 'completed',
        ),
      );
      const staleReply = HistoryLogEntry(
        kind: 'markdown',
        message: 'dart analyze 已通过。',
        timestamp: '2026-05-27T05:22:04Z',
        context: HistoryContext(
          source: 'codex',
          type: 'assistant',
        ),
      );
      const nextUser = HistoryLogEntry(
        kind: 'user',
        message: '继续检查',
        timestamp: '2026-05-27T05:23:00Z',
      );
      const nextAssistant = HistoryLogEntry(
        kind: 'markdown',
        message: '继续检查也通过了。',
        timestamp: '2026-05-27T05:23:05Z',
        context: HistoryContext(
          source: 'codex',
          type: 'assistant',
        ),
      );

      await controller.connect();
      service.emit(SessionHistoryEvent(
        timestamp: _timestamp,
        sessionId: 'session-current',
        runtimeMeta: const RuntimeMeta(command: 'codex', engine: 'codex'),
        raw: const {'type': 'session_history'},
        summary: const SessionSummary(id: 'session-current', title: '当前会话'),
        latest: const SessionDeltaKnown(eventCursor: 1, logEntryCount: 1),
        resumeRuntimeMeta: const RuntimeMeta(command: 'codex', engine: 'codex'),
      ));
      await _flushEvents();

      service.emit(LogEvent(
        timestamp: DateTime.parse('2026-05-27T05:22:05Z'),
        sessionId: 'session-current',
        runtimeMeta: const RuntimeMeta(command: 'codex', engine: 'codex'),
        raw: const {'type': 'log'},
        message: '已提交，验证也都通过了。',
        stream: 'stdout',
      ));
      await _flushEvents();

      service.emit(SessionDeltaEvent(
        timestamp: _timestamp.add(const Duration(seconds: 1)),
        sessionId: 'session-current',
        runtimeMeta: const RuntimeMeta(command: 'codex', engine: 'codex'),
        raw: const {'type': 'session_delta'},
        summary: const SessionSummary(id: 'session-current', title: '当前会话'),
        base: const SessionDeltaKnown(eventCursor: 1, logEntryCount: 1),
        latest: const SessionDeltaKnown(eventCursor: 5, logEntryCount: 5),
        appendLogEntries: const [
          completed,
          staleReply,
          nextUser,
          nextAssistant,
        ],
        resumeRuntimeMeta: const RuntimeMeta(command: 'codex', engine: 'codex'),
      ));
      await _flushEvents();

      final markdownItems = controller.timeline
          .where((item) => item.kind == 'markdown')
          .toList(growable: false);

      expect(markdownItems, hasLength(2));
      expect(markdownItems.first.body, '已提交，验证也都通过了。');
      expect(markdownItems.last.body, '继续检查也通过了。');
      expect(
        controller.timeline.where((item) => item.kind == 'user').single.body,
        '继续检查',
      );
    });

    test('session_delta 后到无 turn id 的完整回复会更新实时前缀', () async {
      final service = _FakeMobileVcWsService();
      final controller = SessionController(service: service);
      await controller.initialize();
      addTearDown(controller.disposeController);

      const completed = HistoryLogEntry(
        kind: 'system',
        message: 'task completed',
        timestamp: '2026-05-27T05:22:06Z',
        context: HistoryContext(
          source: 'codex-native',
          type: 'codex_task',
          status: 'completed',
        ),
      );
      const fullReply = HistoryLogEntry(
        kind: 'markdown',
        message: '已提交，验证也都通过了。',
        timestamp: '2026-05-27T05:22:07Z',
        context: HistoryContext(
          source: 'codex',
          type: 'assistant',
        ),
      );

      await controller.connect();
      service.emit(SessionHistoryEvent(
        timestamp: _timestamp,
        sessionId: 'session-current',
        runtimeMeta: const RuntimeMeta(command: 'codex', engine: 'codex'),
        raw: const {'type': 'session_history'},
        summary: const SessionSummary(id: 'session-current', title: '当前会话'),
        latest: const SessionDeltaKnown(eventCursor: 1, logEntryCount: 1),
        resumeRuntimeMeta: const RuntimeMeta(command: 'codex', engine: 'codex'),
      ));
      await _flushEvents();

      service.emit(LogEvent(
        timestamp: DateTime.parse('2026-05-27T05:22:05Z'),
        sessionId: 'session-current',
        runtimeMeta: const RuntimeMeta(command: 'codex', engine: 'codex'),
        raw: const {'type': 'log'},
        message: '已提交，',
        stream: 'stdout',
      ));
      await _flushEvents();

      service.emit(SessionDeltaEvent(
        timestamp: _timestamp.add(const Duration(seconds: 1)),
        sessionId: 'session-current',
        runtimeMeta: const RuntimeMeta(command: 'codex', engine: 'codex'),
        raw: const {'type': 'session_delta'},
        summary: const SessionSummary(id: 'session-current', title: '当前会话'),
        base: const SessionDeltaKnown(eventCursor: 1, logEntryCount: 1),
        latest: const SessionDeltaKnown(eventCursor: 3, logEntryCount: 3),
        appendLogEntries: const [completed, fullReply],
        resumeRuntimeMeta: const RuntimeMeta(command: 'codex', engine: 'codex'),
      ));
      await _flushEvents();

      final markdownItems = controller.timeline
          .where((item) => item.kind == 'markdown')
          .toList(growable: false);

      expect(markdownItems, hasLength(1));
      expect(markdownItems.single.body, '已提交，验证也都通过了。');
    });

    test('session_delta 无 completed 时不会用无 turn id 回复覆盖 live 文本', () async {
      final service = _FakeMobileVcWsService();
      final controller = SessionController(service: service);
      await controller.initialize();
      addTearDown(controller.disposeController);

      const replayedReply = HistoryLogEntry(
        kind: 'markdown',
        message: '已提交，验证也都通过了。',
        timestamp: '2026-05-27T05:22:07Z',
        context: HistoryContext(
          source: 'codex',
          type: 'assistant',
        ),
      );

      await controller.connect();
      service.emit(SessionHistoryEvent(
        timestamp: _timestamp,
        sessionId: 'session-current',
        runtimeMeta: const RuntimeMeta(command: 'codex', engine: 'codex'),
        raw: const {'type': 'session_history'},
        summary: const SessionSummary(id: 'session-current', title: '当前会话'),
        latest: const SessionDeltaKnown(eventCursor: 1, logEntryCount: 1),
        resumeRuntimeMeta: const RuntimeMeta(command: 'codex', engine: 'codex'),
      ));
      await _flushEvents();

      service.emit(LogEvent(
        timestamp: DateTime.parse('2026-05-27T05:22:05Z'),
        sessionId: 'session-current',
        runtimeMeta: const RuntimeMeta(command: 'codex', engine: 'codex'),
        raw: const {'type': 'log'},
        message: '已提交，',
        stream: 'stdout',
      ));
      await _flushEvents();

      service.emit(SessionDeltaEvent(
        timestamp: _timestamp.add(const Duration(seconds: 1)),
        sessionId: 'session-current',
        runtimeMeta: const RuntimeMeta(command: 'codex', engine: 'codex'),
        raw: const {'type': 'session_delta'},
        summary: const SessionSummary(id: 'session-current', title: '当前会话'),
        base: const SessionDeltaKnown(eventCursor: 1, logEntryCount: 1),
        latest: const SessionDeltaKnown(eventCursor: 2, logEntryCount: 2),
        appendLogEntries: const [replayedReply],
        resumeRuntimeMeta: const RuntimeMeta(command: 'codex', engine: 'codex'),
      ));
      await _flushEvents();

      final markdownItems = controller.timeline
          .where((item) => item.kind == 'markdown')
          .toList(growable: false);

      expect(markdownItems, hasLength(2));
      expect(markdownItems.first.body, '已提交，');
      expect(markdownItems.last.body, '已提交，验证也都通过了。');
    });

    test('session_delta 后到 isolated completed 不会在最终回答后成卡', () async {
      final service = _FakeMobileVcWsService();
      final controller = SessionController(service: service);
      await controller.initialize();
      addTearDown(controller.disposeController);

      const completed = HistoryLogEntry(
        kind: 'system',
        message: 'task completed',
        timestamp: '2026-05-27T05:22:06Z',
        context: HistoryContext(
          source: 'codex-native',
          type: 'codex_task',
          status: 'completed',
        ),
      );
      const staleReply = HistoryLogEntry(
        kind: 'markdown',
        message: 'dart analyze 已通过。',
        timestamp: '2026-05-27T05:22:07Z',
        context: HistoryContext(
          source: 'codex',
          type: 'assistant',
        ),
      );

      await controller.connect();
      service.emit(SessionHistoryEvent(
        timestamp: _timestamp,
        sessionId: 'session-current',
        runtimeMeta: const RuntimeMeta(command: 'codex', engine: 'codex'),
        raw: const {'type': 'session_history'},
        summary: const SessionSummary(id: 'session-current', title: '当前会话'),
        latest: const SessionDeltaKnown(eventCursor: 1, logEntryCount: 1),
        resumeRuntimeMeta: const RuntimeMeta(command: 'codex', engine: 'codex'),
      ));
      await _flushEvents();

      service.emit(LogEvent(
        timestamp: DateTime.parse('2026-05-27T05:22:05Z'),
        sessionId: 'session-current',
        runtimeMeta: const RuntimeMeta(command: 'codex', engine: 'codex'),
        raw: const {'type': 'log'},
        message: '已提交，验证也都通过了。',
        stream: 'stdout',
      ));
      await _flushEvents();

      service.emit(SessionDeltaEvent(
        timestamp: _timestamp.add(const Duration(seconds: 1)),
        sessionId: 'session-current',
        runtimeMeta: const RuntimeMeta(command: 'codex', engine: 'codex'),
        raw: const {'type': 'session_delta'},
        summary: const SessionSummary(id: 'session-current', title: '当前会话'),
        base: const SessionDeltaKnown(eventCursor: 1, logEntryCount: 1),
        latest: const SessionDeltaKnown(eventCursor: 3, logEntryCount: 3),
        appendLogEntries: const [completed, staleReply],
        resumeRuntimeMeta: const RuntimeMeta(command: 'codex', engine: 'codex'),
      ));
      await _flushEvents();

      expect(
        controller.timeline.where((item) => item.kind == 'codex_tool_group'),
        isEmpty,
      );
      expect(
        controller.timeline.where((item) => item.kind == 'markdown'),
        hasLength(1),
      );
      expect(controller.timeline.last.body, '已提交，验证也都通过了。');
    });

    test('session_delta 新一轮 Codex 原生工具调用不会并入上一组', () async {
      final service = _FakeMobileVcWsService();
      final controller = SessionController(service: service);
      await controller.initialize();
      addTearDown(controller.disposeController);

      const firstToolCall = HistoryLogEntry(
        kind: 'system',
        message: 'read first file',
        timestamp: '2026-05-27T05:23:01Z',
        context: HistoryContext(
          source: 'codex-native',
          type: 'codex_tool_call',
          tool: 'functions.exec_command',
          command: '{"cmd":"cat lib/first.dart"}',
        ),
      );
      const secondToolCall = HistoryLogEntry(
        kind: 'system',
        message: 'read second file',
        timestamp: '2026-05-27T05:24:01Z',
        context: HistoryContext(
          source: 'codex-native',
          type: 'codex_tool_call',
          tool: 'functions.exec_command',
          command: '{"cmd":"cat lib/second.dart"}',
        ),
      );

      await controller.connect();
      service.emit(SessionHistoryEvent(
        timestamp: _timestamp,
        sessionId: 'session-current',
        runtimeMeta: const RuntimeMeta(command: 'codex', engine: 'codex'),
        raw: const {'type': 'session_history'},
        summary: const SessionSummary(id: 'session-current', title: '当前会话'),
        logEntries: const [firstToolCall],
        latest: const SessionDeltaKnown(eventCursor: 1, logEntryCount: 1),
        resumeRuntimeMeta: const RuntimeMeta(command: 'codex', engine: 'codex'),
      ));
      await _flushEvents();

      service.emit(SessionDeltaEvent(
        timestamp: _timestamp.add(const Duration(seconds: 1)),
        sessionId: 'session-current',
        runtimeMeta: const RuntimeMeta(command: 'codex', engine: 'codex'),
        raw: const {'type': 'session_delta'},
        summary: const SessionSummary(id: 'session-current', title: '当前会话'),
        base: const SessionDeltaKnown(eventCursor: 1, logEntryCount: 1),
        latest: const SessionDeltaKnown(eventCursor: 2, logEntryCount: 2),
        appendLogEntries: const [secondToolCall],
        resumeRuntimeMeta: const RuntimeMeta(command: 'codex', engine: 'codex'),
      ));
      await _flushEvents();

      final groups = controller.timeline
          .where((item) => item.kind == 'codex_tool_group')
          .toList(growable: false);
      expect(groups, hasLength(2));
      expect(groups.first.codexSteps, contains('正在读取 first.dart'));
      expect(groups.last.codexSteps, contains('正在读取 second.dart'));
    });

    test('普通运行时事件游标不会提前标记 session_delta 已追上', () async {
      final service = _FakeMobileVcWsService();
      final controller = SessionController(service: service);
      await controller.initialize();
      addTearDown(controller.disposeController);

      await controller.connect();
      service.emit(SessionHistoryEvent(
        timestamp: _timestamp,
        sessionId: 'codex-thread:observe',
        runtimeMeta: const RuntimeMeta(command: 'codex', engine: 'codex'),
        raw: const {'type': 'session_history', 'eventCursor': 1},
        summary: const SessionSummary(
          id: 'codex-thread:observe',
          title: 'Native Codex',
          source: 'codex-native',
          external: true,
          runtime: RuntimeMeta(command: 'codex', engine: 'codex'),
        ),
        logEntryTotal: 1,
        logEntries: const [
          HistoryLogEntry(
            kind: 'markdown',
            message: '已有回复',
            timestamp: '2026-05-27T05:20:00Z',
          ),
        ],
        resumeRuntimeMeta: const RuntimeMeta(command: 'codex', engine: 'codex'),
      ));
      await _flushEvents();

      service.emit(AIStatusEvent(
        timestamp: _timestamp.add(const Duration(seconds: 1)),
        sessionId: 'codex-thread:observe',
        runtimeMeta: const RuntimeMeta(command: 'codex', engine: 'codex'),
        raw: const {'type': 'ai_status', 'eventCursor': 8},
        label: '处理中',
        visible: true,
      ));
      await _flushEvents();

      service.sentPayloads.clear();
      controller.continueSameSessionFromPhone();
      await _flushEvents();

      final deltaPayload = service.sentPayloads.singleWhere(
        (payload) =>
            payload['action'] == 'session_delta_get' &&
            payload['sessionId'] == 'codex-thread:observe',
      );
      final known = deltaPayload['known'] as Map<String, dynamic>;
      expect(
        known['eventCursor'],
        1,
        reason:
            'AI/status/task cursors are resume replay cursors, not proof that '
            'history/diff/terminal delta counters have caught up.',
      );
      expect(known['logEntryCount'], 1);

      final resumePayload = service.sentPayloads.singleWhere(
        (payload) =>
            payload['action'] == 'session_resume' &&
            payload['sessionId'] == 'codex-thread:observe',
      );
      expect(
        resumePayload['lastSeenEventCursor'],
        8,
        reason:
            'The socket replay cursor should still advance for resume replay.',
      );
    });

    test('session_history 会把 Codex 子智能体事件整理成可见状态', () async {
      final service = _FakeMobileVcWsService();
      final controller = SessionController(service: service);
      await controller.initialize();
      addTearDown(controller.disposeController);

      await controller.connect();
      service.emit(SessionHistoryEvent(
        timestamp: _timestamp,
        sessionId: 'session-current',
        runtimeMeta: const RuntimeMeta(command: 'codex', engine: 'codex'),
        raw: const {'type': 'session_history'},
        summary: const SessionSummary(id: 'session-current', title: '当前会话'),
        logEntries: const [
          HistoryLogEntry(
            kind: 'system',
            message: 'spawn subagent',
            timestamp: '2026-05-27T05:22:01Z',
            context: HistoryContext(
              source: 'codex-native',
              type: 'codex_tool_call',
              tool: 'functions.spawn_agent',
              command: '{"task_name":"agent-019e7126","message":"分析通信层"}',
            ),
          ),
          HistoryLogEntry(
            kind: 'system',
            message: 'read source',
            timestamp: '2026-05-27T05:22:02Z',
            context: HistoryContext(
              source: 'codex-native',
              type: 'codex_tool_call',
              tool: 'functions.exec_command',
              command:
                  '{"cmd":"sed -n \'1,120p\' internal/engine/codex_transport.go"}',
            ),
          ),
        ],
        resumeRuntimeMeta: const RuntimeMeta(command: 'codex', engine: 'codex'),
      ));
      await _flushEvents();

      final group = controller.timeline.single;
      expect(group.kind, 'codex_tool_group');
      expect(group.codexSteps, [
        '正在创建智能体：agent-019e7126',
        '正在读取 codex_transport.go',
      ]);
    });

    test('Codex 可见状态优先保留后续关键智能体和补丁状态', () async {
      final service = _FakeMobileVcWsService();
      final controller = SessionController(service: service);
      await controller.initialize();
      addTearDown(controller.disposeController);

      await controller.connect();
      service.emit(SessionHistoryEvent(
        timestamp: _timestamp,
        sessionId: 'session-current',
        runtimeMeta: const RuntimeMeta(command: 'codex', engine: 'codex'),
        raw: const {'type': 'session_history'},
        summary: const SessionSummary(id: 'session-current', title: '当前会话'),
        logEntries: const [
          HistoryLogEntry(
            kind: 'system',
            message: 'read 1',
            timestamp: '2026-05-27T05:23:01Z',
            context: HistoryContext(
              source: 'codex-native',
              type: 'codex_tool_call',
              tool: 'functions.exec_command',
              command: '{"cmd":"sed -n \'1,40p\' lib/a.dart"}',
            ),
          ),
          HistoryLogEntry(
            kind: 'system',
            message: 'read 2',
            timestamp: '2026-05-27T05:23:02Z',
            context: HistoryContext(
              source: 'codex-native',
              type: 'codex_tool_call',
              tool: 'functions.exec_command',
              command: '{"cmd":"cat lib/b.dart"}',
            ),
          ),
          HistoryLogEntry(
            kind: 'system',
            message: 'read 3',
            timestamp: '2026-05-27T05:23:03Z',
            context: HistoryContext(
              source: 'codex-native',
              type: 'codex_tool_call',
              tool: 'functions.exec_command',
              command: '{"cmd":"rg SessionController lib/c.dart"}',
            ),
          ),
          HistoryLogEntry(
            kind: 'system',
            message: 'read 4',
            timestamp: '2026-05-27T05:23:04Z',
            context: HistoryContext(
              source: 'codex-native',
              type: 'codex_tool_call',
              tool: 'functions.exec_command',
              command: '{"cmd":"find lib -name d.dart"}',
            ),
          ),
          HistoryLogEntry(
            kind: 'system',
            message: 'read 5',
            timestamp: '2026-05-27T05:23:05Z',
            context: HistoryContext(
              source: 'codex-native',
              type: 'codex_tool_call',
              tool: 'functions.exec_command',
              command: '{"cmd":"head -20 lib/e.dart"}',
            ),
          ),
          HistoryLogEntry(
            kind: 'system',
            message: 'spawn subagent',
            timestamp: '2026-05-27T05:23:06Z',
            context: HistoryContext(
              source: 'codex-native',
              type: 'codex_tool_call',
              tool: 'functions.spawn_agent',
              command: '{"task_name":"agent-review","message":"审查"}',
            ),
          ),
          HistoryLogEntry(
            kind: 'system',
            message: 'patch success',
            timestamp: '2026-05-27T05:23:07Z',
            context: HistoryContext(
              source: 'codex-native',
              type: 'codex_patch',
              status: 'success',
            ),
          ),
        ],
        resumeRuntimeMeta: const RuntimeMeta(command: 'codex', engine: 'codex'),
      ));
      await _flushEvents();

      final steps = controller.timeline.single.codexSteps;
      expect(steps, hasLength(6));
      expect(steps, contains('正在创建智能体：agent-review'));
      expect(steps, contains('补丁结果：success'));
      expect(steps, contains('正在读取 e.dart'));
      expect(steps, isNot(contains('正在读取 a.dart')));
    });

    test('session_history 会恢复用户消息附件并处理媒体预览结果', () async {
      final service = _FakeMobileVcWsService();
      final controller = SessionController(service: service);
      await controller.initialize();
      addTearDown(controller.disposeController);

      await controller.connect();
      service.emit(SessionHistoryEvent(
        timestamp: _timestamp,
        sessionId: 'session-current',
        runtimeMeta: const RuntimeMeta(command: 'claude'),
        raw: const {'type': 'session_history'},
        summary: const SessionSummary(id: 'session-current', title: '当前会话'),
        logEntries: const [
          HistoryLogEntry(
            kind: 'user',
            message: '看图',
            timestamp: '2026-05-27T05:20:00Z',
            attachments: [
              TimelineAttachment(
                id: 'att-1',
                kind: 'image',
                name: 'screen.png',
                mimeType: 'image/png',
                size: 9,
                path: '/tmp/screen.png',
                previewStatus: 'available',
                source: 'user_upload',
              ),
            ],
          ),
        ],
        resumeRuntimeMeta: RuntimeMeta(command: 'claude'),
      ));
      await _flushEvents();

      expect(controller.timeline, hasLength(1));
      final attachment = controller.timeline.single.attachments.single;
      expect(attachment.name, 'screen.png');

      controller.requestMediaPreview(attachment);
      expect(service.sentPayloads.last['action'], 'media_preview');
      expect(service.sentPayloads.last['attachmentId'], '/tmp/screen.png');
      expect(service.sentPayloads.last['path'], '/tmp/screen.png');

      service.emit(MediaPreviewResultEvent(
        timestamp: _timestamp,
        sessionId: 'session-current',
        runtimeMeta: const RuntimeMeta(),
        raw: const {'type': 'media_preview_result'},
        attachmentId: '/tmp/screen.png',
        path: '/tmp/screen.png',
        content: base64Encode(utf8.encode('png-bytes')),
        status: 'ok',
      ));
      await _flushEvents();

      final preview = controller.mediaPreviewStates['/tmp/screen.png'];
      expect(preview?.ok, isTrue);
      expect(utf8.decode(preview!.bytes!), 'png-bytes');
    });

    test('session_history 同一路径图片附件和文本路径只显示一张', () async {
      final service = _FakeMobileVcWsService();
      final controller = SessionController(service: service);
      await controller.initialize();
      addTearDown(controller.disposeController);

      await controller.connect();
      service.emit(SessionHistoryEvent(
        timestamp: _timestamp,
        sessionId: 'session-current',
        runtimeMeta: const RuntimeMeta(command: 'codex', engine: 'codex'),
        raw: const {'type': 'session_history'},
        summary: const SessionSummary(id: 'session-current', title: '当前会话'),
        logEntries: const [
          HistoryLogEntry(
            kind: 'user',
            message: '看图\n\nAttached local image files:\n- /tmp/screen.png',
            timestamp: '2026-05-27T05:20:00Z',
            attachments: [
              TimelineAttachment(
                id: 'att-1',
                kind: 'image',
                name: 'screen.png',
                mimeType: 'image/png',
                size: 9,
                path: '/tmp/screen.png',
                previewStatus: 'available',
                source: 'user_upload',
              ),
            ],
          ),
        ],
        resumeRuntimeMeta: RuntimeMeta(command: 'codex', engine: 'codex'),
      ));
      await _flushEvents();

      expect(controller.timeline, hasLength(1));
      final item = controller.timeline.single;
      expect(item.attachments, hasLength(1));
      final attachment = item.attachments.single;
      expect(attachment.id, 'att-1');
      expect(attachment.path, '/tmp/screen.png');
      expect(attachment.source, 'user_upload');
      expect(controller.mediaPreviewKey(attachment), '/tmp/screen.png');
    });

    test('session_delta 正式图片历史会替换本地乐观图片气泡', () async {
      final service = _FakeMobileVcWsService();
      final controller = SessionController(service: service);
      await controller.initialize();
      addTearDown(controller.disposeController);

      await controller.saveConfig(const AppConfig(host: '192.168.0.2'));
      await controller.connect();
      service.emit(SessionCreatedEvent(
        timestamp: _timestamp,
        sessionId: 'session-current',
        runtimeMeta: const RuntimeMeta(),
        raw: const {'type': 'session_created'},
        summary: const SessionSummary(
          id: 'session-current',
          title: '当前会话',
          runtime: RuntimeMeta(command: 'codex', engine: 'codex'),
        ),
      ));
      await _flushEvents();
      service.sentPayloads.clear();

      controller.sendInputTextWithImages(
        '看图',
        [
          ChatImageAttachment(
            name: 'screen.png',
            mimeType: 'image/png',
            bytes: utf8.encode('png-bytes'),
          ),
        ],
      );
      await _flushEvents();
      expect(controller.timeline.where((item) => item.kind == 'user'),
          hasLength(1));

      service.emit(SessionDeltaEvent(
        timestamp: _timestamp.add(const Duration(seconds: 1)),
        sessionId: 'session-current',
        runtimeMeta: const RuntimeMeta(command: 'codex', engine: 'codex'),
        raw: const {'type': 'session_delta'},
        summary: const SessionSummary(id: 'session-current', title: '当前会话'),
        appendLogEntries: const [
          HistoryLogEntry(
            kind: 'user',
            message: '看图\n\nAttached local image files:\n- /tmp/screen.png',
            timestamp: '2026-05-27T05:20:00Z',
            attachments: [
              TimelineAttachment(
                id: 'att-1',
                kind: 'image',
                name: 'screen.png',
                mimeType: 'image/png',
                size: 9,
                path: '/tmp/screen.png',
                previewStatus: 'available',
                source: 'user_upload',
              ),
            ],
          ),
        ],
        resumeRuntimeMeta: RuntimeMeta(command: 'codex', engine: 'codex'),
      ));
      await _flushEvents();

      final userItems =
          controller.timeline.where((item) => item.kind == 'user').toList();
      expect(userItems, hasLength(1));
      expect(userItems.single.id.startsWith('history-user-'), isTrue);
      expect(userItems.single.attachments, hasLength(1));
      expect(userItems.single.attachments.single.id, 'att-1');
      expect(userItems.single.attachments.single.path, '/tmp/screen.png');
    });

    test('Codex 原生历史会从本地图片路径恢复附件卡片', () async {
      final service = _FakeMobileVcWsService();
      final controller = SessionController(service: service);
      await controller.initialize();
      addTearDown(controller.disposeController);

      await controller.connect();
      service.emit(SessionHistoryEvent(
        timestamp: _timestamp,
        sessionId: 'codex-thread:local-image',
        runtimeMeta: const RuntimeMeta(command: 'codex', engine: 'codex'),
        raw: const {'type': 'session_history'},
        summary: const SessionSummary(
          id: 'codex-thread:local-image',
          title: 'Codex 图片会话',
          source: 'codex-native',
        ),
        logEntries: const [
          HistoryLogEntry(
            kind: 'markdown',
            message: '已生成图片：![preview](/tmp/codex-output/screen.png)',
            timestamp: '2026-05-27T05:20:00Z',
          ),
        ],
        resumeRuntimeMeta: RuntimeMeta(command: 'codex', engine: 'codex'),
      ));
      await _flushEvents();

      expect(controller.timeline, hasLength(1));
      final item = controller.timeline.single;
      expect(item.kind, 'markdown');
      expect(item.attachments, hasLength(1));
      final attachment = item.attachments.single;
      expect(attachment.kind, 'image');
      expect(attachment.name, 'screen.png');
      expect(attachment.mimeType, 'image/png');
      expect(attachment.path, '/tmp/codex-output/screen.png');
      expect(attachment.source, 'assistant_path');
    });

    test('loadSession 匹配 history 后先显示 timeline 再异步刷新目录和 delta', () async {
      final service = _FakeMobileVcWsService();
      final controller = SessionController(service: service);
      await controller.initialize();
      addTearDown(controller.disposeController);

      await controller.connect();
      service.sentPayloads.clear();
      final notifications = <String>[];
      controller.addListener(() {
        if (controller.selectedSessionId == 'session-target' &&
            controller.timeline.any(
              (item) => item.body.contains('先显示的历史回复'),
            )) {
          notifications.add(
            service.sentPayloads
                .map((payload) => payload['action'].toString())
                .join(','),
          );
        }
      });

      controller.loadSession('session-target');
      service.emit(SessionHistoryEvent(
        timestamp: _timestamp,
        sessionId: 'session-target',
        runtimeMeta: const RuntimeMeta(command: 'claude'),
        raw: const {'type': 'session_history'},
        summary: const SessionSummary(id: 'session-target', title: '历史会话'),
        logEntries: const [
          HistoryLogEntry(
            kind: 'markdown',
            message: '先显示的历史回复',
            timestamp: '2026-01-01T00:00:00Z',
          ),
        ],
        resumeRuntimeMeta: const RuntimeMeta(
          command: 'claude',
          cwd: '/tmp/mobilevc-history-cwd',
        ),
      ));
      await _flushEvents();

      expect(notifications, isNotEmpty);
      expect(notifications.first, isNot(contains('session_delta_get')));
      expect(notifications.first, isNot(contains('session_list')));
      expect(
        service.sentPayloads.any(
          (payload) => payload['action'] == 'session_delta_get',
        ),
        isTrue,
      );
      expect(
        service.sentPayloads.any((payload) => payload['action'] == 'fs_list'),
        isTrue,
      );
    });

    test('公开列表 getter 返回稳定不可变视图，避免 build 时复制长 timeline', () async {
      final service = _FakeMobileVcWsService();
      final controller = SessionController(service: service);
      await controller.initialize();
      addTearDown(controller.disposeController);

      final firstTimeline = controller.timeline;
      final secondTimeline = controller.timeline;
      final firstSessions = controller.sessions;
      final secondSessions = controller.sessions;

      expect(identical(firstTimeline, secondTimeline), isTrue);
      expect(identical(firstSessions, secondSessions), isTrue);
      expect(() => firstTimeline.add(_timelineItem('blocked')),
          throwsUnsupportedError);
      expect(() => firstSessions.clear(), throwsUnsupportedError);
    });

    test('loadSession 期间前台恢复不会向旧会话发送 resume/delta', () async {
      final service = _FakeMobileVcWsService();
      final controller = SessionController(service: service);
      await controller.initialize();
      addTearDown(controller.disposeController);

      await controller.connect();
      service.emit(SessionHistoryEvent(
        timestamp: _timestamp,
        sessionId: 'session-old',
        runtimeMeta: const RuntimeMeta(command: 'claude'),
        raw: const {'type': 'session_history'},
        summary: const SessionSummary(id: 'session-old', title: '旧会话'),
        runtimeAlive: true,
      ));
      await _flushEvents();
      expect(controller.selectedSessionId, 'session-old');

      service.sentPayloads.clear();
      controller.loadSession('session-new');
      controller.resumeConnectionIfNeeded();
      await _flushEvents();

      final sessionActions = service.sentPayloads
          .where((payload) =>
              payload['action'] == 'session_load' ||
              payload['action'] == 'session_resume' ||
              payload['action'] == 'session_delta_get')
          .toList();
      expect(sessionActions, hasLength(1));
      expect(sessionActions.single['action'], 'session_load');
      expect(sessionActions.single['sessionId'], 'session-new');
    });

    test('loadSession 期间旧会话 full-sync delta 不会恢复旧会话', () async {
      final service = _FakeMobileVcWsService();
      final controller = SessionController(service: service);
      await controller.initialize();
      addTearDown(controller.disposeController);

      await controller.connect();
      service.emit(SessionHistoryEvent(
        timestamp: _timestamp,
        sessionId: 'session-old',
        runtimeMeta: const RuntimeMeta(command: 'claude'),
        raw: const {'type': 'session_history'},
        summary: const SessionSummary(id: 'session-old', title: '旧会话'),
        runtimeAlive: true,
      ));
      await _flushEvents();
      expect(controller.selectedSessionId, 'session-old');

      service.sentPayloads.clear();
      controller.loadSession('session-new');
      service.emit(SessionDeltaEvent(
        timestamp: _timestamp.add(const Duration(seconds: 1)),
        sessionId: 'session-old',
        runtimeMeta: const RuntimeMeta(command: 'claude'),
        raw: const {'type': 'session_delta'},
        summary: const SessionSummary(id: 'session-old', title: '旧会话'),
        requiresFullSync: true,
      ));
      await _flushEvents();

      final sessionActions = service.sentPayloads
          .where((payload) =>
              payload['action'] == 'session_load' ||
              payload['action'] == 'session_resume' ||
              payload['action'] == 'session_delta_get')
          .toList();
      expect(sessionActions, hasLength(1));
      expect(sessionActions.single['action'], 'session_load');
      expect(sessionActions.single['sessionId'], 'session-new');
      expect(controller.selectedSessionId, 'session-old');
      expect(controller.isLoadingSession, isTrue);
    });

    test('loadSession 期间旧会话 runtime 事件不会重启 observe delta', () async {
      final service = _FakeMobileVcWsService();
      final controller = SessionController(service: service);
      await controller.initialize();
      addTearDown(controller.disposeController);

      await controller.connect();
      service.emit(SessionHistoryEvent(
        timestamp: _timestamp,
        sessionId: 'session-old',
        runtimeMeta: const RuntimeMeta(command: 'claude'),
        raw: const {'type': 'session_history'},
        summary: const SessionSummary(id: 'session-old', title: '旧会话'),
        runtimeAlive: true,
      ));
      await _flushEvents();
      expect(controller.selectedSessionId, 'session-old');

      service.sentPayloads.clear();
      controller.loadSession('session-new');
      service.emit(SessionStateEvent(
        timestamp: _timestamp.add(const Duration(milliseconds: 100)),
        sessionId: 'session-old',
        runtimeMeta: const RuntimeMeta(command: 'claude'),
        raw: const {'type': 'session_state'},
        state: 'RUNNING',
      ));
      await _flushEvents();

      final deltaRequests = service.sentPayloads
          .where((payload) => payload['action'] == 'session_delta_get')
          .toList();
      expect(deltaRequests, isEmpty);
    });

    test('loadSession 期间断线重连会重发 pending target load', () async {
      final service = _FakeMobileVcWsService();
      final controller = SessionController(service: service);
      await controller.initialize();
      addTearDown(controller.disposeController);

      await controller.connect();
      service.emit(SessionHistoryEvent(
        timestamp: _timestamp,
        sessionId: 'session-old',
        runtimeMeta: const RuntimeMeta(command: 'claude'),
        raw: const {'type': 'session_history'},
        summary: const SessionSummary(id: 'session-old', title: '旧会话'),
        runtimeAlive: true,
      ));
      await _flushEvents();

      service.sentPayloads.clear();
      controller.loadSession('session-new');
      service.emit(ErrorEvent(
        timestamp: _timestamp.add(const Duration(milliseconds: 100)),
        sessionId: 'session-old',
        runtimeMeta: const RuntimeMeta(command: 'claude'),
        raw: const {'type': 'error'},
        code: 'ws_closed',
        message: 'websocket closed',
      ));
      await _flushEvents();

      await Future<void>.delayed(const Duration(milliseconds: 20));
      await _flushEvents();

      final loads = service.sentPayloads
          .where((payload) => payload['action'] == 'session_load')
          .toList();
      expect(loads, hasLength(2));
      expect(loads.last['sessionId'], 'session-new');
      expect(loads.last['reason'], 'reconnect_pending_load');
      expect(
        service.sentPayloads
            .where((payload) => payload['action'] == 'session_resume'),
        isEmpty,
      );
      expect(controller.isLoadingSession, isTrue);
    });

    test('loadSession 目标 session_state 先到不会提前结束 loading', () async {
      final service = _FakeMobileVcWsService();
      final controller = SessionController(service: service);
      await controller.initialize();
      addTearDown(controller.disposeController);

      await controller.connect();
      service.emit(SessionHistoryEvent(
        timestamp: _timestamp,
        sessionId: 'session-old',
        runtimeMeta: const RuntimeMeta(command: 'claude'),
        raw: const {'type': 'session_history'},
        summary: const SessionSummary(id: 'session-old', title: '旧会话'),
      ));
      await _flushEvents();

      controller.loadSession('session-new');
      service.emit(SessionStateEvent(
        timestamp: _timestamp.add(const Duration(milliseconds: 100)),
        sessionId: 'session-new',
        runtimeMeta: const RuntimeMeta(command: 'claude'),
        raw: const {'type': 'session_state'},
        state: 'IDLE',
      ));
      await _flushEvents();

      expect(controller.isLoadingSession, isTrue);
      expect(controller.selectedSessionId, 'session-old');

      service.emit(SessionHistoryEvent(
        timestamp: _timestamp.add(const Duration(milliseconds: 200)),
        sessionId: 'session-new',
        runtimeMeta: const RuntimeMeta(command: 'claude'),
        raw: const {'type': 'session_history'},
        summary: const SessionSummary(id: 'session-new', title: '新会话'),
        logEntries: const [
          HistoryLogEntry(
            kind: 'user',
            message: '新会话 history',
            timestamp: '2026-06-07T05:00:00Z',
          ),
        ],
      ));
      await _flushEvents();

      expect(controller.selectedSessionId, 'session-new');
      expect(controller.isLoadingSession, isFalse);
      expect(
        controller.timeline.map((item) => item.body),
        contains('新会话 history'),
      );
    });

    test('loadSession 超时会清 pending，迟到 history 不会切换会话', () async {
      final service = _FakeMobileVcWsService();
      final controller = SessionController(
        service: service,
        sessionLoadingTimeout: const Duration(milliseconds: 30),
      );
      await controller.initialize();
      addTearDown(controller.disposeController);

      await controller.connect();
      service.emit(SessionHistoryEvent(
        timestamp: _timestamp,
        sessionId: 'session-old',
        runtimeMeta: const RuntimeMeta(command: 'claude'),
        raw: const {'type': 'session_history'},
        summary: const SessionSummary(id: 'session-old', title: '旧会话'),
        runtimeAlive: true,
      ));
      await _flushEvents();

      controller.loadSession('session-new');
      await Future<void>.delayed(const Duration(milliseconds: 80));
      await _flushEvents();
      expect(controller.isLoadingSession, isFalse);
      expect(controller.selectedSessionId, 'session-old');
      expect(controller.isSessionBusy, isFalse);

      service.emit(SessionHistoryEvent(
        timestamp: _timestamp.add(const Duration(seconds: 17)),
        sessionId: 'session-new',
        runtimeMeta: const RuntimeMeta(command: 'claude'),
        raw: const {'type': 'session_history'},
        summary: const SessionSummary(id: 'session-new', title: '新会话'),
        logEntries: const [
          HistoryLogEntry(
            kind: 'user',
            message: '新会话历史',
            timestamp: '2026-06-07T05:00:00Z',
          ),
        ],
      ));
      await _flushEvents();

      expect(controller.selectedSessionId, 'session-old');
      expect(
        controller.timeline.map((item) => item.body),
        isNot(contains('新会话历史')),
      );
    });

    test('loadSession 目标 delta 成功应用会结束 loading', () async {
      final service = _FakeMobileVcWsService();
      final controller = SessionController(service: service);
      await controller.initialize();
      addTearDown(controller.disposeController);

      await controller.connect();
      service.emit(SessionHistoryEvent(
        timestamp: _timestamp,
        sessionId: 'session-old',
        runtimeMeta: const RuntimeMeta(command: 'claude'),
        raw: const {'type': 'session_history'},
        summary: const SessionSummary(id: 'session-old', title: '旧会话'),
        runtimeAlive: true,
      ));
      await _flushEvents();

      controller.loadSession('session-new');
      service.emit(SessionDeltaEvent(
        timestamp: _timestamp.add(const Duration(milliseconds: 100)),
        sessionId: 'session-new',
        runtimeMeta: const RuntimeMeta(command: 'claude'),
        raw: const {'type': 'session_delta'},
        summary: const SessionSummary(id: 'session-new', title: '新会话'),
        latest: const SessionDeltaKnown(logEntryCount: 1),
        appendLogEntries: const [
          HistoryLogEntry(
            kind: 'user',
            message: '新会话 delta',
            timestamp: '2026-06-07T05:00:00Z',
          ),
        ],
      ));
      await _flushEvents();

      expect(controller.selectedSessionId, 'session-new');
      expect(controller.isLoadingSession, isFalse);
      expect(
          controller.timeline.map((item) => item.body), contains('新会话 delta'));
    });

    test('重复 loadSession 同一目标不会重复发送 session_load', () async {
      final service = _FakeMobileVcWsService();
      final controller = SessionController(service: service);
      await controller.initialize();
      addTearDown(controller.disposeController);

      await controller.connect();
      service.sentPayloads.clear();

      controller.loadSession('session-target');
      controller.loadSession('session-target');
      await _flushEvents();

      final loads = service.sentPayloads
          .where((payload) => payload['action'] == 'session_load')
          .toList();
      expect(loads, hasLength(1));
      expect(loads.single['sessionId'], 'session-target');
    });

    test('目标 session_load_failed 会立即结束 loading 并显示错误', () async {
      final service = _FakeMobileVcWsService();
      final controller = SessionController(service: service);
      await controller.initialize();
      addTearDown(controller.disposeController);

      await controller.connect();
      controller.loadSession('session-target');
      expect(controller.isLoadingSession, isTrue);

      service.emit(ErrorEvent(
        timestamp: _timestamp.add(const Duration(seconds: 1)),
        sessionId: 'session-target',
        runtimeMeta: const RuntimeMeta(),
        raw: const {'type': 'error'},
        code: 'session_load_failed',
        message: 'session history window unavailable for session-target',
      ));
      await _flushEvents();

      expect(controller.isLoadingSession, isFalse);
      expect(controller.connectionStage, SessionConnectionStage.ready);
      expect(
        controller.timeline.where((item) =>
            item.kind == 'error' &&
            item.body.contains('session history window unavailable')),
        isNotEmpty,
      );
    });

    test('非目标 session_load_failed 不会结束当前 loading', () async {
      final service = _FakeMobileVcWsService();
      final controller = SessionController(service: service);
      await controller.initialize();
      addTearDown(controller.disposeController);

      await controller.connect();
      controller.loadSession('session-target');
      expect(controller.isLoadingSession, isTrue);

      service.emit(ErrorEvent(
        timestamp: _timestamp.add(const Duration(seconds: 1)),
        sessionId: 'session-other',
        runtimeMeta: const RuntimeMeta(),
        raw: const {'type': 'error'},
        code: 'session_load_failed',
        message: 'session history window unavailable for session-other',
      ));
      await _flushEvents();

      expect(controller.isLoadingSession, isTrue);
    });

    test('session_list already running 不渲染为错误消息', () async {
      final service = _FakeMobileVcWsService();
      final controller = SessionController(service: service);
      await controller.initialize();
      addTearDown(controller.disposeController);

      await controller.connect();
      service.emit(ErrorEvent(
        timestamp: _timestamp.add(const Duration(seconds: 1)),
        sessionId: '',
        runtimeMeta: const RuntimeMeta(),
        raw: const {'type': 'error'},
        message: 'session list is already running for this connection',
      ));
      await _flushEvents();

      expect(controller.latestError, isNull);
      expect(controller.connected, isTrue);
      expect(
        controller.timeline.where((item) => item.kind == 'error'),
        isEmpty,
      );
      expect(
        controller.debugLogs.any((item) => item.contains('session_list 已在刷新')),
        isTrue,
      );
    });

    test('e2ee_decrypt_failed 会立即结束会话 loading', () async {
      final service = _FakeMobileVcWsService();
      final controller = SessionController(service: service);
      await controller.initialize();
      addTearDown(controller.disposeController);

      await controller.saveConfig(const AppConfig(
        connectionMode: 'relay',
        relayUrl: 'wss://relay.example.test',
        relaySessionId: 'rs_test',
        relayClientId: 'rc_test',
        relayClientReconnectSecret: 'reconnect_secret',
      ));
      await controller.connect();
      service.sentPayloads.clear();

      controller.loadSession('session-target');
      expect(controller.isLoadingSession, isTrue);

      service.emit(ErrorEvent(
        timestamp: _timestamp.add(const Duration(seconds: 1)),
        sessionId: 'session-target',
        runtimeMeta: const RuntimeMeta(),
        raw: const {'type': 'error'},
        code: 'e2ee_decrypt_failed',
        message: 'Relay E2EE 解密失败',
      ));
      await _flushEvents();

      expect(controller.isLoadingSession, isFalse);
      expect(controller.connected, isFalse);
      expect(controller.connectionStage, SessionConnectionStage.reconnecting);
      expect(controller.connectionMessage, '恢复连接中...');
      expect(controller.relayDeviceStatus, 'Relay E2EE 解密失败');
      expect(
        service.sentPayloads
            .where((payload) => payload['action'] == 'session_load'),
        hasLength(1),
      );
    });

    test('loadSession history 后延后发送非关键 bootstrap 请求', () async {
      final service = _FakeMobileVcWsService();
      final controller = SessionController(service: service);
      await controller.initialize();
      addTearDown(controller.disposeController);

      await controller.connect();
      service.sentPayloads.clear();

      controller.loadSession('session-target');
      service.emit(SessionHistoryEvent(
        timestamp: _timestamp,
        sessionId: 'session-target',
        runtimeMeta: const RuntimeMeta(command: 'claude'),
        raw: const {'type': 'session_history'},
        summary: const SessionSummary(id: 'session-target', title: '历史会话'),
        logEntries: [
          HistoryLogEntry(
            kind: 'markdown',
            message: '首屏历史',
            timestamp: _timestamp.toIso8601String(),
          ),
        ],
        resumeRuntimeMeta: const RuntimeMeta(cwd: '/workspace'),
      ));
      await _flushEvents();

      expect(controller.timeline.any((item) => item.body.contains('首屏历史')),
          isTrue);
      expect(
        service.sentPayloads.any((payload) =>
            payload['action'] == 'runtime_process_list' ||
            payload['action'] == 'permission_rule_list' ||
            payload['action'] == 'task_snapshot_get'),
        isFalse,
      );
      expect(
        service.sentPayloads.any((payload) => payload['action'] == 'fs_list'),
        isTrue,
        reason: 'cwd/fs_list is critical session context and should not wait '
            'for the delayed bootstrap timer',
      );
      expect(
        service.sentPayloads
            .any((payload) => payload['action'] == 'session_list'),
        isTrue,
        reason: 'switchWorkingDirectory should refresh the global session list '
            'after restoring cwd immediately',
      );

      await Future<void>.delayed(const Duration(milliseconds: 520));
      await _flushEvents();

      expect(
        service.sentPayloads.any((payload) =>
            payload['action'] == 'runtime_process_list' ||
            payload['action'] == 'permission_rule_list' ||
            payload['action'] == 'task_snapshot_get' ||
            payload['action'] == 'fs_list'),
        isTrue,
      );
      expect(
        service.sentPayloads
            .where((payload) => payload['action'] == 'session_diff_page_get'),
        isEmpty,
        reason: 'diff hydration is user-triggered so relay bootstrap does not '
            'queue a slow non-critical projection read after session switch',
      );
    });

    test('loadSession 期间慢 bootstrap 请求不会发到旧会话', () async {
      final service = _FakeMobileVcWsService();
      final controller = SessionController(service: service);
      await controller.initialize();
      addTearDown(controller.disposeController);

      await controller.connect();
      service.emit(SessionHistoryEvent(
        timestamp: _timestamp,
        sessionId: 'session-old',
        runtimeMeta: const RuntimeMeta(command: 'claude'),
        raw: const {'type': 'session_history'},
        summary: const SessionSummary(id: 'session-old', title: '旧会话'),
        latest: const SessionDeltaKnown(
          diffCount: 1,
          terminalExecutionCount: 1,
          terminalStdoutLength: 10,
          terminalStderrLength: 5,
        ),
        resumeRuntimeMeta: const RuntimeMeta(cwd: '/workspace'),
      ));
      await _flushEvents();
      service.sentPayloads.clear();

      controller.loadSession('session-new');
      controller.requestDiffPage();
      controller.requestTerminalHydration();
      controller.requestSessionContext();
      controller.requestTaskSnapshot();
      controller.requestContextWindowUsage();
      controller.requestRuntimeProcessList();
      controller.requestPermissionRuleList();
      await _flushEvents();

      final actions =
          service.sentPayloads.map((payload) => payload['action']).toList();
      expect(actions, contains('session_load'));
      expect(actions, isNot(contains('session_diff_page_get')));
      expect(actions, isNot(contains('session_terminal_range_get')));
      expect(actions, isNot(contains('session_terminal_execution_page_get')));
      expect(actions, isNot(contains('session_context_get')));
      expect(actions, isNot(contains('task_snapshot_get')));
      expect(actions, isNot(contains('context_window_usage_get')));
      expect(actions, isNot(contains('runtime_process_list')));
      expect(actions, isNot(contains('permission_rule_list')));
    });

    test('从长会话快速切短会话会取消长会话延迟 bootstrap', () async {
      final service = _FakeMobileVcWsService();
      final controller = SessionController(service: service);
      await controller.initialize();
      addTearDown(controller.disposeController);

      await controller.connect();
      service.sentPayloads.clear();

      controller.loadSession('session-long');
      service.emit(SessionHistoryEvent(
        timestamp: _timestamp,
        sessionId: 'session-long',
        runtimeMeta: const RuntimeMeta(command: 'claude'),
        raw: const {'type': 'session_history'},
        summary: const SessionSummary(id: 'session-long', title: '长会话'),
        logEntries: [
          HistoryLogEntry(
            kind: 'markdown',
            message: '长会话首屏',
            timestamp: _timestamp.toIso8601String(),
          ),
        ],
        latest: const SessionDeltaKnown(logEntryCount: 17754),
        resumeRuntimeMeta: const RuntimeMeta(cwd: '/workspace'),
      ));
      await _flushEvents();

      service.sentPayloads.clear();
      controller.loadSession('session-short');
      await Future<void>.delayed(const Duration(milliseconds: 520));
      await _flushEvents();

      final actions =
          service.sentPayloads.map((payload) => payload['action']).toList();
      expect(actions, contains('session_load'));
      expect(
        service.sentPayloads
            .where((payload) => payload['action'] == 'session_load')
            .single['sessionId'],
        'session-short',
      );
      expect(actions, isNot(contains('session_context_get')));
      expect(actions, isNot(contains('permission_rule_list')));
      expect(actions, isNot(contains('runtime_process_list')));
      expect(actions, isNot(contains('task_snapshot_get')));
    });

    test('前台恢复只发 session_resume，history_loaded delta 仍会补齐', () async {
      final service = _FakeMobileVcWsService();
      final controller = SessionController(service: service);
      await controller.initialize();
      addTearDown(controller.disposeController);

      await controller.connect();
      service.emit(SessionHistoryEvent(
        timestamp: _timestamp,
        sessionId: 'session-target',
        runtimeMeta: const RuntimeMeta(command: 'claude'),
        raw: const {'type': 'session_history'},
        summary: const SessionSummary(id: 'session-target', title: '历史会话'),
        resumeRuntimeMeta: const RuntimeMeta(cwd: '/workspace'),
      ));
      await _flushEvents();

      service.sentPayloads.clear();
      controller.resumeConnectionIfNeeded();
      await _flushEvents();
      expect(
        service.sentPayloads.any((payload) =>
            payload['action'] == 'session_resume' &&
            payload['reason'] == 'foreground' &&
            payload['limit'] == 120),
        isTrue,
      );
      expect(
        service.sentPayloads.any((payload) =>
            payload['action'] == 'session_delta_get' &&
            payload['reason'] == 'foreground'),
        isFalse,
        reason: 'foreground resume should not also request a potentially large '
            'delta before the bounded history event resets known counters',
      );

      service.sentPayloads.clear();
      service.emit(SessionHistoryEvent(
        timestamp: _timestamp.add(const Duration(seconds: 1)),
        sessionId: 'session-target',
        runtimeMeta: const RuntimeMeta(command: 'claude'),
        raw: const {'type': 'session_history'},
        summary: const SessionSummary(id: 'session-target', title: '新历史'),
        resumeRuntimeMeta: const RuntimeMeta(cwd: '/workspace'),
      ));
      await _flushEvents();

      expect(
        service.sentPayloads.any((payload) =>
            payload['action'] == 'session_delta_get' &&
            payload['reason'] == 'history_loaded'),
        isTrue,
        reason: 'history_loaded is the first post-history sync and must bypass '
            'the short duplicate-request coalesce window',
      );
    });

    test('foreground stale resume_result does not overwrite running state',
        () async {
      final service = _FakeMobileVcWsService();
      final controller = SessionController(service: service);
      await controller.initialize();
      addTearDown(controller.disposeController);

      await controller.connect();
      service.emit(SessionHistoryEvent(
        timestamp: _timestamp,
        sessionId: 'session-target',
        runtimeMeta: const RuntimeMeta(),
        raw: const {'type': 'session_history'},
        summary: const SessionSummary(id: 'session-target', title: 'history'),
        runtimeAlive: true,
        resumeRuntimeMeta: const RuntimeMeta(cwd: '/workspace'),
      ));
      await _flushEvents();

      service.emit(SessionStateEvent(
        timestamp: _timestamp.add(const Duration(seconds: 1)),
        sessionId: 'session-target',
        runtimeMeta: const RuntimeMeta(),
        raw: const {'type': 'session_state'},
        state: 'RUNNING',
      ));
      service.emit(SessionResumeResultEvent(
        timestamp: _timestamp.add(const Duration(seconds: 2)),
        sessionId: 'session-target',
        runtimeMeta: const RuntimeMeta(),
        raw: const {'type': 'session_resume_result'},
        latestCursor: 2,
        runtimeAlive: true,
        runtimeState: 'RUNNING',
      ));
      await _flushEvents();
      expect(controller.canStopCurrentRun, isTrue);

      service.emit(SessionResumeResultEvent(
        timestamp: _timestamp.add(const Duration(seconds: 3)),
        sessionId: 'session-target',
        runtimeMeta: const RuntimeMeta(),
        raw: const {'type': 'session_resume_result'},
        latestCursor: 1,
        runtimeAlive: false,
        runtimeState: 'IDLE',
      ));
      await _flushEvents();

      expect(controller.canStopCurrentRun, isTrue);
    });

    test('permission approve immediately enables stop action', () async {
      final service = _FakeMobileVcWsService();
      final controller = SessionController(service: service);
      await controller.initialize();
      addTearDown(controller.disposeController);

      await controller.connect();
      service.emit(SessionHistoryEvent(
        timestamp: _timestamp,
        sessionId: 'session-current',
        runtimeMeta: const RuntimeMeta(),
        raw: const {'type': 'session_history'},
        summary: const SessionSummary(id: 'session-current', title: 'current'),
        resumeRuntimeMeta: const RuntimeMeta(cwd: '/workspace'),
      ));
      service.emit(InteractionRequestEvent(
        timestamp: _timestamp.add(const Duration(seconds: 1)),
        sessionId: 'session-current',
        runtimeMeta: const RuntimeMeta(
          blockingKind: 'permission',
          permissionRequestId: 'perm-1',
          command: 'codex resume thread-1',
          engine: 'codex',
          cwd: '/workspace',
        ),
        raw: const {'type': 'interaction_request'},
        kind: 'permission',
        message: 'permission required',
        options: const [
          PromptOption(value: 'approve', label: 'approve'),
          PromptOption(value: 'deny', label: 'deny'),
        ],
        contextId: 'perm-ctx',
        resumeSessionId: 'thread-1',
      ));
      await _flushEvents();

      expect(controller.hasPendingPermissionPrompt, isTrue);
      expect(controller.canStopCurrentRun, isFalse);

      controller.submitPromptOption('approve');
      await _flushEvents();

      final decisionPayload = service.sentPayloads.singleWhere(
        (payload) => payload['action'] == 'permission_decision',
      );
      expect(decisionPayload['decision'], 'approve');
      expect(controller.hasPendingPermissionPrompt, isFalse);
      expect(controller.canStopCurrentRun, isTrue);
    });

    test('stale session_delta 不会把旧历史追加到当前最新消息后面', () async {
      final service = _FakeMobileVcWsService();
      final controller = SessionController(service: service);
      await controller.initialize();
      addTearDown(controller.disposeController);

      await controller.connect();
      service.emit(SessionHistoryEvent(
        timestamp: _timestamp,
        sessionId: 'session-current',
        runtimeMeta: const RuntimeMeta(command: 'claude'),
        raw: const {'type': 'session_history'},
        summary: const SessionSummary(id: 'session-current', title: '当前会话'),
        logEntries: const [
          HistoryLogEntry(
            kind: 'markdown',
            message: 'Android release APK 已重新打包成功',
            timestamp: '2026-05-27T05:53:00Z',
          ),
        ],
        resumeRuntimeMeta: RuntimeMeta(command: 'claude'),
      ));
      await _flushEvents();

      service.emit(SessionDeltaEvent(
        timestamp: _timestamp.add(const Duration(seconds: 1)),
        sessionId: 'session-current',
        runtimeMeta: const RuntimeMeta(command: 'claude'),
        raw: const {'type': 'session_delta'},
        summary: const SessionSummary(id: 'session-current', title: '当前会话'),
        base: const SessionDeltaKnown(eventCursor: 1, logEntryCount: 0),
        latest: const SessionDeltaKnown(eventCursor: 3, logEntryCount: 2),
        appendLogEntries: const [
          HistoryLogEntry(
            kind: 'user',
            message: 'LAN-first 和 Relay fallback 方案',
            timestamp: '2026-05-27T05:20:00Z',
          ),
        ],
        resumeRuntimeMeta: RuntimeMeta(command: 'claude'),
      ));
      await _flushEvents();

      expect(
        controller.timeline.map((item) => item.body).toList(),
        ['Android release APK 已重新打包成功'],
      );
    });

    test('oversized session_delta marker 会用窗口化 resume 重新同步当前会话', () async {
      final service = _FakeMobileVcWsService();
      final controller = SessionController(service: service);
      await controller.initialize();
      addTearDown(controller.disposeController);

      await controller.connect();
      service.emit(SessionCreatedEvent(
        timestamp: _timestamp,
        sessionId: 'conn-1',
        runtimeMeta: const RuntimeMeta(command: 'claude'),
        raw: const {'type': 'session_created'},
        summary: const SessionSummary(id: 'session-current', title: '当前会话'),
      ));
      await _flushEvents();
      service.sentPayloads.clear();

      service.emit(SessionHistoryEvent(
        timestamp: _timestamp,
        sessionId: 'session-current',
        runtimeMeta: const RuntimeMeta(command: 'claude'),
        raw: const {'type': 'session_history'},
        summary: const SessionSummary(id: 'session-current', title: '当前会话'),
        logEntryStart: 120,
        logEntryTotal: 240,
        hasMoreBefore: true,
        resumeRuntimeMeta: const RuntimeMeta(cwd: '/workspace'),
      ));
      await _flushEvents();

      service.sentPayloads.clear();
      service.emit(SessionDeltaEvent(
        timestamp: _timestamp.add(const Duration(seconds: 1)),
        sessionId: 'session-current',
        runtimeMeta: const RuntimeMeta(command: 'claude'),
        raw: const {'type': 'session_delta'},
        summary: const SessionSummary(id: 'session-current', title: '当前会话'),
        base: const SessionDeltaKnown(),
        latest: const SessionDeltaKnown(logEntryCount: 240),
        requiresFullSync: true,
        resumeRuntimeMeta: const RuntimeMeta(cwd: '/workspace'),
      ));
      await _flushEvents();

      final resumePayload = service.sentPayloads.singleWhere(
        (payload) => payload['action'] == 'session_resume',
      );
      expect(resumePayload['sessionId'], 'session-current');
      expect(resumePayload['reason'], 'delta_base_mismatch');
      expect(resumePayload['limit'], 120);
    });

    test('payload-limited history 使用 latest known 避免继续请求巨量 delta', () async {
      final service = _FakeMobileVcWsService();
      final controller = SessionController(service: service);
      await controller.initialize();
      addTearDown(controller.disposeController);

      await controller.connect();
      service.emit(SessionCreatedEvent(
        timestamp: _timestamp,
        sessionId: 'conn-1',
        runtimeMeta: const RuntimeMeta(command: 'claude'),
        raw: const {'type': 'session_created'},
        summary: const SessionSummary(id: 'session-current', title: '当前会话'),
      ));
      await _flushEvents();
      service.sentPayloads.clear();

      service.emit(SessionHistoryEvent(
        timestamp: _timestamp,
        sessionId: 'session-current',
        runtimeMeta: const RuntimeMeta(command: 'claude'),
        raw: const {'type': 'session_history'},
        summary: const SessionSummary(id: 'session-current', title: '当前会话'),
        logEntries: const [
          HistoryLogEntry(
            kind: 'markdown',
            message: '窗口里的最后一条',
            timestamp: '2026-05-27T05:53:00Z',
          ),
        ],
        logEntryStart: 13728,
        logEntryTotal: 13848,
        hasMoreBefore: true,
        latest: const SessionDeltaKnown(
          eventCursor: 99,
          logEntryCount: 13848,
          diffCount: 7,
          terminalExecutionCount: 3,
          terminalStdoutLength: 2048,
          terminalStderrLength: 12,
        ),
        payloadLimited: true,
        payloadLimitReason: 'payload_budget_exceeded',
        resumeRuntimeMeta: const RuntimeMeta(cwd: '/workspace'),
      ));
      await _flushEvents();

      await Future<void>.delayed(const Duration(milliseconds: 50));
      service.sentPayloads.clear();
      service.emit(SessionUpdatedEvent(
        timestamp: _timestamp.add(const Duration(seconds: 1)),
        sessionId: 'session-current',
        runtimeMeta: const RuntimeMeta(command: 'claude'),
        raw: const {'type': 'session_updated'},
        generation: 1,
        eventCursor: 100,
        reason: 'projection_persisted',
      ));
      await Future<void>.delayed(const Duration(milliseconds: 900));

      final deltaPayload = service.sentPayloads.singleWhere(
        (payload) =>
            payload['action'] == 'session_delta_get' &&
            payload['reason'] == 'session_updated:projection_persisted',
      );
      final known = deltaPayload['known'] as Map<String, dynamic>;
      expect(known['eventCursor'], 99);
      expect(known['logEntryCount'], 13848);
      expect(known['diffCount'], 7);
      expect(known['terminalExecutionCount'], 3);
      expect(known['terminalStdoutLength'], 2048);
      expect(known['terminalStderrLength'], 12);
    });

    test('other-session full-sync delta marker 不会触发当前会话 resume', () async {
      final service = _FakeMobileVcWsService();
      final controller = SessionController(service: service);
      await controller.initialize();
      addTearDown(controller.disposeController);

      await controller.connect();
      service.emit(SessionCreatedEvent(
        timestamp: _timestamp,
        sessionId: 'conn-1',
        runtimeMeta: const RuntimeMeta(command: 'claude'),
        raw: const {'type': 'session_created'},
        summary: const SessionSummary(id: 'session-current', title: '当前会话'),
      ));
      await _flushEvents();
      service.sentPayloads.clear();

      service.emit(SessionHistoryEvent(
        timestamp: _timestamp,
        sessionId: 'session-current',
        runtimeMeta: const RuntimeMeta(command: 'claude'),
        raw: const {'type': 'session_history'},
        summary: const SessionSummary(id: 'session-current', title: '当前会话'),
        resumeRuntimeMeta: const RuntimeMeta(cwd: '/workspace'),
      ));
      await _flushEvents();

      service.sentPayloads.clear();
      service.emit(SessionDeltaEvent(
        timestamp: _timestamp.add(const Duration(seconds: 1)),
        sessionId: 'session-other',
        runtimeMeta: const RuntimeMeta(command: 'claude'),
        raw: const {'type': 'session_delta'},
        summary: const SessionSummary(id: 'session-other', title: '其他会话'),
        requiresFullSync: true,
      ));
      await _flushEvents();

      expect(
        service.sentPayloads
            .where((payload) => payload['action'] == 'session_resume'),
        isEmpty,
      );
      expect(controller.selectedSessionId, 'session-current');
    });

    test('session_delta 重放已恢复的历史 entry 时不会重复显示', () async {
      final service = _FakeMobileVcWsService();
      final controller = SessionController(service: service);
      await controller.initialize();
      addTearDown(controller.disposeController);

      const replayedEntry = HistoryLogEntry(
        kind: 'markdown',
        message: '同一条原生历史回复',
        timestamp: '2026-05-27T05:53:00Z',
        executionId: 'exec-native-1',
      );

      await controller.connect();
      service.emit(SessionHistoryEvent(
        timestamp: _timestamp,
        sessionId: 'session-current',
        runtimeMeta: const RuntimeMeta(command: 'codex'),
        raw: const {'type': 'session_history'},
        summary: const SessionSummary(id: 'session-current', title: '当前会话'),
        logEntries: const [replayedEntry],
        resumeRuntimeMeta: const RuntimeMeta(command: 'codex'),
      ));
      await _flushEvents();

      service.emit(SessionDeltaEvent(
        timestamp: _timestamp.add(const Duration(seconds: 1)),
        sessionId: 'session-current',
        runtimeMeta: const RuntimeMeta(command: 'codex'),
        raw: const {'type': 'session_delta'},
        summary: const SessionSummary(id: 'session-current', title: '当前会话'),
        base: const SessionDeltaKnown(eventCursor: 1, logEntryCount: 1),
        latest: const SessionDeltaKnown(eventCursor: 2, logEntryCount: 2),
        appendLogEntries: const [replayedEntry],
        resumeRuntimeMeta: const RuntimeMeta(command: 'codex'),
      ));
      await _flushEvents();

      expect(
        controller.timeline.where((item) => item.body == '同一条原生历史回复').length,
        1,
      );
    });

    test('lazy hydration events merge terminal diff and execution state',
        () async {
      final service = _FakeMobileVcWsService();
      final controller = SessionController(service: service);
      await controller.initialize();
      addTearDown(controller.disposeController);

      await controller.connect();
      service.emit(SessionHistoryEvent(
        timestamp: _timestamp,
        sessionId: 'session-current',
        runtimeMeta: const RuntimeMeta(command: 'claude'),
        raw: const {'type': 'session_history'},
        summary: const SessionSummary(id: 'session-current', title: '当前会话'),
        logEntries: const [
          HistoryLogEntry(kind: 'markdown', message: 'visible'),
        ],
        latest: const SessionDeltaKnown(
          eventCursor: 3,
          logEntryCount: 1,
          diffCount: 2,
          terminalExecutionCount: 1,
          terminalStdoutLength: 10,
          terminalStderrLength: 6,
        ),
        resumeRuntimeMeta: const RuntimeMeta(cwd: '/workspace'),
      ));
      await _flushEvents();

      service.sentPayloads.clear();
      controller.requestTerminalHydration();
      controller.requestDiffPage();

      expect(
        service.sentPayloads.where(
            (payload) => payload['action'] == 'session_terminal_range_get'),
        hasLength(2),
      );
      expect(
        service.sentPayloads.where((payload) =>
            payload['action'] == 'session_terminal_execution_page_get'),
        hasLength(1),
      );
      expect(
        service.sentPayloads
            .where((payload) => payload['action'] == 'session_diff_page_get'),
        hasLength(1),
      );

      service.emit(SessionTerminalRangeEvent(
        timestamp: _timestamp.add(const Duration(milliseconds: 1)),
        sessionId: 'session-current',
        runtimeMeta: const RuntimeMeta(command: 'claude'),
        raw: const {'type': 'session_terminal_range'},
        stream: 'stdout',
        start: 0,
        end: 10,
        total: 10,
        content: '0123456789',
        latest: const SessionDeltaKnown(
          eventCursor: 4,
          logEntryCount: 1,
          diffCount: 2,
          terminalExecutionCount: 1,
          terminalStdoutLength: 10,
          terminalStderrLength: 6,
        ),
      ));
      service.emit(SessionDiffPageEvent(
        timestamp: _timestamp.add(const Duration(milliseconds: 2)),
        sessionId: 'session-current',
        runtimeMeta: const RuntimeMeta(command: 'claude'),
        raw: const {'type': 'session_diff_page'},
        diffStart: 1,
        diffTotal: 2,
        hasMoreBefore: true,
        diffs: const [
          HistoryContext(
            id: 'diff-2',
            type: 'diff',
            path: '/workspace/a.dart',
            title: 'a.dart',
            diff: '+new',
            pendingReview: true,
            groupId: 'group-1',
          ),
        ],
        reviewGroups: const [
          ReviewGroup(id: 'group-1', title: 'Review', pendingCount: 1),
        ],
        activeReviewGroup:
            const ReviewGroup(id: 'group-1', title: 'Review', pendingCount: 1),
        currentDiff: const HistoryContext(
          id: 'diff-2',
          type: 'diff',
          path: '/workspace/a.dart',
          title: 'a.dart',
          diff: '+new',
          pendingReview: true,
          groupId: 'group-1',
        ),
      ));
      service.emit(SessionTerminalExecutionPageEvent(
        timestamp: _timestamp.add(const Duration(milliseconds: 3)),
        sessionId: 'session-current',
        runtimeMeta: const RuntimeMeta(command: 'claude'),
        raw: const {'type': 'session_terminal_execution_page'},
        executionStart: 0,
        executionTotal: 1,
        terminalExecutions: const [
          TerminalExecution(
            executionId: 'exec-1',
            command: 'go test',
          ),
        ],
      ));
      await _flushEvents();

      expect(controller.terminalStdout, '0123456789');
      expect(controller.terminalExecutions.single.executionId, 'exec-1');
      expect(controller.currentDiffContext?.id, 'diff-2');
      expect(controller.reviewGroups, hasLength(1));
      expect(controller.timeline.where((item) => item.body == 'visible'),
          hasLength(1));
    });

    test('terminal range hydration counts inserted log separators as bytes',
        () async {
      final service = _FakeMobileVcWsService();
      final controller = SessionController(service: service);
      await controller.initialize();
      addTearDown(controller.disposeController);

      const firstLine = '2026/04/04 15:12:25 [INFO] 你好';
      const secondLine = '2026/04/04 15:12:26 [INFO] abc';
      await controller.connect();
      service.emit(SessionHistoryEvent(
        timestamp: _timestamp,
        sessionId: 'session-current',
        runtimeMeta: const RuntimeMeta(command: 'claude'),
        raw: const {'type': 'session_history'},
        summary: const SessionSummary(id: 'session-current', title: '当前会话'),
        latest: SessionDeltaKnown(
          eventCursor: 3,
          logEntryCount: 0,
          terminalStdoutLength: utf8.encode('$firstLine\n$secondLine!').length,
        ),
        resumeRuntimeMeta: const RuntimeMeta(cwd: '/workspace'),
      ));
      await _flushEvents();

      service.emit(LogEvent(
        timestamp: _timestamp.add(const Duration(milliseconds: 1)),
        sessionId: 'session-current',
        runtimeMeta: const RuntimeMeta(command: 'claude'),
        raw: const {'type': 'log'},
        message: firstLine,
        stream: 'stdout',
      ));
      service.emit(LogEvent(
        timestamp: _timestamp.add(const Duration(milliseconds: 2)),
        sessionId: 'session-current',
        runtimeMeta: const RuntimeMeta(command: 'claude'),
        raw: const {'type': 'log'},
        message: secondLine,
        stream: 'stdout',
      ));
      await _flushEvents();

      expect(controller.terminalStdout, '$firstLine\n$secondLine');
      service.sentPayloads.clear();
      controller.requestTerminalRange('stdout');
      expect(
        service.sentPayloads.singleWhere(
          (payload) => payload['action'] == 'session_terminal_range_get',
        )['start'],
        utf8.encode('$firstLine\n$secondLine').length,
      );
    });

    test('terminal range hydration uses UTF-8 byte offsets', () async {
      final service = _FakeMobileVcWsService();
      final controller = SessionController(service: service);
      await controller.initialize();
      addTearDown(controller.disposeController);

      await controller.connect();
      service.emit(SessionHistoryEvent(
        timestamp: _timestamp,
        sessionId: 'session-current',
        runtimeMeta: const RuntimeMeta(command: 'claude'),
        raw: const {'type': 'session_history'},
        summary: const SessionSummary(id: 'session-current', title: '当前会话'),
        logEntries: const [
          HistoryLogEntry(kind: 'markdown', message: 'visible'),
        ],
        latest: SessionDeltaKnown(
          eventCursor: 3,
          logEntryCount: 1,
          terminalStdoutLength: utf8.encode('你好abc').length,
        ),
        resumeRuntimeMeta: const RuntimeMeta(cwd: '/workspace'),
      ));
      await _flushEvents();

      service.sentPayloads.clear();
      controller.requestTerminalRange('stdout');
      expect(
        service.sentPayloads.singleWhere(
          (payload) => payload['action'] == 'session_terminal_range_get',
        )['start'],
        0,
      );

      service.emit(SessionTerminalRangeEvent(
        timestamp: _timestamp.add(const Duration(milliseconds: 1)),
        sessionId: 'session-current',
        runtimeMeta: const RuntimeMeta(command: 'claude'),
        raw: const {'type': 'session_terminal_range'},
        stream: 'stdout',
        start: 0,
        end: utf8.encode('你好').length,
        total: utf8.encode('你好abc').length,
        content: '你好',
        latest: SessionDeltaKnown(
          eventCursor: 4,
          logEntryCount: 1,
          terminalStdoutLength: utf8.encode('你好abc').length,
        ),
      ));
      await _flushEvents();

      service.sentPayloads.clear();
      controller.requestTerminalRange('stdout');
      expect(
        service.sentPayloads.singleWhere(
          (payload) => payload['action'] == 'session_terminal_range_get',
        )['start'],
        utf8.encode('你好').length,
      );

      service.emit(SessionTerminalRangeEvent(
        timestamp: _timestamp.add(const Duration(milliseconds: 2)),
        sessionId: 'session-current',
        runtimeMeta: const RuntimeMeta(command: 'claude'),
        raw: const {'type': 'session_terminal_range'},
        stream: 'stdout',
        start: utf8.encode('你好').length,
        end: utf8.encode('你好abc').length,
        total: utf8.encode('你好abc').length,
        content: 'abc',
        latest: SessionDeltaKnown(
          eventCursor: 5,
          logEntryCount: 1,
          terminalStdoutLength: utf8.encode('你好abc').length,
        ),
      ));
      await _flushEvents();

      expect(controller.terminalStdout, '你好abc');
      service.sentPayloads.clear();
      controller.requestTerminalRange('stdout');
      expect(
        service.sentPayloads.where(
            (payload) => payload['action'] == 'session_terminal_range_get'),
        isEmpty,
      );
    });

    test('loadSession 期间不属于目标的 stale history 仍会被丢弃', () async {
      final service = _FakeMobileVcWsService();
      final controller = SessionController(service: service);
      await controller.initialize();
      addTearDown(controller.disposeController);

      await controller.connect();

      controller.loadSession('session-target');
      expect(controller.isLoadingSession, isTrue);

      service.emit(SessionHistoryEvent(
        timestamp: _timestamp,
        sessionId: 'session-other',
        runtimeMeta: const RuntimeMeta(command: 'claude'),
        raw: const {'type': 'session_history'},
        summary: const SessionSummary(id: 'session-other', title: 'Stale'),
        resumeRuntimeMeta: const RuntimeMeta(),
      ));
      await _flushEvents();

      expect(controller.selectedSessionId, isNot('session-other'));
      expect(controller.isLoadingSession, isTrue);
    });
  });
}

final _timestamp = DateTime(2026, 1, 1);

TimelineItem _timelineItem(String body) => TimelineItem(
      id: 'timeline-$body',
      kind: 'markdown',
      timestamp: _timestamp,
      body: body,
    );

FileDiffEvent _reviewDiffEvent({
  required String contextId,
  required String path,
  required String title,
  required String groupId,
  required String groupTitle,
}) {
  return FileDiffEvent(
    timestamp: _timestamp,
    sessionId: 'session-1',
    runtimeMeta: RuntimeMeta(
      contextId: contextId,
      contextTitle: title,
      targetPath: path,
      groupId: groupId,
      groupTitle: groupTitle,
    ),
    raw: const {'type': 'file_diff'},
    path: path,
    title: title,
    diff: '@@ -1 +1 @@\n-old\n+new',
    lang: 'dart',
  );
}

class _FakeMobileVcWsService extends MobileVcWsService {
  _FakeMobileVcWsService() : super();

  final StreamController<AppEvent> _controller =
      StreamController<AppEvent>.broadcast();
  final List<Map<String, dynamic>> sentPayloads = [];
  final List<String> connectedUrls = [];
  final List<String> readyDirectUrls = [];
  final List<_RelayConnectCall> connectedRelays = [];
  final List<Object> connectErrors = [];
  final List<Object> readyDirectErrors = [];
  int connectCalls = 0;
  int disconnectCalls = 0;
  int resetRelayBindingCalls = 0;
  bool relayE2eeHandshake = false;
  bool relayE2eeDeviceBinding = false;
  Uint8List relayNodeFingerprint =
      Uint8List.fromList(List<int>.filled(32, 0x11));
  Object? connectRelayError;
  String markedRelayDeviceId = '';
  RelayFileDownloadResult relayDownloadResult = RelayFileDownloadResult(
    bytes: Uint8List.fromList(<int>[1, 2, 3]),
    fileName: 'download.bin',
  );

  @override
  Stream<AppEvent> get events => _controller.stream;

  @override
  bool get hasRelayE2eeHandshake => relayE2eeHandshake;

  @override
  bool get hasRelayE2eeDeviceBinding => relayE2eeDeviceBinding;

  @override
  RelaySecurityInput relaySecurityInput({
    required String connectionMode,
    String expectedNodeFingerprintHex = '',
    RelayE2eeCapabilitySet? configuredCapabilities,
  }) {
    final capabilities = configuredCapabilities;
    return RelaySecurityInput(
      connectionMode: connectionMode,
      expectedNodeFingerprintHex: expectedNodeFingerprintHex,
      actualNodePublicKey: relayNodeFingerprint,
      nodeFingerprintConfirmed: relayE2eeHandshake,
      handshakeComplete: relayE2eeHandshake,
      protocolSupportsE2ee: capabilities?.requiresE2EE == true,
      protocolSupportsTunnel: capabilities != null,
      supportsMultiplexStreams: capabilities?.supportsMultiplexStreams == true,
      supportsFileDownload: capabilities?.supportsFileDownload == true,
      supportsDeviceManagement: capabilities?.supportsDeviceManagement == true,
      requiresE2ee: capabilities?.requiresE2EE == true,
      plaintextTestMode: capabilities?.plaintextTestMode == true,
      productionPlaintextRejected: capabilities?.requiresE2EE == true &&
          capabilities?.plaintextTestMode == false,
      deviceBound: relayE2eeDeviceBinding,
    );
  }

  @override
  void markRelayDeviceRegistered(RelayDeviceRegisterResultEvent event) {
    markedRelayDeviceId = event.deviceId;
    relayE2eeDeviceBinding = event.deviceId.trim().isNotEmpty;
  }

  @override
  Future<void> resetRelayDeviceBinding() async {
    resetRelayBindingCalls++;
    relayE2eeHandshake = false;
    relayE2eeDeviceBinding = false;
  }

  @override
  Future<void> connect(String url) async {
    connectCalls++;
    connectedUrls.add(url);
    if (connectErrors.isNotEmpty) {
      throw connectErrors.removeAt(0);
    }
  }

  @override
  Future<void> connectDirectAfterReady(String url) async {
    connectCalls++;
    readyDirectUrls.add(url);
    if (readyDirectErrors.isNotEmpty) {
      throw readyDirectErrors.removeAt(0);
    }
  }

  @override
  Future<void> connectRelay({
    required String relayUrl,
    required String sessionId,
    String pairingSecret = '',
    String clientId = '',
    String clientReconnectSecret = '',
    String nodeFingerprintHex = '',
    RelayE2eeCapabilitySet? relayCapabilities,
  }) async {
    connectCalls++;
    final error = connectRelayError;
    if (error != null) {
      throw error;
    }
    _relaySession = RelaySession(
      sessionId: sessionId,
      clientId: clientId.trim().isNotEmpty ? clientId : 'rc_test',
      clientReconnectSecret: clientReconnectSecret.trim().isNotEmpty
          ? clientReconnectSecret
          : 'reconnect_secret',
    );
    connectedRelays.add(_RelayConnectCall(
      relayUrl: relayUrl,
      sessionId: sessionId,
      pairingSecret: pairingSecret,
      clientId: clientId,
      clientReconnectSecret: clientReconnectSecret,
      nodeFingerprintHex: nodeFingerprintHex,
      relayCapabilities: relayCapabilities,
    ));
  }

  RelaySession? _relaySession;

  @override
  RelaySession? takeRelaySession() {
    final session = _relaySession;
    _relaySession = null;
    return session;
  }

  @override
  Future<void> disconnect() async {
    disconnectCalls++;
  }

  @override
  bool send(Map<String, dynamic> payload) {
    sentPayloads.add(Map<String, dynamic>.from(payload));
    return true;
  }

  @override
  Future<RelayFileDownloadResult> downloadRelayFile(
    String path, {
    void Function(int receivedBytes, int? totalBytes)? onProgress,
    FutureOr<void> Function(Uint8List chunk)? onChunk,
    RelayFileDownloadCancelToken? cancelToken,
  }) async {
    if (cancelToken?.isCancelled == true) {
      throw StateError(relayFileDownloadErrorCancelled);
    }
    await onChunk?.call(relayDownloadResult.bytes ?? Uint8List(0));
    onProgress?.call(
        relayDownloadResult.bytes?.length ?? 0, relayDownloadResult.totalBytes);
    return relayDownloadResult;
  }

  void emit(AppEvent event) {
    _controller.add(event);
  }

  @override
  Future<void> dispose() async {
    await _controller.close();
  }
}

class _RelayConnectCall {
  const _RelayConnectCall({
    required this.relayUrl,
    required this.sessionId,
    required this.pairingSecret,
    required this.clientId,
    required this.clientReconnectSecret,
    required this.nodeFingerprintHex,
    required this.relayCapabilities,
  });

  final String relayUrl;
  final String sessionId;
  final String pairingSecret;
  final String clientId;
  final String clientReconnectSecret;
  final String nodeFingerprintHex;
  final RelayE2eeCapabilitySet? relayCapabilities;
}
