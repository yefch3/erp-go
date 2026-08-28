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
              <!-- 显示的始终是**现在能登录**的那个地址。待确认的新地址跟在
                   后面单独一行——它还没生效，混在一起会让人以为已经改好了。 -->
              <el-descriptions-item :label="t('employees.email')">
                {{ employee.email || '—' }}
                <div v-if="employee.pendingEmail" class="pending-email">
                  {{ t('employees.emailChangePending', { email: employee.pendingEmail }) }}
                </div>
              </el-descriptions-item>
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
              <el-descriptions-item :label="t('employees.roles')" :span="columns">
                <el-space wrap>
                  <el-tag v-for="role in roleNames" :key="role" type="info">{{ role }}</el-tag>
                  <span v-if="!roleNames.length">—</span>
                </el-space>
              </el-descriptions-item>
            </el-descriptions>
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
import { get, post, quietErrors } from '../api'
import { useAuthStore } from '../stores/auth'

const { t } = useI18n()
const common = (k: string) => t(`common.${k}`)
const route = useRoute()
const router = useRouter()
const auth = useAuthStore()
const canWrite = computed(() => auth.can('iam:employee:write'))

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
  // 待确认的新登录邮箱，空表示没有在改。
  pendingEmail: string
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

const loading = ref(false)
const employee = ref<Employee | null>(null)
const changes = ref<Change[]>([])
const roleNames = ref<string[]>([])
const reports = ref<{ id: string; name: string }[]>([])
const avatarUrl = ref('')
const tab = ref('basic')
const viewportWidth = ref(window.innerWidth)
const columns = computed(() => (viewportWidth.value < 620 ? 1 : 2))

const employeeID = computed(() => String(route.params.id ?? ''))

async function load() {
  const id = employeeID.value
  if (!id) return
  loading.value = true
  employee.value = null
  changes.value = []
  reports.value = []
  avatarUrl.value = ''
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

function activationText(e: Employee): string {
  if (e.emailVerified) return t('employees.activated')
  if (Number(e.inviteExpiresAt)) return t('employees.awaitingActivation')
  return e.username ? t('employees.notInvited') : t('employees.accountUnopened')
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
.pending-email {
  margin-top: 4px;
  font-size: 12px;
  color: var(--el-color-warning);
}
</style>
