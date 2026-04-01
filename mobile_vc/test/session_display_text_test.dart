import 'package:flutter_test/flutter_test.dart';
import 'package:mobile_vc/data/models/runtime_meta.dart';
import 'package:mobile_vc/data/models/session_models.dart';
import 'package:mobile_vc/features/session/session_display_text.dart';

void main() {
  test('会话列表标题和预览会避开启动命令与生命周期噪声', () {
    const item = SessionSummary(
      id: 'session-1',
      title: 'codex -m gpt-5-codex --config model_reasoning_effort=high',
      lastPreview: 'command finished',
      runtime: RuntimeMeta(
        engine: 'codex',
        command: 'codex -m gpt-5-codex --config model_reasoning_effort=high',
        cwd: '/workspace/mobile_vc',
      ),
    );

    expect(sessionDisplayTitle(item), 'Codex 会话');
    expect(sessionDisplaySubtitle(item), 'Codex · gpt-5-codex · high');
    expect(sessionDisplayPreview(item), 'Codex 会话');
  });

  test('有可读上下文标题时优先展示上下文标题', () {
    const item = SessionSummary(
      id: 'session-2',
      title: '--config model_reasoning_effort=high',
      runtime: RuntimeMeta(
        engine: 'claude',
        command: 'claude --model opus',
        contextTitle: '支付页交互修复',
        cwd: '/workspace/app',
      ),
    );

    expect(sessionDisplayTitle(item), '支付页交互修复');
    expect(sessionDisplaySubtitle(item), 'Claude · Opus');
  });

  test('启动命令和 command finished 会被识别成会话噪声', () {
    expect(
      looksLikeSessionBootstrapCommand(
          'codex -m gpt-5-codex --config model_reasoning_effort=high'),
      isTrue,
    );
    expect(
        looksLikeSessionBootstrapCommand(
            '--config model_reasoning_effort=high'),
        isTrue);
    expect(looksLikeSessionNoiseText('command finished'), isTrue);
  });

  test('会话预览优先展示最后一句用户输入，并过滤模型摘要与时间戳', () {
    const item = SessionSummary(
      id: 'codex-thread:1',
      title: '2026-04-01 20:15',
      lastPreview: '修一下登录页按钮间距',
      runtime: RuntimeMeta(
        engine: 'codex',
        command: 'Codex gpt-5-codex -medium',
      ),
    );

    expect(sessionDisplayPreview(item), '修一下登录页按钮间距');
    expect(looksLikeSessionNoiseText('Codex gpt-5-codex -medium'), isTrue);
    expect(looksLikeSessionNoiseText('2026-04-01 20:15'), isTrue);
  });

  test('session 占位标题不会被当成会话文案', () {
    expect(looksLikeSessionPlaceholderTitle('session'), isTrue);
    expect(looksLikeSessionPlaceholderTitle('session-a'), isTrue);
    expect(looksLikeSessionPlaceholderTitle('Desktop Codex Session'), isFalse);
  });
}
