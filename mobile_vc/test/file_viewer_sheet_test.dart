import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:mobile_vc/data/models/events.dart';
import 'package:mobile_vc/data/models/runtime_meta.dart';
import 'package:mobile_vc/data/models/session_models.dart';
import 'package:mobile_vc/features/files/file_viewer_sheet.dart';

void main() {
  group('FileViewerSheet', () {
    testWidgets('权限 prompt 没有 options 时仍显示允许和拒绝按钮', (tester) async {
      String? submitted;
      await tester.pumpWidget(
        _buildTestApp(
          pendingPrompt: _permissionPrompt(options: const []),
          onSubmitPrompt: (value) => submitted = value,
        ),
      );

      expect(find.text('允许'), findsOneWidget);
      expect(find.text('拒绝'), findsOneWidget);

      await tester.tap(find.text('允许'));
      await tester.pump();

      expect(submitted, 'y');
    });

    testWidgets('普通会话 prompt 下输入继续走文件上下文发送', (tester) async {
      String? filePrompt;
      String? submittedPrompt;
      await tester.pumpWidget(
        _buildTestApp(
          pendingPrompt: _readyPrompt(),
          onSendFilePrompt: (value) => filePrompt = value,
          onSubmitPrompt: (value) => submittedPrompt = value,
        ),
      );

      await tester.enterText(find.byType(TextField), '把第一行后面加 111');
      await tester.tap(find.byIcon(Icons.send));
      await tester.pump();

      expect(filePrompt, '把第一行后面加 111');
      expect(submittedPrompt, isNull);
    });

    testWidgets('权限 prompt 下输入走授权提交', (tester) async {
      String? filePrompt;
      String? submittedPrompt;
      await tester.pumpWidget(
        _buildTestApp(
          pendingPrompt: _permissionPrompt(),
          onSendFilePrompt: (value) => filePrompt = value,
          onSubmitPrompt: (value) => submittedPrompt = value,
        ),
      );

      await tester.enterText(find.byType(TextField), 'y');
      await tester.tap(find.byIcon(Icons.send));
      await tester.pump();

      expect(submittedPrompt, 'y');
      expect(filePrompt, isNull);
    });
  });
}

Widget _buildTestApp({
  PromptRequestEvent? pendingPrompt,
  ValueChanged<String>? onSendFilePrompt,
  ValueChanged<String>? onSubmitPrompt,
}) {
  return MaterialApp(
    home: Scaffold(
      body: FileViewerSheet(
        file: const FileReadResult(
          path: '/Users/wust_lh/MobileVC/README.md',
          content: '# MobileVC\n',
          lang: 'markdown',
          isText: true,
          size: 11,
          encoding: 'utf-8',
        ),
        loading: false,
        showReviewActions: false,
        isDiffMode: false,
        reviewDiff: null,
        pendingDiffs: const [],
        reviewGroups: const [],
        activeReviewGroupId: '',
        activeReviewDiffId: '',
        isAutoAcceptMode: false,
        shouldShowReviewChoices: false,
        pendingPrompt: pendingPrompt,
        onAccept: () {},
        onRevert: () {},
        onRevise: () {},
        onSelectReviewGroup: (_) {},
        onSelectReviewDiff: (_) {},
        onOpenDiffList: () {},
        onUseAsContext: () {},
        onSendFilePrompt: onSendFilePrompt ?? (_) {},
        onSubmitPrompt: onSubmitPrompt ?? (_) {},
      ),
    ),
  );
}

PromptRequestEvent _permissionPrompt({List<PromptOption> options = const [PromptOption(value: 'y'), PromptOption(value: 'n')]}) {
  return PromptRequestEvent(
    timestamp: DateTime(2026),
    sessionId: 'session-1',
    runtimeMeta: const RuntimeMeta(),
    raw: const {
      'type': 'prompt_request',
      'msg': 'Claude requested permissions to write to README.md',
    },
    message: 'Claude requested permissions to write to README.md',
    options: options,
  );
}

PromptRequestEvent _readyPrompt() {
  return PromptRequestEvent(
    timestamp: DateTime(2026),
    sessionId: 'session-1',
    runtimeMeta: const RuntimeMeta(),
    raw: const {
      'type': 'prompt_request',
      'msg': 'Claude 会话已就绪，可继续输入',
    },
    message: 'Claude 会话已就绪，可继续输入',
    options: const [],
  );
}
