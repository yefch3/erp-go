<template>
  <div class="page">
    <header class="page-head">
      <div>
        <div class="eyebrow">{{ t('procurementIntakes.eyebrow') }}</div>
        <h1>{{ t('procurementIntakes.title') }}</h1>
        <p>{{ t('procurementIntakes.subtitle') }}</p>
      </div>
      <div class="head-actions"><el-button @click="router.push('/procurement')">← {{ t('procurementNav.backToWorkbench') }}</el-button><el-button v-if="canWrite" type="primary" @click="openUpload">{{ t('procurementIntakes.manualUpload') }}</el-button></div>
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
        <el-form-item :label="t('procurementIntakes.format')">
          <div class="template-picker">
            <el-select v-model="uploadForm.templateId" filterable :loading="templatesLoading" :placeholder="t('procurementIntakes.templatePlaceholder')">
              <el-option value="" :label="t('procurementIntakes.autoRecognize')" />
              <el-option v-for="item in activeTemplates" :key="item.id" :value="String(item.id)" :label="`${item.name} · v${item.version}${item.isDefault ? ` · ${t('procurementIntakes.defaultTemplate')}` : ''}`" />
            </el-select>
            <el-button :disabled="!uploadForm.templateId" @click="downloadSelectedTemplate">{{ t('procurementIntakes.downloadSelectedTemplate') }}</el-button>
          </div>
          <div class="template-help">{{ uploadFormatHint }}</div>
        </el-form-item>
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
        <div><small>{{ t('procurementIntakes.boundFormat') }}</small><strong>{{ resolvedTemplate ? `${resolvedTemplate.name} · v${resolvedTemplate.version}` : t('procurementIntakes.historicalFormat') }}</strong></div>
      </div>
      <el-alert :title="t('procurementIntakes.dynamicReviewHint')" type="warning" :closable="false" show-icon class="review-alert" />
      <div v-if="canWrite" class="review-toolbar">
        <span>{{ t('procurementIntakes.draftHint') }}</span>
        <el-button type="primary" plain @click="openAddLine">+ {{ t('procurementIntakes.addProduct') }}</el-button>
      </div>
      <el-table v-if="detail" :data="detail.lines" border max-height="52vh">
        <el-table-column prop="lineNo" label="#" width="54" />
        <el-table-column v-for="field in displayFields" :key="field.key" :label="field.label" min-width="145">
          <template #default="{ row }">
            <el-input
              :model-value="readTemplateField(row.extracted, field.key)"
              :disabled="row.decision === 'SKIPPED'"
              @update:model-value="(value: string) => writeTemplateField(row.extracted, field.key, String(value ?? ''))"
            />
          </template>
        </el-table-column>
        <el-table-column :label="t('procurementIntakes.completeness')" width="150" fixed="right">
          <template #default="{ row }">
            <el-tag v-if="row.decision === 'SKIPPED'" type="info">{{ t('procurementIntakes.ignored') }}</el-tag>
            <el-tooltip v-else-if="missingFieldLabels(row).length" :content="missingFieldLabels(row).join('、')" placement="top">
              <el-tag type="danger">{{ t('procurementIntakes.missingCount', { count: missingFieldLabels(row).length }) }}</el-tag>
            </el-tooltip>
            <el-tag v-else type="success">{{ t('procurementIntakes.complete') }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column :label="t('common.actions')" width="105" fixed="right"><template #default="{ row }"><el-button link :type="row.decision === 'SKIPPED' ? 'success' : 'danger'" @click="row.decision = row.decision === 'SKIPPED' ? 'PENDING' : 'SKIPPED'">{{ row.decision === 'SKIPPED' ? t('procurementIntakes.restore') : t('procurementIntakes.ignore') }}</el-button></template></el-table-column>
      </el-table>
      <template #footer><el-button @click="detailOpen = false">{{ t('common.cancel') }}</el-button><el-button v-if="canWrite" :loading="saving" @click="saveDraft">{{ t('procurementIntakes.saveDraft') }}</el-button><el-button v-if="canWrite" type="primary" :loading="saving" @click="confirmIntake">{{ t('procurementIntakes.confirmCreate') }}</el-button></template>
    </el-dialog>

    <el-dialog v-model="addLineOpen" :title="t('procurementIntakes.addProductTitle')" width="min(720px, 92vw)" append-to-body destroy-on-close>
      <el-form label-width="115px" class="add-line-form">
        <el-form-item v-for="field in displayFields" :key="field.key" :label="field.label" :required="field.required">
          <el-input :model-value="readTemplateField(addLineForm, field.key)" @update:model-value="(value: unknown) => writeTemplateField(addLineForm, field.key, String(value ?? ''))" />
        </el-form-item>
      </el-form>
      <template #footer><el-button @click="addLineOpen = false">{{ t('common.cancel') }}</el-button><el-button type="primary" :loading="addingLine" @click="addLine">{{ t('procurementIntakes.addAndSave') }}</el-button></template>
    </el-dialog>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { useI18n } from 'vue-i18n'
import { useRoute, useRouter } from 'vue-router'
import { download, get, http, post, put, saveBlob, type Envelope } from '../api'
import { isDialogDismissed } from '../lib/dialogActions'
import type { InquiryTemplate } from '../lib/inquiryTemplates'
import { useAuthStore } from '../stores/auth'

interface Extracted { product: string; materialStandard: string; grade: string; thickness: string; width: string; quantity: string; quantityUnit: string; delivery: string; port: string; customFields?: Record<string, string> }
interface IntakeLine { id: string; lineNo: number; decision: string; extracted: Extracted }
interface Intake { id: string; caseNo: string; title: string; customerName: string; contactName: string; contactEmail: string; sourceMailId: string; sourceFileName: string; createdAt: string; inquiryTemplateId?: string; lines?: IntakeLine[] }
interface TemplateField { fieldKey: string; displayName: string; isRequired: boolean; sortOrder: number }

const { t } = useI18n()
const auth = useAuthStore()
const route = useRoute()
const router = useRouter()
const canWrite = auth.can('procurement:sourcing:write')
const loading = ref(false), saving = ref(false), addingLine = ref(false), templatesLoading = ref(false), uploadOpen = ref(false), detailOpen = ref(false), addLineOpen = ref(false)
const rows = ref<Intake[]>([]), detail = ref<Intake | null>(null), keyword = ref(''), page = ref(1), total = ref(0)
const uploadForm = reactive({ templateId: '', title: '', customerName: '', contactName: '', contactEmail: '', file: null as File | null })
const templates = ref<InquiryTemplate[]>([])
const resolvedTemplate = ref<InquiryTemplate | null>(null)
const activeTemplates = computed(() => templates.value.filter((item) => item.status === 'ACTIVE'))
const selectedUploadTemplate = computed(() => activeTemplates.value.find((item) => String(item.id) === uploadForm.templateId) ?? null)
const uploadFormatHint = computed(() => selectedUploadTemplate.value
  ? t('procurementIntakes.selectedFormatHint', { name: selectedUploadTemplate.value.name, version: selectedUploadTemplate.value.version })
  : t('procurementIntakes.autoFormatHint'))
const emptyExtracted = (): Extracted => ({ product: '', materialStandard: '', grade: '', thickness: '', width: '', quantity: '', quantityUnit: '', delivery: '', port: '', customFields: {} })
const addLineForm = reactive<Extracted>(emptyExtracted())
const templateFields = ref<TemplateField[]>([])
const fallbackFields = ['product', 'material_standard', 'grade', 'thickness', 'width', 'quantity', 'quantity_unit', 'delivery', 'port']
const templateProperty: Record<string, string> = { material_standard: 'materialStandard', length_or_form: 'lengthOrForm', surface_requirement: 'surfaceRequirement', coil_weight: 'coilWeight', coil_id: 'coilId', payment_terms: 'paymentTerms', quantity_unit: 'quantityUnit' }
const fallbackLabels: Record<string, string> = { product: '产品', material_standard: '材质/标准', grade: '牌号/等级', thickness: '厚度', width: '宽度', quantity: '数量', quantity_unit: '单位', delivery: '交期', port: '港口' }
const displayFields = computed(() => templateFields.value.length
  ? templateFields.value.filter((field) => !['unit_price', 'total_price'].includes(field.fieldKey)).sort((a, b) => Number(a.sortOrder) - Number(b.sortOrder)).map((field) => ({ key: field.fieldKey, label: field.displayName, required: field.isRequired }))
  : fallbackFields.map((key) => ({ key, label: fallbackLabels[key] ?? key, required: ['product', 'quantity', 'quantity_unit'].includes(key) })))

function readTemplateField(extracted: Record<string, any>, key: string) {
  if (key.startsWith('custom.')) return extracted.customFields?.[key] ?? ''
  return extracted[templateProperty[key] ?? key] ?? ''
}

function writeTemplateField(extracted: Record<string, any>, key: string, value: string) {
  if (key.startsWith('custom.')) {
    extracted.customFields ??= {}
    if (value === '') delete extracted.customFields[key]; else extracted.customFields[key] = value
    return
  }
  extracted[templateProperty[key] ?? key] = value
}

async function resolveTemplateFields(intake: Intake) {
  templateFields.value = []
  resolvedTemplate.value = null
  if (intake.inquiryTemplateId && Number(intake.inquiryTemplateId) > 0) {
    try {
      const data = await get<{ template: InquiryTemplate & { fields?: TemplateField[] } }>(`/inquiry-templates/${intake.inquiryTemplateId}`)
      resolvedTemplate.value = data.template
      templateFields.value = data.template.fields ?? []
    } catch { /* 历史询盘没有可读取模板时使用兼容字段。 */ }
  }
}

function formatTime(value: string) { return value ? new Date(value).toLocaleString() : '—' }
function pickFile(event: Event) { uploadForm.file = (event.target as HTMLInputElement).files?.[0] ?? null }

async function loadTemplates() {
  templatesLoading.value = true
  try {
    const data = await get<{ templates: InquiryTemplate[] }>('/inquiry-templates')
    templates.value = data.templates ?? []
    const selectedStillActive = activeTemplates.value.some((item) => String(item.id) === uploadForm.templateId)
    if (!selectedStillActive) uploadForm.templateId = ''
  } finally { templatesLoading.value = false }
}

async function openUpload() {
  uploadOpen.value = true
  await loadTemplates()
}

async function downloadSelectedTemplate() {
  if (!uploadForm.templateId) return
  const file = await download(`/inquiry-templates/${uploadForm.templateId}/download`)
  saveBlob(file.blob, file.fileName || 'standard-inquiry.csv')
}

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
  body.append('file', uploadForm.file); if (uploadForm.templateId) body.append('inquiry_template_id', uploadForm.templateId); body.append('title', uploadForm.title); body.append('customer_name', uploadForm.customerName); body.append('contact_name', uploadForm.contactName); body.append('contact_email', uploadForm.contactEmail)
  saving.value = true
  try {
    const response = await http.post<Envelope<{ sourcingCase: Intake }>>('/sourcing-intakes/import', body)
    uploadOpen.value = false; ElMessage.success(t('procurementIntakes.uploaded')); await load(); await openDetail(response.data.data!.sourcingCase)
  } finally { saving.value = false }
}

async function openDetail(row: Intake) {
  const data = await get<{ sourcingCase: Intake }>(`/sourcing-cases/${row.id}`)
  detail.value = data.sourcingCase; detailOpen.value = true
  await resolveTemplateFields(data.sourcingCase)
}

function missingFieldLabels(line: IntakeLine) {
  if (line.decision === 'SKIPPED') return []
  return displayFields.value
    .filter((field) => field.required && !String(readTemplateField(line.extracted, field.key)).trim())
    .map((field) => field.label)
}

async function saveLine(line: IntakeLine) {
  await put(`/sourcing-cases/${detail.value!.id}/lines/${line.id}`, { extracted: line.extracted, decision: line.decision })
}

async function saveDraft() {
  if (!detail.value) return
  saving.value = true
  try {
    for (const line of detail.value.lines ?? []) await saveLine(line)
    const data = await get<{ sourcingCase: Intake }>(`/sourcing-cases/${detail.value.id}`)
    detail.value = data.sourcingCase
    ElMessage.success(t('procurementIntakes.draftSaved'))
  } finally { saving.value = false }
}

function openAddLine() {
  Object.assign(addLineForm, emptyExtracted())
  addLineOpen.value = true
}

async function addLine() {
  if (!detail.value) return
  const missing = displayFields.value.filter((field) => field.required && !String(readTemplateField(addLineForm, field.key)).trim()).map((field) => field.label)
  if (missing.length) {
    ElMessage.warning(`${t('procurementIntakes.newProductRequired')}：${missing.join('、')}`)
    return
  }
  addingLine.value = true
  try {
    const extracted = { ...addLineForm, customFields: { ...(addLineForm.customFields ?? {}) } }
    const data = await post<{ sourcingCase: Intake }>(`/sourcing-cases/${detail.value.id}/lines`, { extracted })
    detail.value = data.sourcingCase
    addLineOpen.value = false
    ElMessage.success(t('procurementIntakes.productAdded'))
  } finally { addingLine.value = false }
}

async function confirmIntake() {
  if (!detail.value) return
  const active = detail.value.lines?.filter((line) => line.decision !== 'SKIPPED') ?? []
  if (!active.length) { ElMessage.warning(t('procurementIntakes.keepOne')); return }
  const incomplete = active.filter((line) => missingFieldLabels(line).length > 0)
  if (incomplete.length) {
    const detail = incomplete.map((line) => `${line.lineNo}（${missingFieldLabels(line).join('、')}）`).join('；')
    ElMessage.warning(t('procurementIntakes.dynamicRequiredMissing', { detail }))
    return
  }
  try {
    await ElMessageBox.confirm(t('procurementIntakes.confirmHint'), t('procurementIntakes.confirmCreate'), { type: 'warning' })
  } catch (action) {
    // 用户取消或关闭确认框时停留在复核页；真正的异常仍交给上层处理。
    if (isDialogDismissed(action)) return
    throw action
  }
  saving.value = true
  try {
    for (const line of detail.value.lines ?? []) await saveLine(line)
    await post(`/sourcing-cases/${detail.value.id}/confirm-lines`, { sourcingLineIds: active.map((line) => Number(line.id)) })
    ElMessage.success(t('procurementIntakes.confirmed')); detailOpen.value = false; await router.push({ path: '/sourcing-cases', query: { case: detail.value.id } })
  } finally { saving.value = false }
}

onMounted(async () => {
  await load()
  if (route.query.upload === '1' && canWrite) await openUpload()
})
</script>

<style scoped>
.page{padding:28px;background:#f4f7f7;min-height:100%}.page-head{display:flex;justify-content:space-between;align-items:flex-start;margin-bottom:18px}.head-actions{display:flex;gap:10px}.eyebrow{color:#16766b;font-size:12px;font-weight:700;letter-spacing:.12em;text-transform:uppercase}.page-head h1{margin:5px 0 4px;font-size:26px;color:#173042}.page-head p{margin:0;color:#71808b}.panel{background:#fff;border:1px solid #dfe8e6;border-radius:12px;padding:18px}.filters{display:flex;gap:10px;width:460px;margin-bottom:14px}.row-actions{display:flex;align-items:center;gap:8px;white-space:nowrap}.el-pagination{justify-content:flex-end;margin-top:16px}.upload-form{margin-top:20px}.template-picker{display:grid;grid-template-columns:minmax(0,1fr) auto;gap:8px;width:100%}.template-help{width:100%;margin-top:5px;color:#7b8992;font-size:12px;line-height:1.5}.detail-summary{display:grid;grid-template-columns:2fr 1fr 1fr 1.25fr;gap:14px;margin-bottom:14px}.detail-summary>div{display:flex;flex-direction:column;gap:4px;padding:11px 14px;background:#f4f7f7;border-radius:8px}.detail-summary small{color:#7b8992}.review-toolbar{display:flex;justify-content:space-between;align-items:center;gap:12px;margin-bottom:14px;padding:10px 14px;border:1px solid #dce8e5;border-radius:8px;background:#f7fbfa;color:#536873}.review-alert{margin-bottom:14px}.stack-input{margin-top:6px}.qty{display:grid;grid-template-columns:1fr 70px;gap:6px}.pair-input{display:grid;grid-template-columns:1fr 1fr;gap:8px;width:100%}.custom-fields{display:grid;gap:8px}.custom-fields label{display:grid;gap:3px}.custom-fields small{color:#71808b}@media(max-width:1050px){.detail-summary{grid-template-columns:1fr 1fr}}@media(max-width:850px){.filters{width:100%}.template-picker{grid-template-columns:1fr}.detail-summary{grid-template-columns:1fr}.page-head{gap:14px;flex-direction:column}.head-actions{flex-wrap:wrap}.review-toolbar{align-items:flex-start;flex-direction:column}.pair-input{grid-template-columns:1fr}}
</style>
