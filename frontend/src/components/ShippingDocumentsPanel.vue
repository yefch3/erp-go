<template>
  <div class="documents-panel">
    <div class="documents-head">
      <div>
        <strong>合同与单证附件</strong>
        <span class="hint">文件保存在私有存储中；新版本不会覆盖旧版本</span>
      </div>
      <el-button v-if="canUpload" type="primary" @click="openUpload()">上传单证</el-button>
    </div>

    <el-alert v-if="!canView" type="warning" :closable="false" title="当前账号没有查看船期单证的权限" />
    <el-table v-else v-loading="loading" :data="documents" empty-text="暂无单证附件">
      <el-table-column label="分类" width="150">
        <template #default="{ row }">{{ categoryText(row.category) }}</template>
      </el-table-column>
      <el-table-column prop="fileName" label="文件名" min-width="220" />
      <el-table-column label="版本" width="75">
        <template #default="{ row }">v{{ row.version }}</template>
      </el-table-column>
      <el-table-column label="大小" width="90">
        <template #default="{ row }">{{ formatSize(row.fileSize) }}</template>
      </el-table-column>
      <el-table-column label="上传信息" width="190">
        <template #default="{ row }">
          <div>{{ row.uploadedByName || '—' }}</div>
          <small class="hint">{{ formatTime(row.uploadedAt) }}</small>
        </template>
      </el-table-column>
      <el-table-column prop="remark" label="备注" min-width="150" show-overflow-tooltip />
      <el-table-column label="状态" width="90">
        <template #default="{ row }">
          <el-tooltip v-if="row.status === 'VOIDED'" :content="`原因：${row.voidReason || '—'}`">
            <el-tag type="info">已作废</el-tag>
          </el-tooltip>
          <el-tag v-else type="success">有效</el-tag>
        </template>
      </el-table-column>
      <el-table-column label="操作" width="285" fixed="right">
        <template #default="{ row }">
          <el-button v-if="canPreview(row)" link type="primary" @click="access(row, 'preview')">预览</el-button>
          <el-button v-if="canDownload" link type="primary" @click="access(row, 'download')">下载</el-button>
          <el-button v-if="canUpload" link type="primary" @click="openUpload(row)">上传新版本</el-button>
          <el-button v-if="canInvalidate && row.status === 'ACTIVE'" link type="danger" @click="invalidate(row)">作废</el-button>
        </template>
      </el-table-column>
    </el-table>

    <el-dialog v-model="uploadOpen" :title="form.replacesDocumentId ? '上传单证新版本' : '上传新单证'" width="560px" destroy-on-close>
      <el-form label-width="100px">
		<el-alert v-if="form.replacesDocumentId" type="info" :closable="false" show-icon title="新文件将成为下一版本，原版本仍会保留" style="margin-bottom:16px" />
        <el-form-item label="文件分类" required>
          <el-select v-model="form.category" style="width:100%">
            <el-option v-for="item in categories" :key="item.value" :label="item.label" :value="item.value" />
          </el-select>
        </el-form-item>
        <el-form-item label="选择文件" required>
          <input type="file" accept=".pdf,.doc,.docx,.xls,.xlsx,.png,.jpg,.jpeg" @change="selectFile" />
        </el-form-item>
        <el-form-item label="备注">
          <el-input v-model="form.remark" type="textarea" :rows="3" maxlength="500" show-word-limit />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="uploadOpen=false">取消</el-button>
        <el-button type="primary" :loading="uploading" @click="upload">上传</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { get, post } from '../api'
import type { ShippingDocument } from '../shipping'
import { useAuthStore } from '../stores/auth'

const props = defineProps<{ scheduleId: string }>()
const emit = defineEmits<{ changed: [] }>()
const auth = useAuthStore()
const loading = ref(false)
const uploading = ref(false)
const uploadOpen = ref(false)
const selectedFile = ref<File>()
const documents = ref<ShippingDocument[]>([])
const form = reactive({ category: 'OTHER', remark: '', replacesDocumentId: '' })

const canView = computed(() => auth.can('shipping:document:view'))
const canDownload = computed(() => auth.can('shipping:document:download'))
const canUpload = computed(() => auth.can('shipping:document:upload'))
const canInvalidate = computed(() => auth.can('shipping:document:invalidate'))

const categories = [
  ['EXPORT_CONTRACT', '外销合同'], ['BOOKING_CONFIRMATION', '订舱确认书'],
  ['COMMERCIAL_INVOICE', '商业发票'], ['PACKING_LIST', '装箱单'],
  ['CUSTOMS_DOCUMENT', '报关资料'], ['BILL_OF_LADING_DRAFT', '提单草稿'],
  ['BILL_OF_LADING_FINAL', '正式提单'], ['ARRIVAL_NOTICE', '到港通知'], ['OTHER', '其他附件'],
].map(([value, label]) => ({ value, label }))

async function load() {
  if (!canView.value) return
  loading.value = true
  try {
    documents.value = (await get<{ documents: ShippingDocument[] }>(`/shipping/schedules/${props.scheduleId}/documents`)).documents ?? []
  } finally {
    loading.value = false
  }
}

function openUpload(previous?: ShippingDocument) {
  form.category = previous?.category ?? 'OTHER'
  form.remark = ''
  form.replacesDocumentId = previous?.id ?? ''
  selectedFile.value = undefined
  uploadOpen.value = true
}

function selectFile(event: Event) {
  selectedFile.value = (event.target as HTMLInputElement).files?.[0]
}

async function upload() {
  const file = selectedFile.value
  if (!file) return ElMessage.warning('请选择要上传的文件')
  if (file.size <= 0 || file.size > 20 * 1024 * 1024) return ElMessage.warning('文件不能为空，且不能超过 20 MB')
  uploading.value = true
  try {
    const signed = await post<{ fileKey: string; uploadUrl: string }>(`/shipping/schedules/${props.scheduleId}/documents/presign`, { fileName: file.name })
    const result = await fetch(signed.uploadUrl, { method: 'PUT', body: file, headers: { 'Content-Type': file.type || 'application/octet-stream' } })
    if (!result.ok) throw new Error(`对象存储上传失败（${result.status}）`)
    await post(`/shipping/schedules/${props.scheduleId}/documents`, {
      fileKey: signed.fileKey, fileName: file.name, category: form.category,
      remark: form.remark,
      ...(form.replacesDocumentId ? { replacesDocumentId: form.replacesDocumentId } : {}),
    })
    ElMessage.success(form.replacesDocumentId ? '单证新版本已上传，旧版本已保留' : '单证已上传')
    uploadOpen.value = false
    await load()
    emit('changed')
  } finally {
    uploading.value = false
  }
}

function canPreview(row: ShippingDocument) {
  return canView.value && ['application/pdf', 'image/png', 'image/jpeg'].includes(row.contentType)
}

async function access(row: ShippingDocument, mode: 'preview' | 'download') {
	// Preview needs a tab reserved during the click or browsers may block it.
	// Downloads use a temporary link and therefore do not leave a blank tab.
	const target = mode === 'preview' ? window.open('about:blank', '_blank') : null
  try {
    const data = await post<{ url: string }>(`/shipping/schedules/${props.scheduleId}/documents/${row.id}/${mode}`)
		if (mode === 'download') {
			const link = document.createElement('a')
			link.href = data.url
			link.style.display = 'none'
			document.body.appendChild(link)
			link.click()
			link.remove()
			return
		}
    if (target) {
      target.opener = null
      target.location.href = data.url
      return
    }
    window.location.href = data.url
  } catch (error) {
    target?.close()
    throw error
  }
}

async function invalidate(row: ShippingDocument) {
  let reason = ''
  try {
    ({ value: reason } = await ElMessageBox.prompt(`作废后仍保留“${row.fileName}”及其历史版本。请输入原因。`, '作废单证', {
      type: 'warning', confirmButtonText: '确认作废', inputValidator: value => !!value.trim() || '作废原因必填',
    }))
  } catch (action) {
    if (action === 'cancel' || action === 'close') return
    throw action
  }
  await post(`/shipping/schedules/${props.scheduleId}/documents/${row.id}/invalidate`, { reason })
  ElMessage.success('单证已作废，历史记录仍然保留')
  await load()
  emit('changed')
}

function categoryText(value: string) { return categories.find(item => item.value === value)?.label ?? value }
function formatTime(value: string) { return value ? new Date(value).toLocaleString() : '—' }
function formatSize(value: string) {
  const size = Number(value)
  return size >= 1024 * 1024 ? `${(size / 1024 / 1024).toFixed(1)} MB` : `${Math.max(1, Math.round(size / 1024))} KB`
}

onMounted(load)
</script>

<style scoped>
.documents-head{display:flex;justify-content:space-between;align-items:center;margin-bottom:16px}.documents-head>div{display:flex;align-items:center;gap:12px}.hint{color:var(--el-text-color-secondary)}
</style>
