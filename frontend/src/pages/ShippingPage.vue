<template>
  <div>
    <div class="page-head">
      <h2>{{ t('shipping.title') }}</h2>
    </div>

    <el-card shadow="never" class="foundation-card" v-loading="loading">
      <el-result
        :icon="error ? 'error' : 'success'"
        :title="error ? t('shipping.unavailable') : t('shipping.foundationReady')"
        :sub-title="t('shipping.description')"
      >
        <template v-if="status" #extra>
          <el-tag type="success" effect="plain">
            {{ t('shipping.schemaVersion') }}：{{ status.schemaVersion }}
          </el-tag>
        </template>
      </el-result>
    </el-card>
  </div>
</template>

<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { get } from '../api'

interface ModuleStatus {
  module: string
  status: string
  schemaVersion: number
}

const { t } = useI18n()
const loading = ref(true)
const error = ref(false)
const status = ref<ModuleStatus>()

onMounted(async () => {
  try {
    status.value = await get<ModuleStatus>('/shipping/status')
  } catch {
    error.value = true
  } finally {
    loading.value = false
  }
})
</script>

<style scoped>
.page-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin-bottom: 16px;
}
.page-head h2 {
  margin: 0;
}
.foundation-card {
  min-height: 360px;
}
</style>
