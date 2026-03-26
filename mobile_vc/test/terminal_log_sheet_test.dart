import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:mobile_vc/data/models/session_models.dart';
import 'package:mobile_vc/features/status/terminal_log_sheet.dart';

void main() {
  testWidgets('按 execution 展示命令列表并切换日志内容', (tester) async {
    String? selectedExecutionId;
    await tester.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: TerminalLogSheet(
            executions: const [
              TerminalExecution(
                executionId: 'exec-1',
                command: 'npm test',
                cwd: '/workspace/app',
                source: 'user',
                sourceLabel: '用户输入',
                contextTitle: '单元测试',
                groupTitle: '测试组',
                stdout: 'pass 1',
                stderr: '',
                exitCode: 0,
              ),
              TerminalExecution(
                executionId: 'exec-2',
                command: 'flutter test',
                cwd: '/workspace/mobile_vc',
                source: 'review-follow-up',
                sourceLabel: '审核后续',
                stdout: 'running',
                stderr: 'warning',
                running: true,
              ),
            ],
            activeExecutionId: 'exec-1',
            stdout: 'pass 1',
            stderr: '',
            onSelectExecution: (value) => selectedExecutionId = value,
          ),
        ),
      ),
    );

    expect(find.text('npm test'), findsWidgets);
    expect(find.text('flutter test'), findsOneWidget);
    expect(find.text('pass 1'), findsOneWidget);
    expect(find.text('来源: 用户输入'), findsOneWidget);
    expect(find.text('上下文: 单元测试'), findsOneWidget);
    expect(find.text('修改组: 测试组'), findsOneWidget);
    expect(find.text('状态: exit 0'), findsOneWidget);

    await tester.tap(find.text('flutter test').first);
    await tester.pump();

    expect(selectedExecutionId, 'exec-2');
  });
}
