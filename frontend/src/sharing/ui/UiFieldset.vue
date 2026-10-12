<script setup>
import { provide } from "vue";
import { disabledKey, useDisabled } from "./context.js";
const props = defineProps({ disabled: Boolean });
const disabled = useDisabled(props);
// 自定义选择器也继承 fieldset 的禁用状态，不能绕过正在上传时的锁定。
provide(disabledKey, disabled);
</script>
<template>
  <fieldset :disabled="disabled"><slot /></fieldset>
</template>
