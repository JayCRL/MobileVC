#!/usr/bin/env node

const WebSocket = require('ws');

const WS_URL = 'ws://localhost:8001/ws?token=dev';
const SESSION_ID = 'test-session-' + Date.now();
const CWD = '/Users/wust_lh/MobileVC';

console.log('=== 连接 WebSocket ===');
const ws = new WebSocket(WS_URL);

let permissionRequestId = null;

ws.on('open', () => {
  console.log('WebSocket 已连接\n');

  // 1. 加载会话
  console.log('=== 1. 发送 session_load ===');
  ws.send(JSON.stringify({
    type: 'session_load',
    sessionId: SESSION_ID,
    cwd: CWD
  }));

  // 2. 等待 2 秒后发送消息
  setTimeout(() => {
    console.log('\n=== 2. 发送消息（触发权限请求）===');
    ws.send(JSON.stringify({
      type: 'input',
      data: '帮我读取 /Users/wust_lh/MobileVC/hhh.txt 文件',
      permissionMode: 'default'
    }));
  }, 2000);
});

ws.on('message', (data) => {
  try {
    const event = JSON.parse(data.toString());

    // 只打印关键事件
    if (event.type === 'prompt_request') {
      console.log('\n=== 收到 PromptRequestEvent ===');
      console.log('Message:', event.message);
      console.log('Choices:', event.choices);
      console.log('RuntimeMeta:', JSON.stringify(event.runtimeMeta, null, 2));

      permissionRequestId = event.runtimeMeta?.permissionRequestId;

      if (permissionRequestId) {
        // 等待 2 秒后批准权限
        setTimeout(() => {
          console.log('\n=== 3. 发送权限批准 ===');
          console.log('Permission Request ID:', permissionRequestId);
          ws.send(JSON.stringify({
            type: 'permission_decision',
            decision: 'approve',
            permissionRequestId: permissionRequestId,
            permissionMode: 'default'
          }));
        }, 2000);
      }
    } else if (event.type === 'log') {
      console.log('[LOG]', event.text?.substring(0, 100));
    } else if (event.type === 'assistant_message') {
      console.log('[ASSISTANT]', event.text?.substring(0, 100));
    } else if (event.type === 'runtime_phase') {
      console.log('[PHASE]', event.phase, event.kind, event.message);
    } else if (event.type === 'step_update') {
      console.log('[STEP]', event.tool, event.status, event.target);
    } else if (event.type === 'session_end') {
      console.log('\n=== 会话结束 ===');
      console.log('Reason:', event.reason);
      ws.close();
      process.exit(0);
    }
  } catch (e) {
    console.error('解析消息失败:', e.message);
  }
});

ws.on('error', (error) => {
  console.error('WebSocket 错误:', error.message);
  process.exit(1);
});

ws.on('close', () => {
  console.log('\nWebSocket 已关闭');
  process.exit(0);
});

// 30 秒后超时
setTimeout(() => {
  console.log('\n=== 测试超时 ===');
  ws.close();
  process.exit(1);
}, 30000);
