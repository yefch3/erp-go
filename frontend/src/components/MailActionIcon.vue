<template>
  <svg
    class="mail-action-icon"
    viewBox="0 0 24 24"
    fill="none"
    aria-hidden="true"
  >
    <template v-if="kind === 'reply'">
      <path d="M9 7 4 12l5 5" />
      <path d="M4 12h8.5c4.15 0 7.5 2.7 7.5 6.5" />
    </template>
    <template v-else-if="kind === 'reply-all'">
      <path d="M7 7 2 12l5 5" />
      <path d="m12 7-5 5 5 5" />
      <path d="M7 12h6c3.87 0 7 2.46 7 6" />
    </template>
    <template v-else>
      <path d="m15 7 5 5-5 5" />
      <path d="M20 12h-8.5C7.35 12 4 14.7 4 18.5" />
    </template>
  </svg>
</template>

<script setup lang="ts">
defineProps<{
  kind: 'reply' | 'reply-all' | 'forward'
}>()
</script>

<style scoped>
.mail-action-icon {
  width: 18px;
  height: 18px;
  stroke: currentColor;
  stroke-width: 1.9;
  stroke-linecap: round;
  stroke-linejoin: round;
}
</style>
