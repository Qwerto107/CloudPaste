<script setup>
import {
  computed,
  h,
  markRaw,
  nextTick,
  onBeforeUnmount,
  ref,
  watch,
} from "vue";
import { NModal, useNotification } from "naive-ui";
import { onBeforeRouteLeave } from "vue-router";
import { session, canTransfer } from "./session.js";
import { settings, loadSettings } from "./settings.js";
import Icon from "./Icon.vue";
import PairCodeInput from "./PairCodeInput.vue";
import IPLocation from "./IPLocation.vue";
import {
  canSaveFolders,
  readFolder,
  folderFromFiles,
  folderMaxConcurrency,
} from "./transfer-folders.js";
import { chatImageHistory, validateChatImage } from "./transfer-images.js";
import {
  normalizePairingCode,
  validPairingCode,
  displayPairingCode,
} from "./transfer-code.js";
import {
  TransferClient,
  canSaveDirectly,
  transferID,
} from "./transfer-client.js";

// 每次进入页面生成短随机后缀，方便区分参与配对的设备，用户仍可自行修改。
const name = ref(`我的设备-${transferID().slice(0, 6)}`);
const code = ref("");
const pairCodeInput = ref(null);
const autoApproveDevice = ref(false);
const pastingCode = ref(false);
const pairingCode = ref("");
const relayOnly = ref(false);
const stunMode = ref("site");
const customSTUN = ref("");
const autoReceive = ref(false);
const error = ref("");
const notification = useNotification();
const notices = new Map();
function notify(type, content) {
  const key = `${type}:${content}`;
  if (notices.has(key)) return;
  if (notices.size >= 3) {
    const [oldKey, oldest] = notices.entries().next().value;
    notices.delete(oldKey);
    oldest.destroy();
  }
  const notice = notification[type]({
    content: () =>
      h("span", { role: type === "error" ? "alert" : "status" }, content),
    duration: type === "error" ? 6000 : 3000,
    closable: true,
    onAfterLeave: () => {
      if (notices.get(key) === notice) notices.delete(key);
    },
  });
  notices.set(key, notice);
}
watch(
  error,
  (message) => {
    if (!message) return;
    notify("error", message);
    // 提示关闭后，同一个错误仍可再次显示；活动中的相同提示由上方合并。
    error.value = "";
  },
  { flush: "sync" },
);
const status = ref("idle");
const joining = ref("");
const joiningIP = ref("");
const joiningDevice = ref(null);
const joiningLocation = ref(null);
const names = ref([]);
const peerIPs = ref([]);
const peerDevices = ref([]);
const peerLocations = ref([]);
const peerEndpoint = ref(null);
const peerDetailsOpen = ref(true);
const peerRelayOnly = ref(false);
const messages = ref([]);
const messageList = ref(null);
let followMessages = true;
const text = ref("");
const files = ref([]);
const selected = ref([]);
const folders = ref([]);
const selectedFolder = ref(null);
const folderSelecting = ref(false);
const folderInput = ref(null);
const folderSavingSupported = canSaveFolders();
let folderScan;
const pastedImages = ref([]);
const previewImage = ref(null);
const previewViewport = ref(null);
const previewScale = ref(1);
const previewOffset = ref({ x: 0, y: 0 });
const previewPointers = new Map();
let pinchDistance = 0;
const messageClock = new Intl.DateTimeFormat("zh-CN", {
  hour: "2-digit",
  minute: "2-digit",
  second: "2-digit",
  hour12: false,
});
const pasting = ref(false);
const offeredCount = ref(0);
const fileInput = ref(null);
const dragDepth = ref(0);
const direct = ref(false);
const myMember = ref(0);
const maxFile = ref(0);
const busy = ref(false);
const canCreate = computed(() => canTransfer("create"));
const canJoin = computed(() => canTransfer("join"));
const active = computed(
  () =>
    files.value.some((file) => file.state === "active") ||
    folders.value.some((folder) => folder.state === "active"),
);
const connected = computed(() => status.value === "paired");
const hasPair = computed(() =>
  ["paired", "reconnecting", "peer-offline"].includes(status.value),
);
const reconnectDeadline = ref("");
const canDirect = computed(() => direct.value && canSaveDirectly());
const peerAddress = computed(() => {
  if (!direct.value) return peerRelayOnly.value ? "已禁用" : "未建立";
  const endpoint = peerEndpoint.value;
  if (!endpoint) return "正在获取链路信息";
  const port =
    Number.isInteger(endpoint.port) && endpoint.port > 0 ? endpoint.port : null;
  if (!endpoint.address)
    return port ? `浏览器未提供 IP（端口 ${port}）` : "浏览器未提供";
  // IPv6 地址加方括号，避免地址自身的冒号与端口混淆。
  const address = endpoint.address.includes(":")
    ? `[${endpoint.address}]`
    : endpoint.address;
  return port ? `${address}:${port}` : `${address}（端口未提供）`;
});
// 点对点入口使用蓝色，与绿色中继按钮区分，并复用浅色、深色主题的配色。
const directButtonTheme = {
  color: "var(--transfer-blue-bg)",
  colorHover: "var(--transfer-blue-bg)",
  colorPressed: "var(--transfer-blue-bg)",
  colorFocus: "var(--transfer-blue-bg)",
  colorDisabled: "var(--transfer-blue-bg)",
  textColor: "var(--transfer-blue)",
  textColorHover: "var(--transfer-blue)",
  textColorPressed: "var(--transfer-blue)",
  textColorFocus: "var(--transfer-blue)",
  textColorDisabled: "var(--transfer-blue)",
  border: "1px solid var(--transfer-blue)",
  borderHover: "1px solid var(--transfer-blue)",
  borderPressed: "1px solid var(--transfer-blue)",
  borderFocus: "1px solid var(--transfer-blue)",
  borderDisabled: "1px solid var(--transfer-blue)",
};
const ordinaryFiles = computed(() =>
  files.value.filter((file) => !file.image_type && !file.folder),
);
// 复用已有逐文件进度；按文件夹分组，不增加请求或轮询。
const folderFileGroups = computed(() => {
  const groups = new Map();
  for (const file of files.value) {
    if (!file.folder || file.folder.directory) continue;
    if (!groups.has(file.folder.id)) groups.set(file.folder.id, []);
    groups.get(file.folder.id).push(file);
  }
  return groups;
});
// 当前任务与已结束记录共用一套操作模板，避免按钮行为在两个列表中分叉。
const fileGroups = computed(() => {
  const finished = (file) =>
    ["cancelled", "failed", "complete"].includes(file.state);
  return [
    {
      key: "current",
      label: "当前任务",
      ended: false,
      folders: folders.value.filter((folder) => !finished(folder)),
      files: ordinaryFiles.value
        .filter((file) => !finished(file))
        .sort(
          (a, b) => Number(b.state === "active") - Number(a.state === "active"),
        ),
    },
    {
      key: "history",
      label: "已结束的传输",
      ended: true,
      folders: folders.value.filter(finished),
      files: ordinaryFiles.value.filter(finished),
    },
  ].filter((group) => group.files.length || group.folders.length);
});
let client;

function resetPreview() {
  previewScale.value = 1;
  previewOffset.value = { x: 0, y: 0 };
  previewPointers.clear();
  pinchDistance = 0;
}
watch(previewImage, resetPreview);
function constrainPreview(
  x = previewOffset.value.x,
  y = previewOffset.value.y,
) {
  const viewport = previewViewport.value;
  const image = viewport?.querySelector("img");
  if (!image) return;
  // 拖动范围以放大后的图片边缘为界，避免把整张图片拖出视野。
  const limitX = Math.max(
    0,
    (image.clientWidth * previewScale.value - viewport.clientWidth) / 2,
  );
  const limitY = Math.max(
    0,
    (image.clientHeight * previewScale.value - viewport.clientHeight) / 2,
  );
  previewOffset.value = {
    x: Math.max(-limitX, Math.min(limitX, x)),
    y: Math.max(-limitY, Math.min(limitY, y)),
  };
}
function zoomPreview(scale) {
  previewScale.value = Math.max(0.25, Math.min(8, scale));
  constrainPreview();
}
function previewDistance() {
  const [first, second] = [...previewPointers.values()];
  return second ? Math.hypot(first.x - second.x, first.y - second.y) : 0;
}
function startPreviewDrag(event) {
  if (event.button !== 0) return;
  previewPointers.set(event.pointerId, { x: event.clientX, y: event.clientY });
  event.currentTarget.setPointerCapture(event.pointerId);
  pinchDistance = previewDistance();
}
function movePreview(event) {
  const previous = previewPointers.get(event.pointerId);
  if (!previous) return;
  previewPointers.set(event.pointerId, { x: event.clientX, y: event.clientY });
  if (previewPointers.size === 1) {
    constrainPreview(
      previewOffset.value.x + event.clientX - previous.x,
      previewOffset.value.y + event.clientY - previous.y,
    );
  } else {
    // 双指缩放与鼠标操作共用同一缩放状态，不额外创建图片副本。
    const distance = previewDistance();
    if (pinchDistance > 0)
      zoomPreview((previewScale.value * distance) / pinchDistance);
    pinchDistance = distance;
  }
}
function endPreviewDrag(event) {
  previewPointers.delete(event.pointerId);
  pinchDistance = previewDistance();
}
function releasePreview(file) {
  // 预览被移除或超出保留数量时一起关闭大图，不保留已经释放的图片地址。
  if (previewImage.value?.id === file?.id) previewImage.value = null;
  if (file?.previewURL) URL.revokeObjectURL(file.previewURL);
  if (file) file.previewURL = "";
}
function appendMessage(message) {
  const sent = new Date(message.sent_at);
  if (!Number.isNaN(sent.getTime())) {
    message.sent_at = sent.toISOString();
    message.timeLabel = messageClock.format(sent);
    message.timeTitle = sent.toLocaleString("zh-CN", { hour12: false });
  }
  messages.value.push(message);
  if (messages.value.length > 100) releasePreview(messages.value.shift().file);
  scrollMessages();
}
function scrollMessages() {
  if (followMessages)
    nextTick(() => {
      if (messageList.value)
        messageList.value.scrollTop = messageList.value.scrollHeight;
    });
}
function trackMessageScroll(event) {
  const list = event.target;
  followMessages = list.scrollHeight - list.scrollTop - list.clientHeight < 64;
}
function removePasted(index) {
  const [item] = pastedImages.value.splice(index, 1);
  if (item) URL.revokeObjectURL(item.url);
}
function clearImages() {
  for (const file of files.value) releasePreview(file);
  while (pastedImages.value.length) removePasted(0);
}
async function pasteImage(event) {
  const images = Array.from(event.clipboardData?.items || [])
    .filter((item) => item.kind === "file" && item.type.startsWith("image/"))
    .map((item) => item.getAsFile())
    .filter(Boolean);
  if (!images.length) return;
  event.preventDefault();
  if (pasting.value) return;
  pasting.value = true;
  error.value = "";
  const current = client;
  try {
    for (const image of images) {
      if (pastedImages.value.length >= chatImageHistory)
        throw new Error("每次最多粘贴 4 张图片");
      await validateChatImage(image);
      if (current !== client || !connected.value) return;
      const file = new File(
        [image],
        `截图-${Date.now()}-${pastedImages.value.length + 1}.${image.type === "image/png" ? "png" : "jpg"}`,
        { type: image.type },
      );
      pastedImages.value.push({ file, url: URL.createObjectURL(file) });
    }
  } catch (reason) {
    error.value = reason.message;
  } finally {
    pasting.value = false;
  }
}

function size(value) {
  if (!value) return "0 B";
  const index = Math.min(3, Math.floor(Math.log(value) / Math.log(1024)));
  return `${(value / 1024 ** index).toFixed(index ? 1 : 0)} ${["B", "KiB", "MiB", "GiB"][index]}`;
}
function event(message) {
  if (message.type === "folder") {
    folders.value.push({
      ...message,
      concurrencyDraft: message.concurrency,
      bytes: 0,
      completed: 0,
      speed: 0,
      measured: performance.now(),
      sampleBytes: 0,
      samples: [],
      recent: [],
      sentLabel: new Date(message.sent_at).toLocaleString("zh-CN", {
        hour12: false,
      }),
    });
  } else if (message.type === "folder-state") {
    const folder = folders.value.find((item) => item.id === message.id);
    if (folder) {
      if (message.state === "active" && folder.state !== "active")
        folder.measured = performance.now();
      Object.assign(folder, message);
    }
  } else if (message.type === "folder-concurrency") {
    const folder = folders.value.find((item) => item.id === message.id);
    if (folder) {
      folder.updatingConcurrency = false;
      if (!message.error) {
        folder.concurrency = message.concurrency;
        folder.concurrencyDraft = message.concurrency;
      }
    }
    if (message.error) error.value = message.error;
  } else if (message.type === "created") {
    joining.value = "";
    joiningIP.value = "";
    joiningDevice.value = null;
    joiningLocation.value = null;
    names.value = [];
    peerIPs.value = [];
    peerDevices.value = [];
    peerLocations.value = [];
    peerEndpoint.value = null;
    pairingCode.value = message.code || "";
    myMember.value = message.member;
    maxFile.value = message.max_file_size;
    status.value = "connecting";
  } else if (["reconnecting", "peer-offline"].includes(message.type)) {
    status.value = message.type;
    reconnectDeadline.value = new Date(
      message.reconnect_until,
    ).toLocaleTimeString("zh-CN", { hour12: false });
    direct.value = false;
    peerEndpoint.value = null;
    folderScan?.abort();
  } else if (message.type === "ready") status.value = "waiting";
  else if (message.type === "joining") {
    joining.value = message.name;
    joiningIP.value = message.ip || "";
    joiningDevice.value = message.device || null;
    joiningLocation.value = message.location || null;
  } else if (message.type === "paired") {
    const recovering = hasPair.value && !connected.value;
    status.value = "paired";
    reconnectDeadline.value = "";
    if (recovering) notify("success", "连接已恢复，中断的文件请重新发送");
    names.value = message.names;
    peerIPs.value = message.ips || [];
    peerDevices.value = message.devices || [];
    peerLocations.value = message.locations || [];
    peerRelayOnly.value = message.relay_only === true;
    pairingCode.value = "";
    joining.value = "";
    joiningIP.value = "";
    joiningDevice.value = null;
    joiningLocation.value = null;
  } else if (message.type === "direct") {
    direct.value = message.ready;
    // 重建、降级时立即清除旧地址，避免把上一次链路当作当前链路。
    peerEndpoint.value = null;
  } else if (message.type === "connection-info") {
    if (direct.value) peerEndpoint.value = message.remote;
  } else if (message.type === "locations") {
    if (Array.isArray(message.locations))
      peerLocations.value = message.locations;
    if (Object.hasOwn(message, "joining_location"))
      joiningLocation.value = message.joining_location;
  } else if (message.type === "text") {
    appendMessage({ ...message, id: transferID() });
  } else if (message.type === "file") {
    if (!message.folder) offeredCount.value++;
    // 双方使用服务端的发送时间；只在收到任务时格式化，进度更新不重复计算。
    const sent = new Date(message.sent_at);
    files.value.push({
      ...message,
      sentLabel: Number.isNaN(sent.getTime())
        ? ""
        : sent.toLocaleString("zh-CN", { hour12: false }),
      sentTimeLabel: Number.isNaN(sent.getTime())
        ? ""
        : messageClock.format(sent),
      source: undefined,
      bytes: 0,
      speed: 0,
      measured: performance.now(),
      sampleBytes: 0,
      samples: [],
    });
    if (message.image_type)
      appendMessage({
        id: message.id,
        from: message.from,
        sent_at: message.sent_at,
        file: files.value.at(-1),
      });
  } else if (message.type === "image" || message.type === "image-error") {
    const file = files.value.find((item) => item.id === message.id);
    if (!file) return;
    if (message.type === "image-error") file.imageError = message.message;
    else {
      releasePreview(file);
      file.previewURL = URL.createObjectURL(message.blob);
      // 浏览器只保留最近四张图片的本地预览和下载数据，文字记录仍保留。
      const previews = files.value.filter((item) => item.previewURL);
      while (previews.length > chatImageHistory)
        releasePreview(previews.shift());
      scrollMessages();
    }
  } else if (message.type === "retry") {
    const file = files.value.find((item) => item.id === message.id);
    if (file) {
      file.retrying = message.pending;
      if (message.retried) file.retried = true;
    }
  } else if (
    [
      "state",
      "progress",
      "download",
      "download-tool",
      "tool-complete",
    ].includes(message.type)
  ) {
    const file = files.value.find((item) => item.id === message.id);
    if (!file) return;
    if (message.type === "progress" && Number.isSafeInteger(message.round)) {
      if (message.round < (file.progressRound || 0)) return;
      if (message.round > (file.progressRound || 0)) {
        // 重新下载允许进度归零，旧一轮进度不得覆盖新一轮；速率也从本轮重新采样。
        file.progressRound = message.round;
        file.completedRound = 0;
        file.bytes = 0;
        file.speed = 0;
        file.samples = [];
        file.sampleBytes = 0;
        file.measured = performance.now();
      }
      file.requested = true;
    }
    if (message.type === "tool-complete") {
      file.progressRound = message.round;
      file.completedRound = message.round;
    }
    if (message.type === "download-tool") {
      // 保留可重复下载的中继链接，也允许复制给外部下载工具。
      file.expiresLabel = new Date(message.expires_at).toLocaleTimeString(
        "zh-CN",
        { hour12: false },
      );
      file.maxConnections = message.max_connections;
    }
    if (["download", "download-tool"].includes(message.type)) {
      file.url = client.downloadURL(message.url);
      // 接收确认后直接交给原生下载器，不把整个响应读成 Blob，也不要求第二次点击。
      const link = document.createElement("a");
      link.href = file.url;
      link.download = file.name;
      link.referrerPolicy = "no-referrer";
      document.body.append(link);
      link.click();
      link.remove();
      file.urlUsed = true;
      file.requested = true;
    }
    if (message.state) {
      file.state = message.state;
      file.mode = message.mode;
      if (file.state === "active") {
        file.measured = performance.now();
        file.sampleBytes = file.bytes;
        file.samples = [];
      }
      if (file.state !== "active") file.url = "";
    }
    if (message.bytes !== undefined) {
      file.bytes = Math.max(file.bytes, message.bytes);
    }
    if (
      file.folder &&
      ["complete", "failed", "cancelled"].includes(file.state)
    ) {
      const folder = folders.value.find((item) => item.id === file.folder.id);
      if (folder) {
        folder.recent.push({
          path: file.folder.path || "（空目录）",
          state: file.state,
        });
        if (folder.recent.length > 20) folder.recent.shift();
      }
      files.value = files.value.filter((item) => item !== file);
    }
  } else if (message.type === "error") error.value = message.message;
  else if (message.type === "closed") {
    folderScan?.abort();
    selectedFolder.value = null;
    for (const folder of folders.value)
      if (["active", "offered"].includes(folder.state))
        folder.state = "cancelled";
    autoReceive.value = false;
    clearImages();
    status.value = "closed";
    direct.value = false;
    pairingCode.value = "";
    joining.value = "";
    joiningIP.value = "";
    joiningDevice.value = null;
    joiningLocation.value = null;
    peerIPs.value = [];
    peerDevices.value = [];
    peerLocations.value = [];
    peerEndpoint.value = null;
    for (const file of files.value) {
      file.url = "";
      if (["active", "offered"].includes(file.state)) file.state = "cancelled";
    }
  }
}
async function open(kind) {
  // 自动提交与手动提交共用入口，请求期间不能重复加入。
  if (busy.value || (kind === "join" && !validPairingCode(code.value))) return;
  if (!canTransfer(kind)) return;
  error.value = "";
  busy.value = true;
  autoReceive.value = false;
  peerDetailsOpen.value = true;
  client?.close();
  clearImages();
  files.value = [];
  folders.value = [];
  selectedFolder.value = null;
  offeredCount.value = 0;
  selected.value = [];
  messages.value = [];
  followMessages = true;
  pairingCode.value = "";
  client = new TransferClient(
    event,
    () => settings.transfer_debug_logs === "true",
  );
  try {
    // 每次配对读取最新开关，其他标签页保存的设置也能用于新连接。
    await loadSettings(true);
    if (
      !relayOnly.value &&
      stunMode.value === "custom" &&
      !customSTUN.value.trim()
    )
      throw new Error("请填写自定义 STUN 地址，或选择不使用 STUN");
    await client.open(
      kind,
      {
        name: name.value,
        relay_only: relayOnly.value,
        ...(kind === "join" ? { code: normalizePairingCode(code.value) } : {}),
      },
      relayOnly.value || stunMode.value === "disabled"
        ? ""
        : stunMode.value === "custom"
          ? customSTUN.value
          : null,
    );
  } catch (reason) {
    client.debug("连接申请失败", { error: reason.name }, true);
    error.value = reason.message;
    client.close();
    if (kind === "join" && reason.code === "TRANSFER_UNAVAILABLE") {
      // 失效代码不保留在分格输入框中，便于直接输入新的配对码。
      code.value = "";
      nextTick(() => pairCodeInput.value?.focus());
    }
  } finally {
    busy.value = false;
  }
}
function send(value) {
  error.value = "";
  try {
    client.send(value);
  } catch (reason) {
    error.value = reason.message;
  }
}
watch([autoApproveDevice, joining], ([enabled, device]) => {
  // 新申请或开启开关时统一确认，仍由服务端校验创建者身份和双方状态。
  if (enabled && myMember.value === 0 && status.value === "waiting" && device)
    send({ type: "confirm" });
});
function sendText() {
  if (!text.value.trim() && !pastedImages.value.length) return;
  if (new TextEncoder().encode(text.value).length > 4096) {
    error.value = "文字最多 4096 字节";
    return;
  }
  error.value = "";
  try {
    if (pastedImages.value.length) {
      client.offer(
        pastedImages.value.map((item) => item.file),
        true,
      );
      while (pastedImages.value.length) removePasted(0);
    }
    if (text.value.trim()) client.send({ type: "text", text: text.value });
    text.value = "";
  } catch (reason) {
    error.value = reason.message;
  }
}
function addFiles(items) {
  const remaining = Math.max(
    0,
    32 - offeredCount.value - selected.value.length,
  );
  const incoming = Array.from(items || []);
  selected.value.push(...incoming.slice(0, remaining));
  if (incoming.length > remaining) error.value = "每次连接最多发送 32 个文件";
}
function choose(event) {
  addFiles(event.target.files);
  event.target.value = "";
}
function dropFiles(event) {
  dragDepth.value = 0;
  if (
    Array.from(event.dataTransfer.items || []).some(
      (item) => item.webkitGetAsEntry?.()?.isDirectory,
    )
  ) {
    error.value = "发送文件夹请使用“选择文件夹”，以保留目录结构";
    return;
  }
  addFiles(event.dataTransfer.files);
}
async function selectFolder(inputFiles = null) {
  if (folderSelecting.value || selectedFolder.value) return;
  if (!inputFiles && !folderSavingSupported) {
    folderInput.value?.click();
    return;
  }
  folderSelecting.value = true;
  error.value = "";
  folderScan = new AbortController();
  const current = client;
  try {
    // 选择器由点击直接触发；扫描只统计数量和总大小，不缓存所有 File。
    const folder = inputFiles
      ? await folderFromFiles(inputFiles, maxFile.value, folderScan.signal)
      : await readFolder(
          await window.showDirectoryPicker({ mode: "read" }),
          maxFile.value,
          folderScan.signal,
        );
    if (current === client && connected.value)
      selectedFolder.value = markRaw(folder);
  } catch (reason) {
    if (reason.name !== "AbortError") error.value = reason.message;
  } finally {
    folderSelecting.value = false;
  }
}
function chooseFolderFiles(event) {
  if (event.target.files.length)
    void selectFolder(Array.from(event.target.files));
  event.target.value = "";
}
function offerFolder() {
  try {
    client.offerFolder(selectedFolder.value);
    selectedFolder.value = null;
  } catch (reason) {
    error.value = reason.message;
  }
}
async function acceptFolder(folder, direct) {
  folder.accepting = true;
  error.value = "";
  try {
    await client.acceptFolder(
      folder.id,
      direct,
      Number(folder.concurrencyDraft),
    );
  } catch (reason) {
    if (reason.name !== "AbortError") error.value = reason.message;
  } finally {
    folder.accepting = false;
  }
}
function updateFolderConcurrency(folder) {
  error.value = "";
  try {
    client.updateFolderConcurrency(folder.id, folder.concurrencyDraft);
    folder.updatingConcurrency = true;
  } catch (reason) {
    error.value = reason.message;
  }
}
function cancelFolder(folder) {
  try {
    client.cancelFolder(folder.id);
  } catch (reason) {
    error.value = reason.message;
  }
}
function folderBytes(folder) {
  return Math.min(
    folder.size,
    folder.bytes +
      files.value
        .filter((file) => file.folder?.id === folder.id)
        .reduce((total, file) => total + file.bytes, 0),
  );
}
function folderFiles(folder) {
  return folderFileGroups.value.get(folder.id) || [];
}
function filePercent(file) {
  return file.size
    ? Math.min(100, Math.round((file.bytes / file.size) * 100))
    : 0;
}
const folderStateLabels = {
  offered: "等待接收",
  active: "传输中",
  complete: "已完成",
  failed: "传输失败",
  cancelled: "已取消",
};
function removeTask(file) {
  if (!["cancelled", "failed", "complete"].includes(file.state)) return;
  client?.forgetSource(file.id);
  releasePreview(file);
  messages.value = messages.value.filter(
    (message) => message.file?.id !== file.id,
  );
  files.value = files.value.filter((item) => item.id !== file.id);
}
function retry(file) {
  error.value = "";
  try {
    client.retry(file.id);
  } catch (reason) {
    error.value = reason.message;
  }
}
function offer() {
  error.value = "";
  try {
    client.offer(selected.value);
    selected.value = [];
  } catch (reason) {
    error.value = reason.message;
  }
}
async function accept(file, directly) {
  error.value = "";
  file.accepting = true;
  try {
    // 中继获取链接后立即开始原生下载，点对点直接选择保存位置。
    await client.accept(file, directly, !directly);
  } catch (reason) {
    if (reason.name !== "AbortError") error.value = reason.message;
  } finally {
    file.accepting = false;
  }
}
async function copyDownloadLink(file) {
  error.value = "";
  try {
    await navigator.clipboard.writeText(file.url);
    file.linkCopied = true;
    notify("success", "下载链接已复制");
  } catch {
    file.linkExpanded = true;
    error.value = "复制失败，请选择下方下载链接手动复制";
  }
}
async function copyCode() {
  try {
    await navigator.clipboard.writeText(
      normalizePairingCode(pairingCode.value),
    );
    notify("success", "配对码已复制");
  } catch {
    error.value = "复制失败，请手动选择配对码复制";
  }
}
async function clearCode() {
  if (busy.value || pastingCode.value || !canJoin.value) return;
  code.value = "";
  // 等五个输入框同步清空后，再将焦点移回第一格，清空不会触发自动加入。
  await nextTick();
  pairCodeInput.value?.focus();
}
async function pasteCode() {
  if (busy.value || pastingCode.value) return;
  const previous = code.value;
  pastingCode.value = true;
  error.value = "";
  try {
    if (!navigator.clipboard?.readText) throw new Error("无法读取剪贴板");
    const clipboard = await navigator.clipboard.readText();
    // 权限询问期间用户可能已输入或开始配对，不能覆盖新的输入状态。
    if (
      busy.value ||
      code.value !== previous ||
      !["idle", "closed"].includes(status.value)
    )
      return;
    const value = normalizePairingCode(clipboard);
    if (!validPairingCode(value)) {
      error.value = "剪贴板中不是有效的配对码（例如 A-1234）";
      return;
    }
    code.value = value;
    await nextTick();
    await open("join");
  } catch {
    error.value = "无法读取剪贴板，请在配对码输入框中手动粘贴";
  } finally {
    pastingCode.value = false;
  }
}
function leave() {
  client?.close();
}
// 正常任务只显示简短状态，失败时保留处理提示；中继转发完成不等同于写盘成功。
function fileStatus(file) {
  // 同一次中继完成按当前用户角色展示，保存结果仍由接收方确认。
  const sentByMe = file.from === myMember.value;
  const deliveredLabel = sentByMe ? "已发送" : "已接收";
  if (file.state === "failed")
    return {
      label: "传输失败",
      kind: "failed",
      detail: "请重新发送，或重新选择文件。",
    };
  if (file.state === "cancelled") return { label: "已取消", kind: "neutral" };
  if (file.state === "complete") {
    if (file.mode === "p2p") return { label: "已保存", kind: "complete" };
    if (file.mode === "tool")
      return {
        label: "已完成",
        kind: "complete",
      };
    return {
      label: deliveredLabel,
      kind: "complete",
    };
  }
  if (file.state === "active") {
    if (file.mode === "tool") {
      if (file.completedRound > 0 && file.completedRound === file.progressRound)
        return {
          label: "下载完成",
          kind: "complete",
        };
      if (!file.bytes) return { label: "等待下载", kind: "neutral" };
    } else if (file.url && !file.urlUsed)
      return { label: "等待下载", kind: "neutral" };
    return { label: "传输中", kind: "active" };
  }
  return { label: "等待接收", kind: "neutral" };
}
function stateLabel(file) {
  if (file.image_type && file.state === "complete")
    return file.from === myMember.value ? "已发送" : "已接收";
  return fileStatus(file).label;
}
function beforeUnload(event) {
  if (hasPair.value || active.value) {
    event.preventDefault();
    event.returnValue = "";
  }
}
window.addEventListener("beforeunload", beforeUnload);
// 速率每秒采样一次，取最近三个采样区间的平均值，不跟随分块消息跳动。
function measureSpeed(transfer, bytes, now) {
  if (transfer.state !== "active") return;
  const elapsed = now - transfer.measured;
  if (elapsed < 1000) return;
  transfer.samples.push({
    bytes: Math.max(0, bytes - transfer.sampleBytes),
    elapsed,
  });
  if (transfer.samples.length > 3) transfer.samples.shift();
  transfer.speed =
    (transfer.samples.reduce((sum, item) => sum + item.bytes, 0) * 1000) /
    transfer.samples.reduce((sum, item) => sum + item.elapsed, 0);
  transfer.sampleBytes = bytes;
  transfer.measured = now;
}
const speedTimer = setInterval(() => {
  const now = performance.now();
  for (const file of files.value) measureSpeed(file, file.bytes, now);
  // 累计已完成文件和在传文件的字节，短文件完成移出列表后仍计入文件夹速率。
  for (const folder of folders.value)
    measureSpeed(folder, folderBytes(folder), now);
}, 1000);
onBeforeRouteLeave(() => {
  if (
    hasPair.value &&
    !window.confirm("离开会断开互传并取消未完成的文件，是否继续？")
  )
    return false;
  leave();
});
watch(
  () => session.token,
  () => {
    if (client && myMember.value === 0) leave();
  },
);
watch(autoReceive, (value) => client?.setAutoReceive(value));
onBeforeUnmount(() => {
  clearInterval(speedTimer);
  window.removeEventListener("beforeunload", beforeUnload);
  leave();
  clearImages();
  for (const notice of notices.values()) notice.destroy();
  notices.clear();
});
</script>

<template>
  <div class="transfer-page">
    <div v-if="!hasPair" class="page-heading">
      <div>
        <p class="eyebrow">文字与文件，即时互传</p>
        <h1>设备互传</h1>
        <p class="muted">输入配对码，互发文字和文件。双方请保持页面在线。</p>
      </div>
    </div>
    <section
      v-if="['idle', 'closed'].includes(status)"
      class="panel transfer-entry"
    >
      <p v-if="status === 'closed'" role="status">
        连接已结束。未完成的文件需要重新发送。
      </p>
      <label
        >设备名称<UiInput
          v-model="name"
          aria-label="设备名称"
          maxlength="60"
          autocomplete="off"
      /></label>
      <label class="check"
        ><UiCheckbox
          v-model="relayOnly"
        />仅使用中继，不向对方交换直连网络地址</label
      >
      <details class="transfer-connection-settings">
        <summary>高级连接设置</summary>
        <div class="transfer-connection-fields">
          <label
            >STUN 服务器
            <UiSelect
              v-model="stunMode"
              aria-label="STUN 模式"
              :disabled="busy || relayOnly"
            >
              <option value="site">使用站点配置（推荐）</option>
              <option value="custom">自定义服务器</option>
              <option value="disabled">不使用 STUN</option>
            </UiSelect>
          </label>
          <template v-if="stunMode === 'custom'">
            <label
              >自定义 STUN 地址
              <UiInput
                v-model="customSTUN"
                type="textarea"
                aria-label="自定义 STUN 地址"
                :rows="3"
                maxlength="1284"
                :disabled="busy || relayOnly"
                spellcheck="false"
                placeholder="stun:stun.example.com:3478"
                aria-describedby="transfer-stun-help"
              />
            </label>
          </template>
          <p id="transfer-stun-help" class="muted small">
            {{
              relayOnly
                ? "仅使用中继时不查询 STUN 服务器。"
                : stunMode === "custom"
                  ? "每行一个，最多 5 个。仅用于本机本次配对，浏览器会向这些服务器查询网络地址。"
                  : stunMode === "disabled"
                    ? "不查询 STUN 服务器，仍可尝试直连；无法直连时可使用中继。"
                    : "使用站点提供的服务器，由浏览器收集候选地址并验证可用链路。"
            }}
          </p>
        </div>
      </details>
      <div class="transfer-entry-grid">
        <div class="transfer-entry-card">
          <h2>创建连接</h2>
          <p class="muted">生成 5 分钟有效的配对码，确认对方加入后即可互传。</p>
          <label v-if="canCreate" class="check">
            <UiCheckbox
              v-model="autoApproveDevice"
              :disabled="busy"
            />自动同意设备加入
          </label>
          <p v-if="canCreate" class="muted small">
            开启后，持有码的设备会自动加入。
          </p>
          <UiButton
            v-if="canCreate"
            class="primary"
            icon="link"
            :disabled="busy"
            @click="open('create')"
            >创建配对码</UiButton
          >
          <p v-else-if="settings.transfer_enabled === 'false'" class="muted">
            设备互传已关闭。
          </p>
          <RouterLink
            v-else-if="session.type === 'none'"
            :to="{ path: '/login', query: { redirect: '/transfer' } }"
            >登录后创建连接</RouterLink
          >
          <p v-else class="muted">创建连接需要文本和文件分享权限。</p>
        </div>
        <form class="transfer-entry-card" @submit.prevent="open('join')">
          <h2>加入连接</h2>
          <p v-if="!canJoin" class="muted">
            {{
              settings.transfer_enabled === "false"
                ? "设备互传已关闭。"
                : "访客加入互传已关闭，请登录后使用。"
            }}
          </p>
          <div class="transfer-code-field">
            <label for="transfer-pair-code">配对码</label>
            <div class="transfer-code-input">
              <PairCodeInput
                ref="pairCodeInput"
                v-model="code"
                :disabled="busy || !canJoin"
                @complete="open('join')"
              />
              <div class="transfer-code-actions">
                <UiButton
                  type="button"
                  aria-label="粘贴配对码"
                  :disabled="busy || !canJoin || pastingCode"
                  @click="pasteCode"
                  >{{ pastingCode ? "读取中" : "粘贴" }}</UiButton
                >
                <UiButton
                  type="button"
                  aria-label="清空配对码"
                  :disabled="busy || !canJoin || pastingCode || !code"
                  @click="clearCode"
                  >清空</UiButton
                >
              </div>
            </div>
          </div>
          <UiButton
            class="primary"
            icon="transfer"
            :disabled="busy || !canJoin || !validPairingCode(code)"
            >加入连接</UiButton
          >
        </form>
      </div>
      <p class="muted transfer-note">
        中继经本站转发；直连会向对方提供网络地址。请只与信任的人配对。
      </p>
    </section>

    <section v-else-if="!hasPair" class="panel transfer-waiting">
      <h2>
        {{
          status === "connecting"
            ? "正在连接"
            : myMember === 0
              ? "等待另一台设备"
              : "等待对方确认"
        }}
      </h2>
      <template v-if="pairingCode">
        <p class="transfer-code">
          {{ displayPairingCode(pairingCode) }}
        </p>
        <UiButton type="button" @click="copyCode">复制配对码</UiButton>
        <p class="muted">配对成功后失效。请通过可信渠道把配对码发送给对方。</p>
      </template>
      <div
        v-if="myMember === 0 && status === 'waiting'"
        class="transfer-auto-approve"
      >
        <label class="check">
          <UiCheckbox v-model="autoApproveDevice" :disabled="busy" />
          自动同意设备加入
        </label>
        <p class="muted small">开启后，持有码的设备会自动加入。</p>
      </div>
      <div v-if="joining" class="transfer-joining" role="status">
        <div class="transfer-peer-info">
          <strong>{{ joining }}</strong>
          <span class="transfer-tag neutral">待授权</span>
        </div>
        <dl class="transfer-peer-details">
          <div>
            <dt>来源 IP</dt>
            <dd>{{ joiningIP || "未提供" }}</dd>
          </div>
          <div v-if="joiningLocation">
            <dt>IP 归属地</dt>
            <dd><IPLocation :location="joiningLocation" /></dd>
          </div>
          <div>
            <dt>设备信息</dt>
            <dd>{{ joiningDevice?.summary || "未提供" }}</dd>
          </div>
        </dl>
        <p class="muted">请求加入，请确认这是你要连接的设备。</p>
        <div class="actions">
          <UiButton class="primary" @click="send({ type: 'confirm' })"
            >确认连接</UiButton
          ><UiButton class="danger" @click="send({ type: 'reject' })"
            >拒绝并关闭</UiButton
          >
        </div>
      </div>
      <UiButton class="quiet" @click="leave">取消连接</UiButton>
    </section>

    <template v-else>
      <div class="panel transfer-peer">
        <div class="transfer-peer-bar">
          <div class="transfer-peer-info">
            <div class="transfer-peer-device">
              <Icon name="monitor" />
              <div>
                <strong>{{ names[myMember] || name }}</strong
                ><small class="muted">这台设备</small>
              </div>
            </div>
            <span class="transfer-peer-wire" aria-hidden="true"></span>
            <div class="transfer-peer-device">
              <Icon
                :name="
                  peerDevices[1 - myMember]?.type === 'mobile'
                    ? 'mobile'
                    : 'monitor'
                "
              />
              <div>
                <strong
                  >{{ connected ? "已连接" : "等待恢复" }}：{{
                    names[1 - myMember]
                  }}</strong
                >
                <span class="transfer-peer-state">{{
                  !connected
                    ? status === "reconnecting"
                      ? "连接中断 · 正在重新连接"
                      : "对方暂时离线 · 等待重新连接"
                    : direct
                      ? "已授权 · 点对点可用 · 中继可用"
                      : "已授权 · 中继可用"
                }}</span>
              </div>
            </div>
          </div>
          <div class="transfer-peer-actions">
            <UiButton
              type="button"
              class="quiet"
              size="small"
              :aria-expanded="peerDetailsOpen"
              aria-controls="transfer-peer-details"
              @click="peerDetailsOpen = !peerDetailsOpen"
              >连接详情</UiButton
            >
            <UiButton
              type="button"
              :class="connected ? 'quiet' : 'danger'"
              size="small"
              @click="leave"
              >断开连接</UiButton
            >
          </div>
        </div>
        <p v-if="!connected" class="muted" role="status">
          配对保留 5 分钟，请在
          {{ reconnectDeadline }} 前返回前台恢复连接。中断的文件需重新发送。
        </p>
        <dl
          v-show="peerDetailsOpen"
          id="transfer-peer-details"
          class="transfer-peer-details"
        >
          <div>
            <dt>来源 IP</dt>
            <dd>{{ peerIPs[1 - myMember] || "未提供" }}</dd>
          </div>
          <div v-if="peerLocations[1 - myMember]">
            <dt>IP 归属地</dt>
            <dd><IPLocation :location="peerLocations[1 - myMember]" /></dd>
          </div>
          <div>
            <dt>设备信息</dt>
            <dd>{{ peerDevices[1 - myMember]?.summary || "未提供" }}</dd>
          </div>
          <div>
            <dt>点对点地址</dt>
            <dd>{{ peerAddress }}</dd>
          </div>
        </dl>
      </div>
      <div class="transfer-workspace" :inert="!connected">
        <section class="panel transfer-chat">
          <div class="transfer-section-heading">
            <h2>消息</h2>
            <span class="muted">文字 / 图片</span>
          </div>
          <div
            class="transfer-messages"
            ref="messageList"
            @scroll="trackMessageScroll"
            role="log"
            aria-label="对话消息"
            aria-live="polite"
          >
            <div v-if="!messages.length" class="transfer-chat-empty muted">
              <Icon name="message" />
              <p>还没有消息<br />发送文字或粘贴图片开始对话</p>
            </div>
            <div
              v-for="message in messages"
              :key="message.id"
              class="transfer-message"
              :class="{ mine: message.from === myMember }"
            >
              <div class="transfer-message-header">
                <div class="transfer-message-meta">
                  <small>{{
                    message.from === myMember ? "我" : names[message.from]
                  }}</small>
                  <time
                    v-if="message.timeLabel"
                    :datetime="message.sent_at"
                    :title="message.timeTitle"
                    >{{ message.timeLabel }}</time
                  >
                </div>
                <UiButton
                  v-if="
                    message.file &&
                    ['failed', 'cancelled', 'complete'].includes(
                      message.file.state,
                    )
                  "
                  type="button"
                  class="quiet transfer-message-remove"
                  size="small"
                  icon="close"
                  :aria-label="`移除图片 ${message.file.name}`"
                  @click="removeTask(message.file)"
                />
              </div>
              <template v-if="message.file">
                <button
                  v-if="message.file.previewURL"
                  type="button"
                  class="transfer-image-open"
                  :aria-label="`查看图片 ${message.file.name}`"
                  @click="previewImage = message.file"
                >
                  <img
                    class="transfer-chat-image"
                    @load="scrollMessages"
                    :src="message.file.previewURL"
                    :alt="message.file.name"
                    @error="
                      message.file.imageError = '图片无法解码';
                      releasePreview(message.file);
                    "
                  />
                </button>
                <p v-else>
                  {{
                    message.file.imageError ||
                    (message.file.state === "complete"
                      ? "图片预览已释放"
                      : ["failed", "cancelled"].includes(message.file.state)
                        ? "图片未接收"
                        : "图片等待接收或接收中…")
                  }}
                </p>
                <div class="transfer-image-footer">
                  <span class="muted">{{ stateLabel(message.file) }}</span>
                  <UiButton
                    v-if="
                      connected &&
                      message.from === myMember &&
                      ['failed', 'cancelled'].includes(message.file.state) &&
                      !message.file.retried
                    "
                    size="small"
                    icon="retry"
                    :disabled="message.file.retrying"
                    @click="retry(message.file)"
                    >重新发送</UiButton
                  >
                </div>
              </template>
              <p v-else>{{ message.text }}</p>
            </div>
          </div>
          <form
            class="transfer-message-composer"
            @submit.prevent="sendText"
            @paste="pasteImage"
          >
            <UiInput
              v-model="text"
              type="textarea"
              aria-label="发送文字"
              placeholder="输入文字，或粘贴图片"
              :rows="1"
              :autosize="{ minRows: 1, maxRows: 4 }"
              maxlength="4096"
            />
            <div
              v-if="pastedImages.length"
              class="transfer-pasted-images"
              aria-label="待发送图片"
            >
              <div v-for="(image, index) in pastedImages" :key="image.url">
                <img :src="image.url" :alt="image.file.name" />
                <UiButton
                  type="button"
                  class="quiet"
                  size="small"
                  icon="close"
                  :aria-label="`移除待发送图片 ${index + 1}`"
                  @click="removePasted(index)"
                />
              </div>
            </div>
            <UiButton
              class="primary transfer-send"
              icon="send"
              :aria-label="pastedImages.length ? '发送消息' : '发送文字'"
              :title="pastedImages.length ? '发送消息' : '发送文字'"
              :disabled="pasting || (!text.trim() && !pastedImages.length)"
              ><span class="transfer-send-label">{{
                pastedImages.length ? "发送消息" : "发送文字"
              }}</span></UiButton
            >
          </form>
          <p class="muted transfer-hint">
            可粘贴 PNG / JPEG 图片，单张最多 4 MiB。
          </p>
        </section>

        <section class="panel transfer-file-panel">
          <div class="transfer-section-heading">
            <h2>文件互传</h2>
            <label
              class="check transfer-auto-receive"
              title="仅本次连接有效，按顺序中继下载"
              ><UiCheckbox
                v-model="autoReceive"
                aria-label="自动接收对方文件"
              />自动接收</label
            >
          </div>
          <div
            class="transfer-drop"
            :class="{ dragging: dragDepth > 0 }"
            @dragenter.prevent="dragDepth++"
            @dragleave.prevent="dragDepth = Math.max(0, dragDepth - 1)"
            @dragover.prevent
            @drop.prevent="dropFiles"
          >
            <span class="transfer-drop-icon"><Icon name="upload" /></span>
            <div class="transfer-drop-copy">
              <strong>{{
                dragDepth ? "松开即可添加文件" : "拖入文件，或点击选择"
              }}</strong
              ><span class="muted"
                >单文件最多 {{ size(maxFile) }} · 文件夹最多 1 万项</span
              >
            </div>
            <div class="transfer-drop-actions">
              <UiButton
                type="button"
                class="transfer-choose"
                @click="fileInput?.click()"
                >选择文件</UiButton
              >
              <UiButton
                type="button"
                class="transfer-choose"
                :disabled="folderSelecting || Boolean(selectedFolder)"
                @click="selectFolder()"
                >{{ folderSelecting ? "正在扫描…" : "选择文件夹" }}</UiButton
              >
            </div>
            <input
              ref="folderInput"
              hidden
              type="file"
              webkitdirectory
              multiple
              aria-label="选择互传文件夹"
              @change="chooseFolderFiles"
            />
            <input
              ref="fileInput"
              hidden
              type="file"
              multiple
              aria-label="选择互传文件"
              @change="choose"
            />
          </div>
          <section
            v-if="selectedFolder"
            class="transfer-queue"
            aria-label="待发送文件夹"
          >
            <h3 class="transfer-list-heading">待发送文件夹</h3>
            <div class="transfer-selected-folder">
              <Icon name="folder" />
              <div class="transfer-selected-folder-copy">
                <strong :title="selectedFolder.name">{{
                  selectedFolder.name
                }}</strong>
                <span class="muted"
                  >{{ selectedFolder.count }} 项 ·
                  {{ size(selectedFolder.size) }}</span
                >
              </div>
              <UiButton
                type="button"
                class="quiet"
                size="small"
                icon="close"
                aria-label="移除待发送文件夹"
                @click="selectedFolder = null"
              />
            </div>
            <UiButton
              class="primary"
              size="small"
              icon="upload"
              @click="offerFolder"
              >发送文件夹</UiButton
            >
          </section>
          <section
            v-if="selected.length"
            class="transfer-queue"
            aria-label="待发送文件"
          >
            <h3 class="transfer-list-heading">
              待发送 <span class="transfer-count">{{ selected.length }}</span>
            </h3>
            <ul class="transfer-selected">
              <li v-for="(file, index) in selected" :key="index">
                <Icon name="file" />
                <span class="transfer-selected-name">{{ file.name }}</span>
                <span class="transfer-selected-size">{{
                  size(file.size)
                }}</span>
                <UiButton
                  type="button"
                  class="quiet"
                  size="small"
                  icon="close"
                  title="移除待发送文件"
                  :aria-label="`移除 ${file.name}`"
                  @click="selected.splice(index, 1)"
                />
              </li>
            </ul>
            <UiButton class="primary" size="small" icon="upload" @click="offer"
              >发送所选文件（{{ selected.length }}）</UiButton
            >
          </section>
          <p
            v-if="!ordinaryFiles.length && !folders.length"
            class="muted transfer-empty"
          >
            暂无文件，发送或接收后将在这里显示。
          </p>
          <!-- 历史默认展开，原生 details 保留手动收起状态；有效的中继仍属于当前任务。 -->
          <component
            v-for="group in fileGroups"
            :key="group.key"
            :is="group.ended ? 'details' : 'section'"
            :open="group.ended"
            :class="group.ended ? 'transfer-history' : 'transfer-current'"
            :aria-label="group.label"
          >
            <summary v-if="group.ended">
              已结束的传输（{{ group.files.length + group.folders.length }}）
            </summary>
            <h3 v-else class="transfer-list-heading">
              当前任务
              <span class="transfer-count">{{
                group.files.length + group.folders.length
              }}</span>
            </h3>
            <div class="transfer-file-list">
              <article
                v-for="folder in group.folders"
                :key="folder.id"
                class="transfer-file transfer-folder"
                :data-folder-id="folder.id"
              >
                <div class="transfer-file-head">
                  <Icon name="folder" class="transfer-file-icon" />
                  <div class="transfer-file-copy">
                    <div class="transfer-file-title">
                      <strong>{{ folder.name }}</strong
                      ><span>{{ size(folder.size) }}</span>
                    </div>
                    <div class="transfer-file-meta">
                      <span class="muted">{{
                        folder.from === myMember ? "↑ 我发送的" : "↓ 对方发送的"
                      }}</span>
                      <span
                        class="transfer-tag"
                        :class="
                          folder.state === 'complete'
                            ? 'complete'
                            : folder.state === 'active'
                              ? 'active'
                              : 'neutral'
                        "
                        >{{ folderStateLabels[folder.state] }}</span
                      >
                      <span class="muted"
                        >{{ folder.completed }}/{{ folder.count }} 项</span
                      >
                      <span v-if="folder.mode" class="muted">{{
                        folder.mode === "p2p" ? "点对点" : "实时中继"
                      }}</span>
                    </div>
                    <time
                      class="muted transfer-folder-time"
                      :datetime="folder.sent_at"
                      >{{ folder.sentLabel }}</time
                    >
                  </div>
                </div>
                <template v-if="folder.state === 'active'">
                  <progress
                    :value="
                      folder.size ? folderBytes(folder) : folder.completed
                    "
                    :max="folder.size || folder.count"
                    :aria-label="`${folder.name} 文件夹传输进度`"
                  />
                  <p class="muted transfer-progress-text">
                    <span
                      >{{ size(folderBytes(folder)) }} /
                      {{ size(folder.size) }} · {{ size(folder.speed) }}/s</span
                    ><span>并发上限 {{ folder.concurrency }} 个</span>
                  </p>
                  <details
                    v-if="folderFiles(folder).length"
                    class="transfer-folder-files"
                    open
                  >
                    <summary>
                      文件进度（{{ folderFiles(folder).length }}）
                    </summary>
                    <ul>
                      <li
                        v-for="file in folderFiles(folder)"
                        :key="file.id"
                        class="transfer-folder-entry"
                      >
                        <div class="transfer-folder-entry-head">
                          <span :title="file.folder.path">{{
                            file.folder.path
                          }}</span>
                          <span class="muted">{{
                            file.state === "offered"
                              ? "准备中"
                              : `${filePercent(file)}%`
                          }}</span>
                        </div>
                        <progress
                          :value="file.bytes"
                          :max="file.size || 1"
                          :aria-label="`${file.folder.path} 传输进度`"
                        />
                        <p class="muted transfer-folder-entry-meta">
                          {{ size(file.bytes) }} / {{ size(file.size) }}
                        </p>
                      </li>
                    </ul>
                  </details>
                  <p v-else class="muted transfer-folder-path">
                    正在准备下一项…
                  </p>
                </template>
                <details
                  v-if="
                    ['offered', 'active'].includes(folder.state) &&
                    folder.from !== myMember
                  "
                  class="transfer-folder-options"
                >
                  <summary>接收选项</summary>
                  <label class="transfer-folder-concurrency">
                    <span>并发数</span>
                    <input
                      v-model.number="folder.concurrencyDraft"
                      :disabled="folder.updatingConcurrency"
                      type="range"
                      :min="1"
                      :max="folderMaxConcurrency"
                      :step="1"
                      aria-label="文件夹并发数"
                      :title="`范围 1–${folderMaxConcurrency} 个`"
                    />
                    <output>{{ folder.concurrencyDraft }} 个</output>
                  </label>
                  <p class="muted transfer-folder-range">
                    范围 1–{{ folderMaxConcurrency }} 个
                  </p>
                  <div
                    v-if="folder.state === 'active'"
                    class="transfer-folder-apply"
                  >
                    <UiButton
                      size="small"
                      :disabled="
                        folder.updatingConcurrency ||
                        Number(folder.concurrencyDraft) === folder.concurrency
                      "
                      @click="updateFolderConcurrency(folder)"
                      >{{
                        folder.updatingConcurrency ? "正在应用…" : "应用"
                      }}</UiButton
                    >
                    <span class="muted"
                      >调整后逐步生效，不中断正在传输的文件。</span
                    >
                  </div>
                </details>
                <div class="transfer-file-actions">
                  <template
                    v-if="
                      folder.state === 'offered' && folder.from !== myMember
                    "
                  >
                    <UiButton
                      class="primary"
                      size="small"
                      :disabled="
                        active || folder.accepting || !folderSavingSupported
                      "
                      @click="acceptFolder(folder, false)"
                      >中继接收文件夹</UiButton
                    >
                    <UiButton
                      v-if="canDirect && folderSavingSupported"
                      size="small"
                      :theme-overrides="directButtonTheme"
                      :disabled="active || folder.accepting"
                      @click="acceptFolder(folder, true)"
                      >点对点接收</UiButton
                    >
                  </template>
                  <UiButton
                    v-if="['active', 'offered'].includes(folder.state)"
                    class="quiet"
                    size="small"
                    @click="cancelFolder(folder)"
                    >{{
                      folder.from !== myMember && folder.state === "offered"
                        ? "拒绝接收"
                        : "取消传输"
                    }}</UiButton
                  >
                  <UiButton
                    v-else
                    class="quiet"
                    size="small"
                    @click="
                      folders = folders.filter((item) => item.id !== folder.id)
                    "
                    >移除记录</UiButton
                  >
                </div>
                <p
                  v-if="
                    folder.from !== myMember &&
                    folder.state === 'offered' &&
                    !folderSavingSupported
                  "
                  class="muted transfer-hint"
                >
                  接收文件夹需要 HTTPS，以及支持选择保存目录的浏览器。
                </p>
                <details
                  v-if="folder.recent.length"
                  class="transfer-folder-recent"
                >
                  <summary>
                    已处理条目（最近 {{ folder.recent.length }} 项）
                  </summary>
                  <ul>
                    <li v-for="(entry, index) in folder.recent" :key="index">
                      <span>{{ entry.path }}</span
                      ><span>{{ folderStateLabels[entry.state] }}</span>
                    </li>
                  </ul>
                </details>
              </article>
              <article
                v-for="file in group.files"
                :key="file.id"
                class="transfer-file"
                :data-transfer-id="file.id"
              >
                <div class="transfer-file-head">
                  <Icon name="file" class="transfer-file-icon" />
                  <div class="transfer-file-copy">
                    <div class="transfer-file-title">
                      <strong>{{ file.name }}</strong
                      ><span>{{ size(file.size) }}</span>
                      <UiButton
                        v-if="
                          ['cancelled', 'failed', 'complete'].includes(
                            file.state,
                          )
                        "
                        type="button"
                        class="quiet transfer-remove"
                        size="small"
                        icon="close"
                        title="移除记录"
                        :aria-label="`移除任务 ${file.name}`"
                        @click="removeTask(file)"
                      />
                    </div>
                    <div class="transfer-file-meta">
                      <span class="muted transfer-direction"
                        ><span aria-hidden="true">{{
                          file.from === myMember ? "↑" : "↓"
                        }}</span>
                        {{
                          file.from === myMember ? "我发送的" : "对方发送的"
                        }}</span
                      >
                      <span
                        class="transfer-tag"
                        :class="fileStatus(file).kind"
                        >{{ fileStatus(file).label }}</span
                      >
                      <!-- 只给任务状态着色，传输方式使用文字，减少重复色块。 -->
                      <span v-if="file.mode" class="muted transfer-mode">
                        {{ file.mode === "p2p" ? "点对点" : "实时中继" }}</span
                      >
                      <time
                        v-if="file.sentLabel"
                        class="muted transfer-file-time"
                        :datetime="file.sent_at"
                        :title="file.sentLabel"
                        :aria-label="`发送时间 ${file.sentLabel}`"
                        >发送于 {{ file.sentTimeLabel }}</time
                      >
                    </div>
                  </div>
                </div>
                <p
                  v-if="file.state === 'failed' && fileStatus(file).detail"
                  class="muted transfer-state-detail"
                >
                  {{ fileStatus(file).detail }}
                </p>
                <progress
                  v-if="file.state === 'active'"
                  :value="file.bytes"
                  :max="file.size || 1"
                  :aria-label="`${file.name} 传输进度`"
                ></progress>
                <p
                  v-if="
                    file.state === 'active' &&
                    (!file.url || file.urlUsed || file.mode === 'tool')
                  "
                  class="muted transfer-progress-text"
                >
                  <span>{{ size(file.bytes) }} / {{ size(file.size) }}</span>
                  <span
                    >{{
                      file.size
                        ? Math.min(
                            100,
                            Math.round((file.bytes / file.size) * 100),
                          )
                        : 0
                    }}% · {{ size(file.speed) }}/s</span
                  >
                </p>
                <div class="transfer-file-actions">
                  <UiButton
                    v-if="
                      connected &&
                      file.from === myMember &&
                      ['failed', 'cancelled'].includes(file.state) &&
                      !file.retried
                    "
                    class="transfer-action"
                    size="small"
                    icon="retry"
                    :disabled="active || file.retrying"
                    @click="retry(file)"
                    >{{ file.retrying ? "等待重发" : "重新发送" }}</UiButton
                  >
                  <template
                    v-if="file.state === 'offered' && file.from !== myMember"
                  >
                    <UiButton
                      class="primary transfer-action"
                      size="small"
                      :disabled="active || file.accepting"
                      @click="accept(file, false)"
                      >中继下载</UiButton
                    >
                    <UiButton
                      v-if="canDirect"
                      class="transfer-action"
                      size="small"
                      :theme-overrides="directButtonTheme"
                      :disabled="active || file.accepting"
                      @click="accept(file, true)"
                      >点对点下载</UiButton
                    >
                  </template>
                  <template v-if="file.url && file.mode === 'tool'">
                    <UiButton
                      tag="a"
                      class="primary transfer-action"
                      size="small"
                      :href="file.url"
                      :download="file.name"
                      referrerpolicy="no-referrer"
                      @click="file.requested = true"
                      >{{
                        file.completedRound > 0 &&
                        file.completedRound === file.progressRound
                          ? "重新下载"
                          : "下载文件"
                      }}</UiButton
                    >
                    <UiButton
                      class="transfer-action"
                      size="small"
                      @click="copyDownloadLink(file)"
                    >
                      {{
                        file.linkCopied ? "已复制链接" : "复制下载链接"
                      }}</UiButton
                    >
                    <UiButton
                      class="transfer-action"
                      size="small"
                      :disabled="
                        !file.completedRound ||
                        file.completedRound !== file.progressRound
                      "
                      title="结束传输后下载链接失效"
                      @click="send({ type: 'finish-download', id: file.id })"
                      >结束传输</UiButton
                    >
                  </template>
                  <span v-else-if="file.url" class="muted"
                    >已请求下载，请查看下载记录</span
                  >
                  <UiButton
                    v-if="['offered', 'active'].includes(file.state)"
                    class="transfer-action"
                    size="small"
                    @click="send({ type: 'cancel', id: file.id })"
                    >{{
                      file.state === "offered" && file.from !== myMember
                        ? "拒绝接收"
                        : file.mode === "tool"
                          ? "取消传输"
                          : "取消"
                    }}</UiButton
                  >
                </div>
                <details
                  v-if="file.url && file.mode === 'tool'"
                  class="transfer-tool-link"
                  :open="file.linkExpanded"
                  @toggle="file.linkExpanded = $event.target.open"
                >
                  <summary>下载链接与说明</summary>
                  <UiInput
                    :model-value="file.url"
                    readonly
                    :aria-label="`${file.name} 下载链接`"
                    @focus="$event.target.select()"
                  />
                  <p class="transfer-hint muted">
                    已自动发起下载，也可复制链接到下载工具（支持 AB
                    扩展接管）。最多
                    {{ file.maxConnections }} 路连接，链接到
                    {{ file.expiresLabel }}
                    有效。每次完整下载都会记录日志；点击“结束传输”后链接失效。
                  </p>
                </details>
              </article>
            </div>
          </component>
        </section>
      </div>
    </template>
    <!-- 常用操作就近提示，详细限制集中折叠，避免传输卡片被说明文字占满。 -->
    <details
      class="panel transfer-help"
      :class="{ 'transfer-help-connected': hasPair }"
    >
      <summary>使用说明</summary>
      <dl class="transfer-help-content">
        <dt>文字与图片</dt>
        <dd>
          图片通过中继发送，支持 PNG / JPEG，单张最多 4 MiB、419
          万像素，长边不超过 4096。最近 4 张图片可预览，保留最近 100
          条消息，关闭后清除；较早的图片请及时保存，大图可作为文件发送。
        </dd>
        <dt>文件接收</dt>
        <dd>
          文件逐个接收。点击“中继下载”即开始下载，也可复制链接到下载工具，中继边传边下载；“点对点下载”取决于浏览器支持。开启自动接收后直接启动中继下载，浏览器仍可能询问保存位置或多文件下载许可。
        </dd>
        <dt>文件夹接收</dt>
        <dd>
          选择文件夹后整体发送，接收方选择保存目录，保留目录结构。文件并发传输，默认
          16 个，可调整至 128
          个；超过并发额度的文件自动排队，不受普通文件每次连接 32
          个的累计限制；每个文件夹最多 1
          万个文件和空目录，仍沿用单文件大小和本次连接总大小上限。同名目录不会自动覆盖。需要
          HTTPS
          和支持目录写入的浏览器；扫描和传输期间请勿修改源文件夹。文件夹需单独确认，不随“自动接收”写入磁盘。
        </dd>
        <dt>连接与隐私</dt>
        <dd>
          每次连接仅限两台设备。意外断线后保留配对 5
          分钟，返回前台自动重连，中断的文件需重新发送。切到后台可能暂停传输；刷新、主动离开或超过恢复期限需重新配对。本站不保存中继文件，但服务端可接触内容；直连可能让对方得知网络地址。
        </dd>
        <dt>下载工具</dt>
        <dd>
          点击“中继下载”后自动开始下载，也可将链接交给 AB Download Manager
          等软件下载。支持文件探测、分段和本次连接内重试，使用实时中继，双方页面必须在线。链接最长
          30 分钟有效，停用 10
          分钟会失效；点击“结束传输”后链接失效。取消传输也会撤销链接。链接包含临时下载权限，请勿转发。自动接收仍使用一次性链接。
        </dd>
      </dl>
    </details>
    <NModal
      :show="!!previewImage?.previewURL"
      preset="card"
      title="图片预览"
      class="transfer-image-preview"
      :style="{ width: 'min(920px, calc(100vw - 32px))' }"
      role="dialog"
      aria-modal="true"
      aria-label="图片预览"
      @update:show="
        (value) => {
          if (!value) previewImage = null;
        }
      "
      @close="previewImage = null"
    >
      <!-- 使用普通图片保留浏览器右键保存功能，预览复用已有 Blob，不再下载一份。 -->
      <div class="transfer-image-toolbar">
        <UiButton
          type="button"
          size="small"
          aria-label="缩小图片"
          :disabled="previewScale <= 0.25"
          @click="zoomPreview(previewScale / 1.25)"
          >−</UiButton
        >
        <span aria-live="polite">{{ Math.round(previewScale * 100) }}%</span>
        <UiButton
          type="button"
          size="small"
          aria-label="放大图片"
          :disabled="previewScale >= 8"
          @click="zoomPreview(previewScale * 1.25)"
          >+</UiButton
        >
        <UiButton type="button" size="small" @click="resetPreview"
          >适应窗口</UiButton
        >
      </div>
      <div
        class="transfer-image-full"
        ref="previewViewport"
        @wheel.prevent="
          zoomPreview(previewScale * ($event.deltaY < 0 ? 1.1 : 1 / 1.1))
        "
        @pointerdown="startPreviewDrag"
        @pointermove="movePreview"
        @pointerup="endPreviewDrag"
        @pointercancel="endPreviewDrag"
        @lostpointercapture="endPreviewDrag"
      >
        <img
          v-if="previewImage?.previewURL"
          :src="previewImage.previewURL"
          :alt="previewImage.name"
          :style="{
            transform: `translate(${previewOffset.x}px, ${previewOffset.y}px) scale(${previewScale})`,
          }"
          draggable="false"
        />
      </div>
    </NModal>
  </div>
</template>

<style scoped>
.transfer-page {
  /* 互传页独立调整宽度和字号，避免影响文本分享与后台页面。 */
  max-width: 1200px;
  margin: 0 auto;
  font-size: 14px;
  --transfer-blue: #1f5da0;
  --transfer-blue-bg: #eaf2ff;
  --transfer-teal: #0b716b;
  --transfer-teal-bg: #e5f5f2;
  --transfer-neutral: #52635b;
  --transfer-neutral-bg: #f0f2f3;
}
/* 标签的文字和背景一起切换，暗色界面仍保留足够对比度。 */
:global(html[data-theme="dark"] .transfer-page) {
  --transfer-blue: #acccff;
  --transfer-blue-bg: #223349;
  --transfer-teal: #91ded4;
  --transfer-teal-bg: #203b38;
  --transfer-neutral: #b4c4ba;
  --transfer-neutral-bg: #29322e;
}
.transfer-entry-grid,
.transfer-workspace {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 20px;
}
.transfer-entry-grid {
  margin-top: 20px;
}
.transfer-entry-card {
  border: 1px solid var(--line);
  border-radius: 12px;
  padding: 20px;
  display: flex;
  flex-direction: column;
  align-items: stretch;
  gap: 12px;
}
.transfer-entry-card h2,
.transfer-entry-card p {
  margin: 0;
}
/* 两栏内容长短不同，操作按钮仍在卡片底部对齐。 */
.transfer-entry-card > .ui-button {
  margin-top: auto;
}
.transfer-code-field > label {
  margin: 0 0 8px;
}
.transfer-code-input {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 8px;
  min-width: 0;
}
.transfer-code-input .pair-code {
  flex: 1 1 220px;
  min-width: 0;
}
.transfer-code-input .ui-button {
  flex-shrink: 0;
}
.transfer-code-actions {
  display: flex;
  gap: 8px;
  margin-left: auto;
}
.transfer-note {
  font-size: 12px;
  line-height: 1.7;
  margin-top: 18px;
}
.transfer-connection-settings {
  margin: 16px 0 20px;
  min-width: 0;
}
.transfer-connection-settings > summary {
  cursor: pointer;
  color: var(--muted);
  font-size: 13px;
  font-weight: 600;
}
.transfer-connection-settings > summary:focus-visible {
  outline: 2px solid var(--accent);
  outline-offset: 4px;
}
.transfer-connection-fields {
  display: grid;
  gap: 12px;
  max-width: 560px;
  padding-top: 16px;
}
.transfer-connection-fields > label,
.transfer-connection-fields > p {
  margin: 0;
}
.transfer-waiting {
  text-align: center;
}
.transfer-auto-approve {
  margin: 20px 0;
}
.transfer-auto-approve > label {
  justify-content: center;
  margin-bottom: 6px;
}
.transfer-auto-approve > p {
  margin: 0;
}
.transfer-code {
  font-size: clamp(24px, 6vw, 40px);
  font-family: ui-monospace, monospace;
  letter-spacing: 0.1em;
  user-select: all;
}
.transfer-joining {
  margin: 20px 0;
  padding: 16px;
  border: 1px solid var(--line);
  border-radius: 12px;
}
.transfer-peer-bar,
.transfer-file-title,
.transfer-selected li {
  display: flex;
  justify-content: space-between;
  align-items: center;
  gap: 12px;
}
.transfer-page > .transfer-peer {
  max-width: none;
  margin-bottom: 16px;
  padding: 16px;
}
.transfer-peer-bar {
  flex-wrap: wrap;
}
.transfer-peer-actions {
  display: flex;
  gap: 8px;
}
.transfer-peer-info {
  flex: 1;
  min-width: 0;
}
.transfer-peer-info strong {
  display: inline-flex;
  align-items: center;
  gap: 8px;
  min-width: 0;
  font-size: 14px;
}
.transfer-peer-device {
  display: flex;
  align-items: center;
  gap: 12px;
  min-width: 0;
}
.transfer-peer-device > .icon {
  width: 40px;
  height: 40px;
  padding: 9px;
  color: var(--muted);
  border: 1px solid var(--line);
  border-radius: 12px;
  background: var(--surface-subtle);
}
.transfer-peer-device > div {
  min-width: 0;
}
.transfer-peer-device small,
.transfer-peer-device .transfer-peer-state {
  display: block;
  font-size: 12px;
  margin-top: 2px;
}
.transfer-peer-wire {
  position: relative;
  width: 90px;
  height: 2px;
  flex-shrink: 0;
  background: repeating-linear-gradient(
    90deg,
    var(--accent) 0 6px,
    transparent 6px 11px
  );
}
.transfer-peer-wire::after {
  content: "";
  position: absolute;
  width: 10px;
  height: 10px;
  top: -4px;
  left: calc(50% - 5px);
  border-radius: 50%;
  background: var(--accent);
  box-shadow: 0 0 0 5px var(--soft);
}
.transfer-peer-state {
  color: var(--accent);
  font-size: 12px;
}
.transfer-peer > .transfer-peer-details {
  border-top: 1px solid var(--line);
  padding-top: 12px;
}
.transfer-peer-details {
  display: flex;
  flex-wrap: wrap;
  gap: 8px 24px;
  margin: 12px 0 0;
  text-align: left;
}
.transfer-peer-details > div {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
  min-width: 0;
}
.transfer-peer-details dt {
  color: var(--muted);
  font-size: 12px;
}
.transfer-peer-details dd {
  margin: 0;
  font-family: ui-monospace, monospace;
  font-size: 13px;
  overflow-wrap: anywhere;
}
.transfer-peer-info {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 8px 16px;
  min-width: 0;
  overflow-wrap: anywhere;
}
.transfer-hint {
  margin: 12px 0 0;
  font-size: 12px;
  line-height: 1.7;
}
.transfer-tool-link {
  margin-top: 10px;
  min-width: 0;
}
.transfer-tool-link .ui-input {
  margin-top: 10px;
}
.transfer-empty {
  margin: 20px 0 0;
  padding: 16px;
  text-align: center;
  font-size: 13px;
  background: var(--surface-subtle);
  border-radius: 8px;
}
.transfer-page > .transfer-help {
  /* 连接前沿用表单宽度，连接后才随双栏工作区展开。 */
  max-width: 960px;
  margin: 20px auto 0;
  padding: 16px 20px;
}
.transfer-page > .transfer-help-connected {
  max-width: none;
}
.transfer-help summary {
  cursor: pointer;
  font-size: 13px;
  font-weight: 600;
  color: var(--muted);
}
.transfer-help summary:focus-visible {
  outline: 2px solid var(--accent);
  outline-offset: 4px;
}
.transfer-help-content {
  display: grid;
  grid-template-columns: 88px minmax(0, 1fr);
  gap: 12px 16px;
  margin: 16px 0 0;
  font-size: 13px;
  line-height: 1.75;
}
.transfer-help-content dt {
  font-weight: 600;
}
.transfer-help-content dd {
  margin: 0;
  color: var(--muted);
}
.transfer-workspace {
  align-items: start;
  grid-template-columns: minmax(0, 1fr) 400px;
  gap: 16px;
}
.transfer-workspace .panel {
  min-width: 0;
  width: 100%;
  margin: 0;
  padding: 20px;
}
.transfer-section-heading {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  justify-content: space-between;
  gap: 8px 12px;
}
.transfer-section-heading h2 {
  margin: 0;
  font-size: 15px;
}
.transfer-section-heading > .muted {
  font-size: 12px;
}
.transfer-messages {
  height: auto;
  min-height: 0;
  flex: 1;
  overflow: auto;
  padding: 18px;
  display: flex;
  flex-direction: column;
  gap: 12px;
  background: radial-gradient(circle at 1px 1px, var(--line) 1px, transparent 0)
    0 0 / 18px 18px;
}
/* 消息区和发送栏独立留白，长记录只在消息区内滚动。 */
.transfer-workspace .transfer-chat {
  display: flex;
  flex-direction: column;
  padding: 0;
  height: 600px;
  overflow: hidden;
}
.transfer-chat .transfer-section-heading {
  padding: 14px 18px;
  border-bottom: 1px solid var(--line);
}
.transfer-chat > .transfer-hint {
  padding: 0 18px 12px;
  margin: 0;
  font-size: 11px;
}
.transfer-chat-empty {
  flex: 1;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 10px;
  text-align: center;
  font-size: 12px;
}
.transfer-chat-empty .icon {
  width: 24px;
  height: 24px;
  opacity: 0.65;
}
.transfer-chat-empty p {
  margin: 0;
  line-height: 1.8;
}
.transfer-message {
  /* 每条消息保持自身高度，消息区滚动时不会压缩或遮住图片的操作栏。 */
  flex-shrink: 0;
  min-width: 0;
  max-width: 78%;
  border: 1px solid var(--line);
  border-radius: 16px 16px 16px 5px;
  padding: 10px 14px;
  background: var(--surface);
  align-self: flex-start;
}
.transfer-message.mine {
  align-self: flex-end;
  border-radius: 16px 16px 5px 16px;
  border-color: transparent;
  background: var(--primary);
  color: var(--on-primary);
}
.transfer-message.mine .transfer-message-meta,
.transfer-message.mine .muted {
  color: inherit;
  opacity: 0.7;
}
.transfer-message.mine .transfer-message-remove {
  color: inherit;
}
.transfer-message small {
  opacity: 0.7;
}
.transfer-message-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
}
.transfer-message-meta {
  display: flex;
  align-items: baseline;
  flex-wrap: wrap;
  gap: 6px;
  min-width: 0;
  font-size: 12px;
  color: var(--muted);
  overflow-wrap: anywhere;
}
.transfer-message-meta time {
  font-size: 11px;
  white-space: nowrap;
}
.transfer-message-remove {
  width: 24px;
  height: 24px;
  padding: 0;
}
.transfer-image-open {
  display: block;
  max-width: 100%;
  margin-top: 6px;
  padding: 0;
  border: 0;
  border-radius: 6px;
  background: transparent;
  cursor: zoom-in;
}
.transfer-image-open:focus-visible {
  outline: 2px solid var(--accent);
  outline-offset: 3px;
}
.transfer-message p {
  white-space: pre-wrap;
  overflow-wrap: anywhere;
  margin: 4px 0;
}
.transfer-chat-image {
  display: block;
  max-width: 100%;
  max-height: 180px;
  width: auto;
  height: auto;
  object-fit: contain;
  border-radius: 6px;
}
.transfer-image-footer {
  display: flex;
  align-items: center;
  justify-content: space-between;
  flex-wrap: wrap;
  gap: 8px;
  margin-top: 8px;
  font-size: 12px;
}
.transfer-image-toolbar {
  display: flex;
  justify-content: center;
  align-items: center;
  flex-wrap: wrap;
  gap: 8px;
  margin-bottom: 12px;
}
.transfer-image-toolbar span {
  min-width: 48px;
  text-align: center;
  font-size: 12px;
  font-variant-numeric: tabular-nums;
}
.transfer-image-full {
  display: flex;
  justify-content: center;
  align-items: center;
  height: min(60vh, 600px);
  overflow: hidden;
  touch-action: none;
  cursor: grab;
  background: var(--surface-subtle);
  border-radius: 8px;
}
.transfer-image-full:active {
  cursor: grabbing;
}
.transfer-image-full img {
  display: block;
  max-width: 100%;
  max-height: 100%;
  width: auto;
  height: auto;
  object-fit: contain;
  flex-shrink: 0;
}
.transfer-pasted-images {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
}
.transfer-pasted-images > div {
  display: flex;
  align-items: center;
  border: 1px solid var(--line);
  border-radius: 8px;
  padding: 4px;
}
.transfer-pasted-images img {
  width: 64px;
  height: 64px;
  object-fit: contain;
}
.transfer-chat form {
  display: grid;
  grid-template-columns: minmax(0, 1fr) auto;
  align-items: end;
  gap: 8px;
  padding: 12px;
  margin: 0;
  border-top: 1px solid var(--line);
}
.transfer-message-composer .transfer-pasted-images {
  grid-column: 1 / -1;
  grid-row: 2;
}
.transfer-send {
  grid-column: 2;
  grid-row: 1;
  width: 44px;
  height: 44px;
  padding: 0;
}
.transfer-send :deep(.n-button__icon) {
  margin: 0;
}
.transfer-send-label {
  position: absolute;
  width: 1px;
  height: 1px;
  overflow: hidden;
  clip-path: inset(50%);
}
.transfer-selected-folder {
  display: grid;
  grid-template-columns: 20px minmax(0, 1fr) auto;
  align-items: start;
  gap: 8px;
  margin-bottom: 12px;
}
.transfer-selected-folder > .icon {
  margin-top: 2px;
}
.transfer-selected-folder-copy {
  min-width: 0;
}
/* 名称单行省略，完整名称通过 title 查看；移除按钮使用独立列，避免被长名称挤走。 */
.transfer-selected-folder-copy > strong {
  display: block;
  overflow: hidden;
  white-space: nowrap;
  text-overflow: ellipsis;
  font-size: 13px;
  font-weight: 600;
}
.transfer-selected-folder-copy > span {
  display: block;
  margin-top: 4px;
  font-size: 12px;
}
.transfer-folder-time,
.transfer-folder-path,
.transfer-folder-recent {
  font-size: 12px;
  overflow-wrap: anywhere;
}
.transfer-folder-time {
  display: block;
  margin-top: 6px;
}
.transfer-folder-recent {
  margin-top: 12px;
}
.transfer-folder-recent ul {
  list-style: none;
  padding: 0;
}
.transfer-folder-recent li {
  display: flex;
  justify-content: space-between;
  gap: 12px;
  margin-top: 6px;
}
.transfer-folder-recent li > span:last-child {
  flex-shrink: 0;
  color: var(--muted);
}
/* 两种选择方式单独成行，避免按钮把文件说明挤窄。 */
.transfer-drop-actions {
  display: flex;
  gap: 8px;
  width: 100%;
  justify-content: flex-end;
}
.transfer-drop {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 12px;
  margin: 16px 0 18px;
  border: 1px dashed var(--line);
  border-radius: 9px;
  padding: 14px;
  background: var(--surface-subtle);
  transition:
    background 0.15s,
    border-color 0.15s;
}
.panel .transfer-auto-receive {
  margin: 0;
  gap: 6px;
  font-size: 12px;
  color: var(--muted);
}
.transfer-drop.dragging,
.transfer-drop:focus-within {
  background: color-mix(in srgb, var(--accent) 10%, var(--surface));
  outline: 2px solid var(--accent);
  outline-offset: 2px;
}
.transfer-drop-icon {
  display: grid;
  place-items: center;
  width: 24px;
  height: 28px;
  flex-shrink: 0;
  color: var(--accent);
}
.transfer-drop-icon .icon {
  width: 20px;
  height: 20px;
}
.transfer-drop-copy {
  display: grid;
  gap: 3px;
  flex: 1;
  min-width: 0;
}
.transfer-drop-copy strong {
  font-size: 13px;
}
.transfer-drop-copy span {
  font-size: 12px;
}
.transfer-drop input[hidden] {
  display: none;
}
.transfer-remove {
  flex-shrink: 0;
}
.transfer-queue {
  padding-bottom: 16px;
  margin-bottom: 18px;
  border-bottom: 1px solid var(--line);
}
.transfer-list-heading {
  display: flex;
  gap: 7px;
  align-items: center;
  margin: 0 0 12px;
  font-size: 12px;
  font-weight: 400;
  color: var(--muted);
}
.transfer-count {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  min-width: 18px;
  height: 18px;
  padding: 0 4px;
  border-radius: 5px;
  font-size: 11px;
  background: var(--surface-subtle);
  font-variant-numeric: tabular-nums;
}
.transfer-history {
  border-top: 1px solid var(--line);
  padding-top: 14px;
  margin-top: 20px;
}
.transfer-history[open] > summary {
  margin-bottom: 16px;
}
.transfer-history > summary,
.transfer-tool-link > summary {
  cursor: pointer;
  font-size: 12px;
  color: var(--muted);
}
.transfer-history > summary:focus-visible,
.transfer-tool-link > summary:focus-visible {
  outline: 2px solid var(--accent);
  outline-offset: 4px;
}
.transfer-state-detail {
  margin-top: 8px;
}
@media (max-width: 600px) {
  .transfer-help-content {
    grid-template-columns: minmax(0, 1fr);
    gap: 4px;
  }
  .transfer-help-content dd + dt {
    margin-top: 8px;
  }
  .transfer-workspace .panel {
    padding: 16px;
  }
  .transfer-file-meta .transfer-file-time {
    margin-left: 0;
  }
}
.transfer-selected {
  list-style: none;
  padding: 0;
  margin: 0 0 12px;
}
.transfer-selected li {
  gap: 8px;
  padding: 4px 0;
  font-size: 12px;
}
.transfer-selected .icon {
  flex-shrink: 0;
  color: var(--muted);
  width: 16px;
  height: 18px;
}
.transfer-selected-name,
.transfer-file-title strong {
  overflow-wrap: anywhere;
  min-width: 0;
  flex: 1;
}
.transfer-selected-size,
.transfer-file-title span {
  flex-shrink: 0;
  font-size: 12px;
  color: var(--muted);
  font-variant-numeric: tabular-nums;
}
.transfer-file-title {
  align-items: flex-start;
  gap: 10px;
}
.transfer-file-title strong {
  font-size: 13px;
  font-weight: 600;
}
.transfer-file {
  border: 1px solid var(--line);
  border-radius: 12px;
  padding: 12px;
  min-width: 0;
  background: var(--surface);
}
.transfer-file-list {
  display: grid;
  gap: 10px;
}
.transfer-file-head {
  display: flex;
  align-items: flex-start;
  gap: 10px;
}
.transfer-file-icon {
  color: var(--accent);
  flex-shrink: 0;
  width: 34px;
  height: 34px;
  padding: 8px;
  border-radius: 9px;
  background: var(--soft);
}
.transfer-file-copy {
  flex: 1;
  min-width: 0;
}
.transfer-file p {
  margin: 6px 0;
  font-size: 12px;
}
.transfer-file-meta {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 6px 8px;
  margin-top: 6px;
  font-size: 11px;
}
.transfer-direction {
  font-size: 11px;
}
.transfer-file-time {
  margin-left: auto;
  white-space: nowrap;
  font-size: 11px;
  font-variant-numeric: tabular-nums;
}
.transfer-file .transfer-tag {
  padding: 1px 7px;
  border-radius: 5px;
  font-size: 11px;
  font-weight: 500;
}
.transfer-tag {
  display: inline-flex;
  align-items: center;
  padding: 3px 8px;
  border-radius: 6px;
  font-size: 12px;
  font-weight: 600;
  line-height: 18px;
  white-space: nowrap;
}
.transfer-tag.neutral {
  color: var(--transfer-neutral);
  background: var(--transfer-neutral-bg);
}
.transfer-tag.active,
.transfer-tag.relay {
  color: var(--transfer-blue);
  background: var(--transfer-blue-bg);
}
.transfer-tag.complete {
  color: var(--success);
  background: var(--success-bg);
}
.transfer-tag.failed {
  color: var(--error);
  background: var(--error-bg);
}
.transfer-tag.p2p {
  color: var(--transfer-teal);
  background: var(--transfer-teal-bg);
}
.transfer-file progress {
  display: block;
  width: 100%;
  height: 5px;
  margin: 12px 0 6px;
  border: 0;
  border-radius: 8px;
  overflow: hidden;
  accent-color: var(--accent);
  appearance: none;
  background: var(--line);
}
.transfer-file progress::-webkit-progress-bar {
  background: var(--line);
}
.transfer-file progress::-webkit-progress-value {
  background: var(--accent);
  border-radius: 8px;
}
.transfer-file progress::-moz-progress-bar {
  background: var(--accent);
}
.transfer-file .transfer-progress-text {
  display: flex;
  flex-wrap: wrap;
  justify-content: space-between;
  gap: 4px 12px;
  font-size: 11px;
  font-variant-numeric: tabular-nums;
}
.transfer-file-actions {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-wrap: wrap;
  margin-top: 12px;
}
.transfer-file-actions:empty {
  display: none;
}
/* 文件操作保持相同高度，宽度随文案变化，避免短按钮留白过多。 */
.transfer-file-actions .transfer-action {
  padding: 0 12px;
}
.transfer-download {
  font-size: 14px;
  text-decoration: underline;
}
@media (max-width: 900px) {
  .transfer-entry-grid,
  .transfer-workspace {
    grid-template-columns: 1fr;
  }
  .transfer-workspace .transfer-chat {
    height: 540px;
  }
  .transfer-peer-wire {
    display: none;
  }
  .transfer-peer-info {
    gap: 14px;
  }
  /* 小屏先展示完整设备信息，再排列操作按钮，避免名称被挤成逐字换行。 */
  .transfer-peer-bar {
    flex-direction: column;
    align-items: stretch;
  }
  .transfer-peer-device {
    flex: 1 1 200px;
  }
  .transfer-peer-actions {
    justify-content: flex-end;
  }
  .transfer-drop {
    flex-wrap: wrap;
    justify-content: flex-end;
  }
  .transfer-drop-copy {
    flex-basis: 160px;
  }
  .transfer-message {
    max-width: 90%;
  }
}
</style>

<style scoped>
.transfer-folder-options {
  margin-top: 12px;
  font-size: 13px;
}
.transfer-folder-options > summary {
  width: fit-content;
  color: var(--muted);
  cursor: pointer;
}
.transfer-folder-options > summary:focus-visible {
  outline: 2px solid var(--accent);
  outline-offset: 3px;
  border-radius: 3px;
}
/* 并发设置只在展开后显示，并覆盖通用表单标签的纵向排版。 */
.transfer-folder-options .transfer-folder-concurrency {
  display: flex;
  flex-direction: row;
  align-items: center;
  gap: 8px;
  margin: 10px 0 0;
  font-size: 13px;
}
.transfer-folder-concurrency > span,
.transfer-folder-concurrency > output {
  flex-shrink: 0;
}
.transfer-folder-concurrency > output {
  min-width: 42px;
  text-align: right;
  font-variant-numeric: tabular-nums;
}
.transfer-folder-concurrency > input[type="range"] {
  flex: 1;
  min-width: 0;
  height: 28px;
  margin: 0;
  padding: 0;
  border: 0;
  background: transparent;
  accent-color: var(--accent);
  cursor: pointer;
}
.transfer-folder-concurrency > input:disabled {
  cursor: default;
  opacity: 0.5;
}
.transfer-folder-range {
  margin: 2px 0 0;
  font-size: 12px;
}
.transfer-folder-files {
  margin-top: 12px;
  font-size: 12px;
}
.transfer-folder-files > summary {
  color: var(--muted);
  cursor: pointer;
}
/* 活动文件有上限，列表内滚动，避免并发较多时撑长整个页面。 */
.transfer-folder-files > ul {
  max-height: 280px;
  overflow-y: auto;
  overscroll-behavior: contain;
  list-style: none;
  margin: 8px 0 0;
  padding: 0 8px 0 0;
}
.transfer-folder-entry + .transfer-folder-entry {
  margin-top: 10px;
  padding-top: 10px;
  border-top: 1px solid var(--line);
}
.transfer-folder-entry-head {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 12px;
}
.transfer-folder-entry-head > span:first-child {
  min-width: 0;
  overflow-wrap: anywhere;
}
.transfer-folder-entry-head > span:last-child {
  flex-shrink: 0;
  font-variant-numeric: tabular-nums;
}
.transfer-folder-entry progress {
  margin-top: 6px;
}
.transfer-folder-entry-meta {
  margin: 0;
  font-size: 11px;
  font-variant-numeric: tabular-nums;
}
.transfer-folder-apply {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 8px;
  margin-top: 10px;
  font-size: 12px;
}
</style>
