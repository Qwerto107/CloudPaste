import assert from "node:assert/strict";
import { test } from "node:test";
import {
  folderPath,
  readFolder,
  folderFromFiles,
  createFolderDestination,
  createFolderEntry,
  folderMaxEntries,
} from "../src/sharing/transfer-folders.js";

function directory(name, initial = []) {
  const entries = new Map(
    initial.map((entry) => [entry.name.toLowerCase(), entry]),
  );
  return {
    name,
    kind: "directory",
    entries,
    async *values() {
      yield* entries.values();
    },
    async getDirectoryHandle(name, { create = false } = {}) {
      let entry = entries.get(name.toLowerCase());
      if (!entry && create) {
        entry = directory(name);
        entries.set(name.toLowerCase(), entry);
      }
      if (!entry) throw new DOMException("不存在", "NotFoundError");
      if (entry.kind !== "directory")
        throw new DOMException("类型不同", "TypeMismatchError");
      return entry;
    },
    async getFileHandle(name, { create = false } = {}) {
      let entry = entries.get(name.toLowerCase());
      if (!entry && create) {
        entry = { name, kind: "file" };
        entries.set(name.toLowerCase(), entry);
      }
      if (!entry) throw new DOMException("不存在", "NotFoundError");
      if (entry.kind !== "file")
        throw new DOMException("类型不同", "TypeMismatchError");
      return entry;
    },
  };
}

test("拒绝目录穿越、Windows 特殊名称、过深路径和控制字符", () => {
  for (const path of [
    "../x",
    "/x",
    "a//x",
    "C:/x",
    "a\\x",
    "a/NUL.txt",
    "x.",
    "x ",
    "a\0b",
    "a/".repeat(16) + "b",
  ])
    assert.throws(() => folderPath(path));
  assert.deepEqual(folderPath("资料/图片/示例.png"), [
    "资料",
    "图片",
    "示例.png",
  ]);
  assert.deepEqual(folderPath("", true), []);
});

test("超过 32 个文件仍按需枚举，保留空目录，不读取文件正文", async () => {
  let reads = 0;
  const files = Array.from({ length: 65 }, (_, index) => ({
    name: `${index}.txt`,
    kind: "file",
    async getFile() {
      reads++;
      const file = new File(["x"], `${index}.txt`);
      file.arrayBuffer = () => {
        throw new Error("不得读取正文");
      };
      return file;
    },
  }));
  const source = directory("资料", [
    directory("子目录", files),
    directory("空目录"),
  ]);
  const folder = await readFolder(source, 100);
  assert.equal(folder.count, 66);
  assert.equal(folder.size, 65);
  const iterator = folder.entries();
  assert.equal(reads, 65);
  assert.equal((await iterator.next()).value.path, "子目录/0.txt");
  assert.equal(reads, 66, "开始传输时只额外读取当前文件引用");
  await iterator.return();
});

test("已有目录和大小写重名文件不能覆盖，正常文件保持目录层级", async () => {
  const parent = directory("保存位置");
  const root = await createFolderDestination(parent, "资料");
  await assert.rejects(createFolderDestination(parent, "资料"), /已存在/);
  const file = await createFolderEntry(root, "子目录/文件.txt", false);
  assert.equal(file.name, "文件.txt");
  await assert.rejects(
    createFolderEntry(root, "子目录/文件.TXT", false),
    /已存在/,
  );
  await createFolderEntry(root, "空目录", true);
  assert.equal((await root.getDirectoryHandle("空目录")).kind, "directory");
  await assert.rejects(createFolderEntry(root, "../越界", false));
});

test("目录扫描支持取消和单文件大小保护，兼容选择器保留相对路径", async () => {
  const file = new File(["abc"], "文件.txt");
  Object.defineProperty(file, "webkitRelativePath", {
    value: "资料/子目录/文件.txt",
  });
  const folder = await folderFromFiles([file], 10);
  assert.equal([...folder.entries()][0].path, "子目录/文件.txt");
  await assert.rejects(folderFromFiles([file], 2), /单文件/);
  const controller = new AbortController();
  controller.abort();
  await assert.rejects(folderFromFiles([file], 10, controller.signal), {
    name: "AbortError",
  });
});

test("路径拒绝 Unicode 格式字符，正常非 ASCII 文件名保留", () => {
  for (const char of [
    "\u061c",
    "\u200b",
    "\u200d",
    "\u200e",
    "\u200f",
    "\u202e",
    "\u2066",
    "\u2067",
    "\u2068",
    "\u2069",
    "\ufeff",
    "\u{e0001}",
  ])
    assert.throws(() => folderPath(`dir/report${char}.txt`));
  assert.deepEqual(folderPath("资料/中文-é.txt"), ["资料", "中文-é.txt"]);
});

test("文件夹最多一万项，扫描不读取正文", async () => {
  const files = Array.from({ length: folderMaxEntries + 1 }, (_, index) => ({
    name: `file-${index}`,
    size: 0,
    webkitRelativePath: `folder/file-${index}`,
    arrayBuffer() {
      throw new Error("不得读取正文");
    },
  }));
  assert.equal(
    (await folderFromFiles(files.slice(0, folderMaxEntries), 1)).count,
    folderMaxEntries,
  );
  await assert.rejects(folderFromFiles(files, 1), /10000/);
});
