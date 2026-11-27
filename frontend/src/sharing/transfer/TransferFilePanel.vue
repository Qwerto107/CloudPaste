<script setup>
import Icon from "../Icon.vue";
import EmptyIllustration from "../EmptyIllustration.vue";
import MobileSheet from "../ui/MobileSheet.vue";
import { fileDisplay } from "../file-display.js";

defineProps({
  open: { type: Boolean, default: false },
  connected: { type: Boolean, default: false },
  autoReceive: { type: Boolean, default: false },
  preferredTransport: { type: String, required: true },
  canDirect: { type: Boolean, default: false },
  receiveConcurrency: { type: Number, required: true },
  folderMaxConcurrency: { type: Number, required: true },
  fileGroups: { type: Array, required: true },
  myMember: { type: Number, required: true },
  active: { type: Boolean, default: false },
  folderSavingSupported: { type: Boolean, default: false },
  directButtonTheme: { type: Object, required: true },
  folderStateLabels: { type: Object, required: true },
  size: { type: Function, required: true },
  folderBytes: { type: Function, required: true },
  folderFiles: { type: Function, required: true },
  filePercent: { type: Function, required: true },
  fileStatus: { type: Function, required: true },
});

const emit = defineEmits([
  "update:open",
  "update:autoReceive",
  "update:preferredTransport",
  "set-receive-concurrency",
  "update-folder-concurrency",
  "accept-folder",
  "cancel-folder",
  "remove-folder",
  "remove-task",
  "retry",
  "accept",
  "copy-download-link",
  "send",
]);
</script>

<template>
  <MobileSheet
    :model-value="open"
    title="文件列表"
    @update:model-value="emit('update:open', $event)"
  >
    <section class="panel transfer-file-panel">
      <div class="transfer-section-heading">
        <h2>文件列表</h2>
        <label
          v-if="connected"
          class="check transfer-auto-receive"
          title="仅本次连接有效，按顺序中继下载"
          ><UiCheckbox
            :model-value="autoReceive"
            aria-label="自动接收对方文件"
            @update:model-value="emit('update:autoReceive', $event)"
          />自动接收</label
        >
      </div>
      <div v-if="connected" class="transfer-receive-settings">
        <span class="transfer-receive-pair"
          ><span>接收</span>
          <div class="page-segment" role="group" aria-label="优先下载方式">
            <button
              type="button"
              :aria-pressed="preferredTransport === 'p2p' && canDirect"
              :disabled="!canDirect"
              @click="emit('update:preferredTransport', 'p2p')"
            >
              点对点
            </button>
            <button
              type="button"
              :aria-pressed="preferredTransport === 'relay' || !canDirect"
              @click="emit('update:preferredTransport', 'relay')"
            >
              中继
            </button>
          </div></span
        >
        <span class="transfer-receive-pair"
          ><span>并发</span>
          <div
            class="transfer-stepper"
            title="设置接收文件夹的默认并发数（1–128）"
          >
            <button
              type="button"
              aria-label="减少默认并发数"
              :disabled="receiveConcurrency <= 1"
              @click="emit('set-receive-concurrency', receiveConcurrency - 1)"
            >
              −
            </button>
            <input
              type="number"
              inputmode="numeric"
              min="1"
              :max="folderMaxConcurrency"
              :value="receiveConcurrency"
              aria-label="接收文件夹的默认并发数"
              @change="
                emit('set-receive-concurrency', $event.target.value);
                $event.target.value = receiveConcurrency;
              "
            />
            <button
              type="button"
              aria-label="增加默认并发数"
              :disabled="receiveConcurrency >= folderMaxConcurrency"
              @click="emit('set-receive-concurrency', receiveConcurrency + 1)"
            >
              +
            </button>
          </div></span
        >
      </div>
      <div v-if="!fileGroups.some((group) => group.key === 'current')">
        <h3 class="transfer-list-heading">传输中</h3>
        <div class="muted transfer-file-empty">
          <EmptyIllustration kind="transfer" />
          <strong>暂无传输任务</strong>
          <span>发送或接收的文件会显示在这里</span>
        </div>
      </div>
      <!-- 历史默认展开，原生 details 保留手动收起状态；有效的中继仍属于当前任务。 -->
      <component
        v-for="group in fileGroups"
        :key="group.key"
        :is="group.ended ? 'details' : 'section'"
        :open="group.ended"
        :class="group.ended ? 'transfer-history' : 'transfer-current'"
        :aria-label="group.label"
      >
        <summary v-if="group.ended">
          已结束的传输（{{ group.files.length + group.folders.length }}）
        </summary>
        <h3 v-else class="transfer-list-heading">
          传输中
          <span class="transfer-count">{{
            group.files.length + group.folders.length
          }}</span>
        </h3>
        <div class="transfer-file-list">
          <article
            v-for="folder in group.folders"
            :key="folder.id"
            class="transfer-file transfer-folder"
            :data-folder-id="folder.id"
          >
            <div class="transfer-file-head">
              <Icon name="folder" class="transfer-file-icon" />
              <div class="transfer-file-copy">
                <div class="transfer-file-title">
                  <strong>{{ folder.name }}</strong
                  ><span>{{ size(folder.size) }}</span>
                </div>
                <div class="transfer-file-meta">
                  <span class="muted">{{
                    folder.from === myMember ? "↑ 我发送的" : "↓ 对方发送的"
                  }}</span>
                  <span
                    class="transfer-tag"
                    :class="
                      folder.state === 'complete'
                        ? 'complete'
                        : folder.state === 'active'
                          ? 'active'
                          : 'neutral'
                    "
                    >{{ folderStateLabels[folder.state] }}</span
                  >
                  <span class="muted"
                    >{{ folder.completed }}/{{ folder.count }} 项</span
                  >
                  <span v-if="folder.mode" class="muted">{{
                    folder.mode === "p2p" ? "点对点" : "实时中继"
                  }}</span>
                </div>
                <time
                  class="muted transfer-folder-time"
                  :datetime="folder.sent_at"
                  >{{ folder.sentLabel }}</time
                >
              </div>
            </div>
            <template v-if="folder.state === 'active'">
              <progress
                :value="folder.size ? folderBytes(folder) : folder.completed"
                :max="folder.size || folder.count"
                :aria-label="`${folder.name} 文件夹传输进度`"
              />
              <p class="muted transfer-progress-text">
                <span
                  >{{ size(folderBytes(folder)) }} / {{ size(folder.size) }} ·
                  {{ size(folder.speed) }}/s</span
                ><span>并发上限 {{ folder.concurrency }} 个</span>
              </p>
              <details
                v-if="folderFiles(folder).length"
                class="transfer-folder-files"
                open
              >
                <summary>
                  文件进度（{{ folderFiles(folder).length }}）
                </summary>
                <ul>
                  <li
                    v-for="file in folderFiles(folder)"
                    :key="file.id"
                    class="transfer-folder-entry"
                  >
                    <div class="transfer-folder-entry-head">
                      <span :title="file.folder.path">{{
                        file.folder.path
                      }}</span>
                      <span class="muted">{{
                        file.state === "offered"
                          ? "准备中"
                          : `${filePercent(file)}%`
                      }}</span>
                    </div>
                    <progress
                      :value="file.bytes"
                      :max="file.size || 1"
                      :aria-label="`${file.folder.path} 传输进度`"
                    />
                    <p class="muted transfer-folder-entry-meta">
                      {{ size(file.bytes) }} / {{ size(file.size) }}
                    </p>
                  </li>
                </ul>
              </details>
              <p v-else class="muted transfer-folder-path">正在准备下一项…</p>
            </template>
            <details
              v-if="
                ['offered', 'active'].includes(folder.state) &&
                folder.from !== myMember
              "
              class="transfer-folder-options"
            >
              <summary>接收选项</summary>
              <label class="transfer-folder-concurrency">
                <span>并发数</span>
                <span class="transfer-stepper">
                  <button
                    type="button"
                    aria-label="减少并发数"
                    :disabled="
                      folder.updatingConcurrency || folder.concurrencyDraft <= 1
                    "
                    @click="
                      folder.concurrencyDraft = Math.max(
                        1,
                        Number(folder.concurrencyDraft || 1) - 1,
                      )
                    "
                  >
                    −
                  </button>
                  <input
                    v-model.number="folder.concurrencyDraft"
                    :disabled="folder.updatingConcurrency"
                    type="number"
                    inputmode="numeric"
                    :min="1"
                    :max="folderMaxConcurrency"
                    :step="1"
                    aria-label="文件夹并发数"
                    :title="`范围 1–${folderMaxConcurrency} 个`"
                    @change="
                      folder.concurrencyDraft = Math.min(
                        folderMaxConcurrency,
                        Math.max(
                          1,
                          Math.round(Number(folder.concurrencyDraft) || 1),
                        ),
                      )
                    "
                  />
                  <button
                    type="button"
                    aria-label="增加并发数"
                    :disabled="
                      folder.updatingConcurrency ||
                      folder.concurrencyDraft >= folderMaxConcurrency
                    "
                    @click="
                      folder.concurrencyDraft = Math.min(
                        folderMaxConcurrency,
                        Number(folder.concurrencyDraft || 1) + 1,
                      )
                    "
                  >
                    +
                  </button>
                </span>
              </label>
              <p class="muted transfer-folder-range">
                范围 1–{{ folderMaxConcurrency }} 个
              </p>
              <div
                v-if="folder.state === 'active'"
                class="transfer-folder-apply"
              >
                <UiButton
                  size="small"
                  :disabled="
                    folder.updatingConcurrency ||
                    Number(folder.concurrencyDraft) === folder.concurrency
                  "
                  @click="emit('update-folder-concurrency', folder)"
                  >{{
                    folder.updatingConcurrency ? "正在应用…" : "应用"
                  }}</UiButton
                >
                <span class="muted">调整后逐步生效，不中断正在传输的文件。</span>
              </div>
            </details>
            <div class="transfer-file-actions">
              <template
                v-if="folder.state === 'offered' && folder.from !== myMember"
              >
                <UiButton
                  class="primary"
                  size="small"
                  icon="download"
                  :disabled="
                    active || folder.accepting || !folderSavingSupported
                  "
                  @click="emit('accept-folder', folder, false)"
                  >中继接收文件夹</UiButton
                >
                <UiButton
                  v-if="canDirect && folderSavingSupported"
                  size="small"
                  icon="download"
                  :theme-overrides="directButtonTheme"
                  :disabled="active || folder.accepting"
                  @click="emit('accept-folder', folder, true)"
                  >点对点接收</UiButton
                >
              </template>
              <UiButton
                v-if="['active', 'offered'].includes(folder.state)"
                class="quiet"
                size="small"
                icon="close"
                @click="emit('cancel-folder', folder)"
                >{{
                  folder.from !== myMember && folder.state === "offered"
                    ? "拒绝接收"
                    : "取消传输"
                }}</UiButton
              >
              <UiButton
                v-else
                class="quiet"
                size="small"
                icon="trash"
                @click="emit('remove-folder', folder.id)"
                >移除记录</UiButton
              >
            </div>
            <p
              v-if="
                folder.from !== myMember &&
                folder.state === 'offered' &&
                !folderSavingSupported
              "
              class="muted transfer-hint"
            >
              接收文件夹需要 HTTPS，以及支持选择保存目录的浏览器。
            </p>
            <details
              v-if="folder.recent.length"
              class="transfer-folder-recent"
            >
              <summary>
                已处理条目（最近 {{ folder.recent.length }} 项）
              </summary>
              <ul>
                <li v-for="(entry, index) in folder.recent" :key="index">
                  <span>{{ entry.path }}</span
                  ><span>{{ folderStateLabels[entry.state] }}</span>
                </li>
              </ul>
            </details>
          </article>
          <article
            v-for="file in group.files"
            :key="file.id"
            class="transfer-file"
            :data-transfer-id="file.id"
          >
            <div class="transfer-file-head">
              <span
                class="file-ext"
                :data-kind="fileDisplay(file.name).kind"
                aria-hidden="true"
                >{{ fileDisplay(file.name).label }}</span
              >
              <div class="transfer-file-copy">
                <div class="transfer-file-title">
                  <strong>{{ file.name }}</strong
                  ><span>{{ size(file.size) }}</span>
                  <UiButton
                    v-if="
                      ['cancelled', 'failed', 'complete'].includes(file.state)
                    "
                    type="button"
                    class="quiet transfer-remove"
                    size="small"
                    icon="close"
                    title="移除记录"
                    :aria-label="`移除任务 ${file.name}`"
                    @click="emit('remove-task', file)"
                  />
                </div>
                <div class="transfer-file-meta">
                  <span class="muted transfer-direction"
                    ><span aria-hidden="true">{{
                      file.from === myMember ? "↑" : "↓"
                    }}</span>
                    {{
                      file.from === myMember ? "我发送的" : "对方发送的"
                    }}</span
                  >
                  <span class="transfer-tag" :class="fileStatus(file).kind">{{
                    fileStatus(file).label
                  }}</span>
                  <!-- 只给任务状态着色，传输方式使用文字，减少重复色块。 -->
                  <span v-if="file.mode" class="muted transfer-mode">
                    {{ file.mode === "p2p" ? "点对点" : "实时中继" }}</span
                  >
                  <time
                    v-if="file.sentLabel"
                    class="muted transfer-file-time"
                    :datetime="file.sent_at"
                    :title="file.sentLabel"
                    :aria-label="`发送时间 ${file.sentLabel}`"
                    >发送于 {{ file.sentTimeLabel }}</time
                  >
                </div>
              </div>
            </div>
            <p
              v-if="file.state === 'failed' && fileStatus(file).detail"
              class="muted transfer-state-detail"
            >
              {{ fileStatus(file).detail }}
            </p>
            <progress
              v-if="file.state === 'active'"
              :value="file.bytes"
              :max="file.size || 1"
              :aria-label="`${file.name} 传输进度`"
            ></progress>
            <p
              v-if="
                file.state === 'active' &&
                (!file.url || file.urlUsed || file.mode === 'tool')
              "
              class="muted transfer-progress-text"
            >
              <span>{{ size(file.bytes) }} / {{ size(file.size) }}</span>
              <span
                >{{
                  file.size
                    ? Math.min(100, Math.round((file.bytes / file.size) * 100))
                    : 0
                }}% · {{ size(file.speed) }}/s</span
              >
            </p>
            <div class="transfer-file-actions">
              <UiButton
                v-if="
                  connected &&
                  file.from === myMember &&
                  ['failed', 'cancelled'].includes(file.state) &&
                  !file.retried
                "
                class="transfer-action"
                size="small"
                icon="retry"
                :disabled="active || file.retrying"
                @click="emit('retry', file)"
                >{{ file.retrying ? "等待重发" : "重新发送" }}</UiButton
              >
              <template
                v-if="
                  connected &&
                  file.state === 'offered' &&
                  file.from !== myMember
                "
              >
                <UiButton
                  v-if="canDirect"
                  class="transfer-action transfer-download-choice"
                  :class="{ primary: preferredTransport === 'p2p' }"
                  size="small"
                  icon="download"
                  :disabled="active || file.accepting"
                  @click="emit('accept', file, true)"
                  >点对点下载</UiButton
                >
                <UiButton
                  class="transfer-action transfer-download-choice"
                  :class="{
                    primary: preferredTransport === 'relay' || !canDirect,
                  }"
                  size="small"
                  icon="download"
                  :disabled="active || file.accepting"
                  @click="emit('accept', file, false)"
                  >中继下载</UiButton
                >
              </template>
              <template v-if="file.url && file.mode === 'tool'">
                <UiButton
                  tag="a"
                  class="primary transfer-action"
                  size="small"
                  icon="download"
                  :href="file.url"
                  :download="file.name"
                  referrerpolicy="no-referrer"
                  @click="file.requested = true"
                  >{{
                    file.completedRound > 0 &&
                    file.completedRound === file.progressRound
                      ? "重新下载"
                      : "下载文件"
                  }}</UiButton
                >
                <UiButton
                  class="transfer-action"
                  size="small"
                  :icon="file.linkCopied ? 'check' : 'clipboard'"
                  @click="emit('copy-download-link', file)"
                >
                  {{ file.linkCopied ? "已复制链接" : "复制下载链接" }}</UiButton
                >
                <UiButton
                  class="transfer-action"
                  size="small"
                  icon="check"
                  :disabled="
                    !file.completedRound ||
                    file.completedRound !== file.progressRound
                  "
                  title="结束传输后下载链接失效"
                  @click="emit('send', { type: 'finish-download', id: file.id })"
                  >结束传输</UiButton
                >
              </template>
              <span v-else-if="file.url" class="muted"
                >已请求下载，请查看下载记录</span
              >
              <UiButton
                v-if="['offered', 'active'].includes(file.state)"
                class="transfer-action"
                size="small"
                icon="close"
                @click="emit('send', { type: 'cancel', id: file.id })"
                >{{
                  file.state === "offered" && file.from !== myMember
                    ? "拒绝接收"
                    : file.mode === "tool"
                      ? "取消传输"
                      : "取消"
                }}</UiButton
              >
            </div>
            <details
              v-if="file.url && file.mode === 'tool'"
              class="transfer-tool-link"
              :open="file.linkExpanded"
              @toggle="file.linkExpanded = $event.target.open"
            >
              <summary>下载链接与说明</summary>
              <UiInput
                :model-value="file.url"
                readonly
                :aria-label="`${file.name} 下载链接`"
                @focus="$event.target.select()"
              />
              <p class="transfer-hint muted">
                已自动发起下载，也可复制链接到下载工具（支持 AB
                扩展接管）。最多
                {{ file.maxConnections }} 路连接，链接到
                {{ file.expiresLabel }}
                有效。每次完整下载都会记录日志；点击“结束传输”后链接失效。
              </p>
            </details>
          </article>
        </div>
      </component>
    </section>
  </MobileSheet>
</template>
