<template>
  <div>
    <div class="page-head">
      <h2>{{ t('fx.title') }}</h2>
      <span class="feed-note">{{ t('fx.feedNote') }}</span>
    </div>

    <el-card shadow="never" class="block">
      <template #header>{{ t('fx.latest') }}</template>
      <el-table
        :data="latest"
        v-loading="loadingLatest"
        highlight-current-row
        @current-change="(row: RateRow | null) => row && selectCurrency(row.quoteCurrency)"
      >
        <el-table-column prop="quoteCurrency" :label="t('fx.currency')" width="100" />
        <el-table-column :label="t('fx.unitsPerUsd')" min-width="150">
          <template #default="{ row }">1 USD = {{ row.unitsPerUsd }} {{ row.quoteCurrency }}</template>
        </el-table-column>
        <el-table-column :label="t('fx.usdPerUnit')" min-width="150">
          <template #default="{ row }">1 {{ row.quoteCurrency }} = {{ row.usdPerUnit }} USD</template>
        </el-table-column>
        <el-table-column prop="rateDate" :label="t('fx.date')" width="130" />
        <el-table-column prop="fetchedAt" :label="t('fx.fetchedAt')" min-width="190" />
      </el-table>
      <p class="hint">{{ t('fx.clickHint') }}</p>
    </el-card>

    <el-card shadow="never" class="block">
      <template #header>{{ t('fx.history') }} — {{ selected }}</template>
      <el-table :data="history" v-loading="loadingHistory" size="small">
        <el-table-column prop="rateDate" :label="t('fx.date')" width="140" />
        <el-table-column :label="t('fx.unitsPerUsd')" min-width="150">
          <template #default="{ row }">{{ row.unitsPerUsd }}</template>
        </el-table-column>
        <el-table-column prop="fetchedAt" :label="t('fx.fetchedAt')" min-width="190" />
      </el-table>
    </el-card>

    <el-card shadow="never" class="block">
      <template #header>{{ t('fx.anomalies') }}</template>
      <el-table :data="anomalies" size="small" :empty-text="t('fx.noAnomalies')">
        <el-table-column prop="quoteCurrency" :label="t('fx.currency')" width="100" />
        <el-table-column prop="newRate" :label="t('fx.newRate')" width="140" />
        <el-table-column prop="prevRate" :label="t('fx.prevRate')" width="140" />
        <el-table-column :label="t('fx.deviation')" width="120">
          <template #default="{ row }">
            <span class="deviation">{{ row.deviationPct }}%</span>
          </template>
        </el-table-column>
        <el-table-column prop="detectedAt" :label="t('fx.detectedAt')" min-width="190" />
        <el-table-column prop="note" :label="t('fx.note')" min-width="150" />
      </el-table>
    </el-card>
  </div>
</template>

<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { get } from '../api'

interface RateRow {
  quoteCurrency: string
  unitsPerUsd: string
  usdPerUnit: string
  rateDate: string
  fetchedAt: string
}
interface AnomalyRow {
  quoteCurrency: string
  newRate: string
  prevRate: string
  deviationPct: string
  detectedAt: string
  note: string
}

const SYMBOLS = ['CNY', 'EUR', 'GBP', 'JPY', 'HKD']

const { t } = useI18n()
const latest = ref<RateRow[]>([])
const history = ref<RateRow[]>([])
const anomalies = ref<AnomalyRow[]>([])
const selected = ref('CNY')
const loadingLatest = ref(false)
const loadingHistory = ref(false)

// ListRates returns 200 with an empty list for unknown currencies, so a
// per-symbol probe never spams error toasts the way a 404 would.
async function loadLatest() {
  loadingLatest.value = true
  try {
    const results = await Promise.all(
      SYMBOLS.map((c) => get<{ rates: RateRow[] }>('/fx/rates', { currency: c, days: 35 })),
    )
    latest.value = results.flatMap((r) => (r.rates.length ? [r.rates[0]] : []))
  } finally {
    loadingLatest.value = false
  }
}

async function selectCurrency(currency: string) {
  selected.value = currency
  loadingHistory.value = true
  try {
    history.value = (await get<{ rates: RateRow[] }>('/fx/rates', { currency, days: 90 })).rates
  } finally {
    loadingHistory.value = false
  }
}

onMounted(async () => {
  loadLatest()
  selectCurrency(selected.value)
  anomalies.value = (await get<{ anomalies: AnomalyRow[] }>('/fx/anomalies')).anomalies
})
</script>

<style scoped>
.page-head {
  display: flex;
  align-items: baseline;
  justify-content: space-between;
  margin-bottom: 16px;
}
.page-head h2 {
  font-size: 18px;
  font-weight: 500;
  margin: 0;
}
.feed-note {
  font-size: 12px;
  color: #94a3b8;
}
.block {
  margin-bottom: 16px;
}
.hint {
  margin: 10px 2px 0;
  font-size: 12px;
  color: #94a3b8;
}
.deviation {
  color: #d97706;
  font-weight: 500;
}
</style>
