<template>
  <div class="profile-grid">
    <el-form-item :label="t('suppliers.nameZh')"><el-input v-model="model.nameZh" /></el-form-item>
    <el-form-item :label="t('suppliers.nameEn')"><el-input v-model="model.nameEn" /></el-form-item>
    <el-form-item :label="t('suppliers.shortName')"><el-input v-model="model.shortName" /></el-form-item>
    <el-form-item :label="t('suppliers.country')">
      <el-select v-model="model.countryCode" filterable clearable>
        <el-option v-for="country in countries" :key="country.code" :label="country.name" :value="country.code" />
      </el-select>
    </el-form-item>
    <el-form-item :label="t('suppliers.taxId')"><el-input v-model="model.taxId" /></el-form-item>
    <el-form-item :label="t('suppliers.businessType')" class="profile-wide">
      <el-checkbox-group v-model="model.businessTypes">
        <el-checkbox v-for="option in roles" :key="option.code" :value="option.code">{{ roleLabel(option.code) }}</el-checkbox>
      </el-checkbox-group>
    </el-form-item>
  </div>
  <h3 class="profile-heading">{{ t('suppliers.settlement') }}</h3>
  <div class="profile-grid settlement-grid">
    <el-form-item :label="t('suppliers.currency')">
      <el-select v-model="currencyCode" filterable allow-create default-first-option :placeholder="t('suppliers.currencyPlaceholder')">
        <el-option v-for="currency in ['USD', 'EUR', 'CNY', 'GBP']" :key="currency" :label="currency" :value="currency" />
      </el-select>
    </el-form-item>
    <el-form-item :label="t('suppliers.paymentTerm')">
      <el-select v-model="model.paymentTerm" filterable allow-create default-first-option clearable :placeholder="t('suppliers.paymentTermPlaceholder')">
        <el-option v-for="option in payments" :key="option.code" :label="option.label" :value="option.code" />
      </el-select>
    </el-form-item>
  </div>
  <h3 class="profile-heading">{{ t('suppliers.addressAndNotes') }}</h3>
  <div class="profile-grid">
    <el-form-item :label="t('suppliers.registeredAddress')" class="profile-wide"><el-input v-model="model.registeredAddress" /></el-form-item>
    <el-form-item :label="t('suppliers.address')" class="profile-wide"><el-input v-model="model.address" type="textarea" :autosize="{ minRows: 2 }" /></el-form-item>
    <el-form-item :label="t('suppliers.remark')" class="profile-wide"><el-input v-model="model.remark" type="textarea" :autosize="{ minRows: 2 }" /></el-form-item>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { countryOptions } from '../lib/countries'
import type { LookupOption } from '../lib/supplierDetailLookups'
import type { SupplierProfile } from '../lib/supplierProfile'

const model = defineModel<SupplierProfile>({ required: true })
const props = defineProps<{ businessOptions: LookupOption[]; payments: LookupOption[]; roleLabel: (role: string) => string }>()
const { t, locale } = useI18n()
const currencyCode = computed({
  get: () => model.value.currency,
  set: (value: string) => { model.value.currency = value.trim().toUpperCase() },
})
const countries = computed(() => countryOptions(locale.value))
const roles = computed(() => {
  const known = new Set(props.businessOptions.map(({ code }) => code))
  return [...props.businessOptions, ...model.value.businessTypes.filter(code => !known.has(code)).map(code => ({ code, label: code }))]
})
</script>

<style scoped>
.profile-grid { display: grid; grid-template-columns: repeat(3, minmax(0, 1fr)); gap: 0 16px; }
.profile-grid :deep(.el-form-item) { min-width: 0; margin-bottom: 10px; }
.profile-grid :deep(.el-form-item__label) { margin-bottom: 2px; }
.profile-grid :deep(.el-select) { width: 100%; }
.profile-grid :deep(.el-checkbox-group) { display: flex; flex-wrap: wrap; gap: 2px 15px; }
.profile-grid :deep(.el-checkbox) { margin-right: 0; white-space: normal; }
.profile-wide { grid-column: 1 / -1; }
.profile-heading { margin: 8px 0 10px; padding-top: 8px; border-top: 1px solid #e4ebf3; font-size: 14px; color: #173b52; }
.settlement-grid { grid-template-columns: minmax(100px, 150px) minmax(0, 420px); }
@media (max-width: 760px) { .profile-grid { grid-template-columns: repeat(2, minmax(0, 1fr)); } }
@media (max-width: 510px) { .profile-grid { grid-template-columns: minmax(0, 1fr); } }
</style>
