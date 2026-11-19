<template>
  <main class="report-page" v-loading="loading">
    <header class="report-heading">
      <div>
        <h1>{{ t('businessReports.title') }}</h1>
        <p v-if="report">{{ t('businessReports.period', { start: report.startDate, cutoff: report.cutoffDate }) }}</p>
      </div>
      <div class="report-filters">
        <el-select v-model="year" :aria-label="t('businessReports.year')" style="width: 100px">
          <el-option v-for="y in years" :key="y" :value="y" :label="String(y)" />
        </el-select>
        <el-select v-model="quarter" :aria-label="t('businessReports.quarter')" style="width: 100px">
          <el-option v-for="q in quarters" :key="q" :value="q" :label="'Q' + q" />
        </el-select>
      </div>
    </header>

    <el-tabs v-model="view">
      <el-tab-pane :label="t('businessReports.overview')" name="quarter" />
      <el-tab-pane :label="t('businessReports.sales')" name="sales" />
      <el-tab-pane :label="t('businessReports.contracts')" name="contracts" />
    </el-tabs>
    <el-alert v-if="failed" :title="t('businessReports.loadFailed')" type="error" show-icon :closable="false">
      <el-button link type="primary" @click="load">{{ t('businessReports.retry') }}</el-button>
    </el-alert>

    <template v-if="report && view === 'quarter'">
      <section class="metrics" :aria-label="t('businessReports.overview')">
        <div v-for="metric in metrics" :key="metric.label" class="metric">
          <span>{{ t(metric.label) }}</span>
          <strong v-for="value in metric.values" :key="value" :class="{ 'overdue-text': metric.warning }">{{ value }}</strong>
        </div>
      </section>

      <section class="report-insights">
        <div class="report-panel trend">
          <header class="panel-heading">
            <h2>{{ t('businessReports.trend') }}</h2>
            <el-select v-if="currencies.length > 1" :model-value="selectedCurrency" @update:model-value="currency = $event" :aria-label="t('businessReports.currency')" size="small" style="width: 88px">
              <el-option v-for="code in currencies" :key="code" :value="code" />
            </el-select>
            <span v-else>{{ selectedCurrency }}</span>
          </header>
          <div class="trend-legend">
            <span aria-hidden="true"></span>
            <span><i class="signed-bar"></i>{{ t('businessReports.signed') }}</span>
            <span><i class="received-bar"></i>{{ t('businessReports.received') }}</span>
          </div>
          <div v-for="month in trend" :key="month.month" class="trend-row">
            <span>{{ month.month }}</span>
            <div v-for="(amount, index) in [month.signed, month.received]" :key="index" class="trend-track"
              :title="t(index ? 'businessReports.received' : 'businessReports.signed') + ' ' + formatNumber(amount)">
              <div class="trend-bar" :class="[index ? 'received-bar' : 'signed-bar', { negative: amount < 0 }]"
                :style="{ width: (Math.abs(amount) / trendMax * 100) + '%' }"></div>
              <span>{{ formatNumber(amount) }}</span>
            </div>
          </div>
        </div>

        <div class="report-panel summary">
          <h2>{{ t('businessReports.summary') }}</h2>
          <ul>
            <li>{{ t('businessReports.signedSummary', { count: report.signedCount }) }}</li>
            <li>{{ t('businessReports.overdueSummary', { count: report.total }) }}</li>
            <li v-if="Number(report.missingDueDateCount)">{{ t('businessReports.missingDue', { count: report.missingDueDateCount }) }}</li>
            <li v-if="Number(report.undatedReceiptCount)">{{ t('businessReports.undated', { count: report.undatedReceiptCount }) }}</li>
          </ul>
        </div>
      </section>

      <section class="report-panel report-table">
        <header class="panel-heading">
          <h2>{{ t('businessReports.overdueContracts') }} <small>{{ report.total }}</small></h2>
          <el-button v-if="Number(report.total) > 5" link type="primary" @click="showAllOverdue = !showAllOverdue">
            {{ t(showAllOverdue ? 'businessReports.showLess' : 'businessReports.showAll') }}
          </el-button>
        </header>
        <el-alert v-if="overdueFailed" :title="t('businessReports.loadFailed')" type="error" show-icon :closable="false">
          <el-button link type="primary" @click="loadOverdue">{{ t('businessReports.retry') }}</el-button>
        </el-alert>
        <el-table :data="showAllOverdue ? overdue : overdue.slice(0, 5)" size="small" row-key="contractId" v-loading="overdueLoading">
          <el-table-column :label="t('businessReports.contractNo')" min-width="150">
            <template #default="{ row }"><el-button link type="primary" @click="openContract(row.contractId)">{{ row.contractNo }}</el-button></template>
          </el-table-column>
          <el-table-column prop="customer" :label="t('businessReports.customer')" min-width="160" show-overflow-tooltip />
          <el-table-column prop="owner" :label="t('businessReports.owner')" min-width="95" show-overflow-tooltip />
          <el-table-column prop="dueDate" :label="t('businessReports.dueDate')" width="118" />
          <el-table-column :label="t('businessReports.balance')" min-width="140" align="right">
            <template #default="{ row }">{{ money(row.balance, row.currency) }}</template>
          </el-table-column>
          <el-table-column prop="overdueDays" :label="t('businessReports.days')" width="88" align="right" class-name="overdue-text" />
        </el-table>
        <el-pagination v-if="showAllOverdue && overdueTotal > pageSize" v-model:current-page="overduePage"
          :page-size="pageSize" :total="overdueTotal" layout="prev, pager, next, total" @current-change="loadOverdue" />
      </section>
    </template>

    <section v-if="report && view === 'sales'" class="report-panel report-table">
      <el-table :data="report.sales" size="small" row-key="employeeId" :default-sort="{ prop: 'signedCount', order: 'descending' }">
        <el-table-column :label="t('businessReports.owner')" min-width="130">
          <template #default="{ row }">{{ row.employee || t('businessReports.unassigned') }}</template>
        </el-table-column>
        <el-table-column prop="inquiryCount" :label="t('businessReports.inquiries')" width="100" align="right" sortable :sort-method="(a: SalesRow, b: SalesRow) => Number(a.inquiryCount) - Number(b.inquiryCount)" />
        <el-table-column prop="quoteCount" :label="t('businessReports.quotes')" width="100" align="right" sortable :sort-method="(a: SalesRow, b: SalesRow) => Number(a.quoteCount) - Number(b.quoteCount)" />
        <el-table-column prop="signedCount" :label="t('businessReports.count')" width="110" align="right" sortable :sort-method="(a: SalesRow, b: SalesRow) => Number(a.signedCount) - Number(b.signedCount)" />
        <el-table-column :label="t('businessReports.signed')" min-width="170" align="right">
          <template #default="{ row }">{{ amounts(row.signed).join(' / ') }}</template>
        </el-table-column>
        <el-table-column :label="t('businessReports.overdue')" min-width="170" align="right">
          <template #default="{ row }">{{ amounts(row.overdue).join(' / ') }}</template>
        </el-table-column>
      </el-table>
    </section>

    <section v-if="report && view === 'contracts'" class="report-panel report-table">
      <el-table :data="report.contracts" size="small" row-key="contractId">
        <el-table-column :label="t('businessReports.contractNo')" min-width="145">
          <template #default="{ row }"><el-button link type="primary" @click="openContract(row.contractId)">{{ row.contractNo }}</el-button></template>
        </el-table-column>
        <el-table-column prop="customer" :label="t('businessReports.customer')" min-width="145" show-overflow-tooltip />
        <el-table-column prop="owner" :label="t('businessReports.owner')" min-width="90" show-overflow-tooltip />
        <el-table-column :label="t('businessReports.dueDate')" width="110">
          <template #default="{ row }">{{ row.dueDate || '—' }}</template>
        </el-table-column>
        <el-table-column v-for="col in contractMoney" :key="col.key" :label="t(col.label)" min-width="130" align="right">
          <template #default="{ row }">{{ money(row[col.key], row.currency) }}</template>
        </el-table-column>
      </el-table>
      <el-pagination v-if="Number(report.total) > pageSize" v-model:current-page="contractPage"
        :page-size="pageSize" :total="Number(report.total)" layout="prev, pager, next, total" @current-change="load" />
    </section>

    <p v-if="report" class="basis">{{ t('businessReports.basis') }}</p>

    <el-drawer v-model="detailOpen" :title="t('businessReports.detail')" :size="'min(560px, 95vw)'">
      <div v-loading="detailLoading" class="detail-content">
        <el-alert v-if="detailFailed" :title="t('businessReports.loadFailed')" type="error" show-icon :closable="false" />
        <template v-if="detail">
          <h2>{{ detail.contractNo }}</h2>
          <el-descriptions :column="2" border size="small">
            <el-descriptions-item :label="t('businessReports.customer')">{{ detail.customer || '—' }}</el-descriptions-item>
            <el-descriptions-item :label="t('businessReports.owner')">{{ detail.owner || '—' }}</el-descriptions-item>
            <el-descriptions-item :label="t('businessReports.dueDate')">{{ detail.dueDate || '—' }}</el-descriptions-item>
            <el-descriptions-item :label="t('businessReports.days')">{{ detail.closed ? '—' : detail.overdueDays }}</el-descriptions-item>
            <el-descriptions-item v-for="col in contractMoney" :key="col.key" :label="t(col.label)">
              {{ money(detail[col.key], detail.currency) }}
            </el-descriptions-item>
            <el-descriptions-item :label="t('businessReports.collectionStatus')">
              {{ t(detail.closed ? 'businessReports.closed' : 'businessReports.open') }}
            </el-descriptions-item>
          </el-descriptions>
          <h3>{{ t('businessReports.directCosts') }}</h3>
          <el-alert v-if="detail.costUnavailable" :title="t('businessReports.costUnavailable')" type="warning" show-icon :closable="false" />
          <el-table v-if="detail.costs?.length" :data="detail.costs" size="small">
            <el-table-column prop="number" :label="t('businessReports.costDocument')" min-width="145" />
            <el-table-column :label="t('businessReports.costType')" width="95">
              <template #default="{ row }">{{ t(row.kind === 'LOGISTICS' ? 'businessReports.freight' : 'businessReports.purchase') }}</template>
            </el-table-column>
            <el-table-column :label="t('businessReports.directCosts')" min-width="120" align="right">
              <template #default="{ row }">{{ money(row.amount, row.currency) }}</template>
            </el-table-column>
          </el-table>
          <p v-if="detail.estimatedMargin">{{ t('businessReports.margin') }} <strong>{{ money(detail.estimatedMargin, detail.currency) }}</strong></p>
          <p v-if="detail.estimatedMargin" class="basis">{{ t('businessReports.marginBasis') }}</p>
          <p v-else-if="!detail.costUnavailable" class="basis">{{ t('businessReports.costPending') }}</p>
          <router-link v-if="auth.can('export:contract:read')" class="contract-link"
            :to="{ path: '/contracts', query: { id: detail.contractId } }">{{ t('businessReports.openContract') }}</router-link>
        </template>
      </div>
    </el-drawer>
  </main>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { get, quietErrors } from '../api'
import { useAuthStore } from '../stores/auth'

type Amount = { currency: string; amount: string }
type Contract = {
  contractId: string; contractNo: string; customer: string; owner: string; dueDate: string;
  currency: string; contractAmount: string; received: string; balance: string; overdueDays: number; closed: boolean;
  costs?: (Amount & { number: string; kind: string })[]; estimatedMargin?: string; costUnavailable?: boolean
}
type SalesRow = { employeeId: string; employee: string; inquiryCount: string; quoteCount: string; signedCount: string; signed: Amount[]; overdue: Amount[] }
type Report = {
  startDate: string; cutoffDate: string; signedCount: string; signed: Amount[]; received: Amount[]; overdue: Amount[];
  months: { month: string; signed: Amount[]; received: Amount[] }[];
  contracts: Contract[]; total: string; missingDueDateCount: string; undatedReceiptCount: string; sales: SalesRow[]
}

const { t, locale } = useI18n()
const auth = useAuthStore()
const businessDate = new Intl.DateTimeFormat('en-CA', { timeZone: 'Asia/Shanghai', year: 'numeric', month: '2-digit' }).formatToParts(new Date())
const currentYear = Number(businessDate.find(part => part.type === 'year')?.value)
const currentMonth = Number(businessDate.find(part => part.type === 'month')?.value)
const currentQuarter = Math.ceil(currentMonth / 3)
const years = Array.from({ length: currentYear - 1899 }, (_, index) => currentYear - index)
const year = ref(currentYear)
const quarter = ref(currentQuarter)
const quarters = computed(() => [1, 2, 3, 4].filter(q => year.value !== currentYear || q <= currentQuarter))
const view = ref('quarter')
const report = ref<Report>()
const loading = ref(false)
const failed = ref(false)
const overdue = ref<Contract[]>([])
const overduePage = ref(1)
const overdueTotal = ref(0)
const overdueLoading = ref(false)
const overdueFailed = ref(false)
const showAllOverdue = ref(false)
const contractPage = ref(1)
const detailOpen = ref(false)
const detail = ref<Contract>()
const detailLoading = ref(false)
const detailFailed = ref(false)
const currency = ref('')
const pageSize = 100
let loadVersion = 0
let overdueVersion = 0
let detailVersion = 0

const contractMoney = [
  { key: 'contractAmount', label: 'businessReports.contractAmount' },
  { key: 'received', label: 'businessReports.received' },
  { key: 'balance', label: 'businessReports.balance' },
] as const

const amountFormat = computed(() => new Intl.NumberFormat(locale.value, { maximumFractionDigits: 2 }))
const formatNumber = (value: number | string) => amountFormat.value.format(Number(value))
const money = (value: string, unit: string) => unit + ' ' + formatNumber(value)
const amounts = (values: Amount[]) => values.length ? [...values].sort((a, b) => a.currency.localeCompare(b.currency)).map(value => money(value.amount, value.currency)) : ['—']
const metrics = computed(() => {
  const data = report.value
  if (!data) return []
  return [
    { label: 'businessReports.signed', values: amounts(data.signed) },
    { label: 'businessReports.count', values: [formatNumber(data.signedCount)] },
    { label: 'businessReports.received', values: amounts(data.received) },
    { label: 'businessReports.overdue', values: amounts(data.overdue), warning: Number(data.total) > 0 },
  ]
})
const currencies = computed(() => [...new Set([...(report.value?.signed ?? []), ...(report.value?.received ?? [])].map(value => value.currency))].sort())
const selectedCurrency = computed(() => currencies.value.includes(currency.value) ? currency.value : currencies.value[0] ?? '')
const trend = computed(() => (report.value?.months ?? []).map(month => ({
  month: month.month,
  signed: Number(month.signed.find(value => value.currency === selectedCurrency.value)?.amount ?? 0),
  received: Number(month.received.find(value => value.currency === selectedCurrency.value)?.amount ?? 0),
})))
const trendMax = computed(() => Math.max(1, ...trend.value.flatMap(month => [Math.abs(month.signed), Math.abs(month.received)])))

const period = () => ({ year: year.value, quarter: quarter.value })
async function load() {
  const version = ++loadVersion
  const selectedView = view.value
  loading.value = true
  failed.value = false
  try {
    const data = await get<Report>('/business-reports/' + selectedView, { ...period(), page: contractPage.value }, quietErrors)
    if (version !== loadVersion) return
    report.value = data
    if (selectedView === 'quarter') {
      if (Number(data.total)) void loadOverdue()
      else { overdue.value = []; overdueTotal.value = 0 }
    }
  } catch {
    if (version === loadVersion) { report.value = undefined; failed.value = true }
  } finally {
    if (version === loadVersion) loading.value = false
  }
}

async function loadOverdue() {
  const version = ++overdueVersion
  overdueLoading.value = true
  overdueFailed.value = false
  try {
    const data = await get<Report>('/business-reports/overdue', { ...period(), page: overduePage.value }, quietErrors)
    if (version === overdueVersion) { overdue.value = data.contracts; overdueTotal.value = Number(data.total) }
  } catch {
    if (version === overdueVersion) { overdue.value = []; overdueFailed.value = true }
  } finally {
    if (version === overdueVersion) overdueLoading.value = false
  }
}

async function openContract(id: string) {
  const version = ++detailVersion
  detailOpen.value = true
  detailLoading.value = true
  detailFailed.value = false
  detail.value = undefined
  try {
    const data = await get<Report>('/business-reports/contracts/' + id, period(), quietErrors)
    if (version === detailVersion) detail.value = data.contracts[0]
  } catch {
    if (version === detailVersion) detailFailed.value = true
  } finally {
    if (version === detailVersion) detailLoading.value = false
  }
}

watch(showAllOverdue, value => {
  if (!value && overduePage.value !== 1) { overduePage.value = 1; void loadOverdue() }
})
watch([year, quarter, view], () => {
  if (!quarters.value.includes(quarter.value)) { quarter.value = quarters.value.at(-1) ?? 1; return }
  ++overdueVersion
  ++detailVersion
  overdueLoading.value = false
  overdueFailed.value = false
  overdue.value = []
  overduePage.value = 1
  overdueTotal.value = 0
  contractPage.value = 1
  showAllOverdue.value = false
  detailOpen.value = false
  report.value = undefined
  void load()
}, { immediate: true })
</script>

<style scoped>
.report-page { padding: 16px 20px; min-width: 0; }
.report-heading, .report-filters, .panel-heading { display: flex; align-items: center; justify-content: space-between; gap: 12px; }
.report-heading h1 { margin: 0; font-size: 23px; line-height: 1.35; }
.report-heading p, .panel-heading > span { margin: 4px 0 0; color: var(--el-text-color-secondary); font-size: 12px; }
.report-page :deep(.el-tabs__header) { margin: 2px 0 12px; }
.metrics { display: grid; grid-template-columns: repeat(4, minmax(0, 1fr)); gap: 12px; margin-bottom: 12px; }
.metric, .report-panel { background: var(--el-bg-color); border: 1px solid var(--el-border-color-light); border-radius: 8px; }
.metric { display: flex; flex-direction: column; gap: 4px; padding: 11px 13px; }
.metric > span { color: var(--el-text-color-secondary); font-size: 12px; }
.metric > strong { font-size: clamp(15px, 1.65vw, 24px); line-height: 1.2; overflow-wrap: anywhere; font-variant-numeric: tabular-nums; }
.metric > strong + strong { font-size: 13px; }
.overdue-text, :deep(.overdue-text) { color: var(--el-color-danger); }
.report-insights { display: grid; grid-template-columns: minmax(0, 1.6fr) minmax(0, 1fr); gap: 12px; margin-bottom: 12px; }
.trend, .summary { padding: 10px 13px; }
h2 { margin: 0; font-size: 15px; font-weight: 650; }
.summary ul { margin: 9px 0 0; padding-left: 18px; font-size: 13px; line-height: 1.65; color: var(--el-text-color-regular); }
.trend-legend { display: grid; grid-template-columns: 58px repeat(2, minmax(0, 1fr)); gap: 8px; margin: 6px 0; font-size: 11px; color: var(--el-text-color-regular); }
.trend-legend i { display: inline-block; width: 8px; height: 8px; border-radius: 2px; margin-right: 4px; }
.signed-bar { background: var(--el-color-primary); }
.received-bar { background: var(--el-color-success); }
.trend-row { display: grid; grid-template-columns: 58px repeat(2, minmax(0, 1fr)); gap: 8px; align-items: center; margin-top: 4px; font-size: 11px; }
.trend-track { position: relative; height: 19px; background: var(--el-fill-color-light); border-radius: 3px; overflow: hidden; text-align: right; }
.trend-bar { position: absolute; left: 0; bottom: 0; height: 3px; }
.trend-bar.negative { background: var(--el-color-danger); }
.trend-track span { position: relative; padding: 0 5px; line-height: 18px; font-variant-numeric: tabular-nums; }
.report-table { overflow: hidden; }
.report-table .panel-heading { padding: 7px 13px; min-height: 22px; }
.report-table small { margin-left: 6px; color: var(--el-text-color-secondary); font-size: 12px; }
.report-table :deep(.el-table__cell) { padding: 3px 0; font-variant-numeric: tabular-nums; }
.report-table :deep(.el-table__cell.is-right > .cell) { text-align: right; }
.report-table :deep(.el-pagination) { justify-content: flex-end; margin: 8px 12px; }
.basis { color: var(--el-text-color-secondary); font-size: 12px; margin: 8px 0 0; }
.detail-content { min-height: 80px; }
.detail-content h2 { margin-bottom: 12px; }
.detail-content h3 { margin: 16px 0 6px; font-size: 14px; }
.detail-content .contract-link { display: inline-block; margin-top: 12px; color: var(--el-color-primary); }
@media (max-width: 850px) {
  .metrics { grid-template-columns: repeat(2, minmax(0, 1fr)); }
  .report-insights { grid-template-columns: minmax(0, 1fr); }
}
</style>
