import 'runtime_meta.dart';

DateTime? _parseDate(String? value) {
  if (value == null || value.isEmpty) {
    return null;
  }
  return DateTime.tryParse(value)?.toLocal();
}

class SessionSummary {
  const SessionSummary({
    required this.id,
    required this.title,
    this.createdAt,
    this.updatedAt,
    this.lastPreview = '',
    this.entryCount = 0,
    this.source = '',
    this.external = false,
    this.runtime = const RuntimeMeta(),
  });

  final String id;
  final String title;
  final DateTime? createdAt;
  final DateTime? updatedAt;
  final String lastPreview;
  final int entryCount;
  final String source;
  final bool external;
  final RuntimeMeta runtime;

  factory SessionSummary.fromJson(Map<String, dynamic> json) {
    return SessionSummary(
      id: (json['id'] ?? '').toString(),
      title: (json['title'] ?? '').toString(),
      createdAt: _parseDate(json['createdAt']?.toString()),
      updatedAt: _parseDate(json['updatedAt']?.toString()),
      lastPreview: (json['lastPreview'] ?? '').toString(),
      entryCount: (json['entryCount'] as num?)?.toInt() ?? 0,
      source: (json['source'] ?? '').toString(),
      external: json['external'] == true,
      runtime: json['runtime'] is Map<String, dynamic>
          ? RuntimeMeta.fromJson(json['runtime'] as Map<String, dynamic>)
          : const RuntimeMeta(),
    );
  }
}

class AdbDevice {
  const AdbDevice({
    this.serial = '',
    this.state = '',
    this.model = '',
    this.product = '',
    this.deviceName = '',
    this.transportId = '',
  });

  final String serial;
  final String state;
  final String model;
  final String product;
  final String deviceName;
  final String transportId;

  String get displayLabel {
    final parts = <String>[
      if (model.trim().isNotEmpty) model.trim(),
      if (serial.trim().isNotEmpty) serial.trim(),
    ];
    if (parts.isEmpty) {
      return '未命名设备';
    }
    return parts.join(' · ');
  }

  factory AdbDevice.fromJson(Map<String, dynamic> json) {
    return AdbDevice(
      serial: (json['serial'] ?? '').toString(),
      state: (json['state'] ?? '').toString(),
      model: (json['model'] ?? '').toString(),
      product: (json['product'] ?? '').toString(),
      deviceName: (json['deviceName'] ?? '').toString(),
      transportId: (json['transportId'] ?? '').toString(),
    );
  }
}

class HistoryContext {
  const HistoryContext({
    this.id = '',
    this.type = '',
    this.message = '',
    this.status = '',
    this.target = '',
    this.targetPath = '',
    this.tool = '',
    this.command = '',
    this.timestamp = '',
    this.title = '',
    this.stack = '',
    this.code = '',
    this.relatedStep = '',
    this.path = '',
    this.diff = '',
    this.lang = '',
    this.pendingReview = false,
    this.source = '',
    this.skillName = '',
    this.executionId = '',
    this.groupId = '',
    this.groupTitle = '',
    this.reviewStatus = '',
  });

  final String id;
  final String type;
  final String message;
  final String status;
  final String target;
  final String targetPath;
  final String tool;
  final String command;
  final String timestamp;
  final String title;
  final String stack;
  final String code;
  final String relatedStep;
  final String path;
  final String diff;
  final String lang;
  final bool pendingReview;
  final String source;
  final String skillName;
  final String executionId;
  final String groupId;
  final String groupTitle;
  final String reviewStatus;

  factory HistoryContext.fromJson(Map<String, dynamic> json) {
    bool pending = false;
    final rawPending = json['pendingReview'];
    if (rawPending is bool) {
      pending = rawPending;
    }
    String read(String key) => (json[key] ?? '').toString();
    return HistoryContext(
      id: read('id'),
      type: read('type'),
      message: read('message'),
      status: read('status'),
      target: read('target'),
      targetPath: read('targetPath'),
      tool: read('tool'),
      command: read('command'),
      timestamp: read('timestamp'),
      title: read('title'),
      stack: read('stack'),
      code: read('code'),
      relatedStep: read('relatedStep'),
      path: read('path'),
      diff: read('diff'),
      lang: read('lang'),
      pendingReview: pending,
      source: read('source'),
      skillName: read('skillName'),
      executionId: read('executionId'),
      groupId: read('groupId'),
      groupTitle: read('groupTitle'),
      reviewStatus: read('reviewStatus'),
    );
  }
}

class ReviewFile {
  const ReviewFile({
    this.id = '',
    this.path = '',
    this.title = '',
    this.diff = '',
    this.lang = '',
    this.pendingReview = false,
    this.reviewStatus = '',
    this.executionId = '',
  });

  final String id;
  final String path;
  final String title;
  final String diff;
  final String lang;
  final bool pendingReview;
  final String reviewStatus;
  final String executionId;

  factory ReviewFile.fromJson(Map<String, dynamic> json) {
    return ReviewFile(
      id: (json['id'] ?? '').toString(),
      path: (json['path'] ?? '').toString(),
      title: (json['title'] ?? '').toString(),
      diff: (json['diff'] ?? '').toString(),
      lang: (json['lang'] ?? '').toString(),
      pendingReview: json['pendingReview'] == true,
      reviewStatus: (json['reviewStatus'] ?? '').toString(),
      executionId: (json['executionId'] ?? '').toString(),
    );
  }
}

class ReviewGroup {
  const ReviewGroup({
    this.id = '',
    this.title = '',
    this.executionId = '',
    this.pendingReview = false,
    this.reviewStatus = '',
    this.currentFileId = '',
    this.currentPath = '',
    this.pendingCount = 0,
    this.acceptedCount = 0,
    this.revertedCount = 0,
    this.revisedCount = 0,
    this.files = const [],
  });

  final String id;
  final String title;
  final String executionId;
  final bool pendingReview;
  final String reviewStatus;
  final String currentFileId;
  final String currentPath;
  final int pendingCount;
  final int acceptedCount;
  final int revertedCount;
  final int revisedCount;
  final List<ReviewFile> files;

  factory ReviewGroup.fromJson(Map<String, dynamic> json) {
    return ReviewGroup(
      id: (json['id'] ?? '').toString(),
      title: (json['title'] ?? '').toString(),
      executionId: (json['executionId'] ?? '').toString(),
      pendingReview: json['pendingReview'] == true,
      reviewStatus: (json['reviewStatus'] ?? '').toString(),
      currentFileId: (json['currentFileId'] ?? '').toString(),
      currentPath: (json['currentPath'] ?? '').toString(),
      pendingCount: (json['pendingCount'] as num?)?.toInt() ?? 0,
      acceptedCount: (json['acceptedCount'] as num?)?.toInt() ?? 0,
      revertedCount: (json['revertedCount'] as num?)?.toInt() ?? 0,
      revisedCount: (json['revisedCount'] as num?)?.toInt() ?? 0,
      files: ((json['files'] as List?) ?? const [])
          .whereType<Map<String, dynamic>>()
          .map(ReviewFile.fromJson)
          .toList(),
    );
  }
}

class HistoryLogEntry {
  const HistoryLogEntry({
    required this.kind,
    this.message = '',
    this.label = '',
    this.timestamp = '',
    this.stream = '',
    this.text = '',
    this.executionId = '',
    this.context,
  });

  final String kind;
  final String message;
  final String label;
  final String timestamp;
  final String stream;
  final String text;
  final String executionId;
  final HistoryContext? context;

  factory HistoryLogEntry.fromJson(Map<String, dynamic> json) {
    return HistoryLogEntry(
      kind: (json['kind'] ?? '').toString(),
      message: (json['message'] ?? '').toString(),
      label: (json['label'] ?? '').toString(),
      timestamp: (json['timestamp'] ?? '').toString(),
      stream: (json['stream'] ?? '').toString(),
      text: (json['text'] ?? '').toString(),
      executionId: (json['executionId'] ?? '').toString(),
      context: json['context'] is Map<String, dynamic>
          ? HistoryContext.fromJson(json['context'] as Map<String, dynamic>)
          : null,
    );
  }
}

class TerminalExecution {
  const TerminalExecution({
    this.executionId = '',
    this.command = '',
    this.cwd = '',
    this.source = '',
    this.sourceLabel = '',
    this.contextId = '',
    this.contextTitle = '',
    this.groupId = '',
    this.groupTitle = '',
    this.startedAt,
    this.completedAt,
    this.running = false,
    this.exitCode,
    this.stdout = '',
    this.stderr = '',
  });

  final String executionId;
  final String command;
  final String cwd;
  final String source;
  final String sourceLabel;
  final String contextId;
  final String contextTitle;
  final String groupId;
  final String groupTitle;
  final DateTime? startedAt;
  final DateTime? completedAt;
  final bool running;
  final int? exitCode;
  final String stdout;
  final String stderr;

  bool get hasOutput => stdout.isNotEmpty || stderr.isNotEmpty;

  String get title {
    final trimmed = command.trim();
    if (trimmed.isNotEmpty) {
      return trimmed;
    }
    if (cwd.trim().isNotEmpty) {
      return cwd.trim();
    }
    return executionId.isNotEmpty ? executionId : '未命名命令';
  }

  factory TerminalExecution.fromJson(Map<String, dynamic> json) {
    final completedAt = _parseDate(
      json['finishedAt']?.toString() ?? json['completedAt']?.toString(),
    );
    final exitCode = (json['exitCode'] as num?)?.toInt();
    final explicitRunning = json['running'] == true;
    return TerminalExecution(
      executionId: (json['executionId'] ?? '').toString(),
      command: (json['command'] ?? '').toString(),
      cwd: (json['cwd'] ?? '').toString(),
      source: (json['source'] ?? '').toString(),
      sourceLabel: (json['sourceLabel'] ?? '').toString(),
      contextId: (json['contextId'] ?? '').toString(),
      contextTitle: (json['contextTitle'] ?? '').toString(),
      groupId: (json['groupId'] ?? '').toString(),
      groupTitle: (json['groupTitle'] ?? '').toString(),
      startedAt: _parseDate(json['startedAt']?.toString()),
      completedAt: completedAt,
      running: explicitRunning || (completedAt == null && exitCode == null),
      exitCode: exitCode,
      stdout: (json['stdout'] ?? '').toString(),
      stderr: (json['stderr'] ?? '').toString(),
    );
  }
}

class RuntimeProcessItem {
  const RuntimeProcessItem({
    this.pid = 0,
    this.ppid = 0,
    this.state = '',
    this.elapsed = '',
    this.command = '',
    this.cwd = '',
    this.executionId = '',
    this.source = '',
    this.root = false,
    this.logAvailable = false,
  });

  final int pid;
  final int ppid;
  final String state;
  final String elapsed;
  final String command;
  final String cwd;
  final String executionId;
  final String source;
  final bool root;
  final bool logAvailable;

  String get title {
    final trimmed = command.trim();
    if (trimmed.isNotEmpty) {
      return trimmed;
    }
    return pid > 0 ? 'PID $pid' : '未命名进程';
  }

  factory RuntimeProcessItem.fromJson(Map<String, dynamic> json) {
    return RuntimeProcessItem(
      pid: (json['pid'] as num?)?.toInt() ?? 0,
      ppid: (json['ppid'] as num?)?.toInt() ?? 0,
      state: (json['state'] ?? '').toString(),
      elapsed: (json['elapsed'] ?? '').toString(),
      command: (json['command'] ?? '').toString(),
      cwd: (json['cwd'] ?? '').toString(),
      executionId: (json['executionId'] ?? '').toString(),
      source: (json['source'] ?? '').toString(),
      root: json['root'] == true,
      logAvailable: json['logAvailable'] == true,
    );
  }
}

class PermissionRule {
  const PermissionRule({
    this.id = '',
    this.scope = '',
    this.enabled = false,
    this.engine = '',
    this.kind = '',
    this.commandHead = '',
    this.targetPathPrefix = '',
    this.summary = '',
    this.createdAt,
    this.lastMatchedAt,
    this.matchCount = 0,
  });

  final String id;
  final String scope;
  final bool enabled;
  final String engine;
  final String kind;
  final String commandHead;
  final String targetPathPrefix;
  final String summary;
  final DateTime? createdAt;
  final DateTime? lastMatchedAt;
  final int matchCount;

  PermissionRule copyWith({
    String? id,
    String? scope,
    bool? enabled,
    String? engine,
    String? kind,
    String? commandHead,
    String? targetPathPrefix,
    String? summary,
    DateTime? createdAt,
    DateTime? lastMatchedAt,
    int? matchCount,
  }) {
    return PermissionRule(
      id: id ?? this.id,
      scope: scope ?? this.scope,
      enabled: enabled ?? this.enabled,
      engine: engine ?? this.engine,
      kind: kind ?? this.kind,
      commandHead: commandHead ?? this.commandHead,
      targetPathPrefix: targetPathPrefix ?? this.targetPathPrefix,
      summary: summary ?? this.summary,
      createdAt: createdAt ?? this.createdAt,
      lastMatchedAt: lastMatchedAt ?? this.lastMatchedAt,
      matchCount: matchCount ?? this.matchCount,
    );
  }

  String get displayTitle {
    if (summary.trim().isNotEmpty) {
      return summary.trim();
    }
    final parts = <String>[
      if (engine.trim().isNotEmpty) engine.trim(),
      if (kind.trim().isNotEmpty) kind.trim(),
      if (commandHead.trim().isNotEmpty) commandHead.trim(),
      if (targetPathPrefix.trim().isNotEmpty) targetPathPrefix.trim(),
    ];
    return parts.isEmpty ? '自动允许规则' : parts.join(' · ');
  }

  factory PermissionRule.fromJson(Map<String, dynamic> json) {
    return PermissionRule(
      id: (json['id'] ?? '').toString(),
      scope: (json['scope'] ?? '').toString(),
      enabled: json['enabled'] != false,
      engine: (json['engine'] ?? '').toString(),
      kind: (json['kind'] ?? '').toString(),
      commandHead: (json['commandHead'] ?? '').toString(),
      targetPathPrefix: (json['targetPathPrefix'] ?? '').toString(),
      summary: (json['summary'] ?? '').toString(),
      createdAt: _parseDate(json['createdAt']?.toString()),
      lastMatchedAt: _parseDate(json['lastMatchedAt']?.toString()),
      matchCount: (json['matchCount'] as num?)?.toInt() ?? 0,
    );
  }

  Map<String, dynamic> toJson() {
    return <String, dynamic>{
      'id': id,
      'scope': scope,
      'enabled': enabled,
      'engine': engine,
      'kind': kind,
      'commandHead': commandHead,
      'targetPathPrefix': targetPathPrefix,
      'summary': summary,
      if (createdAt != null) 'createdAt': createdAt!.toIso8601String(),
      if (lastMatchedAt != null)
        'lastMatchedAt': lastMatchedAt!.toIso8601String(),
      'matchCount': matchCount,
    };
  }
}

class SkillDefinition {
  const SkillDefinition({
    this.name = '',
    this.description = '',
    this.prompt = '',
    this.resultView = '',
    this.targetType = '',
    this.source = '',
    this.sourceOfTruth = '',
    this.syncState = '',
    this.editable = false,
    this.driftDetected = false,
    this.updatedAt,
    this.lastSyncedAt,
  });

  final String name;
  final String description;
  final String prompt;
  final String resultView;
  final String targetType;
  final String source;
  final String sourceOfTruth;
  final String syncState;
  final bool editable;
  final bool driftDetected;
  final DateTime? updatedAt;
  final DateTime? lastSyncedAt;

  SkillDefinition copyWith({
    String? name,
    String? description,
    String? prompt,
    String? resultView,
    String? targetType,
    String? source,
    String? sourceOfTruth,
    String? syncState,
    bool? editable,
    bool? driftDetected,
    DateTime? updatedAt,
    DateTime? lastSyncedAt,
  }) {
    return SkillDefinition(
      name: name ?? this.name,
      description: description ?? this.description,
      prompt: prompt ?? this.prompt,
      resultView: resultView ?? this.resultView,
      targetType: targetType ?? this.targetType,
      source: source ?? this.source,
      sourceOfTruth: sourceOfTruth ?? this.sourceOfTruth,
      syncState: syncState ?? this.syncState,
      editable: editable ?? this.editable,
      driftDetected: driftDetected ?? this.driftDetected,
      updatedAt: updatedAt ?? this.updatedAt,
      lastSyncedAt: lastSyncedAt ?? this.lastSyncedAt,
    );
  }

  factory SkillDefinition.fromJson(Map<String, dynamic> json) {
    return SkillDefinition(
      name: (json['name'] ?? '').toString(),
      description: (json['description'] ?? '').toString(),
      prompt: (json['prompt'] ?? '').toString(),
      resultView: (json['resultView'] ?? '').toString(),
      targetType: (json['targetType'] ?? '').toString(),
      source: (json['source'] ?? '').toString(),
      sourceOfTruth: (json['sourceOfTruth'] ?? '').toString(),
      syncState: (json['syncState'] ?? '').toString(),
      editable: json['editable'] == true,
      driftDetected: json['driftDetected'] == true,
      updatedAt: _parseDate(json['updatedAt']?.toString()),
      lastSyncedAt: _parseDate(json['lastSyncedAt']?.toString()),
    );
  }
}

class MemoryItem {
  const MemoryItem({
    this.id = '',
    this.title = '',
    this.content = '',
    this.source = '',
    this.sourceOfTruth = '',
    this.syncState = '',
    this.editable = false,
    this.driftDetected = false,
    this.updatedAt,
    this.lastSyncedAt,
  });

  final String id;
  final String title;
  final String content;
  final String source;
  final String sourceOfTruth;
  final String syncState;
  final bool editable;
  final bool driftDetected;
  final DateTime? updatedAt;
  final DateTime? lastSyncedAt;

  MemoryItem copyWith({
    String? id,
    String? title,
    String? content,
    String? source,
    String? sourceOfTruth,
    String? syncState,
    bool? editable,
    bool? driftDetected,
    DateTime? updatedAt,
    DateTime? lastSyncedAt,
  }) {
    return MemoryItem(
      id: id ?? this.id,
      title: title ?? this.title,
      content: content ?? this.content,
      source: source ?? this.source,
      sourceOfTruth: sourceOfTruth ?? this.sourceOfTruth,
      syncState: syncState ?? this.syncState,
      editable: editable ?? this.editable,
      driftDetected: driftDetected ?? this.driftDetected,
      updatedAt: updatedAt ?? this.updatedAt,
      lastSyncedAt: lastSyncedAt ?? this.lastSyncedAt,
    );
  }

  factory MemoryItem.fromJson(Map<String, dynamic> json) {
    return MemoryItem(
      id: (json['id'] ?? '').toString(),
      title: (json['title'] ?? '').toString(),
      content: (json['content'] ?? '').toString(),
      source: (json['source'] ?? '').toString(),
      sourceOfTruth: (json['sourceOfTruth'] ?? '').toString(),
      syncState: (json['syncState'] ?? '').toString(),
      editable: json['editable'] == true,
      driftDetected: json['driftDetected'] == true,
      updatedAt: _parseDate(json['updatedAt']?.toString()),
      lastSyncedAt: _parseDate(json['lastSyncedAt']?.toString()),
    );
  }
}

class CatalogMetadata {
  const CatalogMetadata({
    this.domain = '',
    this.sourceOfTruth = '',
    this.syncState = '',
    this.driftDetected = false,
    this.lastSyncedAt,
    this.versionToken = '',
    this.lastError = '',
  });

  final String domain;
  final String sourceOfTruth;
  final String syncState;
  final bool driftDetected;
  final DateTime? lastSyncedAt;
  final String versionToken;
  final String lastError;

  bool get isSyncing => syncState == 'syncing';

  factory CatalogMetadata.fromJson(Map<String, dynamic> json) {
    return CatalogMetadata(
      domain: (json['domain'] ?? '').toString(),
      sourceOfTruth: (json['sourceOfTruth'] ?? '').toString(),
      syncState: (json['syncState'] ?? '').toString(),
      driftDetected: json['driftDetected'] == true,
      lastSyncedAt: _parseDate(json['lastSyncedAt']?.toString()),
      versionToken: (json['versionToken'] ?? '').toString(),
      lastError: (json['lastError'] ?? '').toString(),
    );
  }
}

class SessionContext {
  const SessionContext({
    this.enabledSkillNames = const [],
    this.enabledMemoryIds = const [],
  });

  final List<String> enabledSkillNames;
  final List<String> enabledMemoryIds;

  SessionContext copyWith({
    List<String>? enabledSkillNames,
    List<String>? enabledMemoryIds,
  }) {
    return SessionContext(
      enabledSkillNames: enabledSkillNames ?? this.enabledSkillNames,
      enabledMemoryIds: enabledMemoryIds ?? this.enabledMemoryIds,
    );
  }

  factory SessionContext.fromJson(Map<String, dynamic> json) {
    return SessionContext(
      enabledSkillNames: ((json['enabledSkillNames'] as List?) ?? const [])
          .map((item) => item.toString())
          .toList(),
      enabledMemoryIds: ((json['enabledMemoryIds'] as List?) ?? const [])
          .map((item) => item.toString())
          .toList(),
    );
  }
}

class RuntimeInfoItem {
  const RuntimeInfoItem({
    required this.label,
    this.value = '',
    this.status = '',
    this.available = false,
    this.detail = '',
    this.meta = const <String, dynamic>{},
  });

  final String label;
  final String value;
  final String status;
  final bool available;
  final String detail;
  final Map<String, dynamic> meta;

  factory RuntimeInfoItem.fromJson(Map<String, dynamic> json) {
    return RuntimeInfoItem(
      label: (json['label'] ?? '').toString(),
      value: (json['value'] ?? '').toString(),
      status: (json['status'] ?? '').toString(),
      available: json['available'] == true,
      detail: (json['detail'] ?? '').toString(),
      meta: json['meta'] is Map
          ? Map<String, dynamic>.from(json['meta'] as Map)
          : const <String, dynamic>{},
    );
  }
}

class CodexReasoningEffortOption {
  const CodexReasoningEffortOption({
    required this.reasoningEffort,
    this.description = '',
  });

  final String reasoningEffort;
  final String description;

  factory CodexReasoningEffortOption.fromJson(Map<String, dynamic> json) {
    return CodexReasoningEffortOption(
      reasoningEffort:
          (json['reasoningEffort'] ?? '').toString().trim().toLowerCase(),
      description: (json['description'] ?? '').toString().trim(),
    );
  }
}

class CodexModelCatalogEntry {
  const CodexModelCatalogEntry({
    required this.model,
    this.id = '',
    this.displayName = '',
    this.description = '',
    this.defaultReasoningEffort = '',
    this.supportedReasoningEfforts = const <String>[],
    this.reasoningEffortOptions = const <CodexReasoningEffortOption>[],
    this.isDefault = false,
    this.hidden = false,
  });

  final String id;
  final String model;
  final String displayName;
  final String description;
  final String defaultReasoningEffort;
  final List<String> supportedReasoningEfforts;
  final List<CodexReasoningEffortOption> reasoningEffortOptions;
  final bool isDefault;
  final bool hidden;

  factory CodexModelCatalogEntry.fromRuntimeInfoItem(RuntimeInfoItem item) {
    final meta = item.meta;
    final supported = <String>[
      for (final value in ((meta['supportedReasoningEfforts'] as List?) ??
          const <dynamic>[]))
        if (value.toString().trim().isNotEmpty)
          value.toString().trim().toLowerCase(),
    ];
    final options = <CodexReasoningEffortOption>[
      for (final value
          in ((meta['reasoningEffortOptions'] as List?) ?? const <dynamic>[]))
        if (value is Map<String, dynamic>)
          CodexReasoningEffortOption.fromJson(value)
        else if (value is Map)
          CodexReasoningEffortOption.fromJson(Map<String, dynamic>.from(value)),
    ].where((option) => option.reasoningEffort.isNotEmpty).toList();
    final mergedSupported = supported.isNotEmpty
        ? supported
        : options.map((option) => option.reasoningEffort).toList();
    final mergedOptions = options.isNotEmpty
        ? options
        : mergedSupported
            .map(
                (effort) => CodexReasoningEffortOption(reasoningEffort: effort))
            .toList();
    final defaultReasoningEffort =
        (meta['defaultReasoningEffort'] ?? '').toString().trim().toLowerCase();
    return CodexModelCatalogEntry(
      id: (meta['id'] ?? '').toString().trim(),
      model: (meta['model'] ?? item.label).toString().trim(),
      displayName: (meta['displayName'] ?? item.value).toString().trim(),
      description: (meta['description'] ?? item.detail).toString().trim(),
      defaultReasoningEffort: defaultReasoningEffort.isNotEmpty
          ? defaultReasoningEffort
          : (mergedSupported.isNotEmpty ? mergedSupported.first : ''),
      supportedReasoningEfforts: mergedSupported,
      reasoningEffortOptions: mergedOptions,
      isDefault: meta['isDefault'] == true || item.status == 'default',
      hidden: meta['hidden'] == true,
    );
  }
}

class ClaudeModelCatalogEntry {
  const ClaudeModelCatalogEntry({
    required this.model,
    this.displayName = '',
    this.description = '',
    this.isDefault = false,
  });

  final String model;
  final String displayName;
  final String description;
  final bool isDefault;

  factory ClaudeModelCatalogEntry.fromRuntimeInfoItem(RuntimeInfoItem item) {
    return ClaudeModelCatalogEntry(
      model: item.label.trim(),
      displayName: item.value.trim(),
      description: item.detail.trim(),
      isDefault: item.status == 'default',
    );
  }
}

class FSItem {
  const FSItem({
    required this.name,
    this.isDir = false,
    this.size = 0,
  });

  final String name;
  final bool isDir;
  final int size;

  factory FSItem.fromJson(Map<String, dynamic> json) {
    return FSItem(
      name: (json['name'] ?? '').toString(),
      isDir: json['is_dir'] == true,
      size: (json['size'] as num?)?.toInt() ?? 0,
    );
  }
}

class FileReadResult {
  const FileReadResult({
    this.path = '',
    this.content = '',
    this.lang = '',
    this.isText = true,
    this.size = 0,
    this.encoding = 'utf-8',
  });

  static const Set<String> _imageExtensions = {
    'png',
    'jpg',
    'jpeg',
    'webp',
    'gif',
    'bmp',
    'heic',
    'heif',
  };

  final String path;
  final String content;
  final String lang;
  final bool isText;
  final int size;
  final String encoding;

  String get title {
    if (path.isEmpty) {
      return '文件';
    }
    final normalized = path.replaceAll('\\', '/');
    final index = normalized.lastIndexOf('/');
    return index == -1 ? normalized : normalized.substring(index + 1);
  }

  String get extension {
    final name = title;
    final index = name.lastIndexOf('.');
    if (index == -1 || index == name.length - 1) {
      return '';
    }
    return name.substring(index + 1).toLowerCase();
  }

  bool get isImage => _imageExtensions.contains(extension);

  factory FileReadResult.fromJson(Map<String, dynamic> json) {
    return FileReadResult(
      path: (json['path'] ?? '').toString(),
      content: (json['content'] ?? '').toString(),
      lang: (json['lang'] ?? '').toString(),
      isText: json['isText'] != false,
      size: (json['size'] as num?)?.toInt() ?? 0,
      encoding: (json['encoding'] ?? 'utf-8').toString(),
    );
  }
}
