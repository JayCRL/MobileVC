import 'package:flutter/material.dart';

import '../../data/models/session_models.dart';

class MemoryManagementSheet extends StatefulWidget {
  const MemoryManagementSheet({
    super.key,
    required this.items,
    required this.syncStatus,
    required this.catalogMeta,
    required this.enabledMemoryIds,
    required this.onToggleEnabled,
    required this.onSave,
    required this.onSync,
    required this.onReviseMemory,
  });

  final List<MemoryItem> items;
  final String syncStatus;
  final CatalogMetadata catalogMeta;
  final List<String> enabledMemoryIds;
  final ValueChanged<String> onToggleEnabled;
  final ValueChanged<MemoryItem> onSave;
  final VoidCallback onSync;
  final void Function(MemoryItem item, String request) onReviseMemory;

  @override
  State<MemoryManagementSheet> createState() => _MemoryManagementSheetState();
}

enum _MemoryFilter { all, enabled, editable }

class _MemoryManagementSheetState extends State<MemoryManagementSheet> {
  _MemoryFilter _filter = _MemoryFilter.all;

  List<MemoryItem> get _filteredItems {
    switch (_filter) {
      case _MemoryFilter.enabled:
        return widget.items
            .where((item) => widget.enabledMemoryIds.contains(item.id))
            .toList(growable: false);
      case _MemoryFilter.editable:
        return widget.items.where((item) => item.editable).toList(growable: false);
      case _MemoryFilter.all:
        return widget.items;
    }
  }

  @override
  Widget build(BuildContext context) {
    final meta = widget.catalogMeta;
    final items = _filteredItems;
    return SafeArea(
      top: false,
      child: Padding(
        padding: EdgeInsets.fromLTRB(
          16,
          6,
          16,
          24 + MediaQuery.of(context).viewInsets.bottom,
        ),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            _HeroCard(
              title: 'Memory 管理',
              description: 'Memory 是会话级长期上下文。打开详情可查看完整内容，并像编辑单文件一样一句话让 Claude 帮你修改。',
              action: FilledButton.tonalIcon(
                onPressed: widget.onSync,
                icon: meta.isSyncing
                    ? const SizedBox(
                        width: 16,
                        height: 16,
                        child: CircularProgressIndicator(strokeWidth: 2),
                      )
                    : const Icon(Icons.sync),
                label: Text(meta.isSyncing ? '同步中' : '同步 memory'),
              ),
              chips: [
                _MetaChip(label: '总数', value: '${widget.items.length}'),
                _MetaChip(label: '已启用', value: '${widget.enabledMemoryIds.length}'),
                _MetaChip(label: '状态', value: _syncStateLabel(meta.syncState)),
                if (meta.lastSyncedAt != null)
                  _MetaChip(label: '最近同步', value: _timeLabel(meta.lastSyncedAt)),
              ],
            ),
            if (widget.syncStatus.trim().isNotEmpty) ...[
              const SizedBox(height: 10),
              _StatusBanner(message: widget.syncStatus, tone: _bannerTone(meta)),
            ],
            const SizedBox(height: 12),
            SingleChildScrollView(
              scrollDirection: Axis.horizontal,
              child: Row(
                children: [
                  _FilterChip(
                    label: '全部',
                    selected: _filter == _MemoryFilter.all,
                    onTap: () => setState(() => _filter = _MemoryFilter.all),
                  ),
                  const SizedBox(width: 8),
                  _FilterChip(
                    label: '已启用',
                    selected: _filter == _MemoryFilter.enabled,
                    onTap: () => setState(() => _filter = _MemoryFilter.enabled),
                  ),
                  const SizedBox(width: 8),
                  _FilterChip(
                    label: '可编辑',
                    selected: _filter == _MemoryFilter.editable,
                    onTap: () => setState(() => _filter = _MemoryFilter.editable),
                  ),
                ],
              ),
            ),
            const SizedBox(height: 12),
            Expanded(
              child: ListView(
                children: [
                  if (items.isEmpty)
                    const _EmptyState()
                  else
                    ...[
                      for (var index = 0; index < items.length; index++) ...[
                        if (index > 0) const SizedBox(height: 10),
                        _MemoryCard(
                          item: items[index],
                          enabled: widget.enabledMemoryIds.contains(items[index].id),
                          onToggleEnabled: () => widget.onToggleEnabled(items[index].id),
                          onTap: () => _openDetailSheet(context, items[index]),
                        ),
                      ],
                    ],
                  const SizedBox(height: 12),
                  _ComposerCard(onSave: widget.onSave),
                ],
              ),
            ),
          ],
        ),
      ),
    );
  }

  void _openDetailSheet(BuildContext context, MemoryItem item) {
    final controller = TextEditingController();
    showModalBottomSheet<void>(
      context: context,
      isScrollControlled: true,
      useSafeArea: true,
      showDragHandle: true,
      builder: (context) {
        return Padding(
          padding: EdgeInsets.fromLTRB(
            16,
            8,
            16,
            24 + MediaQuery.of(context).viewInsets.bottom,
          ),
          child: Column(
            mainAxisSize: MainAxisSize.min,
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Text(
                item.title.isEmpty ? item.id : item.title,
                style: Theme.of(context)
                    .textTheme
                    .titleLarge
                    ?.copyWith(fontWeight: FontWeight.w800),
              ),
              const SizedBox(height: 8),
              Wrap(
                spacing: 8,
                runSpacing: 8,
                children: [
                  _MetaChip(label: 'id', value: item.id),
                  _MetaChip(label: 'source', value: item.source.isEmpty ? '-' : item.source),
                  _MetaChip(label: 'sync', value: item.syncState.isEmpty ? '-' : item.syncState),
                  _MetaChip(label: '编辑', value: item.editable ? '可编辑' : '只读'),
                ],
              ),
              const SizedBox(height: 12),
              _DetailBlock(
                title: '完整内容',
                content: item.content.trim().isEmpty ? '暂无内容' : item.content.trim(),
              ),
              const SizedBox(height: 12),
              TextField(
                key: ValueKey('memoryDetail.modifyInput:${item.id}'),
                controller: controller,
                minLines: 3,
                maxLines: 6,
                enabled: item.editable,
                decoration: InputDecoration(
                  hintText: item.editable ? '一句话告诉 Claude 你想怎么修改这条 memory' : '该 memory 为只读，不能直接修改',
                ),
              ),
              const SizedBox(height: 12),
              SizedBox(
                width: double.infinity,
                child: FilledButton.icon(
                  onPressed: !item.editable
                      ? null
                      : () {
                          final value = controller.text.trim();
                          if (value.isEmpty) {
                            return;
                          }
                          widget.onReviseMemory(item, value);
                          Navigator.of(context).pop();
                        },
                  icon: const Icon(Icons.auto_fix_high),
                  label: const Text('让 Claude 修改这个 memory'),
                ),
              ),
            ],
          ),
        );
      },
    );
  }
}

class _ComposerCard extends StatefulWidget {
  const _ComposerCard({required this.onSave});

  final ValueChanged<MemoryItem> onSave;

  @override
  State<_ComposerCard> createState() => _ComposerCardState();
}

class _ComposerCardState extends State<_ComposerCard> {
  late final TextEditingController _idController;
  late final TextEditingController _titleController;
  late final TextEditingController _contentController;
  bool _expanded = false;

  @override
  void initState() {
    super.initState();
    _idController = TextEditingController();
    _titleController = TextEditingController();
    _contentController = TextEditingController();
  }

  @override
  void dispose() {
    _idController.dispose();
    _titleController.dispose();
    _contentController.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    return Container(
      width: double.infinity,
      padding: const EdgeInsets.all(14),
      decoration: BoxDecoration(
        color: Theme.of(context).colorScheme.surfaceContainerLowest,
        borderRadius: BorderRadius.circular(22),
        border: Border.all(
          color: Theme.of(context).colorScheme.outlineVariant.withValues(alpha: 0.4),
        ),
      ),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Row(
            children: [
              Expanded(
                child: Text(
                  '新增 / 手动编辑 memory',
                  style: Theme.of(context)
                      .textTheme
                      .titleMedium
                      ?.copyWith(fontWeight: FontWeight.w700),
                ),
              ),
              TextButton(
                onPressed: () => setState(() => _expanded = !_expanded),
                child: Text(_expanded ? '收起' : '展开'),
              ),
            ],
          ),
          if (_expanded) ...[
            const SizedBox(height: 8),
            TextField(
              controller: _idController,
              decoration: const InputDecoration(labelText: 'id'),
            ),
            const SizedBox(height: 10),
            TextField(
              controller: _titleController,
              decoration: const InputDecoration(labelText: 'title'),
            ),
            const SizedBox(height: 10),
            TextField(
              controller: _contentController,
              minLines: 4,
              maxLines: 8,
              decoration: const InputDecoration(labelText: 'content'),
            ),
            const SizedBox(height: 12),
            Row(
              children: [
                Expanded(
                  child: OutlinedButton(
                    onPressed: _clear,
                    child: const Text('清空'),
                  ),
                ),
                const SizedBox(width: 10),
                Expanded(
                  child: FilledButton(
                    onPressed: _save,
                    child: const Text('保存 memory'),
                  ),
                ),
              ],
            ),
          ],
        ],
      ),
    );
  }

  void _clear() {
    _idController.clear();
    _titleController.clear();
    _contentController.clear();
    setState(() {});
  }

  void _save() {
    final id = _idController.text.trim();
    if (id.isEmpty) {
      return;
    }
    widget.onSave(
      MemoryItem(
        id: id,
        title: _titleController.text.trim(),
        content: _contentController.text.trim(),
      ),
    );
    _clear();
  }
}

class _MemoryCard extends StatelessWidget {
  const _MemoryCard({
    required this.item,
    required this.enabled,
    required this.onToggleEnabled,
    required this.onTap,
  });

  final MemoryItem item;
  final bool enabled;
  final VoidCallback onToggleEnabled;
  final VoidCallback onTap;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return Material(
      color: Colors.transparent,
      child: InkWell(
        key: ValueKey('memoryCard:${item.id}'),
        borderRadius: BorderRadius.circular(24),
        onTap: onTap,
        child: Ink(
          padding: const EdgeInsets.all(16),
          decoration: BoxDecoration(
            color: theme.colorScheme.surface,
            borderRadius: BorderRadius.circular(24),
            border: Border.all(
              color: enabled
                  ? theme.colorScheme.primary.withValues(alpha: 0.45)
                  : theme.colorScheme.outlineVariant.withValues(alpha: 0.45),
            ),
          ),
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Row(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  Expanded(
                    child: Column(
                      crossAxisAlignment: CrossAxisAlignment.start,
                      children: [
                        Text(
                          item.title.isEmpty ? item.id : item.title,
                          style: theme.textTheme.titleMedium?.copyWith(
                            fontWeight: FontWeight.w800,
                          ),
                        ),
                        const SizedBox(height: 6),
                        Text(
                          item.content.trim().isEmpty
                              ? '点击查看完整内容并继续修改。'
                              : item.content.trim().replaceAll('\n', ' '),
                          maxLines: 2,
                          overflow: TextOverflow.ellipsis,
                          style: theme.textTheme.bodySmall?.copyWith(
                            color: theme.colorScheme.onSurfaceVariant,
                            height: 1.45,
                          ),
                        ),
                      ],
                    ),
                  ),
                  const SizedBox(width: 10),
                  Switch(
                    value: enabled,
                    onChanged: (_) => onToggleEnabled(),
                  ),
                ],
              ),
              const SizedBox(height: 10),
              Wrap(
                spacing: 8,
                runSpacing: 8,
                children: [
                  _MetaChip(label: 'id', value: item.id),
                  _MetaChip(label: 'source', value: item.source.isEmpty ? '-' : item.source),
                  _MetaChip(label: 'sync', value: item.syncState.isEmpty ? '-' : item.syncState),
                  _MetaChip(label: '状态', value: enabled ? '已启用' : '未启用'),
                ],
              ),
            ],
          ),
        ),
      ),
    );
  }
}

class _HeroCard extends StatelessWidget {
  const _HeroCard({
    required this.title,
    required this.description,
    required this.action,
    required this.chips,
  });

  final String title;
  final String description;
  final Widget action;
  final List<Widget> chips;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return Container(
      width: double.infinity,
      padding: const EdgeInsets.fromLTRB(16, 16, 16, 14),
      decoration: BoxDecoration(
        gradient: const LinearGradient(
          colors: [Color(0xFFF7F9FC), Color(0xFFFFFFFF)],
          begin: Alignment.topLeft,
          end: Alignment.bottomRight,
        ),
        borderRadius: BorderRadius.circular(24),
        border: Border.all(
          color: theme.colorScheme.outlineVariant.withValues(alpha: 0.42),
        ),
      ),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Row(
            children: [
              Expanded(
                child: Text(
                  title,
                  style: theme.textTheme.titleLarge?.copyWith(
                    fontWeight: FontWeight.w800,
                    letterSpacing: -0.2,
                  ),
                ),
              ),
              action,
            ],
          ),
          const SizedBox(height: 6),
          Text(
            description,
            style: theme.textTheme.bodySmall?.copyWith(
              color: theme.colorScheme.onSurfaceVariant,
              height: 1.45,
            ),
          ),
          const SizedBox(height: 12),
          Wrap(spacing: 8, runSpacing: 8, children: chips),
        ],
      ),
    );
  }
}

class _StatusBanner extends StatelessWidget {
  const _StatusBanner({required this.message, required this.tone});

  final String message;
  final Color tone;

  @override
  Widget build(BuildContext context) {
    return Container(
      width: double.infinity,
      padding: const EdgeInsets.all(12),
      decoration: BoxDecoration(
        color: tone.withValues(alpha: 0.1),
        borderRadius: BorderRadius.circular(16),
        border: Border.all(color: tone.withValues(alpha: 0.2)),
      ),
      child: Text(message),
    );
  }
}

class _DetailBlock extends StatelessWidget {
  const _DetailBlock({required this.title, required this.content});

  final String title;
  final String content;

  @override
  Widget build(BuildContext context) {
    return Container(
      width: double.infinity,
      padding: const EdgeInsets.all(12),
      decoration: BoxDecoration(
        color: Theme.of(context).colorScheme.surfaceContainerLowest,
        borderRadius: BorderRadius.circular(18),
      ),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Text(title, style: Theme.of(context).textTheme.labelLarge),
          const SizedBox(height: 8),
          SelectableText(content),
        ],
      ),
    );
  }
}

class _EmptyState extends StatelessWidget {
  const _EmptyState();

  @override
  Widget build(BuildContext context) {
    return Center(
      child: Container(
        padding: const EdgeInsets.all(20),
        decoration: BoxDecoration(
          color: Theme.of(context).colorScheme.surfaceContainerLowest,
          borderRadius: BorderRadius.circular(24),
        ),
        child: const Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            Icon(Icons.psychology_alt_outlined, size: 34),
            SizedBox(height: 10),
            Text('还没有 memory'),
            SizedBox(height: 6),
            Text('先同步，或展开下方卡片手动新增。', textAlign: TextAlign.center),
          ],
        ),
      ),
    );
  }
}

class _FilterChip extends StatelessWidget {
  const _FilterChip({
    required this.label,
    required this.selected,
    required this.onTap,
  });

  final String label;
  final bool selected;
  final VoidCallback onTap;

  @override
  Widget build(BuildContext context) {
    return ChoiceChip(
      label: Text(label),
      selected: selected,
      onSelected: (_) => onTap(),
    );
  }
}

class _MetaChip extends StatelessWidget {
  const _MetaChip({required this.label, required this.value});

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
      child: Text('$label: $value'),
    );
  }
}

String _syncStateLabel(String value) {
  switch (value.trim()) {
    case 'syncing':
      return '同步中';
    case 'synced':
      return '已同步';
    case 'draft':
      return '有本地修改';
    case 'failed':
      return '同步失败';
    case 'drifted':
      return '已漂移';
    default:
      return value.trim().isEmpty ? '-' : value.trim();
  }
}

String _timeLabel(DateTime? value) {
  if (value == null) {
    return '-';
  }
  final local = value.toLocal();
  final month = local.month.toString().padLeft(2, '0');
  final day = local.day.toString().padLeft(2, '0');
  final hour = local.hour.toString().padLeft(2, '0');
  final minute = local.minute.toString().padLeft(2, '0');
  return '$month-$day $hour:$minute';
}

Color _bannerTone(CatalogMetadata meta) {
  switch (meta.syncState.trim()) {
    case 'failed':
      return Colors.red;
    case 'draft':
    case 'drifted':
      return Colors.orange;
    default:
      return Colors.blue;
  }
}
