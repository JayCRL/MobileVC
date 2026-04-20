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
          DiffCodeView(
            diff: preview,
            emptyLabel: '当前没有 diff 内容',
            collapseUnchanged: false,
            inlineHighlight: false,
          ),
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

class DiffCodeView extends StatefulWidget {
  const DiffCodeView({
    super.key,
    required this.diff,
    this.emptyLabel = '当前没有 diff 内容',
    this.padding = const EdgeInsets.all(14),
    this.collapseUnchanged = true,
    this.inlineHighlight = true,
    this.collapseThreshold = 6,
    this.collapseContextLines = 3,
  });

  final String diff;
  final String emptyLabel;
  final EdgeInsetsGeometry padding;
  final bool collapseUnchanged;
  final bool inlineHighlight;
  final int collapseThreshold;
  final int collapseContextLines;

  @override
  State<DiffCodeView> createState() => _DiffCodeViewState();
}

class _DiffCodeViewState extends State<DiffCodeView> {
  final Set<int> _expandedFolds = <int>{};

  @override
  void didUpdateWidget(covariant DiffCodeView oldWidget) {
    super.didUpdateWidget(oldWidget);
    if (oldWidget.diff != widget.diff) {
      _expandedFolds.clear();
    }
  }

  @override
  Widget build(BuildContext context) {
    if (widget.diff.isEmpty) {
      return _emptyView(context);
    }
    final rows = _parseDiff(widget.diff);
    final blocks = _groupBlocks(rows);
    final widgets = <Widget>[];
    for (var i = 0; i < blocks.length; i++) {
      final block = blocks[i];
      widgets.addAll(_renderBlock(block, i));
    }
    return Container(
      width: double.infinity,
      padding: widget.padding,
      decoration: BoxDecoration(
        color: const Color(0xFF0F172A),
        borderRadius: BorderRadius.circular(18),
      ),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: widgets,
      ),
    );
  }

  Widget _emptyView(BuildContext context) {
    return Container(
      width: double.infinity,
      padding: widget.padding,
      decoration: BoxDecoration(
        color: const Color(0xFF0F172A),
        borderRadius: BorderRadius.circular(18),
      ),
      child: Text(
        widget.emptyLabel,
        style: const TextStyle(
          color: Color(0xFFE2E8F0),
          fontFamily: 'monospace',
          height: 1.45,
        ),
      ),
    );
  }

  List<_DiffRow> _parseDiff(String value) {
    final rows = <_DiffRow>[];
    for (final raw in value.split('\n')) {
      rows.add(_classify(raw));
    }
    return rows;
  }

  _DiffRow _classify(String line) {
    if (line.startsWith('diff --git') || line.startsWith('index ')) {
      return _DiffRow(_DiffKind.meta, line, line);
    }
    if (line.startsWith('@@')) {
      return _DiffRow(_DiffKind.hunk, line, line);
    }
    if (line.startsWith('+++') || line.startsWith('---')) {
      return _DiffRow(_DiffKind.fileHeader, line, line);
    }
    if (line.startsWith('+')) {
      return _DiffRow(_DiffKind.add, line, line.substring(1));
    }
    if (line.startsWith('-')) {
      return _DiffRow(_DiffKind.del, line, line.substring(1));
    }
    final content = line.startsWith(' ') ? line.substring(1) : line;
    return _DiffRow(_DiffKind.context, line, content);
  }

  List<_DiffBlock> _groupBlocks(List<_DiffRow> rows) {
    final blocks = <_DiffBlock>[];
    var i = 0;
    while (i < rows.length) {
      final row = rows[i];
      switch (row.kind) {
        case _DiffKind.meta:
        case _DiffKind.fileHeader:
        case _DiffKind.hunk:
          blocks.add(_DiffBlock(_BlockKind.meta, rows: [row]));
          i++;
          break;
        case _DiffKind.context:
          final buffer = <_DiffRow>[];
          while (i < rows.length && rows[i].kind == _DiffKind.context) {
            buffer.add(rows[i]);
            i++;
          }
          blocks.add(_DiffBlock(_BlockKind.context, rows: buffer));
          break;
        case _DiffKind.del:
          final dels = <_DiffRow>[];
          while (i < rows.length && rows[i].kind == _DiffKind.del) {
            dels.add(rows[i]);
            i++;
          }
          final adds = <_DiffRow>[];
          while (i < rows.length && rows[i].kind == _DiffKind.add) {
            adds.add(rows[i]);
            i++;
          }
          if (adds.isEmpty) {
            blocks.add(_DiffBlock(_BlockKind.pureDel, rows: dels));
          } else {
            blocks.add(_DiffBlock(_BlockKind.change, dels: dels, adds: adds));
          }
          break;
        case _DiffKind.add:
          final adds = <_DiffRow>[];
          while (i < rows.length && rows[i].kind == _DiffKind.add) {
            adds.add(rows[i]);
            i++;
          }
          blocks.add(_DiffBlock(_BlockKind.pureAdd, rows: adds));
          break;
      }
    }
    return blocks;
  }

  List<Widget> _renderBlock(_DiffBlock block, int blockIndex) {
    switch (block.kind) {
      case _BlockKind.meta:
        return block.rows!.map(_renderMetaLine).toList();
      case _BlockKind.context:
        return _renderContextBlock(block.rows!, blockIndex);
      case _BlockKind.pureDel:
        return block.rows!
            .map((row) => _plainLine(row.raw, _delBg, _delFg))
            .toList();
      case _BlockKind.pureAdd:
        return block.rows!
            .map((row) => _plainLine(row.raw, _addBg, _addFg))
            .toList();
      case _BlockKind.change:
        return _renderChangeBlock(block);
    }
  }

  Widget _renderMetaLine(_DiffRow row) {
    final bg = row.kind == _DiffKind.hunk ? _hunkBg : _metaBg;
    final fg = row.kind == _DiffKind.hunk ? _hunkFg : _metaFg;
    return _plainLine(row.raw, bg, fg);
  }

  List<Widget> _renderContextBlock(List<_DiffRow> rows, int blockIndex) {
    final threshold = widget.collapseUnchanged ? widget.collapseThreshold : 1 << 30;
    if (rows.length <= threshold) {
      return rows
          .map((row) => _plainLine(row.raw, Colors.transparent, _contextFg))
          .toList();
    }
    final isExpanded = _expandedFolds.contains(blockIndex);
    if (isExpanded) {
      return [
        ...rows.map(
            (row) => _plainLine(row.raw, Colors.transparent, _contextFg)),
        _foldToggle(blockIndex, rows.length, collapse: true),
      ];
    }
    final head = rows.take(widget.collapseContextLines).toList();
    final tail = rows.length > widget.collapseContextLines * 2
        ? rows.sublist(rows.length - widget.collapseContextLines)
        : <_DiffRow>[];
    final hiddenCount = rows.length - head.length - tail.length;
    final widgets = <Widget>[
      ...head.map(
          (row) => _plainLine(row.raw, Colors.transparent, _contextFg)),
    ];
    if (hiddenCount > 0) {
      widgets.add(_foldToggle(blockIndex, hiddenCount, collapse: false));
    }
    widgets.addAll(tail
        .map((row) => _plainLine(row.raw, Colors.transparent, _contextFg)));
    return widgets;
  }

  Widget _foldToggle(int blockIndex, int hiddenCount, {required bool collapse}) {
    return InkWell(
      onTap: () {
        setState(() {
          if (collapse) {
            _expandedFolds.remove(blockIndex);
          } else {
            _expandedFolds.add(blockIndex);
          }
        });
      },
      child: Container(
        width: double.infinity,
        color: const Color(0x143B82F6),
        padding: const EdgeInsets.symmetric(horizontal: 8, vertical: 3),
        child: Text(
          collapse
              ? '收起未变化的行'
              : '… $hiddenCount 行未变化（点击展开）',
          style: const TextStyle(
            color: Color(0xFF93C5FD),
            fontFamily: 'monospace',
            fontSize: 12,
            height: 1.45,
          ),
        ),
      ),
    );
  }

  List<Widget> _renderChangeBlock(_DiffBlock block) {
    final widgets = <Widget>[];
    final dels = block.dels ?? const <_DiffRow>[];
    final adds = block.adds ?? const <_DiffRow>[];
    final pairCount = dels.length < adds.length ? dels.length : adds.length;
    for (var i = 0; i < pairCount; i++) {
      widgets.addAll(_renderPair(dels[i], adds[i]));
    }
    if (dels.length > pairCount) {
      for (final row in dels.sublist(pairCount)) {
        widgets.add(_plainLine(row.raw, _delBg, _delFg));
      }
    }
    if (adds.length > pairCount) {
      for (final row in adds.sublist(pairCount)) {
        widgets.add(_plainLine(row.raw, _addBg, _addFg));
      }
    }
    return widgets;
  }

  List<Widget> _renderPair(_DiffRow del, _DiffRow add) {
    if (!widget.inlineHighlight) {
      return [
        _plainLine(del.raw, _delBg, _delFg),
        _plainLine(add.raw, _addBg, _addFg),
      ];
    }
    final segments = _inlineDiffSegments(del.content, add.content);
    return [
      _richLine('-', _delBg, _delFg, segments.oldSpans(_delAccentBg, _delFg)),
      _richLine('+', _addBg, _addFg, segments.newSpans(_addAccentBg, _addFg)),
    ];
  }

  Widget _plainLine(String text, Color bg, Color fg) {
    return Container(
      width: double.infinity,
      color: bg,
      padding: const EdgeInsets.symmetric(horizontal: 8, vertical: 2),
      child: SelectableText(
        text,
        style: TextStyle(
          color: fg,
          fontFamily: 'monospace',
          height: 1.45,
        ),
      ),
    );
  }

  Widget _richLine(
    String sign,
    Color bg,
    Color fg,
    List<TextSpan> spans,
  ) {
    return Container(
      width: double.infinity,
      color: bg,
      padding: const EdgeInsets.symmetric(horizontal: 8, vertical: 2),
      child: SelectableText.rich(
        TextSpan(
          style: TextStyle(
            color: fg,
            fontFamily: 'monospace',
            height: 1.45,
          ),
          children: [
            TextSpan(text: sign),
            ...spans,
          ],
        ),
      ),
    );
  }

  static const Color _delFg = Color(0xFFFCA5A5);
  static const Color _delBg = Color(0x1EF43F5E);
  static const Color _delAccentBg = Color(0x66F43F5E);
  static const Color _addFg = Color(0xFF86EFAC);
  static const Color _addBg = Color(0x1E22C55E);
  static const Color _addAccentBg = Color(0x6622C55E);
  static const Color _metaFg = Color(0xFFFDE68A);
  static const Color _metaBg = Color(0x1EF59E0B);
  static const Color _hunkFg = Color(0xFFFDE68A);
  static const Color _hunkBg = Color(0x2EF59E0B);
  static const Color _contextFg = Color(0xFFE2E8F0);
}

enum _DiffKind { meta, hunk, fileHeader, context, add, del }
enum _BlockKind { meta, context, change, pureDel, pureAdd }

class _DiffRow {
  const _DiffRow(this.kind, this.raw, this.content);
  final _DiffKind kind;
  final String raw;
  final String content;
}

class _DiffBlock {
  _DiffBlock(
    this.kind, {
    this.rows,
    this.dels,
    this.adds,
  });

  final _BlockKind kind;
  final List<_DiffRow>? rows;
  final List<_DiffRow>? dels;
  final List<_DiffRow>? adds;
}

class _InlineSegments {
  _InlineSegments(this.segments);
  final List<_InlineSegment> segments;

  List<TextSpan> oldSpans(Color accentBg, Color baseFg) {
    final result = <TextSpan>[];
    for (final seg in segments) {
      if (seg.kind == _SegmentKind.common) {
        result.add(TextSpan(text: seg.oldText));
      } else if (seg.kind == _SegmentKind.replace ||
          seg.kind == _SegmentKind.remove) {
        result.add(TextSpan(
          text: seg.oldText,
          style: TextStyle(backgroundColor: accentBg),
        ));
      }
    }
    return result;
  }

  List<TextSpan> newSpans(Color accentBg, Color baseFg) {
    final result = <TextSpan>[];
    for (final seg in segments) {
      if (seg.kind == _SegmentKind.common) {
        result.add(TextSpan(text: seg.newText));
      } else if (seg.kind == _SegmentKind.replace ||
          seg.kind == _SegmentKind.insert) {
        result.add(TextSpan(
          text: seg.newText,
          style: TextStyle(backgroundColor: accentBg),
        ));
      }
    }
    return result;
  }
}

enum _SegmentKind { common, replace, insert, remove }

class _InlineSegment {
  _InlineSegment(this.kind, this.oldText, this.newText);
  final _SegmentKind kind;
  final String oldText;
  final String newText;
}

/// 对较短的两行做 LCS 字符级 diff；超长行退化为整行标记，避免 O(n*m) 卡顿。
_InlineSegments _inlineDiffSegments(String oldText, String newText) {
  const maxLen = 400;
  if (oldText.length > maxLen || newText.length > maxLen) {
    return _InlineSegments([
      _InlineSegment(_SegmentKind.replace, oldText, newText),
    ]);
  }
  if (oldText == newText) {
    return _InlineSegments([
      _InlineSegment(_SegmentKind.common, oldText, newText),
    ]);
  }
  final a = oldText.runes.toList(growable: false);
  final b = newText.runes.toList(growable: false);
  final rows = a.length + 1;
  final cols = b.length + 1;
  // 用 Uint16 矩阵会更省内存，此处用 List<int> 即可满足需求。
  final dp = List<int>.filled(rows * cols, 0);
  for (var i = a.length - 1; i >= 0; i--) {
    for (var j = b.length - 1; j >= 0; j--) {
      if (a[i] == b[j]) {
        dp[i * cols + j] = dp[(i + 1) * cols + (j + 1)] + 1;
      } else {
        final down = dp[(i + 1) * cols + j];
        final right = dp[i * cols + (j + 1)];
        dp[i * cols + j] = down > right ? down : right;
      }
    }
  }
  final segments = <_InlineSegment>[];
  final oldBuf = StringBuffer();
  final newBuf = StringBuffer();
  _SegmentKind? currentKind;

  void flush() {
    if (currentKind == null) return;
    segments.add(_InlineSegment(
      currentKind!,
      oldBuf.toString(),
      newBuf.toString(),
    ));
    currentKind = null;
    oldBuf.clear();
    newBuf.clear();
  }

  void push(_SegmentKind kind, {String? oldCh, String? newCh}) {
    if (currentKind != kind) {
      flush();
      currentKind = kind;
    }
    if (oldCh != null) oldBuf.write(oldCh);
    if (newCh != null) newBuf.write(newCh);
  }

  var i = 0;
  var j = 0;
  while (i < a.length && j < b.length) {
    if (a[i] == b[j]) {
      final ch = String.fromCharCode(a[i]);
      push(_SegmentKind.common, oldCh: ch, newCh: ch);
      i++;
      j++;
    } else if (dp[(i + 1) * cols + j] >= dp[i * cols + (j + 1)]) {
      push(_SegmentKind.remove, oldCh: String.fromCharCode(a[i]));
      i++;
    } else {
      push(_SegmentKind.insert, newCh: String.fromCharCode(b[j]));
      j++;
    }
  }
  while (i < a.length) {
    push(_SegmentKind.remove, oldCh: String.fromCharCode(a[i]));
    i++;
  }
  while (j < b.length) {
    push(_SegmentKind.insert, newCh: String.fromCharCode(b[j]));
    j++;
  }
  flush();

  // 合并紧邻的 remove + insert → replace，视觉上更紧凑。
  final merged = <_InlineSegment>[];
  for (final seg in segments) {
    if (merged.isNotEmpty &&
        ((merged.last.kind == _SegmentKind.remove &&
                seg.kind == _SegmentKind.insert) ||
            (merged.last.kind == _SegmentKind.insert &&
                seg.kind == _SegmentKind.remove))) {
      final prev = merged.removeLast();
      merged.add(_InlineSegment(
        _SegmentKind.replace,
        prev.oldText + seg.oldText,
        prev.newText + seg.newText,
      ));
    } else {
      merged.add(seg);
    }
  }
  return _InlineSegments(merged);
}
