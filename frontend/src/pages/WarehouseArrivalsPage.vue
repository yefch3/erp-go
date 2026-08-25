<template>
  <div class="page">
    <header class="hero">
      <div>
        <p class="eyebrow">INBOUND DELIVERY</p>
        <h1>待到货</h1>
        <p>承接已下单采购，直送只登记签收，进入仓库才形成库存。</p>
      </div>
      <el-button @click="router.push('/warehouses')">返回仓库工作台</el-button>
    </header>

    <el-card shadow="never" class="filters">
      <el-input v-model="keyword" clearable placeholder="搜索采购单号或供应商" @keyup.enter="applyFilters" @clear="applyFilters" />
      <el-select v-model="routeType" @change="applyFilters">
        <el-option label="全部交付路线" value="ALL" />
        <el-option label="直接交付" value="DIRECT_SHIP" />
        <el-option label="进入仓库" value="WAREHOUSE" />
      </el-select>
      <el-date-picker v-model="expectedDate" type="date" value-format="YYYY-MM-DD" clearable placeholder="要求到货日期" @change="applyFilters" />
      <el-button type="primary" @click="applyFilters">查询</el-button>
    </el-card>

    <el-card shadow="never">
      <el-table :data="pagedRows" v-loading="loading" row-key="id">
        <el-table-column label="采购单" min-width="155">
          <template #default="{ row }"><strong>{{ row.poNo }}</strong><div class="sub">{{ row.buyerName || '未指定采购员' }}</div></template>
        </el-table-column>
        <el-table-column label="供应商" prop="supplierName" min-width="180" />
        <el-table-column label="要求到货" width="120">
          <template #default="{ row }"><span :class="{ overdue: isOverdue(row.expectedDate) }">{{ row.expectedDate || '—' }}</span></template>
        </el-table-column>
        <el-table-column label="交付路线" min-width="220">
          <template #default="{ row }">
            <el-tag size="small" :type="row.fulfillmentMode === 'WAREHOUSE' ? 'success' : 'info'">{{ routeLabel(row) }}</el-tag>
            <div class="sub destination">{{ destinationLabel(row) }}</div>
          </template>
        </el-table-column>
        <el-table-column label="到货进度" min-width="190">
          <template #default="{ row }">
            <el-progress :percentage="progressOf(row)" :stroke-width="8" />
            <div class="sub">已收 {{ trim(row.receivedQty) }} / {{ trim(row.totalQty) }}</div>
          </template>
        </el-table-column>
        <el-table-column label="状态" width="105">
          <template #default="{ row }"><el-tag :type="row.status === 'PARTIALLY_RECEIVED' ? 'warning' : 'primary'" effect="plain">{{ row.status === 'PARTIALLY_RECEIVED' ? '部分到货' : '待到货' }}</el-tag></template>
        </el-table-column>
        <el-table-column label="操作" width="120" fixed="right">
          <template #default="{ row }"><el-button link type="primary" :disabled="!canReceive" @click="openReceipt(row)">登记收货</el-button></template>
        </el-table-column>
        <template #empty><el-empty description="当前没有待到货采购单" /></template>
      </el-table>
      <el-pagination class="pager" layout="total, prev, pager, next" :total="filteredRows.length" :page-size="pageSize" :current-page="pageNo" @current-change="pageNo = $event" />
    </el-card>

    <el-dialog v-model="receiptOpen" :title="`收货登记 · ${receiving?.poNo || ''}`" width="860px" destroy-on-close>
      <el-alert :type="isDirect ? 'info' : 'success'" :closable="false" show-icon class="receipt-alert">
        {{ isDirect ? '本单为直接交付：确认后只回写采购到货进度，不增加公司库存。' : '本单进入仓库：确认后会向所选实体仓库发送库存入账事件。' }}
      </el-alert>
      <el-descriptions :column="2" border size="small" class="receipt-summary">
        <el-descriptions-item label="供应商">{{ receiving?.supplierName }}</el-descriptions-item>
        <el-descriptions-item label="交付地点">{{ receiving ? destinationLabel(receiving) : '—' }}</el-descriptions-item>
      </el-descriptions>
      <el-form label-width="92px">
        <el-form-item v-if="!isDirect" label="实际收货仓" required>
          <el-select v-model="warehouseId" filterable placeholder="请选择同步库存的启用仓库" style="width: 420px">
            <el-option v-for="warehouse in physicalWarehouses" :key="warehouse.id" :value="Number(warehouse.id)" :label="`${warehouse.code} · ${warehouse.name}`" />
          </el-select>
        </el-form-item>
        <el-form-item label="收货备注"><el-input v-model="remark" type="textarea" :rows="2" placeholder="可填写签收人、单据号或本批说明" /></el-form-item>
      </el-form>
      <el-table :data="receiptItems" size="small" max-height="360">
        <el-table-column label="产品" min-width="220"><template #default="{ row }"><strong>{{ row.productName }}</strong><div class="sub">{{ row.productCode }}<span v-if="row.spec"> · {{ row.spec }}</span></div></template></el-table-column>
        <el-table-column label="订购" width="105" align="right"><template #default="{ row }">{{ trim(row.qty) }} {{ row.uomCode }}</template></el-table-column>
        <el-table-column label="已收" width="100" align="right"><template #default="{ row }">{{ trim(row.receivedQty) }}</template></el-table-column>
        <el-table-column label="本次收货" width="170"><template #default="{ row }"><el-input v-model="receiptQty[row.id]" :disabled="outstandingOf(row) <= 0"><template #append>{{ row.uomCode }}</template></el-input></template></el-table-column>
      </el-table>
      <template #footer>
        <el-button @click="receiptOpen = false">取消</el-button>
        <el-button type="primary" :loading="saving" @click="submitReceipt">确认登记</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { ElMessage } from 'element-plus'
import { useRoute, useRouter } from 'vue-router'
import { get, post } from '../api'
import { useAuthStore } from '../stores/auth'

interface Order { id:string; poNo:string; supplierName:string; buyerName:string; expectedDate:string; status:string; totalQty:string; receivedQty:string; fulfillmentMode:string; deliveryLocationType:string; deliveryPortCode:string; deliveryPortName:string; deliveryAddress:string; warehouseId:string; warehouseName:string }
interface OrderItem { id:string; productCode:string; productName:string; spec:string; uomCode:string; qty:string; receivedQty:string }
interface Warehouse { id:string; code:string; name:string; whType:string; profileType:string; accountingMode:string; status:string }

const route=useRoute(), router=useRouter(), auth=useAuthStore()
const canReceive=computed(()=>auth.can('procurement:receipt:write'))
const loading=ref(false), saving=ref(false), rows=ref<Order[]>([]), pageNo=ref(1), pageSize=20
const keyword=ref(String(route.query.keyword ?? '')), routeType=ref(String(route.query.route ?? 'ALL')), expectedDate=ref(String(route.query.date ?? ''))
const receiptOpen=ref(false), receiving=ref<Order|null>(null), receiptItems=ref<OrderItem[]>([]), warehouses=ref<Warehouse[]>([]), warehouseId=ref(0), remark=ref('')
const receiptQty=reactive<Record<string,string>>({})
const isDirect=computed(()=>receiving.value?.fulfillmentMode !== 'WAREHOUSE')
const physicalWarehouses=computed(()=>warehouses.value.filter(item=>item.status==='ACTIVE'&&item.whType!=='VIRTUAL'&&item.accountingMode==='SYNC_INVENTORY'))
const filteredRows=computed(()=>rows.value.filter(row=>(routeType.value==='ALL'||row.fulfillmentMode===routeType.value)&&(!expectedDate.value||row.expectedDate===expectedDate.value)))
const pagedRows=computed(()=>filteredRows.value.slice((pageNo.value-1)*pageSize,(pageNo.value-1)*pageSize+pageSize))

function trim(value:string){const n=Number(value);return Number.isFinite(n)?String(n):value||'0'}
function progressOf(row:Order){const total=Number(row.totalQty);return total>0?Math.min(100,Math.round(Number(row.receivedQty)*1000/total)/10):0}
function outstandingOf(row:OrderItem){return Math.max(0,Number(row.qty)-Number(row.receivedQty))}
function routeLabel(row:Order){return row.fulfillmentMode==='WAREHOUSE'?'进入仓库':'直接交付'}
function destinationLabel(row:Order){if(row.fulfillmentMode==='WAREHOUSE')return row.warehouseName||'待指定仓库';if(row.deliveryLocationType==='PORT')return [row.deliveryPortCode,row.deliveryPortName].filter(Boolean).join(' · ')||'港口';return row.deliveryAddress||'指定地点'}
function isOverdue(value:string){return Boolean(value&&value<new Date().toISOString().slice(0,10))}

async function ordersByStatus(status:string){const first=await get<{orders:Order[];meta?:{total?:string|number}}>('/purchase-orders',{status,keyword:keyword.value,page:1,page_size:200});const pages=Math.ceil(Number(first.meta?.total??0)/200);if(pages<=1)return first.orders??[];const rest=await Promise.all(Array.from({length:pages-1},(_,index)=>get<{orders:Order[]}>('/purchase-orders',{status,keyword:keyword.value,page:index+2,page_size:200})));return [...(first.orders??[]),...rest.flatMap(item=>item.orders??[])]}
async function load(){loading.value=true;try{const lists=await Promise.all(['ORDERED','PARTIALLY_RECEIVED'].map(ordersByStatus));rows.value=lists.flat().sort((a,b)=>(a.expectedDate||'9999').localeCompare(b.expectedDate||'9999'));pageNo.value=1}finally{loading.value=false}}
async function applyFilters(){await router.replace({query:{...(keyword.value?{keyword:keyword.value}:{}),...(routeType.value!=='ALL'?{route:routeType.value}:{}),...(expectedDate.value?{date:expectedDate.value}:{})}});await load()}
async function openReceipt(row:Order){receiving.value=row;remark.value='';const detail=await get<{items:OrderItem[]}>(`/purchase-orders/${row.id}`);receiptItems.value=detail.items??[];Object.keys(receiptQty).forEach(key=>delete receiptQty[key]);receiptItems.value.forEach(item=>{const remaining=outstandingOf(item);receiptQty[item.id]=remaining>0?String(remaining):''});if(row.fulfillmentMode==='WAREHOUSE'&&!warehouses.value.length){warehouses.value=(await get<{warehouses:Warehouse[]}>('/warehouses')).warehouses??[]}const preferred=physicalWarehouses.value.find(item=>Number(item.id)===Number(row.warehouseId));warehouseId.value=row.fulfillmentMode==='WAREHOUSE'?Number(preferred?.id||physicalWarehouses.value[0]?.id||0):0;receiptOpen.value=true}
async function submitReceipt(){const lines=receiptItems.value.filter(item=>Number(receiptQty[item.id])>0).map(item=>({po_item_id:Number(item.id),qty:receiptQty[item.id]}));if(!lines.length){ElMessage.warning('请至少填写一项本次收货数量');return}for(const item of receiptItems.value){const qty=Number(receiptQty[item.id]||0);if(qty<0||qty>outstandingOf(item)){ElMessage.warning(`“${item.productName}”本次收货不能超过剩余数量`);return}}if(!isDirect.value&&!warehouseId.value){ElMessage.warning('请选择实际收货仓库');return}saving.value=true;try{const result=await post<{receiptNo:string}>(`/purchase-orders/${receiving.value?.id}/receive`,{warehouse_id:isDirect.value?0:warehouseId.value,remark:remark.value,lines});ElMessage.success(`收货登记成功：${result.receiptNo}`);receiptOpen.value=false;await load()}finally{saving.value=false}}
onMounted(load)
</script>

<style scoped>
.page{padding:28px;max-width:1500px;margin:auto}.hero{display:flex;align-items:center;justify-content:space-between;gap:20px;margin-bottom:20px}.hero h1{font-size:30px;margin:4px 0}.hero p,.sub{color:#738095}.eyebrow{font-size:12px!important;letter-spacing:2px;color:#087f78!important;font-weight:700}.filters{margin-bottom:16px}.filters :deep(.el-card__body){display:flex;gap:12px;flex-wrap:wrap}.filters .el-input{width:280px}.filters .el-select{width:170px}.destination{margin-top:5px}.overdue{color:#d14343;font-weight:600}.pager{justify-content:flex-end;margin-top:18px}.receipt-alert,.receipt-summary{margin-bottom:18px}@media(max-width:760px){.page{padding:16px}.hero{align-items:flex-start;flex-direction:column}.filters .el-input,.filters .el-select{width:100%}}
</style>
