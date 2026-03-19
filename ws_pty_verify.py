#!/usr/bin/env python3
import asyncio
import json
import sys

if hasattr(sys.stdout, "reconfigure"):
    sys.stdout.reconfigure(encoding="utf-8", errors="replace")
if hasattr(sys.stderr, "reconfigure"):
    sys.stderr.reconfigure(encoding="utf-8", errors="replace")

WS_URL = "ws://127.0.0.1:8080/ws?token=test"
COMMAND = r'''python3 -c "name = input('Enter your name: '); print('Hello, ' + name)"'''

try:
    import websockets
except ImportError:
    print("缺少 websockets 库。请先安装：pip install websockets", file=sys.stderr)
    sys.exit(1)


def should_auto_input(event: dict) -> bool:
    if not isinstance(event, dict):
        return False

    if event.get("type") == "prompt_request":
        return True

    if event.get("type") == "log":
        msg = event.get("msg")
        if isinstance(msg, str):
            text = msg.lower()
            return (
                "enter your name" in text
                or "name:" in text
                or text.endswith(":")
                or text.endswith(": ")
            )

    return False


async def main():
    input_sent = False
    saw_hello = False

    async with websockets.connect(WS_URL) as ws:
        exec_payload = {
            "action": "exec",
            "mode": "pty",
            "cmd": COMMAND,
        }
        print(">>> SEND", json.dumps(exec_payload, ensure_ascii=False))
        await ws.send(json.dumps(exec_payload))

        while True:
            raw = await asyncio.wait_for(ws.recv(), timeout=30)
            print("<<< RECV", raw)

            try:
                event = json.loads(raw)
            except json.JSONDecodeError:
                continue

            if should_auto_input(event) and not input_sent:
                input_sent = True
                input_payload = {
                    "action": "input",
                    "data": "VibeCoder\n",
                }
                print(">>> SEND", json.dumps(input_payload, ensure_ascii=False))
                await ws.send(json.dumps(input_payload))

            if event.get("type") == "log" and "Hello, VibeCoder" in str(event.get("msg", "")):
                saw_hello = True
                print("已收到目标输出: Hello, VibeCoder")

            if event.get("type") == "error":
                print(f"收到错误事件: {event.get('msg')}", file=sys.stderr)

            if event.get("type") == "session_state" and event.get("state") == "closed":
                print("会话结束:", event.get("msg", ""))
                sys.exit(0 if saw_hello else 2)


if __name__ == "__main__":
    try:
        asyncio.run(main())
    except asyncio.TimeoutError:
        print("超时：未在预期时间内收到 Hello, VibeCoder", file=sys.stderr)
        sys.exit(3)
    except KeyboardInterrupt:
        print("已取消")
        sys.exit(130)


def should_auto_input(event: dict) -> bool:
    if not isinstance(event, dict):
        return False

    if event.get("type") == "prompt_request":
        return True

    if event.get("type") == "log":
        msg = event.get("msg")
        if isinstance(msg, str):
            text = msg.lower()
            return (
                "enter your name" in text
                or "name:" in text
                or text.endswith(":")
                or text.endswith(": ")
            )

    return False


async def main():
    input_sent = False
    saw_hello = False

    async with websockets.connect(WS_URL) as ws:
        exec_payload = {
            "action": "exec",
            "mode": "pty",
            "cmd": COMMAND,
        }
        print(">>> SEND", json.dumps(exec_payload, ensure_ascii=False))
        await ws.send(json.dumps(exec_payload))

        while True:
            raw = await asyncio.wait_for(ws.recv(), timeout=30)
            print("<<< RECV", raw)

            try:
                event = json.loads(raw)
            except json.JSONDecodeError:
                continue

            if should_auto_input(event) and not input_sent:
                input_sent = True
                input_payload = {
                    "action": "input",
                    "data": "VibeCoder\n",
                }
                print(">>> SEND", json.dumps(input_payload, ensure_ascii=False))
                await ws.send(json.dumps(input_payload))

            if event.get("type") == "log" and "Hello, VibeCoder" in str(event.get("msg", "")):
                saw_hello = True
                print("已收到目标输出: Hello, VibeCoder")

            if event.get("type") == "error":
                print(f"收到错误事件: {event.get('msg')}", file=sys.stderr)

            if event.get("type") == "session_state" and event.get("state") == "closed":
                print("会话结束:", event.get("msg", ""))
                sys.exit(0 if saw_hello else 2)


if __name__ == "__main__":
    try:
        asyncio.run(main())
    except asyncio.TimeoutError:
        print("超时：未在预期时间内收到 Hello, VibeCoder", file=sys.stderr)
        sys.exit(3)
    except KeyboardInterrupt:
        print("已取消")
        sys.exit(130)
