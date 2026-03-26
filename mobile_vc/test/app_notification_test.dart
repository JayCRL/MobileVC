import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:mobile_vc/app/app.dart';
import 'package:mobile_vc/app/local_notification_service.dart';
import 'package:mobile_vc/data/models/events.dart';
import 'package:mobile_vc/data/models/runtime_meta.dart';
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

  testWidgets('前台时不发送系统通知', (tester) async {
    final service = _FakeMobileVcWsService();
    final controller = SessionController(service: service);
    final notifications = _FakeLocalNotificationService();
    await controller.initialize();
    addTearDown(controller.disposeController);

    await tester.pumpWidget(
      MobileVcApp(
        controller: controller,
        notificationService: notifications,
      ),
    );
    await controller.connect();
    service.emit(
      PromptRequestEvent(
        timestamp: _timestamp,
        sessionId: 'session-1',
        runtimeMeta: const RuntimeMeta(command: 'claude', contextId: 'prompt-1'),
        raw: const {'type': 'prompt_request', 'msg': '请补充上下文'},
        message: '请补充上下文',
        options: const [],
      ),
    );
    await _flushEvents();
    await tester.pump();

    expect(notifications.payloads, isEmpty);
  });

  testWidgets('后台时发送系统通知', (tester) async {
    final service = _FakeMobileVcWsService();
    final controller = SessionController(service: service);
    final notifications = _FakeLocalNotificationService();
    await controller.initialize();
    addTearDown(controller.disposeController);

    await tester.pumpWidget(
      MobileVcApp(
        controller: controller,
        notificationService: notifications,
      ),
    );
    await controller.connect();
    tester.binding.handleAppLifecycleStateChanged(AppLifecycleState.paused);
    await tester.pump();

    service.emit(
      PromptRequestEvent(
        timestamp: _timestamp,
        sessionId: 'session-1',
        runtimeMeta: const RuntimeMeta(command: 'claude', contextId: 'prompt-1'),
        raw: const {'type': 'prompt_request', 'msg': '请补充上下文'},
        message: '请补充上下文',
        options: const [],
      ),
    );
    await _flushEvents();
    await tester.pump();

    expect(notifications.payloads, hasLength(1));
    expect(notifications.payloads.single.body, 'Claude 正在等待你的回复');
  });
}

class _FakeLocalNotificationService implements LocalNotificationService {
  final List<NotificationPayload> payloads = [];

  @override
  Future<void> initialize() async {}

  @override
  Future<void> showActionNeededNotification(NotificationPayload payload) async {
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
