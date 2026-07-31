<template>
  <div>
    <div class="page-head">
      <h2>{{ t('outbounds.title') }}</h2>
      <span class="head-note">{{ t('outbounds.subtitle') }}</span>
    </div>

    <el-card shadow="never">
      <el-radio-group v-model="tab" class="tabs" @change="reload">
        <el-radio-button value="pending">{{ t('outbounds.tabPending') }}</el-radio-button>
        <el-radio-button value="orders">{{ t('outbounds.tabOrders') }}</el-radio-button>
      </el-radio-group>

      <!-- Contracts with goods still owed. This is where a shortage is stated
           before anybody tries to ship and gets refused. -->
      <template v-if="tab === 'pending'">
        <div class="filters">
          <el-input
            v-model="keyword"
            :placeholder="t('outbounds.searchContract')"
            clearable
            style="width: 260px"
            @keyup.enter="reload"
            @clear="reload"
          />
          <el-button @click="reload">{{ t('common.query') }}</el-button>
        </div>

        <el-table :data="shippable" v-loading="loading">
          <el-table-column :label="t('outbounds.contract')" min-width="190">
            <template #default="{ row }">
              <div class="prod">{{ row.contractNo }}</div>
              <div class="sub">{{ row.customerName }}</div>
            </template>
          </el-table-column>
          <el-table-column :label="t('outbounds.deliveryDate')" width="120">
            <template #default="{ row }">
              <span :class="{ overdue: isOverdue(row.deliveryDate) }">{{ row.deliveryDate || '—' }}</span>
            </template>
          </el-table-column>
          <el-table-column :label="t('outbounds.sold')" width="100" align="right">
            <template #default="{ row }"><span class="num">{{ trim(row.demandQty) }}</span></template>
          </el-table-column>
          <el-table-column :label="t('outbounds.ready')" width="100" align="right">
            <template #default="{ row }">
              <span class="num ready" :class="{ none: isZero(row.readyQty) }">{{ trim(row.readyQty) }}</span>
            </template>
          </el-table-column>
          <el-table-column :label="t('outbounds.shipped')" width="100" align="right">
            <template #default="{ row }">
              <span class="num" :class="{ dim: isZero(row.shippedQty) }">{{ trim(row.shippedQty) }}</span>
            </template>
          </el-table-column>
          <!-- The number that decides whether this contract can go out whole.
               Anything above zero has to be bought first. -->
          <el-table-column :label="t('outbounds.shortage')" width="140" align="right">
            <template #default="{ row }">
              <template v-if="isZero(row.shortageQty)">
                <el-tag size="small" type="success" effect="plain">{{ t('outbounds.stockOk') }}</el-tag>
              </template>
              <template v-else>
                <span class="num short">{{ trim(row.shortageQty) }}</span>
                <div class="sub short-note">{{ t('outbounds.needPurchase') }}</div>
              </template>
            </template>
          </el-table-column>
          <el-table-column v-if="canWrite" :label="t('common.actions')" width="110" fixed="right">
            <template #default="{ row }">
              <el-button link type="primary" :disabled="isZero(row.readyQty)" @click="openCreate(row)">
                {{ t('outbounds.pick') }}
              </el-button>
            </template>
          </el-table-column>
          <template #empty>{{ t('outbounds.noPending') }}</template>
        </el-table>
      </template>

      <template v-else>
        <div class="filters">
          <el-select v-model="status" style="width: 150px" @change="reload">
            <el-option value="" :label="t('outbounds.allStatuses')" />
            <el-option value="DRAFT" :label="t('outbounds.statuses.DRAFT')" />
            <el-option value="CONFIRMED" :label="t('outbounds.statuses.CONFIRMED')" />
            <el-option value="CANCELLED" :label="t('outbounds.statuses.CANCELLED')" />
          </el-select>
          <el-input
            v-model="keyword"
            :placeholder="t('outbounds.searchOutbound')"
            clearable
            style="width: 240px"
            @keyup.enter="reload"
            @clear="reload"
          />
          <el-button @click="reload">{{ t('common.query') }}</el-button>
        </div>

        <el-table :data="outbounds" v-loading="loading">
          <el-table-column :label="t('outbounds.outboundNo')" width="160">
            <template #default="{ row }">
              <div class="prod">{{ row.outboundNo }}</div>
              <div class="sub">{{ formatTime(row.createdAt) }}</div>
            </template>
          </el-table-column>
          <el-table-column :label="t('outbounds.contract')" min-width="180">
            <template #default="{ row }">
              <router-link :to="`/contracts?id=${row.contractId}`" class="doc-link">
                {{ row.contractNo }}
              </router-link>
              <div class="sub">{{ row.customerName }}</div>
            </template>
          </el-table-column>
          <el-table-column :label="t('outbounds.qty')" width="130" align="right">
            <template #default="{ row }">
              <span class="num">{{ trim(row.totalQty) }}</span>
              <div class="sub">{{ t('outbounds.lines', { n: row.itemCount }) }}</div>
            </template>
          </el-table-column>
          <el-table-column :label="t('outbounds.operator')" width="110">
            <template #default="{ row }">{{ row.operatorName || '—' }}</template>
          </el-table-column>
          <el-table-column :label="t('common.status')" width="170">
            <template #default="{ row }">
              <el-tag size="small" :type="statusType(row.status)" effect="plain">
                {{ t(`outbounds.statuses.${row.status}`) }}
              </el-tag>
              <div v-if="row.cancelledReason" class="sub reason">{{ row.cancelledReason }}</div>
            </template>
          </el-table-column>
          <el-table-column :label="t('common.actions')" width="180" fixed="right">
            <template #default="{ row }">
              <el-button link type="primary" @click="openDetail(row)">{{ t('common.detail') }}</el-button>
              <template v-if="canWrite && row.status === 'DRAFT'">
                <el-button link type="success" @click="confirm(row)">{{ t('outbounds.confirm') }}</el-button>
                <el-button link type="danger" @click="openCancel(row)">{{ t('common.cancel') }}</el-button>
              </template>
            </template>
          </el-table-column>
          <template #empty>{{ t('outbounds.noOutbounds') }}</template>
        </el-table>
      </template>

      <el-pagination
        class="pager"
        layout="total, prev, pager, next"
        :total="total"
        :page-size="pageSize"
        :current-page="page"
        @current-change="(p: number) => { page = p; load() }"
      />
    </el-card>

    <el-dialog v-model="createOpen" :title="t('outbounds.pickFor', { no: picking?.contractNo })" width="860px">
      <el-alert v-if="anyShort" type="warning" :closable="false" show-icon class="alert">
        {{ t('outbounds.shortWarning') }}
      </el-alert>
      <el-table :data="lines" size="small">
        <el-table-column :label="t('outbounds.product')" min-width="180">
          <template #default="{ row }">
            <div>{{ row.productName }}</div>
            <div class="sub">{{ row.productCode }}</div>
          </template>
        </el-table-column>
        <el-table-column :label="t('outbounds.sold')" width="90" align="right">
          <template #default="{ row }"><span class="num">{{ trim(row.demandQty) }}</span></template>
        </el-table-column>
        <el-table-column :label="t('outbounds.shippedShort')" width="90" align="right">
          <template #default="{ row }">
            <span class="num" :class="{ dim: isZero(row.shippedQty) }">{{ trim(row.shippedQty) }}</span>
          </template>
        </el-table-column>
        <el-table-column :label="t('outbounds.ready')" width="90" align="right">
          <template #default="{ row }">
            <span class="num ready" :class="{ none: isZero(row.readyQty) }">{{ trim(row.readyQty) }}</span>
          </template>
        </el-table-column>
        <el-table-column :label="t('outbounds.shortage')" width="150" align="right">
          <template #default="{ row }">
            <template v-if="isZero(row.shortageQty)">—</template>
            <template v-else>
              <span class="num short">{{ trim(row.shortageQty) }}</span>
              <!-- Free stock of the same item that arrived after the contract
                   was signed. Saying so beats a flat refusal: it tells the
                   picker the missing goods are on the shelf and will be
                   attached automatically. -->
              <div v-if="!isZero(row.availableQty)" class="sub arrived">
                {{ t('outbounds.arrived', { n: trim(row.availableQty) }) }}
              </div>
              <div v-else class="sub short-note">{{ t('outbounds.needPurchase') }}</div>
            </template>
          </template>
        </el-table-column>
        <el-table-column :label="t('outbounds.pickQty')" width="150">
          <template #default="{ row }">
            <el-input v-model="qtyOf[row.contractItemId]" size="small" :disabled="maxOf(row) === 0" />
          </template>
        </el-table-column>
      </el-table>
      <el-form label-width="70px" class="remark-form">
        <el-form-item :label="t('outbounds.remark')">
          <el-input v-model="createRemark" type="textarea" :rows="2" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="createOpen = false">{{ t('common.cancel') }}</el-button>
        <el-button type="primary" :loading="saving" @click="submitCreate">
          {{ t('outbounds.createOutbound') }}
        </el-button>
      </template>
    </el-dialog>

    <el-dialog v-model="detailOpen" :title="detail?.outboundNo" width="700px">
      <el-descriptions :column="2" border size="small" class="desc">
        <el-descriptions-item :label="t('outbounds.contract')">{{ detail?.contractNo }}</el-descriptions-item>
        <el-descriptions-item :label="t('outbounds.customer')">{{ detail?.customerName }}</el-descriptions-item>
        <el-descriptions-item :label="t('common.status')">
          {{ detail ? t(`outbounds.statuses.${detail.status}`) : '' }}
        </el-descriptions-item>
        <el-descriptions-item :label="t('outbounds.operator')">{{ detail?.operatorName || '—' }}</el-descriptions-item>
        <el-descriptions-item :label="t('outbounds.remark')" :span="2">{{ detail?.remark || '—' }}</el-descriptions-item>
      </el-descriptions>
      <el-table :data="detailItems" size="small">
        <el-table-column :label="t('outbounds.product')" min-width="200">
          <template #default="{ row }">
            <div>{{ row.productName }}</div>
            <div class="sub">{{ row.productCode }}</div>
          </template>
        </el-table-column>
        <el-table-column :label="t('outbounds.qty')" width="140" align="right">
          <template #default="{ row }"><span class="num">{{ trim(row.qty) }}</span> {{ row.uomCode }}</template>
        </el-table-column>
      </el-table>
    </el-dialog>

    <el-dialog v-model="cancelOpen" :title="t('outbounds.cancelTitle')" width="460px">
      <el-alert type="warning" :closable="false" show-icon class="alert">
        {{ t('outbounds.cancelWarning') }}
      </el-alert>
      <el-input v-model="cancelReason" type="textarea" :rows="3" :placeholder="t('outbounds.cancelReason')" />
      <template #footer>
        <el-button @click="cancelOpen = false">{{ t('common.cancel') }}</el-button>
        <el-button type="danger" :loading="saving" @click="submitCancel">{{ t('common.confirm') }}</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { useI18n } from 'vue-i18n'
import { get, post } from '../api'
import { useAuthStore } from '../stores/auth'

interface Shippable {
  contractId: string
  contractNo: string
  customerName: string
  deliveryDate: string
  demandQty: string
  readyQty: string
  shortageQty: string
  lockedQty: string
  shippedQty: string
}
interface ShippableLine {
  contractItemId: string
  productCode: string
  productName: string
  uomCode: string
  demandQty: string
  reservedQty: string
  lockedQty: string
  shippedQty: string
  shortageQty: string
  readyQty: string
  availableQty: string
}
interface Outbound {
  id: string
  outboundNo: string
  contractId: string
  contractNo: string
  customerName: string
  status: string
  operatorName: string
  remark: string
  cancelledReason: string
  itemCount: number
  totalQty: string
  createdAt: string
}
interface OutboundItem {
  id: string
  productCode: string
  productName: string
  uomCode: string
  qty: string
}

const { t } = useI18n()
const auth = useAuthStore()
const canWrite = auth.can('inventory:stock:write')

const tab = ref('pending')
const shippable = ref<Shippable[]>([])
const outbounds = ref<Outbound[]>([])
const total = ref(0)
const page = ref(1)
const pageSize = 20
const keyword = ref('')
const status = ref('')
const loading = ref(false)
const saving = ref(false)

const createOpen = ref(false)
const picking = ref<Shippable | null>(null)
const lines = ref<ShippableLine[]>([])
const qtyOf = reactive<Record<string, string>>({})
const createRemark = ref('')

const detailOpen = ref(false)
const detail = ref<Outbound | null>(null)
const detailItems = ref<OutboundItem[]>([])

const cancelOpen = ref(false)
const cancelling = ref<Outbound | null>(null)
const cancelReason = ref('')

const anyShort = computed(() => lines.value.some((l) => !isZero(l.shortageQty)))

async function load() {
  loading.value = true
  try {
    if (tab.value === 'pending') {
      const d = await get<{ contracts: Shippable[]; meta: { total: number } }>('/shippable', {
        page: page.value, page_size: pageSize, keyword: keyword.value,
      })
      shippable.value = d.contracts ?? []
      total.value = Number(d.meta?.total ?? 0)
    } else {
      const d = await get<{ outbounds: Outbound[]; meta: { total: number } }>('/outbounds', {
        page: page.value, page_size: pageSize, status: status.value, keyword: keyword.value,
      })
      outbounds.value = d.outbounds ?? []
      total.value = Number(d.meta?.total ?? 0)
    }
  } finally {
    loading.value = false
  }
}

function reload() {
  page.value = 1
  load()
}

async function openCreate(row: Shippable) {
  picking.value = row
  createRemark.value = ''
  lines.value = (await get<{ lines: ShippableLine[] }>(`/shippable/${row.contractId}/lines`)).lines ?? []
  Object.keys(qtyOf).forEach((k) => delete qtyOf[k])
  // Pre-fill with everything that can actually go. A picker who wants less
  // edits down; nobody has to type the number the system already knows.
  lines.value.forEach((l) => {
    qtyOf[l.contractItemId] = maxOf(l) > 0 ? trim(l.readyQty) : ''
  })
  createOpen.value = true
}

// What this line could ship right now. The server decides for real — including
// pulling in stock that arrived since — but the form should not invite a
// number that is obviously impossible.
function maxOf(l: ShippableLine): number {
  return Number(l.readyQty ?? 0) + Number(l.availableQty ?? 0)
}

async function submitCreate() {
  const payload = lines.value
    .filter((l) => Number(qtyOf[l.contractItemId] ?? 0) > 0)
    .map((l) => ({ contract_item_id: Number(l.contractItemId), qty: qtyOf[l.contractItemId] }))
  if (!payload.length) {
    ElMessage.warning(t('outbounds.pickSomething'))
    return
  }
  saving.value = true
  try {
    const res = await post<{ outboundNo: string }>('/outbounds', {
      contract_id: Number(picking.value?.contractId),
      outbound_type: 'SALES',
      remark: createRemark.value,
      lines: payload,
    })
    ElMessage.success(t('outbounds.created', { no: res.outboundNo }))
    createOpen.value = false
    tab.value = 'orders'
    status.value = 'DRAFT'
    reload()
  } finally {
    saving.value = false
  }
}

async function openDetail(row: Outbound) {
  detail.value = row
  detailItems.value = (await get<{ items: OutboundItem[] }>(`/outbounds/${row.id}/items`)).items ?? []
  detailOpen.value = true
}

async function confirm(row: Outbound) {
  // Confirming moves real goods and cannot be undone from here; a return is a
  // different document with different paperwork.
  await ElMessageBox.confirm(t('outbounds.confirmWarning', { no: row.outboundNo }), t('outbounds.confirm'), {
    type: 'warning',
    confirmButtonText: t('outbounds.confirm'),
    cancelButtonText: t('common.cancel'),
  })
  await post(`/outbounds/${row.id}/confirm`, {})
  ElMessage.success(t('outbounds.confirmed'))
  load()
}

function openCancel(row: Outbound) {
  cancelling.value = row
  cancelReason.value = ''
  cancelOpen.value = true
}

async function submitCancel() {
  if (!cancelReason.value.trim()) {
    ElMessage.warning(t('outbounds.cancelReasonRequired'))
    return
  }
  saving.value = true
  try {
    await post(`/outbounds/${cancelling.value?.id}/cancel`, { reason: cancelReason.value })
    ElMessage.success(t('outbounds.cancelled'))
    cancelOpen.value = false
    load()
  } finally {
    saving.value = false
  }
}

function statusType(s: string): 'warning' | 'success' | 'info' {
  if (s === 'DRAFT') return 'warning'
  if (s === 'CONFIRMED') return 'success'
  return 'info'
}

function trim(v: string): string {
  if (!v) return '0'
  if (!v.includes('.')) return v
  return v.replace(/0+$/, '').replace(/\.$/, '')
}

function isZero(v: string): boolean {
  return Number(v ?? 0) === 0
}

function isOverdue(d: string): boolean {
  return !!d && d < new Date().toISOString().slice(0, 10)
}

function formatTime(v: string): string {
  return v ? v.replace('T', ' ').slice(0, 16) : '—'
}

onMounted(load)
</script>

<style scoped>
.page-head {
  display: flex;
  align-items: baseline;
  gap: 14px;
  margin-bottom: 16px;
}
.page-head h2 {
  margin: 0;
  font-size: 20px;
}
.head-note,
.sub {
  font-size: 12px;
  color: var(--el-text-color-secondary);
}
.tabs {
  margin-bottom: 14px;
}
.filters {
  display: flex;
  gap: 10px;
  margin-bottom: 14px;
}
.prod {
  font-weight: 500;
}
.num {
  font-variant-numeric: tabular-nums;
}
.dim {
  color: var(--el-text-color-placeholder);
}
.ready {
  font-weight: 600;
  color: var(--el-color-success);
}
.ready.none {
  color: var(--el-text-color-placeholder);
  font-weight: 400;
}
.short {
  font-weight: 600;
  color: var(--el-color-danger);
}
.short-note {
  color: var(--el-color-danger);
  margin-top: 2px;
}
.arrived {
  color: var(--el-color-success);
  margin-top: 2px;
}
.overdue {
  color: var(--el-color-danger);
  font-weight: 600;
}
.reason {
  margin-top: 2px;
  line-height: 1.4;
}
.doc-link {
  color: var(--el-color-primary);
  text-decoration: none;
}
.doc-link:hover {
  text-decoration: underline;
}
.alert {
  margin-bottom: 14px;
}
.desc {
  margin-bottom: 14px;
}
.remark-form {
  margin-top: 14px;
}
.pager {
  margin-top: 14px;
  justify-content: flex-end;
}
</style>
