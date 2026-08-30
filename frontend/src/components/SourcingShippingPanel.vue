<template>
  <section class="presales-workspace">
    <div class="queue-intro">
      <div>
        <h3>{{ queueTitle }}</h3>
        <p>{{ queueHint }}</p>
      </div>
      <div class="intro-actions">
        <el-button @click="load">{{ t('common.refresh') }}</el-button>
      </div>
    </div>

    <div class="work-steps">
      <div><span>1</span><strong>{{ t('presalesShipping.stepAccept') }}</strong><small>{{ t('presalesShipping.stepAcceptHint') }}</small></div>
      <i>→</i>
      <div><span>2</span><strong>{{ t('presalesShipping.stepContact') }}</strong><small>{{ t('presalesShipping.stepContactHint') }}</small></div>
      <i>→</i>
      <div><span>3</span><strong>{{ t('presalesShipping.managerPlan') }}</strong><small>{{ t('presalesShipping.managerPlanHint') }}</small></div>
    </div>

    <el-row :gutter="12" class="task-stats" v-loading="summaryLoading">
      <el-col v-for="item in statItems" :key="item.status" :xs="24" :sm="12" :md="6">
        <el-card shadow="hover" class="task-stat" :class="[`is-${item.tone}`, { active: status === item.status }]" @click="selectStatus(item.status)">
          <div class="stat-top"><span>{{ item.label }}</span><strong>{{ item.value }}</strong></div>
          <p>{{ item.hint }}</p>
        </el-card>
      </el-col>
    </el-row>

    <el-card shadow="never" class="task-card">
      <div class="filters">
        <el-input v-model="keyword" clearable :placeholder="t('presalesShipping.searchPlaceholder')" @keyup.enter="search" @clear="search" />
        <el-button type="primary" @click="search">{{ t('common.query') }}</el-button>
      </div>

      <div v-loading="loading" class="task-list">
        <el-card v-for="row in tasks" :key="row.caseId" shadow="never" class="sourcing-task-card">
          <div class="task-card-head">
            <div class="task-identity">
              <div class="title-line"><el-link underline="never" @click="showDetail(row)"><strong>{{ row.caseTitle || row.caseNo }}</strong></el-link><span>{{ row.caseNo }}</span></div>
              <small>{{ t('presalesShipping.requirementVersion') }} V{{ row.requirementVersionNo || 1 }}</small>
            </div>
            <el-tag :type="taskStatus(row.status).type" effect="light" round>{{ taskStatus(row.status).label }}</el-tag>
          </div>

          <div class="task-card-content">
            <div class="task-facts">
              <div><span>{{ t('presalesShipping.customer') }}</span><strong>{{ row.customerName || t('presalesShipping.customerMissing') }}</strong></div>
              <div><span>{{ t('presalesShipping.salesOwner') }}</span><strong>{{ row.salesEmployeeName || '—' }}</strong></div>
              <div><span>{{ t('presalesShipping.dischargePort') }}</span><strong>{{ row.destinationPort || t('presalesShipping.salesToFill') }}</strong></div>
            </div>
            <div class="cargo-box">
              <div class="cargo-head"><span>{{ t('presalesShipping.cargoSpecQty') }}</span><small v-if="cargoItems(row).length > 3">{{ t('presalesShipping.showingFirstThree', { total: cargoItems(row).length }) }}</small></div>
              <div class="cargo-list">
                <div v-for="(cargo, index) in cargoItems(row).slice(0, 3)" :key="index" class="cargo-line"><span>{{ index + 1 }}</span><p>{{ cargo }}</p></div>
                <div v-if="!cargoItems(row).length" class="cargo-empty">{{ t('presalesShipping.cargoMissing') }}</div>
              </div>
            </div>
          </div>

          <div class="task-card-actions">
            <span>{{ nextStepHint(row.status) }}</span>
            <el-button v-if="canWrite || canApprove" type="primary" @click="handlePrimary(row)">{{ primaryActionLabel(row.status) }}</el-button>
          </div>
        </el-card>
      </div>

      <el-empty v-if="!loading && !tasks.length" :description="t('presalesShipping.noTasks')" />
      <el-pagination class="pager" layout="total, prev, pager, next" :total="total" :page-size="20" :current-page="page" @current-change="changePage" />
    </el-card>

    <el-drawer v-model="detailOpen" :title="t('presalesShipping.detailTitle')" size="min(1280px, 96vw)">
      <template v-if="collaboration?.shippingRequest">
        <el-alert class="detail-alert" type="info" :closable="false" :title="t('presalesShipping.detailHint')" />
        <el-descriptions :column="2" border>
          <el-descriptions-item :label="t('presalesShipping.case')">{{ collaboration.shippingRequest.caseNo }} · {{ collaboration.shippingRequest.caseTitle }}</el-descriptions-item>
          <el-descriptions-item :label="t('presalesShipping.salesOwner')">{{ collaboration.shippingRequest.salesEmployeeName || '—' }}</el-descriptions-item>
          <el-descriptions-item :label="t('presalesShipping.cargo')" :span="2">{{ cargoItems(collaboration.shippingRequest).join('；') || '—' }}</el-descriptions-item>
          <el-descriptions-item :label="t('presalesShipping.dischargePort')">{{ collaboration.shippingRequest.destinationPort || t('presalesShipping.pending') }}</el-descriptions-item>
          <el-descriptions-item :label="t('presalesShipping.requirementVersion')">V{{ collaboration.shippingRequest.requirementVersionNo || 1 }}</el-descriptions-item>
        </el-descriptions>
        <section class="team-panel">
          <div class="section-title"><div><h4>{{ t('presalesShipping.shippingTeam') }}</h4><p>{{ t('presalesShipping.shippingTeamHint') }}</p></div><el-button v-if="canWrite && !currentParticipant" type="primary" plain @click="joinTask">{{ t('presalesShipping.joinCase') }}</el-button></div>
          <div v-if="collaboration.participants?.length" class="participant-list">
            <div v-for="item in collaboration.participants" :key="item.id" class="participant-item">
              <span><strong>{{ item.employeeName }}</strong><small>{{ item.joinedAt || '—' }}</small></span>
              <span><el-tag v-if="item.role === 'PRIMARY'" type="success">{{ t('presalesShipping.primaryShipping') }}</el-tag><el-tag v-else-if="item.primaryRequestedAt" type="warning">{{ t('presalesShipping.primaryRequested') }}</el-tag><el-tag v-else>{{ t('presalesShipping.collaboratingShipping') }}</el-tag><el-button v-if="canApprove && item.role !== 'PRIMARY'" link type="primary" @click="assignPrimary(item)">{{ t('presalesShipping.setPrimary') }}</el-button></span>
            </div>
          </div>
          <el-empty v-else :image-size="56" :description="t('presalesShipping.noParticipants')" />
          <el-button v-if="canWrite && currentParticipant && !primaryParticipant" link type="primary" @click="requestPrimary">{{ t('presalesShipping.requestPrimary') }}</el-button>
        </section>
        <SourcingShippingQuoteGroups :collaboration="collaboration" />
        <div class="drawer-actions"><el-button v-if="canWrite && currentParticipant" type="primary" @click="openOption(activeTask)">{{ t('presalesShipping.recordFormalQuote') }}</el-button><el-button v-if="canApprove && managerRows.length" type="success" @click="openPlan">{{ t('presalesShipping.createManagerPlan') }}</el-button></div>
        <section v-if="collaboration.plans?.length" class="plans-panel">
          <h4>{{ t('presalesShipping.managerPlans') }}</h4>
          <el-card v-for="plan in collaboration.plans" :key="plan.id" shadow="never" class="plan-card">
            <header><span><strong>{{ plan.planNo }}</strong><small>{{ plan.managerNote }}</small></span><span><el-tag :type="plan.status === 'SUBMITTED_TO_SALES' ? 'success' : plan.status === 'SUPERSEDED' ? 'info' : 'warning'">{{ plan.status }}</el-tag><el-button v-if="canApprove && plan.status === 'CONFIRMED'" link type="primary" @click="submitPlan(plan)">{{ t('presalesShipping.submitToSales') }}</el-button></span></header>
            <el-table :data="plan.items"><el-table-column prop="productName" :label="t('presalesShipping.cargo')"/><el-table-column :label="t('presalesShipping.company')"><template #default="{row}">{{ row.carrierForwarder }}<small>{{ row.serviceOptionName || t('presalesShipping.defaultServiceOption') }}</small></template></el-table-column><el-table-column :label="t('presalesShipping.totalFreight')"><template #default="{ row }">{{ row.currency }} {{ row.totalFreight }}</template></el-table-column><el-table-column :label="t('presalesShipping.departureArrival')"><template #default="{ row }">{{ row.estimatedDeparture || '—' }} → {{ row.estimatedArrival || '—' }}</template></el-table-column><el-table-column prop="selectionType" :label="t('presalesShipping.selectionType')"/></el-table>
          </el-card>
        </section>
      </template>
    </el-drawer>

    <el-dialog v-model="optionOpen" :title="t('presalesShipping.recordFormalQuote')" width="min(1120px, 96vw)">
      <el-alert type="info" :closable="false" :title="t('presalesShipping.formalQuoteHint')" />
      <el-form label-width="132px" class="option-form">
        <div class="form-grid">
          <el-form-item :label="t('presalesShipping.company')" required>
            <el-select v-model="form.carrierForwarder" filterable allow-create default-first-option clearable :loading="lookupLoading" :placeholder="t('presalesShipping.companyPlaceholder')">
              <el-option v-for="company in carrierOptions" :key="company" :label="company" :value="company" />
            </el-select>
          </el-form-item>
          <el-form-item :label="t('presalesShipping.serviceOption')"><el-input v-model="form.serviceOptionName" :placeholder="t('presalesShipping.serviceOptionPlaceholder')" /></el-form-item>
          <el-form-item :label="t('presalesShipping.quotedAt')"><el-date-picker v-model="form.quotedAt" type="date" value-format="YYYY-MM-DD" /></el-form-item>
          <el-form-item :label="t('presalesShipping.validUntil')"><el-date-picker v-model="form.validUntil" type="date" value-format="YYYY-MM-DD" /></el-form-item>
          <el-form-item :label="t('presalesShipping.loadingPort')">
            <el-select v-model="form.portOfLoading" filterable allow-create default-first-option clearable :loading="lookupLoading"><el-option v-for="port in portOptions" :key="`loading-${port.value}`" :label="port.label" :value="port.value" /></el-select>
          </el-form-item>
          <el-form-item :label="t('presalesShipping.dischargePort')">
            <el-select v-model="form.portOfDischarge" filterable allow-create default-first-option clearable :loading="lookupLoading"><el-option v-for="port in portOptions" :key="`discharge-${port.value}`" :label="port.label" :value="port.value" /></el-select>
          </el-form-item>
          <el-form-item :label="t('presalesShipping.estimatedDeparture')"><el-date-picker v-model="form.estimatedDeparture" type="date" value-format="YYYY-MM-DD" /></el-form-item>
          <el-form-item :label="t('presalesShipping.estimatedArrival')"><el-date-picker v-model="form.estimatedArrival" type="date" value-format="YYYY-MM-DD" /></el-form-item>
        </div>
        <div class="cargo-pricing-head"><div><h4>{{ t('presalesShipping.cargoFreight') }}</h4><p>{{ t('presalesShipping.unquotedCanBeBlank') }}</p></div><span>{{ t('presalesShipping.quotedCount', { quoted: quotedLineCount, total: form.lines.length }) }}</span></div>
        <div class="table-scroll"><el-table :data="form.lines" border style="min-width:980px">
          <el-table-column :label="t('presalesShipping.cargoAndSpec')" min-width="240"><template #default="{ row }"><strong>{{ row.product }}</strong><small>{{ cargoSpecification(row) }}</small><small>{{ row.quantity }} {{ row.quantityUnit }}</small></template></el-table-column>
          <el-table-column :label="t('presalesShipping.currency')" width="105"><template #default="{ row }"><el-select v-model="row.currency" filterable allow-create default-first-option><el-option v-for="currency in currencyOptions" :key="currency" :label="currency" :value="currency" /></el-select></template></el-table-column>
          <el-table-column :label="t('presalesShipping.chargeBasis')" width="135"><template #default="{ row }"><el-select v-model="row.chargeBasis" @change="syncTotalFreight(row)"><el-option value="PER_TON" :label="t('presalesShipping.perTon')" /><el-option value="PER_CONTAINER" :label="t('presalesShipping.perContainer')" /><el-option value="PER_PIECE" :label="t('presalesShipping.perPiece')" /><el-option value="PER_SHIPMENT" :label="t('presalesShipping.perShipment')" /><el-option value="FIXED" :label="t('presalesShipping.fixedAmount')" /></el-select></template></el-table-column>
          <el-table-column :label="t('presalesShipping.unitPriceBlank')" width="160"><template #default="{ row }"><el-input v-model="row.unitRate" clearable inputmode="decimal" @input="syncTotalFreight(row)" /></template></el-table-column>
          <el-table-column :label="t('presalesShipping.totalFreight')" width="150"><template #default="{ row }"><el-input v-model="row.totalFreight" inputmode="decimal" /></template></el-table-column>
        </el-table></div>
        <el-form-item :label="t('presalesShipping.note')"><el-input v-model="form.note" type="textarea" :rows="2" :placeholder="t('presalesShipping.notePlaceholder')" /></el-form-item>
      </el-form>
      <template #footer><el-button @click="optionOpen = false">{{ t('common.cancel') }}</el-button><el-button type="primary" :loading="saving" @click="saveOption">{{ t('presalesShipping.saveCompanyQuote') }}</el-button></template>
    </el-dialog>

    <el-dialog v-model="planOpen" :title="t('presalesShipping.createManagerPlan')" width="min(1240px, 96vw)">
      <el-alert type="info" :closable="false" :title="t('presalesShipping.planDialogHint')" />
      <el-form label-width="110px" class="plan-form"><el-form-item :label="t('presalesShipping.managerNote')" required><el-input v-model="planForm.managerNote" type="textarea" :rows="2" /></el-form-item></el-form>
      <div class="table-scroll"><el-table :data="planRows" border style="min-width:1120px">
        <el-table-column :label="t('presalesShipping.includeInPlan')" width="95" fixed="left"><template #default="{ row }"><el-checkbox v-model="row.selected">{{ row.selected ? t('presalesShipping.included') : t('presalesShipping.excluded') }}</el-checkbox></template></el-table-column>
        <el-table-column prop="product" :label="t('presalesShipping.cargoAndSpec')" min-width="180"/>
        <el-table-column prop="carrierForwarder" :label="t('presalesShipping.companyAndSubmitter')" min-width="190"><template #default="{ row }"><strong>{{ row.carrierForwarder }}</strong><small>{{ row.serviceOptionName || t('presalesShipping.defaultServiceOption') }} · {{ row.createdByName }}</small></template></el-table-column>
        <el-table-column :label="t('presalesShipping.totalFreight')" width="145"><template #default="{ row }">{{ row.currency }} {{ row.totalFreight }}</template></el-table-column>
        <el-table-column :label="t('presalesShipping.departureArrival')" width="185"><template #default="{ row }">{{ row.estimatedDeparture || '—' }} → {{ row.estimatedArrival || '—' }}</template></el-table-column>
        <el-table-column :label="t('presalesShipping.selectionType')" width="130"><template #default="{ row }"><el-select v-model="row.selectionType" :disabled="!row.selected"><el-option value="RECOMMENDED" :label="t('presalesShipping.recommended')"/><el-option value="BACKUP" :label="t('presalesShipping.backup')"/></el-select></template></el-table-column>
        <el-table-column :label="t('presalesShipping.priority')" width="90"><template #default="{ row }"><el-input-number v-model="row.priority" :min="1" controls-position="right" :disabled="!row.selected"/></template></el-table-column>
        <el-table-column :label="t('presalesShipping.reason')" min-width="180"><template #default="{ row }"><el-input v-model="row.reason" :disabled="!row.selected"/></template></el-table-column>
        <el-table-column :label="t('presalesShipping.risk')" min-width="160"><template #default="{ row }"><el-input v-model="row.risk" :disabled="!row.selected"/></template></el-table-column>
      </el-table></div>
      <template #footer><el-button @click="planOpen=false">{{ t('common.cancel') }}</el-button><el-button type="primary" :loading="savingPlan" @click="createPlan">{{ t('presalesShipping.confirmPlan') }}</el-button></template>
    </el-dialog>
  </section>
</template>

<script setup lang="ts">
import { computed, onMounted, onUnmounted, reactive, ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { useI18n } from 'vue-i18n'
import { get, post } from '../api'
import { onLive } from '../live'
import { useAuthStore } from '../stores/auth'
import SourcingShippingQuoteGroups from './SourcingShippingQuoteGroups.vue'

const auth = useAuthStore()
const { t } = useI18n()
const canWrite = computed(() => auth.can('shipping:sourcing:write'))
const canApprove = computed(() => auth.can('shipping:sourcing:approve'))
const tasks = ref<any[]>([])
const loading = ref(false)
const summaryLoading = ref(false)
const total = ref(0)
const page = ref(1)
const keyword = ref('')
const status = ref('WAITING_PARTICIPATION')
const statusCounts = reactive<Record<string, number>>({ WAITING_PARTICIPATION: 0, QUOTING: 0, MANAGER_REVIEW: 0, PLAN_READY: 0, REQUOTE_REQUIRED: 0, PLAN_SUBMITTED_TO_SALES: 0 })
const detailOpen = ref(false)
const optionOpen = ref(false)
const saving = ref(false)
const lookupLoading = ref(false)
const planOpen = ref(false)
const savingPlan = ref(false)
const planRows = ref<any[]>([])
const planForm = reactive({ managerNote: '' })
const collaboration = ref<any>()
const activeTask = ref<any>()
const form = reactive<any>({ carrierForwarder: '', serviceOptionName: '', portOfLoading: '', portOfDischarge: '', quotedAt: '', estimatedDeparture: '', estimatedArrival: '', validUntil: '', note: '', lines: [] })
const carrierOptions = ref<string[]>([])
const portOptions = ref<{ label: string; value: string }[]>([])
const currencyOptions = ['USD', 'CNY', 'EUR', 'GBP', 'JPY', 'AUD', 'CAD', 'HKD', 'SGD']
const quotedLineCount = computed(() => form.lines.filter((line: any) => String(line.unitRate).trim() !== '').length)
const currentParticipant = computed(() => collaboration.value?.participants?.find((item: any) => String(item.employeeId) === String(auth.employeeId)))
const primaryParticipant = computed(() => collaboration.value?.participants?.find((item: any) => item.role === 'PRIMARY'))
const managerRows = computed(() => {
  const options = collaboration.value?.options || []
  const latest = new Map<string, any>()
  for (const option of options.filter((item: any) => item.status === 'SUBMITTED')) {
    for (const line of option.lines || []) {
      const key = `${line.sourcingLineId}:${option.createdBy}:${String(option.carrierForwarder).trim().toLowerCase()}:${String(option.serviceOptionName).trim().toLowerCase()}`
      if (!latest.has(key)) latest.set(key, { ...line, carrierForwarder: option.carrierForwarder, serviceOptionName: option.serviceOptionName, createdBy: option.createdBy, createdByName: option.createdByName, portOfLoading: option.portOfLoading, portOfDischarge: option.portOfDischarge, estimatedDeparture: option.estimatedDeparture, estimatedArrival: option.estimatedArrival, validUntil: option.validUntil, quoteVersionNo: option.versionNo })
    }
  }
  return [...latest.values()]
})
const statItems = computed(() => [
  { status: 'WAITING_PARTICIPATION', label: t('presalesShipping.statusPending'), value: statusCounts.WAITING_PARTICIPATION, hint: t('presalesShipping.statusPendingHint'), tone: 'warning' },
  { status: 'QUOTING', label: t('presalesShipping.statusInProgress'), value: statusCounts.QUOTING, hint: t('presalesShipping.statusInProgressHint'), tone: 'primary' },
  { status: 'MANAGER_REVIEW', label: t('presalesShipping.statusManagerReview'), value: statusCounts.MANAGER_REVIEW, hint: t('presalesShipping.statusManagerReviewHint'), tone: 'success' },
  { status: 'PLAN_READY', label: t('presalesShipping.statusPlanReady'), value: statusCounts.PLAN_READY, hint: t('presalesShipping.statusPlanReadyHint'), tone: 'success' },
  { status: 'REQUOTE_REQUIRED', label: t('presalesShipping.statusReconfirm'), value: statusCounts.REQUOTE_REQUIRED, hint: t('presalesShipping.statusReconfirmHint'), tone: 'danger' },
  { status: 'PLAN_SUBMITTED_TO_SALES', label: t('presalesShipping.statusSubmitted'), value: statusCounts.PLAN_SUBMITTED_TO_SALES, hint: t('presalesShipping.statusSubmittedHint'), tone: 'success' },
])
const viewingSubmitted = computed(() => status.value === 'PLAN_SUBMITTED_TO_SALES')
const queueTitle = computed(() => t(viewingSubmitted.value ? 'presalesShipping.submittedTitle' : 'presalesShipping.currentTitle'))
const queueHint = computed(() => t(viewingSubmitted.value ? 'presalesShipping.submittedHint' : 'presalesShipping.teamWorkflowHint'))

function taskStatus(value: string) { return ({ WAITING_PARTICIPATION: { label: t('presalesShipping.statusPending'), type: 'warning' }, QUOTING: { label: t('presalesShipping.statusInProgress'), type: 'primary' }, MANAGER_REVIEW: { label: t('presalesShipping.statusManagerReview'), type: 'success' }, PLAN_READY: { label: t('presalesShipping.statusPlanReady'), type: 'warning' }, REQUOTE_REQUIRED: { label: t('presalesShipping.statusReconfirm'), type: 'danger' }, PLAN_SUBMITTED_TO_SALES: { label: t('presalesShipping.statusSubmitted'), type: 'success' } } as any)[value] || { label: value, type: 'info' } }
function primaryActionLabel(value: string) { return ({ WAITING_PARTICIPATION: t('presalesShipping.openAndJoin'), QUOTING: t('presalesShipping.openCase'), MANAGER_REVIEW: t('presalesShipping.reviewPlan'), PLAN_READY: t('presalesShipping.submitToSales'), REQUOTE_REQUIRED: t('presalesShipping.reconfirmAction'), PLAN_SUBMITTED_TO_SALES: t('presalesShipping.viewSubmitted') } as any)[value] || t('common.view') }
function nextStepHint(value: string) { return ({ WAITING_PARTICIPATION: t('presalesShipping.nextAccept'), QUOTING: t('presalesShipping.nextAddQuote'), MANAGER_REVIEW: t('presalesShipping.nextManagerReview'), PLAN_READY: t('presalesShipping.nextSubmitSales'), REQUOTE_REQUIRED: t('presalesShipping.nextReconfirm'), PLAN_SUBMITTED_TO_SALES: t('presalesShipping.nextComplete') } as any)[value] || '' }
function cargoSpecification(row: any) { return [row.materialStandard, row.grade, row.thickness, row.width, row.lengthOrForm, row.surfaceRequirement, row.packaging].filter(Boolean).join(' · ') || t('presalesShipping.noExtraSpec') }
function excelDate(serial: number) { return new Date(Date.UTC(1899, 11, 30) + serial * 86400000).toISOString().slice(0, 10) }
function cargoItems(row: any) {
  const summary = String(row.cargoSummary || '').trim()
  if (!summary) return []
  return summary.split(/[;；]\s*/).map((item) => item.trim()).filter(Boolean).map((item) => item.replace(/(时间要求\s*)(4\d{4})(?=\D|$)/g, (_match, prefix, serial) => `${prefix}${excelDate(Number(serial))}`))
}
async function loadCounts() {
  summaryLoading.value = true
  try {
    const statuses = Object.keys(statusCounts)
    const results = await Promise.all(statuses.map((value) => get<any>('/shipping/sourcing-tasks', { page: 1, page_size: 1, status: value })))
    statuses.forEach((value, index) => { statusCounts[value] = Number(results[index]?.meta?.total || 0) })
  } finally { summaryLoading.value = false }
}
async function load() {
  loading.value = true
  try {
    const data = await get<any>('/shipping/sourcing-tasks', { page: page.value, page_size: 20, keyword: keyword.value, status: status.value })
    tasks.value = data.tasks || []
    total.value = Number(data.meta?.total || 0)
    await loadCounts()
  } finally { loading.value = false }
}
function search() { page.value = 1; void load() }
function selectStatus(value: string) { status.value = value; search() }
function changePage(value: number) { page.value = value; void load() }
async function collaborationFor(row: any) { return get<any>(`/shipping/sourcing-tasks/${row.caseId}`) }
async function showDetail(row: any) { activeTask.value = row; collaboration.value = await collaborationFor(row); detailOpen.value = true }
async function handlePrimary(row: any) {
  await showDetail(row)
}
async function refreshCollaboration() { if (activeTask.value) collaboration.value = await collaborationFor(activeTask.value) }
async function joinTask() { await post(`/shipping/sourcing-tasks/${activeTask.value.caseId}/join`, {}); await refreshCollaboration(); await loadCounts(); ElMessage.success(t('presalesShipping.joined')) }
async function requestPrimary() { await post(`/shipping/sourcing-tasks/${activeTask.value.caseId}/primary-request`, {}); await refreshCollaboration(); ElMessage.success(t('presalesShipping.primaryRequestSent')) }
async function assignPrimary(item: any) {
  let reason = ''
  if (primaryParticipant.value) {
    const result = await ElMessageBox.prompt(t('presalesShipping.changePrimaryPrompt', { current: primaryParticipant.value.employeeName, target: item.employeeName }), t('presalesShipping.changePrimary'), { inputValidator: (value: string) => !!value.trim() || t('presalesShipping.changeReasonRequired') }).catch(() => null)
    if (!result) return
    reason = result.value
  }
  await post(`/shipping/sourcing-tasks/${activeTask.value.caseId}/primary`, { employee_id: Number(item.employeeId), reason })
  await refreshCollaboration()
  ElMessage.success(t('presalesShipping.primaryAssigned'))
}
async function openOption(row: any) {
  activeTask.value = row
  const data = await collaborationFor(row)
  collaboration.value = data
  Object.assign(form, { carrierForwarder: '', serviceOptionName: '', portOfLoading: '', portOfDischarge: data.shippingRequest?.destinationPort || '', quotedAt: new Date().toISOString().slice(0, 10), estimatedDeparture: '', estimatedArrival: '', validUntil: '', note: '', lines: (data.cargoItems || []).map((item: any) => ({ ...item, currency: 'USD', chargeBasis: 'PER_TON', unitRate: '', totalFreight: '', note: '' })) })
  optionOpen.value = true
  void loadQuoteLookups()
}

async function loadQuoteLookups() {
  lookupLoading.value = true
  try {
    const requests: Promise<unknown>[] = []
    if (auth.can('masterdata:port:read')) {
      requests.push(get<{ ports: any[] }>('/ports', { page: 1, page_size: 200, status: 'ACTIVE' }).then((data) => {
        portOptions.value = (data.ports || []).map((port: any) => {
          const name = String(port.nameZh || port.nameEn || port.unlocode || '').trim()
          const label = [port.unlocode, port.nameZh || port.nameEn, port.countryCode].filter(Boolean).join(' · ')
          return { label, value: name }
        }).filter((port) => port.value)
      }))
    }
    if (auth.can('masterdata:supplier:read')) {
      requests.push(Promise.all([
        get<{ suppliers: { name: string }[] }>('/suppliers', { page_size: 200, status: 'ACTIVE', business_type: 'CARRIER' }),
        get<{ suppliers: { name: string }[] }>('/suppliers', { page_size: 200, status: 'ACTIVE', business_type: 'FORWARDER' }),
      ]).then(([carriers, forwarders]) => {
        carrierOptions.value = [...new Set([...(carriers.suppliers || []), ...(forwarders.suppliers || [])].map((item) => item.name).filter(Boolean))]
      }))
    }
    await Promise.allSettled(requests)
  } finally { lookupLoading.value = false }
}
function openPlan() {
  planForm.managerNote = ''
  planRows.value = managerRows.value.map((row: any) => ({ ...row, selected: true, selectionType: 'RECOMMENDED', priority: 1, reason: '', risk: '' }))
  planOpen.value = true
}
async function createPlan() {
  const selected = planRows.value.filter((row: any) => row.selected)
  if (!planForm.managerNote.trim()) { ElMessage.warning(t('presalesShipping.managerNoteRequired')); return }
  if (!selected.length) { ElMessage.warning(t('presalesShipping.planSelectionRequired')); return }
  if (selected.some((row: any) => row.selectionType === 'RECOMMENDED' && !String(row.reason).trim())) { ElMessage.warning(t('presalesShipping.recommendedReasonRequired')); return }
  const priorities = new Set<string>()
  if (selected.some((row: any) => { const key = `${row.sourcingLineId}:${row.selectionType}:${row.priority}`; if (priorities.has(key)) return true; priorities.add(key); return false })) { ElMessage.warning(t('presalesShipping.duplicatePriority')); return }
  const cargoIds = new Set<string>((collaboration.value?.cargoItems || []).map((item: any) => String(item.sourcingLineId)))
  const recommended = new Set<string>(selected.filter((row: any) => row.selectionType === 'RECOMMENDED').map((row: any) => String(row.sourcingLineId)))
  if ([...cargoIds].some((id) => !recommended.has(id))) { ElMessage.warning(t('presalesShipping.eachCargoRecommended')); return }
  savingPlan.value = true
  try {
    await post(`/shipping/sourcing-tasks/${activeTask.value.caseId}/plans`, { manager_note: planForm.managerNote, selections: selected.map((row: any) => ({ sourcing_line_id: Number(row.sourcingLineId), shipping_option_line_id: Number(row.id), selection_type: row.selectionType, priority: Number(row.priority), reason: row.reason, risk: row.risk })) })
    planOpen.value = false
    await refreshCollaboration()
    await load()
    ElMessage.success(t('presalesShipping.planCreated'))
  } finally { savingPlan.value = false }
}
async function submitPlan(plan: any) {
  await ElMessageBox.confirm(t('presalesShipping.submitPlanConfirm', { sales: plan.targetSalesName || '—' }), t('presalesShipping.submitToSales'))
  await post(`/shipping/sourcing-plans/${plan.id}/submit-to-sales`, {})
  await refreshCollaboration()
  await load()
  ElMessage.success(t('presalesShipping.planSubmitted'))
}
function syncTotalFreight(line: any) {
  const rate = Number(line.unitRate)
  const quantity = Number(line.quantity)
  if (!Number.isFinite(rate) || rate < 0) return
  if (line.chargeBasis === 'FIXED' || line.chargeBasis === 'PER_SHIPMENT') line.totalFreight = String(rate)
  else if ((line.chargeBasis === 'PER_TON' || line.chargeBasis === 'PER_PIECE') && Number.isFinite(quantity)) line.totalFreight = String(Number((rate * quantity).toFixed(6)))
}
async function saveOption() {
  const lines = form.lines.filter((line: any) => String(line.unitRate).trim() !== '')
  if (!String(form.carrierForwarder).trim()) { ElMessage.warning(t('presalesShipping.companyRequired')); return }
  if (!lines.length) { ElMessage.warning(t('presalesShipping.lineRequired')); return }
  if (lines.some((line: any) => !String(line.currency).trim() || line.unitRate === '' || Number(line.unitRate) < 0 || line.totalFreight === '' || Number(line.totalFreight) < 0)) { ElMessage.warning(t('presalesShipping.priceRequired')); return }
  if (form.estimatedDeparture && form.estimatedArrival && form.estimatedArrival < form.estimatedDeparture) { ElMessage.warning(t('presalesShipping.arrivalBeforeDeparture')); return }
  saving.value = true
  try {
    await post(`/shipping/sourcing-tasks/${activeTask.value.caseId}/options`, { carrier_forwarder: form.carrierForwarder, service_option_name: form.serviceOptionName, port_of_loading: form.portOfLoading, port_of_discharge: form.portOfDischarge, quoted_at: form.quotedAt, estimated_departure: form.estimatedDeparture, estimated_arrival: form.estimatedArrival, valid_until: form.validUntil, note: form.note, lines: lines.map((line: any) => ({ sourcing_line_id: Number(line.sourcingLineId), currency: String(line.currency).toUpperCase(), charge_basis: line.chargeBasis, unit_rate: line.unitRate, total_freight: line.totalFreight, note: line.note })) })
    optionOpen.value = false
    status.value = 'MANAGER_REVIEW'
    await load()
    if (detailOpen.value) collaboration.value = await collaborationFor(activeTask.value)
    ElMessage.success(t('presalesShipping.quoteSaved'))
  } finally { saving.value = false }
}
const stopLive = onLive((event) => { if (event.type === 'requirement.changed' && !loading.value) void load() })
onMounted(load)
onUnmounted(stopLive)
</script>

<style scoped>
.task-stats :deep(.el-col){max-width:16.666%;flex:0 0 16.666%}@media(max-width:1350px){.task-stats :deep(.el-col){max-width:33.333%;flex:0 0 33.333%}}@media(max-width:760px){.task-stats :deep(.el-col){max-width:100%;flex:0 0 100%}}
.task-stat.is-success::before{background:#67c23a}.team-panel,.plans-panel{margin-top:16px;padding:16px;border:1px solid #dfe8e6;border-radius:10px;background:#f8fbfa}.section-title,.participant-item,.plan-card header{display:flex;align-items:center;justify-content:space-between;gap:16px}.section-title h4,.plans-panel>h4{margin:0;color:#172b4d}.section-title p{margin:5px 0 0;color:#6b778c}.participant-list{display:grid;gap:8px;margin-top:12px}.participant-item{padding:10px 12px;border:1px solid #e5ebef;border-radius:8px;background:#fff}.participant-item>span{display:flex;align-items:center;gap:8px}.participant-item small,.plan-card small{display:block;color:#7b8797}.plans-panel{display:grid;gap:12px}.plan-card header{margin-bottom:12px}.plan-card header>span:last-child{display:flex;align-items:center;gap:8px}.plan-form{margin-top:16px}
.presales-workspace{min-width:0}.queue-intro{display:flex;align-items:flex-start;justify-content:space-between;gap:18px;margin-bottom:14px}.queue-intro h3{margin:0;color:#172b4d;font-size:20px}.queue-intro p{max-width:880px;margin:6px 0 0;color:#6b778c;line-height:1.6}.intro-actions{display:flex;flex-shrink:0;gap:8px}.work-steps{display:grid;grid-template-columns:1fr auto 1fr auto 1fr;align-items:center;gap:14px;margin-bottom:14px;padding:14px 18px;border:1px solid #dfe9e7;border-radius:10px;background:#f5faf9}.work-steps>div{display:grid;grid-template-columns:28px 1fr;column-gap:10px;align-items:center}.work-steps span{grid-row:1/3;display:flex;align-items:center;justify-content:center;width:28px;height:28px;border-radius:50%;background:#167d70;color:#fff;font-weight:700}.work-steps strong{color:#253858}.work-steps small{margin-top:2px;color:#7b8797}.work-steps i{color:#9aa8b7;font-style:normal}.task-stats{margin-bottom:14px}.task-stats :deep(.el-col){margin-bottom:12px}.task-stat{position:relative;height:100%;border-color:#e5eaf0;cursor:pointer;overflow:hidden;transition:.18s ease}.task-stat::before{position:absolute;inset:0 auto 0 0;width:4px;background:#a8b3c2;content:''}.task-stat:hover,.task-stat.active{border-color:#8bb9b0;box-shadow:0 7px 22px rgb(23 43 77 / 8%);transform:translateY(-2px)}.task-stat :deep(.el-card__body){padding:15px 17px}.stat-top{display:flex;align-items:center;justify-content:space-between;gap:10px;color:#526174}.stat-top>span{font-weight:600}.stat-top strong{color:#172b4d;font-size:26px;line-height:1}.task-stat p{margin:8px 0 0;color:#7b8797;font-size:13px}.task-stat.is-primary::before{background:#409eff}.task-stat.is-warning::before{background:#e6a23c}.task-stat.is-danger::before{background:#f56c6c}.task-card{border-color:#e6ebf1;background:#f8fafc}.task-card :deep(>.el-card__body){padding:16px}.filters{display:grid;grid-template-columns:minmax(280px,1fr) auto;gap:10px;margin-bottom:16px}.task-list{display:grid;gap:12px}.sourcing-task-card{border-color:#e1e7ee;border-radius:10px;background:#fff;transition:.18s ease}.sourcing-task-card:hover{border-color:#b9d8d2;box-shadow:0 7px 24px rgb(23 43 77 / 7%)}.sourcing-task-card :deep(.el-card__body){padding:0}.task-card-head{display:flex;align-items:flex-start;justify-content:space-between;gap:14px;padding:15px 18px 13px;border-bottom:1px solid #edf1f5}.task-identity small{display:block;margin-top:5px;color:#8a96a6}.title-line{display:flex;align-items:center;flex-wrap:wrap;gap:9px}.title-line strong{color:#172b4d;font-size:16px}.title-line span{padding-left:9px;border-left:1px solid #d8dee7;color:#6b778c;font-size:13px}.task-card-content{display:grid;grid-template-columns:minmax(300px,.75fr) minmax(420px,1.25fr);gap:18px;padding:15px 18px}.task-facts{display:grid;grid-template-columns:repeat(3,minmax(0,1fr));gap:10px}.task-facts div{min-width:0;padding:10px 12px;border-radius:8px;background:#f7f9fb}.task-facts span,.cargo-head>span{display:block;margin-bottom:5px;color:#7b8797;font-size:12px}.task-facts strong{display:block;overflow:hidden;color:#253858;font-size:14px;text-overflow:ellipsis;white-space:nowrap}.cargo-box{min-width:0}.cargo-head{display:flex;align-items:center;justify-content:space-between;gap:12px}.cargo-head small{color:#8a96a6}.cargo-list{display:grid;gap:5px}.cargo-line{display:grid;grid-template-columns:20px minmax(0,1fr);gap:7px;align-items:start}.cargo-line>span{display:flex;align-items:center;justify-content:center;width:18px;height:18px;border-radius:50%;background:#e9f5f2;color:#167d70;font-size:11px;font-weight:700}.cargo-line p{display:-webkit-box;overflow:hidden;margin:0;color:#44546a;line-height:1.45;-webkit-box-orient:vertical;-webkit-line-clamp:1}.cargo-empty{color:#9aa5b1}.task-card-actions{display:flex;align-items:center;justify-content:space-between;gap:16px;padding:11px 18px;border-top:1px solid #edf1f5;background:#fbfcfd}.task-card-actions>span{color:#6b778c;font-size:13px}.pager{margin-top:16px;justify-content:flex-end}.detail-alert{margin-bottom:14px}.option-list{display:grid;gap:14px;margin-top:16px}.option-card{border-color:#dfe8e6}.option-head{display:flex;justify-content:space-between;gap:12px;margin-bottom:10px}.option-head strong{display:block;color:#172b4d;font-size:17px}.option-head span{display:block;margin-top:4px;color:#6b778c}.option-facts{display:flex;flex-wrap:wrap;gap:8px 18px;margin-bottom:12px;color:#526174;font-size:13px}.option-note{margin:10px 0 0;color:#6b778c}.drawer-actions{display:flex;justify-content:flex-end;margin-top:16px}.option-form{margin-top:16px}.form-grid{display:grid;grid-template-columns:minmax(0,1fr) minmax(0,1fr);column-gap:22px}.form-grid :deep(.el-form-item){margin-bottom:18px}.form-grid :deep(.el-select),.form-grid :deep(.el-input),.form-grid :deep(.el-date-editor){width:100%}.currency-input{width:62px!important}.cargo-pricing-head{display:flex;align-items:flex-end;justify-content:space-between;gap:16px;margin:6px 0 10px}.cargo-pricing-head h4{margin:0;color:#172b4d}.cargo-pricing-head p{margin:5px 0 0;color:#6b778c}.cargo-pricing-head>span{color:#167d70;font-weight:600}.unit{margin-left:8px;color:#7a8998}.table-scroll{overflow-x:auto}.table-scroll small{display:block;margin-top:4px;color:#8993a4}
@media(max-width:1150px){.task-card-content{grid-template-columns:1fr}.task-facts{grid-template-columns:repeat(3,minmax(0,1fr))}.work-steps{gap:9px;padding:13px}}
@media(max-width:760px){.queue-intro{flex-direction:column}.intro-actions{width:100%;flex-wrap:wrap}.work-steps{grid-template-columns:1fr}.work-steps i{display:none}.filters{grid-template-columns:1fr}.task-card-head{padding:13px}.task-card-content{padding:13px}.task-facts{grid-template-columns:1fr}.task-card-actions{align-items:flex-start;flex-direction:column;padding:10px 13px}.task-card-actions :deep(.el-button){width:100%}.pager{justify-content:flex-start}.form-grid{grid-template-columns:1fr}}
</style>
