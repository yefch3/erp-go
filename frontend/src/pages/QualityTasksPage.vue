<template>
  <div class="quality-page">
    <WorkflowPageHeader :title="t('quality.title')" :description="t('quality.subtitle')">
      <template #actions><el-button v-if="canCreate" type="primary" @click="openCreate">{{ t('quality.create') }}</el-button></template>
    </WorkflowPageHeader>
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
        <el-table-column prop="expectedDate" :label="t('quality.expectedDate')" width="150" />
        <el-table-column :label="t('common.status')" width="130"><template #default="{ row }"><el-tag :type="row.status === 'COMPLETED' ? 'success' : 'warning'" effect="plain">{{ statusLabel(row.status) }}</el-tag></template></el-table-column>
        <el-table-column :label="t('common.actions')" width="160" align="center"><template #default="{ row }">
          <el-dropdown trigger="click" @command="(command:string) => handleAction(command,row)">
            <el-button plain size="small">{{ t('quality.moreActions') }} ▼</el-button>
            <template #dropdown><el-dropdown-menu>
              <el-dropdown-item command="detail">{{ canWrite && row.status !== 'COMPLETED' ? t('quality.process') : t('common.detail') }}</el-dropdown-item>
              <el-dropdown-item v-if="canWrite && row.status !== 'COMPLETED'" command="edit">{{ t('quality.editBasics') }}</el-dropdown-item>
              <el-dropdown-item v-if="canDelete && row.deletable" command="delete" divided>{{ t('quality.deleteTask') }}</el-dropdown-item>
            </el-dropdown-menu></template>
          </el-dropdown>
        </template></el-table-column>
        <template #empty>{{ t('quality.empty') }}</template>
      </el-table>
      <el-pagination class="pager" layout="total, prev, pager, next" :total="total" :page-size="20" :current-page="page" @current-change="(p:number) => { page = p; load() }" />
    </el-card>

    <el-dialog class="quality-dialog" v-model="createOpen" :title="t('quality.create')" width="min(960px, 94vw)" :close-on-click-modal="false">
      <el-form label-position="top" class="create-quality-form">
        <el-form-item :label="t('quality.poNo')" required>
          <el-input v-model="sourceKeyword" :placeholder="t('quality.sourceSearch')" clearable @keyup.enter="searchSources" @clear="searchSources"><template #append><el-button @click="searchSources">{{ t('common.query') }}</el-button></template></el-input>
        </el-form-item>
        <el-table :data="sourceOrders" v-loading="sourceLoading" highlight-current-row @row-click="selectSource" max-height="230">
          <el-table-column width="52"><template #default="{row}"><el-radio :model-value="selectedSource?.id" :value="row.id" :aria-label="row.poNo" @change="selectSource(row)"><span /></el-radio></template></el-table-column>
          <el-table-column prop="poNo" :label="t('quality.poNo')" />
          <el-table-column prop="supplierName" :label="t('quality.factory')" />
          <el-table-column width="150"><template #default="{row}"><el-button v-if="hasTask(row)" link type="primary" @click.stop="openExisting(row)">{{ t('quality.openExisting') }}</el-button></template></el-table-column>
          <template #empty>{{ t('quality.noSourceOrders') }}</template>
        </el-table>
        <el-pagination small layout="total, prev, pager, next" :total="sourceTotal" :page-size="20" v-model:current-page="sourcePage" @current-change="loadSources" />
        <div v-loading="sourceDetailLoading" class="source-detail">
          <template v-if="selectedSource">
            <p><b>{{ selectedSource.poNo }}</b> · {{ selectedSource.supplierName }}</p>
            <el-alert v-if="hasTask(selectedSource)" :title="t('quality.existingHint')" type="info" :closable="false" />
            <el-table :data="selectedSource.lines || []" max-height="230">
              <el-table-column prop="productName" :label="t('quality.product')" />
              <el-table-column prop="spec" :label="t('quality.sourceSpec')" />
              <el-table-column prop="qty" :label="t('quality.requestedQty')" width="120" />
              <el-table-column prop="uomCode" :label="t('quality.sourceUnit')" width="90" />
            </el-table>
            <div v-if="!hasTask(selectedSource)" class="create-fields">
              <el-form-item :label="t('quality.expectedDate')"><el-date-picker v-model="createForm.expectedDate" type="date" value-format="YYYY-MM-DD" /></el-form-item>
              <el-form-item :label="t('quality.location')"><el-input v-model="createForm.location" /></el-form-item>
              <el-form-item :label="t('quality.contactName')"><el-input v-model="createForm.contactName" maxlength="100" /></el-form-item>
              <el-form-item :label="t('quality.contactPhone')"><el-input v-model="createForm.contactPhone" maxlength="64" /></el-form-item>
              <el-form-item class="full-width" :label="t('quality.requirements')"><el-input v-model="createForm.remark" type="textarea" :rows="3" /></el-form-item>
            </div>
          </template>
        </div>
      </el-form>
      <template #footer>
        <el-button @click="createOpen=false">{{ t('common.cancel') }}</el-button>
        <el-button v-if="selectedSource && hasTask(selectedSource)" type="primary" @click="openExisting(selectedSource)">{{ t('quality.openExisting') }}</el-button>
        <el-button v-else type="primary" :loading="creating" :disabled="!selectedSource || sourceDetailLoading || !selectedSource.lines?.length" @click="createTask">{{ t('quality.create') }}</el-button>
      </template>
    </el-dialog>

    <el-dialog class="quality-dialog" v-model="editOpen" :title="t('quality.editBasics')" width="min(760px, 94vw)" :close-on-click-modal="false" :close-on-press-escape="!editing" :show-close="!editing">
      <el-form label-position="top" class="create-fields">
        <el-form-item :label="t('quality.poNo')"><el-input :model-value="editForm.poNo" readonly /></el-form-item>
        <el-form-item :label="t('quality.factory')"><el-input :model-value="editForm.supplierName" readonly /></el-form-item>
        <el-form-item :label="t('quality.expectedDate')"><el-date-picker v-model="editForm.expectedDate" type="date" value-format="YYYY-MM-DD" /></el-form-item>
        <el-form-item :label="t('quality.location')"><el-input v-model="editForm.location" /></el-form-item>
        <el-form-item :label="t('quality.contactName')"><el-input v-model="editForm.contactName" maxlength="100" /></el-form-item>
        <el-form-item :label="t('quality.contactPhone')"><el-input v-model="editForm.contactPhone" maxlength="64" /></el-form-item>
        <el-form-item class="full-width" :label="t('quality.requirements')"><el-input v-model="editForm.remark" type="textarea" :rows="3" /></el-form-item>
      </el-form>
      <template #footer><el-button :disabled="editing" @click="editOpen=false">{{ t('common.cancel') }}</el-button><el-button type="primary" :loading="editing" @click="saveBasics">{{ t('common.save') }}</el-button></template>
    </el-dialog>

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
          </el-table>
        </section>

        <section v-if="canWrite && task.status !== 'COMPLETED'" class="panel record-panel">
          <div class="section-title"><div><h3>{{ task.rounds.length ? t('quality.reinspect') : t('quality.recordRound') }}</h3><p>{{ t('quality.recordHint') }}</p></div><el-tag v-if="task.rounds.length" type="warning" effect="plain">{{ t('quality.roundNo', { n: task.rounds.length + 1 }) }}</el-tag></div>
          <div class="round-head">
            <el-form-item :label="t('quality.inspectedAt')"><el-date-picker v-model="round.inspectedAt" type="datetime" value-format="YYYY-MM-DDTHH:mm:ssZ" /></el-form-item>
            <el-form-item :label="t('quality.location')"><el-input v-model="round.location" /></el-form-item>
            <el-form-item :label="t('quality.roundRemark')"><el-input v-model="round.remark" :placeholder="t('quality.roundRemarkHint')" /></el-form-item>
          </div>
          <el-table :data="unresolvedLines" border size="small" class="inspection-entry-table" row-key="id">
            <el-table-column :label="t('quality.product')" min-width="220">
              <template #default="{row}"><b>{{ row.productName }}</b><div class="sub">{{ row.spec || '—' }}</div></template>
            </el-table-column>
            <el-table-column :label="t('quality.thisBatchQty')" width="100" align="right"><template #default="{row}">{{ trim(row.unresolvedQty) }}</template></el-table-column>
            <el-table-column prop="uomCode" :label="t('quality.sourceUnit')" width="65" />
            <el-table-column :label="t('quality.result')" width="145"><template #default="{row}">
              <el-select v-model="roundOf[row.id].result" size="small" :aria-label="`${row.productName} ${t('quality.result')}`" @change="applyResult(row.id)">
                <el-option value="PASS" :label="resultLabel('PASS')" /><el-option value="PARTIAL" :label="resultLabel('PARTIAL')" /><el-option value="FAIL" :label="resultLabel('FAIL')" />
              </el-select>
            </template></el-table-column>
            <el-table-column :label="t('quality.inspectedQty')" width="140"><template #default="{row}"><el-input v-model="roundOf[row.id].inspectedQty" size="small" inputmode="decimal" :aria-label="`${row.productName} ${t('quality.inspectedQty')}`" @input="syncBad(row.id)" /></template></el-table-column>
            <el-table-column :label="t('quality.qualifiedQty')" width="135"><template #default="{row}"><el-input v-model="roundOf[row.id].qualifiedQty" size="small" inputmode="decimal" :aria-label="`${row.productName} ${t('quality.qualifiedQty')}`" @input="syncBad(row.id)" /></template></el-table-column>
            <el-table-column :label="t('quality.unqualifiedQty')" width="155" align="right"><template #default="{row}">{{ roundOf[row.id].unqualifiedQty }}</template></el-table-column>
            <el-table-column :label="t('quality.issue')" width="115"><template #default="{row}">
              <el-popover v-if="roundOf[row.id].result !== 'PASS'" trigger="click" :width="360" placement="left" popper-class="quality-issue-popover">
                <el-form label-position="top" size="small">
                  <el-form-item :label="t('quality.issue')"><el-input v-model="roundOf[row.id].issueDescription" type="textarea" :rows="3" :placeholder="t('quality.issueHint')" /></el-form-item>
                  <el-form-item :label="t('quality.suggestion')"><el-input v-model="roundOf[row.id].handlingSuggestion" type="textarea" :rows="2" :placeholder="t('quality.suggestionHint')" /></el-form-item>
                </el-form>
                <template #reference><el-button link type="primary" size="small">{{ t('quality.issue') }}{{ roundOf[row.id].issueDescription || roundOf[row.id].handlingSuggestion ? ' •' : '' }}</el-button></template>
              </el-popover><span v-else class="sub">—</span>
            </template></el-table-column>
          </el-table>
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
import { ElMessage, ElMessageBox } from 'element-plus'
import { useI18n } from 'vue-i18n'
import { useRoute } from 'vue-router'
import { get, post, patch, del } from '../api'
import { useAuthStore } from '../stores/auth'
import WorkflowPageHeader from '../components/WorkflowPageHeader.vue'
type QLine={id:string;productName:string;spec:string;uomCode:string;requestedQty:string;qualifiedQty:string;unresolvedQty:string;finalResult:string;approvedReleaseQty:string}
type RoundLine={taskLineId:string;result:string;qualifiedQty:string;unqualifiedQty:string;issueDescription:string}
type QTask={id:string;deletable:boolean;taskNo:string;poNo:string;supplierName:string;batchNo:number;status:string;expectedDate:string;inspectionLocation:string;contactName:string;contactPhone:string;remark:string;requestedByName:string;requestedAt:string;lines:QLine[];rounds:{id:string;roundNo:number;inspectedAt:string;inspectorName:string;remark:string;lines:RoundLine[]}[];files:any[]}
const { t }=useI18n(); const route=useRoute(); const auth=useAuthStore(); const canWrite=auth.can('quality:task:write'); const canUpload=auth.can('quality:file:upload')
type SourceOrder={id:string;poNo:string;supplierName:string;existingTaskId:string;lines?:{productName:string;spec:string;uomCode:string;qty:string}[]}
const canCreate=auth.can('quality:task:read') && auth.can('quality:task:write')
const createOpen=ref(false), creating=ref(false), sourceLoading=ref(false), sourceDetailLoading=ref(false)
const sourceKeyword=ref(''), sourcePage=ref(1), sourceTotal=ref(0), sourceOrders=ref<SourceOrder[]>([]), selectedSource=ref<SourceOrder|null>(null)
const createForm=reactive({expectedDate:'',location:'',contactName:'',contactPhone:'',remark:''})
let sourceRequest=0, detailRequest=0
const hasTask=(o:SourceOrder)=>!!o.existingTaskId && o.existingTaskId!=='0'
async function openCreate(){createOpen.value=true;selectedSource.value=null;sourceDetailLoading.value=false;++detailRequest;sourceKeyword.value='';sourcePage.value=1;Object.assign(createForm,{expectedDate:'',location:'',contactName:'',contactPhone:'',remark:''});await loadSources()}
async function searchSources(){sourcePage.value=1;await loadSources()}
async function loadSources(){const request=++sourceRequest;sourceLoading.value=true;try{const d=await get<{orders:SourceOrder[];meta:{total:number}}>('/quality/source-orders',{keyword:sourceKeyword.value,page:sourcePage.value,page_size:20});if(request!==sourceRequest)return;sourceOrders.value=d.orders||[];sourceTotal.value=Number(d.meta?.total||0)}finally{if(request===sourceRequest)sourceLoading.value=false}}
async function selectSource(row:SourceOrder){const request=++detailRequest;Object.assign(createForm,{expectedDate:'',location:'',contactName:'',contactPhone:'',remark:''});selectedSource.value=null;sourceDetailLoading.value=true;try{const d=await get<{orders:SourceOrder[]}>(`/quality/source-orders/${row.id}`);if(request===detailRequest)selectedSource.value=d.orders[0]||null}finally{if(request===detailRequest)sourceDetailLoading.value=false}}
async function openExisting(row:SourceOrder){await openTask({id:row.existingTaskId});createOpen.value=false}
async function createTask(){if(!selectedSource.value||creating.value)return;creating.value=true;try{const d=await post<{task:QTask}>('/quality/tasks',{po_id:Number(selectedSource.value.id),expected_date:createForm.expectedDate||'',inspection_location:createForm.location,contact_name:createForm.contactName,contact_phone:createForm.contactPhone,remark:createForm.remark});createOpen.value=false;task.value=d.task;prepare();detailOpen.value=true;tab.value='PENDING';page.value=1;await load();ElMessage.success(t('quality.created'))}finally{creating.value=false}}
const canDelete=ref(false), editOpen=ref(false), editing=ref(false)
const editForm=reactive({id:'',poNo:'',supplierName:'',expectedDate:'',location:'',contactName:'',contactPhone:'',remark:''})
async function handleAction(command:string,row:QTask){
  if(command==='detail'){await openTask(row);return}
  if(command==='edit'){
    const {task:q}=await get<{task:QTask}>(`/quality/tasks/${row.id}`)
    if(q.status==='COMPLETED'){await load();return}
    Object.assign(editForm,{id:q.id,poNo:q.poNo,supplierName:q.supplierName,expectedDate:q.expectedDate||'',location:q.inspectionLocation||'',contactName:q.contactName||'',contactPhone:q.contactPhone||'',remark:q.remark||''})
    editOpen.value=true;return
  }
  if(command==='delete'){
    try{await ElMessageBox.confirm(t('quality.deleteConfirm'),t('quality.deleteTask'),{type:'warning',confirmButtonText:t('quality.deleteTask'),cancelButtonText:t('common.cancel')})}catch{return}
    await del(`/quality/tasks/${row.id}`)
    if(task.value?.id===row.id){detailOpen.value=false;task.value=null}
    if(rows.value.length===1&&page.value>1)page.value--
    await load();ElMessage.success(t('quality.deleted'))
  }
}
async function saveBasics(){
  if(editing.value)return;editing.value=true
  try{
    const {task:q}=await patch<{task:QTask}>(`/quality/tasks/${editForm.id}/basics`,{expected_date:editForm.expectedDate||'',inspection_location:editForm.location,contact_name:editForm.contactName,contact_phone:editForm.contactPhone,remark:editForm.remark})
    if(task.value?.id===q.id){task.value=q;prepare()}
    editOpen.value=false;await load();ElMessage.success(t('quality.basicsSaved'))
  }finally{editing.value=false}
}
const tab=ref('PENDING'), keyword=ref(''), page=ref(1), total=ref(0), loading=ref(false), saving=ref(false), uploading=ref(false), detailOpen=ref(false); const rows=ref<QTask[]>([]), task=ref<QTask|null>(null)
const round=reactive({inspectedAt:'',location:'',remark:''}); const roundOf=reactive<Record<string,any>>({}); const categories=['PHOTO','VIDEO','REPORT','THIRD_PARTY','OTHER']; const fileCategory=ref('PHOTO'); const picked=ref<File[]>([])
const unresolvedLines=computed(()=>task.value?.lines.filter(l=>Number(l.unresolvedQty)>0)||[])
const fmt=(v:string)=>v?new Date(v).toLocaleString():'—'; const trim=(v:string)=>String(Number(v)); const statusLabel=(s:string)=>t(`quality.statuses.${s==='COMPLETED'?'COMPLETED':'WAITING'}`); const resultLabel=(s?:string)=>t(`quality.results.${s||'PENDING'}`); const lineName=(id:string)=>task.value?.lines.find(l=>String(l.id)===String(id))?.productName||id
function prepare(){if(!task.value)return;round.inspectedAt=new Date().toISOString();round.location=task.value.inspectionLocation||'';for(const l of task.value.lines){if(Number(l.unresolvedQty)>0)roundOf[l.id]={result:'PASS',inspectedQty:trim(l.unresolvedQty),qualifiedQty:trim(l.unresolvedQty),unqualifiedQty:'0',issueDescription:'',handlingSuggestion:''}}}
async function load(){loading.value=true;try{const d=await get<{tasks:QTask[];meta:{total:number}}>('/quality/tasks',{tab:tab.value,page:page.value,page_size:20,keyword:keyword.value});rows.value=d.tasks||[];total.value=Number(d.meta?.total||0)}finally{loading.value=false}}
function reload(){page.value=1;void load()}
async function openTask(row:Pick<QTask,'id'>){const d=await get<{task:QTask}>(`/quality/tasks/${row.id}`);task.value=d.task;prepare();detailOpen.value=true}
async function openRequestedTask(){const id=String(route.query.task||'');if(id)await openTask({id})}
async function refresh(){if(!task.value)return;const d=await get<{task:QTask}>(`/quality/tasks/${task.value.id}`);task.value=d.task;prepare();await load()}
function applyResult(id:string){const r=roundOf[id];if(r.result==='PASS'){r.qualifiedQty=r.inspectedQty;r.unqualifiedQty='0'}else if(r.result==='FAIL'){r.qualifiedQty='0';r.unqualifiedQty=r.inspectedQty}else{if(Number(r.qualifiedQty)<=0||Number(r.qualifiedQty)>=Number(r.inspectedQty))r.qualifiedQty='';syncBad(id)}}
function syncBad(id:string){const r=roundOf[id];const inspected=Number(r.inspectedQty);const qualified=Number(r.qualifiedQty);r.unqualifiedQty=String(Math.max(0,inspected-qualified));if(qualified===0&&inspected>0)r.result='FAIL';else if(qualified>=inspected&&inspected>0)r.result='PASS';else if(qualified>0&&qualified<inspected)r.result='PARTIAL'}
async function submitRound(){if(!task.value)return;saving.value=true;try{await post(`/quality/tasks/${task.value.id}/rounds`,{inspected_at:round.inspectedAt||new Date().toISOString(),inspection_location:round.location||task.value.inspectionLocation,remark:round.remark,lines:unresolvedLines.value.map(l=>{const line=roundOf[l.id];return{task_line_id:Number(l.id),result:line.result,inspected_qty:line.inspectedQty,qualified_qty:line.qualifiedQty,unqualified_qty:line.unqualifiedQty,issue_description:line.issueDescription,handling_suggestion:line.handlingSuggestion}})});ElMessage.success(t('quality.roundSaved'));round.remark='';await refresh()}finally{saving.value=false}}
function pickFiles(e:Event){picked.value=Array.from((e.target as HTMLInputElement).files||[])}
async function uploadFiles(){if(!task.value||!picked.value.length)return;uploading.value=true;try{const latestRound=Number(task.value.rounds.at(-1)?.id||0);for(const file of picked.value){const p=await post<{fileKey:string;uploadUrl:string}>(`/quality/tasks/${task.value.id}/files/presign`,{file_name:file.name});const put=await fetch(p.uploadUrl,{method:'PUT',body:file,headers:{'Content-Type':file.type||'application/octet-stream'}});if(!put.ok)throw new Error(t('quality.uploadFailed'));await post(`/quality/tasks/${task.value.id}/files`,{file_key:p.fileKey,file_name:file.name,content_type:file.type,size_bytes:file.size,category:fileCategory.value,round_id:latestRound,supplemental:task.value.status==='COMPLETED'})}picked.value=[];ElMessage.success(t('quality.uploaded'));await refresh()}finally{uploading.value=false}}
onMounted(async()=>{await load();await openRequestedTask();const access=await get<{canDelete:boolean}>('/quality/access');canDelete.value=access.canDelete})
watch(()=>route.query.task,()=>{void openRequestedTask()})
</script>

<style scoped>
.create-quality-form { max-height: 68vh; overflow: auto; padding-right: 8px; }
.create-fields { display: grid; grid-template-columns: 1fr 1fr; gap: 0 20px; margin-top: 18px; }
.create-fields .full-width { grid-column: 1 / -1; }
.source-detail { min-height: 30px; }
@media (max-width: 640px) { .create-fields { grid-template-columns: 1fr; } }

.tabs{margin-bottom:14px}.filters{display:flex;gap:10px;margin-bottom:14px}.filters .el-input{max-width:420px}.pager{justify-content:flex-end;margin-top:14px}.sub{font-size:12px;color:#8492a6;margin-top:4px}.summary{display:grid;grid-template-columns:repeat(4,1fr);gap:14px;margin-bottom:12px}.summary>div{padding:15px;border:1px solid #cfe5ee;border-radius:10px;background:linear-gradient(120deg,#f3fbff,#f4fff9)}.summary span,.task-context span{display:block;color:#64748b;font-size:13px;margin-bottom:6px}.task-context{display:flex;flex-wrap:wrap;gap:12px 30px;padding:12px 16px;margin-bottom:16px;border-radius:8px;background:#f7fafc}.task-context>div{min-width:180px}.task-context small{display:block;margin-top:4px;color:#64748b;font-weight:400}.panel{margin-top:16px;padding:20px;border:1px solid #d7e5ec;border-radius:12px}.section-title{display:flex;justify-content:space-between;align-items:flex-start;margin-bottom:16px}.section-title h3{margin:0;color:#173a4d}.section-title p{margin:5px 0 0;color:#718096;font-size:13px}.round-head{display:grid;grid-template-columns:260px minmax(220px,1fr) minmax(260px,1.3fr);gap:14px}.right{text-align:right;margin-top:12px}.upload-row{display:flex;align-items:flex-end;gap:14px;margin-bottom:14px}.upload-row .el-form-item{margin-bottom:0}.upload-row .el-form-item:first-child{width:220px}.file-form-item{width:max-content;flex:none}.file-picker{display:flex;align-items:center;min-height:32px;cursor:pointer}.file-picker input{position:absolute;width:1px;height:1px;opacity:0}.file-button{padding:7px 13px;border:1px solid #d7dce3;border-radius:4px;background:#fff;color:#4b5563;white-space:nowrap}.file-count{margin-left:10px;color:#8492a6;font-size:13px;white-space:nowrap}.history-line{padding-top:8px;color:#52697a}.history-panel :deep(.el-empty){padding:20px 0}@media(max-width:900px){.summary{grid-template-columns:repeat(2,1fr)}.round-head{grid-template-columns:1fr}.upload-row{align-items:stretch;flex-direction:column}.upload-row .el-form-item:first-child,.file-form-item{width:100%}}

/* Compact typography is limited to the Quality page and its teleported dialogs. */
.quality-page, .quality-dialog { font-size: 13px; --el-font-size-base: 13px; --el-font-size-small: 12px; }
.quality-page :deep(.el-table), .quality-dialog :deep(.el-table),
.quality-dialog :deep(.el-form-item__label), .quality-dialog :deep(.el-input__inner),
.quality-dialog :deep(.el-textarea__inner), .quality-dialog :deep(.el-select__wrapper),
.quality-page :deep(.el-button), .quality-dialog :deep(.el-button) { font-size: 13px; }
.quality-page :deep(.procurement-page-header h1) { font-size: 20px; }
.quality-dialog :deep(.el-dialog__title) { font-size: 16px; }
.quality-dialog :deep(.el-table .cell) { line-height: 19px; padding: 0 8px; }
.quality-dialog :deep(.el-table__cell) { padding: 6px 0; }
.quality-dialog .sub { margin-top: 2px; font-size: 11px; }
.quality-dialog .panel { padding: 12px 0; margin-top: 12px; border: 0; border-top: 1px solid #e5ebf0; border-radius: 0; }
.quality-dialog .section-title { margin-bottom: 10px; }
.quality-dialog .section-title h3 { font-size: 14px; }
.quality-dialog .section-title p { font-size: 12px; margin-top: 3px; }
.quality-dialog .summary > div { padding: 10px; }
.quality-dialog .summary span, .quality-dialog .task-context span { font-size: 12px; margin-bottom: 3px; }
.quality-dialog .round-head { gap: 10px; }
.quality-dialog .round-head :deep(.el-form-item) { margin-bottom: 10px; }
.inspection-entry-table :deep(.el-input__inner) { text-align: right; }
.inspection-entry-table :deep(.el-input__wrapper), .inspection-entry-table :deep(.el-select__wrapper) { min-height: 26px; }
</style>

<style>
.quality-issue-popover { max-width: calc(100vw - 32px); font-size: 13px; }
.quality-issue-popover .el-form-item__label, .quality-issue-popover .el-textarea__inner { font-size: 13px; }
.quality-issue-popover .el-form-item:last-child { margin-bottom: 0; }
</style>
