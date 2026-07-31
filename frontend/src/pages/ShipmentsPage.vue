<template>
  <div>
    <div class="page-head">
      <h2>{{ t('shipments.title') }}</h2>
      <span class="head-note">{{ t('shipments.subtitle') }}</span>
      <span class="grow" />
      <el-button v-if="canWrite" type="primary" @click="openCreate">{{ t('shipments.create') }}</el-button>
    </div>

    <el-card shadow="never">
      <el-radio-group v-model="status" class="tabs" @change="reload">
        <el-radio-button value="">{{ t('shipments.allStatuses') }}</el-radio-button>
        <el-radio-button value="DRAFT">{{ t('shipments.statuses.DRAFT') }}</el-radio-button>
        <el-radio-button value="SHIPPED">{{ t('shipments.statuses.SHIPPED') }}</el-radio-button>
        <el-radio-button value="ARRIVED">{{ t('shipments.statuses.ARRIVED') }}</el-radio-button>
        <el-radio-button value="CANCELLED">{{ t('shipments.statuses.CANCELLED') }}</el-radio-button>
      </el-radio-group>

      <div class="filters">
        <el-input
          v-model="keyword"
          :placeholder="t('shipments.searchPlaceholder')"
          clearable
          style="width: 300px"
          @keyup.enter="reload"
          @clear="reload"
        />
        <el-button @click="reload">{{ common('query') }}</el-button>
      </div>

      <el-table :data="rows" v-loading="loading">
        <!-- Status rides with the document number rather than taking a column
             of its own: seven columns do not fit, and "SH-0001, sailed" is how
             people say it anyway. -->
        <el-table-column :label="t('shipments.shipmentNo')" width="150">
          <template #default="{ row }">
            <div class="prod">{{ row.shipmentNo }}</div>
            <el-tag size="small" :type="statusType(row.status)" effect="plain">
              {{ t(`shipments.statuses.${row.status}`) }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column :label="t('shipments.vessel')" min-width="140">
          <template #default="{ row }">
            <div>{{ row.vesselName || '—' }}</div>
            <div class="sub">{{ row.voyageNo ? t('shipments.voyageShort', { v: row.voyageNo }) : '' }}</div>
          </template>
        </el-table-column>
        <el-table-column :label="t('shipments.blNo')" min-width="150">
          <template #default="{ row }">
            <div>{{ row.blNo || '—' }}</div>
            <div class="sub ellipsis" :title="row.containerNo">{{ row.containerNo }}</div>
          </template>
        </el-table-column>
        <el-table-column :label="t('shipments.route')" min-width="130">
          <template #default="{ row }">
            <div>{{ row.portOfDischarge || '—' }}</div>
            <div class="sub">{{ dateRange(row) }}</div>
          </template>
        </el-table-column>
        <!-- The consolidation is the point of this page, so the number of
             contracts on a box is table-level information, not detail-level. -->
        <el-table-column :label="t('shipments.cargo')" min-width="150">
          <template #default="{ row }">
            <div class="ellipsis" :title="row.contractNos">{{ row.contractNos || '—' }}</div>
            <div class="sub">
              {{ t('shipments.cargoSummary', { c: row.contractCount, l: row.lineCount }) }}
            </div>
          </template>
        </el-table-column>
        <el-table-column :label="common('actions')" width="230" fixed="right">
          <template #default="{ row }">
            <el-button link type="primary" @click="openDetail(row)">{{ common('detail') }}</el-button>
            <template v-if="canWrite">
              <el-button v-if="row.status === 'DRAFT'" link type="primary" @click="openEdit(row)">
                {{ common('edit') }}
              </el-button>
              <el-button v-if="row.status === 'DRAFT'" link type="success" @click="confirmSailing(row)">
                {{ t('shipments.confirm') }}
              </el-button>
              <el-button v-if="row.status === 'SHIPPED'" link type="success" @click="markArrived(row)">
                {{ t('shipments.arrive') }}
              </el-button>
              <el-button
                v-if="row.status === 'DRAFT' || row.status === 'SHIPPED'"
                link
                type="danger"
                @click="cancel(row)"
              >
                {{ t('shipments.void') }}
              </el-button>
            </template>
          </template>
        </el-table-column>
        <template #empty>{{ t('shipments.empty') }}</template>
      </el-table>

      <el-pagination
        class="pager"
        layout="total, prev, pager, next"
        :total="total"
        :page-size="pageSize"
        :current-page="page"
        @current-change="(p: number) => { page = p; load() }"
      />
    </el-card>

    <el-dialog
      v-model="formOpen"
      :title="editing ? t('shipments.editTitle', { no: editing.shipmentNo }) : t('shipments.create')"
      width="1000px"
      top="5vh"
    >
      <el-alert type="info" :closable="false" show-icon class="alert">
        {{ t('shipments.createHint') }}
      </el-alert>

      <el-form :model="form" label-width="90px" class="head-form">
        <el-row :gutter="12">
          <el-col :span="8">
            <el-form-item :label="t('shipments.vesselName')">
              <el-input v-model="form.vesselName" :placeholder="t('shipments.vesselPlaceholder')" />
            </el-form-item>
          </el-col>
          <el-col :span="8">
            <el-form-item :label="t('shipments.voyageNo')">
              <el-input v-model="form.voyageNo" />
            </el-form-item>
          </el-col>
          <el-col :span="8">
            <el-form-item :label="t('shipments.portOfDischarge')">
              <el-input v-model="form.portOfDischarge" />
            </el-form-item>
          </el-col>
        </el-row>
        <el-row :gutter="12">
          <el-col :span="8">
            <el-form-item :label="t('shipments.blNo')">
              <el-input v-model="form.blNo" />
            </el-form-item>
          </el-col>
          <el-col :span="8">
            <el-form-item :label="t('shipments.etd')">
              <el-date-picker v-model="form.etd" type="date" value-format="YYYY-MM-DD" style="width: 100%" />
            </el-form-item>
          </el-col>
          <el-col :span="8">
            <el-form-item :label="t('shipments.eta')">
              <el-date-picker v-model="form.eta" type="date" value-format="YYYY-MM-DD" style="width: 100%" />
            </el-form-item>
          </el-col>
        </el-row>
        <el-form-item :label="t('shipments.containerNo')">
          <el-input v-model="form.containerNo" :placeholder="t('shipments.containerPlaceholder')" />
        </el-form-item>
        <el-form-item :label="t('shipments.remark')">
          <el-input v-model="form.remark" type="textarea" :rows="2" />
        </el-form-item>
      </el-form>

      <div class="side-title">{{ t('shipments.pickCargo') }}</div>
      <div class="picker-row">
        <el-select
          v-model="pickContractId"
          filterable
          clearable
          :placeholder="t('shipments.pickContract')"
          style="width: 380px"
          @change="loadContractLines"
        >
          <el-option
            v-for="c in contracts"
            :key="c.id"
            :value="Number(c.id)"
            :label="`${c.contractNo} · ${c.customerName}`"
          />
        </el-select>
        <span class="sub">{{ t('shipments.pickHint') }}</span>
      </div>

      <el-table v-if="pickLines.length" :data="pickLines" size="small" class="pick-table">
        <el-table-column :label="t('shipments.product')" min-width="180">
          <template #default="{ row }">
            <div>{{ row.productName }}</div>
            <div class="sub">{{ row.spec }}</div>
          </template>
        </el-table-column>
        <el-table-column :label="t('shipments.contracted')" width="110" align="right">
          <template #default="{ row }"><span class="num">{{ trim(row.qty) }}</span></template>
        </el-table-column>
        <el-table-column :label="t('shipments.alreadyShipped')" width="110" align="right">
          <template #default="{ row }"><span class="num dim">{{ trim(row.shippedQty) }}</span></template>
        </el-table-column>
        <el-table-column :label="t('shipments.remaining')" width="110" align="right">
          <template #default="{ row }">
            <span class="num" :class="{ over: Number(row.remainingQty) < 0 }">
              {{ trim(row.remainingQty) }}
            </span>
          </template>
        </el-table-column>
        <el-table-column :label="t('shipments.thisTime')" width="150">
          <template #default="{ row }">
            <el-input v-model="pickQty[row.id]" size="small" :placeholder="trim(row.remainingQty)" />
          </template>
        </el-table-column>
      </el-table>
      <div v-if="pickLines.length" class="pick-actions">
        <el-button size="small" @click="fillRemaining">{{ t('shipments.fillRemaining') }}</el-button>
        <el-button size="small" type="primary" @click="addPicked">{{ t('shipments.addToShipment') }}</el-button>
      </div>

      <div class="side-title">{{ t('shipments.manifest', { n: lines.length }) }}</div>
      <el-table :data="lines" size="small">
        <el-table-column :label="t('shipments.contract')" min-width="150">
          <template #default="{ row }">
            <div>{{ row.contractNo }}</div>
            <div class="sub">{{ row.customerName }}</div>
          </template>
        </el-table-column>
        <el-table-column :label="t('shipments.product')" min-width="180">
          <template #default="{ row }">
            <div>{{ row.productName }}</div>
            <div class="sub">{{ row.spec }}</div>
          </template>
        </el-table-column>
        <el-table-column :label="t('shipments.qty')" width="130" align="right">
          <template #default="{ row }">
            <span class="num money">{{ trim(row.qty) }}</span>
            <span class="uom">{{ row.uomCode }}</span>
          </template>
        </el-table-column>
        <el-table-column width="70" align="center">
          <template #default="{ $index }">
            <el-button link type="danger" @click="lines.splice($index, 1)">{{ common('delete') }}</el-button>
          </template>
        </el-table-column>
        <template #empty>{{ t('shipments.manifestEmpty') }}</template>
      </el-table>

      <template #footer>
        <el-button @click="formOpen = false">{{ common('cancel') }}</el-button>
        <el-button type="primary" :loading="saving" @click="save">{{ common('save') }}</el-button>
      </template>
    </el-dialog>

    <el-drawer v-model="detailOpen" :title="detail?.shipmentNo" size="620px">
      <template v-if="detail">
        <el-descriptions :column="2" border size="small" class="desc">
          <el-descriptions-item :label="t('shipments.vesselName')">{{ detail.vesselName || '—' }}</el-descriptions-item>
          <el-descriptions-item :label="t('shipments.voyageNo')">{{ detail.voyageNo || '—' }}</el-descriptions-item>
          <el-descriptions-item :label="t('shipments.blNo')">{{ detail.blNo || '—' }}</el-descriptions-item>
          <el-descriptions-item :label="t('shipments.portOfDischarge')">{{ detail.portOfDischarge || '—' }}</el-descriptions-item>
          <el-descriptions-item :label="t('shipments.etd')">{{ detail.etd || '—' }}</el-descriptions-item>
          <el-descriptions-item :label="t('shipments.eta')">{{ detail.eta || '—' }}</el-descriptions-item>
          <el-descriptions-item :label="t('shipments.containerNo')" :span="2">
            {{ detail.containerNo || '—' }}
          </el-descriptions-item>
          <el-descriptions-item :label="common('status')">
            <el-tag size="small" :type="statusType(detail.status)" effect="plain">
              {{ t(`shipments.statuses.${detail.status}`) }}
            </el-tag>
          </el-descriptions-item>
          <el-descriptions-item :label="t('shipments.createdBy')">{{ detail.createdByName || '—' }}</el-descriptions-item>
          <el-descriptions-item v-if="detail.remark" :label="t('shipments.remark')" :span="2">
            {{ detail.remark }}
          </el-descriptions-item>
        </el-descriptions>

        <div class="side-title">{{ t('shipments.manifest', { n: detailItems.length }) }}</div>
        <el-table :data="detailItems" size="small">
          <el-table-column :label="t('shipments.contract')" min-width="140">
            <template #default="{ row }">
              <div>{{ row.contractNo }}</div>
              <div class="sub">{{ row.customerName }}</div>
            </template>
          </el-table-column>
          <el-table-column :label="t('shipments.product')" min-width="160">
            <template #default="{ row }">
              <div>{{ row.productName }}</div>
              <div class="sub">{{ row.spec }}</div>
            </template>
          </el-table-column>
          <el-table-column :label="t('shipments.qty')" width="120" align="right">
            <template #default="{ row }">
              <span class="num money">{{ trim(row.qty) }}</span>
              <span class="sub"> {{ row.uomCode }}</span>
            </template>
          </el-table-column>
        </el-table>
      </template>
    </el-drawer>
  </div>
</template>

<script setup lang="ts">
import { onMounted, onUnmounted, reactive, ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { useI18n } from 'vue-i18n'
import { get, post, put } from '../api'
import { onLive } from '../live'
import { useAuthStore } from '../stores/auth'

interface Shipment {
  id: string
  shipmentNo: string
  vesselName: string
  voyageNo: string
  blNo: string
  containerNo: string
  portOfDischarge: string
  etd: string
  eta: string
  status: string
  remark: string
  createdByName: string
  createdAt: string
  lineCount: number
  contractCount: number
  contractNos: string
}
interface ShipmentItem {
  id: string
  contractId: string
  contractNo: string
  customerName: string
  contractItemId: string
  productId: string
  productName: string
  spec: string
  qty: string
  uomCode: string
}
interface ContractRow { id: string; contractNo: string; customerName: string; status: string }
// A contract line with its shipping progress folded in, which is what the
// person loading a box actually needs: not what was sold, but what is left.
interface PickLine {
  id: string
  productId: string
  productName: string
  spec: string
  qty: string
  uomCode: string
  shippedQty: string
  remainingQty: string
}

const { t } = useI18n()
const auth = useAuthStore()
const canWrite = auth.can('export:shipment:write')

const rows = ref<Shipment[]>([])
const total = ref(0)
const page = ref(1)
const pageSize = 20
const status = ref('')
const keyword = ref('')
const loading = ref(false)
const saving = ref(false)

const formOpen = ref(false)
const editing = ref<Shipment | null>(null)
const form = reactive({
  vesselName: '', voyageNo: '', blNo: '', containerNo: '',
  portOfDischarge: '', etd: '', eta: '', remark: '',
})
const lines = ref<ShipmentItem[]>([])

const contracts = ref<ContractRow[]>([])
const pickContractId = ref<number | undefined>(undefined)
const pickLines = ref<PickLine[]>([])
const pickQty = reactive<Record<string, string>>({})

const detailOpen = ref(false)
const detail = ref<Shipment | null>(null)
const detailItems = ref<ShipmentItem[]>([])

const common = (k: string) => t(`common.${k}`)

function trim(v: string | number | undefined): string {
  const n = Number(v ?? 0)
  return Number.isInteger(n) ? String(n) : String(Number(n.toFixed(4)))
}

function dateRange(r: Shipment): string {
  if (!r.etd && !r.eta) return ''
  return `${r.etd || '—'} → ${r.eta || '—'}`
}

function statusType(s: string): 'info' | 'warning' | 'success' | 'danger' {
  if (s === 'SHIPPED') return 'warning'
  if (s === 'ARRIVED') return 'success'
  if (s === 'CANCELLED') return 'danger'
  return 'info'
}

async function load() {
  loading.value = true
  try {
    const d = await get<{ shipments: Shipment[]; total: string }>('/shipments', {
      page: page.value, page_size: pageSize, status: status.value, keyword: keyword.value,
    })
    rows.value = d.shipments ?? []
    total.value = Number(d.total ?? 0)
  } finally {
    loading.value = false
  }
}

function reload() {
  page.value = 1
  load()
}

function resetForm() {
  form.vesselName = ''
  form.voyageNo = ''
  form.blNo = ''
  form.containerNo = ''
  form.portOfDischarge = ''
  form.etd = ''
  form.eta = ''
  form.remark = ''
  lines.value = []
  pickContractId.value = undefined
  pickLines.value = []
  Object.keys(pickQty).forEach((k) => delete pickQty[k])
}

async function loadContracts() {
  // Only what can actually be loaded onto a boat. A draft contract has
  // nothing agreed to ship against, and offering it in the picker would only
  // produce a refusal three clicks later.
  const [effective, executing] = await Promise.all([
    get<{ contracts: ContractRow[] }>('/contracts', { status: 'EFFECTIVE', page_size: 200 }),
    get<{ contracts: ContractRow[] }>('/contracts', { status: 'EXECUTING', page_size: 200 }),
  ])
  contracts.value = [...(effective.contracts ?? []), ...(executing.contracts ?? [])]
}

async function openCreate() {
  resetForm()
  editing.value = null
  await loadContracts()
  formOpen.value = true
}

async function openEdit(row: Shipment) {
  resetForm()
  const d = await get<{ shipment: Shipment; items: ShipmentItem[] }>(`/shipments/${row.id}`)
  editing.value = d.shipment
  Object.assign(form, {
    vesselName: d.shipment.vesselName, voyageNo: d.shipment.voyageNo,
    blNo: d.shipment.blNo, containerNo: d.shipment.containerNo,
    portOfDischarge: d.shipment.portOfDischarge, etd: d.shipment.etd,
    eta: d.shipment.eta, remark: d.shipment.remark,
  })
  lines.value = d.items ?? []
  await loadContracts()
  formOpen.value = true
}

// Products, not contract lines.
//
// A contract can list the same product twice — different price, different
// remark, a line added in a later version. The server checks over-shipment by
// product, summing those lines, so a per-line picker would show the product's
// whole remainder against each of them: 700 left, shown twice, and "load all
// remaining" would offer to ship 1400. Grouping here the same way the server
// groups there is the only version of this that cannot mislead.
async function loadContractLines() {
  pickLines.value = []
  Object.keys(pickQty).forEach((k) => delete pickQty[k])
  if (!pickContractId.value) return
  const d = await get<{
    items: { id: string; productId: string; skuId: string; spec: string }[]
    shipments: {
      productId: string; skuId: string; productName: string; uomCode: string
      qty: string; shippedQty: string; remainingQty: string
    }[]
  }>(`/contracts/${pickContractId.value}`)

  // Any one of a product's lines identifies it for the ledger, which records
  // the product; the first is chosen so repeated edits stay stable.
  const key = (productId: string, skuId?: string) => `${productId}:${skuId ?? '0'}`
  const firstLineOf = new Map<string, { id: string; spec: string }>()
  for (const i of d.items ?? []) {
    const k = key(i.productId, i.skuId)
    if (!firstLineOf.has(k)) firstLineOf.set(k, { id: i.id, spec: i.spec })
  }
  pickLines.value = (d.shipments ?? []).flatMap((p) => {
    const line = firstLineOf.get(key(p.productId, p.skuId))
    if (!line) return []
    return [{
      id: line.id, productId: p.productId, productName: p.productName,
      spec: line.spec, qty: p.qty, uomCode: p.uomCode,
      shippedQty: p.shippedQty, remainingQty: p.remainingQty,
    }]
  })
}

function fillRemaining() {
  pickLines.value.forEach((l) => {
    const left = Number(l.remainingQty)
    if (left > 0) pickQty[l.id] = String(left)
  })
}

function addPicked() {
  const contract = contracts.value.find((c) => Number(c.id) === pickContractId.value)
  if (!contract) return
  let added = 0
  for (const l of pickLines.value) {
    const qty = Number(pickQty[l.id] ?? 0)
    if (qty <= 0) continue
    // One line per contract line, matching the server's uniqueness rule:
    // adding the same line twice means the operator meant to change it.
    const existing = lines.value.find(
      (x) => x.contractId === contract.id && x.contractItemId === l.id,
    )
    if (existing) {
      existing.qty = String(qty)
    } else {
      lines.value.push({
        id: '', contractId: contract.id, contractNo: contract.contractNo,
        customerName: contract.customerName, contractItemId: l.id,
        productId: l.productId, productName: l.productName, spec: l.spec,
        qty: String(qty), uomCode: l.uomCode,
      })
    }
    added += 1
  }
  if (!added) {
    ElMessage.warning(t('shipments.nothingPicked'))
    return
  }
  ElMessage.success(t('shipments.added', { n: added }))
  pickContractId.value = undefined
  pickLines.value = []
  Object.keys(pickQty).forEach((k) => delete pickQty[k])
}

async function save() {
  if (!lines.value.length) {
    ElMessage.warning(t('shipments.manifestRequired'))
    return
  }
  const body = {
    shipment: {
      vessel_name: form.vesselName, voyage_no: form.voyageNo, bl_no: form.blNo,
      container_no: form.containerNo, port_of_discharge: form.portOfDischarge,
      etd: form.etd || '', eta: form.eta || '', remark: form.remark,
      items: lines.value.map((l) => ({
        contract_id: Number(l.contractId),
        contract_item_id: Number(l.contractItemId),
        qty: l.qty,
      })),
    },
  }
  saving.value = true
  try {
    if (editing.value) {
      await put(`/shipments/${editing.value.id}`, body)
      ElMessage.success(t('shipments.saved'))
    } else {
      const res = await post<{ shipment: Shipment }>('/shipments', body)
      ElMessage.success(t('shipments.created', { no: res.shipment.shipmentNo }))
    }
    formOpen.value = false
    reload()
  } finally {
    saving.value = false
  }
}

async function openDetail(row: Shipment) {
  const d = await get<{ shipment: Shipment; items: ShipmentItem[] }>(`/shipments/${row.id}`)
  detail.value = d.shipment
  detailItems.value = d.items ?? []
  detailOpen.value = true
}

async function confirmSailing(row: Shipment) {
  await ElMessageBox.confirm(
    t('shipments.confirmWarning', { no: row.shipmentNo }),
    t('shipments.confirm'),
    { type: 'warning', confirmButtonText: t('shipments.confirm'), cancelButtonText: common('cancel') },
  )
  await post(`/shipments/${row.id}/confirm`, {})
  ElMessage.success(t('shipments.confirmed'))
  load()
}

async function markArrived(row: Shipment) {
  await post(`/shipments/${row.id}/arrive`, {})
  ElMessage.success(t('shipments.arrived'))
  load()
}

async function cancel(row: Shipment) {
  await ElMessageBox.confirm(
    row.status === 'SHIPPED'
      ? t('shipments.voidShippedWarning', { no: row.shipmentNo })
      : t('shipments.voidWarning', { no: row.shipmentNo }),
    t('shipments.void'),
    { type: 'warning', confirmButtonText: t('shipments.void'), cancelButtonText: common('cancel') },
  )
  await post(`/shipments/${row.id}/cancel`, {})
  ElMessage.success(t('shipments.voided'))
  load()
}

let stop: (() => void) | undefined
onMounted(() => {
  load()
  stop = onLive((e) => {
    if (e.subject === 'SHIPMENT') load()
  })
})
onUnmounted(() => stop?.())
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
.grow {
  flex: 1;
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
.money {
  font-weight: 600;
}
.dim {
  color: var(--el-text-color-placeholder);
}
.uom {
  margin-left: 4px;
  font-size: 12px;
  color: var(--el-text-color-secondary);
}
.over {
  color: var(--el-color-danger);
  font-weight: 600;
}
.ellipsis {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.alert {
  margin-bottom: 14px;
}
.head-form {
  margin-bottom: 6px;
}
.side-title {
  margin: 14px 0 8px;
  font-size: 13px;
  font-weight: 600;
  color: var(--el-text-color-regular);
}
.picker-row {
  display: flex;
  align-items: center;
  gap: 12px;
  margin-bottom: 10px;
}
.pick-table {
  margin-bottom: 8px;
}
.pick-actions {
  display: flex;
  justify-content: flex-end;
  gap: 8px;
}
.desc {
  margin-bottom: 14px;
}
.pager {
  margin-top: 14px;
  justify-content: flex-end;
}
</style>
