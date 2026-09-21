<template>
  <section v-loading="loading" class="documents">
    <div class="toolbar">
      <span class="hint">上传附件，可设置提醒更换的日期。</span>
      <el-button v-if="canWrite" type="primary" @click="open()">上传文件</el-button>
    </div>
    <el-table :data="current" empty-text="暂无附件" class="file-list">
      <el-table-column label="文件" min-width="220">
        <template #default="{row}">
          <el-button v-if="previewable(row)" link type="primary" class="filename" @click="preview(row)">{{ row.fileName }}</el-button>
          <span v-else>{{ row.fileName }}</span>
          <span class="size">{{ formatSize(row.sizeBytes) }}</span>
        </template>
      </el-table-column>
      <el-table-column label="提醒日期" min-width="185">
        <template #default="{row}">{{ reminderLabel(row) }}</template>
      </el-table-column>
      <el-table-column label="操作" width="160">
        <template #default="{row}">
          <el-dropdown trigger="click" @command="(command: string) => handleAction(command, row)">
            <el-button link type="primary">更多操作 <span class="dropdown-arrow">▼</span></el-button>
            <template #dropdown>
              <el-dropdown-menu>
                <el-dropdown-item command="download">下载</el-dropdown-item>
                <el-dropdown-item v-if="canWrite" command="remind">设置提醒</el-dropdown-item>
                <el-dropdown-item command="delete" divided>删除</el-dropdown-item>
              </el-dropdown-menu>
            </template>
          </el-dropdown>
        </template>
      </el-table-column>
    </el-table>
    <el-dialog v-model="dialog" :title="editing ? '设置提醒' : '上传文件'" width="min(440px, 92vw)" :close-on-click-modal="!saving" :before-close="closeDialog">
      <el-form label-position="top" @submit.prevent="save">
        <el-form-item v-if="!editing" label="文件" required>
          <input :key="fileKey" type="file" aria-label="选择文件" @change="selectFile" />
          <span class="hint upload-hint">图片、PDF 等，单个文件最大 10 MB</span>
        </el-form-item>
        <p v-else class="selected-name">{{ editing.fileName }}</p>
        <el-form-item label="提醒日期（可选）">
          <el-date-picker v-model="reminderDate" type="date" value-format="YYYY-MM-DD" placeholder="选择需要提醒更换的日期" clearable style="width:100%" />
        </el-form-item>
        <p class="hint">到这一天会在“我的待办”提醒，留空则不提醒。</p>
      </el-form>
      <template #footer>
        <el-button :disabled="saving" @click="dialog=false">取消</el-button>
        <el-button type="primary" :loading="saving" @click="save">{{ editing ? '保存' : '上传' }}</el-button>
      </template>
    </el-dialog>
    <el-dialog v-model="previewOpen" title="附件预览" width="85%" destroy-on-close @closed="releasePreview">
      <CustomerDocumentPdf v-if="previewBlob && previewIsPDF" :source="previewBlob" />
      <img v-else-if="previewURL" :src="previewURL" alt="客户附件" class="image-preview" />
    </el-dialog>
  </section>
</template>
<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { del, get, http } from '../api'
import CustomerDocumentPdf from './CustomerDocumentPdf.vue'
import { announceReminderChanged } from '../lib/homeReminders'
import { type CustomerDocument } from '../lib/customerDocuments'
const props=defineProps<{customerId:string;canWrite:boolean}>()
const loading=ref(false),saving=ref(false),dialog=ref(false),fileKey=ref(0)
const docs=ref<CustomerDocument[]>([]),editing=ref<CustomerDocument|null>(null),selected=ref<File|null>(null)
const reminderDate=ref('')
const now=ref(new Date()), current=computed(()=>docs.value.filter(d=>d.current))
let timer:ReturnType<typeof setInterval>|undefined
const previewOpen=ref(false),previewURL=ref(''),previewIsPDF=ref(false),previewBlob=ref<Blob|null>(null)
const endpoint=computed(()=>`/customers/${props.customerId}/documents`)
async function load(){loading.value=true;try{docs.value=(await get<{documents:CustomerDocument[]}>(endpoint.value)).documents||[]}finally{loading.value=false}}
function reminderOn(doc:CustomerDocument){
 if(!doc.reminderEnabled||!doc.expiresOn)return ''
 const date=new Date(doc.expiresOn+'T00:00:00Z');date.setUTCDate(date.getUTCDate()-doc.remindDays)
 return date.toISOString().slice(0,10)
}
function reminderLabel(doc:CustomerDocument){
 const date=reminderOn(doc);if(!date)return '未设置'
 return date<=now.value.toISOString().slice(0,10)?date+' · 已到提醒时间':date
}
function open(doc?:CustomerDocument){
 editing.value=doc||null;reminderDate.value=doc?reminderOn(doc):'';selected.value=null;fileKey.value++;dialog.value=true
}
function selectFile(event:Event){selected.value=(event.target as HTMLInputElement).files?.[0]||null}
function closeDialog(done:()=>void){if(!saving.value)done()}
async function save(){
 if(saving.value)return
 if(!editing.value&&!selected.value){ElMessage.warning('请选择文件');return}
 if(selected.value&&(!selected.value.size||selected.value.size>10*1024*1024)){ElMessage.warning('请选择不超过 10 MB 的非空文件');return}
 const body=new FormData()
 body.set('title',selected.value?.name||editing.value?.fileName||'附件')
 body.set('remark',editing.value?.remark||'')
 body.set('expiresOn',reminderDate.value||'')
 body.set('remindDays','0')
 body.set('reminderEnabled',String(Boolean(reminderDate.value)))
 if(editing.value)body.set('replacesId',editing.value.id)
 if(selected.value)body.set('file',selected.value)

 saving.value=true;try{await http.post(endpoint.value,body,{timeout:120000});dialog.value=false;ElMessage.success('资料已保存');announceReminderChanged();await load()}finally{saving.value=false}
}
async function handleAction(command:string, doc:CustomerDocument){
 if(command==='download'){await download(doc);return}
 if(command==='remind'){open(doc);return}
 if(command!=='delete')return
 try{await ElMessageBox.confirm('确定删除该附件吗？','删除附件',{type:'warning',confirmButtonText:'删除',cancelButtonText:'取消'})}catch{return}
 await del(`${endpoint.value}/${doc.id}`)
 ElMessage.success('附件已删除');announceReminderChanged();await load()
}
function previewable(doc:CustomerDocument){return ['application/pdf','image/jpeg','image/png','image/gif','image/webp'].includes(doc.contentType)}
async function fileBlob(doc:CustomerDocument){return (await http.get<Blob>(`${endpoint.value}/${doc.id}/file`,{responseType:'blob',timeout:120000})).data}
async function download(doc:CustomerDocument){const blob=await fileBlob(doc);const url=URL.createObjectURL(blob);const a=document.createElement('a');a.href=url;a.download=doc.fileName;a.click();setTimeout(()=>URL.revokeObjectURL(url),1000)}
function releasePreview(){if(previewURL.value)URL.revokeObjectURL(previewURL.value);previewURL.value='';previewBlob.value=null}
async function preview(doc:CustomerDocument){const blob=await fileBlob(doc);releasePreview();previewURL.value=URL.createObjectURL(blob);previewBlob.value=blob;previewIsPDF.value=doc.contentType==='application/pdf';previewOpen.value=true}
function formatSize(size:string){const n=Number(size);return n>=1048576?`${(n/1048576).toFixed(1)} MB`:`${Math.ceil(n/1024)} KB`}
onMounted(()=>{void load();timer=setInterval(()=>{now.value=new Date()},60000)})
onBeforeUnmount(()=>{clearInterval(timer);releasePreview()})
</script>
<style scoped>
.dropdown-arrow { margin-left: 5px; font-size: 10px; }
.documents { padding: 8px 0; }
.toolbar { display: flex; justify-content: space-between; align-items: center; gap: 12px; margin-bottom: 16px; flex-wrap: wrap; }
.hint, .size { color: #8492a6; font-size: 13px; }
.size { margin-left: 12px; white-space: nowrap; }
.filename { max-width: 100%; white-space: normal; text-align: left; height: auto; }
.upload-hint { display: block; width: 100%; margin-top: 8px; }
.selected-name { overflow-wrap: anywhere; }
.image-preview { display: block; max-width: 100%; max-height: 70vh; margin: auto; }
</style>
