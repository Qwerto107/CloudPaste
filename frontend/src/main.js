import { createApp } from "vue";
import App from "./App.vue";
import router from "./router/index.js";
import "./sharing/style.css";
import { cleanupLegacyCaches } from "./sharing/cleanup.js";
cleanupLegacyCaches().catch(() => {});
const app = createApp(App);
app.config.errorHandler = (error) =>
  window.dispatchEvent(
    new CustomEvent("sharing-error", { detail: error.message }),
  );
app.use(router).mount("#app");
