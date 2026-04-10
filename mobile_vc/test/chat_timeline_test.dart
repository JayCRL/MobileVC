import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:mobile_vc/data/models/events.dart';
import 'package:mobile_vc/data/models/runtime_meta.dart';
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

  testWidgets('generic waiting prompt_request 不显示额外卡片', (tester) async {
    await tester.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: ChatTimeline(
            items: const [],
            pendingPrompt: PromptRequestEvent(
              timestamp: DateTime(2026),
              sessionId: 'session-1',
              runtimeMeta: const RuntimeMeta(command: 'claude'),
              raw: const {
                'type': 'prompt_request',
                'msg': '等待输入',
              },
              message: '等待输入',
            ),
          ),
        ),
      ),
    );

    expect(find.text('等待输入'), findsNothing);
    expect(find.text('授权确认'), findsNothing);
  });
}
