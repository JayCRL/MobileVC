import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:mobile_vc/data/models/events.dart';
import 'package:mobile_vc/data/models/runtime_meta.dart';
import 'package:mobile_vc/data/models/session_models.dart';
import 'package:mobile_vc/features/files/file_viewer_sheet.dart';

void main() {
  group('FileViewerSheet', () {
    testWidgets('permission 场景下文件输入框禁用并提示先确认授权', (tester) async {
      await tester.pumpWidget(
        _buildTestApp(
          pendingInteraction: _permissionInteraction(),
          shouldShowPermissionChoices: true,
        ),
      );

      final field = tester.widget<TextField>(
        find.byKey(const ValueKey('fileViewer.input')),
      );
      final sendButton = tester.widget<IconButton>(
        find.byKey(const ValueKey('fileViewer.sendButton')),
      );

      expect(field.enabled, isFalse);
      expect(field.readOnly, isTrue);
      expect(field.canRequestFocus, isFalse);
      expect(field.decoration?.hintText, '请先在上方确认授权');
      expect(sendButton.onPressed, isNull);
    });

    testWidgets('review 场景下文件输入框禁用并提示先完成审核', (tester) async {
      await tester.pumpWidget(
        _buildTestApp(
          showReviewActions: true,
          shouldShowReviewChoices: true,
          reviewDiff: _reviewDiffs.first,
          pendingDiffs: [_reviewDiffs.first],
          reviewGroups: const [],
          activeReviewDiffId: 'diff-1',
        ),
      );

      final field = tester.widget<TextField>(
        find.byKey(const ValueKey('fileViewer.input')),
      );
      final sendButton = tester.widget<IconButton>(
        find.byKey(const ValueKey('fileViewer.sendButton')),
      );

      expect(field.enabled, isFalse);
      expect(field.readOnly, isTrue);
      expect(field.canRequestFocus, isFalse);
      expect(field.decoration?.hintText, '请先在上方完成审核');
      expect(sendButton.onPressed, isNull);
    });

    testWidgets('仅有 pendingInteraction permission 时也显示权限按钮', (tester) async {
      String? submitted;
      await tester.pumpWidget(
        _buildTestApp(
          pendingInteraction: _permissionInteraction(),
          shouldShowPermissionChoices: true,
          onSubmitPrompt: (value) => submitted = value,
        ),
      );

      expect(find.byKey(const ValueKey('fileViewer.permissionBar')),
          findsOneWidget);
      expect(find.text('y'), findsOneWidget);
      expect(find.text('n'), findsOneWidget);

      await tester.tap(find.text('y'));
      await tester.pump();

      expect(submitted, 'y');
    });

    testWidgets('权限按钮点击后只透传权限值，不附带额外文本', (tester) async {
      String? submitted;
      String? filePrompt;
      await tester.pumpWidget(
        _buildTestApp(
          pendingInteraction: _permissionInteraction(),
          shouldShowPermissionChoices: true,
          onSubmitPrompt: (value) => submitted = value,
          onSendFilePrompt: (value) => filePrompt = value,
        ),
      );

      await tester.tap(find.text('n'));
      await tester.pump();

      expect(submitted, 'n');
      expect(filePrompt, isNull);
    });

    testWidgets('runtime_phase permission_blocked + prompt-only 权限场景会显示权限栏', (tester) async {
      await tester.pumpWidget(
        _buildTestApp(
          pendingPrompt: _permissionPrompt(),
          shouldShowPermissionChoices: true,
        ),
      );

      expect(find.byKey(const ValueKey('fileViewer.permissionBar')),
          findsOneWidget);
      expect(find.text('Claude requested permissions to write to README.md'),
          findsOneWidget);
    });

    testWidgets('review 场景优先显示 review 操作，不被权限栏覆盖', (tester) async {
      tester.view.physicalSize = const Size(1200, 2200);
      tester.view.devicePixelRatio = 1.0;
      addTearDown(tester.view.resetPhysicalSize);
      addTearDown(tester.view.resetDevicePixelRatio);

      await tester.pumpWidget(
        _buildTestApp(
          showReviewActions: true,
          pendingInteraction: _permissionInteraction(),
          reviewDiff: _reviewDiffs.first,
          pendingDiffs: [_reviewDiffs.first],
          reviewGroups: const [],
          activeReviewGroupId: '',
          activeReviewDiffId: 'diff-1',
          shouldShowReviewChoices: true,
        ),
      );

      expect(find.text('同意'), findsOneWidget);
      expect(
          find.byKey(const ValueKey('fileViewer.permissionBar')), findsNothing);
    });

    testWidgets('权限 prompt 没有 options 时仍显示允许和拒绝按钮', (tester) async {
      String? submitted;
      await tester.pumpWidget(
        _buildTestApp(
          pendingPrompt: _permissionPrompt(options: const []),
          shouldShowPermissionChoices: true,
          onSubmitPrompt: (value) => submitted = value,
        ),
      );

      expect(find.text('允许'), findsOneWidget);
      expect(find.text('拒绝'), findsOneWidget);

      await tester.tap(find.text('允许'));
      await tester.pump();

      expect(submitted, 'y');
    });

    testWidgets('pendingInteraction permission 下输入框锁定且发送按钮禁用', (tester) async {
      String? filePrompt;
      String? submittedPrompt;
      await tester.pumpWidget(
        _buildTestApp(
          pendingInteraction: _permissionInteraction(),
          shouldShowPermissionChoices: true,
          onSendFilePrompt: (value) => filePrompt = value,
          onSubmitPrompt: (value) => submittedPrompt = value,
        ),
      );

      final field = tester.widget<TextField>(find.byType(TextField));
      final sendButton = tester.widget<IconButton>(
        find.byKey(const ValueKey('fileViewer.sendButton')),
      );

      expect(field.enabled, isFalse);
      expect(field.readOnly, isTrue);
      expect(field.canRequestFocus, isFalse);
      expect(sendButton.onPressed, isNull);
      expect(submittedPrompt, isNull);
      expect(filePrompt, isNull);
    });

    testWidgets('普通文件 prompt 仍走文件上下文发送', (tester) async {
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

    testWidgets('普通 file-context prompt 不显示权限栏', (tester) async {
      await tester.pumpWidget(
        _buildTestApp(
          pendingPrompt: PromptRequestEvent(
            timestamp: DateTime(2026),
            sessionId: 'session-1',
            runtimeMeta: const RuntimeMeta(),
            raw: const {
              'type': 'prompt_request',
              'msg': '请选择输出格式',
            },
            message: '请选择输出格式',
            options: const [
              PromptOption(value: 'markdown'),
              PromptOption(value: 'json'),
            ],
          ),
        ),
      );

      expect(find.byKey(const ValueKey('fileViewer.permissionBar')), findsNothing);
      expect(find.text('请选择输出格式'), findsOneWidget);
    });

    testWidgets('权限 prompt 下输入框锁定且发送按钮禁用', (tester) async {
      String? filePrompt;
      String? submittedPrompt;
      await tester.pumpWidget(
        _buildTestApp(
          pendingPrompt: _permissionPrompt(),
          shouldShowPermissionChoices: true,
          onSendFilePrompt: (value) => filePrompt = value,
          onSubmitPrompt: (value) => submittedPrompt = value,
        ),
      );

      final field = tester.widget<TextField>(find.byType(TextField));
      final sendButton = tester.widget<IconButton>(
        find.byKey(const ValueKey('fileViewer.sendButton')),
      );

      expect(field.enabled, isFalse);
      expect(field.readOnly, isTrue);
      expect(field.canRequestFocus, isFalse);
      expect(sendButton.onPressed, isNull);
      expect(submittedPrompt, isNull);
      expect(filePrompt, isNull);
    });

    testWidgets('修改组与组内文件切换会走联动回调', (tester) async {
      String? selectedGroupId;
      String? selectedDiffId;
      var openedDiffList = false;
      await tester.pumpWidget(
        _buildTestApp(
          showReviewActions: true,
          isDiffMode: true,
          reviewDiff: _reviewDiffs.first,
          pendingDiffs: _reviewDiffs,
          reviewGroups: _reviewGroups,
          activeReviewGroupId: 'group-1',
          activeReviewDiffId: 'diff-1',
          onSelectReviewGroup: (value) => selectedGroupId = value,
          onSelectReviewDiff: (value) => selectedDiffId = value,
          onOpenDiffList: () => openedDiffList = true,
        ),
      );

      expect(find.text('组一'), findsWidgets);
      expect(find.text('组二'), findsWidgets);
      expect(find.text('进入 differ 逐个审核'), findsOneWidget);
      expect(find.text('已同意: 0'), findsOneWidget);
      expect(find.text('已撤销: 0'), findsOneWidget);
      expect(find.text('继续调整: 0'), findsOneWidget);

      await tester.tap(find.text('组二'));
      await tester.pump();
      expect(selectedGroupId, 'group-2');

      await tester.tap(find.text('2. test_b.dart'));
      await tester.pump();
      expect(selectedDiffId, 'diff-2');

      await tester.tap(find.text('进入 differ 逐个审核'));
      await tester.pump();
      expect(openedDiffList, isTrue);
    });
  });
}

Widget _buildTestApp({
  PromptRequestEvent? pendingPrompt,
  InteractionRequestEvent? pendingInteraction,
  ValueChanged<String>? onSendFilePrompt,
  ValueChanged<String>? onSubmitPrompt,
  bool showReviewActions = false,
  bool isDiffMode = false,
  bool shouldShowPermissionChoices = false,
  bool shouldShowReviewChoices = false,
  HistoryContext? reviewDiff,
  List<HistoryContext> pendingDiffs = const [],
  List<ReviewGroup> reviewGroups = const [],
  String activeReviewGroupId = '',
  String activeReviewDiffId = '',
  ValueChanged<String>? onSelectReviewGroup,
  ValueChanged<String>? onSelectReviewDiff,
  VoidCallback? onOpenDiffList,
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
        showReviewActions: showReviewActions,
        isDiffMode: isDiffMode,
        reviewDiff: reviewDiff,
        pendingDiffs: pendingDiffs,
        reviewGroups: reviewGroups,
        activeReviewGroupId: activeReviewGroupId,
        activeReviewDiffId: activeReviewDiffId,
        isAutoAcceptMode: false,
        shouldShowPermissionChoices: shouldShowPermissionChoices,
        shouldShowReviewChoices: shouldShowReviewChoices,
        shouldShowPlanChoices: false,
        pendingPrompt: pendingPrompt,
        pendingInteraction: pendingInteraction,
        onAccept: () {},
        onRevert: () {},
        onRevise: () {},
        onSelectReviewGroup: onSelectReviewGroup ?? (_) {},
        onSelectReviewDiff: onSelectReviewDiff ?? (_) {},
        onOpenDiffList: onOpenDiffList ?? () {},
        onUseAsContext: () {},
        onSendFilePrompt: onSendFilePrompt ?? (_) {},
        onSubmitPrompt: onSubmitPrompt ?? (_) {},
      ),
    ),
  );
}

const _reviewDiffs = [
  HistoryContext(
    id: 'diff-1',
    type: 'diff',
    path: '/workspace/test_a.dart',
    title: 'test_a.dart',
    diff: '@@ -1 +1 @@',
    lang: 'dart',
    pendingReview: true,
    groupId: 'group-1',
    groupTitle: '组一',
  ),
  HistoryContext(
    id: 'diff-2',
    type: 'diff',
    path: '/workspace/test_b.dart',
    title: 'test_b.dart',
    diff: '@@ -1 +1 @@',
    lang: 'dart',
    pendingReview: true,
    groupId: 'group-1',
    groupTitle: '组一',
  ),
  HistoryContext(
    id: 'diff-3',
    type: 'diff',
    path: '/workspace/test_c.dart',
    title: 'test_c.dart',
    diff: '@@ -1 +1 @@',
    lang: 'dart',
    pendingReview: true,
    groupId: 'group-2',
    groupTitle: '组二',
  ),
];

const _reviewGroups = [
  ReviewGroup(
    id: 'group-1',
    title: '组一',
    pendingReview: true,
    reviewStatus: 'pending',
    pendingCount: 2,
    files: [
      ReviewFile(
          id: 'diff-1', path: '/workspace/test_a.dart', title: 'test_a.dart'),
      ReviewFile(
          id: 'diff-2', path: '/workspace/test_b.dart', title: 'test_b.dart'),
    ],
  ),
  ReviewGroup(
    id: 'group-2',
    title: '组二',
    pendingReview: true,
    reviewStatus: 'pending',
    pendingCount: 1,
    files: [
      ReviewFile(
          id: 'diff-3', path: '/workspace/test_c.dart', title: 'test_c.dart'),
    ],
  ),
];

PromptRequestEvent _permissionPrompt(
    {List<PromptOption> options = const [
      PromptOption(value: 'y'),
      PromptOption(value: 'n')
    ]}) {
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

InteractionRequestEvent _permissionInteraction() {
  return InteractionRequestEvent(
    timestamp: DateTime(2026),
    sessionId: 'session-1',
    runtimeMeta: const RuntimeMeta(),
    raw: const {
      'type': 'interaction_request',
      'kind': 'permission',
      'title': 'Permission required',
      'message': 'Claude needs permission to write README.md',
    },
    kind: 'permission',
    title: 'Permission required',
    message: 'Claude needs permission to write README.md',
    options: const [PromptOption(value: 'y'), PromptOption(value: 'n')],
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
