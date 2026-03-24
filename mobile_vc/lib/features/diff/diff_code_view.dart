import 'package:flutter/material.dart';

class DiffPreviewCard extends StatelessWidget {
  const DiffPreviewCard({
    super.key,
    required this.title,
    required this.path,
    required this.diff,
    this.timestampLabel = '',
    this.previewLineCount = 12,
    this.footer,
    this.onOpen,
  });

  final String title;
  final String path;
  final String diff;
  final String timestampLabel;
  final int previewLineCount;
  final Widget? footer;
  final VoidCallback? onOpen;

  @override
  Widget build(BuildContext context) {
    final scheme = Theme.of(context).colorScheme;
    final preview = _previewDiff(diff, previewLineCount);
    return Container(
      decoration: BoxDecoration(
        color: scheme.tertiaryContainer.withValues(alpha: 0.2),
        borderRadius: BorderRadius.circular(22),
        border: Border.all(color: scheme.tertiary.withValues(alpha: 0.28)),
      ),
      padding: const EdgeInsets.all(16),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Row(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Expanded(
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    Text(
                      timestampLabel.isEmpty
                          ? '文件改动'
                          : '文件改动 · $timestampLabel',
                      style: Theme.of(context).textTheme.labelSmall?.copyWith(
                            color: scheme.tertiary,
                            fontWeight: FontWeight.w700,
                          ),
                    ),
                    const SizedBox(height: 8),
                    Text(
                      title.isEmpty
                          ? (path.isEmpty ? '最近改动' : path.split('/').last)
                          : title,
                      maxLines: 1,
                      overflow: TextOverflow.ellipsis,
                      style: Theme.of(context)
                          .textTheme
                          .titleSmall
                          ?.copyWith(fontWeight: FontWeight.w700),
                    ),
                    if (path.isNotEmpty) ...[
                      const SizedBox(height: 4),
                      Text(
                        path,
                        style: Theme.of(context)
                            .textTheme
                            .bodySmall
                            ?.copyWith(color: scheme.onSurfaceVariant),
                      ),
                    ],
                  ],
                ),
              ),
              const SizedBox(width: 12),
              OutlinedButton(
                onPressed: onOpen,
                child: const Text('查看'),
              ),
            ],
          ),
          const SizedBox(height: 12),
          DiffCodeView(diff: preview, emptyLabel: '当前没有 diff 内容'),
          if (footer != null) ...[
            const SizedBox(height: 12),
            footer!,
          ],
        ],
      ),
    );
  }

  String _previewDiff(String value, int lineCount) {
    final lines = value.split('\n');
    if (lines.length <= lineCount) {
      return value;
    }
    return '${lines.take(lineCount).join('\n')}\n…';
  }
}

class DiffCodeView extends StatelessWidget {
  const DiffCodeView({
    super.key,
    required this.diff,
    this.emptyLabel = '当前没有 diff 内容',
    this.padding = const EdgeInsets.all(14),
  });

  final String diff;
  final String emptyLabel;
  final EdgeInsetsGeometry padding;

  @override
  Widget build(BuildContext context) {
    final lines = diff.isEmpty ? <String>[emptyLabel] : diff.split('\n');
    return Container(
      width: double.infinity,
      padding: padding,
      decoration: BoxDecoration(
        color: const Color(0xFF0F172A),
        borderRadius: BorderRadius.circular(18),
      ),
      child: ListView.builder(
        shrinkWrap: true,
        physics: const NeverScrollableScrollPhysics(),
        itemCount: lines.length,
        itemBuilder: (context, index) {
          final line = lines[index];
          final style = DiffLineTheme.styleForLine(line);
          return Container(
            width: double.infinity,
            color: style.background,
            padding: const EdgeInsets.symmetric(horizontal: 8, vertical: 2),
            child: SelectableText(
              line,
              style: TextStyle(
                color: style.foreground,
                fontFamily: 'monospace',
                height: 1.45,
              ),
            ),
          );
        },
      ),
    );
  }
}

class DiffLineTheme {
  static DiffLineStyle styleForLine(String line) {
    if (line.startsWith('diff --git') ||
        line.startsWith('index ') ||
        line.startsWith('@@')) {
      return const DiffLineStyle(Color(0xFFFDE68A), Color(0x1EF59E0B));
    }
    if (line.startsWith('+++') || line.startsWith('---')) {
      return const DiffLineStyle(Color(0xFF93C5FD), Color(0x1E60A5FA));
    }
    if (line.startsWith('+')) {
      return const DiffLineStyle(Color(0xFF86EFAC), Color(0x1E22C55E));
    }
    if (line.startsWith('-')) {
      return const DiffLineStyle(Color(0xFFFCA5A5), Color(0x1EF43F5E));
    }
    return const DiffLineStyle(Color(0xFFE2E8F0), Colors.transparent);
  }
}

class DiffLineStyle {
  const DiffLineStyle(this.foreground, this.background);

  final Color foreground;
  final Color background;
}
