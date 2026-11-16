import 'dart:async';
import 'dart:convert';
import 'dart:io';

import 'package:flutter_test/flutter_test.dart';
import 'package:mobile_vc/features/voice/voice_api_client.dart';

void main() {
  test('VoiceApiClient calls chat completions endpoint', () async {
    final server = await HttpServer.bind('127.0.0.1', 0);
    addTearDown(server.close);
    unawaited(server.first.then((request) async {
      expect(request.uri.path, '/v1/chat/completions');
      expect(request.headers.value('authorization'), 'Bearer test-key');
      final body = jsonDecode(await utf8.decoder.bind(request).join());
      expect(body['model'], 'voice-model');
      expect(body['messages'], isA<List<dynamic>>());
      request.response.headers.contentType = ContentType.json;
      request.response.write(jsonEncode({
        'choices': [
          {
            'message': {'content': '确认好了，可以开始。'},
          },
        ],
      }));
      await request.response.close();
    }));

    final client = VoiceApiClient();
    addTearDown(client.close);
    final result = await client.complete(
      apiUrl: 'http://127.0.0.1:${server.port}/v1/',
      apiKey: 'test-key',
      modelName: 'voice-model',
      messages: const [
        VoiceChatMessage(role: 'user', content: '帮我确认需求'),
      ],
    );

    expect(result.content, '确认好了，可以开始。');
  });

  test('VoiceApiClient supports responses endpoint', () async {
    final server = await HttpServer.bind('127.0.0.1', 0);
    addTearDown(server.close);
    unawaited(server.first.then((request) async {
      expect(request.uri.path, '/v1/responses');
      expect(request.headers.value('authorization'), 'Bearer test-key');
      final body = jsonDecode(await utf8.decoder.bind(request).join());
      expect(body['model'], 'voice-model');
      expect(body['instructions'], contains('系统提示'));
      expect(body['input'], isA<List<dynamic>>());
      request.response.headers.contentType = ContentType.json;
      request.response.write(jsonEncode({
        'output_text': 'Responses 可以使用。',
      }));
      await request.response.close();
    }));

    final client = VoiceApiClient();
    addTearDown(client.close);
    final result = await client.complete(
      apiUrl: 'http://127.0.0.1:${server.port}/v1/responses',
      apiKey: 'test-key',
      modelName: 'voice-model',
      messages: const [
        VoiceChatMessage(role: 'system', content: '系统提示'),
        VoiceChatMessage(role: 'user', content: '帮我确认需求'),
      ],
    );

    expect(result.content, 'Responses 可以使用。');
  });

  test('VoiceApiClient supports anthropic messages endpoint', () async {
    final server = await HttpServer.bind('127.0.0.1', 0);
    addTearDown(server.close);
    unawaited(server.first.then((request) async {
      expect(request.uri.path, '/v1/messages');
      expect(request.headers.value('x-api-key'), 'test-key');
      expect(request.headers.value('anthropic-version'), '2023-06-01');
      final body = jsonDecode(await utf8.decoder.bind(request).join());
      expect(body['model'], 'claude-sonnet');
      expect(body['system'], contains('系统提示'));
      expect(body['messages'], isA<List<dynamic>>());
      request.response.headers.contentType = ContentType.json;
      request.response.write(jsonEncode({
        'content': [
          {'type': 'text', 'text': 'Claude Messages 可以使用。'},
        ],
      }));
      await request.response.close();
    }));

    final client = VoiceApiClient();
    addTearDown(client.close);
    final result = await client.complete(
      apiUrl: 'http://127.0.0.1:${server.port}/v1/messages',
      apiKey: 'test-key',
      modelName: 'claude-sonnet',
      messages: const [
        VoiceChatMessage(role: 'system', content: '系统提示'),
        VoiceChatMessage(role: 'user', content: '帮我确认需求'),
      ],
    );

    expect(result.content, 'Claude Messages 可以使用。');
  });

  test('VoiceApiClient calls audio speech endpoint', () async {
    final server = await HttpServer.bind('127.0.0.1', 0);
    addTearDown(server.close);
    unawaited(server.first.then((request) async {
      expect(request.uri.path, '/v1/audio/speech');
      final body = jsonDecode(await utf8.decoder.bind(request).join());
      expect(body['model'], 'tts-model');
      expect(body['input'], '你好');
      expect(body['voice'], 'verse');
      request.response.headers.set('content-type', 'audio/mpeg');
      request.response.add([1, 2, 3, 4]);
      await request.response.close();
    }));

    final client = VoiceApiClient();
    addTearDown(client.close);
    final result = await client.synthesize(
      ttsUrl: 'http://127.0.0.1:${server.port}/v1/',
      apiKey: '',
      modelName: 'tts-model',
      text: '你好',
      voice: 'verse',
    );

    expect(result.contentType, 'audio/mpeg');
    expect(result.bytes, [1, 2, 3, 4]);
  });

  test('VoiceApiClient accepts json base64 tts response', () async {
    final server = await HttpServer.bind('127.0.0.1', 0);
    addTearDown(server.close);
    unawaited(server.first.then((request) async {
      request.response.headers.contentType = ContentType.json;
      request.response.write(jsonEncode({
        'audio': 'data:audio/wav;base64,${base64Encode([5, 6, 7])}',
      }));
      await request.response.close();
    }));

    final client = VoiceApiClient();
    addTearDown(client.close);
    final result = await client.synthesize(
      ttsUrl: 'http://127.0.0.1:${server.port}/v1/',
      apiKey: '',
      modelName: 'tts-model',
      text: '你好',
    );

    expect(result.contentType, 'audio/wav');
    expect(result.bytes, [5, 6, 7]);
  });

  test('VoiceApiClient supports MiMo chat completions tts response', () async {
    final server = await HttpServer.bind('127.0.0.1', 0);
    addTearDown(server.close);
    unawaited(server.first.then((request) async {
      expect(request.uri.path, '/v1/chat/completions');
      expect(request.headers.value('api-key'), 'test-key');
      expect(request.headers.value('accept'), 'application/json');
      final body = jsonDecode(await utf8.decoder.bind(request).join());
      expect(body['model'], 'mimo-v2-tts');
      expect(body['messages'], [
        {'role': 'assistant', 'content': '你好'},
      ]);
      expect(body['audio'], {
        'format': 'wav',
        'voice': 'mimo_default',
      });
      request.response.headers.contentType = ContentType.json;
      request.response.write(jsonEncode({
        'choices': [
          {
            'message': {
              'audio': {
                'data': base64Encode([8, 9, 10]),
                'format': 'wav',
              },
            },
          },
        ],
      }));
      await request.response.close();
    }));

    final client = VoiceApiClient();
    addTearDown(client.close);
    final result = await client.synthesize(
      ttsUrl: 'http://127.0.0.1:${server.port}/v1/',
      apiKey: 'test-key',
      modelName: 'mimo-v2-tts',
      text: '你好',
      voice: 'alloy',
    );

    expect(result.contentType, 'audio/wav');
    expect(result.bytes, [8, 9, 10]);
  });
}
