<template>
  <el-dialog v-model="open" :title="schedule ? t('shipping.edit') : t('shipping.create')" width="760px" destroy-on-close>
    <el-form ref="formRef" :model="form" :rules="rules" label-width="110px" v-loading="loading">
      <el-row :gutter="18">
        <el-col :span="12"><el-form-item :label="t('shipping.contractNo')"><el-input v-model="form.contractNo" /></el-form-item></el-col>
        <el-col :span="12"><el-form-item :label="t('shipping.customer')">
          <el-input v-if="schedule || handoff" :model-value="form.customerName" disabled />
          <CustomerSelect v-else-if="auth.can('masterdata:customer:read')" v-model="form.customerId" @selected="customer=>form.customerName=customer?.name||''" />
          <el-input v-else v-model="form.customerName" :placeholder="t('shipping.customer')" />
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
        <el-col v-if="!schedule" :span="12"><el-form-item label="预计离港（ETD）" prop="etd"><el-date-picker v-model="form.etd" type="date" value-format="YYYY-MM-DD" style="width:100%" /></el-form-item></el-col>
        <el-col v-if="!schedule" :span="12"><el-form-item label="预计到港（ETA）" prop="eta"><el-date-picker v-model="form.eta" type="date" value-format="YYYY-MM-DD" style="width:100%" /></el-form-item></el-col>
        <el-col :span="12"><el-form-item label="订舱号"><el-input v-model="form.bookingNo" placeholder="可在订舱后补充" /></el-form-item></el-col>
        <el-col :span="12"><el-form-item label="提单号"><el-input v-model="form.billOfLadingNo" placeholder="可在开船前后补充" /></el-form-item></el-col>
        <el-col :span="12"><el-form-item label="进仓日期"><el-date-picker v-model="form.warehouseEntryDate" type="date" value-format="YYYY-MM-DD" clearable style="width:100%" /></el-form-item></el-col>
        <el-col :span="12"><el-form-item label="报关日期"><el-date-picker v-model="form.customsDeclarationDate" type="date" value-format="YYYY-MM-DD" clearable style="width:100%" /></el-form-item></el-col>
        <el-col v-if="datesChanged" :span="24"><el-form-item :label="t('shipping.dateReason')" prop="dateChangeReason"><el-input v-model="form.dateChangeReason" type="textarea" :rows="2" /></el-form-item></el-col>
        <el-col :span="24"><el-form-item :label="t('shipping.remark')"><el-input v-model="form.remark" type="textarea" :rows="2" /></el-form-item></el-col>
        <el-col :span="24"><el-form-item label="运输提醒">
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
        <el-col :span="24"><el-divider content-position="left">我的运输提醒设置</el-divider></el-col>
        <el-col :span="24"><el-form-item label="业务时区">
          <el-select v-model="form.businessTimezone" filterable style="width:100%">
            <el-option v-for="zone in timezoneOptions" :key="zone" :label="zone" :value="zone" />
          </el-select>
          <div class="reminder-help">提醒日期按此时区倒推，并自动跳过周六和周日。</div>
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
import type { ShippingReminderPreference, ShippingSchedule } from '../shipping'
import { portTimezoneOptions } from '../lib/portOptions'
import CustomerSelect from './masterdata/CustomerSelect.vue'
import EmployeeSelect from './masterdata/EmployeeSelect.vue'
import PortSelect, { type PortOption } from './masterdata/PortSelect.vue'

export interface ContractShippingHandoff {
  id:string;contractId:string;contractNo:string;customerId:string;customerName:string;batchNo:number;
  carrierForwarder:string;serviceOptionName:string;customerManaged:boolean;currency:string;freightAmount:string;
  portOfLoading:string;portOfDischarge:string;estimatedDeparture:string;estimatedArrival:string;validUntil:string;remark:string;status:string;scheduleId:string;
  finalForwarderId?:string;finalForwarderName?:string;actualCarrierId?:string;actualCarrierName?:string;finalServiceOption?:string;finalCurrency?:string;finalFreightAmount?:string;finalEtd?:string;finalEta?:string
}
const props = defineProps<{ modelValue:boolean; schedule?:ShippingSchedule; handoff?:ContractShippingHandoff }>()
const emit = defineEmits<{ 'update:modelValue':[value:boolean]; saved:[schedule:ShippingSchedule] }>()
const { t, locale }=useI18n(); const auth=useAuthStore()
const formRef=ref<FormInstance>(); const saving=ref(false); const loading=ref(false)
const newReminderDay=ref(7)
const carriers=ref<{id:string;name:string}[]>([])
const timezoneOptions=portTimezoneOptions('')
const preferenceSnapshot=ref('')
const open=computed({get:()=>props.modelValue,set:(v)=>emit('update:modelValue',v)})
const empty=()=>({contractHandoffId:'',contractNo:'',customerId:'',customerName:'',carrierId:'',carrierForwarder:'',vesselName:'',voyageNo:'',portOfLoading:'',portOfDischarge:'',loadingPortId:'',loadingPortCode:'',loadingPortTimezone:'',dischargePortId:'',dischargePortCode:'',dischargePortTimezone:'',etd:'',eta:'',bookingNo:'',billOfLadingNo:'',warehouseEntryDate:'',customsDeclarationDate:'',freightCurrency:'',freightAmount:'',responsibleEmployeeId:auth.employeeId,responsibleName:auth.employeeName,remark:'',dateChangeReason:'',reminderDays:[7] as number[],businessTimezone:'UTC'})
const form=reactive(empty())
const datesChanged=computed(()=>!!props.schedule&&(form.etd!==props.schedule.etd||form.eta!==props.schedule.eta))
const rules:FormRules={
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
    bookingNo:props.schedule.bookingNo,billOfLadingNo:props.schedule.billOfLadingNo,warehouseEntryDate:props.schedule.warehouseEntryDate,customsDeclarationDate:props.schedule.customsDeclarationDate,
    freightCurrency:props.schedule.freightCurrency,freightAmount:props.schedule.freightAmount,
    responsibleName:props.schedule.responsibleName,remark:props.schedule.remark,dateChangeReason:'',
  }:props.handoff?{
    contractHandoffId:props.handoff.id,contractNo:props.handoff.contractNo,customerId:props.handoff.customerId,customerName:props.handoff.customerName,
    carrierId:props.handoff.actualCarrierId||props.handoff.finalForwarderId||'',
    carrierForwarder:props.handoff.actualCarrierName||props.handoff.finalForwarderName||props.handoff.carrierForwarder,
    portOfLoading:props.handoff.portOfLoading,portOfDischarge:props.handoff.portOfDischarge,
    etd:props.handoff.finalEtd||props.handoff.estimatedDeparture,eta:props.handoff.finalEta||props.handoff.estimatedArrival,
    freightCurrency:props.handoff.finalCurrency||props.handoff.currency,freightAmount:props.handoff.finalFreightAmount||props.handoff.freightAmount,
    remark:[props.handoff.finalServiceOption||props.handoff.serviceOptionName,props.handoff.remark].filter(Boolean).join('；'),
  }:{})
  carriers.value=form.carrierForwarder?[{id:form.carrierId||'0',name:form.carrierForwarder}]:[]
  loading.value=true
  const requests:Promise<void>[]=[]
  requests.push(get<{preference:ShippingReminderPreference}>('/shipping/reminder-preferences').then(({preference})=>{
    form.businessTimezone=preference.timezone||'UTC'
    if(!props.schedule)form.reminderDays=[...(preference.leadDays??[7])]
    preferenceSnapshot.value=preferenceKey()
  }))
  if(props.schedule) requests.push(get<{leadDays:number[]}>(`/shipping/schedules/${props.schedule.id}/reminder-rules`).then(data=>{form.reminderDays=[...(data.leadDays??[])]}))
  // 只列启用的、真是船公司或货代的公司（B3）——钢厂不该出现在这个下拉里，
  // 停用的也不该。一家公司可以两个角色都占，所以取并集去重。
  if(auth.can('masterdata:supplier:read')) requests.push(Promise.all([
    get<{suppliers:{id:string;name:string}[]}>('/suppliers',{page_size:200,status:'ACTIVE',business_type:'CARRIER'}),
    get<{suppliers:{id:string;name:string}[]}>('/suppliers',{page_size:200,status:'ACTIVE',business_type:'FORWARDER'}),
  ]).then(([carrierList,forwarderList])=>{const seen=new Map<string,{id:string;name:string}>();for(const item of[...(carrierList.suppliers??[]),...(forwarderList.suppliers??[])])seen.set(String(item.id),item);carriers.value=[...seen.values()]}))
  await Promise.allSettled(requests)
  if(props.handoff&&!form.carrierId){
    const matched=carriers.value.find(item=>item.name===props.handoff?.carrierForwarder)
    if(matched)form.carrierId=matched.id
  }
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
  const {reminderDays:_,businessTimezone:__,...scheduleFields}=form
  const schedule={
    ...scheduleFields,
    contractHandoffId:Number(form.contractHandoffId)||0,
    customerId:Number(form.customerId)||0,
    carrierId:Number(form.carrierId)||0,
    loadingPortId:Number(form.loadingPortId)||0,
    dischargePortId:Number(form.dischargePortId)||0,
    responsibleEmployeeId:Number(form.responsibleEmployeeId)||0,
    carrierForwarder:carrier?.name??'',
    dateChangeReason:undefined,
  }
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
function preferenceKey(){return JSON.stringify({leadDays:form.reminderDays,timezone:form.businessTimezone})}
function applyPreference(_preference:ShippingReminderPreference){preferenceSnapshot.value=preferenceKey()}
async function savePreference(){
  if(preferenceKey()===preferenceSnapshot.value)return undefined
  const {preference}=await put<{preference:ShippingReminderPreference}>('/shipping/reminder-preferences',{leadDays:form.reminderDays,timezone:form.businessTimezone,holidayCountryCodes:[]})
  applyPreference(preference);return preference
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
    await savePreference()
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
