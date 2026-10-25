import asyncio
import websockets
import json
import sys

async def test_permission_flow():
    uri = "ws://localhost:8001/ws?token=test"

    async with websockets.connect(uri) as ws:
        print("✓ Connected to WebSocket")

        # 1. 创建会话
        create_req = {
            "action": "session_create",
            "title": "Test Permission Flow",
            "cwd": "/Users/wust_lh/MobileVC"
        }
        await ws.send(json.dumps(create_req))
        print(f"→ Sent: {create_req}")

        # 等待会话创建响应
        session_id = None
        while True:
            msg = await ws.recv()
            data = json.loads(msg)
            event_type = data.get('type', 'unknown')
            print(f"← Received: {event_type}")
            if event_type == "session_created":
                print(f"   Full data: {json.dumps(data, indent=2)}")
                session_id = data.get("sessionId") or data.get("id")
                print(f"✓ Session created: {session_id}")
                break

        if not session_id:
            print("✗ Failed to create session")
            return

        # 2. 启动 Claude CLI (使用 exec 而不是 input)
        exec_req = {
            "action": "exec",
            "cmd": "claude",
            "cwd": "/Users/wust_lh/MobileVC",
            "mode": "pty",
            "engine": "claude",
            "command": "claude",
            "permissionMode": "default"
        }
        await ws.send(json.dumps(exec_req))
        print(f"→ Sent exec: {exec_req}")

        # 等待 Claude CLI 启动
        await asyncio.sleep(2)

        # 3. 发送输入触发权限请求（使用 Edit 操作）
        input_req = {
            "action": "input",
            "data": "帮我在 /Users/wust_lh/MobileVC/hhh.txt 文件末尾添加一行：新增内容测试",
            "permissionMode": "default"
        }
        await ws.send(json.dumps(input_req))
        print(f"→ Sent input: {input_req}")

        # 4. 监听权限请求事件
        while True:
            msg = await ws.recv()
            data = json.loads(msg)
            event_type = data.get("type", "unknown")
            print(f"← Received: {event_type}")

            if event_type == "prompt_request":
                print(f"✓ Got permission prompt: {data.get('message', '')}")
                permission_request_id = data.get("runtimeMeta", {}).get("permissionRequestId", "")
                print(f"  Permission Request ID: {permission_request_id}")

                # 5. 批准权限
                decision_req = {
                    "action": "permission_decision",
                    "decision": "approve",
                    "permissionRequestId": permission_request_id
                }
                await ws.send(json.dumps(decision_req))
                print(f"→ Sent decision: {decision_req}")

            elif event_type == "error":
                print(f"✗ Error: {data.get('message', '')}")
                break

            elif event_type == "agent_state":
                state = data.get("state", "")
                message = data.get("message", "")
                print(f"  Agent state: {state} - {message}")
                if state == "idle":
                    print("✓ Agent completed")
                    break

if __name__ == "__main__":
    asyncio.run(test_permission_flow())
