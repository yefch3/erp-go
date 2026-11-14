<template>
  <section class="purchase-quality" v-loading="loading">
    <div class="panel-head"><h3>{{t('purchaseFulfillment.qualityResults')}}</h3><div><el-button v-if="showRefresh" :loading="loading" link @click="load">刷新</el-button><el-button v-if="tasks.length" type="primary" plain @click="detailOpen=true">{{t('purchaseFulfillment.viewReport')}}</el-button><el-button v-else-if="canRequest" type="primary" plain @click="emit('request')">申请质检</el-button></div></div>
    <el-alert v-if="failed" type="error" :closable="false" title="质检结果加载失败，请刷新重试"/>
    <template v-else>
      <div class="quality-state"><el-tag :type="passed?'success':'warning'">{{status}}</el-tag><el-tooltip content="报告上传不等于合格，交货以整单最终质检结论为准。"><span class="quality-explanation" tabindex="0" aria-label="交货须整单质检合格">ⓘ 交货条件</span></el-tooltip></div>
      <el-table v-if="lines.length" :data="lines" size="small" class="quality-summary-table">
        <el-table-column prop="productName" label="产品" min-width="230" show-overflow-tooltip/>
        <el-table-column label="送检数量" width="125" align="right"><template #default="{row}">{{trim(row.requestedQty)}} {{row.uomCode}}</template></el-table-column>
        <el-table-column label="合格数量" width="125" align="right"><template #default="{row}">{{trim(row.qualifiedQty)}} {{row.uomCode}}</template></el-table-column>
        <el-table-column label="尚未合格" width="125" align="right"><template #default="{row}">{{trim(Math.max(0,Number(row.requestedQty)-Number(row.qualifiedQty)))}} {{row.uomCode}}</template></el-table-column>
        <el-table-column label="待检 / 复检" width="135" align="right"><template #default="{row}">{{trim(row.unresolvedQty)}} {{row.uomCode}}</template></el-table-column>
      </el-table>
      <p v-if="!tasks.length" class="muted">尚未申请整单质检。</p>
    </template>
    <el-dialog v-model="detailOpen" title="质检结果与报告" width="min(1060px,94vw)" top="5vh" class="purchase-quality-dialog" append-to-body>
      <el-alert title="采购可查看报告和记录供应商处理情况；检验结论和复检结果由质检部门确认。" type="info" :closable="false"/>
      <section v-for="task in tasks" :key="task.id" class="task-section">
        <div class="panel-head"><h3>{{task.taskNo}}</h3><el-tag :type="hasPassedDeliveryInspection([task])?'success':'warning'">{{taskStatus(task)}}</el-tag></div>
        <el-table :data="task.lines" size="small">
          <el-table-column prop="productName" label="产品" min-width="220"/>
          <el-table-column label="送检" width="105" align="right"><template #default="{row}">{{trim(row.requestedQty)}} {{row.uomCode}}</template></el-table-column>
          <el-table-column label="累计合格" width="110" align="right"><template #default="{row}">{{trim(row.qualifiedQty)}} {{row.uomCode}}</template></el-table-column>
          <el-table-column label="待检 / 复检" width="120" align="right"><template #default="{row}">{{trim(row.unresolvedQty)}} {{row.uomCode}}</template></el-table-column>
          <el-table-column label="结论" width="95"><template #default="{row}">{{resultLabel(row.finalResult)}}</template></el-table-column>
          <el-table-column prop="issueDescription" label="问题说明" min-width="180"/>
        </el-table>
        <h4>历次检验</h4>
        <el-empty v-if="!task.rounds.length" description="尚无检验记录" :image-size="45"/>
        <article v-for="round in task.rounds" :key="round.id" class="round-record">
          <b>第 {{round.roundNo}} 次检验 · {{round.inspectorName}} · {{formatTime(round.inspectedAt)}}</b><p v-if="round.remark">{{round.remark}}</p>
          <div v-for="line in round.lines" :key="line.taskLineId" class="round-line">{{task.lines.find(item=>item.id===line.taskLineId)?.productName}}：{{resultLabel(line.result)}}，本次合格 {{trim(line.qualifiedQty)}}，不合格 {{trim(line.unqualifiedQty)}}<span v-if="line.issueDescription"> · {{line.issueDescription}}</span><span v-if="line.handlingSuggestion"> · 建议：{{line.handlingSuggestion}}</span></div>
        </article>
        <h4>质检报告与附件</h4>
        <el-table :data="task.files" size="small" empty-text="暂无报告或附件">
          <el-table-column prop="fileName" label="文件名" min-width="250"/>
          <el-table-column label="对应检验" width="130"><template #default="{row}">{{Number(row.roundId)?`第 ${task.rounds.find(r=>r.id===row.roundId)?.roundNo||'—'} 次检验`:'整单资料'}}</template></el-table-column>
          <el-table-column prop="uploadedByName" label="上传人" width="150"/>
          <el-table-column label="操作" width="135"><template #default="{row}"><el-button link type="primary" :loading="fileLoading===row.id" @click="readReport(task.id,row.id,false)">预览</el-button><el-button link type="primary" @click="readReport(task.id,row.id,true)">下载</el-button></template></el-table-column>
        </el-table>
        <div v-if="canHandle && task.procurementHandlingStatus==='PENDING'" class="handling">
          <el-alert title="请记录供应商处理结果；返工或换货完成后仍需质检复检合格，不能先交货。" type="warning" :closable="false"/>
          <el-select v-model="actions[task.id]" placeholder="处理方式"><el-option label="返工" value="REWORK"/><el-option label="换货" value="REPLACEMENT"/><el-option label="取消缺少数量" value="CANCEL_SHORTAGE"/></el-select><el-input v-model="notes[task.id]" placeholder="填写供应商处理结果"/><el-button type="primary" :loading="saving" @click="saveHandling(task)">保存处理结果</el-button>
        </div>
        <p v-else-if="task.procurementHandlingNote" class="muted">{{task.procurementHandlingAction ? '采购处理' : '系统处理'}}：<template v-if="task.procurementHandlingAction">{{actionLabel(task.procurementHandlingAction)}} · </template>{{task.procurementHandlingNote}}<template v-if="task.procurementHandledByName"> · {{task.procurementHandledByName}}</template></p>
      </section>
    </el-dialog>
    <FilePreviewDialog v-model="previewOpen" :source="previewBlob" :title="previewName" :content-type="previewBlob?.type"/>
  </section>
</template>
<script setup lang="ts">
import {computed,onUnmounted,ref,watch,reactive} from 'vue'
import {useI18n} from 'vue-i18n'
import {ElMessage} from 'element-plus'
import {get,post,quietErrors,saveBlob} from '../api'
import {hasPassedDeliveryInspection,type DeliveryQualityTask} from '../lib/deliveryQuality'
import {fetchViaRelay} from '../lib/storageRelay'
import {useAuthStore} from '../stores/auth'
import FilePreviewDialog from './FilePreviewDialog.vue'
const props=withDefaults(defineProps<{orderId:string;allowHandling?:boolean;canRequest?:boolean;autoRefresh?:boolean;showRefresh?:boolean}>(),{allowHandling:true,canRequest:false,autoRefresh:true,showRefresh:true})
const emit=defineEmits<{(event:'request'):void;(event:'release-change',allowed:boolean):void;(event:'state-change',state:{passed:boolean;status:string;failed:boolean;loading:boolean}):void}>(),{t}=useI18n(),auth=useAuthStore()
type Line=DeliveryQualityTask['lines'][number]&{id:string;productName:string;uomCode:string;unresolvedQty:string;issueDescription:string}
type Round={id:string;roundNo:number;inspectedAt:string;inspectorName:string;remark:string;lines:{taskLineId:string;result:string;qualifiedQty:string;unqualifiedQty:string;issueDescription:string;handlingSuggestion:string}[]}
type QualityFile={id:string;roundId:string;fileName:string;downloadUrl:string;uploadedByName:string}
type Task=Omit<DeliveryQualityTask,'lines'>&{id:string;taskNo:string;lines:Line[];rounds:Round[];files:QualityFile[];procurementHandlingAction:string;procurementHandlingNote:string;procurementHandledByName:string}
const tasks=ref<Task[]>([]),loading=ref(false),failed=ref(false),detailOpen=ref(false),saving=ref(false),fileLoading=ref('')
const actions=reactive<Record<string,string>>({}),notes=reactive<Record<string,string>>({})
const previewOpen=ref(false),previewBlob=ref<Blob|null>(null),previewName=ref('')
const canHandle=computed(()=>props.allowHandling&&auth.can('procurement:order:write'))
const passed=computed(()=>hasPassedDeliveryInspection(tasks.value)),lines=computed(()=>tasks.value.flatMap(task=>task.lines))
const status=computed(()=>!tasks.value.length?'未质检':passed.value?'整单质检合格':tasks.value.some(task=>task.status==='REINSPECTION')?'待复检':tasks.value.some(task=>task.status==='IN_PROGRESS')?'质检中':tasks.value.some(task=>task.status==='COMPLETED')?'尚未全部合格':'待质检')
const trim=(value:string|number)=>String(Number(value)||0)
const formatTime=(value:string)=>value?new Date(value.replace(' ','T').replace(/([+-]\d{2})$/,'$1:00')).toLocaleString():'—'
const resultLabel=(value:string)=>({PASS:'合格',FAIL:'不合格',PARTIAL:'部分合格',PENDING:'待检'}[value]||value||'待检')
const actionLabel=(value:string)=>({REWORK:'返工',REPLACEMENT:'换货',CANCEL_SHORTAGE:'取消缺少数量'}[value]||value)
const taskStatus=(task:Task)=>hasPassedDeliveryInspection([task])?'整单质检合格':({WAITING:'待质检',IN_PROGRESS:'质检中',REINSPECTION:'待复检',COMPLETED:'尚未全部合格'}[task.status]||task.status)
let generation=0,alive=true
async function load(){const request=++generation;loading.value=true;try{const d=await get<{tasks:Task[]}>(`/purchase-orders/${props.orderId}/quality-inspections`,{},quietErrors);if(!alive||request!==generation)return;tasks.value=d.tasks||[];failed.value=false;emit('release-change',hasPassedDeliveryInspection(tasks.value))}catch{if(alive&&request===generation){failed.value=true;emit('release-change',false)}}finally{if(alive&&request===generation)loading.value=false}}
async function readReport(taskId:string,fileId:string,download:boolean){fileLoading.value=fileId;try{const d=await get<{tasks:Task[]}>(`/purchase-orders/${props.orderId}/quality-inspections`);const file=d.tasks.find(task=>task.id===taskId)?.files.find(file=>file.id===fileId);if(!file?.downloadUrl)throw new Error('报告地址不可用');const blob=await fetchViaRelay(file.downloadUrl);if(download)saveBlob(blob,file.fileName);else{previewName.value=file.fileName;previewBlob.value=blob;previewOpen.value=true}}catch{ElMessage.error('报告读取失败，请刷新重试')}finally{fileLoading.value=''}}
async function saveHandling(task:Task){if(!canHandle.value||saving.value)return;const action=actions[task.id],note=notes[task.id]?.trim();if(!action||!note){ElMessage.warning('请选择处理方式并填写供应商处理结果');return}saving.value=true;try{await post(`/purchase-orders/${props.orderId}/quality-inspections/handling`,{task_id:Number(task.id),action,result_note:note});ElMessage.success('供应商处理结果已保存，等待质检复检');await load()}finally{saving.value=false}}
watch(()=>props.orderId,()=>{tasks.value=[];failed.value=false;detailOpen.value=false;void load()},{immediate:true})
watch([passed,status,failed,loading],()=>emit('state-change',{passed:passed.value,status:status.value,failed:failed.value,loading:loading.value}),{immediate:true})
defineExpose({refresh:load})
const timer=setInterval(()=>{if(props.autoRefresh&&!loading.value)void load()},15000)
onUnmounted(()=>{alive=false;generation++;clearInterval(timer)})
</script>
<style scoped>
.purchase-quality{border:1px solid var(--el-border-color-light);border-radius:8px;padding:14px;margin:16px 0}.panel-head{display:flex;justify-content:space-between;align-items:center;gap:12px}.panel-head h3{margin:0}.quality-state{display:flex;gap:12px;align-items:center;margin:14px 0;color:var(--el-text-color-secondary);font-size:13px}.quality-summary-line{display:flex;gap:20px;flex-wrap:wrap;padding:10px 0;border-top:1px solid var(--el-border-color-lighter);font-size:13px}.quality-summary-line strong{flex:1;min-width:180px;font-weight:500}.muted{font-size:13px;color:var(--el-text-color-secondary)}.task-section{margin:16px 0 24px}.round-record{border-left:3px solid #d9edf7;padding:10px 14px;margin-bottom:10px;font-size:13px}.round-line{margin-top:7px}.handling{display:flex;gap:10px;flex-wrap:wrap;margin-top:14px}.handling>.el-alert{width:100%}.handling>.el-select{width:150px}.handling>.el-input{flex:1;min-width:200px}:global(.purchase-quality-dialog .el-dialog__body){max-height:calc(100vh - 170px);overflow:auto}
</style>
<style scoped>
.purchase-quality{padding:14px;margin:14px 0}.panel-head h3{font-size:16px;color:var(--el-text-color-primary)}.quality-state{margin:10px 0}.quality-explanation{font-size:12px;cursor:help;color:var(--el-text-color-secondary)}.quality-summary-table :deep(.el-table__cell){padding:6px 0}.quality-summary-table :deep(.cell){line-height:22px}
</style>
