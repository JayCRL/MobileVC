"""
Shared test infrastructure for MobileVC regression tests.

Connects to the local backend via WebSocket and provides helpers
for session creation, message sending, and event waiting.
"""

import asyncio
import json
import os
import sys
import time
import signal
import subprocess
from dataclasses import dataclass, field
from datetime import datetime
from pathlib import Path

import websockets
from websockets.asyncio.client import ClientConnection

# ── config ──────────────────────────────────────────────────────────────
BACKEND_HOST = os.environ.get("MOBILEVC_HOST", "localhost")
BACKEND_PORT = os.environ.get("MOBILEVC_PORT", "8001")
AUTH_TOKEN = os.environ.get("AUTH_TOKEN", "test-token-12345")
CWD = os.environ.get("MOBILEVC_CWD", str(Path(__file__).resolve().parents[2]))
WS_URL = f"ws://{BACKEND_HOST}:{BACKEND_PORT}/ws?token={AUTH_TOKEN}"

PROJECT_ROOT = Path(__file__).resolve().parents[2]
SERVER_LOG = PROJECT_ROOT / "server.log"
SERVER_BIN = PROJECT_ROOT / "server"

# ── helpers ─────────────────────────────────────────────────────────────

@dataclass
class TestContext:
    """Holds connection state and received events during a test."""
    ws: ClientConnection | None = None
    session_id: str = ""
    events: list[dict] = field(default_factory=list)
    _event_queue: asyncio.Queue = field(default_factory=asyncio.Queue)
    _listen_task: asyncio.Task | None = None

    async def connect(self) -> None:
        self.ws = await websockets.connect(WS_URL, ping_interval=15)
        self._listen_task = asyncio.create_task(self._listen())

    async def _listen(self) -> None:
        assert self.ws is not None
        async for raw in self.ws:
            try:
                event = json.loads(raw)
                self.events.append(event)
                await self._event_queue.put(event)
            except json.JSONDecodeError:
                continue

    async def create_session(self, cwd: str = CWD) -> str:
        """Create a new session and return the session ID."""
        await self.send({
            "action": "session_create",
            "cwd": cwd,
        })

        created = await self.wait_for("session_created", timeout=10)
        if not created:
            fail("session_created not received after session_create")
        sid = (created.get("summary", {}) or {}).get("id", "") or created.get("sessionId", "")
        if not sid:
            fail("session_created missing session id")
        self.session_id = sid
        return sid

    async def start_claude(
        self,
        cwd: str = CWD,
        model: str = "",
        permission_mode: str = "default",
    ) -> None:
        """Start a Claude runner in the current session."""
        cmd = "claude"
        if model:
            cmd += f" --model {model}"
        await self.send({
            "action": "exec",
            "cmd": cmd,
            "cwd": cwd,
            "mode": "pty",
            "sessionId": self.session_id,
            "permissionMode": permission_mode,
        })
        # Wait for Claude to initialize
        await asyncio.sleep(2)
        await self.drain_events(pause=1)

    async def disconnect(self) -> None:
        if self._listen_task:
            self._listen_task.cancel()
            try:
                await self._listen_task
            except asyncio.CancelledError:
                pass
        if self.ws:
            await self.ws.close()
            self.ws = None

    async def send(self, payload: dict) -> None:
        assert self.ws is not None
        await self.ws.send(json.dumps(payload))

    async def wait_for(
        self,
        event_type: str,
        timeout: float = 15.0,
        **kwargs,
    ) -> dict | None:
        """Wait for an event of the given type, optionally matching extra fields."""
        deadline = time.monotonic() + timeout
        # check already-received events first
        for event in self.events:
            if event.get("type") == event_type and _match_kwargs(event, kwargs):
                return event
        # wait for new events
        while time.monotonic() < deadline:
            try:
                event = await asyncio.wait_for(
                    self._event_queue.get(),
                    timeout=deadline - time.monotonic(),
                )
            except asyncio.TimeoutError:
                return None
            if event.get("type") == event_type and _match_kwargs(event, kwargs):
                return event
        return None

    async def drain_events(self, pause: float = 0.5) -> list[dict]:
        """Collect any events that arrive within `pause` seconds."""
        collected: list[dict] = []
        deadline = time.monotonic() + pause
        while time.monotonic() < deadline:
            try:
                event = await asyncio.wait_for(
                    self._event_queue.get(),
                    timeout=deadline - time.monotonic(),
                )
                collected.append(event)
            except asyncio.TimeoutError:
                break
        return collected

    def filter_events(self, event_type: str) -> list[dict]:
        return [e for e in self.events if e.get("type") == event_type]

    def last_event_of(self, event_type: str) -> dict | None:
        matched = self.filter_events(event_type)
        return matched[-1] if matched else None


def _match_kwargs(event: dict, kwargs: dict) -> bool:
    for key, expected in kwargs.items():
        actual = event.get(key)
        if isinstance(expected, str) and isinstance(actual, str):
            if expected not in actual:
                return False
        elif actual != expected:
            return False
    return True


def log(msg: str) -> None:
    ts = datetime.now().strftime("%H:%M:%S")
    print(f"[{ts}] {msg}")


def fail(msg: str) -> None:
    log(f"FAIL: {msg}")
    sys.exit(1)


def ok(msg: str) -> None:
    log(f"  ✓ {msg}")


# ── server log capture ──────────────────────────────────────────────────

class ServerLogCapture:
    """Tail the server log from a known start position."""

    def __init__(self, log_path: Path = SERVER_LOG):
        self.log_path = log_path
        self._start_pos = 0

    def mark(self) -> None:
        if self.log_path.exists():
            self._start_pos = self.log_path.stat().st_size
        else:
            self._start_pos = 0

    def tail(self) -> list[str]:
        if not self.log_path.exists():
            return []
        with open(self.log_path) as f:
            f.seek(self._start_pos)
            lines = f.readlines()
        return [line.rstrip("\n") for line in lines]

    def grep(self, pattern: str) -> list[str]:
        return [line for line in self.tail() if pattern in line]

    def has_line_containing(self, pattern: str) -> bool:
        return any(pattern in line for line in self.tail())


# ── backend lifecycle ───────────────────────────────────────────────────

async def restart_backend() -> None:
    """Kill existing server, rebuild (if needed), and start fresh."""
    log("stopping old backend...")
    subprocess.run(["pkill", "-f", "./server"], capture_output=True)
    await asyncio.sleep(1)

    if not SERVER_BIN.exists():
        log("building backend...")
        result = subprocess.run(
            ["go", "build", "-o", "server", "./cmd/server"],
            cwd=PROJECT_ROOT,
            capture_output=True,
            text=True,
        )
        if result.returncode != 0:
            fail(f"build failed:\n{result.stderr}")

    log("starting backend...")
    env = os.environ.copy()
    env.update({
        "AUTH_TOKEN": AUTH_TOKEN,
        "APNS_AUTH_KEY_PATH": os.environ.get("APNS_AUTH_KEY_PATH", ""),
        "APNS_KEY_ID": os.environ.get("APNS_KEY_ID", ""),
        "APNS_TEAM_ID": os.environ.get("APNS_TEAM_ID", ""),
        "APNS_TOPIC": os.environ.get("APNS_TOPIC", ""),
        "APNS_PRODUCTION": os.environ.get("APNS_PRODUCTION", "false"),
    })
    log_path = str(SERVER_LOG)
    with open(log_path, "w") as f:
        proc = subprocess.Popen(
            [str(SERVER_BIN)],
            env=env,
            stdout=f,
            stderr=subprocess.STDOUT,
            cwd=PROJECT_ROOT,
        )

    # wait for ready
    for _ in range(30):
        if SERVER_LOG.exists():
            content = SERVER_LOG.read_text()
            if "Ready: addr=:8001" in content or "Ready: addr=" in content:
                log("backend ready")
                return
        await asyncio.sleep(0.5)

    fail("backend did not become ready within 15s")


def stop_backend() -> None:
    subprocess.run(["pkill", "-f", "./server"], capture_output=True)
