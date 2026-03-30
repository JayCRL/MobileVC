import 'dart:typed_data';

import 'package:flutter/material.dart';

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
    required this.frameBytes,
    required this.frameWidth,
    required this.frameHeight,
    required this.intervalMs,
    required this.onRefreshDevices,
    required this.onSelectAvd,
    required this.onLaunchEmulator,
    required this.onStart,
    required this.onStop,
    required this.onTapPreview,
    required this.onIntervalChanged,
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
  final Uint8List? frameBytes;
  final int frameWidth;
  final int frameHeight;
  final int intervalMs;
  final VoidCallback onRefreshDevices;
  final ValueChanged<String> onSelectAvd;
  final ValueChanged<String> onLaunchEmulator;
  final ValueChanged<String> onStart;
  final VoidCallback onStop;
  final void Function(int x, int y, String serial) onTapPreview;
  final ValueChanged<int> onIntervalChanged;

  @override
  State<AdbDebugSheet> createState() => _AdbDebugSheetState();
}

class _AdbDebugSheetState extends State<AdbDebugSheet> {
  late String _selectedSerial;
  late String _selectedAvd;

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
    final scheme = Theme.of(context).colorScheme;
    final hasConnectedDevice = widget.devices
        .any((item) => item.state.trim().toLowerCase() == 'device');
    final canLaunchEmulator =
        widget.emulatorAvailable && widget.availableAvds.isNotEmpty;
    final previewReady = widget.frameBytes != null &&
        widget.frameWidth > 0 &&
        widget.frameHeight > 0;
    return SafeArea(
      top: false,
      child: Padding(
        padding: EdgeInsets.fromLTRB(
          16,
          8,
          16,
          24 + MediaQuery.of(context).viewInsets.bottom,
        ),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Text(
              'ADB 调试',
              style: Theme.of(context)
                  .textTheme
                  .titleLarge
                  ?.copyWith(fontWeight: FontWeight.w800),
            ),
            const SizedBox(height: 6),
            Text(
              '把电脑上的 Android 模拟器画面推到手机端，点击画面会立刻回传为 adb tap。',
              style: Theme.of(context)
                  .textTheme
                  .bodySmall
                  ?.copyWith(color: scheme.onSurfaceVariant),
            ),
            const SizedBox(height: 14),
            Wrap(
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
            ),
            if (widget.availableAvds.isNotEmpty) ...[
              const SizedBox(height: 12),
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
            const SizedBox(height: 12),
            Container(
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
                      if (widget.streaming)
                        FilledButton.icon(
                          onPressed: null,
                          icon: const Icon(Icons.visibility_outlined),
                          label: const Text('调试中'),
                        )
                      else if (hasConnectedDevice)
                        FilledButton.icon(
                          onPressed: () => widget.onStart(_selectedSerial),
                          icon: const Icon(Icons.play_arrow),
                          label: const Text('进入调试'),
                        )
                      else if (canLaunchEmulator)
                        FilledButton.icon(
                          onPressed: () =>
                              widget.onLaunchEmulator(_selectedAvd),
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
                        onPressed: widget.streaming ? widget.onStop : null,
                        icon: const Icon(Icons.stop_circle_outlined),
                        label: const Text('停止'),
                      ),
                    ],
                  ),
                  const SizedBox(height: 10),
                  Wrap(
                    spacing: 8,
                    runSpacing: 8,
                    children: [350, 700, 1200]
                        .map(
                          (value) => ChoiceChip(
                            label: Text('${value}ms'),
                            selected: widget.intervalMs == value,
                            onSelected: (_) => widget.onIntervalChanged(value),
                          ),
                        )
                        .toList(),
                  ),
                  const SizedBox(height: 10),
                  Text(
                    widget.status.trim().isEmpty
                        ? '等待启动 ADB 画面预览'
                        : widget.status.trim(),
                    style: Theme.of(context).textTheme.bodyMedium,
                  ),
                  const SizedBox(height: 6),
                  Text(
                    'adb ${widget.adbAvailable ? "已就绪" : "未就绪"}'
                    ' · emulator ${widget.emulatorAvailable ? "已就绪" : "未就绪"}'
                    '${widget.suggestedAction.trim().isNotEmpty ? ' · 建议：${_suggestionLabel(widget.suggestedAction)}' : ''}',
                    style: Theme.of(context).textTheme.bodySmall?.copyWith(
                          color: scheme.onSurfaceVariant,
                        ),
                  ),
                  if (widget.frameWidth > 0 && widget.frameHeight > 0) ...[
                    const SizedBox(height: 6),
                    Text(
                      '分辨率 ${widget.frameWidth} x ${widget.frameHeight}'
                      '${_selectedSerial.trim().isNotEmpty ? ' · $_selectedSerial' : ''}',
                      style: Theme.of(context).textTheme.bodySmall?.copyWith(
                            color: scheme.onSurfaceVariant,
                          ),
                    ),
                  ],
                ],
              ),
            ),
            const SizedBox(height: 14),
            Expanded(
              child: previewReady
                  ? Center(
                      child: ConstrainedBox(
                        constraints: BoxConstraints(
                          maxHeight: MediaQuery.of(context).size.height * 0.62,
                        ),
                        child: AspectRatio(
                          aspectRatio: widget.frameWidth / widget.frameHeight,
                          child: LayoutBuilder(
                            builder: (context, constraints) {
                              return ClipRRect(
                                borderRadius: BorderRadius.circular(24),
                                child: GestureDetector(
                                  behavior: HitTestBehavior.opaque,
                                  onTapDown: (details) {
                                    final local = details.localPosition;
                                    final width = constraints.maxWidth;
                                    final height = constraints.maxHeight;
                                    if (width <= 0 || height <= 0) {
                                      return;
                                    }
                                    final x =
                                        ((local.dx.clamp(0.0, width) / width) *
                                                widget.frameWidth)
                                            .round()
                                            .clamp(0, widget.frameWidth - 1);
                                    final y = ((local.dy.clamp(0.0, height) /
                                                height) *
                                            widget.frameHeight)
                                        .round()
                                        .clamp(0, widget.frameHeight - 1);
                                    widget.onTapPreview(
                                      x,
                                      y,
                                      _selectedSerial,
                                    );
                                  },
                                  child: Stack(
                                    fit: StackFit.expand,
                                    children: [
                                      Image.memory(
                                        widget.frameBytes!,
                                        fit: BoxFit.fill,
                                        gaplessPlayback: true,
                                        filterQuality: FilterQuality.none,
                                      ),
                                      Positioned(
                                        left: 12,
                                        top: 12,
                                        child: DecoratedBox(
                                          decoration: BoxDecoration(
                                            color: Colors.black
                                                .withValues(alpha: 0.55),
                                            borderRadius:
                                                BorderRadius.circular(999),
                                          ),
                                          child: const Padding(
                                            padding: EdgeInsets.symmetric(
                                              horizontal: 10,
                                              vertical: 6,
                                            ),
                                            child: Text(
                                              '点击画面发送 tap',
                                              style: TextStyle(
                                                  color: Colors.white),
                                            ),
                                          ),
                                        ),
                                      ),
                                    ],
                                  ),
                                ),
                              );
                            },
                          ),
                        ),
                      ),
                    )
                  : _EmptyPreview(status: widget.status),
            ),
          ],
        ),
      ),
    );
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
      decoration: BoxDecoration(
        borderRadius: BorderRadius.circular(28),
        border: Border.all(color: scheme.outlineVariant),
        gradient: LinearGradient(
          begin: Alignment.topLeft,
          end: Alignment.bottomRight,
          colors: [
            scheme.surfaceContainerHighest,
            scheme.surface,
          ],
        ),
      ),
      child: Center(
        child: Padding(
          padding: const EdgeInsets.all(24),
          child: Column(
            mainAxisSize: MainAxisSize.min,
            children: [
              Icon(
                Icons.phone_android,
                size: 46,
                color: scheme.primary,
              ),
              const SizedBox(height: 12),
              Text(
                '还没有预览画面',
                style: Theme.of(context)
                    .textTheme
                    .titleMedium
                    ?.copyWith(fontWeight: FontWeight.w700),
              ),
              const SizedBox(height: 8),
              Text(
                status.trim().isEmpty ? '先刷新设备并开始预览。' : status.trim(),
                textAlign: TextAlign.center,
                style: Theme.of(context).textTheme.bodyMedium?.copyWith(
                      color: scheme.onSurfaceVariant,
                    ),
              ),
            ],
          ),
        ),
      ),
    );
  }
}
