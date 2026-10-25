import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:mobile_vc/features/session/context_window_usage_sheet.dart';
import 'package:mobile_vc/data/models/session_models.dart';

void main() {
  testWidgets('renders concrete usage details', (tester) async {
    await tester.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: ContextWindowUsageSheet(
            engineLabel: 'Codex',
            usage: const ContextWindowUsage(
              tokensUsed: 120000,
              tokenLimit: 200000,
            ),
          ),
        ),
      ),
    );

    expect(find.text('上下文窗口'), findsOneWidget);
    expect(find.text('60% 已使用'), findsOneWidget);
    expect(find.text('120K'), findsOneWidget);
    expect(find.text('200K'), findsOneWidget);
    expect(find.text('80K 剩余'), findsOneWidget);
    expect(find.text('当前上下文空间充足，数值会随着运行时 token usage 实时更新。'), findsOneWidget);
  });

  testWidgets('renders empty state when usage is unavailable', (tester) async {
    await tester.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: ContextWindowUsageSheet(
            engineLabel: 'Claude',
            usage: const ContextWindowUsage(),
          ),
        ),
      ),
    );

    expect(find.text('等待运行时返回真实 token usage。收到后这里会实时更新。'), findsOneWidget);
  });
}
