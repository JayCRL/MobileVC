import 'dart:async';

import 'package:flutter/widgets.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:mobile_vc/app/app_notification_coordinator.dart';
import 'package:mobile_vc/app/local_notification_service.dart';
import 'package:mobile_vc/data/models/events.dart';
import 'package:mobile_vc/data/models/runtime_meta.dart';
import 'package:mobile_vc/data/models/session_models.dart';
import 'package:mobile_vc/data/services/mobilevc_ws_service.dart';
import 'package:mobile_vc/features/session/session_controller.dart';
import 'package:shared_preferences/shared_preferences.dart';

Future<void> _flushEvents() async {
  await Future<void>.delayed(const Duration(milliseconds: 1));
  await Future<void>.delayed(const Duration(milliseconds: 1));
}

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  setUp(() {
    SharedPreferences.setMockInitialValues({});
  });

  test('前台收到回复时不发送系统通知', () async {
    final service = _FakeMobileVcWsService();
    final controller = SessionController(service: service);
    final notifications = _FakeLocalNotificationService();
    final coordinator = AppNotificationCoordinator(
      controller: controller,
      notificationService: notifications,
    );
    await controller.initialize();
    await controller.connect();
    _bindSession(service);
    await _flushEvents();
    await coordinator.initialize();

    service.emit(
      LogEvent(
        timestamp: _timestamp,
        sessionId: 'session-1',
        runtimeMeta: const RuntimeMeta(command: 'codex', engine: 'codex'),
        raw: const {'type': 'log'},
        message: '问题已经修好了，我把原因也一起写在下面。',
        stream: 'stdout',
      ),
    );
    await _flushEvents();

    expect(notifications.payloads, isEmpty);

    await controller.disposeController();
  });

  test('后台收到回复时发送系统通知', () async {
    final service = _FakeMobileVcWsService();
    final controller = SessionController(service: service);
    final notifications = _FakeLocalNotificationService();
    final coordinator = AppNotificationCoordinator(
      controller: controller,
      notificationService: notifications,
    );
    await controller.initialize();
    await controller.connect();
    _bindSession(service);
    await _flushEvents();
    await coordinator.initialize();
    coordinator.handleLifecycleStateChanged(AppLifecycleState.paused);

    service.emit(
      LogEvent(
        timestamp: _timestamp,
        sessionId: 'session-1',
        runtimeMeta: const RuntimeMeta(command: 'codex', engine: 'codex'),
        raw: const {'type': 'log'},
        message: '问题已经修好了，我把原因也一起写在下面。',
        stream: 'stdout',
      ),
    );
    await _flushEvents();
    coordinator.handleControllerChanged();
    await _flushEvents();

    expect(notifications.payloads, hasLength(1));
    expect(notifications.payloads.single.body, '问题已经修好了，我把原因也一起写在下面。');

    await controller.disposeController();
  });

  test('后台收到错误时发送系统通知', () async {
    final service = _FakeMobileVcWsService();
    final controller = SessionController(service: service);
    final notifications = _FakeLocalNotificationService();
    final coordinator = AppNotificationCoordinator(
      controller: controller,
      notificationService: notifications,
    );
    await controller.initialize();
    await controller.connect();
    await coordinator.initialize();
    coordinator.handleLifecycleStateChanged(AppLifecycleState.paused);

    service.emit(
      ErrorEvent(
        timestamp: _timestamp,
        sessionId: 'session-1',
        runtimeMeta: const RuntimeMeta(command: 'claude', engine: 'claude'),
        raw: const {'type': 'error'},
        message: 'Failed to authenticate',
        stack: '',
      ),
    );
    await _flushEvents();
    coordinator.handleControllerChanged();
    await _flushEvents();

    expect(notifications.payloads, hasLength(1));
    expect(notifications.payloads.single.body, 'Failed to authenticate');

    await controller.disposeController();
  });

  test('inactive 过渡期收到权限请求会在进入后台后补发通知', () async {
    final service = _FakeMobileVcWsService();
    final controller = SessionController(service: service);
    final notifications = _FakeLocalNotificationService();
    final coordinator = AppNotificationCoordinator(
      controller: controller,
      notificationService: notifications,
    );
    await controller.initialize();
    await controller.connect();
    await coordinator.initialize();
    coordinator.handleLifecycleStateChanged(AppLifecycleState.inactive);

    service.emit(
      RuntimePhaseEvent(
        timestamp: _timestamp,
        sessionId: 'session-1',
        runtimeMeta: const RuntimeMeta(command: 'codex', engine: 'codex'),
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
          command: 'codex',
          engine: 'codex',
          contextId: 'perm-1',
          targetPath: '/workspace/README.md',
        ),
        raw: const {
          'type': 'interaction_request',
          'kind': 'permission',
          'message': 'Allow edit README.md?',
        },
        kind: 'permission',
        title: 'Permission required',
        message: 'Allow edit README.md?',
        options: const [PromptOption(value: 'approve')],
      ),
    );
    await _flushEvents();
    coordinator.handleControllerChanged();
    await _flushEvents();

    expect(notifications.payloads, isEmpty);

    coordinator.handleLifecycleStateChanged(AppLifecycleState.paused);
    await _flushEvents();

    expect(notifications.payloads, hasLength(1));
    expect(notifications.payloads.single.body, 'AI 助手需要你确认权限');

    await controller.disposeController();
  });

  test('inactive 过渡期收到回复会在进入后台后补发通知', () async {
    final service = _FakeMobileVcWsService();
    final controller = SessionController(service: service);
    final notifications = _FakeLocalNotificationService();
    final coordinator = AppNotificationCoordinator(
      controller: controller,
      notificationService: notifications,
    );
    await controller.initialize();
    await controller.connect();
    _bindSession(service);
    await _flushEvents();
    await coordinator.initialize();
    coordinator.handleLifecycleStateChanged(AppLifecycleState.inactive);

    service.emit(
      LogEvent(
        timestamp: _timestamp,
        sessionId: 'session-1',
        runtimeMeta: const RuntimeMeta(command: 'codex', engine: 'codex'),
        raw: const {'type': 'log'},
        message: '后台任务已经执行完成。',
        stream: 'stdout',
      ),
    );
    await _flushEvents();
    coordinator.handleControllerChanged();
    await _flushEvents();

    expect(notifications.payloads, isEmpty);

    coordinator.handleLifecycleStateChanged(AppLifecycleState.paused);
    await _flushEvents();

    expect(notifications.payloads, hasLength(1));
    expect(notifications.payloads.single.body, '后台任务已经执行完成。');

    await controller.disposeController();
  });

  test('inactive 过渡期收到分片回复会合并后再补发一条通知', () async {
    final service = _FakeMobileVcWsService();
    final controller = SessionController(service: service);
    final notifications = _FakeLocalNotificationService();
    final coordinator = AppNotificationCoordinator(
      controller: controller,
      notificationService: notifications,
    );
    await controller.initialize();
    await controller.connect();
    _bindSession(service);
    await _flushEvents();
    await coordinator.initialize();
    coordinator.handleLifecycleStateChanged(AppLifecycleState.inactive);

    const meta = RuntimeMeta(
      command: 'codex',
      engine: 'codex',
      executionId: 'exec-notify-1',
      contextId: 'turn-notify-1',
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
    coordinator.handleControllerChanged();
    await _flushEvents();

    expect(notifications.payloads, isEmpty);

    coordinator.handleLifecycleStateChanged(AppLifecycleState.paused);
    await _flushEvents();

    expect(notifications.payloads, hasLength(1));
    expect(
      notifications.payloads.single.body,
      '**Code Updates** - Added permission fix. Push completed successfully.',
    );

    await controller.disposeController();
  });
}

class _FakeLocalNotificationService implements LocalNotificationService {
  final List<NotificationPayload> payloads = [];

  @override
  bool get isAvailable => true;

  @override
  Future<void> initialize() async {}

  @override
  Future<void> showNotification(NotificationPayload payload) async {
    payloads.add(payload);
  }
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

final _timestamp = DateTime(2026, 1, 1);

void _bindSession(_FakeMobileVcWsService service) {
  service.emit(
    SessionHistoryEvent(
      timestamp: _timestamp,
      sessionId: 'session-1',
      runtimeMeta: const RuntimeMeta(),
      raw: const {'type': 'session_history'},
      summary: const SessionSummary(id: 'session-1', title: '通知测试'),
    ),
  );
}
