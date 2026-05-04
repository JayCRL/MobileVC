---
name: mobilevc-installer
description: |
  Install and start MobileVC — a Claude Code mobile workspace launcher that lets the user run Claude Code on a phone (iOS / Android) with their dev machine as the backend.

  Use this skill when the user asks to:
  - "install mobilevc" / "set up mobilevc" / "装一个 mobilevc"
  - "在手机上用 Claude Code" / "want to use Claude Code on my phone"
  - "把 Claude Code 装到手机" / "mobile claude code"
  - "扫码连手机" 这类与 mobilevc 关联的请求

  This skill installs the published npm package `@justprove/mobilevc`, runs `mobilevc start` (which prints a LAN QR code), and points the user to https://mobilevc.top/install to grab the iOS / Android client.

  Skip if the user asks to MODIFY MobileVC source code or rebuild it from scratch — this skill is a launcher, not a dev environment bootstrapper.
---

# mobilevc-installer

Install and run MobileVC for the user. The user's dev machine becomes the Claude Code backend; the phone runs the official client app and connects to it over LAN or via the public install site.

GitHub: https://github.com/JayCRL/MobileVC
npm:    https://www.npmjs.com/package/@justprove/mobilevc

## What you do

Run these steps in order. Stop and report to the user as soon as anything fails.

### Step 1 — Preflight

Before installing, check the environment. Run these commands and confirm:

```bash
node --version    # require >= 18.0.0
npm --version
```

If `node` is missing or older than 18, STOP and tell the user how to install Node.js (`brew install node`, `nvm install 20`, or https://nodejs.org). Do NOT proceed.

### Step 2 — Install the npm package

Try the global install first:

```bash
npm install -g @justprove/mobilevc
```

If it fails with `EACCES` or permission errors, do NOT use `sudo`. Instead suggest one of:

- `npm config set prefix ~/.npm-global && export PATH=~/.npm-global/bin:$PATH` then re-run install
- `nvm use <version>` if the user has nvm
- `npm install --prefix ~/.local @justprove/mobilevc` and add `~/.local/bin` to PATH

Verify:

```bash
mobilevc --help
```

### Step 3 — First-run setup (interactive)

Tell the user that `mobilevc start` will ask three questions on first run:

1. **Language** — `zh` or `en`
2. **Port** — default `8001`. Pick something else if 8001 is busy.
3. **Auth token** — any string the user wants. The phone client will need this to connect.

If the user has not picked a token yet, suggest something memorable but non-trivial (e.g. `mvc-` plus 8 random chars).

### Step 4 — Start the backend

```bash
mobilevc start
```

This will:
- Run `mobilevc setup` if no config exists yet
- Spawn the prebuilt server binary in the background (logs in `~/.mobilevc/logs/`)
- Print a LAN QR code in the terminal that encodes `<host>:<port>` + auth token

If start fails, run `mobilevc logs` and surface the relevant error to the user. Common issues:
- Port already in use → `mobilevc stop` then change port via `mobilevc setup`
- Binary missing → reinstall via `npm install -g @justprove/mobilevc --force`

### Step 5 — Direct the user to install the phone client

Tell the user (verbatim, in the user's preferred language):

```
后端已启动。请在手机上：
1. 用 Safari (iOS) / 浏览器 (Android) 打开 https://mobilevc.top/install
2. iOS 选择「一键安装」，安装后到「设置 → VPN 与设备管理」信任证书
3. Android 选择「下载 APK」，按系统提示允许安装
4. 打开 MobileVC App，扫描终端里刚刚打印的二维码
```

(English variant if the user is using English.)

### Step 6 — Done

Confirm the running state:

```bash
mobilevc status
```

Tell the user the helper commands they will need later:
- `mobilevc status` — check if backend is alive
- `mobilevc logs -f` — follow logs
- `mobilevc stop` — stop backend
- `mobilevc restart` — restart with same config
- `mobilevc setup` — reconfigure (port / token / language)

## Important rules

- **Do NOT** clone the MobileVC source repo unless the user explicitly asks to develop on it. This skill is for end-users who only want to RUN MobileVC.
- **Do NOT** ask the user for APNS keys or any backend secrets. The npm-bundled binary is self-contained for LAN usage; APNS push is only required if the user is self-hosting a public deployment, which is out of scope here.
- **Do** check Node.js version first. Almost all install failures upstream of `mobilevc start` are Node version or PATH issues.
- **Do** keep the QR code output visible — that's the primary user-facing artifact.
