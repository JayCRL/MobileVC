import 'dart:async';

import 'package:flutter/material.dart';

import '../features/session/session_controller.dart';
import '../features/session/session_home_page.dart';
import 'app_notification_coordinator.dart';
import 'background_keep_alive_service.dart';
import 'local_notification_service.dart';
import 'theme.dart';

class MobileVcApp extends StatefulWidget {
  const MobileVcApp({
    super.key,
    SessionController? controller,
    LocalNotificationService? notificationService,
  })  : _controller = controller,
        _notificationService = notificationService;

  final SessionController? _controller;
  final LocalNotificationService? _notificationService;

  @override
  State<MobileVcApp> createState() => _MobileVcAppState();
}

class _MobileVcAppState extends State<MobileVcApp> with WidgetsBindingObserver {
  late final SessionController _controller;
  late final AppNotificationCoordinator _notificationCoordinator;
  late final BackgroundKeepAliveService _backgroundKeepAliveService;

  @override
  void initState() {
    super.initState();
    WidgetsBinding.instance.addObserver(this);
    _controller = widget._controller ?? SessionController();
    _notificationCoordinator = AppNotificationCoordinator(
      controller: _controller,
      notificationService:
          widget._notificationService ?? FlutterLocalNotificationService(),
    );
    _backgroundKeepAliveService = BackgroundKeepAliveService();
    _controller.addListener(_handleControllerChanged);
    _startApp();
  }

  Future<void> _startApp() async {
    debugPrint('[startup] app init start');
    try {
      debugPrint('[startup] controller init start');
      await _controller.initialize();
      debugPrint('[startup] controller init end');
    } catch (error, stack) {
      debugPrint('[startup] controller init failed: $error');
      debugPrintStack(
        stackTrace: stack,
        label: '[startup] controller init stack',
      );
    }

    WidgetsBinding.instance.addPostFrameCallback((_) {
      unawaited(_initializeNotifications());
    });

    debugPrint('[startup] app init end');
  }

  Future<void> _initializeNotifications() async {
    try {
      await _notificationCoordinator.initialize();
      await _syncBackgroundKeepAlive();
    } catch (error, stack) {
      debugPrint('[startup] notification bootstrap failed: $error');
      debugPrintStack(
        stackTrace: stack,
        label: '[startup] notification bootstrap stack',
      );
    }
  }

  @override
  void didChangeAppLifecycleState(AppLifecycleState state) {
    _controller
        .handleForegroundStateChanged(state == AppLifecycleState.resumed);
    if (state == AppLifecycleState.resumed) {
      _controller.resumeConnectionIfNeeded();
    } else {
      _controller.pauseConnectionRecovery();
    }
    _notificationCoordinator.handleLifecycleStateChanged(state);
    unawaited(_syncBackgroundKeepAlive());
  }

  @override
  void dispose() {
    WidgetsBinding.instance.removeObserver(this);
    _controller.removeListener(_handleControllerChanged);
    unawaited(_backgroundKeepAliveService.dispose());
    if (widget._controller == null) {
      _controller.disposeController();
    }
    super.dispose();
  }

  void _handleControllerChanged() {
    _notificationCoordinator.handleControllerChanged();
    unawaited(_syncBackgroundKeepAlive());
  }

  Future<void> _syncBackgroundKeepAlive() async {
    await _backgroundKeepAliveService.setActive(
      !_notificationCoordinator.isAppForeground &&
          _controller.connected &&
          _controller.isSessionBusy,
    );
  }

  @override
  Widget build(BuildContext context) {
    return AnimatedBuilder(
      animation: _controller,
      builder: (context, _) {
        return MaterialApp(
          title: 'MobileVC',
          debugShowCheckedModeBanner: false,
          theme: AppTheme.light(),
          home: SessionHomePage(controller: _controller),
        );
      },
    );
  }
}
