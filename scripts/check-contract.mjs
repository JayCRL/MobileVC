#!/usr/bin/env node
/**
 * 契约漂移检查：packages/contract 的 TS 与 Go 必须描述同一组字段。
 *
 * 两条产品线各自实现，字段靠手工对齐；一旦单边改了字段名，行为会在运行时才暴露。
 * 这个脚本把两边的字段名抽出来做集合比对，改漏了当场失败。
 */
import { readFileSync } from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const repoRoot = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..');
const tsPath = path.join(repoRoot, 'packages/contract/contract.ts');
const goPath = path.join(repoRoot, 'packages/contract/contract.go');

const ts = readFileSync(tsPath, 'utf8');
const go = readFileSync(goPath, 'utf8');

/** TS：对象类型里的键名（同时覆盖多行 interface 与单行联合体成员） */
function tsFields(source) {
  const fields = new Set();
  for (const block of source.matchAll(/\{([^{}]*)\}/g)) {
    for (const m of block[1].matchAll(/([a-zA-Z][a-zA-Z0-9]*)\??\s*:/g)) {
      fields.add(m[1]);
    }
  }
  return fields;
}

/** Go：结构体里的 json tag 名 */
function goFields(source) {
  const fields = new Set();
  for (const m of source.matchAll(/json:"([a-zA-Z][a-zA-Z0-9]*)/g)) {
    fields.add(m[1]);
  }
  return fields;
}

const t = tsFields(ts);
const g = goFields(go);

const onlyTs = [...t].filter((f) => !g.has(f)).sort();
const onlyGo = [...g].filter((f) => !t.has(f)).sort();

if (onlyTs.length === 0 && onlyGo.length === 0) {
  console.log(`contract: OK（TS ${t.size} 个字段 / Go ${g.size} 个字段，完全对齐）`);
  process.exit(0);
}

console.error('contract: TS 与 Go 的字段不一致\n');
if (onlyTs.length) console.error(`  只在 TS 里：${onlyTs.join(', ')}`);
if (onlyGo.length) console.error(`  只在 Go 里：${onlyGo.join(', ')}`);
console.error('\n修法：packages/contract/contract.ts 与 contract.go 必须一一对应。');
process.exit(1);
