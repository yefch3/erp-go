<template>
  <el-dialog v-model="visible" :title="form.id ? '编辑历史物流订单' : '补录历史物流订单'" width="min(1160px, 96vw)" top="4vh" class="manual-shipping-dialog" :close-on-click-modal="false" destroy-on-close>
    <p class="form-hint">补录历史物流业务，资料由员工手工填写，创建后直接进入船期管理。外销合同号选填，不会自动带入资料或跳转。</p>
    <el-form label-position="top" size="small" @submit.prevent>
      <section class="form-section">
        <div class="section-heading"><h3>1. 历史订单信息</h3><el-tag v-if="form.id" size="small" type="info">{{ form.manualOrderNo }}</el-tag></div>
        <div class="fields contract-fields">
          <el-form-item label="外销合同号（选填）"><el-input v-model="form.contractNo" :disabled="linkedAtLoad" maxlength="50" placeholder="手工填写原外销合同号，也可留空" /></el-form-item>
          <el-form-item label="客户简称（选填）"><el-input v-model="form.customerName" maxlength="200" placeholder="填写历史订单客户简称" /></el-form-item>
        </div>
        <p class="field-hint">{{ linkedAtLoad ? '此历史订单已有合同关联，保留原关联关系。其他资料仍可手工填写。' : '外销合同号仅作历史参考，不自动关联系统合同。' }}</p>
      </section>
      <section class="form-section">
        <div class="section-heading"><h3>2. 货物明细</h3><span>可稍后补齐；已填写的行需有产品、数量和单位</span></div>
        <div class="cargo-scroll">
          <ColumnFillRegion :rows="form.cargoItems" :fields="cargoFields">
            <el-table :data="form.cargoItems" size="small" border class="cargo-lines" empty-text="点击“添加产品”开始录入">
              <el-table-column label="产品名称" min-width="210"><template #default="{row,$index}"><el-input v-model="row.productName" :aria-label="`第${$index+1}行产品名称`" maxlength="200" placeholder="输入产品名称" /></template></el-table-column>
              <el-table-column label="规格" min-width="180"><template #default="{row,$index}"><el-input v-model="row.specification" :aria-label="`第${$index+1}行规格`" placeholder="如 3mm × 1500mm" /></template></el-table-column>
              <el-table-column label="数量" width="125"><template #default="{row,$index}"><el-input v-model="row.quantity" :aria-label="`第${$index+1}行数量`" inputmode="decimal" placeholder="大于 0" /></template></el-table-column>
              <el-table-column label="单位" width="115"><template #default="{row,$index}"><el-input v-model="row.uomCode" :aria-label="`第${$index+1}行单位`" maxlength="20" placeholder="MT / PCS" /></template></el-table-column>
              <el-table-column label="备注（选填）" min-width="170"><template #default="{row,$index}"><el-input v-model="row.remark" :aria-label="`第${$index+1}行备注`" placeholder="货物说明" /></template></el-table-column>
              <el-table-column label="操作" width="65"><template #default="{$index}"><el-button link type="danger" :aria-label="`移除第${$index+1}行`" @click="removeLine($index)">移除</el-button></template></el-table-column>
            </el-table>
          </ColumnFillRegion>
        </div>
        <el-button class="add" size="small" @click="form.cargoItems.push(emptyLine())">＋ 添加产品</el-button>
      </section>
      <section class="form-section quote-section">
        <div class="section-heading"><h3>3. 运输与付款资料</h3><span>按历史资料填写，起运港、目的港和预计日期必填</span></div>
        <div class="fields">
          <el-form-item label="起运港" required><el-input v-model="form.portOfLoading" maxlength="100" placeholder="如 上海" /></el-form-item>
          <el-form-item label="目的港" required><el-input v-model="form.portOfDischarge" maxlength="100" placeholder="如 洛杉矶" /></el-form-item>
          <el-form-item label="货代 / 物流供应商（可后补）"><el-select v-model="form.finalForwarderId" clearable filterable placeholder="选择签约货代" @change="selectParty('forwarder')"><el-option v-for="p in forwarders" :key="p.id" :value="String(p.id)" :label="p.name" /></el-select></el-form-item>
          <el-form-item label="实际承运方（选填）"><el-select v-model="form.actualCarrierId" clearable filterable placeholder="与货代不同且已知时填写" @change="selectParty('carrier')"><el-option v-for="p in carriers" :key="p.id" :value="String(p.id)" :label="p.name" /></el-select></el-form-item>
          <el-form-item label="币种"><el-select v-model="form.finalCurrency"><el-option v-for="c in ['USD','CNY','EUR','GBP','CAD','AUD','HKD']" :key="c" :value="c" /></el-select></el-form-item>
          <el-form-item label="物流费用（可后补）"><el-input v-model="form.finalFreightAmount" inputmode="decimal" placeholder="填写总费用" /></el-form-item>
          <el-form-item label="预计开船日期" required><el-date-picker v-model="form.finalEtd" value-format="YYYY-MM-DD" placeholder="选择开船日期" /></el-form-item>
          <el-form-item label="预计到港日期" required><el-date-picker v-model="form.finalEta" value-format="YYYY-MM-DD" placeholder="选择到港日期" /></el-form-item>
          <el-form-item label="付款条件（可后补）" class="span-two"><el-autocomplete v-model="form.paymentTerms" :fetch-suggestions="suggestPaymentTerms" :placeholder="t('shipping.paymentTermsPlaceholder')" clearable :debounce="0" /></el-form-item>
          <el-form-item :label="t('shipping.optionalLogisticsContractNo')" class="span-two"><el-input v-model="form.forwarderContractNo" maxlength="80" placeholder="有原物流合同号时填写，也可留空" /></el-form-item>
          <el-form-item label="备注（选填）" class="note-field"><el-input v-model="orderNote" type="textarea" :rows="2" placeholder="填写运输、报价或其他业务补充说明" /></el-form-item>
        </div>
        <el-collapse v-if="!form.id" class="number-settings"><el-collapse-item title="手工指定历史物流订单号（选填，通常无需填写）" name="number"><el-input v-model="form.manualOrderNo" maxlength="80" placeholder="留空由系统自动生成历史物流订单号" /></el-collapse-item></el-collapse>
      </section>
    </el-form>
    <template #footer><div class="dialog-footer"><span>创建后直接进入船期管理，不生成审批或付款申请</span><div><el-button :disabled="saving" @click="visible=false">取消</el-button><el-button type="primary" :loading="saving" @click="save">创建并进入船期管理</el-button></div></div></template>
  </el-dialog>
</template>

<script setup lang="ts">
import { computed, reactive, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { ElMessage } from 'element-plus'
import { get, post } from '../api'
import ColumnFillRegion from './ColumnFillRegion.vue'
import { manualCargoError, manualCargoLines, type ManualCargoLine } from '../lib/manualShippingForm'
import { quoteNoteText } from '../lib/shippingInquiryView'
const { t }=useI18n()
interface ManualOrder { linkedContractId:string; id:string;manualOrderNo:string;contractNo:string;customerName:string;portOfLoading:string;portOfDischarge:string;finalForwarderId:string;finalForwarderName:string;actualCarrierId:string;actualCarrierName:string;finalServiceOption:string;finalCurrency:string;finalFreightAmount:string;finalEtd:string;finalEta:string;paymentTerms:string;forwarderContractNo:string;remark:string;cargoItems:ManualCargoLine[];scheduleId?:string;amountMissing?:boolean }
const props=defineProps<{modelValue:boolean;orderId?:string;forwarders:{id:string;name:string}[];carriers:{id:string;name:string}[]}>()
const emit=defineEmits<{ 'update:modelValue':[boolean];saved:[scheduleId:string] }>()
const visible=computed({get:()=>props.modelValue,set:v=>emit('update:modelValue',v)}),saving=ref(false)
const emptyLine=():ManualCargoLine=>({productCode:'',productName:'',specification:'',quantity:'',uomCode:'',remark:''})
const emptyForm=():ManualOrder=>({linkedContractId:'0',id:'',manualOrderNo:'',contractNo:'',customerName:'',portOfLoading:'',portOfDischarge:'',finalForwarderId:'',finalForwarderName:'',actualCarrierId:'',actualCarrierName:'',finalServiceOption:'',finalCurrency:'USD',finalFreightAmount:'',finalEtd:'',finalEta:'',paymentTerms:'',forwarderContractNo:'',remark:'',cargoItems:[emptyLine()]})
const form=reactive(emptyForm()),linkedAtLoad=ref(false)
const orderNote=computed({get:()=>quoteNoteText(form.finalServiceOption,form.remark),set:(value:string)=>{form.finalServiceOption='';form.remark=value}})
const cargoFields=[{key:'productName',column:0},{key:'specification',column:1},{key:'quantity',column:2},{key:'uomCode',column:3},{key:'remark',column:4}]
let openRequest=0
watch(()=>props.modelValue,async open=>{
 const request=++openRequest;saving.value=false;if(!open)return
 Object.assign(form,emptyForm());linkedAtLoad.value=false
 if(props.orderId){saving.value=true;try{const r=await get<{handoff:ManualOrder}>(`/shipping/contract-handoffs/${props.orderId}`);if(request!==openRequest)return;Object.assign(form,r.handoff,{cargoItems:r.handoff.cargoItems?.length?r.handoff.cargoItems.map(c=>({...c})):[emptyLine()]});linkedAtLoad.value=Number(form.linkedContractId||0)>0;if(form.amountMissing)form.finalFreightAmount='';if(form.finalForwarderId==='0')form.finalForwarderId='';if(form.actualCarrierId==='0')form.actualCarrierId=''}catch(e){if(request===openRequest)visible.value=false;throw e}finally{if(request===openRequest)saving.value=false}}
})
function removeLine(index:number){form.cargoItems.splice(index,1);if(!form.cargoItems.length)form.cargoItems.push(emptyLine())}
function selectParty(kind:'forwarder'|'carrier'){if(kind==='forwarder')form.finalForwarderName=props.forwarders.find(p=>String(p.id)===form.finalForwarderId)?.name||'';else form.actualCarrierName=props.carriers.find(p=>String(p.id)===form.actualCarrierId)?.name||''}
function suggestPaymentTerms(query:string,callback:(items:{value:string}[])=>void){const values=[t('shipping.paymentTermAdvance'),t('shipping.paymentTermBeforeShipment'),t('shipping.paymentTermBlCopy'),t('shipping.paymentTermArrival'),t('shipping.paymentTermNet30')];callback(values.filter(value=>value.toLocaleLowerCase().includes(query.trim().toLocaleLowerCase())).map(value=>({value})))}
async function save(){
 if(saving.value)return
 if(!form.portOfLoading.trim()||!form.portOfDischarge.trim()||!form.finalEtd||!form.finalEta){ElMessage.warning('请填写起运港、目的港、预计开船日期和预计到港日期');return}
 const cargoError=manualCargoError(form.cargoItems);if(cargoError){ElMessage.warning(cargoError);return}
 saving.value=true
 try{const result=await post<{order:ManualOrder}>('/shipping/manual-orders',{order:{...form,id:form.id||'0',finalForwarderId:form.finalForwarderId||'0',actualCarrierId:form.actualCarrierId||'0',finalEtd:form.finalEtd||'',finalEta:form.finalEta||'',cargoItems:manualCargoLines(form.cargoItems)}});ElMessage.success('历史物流订单已创建，进入船期管理');visible.value=false;emit('saved',result.order.scheduleId||'')}finally{saving.value=false}
}
</script>

<style scoped>
.note-field{grid-column:1 / -1}
:global(.manual-shipping-dialog .el-dialog__body){height:calc(88vh - 130px);overflow-y:auto;padding:12px 20px}:global(.manual-shipping-dialog .el-dialog__footer){border-top:1px solid #e5edf2;padding-top:12px}
.form-hint{font-size:13px;color:#607484;background:#f3f9fc;padding:10px 12px;margin:0 0 12px;border-radius:6px}.form-section{margin-bottom:14px;border:1px solid #deebf1;border-radius:8px;padding:12px 14px}.section-heading{display:flex;align-items:center;justify-content:space-between;gap:12px;margin-bottom:10px}.section-heading h3{margin:0;color:#193e52;font-size:15px}.section-heading>span,.field-hint{font-size:12px;color:#71828e}.field-hint{margin:0;line-height:1.6}.fields{display:grid;grid-template-columns:repeat(4,minmax(0,1fr));gap:0 14px}.contract-fields{grid-template-columns:repeat(2,minmax(0,1fr))}.fields :deep(.el-select),.fields :deep(.el-date-editor),.fields :deep(.el-autocomplete){width:100%}.fields :deep(.el-form-item){margin-bottom:12px}.fields :deep(.el-form-item__label){font-size:12px;line-height:18px;margin-bottom:5px}.span-two{grid-column:span 2}.cargo-scroll{overflow-x:auto}.cargo-lines{min-width:900px}.cargo-lines :deep(.el-input__wrapper){background:#fff;box-shadow:0 0 0 1px #cbd8e2 inset;border-radius:4px;padding:1px 8px}.cargo-lines :deep(.el-input__wrapper.is-focus){box-shadow:0 0 0 1px #409eff inset}.cargo-lines :deep(.el-table__cell){padding:7px 0}.cargo-scroll :deep(.el-table){--el-table-header-bg-color:#eef9fe;--el-table-header-text-color:#24323a}.add{margin-top:10px}.number-settings{font-size:12px}.dialog-footer{display:flex;align-items:center;justify-content:space-between;gap:12px}.dialog-footer>span{font-size:12px;color:#71828e}.dialog-footer>div{white-space:nowrap}@media(max-width:800px){.fields,.contract-fields{grid-template-columns:repeat(2,minmax(0,1fr))}.section-heading{align-items:flex-start;flex-direction:column}.dialog-footer{align-items:flex-end}.dialog-footer>span{max-width:45%}}@media(max-width:500px){.fields,.contract-fields{grid-template-columns:1fr}.span-two{grid-column:span 1}}
</style>
