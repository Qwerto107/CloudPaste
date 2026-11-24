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
  NNotificationProvider,
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
  if (["Home", "Upload", "Transfer"].includes(route.name))
    return { path: "/login", query: { redirect: route.fullPath } };
  return "/login";
});
const error = ref("");
const announcement = ref(false);
const announcementEnabled = computed(
  () =>
    settings.site_announcement_enabled === "true" &&
    Boolean(settings.site_announcement_content?.trim()),
);
// 新页面使用站点设置的默认状态；页面内切换路由不会重新展开访客已收起的公告。
watch(
  () => [
    settings.site_announcement_enabled,
    settings.site_announcement_default_open,
    settings.site_announcement_content,
  ],
  () => {
    announcement.value =
      announcementEnabled.value &&
      settings.site_announcement_default_open === "true";
  },
  { immediate: true },
);
const githubURL = computed(() => {
  try {
    const url = new URL(settings.site_github_url || "");
    return ["http:", "https:"].includes(url.protocol) &&
      !url.username &&
      !url.password
      ? url.href
      : "";
  } catch {
    return "";
  }
});
// 首次主题由头部同源脚本提前设置，避免暗色页面刷新时闪白。
const dark = ref(document.documentElement.dataset.theme === "dark");
provide(themeKey, dark);
// 功能区决定主题色：文本、文件、互传、管理各用一组颜色。
const section = computed(() => {
  const path = route.path;
  if (path.startsWith("/transfer")) return "transfer";
  if (path.startsWith("/upload") || path.startsWith("/file/")) return "file";
  if (
    path.startsWith("/admin") ||
    path.startsWith("/manage") ||
    path.startsWith("/login")
  )
    return "admin";
  return "text";
});
const navItems = computed(() => [
  ...(settings.site_home_editor_enabled !== "false"
    ? [
        {
          section: "text",
          to: "/",
          icon: "text-lines",
          label: "文本分享",
          short: "文本",
        },
      ]
    : []),
  ...(settings.site_upload_page_enabled !== "false"
    ? [
        {
          section: "file",
          to: "/upload",
          icon: "folder",
          label: "文件分享",
          short: "文件",
        },
      ]
    : []),
  {
    section: "transfer",
    to: "/transfer",
    icon: "transfer",
    label: "设备互传",
    short: "互传",
  },
  {
    section: "admin",
    to: accountTarget.value,
    icon: "grid",
    label: session.type === "none" ? "登录" : "管理",
    short: "管理",
  },
]);
const navPosition = computed(() =>
  navItems.value.findIndex((item) => item.section === section.value),
);
watch(
  section,
  (value) => {
    document.documentElement.dataset.section = value;
  },
  { immediate: true },
);
const uiTheme = computed(() => {
  // 组件库与页面共享语义色，主题切换时同时更新控件背景、文字和焦点状态。
  const palette = {
    text: ["#6366f1", "#8b5cf6", "#eef0ff", "#818cf8", "#a78bfa", "#1f2147"],
    file: ["#0ea5e9", "#6366f1", "#e3f4fd", "#38bdf8", "#818cf8", "#0f2a3a"],
    transfer: [
      "#10b981",
      "#0ea5e9",
      "#dcf6ec",
      "#34d399",
      "#38bdf8",
      "#0f2e24",
    ],
    admin: ["#f97316", "#ec4899", "#ffeee2", "#fb923c", "#f472b6", "#36210f"],
  }[section.value];
  // 组件库与页面共享语义色；主色随功能区切换，与 theme-vivid.css 保持一致。
  const colors = dark.value
    ? {
        primary: palette[3],
        hover: palette[4],
        pressed: palette[3],
        background: "#0b0d17",
        surface: "#131628",
        input: "#1a1e35",
        border: "#262b47",
        text: "#eef0ff",
        muted: "#99a0c2",
        soft: palette[5],
        onPrimary: "#ffffff",
      }
    : {
        primary: palette[0],
        hover: palette[1],
        pressed: palette[0],
        background: "#f6f7fb",
        surface: "#ffffff",
        input: "#ffffff",
        border: "#e7e9f1",
        text: "#101326",
        muted: "#626a86",
        soft: palette[2],
        onPrimary: "#ffffff",
      };
  // 设计稿（design/cloudpaste-ui-v3.html）的紧凑控件尺寸：按钮 32/26/34px、圆角 6/5px、
  // 输入 32px、字号 13/12px；开关 30×18、勾选框 15px。组件库尺寸统一在这里配置。
  const ring = (hex, alpha) => {
    const value = parseInt(hex.slice(1), 16);
    return `rgba(${value >> 16}, ${(value >> 8) & 255}, ${value & 255}, ${alpha})`;
  };
  const font =
    '"Noto Sans SC", "Microsoft YaHei", "PingFang SC", system-ui, sans-serif';
  const mono = '"JetBrains Mono", ui-monospace, Consolas, monospace';
  const strong = dark.value ? "#394068" : "#d9d9e0";
  return {
    common: {
      primaryColor: colors.primary,
      primaryColorHover: colors.hover,
      primaryColorPressed: colors.pressed,
      primaryColorSuppl: colors.primary,
      bodyColor: colors.background,
      cardColor: colors.surface,
      inputColor: colors.input,
      borderColor: colors.border,
      textColorBase: colors.text,
      textColor1: colors.text,
      textColor2: colors.text,
      textColor3: colors.muted,
      placeholderColor: colors.muted,
      errorColor: dark.value ? "#f27d91" : "#d6455d",
      errorColorHover: dark.value ? "#f27d91" : "#d6455d",
      successColor: dark.value ? "#4cd39a" : "#16a36b",
      warningColor: dark.value ? "#f0b44c" : "#c98410",
      borderRadius: "6px",
      borderRadiusSmall: "5px",
      heightTiny: "22px",
      heightSmall: "26px",
      heightMedium: "32px",
      heightLarge: "34px",
      fontSize: "13px",
      fontSizeTiny: "12px",
      fontSizeSmall: "12px",
      fontSizeMedium: "13px",
      fontSizeLarge: "13px",
      fontWeightStrong: "600",
      fontFamily: font,
      fontFamilyMono: mono,
    },
    Button: {
      heightTiny: "22px",
      heightSmall: "26px",
      heightMedium: "32px",
      heightLarge: "34px",
      paddingTiny: "0 6px",
      paddingSmall: "0 8px",
      paddingMedium: "0 12px",
      paddingLarge: "0 14px",
      paddingRoundSmall: "0 8px",
      paddingRoundMedium: "0 12px",
      fontSizeTiny: "12px",
      fontSizeSmall: "12px",
      fontSizeMedium: "13px",
      fontSizeLarge: "13px",
      fontWeight: "500",
      borderRadiusTiny: "5px",
      borderRadiusSmall: "5px",
      borderRadiusMedium: "6px",
      borderRadiusLarge: "6px",
      iconSizeTiny: "13px",
      iconSizeSmall: "13px",
      iconSizeMedium: "14px",
      iconSizeLarge: "14px",
      iconMarginSmall: "5px",
      iconMarginMedium: "6px",
      iconMarginLarge: "6px",
      border: `1px solid ${colors.border}`,
      borderHover: `1px solid ${ring(colors.primary, 0.45)}`,
      borderPressed: `1px solid ${colors.primary}`,
      borderFocus: `1px solid ${ring(colors.primary, 0.45)}`,
      textColor: colors.text,
      textColorHover: colors.primary,
      textColorPressed: colors.primary,
      textColorFocus: colors.primary,
      color: colors.surface,
      colorHover: colors.surface,
      colorPressed: colors.surface,
      colorFocus: colors.surface,
      textColorPrimary: colors.onPrimary,
      textColorHoverPrimary: colors.onPrimary,
      textColorPressedPrimary: colors.onPrimary,
      textColorFocusPrimary: colors.onPrimary,
      textColorQuaternary: colors.muted,
      textColorQuaternaryHover: colors.text,
      textColorQuaternaryPressed: colors.text,
      colorQuaternaryHover: dark.value ? "#1a1e35" : "#f4f4f6",
      colorQuaternaryPressed: dark.value ? "#1a1e35" : "#f4f4f6",
    },
    Input: {
      heightTiny: "22px",
      heightSmall: "26px",
      heightMedium: "32px",
      heightLarge: "34px",
      fontSizeTiny: "12px",
      fontSizeSmall: "12px",
      fontSizeMedium: "13px",
      fontSizeLarge: "13px",
      paddingTiny: "0 6px",
      paddingSmall: "0 8px",
      paddingMedium: "0 10px",
      paddingLarge: "0 10px",
      borderRadius: "6px",
      color: colors.input,
      colorFocus: colors.input,
      colorDisabled: dark.value ? "#1a1e35" : "#f4f4f6",
      border: `1px solid ${colors.border}`,
      borderHover: `1px solid ${colors.border}`,
      borderFocus: `1px solid ${colors.primary}`,
      boxShadowFocus: `0 0 0 3px ${ring(colors.primary, 0.18)}`,
      placeholderColor: colors.muted,
      lineHeightTextarea: "1.55",
    },
    InternalSelection: {
      heightTiny: "22px",
      heightSmall: "26px",
      heightMedium: "32px",
      heightLarge: "34px",
      fontSizeTiny: "12px",
      fontSizeSmall: "12px",
      fontSizeMedium: "13px",
      fontSizeLarge: "13px",
      paddingSingle: "0 26px 0 10px",
      borderRadius: "6px",
      color: colors.input,
      colorActive: colors.input,
      border: `1px solid ${colors.border}`,
      borderHover: `1px solid ${colors.border}`,
      borderActive: `1px solid ${colors.primary}`,
      borderFocus: `1px solid ${colors.primary}`,
      boxShadowActive: `0 0 0 3px ${ring(colors.primary, 0.18)}`,
      boxShadowFocus: `0 0 0 3px ${ring(colors.primary, 0.18)}`,
      placeholderColor: colors.muted,
    },
    InternalSelectMenu: {
      borderRadius: "8px",
      optionHeightMedium: "30px",
      optionFontSizeMedium: "13px",
      optionPaddingMedium: "0 10px",
      optionTextColorActive: colors.primary,
      optionCheckColor: colors.primary,
    },
    Switch: {
      railHeightSmall: "18px",
      railWidthSmall: "30px",
      buttonHeightSmall: "14px",
      buttonWidthSmall: "14px",
      railHeightMedium: "18px",
      railWidthMedium: "30px",
      buttonHeightMedium: "14px",
      buttonWidthMedium: "14px",
      buttonWidthPressedMedium: "18px",
      railColor: strong,
      railColorActive: colors.primary,
      buttonBoxShadow: "0 1px 3px rgba(0, 0, 0, 0.2)",
    },
    Checkbox: {
      sizeSmall: "15px",
      sizeMedium: "15px",
      sizeLarge: "15px",
      borderRadius: "4px",
      border: `1.5px solid ${strong}`,
      borderFocus: `1.5px solid ${colors.primary}`,
      borderChecked: `1.5px solid ${colors.primary}`,
      colorChecked: colors.primary,
      fontSizeMedium: "13px",
      labelPadding: "0 0 0 8px",
    },
    Card: { borderRadius: "14px" },
    Dialog: { borderRadius: "14px", padding: "16px", titleFontSize: "15px" },
    Notification: { borderRadius: "10px" },
  };
});
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
          ><span class="brand-icon" aria-hidden="true"></span
          ><span class="brand-name">{{
            settings.site_title || "CloudPaste"
          }}</span></RouterLink
        >
        <nav
          class="site-navigation"
          aria-label="主导航"
          :style="{ '--nav-position': navPosition }"
        >
          <span
            v-if="navPosition >= 0"
            class="nav-indicator"
            :class="{ 'nav-indicator-admin': section === 'admin' }"
            aria-hidden="true"
          ></span>
          <RouterLink
            v-for="item in navItems"
            :key="item.section"
            :to="item.to"
            :data-nav="item.section"
            :class="{ 'nav-current': item.section === section }"
            :aria-label="item.label"
          >
            <Icon :name="item.icon" /><span class="nav-label">{{
              item.label
            }}</span
            ><span class="nav-label-short" aria-hidden="true">{{
              item.short
            }}</span>
          </RouterLink>
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
            v-if="announcementEnabled"
            class="quiet icon-button site-bell"
            icon="bell"
            aria-label="站点公告"
            :aria-expanded="announcement"
            @click="announcement = !announcement"
          >
          </UiButton>
          <UiButton
            v-if="settings.site_github_enabled === 'true' && githubURL"
            tag="a"
            type="button"
            class="quiet icon-button"
            icon="github"
            :href="githubURL"
            target="_blank"
            rel="noopener noreferrer"
            aria-label="GitHub"
            title="GitHub"
          />
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
        <Transition name="announcement">
          <section
            v-if="announcement && announcementEnabled"
            class="site-announcement"
            role="region"
            aria-label="站点公告"
          >
            <span class="site-announcement-icon"><Icon name="bell" /></span>
            <MarkdownPreview
              class="site-announcement-content"
              :content="settings.site_announcement_content || ''"
            />
            <UiButton
              type="button"
              class="quiet site-announcement-close"
              size="small"
              icon="close"
              aria-label="关闭公告"
              title="关闭公告"
              @click="announcement = false"
            />
          </section>
        </Transition>
        <NNotificationProvider
          container-class="site-notifications"
          placement="top-right"
          :max="3"
          keep-alive-on-hover
        >
          <div :key="section" class="site-page"><RouterView /></div>
        </NNotificationProvider>
        <div class="custom-content" v-html="body"></div>
      </main>
      <footer class="site-footer">
        <MarkdownPreview
          v-if="settings.site_footer_markdown"
          :content="settings.site_footer_markdown"
        />
        <span v-else class="site-copyright">
          Copyright © 2026
          <a
            href="https://github.com/Qwerto107"
            target="_blank"
            rel="noopener noreferrer"
            >Qwerto107</a
          > · <a
            href="https://github.com/Qwerto107/CloudPaste"
            target="_blank"
            rel="noopener noreferrer"
            >CloudPaste</a
          >. All rights reserved.
        </span>
      </footer>
    </NDialogProvider>
  </NConfigProvider>
</template>
