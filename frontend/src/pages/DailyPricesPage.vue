<template>
  <main class="daily-page">
    <section v-loading="loading" class="sheet panel">
      <div class="sheet-toolbar">
        <div class="date-control"><span>{{ t('dailyPrice.date') }}</span><el-date-picker v-model="selectedDate" type="date" value-format="YYYY-MM-DD" :clearable="false" :aria-label="t('dailyPrice.date')" /><span v-if="demoDataPresent" class="demo-tag">{{ t('dailyPrice.demoData') }}</span></div>
        <div class="toolbar-actions"><span v-if="dirty" class="unsaved">{{ t('dailyPrice.unsavedCount', { count: priceDirty.size + spreadDirty.size }) }}</span><el-button v-if="canManage" text @click="openConfig()">{{ t('dailyPrice.manage') }}</el-button><el-button v-if="canWrite" type="primary" :disabled="!dirty" :loading="saving" @click="saveAll">{{ t('dailyPrice.saveAll') }}</el-button></div>
      </div>
      <p v-if="!activeProducts.length || !activeSuppliers.length" class="setup-hint">{{ t(canManage ? 'dailyPrice.setupHint' : 'dailyPrice.setupHintBuyer') }}</p>
      <div class="sheet-grid">
      <div class="matrix-scroll">
        <table class="data-table price-matrix" :style="{ minWidth: `${180 + activeSuppliers.length * 110}px` }">
          <thead><tr class="group-row"><th class="row-label" /><th :colspan="Math.max(1, activeSuppliers.length)">{{ t('dailyPrice.price') }}</th></tr><tr><th class="row-label">{{ dayLabel }}</th><th v-for="supplier in activeSuppliers" :key="supplier.id">{{ supplier.name }}</th></tr></thead>
          <tbody><tr v-for="product in activeProducts" :key="product.id"><th class="row-label">{{ product.name }}</th><td v-for="supplier in activeSuppliers" :key="supplier.id" :class="{ changed: priceDirty.has(cellKey(product.id, supplier.id)) }"><div class="cell-edit"><el-input :model-value="priceCell(product.id, supplier.id).price" type="number" min="0" step="0.0001" :disabled="!editablePrice(product.id, supplier.id)" :placeholder="t('dailyPrice.empty')" @update:model-value="(v: string) => editPrice(product.id, supplier.id, String(v), 'price')" @keydown.enter.prevent="focusNext($event)" /><el-popover trigger="click" placement="bottom" :width="230"><template #reference><el-button text size="small" :disabled="!editablePrice(product.id, supplier.id)" :class="{ hasRemark: !!priceCell(product.id, supplier.id).remark }" :title="t('dailyPrice.remark')">✎</el-button></template><label class="remark-label">{{ t('dailyPrice.remark') }}<el-input :model-value="priceCell(product.id, supplier.id).remark" maxlength="500" @update:model-value="(v: string) => editPrice(product.id, supplier.id, String(v), 'remark')" /></label></el-popover></div></td></tr></tbody>
          <tfoot><tr class="supplier-note-row"><th class="row-label">{{ t('dailyPrice.supplierNote') }}</th><td v-for="supplier in activeSuppliers" :key="supplier.id"><el-input v-if="canManage" :model-value="supplierNotes[supplier.id] ?? ''" maxlength="200" :aria-label="`${supplier.name} ${t('dailyPrice.supplierNote')}`" :disabled="savingSupplierNote === supplier.id" @update:model-value="(v: string) => supplierNotes[supplier.id] = v" @change="() => saveSupplierNote(supplier)" /><span v-else>{{ supplier.note || '—' }}</span></td></tr></tfoot>
        </table>
      </div>

      <div class="matrix-scroll spread-block">
        <table class="data-table spread-table">
          <thead><tr class="group-row"><th colspan="5">{{ t('dailyPrice.spread') }}</th></tr><tr><th class="row-label">{{ t('dailyPrice.product') }}</th><th>{{ t('dailyPrice.spot') }}</th><th>{{ t('dailyPrice.futures') }}</th><th>{{ t('dailyPrice.basis') }}</th><th>{{ t('dailyPrice.basisPercent') }}</th></tr></thead>
          <tbody><tr v-for="product in activeSpreads" :key="product.id" :class="{ changed: spreadDirty.has(product.id) }"><th class="row-label">{{ product.name }}</th><td><el-input :model-value="spreadCell(product.id).spot" type="number" min="0" step="0.0001" :disabled="!editableSpread(product.id)" @update:model-value="(v: string) => editSpread(product.id, String(v), 'spot')" @keydown.enter.prevent="focusNext($event)" /></td><td><el-input :model-value="spreadCell(product.id).futures" type="number" min="0" step="0.0001" :disabled="!editableSpread(product.id)" @update:model-value="(v: string) => editSpread(product.id, String(v), 'futures')" @keydown.enter.prevent="focusNext($event)" /></td><td>{{ basis(product.id) }}</td><td>{{ basisPercent(product.id) }}</td></tr></tbody>
        </table>
      </div>
      </div>
    </section>

    <section class="panel trend-panel">
      <div class="trend-heading"><div class="trend-title"><h2>{{ t('dailyPrice.historyTrend') }}</h2><span v-if="demoTrendPresent" class="demo-tag">{{ t('dailyPrice.demoData') }}</span></div><el-radio-group v-model="trendKind" size="small"><el-radio-button value="price">{{ t('dailyPrice.price') }}</el-radio-button><el-radio-button value="spread">{{ t('dailyPrice.spread') }}</el-radio-button></el-radio-group></div>
      <div v-if="trendReady" class="filters">
        <template v-if="trendKind === 'price'"><el-select v-model="trendProduct" filterable :placeholder="t('dailyPrice.product')"><el-option v-for="item in products" :key="item.id" :label="item.name" :value="item.id" /></el-select><el-select v-model="trendSuppliers" multiple :multiple-limit="6" collapse-tags filterable :placeholder="t('dailyPrice.suppliers')"><el-option v-for="item in suppliers" :key="item.id" :label="item.name" :value="item.id" /></el-select></template>
        <template v-else><el-select v-model="trendSpread" filterable :placeholder="t('dailyPrice.product')"><el-option v-for="item in spreadProducts" :key="item.id" :label="item.name" :value="item.id" /></el-select><el-select v-model="spreadMetric"><el-option value="spot" :label="t('dailyPrice.spot')" /><el-option value="futures" :label="t('dailyPrice.futures')" /><el-option value="basis" :label="t('dailyPrice.basis')" /></el-select></template>
        <el-select v-model="rangePreset" @change="loadTrend"><el-option v-for="item in rangeOptions" :key="item.value" :label="t(item.label)" :value="item.value" /></el-select>
        <el-date-picker v-if="rangePreset === 'custom'" v-model="customRange" type="daterange" value-format="YYYY-MM-DD" :start-placeholder="t('dailyPrice.from')" :end-placeholder="t('dailyPrice.to')" @change="loadTrend" />
      </div>
      <DailyTrendChart v-if="trendReady" :title="t('dailyPrice.historyTrend')" :lines="trendKind === 'price' ? priceLines : spreadLines" :empty-label="t('dailyPrice.noTrend')" />
      <p v-else class="trend-empty">{{ t('dailyPrice.trendStartHint') }}</p>
    </section>

    <el-dialog v-model="configOpen" :title="t('dailyPrice.manage')" width="min(640px,95vw)">
      <p class="config-hint">{{ t('dailyPrice.configHint') }}</p>
      <div class="config-form">
        <el-select v-model="configKind"><el-option :label="t('dailyPrice.product')" value="PRODUCT" /><el-option :label="t('dailyPrice.supplier')" value="SUPPLIER" /><el-option :label="t('dailyPrice.spreadProduct')" value="SPREAD" /></el-select>
        <el-autocomplete v-model="configName" class="config-name" :fetch-suggestions="suggestConfigNames" :placeholder="t('dailyPrice.configEntry')" clearable maxlength="200" @input="configMaster = undefined" @select="selectConfigMaster" />
        <el-button type="primary" :loading="configSaving" @click="addConfig">{{ t('dailyPrice.add') }}</el-button>
      </div>
      <el-table :data="configRows" size="small" class="config-list"><el-table-column prop="name" :label="t('dailyPrice.name')" /><el-table-column :label="t('dailyPrice.display')" width="100"><template #default="scope"><el-switch :model-value="scope.row.active" @change="(v: boolean) => toggleConfig(scope.row, Boolean(v))" /></template></el-table-column><el-table-column :label="t('dailyPrice.actions')" width="76" align="center"><template #default="scope"><el-button text type="danger" size="small" @click="removeConfig(scope.row)">{{ t('dailyPrice.delete') }}</el-button></template></el-table-column></el-table>
    </el-dialog>
  </main>
</template>

<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref, watch } from 'vue'
import { onBeforeRouteLeave } from 'vue-router'
import { ElMessage, ElMessageBox } from 'element-plus'
import { useI18n } from 'vue-i18n'
import { del, get, put } from '../api'
import { useAuthStore } from '../stores/auth'
import DailyTrendChart, { type TrendLine } from '../components/DailyTrendChart.vue'

type Dimension = { id: number; kind: 'PRODUCT' | 'SUPPLIER' | 'SPREAD'; masterId: number; name: string; note: string; sortOrder: number; active: boolean }
type Price = { id: number; date: string; productId: number; supplierId: number; price: string; previousPrice: string; remark: string; createdBy: number }
type Spread = { id: number; date: string; productId: number; spot: string | null; futures: string | null; basis: string | null; createdBy: number; createdByName?: string }
type PriceCell = { price: string; remark: string; previousPrice: string }

const auth = useAuthStore()
const { t } = useI18n()
const canWrite = computed(() => auth.can('procurement:daily-price:write'))
const canManage = computed(() => auth.can('procurement:daily-price:manage'))
const today = () => { const d = new Date(); return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')}` }
const selectedDate = ref(today())
const loadedDate = ref(today())
const dayLabel = computed(() => { const [, month, day] = selectedDate.value.split('-'); return `${Number(month)}.${Number(day)} ${t('dailyPrice.price')}` })
const dimensions = ref<Dimension[]>([])
const dayPrices = ref<Price[]>([])
const daySpreads = ref<Spread[]>([])
const trendPrices = ref<Price[]>([])
const trendSpreads = ref<Spread[]>([])
const priceDraft = ref<Record<string, PriceCell>>({})
const spreadDraft = ref<Record<number, { spot: string; futures: string }>>({})
const priceDirty = ref(new Set<string>())
const spreadDirty = ref(new Set<number>())
const loading = ref(false)
const saving = ref(false)
const configOpen = ref(false)
const configSaving = ref(false)
const supplierNotes = ref<Record<number, string>>({})
const savingSupplierNote = ref<number | null>(null)
const products = computed(() => dimensions.value.filter(x => x.kind === 'PRODUCT'))
const suppliers = computed(() => dimensions.value.filter(x => x.kind === 'SUPPLIER'))
const spreadProducts = computed(() => dimensions.value.filter(x => x.kind === 'SPREAD'))
const activeProducts = computed(() => products.value.filter(x => x.active || dayPrices.value.some(p => p.productId === x.id)))
const activeSuppliers = computed(() => suppliers.value.filter(x => x.active || dayPrices.value.some(p => p.supplierId === x.id)))
const activeSpreads = computed(() => spreadProducts.value.filter(x => x.active || daySpreads.value.some(p => p.productId === x.id)))
const demoDataPresent = computed(() => dayPrices.value.some(p => p.remark === 'TEST DATA'))
const demoTrendPresent = computed(() => trendKind.value === 'price' ? trendPrices.value.some(p => p.remark === 'TEST DATA') : trendSpreads.value.some(p => p.createdByName === 'TEST DATA'))
const cellKey = (productId: number, supplierId: number) => `${productId}:${supplierId}`
const displayNumber = (value: string | null | undefined) => value?.includes('.') ? value.replace(/0+$/, '').replace(/\.$/, '') : (value ?? '')
const priceCell = (p: number, s: number) => priceDraft.value[cellKey(p, s)] || { price: '', remark: '', previousPrice: '' }
const spreadCell = (p: number) => spreadDraft.value[p] || { spot: '', futures: '' }
const dirty = computed(() => priceDirty.value.size + spreadDirty.value.size > 0)

function editablePrice(p: number, s: number) { const current = dayPrices.value.find(x => x.productId === p && x.supplierId === s); return canWrite.value && (!current || canManage.value || String(current.createdBy) === auth.employeeId) }
function editableSpread(p: number) { const current = daySpreads.value.find(x => x.productId === p); return canWrite.value && (!current || canManage.value || String(current.createdBy) === auth.employeeId) }
function editPrice(p: number, s: number, value: string, field: 'price' | 'remark') { const key = cellKey(p, s); priceDraft.value[key] = { ...priceCell(p, s), [field]: value }; priceDirty.value = new Set([...priceDirty.value, key]) }
function editSpread(p: number, value: string, field: 'spot' | 'futures') { spreadDraft.value[p] = { ...spreadCell(p), [field]: value }; spreadDirty.value = new Set([...spreadDirty.value, p]) }
function focusNext(event: KeyboardEvent) { const inputs = [...document.querySelectorAll<HTMLInputElement>('.daily-page .price-matrix input:not([disabled]), .daily-page .spread-table input:not([disabled])')]; inputs[inputs.indexOf(event.target as HTMLInputElement) + 1]?.focus() }
function basis(p: number) { const x = spreadCell(p); return x.spot !== '' && x.futures !== '' ? (Number(x.spot) - Number(x.futures)).toFixed(2) : '—' }
function basisPercent(p: number) { const x = spreadCell(p); return x.spot !== '' && x.futures !== '' && Number(x.futures) !== 0 ? `${((Number(x.spot) - Number(x.futures)) / Number(x.futures) * 100).toFixed(2)}%` : '—' }
async function confirmDiscard() { if (!dirty.value) return true; try { await ElMessageBox.confirm(t('dailyPrice.unsavedWarning'), t('dailyPrice.unsavedTitle'), { type: 'warning' }); return true } catch { return false } }
onBeforeRouteLeave(async () => await confirmDiscard())
function beforeUnload(event: BeforeUnloadEvent) { if (dirty.value) { event.preventDefault(); event.returnValue = '' } }

async function loadConfig() { const d = await get<{ dimensions: Dimension[] }>('/daily-prices/config'); dimensions.value = d.dimensions || []; supplierNotes.value = Object.fromEntries(dimensions.value.filter(x => x.kind === 'SUPPLIER').map(x => [x.id, x.note || ''])); trendProduct.value ||= products.value[0]?.id; if (!trendSuppliers.value.length) trendSuppliers.value = suppliers.value.filter(x => x.active).slice(0, 3).map(x => x.id); trendSpread.value ||= spreadProducts.value[0]?.id }
async function saveSupplierNote(supplier: Dimension) { const note = (supplierNotes.value[supplier.id] || '').trim(); if (note === supplier.note) return; savingSupplierNote.value = supplier.id; try { await put('/daily-prices/config', { id: supplier.id, kind: supplier.kind, active: supplier.active, sortOrder: supplier.sortOrder, note }); await loadConfig(); ElMessage.success(t('dailyPrice.saved')) } catch { supplierNotes.value[supplier.id] = supplier.note; } finally { savingSupplierNote.value = null } }
async function loadDay(date: string) { loading.value = true; try { const d = await get<{ prices: Price[]; spreads: Spread[]; previous: Record<string, string> }>('/daily-prices/day', { date }); dayPrices.value = d.prices || []; daySpreads.value = d.spreads || []; priceDraft.value = {}; spreadDraft.value = {}; for (const [key, previousPrice] of Object.entries(d.previous || {})) priceDraft.value[key] = { price: '', remark: '', previousPrice }; for (const p of dayPrices.value) priceDraft.value[cellKey(p.productId, p.supplierId)] = { price: displayNumber(p.price), remark: p.remark, previousPrice: p.previousPrice }; for (const p of daySpreads.value) spreadDraft.value[p.productId] = { spot: displayNumber(p.spot), futures: displayNumber(p.futures) }; priceDirty.value = new Set(); spreadDirty.value = new Set(); loadedDate.value = date } catch { selectedDate.value = loadedDate.value } finally { loading.value = false } }
watch(selectedDate, async (value, old) => { if (!value || value === loadedDate.value) return; if (!await confirmDiscard()) { selectedDate.value = old; return } await loadDay(value) })
async function saveAll() { if (!dirty.value) return; saving.value = true; try { if (priceDirty.value.size) { const prices = [...priceDirty.value].map(key => { const [productId, supplierId] = key.split(':').map(Number); const { price, remark } = priceCell(productId, supplierId); return { productId, supplierId, price, remark } }); await put('/daily-prices/prices', { date: selectedDate.value, prices }) } if (spreadDirty.value.size) { const spreads = [...spreadDirty.value].map(productId => ({ productId, ...spreadCell(productId) })); await put('/daily-prices/spreads', { date: selectedDate.value, spreads }) } await loadDay(selectedDate.value); await loadTrend(); ElMessage.success(t('dailyPrice.saved')) } finally { saving.value = false } }

const trendKind = ref<'price' | 'spread'>('price')
const rangeOptions = [{ value: '7', label: 'dailyPrice.last7' }, { value: '30', label: 'dailyPrice.last30' }, { value: '90', label: 'dailyPrice.last90' }, { value: 'custom', label: 'dailyPrice.custom' }]
const rangePreset = ref('30')
const customRange = ref<string[]>([])
const trendProduct = ref<number | undefined>()
const trendSuppliers = ref<number[]>([])
const trendSpread = ref<number | undefined>()
const spreadMetric = ref<'spot' | 'futures' | 'basis'>('basis')
const trendReady = computed(() => trendKind.value === 'price' ? !!trendProduct.value && trendSuppliers.value.length > 0 : !!trendSpread.value)
function rangeDates() { if (rangePreset.value === 'custom') return customRange.value.length === 2 ? customRange.value : null; const to = today(), d = new Date(`${to}T12:00:00`); d.setDate(d.getDate() - Number(rangePreset.value) + 1); return [`${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')}`, to] }
async function loadTrend() { const range = rangeDates(); if (!range || (!trendProduct.value && !trendSpread.value)) { trendPrices.value = []; trendSpreads.value = []; return } const d = await get<{ prices: Price[]; spreads: Spread[] }>('/daily-prices/trend', { from: range[0], to: range[1], productId: trendProduct.value || 0, supplierId: trendSuppliers.value, spreadProductId: trendSpread.value || 0 }); trendPrices.value = d.prices || []; trendSpreads.value = d.spreads || [] }
watch([trendProduct, trendSuppliers, trendSpread], () => { void loadTrend() }, { deep: true })
const colors = ['#087fb3', '#22a96b', '#ea8a28', '#9061cb', '#df6371', '#657e98']
const priceLines = computed<TrendLine[]>(() => trendSuppliers.value.map((id, i) => ({ name: suppliers.value.find(x => x.id === id)?.name || String(id), color: colors[i % colors.length], points: trendPrices.value.filter(p => p.productId === trendProduct.value && p.supplierId === id).map(p => ({ date: p.date, value: Number(p.price), change: p.previousPrice ? `${(Number(p.price) - Number(p.previousPrice)).toFixed(2)}` : '' })) })).filter(x => x.points.length))
const spreadLines = computed<TrendLine[]>(() => [{ name: t(`dailyPrice.${spreadMetric.value}`), color: colors[0], points: trendSpreads.value.filter(p => p.productId === trendSpread.value && p[spreadMetric.value] != null).map(p => ({ date: p.date, value: Number(p[spreadMetric.value]) })) }].filter(x => x.points.length))

const configKind = ref<'PRODUCT' | 'SUPPLIER' | 'SPREAD'>('PRODUCT')
const configMaster = ref<number | undefined>()
const configName = ref('')
type ConfigSuggestion = { id: number; value: string }
const configRows = computed(() => dimensions.value.filter(x => x.kind === configKind.value))
async function suggestConfigNames(keyword: string, done: (items: ConfigSuggestion[]) => void) { if (configKind.value === 'SPREAD') { done([]); return } const path = configKind.value === 'PRODUCT' ? '/products' : '/suppliers'; try { const data = await get<{ products?: { id: string; name: string }[]; suppliers?: { id: string; name: string }[] }>(path, { keyword, page: 1, page_size: 100, status: 'ACTIVE' }); done((data.products || data.suppliers || []).map(x => ({ id: Number(x.id), value: x.name }))) } catch { done([]) } }
function selectConfigMaster(item: ConfigSuggestion) { configMaster.value = item.id; configName.value = item.value }
function openConfig() { configOpen.value = true }
watch(configKind, () => { configMaster.value = undefined; configName.value = '' })
async function addConfig() { if (!configMaster.value && !configName.value.trim()) { ElMessage.warning(t('dailyPrice.chooseItem')); return } configSaving.value = true; try { await put('/daily-prices/config', { kind: configKind.value, masterId: configMaster.value || 0, name: configName.value.trim(), sortOrder: configRows.value.length }); configName.value = ''; configMaster.value = undefined; await loadConfig(); await loadTrend(); ElMessage.success(t('dailyPrice.saved')) } catch { /* The API shows the error; keep the entry for correction. */ } finally { configSaving.value = false } }
async function toggleConfig(row: Dimension, active: boolean) { try { await put('/daily-prices/config', { id: row.id, kind: row.kind, active, sortOrder: row.sortOrder, note: row.note }); await loadConfig() } catch { await loadConfig() } }
async function removeConfig(row: Dimension) { try { await ElMessageBox.confirm(t('dailyPrice.deleteDimensionConfirm', { name: row.name }), t('dailyPrice.delete'), { type: 'warning' }); await del(`/daily-prices/config/${row.id}`); await loadConfig(); ElMessage.success(t('dailyPrice.deleted')) } catch { /* Cancellation or API error keeps the row unchanged. */ } }
onMounted(async () => { window.addEventListener('beforeunload', beforeUnload); await loadConfig(); await Promise.all([loadDay(selectedDate.value), loadTrend()]) })
onUnmounted(() => window.removeEventListener('beforeunload', beforeUnload))
</script>

<style scoped>
.daily-page { width: 100%; min-width: 0; padding-bottom: 20px; font-size: 12px; }
.panel { background: #fff; border: 1px solid #dce8f2; border-radius: 8px; padding: 10px 12px; margin-bottom: 12px; box-shadow: none; }
.sheet, .trend-panel { width: 100%; min-width: 0; }
.sheet { container-type: inline-size; }
.sheet-toolbar, .toolbar-actions, .date-control, .trend-heading, .filters { display: flex; align-items: center; gap: 8px; }
.sheet-toolbar, .trend-heading { justify-content: space-between; }
.sheet-toolbar { padding-bottom: 8px; margin-bottom: 8px; border-bottom: 1px solid #e7eef4; overflow-x: auto; }
.date-control, .toolbar-actions { flex: none; }
.toolbar-actions { margin-left: auto; }
.date-control { font-size: 12px; color: #526578; white-space: nowrap; }
.date-control :deep(.el-date-editor) { width: 130px; }
.daily-page :deep(.el-button) { height: 28px; padding: 4px 8px; font-size: 12px; }
.daily-page :deep(.el-input__wrapper), .daily-page :deep(.el-select__wrapper) { min-height: 28px; font-size: 12px; }
.daily-page :deep(.el-input__inner), .daily-page :deep(.el-select__selected-item) { font-size: 12px; }
.unsaved { font-size: 11px; color: #b27115; }
.demo-tag { color: #a66723; font-size: 11px; }
.setup-hint { color: #6a8190; font-size: 11px; margin: 8px 0; }
h2 { color: #163347; font-size: 14px; margin: 8px 0; }
.trend-heading h2 { margin: 0; }
.trend-title { display: flex; align-items: center; gap: 8px; }
.sheet-grid { display: grid; grid-template-columns: minmax(0, 1.6fr) minmax(0, 1fr); align-items: start; gap: 12px; }
.matrix-scroll { min-width: 0; overflow-x: auto; border: 1px solid #bbcbd5; }
.data-table { width: 100%; border-collapse: collapse; table-layout: fixed; font-size: 12px; }
.price-matrix { min-width: 820px; }
.spread-table { min-width: 530px; }
.data-table th, .data-table td { border-bottom: 1px solid #c8d4dc; border-right: 1px solid #c8d4dc; text-align: center; padding: 1px 4px; height: 30px; }
.data-table tr:last-child th, .data-table tr:last-child td { border-bottom: 0; }
.data-table tfoot th, .data-table tfoot td { border-top: 1px solid #bbcbd5; border-bottom: 0; }
.supplier-note-row th, .supplier-note-row td { background: #f8fbfd; color: #526578; }
.supplier-note-row td :deep(.el-input__inner) { font-size: 12px; }
.data-table tr th:last-child, .data-table tr td:last-child { border-right: 0; }
.data-table thead th { background: #f5faff; color: #294558; font-weight: 600; }
.price-matrix .row-label { width: 180px; }
.spread-table .row-label { width: 120px; }
.data-table .row-label { text-align: center; padding-left: 4px; }
.data-table .group-row th { background: #edf6fb; text-align: center; height: 22px; font-weight: 650; }
.price-matrix .row-label { position: sticky; left: 0; z-index: 1; background: #fff; }
.price-matrix thead .row-label { z-index: 2; background: #f5faff; }
.price-matrix .group-row .row-label { background: #edf6fb; }
.data-table .changed { background: #fff7df; }
.spread-table tr.changed td { background: #fff7df; }
.cell-edit { display: flex; align-items: center; justify-content: center; min-width: 0; position: relative; }
.cell-edit :deep(.el-input) { width: 100%; min-width: 0; }
.cell-edit :deep(.el-button) { position: absolute; right: 0; top: 50%; transform: translateY(-50%); height: 20px; min-height: 20px; padding: 1px; font-size: 11px; color: #a5b3bc; background: #fff; opacity: 0; }
.cell-edit:hover :deep(.el-button), .cell-edit:focus-within :deep(.el-button), .cell-edit :deep(.el-button.hasRemark) { opacity: 1; }
.cell-edit :deep(.el-button.hasRemark) { color: #1685b8; }
.data-table :deep(.el-input__wrapper) { box-shadow: none; background: transparent; padding: 0 2px; min-height: 22px; }
.data-table :deep(.el-input__inner) { text-align: center; font-size: 12px; }
.data-table :deep(input[type='number']) { appearance: textfield; }
.data-table :deep(input[type='number']::-webkit-inner-spin-button), .data-table :deep(input[type='number']::-webkit-outer-spin-button) { appearance: none; margin: 0; }
.price-matrix :deep(.el-input__wrapper) { padding-right: 15px; }
.data-table :deep(.el-input__wrapper:hover), .data-table :deep(.el-input__wrapper.is-focus) { box-shadow: 0 0 0 1px #83c8ee inset; }
.spread-table td :deep(.el-input) { width: 100%; }
.spread-table td :deep(.el-input__wrapper) { margin: auto; }
.filters { flex-wrap: wrap; margin: 10px 0 8px; }
.filters :deep(.el-select) { width: 150px; }
.trend-panel :deep(.el-radio-button__inner) { padding: 6px 9px; font-size: 12px; }
.trend-empty { color: #7a8c99; font-size: 12px; margin: 10px 0 4px; }
.config-form { display: grid; grid-template-columns: 120px minmax(0, 1fr) auto; gap: 8px; }
.config-name { width: 100%; }
.config-hint { margin: 0 0 10px; color: #607789; font-size: 12px; }
.config-list { margin-top: 15px; }
.remark-label { font-size: 11px; color: #607789; }
@media (max-width: 800px) { .panel { padding: 8px; } .trend-heading { align-items: flex-start; flex-direction: column; } .config-form { grid-template-columns: 1fr; } }
@container (max-width: 1450px) { .sheet-grid { grid-template-columns: minmax(0, 1fr); } }
</style>
