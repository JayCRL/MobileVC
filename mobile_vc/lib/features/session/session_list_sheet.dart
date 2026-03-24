import 'package:flutter/material.dart';

import '../../data/models/session_models.dart';

class SessionListSheet extends StatelessWidget {
  const SessionListSheet({
    super.key,
    required this.sessions,
    required this.selectedSessionId,
    required this.onCreate,
    required this.onLoad,
    required this.onDelete,
  });

  final List<SessionSummary> sessions;
  final String selectedSessionId;
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
            Flexible(
              child: ListView.separated(
                shrinkWrap: true,
                itemCount: sessions.length,
                separatorBuilder: (_, __) => const SizedBox(height: 10),
                itemBuilder: (context, index) {
                  final item = sessions[index];
                  final selected = item.id == selectedSessionId;
                  return Card(
                    child: ListTile(
                      onTap: () => onLoad(item.id),
                      shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(18)),
                      title: Text(item.title.isEmpty ? item.id : item.title),
                      subtitle: Text(item.lastPreview.isEmpty ? item.runtime.command : item.lastPreview),
                      trailing: Row(
                        mainAxisSize: MainAxisSize.min,
                        children: [
                          if (selected)
                            const Padding(
                              padding: EdgeInsets.only(right: 8),
                              child: Icon(Icons.check_circle, size: 18),
                            ),
                          IconButton(
                            onPressed: () => onDelete(item.id),
                            icon: const Icon(Icons.delete_outline),
                          ),
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
