import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:mobile_vc/data/models/session_models.dart';
import 'package:mobile_vc/features/memory/memory_management_sheet.dart';

void main() {
  testWidgets('MemoryManagementSheet 展示同步状态与 metadata 并触发 sync',
      (tester) async {
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
                driftDetected: false,
                editable: true,
              ),
            ],
            syncStatus: 'memory 同步完成',
            catalogMeta: CatalogMetadata(
              domain: 'memory',
              sourceOfTruth: 'claude',
              syncState: 'synced',
              driftDetected: false,
              lastSyncedAt: DateTime(2026, 3, 25, 12),
              lastError: 'none',
            ),
            enabledMemoryIds: const ['mem-1'],
            onToggleEnabled: (_) {},
            onSave: (_) {},
            onSync: () {
              synced = true;
            },
          ),
        ),
      ),
    );

    expect(find.text('Memory 管理'), findsOneWidget);
    expect(find.text('memory 同步完成'), findsOneWidget);
    expect(find.textContaining('sourceOfTruth: claude'), findsOneWidget);
    expect(find.textContaining('syncState: synced'), findsWidgets);
    expect(find.textContaining('lastSyncedAt:'), findsWidgets);
    expect(find.textContaining('lastError: none'), findsOneWidget);

    await tester.tap(find.text('同步 memory'));
    await tester.pump();

    expect(synced, true);
  });
}
