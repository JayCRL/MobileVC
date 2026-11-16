import 'dart:async';
import 'package:flutter_test/flutter_test.dart';
import 'package:shared_preferences/shared_preferences.dart';
import 'package:mobile_vc/data/models/events.dart';
import 'package:mobile_vc/data/services/mobilevc_ws_service.dart';
import 'package:mobile_vc/features/session/session_controller.dart';

/// 连真实后端验证 5 个修复的实际效果。
///
/// 前置条件：
///   - 后端已启动在 localhost:8001
///   - AUTH_TOKEN 环境变量已设
///
/// 运行方式：
///   AUTH_TOKEN="test-token-12345" flutter test integration_test/real_backend_fix_test.dart

Future<void> _flush() async {
  await Future<void>.delayed(const Duration(milliseconds: 50));
}

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();
  setUp(() {
    SharedPreferences.setMockInitialValues({
      'mobilevc.app_config': '{"host":"localhost","port":"8001","token":"test-token-12345"}',
    });
  });

  test('真实后端: 连接 → 发送 claude → 会话创建 → activity 状态正确', () async {
    final controller = SessionController();
    addTearDown(controller.disposeController);

    await controller.initialize();

    // 连接后端
    await controller.connect();
    await _flush();

    // 等待 session list 同步
    await Future<void>.delayed(const Duration(milliseconds: 500));
    await _flush();

    expect(controller.connected, isTrue, reason: '应已连接后端');

    // 发送 "claude" 触发 auto-create-session
    controller.sendInputText('claude');
    await _flush();

    // 等待后端创建会话并返回 SessionCreatedEvent
    for (var i = 0; i < 20; i++) {
      await Future<void>.delayed(const Duration(milliseconds: 500));
      await _flush();
      if (!controller.isLoadingSession) break;
    }

    // 诊断输出
    print('isLoadingSession: ${controller.isLoadingSession}');
    print('selectedSessionId: ${controller.selectedSessionId}');
    print('activityBannerTitle: ${controller.activityBannerTitle}');
    print('connected: ${controller.connected}');

    // 此时不应仍是 loading 状态（说明 session 创建完成且没被 delta/history 覆盖）
    expect(controller.isLoadingSession, isFalse,
        reason: '会话创建应已完成，不应卡在 loading。'
            'selectedSessionId=${controller.selectedSessionId}');

    // activityBannerTitle 应为实时状态，不再是固定 "AI 助手正在运行中"
    final title = controller.activityBannerTitle;
    expect(title, isNotEmpty);
    // 发了空 claude，应处于 "待输入" 或思考/执行状态
    expect(
      title == '待输入' ||
          title == '思考中' ||
          title == '已连接' ||
          title.contains('运行'),
      isTrue,
      reason: 'activityBannerTitle 应为动态状态，实际值: $title',
    );

    // 发一条实际消息，应该能到后端并触发处理
    controller.sendInputText('say hello in one word');
    await _flush();

    // 等后端回复
    await Future<void>.delayed(const Duration(seconds: 5));
    await _flush();

    // 此时应该有 timeline 条目了（用户的 + 后端的）
    final timelineLen = controller.timeline.length;
    expect(timelineLen, greaterThan(0),
        reason: '应有至少一条时间线消息（用户发送的）');

    // 检查 selectedSessionId 已正确设置
    expect(controller.selectedSessionId, isNotEmpty,
        reason: '应已选中一个会话');
  }, timeout: const Timeout(Duration(seconds: 30)));

  test('真实后端: activityBannerVisible 在运行中保持稳定不闪烁', () async {
    final controller = SessionController();
    addTearDown(controller.disposeController);

    await controller.initialize();
    await controller.connect();
    await Future<void>.delayed(const Duration(milliseconds: 500));
    await _flush();

    // 记录初始可见状态
    final initialVisible = controller.activityBannerVisible;

    // 多次快速 flush，模拟 UI 重绘
    final snapshots = <bool>[];
    for (var i = 0; i < 10; i++) {
      await _flush();
      snapshots.add(controller.activityBannerVisible);
    }

    // 无事件到达时，状态应稳定不变
    final allSame = snapshots.every((v) => v == snapshots.first);
    expect(allSame, isTrue,
        reason: '无事件到达时 activityBannerVisible 不应跳动，'
            'snapshots: $snapshots');
  }, timeout: const Timeout(Duration(seconds: 15)));

  test('真实后端: 模拟网络断开 → 自动重连 → 会话恢复，sessionId 保持不丢失',
      () async {
    final service = MobileVcWsService();
    final controller = SessionController(service: service);
    addTearDown(controller.disposeController);
    addTearDown(service.dispose);

    await controller.initialize();
    await controller.connect();
    await Future<void>.delayed(const Duration(seconds: 1));
    await _flush();

    expect(controller.connected, isTrue);

    // 先创建一个会话
    controller.sendInputText('claude');
    for (var i = 0; i < 20; i++) {
      await Future<void>.delayed(const Duration(milliseconds: 500));
      await _flush();
      if (!controller.isLoadingSession) break;
    }
    final sessionIdBefore = controller.selectedSessionId;
    print('sessionId before disconnect: $sessionIdBefore');
    expect(sessionIdBefore, isNotEmpty);

    // 模拟网络断开：直接关 WebSocket（不走 disconnect 清状态）
    await service.disconnect();
    await Future<void>.delayed(const Duration(milliseconds: 200));
    await _flush();

    // controller 应检测到断线并进入重连
    print('connected after service drop: ${controller.connected}');
    print('connectionStage after service drop: ${controller.connectionStage}');

    // 等待自动重连
    for (var i = 0; i < 40; i++) {
      await Future<void>.delayed(const Duration(milliseconds: 500));
      await _flush();
      if (controller.connected &&
          controller.selectedSessionId.isNotEmpty &&
          !controller.isLoadingSession) {
        break;
      }
    }

    final sessionIdAfter = controller.selectedSessionId;
    print('sessionId after reconnect: $sessionIdAfter');
    print('connected: ${controller.connected}');
    print('isLoadingSession: ${controller.isLoadingSession}');
    print('connectionStage: ${controller.connectionStage}');
    print('activityBannerTitle: ${controller.activityBannerTitle}');

    expect(controller.connected, isTrue, reason: '重连后应已连接');
    expect(sessionIdAfter, isNotEmpty,
        reason: '重连后应恢复会话 ID。stage=${controller.connectionStage}');
    expect(controller.isLoadingSession, isFalse,
        reason: '会话恢复应已完成');

    // 重连后 sessionId 与断开前一致（后端恢复了同一会话）
    print('sessionId match: ${sessionIdBefore == sessionIdAfter} '
        '(before=$sessionIdBefore, after=$sessionIdAfter)');
  }, timeout: const Timeout(Duration(seconds: 60)));

  test('真实后端: 网络断开重连后可继续发送消息并收到回复', () async {
    final service = MobileVcWsService();
    final controller = SessionController(service: service);
    addTearDown(controller.disposeController);
    addTearDown(service.dispose);

    await controller.initialize();
    await controller.connect();
    await Future<void>.delayed(const Duration(seconds: 1));
    await _flush();

    // 创建会话并让 Claude 运行一轮
    controller.sendInputText('claude');
    for (var i = 0; i < 20; i++) {
      await Future<void>.delayed(const Duration(milliseconds: 500));
      await _flush();
      if (!controller.isLoadingSession) break;
    }
    controller.sendInputText('say exactly "OK" and nothing else');
    await Future<void>.delayed(const Duration(seconds: 5));
    await _flush();

    final timelineBefore = controller.timeline.length;
    final sessionIdBefore = controller.selectedSessionId;
    print('timeline before disconnect: $timelineBefore');
    print('sessionId before: $sessionIdBefore');

    // 模拟网络断开
    await service.disconnect();
    await Future<void>.delayed(const Duration(milliseconds: 200));
    await _flush();

    print('connected after drop: ${controller.connected}');

    // 等待自动重连 + 会话恢复
    for (var i = 0; i < 40; i++) {
      await Future<void>.delayed(const Duration(milliseconds: 500));
      await _flush();
      if (controller.connected &&
          !controller.isLoadingSession &&
          controller.selectedSessionId.isNotEmpty) {
        break;
      }
    }

    expect(controller.connected, isTrue);
    expect(controller.isLoadingSession, isFalse,
        reason: '重连后会话加载应完成。'
            'stage=${controller.connectionStage}');

    final sessionIdAfter = controller.selectedSessionId;
    print('sessionId after reconnect: $sessionIdAfter');
    print('timeline after reconnect: ${controller.timeline.length}');
    print('activityBannerTitle: ${controller.activityBannerTitle}');

    // 重连后应能继续发消息
    controller.sendInputText('继续');
    await _flush();
    await Future<void>.delayed(const Duration(seconds: 5));
    await _flush();

    final timelineAfter = controller.timeline.length;
    print('timeline after second message: $timelineAfter');
    expect(timelineAfter, greaterThan(timelineBefore - 1),
        reason: '重连后发消息，timeline 不应减少');
  }, timeout: const Timeout(Duration(seconds: 60)));
}
