import 'package:flutter/material.dart';
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
}
