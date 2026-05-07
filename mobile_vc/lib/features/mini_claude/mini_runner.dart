import 'anthropic_api.dart';
import 'system_prompt.dart';
import 'tools.dart';

sealed class MiniRunnerEvent {
  const MiniRunnerEvent();
}

class MiniRunnerTextEvent extends MiniRunnerEvent {
  final String text;
  const MiniRunnerTextEvent(this.text);
}

class MiniRunnerToolCallEvent extends MiniRunnerEvent {
  final String toolName;
  final Map<String, dynamic> input;
  const MiniRunnerToolCallEvent(this.toolName, this.input);
}

class MiniRunnerToolResultEvent extends MiniRunnerEvent {
  final String toolName;
  final String content;
  final bool isError;
  const MiniRunnerToolResultEvent(this.toolName, this.content, this.isError);
}

class MiniRunnerDoneEvent extends MiniRunnerEvent {
  final int totalTokens;
  const MiniRunnerDoneEvent({required this.totalTokens});
}

class MiniRunnerErrorEvent extends MiniRunnerEvent {
  final String message;
  const MiniRunnerErrorEvent(this.message);
}

class MiniRunner {
  final AnthropicApi _api;
  final ToolRegistry _tools;
  final String _model;
  final String _systemPrompt;
  final List<AnthropicMessage> _messages = [];
  int _totalTokens = 0;

  MiniRunner({
    required String apiKey,
    required String workingDir,
    String baseUrl = 'https://api.anthropic.com',
    String model = 'claude-sonnet-4-6',
    String gitName = 'mini-claude',
    String gitEmail = 'mini@claude',
    Map<String, String> gitCredentials = const {},
  })  : _api = AnthropicApi(apiKey: apiKey, baseUrl: baseUrl),
        _tools = ToolRegistry(
          workingDir: workingDir,
          gitName: gitName,
          gitEmail: gitEmail,
          gitCredentials: gitCredentials,
        ),
        _model = model,
        _systemPrompt = buildSystemPrompt(workingDir);

  int get messageCount => _messages.length;
  int get totalTokens => _totalTokens;

  Future<void> run(String userMessage, void Function(MiniRunnerEvent) onEvent,
      {String? overrideSystemPrompt}) async {
    _addUserMessage(userMessage);

    try {
      while (true) {
        _compactIfNeeded();

        final response = await _api.createMessage(
          model: _model,
          system: overrideSystemPrompt ?? _systemPrompt,
          messages: _messages,
          tools: _tools.definitions,
        );

        _totalTokens += response.usage.inputTokens + response.usage.outputTokens;

        final content = response.message.content;

        if (content.isEmpty) {
          onEvent(MiniRunnerDoneEvent(totalTokens: _totalTokens));
          return;
        }

        // Categorize content blocks, filter out thinking/redacted
        final textBlocks = <String>[];
        final toolUses = <AnthropicContent>[];
        for (final block in content) {
          if (block.type == 'text' && (block.text ?? '').isNotEmpty) {
            textBlocks.add(block.text!);
          } else if (block.type == 'tool_use') {
            toolUses.add(block);
          }
        }

        // Emit text
        if (textBlocks.isNotEmpty) {
          final combined = textBlocks.join('\n');
          onEvent(MiniRunnerTextEvent(combined));
        }

        // Store assistant message — strip non-text/non-tool-use blocks
        final cleanContent = content
            .where((b) => b.type == 'text' || b.type == 'tool_use')
            .toList();
        _messages.add(AnthropicMessage(
          role: response.message.role,
          content: cleanContent.isNotEmpty ? cleanContent : response.message.content,
        ));

        // If no tool calls, we're done
        if (toolUses.isEmpty) {
          onEvent(MiniRunnerDoneEvent(totalTokens: _totalTokens));
          return;
        }

        // Execute tool calls and collect results
        final toolResults = <AnthropicContent>[];
        for (final toolUse in toolUses) {
          final name = toolUse.toolName!;
          final input = toolUse.toolInput!;

          onEvent(MiniRunnerToolCallEvent(name, input));

          final result = await _tools.execute(name, input);

          onEvent(MiniRunnerToolResultEvent(
            name,
            result.content,
            result.isError,
          ));

          toolResults.add(AnthropicContent.toolResult(
            toolUseId: toolUse.toolUseId!,
            content: result.content,
            isError: result.isError,
          ));
        }

        // Send tool results as a user message
        _messages.add(AnthropicMessage(role: 'user', content: toolResults));
      }
    } catch (e) {
      if (e is AnthropicApiException) {
        onEvent(MiniRunnerErrorEvent('API error (${e.statusCode}): ${e.body}'));
      } else {
        onEvent(MiniRunnerErrorEvent('$e'));
      }
      onEvent(MiniRunnerDoneEvent(totalTokens: _totalTokens));
    }
  }

  void _addUserMessage(String text) {
    _messages.add(AnthropicMessage(
      role: 'user',
      content: [AnthropicContent.text(text)],
    ));
  }

  void _compactIfNeeded() {
    const maxMessages = 40;
    if (_messages.length <= maxMessages) return;

    // Remove oldest messages, keeping the conversation going
    // Keep the first user message and the most recent 30
    final toRemove = _messages.length - 30;
    final firstUser = _messages.firstWhere(
      (m) => m.role == 'user',
      orElse: () => _messages.first,
    );
    _messages.removeWhere((m) => m != firstUser && _messages.indexOf(m) < toRemove);
  }

  void reset() {
    _messages.clear();
    _totalTokens = 0;
  }

  void dispose() {
    _api.dispose();
  }
}
