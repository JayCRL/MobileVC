#!/usr/bin/env python3
"""MobileVC preview server: static UI + real DSH data.

Serves the MobileVC-style UI and read-only APIs over the local DSH session store.
No mock session data: sessions, messages, skills, memory and logs come from the
actual DSH home on this machine.
"""
from __future__ import annotations

import html
import json
import mimetypes
import os

mimetypes.add_type("application/manifest+json", ".webmanifest")
import re
import subprocess
import threading
import time
from concurrent.futures import ThreadPoolExecutor
from http.server import SimpleHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path
from urllib.parse import parse_qs, urlparse

ROOT = Path(__file__).resolve().parent.parent / "preview"
DSH_HOME = Path.home() / ".dsh"
SESSIONS_DIR = DSH_HOME / "sessions"
LOG_DIR = DSH_HOME / "logs"
ALLOWED_MEMORY = [
    DSH_HOME / "AGENTS.md",
    Path.home() / "AGENTS.md",
    Path.home() / "CLAUDE.md",
]

_cache_lock = threading.Lock()
_session_cache: dict[str, tuple[float, int, dict]] = {}


# ---------------------------------------------------------------- formatting
def md_to_html(text: str, limit: int = 14000) -> str:
    text = (text or "").strip()
    if len(text) > limit:
        text = text[:limit] + "\n\n…（已截断）"
    esc = html.escape(text)
    # fenced code blocks
    esc = re.sub(
        r"```[a-zA-Z0-9_+-]*\n(.*?)```",
        lambda m: "<pre><code>" + m.group(1).rstrip() + "</code></pre>",
        esc,
        flags=re.S,
    )
    # inline code, bold
    esc = re.sub(r"`([^`\n]+)`", r"<code>\1</code>", esc)
    esc = re.sub(r"\*\*([^*\n]+)\*\*", r"<strong>\1</strong>", esc)
    # headings
    esc = re.sub(r"^#{1,6}\s+(.+)$", r'<div class="md-h">\1</div>', esc, flags=re.M)
    # simple list markers
    esc = re.sub(r"^(\s*)[-*]\s+", r"\1• ", esc, flags=re.M)
    # remaining triple backticks without a language / closing fence
    esc = esc.replace("```", "")
    # paragraphs
    blocks = re.split(r"\n{2,}", esc)
    out = []
    for block in blocks:
        block = block.strip()
        if not block:
            continue
        if block.startswith(("<pre>", '<div class="md-h">', "<ul>", "<ol>", "<blockquote>")):
            out.append(block)
        else:
            out.append("<p>" + block.replace("\n", "<br>") + "</p>")
    return "".join(out) or "<p></p>"


def text_of_content(content) -> str:
    if not isinstance(content, list):
        return ""
    parts = []
    for part in content:
        if not isinstance(part, dict):
            continue
        if part.get("type") == "text" and part.get("text"):
            parts.append(str(part["text"]))
    return "\n\n".join(parts).strip()


def content_of_message(msg) -> str:
    if not isinstance(msg, dict):
        return ""
    return text_of_content(msg.get("content"))


def tool_arg_summary(name: str, raw) -> str:
    data = raw
    if isinstance(raw, str):
        try:
            data = json.loads(raw)
        except Exception:
            return raw.strip().split("\n")[0][:160]
    if not isinstance(data, dict):
        return str(data)[:160]
    for key in ("command", "file_path", "path", "pattern", "query", "url", "description"):
        if data.get(key):
            val = str(data[key]).strip().split("\n")[0]
            return val[:160]
    if data:
        k = next(iter(data))
        return f"{k}={str(data[k])[:120]}"
    return ""


def pretty_args(raw) -> str:
    if isinstance(raw, str):
        try:
            raw = json.loads(raw)
        except Exception:
            return raw[:4000]
    if isinstance(raw, dict):
        return json.dumps(raw, ensure_ascii=False, indent=2)[:4000]
    return str(raw)[:4000]


def parse_session(path: Path, full: bool = False) -> dict:
    st = path.stat()
    key = str(path)
    with _cache_lock:
        cached = _session_cache.get(key)
        if cached and cached[0] == st.st_mtime and cached[1] == st.st_size:
            if not full or cached[2].get("_full"):
                return cached[2]

    try:
        proc = subprocess.run(
            ["zstd", "-dc", str(path)],
            capture_output=True,
            timeout=40,
        )
        raw = proc.stdout.decode("utf-8", "replace")
    except Exception:
        raw = ""

    header = {}
    title = ""
    cwd = ""
    subagent = False
    blocks: list[dict] = []
    calls: dict[str, dict] = {}
    pending_approvals: dict[str, dict] = {}
    engine = ""
    model = ""
    running = False
    user_count = 0
    assistant_count = 0
    last_text = ""
    last_mtime = st.st_mtime

    for line in raw.splitlines():
        line = line.strip()
        if not line:
            continue
        try:
            ev = json.loads(line)
        except Exception:
            continue
        typ = ev.get("type")
        data = ev.get("data") or {}
        if typ == "session":
            header = ev
            cwd = ev.get("cwd") or ""
            subagent = bool(ev.get("parentSession")) or int(ev.get("delegationDepth") or 0) > 0
            continue
        if typ == "session/title":
            title = str(data.get("title") or "").strip()
            continue
        if typ == "request/header":
            h = data.get("header") or {}
            cfg = h.get("config") or {}
            engine = str(cfg.get("provider") or engine)
            model = str(cfg.get("model") or model)
            continue
        if typ == "user/message":
            txt = text_of_content(data.get("content"))
            if txt:
                user_count += 1
                last_text = txt
                blocks.append({"type": "user", "text": txt[:12000]})
            continue
        if typ == "assistant/message":
            msg = data.get("message") or {}
            txt = content_of_message(msg)
            if txt:
                assistant_count += 1
                last_text = txt
                blocks.append({"type": "assistant", "html": md_to_html(txt)})
            continue
        if typ == "tool/call":
            call_id = str(data.get("callId") or "")
            name = str(data.get("name") or "tool")
            arg = pretty_args(data.get("arguments") or "")
            block = {
                "type": "tool",
                "name": name,
                "arg": tool_arg_summary(name, data.get("arguments") or ""),
                "status": "运行中",
                "body": arg,
                "_callId": call_id,
            }
            blocks.append(block)
            calls[call_id] = block
            continue
        if typ == "tool/result":
            msg = data.get("message") or {}
            call_id = str(data.get("toolCallId") or msg.get("toolCallId") or "")
            body = content_of_message(msg)
            block = calls.get(call_id)
            if block is None:
                block = {
                    "type": "tool",
                    "name": "result",
                    "arg": call_id[:40],
                    "status": "完成",
                    "body": body[:6000],
                    "_callId": call_id,
                }
                blocks.append(block)
            else:
                block["status"] = "完成"
                block["body"] = (body or block.get("body") or "")[:6000]
            continue
        if typ == "approval/asked":
            aid = str(data.get("id") or "")
            pending_approvals[aid] = {
                "id": aid,
                "tool": str(data.get("toolName") or ""),
                "reason": str(data.get("reason") or "")[:600],
                "callId": str(data.get("callId") or ""),
            }
            continue
        if typ == "approval/decided":
            aid = str(data.get("id") or "")
            pending_approvals.pop(aid, None)
            continue
        if typ == "compaction/summary":
            summary = text_of_content(data.get("summary"))
            blocks.append(
                {
                    "type": "compaction",
                    "text": "上下文已压缩" + (" · " + summary[:120] if summary else ""),
                }
            )
            continue
        if typ == "turn/start":
            running = True
            continue
        if typ == "turn/end":
            running = False
            continue
        if typ == "session/end-seed":
            running = False

    # remove internal keys from public blocks
    public_blocks = []
    for b in blocks:
        b = dict(b)
        b.pop("_callId", None)
        public_blocks.append(b)
    if len(public_blocks) > 120:
        public_blocks = public_blocks[-120:]

    if not title:
        for b in blocks:
            if b.get("type") == "user" and b.get("text"):
                title = b["text"].strip().split("\n")[0][:40]
                break
    if not title:
        title = path.parent.name
    if not cwd:
        cwd = str(header.get("cwd") or "")

    result = {
        "id": path.parent.name,
        "title": title,
        "cwd": cwd,
        "engine": engine or "DSH",
        "model": model or header.get("agentPreset") or "default",
        "state": "waiting" if pending_approvals else ("running" if running else "idle"),
        "stateLabel": "待授权" if pending_approvals else ("运行中" if running else "空闲"),
        "last": last_text.replace("\n", " ")[:120],
        "time": _relative_time(st.st_mtime),
        "updatedAt": st.st_mtime,
        "pending": len(pending_approvals),
        "blockCount": len(public_blocks),
        "userCount": user_count,
        "assistantCount": assistant_count,
        "subagent": subagent,
        "ctx": None,
        "_full": full,
    }
    if full:
        result["blocks"] = public_blocks
        result["approvals"] = list(pending_approvals.values())
    with _cache_lock:
        _session_cache[key] = (st.st_mtime, st.st_size, result)
    return result


def _relative_time(ts: float) -> str:
    diff = max(0, time.time() - ts)
    if diff < 90:
        return "刚刚"
    if diff < 3600:
        return f"{int(diff // 60)} 分钟前"
    if diff < 86400:
        return f"{int(diff // 3600)} 小时前"
    if diff < 86400 * 7:
        return f"{int(diff // 86400)} 天前"
    return time.strftime("%m 月 %d 日", time.localtime(ts))


def list_sessions(limit: int = 12) -> list[dict]:
    files = []
    for p in SESSIONS_DIR.glob("*/*/session.v4.jsonl.zstd"):
        try:
            files.append((p.stat().st_mtime, p))
        except OSError:
            continue
    files.sort(reverse=True)
    paths = [p for _, p in files[: max(1, min(limit, 40))]]

    def one(p):
        try:
            item = dict(parse_session(p, full=False))
            item.pop("blocks", None)
            return item
        except Exception:
            return None

    with ThreadPoolExecutor(max_workers=8) as pool:
        results = list(pool.map(one, paths))
    return [r for r in results if r]


# ---------------------------------------------------------------- skills / memory
def skill_roots(cwd: str = "") -> list[tuple[Path, str]]:
    roots = [
        (DSH_HOME / "skills", "DSH"),
        (Path.home() / ".agents" / "skills", "Agents"),
        (Path.home() / ".claude" / "skills", "Claude"),
        (Path.home() / ".codex" / "skills", "Codex"),
    ]
    if cwd:
        roots = [
            (Path(cwd) / ".dsh" / "skills", "项目"),
            (Path(cwd) / ".agents" / "skills", "项目"),
        ] + roots
    return roots


def parse_frontmatter(text: str) -> tuple[dict, str]:
    meta = {}
    body = text
    if text.startswith("---"):
        end = text.find("\n---", 3)
        if end != -1:
            head = text[3:end]
            body = text[end + 4 :]
            for line in head.splitlines():
                if ":" in line:
                    k, v = line.split(":", 1)
                    meta[k.strip()] = v.strip().strip("'\"")
    return meta, body


def list_skills(cwd: str = "") -> list[dict]:
    items = []
    seen = set()
    for root, src in skill_roots(cwd):
        if not root.is_dir():
            continue
        for p in sorted(root.glob("*/SKILL.md")):
            try:
                text = p.read_text(encoding="utf-8", errors="replace")
            except OSError:
                continue
            meta, body = parse_frontmatter(text)
            name = (meta.get("name") or p.parent.name).strip()
            if name in seen:
                continue
            seen.add(name)
            items.append(
                {
                    "name": name,
                    "desc": (meta.get("description") or body.strip().split("\n")[0])[:160],
                    "src": src,
                    "path": str(p),
                }
            )
    return items


def memory_items(cwd: str = "") -> list[dict]:
    paths = list(ALLOWED_MEMORY)
    if cwd:
        paths = [Path(cwd) / "AGENTS.md", Path(cwd) / "CLAUDE.md"] + paths
    out = []
    seen = set()
    for p in paths:
        if str(p) in seen or not p.is_file():
            continue
        seen.add(str(p))
        try:
            text = p.read_text(encoding="utf-8", errors="replace")
        except OSError:
            continue
        if not text.strip():
            continue
        stat = p.stat()
        out.append(
            {
                "id": str(p),
                "title": p.name + (" · 全局" if p.parent == DSH_HOME else ""),
                "body": text.strip()[:12000],
                "src": str(p),
                "updated": _relative_time(stat.st_mtime),
                "path": str(p),
            }
        )
    return out


def read_logs(limit: int = 200) -> dict:
    files = []
    if LOG_DIR.is_dir():
        for p in LOG_DIR.glob("*"):
            if p.is_file():
                try:
                    files.append((p.stat().st_mtime, p))
                except OSError:
                    pass
    files.sort(reverse=True)
    if not files:
        return {"file": "", "lines": []}
    p = files[0][1]
    try:
        text = p.read_text(encoding="utf-8", errors="replace")
    except OSError:
        return {"file": str(p), "lines": []}
    lines = text.splitlines()[-limit:]
    out = []
    for line in lines:
        m = re.match(r"^\s*(\d{2}:\d{2}:\d{2}(?:\.\d+)?)\s+(?:\[?(\w+)\]?\s+)?(.*)$", line)
        if m:
            out.append({"t": m.group(1), "level": (m.group(2) or "info").lower(), "m": m.group(3)[:500]})
        else:
            out.append({"t": "", "level": "info", "m": line[:500]})
    return {"file": str(p), "lines": out}


# ---------------------------------------------------------------- files
def session_cwd(session_id: str) -> str:
    for p in SESSIONS_DIR.glob(f"*/{session_id}/session.v4.jsonl.zstd"):
        try:
            raw = subprocess.run(["zstd", "-dc", str(p)], capture_output=True, timeout=20).stdout.decode(
                "utf-8", "replace"
            )
            first = raw.splitlines()[0] if raw else ""
            return str(json.loads(first).get("cwd") or "")
        except Exception:
            return ""
    return ""


def list_files(session_id: str) -> list[dict]:
    cwd = session_cwd(session_id)
    if not cwd or not Path(cwd).is_dir():
        return []
    out = []
    try:
        entries = sorted(Path(cwd).iterdir(), key=lambda p: (not p.is_dir(), p.name.lower()))
    except OSError:
        return []
    for p in entries[:120]:
        if p.name.startswith(".") and p.name not in (".dsh", ".agents"):
            continue
        try:
            st = p.stat()
            out.append(
                {
                    "name": p.name,
                    "type": "dir" if p.is_dir() else ("img" if p.suffix.lower() in (".png", ".jpg", ".jpeg", ".webp", ".gif") else "file"),
                    "meta": ("目录" if p.is_dir() else _human_size(st.st_size)) + " · " + _relative_time(st.st_mtime),
                }
            )
        except OSError:
            continue
    return out


def _human_size(n: int) -> str:
    for unit in ("B", "KB", "MB", "GB"):
        if n < 1024:
            return f"{n:.0f} {unit}" if unit == "B" else f"{n:.1f} {unit}"
        n /= 1024
    return f"{n:.1f} TB"


# ---------------------------------------------------------------- HTTP
class Handler(SimpleHTTPRequestHandler):
    def __init__(self, *args, **kwargs):
        super().__init__(*args, directory=str(ROOT), **kwargs)

    def log_message(self, fmt, *args):
        pass

    def _json(self, payload, status=200):
        body = json.dumps(payload, ensure_ascii=False).encode("utf-8")
        self.send_response(status)
        self.send_header("Content-Type", "application/json; charset=utf-8")
        self.send_header("Content-Length", str(len(body)))
        self.send_header("Cache-Control", "no-store")
        self.end_headers()
        self.wfile.write(body)

    def _read_json(self):
        n = int(self.headers.get("Content-Length") or 0)
        if n <= 0:
            return {}
        try:
            return json.loads(self.rfile.read(n).decode("utf-8"))
        except Exception:
            return {}

    def do_GET(self):
        u = urlparse(self.path)
        path = u.path
        q = parse_qs(u.query)
        try:
            if path == "/api/sessions":
                try:
                    limit = int((q.get("limit") or ["12"])[0])
                except ValueError:
                    limit = 12
                self._json({"sessions": list_sessions(limit)})
                return
            if path.startswith("/api/sessions/"):
                sid = path.split("/", 3)[3]
                for p in SESSIONS_DIR.glob(f"*/{sid}/session.v4.jsonl.zstd"):
                    self._json(parse_session(p, full=True))
                    return
                self._json({"error": "session not found"}, 404)
                return
            if path == "/api/skills":
                self._json({"skills": list_skills((q.get("cwd") or [""])[0])})
                return
            if path == "/api/memory":
                self._json({"items": memory_items((q.get("cwd") or [""])[0])})
                return
            if path == "/api/logs":
                self._json(read_logs())
                return
            if path == "/api/files":
                self._json({"cwd": session_cwd((q.get("session") or [""])[0]), "items": list_files((q.get("session") or [""])[0])})
                return
            if path == "/api/file":
                sid = (q.get("session") or [""])[0]
                rel = (q.get("path") or [""])[0]
                cwd = session_cwd(sid)
                target = (Path(cwd) / rel).resolve()
                if not cwd or not str(target).startswith(str(Path(cwd).resolve())):
                    self._json({"error": "denied"}, 403)
                    return
                if not target.is_file():
                    self._json({"error": "not found"}, 404)
                    return
                if target.suffix.lower() in (".png", ".jpg", ".jpeg", ".webp", ".gif"):
                    data = target.read_bytes()
                    self.send_response(200)
                    self.send_header("Content-Type", mimetypes.guess_type(str(target))[0] or "application/octet-stream")
                    self.send_header("Content-Length", str(len(data)))
                    self.send_header("Cache-Control", "no-store")
                    self.end_headers()
                    self.wfile.write(data)
                    return
                text = target.read_text(encoding="utf-8", errors="replace")[:200000]
                self._json({"path": rel, "text": text})
                return
            if path == "/api/health":
                self._json({"ok": True})
                return
        except Exception as exc:
            self._json({"error": str(exc)}, 500)
            return
        return super().do_GET()

    def do_POST(self):
        u = urlparse(self.path)
        if u.path == "/api/memory":
            body = self._read_json()
            p = Path(str(body.get("path") or "")).resolve()
            allowed = [x.resolve() for x in ALLOWED_MEMORY]
            if p not in allowed:
                self._json({"error": "path not allowed"}, 403)
                return
            try:
                p.parent.mkdir(parents=True, exist_ok=True)
                p.write_text(str(body.get("content") or ""), encoding="utf-8")
                self._json({"ok": True})
            except Exception as exc:
                self._json({"error": str(exc)}, 500)
            return
        self._json({"error": "not found"}, 404)


def main():
    port = int(os.environ.get("PORT") or 8931)
    server = ThreadingHTTPServer(("127.0.0.1", port), Handler)
    print(f"mobilevc preview on 127.0.0.1:{port}", flush=True)
    server.serve_forever()


if __name__ == "__main__":
    main()
