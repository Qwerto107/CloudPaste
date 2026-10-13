<script setup>
import { computed, nextTick, onBeforeUnmount, ref, watch } from "vue";
import { NModal } from "naive-ui";
import { onBeforeRouteLeave } from "vue-router";
import { session, hasPermission } from "./session.js";
import Icon from "./Icon.vue";
import { chatImageHistory, validateChatImage } from "./transfer-images.js";
import {
  TransferClient,
  canSaveDirectly,
  transferID,
} from "./transfer-client.js";

// 每次进入页面生成短随机后缀，方便区分参与配对的设备，用户仍可自行修改。
const name = ref(`我的设备-${transferID().slice(0, 6)}`);
const code = ref("");
const pastingCode = ref(false);
const pairingCode = ref("");
const relayOnly = ref(false);
const autoReceive = ref(false);
const error = ref("");
const status = ref("idle");
const joining = ref("");
const names = ref([]);
const messages = ref([]);
const messageList = ref(null);
let followMessages = true;
const text = ref("");
const files = ref([]);
const selected = ref([]);
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
const canCreate = computed(() => hasPermission(3));
const active = computed(() =>
  files.value.some((file) => file.state === "active"),
);
const connected = computed(() => status.value === "paired");
const canDirect = computed(() => direct.value && canSaveDirectly());
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
  files.value.filter((file) => !file.image_type),
);
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
  if (message.type === "created") {
    pairingCode.value = message.code || "";
    myMember.value = message.member;
    maxFile.value = message.max_file_size;
    status.value = "connecting";
  } else if (message.type === "ready") status.value = "waiting";
  else if (message.type === "joining") joining.value = message.name;
  else if (message.type === "paired") {
    status.value = "paired";
    names.value = message.names;
    pairingCode.value = "";
    joining.value = "";
  } else if (message.type === "direct") direct.value = message.ready;
  else if (message.type === "text") {
    appendMessage({ ...message, id: transferID() });
  } else if (message.type === "file") {
    offeredCount.value++;
    files.value.push({
      ...message,
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
    ["state", "progress", "download", "download-tool"].includes(message.type)
  ) {
    const file = files.value.find((item) => item.id === message.id);
    if (!file) return;
    if (message.type === "progress" && Number.isSafeInteger(message.round)) {
      if (message.round < (file.progressRound || 0)) return;
      if (message.round > (file.progressRound || 0)) {
        // 重新下载允许进度归零，旧一轮进度不得覆盖新一轮；速率也从本轮重新采样。
        file.progressRound = message.round;
        file.bytes = 0;
        file.speed = 0;
        file.samples = [];
        file.sampleBytes = 0;
        file.measured = performance.now();
      }
      file.requested = true;
    }
    if (message.type === "download-tool") {
      // 手动中继接收先展示下载操作，浏览器和外部工具共用此链接，不提前发起 GET。
      file.url = client.downloadURL(message.url);
      file.expiresLabel = new Date(message.expires_at).toLocaleTimeString(
        "zh-CN",
        { hour12: false },
      );
      file.maxConnections = message.max_connections;
    } else if (message.type === "download") {
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
  } else if (message.type === "error") error.value = message.message;
  else if (message.type === "closed") {
    autoReceive.value = false;
    clearImages();
    status.value = "closed";
    direct.value = false;
    pairingCode.value = "";
    joining.value = "";
    for (const file of files.value) {
      file.url = "";
      if (["active", "offered"].includes(file.state)) file.state = "cancelled";
    }
  }
}
async function open(kind) {
  error.value = "";
  busy.value = true;
  autoReceive.value = false;
  client?.close();
  clearImages();
  files.value = [];
  offeredCount.value = 0;
  selected.value = [];
  messages.value = [];
  followMessages = true;
  pairingCode.value = "";
  client = new TransferClient(event);
  try {
    await client.open(kind, {
      name: name.value,
      relay_only: relayOnly.value,
      ...(kind === "join" ? { code: code.value } : {}),
    });
  } catch (reason) {
    error.value = reason.message;
    client.close();
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
  addFiles(event.dataTransfer.files);
}
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
    // 手动接收只按传输方式选择：中继先展示链接，点对点直接选择保存位置。
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
  } catch {
    error.value = "复制失败，请选择下方下载链接手动复制";
  }
}
async function copyCode() {
  try {
    await navigator.clipboard.writeText(pairingCode.value);
  } catch {
    error.value = "复制失败，请手动选择配对码复制";
  }
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
    const value = clipboard.replace(/[\s-]/g, "").toUpperCase();
    if (!/^[A-HJ-NP-Z2-9]{8}$/.test(value)) {
      error.value = "剪贴板中不是有效的 8 位配对码";
      return;
    }
    code.value = value;
    await nextTick();
    document.getElementById("transfer-pair-code")?.focus();
  } catch {
    error.value = "无法读取剪贴板，请在配对码输入框中手动粘贴";
  } finally {
    pastingCode.value = false;
  }
}
function leave() {
  client?.close();
}
// 标签只放短状态，保存确认与重试说明另起一行；中继转发完成不等同于写盘成功。
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
        detail: "接收方已确认完成，下载链接已失效。",
      };
    return {
      label: deliveredLabel,
      kind: "complete",
      detail: sentByMe
        ? "保存结果以接收方的下载记录为准。"
        : "请在浏览器下载记录中确认保存结果。",
    };
  }
  if (file.state === "active") {
    if (file.mode === "tool") {
      if (file.bytes === file.size && (file.size > 0 || file.requested))
        return {
          label: deliveredLabel,
          kind: "complete",
          detail: sentByMe
            ? "请等待接收方确认保存结果并完成接收。"
            : "请在浏览器或下载工具中确认保存结果，再点击完成接收。",
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
  if (connected.value || active.value) {
    event.preventDefault();
    event.returnValue = "";
  }
}
window.addEventListener("beforeunload", beforeUnload);
// 速率每秒采样一次，取最近三个采样区间的平均值，不跟随分块消息跳动。
const speedTimer = setInterval(() => {
  const now = performance.now();
  for (const file of files.value) {
    if (file.state !== "active") continue;
    const elapsed = now - file.measured;
    if (elapsed < 1000) continue;
    file.samples.push({
      bytes: Math.max(0, file.bytes - file.sampleBytes),
      elapsed,
    });
    if (file.samples.length > 3) file.samples.shift();
    file.speed =
      (file.samples.reduce((sum, item) => sum + item.bytes, 0) * 1000) /
      file.samples.reduce((sum, item) => sum + item.elapsed, 0);
    file.sampleBytes = file.bytes;
    file.measured = now;
  }
}, 1000);
onBeforeRouteLeave(() => {
  if (
    connected.value &&
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
});
</script>

<template>
  <div class="transfer-page">
    <div class="page-heading">
      <div>
        <p class="eyebrow">文字与文件，即时互传</p>
        <h1>设备互传</h1>
        <p class="muted">输入配对码，互发文字和文件。双方请保持页面在线。</p>
      </div>
    </div>
    <p v-if="error" class="error" role="alert">{{ error }}</p>

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
      <div class="transfer-entry-grid">
        <div class="transfer-entry-card">
          <h2>创建连接</h2>
          <p class="muted">生成 5 分钟有效的配对码，确认对方加入后即可互传。</p>
          <UiButton
            v-if="canCreate"
            class="primary"
            icon="link"
            :disabled="busy"
            @click="open('create')"
            >创建配对码</UiButton
          >
          <RouterLink
            v-else-if="session.type === 'none'"
            :to="{ path: '/login', query: { redirect: '/transfer' } }"
            >登录后创建连接</RouterLink
          >
          <p v-else class="muted">创建连接需要文本和文件分享权限。</p>
        </div>
        <form class="transfer-entry-card" @submit.prevent="open('join')">
          <h2>加入连接</h2>
          <div class="transfer-code-field">
            <label for="transfer-pair-code">配对码</label>
            <div class="transfer-code-input">
              <UiInput
                id="transfer-pair-code"
                v-model="code"
                aria-label="配对码"
                placeholder="输入 8 位配对码"
                maxlength="12"
                autocomplete="off"
                autocapitalize="characters"
              />
              <UiButton
                type="button"
                aria-label="粘贴配对码"
                :disabled="busy || pastingCode"
                @click="pasteCode"
                >{{ pastingCode ? "读取中" : "粘贴" }}</UiButton
              >
            </div>
          </div>
          <UiButton
            class="primary"
            icon="transfer"
            :disabled="busy || !code.trim()"
            >加入连接</UiButton
          >
        </form>
      </div>
      <p class="muted transfer-note">
        中继经本站转发；直连会向对方提供网络地址。请只与信任的人配对。
      </p>
    </section>

    <section v-else-if="!connected" class="panel transfer-waiting">
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
          {{ pairingCode.slice(0, 4) }} {{ pairingCode.slice(4) }}
        </p>
        <UiButton type="button" @click="copyCode">复制配对码</UiButton>
        <p class="muted">配对成功后失效。请通过可信渠道把配对码发送给对方。</p>
      </template>
      <div v-if="joining" class="transfer-joining" role="status">
        <p>
          <strong>{{ joining }}</strong> 请求加入。请确认这是你要连接的设备。
        </p>
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
      <div class="transfer-peer">
        <div class="transfer-peer-info">
          <span>已连接：{{ names[1 - myMember] }}</span>
          <span class="transfer-badge">{{
            direct ? "直连可用 · 支持实时中继" : "实时中继可用"
          }}</span>
        </div>
        <UiButton class="quiet" @click="leave">断开连接</UiButton>
      </div>
      <div class="transfer-workspace">
        <section class="panel transfer-chat">
          <h2>消息</h2>
          <div
            class="transfer-messages"
            ref="messageList"
            @scroll="trackMessageScroll"
            role="log"
            aria-label="对话消息"
            aria-live="polite"
          >
            <p v-if="!messages.length" class="muted">
              还没有消息，发送文字或粘贴图片开始对话。
            </p>
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
          <form @submit.prevent="sendText" @paste="pasteImage">
            <UiInput
              v-model="text"
              type="textarea"
              aria-label="发送文字"
              placeholder="输入文字，或粘贴图片"
              :rows="3"
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
              class="primary"
              :disabled="pasting || (!text.trim() && !pastedImages.length)"
              >{{ pastedImages.length ? "发送消息" : "发送文字" }}</UiButton
            >
          </form>
          <p class="muted transfer-hint">
            可粘贴 PNG / JPEG 图片，单张最多 4 MiB。
          </p>
        </section>

        <section class="panel transfer-file-panel">
          <h2>文件互传</h2>
          <div class="transfer-auto-receive">
            <label class="check"
              ><UiCheckbox v-model="autoReceive" />自动接收对方文件</label
            >
            <p class="muted transfer-hint">仅本次连接有效，按顺序中继下载。</p>
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
              <strong>{{ dragDepth ? "松开即可添加文件" : "拖入文件" }}</strong
              ><span class="muted">可多选，再次选择会追加</span>
            </div>
            <UiButton
              type="button"
              class="transfer-choose"
              @click="fileInput?.click()"
              >选择文件</UiButton
            >
            <input
              ref="fileInput"
              hidden
              type="file"
              multiple
              aria-label="选择互传文件"
              @change="choose"
            />
          </div>
          <p class="muted transfer-hint">
            单文件最多 {{ size(maxFile) }} · 本次最多 32 个
          </p>
          <ul v-if="selected.length" class="transfer-selected">
            <li v-for="(file, index) in selected" :key="index">
              <span>{{ file.name }} · {{ size(file.size) }}</span
              ><UiButton
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
          <UiButton v-if="selected.length" class="primary" @click="offer"
            >发送所选文件（{{ selected.length }}）</UiButton
          >
          <p v-if="!ordinaryFiles.length" class="muted transfer-empty">
            暂无文件，发送或接收后将在这里显示。
          </p>
          <article
            v-for="file in ordinaryFiles"
            :key="file.id"
            class="transfer-file"
          >
            <div class="transfer-file-title">
              <strong>{{ file.name }}</strong
              ><span>{{ size(file.size) }}</span>
              <UiButton
                v-if="['cancelled', 'failed', 'complete'].includes(file.state)"
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
              <span class="muted transfer-direction">{{
                file.from === myMember ? "我发送的" : "对方发送的"
              }}</span>
              <span class="transfer-tag" :class="fileStatus(file).kind">{{
                fileStatus(file).label
              }}</span>
              <!-- 中继下载共用同一入口，标签仅区分中继和点对点传输方式。 -->
              <span
                v-if="file.mode"
                class="transfer-tag"
                :class="file.mode === 'p2p' ? 'p2p' : 'relay'"
              >
                {{ file.mode === "p2p" ? "点对点" : "实时中继" }}</span
              >
            </div>
            <p
              v-if="fileStatus(file).detail"
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
              class="muted"
            >
              {{ size(file.bytes) }} / {{ size(file.size) }} ·
              {{ size(file.speed) }}/s
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
                    file.bytes === file.size && file.requested
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
                  :disabled="file.bytes !== file.size || !file.requested"
                  title="请先在浏览器或下载工具中确认保存成功；完成接收后下载链接失效"
                  @click="send({ type: 'finish-download', id: file.id })"
                  >完成接收</UiButton
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
            <div
              v-if="file.url && file.mode === 'tool'"
              class="transfer-tool-link"
            >
              <UiInput
                :model-value="file.url"
                readonly
                :aria-label="`${file.name} 下载链接`"
                @focus="$event.target.select()"
              />
              <p class="transfer-hint muted">
                点击“下载文件”直接下载，也可复制链接到下载工具（支持 AB
                扩展接管）。最多
                {{ file.maxConnections }} 路连接，链接到
                {{ file.expiresLabel }}
                有效。确认保存后点击“完成接收”，链接即失效。
              </p>
            </div>
          </article>
        </section>
      </div>
    </template>
    <!-- 常用操作就近提示，详细限制集中折叠，避免传输卡片被说明文字占满。 -->
    <details class="panel transfer-help">
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
          文件逐个接收。选择“中继下载”后，可直接下载或复制链接到下载工具，中继边传边下载；“点对点下载”取决于浏览器支持。开启自动接收后直接启动中继下载，浏览器仍可能询问保存位置或多文件下载许可。
        </dd>
        <dt>连接与隐私</dt>
        <dd>
          每次连接仅限两台设备，双方请保持页面在线。刷新或离开会断开连接，并取消未完成的传输。本站不保存中继文件，但服务端可接触内容；直连可能让对方得知网络地址。
        </dd>
        <dt>下载工具</dt>
        <dd>
          选择“中继下载”后，可通过“下载文件”直接下载，或将链接交给 AB Download
          Manager
          等软件下载。支持文件探测、分段和本次连接内重试，使用实时中继，双方页面必须在线。链接最长
          30 分钟有效，停用 10
          分钟会失效；确认保存后点击“完成接收”，链接即失效。取消传输也会撤销链接。链接包含临时下载权限，请勿转发。自动接收仍使用一次性链接。
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
  /* 与文本、文件分享使用相同内容宽度，标题和卡片沿同一条左边线。 */
  max-width: 960px;
  margin: 0 auto;
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
  align-items: center;
  gap: 8px;
  min-width: 0;
}
.transfer-code-input .ui-input {
  flex: 1;
  min-width: 0;
}
.transfer-code-input .ui-button {
  flex-shrink: 0;
}
.transfer-note {
  font-size: 12px;
  line-height: 1.7;
  margin-top: 18px;
}
.transfer-badge {
  font-size: 13px;
  color: var(--accent);
}
.transfer-waiting {
  text-align: center;
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
.transfer-peer,
.transfer-file-title,
.transfer-selected li {
  display: flex;
  justify-content: space-between;
  align-items: center;
  gap: 12px;
}
.transfer-peer {
  margin-bottom: 16px;
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
  margin-top: 12px;
  min-width: 0;
}
.transfer-empty {
  margin: 20px 0 0;
  padding: 16px;
  text-align: center;
  font-size: 13px;
  background: var(--surface-subtle);
  border-radius: 8px;
}
.transfer-help {
  margin: 20px auto 0;
  padding: 16px 20px;
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
  grid-template-columns: minmax(0, 0.85fr) minmax(0, 1.15fr);
}
.transfer-workspace .panel {
  min-width: 0;
  width: 100%;
  margin: 0;
}
.transfer-messages {
  height: 320px;
  overflow: auto;
  padding: 8px 0;
  display: flex;
  flex-direction: column;
  gap: 10px;
}
.transfer-message {
  /* 每条消息保持自身高度，消息区滚动时不会压缩或遮住图片的操作栏。 */
  flex-shrink: 0;
  min-width: 0;
  max-width: 92%;
  border: 1px solid var(--line);
  border-radius: 10px;
  padding: 8px 12px;
  align-self: flex-start;
}
.transfer-message.mine {
  align-self: flex-end;
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
  gap: 10px;
  margin-top: 12px;
}
.transfer-drop {
  display: flex;
  align-items: center;
  gap: 14px;
  border: 1px dashed var(--accent);
  border-radius: 12px;
  padding: 22px 18px;
  background: var(--surface-subtle);
  transition:
    background 0.15s,
    border-color 0.15s;
}
.transfer-auto-receive {
  margin-bottom: 16px;
}
.transfer-auto-receive .transfer-hint {
  margin-top: 6px;
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
  width: 44px;
  height: 44px;
  flex-shrink: 0;
  border-radius: 12px;
  color: var(--accent);
  background: var(--surface);
}
.transfer-drop-icon .icon {
  width: 24px;
  height: 24px;
}
.transfer-drop-copy {
  display: grid;
  gap: 5px;
  flex: 1;
  min-width: 0;
}
.transfer-drop-copy strong {
  font-size: 14px;
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
.transfer-file-title strong {
  flex: 1;
}
@media (max-width: 600px) {
  .transfer-help-content {
    grid-template-columns: minmax(0, 1fr);
    gap: 4px;
  }
  .transfer-help-content dd + dt {
    margin-top: 8px;
  }
  .transfer-drop {
    flex-wrap: wrap;
  }
  .transfer-choose {
    width: 100%;
  }
}
.transfer-selected {
  list-style: none;
  padding: 0;
}
.transfer-selected li {
  border-bottom: 1px solid var(--line);
  padding: 6px 0;
}
.transfer-selected span,
.transfer-file-title strong {
  overflow-wrap: anywhere;
  min-width: 0;
}
.transfer-file-title span {
  flex-shrink: 0;
  font-size: 12px;
}
.transfer-file {
  border-top: 1px solid var(--line);
  padding: 14px 0;
}
.transfer-file p {
  margin: 6px 0;
  font-size: 12px;
}
.transfer-file-meta {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 6px;
  margin: 8px 0;
}
.transfer-direction {
  margin-right: 2px;
  font-size: 12px;
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
  width: 100%;
  height: 8px;
  accent-color: var(--accent);
}
.transfer-file-actions {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-wrap: wrap;
}
/* 文件操作保持相同高度，宽度随文案变化，避免短按钮留白过多。 */
.transfer-file-actions .transfer-action {
  padding: 0 12px;
}
.transfer-download {
  font-size: 14px;
  text-decoration: underline;
}
@media (max-width: 800px) {
  .transfer-entry-grid,
  .transfer-workspace {
    grid-template-columns: 1fr;
  }
  .transfer-messages {
    height: 300px;
  }
}
</style>
