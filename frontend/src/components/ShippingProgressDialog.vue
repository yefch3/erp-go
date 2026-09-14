<template>
  <el-dialog v-model="open" title="更新船期进度" width="620px" destroy-on-close>
    <el-alert title="这里记录船舶到港、离港或正在驶向哪个港口。预计时间修改和错误更正请使用港口节点上的“编辑时间”。" type="info" :closable="false" show-icon class="progress-hint" />
    <el-form ref="formRef" :model="form" :rules="rules" label-width="110px">
      <el-form-item label="港口节点" prop="routeNodeId"><el-select v-model="form.routeNodeId" style="width:100%"><el-option v-for="n in nodes" :key="n.id" :value="n.id" :label="`${n.sequenceNo}. ${n.portName}`" /></el-select></el-form-item>
      <el-form-item label="进度动作" prop="action"><el-select v-model="form.action" style="width:100%"><el-option label="正在驶向" value="APPROACH"/><el-option label="已到港" value="ARRIVE"/><el-option label="已离港" value="DEPART"/><el-option label="跳过此港" value="SKIP"/><el-option label="更新目的港预计到港（ETA）" value="UPDATE_ETA"/></el-select></el-form-item>
      <el-form-item v-if="['ARRIVE','DEPART'].includes(form.action)" :label="form.action==='ARRIVE'?'实际到港（ATA）':'实际离港（ATD）'"><el-date-picker v-model="form.actualTime" type="datetime" value-format="YYYY-MM-DDTHH:mm:ssZ" placeholder="留空则使用当前时间" style="width:100%" /></el-form-item>
      <template v-if="form.action==='UPDATE_ETA'">
        <el-form-item label="最新预计到港（ETA）" prop="latestEta"><el-date-picker v-model="form.latestEta" type="date" value-format="YYYY-MM-DD" style="width:100%" /></el-form-item>
        <el-form-item label="影响范围"><el-radio-group v-model="form.impactType"><el-radio value="SCHEDULE">整条船期</el-radio><el-radio value="PORT">港口</el-radio><el-radio value="LEG">航段</el-radio></el-radio-group></el-form-item>
        <el-form-item v-if="form.impactType==='LEG'" label="影响航段"><div class="leg-select"><el-select v-model="form.fromNodeId" placeholder="起点"><el-option v-for="n in nodes" :key="n.id" :value="n.id" :label="n.portName" /></el-select><span>→</span><el-select v-model="form.toNodeId" placeholder="终点"><el-option v-for="n in nodes" :key="n.id" :value="n.id" :label="n.portName" /></el-select></div></el-form-item>
        <el-form-item label="延误原因"><el-select v-model="form.reasonCode" style="width:100%"><el-option label="天气" value="WEATHER"/><el-option label="港口拥堵" value="PORT_CONGESTION"/><el-option label="机械故障" value="MECHANICAL"/><el-option label="临时挂港" value="TEMPORARY_CALL"/><el-option label="其他" value="OTHER"/></el-select></el-form-item>
      </template>
      <el-form-item label="备注"><el-input v-model="form.note" type="textarea" :rows="2" /></el-form-item>
    </el-form>
    <template #footer><el-button @click="open=false">取消</el-button><el-button type="primary" :loading="saving" @click="save">更新进度</el-button></template>
  </el-dialog>
</template>

<script setup lang="ts">
import { computed, reactive, ref, watch } from 'vue'
import type { FormInstance,FormRules } from 'element-plus'
import { ElMessage } from 'element-plus'
import { post } from '../api'
import type { ShippingRouteNode } from '../shipping'
const props=defineProps<{modelValue:boolean;scheduleId:string;routeVersion:number;nodes:ShippingRouteNode[]}>();const emit=defineEmits<{ 'update:modelValue':[boolean];saved:[] }>()
const open=computed({get:()=>props.modelValue,set:v=>emit('update:modelValue',v)});const saving=ref(false);const formRef=ref<FormInstance>()
const empty=()=>({routeNodeId:'',action:'APPROACH',actualTime:'',latestEta:'',reasonCode:'OTHER',impactType:'SCHEDULE',affectedNodeId:'',fromNodeId:'',toNodeId:'',note:''});const form=reactive(empty())
const rules:FormRules={routeNodeId:[{required:true,message:'请选择港口'}],action:[{required:true}],latestEta:[{validator:(_:unknown,v:string,done:(e?:Error)=>void)=>form.action==='UPDATE_ETA'&&!v?done(new Error('请选择最新预计到港日期（ETA）')):done()}]}
watch(open,v=>{if(v)Object.assign(form,empty(),{routeNodeId:props.nodes[0]?.id??''})})
function automaticReason(){const port=props.nodes.find(node=>String(node.id)===String(form.routeNodeId))?.portName||'所选港口';return ({APPROACH:`正在驶向${port}`,ARRIVE:`${port}已到港`,DEPART:`${port}已离港`,SKIP:`跳过${port}`,UPDATE_ETA:'更新目的港预计到港时间（ETA）'} as Record<string,string>)[form.action]||'更新船期进度'}
async function save(){if(!await formRef.value?.validate().catch(()=>false))return;if(form.action==='UPDATE_ETA'&&form.impactType==='LEG'&&(!form.fromNodeId||!form.toNodeId)){ElMessage.warning('请选择受影响航段');return}saving.value=true;try{await post(`/shipping/schedules/${props.scheduleId}/progress`,{...form,reason:automaticReason(),routeVersion:props.routeVersion,affectedNodeId:form.impactType==='PORT'?form.routeNodeId:'0',fromNodeId:form.impactType==='LEG'?form.fromNodeId:'0',toNodeId:form.impactType==='LEG'?form.toNodeId:'0'});ElMessage.success('船期进度已更新');open.value=false;emit('saved')}finally{saving.value=false}}
</script>
<style scoped>.progress-hint{margin-bottom:18px}.leg-select{display:flex;align-items:center;gap:8px;width:100%}.leg-select .el-select{flex:1}</style>
