import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:shared_preferences/shared_preferences.dart';
import 'package:mobile_vc/core/config/app_config.dart';
import 'package:mobile_vc/core/relay_e2ee/relay_e2ee_capability.dart';
import 'package:mobile_vc/data/models/events.dart';
import 'package:mobile_vc/data/models/runtime_meta.dart';
import 'package:mobile_vc/data/models/session_models.dart';
import 'package:mobile_vc/data/services/mobilevc_ws_service.dart';
import 'package:mobile_vc/features/chat/chat_timeline.dart';
import 'package:mobile_vc/features/chat/command_input_bar.dart';
import 'package:mobile_vc/features/session/session_controller.dart';
import 'package:mobile_vc/features/session/session_home_page.dart';

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  setUp(() {
    SharedPreferences.setMockInitialValues({});
  });

  testWidgets('键盘弹出只移动输入栏，不整体 resize 时间线', (tester) async {
    await _useTallSurface(tester);
    final service = _FakeMobileVcWsService();
    final controller = SessionController(service: service);

    await controller.connect();
    for (var i = 0; i < 40; i++) {
      controller.pushSystemMessage('markdown', '历史消息 $i');
    }

    await tester.pumpWidget(
      _buildHomeWithInsets(controller, EdgeInsets.zero),
    );
    await _pumpFrames(tester);

    final timelineSizeBefore = tester.getSize(find.byType(ChatTimeline));
    final commandBarSizeBefore = tester.getSize(find.byType(CommandInputBar));
    final timelineBefore = tester.widget<ChatTimeline>(
      find.byType(ChatTimeline),
    );
    expect(
      timelineBefore.bottomPadding,
      greaterThanOrEqualTo(commandBarSizeBefore.height),
    );

    await tester.pumpWidget(
      _buildHomeWithInsets(
        controller,
        const EdgeInsets.only(bottom: 320),
      ),
    );
    await _pumpFrames(tester);

    final timelineSizeAfter = tester.getSize(find.byType(ChatTimeline));
    final commandBarSizeAfter = tester.getSize(find.byType(CommandInputBar));
    final scaffold = tester.widget<Scaffold>(find.byType(Scaffold).first);
    expect(scaffold.resizeToAvoidBottomInset, isFalse);
    expect(scaffold.bottomNavigationBar, isNull);
    expect(timelineSizeAfter, timelineSizeBefore);
    expect(commandBarSizeAfter, commandBarSizeBefore);
    final timelineAfter = tester.widget<ChatTimeline>(
      find.byType(ChatTimeline),
    );
    expect(timelineAfter.bottomPadding, timelineBefore.bottomPadding + 320);

    final keyboardPadding = tester.widget<AnimatedPadding>(
      find.byKey(const ValueKey('command-bar-keyboard-padding')),
    );
    expect(keyboardPadding.padding, const EdgeInsets.only(bottom: 320));

    await tester.pumpWidget(const SizedBox.shrink());
    await controller.disposeController();
  });

  testWidgets('输入栏重建后仍用实际高度作为时间线底部留白', (tester) async {
    await _useTallSurface(tester);
    final service = _FakeMobileVcWsService();
    final controller = SessionController(service: service);

    await controller.connect();
    for (var i = 0; i < 40; i++) {
      controller.pushSystemMessage('markdown', '历史消息 $i');
    }

    await tester.pumpWidget(
      _buildHomeWithInsets(controller, EdgeInsets.zero),
    );
    await _pumpFrames(tester);

    await tester.enterText(
      find.byType(TextField),
      List<String>.filled(6, '长输入内容').join('\n'),
    );
    await _pumpFrames(tester);

    final commandBarSize = tester.getSize(find.byType(CommandInputBar));
    final expandedTimeline = tester.widget<ChatTimeline>(
      find.byType(ChatTimeline),
    );
    expect(
      expandedTimeline.bottomPadding,
      greaterThanOrEqualTo(commandBarSize.height),
    );

    await tester.pumpWidget(const SizedBox.shrink());
    await controller.disposeController();
  });

  test('主界面顶部上下文胶囊已完全移除', () async {
    final service = _FakeMobileVcWsService();
    final controller = SessionController(service: service);
    addTearDown(controller.disposeController);
    await controller.initialize();

    await controller.saveConfig(
      const AppConfig(
        cwd: '/workspace',
        engine: 'claude',
        permissionMode: 'default',
      ),
    );
    await controller.connect();

    service.emit(
      SessionHistoryEvent(
        timestamp: _timestamp,
        sessionId: 'session-1',
        runtimeMeta: const RuntimeMeta(),
        raw: const {'type': 'session_history'},
        summary: const SessionSummary(id: 'session-1', title: '会话'),
        sessionContext: const SessionContext(
          enabledSkillNames: ['review-pr'],
          enabledMemoryIds: ['mem-1'],
        ),
      ),
    );
    service.emit(
      SkillCatalogResultEvent(
        timestamp: _timestamp,
        sessionId: 'session-1',
        runtimeMeta: const RuntimeMeta(),
        raw: const {'type': 'skill_catalog_result'},
        items: const [
          SkillDefinition(
            name: 'review-pr',
            description: 'review skill',
            prompt: 'do review',
            targetType: 'diff',
            resultView: 'review-card',
          ),
        ],
      ),
    );
    service.emit(
      MemoryListResultEvent(
        timestamp: _timestamp,
        sessionId: 'session-1',
        runtimeMeta: const RuntimeMeta(),
        raw: const {'type': 'memory_list_result'},
        items: const [
          MemoryItem(id: 'mem-1', title: '用户偏好', content: '偏爱深色模式'),
        ],
      ),
    );
    await _flushEvents();

    expect(controller.hasCompactContextSelection, isTrue);
    expect(controller.skills.map((item) => item.name), contains('review-pr'));
    expect(controller.memoryItems.map((item) => item.title), contains('用户偏好'));
  });

  testWidgets('Claude 模型面板点击卡片立即显示选中态，应用前不保存', (tester) async {
    await _useTallSurface(tester);
    final service = _FakeMobileVcWsService();
    final controller = SessionController(service: service);

    await controller.saveConfig(
      const AppConfig(
        cwd: '/workspace',
        engine: 'claude',
        permissionMode: 'default',
        claudeModel: 'sonnet',
      ),
    );

    await tester.pumpWidget(
      MaterialApp(
        home: SessionHomePage(controller: controller),
      ),
    );
    await _pumpFrames(tester);

    await _tapCommandBarModel(tester, '模型 · Sonnet');
    await _pumpFrames(tester);

    expect(controller.configuredAiModel, 'sonnet');
    expect(find.byIcon(Icons.check_circle_rounded), findsOneWidget);
    expect(find.text('Sonnet 4.5'), findsAtLeastNWidgets(1));

    await tester.tap(find.text('Sonnet 4.5').first);
    await _pumpFrames(tester);

    expect(controller.configuredAiModel, 'sonnet');
    expect(find.byIcon(Icons.check_circle_rounded), findsOneWidget);
    expect(find.text('应用 CLAUDE-SONNET-4-5'), findsOneWidget);

    await tester.tap(find.text('应用 CLAUDE-SONNET-4-5'));
    await _pumpFrames(tester);

    expect(controller.configuredAiModel, 'claude-sonnet-4-5');

    await controller.disposeController();
  });

  testWidgets('Codex 模型面板正确高亮 Default 并保存显式模型', (tester) async {
    await _useTallSurface(tester);
    final service = _FakeMobileVcWsService();
    final controller = _ModelCatalogSessionController(service: service);

    await controller.saveConfig(
      const AppConfig(
        cwd: '/workspace',
        engine: 'codex',
        permissionMode: 'default',
      ),
    );

    await tester.pumpWidget(
      MaterialApp(
        home: SessionHomePage(controller: controller),
      ),
    );
    await _pumpFrames(tester);

    await _tapCommandBarModel(tester, '模型 · Default · config.toml');
    await _pumpFrames(tester);

    expect(controller.catalogRequestCount, 1);
    expect(find.byIcon(Icons.check_circle_rounded), findsOneWidget);
    expect(find.text('Default'), findsAtLeastNWidgets(1));

    await tester.tap(find.text('gpt-5.5').first);
    await _pumpFrames(tester);

    expect(controller.configuredAiModel, isEmpty);
    expect(find.text('应用 gpt-5.5 · HIGH'), findsOneWidget);

    await tester.tap(find.text('应用 gpt-5.5 · HIGH'));
    await _pumpFrames(tester);

    expect(controller.configuredAiEngine, 'codex');
    expect(controller.configuredAiModel, 'gpt-5.5');
    expect(controller.configuredAiReasoningEffort, 'high');
    expect(controller.commandBarModelSummary, 'gpt-5.5 · HIGH');
    expect(controller.catalogRequestCount, 1);

    await tester.pumpWidget(const SizedBox.shrink());
    await tester.pump();
    await controller.disposeController();
  });

  testWidgets('连接设置里 Claude 使用官方权限模式下拉', (tester) async {
    await _useTallSurface(tester);
    final service = _FakeMobileVcWsService();
    final controller = SessionController(service: service);
    await controller.saveConfig(
      const AppConfig(
        cwd: '/workspace',
        engine: 'claude',
        permissionMode: 'default',
      ),
    );

    await tester.pumpWidget(
      MaterialApp(
        home: SessionHomePage(controller: controller),
      ),
    );
    await _pumpFrames(tester);

    await tester.tap(find.byIcon(Icons.settings_outlined));
    await _pumpFrames(tester);
    await tester.ensureVisible(find.text('Claude 权限'));
    await tester.pump();

    final dropdown = find.byKey(
      const ValueKey('connection-config-claude-permission-mode'),
    );
    expect(dropdown, findsOneWidget);
    await tester.tap(dropdown);
    await tester.pumpAndSettle();

    expect(find.text('默认权限'), findsAtLeastNWidgets(1));
    expect(find.text('自动模式'), findsOneWidget);
    expect(find.text('完全访问权限'), findsOneWidget);
    expect(find.text('跳过审批'), findsNothing);
    expect(find.text('自动审查'), findsNothing);
    expect(find.text('自定义(config.toml)'), findsNothing);

    await controller.disposeController();
  });

  testWidgets('连接设置里 Codex 沙箱范围和审批策略分开显示', (tester) async {
    await _useTallSurface(tester);
    final service = _FakeMobileVcWsService();
    final controller = SessionController(service: service);
    await controller.saveConfig(
      const AppConfig(
        cwd: '/workspace',
        engine: 'codex',
        codexSandboxMode: 'workspace-write',
        permissionMode: 'default',
      ),
    );

    await tester.pumpWidget(
      MaterialApp(
        home: SessionHomePage(controller: controller),
      ),
    );
    await _pumpFrames(tester);

    await tester.tap(find.byIcon(Icons.settings_outlined));
    await _pumpFrames(tester);
    await tester.ensureVisible(find.text('Codex 沙箱范围'));
    await tester.pump();

    expect(find.text('Codex 沙箱范围'), findsOneWidget);
    expect(find.text('Codex 审批策略'), findsOneWidget);
    expect(find.text('Codex Sandbox'), findsNothing);
    expect(find.text('Codex 权限'), findsNothing);
    expect(find.text('请求目标'), findsNothing);

    await tester.tap(find.text('工作区写入'));
    await tester.pumpAndSettle();
    expect(find.text('关闭沙箱'), findsOneWidget);
    await tester.tap(find.text('关闭沙箱').last);
    await tester.pumpAndSettle();

    final approvalDropdown = find.byKey(
      const ValueKey('connection-config-codex-permission-mode'),
    );
    await tester.tap(approvalDropdown);
    await tester.pumpAndSettle();

    expect(find.text('跳过审批'), findsOneWidget);
    expect(find.text('完全访问权限'), findsNothing);
    expect(find.text('自动审查'), findsOneWidget);
    expect(find.text('自定义(config.toml)'), findsOneWidget);

    await controller.disposeController();
  });

  testWidgets('Relay 连接中禁用重复点击并显示进度', (tester) async {
    await _useTallSurface(tester);
    final service = _BlockingRelayWsService();
    final controller = SessionController(service: service);
    await controller.saveConfig(const AppConfig(
      connectionMode: 'relay',
      relayUrl: 'wss://relay.example.test',
      relaySessionId: 'rs_test',
      relayPairingSecret: 'pair_secret',
      relayPairingExpiresAt: 4102444800,
    ));

    await tester.pumpWidget(
      MaterialApp(
        home: SessionHomePage(controller: controller),
      ),
    );
    await _pumpFrames(tester);

    await tester.tap(find.byIcon(Icons.settings_outlined));
    await _pumpFrames(tester);
    await tester.ensureVisible(find.text('连接'));
    await tester.pump();
    await tester.tap(find.text('连接'));
    await tester.pump();
    await tester.ensureVisible(find.text('连接中'));
    await tester.pump();
    await tester.tap(find.text('连接中'));
    await tester.pump();

    expect(find.text('连接中'), findsOneWidget);
    expect(find.textContaining('一次性使用'), findsOneWidget);
    expect(service.connectRelayCalls, 1);

    service.fail(const RelayPairingException(
      'e2ee_handshake_failed',
      'Relay E2EE 握手失败',
    ));
    await _pumpFrames(tester);

    expect(find.text('连接'), findsOneWidget);
    expect(find.textContaining('Relay E2EE 握手失败'), findsAtLeastNWidgets(1));

    await controller.disposeController();
  });

  testWidgets('顶部连接路径使用紧凑标识避免挤压标题', (tester) async {
    final service = _FakeMobileVcWsService();
    final controller = SessionController(service: service);
    try {
      await controller.saveConfig(const AppConfig(
        connectionMode: 'relay',
        relayUrl: 'wss://relay.example.test',
        relaySessionId: 'rs_test',
        relayPairingSecret: 'pair_secret',
        relayPairingExpiresAt: 4102444800,
      ));
      await controller.connect();

      await tester.pumpWidget(
        MaterialApp(
          home: SessionHomePage(controller: controller),
        ),
      );
      await _pumpFrames(tester);

      expect(
        find.byKey(const ValueKey('connection-transport-label')),
        findsOneWidget,
      );
      expect(find.text('R'), findsOneWidget);
      expect(find.text('Relay'), findsNothing);
    } finally {
      await controller.disposeController();
    }
  });
}

final _timestamp = DateTime(2026, 1, 1);

Future<void> _flushEvents() async {
  await Future<void>.delayed(const Duration(milliseconds: 1));
  await Future<void>.delayed(const Duration(milliseconds: 1));
}

Future<void> _useTallSurface(WidgetTester tester) async {
  await tester.binding.setSurfaceSize(const Size(900, 1100));
  addTearDown(() => tester.binding.setSurfaceSize(null));
}

Future<void> _tapCommandBarModel(WidgetTester tester, String label) async {
  final buttonFinder = find.byKey(const ValueKey('command-bar-model-button'));
  await tester.ensureVisible(buttonFinder);
  await tester.pump();
  expect(find.descendant(of: buttonFinder, matching: find.text(label)),
      findsOneWidget);
  await tester.tap(buttonFinder.first);
}

Future<void> _pumpFrames(
  WidgetTester tester, [
  int count = 4,
  Duration step = const Duration(milliseconds: 120),
]) async {
  for (var i = 0; i < count; i++) {
    await tester.pump(step);
  }
}

Widget _buildHomeWithInsets(
  SessionController controller,
  EdgeInsets viewInsets,
) {
  return MaterialApp(
    home: MediaQuery(
      data: MediaQueryData(
        size: const Size(900, 1100),
        viewInsets: viewInsets,
      ),
      child: SessionHomePage(controller: controller),
    ),
  );
}

class _FakeMobileVcWsService extends MobileVcWsService {
  _FakeMobileVcWsService() : super();

  final StreamController<AppEvent> _controller =
      StreamController<AppEvent>.broadcast();

  @override
  Stream<AppEvent> get events => _controller.stream;

  @override
  Future<void> connect(String url) async {
    connectedUrls.add(url);
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
  }) async {}

  @override
  Future<void> disconnect() async {}

  @override
  bool send(Map<String, dynamic> payload) {
    sentPayloads.add(Map<String, dynamic>.from(payload));
    return true;
  }

  void emit(AppEvent event) {
    _controller.add(event);
  }

  final List<String> connectedUrls = [];
  final List<Map<String, dynamic>> sentPayloads = [];

  @override
  Future<void> dispose() async {
    await _controller.close();
  }
}

class _BlockingRelayWsService extends _FakeMobileVcWsService {
  final Completer<void> _connectCompleter = Completer<void>();
  int connectRelayCalls = 0;

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
    connectRelayCalls++;
    return _connectCompleter.future;
  }

  void fail(Object error) {
    if (!_connectCompleter.isCompleted) {
      _connectCompleter.completeError(error);
    }
  }
}

class _ModelCatalogSessionController extends SessionController {
  _ModelCatalogSessionController({required super.service});

  int catalogRequestCount = 0;

  @override
  List<CodexModelCatalogEntry> get codexModelCatalog => const [
        CodexModelCatalogEntry(
          model: 'gpt-5.5',
          displayName: 'gpt-5.5',
          description: 'GPT-5.5',
          defaultReasoningEffort: 'high',
          supportedReasoningEfforts: ['medium', 'high'],
          reasoningEffortOptions: [
            CodexReasoningEffortOption(reasoningEffort: 'medium'),
            CodexReasoningEffortOption(reasoningEffort: 'high'),
          ],
          isDefault: true,
        ),
      ];

  @override
  bool get codexModelCatalogLoading => false;

  @override
  String get codexModelCatalogMessage => '';

  @override
  void requestCodexModelCatalog({bool force = false}) {
    catalogRequestCount++;
  }
}
