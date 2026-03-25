import 'package:flutter/material.dart';

import '../../data/models/session_models.dart';
import 'diff_code_view.dart';

class DiffViewerSheet extends StatelessWidget {
  const DiffViewerSheet({
    super.key,
    required this.title,
    required this.path,
    required this.diff,
    this.pendingDiffs = const [],
    this.reviewGroups = const [],
    this.activeReviewGroupId = '',
    this.activeDiffId = '',
    this.showReviewActions = false,
    this.onSelectGroup,
    this.onSelectDiff,
    this.onAccept,
    this.onRevert,
    this.onRevise,
  });

  final String title;
  final String path;
  final String diff;
  final List<HistoryContext> pendingDiffs;
  final List<ReviewGroup> reviewGroups;
  final String activeReviewGroupId;
  final String activeDiffId;
  final bool showReviewActions;
  final ValueChanged<String>? onSelectGroup;
  final ValueChanged<String>? onSelectDiff;
  final VoidCallback? onAccept;
  final VoidCallback? onRevert;
  final VoidCallback? onRevise;

  @override
  Widget build(BuildContext context) {
    final bottomInset = MediaQuery.of(context).viewInsets.bottom;
    final activeGroup = _activeGroup();
    final active = _activeDiff(activeGroup);
    final resolvedTitle =
        active?.title.isNotEmpty == true ? active!.title : (title.isEmpty ? 'Diff 预览' : title);
    final resolvedPath = active?.path ?? path;
    final resolvedDiff = active?.diff ?? diff;
    final summary = _diffSummary(resolvedDiff);
    final multiGroup = reviewGroups.length > 1;
    final groupFiles = _groupDiffs(activeGroup);
    final multiDiff = groupFiles.length > 1;
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
                  Row(
                    children: [
                      Container(
                        width: 40,
                        height: 40,
                        decoration: BoxDecoration(
                          color: Theme.of(context).colorScheme.primaryContainer,
                          borderRadius: BorderRadius.circular(14),
                        ),
                        child: Icon(
                          Icons.difference_outlined,
                          color: Theme.of(context).colorScheme.primary,
                        ),
                      ),
                      const SizedBox(width: 12),
                      Expanded(
                        child: Column(
                          crossAxisAlignment: CrossAxisAlignment.start,
                          children: [
                            Text(
                              resolvedTitle,
                              maxLines: 1,
                              overflow: TextOverflow.ellipsis,
                              style: Theme.of(context)
                                  .textTheme
                                  .titleLarge
                                  ?.copyWith(
                                    fontWeight: FontWeight.w800,
                                    letterSpacing: -0.2,
                                  ),
                            ),
                            const SizedBox(height: 4),
                            Text(
                              multiDiff
                                  ? '在当前修改组内横向切换文件，并逐个完成审核。'
                                  : '查看当前变更内容，便于继续审核与对比。',
                              style: Theme.of(context)
                                  .textTheme
                                  .bodySmall
                                  ?.copyWith(
                                    color: Theme.of(context)
                                        .colorScheme
                                        .onSurfaceVariant,
                                    height: 1.45,
                                  ),
                            ),
                          ],
                        ),
                      ),
                    ],
                  ),
                  if (multiGroup) ...[
                    const SizedBox(height: 12),
                    SizedBox(
                      height: 40,
                      child: ListView.separated(
                        scrollDirection: Axis.horizontal,
                        itemCount: reviewGroups.length,
                        separatorBuilder: (_, __) => const SizedBox(width: 8),
                        itemBuilder: (context, index) {
                          final group = reviewGroups[index];
                          final selected = group.id == _resolvedActiveGroupId();
                          return ChoiceChip(
                            selected: selected,
                            label: Text(group.title.isNotEmpty
                                ? group.title
                                : '修改组 ${index + 1}'),
                            onSelected: (_) => onSelectGroup?.call(group.id),
                          );
                        },
                      ),
                    ),
                  ],
                  if (multiDiff) ...[
                    const SizedBox(height: 12),
                    SizedBox(
                      height: 44,
                      child: ListView.separated(
                        scrollDirection: Axis.horizontal,
                        itemCount: groupFiles.length,
                        separatorBuilder: (_, __) => const SizedBox(width: 8),
                        itemBuilder: (context, index) {
                          final item = groupFiles[index];
                          final selected = _diffIdentity(item) == _resolvedActiveDiffId(activeGroup);
                          return ChoiceChip(
                            selected: selected,
                            label: Text('${index + 1}. ${_shortLabel(item)}'),
                            onSelected: (_) => onSelectDiff?.call(_diffIdentity(item)),
                          );
                        },
                      ),
                    ),
                  ],
                  if (resolvedPath.isNotEmpty) ...[
                    const SizedBox(height: 12),
                    Container(
                      width: double.infinity,
                      padding: const EdgeInsets.symmetric(
                        horizontal: 12,
                        vertical: 10,
                      ),
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
                        resolvedPath,
                        style: Theme.of(context).textTheme.bodySmall,
                      ),
                    ),
                  ],
                  const SizedBox(height: 10),
                  Wrap(
                    spacing: 8,
                    runSpacing: 8,
                    children: [
                      _MetaChip(label: '显示', value: 'Unified diff'),
                      _MetaChip(label: '变更', value: summary),
                      if (activeGroup != null)
                        _MetaChip(label: '修改组', value: activeGroup.title.isNotEmpty ? activeGroup.title : activeGroup.id),
                      if (activeGroup != null)
                        _MetaChip(label: '待审核', value: '${activeGroup.pendingCount} / ${activeGroup.files.length}'),
                    ],
                  ),
                ],
              ),
            ),
            const SizedBox(height: 12),
            Expanded(
              child: ClipRRect(
                borderRadius: BorderRadius.circular(20),
                child: SingleChildScrollView(
                  child: DiffCodeView(diff: resolvedDiff),
                ),
              ),
            ),
            if (showReviewActions) ...[
              const SizedBox(height: 12),
              Wrap(
                spacing: 8,
                runSpacing: 8,
                children: [
                  FilledButton(onPressed: onAccept, child: const Text('同意')),
                  FilledButton.tonal(onPressed: onRevert, child: const Text('撤销')),
                  OutlinedButton(onPressed: onRevise, child: const Text('继续调整')),
                ],
              ),
            ],
          ],
        ),
      ),
    );
  }

  HistoryContext? _activeDiff(ReviewGroup? activeGroup) {
    final candidates = _groupDiffs(activeGroup);
    if (candidates.isEmpty) {
      return null;
    }
    final activeId = _resolvedActiveDiffId(activeGroup);
    for (final item in candidates) {
      if (_diffIdentity(item) == activeId) {
        return item;
      }
    }
    return candidates.last;
  }

  ReviewGroup? _activeGroup() {
    if (reviewGroups.isEmpty) {
      return null;
    }
    final activeId = _resolvedActiveGroupId();
    for (final group in reviewGroups) {
      if (group.id == activeId) {
        return group;
      }
    }
    return reviewGroups.last;
  }

  List<HistoryContext> _groupDiffs(ReviewGroup? group) {
    if (group == null) {
      return pendingDiffs;
    }
    final fileIds = group.files
        .where((file) => file.id.trim().isNotEmpty)
        .map((file) => file.id.trim())
        .toSet();
    final filePaths = group.files
        .where((file) => file.path.trim().isNotEmpty)
        .map((file) => _normalizePath(file.path))
        .toSet();
    final matches = pendingDiffs.where((diff) {
      final diffId = diff.id.trim();
      final diffPath = _normalizePath(diff.path);
      return (diffId.isNotEmpty && fileIds.contains(diffId)) ||
          (diffPath.isNotEmpty && filePaths.contains(diffPath));
    }).toList(growable: false);
    return matches.isNotEmpty ? matches : pendingDiffs;
  }

  String _resolvedActiveGroupId() {
    if (activeReviewGroupId.trim().isNotEmpty) {
      return activeReviewGroupId.trim();
    }
    if (reviewGroups.isEmpty) {
      return '';
    }
    return reviewGroups.last.id;
  }

  String _resolvedActiveDiffId(ReviewGroup? activeGroup) {
    if (activeDiffId.trim().isNotEmpty) {
      return activeDiffId.trim();
    }
    final candidates = _groupDiffs(activeGroup);
    if (candidates.isEmpty) {
      return '';
    }
    return _diffIdentity(candidates.last);
  }

  String _diffIdentity(HistoryContext diff) {
    final id = diff.id.trim();
    return id.isNotEmpty ? id : _normalizePath(diff.path.trim());
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
    final segments = normalized.split('/');
    return segments.isEmpty ? source : segments.last;
  }

  String _diffSummary(String value) {
    if (value.trim().isEmpty) {
      return '无内容';
    }
    final lines = value.split('\n');
    final added =
        lines.where((line) => line.startsWith('+') && !line.startsWith('+++')).length;
    final removed =
        lines.where((line) => line.startsWith('-') && !line.startsWith('---')).length;
    if (added == 0 && removed == 0) {
      return '${lines.length} 行';
    }
    return '+$added / -$removed';
  }
}

class _MetaChip extends StatelessWidget {
  const _MetaChip({
    required this.label,
    required this.value,
  });

  final String label;
  final String value;

  @override
  Widget build(BuildContext context) {
    return Container(
      padding: const EdgeInsets.symmetric(horizontal: 10, vertical: 6),
      decoration: BoxDecoration(
        color: Theme.of(context).colorScheme.surfaceContainerHighest,
        borderRadius: BorderRadius.circular(999),
      ),
      child: Text(
        '$label: $value',
        style: Theme.of(context)
            .textTheme
            .labelSmall
            ?.copyWith(fontWeight: FontWeight.w700),
      ),
    );
  }
}
