import 'package:flutter/material.dart';

import '../../data/models/session_models.dart';
import 'session_display_text.dart';

class SessionListSheet extends StatelessWidget {
  const SessionListSheet({
    super.key,
    required this.sessions,
    required this.selectedSessionId,
    required this.cwd,
    required this.onCreate,
    required this.onLoad,
    required this.onDelete,
  });

  final List<SessionSummary> sessions;
  final String selectedSessionId;
  final String cwd;
  final VoidCallback onCreate;
  final ValueChanged<String> onLoad;
  final ValueChanged<String> onDelete;

  @override
  Widget build(BuildContext context) {
    return SafeArea(
      child: Padding(
        padding: const EdgeInsets.fromLTRB(16, 12, 16, 24),
        child: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            Row(
              children: [
                Text('会话列表', style: Theme.of(context).textTheme.titleLarge),
                const Spacer(),
                FilledButton.tonalIcon(
                  onPressed: onCreate,
                  icon: const Icon(Icons.add),
                  label: const Text('新建'),
                ),
              ],
            ),
            const SizedBox(height: 12),
            if (cwd.trim().isNotEmpty)
              Padding(
                padding: const EdgeInsets.only(bottom: 10),
                child: Align(
                  alignment: Alignment.centerLeft,
                  child: Text(
                    '当前目录：$cwd',
                    style: Theme.of(context).textTheme.bodySmall,
                  ),
                ),
              ),
            Flexible(
              child: ListView.separated(
                shrinkWrap: true,
                itemCount: sessions.length,
                separatorBuilder: (_, __) => const SizedBox(height: 10),
                itemBuilder: (context, index) {
                  final item = sessions[index];
                  final selected = item.id == selectedSessionId;
                  final sourceLabel = _sourceLabel(item);
                  final preview = sessionDisplayPreview(item);
                  final title =
                      preview.isNotEmpty ? preview : sessionDisplayTitle(item);
                  final timestampLabel = _timestampLabel(item);
                  return Card(
                    child: ListTile(
                      onTap: () => onLoad(item.id),
                      shape: RoundedRectangleBorder(
                          borderRadius: BorderRadius.circular(18)),
                      title: Row(
                        children: [
                          Expanded(
                            child: Column(
                              crossAxisAlignment: CrossAxisAlignment.start,
                              mainAxisSize: MainAxisSize.min,
                              children: [
                                Text(
                                  title,
                                  maxLines: 2,
                                  overflow: TextOverflow.ellipsis,
                                ),
                                if (timestampLabel.isNotEmpty) ...[
                                  const SizedBox(height: 4),
                                  Text(
                                    timestampLabel,
                                    style: Theme.of(context)
                                        .textTheme
                                        .bodySmall
                                        ?.copyWith(
                                          color: Theme.of(context)
                                              .colorScheme
                                              .onSurfaceVariant,
                                          fontSize: 11,
                                        ),
                                  ),
                                ],
                              ],
                            ),
                          ),
                          if (sourceLabel.isNotEmpty)
                            Container(
                              padding: const EdgeInsets.symmetric(
                                  horizontal: 8, vertical: 4),
                              decoration: BoxDecoration(
                                color: item.external
                                    ? Theme.of(context)
                                        .colorScheme
                                        .secondaryContainer
                                    : Theme.of(context)
                                        .colorScheme
                                        .surfaceContainerHighest,
                                borderRadius: BorderRadius.circular(999),
                              ),
                              child: Text(
                                sourceLabel,
                                style: Theme.of(context).textTheme.labelSmall,
                              ),
                            ),
                        ],
                      ),
                      subtitle: null,
                      isThreeLine: false,
                      contentPadding: const EdgeInsets.symmetric(
                        horizontal: 16,
                        vertical: 8,
                      ),
                      dense: false,
                      minVerticalPadding: 10,
                      trailing: Row(
                        mainAxisSize: MainAxisSize.min,
                        children: [
                          if (selected)
                            const Padding(
                              padding: EdgeInsets.only(right: 8),
                              child: Icon(Icons.check_circle, size: 18),
                            ),
                          IconButton(
                            onPressed:
                                item.external ? null : () => onDelete(item.id),
                            icon: const Icon(Icons.delete_outline),
                          ),
                        ],
                      ),
                      titleAlignment: ListTileTitleAlignment.top,
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

String _sourceLabel(SessionSummary item) {
  if (item.external || item.source == 'codex-native') {
    return '电脑 Codex';
  }
  if (item.runtime.engine.trim().toLowerCase() == 'codex') {
    return 'MobileVC';
  }
  return '';
}

String _timestampLabel(SessionSummary item) {
  final value = item.updatedAt ?? item.createdAt;
  if (value == null) {
    return '';
  }
  final month = value.month.toString().padLeft(2, '0');
  final day = value.day.toString().padLeft(2, '0');
  final hour = value.hour.toString().padLeft(2, '0');
  final minute = value.minute.toString().padLeft(2, '0');
  return '$month-$day $hour:$minute';
}
