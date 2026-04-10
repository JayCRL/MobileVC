import 'dart:async';
import 'dart:convert';
import 'dart:io';

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
        events.whereType<ErrorEvent>().where((e) => e.code == 'ws_stream_error'),
        isEmpty,
      );
    });
  });

  group('SessionController action needed signal', () {
    test('ws 断开时会退出会话切换中状态', () async {
      final service = _FakeMobileVcWsService();
      final controller = SessionController(service: service);
      await controller.initialize();
      addTearDown(controller.disposeController);

      await controller.connect();
      controller.createSession();

      expect(controller.isLoadingSession, isTrue);
      expect(service.sentPayloads, isNotEmpty);
      expect(service.sentPayloads.last['action'], 'session_create');

      service.emit(
        ErrorEvent(
          timestamp: _timestamp,
          sessionId: '',
          runtimeMeta: const RuntimeMeta(),
          raw: const {'type': 'error', 'code': 'ws_closed', 'msg': 'WebSocket 连接已断开'},
          code: 'ws_closed',
          message: 'WebSocket 连接已断开',
        ),
      );
      await _flushEvents();

      expect(controller.isLoadingSession, isFalse);
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
      expect(service.sentPayloads.last['action'], 'exec');
      expect(controller.timeline.any((item) => item.body.contains('请先在上方完成授权')), isFalse);
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

      expect(controller.shouldShowPermissionChoices, isFalse);
      expect(controller.pendingPrompt?.message, 'AI 会话已就绪，可继续输入');
      expect(controller.pendingInteraction, isNull);
      final signal = _expectSignal(controller, ActionNeededType.reply);
      expect(signal.message, 'AI 助手正在等待你的回复');
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

      await controller.saveConfig(const AppConfig(host: '192.168.0.2'));
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
      expect(service.sentPayloads[0]['cmd'], 'claude --model sonnet');
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
      expect(service.sentPayloads[0]['action'], 'exec');
      expect(service.sentPayloads[0]['cmd'], 'claude --model opus');
    });

    test('Claude 启动时会忽略残留的 Codex 模型配置并回退到 sonnet', () async {
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
      expect(service.sentPayloads[0]['action'], 'exec');
      expect(service.sentPayloads[0]['cmd'], 'claude --model sonnet');
      expect(controller.selectedAiModel, 'sonnet');
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

      expect(service.sentPayloads, hasLength(2));
      expect(service.sentPayloads[0]['action'], 'exec');
      expect(service.sentPayloads[0]['cmd'], 'claude --model sonnet');
      expect(service.sentPayloads[1]['action'], 'input');
      expect(service.sentPayloads[1]['data'], '请帮我总结当前问题\n');
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
      expect(service.sentPayloads[0]['action'], 'exec');
      expect(
        service.sentPayloads[0]['cmd'],
        'codex -m gpt-5-codex --config model_reasoning_effort=high',
      );
    });

    test('Codex 启动时会忽略残留的 Claude 模型配置并回退到默认模型', () async {
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
      expect(service.sentPayloads[0]['action'], 'exec');
      expect(
        service.sentPayloads[0]['cmd'],
        'codex -m gpt-5-codex --config model_reasoning_effort=high',
      );
      expect(controller.selectedAiModel, 'gpt-5-codex');
    });

    test('输入 codex 后会立刻切到 Codex 模式，不再停留在 shell', () async {
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

      expect(controller.shouldShowClaudeMode, isTrue);
      expect(controller.commandBarEngine, 'codex');
      expect(service.sentPayloads, hasLength(1));
      expect(
        service.sentPayloads[0]['cmd'],
        'codex -m gpt-5-codex --config model_reasoning_effort=medium',
      );
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

      expect(claudeService.sentPayloads[0]['cmd'], 'claude --model opus');

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

      expect(
        codexService.sentPayloads[0]['cmd'],
        'codex -m gpt-5.4 --config model_reasoning_effort=high',
      );
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

      expect(
        service.sentPayloads[0]['cmd'],
        'codex -m gpt-5.4 --config model_reasoning_effort=xhigh',
      );
      expect(controller.commandBarModelSummary, 'GPT-5.4 · XHIGH');
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
      expect(service.sentPayloads[0]['action'], 'input');
      expect(service.sentPayloads[0]['data'], '继续处理这个问题\n');
    });

    test('Claude 空启动后首条正文不会被 busy guard 拦截', () async {
      final service = _FakeMobileVcWsService();
      final controller = SessionController(service: service);
      await controller.initialize();
      addTearDown(controller.disposeController);

      await controller.connect();
      service.sentPayloads.clear();

      controller.sendInputText('claude');
      expect(service.sentPayloads, hasLength(1));
      expect(service.sentPayloads[0]['action'], 'exec');

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

      expect(controller.isSessionBusy, isFalse);
      expect(controller.activityVisible, isFalse);
      expect(controller.activityBannerVisible, isTrue);
      expect(controller.activityBannerAnimated, isFalse);
      expect(controller.activityBannerShowsElapsed, isFalse);
      expect(controller.activityBannerTitle, '待输入');
      expect(controller.activityBannerDetail, 'Claude 已启动，请继续输入');

      controller.sendInputText('继续处理');

      expect(service.sentPayloads, hasLength(2));
      expect(service.sentPayloads[1]['action'], 'input');
      expect(service.sentPayloads[1]['data'], '继续处理\n');
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

    test('continueWithCurrentFile 在 Claude 会话中只走 input continuation', () async {
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
      expect(service.sentPayloads[0]['action'], 'input');
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
          runtimeMeta:
              const RuntimeMeta(command: 'claude', executionId: 'exec-1'),
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

    test('收到 Claude 最终回复时即使没有后续 idle 事件也会立即退出运行态', () async {
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

      expect(controller.activityVisible, isFalse);
      expect(controller.isSessionBusy, isFalse);
      expect(controller.canStopCurrentRun, isFalse);
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

    test('仅有 RUNNING session state 时也保持 busy，避免过早显示空闲', () async {
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

      expect(controller.isSessionBusy, isFalse);
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

    test('运行中点击 stop 后会立即退出 Claude 待输入占位态', () async {
      final service = _FakeMobileVcWsService();
      final controller = SessionController(service: service);
      await controller.initialize();
      addTearDown(controller.disposeController);

      await controller.connect();
      service.sentPayloads.clear();

      controller.sendInputText('claude');
      expect(service.sentPayloads, hasLength(1));
      expect(controller.activityBannerVisible, isTrue);
      expect(controller.activityBannerTitle, '待输入');

      service.emit(
        SessionStateEvent(
          timestamp: _timestamp,
          sessionId: 'session-1',
          runtimeMeta:
              const RuntimeMeta(command: 'claude', executionId: 'exec-stop-claude'),
          raw: const {'type': 'session_state'},
          state: 'RUNNING',
          message: 'claude running',
        ),
      );
      await _flushEvents();

      expect(controller.canStopCurrentRun, isTrue);

      controller.stopCurrentRun();

      expect(service.sentPayloads.last['action'], 'stop');
      expect(controller.activityBannerTitle, isNot('待输入'));
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

      expect(controller.shouldShowReviewChoices, isFalse);
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
      expect(service.sentPayloads.last['action'], 'input');
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

    test('加载可恢复历史会话后，普通输入会直接走 Claude continuation', () async {
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
      expect(service.sentPayloads.single['action'], 'input');
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
      expect(controller.agentState?.state, 'THINKING');
      expect(controller.agentState?.awaitInput, isFalse);
      expect(controller.agentState?.command, 'codex');
      expect(controller.agentPhaseLabel, '思考中');
      expect(controller.activityVisible, isTrue);
    });

    test('新建会话会清空旧 continuation 状态，首条 codex 输入重新走 exec', () async {
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
      expect(refreshActions, contains('session_context_get'));
      expect(refreshActions, contains('permission_rule_list'));

      service.sentPayloads.clear();
      controller.sendInputText('codex');

      expect(service.sentPayloads, hasLength(1));
      expect(service.sentPayloads.single['action'], 'exec');
      expect(
        (service.sentPayloads.single['cmd'] ?? '').toString(),
        startsWith('codex'),
      );
    });
  });

  group('SessionController auto session binding', () {
    test('connect 会主动请求当前目录 session_list，用于同步可恢复会话', () async {
      final service = _FakeMobileVcWsService();
      final controller = SessionController(service: service);
      await controller.initialize();
      addTearDown(controller.disposeController);

      await controller.connect();

      final sessionListRequest = service.sentPayloads.firstWhere(
        (item) => item['action'] == 'session_list',
      );
      expect(sessionListRequest['cwd'], controller.effectiveCwd);
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
      expect(
        service.sentPayloads.where((item) => item['action'] == 'session_context_get'),
        isNotEmpty,
      );
      expect(
        service.sentPayloads.where((item) => item['action'] == 'review_state_get'),
        isNotEmpty,
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

      expect(controller.timeline, hasLength(1));
      expect(controller.timeline.single.kind, 'user');
      expect(controller.timeline.single.body, '修一下登录页按钮间距');
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
      expect(controller.displayPermissionMode, 'acceptEdits');
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
      expect(controller.runtimeProcessListLoading, isTrue);
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

    test('saveGeneratedSkill 在 Claude 会话中走 input continuation', () async {
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
      expect(service.sentPayloads[0]['action'], 'input');
      expect(service.sentPayloads[0]['command'], 'claude');
      expect(service.sentPayloads[0]['targetType'], 'skill');
      expect(service.sentPayloads[0]['resultView'], 'skill-catalog');
      expect(
          (service.sentPayloads[0]['data'] as String)
              .contains('生成一个总结 diff 的 skill'),
          isTrue);
    });

    test('saveGeneratedSkill 首次发起时仍走 Claude exec 编排链', () async {
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

    test('reviseMemoryWithClaude 在 Claude 会话中走 input continuation', () async {
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
      expect(service.sentPayloads[0]['action'], 'input');
      expect(service.sentPayloads[0]['command'], 'claude');
      expect(service.sentPayloads[0]['targetType'], 'memory');
      expect(service.sentPayloads[0]['resultView'], 'memory-catalog');
      expect(
          (service.sentPayloads[0]['data'] as String)
              .contains('改成强调 iOS 风格 UI 偏好'),
          isTrue);
    });

    test('reviseMemoryWithClaude 首次发起时仍走 Claude exec 编排链', () async {
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

    test('仅凭 acceptEdits 不会把新 diff 标记为已自动接受', () async {
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

      expect(controller.isAutoAcceptMode, isTrue);
      expect(controller.pendingDiffs, hasLength(1));
      expect(controller.pendingDiffs.single.reviewStatus, 'pending');
      expect(controller.pendingDiffs.single.pendingReview, isTrue);
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
          runtimeMeta: const RuntimeMeta(command: 'claude'),
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
  int connectCalls = 0;
  int disconnectCalls = 0;

  @override
  Stream<AppEvent> get events => _controller.stream;

  @override
  Future<void> connect(String url) async {
    connectCalls++;
  }

  @override
  Future<void> disconnect() async {
    disconnectCalls++;
  }

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
