import { createApp, defineAsyncComponent } from "vue";
import App from "./App.vue";
import router from "./router/index.js";
import "./sharing/style.css";
import { cleanupLegacyCaches } from "./sharing/cleanup.js";
import UiButton from "./sharing/ui/UiButton.vue";
import UiInput from "./sharing/ui/UiInput.vue";
import UiSelect from "./sharing/ui/UiSelect.vue";
import UiCheckbox from "./sharing/ui/UiCheckbox.vue";
import UiFieldset from "./sharing/ui/UiFieldset.vue";
cleanupLegacyCaches().catch(() => {});
const app = createApp(App);
// 只注册实际使用的控件；日期选择器和 Markdown 编辑器在对应页面按需加载。
for (const [name, component] of Object.entries({
  UiButton,
  UiInput,
  UiSelect,
  UiCheckbox,
  UiFieldset,
}))
  app.component(name, component);
app.component(
  "UiDateTime",
  defineAsyncComponent(() => import("./sharing/ui/UiDateTime.vue")),
);
app.config.errorHandler = (error) =>
  window.dispatchEvent(
    new CustomEvent("sharing-error", { detail: error.message }),
  );
app.use(router).mount("#app");
