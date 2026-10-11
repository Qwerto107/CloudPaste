<script setup>
import { computed, ref, reactive, watch, onBeforeUnmount } from "vue";
import { useRoute, onBeforeRouteLeave, onBeforeRouteUpdate } from "vue-router";
import { request } from "./api.js";
import { loadSettings } from "./settings.js";
import { fields, sections } from "./settings-fields.js";
import Icon from "./Icon.vue";

const route = useRoute();
const panel = computed(() => route.meta.panel);
const groups = { global: 1, preview: 2, site: 4 };
const tabs = [
  { key: "global", label: "常规", icon: "settings" },
  { key: "preview", label: "文件预览", icon: "file" },
  { key: "site", label: "站点外观", icon: "cloud" },
];
const definitions = ref([]);
const values = reactive({});
const saved = ref({});
const loading = ref(false);
const saving = ref(false);
const error = ref("");
const success = ref("");
let controller;
const editable = computed(() =>
  definitions.value.filter((item) => item.flags !== 2),
);
const dirty = computed(() =>
  editable.value.some(
    (item) => String(values[item.key]) !== String(saved.value[item.key]),
  ),
);
const cards = computed(() => {
  const listed = new Set();
  const result = (sections[panel.value] || [])
    .map((section) => ({
      ...section,
      items: section.keys
        .map((key) => {
          listed.add(key);
          return definitions.value.find((item) => item.key === key);
        })
        .filter(Boolean),
    }))
    .filter((section) => section.items.length);
  // 保留服务端新增的设置项，避免前端分组暂未更新时静默丢失入口。
  const extra = definitions.value.filter((item) => !listed.has(item.key));
  if (extra.length)
    result.push({ title: "其他设置", icon: "settings", items: extra });
  return result;
});
function field(item) {
  return fields[item.key] || { label: item.description || "其他选项" };
}
function reset() {
  Object.assign(values, saved.value);
  success.value = "";
  error.value = "";
}
async function load() {
  controller?.abort();
  const active = (controller = new AbortController());
  loading.value = true;
  definitions.value = [];
  success.value = "";
  error.value = "";
  try {
    const data = await request(`/admin/settings?group=${groups[panel.value]}`, {
      signal: active.signal,
    });
    if (controller !== active) return;
    for (const key of Object.keys(values)) delete values[key];
    for (const item of data)
      values[item.key] =
        item.type === "bool" ? item.value === "true" : item.value;
    definitions.value = data;
    saved.value = { ...values };
  } catch (reason) {
    if (controller === active && reason.name !== "AbortError")
      error.value = reason.message;
  } finally {
    if (controller === active) loading.value = false;
  }
}
async function save() {
  if (saving.value || loading.value || !dirty.value) return;
  saving.value = true;
  error.value = "";
  success.value = "";
  const body = Object.fromEntries(
    editable.value.map((item) => [item.key, values[item.key]]),
  );
  try {
    await request(`/admin/settings/group/${groups[panel.value]}`, {
      method: "PUT",
      body,
    });
    saved.value = { ...values };
    await loadSettings(true);
    success.value = "设置已保存";
  } catch (reason) {
    error.value = reason.message;
  } finally {
    saving.value = false;
  }
}
// 切换页签或离开页面前提醒未保存修改；保存中暂不允许切换，防止状态错位。
function canLeave() {
  return (
    !saving.value &&
    (!dirty.value || window.confirm("有未保存的修改，确定放弃并离开吗？"))
  );
}
onBeforeRouteLeave(canLeave);
onBeforeRouteUpdate(canLeave);
function beforeUnload(event) {
  if (dirty.value || saving.value) {
    event.preventDefault();
    event.returnValue = "";
  }
}
window.addEventListener("beforeunload", beforeUnload);
onBeforeUnmount(() => {
  controller?.abort();
  window.removeEventListener("beforeunload", beforeUnload);
});
watch(panel, load, { immediate: true });
</script>
<template>
  <div class="settings-heading">
    <h1>系统设置</h1>
    <p class="muted">管理分享规则、文件预览与站点外观。</p>
  </div>
  <nav class="settings-tabs" aria-label="设置分类">
    <RouterLink
      v-for="tab in tabs"
      :key="tab.key"
      :to="'/admin/settings/' + tab.key"
      ><Icon :name="tab.icon" />{{ tab.label }}</RouterLink
    >
  </nav>
  <p v-if="error" class="error" role="alert">{{ error }}</p>
  <div v-if="loading" class="panel empty" role="status">正在加载设置…</div>
  <button v-else-if="!definitions.length" @click="load">重新加载</button>
  <form v-else class="settings-form" @submit.prevent="save">
    <fieldset class="settings-fields" :disabled="saving">
      <component
        :is="card.advanced ? 'details' : 'section'"
        v-for="card in cards"
        :key="card.title"
        class="panel settings-card"
      >
        <component
          :is="card.advanced ? 'summary' : 'header'"
          class="settings-card-heading"
        >
          <span class="settings-card-icon"><Icon :name="card.icon" /></span>
          <div>
            <h2>{{ card.title }}</h2>
            <p class="muted small">{{ card.description }}</p>
          </div>
          <span v-if="card.advanced" class="advanced-indicator"
            >展开 / 收起</span
          >
        </component>
        <div class="settings-card-body">
          <div
            v-for="item in card.items"
            :key="item.key"
            class="setting-row"
            :class="{
              'setting-row-wide':
                item.type === 'textarea' || field(item).multiline,
            }"
          >
            <div class="setting-description">
              <label :for="'setting-' + item.key" :id="'label-' + item.key"
                >{{ field(item).label
                }}<span v-if="item.flags === 2" class="badge"
                  >固定策略</span
                ></label
              >
              <p :id="'help-' + item.key">{{ field(item).help }}</p>
            </div>
            <div class="setting-control">
              <span v-if="item.flags === 2" class="setting-readonly"
                >{{
                  item.type === "bool"
                    ? values[item.key]
                      ? "已开启"
                      : "已关闭"
                    : values[item.key]
                }}
                {{ field(item).unit }}</span
              >
              <label
                v-else-if="item.type === 'bool'"
                class="setting-toggle"
                :for="'setting-' + item.key"
              >
                <span class="switch-state" aria-hidden="true">{{
                  values[item.key] ? "已开启" : "已关闭"
                }}</span>
                <input
                  :id="'setting-' + item.key"
                  v-model="values[item.key]"
                  class="setting-switch"
                  type="checkbox"
                  role="switch"
                  :aria-describedby="'help-' + item.key"
                />
              </label>
              <textarea
                v-else-if="item.type === 'textarea' || field(item).multiline"
                :id="'setting-' + item.key"
                v-model="values[item.key]"
                :rows="field(item).rows || 4"
                :class="{ 'setting-code': field(item).code }"
                :spellcheck="!field(item).code && !field(item).multiline"
                :placeholder="field(item).placeholder"
                :aria-describedby="'help-' + item.key"
              ></textarea>
              <div
                v-else
                class="setting-input"
                :class="{ 'with-unit': field(item).unit }"
              >
                <input
                  :id="'setting-' + item.key"
                  v-model="values[item.key]"
                  :type="item.type === 'number' ? 'number' : 'text'"
                  :min="field(item).min"
                  :max="field(item).max"
                  :step="item.type === 'number' ? 1 : undefined"
                  :required="item.type === 'number'"
                  :inputmode="item.type === 'number' ? 'numeric' : undefined"
                  autocomplete="off"
                  :placeholder="field(item).placeholder"
                  :aria-describedby="'help-' + item.key"
                />
                <span v-if="field(item).unit" class="input-unit">{{
                  field(item).unit
                }}</span>
              </div>
            </div>
          </div>
        </div>
      </component>
    </fieldset>
    <div class="settings-savebar">
      <span role="status" :class="dirty ? 'unsaved-state' : 'muted'">{{
        saving
          ? "正在保存…"
          : dirty
            ? "有未保存的修改"
            : success || "所有修改已保存"
      }}</span>
      <div>
        <button type="button" :disabled="!dirty || saving" @click="reset">
          撤销修改</button
        ><button class="primary" :disabled="!dirty || saving">
          {{ saving ? "正在保存…" : "保存设置" }}
        </button>
      </div>
    </div>
  </form>
</template>
