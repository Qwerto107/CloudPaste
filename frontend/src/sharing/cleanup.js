// 仅清理旧 CloudPaste 使用的缓存，保留同域其他用途的 Cache Storage。
const owned = new Set([
  "admin-config-read",
  "admin-config-write",
  "app-images",
  "app-static-resources",
  "external-cdn-resources",
  "fonts",
  "fs-dynamic-operations",
  "fs-presign-operations",
  "gallery-images",
  "pastes-realtime",
  "presigned-urls-realtime",
  "public-api",
  "search-realtime",
  "system-api",
  "upload-operations",
  "user-documents",
  "webdav-realtime",
]);
export async function cleanupLegacyCaches() {
  if (!("serviceWorker" in navigator)) return;
  const registrations = await navigator.serviceWorker.getRegistrations();
  let upgraded = false;
  for (const registration of registrations)
    if (registration.scope === `${location.origin}/`) {
      await registration.unregister();
      upgraded = true;
    }
  if ("caches" in globalThis)
    for (const name of await caches.keys()) {
      if (owned.has(name) || name.startsWith("workbox-precache-"))
        await caches.delete(name);
    }
  // 注销后当前文档仍可能受旧 Worker 控制，刷新一次才会完全退出旧缓存流程。
  if (upgraded && navigator.serviceWorker.controller) location.reload();
}
