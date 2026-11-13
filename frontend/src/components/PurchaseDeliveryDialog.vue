<template>
  <div>
    <el-dialog v-model="opened" :title="`工厂交货 · ${selected?.poNo||''}`" width="min(1080px,94vw)" top="5vh" class="delivery-dialog" append-to-body>
      <template v-if="selected">
        <el-descriptions :column="2"><el-descriptions-item label="供应商">{{selected.supplierName}}</el-descriptions-item><el-descriptions-item label="交接地点">{{deliveryDestination(selected)}}</el-descriptions-item></el-descriptions>
        <PurchaseQualityPanel v-if="opened" :order-id="selected.id" :allow-handling="false" @release-change="passed=$event"/>
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
          <el-button type="primary" :loading="saving" :disabled="!passed" @click="save">确认本次工厂交货</el-button>
        </template>
        <h3>工厂交货记录</h3>
        <el-table :data="receipts" size="small" empty-text="尚无交货记录">
          <el-table-column prop="receiptNo" label="交货记录" min-width="180"/><el-table-column label="实际交货日期" width="140"><template #default="{row}">{{row.actualDeliveryDate||row.receivedAt?.slice(0,10)||'—'}}</template></el-table-column><el-table-column prop="totalQty" label="数量" width="95"/><el-table-column prop="operatorName" label="登记人" width="140"/><el-table-column prop="remark" label="交接说明" min-width="180"/><el-table-column label="凭据" min-width="180"><template #default="{row}"><el-button v-for="file in row.proofFiles" :key="file.id" link type="primary" @click="previewProof(file)">{{file.fileName}}</el-button></template></el-table-column>
        </el-table>
      </template>
    </el-dialog>
    <FilePreviewDialog v-model="previewOpen" :source="previewBlob" :title="previewName" :content-type="previewBlob?.type"/>
  </div>
</template>
<script setup lang="ts">
import {computed,reactive,ref,watch} from 'vue'
import {ElMessage} from 'element-plus'
import {get,post,download,quietErrors} from '../api'
import {useAuthStore} from '../stores/auth'
import {hasPassedDeliveryInspection,type DeliveryQualityTask} from '../lib/deliveryQuality'
import {deliveryDestination} from '../lib/purchaseDelivery'
import PurchaseQualityPanel from './PurchaseQualityPanel.vue'
import FilePreviewDialog from './FilePreviewDialog.vue'
const props=defineProps<{modelValue:boolean;orderId:string;readOnly?:boolean}>()
const emit=defineEmits<{(event:'update:modelValue',value:boolean):void;(event:'saved'):void}>()
const opened=computed({get:()=>props.modelValue,set:value=>emit('update:modelValue',value)})
const auth=useAuthStore()
type Order={id:string;poNo:string;supplierName:string;expectedDate:string;status:string;fulfillmentMode:string;businessType:string;deliveryPortCode:string;deliveryPortName:string;deliveryAddress:string;closedAt:string;sourceBusinessId:string;contractVerifiedAt:string}
type Item={id:string;productName:string;spec:string;qty:string;receivedQty:string;uomCode:string}
type Proof={id:string;fileName:string}
type Receipt={id:string;receiptNo:string;actualDeliveryDate:string;receivedAt:string;totalQty:string;operatorName:string;remark:string;proofFiles:Proof[]}
const selected=ref<Order|null>(null),items=ref<Item[]>([]),receipts=ref<Receipt[]>([]),passed=ref(false),saving=ref(false),quantities=reactive<Record<string,string>>({}),actualDate=ref(''),remark=ref(''),proofFiles=ref<File[]>([])
const previewOpen=ref(false),previewBlob=ref<Blob|null>(null),previewName=ref('')
const canRegister=computed(()=>!props.readOnly&&auth.can('procurement:order:write')&&selected.value&&selected.value.fulfillmentMode!=='WAREHOUSE'&&!selected.value.closedAt&&['ORDERED','PARTIALLY_RECEIVED'].includes(selected.value.status))
let requestKey=''
const remaining=(item:Item)=>Math.max(0,Number(item.qty)-Number(item.receivedQty))
async function openOrder(order:Pick<Order,'id'>){const [d,q]=await Promise.all([get<{order:Order;items:Item[];receipts:Receipt[]}>(`/purchase-orders/${order.id}`),get<{tasks:DeliveryQualityTask[]}>(`/purchase-orders/${order.id}/quality-inspections`)]);selected.value=d.order;items.value=d.items||[];receipts.value=d.receipts||[];passed.value=hasPassedDeliveryInspection(q.tasks||[]);Object.keys(quantities).forEach(key=>delete quantities[key]);items.value.forEach(item=>quantities[item.id]=String(remaining(item)));actualDate.value=new Date().toLocaleDateString('en-CA');remark.value='';proofFiles.value=[];requestKey=crypto.randomUUID();opened.value=true}
function pickFiles(event:Event){const files=Array.from((event.target as HTMLInputElement).files||[]);if(files.length>5||files.reduce((sum,file)=>sum+file.size,0)>2*1024*1024){ElMessage.warning('最多5份凭据，合计不能超过2MB');(event.target as HTMLInputElement).value='';return}proofFiles.value=files}
async function encode(file:File){return new Promise<string>((resolve,reject)=>{const reader=new FileReader();reader.onload=()=>resolve(String(reader.result).split(',')[1]);reader.onerror=reject;reader.readAsDataURL(file)})}
async function save(){if(!selected.value||!passed.value||saving.value||!canRegister.value)return;if(!actualDate.value){ElMessage.warning('请选择实际交货日期');return}const lines=items.value.filter(item=>Number(quantities[item.id])>0).map(item=>({po_item_id:Number(item.id),qty:quantities[item.id]}));if(!lines.length){ElMessage.warning('请填写本次交货数量');return}saving.value=true;try{const proofs=await Promise.all(proofFiles.value.map(async file=>({file_name:file.name,content_type:file.type,file_data:await encode(file)})));const result=await post<{receiptNo:string}>(`/purchase-orders/${selected.value.id}/receive`,{warehouse_id:0,lines,remark:remark.value,actual_delivery_date:actualDate.value,proof_files:proofs,request_key:requestKey});ElMessage.success(`交货已登记：${result.receiptNo}`);opened.value=false;emit('saved')}finally{saving.value=false}}
async function previewProof(file:Proof){if(!selected.value)return;const result=await download(`/purchase-orders/${selected.value.id}/receipt-proofs/${file.id}/download`);previewName.value=file.fileName;previewBlob.value=result.blob;previewOpen.value=true}
watch(()=>[props.modelValue,props.orderId] as const,async([open,id])=>{if(open&&id){selected.value=null;await openOrder({id})}},{immediate:true})
</script>
<style scoped>
.entry{display:grid;grid-template-columns:300px 1fr;gap:20px;margin-top:18px}.proof-upload{display:flex;gap:12px;flex-wrap:wrap;font-size:13px;margin-bottom:15px}.proof-upload input{max-width:270px}:global(.delivery-dialog .el-dialog__body){max-height:calc(100vh - 160px);overflow:auto}
@media(max-width:700px){.entry{grid-template-columns:1fr}}
</style>
