<template>
  <el-dialog v-model="visible" title="批量导入客户" width="760px" destroy-on-close @closed="resetDialog">
    <el-alert type="info" :closable="false" show-icon>
      <template #title>客户名称或简称至少填写一项。错误行会跳过；同一客户可用多行填写联系人，多个分管人用分号分隔。</template>
    </el-alert>

    <div class="import-tools">
      <el-upload :auto-upload="false" :show-file-list="false" accept=".xls,.xlsx" :on-change="readFile">
        <el-button type="primary" plain>选择 Excel 文件</el-button>
      </el-upload>
      <el-button text type="primary" @click="downloadTemplate">下载模板</el-button>
      <span class="support-note">支持 .xls、.xlsx，最多 5000 行</span>
    </div>

    <template v-if="workbook">
      <div class="file-summary">
        <strong>{{ fileName }}</strong>
        <span>{{ currentSheet.columns.length }} 列 · {{ currentSheet.totalRows }} 行数据</span>
        <el-select v-if="workbook.sheets.length > 1" v-model="sheetIndex" size="small" class="sheet-select" @change="selectSheet">
          <el-option v-for="(sheet, index) in workbook.sheets" :key="`${sheet.name}-${index}`" :label="sheet.name" :value="index" />
        </el-select>
        <span v-else>工作表：{{ currentSheet.name }}</span>
      </div>

      <el-alert v-if="mappingProblems.length" class="mapping-warning" type="error" :closable="false" :title="mappingProblems.join('；')" />
      <el-alert v-else class="mapping-warning" type="success" :closable="false" title="模板格式正确，字段将按固定规则导入。" />

      <div v-if="rows.length" class="data-preview">
        <strong>数据预览 <small>前 8 行，共 {{ rows.length }} 行</small></strong>
        <el-table :data="rows.slice(0, 8)" size="small" max-height="220">
          <el-table-column prop="sourceLine" label="行" width="55" />
          <el-table-column prop="code" label="客户代码" width="125" show-overflow-tooltip />
          <el-table-column label="客户名称" min-width="180" show-overflow-tooltip><template #default="{ row }">{{ row.name || row.shortName }}</template></el-table-column>
          <el-table-column prop="contactName" label="联系人" min-width="120" show-overflow-tooltip />
        </el-table>
      </div>
    </template>

    <div v-if="rows.length" class="batch-options">
      <label>已有客户公司资料 <el-select v-model="customerAction" :disabled="checking || saving" @change="invalidatePreview"><el-option label="保留原资料" value="KEEP" /><el-option label="更新非空资料" value="UPDATE" /></el-select></label>
      <label>重复联系人 <el-select v-model="contactAction" :disabled="checking || saving" @change="invalidatePreview"><el-option label="跳过" value="SKIP" /><el-option label="更新非空资料" value="UPDATE" /><el-option label="另增一人" value="ADD" /></el-select></label>
      <small>新客户和新联系人正常新增；模板中的分管人会追加，已有分管人不会被移除。</small>
    </div>

    <div v-if="reviewed" class="preview">
      <div class="preview-head"><strong>预检结果</strong><span class="ok">可导入 {{ ready }} 行</span><span class="blocked">错误 {{ blocked }} 行</span></div>
      <el-table v-if="blocked" :data="errorRows" max-height="260" size="small">
        <el-table-column prop="line" label="行" width="60" />
        <el-table-column prop="code" label="客户代码" width="125" />
        <el-table-column prop="name" label="客户名称" min-width="175" show-overflow-tooltip />
        <el-table-column prop="reason" label="错误原因" min-width="260" show-overflow-tooltip />
      </el-table>
    </div>

    <template #footer>
      <el-button @click="visible = false">取消</el-button>
      <el-button :disabled="saving || !rows.length || mappingProblems.length > 0" :loading="checking" @click="preview">预检全部 {{ rows.length }} 行</el-button>
      <el-button type="primary" :disabled="checking || !reviewed || !preparedRows.length || ready === 0" :loading="saving" @click="commit">确认导入 {{ ready }} 行</el-button>
    </template>
  </el-dialog>
</template>

<script setup lang="ts">
import { computed, ref } from 'vue'
import { ElMessage, type UploadFile } from 'element-plus'
import { get, post, quietErrors } from '../api'
import { type DirectWorkbook } from '../lib/attachmentExcel'
import { parseCustomerImportWorkbook } from '../lib/customerImportWorkbook'
import {
  buildCustomerImportRows,
  fixedCustomerTemplateMapping,
  customerTemplateProblems,
  type CustomerImportEmployee,
} from '../lib/customerImport'
import { applyCustomerImportActions, type ContactImportAction, type CustomerImportAction, type CustomerImportVerdictFlags } from '../lib/customerImportDecisions'

const props = defineProps<{ open: boolean }>()
const emit = defineEmits<{ 'update:open': [value: boolean]; imported: [] }>()
const visible = computed({ get: () => props.open, set: value => emit('update:open', value) })
const workbook = ref<DirectWorkbook | null>(null)
const fileName = ref('')
const sheetIndex = ref(0)
const employees = ref<CustomerImportEmployee[]>([])
const verdicts = ref<any[]>([])
const preparedRows = ref<any[]>([])
const customerAction = ref<CustomerImportAction>('KEEP')
const contactAction = ref<ContactImportAction>('SKIP')
const reviewed = ref(false)
const checking = ref(false)
const saving = ref(false)

const currentSheet = computed(() => workbook.value?.sheets[sheetIndex.value] ?? { name: '', columns: [], rows: [], totalRows: 0 })
const mappingProblems = computed(() => customerTemplateProblems(currentSheet.value.columns))
const mapping = computed(() => fixedCustomerTemplateMapping(currentSheet.value.columns))
const rows = computed(() => mappingProblems.value.length ? [] : buildCustomerImportRows(currentSheet.value.rows, mapping.value, employees.value))
const ready = computed(() => verdicts.value.filter(v => v.ok).length)
const blocked = computed(() => verdicts.value.filter(v => !v.ok).length)
const errorRows = computed(() => verdicts.value.filter(v => !v.ok))

function invalidatePreview() {
  verdicts.value = []; preparedRows.value = []; reviewed.value = false
}

function selectSheet() {
  invalidatePreview()
}

async function loadWorkbook(name: string, data: ArrayBuffer) {
  resetDialog()
  if (!employees.value.length) {
    try {
      const response = await get<any>('/employees', { page: 1, page_size: 500, employment_status: 'ACTIVE' }, quietErrors)
      employees.value = response.employees ?? []
    } catch {
      // Users without IAM directory access can still import customers. A
      // populated owner column will fail precheck with a precise message so it
      // is never silently stored as an unrelated extension field.
    }
  }
  const parsed = await parseCustomerImportWorkbook(name, data)
  if (!parsed.sheets.length) throw new Error('文件中没有可读取的工作表')
  const truncated = parsed.sheets.find(sheet => sheet.totalRows > sheet.rows.length)
  if (truncated) throw new Error(`工作表“${truncated.name}”超过 5000 行，请拆分后再导入`)
  workbook.value = parsed
  fileName.value = name
  sheetIndex.value = 0
  selectSheet()
}

async function readFile(file: UploadFile) {
  if (!file.raw) return
  const extension = file.name.slice(file.name.lastIndexOf('.')).toLowerCase()
  if (!['.xls', '.xlsx'].includes(extension)) {
    ElMessage.error('请选择 .xls 或 .xlsx 文件'); return
  }
  try {
    await loadWorkbook(file.name, await file.raw.arrayBuffer())
  } catch (error) {
    ElMessage.error(error instanceof Error ? error.message : '文件读取失败')
  }
}

function downloadTemplate() {
  const link = document.createElement('a'); link.href = '/templates/customer-import.xlsx'; link.download = '客户导入模板.xlsx'; link.click()
}
async function preview() {
  if (!rows.value.length) { ElMessage.warning('请先上传文件或读取粘贴内容'); return }
  if (mappingProblems.value.length) { ElMessage.warning(mappingProblems.value[0]); return }
  invalidatePreview()
  checking.value = true
  try {
    // The first dry run discovers existing customers and duplicate contacts.
    // Apply the chosen batch rules only to those rows; applying UPDATE to a new
    // contact would make the backend correctly reject it as an ambiguous match.
    const discovery = await post<any>('/customers/import', { templateMode: true, rows: rows.value, dryRun: true })
    let flags: CustomerImportVerdictFlags[] = discovery.verdicts ?? []
    let result = discovery
    for (let attempt = 0; attempt < 3; attempt++) {
      preparedRows.value = applyCustomerImportActions(rows.value, flags, customerAction.value, contactAction.value)
      result = await post<any>('/customers/import', { templateMode: true, rows: preparedRows.value, dryRun: true })
      const next: CustomerImportVerdictFlags[] = result.verdicts ?? []
      const discoveredMore = next.some((verdict, index) =>
        (verdict.existingCustomer && !flags[index]?.existingCustomer)
        || (verdict.duplicateContact && !flags[index]?.duplicateContact))
      if (!discoveredMore) break
      flags = next.map((verdict, index) => ({
        existingCustomer: Boolean(flags[index]?.existingCustomer || verdict.existingCustomer),
        duplicateContact: Boolean(flags[index]?.duplicateContact || verdict.duplicateContact),
      }))
    }
    verdicts.value = result.verdicts ?? []; reviewed.value = true
  } finally { checking.value = false }
}

async function commit() {
  if (!reviewed.value || !preparedRows.value.length || ready.value === 0) return
  saving.value = true
  try {
    const data = await post<any>('/customers/import', { templateMode: true, rows: preparedRows.value, dryRun: false })
    verdicts.value = data.verdicts ?? verdicts.value
    const failed = Number(data.blocked ?? 0)
    if (failed > 0) { ElMessage.warning(`已导入 ${data.imported ?? 0} 行，另有 ${failed} 行未导入，请查看错误说明`) }
    else { ElMessage.success(`已处理 ${data.imported ?? 0} 行客户及联系人资料`); visible.value = false }
    preparedRows.value = []
    emit('imported')
  } finally { saving.value = false }
}

function resetDialog() {
  workbook.value = null
  fileName.value = ''
  sheetIndex.value = 0
  invalidatePreview()
  customerAction.value = 'KEEP'
  contactAction.value = 'SKIP'
}
</script>

<style scoped>
.import-tools { display:flex; align-items:center; gap:12px; margin:16px 0 12px; }
.support-note { color:var(--el-text-color-secondary); font-size:12px; }
.file-summary { display:flex; align-items:center; gap:16px; padding:12px 14px; border:1px solid var(--el-border-color-lighter); border-radius:8px; background:var(--el-fill-color-light); color:var(--el-text-color-secondary); font-size:13px; }
.file-summary strong { color:var(--el-text-color-primary); }
.sheet-select { width:180px; margin-left:auto; }
.mapping-warning { margin-top:10px; }
.data-preview { margin-top:12px; }
.data-preview strong { display:block; margin-bottom:6px; font-size:13px; }
.data-preview small { margin-left:6px; color:var(--el-text-color-secondary); font-weight:400; }
.batch-options { display:flex; align-items:end; flex-wrap:wrap; gap:10px 14px; margin-top:14px; }
.batch-options label { display:flex; flex-direction:column; gap:5px; min-width:180px; font-size:12px; color:var(--el-text-color-secondary); }
.batch-options small { width:100%; color:var(--el-text-color-secondary); }
.preview { margin-top:16px; border:1px solid var(--el-border-color-lighter); border-radius:10px; overflow:hidden; }
.preview-head { display:flex; gap:18px; padding:12px 16px; background:var(--el-fill-color-light); }
.ok { color:var(--el-color-success); }
.blocked { color:var(--el-color-danger); }
@media(max-width:600px){.import-tools,.file-summary{flex-wrap:wrap}.batch-options label{flex:1;min-width:150px}}
</style>
