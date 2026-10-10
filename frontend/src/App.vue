<script setup>
import {
  computed,
  defineAsyncComponent,
  onMounted,
  onBeforeUnmount,
  ref,
  watch,
} from "vue";
import { useRouter } from "vue-router";
import { sanitizeHTML } from "./sharing/sanitize.js";
import { settings } from "./sharing/settings.js";
import { session, clearSession } from "./sharing/session.js";
import { request } from "./sharing/api.js";
import Icon from "./sharing/Icon.vue";
const MarkdownPreview = defineAsyncComponent(
  () => import("./sharing/MarkdownPreview.vue"),
);
const router = useRouter();
const error = ref("");
const announcement = ref(false);
// 首次主题由头部同源脚本提前设置，避免暗色页面刷新时闪白。
const dark = ref(document.documentElement.dataset.theme === "dark");
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
      const url = new URL(value || "/cloudpaste.svg", location.origin);
      if (["https:", "http:"].includes(url.protocol))
        document.querySelector('link[rel="icon"]').href = url.href;
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
  <header class="site-header">
    <RouterLink class="brand" to="/"
      ><span class="brand-icon"><Icon name="cloud" /></span
      ><span class="brand-name">{{
        settings.site_title || "CloudPaste"
      }}</span></RouterLink
    >
    <nav aria-label="主导航">
      <RouterLink v-if="settings.site_home_editor_enabled !== 'false'" to="/"
        ><Icon name="text" />文本分享</RouterLink
      ><RouterLink
        v-if="settings.site_upload_page_enabled !== 'false'"
        to="/upload"
        ><Icon name="folder" />文件分享</RouterLink
      ><RouterLink
        :to="
          session.type === 'none'
            ? '/login'
            : session.type === 'admin'
              ? '/admin'
              : '/manage'
        "
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
        ><span class="account-name">{{ session.name || "用户" }}</span></span
      >
      <button
        v-if="settings.site_announcement_enabled === 'true'"
        class="quiet"
        aria-label="站点公告"
        :aria-expanded="announcement"
        @click="announcement = !announcement"
      >
        <Icon name="bell" /></button
      ><button
        class="quiet theme-toggle"
        :aria-label="dark ? '切换到浅色模式' : '切换到暗色模式'"
        :title="dark ? '切换到浅色模式' : '切换到暗色模式'"
        @click="dark = !dark"
      >
        <Icon :name="dark ? 'sun' : 'moon'" /></button
      ><button
        v-if="session.type !== 'none'"
        class="quiet"
        aria-label="退出"
        title="退出"
        @click="logout"
      >
        <Icon name="logout" />
      </button>
    </div>
  </header>
  <main class="site-main">
    <p v-if="error" class="error" role="alert">
      {{ error }} <button class="quiet" @click="error = ''">关闭</button>
    </p>
    <div class="custom-content" v-html="head"></div>
    <section
      v-if="announcement && settings.site_announcement_enabled === 'true'"
      class="panel"
    >
      <h2>站点公告</h2>
      <MarkdownPreview :content="settings.site_announcement_content || ''" />
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
</template>
