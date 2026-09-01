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
      <!-- 额度。只在看着当月时给百分比：翻回七月问「还剩多少」是没有意义
           的，那个月已经过完了。当月与否由服务端给的月份说了算，不用浏览
           器的时钟去猜。 -->
      <div class="metric" :class="{ 'is-unset': state !== 'live' }">
        <span class="metric-label">{{ t('excelUsage.quota') }}</span>
        <template v-if="state === 'live'">
          <strong class="metric-value">{{ percent }}%</strong>
          <span class="metric-hint">
            {{ t('excelUsage.quotaHint', { used: quota.usedThisMonth, total: quota.monthlyRuns }) }}
          </span>
        </template>
        <!-- 服务端还没说话（首屏、或那一次请求失败）：说「不知道」，不说
             「未设上限」也不说「往月记录」——两句都是断言，而这时候我们什
             么都还不知道。 -->
        <template v-else-if="state === 'unknown'">
          <strong class="metric-value is-unknown">—</strong>
        </template>
        <template v-else-if="state === 'unlimited'">
          <strong class="metric-value is-unknown">{{ t('excelUsage.noQuota') }}</strong>
          <span class="metric-hint">{{ t('excelUsage.noQuotaHint') }}</span>
        </template>
        <template v-else>
          <strong class="metric-value is-unknown">{{ t('excelUsage.pastMonth') }}</strong>
          <span class="metric-hint">{{ t('excelUsage.pastMonthHint') }}</span>
        </template>
      </div>
    </section>

    <!-- 用完了不是提示，是当场就点不动了，所以这条要比警告更重 -->
    <el-alert
      v-if="state === 'live'"
      :type="tone"
      :closable="false"
      show-icon
      :title="headline"
      class="quota-bar"
    >
      <el-progress
        :percentage="Math.min(percent, 100)"
        :status="progressStatus"
        :stroke-width="10"
        :show-text="false"
      />
    </el-alert>

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
        <template #empty>{{ t('excelUsage.empty') }}</template>
      </el-table>
    </section>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { get } from '../api'
import {
  emptyExcelQuota,
  excelQuotaPercent,
  parseExcelQuota,
  excelQuotaProgressStatus,
  excelQuotaRemaining,
  excelQuotaState,
  excelQuotaTone,
  type ExcelQuota,
} from '../lib/excelQuota'

// 智能转换的用量账（计量）。
//
// 这是系统里唯一一处按次花真钱的地方。这一页要回答的就两句话：这个月转了
// 多少次、我本月的额度还剩多少；以及分到每个人头上各是多少次。
//
// **这里不出金额，也不出 token。** 服务端两样都算得出（token 数是事实，
// 单价是配置），接口也还在回 token 字段，但这一页一个都不显示：
//
//   · 金额是我们付给模型厂的钱，是平台那一侧的口径
//   · token 是那笔钱的原料，对用的人同样没有意义——他能决定的是「还要不要
//     再转一次」，而那个决定只看次数
//
// 两样都要看，去平台那一侧的「平台开户」页。
//
// 额度是**每人每月**的（2026-09-01 起）：这一页顶上那个百分比说的是你自己
// 用了多少，不是全公司。
const { t } = useI18n()

interface Row {
  month: string
  ownerId: string
  ownerName: string
  runs: string
  succeeded: string
  failed: string
}

const rows = ref<Row[]>([])
const quota = ref<ExcelQuota>({ ...emptyExcelQuota })
const loading = ref(false)
const month = ref(new Date().toISOString().slice(0, 7))

// 这几条判断（当月与否、百分比、警戒色）都在 lib/excelQuota.ts 里，那里有
// 测试盯着。当月与否由服务端给的月份说了算，不用浏览器的时钟去猜——月初那
// 几个小时正是最容易差出一个月的时候。
const state = computed(() => excelQuotaState(quota.value, month.value))
const percent = computed(() => excelQuotaPercent(quota.value))
const tone = computed(() => excelQuotaTone(percent.value))
const progressStatus = computed(() => excelQuotaProgressStatus(percent.value))
const headline = computed(() => {
  const left = excelQuotaRemaining(quota.value)
  if (left === 0) return t('excelUsage.quotaGone')
  if (percent.value >= 80) return t('excelUsage.quotaLow', { left })
  return t('excelUsage.quotaLeft', { left })
})

const totals = computed(() => {
  let runs = 0, succeeded = 0, failed = 0
  for (const r of rows.value) {
    runs += Number(r.runs)
    succeeded += Number(r.succeeded)
    failed += Number(r.failed)
  }
  return { runs, succeeded, failed }
})

// 首屏那次的月份是浏览器猜的（new Date() 是这台机器的时钟和时区）。服务端
// 一答话就以它为准——这一页从头到尾的规矩是「月份由数据库说了算」，唯独初值
// 还留着浏览器在猜，那正是月初那几个小时会差出一个月的地方。
// 只在第一次对齐，之后用户自己选的月份说了算。
let monthAligned = false

async function load() {
  loading.value = true
  try {
    const d = await get<{ rows: Row[]; quota?: unknown }>('/excel-usage', { month: month.value })
    rows.value = d.rows ?? []
    quota.value = parseExcelQuota(d.quota)
    if (!monthAligned) {
      monthAligned = true
      const serverMonth = quota.value.currentMonth
      if (serverMonth && serverMonth !== month.value) {
        month.value = serverMonth
        loading.value = false
        await load()
        return
      }
    }
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
.quota-bar :deep(.el-alert__content) {
  width: 100%;
}
.quota-bar :deep(.el-progress) {
  margin-top: 8px;
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
