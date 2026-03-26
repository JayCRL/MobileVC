import 'dart:async';
import 'dart:math' as math;

import 'package:flutter/material.dart';

class ActivityRunnerBar extends StatefulWidget {
  const ActivityRunnerBar({
    super.key,
    required this.visible,
    required this.label,
    required this.startedAt,
    required this.elapsedSeconds,
  });

  final bool visible;
  final String label;
  final DateTime? startedAt;
  final int elapsedSeconds;

  @override
  State<ActivityRunnerBar> createState() => _ActivityRunnerBarState();
}

class _ActivityRunnerBarState extends State<ActivityRunnerBar>
    with SingleTickerProviderStateMixin {
  late final AnimationController _controller = AnimationController(
    vsync: this,
    duration: const Duration(milliseconds: 1800),
  );
  Timer? _ticker;
  int _displayElapsedSeconds = 0;

  @override
  void initState() {
    super.initState();
    _syncAnimation();
    _syncTicker();
  }

  @override
  void didUpdateWidget(covariant ActivityRunnerBar oldWidget) {
    super.didUpdateWidget(oldWidget);
    if (oldWidget.visible != widget.visible) {
      _syncAnimation();
    }
    if (oldWidget.visible != widget.visible ||
        oldWidget.startedAt != widget.startedAt ||
        oldWidget.elapsedSeconds != widget.elapsedSeconds) {
      _syncTicker();
    }
  }

  @override
  void dispose() {
    _ticker?.cancel();
    _controller.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    if (!widget.visible) {
      return const SizedBox.shrink();
    }
    final scheme = Theme.of(context).colorScheme;
    return Container(
      margin: const EdgeInsets.fromLTRB(16, 0, 16, 10),
      padding: const EdgeInsets.symmetric(horizontal: 14, vertical: 10),
      decoration: BoxDecoration(
        color: scheme.surfaceContainerHighest,
        borderRadius: BorderRadius.circular(18),
        border: Border.all(color: scheme.outlineVariant),
      ),
      child: Row(
        children: [
          AnimatedBuilder(
            animation: _controller,
            builder: (context, child) {
              final dx = math.sin(_controller.value * math.pi) * 8;
              final rotate = (_controller.value - 0.5) * 0.18;
              return Transform.translate(
                offset: Offset(dx, 0),
                child: Transform.rotate(angle: rotate, child: child),
              );
            },
            child: const Text('🦀', style: TextStyle(fontSize: 18)),
          ),
          const SizedBox(width: 12),
          Expanded(
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Text(
                  'Claude 正在运行中',
                  style: Theme.of(context)
                      .textTheme
                      .labelLarge
                      ?.copyWith(fontWeight: FontWeight.w700),
                ),
                if (widget.label.trim().isNotEmpty)
                  Text(
                    '调用工具 · ${widget.label.trim()}',
                    maxLines: 1,
                    overflow: TextOverflow.ellipsis,
                    style: Theme.of(context)
                        .textTheme
                        .bodySmall
                        ?.copyWith(color: scheme.onSurfaceVariant),
                  ),
              ],
            ),
          ),
          const SizedBox(width: 12),
          Text(
            '${_displayElapsedSeconds}s',
            style: Theme.of(context)
                .textTheme
                .labelMedium
                ?.copyWith(color: scheme.primary, fontWeight: FontWeight.w700),
          ),
        ],
      ),
    );
  }

  void _syncAnimation() {
    if (widget.visible) {
      if (!_controller.isAnimating) {
        _controller.repeat(reverse: true);
      }
      return;
    }
    _controller.stop();
    _controller.value = 0;
  }

  void _syncTicker() {
    _ticker?.cancel();
    _updateElapsedSeconds();
    if (!widget.visible || widget.startedAt == null) {
      return;
    }
    _ticker = Timer.periodic(const Duration(seconds: 1), (_) {
      if (!mounted) {
        return;
      }
      _updateElapsedSeconds();
    });
  }

  void _updateElapsedSeconds() {
    final startedAt = widget.startedAt;
    final nextValue = widget.visible && startedAt != null
        ? DateTime.now().difference(startedAt).inSeconds
        : widget.elapsedSeconds;
    if (_displayElapsedSeconds == nextValue) {
      return;
    }
    setState(() {
      _displayElapsedSeconds = nextValue;
    });
  }
}
