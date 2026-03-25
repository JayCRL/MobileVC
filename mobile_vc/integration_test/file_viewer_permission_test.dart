import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:patrol/patrol.dart';
import 'package:mobile_vc/features/files/file_viewer_sheet.dart';
import 'package:mobile_vc/data/models/events.dart';
import 'package:mobile_vc/data/models/runtime_meta.dart';
import 'package:mobile_vc/data/models/session_models.dart';

void main() {
  patrolTest('文件页权限条显示允许拒绝并可点击授权', ($) async {
    String? submitted;

    await $.pumpWidgetAndSettle(
      MaterialApp(
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
            pendingPrompt: PromptRequestEvent(
              timestamp: DateTime(2026),
              sessionId: 'session-1',
              runtimeMeta: const RuntimeMeta(),
              raw: const {
                'type': 'prompt_request',
                'msg': 'Claude requested permissions to write to README.md',
              },
              message: 'Claude requested permissions to write to README.md',
              options: const [],
            ),
            onAccept: () {},
            onRevert: () {},
            onRevise: () {},
            onSelectReviewGroup: (_) {},
            onSelectReviewDiff: (_) {},
            onOpenDiffList: () {},
            onUseAsContext: () {},
            onSendFilePrompt: (_) {},
            onSubmitPrompt: (value) => submitted = value,
          ),
        ),
      ),
    );

    expect($(#fileViewerPermissionBar), findsOneWidget);
    expect($(#fileViewerPermissionActionY), findsOneWidget);
    expect($(#fileViewerPermissionActionN), findsOneWidget);

    await $(#fileViewerPermissionActionY).tap();
    await $.pumpAndSettle();

    expect(submitted, 'y');
  });
}

const fileViewerPermissionBar = Key('fileViewer.permissionBar');
const fileViewerPermissionActionY = Key('fileViewer.permissionAction.y');
const fileViewerPermissionActionN = Key('fileViewer.permissionAction.n');
