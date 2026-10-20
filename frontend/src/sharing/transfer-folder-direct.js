import { folderMaxConcurrency } from "./transfer-folders.js";

// 文件夹的每个活动文件独立使用一个通道，所有通道共享窗口和发送缓冲额度。
const windowBytes = 8 * 1024 * 1024;
const bufferedBytes = 4 * 1024 * 1024;
const chunkBytes = 256 * 1024;

export class FolderDirectTransfers {
  constructor(client) {
    this.client = client;
    this.jobs = new Map();
    this.waiters = new Set();
    this.outstanding = 0;
    this.queued = 0;
  }

  hasReceiver(id) {
    return Boolean(this.jobs.get(id)?.writer);
  }

  prepare(file, writer) {
    if (this.jobs.size >= folderMaxConcurrency || this.jobs.has(file.id))
      throw new Error("文件夹直连并发超过限制");
    this.jobs.set(file.id, {
      id: file.id,
      folder: file.folder.id,
      size: file.size,
      writer,
      bytes: 0,
      received: 0,
      queued: 0,
      chain: Promise.resolve(),
      lastUI: 0,
      reported: 0,
      progress: Date.now(),
    });
    if (!this.progressTimer)
      this.progressTimer = setInterval(() => this.reportProgress(), 1000);
  }

  reportProgress() {
    const progress = [...this.jobs.values()]
      .filter(
        (job) =>
          job.writer &&
          !job.ending &&
          !job.cancelled &&
          job.bytes > job.reported,
      )
      .map((job) => ({ id: job.id, bytes: job.bytes }));
    if (!progress.length || this.client.closed) return;
    try {
      this.client.send({ type: "folder-progress", progress });
      for (const entry of progress) {
        const job = this.jobs.get(entry.id);
        if (job) job.reported = entry.bytes;
      }
    } catch (error) {
      this.fail(progress[0].id, error);
    }
  }

  attach(channel) {
    const id = channel.label.slice("folder:".length);
    const job = this.jobs.get(id);
    // 只接收已被用户授权且已打开目标文件的通道，陌生通道不分配缓存。
    if (
      !channel.label.startsWith("folder:") ||
      !job?.writer ||
      job.channel ||
      job.cancelled
    ) {
      channel.close();
      return;
    }
    this.bind(job, channel);
  }

  bind(job, channel) {
    job.channel = channel;
    channel.binaryType = "arraybuffer";
    channel.bufferedAmountLowThreshold = 64 * 1024;
    channel.onopen = () => this.wake();
    channel.onbufferedamountlow = () => this.wake();
    channel.onmessage = ({ data }) => {
      if (job.cancelled) return;
      try {
        this.receive(job, data);
      } catch (error) {
        this.fail(job.id, error);
      }
    };
    channel.onerror = () => this.fail(job.id, new Error("文件夹直连通道异常"));
    channel.onclose = () => {
      if (!job.cancelled && !job.ending)
        this.fail(job.id, new Error("文件夹直连通道已关闭"));
      this.wake();
    };
  }

  async start(file, source) {
    if (
      !source ||
      !this.client.directReady ||
      this.jobs.size >= folderMaxConcurrency
    )
      throw new Error("文件夹直连不可用");
    const job = {
      id: file.id,
      folder: file.folder.id,
      size: file.size,
      sent: 0,
      acked: 0,
      outstanding: 0,
      progress: Date.now(),
    };
    this.jobs.set(job.id, job);
    this.bind(
      job,
      this.client.peer.createDataChannel(`folder:${file.id}`, {
        ordered: true,
      }),
    );
    this.wake();
    await this.wait(job, () => job.channel.readyState === "open");
    const negotiated = this.client.peer?.sctp?.maxMessageSize;
    const chunk = Math.min(
      chunkBytes,
      this.client.peerChunkSize || 65536,
      negotiated === 0 ? chunkBytes : negotiated || 16384,
    );
    while (job.sent < source.size) {
      // 并发增大时缩小分块，保证所有活动文件仍共享固定的接收窗口。
      const length = Math.min(
        chunk,
        Math.floor(this.limit() / Math.max(1, this.jobs.size)),
        source.size - job.sent,
      );
      await this.wait(job, () => {
        const senders = [...this.jobs.values()].filter(
          (item) => !item.writer,
        ).length;
        const allowance = Math.max(
          chunk,
          Math.floor(this.limit() / Math.max(1, senders)),
        );
        if (
          this.outstanding + length > this.limit() ||
          job.outstanding + length > allowance
        )
          return false;
        // 条件检查与预留同步执行，多个读取任务不会抢占同一份额度。
        this.outstanding += length;
        job.outstanding += length;
        return true;
      });
      // 读取前预留额度，异步读取中的分块同样计入共享窗口。
      const data = await source
        .slice(job.sent, job.sent + length)
        .arrayBuffer();
      if (data.byteLength !== length) throw new Error("源文件长度已变化");
      await this.wait(job, () => {
        if (this.buffered() + length > bufferedBytes) return false;
        // 检查后立即发送，避免异步恢复时多个通道同时超过共享发送缓冲。
        job.channel.send(data);
        job.sent += length;
        return true;
      });
    }
    await this.wait(job, () => job.acked === job.sent);
    job.ending = true;
    job.channel.send(JSON.stringify({ type: "end", id: job.id }));
  }

  limit() {
    return Math.min(windowBytes, this.client.peerReceiveWindow || windowBytes);
  }
  buffered() {
    return [...this.jobs.values()].reduce(
      (sum, job) => sum + (job.writer ? 0 : job.channel?.bufferedAmount || 0),
      0,
    );
  }
  wake() {
    for (const wake of [...this.waiters]) wake();
  }

  async wait(job, ready) {
    for (;;) {
      if (job.cancelled || this.client.closed || !this.client.directReady)
        throw new DOMException("直连已停止", "AbortError");
      if (Date.now() - job.progress > 45000)
        throw new Error("文件夹接收停滞，请重新发送");
      if (ready()) return;
      await new Promise((resolve) => {
        const wake = () => {
          clearTimeout(timer);
          this.waiters.delete(wake);
          resolve();
        };
        const timer = setTimeout(
          wake,
          Math.max(1, 45000 - (Date.now() - job.progress)),
        );
        this.waiters.add(wake);
      });
    }
  }

  receive(job, data) {
    if (typeof data === "string") {
      if (data.length > 1024) throw new Error("文件夹控制消息过大");
      const message = JSON.parse(data);
      if (message.id !== job.id) throw new Error("文件夹通道编号不匹配");
      if (!job.writer && message.type === "ack") {
        if (
          !Number.isSafeInteger(message.bytes) ||
          message.bytes < job.acked ||
          message.bytes > job.sent
        )
          throw new Error("文件夹接收确认无效");
        const delta = message.bytes - job.acked;
        job.acked = message.bytes;
        job.outstanding -= delta;
        this.outstanding -= delta;
        if (delta) job.progress = Date.now();
        this.wake();
      } else if (job.writer && message.type === "end" && !job.ending) {
        job.ending = true;
        job.chain = job.chain
          .then(async () => {
            if (job.cancelled) return;
            if (job.bytes !== job.size) throw new Error("文件长度不一致");
            await job.writer.close();
            job.saved = true;
            if (!job.cancelled && !this.client.closed)
              this.client.send({
                type: "complete",
                id: job.id,
                bytes: job.bytes,
                connection: this.client.directEndpoints,
              });
          })
          .catch((error) => this.fail(job.id, error));
      } else throw new Error("文件夹直连控制消息无效");
      return;
    }
    if (
      !job.writer ||
      job.ending ||
      !(data instanceof ArrayBuffer) ||
      !data.byteLength ||
      data.byteLength > chunkBytes ||
      this.queued + data.byteLength > windowBytes ||
      job.received + data.byteLength > job.size
    )
      throw new Error("文件夹数据超过共享接收额度");
    const chunk = new Uint8Array(data);
    this.queued += chunk.byteLength;
    job.queued += chunk.byteLength;
    job.received += chunk.byteLength;
    job.chain = job.chain
      .then(async () => {
        if (job.cancelled) return;
        await job.writer.write(chunk);
        if (job.cancelled) return;
        job.bytes += chunk.byteLength;
        job.queued -= chunk.byteLength;
        this.queued -= chunk.byteLength;
        job.channel.send(
          JSON.stringify({ type: "ack", id: job.id, bytes: job.bytes }),
        );
        if (Date.now() - (job.lastUI || 0) >= 100 || job.bytes === job.size) {
          this.client.onEvent({
            type: "progress",
            id: job.id,
            bytes: job.bytes,
          });
          job.lastUI = Date.now();
        }
      })
      .catch((error) => this.fail(job.id, error));
  }

  fail(id, error) {
    const job = this.jobs.get(id);
    if (!job || job.cancelled) return;
    const folder = job.folder;
    this.remove(id);
    this.client.folderFailed(folder, error);
  }

  remove(id) {
    const job = this.jobs.get(id);
    if (!job) return;
    job.cancelled = true;
    this.outstanding -= job.outstanding || 0;
    this.queued -= job.queued || 0;
    job.outstanding = 0;
    job.queued = 0;
    this.jobs.delete(id);
    job.channel?.close();
    if (job.writer && !job.saved) job.writer.abort().catch(() => {});
    if (![...this.jobs.values()].some((item) => item.writer)) {
      clearInterval(this.progressTimer);
      this.progressTimer = null;
    }
    this.wake();
  }

  cancelFolder(id) {
    for (const job of [...this.jobs.values()])
      if (job.folder === id) this.remove(job.id);
  }
  close() {
    for (const id of [...this.jobs.keys()]) this.remove(id);
  }
}
