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
