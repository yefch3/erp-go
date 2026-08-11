<template>
  <div>
    <BasicDataEmployeeNav />
    <div class="page-head">
      <h2>{{ t('departments.title') }}</h2>
      <el-button v-if="canWrite" type="primary" @click="openCreate">{{ t('departments.create') }}</el-button>
    </div>

    <el-card shadow="never">
      <div class="filters">
        <el-input v-model="keyword" clearable :placeholder="t('departments.search')" style="width: 280px" />
      </div>
      <el-table ref="departmentTable" v-loading="loading" :data="tree" row-key="id" :default-expand-all="Boolean(keyword)">
        <el-table-column prop="name" :label="t('departments.name')" min-width="220" />
        <el-table-column prop="code" :label="t('departments.code')" width="140" />
        <el-table-column :label="t('departments.leader')" width="160">
          <template #default="{ row }">{{ leaderName(row) }}</template>
        </el-table-column>
        <el-table-column prop="sortOrder" :label="t('departments.sortOrder')" width="90" />
        <el-table-column :label="t('common.status')" width="90">
          <template #default="{ row }">
            <el-tag :type="row.status === 'ACTIVE' ? 'success' : 'info'">
              {{ row.status === 'ACTIVE' ? t('common.active') : t('common.inactive') }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column :label="t('common.actions')" width="170">
          <template #default="{ row }">
            <el-button v-if="canWrite" link type="primary" @click="openEdit(row)">{{ t('common.edit') }}</el-button>
            <el-button link @click="openChanges(row)">{{ t('departments.changes') }}</el-button>
          </template>
        </el-table-column>
      </el-table>
    </el-card>

    <el-dialog v-model="dialogOpen" :title="editing ? t('departments.edit') : t('departments.create')" width="560px">
      <el-form :model="form" label-width="110px">
        <el-form-item :label="t('departments.code')" required><el-input v-model="form.code" /></el-form-item>
        <el-form-item :label="t('departments.name')" required><el-input v-model="form.name" /></el-form-item>
        <el-form-item :label="t('departments.parent')">
          <el-select v-model="form.parentId" clearable style="width: 100%">
            <el-option v-for="d in availableParents" :key="d.id" :value="d.id" :label="d.name" />
          </el-select>
        </el-form-item>
        <el-form-item :label="t('departments.sortOrder')">
          <div class="sort-field">
            <el-input-number v-model="form.sortOrder" :min="0" />
            <span class="form-hint">{{ t('departments.sortOrderHint') }}</span>
          </div>
        </el-form-item>
        <el-form-item v-if="editing" :label="t('departments.leader')">
          <el-select v-model="form.leaderEmployeeId" clearable filterable style="width: 100%">
            <el-option v-for="e in leaderCandidates" :key="e.id" :value="e.id" :label="`${e.code} · ${e.name}`" />
          </el-select>
        </el-form-item>
        <el-form-item v-if="editing" :label="t('common.status')">
          <el-radio-group v-model="form.status">
            <el-radio-button value="ACTIVE">{{ t('common.active') }}</el-radio-button>
            <el-radio-button value="INACTIVE">{{ t('common.inactive') }}</el-radio-button>
          </el-radio-group>
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="dialogOpen = false">{{ t('common.cancel') }}</el-button>
        <el-button type="primary" :loading="saving" @click="save">{{ t('common.save') }}</el-button>
      </template>
    </el-dialog>

    <el-drawer v-model="changesOpen" :title="t('departments.changes')" size="520px">
      <el-timeline>
        <el-timeline-item v-for="item in changes" :key="item.id" :timestamp="formatTime(item.createdAt)">
          {{ item.action }} · {{ t('departments.operator') }} #{{ item.operatorId }}
          <el-collapse class="change-values">
            <el-collapse-item :title="t('departments.changeValues')">
              <div>{{ t('departments.before') }}</div><pre>{{ prettyJSON(item.beforeJson) }}</pre>
              <div>{{ t('departments.after') }}</div><pre>{{ prettyJSON(item.afterJson) }}</pre>
            </el-collapse-item>
          </el-collapse>
        </el-timeline-item>
      </el-timeline>
      <el-empty v-if="!changes.length" :description="t('departments.noChanges')" />
    </el-drawer>
  </div>
</template>

<script setup lang="ts">
import { computed, nextTick, onMounted, reactive, ref, watch } from 'vue'
import type { ElTable } from 'element-plus'
import { ElMessage } from 'element-plus'
import { useI18n } from 'vue-i18n'
import { useRoute, useRouter } from 'vue-router'
import { get, post, put } from '../api'
import BasicDataEmployeeNav from '../components/BasicDataEmployeeNav.vue'
import { createDepartmentBody, updateDepartmentBody } from '../lib/iamForms'
import { useAuthStore } from '../stores/auth'

interface Department { id: string; code: string; name: string; parentId: string; path: string; sortOrder: number; status: string; leaderEmployeeId: string; version: number; children?: Department[] }
interface Employee { id: string; code: string; name: string; departmentId: string; status: string }
interface Change { id: string; action: string; operatorId: string; createdAt: string; beforeJson: string; afterJson: string }

const EMPTY = { id: '', code: '', name: '', parentId: '', sortOrder: 0, status: 'ACTIVE', leaderEmployeeId: '', version: 0 }
const { t } = useI18n()
const route = useRoute()
const router = useRouter()
const auth = useAuthStore()
const canWrite = auth.can('iam:department:write')
const departments = ref<Department[]>([])
const employees = ref<Employee[]>([])
const changes = ref<Change[]>([])
const keyword = ref(String(route.query.keyword ?? ''))
const loading = ref(false)
const departmentTable = ref<InstanceType<typeof ElTable>>()
const saving = ref(false)
const dialogOpen = ref(false)
const changesOpen = ref(false)
const editing = ref(false)
const form = reactive({ ...EMPTY })

// 保留命中的部门及其祖先节点，让搜索结果仍然能看出组织层级。
const tree = computed(() => {
  const q = keyword.value.trim().toLowerCase()
  const visible = new Set<string>()
  if (q) {
    for (const d of departments.value) {
      if (`${d.code} ${d.name}`.toLowerCase().includes(q)) {
        visible.add(d.id)
        let p = d.parentId
        while (p && p !== '0') {
          visible.add(p)
          p = departments.value.find((x) => x.id === p)?.parentId ?? ''
        }
      }
    }
  }
  const rows = departments.value.filter((d) => !q || visible.has(d.id)).map((d) => ({ ...d, children: [] as Department[] }))
  const byID = new Map(rows.map((d) => [d.id, d]))
  const roots: Department[] = []
  for (const d of rows) {
    const parent = byID.get(d.parentId)
    if (parent) parent.children!.push(d)
    else roots.push(d)
  }
  const sort = (items: Department[]) => items.sort((a, b) => a.sortOrder - b.sortOrder || a.name.localeCompare(b.name)).forEach((d) => sort(d.children ?? []))
  sort(roots)
  return roots
})

const current = computed(() => departments.value.find((d) => d.id === form.id))
const availableParents = computed(() => departments.value.filter((d) => d.status === 'ACTIVE' && d.id !== form.id && !(current.value && d.path.startsWith(current.value.path))))
const leaderCandidates = computed(() => employees.value.filter((e) => e.status === 'ACTIVE' && e.departmentId === form.id))

function leaderName(row: Department) {
  return employees.value.find((e) => e.id === row.leaderEmployeeId)?.name || '—'
}
function openCreate() { editing.value = false; Object.assign(form, EMPTY); dialogOpen.value = true }
function openEdit(row: Department) { editing.value = true; Object.assign(form, row); dialogOpen.value = true }

async function load() {
  loading.value = true
  try {
    const [d, e] = await Promise.all([
      get<{ departments: Department[] }>('/departments'),
      get<{ employees: Employee[] }>('/employees', { page: 1, page_size: 200 }),
    ])
    departments.value = d.departments ?? []
    employees.value = e.employees ?? []
  } finally { loading.value = false }
}

// 移动部门、停用部门和设置负责人都由后端在同一事务中校验并记录审计日志。
async function save() {
  if (!form.code.trim() || !form.name.trim()) { ElMessage.warning(t('departments.required')); return }
  saving.value = true
  try {
    if (editing.value) {
      await put(`/departments/${form.id}`, updateDepartmentBody(form))
    } else {
      const parentID = form.parentId
      await post('/departments', createDepartmentBody(form))
      await load()
      await nextTick()
      // 新建子部门后展开完整父级路径，让刚保存的数据立即出现在用户眼前。
      let ancestorID = parentID
      while (ancestorID && ancestorID !== '0') {
        const ancestor = departments.value.find((item) => item.id === ancestorID)
        if (!ancestor) break
        departmentTable.value?.toggleRowExpansion(ancestor, true)
        ancestorID = ancestor.parentId
      }
    }
    ElMessage.success(t(editing.value ? 'departments.updated' : 'departments.created'))
    dialogOpen.value = false
    if (editing.value) await load()
  } finally { saving.value = false }
}

async function openChanges(row: Department) {
  const data = await get<{ changes: Change[] }>(`/departments/${row.id}/changes`)
  changes.value = data.changes ?? []
  changesOpen.value = true
}
function formatTime(value: string) { return value ? new Date(value).toLocaleString() : '' }
function prettyJSON(value: string) { try { return JSON.stringify(JSON.parse(value || '{}'), null, 2) } catch { return value } }
watch(keyword, (value) => router.replace({ query: value ? { keyword: value } : {} }))
onMounted(load)
</script>

<style scoped>
.page-head, .filters { display: flex; align-items: center; justify-content: space-between; margin-bottom: 16px; }
.page-head h2 { margin: 0; font-size: 18px; font-weight: 500; }
.change-values pre { white-space: pre-wrap; word-break: break-all; font-size: 12px; }
.sort-field { display: flex; flex-direction: column; align-items: flex-start; gap: 4px; }
.form-hint { color: var(--el-text-color-secondary); font-size: 12px; line-height: 1.4; }
</style>
