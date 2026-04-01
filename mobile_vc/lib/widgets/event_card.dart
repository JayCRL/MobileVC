import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter_markdown/flutter_markdown.dart';

import '../data/models/events.dart';

class EventCard extends StatelessWidget {
  const EventCard({
    super.key,
    required this.item,
    this.onTap,
  });

  final TimelineItem item;
  final VoidCallback? onTap;

  @override
  Widget build(BuildContext context) {
    final scheme = Theme.of(context).colorScheme;
    final style = _styleForKind(scheme, item.kind);
    final compact = _isCompactKind(item.kind);
    final isUser = item.kind == 'user';
    final isMarkdown = item.kind == 'markdown';

    if (isMarkdown) {
      final bubbleColor = Color.alphaBlend(
        scheme.primary.withValues(alpha: 0.04),
        scheme.surfaceContainerLowest,
      );
      return Align(
        alignment: Alignment.centerLeft,
        child: ConstrainedBox(
          constraints: const BoxConstraints(maxWidth: 760),
          child: DecoratedBox(
            decoration: BoxDecoration(
              color: bubbleColor,
              borderRadius: BorderRadius.circular(20),
              border: Border.all(
                color: scheme.outlineVariant.withValues(alpha: 0.45),
              ),
            ),
            child: Padding(
              padding: const EdgeInsets.fromLTRB(14, 12, 14, 12),
              child: _buildMarkdownText(context, style),
            ),
          ),
        ),
      );
    }

    return Align(
      alignment: isUser ? Alignment.centerRight : Alignment.centerLeft,
      child: ConstrainedBox(
        constraints: BoxConstraints(maxWidth: isUser ? 320 : 760),
        child: Material(
          color: Colors.transparent,
          child: InkWell(
            onTap: onTap,
            borderRadius: BorderRadius.circular(style.radius),
            child: Ink(
              decoration: BoxDecoration(
                color: style.background,
                borderRadius: BorderRadius.circular(style.radius),
                border: Border.all(color: style.border),
                boxShadow: compact
                    ? null
                    : [
                        BoxShadow(
                          color: style.shadow,
                          blurRadius: isMarkdown ? 18 : 20,
                          offset: const Offset(0, 10),
                        ),
                      ],
              ),
              child: Padding(
                padding: EdgeInsets.symmetric(
                  horizontal: isUser ? 14 : (compact ? 12 : 14),
                  vertical: isUser ? 12 : (compact ? 10 : 12),
                ),
                child: isUser
                    ? _buildUserBubble(context, style)
                    : _buildDefaultCard(context, style),
              ),
            ),
          ),
        ),
      ),
    );
  }

  Widget _buildUserBubble(BuildContext context, _EventCardStyle style) {
    return SelectableText(
      item.body,
      style: Theme.of(context).textTheme.bodyMedium?.copyWith(
            height: 1.5,
            color: style.bodyColor,
            fontWeight: FontWeight.w500,
          ),
      textAlign: TextAlign.left,
    );
  }

  Widget _buildMarkdownText(BuildContext context, _EventCardStyle style) {
    return _TypewriterMarkdown(
      item: item,
      style: style.copyWith(
        background: Colors.transparent,
        border: Colors.transparent,
        shadow: Colors.transparent,
        iconBackground: Colors.transparent,
      ),
      plain: true,
    );
  }

  Widget _buildDefaultCard(BuildContext context, _EventCardStyle style) {
    return Row(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        _LeadingBadge(item: item, style: style),
        const SizedBox(width: 12),
        Expanded(
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Row(
                children: [
                  Expanded(
                    child: Text(
                      item.title.isEmpty
                          ? _titleForKind(item.kind)
                          : item.title,
                      style: Theme.of(context).textTheme.labelLarge?.copyWith(
                            fontWeight: FontWeight.w800,
                            color: style.titleColor,
                          ),
                    ),
                  ),
                  if (!_isCompactKind(item.kind)) ...[
                    const SizedBox(width: 8),
                    Text(
                      _time(item.timestamp),
                      style: Theme.of(context)
                          .textTheme
                          .bodySmall
                          ?.copyWith(color: style.subtitleColor),
                    ),
                  ],
                ],
              ),
              if (item.body.isNotEmpty) ...[
                const SizedBox(height: 8),
                _BodyContent(item: item, style: style),
              ],
              if ((item.context?.path ?? '').isNotEmpty) ...[
                const SizedBox(height: 8),
                Text(
                  item.context!.path,
                  style: Theme.of(context)
                      .textTheme
                      .bodySmall
                      ?.copyWith(color: style.subtitleColor),
                  maxLines: 2,
                  overflow: TextOverflow.ellipsis,
                ),
              ],
            ],
          ),
        ),
      ],
    );
  }

  bool _isCompactKind(String kind) {
    return kind == 'session' || kind == 'system';
  }

  _EventCardStyle _styleForKind(ColorScheme scheme, String kind) {
    return switch (kind) {
      'user' => _EventCardStyle(
          background: scheme.primary,
          border: scheme.primary,
          titleColor: scheme.onPrimary,
          bodyColor: scheme.onPrimary,
          subtitleColor: scheme.onPrimary.withValues(alpha: 0.76),
          iconBackground: scheme.onPrimary.withValues(alpha: 0.14),
          iconColor: scheme.onPrimary,
          shadow: scheme.primary.withValues(alpha: 0.18),
          radius: 22,
        ),
      'markdown' => _EventCardStyle(
          background: Colors.white,
          border: scheme.outlineVariant.withValues(alpha: 0.55),
          titleColor: scheme.onSurface,
          bodyColor: const Color(0xFF0F172A),
          subtitleColor: scheme.onSurfaceVariant,
          iconBackground: scheme.primaryContainer,
          iconColor: scheme.primary,
          shadow: Colors.black.withValues(alpha: 0.05),
          radius: 24,
        ),
      'error' => _EventCardStyle(
          background: scheme.errorContainer.withValues(alpha: 0.72),
          border: scheme.error.withValues(alpha: 0.18),
          titleColor: scheme.onErrorContainer,
          bodyColor: scheme.onErrorContainer,
          subtitleColor: scheme.onErrorContainer.withValues(alpha: 0.74),
          iconBackground: scheme.error.withValues(alpha: 0.10),
          iconColor: scheme.error,
          shadow: scheme.error.withValues(alpha: 0.10),
          radius: 22,
        ),
      'terminal' || 'log' => _EventCardStyle(
          background: Colors.white,
          border: scheme.outlineVariant.withValues(alpha: 0.7),
          titleColor: scheme.onSurface,
          bodyColor: scheme.onSurface,
          subtitleColor: scheme.onSurfaceVariant,
          iconBackground: scheme.surfaceContainerHighest,
          iconColor: scheme.primary,
          shadow: scheme.shadow.withValues(alpha: 0.06),
          radius: 22,
        ),
      'session' || 'system' => _EventCardStyle(
          background: scheme.surfaceContainerLow,
          border: scheme.outlineVariant.withValues(alpha: 0.7),
          titleColor: scheme.onSurfaceVariant,
          bodyColor: scheme.onSurfaceVariant,
          subtitleColor: scheme.onSurfaceVariant.withValues(alpha: 0.84),
          iconBackground: scheme.surfaceContainerHighest,
          iconColor: scheme.primary,
          shadow: Colors.transparent,
          radius: 18,
        ),
      _ => _EventCardStyle(
          background: Colors.white,
          border: scheme.outlineVariant.withValues(alpha: 0.7),
          titleColor: scheme.onSurface,
          bodyColor: scheme.onSurface,
          subtitleColor: scheme.onSurfaceVariant,
          iconBackground: scheme.surfaceContainerHighest,
          iconColor: scheme.primary,
          shadow: scheme.shadow.withValues(alpha: 0.06),
          radius: 22,
        ),
    };
  }

  String _titleForKind(String kind) {
    return switch (kind) {
      'error' => '错误',
      'file_diff' => '文件改动',
      'fs_read_result' => '文件',
      'runtime_info_result' => '运行时信息',
      'terminal' => '终端输出',
      'session' || 'system' => '系统提示',
      _ => kind,
    };
  }

  String _time(DateTime value) {
    final h = value.hour.toString().padLeft(2, '0');
    final m = value.minute.toString().padLeft(2, '0');
    return '$h:$m';
  }
}

class _TypewriterMarkdown extends StatefulWidget {
  const _TypewriterMarkdown({
    required this.item,
    required this.style,
    this.plain = false,
  });

  final TimelineItem item;
  final _EventCardStyle style;
  final bool plain;

  @override
  State<_TypewriterMarkdown> createState() => _TypewriterMarkdownState();
}

class _TypewriterMarkdownState extends State<_TypewriterMarkdown> {
  static final Map<String, String> _revealedTextCache = <String, String>{};

  Timer? _timer;
  late String _visibleText;
  late String _lastBody;

  @override
  void initState() {
    super.initState();
    _lastBody = widget.item.body;
    final cached = _revealedTextCache[widget.item.id];
    if (cached != null && cached.isNotEmpty) {
      _visibleText =
          cached.length > widget.item.body.length ? widget.item.body : cached;
    } else {
      _visibleText = _initialVisibleText(widget.item.body);
      _revealedTextCache[widget.item.id] = _visibleText;
    }
    _scheduleTypingIfNeeded();
  }

  @override
  void didUpdateWidget(covariant _TypewriterMarkdown oldWidget) {
    super.didUpdateWidget(oldWidget);
    if (oldWidget.item.id != widget.item.id ||
        oldWidget.item.body != widget.item.body) {
      _timer?.cancel();
      final previousBody = _lastBody;
      _lastBody = widget.item.body;
      final cached = _revealedTextCache[widget.item.id] ?? '';
      if (cached.isNotEmpty) {
        _visibleText =
            cached.length > widget.item.body.length ? widget.item.body : cached;
      } else if (widget.item.body.startsWith(previousBody) &&
          _visibleText.isNotEmpty) {
        if (_visibleText.length > widget.item.body.length) {
          _visibleText = widget.item.body;
        }
      } else {
        _visibleText = _initialVisibleText(widget.item.body);
      }
      _revealedTextCache[widget.item.id] = _visibleText;
      _scheduleTypingIfNeeded();
    }
  }

  @override
  void dispose() {
    _timer?.cancel();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    return _BodyContent(
      item: TimelineItem(
        id: widget.item.id,
        kind: widget.item.kind,
        timestamp: widget.item.timestamp,
        title: widget.item.title,
        body: _visibleText,
        stream: widget.item.stream,
        status: widget.item.status,
        meta: widget.item.meta,
        context: widget.item.context,
      ),
      style: widget.style,
      plain: widget.plain,
    );
  }

  void _scheduleTypingIfNeeded() {
    final target = widget.item.body;
    if (_visibleText.length >= target.length) {
      _visibleText = target;
      _revealedTextCache[widget.item.id] = target;
      return;
    }
    _timer = Timer.periodic(const Duration(milliseconds: 16), (timer) {
      if (!mounted) {
        timer.cancel();
        return;
      }
      final current = _visibleText.length;
      final remaining = target.length - current;
      final step = remaining > 80
          ? 8
          : remaining > 40
              ? 5
              : remaining > 20
                  ? 3
                  : 1;
      final next = (current + step).clamp(0, target.length);
      setState(() {
        _visibleText = target.substring(0, next);
        _revealedTextCache[widget.item.id] = _visibleText;
      });
      if (next >= target.length) {
        timer.cancel();
      }
    });
  }

  String _initialVisibleText(String body) {
    if (body.length <= 4) {
      return '';
    }
    return body.substring(0, 1);
  }
}

class _BodyContent extends StatelessWidget {
  const _BodyContent({
    required this.item,
    required this.style,
    this.plain = false,
  });

  final TimelineItem item;
  final _EventCardStyle style;
  final bool plain;

  @override
  Widget build(BuildContext context) {
    if (item.kind == 'markdown') {
      return MarkdownBody(
        data: item.body,
        selectable: true,
        styleSheet: MarkdownStyleSheet.fromTheme(Theme.of(context)).copyWith(
          p: Theme.of(context).textTheme.bodyMedium?.copyWith(
                height: 1.62,
                color: style.bodyColor,
              ),
          listBullet: Theme.of(context).textTheme.bodyMedium?.copyWith(
                height: 1.62,
                color: style.bodyColor,
              ),
          h1: Theme.of(context).textTheme.titleLarge?.copyWith(
                color: style.bodyColor,
                fontWeight: FontWeight.w800,
              ),
          h2: Theme.of(context).textTheme.titleMedium?.copyWith(
                color: style.bodyColor,
                fontWeight: FontWeight.w800,
              ),
          h3: Theme.of(context).textTheme.titleSmall?.copyWith(
                color: style.bodyColor,
                fontWeight: FontWeight.w700,
              ),
          code: TextStyle(
            color: style.bodyColor,
            backgroundColor: plain
                ? Theme.of(context)
                    .colorScheme
                    .surfaceContainerHighest
                    .withValues(alpha: 0.46)
                : Theme.of(context)
                    .colorScheme
                    .surfaceContainerHighest
                    .withValues(alpha: 0.8),
            fontFamily: 'monospace',
          ),
          codeblockDecoration: BoxDecoration(
            color: plain
                ? Theme.of(context)
                    .colorScheme
                    .surfaceContainerHighest
                    .withValues(alpha: 0.38)
                : Theme.of(context)
                    .colorScheme
                    .surfaceContainerHighest
                    .withValues(alpha: 0.55),
            borderRadius: BorderRadius.circular(14),
          ),
          blockquote: Theme.of(context)
              .textTheme
              .bodyMedium
              ?.copyWith(color: style.subtitleColor, height: 1.6),
          blockquoteDecoration: BoxDecoration(
            border: Border(left: BorderSide(color: style.border, width: 3)),
          ),
        ),
      );
    }
    return SelectableText(
      item.body,
      style: Theme.of(context).textTheme.bodyMedium?.copyWith(
            height: 1.55,
            color: style.bodyColor,
          ),
    );
  }
}

class _LeadingBadge extends StatelessWidget {
  const _LeadingBadge({required this.item, required this.style});

  final TimelineItem item;
  final _EventCardStyle style;

  @override
  Widget build(BuildContext context) {
    final icon = switch (item.kind) {
      'error' => Icons.error_outline,
      'file_diff' => Icons.compare_arrows,
      'fs_read_result' => Icons.description_outlined,
      'runtime_info_result' => Icons.info_outline,
      'terminal' || 'log' => Icons.terminal,
      'session' || 'system' => Icons.info_outline,
      _ => Icons.notes,
    };
    return Container(
      width: 36,
      height: 36,
      decoration: BoxDecoration(
        color: style.iconBackground,
        borderRadius: BorderRadius.circular(12),
      ),
      child: Icon(icon, size: 18, color: style.iconColor),
    );
  }
}

class _EventCardStyle {
  const _EventCardStyle({
    required this.background,
    required this.border,
    required this.titleColor,
    required this.bodyColor,
    required this.subtitleColor,
    required this.iconBackground,
    required this.iconColor,
    required this.shadow,
    required this.radius,
  });

  final Color background;
  final Color border;
  final Color titleColor;
  final Color bodyColor;
  final Color subtitleColor;
  final Color iconBackground;
  final Color iconColor;
  final Color shadow;
  final double radius;

  _EventCardStyle copyWith({
    Color? background,
    Color? border,
    Color? titleColor,
    Color? bodyColor,
    Color? subtitleColor,
    Color? iconBackground,
    Color? iconColor,
    Color? shadow,
    double? radius,
  }) {
    return _EventCardStyle(
      background: background ?? this.background,
      border: border ?? this.border,
      titleColor: titleColor ?? this.titleColor,
      bodyColor: bodyColor ?? this.bodyColor,
      subtitleColor: subtitleColor ?? this.subtitleColor,
      iconBackground: iconBackground ?? this.iconBackground,
      iconColor: iconColor ?? this.iconColor,
      shadow: shadow ?? this.shadow,
      radius: radius ?? this.radius,
    );
  }
}
