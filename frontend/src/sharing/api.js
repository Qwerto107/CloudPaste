import { session, clearSession } from "./session.js";

export const backendBase = (
  window.appConfig?.backendUrl ||
  (typeof __BACKEND_URL__ !== "undefined" ? __BACKEND_URL__ : "") ||
  window.location.origin
).replace(/\/$/, "");
const base = backendBase;

let guestToken;
function guestUploadToken() {
  // 每个页面使用独立随机凭证，同一批文件可归入合集；不保存为登录身份。
  if (!guestToken) {
    const bytes = crypto.getRandomValues(new Uint8Array(32));
    guestToken = btoa(
      Array.from(bytes, (byte) => String.fromCharCode(byte)).join(""),
    )
      .replace(/\+/g, "-")
      .replace(/\//g, "_")
      .replace(/=+$/, "");
  }
  return guestToken;
}

// 管理员和 API Key 保持独立入口，公开分享绝不附带登录凭据。
export function apiURL(endpoint, publicRequest = false) {
  const scope =
    !publicRequest &&
    session.type === "admin" &&
    !endpoint.startsWith("/admin/")
      ? "/admin"
      : "";
  return `${base}/api${scope}${endpoint}`;
}
export function apiKeyAuthorization(key) {
  // 可见 ASCII 保持原有格式，其他字符按 UTF-8 编码，保留原始空白。
  if (/^[!-~]+$/.test(key)) return `ApiKey ${key}`;
  const bytes = new TextEncoder().encode(key);
  const encoded = btoa(
    Array.from(bytes, (byte) => String.fromCharCode(byte)).join(""),
  );
  return `ApiKeyBase64 ${encoded}`;
}
export function authorization() {
  if (!session.token) return "";
  if (session.type === "admin") return `Bearer ${session.token}`;
  return apiKeyAuthorization(session.token);
}

export async function request(
  endpoint,
  {
    method = "GET",
    body,
    public: publicRequest = false,
    signal,
    shareToken,
  } = {},
) {
  // 数据库等本地文件直接发送原始内容，JSON 请求仍沿用原有编码方式。
  const binary = body instanceof Blob;
  const headers = {
    "Content-Type": binary ? "application/octet-stream" : "application/json",
  };
  if (!publicRequest && authorization())
    headers.Authorization = authorization();
  if (
    !publicRequest &&
    session.type === "none" &&
    endpoint === "/share/collection"
  )
    headers["X-Guest-Token"] = guestUploadToken();
  // 临时分享凭证仅用于公开分享接口，不与管理员或 API Key 身份混用。
  if (publicRequest && shareToken) headers["X-Share-Access"] = shareToken;
  const response = await fetch(apiURL(endpoint, publicRequest), {
    method,
    headers,
    body: body === undefined ? undefined : binary ? body : JSON.stringify(body),
    signal: signal || AbortSignal.timeout(30000),
    cache: "no-store",
  });
  let value;
  try {
    value = await response.json();
  } catch {
    throw new Error(
      `接口未返回 JSON（HTTP ${response.status}），请检查反向代理配置`,
    );
  }
  if (!response.ok || !value.success) {
    const error = Object.assign(
      new Error(value.message || `请求失败（${response.status}）`),
      { status: response.status, code: value.code, data: value.data },
    );
    if (!publicRequest && response.status === 401) clearSession();
    throw error;
  }
  return value.data;
}

export function uploadShare(file, fields, onProgress, signal) {
  return new Promise((resolve, reject) => {
    const xhr = new XMLHttpRequest();
    const abort = () => xhr.abort();
    signal?.addEventListener("abort", abort, { once: true });
    const cleanup = () => signal?.removeEventListener("abort", abort);
    xhr.open("PUT", apiURL("/share/upload"));
    xhr.timeout = 3600000;
    if (authorization()) xhr.setRequestHeader("Authorization", authorization());
    else xhr.setRequestHeader("X-Guest-Token", guestUploadToken());
    xhr.setRequestHeader(
      "Content-Type",
      file.type || "application/octet-stream",
    );
    xhr.setRequestHeader("X-Share-Filename", encodeURIComponent(file.name));
    // UTF-8 编码后转 Base64，中文备注和密码不会损坏。
    const bytes = new TextEncoder().encode(JSON.stringify(fields));
    xhr.setRequestHeader(
      "X-Share-Options",
      btoa(Array.from(bytes, (byte) => String.fromCharCode(byte)).join("")),
    );
    xhr.upload.onprogress = (event) => {
      if (event.lengthComputable)
        onProgress(Math.round((event.loaded / event.total) * 100));
    };
    xhr.onload = () => {
      cleanup();
      try {
        const value = JSON.parse(xhr.responseText);
        if (xhr.status >= 200 && xhr.status < 300 && value.success)
          resolve(value.data);
        else reject(new Error(value.message || "上传失败"));
      } catch {
        reject(new Error(`上传接口返回异常（HTTP ${xhr.status}）`));
      }
    };
    xhr.onerror = () => {
      cleanup();
      reject(new Error("上传连接失败"));
    };
    xhr.ontimeout = () => {
      cleanup();
      reject(new Error("上传超时"));
    };
    xhr.onabort = () => {
      cleanup();
      reject(new Error("上传已取消"));
    };
    if (signal?.aborted) {
      cleanup();
      reject(new Error("上传已取消"));
      return;
    }
    xhr.send(file);
  });
}

// 下载票据也可能经过独立后端域名，统一补全服务器返回的相对链接。
export function contentURL(value) {
  const url = new URL(value, `${base}/`);
  if (!["http:", "https:"].includes(url.protocol))
    throw new Error("文件链接协议无效");
  return url.href;
}
export function shareURL(type, slug) {
  return `${window.location.origin}/${type}/${encodeURIComponent(slug)}`;
}
