<template>
  <el-dialog v-model="visible" :title="t(kind === 'supplier' ? 'suppliers.importSuppliers' : 'suppliers.importFactories')" width="760px" @closed="reset">
    <el-alert :title="t('suppliers.importHint')" type="info" :closable="false" show-icon />
    <div class="toolbar">
      <el-button @click="downloadTemplate">{{ t('suppliers.downloadTemplate') }}</el-button>
      <input type="file" accept=".csv,text/csv" @change="chooseFile" />
    </div>
    <el-table v-if="rows.length" :data="rows.slice(0, 8)" max-height="280">
      <el-table-column prop="rowNumber" :label="t('suppliers.rowNumber')" width="70" />
      <el-table-column prop="code" :label="t('suppliers.code')" width="130" />
      <el-table-column :label="t('suppliers.name')"><template #default="{row}">{{ row.nameZh || row.nameEn }}</template></el-table-column>
    </el-table>
    <el-alert v-if="preview" class="result" :type="preview.issues?.length ? 'error' : 'success'" :closable="false">
      <template #title>{{ t('suppliers.importSummary', { ready: preview.readyCount || 0, errors: preview.issues?.length || 0 }) }}</template>
      <ul v-if="preview.issues?.length"><li v-for="issue in preview.issues" :key="`${issue.rowNumber}-${issue.code}`">{{ t('suppliers.importIssue', { row: issue.rowNumber, message: issue.message }) }}</li></ul>
    </el-alert>
    <template #footer>
      <el-button @click="visible=false">{{ t('common.cancel') }}</el-button>
      <el-button :disabled="!rows.length" :loading="loading" @click="previewRows">{{ t('suppliers.previewImport') }}</el-button>
      <el-button type="primary" :disabled="!preview || preview.issues?.length || !rows.length" :loading="loading" @click="confirmImport">{{ t('suppliers.confirmImport') }}</el-button>
    </template>
  </el-dialog>
</template>

<script setup lang="ts">
import { computed, ref } from 'vue'
import { ElMessage } from 'element-plus'
import { useI18n } from 'vue-i18n'
import { post } from '../api'
import { importTemplate, parseImportCSV, type ImportKind } from '../lib/supplierFactoryImport'

const props = defineProps<{ modelValue: boolean; kind: ImportKind }>()
const emit = defineEmits<{ 'update:modelValue': [value: boolean]; imported: [] }>()
const { t } = useI18n()
const visible = computed({ get: () => props.modelValue, set: (value) => emit('update:modelValue', value) })
const rows = ref<Record<string, unknown>[]>([])
const preview = ref<any>(null)
const loading = ref(false)

function reset() { rows.value = []; preview.value = null }
function downloadTemplate() {
  const blob = new Blob([importTemplate(props.kind)], { type: 'text/csv;charset=utf-8' })
  const url = URL.createObjectURL(blob); const link = document.createElement('a')
  link.href = url; link.download = props.kind === 'supplier' ? 'supplier-import-template.csv' : 'factory-import-template.csv'; link.click(); URL.revokeObjectURL(url)
}
async function chooseFile(event: Event) {
  const file = (event.target as HTMLInputElement).files?.[0]
  if (!file) return
  try { rows.value = parseImportCSV(await file.text(), props.kind); preview.value = null }
  catch { rows.value = []; ElMessage.error(t('suppliers.importColumnsInvalid')) }
}
async function request(confirm: boolean) {
  loading.value = true
  try { return await post<any>(props.kind === 'supplier' ? '/suppliers/import' : '/factories/import', { rows: rows.value, confirm }) }
  finally { loading.value = false }
}
async function previewRows() { preview.value = await request(false) }
async function confirmImport() {
  const result = await request(true); preview.value = result
  if (result.importedCount > 0) { ElMessage.success(t('suppliers.imported', { count: result.importedCount })); emit('imported'); visible.value = false }
}
</script>

<style scoped>
.toolbar{display:flex;align-items:center;gap:16px;margin:18px 0}.result{margin-top:16px}.result ul{margin:8px 0 0;padding-left:20px;max-height:120px;overflow:auto}
</style>
