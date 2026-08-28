/* SoundDock PWA: skipWaiting only. No fetch handler - /api/ and stream
   URLs must never be intercepted (HTMLAudio playback). In production,
   vite-plugin-pwa may replace this file with a shell-only Workbox SW
   that also does not runtime-cache /api/. */
self.addEventListener("install", () => {
  self.skipWaiting();
});

self.addEventListener("activate", (event) => {
  event.waitUntil(self.clients.claim());
});
