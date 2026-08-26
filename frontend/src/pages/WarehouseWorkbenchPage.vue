<template>
  <div class="page">
    <header class="hero">
      <div><p class="eyebrow">WAREHOUSE CONTROL</p><h1>仓库管理</h1><p>统一管理公司仓、港口仓与第三方仓库；是否经过仓库由公司模式决定。</p></div>
      <div class="actions"><el-button v-if="isAdmin" @click="router.push('/warehouses/settings')">业务设置</el-button><el-button type="primary" @click="router.push('/warehouses/profiles')">维护仓库档案</el-button></div>
    </header>

    <el-card v-loading="loading" shadow="never" class="mode-card">
      <div><span class="muted">当前公司模式</span><h2>{{ modeText }}</h2><p>{{ modeDescription }}</p></div>
      <el-tag :type="settings.usageMode === 'NO_WAREHOUSE' ? 'info' : 'success'">{{ modeText }}</el-tag>
    </el-card>

    <section class="stats">
      <el-card v-for="item in stats" :key="item.label" shadow="never"><span>{{ item.label }}</span><strong>{{ item.value }}</strong></el-card>
    </section>

    <section v-if="canReadOrders" class="inbound-grid">
      <el-card shadow="never" class="inbound-card" @click="router.push('/warehouses/arrivals')">
        <span class="muted">到货与收货</span><strong>{{ pendingArrivalCount }}</strong><h3>待到货采购单</h3><p>登记直送签收或仓库分批收货。</p>
      </el-card>
      <el-card shadow="never" class="inbound-card" @click="router.push('/warehouses/receipts')">
        <span class="muted">业务记录</span><strong>→</strong><h3>查看收货记录</h3><p>按收货单查看时间、数量、地点和登记人。</p>
      </el-card>
    </section>

    <el-card v-if="canImport" shadow="never" class="import-card" @click="router.push('/warehouses/imports')">
      <div><span class="muted">期初库存</span><h3>导入中心</h3><p>下载版本模板，上传后先预检，确认无误才写入库存流水。</p></div>
      <el-button type="primary">开始导入 →</el-button>
    </el-card>

    <el-card shadow="never" class="workspace">
      <template #header><div class="card-head"><div><h2>仓库档案概览</h2><p>联系人、负责人、地址和记账方式集中维护。</p></div><el-button link type="primary" @click="router.push('/warehouses/profiles')">查看全部 →</el-button></div></template>
      <el-empty v-if="!warehouses.length" description="尚未建立仓库档案">
        <el-button type="primary" @click="router.push('/warehouses/profiles')">建立第一个仓库</el-button>
      </el-empty>
      <div v-else class="warehouse-grid">
        <article v-for="warehouse in warehouses.slice(0, 6)" :key="warehouse.id">
          <div class="card-head"><strong>{{ warehouse.name }}</strong><el-tag size="small" :type="warehouse.status === 'ACTIVE' ? 'success' : 'info'">{{ warehouse.status === 'ACTIVE' ? '启用' : '停用' }}</el-tag></div>
          <p>{{ profileText(warehouse.profileType) }} · {{ warehouse.code }}</p>
          <p>{{ [warehouse.city, warehouse.countryCode].filter(Boolean).join(' · ') || '未填写地点' }}</p>
        </article>
      </div>
    </el-card>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import { get } from '../api'
import { useAuthStore } from '../stores/auth'

interface Warehouse { id:string; code:string; name:string; profileType:string; city:string; countryCode:string; status:string }
interface Settings { usageMode:string; allowDirectDelivery:boolean; allowInventory:boolean; defaultWarehouseId:string }
const router=useRouter(), auth=useAuthStore(), loading=ref(false), warehouses=ref<Warehouse[]>([])
const isAdmin=computed(()=>auth.can('iam:role:write'))
const canReadOrders=computed(()=>auth.can('procurement:order:read'))
const canImport=computed(()=>auth.can('inventory:stock:import'))
const pendingArrivalCount=ref(0)
const settings=ref<Settings>({usageMode:'USE_WAREHOUSE',allowDirectDelivery:true,allowInventory:true,defaultWarehouseId:'0'})
const modeText=computed(()=>({NO_WAREHOUSE:'默认直接交付',USE_WAREHOUSE:'统一经过仓库',SELECT_PER_ORDER:'按订单选择'}[settings.value.usageMode] ?? settings.value.usageMode))
const modeDescription=computed(()=>settings.value.usageMode==='NO_WAREHOUSE'?'新采购默认直接发往港口、客户或指定地点。':settings.value.usageMode==='SELECT_PER_ORDER'?'经办人在每张采购单上选择直接交付或先入仓再发货。':'新采购统一进入默认仓库，再按收货、库存和出库流程交付。')
const stats=computed(()=>[
  {label:'启用仓库',value:warehouses.value.filter(x=>x.status==='ACTIVE').length},
  {label:'公司自有仓',value:warehouses.value.filter(x=>x.profileType==='OWN').length},
  {label:'港口仓库',value:warehouses.value.filter(x=>x.profileType==='PORT').length},
  {label:'第三方仓库',value:warehouses.value.filter(x=>x.profileType==='THIRD_PARTY').length},
])
function profileText(value:string){return ({OWN:'公司自有仓',PORT:'港口仓库',THIRD_PARTY:'第三方仓库'}[value] ?? value)}
async function load(){loading.value=true;try{const [w,s,ordered,partial]=await Promise.all([get<{warehouses:Warehouse[]}>('/warehouses',{include_inactive:true}),get<{settings:Settings}>('/warehouse-settings'),canReadOrders.value?get<{meta?:{total?:number}}>('/purchase-orders',{status:'ORDERED',page_size:1}):Promise.resolve({}),canReadOrders.value?get<{meta?:{total?:number}}>('/purchase-orders',{status:'PARTIALLY_RECEIVED',page_size:1}):Promise.resolve({})]);warehouses.value=w.warehouses??[];settings.value=s.settings??settings.value;pendingArrivalCount.value=Number(ordered.meta?.total??0)+Number(partial.meta?.total??0)}finally{loading.value=false}}
onMounted(load)
</script>

<style scoped>
.inbound-grid{display:grid;grid-template-columns:repeat(2,1fr);gap:14px;margin-bottom:18px}.inbound-card{cursor:pointer}.inbound-card:hover{border-color:#159b8e}.inbound-card strong{float:right;font-size:30px;color:#087f78}.inbound-card h3{margin:8px 0}.inbound-card p{color:#738095;margin:5px 0}
.import-card{margin-bottom:18px;cursor:pointer}.import-card:hover{border-color:#159b8e}.import-card :deep(.el-card__body){display:flex;align-items:center;justify-content:space-between;gap:20px}.import-card h3{margin:6px 0}.import-card p{margin:0;color:#738095}
.page{padding:28px;max-width:1500px;margin:auto}.hero,.card-head{display:flex;align-items:center;justify-content:space-between;gap:20px}.hero h1{font-size:30px;margin:4px 0}.hero p,.workspace p,.mode-card p{color:#738095;margin:5px 0}.eyebrow{font-size:12px!important;letter-spacing:2px;color:#087f78!important;font-weight:700}.actions{display:flex}.mode-card{margin:22px 0;border-color:#cfe8e4}.mode-card :deep(.el-card__body){display:flex;justify-content:space-between;align-items:center;background:linear-gradient(110deg,#f1faf8,#fff)}.mode-card h2{margin:7px 0}.muted{color:#738095}.stats{display:grid;grid-template-columns:repeat(4,1fr);gap:14px;margin-bottom:18px}.stats span{display:block;color:#738095}.stats strong{display:block;font-size:30px;margin-top:10px;color:#17324d}.workspace h2{margin:0}.warehouse-grid{display:grid;grid-template-columns:repeat(3,1fr);gap:14px}.warehouse-grid article{border:1px solid #e1e8ef;border-radius:10px;padding:16px}.warehouse-grid p{font-size:13px}@media(max-width:900px){.hero{align-items:flex-start;flex-direction:column}.stats,.warehouse-grid{grid-template-columns:1fr 1fr}}@media(max-width:560px){.stats,.warehouse-grid{grid-template-columns:1fr}.page{padding:16px}}
@media(max-width:560px){.inbound-grid{grid-template-columns:1fr}}
</style>
