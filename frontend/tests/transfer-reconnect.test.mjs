import assert from "node:assert/strict";
import { test } from "node:test";

const listeners = new Map();
globalThis.window = {
  location: { origin: "https://share.example" },
  appConfig: {},
  addEventListener() {},
  removeEventListener() {},
};
const browserDocument = {
  visibilityState: "visible",
  addEventListener(name, listener) {
    listeners.set(name, listener);
  },
  removeEventListener(name) {
    listeners.delete(name);
  },
};
globalThis.localStorage = { setItem() {}, removeItem() {} };
globalThis.fetch = async () => ({
  ok: true,
  async json() {
    return {
      success: true,
      data: {
        member: 1,
        token: "memory-only-token",
        expires_at: new Date(Date.now() + 3600000).toISOString(),
      },
    };
  },
});
class FakeSocket {
  static OPEN = 1;
  readyState = 1;
  sent = [];
  send(value) {
    this.sent.push(JSON.parse(value));
  }
  close() {
    this.readyState = 3;
  }
}
globalThis.WebSocket = FakeSocket;
const { TransferClient } = await import("../src/sharing/transfer-client.js");
globalThis.document = browserDocument;
const paired = { type: "paired", relay_only: true, generation: 0 };

async function setup(t) {
  const events = [];
  document.visibilityState = "visible";
  const client = new TransferClient((event) => events.push(event));
  await client.open("join", { code: "A1234", name: "phone" });
  client.socket.onopen();
  client.handle(paired);
  t.after(() => client.close());
  return { client, events };
}

test("意外断线保留凭证，重连认证并忽略旧套接字的迟到事件", async (t) => {
  const { client, events } = await setup(t);
  const old = client.socket;
  old.onclose({ code: 1006 });
  assert.equal(client.closed, false);
  assert.equal(client.paused, true);
  assert.equal(events.at(-1).type, "reconnecting");
  assert.throws(() => client.send({ type: "text", text: "offline" }), /中断/);
  client.foreground();
  const restored = client.socket;
  assert.notEqual(restored, old);
  restored.onopen();
  assert.deepEqual(restored.sent[0], {
    type: "auth",
    token: "memory-only-token",
    resume: true,
  });
  restored.onmessage({ data: JSON.stringify({ type: "ready", paired: true }) });
  restored.onmessage({ data: JSON.stringify({ ...paired, generation: 1 }) });
  old.onclose({ code: 1006 });
  assert.equal(client.socket, restored);
  assert.equal(client.paused, false);
  assert.equal(client.peerGeneration, 1000000);
  client.send({ type: "text", text: "restored" });
  assert.equal(restored.sent.at(-1).text, "restored");
});

test("后台不反复建连，五分钟到期销毁配对及内存凭证", async (t) => {
  t.mock.timers.enable({ apis: ["setTimeout", "Date"] });
  const { client } = await setup(t);
  document.visibilityState = "hidden";
  client.socket.onclose({ code: 1006 });
  t.mock.timers.tick(299999);
  assert.equal(client.closed, false);
  t.mock.timers.tick(1);
  assert.equal(client.closed, true);
  assert.equal(client.resumeToken, null);
  assert.equal(listeners.has("visibilitychange"), false);
});

test("断线取消实际传输，保留普通文件供手动重发，恢复后同步最终结果", async (t) => {
  const { client } = await setup(t);
  const source = { name: "file.bin", size: 10 };
  client.sources.set("source", source);
  client.files.set("file", {
    id: "file",
    client_id: "source",
    state: "active",
    mode: "relay",
    from: 1,
    size: 10,
  });
  const upload = new AbortController();
  client.uploads.set("file", upload);
  client.socket.onclose({ code: 1006 });
  assert.equal(upload.signal.aborted, true);
  assert.equal(client.files.get("file").state, "failed");
  assert.equal(client.sources.get("source"), source);
  client.handle({
    type: "sync",
    folders: [],
    files: [
      {
        type: "state",
        id: "file",
        state: "complete",
        mode: "relay",
        bytes: 10,
      },
    ],
  });
  assert.equal(client.files.get("file").state, "complete");
  assert.equal(client.sources.has("source"), false);
});

test("前台探测有响应不重建，半开连接无响应时进入恢复流程", async (t) => {
  t.mock.timers.enable({ apis: ["setTimeout", "Date"] });
  const { client } = await setup(t);
  const socket = client.socket;
  client.foreground();
  assert.equal(socket.sent.at(-1).type, "check");
  client.handle({ type: "checked" });
  t.mock.timers.tick(3000);
  assert.equal(client.socket, socket);
  client.foreground();
  t.mock.timers.tick(3000);
  assert.equal(client.reconnecting, true);
  assert.equal(client.paused, true);
});

test("主动退出立即通知服务端，失效凭证不自动重试", async (t) => {
  const { client } = await setup(t);
  const socket = client.socket;
  client.close();
  assert.equal(socket.sent.at(-1).type, "leave");
  assert.equal(client.resumeToken, null);
  const other = await setup(t);
  other.client.socket.onclose({ code: 1008 });
  assert.equal(other.client.closed, true);
  assert.equal(other.client.reconnecting, false);
});

test("对方离线保留界面并禁止发送，双方恢复后解除暂停", async (t) => {
  const { client, events } = await setup(t);
  client.handle({
    type: "peer-offline",
    reconnect_until: new Date(Date.now() + 300000).toISOString(),
    generation: 1,
  });
  assert.equal(client.paused, true);
  assert.equal(client.reconnecting, false);
  assert.equal(events.at(-1).type, "peer-offline");
  client.handle({ ...paired, generation: 1 });
  assert.equal(client.paused, false);
  assert.equal(client.paired, true);
});
