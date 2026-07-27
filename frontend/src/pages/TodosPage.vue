<template>
  <div>
    <div class="page-head">
      <h2>{{ t('todos.title') }}</h2>
      <el-button @click="load">{{ t('common.query') }}</el-button>
    </div>

    <el-card shadow="never">
      <el-radio-group v-model="scope" class="tabs" @change="() => { page = 1; load() }">
        <el-radio-button value="">{{ t('todos.tabPending') }}</el-radio-button>
        <el-radio-button value="APPROVED">{{ t('todos.tabApproved') }}</el-radio-button>
        <el-radio-button value="REJECTED">{{ t('todos.tabRejected') }}</el-radio-button>
        <el-radio-button value="RETURNED">{{ t('todos.tabReturned') }}</el-radio-button>
        <el-radio-button value="HANDLED">{{ t('todos.tabHandled') }}</el-radio-button>
      </el-radio-group>

      <el-table :data="todos" v-loading="loading">
        <el-table-column :label="t('todos.bizType')" width="110">
          <template #default="{ row }">
            <el-tag size="small" effect="plain">{{ bizTypeLabel(row.instance.bizType) }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column :label="t('todos.bizNo')" width="150">
          <template #default="{ row }">{{ row.instance.bizNo }}</template>
        </el-table-column>
        <el-table-column :label="t('todos.summary')" min-width="170">
          <template #default="{ row }">
            <span v-for="(v, k) in parseSummary(row.instance.bizSummary)" :key="k" class="sum">
              <span class="sum-k">{{ k }}</span>{{ v }}
            </span>
          </template>
        </el-table-column>
        <el-table-column :label="t('todos.node')" width="110">
          <template #default="{ row }">{{ row.task.nodeName }}</template>
        </el-table-column>
        <!-- Pending rows care who sent it and when; handled rows care what I
             decided and what became of the document, so the two tabs show
             different tails rather than one table too wide for either. -->
        <template v-if="pending">
          <el-table-column :label="t('todos.submitter')" width="80">
            <template #default="{ row }">{{ row.instance.submitterName }}</template>
          </el-table-column>
          <el-table-column :label="t('todos.submittedAt')" width="130">
            <template #default="{ row }">{{ formatTime(row.instance.submittedAt) }}</template>
          </el-table-column>
          <el-table-column :label="t('common.actions')" width="190" fixed="right">
            <template #default="{ row }">
              <el-button link type="success" @click="open(row, 'APPROVE')">{{ t('todos.approve') }}</el-button>
              <el-button link type="danger" @click="open(row, 'REJECT')">{{ t('todos.reject') }}</el-button>
              <el-button link type="warning" @click="open(row, 'RETURN')">{{ t('todos.return') }}</el-button>
            </template>
          </el-table-column>
        </template>
        <template v-else>
          <el-table-column :label="t('todos.myDecision')" min-width="200">
            <template #default="{ row }">
              <el-tag size="small" :type="taskTagType(row.task.status)">
                {{ taskStatusLabel(row.task.status) }}
              </el-tag>
              <span v-if="row.task.comment" class="comment">{{ row.task.comment }}</span>
            </template>
          </el-table-column>
          <el-table-column :label="t('todos.actedAt')" width="130">
            <template #default="{ row }">{{ formatTime(row.task.actedAt) }}</template>
          </el-table-column>
          <el-table-column :label="t('todos.docResult')" width="110" fixed="right">
            <template #default="{ row }">
              <el-tag size="small" effect="plain" :type="instanceTagType(row.instance.status)">
                {{ instanceStatusLabel(row.instance.status) }}
              </el-tag>
            </template>
          </el-table-column>
        </template>
        <template #empty>{{ pending ? t('todos.empty') : t('todos.emptyHandled') }}</template>
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

    <el-dialog v-model="dialogOpen" :title="actionTitle" width="480px">
      <p class="target">
        {{ current?.instance.bizNo }} · {{ current?.task.nodeName }}
      </p>
      <el-input
        v-model="comment"
        type="textarea"
        :rows="3"
        :placeholder="action === 'APPROVE' ? t('todos.commentOptional') : t('todos.commentRequired')"
      />
      <template #footer>
        <el-button @click="dialogOpen = false">{{ t('common.cancel') }}</el-button>
        <el-button :type="action === 'APPROVE' ? 'primary' : 'danger'" :loading="acting" @click="submit">
          {{ t('common.confirm') }}
        </el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { ElMessage } from 'element-plus'
import { useI18n } from 'vue-i18n'
import { get, post } from '../api'

interface Task { id: string; nodeSeq: number; nodeName: string; status: string }
interface Instance {
  id: string
  bizType: string
  bizId: string
  bizNo: string
  bizSummary: string
  submitterName: string
  status: string
  submittedAt: string
}
interface Todo { task: Task; instance: Instance }
type ActionCode = 'APPROVE' | 'REJECT' | 'RETURN'

const { t } = useI18n()
// '' = pending queue; the other values are past decisions of mine.
const scope = ref('')
const pending = computed(() => scope.value === '')
const todos = ref<Todo[]>([])
const total = ref(0)
const page = ref(1)
const pageSize = 10
const loading = ref(false)
const dialogOpen = ref(false)
const acting = ref(false)
const comment = ref('')
const action = ref<ActionCode>('APPROVE')
const current = ref<Todo | null>(null)

const actionTitle = computed(() => ({
  APPROVE: t('todos.approve'), REJECT: t('todos.reject'), RETURN: t('todos.return'),
}[action.value]))

async function load() {
  loading.value = true
  try {
    const data = await get<{ todos: Todo[]; meta: { total: string } }>('/approvals/todos', {
      page: page.value, page_size: pageSize, status: scope.value,
    })
    todos.value = data.todos ?? []
    total.value = Number(data.meta.total)
  } finally {
    loading.value = false
  }
}

function open(row: Todo, next: ActionCode) {
  current.value = row
  action.value = next
  comment.value = ''
  dialogOpen.value = true
}

async function submit() {
  // Turning something down has to say why; approving speaks for itself.
  if (action.value !== 'APPROVE' && !comment.value.trim()) {
    ElMessage.warning(t('todos.commentRequired'))
    return
  }
  acting.value = true
  try {
    await post(`/approvals/tasks/${current.value?.task.id}/act`, {
      action: action.value, comment: comment.value,
    })
    ElMessage.success(t('todos.acted'))
    dialogOpen.value = false
    load()
  } finally {
    acting.value = false
  }
}

// The summary is whatever the business service denormalized at submit time,
// so it is rendered generically rather than assuming known keys.
function parseSummary(raw: string): Record<string, string> {
  if (!raw) return {}
  try {
    const parsed = JSON.parse(raw)
    return typeof parsed === 'object' && parsed !== null ? parsed : {}
  } catch {
    return {}
  }
}

// A task can be APPROVED while the document later got rejected downstream,
// so both statuses are shown and neither is derived from the other.
function taskStatusLabel(code: string): string {
  return labelOr(`todos.task.${code}`, code)
}

function instanceStatusLabel(code: string): string {
  return labelOr(`todos.doc.${code}`, code)
}

function taskTagType(code: string): 'success' | 'danger' | 'warning' | 'info' {
  return { APPROVED: 'success', REJECTED: 'danger', RETURNED: 'warning' }[code] as
    'success' | 'danger' | 'warning' | undefined ?? 'info'
}

function instanceTagType(code: string): 'success' | 'danger' | 'warning' | 'info' {
  return { APPROVED: 'success', REJECTED: 'danger', RETURNED: 'warning' }[code] as
    'success' | 'danger' | 'warning' | undefined ?? 'info'
}

function labelOr(key: string, fallback: string): string {
  const label = t(key)
  return label === key ? fallback : label
}

function bizTypeLabel(code: string): string {
  return labelOr(`todos.biz.${code}`, code)
}

function formatTime(iso: string): string {
  return iso ? iso.replace('T', ' ').slice(0, 16) : ''
}

onMounted(load)
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
.tabs {
  margin-bottom: 14px;
}
.pager {
  margin-top: 14px;
  justify-content: flex-end;
}
.sum + .sum {
  margin-left: 16px;
}
.comment {
  margin-left: 10px;
  color: var(--el-text-color-regular);
}
.sum-k {
  margin-right: 6px;
  color: var(--el-text-color-secondary);
  font-size: 12px;
}
.target {
  margin: 0 0 12px;
  color: var(--el-text-color-regular);
}
</style>
