<script setup>
import { useId } from "vue";
import { MdPreview, config } from "md-editor-v3";
import "md-editor-v3/lib/preview.css";
import { sanitizeHTML } from "./sanitize.js";
import { useTheme } from "./ui/theme.js";
import { settings } from "./settings.js";
const props = defineProps({ content: { type: String, default: "" } });
const dark = useTheme();
const previewId = `markdown-preview-${useId()}`;
config({ markdownItConfig: (md) => md.set({ html: true, breaks: true }) });
// 新预览组件仍清理最终 HTML，不执行脚本、事件属性或嵌入外部页面。
function clean(html) {
  return sanitizeHTML(html, settings.preview_allow_external_images === "true");
}
</script>
<template>
  <MdPreview
    :id="previewId"
    class="markdown-body"
    :model-value="content"
    :theme="dark ? 'dark' : 'light'"
    language="zh-CN"
    preview-theme="github"
    :sanitize="clean"
    no-mermaid
    no-katex
    no-echarts
    no-highlight
    no-img-zoom-in
  />
</template>
