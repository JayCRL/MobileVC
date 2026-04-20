#!/usr/bin/env node
/**
 * mobilevc-dev.js
 *
 * Builds Flutter web, replaces cmd/server/web/, starts the backend,
 * and opens the Flutter web in the default browser.
 */

const { spawn, spawnSync } = require('child_process');
const fs = require('fs');
const path = require('path');
const http = require('http');
const os = require('os');

const FLUTTER_DIR = path.join(__dirname, 'mobile_vc');
const WEB_OUTPUT_TMP = path.join(os.tmpdir(), 'mobilevc-web-build');
const WEB_TARGET = path.join(__dirname, 'cmd', 'server', 'web');
const BACKEND_BINARY = path.join(__dirname, 'server');
const DEFAULT_PORT = process.env.PORT || '8001';
const AUTH_TOKEN = process.env.AUTH_TOKEN || 'test-token-12345';

function log(msg) {
  console.log(`[mobilevc-dev] ${msg}`);
}

function run(cmd, args, opts) {
  return new Promise((resolve, reject) => {
    const child = spawn(cmd, args, { stdio: 'inherit', shell: false, ...opts });
    child.on('close', (code) => {
      if (code === 0) resolve(code);
      else reject(new Error(`${cmd} ${args.join(' ')} exited with code ${code}`));
    });
    child.on('error', reject);
  });
}

function rmdir(dir) {
  if (!fs.existsSync(dir)) return;
  fs.rmSync(dir, { recursive: true, force: true });
}

function copyDir(src, dst) {
  fs.mkdirSync(dst, { recursive: true });
  for (const entry of fs.readdirSync(src, { withFileTypes: true })) {
    const srcPath = path.join(src, entry.name);
    const dstPath = path.join(dst, entry.name);
    if (entry.isDirectory()) {
      copyDir(srcPath, dstPath);
    } else {
      fs.copyFileSync(srcPath, dstPath);
    }
  }
}

async function checkHealth(port, timeoutMs = 10000) {
  const start = Date.now();
  return new Promise((resolve) => {
    const poll = () => {
      const req = http.get({ hostname: '127.0.0.1', port: Number(port), path: '/healthz', timeout: 2000 }, (res) => {
        let body = '';
        res.setEncoding('utf8');
        res.on('data', (chunk) => { body += chunk; });
        res.on('end', () => resolve(res.statusCode === 200 && body.trim() === 'ok'));
      });
      req.on('timeout', () => { req.destroy(); resolve(false); });
      req.on('error', () => resolve(false));
    };
    (function tick() {
      if (Date.now() - start >= timeoutMs) return resolve(false);
      poll();
      setTimeout(tick, 400);
    })();
  });
}

async function main() {
  // 1. Build Flutter web
  log('Building Flutter web...');
  if (fs.existsSync(WEB_OUTPUT_TMP)) {
    rmdir(WEB_OUTPUT_TMP);
  }
  await run('/Users/wust_lh/flutter_sdk/flutter/bin/flutter', ['build', 'web', '--output-dir=' + WEB_OUTPUT_TMP], { cwd: FLUTTER_DIR });

  // 2. Sync to cmd/server/web/
  log('Replacing cmd/server/web/...');
  rmdir(WEB_TARGET);
  copyDir(WEB_OUTPUT_TMP, WEB_TARGET);
  rmdir(WEB_OUTPUT_TMP);
  log('Flutter web synced to cmd/server/web/');

  // 3. Compile backend
  log('Compiling backend...');
  const buildResult = spawnSync('go', ['build', '-o', BACKEND_BINARY, './cmd/server'], {
    cwd: __dirname,
    stdio: 'inherit',
  });
  if (buildResult.status !== 0) {
    console.error('[mobilevc-dev] Backend build failed');
    process.exit(1);
  }

  // 4. Stop existing server
  log('Stopping existing server...');
  try {
    spawnSync('pkill', ['-f', './server'], { stdio: 'ignore' });
    await new Promise(r => setTimeout(r, 1000));
  } catch (_) {}

  // 5. Start backend
  log('Starting backend on port ' + DEFAULT_PORT + '...');
  const env = {
    ...process.env,
    PORT: DEFAULT_PORT,
    AUTH_TOKEN: AUTH_TOKEN,
  };
  const serverProc = spawn(BACKEND_BINARY, [], {
    detached: true,
    stdio: 'ignore',
    env,
  });
  serverProc.unref();

  // 6. Wait for health
  const healthy = await checkHealth(DEFAULT_PORT);
  if (!healthy) {
    console.error('[mobilevc-dev] Backend failed to start. Check server.log');
    process.exit(1);
  }

  // 7. Open browser
  const url = `http://127.0.0.1:${DEFAULT_PORT}/`;
  log('Opening browser: ' + url);
  spawnSync('open', [url], { stdio: 'ignore' });
  log('Done!');
}

main().catch((err) => {
  console.error('[mobilevc-dev] Error:', err.message);
  process.exit(1);
});
