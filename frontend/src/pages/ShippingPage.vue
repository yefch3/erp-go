<template>
  <div class="shipping-workspace">
    <header class="workspace-head">
      <div class="head-copy">
        <el-tag effect="dark" round>{{ t('presalesShipping.executionTag') }}</el-tag>
        <div>
          <h2>{{ t('presalesShipping.scheduleTitle') }}</h2>
          <p>{{ t('presalesShipping.scheduleHint') }}</p>
        </div>
      </div>
      <el-button plain @click="router.push('/shipping/sourcing')">{{ t('presalesShipping.backToPresales') }}</el-button>
    </header>

    <section class="schedule-workspace">
          <el-card v-if="handoffs.length" shadow="never" class="schedule-card handoff-card">
            <div class="section-title">
              <div><h3>{{ t('shipping.contractHandoffs') }}</h3><p>{{ t('shipping.contractHandoffsHint') }}</p></div>
              <el-tag type="warning">{{ handoffs.filter(item => item.status === 'PENDING').length }}</el-tag>
            </div>
            <el-table :data="handoffs">
              <el-table-column prop="contractNo" :label="t('shipping.contractNo')" width="170" />
              <el-table-column prop="customerName" :label="t('shipping.customer')" min-width="130" />
              <el-table-column :label="t('shipping.freightBatch')" min-width="180"><template #default="{ row }">#{{ row.batchNo }} · {{ row.carrierForwarder || t('shipping.customerManaged') }}</template></el-table-column>
              <el-table-column :label="t('shipping.route')" min-width="190"><template #default="{ row }">{{ row.portOfLoading || '—' }} → {{ row.portOfDischarge || '—' }}</template></el-table-column>
              <el-table-column :label="t('shipping.estimatedSailing')" width="210"><template #default="{ row }">{{ row.estimatedDeparture || '—' }} → {{ row.estimatedArrival || '—' }}</template></el-table-column>
              <el-table-column :label="t('common.status')" width="130"><template #default="{ row }"><el-tag :type="row.status === 'PENDING' ? 'warning' : 'info'">{{ t(`shipping.handoffStatuses.${row.status}`) }}</el-tag></template></el-table-column>
              <el-table-column v-if="auth.can('shipping:schedule:write')" :label="t('common.actions')" width="130"><template #default="{ row }"><el-button v-if="row.status === 'PENDING'" link type="primary" @click="openHandoff(row)">{{ t('shipping.createFromHandoff') }}</el-button></template></el-table-column>
            </el-table>
          </el-card>
          <div class="section-title">
            <div>
              <h3>{{ t('shipping.title') }}</h3>
              <p>{{ t('presalesShipping.scheduleHint') }}</p>
            </div>
            <el-button v-if="auth.can('shipping:schedule:write')" type="primary" @click="dialogOpen = true">
              {{ t('shipping.create') }}
            </el-button>
          </div>

          <el-row :gutter="14" class="stats">
            <el-col v-for="item in statItems" :key="item.label" :xs="12" :sm="6">
              <el-card shadow="never" class="stat-card">
                <div class="stat-label">{{ item.label }}</div>
                <div class="stat-value">{{ item.value }}</div>
              </el-card>
            </el-col>
          </el-row>

          <el-card shadow="never" class="schedule-card">
            <div class="filters">
              <el-input v-model="filter.keyword" clearable :placeholder="t('shipping.searchPlaceholder')" @keyup.enter="search" @clear="search" />
              <el-select v-model="filter.status" :placeholder="t('shipping.allStatuses')">
                <el-option value="ACTIVE" label="进行中" />
                <el-option value="ARCHIVED" label="历史记录（已完成/已取消）" />
                <el-option value="" label="全部状态" />
                <el-option v-for="statusOption in SHIPPING_STATUSES" :key="statusOption" :value="statusOption" :label="t(`shipping.statuses.${statusOption}`)" />
              </el-select>
              <el-input v-model="filter.portOfLoading" clearable :placeholder="t('shipping.loadingPort')" />
              <el-input v-model="filter.portOfDischarge" clearable :placeholder="t('shipping.dischargePort')" />
              <el-button @click="more = !more">{{ t('shipping.dateFilters') }}</el-button>
              <el-button type="primary" @click="search">{{ t('common.query') }}</el-button>
            </div>
            <div v-if="more" class="date-filters">
              <span>ETD</span><el-date-picker v-model="filter.etdRange" type="daterange" value-format="YYYY-MM-DD" range-separator="—" />
              <span>ETA</span><el-date-picker v-model="filter.etaRange" type="daterange" value-format="YYYY-MM-DD" range-separator="—" />
            </div>
            <div class="table-scroll">
              <el-table :data="rows" v-loading="loading" @row-dblclick="detail">
                <el-table-column :label="t('shipping.scheduleNo')" width="180"><template #default="{ row }"><el-link type="primary" @click="detail(row)">{{ row.scheduleNo }}</el-link></template></el-table-column>
                <el-table-column prop="customerName" :label="t('shipping.customer')" min-width="140" />
                <el-table-column :label="t('shipping.vesselVoyage')" min-width="160"><template #default="{ row }">{{ row.vesselName }} / {{ row.voyageNo }}</template></el-table-column>
                <el-table-column label="路线" min-width="170"><template #default="{ row }">{{ row.portOfLoading }} → {{ row.portOfDischarge }}</template></el-table-column>
                <el-table-column prop="eta" label="最新ETA" width="115" />
                <el-table-column prop="currentProgress" label="当前进度" min-width="150" />
                <el-table-column label="变化" min-width="160"><template #default="{ row }"><el-tag v-if="row.delayDays > 0" :type="row.delayDays >= 4 ? 'danger' : 'warning'" class="change-tag">+{{ row.delayDays }}天</el-tag><el-tag v-if="row.hasTemporaryCall" type="warning" class="change-tag">临时挂港</el-tag><span v-if="!row.delayDays && !row.hasTemporaryCall">—</span></template></el-table-column>
                <el-table-column :label="t('common.status')" width="110"><template #default="{ row }"><el-tag :type="statusTag(row.status)">{{ t(`shipping.statuses.${row.status}`) }}</el-tag></template></el-table-column>
                <el-table-column prop="responsibleName" :label="t('shipping.responsible')" width="120" />
                <el-table-column v-if="auth.can('shipping:schedule:write')" :label="t('common.actions')" fixed="right" width="90"><template #default="{ row }"><el-button link type="primary" :disabled="['COMPLETED', 'CANCELLED'].includes(row.status)" @click.stop="editing = row; dialogOpen = true">{{ t('common.edit') }}</el-button></template></el-table-column>
              </el-table>
            </div>
            <el-empty v-if="!loading && !rows.length" :description="t('shipping.empty')" />
            <el-pagination class="pager" layout="total, sizes, prev, pager, next" :total="total" :page-size="pageSize" :current-page="page" :page-sizes="[10, 20, 50]" @current-change="changePage" @size-change="changeSize" />
          </el-card>
    </section>

    <ShippingScheduleDialog v-model="dialogOpen" :schedule="editing" :handoff="selectedHandoff" @saved="saved" />
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRouter } from 'vue-router'
import { get } from '../api'
import ShippingScheduleDialog, { type ContractShippingHandoff } from '../components/ShippingScheduleDialog.vue'
import { SHIPPING_STATUSES, statusTag, type ShippingSchedule, type ShippingStatistics } from '../shipping'
import { useAuthStore } from '../stores/auth'

const { t } = useI18n()
const router = useRouter()
const auth = useAuthStore()
const rows = ref<ShippingSchedule[]>([])
const loading = ref(false)
const total = ref(0)
const page = ref(1)
const pageSize = ref(20)
const more = ref(false)
const dialogOpen = ref(false)
const editing = ref<ShippingSchedule>()
const selectedHandoff = ref<ContractShippingHandoff>()
const handoffs = ref<ContractShippingHandoff[]>([])
const filter = reactive({ keyword: '', status: 'ACTIVE', portOfLoading: '', portOfDischarge: '', etdRange: [] as string[], etaRange: [] as string[] })
const stats = ref<ShippingStatistics>({ inTransit: '0', arrivingWithin7Days: '0', delayed: '0', temporaryCall: '0' })
const statItems = computed(() => [
  { label: '运输中', value: stats.value.inTransit },
  { label: '7天内到港', value: stats.value.arrivingWithin7Days },
  { label: '已延误', value: stats.value.delayed },
  { label: '临时挂港', value: stats.value.temporaryCall },
])

watch(dialogOpen, (value) => { if (!value) { editing.value = undefined; selectedHandoff.value = undefined } })

async function load() {
  loading.value = true
  try {
    const [data, summary, handoffData] = await Promise.all([
      get<{ schedules: ShippingSchedule[]; meta: { total: string } }>('/shipping/schedules', {
        page: page.value, page_size: pageSize.value, keyword: filter.keyword, status: filter.status,
        port_of_loading: filter.portOfLoading, port_of_discharge: filter.portOfDischarge,
        etd_from: filter.etdRange?.[0] ?? '', etd_to: filter.etdRange?.[1] ?? '',
        eta_from: filter.etaRange?.[0] ?? '', eta_to: filter.etaRange?.[1] ?? '',
      }),
      get<ShippingStatistics>('/shipping/statistics'),
      get<{handoffs:ContractShippingHandoff[]}>('/shipping/contract-handoffs'),
    ])
    rows.value = data.schedules
    total.value = Number(data.meta.total)
    stats.value = summary
    handoffs.value = handoffData.handoffs ?? []
  } finally { loading.value = false }
}

function search() { page.value = 1; void load() }
function changePage(value: number) { page.value = value; void load() }
function changeSize(value: number) { pageSize.value = value; page.value = 1; void load() }
function detail(row: ShippingSchedule) { void router.push(`/shipping/${row.id}`) }
function openHandoff(row: ContractShippingHandoff) { selectedHandoff.value = row; editing.value = undefined; dialogOpen.value = true }
function saved() { void load() }
onMounted(load)
</script>

<style scoped>
.handoff-card{margin-bottom:16px}
.shipping-workspace{max-width:1680px;margin:0 auto}.workspace-head{display:flex;align-items:center;justify-content:space-between;gap:18px;margin-bottom:16px;padding:18px 22px;border:1px solid #dfe9e7;border-radius:12px;background:linear-gradient(115deg,#f2faf8 0%,#f8fafc 55%,#f4f7fb 100%)}.head-copy{display:flex;align-items:center;gap:16px}.head-copy :deep(.el-tag){border:0;background:#167d70}.workspace-head h2{margin:0 0 4px;color:#172b4d;font-size:24px}.workspace-head p,.section-title p{margin:0;color:#6b778c}.section-title{display:flex;align-items:flex-start;justify-content:space-between;gap:16px;margin-bottom:16px}.section-title h3{margin:0 0 5px;color:#172b4d}.stats{margin-bottom:14px}.stat-card{border-color:#e6ebf1}.stat-label{color:var(--el-text-color-secondary);font-size:13px}.stat-value{margin-top:6px;font-size:26px;font-weight:600}.schedule-card{border-color:#e6ebf1}.filters{display:grid;grid-template-columns:minmax(220px,1.4fr) minmax(180px,1fr) minmax(150px,.8fr) minmax(150px,.8fr) auto auto;gap:10px;margin-bottom:14px;align-items:center}.date-filters{display:flex;flex-wrap:wrap;gap:10px;margin-bottom:14px;align-items:center}.table-scroll{min-width:0;overflow-x:auto}.table-scroll :deep(.el-table){min-width:1180px}.pager{margin-top:14px;justify-content:flex-end;flex-wrap:wrap}.change-tag{margin-right:5px}
@media(max-width:1000px){.shipping-workspace{min-width:0}.workspace-head{padding:15px}.workspace-head h2{font-size:21px}.filters{grid-template-columns:1fr 1fr}.filters>*{width:100%}.date-filters :deep(.el-date-editor){max-width:100%}}
@media(max-width:640px){.workspace-head{align-items:flex-start;flex-direction:column}.head-copy{align-items:flex-start;flex-direction:column;gap:9px}.filters{grid-template-columns:1fr}.section-title{flex-direction:column}.stats :deep(.el-card__body){padding:13px}.pager{justify-content:flex-start}}
</style>
