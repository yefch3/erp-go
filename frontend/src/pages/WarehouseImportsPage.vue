<template>
  <div class="page">
    <header class="hero">
      <div>
        <p class="eyebrow">WAREHOUSE IMPORT</p>
        <h1>导入中心</h1>
        <p>期初库存必须先预检；确认后才增加库存并生成可追溯流水。</p>
      </div>
      <div class="hero-actions">
        <el-button @click="router.push('/warehouses')">← 返回仓库工作台</el-button>
        <el-button type="primary" :loading="downloading" @click="downloadTemplate">下载当前模板</el-button>
      </div>
    </header>

    <el-alert type="warning" :closable="false" show-icon>
      <template #title>期初库存只用于尚未产生库存流水的仓库</template>
      已确认批次不能撤销或覆盖；发现错误时必须通过后续库存调整纠正。
    </el-alert>

    <el-card shadow="never" class="upload-card">
      <template #header><div class="card-head"><div><h2>上传并预检</h2><p>仅接受系统下载的当前版本 .xlsx 模板，最多 1000 行。</p></div><el-tag>WH4-INITIAL-STOCK-V1</el-tag></div></template>
      <div class="upload-grid">
        <el-form-item label="外部批次号">
          <el-input v-model="externalBatchNo" maxlength="100" placeholder="建议填写盘点批次号，用于防止重复入账" />
        </el-form-item>
        <el-form-item label="期初库存文件">
          <el-upload ref="uploadRef" :auto-upload="false" :limit="1" accept=".xlsx" :on-change="fileChanged" :on-remove="fileRemoved">
            <el-button>选择 Excel</el-button>
            <template #tip><span class="upload-tip">{{ selectedFile?.name || '尚未选择文件' }}</span></template>
          </el-upload>
        </el-form-item>
      </div>
      <div class="form-actions"><el-button type="primary" :disabled="!selectedFile" :loading="previewing" @click="previewFile">开始预检</el-button></div>
    </el-card>

    <el-card v-if="preview" shadow="never" class="preview-card">
      <template #header>
        <div class="card-head">
          <div><h2>预检结果 · {{ preview.batch.batchNo }}</h2><p>{{ preview.batch.sourceFileName }}</p></div>
          <el-tag :type="statusType(preview.batch.status)">{{ statusText(preview.batch.status) }}</el-tag>
        </div>
      </template>
      <section class="summary">
        <div><span>总行数</span><strong>{{ preview.batch.totalCount }}</strong></div>
        <div class="ok"><span>有效</span><strong>{{ preview.batch.validCount }}</strong></div>
        <div class="warn"><span>警告</span><strong>{{ preview.batch.warningCount }}</strong></div>
        <div class="bad"><span>错误</span><strong>{{ preview.batch.errorCount }}</strong></div>
        <div class="bad"><span>重复</span><strong>{{ preview.batch.duplicateCount }}</strong></div>
      </section>
      <el-table :data="preview.rows" max-height="430">
        <el-table-column prop="rowNumber" label="行" width="64" />
        <el-table-column label="仓库" min-width="150"><template #default="{row}"><div>{{ row.warehouseName || row.warehouseCode }}</div><span class="sub">{{ row.warehouseCode }}</span></template></el-table-column>
        <el-table-column label="产品 / SKU" min-width="190"><template #default="{row}"><div>{{ row.productName || row.productCode }}</div><span class="sub">{{ row.productCode }}<template v-if="row.skuCode"> · {{ row.skuCode }}</template></span></template></el-table-column>
        <el-table-column label="数量" width="135" align="right"><template #default="{row}">{{ row.qty }} {{ row.uomCode }}</template></el-table-column>
        <el-table-column label="单位成本" width="130" align="right"><template #default="{row}">{{ row.currency }} {{ row.unitCost }}</template></el-table-column>
        <el-table-column label="结果" width="100"><template #default="{row}"><el-tag size="small" :type="verdictType(row.verdict)">{{ verdictText(row.verdict) }}</el-tag></template></el-table-column>
        <el-table-column label="问题" min-width="260"><template #default="{row}"><span v-if="!row.issues?.length" class="sub">—</span><div v-for="issue in row.issues" :key="issue.field+issue.message" class="issue">{{ issue.field }}：{{ issue.message }}</div></template></el-table-column>
      </el-table>
      <div class="preview-actions">
        <el-button v-if="preview.errorFileData" @click="savePreviewReport">下载错误报告</el-button>
        <el-button v-if="preview.batch.status === 'PREVIEW' || preview.batch.status === 'INVALID'" @click="cancelPreview">放弃本次预检</el-button>
        <el-button type="primary" :disabled="preview.batch.status !== 'PREVIEW'" :loading="confirming" @click="confirmPreview">确认期初库存入账</el-button>
      </div>
    </el-card>

    <el-card shadow="never" class="history-card">
      <template #header><div class="card-head"><div><h2>导入记录</h2><p>所有预检、失败、确认和放弃批次都会保留。</p></div><el-button link type="primary" @click="loadHistory">刷新</el-button></div></template>
      <el-table :data="history" v-loading="loadingHistory">
        <el-table-column label="批次" min-width="155"><template #default="{row}"><strong>{{ row.batchNo }}</strong><div class="sub">{{ row.externalBatchNo || '无外部批次号' }}</div></template></el-table-column>
        <el-table-column prop="sourceFileName" label="文件" min-width="180" show-overflow-tooltip />
        <el-table-column label="结果" width="170"><template #default="{row}"><el-tag size="small" :type="statusType(row.status)">{{ statusText(row.status) }}</el-tag><div class="sub">有效 {{ row.validCount }} / 错误 {{ Number(row.errorCount)+Number(row.duplicateCount) }}</div></template></el-table-column>
        <el-table-column label="操作人 / 时间" min-width="180"><template #default="{row}"><div>{{ row.operatorName || '—' }}</div><span class="sub">{{ formatTime(row.confirmedAt || row.createdAt) }}</span></template></el-table-column>
        <el-table-column label="操作" width="115" align="right"><template #default="{row}"><el-button link type="primary" @click="downloadReport(row)">下载报告</el-button></template></el-table-column>
        <template #empty><el-empty description="暂无导入记录" /></template>
      </el-table>
      <div class="pagination"><span>共 {{ total }} 条</span><el-pagination layout="prev, pager, next" :total="total" :page-size="20" v-model:current-page="pageNo" @current-change="loadHistory" /></div>
    </el-card>
  </div>
</template>

<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import { ElMessage, ElMessageBox, type UploadFile, type UploadInstance } from 'element-plus'
import { download, get, post, saveBlob } from '../api'

interface Issue { field:string; value:string; message:string }
interface ImportRow { rowNumber:number; warehouseCode:string; warehouseName:string; productCode:string; productName:string; skuCode:string; qty:string; uomCode:string; unitCost:string; currency:string; remark:string; verdict:string; issues:Issue[] }
interface Batch { id:string; importToken:string; batchNo:string; sourceFileName:string; externalBatchNo:string; templateVersion:string; status:string; totalCount:number; validCount:number; warningCount:number; errorCount:number; duplicateCount:number; operatorName:string; createdAt:string; confirmedAt:string }
interface Preview { batch:Batch; rows:ImportRow[]; errorFileName?:string; errorFileData?:string }

const router=useRouter(), uploadRef=ref<UploadInstance>(), selectedFile=ref<File>(), externalBatchNo=ref('')
const downloading=ref(false), previewing=ref(false), confirming=ref(false), loadingHistory=ref(false)
const preview=ref<Preview>(), history=ref<Batch[]>([]), total=ref(0), pageNo=ref(1)

function fileChanged(file:UploadFile){selectedFile.value=file.raw}
function fileRemoved(){selectedFile.value=undefined}
async function fileBase64(file:File){const bytes=new Uint8Array(await file.arrayBuffer());let binary='';for(let i=0;i<bytes.length;i+=0x8000)binary+=String.fromCharCode(...bytes.subarray(i,i+0x8000));return btoa(binary)}
function base64Blob(value:string){const binary=atob(value),bytes=new Uint8Array(binary.length);for(let i=0;i<binary.length;i++)bytes[i]=binary.charCodeAt(i);return new Blob([bytes],{type:'application/vnd.openxmlformats-officedocument.spreadsheetml.sheet'})}
async function downloadTemplate(){downloading.value=true;try{const file=await download('/stock-imports/template');saveBlob(file.blob,file.fileName||'initial-stock-import-v1.xlsx')}finally{downloading.value=false}}
async function previewFile(){if(!selectedFile.value)return;previewing.value=true;try{preview.value=await post<Preview>('/stock-imports/preview',{file_data:await fileBase64(selectedFile.value),source_file_name:selectedFile.value.name,external_batch_no:externalBatchNo.value.trim()});ElMessage.success(preview.value.batch.status==='PREVIEW'?'预检通过，可以确认入账':'预检完成，请先处理错误行');await loadHistory()}finally{previewing.value=false}}
function savePreviewReport(){if(preview.value?.errorFileData)saveBlob(base64Blob(preview.value.errorFileData),preview.value.errorFileName||'initial-stock-errors.xlsx')}
async function confirmPreview(){if(!preview.value)return;await ElMessageBox.confirm('确认后会立即增加库存并写入期初流水，且不能撤销。是否继续？','确认期初库存',{type:'warning',confirmButtonText:'确认入账'});confirming.value=true;try{const data=await post<{batch:Batch}>(`/stock-imports/${encodeURIComponent(preview.value.batch.importToken)}/confirm`,{});preview.value.batch=data.batch;ElMessage.success('期初库存已入账');await loadHistory()}finally{confirming.value=false}}
async function cancelPreview(){if(!preview.value)return;await ElMessageBox.confirm('放弃后不会产生任何库存变化。','放弃预检',{type:'warning'});const data=await post<{batch:Batch}>(`/stock-imports/${encodeURIComponent(preview.value.batch.importToken)}/cancel`,{});preview.value.batch=data.batch;ElMessage.success('本次预检已放弃');await loadHistory()}
async function loadHistory(){loadingHistory.value=true;try{const data=await get<{batches:Batch[];meta?:{total?:number}}>('/stock-imports',{page:pageNo.value,page_size:20});history.value=data.batches??[];total.value=Number(data.meta?.total??0)}finally{loadingHistory.value=false}}
async function downloadReport(row:Batch){const file=await download(`/stock-imports/${encodeURIComponent(row.importToken)}/report`);saveBlob(file.blob,file.fileName||`${row.batchNo}-report.xlsx`)}
function statusText(v:string){return({PREVIEW:'待确认',INVALID:'预检未通过',CONFIRMED:'已入账',CANCELLED:'已放弃'} as Record<string,string>)[v]??v}
function statusType(v:string):'success'|'warning'|'danger'|'info'{return v==='CONFIRMED'?'success':v==='PREVIEW'?'warning':v==='INVALID'?'danger':'info'}
function verdictText(v:string){return({VALID:'有效',WARNING:'警告',ERROR:'错误',DUPLICATE:'重复'} as Record<string,string>)[v]??v}
function verdictType(v:string):'success'|'warning'|'danger'|'info'{return v==='VALID'?'success':v==='WARNING'?'warning':v==='ERROR'?'danger':'info'}
function formatTime(v:string){return v?v.replace('T',' ').slice(0,16):'—'}
onMounted(loadHistory)
</script>

<style scoped>
.page{padding:28px;max-width:1500px;margin:auto}.hero,.card-head,.hero-actions,.preview-actions,.pagination{display:flex;align-items:center;justify-content:space-between;gap:14px}.hero{margin-bottom:18px}.hero h1{font-size:30px;margin:4px 0}.hero p,.card-head p{color:#738095;margin:5px 0}.eyebrow{font-size:12px!important;letter-spacing:2px;color:#087f78!important;font-weight:700}.upload-card,.preview-card,.history-card{margin-top:18px}.card-head h2{margin:0}.upload-grid{display:grid;grid-template-columns:1fr 1fr;gap:20px}.upload-tip{margin-left:12px;color:#738095}.form-actions,.preview-actions{justify-content:flex-end;margin-top:16px}.summary{display:grid;grid-template-columns:repeat(5,1fr);gap:12px;margin-bottom:16px}.summary div{padding:14px;border:1px solid #e1e8ef;border-radius:8px}.summary span{display:block;color:#738095}.summary strong{display:block;font-size:24px;margin-top:5px}.summary .ok strong{color:#1b9a65}.summary .warn strong{color:#d58a16}.summary .bad strong{color:#d34b4b}.sub{font-size:12px;color:#8793a5}.issue{color:#c84949;font-size:13px;line-height:1.5}.pagination{justify-content:flex-end;margin-top:14px;color:#66758b}@media(max-width:800px){.hero{align-items:flex-start;flex-direction:column}.upload-grid,.summary{grid-template-columns:1fr 1fr}}@media(max-width:520px){.page{padding:16px}.upload-grid,.summary{grid-template-columns:1fr}.hero-actions{flex-wrap:wrap}.preview-actions{flex-wrap:wrap}}
</style>
