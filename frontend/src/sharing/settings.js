import { reactive } from "vue";
import { request } from "./api.js";

export const settings = reactive({
  site_title: "CloudPaste",
  site_home_editor_enabled: "true",
  site_upload_page_enabled: "true",
  site_github_enabled: "true",
  site_github_url: "https://github.com/Qwerto107/CloudPaste",
  site_announcement_default_open: "false",
  share_default_expiry_hours: "24",
  preview_max_text_kb: "512",
  max_upload_size: "10240",
  transfer_debug_logs: "false",
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
