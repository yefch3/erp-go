<template>
  <el-button v-if="allowed" type="danger" plain @click="openDialog">{{ t('masterDelete.button') }}</el-button>
  <el-dialog v-model="open" :title="t('masterDelete.title')" width="1000px" class="master-delete-dialog" append-to-body :close-on-click-modal="false" :before-close="closeDialog">
    <el-alert :title="t('masterDelete.warning')" type="warning" :closable="false" show-icon />
    <div class="delete-filters">
      <label>{{ t('masterDelete.createdAt') }}</label>
      <el-date-picker v-model="dates" type="daterange" value-format="YYYY-MM-DD" :disabled="busy" :start-placeholder="t('masterDelete.start')" :end-placeholder="t('masterDelete.end')" @change="clearPreview" />
      <el-button type="primary" :disabled="dates?.length !== 2" :loading="busy" @click="preview">{{ t('masterDelete.preview') }}</el-button>
    </div>
    <p class="delete-hint">{{ t('masterDelete.scope') }} {{ t('masterDelete.timezone', { zone: timezone }) }}</p>
    <template v-if="previewed">
      <div class="delete-selection">
        <el-checkbox :model-value="allSelected" :indeterminate="selected.length > 0 && !allSelected" :disabled="busy || !eligible.length" @change="selectAll">{{ t('masterDelete.selectAll') }}</el-checkbox>
        <span>{{ t('masterDelete.summary', { total: rows.length, selected: selected.length, blocked: rows.length - eligible.length }) }}</span>
        <el-button link :disabled="busy" @click="selected=[]">{{ t('masterDelete.clear') }}</el-button>
      </div>
      <el-table :data="pageRows" max-height="310" row-key="id" border>
        <el-table-column width="48"><template #default="{ row }"><el-checkbox :model-value="selected.includes(row.id)" :disabled="busy || !!row.blockedReason" :aria-label="t('masterDelete.selectRecord', { name: row.name })" @change="toggle(row.id)" /></template></el-table-column>
        <el-table-column prop="code" :label="t('masterDelete.code')" width="125" />
        <el-table-column prop="name" :label="t('masterDelete.name')" min-width="190" show-overflow-tooltip />
        <el-table-column :label="t('masterDelete.createdAt')" width="170"><template #default="{ row }">{{ formatDate(row.createdAt) }}</template></el-table-column>
        <el-table-column :label="t('masterDelete.result')" min-width="240"><template #default="{ row }"><span :class="{ blocked: row.blockedReason }">{{ row.blockedReason || t('masterDelete.available') }}</span></template></el-table-column>
      </el-table>
      <el-pagination class="delete-pager" v-model:current-page="page" :page-size="100" :total="rows.length" layout="total, prev, pager, next" :disabled="busy" />
    </template>
    <template #footer><el-button :disabled="busy" @click="open=false">{{ t('masterDelete.close') }}</el-button><el-button type="danger" :disabled="!selected.length || !previewed" :loading="busy" @click="execute">{{ t('masterDelete.deleteSelected', { count: selected.length }) }}</el-button></template>
  </el-dialog>
</template>
<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { ElMessage, ElMessageBox } from 'element-plus'
import { get, post, quietErrors } from '../api'
import { deletionWindow, deleteSelections, type MasterDeleteRow } from '../lib/masterdataBulkDelete'
const props = defineProps<{ entity: 'CUSTOMER' | 'SUPPLIER' | 'PORT' }>()
const emit = defineEmits<{ deleted: [] }>()
const { t } = useI18n()
const allowed = ref(false), open = ref(false), busy = ref(false), previewed = ref(false)
const dates = ref<string[]>([]), rows = ref<MasterDeleteRow[]>([]), selected = ref<string[]>([]), page = ref(1)
const timezone = Intl.DateTimeFormat().resolvedOptions().timeZone
const eligible = computed(() => rows.value.filter(row => !row.blockedReason))
const allSelected = computed(() => eligible.value.length > 0 && selected.value.length === eligible.value.length)
const pageRows = computed(() => rows.value.slice((page.value - 1) * 100, page.value * 100))
onMounted(async () => { try { allowed.value = (await get<{ allowed: boolean }>('/masterdata/bulk-delete/access', undefined, quietErrors)).allowed } catch { allowed.value = false } })
function clearPreview() { rows.value = []; selected.value = []; previewed.value = false; page.value = 1 }
function openDialog() { clearPreview(); open.value = true }
function closeDialog(done: () => void) { if (!busy.value) done() }
function selectAll(value: unknown) { selected.value = value ? eligible.value.map(row => row.id) : [] }
function toggle(id: string) { selected.value = selected.value.includes(id) ? selected.value.filter(value => value !== id) : [...selected.value, id] }
function formatDate(value: string) { return new Date(value).toLocaleString() }
async function preview() {
  clearPreview(); busy.value = true
  try { const result = await post<{ rows: MasterDeleteRow[] }>('/masterdata/bulk-delete/preview', { entity: props.entity, ...deletionWindow(dates.value || []) }, { timeout: 60000 }); rows.value = result.rows || []; previewed.value = true }
  finally { busy.value = false }
}
async function execute() {
  if (busy.value || !previewed.value) return
  const selections = deleteSelections(rows.value, selected.value)
  if (!selections.length) return
  // Freeze the selection before asking for confirmation.
  busy.value = true
  try {
    try { await ElMessageBox.confirm(t('masterDelete.confirm', { count: selections.length }), t('masterDelete.confirmTitle'), { type: 'warning', confirmButtonText: t('masterDelete.confirmDelete'), cancelButtonText: t('masterDelete.close'), closeOnClickModal: false }) } catch { return }
    const result = await post<{ deletedCount: number }>('/masterdata/bulk-delete/execute', { entity: props.entity, ...deletionWindow(dates.value), selections }, { timeout: 60000 })
    ElMessage.success(t('masterDelete.deleted', { count: result.deletedCount })); clearPreview(); open.value = false; emit('deleted')
  } catch { clearPreview() } finally { busy.value = false }
}
</script>
<style scoped>
.delete-filters, .delete-selection { display: flex; align-items: center; gap: 12px; flex-wrap: wrap; margin: 16px 0 10px; }
.delete-filters :deep(.el-date-editor) { flex: 0 1 340px; }
.delete-hint { color: #64748b; font-size: 12px; }
.delete-pager { margin-top: 12px; justify-content: flex-end; }
.blocked { color: #b45309; }
</style>
<style>
.master-delete-dialog { max-width: calc(100vw - 32px); margin-top: 6vh !important; }
.master-delete-dialog .el-dialog__body { max-height: 72vh; overflow: auto; }
</style>
