import 'package:flutter/material.dart';

import '../../data/models/session_models.dart';

class MemoryManagementSheet extends StatefulWidget {
  const MemoryManagementSheet({
    super.key,
    required this.items,
    required this.enabledMemoryIds,
    required this.onToggleEnabled,
    required this.onSave,
  });

  final List<MemoryItem> items;
  final List<String> enabledMemoryIds;
  final ValueChanged<String> onToggleEnabled;
  final ValueChanged<MemoryItem> onSave;

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
                  Text(
                    'Memory 管理',
                    style: theme.textTheme.titleLarge?.copyWith(
                      fontWeight: FontWeight.w800,
                      letterSpacing: -0.2,
                    ),
                  ),
                  const SizedBox(height: 6),
                  Text(
                    '这是 MobileVC 内部显式记忆层，不是 Claude 隐式 /memory。',
                    style: theme.textTheme.bodySmall?.copyWith(
                      color: theme.colorScheme.onSurfaceVariant,
                      height: 1.45,
                    ),
                  ),
                ],
              ),
            ),
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
                            if (item.content.isNotEmpty) ...[
                              const SizedBox(height: 8),
                              SelectableText(item.content),
                            ],
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
