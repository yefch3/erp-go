<template>
  <div class="page">
    <header class="page-head">
      <div>
        <div class="eyebrow">{{ t('platform.eyebrow') }}</div>
        <h1>{{ t('platform.title') }}</h1>
        <p>{{ t('platform.subtitle') }}</p>
      </div>
      <el-button type="primary" @click="openCreate">{{ t('platform.create') }}</el-button>
    </header>

    <section class="panel">
      <el-table v-loading="loading" :data="rows">
        <el-table-column :label="t('platform.company')" min-width="180">
          <template #default="{ row }">
            <div>{{ row.name }}</div>
            <div class="sub">#{{ row.id }} · {{ row.createdAt || '—' }}</div>
          </template>
        </el-table-column>
        <el-table-column :label="t('platform.admin')" min-width="200">
          <template #default="{ row }">
            <div>{{ row.adminEmail || '—' }}</div>
            <el-tag v-if="row.adminActivated" type="success" effect="plain" size="small">
              {{ t('platform.activated') }}
            </el-tag>
            <el-tag v-else type="warning" effect="plain" size="small">
              {{ t('platform.pending') }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column :label="t('platform.status')" width="110">
          <template #default="{ row }">
            <el-tag :type="row.status === 'ACTIVE' ? 'success' : 'danger'" effect="plain">
              {{ row.status === 'ACTIVE' ? t('platform.active') : t('platform.suspended') }}
            </el-tag>
          </template>
        </el-table-column>
        <!-- 智能转换是唯一按次花我们钱的功能，所以每家公司用了多少、上限
             多少，摆在开户表上，不藏在别的页里。 -->
        <el-table-column :label="t('platform.quota')" width="150">
          <template #default="{ row }">
            <template v-if="quotaOf(row.id).limited">
              <div class="num">
                {{ quotaOf(row.id).usedThisMonth }} / {{ quotaOf(row.id).monthlyRuns }}
              </div>
              <div class="sub">{{ quotaPercentOf(row.id) }}%</div>
            </template>
            <template v-else>
              <div class="sub">{{ t('platform.quotaNone') }}</div>
              <div class="sub">{{ t('platform.quotaUsed', { n: quotaOf(row.id).usedThisMonth }) }}</div>
            </template>
          </template>
        </el-table-column>
        <!-- 本月成本。只在这一页——客户看的是「还能转几次」，我们看的是
             「这个月花了多少」。没配单价就说没配，不给一个凭空的 0：一个
             猜出来的成本比没有成本更坏，因为它看着像账。 -->
        <el-table-column :label="t('platform.cost')" width="150" align="right">
          <template #default="{ row }">
            <div v-if="quotaOf(row.id).estimatedCost" class="num money">
              {{ quotaOf(row.id).currency }} {{ quotaOf(row.id).estimatedCost }}
            </div>
            <div v-else class="sub">{{ t('platform.costNoPrice') }}</div>
            <div class="sub">
              {{ t('platform.costTokens', { n: formatTokens(quotaOf(row.id).inputTokens + quotaOf(row.id).outputTokens) }) }}
            </div>
          </template>
        </el-table-column>
        <el-table-column :label="t('platform.actions')" width="300" align="right">
          <template #default="{ row }">
            <!-- 没激活才有「重发邀请」：给激活过的人重发不是邀请，是改密码，
                 后端会拒绝，这里干脆不给按钮。 -->
            <el-button
              v-if="!row.adminActivated"
              size="small"
              :loading="busyId === row.id"
              @click="reinvite(row)"
            >
              {{ t('platform.reinvite') }}
            </el-button>
            <el-button size="small" plain @click="openQuota(row)">
              {{ t('platform.quotaEdit') }}
            </el-button>
            <el-button
              v-if="row.status === 'ACTIVE'"
              size="small"
              type="danger"
              plain
              :loading="busyId === row.id"
              @click="setStatus(row, 'SUSPENDED')"
            >
              {{ t('platform.suspend') }}
            </el-button>
            <el-button
              v-else
              size="small"
              type="success"
              plain
              :loading="busyId === row.id"
              @click="setStatus(row, 'ACTIVE')"
            >
              {{ t('platform.resume') }}
            </el-button>
          </template>
        </el-table-column>
        <template #empty>{{ t('platform.empty') }}</template>
      </el-table>
    </section>

    <section class="panel">
      <header class="panel-head">
        <div>
          <h2>{{ t('platform.dlTitle') }}</h2>
          <p class="sub">{{ t('platform.dlSubtitle') }}</p>
        </div>
        <el-button size="small" :loading="dlLoading" @click="loadFailed">
          {{ t('platform.dlRefresh') }}
        </el-button>
      </header>
      <el-table v-loading="dlLoading" :data="failedEvents" size="small">
        <el-table-column :label="t('platform.dlService')" width="120" prop="service" />
        <el-table-column :label="t('platform.dlEvent')" min-width="170">
          <template #default="{ row }">
            <div>{{ row.eventType || '—' }}</div>
            <div class="sub">{{ row.aggregateId }}</div>
          </template>
        </el-table-column>
        <el-table-column :label="t('platform.dlTenant')" width="90" prop="tenantId" />
        <el-table-column :label="t('platform.dlReason')" min-width="260">
          <template #default="{ row }">
            <span class="reason">{{ row.reason }}</span>
          </template>
        </el-table-column>
        <el-table-column :label="t('platform.dlParkedAt')" width="170" prop="parkedAt" />
        <el-table-column :label="t('platform.actions')" width="110" align="right">
          <template #default="{ row }">
            <el-button
              v-if="row.id"
              size="small"
              type="primary"
              plain
              :loading="dlBusy === row.service + row.id"
              @click="replay(row)"
            >
              {{ t('platform.dlReplay') }}
            </el-button>
          </template>
        </el-table-column>
        <!-- 空列表是这一页的正常状态，值得说出来：没有事件被丢下。 -->
        <template #empty>{{ t('platform.dlEmpty') }}</template>
      </el-table>
    </section>

    <el-dialog v-model="createOpen" :title="t('platform.createTitle')" width="min(460px, 92vw)">
      <p class="hint">{{ t('platform.createHint') }}</p>
      <el-form label-position="top">
        <el-form-item :label="t('platform.company')" required>
          <el-input v-model="form.name" :placeholder="t('platform.companyPlaceholder')" />
        </el-form-item>
        <el-form-item :label="t('platform.adminEmail')" required>
          <el-input v-model="form.adminEmail" placeholder="admin@example.com" />
        </el-form-item>
      </el-form>
      <!-- 公共邮箱的公司没有「域名防手滑」那层网——地址填错，激活的就是外人。
           这句提醒放在提交按钮眼前，而不是文档里。 -->
      <el-alert type="warning" :closable="false" show-icon :title="t('platform.typoWarning')" />
      <template #footer>
        <el-button @click="createOpen = false">{{ t('common.cancel') }}</el-button>
        <el-button
          type="primary"
          :loading="creating"
          :disabled="!form.name.trim() || !form.adminEmail.includes('@')"
          @click="create"
        >
          {{ t('platform.createConfirm') }}
        </el-button>
      </template>
    </el-dialog>

    <el-dialog v-model="quotaOpen" :title="t('platform.quotaTitle')" width="min(460px, 92vw)">
      <p class="hint">{{ t('platform.quotaHint', { name: quotaForm.name }) }}</p>
      <el-form label-position="top">
        <el-form-item :label="t('platform.quotaLimited')">
          <el-switch v-model="quotaForm.limited" />
        </el-form-item>
        <el-form-item v-if="quotaForm.limited" :label="t('platform.quotaRuns')">
          <el-input-number v-model="quotaForm.monthlyRuns" :min="0" :step="10" style="width: 180px" />
        </el-form-item>
      </el-form>
      <!-- 0 和「不限」正好相反，而这正是最容易点错的一处：把开关关掉是放开，
           把数字填 0 是彻底关停。所以两种情况各说一句。 -->
      <el-alert
        :type="quotaForm.limited && quotaForm.monthlyRuns === 0 ? 'error' : 'info'"
        :closable="false"
        show-icon
        :title="quotaForm.limited
          ? (quotaForm.monthlyRuns === 0 ? t('platform.quotaZeroWarning') : t('platform.quotaSetNote'))
          : t('platform.quotaOffNote')"
      />
      <template #footer>
        <el-button @click="quotaOpen = false">{{ t('common.cancel') }}</el-button>
        <el-button type="primary" :loading="quotaSaving" @click="saveQuota">
          {{ t('common.save') }}
        </el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup lang="ts">
import { onMounted, reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { ElMessage, ElMessageBox } from 'element-plus'
import { get, post } from '../api'
import { emptyExcelQuota, excelQuotaPercent, type ExcelQuota } from '../lib/excelQuota'

// 平台开户（E7）：给客户公司的第一位管理员发邀请，对方激活后自己邀请员工。
//
// 这一页的守卫在服务端（platform_operators 名单）——菜单只是探测着亮的，
// 直接输网址进来的非操作员会在第一个请求上收到 403。
const { t } = useI18n()

interface Tenant {
  id: string
  name: string
  status: string
  createdAt: string
  adminEmail: string
  adminActivated: boolean
}

const rows = ref<Tenant[]>([])
const loading = ref(false)
const createOpen = ref(false)
const creating = ref(false)
const busyId = ref('')
const form = reactive({ name: '', adminEmail: '' })

// 死信面：三个消费事件的服务里被放弃的事件，汇成一张表。重放是同步的——
// 失败的原因当场弹给正看着屏幕的人。
interface FailedEvent {
  service: string
  id: number
  tenantId: number
  consumerGroup: string
  eventType: string
  aggregateId: string
  reason: string
  parkedAt: string
}
const failedEvents = ref<FailedEvent[]>([])
const dlLoading = ref(false)
const dlBusy = ref('')

async function loadFailed() {
  dlLoading.value = true
  try {
    failedEvents.value = (await get<{ events: FailedEvent[] }>('/platform/failed-events')).events ?? []
  } finally {
    dlLoading.value = false
  }
}

async function replay(row: FailedEvent) {
  dlBusy.value = row.service + row.id
  try {
    await post('/platform/failed-events/replay', {
      service: row.service,
      consumer_group: row.consumerGroup,
      id: row.id,
    })
    ElMessage.success(t('platform.dlReplayed'))
    await loadFailed()
  } finally {
    dlBusy.value = ''
  }
}

// 智能转换的额度：平台给每家客户公司定的每月转换次数上限。
//
// 为什么归这一页管：每一次转换都是我们付给模型厂的钱，定上限的必须是我们。
// 让用钱的一方自己填，那不叫上限，叫偏好设置。
//
// limited=false 是不限；limited=true 且 monthlyRuns=0 是一次都不许用。这
// 两件事正好相反，所以界面上是一个开关加一个数字，不是「填 0 表示不限」。
interface TenantQuota extends ExcelQuota {
  tenantId: number
  // 成本这几项只有这条平台专用路由会返回；客户那一侧的用量接口不给金额。
  inputTokens: number
  outputTokens: number
  // 空串表示没配单价。空和 0 是两回事，所以别在这里补默认值。
  estimatedCost: string
  currency: string
}
const quotas = ref<Record<string, TenantQuota>>({})
const quotaOpen = ref(false)
const quotaSaving = ref(false)
const quotaForm = reactive({ id: '', name: '', limited: false, monthlyRuns: 200 })

// 这一页只关心「用了多少 / 上限多少」，月份由列表本身声明，所以借用同一套
// 百分比算法（含上限 0 的处理），不在这里重写一遍。
const noQuota: TenantQuota = {
  ...emptyExcelQuota, tenantId: 0,
  inputTokens: 0, outputTokens: 0, estimatedCost: '', currency: '',
}

function formatTokens(n: number): string {
  return Number(n || 0).toLocaleString()
}
function quotaOf(id: string): TenantQuota {
  return quotas.value[id] ?? noQuota
}
function quotaPercentOf(id: string): number {
  return excelQuotaPercent(quotaOf(id))
}

async function loadQuotas() {
  const d = await get<{ quotas: TenantQuota[] }>('/platform/excel-quotas')
  const byID: Record<string, TenantQuota> = {}
  for (const q of d.quotas ?? []) byID[String(q.tenantId)] = q
  quotas.value = byID
}

function openQuota(row: Tenant) {
  const current = quotaOf(row.id)
  quotaForm.id = row.id
  quotaForm.name = row.name
  quotaForm.limited = current.limited
  // 没设过就给一个明显是「起点」的数字，而不是 0——0 的意思是停用。
  quotaForm.monthlyRuns = current.limited ? current.monthlyRuns : 200
  quotaOpen.value = true
}

async function saveQuota() {
  quotaSaving.value = true
  try {
    await post('/platform/excel-quotas', {
      tenantId: Number(quotaForm.id),
      // 不带这个字段就是恢复不限。带 0 是「一次都不许用」——两者靠有没有
      // 这个键区分，不靠值。
      monthlyRuns: quotaForm.limited ? quotaForm.monthlyRuns : null,
    })
    quotaOpen.value = false
    ElMessage.success(t('platform.quotaSaved'))
    await loadQuotas()
  } finally {
    quotaSaving.value = false
  }
}

async function load() {
  loading.value = true
  try {
    const d = await get<{ tenants: Tenant[] }>('/platform/tenants')
    rows.value = d.tenants ?? []
  } finally {
    loading.value = false
  }
}

function openCreate() {
  form.name = ''
  form.adminEmail = ''
  createOpen.value = true
}

async function create() {
  creating.value = true
  try {
    const d = await post<{ mailSent: boolean; mailError?: string }>('/platform/tenants', {
      name: form.name.trim(),
      adminEmail: form.adminEmail.trim(),
    })
    createOpen.value = false
    // 开户和发信是两步：公司一定开出来了，信不一定寄出去了。混成一句
    // 「成功」会让操作员以为对方已经在等邮件——分开说，失败给出下一步。
    if (d.mailSent) {
      ElMessage.success(t('platform.created'))
    } else {
      ElMessage.warning(t('platform.createdMailFailed', { reason: d.mailError || '' }))
    }
    await load()
  } finally {
    creating.value = false
  }
}

async function reinvite(row: Tenant) {
  busyId.value = row.id
  try {
    const d = await post<{ mailSent: boolean; mailError?: string }>(`/platform/tenants/${row.id}/reinvite`, {})
    if (d.mailSent) {
      ElMessage.success(t('platform.reinvited', { email: row.adminEmail }))
    } else {
      ElMessage.warning(t('platform.createdMailFailed', { reason: d.mailError || '' }))
    }
  } finally {
    busyId.value = ''
  }
}

async function setStatus(row: Tenant, status: 'ACTIVE' | 'SUSPENDED') {
  if (status === 'SUSPENDED') {
    // 停用是把整家公司的人挡在登录外面——值得一次确认，不值得更多。
    try {
      await ElMessageBox.confirm(t('platform.suspendConfirm', { name: row.name }), { type: 'warning' })
    } catch {
      return
    }
  }
  busyId.value = row.id
  try {
    await post(`/platform/tenants/${row.id}/status`, { status })
    ElMessage.success(status === 'SUSPENDED' ? t('platform.suspended') : t('platform.resumed'))
    await load()
  } finally {
    busyId.value = ''
  }
}

onMounted(() => {
  load()
  loadQuotas()
  loadFailed()
})
</script>

<style scoped>
.page {
  display: flex;
  flex-direction: column;
  gap: 18px;
}
.page-head {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 16px;
  flex-wrap: wrap;
}
.page-head .eyebrow {
  font-size: 12px;
  letter-spacing: 1.5px;
  color: var(--el-text-color-secondary);
}
.page-head h1 {
  margin: 4px 0 6px;
  font-size: 28px;
}
.page-head p {
  margin: 0;
  color: var(--el-text-color-regular);
}
.panel {
  padding: 16px;
  border: 1px solid var(--el-border-color-lighter);
  border-radius: 10px;
  background: var(--el-bg-color);
}
.sub {
  font-size: 12px;
  color: var(--el-text-color-secondary);
}
.num {
  font-variant-numeric: tabular-nums;
}
.num.money {
  font-weight: 600;
}
.hint {
  margin: 0 0 14px;
  color: var(--el-text-color-secondary);
  font-size: 13px;
}
</style>
