<script setup>
import { defineAsyncComponent } from "vue";
import { settings } from "./settings.js";

const MarkdownPreview = defineAsyncComponent(
  () => import("./MarkdownPreview.vue"),
);
defineProps({
  kind: String,
  text: String,
  url: String,
  name: String,
  truncated: Boolean,
});
const markdown = defineModel({ type: Boolean, required: true });
</script>

<template>
  <div class="share-preview-content">
    <template v-if="kind === 'text'">
      <MarkdownPreview v-if="markdown" :content="text" />
      <pre v-else class="plain-preview">{{ text }}</pre>
      <p v-if="truncated" class="notice">
        仅预览前
        {{ settings.preview_max_text_kb || 512 }} KiB，下载可获取完整文件。
      </p>
    </template>
    <img
      v-else-if="kind === 'image'"
      :src="url"
      :alt="name"
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
      <h2>{{ name }}</h2>
      <p class="muted">此文件不提供在线预览，可以直接下载。</p>
    </div>
  </div>
</template>
