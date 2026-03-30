import 'package:flutter/foundation.dart';

import 'runtime_meta.dart';
import 'session_models.dart';

DateTime _readTimestamp(Map<String, dynamic> json) {
  final value = json['timestamp']?.toString();
  return DateTime.tryParse(value ?? '')?.toLocal() ?? DateTime.now();
}

abstract class AppEvent {
  const AppEvent({
    required this.type,
    required this.timestamp,
    required this.sessionId,
    required this.runtimeMeta,
    required this.raw,
  });

  final String type;
  final DateTime timestamp;
  final String sessionId;
  final RuntimeMeta runtimeMeta;
  final Map<String, dynamic> raw;
}

class UnknownEvent extends AppEvent {
  const UnknownEvent({
    required super.type,
    required super.timestamp,
    required super.sessionId,
    required super.runtimeMeta,
    required super.raw,
  });
}

class LogEvent extends AppEvent {
  const LogEvent({
    required super.timestamp,
    required super.sessionId,
    required super.runtimeMeta,
    required super.raw,
    this.message = '',
    this.stream = '',
  }) : super(type: 'log');

  final String message;
  final String stream;

  factory LogEvent.fromJson(Map<String, dynamic> json) => LogEvent(
        timestamp: _readTimestamp(json),
        sessionId: (json['sessionId'] ?? '').toString(),
        runtimeMeta: RuntimeMeta.fromJson(json),
        raw: json,
        message: (json['msg'] ?? '').toString(),
        stream: (json['stream'] ?? '').toString(),
      );
}

class ProgressEvent extends AppEvent {
  const ProgressEvent({
    required super.timestamp,
    required super.sessionId,
    required super.runtimeMeta,
    required super.raw,
    this.message = '',
    this.percent = 0,
  }) : super(type: 'progress');

  final String message;
  final int percent;

  factory ProgressEvent.fromJson(Map<String, dynamic> json) => ProgressEvent(
        timestamp: _readTimestamp(json),
        sessionId: (json['sessionId'] ?? '').toString(),
        runtimeMeta: RuntimeMeta.fromJson(json),
        raw: json,
        message: (json['msg'] ?? '').toString(),
        percent: (json['percent'] as num?)?.toInt() ?? 0,
      );
}

class ErrorEvent extends AppEvent {
  const ErrorEvent({
    required super.timestamp,
    required super.sessionId,
    required super.runtimeMeta,
    required super.raw,
    this.message = '',
    this.stack = '',
    this.code = '',
    this.targetPath = '',
    this.step = '',
    this.command = '',
  }) : super(type: 'error');

  final String message;
  final String stack;
  final String code;
  final String targetPath;
  final String step;
  final String command;

  factory ErrorEvent.fromJson(Map<String, dynamic> json) => ErrorEvent(
        timestamp: _readTimestamp(json),
        sessionId: (json['sessionId'] ?? '').toString(),
        runtimeMeta: RuntimeMeta.fromJson(json),
        raw: json,
        message: (json['msg'] ?? '').toString(),
        stack: (json['stack'] ?? '').toString(),
        code: (json['code'] ?? '').toString(),
        targetPath: (json['targetPath'] ?? '').toString(),
        step: (json['step'] ?? '').toString(),
        command: (json['command'] ?? '').toString(),
      );
}

class PromptOption {
  const PromptOption({
    required this.value,
    this.label = '',
  });

  final String value;
  final String label;

  String get displayText =>
      label.trim().isNotEmpty ? label.trim() : value.trim();
}

class InteractionAction {
  const InteractionAction({
    required this.id,
    this.label = '',
    this.variant = '',
    this.value = '',
    this.decision = '',
    this.submitMode = '',
    this.needsInput = false,
    this.destructive = false,
  });

  final String id;
  final String label;
  final String variant;
  final String value;
  final String decision;
  final String submitMode;
  final bool needsInput;
  final bool destructive;

  String get displayLabel {
    if (label.trim().isNotEmpty) {
      return label.trim();
    }
    if (value.trim().isNotEmpty) {
      return value.trim();
    }
    return id.trim();
  }

  factory InteractionAction.fromJson(Map<String, dynamic> json) {
    return InteractionAction(
      id: (json['id'] ?? json['value'] ?? '').toString(),
      label: (json['label'] ?? '').toString(),
      variant: (json['variant'] ?? '').toString(),
      value: (json['value'] ?? '').toString(),
      decision: (json['decision'] ?? '').toString(),
      submitMode: (json['submitMode'] ?? '').toString(),
      needsInput: json['needsInput'] == true,
      destructive: json['destructive'] == true,
    );
  }
}

class PlanQuestion {
  const PlanQuestion({
    required this.id,
    this.title = '',
    this.message = '',
    this.options = const [],
  });

  final String id;
  final String title;
  final String message;
  final List<PromptOption> options;

  String get displayLabel {
    if (title.trim().isNotEmpty) {
      return title.trim();
    }
    if (message.trim().isNotEmpty) {
      return message.trim();
    }
    return id.trim();
  }

  bool get hasVisiblePrompt =>
      title.trim().isNotEmpty ||
      message.trim().isNotEmpty ||
      options.any((option) => option.displayText.isNotEmpty);

  factory PlanQuestion.fromJson(Map<String, dynamic> json) {
    return PlanQuestion(
      id: (json['id'] ?? json['questionId'] ?? json['key'] ?? '').toString(),
      title: (json['title'] ?? json['label'] ?? '').toString(),
      message: _readPromptMessage(json),
      options: _readPromptOptions(json),
    );
  }
}

class InteractionRequestEvent extends AppEvent {
  const InteractionRequestEvent({
    required super.timestamp,
    required super.sessionId,
    required super.runtimeMeta,
    required super.raw,
    this.kind = '',
    this.title = '',
    this.message = '',
    this.options = const [],
    this.actions = const [],
    this.planQuestions = const [],
    this.contextId = '',
    this.contextTitle = '',
    this.targetPath = '',
    this.executionId = '',
    this.groupId = '',
    this.groupTitle = '',
    this.resumeSessionId = '',
    this.permissionMode = '',
    this.inputLabel = '',
    this.inputPlaceholder = '',
  }) : super(type: 'interaction_request');

  final String kind;
  final String title;
  final String message;
  final List<PromptOption> options;
  final List<InteractionAction> actions;
  final List<PlanQuestion> planQuestions;
  final String contextId;
  final String contextTitle;
  final String targetPath;
  final String executionId;
  final String groupId;
  final String groupTitle;
  final String resumeSessionId;
  final String permissionMode;
  final String inputLabel;
  final String inputPlaceholder;

  bool get hasVisiblePrompt =>
      title.trim().isNotEmpty ||
      message.trim().isNotEmpty ||
      actions.any((action) => action.displayLabel.isNotEmpty) ||
      options.any((option) => option.displayText.isNotEmpty) ||
      planQuestions.any((question) => question.hasVisiblePrompt);

  bool get isPermission => kind.trim().toLowerCase() == 'permission';
  bool get isReview => kind.trim().toLowerCase() == 'review';
  bool get isChoice => kind.trim().toLowerCase() == 'choice';
  bool get isInput => kind.trim().toLowerCase() == 'input';
  bool get isPlan => kind.trim().toLowerCase() == 'plan';

  factory InteractionRequestEvent.fromJson(Map<String, dynamic> json) {
    return InteractionRequestEvent(
      timestamp: _readTimestamp(json),
      sessionId: (json['sessionId'] ?? '').toString(),
      runtimeMeta: RuntimeMeta.fromJson(json),
      raw: json,
      kind: (json['kind'] ?? '').toString(),
      title: (json['title'] ?? '').toString(),
      message: _readPromptMessage(json),
      options: _readPromptOptions(json),
      actions: ((json['actions'] as List?) ?? const [])
          .whereType<Map<String, dynamic>>()
          .map(InteractionAction.fromJson)
          .toList(),
      planQuestions: _readPlanQuestions(json),
      contextId: (json['contextId'] ?? '').toString(),
      contextTitle: (json['contextTitle'] ?? '').toString(),
      targetPath: (json['targetPath'] ?? '').toString(),
      executionId: (json['executionId'] ?? '').toString(),
      groupId: (json['groupId'] ?? '').toString(),
      groupTitle: (json['groupTitle'] ?? '').toString(),
      resumeSessionId: (json['resumeSessionId'] ?? '').toString(),
      permissionMode: (json['permissionMode'] ?? '').toString(),
      inputLabel: (json['inputLabel'] ?? '').toString(),
      inputPlaceholder: (json['inputPlaceholder'] ?? '').toString(),
    );
  }
}

class PromptRequestEvent extends AppEvent {
  const PromptRequestEvent({
    required super.timestamp,
    required super.sessionId,
    required super.runtimeMeta,
    required super.raw,
    this.message = '',
    this.options = const [],
  }) : super(type: 'prompt_request');

  final String message;
  final List<PromptOption> options;

  bool get hasVisiblePrompt =>
      message.trim().isNotEmpty ||
      options.any((option) => option.displayText.isNotEmpty);

  bool get looksLikeReviewPrompt => _looksLikeReviewPromptOptions(options);

  bool get looksLikePermissionPrompt {
    if (looksLikeReviewPrompt) {
      return false;
    }
    final optionValues = _normalizedPromptOptionValues(options);
    const binaryPermissionValues = <String>{
      'y',
      'n',
      'yes',
      'no',
      'allow',
      'deny',
      'approve',
      'reject',
      '允许',
      '拒绝',
      '同意',
      '取消',
    };
    final hasBinaryPermissionOptions = optionValues.isNotEmpty &&
        optionValues.every(binaryPermissionValues.contains);
    final normalizedMessage = message.trim().toLowerCase();
    final looksLikePermissionMessage =
        normalizedMessage.contains('permission') ||
            normalizedMessage.contains('authorize') ||
            normalizedMessage.contains('allow ') ||
            normalizedMessage.contains('allow\n') ||
            normalizedMessage.contains('confirm permission') ||
            normalizedMessage.contains('confirm access') ||
            normalizedMessage.contains('confirm authorization') ||
            normalizedMessage.contains('请求授权') ||
            normalizedMessage.contains('需要授权') ||
            normalizedMessage.contains('确认授权') ||
            normalizedMessage.contains('是否允许') ||
            normalizedMessage.contains('允许') ||
            normalizedMessage.contains('授权');
    return hasBinaryPermissionOptions || looksLikePermissionMessage;
  }

  factory PromptRequestEvent.fromJson(Map<String, dynamic> json) =>
      PromptRequestEvent(
        timestamp: _readTimestamp(json),
        sessionId: (json['sessionId'] ?? '').toString(),
        runtimeMeta: RuntimeMeta.fromJson(json),
        raw: json,
        message: _readPromptMessage(json),
        options: _readPromptOptions(json),
      );
}

Set<String> _normalizedPromptOptionValues(List<PromptOption> options) {
  return options
      .map((option) => option.value.trim().toLowerCase())
      .where((value) => value.isNotEmpty)
      .toSet();
}

bool _looksLikeReviewPromptOptions(List<PromptOption> options) {
  final optionValues = _normalizedPromptOptionValues(options);
  return optionValues.contains('accept') &&
      optionValues.contains('revert') &&
      optionValues.contains('revise');
}

String _readPromptMessage(Map<String, dynamic> json) {
  final candidates = <Object?>[
    json['msg'],
    json['message'],
    json['prompt'],
    json['text'],
    json['question'],
  ];

  final details = json['details'];
  if (details is Map<String, dynamic>) {
    candidates.addAll([
      details['msg'],
      details['message'],
      details['prompt'],
      details['text'],
      details['question'],
    ]);
  }

  for (final candidate in candidates) {
    final value = candidate?.toString().trim() ?? '';
    if (value.isNotEmpty) {
      return value;
    }
  }
  return '';
}

List<PromptOption> _readPromptOptions(Map<String, dynamic> json) {
  final sources = <Object?>[
    json['options'],
    json['choices'],
    json['buttons'],
    json['selections'],
  ];

  final details = json['details'];
  if (details is Map<String, dynamic>) {
    sources.addAll([
      details['options'],
      details['choices'],
      details['buttons'],
      details['selections'],
    ]);
  }

  for (final source in sources) {
    final parsed = _parsePromptOptions(source);
    if (parsed.isNotEmpty) {
      return parsed;
    }
  }
  return const [];
}

List<PromptOption> _parsePromptOptions(Object? source) {
  if (source is! List) {
    return const [];
  }

  final options = <PromptOption>[];
  for (final item in source) {
    if (item is String) {
      final value = item.trim();
      if (value.isNotEmpty) {
        options.add(PromptOption(value: value));
      }
      continue;
    }
    if (item is Map) {
      final value = <Object?>[
        item['value'],
        item['id'],
        item['key'],
        item['data'],
        item['text'],
        item['label'],
        item['title'],
        item['name'],
      ].map((entry) => entry?.toString().trim() ?? '').firstWhere(
            (entry) => entry.isNotEmpty,
            orElse: () => '',
          );
      final label = <Object?>[
        item['label'],
        item['title'],
        item['text'],
        item['name'],
        item['display'],
      ].map((entry) => entry?.toString().trim() ?? '').firstWhere(
            (entry) => entry.isNotEmpty,
            orElse: () => '',
          );
      if (value.isNotEmpty || label.isNotEmpty) {
        options.add(PromptOption(
            value: value.isNotEmpty ? value : label, label: label));
      }
    }
  }
  return options;
}

List<PlanQuestion> _readPlanQuestions(Map<String, dynamic> json) {
  final sources = <Object?>[
    json['questions'],
    json['planQuestions'],
    json['steps'],
  ];

  final details = json['details'];
  if (details is Map<String, dynamic>) {
    sources.addAll([
      details['questions'],
      details['planQuestions'],
      details['steps'],
    ]);
  }

  for (final source in sources) {
    final parsed = _parsePlanQuestions(source);
    if (parsed.isNotEmpty) {
      return parsed;
    }
  }
  return const [];
}

List<PlanQuestion> _parsePlanQuestions(Object? source) {
  if (source is! List) {
    return const [];
  }

  final questions = <PlanQuestion>[];
  for (final item in source) {
    if (item is String) {
      final value = item.trim();
      if (value.isNotEmpty) {
        questions.add(PlanQuestion(id: value, title: value));
      }
      continue;
    }
    if (item is Map<String, dynamic>) {
      final options = _parsePromptOptions(
        item['options'] ??
            item['choices'] ??
            item['buttons'] ??
            item['selections'],
      );
      questions.add(PlanQuestion(
        id: (item['id'] ?? item['questionId'] ?? item['key'] ?? '').toString(),
        title: (item['title'] ?? item['label'] ?? '').toString(),
        message: _readPromptMessage(item),
        options: options,
      ));
      continue;
    }
    if (item is Map) {
      questions.add(PlanQuestion.fromJson(item.cast<String, dynamic>()));
    }
  }
  return questions;
}

class SessionStateEvent extends AppEvent {
  const SessionStateEvent({
    required super.timestamp,
    required super.sessionId,
    required super.runtimeMeta,
    required super.raw,
    this.state = '',
    this.message = '',
  }) : super(type: 'session_state');

  final String state;
  final String message;

  factory SessionStateEvent.fromJson(Map<String, dynamic> json) =>
      SessionStateEvent(
        timestamp: _readTimestamp(json),
        sessionId: (json['sessionId'] ?? '').toString(),
        runtimeMeta: RuntimeMeta.fromJson(json),
        raw: json,
        state: (json['state'] ?? '').toString(),
        message: (json['msg'] ?? '').toString(),
      );
}

class RuntimePhaseEvent extends AppEvent {
  const RuntimePhaseEvent({
    required super.timestamp,
    required super.sessionId,
    required super.runtimeMeta,
    required super.raw,
    this.phase = '',
    this.kind = '',
    this.message = '',
  }) : super(type: 'runtime_phase');

  final String phase;
  final String kind;
  final String message;

  bool get isPermissionBlocked =>
      phase.trim().toLowerCase() == 'permission_blocked';

  factory RuntimePhaseEvent.fromJson(Map<String, dynamic> json) =>
      RuntimePhaseEvent(
        timestamp: _readTimestamp(json),
        sessionId: (json['sessionId'] ?? '').toString(),
        runtimeMeta: RuntimeMeta.fromJson(json),
        raw: json,
        phase: (json['phase'] ?? '').toString(),
        kind: (json['kind'] ?? '').toString(),
        message: (json['msg'] ?? '').toString(),
      );
}

class AgentStateEvent extends AppEvent {
  const AgentStateEvent({
    required super.timestamp,
    required super.sessionId,
    required super.runtimeMeta,
    required super.raw,
    this.state = '',
    this.message = '',
    this.awaitInput = false,
    this.command = '',
    this.step = '',
    this.tool = '',
  }) : super(type: 'agent_state');

  final String state;
  final String message;
  final bool awaitInput;
  final String command;
  final String step;
  final String tool;

  factory AgentStateEvent.fromJson(Map<String, dynamic> json) =>
      AgentStateEvent(
        timestamp: _readTimestamp(json),
        sessionId: (json['sessionId'] ?? '').toString(),
        runtimeMeta: RuntimeMeta.fromJson(json),
        raw: json,
        state: (json['state'] ?? '').toString(),
        message: (json['msg'] ?? '').toString(),
        awaitInput: json['awaitInput'] == true,
        command: (json['command'] ?? '').toString(),
        step: (json['step'] ?? '').toString(),
        tool: (json['tool'] ?? '').toString(),
      );
}

class FSListResultEvent extends AppEvent {
  const FSListResultEvent({
    required super.timestamp,
    required super.sessionId,
    required super.runtimeMeta,
    required super.raw,
    this.currentPath = '',
    this.items = const [],
  }) : super(type: 'fs_list_result');

  final String currentPath;
  final List<FSItem> items;

  factory FSListResultEvent.fromJson(Map<String, dynamic> json) =>
      FSListResultEvent(
        timestamp: _readTimestamp(json),
        sessionId: (json['sessionId'] ?? '').toString(),
        runtimeMeta: RuntimeMeta.fromJson(json),
        raw: json,
        currentPath: (json['current_path'] ?? '').toString(),
        items: ((json['items'] as List?) ?? const [])
            .whereType<Map<String, dynamic>>()
            .map(FSItem.fromJson)
            .toList(),
      );
}

class FSReadResultEvent extends AppEvent {
  const FSReadResultEvent({
    required super.timestamp,
    required super.sessionId,
    required super.runtimeMeta,
    required super.raw,
    required this.result,
  }) : super(type: 'fs_read_result');

  final FileReadResult result;

  factory FSReadResultEvent.fromJson(Map<String, dynamic> json) =>
      FSReadResultEvent(
        timestamp: _readTimestamp(json),
        sessionId: (json['sessionId'] ?? '').toString(),
        runtimeMeta: RuntimeMeta.fromJson(json),
        raw: json,
        result: FileReadResult.fromJson(json),
      );
}

class StepUpdateEvent extends AppEvent {
  const StepUpdateEvent({
    required super.timestamp,
    required super.sessionId,
    required super.runtimeMeta,
    required super.raw,
    this.message = '',
    this.status = '',
    this.target = '',
    this.tool = '',
    this.command = '',
  }) : super(type: 'step_update');

  final String message;
  final String status;
  final String target;
  final String tool;
  final String command;

  factory StepUpdateEvent.fromJson(Map<String, dynamic> json) =>
      StepUpdateEvent(
        timestamp: _readTimestamp(json),
        sessionId: (json['sessionId'] ?? '').toString(),
        runtimeMeta: RuntimeMeta.fromJson(json),
        raw: json,
        message: (json['msg'] ?? '').toString(),
        status: (json['status'] ?? '').toString(),
        target: (json['target'] ?? '').toString(),
        tool: (json['tool'] ?? '').toString(),
        command: (json['command'] ?? '').toString(),
      );
}

class FileDiffEvent extends AppEvent {
  const FileDiffEvent({
    required super.timestamp,
    required super.sessionId,
    required super.runtimeMeta,
    required super.raw,
    this.path = '',
    this.title = '',
    this.diff = '',
    this.lang = '',
  }) : super(type: 'file_diff');

  final String path;
  final String title;
  final String diff;
  final String lang;

  factory FileDiffEvent.fromJson(Map<String, dynamic> json) => FileDiffEvent(
        timestamp: _readTimestamp(json),
        sessionId: (json['sessionId'] ?? '').toString(),
        runtimeMeta: RuntimeMeta.fromJson(json),
        raw: json,
        path: (json['path'] ?? '').toString(),
        title: (json['title'] ?? '').toString(),
        diff: (json['diff'] ?? '').toString(),
        lang: (json['lang'] ?? '').toString(),
      );
}

class RuntimeInfoResultEvent extends AppEvent {
  const RuntimeInfoResultEvent({
    required super.timestamp,
    required super.sessionId,
    required super.runtimeMeta,
    required super.raw,
    this.query = '',
    this.title = '',
    this.items = const [],
    this.unavailable = false,
    this.message = '',
  }) : super(type: 'runtime_info_result');

  final String query;
  final String title;
  final List<RuntimeInfoItem> items;
  final bool unavailable;
  final String message;

  factory RuntimeInfoResultEvent.fromJson(Map<String, dynamic> json) =>
      RuntimeInfoResultEvent(
        timestamp: _readTimestamp(json),
        sessionId: (json['sessionId'] ?? '').toString(),
        runtimeMeta: RuntimeMeta.fromJson(json),
        raw: json,
        query: (json['query'] ?? '').toString(),
        title: (json['title'] ?? '').toString(),
        message: (json['msg'] ?? '').toString(),
        unavailable: json['unavailable'] == true,
        items: ((json['items'] as List?) ?? const [])
            .whereType<Map<String, dynamic>>()
            .map(RuntimeInfoItem.fromJson)
            .toList(),
      );
}

class SessionCreatedEvent extends AppEvent {
  const SessionCreatedEvent({
    required super.timestamp,
    required super.sessionId,
    required super.runtimeMeta,
    required super.raw,
    required this.summary,
  }) : super(type: 'session_created');

  final SessionSummary summary;

  factory SessionCreatedEvent.fromJson(Map<String, dynamic> json) =>
      SessionCreatedEvent(
        timestamp: _readTimestamp(json),
        sessionId: (json['sessionId'] ?? '').toString(),
        runtimeMeta: RuntimeMeta.fromJson(json),
        raw: json,
        summary: SessionSummary.fromJson(
            (json['summary'] as Map<String, dynamic>?) ?? {}),
      );
}

class SessionListResultEvent extends AppEvent {
  const SessionListResultEvent({
    required super.timestamp,
    required super.sessionId,
    required super.runtimeMeta,
    required super.raw,
    this.items = const [],
  }) : super(type: 'session_list_result');

  final List<SessionSummary> items;

  factory SessionListResultEvent.fromJson(Map<String, dynamic> json) =>
      SessionListResultEvent(
        timestamp: _readTimestamp(json),
        sessionId: (json['sessionId'] ?? '').toString(),
        runtimeMeta: RuntimeMeta.fromJson(json),
        raw: json,
        items: ((json['items'] as List?) ?? const [])
            .whereType<Map<String, dynamic>>()
            .map(SessionSummary.fromJson)
            .toList(),
      );
}

class SessionHistoryEvent extends AppEvent {
  const SessionHistoryEvent({
    required super.timestamp,
    required super.sessionId,
    required super.runtimeMeta,
    required super.raw,
    required this.summary,
    this.logEntries = const [],
    this.diffs = const [],
    this.currentDiff,
    this.reviewGroups = const [],
    this.activeReviewGroup,
    this.currentStep,
    this.latestError,
    this.sessionContext = const SessionContext(),
    this.skillCatalogMeta = const CatalogMetadata(domain: 'skill'),
    this.memoryCatalogMeta = const CatalogMetadata(domain: 'memory'),
    this.rawTerminalByStream = const {},
    this.terminalExecutions = const [],
    this.canResume = false,
    this.resumeRuntimeMeta = const RuntimeMeta(),
  }) : super(type: 'session_history');

  final SessionSummary summary;
  final List<HistoryLogEntry> logEntries;
  final List<HistoryContext> diffs;
  final HistoryContext? currentDiff;
  final List<ReviewGroup> reviewGroups;
  final ReviewGroup? activeReviewGroup;
  final HistoryContext? currentStep;
  final HistoryContext? latestError;
  final SessionContext sessionContext;
  final CatalogMetadata skillCatalogMeta;
  final CatalogMetadata memoryCatalogMeta;
  final Map<String, String> rawTerminalByStream;
  final List<TerminalExecution> terminalExecutions;
  final bool canResume;
  final RuntimeMeta resumeRuntimeMeta;

  factory SessionHistoryEvent.fromJson(Map<String, dynamic> json) =>
      SessionHistoryEvent(
        timestamp: _readTimestamp(json),
        sessionId: (json['sessionId'] ?? '').toString(),
        runtimeMeta: RuntimeMeta.fromJson(json),
        raw: json,
        summary: SessionSummary.fromJson(
            (json['summary'] as Map<String, dynamic>?) ?? {}),
        logEntries: ((json['logEntries'] as List?) ?? const [])
            .whereType<Map<String, dynamic>>()
            .map(HistoryLogEntry.fromJson)
            .toList(),
        diffs: ((json['diffs'] as List?) ?? const [])
            .whereType<Map<String, dynamic>>()
            .map(HistoryContext.fromJson)
            .toList(),
        currentDiff: json['currentDiff'] is Map<String, dynamic>
            ? HistoryContext.fromJson(
                json['currentDiff'] as Map<String, dynamic>)
            : null,
        reviewGroups: ((json['reviewGroups'] as List?) ?? const [])
            .whereType<Map<String, dynamic>>()
            .map(ReviewGroup.fromJson)
            .toList(),
        activeReviewGroup: json['activeReviewGroup'] is Map<String, dynamic>
            ? ReviewGroup.fromJson(
                json['activeReviewGroup'] as Map<String, dynamic>)
            : null,
        currentStep: json['currentStep'] is Map<String, dynamic>
            ? HistoryContext.fromJson(
                json['currentStep'] as Map<String, dynamic>)
            : null,
        latestError: json['latestError'] is Map<String, dynamic>
            ? HistoryContext.fromJson(
                json['latestError'] as Map<String, dynamic>)
            : null,
        sessionContext: json['sessionContext'] is Map<String, dynamic>
            ? SessionContext.fromJson(
                json['sessionContext'] as Map<String, dynamic>)
            : const SessionContext(),
        skillCatalogMeta: json['skillCatalogMeta'] is Map<String, dynamic>
            ? CatalogMetadata.fromJson(
                json['skillCatalogMeta'] as Map<String, dynamic>)
            : const CatalogMetadata(domain: 'skill'),
        memoryCatalogMeta: json['memoryCatalogMeta'] is Map<String, dynamic>
            ? CatalogMetadata.fromJson(
                json['memoryCatalogMeta'] as Map<String, dynamic>)
            : const CatalogMetadata(domain: 'memory'),
        rawTerminalByStream: ((json['rawTerminalByStream'] as Map?) ?? const {})
            .map((key, value) => MapEntry(key.toString(), value.toString())),
        terminalExecutions: ((json['terminalExecutions'] as List?) ?? const [])
            .whereType<Map<String, dynamic>>()
            .map(TerminalExecution.fromJson)
            .toList(),
        canResume: json['canResume'] == true,
        resumeRuntimeMeta: json['resumeRuntimeMeta'] is Map<String, dynamic>
            ? RuntimeMeta.fromJson(
                json['resumeRuntimeMeta'] as Map<String, dynamic>)
            : const RuntimeMeta(),
      );
}

class ReviewStateEvent extends AppEvent {
  const ReviewStateEvent({
    required super.timestamp,
    required super.sessionId,
    required super.runtimeMeta,
    required super.raw,
    this.groups = const [],
    this.activeGroup,
  }) : super(type: 'review_state');

  final List<ReviewGroup> groups;
  final ReviewGroup? activeGroup;

  factory ReviewStateEvent.fromJson(Map<String, dynamic> json) =>
      ReviewStateEvent(
        timestamp: _readTimestamp(json),
        sessionId: (json['sessionId'] ?? '').toString(),
        runtimeMeta: RuntimeMeta.fromJson(json),
        raw: json,
        groups: ((json['groups'] as List?) ?? const [])
            .whereType<Map<String, dynamic>>()
            .map(ReviewGroup.fromJson)
            .toList(),
        activeGroup: json['activeGroup'] is Map<String, dynamic>
            ? ReviewGroup.fromJson(json['activeGroup'] as Map<String, dynamic>)
            : null,
      );
}

class SkillCatalogResultEvent extends AppEvent {
  const SkillCatalogResultEvent({
    required super.timestamp,
    required super.sessionId,
    required super.runtimeMeta,
    required super.raw,
    this.meta = const CatalogMetadata(),
    this.items = const [],
  }) : super(type: 'skill_catalog_result');

  final CatalogMetadata meta;
  final List<SkillDefinition> items;

  factory SkillCatalogResultEvent.fromJson(Map<String, dynamic> json) =>
      SkillCatalogResultEvent(
        timestamp: _readTimestamp(json),
        sessionId: (json['sessionId'] ?? '').toString(),
        runtimeMeta: RuntimeMeta.fromJson(json),
        raw: json,
        meta: json['meta'] is Map<String, dynamic>
            ? CatalogMetadata.fromJson(json['meta'] as Map<String, dynamic>)
            : const CatalogMetadata(),
        items: ((json['items'] as List?) ?? const [])
            .whereType<Map<String, dynamic>>()
            .map(SkillDefinition.fromJson)
            .toList(),
      );
}

class MemoryListResultEvent extends AppEvent {
  const MemoryListResultEvent({
    required super.timestamp,
    required super.sessionId,
    required super.runtimeMeta,
    required super.raw,
    this.meta = const CatalogMetadata(),
    this.items = const [],
  }) : super(type: 'memory_list_result');

  final CatalogMetadata meta;
  final List<MemoryItem> items;

  factory MemoryListResultEvent.fromJson(Map<String, dynamic> json) =>
      MemoryListResultEvent(
        timestamp: _readTimestamp(json),
        sessionId: (json['sessionId'] ?? '').toString(),
        runtimeMeta: RuntimeMeta.fromJson(json),
        raw: json,
        meta: json['meta'] is Map<String, dynamic>
            ? CatalogMetadata.fromJson(json['meta'] as Map<String, dynamic>)
            : const CatalogMetadata(),
        items: ((json['items'] as List?) ?? const [])
            .whereType<Map<String, dynamic>>()
            .map(MemoryItem.fromJson)
            .toList(),
      );
}

class SessionContextResultEvent extends AppEvent {
  const SessionContextResultEvent({
    required super.timestamp,
    required super.sessionId,
    required super.runtimeMeta,
    required super.raw,
    this.sessionContext = const SessionContext(),
  }) : super(type: 'session_context_result');

  final SessionContext sessionContext;

  factory SessionContextResultEvent.fromJson(Map<String, dynamic> json) =>
      SessionContextResultEvent(
        timestamp: _readTimestamp(json),
        sessionId: (json['sessionId'] ?? '').toString(),
        runtimeMeta: RuntimeMeta.fromJson(json),
        raw: json,
        sessionContext: json['sessionContext'] is Map<String, dynamic>
            ? SessionContext.fromJson(
                json['sessionContext'] as Map<String, dynamic>)
            : const SessionContext(),
      );
}

class SkillSyncResultEvent extends AppEvent {
  const SkillSyncResultEvent({
    required super.timestamp,
    required super.sessionId,
    required super.runtimeMeta,
    required super.raw,
    this.message = '',
  }) : super(type: 'skill_sync_result');

  final String message;

  factory SkillSyncResultEvent.fromJson(Map<String, dynamic> json) =>
      SkillSyncResultEvent(
        timestamp: _readTimestamp(json),
        sessionId: (json['sessionId'] ?? '').toString(),
        runtimeMeta: RuntimeMeta.fromJson(json),
        raw: json,
        message: (json['msg'] ?? '').toString(),
      );
}

class CatalogSyncStatusEvent extends AppEvent {
  const CatalogSyncStatusEvent({
    required super.timestamp,
    required super.sessionId,
    required super.runtimeMeta,
    required super.raw,
    this.domain = '',
    this.meta = const CatalogMetadata(),
  }) : super(type: 'catalog_sync_status');

  final String domain;
  final CatalogMetadata meta;

  factory CatalogSyncStatusEvent.fromJson(Map<String, dynamic> json) =>
      CatalogSyncStatusEvent(
        timestamp: _readTimestamp(json),
        sessionId: (json['sessionId'] ?? '').toString(),
        runtimeMeta: RuntimeMeta.fromJson(json),
        raw: json,
        domain: (json['domain'] ?? '').toString(),
        meta: json['meta'] is Map<String, dynamic>
            ? CatalogMetadata.fromJson(json['meta'] as Map<String, dynamic>)
            : const CatalogMetadata(),
      );
}

class CatalogSyncResultEvent extends AppEvent {
  const CatalogSyncResultEvent({
    required super.timestamp,
    required super.sessionId,
    required super.runtimeMeta,
    required super.raw,
    this.domain = '',
    this.meta = const CatalogMetadata(),
    this.success = false,
    this.message = '',
  }) : super(type: 'catalog_sync_result');

  final String domain;
  final CatalogMetadata meta;
  final bool success;
  final String message;

  factory CatalogSyncResultEvent.fromJson(Map<String, dynamic> json) =>
      CatalogSyncResultEvent(
        timestamp: _readTimestamp(json),
        sessionId: (json['sessionId'] ?? '').toString(),
        runtimeMeta: RuntimeMeta.fromJson(json),
        raw: json,
        domain: (json['domain'] ?? '').toString(),
        meta: json['meta'] is Map<String, dynamic>
            ? CatalogMetadata.fromJson(json['meta'] as Map<String, dynamic>)
            : const CatalogMetadata(),
        success: json['success'] == true,
        message: (json['msg'] ?? '').toString(),
      );
}

class AdbDevicesResultEvent extends AppEvent {
  const AdbDevicesResultEvent({
    required super.timestamp,
    required super.sessionId,
    required super.runtimeMeta,
    required super.raw,
    this.devices = const [],
    this.selectedSerial = '',
    this.availableAvds = const [],
    this.preferredAvd = '',
    this.adbAvailable = false,
    this.emulatorAvailable = false,
    this.suggestedAction = '',
    this.message = '',
  }) : super(type: 'adb_devices_result');

  final List<AdbDevice> devices;
  final String selectedSerial;
  final List<String> availableAvds;
  final String preferredAvd;
  final bool adbAvailable;
  final bool emulatorAvailable;
  final String suggestedAction;
  final String message;

  factory AdbDevicesResultEvent.fromJson(Map<String, dynamic> json) =>
      AdbDevicesResultEvent(
        timestamp: _readTimestamp(json),
        sessionId: (json['sessionId'] ?? '').toString(),
        runtimeMeta: RuntimeMeta.fromJson(json),
        raw: json,
        devices: ((json['devices'] as List?) ?? const [])
            .whereType<Map<String, dynamic>>()
            .map(AdbDevice.fromJson)
            .toList(),
        selectedSerial: (json['selectedSerial'] ?? '').toString(),
        availableAvds:
            ((json['availableAvds'] as List?) ?? const []).map((item) {
          return item.toString();
        }).toList(),
        preferredAvd: (json['preferredAvd'] ?? '').toString(),
        adbAvailable: json['adbAvailable'] == true,
        emulatorAvailable: json['emulatorAvailable'] == true,
        suggestedAction: (json['suggestedAction'] ?? '').toString(),
        message: (json['msg'] ?? '').toString(),
      );
}

class AdbStreamStateEvent extends AppEvent {
  const AdbStreamStateEvent({
    required super.timestamp,
    required super.sessionId,
    required super.runtimeMeta,
    required super.raw,
    this.running = false,
    this.serial = '',
    this.width = 0,
    this.height = 0,
    this.intervalMs = 0,
    this.message = '',
  }) : super(type: 'adb_stream_state');

  final bool running;
  final String serial;
  final int width;
  final int height;
  final int intervalMs;
  final String message;

  factory AdbStreamStateEvent.fromJson(Map<String, dynamic> json) =>
      AdbStreamStateEvent(
        timestamp: _readTimestamp(json),
        sessionId: (json['sessionId'] ?? '').toString(),
        runtimeMeta: RuntimeMeta.fromJson(json),
        raw: json,
        running: json['running'] == true,
        serial: (json['serial'] ?? '').toString(),
        width: (json['width'] as num?)?.toInt() ?? 0,
        height: (json['height'] as num?)?.toInt() ?? 0,
        intervalMs: (json['intervalMs'] as num?)?.toInt() ?? 0,
        message: (json['msg'] ?? '').toString(),
      );
}

class AdbFrameEvent extends AppEvent {
  const AdbFrameEvent({
    required super.timestamp,
    required super.sessionId,
    required super.runtimeMeta,
    required super.raw,
    this.serial = '',
    this.format = '',
    this.width = 0,
    this.height = 0,
    this.seq = 0,
    this.image = '',
  }) : super(type: 'adb_frame');

  final String serial;
  final String format;
  final int width;
  final int height;
  final int seq;
  final String image;

  factory AdbFrameEvent.fromJson(Map<String, dynamic> json) => AdbFrameEvent(
        timestamp: _readTimestamp(json),
        sessionId: (json['sessionId'] ?? '').toString(),
        runtimeMeta: RuntimeMeta.fromJson(json),
        raw: json,
        serial: (json['serial'] ?? '').toString(),
        format: (json['format'] ?? '').toString(),
        width: (json['width'] as num?)?.toInt() ?? 0,
        height: (json['height'] as num?)?.toInt() ?? 0,
        seq: (json['seq'] as num?)?.toInt() ?? 0,
        image: (json['image'] ?? '').toString(),
      );
}

@immutable
class TimelineItem {
  const TimelineItem({
    required this.id,
    required this.kind,
    required this.timestamp,
    this.title = '',
    this.body = '',
    this.stream = '',
    this.status = '',
    this.meta = const RuntimeMeta(),
    this.context,
  });

  final String id;
  final String kind;
  final DateTime timestamp;
  final String title;
  final String body;
  final String stream;
  final String status;
  final RuntimeMeta meta;
  final HistoryContext? context;
}
