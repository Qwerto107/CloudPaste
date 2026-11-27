<script setup>
import { shareURL } from "../api.js";

defineProps({
  panel: { type: String, required: true },
  items: { type: Array, required: true },
  savedID: { type: String, default: "" },
  permissions: { type: Array, required: true },
  revealedKeys: { type: Object, required: true },
  revealingKeyID: { type: String, default: "" },
  shareState: { type: Function, required: true },
  page: { type: Number, required: true },
  total: { type: Number, required: true },
  busy: { type: Boolean, default: false },
});

const emit = defineEmits([
  "toggle-key",
  "preview",
  "edit",
  "storage-action",
  "remove",
  "page-change",
]);
</script>

<template>
  <div class="table-scroll">
    <table>
      <thead>
        <tr>
          <th>名称</th>
          <th v-if="panel === 'texts' || panel === 'files'">状态</th>
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
        <tr
          v-for="item in items"
          :key="item.id"
          :class="{ 'management-row-saved': item.id === savedID }"
        >
          <td class="management-name">
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
          <td
            v-if="panel === 'texts' || panel === 'files'"
            class="share-status-cell"
          >
            <span class="share-status" :class="shareState(item).kind">{{
              shareState(item).label
            }}</span>
            <small v-if="shareState(item).reason" class="muted">{{
              shareState(item).reason
            }}</small>
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
          <td class="management-row-actions">
            <div class="row-actions">
              <UiButton
                v-if="panel === 'keys'"
                size="small"
                :disabled="!!revealingKeyID"
                :aria-pressed="!!revealedKeys[item.id]"
                @click="emit('toggle-key', item)"
              >
                <span v-if="revealingKeyID === item.id">正在读取…</span>
                <span v-else-if="revealedKeys[item.id]">隐藏密钥</span>
                <span v-else>显示密钥</span>
              </UiButton>
              <!-- 链接保留新窗口打开语义，与同行操作使用相同的小尺寸按钮。 -->
              <UiButton
                v-if="item.slug"
                tag="a"
                type="button"
                size="small"
                :href="shareURL(panel === 'texts' ? 'paste' : 'file', item.slug)"
                target="_blank"
                rel="noopener noreferrer"
                >打开分享</UiButton
              ><UiButton
                v-if="panel === 'texts'"
                size="small"
                @click="emit('preview', item)"
              >
                预览</UiButton
              ><UiButton
                v-if="
                  panel !== 'storage' ||
                  ['LOCAL', 'S3'].includes(item.storage_type)
                "
                size="small"
                @click="emit('edit', item)"
              >
                编辑</UiButton
              ><template
                v-if="
                  panel === 'storage' &&
                  ['LOCAL', 'S3'].includes(item.storage_type)
                "
                ><UiButton
                  size="small"
                  @click="emit('storage-action', item, 'test')"
                  >测试</UiButton
                ><UiButton
                  v-if="!item.is_default"
                  size="small"
                  @click="emit('storage-action', item, 'set-default')"
                >
                  设为默认
                </UiButton></template
              ><UiButton
                class="danger"
                size="small"
                @click="emit('remove', item)"
                >删除</UiButton
              >
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
    <UiButton
      size="small"
      :disabled="page <= 1 || busy"
      @click="emit('page-change', page - 1)"
    >
      上一页</UiButton
    ><span>第 {{ page }} 页，共 {{ total }} 条</span
    ><UiButton
      size="small"
      :disabled="page * 30 >= total || busy"
      @click="emit('page-change', page + 1)"
    >
      下一页
    </UiButton>
  </div>
</template>
