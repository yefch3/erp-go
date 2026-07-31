<template>
  <div>
    <div class="page-head">
      <h2>{{ t('stocks.title') }}</h2>
      <el-button v-if="canWrite" type="primary" @click="openReceive">{{ t('stocks.receive') }}</el-button>
    </div>

    <el-card shadow="never">
      <el-radio-group v-model="tab" class="tabs" @change="reload">
        <el-radio-button value="stock">{{ t('stocks.tabStock') }}</el-radio-button>
        <el-radio-button value="ledger">{{ t('stocks.tabLedger') }}</el-radio-button>
      </el-radio-group>

      <template v-if="tab === 'stock'">
        <div class="filters">
          <el-select v-model="warehouseId" style="width: 170px" @change="reload">
            <el-option :value="0" :label="t('stocks.allWarehouses')" />
            <el-option v-for="w in warehouses" :key="w.id" :value="Number(w.id)" :label="w.name" />
          </el-select>
          <el-input
            v-model="keyword"
            :placeholder="t('stocks.searchPlaceholder')"
            clearable
            style="width: 220px"
            @keyup.enter="reload"
            @clear="reload"
          />
          <el-checkbox v-model="inStockOnly" @change="reload">{{ t('stocks.inStockOnly') }}</el-checkbox>
          <el-button @click="reload">{{ t('common.query') }}</el-button>
        </div>

        <el-alert type="info" :closable="false" class="legend">
          <span class="lg"><b>{{ t('stocks.onHand') }}</b>{{ t('stocks.onHandHint') }}</span>
          <span class="lg"><b>{{ t('stocks.reserved') }}</b>{{ t('stocks.reservedHint') }}</span>
          <span class="lg"><b>{{ t('stocks.locked') }}</b>{{ t('stocks.lockedHint') }}</span>
          <span class="lg"><b>{{ t('stocks.available') }}</b>{{ t('stocks.availableHint') }}</span>
        </el-alert>

        <el-table :data="stocks" v-loading="loading">
          <el-table-column :label="t('stocks.product')" min-width="200">
            <template #default="{ row }">
              <div class="prod">{{ row.productName }}</div>
              <div class="sub">{{ row.productCode }}</div>
            </template>
          </el-table-column>
          <el-table-column :label="t('stocks.warehouse')" width="110">
            <template #default="{ row }">{{ row.warehouseName }}</template>
          </el-table-column>
          <el-table-column :label="t('stocks.onHand')" width="110" align="right">
            <template #default="{ row }"><span class="num">{{ trim(row.onHandQty) }}</span></template>
          </el-table-column>
          <el-table-column :label="t('stocks.reserved')" width="110" align="right">
            <template #default="{ row }">
              <span class="num" :class="{ dim: isZero(row.reservedQty) }">{{ trim(row.reservedQty) }}</span>
            </template>
          </el-table-column>
          <el-table-column :label="t('stocks.locked')" width="110" align="right">
            <template #default="{ row }">
              <span class="num" :class="{ dim: isZero(row.lockedQty) }">{{ trim(row.lockedQty) }}</span>
            </template>
          </el-table-column>
          <!-- The only number that answers "can I promise this to somebody",
               so it is the one that carries weight and colour. -->
          <el-table-column :label="t('stocks.available')" width="120" align="right">
            <template #default="{ row }">
              <span class="num avail" :class="{ none: isZero(row.availableQty) }">
                {{ trim(row.availableQty) }}
              </span>
              <span class="uom">{{ row.uomCode }}</span>
            </template>
          </el-table-column>
          <!-- Moving weighted average, and what the shelf is worth at it.
               This is the number a shipment is costed at. -->
          <el-table-column :label="t('stocks.avgCost')" width="130" align="right">
            <template #default="{ row }">
              <span class="num" :class="{ dim: isZero(row.avgCost) }">{{ money(row.avgCost) }}</span>
              <div class="sub">{{ row.costCurrency }}</div>
            </template>
          </el-table-column>
          <el-table-column :label="t('stocks.stockValue')" width="140" align="right">
            <template #default="{ row }">
              <span class="num" :class="{ dim: isZero(row.totalCost) }">{{ money(row.totalCost, 2) }}</span>
            </template>
          </el-table-column>
          <template #empty>{{ t('stocks.empty') }}</template>
        </el-table>
      </template>

      <template v-else>
        <div class="filters">
          <el-select v-model="movement" clearable :placeholder="t('stocks.allMovements')" style="width: 190px" @change="reload">
            <el-option v-for="m in MOVEMENTS" :key="m" :value="m" :label="t(`stocks.movements.${m}`)" />
          </el-select>
          <el-button @click="reload">{{ t('common.query') }}</el-button>
        </div>
        <el-table :data="ledger" v-loading="loading">
          <el-table-column :label="t('stocks.occurredAt')" width="140">
            <template #default="{ row }">{{ formatTime(row.occurredAt) }}</template>
          </el-table-column>
          <el-table-column :label="t('stocks.product')" min-width="180">
            <template #default="{ row }">{{ row.productName }}</template>
          </el-table-column>
          <el-table-column :label="t('stocks.movement')" width="110">
            <template #default="{ row }">
              <el-tag size="small" :type="movementType(row.movement)" effect="plain">
                {{ t(`stocks.movements.${row.movement}`) }}
              </el-tag>
            </template>
          </el-table-column>
          <el-table-column :label="t('stocks.qty')" width="100" align="right">
            <template #default="{ row }"><span class="num">{{ trim(row.qty) }}</span></template>
          </el-table-column>
          <el-table-column :label="t('stocks.afterwards')" width="170" align="right">
            <template #default="{ row }">
              <span class="sub">{{ t('stocks.onHand') }} {{ trim(row.onHandAfter) }} ·
                {{ t('stocks.available') }} {{ trim(row.availableAfter) }}</span>
            </template>
          </el-table-column>
          <!-- Only inbound and outbound move value; a reservation changes who
               may have the goods, not what they cost. -->
          <el-table-column :label="t('stocks.cost')" width="160" align="right">
            <template #default="{ row }">
              <template v-if="movesValue(row.movement)">
                <span class="num">{{ money(row.amount, 2) }}</span>
                <div class="sub">@ {{ money(row.unitCost) }}</div>
              </template>
              <span v-else class="sub">—</span>
            </template>
          </el-table-column>
          <el-table-column :label="t('stocks.source')" min-width="170">
            <template #default="{ row }">
              <span v-if="row.refNo" class="sub">{{ row.refNo }}</span>
              <span v-else class="sub">{{ row.remark || '—' }}</span>
            </template>
          </el-table-column>
          <template #empty>{{ t('stocks.noLedger') }}</template>
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

    <el-dialog v-model="receiveOpen" :title="t('stocks.receive')" width="620px">
      <el-form label-width="90px">
        <el-form-item :label="t('stocks.warehouse')" required>
          <el-select v-model="form.warehouseId" style="width: 100%">
            <el-option v-for="w in warehouses" :key="w.id" :value="Number(w.id)" :label="w.name" />
          </el-select>
        </el-form-item>
        <el-form-item :label="t('stocks.product')" required>
          <el-select v-model="form.productId" filterable style="width: 100%" :placeholder="t('stocks.pickProduct')">
            <el-option
              v-for="p in products"
              :key="p.id"
              :value="Number(p.id)"
              :label="`${p.code} · ${p.name}`"
            />
          </el-select>
        </el-form-item>
        <el-form-item :label="t('stocks.qty')" required>
          <el-input v-model="form.qty" style="width: 200px" />
        </el-form-item>
        <el-form-item :label="t('stocks.unitCost')">
          <el-input v-model="form.unitCost" style="width: 200px" :placeholder="t('stocks.unitCostHint')">
            <template #append>CNY</template>
          </el-input>
        </el-form-item>
        <el-form-item :label="t('stocks.remark')">
          <el-input v-model="form.remark" type="textarea" :rows="2" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="receiveOpen = false">{{ t('common.cancel') }}</el-button>
        <el-button type="primary" :loading="saving" @click="submitReceive">{{ t('common.save') }}</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup lang="ts">
import { onMounted, ref, reactive } from 'vue'
import { ElMessage } from 'element-plus'
import { useI18n } from 'vue-i18n'
import { get, post } from '../api'
import { useAuthStore } from '../stores/auth'

interface Warehouse { id: string; code: string; name: string }
interface Stock {
  id: string
  warehouseName: string
  productCode: string
  productName: string
  uomCode: string
  onHandQty: string
  reservedQty: string
  lockedQty: string
  availableQty: string
  avgCost: string
  totalCost: string
  costCurrency: string
}
interface Ledger {
  id: string
  productName: string
  movement: string
  qty: string
  refNo: string
  onHandAfter: string
  availableAfter: string
  unitCost: string
  amount: string
  remark: string
  occurredAt: string
}
interface Product { id: string; code: string; name: string; uomId: string; uomCode: string }

const MOVEMENTS = ['INBOUND', 'OUTBOUND', 'RESERVE', 'RELEASE_RESERVE', 'LOCK', 'RELEASE_LOCK']

const { t } = useI18n()
const auth = useAuthStore()
const canWrite = auth.can('inventory:stock:write')

const tab = ref('stock')
const stocks = ref<Stock[]>([])
const ledger = ref<Ledger[]>([])
const warehouses = ref<Warehouse[]>([])
const products = ref<Product[]>([])
const total = ref(0)
const page = ref(1)
const pageSize = 20
const warehouseId = ref(0)
const keyword = ref('')
const inStockOnly = ref(false)
const movement = ref('')
const loading = ref(false)
const saving = ref(false)
const receiveOpen = ref(false)
const form = reactive({ warehouseId: 0, productId: 0, qty: '', unitCost: '', remark: '' })

async function load() {
  loading.value = true
  try {
    if (tab.value === 'stock') {
      const d = await get<{ stocks: Stock[]; meta: { total: number } }>('/stocks', {
        page: page.value, page_size: pageSize, warehouse_id: warehouseId.value,
        keyword: keyword.value, in_stock_only: inStockOnly.value,
      })
      stocks.value = d.stocks ?? []
      total.value = Number(d.meta?.total ?? 0)
    } else {
      const d = await get<{ entries: Ledger[]; meta: { total: number } }>('/stock-ledger', {
        page: page.value, page_size: pageSize, movement: movement.value,
      })
      ledger.value = d.entries ?? []
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

async function openReceive() {
  form.warehouseId = Number(warehouses.value[0]?.id ?? 0)
  form.productId = 0
  form.qty = ''
  form.unitCost = ''
  form.remark = ''
  if (!products.value.length) {
    products.value = (await get<{ products: Product[] }>('/products', { page_size: 200 })).products ?? []
  }
  receiveOpen.value = true
}

async function submitReceive() {
  const product = products.value.find((p) => Number(p.id) === form.productId)
  if (!product || !form.qty) {
    ElMessage.warning(t('stocks.receiveRequired'))
    return
  }
  saving.value = true
  try {
    await post('/stocks/receive', {
      warehouse_id: form.warehouseId,
      remark: form.remark,
      lines: [{
        product_id: Number(product.id),
        // Products without variants have no SKU at all; 0 is the agreed
        // "no variant" value rather than a missing one.
        sku_id: 0,
        product_code: product.code,
        product_name: product.name,
        uom_id: Number(product.uomId ?? 0),
        uom_code: product.uomCode ?? '',
        qty: form.qty,
        // Left blank the receipt is booked at the average already on the row,
        // so a stock correction cannot drag the valuation towards zero.
        unit_cost: form.unitCost,
      }],
    })
    ElMessage.success(t('stocks.received'))
    receiveOpen.value = false
    load()
  } finally {
    saving.value = false
  }
}

function trim(v: string): string {
  if (!v) return '0'
  if (!v.includes('.')) return v
  return v.replace(/0+$/, '').replace(/\.$/, '')
}

function isZero(v: string): boolean {
  return Number(v ?? 0) === 0
}

// Money reads as money: fixed decimals and thousands separators, unlike
// quantities where trailing zeros are just noise.
function money(v: string, digits = 4): string {
  const n = Number(v ?? 0)
  if (!Number.isFinite(n)) return '—'
  return n.toLocaleString(undefined, { minimumFractionDigits: digits, maximumFractionDigits: digits })
}

function movesValue(m: string): boolean {
  return m === 'INBOUND' || m === 'OUTBOUND'
}

function movementType(m: string): 'success' | 'danger' | 'warning' | 'info' {
  if (m === 'INBOUND') return 'success'
  if (m === 'OUTBOUND') return 'danger'
  if (m === 'RESERVE' || m === 'LOCK') return 'warning'
  return 'info'
}

function formatTime(v: string): string {
  return v ? v.replace('T', ' ').slice(0, 16) : '—'
}

onMounted(async () => {
  warehouses.value = (await get<{ warehouses: Warehouse[] }>('/warehouses')).warehouses ?? []
  load()
})
</script>

<style scoped>
.page-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin-bottom: 16px;
}
.page-head h2 {
  margin: 0;
  font-size: 20px;
}
.tabs {
  margin-bottom: 14px;
}
.filters {
  display: flex;
  align-items: center;
  gap: 10px;
  margin-bottom: 14px;
}
.legend {
  margin-bottom: 12px;
}
.lg {
  margin-right: 18px;
  font-size: 12px;
}
.lg b {
  margin-right: 4px;
}
.prod {
  font-weight: 500;
}
.sub {
  font-size: 12px;
  color: var(--el-text-color-secondary);
}
.num {
  font-variant-numeric: tabular-nums;
}
.dim {
  color: var(--el-text-color-placeholder);
}
.avail {
  font-weight: 600;
  color: var(--el-color-success);
}
.avail.none {
  color: var(--el-color-danger);
}
.uom {
  margin-left: 4px;
  font-size: 12px;
  color: var(--el-text-color-secondary);
}
.pager {
  margin-top: 14px;
  justify-content: flex-end;
}
</style>
