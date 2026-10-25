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

  test('电脑 Codex 原生会话统一显示来源标题并保留原始问题文案', () {
    const item = SessionSummary(
      id: 'codex-thread:1',
      title: '这里有问题不能删除mobilevc喃',
      lastPreview: '安卓上显示了 3 次',
      source: 'codex-native',
      external: true,
      runtime: RuntimeMeta(
        source: 'codex-native',
        engine: 'codex',
        cwd: '/workspace/MobileVC',
      ),
    );

    expect(sessionDisplayTitle(item), '电脑 Codex');
    expect(sessionDisplayPreview(item), '安卓上显示了 3 次');
    expect(sessionDisplaySubtitle(item), '安卓上显示了 3 次');
    expect(sessionSourceLabel(item), '电脑 Codex');
  });

  test('电脑 Codex 原生会话没有 preview 时副标题展示原始标题', () {
    const item = SessionSummary(
      id: 'codex-thread:2',
      title: '这里有问题不能删除mobilevc喃',
      source: 'codex-native',
      external: true,
      runtime: RuntimeMeta(source: 'codex-native', engine: 'codex'),
    );

    expect(sessionDisplayTitle(item), '电脑 Codex');
    expect(sessionDisplaySubtitle(item), '这里有问题不能删除mobilevc喃');
  });

  test('电脑 Claude 原生会话统一显示来源标题', () {
    const item = SessionSummary(
      id: 'claude-session:native-1',
      title: '修复 Relay 断开',
      source: 'claude-native',
      runtime: RuntimeMeta(source: 'claude-native', engine: 'claude'),
    );

    expect(sessionDisplayTitle(item), '电脑 Claude');
    expect(sessionDisplaySubtitle(item), '修复 Relay 断开');
    expect(sessionSourceLabel(item), '电脑 Claude');
  });

  test('明确 Codex source 优先于旧 claude ownership', () {
    const item = SessionSummary(
      id: 'codex-thread:stale-ownership',
      title: '修复会话来源',
      source: 'codex-native',
      ownership: 'claude-native',
      external: true,
      runtime: RuntimeMeta(source: 'codex-native', engine: 'codex'),
    );

    expect(sessionDisplayTitle(item), '电脑 Codex');
    expect(sessionSourceLabel(item), '电脑 Codex');
  });

  test('Codex mirror id 优先于旧 claude runtime source', () {
    const item = SessionSummary(
      id: 'codex-thread:stale-runtime',
      title: '修复会话来源',
      external: true,
      runtime: RuntimeMeta(source: 'claude-native', engine: 'claude'),
    );

    expect(sessionNativeSource(item), 'codex-native');
    expect(sessionDisplayTitle(item), '电脑 Codex');
    expect(sessionSourceLabel(item), '电脑 Codex');
  });

  test('未知 external 会话默认按电脑 Codex 显示', () {
    const item = SessionSummary(
      id: 'external-without-provider',
      title: '外部会话',
      external: true,
    );

    expect(sessionNativeSource(item), 'codex-native');
    expect(sessionDisplayTitle(item), '电脑 Codex');
    expect(sessionDisplaySubtitle(item), '外部会话');
  });

  test('MobileVC 自有 Codex 会话不会因为镜像 ID 前缀被显示成电脑会话', () {
    const item = SessionSummary(
      id: 'codex-thread:mobilevc-1',
      title: 'MobileVC 创建的 Codex 会话',
      source: 'mobilevc',
      ownership: 'mobilevc',
      runtime: RuntimeMeta(source: 'mobilevc', engine: 'codex'),
    );

    expect(sessionDisplayTitle(item), 'MobileVC 创建的 Codex 会话');
    expect(sessionSourceLabel(item), 'MobileVC');
  });

  test('MobileVC ownership 优先于旧 native runtime source', () {
    const item = SessionSummary(
      id: 'session-mobilevc-stale-runtime',
      title: 'MobileVC Codex',
      source: 'mobilevc',
      ownership: 'mobilevc',
      runtime: RuntimeMeta(source: 'claude-native', engine: 'codex'),
    );

    expect(sessionDisplayTitle(item), 'MobileVC Codex');
    expect(sessionSourceLabel(item), 'MobileVC');
  });

  test('session 占位标题不会被当成会话文案', () {
    expect(looksLikeSessionPlaceholderTitle('session'), isTrue);
    expect(looksLikeSessionPlaceholderTitle('session-a'), isTrue);
    expect(looksLikeSessionPlaceholderTitle('Desktop Codex Session'), isFalse);
  });
}
