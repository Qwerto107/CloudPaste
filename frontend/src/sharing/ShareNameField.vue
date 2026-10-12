<script setup>
defineProps({
  modelValue: { type: String, default: "" },
  type: { type: String, default: "paste" },
  id: { type: String, required: true },
  disabled: Boolean,
});
defineEmits(["update:modelValue"]);
// 与服务端允许的链接名称一致；HTML pattern 的字符集需要转义连字符。
const pattern = "[A-Za-z0-9_\\-]{1,100}";
</script>
<template>
  <div class="share-name-field">
    <label :for="id"
      >自定义链接名称 <span class="muted small">可选</span></label
    >
    <div class="share-name-input">
      <span aria-hidden="true">/{{ type }}/</span>
      <UiInput
        :id="id"
        :model-value="modelValue"
        @update:model-value="$emit('update:modelValue', $event)"
        :disabled="disabled"
        :pattern="pattern"
        maxlength="100"
        placeholder="留空自动生成"
        autocomplete="off"
        autocapitalize="none"
        spellcheck="false"
        :aria-describedby="id + '-help'"
        title="使用 1–100 个英文字母、数字、短横线或下划线"
      />
    </div>
    <p class="muted small" :id="id + '-help'">
      支持英文字母、数字、短横线和下划线，最多 100 个字符。
    </p>
  </div>
</template>
