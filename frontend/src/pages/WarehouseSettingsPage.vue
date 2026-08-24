<template>
  <div class="page">
    <header class="hero"><div><p class="eyebrow">BUSINESS SETTINGS</p><h1>业务设置</h1><p>设置采购货物默认如何交付；只影响新业务，不删除历史单据、仓库档案或库存记录。</p></div><el-button @click="router.push('/warehouses')">返回工作台</el-button></header>
    <el-card shadow="never" v-loading="loading">
      <h2>采购交付策略</h2>
      <div class="mode-grid">
        <label v-for="item in modes" :key="item.value" :class="['mode',settings.usageMode===item.value&&'active']"><el-radio v-model="settings.usageMode" :value="item.value"><strong>{{ item.title }}</strong></el-radio><p>{{ item.description }}</p></label>
      </div>
      <el-divider/>
      <div v-if="settings.usageMode==='USE_WAREHOUSE'" class="warehouse-default">
        <div><strong>默认仓库</strong><p>统一经过仓库时，新采购默认进入该仓库；具体单据后续仍可按权限调整。</p></div>
        <el-select v-model="settings.defaultWarehouseId" clearable placeholder="请选择启用中的仓库"><el-option v-for="warehouse in activeWarehouses" :key="warehouse.id" :label="`${warehouse.code} · ${warehouse.name}`" :value="warehouse.id"/></el-select>
        <el-alert v-if="activeWarehouses.length===0" type="warning" :closable="false" title="目前没有启用中的仓库，请先维护并启用仓库档案。"/>
      </div>
      <el-alert type="info" :closable="false" title="系统会自动匹配直送和库存能力，无需额外开关；每次保存均保留历史记录。"/>
      <div class="footer"><el-button type="primary" :loading="saving" @click="save">保存设置</el-button></div>
    </el-card>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { ElMessage } from 'element-plus'
import { useRouter } from 'vue-router'
import { get, put } from '../api'
interface Warehouse { id:string; code:string; name:string; status:string }
const router=useRouter(), loading=ref(false), saving=ref(false), warehouses=ref<Warehouse[]>([])
const settings=reactive({usageMode:'USE_WAREHOUSE',allowDirectDelivery:true,allowInventory:true,defaultWarehouseId:'0'})
const modes=[
  {value:'NO_WAREHOUSE',title:'默认直接交付',description:'新采购默认发往港口、客户或指定地点，不形成公司库存。'},
  {value:'USE_WAREHOUSE',title:'统一经过仓库',description:'新采购默认先进入指定仓库，再由仓库完成库存和后续发货。'},
  {value:'SELECT_PER_ORDER',title:'按订单选择',description:'创建采购单时，逐单选择直接交付或先入库再发货。'},
]
const activeWarehouses=computed(()=>warehouses.value.filter(x=>x.status==='ACTIVE'))
async function load(){loading.value=true;try{const [w,s]=await Promise.all([get<{warehouses:Warehouse[]}>('/warehouses',{include_inactive:true}),get<{settings:typeof settings}>('/warehouse-settings')]);warehouses.value=w.warehouses??[];Object.assign(settings,s.settings??{})}finally{loading.value=false}}
async function save(){
  if(settings.usageMode==='USE_WAREHOUSE' && (!settings.defaultWarehouseId || settings.defaultWarehouseId==='0')){
    ElMessage.warning('统一经过仓库时，请先选择默认仓库')
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
    ElMessage.success('业务设置已保存')
  }finally{saving.value=false}
}
onMounted(load)
</script>

<style scoped>
.page{padding:28px;max-width:1200px;margin:auto}.hero{display:flex;justify-content:space-between;align-items:center;margin-bottom:20px}.hero h1{font-size:30px;margin:3px 0}.hero p,.mode p,.warehouse-default p{color:#748196;margin:5px 0}.eyebrow{font-size:12px!important;letter-spacing:2px;color:#087f78!important;font-weight:700}.mode-grid{display:grid;grid-template-columns:repeat(3,1fr);gap:14px;margin-top:16px}.mode{display:block;border:1px solid #dfe7ee;border-radius:12px;padding:18px;cursor:pointer}.mode.active{border-color:#159b8e;background:#f0faf8;box-shadow:0 0 0 1px #159b8e}.mode p{font-size:13px;padding-left:24px}.warehouse-default{display:grid;grid-template-columns:minmax(280px,1fr) 420px;gap:18px;align-items:center;margin-bottom:22px}.warehouse-default .el-alert{grid-column:1/-1}.footer{text-align:right;margin-top:20px}@media(max-width:800px){.mode-grid{grid-template-columns:1fr}.hero{align-items:flex-start;flex-direction:column;gap:12px}.warehouse-default{grid-template-columns:1fr}.page{padding:16px}}
</style>
