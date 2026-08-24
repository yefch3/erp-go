<template>
  <div class="page">
    <header class="hero"><div><p class="eyebrow">WAREHOUSE POLICY</p><h1>仓库设置</h1><p>决定公司业务是否经过仓库；设置只影响新业务，不删除历史单据与仓库档案。</p></div><el-button @click="router.push('/warehouses')">返回工作台</el-button></header>
    <el-card shadow="never" v-loading="loading">
      <h2>公司仓库使用模式</h2>
      <div class="mode-grid">
        <label v-for="item in modes" :key="item.value" :class="['mode',settings.usageMode===item.value&&'active']"><el-radio v-model="settings.usageMode" :value="item.value"><strong>{{ item.title }}</strong></el-radio><p>{{ item.description }}</p></label>
      </div>
      <el-divider/>
      <div class="switch-row"><div><strong>允许直接交付港口或指定地点</strong><p>采购单可不经过公司仓库，直接发往港口、客户或指定地点。</p></div><el-switch v-model="settings.allowDirectDelivery"/></div>
      <div class="switch-row"><div><strong>允许同步库存</strong><p>启用后，可将选择“同步库存”的仓库接入后续收货与库存流程。</p></div><el-switch v-model="settings.allowInventory"/></div>
      <el-form-item label="默认仓库" class="default"><el-select v-model="settings.defaultWarehouseId" clearable placeholder="不设置默认仓库"><el-option v-for="warehouse in activeWarehouses" :key="warehouse.id" :label="`${warehouse.code} · ${warehouse.name}`" :value="warehouse.id"/></el-select></el-form-item>
      <el-alert type="info" :closable="false" title="每次保存都会保留设置历史；切换为不使用仓库也不会删除现有仓库和历史库存记录。"/>
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
  {value:'NO_WAREHOUSE',title:'不使用公司仓库',description:'默认直接发往港口或指定地点，适合无自有仓库的贸易公司。'},
  {value:'USE_WAREHOUSE',title:'启用仓库管理',description:'采购、收货和库存流程可以选择启用中的仓库。'},
  {value:'SELECT_PER_ORDER',title:'每张订单自行选择',description:'同一家公司可按订单决定直接交付或先入库再发货。'},
]
const activeWarehouses=computed(()=>warehouses.value.filter(x=>x.status==='ACTIVE'))
async function load(){loading.value=true;try{const [w,s]=await Promise.all([get<{warehouses:Warehouse[]}>('/warehouses',{include_inactive:true}),get<{settings:typeof settings}>('/warehouse-settings')]);warehouses.value=w.warehouses??[];Object.assign(settings,s.settings??{})}finally{loading.value=false}}
async function save(){saving.value=true;try{await put('/warehouse-settings',{...settings,defaultWarehouseId:settings.defaultWarehouseId||'0'});ElMessage.success('仓库设置已保存')}finally{saving.value=false}}
onMounted(load)
</script>

<style scoped>
.page{padding:28px;max-width:1200px;margin:auto}.hero{display:flex;justify-content:space-between;align-items:center;margin-bottom:20px}.hero h1{font-size:30px;margin:3px 0}.hero p,.mode p,.switch-row p{color:#748196;margin:5px 0}.eyebrow{font-size:12px!important;letter-spacing:2px;color:#087f78!important;font-weight:700}.mode-grid{display:grid;grid-template-columns:repeat(3,1fr);gap:14px;margin-top:16px}.mode{display:block;border:1px solid #dfe7ee;border-radius:12px;padding:18px;cursor:pointer}.mode.active{border-color:#159b8e;background:#f0faf8;box-shadow:0 0 0 1px #159b8e}.mode p{font-size:13px;padding-left:24px}.switch-row{display:flex;align-items:center;justify-content:space-between;padding:14px 2px;border-bottom:1px solid #edf0f3}.default{margin-top:22px}.default .el-select{width:420px}.footer{text-align:right;margin-top:20px}@media(max-width:800px){.mode-grid{grid-template-columns:1fr}.hero{align-items:flex-start;flex-direction:column;gap:12px}.default .el-select{width:100%}.page{padding:16px}}
</style>
