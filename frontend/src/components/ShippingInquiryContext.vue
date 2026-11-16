<template>
  <section class="inquiry-context">
    <h3>{{t('shipping.inquirySupportTitle')}}</h3>
    <div v-if="detail.sourceSalesRemark?.trim()" class="sales-note"><strong>{{t('inquiryWorkspace.detail.salesRemark')}}</strong><p>{{detail.sourceSalesRemark}}</p></div>
    <div class="cargo-heading"><strong>{{t('shipping.inquiryCargo')}}</strong><span>{{t('shipping.inquiryCargoCount',{n:detail.cargoItems?.length||0})}}</span></div>
    <el-table :data="detail.cargoItems||[]" size="small" class="batch-cargo" :empty-text="t('shipping.noCargoDetails')">
      <el-table-column :label="t('shipping.cargoProduct')" min-width="280" show-overflow-tooltip><template #default="{row}"><strong>{{row.productName}}</strong><div v-if="row.productCode" class="sub">{{row.productCode}}</div></template></el-table-column>
      <el-table-column prop="specification" :label="t('shipping.cargoSpecification')" width="180" show-overflow-tooltip><template #default="{row}">{{row.specification||'—'}}</template></el-table-column>
      <el-table-column :label="t('shipping.cargoQuantity')" width="130" align="right"><template #default="{row}">{{number(row.quantity)}} {{row.uomCode}}</template></el-table-column>
      <el-table-column prop="remark" :label="t('shipping.contractRemark')" min-width="260" show-overflow-tooltip><template #default="{row}">{{row.remark||'—'}}</template></el-table-column>
    </el-table>
    <el-collapse class="reference-collapse">
      <el-collapse-item name="presale">
        <template #title><strong>{{t('shipping.inquiryReference')}}</strong><span class="reference-count">{{t('shipping.inquiryReferenceCount',{n:referenceCount})}}</span><span class="reference-hint">{{t('shipping.d4ReferenceOnly')}}</span></template>
        <PresaleReference v-if="detail.presaleReferencesJson" :json="detail.presaleReferencesJson"/>
        <el-descriptions v-else-if="hasLegacyReference" :column="2" size="small" border>
          <el-descriptions-item :label="t('shipping.d4Forwarder')">{{detail.carrierForwarder||'—'}}</el-descriptions-item>
          <el-descriptions-item :label="t('shipping.d4Plan')">{{detail.serviceOptionName||'—'}}</el-descriptions-item>
          <el-descriptions-item :label="t('shipping.d4Amount')">{{detail.currency}} {{detail.freightAmount}}</el-descriptions-item>
          <el-descriptions-item :label="t('shipping.d4Dates')">{{detail.estimatedDeparture||'—'}} / {{detail.estimatedArrival||'—'}}</el-descriptions-item>
        </el-descriptions>
        <p v-else class="empty-reference">{{t('shipping.inquiryReferenceEmpty')}}</p>
      </el-collapse-item>
    </el-collapse>
  </section>
</template>
<script setup lang="ts">
import {computed} from 'vue'
import {useI18n} from 'vue-i18n'
import PresaleReference from './PresaleReference.vue'
import {fulfillmentNumber as number} from '../lib/fulfillmentOverview'
import {presaleReferenceCount} from '../lib/shippingInquiryView'
const props=defineProps<{detail:{sourceSalesRemark:string;presaleReferencesJson?:string;manualOrderNo?:string;carrierForwarder:string;serviceOptionName:string;currency:string;freightAmount:string;estimatedDeparture:string;estimatedArrival:string;cargoItems:{productName:string;productCode:string;specification:string;quantity:string;uomCode:string;remark:string}[]}}>()
const {t}=useI18n()
const hasLegacyReference=computed(()=>!props.detail.manualOrderNo&&(props.detail.carrierForwarder||props.detail.serviceOptionName||Number(props.detail.freightAmount)>0||props.detail.estimatedDeparture||props.detail.estimatedArrival))
const referenceCount=computed(()=>props.detail.presaleReferencesJson?presaleReferenceCount(props.detail.presaleReferencesJson):hasLegacyReference.value?1:0)
</script>
<style scoped>
.inquiry-context{border:1px solid #dfe9ed;border-radius:8px;background:#fff;padding:10px 12px;margin-top:10px;font-size:13px}.inquiry-context h3{font-size:14px;margin:0 0 8px;color:#294551}.sales-note{display:flex;align-items:baseline;gap:12px;background:#f6fafc;border-radius:5px;padding:6px 8px;font-size:12px;line-height:1.5;margin-bottom:8px}.sales-note strong{flex-shrink:0;color:#3d5d6a}.sales-note p{margin:0;white-space:pre-wrap;overflow-wrap:anywhere;min-width:0}.cargo-heading{display:flex;align-items:center;gap:10px;font-size:13px;margin-bottom:6px}.cargo-heading span,.sub,.reference-count,.reference-hint,.empty-reference{font-size:12px;color:#6a7b85}.batch-cargo{font-size:13px}.batch-cargo :deep(.cell){line-height:20px}.reference-collapse :deep(.el-table){font-size:12px}.reference-collapse :deep(.el-table__cell){padding:4px 0}.reference-collapse :deep(.el-table .cell){line-height:19px}.reference-collapse :deep(.presale-snapshots>strong){font-size:13px}.batch-cargo strong{font-size:13px;font-weight:500}.batch-cargo :deep(.el-table__cell){padding:4px 0}.reference-collapse{margin-top:8px}.reference-collapse :deep(.el-collapse-item__header){height:32px;gap:10px;font-size:13px}.reference-collapse :deep(.el-collapse-item__content){padding-bottom:10px}.reference-collapse :deep(.presale-snapshots){margin:0;border:0;padding:0;background:transparent}.reference-hint{font-weight:400}
@media(max-width:650px){.sales-note{display:block}.reference-hint{display:none}}
</style>
