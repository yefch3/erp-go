<template>
  <div class="page">
    <header class="hero">
      <div><p class="eyebrow">{{ t('warehouse.workbench.eyebrow') }}</p><h1>{{ t('warehouse.workbench.title') }}</h1><p>{{ t('warehouse.workbench.subtitle') }}</p></div>
      <div class="actions"><el-button v-if="isAdmin" @click="router.push('/warehouses/settings')">{{ t('warehouse.workbench.settings') }}</el-button><el-button type="primary" @click="router.push('/warehouses/profiles')">{{ t('warehouse.workbench.maintainProfiles') }}</el-button></div>
    </header>

    <el-card v-loading="loading" shadow="never" class="mode-card">
      <div><span class="muted">{{ t('warehouse.workbench.currentMode') }}</span><h2>{{ modeText }}</h2><p>{{ modeDescription }}</p></div>
      <el-tag :type="settings.usageMode === 'NO_WAREHOUSE' ? 'info' : 'success'">{{ modeText }}</el-tag>
    </el-card>

    <section class="stats">
      <el-card v-for="item in stats" :key="item.label" shadow="never"><span>{{ item.label }}</span><strong>{{ item.value }}</strong></el-card>
    </section>

    <section v-if="canReadOrders" class="inbound-grid">
      <el-card shadow="never" class="inbound-card" @click="router.push('/warehouses/arrivals')">
        <span class="muted">{{ t('warehouse.workbench.arrivalAndReceipt') }}</span><strong>{{ pendingArrivalCount }}</strong><h3>{{ t('warehouse.workbench.pendingOrders') }}</h3><p>{{ t('warehouse.workbench.pendingHint') }}</p>
      </el-card>
      <el-card shadow="never" class="inbound-card" @click="router.push('/warehouses/receipts')">
        <span class="muted">{{ t('warehouse.workbench.records') }}</span><strong>→</strong><h3>{{ t('warehouse.workbench.receiptRecords') }}</h3><p>{{ t('warehouse.workbench.receiptHint') }}</p>
      </el-card>
    </section>

    <el-card v-if="canImport" shadow="never" class="import-card" @click="router.push('/warehouses/imports')">
      <div><span class="muted">{{ t('warehouse.workbench.openingStock') }}</span><h3>{{ t('warehouse.workbench.importCenter') }}</h3><p>{{ t('warehouse.workbench.importHint') }}</p></div>
      <el-button type="primary">{{ t('warehouse.workbench.startImport') }}</el-button>
    </el-card>

    <el-card shadow="never" class="workspace">
      <template #header><div class="card-head"><div><h2>{{ t('warehouse.workbench.profileOverview') }}</h2><p>{{ t('warehouse.workbench.profileHint') }}</p></div><el-button link type="primary" @click="router.push('/warehouses/profiles')">{{ t('warehouse.workbench.viewAll') }}</el-button></div></template>
      <el-empty v-if="!warehouses.length" :description="t('warehouse.workbench.noProfiles')">
        <el-button type="primary" @click="router.push('/warehouses/profiles')">{{ t('warehouse.workbench.createFirst') }}</el-button>
      </el-empty>
      <div v-else class="warehouse-grid">
        <article v-for="warehouse in warehouses.slice(0, 6)" :key="warehouse.id">
          <div class="card-head"><strong>{{ warehouse.name }}</strong><el-tag size="small" :type="warehouse.status === 'ACTIVE' ? 'success' : 'info'">{{ warehouse.status === 'ACTIVE' ? t('warehouse.statusActive') : t('warehouse.statusInactive') }}</el-tag></div>
          <p>{{ profileText(warehouse.profileType) }} · {{ warehouse.code }}</p>
          <p>{{ [warehouse.city, warehouse.countryCode].filter(Boolean).join(' · ') || t('warehouse.locationUnset') }}</p>
        </article>
      </div>
    </el-card>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { get } from '../api'
import { useAuthStore } from '../stores/auth'

interface Warehouse { id:string; code:string; name:string; profileType:string; city:string; countryCode:string; status:string }
interface Settings { usageMode:string; allowDirectDelivery:boolean; allowInventory:boolean; defaultWarehouseId:string }
const router=useRouter(), auth=useAuthStore(), { t }=useI18n(), loading=ref(false), warehouses=ref<Warehouse[]>([])
const isAdmin=computed(()=>auth.can('iam:role:write'))
const canReadOrders=computed(()=>auth.can('procurement:order:read'))
const canImport=computed(()=>auth.can('inventory:stock:import'))
const pendingArrivalCount=ref(0)
const settings=ref<Settings>({usageMode:'USE_WAREHOUSE',allowDirectDelivery:true,allowInventory:true,defaultWarehouseId:'0'})
const modeText=computed(()=>t(`warehouse.workbench.modes.${settings.value.usageMode}`))
const modeDescription=computed(()=>t(`warehouse.workbench.modeDescriptions.${settings.value.usageMode}`))
const stats=computed(()=>[
  {label:t('warehouse.workbench.enabled'),value:warehouses.value.filter(x=>x.status==='ACTIVE').length},
  {label:t('warehouse.workbench.own'),value:warehouses.value.filter(x=>x.profileType==='OWN').length},
  {label:t('warehouse.workbench.port'),value:warehouses.value.filter(x=>x.profileType==='PORT').length},
  {label:t('warehouse.workbench.thirdParty'),value:warehouses.value.filter(x=>x.profileType==='THIRD_PARTY').length},
])
function profileText(value:string){return ({OWN:t('warehouse.workbench.own'),PORT:t('warehouse.workbench.port'),THIRD_PARTY:t('warehouse.workbench.thirdParty')}[value] ?? value)}
async function load(){loading.value=true;try{const [w,s,ordered,partial]=await Promise.all([get<{warehouses:Warehouse[]}>('/warehouses',{include_inactive:true}),get<{settings:Settings}>('/warehouse-settings'),canReadOrders.value?get<{meta?:{total?:number}}>('/purchase-orders',{status:'ORDERED',page_size:1}):Promise.resolve<{meta?:{total?:number}}>({}),canReadOrders.value?get<{meta?:{total?:number}}>('/purchase-orders',{status:'PARTIALLY_RECEIVED',page_size:1}):Promise.resolve<{meta?:{total?:number}}>({})]);warehouses.value=w.warehouses??[];settings.value=s.settings??settings.value;pendingArrivalCount.value=Number(ordered.meta?.total??0)+Number(partial.meta?.total??0)}finally{loading.value=false}}
onMounted(load)
</script>

<style scoped>
.inbound-grid{display:grid;grid-template-columns:repeat(2,1fr);gap:14px;margin-bottom:18px}.inbound-card{cursor:pointer}.inbound-card:hover{border-color:#159b8e}.inbound-card strong{float:right;font-size:30px;color:#087f78}.inbound-card h3{margin:8px 0}.inbound-card p{color:#738095;margin:5px 0}
.import-card{margin-bottom:18px;cursor:pointer}.import-card:hover{border-color:#159b8e}.import-card :deep(.el-card__body){display:flex;align-items:center;justify-content:space-between;gap:20px}.import-card h3{margin:6px 0}.import-card p{margin:0;color:#738095}
.page{padding:28px;max-width:1500px;margin:auto}.hero,.card-head{display:flex;align-items:center;justify-content:space-between;gap:20px}.hero h1{font-size:30px;margin:4px 0}.hero p,.workspace p,.mode-card p{color:#738095;margin:5px 0}.eyebrow{font-size:12px!important;letter-spacing:2px;color:#087f78!important;font-weight:700}.actions{display:flex}.mode-card{margin:22px 0;border-color:#cfe8e4}.mode-card :deep(.el-card__body){display:flex;justify-content:space-between;align-items:center;background:linear-gradient(110deg,#f1faf8,#fff)}.mode-card h2{margin:7px 0}.muted{color:#738095}.stats{display:grid;grid-template-columns:repeat(4,1fr);gap:14px;margin-bottom:18px}.stats span{display:block;color:#738095}.stats strong{display:block;font-size:30px;margin-top:10px;color:#17324d}.workspace h2{margin:0}.warehouse-grid{display:grid;grid-template-columns:repeat(3,1fr);gap:14px}.warehouse-grid article{border:1px solid #e1e8ef;border-radius:10px;padding:16px}.warehouse-grid p{font-size:13px}@media(max-width:900px){.hero{align-items:flex-start;flex-direction:column}.stats,.warehouse-grid{grid-template-columns:1fr 1fr}}@media(max-width:560px){.stats,.warehouse-grid{grid-template-columns:1fr}.page{padding:16px}}
@media(max-width:560px){.inbound-grid{grid-template-columns:1fr}}
</style>
