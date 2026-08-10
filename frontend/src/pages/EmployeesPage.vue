<template>
  <div>
    <BasicDataEmployeeNav />
    <div class="page-head">
      <h2>{{ t('employees.title') }}</h2>
      <div class="head-actions">
        <!-- Acts on the selection, so it is stated with a count rather than
             leaving somebody to guess how many mails they are about to send
             from their own mailbox. -->
        <el-button v-if="canWrite && invitable.length" @click="inviteSelected">
          {{ t('employees.inviteSelected', { n: invitable.length }) }}
        </el-button>
        <el-button v-if="canWrite" @click="importOpen = true">{{ t('employees.import') }}</el-button>
        <el-button v-if="canWrite" type="primary" @click="openCreate">{{ t('employees.create') }}</el-button>
      </div>
    </div>

    <el-card shadow="never" class="employee-card">
      <div class="filters">
        <div class="filter-fields">
          <el-input
            v-model="keyword"
            :placeholder="t('employees.searchPlaceholder')"
            clearable
            @keyup.enter="reload"
            @clear="reload"
          />
          <el-select v-model="departmentId" :placeholder="t('employees.allDepartments')" clearable @change="reload">
            <el-option v-for="d in departments" :key="d.id" :value="d.id" :label="d.name" />
          </el-select>
          <el-select v-model="managerId" :placeholder="t('employees.allManagers')" clearable filterable @change="reload">
            <el-option v-for="e in employeeOptions" :key="e.id" :value="e.id" :label="e.name" />
          </el-select>
          <el-select v-if="canReadRoles" v-model="roleId" :placeholder="t('employees.allRoles')" clearable @change="reload">
            <el-option v-for="r in roles" :key="r.id" :value="r.id" :label="r.name" />
          </el-select>
          <el-select v-model="employmentStatus" :placeholder="t('employees.employmentStatus')" clearable @change="reload">
            <el-option value="ACTIVE" :label="t('employees.onDuty')" />
            <el-option value="INACTIVE" :label="t('employees.left')" />
          </el-select>
          <el-select v-model="accountStatus" :placeholder="t('employees.accountStatus')" clearable @change="reload">
            <el-option value="ACTIVE" :label="t('employees.activated')" />
            <el-option value="PENDING" :label="t('employees.awaitingActivation')" />
            <el-option value="NONE" :label="t('employees.accountUnopened')" />
          </el-select>
        </div>
        <div class="filter-actions">
          <el-button @click="resetFilters">{{ t('employees.resetFilters') }}</el-button>
          <el-button type="primary" @click="reload">{{ t('common.query') }}</el-button>
        </div>
      </div>

      <el-table
        ref="table"
        :data="employees"
        v-loading="loading"
        :row-key="(row: Employee) => row.id"
        class="employee-table"
        stripe
        @selection-change="onSelect"
      >
        <!-- Only rows an invitation could actually go to are selectable.
             Offering a checkbox that then reports "已激活" is a slower way of
             saying what the 激活状态 column already says. -->
        <el-table-column
          v-if="canWrite"
          type="selection"
          width="48"
          :selectable="(row: Employee) => canInvite(row)"
        />
        <el-table-column :label="t('employees.employeeInfo')" min-width="240">
          <template #default="{ row }">
            <div class="employee-info">
              <div class="employee-primary">
                <span class="employee-name">{{ row.name }}</span>
                <span class="employee-code">{{ row.code }}</span>
              </div>
              <span class="sub">{{ row.email || t('employees.noMailbox') }}</span>
            </div>
          </template>
        </el-table-column>
        <el-table-column :label="t('employees.organization')" min-width="180">
          <template #default="{ row }">
            <div class="cell-stack">
              <span>{{ row.departmentName || '—' }}</span>
              <span class="sub">{{ row.position || t('employees.noPosition') }}</span>
            </div>
          </template>
        </el-table-column>
        <el-table-column :label="t('employees.manager')" min-width="130">
          <template #default="{ row }"><span class="sub">{{ row.managerName || '—' }}</span></template>
        </el-table-column>
        <el-table-column :label="t('employees.activation')" min-width="155">
          <template #default="{ row }">
            <div class="cell-stack activation-cell">
              <el-tag v-if="row.emailVerified" type="success" size="small">
                {{ t('employees.activated') }}
              </el-tag>
              <el-tag v-else-if="Number(row.inviteExpiresAt)" type="warning" size="small">
                {{ t('employees.awaitingActivation') }}
              </el-tag>
              <el-tag v-else type="info" size="small">{{ t('employees.notInvited') }}</el-tag>
              <span class="sub">{{ row.username || t('employees.noAccount') }}</span>
            </div>
          </template>
        </el-table-column>
        <el-table-column :label="t('common.status')" width="90" align="center">
          <template #default="{ row }">
            <el-tag :type="row.status === 'ACTIVE' ? 'success' : 'info'" size="small">
              {{ row.status === 'ACTIVE' ? t('employees.onDuty') : t('employees.left') }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column v-if="canWrite" :label="t('common.actions')" width="160" fixed="right" align="right">
          <template #default="{ row }">
            <div class="row-actions">
              <el-button link type="primary" @click="openEdit(row)">{{ t('common.edit') }}</el-button>
              <el-dropdown trigger="click" @command="(command: string) => handleRowCommand(command, row)">
                <el-button link type="primary">{{ t('employees.moreActions') }}<span class="dropdown-arrow">⌄</span></el-button>
                <template #dropdown>
                  <el-dropdown-menu>
                    <el-dropdown-item command="changes">{{ t('employees.changes') }}</el-dropdown-item>
                    <el-dropdown-item v-if="canGrant" command="roles">{{ t('employees.roles') }}</el-dropdown-item>
                    <el-dropdown-item v-if="canInvite(row)" command="invite">
                      {{ Number(row.inviteExpiresAt) ? t('employees.reinvite') : t('employees.invite') }}
                    </el-dropdown-item>
                    <el-dropdown-item :command="row.username ? 'resetPassword' : 'openAccount'">
                      {{ row.username ? t('employees.resetPassword') : t('employees.openAccount') }}
                    </el-dropdown-item>
                    <el-dropdown-item v-if="row.username" command="revoke" divided>{{ t('employees.revokeSessions') }}</el-dropdown-item>
                    <el-dropdown-item v-if="row.status === 'ACTIVE'" command="leave" class="danger-action">
                      {{ t('employees.markLeft') }}
                    </el-dropdown-item>
                    <el-dropdown-item v-else command="reinstate">{{ t('employees.reinstate') }}</el-dropdown-item>
                  </el-dropdown-menu>
                </template>
              </el-dropdown>
            </div>
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

    <ImportEmployeesDialog v-model:open="importOpen" @imported="reload" />

    <!-- What actually happened, per person. A batch of eighty is exactly the
         case where "已发送" as a single toast is useless: the useful answer is
         which three did not go and why. -->
    <el-dialog v-model="batchOpen" :title="t('employees.batchResult')" width="620px">
      <div class="summary">{{ t('employees.batchSummary', { sent: batchSent, failed: batchFailed.length }) }}</div>
      <el-table v-if="batchFailed.length" :data="batchFailed" max-height="360" size="small">
        <el-table-column prop="name" :label="t('employees.name')" width="120" />
        <el-table-column prop="email" :label="t('employees.email')" width="200" />
        <el-table-column prop="reason" :label="t('employees.batchReason')" min-width="220" />
      </el-table>
      <template #footer>
        <el-button type="primary" @click="batchOpen = false">{{ t('common.close') }}</el-button>
      </template>
    </el-dialog>

    <el-dialog v-model="createOpen" :title="editing ? t('employees.edit') : t('employees.create')" width="680px">
      <el-form :model="form" label-width="110px">
        <div class="grid">
          <el-form-item :label="t('employees.code')" required>
            <el-input v-model="form.code" placeholder="E003" />
          </el-form-item>
          <el-form-item :label="t('employees.name')" required>
            <el-input v-model="form.name" />
          </el-form-item>
          <el-form-item :label="t('employees.englishName')">
            <el-input v-model="form.englishName" />
          </el-form-item>
          <el-form-item :label="t('employees.department')" required>
            <el-select v-model="form.departmentId" style="width: 100%">
              <el-option v-for="d in activeDepartments" :key="d.id" :value="d.id" :label="d.name" />
            </el-select>
          </el-form-item>
          <el-form-item :label="t('employees.position')">
            <el-input v-model="form.position" />
          </el-form-item>
          <el-form-item :label="t('employees.manager')">
            <el-select v-model="form.managerId" clearable filterable style="width: 100%"
                       :placeholder="t('employees.managerNone')">
              <el-option v-for="e in managerCandidates" :key="e.id" :value="e.id" :label="`${e.code} · ${e.name}`" />
            </el-select>
            <div class="hint">{{ t('employees.managerHint') }}</div>
          </el-form-item>
          <el-form-item :label="t('employees.email')">
            <el-input v-model="form.email" />
          </el-form-item>
          <el-form-item :label="t('employees.phone')">
            <el-input v-model="form.phone" />
          </el-form-item>
          <el-form-item :label="t('employees.hireDate')">
            <el-date-picker v-model="form.hireDate" type="date" value-format="YYYY-MM-DD" style="width: 100%" />
          </el-form-item>
          <el-form-item v-if="editing" :label="t('employees.leaveDate')">
            <el-date-picker v-model="form.leaveDate" type="date" value-format="YYYY-MM-DD" clearable style="width: 100%" />
          </el-form-item>
          <el-form-item :label="t('employees.remark')" class="wide">
            <el-input v-model="form.remark" type="textarea" :rows="2" />
          </el-form-item>
        </div>
        <el-divider v-if="!editing" content-position="left">
          {{ t('employees.accountSection') }}
          <span class="hint">{{ t('employees.accountHint') }}</span>
        </el-divider>
        <div v-if="!editing" class="grid">
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

    <el-drawer v-model="changesOpen" :title="t('employees.changes')" size="520px">
      <el-timeline>
        <el-timeline-item v-for="item in changes" :key="item.id" :timestamp="formatTime(item.createdAt)">
          {{ item.action }} · {{ t('employees.operator') }} #{{ item.operatorId }}
          <el-collapse class="change-values">
            <el-collapse-item :title="t('departments.changeValues')">
              <div>{{ t('departments.before') }}</div><pre>{{ prettyJSON(item.beforeJson) }}</pre>
              <div>{{ t('departments.after') }}</div><pre>{{ prettyJSON(item.afterJson) }}</pre>
            </el-collapse-item>
          </el-collapse>
        </el-timeline-item>
      </el-timeline>
      <el-empty v-if="!changes.length" :description="t('employees.noChanges')" />
    </el-drawer>

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
import { ElMessage, ElMessageBox, type ElTable } from 'element-plus'
import { useI18n } from 'vue-i18n'
import { useRoute, useRouter } from 'vue-router'
import { del, get, post, put } from '../api'
import { useAuthStore } from '../stores/auth'
import ImportEmployeesDialog from '../components/ImportEmployeesDialog.vue'
import BasicDataEmployeeNav from '../components/BasicDataEmployeeNav.vue'

interface Department { id: string; name: string; status: string }
interface Role { id: string; code: string; name: string }
interface Change { id: string; action: string; operatorId: string; createdAt: string; beforeJson: string; afterJson: string }
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
  managerId: string
  managerName: string
  emailVerified: boolean
  // Unix seconds, as a string: int64 over JSON. 0 or absent means no
  // invitation is outstanding.
  inviteExpiresAt: string
  englishName: string
  hireDate: string
  leaveDate: string
  remark: string
  version: number
}

const EMPTY_FORM = {
  code: '', name: '', departmentId: '', position: '', email: '', phone: '',
  username: '', initialPassword: '', managerId: '',
  englishName: '', hireDate: '', leaveDate: '', remark: '', version: 0, id: '',
}

const { t } = useI18n()
const route = useRoute()
const router = useRouter()
const auth = useAuthStore()
const canWrite = auth.can('iam:employee:write')
const canGrant = auth.can('iam:role:write')
const canReadRoles = auth.can('iam:role:read')

const employees = ref<Employee[]>([])
const departments = ref<Department[]>([])
const roles = ref<Role[]>([])
const selectedRoles = ref<string[]>([])
const current = ref<Employee | null>(null)
const total = ref(0)
const page = ref(Math.max(1, Number(route.query.page) || 1))
const pageSize = 10
const keyword = ref(String(route.query.keyword ?? ''))
const departmentId = ref(String(route.query.department_id ?? ''))
const managerId = ref(String(route.query.manager_id ?? ''))
const roleId = ref(String(route.query.role_id ?? ''))
const employmentStatus = ref(String(route.query.employment_status ?? ''))
const accountStatus = ref(String(route.query.account_status ?? ''))
const loading = ref(false)
const saving = ref(false)
// Which row's invitation is in flight, not a plain boolean: the spinner
// belongs on the button that was clicked, and a shared flag would spin all of
// them.
const inviting = ref('')
const createOpen = ref(false)
const editing = ref(false)
const changesOpen = ref(false)
const changes = ref<Change[]>([])
const importOpen = ref(false)
const table = ref<InstanceType<typeof ElTable>>()
const selected = ref<Employee[]>([])
const batchOpen = ref(false)
const batchSent = ref(0)
const batchFailed = ref<{ name: string; email: string; reason: string }[]>([])

// Who an invitation could actually reach: still employed, and not already in.
// Used both for the row button and for which rows may be ticked, so the two
// can never disagree about who is invitable.
function canInvite(row: Employee): boolean {
  return !row.emailVerified && row.status === 'ACTIVE'
}
const invitable = computed(() => selected.value.filter(canInvite))

function onSelect(rows: Employee[]) {
  selected.value = rows
}
const rolesOpen = ref(false)
const passwordOpen = ref(false)
const accountMode = ref(false)
const form = reactive({ ...EMPTY_FORM })
const accountForm = reactive({ username: '', password: '' })
const passwordTitle = computed(() =>
  accountMode.value ? t('employees.openAccount') : t('employees.resetPassword'),
)
const employeeOptions = ref<Employee[]>([])
const activeDepartments = computed(() => departments.value.filter((d) => d.status === 'ACTIVE'))
const managerCandidates = computed(() => employeeOptions.value.filter((e) => e.status === 'ACTIVE' && e.id !== form.id))

async function load() {
  loading.value = true
  try {
    // 将筛选和页码写入地址，刷新或分享当前页面时仍保持同一组结果。
    const query = Object.fromEntries(Object.entries({
      page: page.value > 1 ? String(page.value) : '', keyword: keyword.value,
      department_id: departmentId.value, manager_id: managerId.value, role_id: roleId.value,
      employment_status: employmentStatus.value, account_status: accountStatus.value,
    }).filter(([, value]) => value))
    router.replace({ query })
    const data = await get<{ employees: Employee[]; meta: { total: string } }>('/employees', {
      page: page.value, page_size: pageSize, keyword: keyword.value, department_id: departmentId.value,
      manager_id: managerId.value, role_id: roleId.value,
      employment_status: employmentStatus.value, account_status: accountStatus.value,
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

function resetFilters() {
  keyword.value = ''
  departmentId.value = ''
  managerId.value = ''
  roleId.value = ''
  employmentStatus.value = ''
  accountStatus.value = ''
  reload()
}

// 列表只保留最常用的“编辑”，其余操作统一从更多菜单分发，避免操作列过宽。
function handleRowCommand(command: string, row: Employee) {
  switch (command) {
    case 'changes': void openChanges(row); break
    case 'roles': void openRoles(row); break
    case 'invite': void invite(row); break
    case 'openAccount': openAccount(row); break
    case 'resetPassword': openReset(row); break
    case 'revoke': void revokeSessions(row); break
    case 'leave': void deactivate(row); break
    case 'reinstate': void reinstate(row); break
  }
}

function openCreate() {
  editing.value = false
  Object.assign(form, EMPTY_FORM)
  createOpen.value = true
}

// 编辑前重新读取详情，确保版本号和直属上级等字段不是列表中的旧数据。
async function openEdit(row: Employee) {
  const data = await get<{ employee: Employee }>(`/employees/${row.id}`)
  const employee = data.employee
  editing.value = true
  Object.assign(form, {
    ...EMPTY_FORM, ...employee,
    departmentId: employee.departmentId || '', managerId: employee.managerId || '',
  })
  createOpen.value = true
}

async function save() {
  if (!form.code || !form.name || !form.departmentId) {
    ElMessage.warning(t('employees.required'))
    return
  }
  // The account is optional but half of it is not: iam rejects a username
  // without a password, so catch it before the round trip.
  if (!editing.value && (!form.username !== !form.initialPassword)) {
    ElMessage.warning(t('employees.accountIncomplete'))
    return
  }
  saving.value = true
  try {
    const body = {
      code: form.code, name: form.name, departmentId: form.departmentId,
      position: form.position, email: form.email, phone: form.phone,
      managerId: form.managerId || '0',
      englishName: form.englishName, hireDate: form.hireDate, leaveDate: form.leaveDate,
      remark: form.remark, expectedVersion: form.version,
      username: form.username, initialPassword: form.initialPassword,
    }
    if (editing.value) await put(`/employees/${form.id}`, body)
    else await post('/employees', body)
    ElMessage.success(t(editing.value ? 'employees.updated' : 'employees.created'))
    createOpen.value = false
    load()
  } finally {
    saving.value = false
  }
}

async function openChanges(row: Employee) {
  const data = await get<{ changes: Change[] }>(`/employees/${row.id}/changes`)
  changes.value = data.changes ?? []
  changesOpen.value = true
}
function formatTime(value: string) { return value ? new Date(value).toLocaleString() : '' }
function prettyJSON(value: string) { try { return JSON.stringify(JSON.parse(value || '{}'), null, 2) } catch { return value } }

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

// Sending the invitation. Confirmed first because it puts a mail in somebody
// else's inbox from this administrator's own address — an action with a
// visible outside, not a form save.
async function invite(row: Employee) {
  const again = Number(row.inviteExpiresAt) > 0
  await ElMessageBox.confirm(
    t(again ? 'employees.confirmReinvite' : 'employees.confirmInvite', {
      name: row.name,
      email: row.email,
    }),
    t(again ? 'employees.reinvite' : 'employees.invite'),
  )
  inviting.value = row.id
  try {
    await post(`/employees/${row.id}/invite`, {})
    ElMessage.success(t('employees.invited', { email: row.email }))
    load()
  } finally {
    inviting.value = ''
  }
}

// Batch invitation. Confirmed with the count and the sending address spelled
// out, because this puts N messages into N inboxes from the operator's own
// mailbox — an action with a visible outside, and one nobody can take back.
async function inviteSelected() {
  const targets = invitable.value
  await ElMessageBox.confirm(
    t('employees.confirmInviteMany', { n: targets.length }),
    t('employees.inviteSelected', { n: targets.length }),
  )
  const d = await post<{
    sent: number
    results: { name: string; email: string; sent: boolean; reason: string }[]
  }>('/employees/invite-batch', { employeeIds: targets.map((e) => e.id) })
  batchSent.value = Number(d.sent ?? 0)
  batchFailed.value = (d.results ?? []).filter((r) => !r.sent)
  // The dialog opens either way. "全部发送成功" is worth seeing after eighty
  // sends, and it is the only confirmation that the count was what was meant.
  batchOpen.value = true
  table.value?.clearSelection()
  load()
}

async function deactivate(row: Employee) {
  await ElMessageBox.confirm(t('employees.confirmLeave', { name: row.name }), t('employees.confirmTitle'))
  await del(`/employees/${row.id}`)
  ElMessage.success(t('employees.markedLeft'))
  load()
}

// Confirmed, because it is not undoable and it interrupts somebody: whoever
// is holding that session is thrown back to the login page mid-task.
async function revokeSessions(row: Employee) {
  await ElMessageBox.confirm(
    t('employees.confirmRevoke', { name: row.name }),
    t('employees.revokeSessions'),
  )
  await post(`/employees/${row.id}/revoke-sessions`)
  ElMessage.success(t('employees.sessionsRevoked', { name: row.name }))
}

async function reinstate(row: Employee) {
  await post(`/employees/${row.id}/activate`)
  ElMessage.success(t('employees.reinstated'))
  load()
}

onMounted(async () => {
  load()
  departments.value = (await get<{ departments: Department[] }>('/departments')).departments ?? []
  employeeOptions.value = (await get<{ employees: Employee[] }>('/employees', { page: 1, page_size: 200 })).employees ?? []
  if (canReadRoles) {
    roles.value = (await get<{ roles: Role[] }>('/roles')).roles ?? []
  }
})
</script>

<style scoped>
.change-values pre { white-space: pre-wrap; word-break: break-all; font-size: 12px; }
.page-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin-bottom: 16px;
}
.page-head h2 {
  font-size: 22px;
  font-weight: 600;
  margin: 0;
}
.employee-card {
  overflow: hidden;
}
.employee-card :deep(.el-card__body) {
  padding: 0;
}
.filters {
  display: flex;
  align-items: center;
  gap: 14px;
  padding: 18px 20px;
  background: #f8fafc;
  border-bottom: 1px solid var(--el-border-color-lighter);
}
.filter-fields {
  flex: 1;
  display: grid;
  grid-template-columns: minmax(220px, 1.4fr) repeat(5, minmax(135px, 1fr));
  gap: 10px;
}
.filter-fields :deep(.el-select),
.filter-fields :deep(.el-input) {
  width: 100%;
}
.filter-actions {
  display: flex;
  flex: none;
  gap: 8px;
}
.employee-table {
  width: calc(100% - 40px);
  margin: 16px 20px 0;
}
.employee-table :deep(th.el-table__cell) {
  background: #f8fafc;
  color: #64748b;
  font-weight: 600;
}
.employee-table :deep(.el-table__row td.el-table__cell) {
  padding: 15px 0;
}
.pager {
  padding: 16px 20px 18px;
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
.cell-stack {
  display: flex;
  flex-direction: column;
  align-items: flex-start;
  gap: 2px;
  line-height: 1.4;
}
.employee-info {
  display: flex;
  flex-direction: column;
  gap: 5px;
}
.employee-primary {
  display: flex;
  align-items: center;
  gap: 8px;
}
.employee-name {
  color: var(--el-text-color-primary);
  font-weight: 600;
}
.employee-code {
  padding: 2px 7px;
  border-radius: 5px;
  background: #eef4ff;
  color: var(--el-color-primary);
  font-size: 12px;
}
.activation-cell {
  gap: 6px;
}
.row-actions {
  display: flex;
  align-items: center;
  justify-content: flex-end;
  gap: 14px;
}
.row-actions :deep(.el-button + .el-button) {
  margin-left: 0;
}
.dropdown-arrow {
  margin-left: 3px;
  font-size: 14px;
}
:global(.danger-action) {
  color: var(--el-color-danger) !important;
}
.head-actions {
  display: flex;
  gap: 10px;
}
.summary {
  margin-bottom: 12px;
  font-size: 13px;
  color: var(--el-text-color-regular);
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
@media (max-width: 1300px) {
  .filter-fields {
    grid-template-columns: repeat(3, minmax(150px, 1fr));
  }
}
@media (max-width: 900px) {
  .filters {
    align-items: stretch;
    flex-direction: column;
  }
  .filter-fields {
    grid-template-columns: repeat(2, minmax(140px, 1fr));
  }
  .filter-actions {
    justify-content: flex-end;
  }
}
</style>
