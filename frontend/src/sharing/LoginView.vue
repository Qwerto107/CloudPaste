<script setup>
import { computed, ref, watch } from "vue";
import { useRouter, useRoute } from "vue-router";
import { request, backendBase, apiKeyAuthorization } from "./api.js";
import { saveSession } from "./session.js";
const router = useRouter();
const route = useRoute();
const mode = computed(() => (route.name === "AdminLogin" ? "admin" : "apikey"));
const username = ref("");
const password = ref("");
const apiKey = ref("");
const busy = ref(false);
const error = ref("");
watch(mode, () => {
  username.value = "";
  password.value = "";
  apiKey.value = "";
  error.value = "";
});
async function login() {
  busy.value = true;
  error.value = "";
  try {
    if (mode.value === "admin") {
      const data = await request("/admin/login", {
        method: "POST",
        public: true,
        body: { username: username.value, password: password.value },
      });
      if (!data?.token) throw new Error("登录响应缺少会话令牌");
      saveSession({
        type: "admin",
        token: data.token,
        name: username.value,
        basicPath: "/",
        permissions: 15,
      });
    } else {
      // 验证使用当前输入的 Key，不继承旧管理员会话。
      const response = await fetch(backendBase + "/api/test/api-key", {
        headers: { Authorization: apiKeyAuthorization(apiKey.value) },
        cache: "no-store",
        signal: AbortSignal.timeout(30000),
      });
      const result = await response.json();
      if (!response.ok || !result.success)
        throw new Error(result.message || "密钥验证失败");
      const p = result.data.permissions || {};
      saveSession({
        type: "apikey",
        token: apiKey.value,
        name: result.data.key_info?.name || "API Key",
        basicPath: result.data.basic_path || "/",
        permissions:
          (p.text_share ? 1 : 0) |
          (p.file_share ? 2 : 0) |
          (p.text_manage ? 4 : 0) |
          (p.file_manage ? 8 : 0),
      });
    }
    password.value = "";
    apiKey.value = "";
    // 只允许本站已注册的页面，并等待导航结果，失败时显示具体原因。
    const redirect = route.query.redirect;
    const target =
      typeof redirect === "string" &&
      redirect.startsWith("/") &&
      !redirect.startsWith("//") &&
      !redirect.startsWith("/admin/login") &&
      !redirect.startsWith("/login") &&
      (mode.value === "admin" || !redirect.startsWith("/admin")) &&
      router.resolve(redirect).matched.length
        ? redirect
        : mode.value === "admin"
          ? "/admin"
          : "/manage";
    const failure = await router.replace(target);
    if (failure) throw new Error("已登录，但页面跳转失败，请刷新后重试");
  } catch (reason) {
    error.value = reason.message;
  } finally {
    busy.value = false;
  }
}
</script>
<template>
  <section class="panel login-panel">
    <h1>{{ mode === "admin" ? "管理员登录" : "API Key 登录" }}</h1>
    <p class="muted">
      {{
        mode === "admin"
          ? "登录后台，管理分享、存储与站点设置。"
          : "输入密钥，按授权创建和管理你的分享。"
      }}
    </p>
    <form @submit.prevent="login">
      <template v-if="mode === 'admin'"
        ><label
          >账号<input
            v-model="username"
            autocomplete="username"
            required /></label
        ><label
          >密码<input
            v-model="password"
            type="password"
            autocomplete="current-password"
            required /></label></template
      ><label v-else
        >API Key<input
          v-model="apiKey"
          type="password"
          autocomplete="off"
          required
      /></label>
      <p v-if="error" class="error" role="alert">{{ error }}</p>
      <button class="primary wide" :disabled="busy">
        {{ busy ? "正在登录…" : "登录" }}
      </button>
    </form>
    <p class="login-alternative">
      <RouterLink v-if="mode === 'apikey'" to="/admin/login"
        >管理员登录 →</RouterLink
      >
      <RouterLink v-else to="/login">使用 API Key 登录 →</RouterLink>
    </p>
  </section>
</template>
