import 'dart:convert';

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

class MiniRunnerUsageEvent extends MiniRunnerEvent {
  final int inputTokens;
  final int outputTokens;
  final int totalTokens;
  const MiniRunnerUsageEvent({
    required this.inputTokens,
    required this.outputTokens,
    required this.totalTokens,
  });
}

class MiniRunnerDoneEvent extends MiniRunnerEvent {
  final int totalTokens;
  final int elapsedMs;
  const MiniRunnerDoneEvent({
    required this.totalTokens,
    required this.elapsedMs,
  });
}

class MiniRunnerErrorEvent extends MiniRunnerEvent {
  final String message;
  const MiniRunnerErrorEvent(this.message);
}

class MiniRunnerCancelled implements Exception {}

class MiniRunner {
  final AnthropicApi _api;
  final ToolRegistry _tools;
  final String _model;
  final String _systemPrompt;
  final List<AnthropicMessage> _messages = [];
  int _totalTokens = 0;
  int _inputTokens = 0;
  int _outputTokens = 0;
  bool _cancelled = false;

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

  int get totalTokens => _totalTokens;

  void cancel() {
    _cancelled = true;
  }

  void _checkCancelled() {
    if (_cancelled) throw MiniRunnerCancelled();
  }

  Future<void> run(String userMessage, void Function(MiniRunnerEvent) onEvent,
      {String? overrideSystemPrompt}) async {
    _addUserMessage(userMessage);
    _inputTokens = 0;
    _outputTokens = 0;

    try {
      while (true) {
        _checkCancelled();
        _compactIfNeeded();

        final textBuf = StringBuffer();
        final toolUses = <_StreamToolUse>[];
        _StreamToolUse? currentTool;
        int turnInputTokens = 0;
        int turnOutputTokens = 0;

        final stream = _api.createMessageStream(
          model: _model,
          system: overrideSystemPrompt ?? _systemPrompt,
          messages: _messages,
          tools: _tools.definitions,
        );

        await for (final event in stream) {
          switch (event.type) {
            case 'text_delta':
              textBuf.write(event.text);
              onEvent(MiniRunnerTextEvent(event.text!));
              break;

            case 'tool_start':
              currentTool = _StreamToolUse(
                id: event.toolId!,
                name: event.toolName!,
                jsonBuf: StringBuffer(),
              );
              break;

            case 'tool_input_delta':
              currentTool?.jsonBuf.write(event.partialJson);
              break;

            case 'usage':
              if (event.inputTokens != null && event.inputTokens! > 0) {
                turnInputTokens = event.inputTokens!;
              }
              if (event.outputTokens != null && event.outputTokens! > 0) {
                turnOutputTokens = event.outputTokens!;
              }
              _inputTokens = turnInputTokens;
              _outputTokens = turnOutputTokens;
              _totalTokens += turnInputTokens + turnOutputTokens;
              onEvent(MiniRunnerUsageEvent(
                inputTokens: turnInputTokens,
                outputTokens: turnOutputTokens,
                totalTokens: _totalTokens,
              ));
              break;

            case 'stop':
              if (currentTool != null) {
                toolUses.add(currentTool);
              }
              break;
          }
        }

        // Parse tool inputs from accumulated JSON
        if (textBuf.isEmpty && toolUses.isEmpty) {
          onEvent(MiniRunnerDoneEvent(
            totalTokens: _totalTokens,
            elapsedMs: 0,
          ));
          return;
        }

        // Store assistant message
        final contentBlocks = <AnthropicContent>[];
        if (textBuf.isNotEmpty) {
          contentBlocks.add(AnthropicContent.text(textBuf.toString()));
        }
        for (final tu in toolUses) {
          try {
            final input = jsonDecode(tu.jsonBuf.toString()) as Map<String, dynamic>;
            contentBlocks.add(AnthropicContent.toolUse(
              id: tu.id,
              name: tu.name,
              input: input,
            ));
          } catch (_) {
            contentBlocks.add(AnthropicContent.toolUse(
              id: tu.id,
              name: tu.name,
              input: {},
            ));
          }
        }
        _messages.add(AnthropicMessage(
            role: 'assistant', content: contentBlocks));

        // If no tool calls, done
        if (toolUses.isEmpty) {
          onEvent(MiniRunnerDoneEvent(
            totalTokens: _totalTokens,
            elapsedMs: 0,
          ));
          return;
        }

        // Execute tools
        final toolResults = <AnthropicContent>[];
        for (final tu in toolUses) {
          final name = tu.name;
          Map<String, dynamic> input;
          try {
            input = jsonDecode(tu.jsonBuf.toString()) as Map<String, dynamic>;
          } catch (_) {
            input = {};
          }

          onEvent(MiniRunnerToolCallEvent(name, input));

          _checkCancelled();
          final result = await _tools.execute(name, input);

          onEvent(MiniRunnerToolResultEvent(
            name,
            result.content,
            result.isError,
          ));

          toolResults.add(AnthropicContent.toolResult(
            toolUseId: tu.id,
            content: result.content,
            isError: result.isError,
          ));
        }

        _messages.add(AnthropicMessage(role: 'user', content: toolResults));
      }
    } catch (e) {
      if (e is MiniRunnerCancelled) {
        onEvent(MiniRunnerTextEvent('\n\n[已停止]'));
      } else if (e is AnthropicApiException) {
        onEvent(MiniRunnerErrorEvent('API error (${e.statusCode}): ${e.body}'));
      } else {
        onEvent(MiniRunnerErrorEvent('$e'));
      }
      onEvent(MiniRunnerDoneEvent(
        totalTokens: _totalTokens,
        elapsedMs: 0,
      ));
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
    final toRemove = _messages.length - 30;
    final firstUser = _messages.firstWhere(
      (m) => m.role == 'user',
      orElse: () => _messages.first,
    );
    _messages.removeWhere(
        (m) => m != firstUser && _messages.indexOf(m) < toRemove);
  }

  void reset() {
    _messages.clear();
    _totalTokens = 0;
    _cancelled = false;
  }

  void dispose() {
    _api.dispose();
  }
}

class _StreamToolUse {
  final String id;
  final String name;
  final StringBuffer jsonBuf;
  _StreamToolUse({
    required this.id,
    required this.name,
    required this.jsonBuf,
  });
}
