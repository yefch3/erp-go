<template>
  <div>
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

      <el-table
        ref="table"
        :data="employees"
        v-loading="loading"
        :row-key="(row: Employee) => row.id"
        @selection-change="onSelect"
      >
        <!-- Only rows an invitation could actually go to are selectable.
             Offering a checkbox that then reports "已激活" is a slower way of
             saying what the 激活状态 column already says. -->
        <el-table-column
          v-if="canWrite"
          type="selection"
          width="40"
          :selectable="(row: Employee) => canInvite(row)"
        />
        <el-table-column prop="code" :label="t('employees.code')" width="100" />
        <el-table-column prop="name" :label="t('employees.name')" width="110" />
        <!-- The address and whether it has been proved, in one column and
             early rather than pushed off the right edge behind the fixed
             actions. During a migration this is the only question anybody is
             asking, and 未邀请 / 待激活 are stuck for opposite reasons with
             opposite fixes. The address sits beside the state because
             checking them is the same glance. -->
        <el-table-column :label="t('employees.activation')" min-width="230">
          <template #default="{ row }">
            <div class="cell-stack">
              <el-tag v-if="row.emailVerified" type="success" size="small">
                {{ t('employees.activated') }}
              </el-tag>
              <el-tag v-else-if="Number(row.inviteExpiresAt)" type="warning" size="small">
                {{ t('employees.awaitingActivation') }}
              </el-tag>
              <el-tag v-else type="info" size="small">{{ t('employees.notInvited') }}</el-tag>
              <span class="sub">{{ row.email || t('employees.noMailbox') }}</span>
            </div>
          </template>
        </el-table-column>
        <!-- Whether they still work here, kept left of the fixed actions
             column. The table is wider than the window and scrolls; what gets
             pushed under the fixed column has to be the columns nobody makes a
             decision from, which is 岗位 and 直属上级, not this. -->
        <el-table-column :label="t('common.status')" width="80">
          <template #default="{ row }">
            <el-tag :type="row.status === 'ACTIVE' ? 'success' : 'info'" size="small">
              {{ row.status === 'ACTIVE' ? t('employees.onDuty') : t('employees.left') }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column prop="departmentName" :label="t('employees.department')" width="100" />
        <el-table-column prop="position" :label="t('employees.position')" width="100" />
        <el-table-column :label="t('employees.manager')" width="90">
          <template #default="{ row }"><span class="sub">{{ row.managerName || '—' }}</span></template>
        </el-table-column>
        <el-table-column v-if="canWrite" :label="t('common.actions')" width="330" fixed="right">
          <template #default="{ row }">
            <el-button v-if="canGrant" link type="primary" @click="openRoles(row)">{{ t('employees.roles') }}</el-button>
            <!-- Only for somebody not yet activated. Once they are, the same
                 mail would be a password reset wearing an invitation's
                 clothes, and 重置密码 beside it already says what it does. -->
            <el-button
              v-if="canInvite(row)"
              link
              type="primary"
              :loading="inviting === row.id"
              @click="invite(row)"
            >
              {{ Number(row.inviteExpiresAt) ? t('employees.reinvite') : t('employees.invite') }}
            </el-button>
            <el-button v-if="!row.username" link type="primary" @click="openAccount(row)">{{ t('employees.openAccount') }}</el-button>
            <el-button v-else link type="primary" @click="openReset(row)">{{ t('employees.resetPassword') }}</el-button>
            <!-- Ends the sessions this person is holding right now. Its own
                 action rather than part of 标记离职, because a stolen laptop is
                 not a resignation — and because somebody who is still employed
                 sometimes needs signing out of a machine they no longer have. -->
            <el-button v-if="row.username" link type="warning" @click="revokeSessions(row)">
              {{ t('employees.revokeSessions') }}
            </el-button>
            <el-button v-if="row.status === 'ACTIVE'" link type="danger" @click="deactivate(row)">
              {{ t('employees.markLeft') }}
            </el-button>
            <el-button v-else link type="primary" @click="reinstate(row)">
              {{ t('employees.reinstate') }}
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
          <el-form-item :label="t('employees.manager')">
            <el-select v-model="form.managerId" clearable filterable style="width: 100%"
                       :placeholder="t('employees.managerNone')">
              <el-option v-for="e in employees" :key="e.id" :value="e.id" :label="`${e.code} · ${e.name}`" />
            </el-select>
            <div class="hint">{{ t('employees.managerHint') }}</div>
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
import { ElMessage, ElMessageBox, type ElTable } from 'element-plus'
import { useI18n } from 'vue-i18n'
import { del, get, post } from '../api'
import { useAuthStore } from '../stores/auth'
import ImportEmployeesDialog from '../components/ImportEmployeesDialog.vue'

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
  managerId: string
  managerName: string
  emailVerified: boolean
  // Unix seconds, as a string: int64 over JSON. 0 or absent means no
  // invitation is outstanding.
  inviteExpiresAt: string
}

const EMPTY_FORM = {
  code: '', name: '', departmentId: '', position: '', email: '', phone: '',
  username: '', initialPassword: '', managerId: '',
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
// Which row's invitation is in flight, not a plain boolean: the spinner
// belongs on the button that was clicked, and a shared flag would spin all of
// them.
const inviting = ref('')
const createOpen = ref(false)
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
      managerId: form.managerId || '0',
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
.cell-stack {
  display: flex;
  flex-direction: column;
  align-items: flex-start;
  gap: 2px;
  line-height: 1.4;
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
</style>
