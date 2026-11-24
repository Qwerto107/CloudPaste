<script setup>
import Icon from "./Icon.vue";
import { computed, onMounted, ref, watch, onBeforeUnmount } from "vue";
import { useRoute } from "vue-router";
import { request, contentURL } from "./api.js";
import { settings } from "./settings.js";
import { session } from "./session.js";
import { fileDisplay, formatFileSize } from "./file-display.js";
import SharePreviewContent from "./SharePreviewContent.vue";
import {
  readShareAccess,
  saveShareAccess,
  clearShareAccess,
} from "./share-access.js";
const route = useRoute();
const record = ref(null);
const password = ref("");
const needsPassword = ref(false);
const busy = ref(false);
const error = ref("");
const copied = ref(false);
const lockedTitle = ref("");
const unavailableMessage = "分享已失效或不存在";
const text = ref("");
const truncated = ref(false);
const markdown = ref(true);
const selectedFile = ref(null);
const previewLoading = ref(false);
let controller;
const isFile = computed(() => route.meta.fileShare === true);
const isCollection = computed(() => Boolean(record.value?.files?.length));
const shareTitle = computed(
  () => record.value?.filename || record.value?.title || "一份临时分享",
);
const expiresAt = computed(() =>
  record.value?.expires_at ? new Date(record.value.expires_at) : null,
);
const shareExpired = computed(() =>
  Boolean(
    record.value?.expiry_reason ||
    (expiresAt.value && expiresAt.value.getTime() <= Date.now()),
  ),
);
const expiryLabel = computed(() => {
  if (!expiresAt.value) return "长期保留";
  const minutes = Math.ceil((expiresAt.value.getTime() - Date.now()) / 60000);
  if (minutes <= 0) return "已过期";
  if (minutes >= 1440) return `剩余 ${Math.ceil(minutes / 1440)} 天`;
  if (minutes >= 60) return `剩余 ${Math.ceil(minutes / 60)} 小时`;
  return `剩余 ${minutes} 分钟`;
});
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
  copied.value = false;
  record.value = null;
  selectedFile.value = null;
  previewLoading.value = false;
  text.value = "";
  truncated.value = false;
  const active = controller;
  const accessType = isFile.value ? "file" : "text";
  const accessSlug = String(route.params.slug);
  if (!verify) {
    needsPassword.value = false;
    lockedTitle.value = "";
  }
  try {
    const slug = encodeURIComponent(route.params.slug);
    // 管理员查看使用认证入口，普通用户和访客继续使用公开分享协议。
    const admin = session.type === "admin";
    const endpoint = isFile.value
      ? `/share/${!admin && verify ? "verify" : "get"}/${slug}`
      : `/paste/${slug}`;
    let data;
    if (session.type === "apikey" && !verify) {
      try {
        // 创建者读取走认证入口，其他人的分享仍使用公开协议。
        data = await request(`/owner${endpoint}`, { signal: active.signal });
      } catch (reason) {
        if (![403, 404].includes(reason.status)) throw reason;
      }
    }
    data ??= await request(endpoint, {
      public: !admin,
      method: !admin && verify ? "POST" : "GET",
      body: !admin && verify ? { password: password.value } : undefined,
      shareToken:
        !admin && !verify ? readShareAccess(accessType, accessSlug) : "",
      signal: active.signal,
    });
    if (active !== controller) return;
    needsPassword.value = Boolean(
      data.requires_password || data.requiresPassword,
    );
    if (needsPassword.value) {
      lockedTitle.value = data.filename || data.title || "";
      clearShareAccess(accessType, accessSlug);
      return;
    }
    if (!admin && !data.owner_preview)
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
  copied.value = false;
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
    copied.value = true;
  } catch {
    error.value = "复制失败，请手动选择文本";
  }
}
function downloadMarkdown() {
  const url = URL.createObjectURL(
    new Blob([text.value], { type: "text/markdown;charset=utf-8" }),
  );
  const link = document.createElement("a");
  link.href = url;
  link.download = `${shareTitle.value.replace(/[\\/:*?"<>|]/g, "_").replace(/\.md$/i, "") || "分享"}.md`;
  link.click();
  setTimeout(() => URL.revokeObjectURL(url), 1000);
}
onMounted(() => load());
watch(
  () => route.fullPath,
  () => load(),
);
onBeforeUnmount(() => controller?.abort());
</script>
<template>
  <section v-if="busy && !needsPassword" class="panel empty">
    正在读取分享…
  </section>
  <section
    v-else-if="needsPassword"
    class="panel login-panel share-lock"
    :class="{ 'share-lock-error': error }"
  >
    <span class="share-lock-icon" aria-hidden="true"><Icon name="lock" /></span>
    <h1>这个分享需要密码</h1>
    <p class="muted">输入分享者提供的访问密码</p>
    <div v-if="lockedTitle" class="share-lock-meta">
      <strong>{{ lockedTitle }}</strong
      ><span>{{ isFile ? "文件分享" : "文本分享" }}</span>
    </div>
    <form @submit.prevent="!busy && load(true)">
      <UiInput
        v-model="password"
        type="password"
        aria-label="访问密码"
        placeholder="访问密码"
        required
        autocomplete="off"
      />
      <UiButton class="primary" :disabled="busy">{{
        busy ? "正在打开…" : "打开分享"
      }}</UiButton>
      <p v-if="error" class="error" role="alert">{{ error }}</p>
    </form>
  </section>
  <article
    v-else-if="record"
    class="panel share-view-card share-view-mobile-actions"
  >
    <header class="share-view-heading">
      <div class="share-view-title">
        <h1>{{ shareTitle }}</h1>
        <p v-if="isCollection" class="muted">
          文件合集 · {{ record.files.length }} 个文件 ·
          {{ formatFileSize(record.size) }}
        </p>
        <p v-else-if="isFile" class="muted">
          文件分享 · {{ formatFileSize(record.size) }}
        </p>
      </div>
      <div class="share-view-tags">
        <span class="page-tag" :class="shareExpired ? 'failure' : 'success'">{{
          shareExpired ? "已失效" : "有效"
        }}</span>
        <span class="page-tag" :title="expiresAt?.toLocaleString()">{{
          expiryLabel
        }}</span>
        <span
          v-if="record.max_views > 0 && record.views != null"
          class="page-tag"
          >查看 {{ record.views }} / {{ record.max_views }}</span
        >
        <span
          v-if="record.has_password || record.hasPassword"
          class="page-tag accent"
          >密码保护</span
        >
      </div>
      <div v-if="isCollection" class="share-archive-actions">
        <UiButton
          class="primary"
          tag="a"
          icon="download"
          :href="contentURL(record.archiveUrl)"
          referrerpolicy="no-referrer"
          >打包下载 ZIP</UiButton
        >
      </div>
    </header>

    <p
      v-if="record.admin_preview || record.owner_preview"
      class="notice share-view-notice"
      role="status"
    >
      {{ record.admin_preview ? "管理员查看" : "创建者查看"
      }}{{
        record.expiry_reason ? " · 分享已失效：" + record.expiry_reason : ""
      }}，本次查看不消耗分享次数。
    </p>
    <p v-if="error" class="error share-view-notice" role="alert">{{ error }}</p>

    <ul v-if="isCollection" class="collection-files collection-file-list">
      <li
        v-for="file in record.files"
        :key="file.id"
        :class="{ active: selectedFile?.id === file.id }"
      >
        <div class="collection-file-row">
          <span
            class="file-ext"
            :data-kind="fileDisplay(file.filename).kind"
            aria-hidden="true"
            >{{ fileDisplay(file.filename).label }}</span
          >
          <strong>{{ file.filename }}</strong>
          <small class="muted">{{ formatFileSize(file.size) }}</small>
          <UiButton
            type="button"
            size="small"
            :aria-label="
              selectedFile?.id === file.id
                ? '收起预览 ' + file.filename
                : '预览 ' + file.filename
            "
            :aria-expanded="selectedFile?.id === file.id"
            @click="
              selectedFile?.id === file.id ? closePreview() : openFile(file)
            "
            >{{ selectedFile?.id === file.id ? "收起" : "预览" }}</UiButton
          >
          <UiButton
            tag="a"
            size="small"
            :href="contentURL(file.downloadUrl)"
            referrerpolicy="no-referrer"
            :aria-label="'下载 ' + file.filename"
            >下载</UiButton
          >
        </div>
        <div
          v-if="selectedFile?.id === file.id"
          class="collection-inline-preview"
        >
          <p v-if="previewLoading" class="empty">正在读取文件…</p>
          <template v-else>
            <div v-if="kind === 'text'" class="share-preview-toolbar">
              <div class="page-segment" role="group" aria-label="文本预览方式">
                <button
                  type="button"
                  :aria-pressed="markdown"
                  @click="markdown = true"
                >
                  Markdown
                </button>
                <button
                  type="button"
                  :aria-pressed="!markdown"
                  @click="markdown = false"
                >
                  原文
                </button>
              </div>
              <UiButton type="button" size="small" @click="copy">{{
                copied ? "已复制" : "复制全文"
              }}</UiButton>
            </div>
            <SharePreviewContent
              v-model="markdown"
              :kind="kind"
              :text="text"
              :url="url"
              :name="file.filename"
              :truncated="truncated"
            />
          </template>
        </div>
      </li>
    </ul>
    <template v-else>
      <div
        v-if="kind === 'text'"
        class="share-preview-toolbar"
        :class="{ 'share-text-actions': !isFile }"
      >
        <div class="page-segment" role="group" aria-label="文本预览方式">
          <button
            type="button"
            :aria-pressed="markdown"
            @click="markdown = true"
          >
            Markdown
          </button>
          <button
            type="button"
            :aria-pressed="!markdown"
            @click="markdown = false"
          >
            原文
          </button>
        </div>
        <UiButton type="button" size="small" @click="copy">{{
          copied ? "已复制" : "复制全文"
        }}</UiButton>
        <UiButton
          v-if="!isFile"
          type="button"
          size="small"
          @click="downloadMarkdown"
          >下载 .md</UiButton
        >
      </div>
      <SharePreviewContent
        v-model="markdown"
        :kind="kind"
        :text="text"
        :url="url"
        :name="previewRecord.filename"
        :truncated="truncated"
      />
      <div v-if="isFile" class="share-single-download">
        <UiButton
          class="primary"
          tag="a"
          icon="download"
          :href="contentURL(previewRecord.downloadUrl)"
          referrerpolicy="no-referrer"
          >下载文件</UiButton
        >
      </div>
    </template>
  </article>
  <section v-else class="panel empty">
    <h1>分享暂时无法打开</h1>
    <p class="error" role="alert">{{ error }}</p>
    <RouterLink to="/">回到首页</RouterLink>
  </section>
</template>
