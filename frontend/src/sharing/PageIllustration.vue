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
    <template v-else>
      <rect
        x="16"
        y="14"
        width="118"
        height="70"
        rx="10"
        fill="var(--surface)"
        stroke="var(--accent)"
        stroke-width="2"
      />
      <g class="illustration-float">
        <rect
          x="30"
          y="54"
          width="12"
          height="18"
          rx="3"
          fill="var(--accent)"
          opacity=".5"
        />
        <rect
          x="48"
          y="44"
          width="12"
          height="28"
          rx="3"
          fill="var(--accent)"
          opacity=".7"
        />
        <rect x="66" y="34" width="12" height="38" rx="3" :fill="paint" />
      </g>
      <path
        class="illustration-float delayed"
        d="m90 60 10-10 9 6 14-18"
        stroke="var(--accent-2)"
        stroke-width="3"
        stroke-linecap="round"
        stroke-linejoin="round"
      />
      <circle cx="123" cy="38" r="4" fill="var(--accent-2)" />
    </template>
  </svg>
</template>
