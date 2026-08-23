<template>
  <div>
    <div class="page-head">
      <h2>{{ t('receipts.title') }}</h2>
      <span class="head-note">{{ t('receipts.subtitle') }}</span>
      <span class="grow" />
      <el-button v-if="canWrite" @click="accountsOpen = true">{{ t('receipts.accounts') }}</el-button>
      <el-button v-if="canWrite" type="primary" @click="openRecord">{{ t('receipts.record') }}</el-button>
    </div>

    <el-card shadow="never">
      <el-radio-group v-model="disposition" class="tabs" @change="reload">
        <el-radio-button value="UNPROCESSED">{{ t('receipts.dispositions.UNPROCESSED') }}</el-radio-button>
        <el-radio-button value="ALLOCATED">{{ t('receipts.dispositions.ALLOCATED') }}</el-radio-button>
        <el-radio-button value="IRRELEVANT">{{ t('receipts.dispositions.IRRELEVANT') }}</el-radio-button>
        <el-radio-button value="">{{ t('receipts.allDispositions') }}</el-radio-button>
      </el-radio-group>

      <div class="filters">
        <el-input
          v-model="keyword"
          :placeholder="t('receipts.searchPlaceholder')"
          clearable
          style="width: 300px"
          @keyup.enter="reload"
          @clear="reload"
        />
        <el-button @click="reload">{{ common('query') }}</el-button>
      </div>

      <el-table :data="rows" v-loading="loading">
        <el-table-column :label="t('receipts.bankRef')" width="160">
          <template #default="{ row }">
            <div class="prod">{{ row.bankRef }}</div>
            <div class="sub">{{ row.valueDate }}</div>
          </template>
        </el-table-column>
        <el-table-column :label="t('receipts.counterparty')" min-width="180">
          <template #default="{ row }">
            <div class="ellipsis" :title="row.counterparty">{{ row.counterparty || '—' }}</div>
            <div class="sub ellipsis" :title="row.remittanceInfo">{{ row.remittanceInfo }}</div>
          </template>
        </el-table-column>
        <el-table-column :label="t('receipts.amount')" width="150" align="right">
          <template #default="{ row }">
            <span class="num money" :class="{ out: row.direction === 'DEBIT' }">
              {{ row.direction === 'DEBIT' ? '−' : '' }}{{ row.currency }} {{ row.amount }}
            </span>
          </template>
        </el-table-column>
        <!-- Unallocated is the number this page exists for: it is what is
             still somebody's money and nobody has said whose. -->
        <el-table-column :label="t('receipts.unallocated')" width="130" align="right">
          <template #default="{ row }">
            <span v-if="Number(row.unallocatedAmount) > 0" class="num warn">{{ row.unallocatedAmount }}</span>
            <span v-else class="num dim">0.00</span>
            <div v-if="Number(row.allocatedAmount) > 0" class="sub">
              {{ t('receipts.allocatedSo', { n: row.allocatedAmount }) }}
            </div>
          </template>
        </el-table-column>
        <el-table-column :label="common('status')" width="130">
          <template #default="{ row }">
            <el-tag size="small" :type="dispoType(row.disposition)" effect="plain">
              {{ t(`receipts.dispositions.${row.disposition}`) }}
            </el-tag>
            <div v-if="row.irrelevantType" class="sub">
              {{ t(`receipts.irrelevantTypes.${row.irrelevantType}`) }}
            </div>
          </template>
        </el-table-column>
        <el-table-column :label="common('actions')" width="150" fixed="right">
          <template #default="{ row }">
            <el-button link type="primary" @click="openMatch(row)">
              {{ actionLabel(row) }}
            </el-button>
            <el-button
              v-if="canWrite && row.disposition === 'IRRELEVANT'"
              link
              type="warning"
              @click="reopen(row)"
            >
              {{ t('receipts.reopen') }}
            </el-button>
          </template>
        </el-table-column>
        <template #empty>{{ t('receipts.empty') }}</template>
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

    <el-dialog v-model="matchOpen" :title="detail?.bankRef" width="900px" top="5vh">
      <template v-if="detail">
        <div class="tx-head">
          <span class="num money big">{{ detail.currency }} {{ detail.amount }}</span>
          <span class="sub">{{ detail.valueDate }} · {{ detail.accountName }}</span>
        </div>
        <table class="tx-info">
          <tr><td>{{ t('receipts.counterparty') }}</td><td>{{ detail.counterparty || '—' }}</td></tr>
          <tr><td>{{ t('receipts.remittance') }}</td><td>{{ detail.remittanceInfo || '—' }}</td></tr>
        </table>

        <el-alert v-if="suggestions.length" type="info" :closable="false" show-icon class="alert">
          <template #default>
            <div class="suggest">
              <span>{{ t('receipts.suggestHint', { n: suggestions.length }) }}</span>
              <el-button size="small" @click="adoptSuggestions">{{ t('receipts.adopt') }}</el-button>
            </div>
          </template>
        </el-alert>

        <template v-if="liveAllocations.length">
          <div class="side-title">{{ t('receipts.settled') }}</div>
          <el-table :data="liveAllocations" size="small">
            <el-table-column :label="t('receipts.contract')" min-width="180">
              <template #default="{ row }">
                <div>{{ row.contractNo }}</div>
                <div class="sub">{{ row.customerName }}</div>
              </template>
            </el-table-column>
            <el-table-column :label="t('receipts.settledAmount')" width="120" align="right">
              <template #default="{ row }"><span class="num">{{ row.amount }}</span></template>
            </el-table-column>
            <el-table-column :label="t('receipts.fee')" width="90" align="right">
              <template #default="{ row }"><span class="num dim">{{ row.feeAmount }}</span></template>
            </el-table-column>
            <el-table-column :label="t('receipts.by')" width="90">
              <template #default="{ row }"><span class="sub">{{ row.allocatedByName }}</span></template>
            </el-table-column>
            <el-table-column width="80" align="center">
              <template #default="{ row }">
                <el-button v-if="canWrite" link type="danger" @click="openReverse(row)">
                  {{ t('receipts.reverse') }}
                </el-button>
              </template>
            </el-table-column>
          </el-table>
        </template>

        <template v-if="reversedAllocations.length">
          <div class="side-title">{{ t('receipts.reversedHistory') }}</div>
          <div v-for="r in reversedAllocations" :key="r.id" class="reversed">
            {{ r.contractNo }} · {{ r.amount }} · {{ r.reverseReason }}
            <span class="sub">{{ r.allocatedByName }}</span>
          </div>
        </template>

        <!-- Money we paid out has no receivable to settle. The server
             refuses it; offering the form anyway would just be a trap. -->
        <el-alert v-if="detail.direction === 'DEBIT'" type="warning" :closable="false" show-icon class="alert">
          {{ t('receipts.debitHint') }}
        </el-alert>

        <template v-if="canWrite && detail.disposition !== 'IRRELEVANT' && detail.direction === 'CREDIT'">
          <div class="side-title">
            {{ t('receipts.newAllocation') }}
            <el-button link type="primary" @click="addRow">{{ t('receipts.addContract') }}</el-button>
          </div>
          <el-table :data="draft" size="small">
            <el-table-column :label="t('receipts.contract')" min-width="230">
              <template #default="{ row }">
                <el-select
                  v-model="row.contractId"
                  filterable
                  remote
                  :remote-method="searchReceivables"
                  :loading="searching"
                  style="width: 100%"
                  :placeholder="t('receipts.pickContract')"
                  @change="() => onPick(row)"
                >
                  <el-option
                    v-for="c in receivables"
                    :key="c.contractId"
                    :value="Number(c.contractId)"
                    :label="`${c.contractNo} · ${c.customerName}`"
                  />
                </el-select>
              </template>
            </el-table-column>
            <el-table-column :label="t('receipts.openAmount')" width="110" align="right">
              <template #default="{ row }">
                <span class="num dim">{{ openOf(row.contractId) }}</span>
              </template>
            </el-table-column>
            <el-table-column :label="t('receipts.thisTime')" width="130">
              <template #default="{ row }"><el-input v-model="row.amount" size="small" /></template>
            </el-table-column>
            <el-table-column :label="t('receipts.fee')" width="110">
              <template #default="{ row }"><el-input v-model="row.fee" size="small" /></template>
            </el-table-column>
            <el-table-column width="60" align="center">
              <template #default="{ $index }">
                <el-button link type="danger" @click="draft.splice($index, 1)">
                  {{ common('delete') }}
                </el-button>
              </template>
            </el-table-column>
            <template #empty>{{ t('receipts.draftEmpty') }}</template>
          </el-table>
        </template>

        <div v-if="detail.direction === 'CREDIT'" class="totals">
          <div><span class="sub">{{ t('receipts.amount') }}</span><span class="num">{{ detail.amount }}</span></div>
          <div><span class="sub">{{ t('receipts.settledTotal') }}</span><span class="num">{{ settledTotal }}</span></div>
          <div><span class="sub">{{ t('receipts.draftTotal') }}</span><span class="num">{{ draftTotal }}</span></div>
          <div>
            <span class="sub">{{ t('receipts.unallocated') }}</span>
            <span class="num" :class="remaining === '0.00' ? 'ok' : 'warn'">{{ remaining }}</span>
          </div>
        </div>
      </template>

      <template #footer>
        <el-button
          v-if="canWrite && detail?.disposition !== 'IRRELEVANT'"
          @click="openIrrelevant"
        >
          {{ t('receipts.markIrrelevant') }}
        </el-button>
        <el-button @click="matchOpen = false">{{ common('cancel') }}</el-button>
        <el-button
          v-if="canWrite && detail?.disposition !== 'IRRELEVANT' && detail?.direction === 'CREDIT'"
          type="primary"
          :loading="saving"
          @click="submitAllocation"
        >
          {{ t('receipts.confirm') }}
        </el-button>
      </template>
    </el-dialog>

    <el-dialog v-model="recordOpen" :title="t('receipts.record')" width="640px">
      <el-alert type="info" :closable="false" show-icon class="alert">{{ t('receipts.recordHint') }}</el-alert>
      <el-form :model="form" label-width="100px">
        <el-form-item :label="t('receipts.account')" required>
          <el-select v-model="form.accountId" style="width: 100%" :placeholder="t('receipts.pickAccount')">
            <el-option
              v-for="a in accounts"
              :key="a.id"
              :value="Number(a.id)"
              :label="`${a.accountName} · ${a.accountNo} (${a.currency})`"
            />
          </el-select>
        </el-form-item>
        <el-form-item :label="t('receipts.bankRef')" required>
          <el-input v-model="form.bankRef" :placeholder="t('receipts.bankRefPlaceholder')" />
        </el-form-item>
        <el-form-item :label="t('receipts.direction')">
          <el-radio-group v-model="form.direction">
            <el-radio-button value="CREDIT">{{ t('receipts.credit') }}</el-radio-button>
            <el-radio-button value="DEBIT">{{ t('receipts.debit') }}</el-radio-button>
          </el-radio-group>
        </el-form-item>
        <el-form-item :label="t('receipts.amount')" required>
          <el-input v-model="form.amount" style="width: 180px" />
          <el-select v-model="form.currency" style="width: 110px; margin-left: 12px">
            <el-option v-for="c in CURRENCIES" :key="c" :value="c" :label="c" />
          </el-select>
        </el-form-item>
        <el-form-item :label="t('receipts.valueDate')" required>
          <el-date-picker v-model="form.valueDate" type="date" value-format="YYYY-MM-DD" style="width: 100%" />
        </el-form-item>
        <el-form-item :label="t('receipts.counterparty')">
          <el-input v-model="form.counterparty" :placeholder="t('receipts.counterpartyPlaceholder')" />
        </el-form-item>
        <el-form-item :label="t('receipts.remittance')">
          <el-input v-model="form.remittanceInfo" type="textarea" :rows="2" :placeholder="t('receipts.remittancePlaceholder')" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="recordOpen = false">{{ common('cancel') }}</el-button>
        <el-button type="primary" :loading="saving" @click="submitRecord">{{ common('save') }}</el-button>
      </template>
    </el-dialog>

    <el-dialog v-model="accountsOpen" :title="t('receipts.accounts')" width="640px">
      <el-alert type="info" :closable="false" show-icon class="alert">{{ t('receipts.accountsHint') }}</el-alert>
      <el-table :data="accounts" size="small">
        <el-table-column :label="t('receipts.accountName')" prop="accountName" min-width="180" />
        <el-table-column :label="t('receipts.accountNo')" prop="accountNo" min-width="150" />
        <el-table-column :label="t('receipts.bank')" prop="bankName" min-width="130" />
        <el-table-column :label="t('receipts.currency')" prop="currency" width="80" />
      </el-table>
      <el-form :model="accountForm" label-width="90px" class="acct-form">
        <el-form-item :label="t('receipts.accountName')">
          <el-input v-model="accountForm.accountName" />
        </el-form-item>
        <el-form-item :label="t('receipts.accountNo')">
          <el-input v-model="accountForm.accountNo" />
        </el-form-item>
        <el-form-item :label="t('receipts.bank')">
          <el-input v-model="accountForm.bankName" style="width: 240px" />
          <el-select v-model="accountForm.currency" style="width: 110px; margin-left: 12px">
            <el-option v-for="c in CURRENCIES" :key="c" :value="c" :label="c" />
          </el-select>
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="accountsOpen = false">{{ common('cancel') }}</el-button>
        <el-button type="primary" :loading="saving" @click="submitAccount">{{ t('receipts.addAccount') }}</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { useI18n } from 'vue-i18n'
import { get, post } from '../api'
import { CURRENCIES } from '../constants'
import { useAuthStore } from '../stores/auth'

interface Transaction {
  id: string
  bankRef: string
  direction: string
  amount: string
  currency: string
  valueDate: string
  counterparty: string
  remittanceInfo: string
  source: string
  disposition: string
  irrelevantType: string
  note: string
  accountName: string
  allocatedAmount: string
  unallocatedAmount: string
  feeAmount: string
}
interface Allocation {
  id: string
  contractId: string
  contractNo: string
  customerName: string
  amount: string
  feeAmount: string
  currency: string
  reversalOf: string
  reverseReason: string
  allocatedByName: string
}
interface Suggestion { contractId: string; contractNo: string; customerName: string; currency: string; openAmount: string }
interface Receivable { contractId: string; contractNo: string; customerName: string; currency: string; openAmount: string }
interface Account { id: string; accountNo: string; accountName: string; bankName: string; currency: string }
interface DraftRow { contractId?: number; amount: string; fee: string }


const { t } = useI18n()
const auth = useAuthStore()
const canWrite = auth.can('export:receipt:write')

const rows = ref<Transaction[]>([])
const total = ref(0)
const page = ref(1)
const pageSize = 20
const disposition = ref('UNPROCESSED')
const keyword = ref('')
const loading = ref(false)
const saving = ref(false)
const searching = ref(false)

const matchOpen = ref(false)
const detail = ref<Transaction | null>(null)
const allocations = ref<Allocation[]>([])
const suggestions = ref<Suggestion[]>([])
const receivables = ref<Receivable[]>([])
const draft = ref<DraftRow[]>([])

const recordOpen = ref(false)
const accounts = ref<Account[]>([])
const form = reactive({
  accountId: undefined as number | undefined,
  bankRef: '', direction: 'CREDIT', amount: '', currency: 'USD',
  valueDate: '', counterparty: '', remittanceInfo: '',
})

const accountsOpen = ref(false)
const accountForm = reactive({ accountNo: '', accountName: '', bankName: '', currency: 'USD' })

const common = (k: string) => t(`common.${k}`)

// A reversal cancels exactly one row, so an allocation is live when nothing
// points at it. Summing would give the same money but not the same list — the
// operator needs to see which line to reverse, not just the net.
const reversedIds = computed(
  () => new Set(allocations.value.filter((a) => a.reversalOf !== '0').map((a) => a.reversalOf)),
)
const liveAllocations = computed(
  () => allocations.value.filter((a) => a.reversalOf === '0' && !reversedIds.value.has(a.id)),
)
const reversedAllocations = computed(() => allocations.value.filter((a) => a.reversalOf !== '0'))

const settledTotal = computed(() =>
  liveAllocations.value.reduce((s, a) => s + Number(a.amount), 0).toFixed(2),
)
const draftTotal = computed(() =>
  draft.value.reduce((s, r) => s + Number(r.amount || 0), 0).toFixed(2),
)
const remaining = computed(() =>
  (Number(detail.value?.amount ?? 0) - Number(settledTotal.value) - Number(draftTotal.value)).toFixed(2),
)

// A debit is filed, not matched: there is no receivable on the other side of
// money we sent out.
function actionLabel(row: Transaction): string {
  if (!canWrite || row.disposition === 'IRRELEVANT') return common('detail')
  return row.direction === 'DEBIT' ? t('receipts.file') : t('receipts.match')
}

function dispoType(d: string): 'warning' | 'success' | 'info' {
  if (d === 'ALLOCATED') return 'success'
  if (d === 'IRRELEVANT') return 'info'
  return 'warning'
}

function openOf(contractId?: number): string {
  if (!contractId) return '—'
  return receivables.value.find((r) => Number(r.contractId) === contractId)?.openAmount ?? '—'
}

async function load() {
  loading.value = true
  try {
    const d = await get<{ transactions: Transaction[]; total: string }>('/bank-transactions', {
      page: page.value, page_size: pageSize,
      disposition: disposition.value, keyword: keyword.value,
    })
    rows.value = d.transactions ?? []
    total.value = Number(d.total ?? 0)
  } finally {
    loading.value = false
  }
}

function reload() {
  page.value = 1
  load()
}

async function loadAccounts() {
  accounts.value = (await get<{ accounts: Account[] }>('/bank-accounts')).accounts ?? []
}

async function openRecord() {
  await loadAccounts()
  form.accountId = accounts.value.length ? Number(accounts.value[0].id) : undefined
  form.bankRef = ''
  form.direction = 'CREDIT'
  form.amount = ''
  form.valueDate = ''
  form.counterparty = ''
  form.remittanceInfo = ''
  recordOpen.value = true
}

async function submitRecord() {
  saving.value = true
  try {
    await post('/bank-transactions', {
      transaction: {
        account_id: form.accountId, bank_ref: form.bankRef, direction: form.direction,
        amount: form.amount, currency: form.currency, value_date: form.valueDate,
        counterparty: form.counterparty, remittance_info: form.remittanceInfo,
        source: 'MANUAL',
      },
    })
    ElMessage.success(t('receipts.recorded'))
    recordOpen.value = false
    reload()
  } finally {
    saving.value = false
  }
}

async function submitAccount() {
  saving.value = true
  try {
    await post('/bank-accounts', {
      account_no: accountForm.accountNo, account_name: accountForm.accountName,
      bank_name: accountForm.bankName, currency: accountForm.currency,
    })
    accountForm.accountNo = ''
    accountForm.accountName = ''
    accountForm.bankName = ''
    await loadAccounts()
    ElMessage.success(t('receipts.accountAdded'))
  } finally {
    saving.value = false
  }
}

async function openMatch(row: Transaction) {
  draft.value = []
  const d = await get<{ transaction: Transaction; allocations: Allocation[]; suggestions: Suggestion[] }>(
    `/bank-transactions/${row.id}`,
  )
  detail.value = d.transaction
  allocations.value = d.allocations ?? []
  suggestions.value = d.suggestions ?? []
  // Only same-currency contracts can be settled, so the picker never offers
  // one the server would refuse.
  await searchReceivables('')
  matchOpen.value = true
}

async function searchReceivables(query: string) {
  if (!detail.value) return
  searching.value = true
  try {
    receivables.value = (await get<{ receivables: Receivable[] }>('/open-receivables', {
      currency: detail.value.currency, keyword: query,
    })).receivables ?? []
  } finally {
    searching.value = false
  }
}

function addRow() {
  draft.value.push({ contractId: undefined, amount: '', fee: '' })
}

// Fill the amount with whatever is smaller: what the contract still owes, or
// what is left of the payment. Guessing high would only produce a refusal.
function onPick(row: DraftRow) {
  const open = Number(openOf(row.contractId))
  if (!Number.isFinite(open)) return
  const left = Number(detail.value?.amount ?? 0) - Number(settledTotal.value) - Number(draftTotal.value)
  row.amount = Math.max(0, Math.min(open, left + Number(row.amount || 0))).toFixed(2)
}

function adoptSuggestions() {
  for (const s of suggestions.value) {
    const id = Number(s.contractId)
    if (draft.value.some((r) => r.contractId === id)) continue
    if (liveAllocations.value.some((a) => Number(a.contractId) === id)) continue
    if (!receivables.value.some((r) => Number(r.contractId) === id)) {
      receivables.value.push({ ...s })
    }
    draft.value.push({ contractId: id, amount: '', fee: '' })
    // Read the row back out of the array before filling it in. Pushing stores
    // the raw object; only the proxy that comes back out reports writes to
    // Vue, so mutating the local literal would set the value and never
    // re-render the field.
    onPick(draft.value[draft.value.length - 1])
  }
}

async function submitAllocation() {
  const lines = draft.value
    .filter((r) => r.contractId && Number(r.amount) > 0)
    .map((r) => ({ contract_id: r.contractId, amount: r.amount, fee_amount: r.fee || '0' }))
  if (!lines.length) {
    ElMessage.warning(t('receipts.pickSomething'))
    return
  }
  saving.value = true
  try {
    const d = await post<{ transaction: Transaction; allocations: Allocation[] }>(
      `/bank-transactions/${detail.value!.id}/allocate`,
      { allocations: lines },
    )
    detail.value = d.transaction
    allocations.value = d.allocations ?? []
    draft.value = []
    ElMessage.success(t('receipts.allocated'))
    load()
  } finally {
    saving.value = false
  }
}

async function openReverse(row: Allocation) {
  const { value } = await ElMessageBox.prompt(
    t('receipts.reverseHint', { no: row.contractNo, amount: row.amount }),
    t('receipts.reverse'),
    {
      confirmButtonText: t('receipts.reverse'),
      cancelButtonText: common('cancel'),
      inputPlaceholder: t('receipts.reverseReasonPlaceholder'),
      inputValidator: (v: string) => (v.trim() ? true : t('receipts.reverseReasonRequired')),
    },
  )
  const d = await post<{ transaction: Transaction; allocations: Allocation[] }>(
    `/receipt-allocations/${row.id}/reverse`,
    { reason: value },
  )
  detail.value = d.transaction
  allocations.value = d.allocations ?? []
  ElMessage.success(t('receipts.reversed'))
  load()
}

async function openIrrelevant() {
  const kinds = ['TAX_REFUND', 'INTEREST', 'INTERNAL', 'SUPPLIER_REFUND', 'DEPOSIT_RETURN', 'OTHER']
  const { value } = await ElMessageBox.prompt(
    t('receipts.irrelevantHint'),
    t('receipts.markIrrelevant'),
    {
      confirmButtonText: common('confirm'),
      cancelButtonText: common('cancel'),
      inputPlaceholder: kinds.map((k) => t(`receipts.irrelevantTypes.${k}`)).join(' / '),
      inputValidator: (v: string) =>
        kinds.some((k) => t(`receipts.irrelevantTypes.${k}`) === v.trim())
          ? true
          : t('receipts.irrelevantTypeRequired'),
    },
  )
  const kind = kinds.find((k) => t(`receipts.irrelevantTypes.${k}`) === value.trim())!
  const d = await post<{ transaction: Transaction }>(
    `/bank-transactions/${detail.value!.id}/irrelevant`,
    { irrelevant_type: kind },
  )
  detail.value = d.transaction
  ElMessage.success(t('receipts.markedIrrelevant'))
  matchOpen.value = false
  load()
}

async function reopen(row: Transaction) {
  await post(`/bank-transactions/${row.id}/reopen`, {})
  ElMessage.success(t('receipts.reopened'))
  load()
}

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
.num {
  font-variant-numeric: tabular-nums;
}
.money {
  font-weight: 600;
}
.big {
  font-size: 20px;
}
.dim {
  color: var(--el-text-color-placeholder);
}
.warn {
  color: var(--el-color-warning);
  font-weight: 600;
}
.ok {
  color: var(--el-color-success);
  font-weight: 600;
}
.out {
  color: var(--el-text-color-secondary);
}
.ellipsis {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.alert {
  margin-bottom: 14px;
}
.tx-head {
  display: flex;
  align-items: baseline;
  gap: 12px;
  margin-bottom: 8px;
}
.tx-info {
  width: 100%;
  font-size: 13px;
  margin-bottom: 12px;
}
.tx-info td:first-child {
  width: 90px;
  color: var(--el-text-color-secondary);
  padding: 3px 0;
}
.suggest {
  display: flex;
  align-items: center;
  gap: 12px;
}
.side-title {
  display: flex;
  align-items: center;
  gap: 10px;
  margin: 14px 0 8px;
  font-size: 13px;
  font-weight: 600;
  color: var(--el-text-color-regular);
}
.reversed {
  font-size: 12px;
  color: var(--el-text-color-secondary);
  padding: 3px 0;
  text-decoration: line-through;
}
.totals {
  display: flex;
  justify-content: flex-end;
  gap: 24px;
  margin-top: 14px;
  padding-top: 12px;
  border-top: 1px solid var(--el-border-color);
}
.totals div {
  display: flex;
  flex-direction: column;
  align-items: flex-end;
  gap: 2px;
  font-size: 14px;
}
.acct-form {
  margin-top: 14px;
}
.pager {
  margin-top: 14px;
  justify-content: flex-end;
}
</style>
