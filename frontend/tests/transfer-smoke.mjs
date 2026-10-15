import assert from "node:assert/strict";
import { createServer } from "node:http";
import { mkdtemp, mkdir, readFile, writeFile, rm } from "node:fs/promises";
import { resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { spawn } from "node:child_process";
import { once } from "node:events";
import { createHash } from "node:crypto";
import { existsSync } from "node:fs";
import { chromium } from "playwright";

// 两个独立浏览器身份和临时数据库，测试不会连接现有用户服务。
const root = fileURLToPath(new URL("../../", import.meta.url));
const binary =
  process.env.TRANSFER_TEST_BINARY ||
  resolve(root, "backend/bin/transfer-test.exe");
const temp = await mkdtemp(resolve(root, "backend/bin/transfer-e2e-"));
const reservation = createServer();
await new Promise((done) => reservation.listen(0, "127.0.0.1", done));
const port = reservation.address().port;
await new Promise((done) => reservation.close(done));
const base = `http://127.0.0.1:${port}`;
const password = "isolated-transfer-test-password";
// 可选同机基准，只比较同一环境前后版本；包含浏览器点击和完成状态的开销。
const benchmarkBytes =
  Math.min(128, Math.max(0, Number(process.env.TRANSFER_BENCHMARK_MB) || 0)) *
  1024 *
  1024;
let service, browser, a, b;
let logs = "";
const errors = [];
try {
  service = spawn(binary, [], {
    env: {
      ...process.env,
      PORT: String(port),
      DATA_DIR: resolve(temp, "data"),
      ADMIN_USERNAME: "admin",
      ADMIN_PASSWORD: password,
      TRANSFER_RATE_MB: process.env.TRANSFER_RATE_MB || "64",
    },
    windowsHide: true,
    stdio: ["ignore", "pipe", "pipe"],
  });
  service.stderr.on("data", (data) => (logs += data));
  service.stdout.on("data", (data) => (logs += data));
  let ready = false;
  for (let i = 0; i < 100; i++) {
    try {
      ready = (await fetch(base + "/api/health")).ok;
    } catch {}
    if (ready) break;
    await new Promise((done) => setTimeout(done, 100));
  }
  assert.ok(ready, logs);
  const response = await fetch(base + "/api/admin/login", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ username: "admin", password }),
  });
  const login = await response.json();
  assert.ok(login.success);
  const installed =
    process.platform === "win32" &&
    [
      "C:/Program Files/Google/Chrome/Application/chrome.exe",
      "C:/Program Files (x86)/Microsoft/Edge/Application/msedge.exe",
    ].find(existsSync);
  browser = await chromium.launch({
    headless: true,
    ...(installed ? { executablePath: installed } : {}),
  });
  const ac = await browser.newContext({
    viewport: { width: 1280, height: 900 },
    acceptDownloads: true,
  });
  const bc = await browser.newContext({
    viewport: { width: 390, height: 844 },
    acceptDownloads: true,
  });
  await ac.grantPermissions(["clipboard-read", "clipboard-write"]);
  await bc.grantPermissions(["clipboard-read", "clipboard-write"]);
  await ac.addInitScript(
    (token) =>
      localStorage.setItem(
        "cloudpaste-sharing-session",
        JSON.stringify({ type: "admin", token, name: "admin" }),
      ),
    login.data.token,
  );
  a = await ac.newPage();
  b = await bc.newPage();
  for (const page of [a, b])
    page.on("pageerror", (error) => errors.push(error.message));
  for (const page of [a, b])
    page.on("response", async (response) => {
      if (!/\/api\/(admin\/)?transfer\/create$/.test(response.url())) return;
      const contentType = response.headers()["content-type"];
      if (!contentType?.includes("application/json"))
        console.log(
          "Unexpected create response",
          response.status(),
          contentType,
          (await response.body().catch(() => Buffer.alloc(0))).length,
        );
    });
  await Promise.all([a.goto(base + "/transfer"), b.goto(base + "/transfer")]);
  await a
    .getByRole("textbox", { name: "设备名称", exact: true })
    .fill("发送设备");
  assert.equal(
    await a
      .getByRole("checkbox", { name: "自动同意设备加入", exact: true })
      .isChecked(),
    false,
  );
  await a
    .getByRole("checkbox", {
      name: "仅使用中继，不向对方交换直连网络地址",
      exact: true,
    })
    .check();
  await a.getByRole("button", { name: "创建配对码", exact: true }).click();
  await a.getByRole("heading", { name: "等待另一台设备" }).waitFor();
  const code = (await a.locator(".transfer-code").textContent())
    .replaceAll(" ", "")
    .trim();
  assert.match(code, /^[A-Z]-[0-9]{4}$/);
  await a.getByRole("button", { name: "复制配对码", exact: true }).click();
  const copiedCode = await a.evaluate(() => navigator.clipboard.readText());
  assert.equal(copiedCode, code.replace("-", ""));
  await b
    .getByRole("textbox", { name: "设备名称", exact: true })
    .fill("接收设备");
  const cells = b.locator(".pair-code input");
  assert.equal(await cells.count(), 5);
  assert.equal(await b.locator(".pair-code-separator").textContent(), "-");
  for (let index = 0; index < 5; index++) {
    assert.equal(await cells.nth(index).inputValue(), "");
    assert.equal(await cells.nth(index).getAttribute("placeholder"), null);
  }
  // 暂不填写字母，检查编辑行为时不能提前自动加入。
  for (let index = 1; index < 5; index++)
    await cells.nth(index).fill(copiedCode[index].toLowerCase());
  await b.waitForFunction(
    (code) =>
      Array.from(
        document.querySelectorAll(".pair-code input"),
        (input) => input.value,
      ).join("") === code,
    copiedCode.slice(1),
  );
  // 自动跳格、方向键和中间删除都要保留数字的位置，不能自动挤向前面。
  assert.equal(
    await cells.nth(4).evaluate((input) => input === document.activeElement),
    true,
  );
  await cells.nth(4).press("ArrowLeft");
  assert.equal(
    await cells.nth(3).evaluate((input) => input === document.activeElement),
    true,
  );
  await cells.nth(3).press("Backspace");
  assert.equal(await cells.nth(3).inputValue(), "");
  assert.equal(await cells.nth(4).inputValue(), copiedCode[4]);
  assert.equal(
    await b.getByRole("button", { name: "加入连接", exact: true }).isDisabled(),
    true,
  );
  await cells.nth(3).press("Backspace");
  assert.equal(await cells.nth(2).inputValue(), "");
  await cells.nth(2).fill(copiedCode[2]);
  await cells.nth(3).fill(copiedCode[3]);
  // 完整代码粘贴会自动提交；失效时清空五格并回到字母输入框。
  const invalidCode = (copiedCode[0] === "A" ? "B" : "A") + copiedCode.slice(1);
  await cells.nth(2).evaluate(
    (input, value) => {
      const clipboard = new DataTransfer();
      clipboard.setData("text/plain", value);
      input.dispatchEvent(
        new ClipboardEvent("paste", {
          clipboardData: clipboard,
          bubbles: true,
          cancelable: true,
        }),
      );
    },
    " " + invalidCode[0].toLowerCase() + "-" + invalidCode.slice(1) + " ",
  );
  await b
    .getByRole("alert")
    .filter({ hasText: "连接已失效或不可用" })
    .waitFor();
  await b.waitForFunction(() =>
    Array.from(document.querySelectorAll(".pair-code input")).every(
      (input) => input.value === "",
    ),
  );
  assert.equal(
    await cells.nth(0).evaluate((input) => input === document.activeElement),
    true,
  );
  await b.evaluate(
    (value) => navigator.clipboard.writeText(value),
    invalidCode,
  );
  const pastedResponse = b.waitForResponse((response) =>
    response.url().endsWith("/api/transfer/join"),
  );
  await b.getByRole("button", { name: "粘贴配对码", exact: true }).click();
  assert.equal((await pastedResponse).status(), 404);
  await b.waitForFunction(() =>
    Array.from(document.querySelectorAll(".pair-code input")).every(
      (input) => input.value === "",
    ),
  );
  await b.setViewportSize({ width: 320, height: 844 });
  assert.ok(
    await b.evaluate(() => document.documentElement.scrollWidth <= innerWidth),
    "分格配对码在窄屏溢出",
  );
  await b.screenshot({
    path: resolve(root, "backend/bin/pair-code-mobile.png"),
    fullPage: true,
  });
  await b.setViewportSize({ width: 390, height: 844 });
  for (let index = 0; index < 4; index++)
    await cells.nth(index).fill(copiedCode[index].toLowerCase());
  assert.equal(
    await b.getByRole("button", { name: "加入连接", exact: true }).isDisabled(),
    true,
  );
  let joinRequests = 0;
  const countJoin = (request) => {
    if (request.url().endsWith("/api/transfer/join")) joinRequests++;
  };
  b.on("request", countJoin);
  const joinedResponse = b.waitForResponse((response) =>
    response.url().endsWith("/api/transfer/join"),
  );
  // 最后一位触发加入，不需要再点按钮；同一次输入只发出一次请求。
  await cells.nth(4).fill(copiedCode[4]);
  assert.equal((await joinedResponse).status(), 200);
  await b.getByRole("heading", { name: "等待对方确认" }).waitFor();
  assert.equal(joinRequests, 1);
  b.off("request", countJoin);
  await a.getByRole("button", { name: "确认连接", exact: true }).click();
  await b.getByRole("heading", { name: "消息", exact: true }).waitFor();

  for (const [sender, receiver, content] of [
    [a, b, '<img src=x onerror="alert(1)"> 原样显示'],
    [b, a, "文字可以双向发送"],
  ]) {
    await sender
      .getByRole("textbox", { name: "发送文字", exact: true })
      .fill(content);
    await sender.getByRole("button", { name: "发送文字", exact: true }).click();
    await receiver
      .getByRole("log")
      .getByText(content, { exact: true })
      .waitFor();
    // 双方消息时间来自同一服务端通知，不受浏览器本地时钟差异影响。
    const sent = sender
      .locator(".transfer-message")
      .filter({ hasText: content })
      .locator("time");
    const received = receiver
      .locator(".transfer-message")
      .filter({ hasText: content })
      .locator("time");
    assert.equal(
      await sent.getAttribute("datetime"),
      await received.getAttribute("datetime"),
    );
    assert.match(await received.innerText(), /^\d{2}:\d{2}:\d{2}$/);
  }
  assert.equal(await b.locator(".transfer-message img").count(), 0);

  // 向真实输入框派发剪贴板图片，验证草稿不会提前发送、消息内预览和下载字节一致。
  async function pastePicture(page) {
    return page
      .getByRole("textbox", { name: "发送文字", exact: true })
      .evaluate(async (input) => {
        const canvas = document.createElement("canvas");
        canvas.width = canvas.height = 24;
        const context = canvas.getContext("2d");
        context.fillStyle = "#168477";
        context.fillRect(0, 0, 24, 24);
        const blob = await new Promise((resolve) =>
          canvas.toBlob(resolve, "image/png"),
        );
        const clipboardData = new DataTransfer();
        clipboardData.items.add(
          new File([blob], "clipboard.png", { type: "image/png" }),
        );
        input.dispatchEvent(
          new ClipboardEvent("paste", {
            bubbles: true,
            cancelable: true,
            clipboardData,
          }),
        );
        return Array.from(new Uint8Array(await blob.arrayBuffer()));
      });
  }
  const pastedBytes = await pastePicture(a);
  await a.locator(".transfer-pasted-images img").waitFor();
  assert.equal(await b.locator(".transfer-chat-image").count(), 0);
  await a
    .getByRole("button", { name: "移除待发送图片 1", exact: true })
    .click();
  assert.equal(await a.locator(".transfer-pasted-images img").count(), 0);
  assert.equal(await b.locator(".transfer-chat-image").count(), 0);
  await pastePicture(a);
  await a.getByRole("button", { name: "发送消息", exact: true }).click();
  await b.locator(".transfer-chat-image").waitFor();
  await b.waitForFunction(
    () => document.querySelector(".transfer-chat-image")?.naturalWidth === 24,
  );
  // 图片复用原始 Blob，保留右键保存；点击缩略图打开大图，不再额外显示下载按钮。
  const imageDownload = b.waitForEvent("download");
  // 测试使用原生下载验证原图字节，不通过 fetch 绕过页面的连接安全策略。
  await b.locator(".transfer-chat-image").evaluate((image) => {
    const link = document.createElement("a");
    link.href = image.src;
    link.download = "chat-preview.png";
    document.body.append(link);
    link.click();
    link.remove();
  });
  const imageSaved = await imageDownload;
  assert.equal(await imageSaved.failure(), null);
  assert.deepEqual(
    await readFile(await imageSaved.path()),
    Buffer.from(pastedBytes),
  );
  assert.equal(
    await b.getByRole("link", { name: "下载图片", exact: true }).count(),
    0,
  );
  await b.getByRole("button", { name: /^查看图片 / }).click();
  await b.getByRole("dialog", { name: "图片预览", exact: true }).waitFor();
  await b.getByRole("button", { name: "放大图片", exact: true }).click();
  await b
    .locator(".transfer-image-toolbar")
    .getByText("125%", { exact: true })
    .waitFor();
  await b.getByRole("button", { name: "缩小图片", exact: true }).click();
  await b
    .locator(".transfer-image-toolbar")
    .getByText("100%", { exact: true })
    .waitFor();
  await b.keyboard.press("Escape");
  await b
    .getByRole("dialog", { name: "图片预览", exact: true })
    .waitFor({ state: "hidden" });
  await pastePicture(b);
  await b.getByRole("button", { name: "发送消息", exact: true }).click();
  await a.waitForFunction(
    () =>
      document.querySelectorAll(".transfer-chat-image").length === 2 &&
      [...document.querySelectorAll(".transfer-chat-image")].every(
        (image) => image.naturalWidth === 24,
      ),
  );

  // 多图逐个接收；第五张到达时释放最早的预览，不无限累积 Blob。
  for (let i = 0; i < 3; i++) {
    await pastePicture(b);
    await b.waitForFunction(
      (count) =>
        document.querySelectorAll(".transfer-pasted-images img").length ===
        count,
      i + 1,
    );
  }
  await b.getByRole("button", { name: "发送消息", exact: true }).click();
  await a.getByText("图片预览已释放", { exact: true }).waitFor();
  await a.waitForFunction(
    () =>
      document.querySelectorAll(".transfer-chat-image").length === 4 &&
      [...document.querySelectorAll(".transfer-chat-image")].every(
        (image) => image.naturalWidth === 24,
      ),
  );

  async function relay(sender, receiver, name, payload) {
    const source = resolve(temp, name);
    await writeFile(source, payload);
    await sender
      .getByLabel("选择互传文件", { exact: true })
      .setInputFiles(source);
    await sender.getByRole("button", { name: /^发送所选文件/ }).click();
    const row = receiver
      .locator(".transfer-file")
      .filter({ has: receiver.getByText(name, { exact: true }) });
    const downloadEvent = receiver.waitForEvent("download");
    const started = performance.now();
    // 点击中继下载便由浏览器原生保存，无需再次点击下载链接。
    await row.getByRole("button", { name: "中继下载", exact: true }).click();
    const download = await downloadEvent;
    assert.equal(await download.failure(), null);
    if (benchmarkBytes && payload.length >= benchmarkBytes)
      console.log(
        `BENCH relay: ${(payload.length / 1048576).toFixed(1)} MiB, ${((performance.now() - started) / 1000).toFixed(2)} s, ${(payload.length / 1048576 / ((performance.now() - started) / 1000)).toFixed(1)} MiB/s`,
      );
    const saved = await readFile(await download.path());
    assert.equal(
      createHash("sha256").update(saved).digest("hex"),
      createHash("sha256").update(payload).digest("hex"),
    );
    await row.getByText("下载完成", { exact: true }).waitFor();
    await row.getByRole("button", { name: "结束传输", exact: true }).click();
    await row.getByText(/接收方已确认完成/).waitFor();
  }
  await relay(
    a,
    b,
    "双向文件-1.bin",
    Buffer.alloc(benchmarkBytes || 4 * 1024 * 1024, 87),
  );
  await relay(b, a, "双向文件-2.txt", Buffer.from("反向实时传输\n"));
  await relay(a, b, "空文件.txt", Buffer.alloc(0));

  // 中继先自动下载，短期链接仍支持外部工具按请求区间下载。
  const toolPayload = Buffer.alloc(
    process.env.TRANSFER_TEST_AB ? 32 * 1024 * 1024 : 2 * 1024 * 1024,
  );
  for (let i = 0; i < toolPayload.length; i++) toolPayload[i] = i % 251;
  await a.getByLabel("选择互传文件").setInputFiles({
    name: "download-tool.bin",
    mimeType: "application/octet-stream",
    buffer: toolPayload,
  });
  await a.getByRole("button", { name: /^发送所选文件/ }).click();
  const toolRow = b
    .locator(".transfer-file")
    .filter({ has: b.getByText("download-tool.bin", { exact: true }) });
  const toolSenderRow = a
    .locator(".transfer-file")
    .filter({ has: a.getByText("download-tool.bin", { exact: true }) });
  // 初始接收按传输方式分类，外部工具与浏览器下载共用中继入口。
  assert.equal(
    await toolRow
      .getByRole("button", { name: "下载工具", exact: true })
      .count(),
    0,
  );
  assert.equal(
    await toolRow
      .getByRole("button", { name: "拒绝接收", exact: true })
      .count(),
    1,
  );
  const toolDownloadEvent = b.waitForEvent("download");
  await toolRow.getByRole("button", { name: "中继下载", exact: true }).click();
  const toolDownload = await toolDownloadEvent;
  assert.equal(await toolDownload.failure(), null);
  assert.deepEqual(await readFile(await toolDownload.path()), toolPayload);
  await toolRow.getByText("下载完成", { exact: true }).waitFor();
  const linkInput = toolRow.getByRole("textbox", {
    name: "download-tool.bin 下载链接",
    exact: true,
  });
  await linkInput.waitFor();
  const toolURL = await linkInput.inputValue();
  assert.equal(
    await toolRow
      .getByRole("button", { name: "结束传输", exact: true })
      .isDisabled(),
    false,
  );
  assert.equal(
    await toolRow.getByRole("link", { name: "重新下载", exact: true }).count(),
    1,
  );
  await linkInput.focus();
  assert.equal(
    await linkInput.evaluate(
      (input) => input.selectionEnd - input.selectionStart,
    ),
    toolURL.length,
  );
  const screenshots = resolve(root, "backend/bin/transfer-screenshots");
  await mkdir(screenshots, { recursive: true });
  await b.screenshot({
    path: resolve(screenshots, "download-tool-390.png"),
    fullPage: true,
  });
  const metadata = await fetch(toolURL, { method: "HEAD" });
  assert.equal(metadata.status, 200);
  assert.equal(metadata.headers.get("accept-ranges"), "bytes");
  assert.equal(
    Number(metadata.headers.get("content-length")),
    toolPayload.length,
  );
  const ranges = [
    [0, 49999],
    [50000, 199999],
    [200000, toolPayload.length - 1],
  ];
  await Promise.all(
    ranges.map(async ([first, last]) => {
      const response = await fetch(toolURL, {
        headers: { Range: `bytes=${first}-${last}` },
      });
      assert.equal(response.status, 206);
      assert.deepEqual(
        Buffer.from(await response.arrayBuffer()),
        toolPayload.subarray(first, last + 1),
      );
    }),
  );
  await toolRow.getByText("下载完成", { exact: true }).waitFor();
  await toolSenderRow.getByText("下载完成", { exact: true }).waitFor();
  assert.equal(
    await toolRow.getByRole("link", { name: "重新下载", exact: true }).count(),
    1,
  );
  assert.equal(
    await toolRow
      .getByRole("button", { name: "结束传输", exact: true })
      .isDisabled(),
    false,
  );
  const repeated = await fetch(toolURL, {
    headers: { Range: "bytes=100-199" },
  });
  assert.equal(repeated.status, 206);
  assert.deepEqual(
    Buffer.from(await repeated.arrayBuffer()),
    toolPayload.subarray(100, 200),
  );
  // 已完成后的重下应重新显示进度；HTTP 元数据探测不会重置，累计安全额度仍生效。
  for (const row of [toolRow, toolSenderRow]) {
    await row.getByText("传输中", { exact: true }).waitFor();
    await row.locator("progress").evaluate((progress) => {
      if (progress.value !== 100)
        throw new Error("重复下载进度未归零后重新统计");
    });
  }
  assert.equal(
    await toolRow
      .getByRole("button", { name: "结束传输", exact: true })
      .isDisabled(),
    true,
  );
  const setToolRate = async (rate) => {
    const response = await fetch(base + "/api/admin/settings/group/3", {
      method: "PUT",
      headers: {
        "Content-Type": "application/json",
        Authorization: `Bearer ${login.data.token}`,
      },
      body: JSON.stringify({ transfer_rate_mb: rate }),
    });
    assert.equal(response.status, 200);
  };
  // 在隔离服务中暂时放慢速度，确保第二次下载的双方进度条和速率可观察。
  await setToolRate(process.env.TRANSFER_TEST_AB ? 8 : 1);
  const movingProgress = async () => {
    for (const page of [a, b]) {
      await page.waitForFunction(() => {
        const row = [...document.querySelectorAll(".transfer-file")].find(
          (row) =>
            row.querySelector("strong")?.textContent.trim() ===
            "download-tool.bin",
        );
        const progress = row?.querySelector("progress");
        return (
          progress &&
          progress.value > 100 &&
          progress.value < progress.max &&
          row.querySelector(".transfer-tag.active")?.textContent === "传输中"
        );
      });
    }
  };
  if (process.env.TRANSFER_TEST_AB) {
    // 仅在显式测试开关开启时调用已运行的 AB；目标是本次隔离测试目录中的已知文件。
    const savedName = `CloudPaste-AB-compat-${port}.bin`;
    let abSegments = 0;
    const countSegments = (request) => {
      if (
        /\/api\/transfer\/upload\/[^/]+\/[^/]+$/.test(
          new URL(request.url()).pathname,
        )
      )
        abSegments++;
    };
    a.on("request", countSegments);
    const abResponse = await fetch(
      "http://127.0.0.1:15151/start-headless-download",
      {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
          downloadSource: { link: toolURL },
          folder: temp.replaceAll("\\", "/"),
          name: savedName,
        }),
      },
    );
    assert.equal(abResponse.status, 200, await abResponse.text());
    await movingProgress();
    const savedPath = resolve(temp, savedName);
    const expectedHash = createHash("sha256").update(toolPayload).digest("hex");
    let savedBytes = 0,
      savedHash = "";
    // AB 会先创建甚至预分配最终文件；路径存在并不表示数据已经全部写入。
    for (let i = 0; i < 90; i++) {
      if (existsSync(savedPath)) {
        const data = await readFile(savedPath);
        savedBytes = data.length;
        savedHash = createHash("sha256").update(data).digest("hex");
        if (savedBytes === toolPayload.length && savedHash === expectedHash)
          break;
      }
      await new Promise((done) => setTimeout(done, 500));
    }
    assert.equal(
      savedHash,
      expectedHash,
      `AB 文件校验失败：${savedBytes}/${toolPayload.length} 字节，${abSegments} 次分段上传`,
    );
    a.off("request", countSegments);
    console.log(
      `PASS: 已运行的 AB Download Manager 真实下载与 SHA-256 校验，按需上传 ${abSegments} 个区间`,
    );
  } else {
    const whole = await fetch(toolURL);
    assert.equal(whole.status, 200);
    const saved = whole.arrayBuffer();
    await movingProgress();
    assert.deepEqual(Buffer.from(await saved), toolPayload);
  }
  for (const row of [toolRow, toolSenderRow]) {
    await row.getByText(/(?:KiB|MiB)\/s/).waitFor();
  }
  await toolRow.getByText("下载完成", { exact: true }).waitFor();
  await toolSenderRow.getByText("下载完成", { exact: true }).waitFor();
  await setToolRate(64);
  await toolRow.getByRole("button", { name: "结束传输", exact: true }).click();
  await toolRow.getByText(/接收方已确认完成/).waitFor();
  assert.equal((await fetch(toolURL, { method: "HEAD" })).status, 404);

  // 自动接收是本端显式同意；两份文件应依次下载，关闭后恢复原有手动接收。
  const autoReceive = b.getByRole("checkbox", {
    name: "自动接收对方文件",
    exact: true,
  });
  assert.equal(await autoReceive.isChecked(), false);
  await autoReceive.check();
  const automaticDownloads = [];
  let finishAutomatic;
  const allAutomatic = new Promise((resolve) => (finishAutomatic = resolve));
  const collectAutomatic = (download) => {
    automaticDownloads.push(download);
    if (automaticDownloads.length === 2) finishAutomatic();
  };
  b.on("download", collectAutomatic);
  await a.getByLabel("选择互传文件").setInputFiles([
    {
      name: "auto-one.txt",
      mimeType: "text/plain",
      buffer: Buffer.from("auto one"),
    },
    {
      name: "auto-two.txt",
      mimeType: "text/plain",
      buffer: Buffer.from("auto two"),
    },
  ]);
  await a.getByRole("button", { name: /^发送所选文件/ }).click();
  let automaticTimer;
  try {
    await Promise.race([
      allAutomatic,
      new Promise(
        (_, reject) =>
          (automaticTimer = setTimeout(
            () => reject(new Error("自动下载超时")),
            15000,
          )),
      ),
    ]);
  } finally {
    clearTimeout(automaticTimer);
  }
  b.off("download", collectAutomatic);
  for (const download of automaticDownloads) {
    assert.equal(await download.failure(), null);
    assert.equal(
      (await readFile(await download.path())).toString(),
      download.suggestedFilename() === "auto-one.txt" ? "auto one" : "auto two",
    );
  }
  await autoReceive.uncheck();

  // 模拟一次上传网络失败；发送方无需重新选择文件，接收方必须再次确认新任务。
  let failUpload = true;
  const failOnce = (route) => {
    if (failUpload) {
      failUpload = false;
      return route.abort("failed");
    }
    return route.continue();
  };
  await a.route("**/api/transfer/upload/*", failOnce);
  // 自动接收保留一次性中继流程，继续覆盖上传失败后的重发行为。
  await autoReceive.check();
  const failedDownload = b.waitForEvent("download");
  const retryPayload = Buffer.from("失败后重新发送，同一个本地文件");
  await a.getByLabel("选择互传文件").setInputFiles({
    name: "retry.txt",
    mimeType: "text/plain",
    buffer: retryPayload,
  });
  await a.getByRole("button", { name: /^发送所选文件/ }).click();
  const retrySender = a
    .locator(".transfer-file")
    .filter({ has: a.getByText("retry.txt", { exact: true }) });
  const retryReceiver = b
    .locator(".transfer-file")
    .filter({ has: b.getByText("retry.txt", { exact: true }) });
  assert.ok(await (await failedDownload).failure());
  await retrySender.getByText(/传输失败/).waitFor();
  await autoReceive.uncheck();
  assert.equal(
    await retryReceiver
      .getByRole("button", { name: "重新发送", exact: true })
      .count(),
    0,
  );
  await retrySender
    .getByRole("button", { name: "重新发送", exact: true })
    .click();
  await retryReceiver
    .nth(1)
    .getByText(/等待接收/)
    .waitFor();
  assert.equal(
    await retrySender
      .first()
      .getByRole("button", { name: "重新发送", exact: true })
      .count(),
    0,
  );
  const retriedDownload = b.waitForEvent("download");
  await retryReceiver
    .nth(1)
    .getByRole("button", { name: "中继下载", exact: true })
    .click();
  const receivedRetry = await retriedDownload;
  assert.equal(await receivedRetry.failure(), null);
  assert.deepEqual(await readFile(await receivedRetry.path()), retryPayload);
  await retrySender.nth(1).getByText("下载完成", { exact: true }).waitFor();
  await retryReceiver
    .nth(1)
    .getByRole("button", { name: "结束传输", exact: true })
    .click();
  await retryReceiver
    .nth(1)
    .getByText(/接收方已确认完成/)
    .waitFor();
  await a.unroute("**/api/transfer/upload/*", failOnce);

  // 重复选择追加，移除只影响待发送列表。
  await a.getByLabel("选择互传文件").setInputFiles({
    name: "one.txt",
    mimeType: "text/plain",
    buffer: Buffer.from("one"),
  });
  await a.getByLabel("选择互传文件").setInputFiles({
    name: "two.txt",
    mimeType: "text/plain",
    buffer: Buffer.from("two"),
  });
  assert.equal(await a.locator(".transfer-selected li").count(), 2);
  await a.getByRole("button", { name: "移除 one.txt", exact: true }).click();
  assert.equal(await a.locator(".transfer-selected li").count(), 1);
  // 隐藏原生输入，但保留按钮触发选择器与辅助技术名称。
  assert.equal(await a.getByLabel("选择互传文件").isVisible(), false);
  const chooserEvent = a.waitForEvent("filechooser");
  await a.getByRole("button", { name: "选择文件", exact: true }).click();
  await (
    await chooserEvent
  ).setFiles({
    name: "cancel-me.txt",
    mimeType: "text/plain",
    buffer: Buffer.from("cancel"),
  });
  await a.getByRole("button", { name: /^发送所选文件/ }).click();
  const cancelledRow = a
    .locator(".transfer-file")
    .filter({ has: a.getByText("cancel-me.txt", { exact: true }) });
  await cancelledRow.getByRole("button", { name: "取消", exact: true }).click();
  await cancelledRow.getByText(/已取消/).waitFor();
  await cancelledRow
    .getByRole("button", { name: "移除任务 cancel-me.txt", exact: true })
    .click();
  assert.equal(await cancelledRow.count(), 0);
  for (const page of [a, b])
    assert.ok(
      await page.evaluate(
        () => document.documentElement.scrollWidth <= innerWidth,
      ),
      "layout overflow",
    );
  await mkdir(resolve(root, "backend/bin/transfer-screenshots"), {
    recursive: true,
  });
  await a.screenshot({
    path: resolve(root, "backend/bin/transfer-screenshots/desktop.png"),
    fullPage: true,
  });
  await b.screenshot({
    path: resolve(root, "backend/bin/transfer-screenshots/compact.png"),
    fullPage: true,
  });
  await b.getByRole("button", { name: "断开连接", exact: true }).click();
  await a
    .getByText("连接已结束。未完成的文件需要重新发送。", { exact: true })
    .waitFor();

  // 只替换系统保存对话框；真实 WebRTC、背压确认与浏览器文件写入保持不变。
  // OPFS 是自动化测试的磁盘落点，生产界面仍使用用户选择的普通文件。
  const debugResponse = await fetch(base + "/api/admin/settings/group/3", {
    method: "PUT",
    headers: {
      Authorization: "Bearer " + login.data.token,
      "Content-Type": "application/json",
    },
    body: JSON.stringify({ transfer_debug_logs: true }),
  });
  assert.ok(debugResponse.ok);
  for (const page of [a, b]) {
    await page.evaluate(() => {
      // 记录真实浏览器输出，验证另一页面保存的开关在重新连接时读取。
      window.transferDebugLogs = [];
      for (const method of ["debug", "warn"]) {
        const original = console[method];
        console[method] = (prefix, event, details) => {
          if (prefix === "[CloudPaste P2P]")
            window.transferDebugLogs.push({
              event,
              details: structuredClone(details),
            });
          original(prefix, event, details);
        };
      }
      // 记录真实 DataChannel 的分块，确认双方协商实际启用了大块，而非只修改常量。
      window.directChunkSizes = [];
      const send = RTCDataChannel.prototype.send;
      RTCDataChannel.prototype.send = function (data) {
        if (typeof data !== "string")
          window.directChunkSizes.push(data.byteLength);
        return send.call(this, data);
      };
      window.showSaveFilePicker = async ({ suggestedName }) => {
        const dir = await navigator.storage.getDirectory();
        const handle = await dir.getFileHandle(suggestedName, { create: true });
        if (!suggestedName.startsWith("取消直连")) return handle;
        // 模拟正在等待写盘的分块，确保取消发生在传输中而非完成之后。
        return {
          async createWritable() {
            const writer = await handle.createWritable();
            let release;
            return {
              write: () =>
                new Promise((resolve) => {
                  release = resolve;
                }),
              abort: async () => {
                release?.();
                await writer.abort();
              },
              close: () => writer.close(),
            };
          },
        };
      };
    });
  }
  await a
    .getByRole("checkbox", {
      name: "仅使用中继，不向对方交换直连网络地址",
      exact: true,
    })
    .uncheck();
  await a
    .getByRole("checkbox", { name: "自动同意设备加入", exact: true })
    .check();
  await a.getByRole("button", { name: "创建配对码", exact: true }).click();
  await a.getByRole("heading", { name: "等待另一台设备" }).waitFor();
  const directCode = (await a.locator(".transfer-code").textContent())
    .replaceAll(" ", "")
    .trim();
  assert.match(directCode, /^[A-Z]-[0-9]{4}$/);
  await b
    .getByRole("textbox", { name: "配对码字母", exact: true })
    .evaluate((input, value) => {
      const clipboard = new DataTransfer();
      clipboard.setData("text/plain", value);
      input.dispatchEvent(
        new ClipboardEvent("paste", {
          clipboardData: clipboard,
          bubbles: true,
          cancelable: true,
        }),
      );
    }, directCode);
  await a.getByRole("heading", { name: "消息", exact: true }).waitFor();
  assert.equal(
    await a.getByRole("button", { name: "确认连接", exact: true }).count(),
    0,
  );
  for (const page of [a, b])
    await page
      .getByText("直连可用 · 支持实时中继", { exact: true })
      .waitFor({ timeout: 15000 });
  assert.equal(
    await b
      .getByRole("checkbox", { name: "自动接收对方文件", exact: true })
      .isChecked(),
    false,
  );
  let relayRequests = 0;
  for (const page of [a, b])
    page.on("request", (request) => {
      if (/\/api\/transfer\/(upload|download)\//.test(request.url()))
        relayRequests++;
    });
  async function direct(sender, receiver, name, payload) {
    const source = resolve(temp, name);
    await writeFile(source, payload);
    await sender.getByLabel("选择互传文件").setInputFiles(source);
    await sender.getByRole("button", { name: /^发送所选文件/ }).click();
    const row = receiver
      .locator(".transfer-file")
      .filter({ has: receiver.getByText(name, { exact: true }) });
    const started = performance.now();
    await row.getByRole("button", { name: "点对点下载", exact: true }).click();
    await row.getByText(/已保存/).waitFor();
    if (benchmarkBytes && payload.length >= benchmarkBytes)
      console.log(
        `BENCH p2p: ${(payload.length / 1048576).toFixed(1)} MiB, ${((performance.now() - started) / 1000).toFixed(2)} s, ${(payload.length / 1048576 / ((performance.now() - started) / 1000)).toFixed(1)} MiB/s`,
      );
    const digest = await receiver.evaluate(async (fileName) => {
      const dir = await navigator.storage.getDirectory();
      const handle = await dir.getFileHandle(fileName);
      const bytes = await (await handle.getFile()).arrayBuffer();
      return Array.from(
        new Uint8Array(await crypto.subtle.digest("SHA-256", bytes)),
        (v) => v.toString(16).padStart(2, "0"),
      ).join("");
    }, name);
    assert.equal(digest, createHash("sha256").update(payload).digest("hex"));
  }
  await direct(
    a,
    b,
    "直连正向.bin",
    Buffer.alloc(
      benchmarkBytes ? benchmarkBytes + 37 : 2 * 1024 * 1024 + 37,
      129,
    ),
  );
  await direct(b, a, "直连反向.bin", Buffer.alloc(768 * 1024, 17));
  await direct(a, b, "直连空文件.txt", Buffer.alloc(0));
  for (const page of [a, b]) {
    const debugLogs = await page.evaluate(() => window.transferDebugLogs);
    const link = debugLogs.find(
      (item) => item.event === "直连链路详情",
    )?.details;
    assert.ok(link, "真实浏览器没有输出选中的直连链路");
    assert.equal(link.pairState, "succeeded");
    assert.equal(link.local.protocol, "udp");
    assert.equal(typeof link.local.port, "number");
    assert.equal(typeof link.remote.port, "number");
    const completed = debugLogs.find(
      (item) =>
        item.event === "文件传输结束" && item.details.state === "complete",
    )?.details;
    assert.ok(completed.confirmedBytes > 0);
    assert.ok(completed.durationMs >= 0);
    assert.ok(completed.averageMiBPerSecond > 0);
  }
  for (const [sender, receiver, canceller, label] of [
    [a, b, a, "发送方"],
    [a, b, b, "接收方"],
  ]) {
    const name = `取消直连-${label}.bin`;
    await sender.getByLabel("选择互传文件").setInputFiles({
      name,
      mimeType: "application/octet-stream",
      buffer: Buffer.alloc(16 << 20, 31),
    });
    await sender.getByRole("button", { name: /^发送所选文件/ }).click();
    const row = receiver
      .locator(".transfer-file")
      .filter({ has: receiver.getByText(name, { exact: true }) });
    await row.getByRole("button", { name: "点对点下载", exact: true }).click();
    await row.locator("progress").waitFor();
    await canceller
      .locator(".transfer-file")
      .filter({ has: canceller.getByText(name, { exact: true }) })
      .getByRole("button", { name: "取消", exact: true })
      .click();
    for (const page of [sender, receiver]) {
      await page
        .locator(".transfer-file")
        .filter({ has: page.getByText(name, { exact: true }) })
        .getByText(/已取消/)
        .waitFor();
      await page
        .getByText("直连可用 · 支持实时中继", { exact: true })
        .waitFor({ timeout: 15000 });
    }
    // 验证重建后确实能完成下一份文件，且不会混入取消文件的旧分块。
    await direct(
      sender,
      receiver,
      `取消后-${label}.bin`,
      Buffer.alloc((1 << 20) + 91, 193),
    );
  }
  for (const page of [a, b]) {
    const sizes = await page.evaluate(() => window.directChunkSizes);
    assert.equal(
      Math.max(...sizes),
      256 << 10,
      "大分块没有在真实 WebRTC 中启用",
    );
    assert.ok(sizes.every((size) => size > 0 && size <= 256 << 10));
  }
  assert.equal(
    relayRequests,
    0,
    "direct transfer unexpectedly used HTTP relay",
  );
  await b.getByRole("button", { name: "断开连接", exact: true }).click();
  await a
    .getByText("连接已结束。未完成的文件需要重新发送。", { exact: true })
    .waitFor();
  assert.deepEqual(errors, []);
  await a.goto(base + "/admin/settings/transfer");
  await a.getByLabel("中继总速率上限", { exact: true }).fill("0");
  await a.getByLabel("中继并发请求数", { exact: true }).fill("3");
  await a.getByLabel("互传单文件上限", { exact: true }).fill("128");
  await a.getByRole("button", { name: "保存设置", exact: true }).click();
  await a.getByText("设置已保存", { exact: true }).waitFor();
  await a.reload();
  await a.getByLabel("中继总速率上限", { exact: true }).waitFor();
  assert.equal(
    await a.getByLabel("中继总速率上限", { exact: true }).inputValue(),
    "0",
  );
  assert.equal(
    await a.getByLabel("中继并发请求数", { exact: true }).inputValue(),
    "3",
  );
  assert.equal(
    await a.getByLabel("互传单文件上限", { exact: true }).inputValue(),
    "128",
  );
  assert.deepEqual(errors, []);
  console.log(
    "PASS: 配对、双向文字与图片、图片预览限额及下载、失败重发、真实附件下载、双向 WebRTC 写盘、空文件、追加移除、断开清理、两种宽度布局",
  );
} catch (error) {
  await a
    ?.screenshot({
      path: resolve(root, "backend/bin/transfer-failure-a.png"),
      fullPage: true,
    })
    .catch(() => {});
  await b
    ?.screenshot({
      path: resolve(root, "backend/bin/transfer-failure-b.png"),
      fullPage: true,
    })
    .catch(() => {});
  console.error(logs);
  throw error;
} finally {
  await browser?.close();
  if (service && service.exitCode === null) {
    service.kill();
    await once(service, "exit");
  }
  // 临时路径由本脚本在固定测试目录下创建，防止清理到工作区外。
  if (temp.startsWith(resolve(root, "backend/bin/transfer-e2e-")))
    await rm(temp, { recursive: true, force: true });
}
