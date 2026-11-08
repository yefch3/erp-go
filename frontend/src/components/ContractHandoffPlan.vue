<template>
 <section v-loading="loading" class="handoff-plan">
  <div class="plan-heading"><div><strong>交货分批与备注</strong><p>按客户要求分配每批交货数量，并填写批次说明；财务放行后交给采购和物流执行。</p></div><div v-if="canEdit"><el-button @click="addBatch">添加批次</el-button><el-button type="primary" :disabled="!balanced" @click="save">保存分批</el-button></div></div>
  <el-alert v-if="error" :title="error" type="error" :closable="false"/>
  <el-alert v-if="!plan.revision" title="尚未保存分批：默认整份合同一次交货。需要分批时，请填写各批数量和批次说明后保存。" type="info" :closable="false"/>
  <div class="batch-names"><div v-for="(batch,index) in plan.batches" :key="batch.batchNo"><span>第 {{batch.batchNo}} 批</span><el-input v-model="batch.title" :disabled="!canEdit" placeholder="批次说明"/><el-button v-if="canEdit&&plan.batches.length>1" link type="danger" @click="plan.batches.splice(index,1)">移除</el-button></div></div>
  <el-table :data="items" border size="small" max-height="380" row-key="id">
   <el-table-column prop="productName" label="产品" min-width="180" fixed show-overflow-tooltip/>
   <el-table-column prop="spec" label="规格" min-width="170" show-overflow-tooltip/>
   <el-table-column label="合同数量" width="120" align="right"><template #default="{row}">{{row.qty}} {{row.uomCode}}</template></el-table-column>
   <el-table-column v-for="batch in plan.batches" :key="batch.batchNo" :label="`第 ${batch.batchNo} 批`" width="140"><template #default="{row}"><el-input v-model="batch.quantities[row.id]" :disabled="!canEdit" inputmode="decimal" aria-label="批次数量"/></template></el-table-column>
   <el-table-column label="未分配" width="105" align="right"><template #default="{row}"><span :class="{mismatch:remaining(row)!=='0'}">{{remaining(row)}}</span></template></el-table-column>
   <el-table-column prop="remark" label="交接备注" min-width="200" show-overflow-tooltip/>
  </el-table>
 </section>
</template>
<script setup lang="ts">
import {computed,ref,watch} from 'vue'
import {ElMessage} from 'element-plus'
import {get,post} from '../api'
type Item={id:string;productName:string;spec:string;qty:string;uomCode:string;remark?:string}
type Batch={batchNo:number;title:string;quantities:Record<string,string>}
type Plan={versionId:string;revision:number;batches:Batch[]}
const props=defineProps<{contractId:string;versionId:string;items:Item[];canEdit:boolean}>()
const plan=ref<Plan>({versionId:props.versionId,revision:0,batches:[]}),loading=ref(false),error=ref('')
function scaled(raw:string){if(!/^\d+(\.\d{1,4})?$/.test(raw))return null;const [a,b='']=raw.split('.');return BigInt(a)*10000n+BigInt(b.padEnd(4,'0'))}
function remaining(row:Item){const total=scaled(row.qty);if(total===null)return '无效';let used=0n;for(const b of plan.value.batches){const q=scaled(b.quantities[row.id]||'0');if(q===null)return '无效';used+=q}const n=total-used,sign=n<0n?'-':'',v=n<0n?-n:n;return sign+`${v/10000n}.${String(v%10000n).padStart(4,'0')}`.replace(/\.?0+$/,'')}
const balanced=computed(()=>plan.value.batches.length>0&&props.items.every(i=>remaining(i)==='0'))
function addBatch(){plan.value.batches.push({batchNo:Math.max(0,...plan.value.batches.map(b=>b.batchNo))+1,title:'',quantities:Object.fromEntries(props.items.map(i=>[i.id,'0']))})}
async function load(){loading.value=true;error.value='';try{plan.value=await get<Plan>(`/contracts/${props.contractId}/workflow`,{view:'handoff'});if(!plan.value.batches.length){addBatch();for(const i of props.items)plan.value.batches[0].quantities[i.id]=i.qty}}catch(e){error.value=(e as Error).message}finally{loading.value=false}}
async function save(){loading.value=true;error.value='';try{plan.value=await post<Plan>(`/contracts/${props.contractId}/workflow`,{action:'handoff_save',reason:'维护运输分批交接资料',data:plan.value});ElMessage.success('运输分批已保存，财务放行后按各批数量交接')}catch(e){error.value=(e as Error).message}finally{loading.value=false}}
watch(()=>[props.contractId,props.versionId],load,{immediate:true})
</script>
<style scoped>
.handoff-plan{margin:12px 0 20px}.plan-heading{display:flex;justify-content:space-between;align-items:center;gap:12px}.plan-heading p{font-size:12px;color:#64748b}.batch-names{display:flex;flex-wrap:wrap;gap:12px;margin:12px 0}.batch-names>div{display:flex;align-items:center;gap:8px}.batch-names .el-input{width:180px}.batch-names span{white-space:nowrap}.mismatch{color:#b45309;font-weight:600}
</style>
