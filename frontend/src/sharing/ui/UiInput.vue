<script setup>
import { computed, useAttrs } from "vue";
import { NInput } from "naive-ui";
import { useDisabled } from "./context.js";
defineOptions({ inheritAttrs: false });
const props = defineProps({
  modelValue: [String, Number],
  value: [String, Number],
  type: { type: String, default: "text" },
  disabled: Boolean,
  modelModifiers: { type: Object, default: () => ({}) },
});
const emit = defineEmits(["update:modelValue"]);
const attrs = useAttrs();
const disabled = useDisabled(props);
const nativeProps = computed(() => {
  const { class: unusedClass, style: unusedStyle, ...native } = attrs;
  // required、pattern、自动填充和无障碍属性仍落到真实输入元素上。
  if (!["text", "password", "textarea"].includes(props.type))
    native.type = props.type;
  return native;
});
const text = computed(() => String(props.modelValue ?? props.value ?? ""));
function update(value) {
  const parsed =
    props.modelModifiers.number && value !== "" ? Number(value) : value;
  emit("update:modelValue", parsed);
}
</script>
<template>
  <NInput
    class="ui-input"
    :class="attrs.class"
    :style="attrs.style"
    :value="text"
    :type="type === 'textarea' || type === 'password' ? type : 'text'"
    :disabled="disabled"
    :readonly="attrs.readonly !== undefined && attrs.readonly !== false"
    :placeholder="attrs.placeholder || ''"
    :maxlength="attrs.maxlength == null ? undefined : Number(attrs.maxlength)"
    :minlength="attrs.minlength == null ? undefined : Number(attrs.minlength)"
    :rows="attrs.rows == null ? undefined : Number(attrs.rows)"
    :input-props="nativeProps"
    :show-password-on="type === 'password' ? 'click' : undefined"
    @update:value="update"
  />
</template>
