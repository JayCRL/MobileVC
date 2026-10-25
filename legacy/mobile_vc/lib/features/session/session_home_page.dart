import 'dart:io';
import 'dart:ui';

import 'package:file_picker/file_picker.dart';
import 'package:flutter/foundation.dart';
import 'package:flutter/material.dart';
import 'package:flutter/rendering.dart';
import 'package:path_provider/path_provider.dart';
import 'package:share_plus/share_plus.dart';
import 'package:shared_preferences/shared_preferences.dart';

import '../../core/config/app_config.dart';
import '../../core/config/app_connection_endpoint.dart';
import '../../core/config/app_connection_environment.dart';
import '../../core/config/relay_config.dart';
import '../../core/relay_e2ee/relay_security_state.dart';
import '../../data/services/mobilevc_ws_service.dart';
import '../../data/models/events.dart';
import '../../data/models/session_models.dart';
import '../../features/adb/adb_debug_page.dart';
import '../../features/chat/chat_timeline.dart';
import '../../features/chat/command_input_bar.dart';
import '../../features/debug/debug_log_viewer.dart';
import '../../features/diff/diff_viewer_sheet.dart';
import '../../features/files/file_browser_sheet.dart';
import '../../features/files/file_viewer_sheet.dart';
import '../../features/memory/memory_management_sheet.dart';
import '../../features/permissions/permission_mode_options.dart';
import '../../features/permissions/permission_rule_management_sheet.dart';
import '../../features/runtime_info/runtime_info_sheet.dart';
import '../../features/skills/skill_management_sheet.dart';
import '../../features/status/status_detail_sheet.dart';
import '../../features/status/terminal_log_sheet.dart';
import '../../features/voice/voice_call_sheet.dart';
import 'context_window_usage_sheet.dart';
import 'claude_model_utils.dart';
import 'connection_scan_sheet.dart';
import 'session_controller.dart';
import 'session_list_sheet.dart';

class SessionHomePage extends StatefulWidget {
  const SessionHomePage({
    super.key,
    required this.controller,
    this.darkModeEnabled = false,
    this.onToggleTheme,
  });

  final SessionController controller;
  final bool darkModeEnabled;
  final VoidCallback? onToggleTheme;

  @override
  State<SessionHomePage> createState() => _SessionHomePageState();
}

class _SessionHomePageState extends State<SessionHomePage> {
  static const int _maxImageAttachmentBytes = 4 * 1024 * 1024;
  static const double _defaultTimelineBottomPadding = 128;
  static const double _commandBarGap = 12;

  final GlobalKey<ScaffoldState> _scaffoldKey = GlobalKey<ScaffoldState>();
  int _lastCompactFeedbackId = 0;
  static const String _tipCommandModePrefsKey = 'tip_command_mode_shown_v2';
  bool _lastConnected = false;
  bool _checkingTipShown = false;
  double _timelineBottomPadding = _defaultTimelineBottomPadding;

  SessionController get controller => widget.controller;

  @override
  void initState() {
    super.initState();
    controller.addListener(_handleControllerSignals);
  }

  @override
  void dispose() {
    controller.removeListener(_handleControllerSignals);
    super.dispose();
  }

  @override
  void didUpdateWidget(covariant SessionHomePage oldWidget) {
    super.didUpdateWidget(oldWidget);
    if (oldWidget.controller == widget.controller) {
      return;
    }
    oldWidget.controller.removeListener(_handleControllerSignals);
    widget.controller.addListener(_handleControllerSignals);
    _lastCompactFeedbackId = 0;
  }

  void _handleControllerSignals() {
    if (!mounted) {
      return;
    }
    final justConnected = !_lastConnected && controller.connected;
    _lastConnected = controller.connected;
    if (justConnected && !_checkingTipShown) {
      _checkingTipShown = true;
      _maybeShowCommandModeTip();
    }
    final signal = controller.compactFeedbackSignal;
    if (signal == null || signal.id == _lastCompactFeedbackId) {
      return;
    }
    _lastCompactFeedbackId = signal.id;
    final messenger = ScaffoldMessenger.of(context);
    final scheme = Theme.of(context).colorScheme;
    final isSuccess = signal.tone == CompactFeedbackTone.success;
    messenger
      ..hideCurrentSnackBar()
      ..showSnackBar(
        SnackBar(
          content: Row(
            children: [
              Icon(
                isSuccess ? Icons.check_circle_rounded : Icons.error_rounded,
                size: 18,
                color: Colors.white,
              ),
              const SizedBox(width: 10),
              Expanded(child: Text(signal.message)),
            ],
          ),
          backgroundColor: isSuccess ? const Color(0xFF0F766E) : scheme.error,
          duration: Duration(milliseconds: isSuccess ? 1600 : 3200),
        ),
      );
  }

  Future<void> _maybeShowCommandModeTip() async {
    try {
      final prefs = await SharedPreferences.getInstance();
      if (prefs.getBool(_tipCommandModePrefsKey) ?? false) {
        return;
      }
      if (!mounted) {
        return;
      }
      await showDialog<void>(
        context: context,
        builder: (ctx) => Dialog(
          child: ConstrainedBox(
            constraints: const BoxConstraints(maxWidth: 400, maxHeight: 560),
            child: Column(
              mainAxisSize: MainAxisSize.min,
              children: [
                Padding(
                  padding: const EdgeInsets.fromLTRB(24, 24, 24, 0),
                  child: Row(
                    children: [
                      Icon(Icons.check_circle_rounded,
                          color: Theme.of(ctx).colorScheme.primary, size: 22),
                      const SizedBox(width: 10),
                      const Expanded(
                        child: Text('连接成功',
                            style: TextStyle(
                                fontSize: 17, fontWeight: FontWeight.w700)),
                      ),
                    ],
                  ),
                ),
                Flexible(
                  child: SingleChildScrollView(
                    padding: const EdgeInsets.fromLTRB(24, 12, 24, 4),
                    child: _buildTipContent(),
                  ),
                ),
                Padding(
                  padding:
                      const EdgeInsets.symmetric(horizontal: 16, vertical: 8),
                  child: Align(
                    alignment: Alignment.centerRight,
                    child: TextButton(
                      onPressed: () => Navigator.of(ctx).pop(),
                      child: const Text('知道了'),
                    ),
                  ),
                ),
              ],
            ),
          ),
        ),
      );
      await prefs.setBool(_tipCommandModePrefsKey, true);
    } finally {
      _checkingTipShown = false;
    }
  }

  static Widget _buildTipContent() {
    return const _TipSection(children: [
      _TipItem(
        number: '一',
        title: '命令行模式切换',
        body:
            '默认是命令行模式（Shell）。如需进入 Claude 模式，请在输入框输入 claude；如需进入 Codex 模式，请在输入框输入 codex。',
      ),
      _TipItem(
        number: '二',
        title: '连接成功判断标准',
        body: '点击左上角文件图标，页面成功展示文件树，即代表服务连接成功。',
      ),
      _TipItem(
        number: '三',
        title: 'API 接入配置说明',
        body: '当你使用 Claude Code 或 Codex 接入自己的 API 时，请务必完成以下关键配置：\n'
            '1. 成功连接 API 后，立即前往输入框上方的「模型设置」\n'
            '2. 将当前模型修改为 default\n'
            '3. 完成设置后，再进行后续操作，避免因模型不匹配导致功能异常',
      ),
      _TipItem(
        number: '四',
        title: 'Claude / Codex 会话启动方式',
        body: '1. 连接成功出现文件树之后，系统会自动在界面上弹出「Claude 会话」或「Codex 会话」的启动提示\n'
            '2. 点击提示，即可直接进入对应的 AI 对话界面\n'
            '3. 进入会话后，直接输入你的问题或指令，即可与 AI 交互',
      ),
      _TipItem(
        number: '五',
        title: '会话记录异常刷新方法',
        body: '会话记录加载异常、显示异常时，可在文件树里回退到上级目录，再重新进入当前目录即可刷新，多数为缓存问题。',
      ),
      _TipItem(
        number: '六',
        title: '工作目录配置建议',
        body: '初始连接时建议将工作目录配置为根目录，再通过文件树进入子工作目录，可减少目录与会话加载异常。',
      ),
      _TipItem(
        number: '七',
        title: '注意事项',
        body: '• 接入自定义 API 时，模型修改为 default 是必做步骤，否则可能无法正常调用\n'
            '• 没有弹出会话提示、看不到文件树，优先检查网络和 API 连接状态\n'
            '• 会话记录加载异常，用「回退上级目录→重新进入」刷新\n'
            '• 初始工作目录推荐设为根目录，再通过文件树进入子目录\n'
            '• 如遇功能异常、连接失败等问题，可随时反馈给项目维护者',
      ),
    ]);
  }

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final scheme = theme.colorScheme;
    final isLight = theme.brightness == Brightness.light;
    final keyboardInset = MediaQuery.viewInsetsOf(context).bottom;
    final compactToolbar = MediaQuery.sizeOf(context).width < 430;
    return Scaffold(
      key: _scaffoldKey,
      drawer: Drawer(
        width: 360,
        child: ListenableBuilder(
          listenable: controller,
          builder: (context, _) {
            return FileBrowserSheet(
              currentPath: controller.currentDirectoryPath,
              items: controller.currentDirectoryItems,
              loading: controller.fileListLoading,
              onRefresh: () => controller.refreshFileList(),
              onGoParent: () => controller.goParentDirectory(),
              onOpenDirectory: (path) =>
                  controller.switchWorkingDirectory(path),
              onOpenFile: (path) async {
                controller.openFile(path);
                Navigator.pop(context);
                await _openFileViewer(context);
              },
              onDownloadFile: (path) async {
                Navigator.pop(context);
                await _downloadFile(path);
              },
            );
          },
        ),
      ),
      resizeToAvoidBottomInset: false,
      extendBodyBehindAppBar: true,
      body: Stack(
        children: [
          Positioned.fill(
            child: DecoratedBox(
              decoration: BoxDecoration(
                gradient: LinearGradient(
                  begin: Alignment.topLeft,
                  end: Alignment.bottomRight,
                  colors: [
                    theme.scaffoldBackgroundColor,
                    Color.alphaBlend(
                      scheme.primary.withValues(alpha: isLight ? 0.035 : 0.08),
                      theme.scaffoldBackgroundColor,
                    ),
                    Color.alphaBlend(
                      scheme.secondary
                          .withValues(alpha: isLight ? 0.025 : 0.045),
                      theme.scaffoldBackgroundColor,
                    ),
                  ],
                ),
              ),
            ),
          ),
          controller.shouldShowSessionSurface
              ? GestureDetector(
                  behavior: HitTestBehavior.translucent,
                  onTap: () => FocusManager.instance.primaryFocus?.unfocus(),
                  child: Column(
                    children: [
                      SizedBox(
                          height: MediaQuery.of(context).padding.top +
                              kToolbarHeight),
                      if (controller.shouldShowSessionObservationBanner)
                        _SessionObservationBanner(controller: controller),
                      Expanded(
                        child: (controller.timeline.isEmpty &&
                                controller.pendingPrompt?.hasVisiblePrompt !=
                                    true &&
                                controller
                                        .pendingInteraction?.hasVisiblePrompt !=
                                    true &&
                                !controller.shouldShowPlanChoices &&
                                !controller.aiStatusIndicatorVisible)
                            ? const Center(child: _LandingBrand())
                            : Column(
                                children: [
                                  if (controller.hasCompactContextSelection)
                                    _ContextSelectionBar(
                                        controller: controller),
                                  Expanded(
                                    child: ChatTimeline(
                                      items: controller.timeline,
                                      sessionId: controller.selectedSessionId,
                                      mediaPreviewStates:
                                          controller.mediaPreviewStates,
                                      activeReviewDiff:
                                          controller.currentReviewDiff,
                                      activeReviewGroup:
                                          controller.activeReviewGroup,
                                      pendingDiffCount:
                                          controller.pendingDiffCount,
                                      pendingReviewGroupCount:
                                          controller.pendingReviewGroupCount,
                                      isManualReviewMode:
                                          controller.isManualReviewMode,
                                      isAutoAcceptMode:
                                          controller.isAutoAcceptMode,
                                      pendingPrompt: controller.pendingPrompt,
                                      pendingInteraction:
                                          controller.pendingInteraction,
                                      shouldShowReviewChoices:
                                          controller.shouldShowReviewChoices,
                                      pendingPlanQuestion:
                                          controller.pendingPlanQuestion,
                                      pendingPlanProgressLabel:
                                          controller.pendingPlanProgressLabel,
                                      shouldShowPlanChoices:
                                          controller.shouldShowPlanChoices,
                                      isAiRunning:
                                          controller.aiStatusIndicatorVisible,
                                      aiStatusLabel:
                                          controller.aiStatusIndicatorLabel,
                                      bottomPadding: _timelineBottomPadding +
                                          keyboardInset,
                                      hasOlderItems:
                                          controller.hasOlderTimelineEntries,
                                      isLoadingOlderItems: controller
                                          .isLoadingOlderTimelineEntries,
                                      onOpenDiff: () => _openDiff(context),
                                      onOpenRuntimeInfo: () =>
                                          _openRuntimeInfo(context),
                                      onOpenFile: () =>
                                          _openFileViewer(context),
                                      onOpenAttachment: (attachment) =>
                                          _openAttachment(context, attachment),
                                      onRequestMediaPreview:
                                          controller.requestMediaPreview,
                                      onLoadOlderItems:
                                          controller.loadOlderTimelineEntries,
                                      onReviewDecision:
                                          controller.sendReviewDecision,
                                      onAcceptAll:
                                          controller.acceptAllPendingDiffs,
                                      onPromptSubmit:
                                          controller.submitPromptOption,
                                    ),
                                  ),
                                ],
                              ),
                      ),
                    ],
                  ),
                )
              : const Center(
                  child: _LandingBrand(),
                ),
          Positioned(
            top: 0,
            left: 0,
            right: 0,
            child: ClipRect(
              child: BackdropFilter(
                filter: ImageFilter.blur(sigmaX: 20, sigmaY: 20),
                child: Container(
                  decoration: BoxDecoration(
                    color:
                        scheme.surface.withValues(alpha: isLight ? 0.88 : 0.7),
                    boxShadow: [
                      if (isLight)
                        BoxShadow(
                          color: Colors.black.withValues(alpha: 0.05),
                          blurRadius: 18,
                          offset: const Offset(0, 8),
                        ),
                    ],
                  ),
                  child: SafeArea(
                    bottom: false,
                    child: Container(
                      height: kToolbarHeight,
                      padding: const EdgeInsets.symmetric(horizontal: 4),
                      decoration: BoxDecoration(
                        border: Border(
                          bottom: BorderSide(
                            color: Theme.of(context)
                                .colorScheme
                                .outlineVariant
                                .withValues(alpha: isLight ? 0.56 : 0.2),
                            width: 0.5,
                          ),
                        ),
                      ),
                      child: Row(
                        children: [
                          IconButton(
                            onPressed: _openFileDrawer,
                            icon: const Icon(Icons.folder_outlined),
                            tooltip: '文件树',
                          ),
                          const SizedBox(width: 4),
                          Expanded(
                            child: Text(
                              controller.shouldShowSessionSurface
                                  ? controller.selectedSessionTitle
                                  : 'MobileVC',
                              maxLines: 1,
                              overflow: TextOverflow.ellipsis,
                              style: Theme.of(context)
                                  .textTheme
                                  .titleMedium
                                  ?.copyWith(fontWeight: FontWeight.w700),
                            ),
                          ),
                          const SizedBox(width: 8),
                          _ConnectionDot(
                            connected: controller.connected,
                            label: controller.activeTransportLabel,
                          ),
                          if (!compactToolbar) ...[
                            IconButton(
                              onPressed: controller.currentDiffContext == null
                                  ? null
                                  : () => _openDiff(context),
                              tooltip: 'Diff',
                              icon: Badge.count(
                                isLabelVisible: controller.pendingDiffCount > 0,
                                count: controller.pendingDiffCount > 0
                                    ? controller.pendingDiffCount
                                    : 1,
                                child: const Icon(Icons.difference_outlined),
                              ),
                            ),
                            IconButton(
                              onPressed: () => _openAdbDebug(context),
                              tooltip: 'ADB 调试',
                              icon: Badge(
                                isLabelVisible: controller.adbStreaming ||
                                    controller.adbWebRtcStarting,
                                child: const Icon(Icons.phone_android_outlined),
                              ),
                            ),
                            IconButton(
                              onPressed: () => _openVoiceCall(context),
                              tooltip: '语音通话',
                              icon: const Icon(Icons.call_outlined),
                            ),
                            IconButton(
                              onPressed: widget.onToggleTheme,
                              tooltip:
                                  widget.darkModeEnabled ? '切换浅色模式' : '切换深色模式',
                              icon: Icon(
                                widget.darkModeEnabled
                                    ? Icons.light_mode_outlined
                                    : Icons.dark_mode_outlined,
                              ),
                            ),
                          ] else
                            PopupMenuButton<String>(
                              tooltip: '更多',
                              icon: const Icon(Icons.more_vert_rounded),
                              onSelected: (value) {
                                switch (value) {
                                  case 'diff':
                                    if (controller.currentDiffContext != null) {
                                      _openDiff(context);
                                    }
                                    break;
                                  case 'adb':
                                    _openAdbDebug(context);
                                    break;
                                  case 'voice':
                                    _openVoiceCall(context);
                                    break;
                                  case 'theme':
                                    widget.onToggleTheme?.call();
                                    break;
                                }
                              },
                              itemBuilder: (context) => [
                                PopupMenuItem(
                                  value: 'diff',
                                  enabled:
                                      controller.currentDiffContext != null,
                                  child: const Text('Diff'),
                                ),
                                const PopupMenuItem(
                                  value: 'adb',
                                  child: Text('ADB 调试'),
                                ),
                                const PopupMenuItem(
                                  value: 'voice',
                                  child: Text('语音通话'),
                                ),
                                PopupMenuItem(
                                  value: 'theme',
                                  child: Text(widget.darkModeEnabled
                                      ? '切换浅色模式'
                                      : '切换深色模式'),
                                ),
                              ],
                            ),
                          IconButton(
                            onPressed: () => _openStatusDetails(context),
                            icon: const Icon(Icons.dashboard_outlined),
                          ),
                          IconButton(
                            onPressed: () => _openConnectionConfig(context),
                            icon: const Icon(Icons.settings_outlined),
                          ),
                        ],
                      ),
                    ),
                  ),
                ),
              ),
            ),
          ),
          Positioned(
            left: 0,
            right: 0,
            bottom: 0,
            child: AnimatedPadding(
              key: const ValueKey('command-bar-keyboard-padding'),
              duration: const Duration(milliseconds: 180),
              curve: Curves.easeOutCubic,
              padding: EdgeInsets.only(
                bottom: keyboardInset,
              ),
              child: _MeasuredSize(
                onChanged: _handleCommandBarSizeChanged,
                child: _buildCommandInputBar(context),
              ),
            ),
          ),
        ],
      ),
    );
  }

  void _handleCommandBarSizeChanged(Size size) {
    if (!mounted) {
      return;
    }
    final nextPadding = size.height + _commandBarGap;
    if ((nextPadding - _timelineBottomPadding).abs() < 0.5) {
      return;
    }
    setState(() {
      _timelineBottomPadding = nextPadding;
    });
  }

  Widget _buildCommandInputBar(BuildContext context) {
    return CommandInputBar(
      awaitInput: controller.awaitInput,
      isBusy: controller.isSessionBusy,
      canStop: controller.canStopCurrentRun,
      canCompact: controller.shouldShowCompactButton,
      isCompacting: controller.isCompacting,
      compactStatusLabel: controller.compactStatusLabel,
      contextWindowUsage: controller.contextWindowUsage,
      onOpenContextWindowUsage: () => _openContextWindowUsage(context),
      hasPendingReview: controller.hasPendingReview,
      fastMode: controller.fastMode,
      permissionMode: controller.displayPermissionMode,
      shouldShowPermissionChoices: controller.shouldShowPermissionChoices,
      shouldShowReviewChoices: controller.shouldShowReviewChoices,
      shouldShowPlanChoices: controller.shouldShowPlanChoices,
      onSubmit: controller.sendInputTextWithImages,
      onAttachImage: () => _pickImageAttachment(context),
      onStop: controller.stopCurrentRun,
      onCompact: controller.compactCurrentSession,
      onOpenSessions: () => _openSessions(context),
      onOpenRuntimeInfo: () => _openRuntimeInfo(context),
      onOpenLogs: () => _openLogs(context),
      onOpenSkills: () => _openSkills(context),
      onOpenMemory: () => _openMemory(context),
      onOpenPermissions: () => _openPermissions(context),
      onOpenModels: () => _openModelSwitcher(context),
      onPermissionModeChanged: controller.updatePermissionMode,
      codexTargetMode: controller.codexTargetMode,
      onCodexTargetModeChanged: controller.updateCodexTargetMode,
      showClaudeMode: controller.shouldShowClaudeMode,
      currentEngine: controller.commandBarEngine,
      configuredEngine: controller.configuredAiEngine,
      modelSummary: controller.commandBarModelSummary,
      permissionRuleSummary: controller.permissionRuleSummary,
      isSessionLoading: controller.isLoadingSession,
      canSendToContinuedSameSession: controller.canSendToContinuedSameSession,
      isExternallyLocked: controller.isSessionReadOnly,
      externalLockedHint: controller.sessionReadOnlyHint,
    );
  }

  Future<void> _openContextWindowUsage(BuildContext context) async {
    await showModalBottomSheet<void>(
      context: context,
      useSafeArea: true,
      showDragHandle: true,
      isScrollControlled: true,
      builder: (context) => ListenableBuilder(
        listenable: controller,
        builder: (context, _) {
          final engine = controller.commandBarEngine.trim();
          final engineLabel = engine.isEmpty
              ? 'AI'
              : '${engine[0].toUpperCase()}${engine.substring(1)}';
          return ContextWindowUsageSheet(
            usage: controller.contextWindowUsage,
            engineLabel: engineLabel,
          );
        },
      ),
    );
  }

  Future<void> _openVoiceCall(BuildContext context) async {
    await showModalBottomSheet<void>(
      context: context,
      useSafeArea: true,
      showDragHandle: false,
      isScrollControlled: true,
      builder: (context) => VoiceCallSheet(controller: controller),
    );
  }

  void _openFileDrawer() {
    controller.refreshFileList();
    _scaffoldKey.currentState?.openDrawer();
  }

  Future<ChatImageAttachment?> _pickImageAttachment(
      BuildContext context) async {
    final messenger = ScaffoldMessenger.of(context);
    try {
      final result = await FilePicker.platform.pickFiles(
        type: FileType.custom,
        allowedExtensions: const ['jpg', 'jpeg', 'png', 'webp', 'gif'],
        allowMultiple: false,
        withData: true,
      );
      if (!context.mounted || result == null || result.files.isEmpty) {
        return null;
      }
      final file = result.files.single;
      final bytes = file.bytes ?? await _readPickedFileBytes(file);
      if (bytes == null || bytes.isEmpty) {
        messenger
          ..hideCurrentSnackBar()
          ..showSnackBar(const SnackBar(content: Text('无法读取图片内容')));
        return null;
      }
      if (bytes.length > _maxImageAttachmentBytes) {
        messenger
          ..hideCurrentSnackBar()
          ..showSnackBar(const SnackBar(content: Text('图片不能超过 4 MiB')));
        return null;
      }
      final mimeType = _imageMimeType(file.name);
      if (mimeType.isEmpty) {
        messenger
          ..hideCurrentSnackBar()
          ..showSnackBar(const SnackBar(content: Text('仅支持 JPG、PNG、WebP、GIF')));
        return null;
      }
      return ChatImageAttachment(
        name: file.name,
        mimeType: mimeType,
        bytes: bytes,
      );
    } catch (error) {
      if (!context.mounted) {
        return null;
      }
      messenger
        ..hideCurrentSnackBar()
        ..showSnackBar(SnackBar(content: Text('选择图片失败：$error')));
      return null;
    }
  }

  Future<Uint8List?> _readPickedFileBytes(PlatformFile file) async {
    final path = file.path;
    if (path == null || path.trim().isEmpty) {
      return null;
    }
    return File(path).readAsBytes();
  }

  String _imageMimeType(String name) {
    final lower = name.toLowerCase();
    if (lower.endsWith('.jpg') || lower.endsWith('.jpeg')) {
      return 'image/jpeg';
    }
    if (lower.endsWith('.png')) {
      return 'image/png';
    }
    if (lower.endsWith('.webp')) {
      return 'image/webp';
    }
    if (lower.endsWith('.gif')) {
      return 'image/gif';
    }
    return '';
  }

  Future<void> _openConnectionConfig(BuildContext context) async {
    final hostController =
        TextEditingController(text: controller.config.displayHost);
    final portController = TextEditingController(text: controller.config.port);
    final tokenController =
        TextEditingController(text: controller.config.token);
    final cwdController = TextEditingController(text: controller.config.cwd);
    final historyWindowLimitController = TextEditingController(
      text: controller.config.historyWindowLimit.toString(),
    );
    final linkController = TextEditingController();
    final permissionController =
        TextEditingController(text: controller.config.permissionMode);
    final iceHostController = TextEditingController(
      text: controller.config.adbIceHostOverride,
    );
    final iceUsernameController = TextEditingController(
      text: controller.config.adbIceUsername,
    );
    final iceCredentialController = TextEditingController(
      text: controller.config.adbIceCredential,
    );
    String scanHint = '';
    final hadLegacyIceConfig =
        controller.config.adbIceServersJson.trim().isNotEmpty &&
            !controller.config.hasAutoAdbIceConfig;
    var selectedEngine = controller.config.engine.trim().isEmpty
        ? 'claude'
        : controller.config.engine.trim();
    var selectedCodexSandboxMode = controller.config.codexSandboxMode;
    var pendingConfig = controller.config;
    var connectingFromSheet = false;

    await showModalBottomSheet<void>(
      context: context,
      isScrollControlled: true,
      useSafeArea: true,
      showDragHandle: true,
      builder: (context) {
        return StatefulBuilder(
          builder: (context, setSheetState) {
            String encodedIceConfig() => AppConfig.encodeAutoAdbIceConfig(
                  host: iceHostController.text,
                  username: iceUsernameController.text,
                  credential: iceCredentialController.text,
                );

            final normalizedIceHost = _iceHostLiteral(
              iceHostController.text.trim().isNotEmpty
                  ? iceHostController.text.trim()
                  : (hostController.text.trim().isEmpty
                      ? controller.config.host
                      : hostController.text.trim()),
            );
            final relayModeSelected =
                pendingConfig.connectionMode != ConnectionMode.direct.name;
            final connectionBusy = connectingFromSheet || controller.connecting;
            final canConnectRelay = !relayModeSelected ||
                pendingConfig.relayPairingSecret.trim().isNotEmpty ||
                (pendingConfig.relayClientId.trim().isNotEmpty &&
                    pendingConfig.relayClientReconnectSecret.trim().isNotEmpty);
            final permissionModeEngine = selectedEngine.trim().toLowerCase();
            final selectedPermissionMode = normalizePermissionModeForEngine(
              permissionController.text,
              permissionModeEngine,
            );
            final permissionModeItems =
                permissionModeOptionsForEngine(permissionModeEngine)
                    .map(
                      (option) => DropdownMenuItem<String>(
                        value: option.value,
                        child: Text(option.label),
                      ),
                    )
                    .toList(growable: false);

            void applyScannedConfig(AppConfig scanned) {
              pendingConfig = scanned;
              hostController.text = scanned.displayHost;
              portController.text = scanned.port;
              tokenController.text = scanned.token;
              cwdController.text = scanned.cwd;
              historyWindowLimitController.text =
                  scanned.historyWindowLimit.toString();
              iceHostController.text = scanned.adbIceHostOverride;
              iceUsernameController.text = scanned.adbIceUsername;
              iceCredentialController.text = scanned.adbIceCredential;
              selectedEngine = scanned.engine.trim().isEmpty
                  ? selectedEngine
                  : scanned.engine.trim();
              selectedCodexSandboxMode = scanned.codexSandboxMode;
              scanHint = scanned.connectionMode != ConnectionMode.direct.name
                  ? '已导入 Relay 配对，点击连接完成配对'
                  : '已回填 ${scanned.displayHost}:${scanned.port}${scanned.token.isNotEmpty ? ' 与 token' : ''}';
            }

            void showImportSnackBar(String message) {
              ScaffoldMessenger.of(context)
                ..hideCurrentSnackBar()
                ..showSnackBar(SnackBar(content: Text(message)));
            }

            AppConfig? parseConnectionLink(String raw) {
              final historyWindowLimit = int.tryParse(
                historyWindowLimitController.text.trim(),
              );
              if (historyWindowLimit == null || historyWindowLimit <= 0) {
                scanHint = '历史加载条数必须是正整数';
                return null;
              }
              try {
                return AppConfig.fromLaunchUri(
                  raw,
                  fallback: pendingConfig.copyWith(
                    host: hostController.text.trim(),
                    port: portController.text.trim(),
                    token: tokenController.text.trim(),
                    cwd: cwdController.text.trim(),
                    engine: selectedEngine,
                    codexSandboxMode: selectedCodexSandboxMode,
                    historyWindowLimit: historyWindowLimit,
                    permissionMode: permissionController.text.trim(),
                    fastMode: controller.fastMode,
                    adbIceServersJson: encodedIceConfig(),
                  ),
                );
              } on FormatException catch (error) {
                final message = error.message.toString().trim();
                scanHint = message.isEmpty ? '链接格式错误' : '导入失败：$message';
                return null;
              }
            }

            Future<bool> handleRelayImport(
              AppConfig scanned,
              String raw,
            ) async {
              if (scanned.connectionMode == ConnectionMode.direct.name) {
                return false;
              }
              final imported = await controller.importConnectionLink(raw);
              if (!context.mounted) {
                return imported;
              }
              pendingConfig = controller.config;
              applyScannedConfig(pendingConfig);
              scanHint = imported
                  ? '已导入 Relay 配对，点击连接完成配对'
                  : controller.connectionMessage;
              return true;
            }

            Future<void> handleScan() async {
              if (connectionBusy) {
                return;
              }
              final scannedRaw = await showModalBottomSheet<String>(
                context: context,
                isScrollControlled: true,
                showDragHandle: true,
                builder: (context) => const ConnectionScanSheet(),
              );
              if (!context.mounted || scannedRaw == null) {
                return;
              }
              final scanned = parseConnectionLink(scannedRaw);
              if (scanned == null) {
                setSheetState(() {
                  if (scanHint.trim().isEmpty) {
                    scanHint = '扫码内容无法识别，请确认二维码来自 MobileVC 启动器。';
                  }
                });
                showImportSnackBar(scanHint);
                return;
              }
              final handledRelay = await handleRelayImport(scanned, scannedRaw);
              if (!context.mounted) {
                return;
              }
              setSheetState(() {
                linkController.text = scannedRaw.trim();
                if (!handledRelay) {
                  applyScannedConfig(scanned);
                }
              });
              showImportSnackBar(scanHint);
            }

            Future<void> handlePasteLink() async {
              if (connectionBusy) {
                return;
              }
              final scanned = parseConnectionLink(linkController.text);
              if (scanned == null) {
                setSheetState(() {
                  if (scanHint.trim().isEmpty) {
                    scanHint = '链接无法识别，请粘贴 mobilevc://relay/v1 或启动器二维码内容。';
                  }
                });
                showImportSnackBar(scanHint);
                return;
              }
              final handledRelay =
                  await handleRelayImport(scanned, linkController.text);
              if (!context.mounted) {
                return;
              }
              setSheetState(() {
                if (!handledRelay) {
                  applyScannedConfig(scanned);
                }
              });
              showImportSnackBar(scanHint);
            }

            Future<bool> persistConfig({bool connect = false}) async {
              final hostText = hostController.text.trim();
              final historyWindowLimit = int.tryParse(
                historyWindowLimitController.text.trim(),
              );
              if (historyWindowLimit == null || historyWindowLimit <= 0) {
                setSheetState(() {
                  scanHint = '历史加载条数必须是正整数';
                });
                return false;
              }
              final nextConfig = pendingConfig.copyWith(
                host: pendingConfig.connectionMode == ConnectionMode.relay.name
                    ? pendingConfig.host
                    : hostText,
                port: pendingConfig.connectionMode == ConnectionMode.relay.name
                    ? pendingConfig.port
                    : _portForHostInput(hostText, portController.text),
                token: pendingConfig.connectionMode == ConnectionMode.relay.name
                    ? pendingConfig.token
                    : tokenController.text.trim(),
                cwd: cwdController.text.trim(),
                engine: selectedEngine,
                codexSandboxMode: selectedCodexSandboxMode,
                historyWindowLimit: historyWindowLimit,
                permissionMode: permissionController.text.trim(),
                fastMode: controller.fastMode,
                adbIceServersJson: encodedIceConfig(),
              );
              await controller.saveConfig(nextConfig);
              if (connect) {
                await controller.connect();
                if (!controller.connected) {
                  pendingConfig = controller.config;
                  scanHint = controller.connectionMessage;
                  return false;
                }
              }
              if (context.mounted) {
                Navigator.pop(context);
              }
              return true;
            }

            return Padding(
              padding: EdgeInsets.fromLTRB(
                  16, 16, 16, 16 + MediaQuery.of(context).viewInsets.bottom),
              child: SingleChildScrollView(
                child: Column(
                  mainAxisSize: MainAxisSize.min,
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    Text('连接配置', style: Theme.of(context).textTheme.titleLarge),
                    const SizedBox(height: 6),
                    Text(
                      '支持扫描局域网二维码或 Relay 配对二维码，也保留手动输入方式。',
                      style: Theme.of(context).textTheme.bodySmall,
                    ),
                    const SizedBox(height: 16),
                    Container(
                      padding: const EdgeInsets.all(16),
                      decoration: BoxDecoration(
                        color: Theme.of(context).colorScheme.surface,
                        borderRadius: BorderRadius.circular(16),
                        border: Border.all(
                          color: Theme.of(context)
                              .colorScheme
                              .outline
                              .withValues(alpha: 0.12),
                        ),
                      ),
                      child: Column(
                        crossAxisAlignment: CrossAxisAlignment.start,
                        children: [
                          Row(
                            children: [
                              Icon(Icons.link,
                                  size: 20,
                                  color: Theme.of(context).colorScheme.primary),
                              const SizedBox(width: 8),
                              Text(
                                '快速连接',
                                style: Theme.of(context)
                                    .textTheme
                                    .titleSmall
                                    ?.copyWith(
                                      fontWeight: FontWeight.w600,
                                      color: Theme.of(context)
                                          .colorScheme
                                          .onSurface,
                                    ),
                              ),
                            ],
                          ),
                          const SizedBox(height: 12),
                          SizedBox(
                            width: double.infinity,
                            child: OutlinedButton.icon(
                              onPressed: connectionBusy ? null : handleScan,
                              icon: const Icon(Icons.qr_code_scanner),
                              label: const Text('扫码连接'),
                            ),
                          ),
                          const SizedBox(height: 12),
                          Row(
                            children: [
                              Expanded(
                                child: TextField(
                                  controller: linkController,
                                  enabled: !connectionBusy,
                                  decoration: const InputDecoration(
                                    labelText: '连接链接',
                                    hintText: 'mobilevc://relay/v1?...',
                                  ),
                                ),
                              ),
                              const SizedBox(width: 10),
                              FilledButton.tonal(
                                onPressed:
                                    connectionBusy ? null : handlePasteLink,
                                child: const Text('导入'),
                              ),
                            ],
                          ),
                        ],
                      ),
                    ),
                    if (scanHint.isNotEmpty) ...[
                      const SizedBox(height: 8),
                      Text(
                        scanHint,
                        style: Theme.of(context).textTheme.bodySmall,
                      ),
                    ],
                    const SizedBox(height: 12),
                    if (relayModeSelected) ...[
                      _RelayPairingSummary(config: pendingConfig),
                    ] else ...[
                      TextField(
                        controller: hostController,
                        enabled: !connectionBusy,
                        decoration: const InputDecoration(
                          labelText: 'Host / URL',
                          hintText: 'https://host',
                        ),
                        onChanged: (_) => setSheetState(() {}),
                      ),
                      const SizedBox(height: 10),
                      TextField(
                          controller: portController,
                          enabled: !connectionBusy,
                          decoration: const InputDecoration(labelText: 'Port')),
                      const SizedBox(height: 10),
                      TextField(
                          controller: tokenController,
                          enabled: !connectionBusy,
                          decoration:
                              const InputDecoration(labelText: 'Token')),
                    ],
                    const SizedBox(height: 16),
                    Container(
                      padding: const EdgeInsets.all(16),
                      decoration: BoxDecoration(
                        color: Theme.of(context).colorScheme.surface,
                        borderRadius: BorderRadius.circular(16),
                        border: Border.all(
                          color: Theme.of(context)
                              .colorScheme
                              .outline
                              .withValues(alpha: 0.12),
                        ),
                      ),
                      child: Column(
                        crossAxisAlignment: CrossAxisAlignment.start,
                        children: [
                          Row(
                            children: [
                              Icon(Icons.settings_input_antenna,
                                  size: 20,
                                  color: Theme.of(context).colorScheme.primary),
                              const SizedBox(width: 8),
                              Text(
                                '连接模式',
                                style: Theme.of(context)
                                    .textTheme
                                    .titleSmall
                                    ?.copyWith(
                                      fontWeight: FontWeight.w600,
                                      color: Theme.of(context)
                                          .colorScheme
                                          .onSurface,
                                    ),
                              ),
                            ],
                          ),
                          const SizedBox(height: 12),
                          SegmentedButton<ConnectionMode>(
                            segments: const [
                              ButtonSegment(
                                value: ConnectionMode.direct,
                                icon: Icon(Icons.lan_outlined),
                                label: Text('直连'),
                              ),
                              ButtonSegment(
                                value: ConnectionMode.auto,
                                icon: Icon(Icons.swap_horiz_outlined),
                                label: Text('自动'),
                              ),
                              ButtonSegment(
                                value: ConnectionMode.relay,
                                icon: Icon(Icons.hub_outlined),
                                label: Text('中继'),
                              ),
                            ],
                            selected: {
                              ConnectionMode.values.firstWhere(
                                (mode) =>
                                    mode.name == pendingConfig.connectionMode,
                                orElse: () => ConnectionMode.direct,
                              ),
                            },
                            onSelectionChanged: connectionBusy
                                ? null
                                : (selection) {
                                    final mode = selection.first;
                                    setSheetState(() {
                                      if (mode == ConnectionMode.direct) {
                                        pendingConfig = pendingConfig.copyWith(
                                          connectionMode:
                                              ConnectionMode.direct.name,
                                        );
                                        scanHint = '已切换为局域网直连模式';
                                        return;
                                      }
                                      if (pendingConfig.relayUrl
                                              .trim()
                                              .isEmpty ||
                                          pendingConfig.relaySessionId
                                              .trim()
                                              .isEmpty) {
                                        scanHint = '请先导入 Relay 配对链接';
                                        return;
                                      }
                                      pendingConfig = pendingConfig.copyWith(
                                        connectionMode: mode.name,
                                      );
                                      scanHint = mode == ConnectionMode.auto
                                          ? '已切换为自动模式：优先 LAN，Relay 兜底'
                                          : '已切换为 Relay 中继模式';
                                    });
                                  },
                          ),
                          if (relayModeSelected &&
                              pendingConfig.relayNodeFingerprintHex
                                  .trim()
                                  .isNotEmpty) ...[
                            const SizedBox(height: 8),
                            Text(
                              '节点指纹：${_shortFingerprint(pendingConfig.relayNodeFingerprintHex)}',
                              style: Theme.of(context).textTheme.bodySmall,
                            ),
                          ],
                        ],
                      ),
                    ),
                    const SizedBox(height: 16),
                    Container(
                      padding: const EdgeInsets.all(16),
                      decoration: BoxDecoration(
                        color: Theme.of(context).colorScheme.surface,
                        borderRadius: BorderRadius.circular(16),
                        border: Border.all(
                          color: Theme.of(context)
                              .colorScheme
                              .outline
                              .withValues(alpha: 0.12),
                        ),
                      ),
                      child: Column(
                        crossAxisAlignment: CrossAxisAlignment.start,
                        children: [
                          Row(
                            children: [
                              Icon(Icons.tune,
                                  size: 20,
                                  color: Theme.of(context).colorScheme.primary),
                              const SizedBox(width: 8),
                              Text(
                                '高级设置',
                                style: Theme.of(context)
                                    .textTheme
                                    .titleSmall
                                    ?.copyWith(
                                      fontWeight: FontWeight.w600,
                                      color: Theme.of(context)
                                          .colorScheme
                                          .onSurface,
                                    ),
                              ),
                            ],
                          ),
                          const SizedBox(height: 12),
                          TextField(
                              controller: cwdController,
                              enabled: !connectionBusy,
                              decoration:
                                  const InputDecoration(labelText: 'CWD')),
                          const SizedBox(height: 10),
                          TextField(
                            controller: historyWindowLimitController,
                            enabled: !connectionBusy,
                            keyboardType: TextInputType.number,
                            decoration: const InputDecoration(
                              labelText: '历史加载条数',
                              hintText: '120',
                            ),
                          ),
                          const SizedBox(height: 10),
                          DropdownButtonFormField<String>(
                            initialValue: selectedEngine,
                            decoration:
                                const InputDecoration(labelText: 'Engine'),
                            items: const [
                              DropdownMenuItem(
                                value: 'claude',
                                child: Text('Claude'),
                              ),
                              DropdownMenuItem(
                                value: 'codex',
                                child: Text('Codex'),
                              ),
                              DropdownMenuItem(
                                value: 'gemini',
                                child: Text('Gemini'),
                              ),
                            ],
                            onChanged: connectionBusy
                                ? null
                                : (value) {
                                    if (value == null) {
                                      return;
                                    }
                                    setSheetState(() {
                                      selectedEngine = value;
                                      permissionController.text =
                                          normalizePermissionModeForEngine(
                                        permissionController.text,
                                        selectedEngine,
                                      );
                                    });
                                  },
                          ),
                          if (selectedEngine.trim().toLowerCase() ==
                              'codex') ...[
                            const SizedBox(height: 10),
                            DropdownButtonFormField<String>(
                              initialValue: selectedCodexSandboxMode,
                              decoration: const InputDecoration(
                                labelText: 'Codex 沙箱范围',
                              ),
                              items: const [
                                DropdownMenuItem(
                                  value: 'workspace-write',
                                  child: Text('工作区写入'),
                                ),
                                DropdownMenuItem(
                                  value: 'danger-full-access',
                                  child: Text('关闭沙箱'),
                                ),
                                DropdownMenuItem(
                                  value: 'read-only',
                                  child: Text('只读'),
                                ),
                                DropdownMenuItem(
                                  value: 'config',
                                  child: Text('自定义(config.toml)'),
                                ),
                              ],
                              onChanged: connectionBusy
                                  ? null
                                  : (value) {
                                      if (value == null) {
                                        return;
                                      }
                                      setSheetState(() {
                                        selectedCodexSandboxMode =
                                            AppConfig.normalizeCodexSandboxMode(
                                          value,
                                        );
                                      });
                                    },
                            ),
                          ],
                          const SizedBox(height: 10),
                          if (selectedEngine.trim().toLowerCase() == 'codex')
                            DropdownButtonFormField<String>(
                              key: const ValueKey(
                                'connection-config-codex-permission-mode',
                              ),
                              initialValue: selectedPermissionMode,
                              decoration: const InputDecoration(
                                labelText: 'Codex 审批策略',
                              ),
                              items: permissionModeItems,
                              onChanged: connectionBusy
                                  ? null
                                  : (value) {
                                      if (value == null) {
                                        return;
                                      }
                                      permissionController.text = value;
                                    },
                            )
                          else if (selectedEngine.trim().toLowerCase() ==
                              'claude')
                            DropdownButtonFormField<String>(
                              key: const ValueKey(
                                'connection-config-claude-permission-mode',
                              ),
                              initialValue: selectedPermissionMode,
                              decoration: const InputDecoration(
                                labelText: 'Claude 权限',
                              ),
                              items: permissionModeItems,
                              onChanged: connectionBusy
                                  ? null
                                  : (value) {
                                      if (value == null) {
                                        return;
                                      }
                                      permissionController.text = value;
                                    },
                            )
                          else
                            TextField(
                              controller: permissionController,
                              enabled: !connectionBusy,
                              decoration: const InputDecoration(
                                labelText: 'Permission Mode',
                              ),
                            ),
                        ],
                      ),
                    ),
                    const SizedBox(height: 16),
                    Container(
                      padding: const EdgeInsets.all(16),
                      decoration: BoxDecoration(
                        color: Theme.of(context).colorScheme.surface,
                        borderRadius: BorderRadius.circular(16),
                        border: Border.all(
                          color: Theme.of(context)
                              .colorScheme
                              .outline
                              .withValues(alpha: 0.12),
                        ),
                      ),
                      child: Column(
                        crossAxisAlignment: CrossAxisAlignment.start,
                        children: [
                          Row(
                            children: [
                              Icon(Icons.security,
                                  size: 20,
                                  color: Theme.of(context).colorScheme.primary),
                              const SizedBox(width: 8),
                              Text(
                                'ADB ICE 配置',
                                style: Theme.of(context)
                                    .textTheme
                                    .titleSmall
                                    ?.copyWith(
                                      fontWeight: FontWeight.w600,
                                      color: Theme.of(context)
                                          .colorScheme
                                          .onSurface,
                                    ),
                              ),
                            ],
                          ),
                          const SizedBox(height: 12),
                          TextField(
                            controller: iceHostController,
                            decoration: const InputDecoration(
                              labelText: 'ADB TURN Host Override',
                              hintText: '留空则跟 Host 一致',
                            ),
                            enabled: !relayModeSelected && !connectionBusy,
                            onChanged: (_) => setSheetState(() {}),
                          ),
                          const SizedBox(height: 10),
                          TextField(
                            controller: iceUsernameController,
                            enabled: !connectionBusy,
                            decoration: const InputDecoration(
                              labelText: 'ADB TURN Username',
                              hintText: 'mobilevc',
                            ),
                          ),
                          const SizedBox(height: 10),
                          TextField(
                            controller: iceCredentialController,
                            enabled: !connectionBusy,
                            decoration: const InputDecoration(
                              labelText: 'ADB TURN Credential',
                              hintText: 'credential',
                            ),
                          ),
                          const SizedBox(height: 6),
                          Text(
                            'ADB ICE 默认使用当前 Host，也可单独指定 TURN Host。',
                            style: Theme.of(context).textTheme.bodySmall,
                          ),
                          const SizedBox(height: 4),
                          Text(
                            'STUN: stun:$normalizedIceHost:${AppConfig.adbIcePort}',
                            style: Theme.of(context).textTheme.bodySmall,
                          ),
                          Text(
                            'TURN: turn:$normalizedIceHost:${AppConfig.adbIcePort}?transport=udp / tcp',
                            style: Theme.of(context).textTheme.bodySmall,
                          ),
                          if (hadLegacyIceConfig) ...[
                            const SizedBox(height: 6),
                            Text(
                              '检测到旧版自定义 ICE JSON。保存后会切换为自动 Host 模式。',
                              style: Theme.of(context)
                                  .textTheme
                                  .bodySmall
                                  ?.copyWith(
                                    color:
                                        Theme.of(context).colorScheme.primary,
                                  ),
                            ),
                          ],
                        ],
                      ),
                    ),
                    const SizedBox(height: 16),
                    Container(
                      padding: const EdgeInsets.all(16),
                      decoration: BoxDecoration(
                        color: Theme.of(context).colorScheme.surface,
                        borderRadius: BorderRadius.circular(16),
                        border: Border.all(
                          color: Theme.of(context)
                              .colorScheme
                              .outline
                              .withValues(alpha: 0.12),
                        ),
                      ),
                      child: Column(
                        crossAxisAlignment: CrossAxisAlignment.start,
                        children: [
                          Row(
                            children: [
                              Icon(Icons.play_arrow,
                                  size: 20,
                                  color: Theme.of(context).colorScheme.primary),
                              const SizedBox(width: 8),
                              Text(
                                '操作',
                                style: Theme.of(context)
                                    .textTheme
                                    .titleSmall
                                    ?.copyWith(
                                      fontWeight: FontWeight.w600,
                                      color: Theme.of(context)
                                          .colorScheme
                                          .onSurface,
                                    ),
                              ),
                            ],
                          ),
                          const SizedBox(height: 12),
                          Row(
                            children: [
                              Expanded(
                                child: FilledButton.tonal(
                                  onPressed: connectionBusy
                                      ? null
                                      : () => persistConfig(),
                                  child: const Text('保存'),
                                ),
                              ),
                              const SizedBox(width: 10),
                              Expanded(
                                child: FilledButton(
                                  onPressed: connectionBusy || !canConnectRelay
                                      ? null
                                      : () async {
                                          setSheetState(() {
                                            connectingFromSheet = true;
                                            scanHint = relayModeSelected
                                                ? '正在连接 Relay：配对链接会一次性使用，请等待结果。'
                                                : '正在连接...';
                                          });
                                          var connected = false;
                                          try {
                                            connected = await persistConfig(
                                                connect: true);
                                          } catch (error) {
                                            if (!context.mounted) {
                                              return;
                                            }
                                            setSheetState(() {
                                              connectingFromSheet = false;
                                              pendingConfig = controller.config;
                                              scanHint = '连接配置失败：$error';
                                            });
                                          }
                                          if (!context.mounted ||
                                              connected ||
                                              controller.connected) {
                                            return;
                                          }
                                          setSheetState(() {
                                            connectingFromSheet = false;
                                            pendingConfig = controller.config;
                                            scanHint =
                                                controller.connectionMessage;
                                          });
                                          ScaffoldMessenger.of(context)
                                              .showSnackBar(
                                            SnackBar(
                                              content: Text(
                                                  controller.connectionMessage),
                                            ),
                                          );
                                        },
                                  child: connectionBusy
                                      ? const Row(
                                          mainAxisAlignment:
                                              MainAxisAlignment.center,
                                          mainAxisSize: MainAxisSize.min,
                                          children: [
                                            SizedBox(
                                              width: 16,
                                              height: 16,
                                              child: CircularProgressIndicator(
                                                strokeWidth: 2,
                                              ),
                                            ),
                                            SizedBox(width: 8),
                                            Text('连接中'),
                                          ],
                                        )
                                      : const Text('连接'),
                                ),
                              ),
                            ],
                          ),
                          const SizedBox(height: 10),
                          if (controller.connected)
                            SizedBox(
                              width: double.infinity,
                              child: OutlinedButton(
                                onPressed: connectionBusy
                                    ? null
                                    : () async {
                                        await controller.disconnect();
                                        if (context.mounted) {
                                          Navigator.pop(context);
                                        }
                                      },
                                child: const Text('断开连接'),
                              ),
                            ),
                          if (controller.config.connectionMode !=
                              ConnectionMode.direct.name) ...[
                            const SizedBox(height: 10),
                            SizedBox(
                              width: double.infinity,
                              child: OutlinedButton.icon(
                                onPressed: connectionBusy
                                    ? null
                                    : () => _openRelaySecurity(context),
                                icon: const Icon(Icons.verified_user_outlined),
                                label: const Text('Relay 安全设备'),
                              ),
                            ),
                          ],
                        ],
                      ),
                    ),
                  ],
                ),
              ),
            );
          },
        );
      },
    );
  }

  Future<void> _openSessions(BuildContext context) async {
    await showModalBottomSheet<void>(
      context: context,
      isScrollControlled: true,
      useSafeArea: true,
      showDragHandle: true,
      builder: (context) {
        controller.requestSessionList();
        return ListenableBuilder(
          listenable: controller,
          builder: (context, _) {
            return SessionListSheet(
              sessions: controller.sessions,
              selectedSessionId: controller.selectedSessionId,
              cwd: controller.effectiveCwd,
              onCreate: controller.createSession,
              onLoad: (summary) {
                controller.loadSessionFromSummary(summary);
                Navigator.pop(context);
              },
              onDelete: controller.deleteSession,
            );
          },
        );
      },
    );
  }

  Future<void> _openFileViewer(BuildContext context) async {
    await showModalBottomSheet<void>(
      context: context,
      isScrollControlled: true,
      useSafeArea: true,
      showDragHandle: true,
      backgroundColor: Colors.transparent,
      builder: (context) {
        return FractionallySizedBox(
          heightFactor: 0.92,
          child: ClipRRect(
            borderRadius: const BorderRadius.vertical(top: Radius.circular(28)),
            child: Material(
              color: Theme.of(context).colorScheme.surface,
              child: ListenableBuilder(
                listenable: controller,
                builder: (context, _) {
                  return FileViewerSheet(
                    file: controller.openedFile,
                    loading: controller.fileReading,
                    saving: controller.fileSaving,
                    showReviewActions:
                        controller.openedFileMatchesPendingDiff &&
                            controller.canShowReviewActions,
                    isDiffMode: controller.openedFileDiff != null,
                    reviewDiff: controller.openedFileDiff,
                    pendingDiffs: controller.pendingDiffs,
                    reviewGroups: controller.reviewGroups,
                    activeReviewGroupId: controller.activeReviewGroupId,
                    activeReviewDiffId: controller.activeReviewDiffId,
                    isAutoAcceptMode: controller.isAutoAcceptMode,
                    shouldShowPermissionChoices:
                        controller.shouldShowPermissionChoices,
                    shouldShowReviewChoices:
                        controller.shouldShowReviewChoices &&
                            controller.openedFilePendingDiff != null &&
                            controller.currentReviewDiff != null &&
                            ((controller.openedFilePendingDiff!.id.isNotEmpty &&
                                    controller.openedFilePendingDiff!.id ==
                                        controller.currentReviewDiff!.id) ||
                                controller.openedFilePendingDiff!.path ==
                                    controller.currentReviewDiff!.path),
                    shouldShowPlanChoices: controller.shouldShowPlanChoices,
                    pendingPrompt: controller.pendingPrompt,
                    pendingInteraction: controller.pendingInteraction,
                    onSelectReviewGroup: controller.setActiveReviewGroup,
                    onSelectReviewDiff: controller.setActiveReviewDiff,
                    onOpenDiffList: () => _openDiff(context),
                    onAccept: () => controller.sendReviewDecision('accept'),
                    onRevert: () => controller.sendReviewDecision('revert'),
                    onRevise: () => controller.sendReviewDecision('revise'),
                    onUseAsContext: () =>
                        controller.continueWithCurrentFile('基于当前文件继续处理'),
                    onSaveFile: controller.requestFileWrite,
                    onSendFilePrompt: controller.continueWithCurrentFile,
                    onSubmitPrompt: controller.submitPromptOption,
                  );
                },
              ),
            ),
          ),
        );
      },
    );
  }

  Future<void> _openDiff(BuildContext context) async {
    controller.requestDiffPage();
    await showModalBottomSheet<void>(
      context: context,
      isScrollControlled: true,
      useSafeArea: true,
      showDragHandle: true,
      backgroundColor: Colors.transparent,
      builder: (context) {
        final diff = controller.canShowReviewActions
            ? (controller.reviewActionTargetDiff ??
                controller.currentDiffContext)
            : controller.currentDiffContext;
        return FractionallySizedBox(
          heightFactor: 0.88,
          child: ClipRRect(
            borderRadius: const BorderRadius.vertical(top: Radius.circular(28)),
            child: Material(
              color: Theme.of(context).colorScheme.surface,
              child: DiffViewerSheet(
                title: diff?.title ?? 'Diff',
                path: diff?.path ?? '',
                diff: diff?.diff ?? '',
                pendingDiffs: controller.pendingDiffs,
                reviewGroups: controller.reviewGroups,
                activeReviewGroupId: controller.activeReviewGroupId,
                activeDiffId: controller.activeReviewDiffId,
                showReviewActions: controller.canShowReviewActions,
                onSelectGroup: controller.setActiveReviewGroup,
                onSelectDiff: controller.setActiveReviewDiff,
                onAccept: () => controller.sendReviewDecision('accept'),
                onRevert: () => controller.sendReviewDecision('revert'),
                onRevise: () => controller.sendReviewDecision('revise'),
              ),
            ),
          ),
        );
      },
    );
  }

  Future<void> _openAttachment(
    BuildContext context,
    TimelineAttachment attachment,
  ) async {
    final path = attachment.path.trim();
    if (path.isEmpty) {
      ScaffoldMessenger.of(context)
        ..hideCurrentSnackBar()
        ..showSnackBar(const SnackBar(content: Text('附件缺少可打开的文件路径')));
      return;
    }
    controller.openFile(path);
    await _openFileViewer(context);
  }

  Future<void> _openRuntimeInfo(BuildContext context) async {
    if (controller.runtimeInfo == null) {
      controller.requestRuntimeInfo('context');
    }
    await showModalBottomSheet<void>(
      context: context,
      isScrollControlled: true,
      builder: (context) {
        final info = controller.runtimeInfo;
        return RuntimeInfoSheet(
          title: info?.title ?? '运行时信息',
          message: info?.message ?? '',
          items: info?.items ?? const [],
        );
      },
    );
  }

  Future<void> _openSkills(BuildContext context) async {
    await showGeneralDialog<void>(
      context: context,
      barrierDismissible: true,
      barrierLabel: '关闭 Skill 面板',
      barrierColor: Colors.black54,
      transitionDuration: const Duration(milliseconds: 220),
      pageBuilder: (context, animation, secondaryAnimation) {
        return SafeArea(
          child: Align(
            alignment: Alignment.centerRight,
            child: FractionallySizedBox(
              widthFactor: 0.92,
              child: ClipRRect(
                borderRadius:
                    const BorderRadius.horizontal(left: Radius.circular(28)),
                child: Material(
                  color: Theme.of(context).colorScheme.surface,
                  child: ListenableBuilder(
                    listenable: controller,
                    builder: (context, _) {
                      return SkillManagementSheet(
                        skills: controller.skills,
                        enabledSkillNames:
                            controller.sessionContext.enabledSkillNames,
                        syncStatus: controller.skillSyncStatus,
                        catalogMeta: controller.skillCatalogMeta,
                        onToggleEnabled: controller.toggleSkillEnabled,
                        onSave: controller.saveSkill,
                        onSync: controller.syncSkills,
                        onExecuteSkill: controller.executeSkill,
                        onGenerateSkill: (request) =>
                            controller.saveGeneratedSkill(request: request),
                        onReviseSkill: (skill, request) =>
                            controller.saveGeneratedSkill(
                          request: request,
                          base: skill,
                        ),
                      );
                    },
                  ),
                ),
              ),
            ),
          ),
        );
      },
      transitionBuilder: (context, animation, secondaryAnimation, child) {
        final curved =
            CurvedAnimation(parent: animation, curve: Curves.easeOutCubic);
        return SlideTransition(
          position: Tween<Offset>(
            begin: const Offset(1, 0),
            end: Offset.zero,
          ).animate(curved),
          child: FadeTransition(opacity: curved, child: child),
        );
      },
    );
  }

  Future<void> _openMemory(BuildContext context) async {
    await showModalBottomSheet<void>(
      context: context,
      isScrollControlled: true,
      useSafeArea: true,
      showDragHandle: true,
      backgroundColor: Colors.transparent,
      builder: (context) {
        return FractionallySizedBox(
          heightFactor: 0.92,
          child: ClipRRect(
            borderRadius: const BorderRadius.vertical(top: Radius.circular(28)),
            child: Material(
              color: Theme.of(context).colorScheme.surface,
              child: ListenableBuilder(
                listenable: controller,
                builder: (context, _) {
                  return MemoryManagementSheet(
                    items: controller.memoryItems,
                    syncStatus: controller.memorySyncStatus,
                    catalogMeta: controller.memoryCatalogMeta,
                    enabledMemoryIds:
                        controller.sessionContext.enabledMemoryIds,
                    onToggleEnabled: controller.toggleMemoryEnabled,
                    onSave: controller.saveMemory,
                    onSync: controller.syncMemories,
                    onReviseMemory: controller.reviseMemoryWithClaude,
                  );
                },
              ),
            ),
          ),
        );
      },
    );
  }

  Future<void> _openPermissions(BuildContext context) async {
    controller.requestPermissionRuleList();
    await showModalBottomSheet<void>(
      context: context,
      isScrollControlled: true,
      useSafeArea: true,
      showDragHandle: true,
      backgroundColor: Colors.transparent,
      builder: (context) {
        return FractionallySizedBox(
          heightFactor: 0.92,
          child: ClipRRect(
            borderRadius: const BorderRadius.vertical(top: Radius.circular(28)),
            child: Material(
              color: Theme.of(context).colorScheme.surface,
              child: ListenableBuilder(
                listenable: controller,
                builder: (context, _) {
                  return PermissionRuleManagementSheet(
                    sessionEnabled: controller.sessionPermissionRulesEnabled,
                    persistentEnabled:
                        controller.persistentPermissionRulesEnabled,
                    sessionRules: controller.sessionPermissionRules,
                    persistentRules: controller.persistentPermissionRules,
                    onSetSessionEnabled: (value) =>
                        controller.setPermissionRulesEnabled('session', value),
                    onSetPersistentEnabled: (value) => controller
                        .setPermissionRulesEnabled('persistent', value),
                    onToggleRule: controller.setPermissionRuleEnabled,
                    onDeleteRule: controller.deletePermissionRule,
                    onOpenDebugLog: () => _openDebugLog(context),
                  );
                },
              ),
            ),
          ),
        );
      },
    );
  }

  Future<void> _openDebugLog(BuildContext context) async {
    await Navigator.of(context).push<void>(
      MaterialPageRoute(
        builder: (context) => ListenableBuilder(
          listenable: controller,
          builder: (context, _) {
            return DebugLogViewer(logs: controller.debugLogs);
          },
        ),
      ),
    );
  }

  Future<void> _openRelaySecurity(BuildContext context) async {
    if (controller.canManageRelayDevices) {
      controller.requestRelayDeviceList();
    }
    await showModalBottomSheet<void>(
      context: context,
      isScrollControlled: true,
      useSafeArea: true,
      showDragHandle: true,
      backgroundColor: Colors.transparent,
      builder: (context) => ListenableBuilder(
        listenable: controller,
        builder: (context, _) => _RelaySecuritySheet(
          controller: controller,
          onRefresh: controller.requestRelayDeviceList,
          onRevoke: (device) => _confirmRelayDeviceRevoke(context, device),
          onRotate: () => _confirmRelayDeviceRotate(context),
        ),
      ),
    );
  }

  Future<void> _confirmRelayDeviceRevoke(
    BuildContext context,
    RelayTrustedDevice device,
  ) async {
    final confirmed = await showDialog<bool>(
      context: context,
      builder: (dialogContext) => AlertDialog(
        title: const Text('撤销 Relay 设备'),
        content: Text('撤销后，${device.displayTitle} 需要重新配对才能连接。'),
        actions: [
          TextButton(
            onPressed: () => Navigator.pop(dialogContext, false),
            child: const Text('取消'),
          ),
          FilledButton(
            onPressed: () => Navigator.pop(dialogContext, true),
            child: const Text('撤销'),
          ),
        ],
      ),
    );
    if (confirmed == true) {
      controller.revokeRelayDevice(device.deviceId);
    }
  }

  Future<void> _confirmRelayDeviceRotate(BuildContext context) async {
    final confirmed = await showDialog<bool>(
      context: context,
      builder: (dialogContext) => AlertDialog(
        title: const Text('全局轮换 Relay 身份'),
        content: const Text('轮换会撤销所有已绑定设备，并断开当前连接。之后每台手机都需要导入新的中继链接重新配对。'),
        actions: [
          TextButton(
            onPressed: () => Navigator.pop(dialogContext, false),
            child: const Text('取消'),
          ),
          FilledButton(
            onPressed: () => Navigator.pop(dialogContext, true),
            child: const Text('轮换'),
          ),
        ],
      ),
    );
    if (confirmed == true) {
      controller.rotateRelayDevices();
    }
  }

  Future<void> _openModelSwitcher(BuildContext context) async {
    final engine = controller.configuredAiEngine;
    if (engine == 'codex') {
      controller.requestCodexModelCatalog(force: true);
    }

    // 先打开模态框，显示加载状态
    await showModalBottomSheet<void>(
      context: context,
      isScrollControlled: true,
      useSafeArea: true,
      showDragHandle: true,
      backgroundColor: Colors.transparent,
      builder: (context) {
        return _ModelSwitcherSheet(
          controller: controller,
          engine: engine,
        );
      },
    );
  }

  Future<void> _openAdbDebug(BuildContext context) async {
    await controller.prepareAdbDebug();
    if (!context.mounted) {
      return;
    }
    await Navigator.of(context).push<void>(
      MaterialPageRoute(
        builder: (context) => AdbDebugPage(controller: controller),
      ),
    );
  }

  Future<void> _downloadFile(String path) async {
    final messenger = ScaffoldMessenger.of(context);
    final scaffoldContext = context;
    final fileName = _fileNameOf(path);
    messenger
      ..hideCurrentSnackBar()
      ..showSnackBar(SnackBar(
        content: Text(
            controller.activeTransportPath == ActiveTransportPath.relay
                ? '开始 Relay 加密下载：$path'
                : '开始下载：$path'),
      ));

    try {
      if (controller.activeTransportPath == ActiveTransportPath.relay) {
        final savedFile = await _downloadRelayFileToDisk(
          path: path,
          fileName: fileName,
          messenger: messenger,
        );
        if (!scaffoldContext.mounted || savedFile == null) {
          return;
        }
        _showSavedSnackBar(savedFile);
        return;
      }
      final bytes = await _fetchFileBytes(path);
      final selectedPath = await _pickSavePath(fileName, bytes);
      if (!scaffoldContext.mounted) {
        return;
      }
      if (selectedPath == null || selectedPath.trim().isEmpty) {
        messenger
          ..hideCurrentSnackBar()
          ..showSnackBar(const SnackBar(content: Text('已取消保存')));
        return;
      }

      final savedFile = await _writeDownloadedFile(selectedPath, bytes);
      if (!scaffoldContext.mounted) {
        return;
      }
      _showSavedSnackBar(savedFile);
    } catch (error) {
      if (!scaffoldContext.mounted) {
        return;
      }
      messenger
        ..hideCurrentSnackBar()
        ..showSnackBar(SnackBar(content: Text('下载失败：$error')));
    }
  }

  Future<File?> _downloadRelayFileToDisk({
    required String path,
    required String fileName,
    required ScaffoldMessengerState messenger,
  }) async {
    final selectedPath = await _pickRelaySavePath(fileName);
    if (selectedPath == null || selectedPath.trim().isEmpty) {
      messenger
        ..hideCurrentSnackBar()
        ..showSnackBar(const SnackBar(content: Text('已取消保存')));
      return null;
    }
    final targetFile = File(selectedPath);
    final cancelToken = RelayFileDownloadCancelToken();
    final parent = targetFile.parent;
    if (!await parent.exists()) {
      await parent.create(recursive: true);
    }
    void showProgressSnackBar(String text) {
      messenger
        ..hideCurrentSnackBar()
        ..showSnackBar(SnackBar(
          content: Text(text),
          action: SnackBarAction(
            label: '取消',
            onPressed: cancelToken.cancel,
          ),
        ));
    }

    final sink = targetFile.openWrite();
    try {
      showProgressSnackBar('Relay 加密下载中：准备传输');
      await controller.downloadRelayFile(
        path,
        onChunk: (chunk) async {
          sink.add(chunk);
          await sink.flush();
        },
        onProgress: (received, total) {
          if (!mounted || total == null || total <= 0) {
            return;
          }
          showProgressSnackBar(
            'Relay 加密下载中：${_formatBytes(received)} / ${_formatBytes(total)}',
          );
        },
        cancelToken: cancelToken,
      );
      await sink.close();
      return targetFile;
    } catch (error) {
      try {
        await sink.close();
      } catch (closeError) {
        Error.throwWithStackTrace(closeError, StackTrace.current);
      }
      if (cancelToken.isCancelled) {
        if (await targetFile.exists()) {
          await targetFile.delete();
        }
      }
      Error.throwWithStackTrace(error, StackTrace.current);
    }
  }

  Future<String?> _pickRelaySavePath(String fileName) async {
    if (_shouldUseSystemSaveDialog) {
      return FilePicker.platform.saveFile(
        dialogTitle: '保存文件',
        fileName: fileName,
      );
    }

    final directory = await getApplicationDocumentsDirectory();
    final downloadsDir = Directory('${directory.path}/downloads');
    if (!await downloadsDir.exists()) {
      await downloadsDir.create(recursive: true);
    }
    return '${downloadsDir.path}/$fileName';
  }

  Future<Uint8List> _fetchFileBytes(String path) async {
    final client = HttpClient();
    try {
      final request = await client.getUrl(
        controller.config.downloadUri(
          path,
          secureTransport: defaultSecureBackendTransport ? true : null,
        ),
      );
      final response = await request.close();
      if (response.statusCode != HttpStatus.ok) {
        throw HttpException('下载失败，状态码 ${response.statusCode}');
      }
      return await consolidateHttpClientResponseBytes(response);
    } finally {
      client.close(force: true);
    }
  }

  Future<String?> _pickSavePath(String fileName, List<int> bytes) async {
    if (_shouldUseSystemSaveDialog) {
      return FilePicker.platform.saveFile(
        dialogTitle: '保存文件',
        fileName: fileName,
        bytes: Uint8List.fromList(bytes),
      );
    }

    final directory = await getApplicationDocumentsDirectory();
    final downloadsDir = Directory('${directory.path}/downloads');
    if (!await downloadsDir.exists()) {
      await downloadsDir.create(recursive: true);
    }
    return '${downloadsDir.path}/$fileName';
  }

  Future<File> _writeDownloadedFile(String path, List<int> bytes) async {
    final targetFile = File(path);
    final parent = targetFile.parent;
    if (!await parent.exists()) {
      await parent.create(recursive: true);
    }
    await targetFile.writeAsBytes(bytes, flush: true);
    return targetFile;
  }

  bool get _shouldUseSystemSaveDialog {
    if (kIsWeb) {
      return false;
    }
    return Platform.isMacOS || Platform.isWindows || Platform.isLinux;
  }

  void _showSavedSnackBar(File savedFile) {
    final messenger = ScaffoldMessenger.of(context);
    final savedName = savedFile.path.split(Platform.pathSeparator).last;
    final savedLocation = _shouldUseSystemSaveDialog
        ? savedFile.path
        : '应用文档/downloads/$savedName';
    messenger
      ..hideCurrentSnackBar()
      ..showSnackBar(
        SnackBar(
          content: Text('已保存：$savedLocation'),
          action: SnackBarAction(
            label: '分享',
            onPressed: () => _shareDownloadedFile(savedFile),
          ),
        ),
      );
  }

  Future<void> _shareDownloadedFile(File file) async {
    final messenger = ScaffoldMessenger.of(context);
    try {
      final result = await SharePlus.instance.share(
        ShareParams(files: [XFile(file.path)]),
      );
      if (!mounted || result.status == ShareResultStatus.dismissed) {
        return;
      }
    } catch (error) {
      if (!mounted) {
        return;
      }
      messenger.showSnackBar(SnackBar(content: Text('分享失败：$error')));
    }
  }

  String _fileNameOf(String path) {
    final normalized = path.replaceAll('\\', '/').trim();
    if (normalized.isEmpty) {
      return 'download.bin';
    }
    final index = normalized.lastIndexOf('/');
    final fileName = index == -1 ? normalized : normalized.substring(index + 1);
    return fileName.isEmpty ? 'download.bin' : fileName;
  }

  String _formatBytes(int bytes) {
    if (bytes >= 1024 * 1024) {
      return '${(bytes / (1024 * 1024)).toStringAsFixed(1)} MB';
    }
    if (bytes >= 1024) {
      return '${(bytes / 1024).toStringAsFixed(1)} KB';
    }
    return '$bytes B';
  }

  Future<void> _openStatusDetails(BuildContext context) async {
    await showModalBottomSheet<void>(
      context: context,
      isScrollControlled: true,
      useSafeArea: true,
      showDragHandle: true,
      backgroundColor: Colors.transparent,
      builder: (context) {
        return FractionallySizedBox(
          heightFactor: 0.72,
          child: ClipRRect(
            borderRadius: const BorderRadius.vertical(top: Radius.circular(28)),
            child: Material(
              color: Theme.of(context).colorScheme.surface,
              child: ListenableBuilder(
                listenable: controller,
                builder: (context, _) {
                  return StatusDetailSheet(
                    sessionId: controller.selectedSessionId,
                    sessionTitle: controller.selectedSessionTitle,
                    connected: controller.connected,
                    awaitInput: controller.awaitInput,
                    permissionMode: controller.config.permissionMode,
                    engine: controller.configuredAiEngine,
                    currentPath: controller.currentDirectoryPath,
                    runtimeMeta: controller.currentMeta,
                    currentStep: controller.currentStep,
                    latestError: controller.latestError,
                    canResumeCurrentSession: controller.canResumeCurrentSession,
                    agentPhaseLabel: controller.agentPhaseLabel,
                    currentStepSummary: controller.currentStepSummary,
                    recentDiff: controller.recentDiffs.isNotEmpty
                        ? controller.recentDiffs.last
                        : null,
                    enabledSkillSummary: controller.enabledSkillSummary,
                    enabledMemorySummary: controller.enabledMemorySummary,
                  );
                },
              ),
            ),
          ),
        );
      },
    );
  }

  Future<void> _openLogs(BuildContext context) async {
    controller.requestTerminalHydration();
    controller.requestRuntimeProcessList();
    await showModalBottomSheet<void>(
      context: context,
      isScrollControlled: true,
      useSafeArea: true,
      showDragHandle: true,
      backgroundColor: Colors.transparent,
      builder: (context) {
        return FractionallySizedBox(
          heightFactor: 0.9,
          child: ClipRRect(
            borderRadius: const BorderRadius.vertical(top: Radius.circular(28)),
            child: Material(
              color: Theme.of(context).colorScheme.surface,
              child: ListenableBuilder(
                listenable: controller,
                builder: (context, _) {
                  return TerminalLogSheet(
                    executions: controller.terminalExecutions,
                    activeExecutionId: controller.activeTerminalExecutionId,
                    stdout: controller.activeTerminalStdout,
                    stderr: controller.activeTerminalStderr,
                    runtimeProcesses: controller.runtimeProcesses,
                    activeProcessPid: controller.activeRuntimeProcessPid,
                    processStdout: controller.activeRuntimeProcessStdout,
                    processStderr: controller.activeRuntimeProcessStderr,
                    processMessage: controller.activeRuntimeProcessMessage,
                    runtimeProcessListLoading:
                        controller.runtimeProcessListLoading,
                    runtimeProcessLogLoading:
                        controller.runtimeProcessLogLoading,
                    onSelectExecution: controller.setActiveTerminalExecution,
                    onSelectProcess: controller.setActiveRuntimeProcess,
                    onRefreshProcesses: controller.requestRuntimeProcessList,
                  );
                },
              ),
            ),
          ),
        );
      },
    );
  }
}

class _ModelSwitcherSheet extends StatefulWidget {
  final SessionController controller;
  final String engine;

  const _ModelSwitcherSheet({
    required this.controller,
    required this.engine,
  });

  @override
  State<_ModelSwitcherSheet> createState() => _ModelSwitcherSheetState();
}

class _ModelSwitcherSheetState extends State<_ModelSwitcherSheet> {
  late String selectedModel;
  late String selectedEffort;
  late _ModelSwitcherStep sheetStep;

  @override
  void initState() {
    super.initState();
    selectedModel = widget.controller.configuredAiModel;
    selectedEffort = widget.controller.configuredAiReasoningEffort;
    sheetStep = _ModelSwitcherStep.model;
  }

  @override
  Widget build(BuildContext context) {
    return ListenableBuilder(
      listenable: widget.controller,
      builder: (context, _) {
        final controller = widget.controller;
        final engine = widget.engine;
        final theme = Theme.of(context);
        final isCodex = engine == 'codex';
        final isClaude = engine == 'claude';
        final supportsModels = isClaude || isCodex;
        final selectedCodexDefault = isCodex && selectedModel.trim().isEmpty;
        final selectedCodexModelLabel = selectedCodexDefault
            ? 'Default'
            : controller.codexModelDisplayLabel(selectedModel);

        // 判断是否加载失败
        final modelOptions = isCodex
            ? <_ModelChoice>[
                const _ModelChoice(
                  value: 'default',
                  title: 'Default',
                  subtitle: '跟随 Codex config.toml 当前默认模型',
                ),
                ...controller.codexModelCatalog.map(
                  (entry) => _ModelChoice(
                    value: entry.model,
                    title: controller.codexModelDisplayLabel(
                      entry.model,
                    ),
                    subtitle: entry.description.isNotEmpty
                        ? entry.description
                        : (entry.isDefault ? 'Codex 当前默认模型' : 'Codex 原生模型'),
                  ),
                ),
              ]
            : (isClaude ? _claudeModelChoices : <_ModelChoice>[]);
        final hasSelectedPreset = modelOptions.any(
          (option) => isClaude
              ? isEquivalentClaudeModelSelection(
                  option.value,
                  selectedModel,
                )
              : option.value == selectedModel ||
                  (option.value == 'default' && selectedModel.isEmpty),
        );
        final selectedCatalogEntry =
            isCodex ? controller.codexModelCatalogEntry(selectedModel) : null;
        final effortOptions = isCodex
            ? controller.codexReasoningEffortOptionsForModel(
                selectedModel,
              )
            : const <CodexReasoningEffortOption>[];
        if (isCodex && effortOptions.isNotEmpty) {
          selectedEffort = controller.preferredCodexReasoningEffortForModel(
            selectedModel,
            fallback: selectedEffort,
          );
        }
        final showEffortStep =
            isCodex && sheetStep == _ModelSwitcherStep.effort;
        return FractionallySizedBox(
          heightFactor: 0.7,
          child: ClipRRect(
            borderRadius: const BorderRadius.vertical(top: Radius.circular(28)),
            child: Material(
              color: theme.colorScheme.surface,
              child: Padding(
                padding: EdgeInsets.fromLTRB(
                  16,
                  8,
                  16,
                  24 + MediaQuery.of(context).viewInsets.bottom,
                ),
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    Row(
                      children: [
                        if (showEffortStep)
                          IconButton(
                            onPressed: () {
                              setState(() {
                                sheetStep = _ModelSwitcherStep.model;
                              });
                            },
                            tooltip: '返回模型列表',
                            icon: const Icon(Icons.arrow_back_ios_new),
                          )
                        else
                          const SizedBox(width: 12),
                        Expanded(
                          child: Text(
                            showEffortStep ? '选择推理强度' : '选择模型',
                            style: theme.textTheme.titleLarge?.copyWith(
                              fontWeight: FontWeight.w900,
                            ),
                          ),
                        ),
                        if (isCodex)
                          Container(
                            padding: const EdgeInsets.symmetric(
                              horizontal: 10,
                              vertical: 4,
                            ),
                            decoration: BoxDecoration(
                              color: theme.colorScheme.surfaceContainerHigh,
                              borderRadius: BorderRadius.circular(999),
                            ),
                            child: Text(
                              showEffortStep ? '2 / 2' : '1 / 2',
                              style: theme.textTheme.labelMedium?.copyWith(
                                fontWeight: FontWeight.w800,
                              ),
                            ),
                          ),
                      ],
                    ),
                    const SizedBox(height: 6),
                    Text(
                      supportsModels
                          ? showEffortStep
                              ? '当前模型为 ${selectedModel.trim().isEmpty ? 'Default' : controller.codexModelDisplayLabel(selectedModel)}，选择后将用于下一次 Codex 启动。'
                              : '当前为 ${isCodex ? 'Codex' : 'Claude'} 模式，切换后的配置会用于下一次 AI 启动。'
                          : '当前模式暂不支持快捷切换模型。',
                      style: theme.textTheme.bodySmall?.copyWith(
                        color: theme.colorScheme.onSurfaceVariant,
                      ),
                    ),
                    const SizedBox(height: 14),
                    Expanded(
                      child: supportsModels
                          ? AnimatedSwitcher(
                              duration: const Duration(milliseconds: 220),
                              transitionBuilder: (child, animation) =>
                                  FadeTransition(
                                opacity: animation,
                                child: SlideTransition(
                                  position: Tween<Offset>(
                                    begin: const Offset(0.08, 0),
                                    end: Offset.zero,
                                  ).animate(animation),
                                  child: child,
                                ),
                              ),
                              child: showEffortStep
                                  ? SingleChildScrollView(
                                      key: const ValueKey(
                                        'model-switcher-effort',
                                      ),
                                      child: Column(
                                        crossAxisAlignment:
                                            CrossAxisAlignment.start,
                                        children: [
                                          Container(
                                            width: double.infinity,
                                            padding: const EdgeInsets.all(
                                              14,
                                            ),
                                            decoration: BoxDecoration(
                                              color: theme.colorScheme
                                                  .surfaceContainerLow,
                                              borderRadius:
                                                  BorderRadius.circular(
                                                16,
                                              ),
                                            ),
                                            child: Column(
                                              crossAxisAlignment:
                                                  CrossAxisAlignment.start,
                                              children: [
                                                Text(
                                                  selectedCodexModelLabel,
                                                  style: theme
                                                      .textTheme.titleMedium
                                                      ?.copyWith(
                                                    fontWeight: FontWeight.w800,
                                                  ),
                                                ),
                                                const SizedBox(
                                                  height: 6,
                                                ),
                                                Text(
                                                  selectedCatalogEntry
                                                              ?.description
                                                              .trim()
                                                              .isNotEmpty ==
                                                          true
                                                      ? selectedCatalogEntry!
                                                          .description
                                                      : selectedCodexDefault
                                                          ? '模型和推理强度将跟随 Codex config.toml'
                                                          : '当前选择的 Codex 模型',
                                                  style: theme
                                                      .textTheme.bodySmall
                                                      ?.copyWith(
                                                    color: theme.colorScheme
                                                        .onSurfaceVariant,
                                                  ),
                                                ),
                                              ],
                                            ),
                                          ),
                                          const SizedBox(height: 18),
                                          Text(
                                            '推理强度',
                                            style: theme.textTheme.titleSmall
                                                ?.copyWith(
                                              fontWeight: FontWeight.w800,
                                            ),
                                          ),
                                          const SizedBox(height: 10),
                                          if (selectedCatalogEntry != null &&
                                              effortOptions.isNotEmpty)
                                            Wrap(
                                              spacing: 8,
                                              runSpacing: 8,
                                              children: [
                                                for (final option
                                                    in effortOptions)
                                                  ChoiceChip(
                                                    label: Text(
                                                      option.reasoningEffort
                                                          .toUpperCase(),
                                                    ),
                                                    selected: selectedEffort ==
                                                        option.reasoningEffort,
                                                    onSelected: (_) {
                                                      setState(() {
                                                        selectedEffort = option
                                                            .reasoningEffort;
                                                      });
                                                    },
                                                  ),
                                              ],
                                            )
                                          else ...[
                                            Text(
                                              selectedCodexDefault
                                                  ? '模型和推理强度将跟随 Codex config.toml；MobileVC 不会下发 model_reasoning_effort 覆盖。'
                                                  : '当前保存的模型不在 Codex 原生目录中，因此这里只保留已保存强度，不展示额外原生选项。',
                                              style: theme.textTheme.bodySmall
                                                  ?.copyWith(
                                                color: theme.colorScheme
                                                    .onSurfaceVariant,
                                              ),
                                            ),
                                            if (selectedCodexDefault) ...[
                                              const SizedBox(height: 8),
                                              const ChoiceChip(
                                                label: Text('跟随 config.toml'),
                                                selected: true,
                                                onSelected: null,
                                              ),
                                            ] else if (selectedEffort
                                                .trim()
                                                .isNotEmpty) ...[
                                              const SizedBox(height: 8),
                                              ChoiceChip(
                                                label: Text(
                                                  selectedEffort.toUpperCase(),
                                                ),
                                                selected: true,
                                                onSelected: null,
                                              ),
                                            ],
                                          ],
                                        ],
                                      ),
                                    )
                                  : SingleChildScrollView(
                                      key: const ValueKey(
                                        'model-switcher-model',
                                      ),
                                      child: Column(
                                        crossAxisAlignment:
                                            CrossAxisAlignment.start,
                                        children: [
                                          if (isCodex &&
                                              controller
                                                  .codexModelCatalogLoading) ...[
                                            Row(
                                              children: [
                                                SizedBox(
                                                  width: 16,
                                                  height: 16,
                                                  child:
                                                      CircularProgressIndicator(
                                                    strokeWidth: 2,
                                                    color: theme
                                                        .colorScheme.primary,
                                                  ),
                                                ),
                                                const SizedBox(
                                                  width: 10,
                                                ),
                                                Expanded(
                                                  child: Text(
                                                    controller
                                                            .codexModelCatalogMessage
                                                            .trim()
                                                            .isNotEmpty
                                                        ? controller
                                                            .codexModelCatalogMessage
                                                        : 'Codex 原生模型目录同步中...',
                                                    style: theme
                                                        .textTheme.bodySmall
                                                        ?.copyWith(
                                                      color: theme.colorScheme
                                                          .onSurfaceVariant,
                                                    ),
                                                  ),
                                                ),
                                              ],
                                            ),
                                            const SizedBox(height: 14),
                                          ],
                                          Text(
                                            '模型',
                                            style: theme.textTheme.titleSmall
                                                ?.copyWith(
                                              fontWeight: FontWeight.w800,
                                            ),
                                          ),
                                          const SizedBox(height: 10),
                                          if (isCodex &&
                                              !controller
                                                  .codexModelCatalogLoading &&
                                              modelOptions.isEmpty) ...[
                                            Container(
                                              width: double.infinity,
                                              padding: const EdgeInsets.all(
                                                14,
                                              ),
                                              decoration: BoxDecoration(
                                                color: theme.colorScheme
                                                    .surfaceContainerLow,
                                                borderRadius:
                                                    BorderRadius.circular(16),
                                              ),
                                              child: Text(
                                                controller
                                                        .codexModelCatalogMessage
                                                        .trim()
                                                        .isNotEmpty
                                                    ? controller
                                                        .codexModelCatalogMessage
                                                    : '暂未拿到 Codex 原生模型目录。',
                                                style: theme.textTheme.bodySmall
                                                    ?.copyWith(
                                                  color: theme.colorScheme
                                                      .onSurfaceVariant,
                                                ),
                                              ),
                                            ),
                                            const SizedBox(height: 10),
                                          ],
                                          Wrap(
                                            spacing: 10,
                                            runSpacing: 10,
                                            children: [
                                              if (!hasSelectedPreset &&
                                                  selectedModel
                                                      .trim()
                                                      .isNotEmpty)
                                                _ModelChoiceCard(
                                                  title: controller
                                                      .aiModelSheetSummary(
                                                    engine,
                                                    selectedModel,
                                                    selectedEffort,
                                                  ),
                                                  subtitle: isCodex
                                                      ? '当前为已保存配置；未出现在 Codex 原生目录中'
                                                      : '当前为已保存配置的自定义模型',
                                                  selected: true,
                                                  onTap: () {
                                                    setState(() {
                                                      if (isCodex) {
                                                        sheetStep =
                                                            _ModelSwitcherStep
                                                                .effort;
                                                      }
                                                    });
                                                  },
                                                ),
                                              for (final option in modelOptions)
                                                _ModelChoiceCard(
                                                  title: option.title,
                                                  subtitle: option.subtitle,
                                                  selected: isClaude
                                                      ? isEquivalentClaudeModelSelection(
                                                          selectedModel,
                                                          option.value,
                                                        )
                                                      : option.value ==
                                                              selectedModel ||
                                                          (option.value ==
                                                                  'default' &&
                                                              selectedModel
                                                                  .isEmpty),
                                                  onTap: () {
                                                    setState(() {
                                                      final isDefaultOption =
                                                          option.value ==
                                                              'default';
                                                      selectedModel =
                                                          isDefaultOption
                                                              ? ''
                                                              : option.value;
                                                      if (isCodex) {
                                                        selectedEffort =
                                                            isDefaultOption
                                                                ? ''
                                                                : controller
                                                                    .preferredCodexReasoningEffortForModel(
                                                                    option
                                                                        .value,
                                                                    fallback:
                                                                        selectedEffort,
                                                                  );
                                                        sheetStep =
                                                            _ModelSwitcherStep
                                                                .effort;
                                                      }
                                                    });
                                                  },
                                                ),
                                            ],
                                          ),
                                        ],
                                      ),
                                    ),
                            )
                          : Center(
                              child: Text(
                                '当前引擎暂不支持模型快捷切换。',
                                style: theme.textTheme.bodyMedium,
                              ),
                            ),
                    ),
                    const SizedBox(height: 16),
                    SizedBox(
                      width: double.infinity,
                      child: FilledButton.icon(
                        onPressed: !supportsModels
                            ? null
                            : showEffortStep
                                ? () async {
                                    await controller.updateAiModelSelection(
                                      model: selectedModel,
                                      reasoningEffort: selectedEffort,
                                      engine: engine,
                                    );
                                    if (context.mounted) {
                                      Navigator.of(context).pop();
                                    }
                                  }
                                : isCodex
                                    ? () {
                                        setState(() {
                                          selectedEffort = controller
                                              .preferredCodexReasoningEffortForModel(
                                            selectedModel,
                                            fallback: selectedEffort,
                                          );
                                          sheetStep = _ModelSwitcherStep.effort;
                                        });
                                      }
                                    : () async {
                                        await controller.updateAiModelSelection(
                                          model: selectedModel,
                                          reasoningEffort: selectedEffort,
                                          engine: engine,
                                        );
                                        if (context.mounted) {
                                          Navigator.of(context).pop();
                                        }
                                      },
                        icon: Icon(
                          showEffortStep
                              ? Icons.model_training_outlined
                              : Icons.arrow_forward_rounded,
                        ),
                        label: Text(
                          showEffortStep
                              ? '应用 ${controller.aiModelSheetSummary(engine, selectedModel, selectedEffort)}'
                              : isCodex
                                  ? '继续选择强度'
                                  : '应用 ${selectedModel.toUpperCase()}',
                        ),
                      ),
                    ),
                  ],
                ),
              ),
            ),
          ),
        );
      },
    );
  }
}

class _ContextSelectionBar extends StatelessWidget {
  const _ContextSelectionBar({required this.controller});

  final SessionController controller;

  @override
  Widget build(BuildContext context) {
    return const SizedBox.shrink();
  }
}

enum _ModelSwitcherStep {
  model,
  effort,
}

class _ModelChoice {
  const _ModelChoice({
    required this.value,
    required this.title,
    required this.subtitle,
  });

  final String value;
  final String title;
  final String subtitle;
}

const List<_ModelChoice> _claudeModelChoices = <_ModelChoice>[
  _ModelChoice(
    value: 'default',
    title: 'Default',
    subtitle: '跟随 Claude Code 当前默认模型',
  ),
  _ModelChoice(
    value: 'claude-sonnet-4-5',
    title: 'Sonnet 4.5',
    subtitle: '当前稳定主力模型，适合日常开发与多数任务',
  ),
  _ModelChoice(
    value: 'claude-sonnet-4-6',
    title: 'Sonnet 4.6',
    subtitle: '较新的 Sonnet，适合代码理解、编辑与执行',
  ),
  _ModelChoice(
    value: 'claude-opus-4-6',
    title: 'Opus 4.6',
    subtitle: '更强推理，适合复杂任务、重审阅与深度分析',
  ),
  _ModelChoice(
    value: 'claude-haiku-4-5',
    title: 'Haiku 4.5',
    subtitle: '更轻更快，适合轻量任务与快速往返',
  ),
];

class _ModelChoiceCard extends StatelessWidget {
  static const selectedIndicatorKey = ValueKey<String>(
    'model-choice-selected-indicator',
  );

  const _ModelChoiceCard({
    required this.title,
    required this.subtitle,
    required this.selected,
    required this.onTap,
  });

  final String title;
  final String subtitle;
  final bool selected;
  final VoidCallback onTap;

  @override
  Widget build(BuildContext context) {
    final scheme = Theme.of(context).colorScheme;
    return SizedBox(
      width: 160,
      child: Material(
        color: Colors.transparent,
        child: InkWell(
          onTap: onTap,
          borderRadius: BorderRadius.circular(22),
          child: Ink(
            padding: const EdgeInsets.fromLTRB(14, 14, 14, 12),
            decoration: BoxDecoration(
              gradient: LinearGradient(
                colors: [
                  selected
                      ? scheme.primaryContainer.withValues(alpha: 0.92)
                      : scheme.surface,
                  selected
                      ? scheme.primary.withValues(alpha: 0.14)
                      : scheme.surface,
                ],
                begin: Alignment.topLeft,
                end: Alignment.bottomRight,
              ),
              borderRadius: BorderRadius.circular(22),
              border: Border.all(
                color: selected
                    ? scheme.primary
                    : scheme.outlineVariant.withValues(alpha: 0.55),
                width: selected ? 2 : 1,
              ),
              boxShadow: selected
                  ? [
                      BoxShadow(
                        color: scheme.primary.withValues(alpha: 0.18),
                        blurRadius: 18,
                        offset: const Offset(0, 8),
                      ),
                    ]
                  : null,
            ),
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Row(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    Expanded(
                      child: Text(
                        title,
                        style: Theme.of(context).textTheme.titleSmall?.copyWith(
                              fontWeight: FontWeight.w800,
                              color:
                                  selected ? scheme.onPrimaryContainer : null,
                            ),
                      ),
                    ),
                    if (selected)
                      Padding(
                        padding: const EdgeInsets.only(left: 8),
                        child: Icon(
                          Icons.check_circle_rounded,
                          key: selectedIndicatorKey,
                          size: 20,
                          color: scheme.primary,
                        ),
                      ),
                  ],
                ),
                const SizedBox(height: 6),
                Text(
                  subtitle,
                  style: Theme.of(context).textTheme.bodySmall?.copyWith(
                        color: selected
                            ? scheme.onPrimaryContainer.withValues(alpha: 0.82)
                            : scheme.onSurfaceVariant,
                        height: 1.35,
                      ),
                ),
              ],
            ),
          ),
        ),
      ),
    );
  }
}

class _LandingBrand extends StatelessWidget {
  const _LandingBrand();

  @override
  Widget build(BuildContext context) {
    final scheme = Theme.of(context).colorScheme;
    return Padding(
      padding: const EdgeInsets.symmetric(horizontal: 24),
      child: Column(
        mainAxisSize: MainAxisSize.min,
        children: [
          Image.asset(
            'lib/logo-2.png',
            width: 108,
            height: 108,
            fit: BoxFit.contain,
          ),
          const SizedBox(height: 18),
          Text(
            'MobileVC',
            style: Theme.of(context).textTheme.headlineMedium?.copyWith(
                  fontWeight: FontWeight.w900,
                  letterSpacing: 0,
                ),
          ),
          const SizedBox(height: 8),
          Text(
            '连接后即可开始对话、查看文件和远程调试。',
            style: Theme.of(context)
                .textTheme
                .bodyMedium
                ?.copyWith(color: scheme.onSurfaceVariant, height: 1.45),
            textAlign: TextAlign.center,
          ),
        ],
      ),
    );
  }
}

class _SessionObservationBanner extends StatelessWidget {
  const _SessionObservationBanner({required this.controller});

  final SessionController controller;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final scheme = Theme.of(context).colorScheme;
    final isLight = theme.brightness == Brightness.light;
    return Container(
      width: double.infinity,
      margin: const EdgeInsets.fromLTRB(12, 8, 12, 0),
      padding: const EdgeInsets.all(12),
      decoration: BoxDecoration(
        gradient: LinearGradient(
          colors: [
            scheme.primaryContainer.withValues(alpha: isLight ? 0.82 : 0.72),
            Color.alphaBlend(
              scheme.secondary.withValues(alpha: isLight ? 0.08 : 0.12),
              scheme.primaryContainer.withValues(alpha: isLight ? 0.74 : 0.64),
            ),
          ],
        ),
        borderRadius: BorderRadius.circular(16),
        border: Border.all(
          color: scheme.primary.withValues(alpha: isLight ? 0.22 : 0.18),
        ),
        boxShadow: [
          if (isLight)
            BoxShadow(
              color: scheme.primary.withValues(alpha: 0.08),
              blurRadius: 16,
              offset: const Offset(0, 6),
            ),
        ],
      ),
      child: Row(
        crossAxisAlignment: CrossAxisAlignment.center,
        children: [
          Icon(
            controller.canContinueSameSession
                ? Icons.visibility_outlined
                : Icons.mobile_friendly_outlined,
            color: scheme.primary,
          ),
          const SizedBox(width: 10),
          Expanded(
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Text(
                  controller.sessionObservationTitle,
                  style: Theme.of(context).textTheme.titleSmall?.copyWith(
                        fontWeight: FontWeight.w800,
                        color: scheme.onPrimaryContainer,
                      ),
                ),
                const SizedBox(height: 2),
                Text(
                  controller.sessionObservationDetail,
                  style: Theme.of(context).textTheme.bodySmall?.copyWith(
                        color:
                            scheme.onPrimaryContainer.withValues(alpha: 0.82),
                        height: 1.35,
                      ),
                ),
              ],
            ),
          ),
          if (controller.canContinueSameSession) ...[
            const SizedBox(width: 10),
            FilledButton.tonal(
              onPressed: controller.continueSameSessionFromPhone,
              child: const Text('继续同会话'),
            ),
          ],
        ],
      ),
    );
  }
}

class _RelayPairingSummary extends StatelessWidget {
  const _RelayPairingSummary({required this.config});

  final AppConfig config;

  @override
  Widget build(BuildContext context) {
    final scheme = Theme.of(context).colorScheme;
    final sessionId = config.relaySessionId.trim();
    final relayUrl = config.relayUrl.trim();
    final expiresAt = config.relayPairingExpiresAt;
    return Material(
      color: scheme.surfaceContainerHighest.withValues(alpha: 0.42),
      borderRadius: BorderRadius.circular(16),
      child: Padding(
        padding: const EdgeInsets.all(12),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Row(
              children: [
                Icon(Icons.hub_outlined, color: scheme.primary),
                const SizedBox(width: 8),
                Expanded(
                  child: Text(
                    'Relay 中继已导入',
                    style: Theme.of(context).textTheme.titleSmall?.copyWith(
                          fontWeight: FontWeight.w800,
                        ),
                  ),
                ),
              ],
            ),
            const SizedBox(height: 10),
            _RelaySummaryLine(label: 'Relay', value: relayUrl),
            if (sessionId.isNotEmpty)
              _RelaySummaryLine(label: 'Session', value: sessionId),
            if (expiresAt > 0)
              _RelaySummaryLine(
                label: '有效期',
                value: _formatRelayPairingExpiry(expiresAt),
              ),
            _RelaySummaryLine(
              label: 'E2EE',
              value: config.relayCapabilities?.requiresE2EE == true
                  ? '需要端到端加密'
                  : '未声明',
            ),
          ],
        ),
      ),
    );
  }
}

class _RelaySummaryLine extends StatelessWidget {
  const _RelaySummaryLine({required this.label, required this.value});

  final String label;
  final String value;

  @override
  Widget build(BuildContext context) {
    final scheme = Theme.of(context).colorScheme;
    return Padding(
      padding: const EdgeInsets.only(top: 4),
      child: Row(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          SizedBox(
            width: 64,
            child: Text(
              label,
              style: Theme.of(context).textTheme.bodySmall?.copyWith(
                    color: scheme.onSurfaceVariant,
                  ),
            ),
          ),
          Expanded(
            child: Text(
              value,
              style: Theme.of(context).textTheme.bodySmall?.copyWith(
                fontFeatures: const [FontFeature.tabularFigures()],
                height: 1.35,
              ),
            ),
          ),
        ],
      ),
    );
  }
}

class _RelaySecuritySheet extends StatelessWidget {
  const _RelaySecuritySheet({
    required this.controller,
    required this.onRefresh,
    required this.onRevoke,
    required this.onRotate,
  });

  final SessionController controller;
  final VoidCallback onRefresh;
  final ValueChanged<RelayTrustedDevice> onRevoke;
  final VoidCallback onRotate;

  @override
  Widget build(BuildContext context) {
    final scheme = Theme.of(context).colorScheme;
    final devices = controller.relayDevices;
    return DraggableScrollableSheet(
      initialChildSize: 0.72,
      minChildSize: 0.42,
      maxChildSize: 0.92,
      expand: false,
      builder: (context, scrollController) {
        return DecoratedBox(
          decoration: BoxDecoration(
            color: scheme.surface,
            borderRadius: const BorderRadius.vertical(top: Radius.circular(24)),
          ),
          child: ListView(
            controller: scrollController,
            padding: const EdgeInsets.fromLTRB(16, 10, 16, 24),
            children: [
              Row(
                children: [
                  Expanded(
                    child: Text(
                      'Relay 安全设备',
                      style: Theme.of(context).textTheme.titleLarge?.copyWith(
                            fontWeight: FontWeight.w800,
                          ),
                    ),
                  ),
                  IconButton(
                    onPressed:
                        controller.canManageRelayDevices ? onRefresh : null,
                    icon: controller.relayDeviceListLoading
                        ? const SizedBox.square(
                            dimension: 18,
                            child: CircularProgressIndicator(strokeWidth: 2),
                          )
                        : const Icon(Icons.refresh),
                    tooltip: '刷新',
                  ),
                ],
              ),
              const SizedBox(height: 8),
              _RelaySecurityStatus(controller: controller),
              if (controller.relayDeviceStatus.trim().isNotEmpty) ...[
                const SizedBox(height: 10),
                Text(
                  controller.relayDeviceStatus,
                  style: Theme.of(context).textTheme.bodySmall?.copyWith(
                        color: scheme.onSurfaceVariant,
                      ),
                ),
              ],
              const SizedBox(height: 14),
              SizedBox(
                width: double.infinity,
                child: FilledButton.tonalIcon(
                  onPressed: controller.canManageRelayDevices ? onRotate : null,
                  icon: const Icon(Icons.restart_alt),
                  label: const Text('全局轮换'),
                ),
              ),
              const SizedBox(height: 14),
              if (devices.isEmpty && !controller.relayDeviceListLoading)
                _EmptyRelayDeviceState(
                    canManage: controller.canManageRelayDevices)
              else
                for (final device in devices) ...[
                  _RelayDeviceTile(
                    device: device,
                    onRevoke: device.revoked || device.currentDevice
                        ? null
                        : () => onRevoke(device),
                  ),
                  const SizedBox(height: 10),
                ],
            ],
          ),
        );
      },
    );
  }
}

class _RelaySecurityStatus extends StatelessWidget {
  const _RelaySecurityStatus({required this.controller});

  final SessionController controller;

  @override
  Widget build(BuildContext context) {
    final scheme = Theme.of(context).colorScheme;
    return FutureBuilder<RelaySecurityState>(
      future: controller.relaySecurityState(),
      builder: (context, snapshot) {
        final state = snapshot.data ??
            const RelaySecurityState(
              mode: RelaySecurityMode.relayNotVerified,
              title: 'Relay 未验证',
              detail: '正在读取 Relay 安全状态。',
              canShowVerified: false,
            );
        final verified = state.canShowVerified;
        final blocking = state.isBlocking;
        final color = verified
            ? scheme.primary
            : blocking
                ? scheme.error
                : scheme.tertiary;
        final background = verified
            ? scheme.primaryContainer.withValues(alpha: 0.62)
            : blocking
                ? scheme.errorContainer.withValues(alpha: 0.42)
                : scheme.tertiaryContainer.withValues(alpha: 0.46);
        return Container(
          padding: const EdgeInsets.all(12),
          decoration: BoxDecoration(
            color: background,
            borderRadius: BorderRadius.circular(16),
            border: Border.all(color: color.withValues(alpha: 0.18)),
          ),
          child: Row(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Icon(
                verified
                    ? Icons.verified_user_outlined
                    : blocking
                        ? Icons.gpp_bad_outlined
                        : Icons.shield_outlined,
                color: color,
              ),
              const SizedBox(width: 10),
              Expanded(
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    Text(
                      state.title,
                      style: Theme.of(context).textTheme.bodyMedium?.copyWith(
                            fontWeight: FontWeight.w800,
                            color: verified
                                ? scheme.onPrimaryContainer
                                : blocking
                                    ? scheme.onErrorContainer
                                    : scheme.onTertiaryContainer,
                          ),
                    ),
                    const SizedBox(height: 4),
                    Text(
                      state.detail,
                      style: Theme.of(context).textTheme.bodySmall?.copyWith(
                            color: verified
                                ? scheme.onPrimaryContainer
                                : blocking
                                    ? scheme.onErrorContainer
                                    : scheme.onTertiaryContainer,
                          ),
                    ),
                    if (state.shortFingerprint.trim().isNotEmpty) ...[
                      const SizedBox(height: 6),
                      Text(
                        '节点指纹：${state.shortFingerprint}',
                        style: Theme.of(context).textTheme.labelSmall?.copyWith(
                          color: verified
                              ? scheme.onPrimaryContainer
                              : blocking
                                  ? scheme.onErrorContainer
                                  : scheme.onTertiaryContainer,
                          fontFeatures: const [FontFeature.tabularFigures()],
                        ),
                      ),
                    ],
                  ],
                ),
              ),
            ],
          ),
        );
      },
    );
  }
}

class _EmptyRelayDeviceState extends StatelessWidget {
  const _EmptyRelayDeviceState({required this.canManage});

  final bool canManage;

  @override
  Widget build(BuildContext context) {
    final scheme = Theme.of(context).colorScheme;
    return Container(
      padding: const EdgeInsets.all(16),
      decoration: BoxDecoration(
        color: scheme.surfaceContainerHighest.withValues(alpha: 0.42),
        borderRadius: BorderRadius.circular(16),
      ),
      child: Text(
        canManage ? '还没有绑定设备' : '当前连接不可管理 Relay 设备',
        style: Theme.of(context).textTheme.bodyMedium?.copyWith(
              color: scheme.onSurfaceVariant,
            ),
      ),
    );
  }
}

class _RelayDeviceTile extends StatelessWidget {
  const _RelayDeviceTile({
    required this.device,
    required this.onRevoke,
  });

  final RelayTrustedDevice device;
  final VoidCallback? onRevoke;

  @override
  Widget build(BuildContext context) {
    final scheme = Theme.of(context).colorScheme;
    final status = device.revoked
        ? '已撤销'
        : device.currentDevice
            ? '当前设备'
            : device.connected
                ? '在线'
                : '未连接';
    final statusColor = device.revoked
        ? scheme.error
        : device.connected || device.currentDevice
            ? const Color(0xFF15803D)
            : scheme.onSurfaceVariant;
    return Material(
      color: scheme.surfaceContainerHighest.withValues(alpha: 0.34),
      borderRadius: BorderRadius.circular(16),
      child: Padding(
        padding: const EdgeInsets.all(12),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Row(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Expanded(
                  child: Text(
                    device.displayTitle,
                    style: Theme.of(context).textTheme.titleSmall?.copyWith(
                          fontWeight: FontWeight.w800,
                        ),
                  ),
                ),
                const SizedBox(width: 8),
                Text(
                  status,
                  style: Theme.of(context).textTheme.labelMedium?.copyWith(
                        color: statusColor,
                        fontWeight: FontWeight.w800,
                      ),
                ),
              ],
            ),
            const SizedBox(height: 8),
            Text(
              _shortFingerprint(device.fingerprintHex),
              style: Theme.of(context).textTheme.bodySmall?.copyWith(
                color: scheme.onSurfaceVariant,
                fontFeatures: const [FontFeature.tabularFigures()],
              ),
            ),
            const SizedBox(height: 6),
            Text(
              '最后连接：${_formatRelayDeviceTime(device.lastSeenAt)}',
              style: Theme.of(context).textTheme.bodySmall?.copyWith(
                    color: scheme.onSurfaceVariant,
                  ),
            ),
            if (device.revokedAt != null) ...[
              const SizedBox(height: 4),
              Text(
                '撤销时间：${_formatRelayDeviceTime(device.revokedAt)}',
                style: Theme.of(context).textTheme.bodySmall?.copyWith(
                      color: scheme.error,
                    ),
              ),
            ],
            if (onRevoke != null) ...[
              const SizedBox(height: 10),
              Align(
                alignment: Alignment.centerRight,
                child: FilledButton.tonalIcon(
                  onPressed: onRevoke,
                  icon: const Icon(Icons.block),
                  label: const Text('撤销'),
                ),
              ),
            ],
          ],
        ),
      ),
    );
  }
}

class _ConnectionDot extends StatelessWidget {
  const _ConnectionDot({required this.connected, required this.label});

  final bool connected;
  final String label;

  @override
  Widget build(BuildContext context) {
    final scheme = Theme.of(context).colorScheme;
    final color = connected ? const Color(0xFF22C55E) : scheme.outline;
    final normalizedLabel = label.trim();
    final compactLabel = _compactTransportLabel(normalizedLabel);
    final indicator = Container(
      padding: const EdgeInsets.symmetric(horizontal: 9, vertical: 6),
      decoration: BoxDecoration(
        color: connected
            ? color.withValues(alpha: 0.10)
            : scheme.surfaceContainerHigh.withValues(alpha: 0.72),
        borderRadius: BorderRadius.circular(999),
        border: Border.all(
          color: connected
              ? color.withValues(alpha: 0.24)
              : scheme.outlineVariant.withValues(alpha: 0.42),
        ),
      ),
      child: Row(
        mainAxisSize: MainAxisSize.min,
        children: [
          Container(
            width: 9,
            height: 9,
            decoration: BoxDecoration(
              color: color,
              shape: BoxShape.circle,
              boxShadow: [
                BoxShadow(
                  color: color.withValues(alpha: 0.35),
                  blurRadius: 8,
                  spreadRadius: 1,
                ),
              ],
            ),
          ),
          if (connected && compactLabel.isNotEmpty) ...[
            const SizedBox(width: 5),
            Text(
              compactLabel,
              key: const ValueKey('connection-transport-label'),
              style: Theme.of(context).textTheme.labelSmall?.copyWith(
                    color: scheme.onSurfaceVariant,
                    fontWeight: FontWeight.w800,
                  ),
            ),
          ],
        ],
      ),
    );
    if (!connected || normalizedLabel.isEmpty) {
      return indicator;
    }
    return Tooltip(
      message: normalizedLabel,
      child: indicator,
    );
  }
}

String _compactTransportLabel(String label) {
  return switch (label.toLowerCase()) {
    'lan' => 'L',
    'relay' => 'R',
    _ => label,
  };
}

String _iceHostLiteral(String rawHost) {
  final endpoint = AppConnectionEndpoint.parse(rawHost);
  final host = endpoint.host.trim();
  if (host.startsWith('[') && host.endsWith(']')) {
    return host;
  }
  return host.contains(':') ? '[$host]' : host;
}

String _portForHostInput(String rawHost, String rawPort) {
  final uri = Uri.tryParse(rawHost);
  if (uri == null ||
      uri.host.trim().isEmpty ||
      secureTransportFromScheme(uri.scheme) == null) {
    return rawPort.trim();
  }
  if (uri.hasPort && uri.port > 0) {
    return uri.port.toString();
  }
  return rawPort.trim();
}

class _MeasuredSize extends SingleChildRenderObjectWidget {
  const _MeasuredSize({
    required super.child,
    required this.onChanged,
  });

  final ValueChanged<Size> onChanged;

  @override
  RenderObject createRenderObject(BuildContext context) {
    return _MeasuredSizeRenderObject(onChanged);
  }

  @override
  void updateRenderObject(
    BuildContext context,
    covariant _MeasuredSizeRenderObject renderObject,
  ) {
    renderObject.onChanged = onChanged;
  }
}

class _MeasuredSizeRenderObject extends RenderProxyBox {
  _MeasuredSizeRenderObject(this.onChanged);

  ValueChanged<Size> onChanged;
  Size? _lastSize;

  @override
  void performLayout() {
    super.performLayout();
    final currentSize = size;
    if (currentSize == _lastSize) {
      return;
    }
    _lastSize = currentSize;
    WidgetsBinding.instance.addPostFrameCallback((_) {
      onChanged(currentSize);
    });
  }
}

class _TipSection extends StatelessWidget {
  const _TipSection({required this.children});

  final List<_TipItem> children;

  @override
  Widget build(BuildContext context) {
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: children,
    );
  }
}

class _TipItem extends StatelessWidget {
  const _TipItem({
    required this.number,
    required this.title,
    required this.body,
  });

  final String number;
  final String title;
  final String body;

  @override
  Widget build(BuildContext context) {
    final cs = Theme.of(context).colorScheme;
    return Padding(
      padding: const EdgeInsets.only(bottom: 16),
      child: Row(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Container(
            width: 22,
            height: 22,
            alignment: Alignment.center,
            decoration: BoxDecoration(
              color: cs.primary.withValues(alpha: 0.12),
              borderRadius: BorderRadius.circular(6),
            ),
            child: Text(
              number,
              style: TextStyle(
                fontSize: 12,
                fontWeight: FontWeight.w700,
                color: cs.primary,
              ),
            ),
          ),
          const SizedBox(width: 10),
          Expanded(
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Text(
                  title,
                  style: const TextStyle(
                    fontSize: 14,
                    fontWeight: FontWeight.w600,
                  ),
                ),
                const SizedBox(height: 4),
                Text(
                  body,
                  style: TextStyle(
                    fontSize: 13,
                    height: 1.55,
                    color: cs.onSurface.withValues(alpha: 0.72),
                  ),
                ),
              ],
            ),
          ),
        ],
      ),
    );
  }
}

String _shortFingerprint(String value) {
  final normalized = value.trim();
  if (normalized.isEmpty) {
    return 'fingerprint unavailable';
  }
  final prefix = normalized.length <= 16
      ? normalized
      : '${normalized.substring(0, 8)} ${normalized.substring(8, 16)}';
  return 'fp $prefix';
}

String _formatRelayPairingExpiry(int unixSeconds) {
  final value = DateTime.fromMillisecondsSinceEpoch(
    unixSeconds * 1000,
    isUtc: true,
  ).toLocal();
  return _formatRelayDeviceTime(value);
}

String _formatRelayDeviceTime(DateTime? value) {
  if (value == null) {
    return '-';
  }
  final local = value.toLocal();
  String two(int input) => input.toString().padLeft(2, '0');
  return '${local.year}-${two(local.month)}-${two(local.day)} '
      '${two(local.hour)}:${two(local.minute)}';
}
