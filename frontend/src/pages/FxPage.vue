<template>
  <div class="fx-page">
    <WorkflowPageHeader :title="t('fx.referenceTitle')" :description="t('fx.referenceDescription')">
      <template #actions>
        <el-button v-if="auth.can('fx:rate:write')" @click="watchOpen=true">{{ t('fx.watch') }}</el-button>
        <el-button type="primary" :loading="refreshing" @click="refreshNow">{{ t('fx.syncNow') }}</el-button>
      </template>
    </WorkflowPageHeader>

    <el-alert v-if="sync.usingCache" type="warning" :closable="false" show-icon :title="t('fx.cacheNotice')" />
    <section class="sync-bar">
      <div><span class="dot" :class="sync.state.toLowerCase()"></span><strong>{{ syncLabel }}</strong></div>
      <span>{{ t('fx.source') }}: {{sync.provider || 'Frankfurter / ECB'}}</span>
      <span>{{ t('fx.lastSuccess') }}: {{formatTime(sync.lastSuccessAt)}}</span>
      <span>{{ t('fx.lastAttempt') }}: {{formatTime(sync.lastAttemptAt)}}</span>
    </section>

    <section v-loading="loading" class="cards">
      <article v-for="item in cards" :key="item.currency" class="rate-card">
        <header><div><strong>{{item.currency}}</strong><span>{{ t('fx.perUsd') }}</span></div><el-tag effect="plain">{{item.latest?.source || t('fx.noData')}}</el-tag></header>
        <div class="rate">{{item.latest?.unitsPerUsd || '—'}}</div>
        <div class="inverse">1 {{item.currency}} = {{item.latest?.usdPerUnit || '—'}} USD</div>
        <svg v-if="item.points.length>1" viewBox="0 0 240 70" role="img" :aria-label="t('fx.trend30Aria',{currency:item.currency})"><polyline :points="chartPoints(item.points)" fill="none" stroke="#21a6df" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round" /></svg>
        <div v-else class="chart-empty">{{ t('fx.accumulating') }}</div>
        <footer><span>{{ t('fx.rateDate') }} {{item.latest?.rateDate || '—'}}</span><span>{{ t('fx.fetched') }} {{formatTime(item.latest?.fetchedAt)}}</span></footer>
      </article>
    </section>

    <section class="notice"><strong>{{ t('fx.usage') }}</strong><p>{{ t('fx.usageDescription') }}</p></section>

    <el-dialog v-model="watchOpen" :title="t('fx.watch')" width="min(520px,94vw)">
      <el-select v-model="watchDraft" multiple filterable allow-create default-first-option style="width:100%" :placeholder="t('fx.currencyPlaceholder')">
        <el-option v-for="c in common" :key="c" :label="c" :value="c" />
      </el-select>
      <p class="hint">{{ t('fx.watchHint') }}</p>
      <template #footer><el-button @click="watchOpen=false">{{ t('fx.cancel') }}</el-button><el-button type="primary" :loading="saving" @click="saveWatch">{{ t('fx.save') }}</el-button></template>
    </el-dialog>
  </div>
</template>
<script setup lang="ts">
import {computed,onMounted,ref} from 'vue'
import {ElMessage} from 'element-plus'
import {useI18n} from 'vue-i18n'
import {get,post,put} from '../api'
import {useAuthStore} from '../stores/auth'
import WorkflowPageHeader from '../components/WorkflowPageHeader.vue'
interface Rate{quoteCurrency:string;unitsPerUsd:string;usdPerUnit:string;rateDate:string;source:string;fetchedAt:string}
interface Sync{state:string;lastAttemptAt:string;lastSuccessAt:string;lastError:string;usingCache:boolean;provider:string}
interface Card{currency:string;latest?:Rate;points:number[]}
const auth=useAuthStore(),{t,locale}=useI18n(),loading=ref(false),refreshing=ref(false),saving=ref(false),watchOpen=ref(false)
const watched=ref<string[]>([]),watchDraft=ref<string[]>([]),cards=ref<Card[]>([]),common=['CNY','EUR','GBP','JPY','HKD','CAD','AUD','SGD','CHF','KRW']
const sync=ref<Sync>({state:'STARTING',lastAttemptAt:'',lastSuccessAt:'',lastError:'',usingCache:false,provider:''})
const syncLabel=computed(()=>t(`fx.syncStates.${sync.value.state==='READY'||sync.value.state==='DEGRADED'?sync.value.state:'STARTING'}`))
const formatTime=(v?:string)=>v?new Date(v).toLocaleString(locale.value):'—'
function chartPoints(values:number[]){const min=Math.min(...values),max=Math.max(...values),span=max-min||1;return values.map((v,i)=>`${(i/(values.length-1))*236+2},${66-((v-min)/span)*60}`).join(' ')}
async function load(){loading.value=true;try{const [w,s]=await Promise.all([get<{currencies:string[]}>('/fx/watched'),get<{status:Sync}>('/fx/sync-status')]);watched.value=w.currencies||[];watchDraft.value=[...watched.value];sync.value=s.status;cards.value=await Promise.all(watched.value.map(async currency=>{const [latest,history]=await Promise.all([get<{rate:Rate}>('/fx/latest',{currency}),get<{rates:Rate[]}>('/fx/rates',{currency,days:30})]);return{currency,latest:latest.rate,points:(history.rates||[]).slice().reverse().map(x=>Number(x.unitsPerUsd)).filter(Number.isFinite)}}))}finally{loading.value=false}}
async function refreshNow(){refreshing.value=true;try{await post('/fx/refresh',{}, {timeout:30000});ElMessage.success(t('fx.syncComplete'));await load()}finally{refreshing.value=false}}
async function saveWatch(){const clean=[...new Set(watchDraft.value.map(x=>x.trim().toUpperCase()).filter(x=>/^[A-Z]{3}$/.test(x)&&x!=='USD'))];if(!clean.length||clean.length>12){ElMessage.warning(t('fx.watchInvalid'));return}saving.value=true;try{await put('/fx/watched',{currencies:clean});watchOpen.value=false;await load()}finally{saving.value=false}}
onMounted(load)
</script>
<style scoped>
.fx-page{max-width:1680px;margin:auto}.sync-bar{display:flex;gap:24px;align-items:center;flex-wrap:wrap;padding:14px 18px;margin:0 0 16px;border:1px solid #d9e8ef;border-radius:12px;background:#fff;color:#607284;font-size:13px}.sync-bar div{color:#172b3a}.dot{display:inline-block;width:9px;height:9px;margin-right:8px;border-radius:50%;background:#f2b344}.dot.ready{background:#19b779}.dot.degraded{background:#ed8b3a}.cards{display:grid;grid-template-columns:repeat(auto-fit,minmax(280px,1fr));gap:16px;min-height:180px}.rate-card{padding:18px;border:1px solid #dceaf0;border-radius:14px;background:#fff;box-shadow:0 8px 24px rgba(25,72,91,.05)}.rate-card header,.rate-card footer{display:flex;justify-content:space-between;gap:12px}.rate-card header strong{font-size:20px;margin-right:9px}.rate-card header span,.rate-card footer,.inverse,.hint{color:#718394;font-size:12px}.rate{font-size:32px;font-weight:750;color:#087fb3;margin:16px 0 2px}.rate-card svg{width:100%;height:74px;margin:14px 0 6px;background:linear-gradient(#f8fcfe,#fff);border-radius:8px}.chart-empty{height:74px;margin:14px 0 6px;display:grid;place-items:center;color:#a1adba;background:#f8fafb;border-radius:8px}.notice{margin-top:16px;padding:16px 18px;border-radius:12px;background:#f5fafc;color:#526b7a}.notice p{margin:6px 0 0;font-size:13px}.el-alert{margin-bottom:12px}.hint{margin:10px 0 0}
</style>
