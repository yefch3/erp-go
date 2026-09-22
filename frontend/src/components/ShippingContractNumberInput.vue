<template>
 <el-select :model-value="modelValue" :disabled="disabled" filterable allow-create remote :remote-method="search" :loading="loading" placeholder="搜索销售合同，或填写待补录合同号" @change="choose" @visible-change="onVisible">
  <el-option v-for="c in options" :key="c.id" :value="c.contractNo" :label="c.externalContractNo ? `${c.contractNo}（原单号：${c.externalContractNo}）` : c.contractNo" />
 </el-select>
</template>
<script setup lang="ts">
import { ref } from 'vue'
import { get } from '../api'
const props=defineProps<{modelValue:string;disabled?:boolean}>()
const emit=defineEmits<{'update:modelValue':[string];selected:[string]}>()
const options=ref<{id:string;contractNo:string;externalContractNo:string}[]>([]),loading=ref(false)
let sequence=0
async function search(keyword:string){const current=++sequence;loading.value=true;try{const r=await get<{contracts:typeof options.value}>('/shipping/contract-options',{keyword});if(current===sequence)options.value=r.contracts||[]}finally{if(current===sequence)loading.value=false}}
function onVisible(open:boolean){if(open)void search(props.modelValue)}
function choose(value:string){emit('update:modelValue',value);emit('selected',String(options.value.find(c=>c.contractNo===value)?.id||'0'))}
</script>
