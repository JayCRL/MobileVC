import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:mobile_vc/features/chat/command_input_bar.dart';

void main() {
  group('CommandInputBar', () {
    testWidgets('permission 场景下禁用输入框并提示先确认授权', (tester) async {
      String? submitted;
      await tester.pumpWidget(
        _buildTestApp(
          shouldShowPermissionChoices: true,
          onSubmit: (value) => submitted = value,
        ),
      );

      final field = tester.widget<TextField>(find.byType(TextField));
      final button = tester.widget<FilledButton>(find.byType(FilledButton));

      expect(field.enabled, isFalse);
      expect(field.readOnly, isTrue);
      expect(field.canRequestFocus, isFalse);
      expect(field.decoration?.hintText, '请先在上方确认授权');
      expect(button.onPressed, isNull);

      expect(submitted, isNull);
    });

    testWidgets('review 场景下禁用输入框并提示先完成审核', (tester) async {
      await tester.pumpWidget(
        _buildTestApp(
          shouldShowReviewChoices: true,
        ),
      );

      final field = tester.widget<TextField>(find.byType(TextField));
      final button = tester.widget<FilledButton>(find.byType(FilledButton));

      expect(field.enabled, isFalse);
      expect(field.readOnly, isTrue);
      expect(field.canRequestFocus, isFalse);
      expect(field.decoration?.hintText, '请先在上方完成审核');
      expect(button.onPressed, isNull);
    });

    testWidgets('普通场景下仍可输入并发送', (tester) async {
      String? submitted;
      await tester.pumpWidget(
        _buildTestApp(
          onSubmit: (value) => submitted = value,
        ),
      );

      final field = tester.widget<TextField>(find.byType(TextField));
      final button = tester.widget<FilledButton>(find.byType(FilledButton));

      expect(field.enabled, isTrue);
      expect(button.onPressed, isNotNull);

      await tester.enterText(find.byType(TextField), 'hello');
      await tester.tap(find.byType(FilledButton));
      await tester.pump();

      expect(submitted, 'hello');
    });

    testWidgets('Claude 模式显示 Claude 状态与 hint', (tester) async {
      await tester.pumpWidget(
        _buildTestApp(
          awaitInput: true,
          showClaudeMode: true,
        ),
      );

      expect(find.text('Claude'), findsOneWidget);
      final field = tester.widget<TextField>(find.byType(TextField));
      expect(field.decoration?.hintText, '继续回复 Claude');
    });

    testWidgets('shell 模式显示 Shell 状态与 hint', (tester) async {
      await tester.pumpWidget(
        _buildTestApp(
          isBusy: true,
          showClaudeMode: false,
        ),
      );

      expect(find.text('Shell'), findsOneWidget);
      final field = tester.widget<TextField>(find.byType(TextField));
      expect(field.decoration?.hintText, '当前 shell 会话仍在运行');
    });

    testWidgets('loading 期间显示会话切换中 hint 并禁用输入', (tester) async {
      await tester.pumpWidget(
        _buildTestApp(
          isSessionLoading: true,
        ),
      );

      final field = tester.widget<TextField>(find.byType(TextField));
      final button = tester.widget<FilledButton>(find.byType(FilledButton));
      expect(field.enabled, isFalse);
      expect(field.decoration?.hintText, '会话切换中...');
      expect(button.onPressed, isNull);
    });
  });
}

Widget _buildTestApp({
  bool shouldShowPermissionChoices = false,
  bool shouldShowReviewChoices = false,
  bool awaitInput = false,
  bool isBusy = false,
  bool showClaudeMode = true,
  bool isSessionLoading = false,
  ValueChanged<String>? onSubmit,
}) {
  return MaterialApp(
    home: Scaffold(
      bottomNavigationBar: CommandInputBar(
        awaitInput: awaitInput,
        isBusy: isBusy,
        hasPendingReview: false,
        fastMode: false,
        permissionMode: 'default',
        shouldShowPermissionChoices: shouldShowPermissionChoices,
        shouldShowReviewChoices: shouldShowReviewChoices,
        onSubmit: onSubmit ?? (_) {},
        onOpenSessions: () {},
        onOpenRuntimeInfo: () {},
        onOpenLogs: () {},
        onOpenSkills: () {},
        onOpenMemory: () {},
        onPermissionModeChanged: (_) {},
        showClaudeMode: showClaudeMode,
        isSessionLoading: isSessionLoading,
      ),
    ),
  );
}
