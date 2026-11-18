<template>
  <el-dialog :model-value="modelValue" :title="t('orderBuyer.title')" width="min(560px,94vw)" :close-on-click-modal="false" :close-on-press-escape="!saving" :show-close="!saving" @update:model-value="emit('update:modelValue', $event)">
    <el-alert :title="t('orderBuyer.hint')" type="info" :closable="false" />
    <el-form label-position="top" class="buyer-form" v-loading="loading" @submit.prevent="save">
      <el-form-item :label="t('orderBuyer.current')"><el-input :model-value="order?.buyerName || '—'" disabled /></el-form-item>
      <el-form-item :label="t('orderBuyer.newBuyer')" required>
        <el-select v-model="buyerId" filterable :disabled="saving" :placeholder="t('orderBuyer.placeholder')" :no-data-text="loadFailed ? t('orderBuyer.loadFailed') : t('orderBuyer.empty')" style="width:100%">
          <el-option v-for="option in options" :key="option.value" :value="option.value" :label="option.label" />
        </el-select>
      </el-form-item>
      <el-form-item :label="t('orderBuyer.reason')"><el-input v-model="reason" maxlength="200" :disabled="saving" :placeholder="t('orderBuyer.reasonPlaceholder')" /></el-form-item>
    </el-form>
    <template #footer><el-button :disabled="saving" @click="emit('update:modelValue', false)">{{t('common.cancel')}}</el-button><el-button type="primary" :loading="saving" :disabled="loading" @click="save">{{t('common.confirm')}}</el-button></template>
  </el-dialog>
</template>
<script setup lang="ts">
// 把一张历史补录的采购单换给另一位采购员（#643）。
//
// 只做这一件事。谁看得见这张单、谁接得住，都由采购服务判断；这里出下拉、发
// 请求、把结果说清楚。候选名单每次打开都重新取——人会离职、权限会变，上一次
// 打开时的名单不该留到这一次。
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { ElMessage } from 'element-plus'
import { get, put, quietErrors } from '../api'
import { buyerChangeFailure, handOverOptions, isBuyerChange, type BuyerCandidate } from '../lib/orderBuyer'

const props = defineProps<{modelValue: boolean; order: {id: string; buyerId?: string; buyerName?: string} | null}>()
const emit = defineEmits<{'update:modelValue': [boolean]; saved: []}>()
const { t } = useI18n()
const loading = ref(false), saving = ref(false), loadFailed = ref(false)
const candidates = ref<BuyerCandidate[]>([]), buyerId = ref(''), reason = ref('')
// 「换给谁」里没有现在这位：他的名字上面那一栏写着，换给自己也不算换。
const options = computed(() => handOverOptions(candidates.value, props.order?.buyerId))

watch(() => props.modelValue, async open => {
  if (!open) return
  // 先清空：上一张单选的人、填的原因不能带到这一张。
  buyerId.value = ''; reason.value = ''; candidates.value = []; loadFailed.value = false
  loading.value = true
  try {
    candidates.value = (await get<{candidates: BuyerCandidate[]}>('/purchase-orders/buyer-candidates', undefined, quietErrors)).candidates ?? []
  } catch (err) {
    // 名单没取到和「确实没有人可选」是两回事：下拉里的那句空话也要跟着分开说，
    // 不然人会去查同事的权限，而问题其实在网络或服务这头。
    loadFailed.value = true
    ElMessage.error(buyerChangeFailure(err, t('orderBuyer.loadFailed')))
  } finally { loading.value = false }
})

async function save() {
  if (!props.order || saving.value || loading.value) return
  if (!isBuyerChange(buyerId.value, props.order.buyerId)) {
    // 没选，或者选回了现在这位：不发请求，也别假装做了什么。
    ElMessage.warning(buyerId.value ? t('orderBuyer.unchanged') : t('orderBuyer.required'))
    return
  }
  const id = props.order.id
  saving.value = true
  try {
    const result = await put<{buyerName: string; changed: boolean}>(`/purchase-orders/${id}/buyer`, {buyerId: buyerId.value, reason: reason.value.trim()}, quietErrors)
    ElMessage.success(result.changed ? t('orderBuyer.saved', {name: result.buyerName}) : t('orderBuyer.unchanged'))
    emit('saved')
    emit('update:modelValue', false)
  } catch (err) {
    ElMessage.error(buyerChangeFailure(err, t('orderBuyer.failed')))
  } finally { saving.value = false }
}
</script>
<style scoped>.buyer-form{margin-top:18px}</style>
