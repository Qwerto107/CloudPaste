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
import { useRoute, useRouter } from "vue-router";
import { request, shareURL } from "./api.js";
import { session, clearSession } from "./session.js";
const MarkdownPreview = defineAsyncComponent(
  () => import("./MarkdownPreview.vue"),
);
const route = useRoute();
const router = useRouter();
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
const busy = ref(false);
const error = ref("");
const success = ref("");
const filter = ref("");
const editing = ref(false);
const editID = ref("");
const values = reactive({});
const keyExpiryInput = computed({
  get() {
    if (!values.expires_at) return "";
    const date = new Date(values.expires_at);
    if (Number.isNaN(date.getTime())) return "";
    // 选择器显示本地时间，提交时转换为 UTC，避免有效期偏移。
    const localTime = new Date(
      date.getTime() - date.getTimezoneOffset() * 60000,
    );
    return localTime.toISOString().slice(0, 16);
  },
  set(value) {
    if (!value) {
      values.expires_at = null;
      return;
    }
    values.expires_at = new Date(value).toISOString();
  },
});
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
const visible = computed(() =>
  items.value.filter((item) =>
    String(item.title || item.filename || item.name || item.slug || "")
      .toLowerCase()
      .includes(filter.value.toLowerCase()),
  ),
);
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
  hideKeys();
  busy.value = true;
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
    values.password = "";
    editing.value = true;
  } catch (reason) {
    error.value = reason.message;
  }
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
      if (values.password) body.password = values.password;
      if (panel.value === "texts") {
        body.title = values.title;
        body.content = values.content;
        target = `/pastes/${encodeURIComponent(values.slug)}`;
      } else {
        body.filename = values.filename;
        target = `/files/${encodeURIComponent(editID.value)}`;
      }
    }
    const result = await request(target, { method, body });
    if (panel.value === "keys" && !editID.value) {
      newlyCreatedKey.value = result.key;
      values.custom_key = "";
    }
    success.value = "已保存";
    await load();
  } catch (reason) {
    error.value = reason.message;
  } finally {
    busy.value = false;
  }
}

async function remove(item) {
  if (
    !window.confirm(
      `确定删除“${item.title || item.filename || item.name || item.slug}”？${panel.value === "files" ? "删除分享记录不会删除存储中的原文件。" : ""}`,
    )
  )
    return;
  busy.value = true;
  error.value = "";
  try {
    if (panel.value === "texts" || panel.value === "files")
      await request(`${endpoint.value}/batch-delete`, {
        method: "DELETE",
        body: { ids: [item.id] },
      });
    else
      await request(
        `/admin/${panel.value === "storage" ? "storage" : "api-keys"}/${encodeURIComponent(item.id)}`,
        { method: "DELETE" },
      );
    success.value = "已删除";
    await load();
  } catch (reason) {
    error.value = reason.message;
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
    success.value = action === "test" ? "连接测试通过" : "默认存储已更新";
    await load();
  } catch (reason) {
    error.value = reason.message;
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
const currentPassword = ref("");
const newPassword = ref("");
const newUsername = ref(session.name);
async function changePassword() {
  busy.value = true;
  error.value = "";
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
    error.value = reason.message;
  } finally {
    busy.value = false;
  }
}
async function copyKey() {
  try {
    await navigator.clipboard.writeText(newlyCreatedKey.value);
    success.value = "密钥已复制";
  } catch {
    error.value = "复制失败，请手动复制";
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
watch(panel, () => {
  page.value = 1;
  filter.value = "";
  success.value = "";
  newlyCreatedKey.value = "";
  load();
});
onMounted(load);
onBeforeUnmount(hideKeys);
</script>
<template>
  <div class="management-heading">
    <div>
      <p class="eyebrow">管理你的分享空间</p>
      <h1>{{ title }}</h1>
    </div>
    <button v-if="endpoint" @click="load" :disabled="busy">刷新</button>
  </div>
  <p v-if="error" class="error" role="alert">{{ error }}</p>
  <p v-if="success" class="success" role="status">{{ success }}</p>
  <section v-if="newlyCreatedKey" class="panel result">
    <h2>保存新密钥</h2>
    <p class="muted">请妥善保存密钥，也可在下方列表中切换显示。</p>
    <div class="copy-row">
      <input
        :value="newlyCreatedKey"
        :type="newKeyVisible ? 'text' : 'password'"
        aria-label="新密钥"
        readonly
      />
      <button
        :aria-pressed="newKeyVisible"
        @click="newKeyVisible = !newKeyVisible"
      >
        <span v-if="newKeyVisible">隐藏密钥</span>
        <span v-else>显示密钥</span>
      </button>
      <button @click="copyKey">复制</button>
    </div>
    <button class="quiet" @click="newlyCreatedKey = ''">关闭</button>
  </section>
  <section v-if="panel === 'account'" class="panel">
    <h2>{{ session.name || "当前账号" }}</h2>
    <p class="muted">
      {{ session.type === "admin" ? "管理员" : "API Key 用户" }}
    </p>
    <template v-if="session.type === 'admin'"
      ><form @submit.prevent="changePassword">
        <label
          >账号<input
            v-model="newUsername"
            required
            autocomplete="username" /></label
        ><label
          >当前密码<input
            v-model="currentPassword"
            type="password"
            required
            autocomplete="current-password" /></label
        ><label
          >新密码<input
            v-model="newPassword"
            type="password"
            minlength="12"
            maxlength="72"
            required
            autocomplete="new-password" /></label
        ><button class="primary" :disabled="busy">更新账号并重新登录</button>
      </form></template
    ><template v-else
      ><p>当前权限：</p>
      <ul>
        <li
          v-for="entry in permissions.filter(
            (entry) => (session.permissions & entry.bit) !== 0,
          )"
          :key="entry.bit"
        >
          {{ entry.label }}
        </li>
      </ul></template
    >
  </section>
  <template v-else>
    <section v-if="editing" class="panel">
      <div class="editor-heading">
        <h2>{{ editID ? "编辑" : "新增" }}{{ title }}</h2>
        <button
          class="quiet"
          @click="
            editing = false;
            values.custom_key = '';
          "
        >
          取消
        </button>
      </div>
      <form @submit.prevent="save">
        <template v-if="panel === 'storage'"
          ><div class="form-row">
            <label
              >名称<input
                v-model="values.name"
                required
                maxlength="100" /></label
            ><label
              >类型<select v-model="values.storage_type" :disabled="!!editID">
                <option>LOCAL</option>
                <option>S3</option>
              </select></label
            >
          </div>
          <label v-if="values.storage_type === 'LOCAL'"
            >本地根目录<input
              v-model="values.root_path"
              placeholder="/files"
              required /></label
          ><template v-else
            ><label
              >Endpoint（AWS 可留空）<input
                v-model="values.endpoint_url"
                type="url"
                placeholder="https://s3.example.com"
            /></label>
            <div class="form-row">
              <label
                >Bucket<input v-model="values.bucket_name" required /></label
              ><label
                >Region<input v-model="values.region" placeholder="us-east-1"
              /></label>
            </div>
            <div class="form-row">
              <label
                >Access Key<input
                  v-model="values.access_key_id"
                  autocomplete="off"
                  :placeholder="editID ? '留空保留现有凭据' : ''"
                  :required="!editID" /></label
              ><label
                >Secret Key<input
                  v-model="values.secret_access_key"
                  type="password"
                  autocomplete="new-password"
                  :placeholder="editID ? '留空保留现有凭据' : ''"
                  :required="!editID"
              /></label>
            </div>
            <label class="check"
              ><input v-model="values.force_path_style" type="checkbox" />使用
              Path Style</label
            ></template
          ><label class="check"
            ><input
              v-model="values.is_public"
              type="checkbox"
            />允许有文件权限的 API Key 使用（仍受存储 ACL 限制）</label
          ><label class="check"
            ><input v-model="values.readonly" type="checkbox" />只读存储</label
          ><label
            >状态<select v-model="values.status">
              <option>ENABLED</option>
              <option>DISABLED</option>
            </select></label
          ><label>备注<input v-model="values.remark" /></label
        ></template>
        <template v-else-if="panel === 'keys'"
          ><label
            >名称<input v-model="values.name" required maxlength="100"
          /></label>
          <div v-if="!editID">
            <label for="custom-key">自定义密钥</label>
            <div class="copy-row">
              <input
                id="custom-key"
                v-model="values.custom_key"
                :type="customKeyVisible ? 'text' : 'password'"
                autocomplete="new-password"
                placeholder="留空自动生成"
                aria-describedby="custom-key-hint"
              />
              <button
                type="button"
                :aria-pressed="customKeyVisible"
                @click="customKeyVisible = !customKeyVisible"
              >
                <span v-if="customKeyVisible">隐藏密钥</span>
                <span v-else>显示密钥</span>
              </button>
            </div>
            <small id="custom-key-hint" class="muted">
              可选，不限制长度和字符，留空自动生成。建议使用随机组合。
            </small>
          </div>
          <fieldset>
            <legend>权限</legend>
            <label v-for="entry in permissions" :key="entry.bit" class="check"
              ><input
                v-model="selectedPermissions"
                type="checkbox"
                :value="entry.bit"
              />{{ entry.label }}</label
            >
          </fieldset>
          <fieldset :disabled="busy">
            <legend>允许使用的存储</legend>
            <div v-if="storageChoices.length" class="storage-options">
              <label
                v-for="item in storageChoices"
                :key="item.id"
                class="check"
                :class="{
                  selected: values.storage_config_ids.includes(item.id),
                }"
              >
                <input
                  v-model="values.storage_config_ids"
                  type="checkbox"
                  :value="item.id"
                />
                {{ item.name }}
                <small class="muted">{{ item.storage_type }}</small>
              </label>
            </div>
            <p v-else class="muted small">暂无存储，请先在存储管理中添加。</p>
            <p class="muted small">
              点击勾选或取消，不选表示允许使用全部公开存储。
            </p>
          </fieldset>
          <label
            >授权路径<input v-model="values.basic_path" placeholder="/"
          /></label>
          <div class="expiry-field">
            <label for="key-expires-at">有效期</label>
            <div class="copy-row">
              <input
                id="key-expires-at"
                v-model="keyExpiryInput"
                type="datetime-local"
                :disabled="busy"
                aria-describedby="key-expiry-help"
              />
              <button
                type="button"
                :disabled="busy || !values.expires_at"
                @click="values.expires_at = null"
              >
                清空
              </button>
            </div>
            <p id="key-expiry-help" class="muted small">
              按本地时间选择，清空表示不过期。
            </p>
          </div>
          <label class="check"
            ><input v-model="values.is_enable" type="checkbox" />启用密钥</label
          ></template
        >
        <template v-else
          ><label v-if="panel === 'texts'"
            >标题<input v-model="values.title" maxlength="200" /></label
          ><label v-if="panel === 'files'"
            >文件名称<input v-model="values.filename" required /></label
          ><label v-if="panel === 'texts'"
            >文本内容<textarea
              v-model="values.content"
              rows="12"
              required
            ></textarea></label
          ><label>备注<input v-model="values.remark" maxlength="4000" /></label>
          <div class="form-row">
            <label
              >新访问密码<input
                v-model="values.password"
                type="password"
                autocomplete="new-password"
                maxlength="72"
                placeholder="留空保留现有密码" /></label
            ><label
              >最多查看次数<input
                v-model.number="values.max_views"
                type="number"
                min="0"
            /></label>
          </div>
          <label class="check"
            ><input
              v-model="values.clearPassword"
              type="checkbox"
            />取消现有密码保护</label
          ><label
            >有效期（ISO 时间，留空不过期）<input
              v-model="values.expires_at" /></label
        ></template>
        <button class="primary" :disabled="busy">
          {{ busy ? "正在保存…" : "保存" }}
        </button>
      </form>
    </section>
    <section v-if="previewText !== null" class="panel">
      <div class="editor-heading">
        <h2>文本预览</h2>
        <button @click="previewText = null">关闭</button>
      </div>
      <MarkdownPreview :content="previewText" />
    </section>
    <section class="panel">
      <div class="management-toolbar">
        <input
          v-model="filter"
          placeholder="筛选当前列表"
          aria-label="筛选列表"
        /><button
          v-if="panel === 'storage' || panel === 'keys'"
          class="primary"
          @click="openEditor()"
        >
          新增{{ panel === "storage" ? "存储" : "密钥" }}</button
        ><RouterLink
          v-else
          class="button primary"
          :to="panel === 'texts' ? '/' : '/upload'"
          >创建分享</RouterLink
        >
      </div>
      <p v-if="busy && !items.length" class="empty">正在读取…</p>
      <p v-else-if="!visible.length" class="empty muted">这里还没有记录。</p>
      <div v-else class="table-scroll">
        <table>
          <thead>
            <tr>
              <th>名称</th>
              <th>
                {{
                  panel === "storage"
                    ? "类型"
                    : panel === "keys"
                      ? "状态 / 权限"
                      : "查看次数 / 有效期"
                }}
              </th>
              <th>操作</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="item in visible" :key="item.id">
              <td>
                <strong>{{
                  item.title || item.filename || item.name || item.slug
                }}</strong
                ><small v-if="item.slug" class="muted">{{ item.slug }}</small
                ><small v-if="panel === 'keys'" class="key-value">
                  <template v-if="revealedKeys[item.id]">{{
                    revealedKeys[item.id]
                  }}</template>
                  <template v-else>{{ item.key }}</template>
                </small>
              </td>
              <td v-if="panel === 'storage'">
                {{ item.storage_type }}
                <span v-if="item.is_default" class="badge">默认</span
                ><small
                  >{{
                    ["LOCAL", "S3"].includes(item.storage_type)
                      ? item.readonly
                        ? "只读"
                        : "可上传"
                      : "已移除的存储类型"
                  }}
                  · {{ item.status }}</small
                >
              </td>
              <td v-else-if="panel === 'keys'">
                {{ item.is_enable ? "启用" : "停用"
                }}<small>{{
                  permissions
                    .filter((entry) => (item.permissions & entry.bit) !== 0)
                    .map((entry) => entry.label)
                    .join("、") || "未授权"
                }}</small>
              </td>
              <td v-else>
                {{ item.views }} / {{ item.max_views || "不限"
                }}<small>{{
                  item.expires_at
                    ? new Date(item.expires_at).toLocaleString()
                    : "不过期"
                }}</small>
              </td>
              <td>
                <div class="row-actions">
                  <button
                    v-if="panel === 'keys'"
                    :disabled="!!revealingKeyID"
                    :aria-pressed="!!revealedKeys[item.id]"
                    @click="toggleKey(item)"
                  >
                    <span v-if="revealingKeyID === item.id">正在读取…</span>
                    <span v-else-if="revealedKeys[item.id]">隐藏密钥</span>
                    <span v-else>显示密钥</span>
                  </button>
                  <a
                    v-if="item.slug"
                    :href="
                      shareURL(panel === 'texts' ? 'paste' : 'file', item.slug)
                    "
                    target="_blank"
                    rel="noopener noreferrer"
                    >打开分享</a
                  ><button v-if="panel === 'texts'" @click="textPreview(item)">
                    预览</button
                  ><button
                    v-if="
                      panel !== 'storage' ||
                      ['LOCAL', 'S3'].includes(item.storage_type)
                    "
                    @click="openEditor(item)"
                  >
                    编辑</button
                  ><template
                    v-if="
                      panel === 'storage' &&
                      ['LOCAL', 'S3'].includes(item.storage_type)
                    "
                    ><button @click="storageAction(item, 'test')">测试</button
                    ><button
                      v-if="!item.is_default"
                      @click="storageAction(item, 'set-default')"
                    >
                      设为默认
                    </button></template
                  ><button class="danger" @click="remove(item)">删除</button>
                </div>
              </td>
            </tr>
          </tbody>
        </table>
      </div>
      <div
        v-if="(panel === 'texts' || panel === 'files') && total > 30"
        class="pagination"
      >
        <button
          :disabled="page <= 1 || busy"
          @click="
            page--;
            load();
          "
        >
          上一页</button
        ><span>第 {{ page }} 页，共 {{ total }} 条</span
        ><button
          :disabled="page * 30 >= total || busy"
          @click="
            page++;
            load();
          "
        >
          下一页
        </button>
      </div>
    </section>
  </template>
</template>
