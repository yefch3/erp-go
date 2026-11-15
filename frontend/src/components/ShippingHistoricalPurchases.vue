<template>
  <section class="historical-purchases" :aria-label="t('historicalShipping.shippingTitle')" v-loading="loading">
    <div class="heading"><h3>{{ t('historicalShipping.shippingTitle') }}</h3><el-button size="small" :loading="loading" @click="load">{{ t('common.refresh') }}</el-button></div>
    <p>{{ t('historicalShipping.shippingHint') }}</p>
    <el-alert v-if="failed" type="error" :title="t('historicalShipping.failed')" :closable="false" />
    <el-table v-else :data="items" :empty-text="t('historicalShipping.empty')" size="small">
      <el-table-column prop="poNo" :label="t('historicalShipping.poNo')" min-width="150" />
      <el-table-column prop="supplierName" :label="t('purchaseOrderContext.supplier')" min-width="150" />
      <el-table-column prop="exportContractNo" :label="t('historicalShipping.contract')" min-width="160" />
      <el-table-column :label="t('historicalShipping.port')" min-width="180"><template #default="{row}">{{row.destinationPort || '—'}}</template></el-table-column>
      <el-table-column :label="t('purchaseOrderContext.remark')" min-width="260"><template #default="{row}"><div class="remark">{{row.remark || '—'}}</div></template></el-table-column>
    </el-table>
  </section>
</template>
<script setup lang="ts">
import { onMounted, onUnmounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { get, quietErrors } from '../api'
const props = defineProps<{handoffId: string}>()
const { t } = useI18n()
type Row = {poNo: string; supplierName: string; exportContractNo: string; destinationPort: string; remark: string}
const items = ref<Row[]>([]), loading = ref(false), failed = ref(false)
let generation = 0, alive = true
let timer: ReturnType<typeof setInterval> | undefined
async function load() {
  const request = ++generation
  loading.value = true
  failed.value = false
  try {
    const data = await get<{items: Row[]}>(`/shipping/contract-handoffs/${props.handoffId}/historical-purchases`, {}, quietErrors)
    if (alive && request === generation) items.value = data.items ?? []
  } catch {
    if (alive && request === generation) { items.value = []; failed.value = true }
  } finally { if (alive && request === generation) loading.value = false }
}
watch(() => props.handoffId, () => { items.value = []; void load() }, {immediate: true})
onMounted(() => { timer = setInterval(() => { if (!loading.value) void load() }, 15000) })
onUnmounted(() => { alive = false; generation++; clearInterval(timer) })
</script>
<style scoped>
.historical-purchases{margin:18px 0;padding:14px 16px;border:1px solid #d7e5ec;border-radius:10px;background:#fff}
.heading{display:flex;align-items:center;justify-content:space-between;gap:12px}.heading h3{font-size:16px;margin:0}
.historical-purchases>p{color:#718390;font-size:13px;line-height:1.5}
.remark{white-space:pre-wrap;overflow-wrap:anywhere;line-height:1.6}
</style>
