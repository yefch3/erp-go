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
            <div class="role-name">
              {{ r.name }}
              <!-- 停用的仍然列出来，否则停掉之后没有任何入口能把它启用回来。 -->
              <el-tag v-if="r.status === 'INACTIVE'" type="info" size="small" effect="plain">
                {{ t('roles.inactive') }}
              </el-tag>
            </div>
            <div class="role-code">{{ r.code }} · {{ r.permissionCodes.length }} {{ t('roles.permissionCount') }}</div>
          </div>
        </div>

        <div class="matrix" v-if="selected">
          <div class="matrix-head">
            <div>
              <span class="matrix-title">{{ selected.name }}</span>
              <span class="hint">{{ selected.description || t('roles.noDescription') }}</span>
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
              <el-button v-if="canWrite" type="primary" :loading="saving" @click="saveGrants">
                {{ t('roles.saveGrants') }}
              </el-button>
            </div>
          </div>
          <!-- Grouped by module because that is how people think about it:
               "can this role touch customers", not "code #7". -->
          <div v-for="(items, module) in grouped" :key="module" class="module">
            <div class="module-name">{{ moduleLabel(module) }}</div>
            <el-checkbox-group v-model="checked" :disabled="!canWrite" class="perm-list">
              <el-checkbox v-for="p in items" :key="p.code" :value="p.code">
                {{ p.name }}
                <span class="perm-code">{{ p.code }}</span>
              </el-checkbox>
            </el-checkbox-group>
          </div>
          <!-- Permissions say which features a role may use; the scope says
               whose records those features operate on. -->
          <div class="module">
            <div class="module-name">{{ t('roles.dataScope') }}</div>
            <div class="scope-row">
              <span class="scope-label">{{ t('roles.scopeExport') }}</span>
              <el-select v-model="scopeExport" :disabled="!canWrite" style="width: 220px">
                <el-option v-for="k in SCOPE_TYPES" :key="k" :value="k" :label="t(`roles.scopes.${k}`)" />
              </el-select>
              <el-button v-if="canWrite" :loading="savingScope" @click="saveScope">
                {{ t('roles.saveScope') }}
              </el-button>
            </div>
            <div class="scope-row">
              <span class="scope-label">{{ t('roles.scopeSourcing') }}</span>
              <el-select v-model="scopeSourcing" :disabled="!canWrite" style="width: 220px">
                <el-option v-for="k in SCOPE_TYPES" :key="k" :value="k" :label="t(`roles.scopes.${k}`)" />
              </el-select>
              <el-button v-if="canWrite" :loading="savingScope" @click="saveSourcingScope">
                {{ t('roles.saveScope') }}
              </el-button>
            </div>
            <div class="scope-row">
              <span class="scope-label">{{ t('roles.scopeOrder') }}</span>
              <el-select v-model="scopeOrder" :disabled="!canWrite" style="width: 220px">
                <el-option v-for="k in SCOPE_TYPES" :key="k" :value="k" :label="t(`roles.scopes.${k}`)" />
              </el-select>
              <el-button v-if="canWrite" :loading="savingScope" @click="saveOrderScope">
                {{ t('roles.saveScope') }}
              </el-button>
            </div>
            <div class="scope-row">
              <span class="scope-label">{{ t('roles.scopeRequirement') }}</span>
              <el-select v-model="scopeRequirement" :disabled="!canWrite" style="width: 220px">
                <el-option v-for="k in SCOPE_TYPES" :key="k" :value="k" :label="t(`roles.scopes.${k}`)" />
              </el-select>
              <el-button v-if="canWrite" :loading="savingScope" @click="saveRequirementScope">
                {{ t('roles.saveScope') }}
              </el-button>
            </div>
            <div class="scope-row">
              <span class="scope-label">{{ t('roles.scopeShipping') }}</span>
              <el-select v-model="scopeShipping" :disabled="!canWrite" style="width: 220px">
                <el-option v-for="k in SCOPE_TYPES" :key="k" :value="k" :label="t(`roles.scopes.${k}`)" />
              </el-select>
              <el-button v-if="canWrite" :loading="savingScope" @click="saveShippingScope">
                {{ t('roles.saveScope') }}
              </el-button>
            </div>
            <p class="footnote">{{ t('roles.scopeHint') }}</p>
          </div>
          <p class="footnote">{{ t('roles.serverEnforced') }}</p>
        </div>
        <div v-else class="matrix empty">{{ t('roles.pickRole') }}</div>
      </div>
    </el-card>

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

interface Role { id: string; code: string; name: string; description: string; permissionCodes: string[]; status: string }
interface Permission { id: string; code: string; name: string; module: string }

const { t } = useI18n()
const auth = useAuthStore()
const canWrite = auth.can('iam:role:write')

const roles = ref<Role[]>([])
const permissions = ref<Permission[]>([])
const selected = ref<Role | null>(null)
const checked = ref<string[]>([])
const loading = ref(false)
const saving = ref(false)
const createOpen = ref(false)
// Absence of a row means SELF, so the picker shows SELF for an unconfigured
// role rather than a blank that hides which way it will actually behave.
const SCOPE_TYPES = ['SELF', 'DEPT', 'DEPT_AND_SUB', 'ALL']
const scopes = ref<Record<string, string>>({})
const scopeExport = ref('SELF')
const scopeSourcing = ref('SELF')
const scopeOrder = ref('SELF')
const scopeRequirement = ref('SELF')
const scopeShipping = ref('SELF')
const savingScope = ref(false)
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
  selected.value = role
  checked.value = [...role.permissionCodes]
  scopeExport.value = scopes.value[`${role.id}:export`] ?? 'SELF'
  scopeSourcing.value = scopes.value[`${role.id}:procurement_sourcing`] ?? 'SELF'
  scopeOrder.value = scopes.value[`${role.id}:procurement_order`] ?? 'SELF'
  scopeRequirement.value = scopes.value[`${role.id}:procurement_requirement`] ?? 'SELF'
  scopeShipping.value = scopes.value[`${role.id}:shipping`] ?? 'SELF'
}

async function loadScopes() {
  const data = await get<{ scopes: { roleId: string; module: string; scopeType: string }[] }>('/data-scopes')
  scopes.value = Object.fromEntries((data.scopes ?? []).map((s) => [`${s.roleId}:${s.module}`, s.scopeType]))
  if (selected.value) {
    scopeExport.value = scopes.value[`${selected.value.id}:export`] ?? 'SELF'
    scopeSourcing.value = scopes.value[`${selected.value.id}:procurement_sourcing`] ?? 'SELF'
    scopeOrder.value = scopes.value[`${selected.value.id}:procurement_order`] ?? 'SELF'
    scopeRequirement.value = scopes.value[`${selected.value.id}:procurement_requirement`] ?? 'SELF'
    scopeShipping.value = scopes.value[`${selected.value.id}:shipping`] ?? 'SELF'
  }
}

async function saveScope() {
  savingScope.value = true
  try {
    await put(`/roles/${selected.value!.id}/data-scope`, {
      scope: { module: 'export', scopeType: scopeExport.value },
    })
    ElMessage.success(t('roles.scopeSaved'))
    await loadScopes()
  } finally {
    savingScope.value = false
  }
}

async function saveSourcingScope() {
  savingScope.value = true
  try {
    await put(`/roles/${selected.value!.id}/data-scope`, {
      scope: { module: 'procurement_sourcing', scopeType: scopeSourcing.value },
    })
    ElMessage.success(t('roles.scopeSaved'))
    await loadScopes()
  } finally {
    savingScope.value = false
  }
}

async function saveOrderScope() {
  savingScope.value = true
  try {
    await put(`/roles/${selected.value!.id}/data-scope`, {
      scope: { module: 'procurement_order', scopeType: scopeOrder.value },
    })
    ElMessage.success(t('roles.scopeSaved'))
    await loadScopes()
  } finally {
    savingScope.value = false
  }
}

async function saveRequirementScope() {
  savingScope.value = true
  try {
    await put(`/roles/${selected.value!.id}/data-scope`, {
      scope: { module: 'procurement_requirement', scopeType: scopeRequirement.value },
    })
    ElMessage.success(t('roles.scopeSaved'))
    await loadScopes()
  } finally {
    savingScope.value = false
  }
}

async function saveShippingScope() {
  savingScope.value = true
  try {
    await put(`/roles/${selected.value!.id}/data-scope`, {
      scope: { module: 'shipping', scopeType: scopeShipping.value },
    })
    ElMessage.success(t('roles.scopeSaved'))
    await loadScopes()
  } finally {
    savingScope.value = false
  }
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
    // The backend replaces the whole set, so what is checked here is exactly
    // what the role ends up with.
    await put(`/roles/${selected.value?.id}/permissions`, { permissionCodes: checked.value })
    ElMessage.success(t('roles.grantsSaved'))
    await load()
  } finally {
    saving.value = false
  }
}

function moduleLabel(module: string): string {
  const key = `roles.modules.${module}`
  const label = t(key)
  return label === key ? module : label
}

onMounted(async () => {
  permissions.value = (await get<{ permissions: Permission[] }>('/permissions')).permissions ?? []
  await load()
  await loadScopes()
})
</script>

<style scoped>
.scope-row {
  display: flex;
  align-items: center;
  gap: 10px;
}
.scope-row + .scope-row {
  margin-top: 10px;
}
.scope-label {
  font-size: 13px;
  color: var(--el-text-color-regular);
  min-width: 72px;
}

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
.module {
  margin-bottom: 14px;
}
.module-name {
  font-size: 13px;
  color: var(--el-text-color-secondary);
  margin-bottom: 4px;
}
.perm-list {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 2px 16px;
}
.perm-code {
  margin-left: 6px;
  font-size: 12px;
  color: var(--el-text-color-placeholder);
}
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
</style>
