<template>
  <section class="claim-panel">
    <div class="toolbar">
      <el-button type="primary" @click="openCreate">新建报销</el-button>
    </div>
    <el-table v-loading="loading" :data="rows" row-key="id">
      <el-table-column type="expand"><template #default="{row}"><div class="details">
        <div class="detail-grid"><span>出差：{{row.tripStart}} 至 {{row.tripEnd}}</span><span>路线：{{row.origin}} → {{row.destination}}</span><span>事由：{{row.purpose}}</span><span>收款账户：{{row.paymentAccount}}</span></div>
        <el-alert v-if="row.rejectionReason" type="error" :closable="false" :title="`退回原因：${row.rejectionReason}`" />
        <h4>报销凭证</h4><div class="files"><a v-for="f in row.files" :key="f.id" :href="f.url" target="_blank" rel="noopener">{{fileType[f.category]||f.category}} · {{f.fileName}}</a><span v-if="!row.files?.length">暂无凭证</span></div>
      </div></template></el-table-column>
      <el-table-column prop="claimNo" label="报销单号" min-width="180" />
      <el-table-column prop="claimantName" label="报销人" width="110" /><el-table-column prop="departmentName" label="部门" width="130" />
      <el-table-column label="出差事由" min-width="200"><template #default="{row}">{{row.purpose}}<div class="sub">{{row.destination}}</div></template></el-table-column>
      <el-table-column label="金额" width="150" align="right"><template #default="{row}"><strong>{{row.currency}} {{row.amount}}</strong></template></el-table-column>
      <el-table-column label="状态" width="150"><template #default="{row}"><el-tag :type="statusType(row.status)" effect="plain">{{statusLabel[row.status]||row.status}}</el-tag></template></el-table-column>
      <el-table-column label="操作" width="230" fixed="right"><template #default="{row}">
        <el-button v-if="['DRAFT','REJECTED','PENDING_PAYMENT'].includes(row.status) && row.claimantId===myID" link @click="openEdit(row)">编辑</el-button>
        <el-button v-if="['DRAFT','REJECTED'].includes(row.status) && row.claimantId===myID" link type="primary" @click="submit(row)">提交审批</el-button>
        <el-button v-if="taskFor(row)" link type="success" @click="act(row,'APPROVE')">同意</el-button>
        <el-button v-if="taskFor(row)" link type="danger" @click="act(row,'RETURN')">退回</el-button>
        <el-button v-if="canManage && row.status==='PENDING_PAYMENT'" link type="success" @click="openPay(row)">登记付款</el-button>
        <el-button v-if="canManage && row.status==='PAID'" link type="danger" @click="reverse(row)">冲销付款</el-button>
      </template></el-table-column>
      <template #empty>暂无出差报销</template>
    </el-table>
  </section>

  <el-dialog v-model="formOpen" :title="editing?'编辑出差报销':'新建出差报销'" width="min(720px,95vw)" :close-on-click-modal="false">
    <el-form label-position="top"><div class="grid">
      <el-form-item label="出差开始" required><el-date-picker v-model="form.tripStart" type="date" value-format="YYYY-MM-DD" style="width:100%" /></el-form-item>
      <el-form-item label="出差结束" required><el-date-picker v-model="form.tripEnd" type="date" value-format="YYYY-MM-DD" style="width:100%" /></el-form-item>
      <el-form-item label="出发地" required><el-input v-model="form.origin" /></el-form-item><el-form-item label="目的地" required><el-input v-model="form.destination" /></el-form-item>
    </div><el-form-item label="出差事由" required><el-input v-model="form.purpose" /></el-form-item><div class="grid">
      <el-form-item label="报销金额" required><el-input v-model="form.amount"><template #prepend><el-select v-model="form.currency" style="width:90px"><el-option v-for="c in currencies" :key="c" :value="c" /></el-select></template></el-input></el-form-item>
      <el-form-item label="收款账户" required><el-input v-model="form.paymentAccount" placeholder="银行卡或公司付款账户说明" /></el-form-item>
    </div><el-form-item label="备注"><el-input v-model="form.note" type="textarea" :rows="2" /></el-form-item>
    <el-form-item label="报销凭证（发票、收据、行程单等；提交审批前至少一份）"><div class="upload-row"><el-select v-model="fileCategory" style="width:130px"><el-option v-for="(label,key) in fileType" :key="key" :value="key" :label="label" /></el-select><input type="file" multiple @change="pickFiles" /></div><div v-if="picked.length" class="sub">已选择 {{picked.length}} 个文件</div></el-form-item></el-form>
    <template #footer>
      <el-button @click="formOpen=false">取消</el-button>
      <el-button :loading="saving" @click="save(false)">保存草稿</el-button>
      <el-button v-if="!editing || ['DRAFT','REJECTED'].includes(editing.status)" type="primary" :loading="saving" @click="save(true)">保存并提交审批</el-button>
    </template>
  </el-dialog>
  <el-dialog v-model="payOpen" title="登记报销付款" width="min(480px,94vw)"><el-form label-position="top"><el-form-item label="付款日期" required><el-date-picker v-model="pay.paidAt" type="date" value-format="YYYY-MM-DD" style="width:100%" /></el-form-item><el-form-item label="付款账户" required><el-input v-model="pay.paymentAccount" /></el-form-item><el-form-item label="付款流水号"><el-input v-model="pay.paymentReference" /></el-form-item></el-form><template #footer><el-button @click="payOpen=false">取消</el-button><el-button type="primary" :loading="saving" @click="savePay">确认付款</el-button></template></el-dialog>
</template>
<script setup lang="ts">
import {computed,onMounted,reactive,ref} from 'vue'
import {ElMessage,ElMessageBox} from 'element-plus'
import {get,patch,post} from '../api'
import {useAuthStore} from '../stores/auth'
type Claim=Record<string,any>
const auth=useAuthStore(),rows=ref<Claim[]>([]),approvalTasks=ref<Record<string,string>>({}),loading=ref(false),saving=ref(false),formOpen=ref(false),payOpen=ref(false),editing=ref<Claim|null>(null),paying=ref<Claim|null>(null),picked=ref<File[]>([]),fileCategory=ref('INVOICE')
const myID=computed(()=>String(auth.employeeId||localStorage.getItem('employeeId')||'')),canManage=auth.can('procurement:reimbursement:manage'),currencies=['CNY','USD','EUR','GBP','JPY','HKD','CAD','AUD']
const form=reactive({tripStart:'',tripEnd:'',origin:'',destination:'',purpose:'',amount:'',currency:'CNY',paymentAccount:'',note:''}),pay=reactive({paidAt:new Date().toISOString().slice(0,10),paymentAccount:'',paymentReference:''})
const statusLabel:Record<string,string>={DRAFT:'草稿',PENDING_DEPARTMENT_CONFIRMATION:'待部门负责人确认',PENDING_FINANCE_APPROVAL:'待财务负责人审批',REJECTED:'已退回',PENDING_PAYMENT:'待付款',PAID:'已付款'}
const fileType:Record<string,string>={INVOICE:'发票',RECEIPT:'收据',ITINERARY:'行程单',PAYMENT_PROOF:'付款凭证',OTHER:'其他'}
const statusType=(s:string):'success'|'danger'|'warning'|'info'|undefined=>s==='PAID'?'success':s==='REJECTED'?'danger':s==='PENDING_PAYMENT'?'warning':s==='DRAFT'?'info':undefined
async function load(){loading.value=true;try{const [claims,todos]=await Promise.all([get<{items:Claim[]}>('/travel-reimbursements'),get<{todos:{task:{id:string},instance:{bizId:string}}[]}>('/approvals/todos',{biz_type:'TRAVEL_REIMBURSEMENT',page:1,page_size:200})]);rows.value=claims.items||[];approvalTasks.value=Object.fromEntries((todos.todos||[]).map(x=>[String(x.instance.bizId),String(x.task.id)]))}finally{loading.value=false}}
const taskFor=(row:Claim)=>approvalTasks.value[String(row.id)]||''
const delay=(ms:number)=>new Promise(resolve=>setTimeout(resolve,ms))
async function reloadUntilStatusChanges(id:string,previousStatus:string){for(let attempt=0;attempt<8;attempt++){await load();if(rows.value.find(item=>String(item.id)===String(id))?.status!==previousStatus)return;await delay(300)}}
async function act(row:Claim,action:'APPROVE'|'RETURN'){let comment='';if(action==='RETURN'){const answer=await ElMessageBox.prompt('请填写退回原因','退回报销').catch(()=>({value:''}));comment=answer.value;if(!comment)return}await post(`/approvals/tasks/${taskFor(row)}/act`,{action,comment});ElMessage.success(action==='APPROVE'?'已确认':'已退回申请人');await reloadUntilStatusChanges(row.id,row.status)}
function openCreate(){editing.value=null;picked.value=[];Object.assign(form,{tripStart:'',tripEnd:'',origin:'',destination:'',purpose:'',amount:'',currency:'CNY',paymentAccount:'',note:''});formOpen.value=true}
function openEdit(row:Claim){editing.value=row;picked.value=[];Object.assign(form,{tripStart:row.tripStart,tripEnd:row.tripEnd,origin:row.origin,destination:row.destination,purpose:row.purpose,amount:row.amount,currency:row.currency,paymentAccount:row.paymentAccount,note:row.note});formOpen.value=true}
function pickFiles(e:Event){picked.value=Array.from((e.target as HTMLInputElement).files||[])}
async function uploadFiles(id:string){for(const file of picked.value){const signed=await post<{objectKey:string;uploadUrl:string}>(`/travel-reimbursements/${id}/files/presign`,{file_name:file.name,category:fileCategory.value});const result=await fetch(signed.uploadUrl,{method:'PUT',body:file,headers:{'Content-Type':file.type||'application/octet-stream'}});if(!result.ok)throw new Error('凭证上传失败');await post(`/travel-reimbursements/${id}/files`,{object_key:signed.objectKey,file_name:file.name,category:fileCategory.value})}}
async function save(submitAfterSave=false){if(!form.tripStart||!form.tripEnd||!form.origin.trim()||!form.destination.trim()||!form.purpose.trim()||!form.amount.trim()||!form.paymentAccount.trim()){ElMessage.warning('请填写所有必填项');return}saving.value=true;try{const result=editing.value?await patch<{reimbursement:Claim}>(`/travel-reimbursements/${editing.value.id}`,form):await post<{reimbursement:Claim}>('/travel-reimbursements',form);await uploadFiles(result.reimbursement.id);if(submitAfterSave){await post(`/travel-reimbursements/${result.reimbursement.id}/submit`,{});ElMessage.success('报销已提交，等待部门负责人确认')}else{ElMessage.success('报销草稿已保存')}formOpen.value=false;await load()}finally{saving.value=false}}
async function submit(row:Claim){await post(`/travel-reimbursements/${row.id}/submit`,{});ElMessage.success('已提交，等待部门负责人确认');await load()}
function openPay(row:Claim){paying.value=row;Object.assign(pay,{paidAt:new Date().toISOString().slice(0,10),paymentAccount:row.paymentAccount,paymentReference:''});payOpen.value=true}
async function savePay(){if(!paying.value||!pay.paidAt||!pay.paymentAccount.trim()){ElMessage.warning('请填写付款日期和账户');return}saving.value=true;try{await post(`/travel-reimbursements/${paying.value.id}/pay`,pay);payOpen.value=false;ElMessage.success('付款已登记');await load()}finally{saving.value=false}}
async function reverse(row:Claim){const {value}=await ElMessageBox.prompt('请输入冲销原因','冲销报销付款').catch(()=>({value:''}));if(!value)return;await post(`/travel-reimbursements/${row.id}/payment/reverse`,{reason:value});ElMessage.success('付款已冲销');await load()}
onMounted(load)
</script>
<style scoped>
.claim-panel{border:1px solid #dceaf0;border-radius:12px;background:#fff;padding:16px}.toolbar{display:flex;justify-content:flex-end;gap:12px;margin-bottom:14px}.details{padding:4px 24px 18px}.detail-grid{display:grid;grid-template-columns:repeat(2,minmax(0,1fr));gap:10px 24px;margin-bottom:14px}.files{display:flex;gap:10px;flex-wrap:wrap}.files a{color:#159fdc}.details h4{margin:18px 0 8px}.sub{font-size:12px;color:#80909f}.grid{display:grid;grid-template-columns:1fr 1fr;gap:16px}.upload-row{display:flex;gap:12px;align-items:center}.module-tabs{margin-bottom:14px}@media(max-width:700px){.grid,.detail-grid{grid-template-columns:1fr}.toolbar{align-items:flex-start;flex-direction:column}}
</style>
