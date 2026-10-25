#!/usr/bin/env bash
# MobileVC Claude Code skill installer.
#
# One-liner:
#   curl -fsSL https://raw.githubusercontent.com/JayCRL/MobileVC/main/claude-skills/install.sh | bash
#
# What this does:
#   - Downloads the mobilevc-installer skill from the MobileVC GitHub repo
#   - Drops it into ~/.claude/skills/mobilevc-installer/
#   - The next Claude Code session will auto-detect and offer the skill
#     when the user asks to "install mobilevc" / "set up mobilevc" / etc.

set -euo pipefail

REPO_RAW="${MOBILEVC_SKILL_REPO_RAW:-https://raw.githubusercontent.com/JayCRL/MobileVC/main}"
SKILL_DIR="${HOME}/.claude/skills/mobilevc-installer"

log() { printf '==> %s\n' "$*"; }
die() { printf 'ERROR: %s\n' "$*" >&2; exit 1; }

command -v curl >/dev/null 2>&1 || die "curl is required"

log "installing mobilevc-installer skill into ${SKILL_DIR}"
mkdir -p "${SKILL_DIR}"

curl -fsSL "${REPO_RAW}/claude-skills/mobilevc-installer/SKILL.md" \
  -o "${SKILL_DIR}/SKILL.md" \
  || die "failed to download SKILL.md from ${REPO_RAW}"

# Sanity check: the file should start with frontmatter
head -n 1 "${SKILL_DIR}/SKILL.md" | grep -q '^---$' \
  || die "downloaded SKILL.md looks malformed"

log "done"
echo
echo "Next steps:"
echo "  1. Open Claude Code (or restart your current session so the skill is picked up)."
echo "  2. Tell Claude: 「装一个 mobilevc」 or \"install mobilevc\"."
echo "  3. Claude will run the skill and walk you through install + start."
echo
echo "Skill file: ${SKILL_DIR}/SKILL.md"
