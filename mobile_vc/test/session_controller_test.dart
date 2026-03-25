import 'dart:async';

import 'package:flutter_test/flutter_test.dart';
import 'package:shared_preferences/shared_preferences.dart';
import 'package:mobile_vc/core/config/app_config.dart';
import 'package:mobile_vc/data/models/events.dart';
import 'package:mobile_vc/data/models/runtime_meta.dart';
import 'package:mobile_vc/data/models/session_models.dart';
import 'package:mobile_vc/data/services/mobilevc_ws_service.dart';
import 'package:mobile_vc/features/session/session_controller.dart';

Future<void> _flushEvents() async {
  await Future<void>.delayed(const Duration(milliseconds: 1));
  await Future<void>.delayed(const Duration(milliseconds: 1));
}

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();
  setUp(() {
    SharedPreferences.setMockInitialValues({});
  });

  group('SessionController permission prompt routing', () {
    test('connect 时会补发 session_context_get 和 review_state_get', () async {
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

      final actions = service.sentPayloads
          .map((payload) => payload['action'])
          .whereType<String>()
          .toList();
      expect(actions, contains('session_context_get'));
      expect(actions, contains('review_state_get'));
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
        PromptRequestEvent(
          timestamp: _timestamp,
          sessionId: 'session-1',
          runtimeMeta: const RuntimeMeta(
            command: 'claude',
            targetPath: '/workspace/README.md',
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
        PromptRequestEvent(
          timestamp: _timestamp,
          sessionId: 'session-1',
          runtimeMeta: const RuntimeMeta(command: 'claude'),
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
          runtimeMeta: RuntimeMeta(command: 'claude'),
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

    test('continueWithCurrentFile 在权限 prompt 下发送 permission_decision 而不是 input',
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
        PromptRequestEvent(
          timestamp: _timestamp,
          sessionId: 'session-1',
          runtimeMeta: const RuntimeMeta(
            resumeSessionId: 'resume-123',
            command: 'claude',
            contextId: 'ctx-1',
            contextTitle: 'README',
            targetPath: '/workspace/README.md',
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

      controller.continueWithCurrentFile('允许并继续');

      expect(service.sentPayloads, hasLength(1));
      final payload = service.sentPayloads.single;
      expect(payload['action'], 'permission_decision');
      expect(payload['decision'], 'approve');
      expect(payload.containsKey('data'), isFalse);
    });
  });
}

final _timestamp = DateTime(2026, 1, 1);

class _FakeMobileVcWsService extends MobileVcWsService {
  _FakeMobileVcWsService() : super();

  final StreamController<AppEvent> _controller =
      StreamController<AppEvent>.broadcast();
  final List<Map<String, dynamic>> sentPayloads = [];

  @override
  Stream<AppEvent> get events => _controller.stream;

  @override
  Future<void> connect(String url) async {}

  @override
  Future<void> disconnect() async {}

  @override
  void send(Map<String, dynamic> payload) {
    sentPayloads.add(Map<String, dynamic>.from(payload));
  }

  void emit(AppEvent event) {
    _controller.add(event);
  }

  @override
  Future<void> dispose() async {
    await _controller.close();
  }
}
