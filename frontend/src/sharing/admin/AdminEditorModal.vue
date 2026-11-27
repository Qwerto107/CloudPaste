<script setup>
import { defineAsyncComponent } from "vue";
import { NModal } from "naive-ui";

const MarkdownEditor = defineAsyncComponent(
  () => import("../MarkdownEditor.vue"),
);

defineProps({
  show: { type: Boolean, required: true },
  panel: { type: String, required: true },
  title: { type: String, required: true },
  editID: { type: String, default: "" },
  busy: { type: Boolean, default: false },
  error: { type: String, default: "" },
  values: { type: Object, required: true },
  permissions: { type: Array, required: true },
  selectedPermissions: { type: Array, required: true },
  storageChoices: { type: Array, required: true },
  customKeyVisible: { type: Boolean, default: false },
});

const emit = defineEmits([
  "close",
  "save",
  "update:selectedPermissions",
  "update:customKeyVisible",
]);
</script>

<template>
  <NModal
    :show="show"
    preset="card"
    class="management-dialog"
    :title="
      (editID ? '编辑' : '新增') +
      title.replace('密钥与权限', '密钥').replace('管理', '')
    "
    :mask-closable="!busy"
    :closable="false"
    :auto-focus="false"
    role="dialog"
    :style="{ width: 'min(560px, calc(100vw - 32px))' }"
    @update:show="
      (next) => {
        if (!next && !busy) emit('close');
      }
    "
  >
    <form class="management-dialog-form" @submit.prevent="emit('save')">
      <p v-if="error" class="error" role="alert">{{ error }}</p>
      <template v-if="panel === 'storage'">
        <div class="form-row">
          <label
            >名称<UiInput
              v-model="values.name"
              required
              maxlength="100" /></label
          ><label
            >类型<UiSelect
              v-model="values.storage_type"
              :disabled="!!editID"
              aria-label="存储类型"
            >
              <option>LOCAL</option>
              <option>S3</option>
            </UiSelect></label
          >
        </div>
        <label v-if="values.storage_type === 'LOCAL'"
          >本地根目录<UiInput
            v-model="values.root_path"
            placeholder="/files"
            required /></label
        ><template v-else>
          <label
            >Endpoint（AWS 可留空）<UiInput
              v-model="values.endpoint_url"
              type="url"
              placeholder="https://s3.example.com"
          /></label>
          <div class="form-row">
            <label
              >Bucket<UiInput v-model="values.bucket_name" required /></label
            ><label
              >Region<UiInput v-model="values.region" placeholder="us-east-1"
            /></label>
          </div>
          <div class="form-row">
            <label
              >Access Key<UiInput
                v-model="values.access_key_id"
                autocomplete="off"
                :placeholder="editID ? '留空保留现有凭据' : ''"
                :required="!editID" /></label
            ><label
              >Secret Key<UiInput
                v-model="values.secret_access_key"
                type="password"
                autocomplete="new-password"
                :placeholder="editID ? '留空保留现有凭据' : ''"
                :required="!editID"
            /></label>
          </div>
          <label class="check"
            ><UiCheckbox
              v-model="values.force_path_style"
              type="checkbox"
            />使用 Path Style</label
          > </template
        ><label class="check"
          ><UiCheckbox
            v-model="values.is_public"
            type="checkbox"
          />允许有文件权限的 API Key 使用（仍受存储 ACL 限制）</label
        ><label class="check"
          ><UiCheckbox
            v-model="values.readonly"
            type="checkbox"
          />只读存储</label
        ><label
          >状态<UiSelect v-model="values.status" aria-label="存储状态">
            <option>ENABLED</option>
            <option>DISABLED</option>
          </UiSelect></label
        ><label>备注<UiInput v-model="values.remark" /></label>
      </template>
      <template v-else-if="panel === 'keys'">
        <label
          >名称<UiInput v-model="values.name" required maxlength="100"
        /></label>
        <div v-if="!editID">
          <label for="custom-key">自定义密钥</label>
          <div class="copy-row">
            <UiInput
              id="custom-key"
              v-model="values.custom_key"
              :type="customKeyVisible ? 'text' : 'password'"
              autocomplete="new-password"
              placeholder="留空自动生成"
              aria-describedby="custom-key-hint"
            />
            <UiButton
              type="button"
              :aria-pressed="customKeyVisible"
              @click="emit('update:customKeyVisible', !customKeyVisible)"
            >
              <span v-if="customKeyVisible">隐藏密钥</span>
              <span v-else>显示密钥</span>
            </UiButton>
          </div>
          <small id="custom-key-hint" class="muted">
            可选，不限制长度和字符，留空自动生成。建议使用随机组合。
          </small>
        </div>
        <UiFieldset>
          <legend>权限</legend>
          <label v-for="entry in permissions" :key="entry.bit" class="check"
            ><UiCheckbox
              :model-value="selectedPermissions"
              type="checkbox"
              :value="entry.bit"
              @update:model-value="emit('update:selectedPermissions', $event)"
            />{{ entry.label }}</label
          >
        </UiFieldset>
        <UiFieldset :disabled="busy">
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
              <UiCheckbox
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
        </UiFieldset>
        <label
          >授权路径<UiInput v-model="values.basic_path" placeholder="/"
        /></label>
        <div class="expiry-field">
          <label for="key-expires-at">有效期</label>
          <div class="copy-row">
            <UiDateTime
              id="key-expires-at"
              v-model="values.expires_at"
              type="datetime-local"
              :disabled="busy"
              aria-describedby="key-expiry-help"
            />
            <UiButton
              type="button"
              :disabled="busy || !values.expires_at"
              @click="values.expires_at = null"
            >
              清空
            </UiButton>
          </div>
          <p id="key-expiry-help" class="muted small">
            按本地时间选择，清空表示不过期。
          </p>
        </div>
        <label class="check"
          ><UiCheckbox
            v-model="values.is_enable"
            type="checkbox"
          />启用密钥</label
        >
      </template>
      <template v-else>
        <label v-if="panel === 'texts'"
          >标题<UiInput v-model="values.title" maxlength="200" /></label
        ><label v-if="panel === 'files'"
          >文件名称<UiInput v-model="values.filename" required
        /></label>
        <!-- 编辑器工具栏含按钮，不能包在 label 中，否则点击正文会触发加粗。 -->
        <div v-if="panel === 'texts'" class="markdown-field">
          <span class="field-label">文本内容</span>
          <MarkdownEditor v-model="values.content" :disabled="busy" />
        </div>
        <label
          >备注<UiInput v-model="values.remark" maxlength="4000"
        /></label>
        <div class="form-row">
          <label
            >访问密码<UiInput
              v-model="values.password"
              type="text"
              autocomplete="new-password"
              maxlength="72"
              :placeholder="
                values.password_available === false
                  ? '原密码不可恢复，输入新密码'
                  : '未设置，直接输入即可修改'
              " /></label
          ><label
            >最多查看次数<UiInput
              v-model.number="values.max_views"
              type="number"
              min="0"
          /></label>
        </div>
        <label class="check"
          ><UiCheckbox
            v-model="values.clearPassword"
            type="checkbox"
          />取消现有密码保护</label
        >
        <div class="expiry-field">
          <label for="share-expires-at">有效期</label>
          <div class="copy-row">
            <UiDateTime
              id="share-expires-at"
              v-model="values.expires_at"
              type="datetime-local"
              :disabled="busy"
              aria-describedby="share-expiry-help"
            />
            <UiButton
              type="button"
              :disabled="busy || !values.expires_at"
              @click="values.expires_at = null"
            >
              清空
            </UiButton>
          </div>
          <p id="share-expiry-help" class="muted small">
            按本地时间选择，留空表示不过期。
          </p>
        </div>
      </template>
      <div class="management-dialog-actions">
        <UiButton type="button" :disabled="busy" @click="emit('close')"
          >取消</UiButton
        >
        <UiButton class="primary" :disabled="busy">
          {{ busy ? "正在保存…" : "保存" }}
        </UiButton>
      </div>
    </form>
  </NModal>
</template>
