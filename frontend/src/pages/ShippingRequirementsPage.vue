<template>
  <div class="shipping-requirements-page">
    <WorkflowPageHeader :title="mode === 'orders' ? t('shipping.ordersTitle') : t('shipping.executionInquiryTitle')" :description="mode === 'orders' ? t('shipping.ordersSubtitle') : t('shipping.d4Subtitle')"><template v-if="mode === 'orders' && auth.can('shipping:schedule:write')" #actions><el-button type="primary" @click="manualOrderId='';manualOpen=true">建立物流实单</el-button></template></WorkflowPageHeader>
    <ManualShippingOrderDialog v-model="manualOpen" :order-id="manualOrderId" :forwarders="forwarders" :carriers="carriers" @saved="manualSaved" />
    <el-dialog v-model="linkOpen" title="关联合同" width="520px" :close-on-click-modal="false">
      <p>选择销售合同，或按当前合同号精确匹配。关联不会重新流转或创建单据。</p>
      <ShippingContractNumberInput v-model="linkNumber" @selected="linkContractId=$event" style="width:100%" />
      <template #footer><el-button @click="linkOpen=false">取消</el-button><el-button type="primary" :loading="saving" @click="linkContract">确认关联</el-button></template>
    </el-dialog>
    <section class="summary-strip">
      <div><strong>{{ activeCount }}</strong><span>{{ t('shipping.d4Active') }}</span></div>
      <div><strong>{{ waitingCount }}</strong><span>{{ t('shipping.d4WaitingApproval') }}</span></div>
      <div><strong>{{ paymentCount }}</strong><span>{{ t('shipping.d4PaymentRequested') }}</span></div>
    </section>
    <el-card shadow="never" class="workbench">
      <el-radio-group v-if="mode === 'orders'" v-model="status" class="order-tabs" @change="load"><el-radio-button value="DRAFT">{{ t('shipping.orderTabs.draft') }}</el-radio-button><el-radio-button value="PENDING_APPROVAL">{{ t('shipping.orderTabs.approval') }}</el-radio-button><el-radio-button value="CONTRACT_AND_DELEGATED">{{ t('shipping.orderTabs.contractAndDelegated') }}</el-radio-button></el-radio-group>
      <div class="filters">
        <el-input v-model="keyword" :placeholder="t('shipping.executionInquirySearch')" clearable @keyup.enter="page = 1" />
        <el-select v-model="status" :placeholder="t('common.status')" clearable @change="load"><el-option v-for="item in statuses" :key="item" :label="statusLabel(item)" :value="item" /></el-select>
        <el-button @click="load">{{ t('common.refresh') }}</el-button>
        <el-button v-if="handoffColumns.customized.value" link @click="handoffColumns.reset">{{ t('common.restoreColumnOrder') }}</el-button>
      </div>
      <el-table :data="pagedRows" v-loading="loading" @row-click="openDetail">
        <el-table-column v-for="column in handoffColumns.columns.value" :key="column.key" :min-width="column.minWidth" :width="column.width" :align="column.align" :show-overflow-tooltip="column.showOverflowTooltip">
          <template #header><ReorderableTableHeader :label="column.label" :hint="t('common.dragColumnHint')" :move-left-label="t('common.moveColumnLeft')" :move-right-label="t('common.moveColumnRight')" :can-move-left="handoffColumns.canMoveLeft(column.key)" :can-move-right="handoffColumns.canMoveRight(column.key)" @move-left="handoffColumns.moveBy(column.key,-1)" @move-right="handoffColumns.moveBy(column.key,1)" /></template>
          <template #default="{ row }">
            <div v-if="column.key==='contractNo'"><div class="contract-no">{{ row.contractNo }}</div><div v-if="row.manualOrderNo" class="sub">物流实单：{{ row.manualOrderNo }}</div><div v-if="row.manualOrderNo" class="sub">补录 · {{ Number(row.linkedContractId)>0 ? '合同已关联' : '合同待关联' }}</div></div>
            <span v-else-if="column.key==='customer'">{{ row.customerName || '—' }}</span>
            <span v-else-if="column.key==='route'">{{ row.portOfLoading || '—' }} → {{ row.portOfDischarge || '—' }}</span>
            <template v-else-if="column.key==='parties'"><div>{{ row.finalForwarderName || t('shipping.d4NotConfirmed') }}</div><div class="sub">{{ row.actualCarrierName || row.carrierForwarder || '—' }}</div></template>
            <span v-else-if="column.key==='amount'" class="money">{{ row.amountMissing ? '—' : `${row.finalCurrency || row.currency} ${row.finalFreightAmount || row.freightAmount}` }}</span>
            <el-tag v-else-if="column.key==='status'" :type="statusType(row.status)" effect="plain">{{ statusLabel(row.status) }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column :label="t('common.actions')" width="125" fixed="right" align="center"><template #default="{ row }"><el-button type="primary" plain @click.stop="handleRowAction(row)">{{ nextLabel(row) }}</el-button></template></el-table-column>
        <template #empty>{{ t('shipping.executionInquiryEmpty') }}</template>
      </el-table>
      <el-pagination class="pager" layout="total, sizes, prev, pager, next" :total="filteredRows.length" :page-size="pageSize" :current-page="page" :page-sizes="[20, 50, 100]" @current-change="page = $event" @size-change="pageSize = $event; page = 1" />
    </el-card>

    <el-dialog v-model="detailOpen" fullscreen :title="`${mode === 'inquiry' ? t('shipping.executionInquiryTitle') : t('shipping.ordersTitle')} · ${detail?.contractNo || ''}`" destroy-on-close class="shipping-fullscreen-detail">
      <div class="detail-content">
      <template v-if="detail">
        <div v-if="detail.manualOrderNo" class="submit-selection"><span>{{ detail.manualOrderNo }} · 合同号：{{ detail.contractNo }}（{{ Number(detail.linkedContractId)>0 ? '已关联' : '待关联' }}）</span><el-button v-if="Number(detail.linkedContractId)>0 && auth.can('export:contract:read')" @click="router.push({path:'/contracts',query:{id:detail.linkedContractId}})">查看销售合同</el-button><el-button v-if="auth.can('shipping:schedule:write') && !Number(detail.linkedContractId)" @click="linkNumber=detail.contractNo;linkContractId='0';linkOpen=true">关联合同</el-button><el-button v-if="auth.can('shipping:schedule:write') && ['DRAFT','RETURNED'].includes(detail.status)" @click="manualOrderId=detail.id;detailOpen=false;manualOpen=true">补齐资料</el-button></div>
        <div class="detail-hero"><div><span>{{ detail.customerName }}</span><strong>{{ detail.portOfLoading || '—' }} → {{ detail.portOfDischarge || '—' }}</strong></div><el-tag :type="statusType(detail.status)" effect="plain">{{ statusLabel(detail.status) }}</el-tag></div>
        <div v-if="detail.status === 'PENDING_APPROVAL' && approvalTaskId" class="submit-selection">
          <span>{{ t('shipping.d4ApprovalHint') }}</span>
          <el-dropdown trigger="click" @command="actOnApproval">
            <el-button type="primary" :loading="saving">{{ t('orders.moreActions') }} ▾</el-button>
            <template #dropdown><el-dropdown-menu>
              <el-dropdown-item command="APPROVE">{{ t('todos.approve') }}</el-dropdown-item>
              <el-dropdown-item command="REJECT">{{ t('todos.reject') }}</el-dropdown-item>
            </el-dropdown-menu></template>
          </el-dropdown>
        </div>
        <section v-if="!detail.manualOrderNo" class="reference-card">
          <div class="section-title">{{ t('shipping.d4PresalesReference') }}<span>{{ t('shipping.d4ReferenceOnly') }}</span></div>
          <el-descriptions :column="2" size="small">
            <el-descriptions-item :label="t('shipping.d4Forwarder')">{{ detail.carrierForwarder || '—' }}</el-descriptions-item><el-descriptions-item :label="t('shipping.d4Plan')">{{ detail.serviceOptionName || '—' }}</el-descriptions-item>
            <el-descriptions-item :label="t('shipping.d4Amount')">{{ detail.currency }} {{ detail.freightAmount }}</el-descriptions-item><el-descriptions-item :label="t('shipping.d4Dates')">{{ detail.estimatedDeparture || '—' }} / {{ detail.estimatedArrival || '—' }}</el-descriptions-item>
          </el-descriptions>
        </section>
        <section class="cargo-card">
          <div class="section-title">{{ t('shipping.cargoDetails') }}<span>{{ t('shipping.cargoDetailsHint', { count: detail.cargoItems?.length || 0 }) }}</span></div>
          <el-table :data="detail.cargoItems || []" size="small">
            <el-table-column :label="t('shipping.cargoProduct')" min-width="155"><template #default="{ row }"><strong>{{ row.productName }}</strong><div class="sub">{{ row.productCode || '—' }}</div></template></el-table-column>
            <el-table-column prop="specification" :label="t('shipping.cargoSpecification')" min-width="190"><template #default="{ row }">{{ row.specification || '—' }}</template></el-table-column>
            <el-table-column :label="t('shipping.cargoQuantity')" width="120" align="right"><template #default="{ row }"><span class="money">{{ row.quantity }} {{ row.uomCode }}</span></template></el-table-column>
            <el-table-column prop="remark" :label="t('shipping.contractRemark')" min-width="120"><template #default="{ row }">{{ row.remark || '—' }}</template></el-table-column>
            <template #empty>{{ t('shipping.noCargoDetails') }}</template>
          </el-table>
        </section>
        <section v-if="mode === 'inquiry'" class="stage-card is-current">
          <div class="section-title"><b>1</b>{{ t('shipping.candidatePlans') }}<span>{{ t('shipping.candidateHint') }}</span></div>
          <div v-if="requoteOptions.length" class="candidate-list">
            <div v-for="option in requoteOptions" :key="option.id" class="candidate-option" :class="{ selected: selectedOptionId === Number(option.id) }" @click="canEdit && (selectedOptionId = Number(option.id))">
              <el-radio v-if="canEdit" v-model="selectedOptionId" :value="Number(option.id)" @click.stop />
              <div class="candidate-main"><strong>{{ option.forwarderName }}</strong><span>{{ option.serviceOption }}</span><small>{{ option.etd }} → {{ option.eta }} · {{ option.paymentTerms }}</small><small v-if="option.actualCarrierName">{{ t('shipping.d4Carrier') }}：{{ option.actualCarrierName }}</small><small v-if="option.remark">{{ t('shipping.contractRemark') }}：{{ option.remark }}</small></div>
              <div class="candidate-side"><strong>{{ option.currency }} {{ option.freightAmount }}</strong><span v-if="canEdit"><el-button link type="primary" @click.stop="editOption(option)">{{ t('common.edit') }}</el-button><el-button link type="danger" @click.stop="removeOption(option)">{{ t('common.delete') }}</el-button></span></div>
            </div>
          </div>
          <el-empty v-else :description="t('shipping.noCandidates')" :image-size="58" />
          <div v-if="canManageCandidates" class="candidate-editor">
            <div class="editor-title">{{ editingOptionId ? t('shipping.editCandidate') : t('shipping.addCandidate') }}</div>
            <el-form label-position="top"><div class="form-grid">
            <el-form-item :label="t('shipping.d4Forwarder')" required><el-select v-model="form.finalForwarderId" filterable @change="selectForwarder"><el-option v-for="item in forwarders" :key="item.id" :label="item.name" :value="Number(item.id)" /></el-select></el-form-item>
            <el-form-item :label="`${t('shipping.d4Carrier')}（${t('shipping.carrierLater')}）`"><el-select v-model="form.actualCarrierId" clearable filterable @change="selectCarrier"><el-option v-for="item in carriers" :key="item.id" :label="item.name" :value="Number(item.id)" /></el-select></el-form-item>
            <el-form-item :label="t('shipping.d4Plan')" required><el-input v-model="form.finalServiceOption" /></el-form-item>
            <el-form-item :label="t('shipping.d4Amount')" required><el-input v-model="form.finalFreightAmount"><template #prepend><el-select v-model="form.finalCurrency" style="width:88px"><el-option v-for="c in currencies" :key="c" :value="c" /></el-select></template></el-input></el-form-item>
            <el-form-item :label="t('shipping.d4Etd')" required><el-date-picker v-model="form.finalEtd" value-format="YYYY-MM-DD" style="width:100%" /></el-form-item><el-form-item :label="t('shipping.d4Eta')" required><el-date-picker v-model="form.finalEta" value-format="YYYY-MM-DD" style="width:100%" /></el-form-item>
            <el-form-item :label="t('shipping.d4PaymentTerms')" required><el-input v-model="form.paymentTerms" /></el-form-item><el-form-item :label="t('shipping.contractRemark')"><el-input v-model="form.remark" /></el-form-item>
            </div></el-form>
            <div class="stage-actions"><el-button v-if="editingOptionId" @click="clearCandidateForm">{{ t('common.cancel') }}</el-button><el-button type="primary" :loading="saving" @click="saveDraft">{{ editingOptionId ? t('shipping.updateCandidate') : t('shipping.saveCandidate') }}</el-button></div>
          </div>
          <el-alert v-if="detail.status === 'RETURNED'" type="warning" :closable="false" :title="detail.returnReason || t('shipping.d4Returned')" />
          <div v-if="canManageCandidates" class="submit-selection"><span>{{ t('shipping.selectCandidateHint') }}</span><el-button type="primary" :disabled="!selectedOptionId" :loading="saving" @click="selectDraft">{{ t('shipping.submitSelectedToDraft') }}</el-button></div>
        </section>
        <section v-if="mode === 'orders'" class="stage-card is-current">
          <div class="section-title"><b>1</b>{{ t('shipping.selectedPlan') }}<span>{{ t('shipping.selectedPlanHint') }}</span></div>
          <el-descriptions :column="2" border size="small">
            <el-descriptions-item :label="t('shipping.d4Forwarder')">{{ detail.finalForwarderName || '—' }}</el-descriptions-item>
            <el-descriptions-item :label="t('shipping.d4Carrier')">{{ detail.actualCarrierName || t('shipping.carrierLater') }}</el-descriptions-item>
            <el-descriptions-item :label="t('shipping.d4Plan')">{{ detail.finalServiceOption || '—' }}</el-descriptions-item>
            <el-descriptions-item :label="t('shipping.d4Amount')">{{ detail.finalCurrency }} {{ detail.finalFreightAmount }}</el-descriptions-item>
            <el-descriptions-item :label="t('shipping.d4Etd')">{{ detail.finalEtd || '—' }}</el-descriptions-item>
            <el-descriptions-item :label="t('shipping.d4Eta')">{{ detail.finalEta || '—' }}</el-descriptions-item>
            <el-descriptions-item :label="t('shipping.d4PaymentTerms')">{{ detail.paymentTerms || '—' }}</el-descriptions-item>
            <el-descriptions-item :label="t('shipping.contractRemark')">{{ detail.remark || '—' }}</el-descriptions-item>
          </el-descriptions>
          <div v-if="detail.status === 'DRAFT'" class="submit-selection"><span>{{ t('shipping.orderDraftHint') }}</span><el-button type="primary" :loading="saving" @click="submitRequote">{{ t('shipping.submitDraftApproval') }}</el-button></div>
          <p v-else-if="detail.status === 'PENDING_APPROVAL'" class="stage-note">{{ t('shipping.d4ApprovalHint') }}</p>
        </section>
        <section v-if="mode === 'orders'" class="stage-card" :class="{ 'is-current': ['APPROVED','CONTRACT_UPLOADED'].includes(detail.status) }">
          <div class="section-title"><b>2</b>{{ t('shipping.d4SignedContract') }}</div>
          <div v-if="detail.signedContractName" class="file-row"><a v-if="detail.signedContractUrl" :href="detail.signedContractUrl" target="_blank">{{ detail.signedContractName }}</a><span v-else>{{ detail.signedContractName }}</span><span>{{ detail.signedContractUploadedAt }}</span></div>
          <template v-if="['APPROVED','CONTRACT_UPLOADED'].includes(detail.status)"><el-form label-position="top"><el-form-item :label="t('shipping.d4ContractNo')" required><el-input v-model="form.forwarderContractNo" /></el-form-item></el-form><el-upload :auto-upload="false" :limit="1" :on-change="pickContract" :on-remove="() => contractFile = null"><el-button>{{ t('shipping.d4ChooseContract') }}</el-button></el-upload><div class="stage-actions"><el-button type="primary" :loading="saving" @click="uploadContract">{{ t('shipping.d4SaveContract') }}</el-button></div></template><p v-else-if="!detail.signedContractName" class="stage-note">{{ t('shipping.d4AfterApproval') }}</p>
          <el-alert v-if="detail.status === 'CONTRACT_UPLOADED'" type="warning" :closable="false" :title="t('shipping.awaitingFinance')" />
          <div v-if="detail.status === 'PAYMENT_REQUESTED'" class="submit-selection"><span>{{ t('shipping.delegatedHint') }}</span><el-button type="primary" @click="createSchedule">{{ Number(detail.scheduleId || 0) > 0 ? t('shipping.viewFormalSchedule') : t('shipping.createFromHandoff') }}</el-button></div>
        </section>
      </template>
      </div>
    </el-dialog>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRoute, useRouter } from 'vue-router'
import { ElMessage, ElMessageBox, type UploadFile } from 'element-plus'
import { del, get, post } from '../api'
import ShippingContractNumberInput from '../components/ShippingContractNumberInput.vue'
import ManualShippingOrderDialog from '../components/ManualShippingOrderDialog.vue'
import { useAuthStore } from '../stores/auth'
import WorkflowPageHeader from '../components/WorkflowPageHeader.vue'
import ReorderableTableHeader from '../components/ReorderableTableHeader.vue'
import { getActionableApproval } from '../lib/approvalAction'
import { useTableColumnOrder, type TableColumnDefinition } from '../composables/useTableColumnOrder'
const approvalTaskId = ref('')
const approvalOverride = ref(false)
const props=withDefaults(defineProps<{mode?:'inquiry'|'orders'}>(),{mode:'inquiry'}); const mode=computed(()=>props.mode)
interface CargoItem { id:string;contractItemId:string;lineNo:number;productCode:string;productName:string;specification:string;quantity:string;uomCode:string;remark:string }
interface Handoff { linkedContractId?:string; manualOrderNo?:string;amountMissing?:boolean; id:string;contractNo:string;customerName:string;batchNo:number;carrierForwarder:string;serviceOptionName:string;currency:string;freightAmount:string;portOfLoading:string;portOfDischarge:string;estimatedDeparture:string;estimatedArrival:string;remark:string;status:string;scheduleId:string;finalForwarderId:string;finalForwarderName:string;actualCarrierId:string;actualCarrierName:string;finalServiceOption:string;finalCurrency:string;finalFreightAmount:string;finalEtd:string;finalEta:string;paymentTerms:string;forwarderContractNo:string;returnReason:string;signedContractName:string;signedContractUrl:string;signedContractUploadedAt:string;contractVerifiedAt:string;contractVerifiedByName:string;paymentRequestedAt:string;cargoItems:CargoItem[] }
interface Party { id:string;name:string }
interface RequoteOption { id:string;handoffId:string;forwarderId:string;forwarderName:string;actualCarrierId:string;actualCarrierName:string;serviceOption:string;currency:string;freightAmount:string;etd:string;eta:string;paymentTerms:string;remark:string;createdByName:string;createdAt:string;updatedAt:string }
const linkOpen=ref(false),linkNumber=ref(''),linkContractId=ref('0')
async function linkContract(){if(!detail.value||saving.value)return;saving.value=true;try{await post(`/shipping/manual-orders/${detail.value.id}/link-contract`,{contract_id:linkContractId.value,contract_no:linkNumber.value});ElMessage.success('合同已关联，使用销售合同号');linkOpen.value=false;await refreshDetail()}finally{saving.value=false}}
const auth=useAuthStore(),manualOpen=ref(false),manualOrderId=ref('')
async function manualSaved(){detailOpen.value=false;status.value='DRAFT';await load()}
const { t }=useI18n(), route=useRoute(), router=useRouter(); const loading=ref(false),saving=ref(false),detailOpen=ref(false); const rows=ref<Handoff[]>([]),detail=ref<Handoff|null>(null),forwarders=ref<Party[]>([]),carriers=ref<Party[]>([]),requoteOptions=ref<RequoteOption[]>([]); const keyword=ref(''),status=ref(mode.value==='orders'?'DRAFT':''),page=ref(1),pageSize=ref(100),contractFile=ref<File|null>(null),selectedOptionId=ref(0),editingOptionId=ref(0)
const handoffColumnDefaults=computed<TableColumnDefinition[]>(()=>[
 {key:'contractNo',label:t('shipping.contractNo'),width:170},
 {key:'customer',label:t('shipping.customer'),minWidth:145,showOverflowTooltip:true},
 {key:'route',label:t('shipping.route'),minWidth:185,showOverflowTooltip:true},
 {key:'parties',label:t('shipping.d4FinalParties'),minWidth:200},
 {key:'amount',label:t('shipping.d4Amount'),width:140,align:'right'},
 {key:'status',label:t('common.status'),width:145,align:'center'},
])
const handoffColumns=useTableColumnOrder(computed(()=>mode.value==='orders'?'shipping-order-list':'shipping-inquiry-list'),handoffColumnDefaults)
const contractAndDelegatedStatuses=['APPROVED','CONTRACT_UPLOADED','CONTRACT_VERIFIED','PAYMENT_REQUESTED']
const statuses=computed(()=>mode.value==='orders'?['DRAFT','PENDING_APPROVAL','CONTRACT_AND_DELEGATED']:['WAITING_REQUOTE','RETURNED']), currencies=['USD','CNY','EUR','GBP','CAD','AUD','HKD']
const form=reactive({finalForwarderId:0,finalForwarderName:'',actualCarrierId:0,actualCarrierName:'',finalServiceOption:'',finalCurrency:'USD',finalFreightAmount:'',finalEtd:'',finalEta:'',paymentTerms:'',forwarderContractNo:'',remark:''})
const canEdit=computed(()=>['WAITING_REQUOTE','RETURNED','DRAFT'].includes(detail.value?.status||'')), canManageCandidates=computed(()=>mode.value==='inquiry'&&['WAITING_REQUOTE','RETURNED'].includes(detail.value?.status||'')), filteredRows=computed(()=>{const k=keyword.value.trim().toLowerCase();return k?rows.value.filter(r=>[r.manualOrderNo,r.contractNo,r.customerName,r.finalForwarderName,r.actualCarrierName,r.portOfLoading,r.portOfDischarge].some(v=>String(v||'').toLowerCase().includes(k))):rows.value}), pagedRows=computed(()=>filteredRows.value.slice((page.value-1)*pageSize.value,page.value*pageSize.value)), activeCount=computed(()=>rows.value.length),waitingCount=computed(()=>rows.value.filter(r=>r.status==='PENDING_APPROVAL').length),paymentCount=computed(()=>rows.value.filter(r=>r.status==='PAYMENT_REQUESTED').length)
function statusLabel(v:string){return v==='CONTRACT_AND_DELEGATED'?t('shipping.orderTabs.contractAndDelegated'):t(`shipping.handoffStatuses.${v}`)} function statusType(v:string){if(v==='RETURNED')return 'danger';if(v==='WAITING_REQUOTE'||v==='DRAFT'||v==='PENDING_APPROVAL'||v==='CONTRACT_UPLOADED')return 'warning';if(v==='PAYMENT_REQUESTED')return 'success';return 'info'} function nextLabel(r:Handoff){if(['WAITING_REQUOTE','RETURNED'].includes(r.status))return t('shipping.d4Start');if(r.status==='DRAFT')return t('shipping.editDraft');if(r.status==='APPROVED')return t('shipping.d4Upload');if(r.status==='PAYMENT_REQUESTED')return Number(r.scheduleId||0)>0?t('shipping.viewFormalSchedule'):t('shipping.createFromHandoff');return t('shipping.d4View')}
function handleRowAction(r:Handoff){if(r.status==='PAYMENT_REQUESTED'&&Number(r.scheduleId||0)>0){void router.push(`/shipping/${r.scheduleId}`);return}void openDetail(r)}
function clearCandidateForm(){editingOptionId.value=0;Object.assign(form,{finalForwarderId:0,finalForwarderName:'',actualCarrierId:0,actualCarrierName:'',finalServiceOption:'',finalCurrency:detail.value?.currency||'USD',finalFreightAmount:'',finalEtd:detail.value?.estimatedDeparture||'',finalEta:detail.value?.estimatedArrival||'',paymentTerms:'',remark:''})}
function fillForm(r:Handoff){Object.assign(form,{finalForwarderId:Number(r.finalForwarderId||0),finalForwarderName:r.finalForwarderName||'',actualCarrierId:Number(r.actualCarrierId||0),actualCarrierName:r.actualCarrierName||'',finalServiceOption:r.finalServiceOption||'',finalCurrency:r.finalCurrency||r.currency||'USD',finalFreightAmount:r.amountMissing?'':r.finalFreightAmount||'',finalEtd:r.finalEtd||r.estimatedDeparture||'',finalEta:r.finalEta||r.estimatedArrival||'',paymentTerms:r.paymentTerms||'',forwarderContractNo:r.forwarderContractNo||'',remark:r.remark||''})}
async function load(){loading.value=true;try{const d=await get<{handoffs:Handoff[]}>('/shipping/contract-handoffs');rows.value=(d.handoffs??[]).filter(r=>status.value==='CONTRACT_AND_DELEGATED'?contractAndDelegatedStatuses.includes(r.status):status.value?r.status===status.value:statuses.value.includes(r.status));page.value=1}finally{loading.value=false}} async function loadParties(){const [f,c]=await Promise.all([get<{suppliers:Party[]}>('/suppliers',{page_size:200,status:'ACTIVE',business_type:'FORWARDER'}),get<{suppliers:Party[]}>('/suppliers',{page_size:200,status:'ACTIVE',business_type:'CARRIER'})]);forwarders.value=f.suppliers??[];carriers.value=c.suppliers??[]}
async function loadOptions(){if(!detail.value){requoteOptions.value=[];return}const d=await get<{options:RequoteOption[]}>(`/shipping/contract-handoffs/${detail.value.id}/requote-options`);requoteOptions.value=d.options??[];if(selectedOptionId.value&&!requoteOptions.value.some(x=>Number(x.id)===selectedOptionId.value))selectedOptionId.value=0}
async function openDetail(r:Handoff){detailOpen.value=true;contractFile.value=null;selectedOptionId.value=0;const d=await get<{handoff:Handoff}>(`/shipping/contract-handoffs/${r.id}`);detail.value=d.handoff;await loadApprovalTask();if(canManageCandidates.value)clearCandidateForm();else fillForm(d.handoff);if(mode.value==='inquiry')await loadOptions();else requoteOptions.value=[]} function selectForwarder(id:number){form.finalForwarderName=forwarders.value.find(x=>Number(x.id)===Number(id))?.name||''} function selectCarrier(id:number){form.actualCarrierName=carriers.value.find(x=>Number(x.id)===Number(id))?.name||''} async function refreshDetail(){if(!detail.value)return;await openDetail(detail.value);await load()}
async function loadApprovalTask(){
  approvalTaskId.value=''
  approvalOverride.value=false
  if(detail.value?.status!=='PENDING_APPROVAL')return
  const result=await getActionableApproval('SHIPPING_REQUOTE',detail.value.id)
  approvalTaskId.value=result?.taskId||''
  approvalOverride.value=Boolean(result?.override)
}
async function actOnApproval(action:'APPROVE'|'REJECT'){
  if(!approvalTaskId.value||saving.value)return
  let comment=''
  try{
    if(!approvalOverride.value&&action==='APPROVE')await ElMessageBox.confirm(t('orders.approveConfirm',{no:detail.value?.contractNo}),t('todos.approve'),{confirmButtonText:t('todos.approve'),cancelButtonText:t('common.cancel')})
    else if(!approvalOverride.value){
      const result=await ElMessageBox.prompt(t('orders.rejectReasonHint'),t('todos.reject'),{inputType:'textarea',inputValidator:value=>Boolean(String(value||'').trim())||t('todos.commentRequired'),confirmButtonText:t('todos.reject'),cancelButtonText:t('common.cancel')})
      comment=result.value.trim()
    }
  }catch(error){if(error==='cancel'||error==='close')return;throw error}
  saving.value=true
  try{
    await post('/approvals/tasks/'+approvalTaskId.value+'/act',{action,comment})
    ElMessage.success(t('todos.acted'))
    approvalTaskId.value=''
    approvalOverride.value=false
    detailOpen.value=false
    await load()
  }finally{saving.value=false}
}
function requotePayload(){return {option_id:editingOptionId.value,final_forwarder_id:form.finalForwarderId,final_forwarder_name:form.finalForwarderName,actual_carrier_id:form.actualCarrierId,actual_carrier_name:form.actualCarrierName,final_service_option:form.finalServiceOption,final_currency:form.finalCurrency,final_freight_amount:form.finalFreightAmount,final_etd:form.finalEtd,final_eta:form.finalEta,payment_terms:form.paymentTerms,forwarder_contract_no:'',remark:form.remark}}
function editOption(option:RequoteOption){editingOptionId.value=Number(option.id);Object.assign(form,{finalForwarderId:Number(option.forwarderId),finalForwarderName:option.forwarderName,actualCarrierId:Number(option.actualCarrierId||0),actualCarrierName:option.actualCarrierName||'',finalServiceOption:option.serviceOption,finalCurrency:option.currency,finalFreightAmount:option.freightAmount,finalEtd:option.etd,finalEta:option.eta,paymentTerms:option.paymentTerms,remark:option.remark||''})}
async function saveDraft(){if(!form.finalForwarderId||!form.finalServiceOption.trim()||!form.finalFreightAmount||!form.finalEtd||!form.finalEta||!form.paymentTerms.trim()){ElMessage.warning(t('shipping.requoteRequired'));return}saving.value=true;try{await post(`/shipping/contract-handoffs/${detail.value?.id}/draft`,requotePayload());ElMessage.success(t(editingOptionId.value?'shipping.candidateUpdated':'shipping.candidateSaved'));await loadOptions();clearCandidateForm();await load()}finally{saving.value=false}}
async function removeOption(option:RequoteOption){await del(`/shipping/contract-handoffs/${detail.value?.id}/requote-options/${option.id}`);ElMessage.success(t('shipping.candidateDeleted'));await loadOptions();if(editingOptionId.value===Number(option.id))clearCandidateForm()}
async function selectDraft(){if(!selectedOptionId.value){ElMessage.warning(t('shipping.selectCandidateRequired'));return}saving.value=true;try{await post(`/shipping/contract-handoffs/${detail.value?.id}/select-draft`,{option_id:selectedOptionId.value});ElMessage.success(t('shipping.selectedToDraft'));detailOpen.value=false;await load()}finally{saving.value=false}}
async function submitRequote(){saving.value=true;try{await post(`/shipping/contract-handoffs/${detail.value?.id}/requote`,{});ElMessage.success(t('shipping.d4Submitted'));await refreshDetail()}finally{saving.value=false}} function pickContract(file:UploadFile){contractFile.value=file.raw??null}
async function uploadContract(){if(!contractFile.value||!form.forwarderContractNo.trim()){ElMessage.warning(t('shipping.contractRequired'));return}saving.value=true;try{const f=contractFile.value,s=await post<{fileKey:string;uploadUrl:string}>(`/shipping/contract-handoffs/${detail.value?.id}/contract/presign`,{file_name:f.name}),r=await fetch(s.uploadUrl,{method:'PUT',body:f});if(!r.ok)throw new Error(t('shipping.d4UploadFailed'));await post(`/shipping/contract-handoffs/${detail.value?.id}/contract`,{file_key:s.fileKey,file_name:f.name,contract_no:form.forwarderContractNo});ElMessage.success(t('shipping.d4ContractSaved'));await refreshDetail()}finally{saving.value=false}}
function createSchedule(){if(detail.value)void router.push({path:'/shipping/schedules',query:{handoff:detail.value.id}})}
async function openRoutedHandoff(){const id=String(route.query.handoff??'');if(!id)return;let row=rows.value.find(item=>item.id===id);if(!row&&mode.value==='orders'){const d=await get<{handoff:Handoff}>(`/shipping/contract-handoffs/${id}`);const mapped=contractAndDelegatedStatuses.includes(d.handoff.status)?'CONTRACT_AND_DELEGATED':d.handoff.status;if(['DRAFT','PENDING_APPROVAL','CONTRACT_AND_DELEGATED'].includes(mapped)){status.value=mapped;await load();row=rows.value.find(item=>item.id===id)}}if(row)await openDetail(row)}
watch(()=>props.mode,async nextMode=>{detailOpen.value=false;detail.value=null;keyword.value='';status.value=nextMode==='orders'?'DRAFT':'';await load();await openRoutedHandoff()})
watch(()=>route.query.handoff,async()=>{await openRoutedHandoff()})
onMounted(async()=>{await Promise.all([load(),loadParties()]);await openRoutedHandoff()})
</script>

<style scoped>
.detail-content{width:100%;box-sizing:border-box;padding:8px 16px 24px}.detail-content .form-grid{grid-template-columns:repeat(2,minmax(0,1fr));gap:0 24px}.detail-content .candidate-editor{padding:20px 24px}.detail-content .section-title{flex-wrap:wrap}.detail-content .candidate-main{overflow-wrap:anywhere}.detail-content .candidate-side{gap:12px;margin-left:24px}.detail-content :deep(.el-table){--el-table-header-bg-color:#eef9fe;--el-table-header-text-color:#24323a;--el-table-row-hover-bg-color:#f0fbf6}.detail-content .submit-selection{padding:16px 20px}@media(max-width:700px){.detail-content{padding:0 0 16px}.detail-content .form-grid{grid-template-columns:1fr}.detail-content .candidate-editor{padding:16px}.detail-content .candidate-side{margin-left:0}}
.order-tabs{margin-bottom:14px}
.shipping-requirements-page{color:#141817}.summary-strip{display:grid;grid-template-columns:repeat(3,minmax(0,1fr));gap:12px;margin-bottom:16px}.summary-strip>div{display:flex;align-items:baseline;gap:10px;padding:14px 18px;border:1px solid #d7e9ef;border-radius:12px;background:#fff}.summary-strip strong{font-size:24px;color:#0b84bd}.summary-strip span{color:#627482}.workbench{border-color:#d7e9ef}.filters{display:flex;gap:10px;margin-bottom:14px}.filters :deep(.el-input){width:320px}.filters :deep(.el-select){width:190px}.shipping-requirements-page :deep(.el-table){--el-table-header-bg-color:#eef9fe;--el-table-header-text-color:#24323a;--el-table-row-hover-bg-color:#f0fbf6}.shipping-requirements-page :deep(.el-table th.el-table__cell){border-bottom-color:#d9edf5;font-weight:650}.contract-no{color:#159fdc;font-weight:650}.sub,.stage-note{margin-top:3px;color:#697a87;font-size:12px}.money{font-variant-numeric:tabular-nums}.pager{justify-content:flex-end;margin-top:14px}.detail-hero{display:flex;align-items:center;justify-content:space-between;padding:14px 16px;margin-bottom:14px;border-radius:12px;background:linear-gradient(100deg,#edf9ff,#f0fcf6)}.detail-hero div{display:flex;flex-direction:column;gap:5px}.detail-hero strong{font-size:18px;color:#173a4d}.reference-card,.cargo-card,.stage-card{padding:16px;margin-bottom:14px;border:1px solid #dfe9ed;border-radius:12px;background:#fff}.reference-card{background:#f8fbfc}.cargo-card{padding-bottom:8px}.cargo-card :deep(.el-table){border:1px solid #e2edf1;border-radius:8px}.stage-card.is-current{border-left:4px solid #43bdf4;box-shadow:0 7px 20px rgba(42,107,133,.07)}.section-title{display:flex;align-items:center;gap:8px;margin-bottom:14px;font-size:16px;font-weight:650;color:#193d50}.section-title b{display:inline-grid;place-items:center;width:24px;height:24px;border-radius:50%;background:#e7f7ff;color:#0589c5}.section-title span{font-size:12px;font-weight:400;color:#788995}.candidate-list{display:grid;gap:9px}.candidate-option{display:flex;align-items:flex-start;gap:10px;padding:12px 14px;border:1px solid #dce8ed;border-radius:10px;background:#fff;cursor:pointer;transition:.15s ease}.candidate-option:hover,.candidate-option.selected{border-color:#45bdf3;background:#f0faff}.candidate-option :deep(.el-radio){margin-top:2px;margin-right:0}.candidate-main{display:flex;min-width:0;flex:1;flex-direction:column;gap:3px}.candidate-main strong{color:#173a4d}.candidate-main span{color:#405664}.candidate-main small{color:#73838e}.candidate-side{display:flex;align-items:flex-end;flex-direction:column;gap:8px;white-space:nowrap}.candidate-side>strong{color:#087f6f;font-variant-numeric:tabular-nums}.candidate-editor{padding:14px;margin-top:14px;border-radius:10px;background:#f7fbfd}.editor-title{margin-bottom:12px;color:#173a4d;font-weight:650}.submit-selection{display:flex;align-items:center;justify-content:space-between;gap:16px;padding:13px 14px;margin-top:14px;border:1px solid #cfeaf5;border-radius:10px;background:linear-gradient(90deg,#f2fbff,#f3fcf8);color:#5f7280}.form-grid{display:grid;grid-template-columns:1fr 1fr;gap:0 14px}.form-grid :deep(.el-select){width:100%}.stage-actions{display:flex;justify-content:flex-end;margin-top:10px}.file-row{display:flex;justify-content:space-between;gap:12px;padding:10px 12px;margin-bottom:10px;border-radius:8px;background:#f3f8fa}.file-row span{color:#788995;font-size:12px}.success-line{color:#1a9b63}.payment-grid{display:grid;grid-template-columns:repeat(3,1fr);gap:10px}.payment-grid span{display:flex;flex-direction:column;gap:5px;padding:10px;border-radius:8px;background:#f3f8fa;color:#6b7b87;font-size:12px}.payment-grid strong{font-size:16px;color:#173a4d}@media(max-width:700px){.summary-strip,.form-grid,.payment-grid{grid-template-columns:1fr}.filters{flex-direction:column}.filters :deep(.el-input),.filters :deep(.el-select){width:100%}.candidate-option,.submit-selection{align-items:stretch;flex-direction:column}.candidate-side{align-items:flex-start}.pager{justify-content:flex-start;overflow-x:auto}}
</style>
