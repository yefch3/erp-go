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
        <el-button :loading="loading || countLoading" @click="refreshAll">{{ t('common.refresh') }}</el-button>
      </div>
    </section>

    <section class="summary-grid" aria-label="home summary">
      <button class="summary-card active" type="button" @click="activate('pending')">
        <span>{{ t('todos.summaryPending') }}</span>
        <strong>{{ pendingCountAvailable ? pendingTotal : '—' }}</strong>
        <small>{{ t('todos.summaryPendingHint') }}</small>
      </button>
      <button class="summary-card" type="button" disabled>
        <span>{{ t('todos.summaryUpcoming') }}</span><strong>—</strong><small>{{ t('todos.summaryUpcomingHint') }}</small>
      </button>
      <button class="summary-card danger" type="button" disabled>
        <span>{{ t('todos.summaryOverdue') }}</span><strong>—</strong><small>{{ t('todos.sourceLater') }}</small>
      </button>
      <button class="summary-card" type="button" disabled>
        <span>{{ t('todos.summaryUnread') }}</span><strong>—</strong><small>{{ t('todos.sourceLater') }}</small>
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

      <el-alert v-if="sourceError" class="source-error" type="warning" :closable="false" show-icon :title="t('todos.sourceUnavailable')">
        <template #default><el-button link type="primary" @click="load">{{ t('todos.retry') }}</el-button></template>
      </el-alert>

      <template v-if="activeTab === 'pending'">
        <el-table :data="todos" v-loading="loading" class="home-table">
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
          <template #empty><HomeEmpty :description="t('todos.empty')" /></template>
        </el-table>
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

      <HomeEmpty v-else :description="futureEmptyText" />

      <el-pagination v-if="hasApprovalSource && total > 0" class="pager" layout="total, prev, pager, next" :total="total" :page-size="pageSize" :current-page="page" @current-change="changePage" />
    </el-card>

  </div>
</template>

<script setup lang="ts">
import { computed, defineComponent, h, onMounted, onUnmounted, ref } from 'vue'
import { ElEmpty } from 'element-plus'
import { useI18n } from 'vue-i18n'
import { useRoute } from 'vue-router'
import { get, quietErrors } from '../api'
import { onLive } from '../live'
import { useAuthStore } from '../stores/auth'
import { approvalPriorityTagType, approvalSourceLink, approvalTimeAmount } from '../lib/homeApproval'

interface Task { id: string; nodeSeq: number; nodeName: string; status: string; comment: string; actedAt: string }
interface Instance { id: string; bizType: string; bizId: string; bizNo: string; bizSummary: string; submitterName: string; status: string; currentSeq: number; submittedAt: string; finishedAt: string }
interface Todo { task: Task; instance: Instance; dueAt: string; priority: string; remainingMinutes: string | number }
type HomeTab = 'pending' | 'submitted' | 'responsible' | 'reminders' | 'handled'

const HomeEmpty = defineComponent({
  props: { description: { type: String, required: true } },
  setup(props) { return () => h(ElEmpty, { description: props.description, imageSize: 88 }) },
})

const { t } = useI18n()
const route = useRoute()
const auth = useAuthStore()
const today = new Intl.DateTimeFormat(undefined, { year: 'numeric', month: 'long', day: 'numeric', weekday: 'short' }).format(new Date())
const activeTab = ref<HomeTab>('pending')
const todos = ref<Todo[]>([])
const submitted = ref<Instance[]>([])
const total = ref(0)
const pendingTotal = ref(0)
const pendingCountAvailable = ref(true)
const page = ref(1)
const pageSize = 10
const keyword = ref('')
const bizType = ref('')
const statusFilter = ref('')
const loading = ref(false)
const countLoading = ref(false)
const sourceError = ref(false)
const updatedAt = ref('')

const bizTypes = ['CONTRACT', 'PURCHASE_ORDER', 'PURCHASE_ORDER_CHANGE', 'PAYMENT', 'LC_AMENDMENT', 'STOCK_ADJUST']
const hasApprovalSource = computed(() => ['pending', 'submitted', 'handled'].includes(activeTab.value))
const statusOptions = computed(() => activeTab.value === 'handled'
  ? ['APPROVED', 'REJECTED', 'RETURNED', 'CANCELLED', 'SKIPPED']
  : ['RUNNING', 'APPROVED', 'REJECTED', 'RETURNED', 'CANCELLED'])
const futureEmptyText = computed(() => activeTab.value === 'responsible' ? t('todos.emptyResponsible') : t('todos.emptyReminders'))

function activate(tab: HomeTab) {
  if (activeTab.value === tab) return
  activeTab.value = tab
  onTabChange(tab)
}

function onTabChange(tab: string | number) {
  activeTab.value = String(tab) as HomeTab
  page.value = 1
  keyword.value = ''
  bizType.value = ''
  statusFilter.value = ''
  sourceError.value = false
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

async function load() {
  if (!hasApprovalSource.value) {
    todos.value = []
    submitted.value = []
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
      total.value = targeted ? submitted.value.length : Number(data.meta?.total ?? 0)
    } else {
      const data = await get<{ todos: Todo[]; meta: { total: string } }>('/approvals/todos', params, quietErrors)
      todos.value = (data.todos ?? []).filter((row) => !requestedBizID || String(row.instance.bizId) === requestedBizID)
      submitted.value = []
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

async function refreshAll() { await Promise.all([loadPendingCount(), load()]) }

function parseSummary(raw: string): Record<string, string> {
  if (!raw) return {}
  try { const parsed = JSON.parse(raw); return typeof parsed === 'object' && parsed !== null ? parsed : {} } catch { return {} }
}
function summaryText(raw: string): string { return Object.entries(parseSummary(raw)).map(([key, value]) => `${summaryLabel(key)}：${value}`).join(' · ') }
function summaryLabel(key: string): string { return labelOr(`todos.summaryKeys.${key}`, key) }
function taskStatusLabel(code: string): string { return labelOr(`todos.task.${code}`, code) }
function priorityLabel(code: string): string { return labelOr(`todos.priorityLevels.${code}`, code) }
function instanceStatusLabel(code: string): string { return labelOr(`todos.doc.${code}`, code) }
function bizTypeLabel(code: string): string { return labelOr(`todos.biz.${code}`, code) }
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
  if (event.type === 'todo.changed' || event.type === 'doc.changed') void refreshAll()
})
onUnmounted(stopListening)
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
.source-error { margin-bottom: 14px; }
.home-table { width: 100%; }
.item-title { color: var(--el-text-color-primary); font-weight: 600; }
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
