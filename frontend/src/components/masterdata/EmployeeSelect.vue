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
defineProps<{modelValue:string|number;placeholder?:string}>();const emit=defineEmits<{ 'update:modelValue':[string|number];selected:[EmployeeOption|undefined]}>()
const {t}=useI18n();const loading=ref(false);const options=ref<EmployeeOption[]>([])
// 员工选择统一限制为在职人员。
async function load(keyword=''){loading.value=true;try{const data=await get<{employees:EmployeeOption[]}>('/employees',{page:1,page_size:100,keyword,status:'ACTIVE'});options.value=data.employees??[]}finally{loading.value=false}}
function change(value:string|number){emit('update:modelValue',value);emit('selected',options.value.find(item=>String(item.id)===String(value)))}
onMounted(()=>load())
</script>
