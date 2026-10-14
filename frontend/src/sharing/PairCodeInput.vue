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
const cells = ref(Array(5).fill(""));
const inputs = [];

// 保留编辑中的空格位，不能因删除中间一位而把后面的数字向前挤。
watch(
  () => props.modelValue,
  (value) => {
    const code = normalizePairingCode(value);
    if (code !== cells.value.join(""))
      cells.value = Array.from({ length: 5 }, (_, index) => code[index] || "");
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
  const value = normalizePairingCode(event.target.value);
  const pattern = index === 0 ? /^[A-Z]$/ : /^[0-9]$/;
  if (!value || pattern.test(value)) cells.value[index] = value;
  // 无效字符也立即清除，避免原生输入与 Vue 状态不一致。
  event.target.value = cells.value[index];
  update();
  if (value && pattern.test(value) && index < 4) focusCell(index + 1);
}
function paste(index, event) {
  event.preventDefault();
  if (disabled.value) return;
  const code = normalizePairingCode(event.clipboardData?.getData("text/plain"));
  if (validPairingCode(code)) {
    // 从任意一格粘贴完整配对码，横杠、空白与大小写均自动解析。
    cells.value = Array.from(code);
    update();
    focusCell(4);
  } else if (index > 0 && /^[0-9]+$/.test(code) && code.length <= 5 - index) {
    for (let offset = 0; offset < code.length; offset++)
      cells.value[index + offset] = code[offset];
    update();
    focusCell(Math.min(index + code.length, 4));
  }
}
function keydown(index, event) {
  if (event.key === "ArrowLeft" || event.key === "ArrowRight") {
    event.preventDefault();
    focusCell(
      Math.max(0, Math.min(4, index + (event.key === "ArrowLeft" ? -1 : 1))),
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
  focusCell(empty < 0 ? 4 : empty);
}
defineExpose({ focus });
</script>

<template>
  <div class="pair-code" role="group" aria-label="配对码">
    <template v-for="(_, index) in cells" :key="index">
      <span v-if="index === 1" class="pair-code-separator" aria-hidden="true"
        >-</span
      >
      <input
        :id="index === 0 ? 'transfer-pair-code' : `transfer-pair-code-${index}`"
        :ref="(element) => (inputs[index] = element)"
        :value="cells[index]"
        :aria-label="index === 0 ? '配对码字母' : `配对码第${index}位数字`"
        :inputmode="index === 0 ? 'text' : 'numeric'"
        :pattern="index === 0 ? '[A-Z]' : '[0-9]'"
        :autocomplete="index === 0 ? 'one-time-code' : 'off'"
        autocapitalize="characters"
        :spellcheck="false"
        maxlength="1"
        required
        :disabled="disabled"
        @input="input(index, $event)"
        @paste="paste(index, $event)"
        @keydown="keydown(index, $event)"
        @focus="$event.target.select()"
      />
    </template>
  </div>
</template>

<style scoped>
/* 五个真实输入框共用固定横杠，窄屏下按剩余宽度等分，不产生横向滚动。 */
.pair-code {
  display: grid;
  grid-template-columns: minmax(0, 1fr) 10px repeat(4, minmax(0, 1fr));
  align-items: center;
  gap: 5px;
  min-width: 0;
}
.pair-code input {
  width: 100%;
  min-width: 0;
  height: 44px;
  padding: 0;
  border: 1px solid var(--line);
  border-radius: 8px;
  background: var(--surface-subtle);
  color: inherit;
  text-align: center;
  font:
    600 20px ui-monospace,
    monospace;
}
.pair-code input:focus {
  border-color: var(--accent);
}
.pair-code input:disabled {
  opacity: 0.6;
}
.pair-code-separator {
  text-align: center;
  color: var(--muted);
  font-size: 20px;
}
</style>
