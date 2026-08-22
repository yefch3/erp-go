<template>
  <div class="page">
    <header class="page-head">
      <div>
        <div class="eyebrow">{{ t('supplierStatements.eyebrow') }}</div>
        <h1>{{ t('supplierStatements.title') }}</h1>
        <p>{{ t('supplierStatements.subtitle') }}</p>
      </div>
    </header>

    <section class="panel">
      <div class="filters">
        <el-input v-model="keyword" clearable :placeholder="t('supplierStatements.search')" style="max-width: 260px" @keyup.enter="load" />
        <el-button type="primary" @click="load">{{ t('common.query') }}</el-button>
      </div>

      <el-table v-loading="loading" :data="rows" stripe>
        <el-table-column prop="supplierName" :label="t('supplierStatements.supplier')" min-width="160" show-overflow-tooltip />
        <el-table-column prop="currency" :label="t('supplierStatements.currency')" width="90" />
        <el-table-column :label="t('supplierStatements.ordered')" width="120" align="right">
          <template #default="{ row }">{{ row.orderedAmount }}</template>
        </el-table-column>
        <el-table-column :label="t('supplierStatements.received')" width="120" align="right">
          <template #default="{ row }">{{ row.receivedAmount }}</template>
        </el-table-column>
        <el-table-column :label="t('supplierStatements.exception')" width="110" align="right">
          <template #default="{ row }"><span :class="{ warn: isPositive(row.exceptionAmount) }">{{ row.exceptionAmount }}</span></template>
        </el-table-column>
        <el-table-column :label="t('supplierStatements.invoiced')" width="120" align="right">
          <template #default="{ row }">{{ row.invoicedAmount }}</template>
        </el-table-column>
        <el-table-column :label="t('supplierStatements.paid')" width="120" align="right">
          <template #default="{ row }">{{ row.paidAmount }}</template>
        </el-table-column>
        <el-table-column :label="t('supplierStatements.advance')" width="120" align="right">
          <template #default="{ row }">{{ row.advanceAmount }}</template>
        </el-table-column>
        <el-table-column :label="t('supplierStatements.unallocated')" width="120" align="right">
          <template #default="{ row }"><span :class="{ warn: isPositive(row.unallocatedAmount) }">{{ row.unallocatedAmount }}</span></template>
        </el-table-column>
        <el-table-column :label="t('supplierStatements.balance')" width="130" align="right">
          <template #default="{ row }"><strong>{{ row.balance }}</strong></template>
        </el-table-column>
        <el-table-column :label="t('supplierStatements.overdue')" width="140" align="right">
          <template #default="{ row }">
            <span v-if="row.overdueCount > 0" class="overdue">{{ row.overdueAmount }}（{{ row.overdueCount }}）</span>
            <span v-else>—</span>
          </template>
        </el-table-column>
        <el-table-column :label="t('common.actions')" width="90" fixed="right">
          <template #default="{ row }">
            <el-button size="small" @click="openDetail(row)">{{ t('common.detail') }}</el-button>
          </template>
        </el-table-column>
        <template #empty>{{ t('supplierStatements.empty') }}</template>
      </el-table>
    </section>

    <el-dialog v-model="detailOpen" :title="detailTitle" width="min(1000px, 96vw)" destroy-on-close>
      <el-descriptions v-if="summary" :column="4" border size="small">
        <el-descriptions-item :label="t('supplierStatements.ordered')">{{ summary.orderedAmount }}</el-descriptions-item>
        <el-descriptions-item :label="t('supplierStatements.received')">{{ summary.receivedAmount }}</el-descriptions-item>
        <el-descriptions-item :label="t('supplierStatements.exception')">{{ summary.exceptionAmount }}</el-descriptions-item>
        <el-descriptions-item :label="t('supplierStatements.invoiced')">{{ summary.invoicedAmount }}</el-descriptions-item>
        <el-descriptions-item :label="t('supplierStatements.paid')">{{ summary.paidAmount }}</el-descriptions-item>
        <el-descriptions-item :label="t('supplierStatements.advance')">{{ summary.advanceAmount }}</el-descriptions-item>
        <el-descriptions-item :label="t('supplierStatements.unallocated')">{{ summary.unallocatedAmount }}</el-descriptions-item>
        <el-descriptions-item :label="t('supplierStatements.balance')"><strong>{{ summary.balance }}</strong></el-descriptions-item>
      </el-descriptions>
      <el-table :data="lines" size="small" stripe style="margin-top: 12px" v-loading="detailLoading">
        <el-table-column :label="t('supplierStatements.lineAt')" width="165">
          <template #default="{ row }">{{ (row.at || '').slice(0, 19) }}</template>
        </el-table-column>
        <el-table-column :label="t('supplierStatements.lineType')" width="120">
          <template #default="{ row }">
            <el-tag effect="plain" :type="lineTagType(row.type)">{{ t(`supplierStatements.lineTypes.${row.type}`) }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column prop="ref" :label="t('supplierStatements.lineRef')" width="150" show-overflow-tooltip />
        <el-table-column prop="against" :label="t('supplierStatements.lineAgainst')" width="150" show-overflow-tooltip>
          <template #default="{ row }">{{ row.against || '—' }}</template>
        </el-table-column>
        <el-table-column :label="t('supplierStatements.lineAmount')" width="120" align="right">
          <template #default="{ row }">{{ row.amount }}</template>
        </el-table-column>
        <el-table-column :label="t('supplierStatements.lineBalance')" width="130" align="right">
          <template #default="{ row }"><strong>{{ row.balance }}</strong></template>
        </el-table-column>
        <el-table-column prop="note" :label="t('supplierStatements.lineNote')" min-width="140" show-overflow-tooltip>
          <template #default="{ row }">{{ row.note || '—' }}</template>
        </el-table-column>
      </el-table>
    </el-dialog>
  </div>
</template>

<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { get } from '../api'

const { t } = useI18n()

interface StatementRow {
  supplierId: string
  supplierName: string
  currency: string
  orderedAmount: string
  receivedAmount: string
  exceptionAmount: string
  invoicedAmount: string
  paidAmount: string
  advanceAmount: string
  unallocatedAmount: string
  balance: string
  overdueCount: number
  overdueAmount: string
}
interface LedgerLine {
  at: string
  type: string
  ref: string
  against: string
  amount: string
  balance: string
  note: string
}

const rows = ref<StatementRow[]>([])
const loading = ref(false)
const keyword = ref('')
const detailOpen = ref(false)
const detailLoading = ref(false)
const detailTitle = ref('')
const summary = ref<StatementRow | null>(null)
const lines = ref<LedgerLine[]>([])

function isPositive(v: string): boolean {
  return Number(v) > 0
}
function lineTagType(type: string): string {
  if (type === 'INVOICE') return 'warning'
  if (type === 'PAYMENT') return 'success'
  if (type === 'ADVANCE') return 'info'
  return 'danger' // reversals: the corrections stay visible
}

async function load() {
  loading.value = true
  try {
    const resp = await get<{ items: StatementRow[] }>('/supplier-statements', { keyword: keyword.value })
    rows.value = resp.items || []
  } finally {
    loading.value = false
  }
}

async function openDetail(row: StatementRow) {
  detailTitle.value = `${row.supplierName} · ${row.currency}`
  detailOpen.value = true
  detailLoading.value = true
  try {
    const resp = await get<{ summary: StatementRow; lines: LedgerLine[] }>(
      `/supplier-statements/${row.supplierId}`, { currency: row.currency })
    summary.value = resp.summary
    lines.value = resp.lines || []
  } finally {
    detailLoading.value = false
  }
}

onMounted(load)
</script>

<style scoped>
.overdue { color: var(--el-color-danger); font-weight: 600; }
.warn { color: var(--el-color-warning); }
</style>
