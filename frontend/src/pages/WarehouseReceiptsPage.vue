<template>
  <div class="page">
    <header class="hero">
      <div><p class="eyebrow">RECEIPT HISTORY</p><h1>收货记录</h1><p>查看采购分批收货与直送签收记录；这里展示业务凭证，不重复登记库存。</p></div>
      <div><el-button @click="router.push('/warehouses/arrivals')">待到货</el-button><el-button @click="router.push('/warehouses')">返回工作台</el-button></div>
    </header>
    <el-card shadow="never" class="filters">
      <el-input v-model="keyword" clearable placeholder="搜索收货单、采购单或供应商" @keyup.enter="applyFilters" @clear="applyFilters" />
      <el-select v-model="deliveryType" @change="applyFilters"><el-option label="全部类型" value="ALL"/><el-option label="直接交付" value="DIRECT"/><el-option label="仓库收货" value="WAREHOUSE"/></el-select>
      <el-button type="primary" @click="applyFilters">查询</el-button>
    </el-card>
    <el-card shadow="never">
      <el-table :data="pagedRows" v-loading="loading" row-key="id">
        <el-table-column label="收货单号" min-width="170"><template #default="{row}"><strong>{{row.receiptNo}}</strong><div class="sub">{{formatTime(row.receivedAt)}}</div></template></el-table-column>
        <el-table-column label="采购单" min-width="150" prop="poNo"/>
        <el-table-column label="供应商" min-width="180" prop="supplierName"/>
        <el-table-column label="类型 / 地点" min-width="220"><template #default="{row}"><el-tag size="small" :type="isDirectReceipt(row)?'info':'success'">{{isDirectReceipt(row)?'直接交付':'仓库收货'}}</el-tag><div class="sub destination">{{row.destination}}</div></template></el-table-column>
        <el-table-column label="本次数量" width="120" align="right"><template #default="{row}"><strong>{{trim(row.totalQty)}}</strong></template></el-table-column>
        <el-table-column label="登记人" width="130"><template #default="{row}">{{row.operatorName||'—'}}</template></el-table-column>
        <el-table-column label="备注" min-width="160"><template #default="{row}">{{row.remark||'—'}}</template></el-table-column>
        <template #empty><el-empty description="还没有收货记录"/></template>
      </el-table>
      <el-pagination class="pager" layout="total, prev, pager, next" :total="filteredRows.length" :page-size="pageSize" :current-page="pageNo" @current-change="pageNo=$event"/>
    </el-card>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { get } from '../api'

interface Order {id:string;poNo:string;supplierName:string;fulfillmentMode:string;deliveryLocationType:string;deliveryPortCode:string;deliveryPortName:string;deliveryAddress:string;warehouseName:string;receivedQty:string}
interface Receipt {id:string;receiptNo:string;warehouseId:string;operatorName:string;remark:string;totalQty:string;receivedAt:string}
interface ReceiptRow extends Receipt {poNo:string;supplierName:string;destination:string}
const route=useRoute(),router=useRouter(),loading=ref(false),rows=ref<ReceiptRow[]>([]),pageNo=ref(1),pageSize=20,warehouseNames=ref<Record<string,string>>({})
const keyword=ref(String(route.query.keyword??'')),deliveryType=ref(String(route.query.type??'ALL'))
const filteredRows=computed(()=>{const needle=keyword.value.trim().toLowerCase();return rows.value.filter(row=>(deliveryType.value==='ALL'||(deliveryType.value==='DIRECT'&&isDirectReceipt(row))||(deliveryType.value==='WAREHOUSE'&&!isDirectReceipt(row)))&&(!needle||[row.receiptNo,row.poNo,row.supplierName].join(' ').toLowerCase().includes(needle)))})
const pagedRows=computed(()=>filteredRows.value.slice((pageNo.value-1)*pageSize,(pageNo.value-1)*pageSize+pageSize))
function trim(value:string){const n=Number(value);return Number.isFinite(n)?String(n):value||'0'}
function formatTime(value:string){return value?new Date(value).toLocaleString():'—'}
function isDirectReceipt(receipt:Receipt){return Number(receipt.warehouseId)===0}
function destination(order:Order,receipt:Receipt){if(!isDirectReceipt(receipt))return warehouseNames.value[String(receipt.warehouseId)]||order.warehouseName||`仓库 #${receipt.warehouseId}`;if(order.deliveryLocationType==='PORT')return [order.deliveryPortCode,order.deliveryPortName].filter(Boolean).join(' · ')||'港口';return order.deliveryAddress||'指定地点'}
async function ordersByStatus(status:string){const first=await get<{orders:Order[];meta?:{total?:string|number}}>('/purchase-orders',{status,page:1,page_size:200});const pages=Math.ceil(Number(first.meta?.total??0)/200);if(pages<=1)return first.orders??[];const rest=await Promise.all(Array.from({length:pages-1},(_,index)=>get<{orders:Order[]}>('/purchase-orders',{status,page:index+2,page_size:200})));return [...(first.orders??[]),...rest.flatMap(item=>item.orders??[])]}
async function load(){loading.value=true;try{const [partial,received,warehouseData]=await Promise.all([ordersByStatus('PARTIALLY_RECEIVED'),ordersByStatus('RECEIVED'),get<{warehouses:{id:string;name:string}[]}>('/warehouses',{include_inactive:true})]);warehouseNames.value=Object.fromEntries((warehouseData.warehouses??[]).map(item=>[String(item.id),item.name]));const orders=[...partial,...received].filter(order=>Number(order.receivedQty)>0),detailRows:ReceiptRow[]=[];for(let start=0;start<orders.length;start+=20){const batch=await Promise.all(orders.slice(start,start+20).map(order=>get<{receipts:Receipt[]}>(`/purchase-orders/${order.id}`).then(detail=>(detail.receipts??[]).map(receipt=>({...receipt,poNo:order.poNo,supplierName:order.supplierName,destination:destination(order,receipt)})))));detailRows.push(...batch.flat())}rows.value=detailRows.sort((a,b)=>b.receivedAt.localeCompare(a.receivedAt));pageNo.value=1}finally{loading.value=false}}
async function applyFilters(){pageNo.value=1;await router.replace({query:{...(keyword.value?{keyword:keyword.value}:{}),...(deliveryType.value!=='ALL'?{type:deliveryType.value}:{})}})}
onMounted(load)
</script>

<style scoped>
.page{padding:28px;max-width:1500px;margin:auto}.hero{display:flex;align-items:center;justify-content:space-between;gap:20px;margin-bottom:20px}.hero h1{font-size:30px;margin:4px 0}.hero p,.sub{color:#738095}.eyebrow{font-size:12px!important;letter-spacing:2px;color:#087f78!important;font-weight:700}.filters{margin-bottom:16px}.filters :deep(.el-card__body){display:flex;gap:12px;flex-wrap:wrap}.filters .el-input{width:320px}.filters .el-select{width:160px}.destination{margin-top:5px}.pager{justify-content:flex-end;margin-top:18px}@media(max-width:760px){.page{padding:16px}.hero{align-items:flex-start;flex-direction:column}.filters .el-input,.filters .el-select{width:100%}}
</style>
