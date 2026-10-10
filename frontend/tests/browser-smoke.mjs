import assert from "node:assert/strict";
import { createServer } from "node:http";
import { mkdtemp, mkdir, readFile, readdir, rm } from "node:fs/promises";
import { existsSync } from "node:fs";
import { spawn } from "node:child_process";
import { resolve, extname } from "node:path";
import { fileURLToPath } from "node:url";
import { chromium } from "playwright";

const root = fileURLToPath(new URL("../../", import.meta.url));
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
  await new Promise((done) => frontend.listen(0, "127.0.0.1", done));
  const origin = `http://127.0.0.1:${frontend.address().port}`;
  const installed = [
    process.env.SHARING_TEST_BROWSER,
    "C:/Program Files/Google/Chrome/Application/chrome.exe",
    "C:/Program Files (x86)/Microsoft/Edge/Application/msedge.exe",
  ].find((path) => path && existsSync(path));
  browser = await chromium.launch({
    headless: true,
    ...(installed ? { executablePath: installed } : {}),
  });
  const context = await browser.newContext();
  context.setDefaultTimeout(15000);
  context.setDefaultNavigationTimeout(15000);
  const page = await context.newPage();
  const external = [];
  const downloads = [];
  const errors = [];
  page.on("request", (request) => {
    downloads.push(request.url());
    if (!request.url().startsWith(origin)) external.push(request.url());
  });
  page.on("pageerror", (error) => errors.push(error.message));
  await page.goto(origin + "/admin/login");
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
      '# 本站预览\n中文内容\n<img src="https://tracker.example/test" onerror="window.compromised=true">',
    );
  await page.getByRole("button", { name: "创建分享", exact: true }).click();
  const sharedText = page.getByLabel("分享链接");
  await sharedText.waitFor();
  const textURL = await sharedText.inputValue();
  await page.goto(textURL);
  await page.getByRole("heading", { name: "本站预览" }).waitFor();
  assert.equal(await page.evaluate(() => window.compromised), undefined);
  await page.goto(origin + "/upload");
  await page.locator('input[type="file"]').setInputFiles({
    name: "中文预览.txt",
    mimeType: "text/plain",
    buffer: Buffer.from("中文文件内容"),
  });
  await page.getByRole("button", { name: "上传并创建分享" }).click();
  await page.getByRole("heading", { name: "分享链接", exact: true }).waitFor();
  const fileURL = await page.locator(".share-result input").inputValue();
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
  await page.getByLabel(/新分享默认有效期/).fill("1");
  await page.getByRole("button", { name: "保存设置" }).click();
  await page.getByText("设置已保存", { exact: true }).waitFor();
  await page.goto(origin + "/admin/settings/preview");
  await page.getByLabel(/文本文件预览上限/).fill("64");
  await page.getByRole("button", { name: "保存设置" }).click();
  await page.getByText("设置已保存", { exact: true }).waitFor();
  await page.goto(origin + "/admin/settings/site");
  await page.getByLabel(/^站点标题/).fill("临时分享站");
  await page.getByRole("button", { name: "保存设置" }).click();
  await page.getByText("设置已保存", { exact: true }).waitFor();
  await page.goto(origin + "/admin/key-management");
  await page.getByRole("button", { name: "新增密钥", exact: true }).click();
  await page.getByLabel("名称", { exact: true }).fill("UI 创建密钥");
  await page.getByLabel("创建文本分享", { exact: true }).check();
  await page.getByLabel("上传并创建文件分享", { exact: true }).check();
  await page.getByRole("button", { name: "保存", exact: true }).click();
  await page
    .getByRole("heading", { name: "保存新密钥", exact: true })
    .waitFor();
  await page.goto(origin + "/admin/text-management");
  await page.getByRole("button", { name: "编辑", exact: true }).click();
  await page.getByLabel("标题", { exact: true }).fill("已编辑文本");
  await page.getByRole("button", { name: "保存", exact: true }).click();
  await page.getByText("已编辑文本", { exact: true }).waitFor();
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
  const webm = Buffer.from(
    await page.evaluate(async () => {
      const canvas = document.createElement("canvas");
      canvas.width = 32;
      canvas.height = 32;
      document.body.append(canvas);
      const stream = canvas.captureStream(0);
      const recorder = new MediaRecorder(stream, {
        mimeType: "video/webm;codecs=vp8",
      });
      const chunks = [];
      recorder.ondataavailable = (event) => chunks.push(event.data);
      const stopped = new Promise((done) => (recorder.onstop = done));
      recorder.start();
      const interval = setInterval(() => {
        const context = canvas.getContext("2d");
        context.fillStyle = Math.random() > 0.5 ? "#126954" : "#ffffff";
        context.fillRect(0, 0, 32, 32);
        stream.getVideoTracks()[0].requestFrame();
      }, 100);
      await new Promise((done) => setTimeout(done, 600));
      recorder.stop();
      await stopped;
      clearInterval(interval);
      canvas.remove();
      stream.getTracks().forEach((track) => track.stop());
      return Array.from(new Uint8Array(await new Blob(chunks).arrayBuffer()));
    }),
  );
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
  await page.goto(origin + "/admin/login");
  await page.getByRole("button", { name: "API Key", exact: true }).click();
  await page.getByLabel("API Key", { exact: true }).fill(user.key);
  await page.getByRole("button", { name: "登录", exact: true }).click();
  await page.waitForURL("**/admin/text-management");
  await page.goto(origin + "/admin/storage");
  await page.waitForURL("**/admin/text-management");
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
      login: "自动跳转",
      upload: "中文原始上传、Range 与文本预览",
      administration: "文本、文件、设置和密钥",
      security: "XSS 与角色隔离",
      externalRequests: external.length,
      loginResourceRequests,
    }),
  );
} finally {
  if (browser) await browser.close();
  if (service && service.exitCode === null) {
    service.kill();
    await new Promise((done) => {
      service.once("exit", done);
      setTimeout(done, 3000);
    });
  }
  await new Promise((done) => frontend.close(done));
  // 仅清理本脚本创建且位于测试目录内的临时数据。
  if (temporary.startsWith(resolve(root, "backend/bin/sharing-test-")))
    await rm(temporary, { recursive: true, force: true });
}
