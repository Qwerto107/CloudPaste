<script setup>
import { computed, useTemplateRef, watchPostEffect } from "vue";
import { session, hasPermission } from "./session.js";
import Icon from "./Icon.vue";
import { useRoute } from "vue-router";
const route = useRoute();
const navigation = useTemplateRef("navigation");
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
const settingsActive = computed(() =>
  route.path.startsWith("/admin/settings/"),
);
const settingsLinks = [
  ["global", "常规设置"],
  ["preview", "文件预览"],
  ["transfer", "设备互传"],
  ["security", "安全与限频"],
  ["ip-location", "IP 归属地"],
  ["site", "站点外观"],
  ["logs", "日志设置"],
];
watchPostEffect(() => {
  if (!route.path || !window.matchMedia("(max-width: 650px)").matches) return;
  const active = navigation.value?.querySelector('[aria-current="page"]');
  if (active) navigation.value.scrollLeft = Math.max(0, active.offsetLeft - 12);
});
</script>
<template>
  <div class="admin-layout">
    <nav ref="navigation" class="admin-nav" aria-label="管理导航">
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
      <template v-for="item in systemLinks" :key="item.path">
        <RouterLink
          :to="base + item.path"
          :class="{
            'nav-settings': item.path.startsWith('settings/'),
            'nav-selected':
              item.path === 'operation-logs' &&
              route.path.startsWith('/admin/operation-logs/'),
          }"
        >
          <Icon :name="iconFor(item.path)" />{{ item.label }}
        </RouterLink>
        <div
          v-if="settingsActive && item.path.startsWith('settings/')"
          class="admin-settings-nav"
        >
          <RouterLink
            v-for="[key, label] in settingsLinks"
            :key="key"
            :to="base + 'settings/' + key"
            >{{ label }}</RouterLink
          >
        </div>
      </template>
    </nav>
    <div class="admin-content"><RouterView /></div>
  </div>
</template>
