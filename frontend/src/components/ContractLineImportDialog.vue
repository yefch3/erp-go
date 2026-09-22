<template>
 <el-dialog v-model="visible" title="导入客户成交明细" width="min(900px,96vw)" append-to-body :close-on-click-modal="false">
  <el-alert title="首行为列名。产品名称、数量、单位、对客单价必填，规格可留空；金额自动计算。导入后追加到当前明细，可继续修改。" :closable="false" type="info"/>
  <div class="import-tools"><el-button type="primary" :loading="loading" @click="input?.click()">选择 Excel 文件</el-button><el-button @click="downloadTemplate">下载模板</el-button><input ref="input" type="file" accept=".xlsx,.xls" hidden @change="readFile"/><span>{{fileName||'支持 .xlsx、.xls，最大 5 MB、1000 条明细'}}</span></div>
  <el-select v-if="sheets.length>1" v-model="selected" placeholder="选择工作表"><el-option v-for="(s,i) in sheets" :key="i" :value="i" :label="s.name"/></el-select>
  <div v-if="result.errors.length" class="import-errors" role="alert"><strong>请修正 Excel 后重新选择文件，本次不会导入任何行。</strong><div v-for="e in result.errors.slice(0,30)" :key="e">{{e}}</div><div v-if="result.errors.length>30">另有 {{result.errors.length-30}} 项错误</div></div>
  <template v-if="result.items.length"><p>共 {{result.items.length}} 条，预览前 20 条</p><el-table :data="result.items.slice(0,20)" max-height="320" border><el-table-column prop="productName" label="产品名称" min-width="160"/><el-table-column prop="spec" label="规格"/><el-table-column prop="qty" label="数量"/><el-table-column prop="uomCode" label="单位"/><el-table-column prop="unitPrice" label="对客单价"/></el-table></template>
  <template #footer><el-button @click="visible=false">取消</el-button><el-button type="primary" :disabled="loading||!result.items.length||!!result.errors.length" @click="apply">确认追加 {{result.items.length||''}} 条</el-button></template>
 </el-dialog>
</template>
<script setup lang="ts">
import {computed,ref,watch} from 'vue'
import {ElMessage} from 'element-plus'
import {contractImportHeaders,parseContractRows,type ContractImportLine} from '../lib/contractLineImport'
const props=defineProps<{modelValue:boolean}>();const emit=defineEmits<{'update:modelValue':[boolean];import:[ContractImportLine[]]}>()
const visible=computed({get:()=>props.modelValue,set:v=>emit('update:modelValue',v)})
const input=ref<HTMLInputElement>(),loading=ref(false),fileName=ref(''),selected=ref(0),sheets=ref<{name:string;rows:unknown[][]}[]>([])
const result=computed(()=>sheets.value.length?parseContractRows(sheets.value[selected.value]?.rows||[]):{items:[],errors:[]})
watch(()=>props.modelValue,()=>{sheets.value=[];fileName.value='';selected.value=0;if(input.value)input.value.value=''})
async function readFile(e:Event){
 const f=(e.target as HTMLInputElement).files?.[0];if(!f)return
 sheets.value=[];fileName.value='';loading.value=true
 try{
  if(!/\.(xlsx|xls)$/i.test(f.name))throw new Error('请选择 Excel 文件')
  if(f.size>5*1024*1024)throw new Error('文件不能超过 5 MB')
  const XLSX=await import('xlsx');const book=XLSX.read(await f.arrayBuffer(),{type:'array',sheetRows:1002})
  if(book.SheetNames.length>20)throw new Error('最多支持 20 个工作表')
  sheets.value=book.SheetNames.map(name=>{const ws=book.Sheets[name];const range=XLSX.utils.decode_range(ws['!fullref']||ws['!ref']||'A1');if(range.e.r>1000||range.e.c>49)throw new Error('工作表最多 1001 行（含表头）、50 列');return{name,rows:XLSX.utils.sheet_to_json<unknown[]>(ws,{header:1,range:0,raw:true,defval:'',blankrows:true})}})
  selected.value=0;fileName.value=f.name
 }catch(e){sheets.value=[];ElMessage.error(e instanceof Error?e.message:'Excel 读取失败，请检查文件')}
 finally{loading.value=false;if(input.value)input.value.value=''}
}
async function downloadTemplate(){try{const XLSX=await import('xlsx');const book=XLSX.utils.book_new();const ws=XLSX.utils.aoa_to_sheet([contractImportHeaders]);ws['!cols']=[{wch:28},{wch:30},{wch:16},{wch:12},{wch:16}];XLSX.utils.book_append_sheet(book,ws,'客户成交明细');XLSX.writeFile(book,'客户成交明细导入模板.xlsx')}catch{ElMessage.error('模板下载失败，请重试')}}
function apply(){if(loading.value||result.value.errors.length||!result.value.items.length)return;emit('import',result.value.items.map(v=>({...v})));visible.value=false}
</script>
<style scoped>
.import-tools{display:flex;align-items:center;gap:12px;flex-wrap:wrap;margin:16px 0}.import-tools span{font-size:12px;color:#64748b}.import-errors{margin:16px 0;padding:12px;background:#fef0f0;color:#b42318;line-height:1.8;max-height:220px;overflow:auto}
</style>
