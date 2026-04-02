import 'dart:async';
import 'dart:convert';

import 'package:flutter/foundation.dart';
import 'package:flutter_webrtc/flutter_webrtc.dart';
import 'package:shared_preferences/shared_preferences.dart';

import '../../core/config/app_config.dart';
import '../../data/models/events.dart';
import '../../data/models/runtime_meta.dart';
import '../../data/models/session_models.dart';
import '../../data/services/adb_webrtc_service.dart';
import '../../data/services/mobilevc_ws_service.dart';
import 'claude_model_utils.dart';
import 'session_display_text.dart';

enum ActionNeededType {
  continueInput,
  permission,
  review,
  plan,
  reply,
}

enum AppNotificationType {
  actionNeeded,
  assistantReply,
  error,
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

class AppNotificationSignal {
  const AppNotificationSignal({
    required this.id,
    required this.type,
    required this.title,
    required this.body,
    required this.createdAt,
  });

  final int id;
  final AppNotificationType type;
  final String title;
  final String body;
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

class _PendingAiPreference {
  const _PendingAiPreference({
    required this.model,
    required this.reasoningEffort,
  });

  final String model;
  final String reasoningEffort;
}

class _PermissionDecisionSelection {
  const _PermissionDecisionSelection({
    required this.decision,
    this.scope = '',
  });

  final String decision;
  final String scope;
}

@visibleForTesting
bool shouldPreserveAdbFailureStatus(String status) {
  final normalized = status.trim().toLowerCase();
  if (normalized.isEmpty) {
    return false;
  }
  const detailHints = <String>[
    'turn',
    'relay',
    '候选',
    '3478',
    '凭据',
    'external-ip',
    'ice 状态',
    'ice 收集',
    '信令状态',
    '统计:',
  ];
  return detailHints.any(normalized.contains);
}

class SessionController extends ChangeNotifier {
  SessionController({MobileVcWsService? service})
      : _service = service ?? MobileVcWsService();

  static const _prefsKey = 'mobilevc.app_config';
  final MobileVcWsService _service;
  final AdbWebRtcService _adbWebRtc = AdbWebRtcService();

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
  RuntimePhaseEvent? _runtimePhase;
  SessionStateEvent? _sessionState;
  RuntimeInfoResultEvent? _runtimeInfo;
  FileDiffEvent? _currentDiff;
  PromptRequestEvent? _pendingPrompt;
  InteractionRequestEvent? _pendingInteraction;
  final List<PlanQuestion> _pendingPlanQuestions = [];
  final Map<String, String> _pendingPlanAnswers = <String, String>{};
  int _pendingPlanQuestionIndex = 0;
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
  final List<PermissionRule> _sessionPermissionRules = [];
  final List<PermissionRule> _persistentPermissionRules = [];
  final List<TimelineItem> _timeline = [];
  final List<ReviewGroup> _reviewGroups = [];
  final List<TerminalExecution> _terminalExecutions = [];
  final List<RuntimeProcessItem> _runtimeProcesses = [];
  String _activeReviewGroupId = '';
  String _activeReviewDiffId = '';
  int _activeRuntimeProcessPid = 0;
  bool _runtimeProcessListLoading = false;
  bool _runtimeProcessLogLoading = false;
  RuntimeProcessLogResultEvent? _runtimeProcessLog;
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
  int _nextNotificationSignalId = 0;
  AppNotificationSignal? _notificationSignal;
  _ActionNeededSnapshot? _activeActionNeededSnapshot;
  bool _shouldSuppressNextActionNeededSignal = false;
  bool _autoSessionRequested = false;
  bool _autoSessionCreating = false;
  bool _isLoadingSession = false;
  String _pendingSessionTargetId = '';
  String _pendingAiLaunchEngine = '';
  bool _pendingAiLaunchAwaitingFirstInput = false;
  final Map<String, _PendingAiPreference> _pendingAiPreferences =
      <String, _PendingAiPreference>{};
  final Set<String> _pendingToggleSkillNames = <String>{};
  final Set<String> _pendingToggleMemoryIds = <String>{};
  bool _isSavingSkill = false;
  bool _isSavingMemory = false;
  bool _sessionPermissionRulesEnabled = true;
  bool _persistentPermissionRulesEnabled = false;
  SessionContext? _pendingSessionContextTarget;
  SessionContext? _pendingSessionContextRollback;
  final List<AdbDevice> _adbDevices = [];
  final List<String> _adbAvailableAvds = [];
  Uint8List? _adbFrameBytes;
  String _adbSelectedSerial = '';
  String _adbPreferredAvd = '';
  String _adbSelectedAvd = '';
  String _adbStatus = '';
  String _adbSuggestedAction = '';
  bool _adbAvailable = false;
  bool _adbStreaming = false;
  bool _adbEmulatorAvailable = false;
  int _adbFrameWidth = 0;
  int _adbFrameHeight = 0;
  int _adbFrameSeq = 0;
  int _adbFrameIntervalMs = 700;
  Timer? _adbRefreshTimer;
  Timer? _adbWebRtcStartTimeout;
  bool _adbWebRtcConnected = false;
  bool _adbWebRtcStarting = false;

  AppConfig get config => _config;
  String get currentAiEngine => _resolvedAiEngine(
        command: currentMeta.command,
        engine: currentMeta.engine,
      );
  String get displayAiEngine => _pendingAiLaunchEngine.trim().isNotEmpty
      ? _pendingAiLaunchEngine.trim()
      : currentAiEngine;
  String get selectedAiModel => _resolvedAiModel(
        currentAiEngine,
        currentMeta.model.isNotEmpty
            ? currentMeta.model
            : _configuredModelForEngine(currentAiEngine),
      );
  String get configuredAiModel => _resolvedAiModel(
        currentAiEngine,
        _configuredModelForEngine(currentAiEngine),
      );
  String get selectedAiReasoningEffort => _resolvedAiReasoningEffort(
        currentAiEngine,
        currentMeta.reasoningEffort.isNotEmpty
            ? currentMeta.reasoningEffort
            : _configuredReasoningEffortForEngine(currentAiEngine),
      );
  String get configuredAiReasoningEffort => _resolvedAiReasoningEffort(
        currentAiEngine,
        _configuredReasoningEffortForEngine(currentAiEngine),
      );
  bool get supportsAiModelSwitch =>
      currentAiEngine == 'claude' || currentAiEngine == 'codex';
  String get currentAiModelSummary => _aiModelSummary(
      currentAiEngine, selectedAiModel, selectedAiReasoningEffort);
  String get commandBarEngine =>
      shouldShowClaudeMode ? displayAiEngine : 'shell';
  String get commandBarModelSummary => _aiModelSummary(
        displayAiEngine,
        _resolvedAiModel(
          displayAiEngine,
          _configuredModelForEngine(displayAiEngine),
        ),
        _resolvedAiReasoningEffort(
          displayAiEngine,
          _configuredReasoningEffortForEngine(displayAiEngine),
        ),
      );
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
  List<RuntimeProcessItem> get runtimeProcesses =>
      List.unmodifiable(_runtimeProcesses);
  int get activeRuntimeProcessPid => _activeRuntimeProcessPid;
  RuntimeProcessItem? get activeRuntimeProcess =>
      _resolvedActiveRuntimeProcess();
  RuntimeProcessLogResultEvent? get activeRuntimeProcessLog =>
      _runtimeProcessLog;
  bool get runtimeProcessListLoading => _runtimeProcessListLoading;
  bool get runtimeProcessLogLoading => _runtimeProcessLogLoading;
  String get activeTerminalStdout =>
      activeTerminalExecution?.stdout ?? _terminalStdout;
  String get activeTerminalStderr =>
      activeTerminalExecution?.stderr ?? _terminalStderr;
  String get activeRuntimeProcessStdout => _runtimeProcessLog?.stdout ?? '';
  String get activeRuntimeProcessStderr => _runtimeProcessLog?.stderr ?? '';
  String get activeRuntimeProcessMessage => _runtimeProcessLog?.message ?? '';
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
  List<PermissionRule> get sessionPermissionRules =>
      List.unmodifiable(_sessionPermissionRules);
  List<PermissionRule> get persistentPermissionRules =>
      List.unmodifiable(_persistentPermissionRules);
  bool get sessionPermissionRulesEnabled => _sessionPermissionRulesEnabled;
  bool get persistentPermissionRulesEnabled =>
      _persistentPermissionRulesEnabled;
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
  int get permissionRuleCount =>
      _sessionPermissionRules.length + _persistentPermissionRules.length;
  String get permissionRuleSummary {
    final total = permissionRuleCount;
    if (total == 0) {
      return (_sessionPermissionRulesEnabled ||
              _persistentPermissionRulesEnabled)
          ? '默认'
          : '已关闭';
    }
    final enabledScopes = <String>[];
    if (_sessionPermissionRulesEnabled) {
      enabledScopes.add('会话');
    }
    if (_persistentPermissionRulesEnabled) {
      enabledScopes.add('长期');
    }
    final scopeText = enabledScopes.isEmpty ? '未启用' : enabledScopes.join(' / ');
    return '$total 条 · $scopeText';
  }

  List<AdbDevice> get adbDevices => List.unmodifiable(_adbDevices);
  List<String> get adbAvailableAvds => List.unmodifiable(_adbAvailableAvds);
  Uint8List? get adbFrameBytes => _adbFrameBytes;
  String get adbSelectedSerial => _adbSelectedSerial;
  String get adbPreferredAvd => _adbPreferredAvd;
  String get adbSelectedAvd => _adbSelectedAvd;
  String get adbStatus => _adbStatus;
  String get adbSuggestedAction => _adbSuggestedAction;
  bool get adbAvailable => _adbAvailable;
  bool get adbStreaming => _adbStreaming;
  bool get adbEmulatorAvailable => _adbEmulatorAvailable;
  int get adbFrameWidth => _adbFrameWidth;
  int get adbFrameHeight => _adbFrameHeight;
  int get adbFrameSeq => _adbFrameSeq;
  int get adbFrameIntervalMs => _adbFrameIntervalMs;
  RTCVideoRenderer get adbRenderer => _adbWebRtc.renderer;
  bool get adbWebRtcConnected => _adbWebRtcConnected;
  bool get adbWebRtcStarting => _adbWebRtcStarting;
  bool get hasAdbConnectedDevice =>
      _adbDevices.any((item) => item.state.trim().toLowerCase() == 'device');
  bool get canLaunchAdbEmulator =>
      _adbEmulatorAvailable && _adbAvailableAvds.isNotEmpty;
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
      pendingPrompt?.looksLikePermissionPrompt == true ||
      _runtimePhase?.isPermissionBlocked == true;
  bool get hasPendingPlanPrompt => pendingInteraction?.isPlan == true;
  bool get hasPendingPlanQuestions => _pendingPlanQuestions.isNotEmpty;
  PlanQuestion? get pendingPlanQuestion {
    if (!hasPendingPlanQuestions) {
      return null;
    }
    if (_pendingPlanQuestionIndex < 0 ||
        _pendingPlanQuestionIndex >= _pendingPlanQuestions.length) {
      return null;
    }
    return _pendingPlanQuestions[_pendingPlanQuestionIndex];
  }

  List<PlanQuestion> get pendingPlanQuestionList =>
      List.unmodifiable(_pendingPlanQuestions);
  Map<String, String> get pendingPlanAnswers =>
      Map.unmodifiable(_pendingPlanAnswers);
  int get pendingPlanQuestionIndex => _pendingPlanQuestionIndex;
  bool get isPlanSubmissionReady =>
      hasPendingPlanQuestions &&
      _pendingPlanQuestionIndex >= _pendingPlanQuestions.length;
  String get pendingPlanProgressLabel {
    if (!hasPendingPlanQuestions) {
      return '';
    }
    final current = _pendingPlanQuestionIndex + 1;
    final total = _pendingPlanQuestions.length;
    return current > total ? '$total/$total' : '$current/$total';
  }

  bool get hasVisiblePrompt =>
      pendingInteraction != null || pendingPrompt != null;
  bool get shouldShowPromptComposer =>
      hasVisiblePrompt &&
      !shouldShowReviewChoices &&
      !hasPendingPermissionPrompt &&
      !hasPendingPlanPrompt &&
      !hasPendingPlanQuestions;
  bool get shouldShowPermissionChoices =>
      hasPendingPermissionPrompt && !shouldShowReviewChoices;
  bool get shouldShowPlanChoices =>
      (hasPendingPlanPrompt || hasPendingPlanQuestions) &&
      !shouldShowReviewChoices;
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
  AppNotificationSignal? get notificationSignal => _notificationSignal;
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

  bool _reviewShouldAutoAccept(RuntimeMeta meta) {
    final source = meta.source.trim().toLowerCase();
    return source == 'review-auto-accepted';
  }

  bool get shouldShowReviewChoices {
    final state = (_agentState?.state ?? '').trim().toUpperCase();
    final interaction = pendingInteraction;
    final prompt = pendingPrompt;
    return currentReviewDiff != null &&
        state == 'WAIT_INPUT' &&
        !hasPendingPermissionPrompt &&
        !hasPendingPlanPrompt &&
        !hasPendingPlanQuestions &&
        (interaction?.isReview == true ||
            prompt?.looksLikeReviewPrompt == true ||
            hasPendingReview);
  }

  String _debugReviewStateSummary() {
    final prompt = _pendingPrompt;
    final currentReview = currentReviewDiff;
    final openedPending = openedFilePendingDiff;
    return 'awaitInput=$awaitInput, agentState=${_agentState?.state ?? '-'}, pendingPrompt=${prompt?.message.trim().isNotEmpty == true ? prompt!.message.trim() : '-'}, shouldShowReviewChoices=$shouldShowReviewChoices, currentReviewDiff=${currentReview?.path.isNotEmpty == true ? currentReview!.path : '-'}, openedFilePendingDiff=${openedPending?.path.isNotEmpty == true ? openedPending!.path : '-'}, openedFile=${_openedFile?.path.isNotEmpty == true ? _openedFile!.path : '-'}';
  }

  void _pushDebug(String label, [String? details]) {
    final suffix =
        details == null || details.trim().isEmpty ? '' : ' ${details.trim()}';
    debugPrint('[session] $label$suffix');
  }

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
    if (!_connected) {
      return false;
    }
    if (awaitInput) {
      return false;
    }
    if (_isClaudePendingReadyForInput) {
      return false;
    }
    final agentState = (_agentState?.state ?? '').trim().toUpperCase();
    if (agentState == 'THINKING' || agentState == 'RUNNING_TOOL') {
      return true;
    }
    final sessionState = (_sessionState?.state ?? '').trim().toUpperCase();
    if (sessionState == 'THINKING' || sessionState == 'RUNNING_TOOL') {
      return true;
    }
    return sessionState == 'RUNNING' && _activityVisible;
  }

  bool get _canBypassBusyGuardForCodexContinuation {
    if (!shouldShowClaudeMode) {
      return false;
    }
    final command = currentMeta.command.trim().toLowerCase();
    if (!(command == 'codex' || command.startsWith('codex '))) {
      return false;
    }
    return !awaitInput &&
        !hasPendingPermissionPrompt &&
        !hasPendingPlanQuestions &&
        !hasPendingPlanPrompt &&
        !shouldShowReviewChoices;
  }

  String get agentPhaseLabel => _agentPhaseLabel;
  bool get activityVisible => _activityVisible;
  bool get activityBannerVisible =>
      _activityVisible || _isClaudePendingReadyForInput;
  bool get activityBannerAnimated => _activityVisible;
  String get activityBannerTitle =>
      _isClaudePendingReadyForInput ? '待输入' : 'AI 助手正在运行中';
  String get activityBannerDetail {
    if (_isClaudePendingReadyForInput) {
      return 'Claude 已启动，请继续输入';
    }
    final label = _activityToolLabel.trim();
    if (label.isEmpty) {
      return '';
    }
    return '调用工具 · $label';
  }

  bool get activityBannerShowsElapsed => _activityVisible;
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
    final liveRuntimeMeta = (_agentState?.runtimeMeta ?? const RuntimeMeta())
        .merge(_sessionState?.runtimeMeta ?? const RuntimeMeta())
        .merge(_runtimeInfo?.runtimeMeta ?? const RuntimeMeta());
    const claudeStates = <String>{
      'starting',
      'active',
      'waiting_input',
      'resumable',
    };
    return claudeStates.contains(liveRuntimeMeta.claudeLifecycle.trim());
  }

  bool get shouldShowClaudeMode =>
      inClaudeMode || _pendingAiLaunchEngine.trim().isNotEmpty;

  bool get _canContinuePendingAiLaunch =>
      _pendingAiLaunchEngine.trim().isNotEmpty &&
      _pendingAiLaunchAwaitingFirstInput;

  bool get _isClaudePendingReadyForInput =>
      _pendingAiLaunchAwaitingFirstInput &&
      _pendingAiLaunchEngine.trim().toLowerCase() == 'claude';

  RuntimeMeta get currentMeta {
    final merged = (_agentState?.runtimeMeta ?? const RuntimeMeta())
        .merge(_sessionState?.runtimeMeta ?? const RuntimeMeta())
        .merge(_currentDiff?.runtimeMeta ?? const RuntimeMeta())
        .merge(_runtimeInfo?.runtimeMeta ?? const RuntimeMeta())
        .merge(_resumeRuntimeMeta);
    final runtimeCwd = merged.cwd.trim();
    final targetCwd = runtimeCwd.isNotEmpty ? runtimeCwd : effectiveCwd;
    final runtimeEngine = merged.engine.trim();
    final targetEngine =
        runtimeEngine.isNotEmpty ? runtimeEngine : _config.engine;
    return merged.merge(
      RuntimeMeta(
        engine: targetEngine,
        cwd: targetCwd,
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

  String _preferredAiCommandForEngine(String engine) {
    final normalizedEngine = engine.trim().toLowerCase();
    final parts = <String>[];
    switch (normalizedEngine) {
      case 'codex':
        parts.add('codex');
        parts.addAll([
          '-m',
          _resolvedAiModel('codex', _configuredModelForEngine('codex')),
        ]);
        final effort = _resolvedAiReasoningEffort(
          'codex',
          _configuredReasoningEffortForEngine('codex'),
        ).trim();
        if (effort.isNotEmpty) {
          parts.addAll(['--config', 'model_reasoning_effort=$effort']);
        }
        return parts.join(' ');
      case 'gemini':
        return 'gemini';
      default:
        parts.add('claude');
        final model =
            _resolvedAiModel('claude', _configuredModelForEngine('claude'))
                .trim();
        if (model.isNotEmpty) {
          parts.addAll(['--model', model]);
        }
        return parts.join(' ');
    }
  }

  void _setPendingAiLaunch(String engine) {
    final normalized = engine.trim().toLowerCase();
    if (normalized != 'claude' &&
        normalized != 'codex' &&
        normalized != 'gemini') {
      return;
    }
    _pendingAiLaunchEngine = normalized;
    if (_config.engine.trim().toLowerCase() != normalized) {
      unawaited(saveConfig(_config.copyWith(engine: normalized)));
    }
    _pendingAiPreferences.remove(normalized);
    notifyListeners();
  }

  void _clearPendingAiLaunch() {
    if (_pendingAiLaunchEngine.isEmpty) {
      return;
    }
    _pendingAiLaunchEngine = '';
    _pendingAiLaunchAwaitingFirstInput = false;
  }

  void _consumePendingAiLaunchInput() {
    if (!_pendingAiLaunchAwaitingFirstInput) {
      return;
    }
    _pendingAiLaunchAwaitingFirstInput = false;
    final meta = currentMeta;
    if (meta.command.trim().isNotEmpty ||
        meta.claudeLifecycle.trim().isNotEmpty) {
      _clearPendingAiLaunch();
    }
  }

  void _syncPendingAiLaunchFromRuntime() {
    if (_pendingAiLaunchEngine.isEmpty || _pendingAiLaunchAwaitingFirstInput) {
      return;
    }
    final meta = currentMeta;
    final hasRuntimeOwnership = meta.command.trim().isNotEmpty ||
        meta.claudeLifecycle.trim().isNotEmpty;
    final becameIdle = _isIdleLikeState(_agentState?.state ?? '') ||
        _isIdleLikeState(_sessionState?.state ?? '');
    if (hasRuntimeOwnership || becameIdle) {
      _clearPendingAiLaunch();
    }
  }

  bool _isAiCommand(String value) {
    final normalized = value.trim().toLowerCase();
    return normalized == 'claude' ||
        normalized.startsWith('claude ') ||
        normalized == 'codex' ||
        normalized.startsWith('codex ') ||
        normalized == 'gemini' ||
        normalized.startsWith('gemini ');
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
    _pushDebug('initialize start');
    try {
      await _restoreConfigFromPrefs();
    } catch (error, stack) {
      _pushDebug(
          'initialize prefs restore failed', 'errorType=${error.runtimeType}');
      debugPrintStack(
        stackTrace: stack,
        label: '[session] initialize prefs restore stack',
      );
    }
    _subscription = _service.events.listen(_handleEvent);
    _syncDerivedState();
    notifyListeners();
    _pushDebug('initialize end');
  }

  Future<void> _restoreConfigFromPrefs() async {
    final prefs = await SharedPreferences.getInstance();
    final raw = prefs.getString(_prefsKey);
    if (raw == null || raw.isEmpty) {
      _pushDebug('prefs restore skip', 'key=$_prefsKey empty=true');
      return;
    }

    try {
      final decoded = jsonDecode(raw);
      if (decoded is! Map<String, dynamic>) {
        throw const FormatException('App config JSON is not an object');
      }
      _config = AppConfig.fromJson(decoded);
      _pushDebug('prefs restore success', 'key=$_prefsKey');
    } catch (error, stack) {
      _config = const AppConfig();
      _pushDebug(
        'prefs restore fallback',
        'key=$_prefsKey errorType=${error.runtimeType} reset=true',
      );
      debugPrintStack(
        stackTrace: stack,
        label: '[session] prefs restore stack',
      );
      await prefs.remove(_prefsKey);
    }
  }

  Future<void> disposeController() async {
    _stopAdbRefreshPolling();
    _adbWebRtcStartTimeout?.cancel();
    await _subscription?.cancel();
    await _adbWebRtc.dispose();
    await _service.dispose();
  }

  Future<void> saveConfig(AppConfig config) async {
    _config = config;
    if (_currentDirectoryPath.trim().isEmpty ||
        _normalizePath(_currentDirectoryPath) == _normalizePath(config.cwd)) {
      _currentDirectoryPath = config.cwd.trim();
    }
    final prefs = await SharedPreferences.getInstance();
    try {
      await prefs.setString(_prefsKey, jsonEncode(config.toJson()));
    } catch (error, stack) {
      _pushDebug('save config failed',
          'key=$_prefsKey errorType=${error.runtimeType}');
      debugPrintStack(
        stackTrace: stack,
        label: '[session] save config stack',
      );
    }
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
      try {
        await prefs.setString(_prefsKey, jsonEncode(_config.toJson()));
      } catch (error, stack) {
        _pushDebug(
          'save cwd failed',
          'key=$_prefsKey errorType=${error.runtimeType}',
        );
        debugPrintStack(
          stackTrace: stack,
          label: '[session] save cwd stack',
        );
      }
    }
    if (refreshList && (!samePath || _currentDirectoryItems.isEmpty)) {
      _fileListLoading = true;
      _service.send(
          {'action': 'fs_list', if (nextPath.isNotEmpty) 'path': nextPath});
    }
    notifyListeners();
  }

  Future<void> connect() async {
    if (_isInvalidLoopbackHostForMobile()) {
      _connecting = false;
      _connected = false;
      _connectionMessage = 'iPhone 不能使用 localhost/127.0.0.1，请改成 Mac 的局域网 IP';
      _pushSystem('error', _connectionMessage);
      _syncDerivedState();
      notifyListeners();
      return;
    }
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
      _runtimePermissionMode = '';
      await switchWorkingDirectory(_config.cwd);
      requestRuntimeInfo('context');
      requestSkillCatalog();
      requestMemoryList();
      requestSessionContext();
      requestPermissionRuleList();
      requestReviewState();
      requestAdbDevices();
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
    _stopAdbRefreshPolling();
    await _adbWebRtc.stop();
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
    _resetRuntimeProcessState();
    _canResumeCurrentSession = false;
    _resumeRuntimeMeta = const RuntimeMeta();
    _pendingAiLaunchEngine = '';
    _runtimePermissionMode = '';
    _sessionContext = const SessionContext();
    _skillCatalogMeta = const CatalogMetadata(domain: 'skill');
    _memoryCatalogMeta = const CatalogMetadata(domain: 'memory');
    _skillSyncStatus = '';
    _memorySyncStatus = '';
    _skills.clear();
    _memoryItems.clear();
    _sessionPermissionRules.clear();
    _persistentPermissionRules.clear();
    _sessionPermissionRulesEnabled = true;
    _persistentPermissionRulesEnabled = false;
    _adbDevices.clear();
    _adbAvailableAvds.clear();
    _adbFrameBytes = null;
    _adbSelectedSerial = '';
    _adbPreferredAvd = '';
    _adbSelectedAvd = '';
    _adbStatus = '';
    _adbSuggestedAction = '';
    _adbAvailable = false;
    _adbStreaming = false;
    _adbEmulatorAvailable = false;
    _adbFrameWidth = 0;
    _adbFrameHeight = 0;
    _adbFrameSeq = 0;
    _adbWebRtcConnected = false;
    _adbWebRtcStarting = false;
    _agentState = null;
    _runtimePhase = null;
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

  void _handleUnexpectedSocketDisconnect(String message) {
    final normalized = message.trim().isEmpty ? '连接已断开' : message.trim();
    final alreadyDisconnected =
        !_connected && !_connecting && _connectionMessage == normalized;
    _connected = false;
    _connecting = false;
    _pendingAiLaunchEngine = '';
    _pendingAiLaunchAwaitingFirstInput = false;
    _pendingPrompt = null;
    _pendingInteraction = null;
    _runtimePhase = null;
    _resetActionNeededTracking();
    _connectionMessage = normalized;
    if (!alreadyDisconnected) {
      _pushSystem('error', normalized);
    }
  }

  void requestSessionList() {
    _service.send({'action': 'session_list', 'cwd': effectiveCwd});
  }

  void _handleAutoSessionBinding(List<SessionSummary> items) {
    if (!_connected || _connecting || _autoSessionRequested) {
      return;
    }
    if (_selectedSessionId.trim().isNotEmpty) {
      return;
    }
    if (_autoSessionCreating) {
      return;
    }
    _autoSessionRequested = true;
    if (items.isEmpty) {
      _autoSessionCreating = true;
      _requestAutoCreateSession();
      return;
    }
    _autoSessionCreating = false;
    _requestAutoLoadSession(items.first.id);
  }

  void createSession([String title = '']) {
    _beginSessionLoading();
    _service.send({
      'action': 'session_create',
      'cwd': effectiveCwd,
      if (title.isNotEmpty) 'title': title,
    });
  }

  void _requestAutoCreateSession() {
    _service.send({
      'action': 'session_create',
      'cwd': effectiveCwd,
      'reason': 'auto_bind',
    });
  }

  void loadSession(String sessionId) {
    final targetId = sessionId.trim();
    if (targetId.isEmpty) {
      return;
    }
    _beginSessionLoading(targetId: targetId);
    _service.send({
      'action': 'session_load',
      'sessionId': targetId,
      'cwd': effectiveCwd,
    });
  }

  void _requestAutoLoadSession(String sessionId) {
    final targetId = sessionId.trim();
    if (targetId.isEmpty) {
      return;
    }
    _service.send({
      'action': 'session_load',
      'sessionId': targetId,
      'cwd': effectiveCwd,
      'reason': 'auto_bind',
    });
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
    _pendingAiLaunchEngine = '';
    _pendingPrompt = null;
    _pendingInteraction = null;
    _clearPlanInteractionState();
    _runtimePhase = null;
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
    _resetRuntimeProcessState();
    _sessionPermissionRules.clear();
    _persistentPermissionRules.clear();
    _sessionPermissionRulesEnabled = true;
    _persistentPermissionRulesEnabled = false;
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

  Future<void> updateAiModelSelection({
    required String model,
    String reasoningEffort = '',
  }) async {
    final engine = currentAiEngine;
    if (!(engine == 'claude' || engine == 'codex')) {
      _pushSystem('session', '当前模式暂不支持快捷切换模型');
      return;
    }
    final normalizedModel = _resolvedAiModel(engine, model);
    final normalizedEffort =
        _resolvedAiReasoningEffort(engine, reasoningEffort);
    _pendingAiPreferences[engine] = _PendingAiPreference(
      model: normalizedModel,
      reasoningEffort: normalizedEffort,
    );
    await saveConfig(_config.copyWith(
      claudeModel: engine == 'claude' ? normalizedModel : _config.claudeModel,
      codexModel: engine == 'codex' ? normalizedModel : _config.codexModel,
      codexReasoningEffort:
          engine == 'codex' ? normalizedEffort : _config.codexReasoningEffort,
    ));
    _pushSystem(
      'session',
      engine == 'codex'
          ? 'Codex 模型已切换为 ${_codexModelLabel(normalizedModel)} · ${normalizedEffort.toUpperCase()}，将对下一次 Codex 启动生效'
          : 'Claude 模型已切换为 ${_claudeModelLabel(normalizedModel)}，将对下一次 Claude 启动生效',
    );
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
      base == null ? '请根据下面需求生成一个新的 AI 助手 skill。' : '请根据下面需求修改这个 AI 助手 skill。',
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
      '请根据下面需求修改这个 AI 助手 memory。',
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

  Future<void> prepareAdbDebug() async {
    await _adbWebRtc.ensureInitialized();
    requestAdbDevices();
  }

  void requestAdbDevices() {
    _service.send({'action': 'adb_devices'});
  }

  void selectAdbAvd(String value) {
    _adbSelectedAvd = value.trim();
    notifyListeners();
  }

  void setAdbFrameIntervalMs(int value) {
    if (value <= 0) {
      return;
    }
    _adbFrameIntervalMs = value;
    notifyListeners();
  }

  void startAdbStream({String serial = ''}) {
    unawaited(_startAdbStream(serial: serial));
  }

  Future<void> _startAdbStream({String serial = ''}) async {
    if (_isInvalidLoopbackHostForMobile()) {
      _adbStatus = 'iPhone 不能连接 localhost/127.0.0.1，请改成 Mac 的局域网 IP';
      _adbStreaming = false;
      _adbWebRtcConnected = false;
      _adbWebRtcStarting = false;
      notifyListeners();
      return;
    }
    final target =
        serial.trim().isNotEmpty ? serial.trim() : _adbSelectedSerial.trim();
    _adbWebRtcStartTimeout?.cancel();
    final forceRelay = _config.shouldForceAdbRelay;
    _adbStatus = forceRelay
        ? '正在建立 WebRTC + H264 调试链路（公网 relay 模式）…'
        : '正在建立 WebRTC + H264 调试链路…';
    _adbWebRtcStarting = true;
    notifyListeners();
    try {
      await _adbWebRtc.start(
        iceServers: _config.adbIceServers,
        forceRelay: forceRelay,
        onOfferReady: (sdpType, sdp) async {
          _service.send({
            'action': 'adb_webrtc_offer',
            if (target.isNotEmpty) 'serial': target,
            'sdpType': sdpType,
            'sdp': sdp,
            if (_config.adbIceServers.isNotEmpty)
              'iceServers': _config.adbIceServers,
          });
        },
        onConnectionState: _handleAdbWebRtcConnectionState,
        onDebug: (message) {
          _adbStatus = message;
          notifyListeners();
        },
      );
      _adbWebRtcStartTimeout = Timer(const Duration(seconds: 20), () async {
        if (_adbWebRtcConnected || _adbStreaming) {
          return;
        }
        _adbStatus = _config.adbIceServers.isEmpty
            ? 'WebRTC 建链超时，请配置 TURN/ICE 后重试'
            : (forceRelay
                ? 'WebRTC relay 建链超时，请检查 TURN 3478/UDP、3478/TCP 和凭据'
                : 'WebRTC 建链超时，请检查 TURN/ICE 配置');
        _adbWebRtcStarting = false;
        _adbStreaming = false;
        notifyListeners();
        await _adbWebRtc.stop();
      });
    } catch (error) {
      _adbStatus = 'WebRTC 启动失败：$error';
      _adbStreaming = false;
      _adbWebRtcConnected = false;
      _adbWebRtcStarting = false;
      _adbWebRtcStartTimeout?.cancel();
      notifyListeners();
    }
  }

  void stopAdbStream() {
    unawaited(_stopAdbStream());
  }

  Future<void> _stopAdbStream() async {
    _adbWebRtcStartTimeout?.cancel();
    _service.send({'action': 'adb_webrtc_stop'});
    await _adbWebRtc.stop();
    _adbStreaming = false;
    _adbWebRtcConnected = false;
    _adbWebRtcStarting = false;
    if (_adbStatus.trim().isEmpty) {
      _adbStatus = 'ADB WebRTC 调试已停止';
    }
    notifyListeners();
  }

  void launchAdbEmulator({String avd = ''}) {
    final target = avd.trim().isNotEmpty
        ? avd.trim()
        : (_adbSelectedAvd.trim().isNotEmpty
            ? _adbSelectedAvd.trim()
            : _adbPreferredAvd.trim());
    _adbStatus = '正在启动模拟器…';
    notifyListeners();
    _service.send({
      'action': 'adb_emulator_start',
      if (target.isNotEmpty) 'avd': target,
    });
    _startAdbRefreshPolling();
  }

  void sendAdbTap(int x, int y, {String serial = ''}) {
    if (x < 0 || y < 0) {
      return;
    }
    if (_adbWebRtc.canSendControl) {
      _adbWebRtc.sendTap(x, y);
      return;
    }
    _service.send({
      'action': 'adb_tap',
      if (serial.trim().isNotEmpty) 'serial': serial.trim(),
      'x': x,
      'y': y,
    });
  }

  void sendAdbKeyevent(String keycode, {String serial = ''}) {
    final normalized = keycode.trim();
    if (normalized.isEmpty) {
      return;
    }
    if (_adbWebRtc.canSendControl) {
      _adbWebRtc.sendKeyevent(normalized);
      return;
    }
    _service.send({
      'action': 'adb_keyevent',
      if (serial.trim().isNotEmpty) 'serial': serial.trim(),
      'keycode': normalized,
    });
  }

  void sendAdbSwipe(
    int startX,
    int startY,
    int endX,
    int endY, {
    String serial = '',
    int durationMs = 220,
  }) {
    if (startX < 0 || startY < 0 || endX < 0 || endY < 0) {
      return;
    }
    if (_adbWebRtc.canSendControl) {
      _adbWebRtc.sendSwipe(
        startX,
        startY,
        endX,
        endY,
        durationMs: durationMs,
      );
      return;
    }
    _service.send({
      'action': 'adb_swipe',
      if (serial.trim().isNotEmpty) 'serial': serial.trim(),
      'startX': startX,
      'startY': startY,
      'endX': endX,
      'endY': endY,
      'durationMs': durationMs,
    });
  }

  void _startAdbRefreshPolling() {
    _stopAdbRefreshPolling();
    var remaining = 30;
    _adbRefreshTimer = Timer.periodic(const Duration(seconds: 2), (timer) {
      if (!_connected || remaining <= 0) {
        timer.cancel();
        if (identical(_adbRefreshTimer, timer)) {
          _adbRefreshTimer = null;
        }
        return;
      }
      remaining -= 1;
      requestAdbDevices();
      if (hasAdbConnectedDevice) {
        _stopAdbRefreshPolling();
      }
    });
  }

  void _stopAdbRefreshPolling() {
    _adbRefreshTimer?.cancel();
    _adbRefreshTimer = null;
  }

  void _handleAdbWebRtcConnectionState(RTCPeerConnectionState state) {
    switch (state) {
      case RTCPeerConnectionState.RTCPeerConnectionStateConnected:
        _adbWebRtcStartTimeout?.cancel();
        _adbWebRtcStarting = false;
        _adbWebRtcConnected = true;
        _adbStreaming = true;
        _adbStatus = 'WebRTC 已连接，正在接收 H264 画面';
        break;
      case RTCPeerConnectionState.RTCPeerConnectionStateConnecting:
        _adbWebRtcStartTimeout?.cancel();
        _adbWebRtcStarting = false;
        _adbWebRtcConnected = false;
        _adbStreaming = true;
        _adbStatus = 'WebRTC 连接中…';
        break;
      case RTCPeerConnectionState.RTCPeerConnectionStateDisconnected:
        _adbWebRtcStartTimeout?.cancel();
        _adbWebRtcStarting = false;
        _adbWebRtcConnected = false;
        _adbStreaming = false;
        if (!shouldPreserveAdbFailureStatus(_adbStatus)) {
          _adbStatus = 'WebRTC 已断开';
        }
        break;
      case RTCPeerConnectionState.RTCPeerConnectionStateFailed:
        _adbWebRtcStartTimeout?.cancel();
        _adbWebRtcStarting = false;
        _adbWebRtcConnected = false;
        _adbStreaming = false;
        if (!shouldPreserveAdbFailureStatus(_adbStatus)) {
          _adbStatus = 'WebRTC 连接失败';
        }
        break;
      case RTCPeerConnectionState.RTCPeerConnectionStateClosed:
        _adbWebRtcStartTimeout?.cancel();
        _adbWebRtcStarting = false;
        _adbWebRtcConnected = false;
        _adbStreaming = false;
        if (!shouldPreserveAdbFailureStatus(_adbStatus)) {
          _adbStatus = 'WebRTC 已关闭';
        }
        break;
      default:
        break;
    }
    notifyListeners();
  }

  bool _isInvalidLoopbackHostForMobile() {
    if (kIsWeb) {
      return false;
    }
    switch (defaultTargetPlatform) {
      case TargetPlatform.iOS:
        final host = _config.host.trim().toLowerCase();
        return host == 'localhost' || host == '127.0.0.1';
      default:
        return false;
    }
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
    if (shouldShowClaudeMode) {
      _submitClaudeContinuation(value, meta: meta, label: label);
      return;
    }
    _startClaudeTurn(value, meta: meta, label: label);
  }

  void _startClaudeTurn(
    String prompt, {
    RuntimeMeta? meta,
    String label = '命令',
    String? targetEngine,
  }) {
    final value = prompt.trim();
    final resolvedEngine = (targetEngine ??
            _resolvedAiEngine(
              command: (meta ?? currentMeta).command,
              engine: (meta ?? currentMeta).engine,
            ))
        .trim()
        .toLowerCase();
    final preferredCommand = _preferredAiCommandForEngine(resolvedEngine);
    _setPendingAiLaunch(resolvedEngine);
    final mergedMeta = (meta ?? currentMeta).merge(
      RuntimeMeta(
        engine: resolvedEngine,
        command: resolvedEngine,
        cwd: effectiveCwd,
        permissionMode: _config.permissionMode,
      ),
    );
    _service.send({
      'action': 'exec',
      'cmd': preferredCommand,
      'cwd': effectiveCwd,
      'mode': 'pty',
      ...mergedMeta.toJson(),
      'permissionMode': _config.permissionMode,
    });
    if (value.isEmpty) {
      _pendingAiLaunchAwaitingFirstInput = true;
      return;
    }
    _pendingAiLaunchAwaitingFirstInput = false;
    _submitClaudeContinuation(value, meta: mergedMeta, label: label);
  }

  void _submitClaudeContinuation(
    String prompt, {
    RuntimeMeta? meta,
    String label = '回复',
  }) {
    final value = prompt.trim();
    if (value.isEmpty) {
      return;
    }
    _consumePendingAiLaunchInput();
    final continuationEngine = _resolvedAiEngine(
      command: (meta ?? currentMeta).command,
      engine: (meta ?? currentMeta).engine,
    );
    final mergedMeta = (meta ?? currentMeta).merge(
      RuntimeMeta(
        engine: continuationEngine,
        command: continuationEngine,
        cwd: effectiveCwd,
        permissionMode: _config.permissionMode,
      ),
    );
    _pushUser(value, label);
    _service.send({
      'action': 'input',
      'data': '$value\n',
      'cwd': effectiveCwd,
      ...mergedMeta.toJson(),
      'permissionMode': _config.permissionMode,
    });
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
      if (hasPendingPermissionPrompt) {
        _markActionNeededHandled();
        _sendPermissionDecision(
          _pendingPrompt,
          const _PermissionDecisionSelection(decision: 'approve'),
          promptLabel: '文件回复',
        );
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
    if (shouldShowClaudeMode) {
      _submitClaudeContinuation(prompt, meta: meta, label: '文件命令');
      return;
    }
    _startClaudeTurn(prompt, meta: meta, label: '文件命令');
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
    if (hasPendingPermissionPrompt && !shouldShowReviewChoices) {
      _pushSystem('session', '请先在上方完成授权');
      return;
    }
    if (hasPendingPlanQuestions) {
      _pushSystem('session', '请先在上方完成计划选择');
      return;
    }
    if (hasPendingPlanPrompt) {
      _pushSystem('session', '请先在上方完成计划选择');
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
    if (shouldShowClaudeMode) {
      if (isSessionBusy &&
          !_canBypassBusyGuardForCodexContinuation &&
          !_canContinuePendingAiLaunch) {
        _pushSystem('session', '当前 AI 助手会话仍在处理中，请稍后再试。');
        return;
      }
      _resetActionNeededTracking();
      _submitClaudeContinuation(value, label: '回复');
      return;
    }
    final lower = value.toLowerCase();
    final isAiCommand = _isAiCommand(lower);
    if (!isAiCommand) {
      _clearPendingAiLaunch();
      _service.send({
        'action': 'exec',
        'cmd': value,
        'cwd': effectiveCwd,
        'mode': 'pty',
        ...currentMeta.toJson(),
        'permissionMode': _config.permissionMode,
      });
      _pushUser(value, '命令');
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
    final aiHead = lower.split(RegExp(r'\s+')).first;
    final aiPrompt =
        lower == aiHead ? '' : value.substring(value.indexOf(' ') + 1).trim();
    _startClaudeTurn(
      aiPrompt,
      meta: currentMeta,
      label: '命令',
      targetEngine: aiHead,
    );
    if (lower == aiHead) {
      _pushUser(value, '命令');
    }
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
    final prompt = pendingPrompt;
    if (prompt?.looksLikeReviewPrompt == true) {
      sendReviewDecision(normalized);
      return;
    }
    if (prompt?.looksLikePermissionPrompt == true ||
        hasPendingPermissionPrompt) {
      final selection = _parsePermissionDecisionSelection(normalized);
      if (selection == null) {
        return;
      }
      _sendPermissionDecision(prompt, selection);
      return;
    }
    if (hasPendingPlanQuestions) {
      return;
    }
    if (hasPendingPlanPrompt) {
      return;
    }
    _submitAwaitingPrompt(normalized);
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
      if (hasPendingPermissionPrompt) {
        return;
      }
    } else if (hasPendingPermissionPrompt) {
      return;
    }
    if (!fallbackToInput && !awaitInput) {
      return;
    }
    _submitAwaitingInput(value, promptLabel: promptLabel);
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
      sendReviewDecision(normalized);
      return;
    }
    if (interaction.isPermission) {
      _sendInteractionDecision(interaction, normalized,
          promptLabel: promptLabel);
      return;
    }
    if (interaction.isPlan) {
      _sendPlanDecision(interaction, normalized, promptLabel: promptLabel);
      return;
    }
    _submitAwaitingInput(normalized, promptLabel: promptLabel);
  }

  void _sendPlanDecision(
    InteractionRequestEvent interaction,
    String decision, {
    String promptLabel = '回复',
  }) {
    final normalized = decision.trim();
    if (normalized.isEmpty) {
      return;
    }
    final planQuestions = _pendingPlanQuestions.isNotEmpty
        ? List<PlanQuestion>.from(_pendingPlanQuestions)
        : List<PlanQuestion>.from(interaction.planQuestions);
    if (planQuestions.isNotEmpty &&
        _pendingPlanQuestionIndex < planQuestions.length) {
      final currentQuestion = planQuestions[_pendingPlanQuestionIndex];
      final currentId = currentQuestion.id.trim().isNotEmpty
          ? currentQuestion.id.trim()
          : 'question-${_pendingPlanQuestionIndex + 1}';
      _pendingPlanAnswers[currentId] =
          _resolvePlanAnswerLabel(currentQuestion, normalized);
      final nextIndex = _pendingPlanQuestionIndex + 1;
      if (nextIndex < planQuestions.length) {
        _pendingPlanQuestionIndex = nextIndex;
        _pendingInteraction = interaction;
        _syncDerivedState();
        notifyListeners();
        return;
      }
    }
    final payload = _buildPlanDecisionPayload(
      interaction: interaction,
      lastDecision: normalized,
      planQuestions: planQuestions,
    );
    _pendingPlanAnswers.clear();
    _pendingPlanQuestions.clear();
    _pendingPlanQuestionIndex = 0;
    _pendingInteraction = null;
    _service.send({
      'action': 'plan_decision',
      'decision': payload,
      'permissionMode': _currentDecisionPermissionMode,
      'resumeSessionId': interaction.resumeSessionId,
      'executionId': interaction.executionId,
      'groupId': interaction.groupId,
      'groupTitle': interaction.groupTitle,
      'contextId': interaction.contextId,
      'contextTitle': interaction.contextTitle,
      'promptMessage': interaction.message,
      'command': currentMeta.command,
      'cwd': effectiveCwd,
      'engine': _config.engine,
      'target': currentMeta.target,
      'targetType': currentMeta.targetType,
      'targetPath': interaction.targetPath,
      'targetText': currentMeta.targetText,
    });
    _pushUser('Plan: $payload', promptLabel);
    _syncDerivedState();
    notifyListeners();
  }

  String _buildPlanDecisionPayload({
    required InteractionRequestEvent interaction,
    required String lastDecision,
    required List<PlanQuestion> planQuestions,
  }) {
    if (planQuestions.isEmpty) {
      return lastDecision;
    }
    final answers = <String, String>{};
    for (var index = 0; index < planQuestions.length; index++) {
      final question = planQuestions[index];
      final key = question.id.trim().isNotEmpty
          ? question.id.trim()
          : 'question-${index + 1}';
      final answer = _pendingPlanAnswers[key] ??
          _resolvePlanAnswerLabel(question, lastDecision);
      answers[key] = answer;
    }
    final payload = <String, Object?>{
      'kind': 'plan',
      'sessionId': interaction.sessionId,
      'resumeSessionId': interaction.resumeSessionId,
      'executionId': interaction.executionId,
      'groupId': interaction.groupId,
      'groupTitle': interaction.groupTitle,
      'contextId': interaction.contextId,
      'contextTitle': interaction.contextTitle,
      'targetPath': interaction.targetPath,
      'answers': answers,
    };
    return jsonEncode(payload);
  }

  String _resolvePlanAnswerLabel(PlanQuestion question, String value) {
    final normalized = value.trim();
    if (normalized.isEmpty) {
      return normalized;
    }
    for (final option in question.options) {
      if (option.value.trim() == normalized ||
          option.displayText == normalized) {
        return option.displayText;
      }
    }
    return normalized;
  }

  void _clearPlanInteractionState() {
    _pendingPlanQuestions.clear();
    _pendingPlanAnswers.clear();
    _pendingPlanQuestionIndex = 0;
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
      final selection = _parsePermissionDecisionSelection(decision);
      if (selection == null) {
        return;
      }
      _pendingInteraction = null;
      _service.send({
        'action': 'permission_decision',
        'decision': selection.decision,
        if (selection.scope.isNotEmpty) 'scope': selection.scope,
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
      _pushUser(
          'Permission: ${selection.decision}${selection.scope.isNotEmpty ? ' (${selection.scope})' : ''}',
          promptLabel);
      _syncDerivedState();
      notifyListeners();
      return;
    }
    if (interaction.isPlan) {
      _sendPlanDecision(interaction, decision, promptLabel: promptLabel);
      return;
    }
    _submitAwaitingInput(decision, promptLabel: promptLabel);
  }

  void _submitAwaitingInput(String value, {String promptLabel = '回复'}) {
    _pendingPrompt = null;
    _pendingInteraction = null;
    _clearPlanInteractionState();
    _service.send({
      'action': 'input',
      'data': '$value\n',
      'permissionMode': _config.permissionMode,
    });
    _pushUser(value, promptLabel);
    _syncDerivedState();
    notifyListeners();
  }

  void _sendPermissionDecision(
    PromptRequestEvent? prompt,
    _PermissionDecisionSelection selection, {
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
    final promptMessage = prompt?.message.trim().isNotEmpty == true
        ? prompt!.message
        : _runtimePhase?.message;
    _pendingPrompt = null;
    _pendingInteraction = null;
    _runtimePhase = null;
    _service.send({
      'action': 'permission_decision',
      'decision': selection.decision,
      if (selection.scope.isNotEmpty) 'scope': selection.scope,
      'permissionMode': _currentDecisionPermissionMode,
      'resumeSessionId': meta.resumeSessionId,
      'targetPath': targetPath,
      'contextId': meta.contextId,
      'contextTitle': contextTitle,
      'promptMessage': promptMessage,
      'command': meta.command,
      'cwd': effectiveCwd,
      'engine': _config.engine,
      'target': meta.target,
      'targetType': meta.targetType,
    });
    _pushUser(
        'Permission: ${selection.decision}${selection.scope.isNotEmpty ? ' (${selection.scope})' : ''}',
        promptLabel);
    _syncDerivedState();
    notifyListeners();
  }

  _PermissionDecisionSelection? _parsePermissionDecisionSelection(
      String value) {
    final normalized = value.trim().toLowerCase();
    if (normalized.isEmpty) {
      return null;
    }
    final parts = normalized.split(':');
    final decisionPart = parts.first.trim();
    final scopePart = parts.length > 1 ? parts.last.trim() : '';
    const approveValues = <String>{
      'y',
      'yes',
      'allow',
      'approve',
      'allowed',
      'approved',
      'ok',
      '允许',
      '同意',
    };
    const denyValues = <String>{
      'n',
      'no',
      'deny',
      'denied',
      'reject',
      'rejected',
      '拒绝',
      '取消',
    };
    final normalizedScope = switch (scopePart) {
      'session' => 'session',
      'persistent' => 'persistent',
      _ => '',
    };
    if (approveValues.contains(decisionPart)) {
      return _PermissionDecisionSelection(
        decision: 'approve',
        scope: normalizedScope,
      );
    }
    if (denyValues.contains(decisionPart)) {
      return const _PermissionDecisionSelection(decision: 'deny');
    }
    if (decisionPart == 'approve' || decisionPart == 'deny') {
      return _PermissionDecisionSelection(
        decision: decisionPart,
        scope: decisionPart == 'approve' ? normalizedScope : '',
      );
    }
    return null;
  }

  void requestRuntimeInfo(String query) {
    _service
        .send({'action': 'runtime_info', 'query': query, 'cwd': effectiveCwd});
  }

  void requestRuntimeProcessList() {
    _requestRuntimeProcessList();
  }

  void requestRuntimeProcessLog(int pid) {
    _requestRuntimeProcessLog(pid);
  }

  void requestPermissionRuleList() {
    _service.send({'action': 'permission_rule_list'});
  }

  void setPermissionRulesEnabled(String scope, bool enabled) {
    _service.send({
      'action': 'permission_rules_set_enabled',
      'scope': scope.trim().isEmpty ? 'session' : scope.trim(),
      'enabled': enabled,
    });
  }

  void setPermissionRuleEnabled(PermissionRule rule, bool enabled) {
    final updated = rule.copyWith(enabled: enabled);
    _service.send({
      'action': 'permission_rule_upsert',
      'rule': updated.toJson(),
    });
  }

  void deletePermissionRule(PermissionRule rule) {
    _service.send({
      'action': 'permission_rule_delete',
      'id': rule.id,
      'scope': rule.scope,
    });
  }

  void setActiveRuntimeProcess(int pid) {
    final normalized = pid;
    if (normalized <= 0) {
      if (_activeRuntimeProcessPid == 0 && _runtimeProcessLog == null) {
        return;
      }
      _activeRuntimeProcessPid = 0;
      _runtimeProcessLog = null;
      _runtimeProcessLogLoading = false;
      notifyListeners();
      return;
    }
    if (_activeRuntimeProcessPid == normalized &&
        _runtimeProcessLog?.pid == normalized &&
        !_runtimeProcessLogLoading) {
      return;
    }
    _requestRuntimeProcessLog(normalized);
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
    switch (event) {
      case SessionCreatedEvent created:
        _autoSessionRequested = false;
        _autoSessionCreating = false;
        _selectedSessionId = created.summary.id;
        _selectedSessionTitle = sessionDisplayTitle(created.summary);
        _resetRuntimeProcessState();
        _upsertSession(created.summary);
        requestPermissionRuleList();
        _finishSessionLoading(sessionId: created.summary.id);
        break;
      case SessionListResultEvent list:
        final mergedItems = list.items
            .map((item) => _mergedSessionSummary(
                  _sessions.cast<SessionSummary?>().firstWhere(
                        (existing) => existing?.id == item.id,
                        orElse: () => null,
                      ),
                  item,
                ))
            .toList();
        _sessions
          ..clear()
          ..addAll(mergedItems);
        _handleAutoSessionBinding(mergedItems);
        break;
      case SessionHistoryEvent history:
        _autoSessionRequested = false;
        _autoSessionCreating = false;
        _resetActionNeededTracking(suppressNextSignal: true);
        final resolvedHistorySummary =
            _resolvedHistorySummary(history.summary, history.logEntries);
        _selectedSessionId = resolvedHistorySummary.id;
        _selectedSessionTitle = sessionDisplayTitle(resolvedHistorySummary);
        _sessionContext = history.sessionContext;
        _skillCatalogMeta = history.skillCatalogMeta;
        _memoryCatalogMeta = history.memoryCatalogMeta;
        _runtimePhase = null;
        _runtimePermissionMode =
            history.resumeRuntimeMeta.permissionMode.trim();
        _upsertSession(resolvedHistorySummary);
        _timeline
          ..clear()
          ..addAll(history.logEntries
              .map(_timelineFromHistory)
              .where(_shouldKeepTimelineItem));
        _ensureVisibleHistoryForExternalCodex(history, resolvedHistorySummary);
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
        _resetRuntimeProcessState();
        requestPermissionRuleList();
        if (history.currentDiff != null) {
          final current = _normalizeHistoryDiff(history.currentDiff!);
          _mergeRecentDiff(current);
          _currentDiff = FileDiffEvent(
            timestamp: DateTime.now(),
            sessionId: resolvedHistorySummary.id,
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
                  sessionId: resolvedHistorySummary.id,
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
        if (_matchesPendingSessionTarget(resolvedHistorySummary.id)) {
          _finishSessionLoading(sessionId: resolvedHistorySummary.id);
        }
        final restoredCwd = history.resumeRuntimeMeta.cwd.trim();
        final targetCwd = restoredCwd.isNotEmpty ? restoredCwd : _config.cwd;
        await switchWorkingDirectory(targetCwd);
        break;
      case SessionStateEvent state:
        _sessionState = state;
        _maybeAutoSyncAiModel(state.runtimeMeta);
        _syncRuntimePermissionMode();
        if (_isLoadingSession &&
            _matchesPendingSessionTarget(state.sessionId)) {
          _finishSessionLoading(sessionId: state.sessionId);
        }
        if (_isIdleLikeState(state.state) && !_shouldPreserveBlockingPrompt()) {
          _pendingInteraction = null;
          _pendingPrompt = null;
          _runtimePhase = null;
          _agentState = null;
        }
        _connectionMessage =
            state.message.isNotEmpty ? state.message : state.state;
        _handleSessionStateTimeline(state);
        break;
      case AgentStateEvent agent:
        _agentState = agent;
        _maybeAutoSyncAiModel(agent.runtimeMeta);
        _syncRuntimePermissionMode();
        if (_isIdleLikeState(agent.state) && !_shouldPreserveBlockingPrompt()) {
          _pendingInteraction = null;
          _pendingPrompt = null;
          _runtimePhase = null;
        }
        _syncStepSummary(
          message: agent.step.isNotEmpty ? agent.step : agent.message,
          status: agent.state,
          tool: agent.tool,
          command: agent.command,
          targetPath: agent.runtimeMeta.targetPath,
        );
        break;
      case RuntimePhaseEvent runtimePhase:
        _runtimePhase = runtimePhase;
        _syncRuntimePermissionMode();
        break;
      case LogEvent log:
        _appendTerminalLog(log.stream, log.message,
            executionId: log.runtimeMeta.executionId);
        _maybeAutoSyncAiModel(
          log.runtimeMeta,
          rawText: log.message,
        );
        _handleLogTimeline(log);
        if (!_shouldPreserveBlockingPrompt() &&
            _looksLikeAssistantReply(log.message) &&
            (_agentState?.state.trim().toUpperCase() == 'THINKING' ||
                _agentState?.state.trim().toUpperCase() == 'RUNNING_TOOL' ||
                (_sessionState?.state.trim().toUpperCase() == 'RUNNING' &&
                    _activityVisible))) {
          _agentState = null;
        }
        break;
      case ProgressEvent progress:
        _maybeAutoSyncAiModel(
          progress.runtimeMeta,
          rawText: progress.message,
        );
        if (progress.message.isNotEmpty && _currentStepSummary.isEmpty) {
          _currentStepSummary = progress.message;
        }
        break;
      case ErrorEvent error:
        _fileListLoading = false;
        _fileReading = false;
        final errorMessage = error.message.trim();
        if (error.code == 'ws_closed' || error.code == 'ws_stream_error') {
          _handleUnexpectedSocketDisconnect(errorMessage);
          break;
        }
        if (_shouldSuppressIntentionalHandoffNoise(errorMessage)) {
          break;
        }
        if (errorMessage.contains('ADB') ||
            errorMessage.contains('adb ') ||
            errorMessage.contains('模拟器') ||
            errorMessage.contains('emulator') ||
            errorMessage.contains('WebRTC')) {
          _adbStatus = errorMessage;
          _adbStreaming = false;
          _adbWebRtcConnected = false;
          _adbWebRtcStarting = false;
          _adbWebRtcStartTimeout?.cancel();
        }
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
        final keepBlockingPrompt = _shouldKeepExistingBlockingPrompt(
            prompt, currentInteraction, currentPrompt);
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
        if (interaction.isPlan) {
          _pendingPlanQuestions
            ..clear()
            ..addAll(interaction.planQuestions);
          _pendingPlanAnswers.clear();
          _pendingPlanQuestionIndex = 0;
        } else {
          _clearPlanInteractionState();
        }
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
        final autoAccepted = _reviewShouldAutoAccept(diff.runtimeMeta);
        final historyDiff = HistoryContext(
          id: diff.runtimeMeta.contextId,
          type: 'diff',
          path: diff.path,
          title: diff.title,
          diff: diff.diff,
          lang: diff.lang,
          pendingReview: !autoAccepted,
          source: diff.runtimeMeta.source,
          skillName: diff.runtimeMeta.skillName,
          executionId: diff.runtimeMeta.executionId,
          groupId: diff.runtimeMeta.groupId,
          groupTitle: diff.runtimeMeta.groupTitle,
          reviewStatus: autoAccepted ? 'accepted' : 'pending',
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
        _maybeAutoSyncAiModel(
          runtimeInfo.runtimeMeta,
          runtimeInfo: runtimeInfo,
        );
        break;
      case RuntimeProcessListResultEvent result:
        _runtimeProcessListLoading = false;
        _runtimeProcesses
          ..clear()
          ..addAll(result.items);
        final activeStillExists = _runtimeProcesses
            .any((item) => item.pid == _activeRuntimeProcessPid);
        final nextPid = activeStillExists
            ? _activeRuntimeProcessPid
            : (_runtimeProcesses.isNotEmpty ? _runtimeProcesses.first.pid : 0);
        if (nextPid <= 0) {
          _activeRuntimeProcessPid = 0;
          _runtimeProcessLog = null;
          _runtimeProcessLogLoading = false;
          break;
        }
        _activeRuntimeProcessPid = nextPid;
        _requestRuntimeProcessLog(nextPid, notify: false);
        break;
      case RuntimeProcessLogResultEvent result:
        _runtimeProcessLogLoading = false;
        if (result.pid > 0) {
          _activeRuntimeProcessPid = result.pid;
        }
        _runtimeProcessLog = result;
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
      case PermissionRuleListResultEvent result:
        _sessionPermissionRulesEnabled = result.sessionEnabled;
        _persistentPermissionRulesEnabled = result.persistentEnabled;
        _sessionPermissionRules
          ..clear()
          ..addAll(result.sessionRules);
        _persistentPermissionRules
          ..clear()
          ..addAll(result.persistentRules);
        break;
      case PermissionAutoAppliedEvent result:
        if (result.message.trim().isNotEmpty) {
          _pushSystem('session', result.message.trim());
        }
        requestPermissionRuleList();
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
      case AdbDevicesResultEvent result:
        _adbDevices
          ..clear()
          ..addAll(result.devices);
        _adbAvailableAvds
          ..clear()
          ..addAll(result.availableAvds);
        _adbPreferredAvd = result.preferredAvd.trim();
        _adbAvailable = result.adbAvailable;
        _adbEmulatorAvailable = result.emulatorAvailable;
        _adbSuggestedAction = result.suggestedAction.trim();
        if (result.selectedSerial.trim().isNotEmpty) {
          _adbSelectedSerial = result.selectedSerial.trim();
        } else if (_adbSelectedSerial.trim().isNotEmpty &&
            !_adbDevices.any((item) => item.serial == _adbSelectedSerial)) {
          _adbSelectedSerial = '';
        }
        final selectedAvd = _adbSelectedAvd.trim();
        if (selectedAvd.isEmpty || !_adbAvailableAvds.contains(selectedAvd)) {
          if (_adbPreferredAvd.isNotEmpty &&
              _adbAvailableAvds.contains(_adbPreferredAvd)) {
            _adbSelectedAvd = _adbPreferredAvd;
          } else {
            _adbSelectedAvd =
                _adbAvailableAvds.isNotEmpty ? _adbAvailableAvds.first : '';
          }
        }
        if (result.message.trim().isNotEmpty) {
          _adbStatus = result.message.trim();
        }
        if (hasAdbConnectedDevice) {
          _stopAdbRefreshPolling();
        }
        break;
      case AdbStreamStateEvent state:
        _adbStreaming = state.running;
        if (state.serial.trim().isNotEmpty) {
          _adbSelectedSerial = state.serial.trim();
        }
        if (state.width > 0) {
          _adbFrameWidth = state.width;
        }
        if (state.height > 0) {
          _adbFrameHeight = state.height;
        }
        if (state.intervalMs > 0) {
          _adbFrameIntervalMs = state.intervalMs;
        }
        if (state.message.trim().isNotEmpty) {
          _adbStatus = state.message.trim();
        }
        if (!state.running && _adbStatus.trim().isEmpty) {
          _adbStatus = 'ADB 预览已停止';
        }
        break;
      case AdbFrameEvent frame:
        try {
          _adbFrameBytes = base64Decode(frame.image);
          _adbFrameSeq = frame.seq;
          if (frame.serial.trim().isNotEmpty) {
            _adbSelectedSerial = frame.serial.trim();
          }
          if (frame.width > 0) {
            _adbFrameWidth = frame.width;
          }
          if (frame.height > 0) {
            _adbFrameHeight = frame.height;
          }
          _adbStreaming = true;
          _adbStatus = 'ADB 画面预览中';
        } catch (_) {
          _adbStatus = 'ADB 帧解码失败';
        }
        break;
      case AdbWebRtcAnswerEvent answer:
        if (answer.serial.trim().isNotEmpty) {
          _adbSelectedSerial = answer.serial.trim();
        }
        unawaited(_adbWebRtc.applyAnswer(answer.sdpType, answer.sdp));
        _adbStatus = 'WebRTC answer 已收到，等待连接…';
        break;
      case AdbWebRtcStateEvent state:
        _adbStreaming = state.running;
        _adbWebRtcConnected = state.connected;
        if (state.running || state.connected) {
          _adbWebRtcStarting = false;
          _adbWebRtcStartTimeout?.cancel();
        }
        if (state.serial.trim().isNotEmpty) {
          _adbSelectedSerial = state.serial.trim();
        }
        if (state.width > 0) {
          _adbFrameWidth = state.width;
        }
        if (state.height > 0) {
          _adbFrameHeight = state.height;
        }
        if (state.message.trim().isNotEmpty) {
          _adbStatus = state.message.trim();
        }
        if (!state.running && !state.connected) {
          _adbWebRtcStarting = false;
        }
        break;
      case UnknownEvent unknown:
        _pushSystem('system', '收到未识别事件：${unknown.type}');
        break;
      default:
        break;
    }
    _syncPendingAiLaunchFromRuntime();
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

  bool _isIdleLikeState(String state) {
    final normalized = state.trim().toUpperCase();
    return normalized.isEmpty ||
        normalized == 'IDLE' ||
        normalized == 'DONE' ||
        normalized == 'COMPLETED' ||
        normalized == 'DISCONNECTED';
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
    final restoredBody = _restoredHistoryBody(entry);
    final restoredKind = _restoredHistoryKind(entry, restoredBody);
    return TimelineItem(
      id: 'history-$restoredKind-${entry.timestamp}-${restoredBody.hashCode}',
      kind: restoredKind,
      timestamp:
          DateTime.tryParse(entry.timestamp)?.toLocal() ?? DateTime.now(),
      title: entry.label,
      body: restoredBody,
      stream: entry.stream,
      context: entry.context,
    );
  }

  void _ensureVisibleHistoryForExternalCodex(
    SessionHistoryEvent history,
    SessionSummary summary,
  ) {
    if (_timeline.any(_hasVisibleTimelineContent)) {
      return;
    }
    final isExternal = summary.source == 'codex-native' ||
        summary.external ||
        history.resumeRuntimeMeta.engine.trim().toLowerCase() == 'codex' ||
        history.resumeRuntimeMeta.engine.trim().toLowerCase() == 'claude';
    if (!isExternal) {
      return;
    }
    final preview = sessionDisplayPreview(summary);
    final explicitPreview = summary.lastPreview.trim();
    final hasExplicitPreview = explicitPreview.isNotEmpty &&
        !looksLikeSessionNoiseText(explicitPreview) &&
        !looksLikeSessionBootstrapCommand(explicitPreview) &&
        !looksLikeSessionPlaceholderTitle(explicitPreview);
    final fallbackMessage =
        preview.isNotEmpty ? preview : '会话已恢复，可以继续对话（历史记录暂时不可用）';
    _timeline.add(
      TimelineItem(
        id: 'history-fallback-${summary.id}',
        kind: hasExplicitPreview &&
                (summary.source == 'codex-native' || summary.external)
            ? 'user'
            : 'system',
        timestamp: summary.updatedAt ?? summary.createdAt ?? DateTime.now(),
        body: fallbackMessage,
        meta: history.resumeRuntimeMeta,
      ),
    );
  }

  bool _hasVisibleTimelineContent(TimelineItem item) {
    return item.body.trim().isNotEmpty || item.title.trim().isNotEmpty;
  }

  String _restoredHistoryBody(HistoryLogEntry entry) {
    if (entry.kind == 'terminal') {
      final body = entry.text.isNotEmpty ? entry.text : entry.message;
      return _sanitizeAiBootstrapReply(body, entry.context?.command ?? '');
    }
    final body = entry.message.isNotEmpty ? entry.message : entry.text;
    return _sanitizeAiBootstrapReply(body, entry.context?.command ?? '');
  }

  String _restoredHistoryKind(HistoryLogEntry entry, String body) {
    if (entry.kind != 'terminal') {
      return entry.kind;
    }
    return _timelineKindForLog(body, entry.stream) ?? entry.kind;
  }

  void _upsertSession(SessionSummary summary) {
    final index = _sessions.indexWhere((item) => item.id == summary.id);
    final next = _mergedSessionSummary(
      index == -1 ? null : _sessions[index],
      summary,
    );
    if (index == -1) {
      _sessions.insert(0, next);
    } else {
      _sessions[index] = next;
    }
  }

  SessionSummary _resolvedHistorySummary(
    SessionSummary summary,
    List<HistoryLogEntry> entries,
  ) {
    final derivedPreview = _lastUserHistoryPreview(entries);
    return _mergedSessionSummary(
      _sessions.cast<SessionSummary?>().firstWhere(
            (item) => item?.id == summary.id,
            orElse: () => null,
          ),
      derivedPreview.isEmpty
          ? summary
          : SessionSummary(
              id: summary.id,
              title: summary.title,
              createdAt: summary.createdAt,
              updatedAt: summary.updatedAt,
              lastPreview: derivedPreview,
              entryCount: summary.entryCount,
              source: summary.source,
              external: summary.external,
              runtime: summary.runtime,
            ),
    );
  }

  SessionSummary _mergedSessionSummary(
    SessionSummary? existing,
    SessionSummary incoming,
  ) {
    final preservedTitle = _pickPreferredSessionTitle(
      existing?.title ?? '',
      incoming.title,
    );
    final preservedPreview = _pickPreferredSessionPreview(
      existing?.lastPreview ?? '',
      incoming.lastPreview,
    );
    final runtime =
        (existing?.runtime ?? const RuntimeMeta()).merge(incoming.runtime);
    return SessionSummary(
      id: incoming.id,
      title: preservedTitle,
      createdAt: incoming.createdAt ?? existing?.createdAt,
      updatedAt: incoming.updatedAt ?? existing?.updatedAt,
      lastPreview: preservedPreview,
      entryCount: incoming.entryCount != 0
          ? incoming.entryCount
          : existing?.entryCount ?? 0,
      source:
          incoming.source.isNotEmpty ? incoming.source : existing?.source ?? '',
      external: incoming.external || (existing?.external ?? false),
      runtime: runtime,
    );
  }

  String _pickPreferredSessionTitle(String existing, String incoming) {
    final normalizedIncoming = incoming.trim();
    final normalizedExisting = existing.trim();
    final incomingUsable = normalizedIncoming.isNotEmpty &&
        !looksLikeSessionNoiseText(normalizedIncoming) &&
        !looksLikeSessionBootstrapCommand(normalizedIncoming) &&
        !looksLikeSessionPlaceholderTitle(normalizedIncoming);
    if (incomingUsable) {
      return incoming;
    }
    final existingUsable = normalizedExisting.isNotEmpty &&
        !looksLikeSessionNoiseText(normalizedExisting) &&
        !looksLikeSessionBootstrapCommand(normalizedExisting) &&
        !looksLikeSessionPlaceholderTitle(normalizedExisting);
    if (existingUsable) {
      return existing;
    }
    return incoming;
  }

  String _pickPreferredSessionPreview(String existing, String incoming) {
    final normalizedIncoming = incoming.trim();
    final incomingUsable = normalizedIncoming.isNotEmpty &&
        !looksLikeSessionNoiseText(normalizedIncoming) &&
        !looksLikeSessionBootstrapCommand(normalizedIncoming) &&
        !looksLikeSessionPlaceholderTitle(normalizedIncoming);
    if (incomingUsable) {
      return incoming;
    }
    final normalizedExisting = existing.trim();
    final existingUsable = normalizedExisting.isNotEmpty &&
        !looksLikeSessionNoiseText(normalizedExisting) &&
        !looksLikeSessionBootstrapCommand(normalizedExisting) &&
        !looksLikeSessionPlaceholderTitle(normalizedExisting);
    if (existingUsable) {
      return existing;
    }
    return incoming;
  }

  String _lastUserHistoryPreview(List<HistoryLogEntry> entries) {
    for (final entry in entries.reversed) {
      final kind = entry.kind.trim().toLowerCase();
      if (kind != 'user') {
        continue;
      }
      final body = _restoredHistoryBody(entry).trim();
      if (body.isEmpty ||
          looksLikeSessionNoiseText(body) ||
          looksLikeSessionBootstrapCommand(body) ||
          looksLikeSessionPlaceholderTitle(body)) {
        continue;
      }
      return body;
    }
    return '';
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
    if (!_shouldKeepTimelineItem(item)) {
      return;
    }
    if (_shouldMergeIntoPreviousTimelineItem(item)) {
      final previous = _timeline.removeLast();
      _timeline.add(
        TimelineItem(
          id: previous.id,
          kind: previous.kind,
          timestamp: item.timestamp,
          title: previous.title,
          body: _mergeTimelineBodies(previous.body, item.body),
          stream: previous.stream,
          status: item.status.isNotEmpty ? item.status : previous.status,
          meta: item.meta,
          context: item.context ?? previous.context,
        ),
      );
      return;
    }
    _timeline.add(item);
    _emitTimelineNotification(item);
  }

  bool _shouldMergeIntoPreviousTimelineItem(TimelineItem item) {
    if (_timeline.isEmpty || item.kind != 'markdown') {
      return false;
    }
    final previous = _timeline.last;
    if (previous.kind != 'markdown') {
      return false;
    }
    if (previous.stream != item.stream) {
      return false;
    }
    if (previous.title.isNotEmpty || item.title.isNotEmpty) {
      return false;
    }
    final sameExecution =
        previous.meta.executionId.trim() == item.meta.executionId.trim();
    final sameContext =
        previous.meta.contextId.trim() == item.meta.contextId.trim();
    final sameCommand = previous.meta.command.trim().toLowerCase() ==
            item.meta.command.trim().toLowerCase() &&
        previous.meta.engine.trim().toLowerCase() ==
            item.meta.engine.trim().toLowerCase();
    if (!(sameExecution || sameContext || sameCommand)) {
      return false;
    }
    final gap = item.timestamp.difference(previous.timestamp).inMilliseconds;
    return gap >= 0 && gap <= 5000;
  }

  String _mergeTimelineBodies(String previous, String next) {
    if (previous.isEmpty) {
      return next;
    }
    if (next.isEmpty) {
      return previous;
    }
    if (_endsWithWhitespace(previous) || _startsWithWhitespace(next)) {
      return '$previous$next';
    }
    if (_endsWithSentencePunctuation(previous)) {
      if (_startsWithBlockLikeMarkdown(next)) {
        return '$previous\n\n$next';
      }
      if (!_startsWithClosingPunctuation(next) &&
          !_boundaryHasCjk(previous, next)) {
        return '$previous $next';
      }
    }
    return '$previous$next';
  }

  bool _endsWithWhitespace(String value) => RegExp(r'\s$').hasMatch(value);

  bool _startsWithWhitespace(String value) => RegExp(r'^\s').hasMatch(value);

  bool _endsWithSentencePunctuation(String value) {
    return RegExp(r'[.!?。！？:：;；]$').hasMatch(value);
  }

  bool _startsWithBlockLikeMarkdown(String value) {
    return RegExp(r'^(#{1,6}\s|[-*+]\s|>\s|```|\d+\.\s)').hasMatch(value);
  }

  bool _startsWithClosingPunctuation(String value) {
    return RegExp(r'^[)\]}>.,!?;:，。！？；：]').hasMatch(value);
  }

  bool _boundaryHasCjk(String previous, String next) {
    final previousRune = previous.runes.isEmpty ? null : previous.runes.last;
    final nextRune = next.runes.isEmpty ? null : next.runes.first;
    if (previousRune == null || nextRune == null) {
      return false;
    }
    return _isCjkRune(previousRune) || _isCjkRune(nextRune);
  }

  bool _isCjkRune(int rune) {
    return (rune >= 0x3400 && rune <= 0x4DBF) ||
        (rune >= 0x4E00 && rune <= 0x9FFF) ||
        (rune >= 0xF900 && rune <= 0xFAFF);
  }

  bool _shouldKeepTimelineItem(TimelineItem item) {
    if (item.body.trim().isEmpty && item.title.trim().isEmpty) {
      return false;
    }
    if (_shouldHideTimelineLogMessage(item.body, item.stream)) {
      return false;
    }
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
    if (_shouldSuppressIntentionalHandoffNoise(normalizedMessage)) {
      _lastSessionTimelineKey = key;
      return;
    }
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
    final message =
        _sanitizeAiBootstrapLogMessage(log.message, log.runtimeMeta);
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

    final kind = _timelineKindForLog(
      message,
      log.stream,
      meta: log.runtimeMeta,
    );
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

  String _sanitizeAiBootstrapLogMessage(String message, RuntimeMeta meta) {
    return _sanitizeAiBootstrapReply(
      message,
      _timelineAiEngine(meta),
    );
  }

  String _sanitizeAiBootstrapReply(String message, String engineHint) {
    final trimmed = message.trim();
    if (trimmed.isEmpty) {
      return message;
    }
    final lower = trimmed.toLowerCase();
    final normalizedEngine = engineHint.trim().toLowerCase();
    final isCodex =
        normalizedEngine == 'codex' || normalizedEngine.startsWith('codex ');
    if (!isCodex) {
      return message;
    }
    if (!(lower.contains('reasoning effort') ||
        lower.contains('what would you like to work on next') ||
        lower.contains('how can i help you') ||
        lower.contains('model set to'))) {
      return message;
    }
    final extracted = _extractCodexGreeting(trimmed);
    return extracted.isEmpty ? message : extracted;
  }

  String _extractCodexGreeting(String message) {
    final lines = message
        .split('\n')
        .map((line) => line.trim())
        .where((line) => line.isNotEmpty)
        .toList();
    for (final line in lines) {
      final lower = line.toLowerCase();
      if (lower.contains('how can i help you')) {
        final match = RegExp(r'how can i help you\??', caseSensitive: false)
            .firstMatch(line);
        return match?.group(0)?.trim() ?? line;
      }
      if (lower.contains('what would you like to work on next')) {
        final match = RegExp(
          r'what would you like to work on next\??',
          caseSensitive: false,
        ).firstMatch(line);
        return match?.group(0)?.trim() ?? line;
      }
    }
    final sentenceMatch = RegExp(
      r'(How can I help you\??|What would you like to work on next\??)',
      caseSensitive: false,
    ).firstMatch(message);
    if (sentenceMatch != null) {
      return sentenceMatch.group(0)?.trim() ?? '';
    }
    return '';
  }

  String? _timelineKindForLog(
    String message,
    String stream, {
    RuntimeMeta meta = const RuntimeMeta(),
  }) {
    final trimmed = message.trim();
    if (trimmed.isEmpty) {
      return null;
    }
    if (_shouldHideTimelineLogMessage(trimmed, stream)) {
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
    if (_shouldPreferAssistantText(meta, trimmed)) {
      return 'markdown';
    }
    if (_looksLikeAssistantReply(trimmed)) {
      return 'markdown';
    }
    return 'terminal';
  }

  bool _shouldPreferAssistantText(RuntimeMeta meta, String message) {
    final normalizedEngine = _timelineAiEngine(meta);
    if (normalizedEngine != 'claude' && normalizedEngine != 'codex') {
      return false;
    }
    if (message.isEmpty) {
      return false;
    }
    if (_looksLikeFrontendToolResultNoise(message) ||
        _looksLikeTerminalOutput(message) ||
        _looksLikeProcessNoise(message)) {
      return false;
    }
    return true;
  }

  String _timelineAiEngine(RuntimeMeta meta) {
    final engine = meta.engine.trim().toLowerCase();
    if (engine == 'claude' || engine == 'codex') {
      return engine;
    }
    final command = meta.command.trim().toLowerCase();
    if (command == 'claude' || command.startsWith('claude ')) {
      return 'claude';
    }
    if (command == 'codex' || command.startsWith('codex ')) {
      return 'codex';
    }
    return '';
  }

  bool _shouldSuppressIntentionalHandoffNoise(String message) {
    final trimmed = message.trim().toLowerCase();
    if (trimmed.isEmpty) {
      return false;
    }
    final runtimeMessage = _runtimePhase?.message.trim().toLowerCase() ?? '';
    final temporaryHandoff = _runtimePermissionMode.trim() == 'acceptEdits' ||
        runtimeMessage.contains('权限') ||
        runtimeMessage.contains('permission') ||
        runtimeMessage.contains('授权');
    if (!temporaryHandoff) {
      return false;
    }
    return trimmed == 'command finished with error' ||
        trimmed.contains('signal: killed') ||
        trimmed.contains('command exited with code -1');
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

  bool _shouldHideTimelineLogMessage(String message, String stream) {
    final trimmed = message.trim();
    if (trimmed.isEmpty) {
      return false;
    }
    final lower = trimmed.toLowerCase();
    if (lower.contains('codex_core::tools::router')) {
      return true;
    }
    if (lower.contains(
        'fatal: not a git repository (or any of the parent directories): .git')) {
      return true;
    }
    if (lower.startsWith('wall time:')) {
      return true;
    }
    if (lower.startsWith('output fatal: not a git repository')) {
      return true;
    }
    if (lower.contains('\noutput:\n') || lower.contains('\noutput\n')) {
      return true;
    }
    if (stream.trim().toLowerCase() == 'stderr' &&
        lower.startsWith('error=exit code:')) {
      return true;
    }
    return false;
  }

  bool _looksLikeProcessNoise(String message) {
    if (looksLikeSessionNoiseText(message) ||
        looksLikeSessionBootstrapCommand(message)) {
      return true;
    }
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
        lower == 'command finished' ||
        lower.startsWith('command finished ') ||
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
    if (looksLikeSessionNoiseText(trimmed) ||
        looksLikeSessionBootstrapCommand(trimmed)) {
      return true;
    }
    final lower = trimmed.toLowerCase();
    return lower.startsWith('[debug]') ||
        trimmed == 'AI 会话已续接' ||
        lower == 'ai 会话已续接' ||
        lower.startsWith('command started');
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
      pendingReview: item.pendingReview,
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
      pendingReview: file.pendingReview,
      reviewStatus: file.reviewStatus,
      executionId: file.executionId,
    );
  }

  ReviewGroup _normalizeReviewGroup(ReviewGroup group) {
    return ReviewGroup(
      id: group.id,
      title: group.title,
      executionId: group.executionId,
      pendingReview: group.pendingReview,
      reviewStatus: group.reviewStatus,
      currentFileId: group.currentFileId,
      currentPath: group.currentPath,
      pendingCount: group.pendingCount,
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
      final pending =
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
        _runtimePhase?.isPermissionBlocked == true ||
        _pendingInteraction?.isReview == true ||
        shouldShowReviewChoices ||
        _pendingInteraction?.isPlan == true ||
        hasPendingPlanQuestions ||
        pendingPrompt != null;
  }

  bool _shouldKeepExistingBlockingPrompt(
    PromptRequestEvent incoming,
    InteractionRequestEvent? currentInteraction,
    PromptRequestEvent? currentPrompt,
  ) {
    if (incoming.message.trim().isEmpty) {
      return currentInteraction?.isPermission == true ||
          currentInteraction?.isReview == true ||
          currentInteraction?.isPlan == true ||
          currentPrompt != null ||
          _runtimePhase?.isPermissionBlocked == true;
    }
    if (!_looksLikeGenericReadyPrompt(incoming)) {
      return false;
    }
    return currentInteraction?.isPermission == true ||
        currentInteraction?.isReview == true ||
        currentInteraction?.isPlan == true ||
        currentPrompt?.looksLikePermissionPrompt == true ||
        currentPrompt?.looksLikeReviewPrompt == true ||
        _runtimePhase?.isPermissionBlocked == true ||
        hasPendingPlanQuestions;
  }

  bool _looksLikeGenericReadyPrompt(PromptRequestEvent? prompt) {
    if (prompt == null) {
      return false;
    }
    if (prompt.options.isNotEmpty) {
      return false;
    }
    final message = prompt.message.trim().toLowerCase();
    if (message.isEmpty) {
      return false;
    }
    return message.contains('会话已就绪') ||
        message.contains('可继续输入') ||
        message.contains('waiting for input') ||
        message.contains('continue input') ||
        message.contains('ready for input') ||
        message == 'ready';
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
    final state = (_agentState?.state ?? '').trim().toUpperCase();
    final hasBlockingPrompt = awaitInput ||
        hasPendingPermissionPrompt ||
        shouldShowReviewChoices ||
        hasPendingPlanQuestions;
    final active = _connected &&
        !hasBlockingPrompt &&
        !_isClaudePendingReadyForInput &&
        (state == 'THINKING' || state == 'RUNNING_TOOL');
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
      _actionNeededSignal = null;
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
    final isInitialDisconnectedState =
        !_connecting && _connectionMessage == '未连接';
    if (isInitialDisconnectedState || _isLoadingSession) {
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
        message: 'AI 助手需要你处理代码审核',
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
        message: 'AI 助手需要你确认权限',
      );
    }
    if (hasPendingPlanPrompt || hasPendingPlanQuestions) {
      final interactionIdentity = interaction?.contextId.isNotEmpty == true
          ? interaction!.contextId
          : interaction?.targetPath.isNotEmpty == true
              ? interaction!.targetPath
              : _selectedSessionId;
      return _ActionNeededSnapshot(
        type: ActionNeededType.plan,
        key: 'plan::$interactionIdentity::$pendingPlanQuestionIndex',
        message: 'AI 助手需要你完成计划选择',
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
        message: 'AI 助手正在等待你的回复',
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
        message: 'AI 助手需要你继续输入',
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

  void _emitTimelineNotification(TimelineItem item) {
    final body = item.body.trim();
    if (body.isEmpty) {
      return;
    }
    final type = switch (item.kind) {
      'markdown' => AppNotificationType.assistantReply,
      'error' => AppNotificationType.error,
      _ => null,
    };
    if (type == null) {
      return;
    }
    _notificationSignal = AppNotificationSignal(
      id: ++_nextNotificationSignalId,
      type: type,
      title: 'MobileVC',
      body: _notificationPreview(body),
      createdAt: DateTime.now(),
    );
  }

  String _notificationPreview(String text) {
    final normalized = text.replaceAll(RegExp(r'\s+'), ' ').trim();
    if (normalized.length <= 120) {
      return normalized;
    }
    return '${normalized.substring(0, 117)}...';
  }

  bool _shouldHidePromptCard(PromptRequestEvent? prompt) {
    if (prompt == null) {
      return true;
    }
    if (isBypassPermissionsMode) {
      return true;
    }
    return false;
  }

  String _compactAgentMessage() {
    if (!_connected) {
      return _connecting ? '连接中' : '未连接';
    }
    if (_isClaudePendingReadyForInput) {
      return '待输入';
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

  void _requestRuntimeProcessList({bool notify = true}) {
    _runtimeProcessListLoading = true;
    _service.send({'action': 'runtime_process_list'});
    if (notify) {
      notifyListeners();
    }
  }

  void _requestRuntimeProcessLog(int pid, {bool notify = true}) {
    final normalized = pid;
    if (normalized <= 0) {
      return;
    }
    _activeRuntimeProcessPid = normalized;
    _runtimeProcessLogLoading = true;
    if (_runtimeProcessLog?.pid != normalized) {
      _runtimeProcessLog = null;
    }
    _service.send({'action': 'runtime_process_log_get', 'pid': normalized});
    if (notify) {
      notifyListeners();
    }
  }

  RuntimeProcessItem? _resolvedActiveRuntimeProcess() {
    if (_runtimeProcesses.isEmpty) {
      return null;
    }
    final activePid = _activeRuntimeProcessPid;
    if (activePid > 0) {
      for (final item in _runtimeProcesses) {
        if (item.pid == activePid) {
          return item;
        }
      }
    }
    return _runtimeProcesses.first;
  }

  void _resetRuntimeProcessState() {
    _runtimeProcessListLoading = false;
    _runtimeProcessLogLoading = false;
    _runtimeProcesses.clear();
    _activeRuntimeProcessPid = 0;
    _runtimeProcessLog = null;
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

  void _maybeAutoSyncAiModel(
    RuntimeMeta meta, {
    String rawText = '',
    RuntimeInfoResultEvent? runtimeInfo,
  }) {
    final engine = _resolvedAiEngine(
      command: meta.command.isNotEmpty ? meta.command : currentMeta.command,
      engine: meta.engine.isNotEmpty ? meta.engine : currentMeta.engine,
    );
    if (!(engine == 'claude' || engine == 'codex')) {
      return;
    }

    String nextModel = meta.model.trim();
    String nextEffort = meta.reasoningEffort.trim().toLowerCase();

    if (runtimeInfo != null &&
        runtimeInfo.query.trim().toLowerCase() == 'model') {
      final activeItem =
          runtimeInfo.items.where((item) => item.label == 'active_ai');
      if (activeItem.isNotEmpty) {
        final parsed =
            _parseAiModelFromText(engine, activeItem.first.value.trim());
        nextModel = parsed.$1.isNotEmpty ? parsed.$1 : nextModel;
        nextEffort = parsed.$2.isNotEmpty ? parsed.$2 : nextEffort;
      }
    }

    if (rawText.trim().isNotEmpty) {
      final parsed = _parseAiModelFromText(engine, rawText);
      nextModel = parsed.$1.isNotEmpty ? parsed.$1 : nextModel;
      nextEffort = parsed.$2.isNotEmpty ? parsed.$2 : nextEffort;
    }

    if (nextModel.isEmpty) {
      return;
    }
    final normalizedModel = nextModel.trim();
    final normalizedEffort = nextEffort.trim().toLowerCase();
    final pendingPreference = _pendingAiPreferences[engine];
    if (pendingPreference != null) {
      final matchesPendingModel = normalizedModel == pendingPreference.model;
      final matchesPendingEffort = engine != 'codex' ||
          normalizedEffort.isEmpty ||
          normalizedEffort == pendingPreference.reasoningEffort;
      if (!matchesPendingModel || !matchesPendingEffort) {
        return;
      }
      _pendingAiPreferences.remove(engine);
    }
    final modelChanged =
        normalizedModel != _configuredModelForEngine(engine).trim();
    final effortChanged = engine == 'codex' &&
        normalizedEffort.isNotEmpty &&
        normalizedEffort !=
            _configuredReasoningEffortForEngine(engine).trim().toLowerCase();
    if (!modelChanged && !effortChanged) {
      return;
    }
    unawaited(saveConfig(_config.copyWith(
      claudeModel: engine == 'claude' ? normalizedModel : _config.claudeModel,
      codexModel: engine == 'codex' ? normalizedModel : _config.codexModel,
      codexReasoningEffort:
          engine == 'codex' ? normalizedEffort : _config.codexReasoningEffort,
    )));
  }

  String _configuredModelForEngine(String engine) {
    return _config.modelForEngine(engine);
  }

  String _configuredReasoningEffortForEngine(String engine) {
    return _config.reasoningEffortForEngine(engine);
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

(String, String) _parseAiModelFromText(String engine, String text) {
  final normalized = text.trim();
  if (normalized.isEmpty) {
    return ('', '');
  }
  final lower = normalized.toLowerCase();
  if (engine == 'claude') {
    final parsed = parseClaudeModelFromText(normalized);
    if (parsed != null && parsed.isNotEmpty) {
      return (parsed, '');
    }
    return ('', '');
  }
  String model = '';
  final modelMatch = RegExp(r'(gpt[-\s]?\d(?:\.\d+)?(?:[-\s][a-z0-9]+)?)',
          caseSensitive: false)
      .firstMatch(normalized);
  if (modelMatch != null) {
    model = modelMatch.group(1)!.toLowerCase().replaceAll(' ', '-');
  } else if (lower.contains('codex')) {
    model = 'gpt-5-codex';
  }
  String effort = '';
  for (final candidate in _codexReasoningEffortOptions) {
    if (lower.contains(candidate)) {
      effort = candidate;
      break;
    }
  }
  return (model, effort);
}

String _resolvedAiEngine({
  required String command,
  required String engine,
}) {
  final normalizedEngine = engine.trim().toLowerCase();
  if (normalizedEngine == 'codex' || normalizedEngine == 'gemini') {
    return normalizedEngine;
  }
  final normalizedCommand = command.trim().toLowerCase();
  if (normalizedCommand == 'codex' || normalizedCommand.startsWith('codex ')) {
    return 'codex';
  }
  if (normalizedCommand == 'gemini' ||
      normalizedCommand.startsWith('gemini ')) {
    return 'gemini';
  }
  return 'claude';
}

String _resolvedAiModel(String engine, String configured) {
  final normalized = configured.trim();
  switch (engine) {
    case 'codex':
      final codexModel = _normalizeCodexModel(normalized);
      return codexModel.isNotEmpty ? codexModel : 'gpt-5-codex';
    case 'claude':
      return _normalizeClaudeModel(normalized);
    default:
      return normalized;
  }
}

String _resolvedAiReasoningEffort(String engine, String configured) {
  if (engine != 'codex') {
    return '';
  }
  final normalized = configured.trim().toLowerCase();
  if (_codexReasoningEffortOptions.contains(normalized)) {
    return normalized;
  }
  return 'medium';
}

String _aiModelSummary(String engine, String model, String reasoningEffort) {
  switch (engine) {
    case 'codex':
      return '${_codexModelLabel(model)} · ${reasoningEffort.toUpperCase()}';
    case 'claude':
      return _claudeModelLabel(model);
    case 'gemini':
      return 'Gemini';
    default:
      return model.trim().isEmpty ? '模型' : model.trim();
  }
}

String _claudeModelLabel(String value) {
  return claudeModelDisplayLabel(value);
}

String _normalizeClaudeModel(String value) {
  final normalized = normalizeClaudeModelSelection(value).trim();
  final alias = canonicalClaudeModelAlias(normalized);
  if (alias != null) {
    return alias;
  }
  if (normalized.toLowerCase().startsWith('claude-')) {
    return normalized.toLowerCase();
  }
  return 'sonnet';
}

String _normalizeCodexModel(String value) {
  final normalized = value.trim().toLowerCase();
  if (normalized.isEmpty) {
    return '';
  }
  if (normalized == 'opus' || normalized == 'sonnet') {
    return '';
  }
  if (normalized.startsWith('gpt') || normalized.contains('codex')) {
    return normalized;
  }
  return '';
}

String _codexModelLabel(String value) {
  switch (value.trim()) {
    case 'gpt-5-codex':
      return 'GPT-5-Codex';
    case 'gpt-5':
      return 'GPT-5';
    default:
      return value.trim().isEmpty ? 'Codex' : value.trim();
  }
}

const Set<String> _codexReasoningEffortOptions = <String>{
  'low',
  'medium',
  'high',
};
