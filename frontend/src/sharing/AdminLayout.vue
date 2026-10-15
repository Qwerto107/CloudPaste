<script setup>
import { computed } from "vue";
import { session, hasPermission } from "./session.js";
import Icon from "./Icon.vue";
import { useRoute } from "vue-router";
const route = useRoute();
const base = computed(() =>
  session.type === "admin" ? "/admin/" : "/manage/",
);
// 导航图标复用同一组本地 SVG，不引入额外资源请求。
function iconFor(path) {
  if (path === "text-management") return "text";
  if (path === "file-management") return "folder";
  if (path === "storage") return "storage";
  if (path === "key-management") return "key";
  if (path === "operation-logs") return "file";
  return path === "account" ? "user" : "settings";
}
const shareLinks = computed(() => [
  ...(hasPermission(4) ? [{ path: "text-management", label: "文本管理" }] : []),
  ...(hasPermission(8) ? [{ path: "file-management", label: "文件管理" }] : []),
]);
const systemLinks = computed(() => [
  ...(session.type === "admin"
    ? [
        { path: "storage", label: "存储管理" },
        { path: "key-management", label: "密钥与权限" },
        { path: "settings/global", label: "系统设置" },
      ]
    : []),
  { path: "account", label: "账号管理" },
  ...(session.type === "admin"
    ? [{ path: "operation-logs", label: "操作日志" }]
    : []),
]);
</script>
<template>
  <div class="admin-layout">
    <nav class="admin-nav" aria-label="管理导航">
      <span v-if="shareLinks.length" class="nav-caption">分享管理</span>
      <RouterLink
        v-for="item in shareLinks"
        :key="item.path"
        :to="base + item.path"
        ><Icon :name="iconFor(item.path)" />{{ item.label }}</RouterLink
      >
      <span class="nav-caption">{{
        session.type === "admin" ? "系统管理" : "我的账号"
      }}</span>
      <RouterLink
        v-for="item in systemLinks"
        :key="item.path"
        :to="base + item.path"
        :class="{
          'nav-selected':
            (item.path.startsWith('settings/') &&
              route.path.startsWith('/admin/settings/')) ||
            (item.path === 'operation-logs' &&
              route.path.startsWith('/admin/operation-logs/')),
        }"
        ><Icon :name="iconFor(item.path)" />{{ item.label }}</RouterLink
      >
    </nav>
    <div class="admin-content"><RouterView /></div>
  </div>
</template>
