<template>
  <div class="page">
    <header class="page-head">
      <div>
        <div class="eyebrow">{{ t('supplierInvoices.eyebrow') }}</div>
        <h1>{{ t('supplierInvoices.title') }}</h1>
        <p>{{ t('supplierInvoices.subtitle') }}</p>
      </div>
      <div class="head-actions">
        <el-button v-if="canWrite" type="primary" @click="openCreate">{{ t('supplierInvoices.create') }}</el-button>
      </div>
    </header>

    <section class="panel">
      <div class="filters">
        <el-input v-model="keyword" clearable :placeholder="t('supplierInvoices.search')" style="max-width: 260px" @keyup.enter="reload" />
        <el-select v-model="status" clearable :placeholder="t('supplierInvoices.statusAll')" style="width: 150px" @change="reload">
          <el-option v-for="s in ['OPEN', 'SETTLED', 'VOID']" :key="s" :value="s" :label="t(`supplierInvoices.statuses.${s}`)" />
        </el-select>
        <el-button type="primary" @click="reload">{{ t('common.query') }}</el-button>
      </div>

      <el-table v-loading="loading" :data="rows" stripe>
        <el-table-column prop="invoiceNo" :label="t('supplierInvoices.invoiceNo')" width="170" show-overflow-tooltip />
        <el-table-column prop="supplierName" :label="t('supplierInvoices.supplier')" min-width="170" show-overflow-tooltip />
        <el-table-column :label="t('supplierInvoices.type')" width="120">
          <template #default="{ row }">{{ t(`supplierInvoices.types.${row.invoiceType}`) }}</template>
        </el-table-column>
        <el-table-column :label="t('supplierInvoices.amount')" width="150" align="right">
          <template #default="{ row }">{{ row.currency }} {{ row.totalAmount }}</template>
        </el-table-column>
        <el-table-column prop="invoiceDate" :label="t('supplierInvoices.invoiceDate')" width="115" />
        <el-table-column :label="t('supplierInvoices.dueDate')" width="115">
          <template #default="{ row }"><span :class="{ overdue: isOverdue(row) }">{{ row.dueDate || '—' }}</span></template>
        </el-table-column>
        <el-table-column :label="t('supplierInvoices.status')" width="105">
          <template #default="{ row }">
            <el-tag effect="plain" :type="row.status === 'OPEN' ? 'warning' : row.status === 'SETTLED' ? 'success' : 'info'">
              {{ t(`supplierInvoices.statuses.${row.status}`) }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column :label="t('supplierInvoices.matchStatus')" width="110">
          <template #default="{ row }">
            <el-tag effect="plain" :type="row.matchStatus === 'MATCHED' ? 'success' : row.matchStatus === 'EXCEPTION' ? 'danger' : 'info'">
              {{ t(`supplierInvoices.matchStatuses.${row.matchStatus}`) }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column :label="t('common.actions')" width="150" fixed="right">
          <template #default="{ row }">
            <el-button size="small" @click="openDetail(row)">{{ t('common.detail') }}</el-button>
            <el-button v-if="canWrite && row.status === 'OPEN'" size="small" type="danger" link @click="voidInvoice(row)">{{ t('supplierInvoices.void') }}</el-button>
          </template>
        </el-table-column>
        <template #empty>{{ t('supplierInvoices.empty') }}</template>
      </el-table>
      <el-pagination v-model:current-page="page" :page-size="20" :total="total" layout="total, prev, pager, next" @current-change="load" />
    </section>

    <!-- Entry: the paper as the factory wrote it. -->
    <el-dialog v-model="createOpen" :title="t('supplierInvoices.createTitle')" width="min(1080px, 94vw)" destroy-on-close>
      <el-form label-width="110px">
        <div class="grid2">
          <el-form-item :label="t('supplierInvoices.supplier')" required>
            <el-select v-model="form.supplierId" filterable :placeholder="t('supplierInvoices.supplierPlaceholder')" @change="onSupplierChange">
              <el-option v-for="s in suppliers" :key="s.id" :value="String(s.id)" :label="`${s.name}（${s.code}）`" />
            </el-select>
          </el-form-item>
          <el-form-item :label="t('supplierInvoices.invoiceNo')" required>
            <el-input v-model="form.invoiceNo" :placeholder="t('supplierInvoices.invoiceNoPlaceholder')" />
          </el-form-item>
          <el-form-item :label="t('supplierInvoices.type')">
            <el-select v-model="form.invoiceType">
              <el-option v-for="ty in ['COMMERCIAL', 'PROFORMA', 'VAT_SPECIAL', 'VAT_PLAIN', 'OTHER']" :key="ty" :value="ty" :label="t(`supplierInvoices.types.${ty}`)" />
            </el-select>
          </el-form-item>
          <el-form-item :label="t('supplierInvoices.currency')" required><el-input v-model="form.currency" /></el-form-item>
          <el-form-item :label="t('supplierInvoices.totalAmount')" required><el-input v-model="form.totalAmount" /></el-form-item>
          <el-form-item :label="t('supplierInvoices.taxAmount')"><el-input v-model="form.taxAmount" /></el-form-item>
          <el-form-item :label="t('supplierInvoices.invoiceDate')" required>
            <el-date-picker v-model="form.invoiceDate" type="date" value-format="YYYY-MM-DD" style="width: 100%" />
          </el-form-item>
          <el-form-item :label="t('supplierInvoices.dueDate')">
            <el-date-picker v-model="form.dueDate" type="date" value-format="YYYY-MM-DD" style="width: 100%" />
          </el-form-item>
        </div>

        <el-form-item :label="t('supplierInvoices.lines')">
          <div class="lines-tools">
            <el-select v-model="pickedPO" filterable :disabled="!form.supplierId" :loading="poLoading" :placeholder="t('supplierInvoices.pickPO')" style="width: 320px">
              <el-option v-for="o in supplierPOs" :key="o.id" :value="String(o.id)" :label="`${o.poNo} · ${o.currency} ${o.totalAmount}`" />
            </el-select>
            <el-button :disabled="!pickedPO" @click="pullFromPO">{{ t('supplierInvoices.pullFromPO') }}</el-button>
            <el-button @click="addLine">{{ t('supplierInvoices.addLine') }}</el-button>
          </div>
          <el-table :data="form.lines" size="small" class="lines-table">
            <el-table-column :label="t('supplierInvoices.linePO')" width="150">
              <template #default="{ row }">{{ row.poNo || '—' }}</template>
            </el-table-column>
            <el-table-column :label="t('supplierInvoices.lineDesc')" min-width="180">
              <template #default="{ row }"><el-input v-model="row.description" size="small" /></template>
            </el-table-column>
            <el-table-column :label="t('supplierInvoices.lineQty')" width="120">
              <template #default="{ row }"><el-input v-model="row.qty" size="small" @input="recalc(row)" /></template>
            </el-table-column>
            <el-table-column :label="t('supplierInvoices.linePrice')" width="130">
              <template #default="{ row }"><el-input v-model="row.unitPrice" size="small" @input="recalc(row)" /></template>
            </el-table-column>
            <el-table-column :label="t('supplierInvoices.lineAmount')" width="140">
              <template #default="{ row }"><el-input v-model="row.amount" size="small" /></template>
            </el-table-column>
            <el-table-column width="60">
              <template #default="{ $index }"><el-button size="small" type="danger" link @click="form.lines.splice($index, 1)">✕</el-button></template>
            </el-table-column>
          </el-table>
          <div v-if="form.lines.length" class="lines-sum" :class="{ mismatch: sumMismatch }">
            {{ t('supplierInvoices.linesSum') }}: {{ linesSum }}
            <span v-if="sumMismatch">≠ {{ form.totalAmount || '0' }} — {{ t('supplierInvoices.sumMismatch') }}</span>
          </div>
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="createOpen = false">{{ t('common.cancel') }}</el-button>
        <el-button type="primary" :loading="saving" @click="save">{{ t('common.save') }}</el-button>
      </template>
    </el-dialog>

    <!-- Detail: read-only, the claim and what it binds to. -->
    <el-dialog v-model="detailOpen" :title="detail?.invoiceNo || ''" width="min(960px, 94vw)" destroy-on-close>
      <el-descriptions v-if="detail" :column="3" border size="small">
        <el-descriptions-item :label="t('supplierInvoices.supplier')">{{ detail.supplierName }}</el-descriptions-item>
        <el-descriptions-item :label="t('supplierInvoices.type')">{{ t(`supplierInvoices.types.${detail.invoiceType}`) }}</el-descriptions-item>
        <el-descriptions-item :label="t('supplierInvoices.amount')">{{ detail.currency }} {{ detail.totalAmount }}</el-descriptions-item>
        <el-descriptions-item :label="t('supplierInvoices.taxAmount')">{{ detail.taxAmount }}</el-descriptions-item>
        <el-descriptions-item :label="t('supplierInvoices.invoiceDate')">{{ detail.invoiceDate }}</el-descriptions-item>
        <el-descriptions-item :label="t('supplierInvoices.dueDate')">{{ detail.dueDate || '—' }}</el-descriptions-item>
        <el-descriptions-item :label="t('supplierInvoices.status')">{{ t(`supplierInvoices.statuses.${detail.status}`) }}</el-descriptions-item>
        <el-descriptions-item :label="t('supplierInvoices.matchStatus')">{{ t(`supplierInvoices.matchStatuses.${detail.matchStatus}`) }}</el-descriptions-item>
        <el-descriptions-item :label="t('supplierInvoices.createdBy')">{{ detail.createdBy }}</el-descriptions-item>
        <el-descriptions-item v-if="detail.voidReason" :label="t('supplierInvoices.voidReason')" :span="3">{{ detail.voidReason }}</el-descriptions-item>
      </el-descriptions>
      <el-table v-if="detail?.lines?.length" :data="detail.lines" size="small" stripe style="margin-top: 12px">
        <el-table-column prop="poNo" :label="t('supplierInvoices.linePO')" width="150"><template #default="{ row }">{{ row.poNo || '—' }}</template></el-table-column>
        <el-table-column prop="description" :label="t('supplierInvoices.lineDesc')" min-width="180" />
        <el-table-column prop="qty" :label="t('supplierInvoices.lineQty')" width="110" align="right" />
        <el-table-column prop="unitPrice" :label="t('supplierInvoices.linePrice')" width="120" align="right" />
        <el-table-column prop="amount" :label="t('supplierInvoices.lineAmount')" width="130" align="right" />
      </el-table>
    </el-dialog>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { useI18n } from 'vue-i18n'
import { get, post } from '../api'
import { useAuthStore } from '../stores/auth'

const { t } = useI18n()
const auth = useAuthStore()
const canWrite = computed(() => auth.can('procurement:invoice:write'))

interface InvoiceRow {
  id: string
  supplierId: string
  supplierName: string
  invoiceNo: string
  invoiceType: string
  currency: string
  totalAmount: string
  taxAmount: string
  invoiceDate: string
  dueDate: string
  matchStatus: string
  status: string
  voidReason: string
  createdBy: string
  lines?: LineRow[]
}
interface LineRow {
  poNo?: string
  description: string
  qty: string
  unitPrice: string
  amount: string
  poId?: string
  poItemId?: string
}
interface SupplierOpt { id: string; code: string; name: string; currency: string }
interface POOpt { id: string; poNo: string; currency: string; totalAmount: string; supplierId: string }

const rows = ref<InvoiceRow[]>([])
const loading = ref(false)
const page = ref(1)
const total = ref(0)
const keyword = ref('')
const status = ref('')

const createOpen = ref(false)
const saving = ref(false)
const suppliers = ref<SupplierOpt[]>([])
const supplierPOs = ref<POOpt[]>([])
const poLoading = ref(false)
const pickedPO = ref('')
const detailOpen = ref(false)
const detail = ref<InvoiceRow | null>(null)

const form = reactive({
  supplierId: '', invoiceNo: '', invoiceType: 'COMMERCIAL', currency: '',
  totalAmount: '', taxAmount: '', invoiceDate: '', dueDate: '',
  lines: [] as LineRow[],
})

const linesSum = computed(() =>
  form.lines.reduce((acc, l) => acc + (Number(l.amount) || 0), 0).toFixed(2))
const sumMismatch = computed(() =>
  form.lines.length > 0 && Math.abs(Number(linesSum.value) - Number(form.totalAmount || 0)) > 0.005)

function isOverdue(row: InvoiceRow): boolean {
  return row.status === 'OPEN' && !!row.dueDate && row.dueDate < new Date().toISOString().slice(0, 10)
}

async function load() {
  loading.value = true
  try {
    const resp = await get<{ items: InvoiceRow[]; total: string }>('/supplier-invoices', {
      page: page.value, page_size: 20, keyword: keyword.value, status: status.value,
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

async function onSupplierChange() {
  const s = suppliers.value.find((x) => String(x.id) === form.supplierId)
  if (s?.currency && !form.currency) form.currency = s.currency
  pickedPO.value = ''
  supplierPOs.value = []
  if (!form.supplierId) return
  poLoading.value = true
  try {
    // The list endpoint has no supplier filter yet, so pull a page and keep
    // this supplier's orders. Fine at pilot scale; a filter param is the
    // obvious refinement when order counts grow.
    const resp = await get<{ orders: POOpt[] }>('/purchase-orders', { page_size: 100 })
    supplierPOs.value = (resp.orders || []).filter((o) => String(o.supplierId) === form.supplierId)
  } finally {
    poLoading.value = false
  }
}

async function pullFromPO() {
  const resp = await get<{ order: { poNo: string }; items: Array<{ id: string; productName: string; spec: string; qty: string; unitPrice: string; amount: string }> }>(
    `/purchase-orders/${pickedPO.value}`)
  const poNo = resp.order?.poNo || ''
  for (const it of resp.items || []) {
    form.lines.push({
      poId: pickedPO.value, poItemId: String(it.id), poNo,
      description: [it.productName, it.spec].filter(Boolean).join(' '),
      qty: it.qty, unitPrice: it.unitPrice, amount: it.amount,
    })
  }
}

function addLine() {
  form.lines.push({ description: '', qty: '0', unitPrice: '0', amount: '' })
}

function recalc(row: LineRow) {
  const q = Number(row.qty), p = Number(row.unitPrice)
  if (Number.isFinite(q) && Number.isFinite(p)) row.amount = (q * p).toFixed(2)
}

async function save() {
  saving.value = true
  try {
    const s = suppliers.value.find((x) => String(x.id) === form.supplierId)
    await post('/supplier-invoices', {
      supplierId: form.supplierId, supplierCode: s?.code || '', supplierName: s?.name || '',
      invoiceNo: form.invoiceNo, invoiceType: form.invoiceType, currency: form.currency,
      totalAmount: form.totalAmount, taxAmount: form.taxAmount,
      invoiceDate: form.invoiceDate, dueDate: form.dueDate,
      lines: form.lines.map((l) => ({
        poId: l.poId || '0', poItemId: l.poItemId || '0',
        description: l.description, qty: l.qty, unitPrice: l.unitPrice, amount: l.amount,
      })),
    })
    ElMessage.success(t('supplierInvoices.created'))
    createOpen.value = false
    Object.assign(form, { supplierId: '', invoiceNo: '', invoiceType: 'COMMERCIAL', currency: '', totalAmount: '', taxAmount: '', invoiceDate: '', dueDate: '', lines: [] })
    reload()
  } catch {
    /* the api layer already surfaced the server's message */
  } finally {
    saving.value = false
  }
}

async function openDetail(row: InvoiceRow) {
  const resp = await get<{ invoice: InvoiceRow }>(`/supplier-invoices/${row.id}`)
  detail.value = resp.invoice
  detailOpen.value = true
}

async function voidInvoice(row: InvoiceRow) {
  const { value } = await ElMessageBox.prompt(
    t('supplierInvoices.voidPrompt'), t('supplierInvoices.void'),
    { inputPlaceholder: t('supplierInvoices.voidReason') }).catch(() => ({ value: '' }))
  if (!value) return
  await post(`/supplier-invoices/${row.id}/void`, { reason: value })
  ElMessage.success(t('supplierInvoices.voided'))
  void load()
}

onMounted(load)
</script>

<style scoped>
.grid2 { display: grid; grid-template-columns: 1fr 1fr; column-gap: 20px; }
.lines-tools { display: flex; gap: 8px; margin-bottom: 8px; }
.lines-table { width: 100%; }
.lines-sum { margin-top: 6px; font-size: 13px; color: var(--el-text-color-secondary); }
.lines-sum.mismatch { color: var(--el-color-danger); }
.overdue { color: var(--el-color-danger); font-weight: 600; }
</style>
