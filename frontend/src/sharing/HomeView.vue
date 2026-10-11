<script setup>
import { computed, defineAsyncComponent, ref } from "vue";
import { useRoute } from "vue-router";
import { request, shareURL } from "./api.js";
import { hasPermission } from "./session.js";
import { settings } from "./settings.js";
import Icon from "./Icon.vue";
import ShareNameField from "./ShareNameField.vue";
import ExpirySelect from "./ExpirySelect.vue";
const MarkdownPreview = defineAsyncComponent(
  () => import("./MarkdownPreview.vue"),
);
const route = useRoute();
const title = ref("");
const slug = ref("");
const content = ref("");
const password = ref("");
const hours = ref(Number(settings.share_default_expiry_hours ?? 24));
const maxViews = ref(0);
const preview = ref(false);
const busy = ref(false);
const error = ref("");
const link = ref("");
const copied = ref(false);
const allowed = computed(() => hasPermission(1));
async function share() {
  if (busy.value || !allowed.value) return;
  busy.value = true;
  error.value = "";
  link.value = "";
  copied.value = false;
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
      <fieldset class="share-fields" :disabled="busy || !allowed">
        <input
          v-model="title"
          class="title-input"
          placeholder="标题（可选）"
          maxlength="200"
          aria-label="分享标题"
        />
        <div class="editor-heading compose-heading">
          <div class="tabs" aria-label="编辑视图">
            <button
              type="button"
              :class="{ active: !preview }"
              :aria-pressed="!preview"
              @click="preview = false"
            >
              编辑
            </button>
            <button
              type="button"
              :class="{ active: preview }"
              :aria-pressed="preview"
              @click="preview = true"
            >
              预览
            </button>
          </div>
          <span class="muted small">支持 Markdown</span>
        </div>
        <MarkdownPreview
          v-if="preview"
          :content="content"
          class="editor-preview"
        /><textarea
          v-else
          v-model="content"
          class="text-editor"
          placeholder="粘贴文字，或写一点 Markdown…"
          required
          aria-label="分享内容"
        ></textarea>
        <p class="editor-meta muted small">{{ content.length }} 个字符</p>
        <ShareNameField v-model="slug" id="text-share-name" :disabled="busy" />
        <div class="share-options">
          <div class="form-row share-main-fields">
            <ExpirySelect v-model="hours" />
            <label
              >最多查看次数<input
                v-model.number="maxViews"
                type="number"
                min="0"
                step="1"
                required
              /><small class="muted">0 表示不限次数</small></label
            >
            <label
              >访问密码<input
                v-model="password"
                type="password"
                placeholder="可选"
                autocomplete="new-password"
                maxlength="72"
            /></label>
          </div>
          <button
            class="primary"
            :disabled="busy || !allowed || !content.trim()"
          >
            <Icon name="link" />{{ busy ? "正在创建…" : "创建分享" }}
          </button>
        </div>
      </fieldset>
      <p v-if="error" class="error" role="alert">{{ error }}</p>
    </form>
  </section>
  <section v-if="link" class="panel result">
    <h2>分享已创建</h2>
    <div class="copy-row">
      <input :value="link" readonly aria-label="分享链接" /><button
        @click="copy"
      >
        {{ copied ? "已复制" : "复制链接" }}
      </button>
    </div>
    <a :href="link" target="_blank" rel="noopener noreferrer">打开分享 →</a>
  </section>
</template>
