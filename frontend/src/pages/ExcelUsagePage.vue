<template>
  <div class="page">
    <header class="page-head">
      <div>
        <div class="eyebrow">{{ t('excelUsage.eyebrow') }}</div>
        <h1>{{ t('excelUsage.title') }}</h1>
        <p>{{ t('excelUsage.subtitle') }}</p>
      </div>
      <el-date-picker
        v-model="month"
        type="month"
        value-format="YYYY-MM"
        :clearable="false"
        style="width: 150px"
        @change="load"
      />
    </header>

    <section class="metrics">
      <div class="metric">
        <span class="metric-label">{{ t('excelUsage.runs') }}</span>
        <strong class="metric-value">{{ totals.runs }}</strong>
        <span class="metric-hint">{{ t('excelUsage.runsHint', { ok: totals.succeeded, bad: totals.failed }) }}</span>
      </div>
      <div class="metric">
        <span class="metric-label">{{ t('excelUsage.tokens') }}</span>
        <strong class="metric-value">{{ formatTokens(totals.inputTokens + totals.outputTokens) }}</strong>
        <span class="metric-hint">
          {{ t('excelUsage.tokensHint', { input: formatTokens(totals.inputTokens), output: formatTokens(totals.outputTokens) }) }}
        </span>
      </div>
      <!-- 没配单价就不给金额。空和 0 是两回事：一个是「不知道」，一个是
           「不要钱」，而一个凭空的 0 看着像账。 -->
      <div class="metric" :class="{ 'is-unset': !priced }">
        <span class="metric-label">{{ t('excelUsage.cost') }}</span>
        <strong v-if="priced" class="metric-value">{{ currency }} {{ totals.cost }}</strong>
        <strong v-else class="metric-value is-unknown">{{ t('excelUsage.noPrice') }}</strong>
        <span class="metric-hint">{{ priced ? t('excelUsage.costHint') : t('excelUsage.noPriceHint') }}</span>
      </div>
    </section>

    <section class="panel">
      <el-alert type="info" show-icon :closable="false" class="alert" :title="t('excelUsage.note')" />

      <el-table v-loading="loading" :data="rows">
        <el-table-column :label="t('excelUsage.who')" min-width="160">
          <template #default="{ row }">
            <div>{{ row.ownerName || t('excelUsage.unknownPerson', { id: row.ownerId }) }}</div>
            <div class="sub">{{ row.month }}</div>
          </template>
        </el-table-column>
        <el-table-column :label="t('excelUsage.runs')" width="150" align="right">
          <template #default="{ row }">
            <span class="num">{{ row.runs }}</span>
            <div class="sub">
              <span>{{ t('excelUsage.okCount', { n: row.succeeded }) }}</span>
              <!-- 失败也计费，所以失败数必须和成功数并排显示，不能藏起来 -->
              <span v-if="Number(row.failed) > 0" class="failed">
                · {{ t('excelUsage.failCount', { n: row.failed }) }}
              </span>
            </div>
          </template>
        </el-table-column>
        <el-table-column :label="t('excelUsage.inputTokens')" width="140" align="right">
          <template #default="{ row }"><span class="num">{{ formatTokens(row.inputTokens) }}</span></template>
        </el-table-column>
        <el-table-column :label="t('excelUsage.outputTokens')" width="140" align="right">
          <template #default="{ row }"><span class="num">{{ formatTokens(row.outputTokens) }}</span></template>
        </el-table-column>
        <el-table-column :label="t('excelUsage.cost')" width="150" align="right">
          <template #default="{ row }">
            <span v-if="row.estimatedCost" class="num money">{{ row.currency }} {{ row.estimatedCost }}</span>
            <span v-else class="sub">—</span>
          </template>
        </el-table-column>
        <template #empty>{{ t('excelUsage.empty') }}</template>
      </el-table>
    </section>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { get } from '../api'

// 智能转换的用量账（计量）。
//
// 这是系统里唯一一处按次花真钱的地方。这一页要回答的就三句话：这个月转了
// 多少次、烧了多少 token、大概多少钱；以及分到每个人头上各是多少。
//
// 金额标「估算」不是谦虚：token 数是从模型返回里抄下来的事实，单价是配置
// 里填的，两者相乘得到的是我们这边的推算——真正的账单以模型厂为准。
const { t } = useI18n()

interface Row {
  month: string
  ownerId: string
  ownerName: string
  runs: string
  succeeded: string
  failed: string
  inputTokens: string
  outputTokens: string
  estimatedCost: string
  currency: string
}

const rows = ref<Row[]>([])
const loading = ref(false)
const month = ref(new Date().toISOString().slice(0, 7))

const priced = computed(() => rows.value.some((r) => r.estimatedCost !== ''))
const currency = computed(() => rows.value.find((r) => r.currency)?.currency ?? '')

const totals = computed(() => {
  let runs = 0, succeeded = 0, failed = 0, inputTokens = 0, outputTokens = 0, cost = 0
  for (const r of rows.value) {
    runs += Number(r.runs)
    succeeded += Number(r.succeeded)
    failed += Number(r.failed)
    inputTokens += Number(r.inputTokens)
    outputTokens += Number(r.outputTokens)
    cost += Number(r.estimatedCost || 0)
  }
  // 合计保留四位，理由和单行一样：一个月几十次的量级，两位会把它抹成 0.00。
  return { runs, succeeded, failed, inputTokens, outputTokens, cost: cost.toFixed(4) }
})

function formatTokens(n: number | string): string {
  const v = Number(n)
  if (!Number.isFinite(v)) return String(n)
  return v.toLocaleString()
}

async function load() {
  loading.value = true
  try {
    const d = await get<{ rows: Row[] }>('/excel-usage', { month: month.value })
    rows.value = d.rows ?? []
  } finally {
    loading.value = false
  }
}

onMounted(load)
</script>

<style scoped>
.page {
  display: flex;
  flex-direction: column;
  gap: 18px;
}
.page-head {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 16px;
  flex-wrap: wrap;
}
.page-head .eyebrow {
  font-size: 12px;
  letter-spacing: 1.5px;
  color: var(--el-text-color-secondary);
}
.page-head h1 {
  margin: 4px 0 6px;
  font-size: 28px;
}
.page-head p {
  margin: 0;
  color: var(--el-text-color-regular);
}
.metrics {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(200px, 1fr));
  gap: 14px;
}
.metric {
  display: flex;
  flex-direction: column;
  gap: 4px;
  padding: 14px 16px;
  border: 1px solid var(--el-border-color-lighter);
  border-radius: 10px;
  background: var(--el-bg-color);
}
.metric.is-unset {
  border-style: dashed;
}
.metric-label {
  font-size: 13px;
  color: var(--el-text-color-secondary);
}
.metric-value {
  font-size: 26px;
  font-variant-numeric: tabular-nums;
}
.metric-value.is-unknown {
  font-size: 16px;
  color: var(--el-text-color-placeholder);
}
.metric-hint {
  font-size: 12px;
  color: var(--el-text-color-placeholder);
}
.panel {
  padding: 16px;
  border: 1px solid var(--el-border-color-lighter);
  border-radius: 10px;
  background: var(--el-bg-color);
}
.alert {
  margin-bottom: 12px;
}
.sub {
  font-size: 12px;
  color: var(--el-text-color-secondary);
}
.sub .failed {
  color: var(--el-color-warning);
}
.num {
  font-variant-numeric: tabular-nums;
}
.num.money {
  font-weight: 600;
}
</style>
