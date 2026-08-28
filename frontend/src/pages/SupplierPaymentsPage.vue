<template>
  <div class="page">
    <header class="page-head">
      <div>
        <div class="eyebrow">{{ t('supplierPayments.eyebrow') }}</div>
        <h1>{{ t('supplierPayments.title') }}</h1>
        <p>{{ t('supplierPayments.subtitle') }}</p>
      </div>
      <div class="head-actions">
        <el-button v-if="canWrite" type="primary" @click="openCreate">{{ t('supplierPayments.create') }}</el-button>
      </div>
    </header>

    <section class="panel">
      <div class="filters">
        <el-input v-model="keyword" clearable :placeholder="t('supplierPayments.search')" style="max-width: 260px" @keyup.enter="reload" />
        <el-select v-model="ptype" clearable :placeholder="t('supplierPayments.typeAll')" style="width: 150px" @change="reload">
          <el-option v-for="k in ['ADVANCE', 'SETTLEMENT', 'REFUND']" :key="k" :value="k" :label="t(`supplierPayments.types.${k}`)" />
        </el-select>
        <el-button type="primary" @click="reload">{{ t('common.query') }}</el-button>
      </div>

      <el-table v-loading="loading" :data="rows" stripe>
        <el-table-column :label="t('supplierPayments.paymentNo')" width="185" show-overflow-tooltip>
          <template #default="{ row }">
            {{ row.paymentNo }}
            <el-tooltip v-if="row.source === 'BANK'" :content="t('supplierPayments.sourceBankHint')">
              <el-tag size="small" type="warning" effect="plain">{{ t('supplierPayments.sourceBank') }}</el-tag>
            </el-tooltip>
          </template>
        </el-table-column>
        <el-table-column prop="supplierName" :label="t('supplierPayments.supplier')" min-width="160" show-overflow-tooltip />
        <el-table-column :label="t('supplierPayments.type')" width="100">
          <template #default="{ row }">
            <el-tag effect="plain" :type="row.paymentType === 'ADVANCE' ? 'warning' : 'info'">
              {{ t(`supplierPayments.types.${row.paymentType}`) }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column :label="t('supplierPayments.amount')" width="150" align="right">
          <template #default="{ row }">{{ row.currency }} {{ row.amount }}</template>
        </el-table-column>
        <el-table-column :label="t('supplierPayments.unallocated')" width="140" align="right">
          <template #default="{ row }">
            <span :class="{ pending: Number(row.unallocated) > 0 }">{{ row.unallocated }}</span>
          </template>
        </el-table-column>
        <el-table-column prop="paidAt" :label="t('supplierPayments.paidAt')" width="115" />
        <el-table-column :label="t('supplierPayments.method')" width="95">
          <template #default="{ row }">{{ t(`supplierPayments.methods.${row.method}`) }}</template>
        </el-table-column>
        <el-table-column :label="t('common.actions')" width="170" fixed="right">
          <template #default="{ row }">
            <el-button size="small" @click="openDetail(row.id)">{{ t('common.detail') }}</el-button>
            <el-button v-if="canWrite && Number(row.unallocated) > 0" size="small" type="primary" link @click="openAllocate(row.id)">{{ t('supplierPayments.allocate') }}</el-button>
          </template>
        </el-table-column>
        <template #empty>{{ t('supplierPayments.empty') }}</template>
      </el-table>
      <el-pagination v-model:current-page="page" :page-size="20" :total="total" layout="total, prev, pager, next" @current-change="load" />
    </section>

    <!-- The assertion: money left. Allocation is deliberately elsewhere. -->
    <el-dialog v-model="createOpen" :title="t('supplierPayments.createTitle')" width="620px" destroy-on-close>
      <el-form label-width="110px">
        <el-form-item :label="t('supplierPayments.supplier')" required>
          <el-select v-model="form.supplierId" filterable :placeholder="t('supplierPayments.supplierPlaceholder')" @change="onSupplierChange">
            <el-option v-for="s in suppliers" :key="s.id" :value="String(s.id)" :label="`${s.name}（${s.code}）`" />
          </el-select>
        </el-form-item>
        <el-form-item :label="t('supplierPayments.type')">
          <el-radio-group v-model="form.paymentType">
            <el-radio-button v-for="k in ['SETTLEMENT', 'ADVANCE', 'REFUND']" :key="k" :value="k">{{ t(`supplierPayments.types.${k}`) }}</el-radio-button>
          </el-radio-group>
          <div class="var-hint">{{ form.paymentType === 'ADVANCE' ? t('supplierPayments.advanceHint') : '' }}</div>
        </el-form-item>
        <el-form-item :label="t('supplierPayments.currency')" required><el-input v-model="form.currency" style="width: 160px" /></el-form-item>
        <el-form-item :label="t('supplierPayments.amount')" required><el-input v-model="form.amount" style="width: 220px" /></el-form-item>
        <el-form-item :label="t('supplierPayments.paidAt')" required>
          <el-date-picker v-model="form.paidAt" type="date" value-format="YYYY-MM-DD" />
        </el-form-item>
        <el-form-item :label="t('supplierPayments.method')">
          <el-select v-model="form.method" style="width: 160px">
            <el-option v-for="k in ['WIRE', 'LC', 'TT', 'CASH', 'OTHER']" :key="k" :value="k" :label="t(`supplierPayments.methods.${k}`)" />
          </el-select>
        </el-form-item>
        <el-form-item :label="t('supplierPayments.bankRef')"><el-input v-model="form.bankRef" :placeholder="t('supplierPayments.bankRefHint')" /></el-form-item>
        <el-form-item :label="t('supplierPayments.remark')"><el-input v-model="form.remark" type="textarea" :rows="2" /></el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="createOpen = false">{{ t('common.cancel') }}</el-button>
        <el-button type="primary" :loading="saving" @click="save">{{ t('common.save') }}</el-button>
      </template>
    </el-dialog>

    <!-- Detail: the assertion plus every judgement, reversals included. -->
    <el-dialog v-model="detailOpen" :title="detail?.paymentNo || ''" width="min(980px, 94vw)" destroy-on-close>
      <el-descriptions v-if="detail" :column="3" border size="small">
        <el-descriptions-item :label="t('supplierPayments.supplier')">{{ detail.supplierName }}</el-descriptions-item>
        <el-descriptions-item :label="t('supplierPayments.type')">{{ t(`supplierPayments.types.${detail.paymentType}`) }}</el-descriptions-item>
        <el-descriptions-item :label="t('supplierPayments.amount')">{{ detail.currency }} {{ detail.amount }}</el-descriptions-item>
        <el-descriptions-item :label="t('supplierPayments.unallocated')">{{ detail.unallocated }}</el-descriptions-item>
        <el-descriptions-item :label="t('supplierPayments.paidAt')">{{ detail.paidAt }}</el-descriptions-item>
        <el-descriptions-item :label="t('supplierPayments.method')">{{ t(`supplierPayments.methods.${detail.method}`) }}</el-descriptions-item>
        <el-descriptions-item v-if="detail.bankRef" :label="t('supplierPayments.bankRef')" :span="2">{{ detail.bankRef }}</el-descriptions-item>
        <el-descriptions-item v-if="detail.remark" :label="t('supplierPayments.remark')" :span="3">{{ detail.remark }}</el-descriptions-item>
      </el-descriptions>
      <el-table v-if="detail?.allocations?.length" :data="detail.allocations" size="small" stripe style="margin-top: 12px">
        <el-table-column :label="t('supplierPayments.allocTarget')" min-width="160">
          <template #default="{ row }">
            {{ Number(row.invoiceId) ? `${t('supplierPayments.targetInvoice')} ${row.invoiceNo}` : `${t('supplierPayments.targetPO')} ${row.poNo}` }}
          </template>
        </el-table-column>
        <el-table-column prop="amount" :label="t('supplierPayments.allocAmount')" width="120" align="right">
          <template #default="{ row }"><span :class="{ negative: Number(row.amount) < 0 }">{{ row.amount }}</span></template>
        </el-table-column>
        <el-table-column prop="feeAmount" :label="t('supplierPayments.fee')" width="100" align="right" />
        <el-table-column :label="t('supplierPayments.allocNote')" min-width="150">
          <template #default="{ row }">
            <span v-if="Number(row.reversalOf)">{{ t('supplierPayments.reversalOfRow') }} · {{ row.reverseReason }}</span>
            <span v-else>—</span>
          </template>
        </el-table-column>
        <el-table-column prop="allocatedBy" :label="t('supplierPayments.allocBy')" width="110" />
        <el-table-column :label="t('common.actions')" width="90">
          <template #default="{ row }">
            <el-button
              v-if="canWrite && !Number(row.reversalOf) && Number(row.amount) > 0 && !reversedIds.has(String(row.id))"
              size="small" type="danger" link @click="reverse(row)"
            >{{ t('supplierPayments.reverse') }}</el-button>
          </template>
        </el-table-column>
      </el-table>
      <template #footer>
        <el-button v-if="canWrite && detail && Number(detail.unallocated) > 0" type="primary" @click="openAllocate(detail.id)">{{ t('supplierPayments.allocate') }}</el-button>
      </template>
    </el-dialog>

    <!-- The judgement: what did this money settle. -->
    <el-dialog v-model="allocateOpen" :title="t('supplierPayments.allocateTitle')" width="min(880px, 94vw)" destroy-on-close>
      <el-alert :closable="false" type="info" show-icon
        :title="t('supplierPayments.allocRemaining', { n: detail?.unallocated || '0' })" />
      <el-table :data="allocLines" size="small" style="margin-top: 10px">
        <el-table-column :label="t('supplierPayments.allocTarget')" min-width="300">
          <template #default="{ row }">
            <el-select v-model="row.target" filterable style="width: 100%">
              <el-option-group :label="t('supplierPayments.targetInvoice')">
                <el-option v-for="i in openInvoices" :key="`i${i.id}`" :value="`i${i.id}`" :label="`${i.invoiceNo} · ${i.currency} ${i.totalAmount}`" />
              </el-option-group>
              <el-option-group :label="t('supplierPayments.targetPOAdvance')">
                <el-option v-for="o in supplierPOs" :key="`p${o.id}`" :value="`p${o.id}`" :label="`${o.poNo} · ${o.currency} ${o.totalAmount}`" />
              </el-option-group>
            </el-select>
          </template>
        </el-table-column>
        <el-table-column :label="t('supplierPayments.allocAmount')" width="150">
          <template #default="{ row }"><el-input v-model="row.amount" size="small" /></template>
        </el-table-column>
        <el-table-column :label="t('supplierPayments.fee')" width="120">
          <template #default="{ row }"><el-input v-model="row.fee" size="small" /></template>
        </el-table-column>
        <el-table-column width="60">
          <template #default="{ $index }"><el-button size="small" type="danger" link @click="allocLines.splice($index, 1)">✕</el-button></template>
        </el-table-column>
      </el-table>
      <el-button size="small" style="margin-top: 8px" @click="allocLines.push({ target: '', amount: '', fee: '' })">{{ t('supplierPayments.addLine') }}</el-button>
      <template #footer>
        <el-button @click="allocateOpen = false">{{ common('cancel') }}</el-button>
        <el-button type="primary" :loading="saving" @click="submitAllocation">{{ t('supplierPayments.allocate') }}</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { useI18n } from 'vue-i18n'
import { get, post } from '../api'
import { newIdempotencySession, withIdempotency } from '../lib/idempotency'
import { useAuthStore } from '../stores/auth'

const { t } = useI18n()
const common = (k: string) => t(`common.${k}`)
const auth = useAuthStore()
const canWrite = computed(() => auth.can('procurement:payment:write'))

interface AllocationRow {
  id: string
  invoiceId: string
  invoiceNo: string
  poId: string
  poNo: string
  amount: string
  feeAmount: string
  reversalOf: string
  reverseReason: string
  allocatedBy: string
}
interface PaymentRow {
  id: string
  supplierId: string
  supplierName: string
  paymentNo: string
  // MANUAL 手工登记 / BANK 付款对账直核自动建的影子单。
  source: string
  paymentType: string
  currency: string
  amount: string
  unallocated: string
  paidAt: string
  method: string
  bankRef: string
  remark: string
  allocations?: AllocationRow[]
}
interface SupplierOpt { id: string; code: string; name: string; currency: string }

const rows = ref<PaymentRow[]>([])
const loading = ref(false)
const page = ref(1)
const total = ref(0)
const keyword = ref('')
const ptype = ref('')

const createOpen = ref(false)
const saving = ref(false)
const suppliers = ref<SupplierOpt[]>([])
const detailOpen = ref(false)
const detail = ref<PaymentRow | null>(null)
const allocateOpen = ref(false)
const allocLines = ref<Array<{ target: string; amount: string; fee: string }>>([])
const openInvoices = ref<Array<{ id: string; invoiceNo: string; currency: string; totalAmount: string }>>([])
const supplierPOs = ref<Array<{ id: string; poNo: string; currency: string; totalAmount: string; supplierId: string }>>([])

// A reversal row names its victim; the victim's own reverse button must go.
const reversedIds = computed(() =>
  new Set((detail.value?.allocations ?? []).filter((a) => Number(a.reversalOf)).map((a) => String(a.reversalOf))))

const form = reactive({
  supplierId: '', paymentType: 'SETTLEMENT', currency: '', amount: '',
  paidAt: '', method: 'WIRE', bankRef: '', remark: '',
})

async function load() {
  loading.value = true
  try {
    const resp = await get<{ items: PaymentRow[]; total: string }>('/supplier-payments', {
      page: page.value, page_size: 20, keyword: keyword.value, payment_type: ptype.value,
    })
    rows.value = resp.items || []
    total.value = Number(resp.total || 0)
  } finally {
    loading.value = false
  }
}
function reload() { page.value = 1; void load() }

async function openCreate() {
  createOpen.value = true
  if (!suppliers.value.length) {
    const resp = await get<{ suppliers: SupplierOpt[] }>('/suppliers', { page_size: 200 })
    suppliers.value = resp.suppliers || []
  }
}

function onSupplierChange() {
  const s = suppliers.value.find((x) => String(x.id) === form.supplierId)
  if (s?.currency && !form.currency) form.currency = s.currency
}

// 防重键：付款单重复一笔就是真金白银记两遍。
const createIdem = newIdempotencySession()

async function save() {
  saving.value = true
  try {
    const s = suppliers.value.find((x) => String(x.id) === form.supplierId)
    await post('/supplier-payments', {
      supplierId: form.supplierId, supplierName: s?.name || '',
      paymentType: form.paymentType, currency: form.currency, amount: form.amount,
      paidAt: form.paidAt, method: form.method, bankRef: form.bankRef, remark: form.remark,
    }, withIdempotency(createIdem))
    createIdem.reset()
    ElMessage.success(t('supplierPayments.created'))
    createOpen.value = false
    Object.assign(form, { supplierId: '', paymentType: 'SETTLEMENT', currency: '', amount: '', paidAt: '', method: 'WIRE', bankRef: '', remark: '' })
    reload()
  } catch { /* surfaced by the api layer */ } finally {
    saving.value = false
  }
}

async function openDetail(id: string) {
  const resp = await get<{ payment: PaymentRow }>(`/supplier-payments/${id}`)
  detail.value = resp.payment
  detailOpen.value = true
}

async function openAllocate(id: string) {
  const resp = await get<{ payment: PaymentRow }>(`/supplier-payments/${id}`)
  detail.value = resp.payment
  allocLines.value = [{ target: '', amount: '', fee: '' }]
  const [inv, pos] = await Promise.all([
    get<{ items: Array<{ id: string; invoiceNo: string; currency: string; totalAmount: string }> }>(
      '/supplier-invoices', { supplier_id: detail.value.supplierId, status: 'OPEN', page_size: 100 }),
    get<{ orders: Array<{ id: string; poNo: string; currency: string; totalAmount: string; supplierId: string }> }>(
      '/purchase-orders', { page_size: 100 }),
  ])
  openInvoices.value = inv.items || []
  supplierPOs.value = (pos.orders || []).filter((o) => String(o.supplierId) === String(detail.value!.supplierId))
  allocateOpen.value = true
}

async function submitAllocation() {
  const lines = allocLines.value
    .filter((l) => l.target && l.amount)
    .map((l) => ({
      invoiceId: l.target.startsWith('i') ? l.target.slice(1) : '0',
      poId: l.target.startsWith('p') ? l.target.slice(1) : '0',
      amount: l.amount, feeAmount: l.fee || '0',
    }))
  if (!lines.length) return
  saving.value = true
  try {
    await post(`/supplier-payments/${detail.value!.id}/allocations`, { lines })
    ElMessage.success(t('supplierPayments.allocated'))
    allocateOpen.value = false
    void openDetail(detail.value!.id)
    void load()
  } catch { /* surfaced by the api layer */ } finally {
    saving.value = false
  }
}

async function reverse(row: AllocationRow) {
  const { value } = await ElMessageBox.prompt(
    t('supplierPayments.reversePrompt'), t('supplierPayments.reverse'),
    { inputPlaceholder: t('supplierPayments.reverseReason') }).catch(() => ({ value: '' }))
  if (!value) return
  await post(`/supplier-payments/allocations/${row.id}/reverse`, { reason: value })
  ElMessage.success(t('supplierPayments.reversed'))
  void openDetail(detail.value!.id)
  void load()
}

onMounted(load)
</script>

<style scoped>
.pending { color: var(--el-color-warning); font-weight: 600; }
.negative { color: var(--el-color-danger); }
</style>
