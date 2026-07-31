<template>
  <div v-if="fields.length" class="attr-form">
    <div v-for="d in fields" :key="d.key" class="attr-row">
      <label :class="{ req: d.isRequired }">
        {{ d.label }}
        <span v-if="d.unit" class="unit">({{ d.unit }})</span>
      </label>
      <el-select
        v-if="d.dataType === 'ENUM' && (d.enumValues || []).length"
        :model-value="values[d.key]"
        clearable
        filterable
        allow-create
        size="small"
        style="width: 100%"
        :placeholder="t('attrs.pick')"
        @update:model-value="(v: string) => set(d.key, v)"
      >
        <el-option v-for="v in d.enumValues" :key="v" :value="v" :label="v" />
      </el-select>
      <el-input
        v-else
        :model-value="display(d)"
        size="small"
        :placeholder="placeholderFor(d)"
        @update:model-value="(v: string) => set(d.key, v, d)"
      />
    </div>
  </div>
  <p v-else class="empty">{{ t('attrs.noTemplate') }}</p>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'

interface Def {
  key: string
  label: string
  dataType: string
  unit: string
  enumValues?: string[]
  level: string
  isRequired: boolean
}

const props = defineProps<{
  defs: Def[]
  level: string
  values: Record<string, unknown>
}>()
const emit = defineEmits<{ (e: 'update:values', v: Record<string, unknown>): void }>()

const { t } = useI18n()

const fields = computed(() => props.defs.filter((d) => d.level === props.level))

function display(d: Def): string {
  const v = props.values[d.key]
  return v === undefined || v === null ? '' : String(v)
}

function placeholderFor(d: Def): string {
  if (d.dataType === 'DIMENSION' || d.dataType === 'NUMBER') return t('attrs.numberHint')
  return ''
}

// Numeric fields go back as numbers, not strings. The server stores them as
// JSON numbers and matching compares them numerically, so a "3.0" here would
// simply never equal the 3.0 in the catalogue.
function set(key: string, raw: string, d?: Def) {
  const next = { ...props.values }
  if (raw === '' || raw === null || raw === undefined) {
    delete next[key]
  } else if (d && (d.dataType === 'DIMENSION' || d.dataType === 'NUMBER')) {
    const n = Number(raw)
    next[key] = Number.isFinite(n) ? n : raw
  } else {
    next[key] = raw
  }
  emit('update:values', next)
}
</script>

<style scoped>
.attr-form {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(200px, 1fr));
  gap: 10px 14px;
}
.attr-row label {
  display: block;
  font-size: 12px;
  color: var(--el-text-color-secondary);
  margin-bottom: 3px;
}
.attr-row label.req::after {
  content: ' *';
  color: var(--el-color-danger);
}
.unit {
  color: var(--el-text-color-placeholder);
}
.empty {
  margin: 0;
  font-size: 12px;
  color: var(--el-text-color-secondary);
}
</style>
