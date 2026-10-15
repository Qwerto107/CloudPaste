import assert from "node:assert/strict";
import { test } from "node:test";
import { auditDateBoundary } from "../src/sharing/audit-date.js";

test("日志日期按本地全天查询和清理，支持只选同一天", () => {
  const previous = process.env.TZ;
  try {
    process.env.TZ = "Asia/Taipei";
    assert.equal(auditDateBoundary("2026-10-15"), "2026-10-14T16:00:00.000Z");
    assert.equal(
      auditDateBoundary("2026-10-15", true),
      "2026-10-15T15:59:59.999Z",
    );
    assert.throws(() => auditDateBoundary(""), /有效的日期/);
    assert.throws(() => auditDateBoundary("invalid"), /有效的日期/);

    // 夏令时开始与结束当天的长度不同，不能用固定 24 小时截取日期。
    process.env.TZ = "America/New_York";
    for (const [date, hours] of [
      ["2026-03-08", 23],
      ["2026-11-01", 25],
    ]) {
      const start = Date.parse(auditDateBoundary(date));
      const end = Date.parse(auditDateBoundary(date, true));
      assert.equal(end - start + 1, hours * 60 * 60 * 1000);
    }
  } finally {
    if (previous === undefined) delete process.env.TZ;
    else process.env.TZ = previous;
  }
});
