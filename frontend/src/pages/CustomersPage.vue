<template>
  <div>
    <div class="mobile-country" v-loading="countryLoading">
      <div class="mobile-country__title">
        <span>国家范围</span>
        <strong>{{ selectedCountrySummary }}</strong>
      </div>
      <el-select class="country-select" v-model="selectedCountry" aria-label="按国家筛选客户" @change="selectCountry">
        <el-option :label="`${t('customers.allCountries')}（${countryTotal}）`" value="" />
        <el-option v-for="group in displayedCountryGroups" :key="group.code||'none'" :label="`${group.code ? countryName(group.code, locale) : t('customers.unclassified')}（${Number(group.customerCount)}）`" :value="group.code||'__UNCLASSIFIED__'" />
      </el-select>
    </div>
    <div class="customer-workspace">
      <el-card class="customer-list" shadow="never">
      <div class="filters">
        <el-input
          v-model="keyword"
          :placeholder="t('customers.searchPlaceholder')"
          clearable
          class="filter-search"
          @keyup.enter="load"
          @clear="load"
        />
        <el-button @click="load">{{ t('common.query') }}</el-button>
        <el-select v-model="customerType" clearable :placeholder="t('customers.type')" class="filter-select" @change="changeFilters"><el-option v-for="o in typeOptions" :key="o.code" :label="o.label" :value="o.code" /></el-select>
        <el-select v-model="businessStatus" clearable :placeholder="t('customers.businessStatus')" class="filter-select" @change="changeFilters"><el-option :label="t('customers.statusProspect')" value="PROSPECT"/><el-option :label="t('customers.statusCooperating')" value="COOPERATING"/><el-option :label="t('customers.statusPaused')" value="PAUSED"/><el-option :label="t('customers.statusInactive')" value="INACTIVE"/></el-select>
        <el-button v-if="columnOrder.customized.value" link type="primary" @click="columnOrder.reset">恢复默认列顺序</el-button>
        <div class="filter-end">
          <el-checkbox v-model="showInactive" @change="changeScope">{{ t('customers.showInactive') }}</el-checkbox>
          <div v-if="auth.can('masterdata:customer:write')" class="filter-actions">
            <el-button @click="importOpen=true">{{ t('customers.bulkImport') }}</el-button>
            <el-button type="primary" @click="openCreate">{{ t('customers.create') }}</el-button>
          </div>
        </div>
      </div>

      <div v-if="canManageOwners" class="bulk-owner-bar">
        <el-button @click="toggleSelectPage">{{ allPageSelected ? '取消全选' : '全选本页' }}</el-button>
        <span>已选择 <strong>{{ selectedCustomers.length }}</strong> 条</span>
        <el-button type="primary" :disabled="!selectedCustomers.length" @click="openBulkOwners('ADD')">批量添加负责人</el-button>
        <el-button :disabled="!selectedCustomers.length" @click="openBulkOwners('REMOVE')">批量移除负责人</el-button>
        <el-button v-if="selectedCustomers.length" link @click="clearSelection">取消选择</el-button>
      </div>

      <el-table ref="customerTable" class="customer-table" :data="customers" v-loading="loading" @selection-change="selectedCustomers=$event" @row-click="handleCustomerRowClick">
        <el-table-column v-if="canManageOwners" type="selection" width="48" />
        <el-table-column v-for="column in columnOrder.columns.value" :key="column.key" :min-width="column.minWidth">
<template #header><ReorderableTableHeader :label="column.label" hint="调整列顺序" move-left-label="左移" move-right-label="右移" :can-move-left="!!layoutTenant && columnOrder.canMoveLeft(column.key)" :can-move-right="!!layoutTenant && columnOrder.canMoveRight(column.key)" @move-left="columnOrder.moveBy(column.key, -1)" @move-right="columnOrder.moveBy(column.key, 1)" /></template>
<template #default="{ row }">
<template v-if="column.key === 'name'">
            <button class="customer-identity" type="button" @click.stop="openDetail(row)">
              <strong>{{ row.name }}</strong>
              <span>{{ row.code }}<template v-if="row.shortName || row.englishName"> · {{ row.shortName || row.englishName }}</template></span>
            </button>
          </template>
<template v-if="column.key === 'country'"><span>{{ row.country || (row.countryCode ? countryName(row.countryCode, locale) : '未设置') }}</span></template>
<template v-if="column.key === 'type'">{{ optionLabel(typeOptions,row.customerType) }}</template>
<template v-if="column.key === 'contact'"><span :class="{ muted: !row.primaryContactName }">{{row.primaryContactName||'未维护'}}</span></template>
<template v-if="column.key === 'owners'"><span v-if="row.owners?.length">{{row.owners.map((o:any)=>o.employeeName).join('、')}}</span><span v-else class="muted">未分配</span></template>
<template v-if="column.key === 'status'"><el-tag :type="row.status === 'ACTIVE' ? 'success' : 'info'" size="small">{{ row.status === 'ACTIVE' ? t('common.active') : t('common.inactive') }}</el-tag></template>
<template v-if="column.key === 'actions'">
            <el-dropdown v-if="auth.can('masterdata:customer:write')" trigger="click" @command="handleRowCommand(row, $event)">
              <el-button link type="primary" @click.stop>{{ t('common.more') }}</el-button>
              <template #dropdown><el-dropdown-menu>
                <el-dropdown-item command="view">{{ t('customers.viewDetails') }}</el-dropdown-item>
                <el-dropdown-item v-if="canDelete && row.status === 'ACTIVE'" command="delete" class="danger-action">删除</el-dropdown-item>
                <el-dropdown-item v-if="row.status !== 'ACTIVE'" command="activate">{{ t('common.activate') }}</el-dropdown-item>
              </el-dropdown-menu></template>
            </el-dropdown>
            <el-button v-else link type="primary" @click.stop="openDetail(row)">{{ t('common.view') }}</el-button>
          </template>
</template></el-table-column>
      </el-table>

      <div v-loading="loading" class="customer-cards">
        <article v-for="row in customers" :key="row.id" class="customer-card" tabindex="0" @click="openDetail(row)" @keyup.enter="openDetail(row)">
          <div class="customer-card__head">
            <div class="customer-card__identity"><strong>{{ row.name }}</strong><span>{{ row.code }}</span></div>
            <el-tag :type="row.status === 'ACTIVE' ? 'success' : 'info'" size="small">{{ row.status === 'ACTIVE' ? t('common.active') : t('common.inactive') }}</el-tag>
          </div>
          <div class="customer-card__summary">
            <span>{{ row.countryCode ? countryName(row.countryCode, locale) : (row.country || t('customers.countryUnset')) }}</span>
            <span>{{ optionLabel(typeOptions,row.customerType) }}</span>
          </div>
          <dl class="customer-card__facts">
            <div><dt>{{ t('customers.primaryContact') }}</dt><dd>{{ row.primaryContactName || '未维护' }}</dd></div>
            <div><dt>{{ t('customers.owners') }}</dt><dd>{{ row.owners?.length ? row.owners.map((o:any)=>o.employeeName).join('、') : '未分配' }}</dd></div>
          </dl>
          <el-dropdown v-if="auth.can('masterdata:customer:write')" class="customer-card__actions" trigger="click" @command="handleRowCommand(row, $event)">
            <el-button link type="primary" @click.stop>{{ t('common.more') }}</el-button>
            <template #dropdown><el-dropdown-menu>
              <el-dropdown-item command="view">{{ t('customers.viewDetails') }}</el-dropdown-item>
              <el-dropdown-item v-if="canDelete && row.status === 'ACTIVE'" command="delete" class="danger-action">删除</el-dropdown-item>
                <el-dropdown-item v-if="row.status !== 'ACTIVE'" command="activate">{{ t('common.activate') }}</el-dropdown-item>
            </el-dropdown-menu></template>
          </el-dropdown>
        </article>
        <el-empty v-if="!loading && !customers.length" :description="t('customers.title')" />
      </div>

      <el-pagination
        class="pager"
        layout="total, sizes, prev, pager, next"
        :total="total"
        :page-size="pageSize"
        :page-sizes="[20,50,100]"
        :current-page="page"
        @size-change="changePageSize"
        @current-change="changePage"
      />
      </el-card>
    </div>

    <el-dialog
      v-model="dialogOpen"
      :title="editingId ? t('customers.edit') : t('customers.create')"
      width="560px"
    >
      <el-form :model="form" label-width="110px" v-loading="loadingDetail">
        <el-form-item :label="t('customers.code')">
          <!-- The code is the customer's business identity: documents and
               statements quote it, so it is fixed once issued. -->
          <el-input
            v-model="form.code"
            :disabled="!!editingId"
            :placeholder="t('customers.codeAuto')"
          />
        </el-form-item>
        <el-form-item :label="t('customers.name')" required>
          <el-input v-model="form.name" />
        </el-form-item>
        <el-form-item :label="t('customers.country')">
          <!-- A code, not a name, and no allow-create. Free text is what made
               Brazil / 巴西 / BR three countries; the whole point of grouping
               is that it cannot happen again. Names come from the browser, so
               this list reads in whatever language the user is in. -->
          <el-select
            v-model="form.countryCode"
            filterable
            clearable
            :placeholder="t('customers.countryPick')"
            style="width: 220px"
          >
            <el-option v-for="c in countries" :key="c.code" :value="c.code" :label="c.name" />
          </el-select>
        </el-form-item>
        <el-form-item :label="t('customers.currency')">
          <el-select v-model="form.currency" style="width: 140px">
            <el-option v-for="c in CURRENCIES" :key="c" :value="c" :label="c" />
          </el-select>
        </el-form-item>
        <el-form-item :label="t('customers.timezone')">
          <el-select
            v-model="form.timezone"
            filterable
            clearable
            :placeholder="t('customers.timezonePick')"
            style="width: 100%"
          >
            <el-option v-for="zone in timezoneOptions" :key="zone" :label="zone" :value="zone" />
          </el-select>
          <div class="timezone-help">{{ t('customers.timezoneHelp') }}</div>
        </el-form-item>
        <el-form-item :label="t('customers.paymentTerm')">
          <el-select v-model="form.paymentTerm" style="width: 200px" clearable>
            <el-option v-for="o in paymentOptions" :key="o.code" :value="o.code" :label="o.label" />
          </el-select>
        </el-form-item>
        <el-form-item :label="t('customers.address')">
          <el-input v-model="form.address" type="textarea" :rows="2" />
        </el-form-item>
        <el-form-item :label="t('customers.remark')">
          <el-input v-model="form.remark" type="textarea" :rows="2" />
        </el-form-item>
        <el-divider content-position="left">
          {{ t('customers.contact') }}
          <span class="hint">{{ t('customers.contactOptional') }}</span>
        </el-divider>
        <el-form-item :label="t('customers.contactName')">
          <el-input v-model="form.contactName" style="width: 220px" placeholder="Hans Weber" />
        </el-form-item>
        <el-form-item :label="t('customers.contactPhone')">
          <el-select
            v-model="form.contactDial"
            class="dial-select"
            filterable
            clearable
            :placeholder="t('customers.dialCode')"
          >
            <!-- Options carry both the code and the country names so typing
                 either one finds the entry; the closed field shows only the
                 code, which is all that fits next to the number. -->
            <template #label="{ value }">{{ value }}</template>
            <el-option
              v-for="d in DIAL_CODES"
              :key="d.dial"
              :value="d.dial"
              :label="`${d.dial} ${d.countries}`"
            >
              <span class="dial-row">
                <span>{{ d.dial }}</span>
                <span class="dial-country">{{ d.countries }}</span>
              </span>
            </el-option>
          </el-select>
          <el-input v-model="form.contactPhone" class="phone-input" placeholder="138 0013 8000" />
        </el-form-item>
        <el-form-item :label="t('customers.contactEmail')">
          <el-input v-model="form.contactEmail" style="width: 300px" placeholder="name@example.com" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="dialogOpen = false">{{ t('common.cancel') }}</el-button>
        <el-button type="primary" :loading="saving" @click="save">{{ t('common.save') }}</el-button>
      </template>
    </el-dialog>
    <ImportCustomersDialog v-model:open="importOpen" @imported="changeScope" />
    <BulkOwnerDialog v-model:open="bulkOwnerOpen" entity-type="customer" :action="bulkOwnerAction" :selected-ids="selectedCustomers.map(row=>row.id)" @saved="bulkOwnersSaved" />
  </div>
</template>

<script setup lang="ts">
import ReorderableTableHeader from "../components/ReorderableTableHeader.vue"
import { useTableColumnOrder } from "../composables/useTableColumnOrder"
import { computed, nextTick, onMounted, reactive, ref, watch } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { useI18n } from 'vue-i18n'
import { useRouter } from 'vue-router'
import { del, get, post, put } from '../api'
import { CURRENCIES } from '../constants'
import { DIAL_CODES, dialCodeOfCode, splitPhone } from '../constants'
import { countryName, countryOptions } from '../lib/countries'
import { validateCustomerContact, validateCustomerProfile } from '../lib/customerForms'
import { portTimezoneOptions } from '../lib/portOptions'
import { useAuthStore } from '../stores/auth'
import { confirmPossibleDuplicates } from '../lib/masterDataDuplicates'
import { promptActivationReason } from '../lib/masterDataLifecycle'
import ImportCustomersDialog from '../components/ImportCustomersDialog.vue'
import BulkOwnerDialog from '../components/masterdata/BulkOwnerDialog.vue'

interface Contact {
  name: string
  title: string
  email: string
  phone: string
  isPrimary: boolean
}
interface Customer {
  id: string
  code: string
  name: string
  country: string
  countryCode: string
  address: string
  currency: string
  paymentTerm: string
  remark: string
  status: string
  contacts: Contact[]
  shortName?: string
  englishName?: string
  customerType?: string
  businessStatus?: string
  timezone?: string
  primaryContactName?: string
  owners?: { employeeName: string }[]
}
interface OptionItem { code: string; label: string }
interface CountryGroup { code: string; customerCount: string }

const EMPTY_FORM = {
  code: '', name: '', country: '', countryCode: '', currency: 'USD', paymentTerm: '',
  address: '', remark: '',
  contactName: '', contactDial: '', contactPhone: '', contactEmail: '', timezone: '',
}

const { t, locale } = useI18n()
const router = useRouter()
// Built once per language rather than per render: the list is 249 entries and
// sorting it by the reader's collation is the expensive part.
const countries = computed(() => countryOptions(locale.value))
const auth = useAuthStore()
const canDelete = ref(false)
const canManageOwners = ref(false)
const layoutTenant = ref('')
const columnOrder = useTableColumnOrder(() => `tenant:${layoutTenant.value}:customer-list`, [{"key": "name", "label": "名称", "minWidth": 235}, {"key": "country", "label": "国家/地区", "minWidth": 125}, {"key": "type", "label": "客户类型", "minWidth": 110}, {"key": "contact", "label": "主要联系人", "minWidth": 140}, {"key": "owners", "label": "负责人", "minWidth": 175}, {"key": "status", "label": "状态", "minWidth": 80}, {"key": "actions", "label": "操作", "minWidth": 100}])
const customers = ref<Customer[]>([])
const countryGroups = ref<CountryGroup[]>([])
const paymentOptions = ref<OptionItem[]>([])
const typeOptions = ref<OptionItem[]>([])
const total = ref(0)
const page = ref(1)
const pageSize = ref(100)
const customerTable = ref<any>()
const selectedCustomers = ref<Customer[]>([])
const bulkOwnerOpen = ref(false)
const bulkOwnerAction = ref<'ADD'|'REMOVE'>('ADD')
const allPageSelected = computed(()=>customers.value.length>0&&selectedCustomers.value.length===customers.value.length)
const keyword = ref('')
const showInactive = ref(false)
const loading = ref(false)
const countryLoading = ref(false)
const selectedCountry = ref('')
const customerType = ref('')
const businessStatus = ref('')
const importOpen = ref(false)
const dialogOpen = ref(false)
const saving = ref(false)
const loadingDetail = ref(false)
// null = the dialog is creating; an id = it is editing that customer.
const editingId = ref<string | null>(null)
// An update replaces the whole contact list, so contacts the dialog does not
// show (secondary ones added through the API) are carried over untouched.
const otherContacts = ref<Contact[]>([])
const form = reactive({ ...EMPTY_FORM })
const countryTotal = computed(() => countryGroups.value.reduce(
  (total, group) => total + Number(group.customerCount), 0,
))
const displayedCountryGroups = computed(() => [...countryGroups.value].sort((a, b) => {
  if (!a.code) return 1
  if (!b.code) return -1
  return countryName(a.code, locale.value).localeCompare(countryName(b.code, locale.value), locale.value)
}))
const timezoneOptions = computed(() => portTimezoneOptions(form.countryCode))
const selectedCountrySummary = computed(() => {
  if (!selectedCountry.value) return `${t('customers.allCountries')} · ${countryTotal.value}`
  const group = countryGroups.value.find(item => (item.code || '__UNCLASSIFIED__') === selectedCountry.value)
  const name = selectedCountry.value === '__UNCLASSIFIED__'
    ? t('customers.unclassified')
    : countryName(selectedCountry.value, locale.value)
  return `${name} · ${Number(group?.customerCount || 0)}`
})

// Picking a country pre-fills the matching calling code. A code the user chose
// themselves is never overwritten — only an empty one, or one that still
// matches the previously selected country. Loading an existing customer is not
// a choice, so it must not rewrite a phone the record already has.
const hydrating = ref(false)
watch(
  () => form.countryCode,
  (code, previous) => {
    if (hydrating.value) return
    const next = dialCodeOfCode(code)
    if (next && (!form.contactDial || form.contactDial === dialCodeOfCode(previous ?? ''))) {
      form.contactDial = next
    }
  },
)

async function load() {
  clearSelection()
  loading.value = true
  try {
    const data = await get<{ customers: Customer[]; meta: { total: string } }>('/customers', {
      page: page.value, page_size: pageSize.value, keyword: keyword.value,
      status: showInactive.value ? 'ALL' : '',
      country_code: selectedCountry.value,
      customer_type: customerType.value,
      business_status: businessStatus.value,
    })
    customers.value = data.customers
    total.value = Number(data.meta.total)
  } finally {
    loading.value = false
  }
}

async function loadCountryGroups() {
  countryLoading.value = true
  try {
    const data = await get<{ countries: CountryGroup[] }>('/customers/countries', {
      status: showInactive.value ? 'ALL' : '',
    })
    countryGroups.value = data.countries ?? []
  } finally {
    countryLoading.value = false
  }
}

function selectCountry(code: string) {
  selectedCountry.value = code
  page.value = 1
  void load()
}

function changeScope() {
  page.value = 1
  Promise.all([load(), loadCountryGroups()])
}
function changeFilters(){page.value=1;load()}
function openDetail(row:Customer){router.push(`/basic/customers/${row.id}`)}
function handleCustomerRowClick(row:Customer,column:any){if(column?.type==='selection')return;openDetail(row)}
function clearSelection(){selectedCustomers.value=[];customerTable.value?.clearSelection()}
function toggleSelectPage(){if(allPageSelected.value)clearSelection();else customerTable.value?.toggleAllSelection()}
function openBulkOwners(action:'ADD'|'REMOVE'){bulkOwnerAction.value=action;bulkOwnerOpen.value=true}
async function bulkOwnersSaved(){clearSelection();await load()}
function changePage(p:number){page.value=p;void load()}
function changePageSize(size:number){pageSize.value=size;page.value=1;void load()}
function handleRowCommand(row: Customer, command: string) {
  if (command === 'view') openDetail(row)
  else if (command === 'delete') void deleteCustomer(row)
  else if (command === 'activate') void activate(row)
}
function optionLabel(list:OptionItem[],code?:string){return list.find(o=>o.code===code)?.label||code||'—'}

function openCreate() {
  // The code is left blank on purpose: masterdata issues it when the customer
  // is actually saved, so opening and abandoning this dialog costs no number.
  // Typing one here still wins, for companies with their own conventions.
  editingId.value = null
  otherContacts.value = []
  Object.assign(form, EMPTY_FORM)
  dialogOpen.value = true
}

async function openEdit(row: Customer) {
  editingId.value = row.id
  otherContacts.value = []
  Object.assign(form, EMPTY_FORM)
  dialogOpen.value = true
  loadingDetail.value = true
  try {
    // The list row carries no contacts, address or remark - fetch the detail.
    const { customer } = await get<{ customer: Customer }>(`/customers/${row.id}`)
    const [primary, ...rest] = [...customer.contacts].sort(
      (a, b) => Number(b.isPrimary) - Number(a.isPrimary),
    )
    otherContacts.value = rest
    const phone = splitPhone(primary?.phone ?? '')
    hydrating.value = true
    Object.assign(form, {
      code: customer.code, name: customer.name, country: customer.country,
      countryCode: customer.countryCode ?? '',
      currency: customer.currency, paymentTerm: customer.paymentTerm,
      address: customer.address, remark: customer.remark,
      timezone: customer.timezone ?? '',
      contactName: primary?.name ?? '',
      contactDial: phone.dial, contactPhone: phone.number,
      contactEmail: primary?.email ?? '',
    })
    await nextTick()
    hydrating.value = false
  } catch {
    hydrating.value = false
    dialogOpen.value = false // the interceptor already surfaced the reason
  } finally {
    loadingDetail.value = false
  }
}

async function save() {
  if (!form.name) {
    ElMessage.warning(t('customers.required'))
    return
  }
  // The calling code is stored together with the number so the phone stays
  // dialable from anywhere; a bare code with no number is not a phone.
  const phone = form.contactPhone ? `${form.contactDial} ${form.contactPhone}`.trim() : ''
  if (form.contactName) {
    const error = validateCustomerContact({ name: form.contactName, email: form.contactEmail, phone })
    if (error) {
      ElMessage.warning(t(`customers.${error}`))
      return
    }
  }
  if (validateCustomerProfile({ timezone: form.timezone })) {
    ElMessage.warning(t('customers.timezoneInvalid'))
    return
  }
  saving.value = true
  const primary = form.contactName
    ? [{ name: form.contactName, phone, email: form.contactEmail, isPrimary: true }]
    : []
  const body = {
    name: form.name, country: form.country, countryCode: form.countryCode,
    address: form.address,
    currency: form.currency, paymentTerm: form.paymentTerm, remark: form.remark,
    timezone: form.timezone,
    contacts: [...primary, ...otherContacts.value],
  }
  try {
    const duplicateResult = await get<any>('/customers/duplicates', {
      name: form.name,
      email: form.contactEmail,
      exclude_id: editingId.value || undefined,
    })
    await confirmPossibleDuplicates(duplicateResult.candidates || [], t)
    if (editingId.value) {
      await put(`/customers/${editingId.value}`, body)
      ElMessage.success(t('customers.updated'))
    } else {
      // Code only travels on create; it is immutable afterwards.
      await post('/customers', { ...body, code: form.code })
      ElMessage.success(t('customers.created'))
    }
    dialogOpen.value = false
    await Promise.all([load(), loadCountryGroups()])
  } finally {
    saving.value = false
  }
}

async function deleteCustomer(row: Customer) {
  if (!canDelete.value) return
  try {
    await ElMessageBox.confirm('确定删除该客户吗？', '删除客户', { type: 'warning' })
  } catch { return }
  await del(`/customers/${row.id}`, { reason: '删除客户' })
  ElMessage.success('客户已删除')
  changeScope()
}

async function activate(row: Customer) {
  const reason = await promptActivationReason(row.name, t)
  await post(`/customers/${row.id}/activate`, { reason })
  ElMessage.success(t('customers.activated'))
  await Promise.all([load(), loadCountryGroups()])
}

onMounted(async () => {
  const access = await get<{ canDelete: boolean; canManageOwners:boolean; tenantId: string }>('/customers/access')
  canDelete.value = access.canDelete; canManageOwners.value=Boolean(access.canManageOwners); layoutTenant.value = String(access.tenantId)
  await Promise.all([load(), loadCountryGroups()])
  paymentOptions.value = (
    await get<{ options: OptionItem[] }>('/options', { category: 'PAYMENT_METHOD' })
  ).options
  typeOptions.value = (await get<{ options: OptionItem[] }>('/options', { category: 'CUSTOMER_TYPE' })).options
})
</script>

<style scoped>
.filters{display:flex;align-items:center;flex-wrap:wrap;gap:10px;margin-bottom:12px}.filter-actions{display:flex;align-items:center;gap:10px;margin-left:auto;white-space:nowrap}.filter-actions .el-button+.el-button{margin-left:0}.filter-search{width:min(300px,32%)}.filter-select{width:150px}.customer-workspace{display:grid;grid-template-columns:200px minmax(0,1fr);gap:14px;align-items:start}.customer-workspace.collapsed{grid-template-columns:64px minmax(0,1fr)}.country-panel{position:sticky;top:16px}.country-panel :deep(.el-card__body){padding:10px}.country-panel__title{display:flex;align-items:center;justify-content:space-between;padding:4px 8px 10px;color:var(--el-text-color-secondary);font-size:13px;font-weight:600}.country-panel__title button{width:26px;height:26px;border:0;border-radius:6px;background:var(--el-fill-color);cursor:pointer;color:var(--el-text-color-secondary);font-size:20px}.country-item{width:100%;min-height:38px;display:flex;align-items:center;justify-content:space-between;gap:10px;border:0;border-radius:8px;padding:7px 9px;color:var(--el-text-color-regular);background:transparent;cursor:pointer;text-align:left}.country-item:hover{background:var(--el-fill-color-light)}.country-item.active{color:var(--el-color-primary);background:var(--el-color-primary-light-9)}.country-item strong{min-width:28px;padding:2px 7px;border-radius:999px;color:inherit;background:var(--el-fill-color);font-size:12px;text-align:center}.customer-list{min-width:0}.customer-list :deep(.el-card__body){padding:16px 18px}.customer-table{width:100%;--el-table-row-hover-bg-color:var(--el-fill-color-light)}.customer-table :deep(.el-table__row){cursor:pointer}.customer-table :deep(th.el-table__cell){height:42px;padding:6px 0;color:var(--el-text-color-secondary);font-size:13px}.customer-table :deep(td.el-table__cell){padding:11px 0}.customer-identity{display:flex;min-width:0;flex-direction:column;align-items:flex-start;gap:3px;padding:0;border:0;background:transparent;color:inherit;text-align:left;cursor:pointer}.customer-identity strong{max-width:100%;overflow:hidden;color:var(--el-text-color-primary);font-size:14px;text-overflow:ellipsis;white-space:nowrap}.customer-identity span{max-width:100%;overflow:hidden;color:var(--el-text-color-secondary);font-size:12px;text-overflow:ellipsis;white-space:nowrap}.muted,.stale-country{color:var(--el-text-color-secondary)}.customer-cards{display:none}.mobile-country{display:none;margin-bottom:10px}.mobile-country__title{display:flex;flex-direction:column;gap:2px;white-space:nowrap}.mobile-country__title span{color:var(--el-text-color-secondary);font-size:11px}.mobile-country__title strong{font-size:14px;font-weight:600}.country-select{width:100%}.pager{margin-top:12px;justify-content:flex-end}.hint{margin-left:8px;font-weight:400;font-size:12px;color:var(--el-text-color-secondary)}.dial-select{width:118px}.phone-input{width:200px;margin-left:8px}.dial-row{display:flex;justify-content:space-between;gap:18px}.dial-country{font-size:12px;color:var(--el-text-color-secondary)}.timezone-help{width:100%;margin-top:4px;color:var(--el-text-color-secondary);font-size:12px;line-height:1.4}
.bulk-owner-bar{display:flex;align-items:center;gap:10px;margin:0 0 12px;padding:10px 12px;border:1px solid var(--el-color-primary-light-7);border-radius:8px;background:var(--el-color-primary-light-9)}.bulk-owner-bar span{margin-right:auto;color:var(--el-text-color-regular)}
@media(max-width:1450px){.customer-workspace,.customer-workspace.collapsed{grid-template-columns:1fr}.country-panel{display:none}.mobile-country{display:flex;align-items:center;justify-content:space-between;gap:18px;padding:11px 13px;border:1px solid var(--el-border-color-lighter);border-radius:10px;background:var(--el-bg-color)}.country-select{max-width:300px}}
@media(max-width:820px){.customer-table{display:none}.customer-cards{display:grid;grid-template-columns:1fr;gap:10px}.customer-card{position:relative;display:grid;gap:9px;padding:14px 74px 14px 15px;border:1px solid var(--el-border-color-lighter);border-radius:10px;background:var(--el-bg-color);cursor:pointer;outline:none}.customer-card:hover,.customer-card:focus-visible{border-color:var(--el-color-primary-light-5);box-shadow:0 3px 12px rgb(31 69 89 / 8%)}.customer-card__head{display:flex;align-items:flex-start;justify-content:space-between;gap:12px}.customer-card__identity{display:flex;min-width:0;flex-direction:column;gap:3px}.customer-card__identity strong{overflow:hidden;text-overflow:ellipsis;white-space:nowrap}.customer-card__identity span{color:var(--el-color-primary);font-size:12px}.customer-card__summary{display:flex;flex-wrap:wrap;gap:6px 14px;color:var(--el-text-color-regular);font-size:13px}.customer-card__summary span+span:before{margin-right:14px;color:var(--el-border-color);content:'·'}.customer-card__facts{display:grid;grid-template-columns:repeat(2,minmax(0,1fr));gap:10px;margin:0}.customer-card__facts div{min-width:0}.customer-card__facts dt{margin-bottom:2px;color:var(--el-text-color-secondary);font-size:11px}.customer-card__facts dd{overflow:hidden;margin:0;font-size:13px;text-overflow:ellipsis;white-space:nowrap}.customer-card__actions{position:absolute;right:14px;bottom:12px}}
@media(max-width:760px){.mobile-country{align-items:stretch;flex-direction:column;gap:8px}.country-select{max-width:none}.mobile-country__title{flex-direction:row;align-items:baseline;justify-content:space-between}.filters{display:grid;grid-template-columns:minmax(0,1fr) auto}.filter-search{width:100%}.filter-select{width:100%}.filters .filter-select{grid-column:span 1}.filters .el-checkbox{grid-column:1/-1}.filter-actions{grid-column:1/-1;justify-content:flex-end;margin-left:0}.customer-list :deep(.el-card__body){padding:12px}.customer-cards{grid-template-columns:1fr}.customer-card__facts{grid-template-columns:1fr 1fr}.pager{justify-content:center}.pager :deep(.el-pagination__total){display:none}}
@media(max-width:480px){.filters{grid-template-columns:1fr}.filters>*{grid-column:1!important}.filter-actions{justify-content:stretch}.filter-actions .el-button{flex:1}.customer-card{padding:13px 58px 13px 13px}.customer-card__facts{grid-template-columns:1fr}.customer-card__actions{right:12px}}
.filter-end{display:flex;align-items:center;gap:10px;margin-left:auto;white-space:nowrap}.filter-end .filter-actions{margin-left:0}
@media(max-width:760px){.filter-end{grid-column:1/-1;justify-content:space-between;margin-left:0}}
@media(max-width:480px){.filter-end{flex-wrap:wrap}.filter-end .filter-actions{width:100%;margin-left:0}}
.customer-workspace{display:block}.mobile-country{display:flex;align-items:center;justify-content:space-between;gap:12px;padding:9px 12px;margin-bottom:10px;border:1px solid var(--el-border-color-lighter);border-radius:10px;background:var(--el-bg-color)}.country-select{width:min(320px,45%);max-width:none}.customer-list :deep(.el-card__body){padding:12px 14px}.filters{gap:8px;margin-bottom:10px}.filter-search{width:min(300px,26%)}.filter-select{width:135px}.bulk-owner-bar{gap:8px;padding:7px 10px;margin-bottom:10px}.customer-table :deep(th.el-table__cell){height:36px;padding:4px 0;font-size:12px}.customer-table :deep(td.el-table__cell){padding:6px 0;font-size:12px}.customer-identity{gap:1px}.customer-identity strong{font-size:13px}.customer-identity span{font-size:11px}.pager{margin-top:8px}
@media(max-width:820px){.customer-table{display:block}.customer-cards{display:none}.filter-search{width:100%}.mobile-country{flex-direction:row;align-items:center}.mobile-country__title{flex-direction:column}.country-select{width:min(320px,55%)}}
@media(max-width:480px){.mobile-country{align-items:stretch;flex-direction:column}.country-select{width:100%}.mobile-country__title{flex-direction:row}.filters{grid-template-columns:minmax(0,1fr) auto}}
</style>
