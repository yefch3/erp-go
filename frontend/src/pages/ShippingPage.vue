<template>
  <div class="shipping-workspace">
    <WorkflowPageHeader :title="t('presalesShipping.scheduleTitle')" :description="t('presalesShipping.scheduleHint')">
      <template #actions>
        <el-button v-if="auth.can('shipping:schedule:write')" type="primary" @click="dialogOpen = true">{{ t('shipping.create') }}</el-button>
      </template>
    </WorkflowPageHeader>

    <section class="schedule-workspace">
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
              <span>预计离港（ETD）</span><el-date-picker v-model="filter.etdRange" type="daterange" value-format="YYYY-MM-DD" range-separator="—" />
              <span>预计到港（ETA）</span><el-date-picker v-model="filter.etaRange" type="daterange" value-format="YYYY-MM-DD" range-separator="—" />
            </div>
            <div class="table-scroll">
              <el-table :data="rows" v-loading="loading" @row-dblclick="detail">
                <el-table-column :label="t('shipping.scheduleNo')" width="180"><template #default="{ row }"><el-link type="primary" @click="detail(row)">{{ row.scheduleNo }}</el-link></template></el-table-column>
                <el-table-column prop="customerName" :label="t('shipping.customer')" min-width="140" />
                <el-table-column :label="t('shipping.vesselVoyage')" min-width="160"><template #default="{ row }">{{ row.vesselName }} / {{ row.voyageNo }}</template></el-table-column>
                <el-table-column label="路线" min-width="170"><template #default="{ row }">{{ row.portOfLoading }} → {{ row.portOfDischarge }}</template></el-table-column>
                <el-table-column prop="eta" label="最新预计到港（ETA）" width="175" />
                <el-table-column prop="currentProgress" label="当前进度" min-width="150" />
                <el-table-column label="提醒" min-width="180"><template #default="{ row }"><el-tag v-if="departureNotice(row)" type="warning" class="change-tag">{{departureNotice(row)}}</el-tag><el-tag v-if="row.delayDays > 0" :type="row.delayDays >= 4 ? 'danger' : 'warning'" class="change-tag">到港延后 {{ row.delayDays }} 天</el-tag><el-tag v-if="row.hasTemporaryCall" type="warning" class="change-tag">临时挂港</el-tag><span v-if="!row.delayDays && !row.hasTemporaryCall && !departureNotice(row)">—</span></template></el-table-column>
                <el-table-column :label="t('common.status')" width="110"><template #default="{ row }"><el-tag :type="statusTag(row.status)">{{ shippingStatusLabel(row,t) }}</el-tag></template></el-table-column>
                <el-table-column prop="responsibleName" :label="t('shipping.responsible')" width="120" />
                <el-table-column v-if="auth.can('shipping:schedule:write')" :label="t('common.actions')" fixed="right" width="125" align="center"><template #default="{ row }">
                  <el-dropdown trigger="click" @command="handleRowAction($event, row)">
                    <el-button type="primary" plain @click.stop>{{ t('orders.moreActions') }} ▾</el-button>
                    <template #dropdown><el-dropdown-menu>
                      <el-dropdown-item command="edit" :disabled="['COMPLETED', 'CANCELLED'].includes(row.status)">{{ t('common.edit') }}</el-dropdown-item>
                      <el-dropdown-item command="account">{{ t('shipping.openAccount') }}</el-dropdown-item>
                      <el-dropdown-item command="delete" divided :disabled="row.status !== 'PLANNED'"><span class="danger-action">{{ t('common.delete') }}</span></el-dropdown-item>
                    </el-dropdown-menu></template>
                  </el-dropdown>
                </template></el-table-column>
              </el-table>
            </div>
            <el-empty v-if="!loading && !rows.length" :description="t('shipping.empty')" />
            <el-pagination class="pager" layout="total, sizes, prev, pager, next" :total="total" :page-size="pageSize" :current-page="page" :page-sizes="[10, 20, 50]" @current-change="changePage" @size-change="changeSize" />
          </el-card>
    </section>

    <ShippingScheduleDialog v-model="dialogOpen" :schedule="editing" :handoff="creatingHandoff" @saved="saved" />
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRoute, useRouter } from 'vue-router'
import { get, post } from '../api'
import { ElMessage, ElMessageBox } from 'element-plus'
import ShippingScheduleDialog, { type ContractShippingHandoff } from '../components/ShippingScheduleDialog.vue'
import WorkflowPageHeader from '../components/WorkflowPageHeader.vue'
import { SHIPPING_STATUSES, shippingStatusLabel, statusTag, type ShippingSchedule, type ShippingStatistics } from '../shipping'
import { useAuthStore } from '../stores/auth'

const { t } = useI18n()
const router = useRouter()
const route = useRoute()
const auth = useAuthStore()
const rows = ref<ShippingSchedule[]>([])
const loading = ref(false)
const total = ref(0)
const page = ref(1)
const pageSize = ref(20)
const more = ref(false)
const dialogOpen = ref(false)
const editing = ref<ShippingSchedule>()
const creatingHandoff = ref<ContractShippingHandoff>()
const filter = reactive({ keyword: '', status: 'ACTIVE', portOfLoading: '', portOfDischarge: '', etdRange: [] as string[], etaRange: [] as string[] })
const stats = ref<ShippingStatistics>({ inTransit: '0', arrivingWithin7Days: '0', delayed: '0', temporaryCall: '0' })
const statItems = computed(() => [
  { label: '运输中', value: stats.value.inTransit },
  { label: '7天内到港', value: stats.value.arrivingWithin7Days },
  { label: '已延误', value: stats.value.delayed },
  { label: '临时挂港', value: stats.value.temporaryCall },
])

watch(dialogOpen, (value) => { if (!value) { editing.value = undefined; creatingHandoff.value = undefined } })

async function load() {
  loading.value = true
  try {
    const [data, summary] = await Promise.all([
      get<{ schedules: ShippingSchedule[]; meta: { total: string } }>('/shipping/schedules', {
        page: page.value, page_size: pageSize.value, keyword: filter.keyword, status: filter.status,
        port_of_loading: filter.portOfLoading, port_of_discharge: filter.portOfDischarge,
        etd_from: filter.etdRange?.[0] ?? '', etd_to: filter.etdRange?.[1] ?? '',
        eta_from: filter.etaRange?.[0] ?? '', eta_to: filter.etaRange?.[1] ?? '',
      }),
      get<ShippingStatistics>('/shipping/statistics'),
    ])
    rows.value = data.schedules
    total.value = Number(data.meta.total)
    stats.value = summary
  } finally { loading.value = false }
}

function search() { page.value = 1; void load() }
function changePage(value: number) { page.value = value; void load() }
function changeSize(value: number) { pageSize.value = value; page.value = 1; void load() }
function detail(row: ShippingSchedule) { void router.push(`/shipping/${row.id}`) }
function departureNotice(row:ShippingSchedule){
  if(row.status!=='PLANNED'||!row.etd)return ''
  const target=new Date(`${row.etd}T00:00:00`);if(Number.isNaN(target.getTime()))return ''
  const today=new Date();today.setHours(0,0,0,0)
  const days=Math.round((target.getTime()-today.getTime())/86_400_000)
  if(days<0)return `预计开船已过 ${-days} 天`
  if(days===0)return '预计今天开船'
  return days<=7?`预计 ${days} 天后开船`:''
}
function saved(schedule:ShippingSchedule) { void router.replace(`/shipping/${schedule.id}`) }
async function handleRowAction(command:string,row:ShippingSchedule){
  if(command==='edit'){editing.value=row;dialogOpen.value=true;return}
  if(command==='account'){
    if(!auth.can('procurement:recon:read')){ElMessage.warning(t('shipping.accountPermissionRequired'));return}
    await router.push({path:'/supplier-recon',query:{keyword:row.contractNo}});return
  }
  if(command!=='delete'||row.status!=='PLANNED')return
  try{await ElMessageBox.confirm(t('shipping.deleteScheduleConfirm',{no:row.scheduleNo}),t('shipping.deleteSchedule'),{type:'warning',confirmButtonText:t('common.delete'),cancelButtonText:t('common.cancel')})}catch(action){if(action==='cancel'||action==='close')return;throw action}
  await post(`/shipping/schedules/${row.id}/cancel`,{reason:t('shipping.deletedMistakeReason')})
  ElMessage.success(t('shipping.scheduleDeleted'))
  await load()
}
async function openDelegatedOrder(){
  const id=String(route.query.handoff??'')
  if(!id)return
  const data=await get<{handoff:ContractShippingHandoff}>(`/shipping/contract-handoffs/${id}`)
  if(data.handoff.status==='SCHEDULED'&&data.handoff.scheduleId){await router.replace(`/shipping/${data.handoff.scheduleId}`);return}
  if(data.handoff.status!=='PAYMENT_REQUESTED'){await router.replace('/shipping/schedules');return}
  creatingHandoff.value=data.handoff;dialogOpen.value=true
}
onMounted(async()=>{await load();await openDelegatedOrder()})
</script>

<style scoped>
.handoff-card{margin-bottom:16px}
.shipping-workspace{max-width:1680px;margin:0 auto}.workspace-head{display:flex;align-items:center;justify-content:space-between;gap:18px;margin-bottom:16px;padding:18px 22px;border:1px solid #dfe9e7;border-radius:12px;background:linear-gradient(115deg,#f2faf8 0%,#f8fafc 55%,#f4f7fb 100%)}.head-copy{display:flex;align-items:center;gap:16px}.head-copy :deep(.el-tag){border:0;background:#167d70}.workspace-head h2{margin:0 0 4px;color:#172b4d;font-size:24px}.workspace-head p,.section-title p{margin:0;color:#6b778c}.section-title{display:flex;align-items:flex-start;justify-content:space-between;gap:16px;margin-bottom:16px}.section-title h3{margin:0 0 5px;color:#172b4d}.stats{margin-bottom:14px}.stat-card{border-color:#e6ebf1}.stat-label{color:var(--el-text-color-secondary);font-size:13px}.stat-value{margin-top:6px;font-size:26px;font-weight:600}.schedule-card{border-color:#e6ebf1}.filters{display:grid;grid-template-columns:minmax(220px,1.4fr) minmax(180px,1fr) minmax(150px,.8fr) minmax(150px,.8fr) auto auto;gap:10px;margin-bottom:14px;align-items:center}.date-filters{display:flex;flex-wrap:wrap;gap:10px;margin-bottom:14px;align-items:center}.table-scroll{min-width:0;overflow-x:auto}.table-scroll :deep(.el-table){min-width:1180px}.pager{margin-top:14px;justify-content:flex-end;flex-wrap:wrap}.change-tag{margin-right:5px}
.shipping-workspace{color:#141817}.section-title h3{color:#141817}.stats :deep(.el-col:nth-child(odd) .stat-card){border-top:3px solid #4ac1ff}.stats :deep(.el-col:nth-child(even) .stat-card){border-top:3px solid #1fbf6c}.stat-value{color:#159fdc}.schedule-card :deep(.el-table th.el-table__cell){border-bottom-color:#d9edf5;font-weight:650}.schedule-card :deep(.el-table){--el-table-header-bg-color:#eef9fe;--el-table-header-text-color:#24323a;--el-table-row-hover-bg-color:#f0fbf6}
@media(max-width:1000px){.shipping-workspace{min-width:0}.workspace-head{padding:15px}.workspace-head h2{font-size:21px}.filters{grid-template-columns:1fr 1fr}.filters>*{width:100%}.date-filters :deep(.el-date-editor){max-width:100%}}
@media(max-width:640px){.workspace-head{align-items:flex-start;flex-direction:column}.head-copy{align-items:flex-start;flex-direction:column;gap:9px}.filters{grid-template-columns:1fr}.section-title{flex-direction:column}.stats :deep(.el-card__body){padding:13px}.pager{justify-content:flex-start}}
</style>
