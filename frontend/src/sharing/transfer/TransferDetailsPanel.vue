<script setup>
import { computed } from "vue";
import IPLocation from "../IPLocation.vue";
import MobileSheet from "../ui/MobileSheet.vue";

const props = defineProps({
  open: { type: Boolean, default: false },
  hasHistory: { type: Boolean, default: false },
  connected: { type: Boolean, default: false },
  direct: { type: Boolean, default: false },
  latency: { type: Number, default: null },
  candidateRoute: { type: String, default: "" },
  connectionLink: { type: String, required: true },
  localEndpoint: { type: Object, default: null },
  peerEndpoint: { type: Object, default: null },
  endpointAddress: { type: Function, required: true },
  peerIP: { type: String, default: "" },
  peerLocation: { type: Object, default: null },
  peerDevice: { type: Object, default: null },
  peerAddress: { type: String, required: true },
  joiningTime: { type: Number, default: 0 },
  pairedTime: { type: Number, default: 0 },
  messageClock: { type: Object, required: true },
  stunServers: { type: Array, required: true },
  mobile: { type: Boolean, default: false },
});

const emit = defineEmits(["update:open", "return-to-entry", "leave"]);

function isPublicAddress(address) {
  if (!address) return false;
  const host = String(address).trim().toLowerCase().replace(/^\[|\]$/g, "");
  if (!host || host.endsWith(".local")) return false;
  if (host.includes(":")) {
    if (host === "::1" || host === "::") return false;
    if (/^fe[89ab][0-9a-f]:/.test(host) || /^f[cd][0-9a-f]{2}:/.test(host)) {
      return false;
    }
    const mapped = host.match(/^::ffff:(\d+\.\d+\.\d+\.\d+)$/);
    if (mapped) return isPublicAddress(mapped[1]);
    return true;
  }
  const parts = host.split(".").map(Number);
  if (
    parts.length !== 4 ||
    parts.some((n) => !Number.isInteger(n) || n < 0 || n > 255)
  ) {
    return false;
  }
  const [a, b] = parts;
  if (a === 10 || a === 127 || a === 0) return false;
  if (a === 192 && b === 168) return false;
  if (a === 169 && b === 254) return false;
  if (a === 172 && b >= 16 && b <= 31) return false;
  if (a === 100 && b >= 64 && b <= 127) return false;
  return true;
}

const routeLabel = computed(() => {
  const localType = props.localEndpoint?.type;
  const peerType = props.peerEndpoint?.type;
  if (localType && peerType) {
    if (localType === "relay" || peerType === "relay") return "TURN 中继";
    if (localType === "host" && peerType === "host") {
      if (
        isPublicAddress(props.localEndpoint?.address) ||
        isPublicAddress(props.peerEndpoint?.address)
      ) {
        return "公网直连";
      }
      return "局域网直连";
    }
    return "NAT 穿透直连";
  }
  if (props.hasHistory && props.peerEndpoint) return "点对点";
  return props.direct ? "点对点" : "中继";
});
</script>

<template>
  <MobileSheet
    :model-value="open"
    title="连接详情"
    @update:model-value="emit('update:open', $event)"
  >
    <section
      class="panel transfer-details-panel"
      aria-labelledby="transfer-details-title"
    >
      <div class="transfer-section-heading">
        <h2 id="transfer-details-title">连接详情</h2>
        <span
          class="page-tag"
          :class="
            hasHistory
              ? 'failure'
              : !connected
                ? 'warning'
                : direct
                  ? 'success'
                  : ''
          "
          >{{
            hasHistory
              ? "已断开"
              : !connected
                ? "等待重连"
                : direct
                  ? "点对点 · 中继可用"
                  : "中继"
          }}</span
        >
      </div>
      <div id="transfer-peer-details" class="transfer-details-body">
        <div class="transfer-details-metrics">
          <div class="transfer-details-metric">
            <span>延迟</span>
            <strong class="mono"
              ><span
                :key="latency === null ? 'none' : latency"
                class="transfer-latency-tick"
                >{{ latency === null ? "—" : `${latency} ms` }}</span
              ></strong
            >
          </div>
          <div class="transfer-details-metric">
            <span>链路</span>
            <strong :title="candidateRoute || undefined">{{
              routeLabel
            }}</strong>
          </div>
        </div>
        <div class="transfer-details-groups">
          <div class="transfer-details-group">
            <h3 class="transfer-details-group-title">对方信息</h3>
            <dl class="transfer-details-list">
              <div class="transfer-details-row">
                <dt>设备信息</dt>
                <dd>{{ peerDevice?.summary || "未提供" }}</dd>
              </div>
              <div class="transfer-details-row">
                <dt>来源 IP</dt>
                <dd class="mono">{{ peerIP || "未提供" }}</dd>
              </div>
              <div v-if="peerLocation" class="transfer-details-row">
                <dt>IP 归属地</dt>
                <dd>
                  <IPLocation :location="peerLocation" />
                </dd>
              </div>
              <div
                v-if="joiningTime || pairedTime"
                class="transfer-details-row"
              >
                <dt>申请 / 同意</dt>
                <dd class="mono">
                  {{ joiningTime ? messageClock.format(joiningTime) : "—" }} /
                  {{ pairedTime ? messageClock.format(pairedTime) : "—" }}
                </dd>
              </div>
            </dl>
          </div>
          <div class="transfer-details-group">
            <h3 class="transfer-details-group-title">网络地址</h3>
            <dl class="transfer-details-list">
              <div class="transfer-details-row">
                <dt>本机地址</dt>
                <dd class="mono">
                  {{ localEndpoint ? endpointAddress(localEndpoint) : "—" }}
                </dd>
              </div>
              <div class="transfer-details-row">
                <dt>{{ hasHistory ? "最后直连" : "直连地址" }}</dt>
                <dd
                  :class="{
                    mono: direct || (hasHistory && Boolean(peerEndpoint)),
                  }"
                >
                  {{ peerAddress }}
                </dd>
              </div>
              <div class="transfer-details-row">
                <dt>STUN</dt>
                <dd :class="{ mono: stunServers.length > 0 }">
                  <span
                    v-if="stunServers.length"
                    class="transfer-stun-summary"
                    :title="
                      stunServers
                        .map((server) => server.replace(/^stun:/, ''))
                        .join('\n')
                    "
                    >{{ stunServers[0].replace(/^stun:/, "")
                    }}<template v-if="stunServers.length > 1">
                      等 {{ stunServers.length }} 个</template
                    ></span
                  >
                  <template v-else>未使用</template>
                </dd>
              </div>
            </dl>
          </div>
        </div>
      </div>
      <div
        v-if="mobile"
        class="transfer-peer-actions transfer-details-actions"
      >
        <template v-if="hasHistory">
          <UiButton
            type="button"
            class="primary"
            icon="retry"
            @click="emit('return-to-entry')"
            >重新连接</UiButton
          >
          <UiButton
            type="button"
            icon="plus"
            @click="emit('return-to-entry', 'create')"
            >新建配对</UiButton
          >
        </template>
        <UiButton
          v-else
          type="button"
          class="danger"
          icon="unlink"
          @click="
            emit('update:open', false);
            emit('leave');
          "
          >断开</UiButton
        >
      </div>
    </section>
  </MobileSheet>
</template>
