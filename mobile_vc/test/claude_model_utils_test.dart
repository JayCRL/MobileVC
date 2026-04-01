import 'package:flutter_test/flutter_test.dart';
import 'package:mobile_vc/features/session/claude_model_utils.dart';

void main() {
  group('Claude model utils', () {
    test('normalizes official aliases and friendly variants', () {
      expect(normalizeClaudeModelSelection(''), 'sonnet');
      expect(normalizeClaudeModelSelection('default'), 'default');
      expect(normalizeClaudeModelSelection('Opus Plan'), 'opusplan');
      expect(normalizeClaudeModelSelection('sonnet-1m'), 'sonnet[1m]');
      expect(normalizeClaudeModelSelection('OPUS [1M]'), 'opus[1m]');
      expect(
        normalizeClaudeModelSelection('claude-sonnet-4-20250514'),
        'claude-sonnet-4-20250514',
      );
    });

    test('keeps custom model names when they are not known aliases', () {
      expect(
        normalizeClaudeModelSelection('my-custom-claude-profile'),
        'my-custom-claude-profile',
      );
    });

    test('formats known aliases into UI labels', () {
      expect(claudeModelDisplayLabel('default'), 'Default');
      expect(claudeModelDisplayLabel('sonnet'), 'Sonnet');
      expect(claudeModelDisplayLabel('sonnet[1m]'), 'Sonnet 1M');
      expect(claudeModelDisplayLabel('opus'), 'Opus');
      expect(claudeModelDisplayLabel('opus[1m]'), 'Opus 1M');
      expect(claudeModelDisplayLabel('haiku'), 'Haiku');
      expect(claudeModelDisplayLabel('opusplan'), 'Opus Plan');
    });

    test('parses aliases and pinned models from text', () {
      expect(parseClaudeModelFromText('Model set to opusplan'), 'opusplan');
      expect(parseClaudeModelFromText('active_ai: sonnet[1m]'), 'sonnet[1m]');
      expect(
        parseClaudeModelFromText('using claude-sonnet-4-20250514'),
        'claude-sonnet-4-20250514',
      );
    });
  });
}
