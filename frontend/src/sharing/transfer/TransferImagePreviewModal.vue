<script setup>
import { ref, watch } from "vue";
import { NModal } from "naive-ui";

const props = defineProps({
  image: { type: Object, default: null },
});

const emit = defineEmits(["close"]);

const previewViewport = ref(null);
const previewScale = ref(1);
const previewOffset = ref({ x: 0, y: 0 });
const previewPointers = new Map();
let pinchDistance = 0;

function resetPreview() {
  previewScale.value = 1;
  previewOffset.value = { x: 0, y: 0 };
  previewPointers.clear();
  pinchDistance = 0;
}

watch(() => props.image, resetPreview);

function constrainPreview(
  x = previewOffset.value.x,
  y = previewOffset.value.y,
) {
  const viewport = previewViewport.value;
  const image = viewport?.querySelector("img");
  if (!image) return;
  // 拖动范围以放大后的图片边缘为界，避免把整张图片拖出视野。
  const limitX = Math.max(
    0,
    (image.clientWidth * previewScale.value - viewport.clientWidth) / 2,
  );
  const limitY = Math.max(
    0,
    (image.clientHeight * previewScale.value - viewport.clientHeight) / 2,
  );
  previewOffset.value = {
    x: Math.max(-limitX, Math.min(limitX, x)),
    y: Math.max(-limitY, Math.min(limitY, y)),
  };
}

function zoomPreview(scale) {
  previewScale.value = Math.max(0.25, Math.min(8, scale));
  constrainPreview();
}

function previewDistance() {
  const [first, second] = [...previewPointers.values()];
  return second ? Math.hypot(first.x - second.x, first.y - second.y) : 0;
}

function startPreviewDrag(event) {
  if (event.button !== 0) return;
  previewPointers.set(event.pointerId, { x: event.clientX, y: event.clientY });
  event.currentTarget.setPointerCapture(event.pointerId);
  pinchDistance = previewDistance();
}

function movePreview(event) {
  const previous = previewPointers.get(event.pointerId);
  if (!previous) return;
  previewPointers.set(event.pointerId, { x: event.clientX, y: event.clientY });
  if (previewPointers.size === 1) {
    constrainPreview(
      previewOffset.value.x + event.clientX - previous.x,
      previewOffset.value.y + event.clientY - previous.y,
    );
  } else {
    // 双指缩放与鼠标操作共用同一缩放状态，不额外创建图片副本。
    const distance = previewDistance();
    if (pinchDistance > 0)
      zoomPreview((previewScale.value * distance) / pinchDistance);
    pinchDistance = distance;
  }
}

function endPreviewDrag(event) {
  previewPointers.delete(event.pointerId);
  pinchDistance = previewDistance();
}
</script>

<template>
  <NModal
    :show="!!image?.previewURL"
    preset="card"
    title="图片预览"
    class="transfer-image-preview"
    :style="{ width: 'min(920px, calc(100vw - 32px))' }"
    role="dialog"
    aria-modal="true"
    aria-label="图片预览"
    @update:show="
      (value) => {
        if (!value) emit('close');
      }
    "
    @close="emit('close')"
  >
    <!-- 使用普通图片保留浏览器右键保存功能，预览复用已有 Blob，不再下载一份。 -->
    <div class="transfer-image-toolbar">
      <UiButton
        type="button"
        size="small"
        aria-label="缩小图片"
        :disabled="previewScale <= 0.25"
        @click="zoomPreview(previewScale / 1.25)"
        >−</UiButton
      >
      <span aria-live="polite">{{ Math.round(previewScale * 100) }}%</span>
      <UiButton
        type="button"
        size="small"
        aria-label="放大图片"
        :disabled="previewScale >= 8"
        @click="zoomPreview(previewScale * 1.25)"
        >+</UiButton
      >
      <UiButton type="button" size="small" @click="resetPreview"
        >适应窗口</UiButton
      >
    </div>
    <div
      class="transfer-image-full"
      ref="previewViewport"
      @wheel.prevent="
        zoomPreview(previewScale * ($event.deltaY < 0 ? 1.1 : 1 / 1.1))
      "
      @pointerdown="startPreviewDrag"
      @pointermove="movePreview"
      @pointerup="endPreviewDrag"
      @pointercancel="endPreviewDrag"
      @lostpointercapture="endPreviewDrag"
    >
      <img
        v-if="image?.previewURL"
        :src="image.previewURL"
        :alt="image.name"
        :style="{
          transform: `translate(${previewOffset.x}px, ${previewOffset.y}px) scale(${previewScale})`,
        }"
        draggable="false"
      />
    </div>
  </NModal>
</template>
