import 'dart:typed_data';

import 'package:flutter/material.dart';
import 'package:flutter_markdown/flutter_markdown.dart';
import 'package:flutter_test/flutter_test.dart';

import 'package:mobile_vc/data/models/events.dart';
import 'package:mobile_vc/data/models/session_models.dart';
import 'package:mobile_vc/widgets/event_card.dart';

void main() {
  testWidgets('markdown reply uses SelectionArea for cross-block selection',
      (tester) async {
    await tester.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: EventCard(
            item: TimelineItem(
              id: 'md-1',
              kind: 'markdown',
              timestamp: DateTime(2026, 4, 4, 12),
              body: '# Title\n\nfirst line\nsecond line\n\n- item 1\n- item 2',
            ),
          ),
        ),
      ),
    );

    expect(find.byType(SelectionArea), findsOneWidget);
    final markdown = tester.widget<MarkdownBody>(find.byType(MarkdownBody));
    expect(markdown.selectable, isFalse);
  });

  testWidgets('markdown typewriter advances when same item body extends',
      (tester) async {
    final baseItem = TimelineItem(
      id: 'stream-md-1',
      kind: 'markdown',
      timestamp: DateTime(2026, 4, 4, 12),
      body: '正在生成回复。',
    );
    final expandedItem = baseItem.copyWith(
      body: '正在生成回复。新增内容应该立即参与布局刷新。',
    );

    await tester.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: EventCard(item: baseItem),
        ),
      ),
    );
    await tester.pump(const Duration(milliseconds: 80));

    await tester.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: EventCard(item: expandedItem),
        ),
      ),
    );

    final markdown = tester.widget<MarkdownBody>(find.byType(MarkdownBody));
    expect(markdown.data, startsWith(baseItem.body));

    await tester.pump(const Duration(milliseconds: 80));
    final advanced = tester.widget<MarkdownBody>(find.byType(MarkdownBody));
    expect(advanced.data.length, greaterThan(baseItem.body.length));
  });

  testWidgets('markdown typewriter drops stale cache when same id body changes',
      (tester) async {
    final baseItem = TimelineItem(
      id: 'stream-md-cache',
      kind: 'markdown',
      timestamp: DateTime(2026, 4, 4, 12),
      body: '第一段正在生成的回复内容。',
    );
    final replacementItem = baseItem.copyWith(
      body: '完全不同的新回复内容。',
    );

    await tester.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: EventCard(item: baseItem),
        ),
      ),
    );
    await tester.pump(const Duration(milliseconds: 80));

    await tester.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: EventCard(item: replacementItem),
        ),
      ),
    );

    final markdown = tester.widget<MarkdownBody>(find.byType(MarkdownBody));
    expect(markdown.data, isNot(startsWith('第一段')));
    expect(replacementItem.body, startsWith(markdown.data));
  });

  testWidgets('markdown typewriter batches progress callbacks for long replies',
      (tester) async {
    var progressCalls = 0;
    final longBody = List<String>.generate(
      80,
      (index) => '第 $index 行 live assistant 输出需要减少 markdown 重建。',
    ).join('\n');

    await tester.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: SingleChildScrollView(
            child: EventCard(
              item: TimelineItem(
                id: 'stream-md-batched',
                kind: 'markdown',
                timestamp: DateTime(2026, 4, 4, 12),
                body: longBody,
              ),
              onAnimatedBodyProgress: () => progressCalls++,
            ),
          ),
        ),
      ),
    );

    for (var i = 0; i < 5; i++) {
      await tester.pump(const Duration(milliseconds: 16));
    }

    expect(progressCalls, lessThanOrEqualTo(2));
    final callsAfterFirstChunk = progressCalls;
    await tester.pump(const Duration(milliseconds: 16));
    expect(progressCalls, lessThanOrEqualTo(callsAfterFirstChunk + 1));

    await tester.pump(const Duration(seconds: 3));
    final markdown = tester.widget<MarkdownBody>(find.byType(MarkdownBody));
    expect(markdown.data, longBody);
    expect(progressCalls, lessThan(80));
  });

  testWidgets('compaction marker renders loading state', (tester) async {
    await tester.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: EventCard(
            item: TimelineItem(
              id: 'compaction-1',
              kind: 'compaction',
              timestamp: DateTime(2026, 4, 4, 12),
              status: 'loading',
              trigger: 'manual',
            ),
          ),
        ),
      ),
    );

    expect(find.text('压缩中'), findsOneWidget);
    expect(find.byType(CircularProgressIndicator), findsOneWidget);
  });

  testWidgets('compaction marker renders completed state', (tester) async {
    await tester.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: EventCard(
            item: TimelineItem(
              id: 'compaction-2',
              kind: 'compaction',
              timestamp: DateTime(2026, 4, 4, 12),
              status: 'completed',
              trigger: 'manual',
            ),
          ),
        ),
      ),
    );

    expect(find.text('已压缩'), findsOneWidget);
    expect(find.byIcon(Icons.content_cut_rounded), findsOneWidget);
  });

  testWidgets('compaction marker renders failed state', (tester) async {
    await tester.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: EventCard(
            item: TimelineItem(
              id: 'compaction-3',
              kind: 'compaction',
              timestamp: DateTime(2026, 4, 4, 12),
              status: 'failed',
              trigger: 'manual',
              body: 'backend failed',
            ),
          ),
        ),
      ),
    );

    expect(find.text('压缩失败'), findsOneWidget);
    expect(find.text('backend failed'), findsOneWidget);
    expect(find.byIcon(Icons.error_outline_rounded), findsOneWidget);
  });

  testWidgets('codex native tool group is collapsed until tapped',
      (tester) async {
    await tester.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: EventCard(
            item: TimelineItem(
              id: 'codex-tools-1',
              kind: 'codex_tool_group',
              timestamp: DateTime(2026, 4, 4, 12),
              title: 'Codex 原生操作',
              status: '工具调用 1 · 输出 1 · Patch 1',
              body: '## 工具调用 (1)\n\n'
                  '- **functions.exec_command**\n'
                  '  `ls`\n\n'
                  '## 工具输出 (1)\n\n'
                  '- **functions.exec_command**\n'
                  '  exit 0',
              codexSteps: const [
                '正在读取 codex_transport.go',
                '正在创建智能体：agent-019e7126',
              ],
            ),
          ),
        ),
      ),
    );

    expect(find.text('Codex 原生操作'), findsOneWidget);
    expect(find.text('工具调用 1 · 输出 1 · Patch 1'), findsOneWidget);
    expect(find.text('正在读取 codex_transport.go'), findsOneWidget);
    expect(find.text('正在创建智能体：agent-019e7126'), findsOneWidget);
    expect(find.byKey(const ValueKey('codexToolGroupSteps')), findsOneWidget);
    expect(find.text('工具调用 (1)'), findsNothing);

    await tester.tap(find.byKey(const ValueKey('codexToolGroupToggle')));
    await tester.pumpAndSettle();

    expect(find.byType(MarkdownBody), findsOneWidget);
    final detail = tester.widget<MarkdownBody>(find.byType(MarkdownBody)).data;
    expect(detail, contains('## 工具调用 (1)'));
    expect(detail, contains('## 工具输出 (1)'));
    expect(detail, contains('functions.exec_command'));
    expect(detail, contains('exit 0'));
    expect(find.text('工具调用 (1)'), findsOneWidget);
    expect(find.text('工具输出 (1)'), findsOneWidget);
  });

  testWidgets('thinking card collapses by default and expands on tap',
      (tester) async {
    const detail = 'DETAIL_SEGMENT_COLLAPSED_BY_DEFAULT';
    const body = '第一段思考内容会比较长，用来确认折叠状态只显示预览。第二段内容应该在展开之后才可见。'
        '\n\n$detail';

    await tester.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: EventCard(
            item: TimelineItem(
              id: 'thinking-1',
              kind: 'thinking',
              timestamp: DateTime(2026, 4, 4, 12),
              body: body,
            ),
            collapseThinkingByDefault: true,
          ),
        ),
      ),
    );

    expect(find.text('思考过程'), findsOneWidget);
    expect(find.textContaining(detail), findsNothing);
    expect(find.byIcon(Icons.expand_more_rounded), findsOneWidget);

    await tester.tap(find.byKey(const ValueKey('thinkingToggle')));
    await tester.pumpAndSettle();

    expect(find.textContaining(detail), findsOneWidget);
    expect(find.byIcon(Icons.expand_less_rounded), findsOneWidget);
  });

  testWidgets('thinking card stays expanded when default collapse is false',
      (tester) async {
    const body = '这是仍在进行中的思考内容，结果出现前应该默认展开。';

    await tester.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: EventCard(
            item: TimelineItem(
              id: 'thinking-expanded',
              kind: 'thinking',
              timestamp: DateTime(2026, 4, 4, 12),
              body: body,
            ),
          ),
        ),
      ),
    );

    expect(find.text(body), findsOneWidget);
    expect(find.byIcon(Icons.expand_less_rounded), findsOneWidget);
  });

  testWidgets('thinking card auto-collapses when default changes',
      (tester) async {
    const detail = 'DETAIL_SEGMENT_AUTO_COLLAPSE';
    const body = '同一条思考内容在后续助手结果出现后应该自动折叠。'
        '\n\n$detail';
    final item = TimelineItem(
      id: 'thinking-auto-collapse',
      kind: 'thinking',
      timestamp: DateTime(2026, 4, 4, 12),
      body: body,
    );

    await tester.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: EventCard(item: item),
        ),
      ),
    );

    expect(find.textContaining(detail), findsOneWidget);

    await tester.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: EventCard(
            item: item,
            collapseThinkingByDefault: true,
          ),
        ),
      ),
    );

    expect(find.textContaining(detail), findsNothing);
    expect(find.byIcon(Icons.expand_more_rounded), findsOneWidget);
  });

  testWidgets('thinking manual expansion survives rebuild for same item',
      (tester) async {
    const body = '用户手动展开后，即使同一条思考继续刷新，也不应该被默认折叠重新收起。';
    final item = TimelineItem(
      id: 'thinking-manual-toggle',
      kind: 'thinking',
      timestamp: DateTime(2026, 4, 4, 12),
      body: body,
    );

    await tester.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: EventCard(
            item: item,
            collapseThinkingByDefault: true,
          ),
        ),
      ),
    );

    await tester.tap(find.byKey(const ValueKey('thinkingToggle')));
    await tester.pumpAndSettle();
    expect(find.text(body), findsOneWidget);

    await tester.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: EventCard(
            item: item.copyWith(body: '$body\n新增摘要片段。'),
            collapseThinkingByDefault: true,
          ),
        ),
      ),
    );

    expect(find.textContaining('新增摘要片段'), findsOneWidget);
    expect(find.byIcon(Icons.expand_less_rounded), findsOneWidget);
  });

  testWidgets('user message renders attachment metadata', (tester) async {
    TimelineAttachment? opened;

    await tester.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: EventCard(
            item: TimelineItem(
              id: 'user-attachment-1',
              kind: 'user',
              timestamp: DateTime(2026, 4, 4, 12),
              body: '看这张图',
              attachments: const [
                TimelineAttachment(
                  id: 'att-1',
                  kind: 'image',
                  name: 'screen.png',
                  mimeType: 'image/png',
                  size: 2048,
                  path: '/tmp/screen.png',
                  previewStatus: 'available',
                ),
              ],
            ),
            onOpenAttachment: (attachment) => opened = attachment,
          ),
        ),
      ),
    );

    expect(find.text('screen.png'), findsOneWidget);
    expect(find.textContaining('image/png'), findsOneWidget);
    expect(find.text('/tmp/screen.png'), findsOneWidget);
    expect(find.byKey(const ValueKey('timelineAttachment:screen.png')),
        findsOneWidget);

    await tester
        .tap(find.byKey(const ValueKey('timelineAttachment:screen.png')));
    expect(opened?.path, '/tmp/screen.png');
  });

  testWidgets('image attachment renders loaded preview bytes', (tester) async {
    final previewBytes = Uint8List.fromList(<int>[
      0x89,
      0x50,
      0x4E,
      0x47,
      0x0D,
      0x0A,
      0x1A,
      0x0A,
    ]);

    await tester.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: EventCard(
            item: TimelineItem(
              id: 'user-attachment-2',
              kind: 'user',
              timestamp: DateTime(2026, 4, 4, 12),
              attachments: const [
                TimelineAttachment(
                  id: 'att-preview',
                  kind: 'image',
                  name: 'preview.png',
                  mimeType: 'image/png',
                ),
              ],
            ),
            mediaPreviewStates: {
              'att-preview': MediaPreviewState(
                key: 'att-preview',
                status: 'ok',
                bytes: previewBytes,
              ),
            },
          ),
        ),
      ),
    );

    expect(
      find.byKey(const ValueKey('timelineAttachmentPreview:preview.png')),
      findsOneWidget,
    );
  });

  testWidgets('markdown reply renders attachment and failure state',
      (tester) async {
    TimelineAttachment? requested;

    await tester.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: EventCard(
            item: TimelineItem(
              id: 'markdown-attachment',
              kind: 'markdown',
              timestamp: DateTime(2026, 4, 4, 12),
              body: '生成好了',
              attachments: const [
                TimelineAttachment(
                  id: 'path-preview',
                  kind: 'image',
                  name: 'generated.png',
                  mimeType: 'image/png',
                  path: '/tmp/generated.png',
                ),
              ],
            ),
            mediaPreviewStates: const {
              'path-preview': MediaPreviewState(
                key: 'path-preview',
                status: 'error',
                message: 'file is missing',
              ),
            },
            onRequestMediaPreview: (attachment) => requested = attachment,
          ),
        ),
      ),
    );

    await tester.pump();

    expect(find.text('generated.png'), findsOneWidget);
    expect(find.textContaining('file is missing'), findsOneWidget);
    expect(requested, isNull);
  });
}
