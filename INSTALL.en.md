# Install / Deploy

`mobilevc-dsh` is a **DSH Web host plugin + static MobileVC UI**. It needs a running DSH Web profile; it is not a standalone app and has no npm build step.

- Tested with DSH `0.2.0-rc.2`, Node.js 22+
- Plugin entry: `plugin/index.mjs`
- UI: `preview/` (vanilla HTML/CSS/JS)
- Optional read-only viewer: `tools/server.py`

> The repository root is this plugin. The original Flutter/Go MobileVC lives under `legacy/` and is not part of the installed package.

## 1. Install as a DSH bundle

### From GitHub (main)

```bash
dsh plugin --profile web add "github:JayCRL/MobileVC"
systemctl restart dsh-web
```

The bundle patch inserts:

```yaml
- insert:
    - id: mobilevc
      name: mobilevc-dsh
```

### From a local clone

```bash
git clone https://github.com/JayCRL/MobileVC.git mobilevc
dsh plugin --profile web add ./mobilevc
systemctl restart dsh-web
```

### Verify

```bash
curl http://127.0.0.1:8898/mobilevc/api/health
# {"ok":true,"plugin":"mobilevc","interactive":true}

curl 'http://127.0.0.1:8898/mobilevc/api/sessions?limit=1'
```

Open `http://127.0.0.1:8898/mobilevc/`.

If `dsh-web` is not a systemd unit, restart DSH however you normally start it.

## 2. Manual install

Copy this package somewhere, then add to `~/.dsh/profiles/web/cordis.patch.yml`:

```yaml
- insert:
    - id: mobilevc
      name: '/absolute/path/to/mobilevc-dsh/plugin/index.mjs'
```

Restart DSH Web.

The plugin resolves `preview/` relative to its own file, so the package can live anywhere.

## 3. Remote access / phone

`/mobilevc/` has no login screen and does not go through DSH's `/api` auth fence. Put it behind an authenticating HTTPS reverse proxy.

```nginx
location = /mobilevc { return 301 /mobilevc/; }

location ^~ /mobilevc/ {
    client_max_body_size 25m;
    auth_request /__your_auth;
    error_page 401 = @your_login;
    proxy_pass http://127.0.0.1:8898;
    proxy_http_version 1.1;
    proxy_set_header Host $host;
    proxy_set_header X-Forwarded-Proto $scheme;
    proxy_buffering off;          # SSE follow stream
    proxy_read_timeout 300s;
}
```

If DSH Web uses `--trusted-host`, add your domain.

`client_max_body_size` is needed because image/file attachments are uploaded as base64 JSON. The UI limits 12 MB per file / 20 MB total.

### PWA

- Serve over HTTPS
- Safari → Share → Add to Home Screen
- Launch from the home-screen icon for standalone mode
- iOS ignores `user-scalable=no`; the page also blocks `gesturestart` and sets `touch-action: pan-x pan-y`
- A third-party WKWebView shell still needs `scrollView.pinchGestureRecognizer.isEnabled = false` natively

## 4. Optional read-only viewer

No plugin, no write path; reads `~/.dsh/sessions` directly.

```bash
python3 tools/server.py
# 127.0.0.1:8931
```

Requires the `zstd` command. Reverse proxy `/mobilevc-preview/` behind the same auth.

## 5. Requirements

- Linux (paths are portable; macOS should work for DSH)
- Node.js 22+
- DSH with a Web profile
- Optional: nginx/Caddy + HTTPS
- Optional: Python 3.10+ and `zstd` for `tools/server.py`

No `npm install` is needed for the plugin or UI.

## 6. Security

`/mobilevc/` can read every DSH session/log/workspace file, send prompts, stop runs, approve tool calls, write `AGENTS.md`, upload files and toggle plugins.

All authenticated users share one DSH identity. No multi-tenant isolation.

Do not expose port 8898 directly. Use authentication + HTTPS. Image messages require a model with image input support.

## 7. Upgrade / uninstall

Upgrade:

```bash
# replace package files, then
systemctl restart dsh-web
```

Uninstall: remove the `mobilevc` insert (or disable the bundle), restart DSH Web, remove the package and reverse-proxy location.

## 8. Troubleshooting

**404 `/mobilevc/`** — bundle not loaded or plugin boot failed; check `curl 127.0.0.1:8898/mobilevc/api/health` and DSH logs.

**Health OK but sessions 500** — `ctx.sessionController` unavailable or `~/.dsh/sessions` unreadable.

**413 on attachments** — raise `client_max_body_size` in the `/mobilevc/` location.

**No approval card** — the phone must have an active follow stream for that session; otherwise the approval goes to the desktop/Remote answerer.

**Stale approval click** — returns `{ok:false, stale:true}`; the UI removes the card and no duplicate decision is written.

**No live reasoning** — requires DSH's live assistant stream; durable reasoning still appears at step end.

**PWA still shows URL bar** — launch from the home-screen icon, not Safari. Remove/re-add the old icon after manifest changes.
