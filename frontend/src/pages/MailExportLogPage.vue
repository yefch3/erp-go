<template>
  <div>
    <div class="page-head">
      <h2>{{ t('exportLog.title') }}</h2>
      <span class="head-note">{{ t('exportLog.subtitle') }}</span>
    </div>

    <el-card shadow="never">
      <el-table :data="rows" v-loading="loading">
        <el-table-column :label="t('exportLog.when')" width="170">
          <template #default="{ row }">
            <span class="num">{{ shortTime(row.exportedAt) }}</span>
          </template>
        </el-table-column>
        <el-table-column :label="t('exportLog.who')" width="130">
          <template #default="{ row }">
            <span class="strong">{{ row.employeeName || `#${row.employeeId}` }}</span>
          </template>
        </el-table-column>
        <!-- The subject and the other side, because "they exported four
             conversations" answers nothing. This is also why the page has its
             own permission: it shows subject lines to a reader who may not be
             entitled to the mail itself. -->
        <el-table-column :label="t('exportLog.what')" min-width="260">
          <template #default="{ row }">
            <div class="ellipsis">{{ row.subject || t('emails.noSubject') }}</div>
            <div class="sub ellipsis">{{ row.counterparty || '—' }}</div>
          </template>
        </el-table-column>
        <el-table-column :label="t('exportLog.size')" width="150">
          <template #default="{ row }">
            <span class="num">{{ t('exportLog.messages', { n: row.turnCount }) }}</span>
            <span class="sub"> · {{ humanSize(Number(row.byteSize)) }}</span>
          </template>
        </el-table-column>
        <el-table-column :label="t('exportLog.from')" width="150">
          <template #default="{ row }">
            <span class="sub">{{ row.clientIp || '—' }}</span>
          </template>
        </el-table-column>
      </el-table>

      <el-empty v-if="!loading && rows.length === 0" :description="t('exportLog.empty')" />

      <el-pagination
        v-if="total > size"
        class="pager"
        layout="prev, pager, next, total"
        :total="total"
        :page-size="size"
        :current-page="page"
        @current-change="go"
      />
    </el-card>

    <p class="footnote">{{ t('exportLog.footnote') }}</p>
  </div>
</template>

<script setup lang="ts">
// Who took a conversation out of the system, and which one.
//
// The page exists because the export feature would otherwise be the one act
// in the mail module that leaves no trace: a file that can be mailed on,
// copied to a phone, or taken to the next employer. Nothing here prevents
// that — reading a mail and copying it out have never been separable. What it
// does is make the act visible, and being visible is what makes it rare.
import { onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { get } from '../api'

interface ExportRecord {
  id: string
  employeeId: string
  employeeName: string
  threadKey: string
  subject: string
  counterparty: string
  turnCount: number
  byteSize: string
  format: string
  clientIp: string
  exportedAt: string
}

const { t } = useI18n()
const rows = ref<ExportRecord[]>([])
const total = ref(0)
const page = ref(1)
const size = 20
const loading = ref(false)

async function reload() {
  loading.value = true
  try {
    const d = await get<{ items: ExportRecord[]; total: number }>('/mail-exports', {
      page: page.value,
      size,
    })
    rows.value = d.items ?? []
    total.value = Number(d.total ?? 0)
  } finally {
    loading.value = false
  }
}

function go(p: number) {
  page.value = p
  reload()
}

// Same shape as every other list in the product, deliberately: a page whose
// clock disagreed with the mail list next to it would make "he exported it
// eight hours after she sent it" out of two readings of the same moment.
// (The shared slice-the-ISO-string helper is duplicated across four pages
// already; unifying it is its own change.)
function shortTime(v: string) {
  if (!v) return ''
  return v.replace('T', ' ').replace('Z', '').slice(0, 16)
}

function humanSize(n: number) {
  if (!Number.isFinite(n) || n < 0) return '—'
  if (n < 1024) return `${n} B`
  const units = ['KB', 'MB', 'GB']
  let v = n / 1024
  for (const u of units) {
    if (v < 1024 || u === 'GB') return `${v < 10 ? v.toFixed(1) : Math.round(v)} ${u}`
    v /= 1024
  }
  return `${n} B`
}

onMounted(reload)
</script>

<style scoped>
.page-head {
  display: flex;
  align-items: baseline;
  gap: 12px;
  margin-bottom: 12px;
}
.head-note {
  color: var(--el-text-color-secondary);
  font-size: 13px;
}
.strong {
  font-weight: 600;
}
.sub {
  color: var(--el-text-color-secondary);
  font-size: 12px;
}
.num {
  font-variant-numeric: tabular-nums;
}
.ellipsis {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.pager {
  margin-top: 14px;
  justify-content: flex-end;
}
.footnote {
  margin-top: 14px;
  color: var(--el-text-color-secondary);
  font-size: 12px;
  line-height: 1.7;
}
</style>
