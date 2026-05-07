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

  void dispose() {
    _client.close();
  }
}
