<template>
  <header class="procurement-page-header">
    <h1>{{ title }}</h1>
    <p>{{ description }}</p>
    <div v-if="$slots.actions" class="actions"><slot name="actions" /></div>
  </header>
</template>

<script setup lang="ts">
defineProps<{ title: string; description: string }>()
</script>

<style scoped>
.procurement-page-header {
  display: flex;
  align-items: center;
  gap: 14px;
  min-height: 42px;
  margin-bottom: 18px;
  padding: 18px 20px;
  border: 1px solid #d8eef8;
  border-left: 4px solid #4ac1ff;
  border-radius: 12px;
  background: linear-gradient(110deg, #eefaff 0%, #fff 64%, #effcf5 100%);
}
.procurement-page-header h1 {
  flex: 0 0 auto;
  margin: 0;
  color: #141817;
  font-size: 22px;
  font-weight: 700;
  letter-spacing: -.3px;
  line-height: 1.3;
}
.procurement-page-header p {
  margin: 0;
  padding: 5px 10px;
  border-radius: 999px;
  background: #eaf8ff;
  color: #60717c;
  font-size: 12px;
  line-height: 1.5;
}
.actions {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-left: auto;
}
@media (max-width: 760px) {
  .procurement-page-header { align-items: flex-start; flex-direction: column; gap: 8px; padding: 16px; }
  .procurement-page-header p { border-radius: 8px; }
  .actions { width: 100%; margin-left: 0; }
}
</style>
