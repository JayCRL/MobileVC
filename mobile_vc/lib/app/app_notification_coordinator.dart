import 'package:flutter/widgets.dart';

import '../features/session/session_controller.dart';
import 'local_notification_service.dart';

class AppNotificationCoordinator {
  AppNotificationCoordinator({
    required SessionController controller,
    required LocalNotificationService notificationService,
  })  : _controller = controller,
        _notificationService = notificationService;

  final SessionController _controller;
  final LocalNotificationService _notificationService;

  AppLifecycleState _lifecycleState = AppLifecycleState.resumed;
  int _lastHandledSignalId = 0;

  bool get isAppActive =>
      _lifecycleState == AppLifecycleState.resumed ||
      _lifecycleState == AppLifecycleState.inactive;

  void handleLifecycleStateChanged(AppLifecycleState state) {
    _lifecycleState = state;
    _drainNotificationSignal();
  }

  Future<void> initialize() async {
    await _notificationService.initialize();
    _drainNotificationSignal();
  }

  void handleControllerChanged() {
    _drainNotificationSignal();
  }

  Future<void> _drainNotificationSignal() async {
    final signal = _controller.actionNeededSignal;
    if (signal == null || signal.id == _lastHandledSignalId) {
      return;
    }
    _lastHandledSignalId = signal.id;
    if (isAppActive) {
      return;
    }
    await _notificationService.showActionNeededNotification(
      NotificationPayload(
        title: 'MobileVC',
        body: signal.message,
      ),
    );
  }
}
