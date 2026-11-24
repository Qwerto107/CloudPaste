<script setup>
import { computed, defineAsyncComponent, ref, useTemplateRef } from "vue";
import { useRoute } from "vue-router";
import { request, shareURL } from "./api.js";
import { hasPermission } from "./session.js";
import { settings } from "./settings.js";
import { shareSettingsSummary } from "./share-expiry.js";
import ShareNameField from "./ShareNameField.vue";
import ShareAccessFields from "./ShareAccessFields.vue";
import ShareResult from "./ShareResult.vue";
import ExpirySelect from "./ExpirySelect.vue";
import Icon from "./Icon.vue";
import PageIllustration from "./PageIllustration.vue";
import MobileSheet from "./ui/MobileSheet.vue";

const MarkdownEditor = defineAsyncComponent(
  () => import("./MarkdownEditor.vue"),
);
const route = useRoute();
const form = useTemplateRef("form");
const title = ref("");
const slug = ref("");
const content = ref("");
const password = ref("");
const hours = ref(Number(settings.share_default_expiry_hours ?? 24));
const maxViews = ref(0);
const busy = ref(false);
const error = ref("");
const result = ref(null);
const settingsOpen = ref(false);
const resultOpen = ref(false);
const allowed = computed(() => hasPermission(1));
const canSubmit = computed(
  () =>
    !busy.value &&
    allowed.value &&
    Number.isFinite(hours.value) &&
    content.value.trim(),
);

function shortcut(event) {
  if (
    event.key !== "Enter" ||
    event.isComposing ||
    (!event.ctrlKey && !event.metaKey)
  )
    return;
  event.preventDefault();
  event.stopPropagation();
  if (canSubmit.value) form.value.requestSubmit();
}
async function share() {
  if (!canSubmit.value) return;
  busy.value = true;
  error.value = "";
  result.value = null;
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
    // 结果使用本次创建响应中的密码，不受后续编辑影响。
    result.value = {
      link: shareURL("paste", record.slug),
      password: record.password || "",
    };
    settingsOpen.value = false;
    resultOpen.value = true;
  } catch (reason) {
    error.value = reason.message;
  } finally {
    busy.value = false;
  }
}
</script>

<template>
  <header class="page-heading hero">
    <div>
      <h1>文本分享</h1>
      <p class="muted">粘贴文字或 Markdown，生成一条带有效期的链接</p>
      <ul class="page-chips">
        <li>Markdown 实时预览</li>
        <li>密码与查看次数</li>
        <li>Ctrl + Enter 创建</li>
      </ul>
    </div>
    <PageIllustration section="text" />
  </header>
  <div v-if="!allowed" class="notice share-permission">
    创建分享需要授权。<RouterLink
      :to="{ path: '/login', query: { redirect: route.fullPath } }"
      >使用 API Key 登录</RouterLink
    >
  </div>
  <form
    ref="form"
    class="share-layout"
    @submit.prevent="share"
    @keydown.capture="shortcut"
    @invalid.capture="settingsOpen = true"
  >
    <section class="panel compose-panel" aria-label="Markdown 编辑器">
      <UiInput
        v-model="title"
        class="title-input"
        placeholder="标题（可选）"
        maxlength="200"
        aria-label="分享标题"
        :disabled="busy || !allowed"
      />
      <MarkdownEditor v-model="content" :disabled="busy || !allowed" compact />
      <p v-if="error" class="error share-form-error" role="alert">
        {{ error }}
      </p>
    </section>
    <UiButton
      v-if="result"
      type="button"
      class="mobile-result-link"
      @click="resultOpen = true"
      >查看分享结果</UiButton
    >
    <aside class="share-sidebar" aria-label="分享设置与结果">
      <MobileSheet v-model="settingsOpen" title="分享设置">
        <UiFieldset class="panel share-settings" :disabled="busy || !allowed">
          <h2>分享设置</h2>
          <ShareNameField v-model="slug" id="text-share-name" />
          <ExpirySelect v-model="hours" />
          <ShareAccessFields
            v-model:password="password"
            v-model:max-views="maxViews"
          />
          <UiButton
            class="primary share-submit"
            icon="link"
            :disabled="!canSubmit"
            >{{ busy ? "正在创建…" : "创建分享" }}</UiButton
          >
          <small class="muted share-shortcut">Ctrl + Enter 快速创建</small>
          <p v-if="settingsOpen && error" class="error" role="alert">
            {{ error }}
          </p>
        </UiFieldset>
        <template #footer>
          <UiButton type="button" @click="settingsOpen = false">完成</UiButton>
          <UiButton class="primary" :disabled="!canSubmit">{{
            busy ? "正在创建…" : "创建分享"
          }}</UiButton>
        </template>
      </MobileSheet>
      <MobileSheet v-if="result" v-model="resultOpen" title="分享结果">
        <ShareResult v-bind="result" title="分享已创建" />
      </MobileSheet>
    </aside>
    <div class="mobile-share-actions">
      <button
        type="button"
        class="mobile-settings-summary"
        @click="settingsOpen = true"
      >
        <Icon name="gear" /><span
          ><small>分享设置</small
          >{{ shareSettingsSummary(hours, password, maxViews) }}</span
        >
      </button>
      <UiButton
        class="primary"
        :disabled="busy || !allowed || !content.trim()"
        @click="!Number.isFinite(hours) && (settingsOpen = true)"
        >{{ busy ? "正在创建…" : "创建分享" }}</UiButton
      >
    </div>
  </form>
</template>
