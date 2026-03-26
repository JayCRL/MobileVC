import 'dart:async';

import 'package:flutter_test/flutter_test.dart';
import 'package:shared_preferences/shared_preferences.dart';
import 'package:mobile_vc/core/config/app_config.dart';
import 'package:mobile_vc/data/models/events.dart';
import 'package:mobile_vc/data/models/runtime_meta.dart';
import 'package:mobile_vc/data/models/session_models.dart';
import 'package:mobile_vc/data/services/mobilevc_ws_service.dart';
import 'package:mobile_vc/features/session/session_controller.dart';

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  setUp(() {
    SharedPreferences.setMockInitialValues({});
  });

  test('主界面顶部上下文胶囊已完全移除', () async {
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
}

final _timestamp = DateTime(2026, 1, 1);

Future<void> _flushEvents() async {
  await Future<void>.delayed(const Duration(milliseconds: 1));
  await Future<void>.delayed(const Duration(milliseconds: 1));
}

class _FakeMobileVcWsService extends MobileVcWsService {
  _FakeMobileVcWsService() : super();

  final StreamController<AppEvent> _controller =
      StreamController<AppEvent>.broadcast();

  @override
  Stream<AppEvent> get events => _controller.stream;

  @override
  Future<void> connect(String url) async {}

  @override
  Future<void> disconnect() async {}

  @override
  void send(Map<String, dynamic> payload) {}

  void emit(AppEvent event) {
    _controller.add(event);
  }

  @override
  Future<void> dispose() async {
    await _controller.close();
  }
}
