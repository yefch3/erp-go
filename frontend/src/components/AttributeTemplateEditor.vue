<template>
  <el-dialog
    :model-value="open"
    :title="t('attrs.templateFor', { name: categoryName })"
    width="1080px"
    top="5vh"
    @update:model-value="(v: boolean) => emit('update:open', v)"
  >
    <el-alert type="info" :closable="false" show-icon class="alert">{{ t('attrs.templateHint') }}</el-alert>

    <!-- Inherited fields are shown but not editable here: they belong to the
         ancestor that declared them, and editing them from a child would
         silently change every sibling category too. -->
    <template v-if="inherited.length">
      <div class="side-title">{{ t('attrs.inherited') }}</div>
      <el-table :data="inherited" size="small" class="inherited">
        <el-table-column :label="t('attrs.key')" width="150">
          <template #default="{ row }"><code>{{ row.key }}</code></template>
        </el-table-column>
        <el-table-column prop="label" :label="t('attrs.label')" width="110" />
        <el-table-column :label="t('attrs.dataType')" width="110">
          <template #default="{ row }">{{ t(`attrs.types.${row.dataType}`) }}</template>
        </el-table-column>
        <el-table-column :label="t('attrs.level')" width="90">
          <template #default="{ row }">{{ t(`attrs.levels.${row.level}`) }}</template>
        </el-table-column>
        <el-table-column :label="t('attrs.match')" min-width="150">
          <template #default="{ row }">
            {{ t(`attrs.strategies.${row.matchStrategy}`) }}
            <span v-if="row.tolerancePct" class="sub">±{{ trimPct(row.tolerancePct) }}%</span>
          </template>
        </el-table-column>
      </el-table>
    </template>

    <div class="side-title">
      {{ t('attrs.own') }}
      <el-button link type="primary" @click="addDef">{{ t('attrs.addField') }}</el-button>
    </div>
    <el-table :data="defs" size="small">
      <el-table-column :label="t('attrs.key')" width="140">
        <template #default="{ row }">
          <el-input v-model="row.key" size="small" placeholder="thickness_mm" />
        </template>
      </el-table-column>
      <el-table-column :label="t('attrs.label')" width="100">
        <template #default="{ row }"><el-input v-model="row.label" size="small" /></template>
      </el-table-column>
      <el-table-column :label="t('attrs.dataType')" width="105">
        <template #default="{ row }">
          <el-select v-model="row.dataType" size="small" @change="() => onTypeChange(row)">
            <el-option v-for="ty in TYPES" :key="ty" :value="ty" :label="t(`attrs.types.${ty}`)" />
          </el-select>
        </template>
      </el-table-column>
      <el-table-column :label="t('attrs.unit')" width="70">
        <template #default="{ row }"><el-input v-model="row.unit" size="small" /></template>
      </el-table-column>
      <el-table-column :label="t('attrs.level')" width="100">
        <template #default="{ row }">
          <el-select v-model="row.level" size="small">
            <el-option value="PRODUCT" :label="t('attrs.levels.PRODUCT')" />
            <el-option value="SKU" :label="t('attrs.levels.SKU')" />
          </el-select>
        </template>
      </el-table-column>
      <el-table-column :label="t('attrs.match')" width="110">
        <template #default="{ row }">
          <el-select v-model="row.matchStrategy" size="small">
            <el-option
              v-for="m in strategiesFor(row.dataType)"
              :key="m"
              :value="m"
              :label="t(`attrs.strategies.${m}`)"
            />
          </el-select>
        </template>
      </el-table-column>
      <el-table-column :label="t('attrs.tolerance')" width="80">
        <template #default="{ row }">
          <el-input
            v-if="row.matchStrategy === 'TOLERANCE'"
            v-model="row.tolerancePct"
            size="small"
            placeholder="3"
          />
          <span v-else class="sub">—</span>
        </template>
      </el-table-column>
      <el-table-column :label="t('attrs.enumValues')" min-width="140">
        <template #default="{ row }">
          <el-input
            v-if="row.dataType === 'ENUM'"
            v-model="row.enumText"
            size="small"
            :placeholder="t('attrs.enumPlaceholder')"
          />
          <span v-else class="sub">—</span>
        </template>
      </el-table-column>
      <el-table-column :label="t('attrs.flags')" width="118">
        <template #default="{ row }">
          <div class="flags">
            <el-checkbox v-model="row.isMatchable" size="small">{{ t('attrs.matchable') }}</el-checkbox>
            <el-checkbox v-model="row.isRequired" size="small">{{ t('attrs.required') }}</el-checkbox>
          </div>
        </template>
      </el-table-column>
      <el-table-column width="55">
        <template #default="{ $index }">
          <el-button link type="danger" @click="defs.splice($index, 1)">{{ t('common.delete') }}</el-button>
        </template>
      </el-table-column>
      <template #empty>{{ t('attrs.noFields') }}</template>
    </el-table>

    <template #footer>
      <el-button @click="emit('update:open', false)">{{ t('common.cancel') }}</el-button>
      <el-button type="primary" :loading="saving" @click="save">{{ t('common.save') }}</el-button>
    </template>
  </el-dialog>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { ElMessage } from 'element-plus'
import { useI18n } from 'vue-i18n'
import { get, put } from '../api'

interface DefRow {
  key: string
  label: string
  dataType: string
  unit: string
  enumValues?: string[]
  enumText: string
  matchStrategy: string
  tolerancePct: string
  level: string
  isMatchable: boolean
  isRequired: boolean
  fromCategoryId?: string
}

const TYPES = ['DIMENSION', 'NUMBER', 'ENUM', 'TEXT', 'RANGE']

const props = defineProps<{ open: boolean; categoryId: string; categoryName: string }>()
const emit = defineEmits<{ (e: 'update:open', v: boolean): void; (e: 'saved'): void }>()

const { t } = useI18n()
const defs = ref<DefRow[]>([])
const resolved = ref<DefRow[]>([])
const saving = ref(false)

// Everything the ancestry contributes that this category did not declare
// itself. Shown read-only so the difference is visible at a glance.
const inherited = computed(() => {
  const own = new Set(defs.value.map((d) => d.key))
  return resolved.value.filter((d) => String(d.fromCategoryId) !== String(props.categoryId) && !own.has(d.key))
})

function trimPct(v: string): string {
  const n = Number(v)
  return Number.isFinite(n) ? String(Number(n.toFixed(3))) : v
}

// Tolerance only means something on a number; offering it on an enum would
// produce a template that silently never matches.
function strategiesFor(dataType: string): string[] {
  if (dataType === 'DIMENSION' || dataType === 'NUMBER') return ['EXACT', 'TOLERANCE']
  if (dataType === 'RANGE') return ['EXACT', 'OVERLAP']
  if (dataType === 'ENUM') return ['EXACT', 'SYNONYM']
  return ['EXACT', 'FUZZY']
}

function onTypeChange(row: DefRow) {
  if (!strategiesFor(row.dataType).includes(row.matchStrategy)) {
    row.matchStrategy = 'EXACT'
    row.tolerancePct = ''
  }
}

function addDef() {
  defs.value.push({
    key: '', label: '', dataType: 'TEXT', unit: '', enumText: '',
    matchStrategy: 'EXACT', tolerancePct: '', level: 'SKU',
    isMatchable: true, isRequired: false,
  })
}

function toRow(d: Record<string, unknown>): DefRow {
  return {
    key: String(d.key ?? ''), label: String(d.label ?? ''),
    dataType: String(d.dataType ?? 'TEXT'), unit: String(d.unit ?? ''),
    enumText: ((d.enumValues as string[]) ?? []).join(' / '),
    matchStrategy: String(d.matchStrategy ?? 'EXACT'),
    tolerancePct: String(d.tolerancePct ?? ''),
    level: String(d.level ?? 'SKU'),
    isMatchable: d.isMatchable !== false,
    isRequired: d.isRequired === true,
    fromCategoryId: String(d.fromCategoryId ?? ''),
  }
}

async function load() {
  if (!props.categoryId) return
  const [own, all] = await Promise.all([
    get<{ defs: Record<string, unknown>[] }>(`/categories/${props.categoryId}/attribute-template`),
    get<{ defs: Record<string, unknown>[] }>(`/categories/${props.categoryId}/attributes`),
  ])
  defs.value = (own.defs ?? []).map(toRow)
  resolved.value = (all.defs ?? []).map(toRow)
}

async function save() {
  saving.value = true
  try {
    await put(`/categories/${props.categoryId}/attribute-template`, {
      name: props.categoryName,
      defs: defs.value.map((d) => ({
        key: d.key.trim(),
        label: d.label.trim(),
        data_type: d.dataType,
        unit: d.unit,
        // Split on the separator the placeholder shows, then drop blanks so a
        // trailing slash does not become an empty allowed value.
        enum_values: d.dataType === 'ENUM'
          ? d.enumText.split('/').map((v) => v.trim()).filter(Boolean)
          : [],
        match_strategy: d.matchStrategy,
        tolerance_pct: d.matchStrategy === 'TOLERANCE' ? d.tolerancePct : '',
        level: d.level,
        is_matchable: d.isMatchable,
        is_required: d.isRequired,
      })),
    })
    ElMessage.success(t('attrs.saved'))
    await load()
    emit('saved')
    emit('update:open', false)
  } finally {
    saving.value = false
  }
}

watch(() => [props.open, props.categoryId], () => {
  if (props.open) load()
}, { immediate: true })
</script>

<style scoped>
.alert {
  margin-bottom: 14px;
}
.side-title {
  display: flex;
  align-items: center;
  gap: 10px;
  margin: 14px 0 8px;
  font-size: 13px;
  font-weight: 600;
  color: var(--el-text-color-regular);
}
.inherited {
  opacity: 0.75;
}
.sub {
  font-size: 12px;
  color: var(--el-text-color-secondary);
}
code {
  font-size: 12px;
}
.flags {
  display: flex;
  flex-direction: column;
  line-height: 1.6;
}
</style>
