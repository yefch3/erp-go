<template>
  <el-select :model-value="modelValue" clearable filterable :placeholder="placeholder" @update:model-value="update">
    <el-option v-if="allowDirectDelivery" :label="directLabel" value="DIRECT" />
    <el-option
      v-for="warehouse in activeWarehouses"
      :key="warehouse.id"
      :label="`${warehouse.code} · ${warehouse.name}`"
      :value="`WAREHOUSE:${warehouse.id}`"
    />
  </el-select>
</template>

<script setup lang="ts">
import { computed } from 'vue'

export interface WarehouseOption {
  id: string
  code: string
  name: string
  status?: string
}

const props = withDefaults(defineProps<{
  modelValue: string
  warehouses: WarehouseOption[]
  allowDirectDelivery?: boolean
  placeholder?: string
  directLabel?: string
}>(), {
  allowDirectDelivery: true,
  placeholder: '请选择交付地点',
  directLabel: '直接发往港口 / 指定地点',
})

const emit = defineEmits<{ 'update:modelValue': [value: string] }>()
const activeWarehouses = computed(() => props.warehouses.filter((item) => item.status !== 'INACTIVE'))
function update(value: string) { emit('update:modelValue', value ?? '') }
</script>
