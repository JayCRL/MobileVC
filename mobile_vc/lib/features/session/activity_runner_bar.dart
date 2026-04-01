import 'dart:async';
import 'dart:math' as math;
import 'dart:ui' show lerpDouble;

import 'package:flutter/material.dart';
import 'package:flutter_svg/flutter_svg.dart';

class ActivityRunnerBar extends StatefulWidget {
  const ActivityRunnerBar({
    super.key,
    required this.visible,
    required this.title,
    required this.detail,
    required this.startedAt,
    required this.elapsedSeconds,
    required this.animated,
    required this.showElapsed,
  });

  final bool visible;
  final String title;
  final String detail;
  final DateTime? startedAt;
  final int elapsedSeconds;
  final bool animated;
  final bool showElapsed;

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
    if (oldWidget.visible != widget.visible ||
        oldWidget.animated != widget.animated) {
      _syncAnimation();
    }
    if (oldWidget.visible != widget.visible ||
        oldWidget.startedAt != widget.startedAt ||
        oldWidget.elapsedSeconds != widget.elapsedSeconds ||
        oldWidget.showElapsed != widget.showElapsed) {
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
      padding: const EdgeInsets.fromLTRB(6, 10, 14, 10),
      decoration: BoxDecoration(
        color: scheme.surfaceContainerHighest,
        borderRadius: BorderRadius.circular(18),
        border: Border.all(color: scheme.outlineVariant),
      ),
      child: Row(
        children: [
          SizedBox(
            width: 32,
            height: 22,
            child: LayoutBuilder(
              builder: (context, constraints) {
                return _RollingOrange(
                  progress: _controller,
                  width: constraints.maxWidth,
                  iconSize: 22,
                );
              },
            ),
          ),
          const SizedBox(width: 6),
          Expanded(
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Text(
                  widget.title,
                  style: Theme.of(context)
                      .textTheme
                      .labelLarge
                      ?.copyWith(fontWeight: FontWeight.w700),
                ),
                if (widget.detail.trim().isNotEmpty)
                  Text(
                    widget.detail.trim(),
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
          if (widget.showElapsed)
            Text(
              '${_displayElapsedSeconds}s',
              style: Theme.of(context).textTheme.labelMedium?.copyWith(
                  color: scheme.primary, fontWeight: FontWeight.w700),
            ),
        ],
      ),
    );
  }

  void _syncAnimation() {
    if (widget.visible && widget.animated) {
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
    if (!widget.visible || !widget.showElapsed || widget.startedAt == null) {
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
    final nextValue = widget.visible && widget.showElapsed && startedAt != null
        ? DateTime.now().difference(startedAt).inSeconds
        : 0;
    if (_displayElapsedSeconds == nextValue) {
      return;
    }
    setState(() {
      _displayElapsedSeconds = nextValue;
    });
  }
}

class _RollingOrange extends StatelessWidget {
  const _RollingOrange({
    required this.progress,
    required this.width,
    required this.iconSize,
  });

  final Animation<double> progress;
  final double width;
  final double iconSize;

  @override
  Widget build(BuildContext context) {
    return AnimatedBuilder(
      animation: progress,
      builder: (context, child) {
        final travel = math.max(0.0, width - iconSize);
        final curvedT = Curves.easeInOut.transform(progress.value);
        final dx = travel * curvedT;
        const laneHeight = 22.0;
        final contactY = laneHeight - iconSize;
        final lift = math.sin(curvedT * math.pi) * iconSize * 0.12;
        final squash = math.sin(curvedT * math.pi);
        final angle = lerpDouble(-math.pi / 2, math.pi / 2, curvedT)!;
        final scaleX = 1 + squash * 0.06;
        final scaleY = 1 - squash * 0.06;
        return Transform.translate(
          offset: Offset(dx, contactY - lift),
          child: Transform.rotate(
            angle: angle,
            alignment: Alignment.center,
            child: Transform.scale(
              scaleX: scaleX,
              scaleY: scaleY,
              alignment: Alignment.bottomCenter,
              child: child,
            ),
          ),
        );
      },
      child: SvgPicture.asset(
        'assets/icons/orange_loader.svg',
        width: iconSize,
        height: iconSize,
      ),
    );
  }
}
