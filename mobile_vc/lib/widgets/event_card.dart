import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_markdown/flutter_markdown.dart';

import '../data/models/events.dart';

class EventCard extends StatefulWidget {
  const EventCard({
    super.key,
    required this.item,
    this.onTap,
  });

  final TimelineItem item;
  final VoidCallback? onTap;

  @override
  State<EventCard> createState() => _EventCardState();
}

class _EventCardState extends State<EventCard> {
  bool _selectionMode = false;

  @override
  void didUpdateWidget(covariant EventCard oldWidget) {
    super.didUpdateWidget(oldWidget);
    if (oldWidget.item.id != widget.item.id) {
      _selectionMode = false;
    }
  }

  @override
  Widget build(BuildContext context) {
    final scheme = Theme.of(context).colorScheme;
    final style = _styleForKind(scheme, widget.item.kind);
    final compact = _isCompactKind(widget.item.kind);
    final isUser = widget.item.kind == 'user';
    final isMarkdown = widget.item.kind == 'markdown';

    if (isMarkdown) {
      final bubbleColor = Color.alphaBlend(
        scheme.primary.withValues(alpha: 0.04),
        scheme.surfaceContainerLowest,
      );
      final markdownChild = DecoratedBox(
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
      );
      return Align(
        alignment: Alignment.centerLeft,
        child: ConstrainedBox(
          constraints: const BoxConstraints(maxWidth: 760),
          child: _wrapWithLongPress(context, markdownChild),
        ),
      );
    }

    final bubble = Ink(
      decoration: BoxDecoration(
        color: style.background,
        borderRadius: BorderRadius.circular(style.radius),
        border: Border.all(
          color: style.border,
        ),
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
    );

    final tappable = Material(
      color: Colors.transparent,
      child: widget.onTap == null
          ? bubble
          : InkWell(
              onTap: widget.onTap,
              borderRadius: BorderRadius.circular(style.radius),
              child: bubble,
            ),
    );

    return Align(
      alignment: isUser ? Alignment.centerRight : Alignment.centerLeft,
      child: ConstrainedBox(
        constraints: BoxConstraints(maxWidth: isUser ? 320 : 760),
        child: _wrapWithLongPress(context, tappable),
      ),
    );
  }

  Widget _wrapWithLongPress(BuildContext context, Widget child) {
    if (widget.item.body.trim().isEmpty) {
      return child;
    }
    if (_selectionMode) {
      return child;
    }
    return GestureDetector(
      behavior: HitTestBehavior.opaque,
      onLongPressStart: _handleLongPressStart,
      child: child,
    );
  }

  Future<void> _handleLongPressStart(LongPressStartDetails details) async {
    HapticFeedback.mediumImpact();
    final overlay =
        Overlay.of(context).context.findRenderObject() as RenderBox?;
    if (overlay == null) {
      return;
    }
    final position = RelativeRect.fromRect(
      Rect.fromCenter(
        center: details.globalPosition,
        width: 1,
        height: 1,
      ),
      Offset.zero & overlay.size,
    );
    final scheme = Theme.of(context).colorScheme;
    final selected = await showMenu<String>(
      context: context,
      position: position,
      elevation: 8,
      shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(14)),
      items: [
        PopupMenuItem<String>(
          value: 'copy',
          child: Row(
            children: [
              Icon(Icons.copy_rounded, size: 18, color: scheme.primary),
              const SizedBox(width: 10),
              const Text('复制'),
            ],
          ),
        ),
        PopupMenuItem<String>(
          value: 'select',
          child: Row(
            children: [
              Icon(Icons.text_fields_rounded, size: 18, color: scheme.primary),
              const SizedBox(width: 10),
              const Text('选择文字'),
            ],
          ),
        ),
      ],
    );
    if (!mounted || selected == null) {
      return;
    }
    switch (selected) {
      case 'copy':
        await Clipboard.setData(ClipboardData(text: widget.item.body));
        if (!mounted) return;
        ScaffoldMessenger.of(context)
          ..hideCurrentSnackBar()
          ..showSnackBar(
            const SnackBar(
              content: Text('已复制'),
              duration: Duration(milliseconds: 1200),
              behavior: SnackBarBehavior.floating,
            ),
          );
        break;
      case 'select':
        setState(() => _selectionMode = true);
        break;
    }
  }

  Widget _buildUserBubble(BuildContext context, _EventCardStyle style) {
    final textStyle = Theme.of(context).textTheme.bodyMedium?.copyWith(
          height: 1.5,
          color: style.bodyColor,
          fontWeight: FontWeight.w500,
        );
    if (_selectionMode) {
      return SelectableText(
        widget.item.body,
        style: textStyle,
        textAlign: TextAlign.left,
        contextMenuBuilder: _buildEditableContextMenu,
      );
    }
    return Text(
      widget.item.body,
      style: textStyle,
      textAlign: TextAlign.left,
    );
  }

  Widget _buildMarkdownText(BuildContext context, _EventCardStyle style) {
    final plainStyle = style.copyWith(
      background: Colors.transparent,
      border: Colors.transparent,
      shadow: Colors.transparent,
      iconBackground: Colors.transparent,
    );
    final inner = !widget.item.animateBody
        ? _BodyContent(
            item: widget.item,
            style: plainStyle,
            plain: true,
            useSelectionArea: _selectionMode,
            selectable: _selectionMode,
          )
        : _TypewriterMarkdown(
            item: widget.item,
            style: plainStyle,
            plain: true,
            useSelectionArea: _selectionMode,
            selectable: _selectionMode,
          );
    if (!_selectionMode) {
      return inner;
    }
    return SelectionArea(
      contextMenuBuilder: _buildSelectionAreaContextMenu,
      child: inner,
    );
  }

  Widget _buildDefaultCard(BuildContext context, _EventCardStyle style) {
    return Row(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        _LeadingBadge(item: widget.item, style: style),
        const SizedBox(width: 12),
        Expanded(
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Row(
                children: [
                  Expanded(
                    child: Text(
                      widget.item.title.isEmpty
                          ? _titleForKind(widget.item.kind)
                          : widget.item.title,
                      style: Theme.of(context).textTheme.labelLarge?.copyWith(
                            fontWeight: FontWeight.w800,
                            color: style.titleColor,
                          ),
                    ),
                  ),
                  if (!_isCompactKind(widget.item.kind)) ...[
                    const SizedBox(width: 8),
                    Text(
                      _time(widget.item.timestamp),
                      style: Theme.of(context)
                          .textTheme
                          .bodySmall
                          ?.copyWith(color: style.subtitleColor),
                    ),
                  ],
                ],
              ),
              if (widget.item.body.isNotEmpty) ...[
                const SizedBox(height: 8),
                _BodyContent(
                  item: widget.item,
                  style: style,
                  selectable: _selectionMode,
                  contextMenuBuilder: _buildEditableContextMenu,
                ),
              ],
              if ((widget.item.context?.path ?? '').isNotEmpty) ...[
                const SizedBox(height: 8),
                Text(
                  widget.item.context!.path,
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

  Widget _buildEditableContextMenu(
    BuildContext context,
    EditableTextState state,
  ) {
    final value = state.textEditingValue;
    final selection = value.selection;
    final hasSelection = selection.isValid && !selection.isCollapsed;
    final selectedText =
        hasSelection ? selection.textInside(value.text) : '';
    final fullText = widget.item.body;

    final items = <ContextMenuButtonItem>[];
    if (hasSelection) {
      items.add(ContextMenuButtonItem(
        label: '复制选中',
        onPressed: () => _copyAndDismiss(state, selectedText),
      ));
    }
    items.add(ContextMenuButtonItem(
      label: '复制全部',
      onPressed: () => _copyAndDismiss(state, fullText),
    ));
    if (!hasSelection) {
      items.add(ContextMenuButtonItem(
        label: '全选',
        onPressed: () {
          state.selectAll(SelectionChangedCause.toolbar);
        },
      ));
    }

    return AdaptiveTextSelectionToolbar.buttonItems(
      anchors: state.contextMenuAnchors,
      buttonItems: items,
    );
  }

  Widget _buildSelectionAreaContextMenu(
    BuildContext context,
    SelectableRegionState state,
  ) {
    final fullText = widget.item.body;
    // Reuse system items (复制选中 / 全选) and prepend our 复制全部.
    final systemItems =
        List<ContextMenuButtonItem>.from(state.contextMenuButtonItems);
    final items = <ContextMenuButtonItem>[
      ContextMenuButtonItem(
        label: '复制全部',
        onPressed: () {
          Clipboard.setData(ClipboardData(text: fullText));
          ContextMenuController.removeAny();
          state.hideToolbar();
          _showCopiedSnack();
        },
      ),
      ...systemItems.map(
        (item) => ContextMenuButtonItem(
          type: item.type,
          label: item.label,
          onPressed: () {
            item.onPressed?.call();
            if (item.type == ContextMenuButtonType.copy) {
              _showCopiedSnack();
            }
          },
        ),
      ),
    ];

    return AdaptiveTextSelectionToolbar.buttonItems(
      anchors: state.contextMenuAnchors,
      buttonItems: items,
    );
  }

  void _copyAndDismiss(EditableTextState state, String text) {
    Clipboard.setData(ClipboardData(text: text));
    state.hideToolbar();
    ContextMenuController.removeAny();
    _showCopiedSnack();
  }

  void _showCopiedSnack() {
    if (!mounted) return;
    ScaffoldMessenger.of(context)
      ..hideCurrentSnackBar()
      ..showSnackBar(
        const SnackBar(
          content: Text('已复制'),
          duration: Duration(milliseconds: 1200),
          behavior: SnackBarBehavior.floating,
        ),
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
          background: scheme.surfaceContainerLowest,
          border: scheme.outlineVariant.withValues(alpha: 0.55),
          titleColor: scheme.onSurface,
          bodyColor: scheme.onSurface,
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
          background: scheme.surfaceContainerLowest,
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
          background: scheme.surfaceContainerLowest,
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
    this.useSelectionArea = false,
    this.selectable = false,
  });

  final TimelineItem item;
  final _EventCardStyle style;
  final bool plain;
  final bool useSelectionArea;
  final bool selectable;

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
      useSelectionArea: widget.useSelectionArea,
      selectable: widget.selectable,
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
    this.useSelectionArea = false,
    this.selectable = false,
    this.contextMenuBuilder,
  });

  final TimelineItem item;
  final _EventCardStyle style;
  final bool plain;
  final bool useSelectionArea;
  final bool selectable;
  final EditableTextContextMenuBuilder? contextMenuBuilder;

  @override
  Widget build(BuildContext context) {
    if (item.kind == 'markdown') {
      return MarkdownBody(
        data: item.body,
        selectable: selectable && !useSelectionArea,
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
    final textStyle = Theme.of(context).textTheme.bodyMedium?.copyWith(
          height: 1.55,
          color: style.bodyColor,
        );
    if (selectable) {
      return SelectableText(
        item.body,
        style: textStyle,
        contextMenuBuilder: contextMenuBuilder,
      );
    }
    return Text(
      item.body,
      style: textStyle,
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
