<script setup>
defineProps({ kind: { type: String, required: true } });
</script>

<template>
  <svg
    class="empty-illustration"
    viewBox="0 0 96 72"
    fill="none"
    aria-hidden="true"
    focusable="false"
  >
    <template v-if="kind === 'file'">
      <rect x="14" y="20" width="44" height="40" rx="6" fill="var(--soft)" />
      <g class="illustration-float">
        <rect
          x="34"
          y="10"
          width="40"
          height="48"
          rx="6"
          fill="var(--surface)"
          stroke="var(--accent)"
          stroke-width="2"
        />
        <path
          d="M44 26h20m-20 8h20m-20 8h12"
          stroke="var(--accent)"
          stroke-width="2"
          stroke-linecap="round"
        />
      </g>
      <circle
        class="illustration-float delayed"
        cx="80"
        cy="16"
        r="5"
        fill="var(--warning)"
      />
    </template>
    <template v-else>
      <rect
        x="8"
        y="22"
        width="30"
        height="22"
        rx="4"
        stroke="var(--accent)"
        stroke-width="2"
      />
      <path
        d="M4 50h38"
        stroke="var(--accent)"
        stroke-width="2"
        stroke-linecap="round"
      />
      <rect
        x="66"
        y="16"
        width="20"
        height="36"
        rx="4"
        stroke="var(--accent)"
        stroke-width="2"
      />
      <path
        d="M44 30h18"
        stroke="var(--muted)"
        stroke-opacity=".4"
        stroke-width="2"
        stroke-dasharray="3 4"
      />
      <circle
        class="illustration-float"
        cx="53"
        cy="30"
        r="4"
        fill="var(--warning)"
      />
    </template>
  </svg>
</template>
