<template>
  <el-dialog :model-value="modelValue" :title="t('historicalShipping.edit')" width="min(600px,94vw)" :close-on-click-modal="false" :close-on-press-escape="!saving" :show-close="!saving" @update:model-value="emit('update:modelValue', $event)">
    <el-alert :title="t('historicalShipping.hint')" type="info" :closable="false" />
    <el-form label-position="top" class="trade-form" @submit.prevent="save">
      <el-form-item :label="t('historicalShipping.optionalContract')"><el-input v-model="form.exportContractNo" maxlength="50" :disabled="saving" :placeholder="t('historicalShipping.contractPlaceholder')" /></el-form-item>
      <el-form-item :label="t('historicalShipping.optionalPort')"><el-input v-model="form.destinationPort" maxlength="200" :disabled="saving" :placeholder="t('historicalShipping.portPlaceholder')" /></el-form-item>
    </el-form>
    <template #footer><el-button :disabled="saving" @click="emit('update:modelValue', false)">{{t('common.cancel')}}</el-button><el-button type="primary" :loading="saving" @click="save">{{t('common.save')}}</el-button></template>
  </el-dialog>
</template>
<script setup lang="ts">
import { reactive, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { ElMessage } from 'element-plus'
import { put } from '../api'
import { historicalShippingContext } from '../lib/historicalShippingContext'
const props = defineProps<{modelValue: boolean; order: {id: string; exportContractNo?: string; destinationPort?: string} | null}>()
const emit = defineEmits<{'update:modelValue': [boolean]; saved: []}>()
const { t } = useI18n()
const saving = ref(false)
const form = reactive({exportContractNo: '', destinationPort: ''})
watch(() => props.modelValue, open => {
  if (open) Object.assign(form, historicalShippingContext(props.order))
})
async function save() {
  if (!props.order || saving.value) return
  const id = props.order.id
  saving.value = true
  try {
    await put(`/purchase-orders/${id}/historical-shipping-context`, historicalShippingContext(form))
    ElMessage.success(t('historicalShipping.saved'))
    emit('saved')
    emit('update:modelValue', false)
  } finally { saving.value = false }
}
</script>
<style scoped>.trade-form{margin-top:18px}</style>
