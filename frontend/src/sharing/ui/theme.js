import { inject, ref } from "vue";
export const themeKey = Symbol("界面主题");
export function useTheme() {
  return inject(
    themeKey,
    ref(document.documentElement.dataset.theme === "dark"),
  );
}
