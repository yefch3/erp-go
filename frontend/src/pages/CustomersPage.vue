<template>
  <div>
    <div class="page-head">
      <h2>客户管理</h2>
      <el-button type="primary" @click="openCreate">新建客户</el-button>
    </div>

    <el-card shadow="never">
      <div class="filters">
        <el-input
          v-model="keyword"
          placeholder="搜索名称或编码"
          clearable
          style="width: 260px"
          @keyup.enter="load"
          @clear="load"
        />
        <el-button @click="load">查询</el-button>
      </div>

      <el-table :data="customers" v-loading="loading">
        <el-table-column prop="code" label="编码" width="130" />
        <el-table-column prop="name" label="名称" min-width="200" />
        <el-table-column prop="country" label="国家" width="120" />
        <el-table-column prop="currency" label="币种" width="80" />
        <el-table-column prop="paymentTerm" label="付款方式" width="100" />
        <el-table-column prop="status" label="状态" width="90">
          <template #default="{ row }">
            <el-tag :type="row.status === 'ACTIVE' ? 'success' : 'info'" size="small">
              {{ row.status === 'ACTIVE' ? '启用' : '停用' }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column label="操作" width="100" fixed="right">
          <template #default="{ row }">
            <el-button
              v-if="row.status === 'ACTIVE'"
              link
              type="danger"
              @click="deactivate(row)"
            >停用</el-button>
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

    <el-dialog v-model="dialogOpen" title="新建客户" width="560px">
      <el-form :model="form" label-width="90px">
        <el-form-item label="编码" required>
          <el-input v-model="form.code" placeholder="CUST-002" />
        </el-form-item>
        <el-form-item label="名称" required>
          <el-input v-model="form.name" />
        </el-form-item>
        <el-form-item label="国家">
          <el-input v-model="form.country" />
        </el-form-item>
        <el-form-item label="币种">
          <el-select v-model="form.currency" style="width: 140px">
            <el-option value="USD" label="USD" />
            <el-option value="EUR" label="EUR" />
            <el-option value="CNY" label="CNY" />
          </el-select>
        </el-form-item>
        <el-form-item label="付款方式">
          <el-select v-model="form.paymentTerm" style="width: 200px" clearable>
            <el-option v-for="o in paymentOptions" :key="o.code" :value="o.code" :label="o.label" />
          </el-select>
        </el-form-item>
        <el-form-item label="联系人">
          <el-input v-model="form.contactName" placeholder="姓名（可选）" style="width: 160px" />
          <el-input v-model="form.contactEmail" placeholder="邮箱" style="width: 220px; margin-left: 8px" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="dialogOpen = false">取消</el-button>
        <el-button type="primary" :loading="saving" @click="save">保存</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup lang="ts">
import { onMounted, reactive, ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { del, get, post } from '../api'

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

const customers = ref<Customer[]>([])
const paymentOptions = ref<OptionItem[]>([])
const total = ref(0)
const page = ref(1)
const pageSize = 10
const keyword = ref('')
const loading = ref(false)
const dialogOpen = ref(false)
const saving = ref(false)
const form = reactive({
  code: '', name: '', country: '', currency: 'USD', paymentTerm: '',
  contactName: '', contactEmail: '',
})

async function load() {
  loading.value = true
  try {
    const data = await get<{ customers: Customer[]; meta: { total: string } }>('/customers', {
      page: page.value, page_size: pageSize, keyword: keyword.value,
    })
    customers.value = data.customers
    total.value = Number(data.meta.total)
  } finally {
    loading.value = false
  }
}

function openCreate() {
  Object.assign(form, { code: '', name: '', country: '', currency: 'USD', paymentTerm: '', contactName: '', contactEmail: '' })
  dialogOpen.value = true
}

async function save() {
  if (!form.code || !form.name) {
    ElMessage.warning('编码和名称必填')
    return
  }
  saving.value = true
  try {
    await post('/customers', {
      code: form.code, name: form.name, country: form.country,
      currency: form.currency, paymentTerm: form.paymentTerm,
      contacts: form.contactName
        ? [{ name: form.contactName, email: form.contactEmail, isPrimary: true }]
        : [],
    })
    ElMessage.success('客户已创建')
    dialogOpen.value = false
    load()
  } finally {
    saving.value = false
  }
}

async function deactivate(row: Customer) {
  await ElMessageBox.confirm(`确定停用客户「${row.name}」？停用后不可在新单据中引用。`, '停用确认')
  await del(`/customers/${row.id}`)
  ElMessage.success('已停用')
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
