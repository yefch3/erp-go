<template>
  <el-dialog v-model="open" :title="schedule ? t('shipping.edit') : t('shipping.create')" width="760px" destroy-on-close>
    <el-form ref="formRef" :model="form" :rules="rules" label-width="110px" v-loading="loading">
      <el-row :gutter="18">
        <el-col :span="12"><el-form-item :label="t('shipping.contractNo')"><el-input v-model="form.contractNo" /></el-form-item></el-col>
        <el-col :span="12"><el-form-item :label="t('shipping.customer')">
          <CustomerSelect v-model="form.customerId" @selected="customer=>form.customerName=customer?.name||''" />
        </el-form-item></el-col>
        <el-col :span="12"><el-form-item :label="t('shipping.carrier')">
          <el-select v-model="form.carrierId" filterable clearable style="width:100%" placeholder="请选择船公司/货代">
            <el-option v-for="carrier in carriers" :key="carrier.id" :value="carrier.id" :label="carrier.name" />
            <template #empty><div class="select-empty">暂无船公司/货代基础资料</div></template>
          </el-select>
        </el-form-item></el-col>
        <el-col :span="12"><el-form-item :label="t('shipping.responsible')" prop="responsibleEmployeeId">
          <EmployeeSelect v-model="form.responsibleEmployeeId" data-scope-module="shipping" @selected="employee=>form.responsibleName=employee?.name||''" />
        </el-form-item></el-col>
        <el-col :span="12"><el-form-item :label="t('shipping.vessel')" prop="vesselName"><el-input v-model="form.vesselName" /></el-form-item></el-col>
        <el-col :span="12"><el-form-item :label="t('shipping.voyage')" prop="voyageNo"><el-input v-model="form.voyageNo" /></el-form-item></el-col>
        <el-col v-if="!schedule" :span="12"><el-form-item :label="t('shipping.loadingPort')" prop="loadingPortId"><PortSelect v-model="form.loadingPortId" @selected="port=>applyPort('loading',port)" /></el-form-item></el-col>
        <el-col v-if="!schedule" :span="12"><el-form-item :label="t('shipping.dischargePort')" prop="dischargePortId"><PortSelect v-model="form.dischargePortId" @selected="port=>applyPort('discharge',port)" /></el-form-item></el-col>
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
import CustomerSelect from './masterdata/CustomerSelect.vue'
import EmployeeSelect from './masterdata/EmployeeSelect.vue'
import PortSelect, { type PortOption } from './masterdata/PortSelect.vue'

const props = defineProps<{ modelValue:boolean; schedule?:ShippingSchedule }>()
const emit = defineEmits<{ 'update:modelValue':[value:boolean]; saved:[schedule:ShippingSchedule] }>()
const { t, locale }=useI18n(); const auth=useAuthStore()
const formRef=ref<FormInstance>(); const saving=ref(false); const loading=ref(false)
const newReminderDay=ref(7)
const carriers=ref<{id:string;name:string}[]>([])
const open=computed({get:()=>props.modelValue,set:(v)=>emit('update:modelValue',v)})
const empty=()=>({contractNo:'',customerId:'',customerName:'',carrierId:'',carrierForwarder:'',vesselName:'',voyageNo:'',portOfLoading:'',portOfDischarge:'',loadingPortId:'',loadingPortCode:'',loadingPortTimezone:'',dischargePortId:'',dischargePortCode:'',dischargePortTimezone:'',etd:'',eta:'',responsibleEmployeeId:auth.employeeId,responsibleName:auth.employeeName,remark:'',dateChangeReason:'',reminderDays:[7] as number[]})
const form=reactive(empty())
const datesChanged=computed(()=>!!props.schedule&&(form.etd!==props.schedule.etd||form.eta!==props.schedule.eta))
const rules:FormRules={
  vesselName:[{required:true,message:t('shipping.required'),trigger:'blur'}],voyageNo:[{required:true,message:t('shipping.required'),trigger:'blur'}],
  loadingPortId:[{required:true,message:t('shipping.required'),trigger:'change'}],dischargePortId:[{required:true,message:t('shipping.required'),trigger:'change'}],
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
    loadingPortId:props.schedule.loadingPortId,loadingPortCode:props.schedule.loadingPortCode,loadingPortTimezone:props.schedule.loadingPortTimezone,
    dischargePortId:props.schedule.dischargePortId,dischargePortCode:props.schedule.dischargePortCode,dischargePortTimezone:props.schedule.dischargePortTimezone,
    etd:props.schedule.etd,eta:props.schedule.eta,responsibleEmployeeId:props.schedule.responsibleEmployeeId,
    responsibleName:props.schedule.responsibleName,remark:props.schedule.remark,dateChangeReason:'',
  }:{})
  carriers.value=form.carrierForwarder?[{id:form.carrierId||'0',name:form.carrierForwarder}]:[]
  loading.value=true
  const requests:Promise<void>[]=[]
  if(props.schedule) requests.push(get<{leadDays:number[]}>(`/shipping/schedules/${props.schedule.id}/reminder-rules`).then(data=>{form.reminderDays=[...(data.leadDays??[])]}))
  if(auth.can('masterdata:supplier:read')) requests.push(get<{suppliers:{id:string;name:string}[]}>('/suppliers',{page_size:200}).then(data=>{carriers.value=data.suppliers??[]}))
  await Promise.allSettled(requests)
  loading.value=false
})

function portLabel(port:PortOption){return locale.value==='zh'?(port.nameZh||port.nameEn):(port.nameEn||port.nameZh)}
function applyPort(kind:'loading'|'discharge',port:PortOption|undefined){
  if(!port)return
  if(kind==='loading')Object.assign(form,{portOfLoading:portLabel(port),loadingPortCode:port.unlocode,loadingPortTimezone:port.timezone})
  else Object.assign(form,{portOfDischarge:portLabel(port),dischargePortCode:port.unlocode,dischargePortTimezone:port.timezone})
}

function body(confirmDuplicate=false){
  const carrier=carriers.value.find(item=>item.id===form.carrierId)
  const {reminderDays:_,...scheduleFields}=form
  const schedule={...scheduleFields,customerId:Number(form.customerId)||0,carrierId:Number(form.carrierId)||0,carrierForwarder:carrier?.name??'',dateChangeReason:undefined}
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
