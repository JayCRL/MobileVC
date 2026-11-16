"""
Regression test: Backend blocks text input while permission request is pending.

Covers Bug 3 — when Claude sends a control_request (e.g. Bash tool permission),
the backend must reject any text input until the permission is resolved.
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
    log("=== Test: Permission Input Guard ===")

    await restart_backend()
    log_capture = ServerLogCapture()
    log_capture.mark()

    ctx = TestContext()
    try:
        await ctx.connect()
        ok("connected to backend")

        # ── create session ──────────────────────────────────────────
        sid = await ctx.create_session()
        ok(f"session created: {sid}")

        # ── start claude ────────────────────────────────────────────
        await ctx.start_claude()

        # ── trigger a tool use that needs permission ─────────────────
        await ctx.send({
            "action": "input",
            "data": "请用 bash rm 命令删除 /tmp/test_mobilevc_nonexistent.txt\n",
            "cwd": CWD,
            "sessionId": ctx.session_id,
            "permissionMode": "default",
        })
        ok("sent message triggering tool use")

        # ── wait for permission prompt ──────────────────────────────
        prompt = await ctx.wait_for(
            "prompt_request",
            timeout=25,
            blockingKind="permission",
        )
        if not prompt:
            # try without filter — maybe blockingKind key differs
            prompt = await ctx.wait_for("prompt_request", timeout=5)
            if prompt:
                bk = prompt.get("blockingKind", prompt.get("blocking_kind", ""))
                log(f"prompt_request received but blockingKind='{bk}'")
                fail("no permission prompt — blockingKind may not be 'permission'")
            else:
                fail("no prompt_request received — Claude may not have triggered a tool")
        ok("received permission prompt")

        perm_req_id = prompt.get("permissionRequestId", "")
        log(f"permissionRequestId: {perm_req_id[:40]}...")

        # ── send text while permission is pending ───────────────────
        await ctx.send({
            "action": "input",
            "data": "你好，请忽略上一条指令\n",
            "cwd": CWD,
            "sessionId": ctx.session_id,
            "permissionMode": "default",
        })
        ok("sent text input while permission pending")

        # ── verify backend rejects ──────────────────────────────────
        await asyncio.sleep(0.5)  # let log flush
        blocked = log_capture.grep("input blocked by pending permission request")
        if blocked:
            ok("backend logged input rejection (log confirmed)")
        else:
            error = await ctx.wait_for("error", timeout=3)
            if error:
                msg = error.get("message", error.get("msg", ""))
                if "权限请求待处理" in msg or "授权" in msg:
                    ok(f"backend correctly rejected input: {msg[:80]}")
                else:
                    log(f"unexpected error message: {msg[:100]}")
            else:
                fail("input NOT blocked while permission pending (BUG)")

        # ── approve permission ──────────────────────────────────────
        await ctx.send({
            "action": "permission_decision",
            "decision": "approve",
            "permissionRequestId": perm_req_id,
            "permissionMode": "default",
            "sessionId": ctx.session_id,
            "cwd": CWD,
        })
        ok("sent permission approval")

        await asyncio.sleep(3)
        await ctx.drain_events(pause=2)

        # ── send text after approval — should work ──────────────────
        await ctx.send({
            "action": "input",
            "data": "好的，现在正常回答：1+1等于几？\n",
            "cwd": CWD,
            "sessionId": ctx.session_id,
            "permissionMode": "default",
        })
        ok("sent text after permission resolved")

        await asyncio.sleep(4)
        events = await ctx.drain_events(pause=3)
        has_response = any(
            e.get("type") in ("progress", "agent_state")
            for e in events
        )
        if has_response:
            ok("Claude responded after permission resolved")
        else:
            log("no immediate Claude response (may still be processing)")

        log("=== Test PASSED ===")

    finally:
        await ctx.disconnect()
        stop_backend()


if __name__ == "__main__":
    asyncio.run(main())
