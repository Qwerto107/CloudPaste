<script setup>
import { computed } from "vue";
import qrcode from "qrcode-generator";

const props = defineProps({ link: { type: String, required: true } });
const matrix = computed(() => {
  // 分享地址只在本地编码，二维码不包含访问密码。
  const code = qrcode(0, "M");
  try {
    code.addData(props.link);
    code.make();
  } catch {
    return null;
  }
  const size = code.getModuleCount();
  const modules = [];
  for (let y = 0; y < size; y++) {
    for (let x = 0; x < size; x++) {
      if (code.isDark(y, x)) modules.push(`M${x} ${y}h1v1h-1z`);
    }
  }
  return { viewBox: `-4 -4 ${size + 8} ${size + 8}`, path: modules.join("") };
});
</script>

<template>
  <svg
    v-if="matrix"
    class="share-qr"
    :viewBox="matrix.viewBox"
    role="img"
    aria-label="分享链接二维码"
    shape-rendering="crispEdges"
  >
    <path :d="matrix.path" fill="#000" />
  </svg>
  <p v-else class="error" role="alert">链接太长，无法生成二维码。</p>
</template>
