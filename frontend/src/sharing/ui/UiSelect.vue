<script setup>
import { useAttrs, useSlots } from "vue";
import { NSelect } from "naive-ui";
import { useDisabled } from "./context.js";
const props = defineProps({
  modelValue: [String, Number],
  disabled: Boolean,
  modelModifiers: { type: Object, default: () => ({}) },
});
const emit = defineEmits(["update:modelValue"]);
const attrs = useAttrs();
const slots = useSlots();
const disabled = useDisabled(props);
function text(node) {
  if (typeof node === "string" || typeof node === "number") return String(node);
  if (Array.isArray(node)) return node.map(text).join("");
  return text(node?.children || "");
}
// 沿用页面中可读的 option 列表，不另维护一套选择项数据。
function options() {
  const result = [];
  function visit(nodes) {
    for (const node of nodes) {
      if (node.type === "option")
        result.push({
          label: text(node.children).trim(),
          value: node.props?.value ?? text(node.children),
          disabled:
            node.props?.disabled !== undefined && node.props.disabled !== false,
        });
      else if (Array.isArray(node.children)) visit(node.children);
    }
  }
  visit(slots.default?.() || []);
  return result;
}
function update(value) {
  emit(
    "update:modelValue",
    props.modelModifiers.number ? Number(value) : value,
  );
}
</script>
<template>
  <NSelect
    :value="modelValue === '' ? null : modelValue"
    :options="options()"
    :disabled="disabled"
    :placeholder="options().find((item) => item.disabled)?.label || '请选择'"
    :input-props="{ 'aria-label': attrs['aria-label'] }"
    @update:value="update"
  />
</template>
