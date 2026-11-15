<template>
  <section class="shipping-purchase-remarks" :aria-label="t('purchaseOrderContext.shippingTitle')" v-loading="loading">
    <div class="heading"><h3>{{ t('purchaseOrderContext.shippingTitle') }}</h3><el-button size="small" :loading="loading" @click="load">{{ t('common.refresh') }}</el-button></div>
    <p>{{ t('purchaseOrderContext.shippingHint') }}</p>
    <el-alert v-if="failed" type="error" :title="t('purchaseOrderContext.loadFailed')" :closable="false"/>
    <el-table v-else :data="items" :empty-text="t('purchaseOrderContext.noOrders')" size="small">
      <el-table-column prop="supplierName" :label="t('purchaseOrderContext.supplier')" width="210"/>
      <el-table-column :label="t('purchaseOrderContext.remark')" min-width="300"><template #default="{row}"><div class="remark">{{ row.purchaseOrderRemark?.trim() ? row.purchaseOrderRemark : t('purchaseOrderContext.emptyRemark') }}</div></template></el-table-column>
    </el-table>
  </section>
</template>
<script setup lang="ts">
import { onMounted, onUnmounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { get, quietErrors } from '../api'
const props = defineProps<{ handoffId: string }>()
const { t } = useI18n()
type Remark = { supplierName: string; purchaseOrderRemark: string }
const items = ref<Remark[]>([])
const loading = ref(false)
const failed = ref(false)
let generation = 0
let alive = true
let timer: ReturnType<typeof setInterval> | undefined

async function load() {
  const request = ++generation
  loading.value = true
  failed.value = false
  try {
    const data = await get<{ items: Remark[] }>(
      `/shipping/contract-handoffs/${props.handoffId}/purchase-remarks`, {}, quietErrors,
    )
    if (alive && request === generation) items.value = data.items ?? []
  } catch {
    if (alive && request === generation) {
      items.value = []
      failed.value = true
    }
  } finally {
    if (alive && request === generation) loading.value = false
  }
}
watch(() => props.handoffId, () => {
  items.value = []
  void load()
}, { immediate: true })
onMounted(() => {
  timer = setInterval(() => { if (!loading.value) void load() }, 15000)
})
onUnmounted(() => {
  alive = false
  generation++
  clearInterval(timer)
})
</script>
<style scoped>
.shipping-purchase-remarks{margin:18px 0;padding:14px 16px;border:1px solid #d7e5ec;border-radius:10px;background:#fff}
.heading{display:flex;align-items:center;justify-content:space-between;gap:12px}.heading h3{font-size:16px;margin:0}
.shipping-purchase-remarks>p{color:#718390;font-size:13px;line-height:1.5}
.remark{white-space:pre-wrap;overflow-wrap:anywhere;line-height:1.6}
</style>
