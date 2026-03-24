import 'package:flutter/material.dart';

import '../../data/models/runtime_meta.dart';
import '../../data/models/session_models.dart';

class StatusDetailSheet extends StatelessWidget {
  const StatusDetailSheet({
    super.key,
    required this.sessionId,
    required this.sessionTitle,
    required this.connected,
    required this.awaitInput,
    required this.permissionMode,
    required this.currentPath,
    required this.runtimeMeta,
    required this.currentStep,
    required this.latestError,
    required this.canResumeCurrentSession,
    required this.agentPhaseLabel,
    required this.currentStepSummary,
    required this.recentDiff,
    required this.enabledSkillSummary,
    required this.enabledMemorySummary,
  });

  final String sessionId;
  final String sessionTitle;
  final bool connected;
  final bool awaitInput;
  final String permissionMode;
  final String currentPath;
  final RuntimeMeta runtimeMeta;
  final HistoryContext? currentStep;
  final HistoryContext? latestError;
  final bool canResumeCurrentSession;
  final String agentPhaseLabel;
  final String currentStepSummary;
  final HistoryContext? recentDiff;
  final String enabledSkillSummary;
  final String enabledMemorySummary;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final rows = <MapEntry<String, String>>[
      MapEntry('cwd', currentPath.isNotEmpty ? currentPath : runtimeMeta.cwd),
      MapEntry('sessionId', sessionId),
      MapEntry('resumeSessionId', runtimeMeta.resumeSessionId),
      MapEntry('canResume', canResumeCurrentSession ? 'true' : 'false'),
      MapEntry('permissionMode', _permissionModeLabel(permissionMode)),
      MapEntry('agentState', agentPhaseLabel),
      MapEntry('recentStep', currentStepSummary),
      MapEntry('recentDiff', recentDiff?.path ?? ''),
      MapEntry('recentError', latestError?.message ?? ''),
      MapEntry('skills', enabledSkillSummary),
      MapEntry('memory', enabledMemorySummary),
    ].where((entry) => entry.value.trim().isNotEmpty).toList();

    return SafeArea(
      top: false,
      child: Container(
        color: theme.colorScheme.surface,
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            const SizedBox(height: 8),
            Center(
              child: Container(
                width: 44,
                height: 5,
                decoration: BoxDecoration(
                  color: theme.colorScheme.outlineVariant,
                  borderRadius: BorderRadius.circular(999),
                ),
              ),
            ),
            Padding(
              padding: const EdgeInsets.fromLTRB(20, 16, 20, 8),
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  Text('连接状态详情',
                      style: theme.textTheme.titleLarge
                          ?.copyWith(fontWeight: FontWeight.w700)),
                  const SizedBox(height: 6),
                  Text(
                    sessionTitle.isEmpty
                        ? '当前会话、上下文与路径'
                        : '$sessionTitle · 当前会话、上下文与路径',
                    style: theme.textTheme.bodySmall,
                  ),
                ],
              ),
            ),
            Expanded(
              child: ListView(
                padding: const EdgeInsets.fromLTRB(16, 8, 16, 24),
                children: [
                  ...rows.map(
                    (entry) => _DetailRow(label: entry.key, value: entry.value),
                  ),
                  if (currentStep != null)
                    _DetailRow(
                      label: 'stepTool',
                      value: [
                        currentStep!.tool,
                        currentStep!.command,
                        currentStep!.targetPath
                      ].where((item) => item.isNotEmpty).join(' · '),
                    ),
                ],
              ),
            ),
          ],
        ),
      ),
    );
  }
}

String _permissionModeLabel(String permissionMode) {
  switch (permissionMode.trim()) {
    case 'acceptEdits':
      return '自动接受修改';
    case 'bypassPermissions':
      return '跳过权限确认';
    case 'default':
    default:
      return '默认确认';
  }
}

class _DetailRow extends StatelessWidget {
  const _DetailRow({required this.label, required this.value});

  final String label;
  final String value;

  @override
  Widget build(BuildContext context) {
    return Container(
      margin: const EdgeInsets.only(bottom: 10),
      padding: const EdgeInsets.symmetric(horizontal: 14, vertical: 12),
      decoration: BoxDecoration(
        color: Theme.of(context).colorScheme.surfaceContainerHighest,
        borderRadius: BorderRadius.circular(18),
      ),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Text(
            label,
            style: Theme.of(context).textTheme.labelSmall,
          ),
          const SizedBox(height: 6),
          SelectableText(value),
        ],
      ),
    );
  }
}
