<template>
  <div class="page">
    <header class="page-head">
      <div>
        <div class="eyebrow">{{ t('execution.eyebrow') }}</div>
        <h1>{{ t('execution.title') }}</h1>
        <p>{{ t('execution.subtitle') }}</p>
      </div>
    </header>

    <!-- 卡在哪一段，是这页要一眼答出来的问题。三个数字各自对应一段停住的
         活：货没订、订了没走、走了没收。合计按整个范围算而不是当前页——
         只统计一页的数字会在翻页时变化，那就不叫合计。 -->
    <section class="metrics">
      <!-- 采购那条腿取不回来时这个数字必须让位。「一项都没订」和「问不到
           采购」在算式里长得一模一样，照常显示就会报出一个吓人的假数——
           那正是这一页要消灭的东西。 -->
      <div class="metric" :class="{ 'is-warn': (stalled.procurement ?? 0) > 0 }">
        <span class="metric-label">{{ t('execution.stalledProcurement') }}</span>
        <strong class="metric-value" :class="{ 'is-unknown': stalled.procurement === null }">
          {{ stalled.procurement ?? t('execution.unavailable') }}
        </strong>
        <span class="metric-hint">{{ t('execution.stalledProcurementHint') }}</span>
      </div>
      <div class="metric" :class="{ 'is-warn': stalled.shipping > 0 }">
        <span class="metric-label">{{ t('execution.stalledShipping') }}</span>
        <strong class="metric-value">{{ stalled.shipping }}</strong>
        <span class="metric-hint">{{ t('execution.stalledShippingHint') }}</span>
      </div>
      <div class="metric" :class="{ 'is-alarm': stalled.overdue > 0 }">
        <span class="metric-label">{{ t('execution.stalledMoney') }}</span>
        <strong class="metric-value">{{ stalled.overdue }}</strong>
        <span class="metric-hint">{{ t('execution.stalledMoneyHint') }}</span>
      </div>
    </section>

    <section class="panel">
      <div class="filters">
        <!-- 空值就是「在跑的」这个默认，所以占位文案必须写成它，不能留
             「请选择」——那会让人以为还没筛，其实已经筛掉了草稿和作废的。 -->
        <el-select
          v-model="status"
          style="width: 150px"
          :placeholder="t('execution.statusRunning')"
          @change="reload"
        >
          <el-option value="" :label="t('execution.statusRunning')" />
          <el-option value="EFFECTIVE" :label="t('contracts.statuses.EFFECTIVE')" />
          <el-option value="EXECUTING" :label="t('contracts.statuses.EXECUTING')" />
          <el-option value="COMPLETED" :label="t('contracts.statuses.COMPLETED')" />
        </el-select>
        <el-input
          v-model="keyword"
          clearable
          :placeholder="t('execution.search')"
          style="max-width: 240px"
          @keyup.enter="reload"
          @clear="reload"
        />
        <el-button type="primary" @click="reload">{{ t('common.query') }}</el-button>
      </div>

      <!-- 一条腿取不回来时明说取不到，不把那一列显示成零。零在这张表上的
           意思是「还没开始」，和「服务连不上」是完全不同的两件事。 -->
      <el-alert
        v-if="!procurementAvailable || !shippingAvailable"
        type="warning"
        show-icon
        :closable="false"
        class="alert"
        :title="unavailableNote"
      />

      <el-table v-loading="loading" :data="rows">
        <el-table-column :label="t('execution.contract')" min-width="190" fixed>
          <template #default="{ row }">
            <router-link :to="`/contracts?id=${row.contractId}`" class="doc-link">{{ row.contractNo }}</router-link>
            <div class="sub">{{ row.customerName }}</div>
          </template>
        </el-table-column>

        <el-table-column :label="t('execution.amount')" width="150" align="right">
          <template #default="{ row }">
            <span class="num money">{{ row.currency }} {{ money(row.totalAmount) }}</span>
            <div class="sub">{{ row.effectiveDate || '—' }}</div>
          </template>
        </el-table-column>

        <el-table-column :label="t('execution.stageProcurement')" width="180">
          <template #default="{ row }">
            <template v-if="procurementAvailable">
              <span class="stage" :class="`is-${procurementStage(row).tone}`">{{ procurementStage(row).label }}</span>
              <div class="sub">{{ procurementStage(row).detail }}</div>
            </template>
            <span v-else class="stage is-unknown">{{ t('execution.unavailable') }}</span>
          </template>
        </el-table-column>

        <el-table-column :label="t('execution.stageShipping')" width="170">
          <template #default="{ row }">
            <span class="stage" :class="`is-${shipStage(row).tone}`">{{ shipStage(row).label }}</span>
            <div class="sub">{{ shipStage(row).detail }}</div>
          </template>
        </el-table-column>

        <el-table-column :label="t('execution.stageVessel')" width="190">
          <template #default="{ row }">
            <template v-if="shippingAvailable">
              <span class="stage" :class="`is-${vesselStage(row).tone}`">{{ vesselStage(row).label }}</span>
              <div class="sub">{{ vesselStage(row).detail }}</div>
            </template>
            <span v-else class="stage is-unknown">{{ t('execution.unavailable') }}</span>
          </template>
        </el-table-column>

        <el-table-column :label="t('execution.stageMoney')" width="210">
          <template #default="{ row }">
            <span class="stage" :class="`is-${moneyStage(row).tone}`">{{ moneyStage(row).label }}</span>
            <div class="sub">{{ moneyStage(row).detail }}</div>
          </template>
        </el-table-column>

        <el-table-column :label="t('execution.owner')" min-width="110">
          <template #default="{ row }">{{ row.salesEmployee || '—' }}</template>
        </el-table-column>
        <template #empty>{{ t('execution.empty') }}</template>
      </el-table>

      <el-pagination
        class="pager"
        layout="total, prev, pager, next"
        :total="total"
        :page-size="pageSize"
        :current-page="page"
        @current-change="(p: number) => { page = p; load() }"
      />
    </section>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { get } from '../api'

const { t } = useI18n()

interface Procurement { totalLines: number; orderedLines: number; receivedLines: number }
interface Shipping {
  scheduleId: string; scheduleNo: string; vesselName: string; voyageNo: string
  status: string; etd: string; atd: string; eta: string; ata: string
  delayDays: number; legCount: number
}
interface Row {
  contractId: string
  contractNo: string
  customerId: string
  customerName: string
  salesEmployeeId: string
  salesEmployee: string
  status: string
  effectiveDate: string
  currency: string
  totalAmount: string
  shippedAmount: string
  receivedAmount: string
  dueDate: string
  overdueDays: number
  dueUnset: boolean
  procurement: Procurement
  shipping: Shipping | null
}
interface Board {
  rows: Row[]
  meta: { total: string }
  procurementAvailable: boolean
  shippingAvailable: boolean
}

const rows = ref<Row[]>([])
const total = ref(0)
const page = ref(1)
const pageSize = 50
const status = ref('')
const keyword = ref('')
const loading = ref(false)
const procurementAvailable = ref(true)
const shippingAvailable = ref(true)
// procurement 为 null 表示那条腿这次没问到——不是零。
const stalled = ref<{ procurement: number | null; shipping: number; overdue: number }>(
  { procurement: 0, shipping: 0, overdue: 0 },
)

interface Stage { label: string; detail: string; tone: 'idle' | 'doing' | 'done' | 'warn' | 'alarm' | 'unknown' }

function money(v: string): string {
  const n = Number(v)
  return Number.isFinite(n) ? n.toLocaleString(undefined, { minimumFractionDigits: 2, maximumFractionDigits: 2 }) : v
}

function pct(part: string, whole: string): number {
  const w = Number(whole)
  if (!Number.isFinite(w) || w <= 0) return 0
  return Math.round((Number(part) / w) * 100)
}

// 采购按项数说：一张合同上 100 吨和 50 件加不起来，这是采购员口头汇报
// 的说法，也是唯一单位无关的说法。
function procurementStage(row: Row): Stage {
  const p = row.procurement
  if (p.totalLines === 0) return { label: t('execution.notStarted'), detail: t('execution.noRequirement'), tone: 'idle' }
  if (p.receivedLines >= p.totalLines) {
    return { label: t('execution.allArrived'), detail: t('execution.linesOf', { n: p.totalLines }), tone: 'done' }
  }
  if (p.orderedLines >= p.totalLines) {
    return {
      label: t('execution.allOrdered'),
      detail: t('execution.arrivedOf', { done: p.receivedLines, total: p.totalLines }),
      tone: 'doing',
    }
  }
  return {
    label: t('execution.partlyOrdered'),
    detail: t('execution.orderedOf', { done: p.orderedLines, total: p.totalLines }),
    tone: 'warn',
  }
}

function shipStage(row: Row): Stage {
  const p = pct(row.shippedAmount, row.totalAmount)
  const detail = `${row.currency} ${money(row.shippedAmount)}`
  if (p <= 0) return { label: t('execution.notShipped'), detail: '', tone: 'idle' }
  // 超过 100% 是真事：改版把量调小而货已经发了。露出来才有人去查。
  if (p > 100) return { label: t('execution.overShipped', { n: p }), detail, tone: 'alarm' }
  if (p >= 100) return { label: t('execution.allShipped'), detail, tone: 'done' }
  return { label: t('execution.shippedPct', { n: p }), detail, tone: 'doing' }
}

function vesselStage(row: Row): Stage {
  const s = row.shipping
  if (!s) return { label: t('execution.noBooking'), detail: '', tone: 'idle' }
  const legs = s.legCount > 1 ? t('execution.legs', { n: s.legCount }) : ''
  if (s.ata) {
    return { label: t('execution.arrivedPort'), detail: [s.ata, legs].filter(Boolean).join(' · '), tone: 'done' }
  }
  const eta = s.eta ? t('execution.etaOn', { d: s.eta }) : ''
  if (s.delayDays > 0) {
    return {
      label: t('execution.delayed', { n: s.delayDays }),
      detail: [eta, legs].filter(Boolean).join(' · '),
      tone: 'alarm',
    }
  }
  if (s.atd) return { label: t('execution.sailed'), detail: [eta, legs].filter(Boolean).join(' · '), tone: 'doing' }
  return { label: t('execution.booked'), detail: [s.etd ? t('execution.etdOn', { d: s.etd }) : '', legs].filter(Boolean).join(' · '), tone: 'doing' }
}

function moneyStage(row: Row): Stage {
  const p = pct(row.receivedAmount, row.totalAmount)
  const detail = `${row.currency} ${money(row.receivedAmount)}`
  if (p >= 100) return { label: t('execution.paidUp'), detail, tone: 'done' }
  if (!row.dueUnset && row.overdueDays > 0) {
    return { label: t('execution.overdueBy', { n: row.overdueDays }), detail, tone: 'alarm' }
  }
  if (p <= 0) {
    return {
      label: t('execution.notPaid'),
      detail: row.dueUnset ? t('execution.dueUnset') : t('execution.dueOn', { d: row.dueDate }),
      tone: 'warn',
    }
  }
  return { label: t('execution.paidPct', { n: p }), detail, tone: 'doing' }
}

const unavailableNote = computed(() => {
  const legs: string[] = []
  if (!procurementAvailable.value) legs.push(t('execution.legProcurement'))
  if (!shippingAvailable.value) legs.push(t('execution.legShipping'))
  return t('execution.legUnavailable', { legs: legs.join('、') })
})

async function fetchBoard(params: Record<string, string | number>) {
  return get<Board>('/contract-execution', params)
}

async function load() {
  loading.value = true
  try {
    const d = await fetchBoard({
      page: page.value, page_size: pageSize,
      status: status.value, keyword: keyword.value,
    })
    rows.value = d.rows ?? []
    total.value = Number(d.meta?.total ?? 0)
    procurementAvailable.value = d.procurementAvailable !== false
    shippingAvailable.value = d.shippingAvailable !== false
  } finally {
    loading.value = false
  }
}

// 三个合计拉一页大的来算。服务端没有「卡在某一段」这种筛子，为三个提示
// 数字再开三个接口不划算；上限 200 张在跑的合同对这家公司够用，超出时
// 数字会偏小而不是偏大——宁可少报也不要虚报一个让人放心的数。
async function loadMetrics() {
  const d = await fetchBoard({ page: 1, page_size: 200, status: status.value, keyword: keyword.value })
  const all = d.rows ?? []
  stalled.value = {
    procurement: d.procurementAvailable === false ? null
      : all.filter((r) => r.procurement.totalLines === 0
        || r.procurement.orderedLines < r.procurement.totalLines).length,
    shipping: all.filter((r) => Number(r.shippedAmount) <= 0).length,
    overdue: all.filter((r) => !r.dueUnset && r.overdueDays > 0
      && Number(r.receivedAmount) < Number(r.totalAmount)).length,
  }
}

function reload() {
  page.value = 1
  load()
  loadMetrics()
}

onMounted(() => {
  load()
  loadMetrics()
})
</script>

<style scoped>
.page {
  display: flex;
  flex-direction: column;
  gap: 18px;
}
.page-head .eyebrow {
  font-size: 12px;
  letter-spacing: 1.5px;
  color: var(--el-text-color-secondary);
}
.page-head h1 {
  margin: 4px 0 6px;
  font-size: 28px;
}
.page-head p {
  margin: 0;
  color: var(--el-text-color-regular);
}
.metrics {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(200px, 1fr));
  gap: 14px;
}
.metric {
  display: flex;
  flex-direction: column;
  gap: 4px;
  padding: 14px 16px;
  border: 1px solid var(--el-border-color-lighter);
  border-radius: 10px;
  background: var(--el-bg-color);
}
.metric.is-alarm {
  border-color: var(--el-color-danger-light-5);
  background: var(--el-color-danger-light-9);
}
.metric.is-warn {
  border-color: var(--el-color-warning-light-5);
  background: var(--el-color-warning-light-9);
}
.metric-label {
  font-size: 13px;
  color: var(--el-text-color-secondary);
}
.metric-value {
  font-size: 26px;
  font-variant-numeric: tabular-nums;
}
.metric-value.is-unknown {
  font-size: 18px;
  color: var(--el-text-color-placeholder);
}
.metric-hint {
  font-size: 12px;
  color: var(--el-text-color-placeholder);
}
.panel {
  padding: 16px;
  border: 1px solid var(--el-border-color-lighter);
  border-radius: 10px;
  background: var(--el-bg-color);
}
.filters {
  display: flex;
  align-items: center;
  gap: 10px;
  margin-bottom: 14px;
  flex-wrap: wrap;
}
.alert {
  margin-bottom: 12px;
}
/* 四段进程一个视觉规则：左边一道竖色条说状态，字说程度。竖着扫一列就
   看得出这批单子卡在哪一段。 */
.stage {
  display: inline-block;
  padding-left: 8px;
  border-left: 3px solid var(--el-border-color);
  font-size: 13px;
  line-height: 1.4;
  color: var(--el-text-color-regular);
}
.stage.is-idle {
  border-left-color: var(--el-border-color);
  color: var(--el-text-color-placeholder);
}
.stage.is-doing {
  border-left-color: var(--el-color-primary);
}
.stage.is-done {
  border-left-color: var(--el-color-success);
}
.stage.is-warn {
  border-left-color: var(--el-color-warning);
  color: var(--el-color-warning);
}
.stage.is-alarm {
  border-left-color: var(--el-color-danger);
  color: var(--el-color-danger);
  font-weight: 600;
}
.stage.is-unknown {
  border-left-style: dashed;
  color: var(--el-text-color-placeholder);
}
.sub {
  margin-top: 2px;
  font-size: 12px;
  color: var(--el-text-color-secondary);
}
/* 只有跟在进程条后面的说明才缩进，好和上面那道竖条对齐。 */
.stage + .sub {
  padding-left: 11px;
}
.num {
  font-variant-numeric: tabular-nums;
}
.num.money {
  font-weight: 600;
}
.doc-link {
  color: var(--el-color-primary);
  text-decoration: none;
}
.doc-link:hover {
  text-decoration: underline;
}
.pager {
  margin-top: 14px;
  justify-content: flex-end;
}
</style>
