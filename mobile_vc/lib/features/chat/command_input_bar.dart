import 'package:flutter/material.dart';

class CommandInputBar extends StatefulWidget {
  const CommandInputBar({
    super.key,
    required this.awaitInput,
    required this.isBusy,
    required this.hasPendingReview,
    required this.fastMode,
    required this.permissionMode,
    required this.onSubmit,
    required this.onOpenSessions,
    required this.onOpenRuntimeInfo,
    required this.onOpenLogs,
    required this.onOpenSkills,
    required this.onOpenMemory,
    required this.onPermissionModeChanged,
    required this.showClaudeMode,
    required this.shouldShowPermissionChoices,
    required this.shouldShowReviewChoices,
    required this.isSessionLoading,
  });

  final bool awaitInput;
  final bool isBusy;
  final bool hasPendingReview;
  final bool fastMode;
  final String permissionMode;
  final ValueChanged<String> onSubmit;
  final VoidCallback onOpenSessions;
  final VoidCallback onOpenRuntimeInfo;
  final VoidCallback onOpenLogs;
  final VoidCallback onOpenSkills;
  final VoidCallback onOpenMemory;
  final ValueChanged<String> onPermissionModeChanged;
  final bool showClaudeMode;
  final bool shouldShowPermissionChoices;
  final bool shouldShowReviewChoices;
  final bool isSessionLoading;

  @override
  State<CommandInputBar> createState() => _CommandInputBarState();
}

class _CommandInputBarState extends State<CommandInputBar> {
  final TextEditingController _controller = TextEditingController();
  final FocusNode _focusNode = FocusNode();

  bool get _inputLocked =>
      widget.isSessionLoading ||
      widget.shouldShowPermissionChoices ||
      widget.shouldShowReviewChoices;

  String get _lockedHintText {
    if (widget.isSessionLoading) {
      return '会话切换中...';
    }
    if (widget.shouldShowPermissionChoices) {
      return '请先在上方确认授权';
    }
    if (widget.shouldShowReviewChoices) {
      return '请先在上方完成审核';
    }
    return '';
  }

  @override
  void didUpdateWidget(covariant CommandInputBar oldWidget) {
    super.didUpdateWidget(oldWidget);
    final oldLocked = oldWidget.shouldShowPermissionChoices ||
        oldWidget.shouldShowReviewChoices;
    if (_inputLocked && !oldLocked) {
      _focusNode.unfocus();
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
    if (normalized.isEmpty) {
      return;
    }
    final keepKeyboard = _shouldKeepKeyboard(normalized);
    widget.onSubmit(text);
    _controller.clear();
    if (!keepKeyboard) {
      _focusNode.unfocus();
    }
  }

  bool _shouldKeepKeyboard(String value) {
    final lower = value.trim().toLowerCase();
    return lower == 'claude' || lower.startsWith('claude ');
  }

  @override
  Widget build(BuildContext context) {
    final scheme = Theme.of(context).colorScheme;
    final bottomInset = MediaQuery.of(context).viewInsets.bottom;
    final hintText = _inputLocked
        ? _lockedHintText
        : widget.awaitInput
            ? (widget.showClaudeMode ? '继续回复 Claude' : '继续输入')
            : widget.hasPendingReview
                ? '先处理待审核 diff，再继续'
                : widget.isBusy
                    ? (widget.showClaudeMode ? 'Claude 正在处理中' : '当前 shell 会话仍在运行')
                    : (widget.showClaudeMode ? '给 Claude 发送消息' : '输入命令');
    final modeLabel = widget.showClaudeMode ? 'Claude' : 'Shell';
    final modeColor = widget.showClaudeMode
        ? scheme.primary
        : scheme.onSurfaceVariant.withValues(alpha: 0.75);

    return Padding(
      padding: EdgeInsets.only(bottom: bottomInset),
      child: SafeArea(
        top: false,
        child: Padding(
          padding: EdgeInsets.fromLTRB(10, 6, 10, bottomInset > 0 ? 8 : 10),
          child: Container(
            padding: const EdgeInsets.fromLTRB(10, 8, 10, 10),
            decoration: BoxDecoration(
              color: Colors.white.withValues(alpha: 0.97),
              borderRadius: BorderRadius.circular(30),
              border: Border.all(
                color: scheme.outlineVariant.withValues(alpha: 0.5),
              ),
              boxShadow: [
                BoxShadow(
                  color: Colors.black.withValues(alpha: 0.05),
                  blurRadius: 24,
                  offset: const Offset(0, 8),
                ),
              ],
            ),
            child: Column(
              mainAxisSize: MainAxisSize.min,
              children: [
                SizedBox(
                  width: double.infinity,
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
                        _ToolChip(
                          icon: Icons.terminal,
                          label: '日志',
                          onPressed: widget.onOpenLogs,
                        ),
                        const SizedBox(width: 8),
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
                        DecoratedBox(
                          decoration: BoxDecoration(
                            color: const Color(0xFFF7F8FC),
                            borderRadius: BorderRadius.circular(999),
                            border: Border.all(
                              color: scheme.outlineVariant.withValues(alpha: 0.4),
                            ),
                          ),
                          child: Padding(
                            padding: const EdgeInsets.symmetric(horizontal: 12),
                            child: DropdownButtonHideUnderline(
                              child: DropdownButton<String>(
                                value: widget.permissionMode,
                                borderRadius: BorderRadius.circular(16),
                                style: Theme.of(context)
                                    .textTheme
                                    .bodySmall
                                    ?.copyWith(fontWeight: FontWeight.w600),
                                items: const [
                                  DropdownMenuItem(
                                    value: 'default',
                                    child: Text('默认确认'),
                                  ),
                                  DropdownMenuItem(
                                    value: 'acceptEdits',
                                    child: Text('自动接受修改'),
                                  ),
                                  DropdownMenuItem(
                                    value: 'bypassPermissions',
                                    child: Text('跳过权限确认'),
                                  ),
                                ],
                                onChanged: (value) {
                                  if (value != null) {
                                    widget.onPermissionModeChanged(value);
                                  }
                                },
                              ),
                            ),
                          ),
                        ),
                      ],
                    ),
                  ),
                ),
                const SizedBox(height: 10),
                Row(
                  children: [
                    Container(
                      width: 10,
                      height: 10,
                      decoration: BoxDecoration(
                        color: modeColor,
                        shape: BoxShape.circle,
                        boxShadow: [
                          BoxShadow(
                            color: modeColor.withValues(alpha: 0.28),
                            blurRadius: 8,
                            spreadRadius: 1,
                          ),
                        ],
                      ),
                    ),
                    const SizedBox(width: 8),
                    Text(
                      modeLabel,
                      style: Theme.of(context).textTheme.labelMedium?.copyWith(
                            fontWeight: FontWeight.w700,
                            color: modeColor,
                          ),
                    ),
                  ],
                ),
                const SizedBox(height: 10),
                Container(
                  constraints: const BoxConstraints(minHeight: 56),
                  decoration: BoxDecoration(
                    color: const Color(0xFFF7F8FC),
                    borderRadius: BorderRadius.circular(28),
                    border: Border.all(
                      color: scheme.outlineVariant.withValues(alpha: 0.24),
                    ),
                  ),
                  child: Row(
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
                          textInputAction: TextInputAction.send,
                          onTap: _inputLocked ? () => _focusNode.unfocus() : null,
                          onSubmitted: _inputLocked ? null : (_) => _submit(),
                          textAlignVertical: TextAlignVertical.center,
                          style: Theme.of(context).textTheme.bodyMedium?.copyWith(
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
                            contentPadding:
                                const EdgeInsets.fromLTRB(18, 14, 8, 14),
                            border: InputBorder.none,
                            enabledBorder: InputBorder.none,
                            focusedBorder: InputBorder.none,
                            disabledBorder: InputBorder.none,
                          ),
                        ),
                      ),
                      Padding(
                        padding: const EdgeInsets.fromLTRB(0, 0, 7, 7),
                        child: SizedBox(
                          width: 42,
                          height: 42,
                          child: FilledButton(
                            onPressed: _inputLocked ? null : _submit,
                            style: FilledButton.styleFrom(
                              elevation: 0,
                              backgroundColor: _inputLocked
                                  ? scheme.surfaceContainerHighest
                                  : scheme.primary,
                              foregroundColor: _inputLocked
                                  ? scheme.onSurfaceVariant
                                  : scheme.onPrimary,
                              padding: EdgeInsets.zero,
                              minimumSize: const Size(42, 42),
                              shape: RoundedRectangleBorder(
                                borderRadius: BorderRadius.circular(999),
                              ),
                            ),
                            child: const Icon(Icons.arrow_upward, size: 18),
                          ),
                        ),
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

class _ToolChip extends StatelessWidget {
  const _ToolChip({
    required this.icon,
    required this.label,
    required this.onPressed,
  });

  final IconData icon;
  final String label;
  final VoidCallback onPressed;

  @override
  Widget build(BuildContext context) {
    final scheme = Theme.of(context).colorScheme;
    return Material(
      color: const Color(0xFFF7F8FC),
      borderRadius: BorderRadius.circular(999),
      child: InkWell(
        onTap: onPressed,
        borderRadius: BorderRadius.circular(999),
        child: Ink(
          padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 9),
          decoration: BoxDecoration(
            borderRadius: BorderRadius.circular(999),
            border: Border.all(
              color: scheme.outlineVariant.withValues(alpha: 0.38),
            ),
          ),
          child: Row(
            mainAxisSize: MainAxisSize.min,
            children: [
              Icon(icon, size: 16, color: scheme.onSurfaceVariant),
              const SizedBox(width: 6),
              Text(
                label,
                style: Theme.of(context).textTheme.bodySmall?.copyWith(
                      fontWeight: FontWeight.w600,
                      color: scheme.onSurface,
                    ),
              ),
            ],
          ),
        ),
      ),
    );
  }
}
