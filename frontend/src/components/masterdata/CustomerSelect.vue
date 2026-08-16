<template>
  <el-select :model-value="modelValue" filterable clearable remote :remote-method="load" :loading="loading" :placeholder="placeholder || t('common.selectCustomer')" style="width:100%" @update:model-value="change">
    <el-option v-for="item in options" :key="item.id" :value="item.id" :label="`${item.code} · ${item.name}`" />
  </el-select>
</template>
<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { get } from '../../api'
export interface CustomerOption { id:string; code:string; name:string; status:string }
defineProps<{modelValue:string|number;placeholder?:string}>()
const emit=defineEmits<{ 'update:modelValue':[string|number]; selected:[CustomerOption|undefined] }>()
const {t}=useI18n();const loading=ref(false);const options=ref<CustomerOption[]>([])
// 公共选择器只读取可用客户，停用资料不会进入新业务单据。
async function load(keyword=''){loading.value=true;try{const data=await get<{customers:CustomerOption[]}>('/customers',{page:1,page_size:100,keyword,status:'ACTIVE'});options.value=data.customers??[]}finally{loading.value=false}}
function change(value:string|number){emit('update:modelValue',value);emit('selected',options.value.find(item=>String(item.id)===String(value)))}
onMounted(()=>load())
</script>
