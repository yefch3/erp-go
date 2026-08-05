<template>
  <el-dialog v-model="open" :title="schedule ? t('shipping.edit') : t('shipping.create')" width="760px" destroy-on-close>
    <el-form ref="formRef" :model="form" :rules="rules" label-width="110px" v-loading="loading">
      <el-row :gutter="18">
        <el-col :span="12"><el-form-item :label="t('shipping.contractNo')"><el-input v-model="form.contractNo" /></el-form-item></el-col>
        <el-col :span="12"><el-form-item :label="t('shipping.customer')"><el-input v-model="form.customerName" /></el-form-item></el-col>
        <el-col :span="12"><el-form-item :label="t('shipping.carrier')"><el-input v-model="form.carrierForwarder" /></el-form-item></el-col>
        <el-col :span="12"><el-form-item :label="t('shipping.responsible')" prop="responsibleEmployeeId">
          <el-select v-model="form.responsibleEmployeeId" style="width:100%" filterable>
            <el-option v-for="employee in employees" :key="employee.id" :value="employee.id" :label="employee.name" />
          </el-select>
        </el-form-item></el-col>
        <el-col :span="12"><el-form-item :label="t('shipping.vessel')" prop="vesselName"><el-input v-model="form.vesselName" /></el-form-item></el-col>
        <el-col :span="12"><el-form-item :label="t('shipping.voyage')" prop="voyageNo"><el-input v-model="form.voyageNo" /></el-form-item></el-col>
        <el-col v-if="!schedule" :span="12"><el-form-item :label="t('shipping.loadingPort')" prop="portOfLoading"><el-input v-model="form.portOfLoading" /></el-form-item></el-col>
        <el-col v-if="!schedule" :span="12"><el-form-item :label="t('shipping.dischargePort')" prop="portOfDischarge"><el-input v-model="form.portOfDischarge" /></el-form-item></el-col>
        <el-col v-if="!schedule" :span="12"><el-form-item label="ETD" prop="etd"><el-date-picker v-model="form.etd" type="date" value-format="YYYY-MM-DD" style="width:100%" /></el-form-item></el-col>
        <el-col v-if="!schedule" :span="12"><el-form-item label="ATD"><el-date-picker v-model="form.atd" type="date" value-format="YYYY-MM-DD" clearable style="width:100%" /></el-form-item></el-col>
        <el-col v-if="!schedule" :span="12"><el-form-item label="ETA" prop="eta"><el-date-picker v-model="form.eta" type="date" value-format="YYYY-MM-DD" style="width:100%" /></el-form-item></el-col>
        <el-col v-if="!schedule" :span="12"><el-form-item label="ATA"><el-date-picker v-model="form.ata" type="date" value-format="YYYY-MM-DD" clearable style="width:100%" /></el-form-item></el-col>
        <el-col v-if="datesChanged" :span="24"><el-form-item :label="t('shipping.dateReason')" prop="dateChangeReason"><el-input v-model="form.dateChangeReason" type="textarea" :rows="2" /></el-form-item></el-col>
        <el-col :span="24"><el-form-item :label="t('shipping.remark')"><el-input v-model="form.remark" type="textarea" :rows="2" /></el-form-item></el-col>
      </el-row>
    </el-form>
    <template #footer>
      <el-button @click="open=false">{{ t('common.cancel') }}</el-button>
      <el-button type="primary" :loading="saving" @click="save">{{ t('common.save') }}</el-button>
    </template>
  </el-dialog>
</template>

<script setup lang="ts">
import { computed, reactive, ref, watch } from 'vue'
import type { FormInstance, FormRules } from 'element-plus'
import { ElMessage, ElMessageBox } from 'element-plus'
import { useI18n } from 'vue-i18n'
import { get, post, put, type Envelope } from '../api'
import { useAuthStore } from '../stores/auth'
import type { ShippingSchedule } from '../shipping'

const props = defineProps<{ modelValue:boolean; schedule?:ShippingSchedule }>()
const emit = defineEmits<{ 'update:modelValue':[value:boolean]; saved:[schedule:ShippingSchedule] }>()
const { t }=useI18n(); const auth=useAuthStore()
const formRef=ref<FormInstance>(); const saving=ref(false); const loading=ref(false)
const employees=ref<{id:string;name:string}[]>([])
const open=computed({get:()=>props.modelValue,set:(v)=>emit('update:modelValue',v)})
const empty=()=>({contractNo:'',customerName:'',carrierForwarder:'',vesselName:'',voyageNo:'',portOfLoading:'',portOfDischarge:'',etd:'',atd:'',eta:'',ata:'',responsibleEmployeeId:auth.employeeId,responsibleName:auth.employeeName,remark:'',dateChangeReason:''})
const form=reactive(empty())
const datesChanged=computed(()=>!!props.schedule&&(form.etd!==props.schedule.etd||form.eta!==props.schedule.eta))
const rules:FormRules={
  vesselName:[{required:true,message:t('shipping.required'),trigger:'blur'}],voyageNo:[{required:true,message:t('shipping.required'),trigger:'blur'}],
  portOfLoading:[{required:true,message:t('shipping.required'),trigger:'blur'}],portOfDischarge:[{required:true,message:t('shipping.required'),trigger:'blur'}],
  etd:[{required:true,message:t('shipping.required'),trigger:'change'}],eta:[{required:true,message:t('shipping.required'),trigger:'change'}],
  responsibleEmployeeId:[{required:true,message:t('shipping.required'),trigger:'change'}],
  dateChangeReason:[{validator:(_:unknown,value:string,done:(error?:Error)=>void)=>datesChanged.value&&!value.trim()?done(new Error(t('shipping.dateReasonRequired'))):done(),trigger:'blur'}],
}

watch(()=>props.modelValue,async visible=>{
  if(!visible)return
  Object.assign(form,empty(),props.schedule?{
    contractNo:props.schedule.contractNo,customerName:props.schedule.customerName,carrierForwarder:props.schedule.carrierForwarder,
    vesselName:props.schedule.vesselName,voyageNo:props.schedule.voyageNo,portOfLoading:props.schedule.portOfLoading,portOfDischarge:props.schedule.portOfDischarge,
    etd:props.schedule.etd,atd:props.schedule.atd,eta:props.schedule.eta,ata:props.schedule.ata,responsibleEmployeeId:props.schedule.responsibleEmployeeId,
    responsibleName:props.schedule.responsibleName,remark:props.schedule.remark,dateChangeReason:'',
  }:{})
  employees.value=[{id:form.responsibleEmployeeId,name:form.responsibleName}].filter(x=>x.id)
  if(auth.can('iam:employee:read')){
    loading.value=true
    try{const data=await get<{employees:{id:string;name:string}[]}>('/employees',{page:1,page_size:200,status:'ACTIVE'});employees.value=data.employees}catch{/* current employee remains selectable */}finally{loading.value=false}
  }
})

function body(confirmDuplicate=false){
  const employee=employees.value.find(e=>e.id===form.responsibleEmployeeId)
  const schedule={...form,responsibleName:employee?.name??form.responsibleName,dateChangeReason:undefined}
  return props.schedule
    ? {schedule,dateChangeReason:form.dateChangeReason,confirmDuplicate}
    : {schedule,confirmDuplicate}
}
async function submit(confirmDuplicate=false){
  return props.schedule?put<{schedule:ShippingSchedule}>(`/shipping/schedules/${props.schedule.id}`,body(confirmDuplicate)):post<{schedule:ShippingSchedule}>('/shipping/schedules',body(confirmDuplicate))
}
function isDialogDismissed(error:unknown){return error==='cancel'||error==='close'}
async function save(){
  if(!await formRef.value?.validate().catch(()=>false))return
  if(form.eta<form.etd){ElMessage.warning(t('shipping.etaBeforeEtd'));return}
  saving.value=true
  try{
    let result
    try{result=await submit(false)}catch(error){
      const env=error as Envelope<unknown>
      if(env?.code!=='SHIPPING_POSSIBLE_DUPLICATE')throw error
      try{await ElMessageBox.confirm(t('shipping.duplicateConfirm'),t('shipping.duplicateTitle'),{type:'warning'})}catch(action){if(isDialogDismissed(action))return;throw action}
      result=await submit(true)
    }
    ElMessage.success(props.schedule?t('shipping.updated'):t('shipping.created'))
    emit('saved',result.schedule);open.value=false
  }finally{saving.value=false}
}
</script>
