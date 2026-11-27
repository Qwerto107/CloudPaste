import assert from "node:assert/strict";
import { test } from "node:test";
import {
  collectTransferDevice,
  defaultTransferName,
} from "../src/sharing/transfer-device.js";

test("上报的设备详情只包含原始提示，不包含前端生成的名称", async () => {
  const brands = [
    { brand: "Not;A Brand", version: "99" },
    { brand: "Google Chrome", version: "140.0.1.2" },
  ];
  const device = await collectTransferDevice({
    userAgent: "private-UA (Windows NT 10.0) Chrome/140.0.0.0",
    maxTouchPoints: 0,
    userAgentData: {
      brands,
      async getHighEntropyValues() {
        return {
          platform: "Windows",
          platformVersion: "15.0.0",
          mobile: false,
          formFactors: ["Desktop"],
          fullVersionList: brands,
        };
      },
    },
  });
  assert.equal(device.platform_version, "15.0.0");
  assert.deepEqual(device.full_version_list, brands);
  for (const key of ["os", "os_version", "browser", "type", "summary"])
    assert.equal(key in device, false);
  assert.equal(JSON.stringify(device).includes("private-UA"), false);
});

test("自动名称使用固定格式，优先显示可用型号", () => {
  const nav = {
    userAgent: "Mozilla/5.0 (Windows NT 10.0) Chrome/155.0 Edg/155.0",
  };
  assert.equal(
    defaultTransferName("AWW1", nav),
    "Windows Edge · AWW1",
  );
  const android = {
    userAgent: "Mozilla/5.0 (Linux; Android 10; K) Chrome/155.0 Mobile",
  };
  assert.equal(
    defaultTransferName("AWW1", android, { model: " Pixel 9 " }),
    "Pixel 9 Chrome · AWW1",
  );
  assert.equal(
    defaultTransferName("AWW1", android, { model: "invalid/model" }),
    "Android Chrome · AWW1",
  );
});

test("缺少型号时使用可读的系统和类型，缺少浏览器信息时保留后缀", () => {
  const ipad = {
    userAgent: "Mozilla/5.0 (Macintosh) Version/26.0 Safari/617.1",
    maxTouchPoints: 5,
  };
  assert.equal(
    defaultTransferName("AWW1", ipad),
    "iPad Safari · AWW1",
  );
  assert.equal(
    defaultTransferName("AWW1", null),
    "设备 浏览器 · AWW1",
  );
});

test("读取被拒绝或不响应时限时回退，不阻塞配对", async () => {
  const agent = {
    platform: "Android",
    mobile: true,
    getHighEntropyValues: async () => {
      throw new Error("denied");
    },
  };
  assert.equal(
    (await collectTransferDevice({ userAgentData: agent })).platform,
    "Android",
  );
  agent.getHighEntropyValues = () => new Promise(() => {});
  assert.equal(
    (await collectTransferDevice({ userAgentData: agent }, 1)).mobile,
    true,
  );
  assert.deepEqual(await collectTransferDevice(null), {});
});
