<template>
  <el-dialog v-model="visible" title="批量导入供应商" width="760px" @closed="reset">
    <el-alert title="中文名、英文名或简称至少填写一项，编码可留空由系统生成。错误行会跳过，其他供应商仍可正常导入。" type="info" :closable="false" show-icon />
    <div class="toolbar">
      <el-button @click="downloadTemplate">下载固定 Excel 模板</el-button>
      <input type="file" accept=".xlsx,.xls" @change="chooseFile" />
    </div>
    <el-table v-if="rows.length" :data="rows.slice(0,8)" max-height="280">
      <el-table-column prop="rowNumber" label="行号" width="70" />
      <el-table-column prop="code" label="供应商编码" width="140" />
      <el-table-column label="供应商名称"><template #default="{row}">{{row.nameZh||row.nameEn}}</template></el-table-column>
      <el-table-column prop="contactName" label="联系人" />
    </el-table>
    <el-alert v-if="preview" class="result" :type="preview.issues?.length?'error':'success'" :closable="false">
      <template #title>可导入 {{preview.readyCount||0}} 家供应商，未通过 {{preview.issues?.length||0}} 行</template>
      <ul v-if="preview.issues?.length"><li v-for="issue in preview.issues" :key="`${issue.rowNumber}-${issue.code}`">第 {{issue.rowNumber}} 行：{{issue.message}}</li></ul>
    </el-alert>
    <template #footer><el-button @click="visible=false">取消</el-button><el-button :disabled="!rows.length" :loading="loading" @click="request(false)">预检查</el-button><el-button type="primary" :disabled="!preview||!preview.readyCount" :loading="loading" @click="request(true)">确认导入 {{preview?.readyCount||0}} 家</el-button></template>
  </el-dialog>
</template>
<script setup lang="ts">
import{computed,ref}from'vue';import{ElMessage}from'element-plus';import{post}from'../api';import{parseSupplierImportWorkbook}from'../lib/supplierImportWorkbook'
const props=defineProps<{modelValue:boolean}>(),emit=defineEmits<{'update:modelValue':[boolean];imported:[]}>();const visible=computed({get:()=>props.modelValue,set:value=>emit('update:modelValue',value)}),rows=ref<Record<string,unknown>[]>([]),preview=ref<any>(null),loading=ref(false)
function reset(){rows.value=[];preview.value=null}function downloadTemplate(){const a=document.createElement('a');a.href='/templates/supplier-import.xlsx';a.download='供应商批量导入模板.xlsx';a.click()}
async function chooseFile(event:Event){const file=(event.target as HTMLInputElement).files?.[0];if(!file)return;try{rows.value=await parseSupplierImportWorkbook(file.name,await file.arrayBuffer());preview.value=null;if(!rows.value.length)throw new Error('模板中没有可导入的数据')}catch(error){rows.value=[];preview.value=null;ElMessage.error(error instanceof Error?error.message:'Excel 文件读取失败')}}
async function request(confirm:boolean){loading.value=true;try{const result=await post<any>('/suppliers/import',{rows:rows.value,confirm});preview.value=result;if(confirm&&result.importedCount>0){const failed=Number(result.issues?.length||0);if(failed){preview.value={...result,readyCount:0};ElMessage.warning(`已导入 ${result.importedCount} 家供应商，另有 ${failed} 行未导入，请查看错误说明`)}else{ElMessage.success(`已导入 ${result.importedCount} 家供应商及联系人`);visible.value=false}emit('imported')}}finally{loading.value=false}}
</script>
<style scoped>.toolbar{display:flex;align-items:center;gap:16px;margin:18px 0}.result{margin-top:16px}.result ul{margin:8px 0 0;padding-left:20px;max-height:120px;overflow:auto}</style>
