<script setup>
import IPLocation from "../IPLocation.vue";
import MobileSheet from "../ui/MobileSheet.vue";

defineProps({
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
        <span class="muted">{{
          hasHistory
            ? "已断开"
            : !connected
              ? "等待恢复"
              : direct
                ? "点对点 · 中继可用"
                : "中继"
        }}</span>
      </div>
      <dl id="transfer-peer-details" class="transfer-peer-details">
        <div>
          <dt>延迟</dt>
          <dd>{{ latency === null ? "—" : `${latency} ms` }}</dd>
        </div>
        <div>
          <dt>链路</dt>
          <dd>
            {{
              candidateRoute ||
              (hasHistory && peerEndpoint ? "点对点" : connectionLink)
            }}
          </dd>
        </div>
        <div>
          <dt>本机地址</dt>
          <dd>
            {{ localEndpoint ? endpointAddress(localEndpoint) : "—" }}
          </dd>
        </div>
        <div>
          <dt>来源 IP</dt>
          <dd>{{ peerIP || "未提供" }}</dd>
        </div>
        <div v-if="peerLocation">
          <dt>IP 归属地</dt>
          <dd>
            <IPLocation :location="peerLocation" />
          </dd>
        </div>
        <div>
          <dt>设备信息</dt>
          <dd>{{ peerDevice?.summary || "未提供" }}</dd>
        </div>
        <div>
          <dt>{{ hasHistory ? "最后点对点地址" : "点对点地址" }}</dt>
          <dd>{{ peerAddress }}</dd>
        </div>
        <div v-if="joiningTime || pairedTime">
          <dt>申请 / 同意</dt>
          <dd>
            {{ joiningTime ? messageClock.format(joiningTime) : "—" }} /
            {{ pairedTime ? messageClock.format(pairedTime) : "—" }}
          </dd>
        </div>
        <div>
          <dt>STUN</dt>
          <dd>
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
          <UiButton type="button" @click="emit('return-to-entry', 'create')"
            >新建配对</UiButton
          >
        </template>
        <UiButton
          v-else
          type="button"
          class="danger"
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
