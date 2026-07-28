<template>
  <div>
    <div class="page-head">
      <h2>{{ t('employees.title') }}</h2>
      <el-button v-if="canWrite" type="primary" @click="openCreate">{{ t('employees.create') }}</el-button>
    </div>

    <el-card shadow="never">
      <div class="filters">
        <el-input
          v-model="keyword"
          :placeholder="t('employees.searchPlaceholder')"
          clearable
          style="width: 240px"
          @keyup.enter="reload"
          @clear="reload"
        />
        <el-select v-model="departmentId" :placeholder="t('employees.allDepartments')" clearable style="width: 180px" @change="reload">
          <el-option v-for="d in departments" :key="d.id" :value="d.id" :label="d.name" />
        </el-select>
        <el-button @click="reload">{{ t('common.query') }}</el-button>
      </div>

      <el-table :data="employees" v-loading="loading">
        <el-table-column prop="code" :label="t('employees.code')" width="100" />
        <el-table-column prop="name" :label="t('employees.name')" width="120" />
        <el-table-column prop="departmentName" :label="t('employees.department')" width="110" />
        <el-table-column prop="position" :label="t('employees.position')" width="120" />
        <el-table-column :label="t('employees.account')" min-width="140">
          <template #default="{ row }">
            <span v-if="row.username">{{ row.username }}</span>
            <span v-else class="sub">{{ t('employees.noAccount') }}</span>
          </template>
        </el-table-column>
        <el-table-column :label="t('common.status')" width="90">
          <template #default="{ row }">
            <el-tag :type="row.status === 'ACTIVE' ? 'success' : 'info'" size="small">
              {{ row.status === 'ACTIVE' ? t('employees.onDuty') : t('employees.left') }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column v-if="canWrite" :label="t('common.actions')" width="270" fixed="right">
          <template #default="{ row }">
            <el-button v-if="canGrant" link type="primary" @click="openRoles(row)">{{ t('employees.roles') }}</el-button>
            <el-button v-if="!row.username" link type="primary" @click="openAccount(row)">{{ t('employees.openAccount') }}</el-button>
            <el-button v-else link type="primary" @click="openReset(row)">{{ t('employees.resetPassword') }}</el-button>
            <el-button v-if="row.status === 'ACTIVE'" link type="danger" @click="deactivate(row)">
              {{ t('employees.markLeft') }}
            </el-button>
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

    <el-dialog v-model="createOpen" :title="t('employees.create')" width="600px">
      <el-form :model="form" label-width="110px">
        <div class="grid">
          <el-form-item :label="t('employees.code')" required>
            <el-input v-model="form.code" placeholder="E003" />
          </el-form-item>
          <el-form-item :label="t('employees.name')" required>
            <el-input v-model="form.name" />
          </el-form-item>
          <el-form-item :label="t('employees.department')" required>
            <el-select v-model="form.departmentId" style="width: 100%">
              <el-option v-for="d in departments" :key="d.id" :value="d.id" :label="d.name" />
            </el-select>
          </el-form-item>
          <el-form-item :label="t('employees.position')">
            <el-input v-model="form.position" />
          </el-form-item>
          <el-form-item :label="t('employees.email')">
            <el-input v-model="form.email" />
          </el-form-item>
          <el-form-item :label="t('employees.phone')">
            <el-input v-model="form.phone" />
          </el-form-item>
        </div>
        <el-divider content-position="left">
          {{ t('employees.accountSection') }}
          <span class="hint">{{ t('employees.accountHint') }}</span>
        </el-divider>
        <div class="grid">
          <el-form-item :label="t('employees.username')">
            <el-input v-model="form.username" autocomplete="off" />
          </el-form-item>
          <el-form-item :label="t('employees.initialPassword')">
            <el-input v-model="form.initialPassword" type="password" show-password autocomplete="new-password" />
          </el-form-item>
        </div>
      </el-form>
      <template #footer>
        <el-button @click="createOpen = false">{{ t('common.cancel') }}</el-button>
        <el-button type="primary" :loading="saving" @click="save">{{ t('common.save') }}</el-button>
      </template>
    </el-dialog>

    <el-dialog v-model="rolesOpen" :title="t('employees.assignRoles')" width="440px">
      <p class="target">{{ current?.name }}</p>
      <el-checkbox-group v-model="selectedRoles" class="role-list">
        <el-checkbox v-for="r in roles" :key="r.id" :value="r.id" :label="`${r.name}（${r.code}）`" />
      </el-checkbox-group>
      <template #footer>
        <el-button @click="rolesOpen = false">{{ t('common.cancel') }}</el-button>
        <el-button type="primary" :loading="saving" @click="saveRoles">{{ t('common.save') }}</el-button>
      </template>
    </el-dialog>

    <el-dialog v-model="passwordOpen" :title="passwordTitle" width="420px">
      <p class="target">{{ current?.name }}</p>
      <el-form label-width="100px">
        <el-form-item v-if="accountMode" :label="t('employees.username')">
          <el-input v-model="accountForm.username" autocomplete="off" />
        </el-form-item>
        <el-form-item :label="t('employees.newPassword')">
          <el-input v-model="accountForm.password" type="password" show-password autocomplete="new-password" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="passwordOpen = false">{{ t('common.cancel') }}</el-button>
        <el-button type="primary" :loading="saving" @click="savePassword">{{ t('common.save') }}</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { useI18n } from 'vue-i18n'
import { del, get, post } from '../api'
import { useAuthStore } from '../stores/auth'

interface Department { id: string; name: string }
interface Role { id: string; code: string; name: string }
interface Employee {
  id: string
  code: string
  name: string
  departmentId: string
  departmentName: string
  position: string
  email: string
  phone: string
  status: string
  username: string
  roleIds: string[]
}

const EMPTY_FORM = {
  code: '', name: '', departmentId: '', position: '', email: '', phone: '',
  username: '', initialPassword: '',
}

const { t } = useI18n()
const auth = useAuthStore()
const canWrite = auth.can('iam:employee:write')
const canGrant = auth.can('iam:role:write')

const employees = ref<Employee[]>([])
const departments = ref<Department[]>([])
const roles = ref<Role[]>([])
const selectedRoles = ref<string[]>([])
const current = ref<Employee | null>(null)
const total = ref(0)
const page = ref(1)
const pageSize = 10
const keyword = ref('')
const departmentId = ref('')
const loading = ref(false)
const saving = ref(false)
const createOpen = ref(false)
const rolesOpen = ref(false)
const passwordOpen = ref(false)
const accountMode = ref(false)
const form = reactive({ ...EMPTY_FORM })
const accountForm = reactive({ username: '', password: '' })
const passwordTitle = computed(() =>
  accountMode.value ? t('employees.openAccount') : t('employees.resetPassword'),
)

async function load() {
  loading.value = true
  try {
    const data = await get<{ employees: Employee[]; meta: { total: string } }>('/employees', {
      page: page.value, page_size: pageSize, keyword: keyword.value, department_id: departmentId.value,
    })
    employees.value = data.employees ?? []
    total.value = Number(data.meta.total)
  } finally {
    loading.value = false
  }
}

function reload() {
  page.value = 1
  load()
}

function openCreate() {
  Object.assign(form, EMPTY_FORM)
  createOpen.value = true
}

async function save() {
  if (!form.code || !form.name || !form.departmentId) {
    ElMessage.warning(t('employees.required'))
    return
  }
  // The account is optional but half of it is not: iam rejects a username
  // without a password, so catch it before the round trip.
  if (!form.username !== !form.initialPassword) {
    ElMessage.warning(t('employees.accountIncomplete'))
    return
  }
  saving.value = true
  try {
    await post('/employees', {
      code: form.code, name: form.name, departmentId: form.departmentId,
      position: form.position, email: form.email, phone: form.phone,
      username: form.username, initialPassword: form.initialPassword,
    })
    ElMessage.success(t('employees.created'))
    createOpen.value = false
    load()
  } finally {
    saving.value = false
  }
}

async function openRoles(row: Employee) {
  current.value = row
  const data = await get<{ employee: Employee }>(`/employees/${row.id}`)
  selectedRoles.value = data.employee.roleIds ?? []
  rolesOpen.value = true
}

async function saveRoles() {
  saving.value = true
  try {
    await post(`/employees/${current.value?.id}/roles`, { roleIds: selectedRoles.value })
    ElMessage.success(t('employees.rolesSaved'))
    rolesOpen.value = false
    load()
  } finally {
    saving.value = false
  }
}

function openAccount(row: Employee) {
  current.value = row
  accountMode.value = true
  accountForm.username = ''
  accountForm.password = ''
  passwordOpen.value = true
}

function openReset(row: Employee) {
  current.value = row
  accountMode.value = false
  accountForm.username = row.username
  accountForm.password = ''
  passwordOpen.value = true
}

async function savePassword() {
  if (!accountForm.password || (accountMode.value && !accountForm.username)) {
    ElMessage.warning(t('employees.accountIncomplete'))
    return
  }
  saving.value = true
  try {
    if (accountMode.value) {
      await post(`/employees/${current.value?.id}/account`, {
        username: accountForm.username, initialPassword: accountForm.password,
      })
      ElMessage.success(t('employees.accountOpened'))
    } else {
      await post(`/employees/${current.value?.id}/password`, { newPassword: accountForm.password })
      ElMessage.success(t('employees.passwordReset'))
    }
    passwordOpen.value = false
    load()
  } finally {
    saving.value = false
  }
}

async function deactivate(row: Employee) {
  await ElMessageBox.confirm(t('employees.confirmLeave', { name: row.name }), t('employees.confirmTitle'))
  await del(`/employees/${row.id}`)
  ElMessage.success(t('employees.markedLeft'))
  load()
}

onMounted(async () => {
  load()
  departments.value = (await get<{ departments: Department[] }>('/departments')).departments ?? []
  if (canGrant) {
    roles.value = (await get<{ roles: Role[] }>('/roles')).roles ?? []
  }
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
.grid {
  display: grid;
  grid-template-columns: 1fr 1fr;
  column-gap: 12px;
}
.sub {
  color: var(--el-text-color-secondary);
  font-size: 12px;
}
.hint {
  margin-left: 8px;
  font-weight: 400;
  font-size: 12px;
  color: var(--el-text-color-secondary);
}
.target {
  margin: 0 0 12px;
  color: var(--el-text-color-regular);
}
.role-list {
  display: flex;
  flex-direction: column;
  gap: 6px;
}
</style>
