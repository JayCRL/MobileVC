import 'package:flutter/material.dart';

import '../../data/models/session_models.dart';

class SkillManagementSheet extends StatefulWidget {
  const SkillManagementSheet({
    super.key,
    required this.skills,
    required this.enabledSkillNames,
    required this.syncStatus,
    required this.catalogMeta,
    required this.onToggleEnabled,
    required this.onSave,
    required this.onSync,
  });

  final List<SkillDefinition> skills;
  final List<String> enabledSkillNames;
  final String syncStatus;
  final CatalogMetadata catalogMeta;
  final ValueChanged<String> onToggleEnabled;
  final ValueChanged<SkillDefinition> onSave;
  final VoidCallback onSync;

  @override
  State<SkillManagementSheet> createState() => _SkillManagementSheetState();
}

class _SkillManagementSheetState extends State<SkillManagementSheet> {
  late final TextEditingController _nameController;
  late final TextEditingController _descriptionController;
  late final TextEditingController _promptController;
  late final TextEditingController _targetTypeController;
  late final TextEditingController _resultViewController;

  @override
  void initState() {
    super.initState();
    _nameController = TextEditingController();
    _descriptionController = TextEditingController();
    _promptController = TextEditingController();
    _targetTypeController = TextEditingController();
    _resultViewController = TextEditingController();
  }

  @override
  void dispose() {
    _nameController.dispose();
    _descriptionController.dispose();
    _promptController.dispose();
    _targetTypeController.dispose();
    _resultViewController.dispose();
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
                          'Skill 管理',
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
                        label: Text(meta.isSyncing ? '同步中' : '同步 skill'),
                      ),
                    ],
                  ),
                  const SizedBox(height: 6),
                  Text(
                    '管理当前可用的 Claude skill，并控制本会话启用哪些 skill。',
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
                      _SummaryChip(label: '总数', value: '${widget.skills.length}'),
                      _SummaryChip(
                        label: '已启用',
                        value: '${widget.enabledSkillNames.length}',
                      ),
                    ],
                  ),
                  const SizedBox(height: 12),
                  ...widget.skills.map((item) {
                    final enabled = widget.enabledSkillNames.contains(item.name);
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
                                    item.name.isEmpty ? '(未命名 skill)' : item.name,
                                    style: theme.textTheme.titleSmall?.copyWith(
                                      fontWeight: FontWeight.w700,
                                    ),
                                  ),
                                ),
                                Switch(
                                  value: enabled,
                                  onChanged: (_) => widget.onToggleEnabled(item.name),
                                ),
                              ],
                            ),
                            if (item.description.isNotEmpty) ...[
                              const SizedBox(height: 6),
                              Text(item.description),
                            ],
                            const SizedBox(height: 8),
                            Wrap(
                              spacing: 8,
                              runSpacing: 8,
                              children: [
                                _SummaryChip(label: 'target', value: item.targetType.isEmpty ? '-' : item.targetType),
                                _SummaryChip(label: 'view', value: item.resultView.isEmpty ? '-' : item.resultView),
                                _SummaryChip(label: 'source', value: item.source.isEmpty ? '-' : item.source),
                                _SummaryChip(label: 'truth', value: item.sourceOfTruth.isEmpty ? '-' : item.sourceOfTruth),
                                _SummaryChip(label: 'sync', value: item.syncState.isEmpty ? '-' : item.syncState),
                                _SummaryChip(label: 'drift', value: item.driftDetected ? 'yes' : 'no'),
                                if (item.lastSyncedAt != null)
                                  _SummaryChip(label: 'lastSyncedAt', value: '${item.lastSyncedAt}'),
                                _SummaryChip(label: '编辑', value: item.editable ? '可编辑' : '只读'),
                              ],
                            ),
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
                  Text('新增 / 编辑本地 skill', style: theme.textTheme.titleMedium),
                  const SizedBox(height: 10),
                  TextField(
                    controller: _nameController,
                    decoration: const InputDecoration(labelText: 'name'),
                  ),
                  const SizedBox(height: 10),
                  TextField(
                    controller: _descriptionController,
                    decoration: const InputDecoration(labelText: 'description'),
                  ),
                  const SizedBox(height: 10),
                  TextField(
                    controller: _targetTypeController,
                    decoration: const InputDecoration(labelText: 'targetType'),
                  ),
                  const SizedBox(height: 10),
                  TextField(
                    controller: _resultViewController,
                    decoration: const InputDecoration(labelText: 'resultView'),
                  ),
                  const SizedBox(height: 10),
                  TextField(
                    controller: _promptController,
                    minLines: 5,
                    maxLines: 10,
                    decoration: const InputDecoration(labelText: 'prompt'),
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
                          child: const Text('保存 skill'),
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

  void _fillForm(SkillDefinition item) {
    _nameController.text = item.name;
    _descriptionController.text = item.description;
    _promptController.text = item.prompt;
    _targetTypeController.text = item.targetType;
    _resultViewController.text = item.resultView;
    setState(() {});
  }

  void _clearForm() {
    _nameController.clear();
    _descriptionController.clear();
    _promptController.clear();
    _targetTypeController.clear();
    _resultViewController.clear();
    setState(() {});
  }

  void _save() {
    final name = _nameController.text.trim();
    if (name.isEmpty) {
      return;
    }
    widget.onSave(
      SkillDefinition(
        name: name,
        description: _descriptionController.text.trim(),
        prompt: _promptController.text.trim(),
        targetType: _targetTypeController.text.trim(),
        resultView: _resultViewController.text.trim(),
        editable: true,
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
