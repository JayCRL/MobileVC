import 'package:flutter_test/flutter_test.dart';
import 'package:mobile_vc/core/config/app_config.dart';

void main() {
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
  });
}
