<template>
  <div class="workbench">
    <header class="hero">
      <div><h1>{{ t('procurementWorkbench.title') }}</h1><p>{{ t('procurementWorkbench.subtitle') }}</p></div>
      <div class="hero-actions">
        <el-button type="primary" @click="router.push('/procurement/sourcing/pending')">待开始询价 {{ metrics.reviewing }}</el-button>
      </div>
    </header>
    <section class="metrics">
      <router-link to="/procurement/sourcing/pending" class="metric metric--teal"><span>待开始询价</span><strong>{{ metrics.reviewing }}</strong><small>销售已复核，等待开始工厂询价</small></router-link>
      <router-link to="/procurement/sourcing" class="metric"><span>全部寻源项目</span><strong>{{ metrics.sourcing }}</strong><small>工厂询价、比价与成本方案</small></router-link>
      <router-link to="/requirements" class="metric"><span>{{ t('procurementWorkbench.requirementMetric') }}</span><strong>{{ metrics.requirements }}</strong><small>{{ t('procurementWorkbench.requirementMetricHint') }}</small></router-link>
      <!-- 数字要有行动含义（B4）：不是「一共多少单」，而是「几单等你批、
           几单等你发」——各自点进去就是筛好的列表。 -->
      <div class="metric metric--split">
        <span>{{ t('procurementWorkbench.orderMetric') }}</span>
        <div class="split-rows">
          <router-link :to="{ path: '/purchase-orders', query: { status: 'PENDING_APPROVAL' } }" class="split-row"><strong>{{ metrics.ordersPendingApproval }}</strong><small>{{ t('procurementWorkbench.ordersPendingApproval') }}</small></router-link>
          <router-link :to="{ path: '/purchase-orders', query: { unsent: '1' } }" class="split-row"><strong>{{ metrics.ordersToSend }}</strong><small>{{ t('procurementWorkbench.ordersToSend') }}</small></router-link>
        </div>
      </div>
    </section>
    <section>
      <div class="card focus-card">
        <div class="card-head"><h2>{{ t('procurementWorkbench.focusTitle') }}</h2><span v-if="focusCounts">{{ focusCounts }}</span><span v-else>{{ t('procurementWorkbench.updatedNow') }}</span></div>
        <template v-if="focusRows.length">
          <button v-for="item in focusRows" :key="item.key" class="focus-row" @click="router.push(item.path)"><i :class="`dot dot--${item.color}`"></i><div><strong>{{ item.title }}</strong><small>{{ item.meta }}</small></div><b>›</b></button>
        </template>
        <div v-else class="empty-focus">
          <strong>{{ t('procurementWorkbench.emptyTitle') }}</strong>
          <small>{{ t('procurementWorkbench.emptyHint') }}</small>
          <div><el-button v-if="canSourcingWrite" @click="router.push('/procurement/sourcing/pending')">查看待开始询价</el-button></div>
        </div>
      </div>
    </section>
  </div>
</template>
<script setup lang="ts">
import { onMounted, reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRouter } from 'vue-router'
import { get } from '../api'
import { useAuthStore } from '../stores/auth'
const { t } = useI18n(); const router = useRouter(); const auth = useAuthStore()
const canSourcingWrite = auth.can('procurement:sourcing:write')
const metrics = reactive({ pending: 0, reviewing: 0, sourcing: 0, requirements: 0, ordersPendingApproval: 0, ordersToSend: 0 })
// urgency 与 due 决定顺序：逾期的最上，然后按截止日期从近到远（B4）。
interface FocusRow { key: string; title: string; meta: string; color: 'red' | 'orange' | 'blue' | 'green'; path: string | { path: string; query: Record<string, string> }; urgency: number; due: string }
const focusRows = ref<FocusRow[]>([])
const focusCounts = ref('')
async function total(url: string, params: object) { try { return Number((await get<{ meta?: { total?: number } }>(url, params)).meta?.total ?? 0) } catch { return 0 } }
const today = new Date().toISOString().slice(0, 10)
async function loadFocusRows() {
  const rows: FocusRow[] = []
  const counts: string[] = []
  if (auth.can('procurement:sourcing:read')) {
    // 逾期 RFQ 排最前：工厂欠我们报价的每一天都在拖客户的报价。
    try {
      const overdue = await get<{ items?: { id: string; rfqNo: string; caseId: string; caseNo: string; supplierName: string; responseDueAt: string; overdueDays: number }[] }>('/factory-rfqs/overdue', { limit: 5 })
      for (const item of overdue.items ?? []) rows.push({ key: `rfq-${item.id}`, title: `${item.caseNo} · ${item.supplierName}`, meta: t('procurementWorkbench.overdueRfqItem', { n: item.overdueDays }), color: 'red', path: `/procurement/sourcing/${item.caseId}`, urgency: 0, due: item.responseDueAt })
      if (overdue.items?.length) counts.push(t('procurementWorkbench.countOverdueRfq', { n: overdue.items.length }))
    } catch { /* 工作台待办加载失败不阻止其他模块使用。 */ }
  }
  if (auth.can('procurement:order:read')) {
    try {
      const approvals = await get<{ orders?: { id: string; poNo: string; supplierName: string; expectedDate: string }[]; meta?: { total?: number } }>('/purchase-orders', { status: 'PENDING_APPROVAL', page_size: 3 })
      for (const item of approvals.orders ?? []) rows.push({ key: `approve-${item.id}`, title: `${item.poNo} · ${item.supplierName}`, meta: t('procurementWorkbench.approveOrderItem'), color: 'orange', path: { path: '/purchase-orders', query: { status: 'PENDING_APPROVAL' } }, urgency: 1, due: item.expectedDate || '9999-12-31' })
      const n = Number(approvals.meta?.total ?? 0)
      if (n) counts.push(t('procurementWorkbench.countApprovals', { n }))
    } catch { /* 同上。 */ }
  }
  if (auth.can('procurement:sourcing:read')) {
    try {
      const costing = await get<{ sourcingCases?: { id: string; caseNo: string; title: string; customerName: string }[] }>('/sourcing-cases', { status: 'COSTING', page_size: 3 })
      for (const item of costing.sourcingCases ?? []) rows.push({ key: `cost-${item.id}`, title: `${item.caseNo} · ${item.customerName || item.title}`, meta: t('procurementWorkbench.costScenarioItem'), color: 'blue', path: `/procurement/sourcing/${item.id}`, urgency: 2, due: '9999-12-31' })
      const pending = await get<{ sourcingCases?: { id: string; caseNo: string; title: string; customerName: string }[] }>('/sourcing-cases', { status: 'REVIEWING', page_size: 3 })
      for (const item of pending.sourcingCases ?? []) rows.push({ key: `pending-${item.id}`, title: `${item.caseNo} · ${item.customerName || item.title}`, meta: '销售已复核，等待采购开始寻源', color: 'orange', path: `/procurement/sourcing/${item.id}`, urgency: 3, due: '9999-12-31' })
    } catch { /* 同上。 */ }
  }
  if (auth.can('procurement:requirement:read')) {
    try {
      const requirements = await get<{ requirements?: { id: string; productName: string; customerName: string; requiredDate: string }[] }>('/requirements', { status: 'PENDING', page_size: 5 })
      for (const item of requirements.requirements ?? []) {
        const overdueReq = !!item.requiredDate && item.requiredDate < today
        rows.push({ key: `requirement-${item.id}`, title: `${item.productName} · ${item.customerName || '—'}`, meta: item.requiredDate ? t('procurementWorkbench.requiredBy', { date: item.requiredDate }) : t('procurementWorkbench.requirementItem'), color: overdueReq ? 'red' : 'green', path: '/requirements', urgency: overdueReq ? 0 : 4, due: item.requiredDate || '9999-12-31' })
      }
    } catch { /* 同上。 */ }
  }
  rows.sort((a, b) => a.urgency - b.urgency || a.due.localeCompare(b.due))
  focusRows.value = rows.slice(0, 6)
  focusCounts.value = counts.join(' · ')
}
async function loadMetrics() {
  const tasks: Promise<void>[] = []
  if (auth.can('procurement:sourcing:read')) tasks.push((async () => { const [reviewing, sourcing, quotes, costing] = await Promise.all([total('/sourcing-cases', { status: 'REVIEWING', page_size: 1 }), total('/sourcing-cases', { status: 'SOURCING', page_size: 1 }), total('/sourcing-cases', { status: 'QUOTES_RECEIVED', page_size: 1 }), total('/sourcing-cases', { status: 'COSTING', page_size: 1 })]); metrics.reviewing = reviewing; metrics.sourcing = sourcing + quotes + costing })())
  if (auth.can('procurement:requirement:read')) tasks.push(total('/requirements', { status: 'PENDING', page_size: 1 }).then((n) => { metrics.requirements = n }))
  if (auth.can('procurement:order:read')) {
    tasks.push(total('/purchase-orders', { status: 'PENDING_APPROVAL', page_size: 1 }).then((n) => { metrics.ordersPendingApproval = n }))
    tasks.push(total('/purchase-orders', { unsent: '1', page_size: 1 }).then((n) => { metrics.ordersToSend = n }))
  }
  await Promise.all(tasks)
}
onMounted(() => Promise.all([loadMetrics(), loadFocusRows()]))
</script>
<style scoped>
.workbench{padding:28px;background:#f2f6f5;min-height:100%;color:#183344}.hero{display:flex;justify-content:space-between;gap:20px;align-items:flex-end;margin-bottom:20px}.hero h1{font-family:Georgia,'Noto Serif SC',serif;font-size:31px;margin:0 0 6px}.hero p{margin:0;color:#71808a}.hero-actions{display:flex;gap:10px;flex-wrap:wrap}.template-link{text-decoration:none}.intake-band{display:grid;grid-template-columns:1fr 40px 1fr 54px 1.05fr;align-items:center;padding:20px 22px;background:#173746;color:white;border-radius:14px;box-shadow:0 9px 24px rgb(17 53 68 / 13%)}.source{display:flex;align-items:center;gap:12px;padding:8px;border:0;border-radius:10px;background:transparent;color:inherit;text-align:left;cursor:pointer}.source:hover{background:#204757}.source--passive{cursor:default}.source--passive:hover{background:transparent}.source>span{display:grid;place-items:center;width:40px;height:40px;border-radius:10px;background:#245161;color:#72ddc5;font-size:20px}.source div{display:flex;flex-direction:column;gap:4px}.source small{color:#b8cbd1}.source em{color:#72ddc5;font-size:12px;font-style:normal;font-weight:700}.intake-band>b,.flow-arrow{text-align:center;color:#7997a1}.pending-target{display:flex;flex-direction:column;gap:4px;padding:14px 18px;border-radius:10px;background:#e7f6f0;color:#173746;text-decoration:none}.pending-target small{color:#568078}.pending-target strong{font-size:20px}.pending-target span{color:#147a6d;font-size:13px;font-weight:700}.metrics{display:grid;grid-template-columns:repeat(4,1fr);gap:14px;margin:18px 0}.metric{display:flex;flex-direction:column;gap:7px;padding:18px;border:1px solid #dbe5e3;border-radius:12px;background:#fff;color:inherit;text-decoration:none}.metric:hover{border-color:#66aa9e}.metric span{font-size:13px;color:#667985}.metric strong{font-size:29px}.metric small{color:#8a979e}.metric--teal{border-top:3px solid #1a8a7a}.card{background:#fff;border:1px solid #dbe5e3;border-radius:12px;padding:20px}.card-head{display:flex;justify-content:space-between;align-items:start;margin-bottom:10px}.card-head h2{margin:0;font-size:19px}.card-head>span{font-size:12px;color:#9aa5aa}.focus-row{display:grid;grid-template-columns:18px 1fr 20px;gap:10px;align-items:center;width:100%;padding:15px 4px;border:0;border-top:1px solid #edf1f0;background:transparent;text-align:left;cursor:pointer;color:inherit}.focus-row div{display:flex;flex-direction:column;gap:4px}.focus-row small{color:#7e8b92}.focus-row>b{font-size:22px;color:#94a39f}.empty-focus{display:flex;min-height:145px;flex-direction:column;align-items:center;justify-content:center;gap:8px;border-top:1px solid #edf1f0;color:#60727c;text-align:center}.empty-focus strong{font-size:17px;color:#294554}.empty-focus small{color:#87969e}.empty-focus>div{display:flex;gap:10px;margin-top:8px}.dot{width:9px;height:9px;border-radius:50%}.dot--red{background:#c0392b}.dot--orange{background:#dc8b32}.dot--blue{background:#387da8}.dot--green{background:#258d73}.metric--split .split-rows{display:flex;gap:18px}.split-row{display:flex;flex-direction:column;gap:2px;color:inherit;text-decoration:none}.split-row strong{font-size:29px}.split-row small{color:#8a979e}.split-row:hover strong{color:#1a8a7a}@media(max-width:950px){.metrics{grid-template-columns:repeat(2,1fr)}.intake-band{grid-template-columns:1fr}.intake-band>b,.flow-arrow{transform:rotate(90deg);padding:8px}.hero{align-items:flex-start;flex-direction:column}}
</style>
