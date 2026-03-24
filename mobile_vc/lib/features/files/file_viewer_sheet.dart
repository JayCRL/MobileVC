import 'dart:convert';
import 'dart:typed_data';

import 'package:flutter/material.dart';

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
    required this.activeReviewDiffId,
    required this.isAutoAcceptMode,
    required this.shouldShowReviewChoices,
    required this.onAccept,
    required this.onRevert,
    required this.onRevise,
    required this.onSelectReviewDiff,
    required this.onOpenDiffList,
    required this.onUseAsContext,
    required this.onSendFilePrompt,
  });

  final FileReadResult? file;
  final bool loading;
  final bool showReviewActions;
  final bool isDiffMode;
  final HistoryContext? reviewDiff;
  final List<HistoryContext> pendingDiffs;
  final String activeReviewDiffId;
  final bool isAutoAcceptMode;
  final bool shouldShowReviewChoices;
  final VoidCallback onAccept;
  final VoidCallback onRevert;
  final VoidCallback onRevise;
  final ValueChanged<String> onSelectReviewDiff;
  final VoidCallback onOpenDiffList;
  final VoidCallback onUseAsContext;
  final ValueChanged<String> onSendFilePrompt;

  @override
  State<FileViewerSheet> createState() => _FileViewerSheetState();
}

class _FileViewerSheetState extends State<FileViewerSheet> {
  final TextEditingController _controller = TextEditingController();

  @override
  void dispose() {
    _controller.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final result = widget.file;
    final diff = widget.reviewDiff;
    final modeLabel = widget.isDiffMode ? '待审核改动' : '文件内容';
    final bottomInset = MediaQuery.of(context).viewInsets.bottom;
    final multiPending = widget.pendingDiffs.length > 1;
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
                        ? '查看当前文件与待审核改动内容'
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
                      widget.isAutoAcceptMode ? '当前是自动接受修改模式' : '当前文件包含待审核改动',
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
                    if (multiPending) ...[
                      const SizedBox(height: 10),
                      SizedBox(
                        height: 40,
                        child: ListView.separated(
                          scrollDirection: Axis.horizontal,
                          itemCount: widget.pendingDiffs.length,
                          separatorBuilder: (_, __) => const SizedBox(width: 8),
                          itemBuilder: (context, index) {
                            final item = widget.pendingDiffs[index];
                            final selected = _diffIdentity(item) ==
                                _resolvedActiveReviewDiffId();
                            return ChoiceChip(
                              selected: selected,
                              label: Text('${index + 1}. ${_shortLabel(item)}'),
                              onSelected: (_) =>
                                  widget.onSelectReviewDiff(_diffIdentity(item)),
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
                  ],
                ),
              ),
            ],
            const SizedBox(height: 8),
            TextField(
              controller: _controller,
              minLines: 1,
              maxLines: 3,
              textInputAction: TextInputAction.send,
              onSubmitted: (_) => _submitPrompt(),
              decoration: InputDecoration(
                hintText: '输入针对当前文件的请求',
                suffixIcon: IconButton(
                  onPressed: _submitPrompt,
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
    final value = _controller.text.trim();
    if (value.isEmpty) {
      return;
    }
    widget.onSendFilePrompt(value);
    _controller.clear();
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
    return id.isNotEmpty ? id : diff.path.trim();
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
