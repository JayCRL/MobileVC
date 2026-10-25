import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:mobile_vc/data/models/session_models.dart';
import 'package:mobile_vc/data/models/runtime_meta.dart';
import 'package:mobile_vc/features/session/session_list_sheet.dart';

void main() {
  testWidgets('外部原生会话点击删除会显示不可删除提示', (tester) async {
    String deletedSessionId = '';

    await tester.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: SessionListSheet(
            sessions: const [
              SessionSummary(
                id: 'codex-thread:1',
                title: 'Desktop Codex',
                source: 'codex-native',
                external: true,
              ),
            ],
            selectedSessionId: '',
            cwd: '/workspace',
            onCreate: () {},
            onLoad: (_) {},
            onDelete: (id) => deletedSessionId = id,
          ),
        ),
      ),
    );

    await tester.tap(find.byIcon(Icons.delete_outline));
    await tester.pump();

    expect(deletedSessionId, isEmpty);
    expect(find.text('电脑 Codex 只能恢复，不能在 MobileVC 内删除'), findsOneWidget);
  });

  testWidgets('Claude 原生会话优先显示电脑 Claude 来源', (tester) async {
    String deletedSessionId = '';

    await tester.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: SessionListSheet(
            sessions: const [
              SessionSummary(
                id: 'claude-thread:1',
                title: 'Desktop Claude',
                source: 'claude-native',
                ownership: 'claude-native',
                runtime: RuntimeMeta(source: 'claude-native'),
              ),
            ],
            selectedSessionId: '',
            cwd: '/workspace',
            onCreate: () {},
            onLoad: (_) {},
            onDelete: (id) => deletedSessionId = id,
          ),
        ),
      ),
    );

    await tester.tap(find.byIcon(Icons.delete_outline));
    await tester.pump();

    expect(deletedSessionId, isEmpty);
    expect(find.text('电脑 Claude'), findsOneWidget);
    expect(find.text('MobileVC'), findsNothing);
    expect(find.text('电脑 Claude 只能恢复，不能在 MobileVC 内删除'), findsOneWidget);
  });

  testWidgets('电脑原生会话主标题和顶部标题一致，原始标题显示为副标题', (tester) async {
    await tester.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: SessionListSheet(
            sessions: const [
              SessionSummary(
                id: 'codex-thread:1',
                title: '这里有问题不能删除mobilevc喃',
                source: 'codex-native',
                external: true,
                runtime: RuntimeMeta(
                  source: 'codex-native',
                  engine: 'codex',
                  cwd: '/workspace/MobileVC',
                ),
              ),
            ],
            selectedSessionId: '',
            cwd: '/workspace/MobileVC',
            onCreate: () {},
            onLoad: (_) {},
            onDelete: (_) {},
          ),
        ),
      ),
    );

    expect(find.text('电脑 Codex'), findsOneWidget);
    expect(find.text('这里有问题不能删除mobilevc喃'), findsOneWidget);
  });

  testWidgets('MobileVC 会话点击删除会触发 onDelete', (tester) async {
    String deletedSessionId = '';

    await tester.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: SessionListSheet(
            sessions: const [
              SessionSummary(
                id: 'session-1',
                title: 'MobileVC session',
                source: 'mobilevc',
                ownership: 'mobilevc',
              ),
            ],
            selectedSessionId: '',
            cwd: '/workspace',
            onCreate: () {},
            onLoad: (_) {},
            onDelete: (id) => deletedSessionId = id,
          ),
        ),
      ),
    );

    await tester.tap(find.byIcon(Icons.delete_outline));
    await tester.pump();

    expect(deletedSessionId, 'session-1');
    expect(find.byType(SnackBar), findsNothing);
  });

  testWidgets('MobileVC ownership 优先于旧 native runtime source，仍可删除',
      (tester) async {
    String deletedSessionId = '';

    await tester.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: SessionListSheet(
            sessions: const [
              SessionSummary(
                id: 'session-stale-runtime',
                title: 'MobileVC stale runtime',
                source: 'mobilevc',
                ownership: 'mobilevc',
                runtime: RuntimeMeta(source: 'claude-native', engine: 'codex'),
              ),
            ],
            selectedSessionId: '',
            cwd: '/workspace',
            onCreate: () {},
            onLoad: (_) {},
            onDelete: (id) => deletedSessionId = id,
          ),
        ),
      ),
    );

    expect(find.text('MobileVC stale runtime'), findsOneWidget);
    expect(find.text('电脑 Claude'), findsNothing);

    await tester.tap(find.byTooltip('删除此会话'));
    await tester.pump();

    expect(deletedSessionId, 'session-stale-runtime');
    expect(find.byType(SnackBar), findsNothing);
  });

  testWidgets('Codex mirror id 优先于旧 claude runtime source', (tester) async {
    String deletedSessionId = '';

    await tester.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: SessionListSheet(
            sessions: const [
              SessionSummary(
                id: 'codex-thread:stale-runtime',
                title: '修复会话来源',
                external: true,
                runtime: RuntimeMeta(source: 'claude-native', engine: 'claude'),
              ),
            ],
            selectedSessionId: '',
            cwd: '/workspace',
            onCreate: () {},
            onLoad: (_) {},
            onDelete: (id) => deletedSessionId = id,
          ),
        ),
      ),
    );

    expect(find.text('电脑 Codex'), findsOneWidget);
    expect(find.text('修复会话来源'), findsOneWidget);
    expect(find.text('电脑 Claude'), findsNothing);

    await tester.tap(find.byTooltip('不能删除电脑端原生会话'));
    await tester.pump();

    expect(deletedSessionId, isEmpty);
  });

  testWidgets('MobileVC 项目标题不是删除入口，只有会话卡片可删除', (tester) async {
    String deletedSessionId = '';

    await tester.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: SessionListSheet(
            sessions: const [
              SessionSummary(
                id: 'session-1',
                title: 'MobileVC session',
                source: 'mobilevc',
                ownership: 'mobilevc',
                runtime: RuntimeMeta(cwd: '/workspace/MobileVC'),
              ),
            ],
            selectedSessionId: '',
            cwd: '/workspace/MobileVC',
            onCreate: () {},
            onLoad: (_) {},
            onDelete: (id) => deletedSessionId = id,
          ),
        ),
      ),
    );

    expect(find.text('MobileVC'), findsWidgets);
    expect(find.byIcon(Icons.delete_outline), findsOneWidget);
    expect(find.byTooltip('删除此会话'), findsOneWidget);

    await tester.tap(find.byTooltip('删除此会话'));
    await tester.pump();

    expect(deletedSessionId, 'session-1');
  });

  testWidgets('会话列表按项目分组并把当前项目置顶', (tester) async {
    await tester.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: SessionListSheet(
            sessions: [
              SessionSummary(
                id: 'other',
                title: 'Other session',
                updatedAt: DateTime(2026, 5, 24),
                runtime: const RuntimeMeta(cwd: '/workspace/Other'),
              ),
              SessionSummary(
                id: 'current',
                title: 'Current session',
                updatedAt: DateTime(2026, 5, 23),
                runtime: const RuntimeMeta(cwd: '/workspace/MobileVC'),
              ),
            ],
            selectedSessionId: '',
            cwd: '/workspace/MobileVC',
            onCreate: () {},
            onLoad: (_) {},
            onDelete: (_) {},
          ),
        ),
      ),
    );

    expect(find.widgetWithText(DropdownButtonFormField<String>, '全部项目'),
        findsOneWidget);
    expect(find.text('Other session'), findsOneWidget);
    expect(find.text('Current session'), findsOneWidget);
    expect(find.text('当前'), findsOneWidget);

    final currentTop = tester.getTopLeft(find.text('Current session')).dy;
    final otherTop = tester.getTopLeft(find.text('Other session')).dy;
    expect(currentTop, lessThan(otherTop));
  });

  testWidgets('项目切换 chip 可以只显示指定项目', (tester) async {
    await tester.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: SessionListSheet(
            sessions: [
              SessionSummary(
                id: 'other',
                title: 'Other session',
                updatedAt: DateTime(2026, 5, 24),
                runtime: const RuntimeMeta(cwd: '/workspace/Other'),
              ),
              SessionSummary(
                id: 'current',
                title: 'Current session',
                updatedAt: DateTime(2026, 5, 23),
                runtime: const RuntimeMeta(cwd: '/workspace/MobileVC'),
              ),
            ],
            selectedSessionId: '',
            cwd: '/workspace/MobileVC',
            onCreate: () {},
            onLoad: (_) {},
            onDelete: (_) {},
          ),
        ),
      ),
    );

    await tester.tap(find.byType(DropdownButtonFormField<String>));
    await tester.pumpAndSettle();
    await tester.tap(find.text('Other').last);
    await tester.pumpAndSettle();

    expect(find.text('Other session'), findsOneWidget);
    expect(find.text('Current session'), findsNothing);

    await tester.tap(find.byType(DropdownButtonFormField<String>));
    await tester.pumpAndSettle();
    await tester.tap(find.text('全部项目').last);
    await tester.pumpAndSettle();

    expect(find.text('Other session'), findsOneWidget);
    expect(find.text('Current session'), findsOneWidget);
  });

  testWidgets('项目下拉承载多个 Codex 项目而不占满列表头部', (tester) async {
    final sessions = List<SessionSummary>.generate(24, (index) {
      final project = 'Project${index.toString().padLeft(2, '0')}';
      return SessionSummary(
        id: 'codex-$index',
        title: '$project session',
        updatedAt: DateTime(2026, 5, 24, 12, index),
        source: 'codex-native',
        external: true,
        runtime: RuntimeMeta(engine: 'codex', cwd: '/workspace/$project'),
      );
    });

    await tester.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: SessionListSheet(
            sessions: sessions,
            selectedSessionId: '',
            cwd: '/workspace/Project00',
            onCreate: () {},
            onLoad: (_) {},
            onDelete: (_) {},
          ),
        ),
      ),
    );

    expect(find.byType(DropdownButtonFormField<String>), findsOneWidget);
    expect(find.text('电脑 Codex'), findsWidgets);
    expect(find.text('Project23 session'), findsOneWidget);
    expect(find.widgetWithText(ChoiceChip, 'Project23'), findsNothing);
  });

  testWidgets('单项目大量会话按可见行懒加载', (tester) async {
    final sessions = List<SessionSummary>.generate(120, (index) {
      return SessionSummary(
        id: 'session-$index',
        title: 'Work item $index',
        updatedAt:
            DateTime(2026, 5, 24, 12, 0).subtract(Duration(minutes: index)),
        runtime: const RuntimeMeta(cwd: '/workspace/MobileVC'),
      );
    });

    await tester.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: SizedBox(
            height: 520,
            child: SessionListSheet(
              sessions: sessions,
              selectedSessionId: '',
              cwd: '/workspace/MobileVC',
              onCreate: () {},
              onLoad: (_) {},
              onDelete: (_) {},
            ),
          ),
        ),
      ),
    );

    expect(find.text('Work item 0'), findsOneWidget);
    expect(find.text('Work item 119'), findsNothing);

    await tester.scrollUntilVisible(
      find.text('Work item 119'),
      500,
      scrollable: find.byType(Scrollable),
    );

    expect(find.text('Work item 119'), findsOneWidget);
  });

  testWidgets('Codex Claude 和 Gemini 筛选只显示对应来源', (tester) async {
    await tester.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: SessionListSheet(
            sessions: const [
              SessionSummary(
                id: 'codex',
                title: 'Codex item',
                source: 'codex-native',
                external: true,
                runtime: RuntimeMeta(cwd: '/workspace/A'),
              ),
              SessionSummary(
                id: 'claude',
                title: 'Claude item',
                source: 'claude-native',
                external: true,
                runtime: RuntimeMeta(cwd: '/workspace/B'),
              ),
              SessionSummary(
                id: 'mobilevc',
                title: 'MobileVC Codex item',
                source: 'mobilevc',
                ownership: 'mobilevc',
                runtime: RuntimeMeta(engine: 'codex', cwd: '/workspace/C'),
              ),
              SessionSummary(
                id: 'mobilevc-claude',
                title: 'MobileVC Claude item',
                source: 'mobilevc',
                ownership: 'mobilevc',
                runtime: RuntimeMeta(engine: 'claude', cwd: '/workspace/D'),
              ),
              SessionSummary(
                id: 'gemini',
                title: 'Gemini item',
                source: 'mobilevc',
                ownership: 'mobilevc',
                runtime: RuntimeMeta(engine: 'gemini', cwd: '/workspace/E'),
              ),
            ],
            selectedSessionId: '',
            cwd: '/workspace/A',
            onCreate: () {},
            onLoad: (_) {},
            onDelete: (_) {},
          ),
        ),
      ),
    );

    await tester.tap(find.widgetWithText(ChoiceChip, 'Claude'));
    await tester.pumpAndSettle();

    expect(find.text('电脑 Claude'), findsOneWidget);
    expect(find.text('MobileVC Claude item'), findsOneWidget);
    expect(find.text('电脑 Codex'), findsNothing);
    expect(find.text('MobileVC Codex item'), findsNothing);
    expect(find.text('Gemini item'), findsNothing);

    await tester.tap(find.widgetWithText(ChoiceChip, 'Codex'));
    await tester.pumpAndSettle();

    expect(find.text('电脑 Codex'), findsOneWidget);
    expect(find.text('MobileVC Codex item'), findsOneWidget);
    expect(find.text('电脑 Claude'), findsNothing);
    expect(find.text('MobileVC Claude item'), findsNothing);
    expect(find.text('Gemini item'), findsNothing);

    await tester.tap(find.widgetWithText(ChoiceChip, 'Gemini'));
    await tester.pumpAndSettle();

    expect(find.text('Gemini item'), findsOneWidget);
    expect(find.text('电脑 Codex'), findsNothing);
    expect(find.text('MobileVC Codex item'), findsNothing);
    expect(find.text('电脑 Claude'), findsNothing);
    expect(find.text('MobileVC Claude item'), findsNothing);
    expect(find.widgetWithText(ChoiceChip, 'MobileVC'), findsNothing);
    expect(find.text('MobileVC'), findsOneWidget);
  });

  testWidgets('点击会话返回完整 summary 以便调用方切换 cwd', (tester) async {
    SessionSummary? loaded;
    const summary = SessionSummary(
      id: 'session-1',
      title: 'Target session',
      runtime: RuntimeMeta(cwd: '/workspace/Target'),
    );

    await tester.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: SessionListSheet(
            sessions: const [summary],
            selectedSessionId: '',
            cwd: '/workspace/Current',
            onCreate: () {},
            onLoad: (item) => loaded = item,
            onDelete: (_) {},
          ),
        ),
      ),
    );

    await tester.tap(find.text('Target session'));
    await tester.pump();

    expect(loaded?.id, 'session-1');
    expect(loaded?.runtime.cwd, '/workspace/Target');
  });
}
