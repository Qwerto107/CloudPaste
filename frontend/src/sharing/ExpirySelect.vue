<script setup>
import { computed, ref, watch } from "vue";

const hours = defineModel({ type: Number, required: true });
// 与文件上传接口统一使用小时；小数表示分钟，服务端上限为 876000 小时。
const MAX_HOURS = 876000;
const CUSTOM = -1;
const units = [
  [1 / 60, "分钟"],
  [1, "小时"],
  [24, "天"],
  [168, "周"],
  [720, "月"],
  [8760, "年"],
];
// 快捷项以外的站点默认时长自动进入自定义，保留原有时长。
const quick = [
  [1, "1 小时"],
  [24, "1 天"],
  [168, "7 天"],
  [720, "30 天"],
  [0, "长期"],
];
const isPreset = (value) =>
  quick.some(([preset]) => Math.abs(preset - value) < 1e-9);
// 不在预设内的值（如站点默认 2 小时）直接显示为自定义，换算成最合适的单位。
function split(value) {
  for (const [size] of [...units].reverse())
    if (
      value >= size &&
      Math.abs(value / size - Math.round(value / size)) < 1e-9
    )
      return { amount: Math.round(value / size), unit: size };
  return { amount: Math.max(1, Math.round(value * 60)), unit: 1 / 60 };
}
const choice = ref(isPreset(hours.value) ? hours.value : CUSTOM);
const amount = ref(split(hours.value || 24).amount);
const unit = ref(split(hours.value || 24).unit);
const customHours = computed(() => Number(amount.value) * unit.value);
const customError = computed(() => {
  if (choice.value !== CUSTOM) return "";
  if (!Number.isFinite(customHours.value) || customHours.value <= 0)
    return "请输入大于 0 的时长";
  if (customHours.value < 1 / 60 - 1e-9) return "最短 1 分钟";
  if (customHours.value > MAX_HOURS) return "最长 100 年";
  return "";
});
watch([choice, customHours], () => {
  if (choice.value !== CUSTOM) hours.value = choice.value;
  // 自定义值无效时写入 NaN，页面据此禁用提交，避免沿用上一次的有效期。
  else hours.value = customError.value ? NaN : customHours.value;
});
// 外部重置（如提交后恢复默认）时同步控件状态。
watch(hours, (value) => {
  if (
    choice.value === CUSTOM &&
    (Number.isNaN(value) || Math.abs(value - customHours.value) < 1e-9)
  )
    return;
  if (isPreset(value)) choice.value = value;
  else {
    choice.value = CUSTOM;
    ({ amount: amount.value, unit: unit.value } = split(value));
  }
});
function pick(value) {
  choice.value = value;
}
const expiresAt = computed(() => {
  if (customError.value || !hours.value) return "";
  // 仅作提示，按页面渲染时间估算。
  return new Date(Date.now() + hours.value * 3600000).toLocaleString("zh-CN", {
    year: "numeric",
    month: "2-digit",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit",
  });
});
</script>

<template>
  <div class="expiry-select" role="group" aria-label="有效期">
    <span>有效期</span>
    <div class="expiry-quick" role="group" aria-label="常用有效期">
      <button
        v-for="[value, text] in quick"
        :key="value"
        type="button"
        class="expiry-chip"
        :class="{ active: choice === value }"
        :aria-pressed="choice === value"
        @click="pick(value)"
      >
        {{ text }}
      </button>
      <button
        type="button"
        class="expiry-chip"
        :class="{ active: choice === CUSTOM }"
        :aria-pressed="choice === CUSTOM"
        @click="pick(CUSTOM)"
      >
        自定义
      </button>
    </div>
    <div v-if="choice === CUSTOM" class="expiry-custom">
      <UiInput
        v-model.number="amount"
        type="number"
        size="small"
        min="1"
        step="1"
        aria-label="自定义有效期数值"
      />
      <UiSelect
        v-model.number="unit"
        size="small"
        aria-label="自定义有效期单位"
      >
        <option v-for="[size, text] in units" :key="text" :value="size">
          {{ text }}
        </option>
      </UiSelect>
    </div>
    <small v-if="customError" class="expiry-error" role="alert">{{
      customError
    }}</small>
    <small v-else class="muted">{{
      expiresAt ? `将于 ${expiresAt} 失效` : "不会按时间失效"
    }}</small>
  </div>
</template>
