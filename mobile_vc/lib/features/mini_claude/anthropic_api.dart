import 'dart:convert';

import 'package:http/http.dart' as http;

class AnthropicTool {
  final String name;
  final String description;
  final Map<String, dynamic> inputSchema;

  const AnthropicTool({
    required this.name,
    required this.description,
    required this.inputSchema,
  });

  Map<String, dynamic> toJson() => {
        'name': name,
        'description': description,
        'input_schema': inputSchema,
      };
}

class AnthropicContent {
  final String type; // text, tool_use, tool_result
  final String? text;
  final String? toolUseId;
  final String? toolName;
  final Map<String, dynamic>? toolInput;
  final String? toolResultContent;
  final bool? isError;

  const AnthropicContent._({
    required this.type,
    this.text,
    this.toolUseId,
    this.toolName,
    this.toolInput,
    this.toolResultContent,
    this.isError,
  });

  factory AnthropicContent.text(String text) =>
      AnthropicContent._(type: 'text', text: text);

  factory AnthropicContent.toolUse({
    required String id,
    required String name,
    required Map<String, dynamic> input,
  }) =>
      AnthropicContent._(
        type: 'tool_use',
        toolUseId: id,
        toolName: name,
        toolInput: input,
      );

  factory AnthropicContent.toolResult({
    required String toolUseId,
    required String content,
    bool isError = false,
  }) =>
      AnthropicContent._(
        type: 'tool_result',
        toolUseId: toolUseId,
        toolResultContent: content,
        isError: isError,
      );

  Map<String, dynamic> toJson() {
    switch (type) {
      case 'text':
        return {'type': 'text', 'text': text};
      case 'tool_use':
        return {
          'type': 'tool_use',
          'id': toolUseId,
          'name': toolName,
          'input': toolInput,
        };
      case 'tool_result':
        return {
          'type': 'tool_result',
          'tool_use_id': toolUseId,
          'content': toolResultContent,
          if (isError == true) 'is_error': true,
        };
      default:
        return {'type': type};
    }
  }

  factory AnthropicContent.fromJson(Map<String, dynamic> json) {
    final type = json['type'] as String;
    switch (type) {
      case 'text':
        return AnthropicContent.text((json['text'] ?? '').toString());
      case 'tool_use':
        return AnthropicContent.toolUse(
          id: (json['id'] ?? '').toString(),
          name: (json['name'] ?? '').toString(),
          input: Map<String, dynamic>.from(json['input'] ?? {}),
        );
      case 'tool_result':
        return AnthropicContent.toolResult(
          toolUseId: (json['tool_use_id'] ?? '').toString(),
          content: (json['content'] ?? '').toString(),
          isError: json['is_error'] == true,
        );
      default:
        return AnthropicContent._(type: type);
    }
  }
}

class AnthropicMessage {
  final String role; // user, assistant
  final List<AnthropicContent> content;

  const AnthropicMessage({required this.role, required this.content});

  Map<String, dynamic> toJson() => {
        'role': role,
        'content': content.map((c) => c.toJson()).toList(),
      };

  factory AnthropicMessage.fromJson(Map<String, dynamic> json) {
    final rawContent = json['content'];
    final List<AnthropicContent> content;
    if (rawContent is List) {
      content = rawContent
          .whereType<Map<String, dynamic>>()
          .map((c) => AnthropicContent.fromJson(c))
          .toList();
    } else if (rawContent is String) {
      content = [AnthropicContent.text(rawContent)];
    } else {
      content = [];
    }
    return AnthropicMessage(
      role: (json['role'] ?? 'user').toString(),
      content: content,
    );
  }
}

class AnthropicUsage {
  final int inputTokens;
  final int outputTokens;

  const AnthropicUsage({
    required this.inputTokens,
    required this.outputTokens,
  });

  factory AnthropicUsage.fromJson(Map<String, dynamic> json) => AnthropicUsage(
        inputTokens: (json['input_tokens'] ?? 0) as int,
        outputTokens: (json['output_tokens'] ?? 0) as int,
      );
}

class AnthropicResponse {
  final String id;
  final String stopReason;
  final AnthropicMessage message;
  final AnthropicUsage usage;

  const AnthropicResponse({
    required this.id,
    required this.stopReason,
    required this.message,
    required this.usage,
  });

  factory AnthropicResponse.fromJson(Map<String, dynamic> json) =>
      AnthropicResponse(
        id: (json['id'] ?? '').toString(),
        stopReason: (json['stop_reason'] ?? '').toString(),
        message: AnthropicMessage.fromJson(json),
        usage: AnthropicUsage.fromJson(json['usage'] ?? {}),
      );
}

class AnthropicApiException implements Exception {
  final int statusCode;
  final String body;

  const AnthropicApiException({required this.statusCode, required this.body});

  @override
  String toString() => 'AnthropicApiException($statusCode): $body';
}

class AnthropicStreamEvent {
  final String type; // text_delta, tool_input_delta, tool_start, usage, stop
  final String? text;
  final String? toolId;
  final String? toolName;
  final String? partialJson;
  final int? inputTokens;
  final int? outputTokens;
  final String? stopReason;

  const AnthropicStreamEvent._({
    required this.type,
    this.text,
    this.toolId,
    this.toolName,
    this.partialJson,
    this.inputTokens,
    this.outputTokens,
    this.stopReason,
  });

  factory AnthropicStreamEvent.textDelta(String text) =>
      AnthropicStreamEvent._(type: 'text_delta', text: text);

  factory AnthropicStreamEvent.toolStart(String id, String name) =>
      AnthropicStreamEvent._(
          type: 'tool_start', toolId: id, toolName: name);

  factory AnthropicStreamEvent.toolInputDelta(String partialJson) =>
      AnthropicStreamEvent._(
          type: 'tool_input_delta', partialJson: partialJson);

  factory AnthropicStreamEvent.usage(int input, int output) =>
      AnthropicStreamEvent._(
          type: 'usage', inputTokens: input, outputTokens: output);

  factory AnthropicStreamEvent.stop(String reason) =>
      AnthropicStreamEvent._(type: 'stop', stopReason: reason);
}

class AnthropicApi {
  final String apiKey;
  final String baseUrl;
  final http.Client _client;

  AnthropicApi({
    required this.apiKey,
    this.baseUrl = 'https://api.anthropic.com',
  }) : _client = http.Client();

  Future<AnthropicResponse> createMessage({
    required String model,
    required String system,
    required List<AnthropicMessage> messages,
    required List<AnthropicTool> tools,
    int maxTokens = 16000,
  }) async {
    final uri = Uri.parse('$baseUrl/v1/messages');

    final body = <String, dynamic>{
      'model': model,
      'max_tokens': maxTokens,
      'thinking': {'type': 'disabled'},
      'system': system,
      'messages': messages.map((m) => m.toJson()).toList(),
      'tools': tools.map((t) => t.toJson()).toList(),
    };

    final response = await _client.post(
      uri,
      headers: {
        'x-api-key': apiKey,
        'anthropic-version': '2023-06-01',
        'content-type': 'application/json',
      },
      body: jsonEncode(body),
    );

    if (response.statusCode != 200) {
      throw AnthropicApiException(
        statusCode: response.statusCode,
        body: response.body,
      );
    }

    return AnthropicResponse.fromJson(jsonDecode(response.body));
  }

  /// Streaming version — yields events as they arrive via SSE.
  Stream<AnthropicStreamEvent> createMessageStream({
    required String model,
    required String system,
    required List<AnthropicMessage> messages,
    required List<AnthropicTool> tools,
    int maxTokens = 16000,
  }) async* {
    final uri = Uri.parse('$baseUrl/v1/messages');

    final body = <String, dynamic>{
      'model': model,
      'max_tokens': maxTokens,
      'thinking': {'type': 'disabled'},
      'system': system,
      'messages': messages.map((m) => m.toJson()).toList(),
      'tools': tools.map((t) => t.toJson()).toList(),
      'stream': true,
    };

    final request = http.StreamedRequest('POST', uri);
    request.headers['x-api-key'] = apiKey;
    request.headers['anthropic-version'] = '2023-06-01';
    request.headers['content-type'] = 'application/json';
    request.sink.add(utf8.encode(jsonEncode(body)));
    request.sink.close();

    final response = await _client.send(request);

    if (response.statusCode != 200) {
      final errorBody = await response.stream.bytesToString();
      throw AnthropicApiException(
        statusCode: response.statusCode,
        body: errorBody,
      );
    }

    // Parse SSE stream
    String buffer = '';
    String currentEvent = '';
    final contentBlocks = <int, Map<String, dynamic>>{}; // index → accumulated content

    await for (final chunk in response.stream.transform(utf8.decoder)) {
      buffer += chunk;
      while (buffer.contains('\n')) {
        final newline = buffer.indexOf('\n');
        final line = buffer.substring(0, newline).trim();
        buffer = buffer.substring(newline + 1);

        if (line.isEmpty) continue;

        if (line.startsWith('event: ')) {
          currentEvent = line.substring(7).trim();
          continue;
        }

        if (line.startsWith('data: ')) {
          final data = line.substring(6);
          try {
            final json = jsonDecode(data) as Map<String, dynamic>;
            final type = json['type'] as String?;

            switch (type) {
              case 'message_start':
                final msg = json['message'] as Map<String, dynamic>?;
                final usage = msg?['usage'] as Map<String, dynamic>?;
                if (usage != null) {
                  yield AnthropicStreamEvent.usage(
                    (usage['input_tokens'] as int?) ?? 0,
                    0,
                  );
                }
                break;

              case 'content_block_start':
                final idx = json['index'] as int? ?? 0;
                final block = json['content_block'] as Map<String, dynamic>?;
                if (block != null) {
                  contentBlocks[idx] = block;
                  if (block['type'] == 'tool_use') {
                    yield AnthropicStreamEvent.toolStart(
                      (block['id'] ?? '').toString(),
                      (block['name'] ?? '').toString(),
                    );
                  }
                }
                break;

              case 'content_block_delta':
                final idx = json['index'] as int? ?? 0;
                final delta = json['delta'] as Map<String, dynamic>?;
                if (delta != null) {
                  if (delta['type'] == 'text_delta') {
                    yield AnthropicStreamEvent.textDelta(
                        (delta['text'] ?? '').toString());
                  } else if (delta['type'] == 'input_json_delta') {
                    yield AnthropicStreamEvent.toolInputDelta(
                        (delta['partial_json'] ?? '').toString());
                    // Accumulate
                    final block = contentBlocks[idx];
                    if (block != null) {
                      final existing = (block['input_str'] as String?) ?? '';
                      block['input_str'] =
                          existing + (delta['partial_json'] ?? '');
                    }
                  }
                }
                break;

              case 'content_block_stop':
                break;

              case 'message_delta':
                final delta = json['delta'] as Map<String, dynamic>?;
                final usage = json['usage'] as Map<String, dynamic>?;
                if (usage != null) {
                  yield AnthropicStreamEvent.usage(
                    (usage['input_tokens'] as int?) ?? 0,
                    (usage['output_tokens'] as int?) ?? 0,
                  );
                }
                break;

              case 'message_stop':
                yield AnthropicStreamEvent.stop('completed');
                break;
            }
          } catch (_) {
            // Skip malformed JSON lines
          }
        }
      }
    }
  }

  void dispose() {
    _client.close();
  }
}
