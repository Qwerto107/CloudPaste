<script setup>
import { ref } from "vue";
import { useRouter } from "vue-router";
import { request } from "../api.js";
import { session, clearSession } from "../session.js";

defineProps({
  permissions: { type: Array, required: true },
  busy: { type: Boolean, default: false },
});

const emit = defineEmits(["update:busy", "error"]);
const router = useRouter();
const currentPassword = ref("");
const newPassword = ref("");
const newUsername = ref(session.name);

async function changePassword() {
  emit("update:busy", true);
  emit("error", "");
  try {
    await request("/admin/change-password", {
      method: "POST",
      body: {
        currentPassword: currentPassword.value,
        newPassword: newPassword.value,
        newUsername: newUsername.value,
      },
    });
    clearSession();
    await router.replace("/admin/login");
  } catch (reason) {
    emit("error", reason.message);
  } finally {
    emit("update:busy", false);
  }
}
</script>

<template>
  <section class="panel management-editor account-panel">
    <div class="editor-heading">
      <h2>{{ session.name || "当前账号" }}</h2>
      <span class="muted small">{{
        session.type === "admin" ? "管理员" : "API Key 用户"
      }}</span>
    </div>
    <template v-if="session.type === 'admin'">
      <form class="account-form" @submit.prevent="changePassword">
        <label
          >账号<UiInput
            v-model="newUsername"
            required
            autocomplete="username" /></label
        ><label
          >当前密码<UiInput
            v-model="currentPassword"
            type="password"
            required
            autocomplete="current-password" /></label
        ><label
          >新密码<UiInput
            v-model="newPassword"
            type="password"
            minlength="6"
            maxlength="72"
            required
            aria-label="新密码"
            autocomplete="new-password" /></label
        ><small class="muted account-password-hint"
          >至少 6 个字符，最多 72 字节</small
        >
        <div>
          <UiButton class="primary" :disabled="busy"
            >更新账号并重新登录</UiButton
          >
        </div>
      </form>
    </template>
    <div v-else class="management-panel-body">
      <p>当前权限：</p>
      <ul>
        <li
          v-for="entry in permissions.filter(
            (entry) => (session.permissions & entry.bit) !== 0,
          )"
          :key="entry.bit"
        >
          {{ entry.label }}
        </li>
      </ul>
    </div>
  </section>
</template>
