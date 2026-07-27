<template>
  <div>
    <div class="page-head">
      <h2>{{ t('fx.title') }}</h2>
      <el-button v-if="auth.can('fx:rate:write')" type="primary" @click="openManual">
        {{ t('fx.manualEntry') }}
      </el-button>
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
        <el-table-column :label="t('fx.unitsPerUsd')" min-width="140">
          <template #default="{ row }">1 USD = {{ row.unitsPerUsd }} {{ row.quoteCurrency }}</template>
        </el-table-column>
        <el-table-column :label="t('fx.usdPerUnit')" min-width="140">
          <template #default="{ row }">1 {{ row.quoteCurrency }} = {{ row.usdPerUnit }} USD</template>
        </el-table-column>
        <el-table-column prop="rateDate" :label="t('fx.date')" width="120" />
        <el-table-column :label="t('fx.source')" width="130">
          <template #default="{ row }">
            <el-tag :type="row.source === 'MANUAL' ? 'warning' : 'success'" size="small">
              {{ row.source === 'MANUAL' ? t('fx.sourceManual') : t('fx.sourceApi') }}
            </el-tag>
          </template>
        </el-table-column>
      </el-table>
      <p class="hint">{{ t('fx.clickHint') }}</p>
    </el-card>

    <el-card shadow="never" class="block">
      <template #header>{{ t('fx.history') }} — {{ selected }}</template>
      <el-table :data="history" v-loading="loadingHistory" size="small">
        <el-table-column prop="rateDate" :label="t('fx.date')" width="130" />
        <el-table-column :label="t('fx.unitsPerUsd')" min-width="140">
          <template #default="{ row }">{{ row.unitsPerUsd }}</template>
        </el-table-column>
        <el-table-column :label="t('fx.source')" width="130">
          <template #default="{ row }">
            <el-tag :type="row.source === 'MANUAL' ? 'warning' : 'success'" size="small">
              {{ row.source === 'MANUAL' ? t('fx.sourceManual') : t('fx.sourceApi') }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column prop="fetchedAt" :label="t('fx.fetchedAt')" min-width="180" />
      </el-table>
    </el-card>

    <el-card shadow="never" class="block">
      <template #header>{{ t('fx.anomalies') }}</template>
      <el-table :data="anomalies" size="small" :empty-text="t('fx.noAnomalies')">
        <el-table-column prop="quoteCurrency" :label="t('fx.currency')" width="100" />
        <el-table-column prop="newRate" :label="t('fx.newRate')" width="130" />
        <el-table-column prop="prevRate" :label="t('fx.prevRate')" width="130" />
        <el-table-column :label="t('fx.deviation')" width="110">
          <template #default="{ row }">
            <span class="deviation">{{ row.deviationPct }}%</span>
          </template>
        </el-table-column>
        <el-table-column prop="detectedAt" :label="t('fx.detectedAt')" min-width="180" />
        <el-table-column prop="note" :label="t('fx.note')" min-width="140" />
      </el-table>
    </el-card>

    <el-dialog v-model="dialogOpen" :title="t('fx.manualEntry')" width="440px">
      <el-form label-width="130px">
        <el-form-item :label="t('fx.currency')" required>
          <el-select v-model="form.currency" filterable allow-create style="width: 160px">
            <el-option v-for="c in SYMBOLS" :key="c" :value="c" :label="c" />
          </el-select>
        </el-form-item>
        <el-form-item :label="t('fx.rateInput')" required>
          <el-input v-model="form.rate" placeholder="7.2435" style="width: 200px">
            <template #prepend>1 USD =</template>
          </el-input>
        </el-form-item>
        <el-form-item :label="t('fx.note')">
          <el-input v-model="form.note" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="dialogOpen = false">{{ t('common.cancel') }}</el-button>
        <el-button type="primary" :loading="saving" @click="saveManual">{{ t('common.save') }}</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup lang="ts">
import { onMounted, reactive, ref } from 'vue'
import { ElMessage } from 'element-plus'
import { useI18n } from 'vue-i18n'
import { get, post } from '../api'
import { useAuthStore } from '../stores/auth'

interface RateRow {
  quoteCurrency: string
  unitsPerUsd: string
  usdPerUnit: string
  rateDate: string
  source: string
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
const auth = useAuthStore()
const latest = ref<RateRow[]>([])
const history = ref<RateRow[]>([])
const anomalies = ref<AnomalyRow[]>([])
const selected = ref('CNY')
const loadingLatest = ref(false)
const loadingHistory = ref(false)
const dialogOpen = ref(false)
const saving = ref(false)
const form = reactive({ currency: 'CNY', rate: '', note: '' })

// ListRates returns 200 with an empty list for unknown currencies, so a
// per-symbol history probe never spams error toasts the way a 404 would.
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

async function loadAnomalies() {
  anomalies.value = (await get<{ anomalies: AnomalyRow[] }>('/fx/anomalies')).anomalies
}

function openManual() {
  Object.assign(form, { currency: selected.value, rate: '', note: '' })
  dialogOpen.value = true
}

async function saveManual() {
  if (!form.currency || !form.rate) {
    ElMessage.warning(t('fx.required'))
    return
  }
  saving.value = true
  try {
    const data = await post<{ anomaly: boolean; deviationPct: string }>('/fx/manual', {
      quoteCurrency: form.currency, unitsPerUsd: form.rate, note: form.note,
    })
    if (data.anomaly) {
      ElMessage.warning(t('fx.anomalyFlagged', { pct: data.deviationPct }))
    } else {
      ElMessage.success(t('fx.saved'))
    }
    dialogOpen.value = false
    await Promise.all([loadLatest(), selectCurrency(form.currency), loadAnomalies()])
  } finally {
    saving.value = false
  }
}

onMounted(() => {
  loadLatest()
  selectCurrency(selected.value)
  loadAnomalies()
})
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
