import 'package:flutter/material.dart';

import '../../data/models/session_models.dart';

class TerminalLogSheet extends StatelessWidget {
  const TerminalLogSheet({
    super.key,
    required this.executions,
    required this.activeExecutionId,
    required this.stdout,
    required this.stderr,
    this.onSelectExecution,
  });

  final List<TerminalExecution> executions;
  final String activeExecutionId;
  final String stdout;
  final String stderr;
  final ValueChanged<String>? onSelectExecution;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final hasExecutions = executions.isNotEmpty;
    final activeExecution = _activeExecution();
    final resolvedStdout = activeExecution?.stdout ?? stdout;
    final resolvedStderr = activeExecution?.stderr ?? stderr;
    return DefaultTabController(
      length: 2,
      child: SafeArea(
        top: false,
        child: Padding(
          padding: const EdgeInsets.fromLTRB(16, 6, 16, 24),
          child: Column(
            children: [
              Row(
                children: [
                  Expanded(
                    child: Column(
                      crossAxisAlignment: CrossAxisAlignment.start,
                      children: [
                        Text('运行日志', style: theme.textTheme.titleLarge),
                        const SizedBox(height: 4),
                        Text(
                          hasExecutions
                              ? '按命令查看执行详情、来源以及 stdout / stderr。'
                              : '当前仅有原始 stdout / stderr 日志。',
                          style: theme.textTheme.bodySmall?.copyWith(
                            color: theme.colorScheme.onSurfaceVariant,
                          ),
                        ),
                      ],
                    ),
                  ),
                ],
              ),
              if (hasExecutions) ...[
                const SizedBox(height: 12),
                SizedBox(
                  height: 188,
                  child: ListView.separated(
                    itemCount: executions.length,
                    separatorBuilder: (_, __) => const SizedBox(height: 8),
                    itemBuilder: (context, index) {
                      final item = executions[index];
                      final selected = item.executionId == activeExecutionId;
                      return _ExecutionCard(
                        item: item,
                        selected: selected,
                        onTap: () => onSelectExecution?.call(item.executionId),
                      );
                    },
                  ),
                ),
              ],
              if (activeExecution != null) ...[
                const SizedBox(height: 12),
                _ExecutionDetailCard(item: activeExecution),
              ],
              const SizedBox(height: 12),
              const TabBar(
                tabs: [
                  Tab(text: 'stdout'),
                  Tab(text: 'stderr'),
                ],
              ),
              const SizedBox(height: 12),
              Expanded(
                child: TabBarView(
                  children: [
                    _LogPane(text: resolvedStdout),
                    _LogPane(text: resolvedStderr),
                  ],
                ),
              ),
            ],
          ),
        ),
      ),
    );
  }

  TerminalExecution? _activeExecution() {
    final normalized = activeExecutionId.trim();
    if (normalized.isNotEmpty) {
      for (final item in executions) {
        if (item.executionId == normalized) {
          return item;
        }
      }
    }
    return executions.isEmpty ? null : executions.first;
  }
}

class _ExecutionCard extends StatelessWidget {
  const _ExecutionCard({
    required this.item,
    required this.selected,
    required this.onTap,
  });

  final TerminalExecution item;
  final bool selected;
  final VoidCallback onTap;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final subtitle = <String>[
      if (item.cwd.trim().isNotEmpty) item.cwd.trim(),
      if (item.sourceLabel.trim().isNotEmpty) item.sourceLabel.trim(),
      if (item.running)
        '运行中'
      else if (item.exitCode != null)
        'exit ${item.exitCode}',
    ];
    return Material(
      color: selected
          ? theme.colorScheme.primaryContainer
          : theme.colorScheme.surfaceContainerLow,
      borderRadius: BorderRadius.circular(16),
      child: InkWell(
        onTap: onTap,
        borderRadius: BorderRadius.circular(16),
        child: Padding(
          padding: const EdgeInsets.all(12),
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Text(
                item.title,
                maxLines: 2,
                overflow: TextOverflow.ellipsis,
                style: theme.textTheme.titleSmall?.copyWith(
                  fontWeight: FontWeight.w700,
                ),
              ),
              if (subtitle.isNotEmpty) ...[
                const SizedBox(height: 6),
                Text(
                  subtitle.join(' · '),
                  maxLines: 2,
                  overflow: TextOverflow.ellipsis,
                  style: theme.textTheme.bodySmall?.copyWith(
                    color: theme.colorScheme.onSurfaceVariant,
                  ),
                ),
              ],
            ],
          ),
        ),
      ),
    );
  }
}

class _ExecutionDetailCard extends StatelessWidget {
  const _ExecutionDetailCard({required this.item});

  final TerminalExecution item;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return Container(
      width: double.infinity,
      padding: const EdgeInsets.all(14),
      decoration: BoxDecoration(
        color: theme.colorScheme.surfaceContainerLow,
        borderRadius: BorderRadius.circular(18),
        border: Border.all(
          color: theme.colorScheme.outlineVariant.withValues(alpha: 0.45),
        ),
      ),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Text(
            item.title,
            style: theme.textTheme.titleMedium?.copyWith(
              fontWeight: FontWeight.w800,
            ),
          ),
          const SizedBox(height: 10),
          Wrap(
            spacing: 8,
            runSpacing: 8,
            children: [
              _MetaChip(label: '状态', value: _statusLabel(item)),
              if (item.cwd.trim().isNotEmpty)
                _MetaChip(label: 'CWD', value: item.cwd.trim()),
              if (_sourceLabel(item).isNotEmpty)
                _MetaChip(label: '来源', value: _sourceLabel(item)),
              if (item.contextTitle.trim().isNotEmpty)
                _MetaChip(label: '上下文', value: item.contextTitle.trim()),
              if (item.groupTitle.trim().isNotEmpty)
                _MetaChip(label: '修改组', value: item.groupTitle.trim()),
              if (_timeRangeLabel(item).isNotEmpty)
                _MetaChip(label: '时间', value: _timeRangeLabel(item)),
            ],
          ),
        ],
      ),
    );
  }

  String _statusLabel(TerminalExecution item) {
    if (item.running) {
      return '运行中';
    }
    if (item.exitCode != null) {
      return 'exit ${item.exitCode}';
    }
    return '已结束';
  }

  String _sourceLabel(TerminalExecution item) {
    if (item.sourceLabel.trim().isNotEmpty) {
      return item.sourceLabel.trim();
    }
    return item.source.trim();
  }

  String _timeRangeLabel(TerminalExecution item) {
    final started = item.startedAt;
    final completed = item.completedAt;
    if (started == null && completed == null) {
      return '';
    }
    final parts = <String>[];
    if (started != null) {
      parts.add(_formatTime(started));
    }
    if (completed != null) {
      parts.add(_formatTime(completed));
    }
    return parts.join(' → ');
  }

  String _formatTime(DateTime value) {
    return '${value.month.toString().padLeft(2, '0')}-${value.day.toString().padLeft(2, '0')} ${value.hour.toString().padLeft(2, '0')}:${value.minute.toString().padLeft(2, '0')}';
  }
}

class _LogPane extends StatelessWidget {
  const _LogPane({required this.text});

  final String text;

  @override
  Widget build(BuildContext context) {
    if (text.isEmpty) {
      return const Center(child: Text('暂无日志'));
    }
    return Container(
      width: double.infinity,
      padding: const EdgeInsets.all(14),
      decoration: BoxDecoration(
        color: const Color(0xFF0F172A),
        borderRadius: BorderRadius.circular(18),
      ),
      child: SingleChildScrollView(
        child: SelectableText(
          text,
          style: const TextStyle(
            color: Color(0xFFE2E8F0),
            fontFamily: 'monospace',
            height: 1.45,
          ),
        ),
      ),
    );
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
