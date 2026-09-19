<template>
  <div>
    <BasicDataEmployeeNav />
    <div class="page-head">
      <h2>{{ t('roles.title') }}</h2>
      <el-button v-if="canWrite" type="primary" @click="createOpen = true">{{ t('roles.create') }}</el-button>
    </div>

    <el-card shadow="never" v-loading="loading">
      <div class="layout">
        <div class="role-column">
          <div
            v-for="r in roles"
            :key="r.id"
            class="role-item"
            :class="{ active: r.id === selected?.id }"
            @click="select(r)"
          >
            <div class="role-name" :title="r.code">
              {{ displayRoleName(r) }}
              <!-- 停用的仍然列出来，否则停掉之后没有任何入口能把它启用回来。 -->
              <el-tag v-if="r.status === 'INACTIVE'" type="info" size="small" effect="plain">
                {{ t('roles.inactive') }}
              </el-tag>
            </div>
            <div class="role-code">{{ r.permissionCodes.length }} {{ t('roles.permissionCount') }}</div>
          </div>
        </div>

        <div class="matrix" v-if="selected">
          <div class="matrix-head">
            <div>
              <span class="matrix-title" :title="selected.code">{{ displayRoleName(selected) }}</span>
              <span class="hint">{{ displayRoleDescription(selected) }}</span>
            </div>
            <div class="head-actions">
              <!-- 超管不给这个按钮：停掉之后没有人能把它启用回来。服务端也拦，
                   这里只是不把一个注定被拒的按钮摆在人眼前。 -->
              <el-button
                v-if="canWrite && selected.code !== 'SUPER_ADMIN'"
                :type="selected.status === 'INACTIVE' ? 'success' : 'danger'"
                plain
                :loading="statusSaving"
                @click="toggleStatus"
              >
                {{ selected.status === 'INACTIVE' ? t('roles.activate') : t('roles.deactivate') }}
              </el-button>
              <el-button v-if="canEditSelected" type="primary" :loading="saving" :disabled="!hasUnsavedChanges" @click="saveGrants">
                {{ t('roles.saveGrants') }}
              </el-button>
            </div>
          </div>
          <el-alert
            v-if="selected.code === 'SUPER_ADMIN'"
            :title="t('roles.superAdminFixed')"
            type="info"
            :closable="false"
            show-icon
            class="super-admin-note"
          />
          <div class="access-overview">
            <div class="overview-number">
              <strong>{{ checked.length }}</strong>
              <span>{{ t('roles.enabledPermissions') }}</span>
            </div>
            <div class="overview-number">
              <strong>{{ enabledModuleCount }}</strong>
              <span>{{ t('roles.enabledModules') }}</span>
            </div>
            <p>{{ t('roles.overviewHint') }}</p>
          </div>

          <section class="settings-section">
            <div class="section-heading">
              <div>
                <h3>{{ t('roles.businessAccess') }}</h3>
                <p>{{ t('roles.businessAccessHint') }}</p>
              </div>
            </div>
            <div class="capability-grid">
              <article
                v-for="(items, module) in grouped"
                :key="module"
                class="capability-card"
                :class="{ enabled: enabledPermissionCount(items) > 0 }"
              >
                <div class="capability-head">
                  <strong>{{ moduleLabel(module) }}</strong>
                  <el-tag :type="enabledPermissionCount(items) > 0 ? 'success' : 'info'" size="small" effect="plain">
                    {{ enabledPermissionCount(items) > 0
                      ? t('roles.enabledCount', { n: enabledPermissionCount(items) })
                      : t('roles.notEnabled') }}
                  </el-tag>
                </div>
                <p class="capability-preview">{{ modulePreview(items) }}</p>
                <el-button link type="primary" @click="openPermissionModule(String(module))">
                  {{ canEditSelected ? t('roles.configureModule') : t('roles.viewModule') }}
                </el-button>
              </article>
            </div>
          </section>

          <!-- Permissions say which features a role may use; the scope says
               whose records those features operate on. Keep both questions
               on the same screen and save them as one change. -->
          <section class="settings-section scope-section">
            <div class="section-heading">
              <div>
                <h3>{{ t('roles.dataScope') }}</h3>
                <p>{{ t('roles.scopeHint') }}</p>
              </div>
            </div>
            <div v-if="visibleScopeModules.length" class="scope-grid">
              <label v-for="scopeModule in visibleScopeModules" :key="scopeModule.key" class="scope-card">
                <span>{{ t(scopeModule.label) }}</span>
                <el-select v-model="scopeValues[scopeModule.key]" :disabled="!canEditSelected">
                <el-option v-for="k in SCOPE_TYPES" :key="k" :value="k" :label="t(`roles.scopes.${k}`)" />
              </el-select>
              </label>
            </div>
            <el-empty v-else :description="t('roles.noScopeNeeded')" :image-size="54" />
          </section>
          <p class="footnote">{{ t('roles.serverEnforced') }}</p>
        </div>
        <div v-else class="matrix empty">{{ t('roles.pickRole') }}</div>
      </div>
    </el-card>

    <el-dialog v-model="permissionDialogOpen" :title="activePermissionModuleLabel" width="620px">
      <div class="permission-dialog-head">
        <div>
          <span>{{ t('roles.modulePermissionHint') }}</span>
          <small>{{ t('roles.permissionDependencyHint') }}</small>
        </div>
        <div v-if="canEditSelected">
          <el-button link type="primary" @click="selectAllActiveModule">{{ t('roles.selectAll') }}</el-button>
          <el-button link @click="clearActiveModule">{{ t('roles.clearAll') }}</el-button>
        </div>
      </div>
      <el-checkbox-group v-model="checked" :disabled="!canEditSelected" class="dialog-permission-list">
        <el-checkbox v-for="p in activeModulePermissions" :key="p.code" :value="p.code" :title="p.code">
          <span class="permission-label">{{ displayPermissionName(p) }}</span>
        </el-checkbox>
      </el-checkbox-group>
      <template #footer>
        <el-button type="primary" @click="permissionDialogOpen = false">{{ t('roles.done') }}</el-button>
      </template>
    </el-dialog>

    <el-dialog v-model="createOpen" :title="t('roles.create')" width="440px">
      <el-form :model="form" label-width="100px">
        <el-form-item :label="t('roles.code')" required>
          <el-input v-model="form.code" placeholder="SALES_MANAGER" />
        </el-form-item>
        <el-form-item :label="t('roles.name')" required>
          <el-input v-model="form.name" placeholder="销售主管" />
        </el-form-item>
        <el-form-item :label="t('roles.description')">
          <el-input v-model="form.description" type="textarea" :rows="2" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="createOpen = false">{{ t('common.cancel') }}</el-button>
        <el-button type="primary" :loading="saving" @click="createRole">{{ t('common.save') }}</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { useI18n } from 'vue-i18n'
import { get, post, put } from '../api'
import { useAuthStore } from '../stores/auth'
import BasicDataEmployeeNav from '../components/BasicDataEmployeeNav.vue'
import { permissionDisplayName, roleDisplayDescription, roleDisplayName } from '../lib/roleDisplay'

interface Role { id: string; code: string; name: string; description: string; permissionCodes: string[]; status: string }
interface Permission { id: string; code: string; name: string; module: string }

const { t, locale } = useI18n()
const auth = useAuthStore()
const canWrite = auth.can('iam:role:write')

const roles = ref<Role[]>([])
const permissions = ref<Permission[]>([])
const selected = ref<Role | null>(null)
const canEditSelected = computed(() => canWrite && selected.value?.code !== 'SUPER_ADMIN')
const checked = ref<string[]>([])
const loading = ref(false)
const saving = ref(false)
const createOpen = ref(false)
const permissionDialogOpen = ref(false)
const activePermissionModule = ref('')
// Absence of a row means SELF, so the picker shows SELF for an unconfigured
// role rather than a blank that hides which way it will actually behave.
const SCOPE_TYPES = ['SELF', 'DEPT', 'DEPT_AND_SUB', 'ALL']
// This is the complete list of modules for which a business service asks IAM
// to filter rows. Keeping the controls in one table makes omissions visible:
// quality and mail used to fall back silently to SELF because this page had no
// way to configure them, even when the role held the matching feature access.
const DATA_SCOPE_MODULES = [
  { key: 'export', label: 'roles.scopeExport' },
  { key: 'procurement_sourcing', label: 'roles.scopeSourcing' },
  { key: 'procurement_order', label: 'roles.scopeOrder' },
  { key: 'procurement_requirement', label: 'roles.scopeRequirement' },
  { key: 'shipping', label: 'roles.scopeShipping' },
  { key: 'quality', label: 'roles.scopeQuality' },
  { key: 'mail', label: 'roles.scopeMail' },
] as const
const scopes = ref<Record<string, string>>({})
const scopeValues = reactive<Record<string, string>>(
  Object.fromEntries(DATA_SCOPE_MODULES.map((m) => [m.key, 'SELF'])),
)
const form = reactive({ code: '', name: '', description: '' })
const statusSaving = ref(false)

// 停用是真的收权：持有这个角色的人会当场失去它带来的权限和数据范围。
// 所以先确认，而且把「还有几个人持有」这类拒绝原样弹出来——服务端会在
// 还有人持有时拒绝并给出人数，那句话正是人下一步要做的事。
async function toggleStatus() {
  if (!selected.value) return
  const role = selected.value
  const next = role.status === 'INACTIVE' ? 'ACTIVE' : 'INACTIVE'
  if (next === 'INACTIVE') {
    try {
      await ElMessageBox.confirm(t('roles.deactivateConfirm', { name: role.name }), t('roles.deactivate'), {
        type: 'warning',
      })
    } catch {
      return // 点了取消
    }
  }
  statusSaving.value = true
  try {
    await post(`/roles/${role.id}/status`, { status: next })
    ElMessage.success(next === 'INACTIVE' ? t('roles.deactivated') : t('roles.activated'))
    await load()
  } finally {
    statusSaving.value = false
  }
}

const grouped = computed(() => {
  const buckets: Record<string, Permission[]> = {}
  for (const p of permissions.value) {
    (buckets[p.module] ??= []).push(p)
  }
  const preferred = ['sales', 'export', 'procurement']
  const modules = Object.keys(buckets).sort((a, b) => {
    const ai = preferred.indexOf(a)
    const bi = preferred.indexOf(b)
    if (ai >= 0 || bi >= 0) return (ai < 0 ? preferred.length : ai) - (bi < 0 ? preferred.length : bi)
    return a.localeCompare(b)
  })
  const out: Record<string, Permission[]> = {}
  for (const module of modules) out[module] = buckets[module]
  return out
})

const activeModulePermissions = computed(() => grouped.value[activePermissionModule.value] ?? [])
const activePermissionModuleLabel = computed(() => moduleLabel(activePermissionModule.value))
const enabledModuleCount = computed(() =>
  Object.values(grouped.value).filter((items) => enabledPermissionCount(items) > 0).length,
)
const hasUnsavedChanges = computed(() => {
  if (!selected.value) return false
  const before = [...selected.value.permissionCodes].sort()
  const after = [...new Set(checked.value)].sort()
  if (before.length !== after.length || before.some((code, index) => code !== after[index])) return true
  return DATA_SCOPE_MODULES.some((module) => {
    const saved = scopes.value[`${selected.value!.id}:${module.key}`] ?? 'SELF'
    return scopeValues[module.key] !== saved
  })
})

// A data scope only matters when the role can use the corresponding business
// feature. Hiding unrelated rows turns seven abstract selectors into the two
// or three concrete visibility decisions this role actually needs. A
// non-default saved scope remains visible so an administrator can find and
// remove it even after withdrawing the related feature permissions.
const scopePermissionPrefixes: Record<string, string[]> = {
  export: ['sales:', 'export:'],
  procurement_sourcing: ['sales:inquiry:', 'sales:procurement-progress:', 'procurement:sourcing:', 'shipping:sourcing:'],
  procurement_order: [
    'procurement:order:', 'procurement:receipt:', 'procurement:production:',
    'procurement:exception:', 'procurement:payment:', 'procurement:recon:',
    'procurement:invoice:', 'procurement:reimbursement:',
  ],
  procurement_requirement: ['procurement:requirement:'],
  shipping: ['shipping:'],
  quality: ['quality:'],
  mail: ['mail:'],
}
const visibleScopeModules = computed(() => DATA_SCOPE_MODULES.filter((module) => {
  const configured = (scopes.value[`${selected.value?.id}:${module.key}`] ?? 'SELF') !== 'SELF'
  const relevant = checked.value.some((code) =>
    (scopePermissionPrefixes[module.key] ?? []).some((prefix) => code.startsWith(prefix)),
  )
  return configured || relevant
}))

function enabledPermissionCount(items: Permission[]): number {
  const enabled = new Set(checked.value)
  return items.filter((item) => enabled.has(item.code)).length
}

function modulePreview(items: Permission[]): string {
  const enabled = new Set(checked.value)
  const names = items.filter((item) => enabled.has(item.code)).map(displayPermissionName)
  if (!names.length) return t('roles.moduleDisabled')
  const preview = names.slice(0, 3).join('、')
  return names.length > 3 ? t('roles.modulePreviewMore', { preview, n: names.length - 3 }) : preview
}

function openPermissionModule(module: string) {
  activePermissionModule.value = module
  permissionDialogOpen.value = true
}

function selectAllActiveModule() {
  const next = new Set(checked.value)
  for (const permission of activeModulePermissions.value) next.add(permission.code)
  checked.value = [...next]
}

function clearActiveModule() {
  const moduleCodes = new Set(activeModulePermissions.value.map((permission) => permission.code))
  checked.value = checked.value.filter((code) => !moduleCodes.has(code))
}

async function load() {
  loading.value = true
  try {
    // /roles/all 而不是 /roles：这一页要看得见停用的角色。
    roles.value = (await get<{ roles: Role[] }>('/roles/all')).roles ?? []
    const keep = selected.value?.id
    selected.value = roles.value.find((r) => r.id === keep) ?? roles.value[0] ?? null
    checked.value = [...(selected.value?.permissionCodes ?? [])]
  } finally {
    loading.value = false
  }
}

function select(role: Role) {
  permissionDialogOpen.value = false
  selected.value = role
  checked.value = [...role.permissionCodes]
  syncScopeValues(role.id)
}

function syncScopeValues(roleId: string) {
  for (const module of DATA_SCOPE_MODULES) {
    scopeValues[module.key] = scopes.value[`${roleId}:${module.key}`] ?? 'SELF'
  }
}

async function loadScopes() {
  const data = await get<{ scopes: { roleId: string; module: string; scopeType: string }[] }>('/data-scopes')
  scopes.value = Object.fromEntries((data.scopes ?? []).map((s) => [`${s.roleId}:${s.module}`, s.scopeType]))
  if (selected.value) syncScopeValues(selected.value.id)
}

async function createRole() {
  if (!form.code || !form.name) {
    ElMessage.warning(t('roles.required'))
    return
  }
  saving.value = true
  try {
    await post('/roles', { code: form.code, name: form.name, description: form.description })
    ElMessage.success(t('roles.created'))
    createOpen.value = false
    Object.assign(form, { code: '', name: '', description: '' })
    await load()
  } finally {
    saving.value = false
  }
}

async function saveGrants() {
  saving.value = true
  try {
    // One save covers feature access and data visibility. The old page saved
    // these in separate places, which made it easy to grant a page but leave
    // its list silently restricted to SELF.
    await put(`/roles/${selected.value?.id}/permissions`, { permissionCodes: checked.value })
    for (const module of DATA_SCOPE_MODULES) {
      const saved = scopes.value[`${selected.value?.id}:${module.key}`] ?? 'SELF'
      if (scopeValues[module.key] === saved) continue
      await put(`/roles/${selected.value!.id}/data-scope`, {
        scope: { module: module.key, scopeType: scopeValues[module.key] },
      })
    }
    ElMessage.success(t('roles.grantsSaved'))
    await load()
    await loadScopes()
    // If the administrator edited a role they personally hold, update their
    // own navigation immediately instead of waiting for the focus/interval
    // synchronizer in the shell.
    await auth.refreshPermissionCodes().catch(() => {})
  } finally {
    saving.value = false
  }
}

function moduleLabel(module: string): string {
  const key = `roles.modules.${module}`
  const label = t(key)
  return label === key ? module : label
}

function displayRoleName(role: Role): string {
  return roleDisplayName(role.code, role.name, String(locale.value))
}

function displayRoleDescription(role: Role): string {
  return roleDisplayDescription(role.code, role.description, String(locale.value)) || t('roles.noDescription')
}

function displayPermissionName(permission: Permission): string {
  return permissionDisplayName(permission.code, permission.name, String(locale.value))
}

onMounted(async () => {
  permissions.value = (await get<{ permissions: Permission[] }>('/permissions')).permissions ?? []
  await load()
  await loadScopes()
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
.layout {
  display: grid;
  grid-template-columns: 220px 1fr;
  gap: 20px;
  min-height: 320px;
}
.role-column {
  border-right: 1px solid var(--el-border-color-lighter);
  padding-right: 12px;
}
.role-item {
  padding: 8px 10px;
  border-radius: 6px;
  cursor: pointer;
}
.role-item:hover {
  background: var(--el-fill-color-light);
}
.role-item.active {
  background: var(--el-color-primary-light-9);
}
.role-name {
  font-size: 14px;
}
.role-code {
  font-size: 12px;
  color: var(--el-text-color-secondary);
}
.matrix-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin-bottom: 14px;
}
.head-actions {
  display: flex;
  gap: 8px;
  flex-shrink: 0;
}
.matrix-title {
  font-size: 15px;
  font-weight: 500;
}
.matrix.empty {
  display: flex;
  align-items: center;
  justify-content: center;
  color: var(--el-text-color-secondary);
}
.super-admin-note {
  margin-bottom: 14px;
}
.access-overview {
  display: grid;
  grid-template-columns: 116px 116px 1fr;
  align-items: center;
  gap: 10px;
  padding: 12px;
  border: 1px solid var(--el-border-color-lighter);
  border-radius: 10px;
  background: var(--el-fill-color-extra-light);
}
.overview-number {
  display: flex;
  align-items: baseline;
  gap: 6px;
}
.overview-number strong { font-size: 24px; color: var(--el-color-primary); }
.overview-number span, .access-overview p { font-size: 12px; color: var(--el-text-color-secondary); }
.access-overview p { margin: 0; line-height: 1.6; }
.settings-section { margin-top: 18px; }
.section-heading {
  display: flex;
  align-items: end;
  justify-content: space-between;
  margin-bottom: 10px;
}
.section-heading h3 { margin: 0; font-size: 15px; }
.section-heading p { margin: 3px 0 0; font-size: 12px; color: var(--el-text-color-secondary); }
.capability-grid {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: 10px;
}
.capability-card {
  min-width: 0;
  padding: 12px;
  border: 1px solid var(--el-border-color-lighter);
  border-radius: 10px;
  background: #fff;
}
.capability-card.enabled {
  border-color: var(--el-color-primary-light-7);
  background: var(--el-color-primary-light-9);
}
.capability-head { display: flex; align-items: center; justify-content: space-between; gap: 8px; }
.capability-head strong { font-size: 14px; }
.capability-preview {
  height: 36px;
  margin: 8px 0 4px;
  overflow: hidden;
  color: var(--el-text-color-secondary);
  font-size: 12px;
  line-height: 18px;
}
.scope-grid {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 10px;
}
.scope-card {
  display: grid;
  grid-template-columns: minmax(130px, 1fr) minmax(150px, 210px);
  align-items: center;
  gap: 10px;
  padding: 10px 12px;
  border: 1px solid var(--el-border-color-lighter);
  border-radius: 9px;
  font-size: 13px;
}
.permission-dialog-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin-bottom: 12px;
  color: var(--el-text-color-secondary);
  font-size: 13px;
}
.permission-dialog-head small {
  display: block;
  margin-top: 4px;
  color: var(--el-color-info);
}
.dialog-permission-list {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 8px 14px;
  max-height: 52vh;
  overflow-y: auto;
  padding-right: 6px;
}
.permission-label { white-space: normal; line-height: 1.4; }
.hint {
  margin-left: 10px;
  font-size: 12px;
  color: var(--el-text-color-secondary);
}
.footnote {
  margin: 18px 0 0;
  font-size: 12px;
  color: var(--el-text-color-secondary);
}
@media (max-width: 1180px) {
  .layout { grid-template-columns: 180px 1fr; gap: 14px; }
  .capability-grid { grid-template-columns: repeat(2, minmax(0, 1fr)); }
  .scope-grid { grid-template-columns: 1fr; }
}
@media (max-width: 760px) {
  .layout { display: block; }
  .role-column {
    display: flex;
    gap: 6px;
    overflow-x: auto;
    border-right: 0;
    border-bottom: 1px solid var(--el-border-color-lighter);
    padding: 0 0 10px;
    margin-bottom: 12px;
  }
  .role-item { min-width: 132px; }
  .matrix-head { align-items: flex-start; gap: 10px; }
  .hint { display: block; margin: 3px 0 0; }
  .access-overview { grid-template-columns: 1fr 1fr; }
  .access-overview p { grid-column: 1 / -1; }
  .capability-grid, .scope-grid, .dialog-permission-list { grid-template-columns: 1fr; }
  .scope-card { grid-template-columns: 1fr; }
}
</style>
