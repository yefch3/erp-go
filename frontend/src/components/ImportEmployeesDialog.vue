<template>
  <!-- Bringing a whole company in.

       Paste rather than file upload, because Excel on Chinese Windows writes
       CSV in GBK and a file would spend its life mis-decoding 姓名 for exactly
       the people most likely to use this. The clipboard hands us decoded text.

       Two steps, always: nothing is written until somebody has seen the same
       validation the write will run. -->
  <el-dialog
    :model-value="open"
    :title="t('import.title')"
    width="820px"
    @update:model-value="close"
  >
    <template v-if="!verdicts.length">
      <p class="lead">{{ t('import.lead') }}</p>
      <div class="cols">
        <span v-for="c in columnHints" :key="c" class="col-chip">{{ c }}</span>
      </div>
      <el-input
        v-model="text"
        type="textarea"
        :rows="12"
        :placeholder="t('import.placeholder')"
        class="paste"
      />
      <p class="note">{{ t('import.note') }}</p>
    </template>

    <template v-else>
      <!-- The count first, because it is the whole answer. Somebody who
           pasted 80 rows wants to know "can I press the button" before they
           read a single line of detail. -->
      <div class="summary" :class="{ bad: blocked > 0 }">
        <template v-if="blocked > 0">
          {{ t('import.blockedSummary', { blocked, ready }) }}
        </template>
        <template v-else>
          {{ t('import.readySummary', { ready }) }}
        </template>
      </div>
      <el-table :data="verdicts" max-height="380" size="small">
        <el-table-column prop="line" :label="t('import.line')" width="60" />
        <el-table-column prop="code" :label="t('employees.code')" width="110" />
        <el-table-column prop="name" :label="t('employees.name')" width="110" />
        <el-table-column :label="t('common.status')" min-width="300">
          <template #default="{ row }">
            <span v-if="row.ok" class="ok">✓ {{ t('import.rowReady') }}</span>
            <span v-else class="bad-cell">{{ row.reason }}</span>
          </template>
        </el-table-column>
      </el-table>
    </template>

    <template #footer>
      <el-button v-if="verdicts.length" @click="back">{{ t('import.backToPaste') }}</el-button>
      <el-button @click="close">{{ t('common.cancel') }}</el-button>
      <el-button
        v-if="!verdicts.length"
        type="primary"
        :loading="busy"
        :disabled="!text.trim()"
        @click="preview"
      >
        {{ t('import.preview') }}
      </el-button>
      <!-- Only offered when nothing is wrong. The batch is all-or-nothing on
           the server too; a button that could be pressed and then refused
           would just be a slower way of reading the same list. -->
      <el-button
        v-else
        type="primary"
        :loading="busy"
        :disabled="blocked > 0"
        @click="commit"
      >
        {{ t('import.commit', { n: ready }) }}
      </el-button>
    </template>
  </el-dialog>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { ElMessage } from 'element-plus'
import { useI18n } from 'vue-i18n'
import { post } from '../api'
import { parsePaste, type ImportRow } from '../lib/pasteTable'

const props = defineProps<{ open: boolean }>()
const emit = defineEmits<{ 'update:open': [boolean]; imported: [] }>()
const { t } = useI18n()

interface Verdict {
  line: number
  code: string
  name: string
  ok: boolean
  reason: string
}

const text = ref('')
const rows = ref<ImportRow[]>([])
const verdicts = ref<Verdict[]>([])
const ready = ref(0)
const blocked = ref(0)
const busy = ref(false)

const columnHints = computed(() => [
  t('employees.code'), t('employees.name'), t('employees.department'),
  t('employees.position'), t('employees.email'), t('employees.phone'),
  t('import.managerCode'),
])

watch(
  () => props.open,
  (isOpen) => {
    if (isOpen) reset()
  },
)

function reset() {
  text.value = ''
  rows.value = []
  verdicts.value = []
  ready.value = 0
  blocked.value = 0
}

function close() {
  emit('update:open', false)
}

function back() {
  verdicts.value = []
}

async function send(dryRun: boolean) {
  const d = await post<{ verdicts: Verdict[]; ready: number; blocked: number; imported: number }>(
    '/employees/import',
    { rows: rows.value, dryRun },
  )
  verdicts.value = d.verdicts ?? []
  ready.value = Number(d.ready ?? 0)
  blocked.value = Number(d.blocked ?? 0)
  return Number(d.imported ?? 0)
}

async function preview() {
  const parsed = parsePaste(text.value)
  if (parsed.rows.length === 0) {
    ElMessage.warning(t('import.nothingPasted'))
    return
  }
  rows.value = parsed.rows
  busy.value = true
  try {
    await send(true)
    if (parsed.usedHeader) ElMessage.info(t('import.headerDetected'))
  } finally {
    busy.value = false
  }
}

async function commit() {
  busy.value = true
  try {
    // Re-validated on the way in, not trusted from the preview: departments
    // and addresses can change between the two clicks, and the server is the
    // only thing that knows.
    const imported = await send(false)
    if (imported === 0) {
      // The preview was stale — something became invalid between the clicks.
      // The fresh verdicts are already on screen, so say why and stay put.
      ElMessage.warning(t('import.becameInvalid'))
      return
    }
    ElMessage.success(t('import.done', { n: imported }))
    emit('imported')
    close()
  } finally {
    busy.value = false
  }
}
</script>

<style scoped>
.lead {
  margin: 0 0 8px;
  font-size: 13px;
  color: var(--el-text-color-regular);
}
.cols {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
  margin-bottom: 10px;
}
.col-chip {
  padding: 2px 8px;
  border: 1px solid var(--el-border-color);
  border-radius: 10px;
  font-size: 12px;
  color: var(--el-text-color-secondary);
}
.paste :deep(textarea) {
  font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
  font-size: 12px;
  line-height: 1.7;
}
.note {
  margin: 8px 0 0;
  font-size: 12px;
  line-height: 1.6;
  color: var(--el-text-color-secondary);
}
.summary {
  margin-bottom: 10px;
  padding: 8px 12px;
  border-radius: 6px;
  background: var(--el-color-success-light-9);
  font-size: 13px;
  color: var(--el-color-success);
}
.summary.bad {
  background: var(--el-color-danger-light-9);
  color: var(--el-color-danger);
}
.ok {
  color: var(--el-color-success);
}
.bad-cell {
  color: var(--el-color-danger);
}
</style>
