import 'dart:async';
import 'dart:convert';

import 'package:flutter/foundation.dart';
import 'package:shared_preferences/shared_preferences.dart';

import '../../core/config/app_config.dart';
import '../../data/models/events.dart';
import '../../data/models/runtime_meta.dart';
import '../../data/models/session_models.dart';
import '../../data/services/mobilevc_ws_service.dart';

const Set<String> _approvePromptValues = {
  'y',
  'yes',
  'ok',
  'approve',
  'approved',
  'allow',
  'accept',
  'continue',
  'allow once',
  'allow this time',
  '允许',
  '同意',
  '批准',
  '确认',
  '继续',
};

const Set<String> _denyPromptValues = {
  'n',
  'no',
  'deny',
  'denied',
  'reject',
  'cancel',
  'stop',
  '拒绝',
  '不同意',
  '取消',
  '停止',
};

enum ActionNeededType {
  continueInput,
  permission,
  review,
  reply,
}

class ActionNeededSignal {
  const ActionNeededSignal({
    required this.id,
    required this.type,
    required this.message,
    required this.createdAt,
  });

  final int id;
  final ActionNeededType type;
  final String message;
  final DateTime createdAt;
}

class _ActionNeededSnapshot {
  const _ActionNeededSnapshot({
    required this.type,
    required this.key,
    required this.message,
  });

  final ActionNeededType type;
  final String key;
  final String message;
}

class SessionController extends ChangeNotifier {
  SessionController({MobileVcWsService? service})
      : _service = service ?? MobileVcWsService();

  static const _prefsKey = 'mobilevc.app_config';
  final MobileVcWsService _service;

  StreamSubscription<AppEvent>? _subscription;
  AppConfig _config = const AppConfig();
  bool _connecting = false;
  bool _connected = false;
  bool _fileListLoading = false;
  bool _fileReading = false;
  bool _canResumeCurrentSession = false;
  String _connectionMessage = '未连接';
  String _selectedSessionId = '';
  String _selectedSessionTitle = 'MobileVC';
  String _currentDirectoryPath = '';
  String _terminalStdout = '';
  String _terminalStderr = '';
  String _activeTerminalExecutionId = '';
  AgentStateEvent? _agentState;
  SessionStateEvent? _sessionState;
  RuntimeInfoResultEvent? _runtimeInfo;
  FileDiffEvent? _currentDiff;
  PromptRequestEvent? _pendingPrompt;
  InteractionRequestEvent? _pendingInteraction;
  HistoryContext? _currentStep;
  HistoryContext? _latestError;
  FileReadResult? _openedFile;
  RuntimeMeta _resumeRuntimeMeta = const RuntimeMeta();
  String _runtimePermissionMode = '';
  SessionContext _sessionContext = const SessionContext();
  CatalogMetadata _skillCatalogMeta = const CatalogMetadata(domain: 'skill');
  CatalogMetadata _memoryCatalogMeta = const CatalogMetadata(domain: 'memory');
  String _skillSyncStatus = '';
  String _memorySyncStatus = '';
  final List<FSItem> _currentDirectoryItems = [];
  final List<HistoryContext> _recentDiffs = [];
  final List<SessionSummary> _sessions = [];
  final List<SkillDefinition> _skills = [];
  final List<MemoryItem> _memoryItems = [];
  final List<TimelineItem> _timeline = [];
  final List<ReviewGroup> _reviewGroups = [];
  final List<TerminalExecution> _terminalExecutions = [];
  String _activeReviewGroupId = '';
  String _activeReviewDiffId = '';
  String _agentPhaseLabel = '未连接';
  bool _activityVisible = false;
  DateTime? _activityStartedAt;
  String _activityToolLabel = '';
  String _currentStepSummary = '';
  String _lastStepMessage = '';
  String _lastStepStatus = '';
  String _lastLogMessage = '';
  String _lastLogStream = '';
  DateTime? _lastLogAt;
  String _lastSessionTimelineKey = '';
  int _nextActionNeededSignalId = 0;
  ActionNeededSignal? _actionNeededSignal;
  _ActionNeededSnapshot? _activeActionNeededSnapshot;
  bool _shouldSuppressNextActionNeededSignal = false;
  bool _autoSessionRequested = false;
  bool _autoSessionCreating = false;
  String _autoSessionLoadingId = '';
  bool _isLoadingSession = false;
  String _pendingSessionTargetId = '';
  final Set<String> _pendingToggleSkillNames = <String>{};
  final Set<String> _pendingToggleMemoryIds = <String>{};
  bool _isSavingSkill = false;
  bool _isSavingMemory = false;
  SessionContext? _pendingSessionContextTarget;
  SessionContext? _pendingSessionContextRollback;

  AppConfig get config => _config;
  bool get connecting => _connecting;
  bool get connected => _connected;
  bool get fileListLoading => _fileListLoading;
  bool get fileReading => _fileReading;
  bool get canResumeCurrentSession => _canResumeCurrentSession;
  String get connectionMessage => _connectionMessage;
  String get selectedSessionId => _selectedSessionId;
  String get selectedSessionTitle => _selectedSessionTitle;
  String get currentDirectoryPath =>
      _currentDirectoryPath.isNotEmpty ? _currentDirectoryPath : _config.cwd;
  String get effectiveCwd => currentDirectoryPath;
  String get terminalStdout => _terminalStdout;
  String get terminalStderr => _terminalStderr;
  List<TerminalExecution> get terminalExecutions =>
      List.unmodifiable(_terminalExecutions);
  String get activeTerminalExecutionId => _activeTerminalExecutionId;
  TerminalExecution? get activeTerminalExecution =>
      _resolvedActiveTerminalExecution();
  String get activeTerminalStdout =>
      activeTerminalExecution?.stdout ?? _terminalStdout;
  String get activeTerminalStderr =>
      activeTerminalExecution?.stderr ?? _terminalStderr;
  String get terminalExecutionSummary {
    final count = _terminalExecutions.length;
    if (count == 0) {
      return '';
    }
    final running = _terminalExecutions.where((item) => item.running).length;
    return running > 0 ? '$count 条命令，$running 条运行中' : '$count 条命令';
  }

  String get terminalLogs {
    if (_terminalStdout.isEmpty) {
      return _terminalStderr;
    }
    if (_terminalStderr.isEmpty) {
      return _terminalStdout;
    }
    return '$_terminalStdout\n\n$_terminalStderr';
  }

  AgentStateEvent? get agentState => _agentState;
  SessionStateEvent? get sessionState => _sessionState;
  RuntimeInfoResultEvent? get runtimeInfo => _runtimeInfo;
  FileDiffEvent? get currentDiff => _currentDiff;
  PromptRequestEvent? get pendingPrompt {
    final prompt = _pendingPrompt;
    if (prompt == null || !prompt.hasVisiblePrompt) {
      return null;
    }
    if (_shouldHidePromptCard(prompt)) {
      return null;
    }
    return prompt;
  }

  InteractionRequestEvent? get pendingInteraction {
    final interaction = _pendingInteraction;
    if (interaction == null || !interaction.hasVisiblePrompt) {
      return null;
    }
    return interaction;
  }

  HistoryContext? get currentStep => _currentStep;
  HistoryContext? get latestError => _latestError;
  FileReadResult? get openedFile => _openedFile;
  List<FSItem> get currentDirectoryItems =>
      List.unmodifiable(_currentDirectoryItems);
  List<HistoryContext> get recentDiffs => List.unmodifiable(_recentDiffs);
  List<SessionSummary> get sessions => List.unmodifiable(_sessions);
  List<SkillDefinition> get skills => List.unmodifiable(_skills);
  List<MemoryItem> get memoryItems => List.unmodifiable(_memoryItems);
  SessionContext get sessionContext => _sessionContext;
  CatalogMetadata get skillCatalogMeta => _skillCatalogMeta;
  CatalogMetadata get memoryCatalogMeta => _memoryCatalogMeta;
  String get skillSyncStatus => _skillSyncStatus;
  String get memorySyncStatus => _memorySyncStatus;
  Set<String> get pendingToggleSkillNames =>
      Set.unmodifiable(_pendingToggleSkillNames);
  Set<String> get pendingToggleMemoryIds =>
      Set.unmodifiable(_pendingToggleMemoryIds);
  bool get isSavingSkill => _isSavingSkill;
  bool get isSavingMemory => _isSavingMemory;
  bool isSkillTogglePending(String name) =>
      _pendingToggleSkillNames.contains(name.trim());
  bool isMemoryTogglePending(String id) =>
      _pendingToggleMemoryIds.contains(id.trim());
  String get enabledSkillSummary {
    final names = _sessionContext.enabledSkillNames;
    if (names.isEmpty) {
      return '';
    }
    return '${names.length} 个：${names.join('、')}';
  }

  String get enabledMemorySummary {
    final ids = _sessionContext.enabledMemoryIds;
    if (ids.isEmpty) {
      return '';
    }
    final titles = ids
        .map((id) => _memoryItems
            .cast<MemoryItem?>()
            .firstWhere((item) => item?.id == id, orElse: () => null)
            ?.title
            .trim())
        .map((title) => (title == null || title.isEmpty) ? null : title)
        .whereType<String>()
        .toList();
    final summaryItems = titles.isNotEmpty ? titles : ids;
    return '${ids.length} 个：${summaryItems.join('、')}';
  }

  bool get hasPendingPermissionPrompt =>
      pendingInteraction?.isPermission == true ||
      pendingPrompt?.looksLikePermissionPrompt == true;
  bool get hasVisiblePrompt =>
      pendingInteraction != null || pendingPrompt != null;
  bool get shouldShowPromptComposer =>
      hasVisiblePrompt &&
      !shouldShowReviewChoices &&
      !hasPendingPermissionPrompt;
  bool get shouldShowPermissionChoices =>
      hasPendingPermissionPrompt && !shouldShowReviewChoices;
  bool get hasCompactContextSelection =>
      _skills.isNotEmpty || _memoryItems.isNotEmpty;
  bool get isLoadingSession => _isLoadingSession;

  List<TimelineItem> get timeline => List.unmodifiable(_timeline);
  List<ReviewGroup> get reviewGroups => List.unmodifiable(_reviewGroups);
  ReviewGroup? get activeReviewGroup => _resolvedActiveReviewGroup();
  String get activeReviewGroupId => _activeReviewGroupId;
  bool get awaitInput {
    if (_isLoadingSession) {
      return false;
    }
    return _agentState?.awaitInput == true ||
        pendingPrompt != null ||
        pendingInteraction != null;
  }

  ActionNeededSignal? get actionNeededSignal => _actionNeededSignal;
  bool get fastMode => _config.fastMode;
  String get displayPermissionMode => _runtimePermissionMode.isNotEmpty
      ? _runtimePermissionMode
      : _config.permissionMode;
  bool get hasPendingReview => pendingDiffCount > 0;
  int get pendingDiffCount => _pendingDiffs.length;
  int get pendingReviewGroupCount =>
      _reviewGroups.where((group) => group.pendingCount > 0).length;
  List<HistoryContext> get diffItems => List.unmodifiable(_recentDiffs);
  List<HistoryContext> get pendingDiffs => List.unmodifiable(_pendingDiffs);
  String get activeReviewDiffId => _activeReviewDiffId;
  HistoryContext? get currentDiffContext => _resolvedCurrentDiff();
  HistoryContext? get currentReviewDiff => _currentReviewDiff();
  HistoryContext? get nextPendingDiff => _nextPendingDiff();
  HistoryContext? get openedFileDiff => _diffForOpenedFile();
  HistoryContext? get openedFilePendingDiff => _pendingDiffForOpenedFile();
  bool get openedFileMatchesPendingDiff => openedFilePendingDiff != null;
  bool get isAutoAcceptMode => displayPermissionMode == 'acceptEdits';
  bool get isBypassPermissionsMode =>
      displayPermissionMode == 'bypassPermissions';
  bool get isManualReviewMode => !isAutoAcceptMode;
  bool get shouldShowReviewChoices {
    if (!isManualReviewMode) {
      return false;
    }
    final state = (_agentState?.state ?? '').trim().toUpperCase();
    final interaction = pendingInteraction;
    if (interaction?.isReview == true) {
      return currentReviewDiff != null && state == 'WAIT_INPUT';
    }
    return currentReviewDiff != null &&
        _isReviewDecisionPrompt(_pendingPrompt) &&
        state == 'WAIT_INPUT';
  }

  String _debugReviewStateSummary() {
    final prompt = _pendingPrompt;
    final currentReview = currentReviewDiff;
    final openedPending = openedFilePendingDiff;
    return 'awaitInput=$awaitInput, agentState=${_agentState?.state ?? '-'}, pendingPrompt=${prompt?.message.trim().isNotEmpty == true ? prompt!.message.trim() : '-'}, shouldShowReviewChoices=$shouldShowReviewChoices, currentReviewDiff=${currentReview?.path.isNotEmpty == true ? currentReview!.path : '-'}, openedFilePendingDiff=${openedPending?.path.isNotEmpty == true ? openedPending!.path : '-'}, openedFile=${_openedFile?.path.isNotEmpty == true ? _openedFile!.path : '-'}';
  }

  void _pushDebug(String label, [String? details]) {}

  void setActiveReviewGroup(String groupId) {
    final normalized = groupId.trim();
    if (normalized.isEmpty) {
      if (_activeReviewGroupId.isEmpty) {
        return;
      }
      _activeReviewGroupId = '';
      _syncActiveReviewSelection();
      _syncDerivedState();
      notifyListeners();
      return;
    }
    final group = _findReviewGroupById(normalized);
    if (group == null) {
      return;
    }
    if (_activeReviewGroupId == group.id) {
      return;
    }
    _activeReviewGroupId = group.id;
    _syncActiveReviewSelection();
    _syncDerivedState();
    notifyListeners();
  }

  void setActiveReviewDiff(String diffId) {
    final normalized = diffId.trim();
    if (normalized.isEmpty) {
      if (_activeReviewDiffId.isEmpty) {
        return;
      }
      _activeReviewDiffId = '';
      _syncActiveReviewSelection();
      _syncDerivedState();
      notifyListeners();
      return;
    }
    final diff = _findPendingDiffById(normalized);
    if (diff == null) {
      return;
    }
    final nextId = _diffIdentity(diff);
    if (_activeReviewDiffId == nextId) {
      return;
    }
    _activeReviewDiffId = nextId;
    final groupId = _groupIdForDiff(diff);
    if (groupId.isNotEmpty) {
      _activeReviewGroupId = groupId;
    }
    _syncActiveReviewSelection();
    notifyListeners();
  }

  void setActiveTerminalExecution(String executionId) {
    final normalized = executionId.trim();
    if (normalized.isEmpty) {
      if (_activeTerminalExecutionId.isEmpty) {
        return;
      }
      _activeTerminalExecutionId = '';
      notifyListeners();
      return;
    }
    final exists =
        _terminalExecutions.any((item) => item.executionId == normalized);
    if (!exists || _activeTerminalExecutionId == normalized) {
      return;
    }
    _activeTerminalExecutionId = normalized;
    notifyListeners();
  }

  Future<void> acceptAllPendingDiffs() async {
    if (!isManualReviewMode) {
      _pushSystem('session', '当前是自动接受修改模式，无需手动审核 diff');
      return;
    }
    final diffs = List<HistoryContext>.from(_pendingDiffs);
    if (diffs.isEmpty) {
      _pushSystem('error', '当前没有待审核的 diff');
      return;
    }
    final labels = diffs
        .map((item) => item.title.isNotEmpty ? item.title : item.path)
        .where((item) => item.trim().isNotEmpty)
        .toList();
    _pushUser(
      '一键接受并继续\n${labels.take(3).join('\n')}${labels.length > 3 ? '\n…' : ''}',
      '聚合审核',
    );
    for (final diff in diffs) {
      if (!diff.pendingReview) {
        continue;
      }
      _sendReviewDecisionForDiff(
        diff,
        'accept',
        pushTimeline: false,
      );
    }
    _syncDerivedState();
    notifyListeners();
  }

  bool get isSessionBusy {
    if (_isLoadingSession) {
      return true;
    }
    final agentState = (_agentState?.state ?? '').trim().toUpperCase();
    final sessionState = (_sessionState?.state ?? '').trim().toUpperCase();
    if (awaitInput) {
      return false;
    }
    return agentState == 'THINKING' ||
        agentState == 'RUNNING_TOOL' ||
        sessionState == 'THINKING' ||
        sessionState == 'RUNNING_TOOL' ||
        sessionState == 'RUNNING';
  }

  String get agentPhaseLabel => _agentPhaseLabel;
  bool get activityVisible => _activityVisible;
  DateTime? get activityStartedAt => _activityStartedAt;
  int get activityElapsedSeconds {
    final startedAt = _activityStartedAt;
    if (!_activityVisible || startedAt == null) {
      return 0;
    }
    return DateTime.now().difference(startedAt).inSeconds;
  }

  String get activityToolLabel => _activityToolLabel;
  String get currentStepSummary => _currentStepSummary;
  bool get inClaudeMode {
    if (_isLoadingSession) {
      return false;
    }
    final command = currentMeta.command.trim().toLowerCase();
    return command == 'claude' || command.startsWith('claude ');
  }

  RuntimeMeta get currentMeta {
    final merged = (_agentState?.runtimeMeta ?? const RuntimeMeta())
        .merge(_sessionState?.runtimeMeta ?? const RuntimeMeta())
        .merge(_currentDiff?.runtimeMeta ?? const RuntimeMeta())
        .merge(_runtimeInfo?.runtimeMeta ?? const RuntimeMeta())
        .merge(_resumeRuntimeMeta);
    return merged.merge(
      RuntimeMeta(
        engine: _config.engine,
        cwd: effectiveCwd,
        permissionMode: displayPermissionMode,
        targetDiff: _currentDiff?.diff ?? merged.targetDiff,
        targetPath:
            _openedFile?.path ?? _currentDiff?.path ?? merged.targetPath,
        targetTitle:
            _openedFile?.title ?? _currentDiff?.title ?? merged.targetTitle,
        targetText: _openedFile?.isText == true
            ? _openedFile?.content ?? merged.targetText
            : merged.targetText,
      ),
    );
  }

  String get _currentDecisionPermissionMode {
    final interactionMode =
        pendingInteraction?.runtimeMeta.permissionMode.trim() ?? '';
    if (interactionMode.isNotEmpty) {
      return interactionMode;
    }
    final promptMode = pendingPrompt?.runtimeMeta.permissionMode.trim() ?? '';
    if (promptMode.isNotEmpty) {
      return promptMode;
    }
    return displayPermissionMode;
  }

  Future<void> initialize() async {
    final prefs = await SharedPreferences.getInstance();
    final raw = prefs.getString(_prefsKey);
    if (raw != null && raw.isNotEmpty) {
      final decoded = jsonDecode(raw);
      if (decoded is Map<String, dynamic>) {
        _config = AppConfig.fromJson(decoded);
      }
    }
    _subscription = _service.events.listen(_handleEvent);
    _syncDerivedState();
    notifyListeners();
  }

  Future<void> disposeController() async {
    await _subscription?.cancel();
    await _service.dispose();
  }

  Future<void> saveConfig(AppConfig config) async {
    _config = config;
    if (_currentDirectoryPath.trim().isEmpty ||
        _normalizePath(_currentDirectoryPath) == _normalizePath(config.cwd)) {
      _currentDirectoryPath = config.cwd.trim();
    }
    final prefs = await SharedPreferences.getInstance();
    await prefs.setString(_prefsKey, jsonEncode(config.toJson()));
    notifyListeners();
  }

  Future<void> switchWorkingDirectory(String path,
      {bool refreshList = true}) async {
    final normalized = path.trim();
    final nextPath = normalized.isEmpty ? '.' : normalized;
    final samePath = _normalizePath(effectiveCwd) == _normalizePath(nextPath);
    _currentDirectoryPath = nextPath;
    if (_normalizePath(_config.cwd) != _normalizePath(nextPath)) {
      _config = _config.copyWith(cwd: nextPath);
      final prefs = await SharedPreferences.getInstance();
      await prefs.setString(_prefsKey, jsonEncode(_config.toJson()));
    }
    if (refreshList && (!samePath || _currentDirectoryItems.isEmpty)) {
      _fileListLoading = true;
      _service.send(
          {'action': 'fs_list', if (nextPath.isNotEmpty) 'path': nextPath});
    }
    notifyListeners();
  }

  Future<void> connect() async {
    _connecting = true;
    _connectionMessage = '连接中...';
    _syncDerivedState();
    notifyListeners();
    try {
      await _service.connect(_config.wsUrl);
      _connected = true;
      _connectionMessage = '已连接';
      _autoSessionRequested = false;
      _autoSessionCreating = false;
      _autoSessionLoadingId = '';
      _runtimePermissionMode = '';
      requestSessionList();
      await switchWorkingDirectory(_config.cwd);
      requestRuntimeInfo('context');
      requestSkillCatalog();
      requestMemoryList();
      requestSessionContext();
      requestReviewState();
    } catch (error) {
      _connected = false;
      _connectionMessage = '连接失败：$error';
      _pushSystem('error', _connectionMessage);
    } finally {
      _connecting = false;
      _syncDerivedState();
      notifyListeners();
    }
  }

  Future<void> disconnect() async {
    await _service.disconnect();
    _connected = false;
    _selectedSessionId = '';
    _selectedSessionTitle = 'MobileVC';
    _connectionMessage = '已断开';
    _fileListLoading = false;
    _fileReading = false;
    _currentDirectoryPath = '';
    _currentDirectoryItems.clear();
    _openedFile = null;
    _terminalStdout = '';
    _terminalStderr = '';
    _activeTerminalExecutionId = '';
    _terminalExecutions.clear();
    _canResumeCurrentSession = false;
    _resumeRuntimeMeta = const RuntimeMeta();
    _runtimePermissionMode = '';
    _sessionContext = const SessionContext();
    _skillCatalogMeta = const CatalogMetadata(domain: 'skill');
    _memoryCatalogMeta = const CatalogMetadata(domain: 'memory');
    _skillSyncStatus = '';
    _memorySyncStatus = '';
    _skills.clear();
    _memoryItems.clear();
    _agentState = null;
    _sessionState = null;
    _pendingPrompt = null;
    _pendingInteraction = null;
    _currentStep = null;
    _currentStepSummary = '';
    _activityToolLabel = '';
    _activityStartedAt = null;
    _activityVisible = false;
    _activeReviewDiffId = '';
    _agentPhaseLabel = '未连接';
    _resetActionNeededTracking();
    notifyListeners();
  }

  void requestSessionList() {
    _service.send({'action': 'session_list'});
  }

  void _handleAutoSessionBinding(List<SessionSummary> items) {
    if (!_connected || _connecting || _autoSessionRequested) {
      return;
    }
    if (_selectedSessionId.trim().isNotEmpty) {
      final selectedExists = items.any((item) => item.id == _selectedSessionId);
      if (selectedExists) {
        return;
      }
    }
    if (_autoSessionCreating) {
      return;
    }
    _autoSessionRequested = true;
    _autoSessionCreating = true;
    createSession();
  }

  void createSession([String title = '']) {
    _beginSessionLoading();
    _service.send(
        {'action': 'session_create', if (title.isNotEmpty) 'title': title});
  }

  void loadSession(String sessionId) {
    final targetId = sessionId.trim();
    if (targetId.isEmpty) {
      return;
    }
    _beginSessionLoading(targetId: targetId);
    _service.send({'action': 'session_load', 'sessionId': targetId});
  }

  void deleteSession(String sessionId) {
    final targetId = sessionId.trim();
    if (targetId.isEmpty) {
      return;
    }
    if (targetId == _selectedSessionId) {
      _beginSessionLoading();
    }
    _service.send({'action': 'session_delete', 'sessionId': targetId});
  }

  void _beginSessionLoading({String targetId = ''}) {
    _isLoadingSession = true;
    _pendingSessionTargetId = targetId.trim();
    _pendingPrompt = null;
    _pendingInteraction = null;
    _runtimePermissionMode = '';
    _agentState = null;
    _sessionState = null;
    _currentStep = null;
    _currentStepSummary = '';
    _lastStepMessage = '';
    _lastStepStatus = '';
    _agentPhaseLabel = '切换会话中';
    _activityToolLabel = '';
    _activityStartedAt = null;
    _activityVisible = false;
    _resetActionNeededTracking(suppressNextSignal: true);
    _syncDerivedState();
    notifyListeners();
  }

  bool _matchesPendingSessionTarget(String sessionId) {
    final normalized = sessionId.trim();
    if (normalized.isEmpty) {
      return false;
    }
    final targetId = _pendingSessionTargetId.trim();
    if (targetId.isEmpty) {
      return true;
    }
    return normalized == targetId;
  }

  void _finishSessionLoading({String sessionId = ''}) {
    if (!_isLoadingSession) {
      return;
    }
    final normalized = sessionId.trim();
    if (normalized.isNotEmpty) {
      _pendingSessionTargetId = normalized;
    }
    _isLoadingSession = false;
    _pendingSessionTargetId = '';
  }

  void updatePermissionMode(String permissionMode) {
    _config = _config.copyWith(permissionMode: permissionMode);
    final normalizedDiffs = _recentDiffs.map(_normalizeHistoryDiff).toList();
    _recentDiffs
      ..clear()
      ..addAll(normalizedDiffs);
    _service.send(
        {'action': 'set_permission_mode', 'permissionMode': permissionMode});
    _pushSystem('session',
        'Permission mode 已切换为 ${_permissionModeLabel(permissionMode)}，将对下一次交互生效');
    _syncDerivedState();
    notifyListeners();
  }

  void requestFileList([String? path]) {
    final target = (path ?? effectiveCwd).trim();
    _fileListLoading = true;
    _service.send({'action': 'fs_list', if (target.isNotEmpty) 'path': target});
    notifyListeners();
  }

  Future<void> refreshFileList() async {
    await switchWorkingDirectory(effectiveCwd);
  }

  Future<void> goParentDirectory() async {
    final parent = _parentDirectory(effectiveCwd);
    await switchWorkingDirectory(parent);
  }

  void openFile(String path) {
    requestFileRead(path);
  }

  void requestFileRead(String path) {
    final target = path.trim();
    if (target.isEmpty) {
      return;
    }
    _fileReading = true;
    _service.send({'action': 'fs_read', 'path': target});
    notifyListeners();
  }

  void sendReviewDecision(String decision) {
    final normalized = decision.trim().toLowerCase();
    if (normalized.isEmpty) {
      return;
    }
    _markActionNeededHandled();
    final diff = currentReviewDiff ??
        openedFilePendingDiff ??
        nextPendingDiff ??
        currentDiffContext;
    if (diff == null || diff.diff.isEmpty) {
      _pushSystem('error', '当前没有待审核的 diff');
      return;
    }
    _sendReviewDecisionForDiff(diff, normalized);
  }

  void requestSkillCatalog() {
    _service.send({'action': 'skill_catalog_get'});
  }

  void saveSkill(SkillDefinition definition) {
    final name = definition.name.trim();
    if (name.isEmpty || _isSavingSkill) {
      return;
    }
    final existing = _skills.cast<SkillDefinition?>().firstWhere(
          (item) => item?.name == name,
          orElse: () => null,
        );
    final merged = (existing ?? const SkillDefinition()).copyWith(
      name: name,
      description: definition.description.trim(),
      prompt: definition.prompt.trim(),
      resultView: definition.resultView.trim(),
      targetType: definition.targetType.trim(),
      editable: existing?.editable ?? true,
    );
    _isSavingSkill = true;
    _skillSyncStatus = '正在保存 skill…';
    notifyListeners();
    _service.send({
      'action': 'skill_catalog_upsert',
      'skill': {
        'name': merged.name,
        'description': merged.description,
        'prompt': merged.prompt,
        'resultView': merged.resultView,
        'targetType': merged.targetType,
      },
    });
  }

  void saveGeneratedSkill({
    required String request,
    SkillDefinition? base,
  }) {
    final prompt = buildSkillAuthoringPrompt(request, base: base);
    if (prompt.isEmpty) {
      return;
    }
    final label = base == null ? '生成 Skill' : '修改 Skill';
    _skillSyncStatus = base == null ? '正在生成新 skill…' : '正在修改 skill…';
    notifyListeners();
    _dispatchContextualClaudeRequest(
      prompt,
      label: label,
      targetType: 'skill',
      targetTitle: base?.name ?? 'new skill',
      resultView: 'skill-catalog',
      skillName: base?.name ?? '',
    );
  }

  String buildSkillAuthoringPrompt(String request, {SkillDefinition? base}) {
    final intent = request.trim();
    if (intent.isEmpty) {
      return '';
    }
    final lines = <String>[
      base == null
          ? '请根据下面需求生成一个新的 Claude skill。'
          : '请根据下面需求修改这个 Claude skill。',
      '你必须只返回严格 JSON，不要输出 markdown、解释、代码块标记或额外文字。',
      '返回 JSON 顶层字段必须是：mobilevcCatalogAuthoring、kind、skill。',
      '其中 mobilevcCatalogAuthoring 必须为 true，kind 必须为 "skill"。',
      'skill 对象内必须包含：name、description、prompt、targetType、resultView。',
      '如需更新现有 skill，请沿用原有 name，除非用户明确要求改名。',
      '示例格式：{"mobilevcCatalogAuthoring":true,"kind":"skill","skill":{"name":"example-skill","description":"...","prompt":"...","targetType":"diff","resultView":"review-card"}}',
      if (base != null) ...[
        'CurrentSkillName: ${base.name}',
        if (base.description.trim().isNotEmpty)
          'CurrentDescription: ${base.description.trim()}',
        if (base.targetType.trim().isNotEmpty)
          'CurrentTargetType: ${base.targetType.trim()}',
        if (base.resultView.trim().isNotEmpty)
          'CurrentResultView: ${base.resultView.trim()}',
        if (base.prompt.trim().isNotEmpty)
          'CurrentPrompt:\n${base.prompt.trim()}',
      ],
      'UserIntent: $intent',
    ];
    return lines.join('\n\n');
  }

  void syncSkills() {
    _service.send({'action': 'skill_sync_pull'});
  }

  void syncMemories() {
    _service.send({'action': 'memory_sync_pull'});
  }

  void requestMemoryList() {
    _service.send({'action': 'memory_list'});
  }

  void saveMemory(MemoryItem item) {
    final id = item.id.trim();
    if (id.isEmpty || _isSavingMemory) {
      return;
    }
    _isSavingMemory = true;
    _memorySyncStatus = '正在保存 memory…';
    notifyListeners();
    _service.send({
      'action': 'memory_upsert',
      'item': {
        'id': id,
        'title': item.title.trim(),
        'content': item.content.trim(),
      },
    });
  }

  void reviseMemoryWithClaude(MemoryItem item, String request) {
    final prompt = buildMemoryAuthoringPrompt(item, request);
    if (prompt.isEmpty) {
      return;
    }
    _memorySyncStatus = '正在修改 memory…';
    notifyListeners();
    _dispatchContextualClaudeRequest(
      prompt,
      label: '修改 Memory',
      targetType: 'memory',
      targetTitle: item.title.isNotEmpty ? item.title : item.id,
      resultView: 'memory-catalog',
    );
  }

  String buildMemoryAuthoringPrompt(MemoryItem item, String request) {
    final intent = request.trim();
    if (intent.isEmpty) {
      return '';
    }
    final title =
        item.title.trim().isNotEmpty ? item.title.trim() : item.id.trim();
    final lines = <String>[
      '请根据下面需求修改这个 Claude memory。',
      '你必须只返回严格 JSON，不要输出 markdown、解释、代码块标记或额外文字。',
      '返回 JSON 顶层字段必须是：mobilevcCatalogAuthoring、kind、memory。',
      '其中 mobilevcCatalogAuthoring 必须为 true，kind 必须为 "memory"。',
      'memory 对象内必须包含：id、title、content。',
      '默认保持原有 id 不变，除非用户明确要求改 id。',
      '示例格式：{"mobilevcCatalogAuthoring":true,"kind":"memory","memory":{"id":"memory-id","title":"标题","content":"内容"}}',
      'CurrentMemoryId: ${item.id.trim()}',
      'CurrentMemoryTitle: $title',
      if (item.content.trim().isNotEmpty)
        'CurrentMemoryContent:\n${item.content.trim()}',
      'UserIntent: $intent',
    ];
    return lines.join('\n\n');
  }

  void requestSessionContext() {
    _service.send({'action': 'session_context_get'});
  }

  void requestReviewState() {
    _service.send({'action': 'review_state_get'});
  }

  void updateSessionContext({
    List<String>? enabledSkillNames,
    List<String>? enabledMemoryIds,
  }) {
    final next = _sessionContext.copyWith(
      enabledSkillNames: enabledSkillNames,
      enabledMemoryIds: enabledMemoryIds,
    );
    if (_pendingSessionContextTarget != null) {
      return;
    }
    _pendingSessionContextRollback = _sessionContext;
    _pendingSessionContextTarget = next;
    _sessionContext = next;
    _pendingToggleSkillNames
      ..clear()
      ..addAll(_diffPendingNames(
        _pendingSessionContextRollback!.enabledSkillNames,
        next.enabledSkillNames,
      ));
    _pendingToggleMemoryIds
      ..clear()
      ..addAll(_diffPendingNames(
        _pendingSessionContextRollback!.enabledMemoryIds,
        next.enabledMemoryIds,
      ));
    _service.send({
      'action': 'session_context_update',
      'enabledSkillNames': next.enabledSkillNames,
      'enabledMemoryIds': next.enabledMemoryIds,
    });
    notifyListeners();
  }

  void toggleSkillEnabled(String name) {
    final skillName = name.trim();
    if (skillName.isEmpty ||
        isSkillTogglePending(skillName) ||
        _pendingSessionContextTarget != null) {
      return;
    }
    final next = [..._sessionContext.enabledSkillNames];
    if (next.contains(skillName)) {
      next.remove(skillName);
    } else {
      next.add(skillName);
    }
    updateSessionContext(enabledSkillNames: next);
  }

  void toggleMemoryEnabled(String id) {
    final memoryId = id.trim();
    if (memoryId.isEmpty ||
        isMemoryTogglePending(memoryId) ||
        _pendingSessionContextTarget != null) {
      return;
    }
    final next = [..._sessionContext.enabledMemoryIds];
    if (next.contains(memoryId)) {
      next.remove(memoryId);
    } else {
      next.add(memoryId);
    }
    updateSessionContext(enabledMemoryIds: next);
  }

  void executeSkill(String name, {Map<String, dynamic>? meta}) {
    final skillName = name.trim();
    if (skillName.isEmpty) {
      return;
    }
    _service.send({
      'action': 'skill_exec',
      'name': skillName,
      'engine': _config.engine,
      'cwd': effectiveCwd,
      ...currentMeta.toJson(),
      ...?meta,
    });
    _pushUser('/$skillName', 'Skill');
  }

  void _dispatchContextualClaudeRequest(
    String prompt, {
    required String label,
    String targetType = '',
    String targetTitle = '',
    String resultView = '',
    String skillName = '',
  }) {
    final value = prompt.trim();
    if (value.isEmpty) {
      return;
    }
    if (_isLoadingSession) {
      _pushSystem('session', '会话切换中，请等待加载完成');
      return;
    }
    if (awaitInput) {
      _markActionNeededHandled();
      _submitAwaitingPrompt(value, promptLabel: label, fallbackToInput: true);
      return;
    }
    if (isSessionBusy) {
      _pushSystem('session', '当前会话仍在运行，暂时不能发起新的请求。');
      return;
    }
    _resetActionNeededTracking();
    final meta = currentMeta.merge(
      RuntimeMeta(
        source: 'catalog-authoring',
        targetType: targetType,
        targetTitle: targetTitle,
        resultView: resultView,
        skillName: skillName,
      ),
    );
    _service.send({
      'action': 'exec',
      'cmd': value,
      'cwd': effectiveCwd,
      'mode': 'pty',
      ...meta.toJson(),
      'permissionMode': _config.permissionMode,
    });
    _pushUser(value, label);
  }

  void continueWithCurrentFile([String text = '基于当前文件继续处理']) {
    if (_isLoadingSession) {
      _pushSystem('session', '会话切换中，请等待加载完成');
      return;
    }
    final prompt = buildFileScopedPrompt(text);
    if (prompt.isEmpty) {
      _pushSystem('error', '当前没有可用的文件上下文');
      return;
    }
    if (awaitInput) {
      _pushDebug('文件面板输入走等待态分流', _debugReviewStateSummary());
      final pendingInteraction = _pendingInteraction;
      if (pendingInteraction?.isPermission == true) {
        _markActionNeededHandled();
        _sendInteractionDecision(pendingInteraction!, 'approve',
            promptLabel: '文件回复');
        return;
      }
      final pending = _pendingPrompt;
      if (pending?.looksLikePermissionPrompt == true) {
        _markActionNeededHandled();
        _sendPermissionDecision(pending!, 'approve', promptLabel: '文件回复');
        return;
      }
      _markActionNeededHandled();
      _submitAwaitingPrompt(
        prompt,
        promptLabel: '文件回复',
        fallbackToInput: true,
      );
      return;
    }
    if (isSessionBusy) {
      if (hasPendingReview) {
        _pushSystem('session', '当前会话仍在运行，请先处理待审核 diff。');
      } else {
        _pushSystem('session', '当前会话仍在运行，请等待进入输入态后再继续。');
      }
      return;
    }
    _resetActionNeededTracking();
    final meta = currentMeta.merge(
      RuntimeMeta(
        source: 'file-context',
        targetType: 'file',
        targetPath: _openedFile?.path ?? currentMeta.targetPath,
        contextTitle: _openedFile?.title ?? currentMeta.targetTitle,
        targetTitle: _openedFile?.title ?? currentMeta.targetTitle,
        targetText: _openedFile?.isText == true
            ? _openedFile?.content ?? currentMeta.targetText
            : currentMeta.targetText,
      ),
    );
    _service.send({
      'action': 'exec',
      'cmd': prompt,
      'cwd': effectiveCwd,
      'mode': 'pty',
      ...meta.toJson(),
      'permissionMode': _config.permissionMode,
    });
    _pushUser(prompt, '文件命令');
  }

  String buildFileScopedPrompt(String text) {
    final intent = text.trim().isEmpty ? '基于当前文件继续处理' : text.trim();
    final path = _openedFile?.path ?? currentMeta.targetPath;
    final title = _openedFile?.title ?? currentMeta.targetTitle;
    if (path.isEmpty && title.isEmpty) {
      return '';
    }
    final lines = <String>[
      '请只围绕当前文件继续处理。',
      if (path.isNotEmpty) 'TargetPath: $path',
      'ContextTitle: ${title.isNotEmpty ? title : '当前文件'}',
      'UserIntent: $intent',
    ];
    return lines.join('\n');
  }

  void sendInputText(String text) {
    final value = text.trim();
    if (value.isEmpty) {
      return;
    }
    if (_isLoadingSession) {
      _pushSystem('session', '会话切换中，请等待加载完成');
      return;
    }
    if (awaitInput) {
      _markActionNeededHandled();
      _submitAwaitingPrompt(value);
      return;
    }
    if (value.startsWith('/')) {
      _handleSlashCommand(value);
      return;
    }
    if (isSessionBusy) {
      if (hasPendingReview) {
        _pushSystem('session', '当前会话仍在运行，请先完成待审核 diff，再继续处理。');
      } else {
        _pushSystem('session', '当前会话仍在运行，暂时不能发起新的命令。');
      }
      return;
    }
    _resetActionNeededTracking();
    final meta = currentMeta;
    _service.send({
      'action': 'exec',
      'cmd': value,
      'cwd': effectiveCwd,
      'mode': 'pty',
      ...meta.toJson(),
      'permissionMode': _config.permissionMode,
    });
    _pushUser(value, '命令');
  }

  void submitPromptOption(String value) {
    final normalized = value.trim();
    if (normalized.isEmpty) {
      return;
    }
    if (_isLoadingSession) {
      _pushSystem('session', '会话切换中，请等待加载完成');
      return;
    }
    _markActionNeededHandled();
    _pushDebug(
        '提交 prompt 选项', 'value=$normalized\n${_debugReviewStateSummary()}');
    final interaction = pendingInteraction;
    if (interaction != null) {
      _submitInteractionActionValue(interaction, normalized);
      return;
    }
    if (shouldShowReviewChoices) {
      final decision = _reviewDecisionFromPromptValue(normalized);
      if (decision != null) {
        sendReviewDecision(decision);
        return;
      }
    }
    final resolved = _normalizePromptReply(normalized, _pendingPrompt);
    _submitAwaitingPrompt(resolved);
  }

  String _normalizePromptReply(String value, PromptRequestEvent? prompt) {
    final normalized = value.trim();
    final lower = normalized.toLowerCase();
    if (prompt?.looksLikePermissionPrompt == true) {
      if (_approvePromptValues.contains(lower)) {
        return 'approve';
      }
      if (_denyPromptValues.contains(lower)) {
        return 'deny';
      }
    }
    return normalized;
  }

  void _submitAwaitingPrompt(
    String value, {
    String promptLabel = '回复',
    bool fallbackToInput = false,
  }) {
    if (_isLoadingSession) {
      _pushSystem('session', '会话切换中，请等待加载完成');
      return;
    }
    final interaction = pendingInteraction;
    if (interaction != null) {
      _submitInteractionActionValue(interaction, value,
          promptLabel: promptLabel);
      return;
    }
    final prompt = _pendingPrompt;
    if (prompt != null) {
      if (shouldShowReviewChoices) {
        final decision = _reviewDecisionFromPromptValue(value);
        if (decision != null) {
          sendReviewDecision(decision);
          return;
        }
      }
      if (prompt.looksLikePermissionPrompt) {
        final decision = _permissionDecisionFromPromptValue(value);
        if (decision != null) {
          _sendPermissionDecision(prompt, decision, promptLabel: promptLabel);
          return;
        }
      }
    }
    if (!fallbackToInput && !awaitInput) {
      return;
    }
    _submitAwaitingInput(value, promptLabel: promptLabel);
  }

  String? _permissionDecisionFromPromptValue(String value) {
    final normalized = value.trim().toLowerCase();
    if (_approvePromptValues.contains(normalized) || normalized == 'approve') {
      return 'approve';
    }
    if (_denyPromptValues.contains(normalized) || normalized == 'deny') {
      return 'deny';
    }
    return null;
  }

  void _submitInteractionActionValue(
    InteractionRequestEvent interaction,
    String value, {
    String promptLabel = '回复',
  }) {
    final normalized = value.trim();
    if (normalized.isEmpty) {
      return;
    }
    if (interaction.isReview) {
      final decision = _reviewDecisionFromPromptValue(normalized) ??
          _reviewDecisionFromPromptValue(
            interaction.actions
                    .cast<InteractionAction?>()
                    .firstWhere(
                      (action) =>
                          action?.id == normalized ||
                          action?.value == normalized,
                      orElse: () => null,
                    )
                    ?.decision ??
                '',
          );
      if (decision != null) {
        sendReviewDecision(decision);
        return;
      }
    }
    if (interaction.isPermission) {
      final decision = _permissionDecisionFromPromptValue(normalized) ??
          _permissionDecisionFromPromptValue(
            interaction.actions
                    .cast<InteractionAction?>()
                    .firstWhere(
                      (action) =>
                          action?.id == normalized ||
                          action?.value == normalized,
                      orElse: () => null,
                    )
                    ?.decision ??
                '',
          );
      if (decision != null) {
        _sendInteractionDecision(interaction, decision,
            promptLabel: promptLabel);
        return;
      }
    }
    _submitAwaitingInput(normalized, promptLabel: promptLabel);
  }

  void _sendInteractionDecision(
    InteractionRequestEvent interaction,
    String decision, {
    String promptLabel = '回复',
  }) {
    if (interaction.isReview) {
      sendReviewDecision(decision);
      return;
    }
    if (interaction.isPermission) {
      _pendingInteraction = null;
      _service.send({
        'action': 'permission_decision',
        'decision': decision,
        'permissionMode': _currentDecisionPermissionMode,
        'resumeSessionId': interaction.resumeSessionId,
        'targetPath': interaction.targetPath,
        'contextId': interaction.contextId,
        'contextTitle': interaction.contextTitle,
        'promptMessage': interaction.message,
        'command': currentMeta.command,
        'cwd': effectiveCwd,
        'engine': _config.engine,
        'target': currentMeta.target,
        'targetType': currentMeta.targetType,
      });
      _pushUser('Permission: $decision', promptLabel);
      return;
    }
    _submitAwaitingInput(decision, promptLabel: promptLabel);
  }

  void _submitAwaitingInput(String value, {String promptLabel = '回复'}) {
    _pendingPrompt = null;
    _pendingInteraction = null;
    _service.send({
      'action': 'input',
      'data': '$value\n',
      'permissionMode': _currentDecisionPermissionMode,
    });
    _pushUser(value, promptLabel);
  }

  void _sendPermissionDecision(
    PromptRequestEvent prompt,
    String decision, {
    String promptLabel = '回复',
  }) {
    final meta = currentMeta;
    final targetPath = _openedFile?.path.isNotEmpty == true
        ? _openedFile!.path
        : meta.targetPath;
    final contextTitle = _openedFile?.title.isNotEmpty == true
        ? _openedFile!.title
        : meta.contextTitle.isNotEmpty
            ? meta.contextTitle
            : meta.targetTitle;
    _pendingPrompt = null;
    _service.send({
      'action': 'permission_decision',
      'decision': decision,
      'permissionMode': _currentDecisionPermissionMode,
      'resumeSessionId': meta.resumeSessionId,
      'targetPath': targetPath,
      'contextId': meta.contextId,
      'contextTitle': contextTitle,
      'promptMessage': prompt.message,
      'command': meta.command,
      'cwd': effectiveCwd,
      'engine': _config.engine,
      'target': meta.target,
      'targetType': meta.targetType,
    });
    _pushUser('Permission: $decision', promptLabel);
  }

  void requestRuntimeInfo(String query) {
    _service
        .send({'action': 'runtime_info', 'query': query, 'cwd': effectiveCwd});
  }

  void clearTimeline() {
    _timeline.clear();
    notifyListeners();
  }

  void _handleSlashCommand(String raw) {
    switch (raw.trim()) {
      case '/clear':
        clearTimeline();
        _pushSystem('session', '已清空当前前端时间线');
        break;
      case '/fast':
        _config = _config.copyWith(fastMode: !_config.fastMode);
        _pushSystem('session', _config.fastMode ? 'Fast 模式已开启' : 'Fast 模式已关闭');
        notifyListeners();
        break;
      case '/exit':
      case '/quit':
        disconnect();
        break;
      case '/diff':
        if ((_currentDiff?.diff ?? '').isEmpty) {
          _pushSystem('error', '当前没有可展示的 diff');
        } else {
          _pushSystem('session', '已准备打开最近 diff');
          notifyListeners();
        }
        break;
      default:
        final meta = currentMeta;
        _service.send({
          'action': 'slash_command',
          'command': raw,
          'cwd': effectiveCwd,
          'engine': _config.engine,
          ...meta.toJson(),
          'permissionMode': _config.permissionMode,
        });
        _pushUser(raw, 'Slash');
    }
  }

  void _handleEvent(AppEvent event) async {
    if (_shouldAdoptEventSessionId(event.sessionId)) {
      _selectedSessionId = event.sessionId;
    }

    switch (event) {
      case SessionCreatedEvent created:
        _autoSessionRequested = false;
        _autoSessionCreating = false;
        _autoSessionLoadingId = '';
        _selectedSessionId = created.summary.id;
        _selectedSessionTitle = created.summary.title;
        _upsertSession(created.summary);
        _finishSessionLoading(sessionId: created.summary.id);
        break;
      case SessionListResultEvent list:
        _sessions
          ..clear()
          ..addAll(list.items);
        _handleAutoSessionBinding(list.items);
        break;
      case SessionHistoryEvent history:
        _autoSessionRequested = false;
        _autoSessionCreating = false;
        _autoSessionLoadingId = '';
        _resetActionNeededTracking(suppressNextSignal: true);
        _selectedSessionId = history.summary.id;
        _selectedSessionTitle = history.summary.title;
        _sessionContext = history.sessionContext;
        _skillCatalogMeta = history.skillCatalogMeta;
        _memoryCatalogMeta = history.memoryCatalogMeta;
        _runtimePermissionMode =
            history.resumeRuntimeMeta.permissionMode.trim();
        _upsertSession(history.summary);
        _timeline
          ..clear()
          ..addAll(history.logEntries
              .map(_timelineFromHistory)
              .where(_shouldKeepTimelineItem));
        _recentDiffs
          ..clear()
          ..addAll(history.diffs.map(_normalizeHistoryDiff));
        _reviewGroups
          ..clear()
          ..addAll(history.reviewGroups.map(_normalizeReviewGroup));
        _activeReviewGroupId = history.activeReviewGroup?.id ?? '';
        _syncReviewGroupsFromRecentDiffs();
        _syncActiveReviewSelection();
        _currentStep = history.currentStep;
        _currentStepSummary = _summaryFromHistoryContext(history.currentStep);
        _latestError = history.latestError;
        _canResumeCurrentSession = history.canResume;
        _resumeRuntimeMeta = history.resumeRuntimeMeta;
        _terminalExecutions
          ..clear()
          ..addAll(history.terminalExecutions);
        _restoreTerminalLogs(history.rawTerminalByStream);
        _syncActiveTerminalExecution();
        if (history.currentDiff != null) {
          final current = _normalizeHistoryDiff(history.currentDiff!);
          _mergeRecentDiff(current);
          _currentDiff = FileDiffEvent(
            timestamp: DateTime.now(),
            sessionId: history.summary.id,
            runtimeMeta: history.resumeRuntimeMeta.merge(
              RuntimeMeta(
                contextId: current.id,
                contextTitle: current.title,
                targetPath: current.path,
                targetDiff: current.diff,
                targetTitle: current.title,
                executionId: current.executionId,
                groupId: current.groupId,
                groupTitle: current.groupTitle,
              ),
            ),
            raw: const {},
            path: current.path,
            title: current.title,
            diff: current.diff,
            lang: current.lang,
          );
        } else {
          final resolved = _resolvedCurrentDiff();
          _currentDiff = resolved == null
              ? null
              : FileDiffEvent(
                  timestamp: DateTime.now(),
                  sessionId: history.summary.id,
                  runtimeMeta: history.resumeRuntimeMeta.merge(
                    RuntimeMeta(
                      contextId: resolved.id,
                      contextTitle: resolved.title,
                      targetPath: resolved.path,
                      targetDiff: resolved.diff,
                      targetTitle: resolved.title,
                      executionId: resolved.executionId,
                      groupId: resolved.groupId,
                      groupTitle: resolved.groupTitle,
                    ),
                  ),
                  raw: const {},
                  path: resolved.path,
                  title: resolved.title,
                  diff: resolved.diff,
                  lang: resolved.lang,
                );
        }
        if (_matchesPendingSessionTarget(history.summary.id)) {
          _finishSessionLoading(sessionId: history.summary.id);
        }
        await switchWorkingDirectory(_config.cwd);
        _pushSystem('session',
            '已加载历史会话${history.summary.title.isNotEmpty ? ' · ${history.summary.title}' : ''}');
        break;
      case SessionStateEvent state:
        _sessionState = state;
        _syncRuntimePermissionMode();
        if (_isLoadingSession &&
            _matchesPendingSessionTarget(state.sessionId)) {
          _finishSessionLoading(sessionId: state.sessionId);
        }
        if (_isIdleLikeState(state.state)) {
          if (!_shouldPreserveBlockingPrompt()) {
            _pendingInteraction = null;
            _pendingPrompt = null;
          }
          if (_agentState != null &&
              _isIdleLikeState(_agentState!.state) &&
              !_shouldPreserveBlockingPrompt()) {
            _agentState = null;
          }
        }
        _connectionMessage =
            state.message.isNotEmpty ? state.message : state.state;
        _handleSessionStateTimeline(state);
        break;
      case AgentStateEvent agent:
        _agentState = agent;
        _syncRuntimePermissionMode();
        if (_isIdleLikeState(agent.state) && !_shouldPreserveBlockingPrompt()) {
          _pendingInteraction = null;
          _pendingPrompt = null;
        }
        _syncStepSummary(
          message: agent.step.isNotEmpty ? agent.step : agent.message,
          status: agent.state,
          tool: agent.tool,
          command: agent.command,
          targetPath: agent.runtimeMeta.targetPath,
        );
        break;
      case LogEvent log:
        _appendTerminalLog(log.stream, log.message,
            executionId: log.runtimeMeta.executionId);
        _handleLogTimeline(log);
        break;
      case ProgressEvent progress:
        if (progress.message.isNotEmpty && _currentStepSummary.isEmpty) {
          _currentStepSummary = progress.message;
        }
        break;
      case ErrorEvent error:
        _fileListLoading = false;
        _fileReading = false;
        _latestError = HistoryContext(
          id: error.runtimeMeta.contextId,
          type: 'error',
          message: error.message,
          stack: error.stack,
          code: error.code,
          targetPath: error.targetPath,
          relatedStep: error.step,
          command: error.command,
          title: error.message,
        );
        _pushTimelineItem(
          TimelineItem(
            id: 'error-${error.timestamp.microsecondsSinceEpoch}',
            kind: 'error',
            timestamp: error.timestamp,
            title: error.code,
            body: error.message,
            meta: error.runtimeMeta,
            context: _latestError,
          ),
        );
        _handleMutationFailure(error.message);
        break;
      case PromptRequestEvent prompt:
        final currentInteraction = _pendingInteraction;
        final currentPrompt = _pendingPrompt;
        final keepBlockingPrompt = (currentInteraction?.isPermission == true ||
                currentInteraction?.isReview == true ||
                currentPrompt?.looksLikePermissionPrompt == true ||
                _isReviewDecisionPrompt(currentPrompt)) &&
            _isGenericContinuePrompt(prompt);
        if (keepBlockingPrompt) {
          _pushDebug('忽略通用继续输入 prompt',
              'incoming=${prompt.message}\n${_debugReviewStateSummary()}');
          break;
        }
        _pendingInteraction = null;
        _pendingPrompt = prompt;
        _syncRuntimePermissionMode();
        _pushDebug('收到 prompt_request', _debugReviewStateSummary());
        break;
      case InteractionRequestEvent interaction:
        _pendingPrompt = null;
        _pendingInteraction = interaction;
        _syncRuntimePermissionMode();
        _pushDebug('收到 interaction_request', _debugReviewStateSummary());
        break;
      case FSListResultEvent fsList:
        _fileListLoading = false;
        _currentDirectoryPath = fsList.currentPath.trim().isEmpty
            ? effectiveCwd
            : fsList.currentPath;
        if (_normalizePath(_config.cwd) !=
            _normalizePath(_currentDirectoryPath)) {
          _config = _config.copyWith(cwd: _currentDirectoryPath);
          SharedPreferences.getInstance().then(
            (prefs) => prefs.setString(_prefsKey, jsonEncode(_config.toJson())),
          );
        }
        _currentDirectoryItems
          ..clear()
          ..addAll(fsList.items);
        break;
      case FSReadResultEvent fsRead:
        _fileReading = false;
        _openedFile = fsRead.result;
        _pushTimelineItem(
          TimelineItem(
            id: 'file-${fsRead.timestamp.microsecondsSinceEpoch}',
            kind: 'fs_read_result',
            timestamp: fsRead.timestamp,
            title: fsRead.result.title,
            body: fsRead.result.path,
            meta: fsRead.runtimeMeta,
            context: HistoryContext(
              id: fsRead.runtimeMeta.contextId,
              type: 'file',
              path: fsRead.result.path,
              title: fsRead.result.title,
              lang: fsRead.result.lang,
            ),
          ),
        );
        break;
      case StepUpdateEvent step:
        _currentStep = HistoryContext(
          id: step.runtimeMeta.contextId,
          type: 'step',
          message: step.message,
          status: step.status,
          target: step.target,
          tool: step.tool,
          command: step.command,
          title: step.message,
          targetPath: step.runtimeMeta.targetPath,
        );
        _syncStepSummary(
          message: step.message,
          status: step.status,
          tool: step.tool,
          command: step.command,
          targetPath: step.runtimeMeta.targetPath,
        );
        break;
      case ReviewStateEvent reviewState:
        _reviewGroups
          ..clear()
          ..addAll(reviewState.groups.map(_normalizeReviewGroup));
        _activeReviewGroupId =
            reviewState.activeGroup?.id ?? _activeReviewGroupId;
        _syncReviewGroupsFromRecentDiffs();
        _syncActiveReviewSelection();
        break;
      case FileDiffEvent diff:
        _currentDiff = diff;
        final historyDiff = HistoryContext(
          id: diff.runtimeMeta.contextId,
          type: 'diff',
          path: diff.path,
          title: diff.title,
          diff: diff.diff,
          lang: diff.lang,
          pendingReview: !isAutoAcceptMode,
          source: diff.runtimeMeta.source,
          skillName: diff.runtimeMeta.skillName,
          executionId: diff.runtimeMeta.executionId,
          groupId: diff.runtimeMeta.groupId,
          groupTitle: diff.runtimeMeta.groupTitle,
          reviewStatus: !isAutoAcceptMode ? 'pending' : 'accepted',
        );
        _mergeRecentDiff(historyDiff);
        _syncReviewGroupsFromRecentDiffs();
        _pushTimelineItem(
          TimelineItem(
            id: 'diff-${diff.timestamp.microsecondsSinceEpoch}',
            kind: 'file_diff',
            timestamp: diff.timestamp,
            title: diff.title,
            body: diff.path,
            meta: diff.runtimeMeta,
            context: historyDiff,
          ),
        );
        break;
      case RuntimeInfoResultEvent runtimeInfo:
        _runtimeInfo = runtimeInfo;
        break;
      case SkillCatalogResultEvent result:
        _skillCatalogMeta = result.meta;
        _skills
          ..clear()
          ..addAll(result.items);
        _isSavingSkill = false;
        break;
      case MemoryListResultEvent result:
        _memoryCatalogMeta = result.meta;
        _memoryItems
          ..clear()
          ..addAll(result.items);
        _isSavingMemory = false;
        break;
      case SessionContextResultEvent result:
        _sessionContext = result.sessionContext;
        _pendingSessionContextTarget = null;
        _pendingSessionContextRollback = null;
        _pendingToggleSkillNames.clear();
        _pendingToggleMemoryIds.clear();
        break;
      case SkillSyncResultEvent result:
        _skillSyncStatus =
            result.message.isNotEmpty ? result.message : 'skill 同步完成';
        _pushSystem('session', _skillSyncStatus);
        break;
      case CatalogSyncStatusEvent status:
        if (status.domain == 'skill') {
          _skillCatalogMeta = status.meta;
          _skillSyncStatus = 'Skill 同步中...';
        } else if (status.domain == 'memory') {
          _memoryCatalogMeta = status.meta;
          _memorySyncStatus = 'Memory 同步中...';
        }
        break;
      case CatalogSyncResultEvent result:
        if (result.domain == 'skill') {
          _skillCatalogMeta = result.meta;
          _skillSyncStatus = result.message;
        } else if (result.domain == 'memory') {
          _memoryCatalogMeta = result.meta;
          _memorySyncStatus = result.message;
        }
        if (result.message.trim().isNotEmpty) {
          _pushSystem('session', result.message);
        }
        break;
      case UnknownEvent unknown:
        _pushSystem('system', '收到未识别事件：${unknown.type}');
        break;
      default:
        break;
    }
    _syncDerivedState();
    notifyListeners();
  }

  Set<String> _diffPendingNames(List<String> previous, List<String> next) {
    final changed = <String>{};
    for (final item in previous) {
      if (!next.contains(item)) {
        changed.add(item);
      }
    }
    for (final item in next) {
      if (!previous.contains(item)) {
        changed.add(item);
      }
    }
    return changed;
  }

  void _handleMutationFailure(String message) {
    if (_pendingSessionContextRollback != null) {
      _sessionContext = _pendingSessionContextRollback!;
      _pendingSessionContextRollback = null;
      _pendingSessionContextTarget = null;
      _pendingToggleSkillNames.clear();
      _pendingToggleMemoryIds.clear();
      if (message.trim().isNotEmpty) {
        _pushSystem('error', '会话上下文更新失败：$message');
      }
    }
    if (_isSavingSkill) {
      _isSavingSkill = false;
      if (message.trim().isNotEmpty) {
        _skillSyncStatus = '保存 skill 失败：$message';
      }
    }
    if (_isSavingMemory) {
      _isSavingMemory = false;
      if (message.trim().isNotEmpty) {
        _memorySyncStatus = '保存 memory 失败：$message';
      }
    }
  }

  bool _shouldAdoptEventSessionId(String sessionId) {
    final normalized = sessionId.trim();
    if (normalized.isEmpty) {
      return false;
    }
    if (normalized.startsWith('conn-')) {
      return false;
    }
    return true;
  }

  bool _isIdleLikeState(String state) {
    final normalized = state.trim().toUpperCase();
    return normalized.isEmpty ||
        normalized == 'IDLE' ||
        normalized == 'DONE' ||
        normalized == 'COMPLETED' ||
        normalized == 'DISCONNECTED';
  }

  String? _reviewDecisionFromPromptValue(String value) {
    final normalized = value.trim().toLowerCase();
    switch (normalized) {
      case 'accept':
      case 'revert':
      case 'revise':
        return normalized;
      default:
        return null;
    }
  }

  void _markDiffReviewState(
    HistoryContext diff, {
    required bool keepPending,
    String reviewStatus = '',
  }) {
    final targetId = diff.id.trim();
    final targetPath = diff.path.trim();
    for (var i = 0; i < _recentDiffs.length; i++) {
      final item = _recentDiffs[i];
      final sameId =
          targetId.isNotEmpty && item.id.isNotEmpty && item.id == targetId;
      final samePath =
          targetPath.isNotEmpty && _pathsMatch(item.path, targetPath);
      if (!sameId && !samePath) {
        continue;
      }
      _recentDiffs[i] = HistoryContext(
        id: item.id,
        type: item.type,
        message: item.message,
        status: item.status,
        target: item.target,
        targetPath: item.targetPath,
        tool: item.tool,
        command: item.command,
        timestamp: item.timestamp,
        title: item.title,
        stack: item.stack,
        code: item.code,
        relatedStep: item.relatedStep,
        path: item.path,
        diff: item.diff,
        lang: item.lang,
        pendingReview: keepPending,
        source: item.source,
        skillName: item.skillName,
        executionId: item.executionId,
        groupId: item.groupId,
        groupTitle: item.groupTitle,
        reviewStatus:
            reviewStatus.isNotEmpty ? reviewStatus : item.reviewStatus,
      );
    }
    _syncActiveReviewDiff();
  }

  void _sendReviewDecisionForDiff(
    HistoryContext diff,
    String normalized, {
    bool pushTimeline = true,
  }) {
    if (!isManualReviewMode) {
      _pushSystem('session', '当前是自动接受修改模式，无需手动审核 diff');
      return;
    }
    final reviewedDiffId = _diffIdentity(diff);
    _activeReviewDiffId = reviewedDiffId;
    final groupId = _groupIdForDiff(diff);
    if (groupId.isNotEmpty) {
      _activeReviewGroupId = groupId;
    }
    _service.send({
      'action': 'review_decision',
      'decision': normalized,
      'contextId': diff.id.isNotEmpty ? diff.id : currentMeta.contextId,
      'contextTitle':
          diff.title.isNotEmpty ? diff.title : currentMeta.contextTitle,
      'targetPath': diff.path.isNotEmpty ? diff.path : currentMeta.targetPath,
      'executionId': diff.executionId,
      'groupId': groupId,
      'groupTitle': diff.groupTitle,
      'permissionMode': _currentDecisionPermissionMode,
    });
    _pendingPrompt = normalized == 'revise' ? _pendingPrompt : null;
    _markDiffReviewState(
      diff,
      keepPending: normalized == 'revise',
      reviewStatus: _reviewStatusFromDecision(normalized),
    );
    _syncReviewGroupsFromRecentDiffs();
    _advanceReviewSelectionAfterDecision(diff, reviewedDiffId: reviewedDiffId);
    if (pushTimeline) {
      _pushUser('Review: $normalized', '审阅');
    }
    _syncDerivedState();
    notifyListeners();
  }

  void _advanceReviewSelectionAfterDecision(
    HistoryContext reviewedDiff, {
    required String reviewedDiffId,
  }) {
    final reviewedGroupId = _groupIdForDiff(reviewedDiff);
    final pendingInGroup = _pendingDiffs
        .where((item) => _groupIdForDiff(item) == reviewedGroupId)
        .toList(growable: false);
    if (pendingInGroup.isNotEmpty) {
      final reviewedIndex = pendingInGroup.indexWhere(
        (item) => _diffIdentity(item) == reviewedDiffId,
      );
      final nextInGroup =
          reviewedIndex >= 0 && reviewedIndex + 1 < pendingInGroup.length
              ? pendingInGroup[reviewedIndex + 1]
              : pendingInGroup.first;
      _activeReviewGroupId = reviewedGroupId;
      _activeReviewDiffId = _diffIdentity(nextInGroup);
      return;
    }

    final nextGroup = _reviewGroups.firstWhere(
      (group) => group.pendingCount > 0,
      orElse: () => const ReviewGroup(),
    );
    if (nextGroup.id.isNotEmpty) {
      _activeReviewGroupId = nextGroup.id;
      final nextPending = _pendingDiffs.firstWhere(
        (item) => _groupIdForDiff(item) == nextGroup.id,
        orElse: () => HistoryContext(),
      );
      if (_diffIdentity(nextPending).isNotEmpty) {
        _activeReviewDiffId = _diffIdentity(nextPending);
        return;
      }
    }

    _activeReviewGroupId = reviewedGroupId;
    _activeReviewDiffId = reviewedDiffId;
  }

  TimelineItem _timelineFromHistory(HistoryLogEntry entry) {
    return TimelineItem(
      id: 'history-${entry.kind}-${entry.timestamp}-${entry.message.hashCode}',
      kind: entry.kind,
      timestamp:
          DateTime.tryParse(entry.timestamp)?.toLocal() ?? DateTime.now(),
      title: entry.label,
      body: entry.message.isNotEmpty ? entry.message : entry.text,
      stream: entry.stream,
      context: entry.context,
    );
  }

  void _upsertSession(SessionSummary summary) {
    final index = _sessions.indexWhere((item) => item.id == summary.id);
    if (index == -1) {
      _sessions.insert(0, summary);
    } else {
      _sessions[index] = summary;
    }
  }

  void _pushUser(String text, String label) {
    _pushTimelineItem(
      TimelineItem(
        id: 'user-${DateTime.now().microsecondsSinceEpoch}',
        kind: 'user',
        timestamp: DateTime.now(),
        title: label,
        body: text,
      ),
    );
    notifyListeners();
  }

  void _pushSystem(String kind, String text) {
    if (_shouldFilterTimelineText(text)) {
      return;
    }
    _pushTimelineItem(
      TimelineItem(
        id: 'system-${DateTime.now().microsecondsSinceEpoch}',
        kind: kind,
        timestamp: DateTime.now(),
        body: text,
      ),
    );
    notifyListeners();
  }

  void _pushTimelineItem(TimelineItem item) {
    if (_shouldKeepTimelineItem(item)) {
      _timeline.add(item);
    }
  }

  bool _shouldKeepTimelineItem(TimelineItem item) {
    if (_shouldFilterTimelineText(item.title) ||
        _shouldFilterTimelineText(item.body)) {
      return false;
    }
    switch (item.kind) {
      case 'agent_state':
      case 'step_update':
      case 'progress':
      case 'prompt_request':
        return false;
      default:
        return true;
    }
  }

  void _handleSessionStateTimeline(SessionStateEvent state) {
    final key = '${state.state}|${state.message}';
    if (key == _lastSessionTimelineKey) {
      return;
    }
    final normalizedState = state.state.trim().toLowerCase();
    final normalizedMessage = state.message.trim();
    final looksLikeNoise = normalizedMessage.isEmpty
        ? _looksLikeProcessNoise(normalizedState)
        : _looksLikeProcessNoise(normalizedMessage);
    if (_shouldFilterTimelineText(normalizedState) ||
        _shouldFilterTimelineText(normalizedMessage) ||
        looksLikeNoise) {
      _lastSessionTimelineKey = key;
      return;
    }
    final shouldSurface = normalizedMessage.isNotEmpty ||
        normalizedState == 'connected' ||
        normalizedState == 'disconnected' ||
        normalizedState == 'reconnected';
    if (!shouldSurface) {
      _lastSessionTimelineKey = key;
      return;
    }
    _lastSessionTimelineKey = key;
    _pushTimelineItem(
      TimelineItem(
        id: 'session-${state.timestamp.microsecondsSinceEpoch}',
        kind: 'session',
        timestamp: state.timestamp,
        title: state.state,
        body: state.message,
        meta: state.runtimeMeta,
      ),
    );
  }

  void _handleLogTimeline(LogEvent log) {
    final message = log.message;
    if (message.isEmpty) {
      return;
    }
    final now = log.timestamp;
    if (message == _lastLogMessage &&
        log.stream == _lastLogStream &&
        _lastLogAt != null &&
        now.difference(_lastLogAt!).inMilliseconds < 200) {
      return;
    }
    _lastLogMessage = message;
    _lastLogStream = log.stream;
    _lastLogAt = now;

    final kind = _timelineKindForLog(message, log.stream);
    if (kind == null) {
      return;
    }
    _pushTimelineItem(
      TimelineItem(
        id: 'log-${log.timestamp.microsecondsSinceEpoch}',
        kind: kind,
        timestamp: log.timestamp,
        body: message,
        stream: log.stream,
        meta: log.runtimeMeta,
      ),
    );
  }

  String? _timelineKindForLog(String message, String stream) {
    final trimmed = message.trim();
    if (trimmed.isEmpty) {
      return null;
    }
    if (_looksLikeFrontendToolResultNoise(trimmed) ||
        _shouldFilterTimelineText(trimmed)) {
      return null;
    }
    final normalizedStream = stream.trim().toLowerCase();
    if (normalizedStream == 'stderr') {
      return 'terminal';
    }
    if (_looksLikeProcessNoise(trimmed)) {
      return null;
    }
    if (_looksLikeTerminalOutput(trimmed) || message.startsWith('\r')) {
      return 'terminal';
    }
    if (_looksLikeAssistantReply(trimmed)) {
      return 'markdown';
    }
    return 'terminal';
  }

  bool _looksLikeAssistantReply(String message) {
    if (message.isEmpty || _looksLikeFrontendToolResultNoise(message)) {
      return false;
    }
    if (_looksLikeMarkdown(message)) {
      return true;
    }
    if (_looksLikeTerminalOutput(message)) {
      return false;
    }

    final normalized = message.trim();
    if (normalized.length >= 24 && !normalized.contains(RegExp(r'\s{2,}'))) {
      return true;
    }
    if (normalized.contains('\n')) {
      return true;
    }
    return RegExp(r'[。！？；：]|\.\s+[A-Z]|,\s+\w').hasMatch(normalized);
  }

  bool _looksLikeMarkdown(String message) {
    if (message.isEmpty) {
      return false;
    }
    if (_looksLikeFrontendToolResultNoise(message)) {
      return false;
    }
    return RegExp(
                r'```|^#{1,6}\s|^>\s|^[-*+]\s|^\d+\.\s|\[[^\]]+\]\([^\)]+\)|\|.+\|',
                multiLine: true)
            .hasMatch(message) ||
        message.length > 180;
  }

  bool _looksLikeTerminalOutput(String message) {
    final trimmed = message.trim();
    if (trimmed.isEmpty) {
      return false;
    }
    final lower = trimmed.toLowerCase();
    if (lower.startsWith(r'') || trimmed.contains('[')) {
      return true;
    }
    if (trimmed.contains(RegExp(r'^[\$#>]\s', multiLine: true))) {
      return true;
    }
    if (trimmed.contains(RegExp(
        r'^(npm|pnpm|yarn|flutter|dart|git|gradle|xcodebuild|pod|adb|fastlane|bash|zsh|sh)\b',
        multiLine: true))) {
      return true;
    }
    if (trimmed.contains(RegExp(
        r'^(at |Caused by:|Exception:|Error:|FAILURE:|BUILD FAILED|Task :|\[[^\]]+\])',
        multiLine: true))) {
      return true;
    }
    if (trimmed.contains(
        RegExp(r'(^|\n)(PASS|FAIL|WARN|INFO|ERROR)\b', multiLine: true))) {
      return true;
    }
    if (trimmed.contains(RegExp(r'^\S+\s*[:=]\s*\S+$', multiLine: true)) &&
        !trimmed.contains('。')) {
      return true;
    }
    final lines = trimmed
        .split('\n')
        .map((line) => line.trim())
        .where((line) => line.isNotEmpty)
        .toList();
    if (lines.length >= 3) {
      final terminalLikeLines = lines.where((line) {
        return RegExp(r'^[\$#>]\s').hasMatch(line) ||
            RegExp(r'^(at |Caused by:|Task :|\[[^\]]+\])').hasMatch(line) ||
            RegExp(r'^\S+\s*[:=]\s*\S+$').hasMatch(line) ||
            RegExp(r'^(PASS|FAIL|WARN|INFO|ERROR)\b').hasMatch(line);
      }).length;
      if (terminalLikeLines >= (lines.length / 2).ceil()) {
        return true;
      }
    }
    return false;
  }

  bool _looksLikeFrontendToolResultNoise(String message) {
    final text = message.trim();
    if (!text.startsWith('{') || !text.contains('tool_result')) {
      return false;
    }
    return text.contains('Invalid pages parameter') ||
        text.contains('tool_use_id') ||
        text.contains('session_id');
  }

  bool _looksLikeProcessNoise(String message) {
    final lower = message.trim().toLowerCase();
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
        lower == 'status: active' ||
        lower == 'status: ready' ||
        lower == 'status: idle' ||
        lower == 'session active' ||
        lower == 'session ready' ||
        lower.startsWith('progress:') ||
        lower.startsWith('step:') ||
        lower.startsWith('active:') ||
        lower.startsWith('ready:') ||
        lower.startsWith('idle:') ||
        lower.startsWith('command started');
  }

  bool _shouldFilterTimelineText(String text) {
    final trimmed = text.trim();
    if (trimmed.isEmpty) {
      return false;
    }
    final lower = trimmed.toLowerCase();
    return lower.startsWith('[debug]') ||
        lower == 'command started' ||
        trimmed == 'AI 会话已续接' ||
        lower == 'ai 会话已续接' ||
        lower == 'active' ||
        lower == 'ready' ||
        lower == 'idle' ||
        lower == 'is ready' ||
        trimmed == '已就绪' ||
        lower == 'status: active' ||
        lower == 'status: ready' ||
        lower == 'status: idle' ||
        lower == 'session active' ||
        lower == 'session ready' ||
        lower.startsWith('active:') ||
        lower.startsWith('ready:') ||
        lower.startsWith('idle:');
  }

  HistoryContext _normalizeHistoryDiff(HistoryContext item) {
    return HistoryContext(
      id: item.id,
      type: item.type.isNotEmpty ? item.type : 'diff',
      message: item.message,
      status: item.status,
      target: item.target,
      targetPath: item.targetPath,
      tool: item.tool,
      command: item.command,
      timestamp: item.timestamp,
      title: item.title,
      stack: item.stack,
      code: item.code,
      relatedStep: item.relatedStep,
      path: item.path,
      diff: item.diff,
      lang: item.lang,
      pendingReview: isAutoAcceptMode ? false : item.pendingReview,
      source: item.source,
      skillName: item.skillName,
      executionId: item.executionId,
      groupId: item.groupId,
      groupTitle: item.groupTitle,
      reviewStatus: item.reviewStatus,
    );
  }

  ReviewFile _normalizeReviewFile(ReviewFile file) {
    return ReviewFile(
      id: file.id,
      path: file.path,
      title: file.title,
      diff: file.diff,
      lang: file.lang,
      pendingReview: isAutoAcceptMode ? false : file.pendingReview,
      reviewStatus: file.reviewStatus,
      executionId: file.executionId,
    );
  }

  ReviewGroup _normalizeReviewGroup(ReviewGroup group) {
    return ReviewGroup(
      id: group.id,
      title: group.title,
      executionId: group.executionId,
      pendingReview: isAutoAcceptMode ? false : group.pendingReview,
      reviewStatus: group.reviewStatus,
      currentFileId: group.currentFileId,
      currentPath: group.currentPath,
      pendingCount: isAutoAcceptMode ? 0 : group.pendingCount,
      acceptedCount: group.acceptedCount,
      revertedCount: group.revertedCount,
      revisedCount: group.revisedCount,
      files: group.files.map(_normalizeReviewFile).toList(growable: false),
    );
  }

  String _diffIdentity(HistoryContext diff) {
    final id = diff.id.trim();
    if (id.isNotEmpty) {
      return id;
    }
    return _normalizePath(diff.path);
  }

  String _groupIdForDiff(HistoryContext diff) {
    final groupId = diff.groupId.trim();
    if (groupId.isNotEmpty) {
      return groupId;
    }
    final executionId = diff.executionId.trim();
    if (executionId.isNotEmpty) {
      return executionId;
    }
    final normalizedPath = _normalizePath(diff.path);
    return normalizedPath;
  }

  ReviewGroup? _findReviewGroupById(String groupId) {
    final normalized = groupId.trim();
    if (normalized.isEmpty) {
      return null;
    }
    for (final group in _reviewGroups) {
      if (group.id == normalized) {
        return group;
      }
    }
    return null;
  }

  ReviewGroup? _resolvedActiveReviewGroup() {
    final activeId = _activeReviewGroupId.trim();
    if (activeId.isNotEmpty) {
      final explicit = _findReviewGroupById(activeId);
      if (explicit != null) {
        return explicit;
      }
    }
    final current = _currentReviewDiff();
    if (current != null) {
      final currentGroupId = _groupIdForDiff(current);
      if (currentGroupId.isNotEmpty) {
        final group = _findReviewGroupById(currentGroupId);
        if (group != null) {
          return group;
        }
      }
    }
    if (_reviewGroups.isEmpty) {
      return null;
    }
    for (final group in _reviewGroups) {
      if (group.pendingCount > 0) {
        return group;
      }
    }
    return _reviewGroups.first;
  }

  void _syncReviewGroupsFromRecentDiffs() {
    final grouped = <String, List<HistoryContext>>{};
    final preservedTitles = <String, String>{
      for (final group in _reviewGroups)
        if (group.id.isNotEmpty && group.title.isNotEmpty)
          group.id: group.title,
    };
    final preservedExecutionIds = <String, String>{
      for (final group in _reviewGroups)
        if (group.id.isNotEmpty && group.executionId.isNotEmpty)
          group.id: group.executionId,
    };

    for (final diff in _recentDiffs) {
      if (diff.diff.trim().isEmpty) {
        continue;
      }
      final groupId = _groupIdForDiff(diff);
      if (groupId.isEmpty) {
        continue;
      }
      grouped.putIfAbsent(groupId, () => []).add(diff);
    }

    final nextGroups = <ReviewGroup>[];
    for (final entry in grouped.entries) {
      final diffs = entry.value;
      final files = diffs
          .map(
            (diff) => ReviewFile(
              id: diff.id,
              path: diff.path,
              title: diff.title,
              diff: diff.diff,
              lang: diff.lang,
              pendingReview: diff.pendingReview,
              reviewStatus: diff.reviewStatus,
              executionId: diff.executionId,
            ),
          )
          .toList(growable: false);
      final pendingFiles = files.where((file) => file.pendingReview).toList();
      final acceptedCount =
          files.where((file) => file.reviewStatus == 'accepted').length;
      final revertedCount =
          files.where((file) => file.reviewStatus == 'reverted').length;
      final revisedCount =
          files.where((file) => file.reviewStatus == 'revised').length;
      final currentFile =
          pendingFiles.isNotEmpty ? pendingFiles.first : files.last;
      final groupTitle = diffs
              .map((diff) => diff.groupTitle.trim())
              .firstWhere((title) => title.isNotEmpty, orElse: () => '')
              .trim()
              .isNotEmpty
          ? diffs
              .map((diff) => diff.groupTitle.trim())
              .firstWhere((title) => title.isNotEmpty, orElse: () => '')
              .trim()
          : (preservedTitles[entry.key] ??
              (files.length > 1
                  ? '本轮修改 ${files.length} 个文件'
                  : currentFile.title));
      nextGroups.add(
        ReviewGroup(
          id: entry.key,
          title: groupTitle,
          executionId: diffs
                  .map((diff) => diff.executionId.trim())
                  .firstWhere((id) => id.isNotEmpty, orElse: () => '')
                  .trim()
                  .isNotEmpty
              ? diffs
                  .map((diff) => diff.executionId.trim())
                  .firstWhere((id) => id.isNotEmpty, orElse: () => '')
                  .trim()
              : (preservedExecutionIds[entry.key] ?? ''),
          pendingReview: pendingFiles.isNotEmpty,
          reviewStatus: pendingFiles.isNotEmpty
              ? 'pending'
              : _groupReviewStatusFromCounts(
                  acceptedCount: acceptedCount,
                  revertedCount: revertedCount,
                  revisedCount: revisedCount,
                ),
          currentFileId: currentFile.id,
          currentPath: currentFile.path,
          pendingCount: pendingFiles.length,
          acceptedCount: acceptedCount,
          revertedCount: revertedCount,
          revisedCount: revisedCount,
          files: files,
        ),
      );
    }

    _reviewGroups
      ..clear()
      ..addAll(nextGroups);
  }

  void _syncActiveReviewSelection() {
    _syncReviewGroupsFromRecentDiffs();
    final activeGroup = _resolvedActiveReviewGroup();
    if (activeGroup == null) {
      _activeReviewGroupId = '';
      _activeReviewDiffId = '';
      return;
    }
    _activeReviewGroupId = activeGroup.id;
    final activeDiff = _findPendingDiffById(_activeReviewDiffId);
    if (activeDiff != null && _groupIdForDiff(activeDiff) == activeGroup.id) {
      return;
    }
    final pendingInGroup = _pendingDiffs.where((diff) {
      return _groupIdForDiff(diff) == activeGroup.id;
    }).toList(growable: false);
    if (pendingInGroup.isNotEmpty) {
      _activeReviewDiffId = _diffIdentity(pendingInGroup.first);
      return;
    }
    if (activeGroup.currentFileId.trim().isNotEmpty) {
      final matchedById = _findPendingDiffById(activeGroup.currentFileId);
      if (matchedById != null) {
        _activeReviewDiffId = _diffIdentity(matchedById);
        return;
      }
    }
    if (activeGroup.currentPath.trim().isNotEmpty) {
      final matchedByPath = _recentDiffs.where((diff) {
        return _pathsMatch(diff.path, activeGroup.currentPath) &&
            diff.diff.isNotEmpty;
      }).toList(growable: false);
      if (matchedByPath.isNotEmpty) {
        _activeReviewDiffId = _diffIdentity(matchedByPath.first);
        return;
      }
    }
    if (activeGroup.files.isNotEmpty) {
      final fallback = activeGroup.files.last;
      final matched = _recentDiffs.where((diff) {
        return (fallback.id.isNotEmpty && diff.id == fallback.id) ||
            _pathsMatch(diff.path, fallback.path);
      }).toList(growable: false);
      if (matched.isNotEmpty) {
        _activeReviewDiffId = _diffIdentity(matched.first);
        return;
      }
    }
    _activeReviewDiffId = '';
  }

  String _reviewStatusFromDecision(String decision) {
    switch (decision) {
      case 'accept':
        return 'accepted';
      case 'revert':
        return 'reverted';
      case 'revise':
        return 'revised';
      default:
        return '';
    }
  }

  String _groupReviewStatusFromCounts({
    required int acceptedCount,
    required int revertedCount,
    required int revisedCount,
  }) {
    if (revisedCount > 0) {
      return 'revised';
    }
    if (revertedCount > 0 && acceptedCount == 0) {
      return 'reverted';
    }
    if (acceptedCount > 0 && revertedCount == 0) {
      return 'accepted';
    }
    if (acceptedCount == 0 && revertedCount == 0 && revisedCount == 0) {
      return '';
    }
    return 'mixed';
  }

  void _syncActiveReviewDiff() {
    _syncActiveReviewSelection();
  }

  HistoryContext? _findPendingDiffById(String diffId) {
    final normalized = diffId.trim();
    if (normalized.isEmpty) {
      return null;
    }
    for (final item in _pendingDiffs) {
      if (_diffIdentity(item) == normalized) {
        return item;
      }
    }
    for (final item in _recentDiffs) {
      if (_diffIdentity(item) == normalized && item.diff.isNotEmpty) {
        return item;
      }
    }
    return null;
  }

  void _mergeRecentDiff(HistoryContext diff) {
    final normalized = _normalizeHistoryDiff(diff);
    _recentDiffs.removeWhere((item) => _sameDiffIdentity(item, normalized));
    _recentDiffs.add(normalized);
    _syncActiveReviewDiff();
  }

  bool _sameDiffIdentity(HistoryContext left, HistoryContext right) {
    if (left.id.isNotEmpty && right.id.isNotEmpty) {
      return left.id == right.id;
    }
    final leftGroupId = _groupIdForDiff(left);
    final rightGroupId = _groupIdForDiff(right);
    if (leftGroupId.isNotEmpty && rightGroupId.isNotEmpty) {
      final leftPath = _normalizePath(left.path);
      final rightPath = _normalizePath(right.path);
      if (leftPath.isNotEmpty && rightPath.isNotEmpty) {
        return leftGroupId == rightGroupId && leftPath == rightPath;
      }
    }
    final leftPath = _normalizePath(left.path);
    final rightPath = _normalizePath(right.path);
    return leftPath.isNotEmpty && leftPath == rightPath;
  }

  String _normalizePath(String value) {
    return value
        .replaceAll('\\', '/')
        .replaceAll(RegExp(r'/+'), '/')
        .replaceFirst(RegExp(r'/$'), '')
        .trim();
  }

  bool _pathsMatch(String left, String right) {
    final a = _normalizePath(left);
    final b = _normalizePath(right);
    if (a.isEmpty || b.isEmpty) {
      return false;
    }
    return a == b || a.endsWith('/$b') || b.endsWith('/$a');
  }

  HistoryContext? _resolvedCurrentDiff() {
    final diff = _currentDiff;
    if (diff != null && diff.diff.isNotEmpty) {
      final pending = !isAutoAcceptMode &&
          (_pendingDiffForContextId(diff.runtimeMeta.contextId) != null ||
              _pendingDiffForPath(diff.path) != null);
      return HistoryContext(
        id: diff.runtimeMeta.contextId,
        type: 'diff',
        path: diff.path,
        title: diff.title,
        diff: diff.diff,
        lang: diff.lang,
        pendingReview: pending,
        source: diff.runtimeMeta.source,
        skillName: diff.runtimeMeta.skillName,
        executionId: diff.runtimeMeta.executionId,
        groupId: diff.runtimeMeta.groupId,
        groupTitle: diff.runtimeMeta.groupTitle,
        reviewStatus: pending ? 'pending' : '',
      );
    }
    final currentReview = _currentReviewDiff();
    if (currentReview != null) {
      return currentReview;
    }
    if (_recentDiffs.isEmpty) {
      return null;
    }
    return _recentDiffs.last;
  }

  List<HistoryContext> get _pendingDiffs => _recentDiffs
      .where((item) => item.pendingReview && item.diff.isNotEmpty)
      .toList(growable: false);

  HistoryContext? _nextPendingDiff() {
    final pending = _pendingDiffs;
    if (pending.isEmpty) {
      return null;
    }
    final activeId = _activeReviewDiffId.trim();
    if (activeId.isEmpty) {
      return pending.first;
    }
    final activeIndex =
        pending.indexWhere((item) => _diffIdentity(item) == activeId);
    if (activeIndex == -1) {
      return pending.first;
    }
    if (activeIndex + 1 < pending.length) {
      return pending[activeIndex + 1];
    }
    return pending.first;
  }

  HistoryContext? _currentReviewDiff() {
    final explicit = _findPendingDiffById(_activeReviewDiffId);
    if (explicit != null) {
      return explicit;
    }
    final pendingCurrent = _pendingDiffForCurrentDiff();
    if (pendingCurrent != null) {
      return pendingCurrent;
    }
    final openedPending = _pendingDiffForOpenedFile();
    if (openedPending != null) {
      return openedPending;
    }
    return _pendingDiffs.isEmpty ? null : _pendingDiffs.last;
  }

  HistoryContext? _pendingDiffForCurrentDiff() {
    final diff = _currentDiff;
    if (diff == null) {
      return null;
    }
    return _pendingDiffForContextId(diff.runtimeMeta.contextId) ??
        _pendingDiffForPath(diff.path);
  }

  HistoryContext? _diffForOpenedFile() {
    final path = _openedFile?.path ?? '';
    if (path.isEmpty) {
      return null;
    }
    for (final item in _recentDiffs.reversed) {
      if (_pathsMatch(item.path, path) && item.diff.isNotEmpty) {
        return item;
      }
    }
    return null;
  }

  HistoryContext? _pendingDiffForOpenedFile() {
    final diff = _diffForOpenedFile();
    if (diff?.pendingReview == true) {
      return diff;
    }
    return null;
  }

  void _syncStepSummary({
    required String message,
    required String status,
    required String tool,
    required String command,
    required String targetPath,
  }) {
    if (message == _lastStepMessage && status == _lastStepStatus) {
      return;
    }
    _lastStepMessage = message;
    _lastStepStatus = status;
    _currentStepSummary =
        message.trim().isNotEmpty ? message.trim() : status.trim();
    final labels = [
      _normalizeToolLabel(tool),
      _normalizeToolLabel(_toolLabelFromCommand(command)),
      _toolLabelFromPath(targetPath)
    ].where((item) => item.isNotEmpty).toList();
    _activityToolLabel = labels.isNotEmpty ? labels.first : _activityToolLabel;
  }

  String _normalizeToolLabel(String value) {
    final trimmed = value.trim();
    if (trimmed.isEmpty) {
      return '';
    }
    const knownTools = {
      'read': 'Read',
      'write': 'Write',
      'edit': 'Edit',
      'bash': 'Bash',
      'grep': 'Grep',
      'glob': 'Glob',
      'taskcreate': 'TaskCreate',
      'taskupdate': 'TaskUpdate',
      'tasklist': 'TaskList',
      'taskget': 'TaskGet',
      'webfetch': 'WebFetch',
      'websearch': 'WebSearch',
      'agent': 'Agent',
      'skill': 'Skill',
      'lsp': 'LSP',
    };
    final key = trimmed.toLowerCase().replaceAll(RegExp(r'[^a-z]'), '');
    return knownTools[key] ?? trimmed;
  }

  String _toolLabelFromCommand(String command) {
    final trimmed = command.trim();
    if (trimmed.isEmpty) {
      return '';
    }
    final parts = trimmed.split(RegExp(r'\s+'));
    return parts.isEmpty ? '' : parts.first;
  }

  String _toolLabelFromPath(String path) {
    final normalized = path.replaceAll('\\', '/').trim();
    if (normalized.isEmpty) {
      return '';
    }
    final index = normalized.lastIndexOf('/');
    return index == -1 ? normalized : normalized.substring(index + 1);
  }

  String _summaryFromHistoryContext(HistoryContext? context) {
    if (context == null) {
      return '';
    }
    return context.message.isNotEmpty ? context.message : context.title;
  }

  bool _shouldPreserveBlockingPrompt() {
    return _pendingInteraction?.isPermission == true ||
        _pendingPrompt?.looksLikePermissionPrompt == true ||
        _pendingInteraction?.isReview == true ||
        shouldShowReviewChoices;
  }

  void _syncRuntimePermissionMode() {
    final interactionMode =
        _pendingInteraction?.runtimeMeta.permissionMode.trim() ?? '';
    if (interactionMode.isNotEmpty) {
      _runtimePermissionMode = interactionMode;
      return;
    }
    final promptMode = _pendingPrompt?.runtimeMeta.permissionMode.trim() ?? '';
    if (promptMode.isNotEmpty) {
      _runtimePermissionMode = promptMode;
      return;
    }
    final sessionMode = _sessionState?.runtimeMeta.permissionMode.trim() ?? '';
    if (sessionMode.isNotEmpty) {
      _runtimePermissionMode = sessionMode;
      return;
    }
    final agentMode = _agentState?.runtimeMeta.permissionMode.trim() ?? '';
    if (agentMode.isNotEmpty) {
      _runtimePermissionMode = agentMode;
      return;
    }
    final resumeMode = _resumeRuntimeMeta.permissionMode.trim();
    if (resumeMode.isNotEmpty) {
      _runtimePermissionMode = resumeMode;
      return;
    }
    _runtimePermissionMode = '';
  }

  void _syncDerivedState() {
    _syncRuntimePermissionMode();
    _syncReviewGroupsFromRecentDiffs();
    _agentPhaseLabel = _compactAgentMessage();
    _syncActiveReviewSelection();
    final active = _connected &&
        (_agentState?.state == 'THINKING' ||
            _agentState?.state == 'RUNNING_TOOL');
    if (active) {
      _activityStartedAt ??= _agentState?.timestamp ?? DateTime.now();
      if (_activityToolLabel.isEmpty) {
        _activityToolLabel = _currentStep?.tool.isNotEmpty == true
            ? _currentStep!.tool
            : _agentState?.tool.isNotEmpty == true
                ? _agentState!.tool
                : _toolLabelFromCommand(_agentState?.command ?? '');
      }
    } else {
      _activityStartedAt = null;
      _activityToolLabel = '';
    }
    _activityVisible = active;
    if (_currentStepSummary.isEmpty && _currentStep != null) {
      _currentStepSummary = _summaryFromHistoryContext(_currentStep);
    }
    _syncActionNeededSignal();
  }

  void _syncActionNeededSignal() {
    final snapshot = _currentActionNeededSnapshot();
    if (snapshot == null) {
      _activeActionNeededSnapshot = null;
      return;
    }
    final current = _activeActionNeededSnapshot;
    if (current != null && current.key == snapshot.key) {
      return;
    }
    _activeActionNeededSnapshot = snapshot;
    if (_shouldSuppressNextActionNeededSignal) {
      _shouldSuppressNextActionNeededSignal = false;
      return;
    }
    _actionNeededSignal = ActionNeededSignal(
      id: ++_nextActionNeededSignalId,
      type: snapshot.type,
      message: snapshot.message,
      createdAt: DateTime.now(),
    );
  }

  _ActionNeededSnapshot? _currentActionNeededSnapshot() {
    if (!_connected || !_selectedSessionId.trim().isNotEmpty || isSessionBusy) {
      return null;
    }
    final interaction = pendingInteraction;
    final prompt = pendingPrompt;
    if (shouldShowReviewChoices || interaction?.isReview == true) {
      final diff = currentReviewDiff ?? nextPendingDiff ?? currentDiffContext;
      final identity = diff?.id.isNotEmpty == true
          ? diff!.id
          : diff?.path.isNotEmpty == true
              ? diff!.path
              : _selectedSessionId;
      return _ActionNeededSnapshot(
        type: ActionNeededType.review,
        key: 'review::$identity',
        message: 'Claude 需要你处理代码审核',
      );
    }
    if (hasPendingPermissionPrompt) {
      final identity = interaction?.contextId.isNotEmpty == true
          ? interaction!.contextId
          : interaction?.targetPath.isNotEmpty == true
              ? interaction!.targetPath
              : prompt?.runtimeMeta.contextId.isNotEmpty == true
                  ? prompt!.runtimeMeta.contextId
                  : prompt?.runtimeMeta.targetPath.isNotEmpty == true
                      ? prompt!.runtimeMeta.targetPath
                      : _selectedSessionId;
      return _ActionNeededSnapshot(
        type: ActionNeededType.permission,
        key: 'permission::$identity',
        message: 'Claude 需要你确认权限',
      );
    }
    final hasGenericPrompt =
        interaction != null || (prompt != null && prompt.hasVisiblePrompt);
    if (hasGenericPrompt) {
      final identity = interaction?.contextId.isNotEmpty == true
          ? interaction!.contextId
          : prompt?.runtimeMeta.contextId.isNotEmpty == true
              ? prompt!.runtimeMeta.contextId
              : _selectedSessionId;
      return _ActionNeededSnapshot(
        type: ActionNeededType.reply,
        key:
            'reply::$identity::${interaction?.message ?? prompt?.message ?? ''}',
        message: 'Claude 正在等待你的回复',
      );
    }
    final state = (_agentState?.state ?? '').trim().toUpperCase();
    if (state == 'WAIT_INPUT' && awaitInput) {
      final executionKey =
          _agentState?.runtimeMeta.executionId.isNotEmpty == true
              ? _agentState!.runtimeMeta.executionId
              : _agentState?.runtimeMeta.contextId.isNotEmpty == true
                  ? _agentState!.runtimeMeta.contextId
                  : _selectedSessionId;
      return _ActionNeededSnapshot(
        type: ActionNeededType.continueInput,
        key: 'continue::$executionKey',
        message: 'Claude 需要你继续输入',
      );
    }
    return null;
  }

  void _markActionNeededHandled() {
    _activeActionNeededSnapshot = null;
  }

  void _resetActionNeededTracking({bool suppressNextSignal = false}) {
    _actionNeededSignal = null;
    _activeActionNeededSnapshot = null;
    _shouldSuppressNextActionNeededSignal = suppressNextSignal;
  }

  bool _isReviewDecisionPrompt(PromptRequestEvent? prompt) {
    if (prompt == null) {
      return false;
    }
    final options = prompt.options
        .map((option) => option.value.trim().toLowerCase())
        .where((value) => value.isNotEmpty)
        .toSet();
    if (options.contains('accept') ||
        options.contains('revert') ||
        options.contains('revise')) {
      return true;
    }
    final message = prompt.message.trim().toLowerCase();
    return message.contains('accept') &&
        message.contains('revert') &&
        message.contains('revise');
  }

  bool _shouldHidePromptCard(PromptRequestEvent? prompt) {
    if (prompt == null) {
      return true;
    }
    if (isBypassPermissionsMode) {
      return true;
    }
    return _isReviewDecisionPrompt(prompt);
  }

  bool _isGenericContinuePrompt(PromptRequestEvent prompt) {
    final message = prompt.message.trim();
    if (message.isEmpty || prompt.options.isNotEmpty) {
      return false;
    }
    return message == 'AI 会话已就绪，可继续输入' || message == 'Claude 会话已就绪，可继续输入';
  }

  String _compactAgentMessage() {
    if (!_connected) {
      return _connecting ? '连接中' : '未连接';
    }
    final state = _agentState?.state ?? '';
    if (state == 'WAIT_INPUT' || awaitInput) {
      return '等待输入';
    }
    if (state == 'RUNNING_TOOL') {
      return '执行中';
    }
    if (state == 'THINKING') {
      return '思考中';
    }
    return '已连接';
  }

  void _appendTerminalLog(String stream, String message,
      {String executionId = ''}) {
    if (message.isEmpty) {
      return;
    }
    final normalizedStream = stream.trim().toLowerCase();
    if (normalizedStream == 'stderr') {
      _terminalStderr = _appendChunk(_terminalStderr, message);
    } else {
      _terminalStdout = _appendChunk(_terminalStdout, message);
    }
    _appendExecutionOutput(executionId, normalizedStream, message);
  }

  void _restoreTerminalLogs(Map<String, String> rawTerminalByStream) {
    _terminalStdout = rawTerminalByStream['stdout'] ?? '';
    _terminalStderr = rawTerminalByStream['stderr'] ?? '';
    _syncActiveTerminalExecution();
  }

  void _appendExecutionOutput(
      String executionId, String stream, String message) {
    final normalizedId = executionId.trim();
    if (normalizedId.isEmpty) {
      return;
    }
    final index = _terminalExecutions
        .indexWhere((item) => item.executionId == normalizedId);
    final current = index == -1
        ? TerminalExecution(executionId: normalizedId)
        : _terminalExecutions[index];
    final updated = TerminalExecution(
      executionId: current.executionId,
      command: current.command,
      cwd: current.cwd,
      startedAt: current.startedAt,
      completedAt: current.completedAt,
      running: current.running || current.completedAt == null,
      exitCode: current.exitCode,
      stdout: stream == 'stderr'
          ? current.stdout
          : _appendChunk(current.stdout, message),
      stderr: stream == 'stderr'
          ? _appendChunk(current.stderr, message)
          : current.stderr,
    );
    if (index == -1) {
      _terminalExecutions.add(updated);
    } else {
      _terminalExecutions[index] = updated;
    }
    _syncActiveTerminalExecution();
  }

  TerminalExecution? _resolvedActiveTerminalExecution() {
    if (_terminalExecutions.isEmpty) {
      return null;
    }
    final activeId = _activeTerminalExecutionId.trim();
    if (activeId.isNotEmpty) {
      for (final item in _terminalExecutions) {
        if (item.executionId == activeId) {
          return item;
        }
      }
    }
    return _terminalExecutions.last;
  }

  void _syncActiveTerminalExecution() {
    if (_terminalExecutions.isEmpty) {
      _activeTerminalExecutionId = '';
      return;
    }
    final active = _resolvedActiveTerminalExecution();
    _activeTerminalExecutionId =
        active?.executionId ?? _terminalExecutions.last.executionId;
  }

  String _appendChunk(String original, String chunk) {
    if (original.isEmpty) {
      return chunk;
    }
    return '$original$chunk';
  }

  HistoryContext? _pendingDiffForContextId(String contextId) {
    if (contextId.isEmpty) {
      return null;
    }
    for (final item in _recentDiffs.reversed) {
      if (item.pendingReview && item.id == contextId && item.diff.isNotEmpty) {
        return item;
      }
    }
    return null;
  }

  HistoryContext? _pendingDiffForPath(String path) {
    if (path.isEmpty) {
      return null;
    }
    for (final item in _recentDiffs.reversed) {
      if (item.pendingReview &&
          _pathsMatch(item.path, path) &&
          item.diff.isNotEmpty) {
        return item;
      }
    }
    return null;
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

  String _parentDirectory(String path) {
    final normalized = path.replaceAll('\\', '/').trim();
    if (normalized.isEmpty || normalized == '.' || normalized == '/') {
      return normalized.isEmpty ? '.' : normalized;
    }
    final withoutTrailing = normalized.endsWith('/')
        ? normalized.substring(0, normalized.length - 1)
        : normalized;
    final index = withoutTrailing.lastIndexOf('/');
    if (index <= 0) {
      return '.';
    }
    return withoutTrailing.substring(0, index);
  }
}
