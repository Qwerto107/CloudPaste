<script setup>
import { computed, useAttrs } from "vue";
import { NButton } from "naive-ui";
import { useDisabled } from "./context.js";
import Icon from "../Icon.vue";
const props = defineProps({
  type: { type: String, default: "submit" },
  disabled: Boolean,
  icon: String,
  size: { type: String, default: "medium" },
});
const attrs = useAttrs();
const disabled = useDisabled(props);
// 保留原生按钮的提交语义，同时将现有操作等级映射到组件库。
const variant = computed(() =>
  String(attrs.class || "").includes("danger")
    ? "error"
    : String(attrs.class || "").includes("primary")
      ? "primary"
      : "default",
);
const quiet = computed(() => String(attrs.class || "").includes("quiet"));
</script>
<template>
  <NButton
    class="ui-button"
    :type="variant"
    :attr-type="type"
    :disabled="disabled"
    :quaternary="quiet"
    :size="size"
  >
    <!-- 图标使用组件库的专用插槽，保持与文字的间距和垂直居中。 -->
    <template v-if="icon" #icon><Icon :name="icon" /></template>
    <slot />
  </NButton>
</template>
