<template>
  <el-dialog v-model="open" :title="schedule ? t('shipping.edit') : t('shipping.create')" width="760px" destroy-on-close>
    <el-form ref="formRef" :model="form" :rules="rules" label-width="110px" v-loading="loading">
      <el-row :gutter="18">
        <el-col :span="12"><el-form-item :label="t('shipping.contractNo')"><el-input v-model="form.contractNo" /></el-form-item></el-col>
        <el-col :span="12"><el-form-item :label="t('shipping.customer')">
          <el-select v-model="form.customerId" filterable clearable style="width:100%" placeholder="请选择客户">
            <el-option v-for="customer in customers" :key="customer.id" :value="customer.id" :label="customer.name" />
            <template #empty><div class="select-empty">请先在客户管理中维护客户资料</div></template>
          </el-select>
        </el-form-item></el-col>
        <el-col :span="12"><el-form-item :label="t('shipping.carrier')">
          <el-select v-model="form.carrierId" filterable clearable style="width:100%" placeholder="请选择船公司/货代">
            <el-option v-for="carrier in carriers" :key="carrier.id" :value="carrier.id" :label="carrier.name" />
            <template #empty><div class="select-empty">暂无船公司/货代基础资料</div></template>
          </el-select>
        </el-form-item></el-col>
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
        <el-col v-if="!schedule" :span="12"><el-form-item label="ETA" prop="eta"><el-date-picker v-model="form.eta" type="date" value-format="YYYY-MM-DD" style="width:100%" /></el-form-item></el-col>
        <el-col v-if="datesChanged" :span="24"><el-form-item :label="t('shipping.dateReason')" prop="dateChangeReason"><el-input v-model="form.dateChangeReason" type="textarea" :rows="2" /></el-form-item></el-col>
        <el-col :span="24"><el-form-item :label="t('shipping.remark')"><el-input v-model="form.remark" type="textarea" :rows="2" /></el-form-item></el-col>
        <el-col :span="24"><el-form-item label="到港提醒">
          <div class="reminder-editor">
            <div class="reminder-tags">
              <el-tag v-for="day in form.reminderDays" :key="day" closable @close="removeReminderDay(day)">
                {{ day === 0 ? '到港当天' : `提前 ${day} 天` }}
              </el-tag>
              <span v-if="form.reminderDays.length === 0" class="reminder-empty">已关闭提醒</span>
            </div>
            <div class="reminder-add">
              <el-input-number v-model="newReminderDay" :min="0" :max="3650" :precision="0" controls-position="right" />
              <span>天前</span>
              <el-button @click="addReminderDay">添加提醒</el-button>
            </div>
            <div class="reminder-help">可设置多个提醒；0 表示到港当天，最多 20 个。</div>
          </div>
        </el-form-item></el-col>
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
const newReminderDay=ref(7)
const employees=ref<{id:string;name:string}[]>([])
const customers=ref<{id:string;name:string}[]>([])
const carriers=ref<{id:string;name:string}[]>([])
const open=computed({get:()=>props.modelValue,set:(v)=>emit('update:modelValue',v)})
const empty=()=>({contractNo:'',customerId:'',customerName:'',carrierId:'',carrierForwarder:'',vesselName:'',voyageNo:'',portOfLoading:'',portOfDischarge:'',etd:'',eta:'',responsibleEmployeeId:auth.employeeId,responsibleName:auth.employeeName,remark:'',dateChangeReason:'',reminderDays:[7] as number[]})
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
    contractNo:props.schedule.contractNo,customerId:props.schedule.customerId,customerName:props.schedule.customerName,
    carrierId:props.schedule.carrierId,carrierForwarder:props.schedule.carrierForwarder,
    vesselName:props.schedule.vesselName,voyageNo:props.schedule.voyageNo,portOfLoading:props.schedule.portOfLoading,portOfDischarge:props.schedule.portOfDischarge,
    etd:props.schedule.etd,eta:props.schedule.eta,responsibleEmployeeId:props.schedule.responsibleEmployeeId,
    responsibleName:props.schedule.responsibleName,remark:props.schedule.remark,dateChangeReason:'',
  }:{})
  employees.value=[{id:form.responsibleEmployeeId,name:form.responsibleName}].filter(x=>x.id)
  customers.value=form.customerName?[{id:form.customerId||'0',name:form.customerName}]:[]
  carriers.value=form.carrierForwarder?[{id:form.carrierId||'0',name:form.carrierForwarder}]:[]
  loading.value=true
  const requests:Promise<void>[]=[]
  if(props.schedule) requests.push(get<{leadDays:number[]}>(`/shipping/schedules/${props.schedule.id}/reminder-rules`).then(data=>{form.reminderDays=[...(data.leadDays??[])]}))
  if(auth.can('iam:employee:read')) requests.push(get<{employees:{id:string;name:string}[]}>('/employees',{page:1,page_size:200,status:'ACTIVE'}).then(data=>{employees.value=data.employees}))
  if(auth.can('masterdata:customer:read')) requests.push(get<{customers:{id:string;name:string}[]}>('/customers',{page_size:200}).then(data=>{customers.value=data.customers??[]}))
  if(auth.can('masterdata:supplier:read')) requests.push(get<{suppliers:{id:string;name:string}[]}>('/suppliers',{page_size:200}).then(data=>{carriers.value=data.suppliers??[]}))
  await Promise.allSettled(requests)
  loading.value=false
})

function body(confirmDuplicate=false){
  const employee=employees.value.find(e=>e.id===form.responsibleEmployeeId)
  const customer=customers.value.find(item=>item.id===form.customerId)
  const carrier=carriers.value.find(item=>item.id===form.carrierId)
  const {reminderDays:_,...scheduleFields}=form
  const schedule={...scheduleFields,customerId:Number(form.customerId)||0,customerName:customer?.name??'',carrierId:Number(form.carrierId)||0,carrierForwarder:carrier?.name??'',responsibleName:employee?.name??form.responsibleName,dateChangeReason:undefined}
  return props.schedule
    ? {schedule,dateChangeReason:form.dateChangeReason,confirmDuplicate}
    : {schedule,confirmDuplicate}
}
function addReminderDay(){
  const day=Math.trunc(Number(newReminderDay.value))
  if(!Number.isFinite(day)||day<0||day>3650){ElMessage.warning('提醒天数必须在 0 到 3650 之间');return}
  if(form.reminderDays.includes(day)){ElMessage.info('这个提醒已经添加');return}
  if(form.reminderDays.length>=20){ElMessage.warning('每条船期最多设置 20 个提醒');return}
  form.reminderDays=[...form.reminderDays,day].sort((a,b)=>b-a)
}
function removeReminderDay(day:number){form.reminderDays=form.reminderDays.filter(value=>value!==day)}
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
    await put(`/shipping/schedules/${result.schedule.id}/reminder-rules`,{leadDays:form.reminderDays})
    ElMessage.success(props.schedule?t('shipping.updated'):t('shipping.created'))
    emit('saved',result.schedule);open.value=false
  }finally{saving.value=false}
}
</script>

<style scoped>
.select-empty { padding: 10px 16px; color: var(--el-text-color-secondary); }
.reminder-editor { width: 100%; }
.reminder-tags { display: flex; flex-wrap: wrap; gap: 8px; min-height: 32px; align-items: center; }
.reminder-add { display: flex; gap: 8px; align-items: center; margin-top: 10px; }
.reminder-empty,.reminder-help { color: var(--el-text-color-secondary); font-size: 13px; }
.reminder-help { margin-top: 6px; }
</style>
