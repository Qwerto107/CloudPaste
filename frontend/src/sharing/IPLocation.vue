<script setup>
defineProps({ location: Object });
</script>

<template>
  <!-- 归属地只展示后端结果；使用 LITE 数据时保留来源署名及链接。 -->
  <span>
    {{ location?.summary || "未知" }}
    <small v-if="location?.source === 'ip2location'" class="muted">
      CloudPaste uses the IP2Location LITE database for
      <a
        href="https://www.ip2location.com"
        target="_blank"
        rel="noopener noreferrer"
        >IP geolocation</a
      >.
    </small>
  </span>
</template>
