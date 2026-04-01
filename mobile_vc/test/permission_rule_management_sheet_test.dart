import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:mobile_vc/data/models/session_models.dart';
import 'package:mobile_vc/features/permissions/permission_rule_management_sheet.dart';

void main() {
  testWidgets('permission rule sheet 会渲染规则并触发开关删除回调', (tester) async {
    bool? sessionEnabled;
    bool? persistentEnabled;
    PermissionRule? toggledRule;
    bool? toggledEnabled;
    PermissionRule? deletedRule;

    const sessionRule = PermissionRule(
      id: 'session-rule',
      scope: 'session',
      enabled: true,
      engine: 'codex',
      kind: 'write',
      commandHead: 'bash',
      targetPathPrefix: '/workspace/lib',
      summary: 'Codex · write · bash · /workspace/lib',
      matchCount: 2,
    );
    const persistentRule = PermissionRule(
      id: 'persistent-rule',
      scope: 'persistent',
      enabled: true,
      engine: 'codex',
      kind: 'shell',
      commandHead: 'python',
      summary: 'Codex · shell · python',
    );

    await tester.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: PermissionRuleManagementSheet(
            sessionEnabled: true,
            persistentEnabled: false,
            sessionRules: const [sessionRule],
            persistentRules: const [persistentRule],
            onSetSessionEnabled: (value) => sessionEnabled = value,
            onSetPersistentEnabled: (value) => persistentEnabled = value,
            onToggleRule: (rule, value) {
              toggledRule = rule;
              toggledEnabled = value;
            },
            onDeleteRule: (rule) => deletedRule = rule,
          ),
        ),
      ),
    );

    expect(find.text('权限管理'), findsOneWidget);
    expect(find.text('本会话规则'), findsOneWidget);
    expect(find.text('长期规则'), findsOneWidget);
    expect(find.text('Codex · write · bash · /workspace/lib'), findsOneWidget);
    expect(find.text('Codex · shell · python'), findsOneWidget);

    await tester.tap(
      find.byKey(const ValueKey<String>('permissionRules.scope.本会话规则')),
    );
    await tester.pumpAndSettle();
    expect(sessionEnabled, isFalse);

    await tester.tap(
      find.byKey(const ValueKey<String>('permissionRules.scope.长期规则')),
    );
    await tester.pumpAndSettle();
    expect(persistentEnabled, isTrue);

    await tester.tap(
      find.byKey(const ValueKey<String>('permissionRules.rule.session-rule')),
    );
    await tester.pumpAndSettle();
    expect(toggledRule?.id, 'session-rule');
    expect(toggledEnabled, isFalse);

    await tester.tap(
      find.byKey(
        const ValueKey<String>('permissionRules.delete.persistent-rule'),
      ),
    );
    await tester.pumpAndSettle();
    expect(deletedRule?.id, 'persistent-rule');
  });
}
