<template>
  <el-dialog v-model="visible" title="批量导入客户" width="820px" destroy-on-close>
    <el-alert type="info" :closable="false" show-icon>
      <template #title>支持 CSV 文件或直接粘贴表格；先预检，全部正确后才允许整批写入。</template>
    </el-alert>
    <div class="import-tools">
      <el-upload :auto-upload="false" :show-file-list="false" accept=".csv,.txt" :on-change="readFile">
        <el-button>选择 CSV 文件</el-button>
      </el-upload>
      <el-button text type="primary" @click="downloadTemplate">下载模板</el-button>
      <el-button text type="primary" @click="fillExample">填入示例</el-button>
      <span>列顺序：编码、名称、国家代码、客户类型、币种、付款方式、联系人、邮箱、电话、备注</span>
    </div>
    <el-input v-model="source" type="textarea" :rows="9" placeholder="可从 Excel 复制并粘贴到这里" />
    <div v-if="verdicts.length" class="preview">
      <div class="preview-head">
        <strong>预检结果</strong>
        <span class="ok">可导入 {{ ready }} 行</span>
        <span v-if="blocked" class="blocked">需修正 {{ blocked }} 行</span>
      </div>
      <el-table :data="verdicts" max-height="260" size="small">
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
      <el-button :loading="checking" @click="preview">预检</el-button>
      <el-button type="primary" :disabled="!verdicts.length || blocked > 0" :loading="saving" @click="commit">确认导入 {{ ready }} 行</el-button>
    </template>
  </el-dialog>
</template>

<script setup lang="ts">
import { computed, ref } from 'vue'
import { ElMessage, type UploadFile } from 'element-plus'
import { post } from '../api'

const props = defineProps<{ open: boolean }>()
const emit = defineEmits<{ 'update:open': [value: boolean]; imported: [] }>()
const visible = computed({ get: () => props.open, set: value => emit('update:open', value) })
const source = ref('')
const verdicts = ref<any[]>([])
const checking = ref(false)
const saving = ref(false)
const ready = computed(() => verdicts.value.filter(v => v.ok).length)
const blocked = computed(() => verdicts.value.filter(v => !v.ok).length)

function parseRows() {
  const lines = source.value.split(/\r?\n/).map(v => v.trim()).filter(Boolean)
  if (lines.length && /名称|name/i.test(lines[0])) lines.shift()
  return lines.map(line => {
    const cells = line.includes('\t') ? line.split('\t') : line.split(',')
    const [code, name, countryCode, customerType, currency, paymentTerm, contactName, contactEmail, contactPhone, remark] = cells.map(v => v.trim())
    return { code, name, countryCode, customerType, currency, paymentTerm, contactName, contactEmail, contactPhone, remark }
  })
}

async function readFile(file: UploadFile) {
  if (file.raw) source.value = await file.raw.text()
  verdicts.value = []
}
function fillExample() {
  source.value = '编码,名称,国家代码,客户类型,币种,付款方式,联系人,邮箱,电话,备注\n,示例客户,US,IMPORTER,USD,TT,Amy,amy@example.com,+1 2025550100,展会客户'
  verdicts.value = []
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
  const rows = parseRows(); if (!rows.length) { ElMessage.warning('请先选择文件或粘贴客户数据'); return }
  checking.value = true
  try { const data = await post<any>('/customers/import', { rows, dryRun: true }); verdicts.value = data.verdicts ?? [] } finally { checking.value = false }
}
async function commit() {
  saving.value = true
  try {
    const data = await post<any>('/customers/import', { rows: parseRows(), dryRun: false })
    ElMessage.success(`已导入 ${data.imported ?? 0} 个客户`)
    visible.value = false; source.value = ''; verdicts.value = []; emit('imported')
  } finally { saving.value = false }
}
</script>

<style scoped>
.import-tools { display:flex; align-items:center; gap:12px; margin:16px 0 10px; color:var(--el-text-color-secondary); font-size:12px; }
.preview { margin-top:16px; border:1px solid var(--el-border-color-lighter); border-radius:10px; overflow:hidden; }
.preview-head { display:flex; gap:18px; padding:12px 16px; background:var(--el-fill-color-light); }
.ok { color:var(--el-color-success); }.blocked { color:var(--el-color-danger); }
</style>
