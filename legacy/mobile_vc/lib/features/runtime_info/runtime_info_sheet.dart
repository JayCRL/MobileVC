import 'package:flutter/material.dart';

import '../../data/models/session_models.dart';

class RuntimeInfoSheet extends StatelessWidget {
  const RuntimeInfoSheet({
    super.key,
    required this.title,
    required this.message,
    required this.items,
  });

  final String title;
  final String message;
  final List<RuntimeInfoItem> items;

  @override
  Widget build(BuildContext context) {
    final availableCount = items.where((item) => item.available).length;
    return SafeArea(
      child: Padding(
        padding: const EdgeInsets.fromLTRB(16, 12, 16, 24),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          mainAxisSize: MainAxisSize.min,
          children: [
            Text(title.isEmpty ? '运行时信息' : title, style: Theme.of(context).textTheme.titleLarge),
            const SizedBox(height: 8),
            Wrap(
              spacing: 8,
              runSpacing: 8,
              children: [
                _SummaryChip(label: '项目', value: '${items.length}'),
                _SummaryChip(label: '可用', value: '$availableCount'),
              ],
            ),
            if (message.isNotEmpty) ...[
              const SizedBox(height: 10),
              SelectableText(message),
            ],
            const SizedBox(height: 12),
            Flexible(
              child: ListView.separated(
                shrinkWrap: true,
                itemCount: items.length,
                separatorBuilder: (_, __) => const SizedBox(height: 10),
                itemBuilder: (context, index) {
                  final item = items[index];
                  return Card(
                    child: Padding(
                      padding: const EdgeInsets.all(14),
                      child: Column(
                        crossAxisAlignment: CrossAxisAlignment.start,
                        children: [
                          Row(
                            children: [
                              Expanded(
                                child: Text(item.label, style: Theme.of(context).textTheme.titleSmall?.copyWith(fontWeight: FontWeight.w700)),
                              ),
                              Container(
                                padding: const EdgeInsets.symmetric(horizontal: 8, vertical: 4),
                                decoration: BoxDecoration(
                                  color: item.available
                                      ? Theme.of(context).colorScheme.primaryContainer
                                      : Theme.of(context).colorScheme.surfaceContainerHighest,
                                  borderRadius: BorderRadius.circular(999),
                                ),
                                child: Text(item.status.isEmpty ? (item.available ? 'ok' : '-') : item.status),
                              ),
                            ],
                          ),
                          if (item.value.isNotEmpty) ...[
                            const SizedBox(height: 8),
                            SelectableText(item.value),
                          ],
                          if (item.detail.isNotEmpty) ...[
                            const SizedBox(height: 8),
                            Text(item.detail, style: Theme.of(context).textTheme.bodySmall),
                          ],
                        ],
                      ),
                    ),
                  );
                },
              ),
            ),
          ],
        ),
      ),
    );
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
