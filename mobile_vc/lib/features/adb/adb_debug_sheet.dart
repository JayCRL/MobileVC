import 'package:flutter/foundation.dart';
import 'package:flutter/material.dart';
import 'package:flutter_webrtc/flutter_webrtc.dart';

import '../../data/models/session_models.dart';

class AdbDebugSheet extends StatefulWidget {
  const AdbDebugSheet({
    super.key,
    required this.devices,
    required this.availableAvds,
    required this.selectedSerial,
    required this.selectedAvd,
    required this.status,
    required this.adbAvailable,
    required this.emulatorAvailable,
    required this.suggestedAction,
    required this.streaming,
    required this.webRtcConnected,
    required this.webRtcStarting,
    required this.renderer,
    required this.frameWidth,
    required this.frameHeight,
    required this.onRefreshDevices,
    required this.onSelectAvd,
    required this.onLaunchEmulator,
    required this.onStart,
    required this.onStop,
    required this.onTapPreview,
    required this.onSwipePreview,
    this.showInlineHeader = true,
    this.expandPreview = false,
    this.showImmersiveChrome = true,
  });

  final List<AdbDevice> devices;
  final List<String> availableAvds;
  final String selectedSerial;
  final String selectedAvd;
  final String status;
  final bool adbAvailable;
  final bool emulatorAvailable;
  final String suggestedAction;
  final bool streaming;
  final bool webRtcConnected;
  final bool webRtcStarting;
  final RTCVideoRenderer renderer;
  final int frameWidth;
  final int frameHeight;
  final VoidCallback onRefreshDevices;
  final ValueChanged<String> onSelectAvd;
  final ValueChanged<String> onLaunchEmulator;
  final ValueChanged<String> onStart;
  final VoidCallback onStop;
  final void Function(int x, int y, String serial) onTapPreview;
  final void Function(
    int startX,
    int startY,
    int endX,
    int endY,
    String serial,
    int durationMs,
  ) onSwipePreview;
  final bool showInlineHeader;
  final bool expandPreview;
  final bool showImmersiveChrome;

  @override
  State<AdbDebugSheet> createState() => _AdbDebugSheetState();
}

class _AdbDebugSheetState extends State<AdbDebugSheet> {
  late String _selectedSerial;
  late String _selectedAvd;
  bool _showOverlayControls = false;
  Offset? _dragStart;
  Offset? _dragCurrent;
  DateTime? _dragStartedAt;

  @override
  void initState() {
    super.initState();
    _selectedSerial = widget.selectedSerial.isNotEmpty
        ? widget.selectedSerial
        : (widget.devices.isNotEmpty ? widget.devices.first.serial : '');
    _selectedAvd = widget.selectedAvd.isNotEmpty
        ? widget.selectedAvd
        : (widget.availableAvds.isNotEmpty ? widget.availableAvds.first : '');
  }

  @override
  void didUpdateWidget(covariant AdbDebugSheet oldWidget) {
    super.didUpdateWidget(oldWidget);
    final current = _selectedSerial.trim();
    final hasCurrent = widget.devices.any((item) => item.serial == current);
    if (current.isEmpty || !hasCurrent) {
      _selectedSerial = widget.selectedSerial.trim().isNotEmpty
          ? widget.selectedSerial.trim()
          : (widget.devices.isNotEmpty ? widget.devices.first.serial : '');
    }
    final currentAvd = _selectedAvd.trim();
    final hasCurrentAvd = widget.availableAvds.contains(currentAvd);
    if (currentAvd.isEmpty || !hasCurrentAvd) {
      _selectedAvd = widget.selectedAvd.trim().isNotEmpty
          ? widget.selectedAvd.trim()
          : (widget.availableAvds.isNotEmpty ? widget.availableAvds.first : '');
    }
  }

  @override
  Widget build(BuildContext context) {
    final previewAspectRatio = widget.frameWidth > 0 && widget.frameHeight > 0
        ? widget.frameWidth / widget.frameHeight
        : 9 / 19.5;
    final immersivePreview =
        widget.expandPreview && (widget.streaming || widget.webRtcStarting);
    if (immersivePreview) {
      return _buildImmersiveLayout(context, previewAspectRatio);
    }
    return _buildStandardLayout(context, previewAspectRatio);
  }

  Widget _buildStandardLayout(BuildContext context, double previewAspectRatio) {
    final scheme = Theme.of(context).colorScheme;
    return Padding(
      padding: EdgeInsets.fromLTRB(
        16,
        8,
        16,
        24 + MediaQuery.of(context).viewInsets.bottom,
      ),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          if (widget.showInlineHeader) ...[
            Text(
              'ADB 调试',
              style: Theme.of(context)
                  .textTheme
                  .titleLarge
                  ?.copyWith(fontWeight: FontWeight.w800),
            ),
            const SizedBox(height: 6),
            Text(
              '使用 WebRTC + H264 把电脑上的 Android 模拟器实时推到手机端，点击画面会即时回传到 adb。',
              style: Theme.of(context)
                  .textTheme
                  .bodySmall
                  ?.copyWith(color: scheme.onSurfaceVariant),
            ),
            const SizedBox(height: 14),
          ],
          _buildDeviceSelector(context),
          if (widget.availableAvds.isNotEmpty) ...[
            const SizedBox(height: 12),
            _buildAvdSelector(context),
          ],
          const SizedBox(height: 12),
          _buildControlPanel(context),
          const SizedBox(height: 14),
          Expanded(
            child: Center(
              child: widget.expandPreview
                  ? _buildPreview(context, previewAspectRatio)
                  : ConstrainedBox(
                      constraints: BoxConstraints(
                        maxHeight: MediaQuery.of(context).size.height * 0.62,
                      ),
                      child: _buildPreview(context, previewAspectRatio),
                    ),
            ),
          ),
        ],
      ),
    );
  }

  Widget _buildImmersiveLayout(
      BuildContext context, double previewAspectRatio) {
    final scheme = Theme.of(context).colorScheme;
    final safePadding = MediaQuery.of(context).padding;
    return Stack(
      children: [
        Positioned.fill(
          child: ColoredBox(
            color: Colors.black,
            child: Center(
              child: _buildPreview(
                context,
                previewAspectRatio,
                borderRadius: 0,
                showInstructionBanner: !_showOverlayControls,
              ),
            ),
          ),
        ),
        if (widget.showImmersiveChrome) ...[
          Positioned(
            top: safePadding.top + 12,
            left: 12,
            right: 12,
            child: Row(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Expanded(
                  child: DecoratedBox(
                    decoration: BoxDecoration(
                      color: Colors.black.withValues(alpha: 0.58),
                      borderRadius: BorderRadius.circular(18),
                      border: Border.all(
                        color: Colors.white.withValues(alpha: 0.08),
                      ),
                    ),
                    child: Padding(
                      padding: const EdgeInsets.symmetric(
                        horizontal: 12,
                        vertical: 10,
                      ),
                      child: Column(
                        crossAxisAlignment: CrossAxisAlignment.start,
                        mainAxisSize: MainAxisSize.min,
                        children: [
                          Text(
                            widget.webRtcConnected ? '调试中' : '连接中',
                            style: Theme.of(context)
                                .textTheme
                                .labelLarge
                                ?.copyWith(
                                  color: Colors.white,
                                  fontWeight: FontWeight.w800,
                                ),
                          ),
                          const SizedBox(height: 4),
                          Text(
                            widget.status.trim().isEmpty
                                ? '等待启动 WebRTC 调试'
                                : widget.status.trim(),
                            style: Theme.of(context)
                                .textTheme
                                .bodySmall
                                ?.copyWith(
                                  color: Colors.white.withValues(alpha: 0.88),
                                ),
                            maxLines: 2,
                            overflow: TextOverflow.ellipsis,
                          ),
                          if (widget.frameWidth > 0 &&
                              widget.frameHeight > 0) ...[
                            const SizedBox(height: 4),
                            Text(
                              '${widget.frameWidth} x ${widget.frameHeight}'
                              '${_selectedSerial.trim().isNotEmpty ? ' · $_selectedSerial' : ''}',
                              style: Theme.of(context)
                                  .textTheme
                                  .bodySmall
                                  ?.copyWith(
                                    color: Colors.white.withValues(alpha: 0.7),
                                  ),
                            ),
                          ],
                        ],
                      ),
                    ),
                  ),
                ),
                const SizedBox(width: 8),
                FilledButton.tonalIcon(
                  onPressed: () {
                    setState(() {
                      _showOverlayControls = !_showOverlayControls;
                    });
                  },
                  icon: Icon(
                    _showOverlayControls
                        ? Icons.keyboard_arrow_down_rounded
                        : Icons.tune_rounded,
                  ),
                  label: Text(_showOverlayControls ? '收起' : '控制'),
                ),
              ],
            ),
          ),
          Positioned(
            left: 12,
            right: 12,
            bottom: safePadding.bottom + 12,
            child: AnimatedSlide(
              duration: const Duration(milliseconds: 220),
              curve: Curves.easeOutCubic,
              offset:
                  _showOverlayControls ? Offset.zero : const Offset(0, 1.08),
              child: AnimatedOpacity(
                duration: const Duration(milliseconds: 180),
                opacity: _showOverlayControls ? 1 : 0,
                child: IgnorePointer(
                  ignoring: !_showOverlayControls,
                  child: DecoratedBox(
                    decoration: BoxDecoration(
                      color: scheme.surface.withValues(alpha: 0.96),
                      borderRadius: BorderRadius.circular(24),
                      border: Border.all(color: scheme.outlineVariant),
                      boxShadow: [
                        BoxShadow(
                          color: Colors.black.withValues(alpha: 0.18),
                          blurRadius: 30,
                          offset: const Offset(0, 14),
                        ),
                      ],
                    ),
                    child: ConstrainedBox(
                      constraints: BoxConstraints(
                        maxHeight: MediaQuery.of(context).size.height * 0.42,
                      ),
                      child: SingleChildScrollView(
                        padding: const EdgeInsets.fromLTRB(14, 14, 14, 16),
                        child: Column(
                          crossAxisAlignment: CrossAxisAlignment.start,
                          children: [
                            _buildDeviceSelector(context),
                            if (widget.availableAvds.isNotEmpty) ...[
                              const SizedBox(height: 12),
                              _buildAvdSelector(context),
                            ],
                            const SizedBox(height: 12),
                            _buildControlPanel(context),
                          ],
                        ),
                      ),
                    ),
                  ),
                ),
              ),
            ),
          ),
        ],
      ],
    );
  }

  Widget _buildDeviceSelector(BuildContext context) {
    return Wrap(
      spacing: 8,
      runSpacing: 8,
      children: [
        for (final item in widget.devices)
          ChoiceChip(
            label: Text(item.displayLabel),
            selected: _selectedSerial.trim() == item.serial,
            onSelected: (_) {
              setState(() => _selectedSerial = item.serial);
            },
          ),
        if (widget.devices.isEmpty)
          Text(
            '当前未发现在线设备。',
            style: Theme.of(context).textTheme.bodySmall,
          ),
      ],
    );
  }

  Widget _buildAvdSelector(BuildContext context) {
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        Text(
          '可启动模拟器',
          style: Theme.of(context)
              .textTheme
              .labelLarge
              ?.copyWith(fontWeight: FontWeight.w700),
        ),
        const SizedBox(height: 8),
        Wrap(
          spacing: 8,
          runSpacing: 8,
          children: [
            for (final avd in widget.availableAvds)
              ChoiceChip(
                label: Text(avd),
                selected: _selectedAvd.trim() == avd,
                onSelected: (_) {
                  setState(() => _selectedAvd = avd);
                  widget.onSelectAvd(avd);
                },
              ),
          ],
        ),
      ],
    );
  }

  Widget _buildControlPanel(BuildContext context) {
    final scheme = Theme.of(context).colorScheme;
    final hasConnectedDevice = widget.devices
        .any((item) => item.state.trim().toLowerCase() == 'device');
    final canLaunchEmulator =
        widget.emulatorAvailable && widget.availableAvds.isNotEmpty;
    return Container(
      width: double.infinity,
      padding: const EdgeInsets.all(12),
      decoration: BoxDecoration(
        color: scheme.surfaceContainerHighest,
        borderRadius: BorderRadius.circular(18),
        border: Border.all(color: scheme.outlineVariant),
      ),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Wrap(
            spacing: 8,
            runSpacing: 8,
            crossAxisAlignment: WrapCrossAlignment.center,
            children: [
              FilledButton.tonalIcon(
                onPressed: widget.onRefreshDevices,
                icon: const Icon(Icons.devices_outlined),
                label: const Text('自动检测'),
              ),
              if (widget.streaming || widget.webRtcStarting)
                FilledButton.icon(
                  onPressed: null,
                  icon: const Icon(Icons.wifi_tethering_outlined),
                  label: Text(widget.webRtcConnected ? '调试中' : '连接中'),
                )
              else if (hasConnectedDevice)
                FilledButton.icon(
                  onPressed: () => widget.onStart(_selectedSerial),
                  icon: const Icon(Icons.play_arrow),
                  label: const Text('进入调试'),
                )
              else if (canLaunchEmulator)
                FilledButton.icon(
                  onPressed: () => widget.onLaunchEmulator(_selectedAvd),
                  icon: const Icon(Icons.smart_display_outlined),
                  label: const Text('启动模拟器'),
                )
              else
                FilledButton.icon(
                  onPressed: null,
                  icon: const Icon(Icons.info_outline),
                  label: const Text('无法调试'),
                ),
              OutlinedButton.icon(
                onPressed: widget.streaming || widget.webRtcStarting
                    ? widget.onStop
                    : null,
                icon: const Icon(Icons.stop_circle_outlined),
                label: const Text('停止'),
              ),
            ],
          ),
          const SizedBox(height: 10),
          Text(
            widget.status.trim().isEmpty
                ? '等待启动 WebRTC 调试'
                : widget.status.trim(),
            style: Theme.of(context).textTheme.bodyMedium,
          ),
          const SizedBox(height: 6),
          Text(
            'adb ${widget.adbAvailable ? "已就绪" : "未就绪"}'
            ' · emulator ${widget.emulatorAvailable ? "已就绪" : "未就绪"}'
            ' · 传输：WebRTC / H264'
            '${widget.suggestedAction.trim().isNotEmpty ? ' · 建议：${_suggestionLabel(widget.suggestedAction)}' : ''}',
            style: Theme.of(
              context,
            ).textTheme.bodySmall?.copyWith(color: scheme.onSurfaceVariant),
          ),
          if (widget.frameWidth > 0 && widget.frameHeight > 0) ...[
            const SizedBox(height: 6),
            Text(
              '映射分辨率 ${widget.frameWidth} x ${widget.frameHeight}'
              '${_selectedSerial.trim().isNotEmpty ? ' · $_selectedSerial' : ''}',
              style: Theme.of(
                context,
              ).textTheme.bodySmall?.copyWith(color: scheme.onSurfaceVariant),
            ),
          ],
        ],
      ),
    );
  }

  Widget _buildPreview(
    BuildContext context,
    double previewAspectRatio, {
    double borderRadius = 24,
    bool showInstructionBanner = true,
  }) {
    final scheme = Theme.of(context).colorScheme;
    return AspectRatio(
      aspectRatio: previewAspectRatio,
      child: LayoutBuilder(
        builder: (context, constraints) {
          final leadingGestureInset = _leadingSystemGestureInset(context);
          return ClipRRect(
            borderRadius: BorderRadius.circular(borderRadius),
            child: Stack(
              fit: StackFit.expand,
              children: [
                DecoratedBox(
                  decoration: BoxDecoration(
                    color: Colors.black,
                    gradient: LinearGradient(
                      colors: [
                        Colors.black,
                        scheme.surfaceContainerHighest,
                      ],
                      begin: Alignment.topCenter,
                      end: Alignment.bottomCenter,
                    ),
                  ),
                  child: widget.streaming || widget.webRtcStarting
                      ? RTCVideoView(
                          widget.renderer,
                          objectFit:
                              RTCVideoViewObjectFit.RTCVideoViewObjectFitCover,
                        )
                      : _EmptyPreview(status: widget.status),
                ),
                Positioned.fill(
                  left: leadingGestureInset,
                  child: GestureDetector(
                    behavior: HitTestBehavior.opaque,
                    onPanStart: (details) {
                      _dragStart = details.localPosition;
                      _dragCurrent = details.localPosition;
                      _dragStartedAt = DateTime.now();
                    },
                    onPanUpdate: (details) {
                      _dragCurrent = details.localPosition;
                    },
                    onPanEnd: (_) {
                      final start = _dragStart;
                      final end = _dragCurrent;
                      final startedAt = _dragStartedAt;
                      _dragStart = null;
                      _dragCurrent = null;
                      _dragStartedAt = null;
                      if (start == null || end == null || startedAt == null) {
                        return;
                      }
                      final delta = end - start;
                      if (delta.distance < 18) {
                        return;
                      }
                      final width = constraints.maxWidth - leadingGestureInset;
                      final height = constraints.maxHeight;
                      if (width <= 0 || height <= 0) {
                        return;
                      }
                      final startPoint = _toFramePoint(start, width, height);
                      final endPoint = _toFramePoint(end, width, height);
                      if (startPoint == null || endPoint == null) {
                        return;
                      }
                      final durationMs =
                          (DateTime.now().difference(startedAt).inMilliseconds)
                              .clamp(120, 900);
                      widget.onSwipePreview(
                        startPoint.dx.round(),
                        startPoint.dy.round(),
                        endPoint.dx.round(),
                        endPoint.dy.round(),
                        _selectedSerial,
                        durationMs,
                      );
                    },
                    onTapUp: (details) {
                      final width = constraints.maxWidth - leadingGestureInset;
                      final height = constraints.maxHeight;
                      final point =
                          _toFramePoint(details.localPosition, width, height);
                      if (point == null) {
                        return;
                      }
                      widget.onTapPreview(
                        point.dx.round(),
                        point.dy.round(),
                        _selectedSerial,
                      );
                    },
                    child: const SizedBox.expand(),
                  ),
                ),
                if (showInstructionBanner && widget.showImmersiveChrome)
                  Positioned(
                    left: 12 + leadingGestureInset,
                    top: 12,
                    child: DecoratedBox(
                      decoration: BoxDecoration(
                        color: Colors.black.withValues(alpha: 0.55),
                        borderRadius: BorderRadius.circular(999),
                      ),
                      child: Padding(
                        padding: const EdgeInsets.symmetric(
                          horizontal: 10,
                          vertical: 6,
                        ),
                        child: Text(
                          widget.webRtcConnected
                              ? '点击发送 tap，滑动发送 swipe'
                              : '等待 WebRTC 画面…',
                          style: const TextStyle(color: Colors.white),
                        ),
                      ),
                    ),
                  ),
              ],
            ),
          );
        },
      ),
    );
  }

  Offset? _toFramePoint(Offset local, double width, double height) {
    if (widget.frameWidth <= 0 || widget.frameHeight <= 0) {
      return null;
    }
    if (width <= 0 || height <= 0) {
      return null;
    }
    final x = ((local.dx.clamp(0.0, width) / width) * widget.frameWidth)
        .clamp(0.0, (widget.frameWidth - 1).toDouble());
    final y = ((local.dy.clamp(0.0, height) / height) * widget.frameHeight)
        .clamp(0.0, (widget.frameHeight - 1).toDouble());
    return Offset(x, y);
  }

  double _leadingSystemGestureInset(BuildContext context) {
    if (kIsWeb || defaultTargetPlatform != TargetPlatform.iOS) {
      return 0;
    }
    return 28 + MediaQuery.of(context).padding.left;
  }
}

String _suggestionLabel(String value) {
  switch (value.trim()) {
    case 'debug':
      return '进入调试';
    case 'start':
      return '启动模拟器';
    default:
      return value.trim();
  }
}

class _EmptyPreview extends StatelessWidget {
  const _EmptyPreview({required this.status});

  final String status;

  @override
  Widget build(BuildContext context) {
    final scheme = Theme.of(context).colorScheme;
    return Container(
      width: double.infinity,
      padding: const EdgeInsets.all(18),
      decoration: BoxDecoration(
        color: scheme.surfaceContainerHighest,
      ),
      child: Column(
        mainAxisAlignment: MainAxisAlignment.center,
        children: [
          Icon(
            Icons.phone_android_outlined,
            size: 44,
            color: scheme.onSurfaceVariant,
          ),
          const SizedBox(height: 12),
          Text(
            status.trim().isEmpty ? '等待设备上线后启动 WebRTC 调试' : status.trim(),
            textAlign: TextAlign.center,
            style: Theme.of(context).textTheme.bodyMedium?.copyWith(
                  color: scheme.onSurfaceVariant,
                ),
          ),
        ],
      ),
    );
  }
}
