import 'package:flutter/material.dart';
import 'package:flutter_markdown/flutter_markdown.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:mobile_vc/data/models/events.dart';
import 'package:mobile_vc/data/models/runtime_meta.dart';
import 'package:mobile_vc/data/models/session_models.dart';
import 'package:mobile_vc/features/chat/chat_timeline.dart';

void main() {
  testWidgets('permission interaction 会展示四档授权按钮并透传编码值', (tester) async {
    String? submitted;

    await tester.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: ChatTimeline(
            sessionId: 'test-session',
            items: const [],
            pendingInteraction: InteractionRequestEvent(
              timestamp: DateTime(2026),
              sessionId: 'session-1',
              runtimeMeta: const RuntimeMeta(command: 'codex'),
              raw: const {
                'type': 'interaction_request',
                'kind': 'permission',
              },
              kind: 'permission',
              title: 'Permission required',
              message: 'Allow editing lib/main.dart?',
              options: const [
                PromptOption(value: 'y'),
                PromptOption(value: 'n'),
              ],
            ),
            onPromptSubmit: (value) => submitted = value,
          ),
        ),
      ),
    );

    expect(find.text('允许一次'), findsOneWidget);
    expect(find.text('本会话允许'), findsOneWidget);
    expect(find.text('长期允许'), findsOneWidget);
    expect(find.text('拒绝'), findsOneWidget);

    await tester.tap(find.text('长期允许'));
    await tester.pump();

    expect(submitted, 'approve:persistent');
  });

  testWidgets('generic waiting interaction_request 即使未标记 ready 也不显示额外卡片',
      (tester) async {
    await tester.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: ChatTimeline(
            sessionId: 'test-session',
            items: const [],
            pendingInteraction: InteractionRequestEvent(
              timestamp: DateTime(2026),
              sessionId: 'session-1',
              runtimeMeta: const RuntimeMeta(command: 'claude'),
              raw: const {
                'type': 'interaction_request',
                'message': '等待输入',
              },
              title: '等待输入',
              message: '等待输入',
            ),
          ),
        ),
      ),
    );

    expect(find.text('等待输入'), findsNothing);
    expect(find.text('交互确认'), findsNothing);
  });

  testWidgets('_AiStatusIndicator 用 ValueKey 保持动画不重置', (tester) async {
    // 有 timeline 条目且 isAiRunning 时，indicator 应出现在末尾
    await tester.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: ChatTimeline(
            sessionId: 'test-session',
            items: [
              TimelineItem(
                id: 'item-1',
                kind: 'markdown',
                timestamp: DateTime(2026, 1, 1),
                title: '',
                body: '第一条消息',
              ),
            ],
            isAiRunning: true,
            aiStatusLabel: '思考中',
          ),
        ),
      ),
    );

    // 指示器应该被渲染
    expect(find.text('思考中'), findsOneWidget);

    // 用更多条目 rebuild — 指示器位置从 index 1 移到 index 5
    await tester.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: ChatTimeline(
            sessionId: 'test-session',
            items: [
              TimelineItem(
                id: 'item-1',
                kind: 'markdown',
                timestamp: DateTime(2026, 1, 1),
                title: '',
                body: '第一条消息',
              ),
              TimelineItem(
                id: 'item-2',
                kind: 'markdown',
                timestamp: DateTime(2026, 1, 1, 0, 0, 1),
                title: '',
                body: '第二条消息',
              ),
              TimelineItem(
                id: 'item-3',
                kind: 'markdown',
                timestamp: DateTime(2026, 1, 1, 0, 0, 2),
                title: '',
                body: '第三条消息',
              ),
              TimelineItem(
                id: 'item-4',
                kind: 'markdown',
                timestamp: DateTime(2026, 1, 1, 0, 0, 3),
                title: '',
                body: '第四条消息',
              ),
              TimelineItem(
                id: 'item-5',
                kind: 'markdown',
                timestamp: DateTime(2026, 1, 1, 0, 0, 4),
                title: '',
                body: '第五条消息',
              ),
            ],
            isAiRunning: true,
            aiStatusLabel: '执行中',
          ),
        ),
      ),
    );

    // 位置变了但 key 相同，指示器应该仍然渲染且 label 更新
    expect(find.text('执行中'), findsOneWidget);
    expect(find.text('思考中'), findsNothing);

    // 关键：isAiRunning 为 false 时指示器消失
    await tester.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: ChatTimeline(
            sessionId: 'test-session',
            items: [
              TimelineItem(
                id: 'item-1',
                kind: 'markdown',
                timestamp: DateTime(2026, 1, 1),
                title: '',
                body: '第一条消息',
              ),
            ],
            isAiRunning: false,
            aiStatusLabel: '',
          ),
        ),
      ),
    );

    expect(find.text('执行中'), findsNothing);
  });

  testWidgets('运行状态避开悬浮输入栏底部占位', (tester) async {
    const height = 600.0;
    const bottomPadding = 180.0;

    await tester.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: SizedBox(
            height: height,
            child: ChatTimeline(
              sessionId: 'test-session',
              bottomPadding: bottomPadding,
              items: [
                TimelineItem(
                  id: 'item-1',
                  kind: 'markdown',
                  timestamp: DateTime(2026, 1, 1),
                  title: '',
                  body: '第一条消息',
                ),
              ],
              isAiRunning: true,
              aiStatusLabel: 'Running command',
            ),
          ),
        ),
      ),
    );

    final statusBottom = tester
        .getBottomLeft(find.byKey(const ValueKey('ai-status-indicator')))
        .dy;

    expect(statusBottom, lessThan(height - bottomPadding));
  });

  testWidgets('review summary 仍插入到匹配 diff 后且不复制完整列表', (tester) async {
    final diff = HistoryContext(
      id: 'diff-1',
      type: 'diff',
      title: 'README.md',
      path: '/workspace/README.md',
      diff: '@@ -1 +1 @@',
      pendingReview: true,
    );
    final items = [
      TimelineItem(
        id: 'msg-1',
        kind: 'markdown',
        timestamp: DateTime(2026, 1, 1),
        body: '开始',
      ),
      TimelineItem(
        id: 'diff-1',
        kind: 'file_diff',
        timestamp: DateTime(2026, 1, 1, 0, 0, 1),
        title: 'README.md',
        body: '/workspace/README.md',
        context: diff,
      ),
    ];

    await tester.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: ChatTimeline(
            sessionId: 'test-session',
            items: items,
            activeReviewDiff: diff,
            pendingDiffCount: 1,
            shouldShowReviewChoices: true,
          ),
        ),
      ),
    );

    expect(find.text('README.md'), findsOneWidget);
    expect(find.text('/workspace/README.md'), findsOneWidget);

    await tester.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: ChatTimeline(
            sessionId: 'test-session',
            items: items,
            activeReviewDiff: diff,
            pendingDiffCount: 1,
            shouldShowReviewChoices: true,
            pendingPrompt: PromptRequestEvent(
              timestamp: DateTime(2026, 1, 1, 0, 0, 2),
              sessionId: 'session-1',
              runtimeMeta: const RuntimeMeta(command: 'claude'),
              raw: const {'type': 'prompt_request'},
              message: '请输入确认',
            ),
          ),
        ),
      ),
    );

    expect(find.text('README.md'), findsOneWidget);
    expect(find.text('请输入确认'), findsNothing,
        reason: 'review choices mode hides generic prompt card');
  });

  testWidgets('普通 rebuild 会复用 child index 且无 review 时不扫描全列表', (tester) async {
    final diagnostics = ChatTimelineDiagnostics();
    final items = List<TimelineItem>.generate(
      80,
      (index) => TimelineItem(
        id: 'cached-$index',
        kind: 'markdown',
        timestamp: DateTime(2026, 1, 1, 0, index),
        body: index == 79 ? '正在生成回复...' : '历史消息 $index',
        animateBody: false,
      ),
    );

    await tester.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: SizedBox(
            height: 320,
            child: ChatTimeline(
              sessionId: 'cached-session',
              items: items,
              diagnostics: diagnostics,
            ),
          ),
        ),
      ),
    );
    await tester.pump();

    final initialIndexBuilds = diagnostics.childIndexMapBuilds;
    expect(diagnostics.reviewAnchorFullScans, 0);

    for (var i = 0; i < 3; i++) {
      items[79] = items[79].copyWith(body: '正在生成回复，继续输出更多内容 $i。');
      await tester.pumpWidget(
        MaterialApp(
          home: Scaffold(
            body: SizedBox(
              height: 320,
              child: ChatTimeline(
                sessionId: 'cached-session',
                items: items,
                diagnostics: diagnostics,
              ),
            ),
          ),
        ),
      );
      await tester.pump();
    }

    expect(diagnostics.childIndexMapBuilds - initialIndexBuilds,
        lessThanOrEqualTo(1));
    expect(diagnostics.reviewAnchorFullScans, 0);
  });

  testWidgets('切换到较短历史会话时自动滚到最新消息', (tester) async {
    final longItems = List<TimelineItem>.generate(
      30,
      (index) => TimelineItem(
        id: 'long-$index',
        kind: 'markdown',
        timestamp: DateTime(2026, 1, 1, 0, index),
        body: '长会话消息 $index',
      ),
    );
    final shortItems = List<TimelineItem>.generate(
      10,
      (index) => TimelineItem(
        id: 'short-$index',
        kind: 'markdown',
        timestamp: DateTime(2026, 1, 2, 0, index),
        body: '短会话消息 $index',
      ),
    );

    await tester.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: SizedBox(
            height: 320,
            child: ChatTimeline(
              sessionId: 'long-session',
              items: longItems,
            ),
          ),
        ),
      ),
    );
    await tester.pump();
    await tester.pump();
    final scrollableState =
        tester.state<ScrollableState>(find.byType(Scrollable));
    expect(
      scrollableState.position.pixels,
      scrollableState.position.maxScrollExtent,
    );

    await tester.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: SizedBox(
            height: 320,
            child: ChatTimeline(
              sessionId: 'short-session',
              items: shortItems,
            ),
          ),
        ),
      ),
    );
    await tester.pump();
    await tester.pump();

    expect(
      scrollableState.position.pixels,
      scrollableState.position.maxScrollExtent,
    );
  });

  testWidgets('最后一条流式消息内容变长但 id 不变时自动滚到底部', (tester) async {
    final baseItems = List<TimelineItem>.generate(
      18,
      (index) => TimelineItem(
        id: 'stream-$index',
        kind: 'markdown',
        timestamp: DateTime(2026, 1, 1, 0, index),
        body: index == 17 ? '正在生成回复...' : '历史消息 $index',
        animateBody: false,
      ),
    );
    final expandedItems = [
      ...baseItems.take(17),
      baseItems.last.copyWith(
        body: List<String>.generate(
          48,
          (index) => '流式回复新增内容第 $index 行，需要列表保持在底部。',
        ).join('\n'),
      ),
    ];

    await tester.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: SizedBox(
            height: 320,
            child: ChatTimeline(
              sessionId: 'stream-session',
              bottomPadding: 24,
              items: baseItems,
            ),
          ),
        ),
      ),
    );
    await tester.pump();
    await tester.pump();

    final scrollableState =
        tester.state<ScrollableState>(find.byType(Scrollable));
    final initialMaxScrollExtent = scrollableState.position.maxScrollExtent;
    expect(scrollableState.position.pixels, initialMaxScrollExtent);

    await tester.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: SizedBox(
            height: 320,
            child: ChatTimeline(
              sessionId: 'stream-session',
              bottomPadding: 24,
              items: expandedItems,
            ),
          ),
        ),
      ),
    );
    await tester.pump();
    await tester.pump();

    expect(
      scrollableState.position.maxScrollExtent,
      greaterThan(initialMaxScrollExtent),
    );
    expect(
      scrollableState.position.pixels,
      scrollableState.position.maxScrollExtent,
    );
  });

  testWidgets('同一个 timeline 列表实例内最后一条内容变长时仍自动滚到底部', (tester) async {
    final items = List<TimelineItem>.generate(
      18,
      (index) => TimelineItem(
        id: 'live-stream-$index',
        kind: 'markdown',
        timestamp: DateTime(2026, 1, 1, 0, index),
        body: index == 17 ? '正在生成回复...' : '历史消息 $index',
        animateBody: false,
      ),
    );

    await tester.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: SizedBox(
            height: 320,
            child: ChatTimeline(
              sessionId: 'live-stream-session',
              bottomPadding: 24,
              items: items,
            ),
          ),
        ),
      ),
    );
    await tester.pump();
    await tester.pump();

    final scrollableState =
        tester.state<ScrollableState>(find.byType(Scrollable));
    final initialMaxScrollExtent = scrollableState.position.maxScrollExtent;
    expect(scrollableState.position.pixels, initialMaxScrollExtent);

    items[17] = items[17].copyWith(
      body: List<String>.generate(
        48,
        (index) => '同一个列表实例新增内容第 $index 行，需要真实页面保持底部。',
      ).join('\n'),
    );

    await tester.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: SizedBox(
            height: 320,
            child: ChatTimeline(
              sessionId: 'live-stream-session',
              bottomPadding: 24,
              items: items,
            ),
          ),
        ),
      ),
    );
    await tester.pump();
    await tester.pump();

    expect(
      scrollableState.position.maxScrollExtent,
      greaterThan(initialMaxScrollExtent),
    );
    expect(
      scrollableState.position.pixels,
      scrollableState.position.maxScrollExtent,
    );
  });

  testWidgets('前插历史消息时流式 markdown 状态不串到其他条目', (tester) async {
    final activeItems = [
      TimelineItem(
        id: 'history-existing-1',
        kind: 'markdown',
        timestamp: DateTime(2026, 1, 1),
        body: '当前历史消息',
        animateBody: false,
      ),
      TimelineItem(
        id: 'stream-active',
        kind: 'markdown',
        timestamp: DateTime(2026, 1, 1, 0, 0, 1),
        body: '正在生成的最新回复，需要保持自己的动画状态。',
      ),
    ];
    final prependedItems = [
      TimelineItem(
        id: 'history-older-0',
        kind: 'markdown',
        timestamp: DateTime(2025, 12, 31, 23, 59),
        body: '更早历史消息',
        animateBody: false,
      ),
      ...activeItems,
    ];

    await tester.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: SizedBox(
            height: 320,
            child: ChatTimeline(
              sessionId: 'prepend-history-session',
              bottomPadding: 24,
              items: activeItems,
            ),
          ),
        ),
      ),
    );
    await tester.pump(const Duration(milliseconds: 96));

    await tester.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: SizedBox(
            height: 320,
            child: ChatTimeline(
              sessionId: 'prepend-history-session',
              bottomPadding: 24,
              items: prependedItems,
            ),
          ),
        ),
      ),
    );

    expect(find.text('更早历史消息'), findsOneWidget);
    expect(find.text('当前历史消息'), findsOneWidget);

    final markdownBodies =
        tester.widgetList<MarkdownBody>(find.byType(MarkdownBody)).toList();
    expect(markdownBodies.map((body) => body.data), contains('更早历史消息'));
    expect(markdownBodies.map((body) => body.data), contains('当前历史消息'));
    expect(
      markdownBodies.map((body) => body.data).where(
            (body) => body.startsWith('正在生成'),
          ),
      isNotEmpty,
    );
  });

  testWidgets('thinking 后面已有助手结果时默认折叠', (tester) async {
    const detail = 'DETAIL_SEGMENT_TIMELINE_COLLAPSED';
    const thinkingBody = '模型正在分析上下文，这段完整思考摘要应该在结果出现后默认收起，只留下单行预览。'
        '\n\n$detail';

    await tester.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: SizedBox(
            height: 420,
            child: ChatTimeline(
              sessionId: 'thinking-collapse-session',
              items: [
                TimelineItem(
                  id: 'thinking-1',
                  kind: 'thinking',
                  timestamp: DateTime(2026, 1, 1),
                  body: thinkingBody,
                ),
                TimelineItem(
                  id: 'result-1',
                  kind: 'markdown',
                  timestamp: DateTime(2026, 1, 1, 0, 0, 1),
                  body: '最终结果已经生成。',
                  animateBody: false,
                ),
              ],
            ),
          ),
        ),
      ),
    );

    expect(find.textContaining(detail), findsNothing);
    expect(find.byIcon(Icons.expand_more_rounded), findsOneWidget);
    expect(find.text('最终结果已经生成。'), findsOneWidget);
  });

  testWidgets('最新 thinking 没有后续助手结果时保持展开', (tester) async {
    const thinkingBody = '模型仍在思考中，最新思考摘要应该保持展开方便查看实时进度。';

    await tester.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: SizedBox(
            height: 420,
            child: ChatTimeline(
              sessionId: 'thinking-expanded-session',
              items: [
                TimelineItem(
                  id: 'previous-result',
                  kind: 'markdown',
                  timestamp: DateTime(2026, 1, 1),
                  body: '上一轮结果。',
                  animateBody: false,
                ),
                TimelineItem(
                  id: 'thinking-latest',
                  kind: 'thinking',
                  timestamp: DateTime(2026, 1, 1, 0, 0, 1),
                  body: thinkingBody,
                ),
              ],
            ),
          ),
        ),
      ),
    );

    expect(find.text(thinkingBody), findsOneWidget);
    expect(find.byIcon(Icons.expand_less_rounded), findsOneWidget);
  });

  testWidgets('前插历史消息时 thinking 展开状态不串行', (tester) async {
    const detail = 'DETAIL_SEGMENT_PREPEND_STABLE';
    const thinkingBody = '这条思考已经被用户手动展开，前插历史后还应该保持展开。'
        '\n\n$detail';
    final activeItems = [
      TimelineItem(
        id: 'thinking-stable',
        kind: 'thinking',
        timestamp: DateTime(2026, 1, 1),
        body: thinkingBody,
      ),
      TimelineItem(
        id: 'result-stable',
        kind: 'markdown',
        timestamp: DateTime(2026, 1, 1, 0, 0, 1),
        body: '对应结果。',
        animateBody: false,
      ),
    ];

    await tester.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: SizedBox(
            height: 420,
            child: ChatTimeline(
              sessionId: 'thinking-prepend-session',
              items: activeItems,
            ),
          ),
        ),
      ),
    );

    expect(find.textContaining(detail), findsNothing);
    await tester.tap(find.byKey(const ValueKey('thinkingToggle')));
    await tester.pump();
    expect(find.textContaining(detail), findsOneWidget);

    await tester.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: SizedBox(
            height: 420,
            child: ChatTimeline(
              sessionId: 'thinking-prepend-session',
              items: [
                TimelineItem(
                  id: 'older-message',
                  kind: 'markdown',
                  timestamp: DateTime(2025, 12, 31, 23, 59),
                  body: '更早历史消息。',
                  animateBody: false,
                ),
                ...activeItems,
              ],
            ),
          ),
        ),
      ),
    );

    expect(find.text('更早历史消息。'), findsOneWidget);
    expect(find.textContaining(detail), findsOneWidget);
    expect(find.byIcon(Icons.expand_less_rounded), findsOneWidget);
  });

  testWidgets('正常对话流式回复逐字渲染变高时继续贴住底部', (tester) async {
    final items = List<TimelineItem>.generate(
      18,
      (index) => TimelineItem(
        id: 'normal-stream-$index',
        kind: 'markdown',
        timestamp: DateTime(2026, 1, 1, 0, index),
        body: index == 17
            ? List<String>.generate(
                64,
                (line) => '正常对话流式回复第 $line 行，需要逐步渲染并保持底部跟随。',
              ).join('\n')
            : '正常对话历史消息 $index',
        animateBody: index == 17,
      ),
    );

    await tester.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: SizedBox(
            height: 320,
            child: ChatTimeline(
              sessionId: 'normal-stream-session',
              bottomPadding: 24,
              items: items,
            ),
          ),
        ),
      ),
    );
    await tester.pump();
    await tester.pump();

    final scrollableState =
        tester.state<ScrollableState>(find.byType(Scrollable));
    expect(
      scrollableState.position.pixels,
      scrollableState.position.maxScrollExtent,
    );

    for (var i = 0; i < 8; i++) {
      await tester.pump(const Duration(milliseconds: 16));
      expect(
        scrollableState.position.pixels,
        scrollableState.position.maxScrollExtent,
      );
    }
  });

  testWidgets('底部 plan 选择卡出现时自动滚到最新交互', (tester) async {
    final items = List<TimelineItem>.generate(
      18,
      (index) => TimelineItem(
        id: 'plan-scroll-$index',
        kind: 'markdown',
        timestamp: DateTime(2026, 1, 1, 0, index),
        body: '计划前历史消息 $index',
        animateBody: false,
      ),
    );
    const question = PlanQuestion(
      id: 'implementation-order',
      title: '选择修复顺序',
      message: '先修复哪一项？',
      options: [
        PromptOption(value: 'relay', label: 'Relay'),
        PromptOption(value: 'ui', label: '界面刷新'),
      ],
    );

    await tester.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: SizedBox(
            height: 320,
            child: ChatTimeline(
              sessionId: 'plan-scroll-session',
              bottomPadding: 24,
              items: items,
            ),
          ),
        ),
      ),
    );
    await tester.pump();
    await tester.pump();

    final scrollableState =
        tester.state<ScrollableState>(find.byType(Scrollable));
    final initialMaxScrollExtent = scrollableState.position.maxScrollExtent;
    expect(scrollableState.position.pixels, initialMaxScrollExtent);

    await tester.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: SizedBox(
            height: 320,
            child: ChatTimeline(
              sessionId: 'plan-scroll-session',
              bottomPadding: 24,
              items: items,
              pendingPlanQuestion: question,
              pendingPlanProgressLabel: '1/2',
              shouldShowPlanChoices: true,
            ),
          ),
        ),
      ),
    );
    await tester.pump();
    await tester.pump();

    expect(
      scrollableState.position.maxScrollExtent,
      greaterThan(initialMaxScrollExtent),
    );
    expect(
      scrollableState.position.pixels,
      scrollableState.position.maxScrollExtent,
    );
  });

  testWidgets('底部 plan 选择卡内容变高时继续贴住底部', (tester) async {
    final items = List<TimelineItem>.generate(
      18,
      (index) => TimelineItem(
        id: 'plan-grow-$index',
        kind: 'markdown',
        timestamp: DateTime(2026, 1, 1, 0, index),
        body: '计划内容变化前历史消息 $index',
        animateBody: false,
      ),
    );
    const baseQuestion = PlanQuestion(
      id: 'sync-strategy',
      title: '同步策略',
      message: '请选择同步策略。',
      options: [
        PromptOption(value: 'fast', label: '快速'),
        PromptOption(value: 'safe', label: '稳妥'),
      ],
    );
    final expandedQuestion = PlanQuestion(
      id: baseQuestion.id,
      title: baseQuestion.title,
      message: List<String>.generate(
        16,
        (index) => '同步策略补充说明第 $index 行，需要内容变高后保持底部跟随。',
      ).join('\n'),
      options: baseQuestion.options,
    );

    await tester.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: SizedBox(
            height: 320,
            child: ChatTimeline(
              sessionId: 'plan-grow-session',
              bottomPadding: 24,
              items: items,
              pendingPlanQuestion: baseQuestion,
              pendingPlanProgressLabel: '1/2',
              shouldShowPlanChoices: true,
            ),
          ),
        ),
      ),
    );
    await tester.pump();
    await tester.pump();

    final scrollableState =
        tester.state<ScrollableState>(find.byType(Scrollable));
    final initialMaxScrollExtent = scrollableState.position.maxScrollExtent;
    expect(scrollableState.position.pixels, initialMaxScrollExtent);

    await tester.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: SizedBox(
            height: 320,
            child: ChatTimeline(
              sessionId: 'plan-grow-session',
              bottomPadding: 24,
              items: items,
              pendingPlanQuestion: expandedQuestion,
              pendingPlanProgressLabel: '1/2',
              shouldShowPlanChoices: true,
            ),
          ),
        ),
      ),
    );
    await tester.pump();
    await tester.pump();

    expect(
      scrollableState.position.maxScrollExtent,
      greaterThan(initialMaxScrollExtent),
    );
    expect(
      scrollableState.position.pixels,
      scrollableState.position.maxScrollExtent,
    );
  });

  testWidgets('底部 prompt 按钮变多时继续贴住底部', (tester) async {
    final items = List<TimelineItem>.generate(
      18,
      (index) => TimelineItem(
        id: 'prompt-grow-$index',
        kind: 'markdown',
        timestamp: DateTime(2026, 1, 1, 0, index),
        body: 'Prompt 前历史消息 $index',
        animateBody: false,
      ),
    );
    final basePrompt = PromptRequestEvent(
      timestamp: DateTime(2026, 1, 1, 0, 30),
      sessionId: 'prompt-grow-session',
      runtimeMeta: const RuntimeMeta(command: 'codex'),
      raw: const {'type': 'prompt_request'},
      message: '请选择继续方式',
      options: const [
        PromptOption(value: 'continue', label: '继续'),
      ],
    );
    final expandedPrompt = PromptRequestEvent(
      timestamp: basePrompt.timestamp,
      sessionId: basePrompt.sessionId,
      runtimeMeta: basePrompt.runtimeMeta,
      raw: basePrompt.raw,
      message: basePrompt.message,
      options: List<PromptOption>.generate(
        14,
        (index) => PromptOption(value: 'choice-$index', label: '选项 $index'),
      ),
    );

    await tester.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: SizedBox(
            height: 320,
            child: ChatTimeline(
              sessionId: 'prompt-grow-session',
              bottomPadding: 24,
              items: items,
              pendingPrompt: basePrompt,
            ),
          ),
        ),
      ),
    );
    await tester.pump();
    await tester.pump();

    final scrollableState =
        tester.state<ScrollableState>(find.byType(Scrollable));
    final initialMaxScrollExtent = scrollableState.position.maxScrollExtent;
    expect(scrollableState.position.pixels, initialMaxScrollExtent);

    await tester.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: SizedBox(
            height: 320,
            child: ChatTimeline(
              sessionId: 'prompt-grow-session',
              bottomPadding: 24,
              items: items,
              pendingPrompt: expandedPrompt,
            ),
          ),
        ),
      ),
    );
    await tester.pump();
    await tester.pump();

    expect(
      scrollableState.position.maxScrollExtent,
      greaterThan(initialMaxScrollExtent),
    );
    expect(
      scrollableState.position.pixels,
      scrollableState.position.maxScrollExtent,
    );
  });
}
