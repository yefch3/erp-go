<template>
  <div class="home-page">
    <section class="home-head">
      <div>
        <span class="eyebrow">{{ t('todos.eyebrow') }}</span>
        <h1>{{ t('todos.greeting', { name: auth.employeeName }) }}</h1>
        <p class="employee-context">{{ auth.employeeDepartment || t('todos.departmentUnset') }} · {{ today }}</p>
        <p>{{ t('todos.subtitle') }}</p>
      </div>
      <div class="head-actions">
        <span v-if="updatedAt" class="updated">{{ t('todos.updatedAt', { time: updatedAt }) }}</span>
        <el-button :loading="loading || countLoading || reminderCountLoading" @click="refreshAll">{{ t('common.refresh') }}</el-button>
      </div>
    </section>

    <section class="summary-grid" aria-label="home summary">
      <button class="summary-card active" type="button" @click="activate('pending')">
        <span>{{ t('todos.summaryPending') }}</span>
        <strong>{{ combinedPendingAvailable ? combinedPendingTotal : '—' }}</strong>
        <small>{{ t('todos.summaryPendingHint') }}</small>
      </button>
      <button class="summary-card" :class="{ active: activeTab === 'reminders' && reminderTiming === 'UPCOMING' }" type="button" :disabled="!reminderSummaryAvailable" @click="activateReminderFilter('UPCOMING')">
        <span>{{ t('todos.summaryUpcoming') }}</span><strong>{{ reminderSummaryAvailable ? reminderSummary.upcoming : '—' }}</strong><small>{{ t('todos.summaryUpcomingHint') }}</small>
      </button>
      <button class="summary-card danger" :class="{ active: activeTab === 'reminders' && reminderTiming === 'OVERDUE' }" type="button" :disabled="!reminderSummaryAvailable" @click="activateReminderFilter('OVERDUE')">
        <span>{{ t('todos.summaryOverdue') }}</span><strong>{{ reminderSummaryAvailable ? reminderSummary.overdue : '—' }}</strong><small>{{ t('todos.summaryOverdueHint') }}</small>
      </button>
      <button class="summary-card" :class="{ active: activeTab === 'reminders' && reminderRead === 'UNREAD' }" type="button" :disabled="!reminderSummaryAvailable" @click="activateUnreadReminders">
        <span>{{ t('todos.summaryUnread') }}</span><strong>{{ reminderSummaryAvailable ? reminderSummary.unread : '—' }}</strong><small>{{ t('todos.summaryUnreadHint') }}</small>
      </button>
    </section>

    <el-card class="work-card" shadow="never">
      <el-tabs v-model="activeTab" class="home-tabs" @tab-change="onTabChange">
        <el-tab-pane :label="t('todos.homeTabPending')" name="pending" />
        <el-tab-pane :label="t('todos.homeTabSubmitted')" name="submitted" />
        <el-tab-pane :label="t('todos.homeTabResponsible')" name="responsible" />
        <el-tab-pane :label="t('todos.homeTabReminders')" name="reminders" />
        <el-tab-pane :label="t('todos.homeTabHandled')" name="handled" />
      </el-tabs>

      <div v-if="hasApprovalSource" class="filters">
        <el-input v-model="keyword" clearable :placeholder="t('todos.searchPlaceholder')" @keyup.enter="query" @clear="query" />
        <el-select v-model="bizType" :placeholder="t('todos.allModules')" clearable @change="query">
          <el-option v-for="item in bizTypes" :key="item" :label="bizTypeLabel(item)" :value="item" />
        </el-select>
        <el-select v-if="activeTab !== 'pending'" v-model="statusFilter" :placeholder="t('todos.allStatuses')" clearable @change="query">
          <el-option v-for="item in statusOptions" :key="item" :label="activeTab === 'handled' ? taskStatusLabel(item) : instanceStatusLabel(item)" :value="item" />
        </el-select>
        <el-button type="primary" @click="query">{{ t('common.query') }}</el-button>
      </div>

      <div v-else-if="hasReminderSource" class="filters reminder-filters">
        <el-input v-model="keyword" clearable :placeholder="t('todos.reminderSearchPlaceholder')" @keyup.enter="query" @clear="query" />
        <el-select v-model="reminderSource" :placeholder="t('todos.allReminderSources')" clearable @change="query">
          <el-option v-for="item in reminderSourceOptions" :key="item" :label="reminderSourceLabel(item)" :value="item" />
        </el-select>
        <el-select v-model="reminderTiming" :placeholder="t('todos.allReminderTimings')" clearable @change="query">
          <el-option value="UPCOMING" :label="t('todos.timing.UPCOMING')" />
          <el-option value="OVERDUE" :label="t('todos.timing.OVERDUE')" />
          <el-option value="REMINDER" :label="t('todos.timing.REMINDER')" />
        </el-select>
        <el-select v-model="reminderRead" :placeholder="t('todos.allReadStates')" clearable @change="query">
          <el-option value="UNREAD" :label="t('todos.unread')" />
          <el-option value="READ" :label="t('todos.read')" />
        </el-select>
        <el-button type="primary" @click="query">{{ t('common.query') }}</el-button>
        <el-button v-if="reminderSummary.unread > 0" :loading="markingRead" @click="markAllRemindersRead">{{ t('todos.markAllRead') }}</el-button>
      </div>

      <el-alert v-if="sourceError" class="source-error" type="warning" :closable="false" show-icon :title="t('todos.sourceUnavailable')">
        <template #default><el-button link type="primary" @click="load">{{ t('todos.retry') }}</el-button></template>
      </el-alert>

      <el-alert v-if="hasReminderSource && reminderSourceError" class="source-error" type="warning" :closable="false" show-icon :title="t('todos.reminderSourceUnavailable')">
        <template #default><el-button link type="primary" @click="load">{{ t('todos.retry') }}</el-button></template>
      </el-alert>

      <template v-if="activeTab === 'pending'">
        <div v-loading="loading" class="pending-content">
          <section v-if="visibleProcurementTasks.length" class="todo-source-section">
            <div class="todo-source-head"><div><h3>{{ t('todos.procurementTasks') }}</h3><p>{{ t('todos.procurementTasksHint') }}</p></div><el-tag type="warning" effect="plain">{{ procurementPendingTotal }} {{ t('todos.items') }}</el-tag></div>
            <el-table :data="visibleProcurementTasks" class="home-table sourcing-todo-table">
              <el-table-column :label="t('todos.priority')" width="145"><template #default><el-tag size="small" type="warning" effect="light">{{ t('todos.priorityLevels.HIGH') }}</el-tag><div class="priority-reason">{{ t('todos.procurementActionRequired') }}</div></template></el-table-column>
              <el-table-column :label="t('todos.workItem')" min-width="360"><template #default="{row}"><div class="item-title">{{ procurementReworkLabel(row.requestType) }} · {{ row.caseNo }}</div><div class="item-meta">{{ row.productName||t('todos.allProducts') }} · {{ row.supplierName||t('todos.newSupplier') }}</div><div class="item-summary">{{ row.reason }}</div></template></el-table-column>
              <el-table-column :label="t('todos.submittedAt')" width="170"><template #default="{row}">{{ formatTime(row.createdAt) }}</template></el-table-column>
              <el-table-column :label="t('common.status')" width="120"><template #default><el-tag size="small" type="warning" effect="plain">{{ t('todos.waitingForMe') }}</el-tag></template></el-table-column>
              <el-table-column :label="t('common.actions')" width="150" fixed="right"><template #default="{row}"><router-link :to="{path:`/procurement/sourcing/${row.caseId}`,query:{tab:'plans',rework:String(row.id)}}" class="doc-link">{{ t('todos.goToSource') }}</router-link></template></el-table-column>
            </el-table>
          </section>
          <section v-if="visibleShippingTasks.length" class="todo-source-section">
            <div class="todo-source-head"><div><h3>船运补充任务</h3><p>销售退回指定报价，或向全部船运人员发布新增船运公司任务。</p></div><el-tag type="warning" effect="plain">{{ shippingTasks.length }} 项</el-tag></div>
            <el-table :data="visibleShippingTasks" class="home-table sourcing-todo-table"><el-table-column label="优先级" width="145"><template #default><el-tag size="small" type="warning">高</el-tag><div class="priority-reason">需要补充船运报价</div></template></el-table-column><el-table-column label="工作事项" min-width="360"><template #default="{row}"><div class="item-title">{{ shippingReworkLabel(row.requestType) }} · {{ row.caseNo }}</div><div class="item-meta">{{ row.productName||'全部货物' }} · {{ row.carrierForwarder||'新增船运公司' }}</div><div class="item-summary">{{ row.reason }}</div></template></el-table-column><el-table-column label="发布于" width="170"><template #default="{row}">{{ formatTime(row.createdAt) }}</template></el-table-column><el-table-column label="状态" width="120"><template #default><el-tag size="small" type="warning" effect="plain">待我处理</el-tag></template></el-table-column><el-table-column label="操作" width="190" fixed="right"><template #default="{row}"><router-link to="/shipping/sourcing" class="doc-link">进入船运询价</router-link><el-button link type="primary" @click="resolveShippingTask(row)">完成任务</el-button></template></el-table-column></el-table>
          </section>
          <div v-if="todos.length" class="todo-source-head approval-source-head"><div><h3>{{ t('todos.approvalTasks') }}</h3></div></div>
        <el-table v-if="todos.length" :data="todos" class="home-table">
          <el-table-column :label="t('todos.priority')" width="145">
            <template #default="{ row }">
              <el-tag size="small" :type="priorityTagType(row.priority)" effect="light">{{ priorityLabel(row.priority) }}</el-tag>
              <div class="priority-reason">{{ priorityReason(row.remainingMinutes) }}</div>
            </template>
          </el-table-column>
          <el-table-column :label="t('todos.workItem')" min-width="310">
            <template #default="{ row }">
              <div class="item-title">{{ bizTypeLabel(row.instance.bizType) }} · {{ row.instance.bizNo }}</div>
              <div class="item-meta">{{ row.task.nodeName }} · {{ row.instance.submitterName }}</div>
              <div class="item-summary">{{ summaryText(row.instance.bizSummary) }}</div>
            </template>
          </el-table-column>
          <el-table-column :label="t('todos.submittedAt')" width="170"><template #default="{ row }">{{ formatTime(row.instance.submittedAt) }}</template></el-table-column>
          <el-table-column :label="t('common.status')" width="120"><template #default><el-tag size="small" type="warning" effect="plain">{{ t('todos.waitingForMe') }}</el-tag></template></el-table-column>
          <el-table-column :label="t('common.actions')" width="150" fixed="right">
            <template #default="{ row }">
              <router-link v-if="approvalSourceLink(row.instance)" :to="approvalSourceLink(row.instance)!" class="doc-link">{{ t('todos.goToSource') }}</router-link>
              <span v-else class="no-link">{{ t('todos.noSourceLink') }}</span>
            </template>
          </el-table-column>
        </el-table>
          <HomeEmpty v-if="!visibleProcurementTasks.length&&!visibleShippingTasks.length&&!todos.length" :description="t('todos.empty')" />
        </div>
      </template>

      <template v-else-if="activeTab === 'handled'">
        <el-table :data="todos" v-loading="loading" class="home-table">
          <el-table-column :label="t('todos.workItem')" min-width="330">
            <template #default="{ row }">
              <div class="item-title">{{ bizTypeLabel(row.instance.bizType) }} · {{ row.instance.bizNo }}</div>
              <div class="item-meta">{{ row.task.nodeName }} · {{ summaryText(row.instance.bizSummary) }}</div>
            </template>
          </el-table-column>
          <el-table-column :label="t('todos.myDecision')" width="180">
            <template #default="{ row }"><el-tag size="small" :type="taskTagType(row.task.status)">{{ taskStatusLabel(row.task.status) }}</el-tag><span v-if="row.task.comment" class="comment">{{ row.task.comment }}</span></template>
          </el-table-column>
          <el-table-column :label="t('todos.actedAt')" width="170"><template #default="{ row }">{{ formatTime(row.task.actedAt) }}</template></el-table-column>
          <el-table-column :label="t('todos.docResult')" width="120"><template #default="{ row }"><el-tag size="small" effect="plain" :type="instanceTagType(row.instance.status)">{{ instanceStatusLabel(row.instance.status) }}</el-tag></template></el-table-column>
          <el-table-column :label="t('common.actions')" width="100" fixed="right"><template #default="{ row }"><router-link v-if="approvalSourceLink(row.instance)" :to="approvalSourceLink(row.instance)!" class="doc-link">{{ t('common.view') }}</router-link></template></el-table-column>
          <template #empty><HomeEmpty :description="t('todos.emptyHandled')" /></template>
        </el-table>
      </template>

      <template v-else-if="activeTab === 'submitted'">
        <el-table :data="submitted" v-loading="loading" class="home-table">
          <el-table-column :label="t('todos.workItem')" min-width="360">
            <template #default="{ row }"><div class="item-title">{{ bizTypeLabel(row.bizType) }} · {{ row.bizNo }}</div><div class="item-summary">{{ summaryText(row.bizSummary) }}</div></template>
          </el-table-column>
          <el-table-column :label="t('todos.submittedAt')" width="170"><template #default="{ row }">{{ formatTime(row.submittedAt) }}</template></el-table-column>
          <el-table-column :label="t('common.status')" width="130"><template #default="{ row }"><el-tag size="small" effect="plain" :type="instanceTagType(row.status)">{{ instanceStatusLabel(row.status) }}</el-tag></template></el-table-column>
          <el-table-column :label="t('common.actions')" width="100" fixed="right"><template #default="{ row }"><router-link v-if="approvalSourceLink(row)" :to="approvalSourceLink(row)!" class="doc-link">{{ t('common.view') }}</router-link></template></el-table-column>
          <template #empty><HomeEmpty :description="t('todos.emptySubmitted')" /></template>
        </el-table>
      </template>

      <template v-else-if="activeTab === 'reminders'">
        <el-table :data="reminders" v-loading="loading" class="home-table">
          <el-table-column :label="t('todos.priority')" width="125">
            <template #default="{ row }"><el-tag size="small" :type="homeReminderTagType(row.timing)" effect="light">{{ reminderTimingLabel(row.timing) }}</el-tag></template>
          </el-table-column>
          <el-table-column :label="t('todos.workItem')" min-width="360">
            <template #default="{ row }">
              <div class="item-title"><span v-if="row.unread" class="unread-dot" />{{ row.title }}</div>
              <div class="item-meta">{{ reminderSourceLabel(row.source) }}<template v-if="row.bizNo"> · {{ row.bizNo }}</template></div>
              <div class="item-summary">{{ row.content }}</div>
            </template>
          </el-table-column>
          <el-table-column :label="t('todos.reminderDueAt')" width="145"><template #default="{ row }">{{ row.dueAt || '—' }}</template></el-table-column>
          <el-table-column :label="t('common.status')" width="100"><template #default="{ row }"><el-tag size="small" :type="row.unread ? 'primary' : 'info'" effect="plain">{{ row.unread ? t('todos.unread') : t('todos.read') }}</el-tag></template></el-table-column>
          <el-table-column :label="t('common.actions')" width="170" fixed="right">
            <template #default="{ row }">
              <el-button v-if="row.unread" link type="primary" @click="markReminderRead(row)">{{ t('todos.markRead') }}</el-button>
              <el-button link type="primary" @click="openReminder(row)">{{ t('common.view') }}</el-button>
            </template>
          </el-table-column>
          <template #empty><HomeEmpty :description="t('todos.emptyReminders')" /></template>
        </el-table>
      </template>

      <HomeEmpty v-else :description="futureEmptyText" />

      <el-pagination v-if="hasDataSource && total > 0" class="pager" layout="total, prev, pager, next" :total="total" :page-size="pageSize" :current-page="page" @current-change="changePage" />
    </el-card>

  </div>
</template>

<script setup lang="ts">
import { computed, defineComponent, h, onMounted, onUnmounted, ref } from 'vue'
import { ElEmpty, ElMessage, ElMessageBox } from 'element-plus'
import { useI18n } from 'vue-i18n'
import { useRoute, useRouter } from 'vue-router'
import { get, post, quietErrors } from '../api'
import { onLive } from '../live'
import { useAuthStore } from '../stores/auth'
import { approvalPriorityTagType, approvalSourceLink, approvalTimeAmount } from '../lib/homeApproval'
import {
  announceReminderChanged,
  homeReminderTagType,
  onReminderChanged,
  type HomeReminder,
  type HomeReminderSource,
  type HomeReminderSourceState,
  type HomeReminderSummary,
  type HomeReminderTiming,
} from '../lib/homeReminders'

interface Task { id: string; nodeSeq: number; nodeName: string; status: string; comment: string; actedAt: string }
interface Instance { id: string; bizType: string; bizId: string; bizNo: string; bizSummary: string; submitterName: string; status: string; currentSeq: number; submittedAt: string; finishedAt: string }
interface Todo { task: Task; instance: Instance; dueAt: string; priority: string; remainingMinutes: string | number }
interface ProcurementTaskCase { id:string; caseNo:string; customerName:string; title:string; updatedAt:string; openReworkCount:number|string; myOpenReworkCount:number|string }
interface ProcurementReworkTask { id:string; caseId:string; caseNo:string; requestType:string; productName:string; supplierName:string; reason:string; createdAt:string; assignedBuyerId:string|number; status:string }
interface ShippingReworkTask { id:string; caseId:string; caseNo:string; caseTitle:string; requestType:string; productName:string; carrierForwarder:string; reason:string; createdAt:string; assignedShippingId:string|number; status:string }
type HomeTab = 'pending' | 'submitted' | 'responsible' | 'reminders' | 'handled'

const HomeEmpty = defineComponent({
  props: { description: { type: String, required: true } },
  setup(props) { return () => h(ElEmpty, { description: props.description, imageSize: 88 }) },
})

const { t } = useI18n()
const route = useRoute()
const router = useRouter()
const auth = useAuthStore()
const today = new Intl.DateTimeFormat(undefined, { year: 'numeric', month: 'long', day: 'numeric', weekday: 'short' }).format(new Date())
const activeTab = ref<HomeTab>('pending')
const todos = ref<Todo[]>([])
const submitted = ref<Instance[]>([])
const reminders = ref<HomeReminder[]>([])
const reminderSummary = ref<HomeReminderSummary>({ upcoming: 0, overdue: 0, unread: 0 })
const total = ref(0)
const pendingTotal = ref(0)
const pendingCountAvailable = ref(true)
const procurementTasks = ref<ProcurementReworkTask[]>([])
const procurementTasksAvailable = ref(false)
const shippingTasks = ref<ShippingReworkTask[]>([])
const shippingTasksAvailable = ref(false)
const page = ref(1)
const pageSize = 10
const keyword = ref('')
const bizType = ref('')
const statusFilter = ref('')
const loading = ref(false)
const countLoading = ref(false)
const reminderCountLoading = ref(false)
const sourceError = ref(false)
const reminderSourceError = ref(false)
const reminderSummaryAvailable = ref(false)
const reminderSource = ref('')
const reminderTiming = ref('')
const reminderRead = ref('')
const markingRead = ref(false)
const updatedAt = ref('')
const procurementPendingTotal = computed(() => procurementTasks.value.length)
const combinedPendingTotal = computed(() => (pendingCountAvailable.value ? pendingTotal.value : 0) + (procurementTasksAvailable.value ? procurementPendingTotal.value : 0) + (shippingTasksAvailable.value ? shippingTasks.value.length : 0))
const combinedPendingAvailable = computed(() => pendingCountAvailable.value || procurementTasksAvailable.value || shippingTasksAvailable.value)
const visibleProcurementTasks = computed(() => {
  const query = keyword.value.trim().toLocaleLowerCase()
  return procurementTasks.value.filter(row => !query || [row.caseNo,row.productName,row.supplierName,row.reason].some(value => String(value||'').toLocaleLowerCase().includes(query)))
})
const visibleShippingTasks = computed(() => { const query=keyword.value.trim().toLocaleLowerCase();return shippingTasks.value.filter(row=>!query||[row.caseNo,row.caseTitle,row.productName,row.carrierForwarder,row.reason].some(value=>String(value||'').toLocaleLowerCase().includes(query))) })

const bizTypes = ['CONTRACT', 'PURCHASE_ORDER', 'PURCHASE_ORDER_CHANGE', 'PAYMENT', 'LC_AMENDMENT', 'STOCK_ADJUST']
const hasApprovalSource = computed(() => ['pending', 'submitted', 'handled'].includes(activeTab.value))
const hasReminderSource = computed(() => activeTab.value === 'reminders')
const hasDataSource = computed(() => hasApprovalSource.value || hasReminderSource.value)
const reminderSourceOptions = computed<HomeReminderSource[]>(() => [
  ...(auth.can('export:receipt:read') ? ['RECEIVABLE' as const] : []),
  ...(auth.can('shipping:schedule:read') ? ['ARRIVAL' as const, 'BL' as const] : []),
])
const statusOptions = computed(() => activeTab.value === 'handled'
  ? ['APPROVED', 'REJECTED', 'RETURNED', 'CANCELLED', 'SKIPPED']
  : ['RUNNING', 'APPROVED', 'REJECTED', 'RETURNED', 'CANCELLED'])
const futureEmptyText = computed(() => t('todos.emptyResponsible'))

function activate(tab: HomeTab) {
  if (activeTab.value === tab) return
  activeTab.value = tab
  onTabChange(tab)
}

function activateReminderFilter(timing: HomeReminderTiming) {
  activeTab.value = 'reminders'
  page.value = 1
  keyword.value = ''
  reminderSource.value = ''
  reminderRead.value = ''
  reminderTiming.value = timing
  sourceError.value = false
  void load()
}

function activateUnreadReminders() {
  activeTab.value = 'reminders'
  page.value = 1
  keyword.value = ''
  reminderSource.value = ''
  reminderTiming.value = ''
  reminderRead.value = 'UNREAD'
  sourceError.value = false
  void load()
}

function onTabChange(tab: string | number) {
  activeTab.value = String(tab) as HomeTab
  page.value = 1
  keyword.value = ''
  bizType.value = ''
  statusFilter.value = ''
  reminderSource.value = ''
  reminderTiming.value = ''
  reminderRead.value = ''
  sourceError.value = false
  reminderSourceError.value = false
  void load()
}

function query() { page.value = 1; void load() }
function changePage(next: number) { page.value = next; void load() }

async function loadPendingCount() {
  countLoading.value = true
  try {
    const data = await get<{ meta: { total: string } }>('/approvals/todos', { page: 1, page_size: 1 }, quietErrors)
    pendingTotal.value = Number(data.meta?.total ?? 0)
    pendingCountAvailable.value = true
  } catch {
    pendingCountAvailable.value = false
  } finally {
    countLoading.value = false
  }
}

async function loadProcurementTasks() {
  if (!auth.can('procurement:sourcing:read')) {
    procurementTasks.value = []
    procurementTasksAvailable.value = false
    return
  }
  try {
    const pageSize = 200
    const first = await get<{ sourcingCases: ProcurementTaskCase[]; meta?: { total?: number|string } }>('/sourcing-cases', { page: 1, page_size: pageSize }, quietErrors)
    const pageCount = Math.ceil(Number(first.meta?.total ?? first.sourcingCases?.length ?? 0) / pageSize)
    const rest = pageCount > 1 ? await Promise.all(Array.from({ length: pageCount - 1 }, (_, index) => get<{ sourcingCases: ProcurementTaskCase[] }>('/sourcing-cases', { page: index + 2, page_size: pageSize }, quietErrors))) : []
    const cases = [...(first.sourcingCases ?? []), ...rest.flatMap(data => data.sourcingCases ?? [])].filter(row => Number(row.myOpenReworkCount) > 0)
    const taskGroups = await Promise.all(cases.map(async (item) => {
      const data = await get<{ reworkRequests?: Array<Omit<ProcurementReworkTask,'caseNo'|'caseId'>> }>(`/sourcing-cases/${item.id}/procurement-reworks`, {}, quietErrors)
      return (data.reworkRequests ?? [])
        .filter(task => task.status === 'OPEN' && (!Number(task.assignedBuyerId || 0) || auth.owns(String(task.assignedBuyerId))))
        .map(task => ({ ...task, caseId: String(item.id), caseNo: item.caseNo }))
    }))
    procurementTasks.value = taskGroups.flat()
    procurementTasksAvailable.value = true
  } catch {
    procurementTasks.value = []
    procurementTasksAvailable.value = false
  }
}

async function loadShippingTasks(){if(!auth.can('shipping:sourcing:read')){shippingTasks.value=[];shippingTasksAvailable.value=false;return}try{const data=await get<{reworkRequests?:ShippingReworkTask[]}>('/shipping/sourcing-reworks',{},quietErrors);shippingTasks.value=data.reworkRequests||[];shippingTasksAvailable.value=true}catch{shippingTasks.value=[];shippingTasksAvailable.value=false}}
function shippingReworkLabel(value:string){return value==='ADD_CARRIER'?'增加船运公司':value==='REQUOTE'?'更新船运报价/船期':'重新议价'}
async function resolveShippingTask(row:ShippingReworkTask){const result=await ElMessageBox.prompt('请说明已完成的询价、议价或新增船运公司结果。','完成船运补充任务',{inputPlaceholder:'例如：已录入该船运公司的最新报价版本',inputValidator:(value:string)=>!!value.trim()||'请填写处理结果'}).catch(()=>null);if(!result)return;await post(`/shipping/sourcing-reworks/${row.id}/resolve`,{resolution_note:result.value});await refreshAll();ElMessage.success('船运补充任务已完成')}

function procurementReworkLabel(value: string) {
  return value === 'ADD_SUPPLIER' ? t('todos.addSupplier') : value === 'REQUOTE' ? t('todos.requote') : t('todos.renegotiate')
}

function applyReminderResponse(data: { summary?: HomeReminderSummary; sources?: HomeReminderSourceState[] }) {
  reminderSummary.value = data.summary ?? { upcoming: 0, overdue: 0, unread: 0 }
  reminderSummaryAvailable.value = true
  reminderSourceError.value = (data.sources ?? []).some((source) => !source.available)
}

async function loadReminderSummary() {
  reminderCountLoading.value = true
  try {
    const data = await get<{ summary: HomeReminderSummary; sources: HomeReminderSourceState[] }>(
      '/home/reminders', { page: 1, page_size: 1 }, quietErrors,
    )
    applyReminderResponse(data)
  } catch {
    reminderSummaryAvailable.value = false
    reminderSourceError.value = true
  } finally {
    reminderCountLoading.value = false
  }
}

async function load() {
  if (hasReminderSource.value) {
    loading.value = true
    reminderSourceError.value = false
    try {
      const data = await get<{
        items: HomeReminder[]
        total: number
        summary: HomeReminderSummary
        sources: HomeReminderSourceState[]
      }>('/home/reminders', {
        page: page.value,
        page_size: pageSize,
        keyword: keyword.value,
        source: reminderSource.value,
        timing: reminderTiming.value,
        read: reminderRead.value,
      }, quietErrors)
      reminders.value = data.items ?? []
      todos.value = []
      submitted.value = []
      total.value = Number(data.total ?? 0)
      applyReminderResponse(data)
      updatedAt.value = new Date().toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })
    } catch {
      reminderSourceError.value = true
      reminders.value = []
      total.value = 0
    } finally {
      loading.value = false
    }
    return
  }
  if (!hasApprovalSource.value) {
    todos.value = []
    submitted.value = []
    reminders.value = []
    total.value = 0
    return
  }
  loading.value = true
  sourceError.value = false
  try {
    const requestedBizType = String(route.query.bizType ?? '')
    const requestedBizID = String(route.query.bizId ?? '')
    const targeted = Boolean(requestedBizType || requestedBizID)
    const params = {
      page: targeted ? 1 : page.value,
      page_size: targeted ? 200 : pageSize,
      biz_type: bizType.value || requestedBizType,
      keyword: keyword.value,
      status: activeTab.value === 'pending' ? '' : activeTab.value === 'handled' ? (statusFilter.value || 'HANDLED') : statusFilter.value,
    }
    if (activeTab.value === 'submitted') {
      const data = await get<{ instances: Instance[]; meta: { total: string } }>('/approvals/submitted', params, quietErrors)
      submitted.value = (data.instances ?? []).filter((row) => !requestedBizID || String(row.bizId) === requestedBizID)
      todos.value = []
      reminders.value = []
      total.value = targeted ? submitted.value.length : Number(data.meta?.total ?? 0)
    } else {
      const data = await get<{ todos: Todo[]; meta: { total: string } }>('/approvals/todos', params, quietErrors)
      todos.value = (data.todos ?? []).filter((row) => !requestedBizID || String(row.instance.bizId) === requestedBizID)
      submitted.value = []
      reminders.value = []
      total.value = targeted ? todos.value.length : Number(data.meta?.total ?? 0)
    }
    updatedAt.value = new Date().toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })
  } catch {
    sourceError.value = true
    todos.value = []
    submitted.value = []
    total.value = 0
  } finally {
    loading.value = false
  }
}

async function refreshAll() {
  if (hasReminderSource.value) await Promise.all([loadPendingCount(), loadProcurementTasks(), loadShippingTasks(), load()])
  else await Promise.all([loadPendingCount(), loadProcurementTasks(), loadShippingTasks(), loadReminderSummary(), load()])
}

async function markReminderRead(item: HomeReminder) {
  if (!item.unread) return
  await post('/home/reminders/read', { source: item.source, ids: [item.sourceId] })
  announceReminderChanged()
  await load()
}

async function markAllRemindersRead() {
  markingRead.value = true
  try {
    const data = await post<{ marked: number; sources: HomeReminderSourceState[] }>(
      '/home/reminders/read', { source: 'ALL', ids: [] }, quietErrors,
    )
    announceReminderChanged()
    if ((data.sources ?? []).length > 0) ElMessage.warning(t('todos.markReadPartial'))
    else ElMessage.success(t('todos.markedRead', { count: Number(data.marked ?? 0) }))
    await load()
  } finally {
    markingRead.value = false
  }
}

async function openReminder(item: HomeReminder) {
  if (item.unread) {
    try { await markReminderRead(item) } catch { /* 原业务入口仍应可打开 */ }
  }
  if (item.detailUrl) await router.push(item.detailUrl)
}

function parseSummary(raw: string): Record<string, string> {
  if (!raw) return {}
  try { const parsed = JSON.parse(raw); return typeof parsed === 'object' && parsed !== null ? parsed : {} } catch { return {} }
}
function summaryText(raw: string): string { return Object.entries(parseSummary(raw)).map(([key, value]) => `${summaryLabel(key)}：${value}`).join(' · ') }
function summaryLabel(key: string): string { return labelOr(`todos.summaryKeys.${key}`, key) }
function taskStatusLabel(code: string): string { return code ? labelOr(`todos.task.${code}`, code) : '—' }
function priorityLabel(code: string): string { return labelOr(`todos.priorityLevels.${code}`, code) }
function instanceStatusLabel(code: string): string { return code ? labelOr(`todos.doc.${code}`, code) : '—' }
function bizTypeLabel(code: string): string { return code ? labelOr(`todos.biz.${code}`, code) : '—' }
function reminderSourceLabel(code?: string): string {
  if (!code) return '—'
  return labelOr(`todos.reminderSources.${code}`, code)
}
function reminderTimingLabel(code?: string): string {
  if (!code) return '—'
  return labelOr(`todos.timing.${code}`, code)
}
function labelOr(key: string, fallback: string): string { const label = t(key); return label === key ? fallback : label }
function taskTagType(code: string): 'success' | 'danger' | 'warning' | 'info' { return ({ APPROVED: 'success', REJECTED: 'danger', RETURNED: 'warning' }[code] as 'success' | 'danger' | 'warning' | undefined) ?? 'info' }
function instanceTagType(code: string): 'success' | 'danger' | 'warning' | 'info' { return ({ APPROVED: 'success', REJECTED: 'danger', RETURNED: 'warning', RUNNING: 'warning' }[code] as 'success' | 'danger' | 'warning' | undefined) ?? 'info' }
function priorityTagType(code: string): 'danger' | 'warning' | 'info' { return approvalPriorityTagType(code) }
function priorityReason(rawMinutes: string | number): string {
  const amount = approvalTimeAmount(rawMinutes)
  const duration = t(`todos.duration${amount.unit[0].toUpperCase()}${amount.unit.slice(1)}`, { count: amount.count })
  return amount.overdue ? t('todos.overdueBy', { duration }) : t('todos.dueIn', { duration })
}
function formatTime(iso: string): string { return iso ? iso.replace('T', ' ').slice(0, 16) : '—' }

onMounted(refreshAll)
const stopListening = onLive((event) => {
  if (event.type === 'todo.changed' || event.type === 'doc.changed' || event.type === 'requirement.changed' || event.type === 'shipping.arrival_reminder') void refreshAll()
})
const stopReminderListening = onReminderChanged(() => { void refreshAll() })
onUnmounted(() => {
  stopListening()
  stopReminderListening()
})
</script>

<style scoped>
.home-page { max-width: 1500px; margin: 0 auto; }
.home-head { display: flex; align-items: flex-end; justify-content: space-between; gap: 24px; margin-bottom: 20px; }
.eyebrow { color: #0f8c82; font-size: 12px; font-weight: 700; letter-spacing: .14em; }
.home-head h1 { margin: 6px 0 4px; font-size: 28px; line-height: 1.25; }
.home-head p { margin: 0; color: var(--el-text-color-secondary); }
.home-head .employee-context { margin-bottom: 5px; color: var(--el-text-color-regular); font-size: 13px; }
.head-actions { display: flex; align-items: center; gap: 12px; }
.updated { color: var(--el-text-color-secondary); font-size: 13px; white-space: nowrap; }
.pending-content { min-height: 260px; }
.todo-source-section + .approval-source-head { margin-top: 24px; }
.todo-source-head { display: flex; align-items: center; justify-content: space-between; gap: 16px; padding: 4px 18px 10px; }
.todo-source-head h3 { margin: 0; font-size: 16px; }
.todo-source-head p { margin: 4px 0 0; color: var(--el-text-color-secondary); font-size: 13px; }
.approval-source-head { padding-bottom: 0; }
.summary-grid { display: grid; grid-template-columns: repeat(4, minmax(0, 1fr)); gap: 14px; margin-bottom: 18px; }
.summary-card { min-height: 116px; padding: 18px 20px; text-align: left; border: 1px solid var(--el-border-color-light); border-radius: 12px; background: var(--el-bg-color); color: inherit; }
.summary-card:not(:disabled) { cursor: pointer; }
.summary-card.active { border-left: 4px solid var(--el-color-primary); }
.summary-card.danger { border-left: 4px solid var(--el-color-danger); }
.summary-card span, .summary-card small { display: block; color: var(--el-text-color-secondary); }
.summary-card strong { display: block; margin: 8px 0 4px; color: var(--el-text-color-primary); font-size: 28px; }
.summary-card:disabled { opacity: 1; }
.work-card { border-radius: 12px; }
.home-tabs :deep(.el-tabs__header) { margin-bottom: 18px; }
.filters { display: grid; grid-template-columns: minmax(260px, 1fr) 190px 180px auto; gap: 12px; margin-bottom: 16px; }
.reminder-filters { grid-template-columns: minmax(240px, 1fr) 160px 160px 140px auto auto; }
.source-error { margin-bottom: 14px; }
.home-table { width: 100%; }
.item-title { color: var(--el-text-color-primary); font-weight: 600; }
.unread-dot { display: inline-block; width: 7px; height: 7px; margin-right: 8px; border-radius: 50%; background: var(--el-color-primary); vertical-align: 1px; }
.item-meta, .item-summary { margin-top: 5px; color: var(--el-text-color-secondary); font-size: 13px; }
.item-summary { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.priority-reason { margin-top: 5px; color: var(--el-text-color-secondary); font-size: 12px; white-space: nowrap; }
.comment { margin-left: 8px; color: var(--el-text-color-secondary); }
.doc-link { color: var(--el-color-primary); text-decoration: none; }
.doc-link:hover { text-decoration: underline; }
.no-link { color: var(--el-text-color-placeholder); font-size: 13px; }
.pager { justify-content: flex-end; margin-top: 18px; }
@media (max-width: 900px) { .home-head { align-items: flex-start; flex-direction: column; } .summary-grid { grid-template-columns: repeat(2, minmax(0, 1fr)); } .filters { grid-template-columns: 1fr; } }
@media (max-width: 560px) { .summary-grid { grid-template-columns: 1fr; } .head-actions { width: 100%; justify-content: space-between; } }
</style>
