<template>
  <div class="linked-selects">
    <el-select :model-value="supplierId" filterable clearable remote :remote-method="loadSuppliers" :loading="supplierLoading" :placeholder="t('common.selectSupplier')" @update:model-value="changeSupplier"><el-option v-for="item in suppliers" :key="item.id" :value="item.id" :label="`${item.code} · ${item.nameZh||item.nameEn||item.name}`" /></el-select>
    <el-select :model-value="factoryId" filterable clearable remote :remote-method="loadFactories" :loading="factoryLoading" :disabled="!supplierId" :placeholder="factoryOptional ? '选择合作中工厂（可选）' : t('common.selectFactory')" @update:model-value="changeFactory"><el-option v-for="item in factories" :key="item.id" :value="item.id" :label="`${item.code} · ${item.nameZh||item.nameEn}`" /></el-select>
  </div>
</template>
<script setup lang="ts">
import { onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { get } from '../../api'
interface SupplierOption{id:string;code:string;name:string;nameZh:string;nameEn:string;status:string}
interface FactoryOption{id:string;code:string;nameZh:string;nameEn:string;status:string;supplierId:string}
const props=withDefaults(defineProps<{supplierId:string|number;factoryId:string|number;factoryOptional?:boolean}>(),{factoryOptional:false});const emit=defineEmits<{ 'update:supplierId':[string|number];'update:factoryId':[string|number];supplierSelected:[SupplierOption|undefined];factorySelected:[FactoryOption|undefined]}>()
const {t}=useI18n();const suppliers=ref<SupplierOption[]>([]);const factories=ref<FactoryOption[]>([]);const supplierLoading=ref(false);const factoryLoading=ref(false)
async function loadSuppliers(keyword=''){supplierLoading.value=true;try{const data=await get<{suppliers:SupplierOption[]}>('/suppliers',{page:1,page_size:100,keyword,status:'ACTIVE'});suppliers.value=data.suppliers??[]}finally{supplierLoading.value=false}}
// 新业务只允许选择“合作中”工厂；待评估、暂停和停用工厂都不进入下拉项。
async function loadFactories(keyword=''){if(!props.supplierId){factories.value=[];return}factoryLoading.value=true;try{const data=await get<{factories:FactoryOption[]}>('/factories',{page:1,page_size:100,keyword,supplier_id:props.supplierId,status:'COOPERATING'});factories.value=data.factories??[]}finally{factoryLoading.value=false}}
function changeSupplier(value:string|number){emit('update:supplierId',value);emit('update:factoryId','');emit('supplierSelected',suppliers.value.find(item=>String(item.id)===String(value)));factories.value=[];if(value)loadFactories()}
function changeFactory(value:string|number){emit('update:factoryId',value);emit('factorySelected',factories.value.find(item=>String(item.id)===String(value)))}
watch(()=>props.supplierId,()=>loadFactories());onMounted(()=>{loadSuppliers();loadFactories()})
</script>
<style scoped>.linked-selects{display:grid;grid-template-columns:1fr 1fr;gap:10px}.linked-selects :deep(.el-select){width:100%}@media(max-width:700px){.linked-selects{grid-template-columns:1fr}}</style>
