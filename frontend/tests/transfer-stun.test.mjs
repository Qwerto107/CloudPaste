import assert from "node:assert/strict";
import { test } from "node:test";
import { parseSTUNServers } from "../src/sharing/transfer-stun.js";

test("单地址与多地址兼容，去重且保留 IPv6 和输入顺序", () => {
  assert.deepEqual(parseSTUNServers(""), []);
  assert.deepEqual(parseSTUNServers(" stun:one.example:3478 "), [
    "stun:one.example:3478",
  ]);
  assert.deepEqual(
    parseSTUNServers(
      "\r\nstun:one.example:3478\r\nstun:ONE.example:3478\n\nstun:[2001:db8::1]:3478\n",
    ),
    ["stun:one.example:3478", "stun:[2001:db8::1]:3478"],
  );
  const five = Array.from(
    { length: 5 },
    (_, index) => `stun:server-${index}.example:3478`,
  );
  assert.deepEqual(parseSTUNServers(five.join("\n")), five);
  assert.throws(() =>
    parseSTUNServers([...five, "stun:six.example:3478"].join("\n")),
  );
});

test("拒绝非 STUN、非法地址、凭据、参数和过长输入", () => {
  for (const value of [
    null,
    [],
    true,
    "turn:one.example:3478",
    "https://one.example:3478",
    "STUN:one.example:3478",
    "stun:one.example",
    "stun:one.example:0",
    "stun:one.example:65536",
    "stun:user@one.example:3478",
    "stun:one.example:3478?transport=udp",
    "stun:one.example:3478/path",
    "stun:-invalid.example:3478",
    "stun:invalid..example:3478",
    "stun:2001:db8::1:3478",
    "stun:[not:ipv6]:3478",
    "stun:example\u200b.com:3478",
    " ".repeat(1285),
  ])
    assert.throws(() => parseSTUNServers(value), /STUN/);
});

globalThis.window = {
  location: { origin: "https://share.example" },
  appConfig: {},
};
globalThis.localStorage = { setItem() {}, removeItem() {} };
const { TransferClient } = await import("../src/sharing/transfer-client.js");

test("两端各用本站或本机配置，重建时保留覆盖，强制中继不查询 STUN", async (t) => {
  const original = {
    fetch: globalThis.fetch,
    socket: globalThis.WebSocket,
    peer: globalThis.RTCPeerConnection,
  };
  const bodies = [],
    configurations = [];
  globalThis.fetch = async (url, options) => {
    bodies.push(JSON.parse(options.body));
    return {
      ok: true,
      json: async () => ({
        success: true,
        data: { member: 1, token: "memory-only-token" },
      }),
    };
  };
  globalThis.WebSocket = class {
    static OPEN = 1;
    readyState = 1;
    close() {}
    send() {}
  };
  globalThis.RTCPeerConnection = class {
    constructor(config) {
      configurations.push(config);
    }
    close() {}
  };
  const clients = [];
  t.after(() => {
    for (const client of clients) client.close();
    globalThis.fetch = original.fetch;
    globalThis.WebSocket = original.socket;
    globalThis.RTCPeerConnection = original.peer;
  });
  const site = ["stun:site-a.example:3478", "stun:site-b.example:3478"];
  const message = { relay_only: false, stun_servers: site, stun: site[0] };
  for (const [override, expected] of [
    [null, site],
    ["stun:local.example:3478", ["stun:local.example:3478"]],
    ["", []],
  ]) {
    const client = new TransferClient(() => {});
    clients.push(client);
    await client.open("join", { code: "A1234", name: "device" }, override);
    client.startPeer(message);
    assert.deepEqual(
      configurations.at(-1).iceServers,
      expected.map((url) => ({ urls: url })),
    );
    client.restartPeer();
    assert.deepEqual(
      configurations.at(-1).iceServers,
      expected.map((url) => ({ urls: url })),
    );
    const count = configurations.length;
    client.startPeer({ ...message, relay_only: true });
    assert.equal(configurations.length, count);
  }
  assert.ok(
    bodies.every(
      (body) => Object.keys(body).sort().join(",") === "code,device,name",
    ),
  );
  const client = new TransferClient(() => {});
  clients.push(client);
  const before = bodies.length;
  await assert.rejects(
    client.open("join", {}, "turn:invalid.example:3478"),
    /STUN/,
  );
  assert.equal(bodies.length, before, "非法配置不得创建服务端连接");
  client.member = 1;
  client.startPeer({ stun: "stun:legacy.example:3478" });
  assert.deepEqual(configurations.at(-1).iceServers, [
    { urls: "stun:legacy.example:3478" },
  ]);
});
