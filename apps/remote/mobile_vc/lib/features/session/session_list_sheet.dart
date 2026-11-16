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
  final ValueChanged<SessionSummary> onLoad;
  final ValueChanged<String> onDelete;

  @override
  Widget build(BuildContext context) {
    return _SessionListSheetBody(
      sessions: sessions,
      selectedSessionId: selectedSessionId,
      cwd: cwd,
      onCreate: onCreate,
      onLoad: onLoad,
      onDelete: onDelete,
    );
  }
}

class _SessionListSheetBody extends StatefulWidget {
  const _SessionListSheetBody({
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
  final ValueChanged<SessionSummary> onLoad;
  final ValueChanged<String> onDelete;

  @override
  State<_SessionListSheetBody> createState() => _SessionListSheetBodyState();
}

class _SessionListSheetBodyState extends State<_SessionListSheetBody> {
  _SessionProviderFilter _filter = _SessionProviderFilter.all;
  String _projectFilterKey = '';

  @override
  Widget build(BuildContext context) {
    final filteredByProvider =
        widget.sessions.where((item) => _matchesFilter(item, _filter)).toList();
    final grouped = _groupSessions(filteredByProvider, widget.cwd);
    final projectOptions = _projectOptionsFromGroups(grouped);
    if (_projectFilterKey.isNotEmpty &&
        !projectOptions.any((item) => item.key == _projectFilterKey)) {
      _projectFilterKey = '';
    }
    final visibleGroups = _projectFilterKey.isEmpty
        ? grouped
        : grouped.where((group) => group.key == _projectFilterKey).toList();
    final rows = _sessionListRows(visibleGroups);
    final currentProjectLabel =
        widget.cwd.trim().isEmpty ? '' : _projectLabel(widget.cwd);
    return SafeArea(
      child: Padding(
        padding: const EdgeInsets.fromLTRB(16, 12, 16, 24),
        child: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            _SessionListHeader(onCreate: widget.onCreate),
            const SizedBox(height: 12),
            _SessionFilterChips(
              value: _filter,
              onChanged: (value) => setState(() {
                _filter = value;
                _projectFilterKey = '';
              }),
            ),
            const SizedBox(height: 10),
            _ProjectFilterChips(
              options: projectOptions,
              value: _projectFilterKey,
              currentProjectLabel: currentProjectLabel,
              onChanged: (value) => setState(() => _projectFilterKey = value),
            ),
            const SizedBox(height: 10),
            Flexible(
              child: rows.isEmpty
                  ? const _EmptySessionList()
                  : ListView.builder(
                      itemCount: rows.length,
                      itemBuilder: (context, index) {
                        final row = rows[index];
                        if (row.group != null) {
                          return _ProjectSessionHeader(group: row.group!);
                        }
                        final item = row.session!;
                        return Padding(
                          padding: EdgeInsets.only(
                            bottom: row.lastInGroup ? 14 : 10,
                          ),
                          child: _SessionListTile(
                            item: item,
                            selected: item.id == widget.selectedSessionId,
                            onLoad: widget.onLoad,
                            onDelete: widget.onDelete,
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

class _SessionListHeader extends StatelessWidget {
  const _SessionListHeader({required this.onCreate});

  final VoidCallback onCreate;

  @override
  Widget build(BuildContext context) {
    return Row(
      children: [
        Text('会话列表', style: Theme.of(context).textTheme.titleLarge),
        const Spacer(),
        FilledButton.tonalIcon(
          onPressed: onCreate,
          icon: const Icon(Icons.add),
          label: const Text('新建'),
        ),
      ],
    );
  }
}

class _SessionFilterChips extends StatelessWidget {
  const _SessionFilterChips({
    required this.value,
    required this.onChanged,
  });

  final _SessionProviderFilter value;
  final ValueChanged<_SessionProviderFilter> onChanged;

  @override
  Widget build(BuildContext context) {
    return Align(
      alignment: Alignment.centerLeft,
      child: Wrap(
        spacing: 8,
        runSpacing: 8,
        children: _SessionProviderFilter.values.map((filter) {
          return ChoiceChip(
            label: Text(filter.label),
            selected: value == filter,
            onSelected: (_) => onChanged(filter),
          );
        }).toList(),
      ),
    );
  }
}

class _ProjectFilterChips extends StatelessWidget {
  const _ProjectFilterChips({
    required this.options,
    required this.value,
    required this.currentProjectLabel,
    required this.onChanged,
  });

  final List<_ProjectFilterOption> options;
  final String value;
  final String currentProjectLabel;
  final ValueChanged<String> onChanged;

  @override
  Widget build(BuildContext context) {
    final selectedValue = options.any((item) => item.key == value) ? value : '';
    return DropdownButtonFormField<String>(
      initialValue: selectedValue,
      isExpanded: true,
      decoration: const InputDecoration(
        labelText: '项目',
        prefixIcon: Icon(Icons.folder_open),
      ),
      items: [
        const DropdownMenuItem(value: '', child: Text('全部项目')),
        ...options.map((option) {
          final suffix = option.current ? ' · 当前' : '';
          return DropdownMenuItem(
            value: option.key,
            child: Text(
              '${option.title}$suffix',
              maxLines: 1,
              overflow: TextOverflow.ellipsis,
            ),
          );
        }),
      ],
      onChanged: (next) => onChanged(next ?? ''),
    );
  }
}

class _ProjectSessionHeader extends StatelessWidget {
  const _ProjectSessionHeader({required this.group});

  final _ProjectSessionGroupData group;

  @override
  Widget build(BuildContext context) {
    final scheme = Theme.of(context).colorScheme;
    return Padding(
      padding: const EdgeInsets.only(bottom: 8),
      child: Row(
        children: [
          Expanded(
            child: Text(
              group.title,
              style: Theme.of(context).textTheme.titleSmall?.copyWith(
                    fontWeight: FontWeight.w800,
                  ),
              maxLines: 1,
              overflow: TextOverflow.ellipsis,
            ),
          ),
          if (group.current)
            Container(
              padding: const EdgeInsets.symmetric(horizontal: 8, vertical: 3),
              decoration: BoxDecoration(
                color: scheme.primaryContainer,
                borderRadius: BorderRadius.circular(999),
              ),
              child: Text(
                '当前',
                style: Theme.of(context).textTheme.labelSmall?.copyWith(
                      color: scheme.onPrimaryContainer,
                    ),
              ),
            ),
        ],
      ),
    );
  }
}

class _SessionListTile extends StatelessWidget {
  const _SessionListTile({
    required this.item,
    required this.selected,
    required this.onLoad,
    required this.onDelete,
  });

  final SessionSummary item;
  final bool selected;
  final ValueChanged<SessionSummary> onLoad;
  final ValueChanged<String> onDelete;

  @override
  Widget build(BuildContext context) {
    final sourceLabel = _sourceLabel(item);
    final nativeLabel = sessionNativeSourceLabel(item);
    final preview = sessionDisplayPreview(item);
    final title = nativeLabel.isNotEmpty
        ? nativeLabel
        : preview.isNotEmpty
            ? preview
            : sessionDisplayTitle(item);
    final subtitle = nativeLabel.isNotEmpty ? sessionDisplaySubtitle(item) : '';
    final timestampLabel = _timestampLabel(item);
    return Card(
      child: ListTile(
        onTap: () => onLoad(item),
        shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(18)),
        title: Row(
          children: [
            Expanded(
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                mainAxisSize: MainAxisSize.min,
                children: [
                  Text(title, maxLines: 2, overflow: TextOverflow.ellipsis),
                  if (subtitle.isNotEmpty) ...[
                    const SizedBox(height: 4),
                    Text(
                      subtitle,
                      maxLines: 2,
                      overflow: TextOverflow.ellipsis,
                      style: Theme.of(context).textTheme.bodySmall?.copyWith(
                            color:
                                Theme.of(context).colorScheme.onSurfaceVariant,
                          ),
                    ),
                  ],
                  if (timestampLabel.isNotEmpty) ...[
                    const SizedBox(height: 4),
                    Text(
                      timestampLabel,
                      style: Theme.of(context).textTheme.bodySmall?.copyWith(
                            color:
                                Theme.of(context).colorScheme.onSurfaceVariant,
                            fontSize: 11,
                          ),
                    ),
                  ],
                ],
              ),
            ),
            if (sourceLabel.isNotEmpty && sourceLabel != title)
              _SourceBadge(item: item),
          ],
        ),
        subtitle: null,
        contentPadding: const EdgeInsets.symmetric(horizontal: 16, vertical: 8),
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
              tooltip: _canDeleteSession(item) ? '删除此会话' : '不能删除电脑端原生会话',
              onPressed: () {
                if (!_canDeleteSession(item)) {
                  _showDeleteUnavailable(context, item);
                  return;
                }
                onDelete(item.id);
              },
              icon: const Icon(Icons.delete_outline),
            ),
          ],
        ),
        titleAlignment: ListTileTitleAlignment.top,
      ),
    );
  }
}

class _SourceBadge extends StatelessWidget {
  const _SourceBadge({required this.item});

  final SessionSummary item;

  @override
  Widget build(BuildContext context) {
    return Container(
      padding: const EdgeInsets.symmetric(horizontal: 8, vertical: 4),
      decoration: BoxDecoration(
        color: item.external
            ? Theme.of(context).colorScheme.secondaryContainer
            : Theme.of(context).colorScheme.surfaceContainerHighest,
        borderRadius: BorderRadius.circular(999),
      ),
      child: Text(
        _sourceLabel(item),
        style: Theme.of(context).textTheme.labelSmall,
      ),
    );
  }
}

class _EmptySessionList extends StatelessWidget {
  const _EmptySessionList();

  @override
  Widget build(BuildContext context) {
    return Center(
      child: Padding(
        padding: const EdgeInsets.symmetric(vertical: 32),
        child: Text(
          '没有匹配的会话',
          style: Theme.of(context).textTheme.bodyMedium,
        ),
      ),
    );
  }
}

enum _SessionProviderFilter {
  all('全部'),
  codex('Codex'),
  claude('Claude'),
  gemini('Gemini');

  const _SessionProviderFilter(this.label);

  final String label;
}

class _ProjectSessionGroupData {
  const _ProjectSessionGroupData({
    required this.key,
    required this.cwd,
    required this.title,
    required this.current,
    required this.sessions,
  });

  final String key;
  final String cwd;
  final String title;
  final bool current;
  final List<SessionSummary> sessions;
}

class _ProjectFilterOption {
  const _ProjectFilterOption({
    required this.key,
    required this.title,
    required this.current,
    required this.updatedAt,
  });

  final String key;
  final String title;
  final bool current;
  final DateTime updatedAt;
}

class _SessionListRow {
  const _SessionListRow.header(this.group)
      : session = null,
        lastInGroup = false;

  const _SessionListRow.session(this.session, {required this.lastInGroup})
      : group = null;

  final _ProjectSessionGroupData? group;
  final SessionSummary? session;
  final bool lastInGroup;
}

List<_SessionListRow> _sessionListRows(List<_ProjectSessionGroupData> groups) {
  final rows = <_SessionListRow>[];
  for (final group in groups) {
    rows.add(_SessionListRow.header(group));
    for (var index = 0; index < group.sessions.length; index++) {
      rows.add(
        _SessionListRow.session(
          group.sessions[index],
          lastInGroup: index == group.sessions.length - 1,
        ),
      );
    }
  }
  return rows;
}

List<_ProjectFilterOption> _projectOptionsFromGroups(
  List<_ProjectSessionGroupData> groups,
) {
  return groups.map((group) {
    return _ProjectFilterOption(
      key: group.key,
      title: group.title,
      current: group.current,
      updatedAt: _latestUpdatedAt(group),
    );
  }).toList();
}

List<_ProjectSessionGroupData> _groupSessions(
  List<SessionSummary> sessions,
  String currentCwd,
) {
  final byCwd = <String, List<SessionSummary>>{};
  for (final item in sessions) {
    byCwd
        .putIfAbsent(_projectKey(item.runtime.cwd), () => <SessionSummary>[])
        .add(item);
  }
  final currentKey = _projectKey(currentCwd);
  final groups = byCwd.entries.map((entry) {
    final items = [...entry.value]..sort(_compareSessionsByUpdatedAt);
    final actualCwd = entry.value
        .map((item) => item.runtime.cwd.trim())
        .firstWhere((value) => value.isNotEmpty, orElse: () => '');
    final current = currentKey.isNotEmpty && entry.key == currentKey;
    return _ProjectSessionGroupData(
      key: entry.key,
      cwd: actualCwd,
      title: actualCwd.isEmpty ? '未记录目录' : _projectLabel(actualCwd),
      current: current,
      sessions: items,
    );
  }).toList();
  groups.sort((left, right) {
    if (left.current != right.current) {
      return left.current ? -1 : 1;
    }
    return _latestUpdatedAt(right).compareTo(_latestUpdatedAt(left));
  });
  return groups;
}

int _compareSessionsByUpdatedAt(SessionSummary left, SessionSummary right) {
  final rightTime = right.updatedAt ?? right.createdAt ?? DateTime(0);
  final leftTime = left.updatedAt ?? left.createdAt ?? DateTime(0);
  return rightTime.compareTo(leftTime);
}

DateTime _latestUpdatedAt(_ProjectSessionGroupData group) {
  if (group.sessions.isEmpty) {
    return DateTime(0);
  }
  return group.sessions.first.updatedAt ??
      group.sessions.first.createdAt ??
      DateTime(0);
}

bool _matchesFilter(SessionSummary item, _SessionProviderFilter filter) {
  switch (filter) {
    case _SessionProviderFilter.all:
      return true;
    case _SessionProviderFilter.codex:
      return _sessionEngine(item) == _SessionProviderFilter.codex;
    case _SessionProviderFilter.claude:
      return _sessionEngine(item) == _SessionProviderFilter.claude;
    case _SessionProviderFilter.gemini:
      return _sessionEngine(item) == _SessionProviderFilter.gemini;
  }
}

_SessionProviderFilter? _sessionEngine(SessionSummary item) {
  switch (sessionNativeSource(item)) {
    case 'codex-native':
      return _SessionProviderFilter.codex;
    case 'claude-native':
      return _SessionProviderFilter.claude;
  }
  final engine = item.runtime.engine.trim().toLowerCase();
  final command = item.runtime.command.trim().toLowerCase();
  if (engine == 'codex' || command == 'codex' || command.startsWith('codex ')) {
    return _SessionProviderFilter.codex;
  }
  if (engine == 'claude' ||
      command == 'claude' ||
      command.startsWith('claude ')) {
    return _SessionProviderFilter.claude;
  }
  if (engine == 'gemini' ||
      command == 'gemini' ||
      command.startsWith('gemini ')) {
    return _SessionProviderFilter.gemini;
  }
  return null;
}

String _projectKey(String value) {
  final normalized = _normalizedPath(value);
  return normalized.isEmpty ? '__unknown__' : normalized;
}

String _normalizedPath(String value) {
  return value
      .trim()
      .replaceAll('\\', '/')
      .replaceAll(RegExp(r'/+'), '/')
      .replaceFirst(RegExp(r'/$'), '');
}

String _projectLabel(String path) {
  final normalized = _normalizedPath(path);
  if (normalized.isEmpty) {
    return '未记录目录';
  }
  final parts = normalized.split('/').where((part) => part.isNotEmpty).toList();
  return parts.isEmpty ? normalized : parts.last;
}

bool _canDeleteSession(SessionSummary item) {
  if (sessionIsMobileVcOwned(item)) {
    return true;
  }
  if (sessionNativeSource(item).isNotEmpty) {
    return false;
  }
  return !item.external;
}

void _showDeleteUnavailable(BuildContext context, SessionSummary item) {
  final sourceLabel = _sourceLabel(item);
  final prefix = sourceLabel.isEmpty ? '电脑端原生会话' : sourceLabel;
  ScaffoldMessenger.of(context).showSnackBar(
    SnackBar(content: Text('$prefix 只能恢复，不能在 MobileVC 内删除')),
  );
}

String _sourceLabel(SessionSummary item) {
  return sessionSourceLabel(item);
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
