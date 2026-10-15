import assert from "node:assert/strict";
import { test } from "node:test";
globalThis.window = {
  location: { origin: "https://share.example" },
  appConfig: { backendUrl: "" },
};
globalThis.localStorage = { setItem() {}, removeItem() {} };
const { session, saveSession, clearSession, hasPermission, canTransfer } =
  await import("../src/sharing/session.js");
const { settings } = await import("../src/sharing/settings.js");
const { apiURL, request, uploadShare, contentURL } =
  await import("../src/sharing/api.js");

test("访客四项权限分别控制，管理权限保持关闭，登录后沿用账号权限", () => {
  clearSession();
  assert.equal(hasPermission(1), false);
  assert.equal(hasPermission(2), false);
  assert.equal(canTransfer("create"), false);
  assert.equal(canTransfer("join"), true);
  settings.guest_text_share_enabled = "true";
  settings.guest_file_upload_enabled = "true";
  settings.guest_transfer_join_enabled = "false";
  assert.equal(hasPermission(3), true);
  assert.equal(hasPermission(4), false);
  assert.equal(hasPermission(8), false);
  assert.equal(canTransfer("create"), false);
  assert.equal(canTransfer("join"), false);
  settings.guest_transfer_create_enabled = "true";
  assert.equal(canTransfer("create"), true);
  saveSession({ type: "apikey", token: "test-key", permissions: 1 });
  assert.equal(hasPermission(2), false);
  assert.equal(canTransfer("create"), false);
  assert.equal(canTransfer("join"), true);
  Object.assign(settings, {
    guest_text_share_enabled: "false",
    guest_file_upload_enabled: "false",
    guest_transfer_create_enabled: "false",
    guest_transfer_join_enabled: "true",
  });
});

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

test("访客上传与合集共用随机归属凭证，不附带登录身份", async () => {
  clearSession();
  let xhr;
  globalThis.XMLHttpRequest = class {
    constructor() {
      xhr = this;
      this.headers = {};
      this.upload = {};
    }
    open() {}
    setRequestHeader(name, value) {
      this.headers[name] = value;
    }
    send() {
      this.status = 201;
      this.responseText = JSON.stringify({ success: true, data: {} });
      this.onload();
    }
  };
  await uploadShare({ name: "guest.txt", type: "text/plain" }, {}, () => {});
  const token = xhr.headers["X-Guest-Token"];
  assert.equal(Buffer.from(token, "base64url").length, 32);
  assert.equal(xhr.headers.Authorization, undefined);
  globalThis.fetch = async (_url, options) => {
    assert.equal(options.headers["X-Guest-Token"], token);
    assert.equal(options.headers.Authorization, undefined);
    return new Response(JSON.stringify({ success: true, data: {} }));
  };
  await request("/share/collection", {
    method: "POST",
    body: { ids: ["first", "second"] },
  });
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
