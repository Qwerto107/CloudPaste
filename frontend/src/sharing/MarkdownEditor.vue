<script setup>
import { ref, useId } from "vue";
import { MdEditor, config } from "md-editor-v3";
import { EditorView } from "@codemirror/view";
import screenfull from "screenfull";
import "md-editor-v3/lib/style.css";
import { sanitizeHTML } from "./sanitize.js";
import { settings } from "./settings.js";
import { useDisabled, styleNonce } from "./ui/context.js";
import { useTheme } from "./ui/theme.js";
import { embeddedImage } from "./images.js";
const props = defineProps({
  modelValue: { type: String, default: "" },
  disabled: Boolean,
});
const emit = defineEmits(["update:modelValue"]);
const disabled = useDisabled(props);
const dark = useTheme();
const imageError = ref("");
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
  "image",
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
async function insertImages(files, callback) {
  imageError.value = "";
  if (disabled.value) return;
  try {
    // 图片直接嵌入 Markdown，跟随文本密码保护，不产生孤立的公开图片文件。
    const images = [];
    let size = new TextEncoder().encode(props.modelValue).length;
    for (const file of files) {
      size += Math.ceil(file.size / 3) * 4 + 64;
      if (size > 1900 * 1024)
        throw new Error("插入后文本过大，请减少图片或压缩图片");
      images.push(await embeddedImage(file));
    }
    if (!disabled.value) callback(images);
  } catch (reason) {
    imageError.value = reason.message;
  }
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
    @on-upload-img="insertImages"
    @update:model-value="emit('update:modelValue', $event)"
  />
  <p class="muted small">
    支持粘贴图片或通过工具栏插入图片链接；嵌入图片每张最多 1
    MiB。站外图片需启用站外图片预览。
  </p>
  <p v-if="imageError" class="error" role="alert">{{ imageError }}</p>
</template>
