<template>
 <el-dialog v-model="visible" class="contract-entry-dialog" :class="{'contract-entry-dialog--new':!history}" :title="history?'补录历史合同':'新建合同'" :width="history?'min(720px,96vw)':'min(1100px,96vw)'" top="4vh" :close-on-click-modal="false">
  <el-alert v-if="history" :closable="false" show-icon title="保存原合同资料和附件，无需重复录入产品明细。合同金额仅作记录，不代表未收款金额。"/>
  <div v-if="history&&drafts.length" class="drafts"><span>继续填写历史草稿</span><el-select placeholder="选择已保存草稿" @change="openDraft" style="width:320px"><el-option v-for="d in drafts" :key="d.id" :value="d.id" :label="`${d.body.externalContractNo||'未填原合同号'} · ${d.body.customerName||'未选客户'} · ${d.updatedAt.slice(0,16)}`"/></el-select><el-button v-if="draftId" type="danger" link @click="removeDraft">删除当前草稿</el-button></div>
  <el-alert v-if="entryError" class="entry-error" type="error" show-icon :closable="false" :title="entryError"/>
  <nav v-if="!history" aria-label="新建合同表单分区" class="entry-nav">
    <el-button text @click="tradeSection?.scrollIntoView({block:'start'})">客户与交易条件</el-button>
    <el-button text @click="partiesSection?.scrollIntoView({block:'start'})">合同双方</el-button>
    <el-button text @click="productsSection?.scrollIntoView({block:'start'})">产品明细</el-button>
  </nav>
  <section ref="tradeSection" :class="{'entry-section':!history}">
  <div v-if="!history" class="entry-section-heading">
    <h3>客户与交易条件</h3>
    <small class="entry-meta">负责人：{{auth.employeeName||'当前登录销售'}} · 合同来源：手工新建合同 · 来源报价：无</small>
  </div>
  <el-form label-position="top" :class="['entry-form',history?'history-form':'basic-form']">
   <el-form-item label="客户" required><el-select v-model="form.customerId" filterable remote :remote-method="searchCustomers" placeholder="选择客户" @change="selectCustomer"><el-option v-for="c in customers" :key="c.id" :value="String(c.id)" :label="customerOptionLabel(c)"/></el-select></el-form-item>
   <template v-if="history">
    <el-form-item label="原合同号" required><el-input v-model="form.externalContractNo" placeholder="按原合同填写" maxlength="100"/></el-form-item>
    <el-form-item label="合同金额" required><el-input v-model="form.totalAmount" inputmode="decimal" placeholder="原合同总金额"/></el-form-item>
    <el-form-item label="币种" required><el-select v-model="form.currency"><el-option v-for="c in CURRENCIES" :key="c" :value="c" :label="c"/></el-select></el-form-item>
    <el-form-item label="签订日期" required><el-date-picker v-model="form.signedDate" type="date" value-format="YYYY-MM-DD"/></el-form-item>
    <el-form-item label="合同状态" required><el-select v-model="form.historyStatus"><el-option value="EXECUTING" label="执行中"/><el-option value="COMPLETED" label="已完成"/></el-select></el-form-item>
   </template>
   <template v-else>
   <el-form-item label="客户联系人"><el-autocomplete v-model="form.customerContact" :fetch-suggestions="suggestContacts" placeholder="可选择联系人，也可直接填写" clearable /></el-form-item>
   <el-form-item label="客户合同币种" required><el-select v-model="form.currency"><el-option v-for="c in CURRENCIES" :key="c" :value="c" :label="c"/></el-select></el-form-item>
   <el-form-item label="原／客户合同号"><el-input v-model="form.externalContractNo"/></el-form-item>
   </template>
  </el-form>
  <el-form v-if="!history" label-position="top" class="entry-form trade-form">
   <el-form-item :label="t('contractRepair.deliveryLabel')"><el-date-picker v-model="form.deliveryDate" type="date" value-format="YYYY-MM-DD"/></el-form-item>
   <el-form-item label="贸易条款"><el-select v-model="form.incoterm" filterable allow-create default-first-option><el-option v-for="v in ['FOB','CFR','CIF','EXW','FCA','DAP','DDP']" :key="v" :value="v" :label="v"/></el-select></el-form-item>
   <el-form-item label="付款条件"><el-input v-model="form.paymentMethod" placeholder="按客户合同填写"/></el-form-item>
   <el-form-item label="应收到账日"><el-date-picker v-model="form.receivableDueDate" type="date" value-format="YYYY-MM-DD" clearable/></el-form-item>
   <el-form-item label="装运港"><el-input v-model="form.portOfLoading"/></el-form-item>
   <el-form-item label="目的港"><el-input v-model="form.portOfDischarge"/></el-form-item>
  </el-form>
  </section>
  <section v-if="!history" ref="partiesSection" class="entry-section entry-parties-section">
    <ContractPartiesFields :model-value="form" @update:model-value="Object.assign(form,$event)" />
  </section>
  <section v-if="!history" ref="productsSection" class="entry-section">
  <div class="entry-section-heading"><h3>产品明细</h3><div class="line-actions"><el-button size="small" :disabled="busy" @click="importOpen=true">Excel 导入</el-button><el-button size="small" type="primary" plain @click="addLine">添加产品</el-button></div></div>
  <ColumnFillRegion :rows="form.items" :fields="[{key:'productName',column:0},{key:'spec',column:0},{key:'qty',column:1},{key:'uomCode',column:2},{key:'unitPrice',column:3}]" :editable="!busy">
  <el-table :data="form.items" border max-height="340" size="small" class="entry-lines-table">
   <el-table-column :label="t('inquiryWorkspace.offer.productDescription')" min-width="290">
    <template #default="{row}">
     <div class="entry-product-description">
      <div data-column-fill-field="productName"><el-input v-model="row.productName" size="small" placeholder="产品名称" aria-label="产品名称"/></div>
      <div data-column-fill-field="spec"><el-input v-model="row.spec" size="small" placeholder="规格" aria-label="规格"/></div>
     </div>
    </template>
   </el-table-column>
   <el-table-column label="数量" width="92" align="right"><template #default="{row}"><el-input v-model="row.qty" size="small" inputmode="decimal" aria-label="数量" class="numeric-input"/></template></el-table-column>
   <el-table-column label="单位" width="76"><template #default="{row}"><el-input v-model="row.uomCode" size="small" aria-label="单位"/></template></el-table-column>
   <el-table-column label="对客单价" width="105" align="right"><template #default="{row}"><el-input v-model="row.unitPrice" size="small" inputmode="decimal" aria-label="对客单价" class="numeric-input"/></template></el-table-column>
   <el-table-column label="金额" width="106" align="right"><template #default="{row}">{{amount(row)}}</template></el-table-column>
   <el-table-column label="操作" width="60" align="center"><template #default="{$index}"><el-button size="small" link type="danger" @click="form.items.splice($index,1)">删除</el-button></template></el-table-column>
  </el-table>
  </ColumnFillRegion>
  <div class="entry-total">客户合同合计：{{total}} {{form.currency}}</div>
  </section>
  <el-form label-position="top" :class="{'entry-section entry-notes':!history}"><el-form-item :label="history?'备注':'合同条款／备注'"><el-input v-model="form.terms" type="textarea" :rows="history?3:2"/></el-form-item><el-form-item v-if="history" label="原合同附件（PDF）" required><input ref="fileInput" type="file" accept="application/pdf,.pdf" hidden @change="pickFile"/><el-button @click="fileInput?.click()">{{file||form.signedFileName?'更换附件':'选择合同文件'}}</el-button><span class="file-name">{{file?.name||form.signedFileName||'尚未选择文件'}}</span><small class="file-hint">保存草稿时可以暂缺文件，已上传的文件会随草稿保留。</small></el-form-item></el-form>
  <ContractLineImportDialog v-if="!history" v-model="importOpen" @import="appendImported"/>
  <template #footer><el-button :disabled="busy" @click="visible=false">关闭</el-button><ContractOperationButton v-if="history" label="保存草稿" description="资料未填完时先保存，不建立正式合同或下游单据，下次可继续填写。" :loading="busy" @click="saveHistoryDraft"/><ContractOperationButton :label="history?'保存历史合同':'保存合同草稿'" :description="history?'保存合同资料与原文件，供查询和关联。不补走历史审批，不自动产生采购、物流或应收款。':'保存后由负责销售提交上级确认。签署并经财务放行前，不会建立采购或物流任务。'" type="primary" :loading="busy" @click="save"/></template>
 </el-dialog>
</template>
<script setup lang="ts">
import {computed,reactive,ref,watch} from 'vue'
import {useI18n} from 'vue-i18n'
import {contractLineAmount,contractTotal} from '../lib/contractEditing'
import {ElMessage,ElMessageBox} from 'element-plus'
import {get,post,quietErrors} from '../api'
import {isDialogDismissed} from '../lib/dialogActions'
import {CURRENCIES} from '../constants'
import {customerOptionLabel} from '../lib/customerDisplay'
import {newIdempotencySession,withIdempotency} from '../lib/idempotency'
import ContractLineImportDialog from './ContractLineImportDialog.vue'
import type {ContractImportLine} from '../lib/contractLineImport'
import ContractOperationButton from './ContractOperationButton.vue'
import ColumnFillRegion from './ColumnFillRegion.vue'
import ContractPartiesFields from './ContractPartiesFields.vue'
import {emptyContractParties,buyerFromCustomer} from '../lib/contractParties'
import {useAuthStore} from '../stores/auth'
const {t}=useI18n()
const auth=useAuthStore()
const props=defineProps<{modelValue:boolean;history:boolean}>()
const emit=defineEmits<{ 'update:modelValue':[boolean];saved:[string] }>()
const visible=computed({get:()=>props.modelValue,set:v=>emit('update:modelValue',v)})
const tradeSection=ref<HTMLElement|null>(null)
const partiesSection=ref<HTMLElement|null>(null)
const productsSection=ref<HTMLElement|null>(null)
type Line={productId:string;productName:string;spec:string;qty:string;uomCode:string;unitPrice:string}
const initial=()=>({...emptyContractParties(),customerContact:'',totalAmount:'',historyStatus:'EXECUTING',customerId:'',customerName:'',currency:'USD',externalContractNo:'',signedDate:'',effectiveDate:'',takeoverDate:'',deliveryDate:'',receivableDueDate:'',incoterm:'FOB',paymentMethod:'',portOfLoading:'',portOfDischarge:'',terms:'',signedFileKey:'',signedFileName:'',items:[] as Line[]})
const form=reactive(initial()),busy=ref(false),customers=ref<any[]>([]),draftId=ref(''),revision=ref(0),file=ref<File|null>(null),fileInput=ref<HTMLInputElement|null>(null),drafts=ref<{id:string;revision:number;body:ReturnType<typeof initial>;updatedAt:string}[]>([])
const importOpen=ref(false),entryError=ref('')
function showEntryError(error:unknown){if(isDialogDismissed(error))return;entryError.value=error&&typeof error==='object'&&'message' in error?String(error.message):'操作失败，请稍后重试；已填写的资料仍保留。'}
function appendImported(lines:ContractImportLine[]){form.items=form.items.filter(l=>[l.productName,l.spec,l.qty,l.uomCode,l.unitPrice].some(v=>v.trim()));form.items.push(...lines);ElMessage.success(`已追加 ${lines.length} 条产品明细，请核对后保存合同`)}
const idem=newIdempotencySession()
function addLine(){form.items.push({productId:'0',productName:'',spec:'',qty:'',uomCode:'',unitPrice:''})}
function amount(l:Line){return contractLineAmount(l)}
const total=computed(()=>contractTotal(form.items))
async function searchCustomers(keyword:string){try{customers.value=(await get<{customers:any[]}>('/customers',{keyword,page_size:50},quietErrors)).customers||[]}catch(error){showEntryError(error)}}
let selectedCustomerId='',lastBuyer=buyerFromCustomer()
const contactOptions=ref<{value:string;isPrimary:boolean}[]>([])
function suggestContacts(query:string,callback:(options:{value:string}[])=>void){callback(contactOptions.value.filter(c=>c.value.toLowerCase().includes(query.trim().toLowerCase())))}
async function loadContacts(id:string){
 contactOptions.value=[];form.customerContact=''
 if(!id)return
 try{
  const result=await get<{contacts:{name:string;isPrimary:boolean}[]}>(`/customers/${id}/contacts`,undefined,quietErrors)
  if(form.customerId!==id)return
  contactOptions.value=(result.contacts||[]).filter(c=>c.name?.trim()).map(c=>({value:c.name.trim(),isPrimary:c.isPrimary}))
  const preferred=contactOptions.value.find(c=>c.isPrimary)||(contactOptions.value.length===1?contactOptions.value[0]:undefined)
  if(!form.customerContact&&preferred)form.customerContact=preferred.value
 }catch{if(form.customerId===id)ElMessage.info('联系人列表暂时无法读取，可以直接填写客户联系人')}
}
async function selectCustomer(){
 const id=form.customerId,c=customers.value.find(c=>String(c.id)===id)
 if(!props.history&&selectedCustomerId&&id!==selectedCustomerId&&(form.buyerName!==lastBuyer.buyerName||form.buyerAddress!==lastBuyer.buyerAddress)&&(form.buyerName.trim()||form.buyerAddress.trim())){
  try{await ElMessageBox.confirm('切换客户将重新带入买方公司名称和地址，替换本合同中已修改的买方信息。是否继续？','切换签约客户',{confirmButtonText:'切换客户',cancelButtonText:'保留当前客户'})}catch{form.customerId=selectedCustomerId;return}
 }
 selectedCustomerId=id;form.customerName=c?customerOptionLabel(c):''
 if(!props.history){lastBuyer=buyerFromCustomer(c);Object.assign(form,lastBuyer);await loadContacts(id)}
}
async function loadEntryDefaults(){const defaults=await get<{sellerName:string;sellerAddress:string}>('/contracts/entry-defaults',undefined,quietErrors);if(!visible.value)return;if(!form.sellerName)form.sellerName=defaults.sellerName||'';if(!form.sellerAddress)form.sellerAddress=defaults.sellerAddress||''}
function pickFile(e:Event){file.value=(e.target as HTMLInputElement).files?.[0]||null}
async function uploadFile(){if(!file.value)return;const f=file.value;const signed=await post<{fileKey:string;uploadUrl:string}>('/contracts/existing/files/presign',{fileName:f.name,contentType:f.type||'application/pdf'},quietErrors);const r=await fetch(signed.uploadUrl,{method:'PUT',headers:{'Content-Type':f.type||'application/pdf'},body:f});if(!r.ok)throw new Error('签署文件上传失败');form.signedFileKey=signed.fileKey;form.signedFileName=f.name;file.value=null}
async function loadDrafts(){drafts.value=await post('/contract-history-drafts',{action:'history_list'},quietErrors)}
function openDraft(id:string){const d=drafts.value.find(d=>d.id===id);if(!d||busy.value)return;entryError.value='';idem.reset();Object.assign(form,initial(),JSON.parse(JSON.stringify(d.body)));draftId.value=d.id;revision.value=d.revision;file.value=null;if(fileInput.value)fileInput.value.value='';if(form.customerId&&!customers.value.some(c=>String(c.id)===form.customerId))customers.value.push({id:form.customerId,name:form.customerName})}
async function persistDraft(){await uploadFile();const d=await post<{id:string;revision:number;contractId?:string}>('/contract-history-drafts',{action:'history_save',id:Number(draftId.value||0),revision:revision.value,data:form},quietErrors);draftId.value=d.id;revision.value=d.revision;return d.contractId}
async function saveHistoryDraft(){if(busy.value)return;busy.value=true;entryError.value='';try{const linked=await persistDraft();if(linked){visible.value=false;emit('saved',linked);ElMessage.info('此草稿已接入，已打开原合同；未覆盖原合同资料');return}await loadDrafts();ElMessage.success('历史合同草稿已保存')}catch(error){showEntryError(error)}finally{busy.value=false}}
async function removeDraft(){if(busy.value)return;busy.value=true;entryError.value='';try{await ElMessageBox.confirm('删除这份未接入的历史草稿？删除后无法恢复。','删除草稿',{type:'warning'});await post('/contract-history-drafts',{action:'history_delete',id:Number(draftId.value),revision:revision.value},quietErrors);Object.assign(form,initial());draftId.value='';revision.value=0;addLine();await loadDrafts()}catch(error){showEntryError(error)}finally{busy.value=false}}
function payload(){return{customerId:form.customerId,currency:form.currency,externalContractNo:form.externalContractNo,terms:{customerContact:form.customerContact,buyerName:form.buyerName,buyerAddress:form.buyerAddress,sellerName:form.sellerName,sellerAddress:form.sellerAddress,incoterm:form.incoterm,paymentMethod:form.paymentMethod,portOfLoading:form.portOfLoading,portOfDischarge:form.portOfDischarge,deliveryDate:form.deliveryDate,receivableDueDate:form.receivableDueDate,terms:form.terms},items:form.items}}
async function save(){
 if(busy.value)return
 if(!props.history&&(!form.customerId||!form.items.length||form.items.some(l=>!l.productName.trim()||!l.uomCode.trim()||!Number.isFinite(Number(l.qty))||Number(l.qty)<=0||!l.unitPrice.trim()||!Number.isFinite(Number(l.unitPrice))||Number(l.unitPrice)<0))){ElMessage.warning('请选择客户，并填写完整的产品、单位、数量及对客单价');return}
 if(props.history&&(!form.customerId||!form.externalContractNo.trim()||!form.signedDate||!/^\d+(\.\d{1,2})?$/.test(form.totalAmount.trim())||Number(form.totalAmount)<=0||(!file.value&&!form.signedFileKey))){ElMessage.warning('请填写客户、原合同号、合同金额（大于 0，最多两位小数）、签订日期，并上传合同附件');return}

 busy.value=true
 entryError.value=''
 try{
  let result:{contract:{id:string}}
  if(props.history){const linked=await persistDraft();if(linked){visible.value=false;emit('saved',linked);ElMessage.info('此草稿已接入，已打开原合同');return}result=await post('/contracts/existing',{...payload(),items:[],terms:{terms:form.terms},signedDate:form.signedDate,effectiveDate:'',signedFileKey:form.signedFileKey,signedFileName:form.signedFileName,historyJson:JSON.stringify({recordOnly:true,totalAmount:form.totalAmount.trim(),status:form.historyStatus}),draftId:draftId.value,draftRevision:revision.value},withIdempotency(idem,quietErrors))}
  else {result=await post('/contracts/direct',payload(),withIdempotency(idem,quietErrors))}
  idem.reset();visible.value=false;emit('saved',result.contract.id);ElMessage.success(props.history?'历史合同已保存':'合同草稿已建立')
 }catch(error){showEntryError(error)}finally{busy.value=false}
}
watch(()=>props.modelValue,async v=>{importOpen.value=false;if(!v)return;entryError.value='';try{Object.assign(form,initial());contactOptions.value=[];selectedCustomerId='';lastBuyer=buyerFromCustomer();draftId.value='';revision.value=0;file.value=null;idem.reset();addLine();await searchCustomers('');if(props.history)await loadDrafts();else await loadEntryDefaults()}catch(error){showEntryError(error)}})
</script>
<style scoped>
:global(.contract-entry-dialog.el-dialog){display:flex;flex-direction:column;max-height:92vh}
:global(.contract-entry-dialog .el-dialog__body){overflow-y:auto;min-height:0;padding-right:8px}
:global(.contract-entry-dialog .el-dialog__header),:global(.contract-entry-dialog .el-dialog__footer){flex-shrink:0}
.entry-form.basic-form{grid-template-columns:repeat(4,minmax(0,1fr))}.entry-form :deep(.el-autocomplete){width:100%}
@media(max-width:760px){.entry-form.basic-form{grid-template-columns:repeat(2,minmax(0,1fr))}}
@media(max-width:480px){.entry-form.basic-form{grid-template-columns:1fr}}
.entry-error{margin-top:12px}.entry-form{display:grid;grid-template-columns:repeat(3,minmax(0,1fr));gap:0 18px;margin-top:18px}.history-form{grid-template-columns:repeat(2,minmax(0,1fr))}.file-name{margin-left:12px;overflow-wrap:anywhere}.file-hint{flex-basis:100%;margin-top:6px}.entry-form :deep(.el-select),.entry-form :deep(.el-date-editor){width:100%}.drafts{display:flex;align-items:center;gap:12px;margin:16px 0}small{display:block;font-size:12px;color:#64748b;line-height:1.6}@media(max-width:760px){.entry-form{grid-template-columns:1fr 1fr}.drafts{flex-wrap:wrap}}@media(max-width:480px){.entry-form{grid-template-columns:1fr}}

:global(.contract-entry-dialog--new > .el-dialog__header){padding-bottom:8px}
:global(.contract-entry-dialog--new > .el-dialog__header .el-dialog__title){font-size:18px;font-weight:600;color:#183f54}
:global(.contract-entry-dialog--new > .el-dialog__body){padding:0 12px 8px 0}
:global(.contract-entry-dialog--new > .el-dialog__footer){padding-top:10px;border-top:1px solid #e5edf2}
.entry-nav{
  position:sticky;
  z-index:3;
  top:0;
  display:flex;
  gap:4px;
  padding:2px 0 6px;
  overflow-x:auto;
  border-bottom:1px solid #e2eaf0;
  background:var(--el-bg-color);
}
.entry-nav :deep(.el-button){margin:0;padding:0 8px;height:28px;font-size:12px;color:#386274}
.entry-section{margin:0;padding:10px 0;border-bottom:1px solid #e7eef3;scroll-margin-top:40px}
.entry-section-heading{display:flex;align-items:baseline;justify-content:space-between;flex-wrap:wrap;gap:4px 10px;margin-bottom:6px}
.entry-section-heading h3{margin:0;color:#183f54;font-size:13px;font-weight:600}
.entry-meta{font-size:11px}
.entry-section .entry-form{gap:0 12px;margin:0}
.entry-section :deep(.el-form-item){min-width:0;margin-bottom:8px}
.entry-section :deep(.el-form-item__label){margin-bottom:3px;line-height:17px;font-size:12px}
.entry-section :deep(.el-input__wrapper),.entry-section :deep(.el-select__wrapper){min-height:28px;font-size:12px}
.entry-section :deep(.el-textarea__inner){font-size:12px;line-height:17px}
.entry-parties-section :deep(.contract-parties){margin:0;padding:8px 10px 1px}
.entry-parties-section :deep(header){gap:3px 10px;margin-bottom:6px;font-size:13px}
.entry-parties-section :deep(header span){font-size:11px}
.entry-parties-section :deep(.parties-grid){column-gap:12px}
.line-actions{display:flex;flex-wrap:wrap;gap:6px}
.line-actions :deep(.el-button){margin:0}
.entry-lines-table{font-variant-numeric:tabular-nums}
.entry-lines-table :deep(.el-table__cell){padding:3px 0;font-size:12px}
.entry-lines-table :deep(.cell){padding-inline:7px;line-height:18px}
.entry-lines-table :deep(.el-input__wrapper){min-height:24px}
.entry-product-description{display:grid;gap:2px}
.numeric-input :deep(.el-input__inner){text-align:right}
.entry-total{margin-top:6px;text-align:right;font-size:13px;font-weight:600;color:#183f54}
.entry-notes{border-bottom:0}
.entry-notes :deep(.el-form-item){margin-bottom:0}
</style>
