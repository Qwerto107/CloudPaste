<script setup>
import { computed, provide, useId, useTemplateRef, watchPostEffect } from "vue";
import { useMobile } from "./useMobile.js";
import { selectMountKey } from "./context.js";

defineProps({ title: String, mobileOnly: Boolean });
const open = defineModel({ type: Boolean, default: false });
const mobile = useMobile();
const sheet = useTemplateRef("sheet");
const heading = useId();

// 原生 dialog 位于顶层，下拉菜单也必须在同一对话框内渲染。
provide(
  selectMountKey,
  computed(() => (mobile.value ? sheet.value : undefined)),
);
watchPostEffect(() => {
  const element = sheet.value;
  if (!(element instanceof HTMLDialogElement)) return;
  if (open.value && !element.open) element.showModal();
  if (!open.value && element.open) element.close();
});
</script>

<template>
  <component
    :is="mobile ? 'dialog' : 'div'"
    v-if="mobile || !mobileOnly"
    ref="sheet"
    :class="{ 'mobile-sheet': mobile }"
    :aria-labelledby="mobile ? heading : undefined"
    @cancel.prevent="open = false"
    @click.self="mobile && (open = false)"
  >
    <div v-if="mobile" class="mobile-sheet-heading">
      <strong :id="heading" tabindex="-1" autofocus>{{ title }}</strong>
      <UiButton
        type="button"
        class="quiet icon-button"
        icon="close"
        aria-label="关闭"
        @click="open = false"
      />
    </div>
    <div class="mobile-sheet-content"><slot /></div>
    <div v-if="mobile && $slots.footer" class="mobile-sheet-footer">
      <slot name="footer" />
    </div>
  </component>
</template>
