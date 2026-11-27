<script setup>
import Icon from "../Icon.vue";

defineProps({
  peerName: { type: String, required: true },
  connectionState: { type: String, required: true },
  connectionLabel: { type: String, required: true },
  connectionLink: { type: String, required: true },
  connected: { type: Boolean, default: false },
  hasHistory: { type: Boolean, default: false },
  mobile: { type: Boolean, default: false },
  fileCount: { type: Number, default: 0 },
  localName: { type: String, required: true },
  localDeviceSummary: { type: String, required: true },
  peerDevice: { type: Object, default: null },
  reconnectSeconds: { type: Number, default: 0 },
  remainingTime: { type: Function, required: true },
});

const emit = defineEmits([
  "leave-conversation",
  "open-files",
  "open-details",
  "return-to-entry",
  "leave",
]);
</script>

<template>
  <header class="mobile-chat-heading">
    <UiButton
      type="button"
      class="quiet icon-button"
      icon="back"
      aria-label="返回"
      @click="emit('leave-conversation')"
    />
    <div class="mobile-chat-who">
      <strong>{{ peerName }}</strong>
      <span class="transfer-peer-status" :data-state="connectionState"
        ><i aria-hidden="true"></i>{{ connectionLabel
        }}<template v-if="connected"> · {{ connectionLink }}</template></span
      >
    </div>
    <div class="mobile-chat-file-action">
      <UiButton
        type="button"
        class="quiet icon-button"
        icon="file"
        aria-label="文件列表"
        @click="emit('open-files')"
      />
      <b v-if="fileCount">{{ fileCount }}</b>
    </div>
    <UiButton
      type="button"
      class="quiet icon-button"
      icon="info"
      aria-label="连接详情"
      @click="emit('open-details')"
    />
  </header>
  <div
    class="panel transfer-peer"
    :class="{
      'transfer-peer-disconnected': hasHistory,
      'transfer-peer-waiting': !connected && !hasHistory,
    }"
  >
    <div class="transfer-peer-bar">
      <div class="transfer-peer-info">
        <span class="transfer-peer-status" :data-state="connectionState"
          ><i aria-hidden="true"></i>{{ connectionLabel }}</span
        >
        <span class="transfer-peer-sep" aria-hidden="true"></span>
        <div class="transfer-peer-devices">
          <div class="transfer-peer-device">
            <Icon name="monitor" />
            <div>
              <strong :title="localName">{{ localName }}</strong
              ><small class="muted" :title="localDeviceSummary">{{
                localDeviceSummary
              }}</small>
            </div>
          </div>
          <span
            class="transfer-peer-wire"
            :data-state="connectionState"
            aria-hidden="true"
            ><span class="transfer-peer-wire-line"></span
            ><span class="transfer-peer-wire-line"></span
          ></span>
          <div class="transfer-peer-device">
            <Icon
              :name="peerDevice?.type === 'mobile' ? 'mobile' : 'monitor'"
            />
            <div>
              <strong :title="peerName">{{ peerName }}</strong
              ><small class="muted" :title="peerDevice?.summary">{{
                peerDevice?.summary
              }}</small>
            </div>
          </div>
        </div>
      </div>
      <span v-if="connected" class="transfer-peer-link">{{
        connectionLink
      }}</span>
      <div v-if="!mobile" class="transfer-peer-actions">
        <template v-if="hasHistory">
          <UiButton
            type="button"
            class="primary"
            size="small"
            icon="retry"
            @click="emit('return-to-entry')"
            >重新连接</UiButton
          >
          <UiButton
            type="button"
            size="small"
            @click="emit('return-to-entry', 'create')"
            >新建配对</UiButton
          >
        </template>
        <UiButton
          v-else
          type="button"
          class="danger"
          size="small"
          @click="emit('leave')"
          >断开</UiButton
        >
      </div>
    </div>
  </div>
  <div
    v-if="!connected"
    class="transfer-connection-banner"
    :class="{ disconnected: hasHistory }"
    role="status"
  >
    <Icon :name="hasHistory ? 'unlink' : 'clock'" />
    <div>
      <strong>{{
        hasHistory ? "已断开连接" : "连接中断，正在自动恢复"
      }}</strong
      ><span>{{
        hasHistory
          ? "聊天记录已保留，刷新页面后仍可查看"
          : "恢复期间无法发送，未完成的文件需重发"
      }}</span>
    </div>
    <time v-if="!hasHistory">{{ remainingTime(reconnectSeconds) }}</time>
  </div>
</template>
