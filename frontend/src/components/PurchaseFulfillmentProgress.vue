<template>
 <div class="fulfillment-progress">
  <section class="fulfillment-overview" aria-label="整体进度">
   <div class="overview-cards">
    <div v-for="metric in metrics" :key="metric.key" class="overview-card" :class="metric.key"><span>{{metric.label}}</span><template v-if="metric.key==='quality'"><strong :class="quality.passed?'is-passed':'is-pending'" class="quality-value">{{quality.loading?'加载中':quality.failed?'加载失败':quality.status}}</strong></template><template v-else><strong v-for="row in totals" :key="row.unit">{{number(row[metric.key as 'ordered'|'delivered'|'remaining'])}} <small>{{row.unit}}</small></strong><strong v-if="!totals.length">—</strong></template></div>
   </div>
   <div class="overview-footer"><p role="status">{{nextAction}}</p><div class="progress-toolbar"><span v-if="updated">更新于 {{updated}} · 15秒自动刷新</span><el-button size="small" :loading="loading" @click="refreshAll">刷新进度</el-button></div></div>
  </section>
  <slot name="supplier" />
  <PurchaseQualityPanel ref="qualityPanel" :order-id="orderId" :can-request="canRequest" :auto-refresh="false" :show-refresh="false" @state-change="quality=$event" @request="emit('request')"/>
  <section class="progress-section delivery-section">
   <div class="progress-heading"><h3>{{isWarehouse?'实际入库':'实际交货'}}</h3><div class="delivery-actions"><el-tag v-if="!deliveryError" :type="deliveryState==='fullyDelivered'?'success':'info'">{{deliveryStateLabel}}</el-tag><el-button v-if="canManageDelivery&&deliveryState!=='fullyDelivered'" type="primary" :disabled="!quality.passed||quality.failed||quality.loading" @click="deliveryOpen=true">登记交货</el-button></div></div>
   <el-alert v-if="deliveryError" title="交货进度加载失败，请刷新重试" type="error" :closable="false"/>
   <template v-else>
    <p v-if="canManageDelivery&&deliveryState!=='fullyDelivered'&&!quality.passed" class="blocked-hint">{{quality.failed?'质检结果暂不可用，请刷新核对后再登记交货。':'整单质检尚未合格，暂不能登记交货。'}}</p>
    <el-table :data="goods" size="small">
     <el-table-column prop="productName" label="产品" min-width="230" show-overflow-tooltip/><el-table-column prop="spec" label="规格" min-width="140" show-overflow-tooltip/>
     <el-table-column label="采购数量" width="125" align="right"><template #default="{row}">{{number(row.qty)}} {{row.uomCode}}</template></el-table-column>
     <el-table-column :label="isWarehouse?'已入库':'已交货'" width="125" align="right"><template #default="{row}">{{number(row.receivedQty)}} {{row.uomCode}}</template></el-table-column>
     <el-table-column :label="isWarehouse?'待入库':'待交货'" width="125" align="right"><template #default="{row}">{{number(Math.max(0,Number(row.qty)-Number(row.receivedQty)))}} {{row.uomCode}}</template></el-table-column>
    </el-table>
    <div class="records-title">{{isWarehouse?'入库批次':'交货批次'}} <span>{{receipts.length}} 笔</span></div>
    <el-table :data="receipts" size="small" empty-text="尚未登记交货" class="receipt-history">
     <el-table-column prop="receiptNo" label="交货记录" min-width="175" show-overflow-tooltip/><el-table-column label="实际日期" width="115"><template #default="{row}">{{row.actualDeliveryDate||row.receivedAt?.slice(0,10)||'—'}}</template></el-table-column>
     <el-table-column label="数量" width="130" align="right"><template #default="{row}">{{number(row.totalQty)}} {{totals.length===1?totals[0].unit:'（多单位合计）'}}</template></el-table-column>
     <el-table-column prop="operatorName" label="登记人" min-width="130" show-overflow-tooltip/><el-table-column prop="remark" label="交接说明" min-width="150" show-overflow-tooltip/>
     <el-table-column label="凭据" min-width="150"><template #default="{row}"><el-button v-for="file in row.proofFiles" :key="file.id" link type="primary" class="proof-link" @click="previewProof(file)">{{file.fileName}}</el-button><span v-if="!row.proofFiles?.length">—</span></template></el-table-column>
    </el-table>
    <p class="progress-hint">{{isWarehouse?'由仓库实际收货记录更新。':'以采购登记的工厂实际交货数量为准。'}}</p>
   </template>
  </section>
  <PurchaseDeliveryDialog v-model="deliveryOpen" :order-id="orderId" @saved="deliverySaved"/>
  <FilePreviewDialog v-model="previewOpen" :source="previewBlob" :title="previewName" :content-type="previewBlob?.type"/>
 </div>
</template>
<script setup lang="ts">
import {computed,onUnmounted,ref,watch} from 'vue'
import {useI18n} from 'vue-i18n'
import {ElMessage} from 'element-plus'
import {get,download,quietErrors} from '../api'
import {useAuthStore} from '../stores/auth'
import PurchaseDeliveryDialog from './PurchaseDeliveryDialog.vue'
import PurchaseQualityPanel from './PurchaseQualityPanel.vue'
import FilePreviewDialog from './FilePreviewDialog.vue'
import {deliveryStatusKey} from '../lib/purchaseDelivery'
import {fulfillmentTotals,fulfillmentNumber as number} from '../lib/fulfillmentOverview'
const props=defineProps<{orderId:string;canRequest?:boolean}>()
const emit=defineEmits<{(event:'request'):void;(event:'delivery-saved'):void}>()
const {t}=useI18n(),auth=useAuthStore()
type Goods={productName:string;spec:string;qty:string;receivedQty:string;uomCode:string}
type Proof={id:string;fileName:string}
type Receipt={receiptNo:string;receivedAt:string;actualDeliveryDate:string;totalQty:string;operatorName:string;remark:string;proofFiles:Proof[]}
const goods=ref<Goods[]>([]),receipts=ref<Receipt[]>([])
const loading=ref(false),deliveryError=ref(false),updated=ref('')
const qualityPanel=ref<InstanceType<typeof PurchaseQualityPanel>|null>(null),quality=ref({passed:false,status:'加载中',failed:false,loading:true})
const totals=computed(()=>deliveryError.value?[]:fulfillmentTotals(goods.value))
const outstanding=computed(()=>totals.value.filter(row=>row.remaining>0).map(row=>`${number(row.remaining)} ${row.unit}`).join(' / '))
const isWarehouse=ref(false),deliveryOpen=ref(false),orderStatus=ref(''),closedAt=ref('')
const metrics=computed(()=>[{key:'ordered',label:'采购总量'},{key:'quality',label:'质检状态'},{key:'delivered',label:isWarehouse.value?'已入库':'已交货'},{key:'remaining',label:isWarehouse.value?'待入库':'待交货'}])
let generation=0,alive=true
const deliveryState=computed(()=>goods.value.length&&goods.value.every(row=>Number(row.qty)>0&&Number(row.receivedQty)>=Number(row.qty))?'fullyDelivered':goods.value.some(row=>Number(row.receivedQty)>0)?'partiallyDelivered':'undelivered')
const canManageDelivery=computed(()=>!deliveryError.value&&!isWarehouse.value&&auth.can('procurement:order:write')&&!closedAt.value&&['ORDERED','PARTIALLY_RECEIVED','RECEIVED'].includes(orderStatus.value))
const nextAction=computed(()=>{
 if(loading.value||quality.value.loading)return '正在核对采购、质检和交货进度…'
 if(deliveryError.value||quality.value.failed)return '部分进度加载失败，请刷新核对后继续。'
 if(closedAt.value||orderStatus.value==='CLOSED')return '采购单已结案，以下为履约记录。'
 if(orderStatus.value==='CANCELLED')return '采购单已取消，以下为历史记录。'
 if(!goods.value.length)return '暂无采购明细。'
 if(!quality.value.passed)return `${quality.value.status}，整单质检合格后才能${isWarehouse.value?'入库':'登记交货'}。`
 if(!outstanding.value)return `${isWarehouse.value?'全部入库':'全部交货'}已完成，可查看下方批次记录。`
 return `质检已合格，剩余 ${outstanding.value} 待${isWarehouse.value?'仓库登记入库':'采购登记交货'}。`
})
const deliveryStateLabel=computed(()=>deliveryState.value==='undelivered'?t('purchaseFulfillment.undelivered'):t(deliveryStatusKey({status:deliveryState.value==='fullyDelivered'?'RECEIVED':'PARTIALLY_RECEIVED',fulfillmentMode:isWarehouse.value?'WAREHOUSE':'DIRECT_SHIP'})))
async function deliverySaved(){await refreshAll();emit('delivery-saved')}
const previewOpen=ref(false),previewBlob=ref<Blob|null>(null),previewName=ref('')
async function previewProof(file:Proof){try{const result=await download(`/purchase-orders/${props.orderId}/receipt-proofs/${file.id}/download`);previewName.value=file.fileName;previewBlob.value=result.blob;previewOpen.value=true}catch{ElMessage.error('交货凭据读取失败，请重试')}}
async function refreshAll(){await Promise.all([load(),qualityPanel.value?.refresh()])}
async function load(){
 const id=props.orderId,request=++generation;loading.value=true
 try{
  const delivery=await get<{order:{fulfillmentMode:string;status:string;closedAt:string};items:Goods[];receipts:Receipt[]}>(`/purchase-orders/${id}`,{},quietErrors)
  if(!alive||request!==generation)return
  orderStatus.value=delivery.order.status;closedAt.value=delivery.order.closedAt
  isWarehouse.value=delivery.order.fulfillmentMode==='WAREHOUSE';deliveryError.value=false
  goods.value=delivery.items??[];receipts.value=delivery.receipts??[]
 }catch{
  if(!alive||request!==generation)return
  deliveryError.value=true;goods.value=[];receipts.value=[];orderStatus.value='';closedAt.value=''
 }finally{
  if(alive&&request===generation){updated.value=new Date().toLocaleTimeString('zh-CN',{hour12:false});loading.value=false}
 }
}
watch(()=>props.orderId,()=>{goods.value=[];receipts.value=[];updated.value='';quality.value={passed:false,status:'加载中',failed:false,loading:true};previewOpen.value=false;void load()},{immediate:true})
const timer=setInterval(()=>{if(!loading.value)void refreshAll()},15000)
onUnmounted(()=>{alive=false;generation++;clearInterval(timer)})
</script>
<style scoped>
.fulfillment-overview{margin:0 0 14px;border:1px solid #d7eaf5;border-radius:8px;background:#f6fbff;overflow:hidden}.overview-cards{display:grid;grid-template-columns:repeat(4,minmax(0,1fr))}.overview-card{padding:14px 18px;border-right:1px solid #dfebf3;display:flex;flex-direction:column;gap:6px}.overview-card:last-child{border:0}.overview-card>span{font-size:13px;color:var(--el-text-color-secondary)}.overview-card strong{font-size:24px;line-height:1.35;color:#183c52;font-variant-numeric:tabular-nums}.overview-card small{font-size:13px;font-weight:400}.overview-card .quality-value{font-size:19px}.overview-card .is-passed{color:var(--el-color-success-dark-2)}.overview-card .is-pending{color:var(--el-color-warning-dark-2)}.remaining strong{color:#087da9}.overview-footer{border-top:1px solid #dfebf3;padding:9px 14px;display:flex;align-items:center;justify-content:space-between;gap:12px}.overview-footer p{margin:0;font-size:13px;font-weight:500;color:#254f67}.progress-toolbar,.progress-heading,.delivery-actions{display:flex;align-items:center;gap:12px}.progress-toolbar{flex-shrink:0}.progress-toolbar span,.progress-hint{font-size:12px;color:var(--el-text-color-secondary)}.progress-section{border:1px solid var(--el-border-color-light);border-radius:8px;padding:14px;margin-top:14px}.progress-heading{justify-content:space-between;margin-bottom:10px}.progress-heading h3{font-size:16px;margin:0;color:var(--el-text-color-primary)}.progress-hint{margin:10px 0 0;line-height:1.5}.blocked-hint{font-size:13px;color:var(--el-color-warning-dark-2);margin:0 0 10px}.records-title{margin:14px 0 6px;font-size:13px;font-weight:600}.records-title span{font-weight:400;color:var(--el-text-color-secondary);margin-left:8px}.proof-link{max-width:100%;height:auto;white-space:normal;overflow-wrap:anywhere;line-height:1.5}.fulfillment-progress :deep(.el-table .cell){line-height:22px}.fulfillment-progress :deep(.el-table__cell){padding:6px 0}.fulfillment-progress :deep(.el-table th.el-table__cell){color:#294551}
@media(max-width:850px){.overview-footer{align-items:flex-start;flex-direction:column}.overview-card{padding:12px}}
@media(max-width:560px){.overview-cards{grid-template-columns:repeat(2,minmax(0,1fr))}.overview-card{border-bottom:1px solid #dfebf3}.progress-toolbar{flex-wrap:wrap}}
</style>
