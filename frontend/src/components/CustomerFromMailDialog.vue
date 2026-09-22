<template>
  <el-dialog
    :model-value="open"
    :title="t('emails.createCustomerTitle')"
    width="min(920px, 94vw)"
    top="4vh"
    append-to-body
    draggable
    overflow
    :modal="false"
    :close-on-click-modal="false"
    class="customer-from-mail-dialog"
    @update:model-value="emit('update:open', $event)"
  >
    <p class="dialog-help">{{ t('emails.customerForm.moveHelp') }}</p>
    <el-alert
      v-if="matching"
      type="info"
      :closable="false"
      show-icon
      :title="t('emails.customerForm.matching')"
      class="match-alert"
    />
    <el-alert v-else-if="exactEmailOwner" type="warning" :closable="false" show-icon class="match-alert">
      <template #title>
        {{ t('emails.customerForm.emailMatched', { code: exactEmailOwner.code, name: exactEmailOwner.name }) }}
      </template>
      <el-button size="small" type="warning" plain @click="useExistingCustomer(exactEmailOwner)">
        {{ t('emails.customerForm.useExisting') }}
      </el-button>
    </el-alert>

    <template v-if="!exactEmailOwner">
      <!-- 邮件已经给了联系人姓名和邮箱；先用这三项完成新客户建档。
           其他基础数据都是选填，不该在这一刻挡住用户。 -->
      <el-form :model="form" label-width="110px" class="customer-grid core-fields">
        <el-form-item :label="t('customers.name')" required>
          <el-input v-model="form.name" maxlength="200" :placeholder="t('emails.customerForm.companyNamePlaceholder')" />
        </el-form-item>
        <el-form-item :label="t('customers.contactName')" required>
          <el-input v-model="form.contactName" />
        </el-form-item>
        <el-form-item :label="t('customers.contactEmail')" required class="full-row">
          <el-input v-model="form.contactEmail" />
        </el-form-item>
      </el-form>

      <el-alert v-if="targetCompany" type="success" :closable="false" show-icon class="match-alert">
        <template #title>
          {{ t('emails.customerForm.companyMatched', { code: targetCompany.code, name: targetCompany.name }) }}
        </template>
        {{ t('emails.customerForm.companyMatchedHint') }}
        <el-button size="small" link type="primary" @click="clearCompanyTarget">
          {{ t('emails.customerForm.createSeparateCompany') }}
        </el-button>
      </el-alert>
      <div v-else-if="suggestions.length" class="match-candidates">
        <span>{{ t('emails.customerForm.possibleMatches') }}</span>
        <el-button
          v-for="candidate in suggestions"
          :key="candidate.id"
          size="small"
          plain
          @click="selectExistingCompany(candidate)"
        >
          {{ candidate.code }} · {{ candidate.name }}
        </el-button>
      </div>

      <el-collapse v-model="expandedPanels" class="advanced-collapse">
        <el-collapse-item :title="t('emails.customerForm.moreOptional')" name="advanced">
    <el-tabs v-model="activeSection">
      <el-tab-pane :label="t('emails.customerForm.basic')" name="basic">
        <el-form :model="form" label-width="110px" class="customer-grid">
          <el-form-item :label="t('customers.code')">
            <el-input v-model="form.code" :placeholder="t('customers.codeAuto')" />
          </el-form-item>
          <el-form-item :label="t('emails.customerForm.shortName')">
            <el-input v-model="form.shortName" />
          </el-form-item>
          <el-form-item :label="t('emails.customerForm.englishName')">
            <el-input v-model="form.englishName" />
          </el-form-item>
          <el-form-item :label="t('customers.country')">
            <el-select v-model="form.countryCode" filterable clearable style="width:100%" :placeholder="t('emails.unassignedCountry')">
              <el-option v-for="c in countries" :key="c.code" :label="c.name" :value="c.code" />
            </el-select>
          </el-form-item>
          <el-form-item :label="t('customers.type')">
            <el-select v-model="form.customerType" clearable style="width:100%">
              <el-option v-for="o in typeOptions" :key="o.code" :label="o.label" :value="o.code" />
            </el-select>
          </el-form-item>
          <el-form-item :label="t('emails.customerForm.industry')">
            <el-input v-model="form.industry" />
          </el-form-item>
          <el-form-item :label="t('emails.customerForm.source')">
            <el-select v-model="form.source" clearable style="width:100%">
              <el-option v-for="o in sourceOptions" :key="o.code" :label="o.label" :value="o.code" />
            </el-select>
          </el-form-item>
          <el-form-item :label="t('customers.businessStatus')">
            <el-select v-model="form.businessStatus" style="width:100%">
              <el-option :label="t('customers.statusProspect')" value="PROSPECT" />
              <el-option :label="t('customers.statusCooperating')" value="COOPERATING" />
              <el-option :label="t('customers.statusPaused')" value="PAUSED" />
            </el-select>
          </el-form-item>
          <el-form-item :label="t('emails.customerForm.website')">
            <el-input v-model="form.website" placeholder="https://example.com" />
          </el-form-item>
          <el-form-item :label="t('emails.customerForm.language')">
            <el-input v-model="form.primaryLanguage" />
          </el-form-item>
          <el-form-item :label="t('customers.timezone')">
            <el-select v-model="form.timezone" filterable clearable style="width:100%" :placeholder="t('customers.timezonePick')">
              <el-option v-for="zone in timezoneOptions" :key="zone" :label="zone" :value="zone" />
            </el-select>
          </el-form-item>
          <el-form-item :label="t('emails.customerForm.tags')" class="full-row">
            <el-select v-model="form.tags" multiple allow-create filterable default-first-option style="width:100%" />
          </el-form-item>
          <el-form-item :label="t('customers.remark')" class="full-row">
            <el-input v-model="form.remark" type="textarea" :rows="2" />
          </el-form-item>
        </el-form>
      </el-tab-pane>

      <el-tab-pane :label="t('emails.customerForm.contact')" name="contact">
        <el-form :model="form" label-width="110px" class="customer-grid">
          <el-form-item :label="t('emails.customerForm.department')"><el-input v-model="form.contactDepartment" /></el-form-item>
          <el-form-item :label="t('emails.customerForm.title')"><el-input v-model="form.contactTitle" /></el-form-item>
          <el-form-item :label="t('customers.contactPhone')"><el-input v-model="form.contactPhone" /></el-form-item>
          <el-form-item :label="t('emails.customerForm.mobile')"><el-input v-model="form.contactMobile" /></el-form-item>
          <el-form-item :label="t('emails.customerForm.instantMessaging')"><el-input v-model="form.contactInstantMessaging" placeholder="WhatsApp / WeChat" /></el-form-item>
          <el-form-item :label="t('emails.customerForm.contactLanguage')"><el-input v-model="form.contactLanguage" /></el-form-item>
          <el-form-item :label="t('emails.customerForm.emailPermission')">
            <el-select v-model="form.emailPermission" style="width:100%">
              <el-option :label="t('emails.customerForm.emailAllowed')" value="ALLOWED" />
              <el-option :label="t('emails.customerForm.emailOptedOut')" value="OPTED_OUT" />
              <el-option :label="t('emails.customerForm.emailInvalid')" value="INVALID" />
            </el-select>
          </el-form-item>
          <el-form-item :label="t('emails.customerForm.emailCategories')">
            <el-select v-model="form.emailCategories" multiple clearable style="width:100%" :placeholder="t('emails.customerForm.allEmailCategories')">
              <el-option :label="t('emails.customerForm.categoryBusiness')" value="BUSINESS" />
              <el-option :label="t('emails.customerForm.categoryQuotation')" value="QUOTATION" />
              <el-option :label="t('emails.customerForm.categoryShipping')" value="SHIPPING" />
              <el-option :label="t('emails.customerForm.categoryMarketing')" value="MARKETING" />
            </el-select>
          </el-form-item>
          <el-form-item :label="t('emails.customerForm.contactRemark')" class="full-row"><el-input v-model="form.contactRemark" type="textarea" :rows="3" /></el-form-item>
        </el-form>
      </el-tab-pane>

      <el-tab-pane :label="t('emails.customerForm.tax')" name="tax">
        <el-form :model="form" label-width="110px" class="customer-grid">
          <el-form-item :label="t('emails.customerForm.registeredName')"><el-input v-model="form.registeredName" /></el-form-item>
          <el-form-item :label="t('emails.customerForm.registrationNo')"><el-input v-model="form.registrationNo" /></el-form-item>
          <el-form-item :label="t('emails.customerForm.taxId')"><el-input v-model="form.taxId" /></el-form-item>
          <el-form-item :label="t('emails.customerForm.invoiceTitle')"><el-input v-model="form.invoiceTitle" /></el-form-item>
          <el-form-item :label="t('emails.customerForm.invoiceTaxNo')"><el-input v-model="form.invoiceTaxNo" /></el-form-item>
          <el-form-item :label="t('emails.customerForm.invoiceRemark')" class="full-row"><el-input v-model="form.invoiceRemark" type="textarea" :rows="3" /></el-form-item>
        </el-form>
      </el-tab-pane>

      <el-tab-pane :label="t('emails.customerForm.settlement')" name="settlement">
        <el-form :model="form" label-width="110px" class="customer-grid">
          <el-form-item :label="t('customers.currency')"><el-select v-model="form.currency" style="width:100%"><el-option v-for="c in CURRENCIES" :key="c" :label="c" :value="c" /></el-select></el-form-item>
          <el-form-item :label="t('customers.paymentTerm')"><el-select v-model="form.paymentTerm" clearable style="width:100%"><el-option v-for="o in paymentOptions" :key="o.code" :label="o.label" :value="o.code" /></el-select></el-form-item>
          <el-form-item :label="t('emails.customerForm.creditLimit')"><el-input-number v-model="form.creditAmount" :min="0" :precision="2" style="width:100%" /></el-form-item>
          <el-form-item :label="t('emails.customerForm.creditCurrency')"><el-select v-model="form.creditCurrency" style="width:100%"><el-option v-for="c in CURRENCIES" :key="c" :label="c" :value="c" /></el-select></el-form-item>
          <el-form-item :label="t('emails.customerForm.creditStatus')"><el-select v-model="form.creditStatus" style="width:100%"><el-option :label="t('emails.customerForm.creditNormal')" value="NORMAL" /><el-option :label="t('emails.customerForm.creditWatch')" value="WATCH" /><el-option :label="t('emails.customerForm.creditSuspended')" value="CREDIT_SUSPENDED" /></el-select></el-form-item>
        </el-form>
      </el-tab-pane>

      <el-tab-pane :label="t('emails.customerForm.address')" name="address">
        <el-form :model="form" label-width="110px" class="customer-grid">
          <el-form-item :label="t('emails.customerForm.addAddress')" class="full-row"><el-switch v-model="form.includeAddress" /></el-form-item>
          <template v-if="form.includeAddress">
            <el-form-item :label="t('emails.customerForm.addressType')"><el-select v-model="form.addressType" style="width:100%"><el-option :label="t('emails.customerForm.addressRegistered')" value="REGISTERED" /><el-option :label="t('emails.customerForm.addressOffice')" value="OFFICE" /><el-option :label="t('emails.customerForm.addressBilling')" value="BILLING" /><el-option :label="t('emails.customerForm.addressShipping')" value="SHIPPING" /></el-select></el-form-item>
            <el-form-item :label="t('customers.country')"><el-select v-model="form.addressCountryCode" filterable clearable style="width:100%"><el-option v-for="c in countries" :key="c.code" :label="c.name" :value="c.code" /></el-select></el-form-item>
            <el-form-item :label="t('emails.customerForm.state')"><el-input v-model="form.addressState" /></el-form-item>
            <el-form-item :label="t('emails.customerForm.city')"><el-input v-model="form.addressCity" /></el-form-item>
            <el-form-item :label="t('emails.customerForm.postalCode')"><el-input v-model="form.addressPostalCode" /></el-form-item>
            <el-form-item :label="t('emails.customerForm.defaultAddress')"><el-switch v-model="form.addressDefault" /></el-form-item>
            <el-form-item :label="t('emails.customerForm.addressLine')" required class="full-row"><el-input v-model="form.addressLine" type="textarea" :rows="3" /></el-form-item>
          </template>
        </el-form>
      </el-tab-pane>
    </el-tabs>
        </el-collapse-item>
      </el-collapse>
    </template>
    <template #footer>
      <el-button @click="emit('update:open', false)">{{ t('common.cancel') }}</el-button>
      <el-button v-if="!exactEmailOwner" type="primary" :loading="saving" @click="save">
        {{ targetCompany ? t('emails.addContactToExisting') : t('common.save') }}
      </el-button>
    </template>
  </el-dialog>
</template>

<script setup lang="ts">
import { computed, reactive, ref, watch } from 'vue'
import { ElMessage } from 'element-plus'
import { useI18n } from 'vue-i18n'
import { get, post, quietErrors } from '../api'
import { CURRENCIES } from '../constants'
import { countryOptions } from '../lib/countries'
import { validateCustomerContact, validateCustomerProfile } from '../lib/customerForms'
import {
  classifyMailCustomerDuplicates,
  type MailCustomerDraft,
  type MailCustomerDuplicateCandidate,
} from '../lib/mailCustomerDraft'
import { confirmPossibleDuplicates } from '../lib/masterDataDuplicates'
import { portTimezoneOptions } from '../lib/portOptions'

interface OptionItem { code: string; label: string }

const props = defineProps<{ open: boolean; draft: MailCustomerDraft | null }>()
const emit = defineEmits<{ 'update:open': [value: boolean]; created: [customer: { id: string; code: string; name: string }] }>()
const { t, locale } = useI18n()
const activeSection = ref('basic')
const expandedPanels = ref<string[]>([])
const saving = ref(false)
const matching = ref(false)
const exactEmailOwner = ref<MailCustomerDuplicateCandidate | null>(null)
const matchedCompany = ref<MailCustomerDuplicateCandidate | null>(null)
const selectedCompany = ref<MailCustomerDuplicateCandidate | null>(null)
const ignoreCompanyMatch = ref(false)
const suggestions = ref<MailCustomerDuplicateCandidate[]>([])
const targetCompany = computed(() => selectedCompany.value ?? matchedCompany.value)
const typeOptions = ref<OptionItem[]>([])
const sourceOptions = ref<OptionItem[]>([])
const paymentOptions = ref<OptionItem[]>([])
const countries = computed(() => countryOptions(locale.value))

function emptyForm() {
  return {
    code: '', name: '', shortName: '', englishName: '', countryCode: '', customerType: '', industry: '',
    source: 'EMAIL', businessStatus: 'PROSPECT', website: '', primaryLanguage: '', timezone: '', tags: [] as string[], remark: '',
    contactName: '', contactEmail: '', contactDepartment: '', contactTitle: '', contactPhone: '', contactMobile: '',
    contactInstantMessaging: '', contactLanguage: '', contactRemark: '', emailPermission: 'ALLOWED', emailCategories: [] as string[],
    registeredName: '', registrationNo: '', taxId: '', invoiceTitle: '', invoiceTaxNo: '', invoiceRemark: '',
    currency: 'USD', paymentTerm: '', creditAmount: 0, creditCurrency: 'USD', creditStatus: 'NORMAL',
    includeAddress: false, addressType: 'OFFICE', addressCountryCode: '', addressState: '', addressCity: '',
    addressPostalCode: '', addressLine: '', addressDefault: true,
  }
}
const form = reactive(emptyForm())
const timezoneOptions = computed(() => portTimezoneOptions(form.countryCode))

let matchTimer = 0
let matchSequence = 0

function applyDuplicateCandidates(candidates: MailCustomerDuplicateCandidate[]) {
  const result = classifyMailCustomerDuplicates(candidates)
  exactEmailOwner.value = result.emailOwner
  matchedCompany.value = ignoreCompanyMatch.value ? null : result.existingCompany
  suggestions.value = ignoreCompanyMatch.value && result.existingCompany
    ? [result.existingCompany, ...result.suggestions]
    : result.suggestions
}

async function precheckDuplicates() {
  const sequence = ++matchSequence
  const name = form.name.trim()
  const email = form.contactEmail.trim()
  if (!name && !email) {
    applyDuplicateCandidates([])
    matching.value = false
    return
  }
  matching.value = true
  try {
    const duplicates = await get<{ candidates?: MailCustomerDuplicateCandidate[] }>(
      '/customers/duplicates', { name, email }, quietErrors,
    )
    if (sequence !== matchSequence) return
    applyDuplicateCandidates(duplicates.candidates ?? [])
  } catch {
    // 预检失败不阻塞建档；保存时仍会做最终检查。
    if (sequence === matchSequence) applyDuplicateCandidates([])
  } finally {
    if (sequence === matchSequence) matching.value = false
  }
}

function schedulePrecheck(delay = 350) {
  window.clearTimeout(matchTimer)
  matchTimer = window.setTimeout(() => void precheckDuplicates(), delay)
}

function selectExistingCompany(candidate: MailCustomerDuplicateCandidate) {
  ignoreCompanyMatch.value = false
  selectedCompany.value = candidate
  matchedCompany.value = null
  suggestions.value = suggestions.value.filter((item) => item.id !== candidate.id)
}

function clearCompanyTarget() {
  const previous = targetCompany.value
  selectedCompany.value = null
  matchedCompany.value = null
  ignoreCompanyMatch.value = true
  if (previous && !suggestions.value.some((item) => item.id === previous.id)) {
    suggestions.value = [previous, ...suggestions.value]
  }
}

function useExistingCustomer(customer: MailCustomerDuplicateCandidate) {
  emit('update:open', false)
  emit('created', customer)
}

watch(() => form.countryCode, (countryCode, previous) => {
  if (!form.addressCountryCode || form.addressCountryCode === previous) form.addressCountryCode = countryCode
  if (form.timezone && !timezoneOptions.value.includes(form.timezone)) form.timezone = ''
})

watch([() => form.name, () => form.contactEmail], () => {
  if (!props.open) return
  ignoreCompanyMatch.value = false
  selectedCompany.value = null
  schedulePrecheck()
})

watch(() => props.open, async (open) => {
  if (!open) {
    window.clearTimeout(matchTimer)
    matchSequence++
    matching.value = false
    return
  }
  Object.assign(form, emptyForm(), {
    // From 只能确定联系人，不能把一个人名当成公司名。
    name: '', contactName: props.draft?.name ?? '', contactEmail: props.draft?.email ?? '',
  })
  activeSection.value = 'basic'
  expandedPanels.value = []
  exactEmailOwner.value = null
  matchedCompany.value = null
  selectedCompany.value = null
  ignoreCompanyMatch.value = false
  suggestions.value = []
  schedulePrecheck(0)
  try {
    const [types, sources, payments] = await Promise.all([
      get<{ options: OptionItem[] }>('/options', { category: 'CUSTOMER_TYPE' }),
      get<{ options: OptionItem[] }>('/options', { category: 'CUSTOMER_SOURCE' }),
      get<{ options: OptionItem[] }>('/options', { category: 'PAYMENT_METHOD' }),
    ])
    typeOptions.value = types.options ?? []
    sourceOptions.value = sources.options ?? []
    paymentOptions.value = payments.options ?? []
  } catch {
    // The form remains usable; the shared API interceptor has already shown the error.
  }
})

async function save() {
  const name = form.name.trim()
  const email = form.contactEmail.trim()
  if (!name) { ElMessage.warning(t('customers.required')); return }
  const contactError = validateCustomerContact({ name: form.contactName, email, phone: form.contactPhone, mobile: form.contactMobile })
  if (contactError) {
    if (contactError === 'phoneInvalid') {
      expandedPanels.value = ['advanced']
      activeSection.value = 'contact'
    }
    ElMessage.warning(t(`customers.${contactError}`))
    return
  }
  const profileError = validateCustomerProfile({ website: form.website, timezone: form.timezone })
  if (profileError) {
    expandedPanels.value = ['advanced']
    activeSection.value = 'basic'
    ElMessage.warning(t(`customers.${profileError}`))
    return
  }
  if (form.includeAddress && !form.addressLine.trim()) {
    expandedPanels.value = ['advanced']
    activeSection.value = 'address'
    ElMessage.warning(t('emails.customerForm.addressRequired'))
    return
  }

  saving.value = true
  try {
    const duplicates = await get<{ candidates?: MailCustomerDuplicateCandidate[] }>(
      '/customers/duplicates', { name, email, tax_id: form.taxId },
    )
    const candidates = duplicates.candidates ?? []
    const result = classifyMailCustomerDuplicates(candidates)
    if (result.emailOwner) {
      applyDuplicateCandidates(candidates)
      ElMessage.warning(t('emails.customerEmailExists', {
        code: result.emailOwner.code,
        name: result.emailOwner.name,
      }))
      return
    }
    const contact = {
      name: form.contactName.trim(), email, department: form.contactDepartment, title: form.contactTitle,
      phone: form.contactPhone, mobile: form.contactMobile, instantMessaging: form.contactInstantMessaging,
      language: form.contactLanguage, remark: form.contactRemark, isPrimary: true, emailPermission: form.emailPermission,
      emailCategories: form.emailCategories,
    }
    const existingCompany = selectedCompany.value
      ?? (ignoreCompanyMatch.value ? null : result.existingCompany)
    if (existingCompany) {
      const existingContacts = await get<{ contacts?: Array<{ email?: string }> }>(
        `/customers/${existingCompany.id}/contacts`, { status: 'ALL' },
      )
      if ((existingContacts.contacts ?? []).some((item) => item.email?.trim().toLowerCase() === email.toLowerCase())) {
        ElMessage.warning(t('emails.customerEmailExists', { code: existingCompany.code, name: existingCompany.name }))
        return
      }
      // 名称在最终检查才刚刚命中时，先把结果放回表单，不用一个
      // 提交末尾的弹窗迫使用户立即决定。下一次按键文案会明确说
      // “添加联系人”。
      if (targetCompany.value?.id !== existingCompany.id) {
        matchedCompany.value = existingCompany
        suggestions.value = result.suggestions
        ElMessage.info(t('emails.customerForm.reviewCompanyMatch'))
        return
      }
      await post(`/customers/${existingCompany.id}/contacts`, {
        contact: { ...contact, isPrimary: false },
      })
      emit('update:open', false)
      emit('created', existingCompany)
      ElMessage.success(t('emails.customerContactAdded', {
        contact: contact.name,
        code: existingCompany.code,
        name: existingCompany.name,
      }))
      return
    }
    await confirmPossibleDuplicates(ignoreCompanyMatch.value ? candidates : result.suggestions, t)
    const { customer } = await post<{ customer: { id: string; code: string; name: string } }>('/customers', {
      code: form.code, name, country: '', countryCode: form.countryCode, address: form.includeAddress ? form.addressLine : '',
      currency: form.currency, paymentTerm: form.paymentTerm, remark: form.remark, contacts: [contact],
      shortName: form.shortName, englishName: form.englishName, customerType: form.customerType, industry: form.industry,
      source: form.source || 'EMAIL', tags: form.tags, website: form.website, primaryLanguage: form.primaryLanguage,
      timezone: form.timezone, registeredName: form.registeredName, registrationNo: form.registrationNo, taxId: form.taxId,
      invoiceTitle: form.invoiceTitle, invoiceTaxNo: form.invoiceTaxNo, invoiceRemark: form.invoiceRemark,
      creditLimitMinor: Math.round(Number(form.creditAmount || 0) * 100), creditCurrency: form.creditCurrency,
      creditStatus: form.creditStatus, businessStatus: form.businessStatus,
    })
    if (form.includeAddress) {
      await post(`/customers/${customer.id}/addresses`, { address: {
        addressType: form.addressType, countryCode: form.addressCountryCode, state: form.addressState,
        city: form.addressCity, postalCode: form.addressPostalCode, addressLine: form.addressLine,
        isDefault: form.addressDefault, sortOrder: 0,
      } })
    }
    emit('update:open', false)
    emit('created', customer)
    ElMessage.success(t('emails.customerCreated', { code: customer.code }))
  } finally {
    saving.value = false
  }
}
</script>

<style scoped>
.dialog-help { margin: -6px 0 10px; color: var(--el-text-color-secondary); font-size: 12px; }
.customer-grid { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 0 18px; }
.full-row { grid-column: 1 / -1; }
.core-fields { padding: 14px 14px 0; border: 1px solid var(--el-border-color-lighter); border-radius: 8px; }
.match-alert { margin-bottom: 12px; }
.match-alert :deep(.el-alert__content) { min-width: 0; }
.match-alert .el-button { margin: 8px 0 0; }
.match-candidates { display: flex; align-items: center; flex-wrap: wrap; gap: 8px; margin: 0 0 12px; padding: 10px 12px; border: 1px solid var(--el-color-warning-light-5); border-radius: 8px; background: var(--el-color-warning-light-9); color: var(--el-text-color-regular); font-size: 13px; }
.advanced-collapse { margin-top: 12px; }
.advanced-collapse :deep(.el-collapse-item__header) { font-weight: 600; }
@media (max-width: 720px) { .customer-grid { grid-template-columns: 1fr; } .full-row { grid-column: auto; } }
:global(.customer-from-mail-dialog .el-dialog__body) { max-height: 72vh; overflow: auto; }
</style>
