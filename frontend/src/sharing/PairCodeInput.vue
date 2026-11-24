<script setup>
import { nextTick, ref, watch } from "vue";
import { normalizePairingCode, validPairingCode } from "./transfer-code.js";
import { useDisabled } from "./ui/context.js";

const props = defineProps({
  modelValue: { type: String, default: "" },
  disabled: Boolean,
});
const emit = defineEmits(["update:modelValue", "complete"]);
const disabled = useDisabled(props);
const LENGTH = 4;
const LAST = LENGTH - 1;
const cells = ref(Array(LENGTH).fill(""));
const inputs = [];

// 保留编辑中的空位，不能因删除中间一位而把后面的数字向前挤。
watch(
  () => props.modelValue,
  (value) => {
    const code = normalizePairingCode(value);
    if (code !== cells.value.join(""))
      cells.value = Array.from(
        { length: LENGTH },
        (_, index) => (/^[0-9]$/.test(code[index]) && code[index]) || "",
      );
  },
  { immediate: true },
);

function update() {
  const code = cells.value.join("");
  emit("update:modelValue", code);
  // 只在用户输入或粘贴完整代码时通知父页面，外部清空不会触发加入。
  if (!disabled.value && validPairingCode(code)) emit("complete");
}
async function focusCell(index) {
  await nextTick();
  inputs[index]?.focus();
  inputs[index]?.select();
}
function input(index, event) {
  const value = normalizePairingCode(event.target.value).slice(-1);
  if (!value || /^[0-9]$/.test(value)) cells.value[index] = value;
  // 无效字符也立即清除，避免原生输入与 Vue 状态不一致。
  event.target.value = cells.value[index];
  update();
  if (/^[0-9]$/.test(value) && index < LAST) focusCell(index + 1);
}
function paste(index, event) {
  event.preventDefault();
  if (disabled.value) return;
  const code = normalizePairingCode(event.clipboardData?.getData("text/plain"));
  if (validPairingCode(code)) {
    // 从任意一格粘贴完整配对码，空白与横杠均自动去掉。
    cells.value = Array.from(code);
    update();
    focusCell(LAST);
  } else if (/^[0-9]+$/.test(code) && code.length <= LENGTH - index) {
    for (let offset = 0; offset < code.length; offset++)
      cells.value[index + offset] = code[offset];
    update();
    focusCell(Math.min(index + code.length, LAST));
  }
}
function keydown(index, event) {
  if (event.key === "ArrowLeft" || event.key === "ArrowRight") {
    event.preventDefault();
    focusCell(
      Math.max(0, Math.min(LAST, index + (event.key === "ArrowLeft" ? -1 : 1))),
    );
  } else if (event.key === "Backspace") {
    event.preventDefault();
    const target = cells.value[index] ? index : Math.max(0, index - 1);
    cells.value[target] = "";
    update();
    focusCell(target);
  }
}
function focus() {
  const empty = cells.value.findIndex((value) => !value);
  focusCell(empty < 0 ? LAST : empty);
}
defineExpose({ focus });
</script>

<template>
  <div class="pair-code" role="group" aria-label="配对码">
    <input
      v-for="(_, index) in cells"
      :key="index"
      :id="index === 0 ? 'transfer-pair-code' : `transfer-pair-code-${index}`"
      :ref="(element) => (inputs[index] = element)"
      :value="cells[index]"
      :class="{ filled: cells[index] }"
      :aria-label="`配对码第${index + 1}位数字`"
      inputmode="numeric"
      pattern="[0-9]"
      :autocomplete="index === 0 ? 'one-time-code' : 'off'"
      :spellcheck="false"
      maxlength="1"
      required
      :disabled="disabled"
      @input="input(index, $event)"
      @paste="paste(index, $event)"
      @keydown="keydown(index, $event)"
      @focus="$event.target.select()"
    />
  </div>
</template>

<style scoped>
/* 四个输入框居中排列，窄屏下按剩余宽度等分，不产生横向滚动。 */
.pair-code {
  display: grid;
  grid-template-columns: repeat(4, minmax(0, 64px));
  justify-content: center;
  gap: 10px;
  min-width: 0;
}
.pair-code input {
  width: 100%;
  min-width: 0;
  height: 60px;
  padding: 0;
  border: 1px solid var(--line);
  border-radius: 12px;
  background: var(--surface-subtle);
  color: inherit;
  text-align: center;
  font:
    600 26px ui-monospace,
    monospace;
  transition:
    border-color var(--motion-fast, 150ms),
    background var(--motion-fast, 150ms),
    transform 0.2s var(--motion-spring, ease-out);
}
.pair-code input:focus {
  border-color: var(--accent);
  outline: none;
  box-shadow: 0 0 0 3px color-mix(in srgb, var(--accent) 18%, transparent);
}
.pair-code input.filled {
  border-color: color-mix(in srgb, var(--accent) 55%, var(--line));
  background: var(--soft);
}
.pair-code input:disabled {
  opacity: 0.6;
}
@media (max-width: 650px) {
  .pair-code {
    grid-template-columns: repeat(4, minmax(0, 60px));
  }
  .pair-code input {
    height: 64px;
  }
}
@media (prefers-reduced-motion: no-preference) {
  .pair-code input.filled {
    animation: pair-pop 0.22s var(--motion-spring, ease-out);
  }
}
@keyframes pair-pop {
  50% {
    transform: scale(1.08);
  }
}
</style>
