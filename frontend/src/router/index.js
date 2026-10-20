import { createRouter, createWebHistory } from "vue-router";
import {
  initializeSession,
  session,
  hasPermission,
} from "../sharing/session.js";
import { loadSettings, settings } from "../sharing/settings.js";
import { auditModules } from "../sharing/audit-modules.js";
const management = () => import("../sharing/AdminView.vue");
const routes = [
  {
    path: "/transfer",
    name: "Transfer",
    component: () => import("../sharing/TransferView.vue"),
    meta: { title: "设备互传" },
  },
  {
    path: "/",
    name: "Home",
    component: () => import("../sharing/HomeView.vue"),
    meta: { title: "文本分享" },
  },
  {
    path: "/upload",
    name: "Upload",
    component: () => import("../sharing/UploadView.vue"),
    meta: { title: "文件分享" },
  },
  {
    path: "/paste/:slug",
    name: "PasteView",
    component: () => import("../sharing/ShareView.vue"),
    meta: { title: "文本预览" },
  },
  {
    path: "/file/:slug",
    name: "FileView",
    component: () => import("../sharing/ShareView.vue"),
    meta: { title: "文件预览", fileShare: true },
  },
  {
    path: "/login",
    name: "UserLogin",
    component: () => import("../sharing/LoginView.vue"),
    meta: { title: "API Key 登录" },
  },
  {
    path: "/admin/login",
    name: "AdminLogin",
    component: () => import("../sharing/LoginView.vue"),
    meta: { title: "管理员登录", adminArea: true },
  },
  {
    path: "/admin",
    component: () => import("../sharing/AdminLayout.vue"),
    meta: { requiresAuth: true, adminArea: true },
    children: [
      {
        path: "operation-logs",
        component: () => import("../sharing/AuditView.vue"),
        meta: { title: "操作日志" },
      },
      {
        path: "log-cleanup",
        redirect: "/admin/settings/logs",
      },
      // 每个分类使用固定路由，防止无效模块进入日志查询。
      ...auditModules.map(({ key, label }) => ({
        path: `operation-logs/${key}`,
        component: () => import("../sharing/AuditView.vue"),
        meta: { title: `${label}日志`, auditModule: key },
      })),
      {
        path: "",
        name: "AdminHome",
        component: management,
        meta: { panel: "account" },
      },
      ...[
        ["text-management", "texts", "文本管理", 4],
        ["file-management", "files", "文件管理", 8],
        ["storage", "storage", "存储管理", 0],
        ["key-management", "keys", "密钥与权限", 0],
        ["account", "account", "账号管理", -1],
        ["settings/global", "global", "常规设置", 0],
        ["settings/preview", "preview", "文件预览设置", 0],
        ["settings/transfer", "transfer", "设备互传设置", 0],
        ["settings/security", "security", "安全与限频", 0],
        ["settings/ip-location", "ip-location", "IP 归属地设置", 0],
        ["settings/site", "site", "站点外观设置", 0],
        ["settings/logs", "logs", "日志设置", 0],
      ].map(([path, panel, title, permission]) => ({
        path,
        component: path.startsWith("settings/")
          ? () => import("../sharing/SettingsView.vue")
          : management,
        meta: { panel, title, permission },
      })),
    ],
  },
  {
    path: "/manage",
    component: () => import("../sharing/AdminLayout.vue"),
    meta: { requiresAuth: true, userArea: true },
    children: [
      {
        path: "",
        name: "UserHome",
        component: management,
        meta: { panel: "account" },
      },
      ...[
        ["text-management", "texts", "文本管理", 4],
        ["file-management", "files", "文件管理", 8],
        ["account", "account", "账号信息", -1],
      ].map(([path, panel, title, permission]) => ({
        path,
        component: management,
        meta: { panel, title, permission },
      })),
    ],
  },
  { path: "/:pathMatch(.*)*", redirect: "/" },
];
const router = createRouter({ history: createWebHistory(), routes });
function managementHome() {
  const base = session.type === "admin" ? "/admin" : "/manage";
  return hasPermission(4)
    ? base + "/text-management"
    : hasPermission(8)
      ? base + "/file-management"
      : base + "/account";
}
// 导航先恢复身份，再按权限选择管理页，避免登录成功后被旧守卫重定向回来。
router.beforeEach(async (to, from) => {
  if (to.meta.adminArea && !from.meta.adminArea) {
    const initialNavigation = from.matched.length === 0;
    const adminPageLoaded =
      window.location.pathname === "/admin" ||
      window.location.pathname.startsWith("/admin/");
    // 跨入管理员区域时交由服务器处理；直接加载管理员页面不重复刷新。
    if (!initialNavigation || !adminPageLoaded) {
      // 等待新文档接管，避免取消后退导航时回滚地址，也不提前显示目标页面。
      return new Promise(() => {
        window.location.assign(to.fullPath);
      });
    }
  }
  await Promise.all([initializeSession(), loadSettings()]);
  const loggedIn = session.type !== "none";
  // 页面入口也按身份隔离：API Key 的登录和管理过程始终留在普通用户路由。
  if (
    (to.meta.adminArea && session.type === "apikey") ||
    (to.meta.userArea && session.type === "admin")
  )
    return managementHome();
  if (to.meta.requiresAuth && !loggedIn)
    return {
      path: to.meta.adminArea ? "/admin/login" : "/login",
      query: { redirect: to.fullPath },
    };
  if (
    loggedIn &&
    ["AdminLogin", "AdminHome", "UserLogin", "UserHome"].includes(to.name)
  )
    return managementHome();
  if (
    to.meta.requiresAuth &&
    ((to.meta.permission === 0 && session.type !== "admin") ||
      (to.meta.permission > 0 && !hasPermission(to.meta.permission)))
  )
    return managementHome();
  if (to.name === "Home" && settings.site_home_editor_enabled === "false")
    return settings.site_upload_page_enabled !== "false"
      ? "/upload"
      : loggedIn
        ? managementHome()
        : "/login";
  if (to.name === "Upload" && settings.site_upload_page_enabled === "false")
    return settings.site_home_editor_enabled !== "false"
      ? "/"
      : loggedIn
        ? managementHome()
        : "/login";
});
router.afterEach((to) => {
  document.title = `${to.meta.title || "分享"} · ${settings.site_title || "CloudPaste"}`;
});
router.onError((error) =>
  window.dispatchEvent(
    new CustomEvent("sharing-error", { detail: error.message }),
  ),
);
export default router;
