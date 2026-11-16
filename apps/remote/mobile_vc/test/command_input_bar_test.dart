import 'dart:typed_data';

import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:mobile_vc/data/models/events.dart';
import 'package:mobile_vc/data/models/session_models.dart';
import 'package:mobile_vc/features/chat/chat_timeline.dart';
import 'package:mobile_vc/features/chat/command_input_bar.dart';

void main() {
  group('CommandInputBar', () {
    testWidgets('permission 场景下禁用输入框并提示先确认授权', (tester) async {
      String? submitted;
      await tester.pumpWidget(
        _buildTestApp(
          shouldShowPermissionChoices: true,
          onSubmit: (value, _) => submitted = value,
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
          onSubmit: (value, _) => submitted = value,
        ),
      );

      final field = tester.widget<TextField>(find.byType(TextField));
      final button = tester.widget<FilledButton>(find.byType(FilledButton));

      expect(field.enabled, isTrue);
      expect(field.autocorrect, isTrue);
      expect(field.enableSuggestions, isTrue);
      expect(field.obscureText, isFalse);
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
          currentEngine: 'claude',
        ),
      );

      final field = tester.widget<TextField>(find.byType(TextField));
      expect(field.decoration?.hintText, '回复 Claude');
    });

    testWidgets('Claude 模式显示 Claude 权限菜单', (tester) async {
      String? changedMode;
      await tester.pumpWidget(
        _buildTestApp(
          showClaudeMode: true,
          currentEngine: 'claude',
          onPermissionModeChanged: (value) => changedMode = value,
        ),
      );

      await tester
          .tap(find.byKey(const ValueKey('permission-mode-icon-button')));
      await tester.pumpAndSettle();

      expect(find.text('默认权限'), findsOneWidget);
      expect(find.text('自动模式'), findsOneWidget);
      expect(find.text('完全访问权限'), findsOneWidget);
      expect(find.text('跳过审批'), findsNothing);
      expect(find.text('自动审查'), findsNothing);
      expect(find.text('自定义(config.toml)'), findsNothing);

      await tester.tap(find.text('完全访问权限').last);
      await tester.pumpAndSettle();
      expect(changedMode, 'bypassPermissions');
    });

    testWidgets('Codex 模式显示图标权限菜单和目标工具按钮', (tester) async {
      var targetMode = false;
      await tester.pumpWidget(
        _buildTestApp(
          showClaudeMode: true,
          currentEngine: 'codex',
          configuredEngine: 'codex',
          codexTargetMode: targetMode,
          onCodexTargetModeChanged: (value) => targetMode = value,
        ),
      );

      expect(find.text('请求目标'), findsNothing);
      expect(find.text('目标'), findsOneWidget);
      expect(find.text('完全访问权限'), findsNothing);
      expect(find.text('跳过审批'), findsNothing);
      expect(
        find.byKey(const ValueKey('codex-target-tool-chip')),
        findsOneWidget,
      );
      expect(find.byKey(const ValueKey('permission-mode-icon-button')),
          findsOneWidget);
      expect(
        find.descendant(
          of: find.byKey(const ValueKey('permission-mode-icon-button')),
          matching: find.byIcon(Icons.rule_rounded),
        ),
        findsOneWidget,
      );
      expect(find.byIcon(Icons.lock_outline_rounded), findsNothing);
      await tester
          .tap(find.byKey(const ValueKey('permission-mode-icon-button')));
      await tester.pumpAndSettle();

      expect(find.text('默认权限'), findsOneWidget);
      expect(find.text('自动审查'), findsOneWidget);
      expect(find.text('跳过审批'), findsOneWidget);
      expect(find.text('完全访问权限'), findsNothing);
      expect(find.text('自定义(config.toml)'), findsOneWidget);
      expect(find.byIcon(Icons.bolt_rounded), findsOneWidget);

      await tester.tap(find.text('自动审查'));
      await tester.pumpAndSettle();
      await tester.tap(find.byKey(const ValueKey('codex-target-tool-chip')));
      await tester.pump();
      expect(targetMode, isTrue);
    });

    testWidgets('Codex 窄屏工具栏不会裁切目标工具按钮', (tester) async {
      tester.view.devicePixelRatio = 1;
      tester.view.physicalSize = const Size(360, 780);
      addTearDown(tester.view.resetPhysicalSize);
      addTearDown(tester.view.resetDevicePixelRatio);

      await tester.pumpWidget(
        _buildTestApp(
          showClaudeMode: true,
          currentEngine: 'codex',
          configuredEngine: 'codex',
        ),
      );
      await tester.pump();

      expect(tester.takeException(), isNull);
      final barRect = tester.getRect(find.byType(CommandInputBar));
      final targetRect = tester.getRect(
        find.byKey(const ValueKey('codex-target-tool-chip')),
      );
      expect(targetRect.left, greaterThanOrEqualTo(barRect.left));
      expect(targetRect.right, lessThanOrEqualTo(barRect.right));
    });

    testWidgets('canStop 为 true 时优先显示停止按钮', (tester) async {
      var submitted = false;
      var stopped = false;
      await tester.pumpWidget(
        _buildTestApp(
          awaitInput: true,
          isBusy: true,
          showClaudeMode: true,
          currentEngine: 'claude',
          canStop: true,
          onSubmit: (text, images) => submitted = true,
          onStop: () => stopped = true,
        ),
      );

      expect(
          find.descendant(
            of: find.byType(FilledButton),
            matching: find.byIcon(Icons.stop_rounded),
          ),
          findsOneWidget);
      expect(find.byIcon(Icons.arrow_upward), findsNothing);

      await tester.tap(find.byType(FilledButton));
      await tester.pump();

      expect(submitted, isFalse);
      expect(stopped, isTrue);
    });

    testWidgets('选择图片后即使 canStop 为 true 也优先显示发送按钮', (tester) async {
      String? submittedText;
      List<ChatImageAttachment> submittedImages = const [];
      var stopped = false;
      await tester.pumpWidget(
        _buildTestApp(
          awaitInput: true,
          isBusy: true,
          canStop: true,
          currentEngine: 'codex',
          onAttachImage: () async => ChatImageAttachment(
            name: 'screen.png',
            mimeType: 'image/png',
            bytes: _transparentPngBytes,
          ),
          onSubmit: (text, images) {
            submittedText = text;
            submittedImages = images;
          },
          onStop: () => stopped = true,
        ),
      );

      expect(find.byIcon(Icons.stop_rounded), findsOneWidget);

      await tester.tap(find.byTooltip('添加图片'));
      await tester.pump();

      expect(find.byIcon(Icons.stop_rounded), findsNothing);
      expect(find.byIcon(Icons.arrow_upward), findsOneWidget);

      await tester.tap(find.byType(FilledButton));
      await tester.pump();

      expect(stopped, isFalse);
      expect(submittedText, '');
      expect(submittedImages, hasLength(1));
      expect(find.byKey(const ValueKey('imageAttachmentPreview:screen.png')),
          findsNothing);
    });

    testWidgets('运行中不可提交草稿时选择图片仍保留停止按钮', (tester) async {
      var submitted = false;
      var stopped = false;
      await tester.pumpWidget(
        _buildTestApp(
          isBusy: true,
          canStop: true,
          currentEngine: 'codex',
          onAttachImage: () async => ChatImageAttachment(
            name: 'screen.png',
            mimeType: 'image/png',
            bytes: _transparentPngBytes,
          ),
          onSubmit: (_, __) => submitted = true,
          onStop: () => stopped = true,
        ),
      );

      await tester.tap(find.byTooltip('添加图片'));
      await tester.pump();

      expect(find.byIcon(Icons.stop_rounded), findsOneWidget);
      expect(find.byIcon(Icons.arrow_upward), findsNothing);

      await tester.tap(find.byType(FilledButton));
      await tester.pump();

      expect(stopped, isTrue);
      expect(submitted, isFalse);
    });

    testWidgets('选择图片后输入文字不会重建图片预览', (tester) async {
      await tester.pumpWidget(
        _buildTestApp(
          awaitInput: true,
          isBusy: true,
          canStop: true,
          currentEngine: 'codex',
          onAttachImage: () async => ChatImageAttachment(
            name: 'screen.png',
            mimeType: 'image/png',
            bytes: _transparentPngBytes,
          ),
        ),
      );

      await tester.tap(find.byTooltip('添加图片'));
      await tester.pump();

      final previewFinder =
          find.byKey(const ValueKey('imageAttachmentPreview:screen.png'));
      expect(previewFinder, findsOneWidget);
      final previewBefore = tester.widget(previewFinder);

      await tester.enterText(find.byType(TextField), '看下这张图');
      await tester.pump();

      expect(previewFinder, findsOneWidget);
      expect(identical(tester.widget(previewFinder), previewBefore), isTrue);
      expect(find.byIcon(Icons.arrow_upward), findsOneWidget);
    });

    testWidgets('等待输入时纯文字草稿显示发送按钮并可提交', (tester) async {
      String? submitted;
      await tester.pumpWidget(
        _buildTestApp(
          awaitInput: true,
          isBusy: true,
          canStop: true,
          currentEngine: 'codex',
          onSubmit: (text, _) => submitted = text,
        ),
      );

      expect(find.byIcon(Icons.stop_rounded), findsOneWidget);

      await tester.enterText(find.byType(TextField), '只输入文字');
      await tester.pump();

      expect(find.byIcon(Icons.arrow_upward), findsOneWidget);
      expect(find.byIcon(Icons.stop_rounded), findsNothing);

      await tester.tap(find.byType(FilledButton));
      await tester.pump();

      expect(submitted, '只输入文字');
      expect(find.byIcon(Icons.stop_rounded), findsOneWidget);
    });

    testWidgets('纯文字输入不会重建同页时间线兄弟节点', (tester) async {
      var timelineBuilds = 0;
      await tester.pumpWidget(
        _buildTestApp(
          body: _BuildCounter(
            onBuild: () => timelineBuilds++,
            child: ChatTimeline(
              sessionId: 'input-session',
              items: [
                TimelineItem(
                  id: 'item-1',
                  kind: 'markdown',
                  timestamp: DateTime(2026, 1, 1),
                  body: '历史消息',
                ),
              ],
            ),
          ),
        ),
      );

      expect(find.byType(ChatTimeline), findsOneWidget);
      final buildsBeforeInput = timelineBuilds;

      await tester.enterText(find.byType(TextField), '只输入文字');
      await tester.pump();

      expect(timelineBuilds, buildsBeforeInput);
      expect(find.byIcon(Icons.arrow_upward), findsOneWidget);
    });

    testWidgets('输入栏不使用键盘期间高成本 BackdropFilter', (tester) async {
      await tester.pumpWidget(_buildTestApp());

      expect(find.byType(BackdropFilter), findsNothing);
    });

    testWidgets('输入框右侧图片和发送按钮保持紧凑尺寸', (tester) async {
      await tester.pumpWidget(_buildTestApp());

      expect(
        tester
            .getSize(find.byKey(const ValueKey('command-image-action-button'))),
        const Size.square(36),
      );
      expect(
        tester
            .getSize(find.byKey(const ValueKey('command-send-action-button'))),
        const Size.square(36),
      );
    });

    testWidgets('键盘 inset 不会额外撑高输入栏底部 padding', (tester) async {
      await tester.pumpWidget(
        _buildTestApp(
          viewInsets: const EdgeInsets.only(bottom: 320),
        ),
      );

      final paddings = tester
          .widgetList<Padding>(find.byType(Padding))
          .map((padding) => padding.padding)
          .toList();
      expect(paddings, contains(const EdgeInsets.fromLTRB(10, 6, 10, 10)));
      expect(paddings, isNot(contains(const EdgeInsets.only(bottom: 320))));
    });

    testWidgets('Codex 模式显示 Codex 状态与 hint', (tester) async {
      await tester.pumpWidget(
        _buildTestApp(
          awaitInput: true,
          showClaudeMode: true,
          currentEngine: 'codex',
        ),
      );

      final field = tester.widget<TextField>(find.byType(TextField));
      expect(field.decoration?.hintText, '回复 Codex');
    });

    testWidgets('shell 运行中仍可继续编辑草稿', (tester) async {
      await tester.pumpWidget(
        _buildTestApp(
          isBusy: true,
          showClaudeMode: false,
          currentEngine: 'shell',
        ),
      );

      final field = tester.widget<TextField>(find.byType(TextField));
      expect(field.enabled, isTrue);
      expect(field.readOnly, isFalse);
      expect(field.canRequestFocus, isTrue);
      expect(field.decoration?.hintText, 'Shell 运行中');
    });

    testWidgets('busy 状态抖动不会打断正在输入的草稿', (tester) async {
      await tester.pumpWidget(
        _buildTestApp(
          awaitInput: true,
          isBusy: true,
          canStop: true,
          currentEngine: 'codex',
        ),
      );

      await tester.enterText(find.byType(TextField), '测试输入');
      await tester.pump();

      await tester.pumpWidget(
        _buildTestApp(
          isBusy: true,
          canStop: false,
          currentEngine: 'codex',
        ),
      );
      await tester.pump();

      final field = tester.widget<TextField>(find.byType(TextField));
      expect(field.enabled, isTrue);
      expect(field.readOnly, isFalse);
      expect(field.canRequestFocus, isTrue);
      expect(find.text('测试输入'), findsOneWidget);
    });

    testWidgets('busy 且非等待输入时发送按钮切为停止按钮', (tester) async {
      var stopped = false;
      await tester.pumpWidget(
        _buildTestApp(
          isBusy: true,
          canStop: true,
          showClaudeMode: true,
          currentEngine: 'codex',
          onStop: () => stopped = true,
        ),
      );

      expect(find.byIcon(Icons.stop_rounded), findsOneWidget);

      await tester.tap(find.byType(FilledButton));
      await tester.pump();

      expect(stopped, isTrue);
    });

    testWidgets('权限提示锁住输入时仍允许停止运行任务', (tester) async {
      var stopped = false;
      await tester.pumpWidget(
        _buildTestApp(
          shouldShowPermissionChoices: true,
          isBusy: true,
          canStop: true,
          onStop: () => stopped = true,
        ),
      );

      final field = tester.widget<TextField>(find.byType(TextField));
      final button = tester.widget<FilledButton>(find.byType(FilledButton));

      expect(field.enabled, isFalse);
      expect(find.byIcon(Icons.stop_rounded), findsOneWidget);
      expect(button.onPressed, isNotNull);

      await tester.tap(find.byType(FilledButton));
      await tester.pump();

      expect(stopped, isTrue);
    });

    testWidgets('选择图片后先显示预览，发送时连同用户文本一起提交', (tester) async {
      String? submittedText;
      List<ChatImageAttachment> submittedImages = const [];
      var pickCount = 0;
      await tester.pumpWidget(
        _buildTestApp(
          onAttachImage: () async {
            pickCount++;
            return ChatImageAttachment(
              name: 'screen.png',
              mimeType: 'image/png',
              bytes: _transparentPngBytes,
            );
          },
          onSubmit: (value, images) {
            submittedText = value;
            submittedImages = images;
          },
        ),
      );

      await tester.tap(find.byTooltip('添加图片'));
      await tester.pump();

      expect(pickCount, 1);
      expect(find.byKey(const ValueKey('imageAttachmentPreview:screen.png')),
          findsOneWidget);
      expect(submittedText, isNull);

      await tester.enterText(find.byType(TextField), '这张图哪里有问题？');
      await tester.tap(find.byType(FilledButton));
      await tester.pump();

      expect(submittedText, '这张图哪里有问题？');
      expect(submittedImages, hasLength(1));
      expect(submittedImages.single.name, 'screen.png');
      expect(find.byKey(const ValueKey('imageAttachmentPreview:screen.png')),
          findsNothing);
    });

    testWidgets('图片预览可以在发送前移除', (tester) async {
      List<ChatImageAttachment>? submittedImages;
      await tester.pumpWidget(
        _buildTestApp(
          onAttachImage: () async => ChatImageAttachment(
            name: 'screen.png',
            mimeType: 'image/png',
            bytes: _transparentPngBytes,
          ),
          onSubmit: (_, images) => submittedImages = images,
        ),
      );

      await tester.tap(find.byTooltip('添加图片'));
      await tester.pump();
      expect(find.byKey(const ValueKey('imageAttachmentPreview:screen.png')),
          findsOneWidget);

      await tester.tap(find.byTooltip('移除图片'));
      await tester.pump();
      expect(find.byKey(const ValueKey('imageAttachmentPreview:screen.png')),
          findsNothing);

      await tester.enterText(find.byType(TextField), '只发送文字');
      await tester.tap(find.byType(FilledButton));
      await tester.pump();
      expect(submittedImages, isEmpty);
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

    testWidgets('底部工具栏常驻显示上下文圆形入口', (tester) async {
      await tester.pumpWidget(
        _buildTestApp(
          contextWindowUsage: const ContextWindowUsage(
            tokensUsed: 120000,
            tokenLimit: 200000,
          ),
        ),
      );

      expect(
          find.byKey(const ValueKey('context-window-button')), findsOneWidget);
    });
  });
}

Widget _buildTestApp({
  bool shouldShowPermissionChoices = false,
  bool shouldShowReviewChoices = false,
  bool awaitInput = false,
  bool isBusy = false,
  bool canStop = false,
  bool showClaudeMode = true,
  String currentEngine = 'claude',
  String? configuredEngine,
  bool codexTargetMode = false,
  ValueChanged<bool>? onCodexTargetModeChanged,
  bool isSessionLoading = false,
  ContextWindowUsage contextWindowUsage = const ContextWindowUsage(),
  void Function(String text, List<ChatImageAttachment> imageAttachments)?
      onSubmit,
  Future<ChatImageAttachment?> Function()? onAttachImage,
  VoidCallback? onStop,
  ValueChanged<String>? onPermissionModeChanged,
  EdgeInsets viewInsets = EdgeInsets.zero,
  Widget body = const SizedBox.shrink(),
}) {
  return MaterialApp(
    home: Scaffold(
      body: body,
      bottomNavigationBar: MediaQuery(
        data: MediaQueryData(viewInsets: viewInsets),
        child: CommandInputBar(
          awaitInput: awaitInput,
          isBusy: isBusy,
          canStop: canStop,
          canCompact: false,
          isCompacting: false,
          compactStatusLabel: '',
          contextWindowUsage: contextWindowUsage,
          onOpenContextWindowUsage: () {},
          hasPendingReview: false,
          fastMode: false,
          permissionMode: 'default',
          shouldShowPermissionChoices: shouldShowPermissionChoices,
          shouldShowReviewChoices: shouldShowReviewChoices,
          onSubmit: onSubmit ?? (text, images) {},
          onAttachImage: onAttachImage ?? () async => null,
          onStop: onStop ?? () {},
          onCompact: () {},
          onOpenSessions: () {},
          onOpenRuntimeInfo: () {},
          onOpenLogs: () {},
          onOpenSkills: () {},
          onOpenMemory: () {},
          onOpenPermissions: () {},
          onOpenModels: () {},
          onPermissionModeChanged: onPermissionModeChanged ?? (_) {},
          codexTargetMode: codexTargetMode,
          onCodexTargetModeChanged: onCodexTargetModeChanged ?? (_) {},
          showClaudeMode: showClaudeMode,
          currentEngine: currentEngine,
          configuredEngine: configuredEngine ?? currentEngine,
          modelSummary: 'Sonnet',
          permissionRuleSummary: '默认',
          shouldShowPlanChoices: false,
          isSessionLoading: isSessionLoading,
        ),
      ),
    ),
  );
}

class _BuildCounter extends StatelessWidget {
  const _BuildCounter({
    required this.onBuild,
    required this.child,
  });

  final VoidCallback onBuild;
  final Widget child;

  @override
  Widget build(BuildContext context) {
    onBuild();
    return child;
  }
}

final _transparentPngBytes = Uint8List.fromList([
  0x89,
  0x50,
  0x4e,
  0x47,
  0x0d,
  0x0a,
  0x1a,
  0x0a,
  0x00,
  0x00,
  0x00,
  0x0d,
  0x49,
  0x48,
  0x44,
  0x52,
  0x00,
  0x00,
  0x00,
  0x01,
  0x00,
  0x00,
  0x00,
  0x01,
  0x08,
  0x06,
  0x00,
  0x00,
  0x00,
  0x1f,
  0x15,
  0xc4,
  0x89,
  0x00,
  0x00,
  0x00,
  0x0a,
  0x49,
  0x44,
  0x41,
  0x54,
  0x78,
  0x9c,
  0x63,
  0x00,
  0x01,
  0x00,
  0x00,
  0x05,
  0x00,
  0x01,
  0x0d,
  0x0a,
  0x2d,
  0xb4,
  0x00,
  0x00,
  0x00,
  0x00,
  0x49,
  0x45,
  0x4e,
  0x44,
  0xae,
  0x42,
  0x60,
  0x82,
]);
