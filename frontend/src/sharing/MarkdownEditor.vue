<script setup>
import { useId } from "vue";
import { MdEditor, config } from "md-editor-v3";
import { EditorView } from "@codemirror/view";
import screenfull from "screenfull";
import "md-editor-v3/lib/style.css";
import { sanitizeHTML } from "./sanitize.js";
import { settings } from "./settings.js";
import { useDisabled, styleNonce } from "./ui/context.js";
import { useTheme } from "./ui/theme.js";
const props = defineProps({
  modelValue: { type: String, default: "" },
  disabled: Boolean,
});
const emit = defineEmits(["update:modelValue"]);
const disabled = useDisabled(props);
const dark = useTheme();
// 公告、页脚和编辑区可能同时存在，为每个实例提供独立标识。
const editorId = `markdown-editor-${useId()}`;
// 编辑器只启用基础 Markdown；所有预览 HTML 继续经过本站的安全过滤。
config({
  // 全屏功能随前端一起打包，不在运行时从第三方 CDN 加载脚本。
  editorExtensions: { screenfull: { instance: screenfull } },
  codeMirrorExtensions: (extensions) => [
    ...extensions,
    { type: "pageStyleNonce", extension: EditorView.cspNonce.of(styleNonce) },
    {
      type: "contentLabel",
      extension: EditorView.contentAttributes.of({
        "aria-label": "Markdown 内容",
      }),
    },
  ],
  markdownItConfig: (md) => md.set({ html: true, breaks: true }),
});
const toolbar = [
  "bold",
  "italic",
  "strikeThrough",
  "title",
  "quote",
  "unorderedList",
  "orderedList",
  "codeRow",
  "code",
  "link",
  "table",
  "-",
  "revoke",
  "next",
  "=",
  "preview",
  "fullscreen",
];
function clean(html) {
  return sanitizeHTML(html, settings.preview_allow_external_images === "true");
}
</script>
<template>
  <MdEditor
    :id="editorId"
    class="sharing-editor"
    :model-value="modelValue"
    :theme="dark ? 'dark' : 'light'"
    language="zh-CN"
    preview-theme="github"
    :preview="false"
    :disabled="disabled"
    :read-only="disabled"
    :toolbars="toolbar"
    :footers="['markdownTotal']"
    :sanitize="clean"
    no-mermaid
    no-katex
    no-echarts
    no-highlight
    no-prettier
    no-upload-img
    no-img-zoom-in
    @update:model-value="emit('update:modelValue', $event)"
  />
</template>
