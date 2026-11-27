<script setup>
import Icon from "../Icon.vue";
import IPLocation from "../IPLocation.vue";
import { displayPairingCode } from "../transfer-code.js";

defineProps({
  status: { type: String, required: true },
  myMember: { type: Number, required: true },
  pairingCode: { type: String, default: "" },
  pairingSeconds: { type: Number, default: 0 },
  remainingTime: { type: Function, required: true },
  names: { type: Array, required: true },
  joining: { type: String, default: "" },
  joiningIP: { type: String, default: "" },
  joiningLocation: { type: Object, default: null },
  joiningDevice: { type: Object, default: null },
  joiningTime: { type: Number, default: 0 },
  messageClock: { type: Object, required: true },
  autoApproveDevice: { type: Boolean, default: false },
  busy: { type: Boolean, default: false },
});

const emit = defineEmits([
  "leave",
  "copy-code",
  "copy-join-link",
  "confirm-join",
  "reject-join",
  "update:autoApproveDevice",
]);
</script>

<template>
  <header class="transfer-wait-heading">
    <div>
      <h1>
        {{
          status === "connecting"
            ? "正在连接"
            : myMember === 0
              ? "等待对方加入"
              : "等待对方确认"
        }}
      </h1>
    </div>
    <UiButton type="button" @click="emit('leave')">取消连接</UiButton>
  </header>
  <div class="transfer-wait-layout" :class="{ 'has-code': pairingCode }">
    <section class="panel transfer-waiting">
      <div class="transfer-radar" aria-hidden="true">
        <i></i><i></i><i></i><span><Icon name="transfer" /></span>
      </div>
      <template v-if="pairingCode">
        <p class="transfer-code" aria-label="配对码">
          <span
            v-for="(digit, index) in displayPairingCode(pairingCode)"
            :key="index"
            >{{ digit }}</span
          >
        </p>
        <div class="transfer-wait-actions">
          <UiButton type="button" size="small" @click="emit('copy-code')"
            >复制配对码</UiButton
          >
          <UiButton
            type="button"
            class="quiet"
            size="small"
            @click="emit('copy-join-link')"
            >复制加入链接</UiButton
          >
        </div>
        <div class="transfer-countdown">
          <svg viewBox="0 0 24 24" aria-hidden="true">
            <circle cx="12" cy="12" r="9" pathLength="100" />
            <circle
              cx="12"
              cy="12"
              r="9"
              pathLength="100"
              :stroke-dasharray="Math.min(100, pairingSeconds / 3) + ' 100'"
            />
          </svg>
          <span
            ><b>{{ remainingTime(pairingSeconds) }}</b>
            后失效，配对成功后作废</span
          >
        </div>
      </template>
      <template v-else>
        <p class="transfer-wait-title">已发送加入请求</p>
        <p class="muted">
          请对方在设备上确认连接。确认前请保持此页面在线，拒绝或超时后可重新输入配对码。
        </p>
        <dl v-if="names[1 - myMember]" class="transfer-wait-peer">
          <div>
            <dt>连接对象</dt>
            <dd>{{ names[1 - myMember] }}</dd>
          </div>
        </dl>
      </template>
    </section>
    <section v-if="pairingCode" class="panel transfer-requests">
      <div class="transfer-section-heading">
        <h2>加入请求</h2>
        <span class="page-tag" :class="joining ? 'accent' : 'warning'">{{
          joining ? "1 个请求" : "等待中"
        }}</span>
      </div>
      <div v-if="joining" class="transfer-joining" role="status">
        <div class="transfer-peer-info">
          <strong>{{ joining }}</strong
          ><span class="page-tag accent">待授权</span>
        </div>
        <dl class="transfer-peer-details">
          <div>
            <dt>来源 IP</dt>
            <dd>{{ joiningIP || "未提供" }}</dd>
          </div>
          <div v-if="joiningLocation">
            <dt>IP 归属地</dt>
            <dd><IPLocation :location="joiningLocation" /></dd>
          </div>
          <div>
            <dt>设备信息</dt>
            <dd>{{ joiningDevice?.summary || "未提供" }}</dd>
          </div>
          <div v-if="joiningTime">
            <dt>申请时间</dt>
            <dd>{{ messageClock.format(joiningTime) }}</dd>
          </div>
        </dl>
        <p class="muted small">请求加入，请确认这是你要连接的设备。</p>
        <div class="transfer-confirm-actions">
          <UiButton
            type="button"
            class="primary"
            @click="emit('confirm-join')"
            >确认连接</UiButton
          >
          <UiButton
            type="button"
            class="danger"
            @click="emit('reject-join')"
            >拒绝并关闭</UiButton
          >
        </div>
      </div>
      <p v-else class="muted">对方输入配对码后，在这里确认设备信息。</p>
      <div
        v-if="myMember === 0 && status === 'waiting'"
        class="transfer-options"
      >
        <label class="switch-row"
          ><span>自动同意设备加入</span
          ><input
            :checked="autoApproveDevice"
            class="page-switch"
            role="switch"
            type="checkbox"
            :disabled="busy"
            @change="emit('update:autoApproveDevice', $event.target.checked)"
        /></label>
      </div>
    </section>
  </div>
</template>
