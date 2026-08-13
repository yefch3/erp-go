<template>
  <el-dialog v-model="open" :title="t('shipping.addRouteNode')" width="580px" destroy-on-close>
    <el-form ref="formRef" :model="form" :rules="rules" label-width="110px">
      <el-form-item :label="t('shipping.portType')" prop="nodeType">
        <el-radio-group v-model="form.nodeType"><el-radio-button value="TRANSIT">{{ t('shipping.transitPort') }}</el-radio-button><el-radio-button value="TEMPORARY">{{ t('shipping.temporaryPort') }}</el-radio-button></el-radio-group>
      </el-form-item>
      <el-form-item :label="t('shipping.insertPosition')" prop="insertAfterNodeId"><el-select v-model="form.insertAfterNodeId" style="width:100%"><el-option v-for="n in insertOptions" :key="n.id" :value="n.id" :label="t('shipping.afterPort',{name:n.portName})" /></el-select></el-form-item>
      <el-form-item :label="t('shipping.port')" prop="portId"><el-select v-model="form.portId" filterable style="width:100%" @change="applyPort"><el-option v-for="port in ports" :key="port.id" :value="port.id" :label="`${port.unlocode} · ${portLabel(port)}`" /></el-select></el-form-item>
      <el-form-item :label="t('shipping.estimatedArrival')"><el-date-picker v-model="form.latestEtaAt" type="datetime" value-format="YYYY-MM-DD HH:mm" style="width:100%" /></el-form-item>
      <el-form-item :label="t('shipping.estimatedDeparture')"><el-date-picker v-model="form.latestEtdAt" type="datetime" value-format="YYYY-MM-DD HH:mm" style="width:100%" /></el-form-item>
      <div v-if="form.timezone" class="timezone-hint">{{ t('shipping.portTimezone',{zone:form.timezone}) }}</div>
      <el-form-item :label="t('shipping.changeReason')" prop="reason"><el-input v-model="form.reason" type="textarea" :rows="2" /></el-form-item>
      <el-form-item :label="t('shipping.remark')"><el-input v-model="form.remark" type="textarea" :rows="2" /></el-form-item>
    </el-form>
    <template #footer><el-button @click="open=false">{{ t('common.cancel') }}</el-button><el-button type="primary" :loading="saving" @click="save">{{ t('common.save') }}</el-button></template>
  </el-dialog>
</template>

<script setup lang="ts">
import { computed, reactive, ref, watch } from 'vue'
import type { FormInstance, FormRules } from 'element-plus'
import { ElMessage } from 'element-plus'
import { useI18n } from 'vue-i18n'
import { get, post } from '../api'
import { zonedToInstant } from '../lib/zonedtime'
import type { ShippingRouteNode } from '../shipping'

const props=defineProps<{modelValue:boolean;scheduleId:string;routeVersion:number;nodes:ShippingRouteNode[]}>()
const emit=defineEmits<{ 'update:modelValue':[boolean]; saved:[] }>()
const open=computed({get:()=>props.modelValue,set:v=>emit('update:modelValue',v)})
const {t,locale}=useI18n()
const saving=ref(false);const formRef=ref<FormInstance>()
interface PortOption{id:string;unlocode:string;nameZh:string;nameEn:string;timezone:string}
const ports=ref<PortOption[]>([])
const empty=()=>({nodeType:'TRANSIT',insertAfterNodeId:'',portId:'',portName:'',portCode:'',timezone:'',latestEtaAt:'',latestEtdAt:'',reason:'',remark:''})
const form=reactive(empty())
const insertOptions=computed(()=>props.nodes.filter(n=>n.nodeType!=='DESTINATION'))
const rules:FormRules={nodeType:[{required:true}],insertAfterNodeId:[{required:true,message:t('shipping.positionRequired')}],portId:[{required:true,message:t('shipping.portRequired')}],reason:[{required:true,message:t('shipping.reasonRequired')}]}
watch(open,async v=>{if(!v)return;Object.assign(form,empty(),{insertAfterNodeId:insertOptions.value.at(-1)?.id??''});const data=await get<{ports:PortOption[]}>('/ports',{page_size:200,status:'ACTIVE'});ports.value=data.ports??[]})
function portLabel(port:PortOption){return locale.value==='zh'?(port.nameZh||port.nameEn):(port.nameEn||port.nameZh)}
function applyPort(){const port=ports.value.find(item=>item.id===form.portId);if(port)Object.assign(form,{portName:portLabel(port),portCode:port.unlocode,timezone:port.timezone})}
function instant(wall:string){if(!wall)return '';return zonedToInstant(wall,form.timezone)?.toISOString()??''}
async function save(){if(!await formRef.value?.validate().catch(()=>false))return;saving.value=true;try{await post(`/shipping/schedules/${props.scheduleId}/route/nodes`,{...form,latestEtaAt:instant(form.latestEtaAt),latestEtdAt:instant(form.latestEtdAt),routeVersion:props.routeVersion});ElMessage.success(t('shipping.routeNodeAdded'));open.value=false;emit('saved')}finally{saving.value=false}}
</script>

<style scoped>.timezone-hint{margin:-8px 0 14px 110px;color:var(--el-text-color-secondary);font-size:12px}</style>
