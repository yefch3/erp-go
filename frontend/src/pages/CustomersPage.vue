<template>
  <div>
    <div class="page-head">
      <div><h2>{{ t('customers.title') }}</h2><p>{{ t('customers.subtitle') }}</p></div>
      <div class="head-actions"><el-button v-if="auth.can('masterdata:customer:write')" @click="importOpen=true">{{ t('customers.bulkImport') }}</el-button><el-button v-if="auth.can('masterdata:customer:write')" type="primary" @click="openCreate">{{ t('customers.create') }}</el-button></div>
    </div>

    <div class="mobile-country"><el-select v-model="selectedCountry" @change="selectCountry"><el-option :label="t('customers.allCountries')" value=""/><el-option v-for="group in displayedCountryGroups" :key="group.code||'none'" :label="group.code ? countryName(group.code, locale) : t('customers.unclassified')" :value="group.code||'__UNCLASSIFIED__'"/></el-select></div>
    <div class="customer-workspace" :class="{ collapsed: countryCollapsed }">
      <el-card class="country-panel" shadow="never" v-loading="countryLoading">
        <div class="country-panel__title"><span v-if="!countryCollapsed">{{ t('customers.countryGroups') }}</span><button type="button" @click="countryCollapsed=!countryCollapsed">{{ countryCollapsed ? '›' : '‹' }}</button></div>
        <button
          class="country-item"
          :class="{ active: selectedCountry === '' }"
          type="button"
          @click="selectCountry('')"
        >
          <span v-if="!countryCollapsed">{{ t('customers.allCountries') }}</span><span v-else>{{ t('customers.allShort') }}</span><strong v-if="!countryCollapsed">{{ countryTotal }}</strong>
        </button>
        <button
          v-for="group in displayedCountryGroups"
          :key="group.code || '__UNCLASSIFIED__'"
          class="country-item"
          :class="{ active: selectedCountry === (group.code || '__UNCLASSIFIED__') }"
          type="button"
          @click="selectCountry(group.code || '__UNCLASSIFIED__')"
        >
          <span v-if="!countryCollapsed">{{ group.code ? countryName(group.code, locale) : t('customers.unclassified') }}</span><span v-else>{{ group.code || '—' }}</span><strong v-if="!countryCollapsed">{{ Number(group.customerCount) }}</strong>
        </button>
      </el-card>

      <el-card class="customer-list" shadow="never">
      <div class="filters">
        <el-input
          v-model="keyword"
          :placeholder="t('customers.searchPlaceholder')"
          clearable
          style="width: 260px"
          @keyup.enter="load"
          @clear="load"
        />
        <el-button @click="load">{{ t('common.query') }}</el-button>
        <el-select v-model="customerType" clearable :placeholder="t('customers.type')" style="width:140px" @change="changeFilters"><el-option v-for="o in typeOptions" :key="o.code" :label="o.label" :value="o.code" /></el-select>
        <el-select v-model="businessStatus" clearable :placeholder="t('customers.businessStatus')" style="width:140px" @change="changeFilters"><el-option :label="t('customers.statusProspect')" value="PROSPECT"/><el-option :label="t('customers.statusCooperating')" value="COOPERATING"/><el-option :label="t('customers.statusPaused')" value="PAUSED"/><el-option :label="t('customers.statusInactive')" value="INACTIVE"/></el-select>
        <el-checkbox v-model="showInactive" @change="changeScope">{{ t('customers.showInactive') }}</el-checkbox>
      </div>

      <el-table :data="customers" v-loading="loading">
        <el-table-column :label="t('customers.code')" width="145"><template #default="{row}"><el-button link type="primary" @click="openDetail(row)">{{row.code}}</el-button></template></el-table-column>
        <el-table-column :label="t('customers.name')" min-width="190"><template #default="{row}"><div class="customer-name"><strong>{{row.name}}</strong><small>{{row.shortName||row.englishName}}</small></div></template></el-table-column>
        <!-- Wide enough for spelled-out names such as United Arab Emirates. -->
        <el-table-column :label="t('customers.country')" width="180">
          <template #default="{ row }">
            <span v-if="row.countryCode">{{ countryName(row.countryCode, locale) }}</span>
            <!-- A row imported before country codes existed, or one whose free
                 text matched nothing. Shown rather than blanked, because it is
                 exactly the row that keeps it out of a country group. -->
            <span v-else-if="row.country" class="stale-country">
              {{ row.country }} · {{ t('customers.countryUnmapped') }}
            </span>
            <span v-else class="stale-country">{{ t('customers.countryUnset') }}</span>
          </template>
        </el-table-column>
        <el-table-column :label="t('customers.type')" width="120"><template #default="{row}">{{ optionLabel(typeOptions,row.customerType) }}</template></el-table-column>
        <el-table-column :label="t('customers.primaryContact')" width="130"><template #default="{row}">{{row.primaryContactName||'—'}}</template></el-table-column>
        <el-table-column :label="t('customers.owners')" min-width="150"><template #default="{row}"><span v-if="row.owners?.length">{{row.owners.map((o:any)=>o.employeeName).join('、')}}</span><span v-else>—</span></template></el-table-column>
        <el-table-column :label="t('common.status')" width="100">
          <template #default="{ row }">
            <el-tag :type="row.status === 'ACTIVE' ? 'success' : 'info'" size="small">
              {{ row.status === 'ACTIVE' ? t('common.active') : t('common.inactive') }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column
          v-if="auth.can('masterdata:customer:write')"
          :label="t('common.actions')"
          width="165"
          fixed="right"
        >
          <template #default="{ row }">
            <el-button link type="primary" @click="openDetail(row)">{{ t('customers.viewDetails') }}</el-button>
            <el-button
              v-if="row.status === 'ACTIVE'"
              link
              type="danger"
              @click="deactivate(row)"
            >{{ t('common.deactivate') }}</el-button>
            <el-button
              v-else
              link
              type="primary"
              @click="activate(row)"
            >{{ t('common.activate') }}</el-button>
          </template>
        </el-table-column>
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
  </div>
</template>

<script setup lang="ts">
import { computed, nextTick, onMounted, reactive, ref, watch } from 'vue'
import { ElMessage } from 'element-plus'
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
import { confirmDeactivation, promptActivationReason } from '../lib/masterDataLifecycle'
import ImportCustomersDialog from '../components/ImportCustomersDialog.vue'

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
const customers = ref<Customer[]>([])
const countryGroups = ref<CountryGroup[]>([])
const paymentOptions = ref<OptionItem[]>([])
const typeOptions = ref<OptionItem[]>([])
const total = ref(0)
const page = ref(1)
const pageSize = 10
const keyword = ref('')
const showInactive = ref(false)
const loading = ref(false)
const countryLoading = ref(false)
const selectedCountry = ref('')
const customerType = ref('')
const businessStatus = ref('')
const countryCollapsed = ref(false)
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
  loading.value = true
  try {
    const data = await get<{ customers: Customer[]; meta: { total: string } }>('/customers', {
      page: page.value, page_size: pageSize, keyword: keyword.value,
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
  if (selectedCountry.value === code) return
  selectedCountry.value = code
  page.value = 1
  load()
}

function changeScope() {
  page.value = 1
  Promise.all([load(), loadCountryGroups()])
}
function changeFilters(){page.value=1;load()}
function openDetail(row:Customer){router.push(`/basic/customers/${row.id}`)}
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

async function deactivate(row: Customer) {
  const reason = await confirmDeactivation(`/customers/${row.id}/deactivation-impact`, row.name, t)
  await del(`/customers/${row.id}`, { reason })
  ElMessage.success(t('customers.deactivated'))
  await Promise.all([load(), loadCountryGroups()])
}

async function activate(row: Customer) {
  const reason = await promptActivationReason(row.name, t)
  await post(`/customers/${row.id}/activate`, { reason })
  ElMessage.success(t('customers.activated'))
  await Promise.all([load(), loadCountryGroups()])
}

onMounted(async () => {
  await Promise.all([load(), loadCountryGroups()])
  paymentOptions.value = (
    await get<{ options: OptionItem[] }>('/options', { category: 'PAYMENT_METHOD' })
  ).options
  typeOptions.value = (await get<{ options: OptionItem[] }>('/options', { category: 'CUSTOMER_TYPE' })).options
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
  font-size: 18px;
  font-weight: 500;
  margin: 0;
}
.filters {
  display: flex;
  gap: 10px;
  margin-bottom: 14px;
}
.page-head p{margin:5px 0 0;color:var(--el-text-color-secondary);font-size:13px}.head-actions{display:flex;gap:10px}
.customer-workspace {
  display: grid;
  grid-template-columns: 220px minmax(0, 1fr);
  gap: 16px;
  align-items: start;
}
.customer-workspace.collapsed{grid-template-columns:72px minmax(0,1fr)}
.country-panel {
  position: sticky;
  top: 16px;
}
.country-panel :deep(.el-card__body) {
  padding: 12px;
}
.country-panel__title {
  padding: 4px 10px 12px;
  color: var(--el-text-color-secondary);
  font-size: 13px;
  font-weight: 600;
}
.country-panel__title{display:flex;align-items:center;justify-content:space-between}.country-panel__title button{border:0;border-radius:6px;background:var(--el-fill-color);cursor:pointer;color:var(--el-text-color-secondary);font-size:20px}
.country-item {
  width: 100%;
  min-height: 40px;
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  border: 0;
  border-radius: 8px;
  padding: 8px 10px;
  color: var(--el-text-color-regular);
  background: transparent;
  cursor: pointer;
  text-align: left;
}
.country-item:hover {
  background: var(--el-fill-color-light);
}
.country-item.active {
  color: var(--el-color-primary);
  background: var(--el-color-primary-light-9);
}
.country-item strong {
  min-width: 28px;
  padding: 2px 7px;
  border-radius: 999px;
  color: inherit;
  background: var(--el-fill-color);
  font-size: 12px;
  text-align: center;
}
.customer-list {
  min-width: 0;
}
.customer-name{display:flex;flex-direction:column;gap:3px}.customer-name small{color:var(--el-text-color-secondary)}.mobile-country{display:none;margin-bottom:12px}
@media (max-width: 900px) {
  .customer-workspace {
    grid-template-columns: 1fr;
  }
  .country-panel {
    display:none;
  }
  .mobile-country{display:block}.filters{flex-wrap:wrap}
}
.pager {
  margin-top: 14px;
  justify-content: flex-end;
}
.hint {
  margin-left: 8px;
  font-weight: 400;
  font-size: 12px;
  color: var(--el-text-color-secondary);
}
.dial-select {
  width: 118px;
}
.phone-input {
  width: 200px;
  margin-left: 8px;
}
.dial-row {
  display: flex;
  justify-content: space-between;
  gap: 18px;
}
.dial-country {
  font-size: 12px;
  color: var(--el-text-color-secondary);
}
.timezone-help {
  width: 100%;
  margin-top: 4px;
  color: var(--el-text-color-secondary);
  font-size: 12px;
  line-height: 1.4;
}
</style>
