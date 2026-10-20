<script setup>
defineProps({ location: Object });
</script>

<template>
  <span>{{ location?.summary || "未知" }}</span>
</template>
