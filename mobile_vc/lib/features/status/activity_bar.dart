import 'dart:math' as math;
import 'dart:ui' show lerpDouble;

import 'package:flutter/material.dart';
import 'package:flutter_svg/flutter_svg.dart';

class ActivityBar extends StatefulWidget {
  const ActivityBar({
    super.key,
    required this.phaseLabel,
    required this.toolLabel,
    required this.elapsedSeconds,
  });

  final String phaseLabel;
  final String toolLabel;
  final int elapsedSeconds;

  @override
  State<ActivityBar> createState() => _ActivityBarState();
}

class _ActivityBarState extends State<ActivityBar> with SingleTickerProviderStateMixin {
  late final AnimationController _controller = AnimationController(
    vsync: this,
    duration: const Duration(milliseconds: 1800),
  )..repeat(reverse: true);

  @override
  void dispose() {
    _controller.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final scheme = theme.colorScheme;

    return Container(
      padding: const EdgeInsets.fromLTRB(6, 10, 12, 10),
      decoration: BoxDecoration(
        color: scheme.surfaceContainerHighest,
        borderRadius: BorderRadius.circular(18),
        border: Border.all(color: scheme.outlineVariant),
      ),
      child: Row(
        children: [
          SizedBox(
            width: 34,
            height: 18,
            child: LayoutBuilder(
              builder: (context, constraints) {
                return _RollingOrange(
                  progress: _controller,
                  width: constraints.maxWidth,
                  iconSize: 16,
                );
              },
            ),
          ),
          const SizedBox(width: 4),
          Expanded(
            child: Text(
              widget.toolLabel.isEmpty ? widget.phaseLabel : '${widget.phaseLabel} · ${widget.toolLabel}',
              maxLines: 1,
              overflow: TextOverflow.ellipsis,
              style: theme.textTheme.bodyMedium?.copyWith(fontWeight: FontWeight.w600),
            ),
          ),
          const SizedBox(width: 10),
          Text(
            '${widget.elapsedSeconds}s',
            style: theme.textTheme.labelMedium?.copyWith(color: scheme.primary, fontWeight: FontWeight.w700),
          ),
        ],
      ),
    );
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
        const laneHeight = 18.0;
        final contactY = laneHeight - iconSize;
        final lift = math.sin(curvedT * math.pi) * iconSize * 0.1;
        final squash = math.sin(curvedT * math.pi);
        final angle = lerpDouble(-math.pi / 2, math.pi / 2, curvedT)!;
        final scaleX = 1 + squash * 0.05;
        final scaleY = 1 - squash * 0.05;
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
