<script setup>
defineProps({ name: { type: String, default: "file" } });
// 只使用本站内置路径，避免为了少量图标加载字体或第三方图标库。
const paths = {
  close: "M6 6l12 12M18 6 6 18",
  retry: "M20 7v5h-5M4 17v-5h5M6.1 6.1A8 8 0 0 1 20 12M4 12a8 8 0 0 0 13.9 5.9",
  transfer: "M3 7h17m-4-4 4 4-4 4M21 17H4m4-4-4 4 4 4",
  cloud: "M7 18a5 5 0 1 1 .7-9.95A6 6 0 0 1 19 10a4 4 0 0 1-1 8H7Z",
  text: "M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8l-6-6Zm0 0v6h6M8 12h8M8 16h6",
  file: "M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8l-6-6Zm0 0v6h6",
  folder:
    "M3 7V5a2 2 0 0 1 2-2h5l2 3h7a2 2 0 0 1 2 2v11a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2V7Z",
  grid: "M3 3h7v7H3V3Zm11 0h7v7h-7V3ZM3 14h7v7H3v-7Zm11 0h7v7h-7v-7Z",
  upload: "M12 16V3m-5 5 5-5 5 5M4 16v4a1 1 0 0 0 1 1h14a1 1 0 0 0 1-1v-4",
  sun: "M12 3V1m0 22v-2M3 12H1m22 0h-2M5.6 5.6 4.2 4.2m15.6 15.6-1.4-1.4m0-12.8 1.4-1.4M4.2 19.8l1.4-1.4M16 12a4 4 0 1 1-8 0 4 4 0 0 1 8 0Z",
  moon: "M21 13a9 9 0 0 1-10-10A9 9 0 1 0 21 13Z",
  key: "M15 14a6 6 0 1 0-5-5L2 17v5h5v-3h3v-3l5-2Z",
  user: "M16 7a4 4 0 1 1-8 0 4 4 0 0 1 8 0ZM4 21v-2a8 8 0 0 1 16 0v2",
  settings: "M4 6h16M4 12h16M4 18h16M8 3v6m8 0v6m-6 0v6",
  trash: "M4 7h16M9 4h6M6 7l1 14h10l1-14M10 11v6m4-6v6",
  storage: "M3 6h18v6H3V6Zm0 6v6h18v-6M7 9h.01M7 15h.01",
  link: "M10 13a5 5 0 0 0 7 0l3-3a5 5 0 0 0-7-7l-2 2m3 6a5 5 0 0 0-7 0l-3 3a5 5 0 0 0 7 7l2-2",
  bell: "M18 8a6 6 0 0 0-12 0c0 7-3 7-3 9h18c0-2-3-2-3-9M10 21h4",
  github:
    "M9 19c-4.3 1.3-4.3-2.5-6-3m12 6v-3.9a3.4 3.4 0 0 0-.9-2.7c3-.3 6.2-1.5 6.2-6.9a5.4 5.4 0 0 0-1.5-3.8 5 5 0 0 0-.1-3.8s-1.2-.4-3.9 1.5a13.4 13.4 0 0 0-7 0C5.1.5 3.9.9 3.9.9A5 5 0 0 0 3.8 4.7 5.4 5.4 0 0 0 2.3 8.5c0 5.4 3.2 6.6 6.2 6.9a3.4 3.4 0 0 0-.9 2.7V22",
  logout: "M9 3H4v18h5m5-5 5-4-5-4m-6 4h11",
};
</script>
<template>
  <svg
    class="icon"
    viewBox="0 0 24 24"
    fill="none"
    stroke="currentColor"
    stroke-width="1.8"
    stroke-linecap="round"
    stroke-linejoin="round"
    aria-hidden="true"
    focusable="false"
  >
    <path :d="paths[name] || paths.file" />
  </svg>
</template>
