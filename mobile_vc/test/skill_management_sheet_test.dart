import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:mobile_vc/data/models/session_models.dart';
import 'package:mobile_vc/features/skills/skill_management_sheet.dart';

void main() {
  testWidgets('SkillManagementSheet 展示同步状态与 metadata 并触发 sync',
      (tester) async {
    var synced = false;

    await tester.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: SkillManagementSheet(
            skills: const [
              SkillDefinition(
                name: 'demo-skill',
                description: 'desc',
                source: 'external',
                sourceOfTruth: 'claude',
                syncState: 'synced',
                driftDetected: false,
                editable: true,
              ),
            ],
            enabledSkillNames: const ['demo-skill'],
            syncStatus: 'skill 同步完成',
            catalogMeta: CatalogMetadata(
              domain: 'skill',
              sourceOfTruth: 'claude',
              syncState: 'synced',
              driftDetected: false,
              lastSyncedAt: DateTime(2026, 3, 25, 12),
              lastError: 'none',
            ),
            onToggleEnabled: (_) {},
            onSave: (_) {},
            onSync: () {
              synced = true;
            },
          ),
        ),
      ),
    );

    expect(find.text('Skill 管理'), findsOneWidget);
    expect(find.text('skill 同步完成'), findsOneWidget);
    expect(find.textContaining('sourceOfTruth: claude'), findsOneWidget);
    expect(find.textContaining('syncState: synced'), findsWidgets);
    expect(find.textContaining('lastSyncedAt:'), findsWidgets);
    expect(find.textContaining('lastError: none'), findsOneWidget);

    await tester.tap(find.text('同步 skill'));
    await tester.pump();

    expect(synced, true);
  });
}
