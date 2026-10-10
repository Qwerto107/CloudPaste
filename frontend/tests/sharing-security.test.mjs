import assert from "node:assert/strict";
import { test } from "node:test";
import { JSDOM } from "jsdom";
import { readFile } from "node:fs/promises";
import vm from "node:vm";
const dom = new JSDOM("", { url: "https://share.example/" });
globalThis.window = dom.window;
globalThis.document = dom.window.document;
const { renderMarkdown } = await import("../src/sharing/render.js");
const { sanitizeHTML } = await import("../src/sharing/sanitize.js");

test("升级 Worker 清理旧分享缓存、撤销注册并刷新一次，不再拦截资源请求", async () => {
  const events = new Map();
  const deleted = [];
  let unregistered = false;
  let refreshed = 0;
  const context = vm.createContext({
    self: {
      addEventListener: (name, callback) => events.set(name, callback),
      skipWaiting() {},
      registration: {
        async unregister() {
          unregistered = true;
        },
      },
      clients: {
        async claim() {},
        async matchAll() {
          return [
            {
              url: "https://share.example/admin/login",
              async navigate() {
                refreshed++;
              },
            },
          ];
        },
      },
    },
    caches: {
      async keys() {
        return [
          "workbox-precache-v2",
          "admin-config-read",
          "webdav-realtime",
          "foreign-application",
        ];
      },
      async delete(name) {
        deleted.push(name);
      },
    },
  });
  vm.runInContext(
    await readFile(new URL("../public/sw.js", import.meta.url), "utf8"),
    context,
  );
  assert.equal(events.has("fetch"), false);
  let completion;
  events.get("activate")({
    waitUntil(promise) {
      completion = promise;
    },
  });
  await completion;
  assert.deepEqual(deleted, [
    "workbox-precache-v2",
    "admin-config-read",
    "webdav-realtime",
  ]);
  assert.equal(unregistered, true);
  assert.equal(refreshed, 1);
});

test("恶意 Markdown、SVG 事件与脚本链接不能变成可执行内容", () => {
  const html = renderMarkdown(
    '[click](javascript:alert(1)) <img src="/x" onerror="alert(2)"><svg/onload=alert(3)><script>alert(4)</script><iframe src="/api/admin"></iframe>',
  );
  const body = new JSDOM(html).window.document;
  assert.equal(body.querySelector("script,iframe,svg"), null);
  for (const element of body.querySelectorAll("*"))
    for (const attr of element.attributes)
      assert.equal(
        attr.name.startsWith("on") || attr.value.startsWith("javascript:"),
        false,
      );
});
test("默认禁止站外图片与 srcset，保留本站图片和正常 Markdown", () => {
  const html = renderMarkdown(
    '# 标题\n![本站](/image.png)\n![追踪](https://tracker.example/x)\n<img src="/local" srcset="https://tracker.example/retina 2x">',
  );
  const body = new JSDOM(html).window.document;
  assert.equal(body.querySelector("h1").textContent, "标题");
  assert.equal(body.querySelectorAll("img").length, 2);
  assert.equal(body.querySelector("img[srcset]"), null);
  assert.equal(html.includes("tracker.example"), false);
  assert.ok(
    renderMarkdown("![opt-in](https://images.example/x)", true).includes(
      "images.example",
    ),
  );
});
test("自定义站点 HTML 使用相同安全过滤，不接受内联脚本、表单或图片数据 URI", () => {
  const html = sanitizeHTML(
    '<form action="https://evil.example"><input></form><p style="background:url(https://evil.example)">公告</p><img src="data:image/svg+xml,test"><a href="https://example.com" target="_blank">链接</a>',
  );
  const body = new JSDOM(html).window.document;
  assert.equal(body.querySelector("form,input,img,[style]"), null);
  assert.equal(body.querySelector("a").rel, "noopener noreferrer");
});
