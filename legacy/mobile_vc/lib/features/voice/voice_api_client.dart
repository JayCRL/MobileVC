import 'dart:convert';
import 'dart:typed_data';

import 'package:http/http.dart' as http;

class VoiceChatMessage {
  const VoiceChatMessage({
    required this.role,
    required this.content,
  });

  final String role;
  final String content;

  Map<String, Object> toJson() => {
        'role': role,
        'content': content,
      };
}

class VoiceChatResult {
  const VoiceChatResult({
    required this.content,
  });

  final String content;
}

class VoiceSynthesisResult {
  const VoiceSynthesisResult({
    required this.bytes,
    required this.contentType,
  });

  final Uint8List bytes;
  final String contentType;
}

class VoiceApiException implements Exception {
  const VoiceApiException(this.message);

  final String message;

  @override
  String toString() => message;
}

class VoiceApiClient {
  VoiceApiClient({http.Client? httpClient})
      : _httpClient = httpClient ?? http.Client();

  final http.Client _httpClient;

  void close() => _httpClient.close();

  Future<VoiceChatResult> complete({
    required String apiUrl,
    required String apiKey,
    required String modelName,
    required List<VoiceChatMessage> messages,
  }) async {
    final endpoint = _resolveVoiceChatEndpoint(apiUrl);
    final model = modelName.trim();
    if (model.isEmpty) {
      throw const VoiceApiException('请先配置语音对话模型名称');
    }
    if (_isResponsesEndpoint(endpoint)) {
      return _completeResponses(
        endpoint: endpoint,
        apiKey: apiKey,
        model: model,
        messages: messages,
      );
    }
    if (_isAnthropicMessagesEndpoint(endpoint)) {
      return _completeAnthropicMessages(
        endpoint: endpoint,
        apiKey: apiKey,
        model: model,
        messages: messages,
      );
    }
    return _completeChatCompletions(
      endpoint: endpoint,
      apiKey: apiKey,
      model: model,
      messages: messages,
    );
  }

  Future<VoiceChatResult> _completeChatCompletions({
    required Uri endpoint,
    required String apiKey,
    required String model,
    required List<VoiceChatMessage> messages,
  }) async {
    final response = await _httpClient.post(
      endpoint,
      headers: _headers(apiKey),
      body: jsonEncode({
        'model': model,
        'messages': messages.map((message) => message.toJson()).toList(),
        'temperature': 0.4,
      }),
    );
    if (response.statusCode < 200 || response.statusCode >= 300) {
      throw VoiceApiException(
        '语音对话 API 返回 ${response.statusCode}: ${_bodyPreview(response.body)}',
      );
    }
    return _decodeVoiceChatResponse(response.bodyBytes);
  }

  Future<VoiceChatResult> _completeResponses({
    required Uri endpoint,
    required String apiKey,
    required String model,
    required List<VoiceChatMessage> messages,
  }) async {
    final instructions = messages
        .where((message) => message.role == 'system')
        .map((message) => message.content.trim())
        .where((content) => content.isNotEmpty)
        .join('\n\n');
    final input = messages
        .where((message) => message.role != 'system')
        .map((message) => {
              'role': message.role == 'assistant' ? 'assistant' : 'user',
              'content': message.content,
            })
        .toList();
    final response = await _httpClient.post(
      endpoint,
      headers: _headers(apiKey),
      body: jsonEncode({
        'model': model,
        if (instructions.isNotEmpty) 'instructions': instructions,
        'input': input.isEmpty ? instructions : input,
        'temperature': 0.4,
      }),
    );
    if (response.statusCode < 200 || response.statusCode >= 300) {
      throw VoiceApiException(
        '语音对话 API 返回 ${response.statusCode}: ${_bodyPreview(response.body)}',
      );
    }
    return _decodeVoiceChatResponse(response.bodyBytes);
  }

  Future<VoiceChatResult> _completeAnthropicMessages({
    required Uri endpoint,
    required String apiKey,
    required String model,
    required List<VoiceChatMessage> messages,
  }) async {
    final system = messages
        .where((message) => message.role == 'system')
        .map((message) => message.content.trim())
        .where((content) => content.isNotEmpty)
        .join('\n\n');
    final conversation = messages
        .where((message) => message.role != 'system')
        .map((message) => {
              'role': message.role == 'assistant' ? 'assistant' : 'user',
              'content': message.content,
            })
        .toList();
    final response = await _httpClient.post(
      endpoint,
      headers: _anthropicHeaders(apiKey),
      body: jsonEncode({
        'model': model,
        'max_tokens': 800,
        if (system.isNotEmpty) 'system': system,
        'messages': conversation,
        'temperature': 0.4,
      }),
    );
    if (response.statusCode < 200 || response.statusCode >= 300) {
      throw VoiceApiException(
        '语音对话 API 返回 ${response.statusCode}: ${_bodyPreview(response.body)}',
      );
    }
    return _decodeVoiceChatResponse(response.bodyBytes);
  }

  Future<VoiceSynthesisResult> synthesize({
    required String ttsUrl,
    required String apiKey,
    required String modelName,
    required String text,
    String voice = 'alloy',
  }) async {
    final model = modelName.trim();
    final input = text.trim();
    if (model.isEmpty) {
      throw const VoiceApiException('请先配置文字转语音模型名称');
    }
    if (input.isEmpty) {
      throw const VoiceApiException('没有可朗读的文本');
    }
    if (_shouldUseChatCompletionsTts(ttsUrl, model)) {
      return _synthesizeChatCompletionsTts(
        ttsUrl: ttsUrl,
        apiKey: apiKey,
        model: model,
        input: input,
        voice: voice,
      );
    }
    final endpoint = _resolveEndpoint(ttsUrl, '/audio/speech');
    final response = await _httpClient.post(
      endpoint,
      headers: {
        ..._headers(apiKey),
        'Accept': 'audio/*',
      },
      body: jsonEncode({
        'model': model,
        'input': input,
        'voice': voice.trim().isEmpty ? 'alloy' : voice.trim(),
      }),
    );
    if (response.statusCode < 200 || response.statusCode >= 300) {
      throw VoiceApiException(
        '文字转语音 API 返回 ${response.statusCode}: ${_bodyPreview(response.body)}',
      );
    }
    if (response.bodyBytes.isEmpty) {
      throw const VoiceApiException('文字转语音 API 返回了空音频');
    }
    final contentType = response.headers['content-type'] ?? 'audio/mpeg';
    if (contentType.toLowerCase().contains('application/json')) {
      return _decodeJsonAudio(response.bodyBytes);
    }
    return VoiceSynthesisResult(
      bytes: response.bodyBytes,
      contentType: contentType,
    );
  }

  Future<VoiceSynthesisResult> _synthesizeChatCompletionsTts({
    required String ttsUrl,
    required String apiKey,
    required String model,
    required String input,
    required String voice,
  }) async {
    final endpoint = _resolveEndpoint(ttsUrl, '/chat/completions');
    final isMimo = _isMimoTts(endpoint, model);
    final response = await _httpClient.post(
      endpoint,
      headers: _chatCompletionsTtsHeaders(apiKey, isMimo: isMimo),
      body: jsonEncode({
        'model': model,
        'messages': [
          {
            'role': 'assistant',
            'content': input,
          },
        ],
        'audio': {
          'format': 'wav',
          'voice': _chatCompletionsTtsVoice(voice, isMimo: isMimo),
        },
      }),
    );
    if (response.statusCode < 200 || response.statusCode >= 300) {
      throw VoiceApiException(
        '文字转语音 API 返回 ${response.statusCode}: ${_bodyPreview(response.body)}',
      );
    }
    if (response.bodyBytes.isEmpty) {
      throw const VoiceApiException('文字转语音 API 返回了空音频');
    }
    return _decodeChatCompletionsAudio(response.bodyBytes);
  }

  static Map<String, String> _headers(String apiKey) {
    final headers = <String, String>{
      'Content-Type': 'application/json',
    };
    final key = apiKey.trim();
    if (key.isNotEmpty) {
      headers['Authorization'] = 'Bearer $key';
    }
    return headers;
  }

  static Map<String, String> _anthropicHeaders(String apiKey) {
    final headers = <String, String>{
      'Content-Type': 'application/json',
      'anthropic-version': '2023-06-01',
    };
    final key = apiKey.trim();
    if (key.isNotEmpty) {
      headers['x-api-key'] = key;
      headers['Authorization'] = 'Bearer $key';
    }
    return headers;
  }

  static Map<String, String> _chatCompletionsTtsHeaders(
    String apiKey, {
    required bool isMimo,
  }) {
    final headers = <String, String>{
      'Content-Type': 'application/json',
      'Accept': 'application/json',
    };
    final key = apiKey.trim();
    if (key.isEmpty) {
      return headers;
    }
    if (isMimo) {
      headers['api-key'] = key;
    } else {
      headers['Authorization'] = 'Bearer $key';
    }
    return headers;
  }

  static Uri _resolveVoiceChatEndpoint(String rawUrl) {
    final trimmed = rawUrl.trim();
    if (trimmed.isEmpty) {
      throw const VoiceApiException('请先配置 API URL');
    }
    final uri = Uri.parse(trimmed);
    final path = _withoutTrailingSlashes(uri.path);
    if (path.endsWith('/responses') || path.endsWith('/messages')) {
      return uri;
    }
    return _resolveEndpoint(rawUrl, '/chat/completions');
  }

  static Uri _resolveEndpoint(String rawUrl, String defaultPath) {
    final trimmed = rawUrl.trim();
    if (trimmed.isEmpty) {
      throw const VoiceApiException('请先配置 API URL');
    }
    final uri = Uri.parse(trimmed);
    final path = _withoutTrailingSlashes(uri.path);
    if (path.endsWith(defaultPath) || uri.path.endsWith(defaultPath)) {
      return uri;
    }
    if (path.isEmpty) {
      return uri.replace(path: defaultPath);
    }
    if (path.endsWith('/v1')) {
      return uri.replace(path: '$path$defaultPath');
    }
    return uri;
  }

  static bool _isResponsesEndpoint(Uri endpoint) {
    return _withoutTrailingSlashes(endpoint.path).endsWith('/responses');
  }

  static bool _isAnthropicMessagesEndpoint(Uri endpoint) {
    return _withoutTrailingSlashes(endpoint.path).endsWith('/messages');
  }

  static bool _shouldUseChatCompletionsTts(String rawUrl, String modelName) {
    final trimmed = rawUrl.trim();
    if (trimmed.isEmpty) {
      throw const VoiceApiException('请先配置 API URL');
    }
    final uri = Uri.parse(trimmed);
    final path = _withoutTrailingSlashes(uri.path);
    return path.endsWith('/chat/completions') ||
        _isMimoTts(uri, modelName.trim());
  }

  static bool _isMimoTts(Uri endpoint, String modelName) {
    final host = endpoint.host.toLowerCase();
    final model = modelName.trim().toLowerCase();
    return host.contains('xiaomimimo.com') ||
        (model.startsWith('mimo-') && model.contains('-tts'));
  }

  static String _chatCompletionsTtsVoice(
    String voice, {
    required bool isMimo,
  }) {
    final trimmed = voice.trim();
    if (isMimo && (trimmed.isEmpty || trimmed == 'alloy')) {
      return 'mimo_default';
    }
    if (trimmed.isEmpty) {
      return isMimo ? 'mimo_default' : 'alloy';
    }
    return trimmed;
  }

  static String _withoutTrailingSlashes(String value) {
    var result = value.trim();
    while (result.endsWith('/')) {
      result = result.substring(0, result.length - 1);
    }
    return result;
  }

  static VoiceChatResult _decodeVoiceChatResponse(List<int> bodyBytes) {
    final decoded = jsonDecode(utf8.decode(bodyBytes));
    if (decoded is! Map<String, dynamic>) {
      throw const VoiceApiException('语音对话 API 返回格式不是 JSON object');
    }
    final directContent = decoded['content'];
    if (directContent is String && directContent.trim().isNotEmpty) {
      return VoiceChatResult(content: directContent.trim());
    }
    if (directContent is List) {
      final joined = _contentPartsToText(directContent);
      if (joined.trim().isNotEmpty) {
        return VoiceChatResult(content: joined.trim());
      }
    }
    final outputText = decoded['output_text'];
    if (outputText is String && outputText.trim().isNotEmpty) {
      return VoiceChatResult(content: outputText.trim());
    }
    final output = decoded['output'];
    if (output is List) {
      final joined = _outputItemsToText(output);
      if (joined.trim().isNotEmpty) {
        return VoiceChatResult(content: joined.trim());
      }
    }
    final choices = decoded['choices'];
    if (choices is List && choices.isNotEmpty) {
      final joined = _choicesToText(choices);
      if (joined.trim().isNotEmpty) {
        return VoiceChatResult(content: joined.trim());
      }
    }
    throw const VoiceApiException('语音对话 API 没有返回可显示的回复');
  }

  static String _contentPartsToText(List<dynamic> parts) {
    final lines = <String>[];
    for (final part in parts) {
      if (part is String) {
        lines.add(part);
        continue;
      }
      if (part is Map<String, dynamic>) {
        final text = part['text'];
        if (text is String) {
          lines.add(text);
          continue;
        }
        final content = part['content'];
        if (content is String) {
          lines.add(content);
        }
      }
    }
    return lines.join('\n');
  }

  static String _outputItemsToText(List<dynamic> items) {
    final lines = <String>[];
    for (final item in items) {
      if (item is String) {
        lines.add(item);
        continue;
      }
      if (item is Map<String, dynamic>) {
        final text = item['text'];
        if (text is String) {
          lines.add(text);
          continue;
        }
        final content = item['content'];
        if (content is String) {
          lines.add(content);
          continue;
        }
        if (content is List) {
          final joined = _contentPartsToText(content);
          if (joined.trim().isNotEmpty) {
            lines.add(joined);
          }
        }
      }
    }
    return lines.join('\n');
  }

  static String _choicesToText(List<dynamic> choices) {
    final lines = <String>[];
    for (final choice in choices) {
      if (choice is! Map<String, dynamic>) {
        continue;
      }
      final message = choice['message'];
      if (message is Map<String, dynamic>) {
        final content = message['content'];
        if (content is String) {
          lines.add(content);
          continue;
        }
        if (content is List) {
          final joined = _contentPartsToText(content);
          if (joined.trim().isNotEmpty) {
            lines.add(joined);
            continue;
          }
        }
      }
      final text = choice['text'];
      if (text is String) {
        lines.add(text);
      }
    }
    return lines.join('\n');
  }

  static VoiceSynthesisResult _decodeJsonAudio(List<int> bytes) {
    final decoded = jsonDecode(utf8.decode(bytes));
    if (decoded is! Map<String, dynamic>) {
      throw const VoiceApiException('文字转语音 JSON 返回格式不是 object');
    }
    final audioValue = decoded['audio'] ?? decoded['data'] ?? decoded['bytes'];
    if (audioValue is! String || audioValue.trim().isEmpty) {
      throw const VoiceApiException('文字转语音 JSON 没有 audio/data 字段');
    }
    final parsed = _decodeBase64Audio(audioValue.trim());
    final mime = (decoded['contentType'] ?? decoded['mimeType'] ?? parsed.mime)
        .toString()
        .trim();
    return VoiceSynthesisResult(
      bytes: parsed.bytes,
      contentType: mime.isEmpty ? 'audio/wav' : mime,
    );
  }

  static VoiceSynthesisResult _decodeChatCompletionsAudio(List<int> bytes) {
    final decoded = jsonDecode(utf8.decode(bytes));
    if (decoded is! Map<String, dynamic>) {
      throw const VoiceApiException('文字转语音 JSON 返回格式不是 object');
    }
    final choices = decoded['choices'];
    if (choices is List && choices.isNotEmpty) {
      final first = choices.first;
      if (first is Map<String, dynamic>) {
        final result = _decodeMessageAudio(first['message']) ??
            _decodeMessageAudio(first['delta']);
        if (result != null) {
          return result;
        }
      }
    }
    return _decodeJsonAudio(bytes);
  }

  static VoiceSynthesisResult? _decodeMessageAudio(Object? message) {
    if (message is! Map<String, dynamic>) {
      return null;
    }
    final audio = message['audio'];
    if (audio is String && audio.trim().isNotEmpty) {
      final parsed = _decodeBase64Audio(audio.trim());
      return VoiceSynthesisResult(
          bytes: parsed.bytes, contentType: parsed.mime);
    }
    if (audio is! Map<String, dynamic>) {
      return null;
    }
    final data = audio['data'];
    if (data is! String || data.trim().isEmpty) {
      return null;
    }
    final parsed = _decodeBase64Audio(data.trim());
    final contentType = _audioContentType(
      format: audio['format']?.toString(),
      explicit:
          audio['contentType']?.toString() ?? audio['mimeType']?.toString(),
      fallback: parsed.mime,
    );
    return VoiceSynthesisResult(
      bytes: parsed.bytes,
      contentType: contentType,
    );
  }

  static String _audioContentType({
    required String? format,
    required String? explicit,
    required String fallback,
  }) {
    final contentType = explicit?.trim() ?? '';
    if (contentType.isNotEmpty) {
      return contentType;
    }
    switch (format?.trim().toLowerCase()) {
      case 'wav':
        return 'audio/wav';
      case 'mp3':
      case 'mpeg':
        return 'audio/mpeg';
      case 'pcm16':
      case 'pcm':
        return 'audio/pcm';
    }
    return fallback.isEmpty ? 'audio/wav' : fallback;
  }

  static _DecodedAudio _decodeBase64Audio(String value) {
    final dataPrefixMatch =
        RegExp(r'^data:([^;,]+);base64,(.*)$', dotAll: true).firstMatch(value);
    if (dataPrefixMatch != null) {
      return _DecodedAudio(
        bytes: base64Decode(dataPrefixMatch.group(2)!),
        mime: dataPrefixMatch.group(1) ?? 'audio/wav',
      );
    }
    return _DecodedAudio(bytes: base64Decode(value), mime: 'audio/wav');
  }

  static String _bodyPreview(String body) {
    final trimmed = body.trim();
    if (trimmed.length <= 300) {
      return trimmed;
    }
    return '${trimmed.substring(0, 300)}...';
  }
}

class _DecodedAudio {
  const _DecodedAudio({
    required this.bytes,
    required this.mime,
  });

  final Uint8List bytes;
  final String mime;
}
