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
    final showLivePreview = widget.streaming || widget.webRtcStarting;
    return DecoratedBox(
      decoration: BoxDecoration(
        gradient: LinearGradient(
          colors: [
            scheme.primary.withValues(alpha: 0.06),
            scheme.surface,
            scheme.surface,
          ],
          begin: Alignment.topCenter,
          end: Alignment.bottomCenter,
        ),
      ),
      child: Padding(
        padding: EdgeInsets.fromLTRB(
          16,
          8,
          16,
          24 + MediaQuery.of(context).viewInsets.bottom,
        ),
        child: SingleChildScrollView(
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
              if (showLivePreview) ...[
                const SizedBox(height: 14),
                Center(
                  child: widget.expandPreview
                      ? _buildPreview(context, previewAspectRatio)
                      : ConstrainedBox(
                          constraints: BoxConstraints(
                            maxHeight:
                                MediaQuery.of(context).size.height * 0.62,
                          ),
                          child: _buildPreview(context, previewAspectRatio),
                        ),
                ),
              ],
            ],
          ),
        ),
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
    return _buildSectionCard(
      context,
      title: '在线设备',
      subtitle: '选择一个已连接的 Android 设备或模拟器',
      child: widget.devices.isEmpty
          ? _buildEmptySectionState(
              context,
              icon: Icons.usb_off_rounded,
              title: '当前未发现在线设备',
              description: '可以先自动检测，或者启动一个本地 AVD。',
            )
          : Wrap(
              spacing: 8,
              runSpacing: 8,
              children: [
                for (final item in widget.devices)
                  ChoiceChip(
                    avatar: Icon(
                      item.state.trim().toLowerCase() == 'device'
                          ? Icons.check_circle_rounded
                          : Icons.pending_rounded,
                      size: 18,
                    ),
                    label: Text(item.displayLabel),
                    selected: _selectedSerial.trim() == item.serial,
                    onSelected: (_) {
                      setState(() => _selectedSerial = item.serial);
                    },
                  ),
              ],
            ),
    );
  }

  Widget _buildAvdSelector(BuildContext context) {
    return _buildSectionCard(
      context,
      title: '模拟器模板',
      subtitle: '没有在线设备时，可直接拉起一个预置 AVD',
      child: Wrap(
        spacing: 8,
        runSpacing: 8,
        children: [
          for (final avd in widget.availableAvds)
            ChoiceChip(
              avatar: const Icon(Icons.smart_display_rounded, size: 18),
              label: Text(avd),
              selected: _selectedAvd.trim() == avd,
              onSelected: (_) {
                setState(() => _selectedAvd = avd);
                widget.onSelectAvd(avd);
              },
            ),
        ],
      ),
    );
  }

  Widget _buildControlPanel(BuildContext context) {
    final scheme = Theme.of(context).colorScheme;
    final hasConnectedDevice = widget.devices
        .any((item) => item.state.trim().toLowerCase() == 'device');
    final canLaunchEmulator =
        widget.emulatorAvailable && widget.availableAvds.isNotEmpty;
    final primaryLabel = widget.streaming || widget.webRtcStarting
        ? (widget.webRtcConnected ? '调试中' : '连接中')
        : hasConnectedDevice
            ? '进入调试'
            : canLaunchEmulator
                ? '启动模拟器'
                : '无法调试';
    return Container(
      width: double.infinity,
      padding: const EdgeInsets.fromLTRB(16, 16, 16, 16),
      decoration: BoxDecoration(
        gradient: LinearGradient(
          colors: [
            scheme.surface,
            scheme.surfaceContainerLow,
          ],
          begin: Alignment.topLeft,
          end: Alignment.bottomRight,
        ),
        borderRadius: BorderRadius.circular(28),
        border: Border.all(color: scheme.outlineVariant),
        boxShadow: [
          BoxShadow(
            color: Colors.black.withValues(alpha: 0.05),
            blurRadius: 20,
            offset: const Offset(0, 10),
          ),
        ],
      ),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Row(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Expanded(
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    Text(
                      '调试控制台',
                      style: Theme.of(context).textTheme.titleMedium?.copyWith(
                            fontWeight: FontWeight.w800,
                          ),
                    ),
                    const SizedBox(height: 4),
                    Text(
                      widget.status.trim().isEmpty
                          ? '等待启动 WebRTC 调试'
                          : widget.status.trim(),
                      style: Theme.of(context).textTheme.bodyMedium?.copyWith(
                            color: scheme.onSurfaceVariant,
                            height: 1.35,
                          ),
                    ),
                  ],
                ),
              ),
              const SizedBox(width: 12),
              _buildStatusBadge(
                context,
                icon: widget.streaming || widget.webRtcStarting
                    ? Icons.wifi_tethering_rounded
                    : Icons.bolt_rounded,
                label: primaryLabel,
                highlighted: widget.streaming || widget.webRtcStarting,
              ),
            ],
          ),
          const SizedBox(height: 14),
          Wrap(
            spacing: 8,
            runSpacing: 8,
            children: [
              _buildStatusBadge(
                context,
                icon: Icons.route_rounded,
                label: widget.suggestedAction.trim().isEmpty
                    ? '等待检测'
                    : _suggestionLabel(widget.suggestedAction),
              ),
              _buildStatusBadge(
                context,
                icon: Icons.memory_rounded,
                label: 'WebRTC / H264',
              ),
              if (widget.frameWidth > 0 && widget.frameHeight > 0)
                _buildStatusBadge(
                  context,
                  icon: Icons.straighten_rounded,
                  label: '${widget.frameWidth} x ${widget.frameHeight}',
                ),
            ],
          ),
          const SizedBox(height: 16),
          Wrap(
            spacing: 8,
            runSpacing: 8,
            crossAxisAlignment: WrapCrossAlignment.center,
            children: [
              OutlinedButton.icon(
                onPressed: widget.onRefreshDevices,
                icon: const Icon(Icons.radar_rounded),
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
        ],
      ),
    );
  }

  Widget _buildSectionCard(
    BuildContext context, {
    required String title,
    required String subtitle,
    required Widget child,
  }) {
    final scheme = Theme.of(context).colorScheme;
    return Container(
      width: double.infinity,
      padding: const EdgeInsets.fromLTRB(16, 16, 16, 14),
      decoration: BoxDecoration(
        color: scheme.surface.withValues(alpha: 0.86),
        borderRadius: BorderRadius.circular(24),
        border:
            Border.all(color: scheme.outlineVariant.withValues(alpha: 0.85)),
      ),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Text(
            title,
            style: Theme.of(context).textTheme.titleSmall?.copyWith(
                  fontWeight: FontWeight.w800,
                ),
          ),
          const SizedBox(height: 4),
          Text(
            subtitle,
            style: Theme.of(context).textTheme.bodySmall?.copyWith(
                  color: scheme.onSurfaceVariant,
                ),
          ),
          const SizedBox(height: 12),
          child,
        ],
      ),
    );
  }

  Widget _buildStatusBadge(
    BuildContext context, {
    required IconData icon,
    required String label,
    bool highlighted = false,
  }) {
    final scheme = Theme.of(context).colorScheme;
    return Container(
      padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 8),
      decoration: BoxDecoration(
        color: highlighted
            ? scheme.primary.withValues(alpha: 0.14)
            : scheme.surface.withValues(alpha: 0.72),
        borderRadius: BorderRadius.circular(999),
        border: Border.all(
          color: highlighted
              ? scheme.primary.withValues(alpha: 0.28)
              : scheme.outlineVariant.withValues(alpha: 0.72),
        ),
      ),
      child: Row(
        mainAxisSize: MainAxisSize.min,
        children: [
          Icon(
            icon,
            size: 16,
            color: highlighted ? scheme.primary : scheme.onSurfaceVariant,
          ),
          const SizedBox(width: 8),
          Text(
            label,
            style: Theme.of(context).textTheme.labelMedium?.copyWith(
                  fontWeight: FontWeight.w700,
                  color: highlighted ? scheme.primary : scheme.onSurface,
                ),
          ),
        ],
      ),
    );
  }

  Widget _buildEmptySectionState(
    BuildContext context, {
    required IconData icon,
    required String title,
    required String description,
  }) {
    final scheme = Theme.of(context).colorScheme;
    return Container(
      width: double.infinity,
      padding: const EdgeInsets.fromLTRB(14, 14, 14, 14),
      decoration: BoxDecoration(
        color: scheme.surfaceContainerLowest,
        borderRadius: BorderRadius.circular(18),
        border: Border.all(color: scheme.outlineVariant.withValues(alpha: 0.7)),
      ),
      child: Row(
        children: [
          Container(
            width: 40,
            height: 40,
            decoration: BoxDecoration(
              color: scheme.surfaceContainerHighest,
              borderRadius: BorderRadius.circular(14),
            ),
            child: Icon(icon, color: scheme.onSurfaceVariant),
          ),
          const SizedBox(width: 12),
          Expanded(
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Text(
                  title,
                  style: Theme.of(context).textTheme.labelLarge?.copyWith(
                        fontWeight: FontWeight.w800,
                      ),
                ),
                const SizedBox(height: 4),
                Text(
                  description,
                  style: Theme.of(context).textTheme.bodySmall?.copyWith(
                        color: scheme.onSurfaceVariant,
                      ),
                ),
              ],
            ),
          ),
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
