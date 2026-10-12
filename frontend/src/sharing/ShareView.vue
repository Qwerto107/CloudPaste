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
import {
  readShareAccess,
  saveShareAccess,
  clearShareAccess,
} from "./share-access.js";
const MarkdownPreview = defineAsyncComponent(
  () => import("./MarkdownPreview.vue"),
);
const route = useRoute();
const record = ref(null);
const password = ref("");
const needsPassword = ref(false);
const busy = ref(false);
const error = ref("");
const unavailableMessage = "分享已失效或不存在";
const text = ref("");
const truncated = ref(false);
const markdown = ref(true);
const selectedFile = ref(null);
const previewLoading = ref(false);
let controller;
const isFile = computed(() => route.meta.fileShare === true);
const isCollection = computed(() => Boolean(record.value?.files?.length));
const previewRecord = computed(() => selectedFile.value || record.value);
const url = computed(() =>
  previewRecord.value?.contentUrl
    ? contentURL(previewRecord.value.contentUrl)
    : "",
);
const extension = computed(() =>
  (previewRecord.value?.filename || "").split(".").pop().toLowerCase(),
);
function matches(key, fallback) {
  return String(settings[key] ?? fallback)
    .toLowerCase()
    .split(/[\s,]+/)
    .includes(extension.value);
}
const kind = computed(() => {
  if (isCollection.value && !selectedFile.value) return "collection";
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
  if (!response.ok) {
    const reason = new Error("文件读取失败，请稍后重试");
    reason.status = response.status;
    throw reason;
  }
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
function showError(reason) {
  const unavailable = [404, 410].includes(reason.status);
  if ([401, 403, 404, 410].includes(reason.status))
    clearShareAccess(isFile.value ? "file" : "text", String(route.params.slug));
  if (unavailable) {
    // 验证期间分享也可能失效，关闭密码框和旧内容，统一显示失效提示。
    needsPassword.value = false;
    password.value = "";
    record.value = null;
    selectedFile.value = null;
    text.value = "";
  }
  error.value = unavailable ? unavailableMessage : reason.message;
}
async function load(verify = false) {
  controller?.abort();
  controller = new AbortController();
  busy.value = true;
  error.value = "";
  record.value = null;
  selectedFile.value = null;
  previewLoading.value = false;
  text.value = "";
  truncated.value = false;
  const active = controller;
  const accessType = isFile.value ? "file" : "text";
  const accessSlug = String(route.params.slug);
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
      shareToken: verify ? "" : readShareAccess(accessType, accessSlug),
      signal: active.signal,
    });
    if (active !== controller) return;
    needsPassword.value = Boolean(
      data.requires_password || data.requiresPassword,
    );
    if (needsPassword.value) {
      clearShareAccess(accessType, accessSlug);
      return;
    }
    saveShareAccess(accessType, accessSlug, data);
    password.value = "";
    record.value = data;
    // 普通文本文件默认显示原文，只有 Markdown 文件才加载 Markdown 解析器。
    markdown.value =
      !isFile.value || ["md", "markdown"].includes(extension.value);
    if (!isFile.value) text.value = data.content || "";
    else if (kind.value === "text")
      text.value = await readText(url.value, active.signal);
  } catch (reason) {
    if (active === controller && reason.name !== "AbortError") {
      showError(reason);
    }
  } finally {
    if (active === controller) busy.value = false;
  }
}
// 合集中的预览和下载复用同一查看票据，不为每个文件重新消耗查看次数。
async function openFile(file) {
  controller?.abort();
  const active = new AbortController();
  controller = active;
  selectedFile.value = file;
  text.value = "";
  truncated.value = false;
  error.value = "";
  markdown.value = ["md", "markdown"].includes(extension.value);
  previewLoading.value = true;
  try {
    if (kind.value === "text")
      text.value = await readText(url.value, active.signal);
  } catch (reason) {
    if (controller === active && reason.name !== "AbortError")
      showError(reason);
  } finally {
    if (controller === active) previewLoading.value = false;
  }
}
function closePreview() {
  controller?.abort();
  controller = null;
  selectedFile.value = null;
  previewLoading.value = false;
  text.value = "";
  error.value = "";
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
        >访问密码<UiInput
          v-model="password"
          type="password"
          required
          autocomplete="off"
      /></label>
      <p v-if="error" class="error">{{ error }}</p>
      <UiButton class="primary">打开分享</UiButton>
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
      <template v-if="isCollection">
        <div class="collection-toolbar">
          <strong>{{ record.files.length }} 个文件</strong>
          <a
            class="button primary"
            :href="contentURL(record.archiveUrl)"
            referrerpolicy="no-referrer"
            >打包下载 ZIP</a
          >
        </div>
        <ul class="collection-files">
          <li
            v-for="file in record.files"
            :key="file.id"
            :class="{ active: selectedFile?.id === file.id }"
          >
            <UiButton
              type="button"
              class="collection-file-name"
              @click="openFile(file)"
              :aria-label="'预览 ' + file.filename"
            >
              {{ file.filename }}
            </UiButton>
            <small class="muted"
              >{{ (file.size / 1048576).toFixed(2) }} MiB</small
            >
            <a
              class="button"
              :href="contentURL(file.downloadUrl)"
              referrerpolicy="no-referrer"
              :aria-label="'下载 ' + file.filename"
              >下载</a
            >
          </li>
        </ul>
        <div v-if="selectedFile" class="collection-preview-heading">
          <h2>{{ selectedFile.filename }}</h2>
          <UiButton type="button" @click="closePreview">收起预览</UiButton>
        </div>
      </template>
      <div v-if="previewLoading" class="empty">正在读取文件…</div>
      <template v-else-if="!isCollection || selectedFile">
        <template v-if="kind === 'text'"
          ><div class="editor-heading">
            <div class="tabs">
              <UiButton :class="{ active: markdown }" @click="markdown = true">
                Markdown</UiButton
              ><UiButton
                :class="{ active: !markdown }"
                @click="markdown = false"
              >
                原文
              </UiButton>
            </div>
            <UiButton @click="copy">复制文本</UiButton>
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
          :alt="previewRecord.filename"
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
          <h2>{{ previewRecord.filename }}</h2>
          <p class="muted">此文件不提供在线预览，可以直接下载。</p>
        </div>
        <div v-if="isFile" class="actions">
          <span class="muted small">浏览器原生播放，格式支持取决于浏览器。</span
          ><a
            class="button primary"
            :href="contentURL(previewRecord.downloadUrl)"
            referrerpolicy="no-referrer"
            >下载文件</a
          >
        </div>
      </template>
    </section></template
  >
  <section v-else class="panel empty">
    <h1>分享暂时无法打开</h1>
    <p class="error" role="alert">{{ error }}</p>
    <RouterLink to="/">回到首页</RouterLink>
  </section>
</template>
