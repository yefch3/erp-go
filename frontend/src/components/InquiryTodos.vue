<template>
  <section v-if="rows.length || error || mode==='handled'" class="inquiry-todos">
    <div class="section-head">
      <div><h3>{{ t(mode==='handled'?'todos.inquiryHandledTitle':'todos.inquiryTitle') }}</h3><p>{{ t(mode==='handled'?'todos.inquiryHandledHint':'todos.inquiryHint') }}</p></div>
      <el-tag type="primary" effect="plain">{{ displayedRows.length }} {{ t('todos.items') }}</el-tag>
    </div>
    <div class="filters"><el-input v-model="keyword" clearable :placeholder="t('todos.inquirySearch')"/><el-select v-model="taskFilter" clearable :placeholder="t('todos.inquiryAllTypes')"><el-option v-for="task in taskOptions" :key="task.key" :label="task.label" :value="task.key"/></el-select></div>
    <el-alert v-if="error" :title="error" type="warning" :closable="false" />
    <el-table :data="displayedRows" class="inquiry-todo-table" :empty-text="t('todos.inquiryEmpty')">
      <el-table-column :label="t('todos.workItem')" min-width="320">
        <template #default="{row}">
          <strong class="item-title">{{ t(row.taskKey) }}</strong>
          <div class="item-meta">{{ row.number }} · {{ row.customer || t('todos.customerUnset') }}</div>
        </template>
      </el-table-column>
      <el-table-column v-if="mode==='handled'" :label="t('todos.completedAt')" width="175"><template #default="{row}">{{displayTime(row.completedAt)}}</template></el-table-column>
      <el-table-column :label="t('common.status')" width="110"><template #default><el-tag size="small" :type="mode==='handled'?'success':'warning'" effect="light">{{mode==='handled'?t('todos.completed'):t('todos.waitingForMe')}}</el-tag></template></el-table-column>
      <el-table-column :label="t('common.actions')" width="110" fixed="right"><template #default="{row}"><router-link class="action-link" :to="{path:row.path,query:{id:row.id}}">{{mode==='handled'?t('todos.view'):t('todos.goToSource')}}</router-link></template></el-table-column>
    </el-table>
  </section>
</template>
<script setup lang="ts">
import {computed,onMounted,onUnmounted,ref} from 'vue'
import {useI18n} from 'vue-i18n'
import {get,post} from '../api'
import {onLive} from '../live'
import {useAuthStore} from '../stores/auth'
import type {Result} from '../lib/inquiryWorkspace'
const emit=defineEmits<{count:[value:number]}>()
const props=withDefaults(defineProps<{mode?:'pending'|'handled'}>(),{mode:'pending'})
const mode=computed(()=>props.mode)
type InquiryTodoRow={id:string;number:string;customer:string;taskKey:string;path:string;completedAt:string}
const auth=useAuthStore(),{t,locale}=useI18n(),rows=ref<InquiryTodoRow[]>([]),error=ref(''),keyword=ref(''),taskFilter=ref('')
const taskOptions=computed(()=>[...new Set(rows.value.map(row=>row.taskKey))].map(key=>({key,label:t(key)})))
const displayedRows=computed(()=>{const needle=keyword.value.trim().toLowerCase();return rows.value.filter(row=>(!taskFilter.value||row.taskKey===taskFilter.value)&&(!needle||`${row.number} ${row.customer} ${t(row.taskKey)}`.toLowerCase().includes(needle)))})
function displayTime(value:string){if(!value)return '—';const date=new Date(value);return Number.isNaN(date.getTime())?value:date.toLocaleString(locale.value,{hour12:false})}
let refreshing=false
async function refresh(){
 if(refreshing)return;refreshing=true
 const out:InquiryTodoRow[]=[]
 try{
 const offerStates=auth.can('export:quotation:read')?(await post<{summaries:Record<string,{status:string;confirmedAt:string}>}>('/customer-offer',{action:'summaries'})).summaries:{}
 const pending=[['PROCUREMENT','procurement:sourcing:read','/procurement/sourcing','todos.inquiryTasks.procurementWaiting','WAITING'],['LOGISTICS','shipping:sourcing:read','/shipping/sourcing','todos.inquiryTasks.logisticsWaiting','WAITING'],['SALES','sales:inquiry:read','/sales/inquiries','todos.inquiryTasks.salesUnsubmitted','UNSUBMITTED'],['SALES','sales:inquiry:read','/sales/inquiries','todos.inquiryTasks.salesWithdrawn','WITHDRAWN'],['QUOTATIONS','sales:inquiry:read','/sales/quotations','todos.inquiryTasks.quotationPending','']]
 const handled=[['QUOTATIONS','export:quotation:read','/sales/quotations','todos.inquiryTasks.quotationConfirmed',''],['PROCUREMENT','procurement:sourcing:read','/procurement/sourcing','todos.inquiryTasks.procurementQuoted','QUOTED'],['LOGISTICS','shipping:sourcing:read','/shipping/sourcing','todos.inquiryTasks.logisticsQuoted','QUOTED'],['SALES','sales:inquiry:read','/sales/inquiries','todos.inquiryTasks.salesSubmitted','INQUIRING']]
 for(const [view,permission,path,taskKey,state] of (mode.value==='handled'?handled:pending)){
 if(!auth.can(permission))continue
 let page=1,total=0;do{const r=await post<Result>('/inquiry-workspace',{action:'list',view,state,page,size:100});total=r.total;for(const i of r.items){if((view==='SALES'||view==='QUOTATIONS')&&!auth.owns(i.ownerId))continue;if(view==='QUOTATIONS'&&(mode.value==='handled'?offerStates[i.id]?.status!=='CONFIRMED':offerStates[i.id]?.status==='CONFIRMED'))continue;out.push({id:i.id,number:i.number,customer:i.body.customer,taskKey,path,completedAt:view==='QUOTATIONS'?offerStates[i.id]?.confirmedAt||'':i.submittedAt})}page++}while((page-1)*100<total)
 }
 if(mode.value==='pending'&&auth.can('export:contract:write')){
  for(const status of ['DRAFT','PENDING_SIGN']){let contractPage=1,contractTotal=0;do{const r=await get<{contracts:{id:string;contractNo:string;customerName:string;salesEmployeeId:string}[];meta:{total:number}}>('/contracts',{status,page:contractPage,page_size:100});contractTotal=Number(r.meta?.total||0);for(const c of r.contracts||[]){if(auth.owns(c.salesEmployeeId))out.push({id:c.id,number:c.contractNo,customer:c.customerName,taskKey:status==='DRAFT'?'todos.inquiryTasks.contractDraft':'todos.inquiryTasks.contractSigning',path:'/contracts',completedAt:''})}contractPage++}while((contractPage-1)*100<contractTotal)}
 }
 rows.value=out;emit('count',out.length);error.value=''
 }catch{error.value=t('todos.inquiryUnavailable')}finally{refreshing=false}
}
const stopLive=onLive(e=>{if(e.type==='requirement.changed')void refresh()})
let timer:ReturnType<typeof setInterval>|undefined
onMounted(()=>{void refresh();timer=setInterval(()=>void refresh(),5000)})
onUnmounted(()=>{stopLive();if(timer)clearInterval(timer)})
</script>
<style scoped>
.inquiry-todos { margin-bottom: 26px; }
.section-head { display: flex; align-items: center; justify-content: space-between; gap: 16px; margin-bottom: 12px; }
.section-head h3 { margin: 0; color: #172b4d; font-size: 16px; }
.section-head p { margin: 5px 0 0; color: #718096; font-size: 13px; }
.filters { display:flex; gap:10px; margin-bottom:12px; }.filters .el-input { max-width:320px; }.filters .el-select { width:210px; }
.item-title { color: #24364b; font-size: 14px; }
.item-meta { margin-top: 5px; color: #718096; font-size: 12px; }
.action-link { color: #1677ff; font-weight: 600; text-decoration: none; }
.inquiry-todo-table { border: 1px solid #e5eaf1; border-radius: 10px; overflow: hidden; }
@media(max-width:720px){.filters{flex-direction:column}.filters .el-input,.filters .el-select{width:100%;max-width:none}}
</style>
