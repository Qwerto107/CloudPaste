<script setup>
defineProps({
  createdKey: { type: String, required: true },
  visible: { type: Boolean, default: false },
});

const emit = defineEmits(["close", "toggle-visible", "copy"]);
</script>

<template>
  <section class="panel result management-editor new-key-panel">
    <div class="editor-heading">
      <h2>保存新密钥</h2>
      <UiButton class="quiet" size="small" @click="emit('close')"
        >关闭</UiButton
      >
    </div>
    <div class="management-panel-body">
      <p class="muted small">请妥善保存密钥，也可在下方列表中切换显示。</p>
      <div class="copy-row">
        <UiInput
          :value="createdKey"
          :type="visible ? 'text' : 'password'"
          aria-label="新密钥"
          readonly
        />
        <UiButton :aria-pressed="visible" @click="emit('toggle-visible')">
          <span v-if="visible">隐藏密钥</span>
          <span v-else>显示密钥</span>
        </UiButton>
        <UiButton class="primary" @click="emit('copy')">复制</UiButton>
      </div>
    </div>
  </section>
</template>
