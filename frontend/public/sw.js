// 升级时接管旧 PWA，然后撤销预缓存；本站不再离线缓存分享内容。
self.addEventListener("install", () => self.skipWaiting());
self.addEventListener("activate", (event) =>
  event.waitUntil(
    (async () => {
      const owned = new Set([
        "app-static-resources",
        "fonts",
        "external-cdn-resources",
        "gallery-images",
        "user-documents",
        "app-images",
        "system-api",
        "public-api",
        "fs-dynamic-operations",
        "fs-presign-operations",
        "pastes-realtime",
        "search-realtime",
        "upload-operations",
        "admin-config-read",
        "admin-config-write",
        "presigned-urls-realtime",
        "webdav-realtime",
      ]);
      for (const name of await caches.keys())
        if (owned.has(name) || name.startsWith("workbox-precache-"))
          await caches.delete(name);
      await self.clients.claim();
      await self.registration.unregister();
      // 已打开的旧页面只刷新一次，随后使用没有离线缓存的新页面。
      for (const client of await self.clients.matchAll({ type: "window" }))
        await client.navigate(client.url);
    })(),
  ),
);
