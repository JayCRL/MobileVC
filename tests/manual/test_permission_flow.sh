#!/bin/bash

# 测试权限确认流程
# 模拟 Flutter 端的行为

BASE_URL="http://localhost:8001"
SESSION_ID=""

echo "=== 1. 创建会话 ==="
RESPONSE=$(curl -s -X POST "$BASE_URL/api/sessions" \
  -H "Content-Type: application/json" \
  -d '{
    "deviceId": "test-device",
    "deviceName": "Test Device",
    "platform": "ios"
  }')

SESSION_ID=$(echo "$RESPONSE" | jq -r '.sessionId')
echo "Session ID: $SESSION_ID"

if [ -z "$SESSION_ID" ] || [ "$SESSION_ID" = "null" ]; then
  echo "Failed to create session"
  exit 1
fi

echo ""
echo "=== 2. 发送消息（触发权限请求）==="
curl -s -X POST "$BASE_URL/api/sessions/$SESSION_ID/messages" \
  -H "Content-Type: application/json" \
  -d '{
    "content": "read the file /Users/wust_lh/MobileVC/README.md"
  }' | jq '.'

echo ""
echo "=== 3. 等待 5 秒，让 Claude CLI 发送 control_request ==="
sleep 5

echo ""
echo "=== 4. 检查后端日志中的 control_request ==="
tail -50 /Users/wust_lh/MobileVC/server.log | grep -A 5 "control_request"

echo ""
echo "=== 5. 模拟用户批准权限 ==="
# 从日志中提取 permissionRequestId
PERMISSION_REQUEST_ID=$(tail -100 /Users/wust_lh/MobileVC/server.log | grep -o 'permissionRequestID=[^ ]*' | tail -1 | cut -d= -f2 | tr -d '"')

if [ -z "$PERMISSION_REQUEST_ID" ]; then
  echo "No permission request ID found in logs"
  echo "Checking for cached control request ID..."
  PERMISSION_REQUEST_ID=$(tail -100 /Users/wust_lh/MobileVC/server.log | grep -o 'requestID=[^ ]*' | tail -1 | cut -d= -f2 | tr -d '"')
fi

echo "Permission Request ID: $PERMISSION_REQUEST_ID"

if [ -n "$PERMISSION_REQUEST_ID" ] && [ "$PERMISSION_REQUEST_ID" != "null" ]; then
  curl -s -X POST "$BASE_URL/api/sessions/$SESSION_ID/permission-decision" \
    -H "Content-Type: application/json" \
    -d "{
      \"decision\": \"allow\",
      \"permissionRequestId\": \"$PERMISSION_REQUEST_ID\"
    }" | jq '.'
else
  echo "No permission request ID, skipping decision"
fi

echo ""
echo "=== 6. 等待 3 秒，检查 Claude CLI 是否继续执行 ==="
sleep 3

echo ""
echo "=== 7. 检查后端日志中的 control_response 和后续输出 ==="
tail -50 /Users/wust_lh/MobileVC/server.log | grep -A 10 "control_response\|tool_result"

echo ""
echo "=== 测试完成 ==="
