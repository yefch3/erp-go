<template>
  <div class="page">
    <header class="hero"><div><p class="eyebrow">{{ t('warehouse.settings.eyebrow') }}</p><h1>{{ t('warehouse.settings.title') }}</h1><p>{{ t('warehouse.settings.subtitle') }}</p></div><el-button @click="router.push('/warehouses')">{{ t('warehouse.backWorkbench') }}</el-button></header>
    <el-card shadow="never" v-loading="loading">
      <h2>{{ t('warehouse.settings.deliveryPolicy') }}</h2>
      <div class="mode-grid">
        <label v-for="item in modes" :key="item.value" :class="['mode',settings.usageMode===item.value&&'active']"><el-radio v-model="settings.usageMode" :value="item.value"><strong>{{ item.title }}</strong></el-radio><p>{{ item.description }}</p></label>
      </div>
      <el-divider/>
      <div v-if="settings.usageMode==='USE_WAREHOUSE'" class="warehouse-default">
        <div><strong>{{ t('warehouse.settings.defaultWarehouse') }}</strong><p>{{ t('warehouse.settings.defaultHint') }}</p></div>
        <el-select v-model="settings.defaultWarehouseId" clearable :placeholder="t('warehouse.settings.selectWarehouse')"><el-option v-for="warehouse in activeWarehouses" :key="warehouse.id" :label="`${warehouse.code} · ${warehouse.name}`" :value="warehouse.id"/></el-select>
        <el-alert v-if="activeWarehouses.length===0" type="warning" :closable="false" :title="t('warehouse.settings.noActiveWarehouse')"/>
      </div>
      <el-alert type="info" :closable="false" :title="t('warehouse.settings.automaticCapabilities')"/>
      <div class="footer"><el-button type="primary" :loading="saving" @click="save">{{ t('warehouse.settings.save') }}</el-button></div>
    </el-card>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { ElMessage } from 'element-plus'
import { useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { get, put } from '../api'
interface Warehouse { id:string; code:string; name:string; status:string }
const router=useRouter(), { t }=useI18n(), loading=ref(false), saving=ref(false), warehouses=ref<Warehouse[]>([])
const settings=reactive({usageMode:'USE_WAREHOUSE',allowDirectDelivery:true,allowInventory:true,defaultWarehouseId:'0'})
const modes=computed(()=>['NO_WAREHOUSE','USE_WAREHOUSE','SELECT_PER_ORDER'].map(value=>({value,title:t(`warehouse.workbench.modes.${value}`),description:t(`warehouse.settings.modeDescriptions.${value}`)})))
const activeWarehouses=computed(()=>warehouses.value.filter(x=>x.status==='ACTIVE'))
async function load(){loading.value=true;try{const [w,s]=await Promise.all([get<{warehouses:Warehouse[]}>('/warehouses',{include_inactive:true}),get<{settings:typeof settings}>('/warehouse-settings')]);warehouses.value=w.warehouses??[];Object.assign(settings,s.settings??{})}finally{loading.value=false}}
async function save(){
  if(settings.usageMode==='USE_WAREHOUSE' && (!settings.defaultWarehouseId || settings.defaultWarehouseId==='0')){
    ElMessage.warning(t('warehouse.settings.defaultRequired'))
    return
  }
  saving.value=true
  try{
    await put('/warehouse-settings',{
      usageMode:settings.usageMode,
      allowDirectDelivery:settings.usageMode!=='USE_WAREHOUSE',
      allowInventory:settings.usageMode!=='NO_WAREHOUSE',
      defaultWarehouseId:settings.usageMode==='USE_WAREHOUSE'?settings.defaultWarehouseId:'0',
    })
    ElMessage.success(t('warehouse.settings.saved'))
  }finally{saving.value=false}
}
onMounted(load)
</script>

<style scoped>
.page{padding:28px;max-width:1200px;margin:auto}.hero{display:flex;justify-content:space-between;align-items:center;margin-bottom:20px}.hero h1{font-size:30px;margin:3px 0}.hero p,.mode p,.warehouse-default p{color:#748196;margin:5px 0}.eyebrow{font-size:12px!important;letter-spacing:2px;color:#087f78!important;font-weight:700}.mode-grid{display:grid;grid-template-columns:repeat(3,1fr);gap:14px;margin-top:16px}.mode{display:block;border:1px solid #dfe7ee;border-radius:12px;padding:18px;cursor:pointer}.mode.active{border-color:#159b8e;background:#f0faf8;box-shadow:0 0 0 1px #159b8e}.mode p{font-size:13px;padding-left:24px}.warehouse-default{display:grid;grid-template-columns:minmax(280px,1fr) 420px;gap:18px;align-items:center;margin-bottom:22px}.warehouse-default .el-alert{grid-column:1/-1}.footer{text-align:right;margin-top:20px}@media(max-width:800px){.mode-grid{grid-template-columns:1fr}.hero{align-items:flex-start;flex-direction:column;gap:12px}.warehouse-default{grid-template-columns:1fr}.page{padding:16px}}
</style>
