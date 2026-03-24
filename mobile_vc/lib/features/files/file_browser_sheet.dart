import 'package:flutter/material.dart';

import '../../data/models/session_models.dart';

class FileBrowserSheet extends StatelessWidget {
  const FileBrowserSheet({
    super.key,
    required this.currentPath,
    required this.items,
    required this.loading,
    required this.onRefresh,
    required this.onGoParent,
    required this.onOpenDirectory,
    required this.onOpenFile,
    required this.onDownloadFile,
  });

  final String currentPath;
  final List<FSItem> items;
  final bool loading;
  final VoidCallback onRefresh;
  final VoidCallback onGoParent;
  final ValueChanged<String> onOpenDirectory;
  final ValueChanged<String> onOpenFile;
  final ValueChanged<String> onDownloadFile;

  @override
  Widget build(BuildContext context) {
    return Material(
      color: Theme.of(context).colorScheme.surface,
      child: SafeArea(
        child: SizedBox(
          width: 340,
          child: Padding(
            padding: const EdgeInsets.fromLTRB(12, 12, 12, 16),
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Row(
                  children: [
                    Expanded(
                      child: Text('文件',
                          style: Theme.of(context).textTheme.titleLarge),
                    ),
                    IconButton(
                      onPressed: onRefresh,
                      icon: const Icon(Icons.refresh),
                      tooltip: '刷新',
                    ),
                  ],
                ),
                const SizedBox(height: 8),
                Text(
                  '点按打开，长按文件可下载并选择保存位置',
                  style: Theme.of(context).textTheme.bodySmall,
                ),
                const SizedBox(height: 8),
                Container(
                  width: double.infinity,
                  padding: const EdgeInsets.all(10),
                  decoration: BoxDecoration(
                    color:
                        Theme.of(context).colorScheme.surfaceContainerHighest,
                    borderRadius: BorderRadius.circular(14),
                  ),
                  child: SelectableText(
                    currentPath.isEmpty ? '.' : currentPath,
                    style: Theme.of(context).textTheme.bodySmall,
                  ),
                ),
                const SizedBox(height: 10),
                OutlinedButton.icon(
                  onPressed: onGoParent,
                  icon: const Icon(Icons.arrow_upward),
                  label: const Text('上一级'),
                ),
                const SizedBox(height: 12),
                Expanded(
                  child: loading
                      ? const Center(child: CircularProgressIndicator())
                      : items.isEmpty
                          ? const Center(child: Text('当前目录没有可显示内容'))
                          : ListView.separated(
                              itemCount: items.length,
                              separatorBuilder: (_, __) =>
                                  const SizedBox(height: 6),
                              itemBuilder: (context, index) {
                                final item = items[index];
                                final path = _joinPath(currentPath, item.name);
                                return Material(
                                  color: Colors.transparent,
                                  child: ListTile(
                                    dense: true,
                                    shape: RoundedRectangleBorder(
                                        borderRadius:
                                            BorderRadius.circular(12)),
                                    leading: Icon(item.isDir
                                        ? Icons.folder_outlined
                                        : Icons.description_outlined),
                                    title: Text(
                                      item.name,
                                      maxLines: 1,
                                      overflow: TextOverflow.ellipsis,
                                    ),
                                    subtitle: item.isDir
                                        ? const Text('目录')
                                        : Text(_sizeLabel(item.size)),
                                    onTap: () {
                                      if (item.isDir) {
                                        onOpenDirectory(path);
                                      } else {
                                        onOpenFile(path);
                                      }
                                    },
                                    onLongPress: item.isDir
                                        ? null
                                        : () => onDownloadFile(path),
                                  ),
                                );
                              },
                            ),
                ),
              ],
            ),
          ),
        ),
      ),
    );
  }

  String _joinPath(String base, String name) {
    final normalizedBase = base.replaceAll('\\', '/').trim();
    if (normalizedBase.isEmpty || normalizedBase == '.') {
      return name;
    }
    if (normalizedBase.endsWith('/')) {
      return '$normalizedBase$name';
    }
    return '$normalizedBase/$name';
  }

  String _sizeLabel(int size) {
    if (size <= 0) {
      return '0 B';
    }
    if (size < 1024) {
      return '$size B';
    }
    if (size < 1024 * 1024) {
      return '${(size / 1024).toStringAsFixed(1)} KB';
    }
    return '${(size / (1024 * 1024)).toStringAsFixed(1)} MB';
  }
}
