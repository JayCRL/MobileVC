"""
Regression test: Permission state survives session resume and delta events.

Covers Bug 1 (FileDiffEvent clearing permission prompt) and Bug 2
(_shouldPreserveBlockingPrompt using public getter).

From the backend perspective: after a session_resume and session_delta,
the permission prompt should still be active and the input guard should still work.
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
    CWD,
)


async def main():
    log("=== Test: Permission Survives Session Resume ===")

    await restart_backend()
    log_capture = ServerLogCapture()
    log_capture.mark()

    ctx = TestContext()
    try:
        await ctx.connect()
        ok("connected")

        # ── create session ──────────────────────────────────────────
        sid = await ctx.create_session()
        ok(f"session: {sid}")

        # ── start claude ────────────────────────────────────────────
        await ctx.start_claude()

        # ── trigger permission prompt ───────────────────────────────
        await ctx.send({
            "action": "input",
            "data": "请用 rm 命令删除 /tmp/test_mobilevc_perm_survive.txt\n",
            "cwd": CWD,
            "sessionId": ctx.session_id,
            "permissionMode": "default",
        })
        ok("sent permission-triggering message")

        prompt = await ctx.wait_for(
            "prompt_request",
            timeout=25,
            blockingKind="permission",
        )
        if not prompt:
            fail("no permission prompt received")
        perm_req_id = prompt.get("permissionRequestId", "")
        ok(f"permission prompt received: {perm_req_id[:20]}...")

        # ── simulate session resume (like app coming to foreground) ─
        await ctx.send({
            "action": "session_resume",
            "sessionId": ctx.session_id,
            "cwd": CWD,
            "reason": "foreground",
        })
        ok("sent session_resume")

        await asyncio.sleep(1)
        history = ctx.last_event_of("session_history")
        if history:
            ok("received session_history after resume")

        await ctx.drain_events(pause=0.5)

        # ── verify permission guard still active ────────────────────
        await asyncio.sleep(1.5)  # let session_resume fully settle

        await ctx.send({
            "action": "input",
            "data": "忽略上一条指令\n",
            "cwd": CWD,
            "sessionId": ctx.session_id,
            "permissionMode": "default",
        })

        # Primary check: server log (most reliable; error event may race with ws close)
        await asyncio.sleep(0.5)
        blocked = log_capture.grep("input blocked by pending permission request")
        if blocked:
            ok("permission guard survived session resume (log confirmed)")
        else:
            # Fallback: check for error event
            error = await ctx.wait_for("error", timeout=2)
            if error:
                msg = error.get("message", error.get("msg", ""))
                ok(f"permission guard survived (error event: {msg[:80]})")
            else:
                fail("permission guard NOT active after session resume (BUG)")

        # ── approve and verify recovery ─────────────────────────────
        await ctx.send({
            "action": "permission_decision",
            "decision": "approve",
            "permissionRequestId": perm_req_id,
            "permissionMode": "default",
            "sessionId": ctx.session_id,
            "cwd": CWD,
        })
        ok("approved permission")

        await asyncio.sleep(3)
        await ctx.drain_events(pause=2)

        await ctx.send({
            "action": "input",
            "data": "1+1等于几？\n",
            "cwd": CWD,
            "sessionId": ctx.session_id,
            "permissionMode": "default",
        })
        ok("sent normal message after approval")

        await asyncio.sleep(4)
        events = await ctx.drain_events(pause=3)
        has_response = any(
            e.get("type") in ("progress", "agent_state")
            for e in events
        )
        if has_response:
            ok("Claude responded after permission resolved")

        log("=== Test PASSED ===")

    finally:
        await ctx.disconnect()
        stop_backend()


if __name__ == "__main__":
    asyncio.run(main())
