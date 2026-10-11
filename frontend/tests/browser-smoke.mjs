import assert from "node:assert/strict";
import { createServer } from "node:http";
import { mkdtemp, mkdir, readFile, readdir, rm } from "node:fs/promises";
import { existsSync } from "node:fs";
import { spawn } from "node:child_process";
import { resolve, extname } from "node:path";
import { fileURLToPath } from "node:url";
import { chromium } from "playwright";

const root = fileURLToPath(new URL("../../", import.meta.url));
const standalone = process.env.SHARING_TEST_STANDALONE === "1";
const binary =
  process.env.SHARING_TEST_BINARY ||
  resolve(root, "backend/bin/sharing-backend.exe");
if (!existsSync(binary))
  throw new Error(
    "请先构建 backend/bin/sharing-backend.exe，或设置 SHARING_TEST_BINARY",
  );
const temporary = await mkdtemp(resolve(root, "backend/bin/sharing-test-"));
const filesDirectory = resolve(temporary, "files");
await mkdir(filesDirectory);
const password = "isolated-sharing-test-password";
let service, browser;
// 使用独立临时库和端口，测试不会连接用户部署或读取已有数据。
const reservation = createServer();
await new Promise((done) => reservation.listen(0, "127.0.0.1", done));
const backendPort = reservation.address().port;
await new Promise((done) => reservation.close(done));
const backend = `http://127.0.0.1:${backendPort}`;
const staticRoot = resolve(root, "frontend/dist");
const mime = {
  ".js": "text/javascript",
  ".css": "text/css",
  ".svg": "image/svg+xml",
  ".html": "text/html",
};
const frontend = createServer(async (request, response) => {
  try {
    if (request.url.startsWith("/api/")) {
      const options = { method: request.method, headers: request.headers };
      if (!["GET", "HEAD"].includes(request.method)) {
        options.body = request;
        options.duplex = "half";
      }
      const upstream = await fetch(backend + request.url, options);
      response.writeHead(upstream.status, Object.fromEntries(upstream.headers));
      response.end(Buffer.from(await upstream.arrayBuffer()));
      return;
    }
    const pathname = new URL(request.url, "http://localhost").pathname;
    response.setHeader(
      "Content-Security-Policy",
      "default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self' https: http:; media-src 'self'; connect-src 'self'; object-src 'none'; base-uri 'self'; frame-ancestors 'none'; form-action 'self'",
    );
    let target = resolve(staticRoot, "." + pathname);
    if (
      !target.startsWith(staticRoot + "/") &&
      !target.startsWith(staticRoot + "\\")
    )
      target = resolve(staticRoot, "index.html");
    if (!existsSync(target) || pathname === "/")
      target = resolve(staticRoot, "index.html");
    response.setHeader(
      "Content-Type",
      mime[extname(target)] || "application/octet-stream",
    );
    response.end(await readFile(target));
  } catch (error) {
    response.writeHead(500);
    response.end(error.message);
  }
});
try {
  service = spawn(binary, [], {
    env: {
      ...process.env,
      ADMIN_USERNAME: "admin",
      ADMIN_PASSWORD: password,
      DATA_DIR: resolve(temporary, "data"),
      PORT: String(backendPort),
    },
    windowsHide: true,
    stdio: ["ignore", "pipe", "pipe"],
  });
  let logs = "";
  service.stderr.on("data", (value) => (logs += value));
  service.stdout.on("data", (value) => (logs += value));
  let ready = false;
  for (let attempt = 0; attempt < 100; attempt++) {
    try {
      ready = (await fetch(backend + "/api/health")).ok;
    } catch {}
    if (ready) break;
    await new Promise((done) => setTimeout(done, 100));
  }
  assert.ok(ready, logs);
  async function api(path, method = "GET", body, authorization) {
    const response = await fetch(backend + path, {
      method,
      headers: {
        "Content-Type": "application/json",
        ...(authorization ? { Authorization: authorization } : {}),
      },
      body: body === undefined ? undefined : JSON.stringify(body),
    });
    const result = await response.json();
    assert.ok(response.ok && result.success, JSON.stringify(result));
    return result.data;
  }
  const admin =
    "Bearer " +
    (await api("/api/admin/login", "POST", { username: "admin", password }))
      .token;
  const storage = await api(
    "/api/admin/storage",
    "POST",
    {
      name: "本地测试存储",
      storage_type: "LOCAL",
      root_path: filesDirectory,
      is_public: true,
    },
    admin,
  );
  const user = await api(
    "/api/admin/api-keys",
    "POST",
    { name: "分享用户", permissions: 527, basic_path: "/" },
    admin,
  );
  // 合并版直接访问同一个 Go 进程的页面与 API，不能借用测试静态代理掩盖嵌入问题。
  let origin = backend;
  if (!standalone) {
    await new Promise((done) => frontend.listen(0, "127.0.0.1", done));
    origin = `http://127.0.0.1:${frontend.address().port}`;
  }
  console.log(`浏览器回归：${standalone ? "内嵌" : "分离"}前端与 API`);
  const installed = [
    process.env.SHARING_TEST_BROWSER,
    "C:/Program Files/Google/Chrome/Application/chrome.exe",
    "C:/Program Files (x86)/Microsoft/Edge/Application/msedge.exe",
  ].find((path) => path && existsSync(path));
  browser = await chromium.launch({
    headless: true,
    ...(installed ? { executablePath: installed } : {}),
  });
  const context = await browser.newContext({
    colorScheme: "dark",
    viewport: { width: 1440, height: 1000 },
  });
  context.setDefaultTimeout(15000);
  context.setDefaultNavigationTimeout(15000);
  const page = await context.newPage();
  const external = [];
  const downloads = [];
  const errors = [];
  const backgroundHTML =
    '<table background="https://tracker.example/table"><tr><th background="https://tracker.example/header">表头</th><td background="https://tracker.example/cell">正文</td></tr></table>';
  // 即使过滤回归，也只记录并中止测试追踪请求，避免浏览器真正访问站外服务。
  await page.route("https://tracker.example/**", (route) => route.abort());
  page.on("request", (request) => {
    downloads.push(request.url());
    if (!request.url().startsWith(origin)) external.push(request.url());
  });
  page.on("pageerror", (error) => errors.push(error.message));
  await page.goto(origin + "/login");
  await page.getByRole("link", { name: "管理员登录 →", exact: true }).click();
  await page.waitForURL("**/admin/login");
  // 首次跟随系统，手动选择后刷新仍保留；严格 CSP 下无需内联脚本。
  assert.equal(await page.locator("html").getAttribute("data-theme"), "dark");
  await page.getByRole("button", { name: "切换到浅色模式" }).click();
  await page.reload();
  assert.equal(await page.locator("html").getAttribute("data-theme"), "light");
  await page.screenshot({
    path: resolve(root, "backend/bin/mint-login-light.png"),
    fullPage: true,
  });
  await page.getByLabel("账号", { exact: true }).fill("admin");
  await page.getByLabel("密码", { exact: true }).fill(password);
  await page.getByRole("button", { name: "登录", exact: true }).click();
  await page.waitForURL("**/admin/text-management");
  const loginResourceRequests = downloads.length;
  assert.ok(
    !downloads.some((url) =>
      /vditor|highlight|workbox|office|monaco/.test(url),
    ),
  );
  assert.ok(
    !downloads.some((url) => /MarkdownPreview/.test(url)),
    "登录提前加载了 Markdown",
  );
  await page.getByRole("link", { name: "文本分享", exact: true }).click();
  await page.getByLabel("分享标题").fill("浏览器分享验证");
  await page
    .getByLabel("分享内容")
    .fill(
      "# 临时笔记\n\n把需要分享的内容留在这里。\n\n- 支持文本与 Markdown\n- 为链接设置有效期\n- 添加访问密码，按需分享",
    );
  await page.screenshot({
    path: resolve(root, "backend/bin/mint-home-light.png"),
    fullPage: true,
  });
  await page.getByRole("button", { name: "切换到暗色模式" }).click();
  assert.equal(
    await page.getByLabel("分享标题").inputValue(),
    "浏览器分享验证",
  );
  await page.screenshot({
    path: resolve(root, "backend/bin/mint-home-dark.png"),
    fullPage: true,
  });
  await page.getByRole("button", { name: "预览", exact: true }).click();
  await page.getByRole("heading", { name: "临时笔记", exact: true }).waitFor();
  await page.getByRole("button", { name: "编辑", exact: true }).click();
  assert.ok(
    (await page.getByLabel("分享内容").inputValue()).includes("临时笔记"),
  );
  await page.reload();
  assert.equal(await page.locator("html").getAttribute("data-theme"), "dark");
  // 刷新不保存草稿，重新填写用于后续真实分享验证。
  await page.getByLabel("分享标题").fill("浏览器分享验证");
  await page
    .getByLabel("分享内容")
    .fill(
      '# 本站预览\n中文内容\n<img src="https://tracker.example/test" onerror="window.compromised=true">\n\n' +
        backgroundHTML,
    );
  await page.getByLabel(/自定义链接名称/).fill("../invalid");
  assert.equal(
    await page
      .getByLabel(/自定义链接名称/)
      .evaluate((input) => input.checkValidity()),
    false,
  );
  await page.getByLabel(/自定义链接名称/).fill("mint-text-demo");
  // 查看上限无需展开；分钟有效期写入真实记录，防止只改了下拉文案。
  assert.ok(await page.getByLabel(/最多查看次数/).isVisible());
  await page.getByLabel(/最多查看次数/).fill("20");
  await page
    .getByLabel("有效期", { exact: true })
    .selectOption({ label: "5 分钟" });
  const textCreated = page.waitForResponse(
    (response) =>
      response.url().endsWith("/api/admin/paste") &&
      response.request().method() === "POST",
  );
  await page.getByRole("button", { name: "创建分享", exact: true }).click();
  const textRecord = (await (await textCreated).json()).data;
  assert.equal(textRecord.max_views, 20);
  assert.ok(
    Math.abs(Date.parse(textRecord.expires_at) - Date.now() - 300000) < 10000,
  );
  const sharedText = page.getByLabel("分享链接");
  await sharedText.waitFor();
  const textURL = await sharedText.inputValue();
  assert.ok(textURL.endsWith("/paste/mint-text-demo"));
  await page.goto(origin + "/");
  await page.getByLabel("分享内容").fill("不能覆盖原分享");
  await page.getByLabel(/自定义链接名称/).fill("mint-text-demo");
  await page.getByRole("button", { name: "创建分享", exact: true }).click();
  await page.getByText("链接后缀已被占用", { exact: true }).waitFor();
  await page.goto(textURL);
  await page.getByRole("heading", { name: "本站预览" }).waitFor();
  assert.equal(await page.evaluate(() => window.compromised), undefined);
  assert.equal(await page.locator(".markdown-body [background]").count(), 0);
  assert.equal(await page.locator(".markdown-body td").textContent(), "正文");
  assert.deepEqual(external, []);
  await page.goto(origin + "/upload");
  await page.locator('input[type="file"]').setInputFiles({
    name: "中文预览.txt",
    mimeType: "text/plain",
    buffer: Buffer.from("中文文件内容"),
  });
  await page.getByLabel(/自定义链接名称/).fill("mint-file-demo");
  assert.ok(await page.getByLabel(/最多查看次数/).isVisible());
  await page.getByLabel(/最多查看次数/).fill("20");
  await page
    .getByLabel("有效期", { exact: true })
    .selectOption({ label: "10 分钟" });
  const fileCreated = page.waitForResponse(
    (response) =>
      response.url().endsWith("/api/admin/share/upload") &&
      response.request().method() === "PUT" &&
      response.ok(),
  );
  await page.getByRole("button", { name: "上传并创建分享" }).click();
  const fileRecord = (await (await fileCreated).json()).data;
  assert.equal(fileRecord.max_views, 20);
  assert.ok(
    Math.abs(Date.parse(fileRecord.expires_at) - Date.now() - 600000) < 10000,
  );
  await page.getByRole("heading", { name: "分享链接", exact: true }).waitFor();
  const fileURL = await page.locator(".share-result input").inputValue();
  assert.ok(fileURL.endsWith("/file/mint-file-demo"));
  await page.goto(fileURL);
  await page
    .locator(".plain-preview")
    .filter({ hasText: "中文文件内容" })
    .waitFor();
  const download = await page
    .getByRole("link", { name: "下载文件" })
    .getAttribute("href");
  const range = await fetch(download, { headers: { Range: "bytes=0-2" } });
  assert.equal(range.status, 206);
  assert.equal((await range.arrayBuffer()).byteLength, 3);
  await page.goto(origin + "/admin/file-management");
  await page.getByRole("button", { name: "编辑", exact: true }).click();
  await page.getByLabel("文件名称", { exact: true }).fill("新的名称.txt");
  await page.getByRole("button", { name: "保存", exact: true }).click();
  await page.getByText("新的名称.txt", { exact: true }).waitFor();
  await page.goto(origin + "/admin/settings/global");
  await page.getByLabel(/新分享默认有效期/).fill("2");
  await page.getByText("有未保存的修改", { exact: true }).waitFor();
  page.once("dialog", (dialog) => dialog.dismiss());
  await page.getByRole("link", { name: "文件预览", exact: true }).click();
  assert.ok(page.url().endsWith("/admin/settings/global"));
  await page.getByRole("button", { name: "撤销修改", exact: true }).click();
  assert.equal(await page.getByLabel(/新分享默认有效期/).inputValue(), "24");
  assert.ok(await page.getByRole("button", { name: "保存设置" }).isDisabled());
  // 保存失败保留草稿，固定安全项不进入可编辑表单和提交体。
  await page.route("**/api/admin/settings/group/1", (route) =>
    route.fulfill({
      status: 500,
      contentType: "application/json",
      body: JSON.stringify({ success: false, message: "测试保存失败" }),
    }),
  );
  await page.getByLabel(/新分享默认有效期/).fill("3");
  await page.getByRole("button", { name: "保存设置" }).click();
  await page.getByText("测试保存失败", { exact: true }).waitFor();
  assert.equal(await page.getByLabel(/新分享默认有效期/).inputValue(), "3");
  await page.unroute("**/api/admin/settings/group/1");
  await page.getByLabel(/新分享默认有效期/).fill("1");
  const settingsPut = page.waitForRequest(
    (request) =>
      request.method() === "PUT" &&
      request.url().endsWith("/api/admin/settings/group/1"),
  );
  await page.getByRole("button", { name: "保存设置" }).click();
  assert.deepEqual(Object.keys((await settingsPut).postDataJSON()).sort(), [
    "max_upload_size",
    "share_default_expiry_hours",
  ]);
  await page.getByText("设置已保存", { exact: true }).waitFor();
  await page.screenshot({
    path: resolve(root, "backend/bin/settings-global-dark.png"),
    fullPage: true,
  });
  await page.getByRole("button", { name: "切换到浅色模式" }).click();
  await page.screenshot({
    path: resolve(root, "backend/bin/settings-global-light.png"),
    fullPage: true,
  });
  await page.getByRole("button", { name: "切换到暗色模式" }).click();
  await page.getByRole("link", { name: "文件预览", exact: true }).click();
  await page.getByLabel(/文本文件预览上限/).fill("64");
  await page.getByRole("switch", { name: "允许站外图片", exact: true }).check();
  await page.getByRole("button", { name: "保存设置" }).click();
  await page.getByText("设置已保存", { exact: true }).waitFor();
  await page.reload();
  assert.equal(await page.getByLabel(/文本文件预览上限/).inputValue(), "64");
  assert.ok(
    await page
      .getByRole("switch", { name: "允许站外图片", exact: true })
      .isChecked(),
  );
  await page
    .getByRole("switch", { name: "允许站外图片", exact: true })
    .uncheck();
  await page.getByRole("button", { name: "保存设置" }).click();
  await page.getByText("设置已保存", { exact: true }).waitFor();
  await page.getByRole("link", { name: "站点外观", exact: true }).click();
  assert.equal(await page.locator("details").getAttribute("open"), null);
  await page.locator("summary").click();
  await page
    .getByLabel("页面顶部 HTML", { exact: true })
    .fill("<strong>本地测试说明</strong>" + backgroundHTML);
  await page.getByLabel(/^站点标题/).fill("临时分享站");
  await page.getByRole("button", { name: "保存设置" }).click();
  await page.getByText("设置已保存", { exact: true }).waitFor();
  await page.reload();
  await page.locator("summary").click();
  assert.equal(
    await page.getByLabel("页面顶部 HTML", { exact: true }).inputValue(),
    "<strong>本地测试说明</strong>" + backgroundHTML,
  );
  assert.equal(await page.locator(".custom-content [background]").count(), 0);
  assert.equal(await page.locator(".custom-content td").textContent(), "正文");
  assert.deepEqual(external, []);
  await page.getByLabel("页面顶部 HTML", { exact: true }).fill("");
  await page.getByRole("button", { name: "保存设置" }).click();
  await page.getByText("设置已保存", { exact: true }).waitFor();
  await page.locator("summary").click();
  for (const width of [320, 390, 768, 1440]) {
    await page.setViewportSize({ width, height: 900 });
    assert.ok(
      await page.evaluate(
        () => document.documentElement.scrollWidth <= innerWidth,
      ),
      `设置页溢出: ${width}px`,
    );
  }
  await page.setViewportSize({ width: 390, height: 844 });
  await page.screenshot({
    path: resolve(root, "backend/bin/settings-mobile-dark.png"),
    fullPage: true,
  });
  await page.setViewportSize({ width: 1440, height: 1000 });
  await page.goto(origin + "/admin/key-management");
  await page.getByRole("button", { name: "新增密钥", exact: true }).click();
  await page.getByLabel("名称", { exact: true }).fill("UI 创建密钥");
  await page.getByLabel("创建文本分享", { exact: true }).check();
  await page.getByLabel("上传并创建文件分享", { exact: true }).check();
  await page.getByRole("button", { name: "保存", exact: true }).click();
  await page
    .getByRole("heading", { name: "保存新密钥", exact: true })
    .waitFor();
  // 自动生成和自定义共存，重复密钥由服务端拒绝，不会覆盖已有权限。
  const customKey = "mint-custom-test-key-2026-abcdef";
  await page.getByRole("button", { name: "新增密钥", exact: true }).click();
  await page.getByLabel("名称", { exact: true }).fill("自定义测试密钥");
  const keyInput = page.getByLabel(/自定义密钥/);
  await keyInput.fill("too-short");
  assert.equal(
    await keyInput.evaluate((input) => input.checkValidity()),
    true,
  );
  await keyInput.fill(customKey);
  await page.getByLabel("创建文本分享", { exact: true }).check();
  await page.getByRole("button", { name: "保存", exact: true }).click();
  await page
    .getByRole("heading", { name: "保存新密钥", exact: true })
    .waitFor();
  assert.equal(await page.locator(".result input").inputValue(), customKey);
  const keyContext = await browser.newContext();
  const keyPage = await keyContext.newPage();
  await keyPage.goto(origin + "/login");
  await keyPage.getByLabel("API Key", { exact: true }).fill(customKey);
  await keyPage.getByRole("button", { name: "登录", exact: true }).click();
  await keyPage.waitForURL(origin + "/");
  await keyContext.close();
  await page.getByRole("button", { name: "新增密钥", exact: true }).click();
  await page.getByLabel("名称", { exact: true }).fill("重复密钥");
  await keyInput.fill(customKey);
  await page.getByRole("button", { name: "保存", exact: true }).click();
  await page.getByText("密钥不可用", { exact: true }).waitFor();
  await page.getByRole("button", { name: "取消", exact: true }).click();
  await page
    .getByRole("row")
    .filter({ hasText: "自定义测试密钥" })
    .getByRole("button", { name: "编辑", exact: true })
    .click();
  assert.equal(await page.getByLabel(/自定义密钥/).count(), 0);
  await page.getByRole("button", { name: "取消", exact: true }).click();
  await page.goto(origin + "/admin/text-management");
  await page.getByRole("button", { name: "编辑", exact: true }).click();
  await page.getByLabel("标题", { exact: true }).fill("已编辑文本");
  await page.getByRole("button", { name: "保存", exact: true }).click();
  await page.getByText("已编辑文本", { exact: true }).waitFor();
  await page.screenshot({
    path: resolve(root, "backend/bin/mint-admin-dark.png"),
    fullPage: true,
  });
  await page.getByRole("button", { name: "切换到浅色模式" }).click();
  await page.screenshot({
    path: resolve(root, "backend/bin/mint-admin-light.png"),
    fullPage: true,
  });
  await page.goto(origin + "/admin/storage");
  await page.getByRole("button", { name: "测试", exact: true }).click();
  await page.getByText("连接测试通过", { exact: true }).waitFor();
  await page.getByRole("button", { name: "设为默认", exact: true }).click();
  await page.getByText("默认存储已更新", { exact: true }).waitFor();

  // 真实小文件验证浏览器原生解码，媒体预览不加载外部播放器。
  const png = Buffer.from(
    "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+aD1sAAAAASUVORK5CYII=",
    "base64",
  );
  const wav = Buffer.alloc(8044, 128);
  wav.write("RIFF", 0);
  wav.writeUInt32LE(8036, 4);
  wav.write("WAVEfmt ", 8);
  wav.writeUInt32LE(16, 16);
  wav.writeUInt16LE(1, 20);
  wav.writeUInt16LE(1, 22);
  wav.writeUInt32LE(8000, 24);
  wav.writeUInt32LE(8000, 28);
  wav.writeUInt16LE(1, 32);
  wav.writeUInt16LE(8, 34);
  wav.write("data", 36);
  wav.writeUInt32LE(8000, 40);
  // 固定的 32×32 VP8 视频已独立验证可解码，避免无头录制偶发生成只有容器头的文件。
  const webm = await readFile(
    resolve(root, "frontend/tests/fixtures/preview.webm"),
  );
  console.log("浏览器回归：安全与管理检查通过，验证媒体预览");
  for (const [name, type, bytes, selector] of [
    ["picture.png", "image/png", png, "img.image-preview"],
    ["sound.wav", "audio/wav", wav, "audio"],
    ["movie.webm", "video/webm", webm, "video"],
  ]) {
    const response = await fetch(backend + "/api/admin/share/upload", {
      method: "PUT",
      headers: {
        Authorization: admin,
        "Content-Type": type,
        "X-Share-Filename": name,
        "X-Share-Options": Buffer.from(
          JSON.stringify({ storage_config_id: storage.id }),
        ).toString("base64"),
      },
      body: bytes,
    });
    const result = await response.json();
    assert.ok(response.ok, result.message);
    await page.goto(origin + "/file/" + result.data.slug);
    await page.locator(selector).waitFor();
    if (selector.startsWith("img"))
      await page.waitForFunction(
        () => document.querySelector("img.image-preview")?.naturalWidth === 1,
      );
    else {
      await page.locator(selector).evaluate((media) => media.load());
      await page.waitForFunction(
        (selector) => document.querySelector(selector)?.readyState >= 1,
        selector,
      );
      if (selector === "video") {
        await page.waitForFunction(() => {
          const video = document.querySelector("video");
          return (
            video.readyState >= 2 &&
            video.videoWidth === 32 &&
            video.videoHeight === 32
          );
        });
      }
    }
  }
  await page.goto(origin + "/admin/account");
  await page.getByLabel("当前密码", { exact: true }).fill(password);
  await page.getByLabel("新密码", { exact: true }).fill(password + "-changed");
  await page
    .getByRole("button", { name: "更新账号并重新登录", exact: true })
    .click();
  await page.waitForURL("**/admin/login");
  await page.getByLabel("账号", { exact: true }).fill("admin");
  await page.getByLabel("密码", { exact: true }).fill(password + "-changed");
  await page.getByRole("button", { name: "登录", exact: true }).click();
  await page.waitForURL("**/admin/text-management");
  const uploaded = await readdir(filesDirectory);
  assert.equal(uploaded.length, 4);
  await page.getByRole("button", { name: "退出", exact: true }).click();
  // 退出包含异步撤销请求，必须等待会话清除及跳转完成再进入普通用户测试。
  await page.waitForURL(origin + "/");
  await page.getByRole("link", { name: "登录", exact: true }).waitFor();
  const userRequests = [];
  page.on("request", (request) => userRequests.push(request.url()));
  await page.goto(origin + "/manage/text-management");
  await page.waitForURL("**/login?redirect=**");
  assert.equal(await page.getByLabel("账号", { exact: true }).count(), 0);
  await page.getByLabel("API Key", { exact: true }).fill(user.key);
  await page.getByRole("button", { name: "登录", exact: true }).click();
  await page.waitForURL("**/manage/text-management");
  // URL 先于异步页面渲染更新，等管理页出现后再检查入口隔离。
  await page.getByRole("heading", { name: "文本管理", exact: true }).waitFor();
  assert.equal(await page.locator('a[href^="/admin"]').count(), 0);
  await page.reload();
  await page.getByRole("heading", { name: "文本管理", exact: true }).waitFor();
  assert.ok(
    !userRequests.some((url) =>
      new URL(url).pathname.startsWith("/api/admin/"),
    ),
    "普通用户调用了管理员接口：" +
      userRequests
        .filter((url) => new URL(url).pathname.startsWith("/api/admin/"))
        .map((url) => new URL(url).pathname)
        .join(", "),
  );
  await page.goto(origin + "/admin/storage");
  await page.waitForURL("**/manage/text-management");
  await page.goto(origin + "/admin/settings/global");
  await page.waitForURL("**/manage/text-management");
  assert.equal(
    await page.getByRole("link", { name: "密钥与权限", exact: true }).count(),
    0,
  );
  await page.goto(origin + "/upload");
  await page
    .getByRole("option", { name: /本地测试存储/ })
    .waitFor({ state: "attached" });
  await page.goto(origin + "/");
  await page.getByLabel("分享内容").waitFor();
  // 小屏可操作性检查：导航可见且页面不产生整体横向滚动。
  for (const width of [320, 390, 768, 1440]) {
    await page.setViewportSize({ width, height: 900 });
    for (const path of ["/", "/upload", "/manage/text-management"]) {
      await page.goto(origin + path);
      await page.locator(".panel").first().waitFor();
      assert.ok(
        await page.evaluate(
          () => document.documentElement.scrollWidth <= innerWidth,
        ),
        `页面溢出: ${path}, ${width}px`,
      );
    }
  }
  await page.setViewportSize({ width: 390, height: 844 });
  await page.goto(origin + "/upload");
  await page.locator('input[type="file"]').setInputFiles({
    name: "旅行笔记.txt",
    mimeType: "text/plain",
    buffer: Buffer.from("旅途中的一些记录"),
  });
  assert.ok(await page.getByLabel("访问密码", { exact: true }).isVisible());
  await page.getByLabel("访问密码", { exact: true }).fill("temporary-password");
  await page.getByLabel("备注", { exact: true }).fill("测试备注");
  assert.ok(await page.getByLabel("备注", { exact: true }).isVisible());
  assert.equal(
    await page.getByLabel("访问密码", { exact: true }).inputValue(),
    "temporary-password",
  );
  assert.ok(await page.getByLabel("访问密码", { exact: true }).isVisible());
  await page.screenshot({
    path: resolve(root, "backend/bin/mint-mobile-light.png"),
    fullPage: true,
  });
  await page.getByRole("button", { name: "切换到暗色模式" }).click();
  await page.screenshot({
    path: resolve(root, "backend/bin/mint-mobile-dark.png"),
    fullPage: true,
  });
  // 禁用本地存储时仍可加载并切换主题，不中断登录或分享页面。
  const privateContext = await browser.newContext({ colorScheme: "dark" });
  await privateContext.addInitScript(() => {
    Object.defineProperty(window, "localStorage", {
      get() {
        throw new DOMException("Storage blocked", "SecurityError");
      },
    });
  });
  const privatePage = await privateContext.newPage();
  privatePage.on("pageerror", (error) => errors.push(error.message));
  await privatePage.goto(origin + "/admin/login");
  assert.equal(
    await privatePage.locator("html").getAttribute("data-theme"),
    "dark",
  );
  await privatePage.getByRole("button", { name: "切换到浅色模式" }).click();
  assert.equal(
    await privatePage.locator("html").getAttribute("data-theme"),
    "light",
  );
  await privateContext.close();
  // API Key 在普通接口中批量创建自定义链接；同批重名在上传前拦截。
  await page.locator('input[type="file"]').setInputFiles([
    {
      name: "batch-one.txt",
      mimeType: "text/plain",
      buffer: Buffer.from("one"),
    },
    {
      name: "batch-two.txt",
      mimeType: "text/plain",
      buffer: Buffer.from("two"),
    },
  ]);
  await page
    .getByLabel(/自定义链接名称/)
    .nth(0)
    .fill("batch-one");
  await page
    .getByLabel(/自定义链接名称/)
    .nth(1)
    .fill("batch-one");
  const uploadsBefore = userRequests.filter((url) =>
    url.endsWith("/api/share/upload"),
  ).length;
  await page.getByRole("button", { name: "上传并创建分享" }).click();
  await page
    .getByText("多个文件不能使用相同的链接名称，请分别填写或留空自动生成。", {
      exact: true,
    })
    .waitFor();
  assert.equal(
    userRequests.filter((url) => url.endsWith("/api/share/upload")).length,
    uploadsBefore,
  );
  // 后一个文件使用已占用名称时，保留前一个文件的成功链接，修改后可单独重试。
  await page
    .getByLabel(/自定义链接名称/)
    .nth(1)
    .fill("mint-file-demo");
  await page.getByRole("button", { name: "上传并创建分享" }).click();
  await page.getByText("链接后缀已被占用", { exact: true }).waitFor();
  assert.equal(await page.locator(".share-result input").count(), 1);
  assert.equal(await page.getByLabel(/自定义链接名称/).count(), 1);
  await page.getByLabel(/自定义链接名称/).fill("batch-two");
  await page.getByRole("button", { name: "上传并创建分享" }).click();
  await page.waitForFunction(
    () => document.querySelectorAll(".share-result input").length === 2,
  );
  assert.deepEqual(
    await page
      .locator(".share-result input")
      .evaluateAll((inputs) =>
        inputs.map((input) => new URL(input.value).pathname),
      ),
    ["/file/batch-one", "/file/batch-two"],
  );
  assert.ok(
    !userRequests.some((url) =>
      new URL(url).pathname.startsWith("/api/admin/"),
    ),
    "普通用户操作调用了管理员接口",
  );
  await page.setViewportSize({ width: 1440, height: 1000 });
  await page.goto(origin + "/");
  await page.screenshot({
    path: resolve(root, "backend/bin/sharing-home.png"),
    fullPage: true,
  });
  assert.deepEqual(external, []);
  assert.deepEqual(errors, []);
  assert.equal(
    await page.evaluate(
      async () => (await navigator.serviceWorker.getRegistrations()).length,
    ),
    0,
  );
  console.log(
    JSON.stringify({
      result: "passed",
      deployment: standalone ? "内嵌前端与 API" : "分离前端与 API",
      login: "自动跳转",
      upload: "中文原始上传、Range 与文本预览",
      administration: "文本、文件、设置和密钥",
      security: "XSS 与角色隔离",
      externalRequests: external.length,
      loginResourceRequests,
      theme: "跟随系统、切换记忆、存储禁用降级及四种屏宽",
    }),
  );
} catch (error) {
  // 先输出原始失败，浏览器清理异常不能掩盖具体失败用例。
  console.error("浏览器回归失败：", error);
  throw error;
} finally {
  if (service && service.exitCode === null) {
    service.kill();
    await new Promise((done) => {
      service.once("exit", done);
      setTimeout(done, 3000);
    });
  }
  // 后端先退出；浏览器异常时限定清理时间，避免 CI 永久等待关闭事件。
  const cleanupTimeout = setTimeout(() => {
    console.error("浏览器清理超过 10 秒，终止本次测试");
    process.exit(1);
  }, 10000);
  try {
    if (frontend.listening) {
      frontend.closeAllConnections();
      await new Promise((done) => frontend.close(done));
    }
    if (browser) await browser.close();
  } finally {
    clearTimeout(cleanupTimeout);
  }
  // 仅清理本脚本创建且位于测试目录内的临时数据。
  if (temporary.startsWith(resolve(root, "backend/bin/sharing-test-")))
    await rm(temporary, { recursive: true, force: true });
}
