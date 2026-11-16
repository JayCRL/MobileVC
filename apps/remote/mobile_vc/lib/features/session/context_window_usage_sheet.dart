import 'package:flutter/material.dart';

import '../../data/models/session_models.dart';

class ContextWindowUsageSheet extends StatelessWidget {
  const ContextWindowUsageSheet({
    super.key,
    required this.usage,
    required this.engineLabel,
  });

  final ContextWindowUsage usage;
  final String engineLabel;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final scheme = theme.colorScheme;
    final percent = usage.percentUsed;
    final progress = usage.fractionUsed;
    final usedText = formatTokenCountCompact(usage.tokensUsed);
    final totalText = formatTokenCountCompact(usage.tokenLimit);
    final remainingText = formatTokenCountCompact(usage.tokensRemaining);

    return SafeArea(
      top: false,
      child: Container(
        color: scheme.surface,
        child: Column(
          mainAxisSize: MainAxisSize.min,
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            const SizedBox(height: 8),
            Center(
              child: Container(
                width: 44,
                height: 5,
                decoration: BoxDecoration(
                  color: scheme.outlineVariant,
                  borderRadius: BorderRadius.circular(999),
                ),
              ),
            ),
            Padding(
              padding: const EdgeInsets.fromLTRB(20, 18, 20, 12),
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  Text(
                    '上下文窗口',
                    style: theme.textTheme.titleLarge?.copyWith(
                      fontWeight: FontWeight.w700,
                    ),
                  ),
                  const SizedBox(height: 6),
                  Text(
                    usage.isAvailable
                        ? '$engineLabel 当前会话上下文用量'
                        : '当前还没有可用的上下文用量数据',
                    style: theme.textTheme.bodySmall?.copyWith(
                      color: scheme.onSurfaceVariant,
                    ),
                  ),
                ],
              ),
            ),
            if (!usage.isAvailable)
              Padding(
                padding: const EdgeInsets.fromLTRB(20, 0, 20, 24),
                child: Container(
                  width: double.infinity,
                  padding: const EdgeInsets.all(16),
                  decoration: BoxDecoration(
                    color: scheme.surfaceContainerHigh,
                    borderRadius: BorderRadius.circular(20),
                  ),
                  child: Text(
                    '等待运行时返回真实 token usage。收到后这里会实时更新。',
                    style: theme.textTheme.bodyMedium,
                  ),
                ),
              )
            else
              Padding(
                padding: const EdgeInsets.fromLTRB(20, 0, 20, 24),
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    Container(
                      width: double.infinity,
                      padding: const EdgeInsets.all(18),
                      decoration: BoxDecoration(
                        gradient: LinearGradient(
                          colors: [
                            scheme.surfaceContainerHigh,
                            scheme.surface,
                          ],
                          begin: Alignment.topLeft,
                          end: Alignment.bottomRight,
                        ),
                        borderRadius: BorderRadius.circular(24),
                        border: Border.all(
                          color: scheme.outlineVariant.withValues(alpha: 0.38),
                        ),
                      ),
                      child: Column(
                        crossAxisAlignment: CrossAxisAlignment.start,
                        children: [
                          Row(
                            children: [
                              Expanded(
                                child: Text(
                                  '$percent% 已使用',
                                  style:
                                      theme.textTheme.headlineSmall?.copyWith(
                                    fontWeight: FontWeight.w800,
                                  ),
                                ),
                              ),
                              Text(
                                '$remainingText 剩余',
                                style: theme.textTheme.labelLarge?.copyWith(
                                  color: scheme.onSurfaceVariant,
                                  fontWeight: FontWeight.w700,
                                ),
                              ),
                            ],
                          ),
                          const SizedBox(height: 14),
                          ClipRRect(
                            borderRadius: BorderRadius.circular(999),
                            child: LinearProgressIndicator(
                              value: progress,
                              minHeight: 10,
                              backgroundColor: scheme.surfaceContainerHighest
                                  .withValues(alpha: 0.9),
                              valueColor: AlwaysStoppedAnimation<Color>(
                                _usageColor(scheme, percent),
                              ),
                            ),
                          ),
                          const SizedBox(height: 18),
                          Row(
                            children: [
                              Expanded(
                                child: _UsageStatCard(
                                  label: '已用',
                                  value: usedText,
                                ),
                              ),
                              const SizedBox(width: 12),
                              Expanded(
                                child: _UsageStatCard(
                                  label: '总量',
                                  value: totalText,
                                ),
                              ),
                              const SizedBox(width: 12),
                              Expanded(
                                child: _UsageStatCard(
                                  label: '剩余',
                                  value: remainingText,
                                ),
                              ),
                            ],
                          ),
                          const SizedBox(height: 12),
                          Container(
                            width: double.infinity,
                            padding: const EdgeInsets.symmetric(
                              horizontal: 14,
                              vertical: 12,
                            ),
                            decoration: BoxDecoration(
                              color: _usageColor(scheme, percent)
                                  .withValues(alpha: 0.10),
                              borderRadius: BorderRadius.circular(18),
                            ),
                            child: Row(
                              children: [
                                Icon(
                                  percent > 90
                                      ? Icons.warning_amber_rounded
                                      : percent >= 70
                                          ? Icons.info_outline_rounded
                                          : Icons.check_circle_outline_rounded,
                                  size: 18,
                                  color: _usageColor(scheme, percent),
                                ),
                                const SizedBox(width: 10),
                                Expanded(
                                  child: Text(
                                    _usageHint(percent),
                                    style:
                                        theme.textTheme.bodySmall?.copyWith(
                                      color: scheme.onSurface,
                                      height: 1.35,
                                    ),
                                  ),
                                ),
                              ],
                            ),
                          ),
                        ],
                      ),
                    ),
                    const SizedBox(height: 12),
                    Text(
                      '颜色阈值：70% 开始提醒，90% 进入高风险区。数值会跟随运行时实时同步。',
                      style: theme.textTheme.bodySmall?.copyWith(
                        color: scheme.onSurfaceVariant,
                      ),
                    ),
                  ],
                ),
              ),
          ],
        ),
      ),
    );
  }

  Color _usageColor(ColorScheme scheme, int percent) {
    if (percent > 90) {
      return scheme.error;
    }
    if (percent >= 70) {
      return const Color(0xFFF59E0B);
    }
    return scheme.primary;
  }

  String _usageHint(int percent) {
    if (percent > 90) {
      return '已进入高风险区，建议尽快 compact 或收束当前任务。';
    }
    if (percent >= 70) {
      return '上下文已接近提醒阈值，继续长对话前建议留意剩余空间。';
    }
    return '当前上下文空间充足，数值会随着运行时 token usage 实时更新。';
  }
}

class _UsageStatCard extends StatelessWidget {
  const _UsageStatCard({
    required this.label,
    required this.value,
  });

  final String label;
  final String value;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final scheme = theme.colorScheme;
    return Container(
      padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 12),
      decoration: BoxDecoration(
        color: scheme.surface,
        borderRadius: BorderRadius.circular(18),
      ),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Text(
            label,
            style: theme.textTheme.labelSmall?.copyWith(
              color: scheme.onSurfaceVariant,
            ),
          ),
          const SizedBox(height: 6),
          Text(
            value,
            style: theme.textTheme.titleMedium?.copyWith(
              fontWeight: FontWeight.w800,
            ),
          ),
        ],
      ),
    );
  }
}
