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
  });

  final List<MemoryItem> items;
  final String syncStatus;
  final CatalogMetadata catalogMeta;
  final List<String> enabledMemoryIds;
  final ValueChanged<String> onToggleEnabled;
  final ValueChanged<MemoryItem> onSave;
  final VoidCallback onSync;

  @override
  State<MemoryManagementSheet> createState() => _MemoryManagementSheetState();
}

class _MemoryManagementSheetState extends State<MemoryManagementSheet> {
  late final TextEditingController _idController;
  late final TextEditingController _titleController;
  late final TextEditingController _contentController;

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
    final theme = Theme.of(context);
    final meta = widget.catalogMeta;
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
                          'Memory 管理',
                          style: theme.textTheme.titleLarge?.copyWith(
                            fontWeight: FontWeight.w800,
                            letterSpacing: -0.2,
                          ),
                        ),
                      ),
                      FilledButton.tonalIcon(
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
                    ],
                  ),
                  const SizedBox(height: 6),
                  Text(
                    '这是 MobileVC 的 Claude memory 镜像视图；启用态只影响当前会话上下文。',
                    style: theme.textTheme.bodySmall?.copyWith(
                      color: theme.colorScheme.onSurfaceVariant,
                      height: 1.45,
                    ),
                  ),
                  const SizedBox(height: 10),
                  Wrap(
                    spacing: 8,
                    runSpacing: 8,
                    children: [
                      _SummaryChip(label: 'sourceOfTruth', value: meta.sourceOfTruth.isEmpty ? '-' : meta.sourceOfTruth),
                      _SummaryChip(label: 'syncState', value: meta.syncState.isEmpty ? '-' : meta.syncState),
                      _SummaryChip(label: 'driftDetected', value: meta.driftDetected ? 'yes' : 'no'),
                      if (meta.lastSyncedAt != null)
                        _SummaryChip(label: 'lastSyncedAt', value: '${meta.lastSyncedAt}'),
                      if (meta.lastError.trim().isNotEmpty)
                        _SummaryChip(label: 'lastError', value: meta.lastError),
                    ],
                  ),
                ],
              ),
            ),
            if (widget.syncStatus.trim().isNotEmpty) ...[
              const SizedBox(height: 10),
              Container(
                width: double.infinity,
                padding: const EdgeInsets.all(12),
                decoration: BoxDecoration(
                  color: theme.colorScheme.surfaceContainerHighest,
                  borderRadius: BorderRadius.circular(16),
                ),
                child: Text(widget.syncStatus),
              ),
            ],
            const SizedBox(height: 12),
            Expanded(
              child: ListView(
                children: [
                  Wrap(
                    spacing: 8,
                    runSpacing: 8,
                    children: [
                      _SummaryChip(label: '总数', value: '${widget.items.length}'),
                      _SummaryChip(
                        label: '已启用',
                        value: '${widget.enabledMemoryIds.length}',
                      ),
                    ],
                  ),
                  const SizedBox(height: 12),
                  ...widget.items.map((item) {
                    final enabled = widget.enabledMemoryIds.contains(item.id);
                    return Card(
                      margin: const EdgeInsets.only(bottom: 10),
                      child: Padding(
                        padding: const EdgeInsets.all(14),
                        child: Column(
                          crossAxisAlignment: CrossAxisAlignment.start,
                          children: [
                            Row(
                              children: [
                                Expanded(
                                  child: Text(
                                    item.title.isEmpty ? item.id : item.title,
                                    style: theme.textTheme.titleSmall?.copyWith(
                                      fontWeight: FontWeight.w700,
                                    ),
                                  ),
                                ),
                                Switch(
                                  value: enabled,
                                  onChanged: (_) => widget.onToggleEnabled(item.id),
                                ),
                              ],
                            ),
                            if (item.id.isNotEmpty) ...[
                              const SizedBox(height: 6),
                              Text('ID: ${item.id}', style: theme.textTheme.bodySmall),
                            ],
                            const SizedBox(height: 8),
                            Wrap(
                              spacing: 8,
                              runSpacing: 8,
                              children: [
                                _SummaryChip(label: 'source', value: item.source.isEmpty ? '-' : item.source),
                                _SummaryChip(label: 'truth', value: item.sourceOfTruth.isEmpty ? '-' : item.sourceOfTruth),
                                _SummaryChip(label: 'sync', value: item.syncState.isEmpty ? '-' : item.syncState),
                                _SummaryChip(label: 'drift', value: item.driftDetected ? 'yes' : 'no'),
                                if (item.lastSyncedAt != null)
                                  _SummaryChip(label: 'lastSyncedAt', value: '${item.lastSyncedAt}'),
                                _SummaryChip(label: '编辑', value: item.editable ? '可编辑' : '只读'),
                              ],
                            ),
                            if (item.content.isNotEmpty) ...[
                              const SizedBox(height: 8),
                              SelectableText(item.content),
                            ],
                            if (item.editable) ...[
                              const SizedBox(height: 10),
                              Align(
                                alignment: Alignment.centerRight,
                                child: TextButton.icon(
                                  onPressed: () => _fillForm(item),
                                  icon: const Icon(Icons.edit_outlined),
                                  label: const Text('编辑'),
                                ),
                              ),
                            ],
                          ],
                        ),
                      ),
                    );
                  }),
                  const SizedBox(height: 8),
                  Text('新增 / 编辑 memory', style: theme.textTheme.titleMedium),
                  const SizedBox(height: 10),
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
                    minLines: 5,
                    maxLines: 10,
                    decoration: const InputDecoration(labelText: 'content'),
                  ),
                  const SizedBox(height: 12),
                  Row(
                    children: [
                      Expanded(
                        child: OutlinedButton(
                          onPressed: _clearForm,
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
              ),
            ),
          ],
        ),
      ),
    );
  }

  void _fillForm(MemoryItem item) {
    _idController.text = item.id;
    _titleController.text = item.title;
    _contentController.text = item.content;
    setState(() {});
  }

  void _clearForm() {
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
    _clearForm();
  }
}

class _SummaryChip extends StatelessWidget {
  const _SummaryChip({required this.label, required this.value});

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
