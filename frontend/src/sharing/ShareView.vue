<script setup>
import {
  computed,
  defineAsyncComponent,
  onMounted,
  ref,
  watch,
  onBeforeUnmount,
} from "vue";
import { useRoute } from "vue-router";
import { request, contentURL } from "./api.js";
import { settings } from "./settings.js";
const MarkdownPreview = defineAsyncComponent(
  () => import("./MarkdownPreview.vue"),
);
const route = useRoute();
const record = ref(null);
const password = ref("");
const needsPassword = ref(false);
const busy = ref(false);
const error = ref("");
const text = ref("");
const truncated = ref(false);
const markdown = ref(true);
let controller;
const isFile = computed(() => route.meta.fileShare === true);
const url = computed(() =>
  record.value?.contentUrl ? contentURL(record.value.contentUrl) : "",
);
const extension = computed(() =>
  (record.value?.filename || "").split(".").pop().toLowerCase(),
);
function matches(key, fallback) {
  return String(settings[key] ?? fallback)
    .toLowerCase()
    .split(/[\s,]+/)
    .includes(extension.value);
}
const kind = computed(() => {
  if (!isFile.value) return "text";
  if (
    matches(
      "preview_text_types",
      "txt,md,markdown,json,xml,csv,log,yaml,yml,html,css,js,go,rs,py",
    )
  )
    return "text";
  if (matches("preview_image_types", "jpg,jpeg,png,gif,webp,avif,bmp,svg"))
    return "image";
  if (matches("preview_video_types", "mp4,webm,ogv,mov")) return "video";
  if (matches("preview_audio_types", "mp3,wav,ogg,flac,m4a,aac,opus"))
    return "audio";
  return "download";
});
async function readText(value, signal) {
  const response = await fetch(value, { cache: "no-store", signal });
  if (!response.ok)
    throw new Error(`文件读取失败（${response.status}），链接可能已过期`);
  const reader = response.body.getReader();
  const decoder = new TextDecoder();
  let size = 0;
  let output = "";
  const maximum =
    Math.min(Math.max(Number(settings.preview_max_text_kb) || 512, 16), 2048) *
    1024;
  try {
    for (;;) {
      const part = await reader.read();
      if (part.done) break;
      const remaining = maximum - size;
      output += decoder.decode(part.value.subarray(0, Math.max(remaining, 0)), {
        stream: true,
      });
      size += part.value.length;
      if (size > maximum) {
        truncated.value = true;
        await reader.cancel();
        break;
      }
    }
    output += decoder.decode();
  } finally {
    reader.releaseLock();
  }
  return output;
}
async function load(verify = false) {
  controller?.abort();
  controller = new AbortController();
  busy.value = true;
  error.value = "";
  record.value = null;
  text.value = "";
  truncated.value = false;
  const active = controller;
  if (!verify) needsPassword.value = false;
  try {
    const slug = encodeURIComponent(route.params.slug);
    const endpoint = isFile.value
      ? `/share/${verify ? "verify" : "get"}/${slug}`
      : `/paste/${slug}`;
    const data = await request(endpoint, {
      public: true,
      method: verify ? "POST" : "GET",
      body: verify ? { password: password.value } : undefined,
      signal: active.signal,
    });
    if (active !== controller) return;
    needsPassword.value = Boolean(
      data.requires_password || data.requiresPassword,
    );
    if (needsPassword.value) return;
    record.value = data;
    // 普通文本文件默认显示原文，只有 Markdown 文件才加载 Markdown 解析器。
    markdown.value =
      !isFile.value || ["md", "markdown"].includes(extension.value);
    if (!isFile.value) text.value = data.content || "";
    else if (kind.value === "text")
      text.value = await readText(url.value, active.signal);
    password.value = "";
  } catch (reason) {
    if (active === controller && reason.name !== "AbortError")
      error.value = reason.message;
  } finally {
    if (active === controller) busy.value = false;
  }
}
async function copy() {
  try {
    await navigator.clipboard.writeText(text.value);
  } catch {
    error.value = "复制失败，请手动选择文本";
  }
}
onMounted(() => load());
watch(
  () => route.fullPath,
  () => load(),
);
onBeforeUnmount(() => controller?.abort());
</script>
<template>
  <section v-if="busy" class="panel empty">正在读取分享…</section>
  <section v-else-if="needsPassword" class="panel login-panel">
    <h1>这个分享需要密码</h1>
    <form @submit.prevent="load(true)">
      <label
        >访问密码<input
          v-model="password"
          type="password"
          required
          autocomplete="off"
      /></label>
      <p v-if="error" class="error">{{ error }}</p>
      <button class="primary">打开分享</button>
    </form>
  </section>
  <template v-else-if="record"
    ><div class="page-heading">
      <p class="eyebrow">{{ isFile ? "文件分享" : "文本分享" }}</p>
      <h1>{{ record.filename || record.title || "一份临时分享" }}</h1>
      <p class="muted">
        {{
          record.expires_at
            ? "有效期至 " + new Date(record.expires_at).toLocaleString()
            : "长期保留"
        }}<span v-if="isFile">
          · {{ (record.size / 1048576).toFixed(2) }} MiB</span
        >
      </p>
    </div>
    <section class="panel">
      <p v-if="error" class="error" role="alert">{{ error }}</p>
      <template v-if="kind === 'text'"
        ><div class="editor-heading">
          <div class="tabs">
            <button :class="{ active: markdown }" @click="markdown = true">
              Markdown</button
            ><button :class="{ active: !markdown }" @click="markdown = false">
              原文
            </button>
          </div>
          <button @click="copy">复制文本</button>
        </div>
        <MarkdownPreview v-if="markdown" :content="text" />
        <pre v-else class="plain-preview">{{ text }}</pre>
        <p v-if="truncated" class="notice">
          仅预览前
          {{ settings.preview_max_text_kb || 512 }} KiB，下载可获取完整文件。
        </p></template
      >
      <img
        v-else-if="kind === 'image'"
        :src="url"
        :alt="record.filename"
        class="image-preview"
        referrerpolicy="no-referrer"
      />
      <video
        v-else-if="kind === 'video'"
        :src="url"
        controls
        playsinline
        preload="none"
        class="video-preview"
      ></video>
      <audio
        v-else-if="kind === 'audio'"
        :src="url"
        controls
        preload="none"
        class="audio-preview"
      ></audio>
      <div v-else class="empty">
        <h2>{{ record.filename }}</h2>
        <p class="muted">此文件不提供在线预览，可以直接下载。</p>
      </div>
      <div v-if="isFile" class="actions">
        <span class="muted small">浏览器原生播放，格式支持取决于浏览器。</span
        ><a
          class="button primary"
          :href="contentURL(record.downloadUrl)"
          referrerpolicy="no-referrer"
          >下载文件</a
        >
      </div>
    </section></template
  >
  <section v-else class="panel empty">
    <h1>分享暂时无法打开</h1>
    <p class="error" role="alert">{{ error }}</p>
    <RouterLink to="/">回到首页</RouterLink>
  </section>
</template>
