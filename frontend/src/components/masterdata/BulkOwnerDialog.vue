<template>
  <el-dialog :model-value="open" :title="title" width="560px" @update:model-value="emit('update:open', $event)">
    <p class="summary">已选择 <strong>{{ selectedIds.length }}</strong> 个{{ entityLabel }}</p>
    <el-form label-position="top">
      <el-form-item :label="personLabel" required>
        <el-select
          v-model="selectedOwnerIds"
          multiple
          filterable
          collapse-tags
          collapse-tags-tooltip
          :loading="loading"
          :placeholder="`搜索并选择${personLabel}`"
          style="width:100%"
        >
          <el-option v-for="employee in employees" :key="employee.id" :value="employee.id" :label="employeeLabel(employee)">
            <div class="employee-option"><span>{{ employee.name }}</span><small>{{ employee.employeeNo }}<template v-if="employee.departmentName"> · {{ employee.departmentName }}</template></small></div>
          </el-option>
        </el-select>
      </el-form-item>
    </el-form>
    <el-alert
      :title="action === 'ADD' ? '新增负责人会保留原有负责人。' : '只移除所选负责人，其他负责人不受影响。'"
      type="info"
      :closable="false"
      show-icon
    />
    <template #footer>
      <el-button @click="emit('update:open', false)">取消</el-button>
      <el-button type="primary" :loading="saving" :disabled="!selectedOwnerIds.length" @click="submit">确认{{ actionLabel }}</el-button>
    </template>
  </el-dialog>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { ElMessage } from 'element-plus'
import { get, post } from '../../api'

interface EmployeeOption { id:number;employeeNo:string;name:string;departmentName:string }
const props = defineProps<{ open:boolean; entityType:'customer'|'supplier'; action:'ADD'|'REMOVE'; selectedIds:string[] }>()
const emit = defineEmits<{ 'update:open':[value:boolean]; saved:[] }>()
const employees=ref<EmployeeOption[]>([]),selectedOwnerIds=ref<number[]>([]),loading=ref(false),saving=ref(false)
const entityLabel=computed(()=>props.entityType==='customer'?'客户':'供应商')
const personLabel=computed(()=>props.entityType==='customer'?'销售负责人':'采购负责人')
const actionLabel=computed(()=>props.action==='ADD'?'添加':'移除')
const title=computed(()=>`批量${actionLabel.value}${entityLabel.value}负责人`)
const basePath=computed(()=>props.entityType==='customer'?'/customers':'/suppliers')
function employeeLabel(employee:EmployeeOption){return `${employee.employeeNo} · ${employee.name}${employee.departmentName?` · ${employee.departmentName}`:''}`}
async function loadOptions(){loading.value=true;try{const data=await get<{employees:EmployeeOption[]}>(`${basePath.value}/owner-options`);employees.value=data.employees||[]}finally{loading.value=false}}
async function submit(){
  if(!selectedOwnerIds.value.length)return
  saving.value=true
  try{
    const idKey=props.entityType==='customer'?'customerIds':'supplierIds'
    const data=await post<{changedCount:number}>(`${basePath.value}/owners/batch`,{[idKey]:props.selectedIds,owners:selectedOwnerIds.value.map(employeeId=>({employeeId})),action:props.action})
    ElMessage.success(`已为 ${props.selectedIds.length} 个${entityLabel.value}${actionLabel.value}负责人，实际更新 ${Number(data.changedCount||0)} 条关系`)
    emit('update:open',false);emit('saved')
  }finally{saving.value=false}
}
watch(()=>props.open,(open)=>{if(!open)return;selectedOwnerIds.value=[];void loadOptions()})
</script>

<style scoped>
.summary{margin:0 0 18px;color:var(--el-text-color-regular)}.employee-option{display:flex;align-items:center;justify-content:space-between;gap:18px}.employee-option small{color:var(--el-text-color-secondary)}
</style>
