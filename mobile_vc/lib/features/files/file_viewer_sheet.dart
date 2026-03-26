import 'dart:convert';
import 'dart:typed_data';

import 'package:flutter/material.dart';

import '../../data/models/events.dart';
import '../../data/models/session_models.dart';
import '../diff/diff_code_view.dart';

class FileViewerSheet extends StatefulWidget {
  const FileViewerSheet({
    super.key,
    required this.file,
    required this.loading,
    required this.showReviewActions,
    required this.isDiffMode,
    required this.reviewDiff,
    required this.pendingDiffs,
    required this.reviewGroups,
    required this.activeReviewGroupId,
    required this.activeReviewDiffId,
    required this.isAutoAcceptMode,
    required this.shouldShowPermissionChoices,
    required this.shouldShowReviewChoices,
    required this.pendingPrompt,
    required this.pendingInteraction,
    required this.onAccept,
    required this.onRevert,
    required this.onRevise,
    required this.onSelectReviewGroup,
    required this.onSelectReviewDiff,
    required this.onOpenDiffList,
    required this.onUseAsContext,
    required this.onSendFilePrompt,
    required this.onSubmitPrompt,
  });

  final FileReadResult? file;
  final bool loading;
  final bool showReviewActions;
  final bool isDiffMode;
  final HistoryContext? reviewDiff;
  final List<HistoryContext> pendingDiffs;
  final List<ReviewGroup> reviewGroups;
  final String activeReviewGroupId;
  final String activeReviewDiffId;
  final bool isAutoAcceptMode;
  final bool shouldShowPermissionChoices;
  final bool shouldShowReviewChoices;
  final PromptRequestEvent? pendingPrompt;
  final InteractionRequestEvent? pendingInteraction;
  final VoidCallback onAccept;
  final VoidCallback onRevert;
  final VoidCallback onRevise;
  final ValueChanged<String> onSelectReviewGroup;
  final ValueChanged<String> onSelectReviewDiff;
  final VoidCallback onOpenDiffList;
  final VoidCallback onUseAsContext;
  final ValueChanged<String> onSendFilePrompt;
  final ValueChanged<String> onSubmitPrompt;

  @override
  State<FileViewerSheet> createState() => _FileViewerSheetState();
}

class _FileViewerSheetState extends State<FileViewerSheet> {
  final TextEditingController _controller = TextEditingController();
  final FocusNode _focusNode = FocusNode();

  bool get _inputLocked =>
      widget.shouldShowPermissionChoices || widget.shouldShowReviewChoices;

  String get _lockedHintText {
    if (widget.shouldShowPermissionChoices) {
      return '请先在上方确认授权';
    }
    if (widget.shouldShowReviewChoices) {
      return '请先在上方完成审核';
    }
    return '输入针对当前文件的请求';
  }

  @override
  void didUpdateWidget(covariant FileViewerSheet oldWidget) {
    super.didUpdateWidget(oldWidget);
    if (_inputLocked &&
        (!oldWidget.shouldShowPermissionChoices &&
            !oldWidget.shouldShowReviewChoices)) {
      _focusNode.unfocus();
    }
  }

  @override
  void dispose() {
    _controller.dispose();
    _focusNode.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final result = widget.file;
    final diff = widget.reviewDiff;
    final activeGroup = _activeGroup();
    final groupDiffs = _groupDiffs(activeGroup);
    final modeLabel = widget.isDiffMode ? '待审核改动' : '文件内容';
    final bottomInset = MediaQuery.of(context).viewInsets.bottom;
    final multiPending = groupDiffs.length > 1;
    final multiGroup = widget.reviewGroups.length > 1;
    final prompt = widget.pendingPrompt;
    final interaction = widget.pendingInteraction;
    final showPermissionBar = !widget.shouldShowReviewChoices &&
        ((interaction?.isPermission == true) ||
            (prompt != null &&
                prompt.hasVisiblePrompt &&
                prompt.looksLikePermissionPrompt));
    return SafeArea(
      top: false,
      child: AnimatedPadding(
        duration: const Duration(milliseconds: 180),
        curve: Curves.easeOut,
        padding: EdgeInsets.fromLTRB(16, 6, 16, 24 + bottomInset),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Container(
              width: double.infinity,
              padding: const EdgeInsets.fromLTRB(16, 16, 16, 14),
              decoration: BoxDecoration(
                gradient: const LinearGradient(
                  colors: [Color(0xFFF7F9FC), Color(0xFFFFFFFF)],
                  begin: Alignment.topLeft,
                  end: Alignment.bottomRight,
                ),
                borderRadius: BorderRadius.circular(22),
                border: Border.all(
                  color: Theme.of(context)
                      .colorScheme
                      .outlineVariant
                      .withValues(alpha: 0.45),
                ),
              ),
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  Text(
                    result?.title ?? '文件内容',
                    style: Theme.of(context).textTheme.titleLarge?.copyWith(
                          fontWeight: FontWeight.w800,
                          letterSpacing: -0.2,
                        ),
                  ),
                  const SizedBox(height: 6),
                  Text(
                    widget.isDiffMode
                        ? (activeGroup != null
                            ? '查看当前文件与所属修改组中的待审核内容'
                            : '查看当前文件与待审核改动内容')
                        : '查看当前文件内容，并可直接基于它继续提问',
                    style: Theme.of(context).textTheme.bodySmall?.copyWith(
                          color: Theme.of(context).colorScheme.onSurfaceVariant,
                          height: 1.45,
                        ),
                  ),
                ],
              ),
            ),
            const SizedBox(height: 10),
            Expanded(
              child: widget.loading
                  ? const Center(child: CircularProgressIndicator())
                  : result == null
                      ? const Center(child: Text('请先选择一个文件'))
                      : widget.isDiffMode && (diff?.diff ?? '').isNotEmpty
                          ? SingleChildScrollView(
                              child: DiffCodeView(diff: diff!.diff),
                            )
                          : _buildFileContent(context, result),
            ),
            const SizedBox(height: 8),
            SingleChildScrollView(
              scrollDirection: Axis.horizontal,
              child: Row(
                children: [
                  _MetaChip(label: '显示', value: modeLabel, compact: true),
                  const SizedBox(width: 6),
                  _MetaChip(
                    label: '语言',
                    value: (result?.lang ?? '').isEmpty ? '-' : result!.lang,
                    compact: true,
                  ),
                  const SizedBox(width: 6),
                  _MetaChip(
                    label: '编码',
                    value: result?.encoding ?? 'utf-8',
                    compact: true,
                  ),
                  const SizedBox(width: 6),
                  _MetaChip(
                    label: '大小',
                    value: _sizeLabel(result?.size ?? 0),
                    compact: true,
                  ),
                  const SizedBox(width: 6),
                  FilledButton.tonalIcon(
                    onPressed: result == null ? null : widget.onUseAsContext,
                    style: FilledButton.styleFrom(
                      visualDensity: VisualDensity.compact,
                      padding: const EdgeInsets.symmetric(
                        horizontal: 12,
                        vertical: 10,
                      ),
                    ),
                    icon: const Icon(Icons.chat_bubble_outline, size: 16),
                    label: const Text('继续提问'),
                  ),
                ],
              ),
            ),
            if ((result?.path ?? '').isNotEmpty) ...[
              const SizedBox(height: 8),
              Container(
                width: double.infinity,
                padding:
                    const EdgeInsets.symmetric(horizontal: 12, vertical: 10),
                decoration: BoxDecoration(
                  color: Colors.white,
                  borderRadius: BorderRadius.circular(16),
                  border: Border.all(
                    color: Theme.of(context)
                        .colorScheme
                        .outlineVariant
                        .withValues(alpha: 0.35),
                  ),
                ),
                child: SelectableText(
                  result!.path,
                  style: Theme.of(context).textTheme.bodySmall,
                ),
              ),
            ],
            if (widget.showReviewActions) ...[
              const SizedBox(height: 8),
              Container(
                width: double.infinity,
                padding: const EdgeInsets.all(12),
                decoration: BoxDecoration(
                  color: Theme.of(context).colorScheme.surfaceContainerHighest,
                  borderRadius: BorderRadius.circular(16),
                ),
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    Text(
                      widget.isAutoAcceptMode
                          ? '当前是自动接受修改模式'
                          : widget.shouldShowReviewChoices
                              ? '当前文件正在等待你审核'
                              : '当前文件包含待审核改动',
                      style: Theme.of(context)
                          .textTheme
                          .titleSmall
                          ?.copyWith(fontWeight: FontWeight.w700),
                    ),
                    if ((diff?.path ?? '').isNotEmpty) ...[
                      const SizedBox(height: 4),
                      Text(diff!.path,
                          style: Theme.of(context).textTheme.bodySmall),
                    ],
                    if (activeGroup != null) ...[
                      const SizedBox(height: 8),
                      Text(
                        activeGroup.title.isNotEmpty
                            ? activeGroup.title
                            : '当前修改组',
                        style: Theme.of(context)
                            .textTheme
                            .bodySmall
                            ?.copyWith(fontWeight: FontWeight.w700),
                      ),
                      const SizedBox(height: 4),
                      Text(
                        '本组共 ${activeGroup.files.length} 个文件，剩余 ${activeGroup.pendingCount} 个待审核。',
                        style: Theme.of(context).textTheme.bodySmall?.copyWith(
                              color: Theme.of(context)
                                  .colorScheme
                                  .onSurfaceVariant,
                            ),
                      ),
                      if ((diff?.path ?? '').isNotEmpty) ...[
                        const SizedBox(height: 4),
                        Text(
                          '当前文件：${_shortLabel(diff!)}',
                          style:
                              Theme.of(context).textTheme.bodySmall?.copyWith(
                                    color: Theme.of(context)
                                        .colorScheme
                                        .onSurfaceVariant,
                                  ),
                        ),
                      ],
                      const SizedBox(height: 8),
                      Wrap(
                        spacing: 8,
                        runSpacing: 8,
                        children: [
                          _MetaChip(
                            label: '状态',
                            value: _reviewStatusLabel(activeGroup.reviewStatus),
                            compact: true,
                          ),
                          _MetaChip(
                            label: '进度',
                            value:
                                '${activeGroup.pendingCount} / ${activeGroup.files.length}',
                            compact: true,
                          ),
                          _MetaChip(
                            label: '已同意',
                            value: '${activeGroup.acceptedCount}',
                            compact: true,
                          ),
                          _MetaChip(
                            label: '已撤销',
                            value: '${activeGroup.revertedCount}',
                            compact: true,
                          ),
                          _MetaChip(
                            label: '继续调整',
                            value: '${activeGroup.revisedCount}',
                            compact: true,
                          ),
                        ],
                      ),
                    ],
                    if (multiGroup) ...[
                      const SizedBox(height: 10),
                      SizedBox(
                        height: 40,
                        child: ListView.separated(
                          scrollDirection: Axis.horizontal,
                          itemCount: widget.reviewGroups.length,
                          separatorBuilder: (_, __) => const SizedBox(width: 8),
                          itemBuilder: (context, index) {
                            final group = widget.reviewGroups[index];
                            final selected =
                                group.id == _resolvedActiveReviewGroupId();
                            return ChoiceChip(
                              selected: selected,
                              label: Text(group.title.isNotEmpty
                                  ? group.title
                                  : '修改组 ${index + 1}'),
                              onSelected: (_) =>
                                  widget.onSelectReviewGroup(group.id),
                            );
                          },
                        ),
                      ),
                    ],
                    if (multiPending) ...[
                      const SizedBox(height: 10),
                      SizedBox(
                        height: 40,
                        child: ListView.separated(
                          scrollDirection: Axis.horizontal,
                          itemCount: groupDiffs.length,
                          separatorBuilder: (_, __) => const SizedBox(width: 8),
                          itemBuilder: (context, index) {
                            final item = groupDiffs[index];
                            final selected = _diffIdentity(item) ==
                                _resolvedActiveReviewDiffId();
                            return ChoiceChip(
                              selected: selected,
                              label: Text('${index + 1}. ${_shortLabel(item)}'),
                              onSelected: (_) => widget
                                  .onSelectReviewDiff(_diffIdentity(item)),
                            );
                          },
                        ),
                      ),
                      const SizedBox(height: 8),
                      Align(
                        alignment: Alignment.centerLeft,
                        child: TextButton.icon(
                          onPressed: widget.onOpenDiffList,
                          icon: const Icon(Icons.difference_outlined, size: 16),
                          label: const Text('进入 differ 逐个审核'),
                        ),
                      ),
                    ],
                    if (!widget.isAutoAcceptMode &&
                        widget.shouldShowReviewChoices) ...[
                      const SizedBox(height: 10),
                      Wrap(
                        spacing: 8,
                        runSpacing: 8,
                        children: [
                          FilledButton(
                              onPressed: widget.onAccept,
                              child: const Text('同意')),
                          FilledButton.tonal(
                              onPressed: widget.onRevert,
                              child: const Text('撤销')),
                          OutlinedButton(
                              onPressed: widget.onRevise,
                              child: const Text('继续调整')),
                        ],
                      ),
                    ],
                    if (!widget.shouldShowReviewChoices &&
                        widget.pendingPrompt != null &&
                        widget.pendingPrompt!.hasVisiblePrompt) ...[
                      const SizedBox(height: 10),
                      _PromptRequestSection(
                        prompt: widget.pendingPrompt!,
                        onSubmit: widget.onSubmitPrompt,
                      ),
                    ],
                  ],
                ),
              ),
            ],
            if (!widget.showReviewActions &&
                !widget.shouldShowReviewChoices &&
                widget.pendingPrompt != null &&
                widget.pendingPrompt!.hasVisiblePrompt &&
                !widget.pendingPrompt!.looksLikePermissionPrompt) ...[
              const SizedBox(height: 8),
              _PromptRequestSection(
                prompt: widget.pendingPrompt!,
                onSubmit: widget.onSubmitPrompt,
              ),
            ],
            if (showPermissionBar) ...[
              const SizedBox(height: 8),
              _PermissionActionBar(
                key: const ValueKey('fileViewer.permissionBar'),
                prompt: prompt,
                interaction: interaction,
                onSubmit: widget.onSubmitPrompt,
              ),
            ],
            const SizedBox(height: 8),
            TextField(
              key: const ValueKey('fileViewer.input'),
              controller: _controller,
              focusNode: _focusNode,
              enabled: !_inputLocked,
              readOnly: _inputLocked,
              canRequestFocus: !_inputLocked,
              minLines: 1,
              maxLines: 3,
              textInputAction: TextInputAction.send,
              onTap: _inputLocked ? () => _focusNode.unfocus() : null,
              onSubmitted: _inputLocked ? null : (_) => _submitPrompt(),
              decoration: InputDecoration(
                hintText: _lockedHintText,
                suffixIcon: IconButton(
                  key: const ValueKey('fileViewer.sendButton'),
                  onPressed: _inputLocked ? null : _submitPrompt,
                  icon: const Icon(Icons.send),
                ),
              ),
            ),
          ],
        ),
      ),
    );
  }

  Widget _buildFileContent(BuildContext context, FileReadResult result) {
    if (result.isText) {
      return SingleChildScrollView(
        child: Container(
          width: double.infinity,
          padding: const EdgeInsets.all(14),
          decoration: BoxDecoration(
            color: Colors.white,
            borderRadius: BorderRadius.circular(18),
            border: Border.all(
              color: Theme.of(context)
                  .colorScheme
                  .outlineVariant
                  .withValues(alpha: 0.45),
            ),
          ),
          child: SelectableText(
            result.content,
            style: Theme.of(context).textTheme.bodyMedium?.copyWith(
                  color: Theme.of(context).colorScheme.onSurface,
                  fontFamily: 'monospace',
                  height: 1.45,
                ),
          ),
        ),
      );
    }

    if (result.isImage) {
      final bytes = _decodeImageBytes(result.content);
      if (bytes != null) {
        return Container(
          width: double.infinity,
          decoration: BoxDecoration(
            color: const Color(0xFFF8FAFC),
            borderRadius: BorderRadius.circular(20),
            border: Border.all(
              color: Theme.of(context)
                  .colorScheme
                  .outlineVariant
                  .withValues(alpha: 0.5),
            ),
          ),
          child: ClipRRect(
            borderRadius: BorderRadius.circular(20),
            child: InteractiveViewer(
              minScale: 0.6,
              maxScale: 4,
              child: SingleChildScrollView(
                padding: const EdgeInsets.all(16),
                child: Center(
                  child: Image.memory(
                    bytes,
                    fit: BoxFit.contain,
                    errorBuilder: (context, error, stackTrace) {
                      return _buildUnsupportedPreview(
                          context, '图片解码失败，当前无法预览。');
                    },
                  ),
                ),
              ),
            ),
          ),
        );
      }
      return _buildUnsupportedPreview(context, '已识别为图片文件，但当前返回内容无法直接预览。');
    }

    return _buildUnsupportedPreview(context, '该文件不是文本文件，当前无法预览。');
  }

  Widget _buildUnsupportedPreview(BuildContext context, String message) {
    return Center(
      child: Padding(
        padding: const EdgeInsets.all(24),
        child: Text(
          message,
          style: Theme.of(context).textTheme.bodyLarge,
          textAlign: TextAlign.center,
        ),
      ),
    );
  }

  Uint8List? _decodeImageBytes(String content) {
    final trimmed = content.trim();
    if (trimmed.isEmpty) {
      return null;
    }
    final dataPart = trimmed.startsWith('data:')
        ? trimmed.substring(trimmed.indexOf(',') + 1)
        : trimmed;
    try {
      return base64Decode(dataPart);
    } catch (_) {
      return null;
    }
  }

  void _submitPrompt() {
    if (_inputLocked) {
      _focusNode.unfocus();
      return;
    }

    final value = _controller.text.trim();
    if (value.isEmpty) {
      return;
    }
    final prompt = widget.pendingPrompt;
    final interaction = widget.pendingInteraction;
    final shouldSubmitPrompt = !widget.shouldShowReviewChoices &&
        ((interaction != null && interaction.hasVisiblePrompt) ||
            (prompt != null &&
                prompt.hasVisiblePrompt &&
                (prompt.options.isNotEmpty || prompt.looksLikePermissionPrompt)));
    if (shouldSubmitPrompt) {
      widget.onSubmitPrompt(value);
    } else {
      widget.onSendFilePrompt(value);
    }
    _controller.clear();
  }

  String _resolvedActiveReviewGroupId() {
    if (widget.activeReviewGroupId.trim().isNotEmpty) {
      return widget.activeReviewGroupId.trim();
    }
    final diff = widget.reviewDiff;
    if (diff == null) {
      return '';
    }
    final groupId = diff.groupId.trim();
    if (groupId.isNotEmpty) {
      return groupId;
    }
    final executionId = diff.executionId.trim();
    if (executionId.isNotEmpty) {
      return executionId;
    }
    return _normalizePath(diff.path);
  }

  ReviewGroup? _activeGroup() {
    final groupId = _resolvedActiveReviewGroupId();
    if (groupId.isEmpty) {
      return null;
    }
    for (final group in widget.reviewGroups) {
      if (group.id == groupId) {
        return group;
      }
    }
    return null;
  }

  List<HistoryContext> _groupDiffs(ReviewGroup? group) {
    if (group == null) {
      return widget.pendingDiffs;
    }
    final fileIds = group.files
        .where((file) => file.id.trim().isNotEmpty)
        .map((file) => file.id.trim())
        .toSet();
    final filePaths = group.files
        .where((file) => file.path.trim().isNotEmpty)
        .map((file) => _normalizePath(file.path))
        .toSet();
    final matches = widget.pendingDiffs.where((item) {
      final itemId = item.id.trim();
      final itemPath = _normalizePath(item.path);
      return (itemId.isNotEmpty && fileIds.contains(itemId)) ||
          (itemPath.isNotEmpty && filePaths.contains(itemPath));
    }).toList(growable: false);
    return matches.isNotEmpty ? matches : widget.pendingDiffs;
  }

  String _resolvedActiveReviewDiffId() {
    if (widget.activeReviewDiffId.trim().isNotEmpty) {
      return widget.activeReviewDiffId.trim();
    }
    final diff = widget.reviewDiff;
    if (diff == null) {
      return '';
    }
    return _diffIdentity(diff);
  }

  String _diffIdentity(HistoryContext diff) {
    final id = diff.id.trim();
    return id.isNotEmpty ? id : _normalizePath(diff.path);
  }

  String _normalizePath(String value) {
    return value.replaceAll('\\', '/').trim();
  }

  String _shortLabel(HistoryContext diff) {
    final source = diff.title.isNotEmpty ? diff.title : diff.path;
    if (source.isEmpty) {
      return '未命名文件';
    }
    final normalized = source.replaceAll('\\', '/');
    final index = normalized.lastIndexOf('/');
    return index == -1 ? normalized : normalized.substring(index + 1);
  }

  String _reviewStatusLabel(String value) {
    switch (value.trim()) {
      case 'pending':
        return '待审核';
      case 'accepted':
        return '已同意';
      case 'reverted':
        return '已撤销';
      case 'revised':
        return '继续调整';
      case 'mixed':
        return '混合';
      default:
        return '进行中';
    }
  }

  String _sizeLabel(int size) {
    if (size <= 0) {
      return '0 B';
    }
    if (size < 1024) {
      return '$size B';
    }
    if (size < 1024 * 1024) {
      return '${(size / 1024).toStringAsFixed(1)} KB';
    }
    return '${(size / (1024 * 1024)).toStringAsFixed(1)} MB';
  }
}

class _PermissionActionBar extends StatelessWidget {
  const _PermissionActionBar({
    super.key,
    this.prompt,
    this.interaction,
    required this.onSubmit,
  });

  final PromptRequestEvent? prompt;
  final InteractionRequestEvent? interaction;
  final ValueChanged<String> onSubmit;

  @override
  Widget build(BuildContext context) {
    final options = _resolvedOptions();
    final message = _resolvedMessage();
    return Container(
      width: double.infinity,
      padding: const EdgeInsets.all(12),
      decoration: BoxDecoration(
        color: Theme.of(context).colorScheme.primaryContainer,
        borderRadius: BorderRadius.circular(18),
        border: Border.all(
          color: Theme.of(context).colorScheme.primary.withValues(alpha: 0.22),
        ),
      ),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Text(
            message,
            style: Theme.of(context).textTheme.bodyMedium?.copyWith(
                  color: Theme.of(context).colorScheme.onPrimaryContainer,
                ),
          ),
          const SizedBox(height: 10),
          Wrap(
            spacing: 8,
            runSpacing: 8,
            children: options
                .map((option) => _PromptOptionAction(
                      key: ValueKey(
                          'fileViewer.permissionAction.${option.value.trim().toLowerCase()}'),
                      label:
                          _promptOptionLabel(option.value, option.displayText),
                      style: _promptOptionStyle(option.value),
                      onPressed: () => onSubmit(option.value),
                    ))
                .toList(growable: false),
          ),
        ],
      ),
    );
  }

  List<PromptOption> _resolvedOptions() {
    final options =
        (prompt?.options ?? interaction?.options ?? const <PromptOption>[])
            .where((option) => option.displayText.isNotEmpty)
            .toList(growable: false);
    if (options.isNotEmpty) {
      return options;
    }
    return const [
      PromptOption(value: 'y', label: '允许'),
      PromptOption(value: 'n', label: '拒绝'),
    ];
  }

  String _resolvedMessage() {
    final promptMessage = prompt?.message.trim() ?? '';
    if (promptMessage.isNotEmpty) {
      return promptMessage;
    }
    final interactionMessage = interaction?.message.trim() ?? '';
    if (interactionMessage.isNotEmpty) {
      return interactionMessage;
    }
    final interactionTitle = interaction?.title.trim() ?? '';
    if (interactionTitle.isNotEmpty) {
      return interactionTitle;
    }
    return '当前操作需要你的授权。';
  }

  String _promptOptionLabel(String value, String fallback) {
    final normalized = value.trim().toLowerCase();
    if (_approveValues.contains(normalized)) {
      return '允许';
    }
    if (_denyValues.contains(normalized)) {
      return '拒绝';
    }
    return fallback;
  }

  _PromptActionStyle _promptOptionStyle(String value) {
    final normalized = value.trim().toLowerCase();
    if (_approveValues.contains(normalized)) {
      return _PromptActionStyle.primary;
    }
    if (_denyValues.contains(normalized)) {
      return _PromptActionStyle.tonal;
    }
    return _PromptActionStyle.outlined;
  }
}

class _PromptRequestSection extends StatelessWidget {
  const _PromptRequestSection({
    required this.prompt,
    required this.onSubmit,
  });

  final PromptRequestEvent prompt;
  final ValueChanged<String> onSubmit;

  @override
  Widget build(BuildContext context) {
    final options = _resolvedOptions();
    return Container(
      width: double.infinity,
      padding: const EdgeInsets.all(12),
      decoration: BoxDecoration(
        color: Colors.white,
        borderRadius: BorderRadius.circular(16),
        border: Border.all(
          color: Theme.of(context)
              .colorScheme
              .outlineVariant
              .withValues(alpha: 0.35),
        ),
      ),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          if (prompt.message.trim().isNotEmpty)
            Text(
              prompt.message.trim(),
              style: Theme.of(context).textTheme.bodyMedium,
            ),
          if (options.isNotEmpty) ...[
            if (prompt.message.trim().isNotEmpty) const SizedBox(height: 10),
            Wrap(
              spacing: 8,
              runSpacing: 8,
              children: options
                  .map((option) => _PromptOptionAction(
                        label: _promptOptionLabel(
                            option.value, option.displayText),
                        style: _promptOptionStyle(option.value),
                        onPressed: () => onSubmit(option.value),
                      ))
                  .toList(growable: false),
            ),
          ],
        ],
      ),
    );
  }

  List<PromptOption> _resolvedOptions() {
    final options = prompt.options
        .where((option) => option.displayText.isNotEmpty)
        .toList(growable: false);
    if (options.isNotEmpty) {
      return options;
    }
    if (prompt.looksLikePermissionPrompt) {
      return const [
        PromptOption(value: 'y', label: '允许'),
        PromptOption(value: 'n', label: '拒绝'),
      ];
    }
    return const [];
  }

  String _promptOptionLabel(String value, String fallback) {
    final normalized = value.trim().toLowerCase();
    if (_approveValues.contains(normalized)) {
      return '允许';
    }
    if (_denyValues.contains(normalized)) {
      return '拒绝';
    }
    return fallback;
  }

  _PromptActionStyle _promptOptionStyle(String value) {
    final normalized = value.trim().toLowerCase();
    if (_approveValues.contains(normalized)) {
      return _PromptActionStyle.primary;
    }
    if (_denyValues.contains(normalized)) {
      return _PromptActionStyle.tonal;
    }
    return _PromptActionStyle.outlined;
  }
}

class _PromptOptionAction extends StatelessWidget {
  const _PromptOptionAction({
    super.key,
    required this.label,
    required this.style,
    required this.onPressed,
  });

  final String label;
  final _PromptActionStyle style;
  final VoidCallback onPressed;

  @override
  Widget build(BuildContext context) {
    switch (style) {
      case _PromptActionStyle.primary:
        return FilledButton(onPressed: onPressed, child: Text(label));
      case _PromptActionStyle.tonal:
        return FilledButton.tonal(onPressed: onPressed, child: Text(label));
      case _PromptActionStyle.outlined:
        return OutlinedButton(onPressed: onPressed, child: Text(label));
    }
  }
}

enum _PromptActionStyle { primary, tonal, outlined }

const Set<String> _approveValues = {
  'y',
  'yes',
  'ok',
  'approve',
  'approved',
  'allow',
  'accept',
  'continue',
};

const Set<String> _denyValues = {
  'n',
  'no',
  'deny',
  'denied',
  'reject',
  'cancel',
  'stop',
};

class _MetaChip extends StatelessWidget {
  const _MetaChip({
    required this.label,
    required this.value,
    this.compact = false,
  });

  final String label;
  final String value;
  final bool compact;

  @override
  Widget build(BuildContext context) {
    return Container(
      padding: EdgeInsets.symmetric(
        horizontal: compact ? 8 : 10,
        vertical: compact ? 5 : 6,
      ),
      decoration: BoxDecoration(
        color: Theme.of(context).colorScheme.surfaceContainerHighest,
        borderRadius: BorderRadius.circular(999),
      ),
      child: Text(
        '$label: $value',
        style: (compact
                ? Theme.of(context).textTheme.labelSmall
                : Theme.of(context).textTheme.bodySmall)
            ?.copyWith(fontWeight: FontWeight.w600),
      ),
    );
  }
}
