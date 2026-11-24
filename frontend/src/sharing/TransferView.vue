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
import { NModal, useNotification } from "naive-ui";
import { onBeforeRouteLeave, useRoute, useRouter } from "vue-router";
import { session, canTransfer } from "./session.js";
import { settings, loadSettings } from "./settings.js";
import Icon from "./Icon.vue";
import PairCodeInput from "./PairCodeInput.vue";
import PageIllustration from "./PageIllustration.vue";
import EmptyIllustration from "./EmptyIllustration.vue";
import IPLocation from "./IPLocation.vue";
import MobileSheet from "./ui/MobileSheet.vue";
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
  displayPairingCode,
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
  .padStart(5, "0")
  .slice(-5);
const generatedName = ref(defaultTransferName(nameSuffix));
const customName = ref(null);
const name = computed({
  get: () => customName.value ?? generatedName.value,
  set: (value) => {
    customName.value = value;
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
const pairCodeInput = ref(null);
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
  hasHistory.value ? "已断开" : connected.value ? "已连接" : "恢复中",
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
    <header
      v-if="!inConversation && ['idle', 'closed'].includes(status)"
      class="page-heading hero"
    >
      <div>
        <h1>设备互传</h1>
        <p class="muted">配对后互发文字、图片、文件和文件夹，双方需保持页面在线</p>
      </div>
      <PageIllustration section="transfer" />
    </header>
    <section
      v-if="!inConversation && ['idle', 'closed'].includes(status)"
      class="transfer-entry"
    >
      <div
        v-if="archived && archived.messages.length"
        class="panel transfer-history-notice"
      >
        <span
          >上次与 {{ archived.names[1 - archived.member] }} 的聊天记录已保留 ·
          {{ archived.messages.length }} 条消息</span
        >
        <UiButton size="small" type="button" @click="viewHistory"
          >查看记录</UiButton
        >
        <UiButton
          size="small"
          type="button"
          class="quiet"
          @click="clearHistory"
          >清空</UiButton
        >
      </div>
      <div
        class="page-segment transfer-entry-tabs"
        role="group"
        aria-label="连接方式"
      >
        <button
          type="button"
          :aria-pressed="entryTab === 'join'"
          @click="entryTab = 'join'"
        >
          加入连接
        </button>
        <button
          type="button"
          :aria-pressed="entryTab === 'create'"
          @click="entryTab = 'create'"
        >
          创建连接
        </button>
      </div>
      <div class="transfer-entry-grid" :data-tab="entryTab">
        <section class="panel transfer-entry-card">
          <div class="transfer-entry-heading">
            <span><Icon name="plus" /></span>
            <div>
              <h2>创建连接</h2>
              <p class="muted">生成 5 分钟有效的配对码</p>
            </div>
          </div>
          <label
            >设备名称<UiInput
              v-model="name"
              aria-label="设备名称"
              maxlength="60"
              autocomplete="off"
              :disabled="busy"
          /></label>
          <div v-if="canCreate" class="transfer-options">
            <label class="switch-row transfer-switch-described"
              ><span
                >自动同意设备加入<small id="create-approve-help" class="muted"
                  >持有配对码的设备直接加入，选择会保存在本浏览器</small
                ></span
              ><input
                v-model="autoApproveDevice"
                class="page-switch"
                role="switch"
                type="checkbox"
                aria-label="自动同意设备加入"
                aria-describedby="create-approve-help"
                :disabled="busy"
            /></label>
          </div>
          <label class="switch-row transfer-switch-described"
            ><span
              >仅使用中继<small id="create-relay-help" class="muted"
                >不向对方交换直连网络地址</small
              ></span
            ><input
              v-model="relayOnly"
              class="page-switch"
              role="switch"
              type="checkbox"
              aria-label="仅使用中继"
              aria-describedby="create-relay-help"
              :disabled="busy"
          /></label>
          <details class="transfer-connection-settings">
            <summary>
              <span>高级连接设置</span><Icon name="chevron-down" />
            </summary>
            <div class="transfer-connection-fields">
              <label
                >STUN 模式
                <UiSelect
                  v-model="stunMode"
                  aria-label="STUN 模式"
                  :disabled="busy || relayOnly"
                >
                  <option value="site">使用站点配置（推荐）</option>
                  <option value="custom">选择服务器</option>
                  <option value="disabled">不使用 STUN</option>
                </UiSelect>
              </label>
              <fieldset
                v-if="stunMode === 'custom'"
                class="transfer-stun-picks"
                :disabled="busy || relayOnly"
                aria-describedby="transfer-stun-help"
              >
                <legend>选择 STUN 服务器</legend>
                <label
                  v-for="server in builtinSTUN"
                  :key="server"
                  class="check transfer-stun-pick"
                  :class="{ selected: stunPicks.includes(server) }"
                  ><UiCheckbox v-model="stunPicks" :value="server" /><code>{{
                    server.replace(/^stun:/, "")
                  }}</code></label
                >
              </fieldset>
              <p id="transfer-stun-help" class="muted small">
                {{
                  relayOnly
                    ? "仅使用中继时不查询 STUN 服务器。"
                    : stunMode === "custom"
                      ? "只在本浏览器本次生效，不改变站点配置"
                      : stunMode === "disabled"
                        ? "不查询 STUN 服务器，仍可尝试直连；无法直连时可使用中继。"
                        : "使用站点提供的服务器，由浏览器收集候选地址并验证可用链路。"
                }}
              </p>
            </div>
          </details>
          <UiButton
            v-if="canCreate"
            type="button"
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
        </section>
        <form class="panel transfer-entry-card" @submit.prevent="open('join')">
          <div class="transfer-entry-heading">
            <span><Icon name="login" /></span>
            <div>
              <h2>加入连接</h2>
              <p class="muted">输入对方屏幕上的 4 位配对码</p>
            </div>
          </div>
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
                  size="small"
                  icon="clipboard"
                  aria-label="粘贴配对码"
                  :disabled="busy || !canJoin || pastingCode"
                  @click="pasteCode"
                  >{{ pastingCode ? "读取中" : "一键粘贴" }}</UiButton
                >
                <UiButton
                  type="button"
                  class="quiet"
                  size="small"
                  aria-label="清空配对码"
                  :disabled="busy || !canJoin || pastingCode || !code"
                  @click="clearCode"
                  >清空</UiButton
                >
              </div>
            </div>
          </div>
          <label
            >设备名称<UiInput
              v-model="name"
              aria-label="加入时的设备名称"
              maxlength="60"
              autocomplete="off"
              :disabled="busy"
          /></label>
          <label class="switch-row transfer-join-relay"
            ><span
              >仅使用中继<small id="join-relay-help" class="muted"
                >不向对方交换直连网络地址</small
              ></span
            ><input
              v-model="relayOnly"
              class="page-switch"
              role="switch"
              type="checkbox"
              aria-label="仅使用中继"
              aria-describedby="join-relay-help"
              :disabled="busy"
          /></label>
          <p class="muted small transfer-join-safety">
            <Icon
              name="shield"
            />中继经本站转发；直连会向对方提供网络地址。只与信任的人配对。
          </p>
          <UiButton
            class="primary"
            :disabled="busy || !canJoin || !validPairingCode(code)"
            >加入</UiButton
          >
        </form>
      </div>
    </section>

    <template v-else-if="!inConversation">
      <header class="transfer-wait-heading">
        <div>
          <h1>
            {{
              status === "connecting"
                ? "正在连接"
                : myMember === 0
                  ? "等待对方加入"
                  : "等待对方确认"
            }}
          </h1>
        </div>
        <UiButton type="button" @click="leave"
          >取消连接</UiButton
        >
      </header>
      <div class="transfer-wait-layout" :class="{ 'has-code': pairingCode }">
        <section class="panel transfer-waiting">
          <div class="transfer-radar" aria-hidden="true">
            <i></i><i></i><i></i><span><Icon name="transfer" /></span>
          </div>
          <template v-if="pairingCode">
            <p class="transfer-code" aria-label="配对码">
              <span
                v-for="(digit, index) in displayPairingCode(pairingCode)"
                :key="index"
                >{{ digit }}</span
              >
            </p>
            <div class="transfer-wait-actions">
              <UiButton type="button" size="small" @click="copyCode"
                >复制配对码</UiButton
              >
              <UiButton
                type="button"
                class="quiet"
                size="small"
                @click="copyJoinLink"
                >复制加入链接</UiButton
              >
            </div>
            <div class="transfer-countdown">
              <svg viewBox="0 0 24 24" aria-hidden="true">
                <circle cx="12" cy="12" r="9" pathLength="100" />
                <circle
                  cx="12"
                  cy="12"
                  r="9"
                  pathLength="100"
                  :stroke-dasharray="Math.min(100, pairingSeconds / 3) + ' 100'"
                />
              </svg>
              <span
                ><b>{{ remainingTime(pairingSeconds) }}</b>
                后失效，配对成功后作废</span
              >
            </div>
          </template>
          <template v-else>
            <p class="transfer-wait-title">已发送加入请求</p>
            <p class="muted">
              请对方在设备上确认连接。确认前请保持此页面在线，拒绝或超时后可重新输入配对码。
            </p>
            <dl v-if="names[1 - myMember]" class="transfer-wait-peer">
              <div>
                <dt>连接对象</dt>
                <dd>{{ names[1 - myMember] }}</dd>
              </div>
            </dl>
          </template>
        </section>
        <section v-if="pairingCode" class="panel transfer-requests">
          <div class="transfer-section-heading">
            <h2>加入请求</h2>
            <span class="page-tag" :class="joining ? 'accent' : 'warning'">{{
              joining ? "1 个请求" : "等待中"
            }}</span>
          </div>
          <div v-if="joining" class="transfer-joining" role="status">
            <div class="transfer-peer-info">
              <strong>{{ joining }}</strong
              ><span class="page-tag accent">待授权</span>
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
              <div v-if="joiningTime">
                <dt>申请时间</dt>
                <dd>{{ messageClock.format(joiningTime) }}</dd>
              </div>
            </dl>
            <p class="muted small">请求加入，请确认这是你要连接的设备。</p>
            <div class="transfer-confirm-actions">
              <UiButton
                type="button"
                class="primary"
                @click="send({ type: 'confirm' })"
                >确认连接</UiButton
              >
              <UiButton
                type="button"
                class="danger"
                @click="send({ type: 'reject' })"
                >拒绝并关闭</UiButton
              >
            </div>
          </div>
          <p v-else class="muted">对方输入配对码后，在这里确认设备信息。</p>
          <div
            v-if="myMember === 0 && status === 'waiting'"
            class="transfer-options"
          >
            <label class="switch-row"
              ><span>自动同意设备加入</span
              ><input
                v-model="autoApproveDevice"
                class="page-switch"
                role="switch"
                type="checkbox"
                :disabled="busy"
            /></label>
          </div>
        </section>
      </div>
    </template>

    <template v-if="inConversation">
      <header class="mobile-chat-heading">
        <UiButton
          type="button"
          class="quiet icon-button"
          icon="back"
          aria-label="返回"
          @click="leaveConversation"
        />
        <div class="mobile-chat-who">
          <strong>{{ peerName }}</strong>
          <span class="transfer-peer-status" :data-state="connectionState"
            ><i aria-hidden="true"></i>{{ connectionLabel
            }}<template v-if="connected">
              · {{ connectionLink }}</template
            ></span
          >
        </div>
        <div class="mobile-chat-file-action">
          <UiButton
            type="button"
            class="quiet icon-button"
            icon="file"
            aria-label="文件列表"
            @click="filesOpen = true"
          />
          <b v-if="ordinaryFiles.length + folders.length">{{
            ordinaryFiles.length + folders.length
          }}</b>
        </div>
        <UiButton
          type="button"
          class="quiet icon-button"
          icon="info"
          aria-label="连接详情"
          @click="detailsOpen = true"
        />
      </header>
      <div
        class="panel transfer-peer"
        :class="{
          'transfer-peer-disconnected': hasHistory,
          'transfer-peer-waiting': !connected && !hasHistory,
        }"
      >
        <div class="transfer-peer-bar">
          <div class="transfer-peer-info">
            <span class="transfer-peer-status" :data-state="connectionState"
              ><i aria-hidden="true"></i>{{ connectionLabel }}</span
            >
            <span class="transfer-peer-sep" aria-hidden="true"></span>
            <div class="transfer-peer-devices">
              <div class="transfer-peer-device">
                <Icon name="monitor" />
                <div>
                  <strong :title="names[myMember] || name">{{
                    names[myMember] || name
                  }}</strong
                  ><small class="muted" :title="localDeviceSummary">{{
                    localDeviceSummary
                  }}</small>
                </div>
              </div>
              <span
                class="transfer-peer-wire"
                :data-state="connectionState"
                aria-hidden="true"
                ><span class="transfer-peer-wire-line"></span
                ><span class="transfer-peer-wire-line"></span
              ></span>
              <div class="transfer-peer-device">
                <Icon
                  :name="peerDevice?.type === 'mobile' ? 'mobile' : 'monitor'"
                />
                <div>
                  <strong :title="peerName">{{ peerName }}</strong
                  ><small class="muted" :title="peerDevice?.summary">{{
                    peerDevice?.summary
                  }}</small>
                </div>
              </div>
            </div>
          </div>
          <span v-if="connected" class="transfer-peer-link">{{
            connectionLink
          }}</span>
          <div v-if="!mobile" class="transfer-peer-actions">
            <template v-if="hasHistory">
              <UiButton
                type="button"
                class="primary"
                size="small"
                icon="retry"
                @click="returnToEntry()"
                >重新连接</UiButton
              >
              <UiButton
                type="button"
                size="small"
                @click="returnToEntry('create')"
                >新建配对</UiButton
              >
            </template>
            <UiButton
              v-else
              type="button"
              class="danger"
              size="small"
              @click="leave"
              >断开</UiButton
            >
          </div>
        </div>
      </div>
      <div
        v-if="!connected"
        class="transfer-connection-banner"
        :class="{ disconnected: hasHistory }"
        role="status"
      >
        <Icon :name="hasHistory ? 'unlink' : 'clock'" />
        <div>
          <strong>{{
            hasHistory ? "已断开连接" : "连接中断，正在自动恢复"
          }}</strong
          ><span>{{
            hasHistory
              ? "聊天记录已保留，刷新页面后仍可查看"
              : "恢复期间无法发送，未完成的文件需重发"
          }}</span>
        </div>
        <time v-if="!hasHistory">{{ remainingTime(reconnectSeconds) }}</time>
      </div>
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
                        :disabled="active || message.file.accepting"
                        @click="accept(message.file, true)"
                        >点对点下载</UiButton
                      >
                      <UiButton
                        type="button"
                        size="small"
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
          <MobileSheet v-model="detailsOpen" title="连接详情">
            <section
              class="panel transfer-details-panel"
              aria-labelledby="transfer-details-title"
            >
              <div class="transfer-section-heading">
                <h2 id="transfer-details-title">连接详情</h2>
                <span class="muted">{{
                  hasHistory
                    ? "已断开"
                    : !connected
                      ? "等待恢复"
                      : direct
                        ? "点对点 · 中继可用"
                        : "中继"
                }}</span>
              </div>
              <dl id="transfer-peer-details" class="transfer-peer-details">
                <div>
                  <dt>延迟</dt>
                  <dd>{{ latency === null ? "—" : `${latency} ms` }}</dd>
                </div>
                <div>
                  <dt>链路</dt>
                  <dd>
                    {{
                      candidateRoute ||
                      (hasHistory && peerEndpoint ? "点对点" : connectionLink)
                    }}
                  </dd>
                </div>
                <div>
                  <dt>本机地址</dt>
                  <dd>
                    {{ localEndpoint ? endpointAddress(localEndpoint) : "—" }}
                  </dd>
                </div>
                <div>
                  <dt>来源 IP</dt>
                  <dd>{{ peerIPs[1 - myMember] || "未提供" }}</dd>
                </div>
                <div v-if="peerLocations[1 - myMember]">
                  <dt>IP 归属地</dt>
                  <dd>
                    <IPLocation :location="peerLocations[1 - myMember]" />
                  </dd>
                </div>
                <div>
                  <dt>设备信息</dt>
                  <dd>{{ peerDevice?.summary || "未提供" }}</dd>
                </div>
                <div>
                  <dt>{{ hasHistory ? "最后点对点地址" : "点对点地址" }}</dt>
                  <dd>{{ peerAddress }}</dd>
                </div>
                <div v-if="joiningTime || pairedTime">
                  <dt>申请 / 同意</dt>
                  <dd>
                    {{ joiningTime ? messageClock.format(joiningTime) : "—" }} /
                    {{ pairedTime ? messageClock.format(pairedTime) : "—" }}
                  </dd>
                </div>
                <div>
                  <dt>STUN</dt>
                  <dd>
                    <span
                      v-if="stunServers.length"
                      class="transfer-stun-summary"
                      :title="
                        stunServers
                          .map((server) => server.replace(/^stun:/, ''))
                          .join('\n')
                      "
                      >{{ stunServers[0].replace(/^stun:/, "")
                      }}<template v-if="stunServers.length > 1">
                        等 {{ stunServers.length }} 个</template
                      ></span
                    >
                    <template v-else>未使用</template>
                  </dd>
                </div>
              </dl>
              <div
                v-if="mobile"
                class="transfer-peer-actions transfer-details-actions"
              >
                <template v-if="hasHistory">
                  <UiButton
                    type="button"
                    class="primary"
                    icon="retry"
                    @click="returnToEntry()"
                    >重新连接</UiButton
                  >
                  <UiButton type="button" @click="returnToEntry('create')"
                    >新建配对</UiButton
                  >
                </template>
                <UiButton
                  v-else
                  type="button"
                  class="danger"
                  @click="
                    detailsOpen = false;
                    leave();
                  "
                  >断开</UiButton
                >
              </div>
            </section>
          </MobileSheet>

          <MobileSheet v-model="filesOpen" title="文件列表">
            <section class="panel transfer-file-panel">
              <div class="transfer-section-heading">
                <h2>文件列表</h2>
                <label
                  v-if="connected"
                  class="check transfer-auto-receive"
                  title="仅本次连接有效，按顺序中继下载"
                  ><UiCheckbox
                    v-model="autoReceive"
                    aria-label="自动接收对方文件"
                  />自动接收</label
                >
              </div>
              <div v-if="connected" class="transfer-receive-settings">
                <span class="transfer-receive-pair"><span>接收</span>
                <div
                  class="page-segment"
                  role="group"
                  aria-label="优先下载方式"
                >
                  <button
                    type="button"
                    :aria-pressed="preferredTransport === 'p2p' && canDirect"
                    :disabled="!canDirect"
                    @click="preferredTransport = 'p2p'"
                  >
                    点对点
                  </button>
                  <button
                    type="button"
                    :aria-pressed="preferredTransport === 'relay' || !canDirect"
                    @click="preferredTransport = 'relay'"
                  >
                    中继
                  </button>
                </div></span>
                <span class="transfer-receive-pair"><span>并发</span>
                <div
                  class="transfer-stepper"
                  title="设置接收文件夹的默认并发数（1–128）"
                >
                  <button
                    type="button"
                    aria-label="减少默认并发数"
                    :disabled="receiveConcurrency <= 1"
                    @click="setReceiveConcurrency(receiveConcurrency - 1)"
                  >
                    −
                  </button>
                  <input
                    type="number"
                    inputmode="numeric"
                    min="1"
                    :max="folderMaxConcurrency"
                    :value="receiveConcurrency"
                    aria-label="接收文件夹的默认并发数"
                    @change="
                      setReceiveConcurrency($event.target.value);
                      $event.target.value = receiveConcurrency;
                    "
                  />
                  <button
                    type="button"
                    aria-label="增加默认并发数"
                    :disabled="receiveConcurrency >= folderMaxConcurrency"
                    @click="setReceiveConcurrency(receiveConcurrency + 1)"
                  >
                    +
                  </button>
                </div></span>
              </div>
              <div v-if="!fileGroups.some((group) => group.key === 'current')">
                <h3 class="transfer-list-heading">传输中</h3>
                <div class="muted transfer-file-empty">
                  <EmptyIllustration kind="transfer" />
                  <strong>暂无传输任务</strong>
                  <span>发送或接收的文件会显示在这里</span>
                </div>
              </div>
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
                  已结束的传输（{{
                    group.files.length + group.folders.length
                  }}）
                </summary>
                <h3 v-else class="transfer-list-heading">
                  传输中
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
                            folder.from === myMember
                              ? "↑ 我发送的"
                              : "↓ 对方发送的"
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
                          {{ size(folder.size) }} ·
                          {{ size(folder.speed) }}/s</span
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
                        <span class="transfer-stepper">
                          <button
                            type="button"
                            aria-label="减少并发数"
                            :disabled="
                              folder.updatingConcurrency ||
                              folder.concurrencyDraft <= 1
                            "
                            @click="
                              folder.concurrencyDraft = Math.max(
                                1,
                                Number(folder.concurrencyDraft || 1) - 1,
                              )
                            "
                          >
                            −
                          </button>
                          <input
                            v-model.number="folder.concurrencyDraft"
                            :disabled="folder.updatingConcurrency"
                            type="number"
                            inputmode="numeric"
                            :min="1"
                            :max="folderMaxConcurrency"
                            :step="1"
                            aria-label="文件夹并发数"
                            :title="`范围 1–${folderMaxConcurrency} 个`"
                            @change="
                              folder.concurrencyDraft = Math.min(
                                folderMaxConcurrency,
                                Math.max(
                                  1,
                                  Math.round(
                                    Number(folder.concurrencyDraft) || 1,
                                  ),
                                ),
                              )
                            "
                          />
                          <button
                            type="button"
                            aria-label="增加并发数"
                            :disabled="
                              folder.updatingConcurrency ||
                              folder.concurrencyDraft >= folderMaxConcurrency
                            "
                            @click="
                              folder.concurrencyDraft = Math.min(
                                folderMaxConcurrency,
                                Number(folder.concurrencyDraft || 1) + 1,
                              )
                            "
                          >
                            +
                          </button>
                        </span>
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
                            Number(folder.concurrencyDraft) ===
                              folder.concurrency
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
                          folders = folders.filter(
                            (item) => item.id !== folder.id,
                          )
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
                        <li
                          v-for="(entry, index) in folder.recent"
                          :key="index"
                        >
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
                      <span
                        class="file-ext"
                        :data-kind="fileDisplay(file.name).kind"
                        aria-hidden="true"
                        >{{ fileDisplay(file.name).label }}</span
                      >
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
                            {{
                              file.mode === "p2p" ? "点对点" : "实时中继"
                            }}</span
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
                      <span
                        >{{ size(file.bytes) }} / {{ size(file.size) }}</span
                      >
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
                        v-if="
                          connected &&
                          file.state === 'offered' &&
                          file.from !== myMember
                        "
                      >
                        <UiButton
                          v-if="canDirect"
                          class="transfer-action transfer-download-choice"
                          :class="{ primary: preferredTransport === 'p2p' }"
                          size="small"
                          :disabled="active || file.accepting"
                          @click="accept(file, true)"
                          >点对点下载</UiButton
                        >
                        <UiButton
                          class="transfer-action transfer-download-choice"
                          :class="{
                            primary:
                              preferredTransport === 'relay' || !canDirect,
                          }"
                          size="small"
                          :disabled="active || file.accepting"
                          @click="accept(file, false)"
                          >中继下载</UiButton
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
                          @click="
                            send({ type: 'finish-download', id: file.id })
                          "
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
          </MobileSheet>
        </aside>
      </div>
    </template>
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
  --transfer-waiting: #9c5300;
}
/* 标签的文字和背景一起切换，暗色界面仍保留足够对比度。 */
:global(html[data-theme="dark"] .transfer-page) {
  --transfer-blue: #acccff;
  --transfer-blue-bg: #223349;
  --transfer-teal: #91ded4;
  --transfer-teal-bg: #203b38;
  --transfer-neutral: #b4c4ba;
  --transfer-neutral-bg: #29322e;
  --transfer-waiting: #f1b56a;
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
  border: 1px solid transparent;
  border-radius: 14px;
  background: var(--surface-subtle);
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
/* 确认与拒绝共用独立布局，不受通用 actions 仅拉伸主按钮的规则影响。 */
.transfer-confirm-actions {
  display: flex;
  flex-wrap: wrap;
  justify-content: space-between;
  align-items: center;
  gap: 0.8rem;
  margin-top: 1.2rem;
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
.transfer-peer-devices {
  display: contents;
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
  display: flex;
  align-items: center;
  gap: 8px;
  width: 90px;
  height: 24px;
  flex-shrink: 0;
  color: var(--accent);
}
.transfer-peer-wire-line {
  flex: 1;
  height: 2px;
  background: repeating-linear-gradient(
    90deg,
    currentColor 0 6px,
    transparent 6px 11px
  );
}
.transfer-peer-wire-dot {
  width: 8px;
  height: 8px;
  border-radius: 50%;
  background: currentColor;
}
.transfer-peer-wire > .icon {
  width: 18px;
  height: 18px;
  flex-shrink: 0;
}
.transfer-peer-waiting .transfer-peer-wire,
.transfer-peer-waiting .transfer-peer-state {
  color: var(--transfer-waiting);
}
.transfer-peer-state {
  color: var(--accent);
  font-size: 12px;
}
.transfer-peer-disconnected .transfer-peer-device:last-child > .icon {
  color: var(--error);
  background: var(--error-bg);
  border-color: var(--error-bg);
}
.transfer-peer-disconnected .transfer-peer-state {
  color: var(--error);
}
.transfer-peer-disconnected .transfer-peer-wire {
  gap: 10px;
  color: var(--error);
}
.transfer-peer-disconnected .transfer-peer-wire-line {
  background: var(--muted);
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
  background: var(--surface);
  background-image: radial-gradient(
    circle at 1px 1px,
    var(--line) 1px,
    transparent 0
  );
  background-size: 18px 18px;
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
  max-width: 86%;
  border: 1px solid transparent;
  border-radius: 3px 14px 14px 14px;
  padding: 10px 14px;
  background: var(--surface-subtle);
  align-self: flex-start;
}
.transfer-message.mine {
  align-self: flex-end;
  border-radius: 14px 3px 14px 14px;
  border-color: transparent;
  background: var(--soft);
  color: var(--accent);
}
.transfer-message.mine .transfer-message-meta,
.transfer-message.mine .muted {
  color: inherit;
  opacity: 1;
}
.transfer-message.mine .transfer-message-remove {
  color: inherit;
}
.transfer-message small {
  opacity: 1;
}
/* 只对新增消息做短暂过渡；初始历史不重放动画，也不参与传输调度。 */
.transfer-message-enter-active {
  transition:
    opacity var(--motion-standard) var(--motion-ease),
    transform var(--motion-standard) var(--motion-ease);
}
.transfer-message-enter-from {
  opacity: 0;
  transform: translateY(8px);
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
  min-width: 56px;
  height: 36px;
  padding-inline: 10px;
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

/* ===== 界面改版（对应 design/cloudpaste-ui-v3.html） ===== */
/* 连接栏：状态 | 本机 — 对方 | 链路 | 操作；不使用左侧色条和渐变底 */
.transfer-peer {
  --peer-c: var(--success);
  --peer-bg: var(--success-bg);
  border-left-width: 1px !important;
  background: var(--surface) !important;
  background-image: none !important;
}
.transfer-peer.transfer-peer-waiting {
  --peer-c: var(--warning, #d97706);
  --peer-bg: var(--warning-bg, #fef3c7);
}
.transfer-peer.transfer-peer-disconnected {
  --peer-c: var(--error);
  --peer-bg: var(--error-bg);
}
.transfer-peer-status {
  display: inline-flex;
  align-items: center;
  gap: 9px;
  color: var(--peer-c);
  font-weight: 650;
  font-size: 14px;
  white-space: nowrap;
}
.transfer-peer-status i {
  width: 10px;
  height: 10px;
  border-radius: 50%;
  background: var(--peer-c);
  box-shadow: 0 0 0 4px var(--peer-bg);
}
.transfer-peer-status[data-state="disconnected"] i {
  border-radius: 2px;
}
.transfer-peer-sep {
  width: 1px;
  height: 28px;
  background: var(--line);
}
.transfer-peer-link {
  flex: none;
  padding: 3px 10px;
  border-radius: 8px;
  background: var(--surface-subtle);
  color: var(--muted);
  font:
    500 12px ui-monospace,
    monospace;
  white-space: nowrap;
}
.transfer-peer-disconnected .transfer-peer-device:last-of-type {
  filter: grayscale(1);
  opacity: 0.55;
}
@media (prefers-reduced-motion: no-preference) {
  .transfer-peer-status[data-state="connected"] i {
    animation: peer-breathe 2.2s ease-in-out infinite;
  }
  .transfer-peer-status[data-state="waiting"] i {
    animation: peer-blink 1s ease-in-out infinite;
  }
}
@keyframes peer-breathe {
  50% {
    box-shadow: 0 0 0 7px color-mix(in srgb, var(--peer-c) 12%, transparent);
  }
}
@keyframes peer-blink {
  50% {
    opacity: 0.35;
  }
}
/* 工作区：左侧消息，右侧连接详情与文件列表 */
/* 只在宽屏生效，不能覆盖上方 900px 以下的单列规则。 */
@media (min-width: 901px) {
  .transfer-workspace {
    grid-template-columns: minmax(0, 1fr) 380px;
    align-items: start;
  }
  .transfer-workspace .transfer-chat {
    grid-row: span 2;
  }
}
.transfer-details-panel .transfer-peer-details {
  display: grid;
  grid-template-columns: auto minmax(0, 1fr);
  gap: 8px 14px;
  margin: 10px 0 0;
  padding-top: 0;
  border-top: 0;
}
.transfer-details-panel .transfer-peer-details > div {
  display: contents;
}
/* 输入栏左侧发送文件、文件夹 */
.transfer-chat form {
  grid-template-columns: auto minmax(0, 1fr) auto;
}
.transfer-composer-tools {
  grid-column: 1;
  grid-row: 1;
  display: flex;
  gap: 2px;
  align-self: center;
}
.transfer-send {
  grid-column: 3;
}
/* 已完成文件的下载按钮并排等宽 */
.transfer-file-actions {
  display: grid;
  grid-auto-flow: column;
  grid-auto-columns: minmax(0, 1fr);
  gap: 6px;
}
.transfer-file-actions :deep(.transfer-action) {
  width: 100%;
  min-width: 0;
}
/* 并发数步进器 */
.transfer-stepper {
  display: inline-flex;
  align-items: stretch;
  height: 30px;
  border: 1px solid var(--line);
  border-radius: 8px;
  overflow: hidden;
  background: var(--surface);
}
.transfer-stepper button {
  width: 30px;
  border: 0;
  background: none;
  color: var(--muted);
  font-size: 16px;
  cursor: pointer;
}
.transfer-stepper button:hover:not(:disabled) {
  background: var(--surface-subtle);
  color: var(--text);
}
.transfer-stepper button:disabled {
  opacity: 0.4;
  cursor: default;
}
.transfer-stepper input {
  width: 48px;
  border: 0;
  border-inline: 1px solid var(--line);
  border-radius: 0;
  background: none;
  text-align: center;
  font:
    600 13px ui-monospace,
    monospace;
  appearance: textfield;
  -moz-appearance: textfield;
}
.transfer-stepper input::-webkit-inner-spin-button,
.transfer-stepper input::-webkit-outer-spin-button {
  -webkit-appearance: none;
  margin: 0;
}
.transfer-stepper:focus-within {
  border-color: var(--accent);
}
/* STUN 服务器勾选 */
.transfer-stun-picks {
  display: grid;
  gap: 6px;
  margin: 0;
  padding: 10px 12px;
  border: 1px solid var(--line);
  border-radius: 10px;
}
.transfer-stun-picks legend {
  padding: 0 6px;
  color: var(--muted);
  font-size: 12px;
}
.transfer-stun-pick {
  padding: 6px 10px;
  border: 1px solid var(--line);
  border-radius: 8px;
  margin: 0 !important;
}
.transfer-stun-pick.selected {
  border-color: var(--accent);
  background: var(--soft);
}
.transfer-stun-pick code {
  font-size: 12.5px;
}
/* 等待页：雷达与渐变配对码 */
.transfer-radar {
  position: relative;
  display: grid;
  place-items: center;
  width: 140px;
  height: 140px;
  margin: 8px auto 4px;
}
.transfer-radar i {
  position: absolute;
  inset: 0;
  border: 2px solid var(--accent);
  border-radius: 50%;
  opacity: 0;
}
.transfer-radar::after {
  content: "";
  position: absolute;
  inset: 0;
  border-radius: 50%;
  background: conic-gradient(
    from 0deg,
    color-mix(in srgb, var(--accent) 26%, transparent),
    transparent 30%
  );
  -webkit-mask: radial-gradient(circle, transparent 30%, #000 31%);
  mask: radial-gradient(circle, transparent 30%, #000 31%);
}
.transfer-radar span {
  position: relative;
  z-index: 1;
  display: grid;
  place-items: center;
  width: 56px;
  height: 56px;
  border-radius: 16px;
  background: var(--accent-gradient, var(--accent));
  color: #fff;
}
.transfer-waiting .transfer-code {
  margin: 6px 0 12px;
  font-size: clamp(40px, 9vw, 56px);
  font-weight: 700;
  background: var(--accent-gradient, var(--accent));
  -webkit-background-clip: text;
  background-clip: text;
  color: transparent;
}
@media (prefers-reduced-motion: no-preference) {
  .transfer-radar i {
    animation: radar-wave 2.6s cubic-bezier(0.2, 0.8, 0.2, 1) infinite;
  }
  .transfer-radar i:nth-child(2) {
    animation-delay: 0.85s;
  }
  .transfer-radar i:nth-child(3) {
    animation-delay: 1.7s;
  }
  .transfer-radar::after {
    animation: radar-sweep 2.4s linear infinite;
  }
}
@keyframes radar-wave {
  0% {
    transform: scale(0.45);
    opacity: 0.5;
  }
  100% {
    transform: scale(1);
    opacity: 0;
  }
}
@keyframes radar-sweep {
  to {
    transform: rotate(360deg);
  }
}
@media (max-width: 900px) {
  .transfer-workspace .transfer-chat {
    grid-row: auto;
  }
}
/* 手机：已连接时聊天优先，连接栏紧凑 */
@media (max-width: 650px) {
  .transfer-peer-sep,
  .transfer-peer-link {
    display: none;
  }
  .transfer-peer-info {
    gap: 10px;
  }
  .transfer-workspace .transfer-chat {
    height: calc(100dvh - 220px);
    min-height: 420px;
  }
  .transfer-chat form {
    position: sticky;
    bottom: 0;
    background: var(--surface);
  }
  .transfer-file-actions {
    grid-auto-flow: row;
    grid-template-columns: 1fr 1fr;
  }
}

/* 入口：手机上加入 / 创建分段切换 */
.transfer-entry-tabs {
  display: none;
}
@media (max-width: 650px) {
  .transfer-entry-tabs {
    display: flex;
    gap: 2px;
    margin-top: 16px;
    padding: 3px;
    border-radius: 10px;
    background: var(--surface-subtle);
  }
  .transfer-entry-tabs button {
    flex: 1;
    padding: 8px 0;
    border: 0;
    border-radius: 8px;
    background: none;
    color: var(--muted);
    font: inherit;
    font-size: 14px;
    cursor: pointer;
  }
  .transfer-entry-tabs button[aria-pressed="true"] {
    background: var(--soft);
    color: var(--accent);
    font-weight: 600;
    box-shadow: 0 1px 2px #1013260a;
  }
  .transfer-entry-grid[data-tab="join"] > :first-child,
  .transfer-entry-grid[data-tab="create"] > :last-child {
    display: none;
  }
  .transfer-entry-grid {
    margin-top: 12px;
  }
}

/* 配对入口和等待页 */
.transfer-page {
  width: 100%;
  font-size: 13.5px;
}
.transfer-entry-grid {
  margin: 0;
  gap: 14px;
}
.transfer-entry-card {
  margin: 0;
  padding: 16px;
  gap: 10px;
  background: var(--surface);
}
.transfer-entry-heading {
  display: flex;
  align-items: center;
  gap: 10px;
}
.transfer-entry-heading h2 {
  color: var(--text);
  font-size: 14px;
}
.transfer-entry-heading > span {
  display: grid;
  flex: 0 0 32px;
  height: 32px;
  place-items: center;
  border-radius: 8px;
  color: var(--accent);
  background: var(--soft);
}
.transfer-entry-heading p {
  font-size: 12px;
}
.transfer-entry-card > label {
  margin: 0;
}
.transfer-entry-card .transfer-code-field > label {
  text-align: center;
  margin: 0 0 4px;
}
.transfer-code-input {
  display: grid;
  gap: 8px;
  justify-items: center;
}
.transfer-code-input .pair-code {
  width: 100%;
}
.transfer-code-actions {
  margin: 0;
}
.transfer-join-relay small {
  display: block;
}
.transfer-join-safety {
  display: flex;
  align-items: center;
  gap: 8px;
}
.transfer-join-safety .icon {
  flex: 0 0 15px;
  width: 15px;
  height: 15px;
}
.transfer-history-notice {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 8px;
  margin: 0 0 14px;
  padding: 10px 14px;
}
.transfer-history-notice > span {
  flex: 1;
  min-width: 200px;
}
.transfer-connection-settings {
  margin: 2px 0;
}
.transfer-connection-fields {
  padding-top: 10px;
}
.transfer-options p {
  margin: 5px 0 0;
}
.transfer-risk {
  color: var(--warning);
}
.transfer-entry-card > .ui-button {
  min-height: 36px;
}

.transfer-wait-heading {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 10px;
  margin-bottom: 14px;
}
.transfer-wait-heading h1 {
  margin: 0;
  font-size: 24px;
  font-weight: 750;
}
.transfer-wait-layout {
  display: grid;
  grid-template-columns: minmax(0, 1fr);
  gap: 14px;
}
.transfer-wait-layout.has-code {
  grid-template-columns: minmax(0, 1fr) 320px;
}
.transfer-wait-layout .panel {
  width: 100%;
  max-width: none;
  margin: 0;
  padding: 16px;
}
.transfer-waiting {
  display: grid;
  align-content: start;
  justify-items: center;
  gap: 10px;
  padding-block: 20px;
}
.transfer-waiting p {
  margin: 0;
}
.transfer-waiting .transfer-radar {
  margin: 6px auto;
}
.transfer-waiting .transfer-code {
  font-family: var(--page-mono);
  font-size: 52px;
  line-height: 1.12;
}
.transfer-code span {
  display: inline-block;
  background: var(--accent-gradient);
  background-clip: text;
  color: transparent;
}
.transfer-wait-actions,
.transfer-countdown {
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 8px;
  flex-wrap: wrap;
}
.transfer-countdown {
  color: var(--muted);
  font-size: 12px;
}
.transfer-countdown b {
  font-family: var(--page-mono);
  color: var(--text);
}
.transfer-countdown svg {
  width: 24px;
  height: 24px;
  fill: none;
  stroke-width: 2;
  transform: rotate(-90deg);
}
.transfer-countdown svg circle:first-child {
  stroke: var(--line);
}
.transfer-countdown svg circle:last-child {
  stroke: var(--accent);
  stroke-linecap: round;
}
.transfer-requests {
  align-self: start;
}
.transfer-joining {
  margin-block: 10px;
}
.transfer-joining dl {
  margin-block: 8px;
}

/* 连接栏与消息区 */
.mobile-chat-heading,
.mobile-chat-plus.ui-button {
  display: none;
}
.transfer-peer-status[data-state="connected"] {
  --peer-c: var(--success);
  --peer-bg: var(--success-bg);
}
.transfer-peer-status[data-state="waiting"] {
  --peer-c: var(--warning);
  --peer-bg: var(--warning-bg);
}
.transfer-peer-status[data-state="disconnected"] {
  --peer-c: var(--error);
  --peer-bg: var(--error-bg);
}
.transfer-page > .transfer-peer {
  padding: 9px 14px;
  margin-bottom: 12px;
}
.transfer-peer-bar {
  flex-wrap: nowrap;
}
.transfer-peer-info {
  flex: 0 1 auto;
}
.transfer-peer-bar > .transfer-peer-info {
  flex-wrap: nowrap;
}
.transfer-peer-device {
  flex: 0 1 auto;
}
.transfer-peer-device > div {
  flex: 0 1 auto;
  min-width: 0;
}
.transfer-peer-device > .icon {
  background: var(--surface-subtle);
  color: var(--muted);
  border-radius: 7px;
  width: 34px;
  height: 34px;
  padding: 8px;
}
.transfer-peer-device strong,
.transfer-peer-device small {
  display: block;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.transfer-peer-link {
  margin-left: 0;
}
.transfer-peer-actions {
  margin-left: auto;
}
.transfer-peer .transfer-peer-wire {
  width: 44px;
  min-width: 20px;
  gap: 0;
}
.transfer-peer .transfer-peer-wire[data-state="disconnected"] {
  gap: 7px;
}
.transfer-peer .transfer-peer-wire-line {
  height: 2px;
}
.transfer-peer-wire[data-state="connected"] .transfer-peer-wire-line {
  background: var(--success);
}
.transfer-peer-wire[data-state="waiting"] .transfer-peer-wire-line {
  background: repeating-linear-gradient(
    90deg,
    var(--warning) 0 6px,
    transparent 6px 10px
  );
  background-size: 10px 100%;
}
.transfer-connection-banner {
  display: flex;
  align-items: center;
  gap: 8px;
  color: var(--warning);
  background: var(--warning-bg);
  border: 1px solid color-mix(in srgb, currentColor 32%, var(--line));
  border-radius: 9px;
  padding: 8px 12px;
  margin-bottom: 12px;
  font-size: 12px;
}
.transfer-connection-banner.disconnected {
  color: var(--error);
  background: var(--error-bg);
}
.transfer-connection-banner > .icon {
  flex: 0 0 16px;
}
.transfer-connection-banner > div {
  display: flex;
  flex-wrap: wrap;
  column-gap: 8px;
  flex: 1;
  min-width: 0;
}
.transfer-connection-banner time {
  font-family: var(--page-mono);
  font-weight: 700;
}
.transfer-workspace {
  grid-template-columns: minmax(0, 1fr);
  gap: 14px;
}
.transfer-workspace .transfer-chat {
  grid-row: auto;
  min-height: 0;
  height: 522px;
}
.transfer-workspace .transfer-details-panel,
.transfer-workspace .transfer-file-panel {
  padding: 12px 14px;
}
.transfer-conversation-sidebar {
  display: grid;
  gap: 14px;
  min-width: 0;
}
.transfer-chat .transfer-section-heading {
  padding: 10px 14px;
}
.transfer-section-heading h2 {
  font-size: 14px;
}
.transfer-messages {
  padding: 14px;
  gap: 10px;
  background: var(--surface-subtle);
  opacity: 1;
  filter: none;
  overscroll-behavior: contain;
}
:global(html[data-theme="dark"]) .transfer-messages {
  background: #0003;
}
.transfer-messages.dragging {
  outline: 2px dashed var(--accent);
  outline-offset: -3px;
  background: var(--soft);
}
.transfer-message {
  padding: 8px 10px;
  border-radius: 10px 10px 10px 3px;
  border-color: var(--line);
  background: var(--surface);
}
.transfer-message.mine {
  border-radius: 10px 10px 3px 10px;
  background: var(--accent-gradient);
  color: white;
}
.transfer-message.mine .transfer-message-meta,
.transfer-message.mine .muted {
  color: #ffffffe0;
}
.transfer-message.mine .transfer-message-remove {
  color: white;
}
.transfer-message-header {
  margin-bottom: 4px;
}
.transfer-system-message {
  margin: 4px 0;
  text-align: center;
  color: var(--muted);
  font-size: 11.5px;
}
.transfer-bubble-file {
  display: flex;
  align-items: center;
  gap: 8px;
  min-width: 0;
}
.transfer-bubble-file strong,
.transfer-bubble-file small {
  display: block;
  overflow-wrap: anywhere;
}
.transfer-bubble-file small {
  margin-top: 2px;
}
.transfer-bubble-file .file-ext,
.transfer-file-head .file-ext {
  width: 30px;
  height: 30px;
  flex: 0 0 30px;
  border-radius: 6px;
}
.transfer-bubble-file .icon {
  width: 14px;
  height: 14px;
}
.transfer-bubble-downloads {
  display: flex;
  flex-wrap: wrap;
  gap: 4px;
  margin-top: 6px;
}
.transfer-bubble-downloads:empty,
.transfer-file-actions:empty {
  display: none;
}
.transfer-chat .transfer-message-composer {
  padding: 8px 10px;
  border-top: 1px solid var(--line);
  gap: 6px;
}
.transfer-composer-tools {
  align-items: center;
}
.transfer-composer-tools :deep(.ui-button) {
  width: 29px;
  padding: 0;
}
.transfer-chat-queue {
  padding: 8px 12px;
  border-top: 1px solid var(--line);
  max-height: 180px;
  overflow: auto;
}
.transfer-chat-queue .transfer-queue {
  margin: 0;
  padding: 6px 0 0;
  border: 0;
}
.transfer-receive-settings {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 5px;
  margin: 8px 0 10px;
  color: var(--muted);
  font-size: 11.5px;
}
.transfer-receive-settings .transfer-stepper {
  height: 26px;
}
.transfer-receive-settings .transfer-stepper button {
  width: 25px;
}
.transfer-receive-settings .transfer-stepper input {
  width: 30px;
  color: var(--text);
}
.transfer-receive-settings .page-segment button {
  padding-inline: 5px;
  min-height: 22px;
}
.transfer-file-empty {
  display: grid;
  justify-items: center;
  gap: 6px;
  padding: 10px 8px;
  margin: 5px 0;
  font-size: 12px;
  text-align: center;
}
.transfer-file-empty strong {
  color: var(--text);
  font-weight: 650;
}
.transfer-file-empty .empty-illustration {
  width: 80px;
  height: 60px;
}
.transfer-file-actions {
  grid-auto-flow: row;
  grid-template-columns: repeat(2, minmax(0, 1fr));
}
.transfer-file-actions :deep(.transfer-action) {
  padding-inline: 4px;
  overflow: visible;
  font-size: 12px;
  min-width: 0;
}
.transfer-file-actions :deep(.transfer-download-choice.primary) {
  box-shadow: none;
}
.transfer-file-actions :deep(.transfer-download-choice:hover) {
  transform: none;
}
.transfer-file-actions > :only-child {
  grid-column: 1 / -1;
}
.transfer-file .transfer-file-copy {
  min-width: 0;
}
.transfer-file-title {
  gap: 5px;
  flex-wrap: wrap;
}
.transfer-file-title strong {
  overflow-wrap: anywhere;
}
.transfer-details-panel dd {
  font-family: var(--page-mono);
  font-size: 12px;
  overflow-wrap: anywhere;
}

@media (min-width: 901px) {
  .transfer-workspace {
    grid-template-columns: minmax(0, 1fr) 280px;
  }
}
@media (max-width: 900px) {
  .transfer-peer-info {
    gap: 8px;
  }
  /* 连接状态和操作占一行，两台设备共用下一行。 */
  :global(#app .transfer-peer > .transfer-peer-bar) {
    display: grid;
    grid-template-columns: auto minmax(0, 1fr) auto;
    grid-template-areas:
      "status link actions"
      "devices devices devices";
    gap: 8px 12px;
  }
  :global(#app .transfer-peer-bar > .transfer-peer-info) {
    display: contents;
  }
  .transfer-peer-bar .transfer-peer-status {
    grid-area: status;
  }
  .transfer-peer-bar .transfer-peer-sep {
    display: none;
  }
  .transfer-peer-bar .transfer-peer-link {
    grid-area: link;
    justify-self: start;
  }
  .transfer-peer-bar .transfer-peer-actions {
    grid-area: actions;
  }
  .transfer-peer-devices {
    display: flex;
    grid-area: devices;
    align-items: center;
    gap: 12px;
  }
}
@media (max-width: 720px) {
  .transfer-wait-layout.has-code {
    grid-template-columns: minmax(0, 1fr) 240px;
  }
}
@media (max-width: 650px) {
  .transfer-page > .hero .muted {
    display: none;
  }
  .transfer-entry-tabs {
    margin: 0 0 14px;
  }
  .transfer-entry-grid {
    margin: 0;
    grid-template-columns: minmax(0, 1fr);
  }
  .transfer-entry-heading {
    display: none;
  }
  .transfer-entry-card {
    padding: 18px;
    gap: 16px;
  }
  .transfer-entry-card .transfer-code-field {
    padding-block: 8px 9px;
  }
  .transfer-entry-card .transfer-code-field > label {
    position: absolute;
    width: 1px;
    height: 1px;
    overflow: hidden;
    clip-path: inset(50%);
  }
  .transfer-code-actions :deep(.ui-button) {
    height: 32px;
  }
  .transfer-entry-card .primary {
    height: 48px;
    border-radius: 12px;
    font-size: 16px;
  }
  .transfer-wait-layout.has-code {
    grid-template-columns: minmax(0, 1fr);
  }
  .transfer-waiting .transfer-radar {
    width: 120px;
    height: 120px;
  }
  .transfer-waiting .transfer-code {
    font-size: 44px;
  }
  .transfer-wait-heading h1 {
    font-size: 20px;
  }
  :global(html:has(.transfer-conversation) #app) {
    padding-bottom: 0;
  }
  :global(html:has(.transfer-conversation) .site-header),
  :global(html:has(.transfer-conversation) .site-announcement),
  :global(html:has(.transfer-conversation) .site-footer),
  :global(html:has(.transfer-conversation) .custom-content) {
    display: none;
  }
  :global(html:has(.transfer-conversation) .site-main) {
    padding: 0;
  }
  .transfer-conversation {
    height: 100dvh;
    display: flex;
    flex-direction: column;
    overflow: hidden;
  }
  .transfer-page > .transfer-peer {
    display: none;
  }
  .mobile-chat-heading {
    display: flex;
    align-items: center;
    gap: 6px;
    min-height: 54px;
    padding: max(7px, env(safe-area-inset-top)) 10px 7px;
    background: linear-gradient(90deg, var(--soft), var(--surface) 70%);
    border-bottom: 1px solid var(--line);
  }
  .mobile-chat-heading .icon-button {
    width: 36px;
    height: 36px;
    padding: 0;
  }
  .mobile-chat-who {
    flex: 1;
    min-width: 0;
  }
  .mobile-chat-who strong {
    display: block;
    overflow: hidden;
    white-space: nowrap;
    text-overflow: ellipsis;
  }
  .mobile-chat-who .transfer-peer-status {
    gap: 7px;
    font-size: 11.5px;
  }
  .mobile-chat-who .transfer-peer-status i {
    width: 6px;
    height: 6px;
    box-shadow: 0 0 0 3px var(--peer-bg);
  }
  .mobile-chat-file-action {
    position: relative;
  }
  .mobile-chat-file-action b {
    position: absolute;
    inset: 1px 0 auto auto;
    display: grid;
    place-items: center;
    min-width: 15px;
    height: 15px;
    border-radius: 99px;
    background: var(--accent);
    color: white;
    font-size: 9px;
    pointer-events: none;
  }
  .transfer-connection-banner {
    margin: 0;
    border-radius: 0;
    border-width: 0 0 1px;
  }
  .transfer-workspace {
    display: flex;
    flex-direction: column;
    flex: 1;
    min-height: 0;
    gap: 0;
  }
  .transfer-workspace .transfer-chat {
    flex: 1;
    width: 100%;
    height: auto;
    min-height: 0;
    padding: 0;
    border: 0;
    border-radius: 0;
  }
  .transfer-chat .transfer-section-heading {
    display: none;
  }
  .transfer-conversation-sidebar {
    display: contents;
  }
  .transfer-conversation-sidebar .panel {
    padding: 0;
    border: 0;
    box-shadow: none;
    background: transparent;
  }
  .transfer-details-actions {
    justify-content: flex-end;
    margin-top: 16px;
  }
  .transfer-details-panel .transfer-section-heading h2,
  .transfer-file-panel .transfer-section-heading h2 {
    display: none;
  }
  .transfer-messages {
    padding: 12px;
  }
  .transfer-chat .transfer-message-composer {
    position: static;
    padding-block: 8px calc(8px + env(safe-area-inset-bottom));
    align-items: center;
  }
  .transfer-composer-tools {
    display: none;
  }
  .mobile-chat-plus.ui-button {
    display: inline-flex;
    width: 36px;
    height: 36px;
    padding: 0;
    background: var(--soft);
    color: var(--accent);
    border-radius: 50%;
  }
  .transfer-send {
    height: 42px;
    border-radius: 99px;
  }
  .transfer-message-composer :deep(.ui-input) {
    border-radius: 18px;
  }
  .transfer-message-composer :deep(.n-input__textarea-el) {
    min-height: 40px;
    padding-block: 7px;
  }
  .transfer-message-composer :deep(.n-input__placeholder) {
    display: block;
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .transfer-attachment-options {
    display: grid;
    grid-template-columns: repeat(3, minmax(0, 1fr));
    gap: 8px;
  }
  .transfer-attachment-options button {
    display: grid;
    justify-items: center;
    gap: 8px;
    padding: 12px 4px;
    border: 1px solid var(--line);
    border-radius: 12px;
    background: var(--surface);
    color: var(--text);
  }
  .transfer-attachment-options .icon {
    box-sizing: content-box;
    width: 20px;
    height: 20px;
    padding: 10px;
    color: white;
    border-radius: 10px;
    background: var(--accent-gradient);
  }
}
@media (prefers-reduced-motion: no-preference) {
  .transfer-peer-wire[data-state="waiting"] .transfer-peer-wire-line {
    animation: peer-flow 0.55s linear infinite;
  }
  .transfer-code span {
    animation: code-enter 0.44s var(--motion-spring) both;
  }
  .transfer-code span:nth-child(2) {
    animation-delay: 45ms;
  }
  .transfer-code span:nth-child(3) {
    animation-delay: 90ms;
  }
  .transfer-code span:nth-child(4) {
    animation-delay: 135ms;
  }
  .transfer-connection-banner {
    animation: connecting-enter 0.32s var(--motion-ease);
  }
  @keyframes code-enter {
    from {
      opacity: 0;
      transform: translateY(7px) scale(0.8);
    }
  }
  @keyframes connecting-enter {
    from {
      opacity: 0;
      transform: translateY(-4px);
    }
  }
  @keyframes peer-flow {
    to {
      background-position: 10px 0;
    }
  }
}

/* ===== 对照设计稿的修正（2026-11-23 晚） ===== */
/* 雷达：扫描光只显示在圆环内，不再溢出成扇形 */
.transfer-radar {
  overflow: hidden;
  border-radius: 50%;
}
.transfer-radar::after {
  inset: 2px;
  background: conic-gradient(
    from 0deg,
    color-mix(in srgb, var(--accent) 22%, transparent) 0deg,
    transparent 70deg,
    transparent 360deg
  );
  -webkit-mask: radial-gradient(circle, transparent 38%, #000 39%);
  mask: radial-gradient(circle, transparent 38%, #000 39%);
}
/* 配对码：整体渐变字，不按单字重复渐变 */
.transfer-waiting .transfer-code {
  display: inline-flex;
  gap: 0.06em;
  letter-spacing: 0.08em;
  background: linear-gradient(135deg, var(--accent), var(--accent-2, var(--accent)));
  -webkit-background-clip: text;
  background-clip: text;
  color: transparent;
}
.transfer-code span {
  background: none !important;
  color: inherit;
}
/* 等待对方确认：补充说明 */
.transfer-wait-title {
  font-size: 16px;
  font-weight: 650;
  color: var(--text);
}
.transfer-waiting > .muted {
  max-width: 420px;
  text-align: center;
}
.transfer-wait-peer {
  display: flex;
  gap: 10px;
  margin: 4px 0 0;
  padding: 8px 14px;
  border-radius: 10px;
  background: var(--surface-subtle);
  font-size: 13px;
}
.transfer-wait-peer div {
  display: flex;
  gap: 10px;
}
.transfer-wait-peer dt {
  color: var(--muted);
}
.transfer-wait-peer dd {
  margin: 0;
  font-weight: 600;
}
/* 历史提示与下方卡片等宽 */
.transfer-history-notice {
  width: 100%;
  box-sizing: border-box;
}
/* 消息气泡：名字与时间之间留空，正文与头部分开 */
.transfer-message {
  min-width: 140px;
  padding: 10px 14px;
}
.transfer-message-header {
  display: flex;
  align-items: baseline;
  justify-content: space-between;
  gap: 12px;
  margin-bottom: 6px;
}
.transfer-message-meta {
  gap: 10px;
}
.transfer-message p {
  margin: 0;
  line-height: 1.55;
}
/* 文件列表：标题与“自动接收”同一行对齐，接收设置单独一行、间距宽松 */
.transfer-file-panel .transfer-section-heading {
  flex-wrap: nowrap;
  align-items: center;
}
.transfer-file-panel .transfer-auto-receive {
  margin: 0 !important;
  font-size: 13px;
  white-space: nowrap;
}
.transfer-receive-settings {
  display: grid;
  grid-template-columns: auto auto 1fr auto auto;
  align-items: center;
  gap: 8px;
  margin: 12px 0 12px;
  font-size: 12.5px;
}
.transfer-receive-settings .page-segment button {
  padding-inline: 10px;
  min-height: 26px;
}
.transfer-receive-settings .transfer-stepper {
  height: 28px;
}
.transfer-receive-settings .transfer-stepper button {
  width: 28px;
}
.transfer-receive-settings .transfer-stepper input {
  width: 36px;
}
/* STUN 只显示第一个服务器和数量，完整列表放在悬停提示里 */
.transfer-stun-summary {
  word-break: normal;
  overflow-wrap: anywhere;
}

/* 雷达：加底环与浅底，扫描光不再像孤立的扇形 */
.transfer-radar {
  background: radial-gradient(
    circle,
    color-mix(in srgb, var(--accent) 6%, transparent) 0 60%,
    transparent 61%
  );
  box-shadow: inset 0 0 0 1px color-mix(in srgb, var(--accent) 22%, transparent);
}
.transfer-radar::before {
  content: "";
  position: absolute;
  inset: 22%;
  border-radius: 50%;
  border: 1px dashed color-mix(in srgb, var(--accent) 30%, transparent);
}
/* 接收设置：一行排开，窄时整组换行，文字不拆字 */
.transfer-receive-settings {
  display: flex !important;
  flex-wrap: wrap;
  align-items: center;
  gap: 8px 10px;
}
.transfer-receive-settings > span {
  white-space: nowrap;
}
.transfer-receive-settings .transfer-stepper {
  flex: none;
}

.transfer-receive-pair {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  white-space: nowrap;
}
</style>
