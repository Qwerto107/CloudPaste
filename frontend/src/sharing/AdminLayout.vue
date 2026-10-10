<script setup>
import { computed } from "vue";
import { session, hasPermission } from "./session.js";
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
      <RouterLink
        v-for="item in links"
        :key="item.path"
        :to="'/admin/' + item.path"
        >{{ item.label }}</RouterLink
      >
    </nav>
    <div class="admin-content"><RouterView /></div>
  </div>
</template>
