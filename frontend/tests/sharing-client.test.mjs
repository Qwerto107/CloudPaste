import assert from "node:assert/strict";
import { test } from "node:test";
globalThis.window = {
  location: { origin: "https://share.example" },
  appConfig: { backendUrl: "" },
};
globalThis.localStorage = { setItem() {}, removeItem() {} };
const { session, saveSession } = await import("../src/sharing/session.js");
const { apiURL, request, uploadShare, contentURL } =
  await import("../src/sharing/api.js");

test("管理员走独立前缀，公开分享请求不携带凭据", async () => {
  saveSession({ type: "admin", token: "test-admin", permissions: 15 });
  assert.equal(apiURL("/pastes"), "https://share.example/api/admin/pastes");
  assert.equal(
    apiURL("/admin/settings"),
    "https://share.example/api/admin/settings",
  );
  globalThis.fetch = async (url, options) => {
    assert.equal(url, "https://share.example/api/paste/public");
    assert.equal(options.headers.Authorization, undefined);
    return new Response(
      JSON.stringify({ success: true, data: { content: "hello" } }),
    );
  };
  assert.equal(
    (await request("/paste/public", { public: true })).content,
    "hello",
  );
  assert.throws(() => contentURL("javascript:alert(1)"));
});
test("上传原始文件和中文选项，不生成 multipart 副本；撤销信号确实中断上传", async () => {
  saveSession({ type: "apikey", token: "test-key", permissions: 2 });
  let xhr;
  globalThis.XMLHttpRequest = class {
    constructor() {
      xhr = this;
      this.headers = {};
      this.upload = {};
    }
    open(method, url) {
      this.method = method;
      this.url = url;
    }
    setRequestHeader(name, value) {
      this.headers[name] = value;
    }
    send(file) {
      this.file = file;
    }
    abort() {
      this.onabort();
    }
  };
  const controller = new AbortController();
  const file = { name: "测试.txt", type: "text/plain" };
  const pending = uploadShare(
    file,
    { password: "中文密码", remark: "备注", expires_in: 24 },
    () => {},
    controller.signal,
  );
  assert.equal(xhr.method, "PUT");
  assert.equal(xhr.url, "https://share.example/api/share/upload");
  assert.equal(xhr.file, file);
  assert.equal(xhr.headers.Authorization, "ApiKey test-key");
  assert.equal(decodeURIComponent(xhr.headers["X-Share-Filename"]), "测试.txt");
  assert.deepEqual(
    JSON.parse(
      Buffer.from(xhr.headers["X-Share-Options"], "base64").toString("utf8"),
    ),
    { password: "中文密码", remark: "备注", expires_in: 24 },
  );
  controller.abort();
  await assert.rejects(pending, /取消/);
});
test("认证过期清除本地身份，403 不破坏有效身份", async () => {
  globalThis.fetch = async () =>
    new Response(JSON.stringify({ success: false, message: "无权限" }), {
      status: 403,
    });
  await assert.rejects(request("/files"), /无权限/);
  assert.equal(session.type, "apikey");
  globalThis.fetch = async () =>
    new Response(JSON.stringify({ success: false, message: "会话过期" }), {
      status: 401,
    });
  await assert.rejects(request("/files"), /会话过期/);
  assert.equal(session.type, "none");
});

test("公开分享使用独立解锁凭证，不附带登录身份或把凭证放入 URL", async () => {
  saveSession({ type: "admin", token: "test-admin", permissions: 15 });
  globalThis.fetch = async (url, options) => {
    assert.equal(url, "https://share.example/api/paste/protected");
    assert.equal(options.headers.Authorization, undefined);
    assert.equal(options.headers["X-Share-Access"], "temporary-grant");
    return new Response(
      JSON.stringify({ success: true, data: { content: "hello" } }),
    );
  };
  await request("/paste/protected", {
    public: true,
    shareToken: "temporary-grant",
  });
});

test("分享凭证按类型和链接隔离，只保存 token 与期限，过期或损坏后清除", async () => {
  const storage = new Map();
  window.sessionStorage = {
    getItem: (key) => storage.get(key) ?? null,
    setItem: (key, value) => storage.set(key, value),
    removeItem: (key) => storage.delete(key),
  };
  const { readShareAccess, saveShareAccess, clearShareAccess } =
    await import("../src/sharing/share-access.js");
  saveShareAccess("text", "same", {
    access_token: "text-token",
    access_expires_at: new Date(Date.now() + 60000).toISOString(),
    password: "must-not-store",
    content: "private",
  });
  saveShareAccess("file", "same", {
    access_token: "file-token",
    access_expires_at: new Date(Date.now() + 60000).toISOString(),
  });
  assert.equal(readShareAccess("text", "same"), "text-token");
  assert.equal(readShareAccess("file", "same"), "file-token");
  assert.equal(readShareAccess("text", "another"), "");
  for (const stored of storage.values())
    assert.deepEqual(Object.keys(JSON.parse(stored)).sort(), [
      "expires",
      "token",
    ]);
  clearShareAccess("file", "same");
  assert.equal(readShareAccess("file", "same"), "");
  const textKey = [...storage.keys()][0];
  storage.set(
    textKey,
    JSON.stringify({ token: "expired", expires: Date.now() - 1 }),
  );
  assert.equal(readShareAccess("text", "same"), "");
  assert.equal(storage.has(textKey), false);
  storage.set(textKey, "invalid-json");
  assert.equal(readShareAccess("text", "same"), "");
  window.sessionStorage = {
    getItem() {
      throw new Error("blocked");
    },
    setItem() {
      throw new Error("blocked");
    },
    removeItem() {
      throw new Error("blocked");
    },
  };
  assert.equal(readShareAccess("text", "same"), "");
  assert.doesNotThrow(() =>
    saveShareAccess("text", "same", {
      access_token: "token",
      access_expires_at: new Date(Date.now() + 60000).toISOString(),
    }),
  );
});
