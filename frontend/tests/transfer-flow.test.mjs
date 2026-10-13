import assert from "node:assert/strict";
import { test } from "node:test";

globalThis.window = {
  location: { origin: "https://share.example" },
  appConfig: {},
};
globalThis.localStorage = { setItem() {}, removeItem() {} };
const { TransferClient } = await import("../src/sharing/transfer-client.js");
const { validateChatImage } = await import("../src/sharing/transfer-images.js");

test("下载工具按需切片，分段取消相互独立，断开后全部释放", async () => {
  const originalFetch = globalThis.fetch;
  const captured = [];
  globalThis.fetch = (url, options) =>
    new Promise((resolve, reject) => {
      captured.push({ url, ...options, resolve });
      options.signal.addEventListener("abort", () =>
        reject(new DOMException("cancelled", "AbortError")),
      );
    });
  const client = new TransferClient(() => {});
  client.files.set("file", {
    id: "file",
    client_id: "source",
    state: "active",
    mode: "tool",
  });
  const slices = [];
  client.sources.set("source", {
    size: 1000,
    slice(first, end) {
      slices.push([first, end]);
      return new Blob(["part"]);
    },
    arrayBuffer() {
      throw new Error("must not buffer the source");
    },
  });
  try {
    const one = client.uploadRange({
      id: "file",
      request_id: "first",
      start: 100,
      end: 200,
      token: "one",
    });
    const two = client.uploadRange({
      id: "file",
      request_id: "second",
      start: 400,
      end: 1000,
      token: "two",
    });
    assert.deepEqual(slices, [
      [100, 200],
      [400, 1000],
    ]);
    assert.match(captured[0].url, /\/file\/first$/);
    assert.equal(captured[0].headers.Authorization, "Transfer one");
    assert.equal(captured[1].headers.Authorization, "Transfer two");
    client.handle({ type: "range-stop", id: "file", request_id: "first" });
    await assert.rejects(one, { name: "AbortError" });
    assert.equal(captured[1].signal.aborted, false);
    captured[1].resolve({ ok: true });
    await two;
    assert.equal(client.rangeUploads.size, 0);
    await assert.rejects(
      client.uploadRange({ id: "file", request_id: "bad", start: -1, end: 5 }),
      /无效/,
    );
    const pending = client.uploadRange({
      id: "file",
      request_id: "third",
      start: 0,
      end: 1000,
      token: "three",
    });
    client.close();
    await assert.rejects(pending, { name: "AbortError" });
    assert.equal(client.sources.size, 0);
    assert.equal(client.rangeUploads.size, 0);
  } finally {
    globalThis.fetch = originalFetch;
    client.close();
  }
});

test("聊天图片在解码前拒绝伪装格式、超大文件和像素，以及动画 PNG", async () => {
  const png = new Uint8Array(33);
  const header = new DataView(png.buffer);
  header.setUint32(0, 0x89504e47);
  header.setUint32(4, 0x0d0a1a0a);
  header.setUint32(8, 13);
  header.setUint32(12, 0x49484452);
  header.setUint32(16, 320);
  header.setUint32(20, 240);
  await validateChatImage(new Blob([png], { type: "image/png" }));
  await assert.rejects(
    validateChatImage(new Blob(["<svg/>"], { type: "image/svg+xml" })),
    /仅支持/,
  );
  await assert.rejects(
    validateChatImage(new Blob(["<script>"], { type: "image/png" })),
    /格式无效/,
  );
  header.setUint32(16, 65535);
  await assert.rejects(
    validateChatImage(new Blob([png], { type: "image/png" })),
    /像素/,
  );
  await assert.rejects(
    validateChatImage(
      new Blob([new Uint8Array((4 << 20) + 1)], { type: "image/png" }),
    ),
    /4 MiB/,
  );
  header.setUint32(16, 320);
  const animation = new Uint8Array(12);
  new DataView(animation.buffer).setUint32(4, 0x6163544c);
  await assert.rejects(
    validateChatImage(new Blob([png, animation], { type: "image/png" })),
    /动态图片/,
  );
});

test("图片等待当前文件完成后自动接收，被拒绝时不会循环请求", () => {
  const client = new TransferClient(() => {});
  const sent = [];
  client.member = 0;
  client.send = (message) => sent.push(message);
  client.files.set("busy", { id: "busy", state: "active" });
  client.handle({
    type: "file",
    id: "picture",
    from: 1,
    state: "offered",
    image_type: "image/png",
    size: 100,
  });
  assert.equal(sent.length, 0);
  client.handle({
    type: "state",
    id: "busy",
    state: "complete",
    mode: "relay",
  });
  assert.deepEqual(sent, [{ type: "accept", id: "picture", mode: "relay" }]);
  client.handle({ type: "error", id: "picture", message: "繁忙" });
  assert.deepEqual(sent.at(-1), { type: "cancel", id: "picture" });
  assert.equal(sent.length, 2);
});

test("自动接收默认关闭，开启后逐个接收，关闭或断开后不再接收", () => {
  const client = new TransferClient(() => {});
  client.member = 0;
  const sent = [];
  client.send = (message) => sent.push(message);
  for (const id of ["first", "second"])
    client.handle({
      type: "file",
      id,
      client_id: id,
      from: 1,
      state: "offered",
    });
  assert.equal(sent.length, 0);
  client.setAutoReceive(true);
  assert.deepEqual(sent, [{ type: "accept", id: "first", mode: "relay" }]);
  client.handle({ type: "state", id: "first", state: "active", mode: "relay" });
  assert.equal(sent.length, 1);
  client.handle({
    type: "state",
    id: "first",
    state: "complete",
    mode: "relay",
  });
  assert.deepEqual(sent.at(-1), {
    type: "accept",
    id: "second",
    mode: "relay",
  });
  client.setAutoReceive(false);
  client.handle({
    type: "file",
    id: "third",
    client_id: "third",
    from: 1,
    state: "offered",
  });
  client.handle({
    type: "state",
    id: "second",
    state: "complete",
    mode: "relay",
  });
  assert.equal(sent.length, 2);
  client.close();
  assert.equal(client.autoReceive, false);
});

test("失败重发复用文件但创建新任务，拒绝重复点击，服务端拒绝后仍可重试", () => {
  const events = [],
    sent = [];
  const client = new TransferClient((event) => events.push(event));
  client.member = 0;
  client.maxFile = 1024;
  client.send = (value) => sent.push(value);
  const source = { name: "retry.txt", size: 10 };
  client.files.set("old", {
    id: "old",
    client_id: "original",
    from: 0,
    state: "active",
  });
  client.sources.set("original", source);
  client.handle({ type: "state", id: "old", state: "failed", mode: "relay" });
  assert.equal(client.sources.get("original"), source);
  client.retry("old");
  const rejected = sent.at(-1).client_id;
  assert.equal(sent.at(-1).type, "offer");
  assert.notEqual(rejected, "original");
  assert.throws(() => client.retry("old"), /无法重新发送/);
  client.handle({ type: "error", client_id: rejected, message: "额度不足" });
  assert.equal(client.sources.has(rejected), false);
  assert.equal(events.at(-2).pending, false);
  client.retry("old");
  const accepted = sent.at(-1).client_id;
  client.handle({
    type: "file",
    id: "new",
    client_id: accepted,
    from: 0,
    state: "offered",
  });
  assert.equal(client.sources.has("original"), false);
  assert.equal(client.sources.get(accepted), source);
  assert.equal(client.files.get("old").retried, true);
  assert.throws(() => client.retry("old"), /无法重新发送/);
  client.handle({ type: "state", id: "new", state: "complete", mode: "relay" });
  assert.equal(client.sources.size, 0);
});

test("重发不绕过发送者身份、会话额度或主动移除，晚到异常不覆盖终态", () => {
  const client = new TransferClient(() => {});
  client.member = 0;
  client.maxFile = 1024;
  const sent = [];
  client.send = (value) => sent.push(value);
  const file = { id: "old", client_id: "original", from: 1, state: "failed" };
  client.files.set(file.id, file);
  client.sources.set(file.client_id, { name: "retry.txt", size: 10 });
  assert.throws(() => client.retry(file.id), /无法重新发送/);
  file.from = 0;
  for (let i = 0; i < 31; i++) client.files.set(String(i), {});
  assert.throws(() => client.retry(file.id), /最多发送 32/);
  assert.equal(sent.length, 0);
  client.forgetSource(file.id);
  assert.equal(client.sources.size, 0);
  client.fileFailed(file.id, new Error("迟到的错误"));
  assert.equal(sent.length, 0);
  file.state = "active";
  client.fileFailed(file.id, new Error("读取失败"));
  assert.deepEqual(sent, [{ type: "fail", id: file.id }]);
});

test("取消直连后重建连接，旧通道事件和旧协商不会影响后续文件", async () => {
  const originalPeer = globalThis.RTCPeerConnection;
  class Peer {
    close() {
      this.connectionState = "closed";
      this.onconnectionstatechange?.();
    }
    async setRemoteDescription(value) {
      this.remoteDescription = value;
    }
    async addIceCandidate() {}
    async createAnswer() {
      return { type: "answer" };
    }
    async setLocalDescription(value) {
      this.localDescription = value;
    }
  }
  const channel = () => ({
    label: "transfer",
    close() {
      this.onclose?.();
    },
  });
  globalThis.RTCPeerConnection = Peer;
  try {
    const events = [],
      signals = [];
    const client = new TransferClient((event) => events.push(event));
    client.member = 1;
    client.send = (message) => signals.push(message);
    client.startPeer({ relay_only: false, stun: "" });
    const oldPeer = client.peer,
      oldChannel = channel();
    client.attachChannel(oldChannel);
    oldChannel.onopen();
    const job = { id: "cancelled-file", cancelled: false };
    client.sending = job;
    client.files.set(job.id, { id: job.id, state: "active", mode: "p2p" });
    client.handle({
      type: "state",
      id: job.id,
      state: "cancelled",
      mode: "p2p",
    });
    assert.ok(job.cancelled);
    assert.notEqual(client.peer, oldPeer);
    assert.equal(oldPeer.connectionState, "closed");
    assert.equal(client.peerGeneration, 1);
    const newPeer = client.peer,
      newChannel = channel();
    client.attachChannel(newChannel);
    newChannel.onopen();
    // 模拟迟到的关闭、数据和 ICE 事件，新的直连不能被这些旧事件关闭。
    oldChannel.onclose();
    oldChannel.onmessage({ data: new ArrayBuffer(1) });
    oldPeer.onicecandidate({ candidate: null });
    assert.equal(client.directReady, true);
    assert.equal(events.filter((event) => event.type === "error").length, 0);
    assert.equal(signals.length, 0);
    await client.signal({ generation: 0, description: { type: "offer" } });
    assert.equal(newPeer.remoteDescription, undefined);
    await client.signal({ generation: 1, description: { type: "offer" } });
    assert.equal(signals.at(-1).data.generation, 1);
    client.handle({
      type: "state",
      id: job.id,
      state: "cancelled",
      mode: "p2p",
    });
    assert.equal(client.peer, newPeer);
    assert.equal(client.directReady, true);
    client.close();
  } finally {
    globalThis.RTCPeerConnection = originalPeer;
  }
});

test("取消时尚未完成的旧写盘关闭不会清空下一次接收", async () => {
  const client = new TransferClient(() => {});
  const sent = [];
  client.send = (value) => sent.push(value);
  let finishClose;
  const closing = new Promise((resolve) => {
    finishClose = resolve;
  });
  const old = {
    id: "old",
    size: 0,
    bytes: 0,
    chain: Promise.resolve(),
    writer: { close: () => closing, abort: async () => {} },
  };
  client.receiving = old;
  client.receiveDirect(JSON.stringify({ type: "end", id: old.id }));
  await Promise.resolve();
  client.abortDirect(old.id);
  const next = { id: "next" };
  client.receiving = next;
  finishClose();
  await old.chain;
  assert.equal(client.receiving, next);
  assert.equal(sent.length, 0);
});

test("直连窗口由写盘确认唤醒，取消立即结束等待", async () => {
  const client = new TransferClient(() => {});
  client.directReady = true;
  client.peerReceiveWindow = 8 << 20;
  client.channel = { bufferedAmount: 0 };
  const job = { id: "file", sent: 8 << 20, acked: 0, progress: Date.now() };
  client.sending = job;
  let resolved = false;
  const pending = client
    .waitWindow(job, 64 << 10)
    .then(() => (resolved = true));
  await Promise.resolve();
  assert.equal(resolved, false, "未确认的数据不能超过窗口");
  assert.equal(typeof job.wake, "function");
  client.receiveDirect(
    JSON.stringify({ type: "ack", id: job.id, bytes: 256 << 10 }),
  );
  await pending;
  assert.equal(resolved, true);
  job.acked = 0;
  const cancelled = client.waitWindow(job, 64 << 10);
  client.abortDirect(job.id);
  await assert.rejects(cancelled, /中断|取消|停滞/);
});

test("发送端对旧页面保留 2 MiB 窗口，网络缓冲独立限制为 4 MiB", async () => {
  const client = new TransferClient(() => {});
  client.directReady = true;
  client.channel = { bufferedAmount: 0 };
  const job = { id: "file", sent: 2 << 20, acked: 0, progress: Date.now() };
  client.sending = job;
  let resolved = false;
  const legacy = client.waitWindow(job, 256 << 10).then(() => {
    resolved = true;
  });
  await Promise.resolve();
  assert.equal(resolved, false, "旧页面不能被新窗口压满");
  client.receiveDirect(
    JSON.stringify({ type: "ack", id: job.id, bytes: 256 << 10 }),
  );
  await legacy;
  client.peerReceiveWindow = 8 << 20;
  client.channel.bufferedAmount = 4 << 20;
  resolved = false;
  const buffered = client.waitWindow(job, 256 << 10).then(() => {
    resolved = true;
  });
  await Promise.resolve();
  assert.equal(resolved, false, "接收窗口未满时也不能超过网络缓冲上限");
  client.channel.bufferedAmount = 2 << 20;
  job.wake();
  await buffered;
});

test("直连分块同时遵守对端应用与 SCTP 上限，旧页面保留兼容分块", async () => {
  for (const [application, sctp, expected] of [
    [256 << 10, 256 << 10, 256 << 10],
    [256 << 10, 16 << 10, 16 << 10],
    [256 << 10, 0, 256 << 10],
    [undefined, 256 << 10, 64 << 10],
    [256 << 10, undefined, 16 << 10],
  ]) {
    const client = new TransferClient(() => {});
    const payload = Uint8Array.from(
      { length: (768 << 10) + 37 },
      (_, i) => i % 251,
    );
    const chunks = [],
      controls = [];
    client.directReady = true;
    client.peerChunkSize = application;
    client.peer = { sctp: { maxMessageSize: sctp } };
    client.files.set("file", { client_id: "source", state: "active" });
    client.sources.set("source", new Blob([payload]));
    client.channel = {
      bufferedAmount: 0,
      send(data) {
        if (typeof data === "string") controls.push(JSON.parse(data));
        else {
          chunks.push(Buffer.from(data));
          // 模拟异步写盘确认，不能在 send 返回前确认尚未计入 sent 的数据。
          queueMicrotask(() =>
            client.receiveDirect(
              JSON.stringify({
                type: "ack",
                id: "file",
                bytes: client.sending.sent,
              }),
            ),
          );
        }
      },
    };
    await client.sendDirect("file");
    assert.equal(chunks[0].byteLength, expected);
    assert.ok(chunks.every((chunk) => chunk.length <= expected));
    assert.deepEqual(Buffer.concat(chunks), Buffer.from(payload));
    assert.deepEqual(controls, [{ type: "end", id: "file" }]);
  }
});

test("应用分块能力只在首次协商时确认，缺失或无效能力按旧页面处理", async () => {
  for (const [advertised, expected] of [
    [256 << 10, 256 << 10],
    [1 << 20, 256 << 10],
    [undefined, 64 << 10],
    ["262144", 64 << 10],
    [-1, 64 << 10],
  ]) {
    const client = new TransferClient(() => {});
    const sent = [];
    client.member = 1;
    client.send = (message) => sent.push(message);
    client.peer = {
      async setRemoteDescription(value) {
        this.remoteDescription = value;
      },
      async createAnswer() {
        return { type: "answer" };
      },
      async setLocalDescription(value) {
        this.localDescription = value;
      },
    };
    await client.signal({
      description: { type: "offer" },
      max_chunk_size: advertised,
    });
    assert.equal(client.peerChunkSize, expected);
    assert.equal(sent[0].data.max_chunk_size, 256 << 10);
    await assert.rejects(
      client.signal({ description: { type: "offer" } }),
      /重复协商/,
    );
    assert.equal(client.peerChunkSize, expected);
  }
});

test("接收窗口只接受有界整数声明，缺失或无效时兼容旧页面", async () => {
  for (const [advertised, expected] of [
    [8 << 20, 8 << 20],
    [64 << 20, 8 << 20],
    [2 << 20, 2 << 20],
    [undefined, 2 << 20],
    ["8388608", 2 << 20],
    [-1, 2 << 20],
    [64 << 10, 2 << 20],
    [Infinity, 2 << 20],
  ]) {
    const client = new TransferClient(() => {});
    const sent = [];
    client.member = 1;
    client.send = (message) => sent.push(message);
    client.peer = {
      async setRemoteDescription(value) {
        this.remoteDescription = value;
      },
      async createAnswer() {
        return { type: "answer" };
      },
      async setLocalDescription(value) {
        this.localDescription = value;
      },
    };
    await client.signal({
      description: { type: "offer" },
      receive_window: advertised,
    });
    assert.equal(client.peerReceiveWindow, expected);
    assert.equal(sent[0].data.receive_window, 8 << 20);
    await assert.rejects(
      client.signal({ description: { type: "offer" } }),
      /重复协商/,
    );
    assert.equal(client.peerReceiveWindow, expected);
  }
});

test("慢写盘保持有界队列，批量确认只包含已写入字节，尾块也被保存", async () => {
  const client = new TransferClient(() => {});
  const acks = [],
    writes = [],
    controls = [];
  let release;
  let closed = false;
  const gate = new Promise((resolve) => (release = resolve));
  client.channel = { send: (raw) => acks.push(JSON.parse(raw)) };
  client.send = (value) => controls.push(value);
  const job = {
    id: "file",
    size: (8 << 20) + 37,
    bytes: 0,
    received: 0,
    queued: 0,
    chunks: [],
    pendingBytes: 0,
    chain: Promise.resolve(),
    lastProgress: 0,
    lastUI: 0,
    writer: {
      async write(bytes) {
        await gate;
        writes.push(bytes);
      },
      async close() {
        closed = true;
      },
    },
  };
  client.receiving = job;
  for (let i = 0; i < 128; i++)
    client.receiveDirect(new Uint8Array(64 << 10).fill(i).buffer);
  assert.equal(job.queued, 8 << 20);
  assert.equal(acks.length, 0, "慢写盘期间不能提前确认");
  assert.throws(() => client.receiveDirect(new ArrayBuffer(37)), /接收限制/);
  release();
  await job.chain;
  assert.equal(job.queued, 0);
  assert.equal(acks.length, 32, "每批写盘后确认，避免逐消息确认");
  assert.equal(acks.at(-1).bytes, 8 << 20);
  for (let i = 0; i < 128; i++)
    assert.ok(
      writes[Math.floor(i / 4)]
        .subarray((i % 4) * (64 << 10), ((i % 4) + 1) * (64 << 10))
        .every((value) => value === i),
    );
  client.receiveDirect(new Uint8Array(37).fill(99).buffer);
  await job.chain;
  assert.equal(writes.at(-1).length, 37);
  client.receiveDirect(JSON.stringify({ type: "end", id: job.id }));
  await job.chain;
  assert.equal(closed, true);
  assert.equal(controls.at(-1).type, "complete");
  assert.equal(controls.at(-1).bytes, job.size);
});
