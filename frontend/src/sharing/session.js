import { reactive } from "vue";
import { request } from "./api.js";

const storageKey = "cloudpaste-sharing-session";
export const session = reactive({
  type: "none",
  token: "",
  name: "",
  basicPath: "/",
  permissions: 0,
  initialized: false,
});

export function saveSession(value) {
  Object.assign(session, value);
  // 密码不保存；仅保存服务器签发的会话或用户自行输入的 API Key。
  try {
    localStorage.setItem(
      storageKey,
      JSON.stringify({
        type: session.type,
        token: session.token,
        name: session.name,
        basicPath: session.basicPath,
        permissions: session.permissions,
      }),
    );
  } catch {
    /* 禁用存储时仍可在当前页面使用。 */
  }
}

export function clearSession() {
  Object.assign(session, {
    type: "none",
    token: "",
    name: "",
    basicPath: "/",
    permissions: 0,
  });
  try {
    localStorage.removeItem(storageKey);
  } catch {
    /* 内存中的登录状态已清除。 */
  }
}

let initialization;
export function initializeSession() {
  if (initialization) return initialization;
  initialization = (async () => {
    try {
      const stored = JSON.parse(localStorage.getItem(storageKey) || "null");
      if (
        stored &&
        ["admin", "apikey"].includes(stored.type) &&
        typeof stored.token === "string"
      )
        Object.assign(session, stored);
      if (session.token) {
        const result = await request(
          session.type === "admin"
            ? "/admin/test/admin-token"
            : "/test/api-key",
        );
        if (session.type === "apikey") {
          const p = result.permissions || {};
          session.permissions =
            (p.text_share ? 1 : 0) |
            (p.file_share ? 2 : 0) |
            (p.text_manage ? 4 : 0) |
            (p.file_manage ? 8 : 0);
          session.name = result.key_info?.name || "API Key";
          session.basicPath = result.basic_path || "/";
        }
      }
    } catch (error) {
      if (
        error.status === 401 ||
        error.status === 403 ||
        error instanceof SyntaxError
      )
        clearSession();
    } finally {
      session.initialized = true;
    }
  })();
  return initialization;
}

export function hasPermission(flag) {
  return session.type === "admin" || (session.permissions & flag) === flag;
}
