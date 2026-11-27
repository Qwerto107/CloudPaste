<script setup>
import {
  computed,
  defineAsyncComponent,
  onMounted,
  onBeforeUnmount,
  reactive,
  ref,
  watch,
} from "vue";
import { useRoute } from "vue-router";
import { NModal } from "naive-ui";
import { request } from "./api.js";
import { useConfirmation, useFileDeletionConfirmation } from "./ui/confirm.js";
import { useNotify } from "./ui/notify.js";
import Icon from "./Icon.vue";
import PageIllustration from "./PageIllustration.vue";
import StatValue from "./StatValue.vue";
import AdminAccountPanel from "./admin/AdminAccountPanel.vue";
import AdminKeyCreatedBanner from "./admin/AdminKeyCreatedBanner.vue";
import AdminEditorModal from "./admin/AdminEditorModal.vue";
import AdminTable from "./admin/AdminTable.vue";

const confirmAction = useConfirmation();
const confirmFileDeletion = useFileDeletionConfirmation();
const notify = useNotify();
const MarkdownPreview = defineAsyncComponent(
  () => import("./MarkdownPreview.vue"),
);
const route = useRoute();
const panel = computed(() => route.meta.panel);
const title = computed(
  () =>
    ({
      texts: "文本管理",
      files: "文件管理",
      storage: "存储管理",
      keys: "密钥与权限",
      account: "账号管理",
    })[panel.value],
);
const items = ref([]);
const total = ref(0);
const page = ref(1);
const statusTime = ref(Date.now());
let statusTimer;
const busy = ref(false);
const showSkeleton = ref(false);
let skeletonTimer;
const error = ref("");
const success = ref("");
const savedID = ref("");
const filter = ref("");
const editing = ref(false);
const editID = ref("");
const values = reactive({});
const newlyCreatedKey = ref("");
const newKeyVisible = ref(false);
const customKeyVisible = ref(false);
const revealedKeys = ref({});
const revealingKeyID = ref("");
let keyController;
const previewText = ref(null);
const storageChoices = ref([]);
const permissions = [
  { bit: 1, label: "创建文本分享" },
  { bit: 2, label: "上传并创建文件分享" },
  { bit: 4, label: "管理自己的文本分享" },
  { bit: 8, label: "管理自己的文件分享" },
];
const selectedPermissions = ref([]);
const originalMaxViews = ref(0);
const originalPassword = ref("");
const shareStates = {
  valid: { label: "有效", reason: "", kind: "valid" },
  expired: { label: "已失效", reason: "已过期", kind: "invalid" },
  exhausted: { label: "已失效", reason: "查看次数已用完", kind: "invalid" },
  private: {
    label: "未公开",
    reason: "仅所有者或管理员可访问",
    kind: "private",
  },
  malformed: { label: "已失效", reason: "有效期无效", kind: "invalid" },
};
function shareState(item) {
  // 与分享访问规则一致：到期或达到查看上限即失效，访问密码不影响有效性。
  if (item.expires_at) {
    const expiry = Date.parse(item.expires_at);
    if (!Number.isFinite(expiry)) return shareStates.malformed;
    if (expiry <= statusTime.value) return shareStates.expired;
  }
  if (
    Number(item.max_views) > 0 &&
    Number(item.views) >= Number(item.max_views)
  )
    return shareStates.exhausted;
  if (
    panel.value === "texts" &&
    (item.is_public === 0 || item.is_public === false)
  )
    return shareStates.private;
  return shareStates.valid;
}
const visible = computed(() =>
  items.value.filter((item) =>
    String(item.title || item.filename || item.name || item.slug || "")
      .toLowerCase()
      .includes(filter.value.toLowerCase()),
  ),
);
// 统计卡只使用列表接口已返回的数据：总数来自分页信息，其余按当前页计算。
const stats = computed(() => {
  const list = items.value;
  const count = (test) => list.filter(test).length;
  if (panel.value === "texts" || panel.value === "files") {
    const paged = total.value > list.length;
    const scope = paged ? "本页" : "";
    return [
      {
        label: panel.value === "texts" ? "全部文本" : "全部文件",
        value: total.value,
        icon: panel.value === "texts" ? "text" : "file",
        tone: "a",
      },
      {
        label: scope + "有效",
        value: count((item) => shareState(item).kind === "valid"),
        icon: "check-circle",
        tone: "ok",
      },
      {
        label: scope + "已失效",
        value: count((item) => shareState(item).kind === "invalid"),
        icon: "clock",
        tone: "warn",
      },
      {
        label: scope + "查看次数",
        value: list.reduce((sum, item) => sum + (Number(item.views) || 0), 0),
        icon: "eye",
        tone: "info",
      },
    ];
  }
  if (panel.value === "storage")
    return [
      { label: "存储", value: list.length, icon: "storage", tone: "a" },
      {
        label: "已启用",
        value: count((item) => item.status === "ENABLED"),
        icon: "check-circle",
        tone: "ok",
      },
      {
        label: "只读",
        value: count((item) => item.readonly),
        icon: "lock",
        tone: "warn",
      },
      {
        label: "默认存储",
        value: list.find((item) => item.is_default)?.name || "未设置",
        icon: "star",
        tone: "info",
      },
    ];
  if (panel.value === "keys")
    return [
      { label: "密钥", value: list.length, icon: "key", tone: "a" },
      {
        label: "已启用",
        value: count((item) => item.is_enable),
        icon: "check-circle",
        tone: "ok",
      },
      {
        label: "已停用",
        value: count((item) => !item.is_enable),
        icon: "ban",
        tone: "warn",
      },
      {
        label: "已过期",
        value: count(
          (item) =>
            item.expires_at && Date.parse(item.expires_at) <= statusTime.value,
        ),
        icon: "clock",
        tone: "danger",
      },
    ];
  return [];
});
const endpoint = computed(
  () =>
    ({
      texts: "/pastes",
      files: "/files",
      storage: "/storage",
      keys: "/admin/api-keys",
    })[panel.value],
);

async function load() {
  statusTime.value = Date.now();
  hideKeys();
  busy.value = true;
  clearTimeout(skeletonTimer);
  // 延迟 180ms 才展示骨架屏，避免快速切换管理菜单时闪烁。
  skeletonTimer = setTimeout(() => {
    if (busy.value && !items.value.length) showSkeleton.value = true;
  }, 180);
  error.value = "";
  editing.value = false;
  previewText.value = null;
  try {
    if (panel.value !== "account") {
      const data = await request(
        endpoint.value +
          (panel.value === "texts" || panel.value === "files"
            ? `?limit=30&page=${page.value}`
            : ""),
      );
      items.value = Array.isArray(data)
        ? data
        : data.items || data.results || data.files || [];
      total.value = data.pagination?.total ?? data.total ?? items.value.length;
      if (panel.value === "keys") {
        const storage = await request("/storage");
        storageChoices.value = storage.items || [];
      }
    }
  } catch (reason) {
    error.value = reason.message;
  } finally {
    clearTimeout(skeletonTimer);
    showSkeleton.value = false;
    busy.value = false;
  }
}

async function openEditor(item = null) {
  error.value = "";
  success.value = "";
  newlyCreatedKey.value = "";
  customKeyVisible.value = false;
  for (const key of Object.keys(values)) delete values[key];
  editID.value = item?.id || "";
  try {
    if (panel.value === "texts" && item)
      item = await request(`/pastes/${encodeURIComponent(item.id)}`);
    if (panel.value === "storage")
      Object.assign(
        values,
        {
          name: "",
          storage_type: "LOCAL",
          root_path: "/files",
          endpoint_url: "",
          bucket_name: "",
          region: "us-east-1",
          access_key_id: "",
          secret_access_key: "",
          force_path_style: true,
          is_public: false,
          readonly: false,
          status: "ENABLED",
          remark: "",
        },
        item || {},
      );
    else if (panel.value === "keys") {
      Object.assign(
        values,
        {
          name: "",
          is_enable: true,
          custom_key: "",
          basic_path: "/",
          storage_config_ids: [],
          expires_at: new Date(Date.now() + 365 * 86400000).toISOString(),
        },
        item || {},
      );
      // 接口可能返回 null，复选框始终使用独立数组保存存储授权。
      values.storage_config_ids = [...(item?.storage_config_ids || [])];
      selectedPermissions.value = permissions
        .filter((entry) => (Number(item?.permissions || 0) & entry.bit) !== 0)
        .map((entry) => entry.bit);
    } else
      Object.assign(
        values,
        {
          title: "",
          filename: "",
          content: "",
          remark: "",
          password: "",
          clearPassword: false,
          max_views: 0,
          expires_at: null,
        },
        item || {},
      );
    if (panel.value === "storage")
      for (const key of ["force_path_style", "is_public", "readonly"])
        values[key] = Boolean(values[key]);
    if (panel.value === "keys") values.is_enable = Boolean(values.is_enable);
    originalMaxViews.value = Number(values.max_views || 0);
    if (panel.value === "texts" || panel.value === "files") {
      values.password = item?.password || "";
      originalPassword.value = values.password;
    } else values.password = "";
    editing.value = true;
  } catch (reason) {
    error.value = reason.message;
  }
}

function closeEditor() {
  editing.value = false;
  values.custom_key = "";
}

async function save() {
  busy.value = true;
  error.value = "";
  try {
    let body;
    let target;
    let method = editID.value ? "PUT" : "POST";
    if (panel.value === "storage") {
      body = { ...values };
      if (!body.access_key_id) delete body.access_key_id;
      if (!body.secret_access_key) delete body.secret_access_key;
      target = `/admin/storage${editID.value ? "/" + encodeURIComponent(editID.value) : ""}`;
    } else if (panel.value === "keys") {
      let flags = selectedPermissions.value.reduce(
        (result, bit) => result | bit,
        0,
      );
      // 文件分享包含上传能力；普通 Key 不获得文件浏览和 WebDAV 权限。
      if (flags & 2) flags |= 512;
      body = {
        name: values.name,
        permissions: flags,
        role: "GENERAL",
        is_enable: values.is_enable,
        basic_path: values.basic_path || "/",
        storage_config_ids: values.storage_config_ids || [],
        expires_at: values.expires_at || null,
      };
      // 仅创建时提交完整密钥；编辑权限不会更换已有凭据。
      if (!editID.value) body.custom_key = values.custom_key || "";
      target = `/admin/api-keys${editID.value ? "/" + encodeURIComponent(editID.value) : ""}`;
    } else {
      body = {
        remark: values.remark,
        expires_at: values.expires_at || null,
        clearPassword: values.clearPassword,
      };
      // 改显示名或备注不重置已使用的查看额度。
      if (Number(values.max_views) !== originalMaxViews.value)
        body.max_views = Number(values.max_views);
      // 没改密码不重新加密；旧哈希记录留空也继续保留保护。
      if (values.password !== originalPassword.value)
        body.password = values.password;
      if (panel.value === "texts") {
        // 后缀复用后，旧编辑窗口仍只能更新原记录。
        body.id = editID.value;
        body.title = values.title;
        body.content = values.content;
        target = `/pastes/${encodeURIComponent(values.slug)}`;
      } else {
        body.filename = values.filename;
        target = `/files/${encodeURIComponent(editID.value)}`;
      }
    }
    const result = await request(target, { method, body });
    savedID.value = result?.id || editID.value;
    if (panel.value === "keys" && !editID.value) {
      newlyCreatedKey.value = result.key;
      values.custom_key = "";
    }
    notify("success", "已保存");
    await load();
  } catch (reason) {
    error.value = reason.message;
  } finally {
    busy.value = false;
  }
}

async function remove(item) {
  const name = item.title || item.filename || item.name || item.slug;
  // 文件删除明确选择是否删除原件；关闭确认框不会执行任何操作。
  const deleteMode =
    panel.value === "files"
      ? await confirmFileDeletion(name)
      : await confirmAction(`确定删除“${name}”？`);
  if (!deleteMode) return;
  busy.value = true;
  error.value = "";
  try {
    if (panel.value === "texts" || panel.value === "files")
      await request(`${endpoint.value}/batch-delete`, {
        method: "DELETE",
        body: {
          ids: [item.id],
          ...(panel.value === "files" ? { delete_mode: deleteMode } : {}),
        },
      });
    else
      await request(
        `/admin/${panel.value === "storage" ? "storage" : "api-keys"}/${encodeURIComponent(item.id)}`,
        { method: "DELETE" },
      );
    notify("success", "已删除");
    await load();
  } catch (reason) {
    error.value = reason.message;
    notify("error", reason.message);
  } finally {
    busy.value = false;
  }
}

async function storageAction(item, action) {
  busy.value = true;
  error.value = "";
  try {
    await request(`/admin/storage/${encodeURIComponent(item.id)}/${action}`, {
      method: action === "test" ? "POST" : "PUT",
    });
    notify("success", action === "test" ? "连接测试通过" : "默认存储已更新");
    await load();
  } catch (reason) {
    error.value = reason.message;
    notify("error", reason.message);
  } finally {
    busy.value = false;
  }
}

async function textPreview(item) {
  try {
    const record = await request(`/pastes/${encodeURIComponent(item.id)}`);
    previewText.value = record.content;
  } catch (reason) {
    error.value = reason.message;
  }
}

async function copyKey() {
  try {
    await navigator.clipboard.writeText(newlyCreatedKey.value);
    notify("success", "密钥已复制");
  } catch {
    error.value = "复制失败，请手动复制";
    notify("error", "复制失败，请手动复制");
  }
}

function hideKeys() {
  // 刷新或离开页面时清理明文，并取消尚未完成的显示请求。
  keyController?.abort();
  keyController = null;
  revealedKeys.value = {};
  revealingKeyID.value = "";
  newKeyVisible.value = false;
  customKeyVisible.value = false;
}

async function toggleKey(item) {
  if (revealedKeys.value[item.id]) {
    delete revealedKeys.value[item.id];
    return;
  }
  const active = new AbortController();
  keyController = active;
  revealingKeyID.value = item.id;
  error.value = "";
  try {
    const data = await request(
      `/admin/api-keys/${encodeURIComponent(item.id)}/reveal`,
      { signal: active.signal },
    );
    if (keyController === active) revealedKeys.value[item.id] = data.key;
  } catch (reason) {
    if (keyController === active && reason.name !== "AbortError")
      error.value = reason.message;
  } finally {
    if (keyController === active) {
      revealingKeyID.value = "";
      keyController = null;
    }
  }
}

function changePage(nextPage) {
  page.value = nextPage;
  load();
}

watch(panel, () => {
  page.value = 1;
  filter.value = "";
  success.value = "";
  newlyCreatedKey.value = "";
  load();
});
onMounted(() => {
  load();
  // 页面停留期间也更新到期状态，不额外请求接口；查看次数随列表刷新更新。
  statusTimer = setInterval(() => {
    statusTime.value = Date.now();
  }, 1000);
});
onBeforeUnmount(() => {
  clearInterval(statusTimer);
  clearTimeout(skeletonTimer);
  hideKeys();
});
</script>

<template>
  <header class="management-heading">
    <div>
      <p class="eyebrow">管理你的分享空间</p>
      <h1>{{ title }}</h1>
    </div>
    <PageIllustration :section="'admin-' + panel" />
  </header>
  <div v-if="stats.length" class="admin-stats" aria-label="统计">
    <div
      v-for="(stat, index) in stats"
      :key="index"
      class="admin-stat"
      :data-tone="stat.tone"
    >
      <span class="admin-stat-icon" aria-hidden="true"
        ><Icon :name="stat.icon"
      /></span>
      <small>{{ stat.label }}</small>
      <StatValue :value="stat.value" />
    </div>
  </div>
  <p v-if="error && !editing" class="error" role="alert">{{ error }}</p>
  <AdminKeyCreatedBanner
    v-if="newlyCreatedKey"
    :created-key="newlyCreatedKey"
    :visible="newKeyVisible"
    @close="newlyCreatedKey = ''"
    @toggle-visible="newKeyVisible = !newKeyVisible"
    @copy="copyKey"
  />
  <AdminAccountPanel
    v-if="panel === 'account'"
    :permissions="permissions"
    :busy="busy"
    @update:busy="busy = $event"
    @error="error = $event"
  />
  <template v-else>
    <AdminEditorModal
      :show="editing"
      :panel="panel"
      :title="title"
      :edit-i-d="editID"
      :busy="busy"
      :error="error"
      :values="values"
      :permissions="permissions"
      :selected-permissions="selectedPermissions"
      :storage-choices="storageChoices"
      :custom-key-visible="customKeyVisible"
      @close="closeEditor"
      @save="save"
      @update:selected-permissions="selectedPermissions = $event"
      @update:custom-key-visible="customKeyVisible = $event"
    />
    <NModal
      :show="previewText !== null"
      preset="card"
      title="文本预览"
      class="management-dialog"
      role="dialog"
      :style="{ width: 'min(760px, calc(100vw - 32px))' }"
      @update:show="(show) => !show && (previewText = null)"
    >
      <div class="management-dialog-preview">
        <MarkdownPreview :content="previewText || ''" />
      </div>
      <template #footer>
        <div class="management-dialog-actions">
          <UiButton type="button" @click="previewText = null">关闭</UiButton>
        </div>
      </template>
    </NModal>
    <section class="panel management-list" :aria-busy="busy">
      <div class="management-toolbar">
        <UiInput
          v-model="filter"
          placeholder="筛选当前列表"
          aria-label="筛选列表"
        /><UiButton
          v-if="panel === 'storage' || panel === 'keys'"
          class="primary management-create"
          :aria-label="panel === 'storage' ? '新增存储' : '新增密钥'"
          @click="openEditor()"
        >
          <Icon name="plus" /><span
            >新增{{ panel === "storage" ? "存储" : "密钥" }}</span
          ></UiButton
        ><RouterLink
          v-else
          class="button primary management-create"
          aria-label="创建分享"
          :to="panel === 'texts' ? '/' : '/upload'"
          ><Icon name="plus" /><span>创建分享</span></RouterLink
        >
      </div>
      <div
        v-if="showSkeleton && !items.length"
        class="management-skeleton"
        role="status"
        aria-label="正在读取"
      >
        <span v-for="row in 3" :key="row"></span>
      </div>
      <p v-else-if="!busy && !visible.length" class="empty muted">
        这里还没有记录。
      </p>
      <AdminTable
        v-else-if="visible.length"
        :panel="panel"
        :items="visible"
        :saved-i-d="savedID"
        :permissions="permissions"
        :revealed-keys="revealedKeys"
        :revealing-key-i-d="revealingKeyID"
        :share-state="shareState"
        :page="page"
        :total="total"
        :busy="busy"
        @toggle-key="toggleKey"
        @preview="textPreview"
        @edit="openEditor"
        @storage-action="storageAction"
        @remove="remove"
        @page-change="changePage"
      />
    </section>
  </template>
</template>
