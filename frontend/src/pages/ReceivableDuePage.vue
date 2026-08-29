<template>
  <div class="page">
    <header class="page-head">
      <div>
        <div class="eyebrow">{{ t('receivableDue.eyebrow') }}</div>
        <h1>{{ t('receivableDue.title') }}</h1>
        <p>{{ t('receivableDue.subtitle') }}</p>
      </div>
    </header>

    <!-- 三个数字，回答财务开门第一句话：现在有多少钱该收而没收到。
         逾期额单独一栏并染红——总额里混着还没到期的，那个数字安慰人，
         逾期额才是要打电话的理由。 -->
    <section class="metrics">
      <div class="metric" :class="{ 'is-alarm': overdueCount > 0 }">
        <span class="metric-label">{{ t('receivableDue.overdueCount') }}</span>
        <strong class="metric-value">{{ overdueCount }}</strong>
        <span class="metric-hint">{{ t('receivableDue.overdueHint') }}</span>
      </div>
      <div class="metric">
        <span class="metric-label">{{ t('receivableDue.dueSoonCount') }}</span>
        <strong class="metric-value">{{ dueSoonCount }}</strong>
        <span class="metric-hint">{{ t('receivableDue.dueSoonHint') }}</span>
      </div>
      <div class="metric" :class="{ 'is-warn': unsetCount > 0 }">
        <span class="metric-label">{{ t('receivableDue.unsetCount') }}</span>
        <strong class="metric-value">{{ unsetCount }}</strong>
        <span class="metric-hint">{{ t('receivableDue.unsetHint') }}</span>
      </div>
    </section>

    <section class="panel">
      <div class="filters">
        <!-- 待核销 / 已完成是两条独立地址（菜单高亮按精确路径判断），
             这里的三个只是待核销页内部的收窄筛子。 -->
        <el-radio-group v-if="!isDone" v-model="view" @change="reload">
          <el-radio-button value="">{{ t('receivableDue.viewAll') }}</el-radio-button>
          <el-radio-button value="overdue">{{ t('receivableDue.viewOverdue') }}</el-radio-button>
          <el-radio-button value="unset">{{ t('receivableDue.viewUnset') }}</el-radio-button>
        </el-radio-group>
        <el-input
          v-model="keyword"
          clearable
          :placeholder="t('receivableDue.search')"
          style="max-width: 240px"
          @keyup.enter="reload"
          @clear="reload"
        />
        <el-button type="primary" @click="reload">{{ t('common.query') }}</el-button>
      </div>

      <el-table v-loading="loading" :data="rows" :row-class-name="rowClass" @expand-change="onExpand">
        <!-- 展开看这张合同收过哪几笔。明细按需加载：每行都预先拉一次，
             一页就是 50 次往返。 -->
        <el-table-column type="expand">
          <template #default="{ row }">
            <div class="entries">
              <el-table v-if="entriesOf(row).length" :data="entriesOf(row)" size="small">
                <el-table-column :label="t('receivableDue.entryDate')" width="120">
                  <template #default="{ row: e }">{{ e.receivedAt || '—' }}</template>
                </el-table-column>
                <el-table-column :label="t('receivableDue.entryAmount')" width="150" align="right">
                  <template #default="{ row: e }">
                    <span :class="{ negative: Number(e.amount) < 0 }">{{ e.currency }} {{ e.amount }}</span>
                    <div v-if="Number(e.feeAmount)" class="sub">
                      {{ t('receivableDue.entryFee', { n: e.feeAmount }) }}
                    </div>
                  </template>
                </el-table-column>
                <el-table-column :label="t('receivableDue.entryNote')" min-width="180">
                  <template #default="{ row: e }">
                    <span v-if="Number(e.reversalOf)">{{ t('receivableDue.entryReversal') }} · {{ e.reverseReason }}</span>
                    <span v-else>{{ e.note || '—' }}</span>
                  </template>
                </el-table-column>
                <el-table-column :label="t('receivableDue.entryBy')" width="160">
                  <template #default="{ row: e }">{{ e.allocatedByName }}<div class="sub">{{ e.allocatedAt }}</div></template>
                </el-table-column>
                <el-table-column width="90">
                  <template #default="{ row: e }">
                    <el-button
                      v-if="canWrite && !Number(e.reversalOf) && !reversedIds(row).has(String(e.allocationId))"
                      link type="danger" @click="reverseEntry(row, e)"
                    >{{ t('receivableDue.entryReverse') }}</el-button>
                  </template>
                </el-table-column>
              </el-table>
              <p v-else class="sub">{{ t('receivableDue.entriesEmpty') }}</p>
            </div>
          </template>
        </el-table-column>
        <el-table-column :label="t('receivableDue.contract')" min-width="180">
          <template #default="{ row }">
            <router-link :to="`/contracts?id=${row.contractId}`" class="doc-link">{{ row.contractNo }}</router-link>
            <div class="sub">{{ row.customerName }}</div>
          </template>
        </el-table-column>
        <el-table-column :label="t('receivableDue.dueDate')" width="150">
          <template #default="{ row }">
            <template v-if="row.dueUnset">
              <el-tag size="small" type="warning" effect="plain">{{ t('receivableDue.unsetTag') }}</el-tag>
            </template>
            <template v-else>
              <div>{{ row.dueDate }}</div>
              <div class="sub" :class="{ overdue: row.overdueDays > 0 }">{{ dueLabel(row) }}</div>
            </template>
          </template>
        </el-table-column>
        <el-table-column :label="t('receivableDue.openAmount')" width="160" align="right">
          <template #default="{ row }">
            <span class="num money" :class="{ overdue: row.overdueDays > 0 && !row.dueUnset }">
              {{ row.currency }} {{ row.openAmount }}
            </span>
            <div class="sub">{{ t('receivableDue.ofTotal', { total: row.totalAmount }) }}</div>
          </template>
        </el-table-column>
        <el-table-column :label="t('receivableDue.received')" width="130" align="right">
          <template #default="{ row }">
            <span class="num" :class="{ dim: Number(row.receivedAmount) === 0 }">{{ row.receivedAmount }}</span>
          </template>
        </el-table-column>
        <el-table-column :label="t('receivableDue.effectiveDate')" width="120">
          <template #default="{ row }">{{ row.effectiveDate || '—' }}</template>
        </el-table-column>
        <el-table-column :label="t('receivableDue.owner')" min-width="110">
          <template #default="{ row }">{{ row.salesEmployee || '—' }}</template>
        </el-table-column>
        <!-- 已结清视图多一列：为什么不催了、谁定的。 -->
        <el-table-column v-if="isDone" :label="t('receivableDue.closedWhy')" min-width="170">
          <template #default="{ row }">
            <el-tag size="small" effect="plain">{{ t(`receivableDue.closureCategories.${row.closedCategory}`) }}</el-tag>
            <div class="sub">{{ row.closedByName }}<template v-if="row.closedNote"> · {{ row.closedNote }}</template></div>
          </template>
        </el-table-column>
        <el-table-column v-if="canWrite" :label="t('common.actions')" width="230" fixed="right">
          <template #default="{ row }">
            <!-- 已完成的合同照样能记钱：钱真的又来了，先记上再撤销完成。 -->
            <el-button link type="primary" @click="openEntry(row)">
              {{ t('receivableDue.addReceipt') }}
            </el-button>
            <el-button v-if="isDone" link type="warning" @click="reopenRow(row)">
              {{ t('receivableDue.reopen') }}
            </el-button>
            <el-button v-else link type="success" @click="openClose(row)">
              {{ t('receivableDue.close') }}
            </el-button>
          </template>
        </el-table-column>
        <template #empty>{{ emptyText }}</template>
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

    <!-- 收款结清：这张合同的钱「不用再催了」。三个数并排亮着，员工看着差额
         做决定——这正是「完成由人确认」那条原则在合同侧的样子。只关催收的
         口，不关钱的门：结清的合同照样能核销，钱真的又来了就撤销。 -->
    <el-dialog v-model="closeOpen" :title="t('receivableDue.closeTitle')" width="min(520px, 94vw)" destroy-on-close>
      <template v-if="closing">
        <p class="close-target">{{ closing.contractNo }} · {{ closing.customerName }}</p>
        <div class="close-figures">
          <div><span class="sub">{{ t('receivableDue.figTotal') }}</span><span class="num">{{ closing.currency }} {{ closing.totalAmount }}</span></div>
          <div><span class="sub">{{ t('receivableDue.figReceived') }}</span><span class="num">{{ closing.receivedAmount }}</span></div>
          <div><span class="sub">{{ t('receivableDue.figOpen') }}</span><span class="num warn">{{ closing.openAmount }}</span></div>
        </div>
        <el-form label-position="top">
          <el-form-item :label="t('receivableDue.closeWhat')">
            <el-radio-group v-model="closeForm.category">
              <el-radio v-for="k in CLOSURE_CATEGORIES" :key="k" :value="k">
                {{ t(`receivableDue.closureCategories.${k}`) }}
              </el-radio>
            </el-radio-group>
          </el-form-item>
          <el-form-item :label="t('receivableDue.closeNote')">
            <el-input v-model="closeForm.note" :placeholder="t('receivableDue.closeNoteHint')" />
          </el-form-item>
        </el-form>
        <el-alert type="info" :closable="false" show-icon :title="t('receivableDue.closeHint')" />
      </template>
      <template #footer>
        <el-button @click="closeOpen = false">{{ t('common.cancel') }}</el-button>
        <el-button type="primary" :loading="closingBusy" @click="submitClose">{{ t('receivableDue.closeConfirm') }}</el-button>
      </template>
    </el-dialog>

    <!-- 记一笔收款：选中的合同已经定了，员工只填「多少钱、哪天到的」。
         币种跟合同走，不问——只有一个正确答案的问题只会制造错答案。 -->
    <el-dialog v-model="entryOpen" :title="t('receivableDue.addReceiptTitle')" width="min(520px, 94vw)" destroy-on-close>
      <template v-if="entryRow">
        <p class="close-target">{{ entryRow.contractNo }} · {{ entryRow.customerName }}</p>
        <div class="close-figures">
          <div><span class="sub">{{ t('receivableDue.figTotal') }}</span><span class="num">{{ entryRow.currency }} {{ entryRow.totalAmount }}</span></div>
          <div><span class="sub">{{ t('receivableDue.figReceived') }}</span><span class="num">{{ entryRow.receivedAmount }}</span></div>
          <div><span class="sub">{{ t('receivableDue.figOpen') }}</span><span class="num warn">{{ entryRow.openAmount }}</span></div>
        </div>
        <el-form label-position="top">
          <el-form-item :label="t('receivableDue.entryKind')">
            <el-radio-group v-model="entryForm.isRefund">
              <el-radio-button :value="false">{{ t('receivableDue.kindReceipt') }}</el-radio-button>
              <el-radio-button :value="true">{{ t('receivableDue.kindRefund') }}</el-radio-button>
            </el-radio-group>
          </el-form-item>
          <el-form-item :label="t('receivableDue.entryAmount')">
            <el-input v-model="entryForm.amount" :placeholder="t('receivableDue.entryAmountHint')">
              <template #prepend>{{ entryRow.currency }}</template>
            </el-input>
          </el-form-item>
          <el-form-item :label="t('receivableDue.entryDate')">
            <el-date-picker v-model="entryForm.receivedAt" type="date" value-format="YYYY-MM-DD" style="width: 100%" />
          </el-form-item>
          <el-form-item :label="t('receivableDue.entryNote')">
            <el-input v-model="entryForm.note" :placeholder="t('receivableDue.entryNoteHint')" />
          </el-form-item>
        </el-form>
        <el-alert type="info" :closable="false" show-icon :title="t('receivableDue.entryHint')" />
      </template>
      <template #footer>
        <el-button @click="entryOpen = false">{{ t('common.cancel') }}</el-button>
        <el-button type="primary" :loading="entryBusy" @click="submitEntry">{{ t('common.save') }}</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRoute } from 'vue-router'
import { ElMessage, ElMessageBox } from 'element-plus'
import { get, post } from '../api'
import { useAuthStore } from '../stores/auth'

const { t } = useI18n()
const route = useRoute()
const auth = useAuthStore()
const canWrite = auth.can('export:receipt:write')

interface Row {
  contractId: string
  contractNo: string
  customerId: string
  customerName: string
  salesEmployeeId: string
  salesEmployee: string
  dueDate: string
  effectiveDate: string
  currency: string
  totalAmount: string
  receivedAmount: string
  openAmount: string
  overdueDays: number
  dueUnset: boolean
  closedCategory: string
  closedNote: string
  closedByName: string
  closedAt: string
}

interface Entry {
  allocationId: string
  amount: string
  // 老行才可能非零：那是「不从银行那一笔出、但要把合同补满」的补足额，
  // 行上的「已收」算的是 amount + feeAmount。不显示它，明细加起来会
  // 比行上的已收少，而页面不解释差在哪。新的手工记账恒为 0。
  feeAmount: string
  currency: string
  receivedAt: string
  note: string
  reversalOf: string
  reverseReason: string
  allocatedByName: string
  allocatedAt: string
}

const rows = ref<Row[]>([])
const total = ref(0)
const page = ref(1)
const pageSize = 50
const view = ref('')
// 待核销 / 已完成是两条独立地址指向同一个组件——菜单高亮按精确路径相等
// 判断，用一页带 query 的写法菜单不会亮。
const isDone = computed(() => route.path.endsWith('-done'))
const emptyText = computed(() => {
  if (isDone.value) return t('receivableDue.emptyDone')
  return view.value === 'overdue' ? t('receivableDue.emptyOverdue') : t('receivableDue.empty')
})
const keyword = ref(String(route.query.keyword ?? ''))
const loading = ref(false)

// 三个数字算的是「当前这一页之外的全局」，所以各查一次 total——
// 页面上的合计如果只统计当前页，会在翻页时变化，那就不是合计了。
const overdueCount = ref(0)
const dueSoonCount = ref(0)
const unsetCount = ref(0)

const dueSoonDays = 30

function rowClass({ row }: { row: Row }) {
  if (row.dueUnset) return 'row-unset'
  return row.overdueDays > 0 ? 'row-overdue' : ''
}

function dueLabel(row: Row): string {
  if (row.overdueDays > 0) return t('receivableDue.overdueBy', { n: row.overdueDays })
  if (row.overdueDays === 0) return t('receivableDue.dueToday')
  return t('receivableDue.dueIn', { n: -row.overdueDays })
}

async function fetchPage(params: Record<string, string | number>) {
  return get<{ items: Row[]; meta: { total: string } }>('/receivable-due', params)
}

async function load() {
  loading.value = true
  try {
    const d = await fetchPage({
      page: page.value, page_size: pageSize,
      overdue: !isDone.value && view.value === 'overdue' ? '1' : '',
      unset: !isDone.value && view.value === 'unset' ? '1' : '',
      closed: isDone.value ? '1' : '',
      keyword: keyword.value,
    })
    rows.value = d.items ?? []
    total.value = Number(d.meta?.total ?? 0)
  } finally {
    loading.value = false
  }
}

async function loadMetrics() {
  const [overdue, unset, all] = await Promise.all([
    fetchPage({ page: 1, page_size: 1, overdue: '1' }),
    fetchPage({ page: 1, page_size: 1, unset: '1' }),
    // 「即将到期」没有独立筛子（服务端只认逾期/未配置两个），所以拉一页
    // 算：够用且不必为一个提示数字再开一个接口。
    fetchPage({ page: 1, page_size: 200 }),
  ])
  overdueCount.value = Number(overdue.meta?.total ?? 0)
  unsetCount.value = Number(unset.meta?.total ?? 0)
  dueSoonCount.value = (all.items ?? []).filter(
    (r) => !r.dueUnset && r.overdueDays <= 0 && -r.overdueDays <= dueSoonDays,
  ).length
}

// ── 记一笔收款 ────────────────────────────────────────────
//
// 选中的合同已经定了，员工只填「多少钱、哪天到的」。**不连银行流水**：
// 那本账只用来存银行给的对账单，这里一根线都不牵。
const entryOpen = ref(false)
const entryBusy = ref(false)
const entryRow = ref<Row | null>(null)
const entryForm = ref({ amount: '', isRefund: false, receivedAt: '', note: '' })
// 展开行的明细，按合同缓存——展开一次拉一次，不预先拉。
const entries = ref<Record<string, Entry[]>>({})

function entriesOf(row: Row): Entry[] {
  return entries.value[String(row.contractId)] ?? []
}

// 冲销记录点名它冲的是谁，被点名的那条就不该再有冲销按钮。
function reversedIds(row: Row): Set<string> {
  return new Set(entriesOf(row).filter((e) => Number(e.reversalOf)).map((e) => String(e.reversalOf)))
}

async function loadEntries(row: Row) {
  const d = await get<{ receipts: Entry[] }>(`/contracts/${row.contractId}/receipts`)
  entries.value = { ...entries.value, [String(row.contractId)]: d.receipts ?? [] }
}

function onExpand(row: Row, expanded: Row[]) {
  if (expanded.includes(row)) void loadEntries(row)
}

function openEntry(row: Row) {
  entryRow.value = row
  entryForm.value = { amount: '', isRefund: false, receivedAt: '', note: '' }
  entryOpen.value = true
}

async function submitEntry() {
  if (!entryRow.value) return
  if (!entryForm.value.amount.trim()) {
    ElMessage.warning(t('receivableDue.entryAmountRequired'))
    return
  }
  entryBusy.value = true
  try {
    await post(`/receivable-due/${entryRow.value.contractId}/receipts`, {
      amount: entryForm.value.amount,
      isRefund: entryForm.value.isRefund,
      receivedAt: entryForm.value.receivedAt,
      note: entryForm.value.note,
    })
    entryOpen.value = false
    ElMessage.success(t('receivableDue.entrySaved'))
    await loadEntries(entryRow.value)
    reload()
  } catch { /* surfaced by the api layer */ } finally {
    entryBusy.value = false
  }
}

async function reverseEntry(row: Row, e: Entry) {
  // 冲销必须给理由，和撤销完成同一条纪律。
  const { value } = await ElMessageBox.prompt(
    t('receivableDue.entryReverseWhy'), t('receivableDue.entryReverse'),
    { inputPlaceholder: t('receivableDue.entryReverseReason') },
  ).catch(() => ({ value: '' }))
  if (!value) return
  await post(`/contract-receipts/${e.allocationId}/reverse`, { reason: value })
  ElMessage.success(t('receivableDue.entryReversed'))
  await loadEntries(row)
  reload()
}

// ── 确认核销完成 ──────────────────────────────────────────
//
// 「正常收完」排在第一档：新模型下最常见的完成理由。其余四档是「算式说
// 还欠、人说不欠了」的各种情形。
const CLOSURE_CATEGORIES = ['SETTLED', 'LOSS', 'ROUNDING', 'CANCELLED', 'OTHER'] as const
const closeOpen = ref(false)
const closingBusy = ref(false)
const closing = ref<Row | null>(null)
const closeForm = ref({ category: 'SETTLED', note: '' })

function openClose(row: Row) {
  closing.value = row
  closeForm.value = { category: 'SETTLED', note: '' }
  closeOpen.value = true
}

async function submitClose() {
  if (!closing.value) return
  closingBusy.value = true
  try {
    await post(`/receivable-due/${closing.value.contractId}/close`, {
      category: closeForm.value.category, note: closeForm.value.note,
    })
    closeOpen.value = false
    ElMessage.success(t('receivableDue.closed'))
    reload()
  } finally {
    closingBusy.value = false
  }
}

async function reopenRow(row: Row) {
  // 撤销必须给理由，和冲销、认差撤销同一条纪律。
  const { value } = await ElMessageBox.prompt(
    t('receivableDue.reopenWhy', { no: row.contractNo }), t('receivableDue.reopen'),
    { inputPlaceholder: t('receivableDue.reopenReason') },
  ).catch(() => ({ value: '' }))
  if (!value) return
  await post(`/receivable-due/${row.contractId}/reopen`, { reason: value })
  ElMessage.success(t('receivableDue.reopened'))
  reload()
}

function reload() {
  page.value = 1
  load()
  loadMetrics()
}

watch(() => route.query.keyword, (value) => {
  keyword.value = String(value ?? '')
  reload()
})

// 待核销 ⇄ 已完成走的是两条地址、**同一个组件实例**——vue-router 原地复用，
// setup 和 onMounted 都不会再跑一遍。不盯着 path 重新拉数据的话，切过去
// 表头和按钮都变了（isDone 是响应式的），表格里却还是上一页那批数据：
// 对着一张从没确认过的合同点「撤销完成」，或者对着已完成的点「确认完成」。
// 采购寻源那个列表页早就踩过同一个坑（SourcingCasesListPage 里有同款 watch）。
watch(() => route.path, () => {
  view.value = ''
  entries.value = {}
  reload()
})

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
.sub {
  font-size: 12px;
  color: var(--el-text-color-secondary);
}
.sub.overdue,
.num.overdue {
  color: var(--el-color-danger);
}
.num {
  font-variant-numeric: tabular-nums;
}
.num.money {
  font-weight: 600;
}
.num.dim {
  color: var(--el-text-color-placeholder);
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
:deep(.row-overdue) {
  background: var(--el-color-danger-light-9);
}
:deep(.row-unset) {
  background: var(--el-color-warning-light-9);
}
.close-target {
  margin: 0 0 10px;
  font-weight: 600;
}
.close-figures {
  display: flex;
  gap: 24px;
  margin-bottom: 14px;
}
.close-figures > div {
  display: flex;
  flex-direction: column;
  gap: 2px;
}
.close-figures .warn {
  color: var(--el-color-warning);
}
</style>
