#!/usr/bin/env node

const fs = require('fs');
const path = require('path');

const ROOT = path.resolve(__dirname, '..');
const SOURCE_DIR = path.join(ROOT, 'mobile_vc', 'build', 'web');
const TARGET_DIR = path.join(ROOT, 'cmd', 'server', 'web');
const KEEP_FILE = path.join(TARGET_DIR, '.gitkeep');
const SERVICE_WORKER_FILE = path.join(TARGET_DIR, 'flutter_service_worker.js');
const CLEANUP_FILE = path.join(TARGET_DIR, 'sw-cleanup.html');
const SERVICE_WORKER_CLEANUP = String.raw`self.addEventListener('install', (event) => {
  event.waitUntil(self.skipWaiting());
});

self.addEventListener('activate', (event) => {
  event.waitUntil((async () => {
    const keys = await caches.keys();
    await Promise.all(keys.map((key) => caches.delete(key)));
    await self.clients.claim();
    await self.registration.unregister();
  })());
});
`;
const CLEANUP_HTML = String.raw`<!doctype html>
<html>
  <head>
    <meta charset="utf-8">
    <meta name="viewport" content="width=device-width, initial-scale=1">
    <title>MobileVC Service Worker Cleanup</title>
  </head>
  <body>
    <pre id="status">Cleaning service worker cache...</pre>
    <script>
      (async () => {
        const lines = [];
        const log = (message) => {
          lines.push(message);
          document.getElementById('status').textContent = lines.join('\n');
        };

        if ('serviceWorker' in navigator) {
          const registration = await navigator.serviceWorker.register('/flutter_service_worker.js?kill=' + Date.now());
          await registration.update();
          log('updated: ' + registration.scope);

          const registrations = await navigator.serviceWorker.getRegistrations();
          for (const current of registrations) {
            await current.unregister();
            log('unregistered: ' + current.scope);
          }
        }

        if ('caches' in window) {
          const keys = await caches.keys();
          for (const key of keys) {
            await caches.delete(key);
            log('deleted cache: ' + key);
          }
        }

        log('done. redirecting...');
        setTimeout(() => {
          location.replace('/?cacheBust=' + Date.now());
        }, 500);
      })().catch((error) => {
        document.getElementById('status').textContent = String(error);
      });
    </script>
  </body>
</html>
`;

syncDir(SOURCE_DIR, TARGET_DIR);

function syncDir(sourceDir, targetDir) {
  if (!fs.existsSync(sourceDir)) {
    throw new Error(`Source web directory not found: ${sourceDir}`);
  }

  fs.rmSync(targetDir, { recursive: true, force: true });
  fs.mkdirSync(targetDir, { recursive: true });
  copyRecursive(sourceDir, targetDir);
  fs.writeFileSync(SERVICE_WORKER_FILE, SERVICE_WORKER_CLEANUP);
  fs.writeFileSync(CLEANUP_FILE, CLEANUP_HTML);
  fs.closeSync(fs.openSync(KEEP_FILE, 'a'));
}

function copyRecursive(sourceDir, targetDir) {
  const entries = fs.readdirSync(sourceDir, { withFileTypes: true });
  for (const entry of entries) {
    const sourcePath = path.join(sourceDir, entry.name);
    const targetPath = path.join(targetDir, entry.name);

    if (entry.isDirectory()) {
      fs.mkdirSync(targetPath, { recursive: true });
      copyRecursive(sourcePath, targetPath);
      continue;
    }

    if (entry.isFile()) {
      fs.copyFileSync(sourcePath, targetPath);
    }
  }
}
