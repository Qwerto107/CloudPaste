<script setup>
import { onMounted, onBeforeUnmount, ref } from "vue";
import { request } from "./api.js";
import Icon from "./Icon.vue";

const database = ref({ loaded: false });
const file = ref(null);
const input = ref(null);
const busy = ref(false);
const error = ref("");
const success = ref("");
const controller = new AbortController();

function selectFile(event) {
  error.value = "";
  success.value = "";
  const selected = event.target.files?.[0];
  file.value = null;
  if (!selected) return;
  if (
    !/\.bin$/i.test(selected.name) ||
    selected.size < 64 ||
    selected.size > 512 * 1024 * 1024
  ) {
    error.value = "请选择解压后的 DB3 IPv4 BIN 文件，大小不能超过 512 MiB";
    event.target.value = "";
    return;
  }
  file.value = selected;
}

async function loadStatus() {
  busy.value = true;
  try {
    database.value = await request("/admin/ip-location", {
      signal: controller.signal,
    });
  } catch (reason) {
    if (reason.name !== "AbortError") error.value = reason.message;
  } finally {
    busy.value = false;
  }
}

async function update(method) {
  if (busy.value || (method === "PUT" && !file.value)) return;
  busy.value = true;
  error.value = "";
  success.value = "";
  // File 直接作为请求体发送，避免把几十 MiB 的数据库转成文本或整文件缓冲。
  const timeout = AbortSignal.timeout(10 * 60 * 1000);
  const updating = method === "PUT" && database.value.loaded;
  try {
    database.value = await request("/admin/ip-location", {
      method,
      body: method === "PUT" ? file.value : undefined,
      signal: AbortSignal.any([controller.signal, timeout]),
    });
    success.value =
      method === "PUT"
        ? updating
          ? "数据库已更新"
          : "数据库已加载"
        : "数据库已卸载，已停止显示归属地";
    file.value = null;
    if (input.value) input.value.value = "";
  } catch (reason) {
    if (!controller.signal.aborted) error.value = reason.message;
  } finally {
    busy.value = false;
  }
}

onMounted(loadStatus);
onBeforeUnmount(() => controller.abort());
</script>

<template>
  <section class="panel settings-card ip-location-settings">
    <header class="settings-card-heading">
      <span class="settings-card-icon"><Icon name="globe" /></span>
      <div>
        <h2>IP 归属地</h2>
        <p class="muted small">
          全站共用的离线数据库，支持 IPv4 国家、省份查询。
        </p>
      </div>
      <span
        class="ip-location-status"
        :class="{ loaded: database.loaded }"
        role="status"
      >
        <span class="ip-location-status-dot"></span
        >{{ database.loaded ? "已加载" : "未加载" }}
      </span>
    </header>
    <div class="ip-location-body">
      <div class="ip-location-source">
        <p class="muted small">下载并解压 DB3 IPv4 BIN 文件，最大 512 MiB。</p>
        <a
          class="ip-location-download small"
          href="https://lite.ip2location.com/database/db3-ip-country-region-city"
          target="_blank"
          rel="noopener noreferrer"
          ><Icon name="link" />下载免费数据库</a
        >
      </div>
      <!-- 隐藏原生文件控件，以同一套按钮和文件摘要呈现，避免全局输入框样式撑满整行。 -->
      <div class="ip-location-picker">
        <span class="ip-location-file-icon"><Icon name="file" /></span>
        <div class="ip-location-file-info">
          <strong :title="file?.name">{{
            file?.name || "尚未选择数据库"
          }}</strong>
          <span class="muted small">{{
            file
              ? `${(file.size / 1024 / 1024).toFixed(1)} MiB · 等待加载`
              : "请选择本地 .BIN 文件"
          }}</span>
        </div>
        <UiButton
          type="button"
          icon="folder"
          :disabled="busy"
          @click="input?.click()"
          >{{ file ? "重新选择" : "选择文件" }}</UiButton
        >
        <input
          ref="input"
          class="ip-location-file"
          hidden
          type="file"
          accept=".bin,.BIN"
          :disabled="busy"
          aria-label="选择 DB3 IPv4 数据库"
          @change="selectFile"
        />
      </div>
      <div class="ip-location-controls">
        <p class="muted small">
          <template v-if="database.loaded"
            >当前数据 {{ database.date }} ·
            {{ (database.size / 1024 / 1024).toFixed(1) }} MiB</template
          >
          <template v-else>未加载数据库，相关页面不显示归属地。</template>
        </p>
        <div class="ip-location-buttons">
          <UiButton
            type="button"
            class="primary"
            :disabled="busy || !file"
            @click="update('PUT')"
            >{{
              busy ? "正在处理…" : database.loaded ? "更新数据库" : "加载数据库"
            }}</UiButton
          >
          <UiButton
            v-if="database.loaded"
            type="button"
            class="quiet danger"
            :disabled="busy"
            @click="update('DELETE')"
            >卸载数据库</UiButton
          >
        </div>
      </div>
      <p v-if="error" class="error" role="alert">{{ error }}</p>
      <p v-if="success" class="ip-location-success small" role="status">
        {{ success }}
      </p>
      <footer class="ip-location-notes muted small">
        <p>手动更新 · 加载后立即生效 · 重启保留 · IPv6 不显示位置</p>
      </footer>
    </div>
  </section>
</template>

<style scoped>
.ip-location-settings {
  margin-top: 1rem;
}
.ip-location-body {
  display: grid;
  gap: 16px;
  padding: 20px 24px;
}
.ip-location-body p {
  margin: 0;
}
.ip-location-status {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  flex-shrink: 0;
  margin-left: auto;
  padding: 4px 10px;
  border: 1px solid var(--line);
  border-radius: 20px;
  color: var(--muted);
  background: var(--surface);
  font-size: 12px;
  white-space: nowrap;
}
.ip-location-status.loaded {
  color: var(--accent);
  background: var(--soft);
}
.ip-location-status-dot {
  width: 6px;
  height: 6px;
  border-radius: 50%;
  background: currentColor;
}
.ip-location-source,
.ip-location-controls {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px 20px;
  flex-wrap: wrap;
}
.ip-location-download {
  display: inline-flex;
  align-items: center;
  gap: 5px;
  white-space: nowrap;
}
.ip-location-picker {
  display: flex;
  align-items: center;
  gap: 12px;
  min-width: 0;
  padding: 12px 16px;
  border: 1px solid var(--line);
  border-radius: 10px;
  background: var(--surface-subtle);
}
.ip-location-file-icon {
  display: grid;
  place-items: center;
  width: 36px;
  height: 36px;
  flex-shrink: 0;
  color: var(--muted);
}
.ip-location-file-info {
  display: grid;
  gap: 3px;
  min-width: 0;
  flex: 1;
}
.ip-location-file-info strong {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  font-size: 14px;
  font-weight: 500;
}
.ip-location-file {
  display: none;
}
.ip-location-buttons {
  display: flex;
  gap: 10px;
  margin-left: auto;
}
.ip-location-notes {
  display: grid;
  gap: 5px;
  padding-top: 14px;
  border-top: 1px solid var(--line);
  line-height: 1.6;
}
.ip-location-success {
  color: var(--accent);
}
@media (max-width: 600px) {
  .ip-location-body {
    padding: 16px;
  }
  .ip-location-settings .settings-card-heading {
    flex-wrap: wrap;
  }
  .ip-location-settings .settings-card-heading > div {
    flex: 1;
    min-width: 160px;
  }
  .ip-location-picker {
    gap: 8px;
    padding: 12px;
  }
  .ip-location-file-icon {
    display: none;
  }
  .ip-location-buttons {
    flex-wrap: wrap;
  }
}
</style>
