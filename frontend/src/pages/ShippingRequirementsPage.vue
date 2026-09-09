<template>
  <div class="shipping-requirements-page">
    <WorkflowPageHeader :title="t('shipping.executionInquiryTitle')" :description="t('shipping.executionInquiryHint')" />

    <el-card shadow="never">
      <div class="filters">
        <el-input v-model="keyword" :placeholder="t('shipping.executionInquirySearch')" clearable @clear="page = 1" @keyup.enter="page = 1" />
        <el-button @click="load">{{ t('common.refresh') }}</el-button>
      </div>

      <el-table :data="pagedRows" v-loading="loading">
        <el-table-column :label="t('shipping.contractNo')" width="180">
          <template #default="{ row }"><span class="contract-no">{{ row.contractNo }}</span></template>
        </el-table-column>
        <el-table-column prop="customerName" :label="t('shipping.customer')" min-width="150" />
        <el-table-column :label="t('shipping.freightBatch')" width="120">
          <template #default="{ row }">#{{ row.batchNo }}</template>
        </el-table-column>
        <el-table-column :label="t('shipping.route')" min-width="210">
          <template #default="{ row }">{{ row.portOfLoading || '—' }} → {{ row.portOfDischarge || '—' }}</template>
        </el-table-column>
        <el-table-column :label="t('shipping.presalesReference')" min-width="210">
          <template #default="{ row }">
            <div>{{ row.carrierForwarder || '—' }}</div>
            <div class="sub">{{ row.serviceOptionName || '—' }}</div>
          </template>
        </el-table-column>
        <el-table-column :label="t('common.status')" width="140" align="center">
          <template #default="{ row }">
            <el-tag :type="row.status === 'WAITING_REQUOTE' ? 'warning' : row.status === 'SCHEDULED' ? 'success' : 'info'">
              {{ t(`shipping.handoffStatuses.${row.status}`) }}
            </el-tag>
          </template>
        </el-table-column>
        <template #empty>{{ t('shipping.executionInquiryEmpty') }}</template>
      </el-table>

      <el-pagination
        class="pager"
        layout="total, sizes, prev, pager, next"
        :total="filteredRows.length"
        :page-size="pageSize"
        :current-page="page"
        :page-sizes="[20, 50, 100]"
        @current-change="page = $event"
        @size-change="pageSize = $event; page = 1"
      />
    </el-card>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { get } from '../api'
import WorkflowPageHeader from '../components/WorkflowPageHeader.vue'
import type { ContractShippingHandoff } from '../components/ShippingScheduleDialog.vue'

const { t } = useI18n()
const loading = ref(false)
const rows = ref<ContractShippingHandoff[]>([])
const keyword = ref('')
const page = ref(1)
const pageSize = ref(20)

const filteredRows = computed(() => {
  const value = keyword.value.trim().toLowerCase()
  if (!value) return rows.value
  return rows.value.filter((row) => [row.contractNo, row.customerName, row.portOfLoading, row.portOfDischarge, row.carrierForwarder, row.serviceOptionName]
    .some((field) => String(field || '').toLowerCase().includes(value)))
})
const pagedRows = computed(() => filteredRows.value.slice((page.value - 1) * pageSize.value, page.value * pageSize.value))

async function load() {
  loading.value = true
  try {
    const data = await get<{ handoffs: ContractShippingHandoff[] }>('/shipping/contract-handoffs')
    rows.value = data.handoffs ?? []
    page.value = 1
  } finally {
    loading.value = false
  }
}

onMounted(load)
</script>

<style scoped>
.shipping-requirements-page { color:#141817; }
.filters { display:flex; gap:10px; margin-bottom:14px; }
.filters :deep(.el-input) { width:320px; max-width:100%; }
.shipping-requirements-page :deep(.el-table) { --el-table-header-bg-color:#eef9fe; --el-table-header-text-color:#24323a; --el-table-row-hover-bg-color:#f0fbf6; }
.shipping-requirements-page :deep(.el-table th.el-table__cell) { border-bottom-color:#d9edf5; font-weight:650; }
.contract-no { color:#159fdc; font-weight:600; }
.sub { margin-top:3px; color:#66727d; font-size:12px; }
.pager { justify-content:flex-end; margin-top:14px; }
@media (max-width:640px) { .filters { align-items:stretch; flex-direction:column; } .filters :deep(.el-input) { width:100%; } .pager { justify-content:flex-start; overflow-x:auto; } }
</style>
