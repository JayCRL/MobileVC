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
        final immersive =
            controller.adbStreaming || controller.adbWebRtcStarting;
        final safePadding = MediaQuery.of(context).padding;
        String targetSerial = controller.adbSelectedSerial.trim();
        if (targetSerial.isEmpty) {
          for (final item in controller.adbDevices) {
            if (item.state.trim().toLowerCase() == 'device' &&
                item.serial.trim().isNotEmpty) {
              targetSerial = item.serial.trim();
              break;
            }
          }
        }
        return Scaffold(
          body: Stack(
            children: [
              Positioned.fill(
                child: Padding(
                  padding: immersive
                      ? EdgeInsets.zero
                      : EdgeInsets.only(
                          top: safePadding.top,
                          bottom: safePadding.bottom,
                        ),
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
                    onLaunchEmulator: (avd) =>
                        controller.launchAdbEmulator(avd: avd),
                    onStart: (serial) =>
                        controller.startAdbStream(serial: serial),
                    onStop: controller.stopAdbStream,
                    onTapPreview: (x, y, serial) =>
                        controller.sendAdbTap(x, y, serial: serial),
                    onSwipePreview:
                        (startX, startY, endX, endY, serial, durationMs) =>
                            controller.sendAdbSwipe(
                      startX,
                      startY,
                      endX,
                      endY,
                      serial: serial,
                      durationMs: durationMs,
                    ),
                    showInlineHeader: false,
                    expandPreview: true,
                    showImmersiveChrome: false,
                  ),
                ),
              ),
              Positioned(
                right: 18,
                bottom: 18 + safePadding.bottom,
                child: Row(
                  mainAxisSize: MainAxisSize.min,
                  children: [
                    Material(
                      color: Colors.transparent,
                      child: InkWell(
                        onTap: () {
                          if (targetSerial.isNotEmpty ||
                              controller.adbStreaming ||
                              controller.adbWebRtcConnected) {
                            controller.sendAdbKeyevent('KEYCODE_BACK',
                                serial: targetSerial);
                          } else {
                            Navigator.of(context).maybePop();
                          }
                        },
                        borderRadius: BorderRadius.circular(999),
                        child: Ink(
                          width: 48,
                          height: 48,
                          decoration: BoxDecoration(
                            color: Colors.black.withValues(alpha: 0.58),
                            borderRadius: BorderRadius.circular(999),
                            border: Border.all(
                              color: Colors.white.withValues(alpha: 0.14),
                            ),
                          ),
                          child: const Icon(
                            Icons.arrow_back_rounded,
                            color: Colors.white,
                            size: 22,
                          ),
                        ),
                      ),
                    ),
                    const SizedBox(width: 12),
                    Material(
                      color: Colors.transparent,
                      child: InkWell(
                        onTap: () {
                          if (canStop) {
                            controller.stopAdbStream();
                          }
                          Navigator.of(context).maybePop();
                        },
                        borderRadius: BorderRadius.circular(999),
                        child: Ink(
                          width: 48,
                          height: 48,
                          decoration: BoxDecoration(
                            color: Colors.black.withValues(alpha: 0.58),
                            borderRadius: BorderRadius.circular(999),
                            border: Border.all(
                              color: Colors.white.withValues(alpha: 0.14),
                            ),
                          ),
                          child: const Icon(
                            Icons.close_rounded,
                            color: Colors.white,
                            size: 22,
                          ),
                        ),
                      ),
                    ),
                  ],
                ),
              ),
            ],
          ),
        );
      },
    );
  }
}
