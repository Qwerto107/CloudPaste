import { readFile, writeFile } from "node:fs/promises";
import { existsSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { chromium } from "playwright";

// 所有位图从同一 SVG 导出，修改图形后运行此脚本即可同步。
const publicDirectory = new URL("../public/", import.meta.url);
const source = await readFile(
  new URL("chain-cloud.svg", publicDirectory),
  "utf8",
);
const executablePath = [
  process.env.SHARING_TEST_BROWSER,
  "C:/Program Files/Google/Chrome/Application/chrome.exe",
  "C:/Program Files (x86)/Microsoft/Edge/Application/msedge.exe",
].find((path) => path && existsSync(path));
const browser = await chromium.launch({
  headless: true,
  ...(executablePath ? { executablePath } : {}),
});
try {
  const page = await browser.newPage({
    deviceScaleFactor: 1,
    colorScheme: "light",
  });
  const frames = [];
  for (const size of [16, 32, 48]) {
    await page.setViewportSize({ width: size, height: size });
    await page.setContent(
      `<style>html,body{margin:0;background:transparent}svg{display:block;width:100%;height:100%}</style>${source}`,
    );
    const png = await page.screenshot({ omitBackground: true });
    frames.push({ size, png });
    if (size === 32)
      await writeFile(new URL("icons-32.png", publicDirectory), png);
  }
  // ICO 目录指向原生 PNG 帧，无需引入额外图像处理依赖。
  const header = Buffer.alloc(6 + frames.length * 16);
  header.writeUInt16LE(1, 2);
  header.writeUInt16LE(frames.length, 4);
  let offset = header.length;
  frames.forEach(({ size, png }, index) => {
    const entry = 6 + index * 16;
    header[entry] = size;
    header[entry + 1] = size;
    header.writeUInt16LE(1, entry + 4);
    header.writeUInt16LE(32, entry + 6);
    header.writeUInt32LE(png.length, entry + 8);
    header.writeUInt32LE(offset, entry + 12);
    offset += png.length;
  });
  await writeFile(
    new URL("favicon.ico", publicDirectory),
    Buffer.concat([header, ...frames.map(({ png }) => png)]),
  );
  // 主屏幕图标使用浅色实底并留出安全边距，圆角由操作系统裁切。
  await page.setViewportSize({ width: 180, height: 180 });
  await page.setContent(
    `<style>html,body{margin:0;background:#f0faf6}body{display:grid;place-items:center;width:180px;height:180px}svg{width:136px;height:136px}</style>${source}`,
  );
  await page.screenshot({
    path: fileURLToPath(new URL("apple-touch-icon.png", publicDirectory)),
  });
  console.log("已生成 ICO（16/32/48）、32px PNG 与 180px 触屏图标");
} finally {
  await browser.close();
}
