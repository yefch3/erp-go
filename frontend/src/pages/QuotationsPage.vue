<template>
  <div :class="{ 'quotation-embedded': embedded }">
    <div v-if="!embedded" class="page-head">
      <h2>{{ t('quotations.title') }}</h2>
      <el-button v-if="canWrite" type="primary" @click="openCreate">{{ t('quotations.create') }}</el-button>
    </div>

    <el-card shadow="never">
      <div v-if="!embedded" class="filters">
        <el-input
          v-model="keyword"
          :placeholder="t('quotations.searchPlaceholder')"
          clearable
          style="width: 240px"
          @keyup.enter="reload"
          @clear="reload"
        />
        <el-select v-model="status" :placeholder="t('quotations.allStatus')" clearable style="width: 160px" @change="reload">
          <el-option v-for="s in STATUSES" :key="s" :value="s" :label="t(`quotations.statuses.${s}`)" />
        </el-select>
        <el-button @click="reload">{{ t('common.query') }}</el-button>
      </div>

      <el-table :data="quotations" v-loading="loading">
        <el-table-column prop="quoteNo" :label="t('quotations.quoteNo')" width="150" />
        <el-table-column prop="customerName" :label="t('quotations.customer')" min-width="160" />
        <el-table-column :label="t('quotations.amount')" width="140" align="right">
          <template #default="{ row }">{{ row.totalAmount }} {{ row.currency }}</template>
        </el-table-column>
        <el-table-column :label="t('quotations.baseAmount')" width="120" align="right">
          <template #default="{ row }"><span class="sub">{{ row.baseAmount }} USD</span></template>
        </el-table-column>
        <el-table-column :label="t('quotations.validUntil')" width="100">
          <template #default="{ row }">{{ row.validUntil || '—' }}</template>
        </el-table-column>
        <el-table-column :label="t('common.status')" width="90">
          <template #default="{ row }">
            <el-tag size="small" :type="statusType(row.status)">{{ t(`quotations.statuses.${row.status}`) }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column :label="t('common.actions')" width="300" fixed="right">
          <template #default="{ row }">
            <el-button link type="primary" @click="openEdit(row)">
              {{ row.status === 'DRAFT' && !row.sourceCostScenarioId && canWrite && auth.owns(row.salesEmployeeId) ? t('common.edit') : t('quotations.view') }}
            </el-button>
            <el-button link type="primary" @click="downloadQuotation(row, 'workbook')">{{ t('quotations.downloadExcel') }}</el-button>
            <el-button link type="primary" @click="downloadQuotation(row, 'pdf')">{{ t('quotations.downloadPdf') }}</el-button>
            <!-- Ownership, not just the permission code: a wide data scope is
                 for watching other people's work, not doing it. -->
            <template v-if="canWrite && auth.owns(row.salesEmployeeId)">
              <el-button v-if="row.status === 'DRAFT'" link type="primary" @click="act(row, 'send')">
                {{ t('quotations.send') }}
              </el-button>
              <template v-if="row.status === 'SENT'">
                <el-button link type="success" @click="respond(row, 'ACCEPTED')">{{ t('quotations.accept') }}</el-button>
                <el-button link type="danger" @click="respond(row, 'REJECTED')">{{ t('quotations.reject') }}</el-button>
              </template>
            </template>
          </template>
        </el-table-column>
      </el-table>

      <el-empty v-if="embedded && !loading && quotations.length === 0" :description="t('quotations.empty')" />
      <el-pagination
        v-if="!embedded"
        class="pager"
        layout="total, prev, pager, next"
        :total="total"
        :page-size="pageSize"
        :current-page="page"
        @current-change="(p: number) => { page = p; load() }"
      />
    </el-card>

    <el-dialog v-model="dialogOpen" :title="dialogTitle" width="860px">
      <el-form :model="form" label-width="100px" v-loading="loadingDetail" :disabled="readOnly">
        <div class="grid">
          <el-form-item :label="t('quotations.customer')" required>
            <el-select v-model="form.customerId" filterable style="width: 100%">
              <el-option v-for="c in customers" :key="c.id" :value="c.id" :label="`${c.code} · ${c.name}`" />
            </el-select>
          </el-form-item>
          <el-form-item :label="t('quotations.contact')">
            <el-select v-model="form.contactId" clearable style="width: 100%" :placeholder="t('quotations.contactAuto')">
              <el-option
                v-for="c in contacts"
                :key="c.id"
                :value="c.id"
                :label="c.email ? `${c.name} · ${c.email}` : c.name"
              />
            </el-select>
          </el-form-item>
          <el-form-item :label="t('quotations.currency')" required>
            <el-select v-model="form.currency" style="width: 100%">
              <el-option v-for="c in CURRENCIES" :key="c" :value="c" :label="c" />
            </el-select>
          </el-form-item>
          <el-form-item :label="t('quotations.incoterm')">
            <el-select v-model="form.incoterm" style="width: 100%">
              <el-option v-for="i in INCOTERMS" :key="i" :value="i" :label="i" />
            </el-select>
          </el-form-item>
          <el-form-item :label="t('quotations.paymentMethod')">
            <el-select v-model="form.paymentMethod" clearable style="width: 100%">
              <el-option v-for="o in paymentOptions" :key="o.code" :value="o.code" :label="o.label" />
            </el-select>
          </el-form-item>
          <el-form-item :label="t('quotations.portOfLoading')">
            <el-input v-model="form.portOfLoading" placeholder="Ningbo" />
          </el-form-item>
          <el-form-item :label="t('quotations.portOfDischarge')">
            <el-input v-model="form.portOfDischarge" placeholder="Hamburg" />
          </el-form-item>
          <el-form-item :label="t('quotations.validUntil')">
            <el-date-picker v-model="form.validUntil" type="date" value-format="YYYY-MM-DD" style="width: 100%" />
          </el-form-item>
          <el-form-item :label="t('quotations.remark')">
            <el-input v-model="form.remark" />
          </el-form-item>
        </div>

        <el-divider content-position="left">
          {{ t('quotations.items') }}
          <span class="hint">{{ t('quotations.itemsHint') }}</span>
        </el-divider>
        <el-table :data="form.items" size="small">
          <el-table-column :label="t('quotations.product')" min-width="220">
            <template #default="{ row }">
              <div v-if="row.productName && (!row.productId || row.productId === '0')" class="product-snapshot">
                <span>{{ row.productName }}</span>
                <small v-if="row.productCode">{{ row.productCode }}</small>
              </div>
              <el-select v-else v-model="row.productId" filterable style="width: 100%" :placeholder="t('quotations.pickProduct')">
                <el-option v-for="p in products" :key="p.id" :value="p.id" :label="`${p.code} · ${p.name}`" />
              </el-select>
            </template>
          </el-table-column>
          <el-table-column :label="t('quotations.spec')" min-width="260">
            <template #default="{ row }">
              <el-tooltip :content="row.spec || '—'" placement="top" :disabled="!row.spec">
                <el-input v-model="row.spec" />
              </el-tooltip>
            </template>
          </el-table-column>
          <el-table-column :label="t('quotations.qty')" width="110">
            <template #default="{ row }"><el-input v-model="row.qty" placeholder="0" /></template>
          </el-table-column>
          <el-table-column :label="t('quotations.unitPrice')" width="110">
            <template #default="{ row }"><el-input v-model="row.unitPrice" placeholder="0.00" /></template>
          </el-table-column>
          <el-table-column :label="t('quotations.lineAmount')" width="120" align="right">
            <template #default="{ row }">{{ lineAmount(row) }}</template>
          </el-table-column>
          <el-table-column width="60">
            <template #default="{ $index }">
              <el-button link type="danger" :disabled="readOnly" @click="form.items.splice($index, 1)">
                {{ t('common.delete') }}
              </el-button>
            </template>
          </el-table-column>
        </el-table>
        <div class="items-foot">
          <el-button :disabled="readOnly" @click="addItem">{{ t('quotations.addItem') }}</el-button>
          <div class="totals">
            <span class="hint">{{ t('quotations.totalPreview') }}</span>
            <strong>{{ previewTotal }} {{ form.currency }}</strong>
          </div>
        </div>

        <div v-if="detail?.contactEmail" class="snapshot">
          {{ t('quotations.addressedTo') }}: {{ detail.contactName }} &lt;{{ detail.contactEmail }}&gt;
          <span class="hint">{{ t('quotations.sendManual') }}</span>
        </div>
        <div v-if="detail" class="snapshot">
          {{ t('quotations.fxSnapshot') }}:
          1 {{ detail.fx.baseCurrency }} = {{ detail.fx.rate }} {{ detail.currency }}
          · {{ detail.fx.source }} · {{ formatTime(detail.fx.rateAt) }}
          <span class="hint">{{ t('quotations.fxFrozen') }}</span>
        </div>
        <div v-if="detail?.sourceCostScenarioNo" class="snapshot">
          {{ t('quotations.sourceCostScenario') }}: {{ detail.sourceCostScenarioNo }}
          <span class="hint">{{ t('quotations.sourceLocked') }}</span>
        </div>
      </el-form>
      <template #footer>
        <el-button @click="dialogOpen = false">{{ readOnly ? t('common.cancel') : t('common.cancel') }}</el-button>
        <el-button v-if="!readOnly" type="primary" :loading="saving" @click="save">{{ t('common.save') }}</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref, watch } from 'vue'
import { ElMessage } from 'element-plus'
import { useI18n } from 'vue-i18n'
import { useRoute } from 'vue-router'
import { download, get, post, put, saveBlob } from '../api'
import { useAuthStore } from '../stores/auth'

interface Customer { id: string; code: string; name: string }
interface Contact { id: string; name: string; email: string; isPrimary: boolean }
interface Product { id: string; code: string; name: string }
interface OptionItem { code: string; label: string }
interface Fx { rate: string; rateAt: string; source: string; baseCurrency: string }
interface Quotation {
  id: string
  quoteNo: string
  salesEmployeeId: string
  customerId: string
  customerName: string
  contactId: string
  contactName: string
  contactEmail: string
  currency: string
  incoterm: string
  portOfLoading: string
  portOfDischarge: string
  paymentMethod: string
  validUntil: string
  fx: Fx
  totalAmount: string
  baseAmount: string
  remark: string
  status: string
  sourceCostScenarioId: string
  sourceCostScenarioNo: string
}
interface Item {
  productId: string
  productCode: string
  productName: string
  uomCode: string
  spec: string
  qty: string
  unitPrice: string
  remark: string
}

const STATUSES = ['DRAFT', 'SENT', 'ACCEPTED', 'REJECTED', 'EXPIRED', 'CANCELLED']
const CURRENCIES = ['USD', 'EUR', 'CNY', 'GBP', 'JPY', 'HKD']
const INCOTERMS = ['FOB', 'CIF', 'CFR', 'EXW', 'DDP']
const EMPTY_FORM = {
  customerId: '', contactId: '', currency: 'USD', incoterm: 'FOB', paymentMethod: '',
  portOfLoading: '', portOfDischarge: '', validUntil: '', remark: '',
  items: [] as Item[],
}

const { t } = useI18n()
const props = withDefaults(defineProps<{
  embedded?: boolean
  quotationIds?: Array<string | number>
}>(), {
  embedded: false,
  quotationIds: () => [],
})
const embedded = computed(() => props.embedded)
const auth = useAuthStore()
const route = useRoute()
const canWrite = auth.can('export:quotation:write')

const quotations = ref<Quotation[]>([])
const customers = ref<Customer[]>([])
const products = ref<Product[]>([])
const paymentOptions = ref<OptionItem[]>([])
const detail = ref<Quotation | null>(null)
const contacts = ref<Contact[]>([])
const total = ref(0)
const page = ref(1)
const pageSize = 10
const keyword = ref('')
const status = ref('')
const loading = ref(false)
const loadingDetail = ref(false)
const saving = ref(false)
const dialogOpen = ref(false)
const editingId = ref<string | null>(null)
const readOnly = ref(false)
const form = reactive({ ...EMPTY_FORM, items: [] as Item[] })

const dialogTitle = computed(() =>
  readOnly.value ? t('quotations.view') : editingId.value ? t('quotations.edit') : t('quotations.create'),
)

// Preview only: the server recomputes every amount, and its numbers are the
// ones stored. This just keeps the salesperson oriented while typing.
function lineAmount(row: Item): string {
  const qty = Number(row.qty)
  const price = Number(row.unitPrice)
  if (!Number.isFinite(qty) || !Number.isFinite(price)) return '—'
  return (Math.round(qty * price * 100) / 100).toFixed(2)
}

const previewTotal = computed(() =>
  form.items
    .reduce((sum, row) => {
      const amount = Number(lineAmount(row))
      return Number.isFinite(amount) ? sum + amount : sum
    }, 0)
    .toFixed(2),
)

// The addressee list belongs to the chosen customer, so it is fetched with it.
watch(() => form.customerId, async (id) => {
  contacts.value = []
  if (!id) return
  const data = await get<{ customer: { contacts: { name: string; email: string; isPrimary: boolean }[] } }>(`/customers/${id}`)
  // masterdata returns contacts positionally; the same 1-based handle the
  // export service uses.
  contacts.value = (data.customer.contacts ?? []).map((c, i) => ({
    id: String(i + 1), name: c.name, email: c.email, isPrimary: c.isPrimary,
  }))
})

async function load() {
  loading.value = true
  try {
    if (embedded.value) {
      const ids = [...new Set(props.quotationIds.map(String).filter(Boolean))]
      const results = await Promise.all(ids.map(async quotationID => {
        const data = await get<{ quotation: Quotation }>(`/quotations/${quotationID}`)
        return data.quotation
      }))
      quotations.value = results
      total.value = results.length
      return
    }
    const data = await get<{ quotations: Quotation[]; meta: { total: string } }>('/quotations', {
      page: page.value, page_size: pageSize, keyword: keyword.value, status: status.value,
    })
    quotations.value = data.quotations ?? []
    total.value = Number(data.meta.total)
  } finally {
    loading.value = false
  }
}

function reload() {
  page.value = 1
  load()
}

function openCreate() {
  editingId.value = null
  readOnly.value = false
  detail.value = null
  Object.assign(form, { ...EMPTY_FORM, items: [{ productId: '', productCode: '', productName: '', uomCode: '', spec: '', qty: '', unitPrice: '', remark: '' }] })
  dialogOpen.value = true
}

async function openEdit(row: Quotation) {
  editingId.value = row.id
  // Somebody else's quotation opens read-only however wide the scope is.
  readOnly.value = row.status !== 'DRAFT' || !canWrite || !auth.owns(row.salesEmployeeId)
  dialogOpen.value = true
  loadingDetail.value = true
  try {
    const data = await get<{ quotation: Quotation; items: any[] }>(`/quotations/${row.id}`)
    detail.value = data.quotation
    readOnly.value = readOnly.value || !!data.quotation.sourceCostScenarioId
    Object.assign(form, {
      customerId: data.quotation.customerId, contactId: data.quotation.contactId || '',
      currency: data.quotation.currency,
      incoterm: data.quotation.incoterm, paymentMethod: data.quotation.paymentMethod,
      portOfLoading: data.quotation.portOfLoading, portOfDischarge: data.quotation.portOfDischarge,
      validUntil: data.quotation.validUntil, remark: data.quotation.remark,
      items: (data.items ?? []).map((i) => ({
        productId: String(i.productId || ''), productCode: i.productCode || '', productName: i.productName || '',
        uomCode: i.uomCode || '', spec: i.spec, qty: i.qty, unitPrice: i.unitPrice, remark: i.remark,
      })),
    })
  } catch {
    dialogOpen.value = false
  } finally {
    loadingDetail.value = false
  }
}

async function downloadQuotation(row: Quotation, type: 'workbook' | 'pdf') {
  const file = await download(`/quotations/${row.id}/${type}`)
  saveBlob(file.blob, file.fileName || `${row.quoteNo}.${type === 'pdf' ? 'pdf' : 'xlsx'}`)
}

function addItem() {
  form.items.push({ productId: '', productCode: '', productName: '', uomCode: '', spec: '', qty: '', unitPrice: '', remark: '' })
}

async function save() {
  if (!form.customerId || form.items.length === 0) {
    ElMessage.warning(t('quotations.required'))
    return
  }
  saving.value = true
  const body = {
    customerId: form.customerId, contactId: form.contactId || '0', currency: form.currency, incoterm: form.incoterm,
    portOfLoading: form.portOfLoading, portOfDischarge: form.portOfDischarge,
    paymentMethod: form.paymentMethod, validUntil: form.validUntil, remark: form.remark,
    items: form.items.map((i) => ({
      productId: i.productId, spec: i.spec, qty: i.qty, unitPrice: i.unitPrice, remark: i.remark,
    })),
  }
  try {
    if (editingId.value) {
      await put(`/quotations/${editingId.value}`, body)
      ElMessage.success(t('quotations.updated'))
    } else {
      await post('/quotations', body)
      ElMessage.success(t('quotations.created'))
    }
    dialogOpen.value = false
    load()
  } finally {
    saving.value = false
  }
}

async function act(row: Quotation, action: string) {
  await post(`/quotations/${row.id}/${action}`)
  ElMessage.success(t('quotations.sent'))
  load()
}

async function respond(row: Quotation, answer: string) {
  await post(`/quotations/${row.id}/respond`, { status: answer })
  ElMessage.success(t(answer === 'ACCEPTED' ? 'quotations.accepted' : 'quotations.rejected'))
  load()
}

function statusType(s: string): 'success' | 'danger' | 'warning' | 'info' {
  return ({ ACCEPTED: 'success', REJECTED: 'danger', SENT: 'warning' } as const)[s] ?? 'info'
}

function formatTime(iso: string): string {
  return iso ? iso.replace('T', ' ').slice(0, 16) : ''
}

onMounted(async () => {
  await load()
  customers.value = (await get<{ customers: Customer[] }>('/customers', { page_size: 200 })).customers ?? []
  products.value = (await get<{ products: Product[] }>('/products', { page_size: 200 })).products ?? []
  paymentOptions.value = (await get<{ options: OptionItem[] }>('/options', { category: 'PAYMENT_METHOD' })).options ?? []
  const quoteID = embedded.value ? '' : String(route.query.quote || '')
  const row = quotations.value.find(quotation => String(quotation.id) === quoteID)
  if (row) await openEdit(row)
})

watch(
  () => props.quotationIds.map(String).join(','),
  () => { if (embedded.value) void load() },
)
</script>

<style scoped>
.page-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin-bottom: 16px;
}
.page-head h2 {
  font-size: 18px;
  font-weight: 500;
  margin: 0;
}
.filters {
  display: flex;
  gap: 10px;
  margin-bottom: 14px;
}
.pager {
  margin-top: 14px;
  justify-content: flex-end;
}
.grid {
  display: grid;
  grid-template-columns: 1fr 1fr;
  column-gap: 16px;
}
.sub {
  color: var(--el-text-color-secondary);
  font-size: 12px;
}
.hint {
  margin-left: 8px;
  font-weight: 400;
  font-size: 12px;
  color: var(--el-text-color-secondary);
}
.items-foot {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin-top: 10px;
}
.totals strong {
  margin-left: 8px;
  font-size: 15px;
}
.snapshot {
  margin-top: 16px;
  padding: 8px 12px;
  border-radius: 6px;
  background: var(--el-fill-color-light);
  font-size: 13px;
}
.quotation-embedded :deep(.el-card) {
  border: 0;
}
.quotation-embedded :deep(.el-card__body) {
  padding: 0;
}
.product-snapshot {
  display: flex;
  min-height: 32px;
  flex-direction: column;
  justify-content: center;
  line-height: 1.35;
  color: var(--el-text-color-primary);
}
.product-snapshot small {
  color: var(--el-text-color-secondary);
}
</style>
