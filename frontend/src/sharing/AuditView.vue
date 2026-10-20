<script setup>
import IPLocation from "./IPLocation.vue";
import { computed, ref, reactive, watch, onBeforeUnmount } from "vue";
import { useRoute, useRouter } from "vue-router";
import { request } from "./api.js";
import { auditModules } from "./audit-modules.js";
import { fields } from "./settings-fields.js";
import { auditDateBoundary } from "./audit-date.js";

const route = useRoute();
const router = useRouter();
const modules = Object.fromEntries(
  auditModules.map(({ key, label }) => [key, label]),
);
const selectedModule = computed(() => route.meta.auditModule || "");
const pageTitle = computed(() => modules[selectedModule.value] || "全部日志");
const identities = {
  admin: "管理员",
  apikey: "API Key",
  guest: "访客",
  unknown: "未认证",
};
const results = { success: "成功", failure: "失败", cancelled: "已取消" };
const filters = reactive({
  actor_type: "",
  actor: "",
  result: "",
  from: "",
  to: "",
});
const items = ref([]),
  page = ref(1),
  pageSize = ref(50),
  hasMore = ref(false),
  busy = ref(false),
  error = ref(""),
  days = ref(30),
  enabled = ref(false),
  expandedId = ref(null);
let controller;
async function load(reset = false) {
  if (reset) page.value = 1;
  expandedId.value = null;
  controller?.abort();
  const active = (controller = new AbortController());
  busy.value = true;
  error.value = "";
  try {
    if (filters.from && filters.to && filters.from > filters.to)
      throw new Error("开始日期不能晚于结束日期");
    const query = new URLSearchParams({
      limit: String(pageSize.value),
      offset: String((page.value - 1) * pageSize.value),
    });
    // 侧栏分类决定模块；查询表单只负责该分类内的其他筛选条件。
    if (selectedModule.value) query.set("module", selectedModule.value);
    for (const [key, value] of Object.entries(filters)) {
      if (value)
        query.set(
          key,
          key === "from" || key === "to"
            ? auditDateBoundary(value, key === "to")
            : value,
        );
    }
    const data = await request(`/admin/operation-logs?${query}`, {
      signal: active.signal,
    });
    if (controller !== active) return;
    items.value = data.items;
    hasMore.value = data.has_more;
    days.value = data.retention_days;
    enabled.value = data.enabled;
  } catch (reason) {
    if (controller === active && reason.name !== "AbortError")
      error.value = reason.message;
  } finally {
    if (controller === active) busy.value = false;
  }
}
function turn(delta) {
  page.value += delta;
  load();
}
function changePageSize(value) {
  pageSize.value = value;
  load(true);
}
function changes(item) {
  try {
    return Object.entries(JSON.parse(item.details || "{}"));
  } catch {
    return [];
  }
}
function detailName(key) {
  return (
    fields[key]?.label ||
    {
      state: "状态",
      mode: "传输方式",
      bytes: "已传字节",
      size: "文件大小",
      name: "文件名",
      image_type: "图片类型",
      started_at: "发起时间",
      folder_name: "文件夹",
      relative_path: "相对路径",
      directory: "空目录",
      text: "发送内容",
      // 来源 IP 与直连 IP 可能不同，端口只能与浏览器上报的直连地址配对。
      sender_ip: "发送方来源 IP",
      receiver_ip: "接收方来源 IP",
      p2p_local: "接收方 IP:端口（点对点）",
      p2p_remote: "发送方 IP:端口（点对点）",
      p2p_reporter: "直连地址来源",
      round: "下载次数",
      download_ips: "下载请求 IP",
      room: "连接编号",
      creator_name: "创建方设备名称",
      creator_type: "创建方身份",
      creator_account: "创建方账号",
      creator_ip: "创建方来源 IP",
      joiner_name: "加入方设备名称",
      joiner_type: "加入方身份",
      joiner_account: "加入方账号",
      joiner_ip: "加入方来源 IP",
      requested_at: "申请时间",
      approved_at: "同意时间",
      join_state: "授权状态",
      relay_only: "仅允许中继",
      device: "操作设备",
      peer_device: "对端设备",
      location: "操作方 IP 归属地",
      peer_location: "对端 IP 归属地",
      loaded: "数据库已加载",
      database_date: "数据日期",
      database_size: "数据库大小",
      delete_files: "同时删除原文件",
      merged_requests: "已合并的同类请求",
    }[key] ||
    key
  );
}
function detailValue(key, value) {
  if (key === "creator_type" || key === "joiner_type")
    return identities[value] || value;
  if (key === "join_state")
    return { pending: "等待同意", approved: "已同意加入" }[value] || value;
  if (key === "device" || key === "peer_device")
    return value?.summary || "未提供";
  if (key === "mode")
    return (
      { p2p: "点对点", relay: "实时中继", tool: "下载工具中继" }[value] || value
    );
  if (key === "state")
    return (
      {
        active: "传输中",
        complete: "已完成",
        cancelled: "已取消",
        failed: "失败",
        download_complete: "本轮下载完成",
      }[value] || value
    );
  if (key === "download_ips" && Array.isArray(value))
    return value.join("、") || "—";
  if (key === "loaded" || key === "relay_only") return value ? "是" : "否";
  if (
    ["size", "bytes", "database_size"].includes(key) &&
    Number.isSafeInteger(value)
  ) {
    const units = ["B", "KiB", "MiB", "GiB", "TiB"];
    const index =
      value > 0 ? Math.min(4, Math.floor(Math.log(value) / Math.log(1024))) : 0;
    return `${(value / 1024 ** index).toFixed(index ? 1 : 0)} ${units[index]} (${value} 字节)`;
  }
  if (["started_at", "requested_at", "approved_at"].includes(key))
    return new Date(value).toLocaleString();
  return typeof value === "object" && value !== null
    ? JSON.stringify(value)
    : String(value);
}
function mergedCount(item) {
  try {
    const count = JSON.parse(item.details || "{}").merged_requests;
    return Number.isSafeInteger(count) && count > 0 ? count : 0;
  } catch {
    return 0;
  }
}
watch(selectedModule, () => load(true), { immediate: true });
onBeforeUnmount(() => controller?.abort());
</script>

<template>
  <div class="settings-heading audit-heading">
    <div>
      <h1>{{ pageTitle }}</h1>
      <p class="muted">
        {{
          enabled ? "正在记录操作日志" : "操作日志已关闭，仍可查看已有记录"
        }}；保留 {{ days }} 天。{{ enabled ? "新记录可能稍有延迟。" : "" }}
      </p>
    </div>
    <!-- 日志开关和清理统一放到系统设置，列表页只保留查看与查询。 -->
    <UiButton
      type="button"
      class="quiet"
      @click="router.push('/admin/settings/logs')"
      >日志设置</UiButton
    >
  </div>
  <!-- 分类放在内容区，避免侧栏因展开多个模块而变得过长。 -->
  <nav class="audit-categories" aria-label="日志分类">
    <RouterLink to="/admin/operation-logs">全部日志</RouterLink>
    <RouterLink
      v-for="module in auditModules"
      :key="module.key"
      :to="`/admin/operation-logs/${module.key}`"
      >{{ module.label }}</RouterLink
    >
  </nav>
  <form class="panel audit-filters" @submit.prevent="load(true)">
    <label
      >身份<UiSelect v-model="filters.actor_type" aria-label="身份"
        ><option value="">全部身份</option>
        <option v-for="(label, key) in identities" :key="key" :value="key">
          {{ label }}
        </option></UiSelect
      ></label
    >
    <label
      >操作人<UiInput
        v-model="filters.actor"
        placeholder="名称或完整 ID"
        maxlength="200"
    /></label>
    <label
      >结果<UiSelect v-model="filters.result" aria-label="结果"
        ><option value="">全部结果</option>
        <option v-for="(label, key) in results" :key="key" :value="key">
          {{ label }}
        </option></UiSelect
      ></label
    >
    <label
      >开始日期<UiInput
        v-model="filters.from"
        type="date"
        :max="filters.to || undefined"
    /></label>
    <label
      >结束日期<UiInput
        v-model="filters.to"
        type="date"
        :min="filters.from || undefined"
    /></label>
    <UiButton class="primary" :disabled="busy">{{
      busy ? "查询中…" : "查询"
    }}</UiButton>
  </form>
  <p v-if="error" class="error" role="alert">{{ error }}</p>
  <section class="panel audit-list" :aria-busy="busy">
    <div class="audit-table-scroll">
      <table class="audit-table">
        <thead>
          <tr>
            <th scope="col">时间</th>
            <th scope="col">模块</th>
            <th scope="col">操作</th>
            <th scope="col">操作人</th>
            <th scope="col">IP</th>
            <th scope="col">结果</th>
            <th scope="col">详情</th>
          </tr>
        </thead>
        <tbody>
          <tr v-if="!items.length">
            <td colspan="7" class="audit-empty muted">
              {{ busy ? "正在加载…" : "暂无符合条件的操作记录" }}
            </td>
          </tr>
          <template v-for="item in items" :key="item.id">
            <tr class="audit-row">
              <td>
                <time class="audit-time">{{
                  new Date(item.time).toLocaleString()
                }}</time>
              </td>
              <td>
                <span class="audit-module">{{
                  modules[item.module] || item.module
                }}</span>
              </td>
              <td>
                <strong class="audit-action">{{ item.action }}</strong>
                <span
                  v-if="mergedCount(item)"
                  class="audit-merged"
                  :title="`另有 ${mergedCount(item)} 次同类请求`"
                  >+{{ mergedCount(item) }}</span
                >
              </td>
              <td>
                <!-- 列表只显示名称，访客不会出现“访客 · 访客”等重复文本。 -->
                <span class="audit-actor">{{
                  item.actor_name || identities[item.actor_type] || "—"
                }}</span>
              </td>
              <td class="audit-ip">{{ item.ip || "—" }}</td>
              <td>
                <span
                  class="audit-result"
                  :class="
                    item.result === 'success'
                      ? 'success'
                      : item.result === 'failure'
                        ? 'failure'
                        : 'other'
                  "
                  >{{ results[item.result] || item.result }}</span
                >
              </td>
              <td>
                <button
                  type="button"
                  class="audit-detail-toggle"
                  :aria-expanded="expandedId === item.id"
                  :aria-controls="`audit-detail-${item.id}`"
                  @click="expandedId = expandedId === item.id ? null : item.id"
                >
                  {{ expandedId === item.id ? "收起" : "查看" }}
                </button>
              </td>
            </tr>
            <tr v-if="expandedId === item.id" :id="`audit-detail-${item.id}`">
              <td colspan="7" class="audit-detail">
                <div class="audit-detail-meta">
                  <span class="audit-detail-tag">{{
                    identities[item.actor_type] || item.actor_type
                  }}</span>
                  <strong>{{ item.actor_name || "—" }}</strong>
                  <span class="audit-detail-tag">{{ item.method }}</span>
                  <span class="audit-detail-endpoint">{{ item.endpoint }}</span>
                  <span v-if="item.status" class="audit-detail-tag"
                    >HTTP {{ item.status }}</span
                  >
                  <span class="audit-detail-duration"
                    >耗时 {{ item.duration_ms }} ms</span
                  >
                </div>
                <div
                  v-if="item.actor_id || item.target"
                  class="audit-detail-ids"
                >
                  <div v-if="item.actor_id">
                    <span>操作人 ID</span><code>{{ item.actor_id }}</code>
                  </div>
                  <div v-if="item.target">
                    <span>对象</span><code>{{ item.target }}</code>
                  </div>
                </div>
                <!-- 日志包含外部输入，始终按纯文本展示，禁止渲染为 HTML 或 Markdown。 -->
                <dl v-if="changes(item).length" class="audit-detail-fields">
                  <div
                    v-for="[key, value] in changes(item)"
                    :key="key"
                    :class="{
                      'audit-detail-wide': [
                        'text',
                        'name',
                        'download_ips',
                        'p2p_local',
                        'p2p_remote',
                        'device',
                        'peer_device',
                        'location',
                        'peer_location',
                      ].includes(key),
                    }"
                  >
                    <dt>{{ detailName(key) }}</dt>
                    <dd
                      v-if="
                        value && typeof value === 'object' && 'before' in value
                      "
                    >
                      {{ value.before }} → {{ value.after }}
                    </dd>
                    <dd
                      v-else-if="!['location', 'peer_location'].includes(key)"
                      :class="{
                        'audit-message': key === 'text',
                        'audit-detail-value-tag': [
                          'state',
                          'mode',
                          'join_state',
                        ].includes(key),
                      }"
                    >
                      {{ detailValue(key, value) }}
                    </dd>
                    <dd v-else><IPLocation :location="value" /></dd>
                  </div>
                </dl>
              </td>
            </tr>
          </template>
        </tbody>
      </table>
    </div>
  </section>
  <div class="pagination audit-pagination">
    <label class="audit-page-size"
      >每页
      <UiSelect
        :model-value="pageSize"
        :disabled="busy"
        aria-label="每页条数"
        @update:model-value="changePageSize"
      >
        <option v-for="count in [20, 50, 100, 200]" :key="count" :value="count">
          {{ count }} 条
        </option>
      </UiSelect>
    </label>
    <UiButton :disabled="busy || page === 1" @click="turn(-1)">上一页</UiButton
    ><span>第 {{ page }} 页</span
    ><UiButton :disabled="busy || !hasMore" @click="turn(1)">下一页</UiButton>
  </div>
</template>

<style>
.audit-pagination {
  flex-wrap: wrap;
}
.audit-page-size {
  display: flex;
  align-items: center;
  gap: 8px;
  white-space: nowrap;
  margin: 0;
}
.audit-page-size .n-select {
  width: 100px;
}
.audit-heading {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 16px;
}
.audit-heading > .ui-button {
  flex: none;
}
.audit-categories {
  display: flex;
  gap: 4px;
  overflow-x: auto;
  padding-bottom: 10px;
  margin-bottom: 14px;
  border-bottom: 1px solid var(--line);
}
.audit-categories a {
  flex: none;
  padding: 7px 11px;
  border-radius: 7px;
  color: inherit;
  font-size: 0.875rem;
  text-decoration: none;
  white-space: nowrap;
}
.audit-categories a:hover {
  background: var(--surface-subtle);
}
.audit-categories .router-link-exact-active {
  background: var(--soft);
  color: var(--accent);
  font-weight: 650;
}
.audit-filters {
  display: grid;
  grid-template-columns:
    minmax(105px, 0.8fr) minmax(150px, 1.1fr) minmax(105px, 0.8fr)
    minmax(165px, 1.3fr) minmax(165px, 1.3fr) 96px;
  gap: 10px;
  align-items: end;
  margin-bottom: 16px;
  padding: 1.25rem;
}
.panel.audit-filters label {
  display: grid;
  gap: 6px;
  min-width: 0;
  /* 覆盖通用表单标签的下边距，使查询按钮与输入框底边对齐。 */
  margin-bottom: 0;
}
.audit-filters > .ui-button {
  width: 100%;
}
.audit-table-scroll {
  overflow-x: auto;
}
.audit-table {
  width: 100%;
  border-collapse: collapse;
  font-size: 0.875rem;
}
.audit-table th,
.audit-table td {
  padding: 11px 10px;
  border-bottom: 1px solid var(--line);
  text-align: left;
  vertical-align: top;
  overflow-wrap: anywhere;
}
.audit-table th {
  color: var(--muted);
  font-weight: 600;
  white-space: nowrap;
  background: var(--surface-subtle);
}
.audit-row:hover td {
  background: var(--surface-subtle);
}
.audit-action {
  font-weight: 600;
}
.audit-merged {
  margin-left: 5px;
  color: var(--muted);
  font-size: 0.75rem;
  white-space: nowrap;
}
.audit-time,
.audit-ip {
  color: var(--muted);
}
.audit-module,
.audit-result {
  display: inline-block;
  padding: 3px 8px;
  border-radius: 6px;
  font-size: 0.8rem;
  line-height: 1.4;
  white-space: nowrap;
}
.audit-module {
  background: var(--surface-subtle);
}
.audit-result.success {
  background: var(--success-bg);
  color: var(--success);
}
.audit-result.failure {
  background: var(--error-bg);
  color: var(--error);
}
.audit-result.other {
  background: var(--surface-subtle);
  color: var(--muted);
}
.audit-table time,
.audit-table td:nth-child(5) {
  white-space: nowrap;
}
.audit-actor {
  display: inline-block;
  max-width: 160px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  vertical-align: bottom;
}
.audit-empty {
  text-align: center !important;
  padding: 24px !important;
}
.audit-detail-toggle {
  border: 0;
  padding: 0;
  background: transparent;
  color: var(--accent);
  cursor: pointer;
  font: inherit;
}
.audit-table td.audit-detail {
  background: var(--surface-subtle);
  padding: 18px 20px;
}
.audit-detail-meta {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 8px 10px;
  padding-bottom: 12px;
  border-bottom: 1px solid var(--line);
}
.audit-detail-meta strong {
  font-weight: 600;
}
.audit-detail-tag,
.audit-detail-value-tag {
  display: inline-block;
  padding: 3px 8px;
  border-radius: 6px;
  background: var(--soft);
  color: var(--accent);
  line-height: 1.4;
  white-space: nowrap;
}
.audit-detail-endpoint,
.audit-detail-duration {
  color: var(--muted);
}
.audit-detail-endpoint {
  overflow-wrap: anywhere;
}
.audit-detail-ids {
  display: grid;
  gap: 6px;
  padding: 12px 0;
}
.audit-detail-ids > div {
  display: flex;
  align-items: baseline;
  gap: 10px;
  min-width: 0;
}
.audit-detail-ids span,
.audit-detail-fields dt {
  color: var(--muted);
  font-size: 0.8rem;
}
.audit-detail-ids span {
  flex: none;
  min-width: 72px;
}
.audit-detail-ids code {
  overflow-wrap: anywhere;
  font: inherit;
}
.audit-detail-fields {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(min(100%, 190px), 1fr));
  gap: 14px 20px;
  margin: 0;
  padding-top: 12px;
}
.audit-detail-fields > div {
  margin: 0;
  min-width: 0;
}
.audit-detail-fields .audit-detail-wide {
  grid-column: 1 / -1;
}
.audit-detail-fields dd {
  margin: 4px 0 0;
  overflow-wrap: anywhere;
}
.audit-message {
  white-space: pre-wrap;
}
@media (max-width: 1250px) {
  .audit-filters {
    grid-template-columns: repeat(3, minmax(0, 1fr));
  }
  .audit-filters > .ui-button {
    width: 120px;
    justify-self: end;
  }
}
@media (max-width: 650px) {
  .audit-heading {
    flex-wrap: wrap;
  }
  .audit-filters {
    grid-template-columns: 1fr;
  }
  .audit-filters {
    padding: 1rem;
  }
  .audit-filters > .ui-button {
    width: 100%;
  }
}
</style>
