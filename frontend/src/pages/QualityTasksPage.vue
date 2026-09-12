<template>
  <div class="quality-page">
    <WorkflowPageHeader :title="t('quality.title')" :description="t('quality.subtitle')" />
    <el-card shadow="never">
      <el-radio-group v-model="tab" class="tabs" @change="reload">
        <el-radio-button value="PENDING">{{ t('quality.pending') }}</el-radio-button>
        <el-radio-button value="COMPLETED">{{ t('quality.completed') }}</el-radio-button>
      </el-radio-group>
      <div class="filters">
        <el-input v-model="keyword" clearable :placeholder="t('quality.search')" @keyup.enter="reload" @clear="reload" />
        <el-button @click="reload">{{ t('common.query') }}</el-button>
      </div>
      <el-table :data="rows" v-loading="loading">
        <el-table-column prop="taskNo" :label="t('quality.taskNo')" min-width="180"><template #default="{ row }"><b>{{ row.taskNo }}</b><div class="sub">{{ fmt(row.requestedAt) }}</div></template></el-table-column>
        <el-table-column prop="poNo" :label="t('quality.poNo')" min-width="160" />
        <el-table-column prop="supplierName" :label="t('quality.factory')" min-width="180" />
        <el-table-column :label="t('quality.batch')" width="100"><template #default="{ row }">#{{ row.batchNo }}</template></el-table-column>
        <el-table-column prop="expectedDate" :label="t('quality.expectedDate')" width="150" />
        <el-table-column :label="t('common.status')" width="130"><template #default="{ row }"><el-tag :type="row.status === 'COMPLETED' ? 'success' : 'warning'" effect="plain">{{ statusLabel(row.status) }}</el-tag></template></el-table-column>
        <el-table-column :label="t('common.actions')" width="140" align="center"><template #default="{ row }"><el-button type="primary" plain size="small" @click="openTask(row)">{{ canWrite && row.status !== 'COMPLETED' ? t('quality.process') : t('common.detail') }}</el-button></template></el-table-column>
        <template #empty>{{ t('quality.empty') }}</template>
      </el-table>
      <el-pagination class="pager" layout="total, prev, pager, next" :total="total" :page-size="20" :current-page="page" @current-change="(p:number) => { page = p; load() }" />
    </el-card>

    <el-dialog v-model="detailOpen" fullscreen :title="task?.taskNo || t('quality.title')" class="quality-dialog">
      <template v-if="task">
        <section class="summary">
          <div><span>{{ t('quality.poNo') }}</span><b>{{ task.poNo }}</b></div>
          <div><span>{{ t('quality.factory') }}</span><b>{{ task.supplierName }}</b></div>
          <div><span>{{ t('quality.expectedDate') }}</span><b>{{ task.expectedDate || '—' }}</b></div>
          <div><span>{{ t('common.status') }}</span><b>{{ statusLabel(task.status) }}</b></div>
        </section>
        <section class="task-context">
          <div><span>{{ t('quality.location') }}</span><b>{{ task.inspectionLocation || '—' }}</b></div>
          <div><span>{{ t('quality.contactName') }}</span><b>{{ task.contactName || '—' }}<small v-if="task.contactPhone">{{ task.contactPhone }}</small></b></div>
          <div><span>{{ t('quality.requestedBy') }}</span><b>{{ task.requestedByName || '—' }}</b></div>
          <div v-if="task.remark"><span>{{ t('quality.remark') }}</span><b>{{ task.remark }}</b></div>
        </section>
        <el-alert v-if="!canWrite" type="info" :closable="false" show-icon>{{ t('quality.readOnlyHint') }}</el-alert>

        <section class="panel product-summary">
          <div class="section-title"><div><h3>{{ t('quality.products') }}</h3><p>{{ t('quality.productsHint') }}</p></div></div>
          <el-table :data="task.lines" border>
            <el-table-column prop="productName" :label="t('quality.product')" min-width="210"><template #default="{ row }"><b>{{ row.productName }}</b><div class="sub">{{ row.spec || '—' }}</div></template></el-table-column>
            <el-table-column :label="t('quality.requestedQty')" width="150"><template #default="{ row }">{{ trim(row.requestedQty) }} {{ row.uomCode }}</template></el-table-column>
            <el-table-column :label="t('quality.qualifiedQty')" width="150"><template #default="{ row }">{{ trim(row.qualifiedQty) }} {{ row.uomCode }}</template></el-table-column>
            <el-table-column :label="t('quality.unresolvedQty')" width="160"><template #default="{ row }">{{ trim(row.unresolvedQty) }} {{ row.uomCode }}</template></el-table-column>
            <el-table-column :label="t('quality.result')" width="130"><template #default="{ row }">{{ resultLabel(row.finalResult) }}</template></el-table-column>
            <el-table-column :label="t('quality.releaseQty')" width="180"><template #default="{ row }"><el-input-number v-if="canRelease && Number(row.qualifiedQty) > 0 && task.status !== 'COMPLETED'" v-model="releaseOf[row.id]" :min="0" :max="Number(row.qualifiedQty)" :precision="4" controls-position="right" /><span v-else>{{ trim(row.approvedReleaseQty) }} {{ row.uomCode }}</span></template></el-table-column>
          </el-table>
          <div v-if="canRelease && task.status !== 'COMPLETED'" class="right"><el-button type="primary" plain @click="saveRelease">{{ t('quality.saveRelease') }}</el-button></div>
        </section>

        <section v-if="canWrite && task.status !== 'COMPLETED'" class="panel record-panel">
          <div class="section-title"><div><h3>{{ task.rounds.length ? t('quality.reinspect') : t('quality.recordRound') }}</h3><p>{{ t('quality.recordHint') }}</p></div><el-tag v-if="task.rounds.length" type="warning" effect="plain">{{ t('quality.roundNo', { n: task.rounds.length + 1 }) }}</el-tag></div>
          <div class="round-head">
            <el-form-item :label="t('quality.inspectedAt')"><el-date-picker v-model="round.inspectedAt" type="datetime" value-format="YYYY-MM-DDTHH:mm:ssZ" /></el-form-item>
            <el-form-item :label="t('quality.location')"><el-input v-model="round.location" /></el-form-item>
            <el-form-item :label="t('quality.roundRemark')"><el-input v-model="round.remark" :placeholder="t('quality.roundRemarkHint')" /></el-form-item>
          </div>
          <article v-for="line in unresolvedLines" :key="line.id" class="line-editor">
            <header class="line-title">
              <div><b>{{ line.productName }}</b><span>{{ line.spec || '—' }}</span></div>
              <div class="line-quantity"><span>{{ t('quality.thisBatchQty') }}</span><strong>{{ trim(line.unresolvedQty) }} {{ line.uomCode }}</strong></div>
            </header>
            <div class="result-grid">
              <el-form-item :label="t('quality.result')" required><el-select v-model="roundOf[line.id].result" @change="applyResult(line.id)"><el-option value="PASS" :label="resultLabel('PASS')" /><el-option value="PARTIAL" :label="resultLabel('PARTIAL')" /><el-option value="FAIL" :label="resultLabel('FAIL')" /></el-select></el-form-item>
              <el-form-item :label="t('quality.inspectedQty')" required><el-input v-model="roundOf[line.id].inspectedQty" inputmode="decimal" @input="syncBad(line.id)"><template #append>{{ line.uomCode }}</template></el-input></el-form-item>
              <el-form-item :label="t('quality.qualifiedQty')" required><el-input v-model="roundOf[line.id].qualifiedQty" inputmode="decimal" @input="syncBad(line.id)"><template #append>{{ line.uomCode }}</template></el-input></el-form-item>
              <el-form-item :label="t('quality.unqualifiedQty')"><el-input v-model="roundOf[line.id].unqualifiedQty" readonly><template #append>{{ line.uomCode }}</template></el-input></el-form-item>
            </div>
            <div v-if="roundOf[line.id].result !== 'PASS'" class="exception-grid">
              <el-form-item :label="t('quality.issue')"><el-input v-model="roundOf[line.id].issueDescription" type="textarea" :rows="2" :placeholder="t('quality.issueHint')" /></el-form-item>
              <el-form-item :label="t('quality.suggestion')"><el-input v-model="roundOf[line.id].handlingSuggestion" type="textarea" :rows="2" :placeholder="t('quality.suggestionHint')" /></el-form-item>
            </div>
          </article>
          <div class="right"><el-button type="primary" :loading="saving" @click="submitRound">{{ t('quality.saveRound') }}</el-button></div>
        </section>

        <section class="panel files-panel">
          <div class="section-title"><div><h3>{{ t('quality.attachments') }}</h3><p>{{ t('quality.attachmentsHint') }}</p></div></div>
          <div v-if="canUpload" class="upload-row">
            <el-form-item :label="t('quality.category')"><el-select v-model="fileCategory"><el-option v-for="c in categories" :key="c" :value="c" :label="t(`quality.categories.${c}`)" /></el-select></el-form-item>
            <el-form-item class="file-form-item"><label class="file-picker"><span class="file-button">{{ t('quality.chooseFiles') }}</span><span class="file-count">{{ picked.length ? t('quality.selectedFiles', { n: picked.length }) : t('quality.noFileSelected') }}</span><input type="file" multiple @change="pickFiles"></label></el-form-item>
            <el-button type="primary" plain :loading="uploading" :disabled="!picked.length" @click="uploadFiles">{{ t('quality.upload') }}</el-button>
          </div>
          <el-table :data="task.files" size="small">
            <el-table-column prop="fileName" :label="t('quality.fileName')"><template #default="{ row }"><a :href="row.downloadUrl" target="_blank">{{ row.fileName }}</a></template></el-table-column>
            <el-table-column :label="t('quality.category')" width="160"><template #default="{ row }">{{ row.category ? t(`quality.categories.${row.category}`) : '—' }}</template></el-table-column>
            <el-table-column prop="uploadedByName" :label="t('quality.uploader')" width="150" />
            <el-table-column :label="t('quality.uploadedAt')" width="180"><template #default="{ row }">{{ fmt(row.uploadedAt) }}</template></el-table-column>
            <template #empty>{{ t('quality.noAttachments') }}</template>
          </el-table>
        </section>

        <section class="panel history-panel">
          <div class="section-title"><div><h3>{{ t('quality.history') }}</h3><p>{{ t('quality.historyHint') }}</p></div></div>
          <el-empty v-if="!task.rounds.length" :description="t('quality.noHistory')" :image-size="72" />
          <el-timeline v-else><el-timeline-item v-for="r in task.rounds" :key="r.id" :timestamp="fmt(r.inspectedAt)" placement="top"><el-card shadow="never"><b>{{ t('quality.roundNo', { n: r.roundNo }) }} · {{ r.inspectorName }}</b><p v-if="r.remark">{{ r.remark }}</p><div v-for="l in r.lines" :key="l.taskLineId" class="history-line">{{ lineName(l.taskLineId) }}：{{ resultLabel(l.result) }}，{{ t('quality.qualifiedQty') }} {{ trim(l.qualifiedQty) }}，{{ t('quality.unqualifiedQty') }} {{ trim(l.unqualifiedQty) }}<span v-if="l.issueDescription"> · {{ l.issueDescription }}</span></div></el-card></el-timeline-item></el-timeline>
        </section>
      </template>
    </el-dialog>
  </div>
</template>

<script setup lang="ts">
import { computed, reactive, ref, onMounted, watch } from 'vue'
import { ElMessage } from 'element-plus'
import { useI18n } from 'vue-i18n'
import { useRoute } from 'vue-router'
import { get, post } from '../api'
import { useAuthStore } from '../stores/auth'
import WorkflowPageHeader from '../components/WorkflowPageHeader.vue'
type QLine={id:string;productName:string;spec:string;uomCode:string;requestedQty:string;qualifiedQty:string;unresolvedQty:string;finalResult:string;approvedReleaseQty:string}
type RoundLine={taskLineId:string;result:string;qualifiedQty:string;unqualifiedQty:string;issueDescription:string}
type QTask={id:string;taskNo:string;poNo:string;supplierName:string;batchNo:number;status:string;expectedDate:string;inspectionLocation:string;contactName:string;contactPhone:string;remark:string;requestedByName:string;requestedAt:string;lines:QLine[];rounds:{id:string;roundNo:number;inspectedAt:string;inspectorName:string;remark:string;lines:RoundLine[]}[];files:any[]}
const { t }=useI18n(); const route=useRoute(); const auth=useAuthStore(); const canWrite=auth.can('quality:task:write'); const canUpload=auth.can('quality:file:upload'); const canRelease=auth.can('quality:release:decide')
const tab=ref('PENDING'), keyword=ref(''), page=ref(1), total=ref(0), loading=ref(false), saving=ref(false), uploading=ref(false), detailOpen=ref(false); const rows=ref<QTask[]>([]), task=ref<QTask|null>(null)
const round=reactive({inspectedAt:'',location:'',remark:''}); const roundOf=reactive<Record<string,any>>({}); const releaseOf=reactive<Record<string,number>>({}); const categories=['PHOTO','VIDEO','REPORT','THIRD_PARTY','OTHER']; const fileCategory=ref('PHOTO'); const picked=ref<File[]>([])
const unresolvedLines=computed(()=>task.value?.lines.filter(l=>Number(l.unresolvedQty)>0)||[])
const fmt=(v:string)=>v?new Date(v).toLocaleString():'—'; const trim=(v:string)=>String(Number(v)); const statusLabel=(s:string)=>t(`quality.statuses.${s==='COMPLETED'?'COMPLETED':'WAITING'}`); const resultLabel=(s?:string)=>t(`quality.results.${s||'PENDING'}`); const lineName=(id:string)=>task.value?.lines.find(l=>String(l.id)===String(id))?.productName||id
function prepare(){if(!task.value)return;round.inspectedAt=new Date().toISOString();round.location=task.value.inspectionLocation||'';for(const l of task.value.lines){releaseOf[l.id]=Number(l.approvedReleaseQty);if(Number(l.unresolvedQty)>0)roundOf[l.id]={result:'PASS',inspectedQty:trim(l.unresolvedQty),qualifiedQty:trim(l.unresolvedQty),unqualifiedQty:'0',issueDescription:'',handlingSuggestion:''}}}
async function load(){loading.value=true;try{const d=await get<{tasks:QTask[];meta:{total:number}}>('/quality/tasks',{tab:tab.value,page:page.value,page_size:20,keyword:keyword.value});rows.value=d.tasks||[];total.value=Number(d.meta?.total||0)}finally{loading.value=false}}
function reload(){page.value=1;void load()}
async function openTask(row:Pick<QTask,'id'>){const d=await get<{task:QTask}>(`/quality/tasks/${row.id}`);task.value=d.task;prepare();detailOpen.value=true}
async function openRequestedTask(){const id=String(route.query.task||'');if(id)await openTask({id})}
async function refresh(){if(!task.value)return;const d=await get<{task:QTask}>(`/quality/tasks/${task.value.id}`);task.value=d.task;prepare();await load()}
function applyResult(id:string){const r=roundOf[id];if(r.result==='PASS'){r.qualifiedQty=r.inspectedQty;r.unqualifiedQty='0'}else if(r.result==='FAIL'){r.qualifiedQty='0';r.unqualifiedQty=r.inspectedQty}else{if(Number(r.qualifiedQty)<=0||Number(r.qualifiedQty)>=Number(r.inspectedQty))r.qualifiedQty='';syncBad(id)}}
function syncBad(id:string){const r=roundOf[id];const inspected=Number(r.inspectedQty);const qualified=Number(r.qualifiedQty);r.unqualifiedQty=String(Math.max(0,inspected-qualified));if(qualified===0&&inspected>0)r.result='FAIL';else if(qualified>=inspected&&inspected>0)r.result='PASS';else if(qualified>0&&qualified<inspected)r.result='PARTIAL'}
async function submitRound(){if(!task.value)return;saving.value=true;try{await post(`/quality/tasks/${task.value.id}/rounds`,{inspected_at:round.inspectedAt||new Date().toISOString(),inspection_location:round.location||task.value.inspectionLocation,remark:round.remark,lines:unresolvedLines.value.map(l=>{const line=roundOf[l.id];return{task_line_id:Number(l.id),result:line.result,inspected_qty:line.inspectedQty,qualified_qty:line.qualifiedQty,unqualified_qty:line.unqualifiedQty,issue_description:line.issueDescription,handling_suggestion:line.handlingSuggestion}})});ElMessage.success(t('quality.roundSaved'));round.remark='';await refresh()}finally{saving.value=false}}
async function saveRelease(){if(!task.value)return;await post(`/quality/tasks/${task.value.id}/release`,{lines:task.value.lines.filter(l=>Number(l.qualifiedQty)>0).map(l=>({task_line_id:Number(l.id),qty:String(releaseOf[l.id]||0)}))});ElMessage.success(t('quality.releaseSaved'));await refresh()}
function pickFiles(e:Event){picked.value=Array.from((e.target as HTMLInputElement).files||[])}
async function uploadFiles(){if(!task.value||!picked.value.length)return;uploading.value=true;try{const latestRound=Number(task.value.rounds.at(-1)?.id||0);for(const file of picked.value){const p=await post<{fileKey:string;uploadUrl:string}>(`/quality/tasks/${task.value.id}/files/presign`,{file_name:file.name});const put=await fetch(p.uploadUrl,{method:'PUT',body:file,headers:{'Content-Type':file.type||'application/octet-stream'}});if(!put.ok)throw new Error(t('quality.uploadFailed'));await post(`/quality/tasks/${task.value.id}/files`,{file_key:p.fileKey,file_name:file.name,content_type:file.type,size_bytes:file.size,category:fileCategory.value,round_id:latestRound,supplemental:task.value.status==='COMPLETED'})}picked.value=[];ElMessage.success(t('quality.uploaded'));await refresh()}finally{uploading.value=false}}
onMounted(async()=>{await load();await openRequestedTask()})
watch(()=>route.query.task,()=>{void openRequestedTask()})
</script>

<style scoped>
.tabs{margin-bottom:14px}.filters{display:flex;gap:10px;margin-bottom:14px}.filters .el-input{max-width:420px}.pager{justify-content:flex-end;margin-top:14px}.sub{font-size:12px;color:#8492a6;margin-top:4px}.summary{display:grid;grid-template-columns:repeat(4,1fr);gap:14px;margin-bottom:12px}.summary>div{padding:15px;border:1px solid #cfe5ee;border-radius:10px;background:linear-gradient(120deg,#f3fbff,#f4fff9)}.summary span,.task-context span{display:block;color:#64748b;font-size:13px;margin-bottom:6px}.task-context{display:flex;flex-wrap:wrap;gap:12px 30px;padding:12px 16px;margin-bottom:16px;border-radius:8px;background:#f7fafc}.task-context>div{min-width:180px}.task-context small{display:block;margin-top:4px;color:#64748b;font-weight:400}.panel{margin-top:16px;padding:20px;border:1px solid #d7e5ec;border-radius:12px}.section-title{display:flex;justify-content:space-between;align-items:flex-start;margin-bottom:16px}.section-title h3{margin:0;color:#173a4d}.section-title p{margin:5px 0 0;color:#718096;font-size:13px}.round-head{display:grid;grid-template-columns:260px minmax(220px,1fr) minmax(260px,1.3fr);gap:14px}.line-editor{margin-top:14px;padding:18px;border:1px solid #dce8ee;border-radius:10px;background:#fbfdfe}.line-title{display:flex;justify-content:space-between;align-items:center;padding-bottom:14px;border-bottom:1px solid #e7eef2}.line-title b{font-size:16px;color:#173a4d}.line-title span{display:block;color:#64748b;font-size:12px;margin-top:5px}.line-quantity{text-align:right}.line-quantity strong{font-size:17px;color:#00856f}.result-grid{display:grid;grid-template-columns:1.15fr repeat(3,1fr);gap:14px;padding-top:16px}.exception-grid{display:grid;grid-template-columns:1fr 1fr;gap:14px}.right{text-align:right;margin-top:12px}.upload-row{display:flex;align-items:flex-end;gap:14px;margin-bottom:14px}.upload-row .el-form-item{margin-bottom:0}.upload-row .el-form-item:first-child{width:220px}.file-form-item{width:max-content;flex:none}.file-picker{display:flex;align-items:center;min-height:32px;cursor:pointer}.file-picker input{position:absolute;width:1px;height:1px;opacity:0}.file-button{padding:7px 13px;border:1px solid #d7dce3;border-radius:4px;background:#fff;color:#4b5563;white-space:nowrap}.file-count{margin-left:10px;color:#8492a6;font-size:13px;white-space:nowrap}.history-line{padding-top:8px;color:#52697a}.history-panel :deep(.el-empty){padding:20px 0}@media(max-width:900px){.summary{grid-template-columns:repeat(2,1fr)}.round-head,.result-grid,.exception-grid{grid-template-columns:1fr}.upload-row{align-items:stretch;flex-direction:column}.upload-row .el-form-item:first-child,.file-form-item{width:100%}}
</style>
