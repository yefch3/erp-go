<template>
  <el-dialog v-model="open" :title="t('ports.importTitle')" width="760px" destroy-on-close @closed="reset">
    <el-alert :title="t('ports.importHint')" type="info" :closable="false" show-icon />
    <div class="import-actions">
      <el-button @click="downloadTemplate">{{ t('ports.downloadTemplate') }}</el-button>
      <input ref="fileInput" type="file" accept=".csv,text/csv" @change="readFile" />
    </div>
    <el-table v-if="rows.length" :data="rows.slice(0,8)" size="small" max-height="300">
      <el-table-column prop="rowNumber" :label="t('ports.row')" width="70" />
      <el-table-column prop="unlocode" label="UN/LOCODE" width="110" />
      <el-table-column prop="nameZh" :label="t('ports.nameZh')" />
      <el-table-column prop="nameEn" :label="t('ports.nameEn')" />
      <el-table-column prop="countryCode" :label="t('ports.country')" width="80" />
      <el-table-column prop="timezone" :label="t('ports.timezone')" width="150" />
    </el-table>
    <div v-if="rows.length>8" class="muted">{{ t('ports.moreRows',{count:rows.length-8}) }}</div>
    <el-descriptions v-if="preview" :column="4" border class="summary">
      <el-descriptions-item :label="t('ports.toCreate')">{{ preview.createCount }}</el-descriptions-item>
      <el-descriptions-item :label="t('ports.toUpdate')">{{ preview.updateCount }}</el-descriptions-item>
      <el-descriptions-item :label="t('ports.toSkip')">{{ preview.skipCount }}</el-descriptions-item>
      <el-descriptions-item :label="t('ports.errors')">{{ preview.issues?.length??0 }}</el-descriptions-item>
    </el-descriptions>
    <el-table v-if="preview?.issues?.length" :data="preview.issues" size="small" max-height="220">
      <el-table-column prop="rowNumber" :label="t('ports.row')" width="70" />
      <el-table-column prop="message" :label="t('ports.errorMessage')" />
    </el-table>
    <template #footer>
      <el-button @click="open=false">{{ t('common.cancel') }}</el-button>
      <el-button :disabled="!rows.length" :loading="checking" @click="check">{{ t('ports.previewImport') }}</el-button>
      <el-button type="primary" :disabled="!canCommit" :loading="saving" @click="commit">{{ t('ports.confirmImport') }}</el-button>
    </template>
  </el-dialog>
</template>

<script setup lang="ts">
import { computed, ref } from 'vue'
import { ElMessage } from 'element-plus'
import { useI18n } from 'vue-i18n'
import { post } from '../api'

interface Row { rowNumber:number; unlocode:string; nameZh:string; nameEn:string; countryCode:string; city:string; timezone:string; aliases:string[]; remark:string }
interface Result { createCount:number; updateCount:number; skipCount:number; issues:{rowNumber:number;code:string;message:string}[] }
const props=defineProps<{modelValue:boolean}>();const emit=defineEmits<{ 'update:modelValue':[boolean]; imported:[] }>()
const {t}=useI18n();const rows=ref<Row[]>([]);const preview=ref<Result>();const checking=ref(false);const saving=ref(false);const fileInput=ref<HTMLInputElement>()
const open=computed({get:()=>props.modelValue,set:value=>emit('update:modelValue',value)})
const canCommit=computed(()=>!!preview.value&&!preview.value.issues?.length&&(preview.value.createCount+preview.value.updateCount)>0)

function parseCSVLine(line:string){
  const values:string[]=[];let current='';let quoted=false
  for(let i=0;i<line.length;i++){const char=line[i];if(char==='"'){if(quoted&&line[i+1]==='"'){current+='"';i++}else quoted=!quoted}else if(char===','&&!quoted){values.push(current.trim());current=''}else current+=char}
  values.push(current.trim());return values
}
async function readFile(event:Event){
  preview.value=undefined
  const file=(event.target as HTMLInputElement).files?.[0];if(!file)return
  const lines=(await file.text()).replace(/^\uFEFF/,'').split(/\r?\n/).filter(line=>line.trim())
  const header=parseCSVLine(lines.shift()??'').map(value=>value.toLowerCase().replace(/[^a-z]/g,''))
  const at=(values:string[],name:string)=>values[header.indexOf(name)]??''
  rows.value=lines.map((line,index)=>{const values=parseCSVLine(line);return{rowNumber:index+2,unlocode:at(values,'unlocode').toUpperCase(),nameZh:at(values,'namezh'),nameEn:at(values,'nameen'),countryCode:at(values,'countrycode').toUpperCase(),city:at(values,'city'),timezone:at(values,'timezone'),aliases:at(values,'aliases').split('|').map(v=>v.trim()).filter(Boolean),remark:at(values,'remark')}})
  if(!header.includes('unlocode')||!header.includes('namezh')||!header.includes('nameen')||!header.includes('countrycode')||!header.includes('timezone')){rows.value=[];ElMessage.error(t('ports.importColumnsInvalid'))}
}
async function check(){checking.value=true;try{preview.value=await post<Result>('/ports/import',{rows:rows.value,confirm:false})}finally{checking.value=false}}
async function commit(){saving.value=true;try{const result=await post<Result>('/ports/import',{rows:rows.value,confirm:true});ElMessage.success(t('ports.imported',{create:result.createCount,update:result.updateCount,skip:result.skipCount}));emit('imported');open.value=false}finally{saving.value=false}}
function downloadTemplate(){const content='UNLOCODE,NameZH,NameEN,CountryCode,City,Timezone,Aliases,Remark\nCNSHA,上海港,Shanghai,CN,上海,Asia/Shanghai,Port of Shanghai|上海,\n';const url=URL.createObjectURL(new Blob(['\uFEFF'+content],{type:'text/csv;charset=utf-8'}));const a=document.createElement('a');a.href=url;a.download='port-import-template.csv';a.click();URL.revokeObjectURL(url)}
function reset(){rows.value=[];preview.value=undefined;if(fileInput.value)fileInput.value.value=''}
</script>

<style scoped>
.import-actions{display:flex;align-items:center;gap:16px;margin:18px 0}.summary{margin:16px 0}.muted{color:var(--el-text-color-secondary);font-size:12px;margin-top:6px}
</style>
