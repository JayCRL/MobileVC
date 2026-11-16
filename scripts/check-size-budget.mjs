#!/usr/bin/env node
/**
 * 发布体积预算。
 *
 * `npm pack` 的内容就是用户实际下载的东西。当前约 25 MB，主要是
 * startup-boot-portrait-lite.mp4 与图标；再加一段启动视频就会悄悄翻倍。
 *
 * 预算：默认 30 MB 失败，26 MB 警告。可用 SIZE_BUDGET_MB 覆盖上限。
 */
import { execFileSync } from 'node:child_process';

const LIMIT_MB = Number(process.env.SIZE_BUDGET_MB ?? 30);
const WARN_MB = LIMIT_MB * 0.85;

let raw;
try {
  raw = execFileSync('npm', ['pack', '--dry-run', '--json', '--ignore-scripts'], {
    encoding: 'utf8',
    shell: process.platform === 'win32',
    stdio: ['ignore', 'pipe', 'pipe'],
  });
} catch (error) {
  console.error('size budget: npm pack 失败');
  console.error(error.stderr?.toString?.() ?? error.message);
  process.exit(2);
}

// npm 有时会在 JSON 之前打印 notice，截取到第一个 [
const start = raw.indexOf('[');
if (start < 0) {
  console.error('size budget: 无法解析 npm pack 输出');
  process.exit(2);
}
const [info] = JSON.parse(raw.slice(start));
const sizeMB = info.size / (1024 * 1024);
const detail = `npm 包：${info.entryCount} 个文件 / ${sizeMB.toFixed(1)} MB（预算 ${LIMIT_MB} MB）`;

if (sizeMB > LIMIT_MB) {
  console.error(`size budget: 超出预算\n  ${detail}`);
  console.error('\n  修法：压缩/外链大文件（启动视频、图标），或确实需要时调高 SIZE_BUDGET_MB 并说明原因。');
  process.exit(1);
}

if (sizeMB > WARN_MB) {
  console.warn(`size budget: 接近上限\n  ${detail}`);
  process.exit(0);
}

console.log(`size budget: OK\n  ${detail}`);
