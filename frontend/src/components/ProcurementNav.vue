<template>
  <nav class="procurement-nav" :aria-label="t('procurementNav.title')">
    <router-link
      v-for="item in items"
      :key="item.path"
      :to="item.path"
      class="procurement-nav__item"
      :class="{ active: route.path === item.path }"
    >
      {{ item.label }}
    </router-link>
  </nav>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRoute } from 'vue-router'
import { useAuthStore } from '../stores/auth'

const { t } = useI18n()
const route = useRoute()
const auth = useAuthStore()

const items = computed(() => [
  { path: '/procurement', label: t('procurementNav.workbench'), allowed: true },
  { path: '/sourcing-cases', label: t('procurementNav.sourcing'), allowed: auth.can('procurement:sourcing:read') },
  { path: '/requirements', label: t('procurementNav.requirements'), allowed: auth.can('procurement:requirement:read') },
  { path: '/purchase-orders', label: t('procurementNav.orders'), allowed: auth.can('procurement:order:read') },
].filter((item) => item.allowed))
</script>

<style scoped>
.procurement-nav {
  display: flex;
  gap: 6px;
  padding: 4px;
  margin-bottom: 18px;
  width: fit-content;
  max-width: 100%;
  overflow-x: auto;
  border: 1px solid var(--el-border-color-light);
  border-radius: 10px;
  background: var(--el-fill-color-light);
}
.procurement-nav__item {
  padding: 8px 14px;
  border-radius: 7px;
  color: var(--el-text-color-regular);
  text-decoration: none;
  white-space: nowrap;
}
.procurement-nav__item:hover { color: var(--el-color-primary); }
.procurement-nav__item.active {
  color: var(--el-color-primary);
  background: var(--el-bg-color);
  box-shadow: 0 1px 3px rgb(15 23 42 / 10%);
}
</style>
