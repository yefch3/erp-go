<template>
  <div class="shipping-requirements-page">
    <WorkflowPageHeader :title="mode === 'orders' ? t('shipping.ordersTitle') : t('shipping.executionInquiryTitle')" :description="mode === 'orders' ? t('shipping.ordersSubtitle') : t('shipping.d4Subtitle')"><template v-if="mode === 'orders' && auth.can('shipping:schedule:write')" #actions><el-button type="primary" @click="manualOrderId='';manualOpen=true">建立物流实单</el-button></template></WorkflowPageHeader>
    <el-alert v-if="entryError" :title="entryError" type="error" :closable="false" class="entry-error"/>
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
      <el-table :data="pagedRows" v-loading="loading" @row-click="openRowDetail">
        <el-table-column v-for="column in handoffColumns.columns.value" :key="column.key" :min-width="column.minWidth" :width="column.width" :align="column.align" :show-overflow-tooltip="column.showOverflowTooltip">
          <template #header><ReorderableTableHeader :label="column.label" :hint="t('common.dragColumnHint')" :move-left-label="t('common.moveColumnLeft')" :move-right-label="t('common.moveColumnRight')" :can-move-left="handoffColumns.canMoveLeft(column.key)" :can-move-right="handoffColumns.canMoveRight(column.key)" @move-left="handoffColumns.moveBy(column.key,-1)" @move-right="handoffColumns.moveBy(column.key,1)" /></template>
          <template #default="{ row }">
            <div v-if="column.key==='contractNo'"><div class="contract-no">{{ row.contractNo }}</div><div v-if="!row.manualOrderNo" class="sub">第 {{row.batchNo}} 批<span v-if="row.allocationStatus==='PENDING_ALLOCATION'"> · 待分配货物</span></div><div v-if="row.manualOrderNo" class="sub">物流实单：{{ row.manualOrderNo }}</div><div v-if="row.manualOrderNo" class="sub">补录 · {{ Number(row.linkedContractId)>0 ? '合同已关联' : '合同待关联' }}</div></div>
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

    <el-dialog v-model="detailOpen" fullscreen :title="`${mode === 'inquiry' ? t('shipping.executionInquiryTitle') : t('shipping.ordersTitle')} · ${detail?.contractNo || ''}${detail ? ' · '+t('shipping.todoBatchTitle',{n:detail.batchNo}) : ''}`" destroy-on-close class="shipping-fullscreen-detail" :class="{'shipping-inquiry-dialog':mode==='inquiry'}">
      <div class="detail-content" :class="{'is-inquiry':mode==='inquiry'}">
      <template v-if="detail">
        <div v-if="detail.manualOrderNo" class="submit-selection"><span>{{ detail.manualOrderNo }} · 合同号：{{ detail.contractNo }}（{{ Number(detail.linkedContractId)>0 ? '已关联' : '待关联' }}）</span><el-button v-if="Number(detail.linkedContractId)>0 && auth.can('export:contract:read')" @click="router.push({path:'/contracts',query:{id:detail.linkedContractId}})">查看销售合同</el-button><el-button v-if="auth.can('shipping:schedule:write') && !Number(detail.linkedContractId)" @click="linkNumber=detail.contractNo;linkContractId='0';linkOpen=true">关联合同</el-button><el-button v-if="auth.can('shipping:schedule:write') && ['DRAFT','RETURNED'].includes(detail.status)" @click="manualOrderId=detail.id;detailOpen=false;manualOpen=true">补齐资料</el-button></div>
        <div class="detail-hero"><div><span>{{ detail.customerName }} · {{t('shipping.todoBatchTitle',{n:detail.batchNo})}}</span><strong>{{ detail.portOfLoading || '—' }} → {{ detail.portOfDischarge || '—' }}</strong></div><div v-if="mode==='inquiry'" class="batch-total"><span>{{t('shipping.inquiryQuantity')}}</span><strong>{{batchQuantity||'—'}}</strong></div><el-tag :type="statusType(detail.status)" effect="plain">{{ statusLabel(detail.status) }}</el-tag></div>
        <p v-if="mode==='inquiry'" class="inquiry-next">{{t(detail.status==='RETURNED'?'shipping.inquiryReturnHint':'shipping.inquiryFirstHint')}}</p>
        <el-alert v-if="mode==='inquiry'&&detail.status==='RETURNED'" type="warning" :closable="false" class="return-reason" :title="detail.returnReason || t('shipping.d4Returned')"/>
        <section v-if="mode==='orders'&&detail.sourceSalesRemark?.trim()" class="source-sales-remark"><strong>{{ t('inquiryWorkspace.detail.salesRemark') }}</strong><p>{{ detail.sourceSalesRemark }}</p></section>
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
        <el-alert v-if="detail.allocationStatus==='PENDING_ALLOCATION'" title="本任务尚未明确分批数量，货物清单是整份合同参考，请先核对分批安排，不能把每项参考方案都作为全量运输。" type="warning" :closable="false"/>
        <template v-if="mode==='orders'">
        <PresaleReference :json="detail.presaleReferencesJson"/>
        <section v-if="!detail.manualOrderNo && !detail.presaleReferencesJson" class="reference-card">
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
        </template>
        <ShippingPurchaseDeliverySummary v-if="mode==='orders' && detail && auth.can('procurement:order:read')" :key="detail.id" :handoff-id="detail.id"/>
        <section v-if="mode === 'inquiry'" class="stage-card is-current">
          <div class="quote-table-heading"><div class="section-title">{{ t('shipping.quoteComparison') }}<span>{{ t('shipping.candidateHint') }}</span></div><el-button v-if="canManageCandidates" type="primary" @click="clearCandidateForm();quoteEditorOpen=true">{{t('shipping.newQuote')}}</el-button></div>
          <el-table :data="requoteOptions" class="quote-comparison" size="small" :row-class-name="quoteRowClass" :empty-text="t('shipping.inquiryNoCandidates')" @row-click="chooseQuote">
            <el-table-column :label="t('shipping.quoteSelect')" width="58" align="center"><template #default="{row}"><el-radio v-if="canManageCandidates" v-model="selectedOptionId" :value="Number(row.id)" :aria-label="t('shipping.selectQuote',{name:row.forwarderName,amount:row.currency+' '+row.freightAmount})" @click.stop><span class="sr-only">{{row.forwarderName}}</span></el-radio></template></el-table-column>
            <el-table-column prop="forwarderName" :label="t('shipping.d4Forwarder')" width="180" show-overflow-tooltip/>
            <el-table-column :label="t('shipping.d4Amount')" width="150" align="right" class-name="quote-money-column"><template #default="{row}"><strong class="quote-amount">{{row.currency}} {{row.freightAmount}}</strong></template></el-table-column>
            <el-table-column prop="etd" :label="t('shipping.quoteEtd')" width="110" align="center"/>
            <el-table-column prop="eta" :label="t('shipping.quoteEta')" width="110" align="center"/>
            <el-table-column prop="paymentTerms" :label="t('shipping.d4PaymentTerms')" min-width="170" show-overflow-tooltip/>
            <el-table-column :label="t('shipping.contractRemark')" min-width="200" show-overflow-tooltip><template #default="{row}">{{quoteNoteText(row.serviceOption,row.remark)||'—'}}</template></el-table-column>
            <el-table-column v-if="requoteOptions.some(option=>option.actualCarrierName)" prop="actualCarrierName" :label="t('shipping.d4Carrier')" min-width="150" show-overflow-tooltip/>
            <el-table-column v-if="canManageCandidates" :label="t('common.actions')" width="116" fixed="right"><template #default="{row}"><el-button link type="primary" @click.stop="editOption(row)">{{t('common.edit')}}</el-button><el-button link type="danger" @click.stop="removeOption(row)">{{t('common.delete')}}</el-button></template></el-table-column>
          </el-table>
          <el-dialog v-model="quoteEditorOpen" :title="t(editingOptionId?'shipping.editQuote':'shipping.newQuote')" width="900px" style="max-width:calc(100vw - 32px)" append-to-body :close-on-click-modal="false" destroy-on-close class="shipping-inquiry-dialog" @closed="clearCandidateForm">
            <div class="detail-content is-inquiry quote-editor-content">
            <el-form label-position="top"><div class="form-grid">
            <el-form-item :label="t('shipping.d4Forwarder')" required><el-select :model-value="form.finalForwarderId||undefined" @update:model-value="form.finalForwarderId=Number($event||0)" :placeholder="t('shipping.inquiryChooseForwarder')" filterable @change="selectForwarder"><el-option v-for="item in forwarders" :key="item.id" :label="item.name" :value="Number(item.id)" /></el-select></el-form-item>
            <el-form-item :label="`${t('shipping.d4Carrier')}（${t('shipping.carrierLater')}）`"><el-select :model-value="form.actualCarrierId||undefined" @update:model-value="form.actualCarrierId=Number($event||0)" :placeholder="t('shipping.inquiryCarrierOptional')" clearable filterable @change="selectCarrier"><el-option v-for="item in carriers" :key="item.id" :label="item.name" :value="Number(item.id)" /></el-select></el-form-item>
            <el-form-item :label="t('shipping.d4Amount')" required><el-input v-model="form.finalFreightAmount"><template #prepend><el-select v-model="form.finalCurrency" style="width:88px"><el-option v-for="c in currencies" :key="c" :value="c" /></el-select></template></el-input></el-form-item>
            <el-form-item :label="t('shipping.d4Etd')" required><el-date-picker v-model="form.finalEtd" value-format="YYYY-MM-DD" style="width:100%" /></el-form-item><el-form-item :label="t('shipping.d4Eta')" required><el-date-picker v-model="form.finalEta" value-format="YYYY-MM-DD" style="width:100%" /></el-form-item>
            <el-form-item :label="t('shipping.d4PaymentTerms')" required><el-autocomplete v-model="form.paymentTerms" :fetch-suggestions="suggestPaymentTerms" :placeholder="t('shipping.paymentTermsPlaceholder')" clearable :debounce="0" style="width:100%" /></el-form-item><el-form-item :label="t('shipping.optionalNote')" class="quote-note"><el-input v-model="quoteNote" type="textarea" :autosize="{minRows:2,maxRows:4}" /></el-form-item>
            </div></el-form>
            <div class="stage-actions"><el-button :disabled="saving" @click="quoteEditorOpen=false">{{ t('common.cancel') }}</el-button><el-button type="primary" :loading="saving" @click="saveDraft">{{t('shipping.saveQuote')}}</el-button></div>
            </div>
          </el-dialog>
          <div v-if="canManageCandidates" class="submit-selection quote-selection-footer"><span v-if="selectedQuote"><strong>{{t('shipping.chosenQuote')}}：{{selectedQuote.forwarderName}}｜{{selectedQuote.currency}} {{selectedQuote.freightAmount}}</strong><small class="selection-help">{{t('shipping.selectCandidateHint')}}</small></span><span v-else>{{t('shipping.selectCandidateHint')}}</span><el-button type="primary" :disabled="!selectedQuote" :loading="saving" @click="selectDraft">{{ t('shipping.submitSelectedToDraft') }}</el-button></div>
        </section>
        <ShippingInquiryContext v-if="mode==='inquiry'" :key="detail.id" :detail="detail"/>
        <section v-if="mode === 'orders'" class="stage-card is-current">
          <div class="section-title"><b>1</b>{{ t('shipping.selectedPlan') }}<span>{{ t('shipping.selectedPlanHint') }}</span></div>
          <el-descriptions :column="2" border size="small">
            <el-descriptions-item :label="t('shipping.d4Forwarder')">{{ detail.finalForwarderName || '—' }}</el-descriptions-item>
            <el-descriptions-item :label="t('shipping.d4Carrier')">{{ detail.actualCarrierName || t('shipping.carrierLater') }}</el-descriptions-item>
            <el-descriptions-item :label="t('shipping.d4Amount')">{{ detail.finalCurrency }} {{ detail.finalFreightAmount }}</el-descriptions-item>
            <el-descriptions-item :label="t('shipping.d4Etd')">{{ detail.finalEtd || '—' }}</el-descriptions-item>
            <el-descriptions-item :label="t('shipping.d4Eta')">{{ detail.finalEta || '—' }}</el-descriptions-item>
            <el-descriptions-item :label="t('shipping.d4PaymentTerms')">{{ detail.paymentTerms || '—' }}</el-descriptions-item>
            <el-descriptions-item :label="t('shipping.contractRemark')"><span class="quote-note-text">{{quoteNoteText(detail.finalServiceOption,detail.remark)||'—'}}</span></el-descriptions-item>
          </el-descriptions>
          <div v-if="detail.status === 'DRAFT' && auth.can('shipping:schedule:write')" class="submit-selection"><span>{{ t('shipping.orderDraftHint') }}</span><el-button type="primary" :loading="saving" @click="submitRequote">{{ t('shipping.submitDraftApproval') }}</el-button></div>
          <p v-else-if="detail.status === 'PENDING_APPROVAL'" class="stage-note">{{ t('shipping.d4ApprovalHint') }}</p>
        </section>
        <section v-if="mode === 'orders'" class="stage-card" :class="{ 'is-current': ['APPROVED','CONTRACT_UPLOADED'].includes(detail.status) }">
          <div class="section-title"><b>2</b>{{ t('shipping.d4SignedContract') }}</div><p class="stage-note">{{t('supplierRecon.exportContract')}}：{{detail.contractNo}}　{{t('shipping.transportBatch')}}：{{t('shipping.todoBatchTitle',{n:detail.batchNo})}}</p><p v-if="detail.forwarderContractNo" class="stage-note">{{t('shipping.logisticsContractNo')}}：{{detail.forwarderContractNo}} <el-tag v-if="detail.forwarderContractNoGenerated" size="small" type="info">{{t('shipping.systemGenerated')}}</el-tag></p>
          <div v-if="detail.signedContractName" class="file-row"><a v-if="detail.signedContractUrl" :href="detail.signedContractUrl" target="_blank">{{ detail.signedContractName }}</a><span v-else>{{ detail.signedContractName }}</span><span>{{ detail.signedContractUploadedAt }}</span></div>
          <template v-if="['APPROVED','CONTRACT_UPLOADED'].includes(detail.status) && auth.can('shipping:schedule:write')"><el-form label-position="top"><el-form-item :label="t('shipping.optionalLogisticsContractNo')" style="max-width:420px"><el-input v-model="form.forwarderContractNo" maxlength="80" :placeholder="t('shipping.contractNoPlaceholder')" /></el-form-item></el-form><el-upload :auto-upload="false" :limit="1" :on-change="pickContract" :on-remove="() => contractFile = null"><el-button>{{ t('shipping.d4ChooseContract') }}</el-button></el-upload><div class="stage-actions"><el-button type="primary" :loading="saving" @click="uploadContract">{{ t('shipping.d4SaveContract') }}</el-button></div></template><p v-else-if="!detail.signedContractName" class="stage-note">{{ t('shipping.d4AfterApproval') }}</p>
          <el-alert v-if="detail.status === 'CONTRACT_UPLOADED'" type="warning" :closable="false" :title="t('shipping.awaitingFinance')" /><div v-if="detail.status === 'CONTRACT_UPLOADED' && auth.can('procurement:recon:write')" class="stage-actions"><el-button type="primary" :loading="saving" @click="verifyContract">{{ t('supplierRecon.approveFinance') }}</el-button></div>
          <div v-if="['PAYMENT_REQUESTED','SCHEDULED'].includes(detail.status)" class="submit-selection"><span>{{ t('shipping.delegatedHint') }}</span><el-button v-if="Number(detail.scheduleId || 0) > 0 || auth.can('shipping:schedule:write')" type="primary" @click="createSchedule">{{ Number(detail.scheduleId || 0) > 0 ? t('shipping.viewFormalSchedule') : t('shipping.createFromHandoff') }}</el-button></div>
        </section>
      </template>
      </div>
    </el-dialog>
  </div>
</template>

<script setup lang="ts">
import {shippingHandoffStage} from '../lib/shippingTodoEntry'
import ShippingInquiryContext from '../components/ShippingInquiryContext.vue'
import {cargoQuantityTotals,inquiryStatusKey,quoteNoteText} from '../lib/shippingInquiryView'
import {fulfillmentNumber as number} from '../lib/fulfillmentOverview'
import ShippingPurchaseDeliverySummary from '../components/ShippingPurchaseDeliverySummary.vue'
import PresaleReference from '../components/PresaleReference.vue'
import { computed, onMounted, onUnmounted, reactive, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRoute, useRouter } from 'vue-router'
import { ElMessage, ElMessageBox, type UploadFile } from 'element-plus'
import { del, get, post, quietErrors } from '../api'
import ShippingContractNumberInput from '../components/ShippingContractNumberInput.vue'
import ManualShippingOrderDialog from '../components/ManualShippingOrderDialog.vue'
import { useAuthStore } from '../stores/auth'
import WorkflowPageHeader from '../components/WorkflowPageHeader.vue'
import ReorderableTableHeader from '../components/ReorderableTableHeader.vue'
import { getActionableApproval } from '../lib/approvalAction'
import { useTableColumnOrder, type TableColumnDefinition } from '../composables/useTableColumnOrder'
const approvalTaskId = ref('')
const approvalOverride = ref(false)
const entryError = ref('')
const props=withDefaults(defineProps<{mode?:'inquiry'|'orders'}>(),{mode:'inquiry'}); const mode=computed(()=>props.mode)
interface CargoItem { id:string;contractItemId:string;lineNo:number;productCode:string;productName:string;specification:string;quantity:string;uomCode:string;remark:string }
interface Handoff {presaleReferencesJson?:string;allocationStatus?:string; linkedContractId?:string; manualOrderNo?:string;amountMissing?:boolean; id:string;contractNo:string;customerName:string;batchNo:number;carrierForwarder:string;serviceOptionName:string;currency:string;freightAmount:string;portOfLoading:string;portOfDischarge:string;estimatedDeparture:string;estimatedArrival:string;remark:string;sourceSalesRemark:string;status:string;scheduleId:string;finalForwarderId:string;finalForwarderName:string;actualCarrierId:string;actualCarrierName:string;finalServiceOption:string;finalCurrency:string;finalFreightAmount:string;finalEtd:string;finalEta:string;paymentTerms:string;forwarderContractNo:string;forwarderContractNoGenerated?:boolean;returnReason:string;signedContractName:string;signedContractUrl:string;signedContractUploadedAt:string;contractVerifiedAt:string;contractVerifiedByName:string;paymentRequestedAt:string;cargoItems:CargoItem[] }
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
const contractAndDelegatedStatuses=['APPROVED','CONTRACT_UPLOADED','CONTRACT_VERIFIED','PAYMENT_REQUESTED','SCHEDULED']
const statuses=computed(()=>mode.value==='orders'?['DRAFT','PENDING_APPROVAL','CONTRACT_AND_DELEGATED']:['WAITING_REQUOTE','RETURNED']), currencies=['USD','CNY','EUR','GBP','CAD','AUD','HKD']
const form=reactive({finalForwarderId:0,finalForwarderName:'',actualCarrierId:0,actualCarrierName:'',finalServiceOption:'',finalCurrency:'USD',finalFreightAmount:'',finalEtd:'',finalEta:'',paymentTerms:'',forwarderContractNo:'',remark:''})
const canEdit=computed(()=>['WAITING_REQUOTE','RETURNED','DRAFT'].includes(detail.value?.status||'')), canManageCandidates=computed(()=>mode.value==='inquiry'&&['WAITING_REQUOTE','RETURNED'].includes(detail.value?.status||'')), filteredRows=computed(()=>{const k=keyword.value.trim().toLowerCase();return k?rows.value.filter(r=>[r.manualOrderNo,r.contractNo,r.customerName,r.finalForwarderName,r.actualCarrierName,r.portOfLoading,r.portOfDischarge].some(v=>String(v||'').toLowerCase().includes(k))):rows.value}), pagedRows=computed(()=>filteredRows.value.slice((page.value-1)*pageSize.value,page.value*pageSize.value)), activeCount=computed(()=>rows.value.length),waitingCount=computed(()=>rows.value.filter(r=>r.status==='PENDING_APPROVAL').length),paymentCount=computed(()=>rows.value.filter(r=>r.status==='PAYMENT_REQUESTED').length)
const quoteNote=computed({get:()=>quoteNoteText(form.finalServiceOption,form.remark),set:(value:string)=>{form.finalServiceOption='';form.remark=value}})
function quoteRowClass({row}:{row:RequoteOption}){return selectedOptionId.value===Number(row.id)?'selected-quote':''}
function chooseQuote(row:RequoteOption){if(canManageCandidates.value)selectedOptionId.value=Number(row.id)}
const quoteEditorOpen=ref(false)
const selectedQuote=computed(()=>requoteOptions.value.find(option=>Number(option.id)===selectedOptionId.value))
watch(detailOpen,open=>{if(!open)quoteEditorOpen.value=false})
function suggestPaymentTerms(query:string,callback:(items:{value:string}[])=>void){
 const values=[t('shipping.paymentTermAdvance'),t('shipping.paymentTermBeforeShipment'),t('shipping.paymentTermBlCopy'),t('shipping.paymentTermArrival'),t('shipping.paymentTermNet30')]
 callback(values.filter(value=>value.toLocaleLowerCase().includes(query.trim().toLocaleLowerCase())).map(value=>({value})))
}
const batchQuantity=computed(()=>cargoQuantityTotals(detail.value?.cargoItems||[]).map(row=>`${number(row.quantity)} ${row.unit}`).join(' / '))
function statusLabel(v:string){return v==='CONTRACT_AND_DELEGATED'?t('shipping.orderTabs.contractAndDelegated'):t(inquiryStatusKey(v))} function statusType(v:string){if(v==='RETURNED')return 'danger';if(v==='WAITING_REQUOTE'||v==='DRAFT'||v==='PENDING_APPROVAL'||v==='CONTRACT_UPLOADED')return 'warning';if(v==='PAYMENT_REQUESTED')return 'success';return 'info'} function nextLabel(r:Handoff){if(r.status==='RETURNED')return t('shipping.todoRetryInquiry');if(r.status==='WAITING_REQUOTE')return t('shipping.d4Start');if(r.status==='DRAFT')return t('shipping.editDraft');if(r.status==='APPROVED')return t('shipping.d4Upload');if(['PAYMENT_REQUESTED','SCHEDULED'].includes(r.status))return Number(r.scheduleId||0)>0?t('shipping.viewFormalSchedule'):t('shipping.createFromHandoff');return t('shipping.d4View')}
function handleRowAction(r:Handoff){if(['PAYMENT_REQUESTED','SCHEDULED'].includes(r.status)&&Number(r.scheduleId||0)>0){void router.push(`/shipping/${r.scheduleId}`);return}void openDetail(r)}
function openRowDetail(row:Handoff){void openDetail(row)}
function clearCandidateForm(){editingOptionId.value=0;Object.assign(form,{finalForwarderId:0,finalForwarderName:'',actualCarrierId:0,actualCarrierName:'',finalServiceOption:'',finalCurrency:detail.value?.currency||'USD',finalFreightAmount:'',finalEtd:detail.value?.estimatedDeparture||'',finalEta:detail.value?.estimatedArrival||'',paymentTerms:'',remark:''})}
function fillForm(r:Handoff){Object.assign(form,{finalForwarderId:Number(r.finalForwarderId||0),finalForwarderName:r.finalForwarderName||'',actualCarrierId:Number(r.actualCarrierId||0),actualCarrierName:r.actualCarrierName||'',finalServiceOption:r.finalServiceOption||'',finalCurrency:r.finalCurrency||r.currency||'USD',finalFreightAmount:r.amountMissing?'':r.finalFreightAmount||'',finalEtd:r.finalEtd||r.estimatedDeparture||'',finalEta:r.finalEta||r.estimatedArrival||'',paymentTerms:r.paymentTerms||'',forwarderContractNo:r.forwarderContractNo||'',remark:r.remark||''})}
async function load(){loading.value=true;try{const d=await get<{handoffs:Handoff[]}>('/shipping/contract-handoffs');rows.value=(d.handoffs??[]).filter(r=>status.value==='CONTRACT_AND_DELEGATED'?contractAndDelegatedStatuses.includes(r.status):status.value?r.status===status.value:statuses.value.includes(r.status));page.value=1}finally{loading.value=false}} async function loadParties(){if(!auth.can('masterdata:supplier:read'))return;const [f,c]=await Promise.all([get<{suppliers:Party[]}>('/suppliers',{page_size:200,status:'ACTIVE',business_type:'FORWARDER'}),get<{suppliers:Party[]}>('/suppliers',{page_size:200,status:'ACTIVE',business_type:'CARRIER'})]);forwarders.value=f.suppliers??[];carriers.value=c.suppliers??[]}
async function loadOptions(){if(!detail.value){requoteOptions.value=[];return}const id=detail.value.id;const d=await get<{options:RequoteOption[]}>(`/shipping/contract-handoffs/${id}/requote-options`);if(detail.value?.id!==id)return;requoteOptions.value=d.options??[];if(selectedOptionId.value&&!requoteOptions.value.some(x=>Number(x.id)===selectedOptionId.value))selectedOptionId.value=0}
let detailRequest=0,routeRequest=0
async function openDetail(r:Handoff,loaded?:Handoff){
 const request=++detailRequest
 detail.value=null;detailOpen.value=true;contractFile.value=null;selectedOptionId.value=0;requoteOptions.value=[]
 try{
  const handoff=loaded||(await get<{handoff:Handoff}>(`/shipping/contract-handoffs/${r.id}`,{},quietErrors)).handoff
  if(request!==detailRequest)return
  detail.value=handoff
  await loadApprovalTask()
  if(request!==detailRequest)return
  if(canManageCandidates.value)clearCandidateForm();else fillForm(handoff)
  if(mode.value==='inquiry')await loadOptions()
 }catch{
  if(request!==detailRequest)return
  detailOpen.value=false;detail.value=null;entryError.value=t('shipping.todoOpenError')
 }
} function selectForwarder(id:number){form.finalForwarderName=forwarders.value.find(x=>Number(x.id)===Number(id))?.name||''} function selectCarrier(id:number){form.actualCarrierName=carriers.value.find(x=>Number(x.id)===Number(id))?.name||''} async function refreshDetail(){if(!detail.value)return;await openDetail(detail.value);await load()}
async function loadApprovalTask(){
  approvalTaskId.value=''
  approvalOverride.value=false
  if(detail.value?.status!=='PENDING_APPROVAL')return
  const id=detail.value.id
  const result=await getActionableApproval('SHIPPING_REQUOTE',id)
  if(detail.value?.id!==id)return
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
function editOption(option:RequoteOption){editingOptionId.value=Number(option.id);Object.assign(form,{finalForwarderId:Number(option.forwarderId),finalForwarderName:option.forwarderName,actualCarrierId:Number(option.actualCarrierId||0),actualCarrierName:option.actualCarrierName||'',finalServiceOption:option.serviceOption,finalCurrency:option.currency,finalFreightAmount:option.freightAmount,finalEtd:option.etd,finalEta:option.eta,paymentTerms:option.paymentTerms,remark:option.remark||''});quoteEditorOpen.value=true}
async function saveDraft(){if(!form.finalForwarderId||!form.finalFreightAmount||!form.finalEtd||!form.finalEta||!form.paymentTerms.trim()){ElMessage.warning(t('shipping.requoteRequired'));return}saving.value=true;try{await post(`/shipping/contract-handoffs/${detail.value?.id}/draft`,requotePayload());ElMessage.success(t(editingOptionId.value?'shipping.candidateUpdated':'shipping.candidateSaved'));await loadOptions();quoteEditorOpen.value=false;clearCandidateForm();await load()}finally{saving.value=false}}
async function removeOption(option:RequoteOption){try{await ElMessageBox.confirm(t('shipping.deleteQuoteConfirm',{name:option.forwarderName,amount:option.currency+' '+option.freightAmount}),t('common.delete'),{type:'warning'})}catch{return}await del(`/shipping/contract-handoffs/${detail.value?.id}/requote-options/${option.id}`);ElMessage.success(t('shipping.candidateDeleted'));await loadOptions();if(editingOptionId.value===Number(option.id))clearCandidateForm()}
async function selectDraft(){if(!selectedOptionId.value||!detail.value)return;const id=detail.value.id;saving.value=true;try{await post(`/shipping/contract-handoffs/${id}/select-draft`,{option_id:selectedOptionId.value});ElMessage.success(t('shipping.selectedToDraft'));detailOpen.value=false;await router.push({path:'/shipping/orders',query:{handoff:id}})}finally{saving.value=false}}
async function submitRequote(){saving.value=true;try{await post(`/shipping/contract-handoffs/${detail.value?.id}/requote`,{});ElMessage.success(t('shipping.d4Submitted'));await refreshDetail()}finally{saving.value=false}} function pickContract(file:UploadFile){contractFile.value=file.raw??null}
async function uploadContract(){if(!contractFile.value){ElMessage.warning(t('shipping.contractRequired'));return}saving.value=true;try{const f=contractFile.value,s=await post<{fileKey:string;uploadUrl:string}>(`/shipping/contract-handoffs/${detail.value?.id}/contract/presign`,{file_name:f.name}),r=await fetch(s.uploadUrl,{method:'PUT',body:f});if(!r.ok)throw new Error(t('shipping.d4UploadFailed'));await post(`/shipping/contract-handoffs/${detail.value?.id}/contract`,{file_key:s.fileKey,file_name:f.name,contract_no:form.forwarderContractNo.trim()});ElMessage.success(t('shipping.d4ContractSaved'));await refreshDetail()}finally{saving.value=false}}
async function verifyContract() {
 if (!detail.value) return
 await ElMessageBox.confirm(t('supplierRecon.approveFinanceConfirm', { no:detail.value.contractNo, file:detail.value.signedContractName }), t('supplierRecon.approveFinance'), {type:'warning'})
 saving.value=true
 try {
  await post(`/shipping/contract-handoffs/${detail.value.id}/contract/verify`, {})
  ElMessage.success(t('supplierRecon.financeApproved'))
  await refreshDetail()
 } finally {saving.value=false}
}
function createSchedule(){if(!detail.value)return;if(Number(detail.value.scheduleId||0)>0){void router.push(`/shipping/${detail.value.scheduleId}`);return}void router.push({path:'/shipping/schedules',query:{handoff:detail.value.id}})}
async function openRoutedHandoff(request:number){
 entryError.value=''
 const id=String(route.query.handoff??'')
 if(!id)return
 if(!/^[1-9]\d*$/.test(id)){entryError.value=t('shipping.todoInvalidBatch');return}
 try{
  // Resolve this exact transport batch afresh. The task/list snapshot may be stale.
  const d=await get<{handoff:Handoff}>(`/shipping/contract-handoffs/${id}`,{},quietErrors)
  if(request!==routeRequest)return
  if(String(d.handoff.id)!==id)throw new Error('Mismatched handoff')
  const stage=shippingHandoffStage(d.handoff.status)
  if(!stage){entryError.value=t('shipping.todoUnknownStage');return}
  if(route.path!==stage.path){await router.replace({path:stage.path,query:{...route.query,handoff:id}});return}
  status.value=stage.filter;keyword.value=d.handoff.contractNo;page.value=1
  await load()
  if(request!==routeRequest)return
  await openDetail(d.handoff,d.handoff)
 }catch{
  if(request===routeRequest)entryError.value=t('shipping.todoOpenError')
 }
}
watch(()=>[props.mode,route.query.handoff] as const,async([nextMode],[previousMode])=>{
 const request=++routeRequest
 ++detailRequest;detailOpen.value=false;detail.value=null
 if(nextMode!==previousMode){keyword.value='';status.value=nextMode==='orders'?'DRAFT':''}
 await load()
 if(request===routeRequest)await openRoutedHandoff(request)
})
onMounted(async()=>{const request=++routeRequest;await Promise.all([load(),loadParties()]);if(request===routeRequest)await openRoutedHandoff(request)})
onUnmounted(()=>{++routeRequest;++detailRequest})
</script>

<style scoped>
.quote-table-heading{display:flex;align-items:center;justify-content:space-between;gap:12px;margin-bottom:8px}.quote-table-heading .section-title{margin-bottom:0}.quote-comparison{font-size:13px}.quote-comparison :deep(.el-table__cell){padding:7px 0}.quote-comparison :deep(.cell){line-height:20px;white-space:nowrap}.quote-comparison :deep(.selected-quote){--el-table-tr-bg-color:#edf9ff;--el-table-row-hover-bg-color:#e5f5ff}.quote-comparison :deep(.el-radio){margin-right:0;height:22px}.quote-comparison :deep(.el-radio__label){padding:0}.quote-amount{font-weight:600;color:#087f6f;font-variant-numeric:tabular-nums}.selection-help{display:block;margin-top:3px;font-size:12px;font-weight:400}.sr-only{position:absolute;width:1px;height:1px;overflow:hidden;clip-path:inset(50%)}.quote-comparison :deep(.el-button.is-link){padding:0;height:auto}.quote-editor-content .stage-actions{gap:8px}.quote-editor-content{padding-top:8px!important}.quote-comparison :deep(.quote-money-column .cell){padding-right:24px}.is-inquiry .quote-selection-footer{background:transparent;border:0;border-radius:0;padding:8px 0 0;margin-top:6px;min-height:28px}.quote-selection-footer>span{min-width:0}.quote-selection-footer>.el-button{flex-shrink:0}
.entry-error{margin-bottom:14px}
.detail-content{width:100%;box-sizing:border-box;padding:8px 16px 24px}.detail-content .form-grid{grid-template-columns:repeat(2,minmax(0,1fr));gap:0 24px}.detail-content .candidate-editor{padding:20px 24px}.detail-content .section-title{flex-wrap:wrap}.detail-content .candidate-main{overflow-wrap:anywhere}.detail-content .candidate-side{gap:12px;margin-left:24px}.detail-content :deep(.el-table){--el-table-header-bg-color:#eef9fe;--el-table-header-text-color:#24323a;--el-table-row-hover-bg-color:#f0fbf6}.detail-content .submit-selection{padding:16px 20px}@media(max-width:700px){.detail-content{padding:0 0 16px}.detail-content .form-grid{grid-template-columns:1fr}.detail-content .candidate-editor{padding:16px}.detail-content .candidate-side{margin-left:0}}
.order-tabs{margin-bottom:14px}
.shipping-requirements-page{color:#141817}.summary-strip{display:grid;grid-template-columns:repeat(3,minmax(0,1fr));gap:12px;margin-bottom:16px}.summary-strip>div{display:flex;align-items:baseline;gap:10px;padding:14px 18px;border:1px solid #d7e9ef;border-radius:12px;background:#fff}.summary-strip strong{font-size:24px;color:#0b84bd}.summary-strip span{color:#627482}.workbench{border-color:#d7e9ef}.filters{display:flex;gap:10px;margin-bottom:14px}.filters :deep(.el-input){width:320px}.filters :deep(.el-select){width:190px}.shipping-requirements-page :deep(.el-table){--el-table-header-bg-color:#eef9fe;--el-table-header-text-color:#24323a;--el-table-row-hover-bg-color:#f0fbf6}.shipping-requirements-page :deep(.el-table th.el-table__cell){border-bottom-color:#d9edf5;font-weight:650}.contract-no{color:#159fdc;font-weight:650}.sub,.stage-note{margin-top:3px;color:#697a87;font-size:12px}.money{font-variant-numeric:tabular-nums}.pager{justify-content:flex-end;margin-top:14px}.detail-hero{display:flex;align-items:center;justify-content:space-between;padding:14px 16px;margin-bottom:14px;border-radius:12px;background:linear-gradient(100deg,#edf9ff,#f0fcf6)}.detail-hero div{display:flex;flex-direction:column;gap:5px}.detail-hero strong{font-size:18px;color:#173a4d}.reference-card,.cargo-card,.stage-card{padding:16px;margin-bottom:14px;border:1px solid #dfe9ed;border-radius:12px;background:#fff}.reference-card{background:#f8fbfc}.cargo-card{padding-bottom:8px}.cargo-card :deep(.el-table){border:1px solid #e2edf1;border-radius:8px}.stage-card.is-current{border-left:4px solid #43bdf4;box-shadow:0 7px 20px rgba(42,107,133,.07)}.section-title{display:flex;align-items:center;gap:8px;margin-bottom:14px;font-size:16px;font-weight:650;color:#193d50}.section-title b{display:inline-grid;place-items:center;width:24px;height:24px;border-radius:50%;background:#e7f7ff;color:#0589c5}.section-title span{font-size:12px;font-weight:400;color:#788995}.candidate-list{display:grid;gap:9px}.candidate-option{display:flex;align-items:flex-start;gap:10px;padding:12px 14px;border:1px solid #dce8ed;border-radius:10px;background:#fff;cursor:pointer;transition:.15s ease}.candidate-option:hover,.candidate-option.selected{border-color:#45bdf3;background:#f0faff}.candidate-option :deep(.el-radio){margin-top:2px;margin-right:0}.candidate-main{display:flex;min-width:0;flex:1;flex-direction:column;gap:3px}.candidate-main strong{color:#173a4d}.candidate-main span{color:#405664}.candidate-main small{color:#73838e}.candidate-side{display:flex;align-items:flex-end;flex-direction:column;gap:8px;white-space:nowrap}.candidate-side>strong{color:#087f6f;font-variant-numeric:tabular-nums}.candidate-editor{padding:14px;margin-top:14px;border-radius:10px;background:#f7fbfd}.editor-title{margin-bottom:12px;color:#173a4d;font-weight:650}.submit-selection{display:flex;align-items:center;justify-content:space-between;gap:16px;padding:13px 14px;margin-top:14px;border:1px solid #cfeaf5;border-radius:10px;background:linear-gradient(90deg,#f2fbff,#f3fcf8);color:#5f7280}.form-grid{display:grid;grid-template-columns:1fr 1fr;gap:0 14px}.form-grid :deep(.el-select){width:100%}.stage-actions{display:flex;justify-content:flex-end;margin-top:10px}.file-row{display:flex;justify-content:space-between;gap:12px;padding:10px 12px;margin-bottom:10px;border-radius:8px;background:#f3f8fa}.file-row span{color:#788995;font-size:12px}.success-line{color:#1a9b63}.payment-grid{display:grid;grid-template-columns:repeat(3,1fr);gap:10px}.payment-grid span{display:flex;flex-direction:column;gap:5px;padding:10px;border-radius:8px;background:#f3f8fa;color:#6b7b87;font-size:12px}.payment-grid strong{font-size:16px;color:#173a4d}@media(max-width:700px){.summary-strip,.form-grid,.payment-grid{grid-template-columns:1fr}.filters{flex-direction:column}.filters :deep(.el-input),.filters :deep(.el-select){width:100%}.candidate-option,.submit-selection{align-items:stretch;flex-direction:column}.candidate-side{align-items:flex-start}.pager{justify-content:flex-start;overflow-x:auto}}
.source-sales-remark{margin-bottom:14px;padding:13px 16px;border:1px solid #cfe3ea;border-radius:10px;background:#f4fafc;color:#17485d}.source-sales-remark p{margin:6px 0 0;white-space:pre-wrap;overflow-wrap:anywhere;color:#334e5c}
</style>

<style scoped>
.detail-content.is-inquiry{max-width:1400px;margin:0 auto;padding:0 16px 18px;font-size:13px;line-height:1.4;--el-font-size-base:13px;--el-component-size:28px}.is-inquiry .detail-hero{padding:8px 12px;margin-bottom:5px;gap:16px;border-radius:6px}.is-inquiry .detail-hero>div:first-child{flex:1}.is-inquiry .detail-hero span{font-size:12px}.is-inquiry .detail-hero strong{font-size:16px}.is-inquiry .detail-hero .batch-total{min-width:130px}.inquiry-next{margin:0 0 8px;font-size:12px;line-height:1.4;color:#536f7e}.return-reason{margin:0 0 8px}.is-inquiry .stage-card{border-radius:6px;padding:10px 12px;margin-bottom:0;box-shadow:none}.is-inquiry .section-title{font-size:14px;margin-bottom:6px;gap:10px}.is-inquiry .section-title span{font-size:12px}.is-inquiry .candidate-empty{margin:0 0 6px;font-size:12px;color:#71818b}.is-inquiry .candidate-editor{margin-top:6px;padding:8px 10px}.is-inquiry .editor-title{font-size:13px;margin-bottom:8px}.is-inquiry .form-grid{grid-template-columns:repeat(3,minmax(0,1fr));gap:0 12px}.is-inquiry :deep(.el-form-item){margin-bottom:8px}.is-inquiry :deep(.el-form-item__label){font-size:12px;line-height:18px;margin-bottom:4px}.is-inquiry :deep(.el-input),.is-inquiry :deep(.el-select){font-size:13px}.is-inquiry :deep(.el-input__wrapper),.is-inquiry :deep(.el-select__wrapper){min-height:28px}.is-inquiry :deep(.el-button){font-size:12px;height:28px;padding:6px 11px}.is-inquiry :deep(.el-alert__title){font-size:12px;line-height:18px}.is-inquiry .stage-actions{margin-top:0}.is-inquiry .submit-selection{font-size:12px;line-height:1.4;padding:8px 10px;margin-top:8px;border-radius:5px}.is-inquiry .candidate-option{font-size:13px;line-height:1.4;padding:8px 10px;gap:8px;border-radius:5px}.is-inquiry .candidate-main{gap:2px}.is-inquiry .candidate-main small{font-size:12px}.is-inquiry .candidate-side{margin-left:10px;gap:4px}.is-inquiry .candidate-list{gap:6px}.is-inquiry .quote-note{grid-column:1/-1}.quote-note-text{white-space:pre-wrap;overflow-wrap:anywhere}
:global(.shipping-inquiry-dialog .el-dialog__title){font-size:16px;line-height:22px}:global(.shipping-inquiry-dialog .el-dialog__header){padding-bottom:8px}
@media(max-width:1050px){.is-inquiry .form-grid{grid-template-columns:repeat(2,minmax(0,1fr))}.is-inquiry .detail-hero{flex-wrap:wrap}}
@media(max-width:650px){.detail-content.is-inquiry{padding:0 0 16px}.is-inquiry .form-grid{grid-template-columns:1fr}.is-inquiry .detail-hero .batch-total{min-width:0}.is-inquiry .section-title span{width:100%}}

</style>
