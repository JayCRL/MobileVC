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
    final stdoutLines = _lineCount(resolvedStdout);
    final stderrLines = _lineCount(resolvedStderr);
    return DefaultTabController(
      length: 2,
      child: SafeArea(
        top: false,
        child: DecoratedBox(
          decoration: BoxDecoration(
            gradient: LinearGradient(
              colors: [
                theme.colorScheme.primary.withValues(alpha: 0.06),
                theme.colorScheme.surface,
                theme.colorScheme.surface,
              ],
              begin: Alignment.topCenter,
              end: Alignment.bottomCenter,
            ),
          ),
          child: Padding(
            padding: const EdgeInsets.fromLTRB(16, 6, 16, 24),
            child: Column(
              children: [
                _HeroCard(
                  hasExecutions: hasExecutions,
                  executionCount: executions.length,
                  activeExecution: activeExecution,
                  stdoutLines: stdoutLines,
                  stderrLines: stderrLines,
                ),
                if (hasExecutions) ...[
                  const SizedBox(height: 12),
                  SizedBox(
                    height: 196,
                    child: ListView.separated(
                      itemCount: executions.length,
                      separatorBuilder: (_, __) => const SizedBox(height: 10),
                      itemBuilder: (context, index) {
                        final item = executions[index];
                        final selected = item.executionId == activeExecutionId;
                        return _ExecutionCard(
                          item: item,
                          selected: selected,
                          onTap: () =>
                              onSelectExecution?.call(item.executionId),
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
                _LogTabs(),
                const SizedBox(height: 12),
                Expanded(
                  child: TabBarView(
                    children: [
                      _LogPane(
                        label: 'stdout',
                        text: resolvedStdout,
                        accent: const Color(0xFF22C55E),
                        emptyLabel: 'stdout 暂无输出',
                      ),
                      _LogPane(
                        label: 'stderr',
                        text: resolvedStderr,
                        accent: const Color(0xFFF97316),
                        emptyLabel: 'stderr 暂无输出',
                      ),
                    ],
                  ),
                ),
              ],
            ),
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

class _HeroCard extends StatelessWidget {
  const _HeroCard({
    required this.hasExecutions,
    required this.executionCount,
    required this.activeExecution,
    required this.stdoutLines,
    required this.stderrLines,
  });

  final bool hasExecutions;
  final int executionCount;
  final TerminalExecution? activeExecution;
  final int stdoutLines;
  final int stderrLines;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return Container(
      width: double.infinity,
      padding: const EdgeInsets.fromLTRB(18, 18, 18, 16),
      decoration: BoxDecoration(
        gradient: LinearGradient(
          colors: [
            theme.colorScheme.primaryContainer,
            Color.alphaBlend(
              theme.colorScheme.primary.withValues(alpha: 0.08),
              theme.colorScheme.surface,
            ),
          ],
          begin: Alignment.topLeft,
          end: Alignment.bottomRight,
        ),
        borderRadius: BorderRadius.circular(28),
        border: Border.all(
          color: theme.colorScheme.outlineVariant.withValues(alpha: 0.7),
        ),
        boxShadow: [
          BoxShadow(
            color: theme.colorScheme.primary.withValues(alpha: 0.10),
            blurRadius: 24,
            offset: const Offset(0, 12),
          ),
        ],
      ),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Row(
            children: [
              Container(
                width: 44,
                height: 44,
                alignment: Alignment.center,
                decoration: BoxDecoration(
                  color: theme.colorScheme.surface.withValues(alpha: 0.72),
                  borderRadius: BorderRadius.circular(16),
                ),
                child: const Icon(Icons.terminal_rounded),
              ),
              const SizedBox(width: 12),
              Expanded(
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    Text(
                      '运行日志',
                      style: theme.textTheme.titleLarge?.copyWith(
                        fontWeight: FontWeight.w900,
                        letterSpacing: -0.3,
                      ),
                    ),
                    const SizedBox(height: 4),
                    Text(
                      hasExecutions
                          ? '按命令查看执行详情、状态与 stdout / stderr。'
                          : '当前仅有原始 stdout / stderr 输出。',
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
          const SizedBox(height: 14),
          Wrap(
            spacing: 8,
            runSpacing: 8,
            children: [
              _MetaChip(label: '命令数', value: '$executionCount'),
              _MetaChip(
                label: '当前',
                value: activeExecution?.title.trim().isNotEmpty == true
                    ? activeExecution!.title
                    : '原始输出',
              ),
              _MetaChip(label: 'stdout', value: '$stdoutLines 行'),
              _MetaChip(label: 'stderr', value: '$stderrLines 行'),
            ],
          ),
        ],
      ),
    );
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
    final accent = item.running
        ? const Color(0xFF22C55E)
        : (item.exitCode == null || item.exitCode == 0)
            ? theme.colorScheme.primary
            : const Color(0xFFF97316);
    return Material(
      color: Colors.transparent,
      child: InkWell(
        onTap: onTap,
        borderRadius: BorderRadius.circular(22),
        child: Ink(
          decoration: BoxDecoration(
            gradient: LinearGradient(
              colors: [
                selected
                    ? theme.colorScheme.primary.withValues(alpha: 0.12)
                    : theme.colorScheme.surface,
                theme.colorScheme.surface,
              ],
              begin: Alignment.centerLeft,
              end: Alignment.centerRight,
            ),
            borderRadius: BorderRadius.circular(22),
            border: Border.all(
              color: selected
                  ? theme.colorScheme.primary.withValues(alpha: 0.42)
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
          child: Padding(
            padding: const EdgeInsets.fromLTRB(14, 14, 14, 12),
            child: Row(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Container(
                  width: 10,
                  height: 10,
                  margin: const EdgeInsets.only(top: 6),
                  decoration: BoxDecoration(
                    color: accent,
                    shape: BoxShape.circle,
                    boxShadow: [
                      BoxShadow(
                        color: accent.withValues(alpha: 0.35),
                        blurRadius: 12,
                      ),
                    ],
                  ),
                ),
                const SizedBox(width: 12),
                Expanded(
                  child: Column(
                    crossAxisAlignment: CrossAxisAlignment.start,
                    children: [
                      Text(
                        item.title,
                        maxLines: 2,
                        overflow: TextOverflow.ellipsis,
                        style: theme.textTheme.titleSmall?.copyWith(
                          fontWeight: FontWeight.w800,
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
                            height: 1.35,
                          ),
                        ),
                      ],
                    ],
                  ),
                ),
                const SizedBox(width: 10),
                _StatusPill(item: item),
              ],
            ),
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
      padding: const EdgeInsets.fromLTRB(16, 16, 16, 14),
      decoration: BoxDecoration(
        color: theme.colorScheme.surface.withValues(alpha: 0.9),
        borderRadius: BorderRadius.circular(24),
        border: Border.all(
          color: theme.colorScheme.outlineVariant.withValues(alpha: 0.45),
        ),
        boxShadow: [
          BoxShadow(
            color: Colors.black.withValues(alpha: 0.03),
            blurRadius: 18,
            offset: const Offset(0, 8),
          ),
        ],
      ),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Row(
            children: [
              Expanded(
                child: Text(
                  item.title,
                  style: theme.textTheme.titleMedium?.copyWith(
                    fontWeight: FontWeight.w900,
                  ),
                ),
              ),
              _StatusPill(item: item),
            ],
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

class _LogTabs extends StatelessWidget {
  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return Container(
      decoration: BoxDecoration(
        color: theme.colorScheme.surfaceContainerLowest,
        borderRadius: BorderRadius.circular(18),
        border: Border.all(
          color: theme.colorScheme.outlineVariant.withValues(alpha: 0.55),
        ),
      ),
      child: TabBar(
        dividerColor: Colors.transparent,
        indicatorSize: TabBarIndicatorSize.tab,
        indicator: BoxDecoration(
          color: theme.colorScheme.primaryContainer,
          borderRadius: BorderRadius.circular(14),
        ),
        labelColor: theme.colorScheme.onPrimaryContainer,
        unselectedLabelColor: theme.colorScheme.onSurfaceVariant,
        tabs: const [
          Tab(text: 'stdout'),
          Tab(text: 'stderr'),
        ],
      ),
    );
  }
}

class _LogPane extends StatelessWidget {
  const _LogPane({
    required this.label,
    required this.text,
    required this.accent,
    required this.emptyLabel,
  });

  final String label;
  final String text;
  final Color accent;
  final String emptyLabel;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    if (text.isEmpty) {
      return Container(
        width: double.infinity,
        decoration: BoxDecoration(
          color: const Color(0xFF0B1220),
          borderRadius: BorderRadius.circular(22),
          boxShadow: [
            BoxShadow(
              color: Colors.black.withValues(alpha: 0.16),
              blurRadius: 26,
              offset: const Offset(0, 12),
            ),
          ],
        ),
        child: Center(
          child: Text(
            emptyLabel,
            style: theme.textTheme.bodyMedium?.copyWith(
              color: const Color(0xFF94A3B8),
            ),
          ),
        ),
      );
    }
    return Container(
      width: double.infinity,
      decoration: BoxDecoration(
        color: const Color(0xFF0B1220),
        borderRadius: BorderRadius.circular(22),
        boxShadow: [
          BoxShadow(
            color: Colors.black.withValues(alpha: 0.16),
            blurRadius: 26,
            offset: const Offset(0, 12),
          ),
        ],
      ),
      child: Column(
        children: [
          Container(
            padding: const EdgeInsets.fromLTRB(14, 12, 14, 12),
            decoration: const BoxDecoration(
              color: Color(0xFF111827),
              borderRadius: BorderRadius.vertical(top: Radius.circular(22)),
            ),
            child: Row(
              children: [
                _TerminalDot(color: const Color(0xFFFB7185)),
                const SizedBox(width: 6),
                _TerminalDot(color: const Color(0xFFFBBF24)),
                const SizedBox(width: 6),
                _TerminalDot(color: const Color(0xFF4ADE80)),
                const SizedBox(width: 12),
                Expanded(
                  child: Text(
                    '$label • ${_lineCount(text)} lines',
                    style: const TextStyle(
                      color: Color(0xFFCBD5E1),
                      fontSize: 12,
                      fontWeight: FontWeight.w700,
                      letterSpacing: 0.2,
                    ),
                  ),
                ),
                Container(
                  width: 9,
                  height: 9,
                  decoration: BoxDecoration(
                    color: accent,
                    shape: BoxShape.circle,
                    boxShadow: [
                      BoxShadow(
                        color: accent.withValues(alpha: 0.45),
                        blurRadius: 12,
                      ),
                    ],
                  ),
                ),
              ],
            ),
          ),
          Expanded(
            child: Padding(
              padding: const EdgeInsets.all(14),
              child: SingleChildScrollView(
                child: SelectableText(
                  text,
                  style: const TextStyle(
                    color: Color(0xFFE2E8F0),
                    fontFamily: 'monospace',
                    fontSize: 12.5,
                    height: 1.5,
                  ),
                ),
              ),
            ),
          ),
        ],
      ),
    );
  }
}

class _StatusPill extends StatelessWidget {
  const _StatusPill({required this.item});

  final TerminalExecution item;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final color = item.running
        ? const Color(0xFF22C55E)
        : (item.exitCode == null || item.exitCode == 0)
            ? theme.colorScheme.primary
            : const Color(0xFFF97316);
    final label = item.running
        ? '运行中'
        : item.exitCode != null
            ? 'exit ${item.exitCode}'
            : '已结束';
    return Container(
      padding: const EdgeInsets.symmetric(horizontal: 10, vertical: 6),
      decoration: BoxDecoration(
        color: color.withValues(alpha: 0.12),
        borderRadius: BorderRadius.circular(999),
        border: Border.all(color: color.withValues(alpha: 0.24)),
      ),
      child: Text(
        label,
        style: theme.textTheme.labelSmall?.copyWith(
          color: color,
          fontWeight: FontWeight.w800,
        ),
      ),
    );
  }
}

class _TerminalDot extends StatelessWidget {
  const _TerminalDot({required this.color});

  final Color color;

  @override
  Widget build(BuildContext context) {
    return Container(
      width: 10,
      height: 10,
      decoration: BoxDecoration(
        color: color,
        shape: BoxShape.circle,
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

int _lineCount(String text) {
  final normalized = text.trim();
  if (normalized.isEmpty) {
    return 0;
  }
  return '\n'.allMatches(normalized).length + 1;
}
