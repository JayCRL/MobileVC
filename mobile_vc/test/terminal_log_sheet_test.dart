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
                stdout: 'pass 1',
                stderr: '',
              ),
              TerminalExecution(
                executionId: 'exec-2',
                command: 'flutter test',
                cwd: '/workspace/mobile_vc',
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

    expect(find.text('npm test'), findsOneWidget);
    expect(find.text('flutter test'), findsOneWidget);
    expect(find.text('pass 1'), findsOneWidget);

    await tester.tap(find.text('flutter test'));
    await tester.pump();

    expect(selectedExecutionId, 'exec-2');
  });
}
