<template>
  <el-dialog v-model="visible" :title="form.id ? '补齐物流实单资料' : '建立物流实单'" width="min(1200px, 96vw)" :close-on-click-modal="false" destroy-on-close>
    <p class="hint">独立补录物流业务。外销合同单号可先手填，合同尚未录入也能保存；选择已有合同或按单号唯一匹配后关联；尚未录入的号码保留待关联，不重复生成业务单据。</p>
    <el-form label-position="top" size="small">
      <div class="fields">
        <el-form-item label="物流实单号"><el-input v-model="form.manualOrderNo" :disabled="!!form.id" maxlength="80" placeholder="留空自动生成" /></el-form-item>
        <el-form-item label="合同号" required><ShippingContractNumberInput v-model="form.contractNo" :disabled="linkedAtLoad" @selected="form.linkedContractId=$event" /></el-form-item>
        <el-form-item label="客户简称（选填）"><el-input v-model="form.customerName" maxlength="200" /></el-form-item>
        <el-form-item label="币种"><el-select v-model="form.finalCurrency"><el-option v-for="c in ['USD','CNY','EUR','GBP','CAD','AUD','HKD']" :key="c" :value="c" /></el-select></el-form-item>
        <el-form-item label="起运港"><el-input v-model="form.portOfLoading" maxlength="100" /></el-form-item>
        <el-form-item label="目的港"><el-input v-model="form.portOfDischarge" maxlength="100" /></el-form-item>
        <el-form-item label="货代 / 物流供应商"><el-select v-model="form.finalForwarderId" clearable filterable @change="selectParty('forwarder')"><el-option v-for="p in forwarders" :key="p.id" :value="String(p.id)" :label="p.name" /></el-select></el-form-item>
        <el-form-item label="承运方（选填）"><el-select v-model="form.actualCarrierId" clearable filterable @change="selectParty('carrier')"><el-option v-for="p in carriers" :key="p.id" :value="String(p.id)" :label="p.name" /></el-select></el-form-item>
        <el-form-item label="运输方案"><el-input v-model="form.finalServiceOption" maxlength="200" /></el-form-item>
        <el-form-item label="物流费用"><el-input v-model="form.finalFreightAmount" placeholder="可稍后补齐" /></el-form-item>
        <el-form-item label="预计开船日期"><el-date-picker v-model="form.finalEtd" value-format="YYYY-MM-DD" /></el-form-item>
        <el-form-item label="预计到港日期"><el-date-picker v-model="form.finalEta" value-format="YYYY-MM-DD" /></el-form-item>
        <el-form-item label="付款条件"><el-input v-model="form.paymentTerms" /></el-form-item>
        <el-form-item label="货代合同号（选填）"><el-input v-model="form.forwarderContractNo" maxlength="80" /></el-form-item>
      </div>
      <ColumnFillRegion :rows="form.cargoItems" :fields="[{key:'productName',column:0},{key:'specification',column:1},{key:'quantity',column:2},{key:'uomCode',column:3},{key:'remark',column:4}]">
      <el-table :data="form.cargoItems" size="small" class="cargo-lines">
        <el-table-column label="产品名称" min-width="160"><template #default="{row}"><el-input v-model="row.productName" maxlength="200" /></template></el-table-column>
        <el-table-column label="规格" min-width="180"><template #default="{row}"><el-input v-model="row.specification" /></template></el-table-column>
        <el-table-column label="数量" width="120"><template #default="{row}"><el-input v-model="row.quantity" /></template></el-table-column>
        <el-table-column label="单位" width="100"><template #default="{row}"><el-input v-model="row.uomCode" maxlength="20" /></template></el-table-column>
        <el-table-column label="备注" min-width="140"><template #default="{row}"><el-input v-model="row.remark" /></template></el-table-column>
        <el-table-column width="65"><template #default="{$index}"><el-button link type="danger" @click="form.cargoItems.splice($index,1)">移除</el-button></template></el-table-column>
      </el-table>
      </ColumnFillRegion>
      <el-button class="add" size="small" @click="form.cargoItems.push(emptyLine())">添加产品</el-button>
      <el-form-item label="备注"><el-input v-model="form.remark" type="textarea" :rows="2" /></el-form-item>
    </el-form>
    <template #footer><el-button :disabled="saving" @click="visible=false">取消</el-button><el-button type="primary" :loading="saving" @click="save">保存实单</el-button></template>
  </el-dialog>
</template>

<script setup lang="ts">
import { computed, reactive, ref, watch } from 'vue'
import { ElMessage } from 'element-plus'
import { get, post } from '../api'
import ShippingContractNumberInput from './ShippingContractNumberInput.vue'
import ColumnFillRegion from './ColumnFillRegion.vue'
interface Cargo { productCode:string;productName:string;specification:string;quantity:string;uomCode:string;remark:string }
interface ManualOrder { linkedContractId:string; id:string;manualOrderNo:string;contractNo:string;customerName:string;portOfLoading:string;portOfDischarge:string;finalForwarderId:string;finalForwarderName:string;actualCarrierId:string;actualCarrierName:string;finalServiceOption:string;finalCurrency:string;finalFreightAmount:string;finalEtd:string;finalEta:string;paymentTerms:string;forwarderContractNo:string;remark:string;cargoItems:Cargo[];amountMissing?:boolean }
const props=defineProps<{modelValue:boolean;orderId?:string;forwarders:{id:string;name:string}[];carriers:{id:string;name:string}[]}>()
const emit=defineEmits<{ 'update:modelValue':[boolean];saved:[] }>()
const visible=computed({get:()=>props.modelValue,set:v=>emit('update:modelValue',v)}),saving=ref(false)
const emptyLine=():Cargo=>({productCode:'',productName:'',specification:'',quantity:'',uomCode:'',remark:''})
const emptyForm=():ManualOrder=>({linkedContractId:'0',id:'',manualOrderNo:'',contractNo:'',customerName:'',portOfLoading:'',portOfDischarge:'',finalForwarderId:'',finalForwarderName:'',actualCarrierId:'',actualCarrierName:'',finalServiceOption:'',finalCurrency:'USD',finalFreightAmount:'',finalEtd:'',finalEta:'',paymentTerms:'',forwarderContractNo:'',remark:'',cargoItems:[]})
const form=reactive(emptyForm()),linkedAtLoad=ref(false)
watch(()=>props.modelValue,async open=>{if(!open)return;Object.assign(form,emptyForm());linkedAtLoad.value=false;if(props.orderId){saving.value=true;try{const r=await get<{handoff:ManualOrder}>(`/shipping/contract-handoffs/${props.orderId}`);Object.assign(form,r.handoff,{cargoItems:(r.handoff.cargoItems||[]).map(c=>({...c}))});linkedAtLoad.value=Number(form.linkedContractId||0)>0;if(form.amountMissing)form.finalFreightAmount='';if(form.finalForwarderId==='0')form.finalForwarderId='';if(form.actualCarrierId==='0')form.actualCarrierId=''}catch(e){visible.value=false;throw e}finally{saving.value=false}}})
function selectParty(kind:'forwarder'|'carrier'){if(kind==='forwarder')form.finalForwarderName=props.forwarders.find(p=>String(p.id)===form.finalForwarderId)?.name||'';else form.actualCarrierName=props.carriers.find(p=>String(p.id)===form.actualCarrierId)?.name||''}
async function save(){
 if(saving.value)return
 if(!form.contractNo.trim()){ElMessage.warning('请填写合同号，系统暂无此合同也可以保存');return}
 saving.value=true
 try{const result=await post<{order:ManualOrder}>('/shipping/manual-orders',{order:{...form,id:form.id||'0',finalForwarderId:form.finalForwarderId||'0',actualCarrierId:form.actualCarrierId||'0',finalEtd:form.finalEtd||'',finalEta:form.finalEta||'',cargoItems:form.cargoItems.filter(c=>[c.productName,c.specification,c.quantity,c.uomCode,c.remark].some(v=>v.trim()))}});ElMessage.success(Number(result.order.linkedContractId)>0?'物流实单已保存，合同已关联':'物流实单已保存，合同待关联');visible.value=false;emit('saved')}finally{saving.value=false}
}
</script>

<style scoped>
.hint{font-size:13px;color:#6b7a86;background:#f5f8fa;padding:12px;margin:0 0 16px}.fields{display:grid;grid-template-columns:repeat(4,minmax(0,1fr));gap:0 16px}.fields :deep(.el-select),.fields :deep(.el-date-editor){width:100%}.cargo-lines :deep(.el-input__wrapper){box-shadow:none;background:transparent;padding:0 4px;border-bottom:1px solid transparent;border-radius:0}.cargo-lines :deep(.el-input__wrapper.is-focus){border-color:#40b9ef;background:#f1faff}.cargo-lines :deep(.el-table__cell){padding:5px 0}.add{margin:12px 0}.fields :deep(.el-form-item__label){font-size:12px}@media(max-width:800px){.fields{grid-template-columns:repeat(2,minmax(0,1fr))}}
</style>
