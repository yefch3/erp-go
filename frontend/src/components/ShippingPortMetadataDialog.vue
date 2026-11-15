<template>
 <el-dialog v-model="open" title="纠正港口编码与时区" width="min(560px,94vw)" :close-on-click-modal="false" destroy-on-close>
  <el-alert title="仅纠正原港口的主数据关联，不更换实际港口，保留预计、实际发生时刻和船期状态；日期按正确时区重新换算。" type="info" :closable="false"/>
  <el-form label-position="top" style="margin-top:16px">
   <el-form-item label="需要纠正的港口"><el-radio-group v-model="terminal" @change="portId=''">
    <el-radio-button value="ORIGIN">起运港</el-radio-button><el-radio-button value="DESTINATION">目的港</el-radio-button>
   </el-radio-group></el-form-item>
   <p>当前港口：<b>{{terminal==='ORIGIN'?schedule.portOfLoading:schedule.portOfDischarge}}</b></p>
   <p class="current">当前编码 / 时区：{{terminal==='ORIGIN'?schedule.loadingPortCode:schedule.dischargePortCode}} / {{terminal==='ORIGIN'?schedule.loadingPortTimezone:schedule.dischargePortTimezone}}</p>
   <el-form-item label="正确的港口主数据" required><PortSelect v-model="portId" placeholder="按当前港口名称选择正确主数据"/></el-form-item>
   <el-form-item label="纠正原因" required><el-input v-model="reason" type="textarea" :rows="2" placeholder="说明原编码或时区为什么不正确"/></el-form-item>
  </el-form>
  <template #footer><el-button :disabled="saving" @click="open=false">取消</el-button><el-button type="primary" :loading="saving" @click="save">保存纠正</el-button></template>
 </el-dialog>
</template>
<script setup lang="ts">
import {computed,ref,watch} from 'vue'
import {ElMessage} from 'element-plus'
import {post} from '../api'
import type {ShippingSchedule} from '../shipping'
import PortSelect from './masterdata/PortSelect.vue'
const props=defineProps<{modelValue:boolean;schedule:ShippingSchedule}>()
const emit=defineEmits<{'update:modelValue':[boolean];saved:[]}>()
const open=computed({get:()=>props.modelValue,set:value=>emit('update:modelValue',value)})
const terminal=ref('DESTINATION'),portId=ref<string|number>(''),reason=ref(''),saving=ref(false)
watch(()=>props.modelValue,value=>{if(value){terminal.value='DESTINATION';portId.value='';reason.value=''}})
async function save(){if(!Number(portId.value)||!reason.value.trim()){ElMessage.warning('请选择正确的港口主数据并填写纠正原因');return}if(saving.value)return;saving.value=true
 try{await post(`/shipping/schedules/${props.schedule.id}/port-metadata`,{terminal:terminal.value,portId:String(portId.value),routeVersion:props.schedule.routeVersion,reason:reason.value.trim()});ElMessage.success('港口编码与时区已纠正');open.value=false;emit('saved')}finally{saving.value=false}}
</script>
<style scoped>.current{font-size:13px;color:#71828e;overflow-wrap:anywhere}</style>

