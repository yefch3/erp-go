<template>
  <div>
    <div class="page-head">
      <h2>{{ t('flows.title') }}</h2>
    </div>

    <el-alert :title="t('flows.versionHint')" type="info" :closable="false" show-icon class="alert" />

    <div class="layout">
      <el-card shadow="never" class="side">
        <div class="side-title">{{ t('flows.bizTypes') }}</div>
        <div
          v-for="b in BIZ_TYPES"
          :key="b"
          class="biz"
          :class="{ on: bizType === b }"
          @click="select(b)"
        >
          <div class="biz-name">{{ t(`flows.biz.${b}`) }}</div>
          <div class="sub">{{ activeOf(b) ? t('flows.activeVersion', { v: activeOf(b)!.version }) : t('flows.none') }}</div>
        </div>
      </el-card>

      <el-card shadow="never" class="main" v-loading="loading">
        <template v-if="bizType">
          <div class="main-head">
            <div>
              <span class="flow-name">{{ t(`flows.biz.${bizType}`) }}</span>
              <el-tag v-if="active" size="small" effect="plain" class="chip">
                {{ t('flows.activeVersion', { v: active.version }) }}
              </el-tag>
              <span v-if="active && active.runningCount > 0" class="sub">
                {{ t('flows.running', { n: active.runningCount }) }}
              </span>
            </div>
            <el-button v-if="canWrite" type="primary" :loading="saving" @click="save">
              {{ t('flows.saveAsNew') }}
            </el-button>
          </div>

          <!-- One flow per amount band. A band is its floor: the next band's
               floor is where it ends, so gaps and overlaps cannot happen. -->
          <div class="bands">
            <span class="bands-label">{{ t('flows.bands') }}</span>
            <el-radio-group v-model="band" @change="loadBand">
              <el-radio-button v-for="b in bands" :key="b.min" :value="b.min">
                {{ bandLabel(b.min) }}
              </el-radio-button>
            </el-radio-group>
            <el-button v-if="canWrite" link type="primary" @click="addBandOpen = true">
              {{ t('flows.addBand') }}
            </el-button>
            <el-button v-if="canWrite && band !== '0'" link type="danger" @click="removeBand">
              {{ t('flows.removeBand') }}
            </el-button>
          </div>
          <p class="band-hint">{{ t('flows.bandHint') }}</p>

          <el-table :data="nodes" size="small">
            <el-table-column :label="t('flows.seq')" width="55" align="center">
              <template #default="{ $index }">{{ $index + 1 }}</template>
            </el-table-column>
            <el-table-column :label="t('flows.nodeName')" min-width="150">
              <template #default="{ row }">
                <el-input v-model="row.name" :disabled="!canWrite" />
              </template>
            </el-table-column>
            <el-table-column :label="t('flows.approverType')" width="130">
              <template #default="{ row }">
                <el-select v-model="row.approverType" :disabled="!canWrite" style="width: 100%"
                           @change="() => (row.approverRef = row.approverType === 'MANAGER' ? '1' : '')">
                  <el-option v-for="k in APPROVER_TYPES" :key="k" :value="k" :label="t(`flows.types.${k}`)" />
                </el-select>
              </template>
            </el-table-column>
            <el-table-column :label="t('flows.approver')" min-width="180">
              <template #default="{ row }">
                <el-select v-if="row.approverType === 'ROLE'" v-model="row.approverRef"
                           :disabled="!canWrite" filterable style="width: 100%">
                  <el-option v-for="r in roles" :key="r.id" :value="r.id" :label="r.name" />
                </el-select>
                <el-select v-else-if="row.approverType === 'EMPLOYEE'" v-model="row.approverRef"
                           :disabled="!canWrite" filterable style="width: 100%">
                  <el-option v-for="e in employees" :key="e.id" :value="e.id" :label="`${e.code} · ${e.name}`" />
                </el-select>
                <el-select v-else v-model="row.approverRef" :disabled="!canWrite" style="width: 100%">
                  <el-option v-for="l in [1, 2, 3, 4]" :key="l" :value="String(l)"
                             :label="t('flows.levelsUp', { n: l })" />
                </el-select>
              </template>
            </el-table-column>
            <el-table-column :label="t('flows.approveMode')" width="120">
              <template #default="{ row }">
                <el-select v-model="row.approveMode" :disabled="!canWrite" style="width: 100%">
                  <el-option v-for="m in APPROVE_MODES" :key="m" :value="m" :label="t(`flows.modes.${m}`)" />
                </el-select>
              </template>
            </el-table-column>
            <el-table-column v-if="canWrite" width="120" align="right">
              <template #default="{ $index }">
                <el-button link :disabled="$index === 0" @click="move($index, -1)">↑</el-button>
                <el-button link :disabled="$index === nodes.length - 1" @click="move($index, 1)">↓</el-button>
                <el-button link type="danger" @click="nodes.splice($index, 1)">{{ t('common.delete') }}</el-button>
              </template>
            </el-table-column>
            <template #empty>{{ t('flows.noNodes') }}</template>
          </el-table>

          <div v-if="canWrite" class="foot">
            <el-button @click="addNode">{{ t('flows.addNode') }}</el-button>
            <span class="hint">{{ t('flows.managerHint') }}</span>
          </div>

          <el-divider content-position="left">
            {{ t('flows.history') }}
            <span class="hint">{{ t('flows.historyHint') }}</span>
          </el-divider>
          <el-table :data="versionsOfBand()" size="small">
            <el-table-column :label="t('flows.version')" width="80">
              <template #default="{ row }">v{{ row.version }}</template>
            </el-table-column>
            <el-table-column :label="t('common.status')" width="90">
              <template #default="{ row }">
                <el-tag size="small" :type="row.status === 'ACTIVE' ? 'success' : 'info'" effect="plain">
                  {{ t(`flows.status.${row.status}`) }}
                </el-tag>
              </template>
            </el-table-column>
            <el-table-column :label="t('flows.nodeCount')" width="90" align="center">
              <template #default="{ row }">{{ row.nodeCount }}</template>
            </el-table-column>
            <el-table-column :label="t('flows.runningCount')" width="110" align="center">
              <template #default="{ row }">
                <span :class="{ 'has-running': row.runningCount > 0 }">{{ row.runningCount }}</span>
              </template>
            </el-table-column>
            <el-table-column :label="t('flows.createdAt')" min-width="140">
              <template #default="{ row }">{{ formatTime(row.createdAt) }}</template>
            </el-table-column>
            <el-table-column :label="t('flows.createdBy')" width="100">
              <template #default="{ row }">{{ employeeName(row.createdBy) }}</template>
            </el-table-column>
          </el-table>
        </template>
        <el-empty v-else :description="t('flows.pickBiz')" />
      </el-card>
    </div>

    <el-dialog v-model="addBandOpen" :title="t('flows.addBand')" width="440px">
      <el-form label-width="110px">
        <el-form-item :label="t('flows.bandFloor')" required>
          <el-input v-model="newBandFloor" placeholder="100000" />
          <div class="hint">{{ t('flows.bandFloorHint') }}</div>
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="addBandOpen = false">{{ t('common.cancel') }}</el-button>
        <el-button type="primary" @click="addBand">{{ t('common.confirm') }}</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { useI18n } from 'vue-i18n'
import { del, get, post } from '../api'
import { useAuthStore } from '../stores/auth'

interface Definition {
  id: string
  bizType: string
  name: string
  version: number
  status: string
  createdAt: string
  nodeCount: number
  runningCount: number
  minAmount: string
  createdBy: string
}
interface Node {
  name: string
  approverType: string
  // A string because every int64 arrives as one; the level for MANAGER is
  // carried in the same field, which is what the server expects.
  approverRef: string
  approveMode: string
}
interface Role { id: string; name: string }
interface Employee { id: string; code: string; name: string }

// Document types that have a flow. Extend as services gain approvals.
const BIZ_TYPES = ['CONTRACT', 'PURCHASE_ORDER']
const APPROVER_TYPES = ['MANAGER', 'ROLE', 'EMPLOYEE']
const APPROVE_MODES = ['ANY', 'ALL']

const { t } = useI18n()
const auth = useAuthStore()
const canWrite = auth.can('approval:flow:write')

const definitions = ref<Definition[]>([])
const roles = ref<Role[]>([])
const employees = ref<Employee[]>([])
const bizType = ref('')
const band = ref('0')
const nodes = ref<Node[]>([])
const addBandOpen = ref(false)
const newBandFloor = ref('')
const employees2 = ref<Record<string, string>>({})
const loading = ref(false)
const saving = ref(false)

// The flow shown is the active version of the SELECTED band, not just of the
// document type: a type can have several live flows, one per amount band.
const active = computed(() => activeOf(bizType.value, band.value))

// Every band that exists for this type, lowest floor first. Floor 0 always
// exists, because something has to match an amount of zero.
const bands = computed(() => {
  const floors = new Set<string>(['0'])
  for (const d of definitions.value) {
    if (d.bizType === bizType.value && d.status === 'ACTIVE') floors.add(normalise(d.minAmount))
  }
  return [...floors].sort((a, b) => Number(a) - Number(b)).map((min) => ({ min }))
})

function normalise(v: string): string {
  return String(Number(v ?? 0))
}

function activeOf(biz: string, floor = '0'): Definition | undefined {
  return definitions.value.find(
    (d) => d.bizType === biz && d.status === 'ACTIVE' && normalise(d.minAmount) === normalise(floor),
  )
}

function versionsOfBand(): Definition[] {
  return definitions.value.filter(
    (d) => d.bizType === bizType.value && normalise(d.minAmount) === normalise(band.value),
  )
}

// "0 起" for the base band, "100,000 起" above it. The ceiling is the next
// band's floor, so it is never written down and can never disagree.
function bandLabel(min: string): string {
  const n = Number(min)
  return n === 0 ? t('flows.baseBand') : t('flows.fromAmount', { n: n.toLocaleString() })
}

function employeeName(id: string): string {
  if (!id || id === '0') return '—'
  return employees2.value[id] ?? `#${id}`
}

async function loadDefinitions() {
  definitions.value = (await get<{ definitions: Definition[] }>('/approval-flows')).definitions ?? []
}

async function select(biz: string) {
  bizType.value = biz
  band.value = '0'
  await loadBand()
}

async function loadBand() {
  nodes.value = []
  const def = activeOf(bizType.value, band.value)
  if (!def) return
  loading.value = true
  try {
    const data = await get<{ nodes: Node[] }>(`/approval-flows/${def.id}`)
    nodes.value = (data.nodes ?? []).map((n) => ({ ...n }))
  } finally {
    loading.value = false
  }
}

// A new band starts as a copy of the one below it: an admin adding a
// high-value tier almost always wants the existing steps plus one, not a
// blank sheet they must rebuild from memory.
function addBand() {
  const floor = normalise(newBandFloor.value)
  if (!newBandFloor.value.trim() || Number(floor) <= 0) {
    ElMessage.warning(t('flows.bandFloorInvalid'))
    return
  }
  if (bands.value.some((b) => b.min === floor)) {
    ElMessage.warning(t('flows.bandExists'))
    return
  }
  band.value = floor
  addBandOpen.value = false
  newBandFloor.value = ''
  ElMessage.info(t('flows.bandDraft'))
}

async function removeBand() {
  await ElMessageBox.confirm(t('flows.removeBandConfirm'), t('flows.removeBand'), { type: 'warning' })
  await del(`/approval-flows/band?biz_type=${bizType.value}&min_amount=${band.value}`)
  ElMessage.success(t('flows.bandRemoved'))
  await loadDefinitions()
  band.value = '0'
  await loadBand()
}

function addNode() {
  nodes.value.push({ name: '', approverType: 'MANAGER', approverRef: '1', approveMode: 'ANY' })
}

function move(index: number, delta: number) {
  const next = index + delta
  const copy = [...nodes.value]
  ;[copy[index], copy[next]] = [copy[next], copy[index]]
  nodes.value = copy
}

async function save() {
  if (nodes.value.length === 0) {
    ElMessage.warning(t('flows.needNode'))
    return
  }
  const blank = nodes.value.find((n) => !n.name.trim())
  if (blank) {
    ElMessage.warning(t('flows.needName'))
    return
  }
  await ElMessageBox.confirm(t('flows.saveConfirm'), t('flows.saveAsNew'), { type: 'warning' })
  saving.value = true
  try {
    await post('/approval-flows', {
      bizType: bizType.value,
      minAmount: band.value,
      name: active.value?.name ?? '',
      nodes: nodes.value.map((n) => ({
        name: n.name,
        approverType: n.approverType,
        // MANAGER carries a level here; the others carry a row id.
        approverRef: n.approverRef || '0',
        approveMode: n.approveMode,
      })),
    })
    ElMessage.success(t('flows.saved'))
    await loadDefinitions()
    await loadBand()
  } finally {
    saving.value = false
  }
}

function formatTime(iso: string): string {
  return iso ? iso.replace('T', ' ').slice(0, 16) : ''
}

onMounted(async () => {
  await loadDefinitions()
  if (auth.can('iam:role:read')) {
    roles.value = (await get<{ roles: Role[] }>('/roles')).roles ?? []
  }
  if (auth.can('iam:employee:read')) {
    employees.value = (await get<{ employees: Employee[] }>('/employees', { page_size: 200 })).employees ?? []
    employees2.value = Object.fromEntries(employees.value.map((e) => [e.id, e.name]))
  }
  await select(BIZ_TYPES[0])
})
</script>

<style scoped>
.bands {
  display: flex;
  align-items: center;
  gap: 10px;
  margin-bottom: 6px;
  flex-wrap: wrap;
}
.bands-label {
  font-size: 13px;
  color: var(--el-text-color-regular);
}
.band-hint {
  margin: 0 0 14px;
  font-size: 12px;
  color: var(--el-text-color-secondary);
}

.page-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin-bottom: 16px;
}
.page-head h2 {
  font-size: 18px;
  font-weight: 500;
  margin: 0;
}
.alert {
  margin-bottom: 14px;
}
.layout {
  display: grid;
  grid-template-columns: 220px 1fr;
  gap: 14px;
  align-items: start;
}
.side-title {
  font-size: 13px;
  color: var(--el-text-color-secondary);
  margin-bottom: 10px;
}
.biz {
  padding: 8px 10px;
  border-radius: 6px;
  cursor: pointer;
}
.biz:hover {
  background: var(--el-fill-color-light);
}
.biz.on {
  background: var(--el-color-primary-light-9);
}
.biz-name {
  font-size: 14px;
}
.main-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin-bottom: 14px;
}
.flow-name {
  font-size: 15px;
}
.chip {
  margin-left: 8px;
}
.foot {
  margin-top: 10px;
}
.sub {
  color: var(--el-text-color-secondary);
  font-size: 12px;
}
.hint {
  margin-left: 8px;
  font-size: 12px;
  color: var(--el-text-color-secondary);
}
.has-running {
  color: var(--el-color-warning);
}
</style>
