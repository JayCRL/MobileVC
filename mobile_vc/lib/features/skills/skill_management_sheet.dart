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
    required this.onExecuteSkill,
    required this.onGenerateSkill,
    required this.onReviseSkill,
  });

  final List<SkillDefinition> skills;
  final List<String> enabledSkillNames;
  final String syncStatus;
  final CatalogMetadata catalogMeta;
  final ValueChanged<String> onToggleEnabled;
  final ValueChanged<SkillDefinition> onSave;
  final VoidCallback onSync;
  final ValueChanged<String> onExecuteSkill;
  final ValueChanged<String> onGenerateSkill;
  final void Function(SkillDefinition skill, String request) onReviseSkill;

  @override
  State<SkillManagementSheet> createState() => _SkillManagementSheetState();
}

enum _SkillFilter { all, enabled, editable }

class _SkillManagementSheetState extends State<SkillManagementSheet> {
  _SkillFilter _filter = _SkillFilter.all;

  List<SkillDefinition> get _filteredSkills {
    switch (_filter) {
      case _SkillFilter.enabled:
        return widget.skills
            .where((item) => widget.enabledSkillNames.contains(item.name))
            .toList(growable: false);
      case _SkillFilter.editable:
        return widget.skills
            .where((item) => item.editable)
            .toList(growable: false);
      case _SkillFilter.all:
        return widget.skills;
    }
  }

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final meta = widget.catalogMeta;
    final enabledCount = widget.enabledSkillNames.length;
    final items = _filteredSkills;
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
              title: 'Skill 管理',
              description: '这里是当前会话唯一的 skill 启用入口。轻点即可执行，开关控制它是否持续参与当前会话。',
              primaryAction: FilledButton.tonalIcon(
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
              secondaryAction: FilledButton.icon(
                key: const ValueKey('skillManagement.generateButton'),
                onPressed: () => _openGenerateSheet(context),
                icon: const Icon(Icons.auto_awesome),
                label: const Text('新建 skill'),
              ),
              chips: [
                _MetaChip(label: '总数', value: '${widget.skills.length}'),
                _MetaChip(label: '已启用', value: '$enabledCount'),
                _MetaChip(label: '状态', value: _syncStateLabel(meta.syncState)),
                if (meta.lastSyncedAt != null)
                  _MetaChip(
                      label: '最近同步', value: _timeLabel(meta.lastSyncedAt)),
              ],
            ),
            if (widget.syncStatus.trim().isNotEmpty) ...[
              const SizedBox(height: 10),
              _StatusBanner(
                message: widget.syncStatus,
                tone: _bannerTone(meta),
              ),
            ],
            const SizedBox(height: 12),
            SingleChildScrollView(
              scrollDirection: Axis.horizontal,
              child: Row(
                children: [
                  _FilterChip(
                    label: '全部',
                    selected: _filter == _SkillFilter.all,
                    onTap: () => setState(() => _filter = _SkillFilter.all),
                  ),
                  const SizedBox(width: 8),
                  _FilterChip(
                    label: '已启用',
                    selected: _filter == _SkillFilter.enabled,
                    onTap: () => setState(() => _filter = _SkillFilter.enabled),
                  ),
                  const SizedBox(width: 8),
                  _FilterChip(
                    label: '可编辑',
                    selected: _filter == _SkillFilter.editable,
                    onTap: () =>
                        setState(() => _filter = _SkillFilter.editable),
                  ),
                ],
              ),
            ),
            const SizedBox(height: 12),
            Expanded(
              child: items.isEmpty
                  ? _EmptyState(
                      title: widget.skills.isEmpty
                          ? '还没有可用 skill'
                          : '当前筛选下没有 skill',
                      description: widget.skills.isEmpty
                          ? '你可以先同步，也可以直接一句话让 Claude 帮你生成新的 skill。'
                          : '切换筛选条件，或直接新建一个适合当前任务的 skill。',
                      actionLabel: '一句话生成 skill',
                      onAction: () => _openGenerateSheet(context),
                    )
                  : ListView.separated(
                      itemCount: items.length,
                      separatorBuilder: (context, index) =>
                          const SizedBox(height: 10),
                      itemBuilder: (context, index) {
                        final item = items[index];
                        final enabled =
                            widget.enabledSkillNames.contains(item.name);
                        return _SkillCapsuleCard(
                          skill: item,
                          enabled: enabled,
                          onTap: item.name.trim().isEmpty
                              ? null
                              : () => widget.onExecuteSkill(item.name),
                          onLongPress: () => _openDetailSheet(context, item),
                          onToggleEnabled: () =>
                              widget.onToggleEnabled(item.name),
                        );
                      },
                    ),
            ),
          ],
        ),
      ),
    );
  }

  void _openGenerateSheet(BuildContext context) {
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
                '一句话生成新 skill',
                style: Theme.of(context)
                    .textTheme
                    .titleLarge
                    ?.copyWith(fontWeight: FontWeight.w800),
              ),
              const SizedBox(height: 8),
              Text(
                '描述你想让 Claude 以后反复复用的能力，例如“生成适合移动端 diff 审阅的总结 skill”。',
                style: Theme.of(context).textTheme.bodySmall,
              ),
              const SizedBox(height: 12),
              TextField(
                key: const ValueKey('skillManagement.generateInput'),
                controller: controller,
                minLines: 3,
                maxLines: 6,
                decoration: const InputDecoration(
                  hintText: '例如：创建一个专门总结 Flutter Widget 测试失败原因的 skill',
                ),
              ),
              const SizedBox(height: 12),
              SizedBox(
                width: double.infinity,
                child: FilledButton.icon(
                  onPressed: () {
                    final value = controller.text.trim();
                    if (value.isEmpty) {
                      return;
                    }
                    widget.onGenerateSkill(value);
                    Navigator.of(context).pop();
                  },
                  icon: const Icon(Icons.auto_awesome),
                  label: const Text('交给 Claude 生成'),
                ),
              ),
            ],
          ),
        );
      },
    );
  }

  void _openDetailSheet(BuildContext context, SkillDefinition item) {
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
                item.name.isEmpty ? '未命名 skill' : item.name,
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
                  _MetaChip(
                      label: 'target',
                      value: item.targetType.isEmpty ? '-' : item.targetType),
                  _MetaChip(
                      label: 'view',
                      value: item.resultView.isEmpty ? '-' : item.resultView),
                  _MetaChip(
                      label: 'source',
                      value: item.source.isEmpty ? '-' : item.source),
                  _MetaChip(label: '编辑', value: item.editable ? '可编辑' : '只读'),
                ],
              ),
              if (item.description.trim().isNotEmpty) ...[
                const SizedBox(height: 12),
                _DetailBlock(title: '说明', content: item.description.trim()),
              ],
              if (item.prompt.trim().isNotEmpty) ...[
                const SizedBox(height: 12),
                _DetailBlock(title: 'Prompt', content: item.prompt.trim()),
              ],
              const SizedBox(height: 12),
              TextField(
                key: ValueKey('skillDetail.modifyInput:${item.name}'),
                controller: controller,
                minLines: 3,
                maxLines: 6,
                enabled: item.editable,
                decoration: InputDecoration(
                  hintText: item.editable
                      ? '一句话告诉 Claude 你想怎么修改这个 skill'
                      : '该 skill 为只读，不能直接修改',
                ),
              ),
              const SizedBox(height: 12),
              Row(
                children: [
                  Expanded(
                    child: OutlinedButton.icon(
                      onPressed: () {
                        widget.onExecuteSkill(item.name);
                        Navigator.of(context).pop();
                      },
                      icon: const Icon(Icons.play_arrow),
                      label: const Text('立即执行'),
                    ),
                  ),
                  const SizedBox(width: 10),
                  Expanded(
                    child: FilledButton.icon(
                      onPressed: !item.editable
                          ? null
                          : () {
                              final value = controller.text.trim();
                              if (value.isEmpty) {
                                return;
                              }
                              widget.onReviseSkill(item, value);
                              Navigator.of(context).pop();
                            },
                      icon: const Icon(Icons.edit_outlined),
                      label: const Text('让 Claude 修改'),
                    ),
                  ),
                ],
              ),
            ],
          ),
        );
      },
    );
  }
}

class _SkillCapsuleCard extends StatelessWidget {
  const _SkillCapsuleCard({
    required this.skill,
    required this.enabled,
    required this.onTap,
    required this.onLongPress,
    required this.onToggleEnabled,
  });

  final SkillDefinition skill;
  final bool enabled;
  final VoidCallback? onTap;
  final VoidCallback onLongPress;
  final VoidCallback onToggleEnabled;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return Material(
      color: Colors.transparent,
      child: InkWell(
        key: ValueKey('skillCapsule:${skill.name}'),
        borderRadius: BorderRadius.circular(24),
        onTap: onTap,
        onLongPress: onLongPress,
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
            boxShadow: [
              BoxShadow(
                color: Colors.black.withValues(alpha: 0.03),
                blurRadius: 16,
                offset: const Offset(0, 8),
              ),
            ],
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
                          skill.name.isEmpty ? '未命名 skill' : skill.name,
                          style: theme.textTheme.titleMedium?.copyWith(
                            fontWeight: FontWeight.w800,
                          ),
                        ),
                        const SizedBox(height: 6),
                        Text(
                          skill.description.trim().isNotEmpty
                              ? skill.description.trim()
                              : '轻点执行，长按查看详情与修改入口。',
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
                  _MetaChip(
                      label: 'target',
                      value: skill.targetType.isEmpty ? '-' : skill.targetType),
                  _MetaChip(
                      label: 'source',
                      value: skill.source.isEmpty ? '-' : skill.source),
                  _MetaChip(
                      label: 'sync',
                      value: skill.syncState.isEmpty ? '-' : skill.syncState),
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
    required this.primaryAction,
    required this.secondaryAction,
    required this.chips,
  });

  final String title;
  final String description;
  final Widget primaryAction;
  final Widget secondaryAction;
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
          Text(
            title,
            style: theme.textTheme.titleLarge?.copyWith(
              fontWeight: FontWeight.w800,
              letterSpacing: -0.2,
            ),
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
          Wrap(
            spacing: 8,
            runSpacing: 8,
            children: chips,
          ),
          const SizedBox(height: 12),
          Wrap(
            spacing: 10,
            runSpacing: 10,
            children: [primaryAction, secondaryAction],
          ),
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
    final theme = Theme.of(context);
    return Container(
      width: double.infinity,
      padding: const EdgeInsets.all(12),
      decoration: BoxDecoration(
        color: theme.colorScheme.surfaceContainerLowest,
        borderRadius: BorderRadius.circular(18),
        border: Border.all(
          color: theme.colorScheme.outlineVariant.withValues(alpha: 0.4),
        ),
      ),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Text(
            title,
            style: theme.textTheme.labelLarge
                ?.copyWith(fontWeight: FontWeight.w700),
          ),
          const SizedBox(height: 8),
          SelectableText(content),
        ],
      ),
    );
  }
}

class _EmptyState extends StatelessWidget {
  const _EmptyState({
    required this.title,
    required this.description,
    required this.actionLabel,
    required this.onAction,
  });

  final String title;
  final String description;
  final String actionLabel;
  final VoidCallback onAction;

  @override
  Widget build(BuildContext context) {
    return Center(
      child: Container(
        padding: const EdgeInsets.all(20),
        decoration: BoxDecoration(
          color: Theme.of(context).colorScheme.surfaceContainerLowest,
          borderRadius: BorderRadius.circular(24),
        ),
        child: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            const Icon(Icons.extension_outlined, size: 34),
            const SizedBox(height: 10),
            Text(title, style: Theme.of(context).textTheme.titleMedium),
            const SizedBox(height: 6),
            Text(description, textAlign: TextAlign.center),
            const SizedBox(height: 12),
            FilledButton(onPressed: onAction, child: Text(actionLabel)),
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
