import 'package:flutter_test/flutter_test.dart';
import 'package:mobile_vc/core/config/app_config.dart';
import 'package:mobile_vc/core/config/relay_config.dart';

void main() {
  group('AppConfig connection urls', () {
    test('defaults to plain http and websocket transport', () {
      const config = AppConfig(
        host: '127.0.0.1',
        port: '8001',
        token: 'test-token',
      );

      expect(
        config.baseHttpUrlFor(secureTransport: false),
        'http://127.0.0.1:8001',
      );
      expect(
        config.wsUrlFor(secureTransport: false),
        'ws://127.0.0.1:8001/ws?token=test-token',
      );
    });

    test('secure transport uses https and wss before connecting', () {
      const config = AppConfig(
        host: 'api.example.com',
        port: '443',
        token: 'token with spaces',
      );

      final httpUri = Uri.parse(config.baseHttpUrlFor(secureTransport: true));
      final wsUri = Uri.parse(config.wsUrlFor(secureTransport: true));
      final downloadUri = config.downloadUri(
        'logs/session.txt',
        secureTransport: true,
      );

      expect(httpUri.scheme, 'https');
      expect(httpUri.host, 'api.example.com');
      expect(wsUri.scheme, 'wss');
      expect(wsUri.path, '/ws');
      expect(wsUri.queryParameters['token'], 'token with spaces');
      expect(downloadUri.scheme, 'https');
      expect(downloadUri.queryParameters['path'], 'logs/session.txt');
    });

    test('secure transport preserves non-default backend ports', () {
      const config = AppConfig(host: 'api.example.com', port: '8443');

      expect(
        config.baseHttpUrlFor(secureTransport: true),
        'https://api.example.com:8443',
      );
      expect(
        config.wsUrlFor(secureTransport: true),
        'wss://api.example.com:8443/ws?token=test',
      );
    });

    test('manual host accepts http URL without treating it as a host', () {
      const config = AppConfig(host: 'http://example.com', port: '9999');

      expect(config.baseHttpUrl, 'http://example.com:9999');
      expect(config.displayEndpoint, 'http://example.com:9999');
      expect(config.displayHost, 'http://example.com');
      expect(config.wsUrl, 'ws://example.com:9999/ws?token=test');
    });

    test('manual host URL can carry backend port', () {
      const config = AppConfig(host: 'https://example.com:9999');

      expect(config.baseHttpUrl, 'https://example.com:9999');
      expect(config.displayEndpoint, 'https://example.com:9999');
      expect(config.displayHost, 'https://example.com');
      expect(config.wsUrl, 'wss://example.com:9999/ws?token=test');
    });

    test('manual host port accepts a trailing slash', () {
      const config = AppConfig(host: 'example.com:9999/');

      expect(config.baseHttpUrlFor(secureTransport: true),
          'https://example.com:9999');
      expect(config.wsUrlFor(secureTransport: true),
          'wss://example.com:9999/ws?token=test');
    });

    test('copyWith stores manual endpoint URL as host and port', () {
      final config = const AppConfig(port: '19000').copyWith(
        host: 'http://example.com:9999',
      );

      expect(config.host, 'example.com');
      expect(config.port, '9999');
      expect(config.secureTransport, isFalse);
      expect(config.wsUrl, 'ws://example.com:9999/ws?token=test');
    });

    test('fromJson migrates legacy endpoint URL host', () {
      final config = AppConfig.fromJson(const {
        'host': 'http://example.com:9999',
        'port': '19000',
      });

      expect(config.host, 'example.com');
      expect(config.port, '9999');
      expect(config.secureTransport, isFalse);
      expect(config.baseHttpUrl, 'http://example.com:9999');
    });

    test('fromJson preserves explicit secure transport for plain host', () {
      final config = AppConfig.fromJson(const {
        'host': 'example.com',
        'port': '9999',
        'secureTransport': true,
      });

      expect(config.baseHttpUrl, 'https://example.com:9999');
      expect(config.wsUrl, 'wss://example.com:9999/ws?token=test');
    });

    test('launch uri without port uses scheme default port', () {
      final config = AppConfig.fromLaunchUri(
        'https://example.com?token=test',
        fallback: const AppConfig(port: '19080'),
      );

      expect(config, isNotNull);
      expect(config!.host, 'example.com');
      expect(config.port, isEmpty);
      expect(config.baseHttpUrl, 'https://example.com');
      expect(config.wsUrl, 'wss://example.com/ws?token=test');
    });

    test('launch uri with http scheme defaults to port 80', () {
      final config = AppConfig.fromLaunchUri(
        'http://example.com?token=test',
        fallback: const AppConfig(port: '19080'),
      );

      expect(config, isNotNull);
      expect(config!.port, isEmpty);
      expect(config.baseHttpUrl, 'http://example.com');
      expect(config.wsUrl, 'ws://example.com/ws?token=test');
    });

    test('launch uri keeps explicit non-default backend port', () {
      final config = AppConfig.fromLaunchUri(
        'https://example.com:8443?token=test',
        fallback: const AppConfig(port: '19080'),
      );

      expect(config, isNotNull);
      expect(config!.port, '8443');
      expect(config.baseHttpUrl, 'https://example.com:8443');
      expect(config.wsUrl, 'wss://example.com:8443/ws?token=test');
    });

    test('invalid port surfaces as a format error', () {
      const config = AppConfig(port: 'not-a-port');

      expect(
        () => config.wsUrlFor(secureTransport: false),
        throwsA(isA<FormatException>()),
      );
    });

    test('relay pairing uri selects relay mode without changing direct fields',
        () {
      final config = AppConfig.fromLaunchUri(
        'mobilevc://relay/v1?relay=wss%3A%2F%2Frelay.example.test&session=rs_test&secret=pair_secret&exp=4102444800&nodeFingerprint=$testNodeFingerprint',
        fallback: const AppConfig(
          host: '192.168.1.2',
          port: '8001',
          token: 'direct-token',
        ),
      );

      expect(config, isNotNull);
      expect(config!.isRelayMode, isTrue);
      expect(config.host, '192.168.1.2');
      expect(config.port, '8001');
      expect(config.token, 'direct-token');
      expect(config.relayUrl, 'wss://relay.example.test');
      expect(config.relaySessionId, 'rs_test');
      expect(config.relayPairingSecret, 'pair_secret');
      expect(config.relayPairingExpiresAt, 4102444800);
      expect(config.relayNodeFingerprintHex, testNodeFingerprint);
    });

    test('relay pairing uri with LAN endpoint selects auto mode', () {
      final config = AppConfig.fromLaunchUri(
        'mobilevc://relay/v1?relay=wss%3A%2F%2Frelay.example.test'
        '&session=rs_test&secret=pair_secret&exp=4102444800'
        '&nodeFingerprint=$testNodeFingerprint'
        '&lanHost=192.168.1.9&lanPort=19080&lanToken=direct_secret'
        '&lanCwd=%2Fworkspace&lanSecureTransport=false',
        fallback: const AppConfig(
          host: 'old-host',
          port: '8001',
          token: 'old-token',
          secureTransport: true,
        ),
      );

      expect(config, isNotNull);
      expect(config!.connectionMode, ConnectionMode.auto.name);
      expect(config.isAutoMode, isTrue);
      expect(config.host, '192.168.1.9');
      expect(config.port, '19080');
      expect(config.token, 'direct_secret');
      expect(config.cwd, '/workspace');
      expect(config.secureTransport, isFalse);
      expect(config.relayUrl, 'wss://relay.example.test');
      expect(config.relaySessionId, 'rs_test');
      expect(config.relayPairingSecret, 'pair_secret');
    });

    test('relay pairing event json includes LAN endpoint in generated URI', () {
      final uri = relayPairingUriFromEventJson({
        'type': 'mobilevc.relay.pairing_ready',
        'relayUrl': 'wss://relay.example.test',
        'sessionId': 'rs_json',
        'pairingSecret': 'pair_secret',
        'expiresAt': 4102444800,
        'nodeFingerprintHex': testNodeFingerprint,
        'lanHost': '192.168.1.9',
        'lanPort': '19080',
        'lanToken': 'direct_secret',
        'lanCwd': '/workspace',
        'lanSecureTransport': false,
        'capabilities': {
          'relayProtocolVersion': 1,
          'e2eeProtocolVersion': 1,
          'cryptoSuite': 'p256-ecdsa+p256-ecdh+hkdf-sha256+aes-256-gcm',
          'tunnelProtocolVersion': 1,
          'supportsMultiplexStreams': true,
          'supportsFileDownloadStream': true,
          'supportsDeviceManagement': true,
          'requiresE2EE': true,
          'plaintextTestMode': false,
        },
      });
      final pairing = parseRelayPairingUri(uri);

      expect(pairing, isNotNull);
      expect(pairing!.hasLanEndpoint, isTrue);
      expect(pairing.lanHost, '192.168.1.9');
      expect(pairing.lanPort, '19080');
      expect(pairing.lanToken, 'direct_secret');
      expect(pairing.lanCwd, '/workspace');
      expect(pairing.lanSecureTransport, isFalse);
    });

    test('relay pairing uri rejects partial LAN endpoint', () {
      expect(
        () => parseRelayPairingUri(
          'mobilevc://relay/v1?relay=wss%3A%2F%2Frelay.example.test'
          '&session=rs_test&secret=pair_secret'
          '&nodeFingerprint=$testNodeFingerprint&lanHost=192.168.1.9',
        ),
        throwsA(
          isA<FormatException>().having(
            (error) => error.message,
            'message',
            contains('lanHost and lanToken'),
          ),
        ),
      );
    });

    test('normalizes persisted auto connection mode', () {
      final config = AppConfig.fromJson(const {'connectionMode': 'auto'});

      expect(config.connectionMode, ConnectionMode.auto.name);
      expect(config.isAutoMode, isTrue);
    });

    test('relay config persists reconnect fields but not pairing secret', () {
      const config = AppConfig(
        connectionMode: 'relay',
        relayUrl: 'wss://relay.example.test',
        relaySessionId: 'rs_test',
        relayPairingSecret: 'pair_secret',
        relayPairingExpiresAt: 4102444800,
        relayClientId: 'rc_test',
        relayClientReconnectSecret: 'reconnect_secret',
        relayNodeFingerprintHex: testNodeFingerprint,
      );

      final json = config.toJson();
      expect(json['relayUrl'], 'wss://relay.example.test');
      expect(json['relaySessionId'], 'rs_test');
      expect(json.containsKey('relayPairingSecret'), isFalse);
      expect(json.containsKey('relayPairingExpiresAt'), isFalse);
      expect(json['relayClientId'], 'rc_test');
      expect(json['relayClientReconnectSecret'], 'reconnect_secret');
      expect(json['relayNodeFingerprintHex'], testNodeFingerprint);

      final restored = AppConfig.fromJson(json);
      expect(restored.relaySessionId, 'rs_test');
      expect(restored.relayClientId, 'rc_test');
      expect(restored.relayClientReconnectSecret, 'reconnect_secret');
      expect(restored.relayNodeFingerprintHex, testNodeFingerprint);
    });

    test('voice call config persists model and tts endpoints', () {
      const config = AppConfig(
        voiceApiUrl: 'https://api.example.test/v1/chat/completions',
        voiceApiKey: 'voice-key',
        voiceModelName: 'voice-chat',
        voiceTtsUrl: 'https://api.example.test/v1/audio/speech',
        voiceTtsApiKey: 'tts-key',
        voiceTtsModelName: 'voice-tts',
        voiceTtsVoice: 'verse',
      );

      expect(config.hasVoiceCallConfig, isTrue);
      expect(config.hasVoiceTtsConfig, isTrue);

      final json = config.toJson();
      expect(
          json['voiceApiUrl'], 'https://api.example.test/v1/chat/completions');
      expect(json['voiceApiKey'], 'voice-key');
      expect(json['voiceModelName'], 'voice-chat');
      expect(json['voiceTtsUrl'], 'https://api.example.test/v1/audio/speech');
      expect(json['voiceTtsApiKey'], 'tts-key');
      expect(json['voiceTtsModelName'], 'voice-tts');
      expect(json['voiceTtsVoice'], 'verse');

      final restored = AppConfig.fromJson(json);
      expect(restored.voiceApiUrl, config.voiceApiUrl);
      expect(restored.voiceApiKey, config.voiceApiKey);
      expect(restored.voiceModelName, config.voiceModelName);
      expect(restored.voiceTtsUrl, config.voiceTtsUrl);
      expect(restored.voiceTtsApiKey, config.voiceTtsApiKey);
      expect(restored.voiceTtsModelName, config.voiceTtsModelName);
      expect(restored.voiceTtsVoice, config.voiceTtsVoice);
    });

    test('invalid persisted relay capabilities keep reconnect credentials', () {
      final config = AppConfig.fromJson(const {
        'connectionMode': 'relay',
        'relayUrl': 'wss://relay.example.test',
        'relaySessionId': 'rs_test',
        'relayClientId': 'rc_test',
        'relayClientReconnectSecret': 'reconnect_secret',
        'relayNodeFingerprintHex': testNodeFingerprint,
        'relayCapabilities': {
          'relayProtocolVersion': 0,
        },
      });

      expect(config.isRelayMode, isTrue);
      expect(config.relayUrl, 'wss://relay.example.test');
      expect(config.relaySessionId, 'rs_test');
      expect(config.relayClientId, 'rc_test');
      expect(config.relayClientReconnectSecret, 'reconnect_secret');
      expect(config.relayNodeFingerprintHex, testNodeFingerprint);
      config.relayCapabilities!.validateProduction();
    });

    test('same relay pairing import preserves existing reconnect credentials',
        () {
      final config = AppConfig.fromLaunchUri(
        'mobilevc://relay/v1?relay=wss%3A%2F%2Frelay.example.test'
        '&session=rs_test&secret=stale_pair_secret&exp=1'
        '&nodeFingerprint=$testNodeFingerprint',
        fallback: const AppConfig(
          connectionMode: 'relay',
          relayUrl: 'wss://relay.example.test',
          relaySessionId: 'rs_test',
          relayClientId: 'rc_existing',
          relayClientReconnectSecret: 'reconnect_existing',
          relayNodeFingerprintHex: testNodeFingerprint,
        ),
      );

      expect(config, isNotNull);
      expect(config!.relayPairingSecret, isEmpty);
      expect(config.relayPairingExpiresAt, 0);
      expect(config.relayClientId, 'rc_existing');
      expect(config.relayClientReconnectSecret, 'reconnect_existing');
    });

    test('expired relay pairing import rejects new sessions', () {
      expect(
        () => AppConfig.fromLaunchUri(
          'mobilevc://relay/v1?relay=wss%3A%2F%2Frelay.example.test'
          '&session=rs_expired&secret=stale_pair_secret&exp=1'
          '&nodeFingerprint=$testNodeFingerprint',
          fallback: const AppConfig(
            connectionMode: 'relay',
            relayUrl: 'wss://relay.example.test',
            relaySessionId: 'rs_existing',
            relayClientId: 'rc_existing',
            relayClientReconnectSecret: 'reconnect_existing',
            relayNodeFingerprintHex: testNodeFingerprint,
          ),
        ),
        throwsA(
          isA<FormatException>().having(
            (error) => error.message,
            'message',
            contains('Relay 配对链接已过期'),
          ),
        ),
      );
    });

    test('relay url validation rejects public ws and http schemes', () {
      expect(
        () => AppConfig.fromLaunchUri(
          'mobilevc://relay/v1?relay=ws%3A%2F%2Frelay.example.test&session=rs&secret=secret&nodeFingerprint=$testNodeFingerprint',
        ),
        throwsFormatException,
      );
      expect(
        () => AppConfig.fromLaunchUri(
          'mobilevc://relay/v1?relay=https%3A%2F%2Frelay.example.test&session=rs&secret=secret&nodeFingerprint=$testNodeFingerprint',
        ),
        throwsFormatException,
      );
    });

    test('relay pairing uri validates capability hints', () {
      final pairing = parseRelayPairingUri(
        'mobilevc://relay/v1?relay=wss%3A%2F%2Frelay.example.test'
        '&session=rs_test&secret=pair_secret&exp=4102444800'
        '&nodeFingerprint=$testNodeFingerprint'
        '&relayProtocolVersion=1&e2eeProtocolVersion=1'
        '&cryptoSuite=p256-ecdsa%2Bp256-ecdh%2Bhkdf-sha256%2Baes-256-gcm'
        '&tunnelProtocolVersion=1&supportsMultiplexStreams=true'
        '&supportsFileDownloadStream=true&supportsDeviceManagement=true'
        '&requiresE2EE=false&plaintextTestMode=true',
      );

      expect(pairing, isNotNull);
      final parsedPairing = pairing!;
      expect(parsedPairing.nodeFingerprintHex, testNodeFingerprint);
      expect(parsedPairing.capabilities, isNotNull);
      parsedPairing.capabilities!.validatePlaintextTestMode();
    });

    test('relay capability hints are stored as non-secret config', () {
      final config = AppConfig.fromLaunchUri(
        'mobilevc://relay/v1?relay=wss%3A%2F%2Frelay.example.test'
        '&session=rs_test&secret=pair_secret&exp=4102444800'
        '&nodeFingerprint=$testNodeFingerprint'
        '&relayProtocolVersion=1&e2eeProtocolVersion=1'
        '&cryptoSuite=p256-ecdsa%2Bp256-ecdh%2Bhkdf-sha256%2Baes-256-gcm'
        '&tunnelProtocolVersion=1&supportsMultiplexStreams=true'
        '&supportsFileDownloadStream=true&supportsDeviceManagement=true'
        '&requiresE2EE=true&plaintextTestMode=false',
      );

      expect(config, isNotNull);
      config!.relayCapabilities!.validateProduction();
      final json = config.toJson();
      expect(json.containsKey('relayPairingSecret'), isFalse);
      expect(json['relayCapabilities'], isA<Map<String, Object>>());
      final restored = AppConfig.fromJson(json);
      restored.relayCapabilities!.validateProduction();
    });

    test('relay pairing uri rejects invalid capability hints', () {
      expect(
        () => parseRelayPairingUri(
          'mobilevc://relay/v1?relay=wss%3A%2F%2Frelay.example.test'
          '&session=rs_test&secret=pair_secret'
          '&nodeFingerprint=$testNodeFingerprint'
          '&relayProtocolVersion=1&e2eeProtocolVersion=1'
          '&cryptoSuite=p256-ecdsa%2Bp256-ecdh%2Bhkdf-sha256%2Baes-256-gcm'
          '&tunnelProtocolVersion=1&supportsMultiplexStreams=true'
          '&supportsFileDownloadStream=true&supportsDeviceManagement=true'
          '&requiresE2EE=true&plaintextTestMode=true',
        ),
        throwsArgumentError,
      );
    });

    test('relay pairing uri requires node fingerprint', () {
      expect(
        () => parseRelayPairingUri(
          'mobilevc://relay/v1?relay=wss%3A%2F%2Frelay.example.test'
          '&session=rs_test&secret=pair_secret',
        ),
        throwsFormatException,
      );
    });

    test('relay pairing uri reports specific missing fields', () {
      expect(
        () => parseRelayPairingUri('mobilevc://relay/v1'),
        throwsA(
          isA<FormatException>().having(
            (error) => error.message,
            'message',
            contains('relay, session, secret'),
          ),
        ),
      );
    });

    test('relay pairing event json imports as relay config', () {
      final config = AppConfig.fromLaunchUri(
        '''
{
  "type": "mobilevc.relay.pairing_ready",
  "relayUrl": "wss://relay.example.test",
  "sessionId": "rs_json",
  "pairingSecret": "pair_secret",
  "expiresAt": 4102444800,
  "nodeFingerprintHex": "$testNodeFingerprint",
  "capabilities": {
    "relayProtocolVersion": 1,
    "e2eeProtocolVersion": 1,
    "cryptoSuite": "p256-ecdsa+p256-ecdh+hkdf-sha256+aes-256-gcm",
    "tunnelProtocolVersion": 1,
    "supportsMultiplexStreams": true,
    "supportsFileDownloadStream": true,
    "supportsDeviceManagement": true,
    "requiresE2EE": true,
    "plaintextTestMode": false
  }
}
''',
      );

      expect(config, isNotNull);
      expect(config!.isRelayMode, isTrue);
      expect(config.relayUrl, 'wss://relay.example.test');
      expect(config.relaySessionId, 'rs_json');
      expect(config.relayPairingSecret, 'pair_secret');
      expect(config.relayNodeFingerprintHex, testNodeFingerprint);
      config.relayCapabilities!.validateProduction();
    });

    test('relay pairing uri rejects redacted secret', () {
      expect(
        () => parseRelayPairingUri(
          'mobilevc://relay/v1?relay=wss%3A%2F%2Frelay.example.test'
          '&session=rs_test&secret=%3Credacted%3E'
          '&nodeFingerprint=$testNodeFingerprint',
        ),
        throwsA(
          isA<FormatException>().having(
            (error) => error.message,
            'message',
            contains('secret is redacted'),
          ),
        ),
      );
    });
  });

  group('AppConfig adb ice', () {
    test('auto config builds stun and turn from host', () {
      const config = AppConfig(
        host: '8.162.1.176',
        adbIceServersJson: '{"username":"mobilevc","credential":"secret"}',
      );

      expect(config.adbIceUsername, 'mobilevc');
      expect(config.adbIceCredential, 'secret');
      expect(config.hasAutoAdbIceConfig, isTrue);
      expect(config.adbIceServers, <Map<String, dynamic>>[
        <String, dynamic>{
          'urls': <String>['stun:8.162.1.176:3478'],
        },
        <String, dynamic>{
          'urls': <String>[
            'turn:8.162.1.176:3478?transport=udp',
            'turn:8.162.1.176:3478?transport=tcp',
          ],
          'username': 'mobilevc',
          'credential': 'secret',
        },
      ]);
    });

    test('auto config honors explicit turn host override', () {
      const config = AppConfig(
        host: '8.162.1.176',
        adbIceServersJson:
            '{"host":"turn.example.com","username":"mobilevc","credential":"secret"}',
      );

      expect(config.adbIceHostOverride, 'turn.example.com');
      expect(config.adbIceServers, <Map<String, dynamic>>[
        <String, dynamic>{
          'urls': <String>['stun:turn.example.com:3478'],
        },
        <String, dynamic>{
          'urls': <String>[
            'turn:turn.example.com:3478?transport=udp',
            'turn:turn.example.com:3478?transport=tcp',
          ],
          'username': 'mobilevc',
          'credential': 'secret',
        },
      ]);
    });

    test('auto config normalizes url host override before building ice urls',
        () {
      final rawJson = AppConfig.encodeAutoAdbIceConfig(
        host: 'https://turn.example.com:9999',
        username: 'mobilevc',
        credential: 'secret',
      );
      final config = AppConfig(
        host: '8.162.1.176',
        adbIceServersJson: rawJson,
      );

      expect(config.adbIceHostOverride, 'turn.example.com');
      expect(config.adbIceServers.first['urls'], <String>[
        'stun:turn.example.com:3478',
      ]);
    });

    test('legacy json remains readable for username and credential', () {
      const config = AppConfig(
        host: '8.162.1.176',
        adbIceServersJson:
            '[{"urls":["stun:stun.l.google.com:19302"]},{"urls":["turn:8.162.1.176:3478?transport=udp"],"username":"legacy","credential":"cred"}]',
      );

      expect(config.hasAutoAdbIceConfig, isFalse);
      expect(config.adbIceUsername, 'legacy');
      expect(config.adbIceCredential, 'cred');
      expect(config.hasTurnAdbIceServer, isTrue);
      expect(config.shouldForceAdbRelay, isTrue);
      expect(config.adbIceServers.length, 2);
    });

    test('private host keeps mixed ice instead of forcing relay', () {
      const config = AppConfig(
        host: '192.168.0.8',
        adbIceServersJson: '{"username":"mobilevc","credential":"secret"}',
      );

      expect(config.hasTurnAdbIceServer, isTrue);
      expect(config.shouldForceAdbRelay, isFalse);
    });

    test('encode auto config returns empty when both fields are blank', () {
      expect(
        AppConfig.encodeAutoAdbIceConfig(username: ' ', credential: ''),
        isEmpty,
      );
    });

    test('encode auto config keeps explicit turn host override', () {
      expect(
        AppConfig.encodeAutoAdbIceConfig(
          host: 'turn.example.com',
          username: 'mobilevc',
          credential: 'secret',
        ),
        '{"host":"turn.example.com","username":"mobilevc","credential":"secret"}',
      );
    });

    test('legacy model config migrates into the matching engine slot', () {
      final config = AppConfig.fromJson(const {
        'engine': 'codex',
        'model': 'gpt-5.4',
        'reasoningEffort': 'high',
      });

      expect(config.codexModel, 'gpt-5.4');
      expect(config.codexReasoningEffort, 'high');
      expect(config.claudeModel, isEmpty);
    });

    test('separate Claude and Codex model settings are preserved', () {
      const config = AppConfig(
        engine: 'claude',
        claudeModel: 'opus',
        codexModel: 'gpt-5.4',
        codexReasoningEffort: 'high',
      );

      expect(config.modelForEngine('claude'), 'opus');
      expect(config.modelForEngine('codex'), 'gpt-5.4');
      expect(config.reasoningEffortForEngine('codex'), 'high');
    });

    test('codex xhigh reasoning effort is preserved', () {
      const config = AppConfig(
        engine: 'codex',
        codexModel: 'gpt-5.4',
        codexReasoningEffort: 'xhigh',
      );

      expect(config.modelForEngine('codex'), 'gpt-5.4');
      expect(config.reasoningEffortForEngine('codex'), 'xhigh');
    });

    test('codex sandbox mode is persisted', () {
      const config = AppConfig(
        engine: 'codex',
        codexSandboxMode: 'danger-full-access',
        codexTargetMode: true,
        permissionMode: 'config',
      );

      final restored = AppConfig.fromJson(config.toJson());

      expect(restored.codexSandboxMode, 'danger-full-access');
      expect(restored.codexTargetMode, isTrue);
      expect(restored.permissionMode, 'config');
    });

    test('history window limit defaults and persists', () {
      expect(const AppConfig().historyWindowLimit, defaultHistoryWindowLimit);

      const config = AppConfig(historyWindowLimit: 240);
      final restored = AppConfig.fromJson(config.toJson());

      expect(restored.historyWindowLimit, 240);
      expect(
        AppConfig.fromJson(const {'historyWindowLimit': '360'})
            .historyWindowLimit,
        360,
      );
      expect(
        AppConfig.fromJson(const <String, Object?>{}).historyWindowLimit,
        defaultHistoryWindowLimit,
      );
      expect(
        () => AppConfig.fromJson(const {'historyWindowLimit': 0}),
        throwsA(isA<FormatException>()),
      );
      expect(
        () => const AppConfig().copyWith(historyWindowLimit: -1),
        throwsA(isA<FormatException>()),
      );
    });

    test('last selected session metadata is persisted', () {
      const config = AppConfig(
        lastSessionId: 'session-last',
        lastSessionCwd: '/workspace/mobilevc',
      );

      final json = config.toJson();
      final restored = AppConfig.fromJson(json);

      expect(json['lastSessionId'], 'session-last');
      expect(json['lastSessionCwd'], '/workspace/mobilevc');
      expect(restored.lastSessionId, 'session-last');
      expect(restored.lastSessionCwd, '/workspace/mobilevc');
    });

    test('launch uri overrides saved cwd token host and port', () {
      const fallback = AppConfig(
        host: '10.0.0.2',
        port: '8001',
        token: 'old-token',
        cwd: r'C:\Users\29573\Desktop\fsdownload\远程控制codex',
      );

      final next = AppConfig.fromLaunchUri(
        'http://10.136.78.122:8001/?token=123456&cwd=${Uri.encodeComponent(r'C:\Users\29573\Desktop\fsdownload\codexxm')}',
        fallback: fallback,
      );

      expect(next, isNotNull);
      expect(next!.host, '10.136.78.122');
      expect(next.port, '8001');
      expect(next.token, '123456');
      expect(next.cwd, r'C:\Users\29573\Desktop\fsdownload\codexxm');
    });
  });
}

const testNodeFingerprint =
    '0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef';
