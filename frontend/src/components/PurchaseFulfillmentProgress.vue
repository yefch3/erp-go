<template>
  <div class="fulfillment-progress" v-loading="loading">
    <div class="progress-toolbar"><div><p>{{t('purchaseFulfillment.autoHint')}}</p><span v-if="updated">{{t('purchaseFulfillment.updated')}} {{updated}}</span></div><el-button :loading="loading" @click="load">{{t('purchaseFulfillment.refresh')}}</el-button></div>
    <section class="progress-section">
      <div class="progress-heading"><h3>{{t('purchaseFulfillment.quality')}}</h3><el-tag v-if="!qualityError" :type="qualityPassed?'success':'warning'">{{qualityLabel}}</el-tag></div>
      <el-alert v-if="qualityError" :title="t('purchaseFulfillment.qualityError')" type="error" :closable="false"/>
      <el-table v-else :data="qualityLines" size="small" :empty-text="t('purchaseFulfillment.noQuality')">
        <el-table-column prop="taskNo" :label="t('quality.taskNo')" width="160"/>
        <el-table-column prop="productName" :label="t('orders.product')" min-width="200" show-overflow-tooltip/>
        <el-table-column :label="t('quality.requestedQty')" width="130" align="right"><template #default="{row}">{{row.requestedQty}} {{row.uomCode}}</template></el-table-column>
        <el-table-column :label="t('purchaseFulfillment.qualified')" width="120" align="right"><template #default="{row}">{{row.qualifiedQty}} {{row.uomCode}}</template></el-table-column>
        <el-table-column :label="t('purchaseFulfillment.unresolved')" width="130" align="right"><template #default="{row}">{{row.unresolvedQty}} {{row.uomCode}}</template></el-table-column>
        <el-table-column :label="t('quality.result')" width="110"><template #default="{row}">{{resultLabel(row.finalResult)}}</template></el-table-column>
      </el-table>
    </section>
    <section class="progress-section">
      <div class="progress-heading"><h3>{{t('purchaseFulfillment.delivery')}}</h3><el-tag v-if="!deliveryError" :type="deliveryState==='fullyDelivered'?'success':'info'">{{t(`purchaseFulfillment.${deliveryState}`)}}</el-tag></div>
      <el-alert v-if="deliveryError" :title="t('purchaseFulfillment.deliveryError')" type="error" :closable="false"/>
      <template v-else>
        <el-table :data="goods" size="small"><el-table-column prop="productName" :label="t('orders.product')" min-width="200"/><el-table-column prop="spec" :label="t('quality.sourceSpec')" min-width="140"/><el-table-column :label="t('purchaseFulfillment.quantity')" width="140" align="right"><template #default="{row}">{{row.qty}} {{row.uomCode}}</template></el-table-column><el-table-column :label="t('purchaseFulfillment.delivered')" width="140" align="right"><template #default="{row}">{{row.receivedQty}} {{row.uomCode}}</template></el-table-column><el-table-column :label="t('purchaseFulfillment.remaining')" width="140" align="right"><template #default="{row}">{{remaining(row)}} {{row.uomCode}}</template></el-table-column></el-table>
        <el-table :data="receipts" size="small" :empty-text="t('purchaseFulfillment.noReceipts')" class="receipt-history"><el-table-column prop="receiptNo" :label="t('purchaseFulfillment.receiptNo')" min-width="180"/><el-table-column :label="t('purchaseFulfillment.time')" width="190"><template #default="{row}">{{formatTime(row.receivedAt)}}</template></el-table-column><el-table-column prop="operatorName" :label="t('purchaseFulfillment.operator')" width="150"/><el-table-column prop="remark" :label="t('orders.remark')" min-width="170"/></el-table>
      </template>
    </section>
    <section class="progress-section">
      <div class="progress-heading"><h3>{{t('purchaseFulfillment.logistics')}}</h3></div><p class="progress-hint">{{t('purchaseFulfillment.logisticsHint')}}</p>
      <el-alert v-if="logisticsError" :title="t('purchaseFulfillment.logisticsError')" type="error" :closable="false"/>
      <el-table v-else :data="logistics" size="small" :empty-text="t('purchaseFulfillment.noLogistics')">
        <el-table-column prop="batchNo" :label="t('purchaseFulfillment.batch')" width="60"/>
        <el-table-column :label="t('purchaseFulfillment.schedule')" min-width="140"><template #default="{row}">{{row.scheduleNo||t('purchaseFulfillment.noSchedule')}}</template></el-table-column>
        <el-table-column :label="t('purchaseFulfillment.status')" width="120"><template #default="{row}">{{shippingLabel(row.scheduleStatus||row.status)}}</template></el-table-column>
        <el-table-column :label="t('purchaseFulfillment.route')" min-width="140"><template #default="{row}">{{row.portOfLoading||'—'}} → {{row.portOfDischarge||'—'}}</template></el-table-column>
        <el-table-column prop="etd" :label="t('purchaseFulfillment.etd')" width="110"/><el-table-column prop="eta" :label="t('purchaseFulfillment.eta')" width="110"/>
        <el-table-column :label="t('purchaseFulfillment.atd')" width="150"><template #default="{row}">{{formatTime(row.atd)}}</template></el-table-column><el-table-column :label="t('purchaseFulfillment.ata')" width="150"><template #default="{row}">{{formatTime(row.ata)}}</template></el-table-column>
      </el-table>
    </section>
  </div>
</template>
<script setup lang="ts">
import {computed,onUnmounted,ref,watch} from 'vue'
import {useI18n} from 'vue-i18n'
import {get,quietErrors} from '../api'
import {hasPassedDeliveryInspection,type DeliveryQualityTask} from '../lib/deliveryQuality'
const props=defineProps<{orderId:string}>()
const {t,te}=useI18n()
type QualityTask=DeliveryQualityTask&{taskNo:string;lines:Array<DeliveryQualityTask['lines'][number]&{productName:string;uomCode:string;unresolvedQty:string}>}
type Goods={productName:string;spec:string;qty:string;receivedQty:string;uomCode:string}
type Receipt={receiptNo:string;receivedAt:string;operatorName:string;remark:string}
type Logistics={id:string;batchNo:number;status:string;scheduleStatus:string;scheduleNo:string;portOfLoading:string;portOfDischarge:string;etd:string;eta:string;atd:string;ata:string}
const tasks=ref<QualityTask[]>([]),goods=ref<Goods[]>([]),receipts=ref<Receipt[]>([]),logistics=ref<Logistics[]>([])
const loading=ref(false),qualityError=ref(false),deliveryError=ref(false),logisticsError=ref(false),updated=ref('')
let generation=0,alive=true
const qualityPassed=computed(()=>hasPassedDeliveryInspection(tasks.value))
const qualityLines=computed(()=>tasks.value.flatMap(task=>task.lines.map(line=>({...line,taskNo:task.taskNo}))))
const qualityLabel=computed(()=>t(`purchaseFulfillment.${qualityPassed.value?'passed':!tasks.value.length?'uninspected':tasks.value.some(task=>task.status==='REINSPECTION'||task.procurementHandlingStatus==='PENDING')?'reinspection':tasks.value.some(task=>task.status==='IN_PROGRESS')?'inspecting':tasks.value.some(task=>task.status==='COMPLETED')?'failed':'pending'}`))
const deliveryState=computed(()=>goods.value.length&&goods.value.every(row=>Number(row.qty)>0&&Number(row.receivedQty)>=Number(row.qty))?'fullyDelivered':goods.value.some(row=>Number(row.receivedQty)>0)?'partiallyDelivered':'undelivered')
const remaining=(row:Goods)=>String(Math.max(0,Number(row.qty)-Number(row.receivedQty)))
function formatTime(value:string){if(!value)return '—';if(/^\d{4}-\d{2}-\d{2}$/.test(value))return value;const date=new Date(value.replace(' ','T').replace(/([+-]\d{2})$/,'$1:00'));return Number.isNaN(date.getTime())?value:date.toLocaleString()}
function resultLabel(value:string){const key=`quality.results.${value||'PENDING'}`;return te(key)?t(key):t('purchaseFulfillment.unknown')}
function shippingLabel(value:string){const key=`purchaseFulfillment.shippingStatuses.${value}`;return te(key)?t(key):t('purchaseFulfillment.unknown')}
async function load(){
 const id=props.orderId,request=++generation
 loading.value=true
 const [quality,delivery,shipping]=await Promise.allSettled([
  get<{tasks:QualityTask[]}>(`/purchase-orders/${id}/quality-inspections`,{},quietErrors),
  get<{items:Goods[];receipts:Receipt[]}>(`/purchase-orders/${id}`,{},quietErrors),
  get<{items:Logistics[]}>(`/purchase-orders/${id}/logistics-progress`,{},quietErrors)
 ])
 if(!alive||request!==generation)return
 qualityError.value=quality.status==='rejected';tasks.value=quality.status==='fulfilled'?quality.value.tasks??[]:[]
 deliveryError.value=delivery.status==='rejected';goods.value=delivery.status==='fulfilled'?delivery.value.items??[]:[];receipts.value=delivery.status==='fulfilled'?delivery.value.receipts??[]:[]
 logisticsError.value=shipping.status==='rejected';logistics.value=shipping.status==='fulfilled'?shipping.value.items??[]:[]
 updated.value=new Date().toLocaleTimeString();loading.value=false
}
watch(()=>props.orderId,()=>{tasks.value=[];goods.value=[];receipts.value=[];logistics.value=[];updated.value='';void load()},{immediate:true})
const timer=setInterval(()=>{if(!loading.value)void load()},15000)
onUnmounted(()=>{alive=false;generation++;clearInterval(timer)})
</script>
<style scoped>
.progress-toolbar,.progress-heading{display:flex;align-items:center;justify-content:space-between;gap:16px}.progress-toolbar p,.progress-hint{font-size:13px;color:var(--el-text-color-secondary);margin:0 0 6px}.progress-toolbar span{font-size:12px;color:var(--el-text-color-secondary)}.progress-section{margin-top:20px}.progress-heading{margin-bottom:10px}.progress-heading h3{font-size:16px;margin:0}.receipt-history{margin-top:12px}.fulfillment-progress :deep(.el-table :is(th,td).el-table__cell > .cell){text-align:left!important}.fulfillment-progress :deep(.el-table :is(th,td).el-table__cell.is-right > .cell){text-align:right!important}
</style>

