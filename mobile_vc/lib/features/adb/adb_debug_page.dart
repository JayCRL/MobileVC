import 'package:flutter/material.dart';

import '../session/session_controller.dart';
import 'adb_debug_sheet.dart';

class AdbDebugPage extends StatelessWidget {
  const AdbDebugPage({super.key, required this.controller});

  final SessionController controller;

  @override
  Widget build(BuildContext context) {
    return ListenableBuilder(
      listenable: controller,
      builder: (context, _) {
        final canStop = controller.adbStreaming || controller.adbWebRtcStarting;
        return Scaffold(
          appBar: AppBar(
            leading: IconButton(
              tooltip: '返回',
              icon: const Icon(Icons.arrow_back_rounded),
              onPressed: () => Navigator.of(context).maybePop(),
            ),
            title: const Text('ADB 调试'),
            actions: [
              TextButton.icon(
                onPressed: canStop
                    ? () {
                        controller.stopAdbStream();
                        Navigator.of(context).maybePop();
                      }
                    : null,
                icon: const Icon(Icons.close_rounded),
                label: const Text('退出'),
              ),
            ],
          ),
          body: SafeArea(
            child: AdbDebugSheet(
              devices: controller.adbDevices,
              availableAvds: controller.adbAvailableAvds,
              selectedSerial: controller.adbSelectedSerial,
              selectedAvd: controller.adbSelectedAvd,
              status: controller.adbStatus,
              adbAvailable: controller.adbAvailable,
              emulatorAvailable: controller.adbEmulatorAvailable,
              suggestedAction: controller.adbSuggestedAction,
              streaming: controller.adbStreaming,
              webRtcConnected: controller.adbWebRtcConnected,
              webRtcStarting: controller.adbWebRtcStarting,
              renderer: controller.adbRenderer,
              frameWidth: controller.adbFrameWidth,
              frameHeight: controller.adbFrameHeight,
              onRefreshDevices: controller.requestAdbDevices,
              onSelectAvd: controller.selectAdbAvd,
              onLaunchEmulator: (avd) => controller.launchAdbEmulator(avd: avd),
              onStart: (serial) => controller.startAdbStream(serial: serial),
              onStop: controller.stopAdbStream,
              onTapPreview: (x, y, serial) =>
                  controller.sendAdbTap(x, y, serial: serial),
              showInlineHeader: false,
              expandPreview: true,
            ),
          ),
        );
      },
    );
  }
}
