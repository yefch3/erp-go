<template>
  <el-dialog v-model="open" :title="`编辑港口时间 · ${node.portName}`" width="640px" destroy-on-close>
    <el-alert
      title="原始预计时间会永久保留；这里修改的是最新预计时间或对实际时间进行更正。所有修改都会写入变更记录。"
      type="info"
      :closable="false"
      show-icon
      class="hint"
    />
    <el-form ref="formRef" :model="form" :rules="rules" label-width="130px">
      <el-form-item label="原始预计到港">
        <span class="original-time">{{ formatTime(node.originalEtaAt) }}</span>
      </el-form-item>
      <el-form-item label="最新预计到港">
        <el-date-picker v-model="form.latestEtaAt" type="datetime" value-format="YYYY-MM-DDTHH:mm:ssZ" clearable style="width:100%" />
      </el-form-item>
      <el-form-item label="原始预计离港">
        <span class="original-time">{{ formatTime(node.originalEtdAt) }}</span>
      </el-form-item>
      <el-form-item label="最新预计离港">
        <el-date-picker v-model="form.latestEtdAt" type="datetime" value-format="YYYY-MM-DDTHH:mm:ssZ" clearable style="width:100%" />
      </el-form-item>
      <el-divider content-position="left">实际时间更正</el-divider>
      <el-alert
        title="如果之前选错了到港或离港，可清空对应实际时间；系统会重新计算当前进度和船期状态。"
        type="warning"
        :closable="false"
        class="hint"
      />
      <el-form-item label="实际到港">
        <el-date-picker v-model="form.actualArrivalAt" type="datetime" value-format="YYYY-MM-DDTHH:mm:ssZ" clearable style="width:100%" />
      </el-form-item>
      <el-form-item label="实际离港">
        <el-date-picker v-model="form.actualDepartureAt" type="datetime" value-format="YYYY-MM-DDTHH:mm:ssZ" clearable style="width:100%" />
      </el-form-item>
      <el-form-item label="更正原因" prop="reason">
        <el-input v-model="form.reason" type="textarea" :rows="2" placeholder="请说明时间来源或更正原因" />
      </el-form-item>
      <el-form-item label="备注">
        <el-input v-model="form.note" type="textarea" :rows="2" />
      </el-form-item>
    </el-form>
    <template #footer>
      <el-button @click="open=false">取消</el-button>
      <el-button type="primary" :loading="saving" @click="save">保存时间</el-button>
    </template>
  </el-dialog>
</template>

<script setup lang="ts">
import { computed, reactive, ref, watch } from 'vue'
import type { FormInstance, FormRules } from 'element-plus'
import { ElMessage } from 'element-plus'
import { post } from '../api'
import type { ShippingRouteNode } from '../shipping'

const props = defineProps<{
  modelValue: boolean
  scheduleId: string
  routeVersion: number
  node: ShippingRouteNode
}>()
const emit = defineEmits<{ 'update:modelValue': [boolean]; saved: [] }>()
const open = computed({ get: () => props.modelValue, set: value => emit('update:modelValue', value) })
const saving = ref(false)
const formRef = ref<FormInstance>()
const form = reactive({ latestEtaAt: '', latestEtdAt: '', actualArrivalAt: '', actualDepartureAt: '', reason: '', note: '' })
const rules: FormRules = { reason: [{ required: true, message: '请输入更正原因' }] }

function formatTime(value: string) {
  return value ? new Date(value).toLocaleString() : '—'
}

function resetForm() {
  Object.assign(form, {
    latestEtaAt: props.node.latestEtaAt || '',
    latestEtdAt: props.node.latestEtdAt || '',
    actualArrivalAt: props.node.actualArrivalAt || '',
    actualDepartureAt: props.node.actualDepartureAt || '',
    reason: '',
    note: '',
  })
}

watch(open, value => { if (value) resetForm() })

async function save() {
  if (!await formRef.value?.validate().catch(() => false)) return
  saving.value = true
  try {
    await post(`/shipping/schedules/${props.scheduleId}/progress`, {
      routeNodeId: props.node.id,
      routeVersion: props.routeVersion,
      action: 'UPDATE_TIMES',
      ...form,
    })
    ElMessage.success('港口时间已更新，变更记录已保存')
    open.value = false
    emit('saved')
  } finally {
    saving.value = false
  }
}
</script>

<style scoped>
.hint { margin-bottom: 18px; }
.original-time { color: var(--el-text-color-secondary); }
</style>
