#!/usr/bin/env node
/**
 * 文档路径检查。
 *
 * 文档天然会漂移：CLAUDE.md 里写过的 internal/runner、internal/ws、internal/runtime
 * 早就被删了，但没人发现。这个脚本把文档里所有「像仓库路径」的行内代码抽出来，
 * 逐个验证存在性，不存在就以非零退出码失败，让 CI 拦住漂移。
 *
 * 只检查行内代码（`...`），不解析链接，避免误伤 URL。
 * 路径同时按「仓库根」和「文档所在目录」解析，兼容子目录里的相对路径。
 */
import { readFileSync, existsSync, statSync } from 'node:fs';
import { readdirSync } from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const repoRoot = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..');

/** 需要检查的文档 */
function collectDocs() {
  const explicit = [
    'README.md',
    'README.en.md',
    'INSTALL.md',
    'INSTALL.en.md',
    'CLAUDE.md',
    'packages/contract/README.md',
    'packages/contract/events.md',
    'apps/remote/CLAUDE.md',
    'apps/remote/README.md',
    'apps/dsh/edge/README.md',
  ];

  const walk = (dir, out) => {
    let entries;
    try {
      entries = readdirSync(path.join(repoRoot, dir), { withFileTypes: true });
    } catch {
      return out;
    }
    for (const e of entries) {
      if (e.name === 'node_modules' || e.name === 'vendor' || e.name.startsWith('.')) continue;
      const rel = path.posix.join(dir, e.name);
      if (e.isDirectory()) walk(rel, out);
      else if (e.name.endsWith('.md')) out.push(rel);
    }
    return out;
  };

  return [...new Set([...explicit, ...walk('docs', [])])].filter((p) =>
    existsSync(path.join(repoRoot, p)),
  );
}

/** 看起来像仓库路径的行内代码 */
function looksLikePath(text) {
  if (!text.includes('/')) return false;
  if (text.includes(' ') || text.includes('<') || text.includes('>')) return false;
  if (/[*?{}\[\]]/.test(text)) return false;
  if (/^(https?|wss?):/.test(text)) return false;
  if (text.startsWith('~/') || text.startsWith('$')) return false;
  // 以 / 开头的是 HTTP 路由（如 `/mobilevc/`），不是仓库路径
  if (text.startsWith('/')) return false;
  // 相对路径的语义依赖上下文，跳过以免误报
  if (text.startsWith('./') || text.startsWith('../')) return false;
  // 单个词 + 斜杠（如 `internal/`）太泛，跳过
  const segments = text.replace(/\/+$/, '').split('/').filter(Boolean);
  if (segments.length < 2) return false;
  // 点目录（.claude/、.vite/ …）除 .github 外都不是需要校验的资产
  if (segments[0].startsWith('.') && segments[0] !== '.github') return false;
  if (/\.(md|go|mjs|ts|json|yml|yaml|png|gif|mp4)$/.test(text)) return true;
  return (
    text.endsWith('/') ||
    /^(apps|packages|docs|scripts|internal|cmd|mobile_vc|tools|tests|deploy|\.github)\//.test(text)
  );
}

/** 路径是否真实存在（允许以 / 结尾表示目录） */
function resolves(docRel, candidate) {
  const clean = candidate.replace(/\/+$/, '');
  if (!clean) return false;
  const bases = [repoRoot, path.dirname(path.join(repoRoot, docRel))];
  return bases.some((base) => {
    const target = path.join(base, clean);
    try {
      return existsSync(target) && (statSync(target).isDirectory() || statSync(target).isFile());
    } catch {
      return false;
    }
  });
}

const docs = collectDocs();
const problems = [];

for (const doc of docs) {
  const lines = readFileSync(path.join(repoRoot, doc), 'utf8').split(/\r?\n/);
  lines.forEach((line, i) => {
    for (const m of line.matchAll(/`([^`\n]{1,200})`/g)) {
      const candidate = m[1].trim();
      if (!looksLikePath(candidate)) continue;
      if (!resolves(doc, candidate)) {
        problems.push({ doc, line: i + 1, candidate });
      }
    }
  });
}

if (problems.length === 0) {
  console.log(`doc paths: OK（检查了 ${docs.length} 份文档）`);
  process.exit(0);
}

console.error(`doc paths: ${problems.length} 处引用了不存在的路径\n`);
for (const p of problems) {
  console.error(`  ${p.doc}:${p.line}  \`${p.candidate}\``);
}
console.error('\n修法：删掉该路径、改成当前路径，或在真的有意保留时加进 collectDocs 白名单。');
process.exit(1);
