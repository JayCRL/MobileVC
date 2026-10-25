"""测试：权限提示期间断连重连后能否成功授权"""
import asyncio
import json
import websockets

WS_URL = "ws://localhost:8001/ws?token=test-token-12345"

async def recv_all(ws, timeout=4.0):
    msgs = []
    try:
        while True:
            msg = await asyncio.wait_for(ws.recv(), timeout=timeout)
            msgs.append(json.loads(msg))
    except asyncio.TimeoutError:
        pass
    return msgs

async def recv_until(ws, condition, max_wait=30.0):
    """接收消息直到 condition(m) 为 True 或超时"""
    msgs = []
    elapsed = 0
    interval = 0.5
    while elapsed < max_wait:
        try:
            msg = await asyncio.wait_for(ws.recv(), timeout=interval)
            msgs.append(json.loads(msg))
            if condition(json.loads(msg)):
                return msgs, True
        except asyncio.TimeoutError:
            pass
        elapsed += interval
    return msgs, False

def by_type(msgs, typ):
    return [m for m in msgs if m.get("type") == typ]

async def test():
    # ---- Phase 1: 新建 session + exec ----
    print("=" * 60)
    print("Phase 1: 连接 → 创建 session → exec claude")
    ws = await websockets.connect(WS_URL)
    await ws.send(json.dumps({"action": "session_create", "title": "perm-test", "cwd": "/Users/wust_lh/MobileVC"}))
    await asyncio.sleep(1)
    msgs = await recv_all(ws)
    created = by_type(msgs, "session_created")
    sid = created[0]["sessionId"]
    print(f"  sessionId = {sid}")

    await ws.send(json.dumps({
        "action": "exec", "cmd": "claude --model sonnet",
        "cwd": "/Users/wust_lh/MobileVC", "mode": "pty",
        "permissionMode": "default", "source": "command",
    }))
    await asyncio.sleep(4)
    await recv_all(ws)

    # ---- Phase 2: 发送命令，同时监听权限请求 ----
    print("\nPhase 2: 发送需要 Bash 的命令，监听权限请求")
    await ws.send(json.dumps({
        "action": "input",
        "data": "用 find 命令找出当前目录下所有 .go 文件的数量\n",
        "permissionMode": "default", "source": "command",
        "cwd": "/Users/wust_lh/MobileVC",
    }))

    # 持续接收，等待权限请求 (有 requestId 的 prompt_request)
    msgs, found = await recv_until(ws, lambda m:
        m.get("type") in ("prompt_request",) and
        m.get("requestId", "") != "", max_wait=20)

    perm_prompts = [m for m in msgs if m.get("type") in ("prompt_request",) and m.get("requestId", "")]
    print(f"  收到 {len(msgs)} 条消息，权限请求: {len(perm_prompts)}")
    had_permission = len(perm_prompts) > 0

    if had_permission:
        for p in perm_prompts:
            print(f"    requestId={p.get('requestId','')} msg={p.get('msg','')[:120]}")

        # ---- Phase 3: 断连重连 (模拟切后台) ----
        print("\nPhase 3: 断连 → 新连接 → session_resume")
        pending_rid = perm_prompts[-1].get("requestId", "")
        await ws.close()
        await asyncio.sleep(1)

        ws2 = await websockets.connect(WS_URL)
        await ws2.send(json.dumps({
            "action": "session_resume", "sessionId": sid,
            "cwd": "/Users/wust_lh/MobileVC",
            "reason": "foreground", "lastSeenEventCursor": 30,
            "lastKnownRuntimeState": "",
        }))
        await asyncio.sleep(4)
        msgs = await recv_all(ws2)
        print(f"  重连后事件: {[m.get('type') for m in msgs]}")
        for h in by_type(msgs, "session_history"):
            print(f"  runtimeAlive={h.get('runtimeAlive')}")

        # ---- Phase 4: 模拟点授权按钮 ----
        print(f"\nPhase 4: 模拟点击授权 (发送 'y' 回答 requestId={pending_rid})")
        await ws2.send(json.dumps({
            "action": "input",
            "data": "y\n",
            "permissionMode": "default", "source": "command",
            "cwd": "/Users/wust_lh/MobileVC",
        }))
        # 等待 Claude 处理权限回答并继续执行
        msgs, found = await recv_until(ws2, lambda m:
            m.get("type") in ("result", "error"), max_wait=25)

        results = [m for m in msgs if m.get("type") == "result"]
        errors = [m for m in msgs if m.get("type") == "error"]
        assistant = [m for m in msgs if m.get("type") == "assistant"]

        if results:
            for r in results:
                text = r.get("result", "")
                print(f"  [OK] Result: {text[:200]}")
                if ".go" in text.lower() or "go文件" in text.lower() or "find" in text.lower():
                    print(f"  [PASS] 授权后 Claude 正常执行并返回结果!")
                else:
                    print(f"  [CHECK] Claude 有回复，但内容需确认")
        elif errors:
            for e in errors:
                print(f"  [FAIL] Error: {e.get('msg','')[:150]}")
        else:
            print(f"  [CHECK] 收到 {len(msgs)} 条消息，无 result/error:")
            for m in msgs:
                t = m.get("type")
                if t == "assistant":
                    content = m.get("message", {}).get("content", [])
                    for c in content:
                        if c.get("type") == "text":
                            print(f"    assistant text: {c.get('text','')[:120]}")

        await ws2.close()

    else:
        print("  [SKIP] 未触发权限请求，无法测试授权流程")
        # 直接验证连通性
        print("  改为测试重连后输入是否可达 Claude...")
        await ws.close()
        await asyncio.sleep(1)
        ws2 = await websockets.connect(WS_URL)
        await ws2.send(json.dumps({
            "action": "session_resume", "sessionId": sid,
            "cwd": "/Users/wust_lh/MobileVC",
            "reason": "foreground", "lastSeenEventCursor": 20,
            "lastKnownRuntimeState": "",
        }))
        await asyncio.sleep(4)
        await recv_all(ws2)
        await ws2.send(json.dumps({
            "action": "input",
            "data": "你好，只回复 OK 两个字\n",
            "permissionMode": "default", "source": "command",
            "cwd": "/Users/wust_lh/MobileVC",
        }))
        msgs, found = await recv_until(ws2, lambda m:
            m.get("type") in ("result",), max_wait=20)
        for r in [m for m in msgs if m.get("type") == "result"]:
            text = r.get("result", "")
            print(f"  Result: {text[:120]}")
            if "OK" in text or "ok" in text.lower():
                print(f"  [PASS] 重连后输入可达 Claude!")
            else:
                print(f"  [CHECK] Claude 有了回复")
        await ws2.close()

    print("\nDone.")

asyncio.run(test())
