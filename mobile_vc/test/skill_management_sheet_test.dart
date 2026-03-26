import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:mobile_vc/data/models/session_models.dart';
import 'package:mobile_vc/features/skills/skill_management_sheet.dart';

void main() {
  testWidgets('SkillManagementSheet 展示新总览卡并触发 sync', (tester) async {
    var synced = false;
    var generated = false;

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
                editable: true,
              ),
            ],
            enabledSkillNames: const ['demo-skill'],
            syncStatus: 'skill 同步完成',
            catalogMeta: CatalogMetadata(
              domain: 'skill',
              sourceOfTruth: 'claude',
              syncState: 'synced',
              lastSyncedAt: DateTime(2026, 3, 25, 12),
            ),
            onToggleEnabled: (_) {},
            onSave: (_) {},
            onSync: () => synced = true,
            onExecuteSkill: (_) {},
            onGenerateSkill: (_) => generated = true,
            onReviseSkill: (_, __) {},
          ),
        ),
      ),
    );

    expect(find.text('Skill 管理'), findsOneWidget);
    expect(find.text('skill 同步完成'), findsOneWidget);
    expect(find.textContaining('总数: 1'), findsOneWidget);
    expect(find.textContaining('已启用: 1'), findsOneWidget);

    await tester.tap(find.text('同步 skill'));
    await tester.pump();
    expect(synced, true);

    await tester.tap(find.byKey(const ValueKey('skillManagement.generateButton')));
    await tester.pumpAndSettle();
    await tester.enterText(
      find.byKey(const ValueKey('skillManagement.generateInput')),
      '生成一个新的 skill',
    );
    await tester.tap(find.text('交给 Claude 生成'));
    await tester.pumpAndSettle();
    expect(generated, true);
  });

  testWidgets('skill 胶囊支持点击执行、切换启用和长按详情修改', (tester) async {
    String? executed;
    String? toggled;
    String? revised;

    await tester.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: SkillManagementSheet(
            skills: const [
              SkillDefinition(
                name: 'review-pr',
                description: 'review desc',
                prompt: 'do review',
                targetType: 'diff',
                resultView: 'review-card',
                source: 'local',
                syncState: 'draft',
                editable: true,
              ),
            ],
            enabledSkillNames: const [],
            syncStatus: '',
            catalogMeta: const CatalogMetadata(domain: 'skill'),
            onToggleEnabled: (name) => toggled = name,
            onSave: (_) {},
            onSync: () {},
            onExecuteSkill: (name) => executed = name,
            onGenerateSkill: (_) {},
            onReviseSkill: (skill, request) => revised = '${skill.name}:$request',
          ),
        ),
      ),
    );

    await tester.tap(find.byKey(const ValueKey('skillCapsule:review-pr')));
    await tester.pump();
    expect(executed, 'review-pr');

    await tester.tap(find.byType(Switch));
    await tester.pump();
    expect(toggled, 'review-pr');

    await tester.longPress(find.byKey(const ValueKey('skillCapsule:review-pr')));
    await tester.pumpAndSettle();
    expect(find.text('Prompt'), findsOneWidget);

    await tester.enterText(
      find.byKey(const ValueKey('skillDetail.modifyInput:review-pr')),
      '把它改成更适合移动端审阅',
    );
    await tester.tap(find.text('让 Claude 修改'));
    await tester.pumpAndSettle();
    expect(revised, 'review-pr:把它改成更适合移动端审阅');
  });
}
