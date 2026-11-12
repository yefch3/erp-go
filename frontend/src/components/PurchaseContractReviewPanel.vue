<template>
  <section class="review-panel">
    <div class="review-heading"><div><h2>待合同核验</h2><p>查看采购提交的双方签署合同，核验后进入后续履约与应付流程。核验不会自动登记付款。</p></div><el-button :loading="loading" @click="load">刷新</el-button></div>
    <el-alert v-if="!canReview" title="当前账号没有采购合同财务核验权限" type="warning" :closable="false"/>
    <template v-else>
      <div class="review-filters"><el-input v-model="keyword" placeholder="搜索采购单、外销合同、供应商或文件名" clearable @input="page=1"/><span>{{filtered.length}} 项待核验</span></div>
      <el-alert v-if="error" :title="error" type="error" :closable="false"/>
      <el-table v-else v-loading="loading" :data="paged" empty-text="暂无待核验采购合同；已核验的合同不会重复显示。">
        <el-table-column label="采购单 / 外销合同" min-width="170"><template #default="{row}"><strong>{{row.poNo}}</strong><div class="sub">外销：{{row.exportContractNo||'—'}}</div></template></el-table-column>
        <el-table-column prop="supplierName" label="供应商" min-width="140" show-overflow-tooltip/>
        <el-table-column label="采购金额" width="130" align="right"><template #default="{row}">{{row.currency}} {{money(row.totalAmount)}}</template></el-table-column>
        <el-table-column prop="signedContractName" label="签署合同文件" min-width="180" show-overflow-tooltip/>
        <el-table-column label="上传时间" width="145"><template #default="{row}">{{formatTime(row.signedContractUploadedAt)}}</template></el-table-column>
        <el-table-column label="状态" width="110"><template #default><el-tag type="warning">待财务核验</el-tag></template></el-table-column>
        <el-table-column label="操作" width="120" fixed="right"><template #default="{row}"><el-button type="primary" plain size="small" @click="select(row.id)">查看并核验</el-button></template></el-table-column>
      </el-table>
      <el-pagination v-if="filtered.length>pageSize" v-model:current-page="page" :page-size="pageSize" :total="filtered.length" layout="total,prev,pager,next"/>
    </template>
    <el-dialog v-model="detailOpen" title="采购合同财务核验" width="min(920px, 96vw)" :close-on-click-modal="!verifying" :show-close="!verifying" :close-on-press-escape="!verifying">
      <div v-loading="detailLoading">
        <el-alert v-if="detailError" :title="detailError" type="error" :closable="false"/>
        <template v-else-if="order">
          <el-alert :title="order.contractVerifiedAt?'已完成核验，已从待核验列表移除。付款仍需另行登记。':'请核对采购内容、付款条件和双方签署合同附件后确认核验。'" :type="order.contractVerifiedAt?'success':'info'" :closable="false" show-icon/>
          <el-descriptions :column="2" border size="small" class="review-details">
            <el-descriptions-item label="采购单号">{{order.poNo}}</el-descriptions-item><el-descriptions-item label="关联外销合同">{{order.exportContractNo||'—'}}</el-descriptions-item>
            <el-descriptions-item label="供应商">{{order.supplierName}}</el-descriptions-item><el-descriptions-item label="采购金额">{{order.currency}} {{money(order.totalAmount)}}</el-descriptions-item>
            <el-descriptions-item label="付款条件" :span="2">{{order.paymentTerms||'—'}}</el-descriptions-item>
            <el-descriptions-item label="签署合同" :span="2"><a v-if="order.signedContractUrl" :href="order.signedContractUrl" target="_blank" rel="noopener noreferrer">{{order.signedContractName}} · 查看附件</a><span v-else>{{order.signedContractName||'未上传'}}（附件链接不可用，请刷新后重试）</span></el-descriptions-item>
            <el-descriptions-item label="核验状态">{{order.contractVerifiedAt?'已财务核验':reviewable?'待财务核验':'当前不可核验'}}</el-descriptions-item><el-descriptions-item label="上传时间">{{formatTime(order.signedContractUploadedAt)}}</el-descriptions-item>
            <template v-if="order.contractVerifiedAt"><el-descriptions-item label="核验人">{{order.contractVerifiedByName||'—'}}</el-descriptions-item><el-descriptions-item label="核验时间">{{formatTime(order.contractVerifiedAt)}}</el-descriptions-item></template>
          </el-descriptions>
          <el-table :data="items" size="small"><el-table-column prop="productName" label="商品" min-width="190"/><el-table-column prop="spec" label="规格" min-width="140"/><el-table-column label="数量" width="120" align="right"><template #default="{row}">{{row.qty}} {{row.uomCode}}</template></el-table-column><el-table-column prop="unitPrice" label="单价" width="120" align="right"/><el-table-column prop="amount" label="金额" width="130" align="right"/></el-table>
        </template>
      </div>
      <template #footer><el-button :disabled="verifying" @click="detailOpen=false">关闭</el-button><el-button v-if="reviewable" type="success" :loading="verifying" :disabled="!order?.signedContractUrl" @click="verify">确认核验</el-button></template>
    </el-dialog>
  </section>
</template>
<script setup lang="ts">
import {computed,ref,watch} from 'vue'
import {useRoute,useRouter} from 'vue-router'
import {ElMessage,ElMessageBox} from 'element-plus'
import {get,post,quietErrors} from '../api'
import {useAuthStore} from '../stores/auth'
type Task={id:string;poNo:string;supplierName:string;exportContractNo:string;currency:string;totalAmount:string;signedContractName:string;signedContractUploadedAt:string}
type Order=Task&{businessType:string;sourceBusinessId:string;status:string;closedAt:string;paymentTerms:string;signedContractUrl:string;contractVerifiedAt:string;contractVerifiedByName:string}
type Item={id:string;productName:string;spec:string;qty:string;uomCode:string;unitPrice:string;amount:string}
const props=defineProps<{orderId?:string;initialKeyword?:string}>(),emit=defineEmits<{verified:[]}>()
const auth=useAuthStore(),route=useRoute(),router=useRouter()
const canReview=computed(()=>auth.can('procurement:recon:write')&&auth.can('procurement:order:read'))
const rows=ref<Task[]>([]),loading=ref(false),error=ref(''),keyword=ref(props.initialKeyword||''),page=ref(1),pageSize=100
const filtered=computed(()=>rows.value.filter(row=>!keyword.value.trim()||[row.poNo,row.exportContractNo,row.supplierName,row.signedContractName].some(value=>String(value||'').toLocaleLowerCase().includes(keyword.value.trim().toLocaleLowerCase()))))
const paged=computed(()=>filtered.value.slice((page.value-1)*pageSize,page.value*pageSize))
const detailOpen=ref(false),detailLoading=ref(false),detailError=ref(''),verifying=ref(false),order=ref<Order|null>(null),items=ref<Item[]>([])
const reviewable=computed(()=>canReview.value&&order.value?.businessType==='PROCUREMENT'&&Number(order.value.sourceBusinessId)>0&&order.value.status==='ORDERED'&&!order.value.closedAt&&!!order.value.signedContractUploadedAt&&!order.value.contractVerifiedAt)
function money(value:string){const n=Number(value);return Number.isFinite(n)?n.toLocaleString('en-US',{minimumFractionDigits:2,maximumFractionDigits:2}):value}
function formatTime(value:string){if(!value)return '—';const date=new Date(value.replace(' ','T').replace(/([+-]\d{2})$/,'$1:00'));return Number.isNaN(date.getTime())?value:date.toLocaleString()}
async function load(){if(!canReview.value)return;loading.value=true;error.value='';try{const data=await get<{purchaseFinance:Task[]}>('/home/execution-todos',{},quietErrors);rows.value=data.purchaseFinance??[];page.value=Math.min(page.value,Math.max(1,Math.ceil(filtered.value.length/pageSize)))}catch{rows.value=[];error.value='采购合同核验列表加载失败，请点击刷新重试。'}finally{loading.value=false}}
async function open(id:string){if(!canReview.value)return;detailOpen.value=true;detailLoading.value=true;detailError.value='';order.value=null;items.value=[];try{const data=await get<{order:Order;items:Item[]}>(`/purchase-orders/${id}`,{},quietErrors);if(id!==props.orderId)return;if(data.order.businessType!=='PROCUREMENT'||Number(data.order.sourceBusinessId)<=0){detailError.value='该单据不属于采购合同核验，请核对待办入口。';return}order.value=data.order;items.value=data.items??[]}catch{detailError.value='采购合同详情加载失败，请核对单据权限后重新打开。'}finally{detailLoading.value=false}}
function select(id:string){if(props.orderId===id){void open(id);return}void router.push({path:'/supplier-recon',query:{...route.query,view:'contracts',order:id}})}
async function verify(){if(!reviewable.value||!order.value||verifying.value)return;const id=order.value.id;try{await ElMessageBox.confirm(`确认“${order.value.signedContractName}”是当前供应商双方签署的采购合同？本次核验不会登记付款。`,'核验采购合同',{type:'warning'})}catch{return}verifying.value=true;try{await post(`/purchase-orders/${id}/contract/verify`,{});ElMessage.success('采购合同已核验');await Promise.all([load(),open(id)]);emit('verified')}finally{verifying.value=false}}
watch(()=>props.initialKeyword,value=>{keyword.value=value||'';page.value=1})
watch(()=>props.orderId,id=>{if(id)void open(id);else detailOpen.value=false},{immediate:true})
watch(canReview,()=>{void load()},{immediate:true})
</script>
<style scoped>
.review-panel{padding:18px 20px;border:1px solid #dce8ef;border-radius:10px;background:#fff}.review-heading{display:flex;justify-content:space-between;align-items:center;gap:14px}.review-heading h2{margin:0;font-size:18px}.review-heading p,.sub{color:#75818e;font-size:12px}.review-filters{display:flex;align-items:center;gap:14px;margin:14px 0}.review-filters .el-input{max-width:380px}.review-details{margin:16px 0}.review-panel :deep(.el-table :is(th,td).el-table__cell > .cell){text-align:left!important}.review-panel :deep(.el-table :is(th,td).el-table__cell.is-right > .cell){text-align:right!important}.el-pagination{margin-top:12px;justify-content:flex-end}
</style>
