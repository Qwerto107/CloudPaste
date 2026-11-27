<script setup>
import {
  computed,
  h,
  markRaw,
  nextTick,
  onMounted,
  onBeforeUnmount,
  ref,
  watch,
} from "vue";
import { useNotification } from "naive-ui";
import { onBeforeRouteLeave, useRoute, useRouter } from "vue-router";
import { session, canTransfer } from "./session.js";
import { settings, loadSettings } from "./settings.js";
import Icon from "./Icon.vue";
import MobileSheet from "./ui/MobileSheet.vue";
import TransferEntry from "./transfer/TransferEntry.vue";
import TransferWaiting from "./transfer/TransferWaiting.vue";
import TransferPeerBar from "./transfer/TransferPeerBar.vue";
import TransferDetailsPanel from "./transfer/TransferDetailsPanel.vue";
import TransferFilePanel from "./transfer/TransferFilePanel.vue";
import TransferImagePreviewModal from "./transfer/TransferImagePreviewModal.vue";
import "./transfer/transfer.css";
import { useMobile } from "./ui/useMobile.js";
import { useConfirmation } from "./ui/confirm.js";
import { fileDisplay } from "./file-display.js";
import { parseSTUNServers } from "./transfer-stun.js";
import {
  readHistoryImages,
  writeHistoryImages,
} from "./transfer-history-images.js";
import {
  canSaveFolders,
  readFolder,
  folderFromFiles,
  folderMaxConcurrency,
} from "./transfer-folders.js";
import { chatImageHistory, validateChatImage } from "./transfer-images.js";
import { collectTransferDevice, defaultTransferName } from "./transfer-device.js";
import {
  normalizePairingCode,
  validPairingCode,
} from "./transfer-code.js";
import {
  TransferClient,
  canSaveDirectly,
  transferID,
} from "./transfer-client.js";

// 每个页面独立生成后缀，识别到型号后只更新自动名称。
const nameSuffix = parseInt(transferID().slice(0, 8), 16)
  .toString(36)
  .toUpperCase()
  .padStart(4, "0")
  .slice(-4);
const customNameStorageKey = "cloudpaste-transfer-device-name";
const generatedName = ref(defaultTransferName(nameSuffix));
const customName = ref(null);
try {
  const savedName = localStorage.getItem(customNameStorageKey);
  if (savedName && savedName.trim()) customName.value = savedName;
} catch {
  // 浏览器禁用存储时使用自动生成名称。
}
const name = computed({
  get: () => customName.value ?? generatedName.value,
  set: (value) => {
    customName.value = value;
    try {
      const trimmed = String(value || "").trim();
      if (trimmed && trimmed !== generatedName.value)
        localStorage.setItem(customNameStorageKey, trimmed);
      else localStorage.removeItem(customNameStorageKey);
    } catch {
      // 存储失败不影响当前输入。
    }
  },
});
const transferDevice = collectTransferDevice().then((device) => {
  generatedName.value = defaultTransferName(nameSuffix, navigator, device);
  return device;
});
const code = ref("");
const route = useRoute();
const router = useRouter();
const confirm = useConfirmation();
const mobile = useMobile();
const transferEntry = ref(null);
const autoApproveStorageKey = "cloudpaste-transfer-auto-approve";
const autoApproveDevice = ref(false);
try {
  autoApproveDevice.value =
    localStorage.getItem(autoApproveStorageKey) === "true";
} catch {
  // 浏览器禁用存储时保持默认关闭，仍允许在当前页面手动切换。
}
watch(autoApproveDevice, (enabled) => {
  try {
    localStorage.setItem(autoApproveStorageKey, String(enabled));
  } catch {
    // 存储失败不影响本次连接的开关。
  }
});
const pastingCode = ref(false);
const pairingCode = ref("");
const pairingDeadline = ref(0);
const relayOnly = ref(false);
const stunMode = ref("site");
// 手机上加入与创建分段显示，默认先显示加入。
const entryTab = ref("join");
const customSTUN = ref("");
// 与服务端默认 STUN 列表一致，只能勾选，不再手动输入地址。
const builtinSTUN = [
  "stun:stun.miwifi.com:3478",
  "stun:stun.hitv.com:3478",
  "stun:stun.cloudflare.com:3478",
  "stun:stun.l.google.com:19302",
  "stun:stun.nextcloud.com:3478",
];
const stunPicks = ref(builtinSTUN.slice(0, 2));
watch(
  stunPicks,
  (servers) => {
    customSTUN.value = servers.join("\n");
  },
  { immediate: true, deep: true },
);
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
const joiningTime = ref(0);
const pairedTime = ref(0);
const names = ref([]);
const peerIPs = ref([]);
const peerDevices = ref([]);
const peerLocations = ref([]);
const peerEndpoint = ref(null);
const lastConnectionInfo = ref(null);
const localEndpoint = ref(null);
const latency = ref(null);
const stunServers = ref([]);
const peerRelayOnly = ref(false);
const messages = ref([]);
const messageList = ref(null);
const detailsOpen = ref(false);
const filesOpen = ref(false);
const attachmentsOpen = ref(false);
const archived = ref(null);
const reviewingHistory = ref(false);
let historyKey = "";
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
const messageClock = new Intl.DateTimeFormat("zh-CN", {
  hour: "2-digit",
  minute: "2-digit",
  second: "2-digit",
  hour12: false,
});
const pasting = ref(false);
const offeredCount = ref(0);
const fileInput = ref(null);
const imageInput = ref(null);
const dragDepth = ref(0);
const direct = ref(false);
const myMember = ref(0);
const maxFile = ref(0);
const busy = ref(false);
const preferredTransport = ref("p2p");
const receiveConcurrency = ref(16);
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
const hasHistory = computed(
  () =>
    status.value === "closed" &&
    (names.value.length === 2 ||
      messages.value.length > 0 ||
      files.value.length > 0 ||
      folders.value.length > 0),
);
const inConversation = computed(
  () => hasPair.value || (hasHistory.value && reviewingHistory.value),
);
const connectionState = computed(() =>
  hasHistory.value ? "disconnected" : connected.value ? "connected" : "waiting",
);
const connectionLabel = computed(() =>
  hasHistory.value ? "已断开" : connected.value ? "已连接" : "重连中",
);
const reconnectDeadline = ref(0);
const now = ref(Date.now());
const pairingSeconds = computed(() => secondsLeft(pairingDeadline.value));
const reconnectSeconds = computed(() => secondsLeft(reconnectDeadline.value));
const connectionLink = computed(() =>
  direct.value
    ? `点对点${latency.value === null ? "" : ` · ${latency.value} ms`}`
    : "中继",
);
const canDirect = computed(() => direct.value && canSaveDirectly());
const peerName = computed(() => names.value[1 - myMember.value] || "对方设备");
const peerDevice = computed(() => peerDevices.value[1 - myMember.value]);
const localDeviceSummary = computed(() => {
  const summary = peerDevices.value[myMember.value]?.summary;
  return summary ? `这台设备 · ${summary}` : "这台设备";
});
const candidateRoute = computed(() =>
  [localEndpoint.value?.type, peerEndpoint.value?.type]
    .filter(Boolean)
    .join(" → "),
);
const peerAddress = computed(() => {
  if (!direct.value && !(hasHistory.value && peerEndpoint.value))
    return peerRelayOnly.value ? "已禁用" : "未建立";
  return endpointAddress(peerEndpoint.value);
});
function endpointAddress(endpoint) {
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
}
function secondsLeft(deadline) {
  return Math.max(0, Math.ceil((deadline - now.value) / 1000));
}
function remainingTime(seconds) {
  return `${String(Math.floor(seconds / 60)).padStart(2, "0")}:${String(seconds % 60).padStart(2, "0")}`;
}
function receiveLimit(value) {
  return Math.min(
    folderMaxConcurrency,
    Math.max(1, Math.round(Number(value) || 1)),
  );
}
function setReceiveConcurrency(value) {
  receiveConcurrency.value = receiveLimit(value);
  for (const folder of folders.value) {
    if (folder.state === "offered" && folder.from !== myMember.value)
      folder.concurrencyDraft = receiveConcurrency.value;
  }
}
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
      label: "传输中",
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
      label: "已完成与已结束的传输",
      ended: true,
      folders: folders.value.filter(finished),
      files: ordinaryFiles.value.filter(finished),
    },
  ].filter((group) => group.files.length || group.folders.length);
});
let client;

function resetPeerDetails() {
  localEndpoint.value = null;
  latency.value = null;
  stunServers.value = [];
  names.value = [];
  peerIPs.value = [];
  peerDevices.value = [];
  peerLocations.value = [];
  peerEndpoint.value = null;
  lastConnectionInfo.value = null;
}

async function loadHistory() {
  const token = session.token;
  historyKey = "";
  archived.value = null;
  try {
    // 分开访客与已登录身份，不在历史里保存配对码、会话凭证或下载票据。
    const owner = token
      ? Array.from(
          new Uint8Array(
            await crypto.subtle.digest(
              "SHA-256",
              new TextEncoder().encode(token),
            ),
          ),
          (byte) => byte.toString(16).padStart(2, "0"),
        ).join("")
      : "guest";
    if (session.token !== token) return;
    historyKey = `cloudpaste-transfer-history:${owner}`;
    if (hasPair.value) {
      persistHistory();
      persistHistoryImages();
      return;
    }
    const snapshot = JSON.parse(localStorage.getItem(historyKey) || "null");
    if (snapshot?.names?.length === 2 && Array.isArray(snapshot.messages))
      archived.value = snapshot;
  } catch {
    // 禁用浏览器存储时，当次连接仍保留在页面中。
  }
}
function persistHistory() {
  if (names.value.length !== 2) return;
  const snapshot = {
    names: [...names.value],
    member: myMember.value,
    messages: messages.value.map(
      ({ id, from, sent_at, text, file, folder }) => ({
        id,
        from,
        sent_at,
        text,
        file: file && {
          id: file.id,
          name: file.name,
          size: file.size,
          from: file.from,
          image_type: file.image_type,
          state: ["active", "offered"].includes(file.state)
            ? "cancelled"
            : file.state,
        },
        folder: folder && {
          id: folder.id,
          name: folder.name,
          size: folder.size,
          count: folder.count,
          state: ["active", "offered"].includes(folder.state)
            ? "cancelled"
            : folder.state,
        },
      }),
    ),
  };
  archived.value = snapshot;
  try {
    if (historyKey) localStorage.setItem(historyKey, JSON.stringify(snapshot));
  } catch {
    // 存满时仅保留当前页面的历史，互传不受影响。
  }
}
function persistHistoryImages() {
  // 图片使用 IndexedDB 保存 Blob，避免 Base64 膨胀并占满 localStorage。
  if (historyKey)
    void writeHistoryImages(
      historyKey,
      files.value
        .filter((file) => file.historyBlob)
        .map((file) => [file.id, file.historyBlob]),
    );
}
async function viewHistory() {
  if (!archived.value) return;
  status.value = "closed";
  reviewingHistory.value = true;
  followMessages = true;
  if (names.value.length === 2) return scrollMessages();
  names.value = archived.value.names;
  myMember.value = archived.value.member;
  for (const message of archived.value.messages) {
    let file;
    if (message.file) {
      files.value.push({ ...message.file });
      file = files.value.at(-1);
    }
    appendMessage({ ...message, file });
  }
  if (!historyKey) return;
  const snapshot = archived.value;
  const images = new Map(await readHistoryImages(historyKey));
  if (archived.value !== snapshot || !inConversation.value) return;
  for (const file of files.value) {
    const image = images.get(file.id);
    if (image instanceof Blob && image.type === file.image_type) {
      file.historyBlob = image;
      file.previewURL = URL.createObjectURL(image);
    }
  }
  scrollMessages();
}
function clearHistory() {
  try {
    if (historyKey) localStorage.removeItem(historyKey);
  } catch {}
  if (historyKey) void writeHistoryImages(historyKey, []);
  archived.value = null;
  clearImages();
  messages.value = [];
  files.value = [];
  folders.value = [];
  names.value = [];
  resetPeerDetails();
  reviewingHistory.value = false;
  status.value = "idle";
}
function returnToEntry(tab = "join") {
  reviewingHistory.value = false;
  entryTab.value = tab;
  code.value = "";
  detailsOpen.value = filesOpen.value = attachmentsOpen.value = false;
  if (tab === "join") nextTick(() => pairCodeInput.value?.focus());
}
async function leaveConversation() {
  if (
    hasPair.value &&
    !(await confirm("断开连接并取消未完成的文件？", {
      title: "断开连接",
      positiveText: "断开",
    }))
  )
    return;
  if (hasPair.value) leave();
  returnToEntry();
}

function releasePreview(file) {
  // 预览被移除或超出保留数量时一起关闭大图，不保留已经释放的图片地址。
  if (previewImage.value?.id === file?.id) previewImage.value = null;
  if (file?.previewURL) URL.revokeObjectURL(file.previewURL);
  if (file) {
    file.previewURL = "";
    file.historyBlob = null;
  }
}
function appendMessage(message) {
  const sent = new Date(message.sent_at);
  if (!Number.isNaN(sent.getTime())) {
    message.sent_at = sent.toISOString();
    message.timeLabel = messageClock.format(sent);
    message.timeTitle = sent.toLocaleString("zh-CN", { hour12: false });
  }
  messages.value.push(message);
  if (messages.value.length > 100) {
    const old = messages.value.shift().file;
    const hadImage = Boolean(old?.historyBlob);
    releasePreview(old);
    if (hadImage) persistHistoryImages();
  }
  if (hasPair.value) persistHistory();
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
      concurrencyDraft:
        message.from !== myMember.value
          ? receiveConcurrency.value
          : message.concurrency,
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
    appendMessage({
      id: message.id,
      from: message.from,
      sent_at: message.sent_at,
      folder: folders.value.at(-1),
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
    resetPeerDetails();
    pairingCode.value = message.code || "";
    // expires_at 是房间整体有效期（约 2 小时）；配对码只在创建后 5 分钟内可用，
    // 与服务端加入校验一致，取两者中较早的时间。
    const roomExpires = Date.parse(message.expires_at) || Infinity;
    const codeExpires = Date.now() + 5 * 60 * 1000;
    pairingDeadline.value = Math.min(roomExpires, codeExpires);
    now.value = Date.now();
    myMember.value = message.member;
    maxFile.value = message.max_file_size;
    status.value = "connecting";
  } else if (["reconnecting", "peer-offline"].includes(message.type)) {
    status.value = message.type;
    reconnectDeadline.value = Date.parse(message.reconnect_until) || 0;
    now.value = Date.now();
    direct.value = false;
    peerEndpoint.value = null;
    folderScan?.abort();
  } else if (message.type === "ready") status.value = "waiting";
  else if (message.type === "joining") {
    joiningTime.value = Date.now();
    joining.value = message.name;
    joiningIP.value = message.ip || "";
    joiningDevice.value = message.device || null;
    joiningLocation.value = message.location || null;
  } else if (message.type === "paired") {
    const recovering = hasPair.value && !connected.value;
    status.value = "paired";
    reconnectDeadline.value = 0;
    if (recovering) notify("success", "连接已恢复，中断的文件请重新发送");
    names.value = message.names;
    peerIPs.value = message.ips || [];
    peerDevices.value = message.devices || [];
    peerLocations.value = message.locations || [];
    peerRelayOnly.value = message.relay_only === true;
    if (!recovering) pairedTime.value = Date.now();
    stunServers.value = message.relay_only
      ? []
      : (client.stunOverride ??
        parseSTUNServers(
          message.stun_servers?.join("\n") || message.stun || "",
        ));
    pairingCode.value = "";
    joining.value = "";
    joiningIP.value = "";
    joiningDevice.value = null;
    joiningLocation.value = null;
    persistHistory();
    if (!recovering) persistHistoryImages();
  } else if (message.type === "direct") {
    direct.value = message.ready;
    // 重建、降级时立即清除旧地址，避免把上一次链路当作当前链路。
    peerEndpoint.value = null;
    localEndpoint.value = null;
    latency.value = null;
  } else if (message.type === "connection-info") {
    if (direct.value) {
      peerEndpoint.value = message.remote;
      localEndpoint.value = message.local || null;
      latency.value = message.roundTripMs ?? null;
      // 只保留当前配对的最后一次链路快照，断开后明确作为历史信息展示。
      if (message.local || message.remote?.address || message.remote?.port) {
        lastConnectionInfo.value = {
          remote: peerEndpoint.value,
          local: localEndpoint.value,
          latency: latency.value,
        };
      }
    }
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
    if (!message.folder)
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
      file.historyBlob = message.blob;
      // 浏览器只保留最近四张图片的本地预览和下载数据，文字记录仍保留。
      const previews = files.value.filter((item) => item.previewURL);
      while (previews.length > chatImageHistory)
        releasePreview(previews.shift());
      persistHistory();
      persistHistoryImages();
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
    // 断开只释放待发送内容，已接收的预览随历史保留；新配对或离开页面时再清理。
    while (pastedImages.value.length) removePasted(0);
    selected.value = [];
    text.value = "";
    status.value = "closed";
    direct.value = false;
    pairingCode.value = "";
    joining.value = "";
    joiningIP.value = "";
    joiningDevice.value = null;
    joiningLocation.value = null;
    peerEndpoint.value = lastConnectionInfo.value?.remote ?? null;
    localEndpoint.value = lastConnectionInfo.value?.local ?? null;
    latency.value = lastConnectionInfo.value?.latency ?? null;
    for (const file of files.value) {
      file.url = "";
      if (["active", "offered"].includes(file.state)) file.state = "cancelled";
    }
    if (hasHistory.value) {
      reviewingHistory.value = true;
      persistHistory();
      scrollMessages();
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
  client?.close();
  clearImages();
  files.value = [];
  folders.value = [];
  selectedFolder.value = null;
  offeredCount.value = 0;
  selected.value = [];
  messages.value = [];
  names.value = [];
  reviewingHistory.value = false;
  joiningTime.value = pairedTime.value = 0;
  resetPeerDetails();
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
      throw new Error("请选择至少一个 STUN 服务器，或选择不使用 STUN");
    const device = await transferDevice;
    await client.open(
      kind,
      {
        name: name.value,
        device,
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
  if (!connected.value) return;
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
function handleMessageKeydown(event) {
  if (
    event.key !== "Enter" ||
    event.isComposing ||
    event.keyCode === 229 ||
    !(event.target instanceof HTMLTextAreaElement) ||
    event.altKey ||
    event.metaKey ||
    event.shiftKey
  )
    return;
  event.preventDefault();
  if (event.ctrlKey) {
    // Ctrl+Enter 显式插入换行，保留光标选区，并通过 input 同步组件值。
    const input = event.target;
    const length =
      input.value.length - (input.selectionEnd - input.selectionStart);
    if (input.maxLength >= 0 && length >= input.maxLength) return;
    input.setRangeText("\n", input.selectionStart, input.selectionEnd, "end");
    input.dispatchEvent(new Event("input", { bubbles: true }));
  } else if (!event.repeat && connected.value && !pasting.value) {
    // 输入法选词和长按回车都不应误发消息。
    sendText();
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
async function chooseAttachment(kind) {
  attachmentsOpen.value = false;
  await nextTick();
  if (!connected.value) return;
  if (kind === "folder") void selectFolder();
  else if (kind === "image") imageInput.value?.click();
  else fileInput.value?.click();
}
async function chooseImages(event) {
  const items = Array.from(event.target.files);
  event.target.value = "";
  if (!connected.value || !items.length || pasting.value) return;
  const current = client;
  pasting.value = true;
  try {
    for (const file of items) {
      if (pastedImages.value.length >= chatImageHistory)
        throw new Error(`每次最多发送 ${chatImageHistory} 张图片`);
      await validateChatImage(file);
      if (current !== client || !connected.value) return;
      pastedImages.value.push({ file, url: URL.createObjectURL(file) });
    }
  } catch (reason) {
    error.value = reason.message;
  } finally {
    pasting.value = false;
  }
}
function dropFiles(event) {
  dragDepth.value = 0;
  if (!connected.value) return;
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
  const hadImage = Boolean(file.historyBlob);
  releasePreview(file);
  messages.value = messages.value.filter(
    (message) => message.file?.id !== file.id,
  );
  files.value = files.value.filter((item) => item.id !== file.id);
  if (names.value.length === 2 && archived.value) persistHistory();
  if (hadImage) persistHistoryImages();
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
  if (!connected.value || file.accepting) return;
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
async function copyJoinLink() {
  const url = new URL(router.resolve("/transfer").href, location.origin);
  // 配对码放在片段中，不随页面请求发送给服务器。
  url.hash = new URLSearchParams({ code: pairingCode.value }).toString();
  try {
    await navigator.clipboard.writeText(url.href);
    notify("success", "加入链接已复制");
  } catch {
    error.value = "复制失败，请手动分享配对码";
  }
}
async function clearCode() {
  if (busy.value || pastingCode.value || !canJoin.value) return;
  code.value = "";
  // 等输入框同步清空后，再将焦点移回第一格，清空不会触发自动加入。
  await nextTick();
  transferEntry.value?.focusCode();
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
      error.value = "剪贴板中不是有效的配对码（4 位数字，例如 0429）";
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
  now.value = Date.now();
  const measuredAt = performance.now();
  for (const file of files.value) measureSpeed(file, file.bytes, measuredAt);
  // 累计已完成文件和在传文件的字节，短文件完成移出列表后仍计入文件夹速率。
  for (const folder of folders.value)
    measureSpeed(folder, folderBytes(folder), measuredAt);
}, 1000);
onMounted(() => {
  const linkedCode = new URLSearchParams(route.hash.slice(1)).get("code");
  if (validPairingCode(linkedCode)) {
    code.value = linkedCode;
    void router.replace({ hash: "" });
  }
  void loadHistory();
});
onBeforeRouteLeave(async () => {
  if (
    hasPair.value &&
    !(await confirm("离开会断开互传并取消未完成的文件，是否继续？", {
      title: "离开设备互传",
      positiveText: "离开",
    }))
  )
    return false;
  leave();
});
watch(
  () => session.token,
  () => {
    if (client && myMember.value === 0) leave();
    if (!hasPair.value) {
      clearImages();
      names.value = [];
      files.value = [];
      folders.value = [];
      messages.value = [];
      reviewingHistory.value = false;
    }
    void loadHistory();
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
  <div
    class="transfer-page"
    :class="{ 'transfer-conversation': inConversation }"
  >
    <TransferEntry
      v-if="!inConversation && ['idle', 'closed'].includes(status)"
      ref="transferEntry"
      v-model:entry-tab="entryTab"
      v-model:name="name"
      v-model:code="code"
      v-model:auto-approve-device="autoApproveDevice"
      v-model:relay-only="relayOnly"
      v-model:stun-mode="stunMode"
      v-model:stun-picks="stunPicks"
      :archived="archived"
      :busy="busy"
      :pasting-code="pastingCode"
      :can-create="canCreate"
      :can-join="canJoin"
      :builtin-s-t-u-n="builtinSTUN"
      @view-history="viewHistory"
      @clear-history="clearHistory"
      @open="open"
      @paste-code="pasteCode"
      @clear-code="clearCode"
    />

    <TransferWaiting
      v-else-if="!inConversation"
      v-model:auto-approve-device="autoApproveDevice"
      :status="status"
      :my-member="myMember"
      :pairing-code="pairingCode"
      :pairing-seconds="pairingSeconds"
      :remaining-time="remainingTime"
      :names="names"
      :joining="joining"
      :joining-i-p="joiningIP"
      :joining-location="joiningLocation"
      :joining-device="joiningDevice"
      :joining-time="joiningTime"
      :message-clock="messageClock"
      :busy="busy"
      @leave="leave"
      @copy-code="copyCode"
      @copy-join-link="copyJoinLink"
      @confirm-join="send({ type: 'confirm' })"
      @reject-join="send({ type: 'reject' })"
    />

    <template v-if="inConversation">
      <TransferPeerBar
        :peer-name="peerName"
        :connection-state="connectionState"
        :connection-label="connectionLabel"
        :connection-link="connectionLink"
        :connected="connected"
        :has-history="hasHistory"
        :mobile="mobile"
        :file-count="ordinaryFiles.length + folders.length"
        :local-name="names[myMember] || name"
        :local-device-summary="localDeviceSummary"
        :peer-device="peerDevice"
        :reconnect-seconds="reconnectSeconds"
        :remaining-time="remainingTime"
        @leave-conversation="leaveConversation"
        @open-files="filesOpen = true"
        @open-details="detailsOpen = true"
        @return-to-entry="returnToEntry"
        @leave="leave"
      />
      <div class="transfer-workspace">
        <section class="panel transfer-chat">
          <div class="transfer-section-heading">
            <h2>消息与文件</h2>
            <span class="muted">文字、图片、文件都在这里发送</span>
          </div>
          <div
            class="transfer-messages"
            :class="{ dragging: connected && dragDepth > 0 }"
            ref="messageList"
            @scroll="trackMessageScroll"
            @dragenter.prevent="connected && dragDepth++"
            @dragleave.prevent="dragDepth = Math.max(0, dragDepth - 1)"
            @dragover.prevent
            @drop.prevent="dropFiles"
            role="log"
            aria-label="对话消息"
            aria-live="polite"
          >
            <div v-if="!messages.length" class="transfer-chat-empty muted">
              <Icon name="message" />
              <p v-if="hasHistory">本次连接没有消息。</p>
              <p v-else>还没有消息<br />发送文字或粘贴图片开始对话</p>
            </div>
            <TransitionGroup name="transfer-message">
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
                      message.file?.image_type &&
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
                <template v-if="message.file?.image_type">
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
                <template v-else-if="message.file || message.folder">
                  <div class="transfer-bubble-file">
                    <span
                      v-if="message.file"
                      class="file-ext"
                      :data-kind="fileDisplay(message.file.name).kind"
                      aria-hidden="true"
                      >{{ fileDisplay(message.file.name).label }}</span
                    >
                    <span v-else class="file-ext"><Icon name="folder" /></span>
                    <div>
                      <strong>{{
                        (message.file || message.folder).name
                      }}</strong>
                      <small
                        >{{ size((message.file || message.folder).size) }} ·
                        {{
                          message.file
                            ? stateLabel(message.file)
                            : folderStateLabels[message.folder.state]
                        }}</small
                      >
                    </div>
                  </div>
                  <progress
                    v-if="message.file?.state === 'active'"
                    :value="message.file.bytes"
                    :max="message.file.size || 1"
                    :aria-label="message.file.name + ' 传输进度'"
                  />
                  <div
                    v-if="
                      connected && message.file && message.from !== myMember
                    "
                    class="transfer-bubble-downloads"
                  >
                    <template v-if="message.file.state === 'offered'">
                      <UiButton
                        v-if="canDirect"
                        type="button"
                        size="small"
                        icon="download"
                        :disabled="active || message.file.accepting"
                        @click="accept(message.file, true)"
                        >点对点下载</UiButton
                      >
                      <UiButton
                        type="button"
                        size="small"
                        icon="download"
                        :disabled="active || message.file.accepting"
                        @click="accept(message.file, false)"
                        >中继下载</UiButton
                      >
                    </template>
                    <UiButton
                      v-else-if="
                        message.file.url && message.file.mode === 'tool'
                      "
                      tag="a"
                      size="small"
                      icon="download"
                      :href="message.file.url"
                      :download="message.file.name"
                      referrerpolicy="no-referrer"
                      >下载文件</UiButton
                    >
                  </div>
                </template>
                <p v-else>{{ message.text }}</p>
              </div>
            </TransitionGroup>
            <p
              v-if="hasHistory && messages.length"
              class="transfer-system-message"
            >
              连接已断开 · 消息已保留
            </p>
          </div>
          <form
            class="transfer-message-composer"
            @submit.prevent="sendText"
            @paste="pasteImage"
            @keydown="handleMessageKeydown"
          >
            <div class="transfer-composer-tools">
              <UiButton
                type="button"
                class="quiet"
                size="small"
                icon="attachment"
                aria-label="发送文件"
                title="发送文件"
                :disabled="!connected"
                @click="chooseAttachment('file')"
              />
              <UiButton
                type="button"
                class="quiet"
                size="small"
                icon="folder"
                aria-label="发送文件夹"
                title="发送文件夹"
                :disabled="
                  !connected || folderSelecting || Boolean(selectedFolder)
                "
                @click="chooseAttachment('folder')"
              />
              <UiButton
                type="button"
                class="quiet"
                size="small"
                icon="image"
                aria-label="发送图片"
                title="发送图片"
                :disabled="!connected"
                @click="chooseAttachment('image')"
              />
            </div>
            <UiButton
              type="button"
              class="mobile-chat-plus quiet icon-button"
              icon="plus"
              aria-label="添加附件"
              :disabled="!connected"
              @click="attachmentsOpen = true"
            />
            <UiInput
              v-model="text"
              type="textarea"
              aria-label="发送文字"
              :placeholder="
                connected
                  ? '输入文字、粘贴图片，或拖入文件；Enter 发送'
                  : hasHistory
                    ? '连接已断开，重新连接后可继续发送'
                    : '恢复连接后可继续发送'
              "
              :rows="1"
              :autosize="{ minRows: 1, maxRows: 4 }"
              maxlength="4096"
              :disabled="!connected"
              title="Enter 发送，Ctrl+Enter 换行；支持粘贴 PNG / JPEG 图片，单张最多 4 MiB"
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
              :disabled="
                !connected || pasting || (!text.trim() && !pastedImages.length)
              "
              ><span class="transfer-send-label">发送</span></UiButton
            >
          </form>
          <div
            v-if="connected && (selected.length || selectedFolder)"
            class="transfer-chat-queue"
          >
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
              <UiButton
                class="primary"
                size="small"
                icon="upload"
                @click="offer"
                >发送所选文件（{{ selected.length }}）</UiButton
              >
            </section>
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
          <input
            ref="imageInput"
            hidden
            type="file"
            accept="image/png,image/jpeg"
            multiple
            aria-label="选择互传图片"
            @change="chooseImages"
          />
          <MobileSheet v-model="attachmentsOpen" title="发送附件" mobile-only>
            <div class="transfer-attachment-options">
              <button type="button" @click="chooseAttachment('file')">
                <Icon name="file" />文件
              </button>
              <button
                type="button"
                :disabled="folderSelecting || Boolean(selectedFolder)"
                @click="chooseAttachment('folder')"
              >
                <Icon name="folder" />文件夹
              </button>
              <button type="button" @click="chooseAttachment('image')">
                <Icon name="image" />图片
              </button>
            </div>
          </MobileSheet>
        </section>
        <aside class="transfer-conversation-sidebar">
          <TransferDetailsPanel
            v-model:open="detailsOpen"
            :has-history="hasHistory"
            :connected="connected"
            :direct="direct"
            :latency="latency"
            :candidate-route="candidateRoute"
            :connection-link="connectionLink"
            :local-endpoint="localEndpoint"
            :peer-endpoint="peerEndpoint"
            :endpoint-address="endpointAddress"
            :peer-i-p="peerIPs[1 - myMember]"
            :peer-location="peerLocations[1 - myMember]"
            :peer-device="peerDevice"
            :peer-address="peerAddress"
            :joining-time="joiningTime"
            :paired-time="pairedTime"
            :message-clock="messageClock"
            :stun-servers="stunServers"
            :mobile="mobile"
            @return-to-entry="returnToEntry"
            @leave="leave"
          />

          <TransferFilePanel
            v-model:open="filesOpen"
            v-model:auto-receive="autoReceive"
            v-model:preferred-transport="preferredTransport"
            :connected="connected"
            :can-direct="canDirect"
            :receive-concurrency="receiveConcurrency"
            :folder-max-concurrency="folderMaxConcurrency"
            :file-groups="fileGroups"
            :my-member="myMember"
            :active="active"
            :folder-saving-supported="folderSavingSupported"
            :direct-button-theme="directButtonTheme"
            :folder-state-labels="folderStateLabels"
            :size="size"
            :folder-bytes="folderBytes"
            :folder-files="folderFiles"
            :file-percent="filePercent"
            :file-status="fileStatus"
            @set-receive-concurrency="setReceiveConcurrency"
            @update-folder-concurrency="updateFolderConcurrency"
            @accept-folder="acceptFolder"
            @cancel-folder="cancelFolder"
            @remove-folder="(id) => (folders = folders.filter((item) => item.id !== id))"
            @remove-task="removeTask"
            @retry="retry"
            @accept="accept"
            @copy-download-link="copyDownloadLink"
            @send="send"
          />
        </aside>
      </div>
    </template>
    <TransferImagePreviewModal
      :image="previewImage"
      @close="previewImage = null"
    />
  </div>
</template>
