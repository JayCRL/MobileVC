import 'package:flutter/material.dart';

import '../../data/models/session_models.dart';

class TerminalLogSheet extends StatelessWidget {
  const TerminalLogSheet({
    super.key,
    required this.executions,
    required this.activeExecutionId,
    required this.stdout,
    required this.stderr,
    this.runtimeProcesses = const [],
    this.activeProcessPid = 0,
    this.processStdout = '',
    this.processStderr = '',
    this.processMessage = '',
    this.runtimeProcessListLoading = false,
    this.runtimeProcessLogLoading = false,
    this.onSelectExecution,
    this.onSelectProcess,
    this.onRefreshProcesses,
  });

  final List<TerminalExecution> executions;
  final String activeExecutionId;
  final String stdout;
  final String stderr;
  final List<RuntimeProcessItem> runtimeProcesses;
  final int activeProcessPid;
  final String processStdout;
  final String processStderr;
  final String processMessage;
  final bool runtimeProcessListLoading;
  final bool runtimeProcessLogLoading;
  final ValueChanged<String>? onSelectExecution;
  final ValueChanged<int>? onSelectProcess;
  final VoidCallback? onRefreshProcesses;

  @override
  Widget build(BuildContext context) {
    final hasExecutions = executions.isNotEmpty;
    final hasProcesses = runtimeProcesses.isNotEmpty;
    final activeExecution = _activeExecution();
    final activeProcess = _activeProcess();
    final resolvedStdout = hasProcesses && activeProcess != null
        ? processStdout
        : (activeExecution?.stdout ?? stdout);
    final resolvedStderr = hasProcesses && activeProcess != null
        ? processStderr
        : (activeExecution?.stderr ?? stderr);
    final statusMessage = hasProcesses ? processMessage.trim() : '';

    return DefaultTabController(
      length: 2,
      child: SafeArea(
        top: false,
        child: DecoratedBox(
          decoration: BoxDecoration(
            gradient: LinearGradient(
              colors: [
                Theme.of(context).colorScheme.primary.withValues(alpha: 0.06),
                Theme.of(context).colorScheme.surface,
                Theme.of(context).colorScheme.surface,
              ],
              begin: Alignment.topCenter,
              end: Alignment.bottomCenter,
            ),
          ),
          child: Padding(
            padding: const EdgeInsets.fromLTRB(16, 6, 16, 24),
            child: Column(
              children: [
                Flexible(
                  fit: FlexFit.loose,
                  child: SingleChildScrollView(
                    child: Column(
                      children: [
                        _HeroCard(
                          hasProcesses: hasProcesses,
                          hasExecutions: hasExecutions,
                          runtimeProcessListLoading: runtimeProcessListLoading,
                          processCount: runtimeProcesses.length,
                          executionCount: executions.length,
                          activeTitle: hasProcesses
                              ? activeProcess?.title ?? '原始输出'
                              : activeExecution?.title ?? '原始输出',
                          stdoutLines: _lineCount(resolvedStdout),
                          stderrLines: _lineCount(resolvedStderr),
                          onRefresh: onRefreshProcesses,
                        ),
                        const SizedBox(height: 12),
                        if (hasProcesses) ...[
                          _SectionHeader(
                            title: '后台进程',
                            subtitle: runtimeProcessListLoading
                                ? '正在刷新当前活跃进程树'
                                : '优先显示活跃 bash / tool 子进程及其捕获日志',
                          ),
                          const SizedBox(height: 10),
                          SizedBox(
                            height: 188,
                            child: ListView.separated(
                              itemCount: runtimeProcesses.length,
                              separatorBuilder: (_, __) =>
                                  const SizedBox(height: 10),
                              itemBuilder: (context, index) {
                                final item = runtimeProcesses[index];
                                return _ProcessCard(
                                  item: item,
                                  selected:
                                      item.pid == (activeProcess?.pid ?? 0),
                                  onTap: () => onSelectProcess?.call(item.pid),
                                );
                              },
                            ),
                          ),
                          if (activeProcess != null) ...[
                            const SizedBox(height: 12),
                            _ProcessDetailCard(
                              item: activeProcess,
                              loadingLogs: runtimeProcessLogLoading,
                            ),
                          ],
                        ] else if (hasExecutions) ...[
                          _SectionHeader(
                            title: '命令执行',
                            subtitle: '当前没有活跃后台进程，展示最近捕获的命令执行日志',
                          ),
                          const SizedBox(height: 10),
                          SizedBox(
                            height: 196,
                            child: ListView.separated(
                              itemCount: executions.length,
                              separatorBuilder: (_, __) =>
                                  const SizedBox(height: 10),
                              itemBuilder: (context, index) {
                                final item = executions[index];
                                return _ExecutionCard(
                                  item: item,
                                  selected:
                                      item.executionId == activeExecutionId,
                                  onTap: () =>
                                      onSelectExecution?.call(item.executionId),
                                );
                              },
                            ),
                          ),
                          if (activeExecution != null) ...[
                            const SizedBox(height: 12),
                            _ExecutionDetailCard(item: activeExecution),
                          ],
                        ] else ...[
                          _SectionHeader(
                            title: '当前输出',
                            subtitle: runtimeProcessListLoading
                                ? '正在刷新后台进程'
                                : '当前没有结构化执行记录，仅展示聚合 stdout / stderr',
                          ),
                        ],
                        if (statusMessage.isNotEmpty) ...[
                          const SizedBox(height: 12),
                          _MessageBanner(message: statusMessage),
                        ],
                        if (runtimeProcessListLoading && !hasProcesses) ...[
                          const SizedBox(height: 12),
                          const _MessageBanner(message: '正在拉取活跃后台进程列表…'),
                        ],
                      ],
                    ),
                  ),
                ),
                const SizedBox(height: 12),
                const _LogTabs(),
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

  RuntimeProcessItem? _activeProcess() {
    if (runtimeProcesses.isEmpty) {
      return null;
    }
    if (activeProcessPid > 0) {
      for (final item in runtimeProcesses) {
        if (item.pid == activeProcessPid) {
          return item;
        }
      }
    }
    return runtimeProcesses.first;
  }
}

class _HeroCard extends StatelessWidget {
  const _HeroCard({
    required this.hasProcesses,
    required this.hasExecutions,
    required this.runtimeProcessListLoading,
    required this.processCount,
    required this.executionCount,
    required this.activeTitle,
    required this.stdoutLines,
    required this.stderrLines,
    this.onRefresh,
  });

  final bool hasProcesses;
  final bool hasExecutions;
  final bool runtimeProcessListLoading;
  final int processCount;
  final int executionCount;
  final String activeTitle;
  final int stdoutLines;
  final int stderrLines;
  final VoidCallback? onRefresh;

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
                      hasProcesses
                          ? '优先展示当前活跃后台进程，并支持按进程查看捕获日志。'
                          : hasExecutions
                              ? '当前没有活跃后台进程，展示最近命令执行的 stdout / stderr。'
                              : '当前仅有聚合 stdout / stderr 输出。',
                      style: theme.textTheme.bodySmall?.copyWith(
                        color: theme.colorScheme.onSurfaceVariant,
                        height: 1.35,
                      ),
                    ),
                  ],
                ),
              ),
              if (onRefresh != null)
                IconButton.filledTonal(
                  onPressed: onRefresh,
                  tooltip: '刷新后台进程',
                  icon: runtimeProcessListLoading
                      ? const SizedBox(
                          width: 18,
                          height: 18,
                          child: CircularProgressIndicator(strokeWidth: 2.2),
                        )
                      : const Icon(Icons.refresh_rounded),
                ),
            ],
          ),
          const SizedBox(height: 14),
          Wrap(
            spacing: 8,
            runSpacing: 8,
            children: [
              _MetaChip(label: '进程数', value: '$processCount'),
              _MetaChip(label: '命令数', value: '$executionCount'),
              _MetaChip(
                label: '当前',
                value: activeTitle.trim().isNotEmpty ? activeTitle : '原始输出',
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

class _SectionHeader extends StatelessWidget {
  const _SectionHeader({
    required this.title,
    required this.subtitle,
  });

  final String title;
  final String subtitle;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return Row(
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
              const SizedBox(height: 3),
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
      ],
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
    final accent = _executionStatusColor(theme, item);
    return _SelectableCard(
      selected: selected,
      accent: accent,
      onTap: onTap,
      child: Row(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          _AccentDot(color: accent),
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
          _StatePill(
            label: _executionStatusLabel(item),
            color: accent,
          ),
        ],
      ),
    );
  }
}

class _ProcessCard extends StatelessWidget {
  const _ProcessCard({
    required this.item,
    required this.selected,
    required this.onTap,
  });

  final RuntimeProcessItem item;
  final bool selected;
  final VoidCallback onTap;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final subtitle = <String>[
      'PID ${item.pid}',
      if (item.ppid > 0) 'PPID ${item.ppid}',
      if (item.state.trim().isNotEmpty) item.state.trim(),
      if (item.elapsed.trim().isNotEmpty) item.elapsed.trim(),
    ];
    final accent =
        item.root ? theme.colorScheme.primary : const Color(0xFF0EA5E9);
    return _SelectableCard(
      selected: selected,
      accent: accent,
      onTap: onTap,
      child: Row(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          _AccentDot(color: accent),
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
            ),
          ),
          const SizedBox(width: 10),
          _StatePill(
            label: item.root ? '根进程' : '子进程',
            color: accent,
          ),
        ],
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
      decoration: _detailDecoration(theme),
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
              _StatePill(
                label: _executionStatusLabel(item),
                color: _executionStatusColor(theme, item),
              ),
            ],
          ),
          const SizedBox(height: 10),
          Wrap(
            spacing: 8,
            runSpacing: 8,
            children: [
              _MetaChip(label: '状态', value: _executionStatusLabel(item)),
              if (item.cwd.trim().isNotEmpty)
                _MetaChip(label: 'CWD', value: item.cwd.trim()),
              if (_executionSourceLabel(item).isNotEmpty)
                _MetaChip(label: '来源', value: _executionSourceLabel(item)),
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
}

class _ProcessDetailCard extends StatelessWidget {
  const _ProcessDetailCard({
    required this.item,
    required this.loadingLogs,
  });

  final RuntimeProcessItem item;
  final bool loadingLogs;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return Container(
      width: double.infinity,
      padding: const EdgeInsets.fromLTRB(16, 16, 16, 14),
      decoration: _detailDecoration(theme),
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
              if (loadingLogs)
                const SizedBox(
                  width: 18,
                  height: 18,
                  child: CircularProgressIndicator(strokeWidth: 2.2),
                )
              else
                _StatePill(
                  label: item.root ? '根进程' : '子进程',
                  color: item.root
                      ? theme.colorScheme.primary
                      : const Color(0xFF0EA5E9),
                ),
            ],
          ),
          const SizedBox(height: 10),
          Wrap(
            spacing: 8,
            runSpacing: 8,
            children: [
              _MetaChip(label: 'PID', value: '${item.pid}'),
              if (item.ppid > 0)
                _MetaChip(label: 'PPID', value: '${item.ppid}'),
              if (item.state.trim().isNotEmpty)
                _MetaChip(label: '状态', value: item.state.trim()),
              if (item.elapsed.trim().isNotEmpty)
                _MetaChip(label: '运行时长', value: item.elapsed.trim()),
              if (item.cwd.trim().isNotEmpty)
                _MetaChip(label: 'CWD', value: item.cwd.trim()),
              if (item.source.trim().isNotEmpty)
                _MetaChip(label: '来源', value: item.source.trim()),
              if (item.executionId.trim().isNotEmpty)
                _MetaChip(label: '执行 ID', value: item.executionId.trim()),
            ],
          ),
        ],
      ),
    );
  }
}

class _SelectableCard extends StatelessWidget {
  const _SelectableCard({
    required this.selected,
    required this.accent,
    required this.onTap,
    required this.child,
  });

  final bool selected;
  final Color accent;
  final VoidCallback onTap;
  final Widget child;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
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
                    ? accent.withValues(alpha: 0.12)
                    : theme.colorScheme.surface,
                theme.colorScheme.surface,
              ],
              begin: Alignment.centerLeft,
              end: Alignment.centerRight,
            ),
            borderRadius: BorderRadius.circular(22),
            border: Border.all(
              color: selected
                  ? accent.withValues(alpha: 0.4)
                  : theme.colorScheme.outlineVariant.withValues(alpha: 0.45),
            ),
          ),
          child: Padding(
            padding: const EdgeInsets.fromLTRB(14, 14, 14, 12),
            child: child,
          ),
        ),
      ),
    );
  }
}

class _MessageBanner extends StatelessWidget {
  const _MessageBanner({required this.message});

  final String message;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return Container(
      width: double.infinity,
      padding: const EdgeInsets.fromLTRB(14, 12, 14, 12),
      decoration: BoxDecoration(
        color: theme.colorScheme.secondaryContainer.withValues(alpha: 0.7),
        borderRadius: BorderRadius.circular(18),
        border: Border.all(
          color: theme.colorScheme.outlineVariant.withValues(alpha: 0.5),
        ),
      ),
      child: Text(
        message,
        style: theme.textTheme.bodySmall?.copyWith(
          color: theme.colorScheme.onSecondaryContainer,
          height: 1.35,
        ),
      ),
    );
  }
}

class _LogTabs extends StatelessWidget {
  const _LogTabs();

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
        decoration: _terminalDecoration(),
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
      decoration: _terminalDecoration(),
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
                const _TerminalDot(color: Color(0xFFFB7185)),
                const SizedBox(width: 6),
                const _TerminalDot(color: Color(0xFFFBBF24)),
                const SizedBox(width: 6),
                const _TerminalDot(color: Color(0xFF4ADE80)),
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

class _AccentDot extends StatelessWidget {
  const _AccentDot({required this.color});

  final Color color;

  @override
  Widget build(BuildContext context) {
    return Container(
      width: 10,
      height: 10,
      margin: const EdgeInsets.only(top: 6),
      decoration: BoxDecoration(
        color: color,
        shape: BoxShape.circle,
        boxShadow: [
          BoxShadow(
            color: color.withValues(alpha: 0.35),
            blurRadius: 12,
          ),
        ],
      ),
    );
  }
}

class _StatePill extends StatelessWidget {
  const _StatePill({
    required this.label,
    required this.color,
  });

  final String label;
  final Color color;

  @override
  Widget build(BuildContext context) {
    return Container(
      padding: const EdgeInsets.symmetric(horizontal: 10, vertical: 6),
      decoration: BoxDecoration(
        color: color.withValues(alpha: 0.12),
        borderRadius: BorderRadius.circular(999),
        border: Border.all(color: color.withValues(alpha: 0.24)),
      ),
      child: Text(
        label,
        style: Theme.of(context).textTheme.labelSmall?.copyWith(
              color: color,
              fontWeight: FontWeight.w800,
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

BoxDecoration _detailDecoration(ThemeData theme) {
  return BoxDecoration(
    color: theme.colorScheme.surface.withValues(alpha: 0.9),
    borderRadius: BorderRadius.circular(24),
    border: Border.all(
      color: theme.colorScheme.outlineVariant.withValues(alpha: 0.45),
    ),
  );
}

BoxDecoration _terminalDecoration() {
  return BoxDecoration(
    color: const Color(0xFF0B1220),
    borderRadius: BorderRadius.circular(22),
    border: Border.all(
      color: Colors.white.withValues(alpha: 0.06),
    ),
  );
}

Color _executionStatusColor(ThemeData theme, TerminalExecution item) {
  if (item.running) {
    return const Color(0xFF22C55E);
  }
  if (item.exitCode == null || item.exitCode == 0) {
    return theme.colorScheme.primary;
  }
  return const Color(0xFFF97316);
}

String _executionStatusLabel(TerminalExecution item) {
  if (item.running) {
    return '运行中';
  }
  if (item.exitCode != null) {
    return 'exit ${item.exitCode}';
  }
  return '已结束';
}

String _executionSourceLabel(TerminalExecution item) {
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

int _lineCount(String text) {
  final normalized = text.trim();
  if (normalized.isEmpty) {
    return 0;
  }
  return '\n'.allMatches(normalized).length + 1;
}
