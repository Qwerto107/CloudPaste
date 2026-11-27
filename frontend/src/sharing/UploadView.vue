<script setup>
import { computed, onMounted, onBeforeUnmount, ref, useTemplateRef } from "vue";
import { useRoute } from "vue-router";
import { request, uploadShare, shareURL } from "./api.js";
import { hasPermission, session } from "./session.js";
import { settings } from "./settings.js";
import Icon from "./Icon.vue";
import EmptyIllustration from "./EmptyIllustration.vue";
import PageIllustration from "./PageIllustration.vue";
import ShareNameField from "./ShareNameField.vue";
import ShareAccessFields from "./ShareAccessFields.vue";
import ShareResult from "./ShareResult.vue";
import ExpirySelect from "./ExpirySelect.vue";
import MobileSheet from "./ui/MobileSheet.vue";
import { shareSettingsSummary } from "./share-expiry.js";
import { fileDisplay, formatFileSize } from "./file-display.js";
const route = useRoute();
const picker = useTemplateRef("picker");
const files = ref([]);
const storages = ref([]);
const storage = ref("");
const storageLoading = ref(false);
const hours = ref(Number(settings.share_default_expiry_hours ?? 24));
const password = ref("");
const remark = ref("");
const maxViews = ref(0);
const busy = ref(false);
const results = ref([]);
const slug = ref("");
const collectionName = ref("");
const collection = ref(false);
const uploaded = ref([]);
const error = ref("");
const settingsOpen = ref(false);
const resultOpen = ref(false);
const dropDepth = ref(0);
let fileKey = 0;
let controller;
const allowed = computed(() => hasPermission(2));
const validHours = computed(() => Number.isFinite(hours.value));
const pending = computed(() =>
  files.value.filter((entry) => entry.status !== "done"),
);
const hasCompleted = computed(() => files.value.length > pending.value.length);
const totalSize = computed(() =>
  files.value.reduce((total, entry) => total + entry.file.size, 0),
);
const canUpload = computed(
  () =>
    !busy.value &&
    allowed.value &&
    storage.value &&
    (pending.value.length || uploaded.value.length),
);
const uploadLabel = computed(() =>
  busy.value
    ? "正在上传…"
    : uploaded.value.length && !pending.value.length
      ? "生成合集链接"
      : pending.value.length &&
          (hasCompleted.value ||
            pending.value.some((entry) =>
              ["cancelled", "failed"].includes(entry.status),
            ))
        ? "重试剩余文件"
        : "上传并分享",
);
const uploadIcon = computed(() =>
  busy.value
    ? "upload"
    : uploaded.value.length && !pending.value.length
      ? "link"
      : pending.value.length &&
          (hasCompleted.value ||
            pending.value.some((entry) =>
              ["cancelled", "failed"].includes(entry.status),
            ))
        ? "retry"
        : "upload",
);
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
  if (selected.some((file) => /[\p{Cc}\p{Cf}]/u.test(file.name))) {
    error.value = "文件名包含不支持的控制或格式字符";
    return;
  }
  if (!pending.value.length) {
    files.value = [];
    results.value = [];
    resultOpen.value = false;
  }
  const available = Math.max(0, 30 - files.value.length);
  // 多次选择或拖放都追加到当前队列，保留之前已选的文件。
  files.value.push(
    ...selected.slice(0, available).map((file) => ({
      id: ++fileKey,
      file,
      name: file.name,
      status: "ready",
      progress: 0,
    })),
  );
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
  if (busy.value || entry.status === "done") return;
  // 这里只移除待上传项，不触碰用户本地文件或已上传的分享。
  files.value = files.value.filter((item) => item !== entry);
  if (!uploaded.value.length) collection.value = files.value.length > 1;
}
function drop(event) {
  dropDepth.value = 0;
  addFiles(event.dataTransfer.files);
}
async function upload() {
  if (!canUpload.value || !validHours.value) return;
  busy.value = true;
  error.value = "";
  controller = new AbortController();
  let current;
  try {
    // 已完成项保留在列表；失败或取消后只重试未完成项。
    for (const entry of pending.value) {
      current = entry;
      entry.status = "uploading";
      entry.progress = 0;
      const { name } = entry;
      const original = entry.file;
      const file =
        name === original.name
          ? original
          : new File([original], name, {
              type: original.type,
              lastModified: original.lastModified,
            });
      if (file.size > Number(settings.max_upload_size) * 1048576)
        throw new Error(`${file.name} 超过单文件大小限制`);
      if (
        !name ||
        /[/\\:\p{Cc}\p{Cf}]/u.test(name) ||
        /[. ]$/.test(name) ||
        new TextEncoder().encode(name).length > 255
      )
        throw new Error(`“${name}”不是有效的文件名`);
      const record = await uploadShare(
        file,
        {
          storage_config_id: storage.value,
          slug: collection.value || results.value.length ? "" : slug.value,
          path: session.basicPath || "/",
          expires_in: hours.value,
          password: password.value,
          remark: remark.value,
          max_views: Number(maxViews.value),
        },
        (value) => (entry.progress = value),
        controller.signal,
      );
      if (collection.value) uploaded.value.push(record.id);
      else
        results.value.push({
          title: "文件分享已创建",
          detail: `${file.name} · ${formatFileSize(file.size)}`,
          link: shareURL("file", record.slug),
          password: record.password || "",
        });
      entry.status = "done";
      current = null;
    }
    if (collection.value && uploaded.value.length) {
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
        {
          title: "文件合集已创建",
          detail: `${record.filename} · ${uploaded.value.length} 个文件 · ${formatFileSize(totalSize.value)}${hours.value ? ` · ${new Date(record.expires_at || Date.now() + hours.value * 3600000).toLocaleString("zh-CN", { hour12: false })} 到期` : ""}`,
          link: shareURL("file", record.slug),
          password: record.password || "",
        },
      ];
      uploaded.value = [];
      collection.value = false;
    }
  } catch (reason) {
    if (current)
      current.status = controller.signal.aborted ? "cancelled" : "failed";
    error.value = reason.message;
  } finally {
    busy.value = false;
    controller = null;
    if (results.value.length && !error.value) {
      settingsOpen.value = false;
      resultOpen.value = true;
    }
  }
}
</script>
<template>
  <header class="page-heading hero">
    <div>
      <h1>文件分享</h1>
      <p class="muted">拖入文件即可分享，多个文件自动合并成合集</p>
      <ul class="page-chips">
        <li>单文件 ≤ {{ uploadLimit }}</li>
        <li>单次最多 30 个</li>
        <li>本地 / S3</li>
      </ul>
    </div>
    <PageIllustration section="file" />
  </header>
  <div v-if="!allowed" class="notice share-permission">
    上传需要文件分享权限。<RouterLink
      :to="{ path: '/login', query: { redirect: route.fullPath } }"
      >使用 API Key 登录</RouterLink
    >
  </div>
  <form
    class="share-layout"
    @submit.prevent="upload"
    @invalid.capture="settingsOpen = true"
  >
    <div class="upload-workspace">
      <section
        class="drop-zone"
        :class="{
          disabled: busy || !allowed || uploaded.length,
          'drag-over': dropDepth > 0,
        }"
        @dragenter.prevent="dropDepth++"
        @dragleave.prevent="dropDepth = Math.max(0, dropDepth - 1)"
        @dragover.prevent
        @drop.prevent="drop"
        @click="picker?.click()"
      >
        <input
          ref="picker"
          class="visually-hidden"
          type="file"
          multiple
          tabindex="-1"
          aria-label="选择文件"
          :disabled="busy || !allowed || uploaded.length > 0"
          @change="select"
          @click.stop
        />
        <span class="upload-drop-icon"><Icon name="upload" /></span>
        <h2>拖入文件，或点击选择</h2>
        <p class="muted small">单次最多 30 个 · 单文件 ≤ {{ uploadLimit }}</p>
        <UiButton
          type="button"
          icon="folder"
          :disabled="busy || !allowed || uploaded.length > 0"
          @click.stop="picker?.click()"
          >选择文件</UiButton
        >
      </section>
      <TransitionGroup
        v-if="files.length"
        name="file-queue"
        tag="ul"
        class="file-queue"
      >
        <li
          v-for="entry in files"
          :key="entry.id"
          class="panel file-upload-entry"
          :class="entry.status"
          :data-kind="fileDisplay(entry.name).kind"
        >
          <span class="file-ext" aria-hidden="true">{{
            fileDisplay(entry.name).label
          }}</span>
          <UiInput
            v-if="entry.status !== 'done'"
            v-model="entry.name"
            class="file-rename"
            :disabled="busy"
            maxlength="255"
            :aria-label="'文件名 ' + entry.file.name"
            required
          />
          <strong v-else class="uploaded-name">{{ entry.name }}</strong>
          <small class="file-upload-size">{{
            formatFileSize(entry.file.size)
          }}</small>
          <div class="file-upload-meta">
            <span v-if="entry.status === 'done'" class="page-tag success"
              >已完成</span
            >
            <span
              v-else-if="entry.status === 'uploading'"
              class="page-tag accent"
              >{{ entry.progress }}%</span
            >
            <template v-else>
              <span v-if="entry.status === 'failed'" class="page-tag failure"
                >失败</span
              >
              <span v-else-if="entry.status === 'cancelled'" class="page-tag"
                >已取消</span
              >
              <UiButton
                type="button"
                size="small"
                class="quiet icon-button"
                icon="close"
                :aria-label="'移除 ' + entry.name"
                :disabled="busy"
                @click="remove(entry)"
              />
            </template>
          </div>
          <progress
            v-if="!['ready', 'cancelled'].includes(entry.status)"
            :value="entry.status === 'done' ? 100 : entry.progress"
            max="100"
            :aria-label="entry.name + ' 上传进度'"
          ></progress>
        </li>
      </TransitionGroup>
      <div v-else class="upload-empty muted">
        <EmptyIllustration kind="file" />
        <strong>还没有选择文件</strong>
        <span>拖进上方区域，或点「选择文件」</span>
      </div>
      <p v-if="uploaded.length && !busy" class="notice small">
        已上传 {{ uploaded.length }} 个文件。{{
          pending.length
            ? "继续上传剩余文件后生成合集链接；已完成的文件不会重复上传。"
            : "可直接生成合集链接，无需再次上传。"
        }}
      </p>
      <p v-if="error" class="error share-form-error" role="alert">
        {{ error }}
      </p>
      <UiButton
        v-if="results.length"
        type="button"
        class="mobile-result-link"
        @click="resultOpen = true"
        >查看分享结果</UiButton
      >
    </div>
    <aside class="share-sidebar" aria-label="上传设置与结果">
      <MobileSheet v-model="settingsOpen" title="上传设置">
        <div class="panel share-settings">
          <h2>上传设置</h2>
          <UiFieldset
            class="upload-settings-fields"
            :disabled="busy || !allowed"
          >
            <label
              >存储位置
              <UiSelect
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
                <option
                  v-for="item in storages"
                  :key="item.id"
                  :value="item.id"
                >
                  {{ item.name }} · {{ item.storage_type
                  }}{{ item.is_default ? "（默认）" : "" }}
                </option>
              </UiSelect>
              <small
                v-if="allowed && !storageLoading && !storages.length && !error"
                class="muted"
              >
                没有可用的可写存储。<RouterLink
                  v-if="session.type === 'admin'"
                  to="/admin/storage"
                  >前往存储管理</RouterLink
                ><span v-else>请联系管理员检查存储的只读设置和密钥授权。</span>
              </small>
            </label>
            <label class="switch-row"
              ><span>合并为文件合集</span
              ><input
                v-model="collection"
                class="page-switch"
                role="switch"
                type="checkbox"
                :disabled="uploaded.length > 0"
            /></label>
            <div
              class="field-reveal"
              :class="{ expanded: collection }"
              :inert="!collection"
            >
              <div>
                <UiInput
                  v-model="collectionName"
                  aria-label="合集名称"
                  maxlength="255"
                  placeholder="合集名称"
                  :disabled="!collection"
                />
              </div>
            </div>
            <ShareNameField v-model="slug" id="file-share-name" type="file" />
            <ExpirySelect v-model="hours" />
            <ShareAccessFields
              v-model:password="password"
              v-model:max-views="maxViews"
            />
            <label
              >备注
              <UiInput
                v-model="remark"
                maxlength="4000"
                placeholder="仅自己和管理员可见"
            /></label>
          </UiFieldset>
          <UiButton
            class="primary share-submit"
            :icon="uploadIcon"
            :disabled="!canUpload || !validHours"
            >{{ uploadLabel }}</UiButton
          >
          <UiButton
            v-if="busy"
            type="button"
            class="share-cancel"
            icon="close"
            @click="controller?.abort()"
            >取消上传</UiButton
          >
          <small class="muted share-shortcut">{{
            files.length
              ? `${files.length} 个文件 · ${formatFileSize(totalSize)}`
              : "未选择文件"
          }}</small>
          <p v-if="settingsOpen && error" class="error" role="alert">
            {{ error }}
          </p>
        </div>
        <template #footer>
          <UiButton type="button" @click="settingsOpen = false">完成</UiButton>
          <UiButton
            v-if="busy"
            type="button"
            class="danger"
            icon="close"
            @click="controller?.abort()"
            >取消上传</UiButton
          >
          <UiButton
            v-else
            class="primary"
            :icon="uploadIcon"
            :disabled="!canUpload || !validHours"
            >{{ uploadLabel }}</UiButton
          >
        </template>
      </MobileSheet>
      <MobileSheet v-if="results.length" v-model="resultOpen" title="分享结果">
        <ShareResult v-for="item in results" :key="item.link" v-bind="item" />
      </MobileSheet>
    </aside>
    <div class="mobile-share-actions">
      <button
        type="button"
        class="mobile-settings-summary"
        @click="settingsOpen = true"
      >
        <Icon name="gear" /><span
          ><small>上传设置</small
          >{{ files.length ? `${files.length} 个文件 · ` : ""
          }}{{ shareSettingsSummary(hours, password, maxViews) }}</span
        >
      </button>
      <UiButton v-if="busy" type="button" icon="close" @click="controller?.abort()"
        >取消上传</UiButton
      >
      <UiButton
        v-else
        class="primary"
        :icon="uploadIcon"
        :disabled="!canUpload"
        @click="!validHours && (settingsOpen = true)"
        >{{ uploadLabel }}</UiButton
      >
    </div>
  </form>
</template>
