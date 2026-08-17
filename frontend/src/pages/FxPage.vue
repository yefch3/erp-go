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

    <el-card shadow="never" class="block" v-loading="loadingHistory">
      <template #header>
        <div class="chart-head">
          <span>{{ t('fx.trend') }} — {{ selected }}</span>
          <el-radio-group v-model="days" size="small" @change="() => selectCurrency(selected)">
            <el-radio-button v-for="d in RANGES" :key="d" :value="d">
              {{ t('fx.rangeDays', { n: d }) }}
            </el-radio-button>
          </el-radio-group>
        </div>
      </template>
      <RateChart :points="chartPoints" :currency="selected" />
    </el-card>

    <el-card shadow="never" class="block">
      <template #header>{{ t('fx.history') }} — {{ selected }}</template>
      <el-table :data="history" v-loading="loadingHistory" size="small" max-height="320">
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
import { computed, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { get } from '../api'
import RateChart, { type ChartPoint } from '../components/RateChart.vue'

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
// 30 for "what happened lately", 365 for "what is this pair actually doing".
// The feed publishes working days only, so 30 days is about 22 points.
const RANGES = [30, 90, 365]

const { t } = useI18n()
const latest = ref<RateRow[]>([])
const history = ref<RateRow[]>([])
const anomalies = ref<AnomalyRow[]>([])
const selected = ref('CNY')
const days = ref(90)
const loadingLatest = ref(false)
const loadingHistory = ref(false)

// The table reads newest-first (which is what somebody scanning for today's
// number wants); a chart has to read oldest-first, or time runs backwards.
const chartPoints = computed<ChartPoint[]>(() =>
  history.value
    .map((r) => ({ date: r.rateDate, value: Number(r.unitsPerUsd) }))
    .filter((p) => isFinite(p.value) && p.value > 0)
    .reverse(),
)

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
    history.value = (await get<{ rates: RateRow[] }>('/fx/rates', { currency, days: days.value })).rates
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
.chart-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  flex-wrap: wrap;
}
</style>
