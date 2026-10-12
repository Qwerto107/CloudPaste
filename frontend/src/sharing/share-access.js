import { backendBase } from "./api.js";

// sessionStorage 只保留当前标签页的临时凭证；不保存密码、正文或文件列表。
function storageKey(type, slug) {
  return `cloudpaste-share-access:${backendBase}:${type}:${slug}`;
}

export function clearShareAccess(type, slug) {
  try {
    window.sessionStorage.removeItem(storageKey(type, slug));
  } catch {
    /* 浏览器禁用存储时，仍允许在当前页面输入密码访问。 */
  }
}

export function readShareAccess(type, slug) {
  try {
    const stored = JSON.parse(
      window.sessionStorage.getItem(storageKey(type, slug)) || "null",
    );
    if (
      typeof stored?.token === "string" &&
      stored.token.length > 0 &&
      stored.token.length <= 8192 &&
      Number.isFinite(stored.expires) &&
      stored.expires > Date.now()
    )
      return stored.token;
  } catch {
    /* 损坏、过期或不可读的凭证均回到正常密码验证流程。 */
  }
  clearShareAccess(type, slug);
  return "";
}

export function saveShareAccess(type, slug, data) {
  const expires = Date.parse(data.access_expires_at || "");
  if (
    typeof data.access_token !== "string" ||
    !data.access_token ||
    data.access_token.length > 8192 ||
    !Number.isFinite(expires) ||
    expires <= Date.now()
  ) {
    clearShareAccess(type, slug);
    return;
  }
  try {
    window.sessionStorage.setItem(
      storageKey(type, slug),
      JSON.stringify({ token: data.access_token, expires }),
    );
  } catch {
    /* 存储不可用不影响本次已经完成的密码验证。 */
  }
}
