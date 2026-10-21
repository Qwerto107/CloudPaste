import { FolderDirectTransfers } from "./transfer-folder-direct.js";
import { apiURL, backendBase, request } from "./api.js";
import { chatImageLimit, validateChatImage } from "./transfer-images.js";
import { collectTransferDevice } from "./transfer-device.js";
import { parseSTUNServers } from "./transfer-stun.js";
import {
  canSaveFolders,
  folderMaxConcurrency,
  folderDefaultConcurrency,
  folderMaxEntries,
  folderPath,
  createFolderDestination,
  createFolderEntry,
} from "./transfer-folders.js";

const maxChunkSize = 256 * 1024;
const legacyChunkSize = 64 * 1024;
const diskBatchSize = 256 * 1024;
// 放大有界窗口以减少等待；接收队列仍由写盘确认控制，不随文件总大小增长。
const receiveWindow = 8 * 1024 * 1024;
const legacyReceiveWindow = 2 * 1024 * 1024;
const sendBufferLimit = 4 * 1024 * 1024;
const reconnectWindow = 5 * 60 * 1000;

// getRandomValues 也能用于局域网 HTTP；这些编号仅关联消息，认证票据由服务端生成。
export function transferID() {
  return Array.from(crypto.getRandomValues(new Uint8Array(16)), (byte) =>
    byte.toString(16).padStart(2, "0"),
  ).join("");
}

// 所有设备使用同一个下载入口；直连保存按实际能力检测，不判断设备名称。
export function canSaveDirectly() {
  return (
    globalThis.isSecureContext &&
    typeof window.showSaveFilePicker === "function"
  );
}

export class TransferClient {
  constructor(onEvent, debugEnabled = () => false) {
    this.onEvent = onEvent;
    this.debugEnabled = debugEnabled;
    this.sources = new Map();
    this.pendingOffers = new Set();
    this.files = new Map();
    this.uploads = new Map();
    this.rangeUploads = new Map();
    this.imageDownloads = new Map();
    this.autoReceive = false;
    this.signalChain = Promise.resolve();
    this.peerGeneration = 0;
    this.candidates = [];
    this.transferTimings = new Map();
    this.directEndpoints = null;
    this.closed = false;
    this.folders = new Map();
    this.folderSources = new Map();
    this.folderDestinations = new Map();
    this.folderDownloads = new Map();
    this.folderRelayStarting = null;
    this.folderOfferOwners = new Map();
    this.folderConcurrencyUpdates = new Set();
    this.folderDirect = new FolderDirectTransfers(this);
    this.paused = false;
    this.paired = false;
    this.reconnectAttempt = 0;
    this.foreground = () => {
      if (globalThis.document?.visibilityState === "hidden" || this.closed)
        return;
      if (this.reconnecting) {
        clearTimeout(this.reconnectTimer);
        this.connectSocket(true);
      } else if (this.paired && this.socket?.readyState === WebSocket.OPEN) {
        // 恢复前台后先检查旧连接，不能只依赖可能尚未更新的 readyState。
        clearTimeout(this.checkTimer);
        try {
          this.send({ type: "check" });
        } catch {
          this.connectionLost();
          return;
        }
        this.checkTimer = setTimeout(() => this.connectionLost(), 3000);
      }
    };
  }

  debug(event, details = {}, warning = false) {
    if (!this.debugEnabled()) return;
    // 只记录选取的诊断字段；连接地址仅在开启调试时输出，完整信令、票据和用户内容不输出。
    console[warning ? "warn" : "debug"]("[CloudPaste P2P]", event, {
      role:
        this.member === 0 ? "创建者" : this.member === 1 ? "加入者" : "未配对",
      generation: this.peerGeneration,
      ...details,
    });
  }

  async connectionDetails(peer = this.peer) {
    if (this.closed || !peer?.getStats) return null;
    const stats = await peer.getStats();
    // 异步读取期间可能已重建或关闭连接，旧统计不能归入新的连接代次。
    if (this.closed || this.peer !== peer) return null;
    const reports = [...stats.values()];
    const transport = reports.find(
      (item) => item.type === "transport" && item.selectedCandidatePairId,
    );
    const pair = transport
      ? stats.get(transport.selectedCandidatePairId)
      : reports.find(
          (item) =>
            item.type === "candidate-pair" &&
            item.state === "succeeded" &&
            (item.selected || item.nominated),
        );
    if (!pair) return null;
    const endpoint = (candidate) => ({
      address: candidate?.address ?? candidate?.ip ?? null,
      port: candidate?.port ?? null,
      type: candidate?.candidateType ?? null,
      protocol: candidate?.protocol ?? null,
      relayProtocol: candidate?.relayProtocol ?? null,
    });
    return {
      local: endpoint(stats.get(pair.localCandidateId)),
      remote: endpoint(stats.get(pair.remoteCandidateId)),
      pairState: pair.state,
      roundTripMs: Number.isFinite(pair.currentRoundTripTime)
        ? Math.round(pair.currentRoundTripTime * 100000) / 100
        : null,
      dtlsState: transport?.dtlsState ?? null,
      dtlsCipher: transport?.dtlsCipher ?? null,
      // 这是整条链路的累计统计，与单个文件的已确认字节分别记录。
      bytesSent: pair.bytesSent ?? null,
      bytesReceived: pair.bytesReceived ?? null,
    };
  }

  async logConnectionDetails(peer = this.peer, display = false) {
    if (!display && !this.debugEnabled() && !this.auditEnabled) return;
    try {
      const details = await this.connectionDetails(peer);
      if (details && this.peer === peer) {
        this.directEndpoints = {
          local: { address: details.local.address, port: details.local.port },
          remote: {
            address: details.remote.address,
            port: details.remote.port,
          },
        };
        // 连接建立时只取一次界面快照，不要求开启日志，也不新增统计轮询。
        if (display && this.directReady)
          this.onEvent({ type: "connection-info", remote: details.remote });
        if (this.debugEnabled()) this.debug("直连链路详情", details);
      } else if (!this.closed && this.peer === peer) {
        if (display && this.directReady)
          this.onEvent({ type: "connection-info", remote: {} });
        if (this.debugEnabled()) this.debug("直连链路统计暂不可用");
      }
    } catch (error) {
      if (!this.closed && this.peer === peer) {
        if (display && this.directReady)
          this.onEvent({ type: "connection-info", remote: {} });
        this.debug("读取直连统计失败", { error: error.name }, true);
      }
    }
  }

  logTransferEnd(message) {
    const startedAt = this.transferTimings.get(message.id);
    this.transferTimings.delete(message.id);
    const elapsedMs =
      startedAt === undefined ? null : performance.now() - startedAt;
    const file = this.files.get(message.id);
    const bytes = message.bytes ?? file?.bytes ?? 0;
    this.debug(
      "文件传输结束",
      {
        mode: message.mode,
        state: message.state,
        direction: file ? (file.from === this.member ? "发送" : "接收") : null,
        fileBytes: file?.size ?? null,
        confirmedBytes: bytes,
        durationMs: elapsedMs === null ? null : Math.round(elapsedMs),
        averageMiBPerSecond:
          elapsedMs > 0
            ? Math.round((bytes / (1 << 20) / (elapsedMs / 1000)) * 100) / 100
            : null,
      },
      message.state === "failed",
    );
    if (message.mode === "p2p") void this.logConnectionDetails();
  }

  async open(kind, fields, stunOverride = null) {
    if (this.closed) return;
    // 临时配置只保存在本机页面，不上报或改变对方的 ICE 服务器。
    this.stunOverride =
      stunOverride === null ? null : parseSTUNServers(stunOverride);
    this.debug("申请连接", { action: kind });
    const device = await collectTransferDevice();
    if (this.closed) return;
    const data = await request(`/transfer/${kind}`, {
      method: "POST",
      body: { ...fields, device },
    });
    if (this.closed) return;
    this.member = data.member;
    this.maxFile = data.max_file_size;
    this.resumeToken = data.token;
    this.expiresAt = Date.parse(data.expires_at) || Infinity;
    this.onEvent({ type: "created", ...data, token: undefined });
    this.debug("连接申请成功");
    globalThis.document?.addEventListener("visibilitychange", this.foreground);
    globalThis.window?.addEventListener?.("online", this.foreground);
    this.connectSocket();
  }

  connectSocket(resume = false) {
    if (this.closed || (resume && !this.paired)) return;
    if (resume && globalThis.document?.visibilityState === "hidden") return;
    if (resume && Date.now() >= this.reconnectUntil) {
      this.fail("超过 5 分钟未恢复连接，请重新配对");
      return;
    }
    // 丢弃旧套接字事件，迟到的 close 不能关闭恢复后的连接。
    const previous = this.socket;
    this.socket = null;
    previous?.close();
    const url = new URL(apiURL("/transfer/socket", true));
    url.protocol = url.protocol === "https:" ? "wss:" : "ws:";
    const socket = new WebSocket(url);
    this.socket = socket;
    // 临时凭证只通过首条消息提交，不写入 URL 或浏览器存储；公网应部署 HTTPS/WSS。
    socket.onopen = () => {
      if (this.socket !== socket || this.closed) return;
      this.debug("WebSocket 已连接");
      socket.send(
        JSON.stringify({
          type: "auth",
          token: this.resumeToken,
          ...(resume ? { resume: true } : {}),
        }),
      );
    };
    socket.onmessage = (event) => {
      if (this.socket !== socket || this.closed) return;
      try {
        this.handle(JSON.parse(event.data));
      } catch {
        this.debug("WebSocket 消息处理失败", {}, true);
        this.fail("连接消息无效，请重新配对");
      }
    };
    socket.onerror = () => {
      if (this.socket !== socket || this.closed) return;
      this.debug("WebSocket 连接异常", {}, true);
      if (this.paired) return;
      this.onEvent({
        type: "error",
        message: "连接失败，请检查服务和 WebSocket 代理配置",
      });
    };
    socket.onclose = (event) => {
      if (this.socket !== socket || this.closed) return;
      this.debug("WebSocket 已断开", {
        code: event.code,
        clean: event.wasClean,
      });
      if (this.paired && event.code !== 1008) this.connectionLost();
      else this.close();
    };
    clearTimeout(this.connectTimer);
    // 建连或认证无响应也必须受限，避免半开连接阻止后续重试。
    if (resume)
      this.connectTimer = setTimeout(() => {
        if (this.socket === socket) this.connectionLost();
      }, 12000);
  }

  connectionLost() {
    if (this.closed || !this.paired) return;
    clearTimeout(this.checkTimer);
    clearTimeout(this.connectTimer);
    const socket = this.socket;
    this.socket = null;
    socket?.close();
    if (!this.reconnecting) {
      this.reconnectUntil = Math.min(
        this.paused ? this.reconnectUntil : Infinity,
        Date.now() + reconnectWindow,
        this.expiresAt,
      );
      this.reconnecting = true;
      this.pauseTransfers();
      this.onEvent({
        type: "reconnecting",
        reconnect_until: new Date(this.reconnectUntil).toISOString(),
      });
      this.armReconnectExpiry();
    }
    clearTimeout(this.reconnectTimer);
    if (Date.now() >= this.reconnectUntil) {
      this.fail("超过 5 分钟未恢复连接，请重新配对");
      return;
    }
    if (globalThis.document?.visibilityState !== "hidden") {
      const delay = Math.min(
        10000,
        1000 * 2 ** Math.min(this.reconnectAttempt++, 4),
      );
      this.reconnectTimer = setTimeout(() => this.connectSocket(true), delay);
    }
  }

  armReconnectExpiry() {
    clearTimeout(this.expiryTimer);
    this.expiryTimer = setTimeout(
      () => this.fail("连接恢复超时，请重新配对"),
      Math.max(0, this.reconnectUntil - Date.now()),
    );
  }

  pauseTransfers() {
    if (this.paused) return;
    this.paused = true;
    const peer = this.peer,
      channel = this.channel;
    this.peer = this.channel = null;
    this.peerSettings = null;
    this.directReady = false;
    this.directEndpoints = null;
    this.peerGeneration++;
    this.candidates = [];
    this.signalChain = Promise.resolve();
    this.folderDirect.close();
    channel?.close();
    peer?.close();
    this.onEvent({ type: "direct", ready: false });
    // 取消实际传输和写盘，不缓存后台文件；普通 File 引用保留供用户手动重发。
    for (const folder of this.folders.values())
      if (["active", "offered"].includes(folder.state))
        this.handle({
          type: "folder-state",
          id: folder.id,
          state: "failed",
          mode: folder.mode,
        });
    for (const file of [...this.files.values()])
      if (["active", "offered"].includes(file.state))
        this.handle({
          type: "state",
          id: file.id,
          state: "failed",
          mode: file.mode,
        });
    for (const id of this.pendingOffers) this.sources.delete(id);
    this.pendingOffers.clear();
    this.folderOfferOwners.clear();
    this.folderConcurrencyUpdates.clear();
  }

  send(message) {
    if (this.paused && !["check", "leave"].includes(message.type))
      throw new Error("连接暂时中断，请等待恢复后再发送");
    if (this.socket?.readyState !== WebSocket.OPEN)
      throw new Error("连接尚未就绪");
    this.socket.send(JSON.stringify(message));
  }

  handle(message) {
    if (message.type === "checked") {
      clearTimeout(this.checkTimer);
      return;
    }
    if (message.type === "ready" && message.paired) {
      clearTimeout(this.connectTimer);
      return;
    }
    if (message.type === "sync") {
      for (const state of [...message.folders, ...message.files])
        this.handle(state);
      return;
    }
    if (message.type === "peer-offline") {
      clearTimeout(this.connectTimer);
      this.reconnecting = false;
      clearTimeout(this.reconnectTimer);
      this.pauseTransfers();
      this.reconnectUntil = Math.min(
        Date.parse(message.reconnect_until),
        this.expiresAt,
      );
      this.armReconnectExpiry();
      this.onEvent(message);
      return;
    }
    if (message.type === "paired") {
      if (
        this.paired &&
        !this.paused &&
        this.connectionGeneration === (message.generation ?? 0)
      )
        return;
      clearTimeout(this.reconnectTimer);
      clearTimeout(this.connectTimer);
      clearTimeout(this.checkTimer);
      clearTimeout(this.expiryTimer);
      this.paired = true;
      this.paused = this.reconnecting = false;
      this.reconnectAttempt = 0;
      this.connectionGeneration = message.generation ?? 0;
      this.peerGeneration = this.connectionGeneration * 1000000;
    }
    if (message.type === "relay-ready") {
      // 上传实际接入后才放行下一对，避免同一浏览器的下载占满共享连接池。
      if (this.folderRelayStarting?.id === message.id) {
        this.folderRelayStarting = null;
        this.startNextFolderRelay();
      }
      return;
    }
    if (message.type === "folder-reset") {
      const folder = this.folders.get(message.id);
      const source = this.folderSources.get(folder?.client_id);
      // 清除尚未使用的额度；确认之后发出的 offer 只使用服务端重新分配的额度。
      if (source) source.credits = 0;
      this.send({ type: "folder-ready", id: message.id });
      return;
    }
    if (message.type === "folder-concurrency") {
      this.folderConcurrencyUpdates.delete(message.id);
      const folder = this.folders.get(message.id);
      if (folder) folder.concurrency = message.concurrency;
      this.onEvent(message);
      return;
    }
    if (message.type === "error" && message.action === "folder-concurrency") {
      this.folderConcurrencyUpdates.delete(message.id);
      // 调整失败只提示错误，不取消已经在传输的文件。
      this.onEvent({
        type: "folder-concurrency",
        id: message.id,
        error: message.message,
      });
      return;
    }
    if (message.type === "folder") {
      this.folders.set(message.id, message);
      this.onEvent(message);
      return;
    }
    if (message.type === "folder-next") {
      this.sendFolderEntry(message.id, message.count || 1).catch((error) =>
        this.folderFailed(message.id, error),
      );
      return;
    }
    if (message.type === "folder-state") {
      const folder = this.folders.get(message.id);
      if (!folder) return;
      Object.assign(folder, message);
      if (["complete", "failed", "cancelled"].includes(message.state)) {
        this.folderConcurrencyUpdates.delete(folder.id);
        this.folderSources.get(folder.client_id)?.controller.abort();
        this.folderSources.delete(folder.client_id);
        this.stopFolderDownloads(folder.id);
        this.folderDestinations.delete(folder.id);
      }
      this.onEvent(message);
      return;
    }
    if (message.type === "folder-progress") {
      for (const entry of message.progress || [])
        this.handle({ type: "progress", ...entry });
      return;
    }
    // 接收端已按本地写盘进度更新，忽略服务器回传的同一份进度。
    if (
      message.type === "progress" &&
      (this.receiving?.id === message.id ||
        this.folderDirect.hasReceiver(message.id))
    )
      return;
    if (message.type === "paired") this.startPeer(message);
    if (message.type === "signal" && !this.paused) {
      this.signalChain = this.signalChain
        .then(() => this.signal(message.data))
        .catch((error) => {
          if ((message.data?.generation ?? 0) === this.peerGeneration) {
            this.debug("WebRTC 信令处理失败", { error: error.name }, true);
            this.peerFailed("信令协商失败");
          }
        });
    }
    if (message.type === "file") {
      // 新任务确认后才释放旧任务的文件引用，失败时仍可再次尝试。
      const previous = [...this.files.values()].find(
        (file) =>
          message.from === this.member &&
          file.retryClientID &&
          file.retryClientID === message.client_id,
      );
      if (previous) {
        previous.retried = true;
        this.sources.delete(previous.client_id);
        this.onEvent({
          type: "retry",
          id: previous.id,
          pending: false,
          retried: true,
        });
      }
      this.folderOfferOwners.delete(message.client_id);
      this.files.set(message.id, message);
      if (message.from === this.member)
        this.pendingOffers.delete(message.client_id);
      if (message.image_type && message.from === this.member) {
        const blob = this.sources.get(message.client_id);
        if (blob)
          queueMicrotask(() => {
            if (!this.closed)
              this.onEvent({ type: "image", id: message.id, blob });
          });
      }
    }
    if (message.type === "error") {
      this.debug("服务端拒绝互传请求", {}, true);
      this.abortDirect(message.id);
      this.pendingOffers.delete(message.client_id);
      this.sources.delete(message.client_id);
      const folderSource = this.folderSources.get(message.client_id);
      if (folderSource) {
        folderSource.controller.abort();
        this.folderSources.delete(message.client_id);
      }
      const folderID =
        this.files.get(message.id)?.folder?.id ||
        (this.folders.has(message.id) ? message.id : null) ||
        this.folderOfferOwners.get(message.client_id);
      this.folderOfferOwners.delete(message.client_id);
      if (folderID) this.folderFailed(folderID, new Error(message.message));
      if (message.id && this.autoAccepting === message.id) {
        this.autoAccepting = null;
        const file = this.files.get(message.id);
        if (file) file.autoBlocked = true;
        // 自动接收被拒绝时结束该任务，发送方可以手动重发，不无限重试占用资源。
        this.send({ type: "cancel", id: message.id });
      }
      for (const file of this.files.values()) {
        if (file.retryClientID !== message.client_id || !message.client_id)
          continue;
        delete file.retryClientID;
        this.onEvent({ type: "retry", id: file.id, pending: false });
      }
    }
    if (message.type === "state" && this.files.has(message.id)) {
      Object.assign(this.files.get(message.id), message);
      if (
        message.state === "active" &&
        this.debugEnabled() &&
        !this.transferTimings.has(message.id)
      ) {
        this.transferTimings.set(message.id, performance.now());
        this.debug("文件传输已开始", {
          mode: message.mode,
          fileBytes: this.files.get(message.id).size,
        });
      }
    }
    if (message.type === "start") {
      this.debug("开始发送文件", { mode: message.mode });
      const task =
        message.mode === "relay"
          ? this.upload(message)
          : this.files.get(message.id)?.folder
            ? this.folderDirect.start(
                this.files.get(message.id),
                this.sources.get(this.files.get(message.id).client_id),
              )
            : this.sendDirect(message.id);
      task.catch((error) => {
        const folder = this.files.get(message.id)?.folder;
        if (folder) this.folderFailed(folder.id, error);
        else this.fileFailed(message.id, error);
      });
    }
    if (message.type === "range-start") {
      this.uploadRange(message).catch((error) => {
        // 外部下载器自行重试该分段，不能把一次探测中断当作整个任务失败。
        if (
          !this.closed &&
          error.name !== "AbortError" &&
          this.files.get(message.id)?.state === "active"
        ) {
          try {
            this.send({
              type: "range-failed",
              id: message.id,
              request_id: message.request_id,
            });
          } catch {}
          this.onEvent({ type: "error", message: error.message });
        }
      });
    }
    if (message.type === "range-stop") {
      const upload = this.rangeUploads.get(message.request_id);
      if (upload?.id === message.id) {
        upload.controller.abort();
        this.rangeUploads.delete(message.request_id);
      }
    }
    if (message.type === "download" && this.files.get(message.id)?.image_type) {
      this.receiveImage(message).catch((error) =>
        this.fileFailed(message.id, error),
      );
      return;
    }
    if (message.type === "download" && this.files.get(message.id)?.folder) {
      this.queueFolderRelay(message);
      return;
    }
    if (
      message.type === "state" &&
      ["complete", "failed", "cancelled"].includes(message.state)
    ) {
      this.logTransferEnd(message);
      this.uploads.get(message.id)?.abort();
      this.uploads.delete(message.id);
      for (const upload of this.rangeUploads.values()) {
        if (upload.id === message.id) upload.controller.abort();
      }
      if (message.state !== "complete")
        this.imageDownloads.get(message.id)?.abort();
      if (
        this.autoAccepting === message.id &&
        !this.imageDownloads.has(message.id)
      )
        this.autoAccepting = null;
      this.abortDirect(message.id);
      this.folderDirect.remove(message.id);
      this.stopFolderDownload(message.id);
      // 取消后重建直连，旧通道及其残留分块不进入下一次接收。
      if (message.mode === "p2p" && message.state !== "complete") {
        const file = this.files.get(message.id);
        if (file && !file.folder && !file.directReset) {
          file.directReset = true;
          this.restartPeer();
        }
      }
      const file = this.files.get(message.id);
      // File 仅保留本地引用用于重发，不把整个文件读入内存；完成、移除或离开时释放。
      if (file && message.state === "complete")
        this.sources.delete(file.client_id);
    }
    this.onEvent(message);
    if (
      message.type === "file" &&
      message.folder &&
      message.from !== this.member
    ) {
      this.receiveFolderEntry(message).catch((error) =>
        this.folderFailed(message.folder.id, error),
      );
    }
    if (
      message.type === "state" &&
      ["complete", "failed", "cancelled"].includes(message.state)
    ) {
      const file = this.files.get(message.id);
      if (file?.folder) {
        this.sources.delete(file.client_id);
        this.files.delete(message.id);
      }
    }
    this.receiveNextAutoFile();
  }

  offerFolder(folder) {
    if (
      !Number.isInteger(folder.count) ||
      folder.count < 1 ||
      folder.count > folderMaxEntries
    )
      throw new Error(`文件夹最多包含 ${folderMaxEntries} 个文件和空目录`);
    if (this.folders.size + this.files.size + this.pendingOffers.size >= 32)
      throw new Error("本次连接的任务额度已用完");
    const id = transferID();
    this.folderSources.set(id, {
      folder,
      controller: new AbortController(),
      lastEntry: 0,
    });
    try {
      this.send({
        type: "folder-offer",
        client_id: id,
        name: folder.name,
        count: folder.count,
        size: folder.size,
      });
    } catch (error) {
      this.folderSources.delete(id);
      throw error;
    }
  }

  async sendFolderEntry(id, credits) {
    const folder = this.folders.get(id);
    const source = this.folderSources.get(folder?.client_id);
    if (!source || folder.state !== "active") return;
    if (
      !Number.isInteger(credits) ||
      credits < 1 ||
      credits > folderMaxConcurrency
    )
      throw new Error("文件夹队列额度无效");
    source.credits = (source.credits || 0) + credits;
    if (source.running) return;
    source.running = true;
    // 服务端额度已包含并发保护，完成即补位，不再人为等待固定间隔。
    try {
      const entries = source.folder.entries();
      source.iterator ??=
        entries[Symbol.asyncIterator]?.() || entries[Symbol.iterator]();
      while (source.credits > 0) {
        source.controller.signal.throwIfAborted();
        if (!source.nextEntry) {
          const { value, done } = await source.iterator.next();
          if (done) throw new Error("文件夹内容已变化，请重新选择并发送");
          source.nextEntry = value;
        }
        // 调整并发时，异步枚举可能刚好返回；保留该条目，等待新额度，不能跳过文件。
        if (source.credits <= 0) break;
        const entry = source.nextEntry;
        folderPath(entry.path, entry.directory);
        source.controller.signal.throwIfAborted();
        source.credits--;
        this.offer([entry.file], false, {
          id,
          path: entry.path,
          directory: entry.directory,
        });
        source.nextEntry = null;
      }
    } finally {
      source.running = false;
    }
  }

  async acceptFolder(
    id,
    direct,
    concurrency = this.folders.get(id)?.concurrency || folderDefaultConcurrency,
  ) {
    if (
      !Number.isInteger(Number(concurrency)) ||
      Number(concurrency) < 1 ||
      Number(concurrency) > folderMaxConcurrency
    )
      throw new Error("文件夹并发数应为 1–128");
    if (!canSaveFolders())
      throw new Error("接收文件夹需要 HTTPS 和支持选择保存目录的浏览器");
    const folder = this.folders.get(id);
    if (!folder || folder.from === this.member || folder.state !== "offered")
      throw new Error("文件夹任务不可用");
    if (
      this.accepting ||
      this.receiving ||
      this.folderDestinations.size ||
      [...this.files.values()].some((file) => file.state === "active")
    )
      throw new Error("请先完成当前接收操作");
    if (direct && !this.directReady) throw new Error("点对点连接尚未就绪");
    this.accepting = true;
    try {
      const parent = await window.showDirectoryPicker({ mode: "readwrite" });
      if (this.closed || folder.state !== "offered")
        throw new Error("连接或文件夹任务已结束");
      const root = await createFolderDestination(parent, folder.name);
      if (this.closed || folder.state !== "offered")
        throw new Error("连接或文件夹任务已结束");
      this.folderDestinations.set(id, {
        root,
        direct,
        preparing: Promise.resolve(),
      });
      this.send({
        type: "accept-folder",
        id,
        mode: direct ? "p2p" : "relay",
        concurrency: Number(concurrency),
      });
    } catch (error) {
      this.folderDestinations.delete(id);
      throw error;
    } finally {
      this.accepting = false;
    }
  }

  updateFolderConcurrency(id, concurrency) {
    const value = Number(concurrency);
    if (!Number.isInteger(value) || value < 1 || value > folderMaxConcurrency)
      throw new Error("文件夹并发数应为 1–128");
    const folder = this.folders.get(id);
    if (
      this.closed ||
      !folder ||
      folder.from === this.member ||
      folder.state !== "active"
    )
      throw new Error("当前文件夹不可调整并发数");
    if (this.folderConcurrencyUpdates.has(id))
      throw new Error("正在应用并发设置");
    this.folderConcurrencyUpdates.add(id);
    try {
      this.send({ type: "folder-concurrency", id, concurrency: value });
    } catch (error) {
      this.folderConcurrencyUpdates.delete(id);
      throw error;
    }
  }

  async receiveFolderEntry(file) {
    const destination = this.folderDestinations.get(file.folder.id);
    if (!destination) throw new Error("请先选择文件夹保存目录");
    // 路径检查与创建串行，避免同名目标竞争；文件内容在各自的流中并行写入。
    const preparing = destination.preparing.then(() => {
      if (this.closed || this.folders.get(file.folder.id)?.state !== "active")
        return null;
      return createFolderEntry(
        destination.root,
        file.folder.path,
        file.folder.directory,
      );
    });
    destination.preparing = preparing.catch(() => {});
    const handle = await preparing;
    if (this.closed || this.folders.get(file.folder.id)?.state !== "active")
      return;
    if (file.folder.directory) {
      this.send({ type: "accept", id: file.id, mode: "directory" });
      return;
    }
    const writer = await handle.createWritable();
    if (this.closed || this.folders.get(file.folder.id)?.state !== "active") {
      await writer.abort();
      return;
    }
    try {
      if (destination.direct) this.folderDirect.prepare(file, writer);
      else
        this.folderDownloads.set(file.id, {
          id: file.id,
          folder: file.folder.id,
          writer,
          controller: new AbortController(),
        });
      this.send({
        type: "accept",
        id: file.id,
        mode: destination.direct ? "p2p" : "relay",
      });
    } catch (error) {
      this.folderDirect.remove(file.id);
      this.stopFolderDownload(file.id);
      await writer.abort().catch(() => {});
      throw error;
    }
  }

  queueFolderRelay(message) {
    const job = this.folderDownloads.get(message.id);
    if (!job || job.download || job.controller.signal.aborted) return;
    job.download = message;
    this.startNextFolderRelay();
  }

  startNextFolderRelay() {
    if (this.closed || this.folderRelayStarting) return;
    for (const job of this.folderDownloads.values()) {
      if (
        !job.download ||
        job.started ||
        job.controller.signal.aborted ||
        this.folders.get(job.folder)?.state !== "active"
      )
        continue;
      job.started = true;
      this.folderRelayStarting = job;
      // 只串行配对启动，已接入的文件继续并行传输，不等待前一个文件完成。
      this.receiveFolderRelay(job.download).catch((error) =>
        this.folderFailed(job.folder, error),
      );
      return;
    }
  }

  async receiveFolderRelay(message) {
    const job = this.folderDownloads.get(message.id);
    const file = this.files.get(message.id);
    if (!job || job.id !== file?.id) throw new Error("文件夹接收状态无效");
    let reader;
    try {
      const response = await fetch(this.downloadURL(message.url), {
        signal: job.controller.signal,
        cache: "no-store",
        referrerPolicy: "no-referrer",
      });
      if (!response.ok || !response.body) throw new Error("文件夹中继接收失败");
      reader = response.body.getReader();
      let bytes = 0;
      for (;;) {
        const { value, done } = await reader.read();
        if (done) break;
        bytes += value.byteLength;
        if (bytes > file.size) throw new Error("文件长度不一致");
        // 每次写盘完成后再读下一块，保持接收背压，不聚合为 Blob。
        await job.writer.write(value);
        this.onEvent({ type: "progress", id: file.id, bytes });
      }
      if (bytes !== file.size) throw new Error("文件长度不一致");
      await job.writer.close();
      if (this.folderDownloads.get(job.id) === job)
        this.folderDownloads.delete(job.id);
      if (!this.closed && !job.controller.signal.aborted)
        this.send({ type: "complete", id: file.id, bytes });
    } catch (error) {
      await reader?.cancel().catch(() => {});
      await job.writer.abort().catch(() => {});
      throw error;
    } finally {
      if (this.folderDownloads.get(job.id) === job)
        this.folderDownloads.delete(job.id);
    }
  }

  stopFolderDownload(id) {
    if (this.folderRelayStarting?.id === id) this.folderRelayStarting = null;
    const job = this.folderDownloads.get(id);
    if (!job) return;
    this.folderDownloads.delete(id);
    job.controller.abort();
    job.writer.abort().catch(() => {});
  }

  stopFolderDownloads(id) {
    if (this.folderRelayStarting?.folder === id)
      this.folderRelayStarting = null;
    this.folderDirect.cancelFolder(id);
    for (const job of this.folderDownloads.values())
      if (job.folder === id) this.stopFolderDownload(job.id);
    for (const [clientID, folderID] of this.folderOfferOwners)
      if (folderID === id) {
        this.folderOfferOwners.delete(clientID);
        this.sources.delete(clientID);
        this.pendingOffers.delete(clientID);
      }
  }

  cancelFolder(id) {
    const folder = this.folders.get(id);
    this.folderSources.get(folder?.client_id)?.controller.abort();
    this.stopFolderDownloads(id);
    this.send({ type: "cancel-folder", id });
  }

  folderFailed(id, error) {
    if (this.closed || !id) return;
    if (error.name !== "AbortError")
      this.onEvent({ type: "error", message: error.message });
    const folder = this.folders.get(id);
    if (folder && ["offered", "active"].includes(folder.state)) {
      try {
        this.cancelFolder(id);
      } catch {}
    }
  }

  offer(files, images = false, folder = null) {
    if (
      !folder &&
      this.files.size +
        this.pendingOffers.size +
        this.folders.size +
        files.length >
        32
    )
      throw new Error("每次连接最多发送 32 个文件");
    for (const file of files) {
      if (/[\p{Cc}\p{Cf}]/u.test(file.name))
        throw new Error("文件名包含不支持的控制或格式字符");
      if (file.size > this.maxFile)
        throw new Error(`${file.name} 超过单文件大小限制`);
      if (
        images &&
        (file.size > chatImageLimit ||
          !["image/png", "image/jpeg"].includes(file.type))
      )
        throw new Error("聊天图片仅支持不超过 4 MiB 的 PNG / JPEG");
    }
    const offered = [];
    for (const file of files) {
      const id = transferID();
      this.send({
        type: "offer",
        client_id: id,
        name: file.name,
        size: file.size,
        ...(images ? { image_type: file.type } : {}),
        ...(folder ? { folder } : {}),
      });
      this.sources.set(id, file);
      if (folder) this.folderOfferOwners.set(id, folder.id);
      this.pendingOffers.add(id);
      offered.push(id);
    }
    return offered;
  }

  retry(id) {
    const file = this.files.get(id);
    const source = this.sources.get(file?.client_id);
    if (
      this.closed ||
      !file ||
      file.from !== this.member ||
      !source ||
      !["failed", "cancelled"].includes(file.state) ||
      file.retryClientID ||
      file.retried
    )
      throw new Error("该任务无法重新发送，请重新选择文件");
    // 重发创建独立任务并继续计入会话额度，不复用旧下载票据；普通文件仍需接收方确认。
    file.retryClientID = this.offer([source], Boolean(file.image_type))[0];
    this.onEvent({ type: "retry", id, pending: true });
  }

  forgetSource(id) {
    const file = this.files.get(id);
    if (file && ["complete", "failed", "cancelled"].includes(file.state))
      this.sources.delete(file.client_id);
  }

  receiveNextAutoFile() {
    if (
      this.closed ||
      this.paused ||
      this.autoAccepting ||
      this.accepting ||
      this.receiving ||
      this.folderDestinations.size ||
      [...this.files.values()].some((file) => file.state === "active")
    )
      return;
    const file = [...this.files.values()].find(
      (file) =>
        (file.image_type || this.autoReceive) &&
        !file.folder &&
        !file.autoBlocked &&
        file.from !== this.member &&
        file.state === "offered",
    );
    if (!file) return;
    this.autoAccepting = file.id;
    this.accept(file, false).catch((error) => {
      this.autoAccepting = null;
      this.fileFailed(file.id, error);
    });
  }

  setAutoReceive(enabled) {
    // 仅保存本次连接的接收同意；关闭只影响后续任务，不中断正在下载的文件。
    this.autoReceive = Boolean(enabled);
    this.receiveNextAutoFile();
  }

  async receiveImage(message) {
    const file = this.files.get(message.id);
    const controller = new AbortController();
    this.imageDownloads.set(file.id, controller);
    try {
      if (file.size <= 0 || file.size > chatImageLimit)
        throw new Error("聊天图片超过大小限制");
      const response = await fetch(this.downloadURL(message.url), {
        signal: controller.signal,
        cache: "no-store",
        referrerPolicy: "no-referrer",
      });
      if (
        !response.ok ||
        Number(response.headers.get("Content-Length")) !== file.size
      )
        throw new Error("图片接收失败");
      // 只有严格限额的聊天图片才聚合为 Blob；普通大文件仍走原生流式下载。
      const blob = new Blob([await response.blob()], { type: file.image_type });
      if (blob.size !== file.size) throw new Error("图片长度不一致");
      await validateChatImage(blob);
      if (!this.closed && !controller.signal.aborted)
        this.onEvent({ type: "image", id: file.id, blob });
    } catch (error) {
      if (!this.closed && error.name !== "AbortError")
        this.onEvent({
          type: "image-error",
          id: file.id,
          message: error.message,
        });
      throw error;
    } finally {
      this.imageDownloads.delete(file.id);
      if (this.autoAccepting === file.id) this.autoAccepting = null;
      this.receiveNextAutoFile();
    }
  }

  async accept(file, direct, useDownloadTool = false) {
    if (
      this.accepting ||
      this.receiving ||
      (this.folderDestinations.size && !file.folder) ||
      (this.autoAccepting && this.autoAccepting !== file.id)
    )
      throw new Error("请先完成当前接收操作");
    this.accepting = true;
    try {
      if (direct) {
        if (!this.directReady || !canSaveDirectly())
          throw new Error("当前浏览器无法直连保存，请使用中继接收");
        // 文件选择器必须直接由用户点击触发，写盘完成后才确认该块已接收。
        const handle = await window.showSaveFilePicker({
          suggestedName: file.name,
        });
        const writer = await handle.createWritable();
        if (this.closed || !this.directReady) {
          await writer.abort();
          throw new Error("连接已断开");
        }
        this.receiving = {
          id: file.id,
          size: file.size,
          bytes: 0,
          queued: 0,
          received: 0,
          chunks: [],
          pendingBytes: 0,
          writer,
          chain: Promise.resolve(),
          lastProgress: 0,
          lastUI: 0,
        };
      }
      try {
        this.debug("选择接收方式", {
          mode: useDownloadTool ? "tool" : direct ? "p2p" : "relay",
          bytes: file.size,
        });
        this.send({
          type: "accept",
          id: file.id,
          mode: useDownloadTool ? "tool" : direct ? "p2p" : "relay",
        });
      } catch (error) {
        this.abortDirect(file.id);
        throw error;
      }
    } finally {
      this.accepting = false;
    }
  }

  downloadURL(path) {
    const url = new URL(path, backendBase + "/");
    if (
      url.origin !== new URL(backendBase).origin ||
      !url.pathname.startsWith("/api/transfer/download/")
    )
      throw new Error("下载地址无效");
    return url.href;
  }

  async upload(message) {
    const file = this.files.get(message.id);
    const source = this.sources.get(file?.client_id);
    if (!source) throw new Error("原文件已不可用，请重新选择");
    const controller = new AbortController();
    this.uploads.set(message.id, controller);
    // File 交给浏览器发送；不调用 arrayBuffer/blob 聚合整个文件。
    const response = await fetch(
      apiURL(`/transfer/upload/${message.id}`, true),
      {
        method: "PUT",
        headers: {
          Authorization: `Transfer ${message.token}`,
          "Content-Type": "application/octet-stream",
        },
        body: source,
        signal: controller.signal,
        cache: "no-store",
      },
    );
    if (!response.ok)
      throw new Error((await response.json()).message || "发送中断");
    this.uploads.delete(message.id);
  }

  async uploadRange(message) {
    const file = this.files.get(message.id);
    const source = this.sources.get(file?.client_id);
    if (!source) throw new Error("原文件已不可用，请重新选择");
    if (
      !Number.isSafeInteger(message.start) ||
      !Number.isSafeInteger(message.end) ||
      message.start < 0 ||
      message.end < message.start ||
      message.end > source.size ||
      !/^[A-Za-z0-9_-]{1,64}$/.test(message.request_id) ||
      this.rangeUploads.size >= 4 ||
      this.rangeUploads.has(message.request_id)
    )
      throw new Error("下载分段请求无效或并发过多");
    const controller = new AbortController();
    this.rangeUploads.set(message.request_id, { id: message.id, controller });
    try {
      // slice 只引用所需文件区间，浏览器负责流式上传，大文件不会整体读入内存。
      const response = await fetch(
        apiURL(
          `/transfer/upload/${encodeURIComponent(message.id)}/${message.request_id}`,
          true,
        ),
        {
          method: "PUT",
          headers: {
            Authorization: `Transfer ${message.token}`,
            "Content-Type": "application/octet-stream",
          },
          body: source.slice(message.start, message.end),
          signal: controller.signal,
          cache: "no-store",
        },
      );
      if (!response.ok) throw new Error("分段发送中断，下载工具可重试");
    } finally {
      this.rangeUploads.delete(message.request_id);
    }
  }

  restartPeer() {
    if (this.closed || this.paused || !this.peerSettings) return;
    this.debug("重建 WebRTC 连接");
    this.folderDirect.close();
    this.directReady = false;
    this.onEvent({ type: "direct", ready: false });
    const peer = this.peer,
      channel = this.channel;
    // 先切断旧事件与当前连接的关联，再关闭资源，避免迟到的关闭事件破坏新连接。
    this.peer = null;
    this.channel = null;
    this.directEndpoints = null;
    this.candidates = [];
    this.signalChain = Promise.resolve();
    this.peerGeneration++;
    channel?.close();
    peer?.close();
    this.startPeer(this.peerSettings);
  }

  startPeer(message) {
    this.peerSettings = message;
    this.auditEnabled = message.audit_enabled === true;
    if (message.relay_only || typeof RTCPeerConnection === "undefined") {
      this.debug("使用中继连接", {
        reason: message.relay_only ? "仅中继模式" : "浏览器不支持 WebRTC",
      });
      return;
    }
    const stunServers =
      this.stunOverride ??
      parseSTUNServers(
        message.stun_servers
          ? message.stun_servers.join("\n")
          : message.stun || "",
      );
    this.debug("开始 WebRTC 协商", {
      stunEnabled: stunServers.length > 0,
      stunServerCount: stunServers.length,
    });
    const generation = this.peerGeneration;
    const peer = new RTCPeerConnection({
      iceServers: stunServers.map((url) => ({ urls: url })),
    });
    this.peer = peer;
    this.peerStartedAt = performance.now();
    peer.onicecandidate = (event) => {
      if (!this.closed && this.peer === peer) {
        if (event.candidate)
          this.debug("发现本地 ICE 候选", {
            type: event.candidate.type,
            protocol: event.candidate.protocol,
            address: event.candidate.address ?? null,
            port: event.candidate.port ?? null,
          });
        else this.debug("ICE 候选收集完成");
        this.send({
          type: "signal",
          data: { candidate: event.candidate?.toJSON() ?? null, generation },
        });
      }
    };
    // 按状态变化记录，避免定时轮询和逐个数据分块输出影响传输性能。
    peer.oniceconnectionstatechange = () => {
      if (!this.closed && this.peer === peer)
        this.debug("ICE 连接状态", { state: peer.iceConnectionState });
    };
    peer.onicegatheringstatechange = () => {
      if (!this.closed && this.peer === peer)
        this.debug("ICE 收集状态", { state: peer.iceGatheringState });
    };
    peer.onsignalingstatechange = () => {
      if (!this.closed && this.peer === peer)
        this.debug("WebRTC 信令状态", { state: peer.signalingState });
    };
    peer.onconnectionstatechange = () => {
      if (this.closed || this.peer !== peer) return;
      this.debug("WebRTC 连接状态", { state: peer.connectionState });
      if (
        this.peer === peer &&
        ["failed", "closed", "disconnected"].includes(peer.connectionState)
      )
        this.peerFailed(`连接状态为 ${peer.connectionState}`);
    };
    if (this.member === 0) {
      this.attachChannel(peer.createDataChannel("transfer", { ordered: true }));
      peer
        .createOffer()
        .then(async (offer) => {
          if (this.closed || this.peer !== peer) return;
          await peer.setLocalDescription(offer);
          if (!this.closed && this.peer === peer) {
            this.debug("发送 WebRTC offer");
            this.send({
              type: "signal",
              data: {
                description: peer.localDescription,
                generation,
                max_chunk_size: maxChunkSize,
                receive_window: receiveWindow,
              },
            });
          }
        })
        .catch((error) => {
          if (this.peer === peer) {
            this.debug("WebRTC offer 创建失败", { error: error.name }, true);
            this.peerFailed("offer 协商失败");
          }
        });
    }
    // 两端都可能发送文件夹；独立通道只绑定已授权的子任务。
    peer.ondatachannel = ({ channel }) => {
      if (this.closed || this.peer !== peer) channel.close();
      else if (channel.label.startsWith("folder:"))
        this.folderDirect.attach(channel);
      else if (channel.label === "transfer" && !this.channel)
        this.attachChannel(channel);
      else channel.close();
    };
  }

  async signal(data) {
    const peer = this.peer;
    // 双方在同一文件终态后递增代次；旧 SDP 和候选地址即使迟到也不会污染新连接。
    if (
      !peer ||
      this.closed ||
      !data ||
      (data.generation ?? 0) !== this.peerGeneration
    )
      return;
    if (data.description) {
      const expected = this.member === 0 ? "answer" : "offer";
      if (data.description.type !== expected || peer.remoteDescription)
        throw new Error("重复协商");
      // 旧页面只接收 64 KiB；应用层上限和 SCTP 上限都确认后，才能发送更大的分块。
      this.peerChunkSize =
        Number.isSafeInteger(data.max_chunk_size) &&
        data.max_chunk_size >= 16 * 1024
          ? Math.min(maxChunkSize, data.max_chunk_size)
          : legacyChunkSize;
      // 旧页面的接收窗口只有 2 MiB；发送方不能用自己的新上限压满对方的队列。
      this.peerReceiveWindow =
        Number.isSafeInteger(data.receive_window) &&
        data.receive_window >= maxChunkSize
          ? Math.min(receiveWindow, data.receive_window)
          : legacyReceiveWindow;
      await peer.setRemoteDescription(data.description);
      if (this.closed || this.peer !== peer) return;
      this.debug("已应用远端描述", { type: expected });
      this.debug("协商传输参数", {
        maxChunkBytes: this.peerChunkSize,
        receiveWindowBytes: this.peerReceiveWindow,
        sctpMaxMessageBytes: peer.sctp?.maxMessageSize ?? null,
      });
      for (const candidate of this.candidates)
        await peer.addIceCandidate(candidate);
      if (this.closed || this.peer !== peer) return;
      this.candidates = [];
      if (this.member === 1) {
        const answer = await peer.createAnswer();
        if (this.closed || this.peer !== peer) return;
        await peer.setLocalDescription(answer);
        if (this.closed || this.peer !== peer) return;
        this.debug("发送 WebRTC answer");
        this.send({
          type: "signal",
          data: {
            description: peer.localDescription,
            generation: this.peerGeneration,
            max_chunk_size: maxChunkSize,
            receive_window: receiveWindow,
          },
        });
      }
    } else if ("candidate" in data) {
      if (
        this.debugEnabled() &&
        data.candidate?.candidate &&
        typeof RTCIceCandidate !== "undefined"
      ) {
        try {
          const candidate = new RTCIceCandidate(data.candidate);
          this.debug("收到远端 ICE 候选", {
            type: candidate.type,
            protocol: candidate.protocol,
            address: candidate.address,
            port: candidate.port,
          });
        } catch (error) {
          // 诊断失败不能改变原有候选队列和连接流程，实际校验仍由浏览器执行。
          this.debug("无法读取远端候选详情", { error: error.name }, true);
        }
      }
      if (peer.remoteDescription) await peer.addIceCandidate(data.candidate);
      else if (this.candidates.length < 64)
        this.candidates.push(data.candidate);
      else throw new Error("候选地址过多");
    }
  }

  attachChannel(channel) {
    if (this.channel || channel.label !== "transfer") {
      channel.close();
      return;
    }
    this.channel = channel;
    channel.binaryType = "arraybuffer";
    channel.bufferedAmountLowThreshold = sendBufferLimit / 2;
    channel.onbufferedamountlow = () => {
      if (this.channel === channel) this.sending?.wake?.();
    };
    channel.onopen = () => {
      if (this.closed || this.channel !== channel) return;
      this.directReady = true;
      this.debug("数据通道已打开，直连可用", {
        negotiationMs: Math.round(performance.now() - this.peerStartedAt),
        channelID: channel.id,
        ordered: channel.ordered,
        protocol: channel.protocol,
        sctpMaxMessageBytes: this.peer?.sctp?.maxMessageSize ?? null,
      });
      // 只在通道建立和文件终态取统计快照，不定时轮询或逐块打印。
      void this.logConnectionDetails(this.peer, true);
      this.onEvent({ type: "direct", ready: true });
    };
    channel.onclose = () => {
      if (this.channel === channel) this.peerFailed("数据通道关闭");
    };
    channel.onerror = (event) => {
      if (this.channel !== channel || this.closed) return;
      this.debug(
        "数据通道异常",
        {
          error: event.error?.name,
          detail: event.error?.errorDetail,
          sctpCauseCode: event.error?.sctpCauseCode,
        },
        true,
      );
      this.peerFailed("数据通道异常");
    };
    channel.onmessage = (event) => {
      if (this.closed || this.channel !== channel) return;
      try {
        this.receiveDirect(event.data);
      } catch (error) {
        this.debug("直连数据处理失败", { error: error.name }, true);
        this.peerFailed("直连数据处理失败");
        this.onEvent({ type: "error", message: error.message });
      }
    };
  }

  async sendDirect(id) {
    const file = this.files.get(id);
    const source = this.sources.get(file?.client_id);
    if (!source || !this.directReady || this.sending)
      throw new Error("直连不可用，请取消后通过中继重新发送");
    const job = {
      id,
      sent: 0,
      acked: 0,
      cancelled: false,
      progress: Date.now(),
    };
    this.sending = job;
    // 双方支持时每批直接发送一条消息；待写盘数据不能超过对端声明的有界窗口。
    const negotiated = this.peer?.sctp?.maxMessageSize;
    const chunkSize = Math.min(
      maxChunkSize,
      this.peerChunkSize || legacyChunkSize,
      negotiated === 0 ? maxChunkSize : negotiated || 16 * 1024,
    );
    while (job.sent < source.size) {
      const batch = new Uint8Array(
        await source.slice(job.sent, job.sent + diskBatchSize).arrayBuffer(),
      );
      for (let offset = 0; offset < batch.byteLength; offset += chunkSize) {
        const bytes = batch.subarray(offset, offset + chunkSize);
        await this.waitWindow(job, bytes.byteLength);
        if (job.cancelled) throw new Error("传输已取消");
        this.channel.send(bytes);
        job.sent += bytes.byteLength;
      }
    }
    await this.waitWindow(job, 0, true);
    if (job.cancelled) throw new Error("传输已取消");
    this.channel.send(JSON.stringify({ type: "end", id }));
  }

  async waitWindow(job, next, drain = false) {
    const windowLimit = Math.min(
      receiveWindow,
      this.peerReceiveWindow || legacyReceiveWindow,
    );
    while (
      drain
        ? job.acked < job.sent
        : job.sent - job.acked + next > windowLimit ||
          this.channel?.bufferedAmount + next > sendBufferLimit
    ) {
      if (
        job.cancelled ||
        !this.directReady ||
        Date.now() - job.progress > 45000
      )
        throw new Error("接收停滞，请重新发送");
      // 写盘确认或网络缓冲下降立即唤醒；定时器只负责停滞超时，不轮询限速。
      await new Promise((resolve) => {
        const wake = () => {
          clearTimeout(timer);
          if (job.wake === wake) job.wake = null;
          resolve();
        };
        const timer = setTimeout(
          wake,
          Math.max(1, 45000 - (Date.now() - job.progress)),
        );
        job.wake = wake;
      });
    }
    if (job.cancelled || !this.directReady) throw new Error("直连已中断");
  }

  receiveDirect(data) {
    if (typeof data === "string") {
      if (data.length > 1024) throw new Error("直连控制消息过大");
      const message = JSON.parse(data);
      if (message.type === "ack") {
        // 完成通知走 WebSocket，可能先于最后一个 DataChannel 确认抵达。
        if (this.files.get(message.id)?.state === "complete") return;
        const job = this.sending;
        if (
          !job ||
          message.id !== job.id ||
          !Number.isSafeInteger(message.bytes) ||
          message.bytes < job.acked ||
          message.bytes > job.sent
        )
          throw new Error("接收确认无效");
        if (message.bytes > job.acked) job.progress = Date.now();
        job.acked = message.bytes;
        job.wake?.();
      } else if (message.type === "end") {
        const job = this.receiving;
        if (!job || message.id !== job.id || job.ending)
          throw new Error("文件结束信息无效");
        job.ending = true;
        job.chain = job.chain
          .then(async () => {
            if (job.cancelled) return;
            if (job.bytes !== job.size) throw new Error("文件长度不一致");
            await job.writer.close();
            // 关闭文件期间可能已取消并开始下一次接收，旧任务不能清空新任务。
            if (job.cancelled || this.receiving !== job) return;
            this.receiving = null;
            // 连接建立时已异步采集地址，完成通知直接使用缓存，不等待统计接口。
            this.send({
              type: "complete",
              id: job.id,
              bytes: job.bytes,
              connection: this.directEndpoints,
            });
          })
          .catch((error) => this.fileFailed(job.id, error));
      } else throw new Error("直连控制消息无效");
      return;
    }
    const job = this.receiving;
    if (
      !(data instanceof ArrayBuffer) ||
      !job ||
      job.ending ||
      data.byteLength === 0 ||
      data.byteLength > maxChunkSize ||
      job.queued + data.byteLength > receiveWindow ||
      job.received + data.byteLength > job.size
    )
      throw new Error("文件数据超过接收限制");
    job.queued += data.byteLength;
    job.received += data.byteLength;
    job.chunks.push(new Uint8Array(data));
    job.pendingBytes += data.byteLength;
    if (job.pendingBytes < diskBatchSize && job.received !== job.size) return;
    // 合并有界小块再写盘，减少文件系统调用；最后不足一批的数据也立即提交。
    const batch = new Uint8Array(job.pendingBytes);
    let offset = 0;
    for (const chunk of job.chunks) {
      batch.set(chunk, offset);
      offset += chunk.byteLength;
    }
    job.chunks = [];
    job.pendingBytes = 0;
    job.chain = job.chain
      .then(async () => {
        if (job.cancelled) return;
        await job.writer.write(batch);
        if (job.cancelled) return;
        job.queued -= batch.byteLength;
        job.bytes += batch.byteLength;
        this.channel.send(
          JSON.stringify({ type: "ack", id: job.id, bytes: job.bytes }),
        );
        if (Date.now() - job.lastProgress >= 1000) {
          this.send({ type: "progress", id: job.id, bytes: job.bytes });
          job.lastProgress = Date.now();
        }
        if (Date.now() - job.lastUI >= 100 || job.bytes === job.size) {
          this.onEvent({ type: "progress", id: job.id, bytes: job.bytes });
          job.lastUI = Date.now();
        }
      })
      .catch((error) => this.fileFailed(job.id, error));
  }

  abortDirect(id) {
    if (this.sending && this.sending.id === id) {
      this.sending.cancelled = true;
      this.sending.wake?.();
      this.sending = null;
    }
    if (this.receiving && this.receiving.id === id) {
      this.receiving.cancelled = true;
      this.receiving.chunks = [];
      this.receiving.writer.abort().catch(() => {});
      this.receiving = null;
    }
  }

  peerFailed(reason = "直连不可用") {
    if (this.closed) return;
    this.debug("直连不可用", { reason }, true);
    this.directReady = false;
    this.onEvent({ type: "direct", ready: false });
    for (const folder of this.folders.values())
      if (folder.state === "active" && folder.mode === "p2p")
        this.folderFailed(folder.id, new Error("直连中断，请重新发送文件夹"));
    this.folderDirect.close();
    const id = this.sending?.id || this.receiving?.id;
    if (id)
      this.fileFailed(id, new Error("直连中断，本次传输已停止，请重新发送"));
  }

  fileFailed(id, error) {
    // 终态可能先于请求的 abort 回调到达，避免把已完成或主动取消的任务改成失败。
    if (
      this.closed ||
      ["complete", "failed", "cancelled"].includes(this.files.get(id)?.state)
    )
      return;
    this.debug("文件传输异常", { error: error.name }, true);
    this.abortDirect(id);
    try {
      this.send({ type: "fail", id });
    } catch {}
    if (error.name !== "AbortError")
      this.onEvent({ type: "error", message: error.message });
  }

  fail(message) {
    this.onEvent({ type: "error", message });
    this.close();
  }

  close() {
    if (this.closed) return;
    // 主动离开立即销毁房间，不把用户退出当成可恢复的网络故障。
    try {
      if (this.socket?.readyState === WebSocket.OPEN)
        this.send({ type: "leave" });
    } catch {}
    this.debug("关闭连接并释放资源");
    this.closed = true;
    this.resumeToken = null;
    this.stunOverride = null;
    for (const timer of [
      this.reconnectTimer,
      this.connectTimer,
      this.checkTimer,
      this.expiryTimer,
    ])
      clearTimeout(timer);
    globalThis.document?.removeEventListener(
      "visibilitychange",
      this.foreground,
    );
    globalThis.window?.removeEventListener?.("online", this.foreground);
    this.autoReceive = false;
    for (const source of this.folderSources.values()) source.controller.abort();
    this.folderDirect.close();
    for (const id of this.folderDownloads.keys()) this.stopFolderDownload(id);
    this.folderRelayStarting = null;
    this.folderOfferOwners.clear();
    this.folderConcurrencyUpdates.clear();
    this.folderSources.clear();
    this.folderDestinations.clear();
    this.folders.clear();
    for (const controller of this.uploads.values()) controller.abort();
    for (const upload of this.rangeUploads.values()) upload.controller.abort();
    for (const controller of this.imageDownloads.values()) controller.abort();
    this.abortDirect(this.sending?.id);
    this.abortDirect(this.receiving?.id);
    this.channel?.close();
    this.peer?.close();
    this.socket?.close();
    this.sources.clear();
    this.files.clear();
    this.uploads.clear();
    this.rangeUploads.clear();
    this.transferTimings.clear();
    this.pendingOffers.clear();
    this.onEvent({ type: "closed" });
  }
}
