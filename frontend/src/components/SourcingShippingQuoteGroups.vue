<template>
  <div class="shipping-quote-groups">
    <div class="group-summary">
      <div><b>{{ groups.length }}</b><span>{{ t('presalesShipping.cargoGroups') }}</span></div>
      <div><b>{{ currentQuoteCount }}</b><span>{{ t('presalesShipping.currentQuotes') }}</span></div>
      <div><b>{{ companyCount }}</b><span>{{ t('presalesShipping.quotedCompanies') }}</span></div>
    </div>

    <el-empty v-if="!groups.length" :description="t('presalesShipping.noCargoGroups')" />
    <div v-else class="quote-groups">
      <section v-for="group in groups" :key="group.sourcingLineId" class="cargo-quote-card">
        <header>
          <div>
            <small>{{ t('presalesShipping.productLine', { line: group.lineNo }) }}</small>
            <h3>{{ group.product }}</h3>
            <p>{{ t('presalesShipping.templateSpec') }}：{{ group.specification || t('presalesShipping.noExtraSpec') }}</p>
            <p>{{ t('presalesShipping.customerDemand') }}：{{ group.quantity || '—' }} {{ group.quantityUnit }}</p>
          </div>
          <div class="cargo-meta">
            <el-tag effect="plain">{{ t('presalesShipping.validQuoteCount', { count: group.rows.length }) }}</el-tag>
            <small>{{ t('presalesShipping.sortedByTotal') }}</small>
          </div>
        </header>

        <div v-if="!group.rows.length" class="compact-empty"><span>—</span>{{ t('presalesShipping.noCargoQuote') }}</div>
        <div v-else class="table-scroll">
          <el-table :data="group.rows" style="min-width: 1080px">
            <el-table-column :label="t('presalesShipping.companyAndSubmitter')" min-width="205">
              <template #default="{ row }"><strong>{{ row.carrierForwarder }}</strong><small>{{ row.serviceOptionName || t('presalesShipping.defaultServiceOption') }} · {{ row.createdByName || '—' }}</small></template>
            </el-table-column>
            <el-table-column :label="t('presalesShipping.unitFreight')" min-width="170">
              <template #default="{ row }"><strong class="price">{{ row.currency }} {{ row.unitRate }}</strong><small>{{ basisLabel(row.chargeBasis) }}</small></template>
            </el-table-column>
            <el-table-column :label="t('presalesShipping.totalFreight')" min-width="165">
              <template #default="{ row }"><strong class="price">{{ row.currency }} {{ row.totalFreight }}</strong><el-tag v-if="isLowest(group.rows, row)" size="small" type="success">{{ t('presalesShipping.lowestTotal') }}</el-tag></template>
            </el-table-column>
            <el-table-column :label="t('presalesShipping.route')" min-width="190">
              <template #default="{ row }">{{ row.portOfLoading || '—' }} → {{ row.portOfDischarge || '—' }}</template>
            </el-table-column>
            <el-table-column :label="t('presalesShipping.departureArrival')" min-width="190">
              <template #default="{ row }"><span>{{ row.estimatedDeparture || '—' }}</span><small>→ {{ row.estimatedArrival || '—' }}</small></template>
            </el-table-column>
            <el-table-column :label="t('presalesShipping.quoteValidity')" min-width="155">
              <template #default="{ row }"><span>{{ row.quotedAt || '—' }}</span><small>{{ t('presalesShipping.validToShort') }} {{ row.validUntil || '—' }}</small></template>
            </el-table-column>
            <el-table-column :label="t('common.actions')" width="90" fixed="right">
              <template #default="{ row }"><el-button link @click="openHistory(row)">{{ t('presalesShipping.history') }}</el-button></template>
            </el-table-column>
          </el-table>
        </div>
      </section>
    </div>
    <el-dialog v-model="historyOpen" :title="historyTitle" width="min(980px, 94vw)" append-to-body>
      <el-alert type="info" :closable="false" :title="t('presalesShipping.historyHint')" />
      <el-table :data="historyRows" style="margin-top:14px">
        <el-table-column :label="t('presalesShipping.quotedAt')" prop="quotedAt" width="120" />
        <el-table-column :label="t('presalesShipping.unitFreight')" width="150"><template #default="{ row }">{{ row.currency }} {{ row.unitRate }}</template></el-table-column>
        <el-table-column :label="t('presalesShipping.totalFreight')" width="150"><template #default="{ row }">{{ row.currency }} {{ row.totalFreight }}</template></el-table-column>
        <el-table-column :label="t('presalesShipping.departureArrival')" min-width="190"><template #default="{ row }">{{ row.estimatedDeparture || '—' }} → {{ row.estimatedArrival || '—' }}</template></el-table-column>
        <el-table-column :label="t('presalesShipping.submittedBy')" prop="createdByName" min-width="130" />
      </el-table>
    </el-dialog>
  </div>
</template>

<script setup lang="ts">
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'

const props = defineProps<{ collaboration?: Record<string, any> }>()
const { t } = useI18n()
const historyOpen = ref(false)
const historyRows = ref<any[]>([])
const historyTitle = ref('')

const allRows = computed(() => {
  const options = props.collaboration?.options || []
  return options.flatMap((option: any) => (option.lines || []).map((line: any) => ({ ...line, ...option, id: line.id, optionId: option.id })))
})
const groups = computed(() => {
  const cargo = props.collaboration?.cargoItems || []
  const latest = new Map<string, any>()
  for (const row of allRows.value) {
    const key = `${String(row.sourcingLineId)}:${String(row.createdBy)}:${String(row.carrierForwarder || '').trim().toLowerCase()}:${String(row.serviceOptionName || '').trim().toLowerCase()}`
    if (!latest.has(key)) latest.set(key, row)
  }
  return cargo.map((item: any) => {
    const rows = [...latest.values()]
      .filter((row: any) => String(row.sourcingLineId) === String(item.sourcingLineId))
      .sort((a: any, b: any) => Number(a.totalFreight || 0) - Number(b.totalFreight || 0))
    return {
      sourcingLineId: item.sourcingLineId,
      lineNo: item.lineNo,
      product: item.product,
      specification: [item.materialStandard, item.grade, item.thickness, item.width, item.lengthOrForm, item.surfaceRequirement, item.packaging].filter(Boolean).join(' · '),
      quantity: item.quantity,
      quantityUnit: item.quantityUnit,
      rows,
    }
  })
})
const currentQuoteCount = computed(() => groups.value.reduce((sum: number, group: any) => sum + group.rows.length, 0))
const companyCount = computed(() => new Set(groups.value.flatMap((group: any) => group.rows.map((row: any) => String(row.carrierForwarder).trim().toLowerCase()))).size)

function basisLabel(value: string) {
  return ({ PER_TON: t('presalesShipping.perTon'), PER_CONTAINER: t('presalesShipping.perContainer'), PER_PIECE: t('presalesShipping.perPiece'), PER_SHIPMENT: t('presalesShipping.perShipment'), FIXED: t('presalesShipping.fixedAmount') } as Record<string, string>)[value] || value
}
function isLowest(rows: any[], row: any) {
  const comparable = rows.filter((item) => item.currency === row.currency).map((item) => Number(item.totalFreight)).filter(Number.isFinite)
  return comparable.length > 1 && Number(row.totalFreight) === Math.min(...comparable)
}
function openHistory(row: any) {
  historyRows.value = allRows.value.filter((item: any) => String(item.sourcingLineId) === String(row.sourcingLineId) && String(item.createdBy) === String(row.createdBy) && String(item.carrierForwarder).trim().toLowerCase() === String(row.carrierForwarder).trim().toLowerCase() && String(item.serviceOptionName || '').trim().toLowerCase() === String(row.serviceOptionName || '').trim().toLowerCase())
  historyTitle.value = `${row.product} · ${row.carrierForwarder} · ${row.serviceOptionName || t('presalesShipping.defaultServiceOption')} · ${t('presalesShipping.history')}`
  historyOpen.value = true
}
</script>

<style scoped>
.group-summary{display:grid;grid-template-columns:repeat(3,minmax(0,1fr));gap:12px;margin:14px 0}.group-summary>div{display:flex;align-items:baseline;gap:8px;padding:12px 15px;border:1px solid #e1e8ed;border-radius:9px;background:#f8fafb}.group-summary b{color:#17324d;font-size:22px}.group-summary span{color:#718395;font-size:13px}.quote-groups{display:grid;gap:12px}.cargo-quote-card{overflow:hidden;border:1px solid #dfe7ec;border-radius:12px;background:#fff}.cargo-quote-card>header{display:flex;align-items:center;justify-content:space-between;gap:18px;padding:12px 18px;border-bottom:1px solid #e0e9ec;background:#f3f9f8}.cargo-quote-card h3{margin:3px 0;color:#17324d;font-size:17px}.cargo-quote-card p,.cargo-quote-card small{display:block;margin:2px 0 0;color:#718395}.cargo-meta{display:flex;align-items:flex-end;flex-direction:column;gap:6px}.compact-empty{display:flex;align-items:center;justify-content:center;gap:8px;min-height:58px;padding:10px;color:#98a3af;font-size:14px}.compact-empty span{color:#c1c8d0}.price{margin-right:7px;color:#17324d}.table-scroll{overflow-x:auto}.table-scroll small{display:block;margin-top:4px;color:#82909f}.cargo-quote-card :deep(.el-table__row:hover>td){background:#f7fbfb!important}
@media(max-width:720px){.group-summary{grid-template-columns:1fr}.cargo-quote-card>header{align-items:flex-start;flex-direction:column}.cargo-meta{align-items:flex-start}}
</style>
