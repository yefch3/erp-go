<template>
  <el-dialog v-model="visible" title="批量导入客户" width="960px" destroy-on-close @closed="resetDialog">
    <el-alert type="info" :closable="false" show-icon>
      <template #title>请使用固定模板填写数据，不要修改表头或列顺序。同一客户代码可填写多行联系人。已有客户需确认更新，空单元格保留原值；多个分管人用分号分隔。</template>
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
        <div class="section-title">
          <div><strong>数据预览</strong><small>显示前 5 行；正式预检会检查全部 {{ rows.length }} 行。</small></div>
        </div>
        <el-table :data="previewRows" border size="small" max-height="240">
          <el-table-column v-for="column in previewColumns" :key="column.key" :prop="column.key" :label="column.label" min-width="140" show-overflow-tooltip />
        </el-table>
      </div>
    </template>

    <div v-if="verdicts.length" class="preview">
      <div class="preview-head">
        <strong>预检结果</strong>
        <span class="ok">可导入 {{ ready }} 行</span>
        <span v-if="blocked" class="blocked">需修正 {{ blocked }} 行</span>
      </div>
      <el-table :data="verdicts" max-height="280" size="small">
        <el-table-column prop="line" label="行" width="60" />
        <el-table-column prop="code" label="编码" width="130" />
        <el-table-column prop="name" label="客户名称" min-width="180" />
        <el-table-column label="结果" width="90">
          <template #default="{ row }"><el-tag :type="row.ok ? 'success' : 'danger'">{{ row.ok ? '通过' : '错误' }}</el-tag></template>
        </el-table-column>
        <el-table-column label="客户资料" min-width="190"><template #default="{ row, $index }">
          <el-select v-if="row.existingCustomer" :model-value="decisions[$index]?.customerAction" placeholder="请选择" @change="decide($index, 'customerAction', $event)"><el-option label="更新非空资料" value="UPDATE"/><el-option label="保留原资料" value="KEEP"/></el-select><span v-else>新增客户／合并同代码行</span>
        </template></el-table-column>
        <el-table-column label="联系人处理" min-width="160"><template #default="{ row, $index }">
          <el-select v-if="row.duplicateContact" :model-value="decisions[$index]?.contactAction" placeholder="请选择" @change="decide($index, 'contactAction', $event)"><el-option label="更新非空资料" value="UPDATE"/><el-option label="另增一人" value="ADD"/><el-option label="跳过" value="SKIP"/></el-select><span v-else>新增（有联系人时）</span>
        </template></el-table-column>
        <el-table-column prop="reason" label="说明" min-width="220" />
      </el-table>
    </div>

    <template #footer>
      <el-button @click="visible = false">取消</el-button>
      <el-button :disabled="!rows.length || mappingProblems.length > 0" :loading="checking" @click="preview">预检全部 {{ rows.length }} 行</el-button>
      <el-button type="primary" :disabled="!reviewed || !verdicts.length || blocked > 0" :loading="saving" @click="commit">确认导入 {{ ready }} 行</el-button>
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

const props = defineProps<{ open: boolean }>()
const emit = defineEmits<{ 'update:open': [value: boolean]; imported: [] }>()
const visible = computed({ get: () => props.open, set: value => emit('update:open', value) })
const workbook = ref<DirectWorkbook | null>(null)
const fileName = ref('')
const sheetIndex = ref(0)
const employees = ref<CustomerImportEmployee[]>([])
const verdicts = ref<any[]>([])
const decisions = ref<Record<number, { customerAction?: string; contactAction?: string }>>({})
const reviewed = ref(false)
const checking = ref(false)
const saving = ref(false)

const currentSheet = computed(() => workbook.value?.sheets[sheetIndex.value] ?? { name: '', columns: [], rows: [], totalRows: 0 })
const mappingProblems = computed(() => customerTemplateProblems(currentSheet.value.columns))
const mapping = computed(() => fixedCustomerTemplateMapping(currentSheet.value.columns))
const rows = computed(() => mappingProblems.value.length ? [] : buildCustomerImportRows(currentSheet.value.rows, mapping.value, employees.value).map((row, index) => ({ ...row, ...decisions.value[index] })))
const previewColumns = computed(() => currentSheet.value.columns.map((label, index) => ({ key: `column_${index}`, label })))
const previewRows = computed(() => currentSheet.value.rows.slice(0, 5).map(sourceRow => Object.fromEntries(previewColumns.value.map(column => [column.key, sourceRow[Number(column.key.slice(7))] ?? '']))))
const ready = computed(() => verdicts.value.filter(v => v.ok).length)
const blocked = computed(() => verdicts.value.filter(v => !v.ok).length)

function selectSheet() {
  verdicts.value = []; decisions.value = {}; reviewed.value = false
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
function decide(index: number, field: 'customerAction' | 'contactAction', value: string) {
  decisions.value[index] = { ...decisions.value[index], [field]: value }; reviewed.value = false
  if (field === 'customerAction') {
    const code = verdicts.value[index]?.code
    if (code) verdicts.value.forEach((row, i) => { if (row.code === code) decisions.value[i] = { ...decisions.value[i], [field]: value } })
  }
}

async function preview() {
  if (!rows.value.length) { ElMessage.warning('请先上传文件或读取粘贴内容'); return }
  if (mappingProblems.value.length) { ElMessage.warning(mappingProblems.value[0]); return }
  checking.value = true
  try {
    const data = await post<any>('/customers/import', { templateMode: true, rows: rows.value, dryRun: true })
    verdicts.value = data.verdicts ?? []; reviewed.value = true
  } finally { checking.value = false }
}

async function commit() {
  saving.value = true
  try {
    const data = await post<any>('/customers/import', { templateMode: true, rows: rows.value, dryRun: false })
    if (Number(data.blocked) > 0) { verdicts.value = data.verdicts ?? []; reviewed.value = true; ElMessage.warning('数据已变化，请检查预检结果'); return }
    ElMessage.success(`已处理 ${data.imported ?? 0} 行客户及联系人资料`)
    visible.value = false
    emit('imported')
  } finally { saving.value = false }
}

function resetDialog() {
  workbook.value = null
  fileName.value = ''
  sheetIndex.value = 0
  verdicts.value = []; decisions.value = {}; reviewed.value = false
}
</script>

<style scoped>
.import-tools { display:flex; align-items:center; gap:12px; margin:16px 0 12px; }
.support-note { color:var(--el-text-color-secondary); font-size:12px; }
.file-summary { display:flex; align-items:center; gap:16px; padding:12px 14px; border:1px solid var(--el-border-color-lighter); border-radius:8px; background:var(--el-fill-color-light); color:var(--el-text-color-secondary); font-size:13px; }
.file-summary strong { color:var(--el-text-color-primary); }
.sheet-select { width:180px; margin-left:auto; }
.section-title { display:flex; justify-content:space-between; align-items:center; margin:18px 0 9px; }
.section-title > div { display:flex; align-items:baseline; gap:10px; }
.section-title small { color:var(--el-text-color-secondary); }
.mapping-warning { margin-top:10px; }
.data-preview { margin-top:4px; }
.preview { margin-top:16px; border:1px solid var(--el-border-color-lighter); border-radius:10px; overflow:hidden; }
.preview-head { display:flex; gap:18px; padding:12px 16px; background:var(--el-fill-color-light); }
.ok { color:var(--el-color-success); }
.blocked { color:var(--el-color-danger); }
</style>
