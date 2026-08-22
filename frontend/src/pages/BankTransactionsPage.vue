<template>
  <div class="page">
    <header class="page-head">
      <div>
        <div class="eyebrow">{{ t('bankTransactions.eyebrow') }}</div>
        <h1>{{ t('bankTransactions.title') }}</h1>
        <p>{{ t('bankTransactions.subtitle') }}</p>
      </div>
      <div class="head-actions">
        <el-input v-model="defaultCurrency" style="width: 110px" :placeholder="t('bankTransactions.defaultCurrency')" />
        <el-button v-if="canWrite" type="primary" :loading="importing" @click="fileInput?.click()">
          {{ t('bankTransactions.import') }}
        </el-button>
        <input ref="fileInput" type="file" accept=".csv,text/csv" style="display: none" @change="onFilePicked" />
      </div>
    </header>

    <section class="panel">
      <div class="filters">
        <el-select v-model="status" clearable :placeholder="t('bankTransactions.statusAll')" style="width: 140px" @change="reload">
          <el-option value="UNMATCHED" :label="t('bankTransactions.unmatched')" />
          <el-option value="MATCHED" :label="t('bankTransactions.matched')" />
        </el-select>
        <el-select v-model="direction" clearable :placeholder="t('bankTransactions.directionAll')" style="width: 130px" @change="reload">
          <el-option value="DEBIT" :label="t('bankTransactions.debit')" />
          <el-option value="CREDIT" :label="t('bankTransactions.credit')" />
        </el-select>
        <el-input v-model="keyword" clearable :placeholder="t('bankTransactions.search')" style="max-width: 240px" @keyup.enter="reload" />
        <el-button type="primary" @click="reload">{{ t('common.query') }}</el-button>
      </div>

      <el-table v-loading="loading" :data="rows" stripe>
        <el-table-column prop="txnDate" :label="t('bankTransactions.date')" width="110" />
        <el-table-column :label="t('bankTransactions.direction')" width="90">
          <template #default="{ row }">
            <el-tag effect="plain" :type="row.direction === 'DEBIT' ? 'warning' : 'success'">
              {{ row.direction === 'DEBIT' ? t('bankTransactions.debit') : t('bankTransactions.credit') }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column :label="t('bankTransactions.amount')" width="140" align="right">
          <template #default="{ row }">{{ row.currency }} {{ row.amount }}</template>
        </el-table-column>
        <el-table-column prop="counterparty" :label="t('bankTransactions.counterparty')" min-width="150" show-overflow-tooltip>
          <template #default="{ row }">{{ row.counterparty || '—' }}</template>
        </el-table-column>
        <el-table-column prop="bankRef" :label="t('bankTransactions.bankRef')" width="170" show-overflow-tooltip />
        <el-table-column prop="remark" :label="t('bankTransactions.remark')" min-width="130" show-overflow-tooltip>
          <template #default="{ row }">{{ row.remark || '—' }}</template>
        </el-table-column>
        <el-table-column :label="t('bankTransactions.matchState')" min-width="210">
          <template #default="{ row }">
            <el-tag v-if="row.matchedPaymentNo" type="success" effect="plain">{{ row.matchedPaymentNo }}</el-tag>
            <template v-else-if="row.suggestedPaymentNo">
              <span class="suggest">{{ t('bankTransactions.suggested') }}: {{ row.suggestedPaymentNo }} · {{ row.suggestedPaymentSupplier }}</span>
              <el-button v-if="canWrite" size="small" link type="primary" @click="match(row, row.suggestedPaymentId)">
                {{ t('bankTransactions.accept') }}
              </el-button>
            </template>
            <span v-else class="none">—</span>
          </template>
        </el-table-column>
        <el-table-column :label="t('common.actions')" width="150" fixed="right">
          <template #default="{ row }">
            <template v-if="canWrite && row.direction === 'DEBIT'">
              <el-button v-if="!row.matchedPaymentId" size="small" @click="openPick(row)">{{ t('bankTransactions.pickPayment') }}</el-button>
              <el-button v-else size="small" type="danger" link @click="unmatch(row)">{{ t('bankTransactions.unmatch') }}</el-button>
            </template>
          </template>
        </el-table-column>
        <template #empty>{{ t('bankTransactions.empty') }}</template>
      </el-table>
      <el-pagination v-model:current-page="page" :page-size="20" :total="total" layout="total, prev, pager, next" @current-change="load" />
    </section>

    <!-- Which payment does the bank confirm? Amounts may differ (fees shave
         wires) so both numbers stay visible; currency may not. -->
    <el-dialog v-model="pickOpen" :title="t('bankTransactions.pickTitle')" width="min(680px, 94vw)" destroy-on-close>
      <p v-if="picking" class="pick-context">
        {{ picking.txnDate }} · {{ picking.currency }} {{ picking.amount }} · {{ picking.counterparty || picking.bankRef }}
      </p>
      <el-select v-model="pickedPayment" filterable style="width: 100%" :placeholder="t('bankTransactions.pickPlaceholder')">
        <el-option v-for="p in candidatePayments" :key="p.id" :value="String(p.id)"
          :label="`${p.paymentNo} · ${p.supplierName} · ${p.currency} ${p.amount} · ${p.paidAt}`" />
      </el-select>
      <template #footer>
        <el-button @click="pickOpen = false">{{ t('common.cancel') }}</el-button>
        <el-button type="primary" :disabled="!pickedPayment" :loading="matching" @click="confirmPick">{{ t('common.save') }}</el-button>
      </template>
    </el-dialog>

    <el-dialog v-model="errorsOpen" :title="t('bankTransactions.importErrors')" width="min(560px, 94vw)">
      <el-table :data="importErrors" size="small">
        <el-table-column prop="rowNo" :label="t('bankTransactions.rowNo')" width="90" />
        <el-table-column prop="reason" :label="t('bankTransactions.reason')" min-width="200" />
      </el-table>
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
  matchedPaymentId: string
  matchedPaymentNo: string
  suggestedPaymentId: string
  suggestedPaymentNo: string
  suggestedPaymentSupplier: string
}
interface PaymentOpt { id: string; paymentNo: string; supplierName: string; currency: string; amount: string; paidAt: string; bankRef: string }
interface RowError { rowNo: number; reason: string }

const rows = ref<TxnRow[]>([])
const loading = ref(false)
const page = ref(1)
const total = ref(0)
const status = ref('')
const direction = ref('')
const keyword = ref('')
const defaultCurrency = ref('')
const importing = ref(false)
const fileInput = ref<HTMLInputElement | null>(null)
const importErrors = ref<RowError[]>([])
const errorsOpen = ref(false)

const pickOpen = ref(false)
const picking = ref<TxnRow | null>(null)
const pickedPayment = ref('')
const candidatePayments = ref<PaymentOpt[]>([])
const matching = ref(false)

async function load() {
  loading.value = true
  try {
    const resp = await get<{ items: TxnRow[]; total: string }>('/bank-transactions', {
      page: page.value, page_size: 20, status: status.value, direction: direction.value, keyword: keyword.value,
    })
    rows.value = resp.items || []
    total.value = Number(resp.total || 0)
  } finally {
    loading.value = false
  }
}
function reload() { page.value = 1; void load() }

function fileBase64(file: File): Promise<string> {
  return new Promise((resolve, reject) => {
    const reader = new FileReader()
    reader.onload = () => resolve(String(reader.result).split(',')[1] || '')
    reader.onerror = reject
    reader.readAsDataURL(file)
  })
}

async function onFilePicked(e: Event) {
  const input = e.target as HTMLInputElement
  const file = input.files?.[0]
  if (!file) return
  importing.value = true
  try {
    const resp = await post<{ imported: number; duplicates: number; errors?: RowError[] }>(
      '/bank-transactions/import',
      { fileName: file.name, data: await fileBase64(file), defaultCurrency: defaultCurrency.value },
    )
    ElMessage.success(t('bankTransactions.imported', { n: resp.imported || 0, d: resp.duplicates || 0 }))
    if (resp.errors?.length) {
      importErrors.value = resp.errors
      errorsOpen.value = true
    }
    reload()
  } catch {
    /* the api layer already surfaced the server's message */
  } finally {
    importing.value = false
    input.value = ''
  }
}

async function openPick(row: TxnRow) {
  picking.value = row
  pickedPayment.value = ''
  pickOpen.value = true
  const resp = await get<{ items: PaymentOpt[] }>('/supplier-payments', { page_size: 200 })
  // Only unclaimed payments in the row's currency are candidates; the
  // amounts stay visible in the label so a fee-shaved wire is a deliberate
  // human call, not a surprise.
  candidatePayments.value = (resp.items || []).filter(
    (p) => !p.bankRef && p.currency === row.currency)
}

async function confirmPick() {
  if (!picking.value || !pickedPayment.value) return
  matching.value = true
  try {
    await match(picking.value, pickedPayment.value)
    pickOpen.value = false
  } finally {
    matching.value = false
  }
}

async function match(row: TxnRow, paymentId: string) {
  await post(`/bank-transactions/${row.id}/match`, { paymentId })
  ElMessage.success(t('bankTransactions.matchedOk'))
  void load()
}

async function unmatch(row: TxnRow) {
  await post(`/bank-transactions/${row.id}/unmatch`, {})
  ElMessage.success(t('bankTransactions.unmatchedOk'))
  void load()
}

onMounted(load)
</script>

<style scoped>
.suggest { color: var(--el-color-primary); font-size: 13px; }
.none { color: var(--el-text-color-secondary); }
.pick-context { margin: 0 0 12px; color: var(--el-text-color-secondary); }
.head-actions { display: flex; gap: 8px; align-items: center; }
</style>
