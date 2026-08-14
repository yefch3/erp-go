<template>
  <div class="page">
    <ProcurementNav />
    <div class="page-head">
      <div>
        <h1>{{ t('sourcing.title') }}</h1>
        <p>{{ t('sourcing.subtitle') }}</p>
      </div>
    </div>

    <div class="toolbar">
      <el-input v-model="keyword" :placeholder="t('sourcing.search')" clearable @keyup.enter="reload" />
      <el-select v-model="status" clearable :placeholder="t('common.status')" @change="reload">
        <el-option v-for="s in statuses" :key="s" :value="s" :label="t(`sourcing.statuses.${s}`)" />
      </el-select>
      <el-button type="primary" @click="reload">{{ t('common.query') }}</el-button>
    </div>

    <el-table v-loading="loading" :data="rows" stripe @row-click="openCase">
      <el-table-column prop="caseNo" :label="t('sourcing.caseNo')" width="180" />
      <el-table-column prop="title" :label="t('sourcing.inquiry')" min-width="240" />
      <el-table-column prop="customerName" :label="t('sourcing.customer')" min-width="180" />
      <el-table-column prop="ownerName" :label="t('sourcing.owner')" width="140" />
      <el-table-column :label="t('common.status')" width="160">
        <template #default="{ row }">
          <el-tag effect="plain">{{ t(`sourcing.statuses.${row.status}`) }}</el-tag>
        </template>
      </el-table-column>
      <el-table-column :label="t('sourcing.createdAt')" width="170">
        <template #default="{ row }">{{ formatTime(row.createdAt) }}</template>
      </el-table-column>
      <template #empty>{{ t('sourcing.empty') }}</template>
    </el-table>

    <el-pagination
      class="pager" layout="total, prev, pager, next" :total="total"
      :page-size="pageSize" v-model:current-page="page" @current-change="load"
    />

    <el-dialog v-model="detailOpen" :title="detail?.caseNo || t('sourcing.title')" width="min(1200px, 94vw)">
      <el-descriptions v-if="detail" :column="3" border class="meta">
        <el-descriptions-item :label="t('sourcing.customer')">{{ detail.customerName || '—' }}</el-descriptions-item>
        <el-descriptions-item :label="t('sourcing.contact')">{{ detail.contactName || detail.contactEmail || '—' }}</el-descriptions-item>
        <el-descriptions-item :label="t('common.status')">{{ t(`sourcing.statuses.${detail.status}`) }}</el-descriptions-item>
      </el-descriptions>
      <el-table v-if="detail" :data="detail.lines" size="small" border>
        <el-table-column prop="lineNo" label="#" width="55" />
        <el-table-column :label="t('sourcing.product')" min-width="150">
          <template #default="{ row }">{{ row.extracted.product || '—' }}</template>
        </el-table-column>
        <el-table-column :label="t('sourcing.standard')" min-width="190">
          <template #default="{ row }">{{ row.extracted.materialStandard || row.extracted.grade || '—' }}</template>
        </el-table-column>
        <el-table-column :label="t('sourcing.size')" min-width="180">
          <template #default="{ row }">{{ sizeOf(row.extracted) }}</template>
        </el-table-column>
        <el-table-column :label="t('sourcing.port')" min-width="130">
          <template #default="{ row }">{{ row.extracted.port || '—' }}</template>
        </el-table-column>
        <el-table-column :label="t('sourcing.quantity')" width="140" align="right">
          <template #default="{ row }">{{ row.extracted.quantity }} {{ row.extracted.quantityUnit }}</template>
        </el-table-column>
        <el-table-column :label="t('sourcing.decision')" width="120">
          <template #default="{ row }">{{ t(`sourcing.decisions.${row.decision}`) }}</template>
        </el-table-column>
      </el-table>
      <h3 class="section-title">{{ t('sourcing.factoryRfqs') }}</h3>
      <el-table :data="rfqs" size="small" border>
        <el-table-column prop="rfqNo" :label="t('sourcing.rfqNo')" width="170" />
        <el-table-column prop="supplierName" :label="t('sourcing.supplier')" min-width="180" />
        <el-table-column prop="currency" :label="t('sourcing.currency')" width="90" />
        <el-table-column prop="responseDueAt" :label="t('sourcing.responseDue')" width="130" />
        <el-table-column prop="lineCount" :label="t('sourcing.lineCount')" width="90" />
        <el-table-column :label="t('common.status')" width="130"><template #default="{ row }">{{ t(`sourcing.rfqStatuses.${row.status}`) }}</template></el-table-column>
        <el-table-column :label="t('common.actions')" width="130"><template #default="{ row }"><el-button v-if="canWrite" link type="primary" @click.stop="openQuote(row)">{{ t('sourcing.enterQuote') }}</el-button></template></el-table-column>
      </el-table>
      <h3 class="section-title">{{ t('sourcing.quoteComparison') }}</h3>
      <el-table :data="quoteLines" size="small" border>
        <el-table-column prop="supplierName" :label="t('sourcing.supplier')" min-width="150" />
        <el-table-column :label="t('sourcing.sourceLine')" width="100"><template #default="{ row }">{{ sourceLineNo(row.sourcingLineId) }}</template></el-table-column>
        <el-table-column prop="qty" :label="t('sourcing.quantity')" width="110" />
        <el-table-column :label="t('sourcing.unitPrice')" width="150"><template #default="{ row }">{{ row.currency }} {{ row.unitPrice }}</template></el-table-column>
        <el-table-column prop="amount" :label="t('sourcing.amount')" width="140" />
        <el-table-column prop="delivery" :label="t('sourcing.delivery')" min-width="150" />
        <el-table-column prop="paymentTerms" :label="t('sourcing.paymentTerms')" min-width="180" />
      </el-table>
      <template #footer>
        <el-button @click="detailOpen = false">{{ t('common.close') }}</el-button>
        <el-button v-if="canWrite && detail?.lines.some(line => line.decision !== 'CONFIRMED')" type="success" :loading="saving" @click="confirmAllLines">{{ t('sourcing.confirmAllLines') }}</el-button>
        <el-button v-if="canWrite" type="primary" :disabled="!!detail?.lines.some(line => line.decision !== 'CONFIRMED')" @click="openRFQ">{{ t('sourcing.createFactoryRfq') }}</el-button>
      </template>
    </el-dialog>

    <el-dialog v-model="rfqOpen" :title="t('sourcing.createFactoryRfq')" width="560px" append-to-body>
      <el-form label-width="110px">
        <el-form-item :label="t('sourcing.supplier')" required><el-select v-model="rfqForm.supplierId" filterable style="width:100%"><el-option v-for="s in suppliers" :key="s.id" :value="Number(s.id)" :label="`${s.code} · ${s.name}`" /></el-select></el-form-item>
        <el-form-item :label="t('sourcing.contactEmail')"><el-input v-model="rfqForm.contactEmail" /></el-form-item>
        <el-form-item :label="t('sourcing.currency')"><el-input v-model="rfqForm.currency" maxlength="3" /></el-form-item>
        <el-form-item :label="t('sourcing.responseDue')"><el-date-picker v-model="rfqForm.responseDueAt" value-format="YYYY-MM-DD" /></el-form-item>
      </el-form>
      <template #footer><el-button @click="rfqOpen=false">{{ t('common.cancel') }}</el-button><el-button type="primary" :loading="saving" @click="createRFQ">{{ t('common.confirm') }}</el-button></template>
    </el-dialog>

    <el-dialog v-model="quoteOpen" :title="t('sourcing.enterQuote')" width="900px" append-to-body>
      <el-form inline>
        <el-form-item :label="t('sourcing.currency')"><el-input v-model="quoteForm.currency" style="width:90px" /></el-form-item>
        <el-form-item :label="t('sourcing.quotedAt')"><el-date-picker v-model="quoteForm.quotedAt" value-format="YYYY-MM-DD" /></el-form-item>
        <el-form-item :label="t('sourcing.validUntil')"><el-date-picker v-model="quoteForm.validUntil" value-format="YYYY-MM-DD" /></el-form-item>
        <el-form-item :label="t('sourcing.delivery')"><el-input v-model="quoteForm.delivery" /></el-form-item>
        <el-form-item :label="t('sourcing.paymentTerms')"><el-input v-model="quoteForm.paymentTerms" /></el-form-item>
      </el-form>
      <el-table :data="quoteRows" size="small" border>
        <el-table-column prop="product" :label="t('sourcing.product')" min-width="180" />
        <el-table-column prop="qty" :label="t('sourcing.quantity')" width="120" />
        <el-table-column :label="t('sourcing.unitPrice')" width="150"><template #default="{ row }"><el-input v-model="row.unitPrice" /></template></el-table-column>
        <el-table-column :label="t('sourcing.moq')" width="130"><template #default="{ row }"><el-input v-model="row.moq" /></template></el-table-column>
        <el-table-column :label="t('sourcing.leadTime')" min-width="160"><template #default="{ row }"><el-input v-model="row.leadTime" /></template></el-table-column>
      </el-table>
      <template #footer><el-button @click="quoteOpen=false">{{ t('common.cancel') }}</el-button><el-button type="primary" :loading="saving" @click="saveQuote">{{ t('common.save') }}</el-button></template>
    </el-dialog>
  </div>
</template>

<script setup lang="ts">
import { onMounted, reactive, ref } from 'vue'
import { ElMessage } from 'element-plus'
import { useI18n } from 'vue-i18n'
import { useRoute, useRouter } from 'vue-router'
import { get, post } from '../api'
import { useAuthStore } from '../stores/auth'
import ProcurementNav from '../components/ProcurementNav.vue'

interface ExtractedLine {
  product: string; materialStandard: string; grade: string; thickness: string
  width: string; lengthOrForm: string; port: string; quantity: string; quantityUnit: string
}
interface SourcingLine { id: string; lineNo: number; decision: string; extracted: ExtractedLine }
interface SourcingCase {
  id: string; caseNo: string; title: string; customerName: string; contactName: string
  contactEmail: string; ownerName: string; status: string; createdAt: string; lines: SourcingLine[]
}
interface Supplier { id: string; code: string; name: string }
interface FactoryRFQ { id: string; rfqNo: string; supplierName: string; currency: string; responseDueAt: string; status: string; lineCount: number }
interface QuoteComparison { supplierName: string; sourcingLineId: string; qty: string; currency: string; unitPrice: string; amount: string; delivery: string; paymentTerms: string }

const { t } = useI18n()
const route = useRoute()
const router = useRouter()
const auth = useAuthStore()
const canWrite = auth.can('procurement:sourcing:write')
const statuses = ['REVIEWING', 'SOURCING', 'QUOTES_RECEIVED', 'COSTING', 'CUSTOMER_QUOTE_CREATED', 'CANCELLED']
const rows = ref<SourcingCase[]>([])
const total = ref(0)
const page = ref(1)
const pageSize = 20
const keyword = ref('')
const status = ref('')
const loading = ref(false)
const detailOpen = ref(false)
const detail = ref<SourcingCase | null>(null)
const rfqs = ref<FactoryRFQ[]>([])
const quoteLines = ref<QuoteComparison[]>([])
const suppliers = ref<Supplier[]>([])
const saving = ref(false)
const rfqOpen = ref(false)
const rfqForm = reactive({ supplierId: 0, contactEmail: '', currency: 'USD', responseDueAt: '' })
const quoteOpen = ref(false)
const quotingRFQ = ref<FactoryRFQ | null>(null)
const quoteForm = reactive({ currency: 'USD', quotedAt: '', validUntil: '', delivery: '', paymentTerms: '' })
const quoteRows = ref<{ sourcingLineId: string; product: string; qty: string; unitPrice: string; moq: string; leadTime: string }[]>([])

async function load() {
  loading.value = true
  try {
    const response = await get<{ sourcingCases: SourcingCase[]; meta: { total: number } }>('/sourcing-cases', {
      page: page.value, page_size: pageSize, keyword: keyword.value, status: status.value,
    })
    rows.value = response.sourcingCases ?? []
    total.value = Number(response.meta?.total ?? 0)
  } finally { loading.value = false }
}

function reload() { page.value = 1; load() }

async function openCase(row: Pick<SourcingCase, 'id'>) {
  const response = await get<{ sourcingCase: SourcingCase }>(`/sourcing-cases/${row.id}`)
  detail.value = response.sourcingCase
  await loadSourcingCommercial(row.id)
  detailOpen.value = true
  router.replace({ query: { ...route.query, case: row.id } })
}

async function loadSourcingCommercial(caseID: string) {
  const [rfqResponse, quoteResponse] = await Promise.all([
    get<{ factoryRfqs: FactoryRFQ[] }>(`/sourcing-cases/${caseID}/factory-rfqs`),
    get<{ lines: QuoteComparison[] }>(`/sourcing-cases/${caseID}/supplier-quotes`),
  ])
  rfqs.value = rfqResponse.factoryRfqs ?? []
  quoteLines.value = quoteResponse.lines ?? []
}

function sourceLineNo(id: string) { return detail.value?.lines.find(line => String(line.id) === String(id))?.lineNo ?? id }

async function confirmAllLines() {
  if (!detail.value) return
  saving.value = true
  try {
    const response = await post<{ sourcingCase: SourcingCase }>(`/sourcing-cases/${detail.value.id}/confirm-lines`, { sourcing_line_ids: detail.value.lines.map(line => Number(line.id)) })
    detail.value = response.sourcingCase
    ElMessage.success(t('sourcing.linesConfirmed'))
  } finally { saving.value = false }
}

async function openRFQ() {
  if (!suppliers.value.length) suppliers.value = (await get<{ suppliers: Supplier[] }>('/suppliers', { page_size: 200, status: 'ACTIVE' })).suppliers ?? []
  rfqForm.supplierId = 0; rfqForm.contactEmail = ''; rfqForm.currency = 'USD'; rfqForm.responseDueAt = ''
  rfqOpen.value = true
}

async function createRFQ() {
  if (!detail.value || !rfqForm.supplierId) { ElMessage.warning(t('sourcing.supplierRequired')); return }
  saving.value = true
  try {
    await post(`/sourcing-cases/${detail.value.id}/factory-rfqs`, { supplier_id: rfqForm.supplierId, contact_email: rfqForm.contactEmail, currency: rfqForm.currency, response_due_at: rfqForm.responseDueAt, sourcing_line_ids: detail.value.lines.map(l => Number(l.id)) })
    rfqOpen.value = false; await loadSourcingCommercial(detail.value.id); ElMessage.success(t('sourcing.rfqCreated'))
  } finally { saving.value = false }
}

function openQuote(row: FactoryRFQ) {
  if (!detail.value) return
  quotingRFQ.value = row; quoteForm.currency = row.currency || 'USD'; quoteForm.quotedAt = ''; quoteForm.validUntil = ''; quoteForm.delivery = ''; quoteForm.paymentTerms = ''
  quoteRows.value = detail.value.lines.map(line => ({ sourcingLineId: line.id, product: line.extracted.product, qty: line.extracted.quantity, unitPrice: '', moq: '', leadTime: '' }))
  quoteOpen.value = true
}

async function saveQuote() {
  if (!quotingRFQ.value || quoteRows.value.some(row => Number(row.unitPrice) < 0 || row.unitPrice === '')) { ElMessage.warning(t('sourcing.priceRequired')); return }
  saving.value = true
  try {
    await post(`/factory-rfqs/${quotingRFQ.value.id}/supplier-quotes`, { quoted_at: quoteForm.quotedAt, valid_until: quoteForm.validUntil, currency: quoteForm.currency, payment_terms: quoteForm.paymentTerms, delivery: quoteForm.delivery, source: 'MANUAL', lines: quoteRows.value.map(row => ({ sourcing_line_id: Number(row.sourcingLineId), qty: row.qty, unit_price: row.unitPrice, moq: row.moq, lead_time: row.leadTime })) })
    quoteOpen.value = false; if (detail.value) await loadSourcingCommercial(detail.value.id); await load(); ElMessage.success(t('sourcing.quoteSaved'))
  } finally { saving.value = false }
}

function sizeOf(line: ExtractedLine) {
  return [line.thickness, line.width, line.lengthOrForm].filter(Boolean).join(' × ') || '—'
}
function formatTime(value: string) { return value ? new Date(value).toLocaleString() : '—' }

onMounted(async () => {
  await load()
  const id = String(route.query.case || '')
  if (id) await openCase({ id })
})
</script>

<style scoped>
.page { padding: 24px; }
.page-head { display:flex; justify-content:space-between; margin-bottom:18px; }
h1 { margin:0; font-size:24px; } .page-head p { margin:6px 0 0; color:#6b7280; }
.toolbar { display:flex; gap:10px; margin-bottom:14px; }
.toolbar .el-input { width:300px; } .toolbar .el-select { width:190px; }
.pager { margin-top:16px; justify-content:flex-end; } .meta { margin-bottom:16px; }
.section-title { margin:20px 0 10px; font-size:16px; }
</style>
