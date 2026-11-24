import { onBeforeUnmount, ref } from "vue";

export function useMobile() {
  const media = window.matchMedia("(max-width: 650px)");
  const mobile = ref(media.matches);
  const update = (event) => (mobile.value = event.matches);
  media.addEventListener("change", update);
  onBeforeUnmount(() => media.removeEventListener("change", update));
  return mobile;
}
