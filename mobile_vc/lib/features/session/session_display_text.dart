import '../../data/models/runtime_meta.dart';
import '../../data/models/session_models.dart';
import 'claude_model_utils.dart';

String sessionDisplayTitle(SessionSummary item) {
  final runtime = _displayRuntime(item);
  final candidates = <String?>[
    _visibleSessionTitle(item.title),
    _visibleSessionText(runtime.contextTitle),
    _visibleSessionText(runtime.targetTitle),
    _visibleSessionText(runtime.command),
    _basename(runtime.targetPath),
  ];
  for (final candidate in candidates) {
    if (candidate != null && candidate.isNotEmpty) {
      return candidate;
    }
  }
  final engine = _sessionEngineLabel(runtime);
  if (engine.isNotEmpty) {
    return '$engine 会话';
  }
  return item.id;
}

String sessionDisplayPreview(SessionSummary item) {
  final runtime = _displayRuntime(item);
  final candidates = <String?>[
    _visibleSessionPreview(item.lastPreview),
    _visibleSessionPreview(runtime.contextTitle),
    _visibleSessionPreview(runtime.targetTitle),
    _visibleSessionTitle(item.title),
    _visibleSessionPreview(runtime.command),
    _basename(runtime.targetPath),
  ];
  for (final candidate in candidates) {
    if (candidate != null && candidate.isNotEmpty) {
      return candidate;
    }
  }
  final title = sessionDisplayTitle(item);
  return title == item.id ? '' : title;
}

String sessionDisplaySubtitle(SessionSummary item) {
  final runtime = _displayRuntime(item);
  final title = sessionDisplayTitle(item);
  final candidates = <String?>[
    _visibleSessionText(item.lastPreview),
    _visibleSessionText(runtime.targetTitle),
    _visibleSessionText(runtime.contextTitle),
    _visibleSessionText(runtime.command),
    _basename(runtime.targetPath),
  ];
  for (final candidate in candidates) {
    if (candidate != null && candidate.isNotEmpty && candidate != title) {
      return candidate;
    }
  }
  final runtimeSummary = sessionRuntimeSummary(runtime);
  if (runtimeSummary.isNotEmpty && runtimeSummary != title) {
    return runtimeSummary;
  }
  final cwd = _basename(runtime.cwd);
  if (cwd.isNotEmpty && cwd != title) {
    return cwd;
  }
  return item.id == title ? '' : item.id;
}

String sessionRuntimeSummary(RuntimeMeta runtime) {
  final engine = _sessionEngineLabel(runtime);
  final model = _sessionModelLabel(runtime);
  final effort = _sessionReasoningEffortLabel(runtime);
  final parts = <String>[
    if (engine.isNotEmpty) engine,
    if (model.isNotEmpty) model,
    if (effort.isNotEmpty) effort,
  ];
  return parts.join(' · ');
}

bool looksLikeSessionNoiseText(String text) {
  final lower = _normalizeWhitespace(text).toLowerCase();
  if (lower.isEmpty) {
    return true;
  }
  return lower == 'ok' ||
      lower == 'done' ||
      lower == 'running' ||
      lower == 'thinking' ||
      lower == 'processing' ||
      lower == 'active' ||
      lower == 'ready' ||
      lower == 'idle' ||
      lower == 'is ready' ||
      lower == '已就绪' ||
      lower == 'session active' ||
      lower == 'session ready' ||
      lower == 'command started' ||
      lower.startsWith('command started ') ||
      lower == 'command finished' ||
      lower.startsWith('command finished ') ||
      lower == 'status: active' ||
      lower == 'status: ready' ||
      lower == 'status: idle' ||
      lower.startsWith('active:') ||
      lower.startsWith('ready:') ||
      lower.startsWith('idle:') ||
      lower.startsWith('--config model_reasoning_effort=') ||
      lower.startsWith('model_reasoning_effort=') ||
      _looksLikeSessionTimestamp(lower) ||
      _looksLikeSessionModelSummary(lower);
}

bool looksLikeSessionPlaceholderTitle(String text) {
  final normalized = _normalizeWhitespace(text).toLowerCase();
  if (normalized.isEmpty) {
    return true;
  }
  return normalized == 'session' ||
      normalized == 'new session' ||
      normalized == 'history' ||
      RegExp(r'^session(?:[-_\s][a-z0-9]+)?$').hasMatch(normalized);
}

bool looksLikeSessionBootstrapCommand(String text) {
  final normalized = _normalizeWhitespace(text);
  if (normalized.isEmpty) {
    return false;
  }
  final lower = normalized.toLowerCase();
  if (lower.startsWith('--config ') ||
      lower.startsWith('--model ') ||
      lower.startsWith('-m ')) {
    return true;
  }
  final startsWithAiCommand = lower == 'claude' ||
      lower.startsWith('claude ') ||
      lower == 'codex' ||
      lower.startsWith('codex ') ||
      lower == 'gemini' ||
      lower.startsWith('gemini ');
  if (!startsWithAiCommand) {
    return false;
  }
  if (!normalized.contains(' ')) {
    return true;
  }
  return lower.contains(' --model ') ||
      lower.contains(' -m ') ||
      lower.contains(' --config ') ||
      lower.contains(' --permission-mode ') ||
      lower.contains(' --approval-mode ') ||
      lower.contains(' --dangerously-skip-permissions');
}

String? _visibleSessionText(String text) {
  final normalized = _normalizeWhitespace(text);
  if (normalized.isEmpty) {
    return null;
  }
  if (looksLikeSessionNoiseText(normalized) ||
      looksLikeSessionBootstrapCommand(normalized)) {
    return null;
  }
  return normalized;
}

String? _visibleSessionTitle(String text) {
  final normalized = _visibleSessionText(text);
  if (normalized == null || looksLikeSessionPlaceholderTitle(normalized)) {
    return null;
  }
  return normalized;
}

String? _visibleSessionPreview(String text) {
  final normalized = _normalizeWhitespace(text);
  if (normalized.isEmpty) {
    return null;
  }
  if (looksLikeSessionNoiseText(normalized) ||
      looksLikeSessionBootstrapCommand(normalized) ||
      looksLikeSessionPlaceholderTitle(normalized)) {
    return null;
  }
  return normalized;
}

String _sessionEngineLabel(RuntimeMeta runtime) {
  final engine = runtime.engine.trim().toLowerCase();
  if (engine == 'claude') {
    return 'Claude';
  }
  if (engine == 'codex') {
    return 'Codex';
  }
  if (engine == 'gemini') {
    return 'Gemini';
  }
  final command = runtime.command.trim().toLowerCase();
  if (command == 'claude' || command.startsWith('claude ')) {
    return 'Claude';
  }
  if (command == 'codex' || command.startsWith('codex ')) {
    return 'Codex';
  }
  if (command == 'gemini' || command.startsWith('gemini ')) {
    return 'Gemini';
  }
  return '';
}

String _sessionModelLabel(RuntimeMeta runtime) {
  final explicit = runtime.model.trim();
  final engine = _sessionEngineLabel(runtime).toLowerCase();
  if (explicit.isNotEmpty) {
    return engine == 'claude' ? claudeModelDisplayLabel(explicit) : explicit;
  }
  final command = _normalizeWhitespace(runtime.command);
  final codexMatch = RegExp(r'(?:^|\s)-m\s+([^\s]+)').firstMatch(command);
  if (codexMatch != null) {
    return codexMatch.group(1) ?? '';
  }
  final claudeMatch = RegExp(r'(?:^|\s)--model\s+([^\s]+)').firstMatch(command);
  if (claudeMatch != null) {
    return claudeModelDisplayLabel(claudeMatch.group(1) ?? '');
  }
  return '';
}

String _sessionReasoningEffortLabel(RuntimeMeta runtime) {
  final explicit = runtime.reasoningEffort.trim();
  if (explicit.isNotEmpty) {
    return explicit;
  }
  final command = _normalizeWhitespace(runtime.command);
  final match = RegExp(r'model_reasoning_effort=([^\s]+)').firstMatch(command);
  if (match == null) {
    return '';
  }
  return match.group(1) ?? '';
}

RuntimeMeta _displayRuntime(SessionSummary item) {
  final title = _normalizeWhitespace(item.title);
  if (item.runtime.command.trim().isNotEmpty ||
      !looksLikeSessionBootstrapCommand(title)) {
    return item.runtime;
  }
  return item.runtime.merge(RuntimeMeta(command: title));
}

String _basename(String path) {
  final normalized = path.trim();
  if (normalized.isEmpty) {
    return '';
  }
  final segments = normalized.split(RegExp(r'[\\/]'));
  for (final segment in segments.reversed) {
    final trimmed = segment.trim();
    if (trimmed.isNotEmpty) {
      return trimmed;
    }
  }
  return '';
}

String _normalizeWhitespace(String text) {
  return text.replaceAll(RegExp(r'\s+'), ' ').trim();
}

bool _looksLikeSessionTimestamp(String text) {
  return RegExp(
    r'^(?:\d{4}[-/]\d{1,2}[-/]\d{1,2})(?:[ t]\d{1,2}:\d{2}(?::\d{2})?)?(?:z|[+-]\d{2}:?\d{2})?$',
    caseSensitive: false,
  ).hasMatch(text);
}

bool _looksLikeSessionModelSummary(String text) {
  final normalized = text.trim().toLowerCase();
  if (normalized.isEmpty) {
    return false;
  }
  final hasEngine = normalized.startsWith('codex ') ||
      normalized == 'codex' ||
      normalized.startsWith('claude ') ||
      normalized == 'claude' ||
      normalized.startsWith('gemini ') ||
      normalized == 'gemini';
  if (!hasEngine) {
    return false;
  }
  return normalized.contains('gpt-') ||
      normalized.contains('default') ||
      normalized.contains('sonnet') ||
      normalized.contains('opus') ||
      normalized.contains('haiku') ||
      normalized.contains('opusplan') ||
      normalized.contains('[1m]') ||
      normalized.contains('gemini-') ||
      normalized.endsWith('-low') ||
      normalized.endsWith('-medium') ||
      normalized.endsWith('-high') ||
      normalized.contains(' model ');
}
