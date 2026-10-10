<script setup>
import { computed } from "vue";
import { session, hasPermission } from "./session.js";
import Icon from "./Icon.vue";
// 导航图标复用同一组本地 SVG，不引入额外资源请求。
function iconFor(path) {
  if (path === "text-management") return "text";
  if (path === "file-management") return "folder";
  if (path === "storage") return "storage";
  if (path === "key-management") return "key";
  return path === "account" ? "user" : "settings";
}
const links = computed(() => [
  ...(hasPermission(4) ? [{ path: "text-management", label: "文本管理" }] : []),
  ...(hasPermission(8) ? [{ path: "file-management", label: "文件管理" }] : []),
  ...(session.type === "admin"
    ? [
        { path: "storage", label: "存储管理" },
        { path: "key-management", label: "密钥与权限" },
        { path: "settings/global", label: "全局设置" },
        { path: "settings/preview", label: "预览设置" },
        { path: "settings/site", label: "站点设置" },
      ]
    : []),
  { path: "account", label: "账号管理" },
]);
</script>
<template>
  <div class="admin-layout">
    <nav class="admin-nav" aria-label="管理导航">
      <span class="nav-caption">管理空间</span>
      <RouterLink
        v-for="item in links"
        :key="item.path"
        :to="'/admin/' + item.path"
        ><Icon :name="iconFor(item.path)" />{{ item.label }}</RouterLink
      >
    </nav>
    <div class="admin-content"><RouterView /></div>
  </div>
</template>
