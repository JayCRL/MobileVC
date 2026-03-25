import 'dart:io';

import 'package:file_picker/file_picker.dart';
import 'package:flutter/foundation.dart';
import 'package:flutter/material.dart';
import 'package:path_provider/path_provider.dart';
import 'package:share_plus/share_plus.dart';

import '../../core/config/app_config.dart';
import '../../features/chat/chat_timeline.dart';
import '../../features/chat/command_input_bar.dart';
import '../../features/diff/diff_viewer_sheet.dart';
import '../../features/files/file_browser_sheet.dart';
import '../../features/files/file_viewer_sheet.dart';
import '../../features/memory/memory_management_sheet.dart';
import '../../features/runtime_info/runtime_info_sheet.dart';
import '../../features/skills/skill_management_sheet.dart';
import '../../features/status/status_detail_sheet.dart';
import '../../features/status/terminal_log_sheet.dart';
import '../../widgets/brand_badge.dart';
import 'activity_runner_bar.dart';
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
                controller.connected
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
            onPressed: () => _openStatusDetails(context),
            icon: const Icon(Icons.dashboard_outlined),
          ),
          IconButton(
            onPressed: () => _openConnectionConfig(context),
            icon: const Icon(Icons.settings_outlined),
          ),
        ],
      ),
      body: controller.connected
          ? GestureDetector(
              behavior: HitTestBehavior.translucent,
              onTap: () => FocusManager.instance.primaryFocus?.unfocus(),
              child: Column(
                children: [
                  AnimatedBuilder(
                    animation: Listenable.merge([controller]),
                    builder: (context, _) {
                      return ActivityRunnerBar(
                        visible: controller.activityVisible,
                        label: controller.activityToolLabel,
                        startedAt: controller.activityStartedAt,
                        elapsedSeconds: controller.activityElapsedSeconds,
                      );
                    },
                  ),
                  Expanded(
                    child: (controller.timeline.isEmpty &&
                            controller.pendingPrompt?.hasVisiblePrompt != true)
                        ? const Center(child: _LandingBrand())
                        : ChatTimeline(
                            items: controller.timeline,
                            activeReviewDiff: controller.currentReviewDiff,
                            activeReviewGroup: controller.activeReviewGroup,
                            pendingDiffCount: controller.pendingDiffCount,
                            pendingReviewGroupCount:
                                controller.pendingReviewGroupCount,
                            isManualReviewMode: controller.isManualReviewMode,
                            isAutoAcceptMode: controller.isAutoAcceptMode,
                            pendingPrompt: controller.pendingPrompt,
                            shouldShowReviewChoices:
                                controller.shouldShowReviewChoices,
                            onOpenDiff: () => _openDiff(context),
                            onOpenRuntimeInfo: () => _openRuntimeInfo(context),
                            onOpenFile: () => _openFileViewer(context),
                            onReviewDecision: controller.sendReviewDecision,
                            onAcceptAll: controller.acceptAllPendingDiffs,
                            onPromptSubmit: controller.submitPromptOption,
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
        hasPendingReview: controller.hasPendingReview,
        fastMode: controller.fastMode,
        permissionMode: controller.config.permissionMode,
        onSubmit: controller.sendInputText,
        onOpenSessions: () => _openSessions(context),
        onOpenRuntimeInfo: () => _openRuntimeInfo(context),
        onOpenLogs: () => _openLogs(context),
        onOpenSkills: () => _openSkills(context),
        onOpenMemory: () => _openMemory(context),
        onPermissionModeChanged: controller.updatePermissionMode,
        showClaudeMode: controller.inClaudeMode,
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
    final engineController =
        TextEditingController(text: controller.config.engine);
    final permissionController =
        TextEditingController(text: controller.config.permissionMode);

    await showModalBottomSheet<void>(
      context: context,
      isScrollControlled: true,
      builder: (context) {
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
                  'IP 地址、端口和 token 都可以手动输入，适合你自己连本地或局域网后端。',
                  style: Theme.of(context).textTheme.bodySmall,
                ),
                const SizedBox(height: 12),
                TextField(
                    controller: hostController,
                    decoration: const InputDecoration(labelText: 'Host')),
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
                TextField(
                    controller: engineController,
                    decoration: const InputDecoration(labelText: 'Engine')),
                const SizedBox(height: 10),
                TextField(
                    controller: permissionController,
                    decoration:
                        const InputDecoration(labelText: 'Permission Mode')),
                const SizedBox(height: 16),
                Row(
                  children: [
                    Expanded(
                      child: FilledButton.tonal(
                        onPressed: () async {
                          await controller.saveConfig(
                            AppConfig(
                              host: hostController.text.trim(),
                              port: portController.text.trim(),
                              token: tokenController.text.trim(),
                              cwd: cwdController.text.trim(),
                              engine: engineController.text.trim(),
                              permissionMode: permissionController.text.trim(),
                              fastMode: controller.fastMode,
                            ),
                          );
                          if (context.mounted) {
                            Navigator.pop(context);
                          }
                        },
                        child: const Text('保存'),
                      ),
                    ),
                    const SizedBox(width: 10),
                    Expanded(
                      child: FilledButton(
                        onPressed: () async {
                          await controller.saveConfig(
                            AppConfig(
                              host: hostController.text.trim(),
                              port: portController.text.trim(),
                              token: tokenController.text.trim(),
                              cwd: cwdController.text.trim(),
                              engine: engineController.text.trim(),
                              permissionMode: permissionController.text.trim(),
                              fastMode: controller.fastMode,
                            ),
                          );
                          await controller.connect();
                          if (context.mounted) {
                            Navigator.pop(context);
                          }
                        },
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
  }

  Future<void> _openSessions(BuildContext context) async {
    await showModalBottomSheet<void>(
      context: context,
      isScrollControlled: true,
      builder: (context) {
        return SessionListSheet(
          sessions: controller.sessions,
          selectedSessionId: controller.selectedSessionId,
          onCreate: controller.createSession,
          onLoad: (id) {
            controller.loadSession(id);
            Navigator.pop(context);
          },
          onDelete: controller.deleteSession,
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
                    showReviewActions: controller.openedFileMatchesPendingDiff,
                    isDiffMode: controller.openedFileDiff != null,
                    reviewDiff: controller.openedFileDiff,
                    pendingDiffs: controller.pendingDiffs,
                    reviewGroups: controller.reviewGroups,
                    activeReviewGroupId: controller.activeReviewGroupId,
                    activeReviewDiffId: controller.activeReviewDiffId,
                    isAutoAcceptMode: controller.isAutoAcceptMode,
                    shouldShowReviewChoices:
                        controller.shouldShowReviewChoices &&
                        controller.openedFilePendingDiff != null &&
                        controller.currentReviewDiff != null &&
                        ((controller.openedFilePendingDiff!.id.isNotEmpty &&
                                controller.openedFilePendingDiff!.id ==
                                    controller.currentReviewDiff!.id) ||
                            controller.openedFilePendingDiff!.path ==
                                controller.currentReviewDiff!.path),
                    pendingPrompt: controller.pendingPrompt,
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
        final diff = controller.currentDiffContext;
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
                showReviewActions: controller.shouldShowReviewChoices,
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
                  return SkillManagementSheet(
                    skills: controller.skills,
                    enabledSkillNames:
                        controller.sessionContext.enabledSkillNames,
                    syncStatus: controller.skillSyncStatus,
                    onToggleEnabled: controller.toggleSkillEnabled,
                    onSave: controller.saveSkill,
                    onSync: controller.syncSkills,
                  );
                },
              ),
            ),
          ),
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
                    enabledMemoryIds:
                        controller.sessionContext.enabledMemoryIds,
                    onToggleEnabled: controller.toggleMemoryEnabled,
                    onSave: controller.saveMemory,
                  );
                },
              ),
            ),
          ),
        );
      },
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
                    stdout: controller.terminalStdout,
                    stderr: controller.terminalStderr,
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

class _LandingBrand extends StatelessWidget {
  const _LandingBrand();

  @override
  Widget build(BuildContext context) {
    final scheme = Theme.of(context).colorScheme;
    return Column(
      mainAxisSize: MainAxisSize.min,
      children: [
        const BrandBadge(size: 64),
        const SizedBox(height: 16),
        Text(
          'MobileVC',
          style: Theme.of(context).textTheme.headlineMedium?.copyWith(
                fontWeight: FontWeight.w800,
                letterSpacing: -0.4,
              ),
        ),
        const SizedBox(height: 8),
        Text(
          '连接 Claude 会话、文件树与 diff 审核的统一工作台',
          style: Theme.of(context)
              .textTheme
              .bodyMedium
              ?.copyWith(color: scheme.onSurfaceVariant),
          textAlign: TextAlign.center,
        ),
      ],
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
