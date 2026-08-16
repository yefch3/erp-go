<template>
  <el-select :model-value="modelValue" filterable clearable remote :remote-method="load" :loading="loading" :placeholder="placeholder || t('common.selectEmployee')" style="width:100%" @update:model-value="change">
    <el-option v-for="item in options" :key="item.id" :value="item.id" :label="`${item.employeeNo || ''} · ${item.name}`" />
  </el-select>
</template>
<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { get } from '../../api'
export interface EmployeeOption{id:string;employeeNo?:string;name:string;status:string}
const props=defineProps<{modelValue:string|number;placeholder?:string;dataScopeModule?:string}>();const emit=defineEmits<{ 'update:modelValue':[string|number];selected:[EmployeeOption|undefined]}>()
const {t}=useI18n();const loading=ref(false);const options=ref<EmployeeOption[]>([])
// 员工选择统一限制为在职人员。
async function load(keyword=''){
  loading.value=true
  try{
    const path=props.dataScopeModule==="shipping"?'/shipping/responsible-options':'/employees'
    const params=props.dataScopeModule==="shipping"?{keyword}:{page:1,page_size:100,keyword,employment_status:'ACTIVE'}
    const data=await get<{employees:EmployeeOption[]}>(path,params)
    const normalized=keyword.trim().toLowerCase()
    options.value=(data.employees??[]).filter(item=>!normalized||`${item.employeeNo??''} ${item.name}`.toLowerCase().includes(normalized))
  }finally{loading.value=false}
}
function change(value:string|number){emit('update:modelValue',value);emit('selected',options.value.find(item=>String(item.id)===String(value)))}
onMounted(()=>load())
</script>
