<template>
  <el-dialog :model-value="modelValue" :title="orderId ? '补齐历史采购单' : '补录历史采购单'" width="min(1100px, 96vw)" :close-on-click-modal="false" @update:model-value="emit('update:modelValue', $event)">
    <el-alert :closable="false" type="info" title="补录以前已下达、仍需继续执行的采购单。确认后进入已下单；不会自动登记收货或付款。" />
    <el-form label-position="top" size="small" class="history-form" v-loading="loading">
      <el-form-item label="原采购单号"><el-input v-model="form.poNo" :disabled="!!savedId" maxlength="50" placeholder="留空自动生成" /></el-form-item>
      <el-form-item label="供应商" required><el-select v-model="form.supplierId" filterable :disabled="confirmed" style="width:100%"><el-option v-for="s in suppliers" :key="s.id" :value="s.id" :label="`${s.code} · ${s.name}`" /></el-select></el-form-item>
      <el-form-item label="原下单日期" required><el-date-picker v-model="form.originalDate" type="date" value-format="YYYY-MM-DD" style="width:100%" /></el-form-item>
      <el-form-item label="币种"><el-select v-model="form.currency" filterable allow-create :disabled="confirmed" style="width:100%"><el-option v-for="c in ['CNY','USD','EUR','GBP','JPY']" :key="c" :value="c" /></el-select></el-form-item>
      <el-form-item label="预计到货日期"><el-date-picker v-model="form.expectedDate" type="date" value-format="YYYY-MM-DD" style="width:100%" /></el-form-item>
      <el-form-item label="采购员"><el-input :model-value="buyerName" disabled /></el-form-item>
      <el-form-item label="总金额"><el-input :model-value="totalAmount" disabled /></el-form-item>
    </el-form>
    <div v-if="!confirmed" class="history-tools">
      <el-button size="small" :disabled="saving || importing" @click="downloadTemplate">下载 Excel 模板</el-button>
      <el-button size="small" :loading="importing" :disabled="saving || loading" @click="excelInput?.click()">导入 Excel</el-button>
      <input ref="excelInput" type="file" accept=".xlsx,.xls" hidden @change="importExcel" />
      <span>导入产品明细，追加到当前表单；检查后再保存。</span>
    </div>
    <el-alert v-if="importMessage" :title="importMessage" :type="importErrors.length ? 'warning' : 'success'" :closable="true" @close="importMessage=''" />
    <details v-if="importMessage && importErrors.length" class="history-tools"><summary>查看未导入的行（{{importErrors.length}}）</summary><div><div v-for="error in importErrors" :key="error">{{error}}</div></div></details>
    <el-table :data="form.lines" class="history-lines" size="small" max-height="380">
      <el-table-column label="产品名称 *" min-width="165"><template #default="{row}"><el-select  :model-value="row.productId !== '0' ? row.productId : row.productName" filterable allow-create default-first-option :disabled="confirmed" size="small" placeholder="选择产品或输入历史名称" @change="(value:string)=>chooseProduct(row,value)"><el-option v-if="row.productId!=='0' && !products.some(p=>p.id===row.productId)" :value="row.productId" :label="row.productName" /><el-option v-for="p in products" :key="p.id" :value="p.id" :label="`${p.code} · ${p.name}`" /></el-select></template></el-table-column>
      <el-table-column label="规格" min-width="170"><template #default="{row}"><el-input v-model="row.spec" :disabled="confirmed" size="small" maxlength="300" /></template></el-table-column>
      <el-table-column label="本次下单 *" width="110"><template #default="{row}"><el-input v-model="row.qty" :disabled="confirmed" size="small" /></template></el-table-column>
      <el-table-column label="单位 *" width="95"><template #default="{row}"><el-input v-model="row.uom" :disabled="confirmed" size="small" maxlength="32" placeholder="MT/PCS" /></template></el-table-column>
      <el-table-column label="单价" width="120"><template #default="{row}"><el-input v-model="row.unitPrice" size="small" placeholder="未填写" /></template></el-table-column>
      <el-table-column label="金额" width="120"><template #default="{row}">{{ amount(row) }}</template></el-table-column>
      <el-table-column v-if="!confirmed" width="65"><template #default="{$index}"><el-button link type="danger" @click="form.lines.splice($index,1)">移除</el-button></template></el-table-column>
    </el-table>
    <div class="history-tools"><el-button v-if="!confirmed" size="small" @click="form.lines.push(blankLine())">添加产品</el-button><span>空白价格表示未填写，补齐前不能登记到货或付款。</span></div>
    <el-input v-model="form.remark" type="textarea" :rows="2" placeholder="备注（选填）" />
    <div class="history-tools"><label>原合同 / 订单附件（单个不超过 10 MB） <input type="file" multiple @change="selectFiles" /></label></div>
    <div v-for="f in files" :key="f.id" class="history-file">{{f.fileName}} <el-button link type="primary" @click="downloadFile(f)">下载</el-button></div>
    <template #footer><el-button :disabled="saving" @click="emit('update:modelValue',false)">取消</el-button><el-button v-if="!confirmed" :loading="saving" @click="save(false)">保存草稿</el-button><el-button type="primary" :loading="saving" :disabled="loading" @click="save(true)">{{ confirmed ? '保存资料' : '确认补录' }}</el-button></template>
  </el-dialog>
</template>
<script setup lang="ts">
import { computed, reactive, ref, watch } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { get, post, download, saveBlob } from '../api'
import { historicalLineHeaders, parseHistoricalLines } from '../lib/historicalLineImport'
import { useAuthStore } from '../stores/auth'
const excelInput=ref<HTMLInputElement>(),importing=ref(false),importMessage=ref(''),importErrors=ref<string[]>([])
const auth=useAuthStore(),buyerName=ref(auth.employeeName)
const products=ref<{id:string;code:string;name:string;baseUomCode:string}[]>([])
const props=defineProps<{modelValue:boolean;orderId?:string}>()
const emit=defineEmits<{ 'update:modelValue':[boolean];saved:[string] }>()
type Line={id:string;productId:string;productName:string;spec:string;uom:string;qty:string;unitPrice:string}
const blankLine=():Line=>({id:'0',productId:'0',productName:'',spec:'',uom:'',qty:'',unitPrice:''})
const empty=()=>({supplierId:'',poNo:'',originalDate:'',currency:'CNY',expectedDate:'',payableDueDate:'',remark:'',lines:[blankLine()]})
const form=reactive(empty()), suppliers=ref<{id:string;code:string;name:string}[]>([])
const files=ref<{id:string;fileName:string}[]>([]),pendingFiles=ref<File[]>([]),savedId=ref(''),confirmed=ref(false),loading=ref(false),saving=ref(false)
watch(()=>props.modelValue,async open=>{if(!open)return;importMessage.value='';importErrors.value=[];loading.value=true;Object.assign(form,empty());files.value=[];pendingFiles.value=[];savedId.value=props.orderId||'';confirmed.value=false
 try{buyerName.value=auth.employeeName;if(auth.can('product:product:read'))products.value=(await get<{products:typeof products.value}>('/products',{page_size:200,status:'ACTIVE'})).products||[];const s=await get<{suppliers:{id:string;code:string;name:string}[]}>('/suppliers',{page_size:200,status:'ACTIVE'});suppliers.value=s.suppliers||[]
 if(props.orderId){const d=await get<{order:any;items:any[]}>(`/purchase-orders/${props.orderId}`);for(const key of Object.keys(empty()) as (keyof ReturnType<typeof empty>)[]){if(key!=='lines')form[key]=d.order[key]||''}buyerName.value=d.order.buyerName||auth.employeeName;form.lines=d.items.map(l=>({id:l.id,productId:String(l.productId||'0'),productName:l.productName,spec:l.spec,uom:l.uomCode,qty:l.qty,unitPrice:l.unitPrice}));confirmed.value=d.order.status!=='DRAFT';files.value=(await get<{files:typeof files.value}>(`/purchase-orders/${savedId.value}/draft-files`)).files||[]}
 }finally{loading.value=false}})
function chooseProduct(l:Line,value:string){const p=products.value.find(p=>p.id===value);l.productId=p?.id||'0';l.productName=p?.name||value;if(p?.baseUomCode)l.uom=p.baseUomCode}
const totalAmount=computed(()=>form.lines.some(l=>l.unitPrice.trim()===""||!Number.isFinite(Number(l.qty)*Number(l.unitPrice)))?"":form.currency+" "+form.lines.reduce((sum,l)=>sum+Number(l.qty)*Number(l.unitPrice),0).toFixed(2))
function amount(l:Line){return l.unitPrice.trim()===''||!Number.isFinite(Number(l.qty)*Number(l.unitPrice))?'未填写':(Number(l.qty)*Number(l.unitPrice)).toFixed(2)}
function selectFiles(e:Event){const selected=Array.from((e.target as HTMLInputElement).files||[]);if(selected.some(f=>f.size>10*1024*1024)){ElMessage.warning('单个附件不能超过 10 MB');return}pendingFiles.value=selected}
async function downloadFile(f:{id:string;fileName:string}){const r=await download(`/purchase-orders/${savedId.value}/draft-files/${f.id}/download`);saveBlob(r.blob,r.fileName||f.fileName)}
async function base64(f:File){const bytes=new Uint8Array(await f.arrayBuffer());let text='';for(let i=0;i<bytes.length;i+=8192)text+=String.fromCharCode(...bytes.subarray(i,i+8192));return btoa(text)}
async function downloadTemplate(){
 const XLSX=await import('xlsx')
 const sheet=XLSX.utils.aoa_to_sheet([historicalLineHeaders])
 sheet['!cols']=[{wch:22},{wch:25},{wch:35},{wch:18},{wch:12},{wch:18}]
 const book=XLSX.utils.book_new();XLSX.utils.book_append_sheet(book,sheet,'产品明细')
 saveBlob(new Blob([XLSX.write(book,{type:'array',bookType:'xlsx'})],{type:'application/vnd.openxmlformats-officedocument.spreadsheetml.sheet'}),'历史采购单产品明细模板.xlsx')
}
async function importExcel(e:Event){
 const input=e.target as HTMLInputElement,file=input.files?.[0];input.value=''
 if(!file||confirmed.value||saving.value)return
 if(!/\.(xlsx|xls)$/i.test(file.name)||file.size>10*1024*1024){ElMessage.warning('请选择不超过 10 MB 的 Excel 文件');return}
 importing.value=true;importErrors.value=[];importMessage.value=''
 try{
 const XLSX=await import('xlsx'),book=XLSX.read(await file.arrayBuffer(),{type:'array',sheetRows:502})
 const sheet=book.Sheets[book.SheetNames[0]];if(!sheet)throw new Error('Excel 没有可读取的工作表')
 const result=parseHistoricalLines(XLSX.utils.sheet_to_json<unknown[]>(sheet,{header:1,defval:'',raw:true}))
 const existing=form.lines.filter(l=>l.productName.trim()||l.spec.trim()||l.qty.trim()||l.uom.trim()||l.unitPrice.trim())
 const range=sheet['!fullref']||sheet['!ref']||'A1'
 if(XLSX.utils.decode_range(range).e.r>500||existing.length+result.lines.length>500)throw new Error('一张采购单最多 500 条明细，请拆分文件后导入。')
 const added:Line[]=[]
 result.lines.forEach(l=>{
 const matches=products.value.filter(p=>l.productCode?p.code===l.productCode:p.name===l.productName)
 // Only an unambiguous match binds to a product; historical names remain usable.
 const p=matches.length===1?matches[0]:undefined
 added.push({id:'0',productId:p?.id||'0',productName:p?.name||l.productName,spec:l.spec,qty:l.qty,uom:l.uom,unitPrice:l.unitPrice})
 })
 if(added.length)form.lines=[...existing,...added]
 importErrors.value=result.errors
 importMessage.value=`已导入 ${added.length} 行${result.errors.length ? `，${result.errors.length} 行未导入` : ''}。请检查后保存。`
 }catch(error){ElMessage.error(error instanceof Error?error.message:'Excel 读取失败，请检查文件格式')}finally{importing.value=false}
}
async function save(confirm:boolean){
 if(importing.value)return
 if(!form.supplierId||!form.originalDate||!form.lines.length||form.lines.some(l=>!l.productName.trim()||!l.uom.trim()||!(Number(l.qty)>0))){ElMessage.warning('请填写供应商、原下单日期及产品名称、数量和单位');return}
 if(confirm&&!confirmed.value){try{await ElMessageBox.confirm('确认这是以前已经下达的采购单？补录后将进入已下单。','确认补录',{type:'warning'})}catch{return}}
 saving.value=true
 try{
 // Save first so failed uploads can be retried against this same draft.
 const payload=()=>({...form,id:savedId.value||'0',confirm:false})
 let r=await post<{id:string;poNo:string;status:string}>('/purchase-orders/historical',payload());savedId.value=r.id;form.poNo=r.poNo
 while(pendingFiles.value.length){const f=pendingFiles.value[0];await post(`/purchase-orders/${r.id}/draft-files`,{file_name:f.name,content_type:f.type,file_data:await base64(f)});pendingFiles.value.shift()}
 if(confirm&&!confirmed.value)r=await post('/purchase-orders/historical',{...payload(),confirm:true})
 ElMessage.success(confirm?'采购单已保存':'草稿已保存');emit('saved',r.status);emit('update:modelValue',false)
 }finally{saving.value=false}
}
</script>
<style scoped>
.history-form{display:grid;grid-template-columns:repeat(4,minmax(0,1fr));gap:0 14px;margin-top:14px}.history-tools{display:flex;gap:16px;align-items:center;margin:12px 0;font-size:12px;color:#64748b}.history-file{font-size:13px} :deep(.el-table){font-size:13px}@media(max-width:750px){.history-form{grid-template-columns:repeat(2,minmax(0,1fr))}}
/* Compact ledger rows: separators belong to rows, not each input. */
.history-lines :deep(.el-table__cell) { padding: 3px 0; }
.history-lines :deep(.cell) { padding: 0 8px; line-height: 26px; }
.history-lines :deep(td.el-table__cell) { border-bottom: 1px solid #e2e8f0; }
.history-lines :deep(.el-input__wrapper),
.history-lines :deep(.el-select__wrapper) {
  min-height: 26px;
  padding: 0 4px;
  background: transparent;
  border: 0;
  border-radius: 0;
  box-shadow: none !important;
}
.history-lines :deep(.el-input__wrapper:focus-within),
.history-lines :deep(.el-select__wrapper.is-focused) {
  background: #f0f9ff;
  box-shadow: inset 0 -2px 0 #409eff !important;
}
.history-lines :deep(.el-input__inner) { height: 26px; font-size: 13px; }
.history-lines :deep(.el-select__wrapper) { font-size: 13px; }
.history-lines :deep(.el-select) { width: 100%; }
.history-lines :deep(.el-input.is-disabled .el-input__inner),
.history-lines :deep(.el-select__wrapper.is-disabled) { color: #64748b; -webkit-text-fill-color: #64748b; }
</style>
