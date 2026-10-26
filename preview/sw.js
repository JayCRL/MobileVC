/* MobileVC service worker: installable shell + Web Push. */
const VERSION = 'mobilevc-2026-10-26-fill';
const BOOT_CACHE = 'mobilevc-boot-fill-v1';
const BOOT_URL = '/mobilevc/assets/startup-boot-portrait-fill.mp4';
self.addEventListener('install', (event) => {
  event.waitUntil((async () => {
    try {
      const response = await fetch(BOOT_URL, { cache: 'reload', credentials: 'same-origin' });
      const type = response.headers.get('content-type') || '';
      if (response.ok && type.startsWith('video/')) {
        const cache = await caches.open(BOOT_CACHE);
        await cache.put(BOOT_URL, response.clone());
      }
    } catch (_) {}
    await self.skipWaiting();
  })());
});
self.addEventListener('activate', (event) => event.waitUntil(self.clients.claim()));
self.addEventListener('fetch', (event) => {
  if (event.request.method !== 'GET') return;
  const url = new URL(event.request.url);
  if (url.pathname === BOOT_URL) {
    event.respondWith(caches.match(event.request).then((cached) => cached || fetch(event.request)));
    return;
  }
  /* everything else passes through: the UI needs live DSH data */
});

self.addEventListener('push', (event) => {
  let data = {};
  try { data = event.data ? event.data.json() : {}; } catch (_) {}
  const title = data.title || 'MobileVC';
  const body = data.body || 'DSH 有新状态';
  event.waitUntil(self.registration.showNotification(title, {
    body,
    icon: '/mobilevc/assets/icon-192.png',
    badge: '/mobilevc/assets/icon-192.png',
    tag: data.tag || 'mobilevc',
    renotify: true,
    data: { url: data.url || '/mobilevc/' },
  }));
});

self.addEventListener('notificationclick', (event) => {
  event.notification.close();
  const url = event.notification.data?.url || '/mobilevc/';
  event.waitUntil((async () => {
    const windows = await self.clients.matchAll({ type: 'window', includeUncontrolled: true });
    const existing = windows.find((client) => client.url.includes('/mobilevc'));
    if (existing) {
      await existing.focus();
      return;
    }
    await self.clients.openWindow(url);
  })());
});
