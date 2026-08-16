<template>
  <el-select :model-value="modelValue" filterable clearable remote :remote-method="load" :loading="loading" :placeholder="placeholder || t('common.selectPort')" style="width:100%" @update:model-value="change">
    <el-option v-for="item in options" :key="item.id" :value="item.id" :label="label(item)" />
  </el-select>
</template>
<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { get } from '../../api'
export interface PortOption{id:string;unlocode:string;nameZh:string;nameEn:string;timezone:string;countryCode:string;isFavorite:boolean;status:string}
defineProps<{modelValue:string|number;placeholder?:string}>();const emit=defineEmits<{ 'update:modelValue':[string|number];selected:[PortOption|undefined]}>()
const {t,locale}=useI18n();const loading=ref(false);const options=ref<PortOption[]>([])
function label(item:PortOption){const name=locale.value==='zh'?(item.nameZh||item.nameEn):(item.nameEn||item.nameZh);return `${item.isFavorite?'★ ':''}${item.unlocode} · ${name} · ${item.countryCode}`}
// 港口选择始终过滤停用记录，历史船期仍保留原港口快照。
async function load(keyword=''){loading.value=true;try{const data=await get<{ports:PortOption[]}>('/ports',{page:1,page_size:100,keyword,status:'ACTIVE'});options.value=data.ports??[]}finally{loading.value=false}}
function change(value:string|number){emit('update:modelValue',value);emit('selected',options.value.find(item=>String(item.id)===String(value)))}
onMounted(()=>load())
</script>
