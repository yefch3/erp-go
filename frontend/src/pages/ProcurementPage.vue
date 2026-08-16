<template>
  <div class="page">
    <div class="page-head">
      <div>
        <h1>{{ t('procurementWorkbench.title') }}</h1>
        <p>{{ t('procurementWorkbench.subtitle') }}</p>
      </div>
    </div>

    <div class="stages">
      <router-link
        v-for="stage in stages"
        :key="stage.path"
        :to="stage.path"
        class="stage"
      >
        <div class="stage__step">{{ stage.step }}</div>
        <div class="stage__body">
          <strong>{{ stage.title }}</strong>
          <span>{{ stage.description }}</span>
        </div>
        <span class="stage__arrow" aria-hidden="true">›</span>
      </router-link>
    </div>

    <el-alert
      :title="t('procurementWorkbench.flowHint')"
      type="info"
      :closable="false"
      show-icon
    />
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { useAuthStore } from '../stores/auth'

const { t } = useI18n()
const auth = useAuthStore()

const stages = computed(() => [
  {
    path: '/sourcing-cases', permission: 'procurement:sourcing:read', step: '1',
    title: t('procurementWorkbench.sourcingTitle'), description: t('procurementWorkbench.sourcingDescription'),
  },
  {
    path: '/requirements', permission: 'procurement:requirement:read', step: '2',
    title: t('procurementWorkbench.requirementsTitle'), description: t('procurementWorkbench.requirementsDescription'),
  },
  {
    path: '/purchase-orders', permission: 'procurement:order:read', step: '3',
    title: t('procurementWorkbench.ordersTitle'), description: t('procurementWorkbench.ordersDescription'),
  },
].filter((stage) => auth.can(stage.permission)))
</script>

<style scoped>
.page { padding: 24px; }
.page-head { margin-bottom: 18px; }
h1 { margin: 0; font-size: 24px; }
.page-head p { margin: 6px 0 0; color: var(--el-text-color-secondary); }
.stages { display: grid; gap: 12px; margin-bottom: 20px; }
.stage {
  display: flex;
  align-items: center;
  gap: 16px;
  padding: 18px;
  border: 1px solid var(--el-border-color-light);
  border-radius: 10px;
  color: inherit;
  text-decoration: none;
  background: var(--el-bg-color);
  transition: border-color .15s, box-shadow .15s;
}
.stage:hover { border-color: var(--el-color-primary-light-5); box-shadow: 0 4px 14px rgb(15 23 42 / 7%); }
.stage__step {
  display: grid;
  place-items: center;
  width: 34px;
  height: 34px;
  flex: 0 0 34px;
  border-radius: 50%;
  color: var(--el-color-primary);
  background: var(--el-color-primary-light-9);
  font-weight: 700;
}
.stage__body { display: flex; flex-direction: column; gap: 5px; min-width: 0; }
.stage__body span { color: var(--el-text-color-secondary); }
.stage__arrow { margin-left: auto; color: var(--el-text-color-placeholder); font-size: 26px; }
</style>
