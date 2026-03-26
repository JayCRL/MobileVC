import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:mobile_vc/data/models/session_models.dart';
import 'package:mobile_vc/features/memory/memory_management_sheet.dart';

void main() {
  testWidgets('MemoryManagementSheet 展示新总览卡并触发 sync', (tester) async {
    var synced = false;

    await tester.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: MemoryManagementSheet(
            items: const [
              MemoryItem(
                id: 'mem-1',
                title: 'Memory 1',
                content: 'hello',
                source: 'external',
                sourceOfTruth: 'claude',
                syncState: 'synced',
                editable: true,
              ),
            ],
            syncStatus: 'memory 同步完成',
            catalogMeta: CatalogMetadata(
              domain: 'memory',
              sourceOfTruth: 'claude',
              syncState: 'synced',
              lastSyncedAt: DateTime(2026, 3, 25, 12),
            ),
            enabledMemoryIds: const ['mem-1'],
            onToggleEnabled: (_) {},
            onSave: (_) {},
            onSync: () => synced = true,
            onReviseMemory: (_, __) {},
          ),
        ),
      ),
    );

    expect(find.text('Memory 管理'), findsOneWidget);
    expect(find.text('memory 同步完成'), findsOneWidget);
    expect(find.textContaining('总数: 1'), findsOneWidget);
    expect(find.textContaining('已启用: 1'), findsOneWidget);

    await tester.tap(find.text('同步 memory'));
    await tester.pump();
    expect(synced, true);
  });

  testWidgets('memory 支持查看详情并一句话修改', (tester) async {
    String? revised;
    String? toggled;

    await tester.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: MemoryManagementSheet(
            items: const [
              MemoryItem(
                id: 'mem-9',
                title: '偏好',
                content: '用户喜欢深色模式',
                source: 'local',
                syncState: 'draft',
                editable: true,
              ),
            ],
            syncStatus: '',
            catalogMeta: const CatalogMetadata(domain: 'memory'),
            enabledMemoryIds: const [],
            onToggleEnabled: (id) => toggled = id,
            onSave: (_) {},
            onSync: () {},
            onReviseMemory: (item, request) => revised = '${item.id}:$request',
          ),
        ),
      ),
    );

    await tester.tap(find.byType(Switch));
    await tester.pump();
    expect(toggled, 'mem-9');

    await tester.tap(find.byKey(const ValueKey('memoryCard:mem-9')));
    await tester.pumpAndSettle();
    expect(find.text('完整内容'), findsOneWidget);

    await tester.enterText(
      find.byKey(const ValueKey('memoryDetail.modifyInput:mem-9')),
      '改成偏好浅色、但保留 iOS 风格',
    );
    await tester.tap(find.text('让 Claude 修改这个 memory'));
    await tester.pumpAndSettle();
    expect(revised, 'mem-9:改成偏好浅色、但保留 iOS 风格');
  });

  testWidgets('展开手动编辑区后可输入 memory 草稿', (tester) async {
    await tester.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: MemoryManagementSheet(
            items: const [],
            syncStatus: '',
            catalogMeta: const CatalogMetadata(domain: 'memory'),
            enabledMemoryIds: const [],
            onToggleEnabled: (_) {},
            onSave: (_) {},
            onSync: () {},
            onReviseMemory: (_, __) {},
          ),
        ),
      ),
    );

    await tester.tap(find.text('展开'));
    await tester.pumpAndSettle();

    await tester.enterText(find.widgetWithText(TextField, 'id'), 'mem-2');
    await tester.enterText(find.widgetWithText(TextField, 'title'), 'New Memory');
    await tester.enterText(find.widgetWithText(TextField, 'content'), 'remember this');

    expect(find.text('mem-2'), findsOneWidget);
    expect(find.text('New Memory'), findsOneWidget);
    expect(find.text('remember this'), findsOneWidget);
    expect(find.widgetWithText(FilledButton, '保存 memory'), findsOneWidget);
  });
}
