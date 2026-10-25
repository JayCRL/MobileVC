import 'package:flutter/material.dart';

import '../../data/models/session_models.dart';
import 'file_type_utils.dart';

class FileBrowserSheet extends StatefulWidget {
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
  State<FileBrowserSheet> createState() => _FileBrowserSheetState();
}

class _FileBrowserSheetState extends State<FileBrowserSheet> {
  final TextEditingController _searchController = TextEditingController();
  String _typeFilter = _allTypeFilter;

  static const String _allTypeFilter = '全部';
  static const List<String> _typeOrder = [
    '目录',
    '图片',
    '代码',
    '配置',
    '文本',
    '文档',
    '压缩包',
    '音频',
    '视频',
    '安装包',
    '文件',
  ];

  @override
  void initState() {
    super.initState();
    _searchController.addListener(_handleSearchChanged);
  }

  @override
  void dispose() {
    _searchController
      ..removeListener(_handleSearchChanged)
      ..dispose();
    super.dispose();
  }

  void _handleSearchChanged() {
    setState(() {});
  }

  List<FSItem> get _visibleItems {
    final query = _searchController.text.trim().toLowerCase();
    return widget.items.where((item) {
      final typeInfo = fileTypeInfoFor(item.name, isDir: item.isDir);
      if (_typeFilter != _allTypeFilter && typeInfo.label != _typeFilter) {
        return false;
      }
      if (query.isEmpty) {
        return true;
      }
      return item.name.toLowerCase().contains(query);
    }).toList(growable: false);
  }

  List<String> get _availableTypeFilters {
    final presentTypes = widget.items
        .map((item) => fileTypeInfoFor(item.name, isDir: item.isDir).label)
        .toSet();
    return [
      _allTypeFilter,
      ..._typeOrder.where(
        (type) => presentTypes.contains(type) || type == _typeFilter,
      ),
    ];
  }

  @override
  Widget build(BuildContext context) {
    final visibleItems = _visibleItems;
    final hasActiveFilter = _searchController.text.trim().isNotEmpty ||
        _typeFilter != _allTypeFilter;
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
                      onPressed: widget.onRefresh,
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
                    widget.currentPath.isEmpty ? '.' : widget.currentPath,
                    style: Theme.of(context).textTheme.bodySmall,
                  ),
                ),
                const SizedBox(height: 10),
                Row(
                  children: [
                    OutlinedButton.icon(
                      onPressed: widget.onGoParent,
                      icon: const Icon(Icons.arrow_upward),
                      label: const Text('上一级'),
                    ),
                    const Spacer(),
                    if (hasActiveFilter)
                      TextButton(
                        onPressed: () {
                          _searchController.clear();
                          setState(() {
                            _typeFilter = _allTypeFilter;
                          });
                        },
                        child: const Text('清除筛选'),
                      ),
                  ],
                ),
                const SizedBox(height: 10),
                TextField(
                  controller: _searchController,
                  textInputAction: TextInputAction.search,
                  decoration: InputDecoration(
                    isDense: true,
                    prefixIcon: const Icon(Icons.search_rounded),
                    hintText: '按文件名搜索当前目录',
                    suffixIcon: _searchController.text.trim().isEmpty
                        ? null
                        : IconButton(
                            onPressed: _searchController.clear,
                            icon: const Icon(Icons.close_rounded),
                            tooltip: '清空搜索',
                          ),
                  ),
                ),
                const SizedBox(height: 8),
                SizedBox(
                  height: 36,
                  child: ListView.separated(
                    scrollDirection: Axis.horizontal,
                    itemCount: _availableTypeFilters.length,
                    separatorBuilder: (_, __) => const SizedBox(width: 6),
                    itemBuilder: (context, index) {
                      final type = _availableTypeFilters[index];
                      return FilterChip(
                        label: Text(type),
                        selected: _typeFilter == type,
                        onSelected: (_) {
                          setState(() {
                            _typeFilter = type;
                          });
                        },
                      );
                    },
                  ),
                ),
                const SizedBox(height: 12),
                Expanded(
                  child: widget.loading
                      ? const Center(child: CircularProgressIndicator())
                      : widget.items.isEmpty
                          ? const Center(child: Text('当前目录没有可显示内容'))
                          : visibleItems.isEmpty
                              ? const Center(child: Text('没有匹配的文件'))
                              : ListView.separated(
                                  itemCount: visibleItems.length,
                                  separatorBuilder: (_, __) =>
                                      const SizedBox(height: 6),
                                  itemBuilder: (context, index) {
                                    final item = visibleItems[index];
                                    final path = _joinPath(
                                        widget.currentPath, item.name);
                                    final typeInfo = fileTypeInfoFor(
                                      item.name,
                                      isDir: item.isDir,
                                    );
                                    return Material(
                                      color: Colors.transparent,
                                      child: ListTile(
                                        dense: true,
                                        shape: RoundedRectangleBorder(
                                            borderRadius:
                                                BorderRadius.circular(12)),
                                        leading: _FileTypeIcon(info: typeInfo),
                                        title: Text(
                                          item.name,
                                          maxLines: 1,
                                          overflow: TextOverflow.ellipsis,
                                        ),
                                        subtitle: Text(item.isDir
                                            ? typeInfo.label
                                            : '${typeInfo.label} · ${_sizeLabel(item.size)}'),
                                        onTap: () {
                                          if (item.isDir) {
                                            widget.onOpenDirectory(path);
                                          } else {
                                            widget.onOpenFile(path);
                                          }
                                        },
                                        onLongPress: item.isDir
                                            ? null
                                            : () => widget.onDownloadFile(path),
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

class _FileTypeIcon extends StatelessWidget {
  const _FileTypeIcon({required this.info});

  final FileTypeInfo info;

  @override
  Widget build(BuildContext context) {
    return Container(
      width: 40,
      height: 40,
      decoration: BoxDecoration(
        color: info.color.withValues(alpha: 0.14),
        borderRadius: BorderRadius.circular(12),
      ),
      child: Icon(
        info.icon,
        color: info.color,
        size: 22,
      ),
    );
  }
}
