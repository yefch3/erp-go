<template><div v-loading="loading">
  <div class="page-head"><div><el-button link @click="router.push('/shipping/schedules')">← {{ t('shipping.back') }}</el-button><h2>{{ schedule?.scheduleNo }}</h2></div><div v-if="schedule"><el-tag :type="statusTag(schedule.status)">{{ t(`shipping.statuses.${schedule.status}`) }}</el-tag><el-tag v-if="schedule.delayDays>0" :type="schedule.delayDays>=4?'danger':'warning'">+{{schedule.delayDays}}天</el-tag><el-tag v-if="schedule.hasTemporaryCall" type="warning">临时挂港</el-tag><el-button v-if="basicWritable" @click="editOpen=true">编辑基础信息</el-button><el-button v-if="progressWritable" type="primary" @click="progressOpen=true">更新船期进度</el-button><el-button v-if="basicWritable&&schedule.status==='ARRIVED'" :loading="saving" @click="completeSchedule">确认完成</el-button><el-button v-if="basicWritable&&schedule.status!=='ARRIVED'" type="danger" plain @click="cancelSchedule">{{ t('shipping.cancelSchedule') }}</el-button></div></div>
  <el-card v-if="schedule" shadow="never"><el-tabs>
    <el-tab-pane label="基础信息"><el-descriptions :column="2" border><el-descriptions-item :label="t('shipping.contractNo')">{{schedule.contractNo||'—'}}</el-descriptions-item><el-descriptions-item :label="t('shipping.customer')">{{schedule.customerName||'—'}}</el-descriptions-item><el-descriptions-item :label="t('shipping.carrier')">{{schedule.carrierForwarder||'—'}}</el-descriptions-item><el-descriptions-item :label="t('shipping.responsible')">{{schedule.responsibleName}}</el-descriptions-item><el-descriptions-item :label="t('shipping.vessel')">{{schedule.vesselName}}</el-descriptions-item><el-descriptions-item :label="t('shipping.voyage')">{{schedule.voyageNo}}</el-descriptions-item><el-descriptions-item label="原始ETA">{{schedule.originalEta}}</el-descriptions-item><el-descriptions-item label="最新ETA"><span :class="delayClass">{{schedule.eta}}</span><span v-if="schedule.delayDays>0">（+{{schedule.delayDays}}天）</span></el-descriptions-item><el-descriptions-item label="当前进度">{{schedule.currentProgress||'—'}}</el-descriptions-item><el-descriptions-item :label="t('shipping.remark')">{{schedule.remark||'—'}}</el-descriptions-item><el-descriptions-item :label="t('shipping.createdBy')">{{schedule.createdByName}} · {{formatTime(schedule.createdAt)}}</el-descriptions-item><el-descriptions-item :label="t('shipping.updatedBy')">{{schedule.updatedByName}} · {{formatTime(schedule.updatedAt)}}</el-descriptions-item></el-descriptions></el-tab-pane>
    <el-tab-pane label="港口路线">
      <div class="route-head"><div><strong>{{schedule.currentProgress}}</strong><span class="muted"> · 路线版本 {{schedule.routeVersion}}</span></div><el-button v-if="routeWritable" type="primary" plain @click="routeOpen=true">新增港口</el-button></div>
      <div v-if="hiddenCompletedCount" class="completed-toggle"><span>已收起 {{hiddenCompletedCount}} 个已离港/已跳过港口</span><el-button link type="primary" @click="showCompleted=!showCompleted">{{showCompleted?'收起历史港口':'展开查看'}}</el-button></div>
      <el-timeline class="route-timeline"><el-timeline-item v-for="item in visibleRouteNodes" :key="item.node.id" :type="nodeColor(item.node)" :hollow="item.node.nodeStatus==='PLANNED'" placement="top">
        <el-card shadow="never" :class="['route-node',item.node.nodeType==='TEMPORARY'?'temporary':'',isCompletedNode(item.node)?'completed':'',isCurrentNode(item.node)?'current':'']"><div class="node-title"><div><strong>{{item.node.portName}}</strong><el-tag size="small" :type="item.node.nodeType==='TEMPORARY'?'warning':'info'">{{nodeTypeText(item.node.nodeType)}}</el-tag><el-tag size="small" :type="nodeStatusType(displayNodeStatus(item.node))">{{nodeStatusText(displayNodeStatus(item.node))}}</el-tag><el-tag v-if="item.node.nodeType==='TEMPORARY'" size="small" type="warning">临时挂港</el-tag></div><div class="node-actions"><el-button v-if="progressWritable" link type="primary" @click="openTimeEditor(item.node)">编辑时间</el-button><template v-if="routeWritable&&['TRANSIT','TEMPORARY'].includes(item.node.nodeType)"><el-button link :disabled="item.index<=1" @click="moveNode(item.index,-1)">上移</el-button><el-button link :disabled="item.index>=routeNodes.length-2" @click="moveNode(item.index,1)">下移</el-button><el-button v-if="canRemoveNode(item.node)" link type="danger" @click="removeNode(item.node)">移除港口</el-button></template></div></div><div class="times"><span>预计到港：{{formatNodeTime(item.node.latestEtaAt,item.node.timezone)}}</span><span>实际到港：{{formatNodeTime(item.node.actualArrivalAt,item.node.timezone)}}</span><span>预计离港：{{formatNodeTime(item.node.latestEtdAt,item.node.timezone)}}</span><span>实际离港：{{formatNodeTime(item.node.actualDepartureAt,item.node.timezone)}}</span></div><div v-if="item.node.remark" class="muted">{{item.node.remark}}</div></el-card></el-timeline-item>
      </el-timeline>
      <el-divider content-position="left">延误记录</el-divider>
      <el-table :data="delayEvents" size="small"><el-table-column prop="createdAt" label="时间" width="170"><template #default="{row}">{{formatTime(row.createdAt)}}</template></el-table-column><el-table-column label="ETA变化" width="190"><template #default="{row}">{{row.oldEta}} → {{row.newEta}}</template></el-table-column><el-table-column label="本次变化" width="100"><template #default="{row}"><span :class="row.changeDays>0?'danger-text':'success-text'">{{row.changeDays>0?'+':''}}{{row.changeDays}}天</span></template></el-table-column><el-table-column label="累计" width="100"><template #default="{row}">+{{row.cumulativeDelayDays}}天</template></el-table-column><el-table-column prop="reason" label="原因"/><el-table-column prop="operatorName" label="操作人" width="110"/></el-table>
    </el-tab-pane>
    <el-tab-pane label="单证附件"><ShippingDocumentsPanel v-if="schedule" :schedule-id="schedule.id" @changed="load" /></el-tab-pane>
    <el-tab-pane label="变更记录"><el-timeline v-if="changes.length"><el-timeline-item v-for="change in changes" :key="change.id" :timestamp="formatTime(change.createdAt)" placement="top"><strong>{{change.operatorName}} · {{change.changeType}}</strong><div>{{change.fieldName}}：{{change.oldValue||'—'}} → {{change.newValue||'—'}}</div><div class="reason">{{change.reason}}</div></el-timeline-item></el-timeline><el-empty v-else :description="t('shipping.noChanges')" /></el-tab-pane>
  </el-tabs></el-card>
  <ShippingScheduleDialog v-if="schedule" v-model="editOpen" :schedule="schedule" @saved="load" />
  <ShippingRouteNodeDialog v-if="schedule" v-model="routeOpen" :schedule-id="schedule.id" :route-version="schedule.routeVersion" :nodes="routeNodes" @saved="load" />
  <ShippingProgressDialog v-if="schedule" v-model="progressOpen" :schedule-id="schedule.id" :route-version="schedule.routeVersion" :nodes="routeNodes" @saved="load" />
  <ShippingRouteNodeTimeDialog v-if="schedule&&editingNode" v-model="timeOpen" :schedule-id="schedule.id" :route-version="schedule.routeVersion" :node="editingNode" @saved="load" />
</div></template>
<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { useI18n } from 'vue-i18n'
import { useRoute, useRouter } from 'vue-router'
import { del, get, post, put } from '../api'
import ShippingScheduleDialog from '../components/ShippingScheduleDialog.vue'
import ShippingRouteNodeDialog from '../components/ShippingRouteNodeDialog.vue'
import ShippingProgressDialog from '../components/ShippingProgressDialog.vue'
import ShippingRouteNodeTimeDialog from '../components/ShippingRouteNodeTimeDialog.vue'
import ShippingDocumentsPanel from '../components/ShippingDocumentsPanel.vue'
import { statusTag, type ScheduleChange, type ShippingSchedule, type ShippingRouteNode, type ShippingDelayEvent } from '../shipping'
import { useAuthStore } from '../stores/auth'

const { t } = useI18n()
const route = useRoute()
const router = useRouter()
const auth = useAuthStore()
const schedule = ref<ShippingSchedule>()
const changes = ref<ScheduleChange[]>([])
const routeNodes = ref<ShippingRouteNode[]>([])
const delayEvents = ref<ShippingDelayEvent[]>([])
const editingNode = ref<ShippingRouteNode>()
const loading = ref(false)
const saving = ref(false)
const editOpen = ref(false)
const routeOpen = ref(false)
const progressOpen = ref(false)
const timeOpen = ref(false)
const showCompleted = ref(false)

const basicWritable = computed(() => auth.can('shipping:schedule:write') && !['COMPLETED', 'CANCELLED'].includes(schedule.value?.status ?? ''))
const routeWritable = computed(() => auth.can('shipping:route:write') && basicWritable.value)
const progressWritable = computed(() => auth.can('shipping:progress:write') && basicWritable.value)
const delayClass = computed(() => schedule.value?.delayDays ? schedule.value.delayDays >= 4 ? 'danger-text' : 'warning-text' : 'success-text')
const hiddenCompletedCount = computed(() => routeNodes.value.filter(isCompletedNode).length)
const visibleRouteNodes = computed(() => routeNodes.value
  .map((node, index) => ({ node, index }))
  .filter(item => showCompleted.value || !isCompletedNode(item.node)))

async function load() {
  loading.value = true
  try {
    const data = await get<{ schedule: ShippingSchedule; changes: ScheduleChange[]; routeNodes: ShippingRouteNode[]; delayEvents: ShippingDelayEvent[] }>(`/shipping/schedules/${route.params.id}`)
    schedule.value = data.schedule
    changes.value = data.changes
    routeNodes.value = data.routeNodes
    delayEvents.value = data.delayEvents
  } finally {
    loading.value = false
  }
}

function formatTime(value: string) { return value ? new Date(value).toLocaleString() : '—' }
function formatNodeTime(value: string, timezone: string) {
  if (!value) return '—'
  try {
    return new Intl.DateTimeFormat('zh-CN', { timeZone: timezone || 'UTC', year: 'numeric', month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit' }).format(new Date(value))
  } catch {
    return formatTime(value)
  }
}
function nodeTypeText(value: string) { return ({ ORIGIN: '起运港', TRANSIT: '中转港', TEMPORARY: '临时挂靠港', DESTINATION: '目的港' } as Record<string, string>)[value] ?? value }
function nodeStatusText(value: string) { return ({ PLANNED: '未到达', APPROACHING: '驶向中', ARRIVED: '已到港', DEPARTED: '已离港', SKIPPED: '已跳过' } as Record<string, string>)[value] ?? value }
function nodeStatusType(value: string) { return value === 'DEPARTED' ? 'success' : value === 'SKIPPED' ? 'info' : value === 'APPROACHING' ? 'primary' : value === 'ARRIVED' ? 'success' : 'info' }
function displayNodeStatus(node: ShippingRouteNode) { if (node.nodeStatus !== 'APPROACHING' || isCurrentNode(node)) return node.nodeStatus; return node.actualArrivalAt ? 'ARRIVED' : 'PLANNED' }
function nodeColor(node: ShippingRouteNode) { if (node.nodeType === 'TEMPORARY') return 'warning'; if (['ARRIVED', 'DEPARTED'].includes(node.nodeStatus)) return 'success'; return 'primary' }
function isCompletedNode(node: ShippingRouteNode) { return ['DEPARTED', 'SKIPPED'].includes(node.nodeStatus) }
function isCurrentNode(node: ShippingRouteNode) { return Boolean(schedule.value?.currentProgress?.includes(node.portName)) }
function canRemoveNode(node: ShippingRouteNode) { return ['TRANSIT', 'TEMPORARY'].includes(node.nodeType) && !node.actualArrivalAt && !node.actualDepartureAt && !['ARRIVED', 'DEPARTED', 'SKIPPED'].includes(node.nodeStatus) }
function isDialogDismissed(error: unknown) { return error === 'cancel' || error === 'close' }

function openTimeEditor(node: ShippingRouteNode) {
  editingNode.value = node
  timeOpen.value = true
}

async function moveNode(index: number, direction: number) {
  if (!schedule.value) return
  const reordered = [...routeNodes.value]
  const [node] = reordered.splice(index, 1)
  reordered.splice(index + direction, 0, node)
  let value: string
  try {
    ({ value } = await ElMessageBox.prompt('请输入调整路线顺序的原因', '调整路线', { inputValidator: input => !!input.trim() || '原因必填' }))
  } catch (action) {
    if (isDialogDismissed(action)) return
    throw action
  }
  await put(`/shipping/schedules/${schedule.value.id}/route/order`, { nodeIds: reordered.map(item => item.id), reason: value, routeVersion: schedule.value.routeVersion })
  ElMessage.success('路线顺序已更新')
  await load()
}

async function removeNode(node: ShippingRouteNode) {
  if (!schedule.value) return
  let value: string
  try {
    ({ value } = await ElMessageBox.prompt(`将从当前路线移除“${node.portName}”，历史记录仍会保留。请输入移除原因。`, '移除港口', { type: 'warning', confirmButtonText: '确认移除', inputValidator: input => !!input.trim() || '移除原因必填' }))
  } catch (action) {
    if (isDialogDismissed(action)) return
    throw action
  }
  await del(`/shipping/schedules/${schedule.value.id}/route/nodes/${node.id}`, { reason: value, routeVersion: schedule.value.routeVersion })
  ElMessage.success('港口已从路线移除，变更记录已保存')
  await load()
}

async function completeSchedule() {
  let value: string
  try {
    ({ value } = await ElMessageBox.prompt('确认船期业务已经结束。请输入完成说明。', '确认完成', { inputValue: '船期已完成', inputValidator: input => !!input.trim() || '完成说明必填' }))
  } catch (action) {
    if (isDialogDismissed(action)) return
    throw action
  }
  saving.value = true
  try {
    await post(`/shipping/schedules/${route.params.id}/status`, { status: 'COMPLETED', reason: value })
    ElMessage.success('船期已完成')
    await load()
  } finally {
    saving.value = false
  }
}

async function cancelSchedule() {
  let value: string
  try {
    ({ value } = await ElMessageBox.prompt(t('shipping.cancelReasonPrompt'), t('shipping.cancelSchedule'), { inputValidator: input => !!input.trim() || t('shipping.reasonRequired'), type: 'warning' }))
  } catch (action) {
    if (isDialogDismissed(action)) return
    throw action
  }
  await post(`/shipping/schedules/${route.params.id}/cancel`, { reason: value })
  ElMessage.success(t('shipping.cancelled'))
  await load()
}

onMounted(load)
</script>
<style scoped>
.page-head{display:flex;align-items:flex-end;justify-content:space-between;margin-bottom:16px}.page-head h2{display:inline-block;margin:0 18px 0 8px}.page-head>div{display:flex;align-items:center;gap:10px}
.route-head,.node-title{display:flex;align-items:center;justify-content:space-between}.route-head{margin-bottom:18px}.route-timeline{max-width:900px}.route-node.temporary{border-color:var(--el-color-warning)}.route-node.completed{opacity:.72;background:var(--el-fill-color-lighter)}.route-node.current{border-color:var(--el-color-primary);box-shadow:0 0 0 1px var(--el-color-primary-light-7)}
.node-title>div,.node-actions{display:flex;align-items:center;gap:8px}.completed-toggle{display:flex;align-items:center;gap:8px;margin:0 0 16px 28px;color:var(--el-text-color-secondary)}
.times{display:grid;grid-template-columns:repeat(2,minmax(220px,1fr));gap:8px;margin:12px 0;color:var(--el-text-color-regular)}.muted,.reason{color:var(--el-text-color-secondary)}.reason{margin-top:5px}.danger-text{color:var(--el-color-danger);font-weight:600}.warning-text{color:var(--el-color-warning);font-weight:600}.success-text{color:var(--el-color-success);font-weight:600}
@media(max-width:700px){.times{grid-template-columns:1fr}.page-head{align-items:flex-start}.page-head>div:last-child{flex-wrap:wrap;justify-content:flex-end}}
</style>
