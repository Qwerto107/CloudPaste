import assert from "node:assert/strict";
import test from "node:test";
import { shareText } from "../src/sharing/share-text.js";

test("分享文字复制包含链接和原样密码，无密码时省略密码行", () => {
  assert.equal(
    shareText("https://example.test/paste/note", "中文 pass"),
    "链接：https://example.test/paste/note\n密码：中文 pass",
  );
  assert.equal(
    shareText("https://example.test/file/files"),
    "链接：https://example.test/file/files",
  );
});
