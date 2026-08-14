<template>
  <div class="page">
    <ProcurementNav />
    <div class="page-head">
      <div>
        <h1>{{ t('sourcing.title') }}</h1>
        <p>{{ t('sourcing.subtitle') }}</p>
      </div>
    </div>

    <div class="toolbar">
      <el-input v-model="keyword" :placeholder="t('sourcing.search')" clearable @keyup.enter="reload" />
      <el-select v-model="status" clearable :placeholder="t('common.status')" @change="reload">
        <el-option v-for="s in statuses" :key="s" :value="s" :label="t(`sourcing.statuses.${s}`)" />
      </el-select>
      <el-button type="primary" @click="reload">{{ t('common.query') }}</el-button>
    </div>

    <el-table v-loading="loading" :data="rows" stripe @row-click="openCase">
      <el-table-column prop="caseNo" :label="t('sourcing.caseNo')" width="180" />
      <el-table-column prop="title" :label="t('sourcing.inquiry')" min-width="240" />
      <el-table-column prop="customerName" :label="t('sourcing.customer')" min-width="180" />
      <el-table-column prop="ownerName" :label="t('sourcing.owner')" width="140" />
      <el-table-column :label="t('common.status')" width="160">
        <template #default="{ row }">
          <el-tag effect="plain">{{ t(`sourcing.statuses.${row.status}`) }}</el-tag>
        </template>
      </el-table-column>
      <el-table-column :label="t('sourcing.createdAt')" width="170">
        <template #default="{ row }">{{ formatTime(row.createdAt) }}</template>
      </el-table-column>
      <template #empty>{{ t('sourcing.empty') }}</template>
    </el-table>

    <el-pagination
      class="pager" layout="total, prev, pager, next" :total="total"
      :page-size="pageSize" v-model:current-page="page" @current-change="load"
    />

    <el-dialog v-model="detailOpen" :title="detail?.caseNo || t('sourcing.title')" width="min(1200px, 94vw)">
      <el-descriptions v-if="detail" :column="3" border class="meta">
        <el-descriptions-item :label="t('sourcing.customer')">{{ detail.customerName || '—' }}</el-descriptions-item>
        <el-descriptions-item :label="t('sourcing.contact')">{{ detail.contactName || detail.contactEmail || '—' }}</el-descriptions-item>
        <el-descriptions-item :label="t('common.status')">{{ t(`sourcing.statuses.${detail.status}`) }}</el-descriptions-item>
      </el-descriptions>
      <el-table v-if="detail" :data="detail.lines" size="small" border>
        <el-table-column prop="lineNo" label="#" width="55" />
        <el-table-column :label="t('sourcing.product')" min-width="150">
          <template #default="{ row }">{{ row.extracted.product || '—' }}</template>
        </el-table-column>
        <el-table-column :label="t('sourcing.standard')" min-width="190">
          <template #default="{ row }">{{ row.extracted.materialStandard || row.extracted.grade || '—' }}</template>
        </el-table-column>
        <el-table-column :label="t('sourcing.size')" min-width="180">
          <template #default="{ row }">{{ sizeOf(row.extracted) }}</template>
        </el-table-column>
        <el-table-column :label="t('sourcing.port')" min-width="130">
          <template #default="{ row }">{{ row.extracted.port || '—' }}</template>
        </el-table-column>
        <el-table-column :label="t('sourcing.quantity')" width="140" align="right">
          <template #default="{ row }">{{ row.extracted.quantity }} {{ row.extracted.quantityUnit }}</template>
        </el-table-column>
        <el-table-column :label="t('sourcing.decision')" width="120">
          <template #default="{ row }">{{ t(`sourcing.decisions.${row.decision}`) }}</template>
        </el-table-column>
      </el-table>
      <template #footer><el-button @click="detailOpen = false">{{ t('common.close') }}</el-button></template>
    </el-dialog>
  </div>
</template>

<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRoute, useRouter } from 'vue-router'
import { get } from '../api'
import ProcurementNav from '../components/ProcurementNav.vue'

interface ExtractedLine {
  product: string; materialStandard: string; grade: string; thickness: string
  width: string; lengthOrForm: string; port: string; quantity: string; quantityUnit: string
}
interface SourcingLine { id: string; lineNo: number; decision: string; extracted: ExtractedLine }
interface SourcingCase {
  id: string; caseNo: string; title: string; customerName: string; contactName: string
  contactEmail: string; ownerName: string; status: string; createdAt: string; lines: SourcingLine[]
}

const { t } = useI18n()
const route = useRoute()
const router = useRouter()
const statuses = ['REVIEWING', 'SOURCING', 'QUOTES_RECEIVED', 'COSTING', 'CUSTOMER_QUOTE_CREATED', 'CANCELLED']
const rows = ref<SourcingCase[]>([])
const total = ref(0)
const page = ref(1)
const pageSize = 20
const keyword = ref('')
const status = ref('')
const loading = ref(false)
const detailOpen = ref(false)
const detail = ref<SourcingCase | null>(null)

async function load() {
  loading.value = true
  try {
    const response = await get<{ sourcingCases: SourcingCase[]; meta: { total: number } }>('/sourcing-cases', {
      page: page.value, page_size: pageSize, keyword: keyword.value, status: status.value,
    })
    rows.value = response.sourcingCases ?? []
    total.value = Number(response.meta?.total ?? 0)
  } finally { loading.value = false }
}

function reload() { page.value = 1; load() }

async function openCase(row: Pick<SourcingCase, 'id'>) {
  const response = await get<{ sourcingCase: SourcingCase }>(`/sourcing-cases/${row.id}`)
  detail.value = response.sourcingCase
  detailOpen.value = true
  router.replace({ query: { ...route.query, case: row.id } })
}

function sizeOf(line: ExtractedLine) {
  return [line.thickness, line.width, line.lengthOrForm].filter(Boolean).join(' × ') || '—'
}
function formatTime(value: string) { return value ? new Date(value).toLocaleString() : '—' }

onMounted(async () => {
  await load()
  const id = String(route.query.case || '')
  if (id) await openCase({ id })
})
</script>

<style scoped>
.page { padding: 24px; }
.page-head { display:flex; justify-content:space-between; margin-bottom:18px; }
h1 { margin:0; font-size:24px; } .page-head p { margin:6px 0 0; color:#6b7280; }
.toolbar { display:flex; gap:10px; margin-bottom:14px; }
.toolbar .el-input { width:300px; } .toolbar .el-select { width:190px; }
.pager { margin-top:16px; justify-content:flex-end; } .meta { margin-bottom:16px; }
</style>
