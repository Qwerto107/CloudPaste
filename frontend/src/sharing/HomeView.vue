<script setup>
import { computed, defineAsyncComponent, ref } from "vue";
import { request, shareURL } from "./api.js";
import { hasPermission } from "./session.js";
import { settings } from "./settings.js";
const MarkdownPreview = defineAsyncComponent(
  () => import("./MarkdownPreview.vue"),
);
const title = ref("");
const content = ref("");
const password = ref("");
const hours = ref(Number(settings.share_default_expiry_hours || 24));
const maxViews = ref(0);
const preview = ref(false);
const advanced = ref(false);
const busy = ref(false);
const error = ref("");
const link = ref("");
const copied = ref(false);
const allowed = computed(() => hasPermission(1));
async function share() {
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
  <section class="panel">
    <div v-if="!allowed" class="notice">
      创建分享需要授权。<RouterLink to="/admin/login"
        >登录管理员或 API Key</RouterLink
      >
    </div>
    <form @submit.prevent="share">
      <input
        v-model="title"
        class="title-input"
        placeholder="标题（可选）"
        maxlength="200"
        aria-label="分享标题"
      />
      <div class="editor-heading">
        <span>{{ preview ? "Markdown 预览" : "文本内容" }}</span
        ><button type="button" class="quiet" @click="preview = !preview">
          {{ preview ? "继续编辑" : "预览" }}
        </button>
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
      <div class="form-row">
        <label
          >有效期<select v-model.number="hours">
            <option
              v-if="
                ![0, 1, 24, 168, 720].includes(
                  Number(settings.share_default_expiry_hours),
                )
              "
              :value="Number(settings.share_default_expiry_hours)"
            >
              站点默认（{{ settings.share_default_expiry_hours }} 小时）
            </option>
            <option :value="1">1 小时</option>
            <option :value="24">1 天</option>
            <option :value="168">7 天</option>
            <option :value="720">30 天</option>
            <option :value="0">长期保留</option>
          </select></label
        ><label
          >访问密码<input
            v-model="password"
            type="password"
            placeholder="可选"
            autocomplete="new-password"
            maxlength="72"
        /></label>
      </div>
      <button type="button" class="quiet" @click="advanced = !advanced">
        {{ advanced ? "收起" : "更多选项" }}</button
      ><label v-if="advanced"
        >最多查看次数<input
          v-model.number="maxViews"
          type="number"
          min="0"
          placeholder="0 为不限"
      /></label>
      <p v-if="error" class="error" role="alert">{{ error }}</p>
      <div class="actions">
        <span class="muted small">{{ content.length }} 个字符</span
        ><button
          class="primary"
          :disabled="busy || !allowed || !content.trim()"
        >
          {{ busy ? "正在创建…" : "创建分享" }}
        </button>
      </div>
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
