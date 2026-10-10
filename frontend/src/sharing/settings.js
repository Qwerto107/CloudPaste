import { reactive } from "vue";
import { request } from "./api.js";

export const settings = reactive({
  site_title: "CloudPaste",
  site_home_editor_enabled: "true",
  site_upload_page_enabled: "true",
  share_default_expiry_hours: "24",
  preview_max_text_kb: "512",
  max_upload_size: "100",
});
let pending;
export function loadSettings(force = false) {
  if (!pending || force)
    pending = request("/public/settings", { public: true })
      .then((groups) => {
        for (const items of Object.values(groups || {}))
          for (const item of items) settings[item.key] = item.value;
        return settings;
      })
      .catch(() => settings);
  return pending;
}
