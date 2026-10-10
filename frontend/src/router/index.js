import { createRouter, createWebHistory } from "vue-router";
import {
  initializeSession,
  session,
  hasPermission,
} from "../sharing/session.js";
import { loadSettings, settings } from "../sharing/settings.js";
const management = () => import("../sharing/AdminView.vue");
const routes = [
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
    path: "/admin/login",
    name: "AdminLogin",
    component: () => import("../sharing/LoginView.vue"),
    meta: { title: "登录" },
  },
  {
    path: "/admin",
    component: () => import("../sharing/AdminLayout.vue"),
    meta: { requiresAuth: true },
    children: [
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
        ["settings/global", "global", "全局设置", 0],
        ["settings/preview", "preview", "预览设置", 0],
        ["settings/site", "site", "站点设置", 0],
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
  return hasPermission(4)
    ? "/admin/text-management"
    : hasPermission(8)
      ? "/admin/file-management"
      : "/admin/account";
}
// 导航先恢复身份，再按权限选择管理页，避免登录成功后被旧守卫重定向回来。
router.beforeEach(async (to) => {
  await Promise.all([initializeSession(), loadSettings()]);
  const loggedIn = session.type !== "none";
  if (to.meta.requiresAuth && !loggedIn)
    return { path: "/admin/login", query: { redirect: to.fullPath } };
  if (loggedIn && (to.name === "AdminLogin" || to.name === "AdminHome"))
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
        : "/admin/login";
  if (to.name === "Upload" && settings.site_upload_page_enabled === "false")
    return settings.site_home_editor_enabled !== "false"
      ? "/"
      : loggedIn
        ? managementHome()
        : "/admin/login";
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
