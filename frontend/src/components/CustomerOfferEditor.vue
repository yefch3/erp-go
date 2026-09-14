<template>
 <section v-loading="busy" class="customer-offer">
  <el-alert v-if="error" :title="error" type="error" :closable="false" show-icon />
  <template v-if="offer">
   <div class="offer-toolbar"><div><h3>给客户的报价 <el-tag>{{statusLabel}}</el-tag></h3><p>{{offer.canEdit?(offer.body.incoterm==='FOB'?'选择国内费用来源，按 FOB 公式计算并核对客户单价。':'先提供物流候选方案；记录客户选择后，按贸易公式计算报价。'):'已按客户确认的成交内容保留报价。'}}</p></div></div>
   <el-form label-position="top" :disabled="!offer.canEdit" class="offer-fields">
    <el-form-item label="客户"><el-autocomplete v-model="offer.body.customer" :fetch-suggestions="suggestCustomers" placeholder="选择或输入客户" @select="chooseCustomer" @input="offer.body.customerId='';offer.body.contactId=''"/></el-form-item><el-form-item label="联系人"><el-autocomplete v-model="offer.body.contact" :fetch-suggestions="suggestContacts" placeholder="选择或输入联系人" @select="chooseContact" @input="offer.body.contactId=''"/></el-form-item>
    <el-form-item label="报价币种"><el-input v-model="offer.body.currency" maxlength="3" /></el-form-item><el-form-item label="本次报价汇率（USD/CNY）"><el-input v-model="offer.body.quoteFx" inputmode="decimal" placeholder="由销售手工填写" /></el-form-item><el-form-item label="汇率确认"><el-checkbox v-model="offer.body.quoteFxConfirmed">我已核对并确认本次报价汇率</el-checkbox></el-form-item><el-form-item label="交货日期"><el-date-picker v-model="offer.body.delivery" value-format="YYYY-MM-DD" type="date" /></el-form-item><el-form-item label="有效期至"><el-date-picker v-model="offer.body.validUntil" value-format="YYYY-MM-DD" type="date" /></el-form-item>
    <el-form-item label="贸易条件"><el-select v-model="offer.body.incoterm" @change="termsChanged"><el-option label="FOB" value="FOB"/><el-option label="CFR" value="CFR"/></el-select></el-form-item><el-form-item label="贸易公式"><el-select :model-value="pricingCalculation.formula||undefined" @update:model-value="pricingCalculation.formula=$event" placeholder="选择贸易公式" @change="formulaChanged"><el-option v-for="f in applicableFormulas" :key="f.value" :value="f.value" :label="f.label"/></el-select></el-form-item>
<el-form-item v-if="offer.body.incoterm==='FOB'" label="国内费用来源"><el-select v-model="offer.body.logisticsQuoteId" placeholder="选择物流提供的费用报价" @change="pricingDirty=true"><el-option v-for="q in logisticsQuotes" :key="q.id" :value="q.id" :label="sourceQuoteTitle(q)"/></el-select></el-form-item><el-form-item v-else label="客户选定物流"><span>{{customerSelectedTransport?.title||'等待客户选择'}}</span></el-form-item><el-form-item label="装货港"><el-input v-model="offer.body.loadingPort" /></el-form-item><el-form-item label="目的港"><el-input v-model="offer.body.destinationPort" /></el-form-item><el-form-item label="付款条件"><el-input v-model="offer.body.payment" /></el-form-item><el-form-item label="备注"><el-input v-model="offer.body.remark" type="textarea" :rows="2" /></el-form-item>
   </el-form>
   <div v-if="offer.canEdit&&pricingCalculation.formula&&pricingSourceReady" class="pricing-status"><span>{{selectedFormula?.expression}}</span><span v-if="pricingStale">报价输入已变化，请重新计算。</span><el-button link type="primary" @click="calculateAll">{{offer.body.lines.some(l=>l.calculatedPrice)?'重新计算':'计算报价'}}</el-button></div>
   <el-alert v-if="offer.canEdit&&pricingStale" title="原单价待重新核验，请选好核价依据后重新计算；当前不能确认成交。" type="warning" :closable="false" show-icon/>
   <section class="pricing-panel">
   <div class="pricing-head"><div><span class="section-kicker">产品核价</span><h3>产品报价</h3><p>核对工厂报价，填写客户数量、单位及最终单价。</p></div><div class="pricing-total"><span>产品合计</span><strong>{{offer.body.currency}} {{productTotal}}</strong><small>{{offer.body.lines.length}} 项产品</small></div></div>
   <div class="table-tools"><el-input v-model="search" placeholder="搜索产品或规格" clearable /><div class="table-actions"><el-button v-if="offer.canEdit" plain @click="addProduct">添加产品</el-button></div></div>
   <el-table class="pricing-table" :data="visibleLines" max-height="470" row-key="id">
    <el-table-column type="expand"><template #default="{row}"><el-input v-if="offer.canEdit" v-model="row.specification" placeholder="产品规格" class="spec-editor" /><el-form-item v-if="offer.canEdit" label="每个客户报价单位对应吨数"><el-input v-model="row.calculation.mtPerUnit" @change="pricingDirty=true" placeholder="吨填 1；件按实际重量填写"/></el-form-item><el-descriptions :column="3" border><el-descriptions-item v-for="(value,key) in row.customFields" :key="key" :label="offer.source.body.template?.fields.find(f=>f.fieldKey===String(key))?.displayName||String(key)"><el-input v-if="offer.canEdit" v-model="row.customFields[key]"/><span v-else>{{value||'—'}}</span></el-descriptions-item></el-descriptions></template></el-table-column>
    <el-table-column label="产品 / 规格" min-width="220" fixed><template #default="{row}"><div class="product-cell"><el-input v-if="offer.canEdit" v-model="row.product" /><strong v-else>{{row.product}}</strong><small class="spec-summary" :title="specification(row)">{{specification(row)||'展开补充规格'}}</small></div></template></el-table-column>
    <el-table-column label="成交数量" width="125"><template #default="{row}"><el-input v-if="offer.canEdit" v-model="row.quantity" inputmode="decimal" /><span v-else>{{row.quantity}}</span></template></el-table-column>
    <el-table-column label="单位" width="85"><template #default="{row}"><el-input v-if="offer.canEdit" v-model="row.unit" @change="row.calculation.mtPerUnit='';pricingDirty=true" /><span v-else>{{row.unit}}</span></template></el-table-column>
    <el-table-column label="工厂报价" min-width="160"><template #default="{row}"><span>{{procurementBasis(offer.source.quotes,row.id)||'等待采购提供价格'}}</span><small>／{{offer.source.body.products.find(p=>p.id===row.id)?.unit||'—'}}</small></template></el-table-column>
    <el-table-column label="客户单价" width="170"><template #default="{row}"><el-input v-if="offer.canEdit" v-model="row.unitPrice" inputmode="decimal" /><strong v-else class="unit-price">{{row.unitPrice}}</strong><small>／{{row.unit}}</small><small v-if="row.calculatedPrice&&pricingStale" class="missing-price">原核价已失效</small><small v-else-if="row.calculatedPrice">公式计算价：{{offer.body.currency}} {{row.calculatedPrice}}</small><small v-else-if="!row.unitPrice" class="missing-price">待核价</small></template></el-table-column>
    <el-table-column label="金额" width="150"><template #default="{row}"><strong class="line-total">{{chargeSubtotal(row.unitPrice,row.quantity)||'—'}}</strong></template></el-table-column>
    <el-table-column v-if="offer.canEdit" label="操作" width="80" fixed="right"><template #default="{row}"><el-popconfirm title="确定移除这个产品？" confirm-button-text="移除" cancel-button-text="取消" @confirm="removeProduct(row)"><template #reference><el-button class="quiet-danger" link>移除</el-button></template></el-popconfirm></template></el-table-column>
   </el-table>
   <el-pagination v-model:current-page="page" :total="filteredLines.length" :page-size="20" layout="total,prev,pager,next" />
   </section>

   <section v-if="offer.body.incoterm!=='FOB'" class="transport-section">
   <div class="section-heading"><div><span class="section-kicker">物流方案</span><h3>运输报价</h3><p>查看货代提交的统一报价与运输时效。</p></div><div class="selection-count"><strong>{{offer.body.transports.length}}</strong><span>已加入</span><small>共 {{logisticsQuotes.length}} 个可选方案</small></div></div>
   <el-table class="transport-table" :data="logisticsQuotes" max-height="380" row-key="id" empty-text="等待物流提交报价">
    <el-table-column type="expand"><template #default="{row}">
     <div class="transport-details">
      <el-form v-if="selectedTransport(row.id)" label-position="top" :disabled="!offer.canEdit">
       <el-form-item label="报价单上的方案名称"><el-input v-model="selectedTransport(row.id)!.title"/></el-form-item>
       <el-form-item label="方案备注"><el-input v-model="selectedTransport(row.id)!.remark"/></el-form-item>
      </el-form>
      <strong>适用产品与规格</strong>
      <el-table :data="cargoLines(row)" max-height="240" size="small">
       <el-table-column prop="product" label="产品" width="150"/><el-table-column label="规格" min-width="220"><template #default="{row:line}">{{specification(line)||'—'}}</template></el-table-column>
       <el-table-column prop="quantity" label="询盘数量" width="100"/><el-table-column prop="unit" label="单位" width="80"/>
      </el-table>
      <p>物流提交费用：{{sourceQuoteTotals(row)}} · {{row.author||'—'}} · {{displayTime(row.submittedAt)}}</p>
      <el-table :data="row.body.charges||[]" max-height="200" size="small"><el-table-column prop="name" label="费用"/><el-table-column prop="amount" label="单价"/><el-table-column prop="unit" label="计费单位"/><el-table-column prop="quantity" label="数量"/><el-table-column prop="currency" label="币种"/><el-table-column prop="subtotal" label="金额"/></el-table>
     </div>
    </template></el-table-column>
    <el-table-column label="运输方案" min-width="280"><template #default="{row}"><div class="plan-title"><strong>{{sourceQuoteTitle(row)}}</strong><el-tag v-if="hasTransport(row.id)" size="small" type="success" effect="light">已加入</el-tag></div><small>{{sourceQuoteRoute(row)}}</small><div class="plan-meta"><span>{{row.body.transitDays||'—'}} 天</span><span>开船 {{row.body.departure||'待定'}}</span></div></template></el-table-column>
    <el-table-column label="覆盖产品" width="145"><template #default="{row}"><strong>{{cargoLines(row).length}} 项产品</strong><small>展开查看规格</small></template></el-table-column>
    <el-table-column label="物流报价" min-width="230"><template #default="{row}"><strong class="transport-price">{{sourceQuoteTotals(row)}}</strong><small>展开查看费用构成</small></template></el-table-column>
    <el-table-column label="选择" width="150" fixed="right"><template #default="{row}"><el-button v-if="offer.canEdit" :type="hasTransport(row.id)?'success':'primary'" :plain="hasTransport(row.id)" @click="toggleTransport(row,!hasTransport(row.id))">{{hasTransport(row.id)?'已加入 · 取消':'加入客户报价'}}</el-button><el-tag v-else-if="hasTransport(row.id)" type="success">已加入</el-tag></template></el-table-column>
   </el-table>
   </section>
<div class="offer-footer"><span v-if="pricingStale" class="missing-price">请先点击“重新计算”，再导出或确认成交。</span><div class="actions"><el-button @click="load">刷新上游报价</el-button><el-button :disabled="!offer.revision||pricingStale" @click="download">导出客户报价 PDF</el-button><el-button v-if="offer.canEdit" type="primary" @click="save">保存报价</el-button><el-button v-if="offer.canEdit&&offer.body.incoterm==='CFR'" @click="selectionOpen=true">记录客户选择</el-button><el-button v-if="offer.canEdit" type="success" :disabled="!canConfirm" @click="confirm">客户确认成交</el-button><el-button v-if="offer.contractId&&offer.contractId!=='0'" type="primary" @click="goContract">打开外销合同</el-button></div></div>
  </template>

  <el-dialog v-model="selectionOpen" title="记录客户选定的物流方案" width="min(760px,94vw)">
   <template v-if="offer"><p>客户选定一个方案后在这里记录，所选方案覆盖本次全部成交产品。此步骤不会生成合同。</p>
    <el-table :data="offer.body.transports" empty-text="请先将物流候选加入客户报价">
     <el-table-column prop="title" label="方案"/><el-table-column label="物流报价"><template #default="{row}">{{row.currency}} {{row.price||'—'}}</template></el-table-column>
     <el-table-column label="客户选定" width="110"><template #default="{row}"><el-switch :model-value="row.accepted" @update:model-value="selectCustomerTransport(row,!!$event)"/></template></el-table-column>
    </el-table>
   </template>
   <template #footer><el-button @click="selectionOpen=false">返回报价</el-button><el-button type="primary" :disabled="!customerSelectedTransport" :loading="busy" @click="calculateCustomerSelection">按选定方案计算报价</el-button></template>
  </el-dialog>
  <el-dialog v-model="confirmationOpen" title="客户确认成交" width="min(760px,94vw)">
   <template v-if="offer"><p>确认客户已接受以下最终价格。确认后生成合同并锁定报价。</p><p>贸易条件：{{offer.body.incoterm}} · {{offer.body.incoterm==='FOB'?'国内费用来源：'+(logisticsQuotes.find(q=>q.id===offer?.body.logisticsQuoteId)?.body.company||'已选定'):'客户选定物流：'+customerSelectedTransport?.title}}</p>
   <el-table :data="offer.body.lines"><el-table-column prop="product" label="产品"/><el-table-column prop="quantity" label="数量"/><el-table-column prop="unit" label="单位"/><el-table-column prop="unitPrice" label="最终单价"/></el-table><p>产品总额：{{offer.body.currency}} {{productTotal}}</p><p v-if="offer.body.incoterm==='CFR'">运费已计入产品单价，不另加收。</p></template>
   <template #footer><el-button @click="confirmationOpen=false">返回报价</el-button><el-button :disabled="!canConfirm" type="success" :loading="busy" @click="completeConfirmation">确认成交并生成合同</el-button></template>
  </el-dialog>
  <el-drawer :model-value="!!transport" title="方案对应的成交产品" size="min(660px,94vw)" @close="transport=null"><template v-if="transport&&offer"><el-input v-model="allocationSearch" placeholder="搜索产品" clearable/><el-table :data="offer.body.lines.filter(l=>!allocationSearch||`${l.product} ${specification(l)}`.includes(allocationSearch))" max-height="560"><el-table-column label="产品 / 规格"><template #default="{row}">{{row.product}}<small>{{specification(row)}}</small></template></el-table-column><el-table-column prop="quantity" label="成交数量"/><el-table-column label="本方案数量"><template #default="{row}"><el-input :model-value="transport.quantities[row.id]||''" :disabled="!offer.canEdit" @update:model-value="setAllocation(row.id,$event)" /></template></el-table-column></el-table></template></el-drawer>
 </section>
</template>
<script setup lang="ts">
import {computed,ref,watch} from 'vue'
import {useRouter} from 'vue-router'
import {ElMessage,ElMessageBox} from 'element-plus'
import {get,post,quietErrors} from '../api'
import {blankProduct,chargeSubtotal} from '../lib/inquiryWorkspace'
import {applyProcurementFormulaInput,emptyCalculation,procurementBasis,type Calculation,type Offer,type OfferBody,type OfferLine,type Transport,offerProductTotal,offerSpecification} from '../lib/customerOffer'
import type {Quote} from '../lib/inquiryWorkspace'
 const props=defineProps<{caseId:string;sourceVersion?:string}>(),router=useRouter(),offer=ref<Offer|null>(null),busy=ref(false),error=ref(''),search=ref(''),page=ref(1),confirmationOpen=ref(false),selectionOpen=ref(false),calculationOpen=ref(false),pricingCalculation=ref<Calculation>(emptyCalculation()),calculationQuoteId=ref(''),transport=ref<Transport|null>(null),allocationSearch=ref('')
 const filteredLines=computed(()=>offer.value?.body.lines.filter(l=>!search.value||`${l.product} ${specification(l)}`.includes(search.value))||[]),visibleLines=computed(()=>filteredLines.value.slice((page.value-1)*20,page.value*20)),statusLabel=computed(()=>pricingStale.value?'待重新核价':({PENDING:'待报价',QUOTED:'已报价',CONFIRMED:'客户已确认'}[offer.value?.status||'PENDING']))
 const pricingDirty=ref(false)
 const pricingStale=computed(()=>!!offer.value?.canEdit&&(pricingDirty.value||!!offer.value.pricingStale||!!offer.value.body.lines.some(l=>l.calculatedPrice&&!offer.value?.body.pricingSnapshot)))
 const pricingSourceReady=computed(()=>offer.value?.body.incoterm==='FOB'?!!offer.value.body.logisticsQuoteId:!!customerSelectedTransport.value)
 const canConfirm=computed(()=>!pricingStale.value&&pricingSourceReady.value&&!!pricingCalculation.value.formula&&!!offer.value?.body.quoteFxConfirmed&&!!offer.value?.body.lines.length&&offer.value.body.lines.every(l=>l.unitPrice&&l.calculatedPrice))
 const logisticsQuotes=computed(()=>offer.value?.source.quotes.filter(q=>q.kind==='LOGISTICS')||[])
const formulaOptions=[
 {value:1,label:'公式 1 · CFR｜基础费用（人民币核价）',expression:'工厂报价人民币折算值 ÷（有效汇率 − 0.05）＋ 海运费 ＋ 资金利息'},
 {value:2,label:'公式 2 · CFR｜基础费用（美元核价）',expression:'工厂报价美元折算值 ＋ 海运费 ＋ 资金利息'},
 {value:3,label:'公式 3 · CFR｜工厂价 + 内陆 + 港杂',expression:'（工厂价 ＋ 内陆运费 ＋ 港杂费）÷（有效汇率 − 0.05）＋ 海运费 ＋ 资金利息'},
 {value:4,label:'公式 4 · CFR｜完整国内费用',expression:'（工厂价 ＋ 分条费 ＋ 短导费 ＋ 内陆运费 ＋ 港杂费）÷（有效汇率 − 0.05）＋ 海运费 ＋ 资金利息'},
 {value:5,label:'公式 5 · FOB｜完整国内费用',expression:'（工厂价 ＋ 分条费 ＋ 短导费 ＋ 内陆运费 ＋ 港杂费）÷（有效汇率 − 0.05）＋ 资金利息'}
]
const calculationFields:{key:Exclude<keyof Calculation,'formula'|'factory'|'slitting'>;label:string}[]=[{key:'shortHaul',label:'短导费 RMB / 销售单位'},{key:'inland',label:'内陆运费 RMB / 销售单位'},{key:'port',label:'港区港杂费 RMB / 销售单位'},{key:'ocean',label:'海运费 USD / 销售单位'},{key:'days',label:'资金利息天数'},{key:'mtPerUnit',label:'每个销售单位的吨数（MT 填 1）'}]
const formulaFieldKeys:Record<number,Array<Exclude<keyof Calculation,'formula'|'factory'|'slitting'>>>={1:['ocean','days','mtPerUnit'],2:['ocean','days','mtPerUnit'],3:['inland','port','ocean','days','mtPerUnit'],4:['shortHaul','inland','port','ocean','days','mtPerUnit'],5:['shortHaul','inland','port','days','mtPerUnit']}
const applicableFormulas=computed(()=>formulaOptions.filter(f=>offer.value?.body.incoterm==='FOB'?f.value===5:offer.value?.body.incoterm==='CFR'?f.value!==5:false))
const selectedFormula=computed(()=>formulaOptions.find(item=>item.value===Number(pricingCalculation.value.formula)))
const visibleCalculationFields=computed(()=>{const keys=formulaFieldKeys[Number(pricingCalculation.value.formula)]||[];return calculationFields.filter(field=>keys.includes(field.key))})
type CustomerOption={id:string;name:string;code:string;value:string}
type ContactOption={id:string;name:string;value:string;isPrimary:boolean}
const customers=ref<CustomerOption[]>([]),contacts=ref<ContactOption[]>([])
async function suggestCustomers(query:string,done:(rows:CustomerOption[])=>void){try{if(!customers.value.length){const r=await get<{customers:CustomerOption[]}>('/sourcing-customer-options',{page_size:500});customers.value=r.customers.map(c=>({...c,value:c.name}))}done(customers.value.filter(c=>`${c.name} ${c.code}`.toLowerCase().includes(query.toLowerCase())).slice(0,50))}catch{done([])}}
async function suggestContacts(query:string,done:(rows:ContactOption[])=>void){try{if(!offer.value?.body.customerId){done([]);return}const r=await get<{contacts:ContactOption[]}>(`/sourcing-customer-options/${offer.value.body.customerId}/contacts`);contacts.value=r.contacts.map(c=>({...c,value:c.name}));done(contacts.value.filter(c=>c.name.toLowerCase().includes(query.toLowerCase())))}catch{done([])}}
function chooseCustomer(c:CustomerOption){if(!offer.value)return;offer.value.body.customerId=c.id;offer.value.body.customer=c.name;offer.value.body.contact='';offer.value.body.contactId='';contacts.value=[]}
function chooseContact(c:ContactOption){if(!offer.value)return;offer.value.body.contactId=c.id;offer.value.body.contact=c.name}
async function command(action:string,body:OfferBody|undefined=offer.value?.body){
 error.value=''
 try{return await post<Offer>('/customer-offer',{action,caseId:props.caseId,revision:offer.value?.revision||0,body},quietErrors)}
 catch(e){const failure=e as {message?:string;code?:string};if(failure.code==='OFFER_PRICING_STALE'&&offer.value)offer.value.pricingStale=true;error.value=failure.message||'报价操作失败，请重试';ElMessage.error(error.value);throw e}
}
let loadSequence=0
const productTotal=computed(()=>offerProductTotal(offer.value?.body.lines||[]))
const specification=offerSpecification
function prepare(view:Offer){view.body.transports??=[];view.body.lines.forEach(l=>{l.calculation={...emptyCalculation(),...l.calculation}});return view}
async function load(){if(busy.value)return;const seq=++loadSequence,caseId=props.caseId;busy.value=true;error.value='';try{const next=prepare(await command('get'));if(seq!==loadSequence||caseId!==props.caseId)return;if(offer.value&&offer.value.canEdit&&next.canEdit){if(JSON.stringify(offer.value.source.quotes)!==JSON.stringify(next.source.quotes))pricingDirty.value=true;offer.value.source=next.source;offer.value.pricingStale=next.pricingStale;}else {offer.value=next;pricingCalculation.value={...(next.body.lines[0]?.calculation||emptyCalculation())}}}catch{error.value='客户报价加载失败，请重试'}finally{busy.value=false}}
async function save(){if(busy.value)return;if(pricingStale.value&&offer.value?.body.lines.some(l=>l.calculatedPrice)){ElMessage.warning('核价输入已变化，请先重新计算');return}busy.value=true;try{offer.value=await command('save');pricingDirty.value=false;ElMessage.success('客户报价已保存')}catch{}finally{busy.value=false}}
const customerSelectedTransport=computed(()=>offer.value?.body.transports.find(t=>t.accepted))
function confirm(){if(canConfirm.value)confirmationOpen.value=true}
function selectCustomerTransport(row:Transport,yes:boolean){
 if(!offer.value)return
 const previous=offer.value.body.logisticsQuoteId
 offer.value.body.transports.forEach(t=>t.accepted=yes&&t.quoteId===row.quoteId)
 offer.value.body.logisticsQuoteId=yes?row.quoteId:''
 if(yes)row.quantities=Object.fromEntries(offer.value.body.lines.map(l=>[l.id,l.quantity]))
 if(previous!==offer.value.body.logisticsQuoteId)pricingDirty.value=true
}
async function calculateCustomerSelection(){await calculateAll();if(!pricingStale.value)selectionOpen.value=false}
async function completeConfirmation(){if(busy.value||!canConfirm.value)return;if(pricingStale.value&&offer.value?.body.lines.some(l=>l.calculatedPrice)){ElMessage.warning('核价输入已变化，请先重新计算');return}busy.value=true;try{offer.value=await command('save');offer.value=await command('confirm');confirmationOpen.value=false;goContract()}catch{}finally{busy.value=false}}
function goContract(){if(offer.value)router.push({path:'/contracts',query:{id:offer.value.contractId}})}
async function download(){if(busy.value)return;if(pricingStale.value){ElMessage.warning('请先重新核价再导出');return}busy.value=true;try{if(offer.value?.canEdit)offer.value=await command('save');const file=await post<{fileData:string;fileName:string}>('/customer-offer',{action:'pdf',caseId:props.caseId});const data=Uint8Array.from(atob(file.fileData),c=>c.charCodeAt(0)),url=URL.createObjectURL(new Blob([data],{type:'application/pdf'})),a=document.createElement('a');a.href=url;a.download=file.fileName;a.click();setTimeout(()=>URL.revokeObjectURL(url),1000)}catch{}finally{busy.value=false}}
function formulaChanged(){pricingDirty.value=true}
function termsChanged(){if(!offer.value)return;pricingCalculation.value.formula=0;pricingDirty.value=true;offer.value.body.logisticsQuoteId='';offer.value.body.transports.forEach(t=>t.accepted=false)}
async function calculateAll(){
 if(busy.value||!offer.value)return
 const formula=Number(pricingCalculation.value.formula)
 const selected=customerSelectedTransport.value
 if(offer.value.body.incoterm==='CFR'){
  if(!selected){ElMessage.warning('请先记录客户选定的物流方案');return}
  offer.value.body.logisticsQuoteId=selected.quoteId
  selected.quantities=Object.fromEntries(offer.value.body.lines.map(l=>[l.id,l.quantity]))
 } else if(!offer.value.body.logisticsQuoteId){ElMessage.warning('请选择国内费用来源');return}

 if(offer.value.body.lines.some(l=>l.unitPrice)){
  try{await ElMessageBox.confirm('重新计算会替换当前客户单价，是否继续？','重新计算报价',{type:'warning'})}catch{return}
 }
 const logistics=logisticsQuotes.value.find(q=>q.id===offer.value?.body.logisticsQuoteId)
 if(!logistics){ElMessage.warning('客户选定的物流报价已失效，请刷新');return}
 const fx=Number(offer.value.body.quoteFx)
 if(!offer.value.body.quoteFxConfirmed||!(fx>0.05)){ElMessage.warning('请填写并确认报价汇率');return}
 const prepared:OfferLine[]=[]

 if(!formula){ElMessage.warning('请先选择公司报价公式');return}
 for(const original of offer.value.body.lines){
  const line={...original,calculation:{...pricingCalculation.value,mtPerUnit:original.calculation.mtPerUnit}}
  const mt=Number(line.calculation.mtPerUnit)
  if(!(mt>0)){ElMessage.warning(line.product+'：请展开产品填写每个客户单位对应吨数');return}
  const factor=(unit:string)=>unit===line.unit?1:unit==='MT'?mt:NaN
  const find=(pattern:RegExp,target:string)=>{
   let total=0
   for(const c of logistics.body.charges.filter(c=>pattern.test(c.name))){
    const ratio=factor(c.unit);if(!Number.isFinite(ratio))throw new Error('物流单位与客户单位不同，请先统一单位或补齐吨数')
    let amount=Number(c.amount)*ratio
    if(c.currency!==target){if(c.currency==='CNY'&&target==='USD')amount/=fx;else if(c.currency==='USD'&&target==='CNY')amount*=fx;else throw new Error('当前公式仅支持人民币和美元物流报价')}
    total+=amount
   }
   return total.toFixed(8)
  }
  try{
   line.calculation.shortHaul=find(/短导|short.?haul/i,'CNY');line.calculation.inland=find(/内陆|inland/i,'CNY')
   line.calculation.port=find(/港杂|terminal|port/i,'CNY');line.calculation.ocean=find(/海运|ocean/i,'USD')
   line.calculation.days=logistics.body.transitDays||'0'
  }catch(e){ElMessage.warning((e as Error).message);return}
  if(!applyProcurementFormulaInput(line,offer.value.source.quotes,formula)){ElMessage.warning(`${line.product} 缺少该公式所需的采购价格，请采购补齐后刷新`);return}
  prepared.push(line)
 }
 pricingDirty.value=true
 busy.value=true;try{offer.value=prepare(await command('calculate_all',{...offer.value.body,lines:prepared}));pricingDirty.value=false;ElMessage.success('客户单价已计算，可继续修改')}catch{}finally{busy.value=false}
}
function addProduct(){if(!offer.value)return;offer.value.body.lines.push({...blankProduct(),id:crypto.randomUUID(),factoryQuoteId:'',calculation:emptyCalculation(),calculatedPrice:'',unitPrice:'',amount:''});search.value='';page.value=Math.ceil(offer.value.body.lines.length/20)}
function removeProduct(line:OfferLine){if(!offer.value)return;offer.value.body.lines=offer.value.body.lines.filter(l=>l.id!==line.id);offer.value.body.transports.forEach(t=>delete t.quantities[line.id]);page.value=Math.min(page.value,Math.max(1,Math.ceil(filteredLines.value.length/20)))}
 function sourceQuoteTitle(q:Quote){return q.body.company||q.body.carrier||`物流报价 ${q.id}`}
 function sourceQuoteRoute(q:Quote){return [q.body.route,q.body.carrier,q.body.loadingPort&&q.body.destinationPort?`${q.body.loadingPort} → ${q.body.destinationPort}`:''].filter(Boolean).join(' · ')||'未填写航线信息'}
 function sourceQuoteTotals(q:Quote){const entries=Object.entries(q.body.totals||{});if(entries.length>1)return '待物流统一币种后重新提交';return entries.length?entries[0][0]+' '+entries[0][1]+'／'+(q.body.charges[0]?.unit||'—'):'等待物流报价'}
 function displayTime(value:string){if(!value)return '—';const date=new Date(value);return Number.isNaN(date.getTime())?value:date.toLocaleString('zh-CN',{year:'numeric',month:'2-digit',day:'2-digit',hour:'2-digit',minute:'2-digit',hour12:false})}
 function cargoLines(q:Quote){const lines=offer.value?.source.body.products||[];return q.body.cargoIds?.length?lines.filter(l=>q.body.cargoIds.includes(l.id)):lines}
 function selectedTransport(id:string){return offer.value?.body.transports.find(t=>t.quoteId===id)}
 function hasTransport(id:string){return !!offer.value?.body.transports.some(row=>row.quoteId===id)}
 function transportFromQuote(q:Quote):Transport{const totals=Object.entries(q.body.totals||{}),single=totals.length===1?totals[0]:undefined;return{quoteId:q.id,title:q.body.route||q.body.carrier||q.body.company||`物流报价 ${q.id}`,currency:single?.[0]||offer.value?.body.currency||'USD',price:single?.[1]||'',remark:'',accepted:false,quantities:{}}}
 function toggleTransport(q:Quote,yes:boolean){
 if(!offer.value)return
 if(yes&&!hasTransport(q.id))offer.value.body.transports.push(transportFromQuote(q))
 if(!yes){
  if(customerSelectedTransport.value?.quoteId===q.id){ElMessage.warning('该方案已由客户选定，请先更改客户选择');return}
  offer.value.body.transports=offer.value.body.transports.filter(t=>t.quoteId!==q.id)
 }
 }
function setAllocation(id:string,value:string){if(!transport.value)return;if(value.trim())transport.value.quantities[id]=value;else delete transport.value.quantities[id]}
 watch(()=>JSON.stringify(offer.value&&[offer.value.body.quoteFx,offer.value.body.quoteFxConfirmed,offer.value.body.currency,offer.value.body.lines.map(l=>[l.id,l.quantity,l.unit,l.calculation.mtPerUnit]),offer.value.source.quotes]),(v,old)=>{if(old&&v!==old&&!busy.value)pricingDirty.value=true},{flush:'sync'});watch(search,()=>page.value=1);watch(()=>props.caseId,()=>{loadSequence++;offer.value=null;pricingDirty.value=false;selectionOpen.value=false;page.value=1;calculationOpen.value=false;pricingCalculation.value=emptyCalculation();calculationQuoteId.value='';transport.value=null;confirmationOpen.value=false;busy.value=false;void load()},{immediate:true});watch(()=>props.sourceVersion,()=>{void load()})
</script>
<style scoped>
.customer-offer{padding:18px;background:white;border:1px solid #dbe5ed;border-radius:12px}.offer-toolbar,.table-tools,.section-heading{display:flex;justify-content:space-between;align-items:center;gap:14px;margin-bottom:18px}.actions,.table-actions{display:flex;gap:8px;flex-wrap:wrap}.actions .el-button,.table-actions .el-button{margin:0}h3{color:#173e53;margin:0 0 10px}p,small{color:#637889}small{display:block;margin-top:5px}.offer-fields{display:grid;grid-template-columns:repeat(5,minmax(0,1fr));gap:0 16px}.source-summary{display:grid;grid-template-columns:repeat(3,minmax(0,1fr));gap:12px;margin:0 0 20px}.source-summary>div{display:flex;flex-direction:column;padding:14px 16px;border:1px solid #d8e5ec;border-radius:10px;background:#f6fafc}.source-summary strong{margin-top:5px;font-size:22px;color:#0f6078}.selected-heading{margin-top:24px}.table-tools>.el-input{max-width:300px}.el-pagination{margin:14px 0 24px}.el-table .el-input+.el-input{margin-top:6px}.el-alert{margin-bottom:18px}@media(max-width:1100px){.offer-fields{grid-template-columns:repeat(3,minmax(0,1fr))}.offer-toolbar{align-items:flex-start;flex-direction:column}}@media(max-width:760px){.source-summary{grid-template-columns:1fr}}@media(max-width:640px){.offer-fields{grid-template-columns:1fr 1fr}.table-tools,.section-heading{align-items:flex-start;flex-direction:column}}
.offer-footer{position:sticky;bottom:0;z-index:5;display:flex;justify-content:space-between;align-items:center;gap:16px;background:#f3f8fb;border:1px solid #d9e5ec;border-radius:10px;padding:16px;margin-top:20px;box-shadow:0 -4px 18px #173e5308}.transport-details{padding:16px 24px;background:#f6f9fb}.spec-summary{overflow:hidden;text-overflow:ellipsis;white-space:nowrap;max-width:260px}.spec-editor{margin:12px 0}.section-heading{margin-top:24px}.formula-preview{display:flex;flex-direction:column;gap:6px;margin:-4px 0 16px;padding:13px 15px;border:1px solid #cfe2ea;border-left:4px solid #16758e;border-radius:8px;background:#f4fafc;color:#23485b}.formula-preview span{font-size:13px;line-height:1.6;color:#627b88}@media(max-width:760px){.offer-footer{position:static;align-items:flex-start;flex-direction:column}}
.pricing-panel,.transport-section{margin-top:24px;border:1px solid #d9e5ec;border-radius:14px;background:#fff;overflow:hidden;box-shadow:0 8px 24px rgba(29,65,84,.045)}
.pricing-head,.section-heading{display:flex;align-items:center;justify-content:space-between;gap:24px;padding:22px 24px;margin:0;background:linear-gradient(135deg,#f8fbfc 0%,#f1f7f9 100%);border-bottom:1px solid #dce8ed}
.pricing-head h3,.section-heading h3{margin:4px 0 5px;font-size:20px;line-height:1.25;color:#153b4f}.pricing-head p,.section-heading p{margin:0;font-size:13px}
.section-kicker{font-size:10px;font-weight:700;letter-spacing:.14em;color:#16809a}
.pricing-total,.selection-count{min-width:180px;padding:12px 16px;border:1px solid #d4e5eb;border-radius:10px;background:#fff;text-align:right}.pricing-total span,.selection-count span{font-size:12px;color:#667e8b}.pricing-total strong{display:block;margin:3px 0;font-size:21px;color:#123f55}.selection-count strong{margin-right:7px;font-size:24px;color:#11758d}.pricing-total small,.selection-count small{margin:0;font-size:12px}
.table-tools{margin:0;padding:16px 20px;background:#fff}.table-tools>.el-input{max-width:280px}.table-actions{margin-left:auto}
.product-cell{padding:4px 0}.product-cell .spec-summary{margin:7px 0 0;color:#587181;font-size:12px}.line-total,.unit-price{color:#183d50;font-variant-numeric:tabular-nums}.line-total{font-size:15px}.missing-price{color:#a16b24}.quiet-danger{color:#82939d}.quiet-danger:hover{color:#d45b5b}
.transport-section{margin-top:26px}.transport-section .section-heading{margin:0}.selection-count{min-width:150px}.plan-title{display:flex;align-items:center;gap:10px}.plan-title strong{font-size:15px;color:#183d50}.plan-meta{display:flex;gap:7px;margin-top:9px}.plan-meta span{padding:3px 8px;border-radius:999px;background:#eef5f7;color:#5d7582;font-size:12px}.transport-price{font-size:17px;color:#116f87;font-variant-numeric:tabular-nums}
.pricing-table,.transport-table{--el-table-header-bg-color:#f6f9fa;--el-table-header-text-color:#35586a;--el-table-row-hover-bg-color:#f5fafb;--el-table-border-color:#e3ebef}.pricing-table :deep(th.el-table__cell),.transport-table :deep(th.el-table__cell){height:52px;font-weight:650}.pricing-table :deep(td.el-table__cell),.transport-table :deep(td.el-table__cell){padding:13px 0}.pricing-table :deep(.el-input__wrapper),.pricing-table :deep(.el-select__wrapper){background:#fbfcfd;box-shadow:0 0 0 1px #d7e2e8 inset}.pricing-table :deep(.el-input__wrapper:hover),.pricing-table :deep(.el-select__wrapper:hover){box-shadow:0 0 0 1px #81afbc inset}
.customer-offer :deep(.el-button--primary:not(.is-plain)){color:#fff;background:#116f87;border-color:#116f87}.customer-offer :deep(.el-button--primary:not(.is-plain):hover){background:#0c6075;border-color:#0c6075}.customer-offer :deep(.el-button--success:not(.is-plain)){color:#fff}
.pricing-panel>.el-pagination{padding:15px 20px;margin:0;border-top:1px solid #e5edf0}.transport-section+.offer-footer{margin-top:22px}
@media(max-width:900px){.pricing-head,.section-heading{align-items:flex-start;flex-direction:column}.pricing-total,.selection-count{width:100%;text-align:left}.table-tools{align-items:stretch;flex-direction:column}.table-tools>.el-input{max-width:none}.table-actions{margin-left:0}}
.pricing-head,.section-heading{padding:14px 18px;background:#fff}.pricing-head .section-kicker,.section-heading .section-kicker,.pricing-total,.selection-count{display:none}.pricing-panel,.transport-section{box-shadow:none;border-radius:8px}.pricing-head h3,.section-heading h3{font-size:17px}.pricing-status{display:flex;align-items:center;gap:16px;padding:8px 0;color:#637889}.pricing-panel small{margin-top:3px}
.offer-fields{grid-template-columns:repeat(auto-fit,minmax(210px,1fr))}.offer-fields :deep(.el-checkbox){height:auto;align-items:flex-start;white-space:normal}.offer-fields :deep(.el-checkbox__label){white-space:normal;line-height:20px}.pricing-status{flex-wrap:wrap}
</style>
