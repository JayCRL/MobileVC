/* MobileVC service worker: installable shell, network-first, no API caching. */
const VERSION = 'mobilevc-2026-10-25';
self.addEventListener('install', () => self.skipWaiting());
self.addEventListener('activate', (event) => event.waitUntil(self.clients.claim()));
self.addEventListener('fetch', () => {
  /* pass through: the UI needs live DSH data, never a cached API response */
});
