<template>
  <div class="fulfillment-progress" v-loading="loading">
    <div class="progress-toolbar"><div><p>{{t('purchaseFulfillment.autoHint')}}</p><span v-if="updated">{{t('purchaseFulfillment.updated')}} {{updated}}</span></div><el-button :loading="loading" @click="load">{{t('purchaseFulfillment.refresh')}}</el-button></div>
    <PurchaseQualityPanel :order-id="orderId" :can-request="canRequest" @request="emit('request')" />
    <section class="progress-section">
      <div class="progress-heading"><h3>{{t('purchaseFulfillment.deliverySummary')}}</h3><div class="delivery-actions"><el-tag v-if="!deliveryError">{{deliveryStateLabel}}</el-tag><el-button v-if="canManageDelivery" type="primary" @click="deliveryOpen=true">{{deliveryState==='fullyDelivered'?'查看交货记录':'登记工厂交货'}}</el-button></div></div>
      <el-alert v-if="deliveryError" :title="t('purchaseFulfillment.deliveryError')" type="error" :closable="false"/>
      <div v-else class="delivery-brief"><p v-for="(row,index) in goods" :key="index"><strong>{{row.productName}}</strong><span>{{isWarehouse?'已入库':'已交货'}} {{row.receivedQty}} / {{row.qty}} {{row.uomCode}}</span></p><p class="progress-hint">{{isWarehouse?'由仓库实际收货记录更新。':'由采购确认工厂交货后更新；安排船期或离港到港不会增加交货数量。'}}</p></div>
    </section>
    <section class="progress-section">
      <div class="progress-heading"><h3>{{t('purchaseFulfillment.transportSummary')}}</h3></div>
      <el-alert v-if="logisticsError" :title="t('purchaseFulfillment.logisticsError')" type="error" :closable="false"/>
      <template v-else><p v-if="!logistics.length" class="progress-hint">{{t('purchaseFulfillment.noLogistics')}}</p><p v-for="item in logistics" :key="item.id" class="transport-brief">第 {{item.batchNo}} 批 · {{shippingLabel(item.scheduleStatus||item.status)}} · {{item.portOfLoading||'—'}} → {{item.portOfDischarge||'—'}}</p></template>
      <p class="progress-hint">船期、发运、离港和到港由物流操作，此处仅查看摘要。</p>
    </section>
    <PurchaseDeliveryDialog v-model="deliveryOpen" :order-id="orderId" @saved="deliverySaved"/>
  </div>
</template>
<script setup lang="ts">
import {computed,onUnmounted,ref,watch} from 'vue'
import {useI18n} from 'vue-i18n'
import {get,quietErrors} from '../api'
import {useAuthStore} from '../stores/auth'
import PurchaseDeliveryDialog from './PurchaseDeliveryDialog.vue'
import PurchaseQualityPanel from './PurchaseQualityPanel.vue'
import {deliveryStatusKey} from '../lib/purchaseDelivery'
const props=defineProps<{orderId:string;canRequest?:boolean}>()
const emit=defineEmits<{(event:'request'):void;(event:'delivery-saved'):void}>()
const {t,te}=useI18n()
type Goods={productName:string;spec:string;qty:string;receivedQty:string;uomCode:string}
type Receipt={receiptNo:string;receivedAt:string;operatorName:string;remark:string}
type Logistics={id:string;batchNo:number;status:string;scheduleStatus:string;scheduleNo:string;portOfLoading:string;portOfDischarge:string;etd:string;eta:string;atd:string;ata:string}
const goods=ref<Goods[]>([]),receipts=ref<Receipt[]>([]),logistics=ref<Logistics[]>([])
const loading=ref(false),deliveryError=ref(false),logisticsError=ref(false),updated=ref('')
let generation=0,alive=true
const deliveryState=computed(()=>goods.value.length&&goods.value.every(row=>Number(row.qty)>0&&Number(row.receivedQty)>=Number(row.qty))?'fullyDelivered':goods.value.some(row=>Number(row.receivedQty)>0)?'partiallyDelivered':'undelivered')
const isWarehouse=ref(false),deliveryOpen=ref(false),orderStatus=ref(''),closedAt=ref('')
const auth=useAuthStore()
const canManageDelivery=computed(()=>!deliveryError.value&&!isWarehouse.value&&auth.can('procurement:order:write')&&!closedAt.value&&['ORDERED','PARTIALLY_RECEIVED','RECEIVED'].includes(orderStatus.value))
async function deliverySaved(){await load();emit('delivery-saved')}
const deliveryStateLabel=computed(()=>deliveryState.value==='undelivered'?'尚未交付':t(deliveryStatusKey({status:deliveryState.value==='fullyDelivered'?'RECEIVED':'PARTIALLY_RECEIVED',fulfillmentMode:isWarehouse.value?'WAREHOUSE':'DIRECT_SHIP'})))
const remaining=(row:Goods)=>String(Math.max(0,Number(row.qty)-Number(row.receivedQty)))
function formatTime(value:string){if(!value)return '—';if(/^\d{4}-\d{2}-\d{2}$/.test(value))return value;const date=new Date(value.replace(' ','T').replace(/([+-]\d{2})$/,'$1:00'));return Number.isNaN(date.getTime())?value:date.toLocaleString()}
function shippingLabel(value:string){const key=`purchaseFulfillment.shippingStatuses.${value}`;return te(key)?t(key):t('purchaseFulfillment.unknown')}
async function load(){
 const id=props.orderId,request=++generation
 loading.value=true
 const [delivery,shipping]=await Promise.allSettled([
  get<{order:{fulfillmentMode:string;status:string;closedAt:string};items:Goods[];receipts:Receipt[]}>(`/purchase-orders/${id}`,{},quietErrors),
  get<{items:Logistics[]}>(`/purchase-orders/${id}/logistics-progress`,{},quietErrors)
 ])
 if(!alive||request!==generation)return
 orderStatus.value=delivery.status==='fulfilled'?delivery.value.order.status:'';closedAt.value=delivery.status==='fulfilled'?delivery.value.order.closedAt:''
 isWarehouse.value=delivery.status==='fulfilled'&&delivery.value.order.fulfillmentMode==='WAREHOUSE';deliveryError.value=delivery.status==='rejected';goods.value=delivery.status==='fulfilled'?delivery.value.items??[]:[];receipts.value=delivery.status==='fulfilled'?delivery.value.receipts??[]:[]
 logisticsError.value=shipping.status==='rejected';logistics.value=shipping.status==='fulfilled'?shipping.value.items??[]:[]
 updated.value=new Date().toLocaleTimeString();loading.value=false
}
watch(()=>props.orderId,()=>{goods.value=[];receipts.value=[];logistics.value=[];updated.value='';void load()},{immediate:true})
const timer=setInterval(()=>{if(!loading.value)void load()},15000)
onUnmounted(()=>{alive=false;generation++;clearInterval(timer)})
</script>
<style scoped>
.delivery-actions{display:flex;align-items:center;gap:12px}.delivery-brief p{display:flex;justify-content:space-between;gap:16px;font-size:13px}.delivery-brief strong{font-weight:500}.transport-brief{font-size:13px;color:var(--el-text-color-regular)}

.progress-toolbar,.progress-heading{display:flex;align-items:center;justify-content:space-between;gap:16px}.progress-toolbar p,.progress-hint{font-size:13px;color:var(--el-text-color-secondary);margin:0 0 6px}.progress-toolbar span{font-size:12px;color:var(--el-text-color-secondary)}.progress-section{margin-top:20px}.progress-heading{margin-bottom:10px}.progress-heading h3{font-size:16px;margin:0}.receipt-history{margin-top:12px}.fulfillment-progress :deep(.el-table :is(th,td).el-table__cell > .cell){text-align:left!important}.fulfillment-progress :deep(.el-table :is(th,td).el-table__cell.is-right > .cell){text-align:right!important}
</style>

