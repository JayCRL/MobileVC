# Install / Deploy

`mobilevc-dsh` is a **DSH Web host plugin + static MobileVC UI**. It needs a running DSH Web profile; it is not a standalone app and has no npm build step.

- Tested with: DSH `0.2.0-rc.2`, Node.js 22+
- Plugin entry: `plugin/index.mjs`
- UI: `preview/` (vanilla HTML/CSS/JS)
- No runtime npm dependencies

---

## 1. Install as a DSH bundle

### From GitHub (this branch)

```bash
dsh plugin --profile web add "github:JayCRL/MobileVC#mobilevc-dsh"
systemctl restart dsh-web
```

`dsh plugin` installs the package and selects its bundle patch; the patch inserts:

```yaml
- insert:
    - id: mobilevc
      name: mobilevc-dsh
```

### From a local clone

```bash
git clone --branch mobilevc-dsh https://github.com/JayCRL/MobileVC.git mobilevc-dsh
dsh plugin --profile web add ./mobilevc-dsh
systemctl restart dsh-web
```

### Verify

```bash
curl http://127.0.0.1:8898/mobilevc/api/health
# {"ok":true,"plugin":"mobilevc","interactive":true}

curl 'http://127.0.0.1:8898/mobilevc/api/sessions?limit=1'
```

Open `http://127.0.0.1:8898/mobilevc/`.

If `dsh-web` is not a systemd unit on your machine, restart however you normally boot `dsh web`.

---

## 2. Manual install (without the bundle manager)

Copy this package somewhere, then add to `~/.dsh/profiles/web/cordis.patch.yml`:

```yaml
- insert:
    - id: mobilevc
      name: '/absolute/path/to/mobilevc-dsh/plugin/index.mjs'
```

Restart `dsh web`.

The plugin resolves `preview/` relative to its own file, so the package can live anywhere. The patch `name` must be the absolute path if you are not installing it as a package.

---

## 3. Remote access / phone

The plugin has **no login screen**. `/mobilevc/` is registered by the Host plugin and does not go through DSH's `/api` auth fence.

Put it behind an HTTPS reverse proxy with authentication. Example (replace the auth mechanism with yours):

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

Why `client_max_body_size`: image/file attachments are uploaded as base64 JSON. The UI limits 12 MB per file / 20 MB total; 25m gives headroom.

### PWA

- Serve over HTTPS
- Open in Safari → Share → **Add to Home Screen**
- Launch from the home-screen icon for standalone mode (no URL bar)
- iOS Safari ignores `user-scalable=no`; the page also blocks `gesturestart` and sets `touch-action: pan-x pan-y`
- A third-party WKWebView shell still needs `scrollView.pinchGestureRecognizer.isEnabled = false` natively

---

## 4. Optional read-only viewer

No plugin, no write path; reads `~/.dsh/sessions` directly.

```bash
python3 tools/server.py
# 127.0.0.1:8931
```

Requires the `zstd` command.

Reverse proxy `/mobilevc-preview/` to `127.0.0.1:8931`, behind the same auth. The UI detects the path and stays read-only.

---

## 5. Requirements

- Linux (macOS should work for DSH; paths are portable)
- Node.js 22+
- DSH with a Web profile
- Optional: nginx/Caddy + HTTPS
- Optional: Python 3.10+ and `zstd` for `tools/server.py`

No `npm install` is needed for the plugin or the UI.

---

## 6. Security

`/mobilevc/` can:

- list/read every DSH session, logs and workspace files
- send prompts and stop runs
- approve/reject tool calls
- write `AGENTS.md`
- upload images/files
- enable/disable DSH plugins

All authenticated users share the same DSH identity. There is no multi-tenant isolation in this version.

Do not expose port 8898 directly. Use authentication + HTTPS. Only give access to people you would trust with a shell on the DSH host.

Image messages require a model whose catalog declares image input; otherwise DSH rejects the prompt.

---

## 7. Upgrade

```bash
# replace the package files, then
systemctl restart dsh-web
```

The profile patch does not need to change.

## 8. Uninstall

1. Remove the `mobilevc` insert from the profile patch, or use the DSH plugin manager to disable the bundle
2. `systemctl restart dsh-web`
3. Remove the package directory and reverse-proxy location

---

## 9. Troubleshooting

**404 `/mobilevc/`**

- patch/bundle not loaded, or the plugin threw during boot
- check `curl 127.0.0.1:8898/mobilevc/api/health`
- check `journalctl -u dsh-web` or your DSH logs

**Health OK but sessions 500**

- `ctx.sessionController` unavailable or the session store is unreadable
- check DSH profile composition and `~/.dsh/sessions`

**413 on attachments**

- raise `client_max_body_size` in the `/mobilevc/` location

**Approval card does not appear**

- the phone must have an active follow stream for that session (open it and wait for the SSE connection)
- without a mobile follow, the approval is delegated to the desktop/Remote answerer

**A stale approval click**

- returns `{ok:false, stale:true}` and the UI removes the card; no duplicate decision is written

**Trace shows history but no live reasoning**

- `assistantStream: true` follow requires DSH's live assistant stream; older DSH versions may not provide it
- durable reasoning still appears at step end

**PWA still shows URL bar**

- it must be launched from the home-screen icon, not Safari
- remove/re-add the old icon after manifest/meta changes
