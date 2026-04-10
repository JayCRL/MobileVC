import 'dart:io';

import 'package:file_picker/file_picker.dart';
import 'package:flutter/foundation.dart';
import 'package:flutter/material.dart';
import 'package:path_provider/path_provider.dart';
import 'package:share_plus/share_plus.dart';

import '../../core/config/app_config.dart';
import '../../data/models/session_models.dart';
import '../../features/adb/adb_debug_page.dart';
import '../../features/chat/chat_timeline.dart';
import '../../features/chat/command_input_bar.dart';
import '../../features/diff/diff_viewer_sheet.dart';
import '../../features/files/file_browser_sheet.dart';
import '../../features/files/file_viewer_sheet.dart';
import '../../features/memory/memory_management_sheet.dart';
import '../../features/permissions/permission_rule_management_sheet.dart';
import '../../features/runtime_info/runtime_info_sheet.dart';
import '../../features/skills/skill_management_sheet.dart';
import '../../features/status/status_detail_sheet.dart';
import '../../features/status/terminal_log_sheet.dart';
import 'activity_runner_bar.dart';
import 'claude_model_utils.dart';
import 'connection_scan_sheet.dart';
import 'session_controller.dart';
import 'session_list_sheet.dart';

class SessionHomePage extends StatefulWidget {
  const SessionHomePage({
    super.key,
    required this.controller,
  });

  final SessionController controller;

  @override
  State<SessionHomePage> createState() => _SessionHomePageState();
}

class _SessionHomePageState extends State<SessionHomePage> {
  final GlobalKey<ScaffoldState> _scaffoldKey = GlobalKey<ScaffoldState>();

  SessionController get controller => widget.controller;

  @override
  Widget build(BuildContext context) {
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
      resizeToAvoidBottomInset: true,
      appBar: AppBar(
        leading: IconButton(
          onPressed: _openFileDrawer,
          icon: const Icon(Icons.folder_outlined),
          tooltip: '文件树',
        ),
        titleSpacing: 0,
        title: Row(
          children: [
            Expanded(
              child: Text(
                controller.shouldShowSessionSurface
                    ? controller.selectedSessionTitle
                    : 'MobileVC',
                maxLines: 1,
                overflow: TextOverflow.ellipsis,
                style: Theme.of(context).textTheme.titleMedium?.copyWith(
                      fontWeight: FontWeight.w700,
                    ),
              ),
            ),
            const SizedBox(width: 8),
            _ConnectionDot(connected: controller.connected),
          ],
        ),
        actions: [
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
              isLabelVisible:
                  controller.adbStreaming || controller.adbWebRtcStarting,
              child: const Icon(Icons.phone_android_outlined),
            ),
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
      body: controller.shouldShowSessionSurface
          ? GestureDetector(
              behavior: HitTestBehavior.translucent,
              onTap: () => FocusManager.instance.primaryFocus?.unfocus(),
              child: Column(
                children: [
                  AnimatedBuilder(
                    animation: Listenable.merge([controller]),
                    builder: (context, _) {
                      return ActivityRunnerBar(
                        visible: controller.activityBannerVisible,
                        title: controller.activityBannerTitle,
                        detail: controller.activityBannerDetail,
                        startedAt: controller.activityStartedAt,
                        elapsedSeconds: controller.activityElapsedSeconds,
                        animated: controller.activityBannerAnimated,
                        showElapsed: controller.activityBannerShowsElapsed,
                      );
                    },
                  ),
                  Expanded(
                    child: (controller.timeline.isEmpty &&
                            controller.pendingPrompt?.hasVisiblePrompt !=
                                true &&
                            controller.pendingInteraction?.hasVisiblePrompt !=
                                true)
                        ? const Center(child: _LandingBrand())
                        : Column(
                            children: [
                              if (controller.hasCompactContextSelection)
                                _ContextSelectionBar(controller: controller),
                              Expanded(
                                child: ChatTimeline(
                                  items: controller.timeline,
                                  activeReviewDiff:
                                      controller.currentReviewDiff,
                                  activeReviewGroup:
                                      controller.activeReviewGroup,
                                  pendingDiffCount: controller.pendingDiffCount,
                                  pendingReviewGroupCount:
                                      controller.pendingReviewGroupCount,
                                  isManualReviewMode:
                                      controller.isManualReviewMode,
                                  isAutoAcceptMode: controller.isAutoAcceptMode,
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
                                  onOpenDiff: () => _openDiff(context),
                                  onOpenRuntimeInfo: () =>
                                      _openRuntimeInfo(context),
                                  onOpenFile: () => _openFileViewer(context),
                                  onReviewDecision:
                                      controller.sendReviewDecision,
                                  onAcceptAll: controller.acceptAllPendingDiffs,
                                  onPromptSubmit: controller.submitPromptOption,
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
      bottomNavigationBar: CommandInputBar(
        awaitInput: controller.awaitInput,
        isBusy: controller.isSessionBusy,
        canStop: controller.canStopCurrentRun,
        hasPendingReview: controller.hasPendingReview,
        fastMode: controller.fastMode,
        permissionMode: controller.displayPermissionMode,
        shouldShowPermissionChoices: controller.shouldShowPermissionChoices,
        shouldShowReviewChoices: controller.shouldShowReviewChoices,
        shouldShowPlanChoices: controller.shouldShowPlanChoices,
        onSubmit: controller.sendInputText,
        onStop: controller.stopCurrentRun,
        onOpenSessions: () => _openSessions(context),
        onOpenRuntimeInfo: () => _openRuntimeInfo(context),
        onOpenLogs: () => _openLogs(context),
        onOpenSkills: () => _openSkills(context),
        onOpenMemory: () => _openMemory(context),
        onOpenPermissions: () => _openPermissions(context),
        onOpenModels: () => _openModelSwitcher(context),
        onPermissionModeChanged: controller.updatePermissionMode,
        showClaudeMode: controller.shouldShowClaudeMode,
        currentEngine: controller.commandBarEngine,
        modelSummary: controller.commandBarModelSummary,
        permissionRuleSummary: controller.permissionRuleSummary,
        isSessionLoading: controller.isLoadingSession,
      ),
    );
  }

  void _openFileDrawer() {
    controller.refreshFileList();
    _scaffoldKey.currentState?.openDrawer();
  }

  Future<void> _openConnectionConfig(BuildContext context) async {
    final hostController = TextEditingController(text: controller.config.host);
    final portController = TextEditingController(text: controller.config.port);
    final tokenController =
        TextEditingController(text: controller.config.token);
    final cwdController = TextEditingController(text: controller.config.cwd);
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

            final normalizedIceHost = iceHostController.text.trim().isNotEmpty
                ? iceHostController.text.trim()
                : (hostController.text.trim().isEmpty
                    ? controller.config.host
                    : hostController.text.trim());

            Future<void> handleScan() async {
              final scannedRaw = await showModalBottomSheet<String>(
                context: context,
                isScrollControlled: true,
                builder: (context) => const ConnectionScanSheet(),
              );
              if (!context.mounted || scannedRaw == null) {
                return;
              }
              final scanned = AppConfig.fromLaunchUri(
                scannedRaw,
                fallback: AppConfig(
                  host: hostController.text.trim(),
                  port: portController.text.trim(),
                  token: tokenController.text.trim(),
                  cwd: cwdController.text.trim(),
                  engine: selectedEngine,
                  claudeModel: controller.config.claudeModel,
                  codexModel: controller.config.codexModel,
                  codexReasoningEffort: controller.config.codexReasoningEffort,
                  permissionMode: permissionController.text.trim(),
                  fastMode: controller.fastMode,
                  adbIceServersJson: encodedIceConfig(),
                ),
              );
              if (scanned == null) {
                setSheetState(() {
                  scanHint = '扫码内容无法识别，请确认二维码来自 MobileVC 启动器。';
                });
                return;
              }
              hostController.text = scanned.host;
              portController.text = scanned.port;
              tokenController.text = scanned.token;
              iceHostController.text = scanned.adbIceHostOverride;
              iceUsernameController.text = scanned.adbIceUsername;
              iceCredentialController.text = scanned.adbIceCredential;
              setSheetState(() {
                selectedEngine = scanned.engine.trim().isEmpty
                    ? selectedEngine
                    : scanned.engine.trim();
                scanHint =
                    '已回填 ${scanned.host}:${scanned.port}${scanned.token.isNotEmpty ? ' 与 token' : ''}';
              });
            }

            Future<void> persistConfig({bool connect = false}) async {
              await controller.saveConfig(controller.config.copyWith(
                host: hostController.text.trim(),
                port: portController.text.trim(),
                token: tokenController.text.trim(),
                cwd: cwdController.text.trim(),
                engine: selectedEngine,
                permissionMode: permissionController.text.trim(),
                fastMode: controller.fastMode,
                adbIceServersJson: encodedIceConfig(),
              ));
              if (connect) {
                await controller.connect();
              }
              if (context.mounted) {
                Navigator.pop(context);
              }
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
                      '支持扫描 MobileVC 后端二维码自动回填，也保留手动输入方式。',
                      style: Theme.of(context).textTheme.bodySmall,
                    ),
                    const SizedBox(height: 12),
                    SizedBox(
                      width: double.infinity,
                      child: OutlinedButton.icon(
                        onPressed: handleScan,
                        icon: const Icon(Icons.qr_code_scanner),
                        label: const Text('扫码连接'),
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
                    TextField(
                      controller: hostController,
                      decoration: const InputDecoration(labelText: 'Host'),
                      onChanged: (_) => setSheetState(() {}),
                    ),
                    const SizedBox(height: 10),
                    TextField(
                        controller: portController,
                        decoration: const InputDecoration(labelText: 'Port')),
                    const SizedBox(height: 10),
                    TextField(
                        controller: tokenController,
                        decoration: const InputDecoration(labelText: 'Token')),
                    const SizedBox(height: 10),
                    TextField(
                        controller: cwdController,
                        decoration: const InputDecoration(labelText: 'CWD')),
                    const SizedBox(height: 10),
                    DropdownButtonFormField<String>(
                      value: selectedEngine,
                      decoration: const InputDecoration(labelText: 'Engine'),
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
                      onChanged: (value) {
                        if (value == null) {
                          return;
                        }
                        setSheetState(() {
                          selectedEngine = value;
                        });
                      },
                    ),
                    const SizedBox(height: 10),
                    TextField(
                        controller: permissionController,
                        decoration: const InputDecoration(
                            labelText: 'Permission Mode')),
                    const SizedBox(height: 10),
                    TextField(
                      controller: iceHostController,
                      decoration: const InputDecoration(
                        labelText: 'ADB TURN Host Override',
                        hintText: '留空则跟 Host 一致',
                      ),
                      onChanged: (_) => setSheetState(() {}),
                    ),
                    const SizedBox(height: 10),
                    TextField(
                      controller: iceUsernameController,
                      decoration: const InputDecoration(
                        labelText: 'ADB TURN Username',
                        hintText: 'mobilevc',
                      ),
                    ),
                    const SizedBox(height: 10),
                    TextField(
                      controller: iceCredentialController,
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
                        style: Theme.of(context).textTheme.bodySmall?.copyWith(
                              color: Theme.of(context).colorScheme.primary,
                            ),
                      ),
                    ],
                    const SizedBox(height: 16),
                    Row(
                      children: [
                        Expanded(
                          child: FilledButton.tonal(
                            onPressed: () => persistConfig(),
                            child: const Text('保存'),
                          ),
                        ),
                        const SizedBox(width: 10),
                        Expanded(
                          child: FilledButton(
                            onPressed: () => persistConfig(connect: true),
                            child: const Text('连接'),
                          ),
                        ),
                      ],
                    ),
                    const SizedBox(height: 10),
                    if (controller.connected)
                      SizedBox(
                        width: double.infinity,
                        child: OutlinedButton(
                          onPressed: () async {
                            await controller.disconnect();
                            if (context.mounted) {
                              Navigator.pop(context);
                            }
                          },
                          child: const Text('断开连接'),
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
              onLoad: (id) {
                controller.loadSession(id);
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
                  );
                },
              ),
            ),
          ),
        );
      },
    );
  }

  Future<void> _openModelSwitcher(BuildContext context) async {
    final engine = controller.currentAiEngine;

    // 先打开模态框，显示加载状态
    await showModalBottomSheet<void>(
      context: context,
      isScrollControlled: true,
      useSafeArea: true,
      showDragHandle: true,
      backgroundColor: Colors.transparent,
      builder: (context) {
        // 在打开时请求模型目录（移除 isEmpty 检查，总是请求）
        if (engine == 'codex') {
          controller.requestCodexModelCatalog(force: true);
        }

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
      ..showSnackBar(SnackBar(content: Text('开始下载：$path')));

    try {
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

  Future<Uint8List> _fetchFileBytes(String path) async {
    final client = HttpClient();
    try {
      final request = await client.getUrl(controller.config.downloadUri(path));
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

                // 判断是否加载失败
                final modelOptions = isCodex
                    ? controller.codexModelCatalog
                        .map(
                          (entry) => _ModelChoice(
                            value: entry.model,
                            title: controller.codexModelDisplayLabel(
                              entry.model,
                            ),
                            subtitle: entry.description.isNotEmpty
                                ? entry.description
                                : (entry.isDefault
                                    ? 'Codex 当前默认模型'
                                    : 'Codex 原生模型'),
                          ),
                        )
                        .toList()
                    : (isClaude ? _claudeModelChoices : <_ModelChoice>[]);
                final hasSelectedPreset = modelOptions.any(
                  (option) => isClaude
                      ? isEquivalentClaudeModelSelection(
                          option.value,
                          selectedModel,
                        )
                      : option.value == selectedModel,
                );
                final selectedCatalogEntry = isCodex
                    ? controller.codexModelCatalogEntry(selectedModel)
                    : null;
                final effortOptions = isCodex
                    ? controller.codexReasoningEffortOptionsForModel(
                        selectedModel,
                      )
                    : const <CodexReasoningEffortOption>[];
                if (isCodex && effortOptions.isNotEmpty) {
                  selectedEffort =
                      controller.preferredCodexReasoningEffortForModel(
                    selectedModel,
                    fallback: selectedEffort,
                  );
                }
                final showEffortStep =
                    isCodex && sheetStep == _ModelSwitcherStep.effort;
                return FractionallySizedBox(
                  heightFactor: 0.7,
                  child: ClipRRect(
                    borderRadius:
                        const BorderRadius.vertical(top: Radius.circular(28)),
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
                                      color: theme
                                          .colorScheme.surfaceContainerHigh,
                                      borderRadius: BorderRadius.circular(999),
                                    ),
                                    child: Text(
                                      showEffortStep ? '2 / 2' : '1 / 2',
                                      style:
                                          theme.textTheme.labelMedium?.copyWith(
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
                                      ? '当前模型为 ${controller.codexModelDisplayLabel(selectedModel)}，选择后将用于下一次 Codex 启动。'
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
                                      duration:
                                          const Duration(milliseconds: 220),
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
                                                    padding:
                                                        const EdgeInsets.all(
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
                                                          CrossAxisAlignment
                                                              .start,
                                                      children: [
                                                        Text(
                                                          controller
                                                              .codexModelDisplayLabel(
                                                            selectedModel,
                                                          ),
                                                          style: theme.textTheme
                                                              .titleMedium
                                                              ?.copyWith(
                                                            fontWeight:
                                                                FontWeight.w800,
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
                                                              : '当前选择的 Codex 模型',
                                                          style: theme.textTheme
                                                              .bodySmall
                                                              ?.copyWith(
                                                            color: theme
                                                                .colorScheme
                                                                .onSurfaceVariant,
                                                          ),
                                                        ),
                                                      ],
                                                    ),
                                                  ),
                                                  const SizedBox(height: 18),
                                                  Text(
                                                    '推理强度',
                                                    style: theme
                                                        .textTheme.titleSmall
                                                        ?.copyWith(
                                                      fontWeight:
                                                          FontWeight.w800,
                                                    ),
                                                  ),
                                                  const SizedBox(height: 10),
                                                  if (selectedCatalogEntry !=
                                                          null &&
                                                      effortOptions.isNotEmpty)
                                                    Wrap(
                                                      spacing: 8,
                                                      runSpacing: 8,
                                                      children: [
                                                        for (final option
                                                            in effortOptions)
                                                          ChoiceChip(
                                                            label: Text(
                                                              option
                                                                  .reasoningEffort
                                                                  .toUpperCase(),
                                                            ),
                                                            selected:
                                                                selectedEffort ==
                                                                    option
                                                                        .reasoningEffort,
                                                            onSelected: (_) {
                                                              setState(() {
                                                                selectedEffort =
                                                                    option
                                                                        .reasoningEffort;
                                                              });
                                                            },
                                                          ),
                                                      ],
                                                    )
                                                  else ...[
                                                    Text(
                                                      selectedModel
                                                              .trim()
                                                              .isEmpty
                                                          ? '先选择一个 Codex 模型。'
                                                          : '当前保存的模型不在 Codex 原生目录中，因此这里只保留已保存强度，不展示额外原生选项。',
                                                      style: theme
                                                          .textTheme.bodySmall
                                                          ?.copyWith(
                                                        color: theme.colorScheme
                                                            .onSurfaceVariant,
                                                      ),
                                                    ),
                                                    if (selectedEffort
                                                        .trim()
                                                        .isNotEmpty) ...[
                                                      const SizedBox(height: 8),
                                                      ChoiceChip(
                                                        label: Text(
                                                          selectedEffort
                                                              .toUpperCase(),
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
                                                                .colorScheme
                                                                .primary,
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
                                                                .textTheme
                                                                .bodySmall
                                                                ?.copyWith(
                                                              color: theme
                                                                  .colorScheme
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
                                                    style: theme
                                                        .textTheme.titleSmall
                                                        ?.copyWith(
                                                      fontWeight:
                                                          FontWeight.w800,
                                                    ),
                                                  ),
                                                  const SizedBox(height: 10),
                                                  if (isCodex &&
                                                      !controller
                                                          .codexModelCatalogLoading &&
                                                      modelOptions.isEmpty) ...[
                                                    Container(
                                                      width: double.infinity,
                                                      padding:
                                                          const EdgeInsets.all(
                                                        14,
                                                      ),
                                                      decoration: BoxDecoration(
                                                        color: theme.colorScheme
                                                            .surfaceContainerLow,
                                                        borderRadius:
                                                            BorderRadius
                                                                .circular(16),
                                                      ),
                                                      child: Text(
                                                        controller
                                                                .codexModelCatalogMessage
                                                                .trim()
                                                                .isNotEmpty
                                                            ? controller
                                                                .codexModelCatalogMessage
                                                            : '暂未拿到 Codex 原生模型目录。',
                                                        style: theme
                                                            .textTheme.bodySmall
                                                            ?.copyWith(
                                                          color: theme
                                                              .colorScheme
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
                                                      for (final option
                                                          in modelOptions)
                                                        _ModelChoiceCard(
                                                          title: option.title,
                                                          subtitle:
                                                              option.subtitle,
                                                          selected: isClaude
                                                              ? isEquivalentClaudeModelSelection(
                                                                  selectedModel,
                                                                  option.value,
                                                                )
                                                              : selectedModel ==
                                                                  option.value,
                                                          onTap: () {
                                                            setState(() {
                                                              selectedModel =
                                                                  option.value;
                                                              if (isCodex) {
                                                                selectedEffort =
                                                                    controller
                                                                        .preferredCodexReasoningEffortForModel(
                                                                  option.value,
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
                                onPressed: !supportsModels ||
                                        selectedModel.trim().isEmpty
                                    ? null
                                    : showEffortStep
                                        ? () async {
                                            await controller
                                                .updateAiModelSelection(
                                              model: selectedModel,
                                              reasoningEffort: selectedEffort,
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
                                                  sheetStep =
                                                      _ModelSwitcherStep.effort;
                                                });
                                              }
                                            : () async {
                                                await controller
                                                    .updateAiModelSelection(
                                                  model: selectedModel,
                                                  reasoningEffort:
                                                      selectedEffort,
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
                              color: selected
                                  ? scheme.onPrimaryContainer
                                  : null,
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
                  letterSpacing: -0.5,
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

class _ConnectionDot extends StatelessWidget {
  const _ConnectionDot({required this.connected});

  final bool connected;

  @override
  Widget build(BuildContext context) {
    final scheme = Theme.of(context).colorScheme;
    final color = connected ? const Color(0xFF22C55E) : scheme.outline;
    return Container(
      width: 10,
      height: 10,
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
    );
  }
}
