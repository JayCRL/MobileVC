import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_markdown/flutter_markdown.dart';

import '../data/models/events.dart';
import '../data/models/session_models.dart';

class EventCard extends StatefulWidget {
  const EventCard({
    super.key,
    required this.item,
    this.onTap,
    this.mediaPreviewStates = const {},
    this.mediaPreviewKeyFor,
    this.onOpenAttachment,
    this.onRequestMediaPreview,
    this.onAnimatedBodyProgress,
    this.collapseThinkingByDefault = false,
  });

  final TimelineItem item;
  final VoidCallback? onTap;
  final Map<String, MediaPreviewState> mediaPreviewStates;
  final String Function(TimelineAttachment attachment)? mediaPreviewKeyFor;
  final ValueChanged<TimelineAttachment>? onOpenAttachment;
  final ValueChanged<TimelineAttachment>? onRequestMediaPreview;
  final VoidCallback? onAnimatedBodyProgress;
  final bool collapseThinkingByDefault;

  @override
  State<EventCard> createState() => _EventCardState();
}

class _EventCardState extends State<EventCard> {
  bool _selectionMode = false;
  late bool _thinkingCollapsed;
  bool _thinkingManuallyToggled = false;

  @override
  void initState() {
    super.initState();
    _thinkingCollapsed =
        widget.item.kind == 'thinking' && widget.collapseThinkingByDefault;
    _requestMissingAttachmentPreviews();
  }

  @override
  void didUpdateWidget(covariant EventCard oldWidget) {
    super.didUpdateWidget(oldWidget);
    if (oldWidget.item.id != widget.item.id) {
      _selectionMode = false;
      _thinkingManuallyToggled = false;
      _thinkingCollapsed =
          widget.item.kind == 'thinking' && widget.collapseThinkingByDefault;
    } else if (widget.item.kind == 'thinking' &&
        oldWidget.collapseThinkingByDefault !=
            widget.collapseThinkingByDefault &&
        !_thinkingManuallyToggled) {
      _thinkingCollapsed = widget.collapseThinkingByDefault;
    }
    _requestMissingAttachmentPreviews();
  }

  void _requestMissingAttachmentPreviews() {
    final requestPreview = widget.onRequestMediaPreview;
    if (requestPreview == null) {
      return;
    }
    WidgetsBinding.instance.addPostFrameCallback((_) {
      if (!mounted) {
        return;
      }
      for (final attachment in widget.item.attachments) {
        if (!attachment.isImage) {
          continue;
        }
        final key = _previewKeyFor(attachment);
        if (key.isEmpty || widget.mediaPreviewStates.containsKey(key)) {
          continue;
        }
        requestPreview(attachment);
      }
    });
  }

  String _previewKeyFor(TimelineAttachment attachment) {
    final resolver = widget.mediaPreviewKeyFor;
    if (resolver != null) {
      return resolver(attachment).trim();
    }
    final id = attachment.id.trim();
    return id.isNotEmpty ? id : attachment.path.trim();
  }

  @override
  Widget build(BuildContext context) {
    final scheme = Theme.of(context).colorScheme;
    final isDark = scheme.brightness == Brightness.dark;
    final style = _styleForKind(scheme, widget.item.kind);
    final compact = _isCompactKind(widget.item.kind);
    final isUser = widget.item.kind == 'user';
    final isMarkdown = widget.item.kind == 'markdown';
    final isCompaction = widget.item.kind == 'compaction';
    final isCodexToolGroup = widget.item.kind == 'codex_tool_group';
    final isThinking = widget.item.kind == 'thinking';

    if (isCompaction) {
      return _CompactionMarker(item: widget.item);
    }

    if (isCodexToolGroup) {
      return Align(
        alignment: Alignment.centerLeft,
        child: ConstrainedBox(
          constraints: const BoxConstraints(maxWidth: 760),
          child: _CodexToolGroupCard(item: widget.item, style: style),
        ),
      );
    }

    if (isThinking) {
      return Align(
        alignment: Alignment.centerLeft,
        child: ConstrainedBox(
          constraints: const BoxConstraints(maxWidth: 760),
          child: _wrapWithLongPress(
            context,
            _buildThinkingCard(context, style),
          ),
        ),
      );
    }

    if (isMarkdown) {
      final bubbleColor = Color.alphaBlend(
        scheme.primary.withValues(alpha: isDark ? 0.05 : 0.025),
        isDark ? scheme.surfaceContainerLow : scheme.surface,
      );
      final markdownChild = DecoratedBox(
        decoration: BoxDecoration(
          color: bubbleColor,
          borderRadius: BorderRadius.circular(20),
          border: Border.all(
            color: scheme.outlineVariant.withValues(alpha: isDark ? 0.24 : 0.5),
          ),
          boxShadow: [
            if (!isDark)
              BoxShadow(
                color: Colors.black.withValues(alpha: 0.04),
                blurRadius: 14,
                offset: const Offset(0, 6),
              ),
          ],
        ),
        child: Padding(
          padding: const EdgeInsets.fromLTRB(14, 12, 14, 12),
          child: Column(
            mainAxisSize: MainAxisSize.min,
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              if (widget.item.body.isNotEmpty)
                _buildMarkdownText(context, style),
              if (widget.item.attachments.isNotEmpty) ...[
                if (widget.item.body.isNotEmpty) const SizedBox(height: 10),
                _AttachmentStrip(
                  attachments: widget.item.attachments,
                  style: style,
                  previewStates: widget.mediaPreviewStates,
                  previewKeyFor: _previewKeyFor,
                  onOpenAttachment: widget.onOpenAttachment,
                ),
              ],
            ],
          ),
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
                  blurRadius: 4,
                  offset: const Offset(0, 1),
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
    final children = <Widget>[];
    if (widget.item.body.isNotEmpty) {
      children.add(
        _selectionMode
            ? SelectableText(
                widget.item.body,
                style: textStyle,
                textAlign: TextAlign.left,
                contextMenuBuilder: _buildEditableContextMenu,
              )
            : Text(
                widget.item.body,
                style: textStyle,
                textAlign: TextAlign.left,
              ),
      );
    }
    if (widget.item.attachments.isNotEmpty) {
      if (children.isNotEmpty) {
        children.add(const SizedBox(height: 10));
      }
      children.add(_AttachmentStrip(
        attachments: widget.item.attachments,
        style: style,
        previewStates: widget.mediaPreviewStates,
        previewKeyFor: _previewKeyFor,
        onOpenAttachment: widget.onOpenAttachment,
      ));
    }
    return Column(
      mainAxisSize: MainAxisSize.min,
      crossAxisAlignment: CrossAxisAlignment.start,
      children: children,
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
            onProgress: widget.onAnimatedBodyProgress,
          );
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
              if (widget.item.attachments.isNotEmpty) ...[
                const SizedBox(height: 8),
                _AttachmentStrip(
                  attachments: widget.item.attachments,
                  style: style,
                  previewStates: widget.mediaPreviewStates,
                  previewKeyFor: _previewKeyFor,
                  onOpenAttachment: widget.onOpenAttachment,
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

  Widget _buildThinkingCard(BuildContext context, _EventCardStyle style) {
    final scheme = Theme.of(context).colorScheme;
    return Material(
      color: Colors.transparent,
      child: InkWell(
        key: const ValueKey('thinkingToggle'),
        onTap: _selectionMode ? null : _toggleThinkingCollapsed,
        borderRadius: BorderRadius.circular(style.radius),
        child: Ink(
          decoration: BoxDecoration(
            color: style.background,
            borderRadius: BorderRadius.circular(style.radius),
            border: Border.all(color: style.border),
          ),
          child: Padding(
            padding: const EdgeInsets.fromLTRB(12, 10, 12, 10),
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Row(
                  children: [
                    _LeadingBadge(item: widget.item, style: style),
                    const SizedBox(width: 10),
                    Expanded(
                      child: Column(
                        crossAxisAlignment: CrossAxisAlignment.start,
                        children: [
                          Text(
                            widget.item.title.isEmpty
                                ? _titleForKind(widget.item.kind)
                                : widget.item.title,
                            style: Theme.of(context)
                                .textTheme
                                .labelLarge
                                ?.copyWith(
                                  fontWeight: FontWeight.w800,
                                  color: style.titleColor,
                                ),
                          ),
                          if (_thinkingCollapsed)
                            Text(
                              _collapsedThinkingPreview(widget.item.body),
                              maxLines: 1,
                              overflow: TextOverflow.ellipsis,
                              style: Theme.of(context)
                                  .textTheme
                                  .bodySmall
                                  ?.copyWith(color: style.subtitleColor),
                            ),
                        ],
                      ),
                    ),
                    Icon(
                      _thinkingCollapsed
                          ? Icons.expand_more_rounded
                          : Icons.expand_less_rounded,
                      color: scheme.onSurfaceVariant,
                    ),
                  ],
                ),
                if (!_thinkingCollapsed && widget.item.body.isNotEmpty) ...[
                  const SizedBox(height: 8),
                  _BodyContent(
                    item: widget.item,
                    style: style,
                    selectable: _selectionMode,
                    contextMenuBuilder: _buildEditableContextMenu,
                  ),
                ],
              ],
            ),
          ),
        ),
      ),
    );
  }

  void _toggleThinkingCollapsed() {
    setState(() {
      _thinkingManuallyToggled = true;
      _thinkingCollapsed = !_thinkingCollapsed;
    });
  }

  String _collapsedThinkingPreview(String body) {
    final text = body.trim().replaceAll(RegExp(r'\s+'), ' ');
    if (text.isEmpty) {
      return '已折叠';
    }
    final runeCount = text.runes.length;
    if (runeCount <= 48) {
      return text;
    }
    return '${String.fromCharCodes(text.runes.take(48))}...';
  }

  Widget _buildEditableContextMenu(
    BuildContext context,
    EditableTextState state,
  ) {
    final value = state.textEditingValue;
    final selection = value.selection;
    final hasSelection = selection.isValid && !selection.isCollapsed;
    final selectedText = hasSelection ? selection.textInside(value.text) : '';
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
    return kind == 'session' ||
        kind == 'system' ||
        kind == 'compaction' ||
        kind == 'thinking';
  }

  _EventCardStyle _styleForKind(ColorScheme scheme, String kind) {
    final isDark = scheme.brightness == Brightness.dark;
    final defaultLightBackground = Color.alphaBlend(
      scheme.primary.withValues(alpha: 0.018),
      scheme.surface,
    );
    final compactLightBackground = Color.alphaBlend(
      scheme.secondary.withValues(alpha: 0.026),
      scheme.surfaceContainerLow,
    );

    return switch (kind) {
      'user' => _EventCardStyle(
          background: scheme.primary,
          border: scheme.primary,
          titleColor: Colors.white,
          bodyColor: Colors.white,
          subtitleColor: Colors.white.withValues(alpha: 0.76),
          iconBackground: Colors.white.withValues(alpha: 0.14),
          iconColor: Colors.white,
          shadow: scheme.primary.withValues(alpha: isDark ? 0.12 : 0.18),
          radius: 20,
        ),
      'markdown' => _EventCardStyle(
          background: isDark ? const Color(0xFF1C1C1E) : defaultLightBackground,
          border: scheme.outlineVariant.withValues(alpha: isDark ? 0.18 : 0.5),
          titleColor: scheme.onSurface,
          bodyColor: scheme.onSurface,
          subtitleColor: scheme.onSurfaceVariant,
          iconBackground: scheme.primaryContainer,
          iconColor: scheme.primary,
          shadow: Colors.black.withValues(alpha: 0.04),
          radius: 20,
        ),
      'error' => _EventCardStyle(
          background: scheme.errorContainer.withValues(alpha: 0.72),
          border: scheme.error.withValues(alpha: 0.18),
          titleColor: scheme.onErrorContainer,
          bodyColor: scheme.onErrorContainer,
          subtitleColor: scheme.onErrorContainer.withValues(alpha: 0.74),
          iconBackground: scheme.error.withValues(alpha: 0.10),
          iconColor: scheme.error,
          shadow: scheme.error.withValues(alpha: 0.06),
          radius: 20,
        ),
      'terminal' || 'log' => _EventCardStyle(
          background: isDark ? const Color(0xFF1C1C1E) : defaultLightBackground,
          border: scheme.outlineVariant.withValues(alpha: isDark ? 0.18 : 0.5),
          titleColor: scheme.onSurface,
          bodyColor: scheme.onSurface,
          subtitleColor: scheme.onSurfaceVariant,
          iconBackground: scheme.surfaceContainerHighest,
          iconColor: scheme.primary,
          shadow: Colors.black.withValues(alpha: 0.04),
          radius: 20,
        ),
      'codex_tool_group' => _EventCardStyle(
          background: scheme.surfaceContainerLow,
          border: scheme.outlineVariant.withValues(alpha: 0.72),
          titleColor: scheme.onSurface,
          bodyColor: scheme.onSurfaceVariant,
          subtitleColor: scheme.onSurfaceVariant.withValues(alpha: 0.82),
          iconBackground: scheme.tertiaryContainer.withValues(alpha: 0.68),
          iconColor: scheme.onTertiaryContainer,
          shadow: Colors.transparent,
          radius: 16,
        ),
      'session' || 'system' => _EventCardStyle(
          background:
              isDark ? scheme.surfaceContainerLow : compactLightBackground,
          border: scheme.outlineVariant.withValues(alpha: isDark ? 0.18 : 0.42),
          titleColor: scheme.onSurfaceVariant,
          bodyColor: scheme.onSurfaceVariant,
          subtitleColor: scheme.onSurfaceVariant.withValues(alpha: 0.84),
          iconBackground: scheme.surfaceContainerHighest,
          iconColor: scheme.primary,
          shadow: Colors.transparent,
          radius: 16,
        ),
      'thinking' => _EventCardStyle(
          background: isDark
              ? const Color(0xFF2C2C2E)
              : Color.alphaBlend(
                  scheme.tertiary.withValues(alpha: 0.035),
                  scheme.surfaceContainerLow,
                ),
          border: scheme.outlineVariant.withValues(alpha: isDark ? 0.12 : 0.34),
          titleColor: scheme.onSurfaceVariant,
          bodyColor: scheme.onSurfaceVariant,
          subtitleColor: scheme.onSurfaceVariant.withValues(alpha: 0.6),
          iconBackground: scheme.tertiaryContainer,
          iconColor: scheme.tertiary,
          shadow: Colors.transparent,
          radius: 14,
        ),
      _ => _EventCardStyle(
          background: isDark ? const Color(0xFF1C1C1E) : defaultLightBackground,
          border: scheme.outlineVariant.withValues(alpha: isDark ? 0.18 : 0.5),
          titleColor: scheme.onSurface,
          bodyColor: scheme.onSurface,
          subtitleColor: scheme.onSurfaceVariant,
          iconBackground: scheme.surfaceContainerHighest,
          iconColor: scheme.primary,
          shadow: Colors.black.withValues(alpha: 0.04),
          radius: 20,
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
      'codex_tool_group' => 'Codex 原生操作',
      'session' || 'system' => '系统提示',
      'thinking' => '思考过程',
      _ => kind,
    };
  }

  String _time(DateTime value) {
    final h = value.hour.toString().padLeft(2, '0');
    final m = value.minute.toString().padLeft(2, '0');
    return '$h:$m';
  }
}

class _AttachmentStrip extends StatelessWidget {
  const _AttachmentStrip({
    required this.attachments,
    required this.style,
    required this.previewStates,
    required this.previewKeyFor,
    required this.onOpenAttachment,
  });

  final List<TimelineAttachment> attachments;
  final _EventCardStyle style;
  final Map<String, MediaPreviewState> previewStates;
  final String Function(TimelineAttachment attachment) previewKeyFor;
  final ValueChanged<TimelineAttachment>? onOpenAttachment;

  @override
  Widget build(BuildContext context) {
    return Wrap(
      spacing: 8,
      runSpacing: 8,
      children: [
        for (final attachment in attachments)
          _AttachmentChip(
            attachment: attachment,
            style: style,
            previewState: previewStates[previewKeyFor(attachment)],
            onOpen: onOpenAttachment == null
                ? null
                : () => onOpenAttachment!(attachment),
          ),
      ],
    );
  }
}

class _CodexToolGroupCard extends StatefulWidget {
  const _CodexToolGroupCard({
    required this.item,
    required this.style,
  });

  final TimelineItem item;
  final _EventCardStyle style;

  @override
  State<_CodexToolGroupCard> createState() => _CodexToolGroupCardState();
}

class _CodexToolGroupCardState extends State<_CodexToolGroupCard> {
  bool _expanded = false;

  @override
  Widget build(BuildContext context) {
    final scheme = Theme.of(context).colorScheme;
    final style = widget.style;
    final steps = widget.item.codexSteps
        .map((step) => step.trim())
        .where((name) => name.isNotEmpty)
        .toList();
    return Material(
      color: Colors.transparent,
      child: InkWell(
        key: const ValueKey('codexToolGroupToggle'),
        onTap: () => setState(() => _expanded = !_expanded),
        borderRadius: BorderRadius.circular(style.radius),
        child: Ink(
          decoration: BoxDecoration(
            color: style.background,
            borderRadius: BorderRadius.circular(style.radius),
            border: Border.all(color: style.border),
          ),
          child: Padding(
            padding: const EdgeInsets.fromLTRB(12, 10, 12, 10),
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Row(
                  children: [
                    Container(
                      width: 28,
                      height: 28,
                      decoration: BoxDecoration(
                        color: style.iconBackground,
                        shape: BoxShape.circle,
                      ),
                      child: Icon(
                        Icons.build_circle_outlined,
                        size: 17,
                        color: style.iconColor,
                      ),
                    ),
                    const SizedBox(width: 10),
                    Expanded(
                      child: Column(
                        crossAxisAlignment: CrossAxisAlignment.start,
                        children: [
                          Text(
                            widget.item.title.isEmpty
                                ? 'Codex 原生操作'
                                : widget.item.title,
                            style: Theme.of(context)
                                .textTheme
                                .labelLarge
                                ?.copyWith(
                                  fontWeight: FontWeight.w800,
                                  color: style.titleColor,
                                ),
                          ),
                          if (widget.item.status.trim().isNotEmpty)
                            Text(
                              widget.item.status.trim(),
                              style: Theme.of(context)
                                  .textTheme
                                  .bodySmall
                                  ?.copyWith(color: style.subtitleColor),
                              maxLines: 2,
                              overflow: TextOverflow.ellipsis,
                            ),
                        ],
                      ),
                    ),
                    Icon(
                      _expanded
                          ? Icons.expand_less_rounded
                          : Icons.expand_more_rounded,
                      color: scheme.onSurfaceVariant,
                    ),
                  ],
                ),
                if (steps.isNotEmpty) ...[
                  const SizedBox(height: 10),
                  Column(
                    key: const ValueKey('codexToolGroupSteps'),
                    crossAxisAlignment: CrossAxisAlignment.start,
                    children: [
                      for (final step in steps)
                        Padding(
                          padding: const EdgeInsets.only(bottom: 6),
                          child: Row(
                            crossAxisAlignment: CrossAxisAlignment.start,
                            children: [
                              Icon(
                                _stepIcon(step),
                                size: 15,
                                color: style.iconColor,
                              ),
                              const SizedBox(width: 8),
                              Expanded(
                                child: Text(
                                  step,
                                  maxLines: 2,
                                  overflow: TextOverflow.ellipsis,
                                  style: Theme.of(context)
                                      .textTheme
                                      .bodySmall
                                      ?.copyWith(
                                        color: style.bodyColor,
                                        height: 1.35,
                                      ),
                                ),
                              ),
                            ],
                          ),
                        ),
                    ],
                  ),
                ],
                AnimatedSwitcher(
                  duration: const Duration(milliseconds: 160),
                  child: !_expanded
                      ? const SizedBox.shrink()
                      : Padding(
                          key: const ValueKey('codexToolGroupDetail'),
                          padding: const EdgeInsets.only(top: 10),
                          child: _BodyContent(
                            item: widget.item.copyWith(kind: 'markdown'),
                            style: style,
                            selectable: true,
                          ),
                        ),
                ),
              ],
            ),
          ),
        ),
      ),
    );
  }

  IconData _stepIcon(String step) {
    if (step.contains('智能体')) {
      return Icons.smart_toy_outlined;
    }
    if (step.contains('读取') || step.contains('查看')) {
      return Icons.description_outlined;
    }
    if (step.contains('补丁')) {
      return Icons.data_object_rounded;
    }
    if (step.contains('命令')) {
      return Icons.terminal_rounded;
    }
    return Icons.play_circle_outline_rounded;
  }
}

class _AttachmentChip extends StatelessWidget {
  const _AttachmentChip({
    required this.attachment,
    required this.style,
    required this.previewState,
    required this.onOpen,
  });

  final TimelineAttachment attachment;
  final _EventCardStyle style;
  final MediaPreviewState? previewState;
  final VoidCallback? onOpen;

  @override
  Widget build(BuildContext context) {
    final scheme = Theme.of(context).colorScheme;
    final foreground = style.bodyColor;
    final surface = Color.alphaBlend(
      foreground.withValues(alpha: 0.10),
      style.background,
    );
    final border = foreground.withValues(alpha: 0.22);
    final chip = Container(
      key: ValueKey('timelineAttachment:${attachment.displayName}'),
      constraints: const BoxConstraints(maxWidth: 260),
      padding: const EdgeInsets.all(6),
      decoration: BoxDecoration(
        color: surface,
        borderRadius: BorderRadius.circular(14),
        border: Border.all(color: border),
      ),
      child: Row(
        mainAxisSize: MainAxisSize.min,
        children: [
          _AttachmentPreviewBox(
            attachment: attachment,
            previewState: previewState,
            foreground: foreground,
            fallbackBackground: scheme.surfaceContainerHighest,
          ),
          const SizedBox(width: 8),
          Flexible(
            child: Column(
              mainAxisSize: MainAxisSize.min,
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Text(
                  attachment.displayName,
                  maxLines: 1,
                  overflow: TextOverflow.ellipsis,
                  style: Theme.of(context).textTheme.labelMedium?.copyWith(
                        color: foreground,
                        fontWeight: FontWeight.w700,
                      ),
                ),
                const SizedBox(height: 2),
                Text(
                  _attachmentSubtitle(attachment, previewState),
                  maxLines: 1,
                  overflow: TextOverflow.ellipsis,
                  style: Theme.of(context).textTheme.labelSmall?.copyWith(
                        color: foreground.withValues(alpha: 0.76),
                      ),
                ),
                if (attachment.path.trim().isNotEmpty) ...[
                  const SizedBox(height: 2),
                  Text(
                    attachment.path.trim(),
                    maxLines: 1,
                    overflow: TextOverflow.ellipsis,
                    style: Theme.of(context).textTheme.labelSmall?.copyWith(
                          color: foreground.withValues(alpha: 0.62),
                        ),
                  ),
                ],
              ],
            ),
          ),
        ],
      ),
    );
    if (onOpen == null) {
      return chip;
    }
    return InkWell(
      onTap: onOpen,
      borderRadius: BorderRadius.circular(14),
      child: chip,
    );
  }

  String _attachmentSubtitle(
    TimelineAttachment attachment,
    MediaPreviewState? previewState,
  ) {
    final parts = <String>[];
    if (attachment.mimeType.trim().isNotEmpty) {
      parts.add(attachment.mimeType.trim());
    } else {
      parts.add(attachment.isImage ? 'image' : 'file');
    }
    if (attachment.size > 0) {
      parts.add(_formatBytes(attachment.size));
    }
    if (previewState?.loading == true) {
      parts.add('加载预览');
    } else if (previewState?.failed == true) {
      parts.add(previewState?.message.trim().isNotEmpty == true
          ? previewState!.message.trim()
          : '预览不可用');
    }
    return parts.join(' · ');
  }

  String _formatBytes(int value) {
    if (value >= 1024 * 1024) {
      return '${(value / (1024 * 1024)).toStringAsFixed(1)} MB';
    }
    if (value >= 1024) {
      return '${(value / 1024).toStringAsFixed(1)} KB';
    }
    return '$value B';
  }
}

class _AttachmentPreviewBox extends StatelessWidget {
  const _AttachmentPreviewBox({
    required this.attachment,
    required this.previewState,
    required this.foreground,
    required this.fallbackBackground,
  });

  final TimelineAttachment attachment;
  final MediaPreviewState? previewState;
  final Color foreground;
  final Color fallbackBackground;

  @override
  Widget build(BuildContext context) {
    final bytes = previewState?.bytes;
    if (attachment.isImage && previewState?.ok == true && bytes != null) {
      return ClipRRect(
        borderRadius: BorderRadius.circular(10),
        child: Image.memory(
          bytes,
          key: ValueKey('timelineAttachmentPreview:${attachment.displayName}'),
          width: 44,
          height: 44,
          fit: BoxFit.cover,
          errorBuilder: (context, error, stackTrace) => _iconBox(
            Icons.broken_image_outlined,
          ),
        ),
      );
    }
    if (previewState?.loading == true) {
      return SizedBox(
        width: 44,
        height: 44,
        child: Center(
          child: SizedBox(
            width: 18,
            height: 18,
            child: CircularProgressIndicator(
              strokeWidth: 2,
              color: foreground,
            ),
          ),
        ),
      );
    }
    return _iconBox(
      attachment.isImage ? Icons.image_outlined : Icons.attach_file_rounded,
    );
  }

  Widget _iconBox(IconData icon) {
    return Container(
      width: 44,
      height: 44,
      decoration: BoxDecoration(
        color: fallbackBackground.withValues(alpha: 0.42),
        borderRadius: BorderRadius.circular(10),
      ),
      child: Icon(icon, size: 22, color: foreground),
    );
  }
}

class _TypewriterMarkdown extends StatefulWidget {
  const _TypewriterMarkdown({
    required this.item,
    required this.style,
    this.plain = false,
    this.useSelectionArea = false,
    this.selectable = false,
    this.onProgress,
  });

  final TimelineItem item;
  final _EventCardStyle style;
  final bool plain;
  final bool useSelectionArea;
  final bool selectable;
  final VoidCallback? onProgress;

  @override
  State<_TypewriterMarkdown> createState() => _TypewriterMarkdownState();
}

class _TypewriterMarkdownState extends State<_TypewriterMarkdown> {
  static const Duration _typingTick = Duration(milliseconds: 48);
  static final Map<String, String> _revealedTextCache = <String, String>{};

  Timer? _timer;
  late String _visibleText;
  late String _lastBody;

  @override
  void initState() {
    super.initState();
    _lastBody = widget.item.body;
    final cached = _revealedPrefixFor(widget.item.id, widget.item.body);
    if (cached != null && cached.isNotEmpty) {
      _visibleText = cached;
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
      final sameItem = oldWidget.item.id == widget.item.id;
      _lastBody = widget.item.body;
      final cached = _revealedPrefixFor(widget.item.id, widget.item.body) ?? '';
      if (cached.isNotEmpty) {
        _visibleText = cached;
        if (sameItem &&
            widget.item.body.startsWith(previousBody) &&
            _visibleText.length < previousBody.length) {
          _visibleText = previousBody;
        }
      } else if (sameItem &&
          widget.item.body.startsWith(previousBody) &&
          _visibleText.isNotEmpty) {
        _visibleText = previousBody.length > widget.item.body.length
            ? widget.item.body
            : previousBody;
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
        attachments: widget.item.attachments,
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
    _timer = Timer.periodic(_typingTick, (timer) {
      if (!mounted) {
        timer.cancel();
        return;
      }
      final current = _visibleText.length;
      final remaining = target.length - current;
      final step = _typingStep(remaining);
      final next = (current + step).clamp(0, target.length);
      setState(() {
        _visibleText = target.substring(0, next);
        _revealedTextCache[widget.item.id] = _visibleText;
      });
      _notifyProgressIfNeeded();
      if (next >= target.length) {
        timer.cancel();
      }
    });
  }

  int _typingStep(int remaining) {
    if (remaining > 1200) {
      return 96;
    }
    if (remaining > 600) {
      return 64;
    }
    if (remaining > 240) {
      return 40;
    }
    if (remaining > 80) {
      return 24;
    }
    if (remaining > 24) {
      return 12;
    }
    return remaining > 6 ? 4 : 1;
  }

  void _notifyProgressIfNeeded() {
    final onProgress = widget.onProgress;
    if (onProgress == null) {
      return;
    }
    onProgress();
  }

  String _initialVisibleText(String body) {
    if (body.length <= 4) {
      return '';
    }
    return body.substring(0, 1);
  }

  String? _revealedPrefixFor(String itemId, String body) {
    final cached = _revealedTextCache[itemId];
    if (cached == null || cached.isEmpty) {
      return null;
    }
    if (!body.startsWith(cached)) {
      _revealedTextCache.remove(itemId);
      return null;
    }
    return cached;
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

class _CompactionMarker extends StatelessWidget {
  const _CompactionMarker({required this.item});

  final TimelineItem item;

  @override
  Widget build(BuildContext context) {
    final scheme = Theme.of(context).colorScheme;
    final status = item.status.trim().toLowerCase();
    final failed = status == 'failed';
    final loading = status == 'loading';
    final color = failed
        ? scheme.error
        : loading
            ? scheme.primary
            : scheme.onSurfaceVariant;
    final lineColor = color.withValues(alpha: failed ? 0.28 : 0.22);
    final label = switch (status) {
      'loading' => '压缩中',
      'failed' => '压缩失败',
      _ => '已压缩',
    };
    final detail = failed ? item.body.trim() : '';

    return Padding(
      padding: const EdgeInsets.symmetric(vertical: 6),
      child: Row(
        children: [
          Expanded(child: Divider(color: lineColor, height: 1)),
          Container(
            margin: const EdgeInsets.symmetric(horizontal: 12),
            padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 7),
            decoration: BoxDecoration(
              color: scheme.surfaceContainerLow,
              borderRadius: BorderRadius.circular(999),
              border: Border.all(color: lineColor),
            ),
            child: Row(
              mainAxisSize: MainAxisSize.min,
              children: [
                if (loading)
                  SizedBox(
                    width: 14,
                    height: 14,
                    child: CircularProgressIndicator(
                      strokeWidth: 1.8,
                      valueColor: AlwaysStoppedAnimation<Color>(color),
                    ),
                  )
                else
                  Icon(
                    failed
                        ? Icons.error_outline_rounded
                        : Icons.content_cut_rounded,
                    size: 16,
                    color: color,
                  ),
                const SizedBox(width: 8),
                Text(
                  label,
                  style: Theme.of(context).textTheme.labelMedium?.copyWith(
                        color: color,
                        fontWeight: FontWeight.w700,
                      ),
                ),
                if (detail.isNotEmpty) ...[
                  const SizedBox(width: 8),
                  ConstrainedBox(
                    constraints: const BoxConstraints(maxWidth: 220),
                    child: Text(
                      detail,
                      maxLines: 1,
                      overflow: TextOverflow.ellipsis,
                      style: Theme.of(context).textTheme.bodySmall?.copyWith(
                            color: scheme.onSurfaceVariant,
                          ),
                    ),
                  ),
                ],
              ],
            ),
          ),
          Expanded(child: Divider(color: lineColor, height: 1)),
        ],
      ),
    );
  }
}
