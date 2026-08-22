<template>
  <div class="page">
    <header class="page-head">
      <div><span class="eyebrow">{{ t('sourcing.listEyebrow') }}</span><h1>{{ t('sourcing.title') }}</h1><p>{{ t('sourcing.subtitle') }}</p></div><el-button @click="router.push('/procurement')">← {{ t('procurementNav.backToWorkbench') }}</el-button>
    </header>
    <section class="list-card">
      <div class="toolbar">
        <el-input v-model="keyword" :placeholder="t('sourcing.search')" clearable @keyup.enter="reload" />
        <el-select v-model="status" clearable :placeholder="t('sourcing.activeProjects')" @change="reload"><el-option v-for="item in statuses" :key="item" :value="item" :label="t(`sourcing.statuses.${item}`)" /></el-select>
        <el-button type="primary" @click="reload">{{ t('common.query') }}</el-button>
      </div>
      <el-table v-loading="loading" :data="rows" stripe @row-click="openCase">
        <el-table-column prop="caseNo" :label="t('sourcing.caseNo')" width="185" />
        <el-table-column :label="t('sourcing.customerAndTitle')" min-width="230"><template #default="{ row }"><strong>{{ row.customerName || '—' }}</strong><small>{{ row.title }}</small></template></el-table-column>
        <el-table-column :label="t('sourcing.currentStage')" width="150"><template #default="{ row }"><el-tag effect="plain">{{ statusLabel(row.status) }}</el-tag></template></el-table-column>
        <el-table-column :label="t('sourcing.currentWaiting')" min-width="180"><template #default="{ row }">{{ waitingFor(row.status) }}</template></el-table-column>
        <el-table-column prop="ownerName" :label="t('sourcing.owner')" width="130" />
        <el-table-column :label="t('sourcing.recentUpdate')" width="175"><template #default="{ row }">{{ formatTime(row.updatedAt) }}</template></el-table-column>
        <el-table-column :label="t('sourcing.exception')" width="135"><template #default="{ row }"><el-tag v-if="isStale(row)" type="warning">{{ t('sourcing.stale') }}</el-tag><span v-else>—</span></template></el-table-column>
        <el-table-column :label="t('common.actions')" width="90" fixed="right"><template #default="{ row }"><el-button link type="primary" @click.stop="openCase(row)">{{ t('common.detail') }}</el-button></template></el-table-column>
        <template #empty>{{ t('sourcing.empty') }}</template>
      </el-table>
      <el-pagination class="pager" layout="total, prev, pager, next" :total="total" :page-size="pageSize" v-model:current-page="page" @current-change="load" />
    </section>
  </div>
</template>

<script setup lang="ts">
import { ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRoute, useRouter } from 'vue-router'
import { get } from '../api'

interface SourcingCase { id:string; caseNo:string; customerName:string; title:string; ownerName:string; status:string; updatedAt:string }
const { t } = useI18n()
const route = useRoute()
const router = useRouter()
// 进行中的项目作为默认视图，同时允许用户主动找回已生成报价单或已取消的历史项目。
const statuses = ['REVIEWING','SOURCING','QUOTES_RECEIVED','COSTING','CUSTOMER_QUOTE_CREATED','CANCELLED']
const rows=ref<SourcingCase[]>([]),total=ref(0),page=ref(1),keyword=ref(''),status=ref(''),loading=ref(false)
const pageSize=20

// load 只负责列表查询；详情操作全部放在独立详情页，避免列表再次变成大型弹窗。
async function load(){loading.value=true;try{const response=await get<{sourcingCases:SourcingCase[];meta:{total:number}}>('/sourcing-cases',{page:page.value,page_size:pageSize,keyword:keyword.value,status:status.value});rows.value=response.sourcingCases??[];total.value=Number(response.meta?.total??0)}finally{loading.value=false}}
function reload(){page.value=1;void load()}
function openCase(row:SourcingCase){void router.push(`/sourcing-cases/${row.id}`)}
function formatTime(value:string){return value?new Date(value).toLocaleString():'—'}
function isStale(row:SourcingCase){return Date.now()-new Date(row.updatedAt).getTime()>7*86400000}
const knownStatuses = new Set(['REVIEWING','SOURCING','QUOTES_RECEIVED','COSTING','CUSTOMER_QUOTE_CREATED','CANCELLED'])
function normalizedStatus(value?:string){return value && knownStatuses.has(value) ? value : 'UNKNOWN'}
function statusLabel(value?:string){const statusKey=normalizedStatus(value);return statusKey==='UNKNOWN'?t('sourcing.unknownStatus'):t(`sourcing.statuses.${statusKey}`)}
function waitingFor(value?:string){return t(`sourcing.waiting.${normalizedStatus(value)}`)}

// 兼容采购工作台和询盘确认页生成的旧链接，并统一跳转到新的独立详情页。
const linkedCaseID = String(route.query.case || '')
if (linkedCaseID) void router.replace(`/sourcing-cases/${linkedCaseID}`)
else void load()
</script>

<style scoped>
.page{padding:24px;background:#f5f7fa;min-height:calc(100vh - 60px)}.page-head{display:flex;justify-content:space-between;align-items:flex-start;margin-bottom:16px}.page-head h1{margin:4px 0;color:#17324d}.page-head p{margin:0;color:#75889a}.eyebrow{color:#0b8f82;font-size:12px;font-weight:700;letter-spacing:.14em}.list-card{padding:18px 20px;background:#fff;border:1px solid #dce5ec;border-radius:14px}.toolbar{display:grid;grid-template-columns:minmax(280px,1fr) 220px auto;gap:12px;margin-bottom:16px}.el-table small{display:block;color:#8795a3;margin-top:4px}.pager{justify-content:flex-end;margin-top:16px}@media(max-width:760px){.page{padding:14px}.page-head{gap:14px;flex-direction:column}.toolbar{grid-template-columns:1fr}.list-card{padding:14px}}
</style>
