<script setup>
import { computed, defineAsyncComponent, ref } from "vue";
import { useRoute } from "vue-router";
import { request, shareURL } from "./api.js";
import { hasPermission } from "./session.js";
import { settings } from "./settings.js";
import { shareText } from "./share-text.js";
import ShareNameField from "./ShareNameField.vue";
import ExpirySelect from "./ExpirySelect.vue";
const MarkdownEditor = defineAsyncComponent(
  () => import("./MarkdownEditor.vue"),
);
const route = useRoute();
const title = ref("");
const slug = ref("");
const content = ref("");
const password = ref("");
const hours = ref(Number(settings.share_default_expiry_hours ?? 24));
const maxViews = ref(0);
const busy = ref(false);
const error = ref("");
const link = ref("");
const copied = ref(false);
const sharedPassword = ref("");
const copiedText = ref(false);
const allowed = computed(() => hasPermission(1));
async function share() {
  if (busy.value || !allowed.value) return;
  busy.value = true;
  error.value = "";
  link.value = "";
  copied.value = false;
  copiedText.value = false;
  try {
    const expires_at =
      hours.value > 0
        ? new Date(Date.now() + hours.value * 3600000).toISOString()
        : null;
    const record = await request("/paste", {
      method: "POST",
      body: {
        title: title.value,
        slug: slug.value,
        content: content.value,
        password: password.value,
        expires_at,
        max_views: Number(maxViews.value),
        is_public: true,
      },
    });
    link.value = shareURL("paste", record.slug);
    // 结果绑定本次创建响应，修改表单不会改变已生成分享的密码。
    sharedPassword.value = record.password || "";
  } catch (reason) {
    error.value = reason.message;
  } finally {
    busy.value = false;
  }
}
async function copy() {
  try {
    await navigator.clipboard.writeText(link.value);
    copied.value = true;
  } catch {
    error.value = "复制失败，请手动复制链接";
  }
}
async function copyShareText() {
  try {
    await navigator.clipboard.writeText(
      shareText(link.value, sharedPassword.value),
    );
    copiedText.value = true;
  } catch {
    error.value = "复制失败，请手动复制分享链接和密码";
  }
}
</script>
<template>
  <div class="page-heading">
    <p class="eyebrow">简单分享，按时到期</p>
    <h1>把文字留在这里</h1>
    <p class="muted">粘贴文本或 Markdown，生成一个带有效期的分享链接。</p>
  </div>
  <section class="panel compose-panel">
    <div v-if="!allowed" class="notice">
      创建分享需要授权。<RouterLink
        :to="{ path: '/login', query: { redirect: route.fullPath } }"
        >使用 API Key 登录</RouterLink
      >
    </div>
    <form @submit.prevent="share">
      <UiFieldset class="share-fields" :disabled="busy || !allowed">
        <UiInput
          v-model="title"
          class="title-input"
          placeholder="标题（可选）"
          maxlength="200"
          aria-label="分享标题"
        />
        <MarkdownEditor v-model="content" :disabled="busy || !allowed" />
        <ShareNameField v-model="slug" id="text-share-name" :disabled="busy" />
        <div class="share-options">
          <div class="form-row share-main-fields">
            <ExpirySelect v-model="hours" />
            <label
              >最多查看次数<UiInput
                v-model.number="maxViews"
                type="number"
                min="0"
                step="1"
                required
              /><small class="muted">0 表示不限次数</small></label
            >
            <label
              >访问密码<UiInput
                v-model="password"
                type="password"
                placeholder="可选"
                autocomplete="new-password"
                maxlength="72"
            /></label>
          </div>
          <UiButton
            class="primary"
            icon="link"
            :disabled="busy || !allowed || !content.trim()"
          >
            {{ busy ? "正在创建…" : "创建分享" }}
          </UiButton>
        </div>
      </UiFieldset>
      <p v-if="error" class="error" role="alert">{{ error }}</p>
    </form>
  </section>
  <section v-if="link" class="panel result">
    <h2>分享已创建</h2>
    <!-- 原生只读文本框可选择、复制整段文字，禁用剪贴板权限时也能使用。 -->
    <UiInput
      :value="shareText(link, sharedPassword)"
      type="textarea"
      :rows="sharedPassword ? 2 : 1"
      :autosize="{ minRows: sharedPassword ? 2 : 1, maxRows: 6 }"
      readonly
      aria-label="分享文字"
    />
    <div class="result-actions">
      <UiButton type="button" @click="copy">{{
        copied ? "链接已复制" : "复制链接"
      }}</UiButton>
      <UiButton type="button" @click="copyShareText">{{
        copiedText ? "分享文字已复制" : "复制分享文字"
      }}</UiButton>
      <UiButton
        tag="a"
        type="button"
        :href="link"
        target="_blank"
        rel="noopener noreferrer"
        >打开分享</UiButton
      >
    </div>
  </section>
</template>
