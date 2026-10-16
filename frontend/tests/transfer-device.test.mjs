import assert from "node:assert/strict";
import { test } from "node:test";
import { collectTransferDevice } from "../src/sharing/transfer-device.js";

test("前端只上传原始提示，不判断系统、浏览器和设备类型", async () => {
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
