class AppConfigEngineModels {
  const AppConfigEngineModels({
    required this.claudeModel,
    required this.codexModel,
    required this.codexReasoningEffort,
  });

  final String claudeModel;
  final String codexModel;
  final String codexReasoningEffort;

  static AppConfigEngineModels resolve({
    required String nextEngine,
    required String? model,
    required String? reasoningEffort,
    required String currentClaudeModel,
    required String currentCodexModel,
    required String currentCodexReasoningEffort,
    required String? claudeModel,
    required String? codexModel,
    required String? codexReasoningEffort,
  }) {
    final isCodex = nextEngine.trim().toLowerCase() == 'codex';
    return AppConfigEngineModels(
      claudeModel: isCodex
          ? (claudeModel ?? currentClaudeModel)
          : (model ?? claudeModel ?? currentClaudeModel),
      codexModel: isCodex
          ? (model ?? codexModel ?? currentCodexModel)
          : (codexModel ?? currentCodexModel),
      codexReasoningEffort: isCodex
          ? (reasoningEffort ??
              codexReasoningEffort ??
              currentCodexReasoningEffort)
          : (codexReasoningEffort ?? currentCodexReasoningEffort),
    );
  }
}
