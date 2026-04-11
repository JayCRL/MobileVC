import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';

import 'package:mobile_vc/app/app.dart';
import 'package:mobile_vc/app/push_notification_service.dart';
import 'package:mobile_vc/features/session/session_controller.dart';
import 'package:shared_preferences/shared_preferences.dart';

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  setUp(() {
    SharedPreferences.setMockInitialValues({});
  });

  testWidgets('renders MobileVC shell', (tester) async {
    await tester.pumpWidget(const MobileVcApp());
    await tester.pump();

    expect(find.text('MobileVC'), findsAtLeastNWidgets(1));
    expect(find.byType(EditableText), findsOneWidget);
  });

  testWidgets('registers token delivered during push initialization', (
    tester,
  ) async {
    final controller = SessionController();
    final pushService = _CallbackFirstPushNotificationService();
    addTearDown(controller.disposeController);

    await tester.pumpWidget(
      MobileVcApp(
        controller: controller,
        pushNotificationService: pushService,
      ),
    );
    await tester.pump();
    await tester.pump(const Duration(milliseconds: 10));

    expect(controller.devicePushToken, 'apns-token-from-callback');
  });

  testWidgets('shows APNs registration error from native layer', (tester) async {
    final controller = SessionController();
    final pushService = _FailingPushNotificationService();
    addTearDown(controller.disposeController);

    await tester.pumpWidget(
      MobileVcApp(
        controller: controller,
        pushNotificationService: pushService,
      ),
    );
    await tester.pump();
    await tester.pump(const Duration(milliseconds: 10));

    expect(controller.timeline.any((item) => item.body.contains('APNs registration failed: simulated native error')), isTrue);
  });
}

class _CallbackFirstPushNotificationService implements PushNotificationService {
  void Function(String token)? _tokenRefreshCallback;

  @override
  bool get isAvailable => true;

  @override
  Future<void> initialize() async {
    _tokenRefreshCallback?.call('apns-token-from-callback');
  }

  @override
  Future<String?> getDeviceToken() async => null;

  @override
  void onMessageOpenedApp(void Function(Map<String, dynamic> message) callback) {}

  @override
  void onMessageReceived(void Function(Map<String, dynamic> message) callback) {}

  @override
  void onRegistrationError(void Function(String message) callback) {}

  @override
  void onTokenRefresh(void Function(String token) callback) {
    _tokenRefreshCallback = callback;
  }
}

class _FailingPushNotificationService implements PushNotificationService {
  void Function(String message)? _registrationErrorCallback;

  @override
  bool get isAvailable => true;

  @override
  Future<void> initialize() async {
    _registrationErrorCallback?.call('APNs registration failed: simulated native error');
  }

  @override
  Future<String?> getDeviceToken() async => null;

  @override
  void onMessageOpenedApp(void Function(Map<String, dynamic> message) callback) {}

  @override
  void onMessageReceived(void Function(Map<String, dynamic> message) callback) {}

  @override
  void onRegistrationError(void Function(String message) callback) {
    _registrationErrorCallback = callback;
  }

  @override
  void onTokenRefresh(void Function(String token) callback) {}
}
