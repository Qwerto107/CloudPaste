<script setup>
import {
  computed,
  defineAsyncComponent,
  onMounted,
  onBeforeUnmount,
  ref,
  watch,
  provide,
} from "vue";
import {
  NConfigProvider,
  NDialogProvider,
  darkTheme,
  zhCN,
  dateZhCN,
} from "naive-ui";
import { styleMountTarget } from "./sharing/ui/context.js";
import { themeKey } from "./sharing/ui/theme.js";
import { useRouter, useRoute } from "vue-router";
import { sanitizeHTML } from "./sharing/sanitize.js";
import { settings } from "./sharing/settings.js";
import { session, clearSession } from "./sharing/session.js";
import { request } from "./sharing/api.js";
import Icon from "./sharing/Icon.vue";
const MarkdownPreview = defineAsyncComponent(
  () => import("./sharing/MarkdownPreview.vue"),
);
const router = useRouter();
const route = useRoute();
const accountTarget = computed(() => {
  if (session.type === "admin") return "/admin";
  if (session.type === "apikey") return "/manage";
  // 分享页的顶部登录入口也保留原路径，包括查询参数和锚点。
  if (route.name === "Home" || route.name === "Upload")
    return { path: "/login", query: { redirect: route.fullPath } };
  return "/login";
});
const error = ref("");
const announcement = ref(false);
// 首次主题由头部同源脚本提前设置，避免暗色页面刷新时闪白。
const dark = ref(document.documentElement.dataset.theme === "dark");
provide(themeKey, dark);
const uiTheme = computed(() => ({
  common: {
    primaryColor: dark.value ? "#87d9b8" : "#126954",
    primaryColorHover: dark.value ? "#a6e7cc" : "#0b5442",
    primaryColorPressed: dark.value ? "#71cba6" : "#084935",
    primaryColorSuppl: dark.value ? "#87d9b8" : "#126954",
    bodyColor: dark.value ? "#101a17" : "#f3f8f6",
    cardColor: dark.value ? "#18251f" : "#ffffff",
    borderColor: dark.value ? "#32473d" : "#dce7e2",
    textColorBase: dark.value ? "#e2eee8" : "#20332e",
    borderRadius: "8px",
    heightMedium: "40px",
    heightSmall: "32px",
    fontFamily: 'system-ui, "Segoe UI", "Microsoft YaHei", sans-serif',
  },
  Button: {
    textColorPrimary: dark.value ? "#102a1f" : "#ffffff",
    textColorHoverPrimary: dark.value ? "#102a1f" : "#ffffff",
  },
}));
function applyTheme() {
  document.documentElement.dataset.theme = dark.value ? "dark" : "light";
  try {
    localStorage.setItem("sharing-theme", dark.value ? "dark" : "light");
  } catch {}
}
watch(dark, applyTheme);
// 站点自定义内容只接受清理后的 HTML，不执行脚本或事件属性。
function clean(value) {
  return sanitizeHTML(
    value || "",
    settings.preview_allow_external_images === "true",
  );
}
const head = computed(() => clean(settings.site_custom_head));
const body = computed(() => clean(settings.site_custom_body));
watch(
  () => settings.site_favicon_url,
  (value) => {
    try {
      const url = value ? new URL(value, location.origin) : null;
      if (url && !["https:", "http:"].includes(url.protocol)) return;
      // 同时更新两种格式，避免浏览器继续选择默认图标；清空设置恢复本站资源。
      for (const icon of document.querySelectorAll("link[data-site-icon]")) {
        icon.dataset.defaultHref ||= icon.getAttribute("href");
        icon.dataset.defaultType ||= icon.type;
        icon.href = url?.href || icon.dataset.defaultHref;
        if (url) icon.removeAttribute("type");
        else icon.type = icon.dataset.defaultType;
      }
    } catch {}
  },
);
function onError(event) {
  error.value = event.detail || "页面加载失败，请刷新重试";
}
onMounted(() => window.addEventListener("sharing-error", onError));
onBeforeUnmount(() => window.removeEventListener("sharing-error", onError));
async function logout() {
  try {
    if (session.type === "admin")
      await request("/admin/logout", { method: "POST" });
    clearSession();
    await router.replace("/");
  } catch (reason) {
    error.value = reason.message;
  }
}
</script>
<template>
  <NConfigProvider
    class="app-theme"
    :theme="dark ? darkTheme : null"
    :theme-overrides="uiTheme"
    :locale="zhCN"
    :date-locale="dateZhCN"
    :style-mount-target="styleMountTarget"
    preflight-style-disabled
  >
    <NDialogProvider>
      <header class="site-header">
        <RouterLink class="brand" to="/"
          ><span class="brand-icon"><Icon name="cloud" /></span
          ><span class="brand-name">{{
            settings.site_title || "CloudPaste"
          }}</span></RouterLink
        >
        <nav aria-label="主导航">
          <RouterLink
            v-if="settings.site_home_editor_enabled !== 'false'"
            to="/"
            ><Icon name="text" />文本分享</RouterLink
          ><RouterLink
            v-if="settings.site_upload_page_enabled !== 'false'"
            to="/upload"
            ><Icon name="folder" />文件分享</RouterLink
          ><RouterLink :to="accountTarget"
            ><Icon name="grid" />{{
              session.type === "none" ? "登录" : "管理"
            }}</RouterLink
          >
        </nav>
        <div class="header-actions">
          <span
            v-if="session.type !== 'none'"
            class="account-chip"
            :title="session.name"
            ><span class="avatar">{{
              (session.name || "U").slice(0, 1).toUpperCase()
            }}</span
            ><span class="account-name">{{
              session.name || "用户"
            }}</span></span
          >
          <UiButton
            v-if="settings.site_announcement_enabled === 'true'"
            class="quiet icon-button"
            icon="bell"
            aria-label="站点公告"
            :aria-expanded="announcement"
            @click="announcement = !announcement"
          >
          </UiButton>
          <UiButton
            class="quiet icon-button theme-toggle"
            :icon="dark ? 'sun' : 'moon'"
            :aria-label="dark ? '切换到浅色模式' : '切换到暗色模式'"
            :title="dark ? '切换到浅色模式' : '切换到暗色模式'"
            @click="dark = !dark"
          >
          </UiButton>
          <UiButton
            v-if="session.type !== 'none'"
            class="quiet icon-button"
            icon="logout"
            aria-label="退出"
            title="退出"
            @click="logout"
          >
          </UiButton>
        </div>
      </header>
      <main class="site-main">
        <p v-if="error" class="error" role="alert">
          {{ error }}
          <UiButton class="quiet" @click="error = ''">关闭</UiButton>
        </p>
        <div class="custom-content" v-html="head"></div>
        <section
          v-if="announcement && settings.site_announcement_enabled === 'true'"
          class="panel"
        >
          <h2>站点公告</h2>
          <MarkdownPreview
            :content="settings.site_announcement_content || ''"
          />
        </section>
        <RouterView />
        <div class="custom-content" v-html="body"></div>
      </main>
      <footer class="site-footer">
        <MarkdownPreview
          v-if="settings.site_footer_markdown"
          :content="settings.site_footer_markdown"
        /><span v-else>CloudPaste · 临时文本与文件分享</span>
      </footer>
    </NDialogProvider>
  </NConfigProvider>
</template>
