import assert from "node:assert/strict";
import { test } from "node:test";
import {
  normalizePairingCode,
  validPairingCode,
  displayPairingCode,
} from "../src/sharing/transfer-code.js";

test("配对码为四位数字，自动去掉空白和横杠", () => {
  for (const [input, compact] of [
    ["1234", "1234"],
    [" 0420 ", "0420"],
    [" 12 - 3 4\n", "1234"],
    ["0000", "0000"],
  ]) {
    assert.equal(normalizePairingCode(input), compact);
    assert.equal(validPairingCode(input), true);
    assert.equal(displayPairingCode(input), compact);
  }
  for (const input of [
    "",
    "123",
    "12345",
    "A1234",
    "12a4",
    "12345678",
    "１２３４",
  ]) {
    assert.equal(validPairingCode(input), false);
    assert.equal(displayPairingCode(input), "");
  }
});
