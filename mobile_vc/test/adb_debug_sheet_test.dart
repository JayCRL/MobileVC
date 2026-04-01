import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:flutter_webrtc/flutter_webrtc.dart';
import 'package:mobile_vc/features/adb/adb_debug_sheet.dart';

void main() {
  testWidgets('模拟器配置界面提供纵向滚动容器', (tester) async {
    await tester.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: AdbDebugSheet(
            devices: const [],
            availableAvds: const ['Pixel_8_API_35', 'Pixel_Tablet_API_35'],
            selectedSerial: '',
            selectedAvd: 'Pixel_8_API_35',
            status: '',
            adbAvailable: true,
            emulatorAvailable: true,
            suggestedAction: 'start',
            streaming: false,
            webRtcConnected: false,
            webRtcStarting: false,
            renderer: RTCVideoRenderer(),
            frameWidth: 0,
            frameHeight: 0,
            onRefreshDevices: () {},
            onSelectAvd: (_) {},
            onLaunchEmulator: (_) {},
            onStart: (_) {},
            onStop: () {},
            onTapPreview: (x, y, serial) {},
            onSwipePreview:
                (startX, startY, endX, endY, serial, durationMs) {},
          ),
        ),
      ),
    );

    expect(find.byType(SingleChildScrollView), findsOneWidget);
    expect(find.text('模拟器模板'), findsOneWidget);
    expect(find.text('点击“进入调试”后直接进入全屏调试器'), findsOneWidget);
  });
}
