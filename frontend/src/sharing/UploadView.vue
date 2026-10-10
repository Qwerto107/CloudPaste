<script setup>
import { computed, onMounted, onBeforeUnmount, ref } from "vue";
import { request, uploadShare, shareURL } from "./api.js";
import { hasPermission, session } from "./session.js";
import { settings } from "./settings.js";
const files = ref([]);
const storages = ref([]);
const storage = ref("");
const hours = ref(Number(settings.share_default_expiry_hours || 24));
const password = ref("");
const remark = ref("");
const maxViews = ref(0);
const busy = ref(false);
const progress = ref(0);
const current = ref("");
const results = ref([]);
const error = ref("");
let controller;
const allowed = computed(() => hasPermission(2));
// 页面离开时取消传输，服务端将清理未完成的临时文件。
onBeforeUnmount(() => controller?.abort());
onMounted(async () => {
  if (!allowed.value) return;
  try {
    const data = await request("/storage");
    storages.value = (Array.isArray(data) ? data : data?.items || []).filter(
      (item) =>
        item.status === "ENABLED" &&
        !item.readonly &&
        ["LOCAL", "S3"].includes(item.storage_type),
    );
    storage.value =
      storages.value.find((item) => item.is_default)?.id ||
      storages.value[0]?.id ||
      "";
  } catch (reason) {
    error.value = reason.message;
  }
});
function select(event) {
  files.value = Array.from(event.target.files || []).slice(0, 30);
}
function drop(event) {
  if (!busy.value)
    files.value = Array.from(event.dataTransfer.files || []).slice(0, 30);
}
async function upload() {
  busy.value = true;
  error.value = "";
  results.value = [];
  controller = new AbortController();
  try {
    for (const file of files.value) {
      if (file.size > Number(settings.max_upload_size) * 1048576)
        throw new Error(`${file.name} 超过单文件大小限制`);
      current.value = file.name;
      progress.value = 0;
      const record = await uploadShare(
        file,
        {
          storage_config_id: storage.value,
          path: session.basicPath || "/",
          expires_in: hours.value,
          password: password.value,
          remark: remark.value,
          max_views: Number(maxViews.value),
        },
        (value) => (progress.value = value),
        controller.signal,
      );
      results.value.push({
        name: file.name,
        url: shareURL("file", record.slug),
      });
    }
    files.value = [];
  } catch (reason) {
    error.value = reason.message;
  } finally {
    busy.value = false;
    controller = null;
  }
}
async function copy(value) {
  try {
    await navigator.clipboard.writeText(value);
  } catch {
    error.value = "复制失败，请手动复制链接";
  }
}
</script>
<template>
  <div class="page-heading">
    <p class="eyebrow">上传一次，分享一个链接</p>
    <h1>分享你的文件</h1>
    <p class="muted">支持文本、图片、视频和音频预览，其他文件可直接下载。</p>
  </div>
  <section class="panel">
    <div v-if="!allowed" class="notice">
      上传需要文件分享权限。<RouterLink to="/admin/login">登录</RouterLink>
    </div>
    <form @submit.prevent="upload">
      <label class="drop-zone" @dragover.prevent @drop.prevent="drop"
        ><input
          type="file"
          multiple
          :disabled="busy || !allowed"
          @change="select"
        /><strong>选择文件，或拖到这里</strong
        ><span class="muted"
          >每次最多 30 个文件，单文件不超过
          {{ settings.max_upload_size }} MiB</span
        ></label
      >
      <ul v-if="files.length" class="file-queue">
        <li v-for="(file, index) in files" :key="index">
          <span>{{ file.name }}</span
          ><small>{{ (file.size / 1048576).toFixed(2) }} MiB</small>
        </li>
      </ul>
      <div class="form-row">
        <label
          >保存位置<select v-model="storage" required>
            <option v-for="item in storages" :key="item.id" :value="item.id">
              {{ item.name }} · {{ item.storage_type }}
            </option>
          </select></label
        ><label
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
        >
      </div>
      <div class="form-row">
        <label
          >访问密码<input
            v-model="password"
            type="password"
            placeholder="可选"
            autocomplete="new-password"
            maxlength="72" /></label
        ><label
          >最多查看次数<input
            v-model.number="maxViews"
            type="number"
            min="0"
            placeholder="0 为不限"
        /></label>
      </div>
      <label
        >备注<input v-model="remark" maxlength="4000" placeholder="可选"
      /></label>
      <p v-if="error" class="error" role="alert">{{ error }}</p>
      <div v-if="busy" class="upload-progress">
        <span
          >{{ current }} · {{ progress }}%{{
            progress === 100 ? "，正在保存…" : ""
          }}</span
        ><progress :value="progress" max="100"></progress>
      </div>
      <div class="actions">
        <button v-if="busy" type="button" @click="controller?.abort()">
          取消上传</button
        ><button
          class="primary"
          :disabled="busy || !allowed || !storage || !files.length"
        >
          {{ busy ? "正在上传…" : "上传并创建分享" }}
        </button>
      </div>
    </form>
  </section>
  <section v-if="results.length" class="panel result">
    <h2>分享链接</h2>
    <div v-for="item in results" :key="item.url" class="share-result">
      <strong>{{ item.name }}</strong>
      <div class="copy-row">
        <input :value="item.url" readonly /><button @click="copy(item.url)">
          复制</button
        ><a :href="item.url" target="_blank" rel="noopener noreferrer">打开</a>
      </div>
    </div>
  </section>
</template>
