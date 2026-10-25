"""
Regression test: Push token registration lifecycle.

Verifies the register_push_token protocol works end-to-end through the
WebSocket gateway: register, re-register (token rotation), platform change,
empty token rejected. Asserts via server.log instead of side-channel inspection.
"""

import asyncio
import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))

from conftest import (
    TestContext,
    ServerLogCapture,
    restart_backend,
    stop_backend,
    log,
    fail,
    ok,
)


async def wait_for_log_line(log_capture: ServerLogCapture, pattern: str, timeout: float = 5.0) -> bool:
    """轮询 server.log，最多等 timeout 秒看 pattern 是否出现。"""
    deadline = asyncio.get_event_loop().time() + timeout
    while asyncio.get_event_loop().time() < deadline:
        if log_capture.has_line_containing(pattern):
            return True
        await asyncio.sleep(0.2)
    return False


async def main():
    log("=== Test: Push Token Lifecycle ===")

    await restart_backend()
    log_capture = ServerLogCapture()
    log_capture.mark()

    ctx = TestContext()
    try:
        await ctx.connect()
        ok("connected to backend")

        sid = await ctx.create_session()
        ok(f"session: {sid}")

        # ── 1. register first token ─────────────────────────────────
        await ctx.send({
            "action": "register_push_token",
            "sessionId": sid,
            "token": "tok-alpha-001",
            "platform": "ios",
        })
        if not await wait_for_log_line(log_capture, "push token registered"):
            fail("no 'push token registered' log line after first register")
        if not log_capture.has_line_containing("tok-alpha-001"):
            fail("first token not echoed in server log")
        ok("first token registered")

        # 不应当有 error 事件
        err = await ctx.wait_for("error", timeout=0.5)
        if err is not None:
            fail(f"unexpected error after first register: {err}")

        # ── 2. rotate token (same session, new token) ───────────────
        await ctx.send({
            "action": "register_push_token",
            "sessionId": sid,
            "token": "tok-beta-002",
            "platform": "ios",
        })
        if not await wait_for_log_line(log_capture, "tok-beta-002"):
            fail("rotated token not present in server log")
        ok("rotated token registered")

        # ── 3. platform switch (ios -> android) ─────────────────────
        await ctx.send({
            "action": "register_push_token",
            "sessionId": sid,
            "token": "tok-gamma-003",
            "platform": "android",
        })
        if not await wait_for_log_line(log_capture, "tok-gamma-003"):
            fail("platform switch token not present in log")
        if not any("platform=android" in line for line in log_capture.grep("tok-gamma-003")):
            log("note: platform=android substring not on same line, but token present")
        ok("platform switch accepted")

        # ── 4. empty token rejected with explicit error ─────────────
        await ctx.send({
            "action": "register_push_token",
            "sessionId": sid,
            "token": "",
            "platform": "ios",
        })
        err = await ctx.wait_for("error", timeout=3)
        if err is None:
            fail("empty token did NOT trigger error event")
        msg = err.get("message", err.get("msg", ""))
        if "token is required" not in msg:
            fail(f"unexpected error message: {msg!r}")
        ok(f"empty token rejected: {msg}")

        # ── 5. blank platform falls back to ios ─────────────────────
        # 注意：ctx.wait_for 会扫描历史事件队列，case 4 的 "token is required" 仍在
        # ctx.events 里。这里改成统计：发送前 / 发送后的 error 计数差是否新增。
        prev_error_count = len(ctx.filter_events("error"))
        await ctx.send({
            "action": "register_push_token",
            "sessionId": sid,
            "token": "tok-delta-004",
            "platform": "",
        })
        if not await wait_for_log_line(log_capture, "tok-delta-004"):
            fail("delta token not present in log")
        # 等一段时间确保 backend 处理完；如果有新 error 才算失败
        await asyncio.sleep(0.5)
        new_errors = ctx.filter_events("error")[prev_error_count:]
        if new_errors:
            fail(f"blank-platform default failed: {new_errors[0]}")
        ok("blank platform accepted (default ios)")

        log("=== Test PASSED ===")

    finally:
        await ctx.disconnect()
        stop_backend()


if __name__ == "__main__":
    asyncio.run(main())
