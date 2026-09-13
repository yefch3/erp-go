<template>
  <el-dialog v-model="open" :title="`${dialogTitle} · ${node.portName}`" width="640px" destroy-on-close>
    <el-alert
      :title="`以下时间均按 ${node.timezone || '港口当地时区'} 填写；原始预计时间和每次修改会自动保留。`"
      type="info"
      :closable="false"
      show-icon
      class="hint"
    />
    <el-form :model="form" label-width="180px">
      <el-form-item v-if="showArrival" label="原始预计到港（ETA）">
        <span class="original-time">{{ formatTime(node.originalEtaAt) }}</span>
      </el-form-item>
      <el-form-item v-if="showArrival" label="最新预计到港（ETA）">
        <el-date-picker v-model="form.latestEtaAt" type="datetime" format="YYYY-MM-DD HH:mm" value-format="YYYY-MM-DD HH:mm" clearable style="width:100%" />
      </el-form-item>
      <el-form-item v-if="showDeparture" label="原始预计离港（ETD）">
        <span class="original-time">{{ formatTime(node.originalEtdAt) }}</span>
      </el-form-item>
      <el-form-item v-if="showDeparture" label="最新预计离港（ETD）">
        <el-date-picker v-model="form.latestEtdAt" type="datetime" format="YYYY-MM-DD HH:mm" value-format="YYYY-MM-DD HH:mm" clearable style="width:100%" />
      </el-form-item>
      <el-divider content-position="left">{{ actualSectionTitle }}</el-divider>
      <el-alert
        :title="actualHint"
        type="warning"
        :closable="false"
        class="hint"
      />
      <el-form-item v-if="showArrival" label="实际到港（ATA）">
        <el-date-picker v-model="form.actualArrivalAt" type="datetime" format="YYYY-MM-DD HH:mm" value-format="YYYY-MM-DD HH:mm" clearable style="width:100%" />
      </el-form-item>
      <el-form-item v-if="showDeparture" label="实际离港（ATD）">
        <el-date-picker v-model="form.actualDepartureAt" type="datetime" format="YYYY-MM-DD HH:mm" value-format="YYYY-MM-DD HH:mm" clearable style="width:100%" />
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
import { ElMessage } from 'element-plus'
import { post } from '../api'
import { wallClockIn, zonedToInstant } from '../lib/zonedtime'
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
const showArrival = computed(() => true)
const showDeparture = computed(() => props.node.nodeType !== 'DESTINATION')
const dialogTitle = computed(() => props.node.nodeType === 'ORIGIN' ? '编辑起运港时间' : props.node.nodeType === 'DESTINATION' ? '编辑目的港到港时间' : '编辑中转港时间')
const actualSectionTitle = computed(() => props.node.nodeType === 'DESTINATION' ? '实际到港' : '实际到港与离港')
const actualHint = computed(() => props.node.nodeType === 'ORIGIN'
  ? '记录船舶抵达起运港和离开起运港的时间；填写实际离港后，船期将进入运输中。'
  : props.node.nodeType === 'DESTINATION'
    ? '填写实际到港时间后，船期将标记为已到港；清空后系统会重新计算进度。'
    : '填写中转港实际到港和离港时间；系统会据此重新计算当前进度。')
const form = reactive({ latestEtaAt: '', latestEtdAt: '', actualArrivalAt: '', actualDepartureAt: '', note: '' })

function formatTime(value: string) {
  if (!value) return '—'
  const instant = new Date(value)
  return Number.isNaN(instant.getTime()) ? value : wallClockIn(instant, props.node.timezone || 'UTC')
}

function toWall(value: string) {
  if (!value) return ''
  const instant = new Date(value)
  return Number.isNaN(instant.getTime()) ? '' : wallClockIn(instant, props.node.timezone || 'UTC')
}

function toInstant(value: string) {
  if (!value) return ''
  return zonedToInstant(value, props.node.timezone || 'UTC')?.toISOString() || ''
}

function resetForm() {
  Object.assign(form, {
    latestEtaAt: toWall(props.node.latestEtaAt),
    latestEtdAt: toWall(props.node.latestEtdAt),
    actualArrivalAt: toWall(props.node.actualArrivalAt),
    actualDepartureAt: toWall(props.node.actualDepartureAt),
    note: '',
  })
}

watch(open, value => { if (value) resetForm() })

async function save() {
  saving.value = true
  try {
    await post(`/shipping/schedules/${props.scheduleId}/progress`, {
      routeNodeId: props.node.id,
      routeVersion: props.routeVersion,
      action: 'UPDATE_TIMES',
      latestEtaAt: showArrival.value ? toInstant(form.latestEtaAt) : props.node.latestEtaAt,
      latestEtdAt: showDeparture.value ? toInstant(form.latestEtdAt) : props.node.latestEtdAt,
      actualArrivalAt: showArrival.value ? toInstant(form.actualArrivalAt) : props.node.actualArrivalAt,
      actualDepartureAt: showDeparture.value ? toInstant(form.actualDepartureAt) : props.node.actualDepartureAt,
      reason: `${dialogTitle.value}（系统记录）`,
      note: form.note,
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
