<template>
  <div class="page">
    <header class="page-head">
      <div>
        <div class="eyebrow">{{ t('procurementIntakes.eyebrow') }}</div>
        <h1>{{ t('procurementIntakes.title') }}</h1>
        <p>{{ t('procurementIntakes.subtitle') }}</p>
      </div>
      <div class="head-actions"><el-button @click="router.push('/procurement')">← {{ t('procurementNav.backToWorkbench') }}</el-button><el-button v-if="canWrite" type="primary" @click="uploadOpen = true">{{ t('procurementIntakes.manualUpload') }}</el-button></div>
    </header>

    <section class="panel">
      <div class="filters">
        <el-input v-model="keyword" clearable :placeholder="t('procurementIntakes.search')" @keyup.enter="load" />
        <el-button type="primary" @click="load">{{ t('common.query') }}</el-button>
      </div>
      <el-table v-loading="loading" :data="rows" stripe>
        <el-table-column prop="caseNo" :label="t('procurementIntakes.number')" width="190" />
        <el-table-column :label="t('procurementIntakes.source')" width="145">
          <template #default="{ row }"><el-tag effect="plain" :type="Number(row.sourceMailId) > 0 ? 'success' : 'info'">{{ Number(row.sourceMailId) > 0 ? t('procurementIntakes.mailAuto') : t('procurementIntakes.manual') }}</el-tag></template>
        </el-table-column>
        <el-table-column prop="title" :label="t('procurementIntakes.fileOrSubject')" min-width="220" show-overflow-tooltip />
        <el-table-column prop="customerName" :label="t('procurementIntakes.customer')" min-width="150"><template #default="{ row }">{{ row.customerName || '—' }}</template></el-table-column>
        <el-table-column :label="t('procurementIntakes.lines')" width="90" align="center"><template #default="{ row }">{{ row.lines?.length ?? 0 }}</template></el-table-column>
        <el-table-column prop="createdAt" :label="t('procurementIntakes.receivedAt')" width="185"><template #default="{ row }">{{ formatTime(row.createdAt) }}</template></el-table-column>
        <el-table-column :label="t('common.actions')" width="125" fixed="right">
          <template #default="{ row }">
            <el-button type="primary" size="small" @click="openDetail(row)">{{ t('procurementIntakes.review') }}</el-button>
          </template>
        </el-table-column>
        <template #empty>{{ t('procurementIntakes.empty') }}</template>
      </el-table>
      <el-pagination v-model:current-page="page" :page-size="20" :total="total" layout="total, prev, pager, next" @current-change="load" />
    </section>

    <el-dialog v-model="uploadOpen" :title="t('procurementIntakes.uploadTitle')" width="620px" destroy-on-close>
      <el-alert :title="t('procurementIntakes.uploadHint')" type="info" :closable="false" show-icon />
      <el-form label-width="110px" class="upload-form">
        <el-form-item :label="t('procurementIntakes.inquiryTitle')"><el-input v-model="uploadForm.title" :placeholder="t('procurementIntakes.titleAuto')" /></el-form-item>
        <el-form-item :label="t('procurementIntakes.customer')"><el-input v-model="uploadForm.customerName" /></el-form-item>
        <el-form-item :label="t('procurementIntakes.contact')"><el-input v-model="uploadForm.contactName" /></el-form-item>
        <el-form-item :label="t('procurementIntakes.contactEmail')"><el-input v-model="uploadForm.contactEmail" /></el-form-item>
        <el-form-item :label="t('procurementIntakes.standardFile')" required>
          <input type="file" accept=".xlsx,.csv" @change="pickFile" />
        </el-form-item>
      </el-form>
      <template #footer><el-button @click="uploadOpen = false">{{ t('common.cancel') }}</el-button><el-button type="primary" :loading="saving" @click="upload">{{ t('procurementIntakes.uploadAndReview') }}</el-button></template>
    </el-dialog>

    <el-dialog v-model="detailOpen" :title="detail?.caseNo || t('procurementIntakes.reviewTitle')" width="min(1280px, 94vw)" top="5vh" destroy-on-close>
      <div v-if="detail" class="detail-summary">
        <div><small>{{ t('procurementIntakes.fileOrSubject') }}</small><strong>{{ detail.title }}</strong></div>
        <div><small>{{ t('procurementIntakes.customer') }}</small><strong>{{ detail.customerName || '—' }}</strong></div>
        <div><small>{{ t('procurementIntakes.source') }}</small><strong>{{ Number(detail.sourceMailId) > 0 ? t('procurementIntakes.mailAuto') : t('procurementIntakes.manual') }}</strong></div>
      </div>
      <el-alert :title="t('procurementIntakes.reviewHint')" type="warning" :closable="false" show-icon class="review-alert" />
      <el-table v-if="detail" :data="detail.lines" border max-height="52vh">
        <el-table-column prop="lineNo" label="#" width="54" />
        <el-table-column :label="t('procurementIntakes.product')" min-width="150"><template #default="{ row }"><el-input v-model="row.extracted.product" :disabled="row.decision === 'SKIPPED'" /></template></el-table-column>
        <el-table-column :label="t('procurementIntakes.material')" min-width="145"><template #default="{ row }"><el-input v-model="row.extracted.materialStandard" :disabled="row.decision === 'SKIPPED'" /></template></el-table-column>
        <el-table-column :label="t('procurementIntakes.grade')" min-width="120"><template #default="{ row }"><el-input v-model="row.extracted.grade" :disabled="row.decision === 'SKIPPED'" /></template></el-table-column>
        <el-table-column :label="t('procurementIntakes.spec')" min-width="180"><template #default="{ row }"><el-input v-model="row.extracted.thickness" :disabled="row.decision === 'SKIPPED'" placeholder="T" /><el-input v-model="row.extracted.width" :disabled="row.decision === 'SKIPPED'" placeholder="W" class="stack-input" /></template></el-table-column>
        <el-table-column :label="t('procurementIntakes.quantity')" min-width="155"><template #default="{ row }"><div class="qty"><el-input v-model="row.extracted.quantity" :disabled="row.decision === 'SKIPPED'" /><el-input v-model="row.extracted.quantityUnit" :disabled="row.decision === 'SKIPPED'" /></div></template></el-table-column>
        <el-table-column :label="t('procurementIntakes.deliveryPort')" min-width="170"><template #default="{ row }"><el-input v-model="row.extracted.delivery" :disabled="row.decision === 'SKIPPED'" /><el-input v-model="row.extracted.port" :disabled="row.decision === 'SKIPPED'" class="stack-input" /></template></el-table-column>
        <el-table-column v-for="column in customColumns" :key="column.key" :label="column.label" min-width="140">
          <template #default="{ row }">
            <el-input
              :model-value="row.extracted.customFields?.[column.key] ?? ''"
              :disabled="row.decision === 'SKIPPED'"
              @input="(value: string) => setCustomField(row, column.key, value)"
            />
          </template>
        </el-table-column>
        <el-table-column :label="t('common.actions')" width="105" fixed="right"><template #default="{ row }"><el-button link :type="row.decision === 'SKIPPED' ? 'success' : 'danger'" @click="row.decision = row.decision === 'SKIPPED' ? 'PENDING' : 'SKIPPED'">{{ row.decision === 'SKIPPED' ? t('procurementIntakes.restore') : t('procurementIntakes.ignore') }}</el-button></template></el-table-column>
      </el-table>
      <template #footer><el-button @click="detailOpen = false">{{ t('common.cancel') }}</el-button><el-button v-if="canWrite" type="primary" :loading="saving" @click="confirmIntake">{{ t('procurementIntakes.confirmCreate') }}</el-button></template>
    </el-dialog>
  </div>
</template>

<script setup lang="ts">
import { onMounted, reactive, ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { useI18n } from 'vue-i18n'
import { useRoute, useRouter } from 'vue-router'
import { get, http, post, put, type Envelope } from '../api'
import { useAuthStore } from '../stores/auth'

interface Extracted { product: string; materialStandard: string; grade: string; thickness: string; width: string; quantity: string; quantityUnit: string; delivery: string; port: string; customFields?: Record<string, string> }
interface IntakeLine { id: string; lineNo: number; decision: string; extracted: Extracted }
interface Intake { id: string; caseNo: string; title: string; customerName: string; contactName: string; contactEmail: string; sourceMailId: string; sourceFileName: string; createdAt: string; inquiryTemplateId?: string; lines?: IntakeLine[] }

const { t } = useI18n()
const auth = useAuthStore()
const route = useRoute()
const router = useRouter()
const canWrite = auth.can('procurement:sourcing:write')
const loading = ref(false), saving = ref(false), uploadOpen = ref(false), detailOpen = ref(false)
const rows = ref<Intake[]>([]), detail = ref<Intake | null>(null), keyword = ref(''), page = ref(1), total = ref(0)
const uploadForm = reactive({ title: '', customerName: '', contactName: '', contactEmail: '', file: null as File | null })
// 模板自定义列（custom.*）：复核页按案件读取时的模板版本解析表头。
const customColumns = ref<{ key: string; label: string }[]>([])

function setCustomField(line: IntakeLine, key: string, value: string) {
  const fields = { ...(line.extracted.customFields ?? {}) }
  if (value === '') delete fields[key]; else fields[key] = value
  line.extracted.customFields = fields
}

async function resolveCustomColumns(intake: Intake) {
  const keys = new Set<string>()
  for (const line of intake.lines ?? []) {
    for (const key of Object.keys(line.extracted.customFields ?? {})) keys.add(key)
  }
  if (!keys.size) { customColumns.value = []; return }
  const labels: Record<string, string> = {}
  if (intake.inquiryTemplateId && Number(intake.inquiryTemplateId) > 0) {
    try {
      const data = await get<{ template: { fields?: { fieldKey: string; displayName: string }[] } }>(`/inquiry-templates/${intake.inquiryTemplateId}`)
      for (const field of data.template.fields ?? []) labels[field.fieldKey] = field.displayName
    } catch { /* 模板读取失败时退化为字段标识 */ }
  }
  customColumns.value = [...keys].map((key) => ({ key, label: labels[key] || key.replace(/^custom\./, '') }))
}

function formatTime(value: string) { return value ? new Date(value).toLocaleString() : '—' }
function pickFile(event: Event) { uploadForm.file = (event.target as HTMLInputElement).files?.[0] ?? null }

async function load() {
  loading.value = true
  try {
    const data = await get<{ sourcingCases: Intake[]; meta: { total: number } }>('/sourcing-cases', { status: 'INTAKE_PENDING', keyword: keyword.value, page: page.value, page_size: 20 })
    rows.value = await Promise.all((data.sourcingCases ?? []).map(async (item) => {
      try { return (await get<{ sourcingCase: Intake }>(`/sourcing-cases/${item.id}`)).sourcingCase } catch { return item }
    })); total.value = Number(data.meta?.total ?? 0)
    const target = String(route.query.intake ?? '')
    if (target) { const found = rows.value.find((item) => String(item.id) === target); if (found) await openDetail(found) }
  } finally { loading.value = false }
}

async function upload() {
  if (!uploadForm.file) { ElMessage.warning(t('procurementIntakes.fileRequired')); return }
  const body = new FormData()
  body.append('file', uploadForm.file); body.append('title', uploadForm.title); body.append('customer_name', uploadForm.customerName); body.append('contact_name', uploadForm.contactName); body.append('contact_email', uploadForm.contactEmail)
  saving.value = true
  try {
    const response = await http.post<Envelope<{ sourcingCase: Intake }>>('/sourcing-intakes/import', body)
    uploadOpen.value = false; ElMessage.success(t('procurementIntakes.uploaded')); await load(); await openDetail(response.data.data!.sourcingCase)
  } finally { saving.value = false }
}

async function openDetail(row: Intake) {
  const data = await get<{ sourcingCase: Intake }>(`/sourcing-cases/${row.id}`)
  detail.value = data.sourcingCase; detailOpen.value = true
  await resolveCustomColumns(data.sourcingCase)
}

async function saveLine(line: IntakeLine) {
  await put(`/sourcing-cases/${detail.value!.id}/lines/${line.id}`, { extracted: line.extracted, decision: line.decision })
}

async function confirmIntake() {
  if (!detail.value) return
  const active = detail.value.lines?.filter((line) => line.decision !== 'SKIPPED') ?? []
  if (!active.length) { ElMessage.warning(t('procurementIntakes.keepOne')); return }
  const incomplete = active.filter((line) => !line.extracted.product?.trim() || !line.extracted.quantity?.trim() || !line.extracted.quantityUnit?.trim())
  if (incomplete.length) { ElMessage.warning(t('procurementIntakes.requiredMissing', { lines: incomplete.map((line) => line.lineNo).join('、') })); return }
  await ElMessageBox.confirm(t('procurementIntakes.confirmHint'), t('procurementIntakes.confirmCreate'), { type: 'warning' })
  saving.value = true
  try {
    for (const line of detail.value.lines ?? []) await saveLine(line)
    await post(`/sourcing-cases/${detail.value.id}/confirm-lines`, { sourcingLineIds: active.map((line) => Number(line.id)) })
    ElMessage.success(t('procurementIntakes.confirmed')); detailOpen.value = false; await router.push({ path: '/sourcing-cases', query: { case: detail.value.id } })
  } finally { saving.value = false }
}

onMounted(async () => {
  await load()
  if (route.query.upload === '1' && canWrite) uploadOpen.value = true
})
</script>

<style scoped>
.page{padding:28px;background:#f4f7f7;min-height:100%}.page-head{display:flex;justify-content:space-between;align-items:flex-start;margin-bottom:18px}.head-actions{display:flex;gap:10px}.eyebrow{color:#16766b;font-size:12px;font-weight:700;letter-spacing:.12em;text-transform:uppercase}.page-head h1{margin:5px 0 4px;font-size:26px;color:#173042}.page-head p{margin:0;color:#71808b}.panel{background:#fff;border:1px solid #dfe8e6;border-radius:12px;padding:18px}.filters{display:flex;gap:10px;width:460px;margin-bottom:14px}.row-actions{display:flex;align-items:center;gap:8px;white-space:nowrap}.el-pagination{justify-content:flex-end;margin-top:16px}.upload-form{margin-top:20px}.detail-summary{display:grid;grid-template-columns:2fr 1fr 1fr;gap:14px;margin-bottom:14px}.detail-summary>div{display:flex;flex-direction:column;gap:4px;padding:11px 14px;background:#f4f7f7;border-radius:8px}.detail-summary small{color:#7b8992}.review-toolbar{display:flex;justify-content:space-between;align-items:center;gap:12px;margin-bottom:14px;padding:10px 14px;border:1px solid #dce8e5;border-radius:8px;background:#f7fbfa;color:#536873}.review-alert{margin-bottom:14px}.stack-input{margin-top:6px}.qty{display:grid;grid-template-columns:1fr 70px;gap:6px}.custom-fields{display:grid;gap:8px}.custom-fields label{display:grid;gap:3px}.custom-fields small{color:#71808b}@media(max-width:850px){.filters{width:100%}.detail-summary{grid-template-columns:1fr}.page-head{gap:14px;flex-direction:column}.head-actions{flex-wrap:wrap}.review-toolbar{align-items:flex-start;flex-direction:column}}
</style>
