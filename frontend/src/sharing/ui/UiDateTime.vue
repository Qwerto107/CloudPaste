<script setup>
import { computed, useAttrs } from "vue";
import { NDatePicker } from "naive-ui";
import { useDisabled } from "./context.js";
defineOptions({ inheritAttrs: false });
const props = defineProps({
  modelValue: String,
  disabled: Boolean,
  type: String,
});
const emit = defineEmits(["update:modelValue"]);
const attrs = useAttrs();
const disabled = useDisabled(props);
const nativeProps = computed(() => {
  const { class: unusedClass, style: unusedStyle, ...native } = attrs;
  return native;
});
const timestamp = computed(() => {
  const value = props.modelValue ? Date.parse(props.modelValue) : NaN;
  return Number.isNaN(value) ? null : value;
});
// 选择器使用浏览器本地时间；持久化与接口继续使用 UTC ISO 时间。
function update(value) {
  emit(
    "update:modelValue",
    value == null ? null : new Date(value).toISOString(),
  );
}
</script>
<template>
  <NDatePicker
    :class="attrs.class"
    :style="attrs.style"
    type="datetime"
    :value="timestamp"
    format="yyyy-MM-dd HH:mm"
    clearable
    :disabled="disabled"
    placeholder="选择日期和时间，留空不过期"
    :input-props="nativeProps"
    @update:value="update"
  />
</template>
