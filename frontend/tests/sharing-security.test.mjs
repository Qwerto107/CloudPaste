import assert from "node:assert/strict";
import { after, test } from "node:test";
import { JSDOM } from "jsdom";
import { readFile } from "node:fs/promises";
import { compileScript, parse } from "vue/compiler-sfc";
import vm from "node:vm";
const dom = new JSDOM("", { url: "https://share.example/" });
Object.assign(globalThis, {
  window: dom.window,
  document: dom.window.document,
  Element: dom.window.Element,
  HTMLElement: dom.window.HTMLElement,
  SVGElement: dom.window.SVGElement,
});
// 编译并挂载页面实际使用的组件，让安全测试经过相同的解析器和清洗入口。
const previewURL = new URL(
  "../src/sharing/MarkdownPreview.vue",
  import.meta.url,
);
const { descriptor } = parse(await readFile(previewURL, "utf8"));
const compiled = compileScript(descriptor, {
  id: "markdown-security-test",
  inlineTemplate: true,
});
const componentCode = compiled.content
  // Node 不加载样式；组件的脚本与模板均保留，由 Vue 执行。
  .replace(/^import\s+["'][^"']+\.css["'];?\s*$/gm, "")
  .replace(/(from\s+)(["'])([^"']+)\2/g, (_, prefix, quote, specifier) => {
    const resolved = specifier.startsWith(".")
      ? new URL(specifier, previewURL).href
      : import.meta.resolve(specifier);
    return `${prefix}${quote}${resolved}${quote}`;
  });
const { default: MarkdownPreview } = await import(
  `data:text/javascript;base64,${Buffer.from(componentCode).toString("base64")}`
);
const { createApp, h, nextTick } = await import("vue");
const { sanitizeHTML } = await import("../src/sharing/sanitize.js");
const { settings } = await import("../src/sharing/settings.js");
after(() => dom.window.close());

async function renderPreview(content, allowExternalImages = false) {
  const previous = settings.preview_allow_external_images;
  settings.preview_allow_external_images = String(allowExternalImages);
  const container = document.createElement("div");
  document.body.append(container);
  const app = createApp({ render: () => h(MarkdownPreview, { content }) });
  try {
    app.mount(container);
    await nextTick();
    return container.querySelector(".md-editor-preview").innerHTML;
  } finally {
    app.unmount();
    container.remove();
    settings.preview_allow_external_images = previous;
  }
}

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

test("恶意 Markdown、SVG 事件与脚本链接不能变成可执行内容", async () => {
  const html = await renderPreview(
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
test("默认禁止站外图片与 srcset，保留本站图片和正常 Markdown", async () => {
  const html = await renderPreview(
    '# 标题\n![本站](/image.png)\n![追踪](https://tracker.example/x)\n<img src="/local" srcset="https://tracker.example/retina 2x">',
  );
  const body = new JSDOM(html).window.document;
  assert.equal(body.querySelector("h1").textContent, "标题");
  assert.equal(body.querySelectorAll("img").length, 2);
  assert.equal(body.querySelector("img[srcset]"), null);
  assert.equal(html.includes("tracker.example"), false);
  assert.ok(
    (await renderPreview("![opt-in](https://images.example/x)", true)).includes(
      "images.example",
    ),
  );
});
test("嵌入位图经过真实 Markdown 预览，SVG 数据图片仍被移除", async () => {
  const image =
    "data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+aD1sAAAAASUVORK5CYII=";
  const html = await renderPreview(
    `![图片](${image})\n<img src="data:image/svg+xml;base64,PHN2Zy8+" onerror="alert(1)">`,
  );
  const body = new JSDOM(html).window.document;
  assert.equal(body.querySelectorAll("img").length, 1);
  assert.equal(body.querySelector("img").getAttribute("src"), image);
  assert.equal(body.querySelector("[onerror]"), null);
});
test("自定义站点 HTML 使用相同安全过滤，不接受内联脚本、表单或图片数据 URI", () => {
  const html = sanitizeHTML(
    '<form action="https://evil.example"><input></form><p style="background:url(https://evil.example)">公告</p><img src="data:image/svg+xml,test"><a href="https://example.com" target="_blank">链接</a>',
  );
  const body = new JSDOM(html).window.document;
  assert.equal(body.querySelector("form,input,img,[style]"), null);
  assert.equal(body.querySelector("a").rel, "noopener noreferrer");
});
test("Markdown 和自定义 HTML 均移除表格背景，正常文本与图片授权仍有效", async () => {
  const content =
    '<table background="https://tracker.example/table"><tr><th background="https://tracker.example/header">表头</th><td background="https://tracker.example/cell">正文</td></tr></table><p>普通文本</p><img src="/local.png"><img src="https://images.example/allowed.png">';
  // 开启站外图片只放行 img，旧式表格背景在两条渲染路径下始终被移除。
  for (const render of [renderPreview, sanitizeHTML]) {
    for (const allowExternalImages of [false, true]) {
      const html = await render(content, allowExternalImages);
      const body = new JSDOM(html).window.document;
      assert.equal(body.querySelector("[background]"), null);
      assert.equal(html.includes("tracker.example"), false);
      assert.equal(body.querySelector("th").textContent, "表头");
      assert.equal(body.querySelector("td").textContent, "正文");
      assert.equal(body.querySelector("p").textContent, "普通文本");
      assert.equal(body.querySelector('img[src="/local.png"]') !== null, true);
      assert.equal(
        body.querySelector('img[src="https://images.example/allowed.png"]') !==
          null,
        allowExternalImages,
      );
    }
  }
});
