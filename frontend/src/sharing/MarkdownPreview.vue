<script setup>
import { computed } from "vue";
import { renderMarkdown } from "./render.js";
import { settings } from "./settings.js";
const props = defineProps({ content: { type: String, default: "" } });
// Markdown 必须先清理 HTML；不执行脚本、事件属性或嵌入外部页面。
const html = computed(() =>
  renderMarkdown(
    props.content,
    settings.preview_allow_external_images === "true",
  ),
);
</script>
<template><div class="markdown-body" v-html="html"></div></template>
