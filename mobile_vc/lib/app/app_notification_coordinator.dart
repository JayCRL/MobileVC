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
  int _lastHandledNotificationId = 0;
  bool _initialized = false;

  bool get isAppForeground => _lifecycleState == AppLifecycleState.resumed;

  bool get canShowBackgroundNotification =>
      _lifecycleState != AppLifecycleState.resumed &&
      _lifecycleState != AppLifecycleState.inactive;

  void handleLifecycleStateChanged(AppLifecycleState state) {
    _lifecycleState = state;
    _drainNotificationSignal();
  }

  Future<void> initialize() async {
    debugPrint('[startup] notification coordinator init start');
    try {
      await _notificationService.initialize();
      _initialized = _notificationService.isAvailable;
      debugPrint(
        '[startup] notification coordinator init end available=$_initialized',
      );
    } catch (error, stack) {
      _initialized = false;
      debugPrint('[startup] notification coordinator init failed: $error');
      debugPrintStack(
        stackTrace: stack,
        label: '[startup] notification coordinator init stack',
      );
    }
    await _drainNotificationSignal();
  }

  void handleControllerChanged() {
    _drainNotificationSignal();
  }

  Future<void> _drainNotificationSignal() async {
    await _drainActionNeededSignal();
    await _drainTimelineNotificationSignal();
  }

  Future<void> _drainActionNeededSignal() async {
    final signal = _controller.actionNeededSignal;
    if (signal == null || signal.id == _lastHandledSignalId) {
      return;
    }
    if (isAppForeground) {
      _lastHandledSignalId = signal.id;
      return;
    }
    if (!_initialized || !canShowBackgroundNotification) {
      return;
    }
    try {
      await _notificationService.showNotification(
        NotificationPayload(
          title: 'MobileVC',
          body: signal.message,
        ),
      );
      _lastHandledSignalId = signal.id;
    } catch (error, stack) {
      debugPrint('[startup] notification drain failed: $error');
      debugPrintStack(
        stackTrace: stack,
        label: '[startup] notification drain stack',
      );
    }
  }

  Future<void> _drainTimelineNotificationSignal() async {
    final signal = _controller.notificationSignal;
    if (signal == null || signal.id == _lastHandledNotificationId) {
      return;
    }
    if (isAppForeground) {
      _lastHandledNotificationId = signal.id;
      return;
    }
    if (!_initialized || !canShowBackgroundNotification) {
      return;
    }
    try {
      await _notificationService.showNotification(
        NotificationPayload(
          title: signal.title,
          body: signal.body,
        ),
      );
      _lastHandledNotificationId = signal.id;
    } catch (error, stack) {
      debugPrint('[startup] notification drain failed: $error');
      debugPrintStack(
        stackTrace: stack,
        label: '[startup] notification drain stack',
      );
    }
  }
}
