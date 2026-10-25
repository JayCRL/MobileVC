import 'package:flutter/material.dart';

import '../../app/theme.dart';
import '../../data/models/session_models.dart';
import '../permissions/permission_mode_options.dart';

class CommandInputBar extends StatefulWidget {
  const CommandInputBar({
    super.key,
    required this.awaitInput,
    required this.isBusy,
    required this.canStop,
    required this.canCompact,
    required this.isCompacting,
    required this.compactStatusLabel,
    required this.contextWindowUsage,
    required this.onOpenContextWindowUsage,
    required this.hasPendingReview,
    required this.fastMode,
    required this.permissionMode,
    required this.onSubmit,
    required this.onAttachImage,
    required this.onStop,
    required this.onCompact,
    required this.onOpenSessions,
    required this.onOpenRuntimeInfo,
    required this.onOpenLogs,
    required this.onOpenSkills,
    required this.onOpenMemory,
    required this.onOpenPermissions,
    required this.onOpenModels,
    required this.onPermissionModeChanged,
    required this.codexTargetMode,
    required this.onCodexTargetModeChanged,
    required this.showClaudeMode,
    required this.currentEngine,
    required this.configuredEngine,
    required this.modelSummary,
    required this.permissionRuleSummary,
    required this.shouldShowPermissionChoices,
    required this.shouldShowReviewChoices,
    required this.shouldShowPlanChoices,
    required this.isSessionLoading,
    this.canSendToContinuedSameSession = false,
    this.isExternallyLocked = false,
    this.externalLockedHint = '',
  });

  final bool awaitInput;
  final bool isBusy;
  final bool canStop;
  final bool canCompact;
  final bool isCompacting;
  final String compactStatusLabel;
  final ContextWindowUsage contextWindowUsage;
  final VoidCallback onOpenContextWindowUsage;
  final bool hasPendingReview;
  final bool fastMode;
  final String permissionMode;
  final void Function(String text, List<ChatImageAttachment> imageAttachments)
      onSubmit;
  final Future<ChatImageAttachment?> Function() onAttachImage;
  final VoidCallback onStop;
  final VoidCallback onCompact;
  final VoidCallback onOpenSessions;
  final VoidCallback onOpenRuntimeInfo;
  final VoidCallback onOpenLogs;
  final VoidCallback onOpenSkills;
  final VoidCallback onOpenMemory;
  final VoidCallback onOpenPermissions;
  final VoidCallback onOpenModels;
  final ValueChanged<String> onPermissionModeChanged;
  final bool codexTargetMode;
  final ValueChanged<bool> onCodexTargetModeChanged;
  final bool showClaudeMode;
  final String currentEngine;
  final String configuredEngine;
  final String modelSummary;
  final String permissionRuleSummary;
  final bool shouldShowPermissionChoices;
  final bool shouldShowReviewChoices;
  final bool shouldShowPlanChoices;
  final bool isSessionLoading;
  final bool canSendToContinuedSameSession;
  final bool isExternallyLocked;
  final String externalLockedHint;

  @override
  State<CommandInputBar> createState() => _CommandInputBarState();
}

class _CommandInputBarState extends State<CommandInputBar> {
  static const double _inputActionButtonSize = 36;
  static const double _inputActionIconSize = 18;

  final TextEditingController _controller = TextEditingController();
  final FocusNode _focusNode = FocusNode();
  final List<ChatImageAttachment> _imageAttachments = [];
  bool _pickingImage = false;
  bool _debouncedCanStop = false;

  @override
  void initState() {
    super.initState();
    _debouncedCanStop = widget.canStop;
  }

  bool get _inputLocked =>
      widget.isExternallyLocked ||
      widget.isSessionLoading ||
      widget.shouldShowPermissionChoices ||
      widget.shouldShowReviewChoices ||
      widget.shouldShowPlanChoices;

  bool _canSubmitDraft(String text) =>
      (text.trim().isNotEmpty || _imageAttachments.isNotEmpty) &&
      !_inputLocked &&
      (!widget.isBusy ||
          widget.awaitInput ||
          widget.canSendToContinuedSameSession);

  bool _shouldShowStopAction(String text) =>
      !widget.isExternallyLocked &&
      !widget.isSessionLoading &&
      _debouncedCanStop &&
      !_canSubmitDraft(text);

  String get _lockedHintText {
    if (widget.isExternallyLocked) {
      return widget.externalLockedHint.trim().isEmpty
          ? '当前为只读观察模式'
          : widget.externalLockedHint.trim();
    }
    if (widget.isSessionLoading) {
      return '会话切换中...';
    }
    if (widget.shouldShowPermissionChoices) {
      return '请先在上方确认授权';
    }
    if (widget.shouldShowReviewChoices) {
      return '请先在上方完成审核';
    }
    if (widget.shouldShowPlanChoices) {
      return '请先在上方完成计划选择';
    }
    return '';
  }

  @override
  void didUpdateWidget(covariant CommandInputBar oldWidget) {
    super.didUpdateWidget(oldWidget);
    final oldLocked = oldWidget.shouldShowPermissionChoices ||
        oldWidget.shouldShowReviewChoices ||
        oldWidget.shouldShowPlanChoices;
    if (_inputLocked && !oldLocked) {
      _focusNode.unfocus();
    }

    if (oldWidget.canStop != widget.canStop) {
      if (widget.canStop) {
        Future.delayed(const Duration(milliseconds: 150), () {
          if (mounted && widget.canStop) {
            setState(() {
              _debouncedCanStop = true;
            });
          }
        });
      } else {
        if (_debouncedCanStop) {
          setState(() {
            _debouncedCanStop = false;
          });
        }
      }
    }
  }

  @override
  void dispose() {
    _controller.dispose();
    _focusNode.dispose();
    super.dispose();
  }

  void _submit() {
    if (_inputLocked) {
      _focusNode.unfocus();
      return;
    }

    final text = _controller.text;
    final normalized = text.trim();
    if (normalized.isEmpty && _imageAttachments.isEmpty) {
      return;
    }
    final keepKeyboard = _shouldKeepKeyboard(normalized);
    final imageAttachments = List<ChatImageAttachment>.unmodifiable(
      _imageAttachments,
    );
    _controller.clear();
    if (_imageAttachments.isNotEmpty) {
      setState(() => _imageAttachments.clear());
    }
    if (!keepKeyboard) {
      _focusNode.unfocus();
    }
    widget.onSubmit(text, imageAttachments);
  }

  Future<void> _attachImage() async {
    if (_inputLocked || _pickingImage) {
      return;
    }
    setState(() => _pickingImage = true);
    try {
      final attachment = await widget.onAttachImage();
      if (!mounted || attachment == null) {
        return;
      }
      setState(() => _imageAttachments.add(attachment));
      _focusNode.requestFocus();
    } finally {
      if (mounted) {
        setState(() => _pickingImage = false);
      }
    }
  }

  void _removeImageAttachment(int index) {
    if (index < 0 || index >= _imageAttachments.length) {
      return;
    }
    setState(() => _imageAttachments.removeAt(index));
  }

  bool _shouldKeepKeyboard(String value) {
    final lower = value.trim().toLowerCase();
    return lower == 'claude' ||
        lower.startsWith('claude ') ||
        lower == 'codex' ||
        lower.startsWith('codex ') ||
        lower == 'gemini' ||
        lower.startsWith('gemini ');
  }

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final scheme = theme.colorScheme;
    final isLight = theme.brightness == Brightness.light;
    final engineLabel =
        _engineLabel(widget.currentEngine, widget.showClaudeMode);
    final isCodex = widget.currentEngine.trim().toLowerCase() == 'codex';
    final permissionEngine = widget.configuredEngine.trim().isEmpty
        ? widget.currentEngine
        : widget.configuredEngine;
    final permissionMode = normalizePermissionModeForEngine(
      widget.permissionMode,
      permissionEngine,
    );
    final permissionModeOptions = permissionModeOptionsForEngine(
      permissionEngine,
    );
    final compactChipLabel = widget.isCompacting
        ? (widget.compactStatusLabel.trim().isEmpty
            ? '压缩中'
            : widget.compactStatusLabel.trim())
        : '压缩';
    final hintText = _inputLocked
        ? _lockedHintText
        : widget.awaitInput
            ? (widget.showClaudeMode ? '回复 $engineLabel' : '继续输入')
            : widget.hasPendingReview
                ? '先处理待审核 diff，再继续'
                : widget.isBusy
                    ? (widget.showClaudeMode
                        ? '$engineLabel 处理中…'
                        : 'Shell 运行中')
                    : (widget.showClaudeMode ? '给 $engineLabel 发送消息' : '输入命令');
    final railColor = isLight
        ? Color.alphaBlend(
            scheme.primary.withValues(alpha: 0.025),
            scheme.surfaceContainerLow,
          )
        : scheme.surfaceContainerLowest.withValues(alpha: 0.88);
    final inputColor = isLight
        ? scheme.surface
        : scheme.surfaceContainerHighest.withValues(alpha: 0.72);
    final dockBorderColor = scheme.outlineVariant.withValues(
      alpha: isLight ? 0.62 : 0.36,
    );

    return SafeArea(
      top: false,
      child: Padding(
        padding: const EdgeInsets.fromLTRB(10, 6, 10, 10),
        child: ClipRRect(
          borderRadius: BorderRadius.circular(IOSTokens.radiusInput),
          child: Container(
            padding: const EdgeInsets.fromLTRB(10, 10, 10, 10),
            decoration: BoxDecoration(
              color: scheme.surface.withValues(alpha: isLight ? 0.94 : 0.72),
              borderRadius: BorderRadius.circular(IOSTokens.radiusInput),
              border: Border.all(color: dockBorderColor),
              boxShadow: [
                if (isLight)
                  BoxShadow(
                    color: Colors.black.withValues(alpha: 0.08),
                    blurRadius: 24,
                    offset: const Offset(0, 12),
                  ),
              ],
            ),
            child: Column(
              mainAxisSize: MainAxisSize.min,
              children: [
                LayoutBuilder(
                  builder: (context, constraints) {
                    final compactRail = constraints.maxWidth < 390;
                    return Container(
                      padding: const EdgeInsets.fromLTRB(8, 8, 8, 8),
                      decoration: BoxDecoration(
                        color: railColor,
                        borderRadius: BorderRadius.circular(24),
                        border: Border.all(
                          color: scheme.outlineVariant
                              .withValues(alpha: isLight ? 0.46 : 0.26),
                        ),
                      ),
                      child: Row(
                        children: [
                          Expanded(
                            child: SingleChildScrollView(
                              scrollDirection: Axis.horizontal,
                              child: Row(
                                children: [
                                  _ToolChip(
                                    icon: Icons.history,
                                    label: '会话',
                                    onPressed: widget.onOpenSessions,
                                  ),
                                  const SizedBox(width: 8),
                                  if (widget.canCompact ||
                                      widget.isCompacting) ...[
                                    _ToolChip(
                                      icon: widget.isCompacting
                                          ? Icons.hourglass_top_rounded
                                          : Icons.content_cut_rounded,
                                      label: compactChipLabel,
                                      onPressed: widget.isCompacting
                                          ? null
                                          : widget.onCompact,
                                      highlighted: widget.isCompacting,
                                      showSpinner: widget.isCompacting,
                                    ),
                                    const SizedBox(width: 8),
                                  ],
                                  _ToolChip(
                                    icon: Icons.terminal,
                                    label: '日志',
                                    onPressed: widget.onOpenLogs,
                                  ),
                                  const SizedBox(width: 8),
                                  if (isCodex) ...[
                                    _ToolChip(
                                      key: const ValueKey(
                                        'codex-target-tool-chip',
                                      ),
                                      icon: Icons.track_changes_outlined,
                                      label: '目标',
                                      onPressed: () =>
                                          widget.onCodexTargetModeChanged(
                                        !widget.codexTargetMode,
                                      ),
                                      highlighted: widget.codexTargetMode,
                                    ),
                                    const SizedBox(width: 8),
                                  ],
                                  _ToolChip(
                                    icon: Icons.extension_outlined,
                                    label: 'Skill',
                                    onPressed: widget.onOpenSkills,
                                  ),
                                  const SizedBox(width: 8),
                                  _ToolChip(
                                    icon: Icons.psychology_alt_outlined,
                                    label: 'Memory',
                                    onPressed: widget.onOpenMemory,
                                  ),
                                  const SizedBox(width: 8),
                                  _ToolChip(
                                    icon: Icons.verified_user_outlined,
                                    label:
                                        '权限 · ${widget.permissionRuleSummary}',
                                    onPressed: widget.onOpenPermissions,
                                  ),
                                  const SizedBox(width: 8),
                                  _ToolChip(
                                    key: const ValueKey(
                                      'command-bar-model-button',
                                    ),
                                    icon: Icons.model_training_outlined,
                                    label: '模型 · ${widget.modelSummary}',
                                    onPressed: widget.onOpenModels,
                                  ),
                                ],
                              ),
                            ),
                          ),
                          SizedBox(width: compactRail ? 6 : 10),
                          _ContextWindowUsageButton(
                            usage: widget.contextWindowUsage,
                            onPressed: widget.onOpenContextWindowUsage,
                          ),
                          const SizedBox(width: 8),
                          _PermissionModeIconButton(
                            value: permissionMode,
                            options: permissionModeOptions,
                            inputColor: inputColor,
                            onSelected: widget.onPermissionModeChanged,
                          ),
                        ],
                      ),
                    );
                  },
                ),
                const SizedBox(height: 10),
                Container(
                  constraints: const BoxConstraints(minHeight: 56),
                  decoration: BoxDecoration(
                    gradient: LinearGradient(
                      colors: [
                        inputColor,
                        isLight
                            ? Color.alphaBlend(
                                scheme.secondary.withValues(alpha: 0.025),
                                scheme.surfaceContainerLowest,
                              )
                            : scheme.surface.withValues(alpha: 0.94),
                      ],
                      begin: Alignment.topLeft,
                      end: Alignment.bottomRight,
                    ),
                    borderRadius: BorderRadius.circular(28),
                    border: Border.all(
                      color: _inputLocked
                          ? scheme.outlineVariant
                              .withValues(alpha: isLight ? 0.42 : 0.24)
                          : scheme.primary
                              .withValues(alpha: isLight ? 0.20 : 0.12),
                    ),
                    boxShadow: [
                      if (isLight)
                        BoxShadow(
                          color: scheme.primary.withValues(alpha: 0.06),
                          blurRadius: 18,
                          offset: const Offset(0, 8),
                        ),
                    ],
                  ),
                  child: Column(
                    mainAxisSize: MainAxisSize.min,
                    children: [
                      if (_imageAttachments.isNotEmpty)
                        _AttachmentPreviewStrip(
                          attachments: _imageAttachments,
                          onRemove:
                              _inputLocked ? null : _removeImageAttachment,
                        ),
                      Row(
                        crossAxisAlignment: CrossAxisAlignment.end,
                        children: [
                          Expanded(
                            child: TextField(
                              controller: _controller,
                              focusNode: _focusNode,
                              enabled: !_inputLocked,
                              readOnly: _inputLocked,
                              canRequestFocus: !_inputLocked,
                              minLines: 1,
                              maxLines: 6,
                              keyboardType: TextInputType.multiline,
                              textInputAction: TextInputAction.send,
                              autocorrect: true,
                              enableSuggestions: true,
                              onTap: _inputLocked
                                  ? () => _focusNode.unfocus()
                                  : null,
                              onSubmitted:
                                  _inputLocked ? null : (_) => _submit(),
                              textAlignVertical: TextAlignVertical.center,
                              style: Theme.of(context)
                                  .textTheme
                                  .bodyMedium
                                  ?.copyWith(
                                    height: 1.45,
                                  ),
                              decoration: InputDecoration(
                                hintText: hintText,
                                hintStyle: Theme.of(context)
                                    .textTheme
                                    .bodyMedium
                                    ?.copyWith(
                                      color: scheme.onSurfaceVariant,
                                    ),
                                filled: false,
                                isCollapsed: false,
                                contentPadding: const EdgeInsets.fromLTRB(
                                  18,
                                  14,
                                  8,
                                  14,
                                ),
                                border: InputBorder.none,
                                enabledBorder: InputBorder.none,
                                focusedBorder: InputBorder.none,
                                disabledBorder: InputBorder.none,
                              ),
                            ),
                          ),
                          Padding(
                            padding: const EdgeInsets.fromLTRB(0, 0, 2, 6),
                            child: SizedBox(
                              key:
                                  const ValueKey('command-image-action-button'),
                              width: _inputActionButtonSize,
                              height: _inputActionButtonSize,
                              child: IconButton.filledTonal(
                                onPressed: _inputLocked || _pickingImage
                                    ? null
                                    : _attachImage,
                                tooltip: '添加图片',
                                style: IconButton.styleFrom(
                                  fixedSize: const Size.square(
                                    _inputActionButtonSize,
                                  ),
                                  minimumSize: const Size.square(
                                    _inputActionButtonSize,
                                  ),
                                  padding: EdgeInsets.zero,
                                  tapTargetSize:
                                      MaterialTapTargetSize.shrinkWrap,
                                  visualDensity: VisualDensity.compact,
                                ),
                                icon: _pickingImage
                                    ? SizedBox(
                                        width: _inputActionIconSize,
                                        height: _inputActionIconSize,
                                        child: CircularProgressIndicator(
                                          strokeWidth: 2,
                                          color: scheme.primary,
                                        ),
                                      )
                                    : const Icon(
                                        Icons.image_outlined,
                                        size: _inputActionIconSize,
                                      ),
                              ),
                            ),
                          ),
                          Padding(
                            padding: const EdgeInsets.fromLTRB(0, 0, 4, 6),
                            child: SizedBox(
                              key: const ValueKey('command-send-action-button'),
                              width: _inputActionButtonSize,
                              height: _inputActionButtonSize,
                              child: ValueListenableBuilder<TextEditingValue>(
                                valueListenable: _controller,
                                builder: (context, value, _) {
                                  final showStopAction =
                                      _shouldShowStopAction(value.text);
                                  return FilledButton(
                                    onPressed: showStopAction
                                        ? widget.onStop
                                        : (_inputLocked ? null : _submit),
                                    style: FilledButton.styleFrom(
                                      elevation: 0,
                                      backgroundColor: _inputLocked
                                          ? scheme.surfaceContainerHighest
                                          : showStopAction
                                              ? scheme.error
                                              : scheme.primary,
                                      foregroundColor: _inputLocked
                                          ? scheme.onSurfaceVariant
                                          : showStopAction
                                              ? scheme.onError
                                              : scheme.onPrimary,
                                      fixedSize: const Size.square(
                                        _inputActionButtonSize,
                                      ),
                                      padding: EdgeInsets.zero,
                                      minimumSize: const Size.square(
                                        _inputActionButtonSize,
                                      ),
                                      tapTargetSize:
                                          MaterialTapTargetSize.shrinkWrap,
                                      visualDensity: VisualDensity.compact,
                                      shape: RoundedRectangleBorder(
                                        borderRadius:
                                            BorderRadius.circular(999),
                                      ),
                                    ),
                                    child: Icon(
                                      showStopAction
                                          ? Icons.stop_rounded
                                          : Icons.arrow_upward,
                                      size: _inputActionIconSize,
                                    ),
                                  );
                                },
                              ),
                            ),
                          ),
                        ],
                      ),
                    ],
                  ),
                ),
              ],
            ),
          ),
        ),
      ),
    );
  }
}

class _ContextWindowUsageButton extends StatelessWidget {
  const _ContextWindowUsageButton({
    required this.usage,
    required this.onPressed,
  });

  final ContextWindowUsage usage;
  final VoidCallback onPressed;

  @override
  Widget build(BuildContext context) {
    final scheme = Theme.of(context).colorScheme;
    final percent = usage.percentUsed;
    final progressColor = percent > 90
        ? scheme.error
        : percent >= 70
            ? const Color(0xFFF59E0B)
            : scheme.primary;
    final tooltipMessage = usage.isAvailable
        ? '上下文已使用 $percent%，剩余 ${formatTokenCountCompact(usage.tokensRemaining)}'
        : '上下文用量暂不可用';
    final backgroundColor = usage.isAvailable
        ? progressColor.withValues(alpha: 0.10)
        : scheme.surfaceContainerHigh.withValues(alpha: 0.78);
    final borderColor = usage.isAvailable
        ? progressColor.withValues(alpha: 0.24)
        : scheme.outlineVariant.withValues(alpha: 0.34);

    return Tooltip(
      message: tooltipMessage,
      waitDuration: const Duration(milliseconds: 250),
      child: Semantics(
        button: true,
        label: tooltipMessage,
        child: Material(
          color: Colors.transparent,
          child: InkWell(
            key: const ValueKey('context-window-button'),
            onTap: onPressed,
            borderRadius: BorderRadius.circular(999),
            child: Ink(
              width: 40,
              height: 40,
              decoration: BoxDecoration(
                color: backgroundColor,
                shape: BoxShape.circle,
                border: Border.all(color: borderColor),
              ),
              child: Center(
                child: SizedBox(
                  width: 24,
                  height: 24,
                  child: Stack(
                    alignment: Alignment.center,
                    children: [
                      if (usage.isAvailable)
                        CircularProgressIndicator(
                          value: usage.fractionUsed,
                          strokeWidth: 2.4,
                          backgroundColor: scheme.surfaceContainerHighest
                              .withValues(alpha: 0.9),
                          valueColor:
                              AlwaysStoppedAnimation<Color>(progressColor),
                        )
                      else
                        DecoratedBox(
                          decoration: BoxDecoration(
                            shape: BoxShape.circle,
                            border: Border.all(
                              color:
                                  scheme.outlineVariant.withValues(alpha: 0.58),
                              width: 1.9,
                            ),
                          ),
                        ),
                      if (usage.isAvailable)
                        Text(
                          '$percent%',
                          style: TextStyle(
                            fontSize: 9,
                            fontWeight: FontWeight.w700,
                            color: progressColor,
                            height: 1,
                          ),
                        )
                      else
                        Container(
                          width: 5,
                          height: 5,
                          decoration: BoxDecoration(
                            shape: BoxShape.circle,
                            color:
                                scheme.outlineVariant.withValues(alpha: 0.82),
                          ),
                        ),
                    ],
                  ),
                ),
              ),
            ),
          ),
        ),
      ),
    );
  }
}

class _PermissionModeIconButton extends StatelessWidget {
  const _PermissionModeIconButton({
    required this.value,
    required this.options,
    required this.inputColor,
    required this.onSelected,
  });

  final String value;
  final List<PermissionModeOption> options;
  final Color inputColor;
  final ValueChanged<String> onSelected;

  @override
  Widget build(BuildContext context) {
    final scheme = Theme.of(context).colorScheme;
    final selected = _selectedOption;
    final tooltip = '权限：${selected.label}';

    return Tooltip(
      message: tooltip,
      child: Semantics(
        button: true,
        label: tooltip,
        child: PopupMenuButton<String>(
          key: const ValueKey('permission-mode-icon-button'),
          initialValue: value,
          tooltip: tooltip,
          onSelected: onSelected,
          itemBuilder: (context) => [
            for (final option in options)
              PopupMenuItem<String>(
                value: option.value,
                child: Row(
                  children: [
                    Icon(
                      option.value == value
                          ? Icons.check_circle_rounded
                          : _permissionModeIcon(option.value),
                      size: 18,
                      color: option.value == value
                          ? scheme.primary
                          : scheme.onSurfaceVariant,
                    ),
                    const SizedBox(width: 10),
                    Text(option.label),
                  ],
                ),
              ),
          ],
          child: DecoratedBox(
            decoration: BoxDecoration(
              color: inputColor,
              borderRadius: BorderRadius.circular(999),
              border: Border.all(
                color: scheme.outlineVariant.withValues(alpha: 0.4),
              ),
            ),
            child: SizedBox(
              width: 44,
              height: 40,
              child: Center(
                child: Icon(
                  _permissionModeIcon(value),
                  size: 20,
                  color: scheme.onSurface,
                ),
              ),
            ),
          ),
        ),
      ),
    );
  }

  PermissionModeOption get _selectedOption {
    for (final option in options) {
      if (option.value == value) {
        return option;
      }
    }
    return options.first;
  }
}

IconData _permissionModeIcon(String value) {
  switch (value) {
    case 'bypassPermissions':
      return Icons.bolt_rounded;
    case 'auto':
      return Icons.rate_review_outlined;
    case 'config':
      return Icons.tune_rounded;
    case 'default':
    default:
      return Icons.rule_rounded;
  }
}

String _engineLabel(String currentEngine, bool showClaudeMode) {
  switch (currentEngine.trim().toLowerCase()) {
    case 'codex':
      return 'Codex';
    case 'claude':
      return 'Claude';
    case 'shell':
      return 'Shell';
    default:
      return showClaudeMode ? 'Claude' : 'Shell';
  }
}

class _AttachmentPreviewStrip extends StatelessWidget {
  const _AttachmentPreviewStrip({
    required this.attachments,
    required this.onRemove,
  });

  final List<ChatImageAttachment> attachments;
  final void Function(int index)? onRemove;

  @override
  Widget build(BuildContext context) {
    return Align(
      alignment: Alignment.centerLeft,
      child: SingleChildScrollView(
        scrollDirection: Axis.horizontal,
        padding: const EdgeInsets.fromLTRB(10, 10, 10, 2),
        child: Row(
          children: [
            for (var index = 0; index < attachments.length; index++) ...[
              _AttachmentPreviewChip(
                attachment: attachments[index],
                onRemove: onRemove == null ? null : () => onRemove!(index),
              ),
              if (index != attachments.length - 1) const SizedBox(width: 8),
            ],
          ],
        ),
      ),
    );
  }
}

class _AttachmentPreviewChip extends StatelessWidget {
  const _AttachmentPreviewChip({
    required this.attachment,
    required this.onRemove,
  });

  final ChatImageAttachment attachment;
  final VoidCallback? onRemove;

  @override
  Widget build(BuildContext context) {
    final scheme = Theme.of(context).colorScheme;
    return Container(
      key: ValueKey('imageAttachmentPreview:${attachment.name}'),
      width: 150,
      height: 56,
      decoration: BoxDecoration(
        color: scheme.surfaceContainerHigh.withValues(alpha: 0.92),
        borderRadius: BorderRadius.circular(18),
        border: Border.all(
          color: scheme.outlineVariant.withValues(alpha: 0.5),
        ),
      ),
      clipBehavior: Clip.antiAlias,
      child: Row(
        children: [
          AspectRatio(
            aspectRatio: 1,
            child: Image.memory(
              attachment.bytes,
              fit: BoxFit.cover,
              errorBuilder: (context, error, stackTrace) => Container(
                color: scheme.surfaceContainerHighest,
                child: Icon(
                  Icons.broken_image_outlined,
                  size: 20,
                  color: scheme.onSurfaceVariant,
                ),
              ),
            ),
          ),
          Expanded(
            child: Padding(
              padding: const EdgeInsets.symmetric(horizontal: 8),
              child: Text(
                attachment.name,
                maxLines: 2,
                overflow: TextOverflow.ellipsis,
                style: Theme.of(context).textTheme.labelSmall?.copyWith(
                      color: scheme.onSurface,
                      fontWeight: FontWeight.w600,
                      height: 1.15,
                    ),
              ),
            ),
          ),
          SizedBox(
            width: 30,
            height: 56,
            child: IconButton(
              onPressed: onRemove,
              tooltip: '移除图片',
              padding: EdgeInsets.zero,
              icon: const Icon(Icons.close_rounded, size: 16),
            ),
          ),
        ],
      ),
    );
  }
}

class _ToolChip extends StatelessWidget {
  const _ToolChip({
    super.key,
    required this.icon,
    required this.label,
    required this.onPressed,
    this.highlighted = false,
    this.showSpinner = false,
  });

  final IconData icon;
  final String label;
  final VoidCallback? onPressed;
  final bool highlighted;
  final bool showSpinner;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final scheme = Theme.of(context).colorScheme;
    final isLight = theme.brightness == Brightness.light;
    final enabled = onPressed != null;
    return Material(
      color: highlighted
          ? scheme.primaryContainer.withValues(alpha: isLight ? 0.86 : 0.94)
          : isLight
              ? Colors.white.withValues(alpha: enabled ? 0.92 : 0.58)
              : scheme.surfaceContainerHigh
                  .withValues(alpha: enabled ? 0.82 : 0.5),
      borderRadius: BorderRadius.circular(999),
      child: InkWell(
        onTap: onPressed,
        borderRadius: BorderRadius.circular(999),
        child: Ink(
          padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 9),
          decoration: BoxDecoration(
            borderRadius: BorderRadius.circular(999),
            border: Border.all(
              color: highlighted
                  ? scheme.primary.withValues(alpha: isLight ? 0.32 : 0.42)
                  : scheme.outlineVariant
                      .withValues(alpha: enabled ? 0.48 : 0.22),
            ),
          ),
          child: Row(
            mainAxisSize: MainAxisSize.min,
            children: [
              if (showSpinner)
                SizedBox(
                  width: 16,
                  height: 16,
                  child: CircularProgressIndicator(
                    strokeWidth: 2.1,
                    valueColor: AlwaysStoppedAnimation<Color>(
                      highlighted
                          ? scheme.onPrimaryContainer
                          : scheme.onSurfaceVariant,
                    ),
                  ),
                )
              else
                Icon(
                  icon,
                  size: 16,
                  color: highlighted
                      ? scheme.onPrimaryContainer
                      : enabled
                          ? scheme.onSurfaceVariant
                          : scheme.onSurfaceVariant.withValues(alpha: 0.56),
                ),
              const SizedBox(width: 6),
              Text(
                label,
                style: Theme.of(context).textTheme.bodySmall?.copyWith(
                      fontWeight: FontWeight.w600,
                      color: highlighted
                          ? scheme.onPrimaryContainer
                          : enabled
                              ? scheme.onSurface
                              : scheme.onSurface.withValues(alpha: 0.56),
                    ),
              ),
            ],
          ),
        ),
      ),
    );
  }
}
