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
                          hasExecutions ? '按命令选择并查看 stdout / stderr。' : '当前仅有原始 stdout / stderr 日志。',
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
                  height: 168,
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
                    _LogPane(text: stdout),
                    _LogPane(text: stderr),
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
    final startedAt = item.startedAt;
    final subtitle = <String>[
      if (item.cwd.trim().isNotEmpty) item.cwd.trim(),
      if (startedAt != null)
        '${startedAt.month.toString().padLeft(2, '0')}-${startedAt.day.toString().padLeft(2, '0')} ${startedAt.hour.toString().padLeft(2, '0')}:${startedAt.minute.toString().padLeft(2, '0')}',
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
