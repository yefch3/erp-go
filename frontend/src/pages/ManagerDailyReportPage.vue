<template>
  <main>
    <WorkflowPageHeader :title="t(`managerReport.${period}Title`)" :description="t('managerReport.subtitle')">
      <template #actions>
        <el-radio-group v-model="period" size="small" :aria-label="t('managerReport.period')" @change="loadReport">
          <el-radio-button v-for="item in periods" :key="item" :value="item">{{ t(`managerReport.${item}`) }}</el-radio-button>
        </el-radio-group>
        <el-date-picker
          :key="period"
          v-model="date"
          :type="period === 'day' ? 'date' : period"
          :value-format="dateFormat"
          :clearable="false"
          :disabled-date="isFutureDate"
          :aria-label="t('managerReport.reportDate')"
          :placeholder="t('managerReport.reportDate')"
          style="width: 145px"
          @change="loadReport"
        />
        <el-button :icon="Refresh" :loading="loading" @click="refresh">{{ t('common.refresh') }}</el-button>
      </template>
    </WorkflowPageHeader>

    <el-alert v-if="failed" type="error" :title="t('managerReport.reportError')" :closable="false" />
    <div v-else v-loading="loading" class="report-content">
      <template v-if="report">
        <div class="report-meta">
          <span>{{ t('managerReport.asOf', { at: asOfLabel }) }}</span>
          <span>{{ t('managerReport.coverage', { synced: report.syncedMailboxes, total: report.mailboxes }) }}</span>
        </div>
        <el-alert
          v-if="Number(report.syncedMailboxes) < Number(report.mailboxes)"
          type="warning"
          :title="t('managerReport.coverageWarning')"
          :closable="false"
          show-icon
        />
        <div class="metric-grid">
          <section v-for="metric in metrics" :key="metric.key" class="metric-card">
            <div class="metric-label">
              <el-icon :class="['metric-icon', metric.key]"><component :is="metric.icon" /></el-icon>
              {{ t(`managerReport.${metric.key}`) }}
            </div>
            <strong>{{ count(report.total?.[metric.key]) }}</strong>
            <small>{{ t(metric.key === 'replied' && report.period !== 'day' ? 'managerReport.periodRepliedHint' : `managerReport.${metric.key}Hint`) }}</small>
          </section>
        </div>

        <section v-if="report.trend?.length" class="report-panel">
          <header class="trend-header">
            <h2>{{ trendTitle }}</h2>
            <div class="trend-controls">
              <el-radio-group v-model="trendCategory" size="small" :aria-label="t('managerReport.trendCategory')">
                <el-radio-button value="mail">{{ t('managerReport.mailActivity') }}</el-radio-button>
                <el-radio-button value="business">{{ t('managerReport.businessActivity') }}</el-radio-button>
              </el-radio-group>
              <el-select v-if="trendCategory === 'mail'" v-model="trendMailMode" size="small" :aria-label="t('managerReport.trendMetric')" class="trend-select">
                <el-option value="flow" :label="t('managerReport.mailFlow')" />
                <el-option value="replied" :label="t('managerReport.replied')" />
              </el-select>
              <el-select v-else v-model="trendActivityKey" size="small" :aria-label="t('managerReport.trendMetric')" class="trend-select">
                <el-option v-for="metric in activityMetrics" :key="metric.key" :value="metric.key" :label="t(`managerReport.${metric.key}`)" />
              </el-select>
            </div>
          </header>
          <ManagerTrendChart
            :label="trendTitle"
            :series="chartSeries"
            :points="chartPoints"
            :selected="selectedTrendKey"
            @select="locateTrendRow"
          />
          <div ref="trendTable" class="trend-table">
            <el-table :data="report.trend" max-height="200" size="small" row-key="startDate" highlight-current-row :current-row-key="selectedTrendKey">
              <el-table-column :label="t('managerReport.period')" min-width="124">
                <template #default="{ row }"><span :data-report-bucket="row.startDate" tabindex="-1">{{ trendLabel(row) }}</span></template>
              </el-table-column>
              <el-table-column v-for="metric in trendMailMetrics" :key="metric.key" :label="t(`managerReport.${metric.key}`)" min-width="86" align="right">
                <template #default="{ row }">{{ count(row.mail?.[metric.key]) }}</template>
              </el-table-column>
              <el-table-column v-for="metric in activityMetrics" :key="metric.key" :label="t(`managerReport.${metric.key}`)" min-width="92" align="right">
                <template #default="{ row }">{{ activityCount(row.activity, metric) }}</template>
              </el-table-column>
            </el-table>
          </div>
          <p class="activity-rules">{{ t('managerReport.trendRules') }}</p>
        </section>

        <div class="detail-grid">
          <section class="report-panel">
            <header>
              <h2>{{ t('managerReport.team') }}</h2>
              <el-radio-group v-model="workloadView" size="small" :aria-label="t('managerReport.team')">
                <el-radio-button value="business">{{ t('managerReport.businessActivity') }}</el-radio-button>
                <el-radio-button value="mail">{{ t('managerReport.mailActivity') }}</el-radio-button>
              </el-radio-group>
            </header>
            <template v-if="workloadView === 'business'">
              <el-alert
                v-if="unavailableActivity.length"
                type="warning"
                :title="t('managerReport.activityUnavailable', { sources: unavailableLabels })"
                :closable="false"
                show-icon
              />
              <el-table :data="businessTeam" :empty-text="t('managerReport.noBusinessActivity')" max-height="282" size="small" row-key="employeeId">
                <el-table-column :label="t('managerReport.employee')" min-width="92" show-overflow-tooltip>
                  <template #default="{ row }">{{ row.total ? t('managerReport.teamTotal') : employeeName(row) }}</template>
                </el-table-column>
                <el-table-column prop="department" :label="t('managerReport.department')" min-width="78" show-overflow-tooltip>
                  <template #default="{ row }">{{ row.department || '—' }}</template>
                </el-table-column>
                <el-table-column v-for="metric in activityMetrics" :key="metric.key" :label="t(`managerReport.${metric.key}`)" min-width="84" align="right">
                  <template #default="{ row }">
                    <span :class="{ 'activity-total': row.total }">{{ activityCount(row.activity, metric) }}</span>
                  </template>
                </el-table-column>
              </el-table>
              <p class="activity-rules">{{ t('managerReport.activityRules') }}</p>
            </template>
            <el-table v-else :data="mailTeam" :empty-text="t('managerReport.noActivity')" max-height="282" size="small" row-key="employeeId">
              <el-table-column :label="t('managerReport.employee')" min-width="92" show-overflow-tooltip>
                <template #default="{ row }">{{ employeeName(row) }}</template>
              </el-table-column>
              <el-table-column prop="department" :label="t('managerReport.department')" min-width="78" show-overflow-tooltip>
                <template #default="{ row }">{{ row.department || '—' }}</template>
              </el-table-column>
              <el-table-column v-for="metric in metrics" :key="metric.key" :label="t(`managerReport.${metric.key}`)" min-width="79" align="right">
                <template #default="{ row }">{{ count(row.counts?.[metric.key]) }}</template>
              </el-table-column>
            </el-table>
          </section>

          <section class="report-panel follow-up">
            <header>
              <h2>{{ t('managerReport.followUp') }}</h2>
              <router-link to="/emails" class="muted-link">{{ t('managerReport.myMailbox') }}</router-link>
            </header>
            <div v-if="followUps.length" class="pending-legend" :aria-label="t('managerReport.pendingDistribution')">
              <span><i class="pending-normal" aria-hidden="true"></i>{{ t('managerReport.pendingWithin24') }}</span>
              <span><i class="pending-overdue" aria-hidden="true"></i>{{ t('managerReport.pending24') }}</span>
            </div>
            <el-table :data="followUps" :empty-text="t('managerReport.noFollowUp')" max-height="282" size="small" row-key="employeeId">
              <el-table-column :label="t('managerReport.employee')" min-width="86" show-overflow-tooltip>
                <template #default="{ row }">{{ employeeName(row) }}</template>
              </el-table-column>
              <el-table-column :label="t('managerReport.pendingDistribution')" min-width="148">
                <template #default="{ row }">
                  <div class="pending-meter">
                    <span class="pending-track" role="img" :aria-label="pendingLabel(row)" :title="pendingLabel(row)">
                      <span class="pending-normal" :style="{ width: pendingWidth(within24(row)) }"></span>
                      <span class="pending-overdue" :style="{ width: pendingWidth(count(row.counts?.overdue)) }"></span>
                    </span>
                    <span class="pending-total">{{ count(row.counts?.pending) }}</span>
                  </div>
                </template>
              </el-table-column>
              <el-table-column :label="t('managerReport.pending24')" min-width="78" align="right">
                <template #default="{ row }">
                  <el-tag v-if="count(row.counts?.overdue)" size="small" type="warning">{{ count(row.counts?.overdue) }}</el-tag>
                  <span v-else>0</span>
                </template>
              </el-table-column>
              <el-table-column :label="t('managerReport.oldest')" min-width="84" align="right">
                <template #default="{ row }">{{ waiting(row.counts?.oldestPendingAt) }}</template>
              </el-table-column>
            </el-table>
          </section>
        </div>

        <section class="report-panel reminder-panel">
          <header><h2>{{ t('managerReport.businessReminders') }}</h2></header>
          <el-alert v-if="remindersIncomplete" type="warning" :title="t('managerReport.reminderError')" :closable="false" />
          <p v-if="!reminders.length && !remindersIncomplete" class="reminder-empty">{{ t('managerReport.noReminders') }}</p>
          <ul v-if="reminders.length" class="reminder-list">
            <li v-for="item in reminders" :key="item.key">
              <span>{{ item.title }} <span class="secondary">{{ item.bizNo }}</span></span>
              <el-tag v-if="item.dueAt" size="small" :type="homeReminderTagType(item.timing)" effect="light">{{ item.dueAt }}</el-tag>
              <router-link v-if="item.detailUrl" :to="item.detailUrl" class="muted-link">{{ t('managerReport.view') }}</router-link>
            </li>
          </ul>
        </section>
        <p class="metric-rules">{{ t('managerReport.rules') }}</p>
      </template>
    </div>
  </main>
</template>

<script setup lang="ts">
import { BottomLeft, ChatDotRound, Clock, Refresh, TopRight } from '@element-plus/icons-vue'
import { computed, nextTick, onMounted, reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { get, quietErrors } from '../api'
import WorkflowPageHeader from '../components/WorkflowPageHeader.vue'
import ManagerTrendChart from '../components/ManagerTrendChart.vue'
import { homeReminderTagType, type HomeReminder, type HomeReminderSourceState } from '../lib/homeReminders'

type MetricValue = number | string
type MailCounts = Partial<Record<'received' | 'sent' | 'replied' | 'pending' | 'overdue' | 'oldestPendingAt', MetricValue>>
type ActivityKey = 'inquiriesCreated' | 'quotationsSent' | 'purchaseOrdersHandled' | 'schedulesUpdated'
type Activity = Partial<Record<ActivityKey, MetricValue>>
interface EmployeeRow {
  employeeId: string
  name?: string
  department?: string
  counts?: MailCounts
  activity?: Activity
}
type ReportPeriod = 'day' | 'month' | 'year'
interface TrendBucket {
  startDate: string
  endDate: string
  mail?: MailCounts
  activity?: Activity
}
interface Report {
  date: string
  period: ReportPeriod
  trend?: TrendBucket[]
  timeZone: string
  asOf: MetricValue
  mailboxes: MetricValue
  syncedMailboxes: MetricValue
  total: MailCounts
  employees: EmployeeRow[]
  activityTotal?: Activity
  unavailableActivitySources?: string[]
}

const { t, locale } = useI18n()
const periods: ReportPeriod[] = ['day', 'month', 'year']
const period = ref<ReportPeriod>('day')
const selectedDates = reactive<Record<ReportPeriod, string>>({ day: '', month: '', year: '' })
const dateFormats: Record<ReportPeriod, string> = { day: 'YYYY-MM-DD', month: 'YYYY-MM', year: 'YYYY' }
const dateFormat = computed(() => dateFormats[period.value])
const date = computed({
  get: () => selectedDates[period.value],
  set: value => { selectedDates[period.value] = value },
})
const report = ref<Report>()
const loading = ref(false)
const failed = ref(false)
const reminders = ref<HomeReminder[]>([])
const workloadView = ref<'business' | 'mail'>('business')
const activityMetrics = [
  { key: 'inquiriesCreated', source: 'PURCHASE' },
  { key: 'quotationsSent', source: 'QUOTATION' },
  { key: 'purchaseOrdersHandled', source: 'PURCHASE' },
  { key: 'schedulesUpdated', source: 'SHIPPING' },
] as const
const unavailableActivity = computed(() => report.value?.unavailableActivitySources ?? [])
const unavailableLabels = computed(() => unavailableActivity.value.map(source => t(`managerReport.source${source}`)).join(' / '))
const remindersIncomplete = ref(false)
const metrics = [
  { key: 'received', icon: BottomLeft },
  { key: 'sent', icon: TopRight },
  { key: 'replied', icon: ChatDotRound },
  { key: 'pending', icon: Clock },
] as const
const trendMailMetrics = metrics.filter(metric => metric.key !== 'pending')
const trendCategory = ref<'mail' | 'business'>('mail')
const trendMailMode = ref<'flow' | 'replied'>('flow')
const trendActivityKey = ref<ActivityKey>('inquiriesCreated')
const selectedTrendKey = ref('')
const trendTable = ref<HTMLElement>()
const trendTitle = computed(() => t(report.value?.period === 'month' ? 'managerReport.weeklyTrend' : 'managerReport.monthlyTrend'))
const activeTrendMetric = computed(() => activityMetrics.find(metric => metric.key === trendActivityKey.value) ?? activityMetrics[0])
const chartSeries = computed(() => {
  if (trendCategory.value === 'business') return [t(`managerReport.${activeTrendMetric.value.key}`)]
  if (trendMailMode.value === 'replied') return [t('managerReport.replied')]
  return [t('managerReport.received'), t('managerReport.sent')]
})
const chartPoints = computed(() => (report.value?.trend ?? []).map(bucket => {
  let values: (number | null)[]
  if (trendCategory.value === 'business') {
    const value = activityCount(bucket.activity, activeTrendMetric.value)
    values = [typeof value === 'number' ? value : null]
  } else {
    values = trendMailMode.value === 'replied'
      ? [count(bucket.mail?.replied)]
      : [count(bucket.mail?.received), count(bucket.mail?.sent)]
  }
  return {
    key: bucket.startDate, label: trendLabel(bucket),
    shortLabel: report.value?.period === 'year' ? bucket.startDate.slice(5, 7) : bucket.startDate.slice(5),
    values,
  }
}))

async function locateTrendRow(key: string) {
  selectedTrendKey.value = key
  await nextTick()
  const row = trendTable.value?.querySelector<HTMLElement>(`[data-report-bucket="${CSS.escape(key)}"]`)
  row?.closest('tr')?.scrollIntoView({ block: 'nearest', inline: 'nearest' })
  row?.focus({ preventScroll: true })
}
function trendLabel(bucket: TrendBucket) {
  return report.value?.period === 'year'
    ? bucket.startDate.slice(0, 7)
    : `${bucket.startDate.slice(5)} – ${bucket.endDate.slice(5)}`
}
const count = (value?: MetricValue) => Number(value ?? 0)
const employeeName = (row: EmployeeRow) => row.name || t('managerReport.unnamed', { id: row.employeeId })
const team = computed(() => [...(report.value?.employees ?? [])].sort((a, b) =>
  (a.department || '').localeCompare(b.department || '', locale.value) ||
  employeeName(a).localeCompare(employeeName(b), locale.value),
))
const mailTeam = computed(() => team.value.filter(row => row.counts))
const businessTeam = computed(() => report.value
  ? [{ employeeId: 'total', activity: report.value.activityTotal, total: true }, ...team.value]
  : [],
)
function activityCount(activity: Activity | undefined, metric: typeof activityMetrics[number]) {
  if (unavailableActivity.value.includes(metric.source)) return '—'
  return count(activity?.[metric.key])
}
const followUps = computed(() => mailTeam.value.filter(row => count(row.counts?.pending) > 0).sort((a, b) =>
  count(b.counts?.overdue) - count(a.counts?.overdue) ||
  count(a.counts?.oldestPendingAt) - count(b.counts?.oldestPendingAt),
))
const largestPending = computed(() => followUps.value.reduce((max, row) => Math.max(max, count(row.counts?.pending)), 1))
const within24 = (row: EmployeeRow) => Math.max(0, count(row.counts?.pending) - count(row.counts?.overdue))
const pendingWidth = (value: number) => `${Math.min(value, largestPending.value) / largestPending.value * 100}%`
const pendingLabel = (row: EmployeeRow) => `${employeeName(row)} · ${t('managerReport.pendingWithin24')} ${within24(row)} · ${t('managerReport.pending24')} ${count(row.counts?.overdue)}`
const asOfLabel = computed(() => report.value
  ? new Intl.DateTimeFormat(locale.value, {
    month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit',
    hourCycle: 'h23', timeZone: report.value.timeZone,
  }).format(new Date(count(report.value.asOf) * 1000))
  : '',
)

const reportDay = new Intl.DateTimeFormat('sv-SE', { timeZone: 'Asia/Shanghai', year: 'numeric', month: '2-digit', day: '2-digit' })
function isFutureDate(value: Date) {
  const year = value.getFullYear()
  const month = String(value.getMonth() + 1).padStart(2, '0')
  const day = String(value.getDate()).padStart(2, '0')
  const today = reportDay.format(new Date())
  const candidate = `${year}-${month}-${day}`
  const length = dateFormat.value.length
  return candidate.slice(0, length) > today.slice(0, length)
}

function waiting(earliest?: MetricValue) {
  const hours = Math.floor((count(report.value?.asOf) - count(earliest)) / 3600)
  return hours > 0 ? t('managerReport.hours', { count: hours }) : t('managerReport.underHour')
}

let reportRequest = 0
async function loadReport() {
  const request = ++reportRequest
  loading.value = true
  failed.value = false
  try {
    const result = await get<Report>('/manager/daily-report', { period: period.value, date: date.value }, quietErrors)
    if (request !== reportRequest) return
    if (report.value?.period !== result.period || report.value.date !== result.date) selectedTrendKey.value = ''
    report.value = result
    date.value = result.date
  } catch {
    if (request !== reportRequest) return
    failed.value = true
    report.value = undefined
  } finally {
    if (request === reportRequest) loading.value = false
  }
}

async function loadReminders() {
  try {
    const data = await get<{ items?: HomeReminder[]; sources?: HomeReminderSourceState[] }>(
      '/home/reminders', { page: 1, page_size: 12 }, quietErrors,
    )
    reminders.value = (data.items ?? []).filter(item => item.timing !== 'REMINDER').slice(0, 5)
    remindersIncomplete.value = (data.sources ?? []).some(source => !source.available)
  } catch {
    reminders.value = []
    remindersIncomplete.value = true
  }
}

function refresh() {
  void loadReport()
  void loadReminders()
}
onMounted(refresh)
</script>

<style scoped>
.report-content { min-height: 90px; display: grid; gap: 12px; }
.report-meta { display: flex; flex-wrap: wrap; justify-content: space-between; gap: 8px; font-size: 12px; color: #60717c; }
.metric-grid { display: grid; grid-template-columns: repeat(4, minmax(0, 1fr)); gap: 12px; }
.metric-card, .report-panel { min-width: 0; border: 1px solid #dceaf0; border-radius: 12px; background: #fff; }
.metric-card { padding: 13px 16px; display: grid; gap: 6px; }
.metric-label { display: flex; align-items: center; gap: 8px; color: #354957; font-weight: 600; }
.metric-icon { border-radius: 7px; width: 27px; height: 27px; color: #226998; background: #eaf8ff; }
.metric-icon.replied { color: #34745a; background: #e9f8ef; }
.metric-icon.pending { color: #946118; background: #fff3df; }
.metric-card strong { font-size: 29px; line-height: 1.15; color: #162a38; font-variant-numeric: tabular-nums; }
.metric-card small, .metric-rules, .secondary, .reminder-empty { color: #60717c; }
.detail-grid { display: grid; grid-template-columns: 1.58fr 1fr; gap: 12px; }
.report-panel { overflow: hidden; }
.report-panel header { display: flex; align-items: center; justify-content: space-between; gap: 8px; padding: 12px 14px; }
.report-panel h2 { margin: 0; font-size: 15px; color: #243745; }
.muted-link { color: #2378a6; font-size: 12px; text-decoration: none; }
.muted-link:hover { text-decoration: underline; }
.reminder-panel { padding-bottom: 10px; }
.reminder-empty, .reminder-list { padding: 0 14px; margin: 0; font-size: 13px; }
.reminder-list { list-style: none; display: grid; gap: 6px; }
.reminder-list li { display: flex; justify-content: space-between; gap: 12px; }
.trend-header { flex-wrap: wrap; }
.trend-controls { display: flex; flex-wrap: wrap; gap: 8px; align-items: center; }
.trend-select { width: 156px; }
.trend-table :deep(.el-table__body tr.current-row > td.el-table__cell) { background: #e6f4fd; color: #174c6d; font-weight: 600; }
.trend-table [data-report-bucket]:focus { outline: 2px solid #1680bb; outline-offset: 2px; }
.pending-legend { display: flex; flex-wrap: wrap; gap: 6px 14px; padding: 0 14px 8px; font-size: 11px; color: #60717c; }
.pending-legend > span { display: inline-flex; gap: 5px; align-items: center; }
.pending-legend i { width: 8px; height: 8px; border-radius: 2px; }
.pending-meter { display: flex; gap: 6px; align-items: center; }
.pending-track { display: flex; flex: 1; height: 8px; border-radius: 4px; overflow: hidden; background: #edf3f7; }
.pending-total { min-width: 22px; text-align: right; font-variant-numeric: tabular-nums; }
.pending-normal { background: #539ec9; }
.pending-overdue { background: #d69a49; }
.activity-total { font-weight: 700; color: #243745; }
.activity-rules { padding: 9px 14px; margin: 0; color: #60717c; font-size: 12px; line-height: 1.5; }
.metric-rules { margin: 0; font-size: 12px; line-height: 1.5; }
@media (max-width: 1100px) { .detail-grid { grid-template-columns: 1fr; } }
@media (max-width: 720px) { .metric-grid { grid-template-columns: repeat(2, minmax(0, 1fr)); } }
</style>
