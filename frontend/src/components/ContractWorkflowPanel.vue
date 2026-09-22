<template>
 <section class="workflow-panel" v-loading="loading">
  <div class="workflow-heading"><strong>合同操作与记录</strong><el-button link @click="load">刷新</el-button></div>
  <el-alert v-if="contract.status==='TERMINATING'" title="终止申请待上级确认，暂停新增履约。已发生业务继续处理善后。" type="warning" :closable="false"/>
  <el-alert v-if="contract.status==='TERMINATED'" title="合同已终止。关联采购、运输和账款不会自动作废，请完成善后后结案。" type="warning" :closable="false"/>
  <div v-if="contract.entrySource==='EXISTING_CONTRACT'" class="history-progress"><strong>历史接续 · {{workflow?.history.takeoverDate||'未记录接续日期'}}</strong><p>由各部门分别补录并核对历史业务。未核对完成不等于尚未采购或发运，不自动生成剩余需求。</p></div>
  <div class="operation-list"><ContractOperationButton v-for="o in contractOperations" :key="o.action" :label="o.label" :description="o.description" :disabled="!canEdit||!!unavailable(o.action)" :unavailable="!canEdit?'只有负责销售可以操作':unavailable(o.action)" :type="o.action==='terminate'||o.action==='void'?'warning':undefined" @click="perform(o.action)"/></div>
  <el-table v-if="workflow?.actions.length" :data="workflow.actions" size="small" max-height="280"><el-table-column label="操作" width="140"><template #default="{row}">{{actionLabel(row.action)}}</template></el-table-column><el-table-column prop="reason" label="原因／核对说明" min-width="200"/><el-table-column prop="actor" label="操作人" width="110"/><el-table-column prop="at" label="时间" width="200"/></el-table>
  <el-dialog v-model="closeOpen" title="合同结案核对" width="min(540px,94vw)" append-to-body><p>合同 {{contract.contractNo}} 结案后停止新增履约。请核对以下事项：</p><div v-for="d in departments" :key="d.key"><el-checkbox v-model="checks[d.key]">{{d.close}}</el-checkbox></div><el-input v-model="closeReason" type="textarea" :rows="3" placeholder="填写结案核对说明，包括差额、退款或无需处理的事项"/><template #footer><el-button @click="closeOpen=false">返回检查</el-button><el-button type="primary" :disabled="!checks.procurement||!checks.logistics||!checks.finance||!closeReason.trim()" :loading="loading" @click="closeContract">确认结案</el-button></template></el-dialog>
 </section>
</template>
<script setup lang="ts">
import {ref,reactive,watch} from 'vue'
import {ElMessage,ElMessageBox} from 'element-plus'
import {get,post} from '../api'
import {contractOperations,contractActionUnavailable,type ContractAction} from '../lib/contractWorkflow'
import ContractOperationButton from './ContractOperationButton.vue'
const props=defineProps<{contract:{id:string;contractNo:string;status:string;entrySource?:string;quotationId?:string;quoteNo?:string;currentVersionId?:string};versionStatus:string;canEdit:boolean}>()
const emit=defineEmits<{changed:[];change:[];deleted:[]}>()
type Workflow={revision:number;history:{takeoverDate?:string};actions:{action:string;reason:string;actor:string;at:string}[]}
const workflow=ref<Workflow>(),loading=ref(false),closeOpen=ref(false),closeReason=ref(''),checks=reactive<Record<string,boolean>>({procurement:false,logistics:false,finance:false})
const departments=[{key:'procurement',label:'采购',close:'采购订单及取消、退货等事项已处理完毕'},{key:'logistics',label:'物流',close:'运输、在途货物及取消安排已处理完毕'},{key:'finance',label:'财务',close:'应收应付、收付款及退款等事项已核对处理完毕'}]
const unavailable=(a:ContractAction)=>contractActionUnavailable(a,{...props.contract,versionStatus:props.versionStatus})
function actionLabel(action:string){return contractOperations.find(o=>o.action===action)?.label||({termination_approved:'终止已同意',termination_returned:'终止已退回',termination_rejected:'终止已退回'} as Record<string,string>)[action]||action}
async function load(){workflow.value=await get(`/contracts/${props.contract.id}/workflow`)}
async function send(action:string,reason:string,data?:unknown){if(loading.value)return;loading.value=true;try{workflow.value=await post(`/contracts/${props.contract.id}/workflow`,{action,reason,data,revision:workflow.value?.revision||1});ElMessage.success('合同操作已保存');if(action==='delete')emit('deleted');else emit('changed')}finally{loading.value=false;await load();emit('changed')}}
async function perform(action:ContractAction){if(action==='change'){emit('change');return}if(action==='complete'){checks.procurement=false;checks.logistics=false;checks.finance=false;closeReason.value='';closeOpen.value=true;return}const o=contractOperations.find(o=>o.action===action)!;try{const r=await ElMessageBox.prompt(`${props.contract.contractNo}：${o.description} 请填写原因。`,o.label,{inputType:'textarea',inputValidator:v=>!!v?.trim()||'请填写原因',confirmButtonText:o.label,cancelButtonText:'返回检查',type:'warning'});await send(action,r.value.trim())}catch(e){if(e!=='cancel'&&e!=='close')throw e}}
async function closeContract(){await send('complete',closeReason.value,checks);closeOpen.value=false}
watch(()=>[props.contract.id,props.contract.status,props.versionStatus],load,{immediate:true})
</script>
<style scoped>
.workflow-panel{padding:16px;border:1px solid #e2e8f0;border-radius:8px;margin:14px 0}.workflow-heading{display:flex;align-items:center;justify-content:space-between}.operation-list{display:flex;flex-wrap:wrap;gap:12px;margin:16px 0}.history-progress{background:#f8fafc;padding:12px;margin-top:12px;font-size:13px;line-height:1.7}.history-progress .el-tag{margin-right:8px;margin-top:4px}
</style>
