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

enum SessionConnectionStage {
  disconnected,
  connecting,
  connected,
  backgroundSuspended,
  reconnecting,
  catchingUp,
  ready,
  failed,
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
    required this.revision,
  });

  final ActionNeededType type;
  final String key;
  final String message;
  final String revision;
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

class _DeferredFirstInput {
  const _DeferredFirstInput(this.text);

  final String text;
}

class _PendingOutboundAction {
  const _PendingOutboundAction({
    required this.payload,
    required this.userText,
    required this.label,
    required this.createdAt,
    required this.clientActionId,
    this.displayed = false,
    this.sendAttempts = 0,
  });

  final Map<String, dynamic> payload;
  final String userText;
  final String label;
  final DateTime createdAt;
  final String clientActionId;
  final bool displayed;
  final int sendAttempts;

  _PendingOutboundAction copyWith({
    Map<String, dynamic>? payload,
    bool? displayed,
    int? sendAttempts,
  }) =>
      _PendingOutboundAction(
        payload: payload ?? this.payload,
        userText: userText,
        label: label,
        createdAt: createdAt,
        clientActionId: clientActionId,
        displayed: displayed ?? this.displayed,
        sendAttempts: sendAttempts ?? this.sendAttempts,
      );
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
  static const int _maxForegroundReconnectAttempts = 4;
  static const Duration _connectionHealthInterval = Duration(seconds: 10);
  static const Duration _connectionSilenceTimeout = Duration(seconds: 45);
  static const Duration _observedSessionSyncInterval = Duration(seconds: 3);
  static const Duration _outboundAckRetryDelay = Duration(seconds: 6);
  final MobileVcWsService _service;
  final AdbWebRtcService _adbWebRtc = AdbWebRtcService();

  StreamSubscription<AppEvent>? _subscription;
  AppConfig _config = const AppConfig();
  SessionConnectionStage _connectionStage = SessionConnectionStage.disconnected;
  bool _connecting = false;
  bool _connected = false;
  bool _appInForeground = true;
  bool _autoReconnectEnabled = false;
  int _reconnectAttempt = 0;
  Timer? _reconnectTimer;
  Timer? _connectionHealthTimer;
  Timer? _observedSessionSyncTimer;
  Timer? _pendingOutboundRetryTimer;
  DateTime? _lastServerEventAt;
  final List<_PendingOutboundAction> _pendingOutboundActions =
      <_PendingOutboundAction>[];
  int _clientActionSequence = 0;
  final Map<String, int> _sessionEventCursors = <String, int>{};
  final Map<String, SessionDeltaKnown> _sessionDeltaKnown =
      <String, SessionDeltaKnown>{};
  bool _fileListLoading = false;
  bool _fileReading = false;
  bool _canResumeCurrentSession = false;
  bool _sessionRuntimeAlive = false;
  bool _selectedSessionExternalNative = false;
  bool _executionActive = false;
  bool _continueSameSessionEnabled = false;
  String _continuedSameSessionId = '';
  bool _isStopping = false;
  Timer? _sessionLoadingTimeout;
  static const Duration _sessionLoadingTimeoutDuration = Duration(seconds: 15);
  String _connectionMessage = '未连接';
  String _selectedSessionId = '';
  String _selectedSessionTitle = 'MobileVC';
  String _currentDirectoryPath = '';
  String _terminalStdout = '';
  String _terminalStderr = '';
  String _activeTerminalExecutionId = '';
  String _lastAssistantReplyExecutionKey = '';
  // 用户点击发送后立即点亮的"提交保护锁"——后端流式 LogEvent 不会再瞬间打掉状态球。
  // 收到带有新 executionKey 的 AgentStateEvent（或 IDLE/WAIT_INPUT）时解锁。
  bool _isSubmitting = false;
  String _isSubmittingBaselineKey = '';
  // 用于把 _activityVisible=false 平滑延迟，避免瞬时跳变造成视觉闪烁。
  Timer? _activityHideDebounce;
  AgentStateEvent? _agentState;
  RuntimePhaseEvent? _runtimePhase;
  SessionStateEvent? _sessionState;
  RuntimeInfoResultEvent? _runtimeInfo;
  final List<CodexModelCatalogEntry> _codexModelCatalog = [];
  bool _codexModelCatalogLoading = false;
  String _codexModelCatalogMessage = '';
  bool _codexModelCatalogUnavailable = false;
  final List<ClaudeModelCatalogEntry> _claudeModelCatalog = [];
  bool _claudeModelCatalogLoading = false;
  String _claudeModelCatalogMessage = '';
  bool _claudeModelCatalogUnavailable = false;
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
  final List<String> _debugLogs = [];
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
  bool _aiStatusIndicatorVisible = false;
  String _aiStatusIndicatorLabel = '思考中';
  Timer? _aiStatusHideDebounce;
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
  bool _sessionListSyncedSinceConnect = false;
  String _pendingSessionTargetId = '';
  String _pendingNotificationSessionTargetId = '';
  _DeferredFirstInput? _deferredFirstInput;
  final Map<String, _PendingAiPreference> _pendingAiPreferences =
      <String, _PendingAiPreference>{};
  final Set<String> _pendingToggleSkillNames = <String>{};
  final Set<String> _pendingToggleMemoryIds = <String>{};
  bool _isSavingSkill = false;
  bool _isSavingMemory = false;
  bool _sessionPermissionRulesEnabled = true;
  bool _persistentPermissionRulesEnabled = false;
  SessionContext? _pendingSessionContextTarget;
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
  SessionConnectionStage get connectionStage => _connectionStage;
  String get currentAiEngine => _resolvedAiEngine(
        command: currentMeta.command,
        engine: currentMeta.engine,
      );
  String get displayAiEngine => currentAiEngine;
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
  String get currentAiModelSummary => _displayAiModelSummary(
      currentAiEngine, selectedAiModel, selectedAiReasoningEffort);
  String get commandBarEngine =>
      shouldShowClaudeMode ? displayAiEngine : 'shell';
  String get commandBarModelSummary => _displayAiModelSummary(
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
  bool get reconnecting =>
      _connectionStage == SessionConnectionStage.reconnecting ||
      _connectionStage == SessionConnectionStage.backgroundSuspended;
  bool get shouldShowSessionSurface =>
      _connected ||
      _connectionStage == SessionConnectionStage.catchingUp ||
      ((_connecting || reconnecting) &&
          (_selectedSessionId.trim().isNotEmpty || _timeline.isNotEmpty));
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
  List<CodexModelCatalogEntry> get codexModelCatalog =>
      List.unmodifiable(_codexModelCatalog);
  bool get codexModelCatalogLoading => _codexModelCatalogLoading;
  String get codexModelCatalogMessage => _codexModelCatalogMessage;
  bool get codexModelCatalogUnavailable => _codexModelCatalogUnavailable;
  List<ClaudeModelCatalogEntry> get claudeModelCatalog =>
      List.unmodifiable(_claudeModelCatalog);
  bool get claudeModelCatalogLoading => _claudeModelCatalogLoading;
  String get claudeModelCatalogMessage => _claudeModelCatalogMessage;
  bool get claudeModelCatalogUnavailable => _claudeModelCatalogUnavailable;
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
  List<String> get debugLogs => List.unmodifiable(_debugLogs);
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
      pendingPrompt?.isPermission == true;
  bool get hasPendingPlanPrompt =>
      pendingInteraction?.isPlan == true || pendingPrompt?.isPlan == true;
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
  bool get isObservingRemoteActiveSession =>
      _selectedSessionExternalNative && !_continueSameSessionEnabled;
  bool get isSessionReadOnly => isObservingRemoteActiveSession;
  String get sessionReadOnlyHint =>
      isSessionReadOnly ? '电脑端原生会话正在运行，手机端当前为观察模式' : '';
  String get sessionObservationTitle =>
      _continueSameSessionEnabled ? '已在手机继续同一会话' : '观察模式';
  String get sessionObservationDetail => _continueSameSessionEnabled
      ? '请避免同时在电脑端原生终端输入，防止上下文交错。'
      : '正在同步电脑端进度。点击继续后，手机会向同一个会话发送输入。';
  bool get shouldShowSessionObservationBanner =>
      _selectedSessionId.trim().isNotEmpty &&
      _sessionRuntimeAlive &&
      _selectedSessionExternalNative &&
      !hasPendingPermissionPrompt &&
      !shouldShowReviewChoices &&
      !hasPendingPlanPrompt &&
      !hasPendingPlanQuestions;
  bool get canContinueSameSession =>
      shouldShowSessionObservationBanner && !_continueSameSessionEnabled;

  bool get canSendToContinuedSameSession =>
      _continueSameSessionEnabled &&
      _selectedSessionId.trim().isNotEmpty &&
      _continuedSameSessionId == _selectedSessionId.trim();

  List<TimelineItem> get timeline => List.unmodifiable(_timeline);
  List<ReviewGroup> get reviewGroups => List.unmodifiable(_reviewGroups);
  ReviewGroup? get activeReviewGroup => _resolvedActiveReviewGroup();
  String get activeReviewGroupId => _activeReviewGroupId;
  bool get awaitInput {
    if (_isLoadingSession) {
      return false;
    }
    return _agentState?.awaitInput == true ||
        _pendingPrompt != null ||
        _pendingInteraction != null;
  }

  ActionNeededSignal? get actionNeededSignal => _actionNeededSignal;
  AppNotificationSignal? get notificationSignal => _notificationSignal;
  bool get fastMode => _config.fastMode;
  String get displayPermissionMode => _normalizeDisplayPermissionMode(
        _runtimePermissionMode.isNotEmpty
            ? _runtimePermissionMode
            : _config.permissionMode,
      );
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
  HistoryContext? get reviewActionTargetDiff => _reviewActionTargetDiff();
  bool get openedFileMatchesPendingDiff => openedFilePendingDiff != null;
  bool get isAutoAcceptMode => displayPermissionMode == 'auto';
  bool get isBypassPermissionsMode =>
      displayPermissionMode == 'bypassPermissions';
  bool get isManualReviewMode => !isAutoAcceptMode;

  bool _reviewShouldAutoAccept(RuntimeMeta meta) {
    final source = meta.source.trim().toLowerCase();
    return source == 'review-auto-accepted';
  }

  bool get shouldShowReviewChoices {
    final interaction = _pendingInteraction;
    final prompt = _pendingPrompt;
    final permissionBlockingOnly =
        (interaction?.isPermission == true || prompt?.isPermission == true) &&
            interaction?.isReview != true &&
            prompt?.isReview != true;
    final waitingForReviewInput = interaction?.isReview == true ||
        prompt?.isReview == true ||
        (hasPendingReview &&
            (!hasPendingPermissionPrompt || permissionBlockingOnly) &&
            !hasPendingPlanPrompt &&
            !hasPendingPlanQuestions);
    return currentReviewDiff != null &&
        waitingForReviewInput &&
        (!hasPendingPermissionPrompt || permissionBlockingOnly) &&
        !hasPendingPlanPrompt &&
        !hasPendingPlanQuestions;
  }

  bool get canShowReviewActions {
    final interaction = _pendingInteraction;
    final prompt = _pendingPrompt;
    final permissionBlockingOnly =
        (interaction?.isPermission == true || prompt?.isPermission == true) &&
            interaction?.isReview != true &&
            prompt?.isReview != true;
    return !isAutoAcceptMode &&
        reviewActionTargetDiff != null &&
        (!hasPendingPermissionPrompt || permissionBlockingOnly) &&
        !hasPendingPlanPrompt &&
        !hasPendingPlanQuestions;
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
    final log = '[session] $label$suffix';
    debugPrint(log);
    _debugLogs.add('${DateTime.now().toString().substring(11, 19)} $log');
    if (_debugLogs.length > 200) {
      _debugLogs.removeAt(0);
    }
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
    _sessionLoadingTimeout = Timer(_sessionLoadingTimeoutDuration, () {
      if (!_isLoadingSession) return;
      _isLoadingSession = false;
      _pendingSessionTargetId = '';
      _agentPhaseLabel = '加载超时';
      _pushSystem('error', '会话加载超时，请检查网络后重试');
      _syncDerivedState();
      notifyListeners();
    });
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

  void pushSystemMessage(String kind, String text) {
    _pushSystem(kind, text);
  }

  Future<void> acceptAllPendingDiffs() async {
    final diffs = List<HistoryContext>.from(_pendingDiffs);
    if (diffs.isEmpty) {
      _pushSystem('error', '当前没有待审核的 diff');
      return;
    }
    for (final diff in diffs) {
      if (!diff.pendingReview) {
        continue;
      }
      _sendReviewDecisionForDiff(
        diff,
        'accept',
      );
    }
    _syncDerivedState();
    notifyListeners();
  }

  bool get isSessionBusy {
    if (_isLoadingSession) {
      return true;
    }
    if (_connectionStage == SessionConnectionStage.catchingUp) {
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
    if (agentState == 'THINKING' ||
        agentState == 'RECOVERING' ||
        agentState == 'RUNNING_TOOL') {
      return true;
    }
    final sessionState = (_sessionState?.state ?? '').trim().toUpperCase();
    if (sessionState == 'THINKING' || sessionState == 'RUNNING_TOOL') {
      return true;
    }
    if (_hasRunningTerminalExecution) {
      return true;
    }
    if (sessionState != 'RUNNING') {
      return false;
    }
    final runningKey = _runtimeExecutionKey(
      _sessionState?.runtimeMeta ??
          _agentState?.runtimeMeta ??
          const RuntimeMeta(),
    );
    if (runningKey.isEmpty) {
      return true;
    }
    return runningKey != _lastAssistantReplyExecutionKey ||
        _isDefinitiveAgentState(agentState, sessionState) ||
        _executionActive;
  }

  bool get canStopCurrentRun {
    if (!connected) {
      return false;
    }
    // 如果正在停止中，不允许再次点击
    if (_isStopping) {
      return false;
    }
    if (_hasRunningTerminalExecution) {
      return true;
    }
    final agentState = (_agentState?.state ?? '').trim().toUpperCase();
    if (agentState == 'THINKING' ||
        agentState == 'RECOVERING' ||
        agentState == 'RUNNING_TOOL') {
      return true;
    }
    final sessionState = (_sessionState?.state ?? '').trim().toUpperCase();
    if (sessionState == 'THINKING' || sessionState == 'RUNNING_TOOL') {
      return true;
    }
    if (sessionState != 'RUNNING') {
      return false;
    }
    final runningKey = _runtimeExecutionKey(
      _sessionState?.runtimeMeta ??
          _agentState?.runtimeMeta ??
          const RuntimeMeta(),
    );
    if (runningKey.isEmpty) {
      return true;
    }
    return runningKey != _lastAssistantReplyExecutionKey ||
        _isDefinitiveAgentState(agentState, sessionState) ||
        _executionActive;
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
  bool get aiStatusIndicatorVisible => _aiStatusIndicatorVisible;
  String get aiStatusIndicatorLabel => _aiStatusIndicatorLabel;

  bool get activityVisible => _activityVisible;
  bool get activityBannerVisible =>
      isObservingRemoteActiveSession ||
      _activityVisible ||
      _isClaudePendingReadyForInput ||
      _isStopping;
  bool get activityBannerAnimated =>
      !isObservingRemoteActiveSession && _activityVisible && !_isStopping;
  String get activityBannerTitle {
    if (isObservingRemoteActiveSession) {
      return '观察模式';
    }
    if (_isStopping) {
      return '正在停止';
    }
    if (_isClaudePendingReadyForInput) {
      return '待输入';
    }
    final stepSummary = _currentStepSummary.trim();
    if (stepSummary.isNotEmpty) {
      return stepSummary;
    }
    final phase = _agentPhaseLabel.trim();
    if (phase.isNotEmpty && phase != '已连接') {
      return phase;
    }
    return '运行中';
  }

  String get activityBannerDetail {
    if (isObservingRemoteActiveSession) {
      return _sessionRuntimeAlive ? '正在同步电脑端原生会话进度' : '电脑端原生会话只读预览';
    }
    if (_isStopping) {
      return '等待后端确认停止...';
    }
    if (_isClaudePendingReadyForInput) {
      return 'Claude 已启动，请继续输入';
    }
    final stepSummary = _currentStepSummary.trim();
    if (stepSummary.isNotEmpty) {
      return stepSummary;
    }
    final fromAgent = _detailFromAgentState();
    if (fromAgent.isNotEmpty) {
      return fromAgent;
    }
    final label = _activityToolLabel.trim();
    if (label.isEmpty) {
      return '';
    }
    return '调用工具 · $label';
  }

  bool get activityBannerShowsElapsed =>
      !isObservingRemoteActiveSession && _activityVisible;
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
  RuntimeMeta get _liveRuntimeMeta =>
      (_agentState?.runtimeMeta ?? const RuntimeMeta())
          .merge(_sessionState?.runtimeMeta ?? const RuntimeMeta())
          .merge(_runtimeInfo?.runtimeMeta ?? const RuntimeMeta())
          .merge(_resumeRuntimeMeta);

  bool get inClaudeMode {
    if (_isLoadingSession) {
      return false;
    }
    const claudeStates = <String>{
      'starting',
      'active',
      'waiting_input',
      'resumable',
    };
    return claudeStates.contains(_liveRuntimeMeta.claudeLifecycle.trim());
  }

  bool get shouldShowClaudeMode => inClaudeMode;

  bool get _isClaudePendingReadyForInput =>
      _agentState?.awaitInput == true &&
      _liveRuntimeMeta.claudeLifecycle.trim().toLowerCase() ==
          'waiting_input' &&
      _liveRuntimeMeta.engine.trim().toLowerCase() == 'claude';

  bool _isDefinitiveAgentState(String agentState, String sessionState) {
    return agentState == 'THINKING' ||
        agentState == 'RECOVERING' ||
        agentState == 'RUNNING_TOOL' ||
        sessionState == 'RUNNING_TOOL' ||
        sessionState == 'THINKING' ||
        sessionState == 'RUNNING';
  }

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

  void _selectAiEngine(String engine) {
    final normalized = engine.trim().toLowerCase();
    if (normalized != 'claude' &&
        normalized != 'codex' &&
        normalized != 'gemini') {
      return;
    }
    if (_config.engine.trim().toLowerCase() != normalized) {
      unawaited(saveConfig(_config.copyWith(engine: normalized)));
    }
    notifyListeners();
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
      return _normalizeDisplayPermissionMode(interactionMode);
    }
    final promptMode = pendingPrompt?.runtimeMeta.permissionMode.trim() ?? '';
    if (promptMode.isNotEmpty) {
      return _normalizeDisplayPermissionMode(promptMode);
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
    _startConnectionHealthMonitor();
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
    _stopObservedSessionSync();
    _adbWebRtcStartTimeout?.cancel();
    _reconnectTimer?.cancel();
    _connectionHealthTimer?.cancel();
    _pendingOutboundRetryTimer?.cancel();
    _sessionLoadingTimeout?.cancel();
    _aiStatusHideDebounce?.cancel();
    _activityHideDebounce?.cancel();
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
    try {
      final prefs = await SharedPreferences.getInstance();
      await prefs.setString(_prefsKey, jsonEncode(config.toJson()));
    } catch (error, stack) {
      _pushDebug('save config failed',
          'key=$_prefsKey errorType=${error.runtimeType}');
      debugPrintStack(
        stackTrace: stack,
        label: '[session] save config stack',
      );
      _pushSystem('error', '保存连接配置失败：$error');
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
    if (_connected && !samePath) {
      requestSessionList();
    }
    notifyListeners();
  }

  String? _devicePushToken;
  String get devicePushToken => _devicePushToken ?? '';

  void _sendCachedPushTokenIfPossible() {
    final token = _devicePushToken?.trim() ?? '';
    if (!_connected || _selectedSessionId.isEmpty || token.isEmpty) {
      return;
    }
    _service.send({
      'action': 'register_push_token',
      'sessionId': _selectedSessionId,
      'token': token,
      'platform': 'ios',
    });
  }

  bool _sendUserVisibleAction(
    Map<String, dynamic> payload, {
    required String userText,
    required String label,
    bool queueOnFailure = true,
  }) {
    final clientActionId = _nextClientActionId();
    final outboundPayload = Map<String, dynamic>.from(payload)
      ..['clientActionId'] = clientActionId;
    final sessionId = _selectedSessionId.trim();
    if (sessionId.isNotEmpty &&
        (outboundPayload['sessionId'] ?? '').toString().trim().isEmpty) {
      outboundPayload['sessionId'] = sessionId;
    }
    final pendingAction = _PendingOutboundAction(
      payload: outboundPayload,
      userText: userText,
      label: label,
      createdAt: DateTime.now(),
      clientActionId: clientActionId,
    );
    final sent = _service.send(outboundPayload);
    if (sent) {
      _pendingOutboundActions.add(
        pendingAction.copyWith(displayed: true, sendAttempts: 1),
      );
      _appendLocalUserTimeline(userText, clientActionId);
      _schedulePendingOutboundRetry();
      return true;
    }
    if (queueOnFailure) {
      _pendingOutboundActions.add(pendingAction);
      _appendLocalUserTimeline(userText, clientActionId);
      _pushSystem('session', '网络暂不可用，消息已排队，恢复连接后自动发送');
      _scheduleReconnect(immediate: true);
      _schedulePendingOutboundRetry();
    } else {
      _pushSystem('error', '消息发送失败：WebSocket 未连接或写入失败');
    }
    return false;
  }

  void _appendLocalUserTimeline(String userText, String clientActionId) {
    final body = userText.trim();
    if (body.isEmpty) {
      return;
    }
    _appendTimelineItem(
      TimelineItem(
        id: 'user-local-$clientActionId',
        kind: 'user',
        timestamp: DateTime.now(),
        body: body,
      ),
      emitNotifications: false,
    );
    notifyListeners();
  }

  void _setAiStatusVisible(bool visible, {String label = '', bool immediate = false}) {
    final resolvedLabel = label.trim().isNotEmpty ? label.trim() : '思考中';
    if (visible) {
      _aiStatusHideDebounce?.cancel();
      _aiStatusHideDebounce = null;
      final changed =
          !_aiStatusIndicatorVisible || _aiStatusIndicatorLabel != resolvedLabel;
      _aiStatusIndicatorVisible = true;
      _aiStatusIndicatorLabel = resolvedLabel;
      if (changed) {
        notifyListeners();
      }
      return;
    }
    if (immediate) {
      _aiStatusHideDebounce?.cancel();
      _aiStatusHideDebounce = null;
      if (_aiStatusIndicatorVisible) {
        _aiStatusIndicatorVisible = false;
        notifyListeners();
      }
      return;
    }
    if (!_aiStatusIndicatorVisible) {
      return;
    }
    _aiStatusHideDebounce?.cancel();
    _aiStatusHideDebounce = Timer(const Duration(milliseconds: 600), () {
      _aiStatusHideDebounce = null;
      if (!_aiStatusIndicatorVisible) {
        return;
      }
      _aiStatusIndicatorVisible = false;
      notifyListeners();
    });
  }

  /// 用户点击"发送"后调用：清空旧的回复结算标记，记录基线 executionKey，
  /// 打开 _isSubmitting 保护锁，并立即点亮 AI 状态球。
  ///
  /// 解锁逻辑统一在 AgentStateEvent 分支处理：当看到带有新 executionKey 的状态、
  /// 或 IDLE / WAIT_INPUT / awaitInput 信号时把锁解开。
  void _beginUserSubmission() {
    _lastAssistantReplyExecutionKey = '';
    _isSubmittingBaselineKey = _runtimeExecutionKey(
      _agentState?.runtimeMeta ??
          _sessionState?.runtimeMeta ??
          const RuntimeMeta(),
    );
    _isSubmitting = true;
    // 任何正在排队的"延迟消隐"都立即取消，保证发送瞬间状态球不会被旧 timer 打灭。
    _activityHideDebounce?.cancel();
    _activityHideDebounce = null;
    // 立即点亮状态球——这是用户最直观的反馈。流式 LogEvent 在后端会被忙碌态保护，
    // 不会再发 visible:false；本端不依赖 backend 即可拿到第一帧亮起。
    _setAiStatusVisible(true, label: '思考中');
    _syncDerivedState();
  }

  String _nextClientActionId() {
    _clientActionSequence += 1;
    return '${DateTime.now().microsecondsSinceEpoch}-$_clientActionSequence';
  }

  void _flushPendingOutboundActions() {
    if (!_connected || _connecting || _pendingOutboundActions.isEmpty) {
      return;
    }
    final pending = List<_PendingOutboundAction>.from(_pendingOutboundActions);
    _pendingOutboundActions.clear();
    var flushed = 0;
    var newlyDisplayed = 0;
    for (final action in pending) {
      final sent = _service.send(action.payload);
      if (!sent) {
        _pendingOutboundActions
          ..add(action)
          ..addAll(pending.skip(flushed + 1));
        _scheduleReconnect(immediate: true);
        _schedulePendingOutboundRetry();
        return;
      }
      flushed++;
      if (!action.displayed) {
        newlyDisplayed++;
      }
      _pendingOutboundActions.add(
        action.copyWith(
          displayed: true,
          sendAttempts: action.sendAttempts + 1,
        ),
      );
    }
    if (newlyDisplayed > 0) {
      _pushSystem('session', '已自动补发 $newlyDisplayed 条排队消息');
    }
    if (flushed > 0) {
      _schedulePendingOutboundRetry();
    }
  }

  void _handleClientActionAck(ClientActionAckEvent ack) {
    final clientActionId = ack.clientActionId.trim();
    if (clientActionId.isEmpty || _pendingOutboundActions.isEmpty) {
      return;
    }
    final before = _pendingOutboundActions.length;
    _pendingOutboundActions
        .removeWhere((action) => action.clientActionId == clientActionId);
    if (_pendingOutboundActions.length != before) {
      _schedulePendingOutboundRetry();
    }
  }

  void _schedulePendingOutboundRetry() {
    _pendingOutboundRetryTimer?.cancel();
    if (_pendingOutboundActions.isEmpty) {
      return;
    }
    _pendingOutboundRetryTimer =
        Timer(_outboundAckRetryDelay, _retryPendingOutboundActions);
  }

  void _retryPendingOutboundActions() {
    if (_pendingOutboundActions.isEmpty) {
      return;
    }
    if (!_connected || _connecting) {
      _scheduleReconnect(immediate: true);
      _schedulePendingOutboundRetry();
      return;
    }
    _flushPendingOutboundActions();
  }

  Map<String, dynamic> _aiTurnPayload({
    required String engine,
    required RuntimeMeta meta,
    required String permissionMode,
    String data = '',
  }) {
    final normalizedEngine = engine.trim().toLowerCase();
    final payload = <String, dynamic>{
      'action': 'ai_turn',
      ...meta.toJson(),
      'engine': normalizedEngine,
      'cwd': effectiveCwd,
      'permissionMode': permissionMode,
    };
    payload.remove('command');
    payload.remove('claudeLifecycle');
    final model = _aiTurnModelForEngine(normalizedEngine);
    if (model.isNotEmpty) {
      payload['model'] = model;
    } else {
      payload.remove('model');
    }
    final reasoningEffort = _aiTurnReasoningEffortForEngine(normalizedEngine);
    if (reasoningEffort.isNotEmpty) {
      payload['reasoningEffort'] = reasoningEffort;
    } else {
      payload.remove('reasoningEffort');
    }
    if (data.trim().isNotEmpty) {
      payload['data'] = data;
    }
    return payload;
  }

  String _aiTurnModelForEngine(String engine) {
    final normalizedEngine = engine.trim().toLowerCase();
    final configured = _configuredModelForEngine(normalizedEngine).trim();
    if (configured.isEmpty || configured.toLowerCase() == 'default') {
      return '';
    }
    final resolved = _resolvedAiModel(normalizedEngine, configured).trim();
    if (resolved.toLowerCase() == 'default') {
      return '';
    }
    return resolved;
  }

  String _aiTurnReasoningEffortForEngine(String engine) {
    if (engine.trim().toLowerCase() != 'codex') {
      return '';
    }
    final configured = _configuredReasoningEffortForEngine('codex').trim();
    if (configured.isEmpty || configured.toLowerCase() == 'default') {
      return '';
    }
    return _resolvedAiReasoningEffort('codex', configured);
  }

  void setDevicePushToken(String token) {
    final normalized = token.trim();
    if (normalized.isEmpty || _devicePushToken == normalized) {
      return;
    }
    _devicePushToken = normalized;
    _sendCachedPushTokenIfPossible();
  }

  void handleForegroundStateChanged(bool isForeground) {
    final previous = _appInForeground;
    _appInForeground = isForeground;
    if (_appInForeground && !previous) {
      _scheduleReconnect(immediate: true);
      _syncObservedSessionPolling();
    } else if (!_appInForeground) {
      _stopObservedSessionSync();
    }
  }

  void _cancelReconnectTimer() {
    _reconnectTimer?.cancel();
    _reconnectTimer = null;
  }

  Duration _nextReconnectDelay({required bool immediate}) {
    if (immediate) {
      return Duration.zero;
    }
    switch (_reconnectAttempt) {
      case 0:
        return const Duration(seconds: 1);
      case 1:
        return const Duration(seconds: 2);
      case 2:
        return const Duration(seconds: 4);
      default:
        return const Duration(seconds: 8);
    }
  }

  void _scheduleReconnect({bool immediate = false}) {
    if (!_autoReconnectEnabled) {
      return;
    }
    if (_connected || _connecting) {
      return;
    }
    _cancelReconnectTimer();
    if (!_appInForeground) {
      _connectionStage = SessionConnectionStage.backgroundSuspended;
      _syncDerivedState();
      notifyListeners();
      return;
    }
    _connectionStage = SessionConnectionStage.reconnecting;
    final delay = _nextReconnectDelay(immediate: immediate);
    if (!immediate) {
      _reconnectAttempt++;
    }
    _reconnectTimer = Timer(delay, () {
      unawaited(connect(silently: true));
    });
    _syncDerivedState();
    notifyListeners();
  }

  Future<void> connect({
    bool restoreSession = false,
    bool silently = false,
  }) async {
    if (_connecting) {
      return;
    }
    _cancelReconnectTimer();
    if (_isInvalidLoopbackHostForMobile()) {
      _connectionStage = SessionConnectionStage.failed;
      _connecting = false;
      _connected = false;
      _connectionMessage = 'iPhone 不能使用 localhost/127.0.0.1，请改成 Mac 的局域网 IP';
      _pushSystem('error', _connectionMessage);
      _syncDerivedState();
      notifyListeners();
      return;
    }
    final reconnectTarget = restoreSession ? _selectedSessionId.trim() : '';
    var shouldRetrySilently = false;
    _autoReconnectEnabled = true;
    _connecting = true;
    _connectionStage = (silently || reconnecting)
        ? SessionConnectionStage.reconnecting
        : SessionConnectionStage.connecting;
    _connectionMessage =
        silently && reconnectTarget.isNotEmpty ? '恢复连接中...' : '连接中...';
    _syncDerivedState();
    notifyListeners();
    try {
      await _service.connect(_config.wsUrl);
      _connected = true;
      _reconnectAttempt = 0;
      _connectionStage = SessionConnectionStage.connected;
      _connectionMessage = '已连接';
      _autoSessionRequested = false;
      _autoSessionCreating = false;
      _sessionListSyncedSinceConnect = false;
      _runtimePermissionMode = '';
      _codexModelCatalogLoading = false;
      _codexModelCatalogMessage = '';
      _codexModelCatalogUnavailable = false;
      _codexModelCatalog.clear();
      _claudeModelCatalogLoading = false;
      _claudeModelCatalogMessage = '';
      _claudeModelCatalogUnavailable = false;
      _claudeModelCatalog.clear();
      await switchWorkingDirectory(_config.cwd);
      requestRuntimeInfo('context');
      requestSkillCatalog();
      requestMemoryList();
      requestSessionList();
      requestAdbDevices();
      requestSessionContext();
      requestPermissionRuleList();
      requestReviewState();
      requestTaskSnapshot();
      if (_selectedSessionId.trim().isNotEmpty) {
        _requestSessionResume(reason: silently ? 'reconnect' : 'connect');
      } else {
        _requestSessionDelta(reason: silently ? 'reconnect' : 'connect');
      }
      _sendCachedPushTokenIfPossible();
      _restorePendingNotificationSessionIfNeeded();
      _flushPendingOutboundActions();
    } catch (error) {
      _connected = false;
      if (silently && _autoReconnectEnabled) {
        if (_appInForeground &&
            _reconnectAttempt >= _maxForegroundReconnectAttempts) {
          _autoReconnectEnabled = false;
          _connectionStage = SessionConnectionStage.failed;
          _connectionMessage = '恢复失败，需要重连';
          _pushSystem('error', _connectionMessage);
        } else {
          _connectionStage = _appInForeground
              ? SessionConnectionStage.reconnecting
              : SessionConnectionStage.backgroundSuspended;
          _connectionMessage =
              reconnectTarget.isNotEmpty ? '恢复连接中...' : '连接中...';
          shouldRetrySilently = true;
        }
      } else {
        _connectionStage = SessionConnectionStage.failed;
        _connectionMessage = '连接失败：$error';
        _pushSystem('error', _connectionMessage);
      }
    } finally {
      _connecting = false;
      if (_connected) {
        _restorePendingNotificationSessionIfNeeded();
      }
      if (shouldRetrySilently) {
        _scheduleReconnect();
      }
      _syncDerivedState();
      notifyListeners();
    }
  }

  Future<void> disconnect() async {
    _autoReconnectEnabled = false;
    _reconnectAttempt = 0;
    _cancelReconnectTimer();
    _stopObservedSessionSync();
    _stopAdbRefreshPolling();
    _connectionHealthTimer?.cancel();
    _connectionHealthTimer = null;
    _sessionLoadingTimeout?.cancel();
    _sessionLoadingTimeout = null;
    await _adbWebRtc.stop();
    await _service.disconnect();
    _connected = false;
    _connectionStage = SessionConnectionStage.disconnected;
    _selectedSessionId = '';
    _selectedSessionTitle = 'MobileVC';
    _connectionMessage = '已断开';
    _clearDeferredFirstInput();
    _pendingOutboundActions.clear();
    _sessionListSyncedSinceConnect = false;
    _fileListLoading = false;
    _fileReading = false;
    _sessionRuntimeAlive = false;
    _selectedSessionExternalNative = false;
    _executionActive = false;
    _continueSameSessionEnabled = false;
    _continuedSameSessionId = '';
    _currentDirectoryPath = '';
    _currentDirectoryItems.clear();
    _openedFile = null;
    _terminalStdout = '';
    _terminalStderr = '';
    _activeTerminalExecutionId = '';
    _lastAssistantReplyExecutionKey = '';
    _terminalExecutions.clear();
    _resetRuntimeProcessState();
    _sessionEventCursors.clear();
    _sessionDeltaKnown.clear();
    _lastServerEventAt = null;
    _canResumeCurrentSession = false;
    _resumeRuntimeMeta = const RuntimeMeta();
    _runtimePermissionMode = '';
    _sessionContext = const SessionContext();
    _skillCatalogMeta = const CatalogMetadata(domain: 'skill');
    _memoryCatalogMeta = const CatalogMetadata(domain: 'memory');
    _skillSyncStatus = '';
    _memorySyncStatus = '';
    _codexModelCatalogLoading = false;
    _codexModelCatalogMessage = '';
    _codexModelCatalogUnavailable = false;
    _codexModelCatalog.clear();
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
    _activityHideDebounce?.cancel();
    _activityHideDebounce = null;
    _isSubmitting = false;
    _isSubmittingBaselineKey = '';
    _aiStatusHideDebounce?.cancel();
    _aiStatusHideDebounce = null;
    _aiStatusIndicatorVisible = false;
    _aiStatusIndicatorLabel = '思考中';
    _activeReviewDiffId = '';
    _agentPhaseLabel = '未连接';
    _resetActionNeededTracking();
    _syncDerivedState();
    notifyListeners();
  }

  void _handleUnexpectedSocketDisconnect(String message) {
    final normalized = message.trim().isEmpty ? '连接已断开' : message.trim();
    final wasRecovering = reconnecting;
    _connected = false;
    _connecting = false;
    _selectedSessionExternalNative = false;
    _executionActive = false;
    _isLoadingSession = false;
    _sessionListSyncedSinceConnect = false;
    _pendingSessionTargetId = '';
    _pendingNotificationSessionTargetId = '';
    _clearDeferredFirstInput();
    _sessionEventCursors.clear();
    _sessionDeltaKnown.clear();
    _sessionRuntimeAlive = false;
    _continueSameSessionEnabled = false;
    _continuedSameSessionId = '';
    _agentState = null;
    _sessionState = null;
    _runtimePhase = null;
    _stopObservedSessionSync();
    _stopAdbRefreshPolling();
    if (_autoReconnectEnabled) {
      _connectionStage = _appInForeground
          ? SessionConnectionStage.reconnecting
          : SessionConnectionStage.backgroundSuspended;
      _connectionMessage = _appInForeground ? '恢复连接中...' : '后台连接已暂停';
      _scheduleReconnect(immediate: _appInForeground && !wasRecovering);
    } else {
      final alreadyDisconnected =
          !_connected && !_connecting && _connectionMessage == normalized;
      _connectionStage = SessionConnectionStage.failed;
      _connectionMessage = normalized;
      _pendingPrompt = null;
      _pendingInteraction = null;
      _runtimePhase = null;
      _resetActionNeededTracking();
      if (!alreadyDisconnected) {
        _pushSystem('error', normalized);
      }
    }
    _syncDerivedState();
    notifyListeners();
  }

  void resumeConnectionIfNeeded() {
    if (_connected && !_connecting) {
      requestTaskSnapshot();
      _requestSessionResume(reason: 'foreground');
      _requestSessionDelta(reason: 'foreground');
      _flushPendingOutboundActions();
      _syncObservedSessionPolling();
      return;
    }
    _scheduleReconnect(immediate: true);
  }

  void _requestSessionResume({String reason = ''}) {
    final sessionId = _selectedSessionId.trim();
    if (!_connected || _connecting || sessionId.isEmpty) {
      return;
    }
    final lastSeenCursor = _sessionEventCursors[sessionId] ?? 0;
    final runtimeState =
        (_agentState?.state ?? _sessionState?.state ?? '').trim();
    _connectionStage = SessionConnectionStage.catchingUp;
    _service.send({
      'action': 'session_resume',
      'sessionId': sessionId,
      'cwd': effectiveCwd,
      if (reason.trim().isNotEmpty) 'reason': reason.trim(),
      if (lastSeenCursor > 0) 'lastSeenEventCursor': lastSeenCursor,
      if (runtimeState.isNotEmpty) 'lastKnownRuntimeState': runtimeState,
    });
    _syncDerivedState();
    notifyListeners();
  }

  void _requestSessionDelta({String reason = ''}) {
    final sessionId = _selectedSessionId.trim();
    if (!_connected || _connecting || sessionId.isEmpty) {
      return;
    }
    _service.send({
      'action': 'session_delta_get',
      'sessionId': sessionId,
      'cwd': effectiveCwd,
      if (reason.trim().isNotEmpty) 'reason': reason.trim(),
      'known': _currentSessionDeltaKnown(sessionId).toJson(),
    });
  }

  void _syncObservedSessionPolling() {
    if (!_connected ||
        _connecting ||
        !_appInForeground ||
        _selectedSessionId.trim().isEmpty ||
        (!_sessionRuntimeAlive && !_selectedSessionExternalNative)) {
      _stopObservedSessionSync();
      return;
    }
    if (_observedSessionSyncTimer != null) {
      return;
    }
    _observedSessionSyncTimer = Timer.periodic(
      _observedSessionSyncInterval,
      (_) => _requestSessionDelta(reason: 'observe_active_session'),
    );
  }

  void _stopObservedSessionSync() {
    _observedSessionSyncTimer?.cancel();
    _observedSessionSyncTimer = null;
  }

  void continueSameSessionFromPhone() {
    if (!canContinueSameSession) {
      return;
    }
    _continueSameSessionEnabled = true;
    _continuedSameSessionId = _selectedSessionId.trim();
    _pushSystem(
      'session',
      '已在手机继续同一会话。电脑端原生终端仍可输入，请避免两端同时输入。',
    );
    _syncDerivedState();
    notifyListeners();
  }

  SessionDeltaKnown _currentSessionDeltaKnown(String sessionId) {
    final normalized = sessionId.trim();
    final known = _sessionDeltaKnown[normalized];
    if (known != null) {
      return known;
    }
    return SessionDeltaKnown(
      eventCursor: _sessionEventCursors[normalized] ?? 0,
      logEntryCount: 0,
      diffCount: 0,
      terminalExecutionCount: 0,
      terminalStdoutLength: _terminalStdout.length,
      terminalStderrLength: _terminalStderr.length,
    );
  }

  Future<void> restoreSessionFromNotification(String sessionId) async {
    final targetId = sessionId.trim();
    if (targetId.isEmpty) {
      resumeConnectionIfNeeded();
      return;
    }
    _pendingNotificationSessionTargetId = targetId;
    if (_connected && !_connecting) {
      if (_selectedSessionId.trim() == targetId && !_isLoadingSession) {
        _pendingNotificationSessionTargetId = '';
        resumeConnectionIfNeeded();
        return;
      }
      loadSession(targetId);
      return;
    }
    if (!_autoReconnectEnabled && !_connecting) {
      await connect(silently: true);
      return;
    }
    resumeConnectionIfNeeded();
  }

  void pauseConnectionRecovery() {
    if (_appInForeground) {
      return;
    }
    _cancelReconnectTimer();
  }

  void _restorePendingNotificationSessionIfNeeded() {
    final targetId = _pendingNotificationSessionTargetId.trim();
    if (targetId.isEmpty || !_connected || _connecting) {
      return;
    }
    if (_selectedSessionId.trim() == targetId && !_isLoadingSession) {
      _pendingNotificationSessionTargetId = '';
      return;
    }
    loadSession(targetId);
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
  }

  void createSession([String title = '']) {
    _beginSessionLoading();
    _service.send({
      'action': 'session_create',
      'cwd': effectiveCwd,
      if (title.isNotEmpty) 'title': title,
    });
  }

  bool _shouldAutoCreateSessionOnFirstInput() {
    return _connected &&
        !_connecting &&
        !_isLoadingSession &&
        _sessionListSyncedSinceConnect &&
        _selectedSessionId.trim().isEmpty &&
        _pendingNotificationSessionTargetId.trim().isEmpty &&
        !_autoSessionCreating;
  }

  void _deferFirstInputAndCreateSession(String value) {
    final normalized = value.trim();
    if (normalized.isEmpty) {
      return;
    }
    _deferredFirstInput = _DeferredFirstInput(normalized);
    _autoSessionRequested = true;
    _autoSessionCreating = true;
    createSession();
  }

  void _clearDeferredFirstInput() {
    _deferredFirstInput = null;
    _autoSessionRequested = false;
    _autoSessionCreating = false;
  }

  void _flushDeferredFirstInputIfNeeded() {
    final deferred = _deferredFirstInput;
    if (deferred == null) {
      return;
    }
    _deferredFirstInput = null;
    _autoSessionRequested = false;
    _autoSessionCreating = false;
    sendInputText(deferred.text);
  }

  void loadSession(String sessionId) {
    final targetId = sessionId.trim();
    if (targetId.isEmpty) {
      return;
    }
    _connectionStage =
        _connected ? SessionConnectionStage.catchingUp : _connectionStage;
    _beginSessionLoading(targetId: targetId);
    _service.send({
      'action': 'session_load',
      'sessionId': targetId,
      'cwd': effectiveCwd,
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
    _sessionLoadingTimeout?.cancel();
    _isLoadingSession = true;
    _pendingSessionTargetId = targetId.trim();
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
    _activityHideDebounce?.cancel();
    _activityHideDebounce = null;
    _isSubmitting = false;
    _isSubmittingBaselineKey = '';
    _aiStatusHideDebounce?.cancel();
    _aiStatusHideDebounce = null;
    _aiStatusIndicatorVisible = false;
    _aiStatusIndicatorLabel = '思考中';
    _resetRuntimeProcessState();
    _sessionPermissionRules.clear();
    _persistentPermissionRules.clear();
    _sessionPermissionRulesEnabled = true;
    _persistentPermissionRulesEnabled = false;
    _resetActionNeededTracking(suppressNextSignal: true);
    _syncDerivedState();
    notifyListeners();
  }

  void _resetNewSessionState() {
    _canResumeCurrentSession = false;
    _resumeRuntimeMeta = const RuntimeMeta();
    _runtimePermissionMode = '';
    _runtimeInfo = null;
    _agentState = null;
    _runtimePhase = null;
    _sessionState = null;
    _pendingPrompt = null;
    _pendingInteraction = null;
    _clearPlanInteractionState();
    _currentStep = null;
    _currentStepSummary = '';
    _lastStepMessage = '';
    _lastStepStatus = '';
    _activityToolLabel = '';
    _activityStartedAt = null;
    _activityVisible = false;
    _activityHideDebounce?.cancel();
    _activityHideDebounce = null;
    _isSubmitting = false;
    _isSubmittingBaselineKey = '';
    _aiStatusHideDebounce?.cancel();
    _aiStatusHideDebounce = null;
    _aiStatusIndicatorVisible = false;
    _aiStatusIndicatorLabel = '思考中';
    _currentDiff = null;
    _latestError = null;
    _recentDiffs.clear();
    _reviewGroups.clear();
    _activeReviewGroupId = '';
    _activeReviewDiffId = '';
    _timeline.clear();
    _terminalStdout = '';
    _terminalStderr = '';
    _activeTerminalExecutionId = '';
    _lastAssistantReplyExecutionKey = '';
    _terminalExecutions.clear();
    _resetRuntimeProcessState();
    _sessionContext = const SessionContext();
    _pendingSessionContextTarget = null;
    _pendingToggleSkillNames.clear();
    _pendingToggleMemoryIds.clear();
    _lastLogMessage = '';
    _lastLogStream = '';
    _lastLogAt = null;
    _lastSessionTimelineKey = '';
    _resetActionNeededTracking(suppressNextSignal: true);
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

  bool _eventTargetsCurrentSession(String sessionId) {
    final normalized = sessionId.trim();
    if (normalized.isEmpty) {
      return _selectedSessionId.trim().isEmpty;
    }
    final selected = _selectedSessionId.trim();
    return selected.isEmpty || normalized == selected;
  }

  void _finishSessionLoading({String sessionId = ''}) {
    if (!_isLoadingSession) {
      return;
    }
    _sessionLoadingTimeout?.cancel();
    _sessionLoadingTimeout = null;
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
    // 用户在 dropdown 的选择是权威意图，覆盖 sync 出来的旧 sessionState/agentState
    // 缓存值，等后端回包刷新 _agentState 后再由后续 sync 接管。
    _runtimePermissionMode = _normalizeDisplayPermissionMode(permissionMode);
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
    final normalizedModel =
        engine == 'codex' && model.trim().toLowerCase() == 'default'
            ? ''
            : _resolvedAiModel(engine, model);
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
          ? 'Codex 模型已切换为 ${normalizedModel.isEmpty ? 'Default' : _codexModelDisplayLabel(normalizedModel)} · ${normalizedEffort.toUpperCase()}，将对下一次 Codex 启动生效'
          : 'Claude 模型已切换为 ${_claudeModelLabel(normalizedModel)}，将对下一次 Claude 启动生效',
    );
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
    final diff = reviewActionTargetDiff;
    if (diff == null || diff.diff.isEmpty) {
      _pushSystem('error', '当前没有待审核的 diff');
      return;
    }
    final interactionType = _pendingInteraction == null
        ? '-'
        : (_pendingInteraction!.isReview
            ? 'review'
            : _pendingInteraction!.isPermission
                ? 'permission'
                : _pendingInteraction!.kind);
    _pushDebug(
      '发送 review_decision',
      'targetId=${diff.id} groupId=${diff.groupId} interactionType=$interactionType',
    );
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
    _service.send({'action': 'memory_sync_pull', 'cwd': effectiveCwd});
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

  void requestTaskSnapshot() {
    if (!_connected || _connecting) {
      return;
    }
    _service.send({
      'action': 'task_snapshot_get',
      'sessionId': _selectedSessionId.trim(),
      'cwd': effectiveCwd,
    });
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
    _pendingSessionContextTarget = next;
    _pendingToggleSkillNames
      ..clear()
      ..addAll(_diffPendingNames(
        _sessionContext.enabledSkillNames,
        next.enabledSkillNames,
      ));
    _pendingToggleMemoryIds
      ..clear()
      ..addAll(_diffPendingNames(
        _sessionContext.enabledMemoryIds,
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
    _selectAiEngine(resolvedEngine);
    _beginUserSubmission();
    final launchPayload = _aiTurnPayload(
      engine: resolvedEngine,
      meta: meta ?? currentMeta,
      permissionMode: _config.permissionMode,
      data: value.isNotEmpty ? '$value\n' : '',
    );
    final launchSent = _sendUserVisibleAction(
      launchPayload,
      userText: value,
      label: '命令',
      queueOnFailure: true,
    );
    if (!launchSent) {
      return;
    }
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
    final continuationEngine = _resolvedAiEngine(
      command: (meta ?? currentMeta).command,
      engine: (meta ?? currentMeta).engine,
    );
    _beginUserSubmission();
    final payload = _aiTurnPayload(
      engine: continuationEngine,
      meta: meta ?? currentMeta,
      permissionMode: _config.permissionMode,
      data: '$value\n',
    );
    final sent = _sendUserVisibleAction(
      payload,
      userText: value,
      label: label,
    );
    if (!sent) {
      return;
    }
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
    final lower = value.toLowerCase();
    final isAiCommand = _isAiCommand(lower);
    if (isAiCommand) {
      _continueSameSessionEnabled = false;
      _continuedSameSessionId = '';
    }
    if (canSendToContinuedSameSession) {
      _resetActionNeededTracking();
      _submitClaudeContinuation(value, label: '回复');
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
    if (_shouldAutoCreateSessionOnFirstInput()) {
      _deferFirstInputAndCreateSession(value);
      return;
    }
    if (isAiCommand) {
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
      return;
    }
    if (shouldShowClaudeMode) {
      final backendAwaitInput = _agentState?.awaitInput == true;
      if (isSessionBusy &&
          !_canBypassBusyGuardForCodexContinuation &&
          !backendAwaitInput) {
        _pushSystem('session', '当前 AI 助手会话仍在处理中，请稍后再试。');
        return;
      }
      _resetActionNeededTracking();
      _submitClaudeContinuation(value, label: '回复');
      return;
    }
    _beginUserSubmission();
    final payload = {
      'action': 'exec',
      'cmd': value,
      'cwd': effectiveCwd,
      'mode': 'pty',
      ...currentMeta.toJson(),
      'permissionMode': _config.permissionMode,
    };
    if (!_sendUserVisibleAction(payload, userText: value, label: '命令')) {
      return;
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
    if (prompt?.isReview == true) {
      sendReviewDecision(normalized);
      return;
    }
    if (prompt?.isPermission == true || hasPendingPermissionPrompt) {
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
      final decisionMeta = interaction.runtimeMeta.merge(
        RuntimeMeta(
          resumeSessionId: interaction.resumeSessionId,
          contextId: interaction.contextId,
          contextTitle: interaction.contextTitle,
          targetPath: interaction.targetPath,
          permissionMode: _currentDecisionPermissionMode,
        ),
      );
      _service.send({
        'action': 'permission_decision',
        'sessionId': _selectedSessionId,
        'decision': selection.decision,
        if (selection.scope.isNotEmpty) 'scope': selection.scope,
        'permissionMode': _currentDecisionPermissionMode,
        'permissionRequestId': decisionMeta.permissionRequestId,
        'resumeSessionId': interaction.resumeSessionId,
        'targetPath': interaction.targetPath,
        'contextId': interaction.contextId,
        'contextTitle': interaction.contextTitle,
        'promptMessage': interaction.message,
        'command': decisionMeta.command,
        'cwd': decisionMeta.cwd.isNotEmpty ? decisionMeta.cwd : effectiveCwd,
        'engine': decisionMeta.engine.isNotEmpty
            ? decisionMeta.engine
            : _config.engine,
        'target': decisionMeta.target,
        'targetType': decisionMeta.targetType,
      });
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
    _beginUserSubmission();
    final payload = {
      'action': 'input',
      'data': '$value\n',
      'permissionMode': _config.permissionMode,
    };
    if (!_sendUserVisibleAction(payload, userText: value, label: promptLabel)) {
      return;
    }
    notifyListeners();
  }

  void _sendPermissionDecision(
    PromptRequestEvent? prompt,
    _PermissionDecisionSelection selection, {
    String promptLabel = '回复',
  }) {
    final promptSnapshot = prompt ?? _pendingPrompt;
    final promptMeta = promptSnapshot?.runtimeMeta ?? const RuntimeMeta();
    final baseMeta = currentMeta;
    final promptRequestId = promptMeta.permissionRequestId.trim();
    final targetPath = promptMeta.targetPath;
    final contextTitle = promptMeta.contextTitle.isNotEmpty
        ? promptMeta.contextTitle
        : promptMeta.targetTitle;
    final promptMessage = promptSnapshot?.message.trim().isNotEmpty == true
        ? promptSnapshot!.message
        : _runtimePhase?.message;
    final decisionMeta = promptMeta.merge(
      RuntimeMeta(
        resumeSessionId: promptMeta.resumeSessionId.isNotEmpty
            ? promptMeta.resumeSessionId
            : baseMeta.resumeSessionId,
        contextId: promptMeta.contextId,
        contextTitle: contextTitle,
        command: promptMeta.command.isNotEmpty
            ? promptMeta.command
            : baseMeta.command,
        engine:
            promptMeta.engine.isNotEmpty ? promptMeta.engine : baseMeta.engine,
        cwd: promptMeta.cwd.isNotEmpty ? promptMeta.cwd : baseMeta.cwd,
        target: promptMeta.target,
        targetType: promptMeta.targetType,
        targetPath: targetPath,
        permissionMode: _currentDecisionPermissionMode,
      ),
    );
    _service.send({
      'action': 'permission_decision',
      'sessionId': _selectedSessionId,
      'decision': selection.decision,
      if (selection.scope.isNotEmpty) 'scope': selection.scope,
      'permissionMode': _currentDecisionPermissionMode,
      'permissionRequestId': promptRequestId,
      'resumeSessionId': decisionMeta.resumeSessionId,
      'targetPath': targetPath,
      'contextId': decisionMeta.contextId,
      'contextTitle': contextTitle,
      'promptMessage': promptMessage,
      'command': decisionMeta.command,
      'cwd': decisionMeta.cwd.isNotEmpty ? decisionMeta.cwd : effectiveCwd,
      'engine':
          decisionMeta.engine.isNotEmpty ? decisionMeta.engine : _config.engine,
      'target': decisionMeta.target,
      'targetType': decisionMeta.targetType,
    });
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

  void requestCodexModelCatalog({bool force = false}) {
    if (!_connected) {
      return;
    }
    if (_codexModelCatalogLoading && !force) {
      return;
    }
    _codexModelCatalogLoading = true;
    _codexModelCatalogMessage = 'Codex 原生模型目录同步中...';
    _codexModelCatalogUnavailable = false;
    _service.send({
      'action': 'runtime_info',
      'query': 'codex_models',
      'cwd': effectiveCwd,
    });
    notifyListeners();
  }

  void requestClaudeModelCatalog({bool force = false}) {
    if (!_connected) {
      return;
    }
    if (_claudeModelCatalogLoading && !force) {
      return;
    }
    _claudeModelCatalogLoading = true;
    _claudeModelCatalogMessage = 'Claude 模型目录同步中...';
    _claudeModelCatalogUnavailable = false;
    _service.send({
      'action': 'runtime_info',
      'query': 'claude_models',
      'cwd': effectiveCwd,
    });
    notifyListeners();
  }

  CodexModelCatalogEntry? codexModelCatalogEntry(String model) {
    return _findCodexModelCatalogEntry(model);
  }

  List<CodexReasoningEffortOption> codexReasoningEffortOptionsForModel(
      String model) {
    final entry = _findCodexModelCatalogEntry(model);
    if (entry == null) {
      return const <CodexReasoningEffortOption>[];
    }
    return List.unmodifiable(entry.reasoningEffortOptions);
  }

  String codexModelDisplayLabel(String model) {
    return _codexModelDisplayLabel(model);
  }

  String aiModelSheetSummary(
    String engine,
    String model,
    String reasoningEffort,
  ) {
    return _displayAiModelSummary(engine, model, reasoningEffort);
  }

  String preferredCodexReasoningEffortForModel(
    String model, {
    String fallback = '',
  }) {
    return _preferredCodexReasoningEffortForModel(
      model,
      fallback: fallback,
    );
  }

  void stopCurrentRun() {
    if (!canStopCurrentRun) {
      return;
    }
    _canResumeCurrentSession = false;
    _resumeRuntimeMeta = const RuntimeMeta();
    _runtimeInfo = null;
    _pendingPrompt = null;
    _pendingInteraction = null;
    _runtimePhase = null;
    _agentState = null;
    _sessionState = null;
    _lastAssistantReplyExecutionKey = '';
    _isSubmitting = false;
    _isSubmittingBaselineKey = '';
    _activityHideDebounce?.cancel();
    _activityHideDebounce = null;

    // 设置停止中标记，按钮立即变灰，但状态栏继续显示
    _isStopping = true;

    _service.send({'action': 'stop'});
    _syncDerivedState();
    notifyListeners();
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
        final payload = {
          'action': 'slash_command',
          'command': raw,
          'cwd': effectiveCwd,
          'engine': _config.engine,
          ...meta.toJson(),
          'permissionMode': _config.permissionMode,
        };
        if (!_sendUserVisibleAction(payload, userText: raw, label: 'Slash')) {
          return;
        }
    }
  }

  void _handleEvent(AppEvent event) async {
    _lastServerEventAt = DateTime.now();
    _trackSessionEventCursor(event);
    var needsDerivedSync = true;
    switch (event) {
      case ClientActionAckEvent ack:
        _handleClientActionAck(ack);
        break;
      case SessionCreatedEvent created:
        _connectionStage = SessionConnectionStage.ready;
        _connectionMessage = '已连接';
        _autoSessionRequested = false;
        _autoSessionCreating = false;
        _selectedSessionId = created.summary.id;
        _selectedSessionTitle = sessionDisplayTitle(created.summary);
        _selectedSessionExternalNative =
            _isExternalNativeSession(created.summary);
        _sendCachedPushTokenIfPossible();
        _resetNewSessionState();
        _upsertSession(created.summary);
        requestSessionContext();
        requestPermissionRuleList();
        _finishSessionLoading(sessionId: created.summary.id);
        _flushDeferredFirstInputIfNeeded();
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
        _sessionListSyncedSinceConnect = true;
        final selectedSessionId = _selectedSessionId.trim();
        if (selectedSessionId.isNotEmpty &&
            !mergedItems.any((item) => item.id == selectedSessionId)) {
          final preservedSelected =
              _sessions.cast<SessionSummary?>().firstWhere(
                    (existing) => existing?.id == selectedSessionId,
                    orElse: () => null,
                  );
          if (preservedSelected != null) {
            mergedItems.insert(0, preservedSelected);
          }
        }
        _sessions
          ..clear()
          ..addAll(mergedItems);
        _handleAutoSessionBinding(mergedItems);
        break;
      case SessionHistoryEvent history:
        // 正在创建或加载会话中，不覆盖 _selectedSessionId，避免与 auto-create 竞态
        if (_isLoadingSession) {
          break;
        }
        _connectionStage = SessionConnectionStage.ready;
        _connectionMessage = '已连接';
        _autoSessionRequested = false;
        _autoSessionCreating = false;
        // 保留阻塞型权限/审查/计划提示，防止切后台重连后被误清
        if (!_shouldPreserveBlockingPrompt()) {
          _pendingPrompt = null;
          _pendingInteraction = null;
          _clearPlanInteractionState();
        }
        _resetActionNeededTracking(suppressNextSignal: true);
        final resolvedHistorySummary =
            _resolvedHistorySummary(history.summary, history.logEntries);
        _selectedSessionId = resolvedHistorySummary.id;
        _selectedSessionTitle = sessionDisplayTitle(resolvedHistorySummary);
        _selectedSessionExternalNative =
            _isExternalNativeSession(resolvedHistorySummary);
        _executionActive = resolvedHistorySummary.executionActive;
        _sendCachedPushTokenIfPossible();
        _sessionContext = history.sessionContext;
        _skillCatalogMeta = history.skillCatalogMeta;
        _memoryCatalogMeta = history.memoryCatalogMeta;
        _runtimePhase = null;
        _runtimePermissionMode =
            history.resumeRuntimeMeta.permissionMode.trim();
        _lastAssistantReplyExecutionKey = '';
        _upsertSession(resolvedHistorySummary);
        _restoreTimelineFromHistory(
          history.logEntries,
          history.resumeRuntimeMeta,
        );
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
        _sessionRuntimeAlive = history.runtimeAlive;
        _continueSameSessionEnabled = false;
        _continuedSameSessionId = '';
        _terminalExecutions
          ..clear()
          ..addAll(history.terminalExecutions);
        _restoreTerminalLogs(history.rawTerminalByStream);
        _sessionDeltaKnown[resolvedHistorySummary.id] = SessionDeltaKnown(
          eventCursor: _sessionEventCursors[resolvedHistorySummary.id] ?? 0,
          logEntryCount: history.logEntries.length,
          diffCount: history.diffs.length,
          terminalExecutionCount: history.terminalExecutions.length,
          terminalStdoutLength: _terminalStdout.length,
          terminalStderrLength: _terminalStderr.length,
        );
        _syncActiveTerminalExecution();
        _resetRuntimeProcessState();
        requestRuntimeProcessList();
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
        if (_pendingNotificationSessionTargetId.trim() ==
            resolvedHistorySummary.id) {
          _pendingNotificationSessionTargetId = '';
        }
        final restoredCwd = history.resumeRuntimeMeta.cwd.trim();
        final targetCwd = restoredCwd.isNotEmpty ? restoredCwd : _config.cwd;
        await switchWorkingDirectory(targetCwd);
        _requestSessionDelta(reason: 'history_loaded');
        if (!_timeline.any(_hasVisibleTimelineContent)) {
          _pushSystem('session', '会话已就绪，可以继续输入');
        }
        break;
      case SessionDeltaEvent delta:
        _handleSessionDelta(delta);
        break;
      case SessionResumeResultEvent result:
        if (!_eventTargetsCurrentSession(result.sessionId)) {
          break;
        }
        if (result.sessionId.trim().isNotEmpty) {
          final previous = _sessionEventCursors[result.sessionId.trim()] ?? 0;
          if (result.latestCursor > previous) {
            _sessionEventCursors[result.sessionId.trim()] = result.latestCursor;
          }
        }
        _connectionStage = SessionConnectionStage.ready;
        _connectionMessage = '已连接';
        _sessionRuntimeAlive = result.runtimeAlive;
        if (!result.runtimeAlive && !canSendToContinuedSameSession) {
          _continueSameSessionEnabled = false;
          _continuedSameSessionId = '';
        }
        _syncObservedSessionPolling();
        break;
      case SessionResumeNoticeEvent notice:
        _emitResumeNotification(notice);
        break;
      case SessionStateEvent state:
        if (!_eventTargetsCurrentSession(state.sessionId)) {
          break;
        }
        _sessionState = state;
        if (_isIdleLikeState(state.state) ||
            state.state.trim().toLowerCase() == 'stopped') {
          _sessionRuntimeAlive = false;
          if (!canSendToContinuedSameSession) {
            _continueSameSessionEnabled = false;
            _continuedSameSessionId = '';
          }
        }
        _maybeAutoSyncAiModel(state.runtimeMeta);
        _syncRuntimePermissionMode();

        // 收到 stopped 事件后，清除停止中标记
        if (state.state.trim().toLowerCase() == 'stopped') {
          _isStopping = false;
        }

        if (_isLoadingSession &&
            _matchesPendingSessionTarget(state.sessionId)) {
          _finishSessionLoading(sessionId: state.sessionId);
        }
        if (_connected) {
          _restorePendingNotificationSessionIfNeeded();
        }
        if (_isIdleLikeState(state.state)) {
          _markTerminalExecutionFinished(
            state.runtimeMeta,
            finishedAt: state.timestamp,
          );
        }
        if (_isIdleLikeState(state.state) && !_shouldPreserveBlockingPrompt()) {
          _pendingInteraction = null;
          _pendingPrompt = null;
          _runtimePhase = null;
          _agentState = null;
        }
        _connectionMessage =
            state.message.isNotEmpty ? state.message : state.state;
        _syncDerivedState();
        _syncObservedSessionPolling();
        _handleSessionStateTimeline(state);
        break;
      case TaskSnapshotEvent snapshot:
        if (!_eventTargetsCurrentSession(snapshot.sessionId)) {
          break;
        }
        final ignoredAsStale = _handleTaskSnapshot(snapshot);
        if (!ignoredAsStale) {
          // Heartbeat snapshots use the backend runner state, which doesn't
          // track external native (desktop Claude) processes. Trust the
          // session history/delta events for runtimeAlive in that case.
          final isExternalHeartbeatIdle = _selectedSessionExternalNative &&
              !snapshot.syncing &&
              !snapshot.runtimeAlive;
          if (!isExternalHeartbeatIdle) {
            _sessionRuntimeAlive = snapshot.runtimeAlive;
          }
          if (!snapshot.runtimeAlive && !canSendToContinuedSameSession) {
            _continueSameSessionEnabled = false;
            _continuedSameSessionId = '';
          }
        }
        _syncObservedSessionPolling();
        break;
      case AgentStateEvent agent:
        if (!_eventTargetsCurrentSession(agent.sessionId)) {
          break;
        }
        _agentState = agent;
        final agentStateName = agent.state.trim().toUpperCase();
        // Only lift to true; never force to false — delta/history events
        // carry the authoritative runtimeAlive and defeat stale override.
        if (agentStateName == 'THINKING' ||
            agentStateName == 'RECOVERING' ||
            agentStateName == 'RUNNING' ||
            agentStateName == 'RUNNING_TOOL' ||
            agentStateName == 'WAIT_INPUT') {
          _sessionRuntimeAlive = true;
        }
        // 提交保护锁解锁：当后端回报新的 executionKey（说明本轮已经被运行时确认接管），
        // 或进入 IDLE / WAIT_INPUT / awaitInput 等终止/阻塞态，就把保护锁解开。
        if (_isSubmitting) {
          final newKey = _runtimeExecutionKey(agent.runtimeMeta);
          final freshTurnObserved =
              newKey.isNotEmpty && newKey != _isSubmittingBaselineKey;
          final terminalSignal = agent.awaitInput ||
              agentStateName == 'IDLE' ||
              agentStateName == 'WAIT_INPUT';
          if (freshTurnObserved || terminalSignal) {
            _isSubmitting = false;
            _isSubmittingBaselineKey = '';
          }
        }
        _maybeAutoSyncAiModel(agent.runtimeMeta);
        _syncRuntimePermissionMode();
        // AI 正在运行中，清掉 pending prompt，防止阻塞态残留导致 awaitInput 误判。
        // 但保留权限/审查/计划等阻塞型提示，防止 RECOVERING 等中间态误清。
        if (!_isIdleLikeState(agent.state) &&
            !agent.awaitInput &&
            !_shouldPreserveBlockingPrompt()) {
          _pendingPrompt = null;
          _pendingInteraction = null;
        }
        if (_isIdleLikeState(agent.state) || agent.awaitInput) {
          _markTerminalExecutionFinished(
            agent.runtimeMeta,
            finishedAt: agent.timestamp,
          );
        }
        if ((_isIdleLikeState(agent.state) || agent.awaitInput) &&
            !_shouldPreserveBlockingPrompt()) {
          _pendingInteraction = null;
          _pendingPrompt = null;
          _runtimePhase = null;
        }
        _checkAndClearExecutionState(agent.state);
        _syncStepSummary(
          message: agent.step.isNotEmpty ? agent.step : agent.message,
          status: agent.state,
          tool: agent.tool,
          command: agent.command,
          targetPath: agent.runtimeMeta.targetPath,
        );
        _syncDerivedState();
        _syncObservedSessionPolling();
        break;
      case AIStatusEvent status:
        if (!_eventTargetsCurrentSession(status.sessionId)) {
          break;
        }
        _setAiStatusVisible(status.visible, label: status.label);
        break;
      case RuntimePhaseEvent runtimePhase:
        _runtimePhase = runtimePhase;
        _syncRuntimePermissionMode();
        break;
      case LogEvent log:
        _appendTerminalLog(
          log.stream,
          log.message,
          executionId: log.runtimeMeta.executionId,
          timestamp: log.timestamp,
          meta: log.runtimeMeta,
        );
        _maybeAutoSyncAiModel(
          log.runtimeMeta,
          rawText: log.message,
        );
        _handleLogTimeline(log);
        break;
      case ProgressEvent progress:
        _maybeAutoSyncAiModel(
          progress.runtimeMeta,
          rawText: progress.message,
        );
        if (progress.message.isNotEmpty && _currentStepSummary.isEmpty) {
          _currentStepSummary = progress.message;
        }
        _syncDerivedState();
        break;
      case ErrorEvent error:
        _fileListLoading = false;
        _fileReading = false;
        final errorMessage = error.message.trim();
        if (error.code == 'ws_closed' ||
            error.code == 'ws_stream_error' ||
            error.code == 'ws_send_error') {
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
        // 后端会自动检查权限规则并应用，前端只需等待 PermissionAutoAppliedEvent
        _syncDerivedState();
        notifyListeners();
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
        // 步骤更新说明 AI 正在工作中，清掉等待输入状态
        // 但保留权限/审查/计划等阻塞型提示
        if (!_shouldPreserveBlockingPrompt()) {
          _pendingPrompt = null;
          _pendingInteraction = null;
        }
        _syncDerivedState();
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
        if (_pendingInteraction?.isPermission == true &&
            !_shouldPreserveBlockingPrompt()) {
          _pendingInteraction = null;
        }
        if (_pendingPrompt?.isPermission == true &&
            !_shouldPreserveBlockingPrompt()) {
          _pendingPrompt = null;
        }
        if (_normalizeDisplayPermissionMode(_runtimePermissionMode) == 'auto' ||
            _normalizeDisplayPermissionMode(diff.runtimeMeta.permissionMode) ==
                'auto') {
          _runtimePermissionMode = _config.permissionMode;
        }
        _runtimePhase = RuntimePhaseEvent(
          timestamp: diff.timestamp,
          sessionId: diff.sessionId,
          runtimeMeta: diff.runtimeMeta.merge(
            RuntimeMeta(
              blockingKind: 'review',
              permissionMode: _runtimePermissionMode,
            ),
          ),
          raw: const {'type': 'runtime_phase'},
          phase: 'reviewing',
          kind: 'review',
          message: '等待审核',
        );
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
        if (runtimeInfo.query.trim().toLowerCase() == 'codex_models') {
          _codexModelCatalogLoading = false;
          _codexModelCatalogMessage = runtimeInfo.message.trim();
          _codexModelCatalogUnavailable = runtimeInfo.unavailable;
          final nextCatalog = runtimeInfo.items
              .where((item) => item.meta.isNotEmpty)
              .map(CodexModelCatalogEntry.fromRuntimeInfoItem)
              .where((item) => item.model.trim().isNotEmpty && !item.hidden)
              .toList();
          _codexModelCatalog
            ..clear()
            ..addAll(nextCatalog);
          _maybeAutoSyncAiModel(
            runtimeInfo.runtimeMeta,
            runtimeInfo: runtimeInfo,
          );
          break;
        }
        if (runtimeInfo.query.trim().toLowerCase() == 'claude_models') {
          _claudeModelCatalogLoading = false;
          _claudeModelCatalogMessage = runtimeInfo.message.trim();
          _claudeModelCatalogUnavailable = runtimeInfo.unavailable;
          final nextCatalog = runtimeInfo.items
              .map(ClaudeModelCatalogEntry.fromRuntimeInfoItem)
              .where((item) => item.model.trim().isNotEmpty)
              .toList();
          _claudeModelCatalog
            ..clear()
            ..addAll(nextCatalog);
          _maybeAutoSyncAiModel(
            runtimeInfo.runtimeMeta,
            runtimeInfo: runtimeInfo,
          );
          break;
        }
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
      case CatalogAuthoringResultEvent result:
        final message = result.message.trim();
        if (message.isNotEmpty) {
          _pushSystem('catalog', message);
        }
        break;
      case SessionContextResultEvent result:
        _sessionContext = result.sessionContext;
        _pendingSessionContextTarget = null;
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
        // 后端已自动应用权限规则，清空 pending 状态
        _pendingPrompt = null;
        _pendingInteraction = null;
        if (result.message.trim().isNotEmpty) {
          _pushSystem('session', result.message.trim());
        }
        requestPermissionRuleList();
        _syncDerivedState();
        notifyListeners();
        break;
      case SkillSyncResultEvent result:
        _skillSyncStatus =
            result.message.isNotEmpty ? result.message : 'skill 同步完成';
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
        needsDerivedSync = false;
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
        needsDerivedSync = false;
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
        needsDerivedSync = false;
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
        needsDerivedSync = false;
        if (answer.serial.trim().isNotEmpty) {
          _adbSelectedSerial = answer.serial.trim();
        }
        unawaited(_adbWebRtc.applyAnswer(answer.sdpType, answer.sdp));
        _adbStatus = 'WebRTC answer 已收到，等待连接…';
        break;
      case AdbWebRtcStateEvent state:
        needsDerivedSync = false;
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
      case PongEvent _:
        needsDerivedSync = false;
        break;
      case UnknownEvent unknown:
        needsDerivedSync = false;
        _pushSystem('system', '收到未识别事件：${unknown.type}');
        break;
      default:
        break;
    }
    if (needsDerivedSync) {
      _syncDerivedState();
    }
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
    if (_autoSessionCreating) {
      _clearDeferredFirstInput();
    }
    if (_pendingSessionContextTarget != null) {
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

  void _checkAndClearExecutionState(String agentState) {
    final normalized = agentState.trim().toUpperCase();
    if (_isIdleLikeState(normalized) || normalized == 'WAIT_INPUT') {
      _executionActive = false;
      _activityStartedAt = null;
      _activityToolLabel = '';
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
    String normalized,
  ) {
    final reviewedDiffId = _diffIdentity(diff);
    final reviewOnly = normalized != 'revert';
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
      'is_review_only': reviewOnly,
    });
    if (normalized != 'revise') {
      _pendingPrompt = null;
      if (_pendingInteraction?.isReview == true) {
        _pendingInteraction = null;
      }
    }
    _markDiffReviewState(
      diff,
      keepPending: normalized == 'revise',
      reviewStatus: _reviewStatusFromDecision(normalized),
    );
    _syncReviewGroupsFromRecentDiffs();
    _advanceReviewSelectionAfterDecision(diff, reviewedDiffId: reviewedDiffId);
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

    _activeReviewGroupId = '';
    _activeReviewDiffId = '';
  }

  void _handleSessionDelta(SessionDeltaEvent delta) {
    if (delta.requiresFullSync) {
      _requestSessionResume(reason: 'delta_base_mismatch');
      return;
    }
    // 正在创建或加载会话中，不覆盖 _selectedSessionId，避免与 auto-create 竞态
    if (_isLoadingSession) {
      return;
    }
    _connectionStage = SessionConnectionStage.ready;
    if (!hasPendingPermissionPrompt) {
      _connectionMessage = 'Syncing latest progress...';
    }
    _autoSessionRequested = false;
    _autoSessionCreating = false;
    final resolvedSummary =
        _resolvedHistorySummary(delta.summary, delta.appendLogEntries);
    if (resolvedSummary.id.trim().isNotEmpty) {
      _selectedSessionId = resolvedSummary.id;
      _selectedSessionTitle = sessionDisplayTitle(resolvedSummary);
      _selectedSessionExternalNative =
          _isExternalNativeSession(resolvedSummary);
      _executionActive = resolvedSummary.executionActive;
      _upsertSession(resolvedSummary);
    }
    _sessionContext = delta.sessionContext;
    _skillCatalogMeta = delta.skillCatalogMeta;
    _memoryCatalogMeta = delta.memoryCatalogMeta;
    _runtimePermissionMode = delta.resumeRuntimeMeta.permissionMode.trim();
    _canResumeCurrentSession = delta.canResume;
    _resumeRuntimeMeta = delta.resumeRuntimeMeta;
    _sessionRuntimeAlive = delta.runtimeAlive;
    if (!delta.runtimeAlive && !canSendToContinuedSameSession) {
      _continueSameSessionEnabled = false;
      _continuedSameSessionId = '';
    }
    if (delta.latest.eventCursor > 0) {
      final sessionId = delta.sessionId.trim();
      final previous = _sessionEventCursors[sessionId] ?? 0;
      if (delta.latest.eventCursor > previous) {
        _sessionEventCursors[sessionId] = delta.latest.eventCursor;
      }
    }
    if (delta.sessionId.trim().isNotEmpty) {
      _sessionDeltaKnown[delta.sessionId.trim()] = delta.latest;
    }
    for (final entry in delta.appendLogEntries) {
      _appendTimelineItem(
        _timelineFromHistory(entry, delta.resumeRuntimeMeta),
        emitNotifications: false,
      );
    }
    for (final diff in delta.upsertDiffs) {
      _mergeRecentDiff(diff);
    }
    if (delta.reviewGroups.isNotEmpty || delta.activeReviewGroup != null) {
      _reviewGroups
        ..clear()
        ..addAll(delta.reviewGroups.map(_normalizeReviewGroup));
      _activeReviewGroupId = delta.activeReviewGroup?.id ?? '';
      _syncReviewGroupsFromRecentDiffs();
      _syncActiveReviewSelection();
    }
    _currentStep = delta.currentStep ?? _currentStep;
    _currentStepSummary = _summaryFromHistoryContext(_currentStep);
    _latestError = delta.latestError ?? _latestError;
    _mergeTerminalExecutions(delta.terminalExecutions);
    _appendTerminalLogs(delta.rawTerminalByStream);
    if (delta.currentDiff != null) {
      final current = _normalizeHistoryDiff(delta.currentDiff!);
      _mergeRecentDiff(current);
      _currentDiff = FileDiffEvent(
        timestamp: DateTime.now(),
        sessionId: delta.sessionId,
        runtimeMeta: delta.resumeRuntimeMeta.merge(
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
    }
    _syncDerivedState();
    notifyListeners();
  }

  void _restoreTimelineFromHistory(
    List<HistoryLogEntry> entries,
    RuntimeMeta resumeMeta,
  ) {
    _timeline.clear();
    for (final entry in entries) {
      _appendTimelineItem(
        _timelineFromHistory(entry, resumeMeta),
        emitNotifications: false,
      );
    }
  }

  TimelineItem _timelineFromHistory(
    HistoryLogEntry entry,
    RuntimeMeta resumeMeta,
  ) {
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
      meta: _historyRuntimeMetaForEntry(entry, resumeMeta),
      context: entry.context,
      animateBody: false,
    );
  }

  RuntimeMeta _historyRuntimeMetaForEntry(
    HistoryLogEntry entry,
    RuntimeMeta resumeMeta,
  ) {
    final context = entry.context;
    final targetPath = (context?.path ?? '').trim().isNotEmpty
        ? context!.path
        : context?.targetPath ?? '';
    return resumeMeta.merge(
      RuntimeMeta(
        executionId: entry.executionId,
        contextId: context?.id ?? '',
        contextTitle: context?.title ?? '',
        targetPath: targetPath,
        targetDiff: context?.diff ?? '',
        targetTitle: context?.title ?? '',
        command: context?.command ?? '',
        source: context?.source ?? '',
        skillName: context?.skillName ?? '',
        groupId: context?.groupId ?? '',
        groupTitle: context?.groupTitle ?? '',
      ),
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
        animateBody: false,
      ),
    );
  }

  bool _hasVisibleTimelineContent(TimelineItem item) {
    return item.body.trim().isNotEmpty || item.title.trim().isNotEmpty;
  }

  String _restoredHistoryBody(HistoryLogEntry entry) {
    if (_isBootstrapSource(entry.context?.source ?? '')) {
      return '';
    }
    if (entry.kind == 'terminal') {
      final body = entry.text.isNotEmpty ? entry.text : entry.message;
      return _sanitizeTimelineLogMessage(
        _sanitizeAiBootstrapReply(body, entry.context?.command ?? ''),
      );
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

  bool _isExternalNativeSession(SessionSummary summary) {
    final ownership = summary.ownership.trim().toLowerCase();
    // Authoritative ownership field set by backend at session creation.
    if (ownership == 'mobilevc') {
      return false;
    }
    if (ownership == 'claude-native' || ownership == 'codex-native') {
      return true;
    }
    // Fallback for legacy sessions without ownership field.
    final source = summary.source.trim().toLowerCase();
    final runtimeSource = summary.runtime.source.trim().toLowerCase();
    return summary.external ||
        source == 'codex-native' ||
        source == 'claude-native' ||
        runtimeSource == 'codex-native' ||
        runtimeSource == 'claude-native';
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
              ownership: summary.ownership,
              executionActive: summary.executionActive,
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
      ownership: incoming.ownership.isNotEmpty
          ? incoming.ownership
          : existing?.ownership ?? '',
      executionActive:
          incoming.executionActive || (existing?.executionActive ?? false),
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
    _appendTimelineItem(item);
  }

  void _appendTimelineItem(
    TimelineItem item, {
    bool emitNotifications = true,
  }) {
    if (!_shouldKeepTimelineItem(item)) {
      return;
    }
    if (_timeline.any((previous) => previous.id == item.id)) {
      return;
    }
    if (_isDuplicateUserTimelineItem(item)) {
      return;
    }
    if (_isDuplicateAssistantTimelineItem(item)) {
      return;
    }
    if (_shouldMergeIntoPreviousTimelineItem(item)) {
      final previous = _timeline.removeLast();
      final mergedItem = TimelineItem(
        id: previous.id,
        kind: _mergedTimelineKind(previous, item),
        timestamp: item.timestamp,
        title: previous.title,
        body: _mergeTimelineBodies(previous.body, item.body),
        stream: previous.stream,
        status: item.status.isNotEmpty ? item.status : previous.status,
        meta: item.meta,
        context: item.context ?? previous.context,
        animateBody: previous.animateBody || item.animateBody,
      );
      _timeline.add(mergedItem);
      if (emitNotifications) {
        _emitTimelineNotification(
          mergedItem,
          preserveExistingAssistantReply: true,
        );
      }
      return;
    }
    _timeline.add(item);
    if (emitNotifications) {
      _emitTimelineNotification(item);
    }
  }

  bool _isDuplicateUserTimelineItem(TimelineItem item) {
    if (item.kind != 'user') {
      return false;
    }
    final body = item.body.trim();
    if (body.isEmpty) {
      return false;
    }
    for (final previous in _timeline.reversed.take(6)) {
      if (previous.kind == 'user' && previous.body.trim() == body) {
        final gap = item.timestamp.difference(previous.timestamp).abs();
        if (gap.inSeconds <= 12) {
          return true;
        }
      }
    }
    return false;
  }

  bool _isDuplicateAssistantTimelineItem(TimelineItem item) {
    if (item.kind != 'markdown' && item.kind != 'session') {
      return false;
    }
    final body = item.body.trim();
    if (body.isEmpty) {
      return false;
    }
    // 3 秒内出现相同内容的 assistant 回复或 session 消息视为重复
    for (final previous in _timeline.reversed.take(10)) {
      if (previous.body.trim() == body) {
        final gap = item.timestamp.difference(previous.timestamp).abs();
        if (gap.inSeconds <= 3) {
          return true;
        }
      }
    }
    return false;
  }

  String _mergedTimelineKind(TimelineItem previous, TimelineItem next) {
    if (_isAssistantReplyTimelineItem(previous) &&
        _isAssistantReplyTimelineItem(next)) {
      return 'markdown';
    }
    if (previous.kind == next.kind) {
      return previous.kind;
    }
    return next.kind.isNotEmpty ? next.kind : previous.kind;
  }

  bool _isAssistantReplyTimelineItem(TimelineItem item) {
    final body = item.body.trim();
    if (body.isEmpty) {
      return false;
    }
    if (item.stream.trim().toLowerCase() == 'stderr') {
      return false;
    }
    if (item.kind == 'markdown') {
      return true;
    }
    return _shouldPreferAssistantText(item.meta, body);
  }

  bool _shouldMergeTimelineBodies(TimelineItem previous, TimelineItem item) {
    if (previous.stream != item.stream) {
      return false;
    }
    final previousAssistant = _isAssistantReplyTimelineItem(previous);
    final nextAssistant = _isAssistantReplyTimelineItem(item);
    if (previous.kind == 'markdown' && item.kind == 'markdown') {
      return true;
    }
    return previousAssistant && nextAssistant;
  }

  bool _hasSameTimelineSource(TimelineItem previous, TimelineItem item) {
    final sameExecution =
        previous.meta.executionId.trim() == item.meta.executionId.trim();
    final sameContext =
        previous.meta.contextId.trim() == item.meta.contextId.trim();
    final sameCommand = previous.meta.command.trim().toLowerCase() ==
            item.meta.command.trim().toLowerCase() &&
        previous.meta.engine.trim().toLowerCase() ==
            item.meta.engine.trim().toLowerCase();
    return sameExecution || sameContext || sameCommand;
  }

  bool _isMergeGapAcceptable(TimelineItem previous, TimelineItem item) {
    final gap = item.timestamp.difference(previous.timestamp).inMilliseconds;
    return gap >= 0 && gap <= 5000;
  }

  bool _shouldMergeIntoPreviousTimelineItem(TimelineItem item) {
    if (_timeline.isEmpty) {
      return false;
    }
    final previous = _timeline.last;
    if (!_shouldMergeTimelineBodies(previous, item)) {
      return false;
    }
    if (previous.title.isNotEmpty || item.title.isNotEmpty) {
      return false;
    }
    if (!_hasSameTimelineSource(previous, item)) {
      return false;
    }
    return _isMergeGapAcceptable(previous, item);
  }

  String _mergeTimelineBodies(String previous, String next) {
    if (previous.isEmpty) {
      return next;
    }
    if (next.isEmpty) {
      return previous;
    }
    if (_continuesMarkdownLink(previous, next)) {
      return '$previous$next';
    }
    if (_endsWithWhitespace(previous) || _startsWithWhitespace(next)) {
      return '$previous$next';
    }
    if (_startsWithBlockLikeMarkdown(next)) {
      return previous.endsWith('\n') ? '$previous$next' : '$previous\n$next';
    }
    if (!_startsWithClosingPunctuation(next) &&
        !_boundaryHasCjk(previous, next)) {
      return '$previous $next';
    }
    return '$previous$next';
  }

  bool _continuesMarkdownLink(String previous, String next) {
    final previousTrimmed = previous.trimRight();
    final nextTrimmed = next.trimLeft();
    return previousTrimmed.endsWith(']') && nextTrimmed.startsWith('(');
  }

  bool _endsWithWhitespace(String value) => RegExp(r'\s$').hasMatch(value);

  bool _startsWithWhitespace(String value) => RegExp(r'^\s').hasMatch(value);

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
      case 'terminal':
      case 'log':
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
    final mergedMeta = currentMeta.merge(log.runtimeMeta);
    final message = _sanitizeTimelineLogMessage(
      _sanitizeAiBootstrapLogMessage(log.message, mergedMeta),
    );
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
      meta: mergedMeta,
    );
    if (kind == null) {
      return;
    }
    if (kind == 'markdown') {
      final executionKey = _runtimeExecutionKey(mergedMeta);
      _lastAssistantReplyExecutionKey = executionKey;
      _markTerminalExecutionFinished(
        mergedMeta,
        finishedAt: log.timestamp,
      );
      _syncDerivedState();
      _syncObservedSessionPolling();
    }
    _pushTimelineItem(
      TimelineItem(
        id: 'log-${log.timestamp.microsecondsSinceEpoch}',
        kind: kind,
        timestamp: log.timestamp,
        body: message,
        stream: log.stream,
        meta: mergedMeta,
      ),
    );
  }

  String _sanitizeAiBootstrapLogMessage(String message, RuntimeMeta meta) {
    if (_isBootstrapSource(meta.source)) {
      return '';
    }
    return _sanitizeAiBootstrapReply(
      message,
      _timelineAiEngine(meta),
    );
  }

  String _sanitizeTimelineLogMessage(String message) {
    var normalized = message.replaceAll('\r\n', '\n').replaceAll('\r', '\n');
    normalized = normalized.trim();
    if (normalized.isEmpty) {
      return '';
    }
    while (normalized.isNotEmpty) {
      final lower = normalized.toLowerCase();
      if (lower.startsWith('wall time:')) {
        final newlineIndex = normalized.indexOf('\n');
        if (newlineIndex == -1) {
          return '';
        }
        normalized = normalized.substring(newlineIndex + 1).trimLeft();
        continue;
      }
      final stripped = _stripLeadingOutputHeader(normalized);
      if (stripped != normalized) {
        normalized = stripped.trimLeft();
        continue;
      }
      break;
    }
    return normalized.trim();
  }

  String _stripLeadingOutputHeader(String message) {
    final lower = message.toLowerCase();
    if (lower == 'output' || lower == 'output:') {
      return '';
    }
    const outputPrefixes = <String>[
      'output:\n',
      'output\n',
      'output: ',
      'output ',
    ];
    for (final prefix in outputPrefixes) {
      if (lower.startsWith(prefix)) {
        return message.substring(prefix.length);
      }
    }
    return message;
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
      return null;
    }
    if (_looksLikeProcessNoise(trimmed)) {
      return null;
    }
    if (_shouldPreferAssistantText(meta, trimmed)) {
      return 'markdown';
    }
    if (_looksLikeTerminalOutput(trimmed) || message.startsWith('\r')) {
      return null;
    }
    if (_looksLikeAssistantReply(trimmed)) {
      return 'markdown';
    }
    return null;
  }

  bool _shouldPreferAssistantText(RuntimeMeta meta, String message) {
    if (_isBootstrapSource(meta.source)) {
      return false;
    }
    final normalizedEngine = _timelineAiEngine(meta);
    if (normalizedEngine != 'claude' && normalizedEngine != 'codex') {
      return false;
    }
    if (message.isEmpty) {
      return false;
    }
    if (_looksLikeFrontendToolResultNoise(message) ||
        _looksLikeHardTerminalOutput(message) ||
        _looksLikeProcessNoise(message)) {
      return false;
    }
    return _looksLikeAssistantReplyAllowingSoftTerminal(message);
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

  bool _isBootstrapSource(String source) {
    return source.trim().toLowerCase() == 'system/bootstrap';
  }

  bool _shouldSuppressIntentionalHandoffNoise(String message) {
    final trimmed = message.trim().toLowerCase();
    if (trimmed.isEmpty) {
      return false;
    }
    final runtimeMessage = _runtimePhase?.message.trim().toLowerCase() ?? '';
    final temporaryHandoff =
        _normalizeDisplayPermissionMode(_runtimePermissionMode) == 'auto' ||
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
    if (_looksLikeHardTerminalOutput(message)) {
      return false;
    }
    return _looksLikeAssistantReplyAllowingSoftTerminal(message);
  }

  bool _looksLikeAssistantReplyAllowingSoftTerminal(String message) {
    final normalized = message.trim();
    if (normalized.length >= 24 && !normalized.contains(RegExp(r'\s{2,}'))) {
      return true;
    }
    if (normalized.contains('\n')) {
      return true;
    }
    if (RegExp(r'[。！？；：]|\.\s+[A-Z]|,\s+\w').hasMatch(normalized)) {
      return true;
    }
    return _looksLikeShortAssistantReply(normalized);
  }

  bool _looksLikeHardTerminalOutput(String message) {
    final trimmed = message.trim();
    if (trimmed.isEmpty) {
      return false;
    }
    final lower = trimmed.toLowerCase();
    if (lower.startsWith(r'') || trimmed.contains('[')) {
      return true;
    }
    if (RegExp(
      r'^\d{4}[/-]\d{2}[/-]\d{2}[ T]\d{2}:\d{2}:\d{2}(?:\.\d+)?\s+\[(TRACE|DEBUG|INFO|WARN|ERROR)\]',
      multiLine: true,
    ).hasMatch(trimmed)) {
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

  bool _looksLikeShortAssistantReply(String message) {
    final normalized = message.trim();
    if (normalized.length < 2 || normalized.length >= 24) {
      return false;
    }
    if (_looksLikePassiveWaitingText(normalized) ||
        _looksLikeProcessNoise(normalized) ||
        _looksLikeTerminalOutput(normalized)) {
      return false;
    }
    if (RegExp(r'^[\$#>]\s*').hasMatch(normalized) ||
        normalized.contains('/') ||
        normalized.contains('\\') ||
        normalized.contains('=') ||
        normalized.contains(':') ||
        normalized.startsWith('{') ||
        normalized.startsWith('[') ||
        normalized.endsWith('}') ||
        normalized.endsWith(']')) {
      return false;
    }
    return RegExp(r'\p{Script=Han}|[A-Za-z]', unicode: true)
        .hasMatch(normalized);
  }

  bool _looksLikePassiveWaitingText(String message) {
    final lower = message.trim().toLowerCase();
    if (lower.isEmpty) {
      return false;
    }
    return lower == '等待输入' ||
        lower == '等待继续输入' ||
        lower == '请继续输入' ||
        lower == '等待中' ||
        lower == 'ready' ||
        lower == 'waiting for input' ||
        lower == 'continue input' ||
        lower == 'awaiting input';
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
    if (_looksLikeHardTerminalOutput(trimmed)) {
      return true;
    }
    if (trimmed.contains(RegExp(r'^\S+\s*[:=]\s*\S+$', multiLine: true)) &&
        !trimmed.contains('。')) {
      return true;
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
    if (lower.contains('.gitmodules') &&
        (lower.contains('no such file or directory') ||
            lower.contains('no submodule mapping found'))) {
      return true;
    }
    if (lower.startsWith('wall time:') && !lower.contains('\n')) {
      return true;
    }
    if (lower == 'output' || lower == 'output:') {
      return true;
    }
    if (lower.startsWith('output fatal: not a git repository')) {
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
        looksLikeSessionBootstrapCommand(trimmed) ||
        looksLikeSessionPlaceholderTitle(trimmed)) {
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

  HistoryContext? _reviewActionTargetDiff() {
    final current = currentReviewDiff;
    if (current != null) {
      return current;
    }
    final openedPending = openedFilePendingDiff;
    if (openedPending != null) {
      return openedPending;
    }
    final nextPending = nextPendingDiff;
    if (nextPending != null) {
      return nextPending;
    }
    final diff = currentDiffContext;
    if (diff?.pendingReview == true && diff!.diff.isNotEmpty) {
      return diff;
    }
    return null;
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

  static const Map<String, String> _toolVerbs = {
    'read': '正在读取',
    'write': '正在写入',
    'edit': '正在修改',
    'bash': '正在执行命令',
    'grep': '正在搜索',
    'glob': '正在查找文件',
    'taskcreate': '正在派发子任务',
    'taskupdate': '正在更新任务',
    'tasklist': '正在整理任务',
    'taskget': '正在查看任务',
    'webfetch': '正在抓取网页',
    'websearch': '正在联网搜索',
    'agent': '正在派发子代理',
    'skill': '正在调用 skill',
    'lsp': '正在查询 LSP',
    'notebookedit': '正在编辑 notebook',
  };

  String _verbFromTool(String tool) {
    final trimmed = tool.trim();
    if (trimmed.isEmpty) {
      return '';
    }
    final key = trimmed.toLowerCase().replaceAll(RegExp(r'[^a-z]'), '');
    return _toolVerbs[key] ?? '';
  }

  String _detailFromAgentState() {
    final agent = _agentState;
    if (agent == null) {
      return '';
    }
    final step = agent.step.trim();
    if (step.isNotEmpty) {
      return step;
    }
    final verb = _verbFromTool(agent.tool);
    final target = _toolLabelFromPath(agent.runtimeMeta.targetPath);
    final commandHead = _toolLabelFromCommand(agent.command);
    final detail = target.isNotEmpty
        ? target
        : commandHead.isNotEmpty
            ? commandHead
            : '';
    if (verb.isNotEmpty && detail.isNotEmpty) {
      return '$verb · $detail';
    }
    if (verb.isNotEmpty) {
      return verb;
    }
    if (detail.isNotEmpty) {
      return detail;
    }
    return '';
  }

  String _summaryFromHistoryContext(HistoryContext? context) {
    if (context == null) {
      return '';
    }
    return context.message.isNotEmpty ? context.message : context.title;
  }

  bool _shouldPreserveBlockingPrompt() {
    return _pendingInteraction?.isPermission == true ||
        _pendingInteraction?.isReview == true ||
        shouldShowReviewChoices ||
        _pendingInteraction?.isPlan == true ||
        hasPendingPlanQuestions ||
        _pendingPrompt != null;
  }

  bool _shouldKeepExistingBlockingPrompt(
    PromptRequestEvent incoming,
    InteractionRequestEvent? currentInteraction,
    PromptRequestEvent? currentPrompt,
  ) {
    if (currentInteraction?.isPermission == true ||
        currentPrompt?.isPermission == true) {
      if (!incoming.isPermission) {
        return true;
      }
      final currentID = (currentInteraction?.runtimeMeta.permissionRequestId ??
              currentPrompt?.runtimeMeta.permissionRequestId ??
              '')
          .trim();
      final incomingID = incoming.runtimeMeta.permissionRequestId.trim();
      return currentID.isNotEmpty &&
          incomingID.isNotEmpty &&
          currentID == incomingID;
    }
    if (incoming.isReady) {
      return currentInteraction?.isPermission == true ||
          currentInteraction?.isReview == true ||
          currentInteraction?.isPlan == true ||
          currentPrompt?.isReview == true ||
          hasPendingPlanQuestions;
    }
    if (incoming.message.trim().isEmpty) {
      return currentInteraction?.isPermission == true ||
          currentInteraction?.isReview == true ||
          currentInteraction?.isPlan == true ||
          currentPrompt != null;
    }
    return false;
  }

  void _syncRuntimePermissionMode() {
    final interactionMode =
        _pendingInteraction?.runtimeMeta.permissionMode.trim() ?? '';
    if (interactionMode.isNotEmpty) {
      _runtimePermissionMode = _normalizeDisplayPermissionMode(interactionMode);
      return;
    }
    final promptMode = _pendingPrompt?.runtimeMeta.permissionMode.trim() ?? '';
    if (promptMode.isNotEmpty) {
      _runtimePermissionMode = _normalizeDisplayPermissionMode(promptMode);
      return;
    }
    // _agentState 是 set_permission_mode 的官方回包来源，优先于 _sessionState 缓存，
    // 避免 SessionStateEvent 携带的滞后 mode 把刚切换的值压回去。
    final agentMode = _agentState?.runtimeMeta.permissionMode.trim() ?? '';
    if (agentMode.isNotEmpty) {
      _runtimePermissionMode = _normalizeDisplayPermissionMode(agentMode);
      return;
    }
    final sessionMode = _sessionState?.runtimeMeta.permissionMode.trim() ?? '';
    if (sessionMode.isNotEmpty) {
      _runtimePermissionMode = _normalizeDisplayPermissionMode(sessionMode);
      return;
    }
    final resumeMode = _resumeRuntimeMeta.permissionMode.trim();
    if (resumeMode.isNotEmpty) {
      _runtimePermissionMode = _normalizeDisplayPermissionMode(resumeMode);
      return;
    }
    _runtimePermissionMode = '';
  }

  void _syncDerivedState() {
    _syncRuntimePermissionMode();
    _syncReviewGroupsFromRecentDiffs();
    _agentPhaseLabel = _compactAgentMessage();
    _syncActiveReviewSelection();
    final agentState = (_agentState?.state ?? '').trim().toUpperCase();
    final sessionState = (_sessionState?.state ?? '').trim().toUpperCase();
    final hasPendingPermission = hasPendingPermissionPrompt;
    final currentExecutionKey = _runtimeExecutionKey(
      _agentState?.runtimeMeta ??
          _sessionState?.runtimeMeta ??
          const RuntimeMeta(),
    );
    final assistantReplySettled = currentExecutionKey.isNotEmpty &&
        currentExecutionKey == _lastAssistantReplyExecutionKey;
    final hasBlockingPrompt = awaitInput ||
        hasPendingPermission ||
        shouldShowReviewChoices ||
        hasPendingPlanQuestions;
    final hasRealRunningSignal = _executionActive ||
        _sessionRuntimeAlive ||
        agentState == 'THINKING' ||
        agentState == 'RECOVERING' ||
        sessionState == 'THINKING' ||
        sessionState == 'RUNNING' ||
        agentState == 'RUNNING_TOOL' ||
        sessionState == 'RUNNING_TOOL';
    final isDefinitiveAgentState =
        _isDefinitiveAgentState(agentState, sessionState);
    // _activityVisible（banner）保留原有由后端运行信号驱动的语义，不被 _isSubmitting 强制点亮。
    // 状态球（_aiStatusIndicatorVisible）在 _beginUserSubmission 中已直接点亮，与 banner 解耦。
    final active = _connected &&
        !hasBlockingPrompt &&
        !_isClaudePendingReadyForInput &&
        hasRealRunningSignal &&
        (!assistantReplySettled || isDefinitiveAgentState || _executionActive);
    // 何时立即隐藏（不延迟）：焦点已被切走或会话被打断时——权限/审核/计划/await/未连接/Claude 待输入。
    // 何时平滑消隐：仅在"自然结算"路径上（turn 结束 → 无新 active 信号 & 也无阻塞），避免后端瞬态打灭。
    final hideImmediately = hasPendingPermission ||
        hasBlockingPrompt ||
        _isClaudePendingReadyForInput ||
        !_connected;
    if (active) {
      // 一旦本轮重新激活，立即取消任何延迟消隐计时器，保证状态球瞬间点亮。
      _activityHideDebounce?.cancel();
      _activityHideDebounce = null;
      _activityVisible = true;
      _activityStartedAt ??= _agentState?.timestamp ?? DateTime.now();
      if (_activityToolLabel.isEmpty) {
        _activityToolLabel = _currentStep?.tool.isNotEmpty == true
            ? _currentStep!.tool
            : _agentState?.tool.isNotEmpty == true
                ? _agentState!.tool
                : _toolLabelFromCommand(_agentState?.command ?? '');
      }
    } else if (hideImmediately) {
      _activityHideDebounce?.cancel();
      _activityHideDebounce = null;
      _activityVisible = false;
      _activityStartedAt = null;
      _activityToolLabel = '';
    } else {
      // 平滑消隐：仅在自然结算路径上启动一次 600ms 延迟 timer，避免后端瞬间状态跳变造成视觉闪烁。
      // 期间若 active 重新变 true（新一轮开始），上面分支会 cancel 掉 timer。
      if (_activityVisible) {
        _activityHideDebounce ??= Timer(
          const Duration(milliseconds: 600),
          () {
            _activityHideDebounce = null;
            if (_activityVisible) {
              _activityVisible = false;
              _activityStartedAt = null;
              _activityToolLabel = '';
              notifyListeners();
            }
          },
        );
      } else {
        _activityHideDebounce?.cancel();
        _activityHideDebounce = null;
        _activityStartedAt = null;
        _activityToolLabel = '';
      }
    }
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
    if (current != null &&
        current.key == snapshot.key &&
        current.revision == snapshot.revision) {
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
        revision: _actionNeededRevisionToken(
          interaction?.timestamp,
          prompt?.timestamp,
          _agentState?.timestamp,
          _sessionState?.timestamp,
        ),
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
        revision: _actionNeededRevisionToken(
          interaction?.timestamp,
          prompt?.timestamp,
          _agentState?.timestamp,
          _sessionState?.timestamp,
        ),
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
        revision: _actionNeededRevisionToken(
          interaction?.timestamp,
          prompt?.timestamp,
          _agentState?.timestamp,
          _sessionState?.timestamp,
        ),
      );
    }
    if (prompt?.isReady == true) {
      final identity = prompt!.runtimeMeta.contextId.isNotEmpty
          ? prompt.runtimeMeta.contextId
          : prompt.runtimeMeta.targetPath.isNotEmpty
              ? prompt.runtimeMeta.targetPath
              : _selectedSessionId;
      return _ActionNeededSnapshot(
        type: ActionNeededType.continueInput,
        key: 'continue::$identity',
        message: 'AI 助手需要你继续输入',
        revision: _actionNeededRevisionToken(
          prompt.timestamp,
          _agentState?.timestamp,
          _sessionState?.timestamp,
          null,
        ),
      );
    }
    final hasGenericPrompt = interaction != null ||
        (prompt != null &&
            prompt.hasVisiblePrompt &&
            !prompt.isPermission &&
            !prompt.isReview &&
            !prompt.isPlan &&
            !prompt.isReady);
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
        revision: _actionNeededRevisionToken(
          interaction?.timestamp,
          prompt?.timestamp,
          _agentState?.timestamp,
          _sessionState?.timestamp,
        ),
      );
    }
    final state = (_agentState?.state ?? '').trim().toUpperCase();
    final blockingKind =
        _agentState?.runtimeMeta.blockingKind.trim().toLowerCase() ?? '';
    if (state == 'WAIT_INPUT' && awaitInput && blockingKind == 'ready') {
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
        revision: _actionNeededRevisionToken(
          prompt?.timestamp,
          _agentState?.timestamp,
          _sessionState?.timestamp,
          null,
        ),
      );
    }
    return null;
  }

  String _actionNeededRevisionToken(
      DateTime? a, DateTime? b, DateTime? c, DateTime? d) {
    final values = <DateTime?>[a, b, c, d]
        .where((item) => item != null)
        .cast<DateTime>()
        .toList();
    if (values.isEmpty) {
      return '';
    }
    values.sort((left, right) => left.compareTo(right));
    return values.last.microsecondsSinceEpoch.toString();
  }

  void _markActionNeededHandled() {
    _activeActionNeededSnapshot = null;
  }

  void _resetActionNeededTracking({bool suppressNextSignal = false}) {
    _actionNeededSignal = null;
    _activeActionNeededSnapshot = null;
    _shouldSuppressNextActionNeededSignal = suppressNextSignal;
  }

  bool _handleTaskSnapshot(TaskSnapshotEvent snapshot) {
    final sessionId = snapshot.sessionId.trim();
    if (sessionId.isNotEmpty && sessionId != _selectedSessionId.trim()) {
      return true;
    }
    if (sessionId.isNotEmpty && snapshot.latestCursor > 0) {
      final previous = _sessionEventCursors[sessionId] ?? 0;
      if (snapshot.latestCursor > previous) {
        _sessionEventCursors[sessionId] = snapshot.latestCursor;
      }
    }
    final state =
        snapshot.state.trim().isEmpty ? 'IDLE' : snapshot.state.trim();
    if (!snapshot.runtimeAlive && _isIdleLikeState(state)) {
      // Heartbeat snapshots use the backend runner state, which doesn't
      // track external native (desktop Claude) processes. Keep the state
      // from the authoritative session events for external sessions.
      if (!snapshot.syncing && _selectedSessionExternalNative) {
        return false;
      }
      _sessionRuntimeAlive = false;
      _syncDerivedState();
      notifyListeners();
      return false;
    }
    final meta = snapshot.runtimeMeta.merge(
      RuntimeMeta(
        command: snapshot.command,
        cwd: snapshot.runtimeMeta.cwd.trim().isNotEmpty
            ? snapshot.runtimeMeta.cwd
            : effectiveCwd,
      ),
    );
    _agentState = AgentStateEvent(
      timestamp: snapshot.timestamp,
      sessionId: snapshot.sessionId,
      runtimeMeta: meta,
      raw: snapshot.raw,
      state: state,
      message: snapshot.message,
      awaitInput: snapshot.awaitInput,
      command: snapshot.command,
      step: snapshot.step,
      tool: snapshot.tool,
    );
    if (snapshot.runtimeAlive) {
      _connectionStage = snapshot.syncing
          ? SessionConnectionStage.catchingUp
          : SessionConnectionStage.ready;
      if (!hasPendingPermissionPrompt) {
        _connectionMessage =
            snapshot.syncing ? 'Syncing latest progress...' : 'Session is live';
      }
    } else if (_connected &&
        _connectionStage != SessionConnectionStage.catchingUp) {
      _connectionStage = SessionConnectionStage.ready;
      if (!hasPendingPermissionPrompt) {
        _connectionMessage =
            snapshot.message.isNotEmpty ? snapshot.message : 'Session ready';
      }
    }
    if (!_isHeartbeatIdleSnapshot(snapshot, state)) {
      _syncStepSummary(
        message: snapshot.step.isNotEmpty ? snapshot.step : snapshot.message,
        status: state,
        tool: snapshot.tool,
        command: snapshot.command,
        targetPath: snapshot.runtimeMeta.targetPath,
      );
    }
    _syncDerivedState();
    notifyListeners();
    return false;
  }

  bool _isHeartbeatIdleSnapshot(TaskSnapshotEvent snapshot, String state) {
    final normalizedState = state.trim().toLowerCase();
    final message = snapshot.message.trim().toLowerCase();
    final step = snapshot.step.trim().toLowerCase();
    return !snapshot.runtimeAlive &&
        normalizedState == 'idle' &&
        (message.contains('heartbeat') || step.contains('heartbeat'));
  }

  void _startConnectionHealthMonitor() {
    _connectionHealthTimer?.cancel();
    _connectionHealthTimer = Timer.periodic(_connectionHealthInterval, (_) {
      if (!_connected || _connecting || !_autoReconnectEnabled) {
        return;
      }
      final lastSeen = _lastServerEventAt;
      if (lastSeen != null &&
          DateTime.now().difference(lastSeen) > _connectionSilenceTimeout) {
        unawaited(_service.disconnect());
        _handleUnexpectedSocketDisconnect(
            'Connection timed out, reconnecting...');
        return;
      }
      _service.send({
        'action': 'ping',
        'sessionId': _selectedSessionId.trim(),
        'ts': DateTime.now().millisecondsSinceEpoch,
      });
    });
  }

  void _trackSessionEventCursor(AppEvent event) {
    final sessionId = event.sessionId.trim();
    if (sessionId.isEmpty) {
      return;
    }
    final rawCursor = event.raw['eventCursor'];
    final cursor = switch (rawCursor) {
      int value => value,
      num value => value.toInt(),
      String value => int.tryParse(value) ?? 0,
      _ => 0,
    };
    if (cursor <= 0) {
      return;
    }
    final previous = _sessionEventCursors[sessionId] ?? 0;
    if (cursor > previous) {
      _sessionEventCursors[sessionId] = cursor;
      // 同步更新 delta known 的游标，避免轮询重复拉取已收内容
      final known = _sessionDeltaKnown[sessionId];
      if (known != null && cursor > known.eventCursor) {
        _sessionDeltaKnown[sessionId] = SessionDeltaKnown(
          eventCursor: cursor,
          logEntryCount: known.logEntryCount,
          diffCount: known.diffCount,
          terminalExecutionCount: known.terminalExecutionCount,
          terminalStdoutLength: known.terminalStdoutLength,
          terminalStderrLength: known.terminalStderrLength,
        );
      }
    }
  }

  void _emitResumeNotification(SessionResumeNoticeEvent notice) {
    final body = notice.message.trim();
    if (body.isEmpty) {
      return;
    }
    final type = switch (notice.noticeType.trim().toLowerCase()) {
      'assistant_reply' => AppNotificationType.assistantReply,
      'error' => AppNotificationType.error,
      _ => null,
    };
    if (type == null) {
      return;
    }
    _notificationSignal = AppNotificationSignal(
      id: ++_nextNotificationSignalId,
      type: type,
      title: notice.title.trim().isNotEmpty ? notice.title.trim() : 'MobileVC',
      body: _notificationPreview(body),
      createdAt: DateTime.now(),
    );
  }

  void _emitTimelineNotification(
    TimelineItem item, {
    bool preserveExistingAssistantReply = false,
  }) {
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
    if (preserveExistingAssistantReply &&
        type == AppNotificationType.assistantReply &&
        _notificationSignal?.type == AppNotificationType.assistantReply) {
      final current = _notificationSignal!;
      _notificationSignal = AppNotificationSignal(
        id: current.id,
        type: current.type,
        title: current.title,
        body: _notificationPreview(body),
        createdAt: current.createdAt,
      );
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
    // 后端会自动检查并应用权限规则，前端不需要预判
    // 如果后端自动应用了，会收到 PermissionAutoAppliedEvent 并清空 _pendingPrompt
    return false;
  }

  /// 检查是否会自动应用权限规则（不实际发送决策）
  String _compactAgentMessage() {
    switch (_connectionStage) {
      case SessionConnectionStage.backgroundSuspended:
        return '后台已暂停';
      case SessionConnectionStage.reconnecting:
        return '恢复连接中';
      case SessionConnectionStage.catchingUp:
        return '恢复会话中';
      case SessionConnectionStage.failed:
        return '恢复失败';
      case SessionConnectionStage.connecting:
        return '连接中';
      case SessionConnectionStage.disconnected:
        return '未连接';
      case SessionConnectionStage.connected:
      case SessionConnectionStage.ready:
        break;
    }
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
    if (state == 'RECOVERING') {
      return '恢复中';
    }
    if (state == 'RUNNING_TOOL') {
      return '执行中';
    }
    if (state == 'THINKING') {
      return '思考中';
    }
    return '已连接';
  }

  void _appendTerminalLog(
    String stream,
    String message, {
    String executionId = '',
    DateTime? timestamp,
    RuntimeMeta meta = const RuntimeMeta(),
  }) {
    if (message.isEmpty) {
      return;
    }
    final normalizedStream = stream.trim().toLowerCase();
    if (!_shouldCaptureTerminalLog(
      normalizedStream,
      message,
      meta: meta,
    )) {
      return;
    }
    if (normalizedStream == 'stderr') {
      _terminalStderr = _appendChunk(_terminalStderr, message);
    } else {
      _terminalStdout = _appendChunk(_terminalStdout, message);
    }
    _appendExecutionOutput(
      executionId,
      normalizedStream,
      message,
      timestamp: timestamp,
    );
  }

  void _mergeTerminalExecutions(List<TerminalExecution> incoming) {
    for (final next in incoming) {
      final normalizedId = next.executionId.trim();
      if (normalizedId.isEmpty) {
        continue;
      }
      final index = _terminalExecutions
          .indexWhere((item) => item.executionId == normalizedId);
      if (index == -1) {
        _terminalExecutions.add(next);
      } else {
        _terminalExecutions[index] = next;
      }
    }
    _syncActiveTerminalExecution();
  }

  void _appendTerminalLogs(Map<String, String> rawTerminalByStream) {
    final stdout = rawTerminalByStream['stdout'] ?? '';
    final stderr = rawTerminalByStream['stderr'] ?? '';
    if (stdout.isNotEmpty) {
      _terminalStdout += stdout;
    }
    if (stderr.isNotEmpty) {
      _terminalStderr += stderr;
    }
  }

  void _restoreTerminalLogs(Map<String, String> rawTerminalByStream) {
    _terminalStdout = rawTerminalByStream['stdout'] ?? '';
    _terminalStderr = rawTerminalByStream['stderr'] ?? '';
    _syncActiveTerminalExecution();
  }

  void _appendExecutionOutput(
    String executionId,
    String stream,
    String message, {
    DateTime? timestamp,
  }) {
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
      startedAt: current.startedAt ?? timestamp ?? DateTime.now(),
      completedAt: null,
      running: true,
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

  bool get _hasRunningTerminalExecution =>
      _terminalExecutions.any((item) => item.running);

  String _runtimeExecutionKey(RuntimeMeta meta) {
    final executionId = meta.executionId.trim();
    if (executionId.isNotEmpty) {
      return 'execution:$executionId';
    }
    final contextId = meta.contextId.trim();
    if (contextId.isNotEmpty) {
      return 'context:$contextId';
    }
    final command = meta.command.trim().toLowerCase();
    if (command.isNotEmpty) {
      return 'command:$command';
    }
    return '';
  }

  void _markTerminalExecutionFinished(
    RuntimeMeta meta, {
    DateTime? finishedAt,
  }) {
    if (_terminalExecutions.isEmpty) {
      return;
    }
    final normalizedId = meta.executionId.trim();
    final completedAt = finishedAt ?? DateTime.now();
    var updatedAny = false;
    for (var i = 0; i < _terminalExecutions.length; i++) {
      final item = _terminalExecutions[i];
      final matches = normalizedId.isNotEmpty
          ? item.executionId == normalizedId
          : item.running;
      if (!matches || !item.running) {
        continue;
      }
      _terminalExecutions[i] = TerminalExecution(
        executionId: item.executionId,
        command: item.command,
        cwd: item.cwd,
        source: item.source,
        sourceLabel: item.sourceLabel,
        contextId: item.contextId,
        contextTitle: item.contextTitle,
        groupId: item.groupId,
        groupTitle: item.groupTitle,
        startedAt: item.startedAt,
        completedAt: completedAt,
        running: false,
        exitCode: item.exitCode,
        stdout: item.stdout,
        stderr: item.stderr,
      );
      updatedAny = true;
    }
    if (updatedAny) {
      _syncActiveTerminalExecution();
    }
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

  bool _shouldCaptureTerminalLog(
    String stream,
    String message, {
    RuntimeMeta meta = const RuntimeMeta(),
  }) {
    final trimmed = message.trim();
    if (trimmed.isEmpty) {
      return false;
    }
    if (_isBootstrapSource(meta.source)) {
      return false;
    }
    if (_looksLikeFrontendToolResultNoise(trimmed) ||
        _shouldFilterTimelineText(trimmed)) {
      return false;
    }
    if (stream != 'stderr' &&
        _timelineKindForLog(message, stream, meta: meta) == 'markdown') {
      return false;
    }
    if (!_looksLikeTerminalOutput(trimmed) &&
        _looksLikeProcessNoise(trimmed) &&
        !message.startsWith('\r')) {
      return false;
    }
    return true;
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
      case 'bypassPermissions':
        return '跳过权限确认';
      case 'default':
        return '手动审核';
      case 'auto':
      case 'acceptEdits':
      default:
        return '自动模式';
    }
  }

  String _normalizeDisplayPermissionMode(String permissionMode) {
    switch (permissionMode.trim()) {
      case 'bypassPermissions':
        return 'bypassPermissions';
      case 'default':
        return 'default';
      default:
        return 'auto';
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
    if (engine != 'claude' && engine != 'codex') {
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

  CodexModelCatalogEntry? _findCodexModelCatalogEntry(String model) {
    final normalized = _normalizeCodexModel(model);
    if (normalized.isEmpty) {
      return null;
    }
    for (final entry in _codexModelCatalog) {
      if (_normalizeCodexModel(entry.model) == normalized) {
        return entry;
      }
    }
    return null;
  }

  String _codexModelDisplayLabel(String model) {
    final entry = _findCodexModelCatalogEntry(model);
    final displayName = entry?.displayName.trim() ?? '';
    if (displayName.isNotEmpty) {
      return displayName;
    }
    return _codexModelLabel(model);
  }

  String _displayAiModelSummary(
    String engine,
    String model,
    String reasoningEffort,
  ) {
    switch (engine) {
      case 'codex':
        final label =
            model.trim().isEmpty ? 'Default' : _codexModelDisplayLabel(model);
        return '$label · ${_resolvedAiReasoningEffort('codex', reasoningEffort).toUpperCase()}';
      case 'claude':
        return _claudeModelLabel(model);
      case 'gemini':
        return 'Gemini';
      default:
        return model.trim().isEmpty ? '模型' : model.trim();
    }
  }

  String _preferredCodexReasoningEffortForModel(
    String model, {
    String fallback = '',
  }) {
    final normalizedFallback = _resolvedAiReasoningEffort('codex', fallback);
    final entry = _findCodexModelCatalogEntry(model);
    if (entry == null) {
      return normalizedFallback;
    }
    final supported = entry.reasoningEffortOptions
        .map((option) => option.reasoningEffort.trim().toLowerCase())
        .where((effort) => effort.isNotEmpty)
        .toList();
    if (supported.contains(normalizedFallback)) {
      return normalizedFallback;
    }
    final defaultEffort = entry.defaultReasoningEffort.trim().toLowerCase();
    if (supported.contains(defaultEffort)) {
      return defaultEffort;
    }
    if (supported.isNotEmpty) {
      return supported.first;
    }
    return normalizedFallback;
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
  final effortMatch = RegExp(
    r'\b(xhigh|high|medium|low|minimal|none)\b',
    caseSensitive: false,
  ).firstMatch(normalized);
  if (effortMatch != null) {
    effort = (effortMatch.group(1) ?? '').toLowerCase();
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
      if (normalized.isEmpty || normalized.toLowerCase() == 'default') {
        return '';
      }
      final codexModel = _normalizeCodexModel(normalized);
      return codexModel;
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
  // 保留完整的模型名（如 moka/claude-sonnet-4-6），不强制转换
  if (normalized.contains('/')) {
    return normalized;
  }
  return 'default';
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
    case 'gpt-5.4':
      return 'GPT-5.4';
    case 'gpt-5-codex':
      return 'GPT-5-Codex';
    case 'gpt-5':
      return 'GPT-5';
    default:
      return value.trim().isEmpty ? 'Codex' : value.trim();
  }
}

const Set<String> _codexReasoningEffortOptions = <String>{
  'none',
  'minimal',
  'low',
  'medium',
  'high',
  'xhigh',
};
