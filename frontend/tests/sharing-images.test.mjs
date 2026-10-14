import assert from "node:assert/strict";
import { test } from "node:test";
import {
  embeddedImage,
  embeddedImageLimit,
  safeEmbeddedImage,
} from "../src/sharing/images.js";

const png = Buffer.from(
  "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+aD1sAAAAASUVORK5CYII=",
  "base64",
);

test("嵌入图片保存完整内容，并限制格式、伪造文件和大小", async () => {
  const value = await embeddedImage(
    new File([png], "pixel.png", { type: "image/png" }),
  );
  assert.equal(safeEmbeddedImage(value), true);
  assert.deepEqual(Buffer.from(value.split(",")[1], "base64"), png);
  await assert.rejects(
    embeddedImage(new File(["<svg/>"], "svg.png", { type: "image/png" })),
    /格式不一致/,
  );
  await assert.rejects(
    embeddedImage(
      new File(["<svg/>"], "vector.svg", { type: "image/svg+xml" }),
    ),
    /只支持/,
  );
  await assert.rejects(
    embeddedImage(new File([], "empty.png", { type: "image/png" })),
    /不能为空/,
  );
  await assert.rejects(
    embeddedImage(
      new File([new Uint8Array(embeddedImageLimit + 1)], "large.png", {
        type: "image/png",
      }),
    ),
    /超过/,
  );
});

test("图片数据 URI 仅接受有界位图数据，不接受 SVG、脚本和损坏的 Base64", () => {
  for (const value of [
    "data:image/svg+xml;base64,PHN2Zy8+",
    "javascript:alert(1)",
    "data:text/html;base64,WA==",
    "data:image/png;base64,***",
    "data:image/png;base64," + "A".repeat(embeddedImageLimit * 2),
  ])
    assert.equal(safeEmbeddedImage(value), false);
});
