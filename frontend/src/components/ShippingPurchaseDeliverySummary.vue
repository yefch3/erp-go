<template>
  <section class="purchase-delivery-summary" v-loading="loading">
    <div class="summary-heading"><h3>工厂交货情况</h3><el-button size="small" :loading="loading" @click="load">刷新交货情况</el-button></div>
    <p>由采购确认后同步，只读展示相关采购单累计交货数量，不代表本批装船数量。</p>
    <el-alert v-if="failed" title="工厂交货情况加载失败，请重试" type="error" :closable="false"/>
    <el-table v-else :data="items" size="small" empty-text="暂无关联的直发采购单">
      <el-table-column prop="poNo" label="采购单" min-width="155"/>
      <el-table-column prop="productName" label="产品" min-width="190" show-overflow-tooltip/>
      <el-table-column label="采购数量" width="115" align="right"><template #default="{row}">{{row.qty}} {{row.uomCode}}</template></el-table-column>
      <el-table-column label="工厂已交" width="115" align="right"><template #default="{row}">{{row.deliveredQty}} {{row.uomCode}}</template></el-table-column>
      <el-table-column label="剩余" width="110" align="right"><template #default="{row}">{{Math.max(0,Number(row.qty)-Number(row.deliveredQty))}} {{row.uomCode}}</template></el-table-column>
      <el-table-column label="操作" width="110"><template #default="{row}"><el-button link type="primary" @click="selectedId=row.orderId;opened=true">查看交货记录</el-button></template></el-table-column>
    </el-table>
    <PurchaseDeliveryDialog v-model="opened" :order-id="selectedId" read-only/>
  </section>
</template>
<script setup lang="ts">
import {ref,watch,onUnmounted} from 'vue'
import {get,quietErrors} from '../api'
import PurchaseDeliveryDialog from './PurchaseDeliveryDialog.vue'
const props=defineProps<{handoffId:string}>()
type Item={orderId:string;poNo:string;productName:string;qty:string;deliveredQty:string;uomCode:string}
const items=ref<Item[]>([]),loading=ref(false),failed=ref(false),opened=ref(false),selectedId=ref('')
let generation=0,alive=true
async function load(){const request=++generation;loading.value=true;failed.value=false;try{const d=await get<{items:Item[]}>(`/shipping/contract-handoffs/${props.handoffId}/purchase-deliveries`,{},quietErrors);if(alive&&request===generation)items.value=d.items??[]}catch{if(alive&&request===generation){items.value=[];failed.value=true}}finally{if(alive&&request===generation)loading.value=false}}
watch(()=>props.handoffId,()=>{opened.value=false;items.value=[];void load()},{immediate:true})
const timer=setInterval(()=>{if(!loading.value)void load()},15000)
onUnmounted(()=>{alive=false;generation++;clearInterval(timer)})
</script>
<style scoped>
.summary-heading{display:flex;align-items:center;justify-content:space-between;gap:12px}.summary-heading h3{font-size:16px;margin:0}.purchase-delivery-summary{margin:18px 0}.purchase-delivery-summary>p{font-size:13px;color:var(--el-text-color-secondary)}
</style>
