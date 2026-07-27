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
        <el-table-column prop="country" :label="t('customers.country')" width="120" />
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
          width="110"
          fixed="right"
        >
          <template #default="{ row }">
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

    <el-dialog v-model="dialogOpen" :title="t('customers.create')" width="560px">
      <el-form :model="form" label-width="110px">
        <el-form-item :label="t('customers.code')" required>
          <el-input v-model="form.code" placeholder="CUST-002" />
        </el-form-item>
        <el-form-item :label="t('customers.name')" required>
          <el-input v-model="form.name" />
        </el-form-item>
        <el-form-item :label="t('customers.country')">
          <el-select v-model="form.country" filterable allow-create clearable style="width: 220px">
            <el-option v-for="c in COUNTRIES" :key="c" :value="c" :label="c" />
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
        <el-form-item :label="t('customers.contact')">
          <el-input v-model="form.contactName" :placeholder="t('customers.contactName')" style="width: 130px" />
          <el-input v-model="form.contactPhone" :placeholder="t('customers.contactPhone')" style="width: 140px; margin-left: 8px" />
          <el-input v-model="form.contactEmail" :placeholder="t('customers.contactEmail')" style="width: 170px; margin-left: 8px" />
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
import { onMounted, reactive, ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { useI18n } from 'vue-i18n'
import { del, get, post } from '../api'
import { COUNTRIES } from '../constants'
import { useAuthStore } from '../stores/auth'

interface Customer {
  id: string
  code: string
  name: string
  country: string
  currency: string
  paymentTerm: string
  status: string
}
interface OptionItem { code: string; label: string }

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
const form = reactive({
  code: '', name: '', country: '', currency: 'USD', paymentTerm: '',
  contactName: '', contactPhone: '', contactEmail: '',
})

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

async function openCreate() {
  Object.assign(form, { code: '', name: '', country: '', currency: 'USD', paymentTerm: '', contactName: '', contactPhone: '', contactEmail: '' })
  dialogOpen.value = true
  // Pre-fill the code from the numbering service; the field stays editable
  // for companies with their own conventions.
  try {
    form.code = (await post<{ number: string }>('/numbering/next', { bizType: 'CUSTOMER' })).number
  } catch {
    // Numbering unavailable: leave the field empty for manual entry.
  }
}

async function save() {
  if (!form.code || !form.name) {
    ElMessage.warning(t('customers.required'))
    return
  }
  saving.value = true
  try {
    await post('/customers', {
      code: form.code, name: form.name, country: form.country,
      currency: form.currency, paymentTerm: form.paymentTerm,
      contacts: form.contactName
        ? [{ name: form.contactName, phone: form.contactPhone, email: form.contactEmail, isPrimary: true }]
        : [],
    })
    ElMessage.success(t('customers.created'))
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
</style>
