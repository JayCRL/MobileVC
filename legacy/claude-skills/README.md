# Claude Code Skills for MobileVC

Drop-in [Claude Code](https://docs.claude.com/en/docs/claude-code/) skills that wrap the MobileVC user-facing flows. The skills only depend on the published npm package, not on the source repo, so end-users do not need Go / Flutter toolchains.

## Available skills

| Skill | Purpose |
|---|---|
| `mobilevc-installer` | One-shot: install `@justprove/mobilevc` from npm, run `mobilevc start`, guide the user through installing the iOS / Android client from the MobileVC homepage. |

## Install

One-liner from a fresh machine that has Claude Code:

```bash
curl -fsSL https://raw.githubusercontent.com/JayCRL/MobileVC/main/claude-skills/install.sh | bash
```

Or manual:

```bash
mkdir -p ~/.claude/skills/mobilevc-installer
curl -fsSL https://raw.githubusercontent.com/JayCRL/MobileVC/main/claude-skills/mobilevc-installer/SKILL.md \
  -o ~/.claude/skills/mobilevc-installer/SKILL.md
```

## Use

In any Claude Code session, ask one of:

- 「装一个 mobilevc」 / 「我想在手机上用 Claude Code」
- "install mobilevc" / "set up mobilevc on this machine"
- "把 Claude Code 装到我手机上"

Claude will pick up the skill, install the npm package, run `mobilevc start`, and walk you through installing the official phone client from <https://www.mobilevc.top>.

Install notes:

- iOS installs through the TestFlight link on the MobileVC homepage.
- Android installs through the APK download link on the MobileVC homepage.
- The homepage may require a domestic network environment. If it does not open, switch networks and try again.
- During `mobilevc start` setup, token input is hidden. Type the token normally and press Enter.

## Uninstall

```bash
rm -rf ~/.claude/skills/mobilevc-installer
```

The npm package itself can be removed with `npm uninstall -g @justprove/mobilevc`.
