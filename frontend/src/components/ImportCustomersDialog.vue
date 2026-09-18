<template>
  <el-dialog v-model="visible" title="批量导入客户" width="960px" destroy-on-close @closed="resetDialog">
    <el-alert type="info" :closable="false" show-icon>
      <template #title>上传 Excel 后读取第一行表头，确认各列对应的客户字段，再预检并整批导入。</template>
    </el-alert>

    <div class="import-tools">
      <el-upload :auto-upload="false" :show-file-list="false" accept=".xlsx,.csv,.tsv" :on-change="readFile">
        <el-button type="primary" plain>选择 Excel 文件</el-button>
      </el-upload>
      <el-button text type="primary" @click="downloadTemplate">下载模板</el-button>
      <span class="support-note">支持 .xlsx、.csv、.tsv，最多 5000 行</span>
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

      <div class="section-title">
        <div><strong>对应导入字段</strong><small>系统已按表头自动匹配，你可以修改或忽略不需要的列。</small></div>
        <el-tag v-if="!mappingProblems.length" type="success" effect="plain">字段对应完成</el-tag>
      </div>
      <el-table :data="mappingRows" border size="small" max-height="300" class="mapping-table">
        <el-table-column prop="header" label="Excel 表头" min-width="190" />
        <el-table-column label="导入为" min-width="300">
          <template #default="{ row }">
            <el-select :model-value="mapping[row.index]" placeholder="忽略此列" clearable @change="changeMapping(row.index, String($event ?? ''))">
              <el-option label="忽略此列" value="" />
              <el-option-group label="ERP 已有基础字段">
                <el-option v-for="field in CUSTOMER_IMPORT_FIELDS" :key="field.key" :label="`${field.label}${'required' in field && field.required ? '（必填）' : ''}`" :value="`system:${field.key}`" :disabled="isFieldUsed(`system:${field.key}`, row.index)" />
              </el-option-group>
              <el-option-group v-if="customerFields.length" label="已有自定义字段">
                <el-option v-for="field in customerFields" :key="field.fieldKey" :label="field.displayName" :value="`custom:${field.fieldKey}`" :disabled="isFieldUsed(`custom:${field.fieldKey}`, row.index)" />
              </el-option-group>
              <el-option-group label="新增字段">
                <el-option :label="`创建新字段：${row.header}`" :value="`new:${row.index}`" />
              </el-option-group>
            </el-select>
            <el-input v-if="mapping[row.index]?.startsWith('new:')" v-model="newFieldNames[row.index]" class="new-field-name" placeholder="新字段显示名称" @input="verdicts = []" />
          </template>
        </el-table-column>
        <el-table-column prop="sample" label="第一条数据示例" min-width="260">
          <template #default="{ row }"><span class="sample">{{ row.sample || '—' }}</span></template>
        </el-table-column>
      </el-table>
      <el-alert v-if="mappingProblems.length" class="mapping-warning" type="warning" :closable="false" :title="mappingProblems.join('；')" />

      <div v-if="rows.length" class="data-preview">
        <div class="section-title">
          <div><strong>数据预览</strong><small>显示前 5 行；正式预检会检查全部 {{ rows.length }} 行。</small></div>
        </div>
        <el-table :data="previewRows" border size="small" max-height="240">
          <el-table-column v-for="column in previewColumns" :key="column.key" :prop="column.key" :label="column.label" min-width="140" show-overflow-tooltip />
        </el-table>
      </div>
    </template>

    <el-collapse v-model="pastePanels" class="paste-panel">
      <el-collapse-item name="paste" title="也可以从 Excel 复制后粘贴">
        <div class="paste-tools">
          <span>第一行必须是表头，列顺序不限。</span>
          <el-button text type="primary" @click="fillExample">填入示例</el-button>
          <el-button type="primary" plain :disabled="!source.trim()" @click="readPasted">读取粘贴内容</el-button>
        </div>
        <el-input v-model="source" type="textarea" :rows="5" placeholder="从 Excel 复制包含表头的数据并粘贴到这里" />
      </el-collapse-item>
    </el-collapse>

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
        <el-table-column prop="reason" label="说明" min-width="220" />
      </el-table>
    </div>

    <template #footer>
      <el-button @click="visible = false">取消</el-button>
      <el-button :disabled="!rows.length || mappingProblems.length > 0" :loading="checking" @click="preview">预检全部 {{ rows.length }} 行</el-button>
      <el-button type="primary" :disabled="!verdicts.length || blocked > 0" :loading="saving" @click="commit">确认导入 {{ ready }} 行</el-button>
    </template>
  </el-dialog>
</template>

<script setup lang="ts">
import { computed, ref } from 'vue'
import { ElMessage, type UploadFile } from 'element-plus'
import { get, post, quietErrors } from '../api'
import { parseTableFile, type DirectWorkbook } from '../lib/attachmentExcel'
import {
  CUSTOMER_IMPORT_FIELDS,
  autoMapCustomerHeaders,
  buildCustomerImportFieldMappings,
  buildCustomerImportRows,
  customerImportMappingProblems,
  extensionCustomerFields,
  type CustomerFieldDefinition,
  type CustomerImportEmployee,
  type CustomerImportMapping,
} from '../lib/customerImport'

const props = defineProps<{ open: boolean }>()
const emit = defineEmits<{ 'update:open': [value: boolean]; imported: [] }>()
const visible = computed({ get: () => props.open, set: value => emit('update:open', value) })
const workbook = ref<DirectWorkbook | null>(null)
const fileName = ref('')
const sheetIndex = ref(0)
const mapping = ref<CustomerImportMapping>({})
const newFieldNames = ref<Record<number, string>>({})
const customerFields = ref<CustomerFieldDefinition[]>([])
const employees = ref<CustomerImportEmployee[]>([])
const source = ref('')
const pastePanels = ref<string[]>([])
const verdicts = ref<any[]>([])
const checking = ref(false)
const saving = ref(false)

const currentSheet = computed(() => workbook.value?.sheets[sheetIndex.value] ?? { name: '', columns: [], rows: [], totalRows: 0 })
const mappingRows = computed(() => currentSheet.value.columns.map((header, index) => ({
  index,
  header: header || `未命名列 ${index + 1}`,
  sample: currentSheet.value.rows.find(row => row[index]?.trim())?.[index] ?? '',
})))
const mappingProblems = computed(() => customerImportMappingProblems(mapping.value, newFieldNames.value))
const rows = computed(() => buildCustomerImportRows(currentSheet.value.rows, mapping.value, employees.value))
const previewColumns = computed(() => mappingRows.value.filter(row => mapping.value[row.index]).map(row => ({ key: `column_${row.index}`, label: targetLabel(row.index, row.header) })))
const previewRows = computed(() => currentSheet.value.rows.slice(0, 5).map(sourceRow => Object.fromEntries(previewColumns.value.map(column => [column.key, sourceRow[Number(column.key.slice(7))] ?? '']))))
const customFieldMappings = computed(() => buildCustomerImportFieldMappings(currentSheet.value.columns, mapping.value, newFieldNames.value, customerFields.value))
const ready = computed(() => verdicts.value.filter(v => v.ok).length)
const blocked = computed(() => verdicts.value.filter(v => !v.ok).length)

function selectSheet() {
  mapping.value = autoMapCustomerHeaders(currentSheet.value.columns, customerFields.value)
  newFieldNames.value = Object.fromEntries(currentSheet.value.columns.map((header, index) => [index, header || `未命名字段 ${index + 1}`]))
  verdicts.value = []
}

function changeMapping(index: number, value: string) {
  mapping.value = { ...mapping.value, [index]: value ?? '' }
  if (value?.startsWith('new:') && !newFieldNames.value[index]) newFieldNames.value[index] = currentSheet.value.columns[index] || `未命名字段 ${index + 1}`
  verdicts.value = []
}

function isFieldUsed(field: string, currentIndex: number) {
  return Object.entries(mapping.value).some(([index, selected]) => Number(index) !== currentIndex && selected === field)
}

function targetLabel(index: number, fallback: string) {
  const target = mapping.value[index] ?? ''
  if (target.startsWith('system:')) return CUSTOMER_IMPORT_FIELDS.find(field => field.key === target.slice(7))?.label ?? fallback
  if (target.startsWith('custom:')) return customerFields.value.find(field => field.fieldKey === target.slice(7))?.displayName ?? fallback
  if (target.startsWith('new:')) return newFieldNames.value[index] || fallback
  return fallback
}

async function loadWorkbook(name: string, data: ArrayBuffer) {
  if (!customerFields.value.length) {
    const response = await get<any>('/customers/fields')
    customerFields.value = extensionCustomerFields(response.fields ?? [])
  }
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
  const parsed = await parseTableFile(name, data, { maxRows: 5000 })
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
  if (extension === '.xls') {
    ElMessage.error('暂不支持旧版 .xls，请在 Excel 中另存为 .xlsx 后上传')
    return
  }
  if (!['.xlsx', '.csv', '.tsv'].includes(extension)) {
    ElMessage.error('请选择 .xlsx、.csv 或 .tsv 文件')
    return
  }
  try {
    await loadWorkbook(file.name, await file.raw.arrayBuffer())
  } catch (error) {
    ElMessage.error(error instanceof Error ? error.message : '文件读取失败')
  }
}

async function readPasted() {
  if (!source.value.trim()) return
  try {
    const delimiterExtension = source.value.includes('\t') ? 'tsv' : 'csv'
    const bytes = new TextEncoder().encode(source.value).buffer
    await loadWorkbook(`粘贴内容.${delimiterExtension}`, bytes)
  } catch (error) {
    ElMessage.error(error instanceof Error ? error.message : '粘贴内容读取失败')
  }
}

function fillExample() {
  source.value = '编码\t名称\t国家代码\t客户类型\t币种\t付款方式\t联系人\t邮箱\t电话\t备注\n\t示例客户\tUS\tIMPORTER\tUSD\tTT\tAmy\tamy@example.com\t+1 2025550100\t展会客户'
  void readPasted()
}

function downloadTemplate() {
  const header = '\uFEFF编码,名称,国家代码,客户类型,币种,付款方式,联系人,邮箱,电话,备注\n'
  const url = URL.createObjectURL(new Blob([header], { type: 'text/csv;charset=utf-8' }))
  const link = document.createElement('a')
  link.href = url
  link.download = '客户导入模板.csv'
  link.click()
  URL.revokeObjectURL(url)
}

async function preview() {
  if (!rows.value.length) { ElMessage.warning('请先上传文件或读取粘贴内容'); return }
  if (mappingProblems.value.length) { ElMessage.warning(mappingProblems.value[0]); return }
  checking.value = true
  try {
    const data = await post<any>('/customers/import', { rows: rows.value, customFieldMappings: customFieldMappings.value, dryRun: true })
    verdicts.value = data.verdicts ?? []
  } finally { checking.value = false }
}

async function commit() {
  saving.value = true
  try {
    const data = await post<any>('/customers/import', { rows: rows.value, customFieldMappings: customFieldMappings.value, dryRun: false })
    ElMessage.success(`已导入 ${data.imported ?? 0} 个客户`)
    visible.value = false
    emit('imported')
  } finally { saving.value = false }
}

function resetDialog() {
  workbook.value = null
  fileName.value = ''
  sheetIndex.value = 0
  mapping.value = {}
  newFieldNames.value = {}
  source.value = ''
  pastePanels.value = []
  verdicts.value = []
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
.mapping-table :deep(.el-select) { width:100%; }
.new-field-name { margin-top:6px; }
.sample { color:var(--el-text-color-regular); }
.mapping-warning { margin-top:10px; }
.data-preview { margin-top:4px; }
.paste-panel { margin-top:18px; }
.paste-tools { display:flex; align-items:center; gap:8px; margin-bottom:8px; color:var(--el-text-color-secondary); font-size:12px; }
.paste-tools .el-button:first-of-type { margin-left:auto; }
.preview { margin-top:16px; border:1px solid var(--el-border-color-lighter); border-radius:10px; overflow:hidden; }
.preview-head { display:flex; gap:18px; padding:12px 16px; background:var(--el-fill-color-light); }
.ok { color:var(--el-color-success); }
.blocked { color:var(--el-color-danger); }
</style>
