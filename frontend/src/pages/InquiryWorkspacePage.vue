<template>
 <main class="inquiry-workspace" v-loading="busy">
  <header v-if="!item" class="workspace-heading">
   <div><el-tag v-if="department" class="module-chip" effect="plain">报价协作</el-tag><h2>{{ title }}</h2><p>{{view==='SALES'?'整理客户需求，提交询价并跟进处理进度。':view==='QUOTATIONS'?'查看采购与物流已提交的报价。':'查看客户需求，填写并提交报价。'}}</p></div>
   <div v-if="view==='SALES'" class="heading-actions"><el-button @click="router.push('/sales/settings/inquiry-templates')">询盘模板</el-button><el-button type="primary" plain @click="router.push('/emails')">从邮箱转入询盘</el-button></div>
  </header>
  <div v-if="!item" class="toolbar list-toolbar"><el-input v-model="keyword" placeholder="搜索编号、客户、产品或规格" clearable @change="loadList"/><el-select v-model="state" clearable placeholder="全部状态" @change="loadList"><el-option v-for="o in states" :key="o.value" :value="o.value" :label="o.label"/></el-select><el-button @click="loadList">刷新</el-button><el-button v-if="view==='SALES'&&canWrite" type="primary" @click="openCreateDialog">上传或新建客户询盘</el-button></div>
  <template v-if="!item">
   <el-table :data="items" stripe @row-dblclick="open"><el-table-column prop="number" label="询盘编号" min-width="170"/><el-table-column prop="body.customer" label="客户" min-width="180"/><el-table-column prop="owner" label="负责销售" width="120"/><el-table-column label="产品项数" width="100"><template #default="{row}">{{row.body.products?.length||0}}</template></el-table-column><el-table-column v-if="department" label="总需求数量" min-width="140"><template #default="{row}">{{productTotal(row.body.products,'quantity','unit')}}</template></el-table-column><el-table-column v-if="view==='LOGISTICS'" label="总重量 / 总体积" min-width="180"><template #default="{row}">{{productTotal(row.body.products,'weight')}} / {{productTotal(row.body.products,'volume')}}</template></el-table-column><el-table-column v-if="department" prop="body.delivery" label="交货要求" min-width="140"/><el-table-column v-if="view==='LOGISTICS'" prop="body.loadingPort" label="装货港" min-width="120"/><el-table-column v-if="view==='LOGISTICS'" prop="body.destinationPort" label="目的港" min-width="120"/><el-table-column v-if="view==='LOGISTICS'" prop="body.incoterm" label="贸易条件" min-width="100"/><el-table-column v-if="department" label="已提交报价数" width="120"><template #default="{row}">{{view==='PROCUREMENT'?row.procurementCount:row.logisticsCount}}</template></el-table-column><el-table-column label="提交时间" min-width="180"><template #default="{row}">{{displayTime(row.submittedAt)}}</template></el-table-column><el-table-column label="状态" width="110"><template #default="{row}"><el-tag effect="light" :type="row.state==='WITHDRAWN'?'info':row.state==='INQUIRING'?'success':'warning'">{{view==='QUOTATIONS'?offerStateLabel(row.id):stateLabel(row)}}</el-tag></template></el-table-column><el-table-column label="操作" width="150" fixed="right"><template #default="{row}"><el-button link type="primary" @click="open(row)">{{department?((view==='PROCUREMENT'?row.procurementCount:row.logisticsCount)>0?'查看并报价':'去报价'):'打开详情 →'}}</el-button></template></el-table-column></el-table>
   <el-pagination v-model:current-page="page" v-model:page-size="size" :page-sizes="[20,50,100]" :total="total" layout="total, sizes, prev, pager, next" @change="loadList"/>
  </template>
  <template v-else>
   <section class="detail-hero">
     <div><span class="detail-kicker">{{item.number?'询盘编号':'正在新建'}}</span><h3>{{item.number||'新建客户询盘'}}</h3><div v-if="item.body.title" class="detail-title">{{item.body.title}}</div><div class="detail-meta"><el-tag :type="item.state==='INQUIRING'?'success':item.state==='WITHDRAWN'?'info':'warning'">{{stateLabel(item)}}</el-tag><span>负责销售：{{item.owner}}</span><span>{{item.body.products.length}} 项产品</span><span v-if="view!=='SALES'">{{item.quotes?.length||0}} 份报价</span></div></div>
     <div class="detail-actions"><el-button v-if="view==='SALES'&&item.canEdit&&item.state!=='INQUIRING'&&auth.can('sales:inquiry:submit')" type="primary" @click="submit">{{item.state==='WITHDRAWN'?'重新提交询价':'提交询价'}}</el-button><el-button v-if="department&&canWrite&&!editor" type="primary" @click="editQuote()">{{view==='PROCUREMENT'?'添加工厂报价':'添加货代报价'}}</el-button><el-dropdown trigger="click" @command="handleMore"><el-button class="more-actions">更多操作 <span class="more-arrow">⌄</span></el-button><template #dropdown><el-dropdown-menu><el-dropdown-item command="back">返回列表</el-dropdown-item><el-dropdown-item command="reload">刷新最新数据</el-dropdown-item><el-dropdown-item v-if="view==='SALES'&&item.canEdit&&item.state!=='INQUIRING'" divided command="save">保存草稿</el-dropdown-item><el-dropdown-item v-if="view==='SALES'&&item.canEdit&&item.state==='INQUIRING'" divided command="withdraw">撤回询价</el-dropdown-item></el-dropdown-menu></template></el-dropdown></div>
    </section>
    <el-tabs v-model="detailTab" class="detail-tabs">
<el-tab-pane v-if="view==='QUOTATIONS'" label="给客户的报价" name="offer"><CustomerOfferEditor :case-id="item.id" /></el-tab-pane>
     <el-tab-pane label="概览与附件" name="overview">
     <div class="overview-grid">
     <section class="business-card customer-card"><div class="section-title"><div><h3>客户与交付信息</h3><p>可从客户资料中选择，也可直接输入临时客户或联系人。</p></div></div><el-form label-position="top" :disabled="!editable" class="inquiry-fields"><el-form-item label="客户"><el-autocomplete v-if="editable" v-model="item.body.customer" :fetch-suggestions="customerSuggestions" placeholder="搜索客户编码或名称，也可直接输入" clearable @select="selectCustomer" @input="customerTyped"><template #default="{item:option}"><div class="option-title">{{option.name}}</div><span class="option-meta">{{option.code}}</span></template></el-autocomplete><el-input v-else v-model="item.body.customer" placeholder="—"/></el-form-item><el-form-item label="联系人"><el-autocomplete v-if="editable" v-model="item.body.contact" :fetch-suggestions="contactSuggestions" placeholder="选择客户联系人，也可直接输入" clearable @select="selectContact" @input="contactTyped"><template #default="{item:option}"><div class="option-title">{{option.name}}</div><span class="option-meta">{{contactOptionLabel(option)}}</span></template></el-autocomplete><el-input v-else v-model="item.body.contact" placeholder="—"/></el-form-item><el-form-item label="交货要求"><el-input v-model="item.body.delivery" placeholder="—"/></el-form-item><el-form-item label="装货港"><el-input v-model="item.body.loadingPort" placeholder="—"/></el-form-item><el-form-item label="目的港"><el-input v-model="item.body.destinationPort" placeholder="—"/></el-form-item><el-form-item label="贸易条款"><el-input v-model="item.body.incoterm" placeholder="—"/></el-form-item><el-form-item label="备注" class="wide"><el-input v-model="item.body.remark" type="textarea" :rows="2" placeholder="—"/></el-form-item></el-form></section>
     <section class="business-card attachment-card"><div class="attachment-heading"><div><h3>客户附件</h3><p>{{item.body.attachments.length?`共 ${item.body.attachments.length} 个原始文件`:'暂无附件'}}</p></div><input v-if="editable" class="file-input" type="file" @change="upload($event,false)"/></div><div v-if="item.body.attachments.length" class="attachment-list"><el-button v-for="a in item.body.attachments" :key="a.key" plain @click="download(a.key)">📎 {{a.name}}</el-button></div><div v-else class="attachment-empty">客户原始文件会显示在这里</div></section>
     </div></el-tab-pane>
     <el-tab-pane :label="`产品需求 (${item.body.products.length})`" name="products"><section class="business-card product-card"><div class="section-title"><div><h3>产品需求</h3><p>表格列按询盘选用的模板显示；保存后固定为当时的模板版本。</p></div><div class="template-actions"><template v-if="view==='SALES'&&editable"><span class="template-label">询盘模板</span><el-select :model-value="item.body.template?.id" placeholder="选择模板" @change="changeTemplate"><el-option v-for="template in selectableTemplates" :key="template.id" :label="`${template.name} · v${template.version}${template.status==='ACTIVE'?'':'（历史版本）'}`" :value="template.id"/></el-select><el-button type="primary" plain @click="addProduct">添加产品</el-button></template><el-tag v-else-if="item.body.template" effect="plain">{{item.body.template.name}} · v{{item.body.template.version}}</el-tag></div></div><el-collapse v-if="editor" v-model="requirementsOpen"><el-collapse-item title="查看客户产品需求" name="products"><InquiryProducts :products="item.body.products" :fields="templateFields"/></el-collapse-item></el-collapse><InquiryProducts v-else :products="item.body.products" :fields="templateFields" :editable="!!editable" @remove="item.body.products.splice($event,1)"/></section></el-tab-pane>
     <el-tab-pane v-if="view!=='SALES'" :label="`已提交报价 (${item.quotes?.length||0})`" name="quotes">
     <section v-if="!editor" class="business-card quotes-section"><div class="section-title"><div><h3>{{view==='QUOTATIONS'?'报价对比':view==='PROCUREMENT'?'工厂报价':'货代报价'}}</h3><p>一行一份报价；点击查看完整报价后再看全部产品、费用与附件。</p></div><el-tag effect="plain">{{item.quotes?.length||0}} 份报价</el-tag></div><el-table :data="item.quotes||[]" :max-height="500" stripe empty-text="暂无报价"><el-table-column prop="body.company" label="报价方" min-width="180" show-overflow-tooltip/><el-table-column label="类型" width="100"><template #default="{row:q}"><el-tag size="small" :type="q.kind==='PROCUREMENT'?'warning':'success'">{{q.kind==='PROCUREMENT'?'工厂':'货代'}}</el-tag></template></el-table-column><el-table-column label="关键报价" min-width="250"><template #default="{row:q}"><strong class="compact-quote">{{q.kind==='PROCUREMENT'?`${q.body.prices?.length||0}/${item.body.products.length} 项 · ${q.body.currency||'—'}`:quoteTotals(q)||'费用未填写'}}</strong><small>{{q.kind==='PROCUREMENT'?`交期：${q.body.delivery||'按产品'}`:`${q.body.carrier||'船公司未填'} · ${q.body.route||'方案未填'}`}}</small></template></el-table-column><el-table-column label="报价人 / 修改人" min-width="180"><template #default="{row:q}">{{q.author||'—'}}<small>{{q.updatedBy&&q.updatedBy!==q.author?`修改：${q.updatedBy}`:''}}</small></template></el-table-column><el-table-column label="提交时间" min-width="155"><template #default="{row:q}">{{displayTime(q.submittedAt)}}</template></el-table-column><el-table-column label="状态" width="100"><template #default="{row:q}"><el-tag size="small" effect="plain" :type="q.historical?'info':q.submittedAt?'success':'warning'">{{q.historical?'历史版本':q.submittedAt?'已提交':'已保存'}}</el-tag></template></el-table-column><el-table-column label="操作" width="180" fixed="right"><template #default="{row:q}"><el-button v-if="q.canEdit" link type="primary" @click="editQuote(q)">修改</el-button><el-button link type="primary" @click="preview=q">查看完整报价</el-button></template></el-table-column></el-table></section>
   <section v-if="editor" class="quote-editor"><h3>{{view==='PROCUREMENT'?'工厂报价':'货代报价'}}</h3><el-form label-position="top" class="inquiry-fields"><el-form-item :label="view==='PROCUREMENT'?'工厂或供应商':'货代公司'"><el-autocomplete v-model="editor.body.company" :fetch-suggestions="companySuggestions" placeholder="选择已有数据或直接输入" :trigger-on-focus="true"/></el-form-item><template v-if="view==='PROCUREMENT'"><el-form-item label="币种"><el-input v-model="editor.body.currency" maxlength="3"/></el-form-item><el-form-item label="统一预计交货日期"><el-date-picker v-model="editor.body.delivery" type="date" value-format="YYYY-MM-DD"/></el-form-item></template><template v-else><el-form-item v-for="f in logisticsFields" :key="f.key" :label="f.label"><el-input v-model="editor.body[f.key]"/></el-form-item><el-form-item label="预计开船日期"><el-date-picker v-model="editor.body.departure" value-format="YYYY-MM-DD"/></el-form-item><el-form-item label="预计到港日期"><el-date-picker v-model="editor.body.arrival" value-format="YYYY-MM-DD"/></el-form-item></template></el-form>
    <el-collapse><el-collapse-item title="更多信息" name="more"><el-form label-position="top" class="inquiry-fields"><el-form-item label="报价有效期"><el-date-picker v-model="editor.body.validUntil" value-format="YYYY-MM-DD"/></el-form-item><el-form-item label="付款方式"><el-input v-model="editor.body.paymentTerms"/></el-form-item><el-form-item label="报价附件" class="wide"><input type="file" @change="upload($event,true)"/><el-tag v-for="a in editor.body.attachments" :key="a.key" closable @close="editor!.body.attachments=editor!.body.attachments.filter(x=>x.key!==a.key)">{{a.name}}</el-tag></el-form-item><el-form-item label="备注" class="wide"><el-input v-model="editor.body.remark" type="textarea"/></el-form-item></el-form></el-collapse-item></el-collapse>
    <template v-if="view==='PROCUREMENT'"><div class="toolbar"><el-input v-model="productSearch" placeholder="搜索产品或规格" @input="productPage=1"/><el-select v-model="productFilter" @change="productPage=1"><el-option label="全部产品" value="all"/><el-option label="未报价产品" value="unquoted"/><el-option label="已报价产品" value="quoted"/></el-select><span>已报价 {{editor.body.prices.filter(p=>p.price!=='').length}} / {{item.body.products.length}} 项</span></div><el-table :data="pagedProducts" row-key="id" :max-height="520"><el-table-column label="选择" width="65"><template #default="{row}"><el-checkbox :model-value="!!priceFor(row.id)" @change="toggleProduct(row.id,!!$event)"/></template></el-table-column><el-table-column prop="product" label="产品" min-width="180"/><el-table-column prop="specification" label="规格" min-width="190"/><el-table-column prop="quantity" label="需求数量" width="100"/><el-table-column prop="unit" label="单位" width="75"/><el-table-column label="工厂单价" min-width="160"><template #default="{row}"><el-input v-if="priceFor(row.id)" v-model="priceFor(row.id)!.price" @paste="paste($event,row.id)"/></template></el-table-column><el-table-column label="交货日期" min-width="180"><template #default="{row}"><el-date-picker v-if="priceFor(row.id)" v-model="priceFor(row.id)!.delivery" value-format="YYYY-MM-DD" :placeholder="editor.body.delivery||'交货日期'"/></template></el-table-column><el-table-column label="备注" min-width="170"><template #default="{row}"><el-input v-if="priceFor(row.id)" v-model="priceFor(row.id)!.remark"/></template></el-table-column></el-table><p>从 Excel 复制价格，在起始产品的单价框粘贴；可同时粘贴单价、交货日期、备注三列，按当前筛选产品顺序填入。</p><el-pagination v-model:current-page="productPage" v-model:page-size="productSize" :page-sizes="[20,50,100]" :total="filteredProducts.length" layout="total, sizes, prev, pager, next"/></template>
     <template v-else><h4>适用货物（不勾选时按整批）</h4><InquiryProducts :products="item.body.products" :fields="templateFields" v-model:cargo-ids="editor.body.cargoIds"/><h4>运输费用</h4><el-table :data="editor.body.charges" :max-height="420"><el-table-column v-for="f in chargeFields" :key="f.key" :label="f.label" :min-width="f.width"><template #default="{row}"><span v-if="f.key==='subtotal'">{{chargeSubtotal(row.amount,row.quantity)||'—'}}</span><el-input v-else v-model="row[f.key]"/></template></el-table-column><el-table-column width="70"><template #default="{$index}"><el-button link type="danger" @click="editor!.body.charges.splice($index,1)">移除</el-button></template></el-table-column></el-table><el-button @click="addCharge">添加费用</el-button><p v-for="(v,k) in chargeTotals(editor.body.charges)" :key="k">{{k}} 合计：{{v}}</p></template>
     <div class="toolbar"><el-button type="primary" @click="saveQuote(false)">保存</el-button><el-button v-if="!editor.submittedAt" type="success" @click="saveQuote(true)">提交报价</el-button><el-button @click="editor=null">返回询盘详情</el-button></div>
    </section>
     </el-tab-pane>
    </el-tabs>
   </template>
   <el-dialog v-model="uploadOpen" title="上传标准询盘" width="min(720px,94vw)" destroy-on-close>
    <el-alert title="仅接收已经标准化的 Excel 或 CSV；上传后直接在网页中人工复核。最大 8MB。" type="info" :closable="false" show-icon/>
    <el-form label-position="top" class="upload-form">
     <el-form-item label="询盘格式"><div class="upload-template"><el-select v-model="uploadForm.templateId" filterable placeholder="自动识别，或手工指定格式"><el-option value="" label="自动识别文件表头"/><el-option v-for="template in activeTemplates" :key="template.id" :value="String(template.id)" :label="`${template.name} · v${template.version}`"/></el-select><el-button :disabled="!uploadForm.templateId" @click="downloadSelectedTemplate">下载所选模板</el-button></div><small>系统按文件表头匹配生效格式；不确定时可先留空自动识别。</small></el-form-item>
     <el-form-item label="询盘标题"><el-input v-model="uploadForm.title" placeholder="留空则使用文件名"/></el-form-item>
     <el-form-item label="客户" required><el-select v-model="uploadForm.customerId" filterable placeholder="搜索客户编号或名称" style="width:100%" @change="uploadCustomerChanged"><el-option v-for="option in customerOptions" :key="option.id" :value="option.id" :label="`${option.code} · ${option.name}`"/></el-select></el-form-item>
     <el-form-item label="客户联系人" required><el-select v-model="uploadForm.contactId" filterable :disabled="!uploadForm.customerId" :placeholder="uploadForm.customerId?'选择客户联系人':'请先选择客户'" style="width:100%"><el-option v-for="option in contactOptions" :key="option.id" :value="option.id" :label="`${option.name}${contactOptionLabel(option)?` · ${contactOptionLabel(option)}`:''}`"/></el-select></el-form-item>
     <el-form-item label="联系邮箱"><el-input :model-value="selectedUploadContact?.email||''" readonly placeholder="选择联系人后自动带出"/></el-form-item>
     <el-form-item label="标准文件" required><input type="file" accept=".xlsx,.csv" @change="pickImportFile"/></el-form-item>
    </el-form>
    <template #footer><el-button @click="uploadOpen=false">取消</el-button><el-button @click="startManualInquiry">少量产品手工录入</el-button><el-button type="primary" :loading="busy" @click="importInquiry">上传并开始复核</el-button></template>
   </el-dialog>
   <el-dialog :model-value="!!preview" title="完整报价" width="92%" @close="preview=null"><template v-if="preview&&item"><div class="toolbar"><el-button v-for="a in preview.body.attachments" :key="a.key" link @click="download(a.key)">{{a.name}}</el-button></div><el-descriptions :column="3" border><el-descriptions-item v-for="f in quoteDisplayFields" :key="f.key" :label="f.label">{{preview.body[f.key]||'—'}}</el-descriptions-item></el-descriptions><el-table v-if="preview.kind==='PROCUREMENT'" :data="preview.body.prices" :max-height="480"><el-table-column label="产品"><template #default="{row}">{{productFor(row.productId)?.product}}</template></el-table-column><el-table-column label="规格"><template #default="{row}">{{productFor(row.productId)?.specification}}</template></el-table-column><el-table-column prop="price" label="单价"/><el-table-column label="交货日期"><template #default="{row}">{{row.delivery||preview.body.delivery||'—'}}</template></el-table-column><el-table-column prop="remark" label="备注"/></el-table><template v-else><p>适用货物：{{preview.body.cargoIds?.length?preview.body.cargoIds.map(id=>productFor(id)?.product).join('、'):'整批'}}</p><el-table :data="preview.body.charges" :max-height="480"><el-table-column v-for="f in chargeFields" :key="f.key" :prop="f.key" :label="f.label"/></el-table><p v-for="(v,k) in preview.body.totals" :key="k">{{k}} 合计：{{v}}</p></template></template></el-dialog>
 </main>
</template>
<script setup lang="ts">
import {computed,onMounted,onUnmounted,reactive,ref,watch} from 'vue'
import {useRoute,useRouter} from 'vue-router'
import {ElMessage,ElMessageBox} from 'element-plus'
import {get,post} from '../api'
import {isAxiosError} from 'axios'
import InquiryProducts from '../components/InquiryProducts.vue'
import CustomerOfferEditor from '../components/CustomerOfferEditor.vue'
import {onLive} from '../live'
import {useAuthStore} from '../stores/auth'
import type {InquiryTemplate} from '../lib/inquiryTemplates'
import {parseTableFile} from '../lib/attachmentExcel'
import {productsFromImportedSheet,selectImportTemplate} from '../lib/inquiryImport'
import {applyTemplateDefaults,blankBody,blankProduct,blankQuote,pastePrices,chargeSubtotal,chargeTotals,productTotal,type Inquiry,type InquiryTemplateSnapshot,type Result,type Quote,type Product} from '../lib/inquiryWorkspace'
const props=defineProps<{view:'SALES'|'QUOTATIONS'|'PROCUREMENT'|'LOGISTICS'}>()
const view=computed(()=>props.view),route=useRoute(),router=useRouter(),auth=useAuthStore()
const title=computed(()=>({SALES:'客户询盘',QUOTATIONS:'客户报价',PROCUREMENT:'采购询价',LOGISTICS:'物流询价'}[view.value]))
const department=computed(()=>view.value==='PROCUREMENT'||view.value==='LOGISTICS')
const canWrite=computed(()=>auth.can(view.value==='SALES'?'sales:inquiry:write':view.value==='PROCUREMENT'?'procurement:sourcing:write':'shipping:sourcing:write'))
const offerStates=ref<Record<string,{status:string;confirmedAt:string}>>({})
const item=ref<Inquiry|null>(null),items=ref<Inquiry[]>([]),total=ref(0),keyword=ref(''),state=ref(''),page=ref(1),size=ref(20),busy=ref(false),editor=ref<Quote|null>(null),preview=ref<Quote|null>(null),detailTab=ref('overview')
interface CustomerOption {value:string;id:string;code:string;name:string}
interface ContactOption {value:string;id:string;name:string;department:string;title:string;email:string;isPrimary:boolean}
const customerOptions=ref<CustomerOption[]>([]),contactOptions=ref<ContactOption[]>([])
const uploadOpen=ref(false)
const uploadForm=reactive<{templateId:string;title:string;customerId:string;contactId:string;file:File|null}>({templateId:'',title:'',customerId:'',contactId:'',file:null})
const selectedUploadContact=computed(()=>contactOptions.value.find(option=>option.id===uploadForm.contactId))
const templates=ref<InquiryTemplate[]>([])
const activeTemplates=computed(()=>templates.value.filter(template=>template.status==='ACTIVE'))
const templateFields=computed(()=>item.value?.body.template?.fields)
const selectableTemplates=computed(()=>{const rows=[...activeTemplates.value];const current=item.value?.body.template;if(current&&!rows.some(row=>row.id===current.id))rows.push({...current,description:'',status:'SUPERSEDED',isDefault:false,isSystem:false,fieldCount:current.fields.length,createdByName:'',updatedAt:''});return rows})
const editable=computed(()=>view.value==='SALES'&&item.value?.canEdit&&item.value.state!=='INQUIRING')
const states=computed(()=>department.value?[{value:'WAITING',label:'待报价'},{value:'QUOTED',label:'已有报价'}]:view.value==='SALES'?[{value:'UNSUBMITTED',label:'未提交'},{value:'INQUIRING',label:'询价中'},{value:'WITHDRAWN',label:'已撤回'}]:[{value:'PENDING',label:'待报价'},{value:'QUOTED',label:'已报价'},{value:'CONFIRMED',label:'客户已确认'}])
const productFields=[{key:'product',label:'产品',width:180},{key:'specification',label:'规格',width:190},{key:'quantity',label:'数量',width:100},{key:'unit',label:'单位',width:80},{key:'delivery',label:'交货要求',width:150},{key:'weight',label:'重量',width:100},{key:'volume',label:'体积',width:100},{key:'packaging',label:'包装',width:130},{key:'remark',label:'备注',width:190}]
const logisticsFields=[{key:'carrier',label:'实际船公司'},{key:'route',label:'运输方案'},{key:'vessel',label:'船名'},{key:'voyage',label:'航次'},{key:'loadingPort',label:'装货港'},{key:'destinationPort',label:'目的港'},{key:'transitDays',label:'预计航程'}] as const
const quoteDisplayFields=[{key:'company',label:'工厂 / 货代'},{key:'currency',label:'币种'},{key:'delivery',label:'统一交货日期'},{key:'validUntil',label:'报价有效期'},{key:'paymentTerms',label:'付款方式'},...logisticsFields,{key:'departure',label:'开船日期'},{key:'arrival',label:'到港日期'},{key:'remark',label:'备注'}] as const
const chargeFields=[{key:'name',label:'费用名称',width:140},{key:'amount',label:'金额',width:110},{key:'currency',label:'币种',width:90},{key:'unit',label:'计价单位',width:110},{key:'quantity',label:'数量',width:100},{key:'subtotal',label:'小计',width:110},{key:'remark',label:'备注',width:170}]
const productSearch=ref(''),productFilter=ref('all'),productPage=ref(1),productSize=ref(20),requirementsOpen=ref<string[]>([])
const filteredProducts=computed(()=>(item.value?.body.products||[]).filter(p=>(`${p.product} ${p.specification}`.toLowerCase().includes(productSearch.value.toLowerCase()))&&(productFilter.value==='all'||(productFilter.value==='quoted')===!!priceFor(p.id)?.price)))
const pagedProducts=computed(()=>filteredProducts.value.slice((productPage.value-1)*productSize.value,productPage.value*productSize.value))
function priceFor(id:string){return editor.value?.body.prices.find(p=>p.productId===id)}
function productFor(id:string){return item.value?.body.products.find(p=>p.id===id)}
function quoteTotals(q:Quote){return Object.entries(q.body.totals||{}).map(([currency,value])=>`${currency} ${value}`).join('；')}
function toggleProduct(id:string,yes:boolean){if(!editor.value)return;if(yes&&!priceFor(id))editor.value.body.prices.push({productId:id,price:'',delivery:'',remark:''});else if(!yes)editor.value.body.prices=editor.value.body.prices.filter(p=>p.productId!==id)}
function paste(e:ClipboardEvent,id:string){const text=e.clipboardData?.getData('text')||'';if(editor.value&&/[\t\n]/.test(text)){e.preventDefault();editor.value.body.prices=pastePrices(text,filteredProducts.value,editor.value.body.prices,id)}}
function displayTime(value:string){if(!value)return '—';const date=new Date(value);return Number.isNaN(date.getTime())?value:date.toLocaleString('zh-CN',{year:'numeric',month:'2-digit',day:'2-digit',hour:'2-digit',minute:'2-digit',hour12:false})}
function stateLabel(r:Inquiry){return department.value?((view.value==='PROCUREMENT'?r.procurementCount:r.logisticsCount)>0?'已有报价':'待报价'):({UNSUBMITTED:'未提交',INQUIRING:'询价中',WITHDRAWN:'已撤回'}[r.state]||r.state)}
const retryKeys=new Map<string,string>()
async function command(action:string,extra:object={}){const body={action,view:view.value,...extra};if(['list','get'].includes(action))return post<Result>('/inquiry-workspace',body);const fingerprint=JSON.stringify(body);let key=retryKeys.get(fingerprint);if(!key){key=crypto.randomUUID();retryKeys.set(fingerprint,key)}const r=await post<Result>('/inquiry-workspace',body,{headers:{'Idempotency-Key':key}});retryKeys.delete(fingerprint);return r}
function offerStateLabel(id:string){const s=offerStates.value[id]?.status;return s==='CONFIRMED'?'客户已确认':s==='QUOTED'?'已报价':'待报价'}
let listRequest=0
async function loadList(){
 const request=++listRequest
 if(view.value==='QUOTATIONS'&&auth.can('export:quotation:read'))offerStates.value=(await post<{summaries:Record<string,{status:string;confirmedAt:string}>}>('/customer-offer',{action:'summaries'})).summaries
 if(view.value==='QUOTATIONS'&&state.value){
  const matches:Inquiry[]=[];let sourcePage=1,sourceTotal=0
  do{const r=await command('list',{page:sourcePage,size:100,keyword:keyword.value,state:''});if(request!==listRequest)return;sourceTotal=r.total;matches.push(...r.items.filter(i=>(offerStates.value[i.id]?.status||'PENDING')===state.value));if(!r.items.length)break;sourcePage++}while((sourcePage-1)*100<sourceTotal)
  total.value=matches.length;page.value=Math.min(page.value,Math.max(1,Math.ceil(matches.length/size.value)));items.value=matches.slice((page.value-1)*size.value,page.value*size.value);return
 }
 const r=await command('list',{page:page.value,size:size.value,keyword:keyword.value,state:state.value});if(request!==listRequest)return;items.value=r.items;total.value=r.total
}

async function reload(){if(item.value?.id){try{const r=await command('get',{id:item.value.id});item.value=normalizeInquiry(r.item)}catch(e){if(isAxiosError(e)&&[403,404].includes(e.response?.status||0)){item.value=null;preview.value=null;await router.replace({query:{}});await loadList()}else throw e}}else await loadList()}
function initialDetailTab(){return view.value==='SALES'?'overview':view.value==='QUOTATIONS'?'offer':'products'}
function normalizeInquiry(inquiry:Inquiry|undefined):Inquiry|null{if(!inquiry)return null;inquiry.body.title??='';inquiry.body.customerId??='';inquiry.body.contactId??='';inquiry.body.products??=[];inquiry.body.attachments??=[];inquiry.quotes??=[];inquiry.body.products.forEach(product=>{product.customFields??={}});return inquiry}
async function open(r:Inquiry){editor.value=null;detailTab.value=initialDetailTab();const out=await command('get',{id:r.id});item.value=normalizeInquiry(out.item);await router.replace({query:{...route.query,id:r.id}})}
async function back(){if(editor.value||editable.value){await ElMessageBox.confirm('返回列表？请先保存需要保留的修改。','提示')}item.value=null;editor.value=null;await router.replace({query:{}});await loadList()}
function templateSnapshot(template:InquiryTemplate):InquiryTemplateSnapshot{return{id:template.id,templateCode:template.templateCode,version:template.version,name:template.name,fields:(template.fields||[]).map(field=>({...field})).sort((a,b)=>a.sortOrder-b.sortOrder)}}
async function loadTemplates(){if(view.value!=='SALES')return;const data=await get<{templates:InquiryTemplate[]}>('/inquiry-templates');templates.value=data.templates||[]}
function create(){const body=blankBody(),template=activeTemplates.value.find(row=>row.isDefault)||activeTemplates.value[0];if(template){body.template=templateSnapshot(template);body.products.forEach(product=>applyTemplateDefaults(product,body.template!.fields))}detailTab.value='overview';item.value={id:'',number:'',ownerId:'',owner:'当前销售',state:'UNSUBMITTED',revision:0,submittedAt:'',body,quotes:[],procurementCount:0,logisticsCount:0,canEdit:true,sourceMailId:'',legacy:false}}
function changeTemplate(id:string){if(!item.value)return;const template=activeTemplates.value.find(row=>row.id===id);if(!template)return;item.value.body.template=templateSnapshot(template);item.value.body.products.forEach(product=>applyTemplateDefaults(product,item.value!.body.template!.fields))}
function addProduct(){if(!item.value)return;const product=blankProduct();applyTemplateDefaults(product,item.value.body.template?.fields||[]);item.value.body.products.push(product)}
async function save(notify=true){if(!item.value)return;busy.value=true;try{const r=await command('save',{id:item.value.id,revision:item.value.revision,body:item.value.body});item.value=normalizeInquiry(r.item);if(notify)ElMessage.success('已保存')}finally{busy.value=false}}
async function submit(){await save();if(!item.value)return;busy.value=true;try{const r=await command('submit',{id:item.value.id,revision:item.value.revision});item.value=normalizeInquiry(r.item);ElMessage.success('已提交询价')}finally{busy.value=false}}
async function withdraw(){if(!item.value)return;await ElMessageBox.confirm('确定撤回吗？采购和物流已经填写的报价将被清除，重新提交后需要重新报价。','撤回询价',{type:'warning'});const r=await command('withdraw',{id:item.value.id,revision:item.value.revision});item.value=normalizeInquiry(r.item)}
async function handleMore(commandName:string){if(commandName==='back')await back();else if(commandName==='reload')await reload();else if(commandName==='save')await save();else if(commandName==='withdraw')await withdraw()}
async function loadCustomerOptions(){const data=await get<{customers:{id:string;code:string;name:string}[]}>('/sourcing-customer-options',{page_size:500});customerOptions.value=(data.customers||[]).map(option=>({...option,value:option.name}))}
async function customerSuggestions(query:string,done:(items:CustomerOption[])=>void){try{if(!customerOptions.value.length)await loadCustomerOptions();const needle=query.trim().toLowerCase();done(customerOptions.value.filter(option=>!needle||`${option.code} ${option.name}`.toLowerCase().includes(needle)).slice(0,50))}catch{done([])}}
async function loadContactOptions(customerId:string){if(!customerId){contactOptions.value=[];return}const data=await get<{contacts:Omit<ContactOption,'value'>[]}>(`/sourcing-customer-options/${customerId}/contacts`);contactOptions.value=(data.contacts||[]).map(option=>({...option,value:option.name}))}
async function contactSuggestions(query:string,done:(items:ContactOption[])=>void){try{if(item.value?.body.customerId&&!contactOptions.value.length)await loadContactOptions(item.value.body.customerId);const needle=query.trim().toLowerCase();done(contactOptions.value.filter(option=>!needle||`${option.name} ${option.department} ${option.title} ${option.email}`.toLowerCase().includes(needle)).slice(0,50))}catch{done([])}}
async function selectCustomer(option:CustomerOption){if(!item.value)return;item.value.body.customerId=option.id;item.value.body.customer=option.name;item.value.body.contactId='';contactOptions.value=[];await loadContactOptions(option.id);if(!item.value.body.contact){const primary=contactOptions.value.find(contact=>contact.isPrimary)||contactOptions.value[0];if(primary)selectContact(primary)}}
function customerTyped(){if(item.value)item.value.body.customerId='';contactOptions.value=[];if(item.value)item.value.body.contactId=''}
function selectContact(option:ContactOption){if(!item.value)return;item.value.body.contactId=option.id;item.value.body.contact=option.name}
function contactTyped(){if(item.value)item.value.body.contactId=''}
function contactOptionLabel(option:ContactOption){return [[option.department,option.title].filter(Boolean).join(' / '),option.email,option.isPrimary?'主要联系人':''].filter(Boolean).join(' · ')}
async function openCreateDialog(){uploadForm.templateId='';uploadForm.title='';uploadForm.customerId='';uploadForm.contactId='';uploadForm.file=null;contactOptions.value=[];if(!templates.value.length)await loadTemplates();if(!customerOptions.value.length)await loadCustomerOptions();uploadOpen.value=true}
async function uploadCustomerChanged(customerId:string){uploadForm.contactId='';await loadContactOptions(customerId);const primary=contactOptions.value.find(contact=>contact.isPrimary)||contactOptions.value[0];if(primary)uploadForm.contactId=primary.id}
function pickImportFile(event:Event){const input=event.target as HTMLInputElement,file=input.files?.[0]||null;if(file&&!/\.(xlsx|csv)$/i.test(file.name)){ElMessage.error('请选择 .xlsx 或 .csv 标准询盘文件');input.value='';uploadForm.file=null;return}if(file&&file.size>8*1024*1024){ElMessage.error('文件不能超过 8MB');input.value='';uploadForm.file=null;return}uploadForm.file=file}
function downloadSelectedTemplate(){if(uploadForm.templateId)window.open(`/api/inquiry-templates/${uploadForm.templateId}/download`,'_blank','noopener')}
function startManualInquiry(){uploadOpen.value=false;create()}
function fileTitle(name:string){return name.replace(/\.(xlsx|csv)$/i,'').trim()||'客户询盘'}
async function importInquiry(){if(!uploadForm.customerId){ElMessage.error('请选择客户');return}if(!uploadForm.contactId){ElMessage.error('请选择客户联系人');return}if(!uploadForm.file){ElMessage.error('请选择标准询盘文件');return}busy.value=true;try{const workbook=await parseTableFile(uploadForm.file.name,await uploadForm.file.arrayBuffer(),10001);const sheet=workbook.sheets[0];if(!sheet)throw new Error('文件中没有可读取的工作表');const selected=selectImportTemplate(sheet.columns,activeTemplates.value,uploadForm.templateId);const customer=customerOptions.value.find(option=>option.id===uploadForm.customerId);const contact=contactOptions.value.find(option=>option.id===uploadForm.contactId);if(!customer||!contact)throw new Error('客户或联系人已变化，请重新选择');const body=blankBody();body.title=uploadForm.title.trim()||fileTitle(uploadForm.file.name);body.customerId=customer.id;body.customer=customer.name;body.contactId=contact.id;body.contact=contact.name;body.template=templateSnapshot(selected);body.products=productsFromImportedSheet(sheet,selected);body.attachments=[];item.value={id:'',number:'',ownerId:'',owner:'当前销售',state:'UNSUBMITTED',revision:0,submittedAt:'',body,quotes:[],procurementCount:0,logisticsCount:0,canEdit:true,sourceMailId:'',legacy:false};await save(false);await storeAttachment(uploadForm.file,false);await save(false);uploadOpen.value=false;detailTab.value='products';ElMessage.success(`已导入 ${body.products.length} 条产品，请在提交询价前复核`)}catch(error){ElMessage.error(error instanceof Error?error.message:'标准询盘导入失败')}finally{busy.value=false}}
function editQuote(q?:Quote){detailTab.value='quotes';editor.value=q?structuredClone(JSON.parse(JSON.stringify(q))):{id:'',kind:view.value,version:0,body:blankQuote(),authorId:'',author:'',submittedAt:'',updatedBy:'',updatedAt:'',canEdit:true};productPage.value=1;productSearch.value='';productFilter.value='all';requirementsOpen.value=[];if(!q&&item.value&&editor.value){editor.value.body.loadingPort=item.value.body.loadingPort;editor.value.body.destinationPort=item.value.body.destinationPort}}
async function companySuggestions(query:string,done:(items:{value:string}[])=>void){try{const factory=view.value==='PROCUREMENT';const r=await get<{factories?:{nameZh?:string;name?:string}[];suppliers?:{name:string}[]}>(factory?'/factories':'/suppliers',{keyword:query,page:1,page_size:50});done((factory?r.factories||[]:r.suppliers||[]).map(x=>({value:('nameZh' in x?x.nameZh:x.name)||x.name||''})).filter(x=>x.value!==''))}catch{done([])}}
function addCharge(){editor.value?.body.charges.push({name:'',amount:'',currency:'USD',unit:'',quantity:'1',subtotal:'',remark:''})}
async function saveQuote(submit:boolean){if(!item.value||!editor.value)return;busy.value=true;try{const r=await command('quote',{id:item.value.id,revision:item.value.revision,quoteId:editor.value.id,quoteVersion:editor.value.version,submit,quote:editor.value.body});item.value=normalizeInquiry(r.item);editor.value=null;ElMessage.success(submit?'报价已提交':'报价已保存')}finally{busy.value=false}}
async function download(key:string){if(!item.value)return;const r=await post<{url:string}>('/inquiry-workspace',{action:'download',view:view.value,id:item.value.id,fileKey:key});window.open(r.url,'_blank','noopener')}
async function storeAttachment(file:File,quote:boolean){if(!item.value)return;if(file.size>8*1024*1024)throw new Error('文件不能超过 8MB');if(!item.value.id)await save(false);if(!item.value)return;const data=await new Promise<string>((resolve,reject)=>{const reader=new FileReader();reader.onload=()=>resolve(String(reader.result).split(',')[1]);reader.onerror=reject;reader.readAsDataURL(file)});const r=await command('upload',{id:item.value.id,revision:item.value.revision,fileName:file.name,fileData:data});if(!r.attachment)throw new Error('附件上传没有返回文件信息');if(quote&&editor.value){editor.value.body.attachments??=[];editor.value.body.attachments.push(r.attachment)}else{item.value.body.attachments??=[];item.value.body.attachments.push(r.attachment)}}
async function upload(e:Event,quote:boolean){const input=e.target as HTMLInputElement,file=input.files?.[0];if(!file||!item.value)return;try{await storeAttachment(file,quote);ElMessage.success('附件已上传')}catch(error){ElMessage.error(error instanceof Error?error.message:'附件上传失败')}finally{input.value=''}}
async function initial(){item.value=null;editor.value=null;detailTab.value=initialDetailTab();state.value='';page.value=1;const id=String(route.params.id||route.query.id||'');if(id){const r=await command('get',{id});item.value=normalizeInquiry(r.item)}else await loadList()}
const stopLive=onLive(e=>{if(e.type==='requirement.changed'&&!editor.value&&!editable.value&&!busy.value)void reload().catch(()=>{})})
let timer:ReturnType<typeof setInterval>|undefined
onMounted(()=>{void (async()=>{await loadTemplates();await initial()})();timer=setInterval(()=>{if(!editor.value&&!editable.value&&!busy.value)void reload().catch(()=>{})},5000)})
onUnmounted(()=>{stopLive();if(timer)clearInterval(timer)})
watch(view,()=>void initial())
</script>
<style scoped>
.inquiry-workspace{padding:20px;min-width:0}.toolbar{display:flex;align-items:center;gap:12px;flex-wrap:wrap;margin-bottom:18px}.toolbar h2,.toolbar h3{margin:0 auto 0 0}.toolbar .el-input{width:280px}.toolbar .el-select{width:150px}.inquiry-fields{display:grid;grid-template-columns:repeat(3,minmax(180px,1fr));gap:0 20px}.wide{grid-column:1/-1}.quote-card{margin:12px 0}.quote-editor{margin-top:20px}.el-pagination{margin:20px 0}.el-table{margin-bottom:14px}.el-date-editor.el-input{max-width:100%}.el-checkbox-group{display:flex;flex-wrap:wrap;gap:8px}.quote-card p{color:var(--el-text-color-secondary)}@media(max-width:900px){.inquiry-fields{grid-template-columns:repeat(2,minmax(150px,1fr))}}

.inquiry-workspace { --ink:#17324d; --muted:#6b7c8f; --line:#dce6ef; --brand:#176b87; --surface:#fff; padding: 12px; color: var(--ink); }
.workspace-heading { display: flex; align-items: center; justify-content: space-between; gap: 24px; margin: 2px 0 24px; padding: 24px 26px; background: linear-gradient(120deg,#f3f9fb 0%,#eef6fb 52%,#f7fafc 100%); border: 1px solid #d8e8ef; border-radius: 14px; }
.workspace-heading h2 { margin: 0; font-size: 28px; letter-spacing: -.5px; color: #12334d; }
.workspace-heading p { margin: 9px 0 0; color: var(--muted); font-size: 14px; line-height: 1.6; }
.module-chip { margin-bottom: 10px; color: var(--brand); border-color: #9fc9d6; background: #f6fcfd; }
.heading-actions { display: flex; gap: 10px; flex-wrap: wrap; justify-content: flex-end; }
.list-toolbar { padding: 16px 18px; background: var(--surface); border: 1px solid var(--line); border-radius: 12px; box-shadow: 0 5px 18px rgba(31,65,91,.04); }
.list-toolbar > .el-input { width: 320px; margin-right: auto; }
.inquiry-workspace :deep(.el-table) { border: 1px solid var(--line); border-radius: 12px; --el-table-header-bg-color: #f4f8fa; --el-table-header-text-color: #486174; --el-table-row-hover-bg-color: #f0f8fa; }
.inquiry-workspace :deep(.el-table th.el-table__cell) { height: 48px; font-weight: 600; }
.inquiry-workspace :deep(.el-table td.el-table__cell) { padding: 13px 0; color: #334155; }
.inquiry-workspace :deep(.el-button) { border-radius: 8px; }
.inquiry-workspace :deep(.el-button--primary) { --el-button-bg-color:var(--brand); --el-button-border-color:var(--brand); --el-button-hover-bg-color:#2086a5; --el-button-hover-border-color:#2086a5; }
.inquiry-workspace :deep(.el-tag) { border-radius: 6px; }
.detail-hero { display:flex; align-items:center; justify-content:space-between; gap:24px; margin-bottom:16px; padding:20px 22px; color:#fff; background:linear-gradient(120deg,#143f5a,#176b87); border-radius:14px; box-shadow:0 10px 24px rgba(16,77,103,.14); }
.detail-kicker { display:block; margin-bottom:5px; color:#c8e7ef; font-size:12px; }
.detail-hero h3 { margin:0; font-size:21px; }
.detail-title { margin-top:5px; color:#eefbff; font-size:14px; }
.detail-meta { display:flex; align-items:center; gap:12px; margin-top:10px; color:#dceef3; font-size:13px; }
.detail-actions { display:flex; gap:10px; flex-wrap:wrap; justify-content:flex-end; }
.detail-actions :deep(.el-button:not(.el-button--primary)) { color:#17455f; border-color:#fff; background:#fff; }
.more-actions { min-width:112px; font-weight:700; box-shadow:0 4px 14px rgba(4,36,54,.18); }
.more-arrow { margin-left:8px; color:#176b87; font-size:17px; }
.business-card { margin-bottom:16px; padding:20px 22px; background:var(--surface); border:1px solid var(--line); border-radius:14px; box-shadow:0 5px 18px rgba(31,65,91,.04); }
.section-title { display:flex; align-items:center; justify-content:space-between; gap:16px; margin-bottom:17px; }
.section-title h3 { margin:0; font-size:16px; color:var(--ink); }
.section-title p { margin:5px 0 0; color:var(--muted); font-size:13px; }
.template-actions { display:flex; align-items:center; gap:10px; flex-wrap:wrap; }
.template-actions .el-select { width:250px; }
.template-label { color:#4b6073; font-size:13px; font-weight:600; }
.inquiry-fields { padding: 0; border: 0; border-radius: 0; background: transparent; margin-bottom: 0; }
.inquiry-fields :deep(.el-form-item) { margin-bottom:17px; }
.inquiry-fields :deep(.el-form-item__label) { padding-bottom:7px; color:#4b6073; font-weight:600; }
.inquiry-fields :deep(.el-input__wrapper),.inquiry-fields :deep(.el-textarea__inner) { box-shadow:0 0 0 1px #cedbe5 inset; }
.inquiry-fields :deep(.el-autocomplete) { width:100%; }
.overview-grid { display:grid; grid-template-columns:minmax(0,2.3fr) minmax(260px,.7fr); gap:16px; align-items:start; }
.customer-card { min-width:0; }
.attachment-card { min-width:0; padding:18px; }
.attachment-heading { display:flex; align-items:flex-start; justify-content:space-between; gap:12px; margin-bottom:12px; }
.attachment-heading h3 { margin:0; font-size:16px; }
.attachment-heading p { margin:5px 0 0; color:var(--muted); font-size:12px; }
.attachment-empty { padding:13px 14px; color:#91a0ad; background:#f7fafb; border:1px dashed #d3e0e7; border-radius:8px; font-size:12px; text-align:center; }
.attachment-list { display:flex; gap:8px; flex-wrap:wrap; }
.attachment-list :deep(.el-button) { max-width:100%; overflow:hidden; text-overflow:ellipsis; }
.file-input { max-width:270px; color:var(--muted); font-size:13px; }
.file-input::file-selector-button { margin-right:10px; padding:8px 12px; color:var(--brand); background:#f1f9fb; border:1px solid #b8d7df; border-radius:7px; cursor:pointer; }
.product-card { overflow:hidden; }
.quotes-section { padding:22px; }
.detail-tabs { min-width:0; }
.detail-tabs :deep(.el-tabs__header) { margin:0 0 16px; padding:0 18px; background:#fff; border:1px solid var(--line); border-radius:12px; box-shadow:0 5px 18px rgba(31,65,91,.04); }
.detail-tabs :deep(.el-tabs__nav-wrap::after) { display:none; }
.detail-tabs :deep(.el-tabs__item) { height:50px; color:#5a6e7f; font-weight:600; }
.detail-tabs :deep(.el-tabs__item.is-active) { color:var(--brand); }
.compact-quote { display:block; overflow:hidden; color:#173f59; text-overflow:ellipsis; white-space:nowrap; }
.compact-quote+small,.quotes-section td small { display:block; margin-top:3px; overflow:hidden; color:#81909c; font-size:12px; text-overflow:ellipsis; white-space:nowrap; }
.upload-form { margin-top:20px; }
.upload-form :deep(.el-form-item__content) { min-width:0; }
.upload-form small { display:block; width:100%; margin-top:6px; color:#7a8995; line-height:1.5; }
.upload-template { display:grid; grid-template-columns:minmax(0,1fr) auto; width:100%; gap:10px; }
.upload-template .el-select { flex:1; min-width:0; }
.upload-form input[type=file] { width:100%; padding:8px; color:#657786; background:#f7fafb; border:1px solid #d7e2e9; border-radius:8px; }
.upload-form input[type=file]::file-selector-button { margin-right:12px; padding:8px 13px; color:var(--brand); background:#fff; border:1px solid #b9d3dd; border-radius:7px; cursor:pointer; }
.option-title { color:#23455c; font-weight:600; }.option-meta { color:#8493a0; font-size:12px; }
.quote-editor { padding:22px; background:#fff; border:1px solid var(--line); border-radius:14px; }
@media (max-width:1100px) { .overview-grid { grid-template-columns:1fr; } }
@media (max-width: 760px) {
  .inquiry-workspace { padding: 0; }
  .workspace-heading { align-items: flex-start; flex-direction: column; gap: 16px; padding:20px; }
  .workspace-heading h2 { font-size: 23px; }
  .heading-actions { width:100%; justify-content:flex-start; }
  .list-toolbar { padding: 12px; gap: 10px; }
  .list-toolbar > .el-input { width: 100%; }
  .detail-hero { align-items:flex-start; flex-direction:column; }
  .detail-actions { justify-content:flex-start; }
  .business-card { padding:16px; }
  .attachment-heading { align-items:flex-start; flex-direction:column; }
  .section-title { align-items:flex-start; flex-direction:column; }
  .inquiry-fields { grid-template-columns: 1fr; }
  .detail-tabs :deep(.el-tabs__header) { padding:0 8px; overflow-x:auto; }
  .upload-template { align-items:stretch; flex-direction:column; }
  .el-pagination { overflow-x: auto; padding-bottom: 8px; }
}

</style>
