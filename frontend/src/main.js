import { createApp, defineAsyncComponent } from "vue";
import App from "./App.vue";
import router from "./router/index.js";
import "./sharing/style.css";
import "./sharing/theme-vivid.css";
// 设计稿对齐层：最后加载，统一控件尺寸、字号与各页面布局。
import "./sharing/theme-design.css";
import { cleanupLegacyCaches } from "./sharing/cleanup.js";
import UiButton from "./sharing/ui/UiButton.vue";
import UiInput from "./sharing/ui/UiInput.vue";
import UiSelect from "./sharing/ui/UiSelect.vue";
import UiCheckbox from "./sharing/ui/UiCheckbox.vue";
import UiFieldset from "./sharing/ui/UiFieldset.vue";
// 控制台版本标识：CloudPaste v版本_提交号，下方为版权行。
console.log(
  `%c CloudPaste %c v${__APP_VERSION__}_${__APP_COMMIT__} %c\n\nCopyright © 2026 Qwerto107. All rights reserved.`,
  "background:#2563eb;color:#fff;padding:2px 6px;border-radius:3px 0 0 3px;font-weight:600",
  "background:#dbeafe;color:#1d4ed8;padding:2px 6px;border-radius:0 3px 3px 0",
  "color:#2563eb",
);
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
