/* MobileVC service worker: installable shell + Web Push. */
const VERSION = 'mobilevc-2026-10-26-boot2';
self.addEventListener('install', () => self.skipWaiting());
self.addEventListener('activate', (event) => event.waitUntil(self.clients.claim()));
self.addEventListener('fetch', () => {
  /* pass through: the UI needs live DSH data, never a cached API response */
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
