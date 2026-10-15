<script setup>
import { onBeforeUnmount, reactive, ref } from "vue";
import { onBeforeRouteLeave, onBeforeRouteUpdate } from "vue-router";
import { request } from "./api.js";
import { auditModules } from "./audit-modules.js";
import { auditDateBoundary } from "./audit-date.js";
import { useConfirmation } from "./ui/confirm.js";

const confirmAction = useConfirmation();
const cleanup = reactive({ from: "", to: "", module: "all" });
const cleaning = ref(false);
const confirming = ref(false);
const message = ref("");
const error = ref("");
let controller;

async function clearLogs() {
  if (cleaning.value || confirming.value) return;
  error.value = "";
  message.value = "";
  if (!cleanup.from || !cleanup.to || cleanup.from > cleanup.to) {
    error.value = "请选择有效的开始和结束日期";
    return;
  }
  const moduleName =
    auditModules.find(({ key }) => key === cleanup.module)?.label || "全部模块";
  confirming.value = true;
  let accepted;
  try {
    accepted = await confirmAction(
      `确定永久删除 ${cleanup.from} 至 ${cleanup.to} 的${moduleName}日志吗？此操作无法撤销。`,
      { title: "清理操作日志", positiveText: "确认删除" },
    );
  } finally {
    confirming.value = false;
  }
  if (!accepted) return;

  cleaning.value = true;
  const active = (controller = new AbortController());
  let total = 0;
  try {
    const body = {
      from: auditDateBoundary(cleanup.from),
      to: auditDateBoundary(cleanup.to, true),
      module: cleanup.module === "all" ? "" : cleanup.module,
    };
    // 服务端限制单次删除数量；按同一条件继续调用，不占用业务数据库写锁。
    let hasMore;
    do {
      const result = await request("/admin/operation-logs", {
        method: "DELETE",
        body,
        signal: active.signal,
      });
      total += result.deleted;
      hasMore = result.has_more;
      message.value = `已删除 ${total} 条日志${hasMore ? "，继续清理中…" : ""}`;
    } while (hasMore && !active.signal.aborted);
  } catch (reason) {
    if (reason.name !== "AbortError")
      error.value = `${reason.message}；已删除 ${total} 条，请检查剩余记录`;
  } finally {
    if (controller === active) cleaning.value = false;
  }
}

onBeforeUnmount(() => controller?.abort());
// 分批清理或确认期间暂不切换页面，避免只执行一部分而误以为已经全部清理。
onBeforeRouteLeave(() => !cleaning.value && !confirming.value);
onBeforeRouteUpdate(() => !cleaning.value && !confirming.value);
</script>

<template>
  <section class="panel audit-cleanup-page">
    <h2>清理日志</h2>
    <p class="muted small">
      按日期范围和模块删除操作日志，包含结束日期当天。删除后无法恢复。
    </p>
    <form class="audit-cleanup-form" @submit.prevent="clearLogs">
      <label
        >模块<UiSelect v-model="cleanup.module" aria-label="清理模块">
          <option value="all">全部模块</option>
          <option
            v-for="module in auditModules"
            :key="module.key"
            :value="module.key"
          >
            {{ module.label }}
          </option>
        </UiSelect></label
      >
      <label
        >开始日期<UiInput
          v-model="cleanup.from"
          type="date"
          :max="cleanup.to || undefined"
          required
      /></label>
      <label
        >结束日期<UiInput
          v-model="cleanup.to"
          type="date"
          :min="cleanup.from || undefined"
          required
      /></label>
      <div class="audit-cleanup-actions">
        <RouterLink to="/admin/operation-logs">查看操作日志</RouterLink>
        <UiButton class="danger" :disabled="cleaning || confirming">{{
          cleaning ? "清理中…" : "删除匹配日志"
        }}</UiButton>
      </div>
    </form>
    <p v-if="error" class="error" role="alert">{{ error }}</p>
    <p v-if="message" class="muted" role="status">{{ message }}</p>
  </section>
</template>

<style>
.audit-cleanup-page {
  margin-top: 24px;
  padding: 1.5rem;
}
.audit-cleanup-page h2 {
  margin: 0 0 8px;
  font-size: 1rem;
}
.audit-cleanup-page > p {
  margin: 0 0 20px;
}
.audit-cleanup-form {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 16px;
}
.audit-cleanup-form label {
  display: grid;
  gap: 7px;
  min-width: 0;
}
.audit-cleanup-form label:first-child {
  grid-column: 1 / -1;
}
.audit-cleanup-actions {
  grid-column: 1 / -1;
  display: flex;
  justify-content: flex-end;
  align-items: center;
  gap: 16px;
  margin-top: 8px;
}
.audit-cleanup-actions a {
  color: var(--accent);
}
@media (max-width: 650px) {
  .audit-cleanup-form {
    grid-template-columns: 1fr;
  }
  .audit-cleanup-actions {
    flex-wrap: wrap;
  }
}
</style>
