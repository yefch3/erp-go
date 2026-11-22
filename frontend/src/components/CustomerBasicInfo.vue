<template>
  <section v-loading="loading" class="customer-basic">
    <div class="basic-heading">
      <h3>基本资料</h3>
    </div>
    <el-form label-position="top" size="small" :disabled="busy || loading || saving" @submit.prevent="save">
      <div class="basic-fields">
        <el-form-item
          v-for="field in fields"
          :key="field.key"
          :label="field.label"
          :required="canWrite && field.key === 'name'"
          :error="fieldErrors[field.key]"
          :class="{ 'basic-field--wide': field.key === 'address' }"
        >
          <div v-if="!canWrite || field.key === 'code' || !loaded" class="basic-value">
            {{ form[field.key] || '—' }}
          </div>
          <el-select v-else-if="field.key === 'creditGrade'" v-model="form.creditGrade" clearable placeholder="未设置">
            <el-option v-for="grade in ['A', 'B', 'C', 'D']" :key="grade" :label="grade" :value="grade" />
          </el-select>
          <el-input
            v-else
            v-model="form[field.key]"
            :maxlength="field.max"
            :type="field.key === 'address' ? 'textarea' : 'text'"
            :autosize="field.key === 'address' ? { minRows: 2 } : undefined"
            @input="delete fieldErrors[field.key]"
          />
        </el-form-item>
      </div>
      <div v-if="canWrite && dirty" class="basic-actions">
        <span v-if="saveError" class="basic-error" role="alert">{{ saveError }}</span>
        <el-button size="small" :disabled="busy || loading || saving" @click="reset">取消</el-button>
        <el-button size="small" native-type="submit" type="primary" :disabled="busy || loading" :loading="saving">保存修改</el-button>
      </div>
    </el-form>
  </section>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { ElMessage } from 'element-plus'
import { get, put, quietErrors, type Envelope } from '../api'
import { countryName } from '../lib/countries'
import { resolveImportedCountry } from '../lib/customerImport'

const props = defineProps<{ customerId: string; canWrite: boolean; busy?: boolean }>()
const emit = defineEmits<{ updated: []; "saving-change": [saving: boolean] }>()

const fields = [
  { key: 'name', label: '客户名称', max: 200 },
  { key: 'shortName', label: '客户简称', max: 100 },
  { key: 'code', label: '客户代码', max: 50 },
  { key: 'countryRegion', label: '所属地区', max: 100 },
  { key: 'customerType', label: '客户类型', max: 50 },
  { key: 'source', label: '客户来源', max: 50 },
  { key: 'archiveCreator', label: '建档人', max: 100 },
  { key: 'creditGrade', label: '客户等级', max: 32 },
  { key: 'postalCode', label: '邮政编码', max: 30 },
  { key: 'companyPhone', label: '公司电话', max: 50 },
  { key: 'faxNumber', label: '传真号码', max: 50 },
  { key: 'companyEmail', label: '电子信箱', max: 200 },
  { key: 'address', label: '通信地址', max: 500 },
] as const

type FieldKey = (typeof fields)[number]['key']
type BasicData = Partial<Record<FieldKey | 'countryCode' | 'addressState', string>>

const form = reactive<BasicData>({})
const baseline = ref<BasicData>({})
const fieldErrors = reactive<Partial<Record<FieldKey, string>>>({})
const saveError = ref('')
const loading = ref(false)
const saving = ref(false)
const loaded = ref(false)
const endpoint = computed(() => '/customers/' + props.customerId + '/basic')
const dirty = computed(() => loaded.value && fields.some(({ key }) => (form[key] || '') !== (baseline.value[key] || '')))

function applyBasic(data: BasicData, preserveEdits: boolean) {
  const next = { ...data }
  if (!next.countryRegion && next.countryCode) next.countryRegion = countryName(next.countryCode, 'zh-CN')
  for (const { key } of fields) {
    if (!preserveEdits || (form[key] || '') === (baseline.value[key] || '')) form[key] = next[key] || ''
  }
  baseline.value = next
  loaded.value = true
}

function reset() {
  applyBasic(baseline.value, false)
  for (const { key } of fields) delete fieldErrors[key]
  saveError.value = ''
}

async function load() {
  loading.value = true
  try {
    const { basic } = await get<{ basic?: BasicData }>(endpoint.value)
    applyBasic(basic ?? {}, true)
  } finally {
    loading.value = false
  }
}

async function save() {
  if (!props.canWrite || props.busy || loading.value || saving.value || !dirty.value) return
  saveError.value = ''
  if (!form.name?.trim()) {
    fieldErrors.name = '请填写客户名称'
    return
  }

  const regionChanged = form.countryRegion !== (baseline.value.countryRegion || '')
  const region = regionChanged
    ? resolveImportedCountry(form.countryRegion || '')
    : { countryCode: baseline.value.countryCode || '', state: baseline.value.addressState || '' }
  const countryCode = /^[A-Z]{2}$/.test(region.countryCode) ? region.countryCode : ''
  const body = { ...form, countryCode, addressState: region.state }
  saving.value = true
  emit("saving-change", true)
  try {
    const { basic } = await put<{ basic: BasicData }>(endpoint.value, { basic: body }, quietErrors)
    applyBasic(basic, false)
    emit('updated')
    ElMessage.success('基本资料已保存')
  } catch (error) {
    const failure = error as Partial<Envelope<unknown>> | undefined
    saveError.value = (failure?.success === false && failure.message) || '基本资料保存失败，请重试。'
    if (failure?.code === 'MD_COMPANY_EMAIL_INVALID') fieldErrors.companyEmail = saveError.value
  } finally {
    saving.value = false
    emit("saving-change", false)
  }
}

onMounted(load)
defineExpose({ load })
</script>

<style scoped>
.basic-heading { margin-bottom: 8px; }
.basic-heading h3 { margin: 0; font-size: 14px; color: #173b52; }
.basic-fields { display: grid; grid-template-columns: repeat(3, minmax(0, 1fr)); gap: 0 18px; }
.basic-fields :deep(.el-form-item) { min-width: 0; margin-bottom: 8px; }
.basic-fields :deep(.el-form-item__label) { margin-bottom: 2px; color: #526578; }
.basic-fields :deep(.el-select) { width: 100%; }
.basic-field--wide { grid-column: 1 / -1; }
.basic-value { min-height: 22px; line-height: 1.45; font-size: 13px; white-space: pre-wrap; overflow-wrap: anywhere; }
.basic-actions { display: flex; justify-content: flex-end; align-items: center; flex-wrap: wrap; gap: 8px; }
.basic-actions .el-button + .el-button { margin-left: 0; }
.basic-error { margin-inline-end: auto; color: var(--el-color-danger-dark-2); font-size: 13px; }
@media (max-width: 900px) {
  .basic-fields { grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 0 12px; }
}
@media (max-width: 540px) {
  .basic-fields { grid-template-columns: minmax(0, 1fr); }
}
</style>
