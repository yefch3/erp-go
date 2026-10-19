<template>
 <section class="workflow-panel" :class="{compact,managed:contract.entrySource!=='EXISTING_CONTRACT'}" v-loading="loading">
  <div v-if="contract.entrySource==='EXISTING_CONTRACT'" class="workflow-heading"><strong>合同操作与记录</strong><el-button link @click="load">刷新</el-button></div>
  <el-alert v-if="contract.status==='TERMINATING'" title="终止申请待上级确认，暂停新增履约。已发生业务继续处理善后。" type="warning" :closable="false"/>
  <el-alert v-if="contract.status==='TERMINATED'" title="合同已终止。关联采购、运输和账款不会自动作废，请完成善后后结案。" type="warning" :closable="false"/>
  <div v-if="contract.entrySource==='EXISTING_CONTRACT'" class="history-progress"><strong>历史接续 · {{workflow?.history.takeoverDate||'未记录接续日期'}}</strong><p>由各部门分别补录并核对历史业务。未核对完成不等于尚未采购或发运，不自动生成剩余需求。</p></div>
  <div v-if="contract.entrySource==='EXISTING_CONTRACT'" class="operation-list"><ContractOperationButton v-for="o in contractOperations" :key="o.action" :label="o.label" :description="o.description" :disabled="!canEdit||!!unavailable(o.action)" :unavailable="!canEdit?'只有负责销售可以操作':unavailable(o.action)" :type="o.action==='terminate'||o.action==='void'?'warning':undefined" @click="perform(o.action)"/></div>
  <div v-else class="operation-list integrated-actions">
   <slot name="primary"/>
   <ContractOperationButton v-for="o in (hasPrimary?[]:primaryOperations)" :key="o.action" :label="o.label" :description="o.description" type="primary" :disabled="loading || !canEdit" :unavailable="!canEdit?'只有负责销售可以操作':''" @click="perform(o.action)"/>
   <el-dropdown v-if="moreOperations.length || endOperations.length" trigger="click" placement="bottom-end" popper-class="contract-management-menu" @command="handleMore">
    <el-button class="manage-contract-trigger" :disabled="loading">管理合同 ▾</el-button>
    <template #dropdown><el-dropdown-menu>
     <li class="management-group" role="presentation">资料维护</li>
     <slot name="management"/>
     <li class="management-group" role="presentation">流程处理</li>
     <el-dropdown-item v-for="o in moreOperations" :key="o.action" :command="o.action" :disabled="!!disabledReason(o.action)"><span class="management-icon" aria-hidden="true">{{operationIcons[o.action]}}</span><div class="menu-operation"><span class="management-label">{{o.label}}</span><small v-if="disabledReason(o.action)">{{disabledReason(o.action)}}</small></div></el-dropdown-item>
     <el-dropdown-item v-if="endOperations.length" command="end"><span class="management-icon" aria-hidden="true">✓</span><div class="menu-operation"><span class="management-label">结束合同</span><small>正常结案或提前终止</small></div><span class="management-arrow" aria-hidden="true">›</span></el-dropdown-item>
     <li class="management-group danger-group" role="presentation">危险操作</li>
     <el-dropdown-item command="delete" class="management-danger" :disabled="!!disabledReason('delete')"><span class="management-icon" aria-hidden="true">×</span><div class="menu-operation"><span class="management-label">删除草稿</span><small>{{disabledReason('delete')||'删除后无法恢复'}}</small></div></el-dropdown-item>
    </el-dropdown-menu></template>
   </el-dropdown>
  </div>
  <el-dialog v-model="endOpen" title="结束合同" width="min(540px,94vw)" append-to-body>
   <p>请选择结束方式。合同及已发生的业务记录会保留。</p>
   <div v-for="o in endOperations" :key="o.action" class="end-option">
    <el-button :type="o.action==='terminate'?'warning':'primary'" :disabled="!!disabledReason(o.action)" @click="chooseEnd(o.action)">{{o.action==='complete'?'正常结案':'提前终止'}}</el-button>
    <p>{{o.description}}</p><p v-if="disabledReason(o.action)" class="unavailable-reason">{{disabledReason(o.action)}}</p>
   </div>
   <template #footer><el-button @click="endOpen=false">返回</el-button></template>
  </el-dialog>
  <el-table v-if="contract.entrySource==='EXISTING_CONTRACT' && showRecords!==false && workflow?.actions.length" :data="workflow.actions" size="small" max-height="280"><el-table-column label="操作" width="140"><template #default="{row}">{{actionLabel(row.action)}}</template></el-table-column><el-table-column prop="reason" label="原因／核对说明" min-width="200"/><el-table-column prop="actor" label="操作人" width="110"/><el-table-column prop="at" label="时间" width="200"/></el-table>
  <el-dialog v-model="closeOpen" title="合同结案核对" width="min(540px,94vw)" append-to-body><p>合同 {{contract.contractNo}} 结案后停止新增履约。请核对以下事项：</p><div v-for="d in departments" :key="d.key"><el-checkbox v-model="checks[d.key]">{{d.close}}</el-checkbox></div><el-input v-model="closeReason" type="textarea" :rows="3" placeholder="填写结案核对说明，包括差额、退款或无需处理的事项"/><template #footer><el-button @click="closeOpen=false">返回检查</el-button><el-button type="primary" :disabled="!checks.procurement||!checks.logistics||!checks.finance||!closeReason.trim()" :loading="loading" @click="closeContract">确认结案</el-button></template></el-dialog>
 </section>
</template>
<script setup lang="ts">
import {computed,ref,reactive,watch} from 'vue'
import {ElMessage,ElMessageBox} from 'element-plus'
import {get,post} from '../api'
import {contractOperations,contractActionUnavailable,type ContractAction} from '../lib/contractWorkflow'
import ContractOperationButton from './ContractOperationButton.vue'
const props=defineProps<{contract:{id:string;contractNo:string;status:string;entrySource?:string;quotationId?:string;quoteNo?:string;currentVersionId?:string};versionStatus:string;canEdit:boolean;compact?:boolean;showRecords?:boolean;hasPrimary?:boolean}>()
const emit=defineEmits<{changed:[];change:[];deleted:[];records:[rows:{label:string;reason:string;actor:string;at:string}[]]}>()
type Workflow={revision:number;history:{takeoverDate?:string};actions:{action:string;reason:string;actor:string;at:string}[]}
const workflow=ref<Workflow>(),loading=ref(false),closeOpen=ref(false),closeReason=ref(''),checks=reactive<Record<string,boolean>>({procurement:false,logistics:false,finance:false})
const departments=[{key:'procurement',label:'采购',close:'采购订单及取消、退货等事项已处理完毕'},{key:'logistics',label:'物流',close:'运输、在途货物及取消安排已处理完毕'},{key:'finance',label:'财务',close:'应收应付、收付款及退款等事项已核对处理完毕'}]
const unavailable=(a:ContractAction)=>contractActionUnavailable(a,{...props.contract,versionStatus:props.versionStatus})
const endOpen=ref(false)
const operationIcons:Record<string,string>={withdraw:'↶',void:'⊘',pause:'Ⅱ',resume:'▷',change:'✎'}
const availableOperations=computed(()=>contractOperations.filter(o=>!unavailable(o.action)&&!(o.action==='terminate'&&props.contract.status==='TERMINATING')))
const primaryOperations=computed(()=>availableOperations.value.filter(o=>['resume','change'].includes(o.action)))
const moreOperations=computed(()=>contractOperations.filter(o=>!['complete','terminate','delete'].includes(o.action)))
const endOperations=contractOperations.filter(o=>['complete','terminate'].includes(o.action)).reverse()
const disabledReason=(action:ContractAction)=>!props.canEdit?'只有负责销售可以操作':action==='terminate'&&props.contract.status==='TERMINATING'?'终止申请已提交，等待上级确认':unavailable(action)
function handleMore(command:string){if(!command)return;if(command==='end'){endOpen.value=true;return}void perform(command as ContractAction)}
function chooseEnd(action:ContractAction){endOpen.value=false;void perform(action)}
function actionLabel(action:string){return contractOperations.find(o=>o.action===action)?.label||({termination_approved:'终止已同意',termination_returned:'终止已退回',termination_rejected:'终止已退回'} as Record<string,string>)[action]||action}
async function load(){workflow.value=await get(`/contracts/${props.contract.id}/workflow`);emit('records',workflow.value!.actions.map(a=>({...a,label:actionLabel(a.action)})))}
async function send(action:string,reason:string,data?:unknown){if(loading.value)return;loading.value=true;try{workflow.value=await post(`/contracts/${props.contract.id}/workflow`,{action,reason,data,revision:workflow.value?.revision||1});ElMessage.success('合同操作已保存');if(action==='delete')emit('deleted');else emit('changed')}finally{loading.value=false;await load();emit('changed')}}
async function perform(action:ContractAction){if(loading.value||disabledReason(action))return;if(action==='change'){emit('change');return}if(action==='complete'){checks.procurement=false;checks.logistics=false;checks.finance=false;closeReason.value='';closeOpen.value=true;return}const o=contractOperations.find(o=>o.action===action)!;try{const r=await ElMessageBox.prompt(`${props.contract.contractNo}：${o.description} 请填写原因。`,o.label,{inputType:'textarea',inputValidator:v=>!!v?.trim()||'请填写原因',confirmButtonText:o.label,cancelButtonText:'返回检查',type:'warning'});await send(action,r.value.trim())}catch(e){if(e!=='cancel'&&e!=='close')throw e}}
async function closeContract(){await send('complete',closeReason.value,checks);closeOpen.value=false}
watch(()=>[props.contract.id,props.contract.status,props.versionStatus],load,{immediate:true})
</script>
<style scoped>
.workflow-panel{padding:16px;border:1px solid #e2e8f0;border-radius:8px;margin:14px 0}.workflow-heading{display:flex;align-items:center;justify-content:space-between}.operation-list{display:flex;flex-wrap:wrap;gap:12px;margin:16px 0}.end-option{padding:12px 0;border-top:1px solid #e2e8f0}.end-option p{margin:8px 0 0;color:#64748b;line-height:1.6}.history-progress{background:#f8fafc;padding:12px;margin-top:12px;font-size:13px;line-height:1.7}.history-progress .el-tag{margin-right:8px;margin-top:4px}
.workflow-panel.compact{border:0;border-bottom:1px solid #e2e8f0;border-radius:0;padding:8px 0;margin:0;display:flex;flex-wrap:wrap;align-items:center;gap:8px 16px}.compact .workflow-heading{margin-left:auto;order:2;gap:12px}.compact .workflow-heading strong{display:none}.compact .operation-list{margin:0;gap:8px;order:1}.compact .el-table{order:3;width:100%}.compact .el-alert{width:100%}
.menu-operation{max-width:340px;padding:4px 0;line-height:1.5}.menu-operation span{display:block;font-weight:500}.menu-operation small{display:block;font-size:12px;white-space:normal;color:#64748b}.unavailable-reason{color:#b45309!important}
.workflow-panel.managed{display:contents}.delete-label{color:#c2413b}.operation-list{align-items:center}.integrated-actions{margin:0;gap:8px}
</style>

<style>
.contract-management-menu{width:350px;max-width:calc(100vw - 32px);border:1px solid #d5e0eb!important;border-radius:10px!important;box-shadow:0 10px 30px #173e5426!important}
.contract-management-menu .el-dropdown-menu{padding:6px;max-height:calc(100vh - 120px);overflow-y:auto}
.contract-management-menu .management-group{list-style:none;padding:9px 10px 5px;margin-top:4px;border-top:1px solid #e6ebf1;color:#64748b;font-size:11px;font-weight:600;letter-spacing:.8px;line-height:16px}
.contract-management-menu .management-group:first-child{margin-top:0;border:0}
.contract-management-menu .el-dropdown-menu__item{position:relative;display:flex;align-items:center;gap:10px;min-height:38px;padding:7px 10px;border-radius:6px;color:#1e3348;line-height:20px;white-space:normal}
.contract-management-menu .management-icon{flex:0 0 24px;text-align:center;font-size:18px;color:#24609b;line-height:24px;background:#edf4fc;border-radius:5px}
.contract-management-menu .menu-operation,.contract-management-menu .management-item{flex:1;min-width:0;padding:0;line-height:20px}
.contract-management-menu .management-label{display:block;font-size:14px;font-weight:650;color:inherit}
.contract-management-menu small{display:block;font-size:11px!important;font-weight:400;line-height:16px;color:#677789!important;margin-top:2px;white-space:normal}
.contract-management-menu .el-dropdown-menu__item:not(.is-disabled):hover,.contract-management-menu .el-dropdown-menu__item:not(.is-disabled):focus{background:#e8f2ff;color:#174e8c;outline:2px solid #bdd7f3;outline-offset:-2px}
.contract-management-menu .el-dropdown-menu__item.is-disabled{color:#6c7887;cursor:not-allowed;padding-right:76px}
.contract-management-menu .el-dropdown-menu__item.is-disabled::after{content:'暂不可用';position:absolute;right:9px;top:9px;color:#6b7280;background:#edf0f3;border:1px solid #e0e5eb;border-radius:4px;padding:0 4px;font-size:10px;line-height:17px}
.contract-management-menu .is-disabled .management-label{font-weight:500}
.contract-management-menu .is-disabled .management-icon{color:#85909d;background:#f0f2f5}
.contract-management-menu .is-disabled small{color:#7b8794!important}
.contract-management-menu .management-danger:not(.is-disabled){color:#b42330}
.contract-management-menu .management-danger:not(.is-disabled) .management-icon{color:#b42330;background:#fff0f0}
.contract-management-menu .management-danger:not(.is-disabled):hover,.contract-management-menu .management-danger:not(.is-disabled):focus{background:#fff0f0;outline-color:#f3c5cb;color:#a61d2a}
.contract-management-menu .el-dropdown-menu__item:not(.is-disabled) .management-label{font-weight:650;color:inherit}.contract-management-menu .management-arrow{margin-left:auto;font-size:20px;color:#708499}
.manage-contract-trigger.el-button{background:#edf4fc;border-color:#9bbad9;color:#1e527f;font-weight:600}
.manage-contract-trigger.el-button:hover,.manage-contract-trigger.el-button:focus-visible{background:#dcecff;border-color:#5089bc;color:#153f68}
</style>
