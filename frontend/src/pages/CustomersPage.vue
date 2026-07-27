<template>
  <div>
    <div class="page-head">
      <h2>{{ t('customers.title') }}</h2>
      <el-button v-if="auth.can('masterdata:customer:write')" type="primary" @click="openCreate">
        {{ t('customers.create') }}
      </el-button>
    </div>

    <el-card shadow="never">
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
        <el-checkbox v-model="showInactive" @change="load">{{ t('customers.showInactive') }}</el-checkbox>
      </div>

      <el-table :data="customers" v-loading="loading">
        <el-table-column prop="code" :label="t('customers.code')" width="130" />
        <el-table-column prop="name" :label="t('customers.name')" min-width="200" />
        <!-- Wide enough for spelled-out names such as United Arab Emirates. -->
        <el-table-column prop="country" :label="t('customers.country')" width="180" />
        <el-table-column prop="currency" :label="t('customers.currency')" width="90" />
        <el-table-column prop="paymentTerm" :label="t('customers.paymentTerm')" width="110" />
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
          width="150"
          fixed="right"
        >
          <template #default="{ row }">
            <el-button link type="primary" @click="openEdit(row)">{{ t('common.edit') }}</el-button>
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
          <el-select v-model="form.country" filterable allow-create clearable style="width: 220px">
            <el-option v-for="c in COUNTRY_NAMES" :key="c" :value="c" :label="c" />
          </el-select>
        </el-form-item>
        <el-form-item :label="t('customers.currency')">
          <el-select v-model="form.currency" style="width: 140px">
            <el-option value="USD" label="USD" />
            <el-option value="EUR" label="EUR" />
            <el-option value="CNY" label="CNY" />
          </el-select>
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
  </div>
</template>

<script setup lang="ts">
import { nextTick, onMounted, reactive, ref, watch } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { useI18n } from 'vue-i18n'
import { del, get, post, put } from '../api'
import { COUNTRY_NAMES, DIAL_CODES, dialCodeOf, splitPhone } from '../constants'
import { useAuthStore } from '../stores/auth'

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
  address: string
  currency: string
  paymentTerm: string
  remark: string
  status: string
  contacts: Contact[]
}
interface OptionItem { code: string; label: string }

const EMPTY_FORM = {
  code: '', name: '', country: '', currency: 'USD', paymentTerm: '',
  address: '', remark: '',
  contactName: '', contactDial: '', contactPhone: '', contactEmail: '',
}

const { t } = useI18n()
const auth = useAuthStore()
const customers = ref<Customer[]>([])
const paymentOptions = ref<OptionItem[]>([])
const total = ref(0)
const page = ref(1)
const pageSize = 10
const keyword = ref('')
const showInactive = ref(false)
const loading = ref(false)
const dialogOpen = ref(false)
const saving = ref(false)
const loadingDetail = ref(false)
// null = the dialog is creating; an id = it is editing that customer.
const editingId = ref<string | null>(null)
// An update replaces the whole contact list, so contacts the dialog does not
// show (secondary ones added through the API) are carried over untouched.
const otherContacts = ref<Contact[]>([])
const form = reactive({ ...EMPTY_FORM })

// Picking a country pre-fills the matching calling code. A code the user chose
// themselves is never overwritten — only an empty one, or one that still
// matches the previously selected country. Loading an existing customer is not
// a choice, so it must not rewrite a phone the record already has.
const hydrating = ref(false)
watch(
  () => form.country,
  (country, previous) => {
    if (hydrating.value) return
    const next = dialCodeOf(country)
    if (next && (!form.contactDial || form.contactDial === dialCodeOf(previous ?? ''))) {
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
    })
    customers.value = data.customers
    total.value = Number(data.meta.total)
  } finally {
    loading.value = false
  }
}

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
      currency: customer.currency, paymentTerm: customer.paymentTerm,
      address: customer.address, remark: customer.remark,
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
  saving.value = true
  // The calling code is stored together with the number so the phone stays
  // dialable from anywhere; a bare code with no number is not a phone.
  const phone = form.contactPhone ? `${form.contactDial} ${form.contactPhone}`.trim() : ''
  const primary = form.contactName
    ? [{ name: form.contactName, phone, email: form.contactEmail, isPrimary: true }]
    : []
  const body = {
    name: form.name, country: form.country, address: form.address,
    currency: form.currency, paymentTerm: form.paymentTerm, remark: form.remark,
    contacts: [...primary, ...otherContacts.value],
  }
  try {
    if (editingId.value) {
      await put(`/customers/${editingId.value}`, body)
      ElMessage.success(t('customers.updated'))
    } else {
      // Code only travels on create; it is immutable afterwards.
      await post('/customers', { ...body, code: form.code })
      ElMessage.success(t('customers.created'))
    }
    dialogOpen.value = false
    load()
  } finally {
    saving.value = false
  }
}

async function deactivate(row: Customer) {
  await ElMessageBox.confirm(
    t('customers.confirmDeactivate', { name: row.name }),
    t('customers.confirmTitle'),
  )
  await del(`/customers/${row.id}`)
  ElMessage.success(t('customers.deactivated'))
  load()
}

async function activate(row: Customer) {
  await post(`/customers/${row.id}/activate`)
  ElMessage.success(t('customers.activated'))
  load()
}

onMounted(async () => {
  load()
  paymentOptions.value = (
    await get<{ options: OptionItem[] }>('/options', { category: 'PAYMENT_METHOD' })
  ).options
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
</style>
