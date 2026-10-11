import assert from "node:assert/strict";
import { test } from "node:test";
import {
  mkdtemp,
  mkdir,
  readFile,
  rm,
  symlink,
  writeFile,
} from "node:fs/promises";
import { tmpdir } from "node:os";
import { isAbsolute, join, relative, resolve } from "node:path";
import { prepareWebUI } from "./prepare-webui.mjs";

async function fixture(t) {
  const root = await mkdtemp(join(tmpdir(), "cloudpaste-webui-test-"));
  t.after(async () => {
    // 仅删除本测试创建的临时项目，先核对绝对路径与固定名称前缀。
    assert.ok(isAbsolute(root));
    assert.ok(
      relative(resolve(tmpdir()), root).startsWith("cloudpaste-webui-test-"),
    );
    await rm(root, { recursive: true, force: true });
  });
  await mkdir(join(root, "frontend/dist/assets"), { recursive: true });
  await writeFile(join(root, "frontend/dist/index.html"), "<html>built</html>");
  await writeFile(
    join(root, "frontend/dist/assets/app.js"),
    "console.log('built');",
  );
  return root;
}

test("创建缺失的嵌入目录，只更新 dist 并移除旧构建文件", async (t) => {
  const root = await fixture(t);
  assert.equal(await prepareWebUI(root), 2);
  const destination = join(root, "backend/internal/webui/dist");
  await writeFile(join(destination, "old.js"), "old");
  await writeFile(
    join(root, "backend/internal/webui/handler.go"),
    "package webui",
  );
  assert.equal(await prepareWebUI(root), 2);
  assert.equal(
    await readFile(join(destination, "assets/app.js"), "utf8"),
    "console.log('built');",
  );
  await assert.rejects(readFile(join(destination, "old.js")), {
    code: "ENOENT",
  });
  assert.equal(
    await readFile(join(root, "backend/internal/webui/handler.go"), "utf8"),
    "package webui",
  );
});

test("拒绝隐藏配置、源码、依赖与源码映射，保留已有输出", async (t) => {
  for (const name of [".env", "src", "source", "node_modules", "app.js.map"]) {
    const root = await fixture(t);
    await prepareWebUI(root);
    await writeFile(join(root, "frontend/dist", name), "must not publish");
    await assert.rejects(prepareWebUI(root), /不应发布/);
    assert.equal(
      await readFile(
        join(root, "backend/internal/webui/dist/index.html"),
        "utf8",
      ),
      "<html>built</html>",
    );
  }
});

test("缺少入口时不清理已有输出", async (t) => {
  const root = await fixture(t);
  await prepareWebUI(root);
  await rm(join(root, "frontend/dist/index.html"));
  await assert.rejects(prepareWebUI(root), /index.html/);
  assert.equal(
    await readFile(
      join(root, "backend/internal/webui/dist/index.html"),
      "utf8",
    ),
    "<html>built</html>",
  );
});

test("拒绝构建源内的链接及指向其他目录的输出链接", async (t) => {
  const root = await fixture(t);
  const outside = join(root, "outside");
  await mkdir(outside);
  await writeFile(join(outside, "keep.txt"), "keep");
  const sourceLink = join(root, "frontend/dist/linked");
  await symlink(outside, sourceLink, "junction");
  await assert.rejects(prepareWebUI(root), /符号链接/);
  await rm(sourceLink);
  await mkdir(join(root, "backend/internal/webui"), { recursive: true });
  await symlink(outside, join(root, "backend/internal/webui/dist"), "junction");
  await assert.rejects(prepareWebUI(root), /符号链接/);
  assert.equal(await readFile(join(outside, "keep.txt"), "utf8"), "keep");
});
