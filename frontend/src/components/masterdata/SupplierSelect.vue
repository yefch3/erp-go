<template>
  <el-select
    :model-value="modelValue ? String(modelValue) : ''"
    filterable
    clearable
    remote
    :remote-method="load"
    :loading="loading"
    :disabled="disabled"
    :placeholder="placeholder ?? t('common.selectSupplier')"
    @update:model-value="change"
  >
    <el-option
      v-for="item in options"
      :key="item.id"
      :value="item.id"
      :label="[item.code, item.nameZh || item.nameEn || item.name].filter(Boolean).join(' · ')"
    />
  </el-select>
</template>

<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { get } from '../../api'

interface SupplierOption { id:string; code:string; name:string; nameZh?:string; nameEn?:string; status?:string }
const props = defineProps<{ modelValue:string|number; pinnedOption?:SupplierOption; disabled?: boolean; placeholder?: string }>()
const emit = defineEmits<{ 'update:modelValue':[string|number]; selected:[SupplierOption|undefined] }>()
const { t } = useI18n()
const suppliers = ref<SupplierOption[]>([])
const selected = ref<SupplierOption>()
const loading = ref(false)
let requestID = 0

const options = computed(() => {
  const entries = new Map<string, SupplierOption>()
  const current = selected.value?.id === String(props.modelValue) ? selected.value : undefined
  for (const item of [props.pinnedOption, current, ...suppliers.value]) {
    if (item && !entries.has(item.id)) entries.set(item.id, item)
  }
  return [...entries.values()]
})

async function load(keyword = '') {
  if (props.disabled) return
  const id = ++requestID
  loading.value = true
  try {
    const data = await get<{suppliers:SupplierOption[]}>('/suppliers', { page:1, page_size:100, keyword:keyword.trim(), status:'ACTIVE' })
    if (id === requestID) suppliers.value = data.suppliers ?? []
  } finally {
    if (id === requestID) loading.value = false
  }
}

function change(value:string|number) {
  selected.value = options.value.find(item => item.id === String(value))
  emit('update:modelValue', value)
  emit('selected', selected.value)
}

onMounted(() => load())
watch(() => props.disabled, disabled => { if (!disabled) void load() })
</script>

<style scoped>.el-select{width:100%}</style>
