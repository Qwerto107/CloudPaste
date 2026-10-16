import { apiURL, backendBase, request } from "./api.js";
import { chatImageLimit, validateChatImage } from "./transfer-images.js";
import { collectTransferDevice } from "./transfer-device.js";

const maxChunkSize = 256 * 1024;
const legacyChunkSize = 64 * 1024;
const diskBatchSize = 256 * 1024;
// 放大有界窗口以减少等待；接收队列仍由写盘确认控制，不随文件总大小增长。
const receiveWindow = 8 * 1024 * 1024;
const legacyReceiveWindow = 2 * 1024 * 1024;
const sendBufferLimit = 4 * 1024 * 1024;

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

  async open(kind, fields) {
    if (this.closed) return;
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
    this.onEvent({ type: "created", ...data, token: undefined });
    this.debug("连接申请成功");
    const url = new URL(apiURL("/transfer/socket", true));
    url.protocol = url.protocol === "https:" ? "wss:" : "ws:";
    this.socket = new WebSocket(url);
    // 临时凭证只通过首条消息提交，不写入 URL 或浏览器存储；公网应部署 HTTPS/WSS。
    this.socket.onopen = () => {
      this.debug("WebSocket 已连接");
      this.send({ type: "auth", token: data.token });
    };
    this.socket.onmessage = (event) => {
      try {
        this.handle(JSON.parse(event.data));
      } catch {
        this.debug("WebSocket 消息处理失败", {}, true);
        this.fail("连接消息无效，请重新配对");
      }
    };
    this.socket.onerror = () => {
      this.debug("WebSocket 连接异常", {}, true);
      this.onEvent({
        type: "error",
        message: "连接失败，请检查服务和 WebSocket 代理配置",
      });
    };
    this.socket.onclose = (event) => {
      this.debug("WebSocket 已断开", {
        code: event.code,
        clean: event.wasClean,
      });
      this.close();
    };
  }

  send(message) {
    if (this.socket?.readyState !== WebSocket.OPEN)
      throw new Error("连接尚未就绪");
    this.socket.send(JSON.stringify(message));
  }

  handle(message) {
    // 接收端已按本地写盘进度更新，忽略服务器回传的同一份进度。
    if (message.type === "progress" && this.receiving?.id === message.id)
      return;
    if (message.type === "paired") this.startPeer(message);
    if (message.type === "signal") {
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
          : this.sendDirect(message.id);
      task.catch((error) => this.fileFailed(message.id, error));
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
      // 取消后重建直连，旧通道及其残留分块不进入下一次接收。
      if (message.mode === "p2p" && message.state !== "complete") {
        const file = this.files.get(message.id);
        if (file && !file.directReset) {
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
    this.receiveNextAutoFile();
  }

  offer(files, images = false) {
    if (this.files.size + this.pendingOffers.size + files.length > 32)
      throw new Error("每次连接最多发送 32 个文件");
    for (const file of files) {
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
      });
      this.sources.set(id, file);
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
      this.autoAccepting ||
      this.accepting ||
      this.receiving ||
      [...this.files.values()].some((file) => file.state === "active")
    )
      return;
    const file = [...this.files.values()].find(
      (file) =>
        (file.image_type || this.autoReceive) &&
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
    if (this.closed || !this.peerSettings) return;
    this.debug("重建 WebRTC 连接");
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
    this.debug("开始 WebRTC 协商", { stunEnabled: Boolean(message.stun) });
    const generation = this.peerGeneration;
    const peer = new RTCPeerConnection({
      iceServers: message.stun ? [{ urls: message.stun }] : [],
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
    } else
      peer.ondatachannel = (event) => {
        if (this.closed || this.peer !== peer) event.channel.close();
        else this.attachChannel(event.channel);
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
    this.debug("关闭连接并释放资源");
    this.closed = true;
    this.autoReceive = false;
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
