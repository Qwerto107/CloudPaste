import assert from "node:assert/strict";
import { test } from "node:test";
import { FolderDirectTransfers } from "../src/sharing/transfer-folder-direct.js";

// 同时启动 128 个异步文件读取，也不能重复抢占同一份共享窗口。
test("128 个点对点任务共享 8 MiB 窗口，取消后归还全部额度", async () => {
  const client = {
    directReady: true,
    peerChunkSize: 262144,
    peerReceiveWindow: 8388608,
    onEvent() {},
    folderFailed() {},
    peer: {
      sctp: { maxMessageSize: 262144 },
      createDataChannel(label) {
        return {
          label,
          readyState: "open",
          bufferedAmount: 0,
          send() {},
          close() {
            this.readyState = "closed";
          },
        };
      },
    },
  };
  const direct = new FolderDirectTransfers(client);
  const source = new Blob([new Uint8Array(1024 * 1024)]);
  const tasks = Array.from({ length: 128 }, (_, i) =>
    direct
      .start(
        { id: String(i), size: source.size, folder: { id: "parent" } },
        source,
      )
      .catch((e) => e.name),
  );
  await new Promise((resolve) => setTimeout(resolve, 30));
  assert.equal(direct.jobs.size, 128);
  assert.ok(direct.outstanding > 0 && direct.outstanding <= 8388608);
  assert.ok(
    [...direct.jobs.values()].every(
      (job) => job.outstanding > 0 && job.outstanding <= 65536,
    ),
  );
  direct.cancelFolder("parent");
  assert.ok((await Promise.all(tasks)).every((name) => name === "AbortError"));
  assert.equal(direct.outstanding, 0);
  assert.equal(direct.waiters.size, 0);
});

// 写盘完成才确认，迟到的写盘回调不能在取消后产生负数或完成事件。
test("拒绝陌生通道，取消慢写盘不继续确认或完成", async () => {
  const events = [];
  let finishWrite;
  let aborted = 0;
  const client = {
    directReady: true,
    onEvent() {},
    send(e) {
      events.push(e);
    },
    folderFailed() {},
  };
  const direct = new FolderDirectTransfers(client);
  let closed = false;
  direct.attach({
    label: "folder:unknown",
    close() {
      closed = true;
    },
  });
  assert.ok(closed);
  direct.prepare(
    { id: "file", size: 4, folder: { id: "parent" } },
    {
      write() {
        return new Promise((resolve) => (finishWrite = resolve));
      },
      async close() {},
      async abort() {
        aborted++;
      },
    },
  );
  direct.attach({
    label: "folder:file",
    bufferedAmount: 0,
    send(e) {
      events.push(e);
    },
    close() {},
  });
  const job = direct.jobs.get("file");
  direct.receive(job, new ArrayBuffer(4));
  await Promise.resolve();
  assert.equal(direct.queued, 4);
  assert.equal(events.length, 0);
  direct.cancelFolder("parent");
  finishWrite();
  await job.chain;
  assert.equal(direct.queued, 0);
  assert.equal(events.length, 0);
  assert.equal(aborted, 1);
});
