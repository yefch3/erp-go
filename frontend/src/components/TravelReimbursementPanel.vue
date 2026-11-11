<template>
  <section class="claim-panel">
    <div class="toolbar">
      <el-button type="primary" @click="openCreate">新建报销</el-button>
    </div>
    <el-table v-loading="loading" :data="rows" row-key="id">
      <el-table-column type="expand"><template #default="{row}"><div class="details">
        <div class="detail-grid"><span>出差：{{row.tripStart}} 至 {{row.tripEnd}}</span><span>路线：{{row.origin}} → {{row.destination}}</span><span>事由：{{row.purpose}}</span><span>收款账户：{{row.paymentAccount}}</span></div>
        <el-alert v-if="row.rejectionReason" type="error" :closable="false" :title="`退回原因：${row.rejectionReason}`" />
        <h4>报销凭证</h4><div class="files"><a v-for="f in row.files" :key="f.id" href="#" @click.prevent="openFile(f)">{{fileType[f.category]||f.category}} · {{f.fileName}}</a><span v-if="!row.files?.length">暂无凭证</span></div>
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

  <el-dialog v-model="formOpen" :title="editing?'编辑出差报销':'新建出差报销'" width="min(720px,95vw)" :close-on-click-modal="false" :close-on-press-escape="!saving" :show-close="!saving">
    <el-form label-position="top"><div class="grid">
      <el-form-item label="出差开始" required><el-date-picker v-model="form.tripStart" type="date" value-format="YYYY-MM-DD" style="width:100%" /></el-form-item>
      <el-form-item label="出差结束" required><el-date-picker v-model="form.tripEnd" type="date" value-format="YYYY-MM-DD" style="width:100%" /></el-form-item>
      <el-form-item label="出发地" required><el-input v-model="form.origin" /></el-form-item><el-form-item label="目的地" required><el-input v-model="form.destination" /></el-form-item>
    </div><el-form-item label="出差事由" required><el-input v-model="form.purpose" /></el-form-item><div class="grid">
      <el-form-item label="报销金额" required><el-input v-model="form.amount"><template #prepend><el-select v-model="form.currency" style="width:90px"><el-option v-for="c in currencies" :key="c" :value="c" /></el-select></template></el-input></el-form-item>
      <el-form-item label="收款账户" required><el-input v-model="form.paymentAccount" placeholder="银行卡或公司付款账户说明" /></el-form-item>
    </div><el-form-item label="备注"><el-input v-model="form.note" type="textarea" :rows="2" /></el-form-item>
    <el-form-item label="报销凭证（发票、收据、行程单等；提交审批前至少一份）"><div class="claim-files">
      <div v-if="editing?.files?.length" class="uploaded-files"><span>已上传 {{editing.files.length}} 份：</span><a v-for="f in editing.files" :key="f.id" href="#" @click.prevent="openFile(f)">{{fileType[f.category]||f.category}} · {{f.fileName}}</a></div>
      <PendingUploadList v-model="picked" :categories="fileType" default-category="INVOICE" :taken-names="uploadedNames" :disabled="saving" />
    </div></el-form-item></el-form>
    <template #footer>
      <el-button :disabled="saving" @click="formOpen=false">取消</el-button>
      <el-button :loading="saving" @click="save(false)">保存草稿</el-button>
      <el-button v-if="!editing || ['DRAFT','REJECTED'].includes(editing.status)" type="primary" :loading="saving" @click="save(true)">保存并提交审批</el-button>
    </template>
  </el-dialog>
  <el-dialog v-model="payOpen" title="登记报销付款" width="min(480px,94vw)"><el-form label-position="top"><el-form-item label="付款日期" required><el-date-picker v-model="pay.paidAt" type="date" value-format="YYYY-MM-DD" style="width:100%" /></el-form-item><el-form-item label="付款账户" required><el-input v-model="pay.paymentAccount" /></el-form-item><el-form-item label="付款流水号"><el-input v-model="pay.paymentReference" /></el-form-item></el-form><template #footer><el-button @click="payOpen=false">取消</el-button><el-button type="primary" :loading="saving" @click="savePay">确认付款</el-button></template></el-dialog>
</template>
<script setup lang="ts">
import {computed,onMounted,reactive,ref} from 'vue'
import {ElMessage,ElMessageBox} from 'element-plus'
import {get,patch,post,quietErrors} from '../api'
import {useAuthStore} from '../stores/auth'
import {getActionableApproval} from '../lib/approvalAction'
import {relayFailureReason,relayTimedOut,saveViaRelay,uploadViaRelay} from '../lib/storageRelay'
import {dropRegistered,uploadPending,type PendingUpload} from '../lib/pendingUploads'
import PendingUploadList from './PendingUploadList.vue'
type Claim=Record<string,any>
const auth=useAuthStore(),rows=ref<Claim[]>([]),approvalTasks=ref<Record<string,string>>({}),approvalOverrides=ref<Record<string,boolean>>({}),loading=ref(false),saving=ref(false),formOpen=ref(false),payOpen=ref(false),editing=ref<Claim|null>(null),paying=ref<Claim|null>(null),picked=ref<PendingUpload[]>([])
const myID=computed(()=>String(auth.employeeId||localStorage.getItem('employeeId')||'')),canManage=auth.can('procurement:reimbursement:manage'),currencies=['CNY','USD','EUR','GBP','JPY','HKD','CAD','AUD']
const form=reactive({tripStart:'',tripEnd:'',origin:'',destination:'',purpose:'',amount:'',currency:'CNY',paymentAccount:'',note:''}),pay=reactive({paidAt:new Date().toISOString().slice(0,10),paymentAccount:'',paymentReference:''})
const statusLabel:Record<string,string>={DRAFT:'草稿',PENDING_DEPARTMENT_CONFIRMATION:'待部门负责人确认',PENDING_FINANCE_APPROVAL:'待财务负责人审批',REJECTED:'已退回',PENDING_PAYMENT:'待付款',PAID:'已付款'}
const fileType:Record<string,string>={INVOICE:'发票',RECEIPT:'收据',ITINERARY:'行程单',PAYMENT_PROOF:'付款凭证',OTHER:'其他'}
const statusType=(s:string):'success'|'danger'|'warning'|'info'|undefined=>s==='PAID'?'success':s==='REJECTED'?'danger':s==='PENDING_PAYMENT'?'warning':s==='DRAFT'?'info':undefined
async function load(){loading.value=true;try{const claims=await get<{items:Claim[]}>('/travel-reimbursements');rows.value=claims.items||[];approvalTasks.value={};approvalOverrides.value={};const pending=rows.value.filter(row=>['PENDING_DEPARTMENT_CONFIRMATION','PENDING_FINANCE_APPROVAL'].includes(row.status));const resolved=await Promise.all(pending.map(async row=>[String(row.id),await getActionableApproval('TRAVEL_REIMBURSEMENT',row.id)] as const));for(const [id,action] of resolved){if(action){approvalTasks.value[id]=action.taskId;approvalOverrides.value[id]=action.override}}}finally{loading.value=false}}
const taskFor=(row:Claim)=>approvalTasks.value[String(row.id)]||''
const overrideFor=(row:Claim)=>Boolean(approvalOverrides.value[String(row.id)])
const delay=(ms:number)=>new Promise(resolve=>setTimeout(resolve,ms))
async function reloadUntilStatusChanges(id:string,previousStatus:string){for(let attempt=0;attempt<8;attempt++){await load();if(rows.value.find(item=>String(item.id)===String(id))?.status!==previousStatus)return;await delay(300)}}
async function act(row:Claim,action:'APPROVE'|'RETURN'){let comment='';if(!overrideFor(row)&&action==='RETURN'){const answer=await ElMessageBox.prompt('请填写退回原因','退回报销').catch(()=>({value:''}));comment=answer.value;if(!comment)return}await post(`/approvals/tasks/${taskFor(row)}/act`,{action,comment});ElMessage.success(action==='APPROVE'?'已确认':'已退回申请人');await reloadUntilStatusChanges(row.id,row.status)}
function openCreate(){editing.value=null;picked.value=[];Object.assign(form,{tripStart:'',tripEnd:'',origin:'',destination:'',purpose:'',amount:'',currency:'CNY',paymentAccount:'',note:''});formOpen.value=true}
function openEdit(row:Claim){editing.value=row;picked.value=[];Object.assign(form,{tripStart:row.tripStart,tripEnd:row.tripEnd,origin:row.origin,destination:row.destination,purpose:row.purpose,amount:row.amount,currency:row.currency,paymentAccount:row.paymentAccount,note:row.note});formOpen.value=true}
// 一趟差常有好几张发票（#591）：待传的文件是一张清单（PendingUploadList），一份一行、各选各的类型，
// 每次「添加文件」是往后加。原来是一个选择框配一个类型，再选一次就把上一次的换掉。
//
// 交的顺序和规矩在 lib/pendingUploads.ts 的 uploadPending（那里有测试）：一份一份经网关中转进存储
// （国内直连存储会连接超时，见 lib/storageRelay.ts）；送文件失败只记在那一份上、接着传下一份；
// 要地址或登记失败、或者等到超时，就整批停下。交上去的从清单里拿掉，清单里剩下的就是没交上去的，
// 再点一次保存只补它们——已经进了存储的不重传，已经登记的不重复登记（登记了的凭证删不掉）。
// 三步都 quiet：失败写在那一行上，不让通用 toast 再说一遍。
// 这里只把三步对上报销的接口。claim 是刚保存完拿回来的那张单子，上面的 files 就是「已登记的」。
async function uploadFiles(claim:Claim){picked.value=await uploadPending(picked.value,{registeredKeys:new Set<string>((claim.files||[]).map((f:Claim)=>String(f.objectKey||''))),presign:async item=>{const signed=await post<{objectKey:string;uploadUrl:string}>(`/travel-reimbursements/${claim.id}/files/presign`,{file_name:item.file.name,category:item.category},quietErrors);return{key:signed.objectKey,uploadUrl:signed.uploadUrl}},send:(uploadUrl,file)=>uploadViaRelay(uploadUrl,file),register:async(item,key)=>{const attached=await post<{reimbursement:Claim}>(`/travel-reimbursements/${claim.id}/files`,{object_key:key,file_name:item.file.name,category:item.category},quietErrors);
// 登记的回答里带着整张单子：对话框里的「已上传 N 份」跟着每一份往上走。
if(attached?.reimbursement)editing.value=attached.reimbursement},reason:uploadFailureReason,timedOut:relayTimedOut},list=>{picked.value=list})}
// 失败那一行上写给人看的话。服务端说了原因就用它的；没等到回音和断网是浏览器这头的事，axios 给的是
// 英文原话（timeout of 15000ms exceeded / Network Error），换成人话。超时那句不说「失败」：很可能已经
// 成功了，下一次保存会自己核对（见 uploadPending 里的 registeredKeys）。
function uploadFailureReason(err:unknown){if(relayTimedOut(err))return '等了太久没有回音（网络慢）；再点一次保存，会从断的地方接着来';if((err as {code?:unknown}|null)?.code==='ERR_NETWORK')return '网络连接不上，请检查网络后再点保存';return relayFailureReason(err)||'请稍后再试'}
// 已经交过的文件名：清单里再出现同名的，提醒一句（不拦——两家酒店的发票都叫「发票.pdf」是正常的）。
const uploadedNames=computed<string[]>(()=>(editing.value?.files||[]).map((f:Claim)=>String(f.fileName)))
async function openFile(f:Claim){try{await saveViaRelay(f.url,f.fileName)}catch(err){ElMessage.error(`凭证打开失败：${relayFailureReason(err)||'请稍后再试'}`)}}
async function save(submitAfterSave=false){if(!form.tripStart||!form.tripEnd||!form.origin.trim()||!form.destination.trim()||!form.purpose.trim()||!form.amount.trim()||!form.paymentAccount.trim()){ElMessage.warning('请填写所有必填项');return}saving.value=true;try{const result=editing.value?await patch<{reimbursement:Claim}>(`/travel-reimbursements/${editing.value.id}`,form):await post<{reimbursement:Claim}>('/travel-reimbursements',form);
// 单子已经落库：从这里起再点保存就是改这一张，不是再建一张。
editing.value=result.reimbursement;
// 有没传上去的就不关对话框、也不提交：单子留着（草稿），清单里剩下的就是没传上去的，标红的那一行
// 写着原因。以前这里什么都不说，员工只能一遍遍点。保存期间对话框关不掉（取消、Esc、右上角都停用）：
// 一批文件要传一阵子，中途关掉再开一张，传完的这一头会把新开的那张关掉。
await uploadFiles(result.reimbursement)
// 清单里还有东西：先以服务器上的单子为准对一遍——登记那一步没等到回音的，多半其实已经记上了。
if(picked.value.length){await load();const fresh=rows.value.find(row=>String(row.id)===String(result.reimbursement.id));if(fresh){editing.value=fresh;picked.value=dropRegistered(picked.value,new Set<string>((fresh.files||[]).map((f:Claim)=>String(f.objectKey||''))))}}
if(picked.value.length){const failed=picked.value.some(item=>item.status==='failed');ElMessage.error({message:`还有 ${picked.value.length} 个文件没传上去${failed?'，原因写在标红的那一行':'（网络太慢，中途停下了）'}。报销单已保存为草稿${submitAfterSave?'，还没有提交审批':''}；再点一次保存，会接着传剩下的。`,duration:8000});return}
if(submitAfterSave){await post(`/travel-reimbursements/${result.reimbursement.id}/submit`,{});ElMessage.success('报销已提交，等待部门负责人确认')}else{ElMessage.success('报销草稿已保存')}formOpen.value=false;await load()}finally{saving.value=false}}
async function submit(row:Claim){await post(`/travel-reimbursements/${row.id}/submit`,{});ElMessage.success('已提交，等待部门负责人确认');await load()}
function openPay(row:Claim){paying.value=row;Object.assign(pay,{paidAt:new Date().toISOString().slice(0,10),paymentAccount:row.paymentAccount,paymentReference:''});payOpen.value=true}
async function savePay(){if(!paying.value||!pay.paidAt||!pay.paymentAccount.trim()){ElMessage.warning('请填写付款日期和账户');return}saving.value=true;try{await post(`/travel-reimbursements/${paying.value.id}/pay`,pay);payOpen.value=false;ElMessage.success('付款已登记');await load()}finally{saving.value=false}}
async function reverse(row:Claim){const {value}=await ElMessageBox.prompt('请输入冲销原因','冲销报销付款').catch(()=>({value:''}));if(!value)return;await post(`/travel-reimbursements/${row.id}/payment/reverse`,{reason:value});ElMessage.success('付款已冲销');await load()}
onMounted(load)
</script>
<style scoped>
.claim-panel{border:1px solid #dceaf0;border-radius:12px;background:#fff;padding:16px}.toolbar{display:flex;justify-content:flex-end;gap:12px;margin-bottom:14px}.details{padding:4px 24px 18px}.detail-grid{display:grid;grid-template-columns:repeat(2,minmax(0,1fr));gap:10px 24px;margin-bottom:14px}.files{display:flex;gap:10px;flex-wrap:wrap}.files a{color:#159fdc}.details h4{margin:18px 0 8px}.sub{font-size:12px;color:#80909f}.grid{display:grid;grid-template-columns:1fr 1fr;gap:16px}.claim-files{display:grid;gap:8px;width:100%}.uploaded-files{display:flex;flex-wrap:wrap;gap:2px 12px;color:#52646f;font-size:13px;line-height:1.7}.uploaded-files a{color:#159fdc}.module-tabs{margin-bottom:14px}@media(max-width:700px){.grid,.detail-grid{grid-template-columns:1fr}.toolbar{align-items:flex-start;flex-direction:column}}
</style>
