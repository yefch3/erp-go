<template>
  <el-select
    :model-value="modelValue"
    filterable
    clearable
    remote
    :remote-method="load"
    :loading="loading"
    :placeholder="t('common.selectSupplier')"
    @update:model-value="change"
  >
    <el-option
      v-for="item in suppliers"
      :key="item.id"
      :value="item.id"
      :label="`${item.code} · ${item.nameZh || item.nameEn || item.name}`"
    />
  </el-select>
</template>

<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { get } from '../../api'

interface SupplierOption { id:string; code:string; name:string; nameZh:string; nameEn:string; status:string }
const props = defineProps<{ modelValue:string|number }>()
const emit = defineEmits<{ 'update:modelValue':[string|number]; selected:[SupplierOption|undefined] }>()
const { t } = useI18n()
const suppliers = ref<SupplierOption[]>([])
const loading = ref(false)

async function load(keyword = '') {
  loading.value = true
  try {
    const data = await get<{suppliers:SupplierOption[]}>('/suppliers', { page:1, page_size:100, keyword, status:'ACTIVE' })
    suppliers.value = data.suppliers ?? []
  } finally {
    loading.value = false
  }
}

function change(value:string|number) {
  emit('update:modelValue', value)
  emit('selected', suppliers.value.find(item => String(item.id) === String(value)))
}

onMounted(() => load())
</script>

<style scoped>.el-select{width:100%}</style>
