<script setup>
import { computed, onMounted, onBeforeUnmount, ref } from "vue";
import { useRoute } from "vue-router";
import { request, uploadShare, shareURL } from "./api.js";
import { hasPermission, session } from "./session.js";
import { settings } from "./settings.js";
import Icon from "./Icon.vue";
import ShareNameField from "./ShareNameField.vue";
import ExpirySelect from "./ExpirySelect.vue";
const route = useRoute();
const files = ref([]);
const storages = ref([]);
const storage = ref("");
const storageLoading = ref(false);
const hours = ref(Number(settings.share_default_expiry_hours ?? 24));
const password = ref("");
const remark = ref("");
const maxViews = ref(0);
const busy = ref(false);
const progress = ref(0);
const current = ref("");
const results = ref([]);
const slug = ref("");
const collectionName = ref("");
const collection = ref(false);
const uploaded = ref([]);
const error = ref("");
let controller;
const allowed = computed(() => hasPermission(2));
const uploadLimit = computed(() => {
  const size = Number(settings.max_upload_size);
  return size >= 1024 ? `${size / 1024} GiB` : `${size} MiB`;
});
// 页面离开时取消传输，服务端将清理未完成的临时文件。
onBeforeUnmount(() => controller?.abort());
onMounted(async () => {
  if (!allowed.value) return;
  storageLoading.value = true;
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
  } finally {
    storageLoading.value = false;
  }
});
function addFiles(selection) {
  if (busy.value || !allowed.value || uploaded.value.length) return;
  const selected = Array.from(selection || []);
  if (!selected.length) return;
  const available = Math.max(0, 30 - files.value.length);
  results.value = [];
  // 多次选择或拖放都追加到当前队列，保留之前已选的文件。
  files.value.push(...selected.slice(0, available).map((file) => ({ file })));
  error.value =
    selected.length > available
      ? `每次最多 30 个文件，本次有 ${selected.length - available} 个文件未加入列表。`
      : "";
  collection.value = files.value.length > 1;
}
function select(event) {
  addFiles(event.target.files);
  // 清空原生选择框，移除后仍可重新选择同一个文件。
  event.target.value = "";
}
function remove(entry) {
  if (busy.value) return;
  // 这里只移除待上传项，不触碰用户本地文件或已上传的分享。
  files.value = files.value.filter((item) => item !== entry);
  if (!uploaded.value.length) collection.value = files.value.length > 1;
}
function drop(event) {
  addFiles(event.dataTransfer.files);
}
async function upload() {
  if (
    busy.value ||
    !allowed.value ||
    (!files.value.length && !uploaded.value.length)
  )
    return;
  busy.value = true;
  error.value = "";
  controller = new AbortController();
  try {
    // 成功文件暂存 ID；失败后只重试剩余文件，全部完成后生成一个合集链接。
    for (const entry of [...files.value]) {
      const { file } = entry;
      if (file.size > Number(settings.max_upload_size) * 1048576)
        throw new Error(`${file.name} 超过单文件大小限制`);
      current.value = file.name;
      progress.value = 0;
      const record = await uploadShare(
        file,
        {
          storage_config_id: storage.value,
          slug: collection.value ? "" : slug.value,
          path: session.basicPath || "/",
          expires_in: hours.value,
          password: password.value,
          remark: remark.value,
          max_views: Number(maxViews.value),
        },
        (value) => (progress.value = value),
        controller.signal,
      );
      if (collection.value) uploaded.value.push(record.id);
      else
        results.value.push({
          name: file.name,
          url: shareURL("file", record.slug),
        });
      files.value = files.value.filter((item) => item !== entry);
    }
    if (collection.value && uploaded.value.length) {
      current.value = "正在生成合集链接";
      const record = await request("/share/collection", {
        method: "POST",
        body: {
          ids: uploaded.value,
          slug: slug.value,
          name: collectionName.value,
          expires_in: hours.value,
          password: password.value,
          remark: remark.value,
          max_views: Number(maxViews.value),
        },
        signal: controller.signal,
      });
      results.value = [
        { name: record.filename, url: shareURL("file", record.slug) },
      ];
      uploaded.value = [];
      collection.value = false;
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
      上传需要文件分享权限。<RouterLink
        :to="{ path: '/login', query: { redirect: route.fullPath } }"
        >使用 API Key 登录</RouterLink
      >
    </div>
    <form @submit.prevent="upload">
      <UiFieldset class="share-fields" :disabled="busy || !allowed">
        <label
          class="drop-zone"
          :class="{
            disabled: busy || !allowed || uploaded.length,
            compact: files.length || uploaded.length,
          }"
          @dragover.prevent
          @drop.prevent="drop"
          ><input
            type="file"
            multiple
            aria-label="选择文件"
            :disabled="busy || !allowed || uploaded.length > 0"
            @change="select"
          /><Icon name="upload" /><strong>{{
            files.length ? "继续添加文件，或拖到这里" : "选择文件，或拖到这里"
          }}</strong
          ><span class="muted"
            >每次最多 30 个文件，单文件不超过 {{ uploadLimit }}</span
          ></label
        >
        <ul v-if="files.length" class="file-queue">
          <li
            v-for="entry in files"
            :key="entry.file"
            class="file-upload-entry"
          >
            <div class="file-upload-heading">
              <span class="file-name"
                ><Icon name="file" />{{ entry.file.name }}</span
              ><span class="file-upload-meta"
                ><small>{{ (entry.file.size / 1048576).toFixed(2) }} MiB</small
                ><UiButton
                  size="small"
                  type="button"
                  :aria-label="'移除 ' + entry.file.name"
                  :disabled="busy"
                  @click="remove(entry)"
                >
                  移除
                </UiButton></span
              >
            </div>
          </li>
        </ul>
        <p v-if="uploaded.length" class="notice small">
          已上传 {{ uploaded.length }} 个文件。
          {{
            files.length
              ? "继续上传剩余文件后生成合集链接；已完成的文件不会重复上传。"
              : "可直接生成合集链接，无需再次上传。"
          }}
        </p>
        <p v-if="collection" class="muted small">
          这些文件将共用一个分享链接，支持文件列表、单独下载和 ZIP 打包下载。
        </p>
        <label v-if="collection">
          合集名称<UiInput
            v-model="collectionName"
            maxlength="255"
            placeholder="文件合集"
          />
        </label>
        <ShareNameField
          v-model="slug"
          id="file-share-name"
          type="file"
          :disabled="busy"
        />
        <div class="form-row">
          <label
            >保存位置<UiSelect
              v-model="storage"
              :disabled="storageLoading || !storages.length"
              required
              aria-label="保存位置"
            >
              <option v-if="storageLoading" value="" disabled>
                正在加载保存位置…
              </option>
              <option v-else-if="!storages.length" value="" disabled>
                暂无可写的保存位置
              </option>
              <option v-for="item in storages" :key="item.id" :value="item.id">
                {{ item.name }} · {{ item.storage_type }}
              </option>
            </UiSelect>
            <small
              v-if="allowed && !storageLoading && !storages.length && !error"
              class="muted"
            >
              没有可用的可写存储。
              <RouterLink v-if="session.type === 'admin'" to="/admin/storage"
                >前往存储管理</RouterLink
              >
              <span v-else>请联系管理员检查存储的只读设置和密钥授权。</span>
            </small>
          </label>
          <ExpirySelect v-model="hours" />
          <label
            >访问密码<UiInput
              v-model="password"
              type="password"
              placeholder="可选"
              autocomplete="new-password"
              maxlength="72"
          /></label>
          <label
            >最多查看次数<UiInput
              v-model.number="maxViews"
              type="number"
              min="0"
              step="1"
              required
            /><small class="muted">0 表示不限次数</small></label
          >
        </div>
        <label
          >备注<UiInput v-model="remark" maxlength="4000" placeholder="可选"
        /></label>
      </UiFieldset>
      <p v-if="error" class="error" role="alert">{{ error }}</p>
      <div v-if="busy" class="upload-progress">
        <span
          >{{ current }} · {{ progress }}%{{
            progress === 100 ? "，正在保存…" : ""
          }}</span
        ><progress :value="progress" max="100"></progress>
      </div>
      <div class="actions">
        <span class="muted small">到期后链接失效，原文件仍保留在存储中。</span>
        <UiButton v-if="busy" type="button" @click="controller?.abort()">
          取消上传</UiButton
        ><UiButton
          class="primary"
          icon="link"
          :disabled="
            busy || !allowed || !storage || (!files.length && !uploaded.length)
          "
        >
          {{
            busy
              ? "正在上传…"
              : uploaded.length && !files.length
                ? "生成合集链接"
                : "上传并创建分享"
          }}
        </UiButton>
      </div>
    </form>
  </section>
  <section v-if="results.length" class="panel result">
    <h2>分享链接</h2>
    <div v-for="item in results" :key="item.url" class="share-result">
      <strong>{{ item.name }}</strong>
      <div class="copy-row">
        <UiInput :value="item.url" readonly /><UiButton @click="copy(item.url)">
          复制</UiButton
        ><a :href="item.url" target="_blank" rel="noopener noreferrer">打开</a>
      </div>
    </div>
  </section>
</template>
