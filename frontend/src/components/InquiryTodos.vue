<template>
  <section v-if="rows.length || error || mode==='handled'" class="inquiry-todos">
    <div class="section-head">
      <div><h3>{{mode==='handled'?'询盘与报价处理记录':'询盘与报价'}}</h3><p>{{mode==='handled'?'查看当前 D1 询盘与报价流程中已经完成的业务动作。':'需要你处理的客户询盘、采购报价和物流报价。'}}</p></div>
      <el-tag type="primary" effect="plain">{{ displayedRows.length }} 项</el-tag>
    </div>
    <div class="filters"><el-input v-model="keyword" clearable placeholder="搜索编号、客户或事项"/><el-select v-model="taskFilter" clearable placeholder="全部询盘类型"><el-option v-for="task in taskOptions" :key="task" :label="task" :value="task"/></el-select></div>
    <el-alert v-if="error" :title="error" type="warning" :closable="false" />
    <el-table :data="displayedRows" class="inquiry-todo-table" empty-text="暂无符合条件的询盘记录">
      <el-table-column label="工作事项" min-width="320">
        <template #default="{row}">
          <strong class="item-title">{{ row.task }}</strong>
          <div class="item-meta">{{ row.number }} · {{ row.customer || '未填写客户' }}</div>
        </template>
      </el-table-column>
      <el-table-column v-if="mode==='handled'" label="完成时间" width="175"><template #default="{row}">{{displayTime(row.completedAt)}}</template></el-table-column>
      <el-table-column label="状态" width="110"><template #default><el-tag size="small" :type="mode==='handled'?'success':'warning'" effect="light">{{mode==='handled'?'已完成':'待我处理'}}</el-tag></template></el-table-column>
      <el-table-column label="操作" width="110" fixed="right"><template #default="{row}"><router-link class="action-link" :to="{path:row.path,query:{id:row.id}}">{{mode==='handled'?'查看':'去处理'}}</router-link></template></el-table-column>
    </el-table>
  </section>
</template>
<script setup lang="ts">
import {computed,onMounted,onUnmounted,ref} from 'vue'
import {post} from '../api'
import {onLive} from '../live'
import {useAuthStore} from '../stores/auth'
import type {Result} from '../lib/inquiryWorkspace'
const emit=defineEmits<{count:[value:number]}>()
const props=withDefaults(defineProps<{mode?:'pending'|'handled'}>(),{mode:'pending'})
const mode=computed(()=>props.mode)
type InquiryTodoRow={id:string;number:string;customer:string;task:string;path:string;completedAt:string}
const auth=useAuthStore(),rows=ref<InquiryTodoRow[]>([]),error=ref(''),keyword=ref(''),taskFilter=ref('')
const taskOptions=computed(()=>[...new Set(rows.value.map(row=>row.task))])
const displayedRows=computed(()=>{const needle=keyword.value.trim().toLowerCase();return rows.value.filter(row=>(!taskFilter.value||row.task===taskFilter.value)&&(!needle||`${row.number} ${row.customer} ${row.task}`.toLowerCase().includes(needle)))})
function displayTime(value:string){if(!value)return '—';const date=new Date(value);return Number.isNaN(date.getTime())?value:date.toLocaleString('zh-CN',{hour12:false})}
async function refresh(){
 const out:InquiryTodoRow[]=[]
 try{
 const pending=[['PROCUREMENT','procurement:sourcing:read','/procurement/sourcing','新询盘待工厂报价','WAITING'],['LOGISTICS','shipping:sourcing:read','/shipping/sourcing','新询盘待货代报价','WAITING'],['SALES','sales:inquiry:read','/sales/inquiries','客户询盘待提交','UNSUBMITTED'],['SALES','sales:inquiry:read','/sales/inquiries','客户询盘待重新提交','WITHDRAWN'],['QUOTATIONS','sales:inquiry:read','/sales/quotations','客户报价待处理','']]
 const handled=[['PROCUREMENT','procurement:sourcing:read','/procurement/sourcing','工厂报价已提交','QUOTED'],['LOGISTICS','shipping:sourcing:read','/shipping/sourcing','货代报价已提交','QUOTED'],['SALES','sales:inquiry:read','/sales/inquiries','客户询盘已提交','INQUIRING']]
 for(const [view,permission,path,task,state] of (mode.value==='handled'?handled:pending)){
 if(!auth.can(permission))continue
 let page=1,total=0;do{const r=await post<Result>('/inquiry-workspace',{action:'list',view,state,page,size:100});total=r.total;for(const i of r.items){if((view==='SALES'||view==='QUOTATIONS')&&!auth.owns(i.ownerId))continue;if(view==='QUOTATIONS'&&i.procurementCount+i.logisticsCount===0)continue;out.push({id:i.id,number:i.number,customer:i.body.customer,task,path,completedAt:i.submittedAt})}page++}while((page-1)*100<total)
 }
 rows.value=out;emit('count',out.length);error.value=''
 }catch{error.value='询盘待办暂时无法读取，请刷新重试'}
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
