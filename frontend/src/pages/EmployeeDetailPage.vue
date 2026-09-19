<template>
  <div v-loading="loading" class="detail">
    <!-- 返回键在最上面，和邮件详情页同一个位置。浏览器的后退键也能用——
         这正是把抽屉换成页面换来的东西之一。 -->
    <el-button link class="back" @click="back">← {{ t('employees.backToList') }}</el-button>

    <template v-if="employee">
      <div class="head">
        <el-avatar :size="64" :src="avatarUrl" class="face">
          {{ (employee.name || '—').slice(0, 1) }}
        </el-avatar>
        <div class="who">
          <div class="name">{{ employee.name }}</div>
          <div class="sub">
            {{ employee.englishName || '—' }} · {{ employee.code }}
            <template v-if="employee.position"> · {{ employee.position }}</template>
          </div>
        </div>
        <el-tag :type="employee.status === 'ACTIVE' ? 'success' : 'info'">
          {{ employee.status === 'ACTIVE' ? t('employees.onDuty') : t('employees.left') }}
        </el-tag>
        <span class="grow" />
        <!-- 编辑回列表页开对话框，而不是在这里再摆一份同样的表单。
             一个表单两处维护，迟早只有一处会被改对。地址里带上 ?edit=，
             所以这个跳转本身也是可分享、可后退的。 -->
        <el-button v-if="canWrite" type="primary" @click="edit">{{ common('edit') }}</el-button>
      </div>

      <el-card shadow="never">
        <el-tabs v-model="tab">
          <el-tab-pane :label="t('employees.basicInformation')" name="basic">
            <el-descriptions :column="columns" border>
              <el-descriptions-item :label="t('employees.code')">{{ employee.code }}</el-descriptions-item>
              <el-descriptions-item :label="t('employees.name')">{{ employee.name }}</el-descriptions-item>
              <el-descriptions-item :label="t('employees.englishName')">{{ employee.englishName || '—' }}</el-descriptions-item>
              <el-descriptions-item :label="t('employees.email')">{{ employee.email || '—' }}</el-descriptions-item>
              <el-descriptions-item :label="t('employees.phone')">{{ employee.phone || '—' }}</el-descriptions-item>
              <!-- 备注是主管写给管理层的评价，员工本人看不到（他打开的是
                   「我的资料」，那个接口根本不返回这个字段）。这里给它一句
                   标注，写的人才知道边界在哪。 -->
              <el-descriptions-item :span="columns">
                <template #label>
                  {{ t('employees.remark') }}
                  <el-tooltip :content="t('employees.remarkPrivate')" placement="top">
                    <el-icon class="hint-icon"><InfoFilled /></el-icon>
                  </el-tooltip>
                </template>
                {{ employee.remark || '—' }}
              </el-descriptions-item>
            </el-descriptions>
          </el-tab-pane>

          <el-tab-pane :label="t('employees.organizationRelationship')" name="organization">
            <el-descriptions :column="columns" border>
              <el-descriptions-item :label="t('employees.department')">{{ employee.departmentName || '—' }}</el-descriptions-item>
              <el-descriptions-item :label="t('employees.position')">{{ employee.position || '—' }}</el-descriptions-item>
              <el-descriptions-item :label="t('employees.manager')">
                <!-- 上级点得进去。汇报线上下走动是这页最常见的动作，
                     而抽屉时代做不到——抽屉里没有第二个抽屉。 -->
                <el-button
                  v-if="Number(employee.managerId)"
                  link
                  type="primary"
                  @click="goTo(employee.managerId)"
                >{{ employee.managerName || '—' }}</el-button>
                <span v-else>—</span>
              </el-descriptions-item>
              <el-descriptions-item :label="t('employees.hireDate')">{{ employee.hireDate || '—' }}</el-descriptions-item>
              <el-descriptions-item :label="t('employees.leaveDate')">{{ employee.leaveDate || '—' }}</el-descriptions-item>
              <el-descriptions-item :label="t('employees.employmentStatus')">
                {{ employee.status === 'ACTIVE' ? t('employees.onDuty') : t('employees.left') }}
              </el-descriptions-item>
              <el-descriptions-item :label="t('employees.directReports')" :span="columns">
                <el-space v-if="reports.length" wrap>
                  <el-button
                    v-for="r in reports"
                    :key="r.id"
                    link
                    type="primary"
                    @click="goTo(r.id)"
                  >{{ r.name }}</el-button>
                </el-space>
                <span v-else>—</span>
              </el-descriptions-item>
            </el-descriptions>
          </el-tab-pane>

          <el-tab-pane :label="t('employees.accountAndRoles')" name="account">
            <el-descriptions :column="columns" border>
              <el-descriptions-item :label="t('employees.username')">{{ employee.username || t('employees.noAccount') }}</el-descriptions-item>
              <el-descriptions-item :label="t('employees.activation')">{{ activationText(employee) }}</el-descriptions-item>
              <!-- 主邮箱（mail 00067）：公司的邮箱，分给这个人用。密码由管理员在这里
                   输入，员工不能改、不能解绑，登录 ERP 后自动开着。 -->
              <el-descriptions-item :label="t('employees.companyMailbox')" :span="columns">
                <el-space wrap>
                  <template v-if="companyMailbox">
                    <span>{{ companyMailbox.email }}</span>
                    <el-tag v-if="companyMailbox.needsReauth" type="danger" size="small">
                      {{ t('employees.companyMailboxNeedsReauth') }}
                    </el-tag>
                    <el-tag v-else-if="companyMailbox.verifiedAt" type="success" size="small">
                      {{ t('employees.companyMailboxVerified', { at: companyMailbox.verifiedAt }) }}
                    </el-tag>
                  </template>
                  <span v-else class="muted">{{ t('employees.companyMailboxNone') }}</span>
                  <el-button v-if="canWrite" size="small" @click="assigning = true">
                    {{ companyMailbox ? t('employees.companyMailboxReplace') : t('employees.companyMailboxAssign') }}
                  </el-button>
                </el-space>
              </el-descriptions-item>
              <el-descriptions-item :label="t('employees.roles')" :span="columns">
                <el-space wrap>
                  <el-tag v-for="role in roleNames" :key="role" type="info">{{ role }}</el-tag>
                  <span v-if="!roleNames.length">—</span>
                </el-space>
              </el-descriptions-item>
            </el-descriptions>
            <!-- 和员工自己绑邮箱是同一份表单（地址、服务商、密码、「其他」的主机），
                 只是提交到替员工分配的那条路。 -->
            <el-dialog
              v-model="assigning"
              :title="t('employees.companyMailboxDialogTitle')"
              width="min(440px, 94vw)"
              append-to-body
            >
              <p class="company-mailbox-hint">{{ t('employees.companyMailboxDialogHint') }}</p>
              <MailboxCredentialsForm
                :submit-to="`/employees/${employeeID}/company-mailbox`"
                :initial-email="companyMailbox?.email || employee.email || ''"
                :submit-label="t('employees.companyMailboxAssign')"
                @bound="onCompanyMailboxBound"
              />
            </el-dialog>
          </el-tab-pane>

          <el-tab-pane v-if="canDiagnose" :label="t('employees.accessDiagnostic')" name="access">
            <el-alert
              :title="t('employees.accessDiagnosticHint')"
              type="info"
              :closable="false"
              show-icon
              class="diagnostic-intro"
            />
            <div v-loading="diagnosticLoading">
              <template v-if="diagnostic">
                <div class="diagnostic-summary">
                  <div class="diagnostic-stat">
                    <span>{{ t('employees.effectiveAccount') }}</span>
                    <strong>{{ diagnostic.username || t('employees.noAccount') }}</strong>
                  </div>
                  <div class="diagnostic-stat">
                    <span>{{ t('employees.effectiveRoles') }}</span>
                    <strong>{{ diagnostic.roles.filter((r) => r.status === 'ACTIVE').length }}</strong>
                  </div>
                  <div class="diagnostic-stat">
                    <span>{{ t('employees.effectivePermissions') }}</span>
                    <strong>{{ diagnostic.permissionCount }}</strong>
                  </div>
                  <div class="diagnostic-stat">
                    <span>{{ t('employees.pendingApprovals') }}</span>
                    <strong>{{ canReadApprovalDiagnostics ? pendingApprovalTotal : '—' }}</strong>
                  </div>
                </div>

                <div class="finding-list">
                  <el-alert
                    v-for="finding in diagnostic.findings"
                    :key="`${finding.code}:${finding.message}`"
                    :title="finding.message"
                    :description="finding.action"
                    :type="findingType(finding.severity)"
                    :closable="false"
                    show-icon
                  />
                </div>

                <section class="diagnostic-section">
                  <h3>{{ t('employees.effectiveRoles') }}</h3>
                  <el-space wrap>
                    <el-tag
                      v-for="role in diagnostic.roles"
                      :key="role.id"
                      :type="role.status === 'ACTIVE' ? (role.code === 'SUPER_ADMIN' ? 'success' : 'info') : 'danger'"
                      effect="plain"
                    >
                      {{ role.name }} · {{ role.status === 'ACTIVE' ? t('employees.roleEffective') : t('employees.roleInactive') }}
                    </el-tag>
                    <span v-if="!diagnostic.roles.length">—</span>
                  </el-space>
                </section>

                <section class="diagnostic-section">
                  <h3>{{ t('employees.effectiveScopes') }}</h3>
                  <el-table :data="diagnostic.scopes" size="small" border>
                    <el-table-column :label="t('employees.businessModule')" min-width="180">
                      <template #default="{ row }">{{ scopeModuleLabel(row.module) }}</template>
                    </el-table-column>
                    <el-table-column :label="t('employees.effectiveScope')" min-width="150">
                      <template #default="{ row }">
                        <el-tag :type="row.all ? 'success' : 'info'" effect="plain">{{ scopeLabel(row.scopeType) }}</el-tag>
                      </template>
                    </el-table-column>
                    <el-table-column :label="t('employees.scopeSource')" min-width="170">
                      <template #default="{ row }">
                        {{ row.configured ? t('employees.scopeConfigured') : t('employees.scopeDefaultSelf') }}
                      </template>
                    </el-table-column>
                  </el-table>
                </section>

                <section class="diagnostic-section">
                  <h3>{{ t('employees.effectivePermissions') }}</h3>
                  <el-collapse>
                    <el-collapse-item
                      v-for="group in diagnostic.permissions"
                      :key="group.module"
                      :title="`${permissionModuleLabel(group.module)}（${group.codes.length}）`"
                    >
                      <el-space wrap>
                        <el-tag v-for="code in group.codes" :key="code" effect="plain">{{ code }}</el-tag>
                      </el-space>
                    </el-collapse-item>
                  </el-collapse>
                </section>

                <section v-if="canReadApprovalDiagnostics" class="diagnostic-section">
                  <div class="section-heading">
                    <h3>{{ t('employees.pendingApprovalDetails') }}</h3>
                    <span>{{ t('employees.pendingApprovalHint') }}</span>
                  </div>
                  <el-table :data="pendingApprovals" size="small" border :empty-text="t('employees.noPendingApprovals')">
                    <el-table-column :label="t('employees.document')" min-width="180">
                      <template #default="{ row }">
                        <strong>{{ row.instance.bizNo || `#${row.instance.bizId}` }}</strong>
                        <div class="muted">{{ approvalBizLabel(row.instance.bizType) }}</div>
                      </template>
                    </el-table-column>
                    <el-table-column :label="t('employees.approvalNode')" prop="task.nodeName" min-width="150" />
                    <el-table-column :label="t('employees.submittedBy')" prop="instance.submitterName" min-width="120" />
                    <el-table-column :label="t('employees.waitingSince')" min-width="150">
                      <template #default="{ row }">{{ formatTime(row.task.createdAt) }}</template>
                    </el-table-column>
                    <el-table-column
                      v-if="diagnostic.canRecoverApprovals && canActApproval"
                      :label="common('actions')"
                      width="150"
                      fixed="right"
                    >
                      <template #default="{ row }">
                        <el-button link type="success" @click="recoverApproval(row, 'APPROVE')">{{ t('employees.emergencyApprove') }}</el-button>
                        <el-button link type="warning" @click="recoverApproval(row, 'RETURN')">{{ t('employees.emergencyReturn') }}</el-button>
                      </template>
                    </el-table-column>
                  </el-table>
                </section>
              </template>
            </div>
          </el-tab-pane>

          <el-tab-pane :label="t('employees.changes')" name="changes">
            <el-timeline v-if="changes.length">
              <el-timeline-item
                v-for="item in changes"
                :key="item.id"
                :timestamp="formatTime(item.createdAt)"
              >
                {{ item.action }} · {{ t('employees.operator') }} #{{ item.operatorId }}
                <el-collapse class="change-values">
                  <el-collapse-item :title="t('departments.changeValues')">
                    <div>{{ t('departments.before') }}</div><pre>{{ prettyJSON(item.beforeJson) }}</pre>
                    <div>{{ t('departments.after') }}</div><pre>{{ prettyJSON(item.afterJson) }}</pre>
                  </el-collapse-item>
                </el-collapse>
              </el-timeline-item>
            </el-timeline>
            <el-empty v-else :description="t('employees.noChanges')" />
          </el-tab-pane>
        </el-tabs>
      </el-card>
    </template>

    <!-- 打不开要说清楚是哪种打不开：地址里的人不存在，和「加载中」是两回事。 -->
    <el-empty v-else-if="!loading" :description="t('employees.notFound')">
      <el-button @click="back">{{ t('employees.backToList') }}</el-button>
    </el-empty>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRoute, useRouter } from 'vue-router'
import { InfoFilled } from '@element-plus/icons-vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { get, post, quietErrors } from '../api'
import { useAuthStore } from '../stores/auth'
import MailboxCredentialsForm from '../components/MailboxCredentialsForm.vue'
import type { VerifyResponse } from '../lib/mailUnlock'

const { t } = useI18n()
const common = (k: string) => t(`common.${k}`)
const route = useRoute()
const router = useRouter()
const auth = useAuthStore()
const canWrite = computed(() => auth.can('iam:employee:write'))
const canDiagnose = computed(() => auth.can('iam:employee:read') && auth.can('iam:role:read'))
const canReadApprovalDiagnostics = computed(() => auth.can('approval:instance:read'))
const canActApproval = computed(() => auth.can('approval:task:act'))

interface Employee {
  id: string
  code: string
  name: string
  englishName: string
  departmentId: string
  departmentName: string
  position: string
  email: string
  phone: string
  status: string
  username: string
  managerId: string
  managerName: string
  emailVerified: boolean
  inviteExpiresAt: string
  hireDate: string
  leaveDate: string
  remark: string
  roleIds: string[]
  avatarKey: string
}

interface Change {
  id: string
  action: string
  beforeJson: string
  afterJson: string
  operatorId: string
  createdAt: string
}

interface AccessFinding {
  code: string
  severity: string
  message: string
  action?: string
}

interface AccessDiagnostic {
  employeeId: string
  employeeName: string
  employeeStatus: string
  username: string
  managerId: string
  managerName: string
  superAdmin: boolean
  canRecoverApprovals: boolean
  roles: { id: string; code: string; name: string; status: string }[]
  permissionCount: number
  permissions: { module: string; codes: string[] }[]
  scopes: { module: string; scopeType: string; configured: boolean; visibleCount: number; all: boolean }[]
  findings: AccessFinding[]
}

interface PendingApproval {
  task: { id: string; nodeName: string; createdAt: string }
  instance: { id: string; bizType: string; bizId: string; bizNo: string; submitterName: string }
}

const loading = ref(false)
const employee = ref<Employee | null>(null)
const changes = ref<Change[]>([])
const roleNames = ref<string[]>([])
const reports = ref<{ id: string; name: string }[]>([])
const avatarUrl = ref('')
const tab = ref('basic')
const diagnostic = ref<AccessDiagnostic | null>(null)
const diagnosticLoading = ref(false)
const pendingApprovals = ref<PendingApproval[]>([])
const pendingApprovalTotal = ref(0)
const viewportWidth = ref(window.innerWidth)
const columns = computed(() => (viewportWidth.value < 620 ? 1 : 2))

const employeeID = computed(() => String(route.params.id ?? ''))

// ---- 主邮箱（mail 00067） ----
// 网关 GET /employees/{id}/company-mailbox 回的那一份。没有就是 null。
interface CompanyMailbox {
  accountId: string
  email: string
  verifiedAt?: string
  lastError?: string
  /** 密码失效了（被改、被服务商锁）。要管理员重新配——员工自己配不了。 */
  needsReauth?: boolean
  isActive?: boolean
  assignedAt?: string
}
const companyMailbox = ref<CompanyMailbox | null>(null)
const assigning = ref(false)

async function loadCompanyMailbox() {
  companyMailbox.value = null
  if (!employeeID.value) return
  try {
    const d = await get<{ mailbox?: CompanyMailbox | null }>(
      `/employees/${employeeID.value}/company-mailbox`,
      undefined,
      quietErrors,
    )
    companyMailbox.value = d.mailbox ?? null
  } catch {
    // 邮件服务没起、或者没权限：这一格空着，不挡整页。
  }
}
watch(employeeID, loadCompanyMailbox, { immediate: true })

function onCompanyMailboxBound(d: VerifyResponse) {
  assigning.value = false
  ElMessage.success(t('employees.companyMailboxAssigned', { email: d.email ?? '' }))
  void loadCompanyMailbox()
}

async function load() {
  const id = employeeID.value
  if (!id) return
  loading.value = true
  employee.value = null
  changes.value = []
  reports.value = []
  avatarUrl.value = ''
  diagnostic.value = null
  pendingApprovals.value = []
  pendingApprovalTotal.value = 0
  try {
    // 详情和变更记录一起取。角色名和下属是锦上添花，单独取并且**失败不拦**——
    // 因为一个人的资料打不开，和「他有几个下属」查不出来，严重程度差着量级。
    const [detail, changed] = await Promise.all([
      get<{ employee: Employee }>(`/employees/${id}`),
      get<{ changes: Change[] }>(`/employees/${id}/changes`),
    ])
    employee.value = detail.employee
    changes.value = changed.changes ?? []
  } catch {
    employee.value = null
    return
  } finally {
    loading.value = false
  }
  void loadExtras(id)
  if (tab.value === 'access') void loadAccessDiagnostic()
}

async function loadExtras(id: string) {
  const emp = employee.value
  if (!emp) return
  const [roles, mates, avatars] = await Promise.allSettled([
    get<{ roles: { id: string; name: string }[] }>('/roles', undefined, quietErrors),
    get<{ employees: { id: string; name: string }[] }>(
      '/employees',
      { manager_id: id, page_size: 200 },
      quietErrors,
    ),
    emp.avatarKey
      ? post<{ urls: Record<string, string> }>(
          '/employees/avatar-urls',
          { employeeIds: [id] },
          quietErrors,
        )
      : Promise.resolve({ urls: {} as Record<string, string> }),
  ])
  if (roles.status === 'fulfilled') {
    const want = new Set(emp.roleIds ?? [])
    roleNames.value = (roles.value.roles ?? []).filter((r) => want.has(r.id)).map((r) => r.name)
  }
  if (mates.status === 'fulfilled') {
    reports.value = (mates.value.employees ?? []).filter((m) => m.id !== id)
  }
  if (avatars.status === 'fulfilled') {
    avatarUrl.value = avatars.value.urls?.[id] ?? ''
  }
}

async function loadAccessDiagnostic() {
  if (!canDiagnose.value || !employeeID.value) return
  diagnosticLoading.value = true
  diagnostic.value = null
  pendingApprovals.value = []
  pendingApprovalTotal.value = 0
  try {
    const requests: Promise<unknown>[] = [
      get<AccessDiagnostic>(`/employees/${employeeID.value}/access-diagnostic`),
    ]
    if (canReadApprovalDiagnostics.value) {
      requests.push(get<{ todos: PendingApproval[]; meta: { total: string } }>(
        `/employees/${employeeID.value}/pending-approvals`,
        { page: 1, page_size: 100 },
        quietErrors,
      ))
    }
    const settled = await Promise.allSettled(requests)
    if (settled[0].status === 'fulfilled') diagnostic.value = settled[0].value as AccessDiagnostic
    if (settled[1]?.status === 'fulfilled') {
      const approvals = settled[1].value as { todos: PendingApproval[]; meta: { total: string } }
      pendingApprovals.value = approvals.todos ?? []
      pendingApprovalTotal.value = Number(approvals.meta?.total ?? pendingApprovals.value.length)
      if (diagnostic.value && pendingApprovalTotal.value > 0 && diagnostic.value.employeeStatus !== 'ACTIVE') {
        diagnostic.value.findings.unshift({
          code: 'INACTIVE_APPROVER_TASKS',
          severity: 'error',
          message: t('employees.inactiveApproverTasks', { n: pendingApprovalTotal.value }),
          action: t('employees.inactiveApproverTasksAction'),
        })
      }
    }
  } finally {
    diagnosticLoading.value = false
  }
}

function findingType(severity: string): 'success' | 'warning' | 'info' | 'error' {
  if (severity === 'error') return 'error'
  if (severity === 'warning') return 'warning'
  if (severity === 'success') return 'success'
  return 'info'
}

function scopeLabel(scope: string): string {
  const labels: Record<string, string> = {
    SELF: t('roles.scopes.SELF'), DEPT: t('roles.scopes.DEPT'),
    DEPT_AND_SUB: t('roles.scopes.DEPT_AND_SUB'), ALL: t('roles.scopes.ALL'),
    CUSTOM: t('employees.scopeCustom'),
  }
  return labels[scope] ?? scope
}

function scopeModuleLabel(module: string): string {
  const labels: Record<string, string> = {
    export: t('roles.scopeExport'), procurement_sourcing: t('roles.scopeSourcing'),
    procurement_order: t('roles.scopeOrder'), procurement_requirement: t('roles.scopeRequirement'),
    shipping: t('roles.scopeShipping'), quality: t('roles.scopeQuality'), mail: t('roles.scopeMail'),
  }
  return labels[module] ?? module
}

function permissionModuleLabel(module: string): string {
  return String(t(`roles.modules.${module}`)) === `roles.modules.${module}` ? module : t(`roles.modules.${module}`)
}

function approvalBizLabel(bizType: string): string {
  const labels: Record<string, string> = {
    CONTRACT: t('flows.biz.CONTRACT'), PURCHASE_ORDER: t('flows.biz.PURCHASE_ORDER'),
    PURCHASE_ORDER_CHANGE: t('flows.biz.PURCHASE_ORDER_CHANGE'),
    TRAVEL_REIMBURSEMENT: t('flows.biz.TRAVEL_REIMBURSEMENT'),
  }
  return labels[bizType] ?? bizType
}

async function recoverApproval(row: PendingApproval, action: 'APPROVE' | 'RETURN') {
  const verb = action === 'APPROVE' ? t('employees.emergencyApprove') : t('employees.emergencyReturn')
  const { value } = await ElMessageBox.prompt(
    t('employees.recoveryConfirm', { action: verb, no: row.instance.bizNo || `#${row.instance.bizId}` }),
    t('employees.recoveryTitle'),
    {
      type: 'warning',
      inputPlaceholder: t('employees.recoveryReason'),
      inputValidator: (v) => Boolean(String(v ?? '').trim()) || t('employees.recoveryReasonRequired'),
      confirmButtonText: verb,
    },
  )
  await post(`/approvals/tasks/${row.task.id}/act`, { action, comment: String(value).trim() })
  ElMessage.success(t('employees.recoveryDone'))
  await loadAccessDiagnostic()
}

// 有登录名就是已激活，理由见列表页同一处的注释。
function activationText(e: Employee): string {
  if (e.username || e.emailVerified) return t('employees.activated')
  if (Number(e.inviteExpiresAt)) return t('employees.awaitingActivation')
  return t('employees.accountUnopened')
}

function prettyJSON(raw: string): string {
  try {
    return JSON.stringify(JSON.parse(raw || '{}'), null, 2)
  } catch {
    return raw
  }
}

function formatTime(v: string): string {
  return v ? new Date(v).toLocaleString() : ''
}

function back() {
  router.push('/basic/employees')
}

function goTo(id: string) {
  router.push(`/basic/employees/${id}`)
}

function edit() {
  router.push({ path: '/basic/employees', query: { edit: employeeID.value } })
}

// 换人时重新加载。点上级、点下属都是在同一个组件里换 :id，
// 不 watch 的话页面会停在上一个人身上——而地址栏已经变了。
watch(employeeID, load)
watch(tab, (value) => {
  if (value === 'access' && !diagnostic.value) void loadAccessDiagnostic()
})
onMounted(() => {
  window.addEventListener('resize', onResize)
  load()
})

function onResize() {
  viewportWidth.value = window.innerWidth
}
</script>

<style scoped>
.back {
  margin-bottom: 8px;
}

.head {
  display: flex;
  align-items: center;
  gap: 16px;
  margin-bottom: 16px;
}

.face {
  flex: none;
  font-size: 24px;
  background: var(--el-color-primary-light-8);
  color: var(--el-color-primary);
}

.name {
  font-size: 20px;
  font-weight: 600;
}

.sub {
  font-size: 12px;
  color: var(--el-text-color-secondary);
}

.grow {
  flex: 1;
}

.hint-icon {
  color: var(--el-text-color-placeholder);
  vertical-align: middle;
}

.change-values pre {
  margin: 4px 0;
  white-space: pre-wrap;
  word-break: break-all;
  font-size: 12px;
}

/* 主邮箱那一格。 */
.muted {
  color: var(--el-text-color-secondary);
}
.company-mailbox-hint {
  margin: 0 0 12px;
  font-size: 13px;
  line-height: 1.6;
  color: var(--el-text-color-secondary);
}

.diagnostic-intro {
  margin-bottom: 14px;
}

.diagnostic-summary {
  display: grid;
  grid-template-columns: repeat(4, minmax(0, 1fr));
  gap: 10px;
  margin-bottom: 14px;
}

.diagnostic-stat {
  min-width: 0;
  padding: 12px 14px;
  border: 1px solid var(--el-border-color-lighter);
  border-radius: 8px;
  background: var(--el-fill-color-extra-light);
}

.diagnostic-stat span {
  display: block;
  margin-bottom: 4px;
  font-size: 12px;
  color: var(--el-text-color-secondary);
}

.diagnostic-stat strong {
  display: block;
  overflow: hidden;
  font-size: 17px;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.finding-list {
  display: grid;
  gap: 8px;
}

.diagnostic-section {
  margin-top: 20px;
}

.diagnostic-section h3 {
  margin: 0 0 10px;
  font-size: 15px;
}

.section-heading {
  display: flex;
  align-items: baseline;
  justify-content: space-between;
  gap: 12px;
}

.section-heading span {
  font-size: 12px;
  color: var(--el-text-color-secondary);
}

@media (max-width: 760px) {
  .diagnostic-summary {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }
  .section-heading {
    display: block;
  }
}
</style>
