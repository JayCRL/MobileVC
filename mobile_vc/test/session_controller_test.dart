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

  group('SessionController action needed signal', () {
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
          runtimeMeta:
              const RuntimeMeta(command: 'claude', executionId: 'exec-1'),
          raw: const {'type': 'agent_state'},
          state: 'WAIT_INPUT',
          message: '等待继续输入',
          awaitInput: true,
          command: 'claude',
        ),
      );
      await _flushEvents();

      final signal = _expectSignal(controller, ActionNeededType.continueInput);
      expect(signal.message, 'Claude 需要你继续输入');
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
      expect(signal.message, 'Claude 需要你确认权限');
    });

    test('review prompt 到来时产出审核处理信号', () async {
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
      expect(signal.message, 'Claude 需要你处理代码审核');
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
      expect(signal.message, 'Claude 正在等待你的回复');
    });

    test('permission prompt 仍保留授权状态但允许更新可见提示', () async {
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
          runtimeMeta: const RuntimeMeta(command: 'claude'),
          raw: const {'type': 'prompt_request', 'msg': 'AI 会话已就绪，可继续输入'},
          message: 'AI 会话已就绪，可继续输入',
          options: const [],
        ),
      );
      await _flushEvents();

      expect(controller.shouldShowPermissionChoices, isTrue);
      expect(controller.pendingPrompt?.message, 'AI 会话已就绪，可继续输入');
      expect(controller.pendingInteraction, isNull);
      final signal = _expectSignal(controller, ActionNeededType.permission);
      expect(signal.message, 'Claude 需要你确认权限');
    });

    test('review prompt 不会被通用可继续输入 prompt 覆盖', () async {
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
        PromptRequestEvent(
          timestamp: _timestamp.add(const Duration(seconds: 1)),
          sessionId: 'session-1',
          runtimeMeta: const RuntimeMeta(command: 'claude'),
          raw: const {'type': 'prompt_request', 'msg': 'AI 会话已就绪，可继续输入'},
          message: 'AI 会话已就绪，可继续输入',
          options: const [],
        ),
      );
      await _flushEvents();

      expect(controller.shouldShowReviewChoices, isTrue);
      expect(controller.currentReviewDiff?.path, '/workspace/a.dart');
      final signal = _expectSignal(controller, ActionNeededType.review);
      expect(signal.message, 'Claude 需要你处理代码审核');
    });

    test('普通 prompt 到来时会被后续通用 prompt 覆盖', () async {
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
        PromptRequestEvent(
          timestamp: _timestamp.add(const Duration(seconds: 1)),
          sessionId: 'session-1',
          runtimeMeta: const RuntimeMeta(command: 'claude'),
          raw: const {'type': 'prompt_request', 'msg': 'AI 会话已就绪，可继续输入'},
          message: 'AI 会话已就绪，可继续输入',
          options: const [],
        ),
      );
      await _flushEvents();

      expect(controller.pendingPrompt?.message, 'AI 会话已就绪，可继续输入');
      final signal = _expectSignal(controller, ActionNeededType.reply);
      expect(signal.message, 'Claude 正在等待你的回复');
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
          runtimeMeta:
              const RuntimeMeta(command: 'claude', executionId: 'exec-2'),
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
  });

  group('SessionController Claude turn dispatch', () {
    test('sendInputText 非等待态输入普通文本时仍按 shell 命令执行', () async {
      final service = _FakeMobileVcWsService();
      final controller = SessionController(service: service);
      await controller.initialize();
      addTearDown(controller.disposeController);

      await controller.connect();
      service.sentPayloads.clear();

      controller.sendInputText('pwd');

      expect(service.sentPayloads, hasLength(1));
      expect(service.sentPayloads[0]['action'], 'exec');
      expect(service.sentPayloads[0]['cmd'], 'pwd');
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
      expect(service.sentPayloads[0]['action'], 'exec');
      expect(service.sentPayloads[0]['cmd'], 'claude');
    });

    test('sendInputText 非等待态输入 claude 后跟正文时会启动 Claude 并通过 input 发送正文', () async {
      final service = _FakeMobileVcWsService();
      final controller = SessionController(service: service);
      await controller.initialize();
      addTearDown(controller.disposeController);

      await controller.connect();
      service.sentPayloads.clear();

      controller.sendInputText('claude 请帮我总结当前问题');

      expect(service.sentPayloads, hasLength(2));
      expect(service.sentPayloads[0]['action'], 'exec');
      expect(service.sentPayloads[0]['cmd'], 'claude');
      expect(service.sentPayloads[1]['action'], 'input');
      expect(service.sentPayloads[1]['data'], '请帮我总结当前问题\n');
    });

    test('sendInputText 在 Claude 模式下继续普通文本时走 input 而不是新的 exec', () async {
      final service = _FakeMobileVcWsService();
      final controller = SessionController(service: service);
      await controller.initialize();
      addTearDown(controller.disposeController);

      await controller.connect();
      service.emit(
        AgentStateEvent(
          timestamp: _timestamp,
          sessionId: 'session-1',
          runtimeMeta: const RuntimeMeta(command: 'claude', executionId: 'exec-keep'),
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
          runtimeMeta: const RuntimeMeta(command: 'claude', executionId: 'exec-keep'),
          raw: const {'type': 'session_state'},
          state: 'ACTIVE',
          message: 'command started',
        ),
      );
      await _flushEvents();
      service.sentPayloads.clear();

      controller.sendInputText('继续处理这个问题');

      expect(service.sentPayloads, hasLength(1));
      expect(service.sentPayloads[0]['action'], 'input');
      expect(service.sentPayloads[0]['data'], '继续处理这个问题\n');
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

    test('continueWithCurrentFile 非等待态不会把文件 prompt 裸发给 exec', () async {
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
            path: '/workspace/lib/main.dart',
            content: 'void main() {}',
            isText: true,
          ),
        ),
      );
      await _flushEvents();
      service.sentPayloads.clear();

      controller.continueWithCurrentFile('基于当前文件继续处理');

      expect(service.sentPayloads, hasLength(2));
      expect(service.sentPayloads[0]['action'], 'exec');
      expect(service.sentPayloads[0]['cmd'], 'claude');
      expect(service.sentPayloads[1]['action'], 'input');
      expect(
        (service.sentPayloads[1]['data'] as String).contains('TargetPath: /workspace/lib/main.dart'),
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
          runtimeMeta: const RuntimeMeta(command: 'claude', executionId: 'exec-1'),
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
          runtimeMeta: const RuntimeMeta(command: 'claude', contextId: 'perm-1'),
          raw: const {'type': 'prompt_request', 'msg': 'Allow edit a.dart?'},
          message: 'Allow edit a.dart?',
          options: const [PromptOption(value: 'y'), PromptOption(value: 'n')],
        ),
      );
      await _flushEvents();

      expect(controller.awaitInput, isTrue);
      expect(controller.activityVisible, isFalse);
    });

    test('收到 Claude 回复后不会因残留 RUNNING session state 卡住', () async {
      final service = _FakeMobileVcWsService();
      final controller = SessionController(service: service);
      await controller.initialize();
      addTearDown(controller.disposeController);

      await controller.connect();
      service.emit(
        SessionStateEvent(
          timestamp: _timestamp,
          sessionId: 'session-1',
          runtimeMeta: const RuntimeMeta(command: 'claude', executionId: 'exec-1'),
          raw: const {'type': 'session_state'},
          state: 'RUNNING',
          message: 'claude running',
        ),
      );
      service.emit(
        AgentStateEvent(
          timestamp: _timestamp,
          sessionId: 'session-1',
          runtimeMeta: const RuntimeMeta(command: 'claude', executionId: 'exec-1'),
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
          runtimeMeta: const RuntimeMeta(command: 'claude', executionId: 'exec-1'),
          raw: const {'type': 'log'},
          message: '你好，我是 Claude，由 Anthropic 开发。有什么我可以帮你处理的吗？',
          stream: 'stdout',
        ),
      );
      service.emit(
        AgentStateEvent(
          timestamp: _timestamp.add(const Duration(seconds: 1)),
          sessionId: 'session-1',
          runtimeMeta: const RuntimeMeta(command: 'claude', executionId: 'exec-1'),
          raw: const {'type': 'agent_state'},
          state: 'IDLE',
          message: '完成',
          command: 'claude',
        ),
      );
      await _flushEvents();

      expect(controller.activityVisible, isFalse);
      expect(controller.isSessionBusy, isFalse);
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
          resumeRuntimeMeta: const RuntimeMeta(command: 'claude'),
        ),
      );
      await _flushEvents();

      expect(controller.isLoadingSession, isFalse);
      expect(controller.selectedSessionId, 'session-new');
    });

    test('仅靠恢复态 runtime meta 也能识别 Claude 模式', () async {
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
          resumeRuntimeMeta:
              const RuntimeMeta(command: 'claude --resume session-1'),
        ),
      );
      await _flushEvents();

      expect(controller.inClaudeMode, isTrue);
    });
  });

  group('SessionController auto session binding', () {
    test('连接后收到非空 session 列表时，会自动 create 新会话，不自动 load 历史会话', () async {
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

      expect(service.sentPayloads, hasLength(1));
      expect(service.sentPayloads.single['action'], 'session_create');
      expect(
        service.sentPayloads
            .any((payload) => payload['action'] == 'session_load'),
        isFalse,
      );
    });

    test('连接后收到空 session 列表时，会自动 create session', () async {
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

      expect(service.sentPayloads, hasLength(1));
      expect(service.sentPayloads.single['action'], 'session_create');
    });

    test(
        '手动 loadSession 仍能恢复历史 timeline / diff / session meta / terminal logs',
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
          terminalExecutions: const [
            TerminalExecution(
              executionId: 'exec-1',
              command: 'npm test',
              cwd: '/workspace/app',
              source: 'user',
              sourceLabel: '用户输入',
              stdout: 'exec-1 stdout',
              stderr: 'exec-1 stderr',
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
          ),
        ),
      );
      await _flushEvents();

      expect(controller.selectedSessionId, 'session-history');
      expect(controller.timeline.any((item) => item.body == '旧回复'), isTrue);
      expect(controller.recentDiffs, hasLength(1));
      expect(controller.currentStepSummary, '历史步骤');
      expect(controller.displayPermissionMode, 'acceptEdits');
      expect(controller.terminalExecutions, hasLength(2));
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

    test('saveGeneratedSkill 走 Claude exec 编排链', () async {
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

      expect(service.sentPayloads, hasLength(2));
      expect(service.sentPayloads[0]['action'], 'exec');
      expect(service.sentPayloads[0]['mode'], 'pty');
      expect(service.sentPayloads[0]['targetType'], 'skill');
      expect(service.sentPayloads[0]['resultView'], 'skill-catalog');
      expect(service.sentPayloads[1]['action'], 'input');
      expect(
          (service.sentPayloads[1]['data'] as String)
              .contains('"mobilevcCatalogAuthoring":true'),
          isTrue);
      expect(
          (service.sentPayloads[1]['data'] as String)
              .contains('"kind":"skill"'),
          isTrue);
      expect(
        (service.sentPayloads[1]['data'] as String)
            .contains('生成一个总结 diff 的 skill'),
        isTrue,
      );
    });

    test('reviseMemoryWithClaude 走 Claude exec 编排链', () async {
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

      expect(service.sentPayloads, hasLength(2));
      expect(service.sentPayloads[0]['action'], 'exec');
      expect(service.sentPayloads[0]['targetType'], 'memory');
      expect(service.sentPayloads[0]['resultView'], 'memory-catalog');
      expect(service.sentPayloads[1]['action'], 'input');
      expect(
          (service.sentPayloads[1]['data'] as String)
              .contains('"mobilevcCatalogAuthoring":true'),
          isTrue);
      expect(
          (service.sentPayloads[1]['data'] as String)
              .contains('"kind":"memory"'),
          isTrue);
      expect(
        (service.sentPayloads[1]['data'] as String)
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

    test('后端临时 permission mode 不改写配置，但 displayPermissionMode 会跟随运行态', () async {
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
      expect(controller.displayPermissionMode, 'acceptEdits');

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
      expect(service.sentPayloads.single['permissionMode'], 'acceptEdits');
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
      expect(service.sentPayloads[0]['action'], 'input');
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
          runtimeMeta: const RuntimeMeta(command: 'claude'),
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
      expect(signal.message, 'Claude 需要你完成计划选择');
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
      expect(payload['decision'], contains('"a"'));
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

    test('pendingInteraction permission 场景下输入发送 permission_decision', () async {
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

      expect(service.sentPayloads, hasLength(1));
      final payload = service.sentPayloads.single;
      expect(payload['action'], 'permission_decision');
      expect(payload['decision'], 'approve');
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
