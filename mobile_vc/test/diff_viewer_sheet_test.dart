import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:mobile_vc/data/models/session_models.dart';
import 'package:mobile_vc/features/diff/diff_viewer_sheet.dart';

void main() {
  group('DiffViewerSheet', () {
    testWidgets('按修改组与组内文件切换并触发审核动作', (tester) async {
      String? selectedGroupId;
      String? selectedDiffId;
      var accepted = false;
      var reverted = false;
      var revised = false;

      await tester.pumpWidget(
        MaterialApp(
          home: Scaffold(
            body: DiffViewerSheet(
              title: 'fallback title',
              path: '/workspace/fallback.dart',
              diff: '@@ -1 +1 @@\n-old\n+new',
              pendingDiffs: _reviewDiffs,
              reviewGroups: _reviewGroups,
              activeReviewGroupId: 'group-1',
              activeDiffId: 'diff-1',
              showReviewActions: true,
              onSelectGroup: (value) => selectedGroupId = value,
              onSelectDiff: (value) => selectedDiffId = value,
              onAccept: () => accepted = true,
              onRevert: () => reverted = true,
              onRevise: () => revised = true,
            ),
          ),
        ),
      );

      expect(find.text('test_a.dart'), findsWidgets);
      expect(find.text('组一'), findsWidgets);
      expect(find.text('组二'), findsWidgets);
      expect(find.text('1. test_a.dart'), findsOneWidget);
      expect(find.text('2. test_b.dart'), findsOneWidget);
      expect(find.text('同意'), findsOneWidget);
      expect(find.text('撤销'), findsOneWidget);
      expect(find.text('继续调整'), findsOneWidget);

      await tester.tap(find.text('组二').last);
      await tester.pump();
      expect(selectedGroupId, 'group-2');

      await tester.tap(find.text('2. test_b.dart'));
      await tester.pump();
      expect(selectedDiffId, 'diff-2');

      await tester.tap(find.text('同意'));
      await tester.pump();
      expect(accepted, isTrue);

      await tester.tap(find.text('撤销'));
      await tester.pump();
      expect(reverted, isTrue);

      await tester.tap(find.text('继续调整'));
      await tester.pump();
      expect(revised, isTrue);
    });

    testWidgets('无 active id 时回退到最后一个修改组和文件', (tester) async {
      await tester.pumpWidget(
        MaterialApp(
          home: Scaffold(
            body: DiffViewerSheet(
              title: '',
              path: '',
              diff: '',
              pendingDiffs: _reviewDiffs,
              reviewGroups: _reviewGroups,
            ),
          ),
        ),
      );

      expect(find.text('test_c.dart'), findsWidgets);
      expect(find.text('/workspace/test_c.dart'), findsOneWidget);
      expect(find.text('组二'), findsWidgets);
    });
  });
}

const _reviewDiffs = [
  HistoryContext(
    id: 'diff-1',
    type: 'diff',
    path: '/workspace/test_a.dart',
    title: 'test_a.dart',
    diff: '@@ -1 +1 @@\n-old a\n+new a',
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
    diff: '@@ -1 +1 @@\n-old b\n+new b',
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
    diff: '@@ -1 +1 @@\n-old c\n+new c',
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
      ReviewFile(id: 'diff-1', path: '/workspace/test_a.dart', title: 'test_a.dart'),
      ReviewFile(id: 'diff-2', path: '/workspace/test_b.dart', title: 'test_b.dart'),
    ],
  ),
  ReviewGroup(
    id: 'group-2',
    title: '组二',
    pendingReview: true,
    reviewStatus: 'pending',
    pendingCount: 1,
    files: [
      ReviewFile(id: 'diff-3', path: '/workspace/test_c.dart', title: 'test_c.dart'),
    ],
  ),
];
