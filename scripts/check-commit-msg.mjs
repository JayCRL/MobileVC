#!/usr/bin/env node
/**
 * 提交信息规范检查。
 *
 * 格式：type(scope): 说明        —— scope 可省略，说明至少 2 个字符
 * type：feat fix chore docs style refactor perf test ci build revert
 *
 * 用法：
 *   node scripts/check-commit-msg.mjs "<消息>"     # 直接给消息
 *   node scripts/check-commit-msg.mjs /path/to/msg # 给 commit-msg 钩子传的文件
 *
 * 合并提交（Merge ...）与 fixup/squash 前缀跳过检查。
 */
import { existsSync, readFileSync } from 'node:fs';

const TYPES = [
  'feat', 'fix', 'chore', 'docs', 'style', 'refactor',
  'perf', 'test', 'ci', 'build', 'revert',
];
const PATTERN = new RegExp(`^(${TYPES.join('|')})(\\([a-z0-9-]+\\))?!?: .{2,}`);

const input = process.argv[2];
if (!input) {
  console.error('usage: node scripts/check-commit-msg.mjs <message|file>');
  process.exit(2);
}

let message = input;
if (existsSync(input)) {
  message = readFileSync(input, 'utf8');
}

// 取第一条非注释、非空行作为标题
const subject = message
  .split(/\r?\n/)
  .map((line) => line.trim())
  .find((line) => line && !line.startsWith('#')) ?? '';

if (/^Merge /.test(subject) || /^(fixup|squash|amend)!/.test(subject)) {
  console.log(`commit msg: skipped (${subject.slice(0, 40)})`);
  process.exit(0);
}

if (PATTERN.test(subject)) {
  console.log(`commit msg: OK (${subject})`);
  process.exit(0);
}

console.error(`commit msg: 不符合规范

  实际：${subject || '(空)'}

  格式：type(scope): 说明
  type：${TYPES.join(' ')}
  例：feat(mindmap): 悬浮菜单支持同级节点
      fix(relay): 重连后恢复配对状态

  这是本地钩子 + CI 都会检查的规则，说明见根目录 CLAUDE.md 的「提交」一节。
`);
process.exit(1);
