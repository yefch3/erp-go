<template>
  <div class="page">
    <header class="page-head">
      <div>
        <div class="eyebrow">{{ t('paymentRecon.eyebrow') }}</div>
        <h1>{{ t('paymentRecon.title') }}</h1>
        <p>{{ t('paymentRecon.subtitle') }}</p>
      </div>
    </header>

    <section class="panel">
      <div class="filters">
        <!-- 两条队列：核销付款看出账，供应商退款看进账。都只收归属=供应商的行；
             归属还空着的先去银行流水页认归属。 -->
        <el-radio-group v-model="mode" @change="onModeChange">
          <el-radio-button value="collect">{{ t('paymentRecon.modeCollect') }}</el-radio-button>
          <el-radio-button value="refund">{{ t('paymentRecon.modeRefund') }}</el-radio-button>
        </el-radio-group>
        <el-radio-group v-model="tab" @change="reload">
          <el-radio-button value="OPEN">{{ t('paymentRecon.tabOpen') }}</el-radio-button>
          <el-radio-button value="CLAIMED">{{ t('paymentRecon.tabClaimed') }}</el-radio-button>
          <el-radio-button value="">{{ t('paymentRecon.tabAll') }}</el-radio-button>
        </el-radio-group>
        <el-input v-model="keyword" clearable :placeholder="t('paymentRecon.search')" style="max-width: 240px" @keyup.enter="reload" />
        <el-button type="primary" @click="reload">{{ t('common.query') }}</el-button>
      </div>

      <el-table v-loading="loading" :data="rows" stripe>
        <el-table-column prop="txnDate" :label="t('paymentRecon.txnDate')" width="110" />
        <el-table-column :label="t('paymentRecon.amount')" width="160" align="right">
          <template #default="{ row }">{{ row.currency }} {{ row.amount }}</template>
        </el-table-column>
        <el-table-column prop="counterparty" :label="t('paymentRecon.counterparty')" min-width="150" show-overflow-tooltip />
        <el-table-column prop="bankRef" :label="t('paymentRecon.bankRef')" min-width="150" show-overflow-tooltip />
        <el-table-column prop="remark" :label="t('paymentRecon.remark')" min-width="140" show-overflow-tooltip />
        <el-table-column :label="t('paymentRecon.state')" width="170">
          <template #default="{ row }">
            <template v-if="isClaimed(row)">
              <el-tag v-if="row.matchedPaymentNo" type="success" effect="plain">{{ row.matchedPaymentNo }}</el-tag>
              <el-tag v-else type="success" effect="plain">{{ t('paymentRecon.claimed') }}</el-tag>
            </template>
            <el-tag v-else type="warning" effect="plain">{{ t('paymentRecon.open') }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column :label="t('common.actions')" width="120" fixed="right">
          <template #default="{ row }">
            <el-button v-if="canWrite && !isClaimed(row)" size="small" type="primary" @click="openSettle(row)">
              {{ mode === 'refund' ? t('paymentRecon.settleRefund') : t('paymentRecon.settle') }}
            </el-button>
            <span v-else-if="isClaimed(row)" class="sub">{{ t('paymentRecon.undoHint') }}</span>
          </template>
        </el-table-column>
        <template #empty>{{ t('paymentRecon.empty') }}</template>
      </el-table>
      <el-pagination v-model:current-page="page" :page-size="20" :total="total" layout="total, prev, pager, next" @current-change="load" />
    </section>

    <!-- 直核：说清这笔钱结算/退了哪些发票或采购单。确认后系统在同一个事务里
         自动建影子付款单（列表里带「银行直核」标签），核销行挂在影子单上。 -->
    <el-dialog v-model="settleOpen" :title="mode === 'refund' ? t('paymentRecon.settleRefundTitle') : t('paymentRecon.settleTitle')" width="min(880px, 94vw)" destroy-on-close>
      <p v-if="settleRow" class="pick-context">
        {{ settleRow.txnDate }} · {{ settleRow.currency }} {{ settleRow.amount }} ·
        {{ settleRow.counterparty || settleRow.bankRef }}
      </p>
      <el-alert :closable="false" type="info" show-icon
        :title="mode === 'refund' ? t('paymentRecon.refundHint') : t('paymentRecon.collectHint')" />
      <el-form label-width="90px" style="margin-top: 10px">
        <el-form-item :label="t('paymentRecon.supplier')" required>
          <el-select v-model="supplierId" filterable :placeholder="t('paymentRecon.supplierPlaceholder')" style="width: 320px" @change="loadTargets">
            <el-option v-for="s in suppliers" :key="s.id" :value="String(s.id)" :label="`${s.name}（${s.code}）`" />
          </el-select>
        </el-form-item>
      </el-form>
      <el-table :data="lines" size="small">
        <el-table-column :label="t('paymentRecon.target')" min-width="300">
          <template #default="{ row }">
            <el-select v-model="row.target" filterable :disabled="!supplierId" style="width: 100%">
              <el-option-group :label="t('paymentRecon.targetInvoice')">
                <el-option v-for="i in invoices" :key="`i${i.id}`" :value="`i${i.id}`" :label="`${i.invoiceNo} · ${i.currency} ${i.totalAmount}`" />
              </el-option-group>
              <el-option-group :label="t('paymentRecon.targetPO')">
                <el-option v-for="o in pos" :key="`p${o.id}`" :value="`p${o.id}`" :label="`${o.poNo} · ${o.currency} ${o.totalAmount}`" />
              </el-option-group>
            </el-select>
          </template>
        </el-table-column>
        <el-table-column :label="t('paymentRecon.lineAmount')" width="150">
          <template #default="{ row }"><el-input v-model="row.amount" size="small" /></template>
        </el-table-column>
        <el-table-column v-if="mode === 'collect'" :label="t('paymentRecon.fee')" width="120">
          <template #default="{ row }"><el-input v-model="row.fee" size="small" /></template>
        </el-table-column>
        <el-table-column width="60">
          <template #default="{ $index }"><el-button size="small" type="danger" link @click="lines.splice($index, 1)">✕</el-button></template>
        </el-table-column>
      </el-table>
      <el-button size="small" style="margin-top: 8px" @click="lines.push({ target: '', amount: '', fee: '' })">{{ t('paymentRecon.addLine') }}</el-button>
      <p class="sum-hint" :class="{ mismatch: sumMismatch }">
        {{ t('paymentRecon.sumHint', { sum: lineSum, amount: settleRow?.amount || '0' }) }}
      </p>
      <template #footer>
        <el-button @click="settleOpen = false">{{ t('common.cancel') }}</el-button>
        <el-button type="primary" :loading="saving" @click="submitSettle">
          {{ mode === 'refund' ? t('paymentRecon.settleRefund') : t('paymentRecon.settle') }}
        </el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { ElMessage } from 'element-plus'
import { useI18n } from 'vue-i18n'
import { get, post } from '../api'
import { useAuthStore } from '../stores/auth'

const { t } = useI18n()
const auth = useAuthStore()
const canWrite = computed(() => auth.can('procurement:payment:write'))

interface TxnRow {
  id: string
  txnDate: string
  direction: string
  amount: string
  currency: string
  counterparty: string
  bankRef: string
  remark: string
  claimedAmount: string
  matchedPaymentNo: string
  ownership: string
}
interface SupplierOpt { id: string; code: string; name: string }
interface InvoiceOpt { id: string; invoiceNo: string; currency: string; totalAmount: string; status: string }
interface POOpt { id: string; poNo: string; currency: string; totalAmount: string; supplierId: string }

// collect = 核销付款（出账），refund = 供应商退款（进账）。
const mode = ref<'collect' | 'refund'>('collect')
const tab = ref<'OPEN' | 'CLAIMED' | ''>('OPEN')
const keyword = ref('')
const rows = ref<TxnRow[]>([])
const loading = ref(false)
const page = ref(1)
const total = ref(0)

const settleOpen = ref(false)
const settleRow = ref<TxnRow | null>(null)
const saving = ref(false)
const suppliers = ref<SupplierOpt[]>([])
const supplierId = ref('')
const invoices = ref<InvoiceOpt[]>([])
const pos = ref<POOpt[]>([])
const lines = ref<Array<{ target: string; amount: string; fee: string }>>([])

// claimed_amount 是字符串金额；这里的数都是钱的数量级，Number 够用
// （大整数 id 才有精度陷阱，见 lib/protoId.ts）。
function isClaimed(row: TxnRow) {
  return Number(row.claimedAmount) > 0
}

const lineSum = computed(() => lines.value
  .reduce((acc, l) => acc + (Number(l.amount) || 0) + (mode.value === 'collect' ? Number(l.fee) || 0 : 0), 0)
  .toFixed(2))
const sumMismatch = computed(() => Number(lineSum.value) !== Number(settleRow.value?.amount || 0))

async function load() {
  loading.value = true
  try {
    const data = await get<{ items: TxnRow[]; total: string }>('/bank-transactions', {
      direction: mode.value === 'refund' ? 'CREDIT' : 'DEBIT',
      ownership: 'SUPPLIER',
      claim_status: tab.value,
      keyword: keyword.value,
      page: page.value, page_size: 20,
    })
    rows.value = data.items || []
    total.value = Number(data.total || 0)
  } finally { loading.value = false }
}
function reload() { page.value = 1; void load() }
function onModeChange() { tab.value = 'OPEN'; reload() }

async function openSettle(row: TxnRow) {
  settleRow.value = row
  supplierId.value = ''
  invoices.value = []
  pos.value = []
  lines.value = [{ target: '', amount: row.amount, fee: '' }]
  settleOpen.value = true
  if (!suppliers.value.length) {
    const resp = await get<{ suppliers: SupplierOpt[] }>('/suppliers', { page_size: 200 })
    suppliers.value = resp.suppliers || []
  }
}

async function loadTargets() {
  if (!supplierId.value) return
  const [inv, orders] = await Promise.all([
    // 退款要能退到已结清的发票上（付清后厂里退一部分正是常态），所以退款
    // 模式不筛 OPEN，只把作废的挡掉；核销模式照旧只出还没结清的。
    get<{ items: InvoiceOpt[] }>('/supplier-invoices', {
      supplier_id: supplierId.value,
      status: mode.value === 'refund' ? '' : 'OPEN',
      page_size: 100,
    }),
    get<{ orders: POOpt[] }>('/purchase-orders', { page_size: 100 }),
  ])
  invoices.value = (inv.items || []).filter((i) => i.status !== 'VOID')
  pos.value = (orders.orders || []).filter((o) => String(o.supplierId) === String(supplierId.value))
}

async function submitSettle() {
  const payload = lines.value
    .filter((l) => l.target && l.amount)
    .map((l) => ({
      invoiceId: l.target.startsWith('i') ? l.target.slice(1) : '0',
      poId: l.target.startsWith('p') ? l.target.slice(1) : '0',
      amount: l.amount,
      feeAmount: mode.value === 'collect' ? l.fee || '0' : '0',
    }))
  if (!payload.length) { ElMessage.warning(t('paymentRecon.linesRequired')); return }
  saving.value = true
  try {
    const resp = await post<{ payment: { paymentNo: string } }>(
      `/bank-transactions/${settleRow.value!.id}/settle-supplier`, { lines: payload })
    ElMessage.success(t('paymentRecon.settled', { no: resp.payment?.paymentNo || '' }))
    settleOpen.value = false
    void load()
  } catch { /* surfaced by the api layer */ } finally {
    saving.value = false
  }
}

onMounted(load)
</script>

<style scoped>
.filters { display: flex; gap: 12px; flex-wrap: wrap; align-items: center; margin-bottom: 12px; }
.pick-context { margin: 0 0 10px; color: var(--el-text-color-secondary); }
.sub { color: var(--el-text-color-secondary); font-size: 12px; }
.sum-hint { margin: 10px 0 0; font-size: 13px; color: var(--el-text-color-secondary); }
.sum-hint.mismatch { color: var(--el-color-warning); }
</style>
