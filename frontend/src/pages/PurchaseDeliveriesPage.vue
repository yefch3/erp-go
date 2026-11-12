<template>
  <div>
    <WorkflowPageHeader title="采购交货" description="物流在这里登记供应商实际交货。船期可以提前安排，交货必须整单质检合格。"/>
    <el-card shadow="never">
      <div class="filters"><el-radio-group v-model="tab" @change="load"><el-radio-button value="PENDING">待交货</el-radio-button><el-radio-button value="RECEIVED">已交货</el-radio-button></el-radio-group><el-input v-model="keyword" placeholder="采购单号 / 供应商" clearable @keyup.enter="load"/><el-button type="primary" @click="load">查询</el-button></div>
      <el-alert v-if="error" type="error" :closable="false" title="交货任务加载失败，请重新查询"/>
      <el-table v-else :data="pagedRows" v-loading="loading">
        <el-table-column prop="poNo" label="采购单号" min-width="140"/>
        <el-table-column prop="supplierName" label="供应商" min-width="115"/>
        <el-table-column label="交接地点" min-width="135" show-overflow-tooltip><template #default="{row}">{{deliveryDestination(row)}}</template></el-table-column>
        <el-table-column prop="expectedDate" label="采购要求日期" width="105"/>
        <el-table-column label="交货状态" width="85"><template #default="{row}"><el-tag>{{row.status==='ORDERED'?'未交货':t(deliveryStatusKey(row))}}</el-tag></template></el-table-column>
        <el-table-column label="质检" width="110"><template #default="{row}"><el-tag :type="quality[row.id]==='PASS'?'success':'warning'">{{qualityLabel(quality[row.id])}}</el-tag></template></el-table-column>
        <el-table-column label="操作" width="125" fixed="right"><template #default="{row}"><el-button link type="primary" @click="openOrder(row)">{{tab==='RECEIVED'?'查看交货记录':'查看 / 登记交货'}}</el-button></template></el-table-column>
      </el-table>
      <el-pagination class="pager" layout="total, prev, pager, next" :total="rows.length" :page-size="100" v-model:current-page="page"/>
    </el-card>
    <el-dialog v-model="opened" :title="`实际交货 · ${selected?.poNo||''}`" width="min(1080px,94vw)" top="5vh" class="delivery-dialog">
      <template v-if="selected">
        <el-descriptions :column="2"><el-descriptions-item label="供应商">{{selected.supplierName}}</el-descriptions-item><el-descriptions-item label="交接地点">{{deliveryDestination(selected)}}</el-descriptions-item></el-descriptions>
        <PurchaseQualityPanel v-if="opened" :order-id="selected.id" :allow-handling="false"/>
        <el-alert v-if="!passed" title="整单尚未质检合格，目前只能查看，不能登记交货。" type="warning" :closable="false"/>
        <el-table :data="items" size="small">
          <el-table-column prop="productName" label="产品" min-width="220"/>
          <el-table-column prop="spec" label="规格" min-width="150"/>
          <el-table-column label="采购数量" width="120" align="right"><template #default="{row}">{{row.qty}} {{row.uomCode}}</template></el-table-column>
          <el-table-column label="已交货" width="120" align="right"><template #default="{row}">{{row.receivedQty}} {{row.uomCode}}</template></el-table-column>
          <el-table-column v-if="canRegister" label="本次交货" width="150"><template #default="{row}"><el-input v-model="quantities[row.id]" :aria-label="`${row.productName} 本次交货`" :disabled="!passed||saving||remaining(row)<=0"/></template></el-table-column>
        </el-table>
        <template v-if="canRegister">
          <div class="entry"><el-form-item label="实际交货日期"><el-date-picker v-model="actualDate" type="date" value-format="YYYY-MM-DD" :disabled="!passed||saving"/></el-form-item><el-form-item label="交接说明"><el-input v-model="remark" :disabled="!passed||saving"/></el-form-item></div>
          <div class="proof-upload"><label>交接凭据（选填，最多5份、合计2MB） <input type="file" multiple :disabled="!passed||saving" @change="pickFiles"/></label><span v-if="proofFiles.length">{{proofFiles.map(file=>file.name).join('、')}}</span></div>
          <el-button type="primary" :loading="saving" :disabled="!passed" @click="save">登记实际交货</el-button>
        </template>
        <h3>交货记录</h3>
        <el-table :data="receipts" size="small" empty-text="尚无交货记录">
          <el-table-column prop="receiptNo" label="交货记录" min-width="180"/><el-table-column label="实际交货日期" width="140"><template #default="{row}">{{row.actualDeliveryDate||row.receivedAt?.slice(0,10)||'—'}}</template></el-table-column><el-table-column prop="totalQty" label="数量" width="95"/><el-table-column prop="operatorName" label="登记人" width="140"/><el-table-column prop="remark" label="交接说明" min-width="180"/><el-table-column label="凭据" min-width="180"><template #default="{row}"><el-button v-for="file in row.proofFiles" :key="file.id" link type="primary" @click="previewProof(file)">{{file.fileName}}</el-button></template></el-table-column>
        </el-table>
      </template>
    </el-dialog>
    <FilePreviewDialog v-model="previewOpen" :source="previewBlob" :title="previewName" :content-type="previewBlob?.type"/>
  </div>
</template>
<script setup lang="ts">
import {computed,onMounted,reactive,ref,onUnmounted,watch} from 'vue'
import {useRoute} from 'vue-router'
import {useI18n} from 'vue-i18n'
import {ElMessage} from 'element-plus'
import {get,post,download,quietErrors} from '../api'
import {useAuthStore} from '../stores/auth'
import {hasPassedDeliveryInspection,type DeliveryQualityTask} from '../lib/deliveryQuality'
import {deliveryDestination,deliveryStatusKey} from '../lib/purchaseDelivery'
import WorkflowPageHeader from '../components/WorkflowPageHeader.vue'
import PurchaseQualityPanel from '../components/PurchaseQualityPanel.vue'
import FilePreviewDialog from '../components/FilePreviewDialog.vue'
const auth=useAuthStore(),route=useRoute(),{t}=useI18n()
type Order={id:string;poNo:string;supplierName:string;expectedDate:string;status:string;fulfillmentMode:string;businessType:string;deliveryPortCode:string;deliveryPortName:string;deliveryAddress:string;closedAt:string;sourceBusinessId:string;contractVerifiedAt:string}
type Item={id:string;productName:string;spec:string;qty:string;receivedQty:string;uomCode:string}
type Proof={id:string;fileName:string}
type Receipt={id:string;receiptNo:string;actualDeliveryDate:string;receivedAt:string;totalQty:string;operatorName:string;remark:string;proofFiles:Proof[]}
const tab=ref('PENDING'),keyword=ref(String(route.query.keyword||'')),page=ref(1),rows=ref<Order[]>([]),quality=reactive<Record<string,string>>({}),loading=ref(false),error=ref(false)
const selected=ref<Order|null>(null),opened=ref(false),items=ref<Item[]>([]),receipts=ref<Receipt[]>([]),passed=ref(false),saving=ref(false),quantities=reactive<Record<string,string>>({}),actualDate=ref(''),remark=ref(''),proofFiles=ref<File[]>([])
const previewOpen=ref(false),previewBlob=ref<Blob|null>(null),previewName=ref('')
const pagedRows=computed(()=>rows.value.slice((page.value-1)*100,page.value*100)),canRegister=computed(()=>auth.can('procurement:receipt:write')&&selected.value&&!selected.value.closedAt&&['ORDERED','PARTIALLY_RECEIVED'].includes(selected.value.status))
let requestKey='',generation=0,alive=true
const remaining=(item:Item)=>Math.max(0,Number(item.qty)-Number(item.receivedQty))
const qualityLabel=(value:string)=>({PASS:'整单质检合格',BLOCKED:'尚未全部合格',NONE:'未质检',ERROR:'读取失败'}[value]||'读取中')
async function list(status:string){const result=await get<{orders:Order[];meta:{total:string}}>('/purchase-orders',{status,keyword:keyword.value,page_size:100});const out=result.orders||[];for(let page=2;page<=Math.ceil(Number(result.meta?.total||0)/100);page++){out.push(...((await get<{orders:Order[]}>('/purchase-orders',{status,keyword:keyword.value,page_size:100,page})).orders||[]))}return out}
async function load(){const current=++generation;loading.value=true;error.value=false;try{const groups=await Promise.all((tab.value==='RECEIVED'?['RECEIVED']:['ORDERED','PARTIALLY_RECEIVED']).map(list));if(!alive||current!==generation)return;rows.value=groups.flat().filter(order=>order.fulfillmentMode!=='WAREHOUSE'&&order.businessType!=='LOGISTICS'&&(tab.value==='RECEIVED'||!order.closedAt)&&(!Number(order.sourceBusinessId)||order.contractVerifiedAt));page.value=1;void loadQuality(current)}catch{if(alive&&current===generation){rows.value=[];error.value=true}}finally{if(alive&&current===generation)loading.value=false}}
async function loadQuality(current:number){for(let index=0;index<rows.value.length;index+=8){await Promise.all(rows.value.slice(index,index+8).map(async row=>{try{const d=await get<{tasks:DeliveryQualityTask[]}>(`/purchase-orders/${row.id}/quality-inspections`,{},quietErrors);if(alive&&current===generation)quality[row.id]=hasPassedDeliveryInspection(d.tasks||[])?'PASS':d.tasks?.length?'BLOCKED':'NONE'}catch{if(alive&&current===generation)quality[row.id]='ERROR'}}));if(current!==generation)return}}
async function openOrder(order:Order){const [d,q]=await Promise.all([get<{order:Order;items:Item[];receipts:Receipt[]}>(`/purchase-orders/${order.id}`),get<{tasks:DeliveryQualityTask[]}>(`/purchase-orders/${order.id}/quality-inspections`)]);selected.value=d.order;items.value=d.items||[];receipts.value=d.receipts||[];passed.value=hasPassedDeliveryInspection(q.tasks||[]);Object.keys(quantities).forEach(key=>delete quantities[key]);items.value.forEach(item=>quantities[item.id]=String(remaining(item)));actualDate.value=new Date().toLocaleDateString('en-CA');remark.value='';proofFiles.value=[];requestKey=crypto.randomUUID();opened.value=true}
function pickFiles(event:Event){const files=Array.from((event.target as HTMLInputElement).files||[]);if(files.length>5||files.reduce((sum,file)=>sum+file.size,0)>2*1024*1024){ElMessage.warning('最多5份凭据，合计不能超过2MB');(event.target as HTMLInputElement).value='';return}proofFiles.value=files}
async function encode(file:File){return new Promise<string>((resolve,reject)=>{const reader=new FileReader();reader.onload=()=>resolve(String(reader.result).split(',')[1]);reader.onerror=reject;reader.readAsDataURL(file)})}
async function save(){if(!selected.value||!passed.value||saving.value||!canRegister.value)return;if(!actualDate.value){ElMessage.warning('请选择实际交货日期');return}const lines=items.value.filter(item=>Number(quantities[item.id])>0).map(item=>({po_item_id:Number(item.id),qty:quantities[item.id]}));if(!lines.length){ElMessage.warning('请填写本次交货数量');return}saving.value=true;try{const proofs=await Promise.all(proofFiles.value.map(async file=>({file_name:file.name,content_type:file.type,file_data:await encode(file)})));const result=await post<{receiptNo:string}>(`/purchase-orders/${selected.value.id}/receive`,{warehouse_id:0,lines,remark:remark.value,actual_delivery_date:actualDate.value,proof_files:proofs,request_key:requestKey});ElMessage.success(`交货已登记：${result.receiptNo}`);opened.value=false;await load()}finally{saving.value=false}}
async function previewProof(file:Proof){if(!selected.value)return;const result=await download(`/purchase-orders/${selected.value.id}/receipt-proofs/${file.id}/download`);previewName.value=file.fileName;previewBlob.value=result.blob;previewOpen.value=true}
watch(()=>route.query.keyword,value=>{keyword.value=String(value||'');void load()})
onMounted(async()=>{await load();if(route.query.order){const d=await get<{order:Order}>(`/purchase-orders/${route.query.order}`);if(d.order.fulfillmentMode!=='WAREHOUSE')await openOrder(d.order)}})
onUnmounted(()=>{alive=false;generation++})
</script>
<style scoped>
.filters{display:flex;gap:12px;align-items:center;margin-bottom:16px}.filters>.el-input{width:260px}.pager{justify-content:flex-end;margin-top:16px}.entry{display:grid;grid-template-columns:300px 1fr;gap:20px;margin-top:18px}.proof-upload{display:flex;gap:12px;flex-wrap:wrap;font-size:13px;margin-bottom:15px}.proof-upload input{max-width:270px}:global(.delivery-dialog .el-dialog__body){max-height:calc(100vh - 160px);overflow:auto}
</style>
