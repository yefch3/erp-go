<template>
  <div class="page">
    <header class="page-head">
      <div>
        <div class="eyebrow">{{ t('supplierRecon.eyebrow') }}</div>
        <h1>{{ t('supplierRecon.title') }}</h1>
        <p>{{ t('supplierRecon.subtitle') }}</p>
      </div>
    </header>

    <!-- 三个数字回答「今天还有多少活」。全是全局合计，不是当前页——
         页面上的合计如果跟着翻页变，那就不是合计了。 -->
    <section class="metrics">
      <div class="metric" :class="{ 'is-warn': pendingCount > 0 }">
        <span class="metric-label">{{ t('supplierRecon.pendingCount') }}</span>
        <strong class="metric-value">{{ pendingCount }}</strong>
        <span class="metric-hint">{{ t('supplierRecon.pendingHint') }}</span>
      </div>
      <div class="metric">
        <span class="metric-label">{{ t('supplierRecon.doneCount') }}</span>
        <strong class="metric-value">{{ doneCount }}</strong>
        <span class="metric-hint">{{ t('supplierRecon.doneHint') }}</span>
      </div>
      <div class="metric">
        <span class="metric-label">{{ t('supplierRecon.unpaidCount') }}</span>
        <strong class="metric-value">{{ unpaidCount }}</strong>
        <span class="metric-hint">{{ t('supplierRecon.unpaidHint') }}</span>
      </div>
    </section>

    <section class="panel">
      <div class="filters">
        <!-- 主页签：供应商对账下面的两个子页。状态放在地址的 query 里，
             前进/后退和分享链接都对；菜单高亮看的是 route.path，不受
             query 影响，仍然稳稳停在「供应商对账」上。 -->
        <el-radio-group :model-value="isDone ? 'done' : 'open'" class="tabs" @update:model-value="switchTab">
          <el-radio-button value="open">{{ t('supplierRecon.tabOpen') }}</el-radio-button>
          <el-radio-button value="done">{{ t('supplierRecon.tabDone') }}</el-radio-button>
        </el-radio-group>
        <el-input
          v-model="keyword"
          clearable
          :placeholder="t('supplierRecon.search')"
          style="max-width: 260px"
          @keyup.enter="reload"
          @clear="reload"
        />
        <el-button type="primary" @click="reload">{{ t('common.query') }}</el-button>
      </div>

      <el-table v-loading="loading" :data="rows" @expand-change="onExpand">
        <!-- 展开看这张采购单付过哪几笔。明细按需加载：每行都预先拉一次，
             一页就是 20 次往返。 -->
        <el-table-column type="expand">
          <template #default="{ row }">
            <div class="entries">
              <el-table v-if="entriesOf(row).length" :data="entriesOf(row)" size="small">
                <el-table-column :label="t('supplierRecon.entryDate')" width="120">
                  <template #default="{ row: e }">{{ e.paidAt || '—' }}</template>
                </el-table-column>
                <el-table-column :label="t('supplierRecon.entryAmount')" width="160" align="right">
                  <template #default="{ row: e }">
                    <span :class="{ negative: Number(e.amount) < 0 }">{{ e.currency }} {{ e.amount }}</span>
                    <div v-if="Number(e.feeAmount)" class="sub">
                      {{ t('supplierRecon.entryFee', { n: e.feeAmount }) }}
                    </div>
                  </template>
                </el-table-column>
                <el-table-column :label="t('supplierRecon.entrySource')" width="150">
                  <template #default="{ row: e }">
                    <span v-if="e.paymentNo">{{ e.paymentNo }}</span>
                    <span v-else class="sub">{{ t('supplierRecon.entryByHand') }}</span>
                  </template>
                </el-table-column>
                <el-table-column :label="t('supplierRecon.entryNote')" min-width="180">
                  <template #default="{ row: e }">
                    <span v-if="Number(e.reversalOf)">{{ t('supplierRecon.entryReversal') }} · {{ e.reverseReason }}</span>
                    <span v-else>{{ e.note || '—' }}</span>
                  </template>
                </el-table-column>
                <el-table-column :label="t('supplierRecon.entryBy')" width="160">
                  <template #default="{ row: e }">{{ e.allocatedBy }}<div class="sub">{{ e.allocatedAt }}</div></template>
                </el-table-column>
                <el-table-column width="90">
                  <template #default="{ row: e }">
                    <el-button
                      v-if="canWrite && !Number(e.reversalOf) && !reversedIds(row).has(String(e.allocationId))"
                      link type="danger" @click="reverseEntry(row, e)"
                    >{{ t('supplierRecon.entryReverse') }}</el-button>
                  </template>
                </el-table-column>
              </el-table>
              <p v-else class="sub">{{ t('supplierRecon.entriesEmpty') }}</p>
            </div>
          </template>
        </el-table-column>
        <el-table-column :label="t('supplierRecon.order')" min-width="190">
          <template #default="{ row }">
            <router-link :to="`/purchase-orders?keyword=${row.poNo}`" class="doc-link">{{ row.poNo }}</router-link>
            <div class="sub">{{ row.supplierName }}</div>
          </template>
        </el-table-column>
        <el-table-column :label="t('supplierRecon.openAmount')" width="170" align="right">
          <template #default="{ row }">
            <span class="num money" :class="{ over: Number(row.openAmount) < 0 }">
              {{ row.currency }} {{ row.openAmount }}
            </span>
            <div v-if="Number(row.openAmount) < 0" class="sub over">{{ t('supplierRecon.overpaid') }}</div>
            <div v-else class="sub">{{ t('supplierRecon.ofTotal', { total: row.orderedAmount }) }}</div>
          </template>
        </el-table-column>
        <el-table-column :label="t('supplierRecon.paid')" width="140" align="right">
          <template #default="{ row }">
            <span class="num" :class="{ dim: Number(row.paidAmount) === 0 }">{{ row.paidAmount }}</span>
          </template>
        </el-table-column>
        <el-table-column :label="t('supplierRecon.orderStatus')" width="130">
          <template #default="{ row }">
            <el-tag size="small" effect="plain" :type="row.orderStatus === 'CANCELLED' ? 'info' : undefined">
              {{ t(`supplierRecon.orderStatuses.${row.orderStatus}`) }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column :label="t('supplierRecon.orderedDate')" width="120">
          <template #default="{ row }">{{ row.orderedDate || '—' }}</template>
        </el-table-column>
        <el-table-column :label="t('supplierRecon.buyer')" min-width="110">
          <template #default="{ row }">{{ row.buyerName || '—' }}</template>
        </el-table-column>
        <!-- 已完成视图多一列：为什么算完了、谁说的。 -->
        <el-table-column v-if="isDone" :label="t('supplierRecon.closedWhy')" min-width="180">
          <template #default="{ row }">
            <el-tag size="small" effect="plain">{{ t(`supplierRecon.closureCategories.${row.closedCategory}`) }}</el-tag>
            <div class="sub">{{ row.closedByName }}<template v-if="row.closedNote"> · {{ row.closedNote }}</template></div>
          </template>
        </el-table-column>
        <el-table-column v-if="canWrite" :label="t('common.actions')" width="230" fixed="right">
          <template #default="{ row }">
            <!-- 已完成的单照样能记钱：钱真的又付了，先记上再撤销完成。 -->
            <el-button link type="primary" @click="openEntry(row)">
              {{ t('supplierRecon.addPayment') }}
            </el-button>
            <el-button v-if="isDone" link type="warning" @click="reopenRow(row)">
              {{ t('supplierRecon.reopen') }}
            </el-button>
            <el-button v-else link type="success" @click="openClose(row)">
              {{ t('supplierRecon.close') }}
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

    <!-- 确认核销完成。三个数并排亮着，员工看着差额自己拿主意——**系统不
         替他判断**：差 7000 也能确认完成，分毫不差也不会自动完成。 -->
    <el-dialog v-model="closeOpen" :title="t('supplierRecon.closeTitle')" width="min(520px, 94vw)" destroy-on-close>
      <template v-if="closing">
        <p class="close-target">{{ closing.poNo }} · {{ closing.supplierName }}</p>
        <div class="close-figures">
          <div><span class="sub">{{ t('supplierRecon.figOrdered') }}</span><span class="num">{{ closing.currency }} {{ closing.orderedAmount }}</span></div>
          <div><span class="sub">{{ t('supplierRecon.figPaid') }}</span><span class="num">{{ closing.paidAmount }}</span></div>
          <div><span class="sub">{{ t('supplierRecon.figOpen') }}</span><span class="num warn">{{ closing.openAmount }}</span></div>
        </div>
        <el-form label-position="top">
          <el-form-item :label="t('supplierRecon.closeWhat')">
            <el-radio-group v-model="closeForm.category">
              <el-radio v-for="k in CLOSURE_CATEGORIES" :key="k" :value="k">
                {{ t(`supplierRecon.closureCategories.${k}`) }}
              </el-radio>
            </el-radio-group>
          </el-form-item>
          <el-form-item :label="t('supplierRecon.closeNote')">
            <el-input v-model="closeForm.note" :placeholder="t('supplierRecon.closeNoteHint')" />
          </el-form-item>
        </el-form>
        <el-alert type="info" :closable="false" show-icon :title="t('supplierRecon.closeHint')" />
      </template>
      <template #footer>
        <el-button @click="closeOpen = false">{{ t('common.cancel') }}</el-button>
        <el-button type="primary" :loading="closingBusy" @click="submitClose">{{ t('supplierRecon.closeConfirm') }}</el-button>
      </template>
    </el-dialog>

    <!-- 记一笔付款：选中的采购单已经定了，员工只填「多少钱、哪天付的」。
         币种跟采购单走，不问——只有一个正确答案的问题只会制造错答案。 -->
    <el-dialog v-model="entryOpen" :title="t('supplierRecon.addPaymentTitle')" width="min(520px, 94vw)" destroy-on-close>
      <template v-if="entryRow">
        <p class="close-target">{{ entryRow.poNo }} · {{ entryRow.supplierName }}</p>
        <div class="close-figures">
          <div><span class="sub">{{ t('supplierRecon.figOrdered') }}</span><span class="num">{{ entryRow.currency }} {{ entryRow.orderedAmount }}</span></div>
          <div><span class="sub">{{ t('supplierRecon.figPaid') }}</span><span class="num">{{ entryRow.paidAmount }}</span></div>
          <div><span class="sub">{{ t('supplierRecon.figOpen') }}</span><span class="num warn">{{ entryRow.openAmount }}</span></div>
        </div>
        <el-form label-position="top">
          <el-form-item :label="t('supplierRecon.entryKind')">
            <el-radio-group v-model="entryForm.isRefund">
              <el-radio-button :value="false">{{ t('supplierRecon.kindPayment') }}</el-radio-button>
              <el-radio-button :value="true">{{ t('supplierRecon.kindRefund') }}</el-radio-button>
            </el-radio-group>
          </el-form-item>
          <el-form-item :label="t('supplierRecon.entryAmount')">
            <el-input v-model="entryForm.amount" :placeholder="t('supplierRecon.entryAmountHint')">
              <template #prepend>{{ entryRow.currency }}</template>
            </el-input>
          </el-form-item>
          <el-form-item :label="t('supplierRecon.entryDate')">
            <el-date-picker v-model="entryForm.paidAt" type="date" value-format="YYYY-MM-DD" style="width: 100%" />
          </el-form-item>
          <el-form-item :label="t('supplierRecon.entryNote')">
            <el-input v-model="entryForm.note" :placeholder="t('supplierRecon.entryNoteHint')" />
          </el-form-item>
        </el-form>
        <el-alert type="info" :closable="false" show-icon :title="t('supplierRecon.entryHint')" />
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
import { useRoute, useRouter } from 'vue-router'
import { ElMessage, ElMessageBox } from 'element-plus'
import { get, post } from '../api'
import { newIdempotencySession, withIdempotency } from '../lib/idempotency'
import { useAuthStore } from '../stores/auth'

const { t } = useI18n()
const route = useRoute()
const router = useRouter()
const auth = useAuthStore()
// 和网关那行 s.perm("procurement:recon:write") 逐字一致。差一个字就是
// 「看得见按钮、点下去 403」，或者更糟，反过来。
const canWrite = auth.can('procurement:recon:write')

interface Row {
  poId: string
  poNo: string
  supplierId: string
  supplierName: string
  currency: string
  orderStatus: string
  buyerName: string
  orderedDate: string
  expectedDate: string
  orderedAmount: string
  paidAmount: string
  openAmount: string
  closedCategory: string
  closedNote: string
  closedByName: string
  closedAt: string
}

interface Entry {
  allocationId: string
  paidAt: string
  amount: string
  // 只有改造前从付款单分配来的老行才可能非零。行上的「已付」不含它，
  // 所以这里单独标一行说明，否则明细加起来会比行上的已付少，页面不解释
  // 差在哪。手填行恒为 0。
  feeAmount: string
  currency: string
  note: string
  allocatedAt: string
  allocatedBy: string
  reversalOf: string
  reverseReason: string
  // 老行带着付款单号；手填行为空。
  paymentNo: string
}

const rows = ref<Row[]>([])
const total = ref(0)
const page = ref(1)
const pageSize = 20
// 「已完成」是 ?view=done。用 query 而不是第二条路径：两个子页属于同一个
// 服务（供应商对账），菜单上只该有一项，而 route.path 不变高亮才不会掉。
const isDone = computed(() => route.query.view === 'done')

function switchTab(v: string | number | boolean | undefined) {
  const q: Record<string, string> = {}
  if (keyword.value) q.keyword = keyword.value
  if (v === 'done') q.view = 'done'
  void router.push({ path: route.path, query: q })
}

const emptyText = computed(() =>
  isDone.value ? t('supplierRecon.emptyDone') : t('supplierRecon.empty'),
)
const keyword = ref(String(route.query.keyword ?? ''))
const loading = ref(false)

const pendingCount = ref(0)
const doneCount = ref(0)
const unpaidCount = ref(0)

async function fetchPage(params: Record<string, string | number>) {
  return get<{ items: Row[]; total: string }>('/supplier-recon', params)
}

async function load() {
  loading.value = true
  try {
    const d = await fetchPage({
      page: page.value, page_size: pageSize,
      view: isDone.value ? 'done' : '',
      keyword: keyword.value,
    })
    rows.value = d.items ?? []
    total.value = Number(d.total ?? 0)
  } finally {
    loading.value = false
  }
}

async function loadMetrics() {
  const [pending, done, sample] = await Promise.all([
    fetchPage({ page: 1, page_size: 1 }),
    fetchPage({ page: 1, page_size: 1, view: 'done' }),
    // 「一分钱都还没付」没有独立筛子——服务端只认「有没有人确认完成」
    // 这一个开关，别的都是页面自己看着数字说话。拉一页样本算，够用，
    // 且不必为一个提示数字再开一个接口。
    fetchPage({ page: 1, page_size: 200 }),
  ])
  pendingCount.value = Number(pending.total ?? 0)
  doneCount.value = Number(done.total ?? 0)
  unpaidCount.value = (sample.items ?? []).filter((r) => Number(r.paidAmount) === 0).length
}

// ── 记一笔付款 ────────────────────────────────────────────
//
// 选中的采购单已经定了，员工只填「多少钱、哪天付的」。**不连银行流水、
// 也不看发票**：流水那本账只用来存银行给的对账单，发票只是凭证。
const entryOpen = ref(false)
const entryBusy = ref(false)
const entryRow = ref<Row | null>(null)
const entryForm = ref({ amount: '', isRefund: false, paidAt: '', note: '' })
// 付款重复记一笔就是真金白银记两遍——网络抖一下重发，账上多一笔。
const payIdem = newIdempotencySession()
// 展开行的明细，按采购单缓存——展开一次拉一次，不预先拉。
const entries = ref<Record<string, Entry[]>>({})

function entriesOf(row: Row): Entry[] {
  return entries.value[String(row.poId)] ?? []
}

// 冲销记录点名它冲的是谁，被点名的那条就不该再有冲销按钮。
function reversedIds(row: Row): Set<string> {
  return new Set(entriesOf(row).filter((e) => Number(e.reversalOf)).map((e) => String(e.reversalOf)))
}

async function loadEntries(row: Row) {
  const d = await get<{ items: Entry[] }>(`/supplier-recon/${row.poId}/payments`)
  entries.value = { ...entries.value, [String(row.poId)]: d.items ?? [] }
}

function onExpand(row: Row, expanded: Row[]) {
  if (expanded.includes(row)) void loadEntries(row)
}

function openEntry(row: Row) {
  entryRow.value = row
  entryForm.value = { amount: '', isRefund: false, paidAt: '', note: '' }
  entryOpen.value = true
}

async function submitEntry() {
  if (!entryRow.value) return
  if (!entryForm.value.amount.trim()) {
    ElMessage.warning(t('supplierRecon.entryAmountRequired'))
    return
  }
  entryBusy.value = true
  try {
    await post(`/supplier-recon/${entryRow.value.poId}/payments`, {
      amount: entryForm.value.amount,
      isRefund: entryForm.value.isRefund,
      paidAt: entryForm.value.paidAt,
      note: entryForm.value.note,
    }, withIdempotency(payIdem))
    payIdem.reset()
    entryOpen.value = false
    ElMessage.success(t('supplierRecon.entrySaved'))
    await loadEntries(entryRow.value)
    reload()
  } catch { /* surfaced by the api layer */ } finally {
    entryBusy.value = false
  }
}

async function reverseEntry(row: Row, e: Entry) {
  // 冲销必须给理由，和撤销完成同一条纪律。
  const { value } = await ElMessageBox.prompt(
    t('supplierRecon.entryReverseWhy'), t('supplierRecon.entryReverse'),
    { inputPlaceholder: t('supplierRecon.entryReverseReason') },
  ).catch(() => ({ value: '' }))
  if (!value) return
  await post(`/supplier-recon/payments/${e.allocationId}/reverse`, { reason: value })
  ElMessage.success(t('supplierRecon.entryReversed'))
  await loadEntries(row)
  reload()
}

// ── 确认核销完成 ──────────────────────────────────────────
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
    await post(`/supplier-recon/${closing.value.poId}/close`, {
      category: closeForm.value.category, note: closeForm.value.note,
    })
    closeOpen.value = false
    ElMessage.success(t('supplierRecon.closed'))
    reload()
  } finally {
    closingBusy.value = false
  }
}

async function reopenRow(row: Row) {
  const { value } = await ElMessageBox.prompt(
    t('supplierRecon.reopenWhy', { no: row.poNo }), t('supplierRecon.reopen'),
    { inputPlaceholder: t('supplierRecon.reopenReason') },
  ).catch(() => ({ value: '' }))
  if (!value) return
  await post(`/supplier-recon/${row.poId}/reopen`, { reason: value })
  ElMessage.success(t('supplierRecon.reopened'))
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

// 待核销 ⇄ 已完成是**同一个组件实例**——vue-router 原地复用，setup 和
// onMounted 都不会再跑一遍。不盯着它重新拉数据的话，切过去表头和按钮都
// 变了（isDone 是响应式的），表格里却还是上一页那批数据：对着一张从没
// 确认过的单点「撤销完成」。ReceivableDuePage 和 SourcingCasesListPage
// 都踩过同一个坑。
watch(isDone, () => {
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
.sub.over,
.num.over {
  color: var(--el-color-warning);
}
.negative {
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
