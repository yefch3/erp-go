<template>
  <div>
    <h2>{{ t(String(route.meta.titleKey)) }}</h2>
    <el-empty :description="t('basicData.laterBatch')" />
  </div>
</template>

<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import { useRoute } from 'vue-router'

const { t } = useI18n()
const route = useRoute()
</script>
