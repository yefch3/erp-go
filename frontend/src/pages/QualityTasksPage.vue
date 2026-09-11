<template>
  <div class="quality-page">
    <WorkflowPageHeader :title="t('quality.title')" :description="t('quality.subtitle')" />
    <el-card shadow="never">
      <el-radio-group v-model="tab" class="tabs" @change="reload">
        <el-radio-button value="PENDING">{{ t('quality.pending') }}</el-radio-button>
        <el-radio-button value="COMPLETED">{{ t('quality.completed') }}</el-radio-button>
      </el-radio-group>
      <div class="filters"><el-input v-model="keyword" clearable :placeholder="t('quality.search')" @keyup.enter="reload" @clear="reload"/><el-button @click="reload">{{t('common.query')}}</el-button></div>
      <el-table :data="rows" v-loading="loading">
        <el-table-column prop="taskNo" :label="t('quality.taskNo')" min-width="180"><template #default="{row}"><b>{{row.taskNo}}</b><div class="sub">{{fmt(row.requestedAt)}}</div></template></el-table-column>
        <el-table-column prop="poNo" :label="t('quality.poNo')" min-width="160" />
        <el-table-column prop="supplierName" :label="t('quality.factory')" min-width="180" />
        <el-table-column :label="t('quality.batch')" width="100"><template #default="{row}">#{{row.batchNo}}</template></el-table-column>
        <el-table-column prop="expectedDate" :label="t('quality.expectedDate')" width="150" />
        <el-table-column :label="t('common.status')" width="150"><template #default="{row}"><el-tag :type="row.status==='COMPLETED'?'success':row.status==='REINSPECTION'?'warning':'primary'" effect="plain">{{statusLabel(row.status)}}</el-tag></template></el-table-column>
        <el-table-column :label="t('common.actions')" width="140" align="center"><template #default="{row}"><el-button type="primary" plain size="small" @click="openTask(row)">{{canWrite&&row.status!=='COMPLETED'?t('quality.process'):t('common.detail')}}</el-button></template></el-table-column>
        <template #empty>{{t('quality.empty')}}</template>
      </el-table>
      <el-pagination class="pager" layout="total, prev, pager, next" :total="total" :page-size="20" :current-page="page" @current-change="(p:number)=>{page=p;load()}" />
    </el-card>

    <el-dialog v-model="detailOpen" fullscreen :title="task?.taskNo || t('quality.title')" class="quality-dialog">
      <template v-if="task">
        <section class="summary">
          <div><span>{{t('quality.poNo')}}</span><b>{{task.poNo}}</b></div><div><span>{{t('quality.factory')}}</span><b>{{task.supplierName}}</b></div><div><span>{{t('quality.location')}}</span><b>{{task.inspectionLocation||'—'}}</b></div><div><span>{{t('common.status')}}</span><b>{{statusLabel(task.status)}}</b></div>
        </section>
        <el-alert type="info" :closable="false" show-icon>{{t('quality.readOnlyHint')}}</el-alert>
        <section class="panel">
          <h3>{{t('quality.products')}}</h3>
          <el-table :data="task.lines" border>
            <el-table-column prop="productName" :label="t('quality.product')" min-width="170"><template #default="{row}"><b>{{row.productName}}</b><div class="sub">{{row.spec||'—'}}</div></template></el-table-column>
            <el-table-column :label="t('quality.requestedQty')" width="140"><template #default="{row}">{{trim(row.requestedQty)}} {{row.uomCode}}</template></el-table-column>
            <el-table-column :label="t('quality.qualifiedQty')" width="140"><template #default="{row}">{{trim(row.qualifiedQty)}} {{row.uomCode}}</template></el-table-column>
            <el-table-column :label="t('quality.unresolvedQty')" width="140"><template #default="{row}">{{trim(row.unresolvedQty)}} {{row.uomCode}}</template></el-table-column>
            <el-table-column :label="t('quality.result')" width="130"><template #default="{row}">{{resultLabel(row.finalResult)}}</template></el-table-column>
            <el-table-column :label="t('quality.releaseQty')" width="160"><template #default="{row}"><el-input-number v-if="canRelease&&Number(row.qualifiedQty)>0&&task.status!=='COMPLETED'" v-model="releaseOf[row.id]" :min="0" :max="Number(row.qualifiedQty)" :precision="4" controls-position="right"/><span v-else>{{trim(row.approvedReleaseQty)}} {{row.uomCode}}</span></template></el-table-column>
          </el-table>
          <div v-if="canRelease&&task.status!=='COMPLETED'" class="right"><el-button type="primary" plain @click="saveRelease">{{t('quality.saveRelease')}}</el-button></div>
        </section>

        <section v-if="canWrite&&task.status==='WAITING'" class="panel action"><h3>{{t('quality.startTitle')}}</h3><p>{{t('quality.startHint')}}</p><el-button type="primary" @click="start">{{t('quality.start')}}</el-button></section>
        <section v-if="canWrite&&['IN_PROGRESS','REINSPECTION'].includes(task.status)" class="panel">
          <h3>{{task.status==='REINSPECTION'?t('quality.reinspect'):t('quality.recordRound')}}</h3>
          <div class="round-head"><el-date-picker v-model="round.inspectedAt" type="datetime" value-format="YYYY-MM-DDTHH:mm:ssZ" :placeholder="t('quality.inspectedAt')"/><el-input v-model="round.location" :placeholder="t('quality.location')"/><el-input v-model="round.remark" :placeholder="t('quality.remark')"/></div>
          <div v-for="line in unresolvedLines" :key="line.id" class="line-editor">
            <div class="line-title"><b>{{line.productName}}</b><span>{{line.spec||'—'}} · {{t('quality.remaining')}} {{trim(line.unresolvedQty)}} {{line.uomCode}}</span></div>
            <el-select v-model="roundOf[line.id].result"><el-option value="PASS" :label="resultLabel('PASS')"/><el-option value="PARTIAL" :label="resultLabel('PARTIAL')"/><el-option value="FAIL" :label="resultLabel('FAIL')"/></el-select>
            <el-input v-model="roundOf[line.id].inspectedQty" :placeholder="t('quality.inspectedQty')" />
            <el-input v-model="roundOf[line.id].qualifiedQty" :placeholder="t('quality.qualifiedQty')" @input="syncBad(line.id)" />
            <el-input v-model="roundOf[line.id].unqualifiedQty" :placeholder="t('quality.unqualifiedQty')" />
            <el-input v-model="roundOf[line.id].issueDescription" :placeholder="t('quality.issue')" />
            <el-input v-model="roundOf[line.id].handlingSuggestion" :placeholder="t('quality.suggestion')" />
          </div>
          <div class="right"><el-button type="primary" :loading="saving" @click="submitRound">{{t('quality.saveRound')}}</el-button></div>
        </section>

        <section class="panel">
          <h3>{{t('quality.attachments')}}</h3>
          <div v-if="canUpload" class="upload-row"><el-select v-model="fileRoundId"><el-option :value="0" :label="t('quality.taskLevel')"/><el-option v-for="r in task.rounds" :key="r.id" :value="Number(r.id)" :label="t('quality.roundNo',{n:r.roundNo})"/></el-select><el-select v-model="fileCategory"><el-option v-for="c in categories" :key="c" :value="c" :label="t(`quality.categories.${c}`)"/></el-select><input type="file" multiple @change="pickFiles"><el-button type="primary" plain :loading="uploading" @click="uploadFiles">{{t('quality.upload')}}</el-button></div>
          <el-table :data="task.files" size="small"><el-table-column prop="fileName" :label="t('quality.fileName')"><template #default="{row}"><a :href="row.downloadUrl" target="_blank">{{row.fileName}}</a></template></el-table-column><el-table-column :label="t('quality.category')" width="150"><template #default="{row}">{{t(`quality.categories.${row.category}`)}}</template></el-table-column><el-table-column prop="uploadedByName" :label="t('quality.uploader')" width="150"/><el-table-column :label="t('quality.uploadedAt')" width="180"><template #default="{row}">{{fmt(row.uploadedAt)}}</template></el-table-column></el-table>
        </section>

        <section class="panel"><h3>{{t('quality.history')}}</h3><el-timeline><el-timeline-item v-for="r in task.rounds" :key="r.id" :timestamp="fmt(r.inspectedAt)" placement="top"><el-card shadow="never"><b>{{t('quality.roundNo',{n:r.roundNo})}} · {{r.inspectorName}}</b><p v-if="r.remark">{{r.remark}}</p><div v-for="l in r.lines" :key="l.taskLineId" class="history-line">{{lineName(l.taskLineId)}}：{{resultLabel(l.result)}}，{{t('quality.qualifiedQty')}} {{trim(l.qualifiedQty)}}，{{t('quality.unqualifiedQty')}} {{trim(l.unqualifiedQty)}}<span v-if="l.issueDescription"> · {{l.issueDescription}}</span></div></el-card></el-timeline-item></el-timeline></section>
      </template>
    </el-dialog>
  </div>
</template>

<script setup lang="ts">
import { computed, reactive, ref, onMounted } from 'vue'
import { ElMessage } from 'element-plus'
import { useI18n } from 'vue-i18n'
import { get, post } from '../api'
import { useAuthStore } from '../stores/auth'
import WorkflowPageHeader from '../components/WorkflowPageHeader.vue'
type QLine={id:string;productName:string;spec:string;uomCode:string;requestedQty:string;qualifiedQty:string;unresolvedQty:string;finalResult:string;approvedReleaseQty:string}
type RoundLine={taskLineId:string;result:string;qualifiedQty:string;unqualifiedQty:string;issueDescription:string}
type QTask={id:string;taskNo:string;poNo:string;supplierName:string;batchNo:number;status:string;expectedDate:string;inspectionLocation:string;requestedAt:string;lines:QLine[];rounds:{id:string;roundNo:number;inspectedAt:string;inspectorName:string;remark:string;lines:RoundLine[]}[];files:any[]}
const {t}=useI18n();const auth=useAuthStore();const canWrite=auth.can('quality:task:write');const canUpload=auth.can('quality:file:upload');const canRelease=auth.can('quality:release:decide')
const tab=ref('PENDING'),keyword=ref(''),page=ref(1),total=ref(0),loading=ref(false),saving=ref(false),uploading=ref(false),detailOpen=ref(false);const rows=ref<QTask[]>([]),task=ref<QTask|null>(null)
const round=reactive({inspectedAt:'',location:'',remark:''});const roundOf=reactive<Record<string,any>>({});const releaseOf=reactive<Record<string,number>>({});const categories=['PHOTO','VIDEO','REPORT','THIRD_PARTY','OTHER'];const fileCategory=ref('PHOTO'),fileRoundId=ref(0);const picked=ref<File[]>([])
const unresolvedLines=computed(()=>task.value?.lines.filter(l=>Number(l.unresolvedQty)>0)||[])
const fmt=(v:string)=>v?new Date(v).toLocaleString():'—';const trim=(v:string)=>String(Number(v));const statusLabel=(s:string)=>t(`quality.statuses.${s}`);const resultLabel=(s:string)=>t(`quality.results.${s}`);const lineName=(id:string)=>task.value?.lines.find(l=>String(l.id)===String(id))?.productName||id
function prepare(){if(!task.value)return;fileRoundId.value=Number(task.value.rounds.at(-1)?.id||0);for(const l of task.value.lines){releaseOf[l.id]=Number(l.approvedReleaseQty);if(Number(l.unresolvedQty)>0)roundOf[l.id]={result:'PASS',inspectedQty:trim(l.unresolvedQty),qualifiedQty:trim(l.unresolvedQty),unqualifiedQty:'0',issueDescription:'',handlingSuggestion:''}}}
async function load(){loading.value=true;try{const d=await get<{tasks:QTask[];meta:{total:number}}>('/quality/tasks',{tab:tab.value,page:page.value,page_size:20,keyword:keyword.value});rows.value=d.tasks||[];total.value=Number(d.meta?.total||0)}finally{loading.value=false}}
function reload(){page.value=1;void load()}
async function openTask(row:QTask){const d=await get<{task:QTask}>(`/quality/tasks/${row.id}`);task.value=d.task;prepare();detailOpen.value=true}
async function refresh(){if(!task.value)return;const d=await get<{task:QTask}>(`/quality/tasks/${task.value.id}`);task.value=d.task;prepare();await load()}
async function start(){await post(`/quality/tasks/${task.value?.id}/start`);ElMessage.success(t('quality.started'));await refresh()}
function syncBad(id:string){const r=roundOf[id];r.unqualifiedQty=String(Math.max(0,Number(r.inspectedQty)-Number(r.qualifiedQty)));r.result=Number(r.qualifiedQty)===0?'FAIL':Number(r.unqualifiedQty)===0?'PASS':'PARTIAL'}
async function submitRound(){if(!task.value)return;saving.value=true;try{await post(`/quality/tasks/${task.value.id}/rounds`,{inspected_at:round.inspectedAt||new Date().toISOString(),inspection_location:round.location||task.value.inspectionLocation,remark:round.remark,lines:unresolvedLines.value.map(l=>({task_line_id:Number(l.id),...roundOf[l.id],inspected_qty:roundOf[l.id].inspectedQty,qualified_qty:roundOf[l.id].qualifiedQty,unqualified_qty:roundOf[l.id].unqualifiedQty,issue_description:roundOf[l.id].issueDescription,handling_suggestion:roundOf[l.id].handlingSuggestion}))});ElMessage.success(t('quality.roundSaved'));round.remark='';await refresh()}finally{saving.value=false}}
async function saveRelease(){if(!task.value)return;await post(`/quality/tasks/${task.value.id}/release`,{lines:task.value.lines.filter(l=>Number(l.qualifiedQty)>0).map(l=>({task_line_id:Number(l.id),qty:String(releaseOf[l.id]||0)}))});ElMessage.success(t('quality.releaseSaved'));await refresh()}
function pickFiles(e:Event){picked.value=Array.from((e.target as HTMLInputElement).files||[])}
async function uploadFiles(){if(!task.value||!picked.value.length)return;uploading.value=true;try{for(const file of picked.value){const p=await post<{fileKey:string;uploadUrl:string}>(`/quality/tasks/${task.value.id}/files/presign`,{file_name:file.name});const put=await fetch(p.uploadUrl,{method:'PUT',body:file,headers:{'Content-Type':file.type||'application/octet-stream'}});if(!put.ok)throw new Error(t('quality.uploadFailed'));await post(`/quality/tasks/${task.value.id}/files`,{file_key:p.fileKey,file_name:file.name,content_type:file.type,size_bytes:file.size,category:fileCategory.value,round_id:fileRoundId.value,supplemental:task.value.status==='COMPLETED'})}picked.value=[];ElMessage.success(t('quality.uploaded'));await refresh()}finally{uploading.value=false}}
onMounted(load)
</script>
<style scoped>
.tabs{margin-bottom:14px}.filters{display:flex;gap:10px;margin-bottom:14px}.filters .el-input{max-width:420px}.pager{justify-content:flex-end;margin-top:14px}.sub{font-size:12px;color:#8492a6;margin-top:4px}.summary{display:grid;grid-template-columns:repeat(4,1fr);gap:14px;margin-bottom:16px}.summary div{padding:15px;border:1px solid #cfe5ee;border-radius:10px;background:linear-gradient(120deg,#f3fbff,#f4fff9)}.summary span{display:block;color:#64748b;font-size:13px;margin-bottom:6px}.panel{margin-top:16px;padding:18px;border:1px solid #d7e5ec;border-radius:12px}.panel h3{margin:0 0 14px;color:#173a4d}.action{display:flex;align-items:center;gap:16px}.action h3,.action p{margin:0}.round-head{display:grid;grid-template-columns:240px 1fr 1fr;gap:12px;margin-bottom:14px}.line-editor{display:grid;grid-template-columns:minmax(210px,1.4fr) 140px repeat(3,120px) minmax(160px,1fr) minmax(160px,1fr);gap:10px;align-items:center;padding:12px 0;border-top:1px solid #edf2f5}.line-title span{display:block;color:#64748b;font-size:12px;margin-top:4px}.right{text-align:right;margin-top:12px}.upload-row{display:flex;gap:12px;align-items:center;margin-bottom:14px}.upload-row .el-select{width:180px}.history-line{padding-top:8px;color:#52697a}@media(max-width:900px){.summary{grid-template-columns:repeat(2,1fr)}.line-editor{grid-template-columns:1fr 1fr}.round-head{grid-template-columns:1fr}}
</style>
