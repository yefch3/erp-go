<template>
 <div v-if="references.length" class="presale-snapshots">
  <strong>售前参考资料</strong><small>仅供对照，最终工厂或货代仍须重新询价确认。</small>
  <el-table :data="references" border size="small">
   <el-table-column label="来源" min-width="130"><template #default="{row}">{{row.kind==='PROCUREMENT'?'工厂报价':row.kind==='LOGISTICS'?'物流报价':'运输方案'}} {{row.id?`#${row.id}`:''}}<small v-if="row.version">版本 {{row.version}}</small></template></el-table-column>
   <el-table-column label="公司 / 方案" min-width="170"><template #default="{row}">{{body(row).company||row.carrier_forwarder||'未记录'}}<small>{{body(row).serviceOption||body(row).route||row.service_option_name||''}}</small></template></el-table-column>
   <el-table-column label="报价条件" min-width="160"><template #default="{row}">{{body(row).currency||row.currency||''}} {{body(row).payment||body(row).paymentTerms||''}}<small>{{body(row).validUntil?`有效期 ${body(row).validUntil}`:''}}</small></template></el-table-column>
   <el-table-column label="报价内容" min-width="200"><template #default="{row}"><div v-for="(price,index) in (body(row).prices||[]).filter((p:any)=>!row.productId||p.productId===row.productId)" :key="index">{{row.productName?`${row.productName}：`:''}}{{price.factoryPrice||price.price||price.fobPrice||'—'}} {{body(row).currency}}</div><div v-for="(rate,index) in (body(row).freightRates||[])" :key="`rate-${index}`">海运单价 {{rate.price}} {{rate.currency}} / {{rate.unit}}</div><div v-for="(charge,index) in (body(row).charges||[])" :key="`charge-${index}`">{{charge.name}}：{{charge.amount}} {{charge.currency}} × {{charge.quantity}}</div><span v-if="!body(row).prices?.length&&!body(row).freightRates?.length&&!body(row).charges?.length">{{body(row).price||body(row).freightAmount||row.freight_amount||'—'}}</span><small>{{body(row).remark||row.remark||''}}</small></template></el-table-column>
  </el-table>
 </div>
</template>
<script setup lang="ts">
import {computed} from 'vue'
const props=defineProps<{json?:string}>()
type Reference={id?:string;kind?:string;version?:number;body?:Record<string,any>;[key:string]:any}
const references=computed<Reference[]>(()=>{try{const parsed=JSON.parse(props.json||'null');return Array.isArray(parsed)?parsed:parsed?[parsed]:[]}catch{return []}})
function body(row:Reference){return row.body||{}}
</script>
<style scoped>
.presale-snapshots{margin:10px 0;padding:10px;background:#f6f9fc;border:1px solid #e1e8f0;border-radius:6px}.presale-snapshots>small{margin:4px 0 10px}.presale-snapshots small{display:block;font-size:12px;line-height:1.5;color:#64748b}
</style>
