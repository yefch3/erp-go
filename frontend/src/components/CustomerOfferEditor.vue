<template>
 <section v-loading="busy" class="customer-offer">
  <el-alert v-if="error" :title="error" type="error" :closable="false" show-icon />
  <template v-if="offer">
   <div class="offer-toolbar"><div><h3>给客户的报价 <el-tag>{{statusLabel}}</el-tag></h3><p>{{offer.canEdit?'先核对产品、规格与单价，再选择需要提供给客户的运输报价。':'已按客户确认的成交内容保留报价。'}}</p></div><el-tag effect="plain">{{offer.body.lines.length}} 项产品 · {{offer.body.transports.length}} 个运输候选</el-tag></div>
   <el-form label-position="top" :disabled="!offer.canEdit" class="offer-fields">
    <el-form-item label="客户"><el-autocomplete v-model="offer.body.customer" :fetch-suggestions="suggestCustomers" placeholder="选择或输入客户" @select="chooseCustomer" @input="offer.body.customerId='';offer.body.contactId=''"/></el-form-item><el-form-item label="联系人"><el-autocomplete v-model="offer.body.contact" :fetch-suggestions="suggestContacts" placeholder="选择或输入联系人" @select="chooseContact" @input="offer.body.contactId=''"/></el-form-item>
    <el-form-item label="报价币种"><el-input v-model="offer.body.currency" maxlength="3" /></el-form-item><el-form-item label="交货日期"><el-date-picker v-model="offer.body.delivery" value-format="YYYY-MM-DD" type="date" /></el-form-item><el-form-item label="有效期至"><el-date-picker v-model="offer.body.validUntil" value-format="YYYY-MM-DD" type="date" /></el-form-item>
    <el-form-item label="贸易条件"><el-input v-model="offer.body.incoterm" /></el-form-item><el-form-item label="装货港"><el-input v-model="offer.body.loadingPort" /></el-form-item><el-form-item label="目的港"><el-input v-model="offer.body.destinationPort" /></el-form-item><el-form-item label="付款条件"><el-input v-model="offer.body.payment" /></el-form-item><el-form-item label="备注"><el-input v-model="offer.body.remark" type="textarea" :rows="2" /></el-form-item>
   </el-form>
   <section class="pricing-panel">
   <div class="pricing-head"><div><span class="section-kicker">产品核价</span><h3>产品报价</h3><p>先选择各产品的工厂报价，再统一核价；计算后仍可调整单价。</p></div><div class="pricing-total"><span>产品合计</span><strong>{{offer.body.currency}} {{productTotal}}</strong><small>{{offer.body.lines.length}} 项产品</small></div></div>
   <div class="table-tools"><el-input v-model="search" placeholder="搜索产品或规格" clearable /><div class="table-actions"><el-button v-if="offer.canEdit" type="primary" @click="openCalculation">统一核价</el-button><el-button v-if="offer.canEdit" plain @click="addProduct">添加产品</el-button></div></div>
   <el-table class="pricing-table" :data="visibleLines" max-height="470" row-key="id">
    <el-table-column type="expand"><template #default="{row}"><el-input v-if="offer.canEdit" v-model="row.specification" placeholder="产品规格" class="spec-editor" /><el-descriptions :column="3" border><el-descriptions-item v-for="(value,key) in row.customFields" :key="key" :label="offer.source.body.template?.fields.find(f=>f.fieldKey===String(key))?.displayName||String(key)"><el-input v-if="offer.canEdit" v-model="row.customFields[key]"/><span v-else>{{value||'—'}}</span></el-descriptions-item></el-descriptions></template></el-table-column>
    <el-table-column label="产品 / 规格" min-width="220" fixed><template #default="{row}"><div class="product-cell"><el-input v-if="offer.canEdit" v-model="row.product" /><strong v-else>{{row.product}}</strong><small class="spec-summary" :title="specification(row)">{{specification(row)||'展开补充规格'}}</small></div></template></el-table-column>
    <el-table-column label="成交数量" width="125"><template #default="{row}"><el-input v-if="offer.canEdit" v-model="row.quantity" inputmode="decimal" /><span v-else>{{row.quantity}}</span></template></el-table-column>
    <el-table-column label="单位" width="85"><template #default="{row}"><el-input v-if="offer.canEdit" v-model="row.unit" /><span v-else>{{row.unit}}</span></template></el-table-column>
    <el-table-column label="工厂报价" min-width="190"><template #default="{row}"><el-select v-if="offer.canEdit" v-model="row.factoryQuoteId" clearable placeholder="选择工厂报价" @change="selectFactory(row)"><el-option v-for="q in factoriesFor(offer.source.quotes,row.id)" :key="q.id" :value="q.id" :label="`${q.body.company} · ${q.body.currency} ${q.body.prices.find(p=>p.productId===row.id)?.price||'—'}`" /></el-select><span v-else>{{offer.source.quotes.find(q=>q.id===row.factoryQuoteId)?.body.company||'—'}}</span></template></el-table-column>
    <el-table-column label="单价" width="140"><template #default="{row}"><el-input v-if="offer.canEdit" v-model="row.unitPrice" inputmode="decimal" /><strong v-else class="unit-price">{{row.unitPrice}}</strong><small v-if="!row.unitPrice" class="missing-price">待核价</small></template></el-table-column>
    <el-table-column label="金额" width="150"><template #default="{row}"><strong class="line-total">{{chargeSubtotal(row.unitPrice,row.quantity)||'—'}}</strong></template></el-table-column>
    <el-table-column v-if="offer.canEdit" label="操作" width="80" fixed="right"><template #default="{row}"><el-popconfirm title="确定移除这个产品？" confirm-button-text="移除" cancel-button-text="取消" @confirm="removeProduct(row)"><template #reference><el-button class="quiet-danger" link>移除</el-button></template></el-popconfirm></template></el-table-column>
   </el-table>
   <el-pagination v-model:current-page="page" :total="filteredLines.length" :page-size="20" layout="total,prev,pager,next" />
   </section>

   <section class="transport-section">
   <div class="section-heading"><div><span class="section-kicker">物流方案</span><h3>运输报价</h3><p>比较物流费用和时效，选择需要展示给客户的候选方案。</p></div><div class="selection-count"><strong>{{offer.body.transports.length}}</strong><span>已加入</span><small>共 {{logisticsQuotes.length}} 个可选方案</small></div></div>
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
<div class="offer-footer"><div class="actions"><el-button @click="load">刷新上游报价</el-button><el-button :disabled="!offer.revision" @click="download">导出客户报价 PDF</el-button><el-button v-if="offer.canEdit" type="primary" @click="save">保存报价</el-button><el-button v-if="offer.canEdit" type="success" @click="confirm">客户已确认</el-button><el-button v-if="offer.contractId&&offer.contractId!=='0'" type="primary" @click="goContract">打开外销合同</el-button></div></div>
  </template>

  <el-drawer v-model="calculationOpen" title="整份报价统一核价" size="min(560px,94vw)">
   <template v-if="calculationOpen">
    <el-form label-position="top">
     <el-form-item label="公司报价公式"><el-select v-model="pricingCalculation.formula" placeholder="选择五种公式之一"><el-option v-for="formula in formulaOptions" :key="formula.value" :value="formula.value" :label="formula.label" /></el-select></el-form-item>
     <div class="formula-preview"><strong>{{selectedFormula?.label||'请选择报价公式'}}</strong><span>{{selectedFormula?.expression||'选择后只显示该公式需要填写的费用。'}}</span></div>
     <el-form-item v-if="Number(pricingCalculation.formula)!==5" label="用于计算的物流报价"><el-select v-model="calculationQuoteId" clearable placeholder="选择物流报价并带入海运费" @change="selectCalculationQuote"><el-option v-for="q in logisticsQuotes" :key="q.id" :value="q.id" :label="`${sourceQuoteTitle(q)} · ${sourceQuoteTotals(q)}`" /></el-select></el-form-item>
     <el-alert :title="`本次统一计算 ${offer?.body.lines.length||0} 项产品；各产品使用表格中所选的工厂报价，物流费用和公式参数统一。`" type="info" :closable="false"/>
     <el-form-item v-for="f in visibleCalculationFields" :key="f.key" :label="f.label"><el-input v-model="pricingCalculation[f.key]" inputmode="decimal" /></el-form-item>
    </el-form>
    <el-button type="primary" @click="calculateAll">计算全部产品单价</el-button>
    <p>公司公式整份报价只选一次；结果分别写入每个产品的普通“单价”，销售仍可逐项修改。公式不显示在客户 PDF 中。</p>
   </template>
  </el-drawer>

  <el-dialog v-model="confirmationOpen" title="确认客户接受的成交内容" width="min(760px,94vw)">
   <template v-if="offer"><p>核对产品数量、单价，并选择客户实际接受的运输方案。</p>
    <el-table :data="offer.body.transports" max-height="350" empty-text="本次没有附加运输方案">
     <el-table-column prop="title" label="方案"/><el-table-column label="费用"><template #default="{row}">{{row.currency}} {{row.price||'—'}}</template></el-table-column>
     <el-table-column label="客户接受" width="110"><template #default="{row}"><el-switch v-model="row.accepted"/></template></el-table-column>
     <el-table-column label="对应产品数量" width="170"><template #default="{row}"><el-button @click="transport=row">{{Object.keys(row.quantities||{}).length}} 项 · 填写数量</el-button></template></el-table-column>
    </el-table>
   </template>
   <template #footer><el-button @click="confirmationOpen=false">继续修改</el-button><el-button type="primary" :loading="busy" @click="completeConfirmation">确认成交并生成合同</el-button></template>
  </el-dialog>
  <el-drawer :model-value="!!transport" title="方案对应的成交产品" size="min(660px,94vw)" @close="transport=null"><template v-if="transport&&offer"><el-input v-model="allocationSearch" placeholder="搜索产品" clearable/><el-table :data="offer.body.lines.filter(l=>!allocationSearch||`${l.product} ${specification(l)}`.includes(allocationSearch))" max-height="560"><el-table-column label="产品 / 规格"><template #default="{row}">{{row.product}}<small>{{specification(row)}}</small></template></el-table-column><el-table-column prop="quantity" label="成交数量"/><el-table-column label="本方案数量"><template #default="{row}"><el-input :model-value="transport.quantities[row.id]||''" :disabled="!offer.canEdit" @update:model-value="setAllocation(row.id,$event)" /></template></el-table-column></el-table></template></el-drawer>
 </section>
</template>
<script setup lang="ts">
import {computed,ref,watch} from 'vue'
import {useRouter} from 'vue-router'
import {ElMessage} from 'element-plus'
import {get,post} from '../api'
import {blankProduct,chargeSubtotal} from '../lib/inquiryWorkspace'
import {emptyCalculation,factoriesFor,type Calculation,type Offer,type OfferLine,type Transport,offerProductTotal,offerSpecification,selectOnlyFactory} from '../lib/customerOffer'
import type {Quote} from '../lib/inquiryWorkspace'
 const props=defineProps<{caseId:string;sourceVersion?:string}>(),router=useRouter(),offer=ref<Offer|null>(null),busy=ref(false),error=ref(''),search=ref(''),page=ref(1),confirmationOpen=ref(false),calculationOpen=ref(false),pricingCalculation=ref<Calculation>(emptyCalculation()),calculationQuoteId=ref(''),transport=ref<Transport|null>(null),allocationSearch=ref('')
 const filteredLines=computed(()=>offer.value?.body.lines.filter(l=>!search.value||`${l.product} ${specification(l)}`.includes(search.value))||[]),visibleLines=computed(()=>filteredLines.value.slice((page.value-1)*20,page.value*20)),statusLabel=computed(()=>({PENDING:'待报价',QUOTED:'已报价',CONFIRMED:'客户已确认'}[offer.value?.status||'PENDING']))
 const logisticsQuotes=computed(()=>offer.value?.source.quotes.filter(q=>q.kind==='LOGISTICS')||[])
const formulaOptions=[
 {value:1,label:'公式 1 · CFR｜FOB 人民币报价',expression:'FOB 人民币价 ÷（有效汇率 − 0.05）＋ 海运费 ＋ 资金利息'},
 {value:2,label:'公式 2 · CFR｜FOB 美元报价',expression:'FOB 美元价 ＋ 海运费 ＋ 资金利息'},
 {value:3,label:'公式 3 · CFR｜工厂价 + 内陆 + 港杂',expression:'（工厂价 ＋ 内陆运费 ＋ 港杂费）÷（有效汇率 − 0.05）＋ 海运费 ＋ 资金利息'},
 {value:4,label:'公式 4 · CFR｜完整国内费用',expression:'（工厂价 ＋ 分条费 ＋ 短导费 ＋ 内陆运费 ＋ 港杂费）÷（有效汇率 − 0.05）＋ 海运费 ＋ 资金利息'},
 {value:5,label:'公式 5 · FOB｜完整国内费用',expression:'（工厂价 ＋ 分条费 ＋ 短导费 ＋ 内陆运费 ＋ 港杂费）÷（有效汇率 − 0.05）＋ 资金利息'}
]
const calculationFields:{key:Exclude<keyof Calculation,'formula'|'factory'>;label:string}[]=[{key:'slitting',label:'分条费 RMB / 销售单位'},{key:'shortHaul',label:'短导费 RMB / 销售单位'},{key:'inland',label:'内陆运费 RMB / 销售单位'},{key:'port',label:'港区港杂费 RMB / 销售单位'},{key:'ocean',label:'海运费 USD / 销售单位'},{key:'days',label:'资金利息天数'},{key:'mtPerUnit',label:'每个销售单位的吨数（MT 填 1）'}]
const formulaFieldKeys:Record<number,Array<Exclude<keyof Calculation,'formula'|'factory'>>>={1:['ocean','days','mtPerUnit'],2:['ocean','days','mtPerUnit'],3:['inland','port','ocean','days','mtPerUnit'],4:['slitting','shortHaul','inland','port','ocean','days','mtPerUnit'],5:['slitting','shortHaul','inland','port','days','mtPerUnit']}
const selectedFormula=computed(()=>formulaOptions.find(item=>item.value===Number(pricingCalculation.value.formula)))
const visibleCalculationFields=computed(()=>{const keys=formulaFieldKeys[Number(pricingCalculation.value.formula)]||[];return calculationFields.filter(field=>keys.includes(field.key))})
type CustomerOption={id:string;name:string;code:string;value:string}
type ContactOption={id:string;name:string;value:string;isPrimary:boolean}
const customers=ref<CustomerOption[]>([]),contacts=ref<ContactOption[]>([])
async function suggestCustomers(query:string,done:(rows:CustomerOption[])=>void){try{if(!customers.value.length){const r=await get<{customers:CustomerOption[]}>('/sourcing-customer-options',{page_size:500});customers.value=r.customers.map(c=>({...c,value:c.name}))}done(customers.value.filter(c=>`${c.name} ${c.code}`.toLowerCase().includes(query.toLowerCase())).slice(0,50))}catch{done([])}}
async function suggestContacts(query:string,done:(rows:ContactOption[])=>void){try{if(!offer.value?.body.customerId){done([]);return}const r=await get<{contacts:ContactOption[]}>(`/sourcing-customer-options/${offer.value.body.customerId}/contacts`);contacts.value=r.contacts.map(c=>({...c,value:c.name}));done(contacts.value.filter(c=>c.name.toLowerCase().includes(query.toLowerCase())))}catch{done([])}}
function chooseCustomer(c:CustomerOption){if(!offer.value)return;offer.value.body.customerId=c.id;offer.value.body.customer=c.name;offer.value.body.contact='';offer.value.body.contactId='';contacts.value=[]}
function chooseContact(c:ContactOption){if(!offer.value)return;offer.value.body.contactId=c.id;offer.value.body.contact=c.name}
async function command(action:string){return post<Offer>('/customer-offer',{action,caseId:props.caseId,revision:offer.value?.revision||0,body:offer.value?.body})}
let loadSequence=0
const productTotal=computed(()=>offerProductTotal(offer.value?.body.lines||[]))
const specification=offerSpecification
function prepare(view:Offer){view.body.transports??=[];view.body.lines.forEach(l=>{l.calculation={...emptyCalculation(),...l.calculation};if(view.canEdit)selectOnlyFactory(l,view.source.quotes)});return view}
async function load(){if(busy.value)return;const seq=++loadSequence,caseId=props.caseId;busy.value=true;error.value='';try{const next=prepare(await command('get'));if(seq!==loadSequence||caseId!==props.caseId)return;if(offer.value&&offer.value.canEdit&&next.canEdit){offer.value.source=next.source;offer.value.body.lines.forEach(l=>selectOnlyFactory(l,next.source.quotes))}else offer.value=next}catch{error.value='客户报价加载失败，请重试'}finally{busy.value=false}}
async function save(){if(busy.value)return;busy.value=true;try{offer.value=await command('save');ElMessage.success('客户报价已保存')}catch{}finally{busy.value=false}}
function confirm(){confirmationOpen.value=true}
async function completeConfirmation(){if(busy.value)return;busy.value=true;try{offer.value=await command('save');offer.value=await command('confirm');confirmationOpen.value=false;goContract()}catch{}finally{busy.value=false}}
function goContract(){if(offer.value)router.push({path:'/contracts',query:{id:offer.value.contractId}})}
async function download(){if(busy.value)return;busy.value=true;try{if(offer.value?.canEdit)offer.value=await command('save');const file=await post<{fileData:string;fileName:string}>('/customer-offer',{action:'pdf',caseId:props.caseId});const data=Uint8Array.from(atob(file.fileData),c=>c.charCodeAt(0)),url=URL.createObjectURL(new Blob([data],{type:'application/pdf'})),a=document.createElement('a');a.href=url;a.download=file.fileName;a.click();setTimeout(()=>URL.revokeObjectURL(url),1000)}catch{}finally{busy.value=false}}
function openCalculation(){const first=offer.value?.body.lines[0]?.calculation||emptyCalculation();pricingCalculation.value={...first,factory:''};calculationQuoteId.value='';calculationOpen.value=true}
function selectCalculationQuote(value:string){const quote=logisticsQuotes.value.find(q=>q.id===value);if(!quote)return;const charge=quote.body.charges.find(row=>row.currency.toUpperCase()==='USD'&&/(海运|ocean)/i.test(row.name));pricingCalculation.value.ocean=charge?.amount||'';if(quote.body.transitDays)pricingCalculation.value.days=quote.body.transitDays}
async function calculateAll(){if(busy.value||!offer.value)return;offer.value.body.lines.forEach(line=>line.calculation={...pricingCalculation.value,factory:line.calculation.factory});busy.value=true;try{offer.value=prepare(await command('calculate_all'));calculationOpen.value=false;ElMessage.success('已统一计算全部产品单价，可继续逐项修改')}catch{}finally{busy.value=false}}
 function selectFactory(line:OfferLine){const q=offer.value?.source.quotes.find(q=>q.id===line.factoryQuoteId);line.calculation.factory=q?.body.prices.find(p=>p.productId===line.id)?.price||'';line.calculatedPrice=''}
function addProduct(){if(!offer.value)return;offer.value.body.lines.push({...blankProduct(),id:crypto.randomUUID(),factoryQuoteId:'',calculation:emptyCalculation(),calculatedPrice:'',unitPrice:'',amount:''});search.value='';page.value=Math.ceil(offer.value.body.lines.length/20)}
function removeProduct(line:OfferLine){if(!offer.value)return;offer.value.body.lines=offer.value.body.lines.filter(l=>l.id!==line.id);offer.value.body.transports.forEach(t=>delete t.quantities[line.id]);page.value=Math.min(page.value,Math.max(1,Math.ceil(filteredLines.value.length/20)))}
 function sourceQuoteTitle(q:Quote){return q.body.company||q.body.carrier||`物流报价 ${q.id}`}
 function sourceQuoteRoute(q:Quote){return [q.body.route,q.body.carrier,q.body.loadingPort&&q.body.destinationPort?`${q.body.loadingPort} → ${q.body.destinationPort}`:''].filter(Boolean).join(' · ')||'未填写航线信息'}
 function sourceQuoteTotals(q:Quote){const entries=Object.entries(q.body.totals||{});return entries.length?entries.map(([currency,value])=>`${currency} ${value}`).join('；'):'费用未填写'}
 function displayTime(value:string){if(!value)return '—';const date=new Date(value);return Number.isNaN(date.getTime())?value:date.toLocaleString('zh-CN',{year:'numeric',month:'2-digit',day:'2-digit',hour:'2-digit',minute:'2-digit',hour12:false})}
 function cargoLines(q:Quote){const lines=offer.value?.source.body.products||[];return q.body.cargoIds?.length?lines.filter(l=>q.body.cargoIds.includes(l.id)):lines}
 function selectedTransport(id:string){return offer.value?.body.transports.find(t=>t.quoteId===id)}
 function hasTransport(id:string){return !!offer.value?.body.transports.some(row=>row.quoteId===id)}
 function transportFromQuote(q:Quote):Transport{const totals=Object.entries(q.body.totals||{}),single=totals.length===1?totals[0]:undefined;return{quoteId:q.id,title:q.body.route||q.body.carrier||q.body.company||`物流报价 ${q.id}`,currency:single?.[0]||offer.value?.body.currency||'USD',price:single?.[1]||'',remark:'',accepted:false,quantities:{}}}
 function toggleTransport(q:Quote,yes:boolean){if(!offer.value)return;if(yes&&!hasTransport(q.id))offer.value.body.transports.push(transportFromQuote(q));if(!yes)offer.value.body.transports=offer.value.body.transports.filter(row=>row.quoteId!==q.id)}
function setAllocation(id:string,value:string){if(!transport.value)return;if(value.trim())transport.value.quantities[id]=value;else delete transport.value.quantities[id]}
 watch(search,()=>page.value=1);watch(()=>props.caseId,()=>{loadSequence++;offer.value=null;page.value=1;calculationOpen.value=false;pricingCalculation.value=emptyCalculation();calculationQuoteId.value='';transport.value=null;confirmationOpen.value=false;busy.value=false;void load()},{immediate:true});watch(()=>props.sourceVersion,()=>{void load()})
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
</style>
