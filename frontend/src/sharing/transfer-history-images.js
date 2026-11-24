let database;

function open() {
  return (database ??= new Promise((resolve, reject) => {
    const request = indexedDB.open("cloudpaste-transfer-history", 1);
    request.onupgradeneeded = () => request.result.createObjectStore("images");
    request.onsuccess = () => resolve(request.result);
    request.onerror = () => reject(request.error);
  }));
}

export async function readHistoryImages(key) {
  try {
    const db = await open();
    return await new Promise((resolve, reject) => {
      const request = db.transaction("images").objectStore("images").get(key);
      request.onsuccess = () => resolve(request.result || []);
      request.onerror = () => reject(request.error);
    });
  } catch {
    return [];
  }
}

export async function writeHistoryImages(key, images) {
  try {
    const db = await open();
    const store = db.transaction("images", "readwrite").objectStore("images");
    if (images.length) store.put(images, key);
    else store.delete(key);
  } catch {
    // 无法写入浏览器存储时，图片仍可在本页预览。
  }
}
