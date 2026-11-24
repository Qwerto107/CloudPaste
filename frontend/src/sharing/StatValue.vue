<script setup>
import { onBeforeUnmount, ref, watch } from "vue";

const props = defineProps({
  value: { type: [Number, String], required: true },
});
const displayed = ref(props.value);
let frame;
watch(
  () => props.value,
  (value) => {
    cancelAnimationFrame(frame);
    if (
      typeof value !== "number" ||
      !window.matchMedia("(prefers-reduced-motion: no-preference)").matches
    ) {
      displayed.value = value;
      return;
    }
    const start = performance.now();
    const from = Number(displayed.value) || 0;
    function tick(now) {
      const progress = Math.min(1, (now - start) / 480);
      displayed.value = Math.round(
        from + (value - from) * (1 - (1 - progress) ** 3),
      );
      if (progress < 1) frame = requestAnimationFrame(tick);
    }
    frame = requestAnimationFrame(tick);
  },
);
onBeforeUnmount(() => cancelAnimationFrame(frame));
</script>

<template>
  <strong
    class="stat-value"
    :data-zero="value === 0 || value === '0' ? '' : null"
    ><span class="visually-hidden">{{ value }}</span
    ><span aria-hidden="true">{{ displayed }}</span></strong
  >
</template>
