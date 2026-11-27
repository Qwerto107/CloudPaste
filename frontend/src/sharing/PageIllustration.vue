<script setup>
import { useId } from "vue";

defineProps({ section: { type: String, required: true } });
const gradient = `hero-${useId()}`;
const paint = `url(#${gradient})`;
</script>

<template>
  <svg
    class="page-illustration"
    viewBox="0 0 150 96"
    fill="none"
    aria-hidden="true"
    focusable="false"
  >
    <defs>
      <linearGradient :id="gradient" x2="1" y2="1">
        <stop stop-color="var(--accent)" />
        <stop offset="1" stop-color="var(--accent-2)" />
      </linearGradient>
    </defs>
    <template v-if="section === 'text'">
      <rect
        x="20"
        y="14"
        width="78"
        height="70"
        rx="10"
        fill="var(--surface)"
        stroke="var(--accent)"
        stroke-width="2"
      />
      <path
        d="M32 32h40m-40 12h54M32 56h30"
        stroke="var(--accent)"
        stroke-width="3"
        stroke-linecap="round"
        opacity=".7"
      />
      <g class="illustration-float">
        <rect x="84" y="46" width="52" height="34" rx="9" :fill="paint" />
        <path
          d="m98 63 6 6 12-12"
          stroke="white"
          stroke-width="3"
          stroke-linecap="round"
          stroke-linejoin="round"
        />
      </g>
      <circle
        class="illustration-float delayed"
        cx="120"
        cy="24"
        r="7"
        fill="var(--accent-2)"
        opacity=".5"
      />
      <circle cx="12" cy="70" r="5" fill="var(--accent)" opacity=".4" />
    </template>
    <template v-else-if="section === 'file'">
      <path
        d="M42 74h66a20 20 0 0 0 0-40 28 28 0 0 0-54-6 22 22 0 0 0-12 46Z"
        fill="var(--surface)"
        stroke="var(--accent)"
        stroke-width="2"
      />
      <g class="illustration-float">
        <rect x="62" y="38" width="26" height="30" rx="5" :fill="paint" />
        <path
          d="M75 60V46m-6 6 6-6 6 6"
          stroke="white"
          stroke-width="2.5"
          stroke-linecap="round"
          stroke-linejoin="round"
        />
      </g>
      <rect
        class="illustration-float delayed"
        x="18"
        y="18"
        width="16"
        height="20"
        rx="4"
        fill="#f59e0b"
        opacity=".55"
      />
      <rect
        x="118"
        y="12"
        width="14"
        height="18"
        rx="4"
        fill="#ef4444"
        opacity=".45"
      />
    </template>
    <template v-else-if="section === 'transfer'">
      <rect
        x="10"
        y="26"
        width="50"
        height="34"
        rx="6"
        fill="var(--surface)"
        stroke="var(--accent)"
        stroke-width="2"
      />
      <path
        d="M4 68h62"
        stroke="var(--accent)"
        stroke-width="2.5"
        stroke-linecap="round"
      />
      <rect
        x="108"
        y="18"
        width="30"
        height="54"
        rx="7"
        fill="var(--surface)"
        stroke="var(--accent-2)"
        stroke-width="2"
      />
      <path
        d="M66 44h38"
        stroke="var(--muted)"
        opacity=".4"
        stroke-width="2"
        stroke-dasharray="4 5"
      />
      <circle
        class="illustration-float"
        cx="78"
        cy="44"
        r="5"
        fill="var(--accent)"
      />
      <circle
        class="illustration-float delayed"
        cx="92"
        cy="44"
        r="4"
        fill="var(--accent-2)"
      />
      <circle cx="120" cy="14" r="4" fill="var(--accent)" opacity=".4" />
    </template>
    <!-- 后台：文本管理（文档清单 + 悬浮文本标签） -->
    <template v-else-if="section === 'admin-texts'">
      <circle cx="118" cy="24" r="18" :fill="paint" opacity=".14" />
      <rect x="20" y="16" width="96" height="66" rx="11" fill="var(--surface)" stroke="var(--line-strong)" stroke-width="1.5" />
      <rect x="30" y="28" width="38" height="6" rx="3" :fill="paint" opacity=".85" />
      <rect x="30" y="40" width="66" height="4.5" rx="2.25" fill="var(--line-strong)" opacity=".8" />
      <rect x="30" y="50" width="54" height="4.5" rx="2.25" fill="var(--line-strong)" opacity=".6" />
      <rect x="30" y="60" width="44" height="4.5" rx="2.25" fill="var(--line-strong)" opacity=".45" />
      <g class="illustration-float">
        <rect x="98" y="46" width="36" height="36" rx="10" :fill="paint" />
        <path d="M108 59h16m-16 5h16m-16 5h10" stroke="#ffffff" stroke-width="2" stroke-linecap="round" />
      </g>
    </template>
    <!-- 后台：文件管理（叠层文件卡 + 悬浮文件夹徽章） -->
    <template v-else-if="section === 'admin-files'">
      <circle cx="28" cy="24" r="14" fill="var(--accent)" opacity=".12" />
      <rect x="28" y="14" width="84" height="58" rx="10" fill="var(--surface-subtle)" stroke="var(--line)" stroke-width="1.4" />
      <rect x="18" y="22" width="94" height="60" rx="11" fill="var(--surface)" stroke="var(--line-strong)" stroke-width="1.5" />
      <rect x="28" y="34" width="22" height="26" rx="5" fill="var(--accent)" opacity=".2" />
      <path d="M35 45h8m-8 5h6" stroke="var(--accent)" stroke-width="1.8" stroke-linecap="round" />
      <rect x="58" y="36" width="42" height="5" rx="2.5" fill="var(--line-strong)" opacity=".85" />
      <rect x="58" y="46" width="32" height="4.5" rx="2.25" fill="var(--line-strong)" opacity=".55" />
      <rect x="28" y="68" width="66" height="5" rx="2.5" fill="var(--surface-subtle)" />
      <rect x="28" y="68" width="42" height="5" rx="2.5" :fill="paint" opacity=".8" />
      <g class="illustration-float">
        <rect x="100" y="48" width="36" height="34" rx="10" :fill="paint" />
        <path d="M110 60v-1.5a1.5 1.5 0 0 1 1.5-1.5h4l1.8 2.2h7.2a1.5 1.5 0 0 1 1.5 1.5v8.8a1.5 1.5 0 0 1-1.5 1.5h-13a1.5 1.5 0 0 1-1.5-1.5V60Z" stroke="#ffffff" stroke-width="1.8" stroke-linejoin="round" fill="#ffffff" fill-opacity=".16" />
      </g>
    </template>
    <!-- 后台：存储管理（双层机架节点 + 悬浮云端徽章） -->
    <template v-else-if="section === 'admin-storage'">
      <circle cx="118" cy="24" r="17" :fill="paint" opacity=".14" />
      <rect x="18" y="18" width="98" height="28" rx="9" fill="var(--surface)" stroke="var(--line-strong)" stroke-width="1.5" />
      <circle cx="32" cy="32" r="3.5" fill="var(--success)" />
      <rect x="43" y="29" width="38" height="6" rx="3" fill="var(--line-strong)" opacity=".8" />
      <rect x="92" y="29" width="14" height="6" rx="3" fill="var(--accent)" opacity=".3" />
      <rect x="18" y="52" width="98" height="28" rx="9" fill="var(--surface)" stroke="var(--line-strong)" stroke-width="1.5" />
      <circle cx="32" cy="66" r="3.5" fill="var(--accent)" />
      <rect x="43" y="63" width="32" height="6" rx="3" fill="var(--line-strong)" opacity=".65" />
      <g class="illustration-float">
        <rect x="100" y="46" width="36" height="36" rx="10" :fill="paint" />
        <path d="M111.5 70h13a4 4 0 0 0 .5-8 5.5 5.5 0 0 0-10.6-1.4A4.5 4.5 0 0 0 111.5 70Z" stroke="#ffffff" stroke-width="1.8" stroke-linejoin="round" fill="#ffffff" fill-opacity=".16" />
      </g>
    </template>
    <!-- 后台：密钥与权限（凭据卡 + 掩码圆点 + 悬浮钥匙徽章） -->
    <template v-else-if="section === 'admin-keys'">
      <circle cx="118" cy="24" r="18" :fill="paint" opacity=".14" />
      <rect x="18" y="18" width="100" height="62" rx="11" fill="var(--surface)" stroke="var(--line-strong)" stroke-width="1.5" />
      <rect x="28" y="29" width="34" height="6" rx="3" :fill="paint" opacity=".8" />
      <rect x="28" y="42" width="72" height="12" rx="6" fill="var(--surface-subtle)" />
      <circle cx="39" cy="48" r="2.2" fill="var(--accent)" />
      <circle cx="48" cy="48" r="2.2" fill="var(--accent)" />
      <circle cx="57" cy="48" r="2.2" fill="var(--accent)" />
      <circle cx="66" cy="48" r="2.2" fill="var(--accent-2)" />
      <circle cx="75" cy="48" r="2.2" fill="var(--accent-2)" />
      <rect x="28" y="61" width="24" height="7" rx="3.5" fill="var(--accent)" opacity=".2" />
      <rect x="57" y="61" width="24" height="7" rx="3.5" fill="var(--accent-2)" opacity=".2" />
      <g class="illustration-float">
        <rect x="100" y="46" width="36" height="36" rx="10" :fill="paint" />
        <path d="M121.5 64.5a4.5 4.5 0 1 0-3.8-3.8l-6.2 6.2V71h3.5v-2.2h2.2v-2.2l4.3-2.1Z" stroke="#ffffff" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round" fill="#ffffff" fill-opacity=".16" />
      </g>
    </template>
    <!-- 后台：账号管理（个人资料卡 + 悬浮盾牌锁徽章） -->
    <template v-else-if="section === 'admin-account'">
      <circle cx="118" cy="24" r="18" :fill="paint" opacity=".14" />
      <rect x="18" y="18" width="100" height="62" rx="11" fill="var(--surface)" stroke="var(--line-strong)" stroke-width="1.5" />
      <circle cx="44" cy="42" r="10" fill="var(--accent)" opacity=".2" />
      <circle cx="44" cy="39.5" r="3.6" stroke="var(--accent)" stroke-width="1.8" />
      <path d="M37.5 49a7 7 0 0 1 13 0" stroke="var(--accent)" stroke-width="1.8" stroke-linecap="round" />
      <rect x="62" y="34" width="38" height="6" rx="3" fill="var(--line-strong)" opacity=".85" />
      <rect x="62" y="45" width="26" height="4.5" rx="2.25" fill="var(--line-strong)" opacity=".55" />
      <rect x="28" y="62" width="62" height="7" rx="3.5" fill="var(--surface-subtle)" />
      <rect x="28" y="62" width="34" height="7" rx="3.5" :fill="paint" opacity=".75" />
      <g class="illustration-float">
        <rect x="100" y="46" width="36" height="36" rx="10" :fill="paint" />
        <path d="M118 56.5 110.5 59.5v5c0 4.6 3.2 8.2 7.5 9.5 4.3-1.3 7.5-4.9 7.5-9.5v-5l-7.5-3Z" fill="#ffffff" fill-opacity=".18" stroke="#ffffff" stroke-width="1.8" stroke-linejoin="round" />
        <path d="m114.8 64.5 2.2 2.2 4.4-4.4" stroke="#ffffff" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" />
      </g>
    </template>
    <!-- 后台：操作日志（时间轴审计流 + 悬浮检视镜徽章） -->
    <template v-else-if="section === 'admin-audit'">
      <circle cx="118" cy="24" r="18" :fill="paint" opacity=".14" />
      <rect x="18" y="16" width="100" height="66" rx="11" fill="var(--surface)" stroke="var(--line-strong)" stroke-width="1.5" />
      <!-- 时间轴竖线 -->
      <path d="M33 30v38" stroke="var(--line)" stroke-width="1.6" stroke-dasharray="3 3" />
      <!-- 三条日志记录 -->
      <circle cx="33" cy="31" r="3.5" fill="var(--success)" />
      <rect x="43" y="28.5" width="44" height="5" rx="2.5" fill="var(--line-strong)" opacity=".85" />
      <rect x="92" y="28.5" width="16" height="5" rx="2.5" fill="var(--success)" opacity=".3" />
      <circle cx="33" cy="49" r="3.5" fill="var(--accent)" />
      <rect x="43" y="46.5" width="54" height="5" rx="2.5" fill="var(--line-strong)" opacity=".75" />
      <circle cx="33" cy="67" r="3.5" fill="var(--accent-2)" />
      <rect x="43" y="64.5" width="38" height="5" rx="2.5" fill="var(--line-strong)" opacity=".55" />
      <g class="illustration-float">
        <rect x="100" y="46" width="36" height="36" rx="10" :fill="paint" />
        <circle cx="116.5" cy="62.5" r="5.5" stroke="#ffffff" stroke-width="2" fill="#ffffff" fill-opacity=".16" />
        <path d="m120.8 66.8 3.7 3.7" stroke="#ffffff" stroke-width="2.2" stroke-linecap="round" />
      </g>
    </template>
    <template v-else>
      <!-- 通用后台控制台兜底插画 -->
      <circle cx="118" cy="24" r="18" :fill="paint" opacity=".14" />
      <rect x="16" y="16" width="104" height="66" rx="11" fill="var(--surface)" stroke="var(--line-strong)" stroke-width="1.5" />
      <path d="M16.75 30h102.5" stroke="var(--line)" stroke-width="1.2" />
      <circle cx="26" cy="23" r="2.2" fill="var(--accent)" opacity=".75" />
      <circle cx="34" cy="23" r="2.2" fill="var(--accent-2)" opacity=".65" />
      <circle cx="42" cy="23" r="2.2" fill="var(--line-strong)" />
      <rect x="25" y="38" width="22" height="6" rx="3" fill="var(--accent)" opacity=".28" />
      <rect x="25" y="49" width="18" height="5" rx="2.5" fill="var(--line-strong)" opacity=".75" />
      <path d="M54 36v38" stroke="var(--line)" stroke-width="1.2" />
      <rect x="62" y="39" width="28" height="5" rx="2.5" fill="var(--line-strong)" opacity=".85" />
      <rect x="96" y="37" width="16" height="9" rx="4.5" :fill="paint" />
      <circle cx="107.5" cy="41.5" r="3" fill="#ffffff" />
      <rect x="62" y="64" width="36" height="6" rx="3" fill="var(--surface-subtle)" />
      <rect x="62" y="64" width="24" height="6" rx="3" :fill="paint" opacity=".85" />
    </template>
  </svg>
</template>
