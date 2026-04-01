import 'package:flutter/material.dart';

import '../../data/models/session_models.dart';

class PermissionRuleManagementSheet extends StatelessWidget {
  const PermissionRuleManagementSheet({
    super.key,
    required this.sessionEnabled,
    required this.persistentEnabled,
    required this.sessionRules,
    required this.persistentRules,
    required this.onSetSessionEnabled,
    required this.onSetPersistentEnabled,
    required this.onToggleRule,
    required this.onDeleteRule,
  });

  final bool sessionEnabled;
  final bool persistentEnabled;
  final List<PermissionRule> sessionRules;
  final List<PermissionRule> persistentRules;
  final ValueChanged<bool> onSetSessionEnabled;
  final ValueChanged<bool> onSetPersistentEnabled;
  final void Function(PermissionRule rule, bool enabled) onToggleRule;
  final ValueChanged<PermissionRule> onDeleteRule;

  @override
  Widget build(BuildContext context) {
    final total = sessionRules.length + persistentRules.length;
    return SafeArea(
      top: false,
      child: DecoratedBox(
        decoration: BoxDecoration(
          gradient: LinearGradient(
            colors: [
              Theme.of(context).colorScheme.primary.withValues(alpha: 0.06),
              Theme.of(context).colorScheme.surface,
            ],
            begin: Alignment.topCenter,
            end: Alignment.bottomCenter,
          ),
        ),
        child: Padding(
          padding: const EdgeInsets.fromLTRB(16, 8, 16, 24),
          child: Column(
            children: [
              _HeaderCard(totalRules: total),
              const SizedBox(height: 12),
              Expanded(
                child: ListView(
                  children: [
                    _ScopeCard(
                      title: '本会话规则',
                      subtitle: '只对当前恢复/运行中的会话生效',
                      enabled: sessionEnabled,
                      rules: sessionRules,
                      onSetEnabled: onSetSessionEnabled,
                      onToggleRule: onToggleRule,
                      onDeleteRule: onDeleteRule,
                    ),
                    const SizedBox(height: 12),
                    _ScopeCard(
                      title: '长期规则',
                      subtitle: '跨设备、跨会话自动允许同类操作',
                      enabled: persistentEnabled,
                      rules: persistentRules,
                      onSetEnabled: onSetPersistentEnabled,
                      onToggleRule: onToggleRule,
                      onDeleteRule: onDeleteRule,
                    ),
                  ],
                ),
              ),
            ],
          ),
        ),
      ),
    );
  }
}

class _HeaderCard extends StatelessWidget {
  const _HeaderCard({required this.totalRules});

  final int totalRules;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return Container(
      width: double.infinity,
      padding: const EdgeInsets.fromLTRB(18, 18, 18, 16),
      decoration: BoxDecoration(
        color: theme.colorScheme.primaryContainer,
        borderRadius: BorderRadius.circular(28),
        border: Border.all(
          color: theme.colorScheme.outlineVariant.withValues(alpha: 0.6),
        ),
      ),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Row(
            children: [
              Container(
                width: 44,
                height: 44,
                decoration: BoxDecoration(
                  color: theme.colorScheme.surface.withValues(alpha: 0.7),
                  borderRadius: BorderRadius.circular(16),
                ),
                child: const Icon(Icons.verified_user_outlined),
              ),
              const SizedBox(width: 12),
              Expanded(
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    Text(
                      '权限管理',
                      style: theme.textTheme.titleLarge?.copyWith(
                        fontWeight: FontWeight.w900,
                      ),
                    ),
                    const SizedBox(height: 4),
                    Text(
                      totalRules == 0
                          ? '当前还没有自动允许规则，后续可在授权时直接记住本次选择。'
                          : '当前共有 $totalRules 条自动允许规则，可单独开关或删除。',
                      style: theme.textTheme.bodySmall?.copyWith(
                        color: theme.colorScheme.onSurfaceVariant,
                        height: 1.35,
                      ),
                    ),
                  ],
                ),
              ),
            ],
          ),
        ],
      ),
    );
  }
}

class _ScopeCard extends StatelessWidget {
  const _ScopeCard({
    required this.title,
    required this.subtitle,
    required this.enabled,
    required this.rules,
    required this.onSetEnabled,
    required this.onToggleRule,
    required this.onDeleteRule,
  });

  final String title;
  final String subtitle;
  final bool enabled;
  final List<PermissionRule> rules;
  final ValueChanged<bool> onSetEnabled;
  final void Function(PermissionRule rule, bool enabled) onToggleRule;
  final ValueChanged<PermissionRule> onDeleteRule;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return Container(
      padding: const EdgeInsets.fromLTRB(16, 16, 16, 14),
      decoration: BoxDecoration(
        color: theme.colorScheme.surface,
        borderRadius: BorderRadius.circular(24),
        border: Border.all(
          color: theme.colorScheme.outlineVariant.withValues(alpha: 0.5),
        ),
      ),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Row(
            children: [
              Expanded(
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    Text(
                      title,
                      style: theme.textTheme.titleMedium?.copyWith(
                        fontWeight: FontWeight.w900,
                      ),
                    ),
                    const SizedBox(height: 4),
                    Text(
                      subtitle,
                      style: theme.textTheme.bodySmall?.copyWith(
                        color: theme.colorScheme.onSurfaceVariant,
                        height: 1.35,
                      ),
                    ),
                  ],
                ),
              ),
              Switch.adaptive(
                key: ValueKey<String>('permissionRules.scope.${title.trim()}'),
                value: enabled,
                onChanged: onSetEnabled,
              ),
            ],
          ),
          const SizedBox(height: 12),
          if (rules.isEmpty)
            Text(
              '暂无规则',
              style: theme.textTheme.bodySmall?.copyWith(
                color: theme.colorScheme.onSurfaceVariant,
              ),
            )
          else
            Column(
              children: rules
                  .map(
                    (rule) => Padding(
                      padding: const EdgeInsets.only(bottom: 10),
                      child: _RuleTile(
                        rule: rule,
                        onToggleRule: onToggleRule,
                        onDeleteRule: onDeleteRule,
                      ),
                    ),
                  )
                  .toList(growable: false),
            ),
        ],
      ),
    );
  }
}

class _RuleTile extends StatelessWidget {
  const _RuleTile({
    required this.rule,
    required this.onToggleRule,
    required this.onDeleteRule,
  });

  final PermissionRule rule;
  final void Function(PermissionRule rule, bool enabled) onToggleRule;
  final ValueChanged<PermissionRule> onDeleteRule;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final meta = <String>[
      if (rule.engine.trim().isNotEmpty) rule.engine.trim(),
      if (rule.kind.trim().isNotEmpty) rule.kind.trim(),
      if (rule.commandHead.trim().isNotEmpty) rule.commandHead.trim(),
      if (rule.targetPathPrefix.trim().isNotEmpty) rule.targetPathPrefix.trim(),
    ];
    final stats = <String>[
      if (rule.matchCount > 0) '命中 ${rule.matchCount} 次',
      if (rule.lastMatchedAt != null) '最近 ${_formatDate(rule.lastMatchedAt!)}',
    ];
    return Container(
      padding: const EdgeInsets.fromLTRB(12, 12, 12, 10),
      decoration: BoxDecoration(
        color: theme.colorScheme.surfaceContainerLowest,
        borderRadius: BorderRadius.circular(18),
        border: Border.all(
          color: theme.colorScheme.outlineVariant.withValues(alpha: 0.35),
        ),
      ),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Row(
            children: [
              Expanded(
                child: Text(
                  rule.displayTitle,
                  style: theme.textTheme.titleSmall?.copyWith(
                    fontWeight: FontWeight.w800,
                  ),
                ),
              ),
              Switch.adaptive(
                key: ValueKey<String>('permissionRules.rule.${rule.id}'),
                value: rule.enabled,
                onChanged: (value) => onToggleRule(rule, value),
              ),
              IconButton(
                key: ValueKey<String>('permissionRules.delete.${rule.id}'),
                onPressed: () => onDeleteRule(rule),
                icon: const Icon(Icons.delete_outline_rounded),
                tooltip: '删除规则',
              ),
            ],
          ),
          if (meta.isNotEmpty) ...[
            const SizedBox(height: 4),
            Text(
              meta.join(' · '),
              style: theme.textTheme.bodySmall?.copyWith(
                color: theme.colorScheme.onSurfaceVariant,
                height: 1.35,
              ),
            ),
          ],
          if (stats.isNotEmpty) ...[
            const SizedBox(height: 6),
            Text(
              stats.join(' · '),
              style: theme.textTheme.labelSmall?.copyWith(
                color: theme.colorScheme.primary,
                fontWeight: FontWeight.w700,
              ),
            ),
          ],
        ],
      ),
    );
  }

  String _formatDate(DateTime value) {
    return '${value.month.toString().padLeft(2, '0')}-${value.day.toString().padLeft(2, '0')} ${value.hour.toString().padLeft(2, '0')}:${value.minute.toString().padLeft(2, '0')}';
  }
}
