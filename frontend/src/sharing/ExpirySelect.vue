<script setup>
import { computed } from "vue";
import { settings } from "./settings.js";

const hours = defineModel({ type: Number, required: true });
// 与文件上传接口统一使用小时；小数表示分钟，避免两种分享的选项不同。
const options = [
  [5 / 60, "5 分钟"],
  [10 / 60, "10 分钟"],
  [0.5, "30 分钟"],
  [1, "1 小时"],
  [3, "3 小时"],
  [6, "6 小时"],
  [12, "12 小时"],
  [24, "1 天"],
  [72, "3 天"],
  [168, "7 天"],
  [336, "14 天"],
  [720, "30 天"],
  [2160, "90 天"],
  [0, "长期保留"],
];
const defaultHours = computed(() =>
  Number(settings.share_default_expiry_hours ?? 24),
);
const hasDefault = computed(() =>
  options.some(([value]) => value === defaultHours.value),
);
</script>

<template>
  <label
    >有效期<UiSelect v-model.number="hours" aria-label="有效期">
      <option v-if="!hasDefault" :value="defaultHours">
        站点默认（{{ defaultHours }} 小时）
      </option>
      <option v-for="[value, label] in options" :key="value" :value="value">
        {{ label }}
      </option>
    </UiSelect></label
  >
</template>
