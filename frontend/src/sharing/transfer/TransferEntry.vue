<script setup>
import { ref } from "vue";
import Icon from "../Icon.vue";
import PairCodeInput from "../PairCodeInput.vue";
import PageIllustration from "../PageIllustration.vue";
import { validPairingCode } from "../transfer-code.js";
import { session } from "../session.js";
import { settings } from "../settings.js";

defineProps({
  archived: { type: Object, default: null },
  entryTab: { type: String, required: true },
  name: { type: String, required: true },
  code: { type: String, required: true },
  busy: { type: Boolean, default: false },
  pastingCode: { type: Boolean, default: false },
  canCreate: { type: Boolean, default: false },
  canJoin: { type: Boolean, default: false },
  autoApproveDevice: { type: Boolean, default: false },
  relayOnly: { type: Boolean, default: false },
  stunMode: { type: String, required: true },
  builtinSTUN: { type: Array, required: true },
  stunPicks: { type: Array, required: true },
});

const emit = defineEmits([
  "update:entryTab",
  "update:name",
  "update:code",
  "update:autoApproveDevice",
  "update:relayOnly",
  "update:stunMode",
  "update:stunPicks",
  "view-history",
  "clear-history",
  "open",
  "paste-code",
  "clear-code",
]);

const pairCodeInput = ref(null);

defineExpose({
  focusCode: () => pairCodeInput.value?.focus(),
});
</script>

<template>
  <header class="page-heading hero">
    <div>
      <h1>设备互传</h1>
      <p class="muted">配对后互发文字、图片、文件和文件夹，双方需保持页面在线</p>
    </div>
    <PageIllustration section="transfer" />
  </header>
  <section class="transfer-entry">
    <div
      v-if="archived && archived.messages.length"
      class="panel transfer-history-notice"
    >
      <span
        >上次与 {{ archived.names[1 - archived.member] }} 的聊天记录已保留 ·
        {{ archived.messages.length }} 条消息</span
      >
      <UiButton size="small" type="button" @click="emit('view-history')"
        >查看记录</UiButton
      >
      <UiButton
        size="small"
        type="button"
        class="quiet"
        @click="emit('clear-history')"
        >清空</UiButton
      >
    </div>
    <div
      class="page-segment transfer-entry-tabs"
      role="group"
      aria-label="连接方式"
    >
      <button
        type="button"
        :aria-pressed="entryTab === 'join'"
        @click="emit('update:entryTab', 'join')"
      >
        加入连接
      </button>
      <button
        type="button"
        :aria-pressed="entryTab === 'create'"
        @click="emit('update:entryTab', 'create')"
      >
        创建连接
      </button>
    </div>
    <div class="transfer-entry-grid" :data-tab="entryTab">
      <section class="panel transfer-entry-card">
        <div class="transfer-entry-heading">
          <span><Icon name="plus" /></span>
          <div>
            <h2>创建连接</h2>
            <p class="muted">生成 5 分钟有效的配对码</p>
          </div>
        </div>
        <label
          >设备名称<UiInput
            :model-value="name"
            aria-label="设备名称"
            maxlength="60"
            autocomplete="off"
            :disabled="busy"
            @update:model-value="emit('update:name', $event)"
        /></label>
        <div v-if="canCreate" class="transfer-options">
          <label class="switch-row transfer-switch-described"
            ><span
              >自动同意设备加入<small id="create-approve-help" class="muted"
                >持有配对码的设备直接加入，选择会保存在本浏览器</small
              ></span
            ><input
              :checked="autoApproveDevice"
              class="page-switch"
              role="switch"
              type="checkbox"
              aria-label="自动同意设备加入"
              aria-describedby="create-approve-help"
              :disabled="busy"
              @change="emit('update:autoApproveDevice', $event.target.checked)"
          /></label>
        </div>
        <label class="switch-row transfer-switch-described"
          ><span
            >仅使用中继<small id="create-relay-help" class="muted"
              >不向对方交换直连网络地址</small
            ></span
          ><input
            :checked="relayOnly"
            class="page-switch"
            role="switch"
            type="checkbox"
            aria-label="仅使用中继"
            aria-describedby="create-relay-help"
            :disabled="busy"
            @change="emit('update:relayOnly', $event.target.checked)"
        /></label>
        <details class="transfer-connection-settings">
          <summary>
            <span>高级连接设置</span><Icon name="chevron-down" />
          </summary>
          <div class="transfer-connection-fields">
            <label
              >STUN 模式
              <UiSelect
                :model-value="stunMode"
                aria-label="STUN 模式"
                :disabled="busy || relayOnly"
                @update:model-value="emit('update:stunMode', $event)"
              >
                <option value="site">使用站点配置（推荐）</option>
                <option value="custom">选择服务器</option>
                <option value="disabled">不使用 STUN</option>
              </UiSelect>
            </label>
            <fieldset
              v-if="stunMode === 'custom'"
              class="transfer-stun-picks"
              :disabled="busy || relayOnly"
              aria-describedby="transfer-stun-help"
            >
              <legend>选择 STUN 服务器</legend>
              <label
                v-for="server in builtinSTUN"
                :key="server"
                class="check transfer-stun-pick"
                :class="{ selected: stunPicks.includes(server) }"
                ><UiCheckbox
                  :model-value="stunPicks"
                  :value="server"
                  @update:model-value="emit('update:stunPicks', $event)"
                /><code>{{ server.replace(/^stun:/, "") }}</code></label
              >
            </fieldset>
            <p id="transfer-stun-help" class="muted small">
              {{
                relayOnly
                  ? "仅使用中继时不查询 STUN 服务器。"
                  : stunMode === "custom"
                    ? "只在本浏览器本次生效，不改变站点配置"
                    : stunMode === "disabled"
                      ? "不查询 STUN 服务器，仍可尝试直连；无法直连时可使用中继。"
                      : "使用站点提供的服务器，由浏览器收集候选地址并验证可用链路。"
              }}
            </p>
          </div>
        </details>
        <UiButton
          v-if="canCreate"
          type="button"
          class="primary"
          icon="link"
          :disabled="busy"
          @click="emit('open', 'create')"
          >创建配对码</UiButton
        >
        <p v-else-if="settings.transfer_enabled === 'false'" class="muted">
          设备互传已关闭。
        </p>
        <RouterLink
          v-else-if="session.type === 'none'"
          :to="{ path: '/login', query: { redirect: '/transfer' } }"
          >登录后创建连接</RouterLink
        >
        <p v-else class="muted">创建连接需要文本和文件分享权限。</p>
      </section>
      <form
        class="panel transfer-entry-card"
        @submit.prevent="emit('open', 'join')"
      >
        <div class="transfer-entry-heading">
          <span><Icon name="login" /></span>
          <div>
            <h2>加入连接</h2>
            <p class="muted">输入对方屏幕上的 4 位配对码</p>
          </div>
        </div>
        <p v-if="!canJoin" class="muted">
          {{
            settings.transfer_enabled === "false"
              ? "设备互传已关闭。"
              : "访客加入互传已关闭，请登录后使用。"
          }}
        </p>
        <div class="transfer-code-field">
          <label for="transfer-pair-code">配对码</label>
          <div class="transfer-code-input">
            <PairCodeInput
              ref="pairCodeInput"
              :model-value="code"
              :disabled="busy || !canJoin"
              @update:model-value="emit('update:code', $event)"
              @complete="emit('open', 'join')"
            />
            <div class="transfer-code-actions">
              <UiButton
                type="button"
                size="small"
                icon="clipboard"
                aria-label="粘贴配对码"
                :disabled="busy || !canJoin || pastingCode"
                @click="emit('paste-code')"
                >{{ pastingCode ? "读取中" : "一键粘贴" }}</UiButton
              >
              <UiButton
                type="button"
                class="quiet"
                size="small"
                aria-label="清空配对码"
                :disabled="busy || !canJoin || pastingCode || !code"
                @click="emit('clear-code')"
                >清空</UiButton
              >
            </div>
          </div>
        </div>
        <label
          >设备名称<UiInput
            :model-value="name"
            aria-label="加入时的设备名称"
            maxlength="60"
            autocomplete="off"
            :disabled="busy"
            @update:model-value="emit('update:name', $event)"
        /></label>
        <label class="switch-row transfer-join-relay"
          ><span
            >仅使用中继<small id="join-relay-help" class="muted"
              >不向对方交换直连网络地址</small
            ></span
          ><input
            :checked="relayOnly"
            class="page-switch"
            role="switch"
            type="checkbox"
            aria-label="仅使用中继"
            aria-describedby="join-relay-help"
            :disabled="busy"
            @change="emit('update:relayOnly', $event.target.checked)"
        /></label>
        <p class="muted small transfer-join-safety">
          <Icon
            name="shield"
          />中继经本站转发；直连会向对方提供网络地址。只与信任的人配对。
        </p>
        <UiButton
          class="primary"
          :disabled="busy || !canJoin || !validPairingCode(code)"
          >加入</UiButton
        >
      </form>
    </div>
  </section>
</template>
