import 'dart:async';

import 'package:flutter/foundation.dart';
import 'package:flutter/widgets.dart';

import 'app/app.dart';

Future<void> main() async {
  await runZonedGuarded(() async {
    FlutterError.onError = (details) {
      debugPrint('[startup] FlutterError: ${details.exceptionAsString()}');
      debugPrintStack(
        stackTrace: details.stack,
        label: '[startup] FlutterError stack',
      );
      FlutterError.presentError(details);
    };

    PlatformDispatcher.instance.onError = (error, stack) {
      debugPrint('[startup] PlatformDispatcher error: $error');
      debugPrintStack(
        stackTrace: stack,
        label: '[startup] PlatformDispatcher stack',
      );
      return true;
    };

    debugPrint('[startup] main start');
    WidgetsFlutterBinding.ensureInitialized();
    debugPrint('[startup] runApp start');
    runApp(const MobileVcApp());
    debugPrint('[startup] runApp end');
  }, (error, stack) {
    debugPrint('[startup] runZonedGuarded error: $error');
    debugPrintStack(
      stackTrace: stack,
      label: '[startup] runZonedGuarded stack',
    );
  });
}
