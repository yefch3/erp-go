<template>
 <el-dialog :model-value="open" :title="copy.title" width="min(760px,94vw)" top="6vh" append-to-body :close-on-click-modal="false" :close-on-press-escape="!saving" :show-close="!saving" class="mail-customer-recognition" @update:model-value="emit('update:open',$event)">
  <div ref="contentEl">
  <p class="recognition-help">{{copy.help}}</p>
  <el-form label-position="top" :disabled="saving" class="recognition-fields">
   <el-form-item :label="copy.email" required class="full"><el-input v-model="form.email" maxlength="200"/></el-form-item>
   <el-form-item :label="copy.company" required><el-input v-model="form.companyName" :placeholder="copy.companyPlaceholder" maxlength="200"/></el-form-item>
   <el-form-item :label="copy.person"><el-input v-model="form.name" :placeholder="copy.optional" maxlength="100"/></el-form-item>
  </el-form>
  <section class="recognition-results" aria-live="polite" :aria-busy="matching">
   <div class="result-heading"><strong>{{copy.result}}</strong><el-button link :disabled="saving||matching" @click="lookup">{{copy.retry}}</el-button></div>
   <p v-if="matching">{{copy.checking}}</p>
   <el-alert v-if="error" :title="error" type="error" show-icon :closable="false"/>
   <template v-if="!matching && checked">
    <el-alert v-if="resolved.conflict||resolved.ambiguous" :title="resolved.conflict?copy.conflict:copy.ambiguous" type="warning" :closable="false" show-icon/>
    <div v-for="c in activeCompanies" :key="c.id" :class="['company-match',{selected:resolved.company?.id===c.id}]">
     <div><strong>{{c.name}}</strong><small>{{c.code}}</small><small v-for="contact in (c.contacts??[]).filter(v=>mailContactMatches(v,form.email))" :key="contact.id">{{contact.name||copy.nameless}} · {{form.email}}</small></div>
     <el-button :disabled="saving" :type="resolved.company?.id===c.id?'primary':undefined" plain size="small" @click="selectCompany(c)">{{resolved.company?.id===c.id?copy.selected:copy.choose}}</el-button>
    </div>
    <p v-if="resolved.company">{{action==='SUPPLEMENT'?copy.supplementTip:emailContacts.length?copy.existing:copy.addHint}}</p>
    <p v-else-if="!activeCompanies.length">{{copy.noMatch}}</p>
    <el-checkbox v-if="!resolved.company && activeCompanies.length && !resolved.owners.length && !activeCompanies.some(c=>c.exactName)" v-model="confirmNew" :disabled="saving">{{copy.confirmNew}}</el-checkbox>
    <template v-if="emailContacts.length>1"><p>{{copy.ambiguous}}</p><el-radio-group v-model="contactChoice"><el-radio v-for="c in emailContacts" :key="c.id" :value="c.id">{{c.name||copy.nameless}} · {{c.email}}</el-radio></el-radio-group></template>
    <template v-if="!emailContacts.length && sameNames.length">
     <p>{{copy.sameName}}</p><el-radio-group v-model="contactChoice" class="contact-choices" :disabled="saving">
      <el-radio v-for="c in sameNames" :key="c.id" :value="c.id">{{copy.supplement}} — {{c.name}} <small>{{copy.retained}}: {{[c.email,...(c.additionalEmails??[])].filter(Boolean).join(' / ')}}</small></el-radio>
      <el-radio value="new">{{copy.newPerson}}</el-radio>
     </el-radio-group>
    </template>
    <el-alert v-if="inactiveMatches" :title="copy.stopped" type="info" :closable="false" show-icon/>
    <div v-if="suppliers.length" class="supplier-matches"><strong>{{copy.suppliers}}</strong><p v-for="s in suppliers" :key="s.id">{{s.code}} · {{s.name}}{{s.status!=='ACTIVE'?' ('+copy.stopped+')':''}}</p><el-checkbox v-if="action==='CREATE' && suppliers.some(s=>s.status==='ACTIVE')" v-model="confirmSupplier" :disabled="saving">{{copy.supplierConfirm}}</el-checkbox></div>
   </template>
  </section>
  <el-collapse v-model="expanded" class="recognition-more"><el-collapse-item name="details" :title="copy.more">
   <el-form label-position="top" class="recognition-fields" :disabled="saving||action==='USE'||action==='SUPPLEMENT'">
    <el-form-item :label="copy.phone"><el-input v-model="form.phone" maxlength="50"/></el-form-item>
    <el-form-item :label="copy.website"><el-input v-model="form.website" maxlength="300" :disabled="action!=='CREATE'"/></el-form-item>
    <el-form-item :label="copy.address" class="full"><el-input v-model="form.address" maxlength="500" :disabled="action!=='CREATE'"/></el-form-item>
    <el-form-item :label="copy.remark" class="full"><el-input v-model="form.remark" type="textarea" :rows="2" maxlength="2000"/></el-form-item>
   </el-form>
  </el-collapse-item></el-collapse>
  </div>
  <template #footer><el-button :disabled="saving" @click="emit('update:open',false)">{{copy.close}}</el-button><el-tooltip :content="actionTip" :show-after="250"><span><el-button type="primary" :loading="saving" :disabled="!canSave" @click="save">{{actionLabel}}</el-button></span></el-tooltip></template>
 </el-dialog>
</template>
<script setup lang="ts">
import {computed,reactive,ref,watch,onBeforeUnmount,onMounted,nextTick} from 'vue'
import {useI18n} from 'vue-i18n'
import {ElMessage} from 'element-plus'
import {get,post,quietErrors} from '../api'
import {newIdempotencySession,withIdempotency} from '../lib/idempotency'
import type {MailCustomerDraft} from '../lib/mailCustomerDraft'
import {mailCustomerCopy} from '../lib/mailCustomerCopy'
import {resolveMailCustomer,mailContactMatches,normalizedMailValue,type MailCompanyMatch,type MailCustomerLink} from '../lib/mailCustomerRecognition'
const props=defineProps<{open:boolean;draft:MailCustomerDraft|null;inboundId:string}>()
const emit=defineEmits<{'update:open':[boolean];created:[MailCustomerLink]}>()
const {locale}=useI18n()
const copy=computed(()=>mailCustomerCopy[locale.value.startsWith('zh')?'zh':locale.value.startsWith('es')?'es':'en'])
const form=reactive({companyName:'',name:'',email:'',phone:'',website:'',address:'',remark:''})
const companies=ref<MailCompanyMatch[]>([]),suppliers=ref<MailCompanyMatch[]>([]),selectedId=ref(''),contactChoice=ref(''),confirmNew=ref(false),confirmSupplier=ref(false)
const matching=ref(false),saving=ref(false),checked=ref(false),error=ref(''),expanded=ref<string[]>([])
const contentEl=ref<HTMLElement|null>(null)
let scrollObserver:ResizeObserver|undefined
async function fitScrollArea(){
 await nextTick()
 scrollObserver?.disconnect()
 const content=contentEl.value,body=content?.parentElement
 if(!content||!body)return
 const fit=()=>{body.style.overflowY=content.scrollHeight>body.clientHeight+1?'auto':'hidden'}
 scrollObserver=new ResizeObserver(fit);scrollObserver.observe(content);scrollObserver.observe(body);fit()
}
const idem=newIdempotencySession()
let sequence=0,timer:ReturnType<typeof setTimeout>|undefined,initializing=false
const activeCompanies=computed(()=>companies.value.filter(c=>c.status==='ACTIVE'))
const resolved=computed(()=>resolveMailCustomer(companies.value,form.email,form.companyName,selectedId.value))
const emailContacts=computed(()=>resolved.value.contacts.filter(c=>mailContactMatches(c,form.email)))
const sameNames=computed(()=>resolved.value.contacts.filter(c=>form.name.trim()&&normalizedMailValue(c.name)===normalizedMailValue(form.name)))
const inactiveMatches=computed(()=>companies.value.some(c=>c.status!=='ACTIVE'||(c.contacts??[]).some(ct=>ct.status!=='ACTIVE'&&[ct.email,...(ct.additionalEmails??[])].some(e=>normalizedMailValue(e)===normalizedMailValue(form.email)))))
const chosenContact=computed(()=>emailContacts.value.length===1?emailContacts.value[0]:resolved.value.contacts.find(c=>c.id===contactChoice.value))
const action=computed(()=>!resolved.value.company?'CREATE':emailContacts.value.length?'USE':sameNames.value.some(c=>c.id===contactChoice.value)?'SUPPLEMENT':'ADD')
const actionLabel=computed(()=>({CREATE:copy.value.create,ADD:copy.value.add,USE:copy.value.use,SUPPLEMENT:copy.value.addEmail})[action.value])
const actionTip=computed(()=>({CREATE:copy.value.createTip,ADD:copy.value.addTip,USE:copy.value.useTip,SUPPLEMENT:copy.value.supplementTip})[action.value])
const canSave=computed(()=>{
 if(saving.value||matching.value||!checked.value||resolved.value.conflict||resolved.value.ambiguous)return false
 if(!form.companyName.trim()||! /^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(form.email.trim()))return false
 if(action.value==='CREATE')return !resolved.value.owners.length&&(!activeCompanies.value.length||confirmNew.value)&&(!suppliers.value.some(s=>s.status==='ACTIVE')||confirmSupplier.value)
 if(action.value==='USE')return Boolean(chosenContact.value)
 return !sameNames.value.length||Boolean(contactChoice.value)
})
function failure(e:unknown){error.value=e&&typeof e==='object'&&'message' in e?String(e.message):copy.value.failed}
async function lookup(){
 const seq=++sequence;checked.value=false;matching.value=true;error.value=''
 const name=form.companyName,email=form.email
 try{
  const v=await get<{customers?:MailCompanyMatch[];suppliers?:MailCompanyMatch[]}>(`/inbound-mails/${props.inboundId}/customer-matches`,{company_name:name,email},quietErrors)
  if(seq!==sequence||!props.open)return
  companies.value=v.customers??[];suppliers.value=v.suppliers??[];checked.value=true
  if(selectedId.value&&!activeCompanies.value.some(c=>c.id===selectedId.value))selectedId.value=''
  if(!name.trim()&&resolved.value.company){selectCompany(resolved.value.company)}
 }catch(e){if(seq===sequence){failure(e);checked.value=false}}finally{if(seq===sequence)matching.value=false}
}
function selectCompany(c:MailCompanyMatch){
 if(saving.value)return
 selectedId.value=c.id;contactChoice.value='';confirmNew.value=false
 initializing=true;form.companyName=c.name;initializing=false
 const owner=(c.contacts??[]).filter(ct=>mailContactMatches(ct,form.email))
 if(owner.length===1&&!form.name.trim())form.name=owner[0].name
}
watch([()=>form.companyName,()=>form.email],()=>{
 if(!props.open||initializing)return
 selectedId.value='';contactChoice.value='';confirmNew.value=false;confirmSupplier.value=false;checked.value=false;++sequence
 clearTimeout(timer);timer=setTimeout(()=>void lookup(),350)
},{flush:'sync'})
watch(()=>form.name,()=>{contactChoice.value=''})
watch(()=>props.open,async open=>{
 clearTimeout(timer);++sequence
 if(!open){matching.value=false;scrollObserver?.disconnect();return}
 initializing=true
 Object.assign(form,{companyName:props.draft?.companyName??'',name:props.draft?.name??'',email:props.draft?.email??'',phone:props.draft?.phone??'',website:props.draft?.website??'',address:props.draft?.address??'',remark:''})
 initializing=false;companies.value=[];suppliers.value=[];selectedId.value='';contactChoice.value='';confirmNew.value=false;confirmSupplier.value=false;error.value='';expanded.value=[];idem.reset()
 await fitScrollArea()
 await lookup()
})
function refreshOnFocus(){if(props.open&&!saving.value)void lookup()}
onMounted(()=>window.addEventListener('focus',refreshOnFocus))
onBeforeUnmount(()=>{scrollObserver?.disconnect();clearTimeout(timer);++sequence;window.removeEventListener('focus',refreshOnFocus)})
async function save(){
 if(!canSave.value)return
 saving.value=true;error.value=''
 const inboundId=props.inboundId
 try{
  const result=await post<MailCustomerLink>(`/inbound-mails/${props.inboundId}/customer-link`,{action:action.value,customerId:resolved.value.company?.id??'0',contactId:chosenContact.value?.id??'0',companyName:form.companyName.trim(),contactName:form.name.trim(),email:form.email.trim(),phone:form.phone.trim(),website:form.website.trim(),address:form.address.trim(),remark:form.remark,confirmSameName:contactChoice.value==='new',confirmSupplier:confirmSupplier.value},withIdempotency(idem,quietErrors))
  idem.reset();emit('created',{...result,inboundId});emit('update:open',false);ElMessage.success(copy.value.saved)
 }catch(e){failure(e);checked.value=false}finally{saving.value=false}
}
</script>
<style scoped>
.recognition-help{margin:0 0 20px;color:var(--el-text-color-secondary);line-height:1.7;white-space:normal}
.recognition-fields{display:grid;grid-template-columns:minmax(0,1fr) minmax(0,1fr);gap:0 20px}.full{grid-column:1/-1}
.recognition-results{border:1px solid var(--el-border-color-light);background:var(--el-fill-color-extra-light);border-radius:10px;padding:14px 16px;margin:0 0 18px}.recognition-results p{line-height:1.65;margin:10px 0}.result-heading{display:flex;justify-content:space-between;align-items:center;margin-bottom:8px}
.company-match{display:flex;justify-content:space-between;align-items:center;gap:12px;background:var(--el-bg-color);border:1px solid var(--el-border-color-light);padding:12px;border-radius:8px;margin:10px 0}.company-match.selected{border-color:var(--el-color-primary)}.company-match small{display:block;color:var(--el-text-color-secondary);margin-top:4px;overflow-wrap:anywhere}.company-match>div{min-width:0}
.contact-choices{display:flex;flex-direction:column;align-items:stretch}.contact-choices :deep(.el-radio){height:auto;margin:8px 0}.contact-choices :deep(.el-radio__label){white-space:normal;line-height:1.7}.contact-choices small{display:block}.supplier-matches{margin-top:14px;padding-top:12px;border-top:1px solid var(--el-border-color-light)}
.recognition-results :deep(.el-checkbox){height:auto;white-space:normal}.recognition-results :deep(.el-checkbox__label){white-space:normal;line-height:1.6}.recognition-more{margin-top:4px}
:global(.mail-customer-recognition){display:flex;flex-direction:column;max-height:88vh;margin-bottom:0;box-sizing:border-box}:global(.mail-customer-recognition .el-dialog__header){margin:0;padding-bottom:18px}:global(.mail-customer-recognition .el-dialog__body){min-height:0;overflow-y:auto;padding:0 6px 0 0}:global(.mail-customer-recognition .el-dialog__footer){flex-shrink:0;padding-top:18px}:global(.mail-customer-recognition .el-dialog__footer>span){display:inline-block;margin-left:12px}
@media(max-width:560px){.recognition-fields{grid-template-columns:1fr}.company-match{align-items:flex-start;flex-direction:column}}
</style>
