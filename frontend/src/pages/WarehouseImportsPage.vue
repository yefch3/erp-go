<template>
  <div class="page">
    <header class="hero">
      <div>
        <p class="eyebrow">{{ t('warehouse.imports.eyebrow') }}</p>
        <h1>{{ t('warehouse.imports.title') }}</h1>
        <p>{{ t('warehouse.imports.subtitle') }}</p>
      </div>
      <div class="hero-actions">
        <el-button @click="router.push('/warehouses')">← {{ t('warehouse.backWorkbench') }}</el-button>
        <el-button type="primary" :loading="downloading" @click="downloadTemplate">{{ t('warehouse.imports.downloadTemplate') }}</el-button>
      </div>
    </header>

    <el-alert type="warning" :closable="false" show-icon>
      <template #title>{{ t('warehouse.imports.warningTitle') }}</template>
      {{ t('warehouse.imports.warningBody') }}
    </el-alert>

    <el-card shadow="never" class="upload-card">
      <template #header><div class="card-head"><div><h2>{{ t('warehouse.imports.uploadPreview') }}</h2><p>{{ t('warehouse.imports.uploadHint') }}</p></div><el-tag>WH4-INITIAL-STOCK-V1</el-tag></div></template>
      <div class="upload-grid">
        <el-form-item :label="t('warehouse.imports.externalBatch')">
          <el-input v-model="externalBatchNo" maxlength="100" :placeholder="t('warehouse.imports.externalPlaceholder')" />
        </el-form-item>
        <el-form-item :label="t('warehouse.imports.file')">
          <el-upload ref="uploadRef" :auto-upload="false" :limit="1" accept=".xlsx" :on-change="fileChanged" :on-remove="fileRemoved">
            <el-button>{{ t('warehouse.imports.chooseExcel') }}</el-button>
            <template #tip><span class="upload-tip">{{ selectedFile?.name || t('warehouse.imports.noFile') }}</span></template>
          </el-upload>
        </el-form-item>
      </div>
      <div class="form-actions"><el-button type="primary" :disabled="!selectedFile" :loading="previewing" @click="previewFile">{{ t('warehouse.imports.startPreview') }}</el-button></div>
    </el-card>

    <el-card v-if="preview" shadow="never" class="preview-card">
      <template #header>
        <div class="card-head">
          <div><h2>{{ t('warehouse.imports.previewResult', { no: preview.batch.batchNo }) }}</h2><p>{{ preview.batch.sourceFileName }}</p></div>
          <el-tag :type="statusType(preview.batch.status)">{{ statusText(preview.batch.status) }}</el-tag>
        </div>
      </template>
      <section class="summary">
        <div><span>{{ t('warehouse.imports.total') }}</span><strong>{{ preview.batch.totalCount }}</strong></div>
        <div class="ok"><span>{{ t('warehouse.imports.valid') }}</span><strong>{{ preview.batch.validCount }}</strong></div>
        <div class="warn"><span>{{ t('warehouse.imports.warning') }}</span><strong>{{ preview.batch.warningCount }}</strong></div>
        <div class="bad"><span>{{ t('warehouse.imports.error') }}</span><strong>{{ preview.batch.errorCount }}</strong></div>
        <div class="bad"><span>{{ t('warehouse.imports.duplicate') }}</span><strong>{{ preview.batch.duplicateCount }}</strong></div>
      </section>
      <el-table :data="preview.rows" max-height="430">
        <el-table-column prop="rowNumber" :label="t('warehouse.imports.row')" width="64" />
        <el-table-column :label="t('warehouse.imports.warehouse')" min-width="150"><template #default="{row}"><div>{{ row.warehouseName || row.warehouseCode }}</div><span class="sub">{{ row.warehouseCode }}</span></template></el-table-column>
        <el-table-column :label="t('warehouse.imports.productSku')" min-width="190"><template #default="{row}"><div>{{ row.productName || row.productCode }}</div><span class="sub">{{ row.productCode }}<template v-if="row.skuCode"> · {{ row.skuCode }}</template></span></template></el-table-column>
        <el-table-column :label="t('warehouse.imports.quantity')" width="135" align="right"><template #default="{row}">{{ row.qty }} {{ row.uomCode }}</template></el-table-column>
        <el-table-column :label="t('warehouse.imports.unitCost')" width="130" align="right"><template #default="{row}">{{ row.currency }} {{ row.unitCost }}</template></el-table-column>
        <el-table-column :label="t('warehouse.imports.result')" width="100"><template #default="{row}"><el-tag size="small" :type="verdictType(row.verdict)">{{ verdictText(row.verdict) }}</el-tag></template></el-table-column>
        <el-table-column :label="t('warehouse.imports.issues')" min-width="260"><template #default="{row}"><span v-if="!row.issues?.length" class="sub">—</span><div v-for="issue in row.issues" :key="issue.field+issue.message" class="issue">{{ issue.field }}: {{ issue.message }}</div></template></el-table-column>
      </el-table>
      <div class="preview-actions">
        <el-button v-if="preview.errorFileData" @click="savePreviewReport">{{ t('warehouse.imports.downloadErrors') }}</el-button>
        <el-button v-if="preview.batch.status === 'PREVIEW' || preview.batch.status === 'INVALID'" @click="cancelPreview">{{ t('warehouse.imports.discardPreview') }}</el-button>
        <el-button type="primary" :disabled="preview.batch.status !== 'PREVIEW'" :loading="confirming" @click="confirmPreview">{{ t('warehouse.imports.confirmPosting') }}</el-button>
      </div>
    </el-card>

    <el-card shadow="never" class="history-card">
      <template #header><div class="card-head"><div><h2>{{ t('warehouse.imports.history') }}</h2><p>{{ t('warehouse.imports.historyHint') }}</p></div><el-button link type="primary" @click="loadHistory">{{ t('warehouse.imports.refresh') }}</el-button></div></template>
      <el-table :data="history" v-loading="loadingHistory">
        <el-table-column :label="t('warehouse.imports.batch')" min-width="155"><template #default="{row}"><strong>{{ row.batchNo }}</strong><div class="sub">{{ row.externalBatchNo || t('warehouse.imports.noExternalBatch') }}</div></template></el-table-column>
        <el-table-column prop="sourceFileName" :label="t('warehouse.imports.file')" min-width="180" show-overflow-tooltip />
        <el-table-column :label="t('warehouse.imports.result')" width="170"><template #default="{row}"><el-tag size="small" :type="statusType(row.status)">{{ statusText(row.status) }}</el-tag><div class="sub">{{ t('warehouse.imports.valid') }} {{ row.validCount }} / {{ t('warehouse.imports.error') }} {{ Number(row.errorCount)+Number(row.duplicateCount) }}</div></template></el-table-column>
        <el-table-column :label="t('warehouse.imports.operatorTime')" min-width="180"><template #default="{row}"><div>{{ row.operatorName || '—' }}</div><span class="sub">{{ formatTime(row.confirmedAt || row.createdAt) }}</span></template></el-table-column>
        <el-table-column :label="t('warehouse.imports.actions')" width="115" align="right"><template #default="{row}"><el-button link type="primary" @click="downloadReport(row)">{{ t('warehouse.imports.downloadReport') }}</el-button></template></el-table-column>
        <template #empty><el-empty :description="t('warehouse.imports.emptyHistory')" /></template>
      </el-table>
      <div class="pagination"><span>{{ t('warehouse.imports.count', { count: total }) }}</span><el-pagination layout="prev, pager, next" :total="total" :page-size="20" v-model:current-page="pageNo" @current-change="loadHistory" /></div>
    </el-card>
  </div>
</template>

<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { ElMessage, ElMessageBox, type UploadFile, type UploadInstance } from 'element-plus'
import { download, get, post, saveBlob } from '../api'

interface Issue { field:string; value:string; message:string }
interface ImportRow { rowNumber:number; warehouseCode:string; warehouseName:string; productCode:string; productName:string; skuCode:string; qty:string; uomCode:string; unitCost:string; currency:string; remark:string; verdict:string; issues:Issue[] }
interface Batch { id:string; importToken:string; batchNo:string; sourceFileName:string; externalBatchNo:string; templateVersion:string; status:string; totalCount:number; validCount:number; warningCount:number; errorCount:number; duplicateCount:number; operatorName:string; createdAt:string; confirmedAt:string }
interface Preview { batch:Batch; rows:ImportRow[]; errorFileName?:string; errorFileData?:string }

const router=useRouter(), {t}=useI18n(), uploadRef=ref<UploadInstance>(), selectedFile=ref<File>(), externalBatchNo=ref('')
const downloading=ref(false), previewing=ref(false), confirming=ref(false), loadingHistory=ref(false)
const preview=ref<Preview>(), history=ref<Batch[]>([]), total=ref(0), pageNo=ref(1)

function fileChanged(file:UploadFile){selectedFile.value=file.raw}
function fileRemoved(){selectedFile.value=undefined}
async function fileBase64(file:File){const bytes=new Uint8Array(await file.arrayBuffer());let binary='';for(let i=0;i<bytes.length;i+=0x8000)binary+=String.fromCharCode(...bytes.subarray(i,i+0x8000));return btoa(binary)}
function base64Blob(value:string){const binary=atob(value),bytes=new Uint8Array(binary.length);for(let i=0;i<binary.length;i++)bytes[i]=binary.charCodeAt(i);return new Blob([bytes],{type:'application/vnd.openxmlformats-officedocument.spreadsheetml.sheet'})}
async function downloadTemplate(){downloading.value=true;try{const file=await download('/stock-imports/template');saveBlob(file.blob,file.fileName||'initial-stock-import-v1.xlsx')}finally{downloading.value=false}}
async function previewFile(){if(!selectedFile.value)return;previewing.value=true;try{preview.value=await post<Preview>('/stock-imports/preview',{file_data:await fileBase64(selectedFile.value),source_file_name:selectedFile.value.name,external_batch_no:externalBatchNo.value.trim()});ElMessage.success(t(preview.value.batch.status==='PREVIEW'?'warehouse.imports.previewPassed':'warehouse.imports.previewFailed'));await loadHistory()}finally{previewing.value=false}}
function savePreviewReport(){if(preview.value?.errorFileData)saveBlob(base64Blob(preview.value.errorFileData),preview.value.errorFileName||'initial-stock-errors.xlsx')}
async function confirmPreview(){if(!preview.value)return;await ElMessageBox.confirm(t('warehouse.imports.confirmMessage'),t('warehouse.imports.confirmTitle'),{type:'warning',confirmButtonText:t('warehouse.imports.confirmButton')});confirming.value=true;try{const data=await post<{batch:Batch}>(`/stock-imports/${encodeURIComponent(preview.value.batch.importToken)}/confirm`,{});preview.value.batch=data.batch;ElMessage.success(t('warehouse.imports.posted'));await loadHistory()}finally{confirming.value=false}}
async function cancelPreview(){if(!preview.value)return;await ElMessageBox.confirm(t('warehouse.imports.discardMessage'),t('warehouse.imports.discardTitle'),{type:'warning'});const data=await post<{batch:Batch}>(`/stock-imports/${encodeURIComponent(preview.value.batch.importToken)}/cancel`,{});preview.value.batch=data.batch;ElMessage.success(t('warehouse.imports.discarded'));await loadHistory()}
async function loadHistory(){loadingHistory.value=true;try{const data=await get<{batches:Batch[];meta?:{total?:number}}>('/stock-imports',{page:pageNo.value,page_size:20});history.value=data.batches??[];total.value=Number(data.meta?.total??0)}finally{loadingHistory.value=false}}
async function downloadReport(row:Batch){const file=await download(`/stock-imports/${encodeURIComponent(row.importToken)}/report`);saveBlob(file.blob,file.fileName||`${row.batchNo}-report.xlsx`)}
function statusText(v:string){return ['PREVIEW','INVALID','CONFIRMED','CANCELLED'].includes(v)?t(`warehouse.imports.status.${v}`):v}
function statusType(v:string):'success'|'warning'|'danger'|'info'{return v==='CONFIRMED'?'success':v==='PREVIEW'?'warning':v==='INVALID'?'danger':'info'}
function verdictText(v:string){return ['VALID','WARNING','ERROR','DUPLICATE'].includes(v)?t(`warehouse.imports.verdict.${v}`):v}
function verdictType(v:string):'success'|'warning'|'danger'|'info'{return v==='VALID'?'success':v==='WARNING'?'warning':v==='ERROR'?'danger':'info'}
function formatTime(v:string){return v?v.replace('T',' ').slice(0,16):'—'}
onMounted(loadHistory)
</script>

<style scoped>
.page{padding:28px;max-width:1500px;margin:auto}.hero,.card-head,.hero-actions,.preview-actions,.pagination{display:flex;align-items:center;justify-content:space-between;gap:14px}.hero{margin-bottom:18px}.hero h1{font-size:30px;margin:4px 0}.hero p,.card-head p{color:#738095;margin:5px 0}.eyebrow{font-size:12px!important;letter-spacing:2px;color:#087f78!important;font-weight:700}.upload-card,.preview-card,.history-card{margin-top:18px}.card-head h2{margin:0}.upload-grid{display:grid;grid-template-columns:1fr 1fr;gap:20px}.upload-tip{margin-left:12px;color:#738095}.form-actions,.preview-actions{justify-content:flex-end;margin-top:16px}.summary{display:grid;grid-template-columns:repeat(5,1fr);gap:12px;margin-bottom:16px}.summary div{padding:14px;border:1px solid #e1e8ef;border-radius:8px}.summary span{display:block;color:#738095}.summary strong{display:block;font-size:24px;margin-top:5px}.summary .ok strong{color:#1b9a65}.summary .warn strong{color:#d58a16}.summary .bad strong{color:#d34b4b}.sub{font-size:12px;color:#8793a5}.issue{color:#c84949;font-size:13px;line-height:1.5}.pagination{justify-content:flex-end;margin-top:14px;color:#66758b}@media(max-width:800px){.hero{align-items:flex-start;flex-direction:column}.upload-grid,.summary{grid-template-columns:1fr 1fr}}@media(max-width:520px){.page{padding:16px}.upload-grid,.summary{grid-template-columns:1fr}.hero-actions{flex-wrap:wrap}.preview-actions{flex-wrap:wrap}}
</style>
