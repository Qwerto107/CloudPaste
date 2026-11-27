<script setup>
import { defineAsyncComponent, ref, watch } from "vue";
import { shareText } from "./share-text.js";
import Icon from "./Icon.vue";

const ShareQRCode = defineAsyncComponent(() => import("./ShareQRCode.vue"));
const props = defineProps({
  link: String,
  password: String,
  title: String,
  detail: String,
});
const copied = ref("");
const showCode = ref(false);
const error = ref("");
let copiedTimer;
watch(
  () => props.link,
  () => {
    clearTimeout(copiedTimer);
    copied.value = "";
    showCode.value = false;
    error.value = "";
  },
);
async function copy(kind) {
  try {
    await navigator.clipboard.writeText(
      kind === "link" ? props.link : shareText(props.link, props.password),
    );
    copied.value = kind;
    error.value = "";
    clearTimeout(copiedTimer);
    copiedTimer = setTimeout(() => {
      if (copied.value === kind) copied.value = "";
    }, 1800);
  } catch {
    error.value = "复制失败，请手动复制";
  }
}
</script>

<template>
  <section class="panel result share-created" aria-label="分享创建结果">
    <header class="share-created-heading">
      <span class="share-created-check"><Icon name="check" /></span>
      <div>
        <h2>{{ title }}</h2>
        <p v-if="detail" class="muted small">{{ detail }}</p>
      </div>
    </header>
    <div class="created-link">
      <UiInput :value="link" readonly aria-label="分享链接" />
      <UiButton
        type="button"
        size="small"
        :icon="copied === 'link' ? 'check' : 'clipboard'"
        :class="{ 'is-copied': copied === 'link' }"
        @click="copy('link')"
        >{{ copied === "link" ? "链接已复制" : "复制链接" }}</UiButton
      >
    </div>
    <UiInput
      v-if="password"
      :value="shareText(link, password)"
      type="textarea"
      :rows="2"
      :autosize="{ minRows: 2, maxRows: 6 }"
      readonly
      aria-label="分享文字"
    />
    <div class="result-actions">
      <UiButton
        type="button"
        size="small"
        :icon="copied === 'text' ? 'check' : 'clipboard'"
        :class="{ 'is-copied': copied === 'text' }"
        @click="copy('text')"
        >{{ copied === "text" ? "分享文字已复制" : "复制分享文字" }}</UiButton
      >
      <UiButton
        type="button"
        size="small"
        :aria-expanded="showCode"
        @click="showCode = !showCode"
        >二维码</UiButton
      >
      <UiButton
        tag="a"
        size="small"
        type="button"
        :href="link"
        target="_blank"
        rel="noopener noreferrer"
        >打开分享</UiButton
      >
    </div>
    <ShareQRCode v-if="showCode" :link="link" />
    <p v-if="error" class="error" role="alert">{{ error }}</p>
  </section>
</template>
