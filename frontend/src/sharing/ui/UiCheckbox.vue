<script setup>
import { computed, useAttrs } from "vue";
import { NCheckbox, NSwitch } from "naive-ui";
import { useDisabled } from "./context.js";
defineOptions({ inheritAttrs: false });
const props = defineProps({
  modelValue: [Boolean, Array],
  value: [String, Number],
  disabled: Boolean,
  type: String,
});
const emit = defineEmits(["update:modelValue"]);
const attrs = useAttrs();
const disabled = useDisabled(props);
const checked = computed(() =>
  Array.isArray(props.modelValue)
    ? props.modelValue.includes(props.value)
    : Boolean(props.modelValue),
);
const nativeProps = computed(() => {
  const { class: unusedClass, style: unusedStyle, ...native } = attrs;
  return native;
});
function update(event) {
  const enabled = event.target.checked;
  // 保留原生布尔值和数组复选框语义，点击外层 label 与键盘操作都有效。
  emit(
    "update:modelValue",
    Array.isArray(props.modelValue)
      ? enabled
        ? [...props.modelValue, props.value]
        : props.modelValue.filter((item) => item !== props.value)
      : enabled,
  );
}
</script>
<template>
  <span class="ui-checkbox" :class="attrs.class"
    ><NSwitch
      v-if="attrs.role === 'switch'"
      :value="checked"
      :disabled="disabled"
      tabindex="-1"
      aria-hidden="true" /><NCheckbox
      v-else
      :checked="checked"
      :disabled="disabled"
      :focusable="false"
      aria-hidden="true" /><input
      v-bind="nativeProps"
      class="ui-checkbox-native"
      type="checkbox"
      :checked="checked"
      :disabled="disabled"
      :value="value"
      @change="update"
  /></span>
</template>
