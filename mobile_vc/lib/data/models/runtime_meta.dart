class RuntimeMeta {
  const RuntimeMeta({
    this.source = '',
    this.skillName = '',
    this.target = '',
    this.targetType = '',
    this.targetPath = '',
    this.resultView = '',
    this.resumeSessionId = '',
    this.executionId = '',
    this.groupId = '',
    this.groupTitle = '',
    this.contextId = '',
    this.contextTitle = '',
    this.targetText = '',
    this.command = '',
    this.engine = '',
    this.model = '',
    this.reasoningEffort = '',
    this.cwd = '',
    this.permissionMode = '',
    this.permissionRequestId = '',
    this.claudeLifecycle = '',
    this.blockingKind = '',
    this.targetDiff = '',
    this.targetTitle = '',
    this.targetStack = '',
    this.isReviewOnly = false,
  });

  final String source;
  final String skillName;
  final String target;
  final String targetType;
  final String targetPath;
  final String resultView;
  final String resumeSessionId;
  final String executionId;
  final String groupId;
  final String groupTitle;
  final String contextId;
  final String contextTitle;
  final String targetText;
  final String command;
  final String engine;
  final String model;
  final String reasoningEffort;
  final String cwd;
  final String permissionMode;
  final String permissionRequestId;
  final String claudeLifecycle;
  final String blockingKind;
  final String targetDiff;
  final String targetTitle;
  final String targetStack;
  final bool isReviewOnly;

  bool get hasContext =>
      contextId.isNotEmpty ||
      contextTitle.isNotEmpty ||
      targetPath.isNotEmpty ||
      targetDiff.isNotEmpty;

  RuntimeMeta merge(RuntimeMeta other) {
    return RuntimeMeta(
      source: other.source.isNotEmpty ? other.source : source,
      skillName: other.skillName.isNotEmpty ? other.skillName : skillName,
      target: other.target.isNotEmpty ? other.target : target,
      targetType: other.targetType.isNotEmpty ? other.targetType : targetType,
      targetPath: other.targetPath.isNotEmpty ? other.targetPath : targetPath,
      resultView: other.resultView.isNotEmpty ? other.resultView : resultView,
      resumeSessionId: other.resumeSessionId.isNotEmpty
          ? other.resumeSessionId
          : resumeSessionId,
      executionId:
          other.executionId.isNotEmpty ? other.executionId : executionId,
      groupId: other.groupId.isNotEmpty ? other.groupId : groupId,
      groupTitle: other.groupTitle.isNotEmpty ? other.groupTitle : groupTitle,
      contextId: other.contextId.isNotEmpty ? other.contextId : contextId,
      contextTitle:
          other.contextTitle.isNotEmpty ? other.contextTitle : contextTitle,
      targetText: other.targetText.isNotEmpty ? other.targetText : targetText,
      command: other.command.isNotEmpty ? other.command : command,
      engine: other.engine.isNotEmpty ? other.engine : engine,
      model: other.model.isNotEmpty ? other.model : model,
      reasoningEffort: other.reasoningEffort.isNotEmpty
          ? other.reasoningEffort
          : reasoningEffort,
      cwd: other.cwd.isNotEmpty ? other.cwd : cwd,
      permissionMode: other.permissionMode.isNotEmpty
          ? other.permissionMode
          : permissionMode,
      permissionRequestId: other.permissionRequestId.isNotEmpty
          ? other.permissionRequestId
          : permissionRequestId,
      claudeLifecycle: other.claudeLifecycle.isNotEmpty
          ? other.claudeLifecycle
          : claudeLifecycle,
      blockingKind:
          other.blockingKind.isNotEmpty ? other.blockingKind : blockingKind,
      targetDiff: other.targetDiff.isNotEmpty ? other.targetDiff : targetDiff,
      targetTitle:
          other.targetTitle.isNotEmpty ? other.targetTitle : targetTitle,
      targetStack:
          other.targetStack.isNotEmpty ? other.targetStack : targetStack,
      isReviewOnly: other.isReviewOnly || isReviewOnly,
    );
  }

  Map<String, dynamic> toJson() => {
        if (source.isNotEmpty) 'source': source,
        if (skillName.isNotEmpty) 'skillName': skillName,
        if (target.isNotEmpty) 'target': target,
        if (targetType.isNotEmpty) 'targetType': targetType,
        if (targetPath.isNotEmpty) 'targetPath': targetPath,
        if (resultView.isNotEmpty) 'resultView': resultView,
        if (resumeSessionId.isNotEmpty) 'resumeSessionId': resumeSessionId,
        if (executionId.isNotEmpty) 'executionId': executionId,
        if (groupId.isNotEmpty) 'groupId': groupId,
        if (groupTitle.isNotEmpty) 'groupTitle': groupTitle,
        if (contextId.isNotEmpty) 'contextId': contextId,
        if (contextTitle.isNotEmpty) 'contextTitle': contextTitle,
        if (targetText.isNotEmpty) 'targetText': targetText,
        if (command.isNotEmpty) 'command': command,
        if (engine.isNotEmpty) 'engine': engine,
        if (model.isNotEmpty) 'model': model,
        if (reasoningEffort.isNotEmpty) 'reasoningEffort': reasoningEffort,
        if (cwd.isNotEmpty) 'cwd': cwd,
        if (permissionMode.isNotEmpty) 'permissionMode': permissionMode,
        if (permissionRequestId.isNotEmpty)
          'permissionRequestId': permissionRequestId,
        if (claudeLifecycle.isNotEmpty) 'claudeLifecycle': claudeLifecycle,
        if (blockingKind.isNotEmpty) 'blockingKind': blockingKind,
        if (targetDiff.isNotEmpty) 'targetDiff': targetDiff,
        if (targetTitle.isNotEmpty) 'targetTitle': targetTitle,
        if (targetStack.isNotEmpty) 'targetStack': targetStack,
        if (isReviewOnly) 'isReviewOnly': isReviewOnly,
      };

  factory RuntimeMeta.fromJson(Map<String, dynamic> json) {
    String read(String key) => (json[key] ?? '').toString();
    return RuntimeMeta(
      source: read('source'),
      skillName: read('skillName'),
      target: read('target'),
      targetType: read('targetType'),
      targetPath: read('targetPath'),
      resultView: read('resultView'),
      resumeSessionId: read('resumeSessionId'),
      executionId: read('executionId'),
      groupId: read('groupId'),
      groupTitle: read('groupTitle'),
      contextId: read('contextId'),
      contextTitle: read('contextTitle'),
      targetText: read('targetText'),
      command: read('command'),
      engine: read('engine'),
      model: read('model'),
      reasoningEffort: read('reasoningEffort'),
      cwd: read('cwd'),
      permissionMode: read('permissionMode'),
      permissionRequestId: read('permissionRequestId'),
      claudeLifecycle: read('claudeLifecycle'),
      blockingKind: read('blockingKind'),
      targetDiff: read('targetDiff'),
      targetTitle: read('targetTitle'),
      targetStack: read('targetStack'),
      isReviewOnly: json['isReviewOnly'] == true,
    );
  }
}
