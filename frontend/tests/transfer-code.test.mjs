import assert from "node:assert/strict";
import { test } from "node:test";
import {
  normalizePairingCode,
  validPairingCode,
  displayPairingCode,
} from "../src/sharing/transfer-code.js";

test("配对码显示横杠，复制与请求去掉横杠，自动解析大小写和空白", () => {
  for (const [input, compact, display] of [
    ["a-1234", "A1234", "A-1234"],
    [" B6666 ", "B6666", "B-6666"],
    [" z - 0 0 0 0\n", "Z0000", "Z-0000"],
  ]) {
    assert.equal(normalizePairingCode(input), compact);
    assert.equal(validPairingCode(input), true);
    assert.equal(displayPairingCode(input), display);
  }
  for (const input of [
    "",
    "12345",
    "AB1234",
    "A123",
    "A12345",
    "A12B4",
    "12345678",
    "Ａ1234",
  ]) {
    assert.equal(validPairingCode(input), false);
    assert.equal(displayPairingCode(input), "");
  }
});
