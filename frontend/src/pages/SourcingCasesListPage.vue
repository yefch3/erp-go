<template>
  <div :class="['page', isSalesView ? 'page--sales' : 'page--procurement']">
    <header class="page-head">
      <div class="page-heading">
        <span class="eyebrow">{{ pageEyebrow }}</span>
        <div class="title-row"><h1>{{ pageTitle }}</h1><span class="perspective-label">{{ isSalesView ? '销售视角' : '采购视角' }}</span></div>
        <p>{{ pageSubtitle }}</p>
      </div>
    </header>
    <section class="list-card">
      <div class="context-bar">
        <div><span class="context-dot"></span><strong>{{ isSalesView ? '客户协作台账' : '采购执行台账' }}</strong><span>{{ isSalesView ? '跟踪客户需求到报价反馈的全过程' : '集中处理询价、比价、经理方案与补充任务' }}</span></div>
        <small>{{ isSalesView ? '采购底价与供应商操作仅在采购侧可见' : '客户沟通与报价反馈由销售侧跟进' }}</small>
      </div>
      <nav v-if="!isSalesView" class="stage-nav" aria-label="寻源阶段筛选">
        <button v-for="item in procurementStages" :key="item.value" type="button" :class="{ active: status === item.value }" @click="setStatus(item.value)"><span>{{ item.label }}</span><small>{{ item.hint }}</small></button>
      </nav>
      <div class="toolbar">
        <el-input v-model="keyword" :placeholder="t('sourcing.search')" clearable @keyup.enter="reload" />
        <el-select v-model="status" clearable :placeholder="isSalesView ? '全部客户进度' : '全部寻源阶段'" @change="reload"><el-option v-for="item in statuses" :key="item" :value="item" :label="t(`sourcing.statuses.${item}`)" /></el-select>
        <el-button type="primary" @click="reload">{{ t('common.query') }}</el-button>
      </div>
      <el-table v-if="isSalesView" v-loading="loading" class="sales-table" :data="rows" @row-click="handleRowClick">
        <el-table-column label="客户询盘" min-width="300"><template #default="{ row }"><strong class="customer-name">{{ row.customerName || '未关联客户' }}</strong><small>{{ row.caseNo }} · {{ row.title || '未命名询盘' }}</small></template></el-table-column>
        <el-table-column label="协作进度" min-width="240"><template #default="{ row }"><el-tag effect="light" :type="stageTagType(row.status)">{{ salesStageLabel(row) }}</el-tag><small class="next-action">{{ waitingForRow(row) }}</small></template></el-table-column>
        <el-table-column label="销售关注" min-width="190"><template #default="{ row }"><span>{{ salesAttention(row) }}</span><el-tag v-if="isStale(row)" size="small" type="warning" class="stale-tag">久未更新</el-tag></template></el-table-column>
        <el-table-column prop="ownerName" label="销售负责人" width="140" />
        <el-table-column label="最近进展" width="185"><template #default="{ row }">{{ formatTime(row.updatedAt) }}</template></el-table-column>
        <el-table-column :label="t('common.actions')" width="112" fixed="right" align="right"><template #default="{ row }">
          <span class="row-actions" @mousedown.stop @click.stop>
            <el-dropdown trigger="click" @command="handleAction($event,row)">
              <el-button link type="primary" aria-label="More actions" @click.stop>•••</el-button>
              <template #dropdown><el-dropdown-menu>
                <el-dropdown-item command="view">{{ t('sourcing.withdraw.view') }}</el-dropdown-item>
                <el-dropdown-item v-if="canWithdraw(row)" command="withdraw" divided class="danger-item">{{ t('sourcing.withdraw.action') }}</el-dropdown-item>
              </el-dropdown-menu></template>
            </el-dropdown>
          </span>
        </template></el-table-column>
        <template #empty>{{ t('sourcing.empty') }}</template>
      </el-table>
      <el-table v-else v-loading="loading" class="procurement-table" :data="rows" stripe @row-click="openCase">
        <el-table-column label="寻源任务" min-width="245"><template #default="{ row }"><strong>{{ row.caseNo }}</strong><small>{{ row.title || '未命名询盘' }}</small></template></el-table-column>
        <el-table-column label="客户需求" min-width="180"><template #default="{ row }">{{ row.customerName || '—' }}</template></el-table-column>
        <el-table-column label="寻源阶段" width="165"><template #default="{ row }"><el-tag effect="dark" :type="stageTagType(row.status)">{{ procurementStageLabel(row) }}</el-tag></template></el-table-column>
        <el-table-column label="下一步采购动作" min-width="220"><template #default="{ row }"><strong class="procurement-next">{{ waitingForRow(row) }}</strong></template></el-table-column>
        <el-table-column label="补充任务" min-width="155"><template #default="{ row }"><el-button v-if="Number(row.myOpenReworkCount)>0" link type="warning" @click.stop="openReworks(row)">待我处理 {{ row.myOpenReworkCount }}</el-button><small v-if="Number(row.openReworkCount)>Number(row.myOpenReworkCount)">团队另有 {{ Number(row.openReworkCount)-Number(row.myOpenReworkCount) }} 项</small><span v-if="!Number(row.openReworkCount)">—</span></template></el-table-column>
        <el-table-column prop="ownerName" label="负责销售" width="140" />
        <el-table-column label="最近更新" width="175"><template #default="{ row }">{{ formatTime(row.updatedAt) }}</template></el-table-column>
        <el-table-column label="异常" width="110"><template #default="{ row }"><el-tag v-if="isStale(row)" type="warning">{{ t('sourcing.stale') }}</el-tag><span v-else>—</span></template></el-table-column>
        <el-table-column label="操作" width="112" fixed="right" align="right"><template #default="{ row }"><el-button link type="primary" @click.stop="openCase(row)">进入寻源 →</el-button></template></el-table-column>
        <template #empty>{{ t('sourcing.empty') }}</template>
      </el-table>
      <el-pagination class="pager" layout="total, prev, pager, next" :total="total" :page-size="pageSize" v-model:current-page="page" @current-change="load" />
    </section>
    <el-dialog v-model="withdrawOpen" :title="t('sourcing.withdraw.title')" width="520px" destroy-on-close>
      <el-alert type="warning" :closable="false" show-icon :title="t('sourcing.withdraw.warning')" />
      <p class="withdraw-case">{{ selectedCase?.caseNo }} · {{ selectedCase?.customerName }}</p>
      <el-form label-position="top">
        <el-form-item :label="t('sourcing.withdraw.reason')" required>
          <el-input v-model="withdrawReason" type="textarea" :rows="4" maxlength="500" show-word-limit :placeholder="t('sourcing.withdraw.placeholder')" />
        </el-form-item>
      </el-form>
      <template #footer><el-button @click="withdrawOpen=false">{{ t('common.cancel') }}</el-button><el-button type="danger" :loading="withdrawing" :disabled="!withdrawReason.trim()" @click="submitWithdrawal">{{ t('sourcing.withdraw.confirm') }}</el-button></template>
    </el-dialog>
  </div>
</template>

<script setup lang="ts">
import { computed, onUnmounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRoute, useRouter } from 'vue-router'
import { ElMessage } from 'element-plus'
import { get, post } from '../api'
import { onLive } from '../live'
import { useAuthStore } from '../stores/auth'

interface SourcingCase { id:string; caseNo:string; customerName:string; title:string; ownerId:string; ownerName:string; status:string; handoffStatus:string; openReworkCount:number|string; myOpenReworkCount:number|string; updatedAt:string }
const { t } = useI18n()
const route = useRoute()
const router = useRouter()
const auth = useAuthStore()
const isSalesView = computed(() => route.path.startsWith('/sales/'))
const isPendingView = computed(() => route.path === '/procurement/sourcing/pending')
const pageEyebrow = computed(() => isSalesView.value ? 'CUSTOMER INQUIRIES' : 'PROCUREMENT SOURCING')
const pageTitle = computed(() => isSalesView.value ? '客户询盘' : isPendingView.value ? '待接单任务' : '售前询价')
const pageSubtitle = computed(() => isSalesView.value
  ? '查看客户需求、采购进度和客户报价；供应商询价与成本操作由采购侧完成。'
  : isPendingView.value
    ? '查看销售已提交、等待采购接收或退回补充的客户需求。'
    : '跟进工厂询价、供应商报价比较和成本方案，并将结果交回销售。')
// 进行中的项目作为默认视图，同时允许用户主动找回已生成报价单或已取消的历史项目。
const statuses = ['REVIEWING','SOURCING','QUOTES_RECEIVED','COSTING','CUSTOMER_QUOTE_CREATED','CANCELLED']
const procurementStages = [
  { value: '', label: '全部', hint: '所有项目' },
  { value: 'REVIEWING', label: '待接单', hint: '接收或退回补充' },
  { value: 'SOURCING', label: '询价中', hint: '等待供应商回复' },
  { value: 'QUOTES_RECEIVED', label: '比价中', hint: '已收到报价' },
  { value: 'COSTING', label: '成本方案', hint: '形成报价依据' },
]
const rows=ref<SourcingCase[]>([]),total=ref(0),page=ref(1),keyword=ref(''),status=ref(isPendingView.value ? 'REVIEWING' : ''),loading=ref(false)
const pageSize=20
const withdrawOpen=ref(false),withdrawing=ref(false),withdrawReason=ref(''),selectedCase=ref<SourcingCase|null>(null)

// load 只负责列表查询；详情操作全部放在独立详情页，避免列表再次变成大型弹窗。
async function load(){loading.value=true;try{const endpoint=isSalesView.value?'/sales-inquiries':'/sourcing-cases';const response=await get<{sourcingCases:SourcingCase[];meta:{total:number}}>(endpoint,{page:page.value,page_size:pageSize,keyword:keyword.value,status:status.value});rows.value=response.sourcingCases??[];total.value=Number(response.meta?.total??0)}finally{loading.value=false}}
function reload(){page.value=1;void load()}
function setStatus(value:string){status.value=value;reload()}
function detailPath(caseID:string){return isSalesView.value ? `/sales/inquiries/${caseID}` : `/procurement/sourcing/${caseID}`}
function openCase(row:SourcingCase){void router.push(detailPath(row.id))}
function handleRowClick(row:SourcingCase,_column:unknown,event:Event){if((event.target as HTMLElement|null)?.closest('.row-actions'))return;openCase(row)}
function canWithdraw(row:SourcingCase){return isSalesView.value&&auth.can('sales:inquiry:write')&&auth.owns(row.ownerId)&&!['INTAKE_PENDING','CANCELLED'].includes(row.status)&&row.handoffStatus!=='SALES_WITHDRAWN'}
function handleAction(command:string,row:SourcingCase){if(command==='view'){openCase(row);return}selectedCase.value=row;withdrawReason.value='';withdrawOpen.value=true}
async function submitWithdrawal(){if(!selectedCase.value||!withdrawReason.value.trim())return;withdrawing.value=true;try{await post(`/sales-inquiries/${selectedCase.value.id}/withdraw`,{reason:withdrawReason.value.trim()});ElMessage.success(t('sourcing.withdraw.success'));withdrawOpen.value=false;await load()}finally{withdrawing.value=false}}
function openReworks(row:SourcingCase){void router.push({path:detailPath(row.id),query:{tab:'plans'}})}
function formatTime(value:string){return value?new Date(value).toLocaleString():'—'}
function isStale(row:SourcingCase){return Date.now()-new Date(row.updatedAt).getTime()>7*86400000}
const knownStatuses = new Set(['REVIEWING','SOURCING','QUOTES_RECEIVED','COSTING','CUSTOMER_QUOTE_CREATED','CANCELLED'])
function normalizedStatus(value?:string){return value && knownStatuses.has(value) ? value : 'UNKNOWN'}
function statusLabel(value?:string){const statusKey=normalizedStatus(value);return statusKey==='UNKNOWN'?t('sourcing.unknownStatus'):t(`sourcing.statuses.${statusKey}`)}
function waitingFor(value?:string){return t(`sourcing.waiting.${normalizedStatus(value)}`)}
function stageTagType(value?:string){return ({REVIEWING:'info',SOURCING:'warning',QUOTES_RECEIVED:'primary',COSTING:'success',CUSTOMER_QUOTE_CREATED:'success',CANCELLED:'info'} as Record<string,'primary'|'success'|'warning'|'info'>)[String(value)]||'info'}
function salesStageLabel(row:SourcingCase){if(row.handoffStatus==='PROCUREMENT_PLAN_READY')return '采购方案待提交';if(row.handoffStatus==='PROCUREMENT_PLAN_SUBMITTED')return '采购方案已收到';return ({REVIEWING:'等待采购接单',SOURCING:'采购询价中',QUOTES_RECEIVED:'采购比价中',COSTING:'采购核算中',CUSTOMER_QUOTE_CREATED:'客户报价中',CANCELLED:'已结束'} as Record<string,string>)[String(row.status)]||statusLabel(row.status)}
function procurementStageLabel(row:SourcingCase){if(row.handoffStatus==='PROCUREMENT_PLAN_READY')return '经理方案待提交';if(row.handoffStatus==='PROCUREMENT_PLAN_SUBMITTED')return '经理方案已提交';return statusLabel(row.status)}
function waitingForRow(row:SourcingCase){if(row.handoffStatus==='PROCUREMENT_PLAN_READY')return isSalesView.value?'等待采购经理提交方案':'将统一方案提交负责销售';if(row.handoffStatus==='PROCUREMENT_PLAN_SUBMITTED')return isSalesView.value?'查看经理统一方案':'补齐船运及其他费用';return waitingFor(row.status)}
function salesAttention(row:SourcingCase){if(row.handoffStatus==='PROCUREMENT_PLAN_READY')return '等待采购提交方案';if(row.handoffStatus==='PROCUREMENT_PLAN_SUBMITTED')return '查看经理统一方案';return ({REVIEWING:'确认客户需求已提交',SOURCING:'等待采购反馈',QUOTES_RECEIVED:'关注报价进度',COSTING:'准备客户报价',CUSTOMER_QUOTE_CREATED:'跟进客户反馈',CANCELLED:'无需继续跟进'} as Record<string,string>)[String(row.status)]||'查看项目进展'}

// 兼容采购工作台和询盘确认页生成的旧链接，并统一跳转到新的独立详情页。
const linkedCaseID = String(route.query.case || '')
if (linkedCaseID) void router.replace(detailPath(linkedCaseID))
else void load()

// 两个采购入口复用同一列表组件；从“待开始询价”切到全部项目时立即重置筛选。
watch(() => route.path, () => {
  status.value = isPendingView.value ? 'REVIEWING' : ''
  reload()
})
const stopLive = onLive(event => { if (event.type === 'requirement.changed') reload() })
onUnmounted(stopLive)
</script>

<style scoped>
.page{--accent:#2563eb;--accent-soft:#eff6ff;padding:28px;min-height:calc(100vh - 60px);background:#f5f7fa}.page--procurement{--accent:#087f6f;--accent-soft:#edf8f5}.page-head{display:flex;justify-content:space-between;align-items:flex-end;max-width:1600px;margin:0 auto 20px}.page-heading{display:block}.eyebrow{display:block;margin-bottom:7px;color:var(--accent);font-size:11px;font-weight:750;letter-spacing:.14em}.title-row{display:flex;align-items:center;gap:12px}.page-head h1{margin:0;color:#172b4d;font-size:27px;line-height:1.25}.page-head p{margin:7px 0 0;color:#6b778c;font-size:14px}.perspective-label{padding:4px 9px;border:1px solid color-mix(in srgb,var(--accent) 25%,white);border-radius:999px;background:var(--accent-soft);color:var(--accent);font-size:12px;font-weight:600}.list-card{max-width:1600px;margin:0 auto;overflow:hidden;background:#fff;border:1px solid #e1e5eb;border-radius:10px;box-shadow:0 2px 8px rgba(23,43,77,.04)}.context-bar{display:flex;justify-content:space-between;align-items:center;gap:20px;padding:13px 18px;border-bottom:1px solid #e8ebf0;background:#fafbfc;color:#5e6c84;font-size:13px}.context-bar>div{display:flex;align-items:center;gap:9px}.context-bar strong{color:#344563;font-size:14px}.context-bar small{color:#8993a4}.context-dot{width:7px;height:7px;border-radius:50%;background:var(--accent);box-shadow:0 0 0 4px var(--accent-soft)}.stage-nav{display:flex;padding:0 18px;border-bottom:1px solid #e8ebf0;background:#fff}.stage-nav button{position:relative;display:flex;align-items:baseline;gap:6px;padding:15px 18px;border:0;background:transparent;color:#6b778c;cursor:pointer}.stage-nav button:after{position:absolute;right:12px;bottom:-1px;left:12px;height:2px;background:transparent;content:''}.stage-nav button:hover{color:#344563}.stage-nav button.active{color:var(--accent);font-weight:650}.stage-nav button.active:after{background:var(--accent)}.stage-nav small{color:#a0a8b5;font-size:11px}.toolbar{display:grid;grid-template-columns:minmax(280px,1fr) 220px auto;gap:10px;padding:16px 18px 10px}.el-table{padding:0 18px}.el-table small{display:block;margin-top:4px;color:#8993a4}.customer-name{color:#253858;font-size:14px}.next-action{color:#6b778c!important}.stale-tag{margin-top:6px}.procurement-next{color:#253858;font-weight:600}.sales-table :deep(.el-table__row),.procurement-table :deep(.el-table__row){cursor:pointer}.sales-table :deep(.el-table__row:hover>td){background:#f7faff!important}.procurement-table :deep(.el-table__row:hover>td){background:#f5fbf9!important}.page :deep(.el-table th.el-table__cell){background:#fff;color:#6b778c;font-weight:600}.page :deep(.el-button--primary){--el-button-bg-color:var(--accent);--el-button-border-color:var(--accent);--el-button-hover-bg-color:color-mix(in srgb,var(--accent) 86%,white);--el-button-hover-border-color:color-mix(in srgb,var(--accent) 86%,white)}.page :deep(.el-button--primary.is-link){--el-button-text-color:var(--accent);--el-button-bg-color:transparent;--el-button-border-color:transparent}.pager{justify-content:flex-end;padding:14px 18px 18px}@media(max-width:900px){.stage-nav{overflow:auto}.context-bar{align-items:flex-start;flex-direction:column}.context-bar small{padding-left:16px}}@media(max-width:760px){.page{padding:16px}.page-head{align-items:flex-start;gap:14px;flex-direction:column}.toolbar{grid-template-columns:1fr}.context-bar>div{align-items:flex-start;flex-wrap:wrap}.stage-nav button{min-width:max-content;padding-inline:12px}}
.page--sales{--accent:#76546f;--accent-soft:#f6f1f5}
.withdraw-case{margin:16px 0 12px;color:#344563;font-weight:650}.danger-item{color:var(--el-color-danger)}.row-actions{display:inline-flex;align-items:center;justify-content:flex-end}
</style>
