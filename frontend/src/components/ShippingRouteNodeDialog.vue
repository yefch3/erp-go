<template>
  <el-dialog v-model="open" title="新增港口节点" width="580px" destroy-on-close>
    <el-form ref="formRef" :model="form" :rules="rules" label-width="110px">
      <el-form-item label="港口类型" prop="nodeType">
        <el-radio-group v-model="form.nodeType"><el-radio-button value="TRANSIT">中转港</el-radio-button><el-radio-button value="TEMPORARY">临时挂靠港</el-radio-button></el-radio-group>
      </el-form-item>
      <el-form-item label="插入位置" prop="insertAfterNodeId"><el-select v-model="form.insertAfterNodeId" style="width:100%"><el-option v-for="n in insertOptions" :key="n.id" :value="n.id" :label="`在 ${n.portName} 之后`" /></el-select></el-form-item>
      <el-form-item label="港口名称" prop="portName"><el-input v-model="form.portName" /></el-form-item>
      <el-form-item label="港口代码"><el-input v-model="form.portCode" placeholder="可选，例如 SGSIN" /></el-form-item>
      <el-form-item label="预计到港"><el-date-picker v-model="form.latestEtaAt" type="datetime" value-format="YYYY-MM-DDTHH:mm:ssZ" style="width:100%" /></el-form-item>
      <el-form-item label="预计离港"><el-date-picker v-model="form.latestEtdAt" type="datetime" value-format="YYYY-MM-DDTHH:mm:ssZ" style="width:100%" /></el-form-item>
      <el-form-item label="变更原因" prop="reason"><el-input v-model="form.reason" type="textarea" :rows="2" /></el-form-item>
      <el-form-item label="备注"><el-input v-model="form.remark" type="textarea" :rows="2" /></el-form-item>
    </el-form>
    <template #footer><el-button @click="open=false">取消</el-button><el-button type="primary" :loading="saving" @click="save">保存</el-button></template>
  </el-dialog>
</template>

<script setup lang="ts">
import { computed, reactive, ref, watch } from 'vue'
import type { FormInstance, FormRules } from 'element-plus'
import { ElMessage } from 'element-plus'
import { post } from '../api'
import type { ShippingRouteNode } from '../shipping'

const props=defineProps<{modelValue:boolean;scheduleId:string;routeVersion:number;nodes:ShippingRouteNode[]}>()
const emit=defineEmits<{ 'update:modelValue':[boolean]; saved:[] }>()
const open=computed({get:()=>props.modelValue,set:v=>emit('update:modelValue',v)})
const saving=ref(false);const formRef=ref<FormInstance>()
const empty=()=>({nodeType:'TRANSIT',insertAfterNodeId:'',portName:'',portCode:'',latestEtaAt:'',latestEtdAt:'',reason:'',remark:''})
const form=reactive(empty())
const insertOptions=computed(()=>props.nodes.filter(n=>n.nodeType!=='DESTINATION'))
const rules:FormRules={nodeType:[{required:true}],insertAfterNodeId:[{required:true,message:'请选择插入位置'}],portName:[{required:true,message:'请输入港口名称'}],reason:[{required:true,message:'请输入变更原因'}]}
watch(open,v=>{if(v)Object.assign(form,empty(),{insertAfterNodeId:insertOptions.value.at(-1)?.id??''})})
async function save(){if(!await formRef.value?.validate().catch(()=>false))return;saving.value=true;try{await post(`/shipping/schedules/${props.scheduleId}/route/nodes`,{...form,routeVersion:props.routeVersion,timezone:'UTC'});ElMessage.success('港口节点已添加');open.value=false;emit('saved')}finally{saving.value=false}}
</script>
