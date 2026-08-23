<template>
  <div>
    <div class="page-head">
      <h2>{{ t('requirements.title') }}</h2>
      <span class="head-note">{{ t('requirements.readOnlyHint') }}</span>
      <span class="grow" />
      <el-button @click="router.push('/procurement')">← {{ t('procurementNav.backToWorkbench') }}</el-button>
      <!-- Ordering is what a buyer actually comes here to do. Several lines at
           once, because one order to one supplier covering three contracts is
           the whole reason the job exists. -->
      <el-button v-if="canOrder && selected.length" type="primary" @click="goOrder">
        {{ t('requirements.orderSelected', { n: selected.length }) }}
      </el-button>
      <el-button v-if="canOrder && selected.length" :loading="saving" @click="exportTemplate">
        {{ t('requirements.exportTemplate', { n: selected.length }) }}
      </el-button>
      <el-button v-if="canOrder" @click="openTemplateImport">
        {{ t('requirements.importTemplate') }}
      </el-button>
      <el-button v-if="canException" @click="openCreate">
        {{ t('requirements.create') }}
      </el-button>
    </div>

    <el-card shadow="never">
      <el-radio-group v-model="status" class="tabs" @change="reload">
        <el-radio-button value="PENDING">{{ t('requirements.tabPending') }}</el-radio-button>
        <el-radio-button value="ORDERED">{{ t('requirements.tabOrdered') }}</el-radio-button>
        <el-radio-button value="SUPERSEDED">{{ t('requirements.tabSuperseded') }}</el-radio-button>
        <el-radio-button value="CANCELLED">{{ t('requirements.tabCancelled') }}</el-radio-button>
        <el-radio-button value="">{{ t('requirements.tabAll') }}</el-radio-button>
      </el-radio-group>

      <div class="filters">
        <el-input
          v-model="keyword"
          :placeholder="t('requirements.searchPlaceholder')"
          clearable
          style="width: 260px"
          @keyup.enter="reload"
          @clear="reload"
        />
        <el-button @click="reload">{{ t('common.query') }}</el-button>
      </div>

      <el-table :data="rows" v-loading="loading" @selection-change="onSelect">
        <el-table-column v-if="canOrder" type="selection" width="42" :selectable="isOrderable" />
        <el-table-column :label="t('requirements.product')" min-width="200">
          <template #default="{ row }">
            <div class="prod">{{ row.productName }}</div>
            <div class="sub">{{ row.productCode }}<span v-if="row.spec"> · {{ row.spec }}</span></div>
          </template>
        </el-table-column>
        <el-table-column :label="t('requirements.qty')" width="150" align="right">
          <template #default="{ row }">
            <span class="qty">{{ trimQty(row.requiredQty) }}</span> {{ row.uomCode }}
            <!-- Progress only means something once part of it is on order;
                 showing "0 / 500" on every untouched line is noise. -->
            <div v-if="Number(row.orderedQty) > 0" class="sub">
              {{ t('requirements.ordered', { n: trimQty(row.orderedQty) }) }}
            </div>
          </template>
        </el-table-column>
        <el-table-column :label="t('requirements.requiredDate')" width="120">
          <template #default="{ row }">
            <span :class="{ overdue: isOverdue(row) }">{{ row.requiredDate || '—' }}</span>
          </template>
        </el-table-column>
        <el-table-column :label="t('requirements.fromContract')" width="180">
          <template #default="{ row }">
            <!-- A manually raised requirement has no contract behind it, and
                 saying so is more useful than an empty cell. -->
            <template v-if="row.source === 'MANUAL'">
              <el-tag size="small" type="info" effect="plain">{{ t('requirements.manual') }}</el-tag>
            </template>
            <template v-else>
            <!-- The contract is the reason this line exists, so it has to be
                 one click away: a buyer about to spend money should be able
                 to read what was actually sold. -->
            <router-link :to="`/contracts?id=${row.contractId}`" class="doc-link">
              {{ row.contractNo }}
            </router-link>
            <span class="ver">v{{ row.versionNo }}</span>
            <div class="sub">{{ row.customerName }}<template v-if="row.ownerName"> · {{ row.ownerName }}</template></div>
            </template>
          </template>
        </el-table-column>
        <el-table-column :label="t('common.status')" width="180">
          <template #default="{ row }">
            <el-tag size="small" :type="statusType(row.status)" effect="plain">
              {{ t(`requirements.statuses.${row.status}`) }}
            </el-tag>
            <div v-if="row.closedReason" class="sub reason">{{ row.closedReason }}</div>
          </template>
        </el-table-column>
        <el-table-column :label="t('common.actions')" width="200" fixed="right">
          <template #default="{ row }">
            <el-button link type="primary" @click="openDetail(row)">{{ common('detail') }}</el-button>
            <el-button
              v-if="canOrder && isOrderable(row)"
              link
              type="success"
              @click="goOrder([row])"
            >
              {{ t('requirements.order') }}
            </el-button>
            <el-button v-if="canWrite && row.status === 'PENDING'" link type="danger" @click="openClose(row)">
              {{ t('requirements.close') }}
            </el-button>
            <!-- A close can be wrong or a contract change can be reverted. -->
            <el-button
              v-if="canWrite && canReopen(row)"
              link
              type="warning"
              @click="reopen(row)"
            >
              {{ t('requirements.reopen') }}
            </el-button>
          </template>
        </el-table-column>
        <template #empty>{{ t('requirements.empty') }}</template>
      </el-table>

      <el-pagination
        class="pager"
        layout="total, prev, pager, next"
        :total="total"
        :page-size="pageSize"
        :current-page="page"
        @current-change="(p: number) => { page = p; load() }"
      />
    </el-card>

    <el-drawer v-model="detailOpen" :title="detail?.productName" size="620px">
      <el-descriptions :column="2" border size="small" class="desc">
        <el-descriptions-item :label="t('requirements.source')">
          <el-tag size="small" :type="detail?.source === 'MANUAL' ? 'info' : 'primary'" effect="plain">
            {{ detail?.source === 'MANUAL' ? t('requirements.manual') : t('requirements.fromContract') }}
          </el-tag>
        </el-descriptions-item>
        <el-descriptions-item :label="t('common.status')">
          {{ detail ? t(`requirements.statuses.${detail.status}`) : '' }}
        </el-descriptions-item>
        <el-descriptions-item v-if="detail?.source !== 'MANUAL'" :label="t('requirements.fromContract')">
          {{ detail?.contractNo }} · {{ detail?.customerName }}
          <template v-if="detail?.ownerName"> · {{ detail.ownerName }}</template>
        </el-descriptions-item>
        <el-descriptions-item :label="t('requirements.requiredDate')">
          {{ detail?.requiredDate || '—' }}
        </el-descriptions-item>
        <el-descriptions-item :label="t('requirements.qty')">
          {{ trimQty(detail?.requiredQty ?? '') }} {{ detail?.uomCode }}
        </el-descriptions-item>
        <el-descriptions-item :label="t('requirements.progress')">
          {{ t('requirements.ordered', { n: trimQty(detail?.orderedQty ?? '0') }) }} ·
          {{ t('requirements.arrived', { n: trimQty(detail?.receivedQty ?? '0') }) }}
        </el-descriptions-item>
        <el-descriptions-item v-if="detail?.closedReason" :label="t('requirements.reason')" :span="2">
          {{ detail?.closedReason }}
        </el-descriptions-item>
      </el-descriptions>

      <!-- The question a buyer actually has in front of an outstanding line:
           is nobody buying this, or is it already on order and merely late? -->
      <div class="side-title">{{ t('requirements.coveringOrders') }}</div>
      <el-table :data="covering" size="small">
        <el-table-column :label="t('requirements.poNo')" min-width="150">
          <template #default="{ row }">
            <router-link :to="`/purchase-orders?keyword=${row.poNo}`" class="doc-link">
              {{ row.poNo }}
            </router-link>
            <div class="sub">{{ row.supplierName }}</div>
          </template>
        </el-table-column>
        <el-table-column :label="t('requirements.qty')" width="150" align="right">
          <template #default="{ row }">
            <span class="qty">{{ trimQty(row.qty) }}</span>
            <div v-if="Number(row.receivedQty) > 0" class="sub">
              {{ t('requirements.arrived', { n: trimQty(row.receivedQty) }) }}
            </div>
          </template>
        </el-table-column>
        <el-table-column :label="t('requirements.expected')" width="110">
          <template #default="{ row }">{{ row.expectedDate || '—' }}</template>
        </el-table-column>
        <el-table-column :label="t('common.status')" width="110">
          <template #default="{ row }">
            <el-tag size="small" effect="plain">{{ t(`orders.statuses.${row.status}`) }}</el-tag>
          </template>
        </el-table-column>
        <template #empty>{{ t('requirements.noOrders') }}</template>
      </el-table>
    </el-drawer>

    <el-dialog v-model="createOpen" :title="t('requirements.create')" width="520px">
      <el-alert type="info" :closable="false" show-icon class="alert">
        {{ t('requirements.createHint') }}
      </el-alert>
      <el-form label-width="90px">
        <el-form-item :label="t('requirements.product')" required>
          <el-select v-model="createForm.productId" filterable style="width: 100%">
            <el-option
              v-for="p in products"
              :key="p.id"
              :value="Number(p.id)"
              :label="`${p.code} · ${p.name}`"
            />
          </el-select>
        </el-form-item>
        <el-form-item :label="t('requirements.qty')" required>
          <el-input v-model="createForm.qty" style="width: 200px" />
        </el-form-item>
        <el-form-item :label="t('requirements.requiredDate')">
          <el-date-picker v-model="createForm.requiredDate" type="date" value-format="YYYY-MM-DD" style="width: 200px" />
        </el-form-item>
        <el-form-item :label="t('requirements.reason')">
          <el-input v-model="createForm.remark" type="textarea" :rows="2" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="createOpen = false">{{ common('cancel') }}</el-button>
        <el-button type="primary" :loading="saving" @click="submitCreate">{{ common('save') }}</el-button>
      </template>
    </el-dialog>

    <el-dialog v-model="closeOpen" :title="t('requirements.close')" width="480px">
      <el-alert type="warning" :closable="false" show-icon class="alert">
        {{ t('requirements.closeWarning') }}
      </el-alert>
      <el-form label-width="90px" class="close-form">
        <el-form-item :label="t('requirements.product')">
          <div>
            <div>{{ closing?.productName }}</div>
            <div class="sub">
              {{ closing?.contractNo }} · {{ trimQty(closing?.requiredQty ?? '') }} {{ closing?.uomCode }}
            </div>
          </div>
        </el-form-item>
        <el-form-item :label="t('requirements.reason')" required>
          <el-input v-model="closeReason" type="textarea" :rows="3" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="closeOpen = false">{{ common('cancel') }}</el-button>
        <el-button type="danger" :loading="saving" @click="confirmClose">
          {{ t('requirements.close') }}
        </el-button>
      </template>
    </el-dialog>

    <el-dialog v-model="templateImportOpen" :title="t('requirements.importTemplate')" width="760px">
      <el-alert :title="t('requirements.templateHint')" type="info" :closable="false" show-icon class="alert" />
      <input type="file" accept=".xlsx,application/vnd.openxmlformats-officedocument.spreadsheetml.sheet" @change="selectTemplateFile" />
      <template v-if="templateGroups.length">
        <div class="side-title">{{ t('requirements.templateGroups', { n: templateGroups.length }) }}</div>
        <el-table :data="templateGroups" size="small" border>
          <el-table-column prop="supplierName" :label="t('orders.supplier')" min-width="180" />
          <el-table-column prop="currency" :label="t('requirements.currency')" width="90" />
          <el-table-column prop="expectedDate" :label="t('orders.expected')" width="120" />
          <el-table-column :label="t('requirements.templateLines')" width="100"><template #default="{ row }">{{ row.lines.length }}</template></el-table-column>
          <el-table-column prop="paymentTerms" :label="t('requirements.paymentTerms')" min-width="150" />
        </el-table>
      </template>
      <template #footer>
        <el-button @click="templateImportOpen = false">{{ common('cancel') }}</el-button>
        <el-button v-if="!templateGroups.length" type="primary" :disabled="!templateFile" :loading="saving" @click="previewTemplateImport">{{ t('requirements.previewTemplate') }}</el-button>
        <el-button v-else type="primary" :loading="saving" @click="confirmTemplateImport">{{ t('requirements.createTemplateOrders', { n: templateGroups.length }) }}</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup lang="ts">
import { onMounted, onUnmounted, reactive, ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { useI18n } from 'vue-i18n'
import { useRouter } from 'vue-router'
import { get, post, postDownload, saveBlob } from '../api'
import { onLive } from '../live'
import { useAuthStore } from '../stores/auth'

interface Requirement {
  id: string
  contractId: string
  contractNo: string
  versionNo: number
  customerName: string
  productCode: string
  productName: string
  spec: string
  uomCode: string
  requiredQty: string
  orderedQty: string
  requiredDate: string
  source: string
  status: string
  ownerName: string
  closedReason: string
  receivedQty: string
}

interface CoveringOrder {
  poId: string
  poNo: string
  supplierName: string
  status: string
  expectedDate: string
  qty: string
  receivedQty: string
}

const { t } = useI18n()
const auth = useAuthStore()
const router = useRouter()
const canWrite = auth.can('procurement:requirement:write')
const canException = auth.can('procurement:requirement:exception')
// Raising a requirement and committing money to a supplier are separate
// permissions, so the ordering actions are gated separately too.
const canOrder = auth.can('procurement:order:write')

const rows = ref<Requirement[]>([])
const total = ref(0)
const page = ref(1)
const pageSize = 20
// Outstanding work is what a buyer opens this page for; everything else is
// history they go looking for deliberately.
const status = ref('PENDING')
const keyword = ref('')
const loading = ref(false)
const saving = ref(false)
const closeOpen = ref(false)
const createOpen = ref(false)
const products = ref<{ id: string; code: string; name: string; uomId: string; uomCode: string }[]>([])
const createForm = reactive({ productId: 0, qty: '', requiredDate: '', remark: '' })
const closing = ref<Requirement | null>(null)
const closeReason = ref('')
const selected = ref<Requirement[]>([])
const detailOpen = ref(false)
const detail = ref<Requirement | null>(null)
const covering = ref<CoveringOrder[]>([])
interface TemplateLine { rowNo: number; requirementId: string; productName: string; qty: string; uomCode: string; unitPrice: string; moq: string }
interface TemplateGroup { importToken: string; supplierId: string; supplierCode: string; supplierName: string; currency: string; expectedDate: string; paymentTerms: string; lines: TemplateLine[] }
const templateImportOpen = ref(false)
const templateFile = ref<File | null>(null)
const templateGroups = ref<TemplateGroup[]>([])

const common = (k: string) => t(`common.${k}`)

async function load() {
  loading.value = true
  try {
    const data = await get<{ requirements: Requirement[]; meta: { total: number } }>(
      '/requirements',
      { page: page.value, page_size: pageSize, status: status.value, keyword: keyword.value },
    )
    rows.value = data.requirements ?? []
    total.value = Number(data.meta?.total ?? 0)
  } finally {
    loading.value = false
  }
}

function reload() {
  page.value = 1
  load()
}

// This is an exceptional requirement raised independently of a contract.
async function openCreate() {
  createForm.productId = 0
  createForm.qty = ''
  createForm.requiredDate = ''
  createForm.remark = ''
  if (!products.value.length) {
    products.value = (await get<{ products: typeof products.value }>('/products', { page_size: 200 })).products ?? []
  }
  createOpen.value = true
}

async function submitCreate() {
  const product = products.value.find((p) => Number(p.id) === createForm.productId)
  if (!product || !createForm.qty) {
    ElMessage.warning(t('requirements.createRequired'))
    return
  }
  saving.value = true
  try {
    await post('/requirements', {
      product_id: Number(product.id),
      sku_id: 0,
      product_code: product.code,
      product_name: product.name,
      uom_id: Number(product.uomId ?? 0),
      uom_code: product.uomCode ?? '',
      required_qty: createForm.qty,
      required_date: createForm.requiredDate,
      remark: createForm.remark,
    })
    ElMessage.success(t('requirements.created'))
    createOpen.value = false
    status.value = 'PENDING'
    reload()
  } finally {
    saving.value = false
  }
}

function openClose(row: Requirement) {
  closing.value = row
  closeReason.value = ''
  closeOpen.value = true
}

async function confirmClose() {
  if (!closing.value) return
  if (!closeReason.value.trim()) {
    ElMessage.warning(t('requirements.reasonRequired'))
    return
  }
  saving.value = true
  try {
    await post(`/requirements/${closing.value.id}/cancel`, { reason: closeReason.value })
    ElMessage.success(t('requirements.closed'))
    closeOpen.value = false
    load()
  } finally {
    saving.value = false
  }
}

// Only lines with something still unbought can go onto an order.
function isOrderable(row: Requirement): boolean {
  if (row.status !== 'PENDING' && row.status !== 'PARTIALLY_ORDERED') return false
  return Number(row.requiredQty ?? 0) - Number(row.orderedQty ?? 0) > 0
}

function canReopen(row: Requirement): boolean {
  return (row.status === 'CANCELLED' || row.status === 'SUPERSEDED')
    && Number(row.orderedQty ?? 0) === 0
}

function onSelect(rows: Requirement[]) {
  selected.value = rows
}

// The order itself is raised on the purchase-order page — one dialog, not two
// that can drift apart. This carries the picked lines across so the buyer does
// not have to find them again by product name.
function goOrder(rows?: Requirement[]) {
  const picked = rows ?? selected.value
  router.push({ path: '/purchase-orders', query: { requirements: picked.map((r) => r.id).join(',') } })
}

async function exportTemplate() {
  saving.value = true
  try {
    const file = await postDownload('/requirements/purchase-template/export', { requirement_ids: selected.value.map((row) => Number(row.id)) })
    saveBlob(file.blob, file.fileName || 'purchase-import.xlsx')
  } finally { saving.value = false }
}

function openTemplateImport() {
  templateFile.value = null
  templateGroups.value = []
  templateImportOpen.value = true
}

function selectTemplateFile(event: Event) {
  templateFile.value = (event.target as HTMLInputElement).files?.[0] ?? null
  templateGroups.value = []
}

async function fileBase64(file: File) {
  const bytes = new Uint8Array(await file.arrayBuffer())
  let binary = ''
  for (let i = 0; i < bytes.length; i += 0x8000) binary += String.fromCharCode(...bytes.subarray(i, i + 0x8000))
  return btoa(binary)
}

function base64Blob(value: string): Blob {
  const binary = atob(value)
  const bytes = new Uint8Array(binary.length)
  for (let i = 0; i < binary.length; i++) bytes[i] = binary.charCodeAt(i)
  return new Blob([bytes], { type: 'application/vnd.openxmlformats-officedocument.spreadsheetml.sheet' })
}

async function previewTemplateImport() {
  if (!templateFile.value) return
  if (templateFile.value.size > 2 * 1024 * 1024) { ElMessage.warning(t('requirements.templateTooLarge')); return }
  saving.value = true
  try {
    const result = await post<{ groups: TemplateGroup[]; errorFileName?: string; errorFileData?: string }>('/purchase-orders/template-imports/preview', { file_data: await fileBase64(templateFile.value), source_file_name: templateFile.value.name })
    if (result.errorFileData) {
      saveBlob(base64Blob(result.errorFileData), result.errorFileName || 'purchase-import-errors.xlsx')
      ElMessage.warning(t('requirements.templateHasErrors'))
      return
    }
    templateGroups.value = result.groups ?? []
    if (!templateGroups.value.length) ElMessage.warning(t('requirements.templateNoGroups'))
  } finally { saving.value = false }
}

async function confirmTemplateImport() {
  saving.value = true
  try {
    const created: { id: string; poNo: string }[] = []
    for (const group of templateGroups.value) {
      const moq = group.lines.filter((line) => line.moq).map((line) => `${line.productName}: MOQ ${line.moq}`).join('; ')
      const remark = [t('requirements.templateImportRemark'), group.paymentTerms ? `${t('requirements.paymentTerms')}: ${group.paymentTerms}` : '', moq].filter(Boolean).join('; ')
      created.push(await post<{ id: string; poNo: string }>(`/purchase-orders/imports/${group.importToken}/confirm`, {
        supplier_id: Number(group.supplierId), currency: group.currency, expected_date: group.expectedDate, remark,
        lines: group.lines.map((line) => ({ row_no: line.rowNo, requirement_id: Number(line.requirementId), qty: line.qty, unit_price: line.unitPrice || '0' })),
      }))
    }
    ElMessage.success(t('requirements.templateOrdersCreated', { n: created.length }))
    templateImportOpen.value = false
    if (created.length === 1) router.push({ path: '/purchase-orders', query: { order: created[0].id } })
    else router.push('/purchase-orders')
  } finally { saving.value = false }
}

async function openDetail(row: Requirement) {
  detail.value = row
  covering.value = []
  detailOpen.value = true
  if (canOrder || auth.can('procurement:order:read')) {
    covering.value = (await get<{ orders: CoveringOrder[] }>(`/requirements/${row.id}/orders`)).orders ?? []
  }
}

async function reopen(row: Requirement) {
  await ElMessageBox.confirm(t('requirements.reopenWarning'), t('requirements.reopen'), {
    type: 'warning',
    confirmButtonText: t('requirements.reopen'),
    cancelButtonText: t('common.cancel'),
  })
  await post(`/requirements/${row.id}/reopen`, {})
  ElMessage.success(t('requirements.reopened'))
  load()
}

function statusType(s: string): 'warning' | 'primary' | 'success' | 'info' {
  if (s === 'PENDING') return 'warning'
  if (s === 'PARTIALLY_ORDERED') return 'primary'
  if (s === 'ORDERED') return 'success'
  if (s === 'RECEIVED') return 'success'
  return 'info'
}

// Quantities arrive as exact decimals; "1500.0000 PCS" reads worse than
// "1500 PCS" and means the same thing.
function trimQty(v: string): string {
  if (!v.includes('.')) return v
  return v.replace(/0+$/, '').replace(/\.$/, '')
}

function isOverdue(row: Requirement): boolean {
  if (!row.requiredDate || row.status !== 'PENDING') return false
  return row.requiredDate < new Date().toISOString().slice(0, 10)
}

// Requirements move for reasons nobody on this page did: a contract change
// retires a line or an order is raised.
// A buyer working from a list that went stale minutes ago orders the wrong
// things, so the page re-reads rather than waiting for a manual refresh.
//
// The re-read goes through the normal API instead of trusting the event, so
// permissions are checked exactly as they are on first load.
const stopListening = onLive((event) => {
  if (event.type !== 'requirement.changed') return
  // Not while a dialog is open: swapping the numbers under somebody who is
  // halfway through filling in a form is worse than showing them stale ones.
  if (createOpen.value || closeOpen.value || detailOpen.value || templateImportOpen.value) return
  load()
})
onUnmounted(stopListening)

onMounted(load)
</script>

<style scoped>
.page-head {
  display: flex;
  align-items: baseline;
  gap: 14px;
  margin-bottom: 16px;
}
.page-head h2 {
  margin: 0;
  font-size: 20px;
}
.grow {
  flex: 1;
}
.head-note,
.sub {
  font-size: 12px;
  color: var(--el-text-color-secondary);
}
.tabs {
  margin-bottom: 14px;
}
.filters {
  display: flex;
  gap: 10px;
  margin-bottom: 14px;
}
.prod {
  font-weight: 500;
}
.qty {
  font-variant-numeric: tabular-nums;
  font-weight: 600;
}
.ver {
  margin-left: 6px;
  font-size: 12px;
  color: var(--el-text-color-secondary);
}
.reason {
  margin-top: 2px;
  line-height: 1.4;
}
.overdue {
  color: var(--el-color-danger);
  font-weight: 600;
}
.doc-link {
  color: var(--el-color-primary);
  text-decoration: none;
}
.doc-link:hover {
  text-decoration: underline;
}
.alert {
  margin-bottom: 16px;
}
.close-form {
  margin-top: 4px;
}
.desc {
  margin-bottom: 16px;
}
.side-title {
  margin: 4px 0 8px;
  font-size: 13px;
  font-weight: 600;
  color: var(--el-text-color-regular);
}
.pager {
  margin-top: 14px;
  justify-content: flex-end;
}
</style>
